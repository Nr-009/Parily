package rooms

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	ycrdt "github.com/skyterra/y-crdt"
	"go.uber.org/zap"

	"parily.dev/app/internal/kafka"
	"parily.dev/app/internal/logger"
	mongoRepo "parily.dev/app/internal/mongo"
	pg "parily.dev/app/internal/postgres"
)

// lastTextCache stores the last saved text per fileID.
// If incoming Yjs blob decodes to the same text, we skip both
// MongoDB and Kafka — content hasn't changed so no point recording it.
var (
	lastTextCache   = make(map[string]string)
	lastTextCacheMu sync.Mutex
)

// yjsBlobToText decodes a Yjs binary update into plain text.
// Same key "content" used by the frontend useYjs hook.
func yjsBlobToText(blob []byte) string {
	doc := ycrdt.NewDoc("dedup", false, nil, nil, false)
	doc.Transact(func(trans *ycrdt.Transaction) {
		ycrdt.ApplyUpdate(doc, blob, nil)
	}, nil)
	return doc.GetText("content").ToString()
}

func (h *Handler) GetFiles(c *gin.Context) {
	roomID := c.Param("roomID")
	userID := c.GetString("userID")
	_, err := pg.GetMemberRole(c.Request.Context(), h.db, roomID, userID)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member of this room"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	files, err := pg.GetFilesForRoom(c.Request.Context(), h.db, roomID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not get files"})
		return
	}
	result := make([]gin.H, 0, len(files))
	for _, f := range files {
		f := f
		result = append(result, fileResponse(&f))
	}
	c.JSON(http.StatusOK, gin.H{"files": result})
}

type createFileRequest struct {
	Name     string  `json:"name"      binding:"required,min=1,max=255"`
	ParentID *string `json:"parent_id"`
	IsFolder bool    `json:"is_folder"`
	Language string  `json:"language"`
}

func (h *Handler) CreateFile(c *gin.Context) {
	roomID := c.Param("roomID")
	userID := c.GetString("userID")
	fmt.Println(">>> CreateFile called room:", roomID)
	var req createFileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	role, err := pg.GetMemberRole(c.Request.Context(), h.db, roomID, userID)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member of this room"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	if role == "viewer" {
		c.JSON(http.StatusForbidden, gin.H{"error": "viewers cannot create files"})
		return
	}
	language := ""
	if !req.IsFolder {
		if req.Language != "" {
			language = req.Language
		} else {
			language = langFromExtension(req.Name)
		}
	}
	file, err := pg.CreateFile(c.Request.Context(), h.db, pg.CreateFileParams{
		RoomID:    roomID,
		Name:      strings.TrimSpace(req.Name),
		Language:  language,
		ParentID:  req.ParentID,
		IsFolder:  req.IsFolder,
		CreatedBy: userID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create file"})
		return
	}
	if !req.IsFolder {
		docRepo := mongoRepo.NewDocumentRepository(h.mongoDB)
		if err := docRepo.CreateDocument(c.Request.Context(), file.ID, roomID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create document"})
			return
		}
	}
	fmt.Println(">>> CreateFile calling publishFiles")
	h.publishFiles(c.Request.Context(), roomID)
	c.JSON(http.StatusCreated, gin.H{"file": fileResponse(file)})
}

type updateFileRequest struct {
	Name     string  `json:"name"`
	Language string  `json:"language"`
	ParentID *string `json:"parent_id"`
}

func (h *Handler) UpdateFile(c *gin.Context) {
	roomID := c.Param("roomID")
	fileID := c.Param("fileID")
	userID := c.GetString("userID")
	fmt.Println(">>> UpdateFile called room:", roomID, "file:", fileID)

	var req updateFileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(req.Name) == "" && req.ParentID == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least name or parent_id is required"})
		return
	}

	role, err := pg.GetMemberRole(c.Request.Context(), h.db, roomID, userID)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member of this room"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	if role == "viewer" {
		c.JSON(http.StatusForbidden, gin.H{"error": "viewers cannot modify files"})
		return
	}

	existing, err := pg.GetFileByID(c.Request.Context(), h.db, fileID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}

	if existing.IsFolder {
		updated, err := pg.UpdateFolder(c.Request.Context(), h.db, pg.UpdateFolderParams{
			FolderID: fileID,
			Name:     strings.TrimSpace(req.Name),
			ParentID: req.ParentID,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not update folder"})
			return
		}
		fmt.Println(">>> UpdateFile (folder) calling publishFiles")
		h.publishFiles(c.Request.Context(), roomID)
		c.JSON(http.StatusOK, gin.H{"file": fileResponse(updated)})
		return
	}

	language := req.Language
	if language == "" {
		language = langFromExtension(req.Name)
	}
	updated, err := pg.UpdateFile(c.Request.Context(), h.db, pg.UpdateFileParams{
		FileID:   fileID,
		Name:     strings.TrimSpace(req.Name),
		Language: language,
		ParentID: req.ParentID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not update file"})
		return
	}
	fmt.Println(">>> UpdateFile (file) calling publishFiles")
	h.publishFiles(c.Request.Context(), roomID)
	c.JSON(http.StatusOK, gin.H{"file": fileResponse(updated)})
}

func (h *Handler) ToggleFile(c *gin.Context) {
	roomID := c.Param("roomID")
	fileID := c.Param("fileID")
	userID := c.GetString("userID")
	fmt.Println(">>> ToggleFile called room:", roomID, "file:", fileID)
	role, err := pg.GetMemberRole(c.Request.Context(), h.db, roomID, userID)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member of this room"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	if role == "viewer" {
		c.JSON(http.StatusForbidden, gin.H{"error": "viewers cannot delete files"})
		return
	}
	existing, err := pg.GetFileByID(c.Request.Context(), h.db, fileID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}
	if existing.IsFolder {
		updated, err := pg.ToggleFolder(c.Request.Context(), h.db, fileID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not toggle folder"})
			return
		}
		result := make([]gin.H, 0, len(updated))
		for _, f := range updated {
			f := f
			result = append(result, fileResponse(&f))
		}
		fmt.Println(">>> ToggleFile (folder) calling publishFiles")
		h.publishFiles(c.Request.Context(), roomID)
		c.JSON(http.StatusOK, gin.H{"files": result})
		return
	}
	updated, err := pg.ToggleFile(c.Request.Context(), h.db, fileID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not toggle file"})
		return
	}
	fmt.Println(">>> ToggleFile (file) calling publishFiles")
	h.publishFiles(c.Request.Context(), roomID)
	c.JSON(http.StatusOK, gin.H{"file": fileResponse(updated)})
}

func (h *Handler) PermanentDeleteFile(c *gin.Context) {
	roomID := c.Param("roomID")
	fileID := c.Param("fileID")
	userID := c.GetString("userID")
	fmt.Println(">>> PermanentDeleteFile called room:", roomID, "file:", fileID)

	role, err := pg.GetMemberRole(c.Request.Context(), h.db, roomID, userID)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member of this room"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	if role == "viewer" {
		c.JSON(http.StatusForbidden, gin.H{"error": "viewers cannot delete files"})
		return
	}

	fileIDs, err := pg.GetFileDescendantIDs(c.Request.Context(), h.db, fileID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not resolve descendants"})
		return
	}

	docRepo := mongoRepo.NewDocumentRepository(h.mongoDB)
	snapRepo := mongoRepo.NewSnapshotRepository(h.mongoDB)
	for _, id := range fileIDs {
		// delete Yjs document for this file
		_ = docRepo.DeleteDocument(c.Request.Context(), id)
		// delete all text snapshots for this file so we don't leave orphaned data
		_ = snapRepo.DeleteSnapshotsByFile(c.Request.Context(), id)
	}

	if err := pg.PermanentDeleteFile(c.Request.Context(), h.db, fileID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not permanently delete"})
		return
	}

	// evict from dedup cache when file is deleted
	lastTextCacheMu.Lock()
	delete(lastTextCache, fileID)
	lastTextCacheMu.Unlock()

	h.publishFiles(c.Request.Context(), roomID)
	c.JSON(http.StatusOK, gin.H{"message": "permanently deleted"})
}

func (h *Handler) SaveState(c *gin.Context) {
	roomID := c.Param("roomID")
	fileID := c.Param("fileID")
	userID := c.GetString("userID")

	role, err := pg.GetMemberRole(c.Request.Context(), h.db, roomID, userID)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member of this room"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	if role == "viewer" {
		c.JSON(http.StatusForbidden, gin.H{"error": "viewers cannot save"})
		return
	}

	existing, err := pg.GetFileByID(c.Request.Context(), h.db, fileID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}
	if existing.IsFolder {
		c.JSON(http.StatusBadRequest, gin.H{"error": "folders do not have state"})
		return
	}

	state, err := io.ReadAll(c.Request.Body)
	if err != nil || len(state) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "empty state"})
		return
	}

	// decode incoming Yjs blob to plain text and compare with last saved text
	// if identical — content hasn't changed, skip MongoDB, Kafka, and snapshot entirely
	incomingText := yjsBlobToText(state)

	lastTextCacheMu.Lock()
	lastText, exists := lastTextCache[fileID]
	if exists && lastText == incomingText {
		lastTextCacheMu.Unlock()
		logger.Log.Info("skipping save — content unchanged", zap.String("file_id", fileID))
		c.JSON(http.StatusOK, gin.H{"message": "saved"})
		return
	}
	lastTextCache[fileID] = incomingText
	lastTextCacheMu.Unlock()

	// content changed — save Yjs binary to MongoDB, get back the new version number
	docRepo := mongoRepo.NewDocumentRepository(h.mongoDB)
	version, err := docRepo.SaveDocument(c.Request.Context(), fileID, state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save state"})
		return
	}

	// capture these for use inside goroutines — avoid closing over loop variables
	capturedText := incomingText
	capturedVersion := version

	// publish Yjs blob + metadata to Kafka async
	// using context.Background() + timeout — never the request context which
	// cancels the moment we return the HTTP response
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		logger.Log.Info("publishing edit event to kafka",
			zap.String("file_id", fileID),
			zap.Int("version", capturedVersion),
		)
		if err := h.kafka.PublishEditEvent(ctx, kafka.EditEvent{
			RoomID:  roomID,
			FileID:  fileID,
			UserID:  userID,
			YjsBlob: state,
			Version: capturedVersion,
			SavedAt: time.Now().UTC(),
		}); err != nil {
			logger.Log.Error("failed to publish edit event", zap.Error(err))
			 // publish to dead letter so we have a record of what failed and why
    		// we already have `state` (the raw Yjs blob) in scope — that's the payload
			_ = h.kafka.PublishDeadLetter(ctx, "edit-events", state, err.Error())
		} else {
			logger.Log.Info("edit event published successfully", zap.String("file_id", fileID))
		}
	}()

	// save plain text snapshot to MongoDB async — text is already decoded above
	// so this goroutine does zero decoding work, just one MongoDB write
	// GetHistoryAtVersion checks this collection first before falling back to Kafka replay
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		snapRepo := mongoRepo.NewSnapshotRepository(h.mongoDB)
		if err := snapRepo.SaveSnapshot(ctx, fileID, roomID, capturedVersion, capturedText); err != nil {
			logger.Log.Error("failed to save snapshot",
				zap.String("file_id", fileID),
				zap.Int("version", capturedVersion),
				zap.Error(err),
			)
		} else {
			logger.Log.Info("snapshot saved",
				zap.String("file_id", fileID),
				zap.Int("version", capturedVersion),
			)
		}
	}()

	c.JSON(http.StatusOK, gin.H{"message": "saved"})
}

func (h *Handler) LoadState(c *gin.Context) {
	roomID := c.Param("roomID")
	fileID := c.Param("fileID")
	userID := c.GetString("userID")
	_, err := pg.GetMemberRole(c.Request.Context(), h.db, roomID, userID)
	if err == pgx.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"error": "not a member of this room"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	existing, err := pg.GetFileByID(c.Request.Context(), h.db, fileID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}
	if existing.IsFolder {
		c.JSON(http.StatusBadRequest, gin.H{"error": "folders do not have state"})
		return
	}
	docRepo := mongoRepo.NewDocumentRepository(h.mongoDB)
	doc, err := docRepo.LoadDocument(c.Request.Context(), fileID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load state"})
		return
	}
	if doc == nil || len(doc.YjsState) == 0 {
		c.Status(http.StatusNoContent)
		return
	}
	c.Data(http.StatusOK, "application/octet-stream", doc.YjsState)
}

func (h *Handler) GetLastExecution(c *gin.Context) {
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

	execRepo := mongoRepo.NewExecutionRepository(h.mongoDB)
	result, err := execRepo.GetLastExecution(c.Request.Context(), fileID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not fetch execution"})
		return
	}
	if result == nil {
		c.Status(http.StatusNoContent)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"output":      result.Output,
		"exit_code":   result.ExitCode,
		"duration_ms": result.DurationMs,
		"truncated":   result.Truncated,
		"executed_at": result.ExecutedAt,
	})
}