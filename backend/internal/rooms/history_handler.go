package rooms

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/segmentio/kafka-go"
	ycrdt "github.com/skyterra/y-crdt"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"

	kafkaPkg "parily.dev/app/internal/kafka"
	"parily.dev/app/internal/metrics"
	mongoRepo "parily.dev/app/internal/mongo"
	pg "parily.dev/app/internal/postgres"
)

type historyEntry struct {
	Version int       `json:"version"`
	SavedAt time.Time `json:"saved_at"`
	UserID  string    `json:"user_id"`
}

// GET /api/rooms/:roomID/files/:fileID/history
func (h *Handler) GetHistory(c *gin.Context) {
	roomID := c.Param("roomID")
	fileID := c.Param("fileID")
	userID := c.GetString("userID")

	_, err := pg.GetMemberRole(c.Request.Context(), h.db, roomID, userID)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	entries, err := h.readHistoryEntries(c.Request.Context(), fileID, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read history"})
		return
	}

	log.Printf("[history] GetHistory file=%s entries=%d", fileID, len(entries))
	c.JSON(http.StatusOK, gin.H{"history": entries})
}

// GET /api/rooms/:roomID/files/:fileID/history/:version
func (h *Handler) GetHistoryAtVersion(c *gin.Context) {
	roomID := c.Param("roomID")
	fileID := c.Param("fileID")
	userID := c.GetString("userID")
	versionStr := c.Param("version")

	targetVersion, err := strconv.Atoi(versionStr)
	if err != nil || targetVersion < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid version"})
		return
	}

	// span wraps the entire version retrieval
	// snapshot.hit attribute tells you in Jaeger whether we hit MongoDB or replayed Kafka
	// KafkaReplay child span shows exactly how long the slow path takes
	tracer := otel.Tracer("pairly")
	ctx, span := tracer.Start(c.Request.Context(), "GetHistoryAtVersion",
		oteltrace.WithAttributes(
			attribute.String("room.id", roomID),
			attribute.String("file.id", fileID),
			attribute.String("user.id", userID),
			attribute.Int("target.version", targetVersion),
		),
	)
	defer span.End()

	_, err = pg.GetMemberRole(ctx, h.db, roomID, userID)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member"})
		return
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	snapRepo := mongoRepo.NewSnapshotRepository(h.mongoDB)
	snap, err := snapRepo.GetSnapshot(ctx, fileID, targetVersion)
	if err != nil {
		log.Printf("[history] snapshot lookup error file=%s version=%d err=%v — falling back to replay", fileID, targetVersion, err)
	}

	if snap != nil {
		log.Printf("[history] snapshot hit file=%s version=%d", fileID, targetVersion)
		metrics.SnapshotHitTotal.Inc()
		span.SetAttributes(attribute.Bool("snapshot.hit", true))
		c.JSON(http.StatusOK, gin.H{"version": targetVersion, "text": snap.Text})
		return
	}

	log.Printf("[history] snapshot miss file=%s version=%d — replaying from Kafka", fileID, targetVersion)
	metrics.SnapshotMissTotal.Inc()
	span.SetAttributes(attribute.Bool("snapshot.hit", false))

	// child span for Kafka replay — the slow path the snapshot cache was built to avoid
	// seeing this span in Jaeger with a long duration tells you cache is not warming
	_, replaySpan := tracer.Start(ctx, "KafkaReplay",
		oteltrace.WithAttributes(
			attribute.Int("target.version", targetVersion),
		),
	)
	text, err := h.replayYjsToVersion(ctx, fileID, targetVersion)
	replaySpan.End()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not replay history"})
		return
	}

	// opportunistically backfill the snapshot so the next request is instant
	go func() {
		gctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := snapRepo.SaveSnapshot(gctx, fileID, roomID, targetVersion, text); err != nil {
			log.Printf("[history] failed to backfill snapshot file=%s version=%d err=%v", fileID, targetVersion, err)
		} else {
			log.Printf("[history] backfilled snapshot file=%s version=%d", fileID, targetVersion)
		}
	}()

	log.Printf("[history] GetHistoryAtVersion file=%s version=%d text_len=%d", fileID, targetVersion, len(text))
	c.JSON(http.StatusOK, gin.H{"version": targetVersion, "text": text})
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func (h *Handler) newKafkaReader() (*kafka.Reader, int64, error) {
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:   []string{h.kafkaBroker},
		Topic:     "edit-events",
		Partition: 0,
		MinBytes:  1,
		MaxBytes:  10e6,
	})

	conn, err := kafka.DialLeader(context.Background(), "tcp", h.kafkaBroker, "edit-events", 0)
	if err != nil {
		r.Close()
		return nil, 0, err
	}
	defer conn.Close()

	_, high, err := conn.ReadOffsets()
	if err != nil {
		r.Close()
		return nil, 0, err
	}

	r.SetOffset(kafka.FirstOffset)
	return r, high, nil
}

func (h *Handler) readHistoryEntries(ctx context.Context, fileID string, maxVersion int) ([]historyEntry, error) {
	r, highWatermark, err := h.newKafkaReader()
	if err != nil {
		log.Printf("[history] could not get high watermark, falling back to timeout: %v", err)
		return h.readHistoryEntriesWithTimeout(ctx, fileID, maxVersion)
	}
	defer r.Close()

	var entries []historyEntry
	var offset int64 = 0

	for offset < highWatermark {
		msg, err := r.FetchMessage(ctx)
		if err != nil {
			break
		}
		offset = msg.Offset + 1

		var event kafkaPkg.EditEvent
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			continue
		}
		if event.FileID != fileID {
			continue
		}

		entries = append(entries, historyEntry{
			Version: event.Version,
			SavedAt: event.SavedAt,
			UserID:  event.UserID,
		})

		if maxVersion > 0 && event.Version >= maxVersion {
			break
		}
	}

	return entries, nil
}

func (h *Handler) replayYjsToVersion(ctx context.Context, fileID string, targetVersion int) (string, error) {
	r, highWatermark, err := h.newKafkaReader()
	if err != nil {
		log.Printf("[history] could not get high watermark, falling back to timeout: %v", err)
		return h.replayYjsToVersionWithTimeout(ctx, fileID, targetVersion)
	}
	defer r.Close()

	doc := ycrdt.NewDoc("replay", false, nil, nil, false)
	var offset int64 = 0

	for offset < highWatermark {
		msg, err := r.FetchMessage(ctx)
		if err != nil {
			break
		}
		offset = msg.Offset + 1

		var event kafkaPkg.EditEvent
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			continue
		}
		if event.FileID != fileID {
			continue
		}

		doc.Transact(func(trans *ycrdt.Transaction) {
			ycrdt.ApplyUpdate(doc, event.YjsBlob, nil)
		}, nil)

		log.Printf("[history] applied version=%d for file=%s", event.Version, fileID)

		if event.Version >= targetVersion {
			break
		}
	}

	return doc.GetText("content").ToString(), nil
}

func (h *Handler) readHistoryEntriesWithTimeout(ctx context.Context, fileID string, maxVersion int) ([]historyEntry, error) {
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:   []string{h.kafkaBroker},
		Topic:     "edit-events",
		Partition: 0,
		MinBytes:  1,
		MaxBytes:  10e6,
	})
	defer r.Close()
	r.SetOffset(kafka.FirstOffset)

	var entries []historyEntry
	readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	for {
		msg, err := r.FetchMessage(readCtx)
		if err != nil {
			break
		}
		var event kafkaPkg.EditEvent
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			continue
		}
		if event.FileID != fileID {
			continue
		}
		entries = append(entries, historyEntry{
			Version: event.Version,
			SavedAt: event.SavedAt,
			UserID:  event.UserID,
		})
		if maxVersion > 0 && event.Version >= maxVersion {
			break
		}
	}
	return entries, nil
}

func (h *Handler) replayYjsToVersionWithTimeout(ctx context.Context, fileID string, targetVersion int) (string, error) {
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:   []string{h.kafkaBroker},
		Topic:     "edit-events",
		Partition: 0,
		MinBytes:  1,
		MaxBytes:  10e6,
	})
	defer r.Close()
	r.SetOffset(kafka.FirstOffset)

	doc := ycrdt.NewDoc("replay", false, nil, nil, false)
	readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	for {
		msg, err := r.FetchMessage(readCtx)
		if err != nil {
			break
		}
		var event kafkaPkg.EditEvent
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			continue
		}
		if event.FileID != fileID {
			continue
		}
		doc.Transact(func(trans *ycrdt.Transaction) {
			ycrdt.ApplyUpdate(doc, event.YjsBlob, nil)
		}, nil)
		log.Printf("[history] applied version=%d for file=%s", event.Version, fileID)
		if event.Version >= targetVersion {
			break
		}
	}
	return doc.GetText("content").ToString(), nil
}