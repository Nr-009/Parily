package rooms

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	mongoRepo "parily.dev/app/internal/mongo"
	pg "parily.dev/app/internal/postgres"
)

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

// PermanentDeleteFile hard deletes a file or folder and all its descendants.
// Deletes MongoDB documents for all file descendants first, then hard DELETEs from DB.
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

	// get all file (non-folder) descendant IDs to clean up MongoDB
	fileIDs, err := pg.GetFileDescendantIDs(c.Request.Context(), h.db, fileID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not resolve descendants"})
		return
	}

	// delete MongoDB documents for all file descendants
	docRepo := mongoRepo.NewDocumentRepository(h.mongoDB)
	for _, id := range fileIDs {
		// best effort — don't fail if doc doesn't exist
		_ = docRepo.DeleteDocument(c.Request.Context(), id)
	}

	// hard delete from DB (recursive CTE deletes entire subtree)
	if err := pg.PermanentDeleteFile(c.Request.Context(), h.db, fileID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not permanently delete"})
		return
	}

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
	docRepo := mongoRepo.NewDocumentRepository(h.mongoDB)
	if err := docRepo.SaveDocument(c.Request.Context(), fileID, state); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save state"})
		return
	}
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
