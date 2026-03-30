package rooms

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/segmentio/kafka-go"
	ycrdt "github.com/skyterra/y-crdt"

	kafkaPkg "parily.dev/app/internal/kafka"
	mongoRepo "parily.dev/app/internal/mongo"
	pg "parily.dev/app/internal/postgres"
)

// historyEntry is one item in the version timeline returned to the frontend.
type historyEntry struct {
	Version int       `json:"version"`
	SavedAt time.Time `json:"saved_at"`
	UserID  string    `json:"user_id"`
}

// GetHistory returns the list of all saved versions for a file.
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

	c.JSON(http.StatusOK, gin.H{"history": entries})
}

// GetHistoryAtVersion reconstructs the document text at a specific version.
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

	_, err = pg.GetMemberRole(c.Request.Context(), h.db, roomID, userID)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	text, err := h.replayYjsToVersion(c.Request.Context(), fileID, targetVersion)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not replay history"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"version": targetVersion,
		"text":    text,
	})
}

// RestoreVersion reconstructs document at target version and saves to MongoDB.
// POST /api/rooms/:roomID/files/:fileID/restore
// Body: { "version": 41 }
func (h *Handler) RestoreVersion(c *gin.Context) {
	roomID := c.Param("roomID")
	fileID := c.Param("fileID")
	userID := c.GetString("userID")

	var req struct {
		Version int `json:"version" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "version is required"})
		return
	}

	role, err := pg.GetMemberRole(c.Request.Context(), h.db, roomID, userID)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	if role == "viewer" {
		c.JSON(http.StatusForbidden, gin.H{"error": "viewers cannot restore versions"})
		return
	}

	text, err := h.replayYjsToVersion(c.Request.Context(), fileID, req.Version)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not replay history"})
		return
	}

	// wrap plain text back into Yjs binary.
	// NewDoc(guid, gc, gcFilter, meta, autoLoad)
	// Insert(index, str, attributes) — nil attributes = no formatting
	// EncodeStateAsUpdate(doc, stateVector) — nil stateVector = full state
	doc := ycrdt.NewDoc("restore", false, nil, nil, false)
	ytext := doc.GetText("content")
	doc.Transact(func(trans *ycrdt.Transaction) {
		ytext.Insert(0, text, nil)
	}, nil)
	snapshot := ycrdt.EncodeStateAsUpdate(doc, nil)

	docRepo := mongoRepo.NewDocumentRepository(h.mongoDB)
	if _, err := docRepo.SaveDocument(c.Request.Context(), fileID, snapshot); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save restored state"})
		return
	}

	// broadcast so all connected clients reload from MongoDB
	h.publishFiles(c.Request.Context(), roomID)

	c.JSON(http.StatusOK, gin.H{
		"message": "restored",
		"version": req.Version,
	})
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func (h *Handler) readHistoryEntries(ctx context.Context, fileID string, maxVersion int) ([]historyEntry, error) {
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

// replayYjsToVersion applies Kafka edit-events for fileID up to targetVersion
// onto a fresh Yjs doc in order and returns plain text.
// ApplyUpdate is package-level: ApplyUpdate(doc, update, transactionOrigin)
func (h *Handler) replayYjsToVersion(ctx context.Context, fileID string, targetVersion int) (string, error) {
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

		if event.Version >= targetVersion {
			break
		}
	}

	return doc.GetText("content").ToString(), nil
}