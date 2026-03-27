package rooms

import (
	"context"
	"encoding/json"
	"path/filepath"
	"time"
	"github.com/gin-gonic/gin"

	pg "parily.dev/app/internal/postgres"
)

func (h *Handler) publishPermission(roomID string, event map[string]string) {
	data, _ := json.Marshal(event)
	h.rdb.Publish("room:"+roomID+":room", data)
}

// publishFiles fetches all files for the room and broadcasts to all clients
// via the room channel so every client rebuilds their file tree in sync.
func (h *Handler) publishFiles(ctx context.Context, roomID string) {
	files, err := pg.GetFilesForRoom(ctx, h.db, roomID)
	if err != nil {
		return
	}
	result := make([]gin.H, 0, len(files))
	for _, f := range files {
		f := f
		result = append(result, fileResponse(&f))
	}
	data, err := json.Marshal(map[string]any{
		"type":  "files_updated",
		"files": result,
	})
	if err != nil {
		return
	}
	h.rdb.Publish("room:"+roomID+":room", data)
}

// publishNotification sends a notification to a specific user via NotifyHub.
func (h *Handler) publishNotification(userID string, event map[string]any) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	h.notifyHub.Publish(userID, data)
}

// publishNotificationToMembers sends a notification to all members of a room.
func (h *Handler) publishNotificationToMembers(ctx context.Context, roomID string, event map[string]any) {
	memberIDs, err := pg.GetRoomMemberIDs(ctx, h.db, roomID)
	if err != nil {
		return
	}
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	for _, userID := range memberIDs {
		h.notifyHub.Publish(userID, data)
	}
}

func langFromExtension(name string) string {
	switch filepath.Ext(name) {
	case ".py":
		return "python"
	case ".js":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".go":
		return "go"
	case ".java":
		return "java"
	case ".c":
		return "c"
	case ".cpp", ".cc":
		return "cpp"
	case ".rs":
		return "rust"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".html":
		return "html"
	case ".css":
		return "css"
	case ".json":
		return "json"
	case ".md":
		return "markdown"
	case ".yaml", ".yml":
		return "yaml"
	case ".sh":
		return "shell"
	default:
		return "plaintext"
	}
}

func fileResponse(f *pg.File) gin.H {
	return gin.H{
		"id":         f.ID,
		"room_id":    f.RoomID,
		"name":       f.Name,
		"language":   f.Language,
		"parent_id":  f.ParentID,
		"is_folder":  f.IsFolder,
		"is_active":  f.IsActive,
		"created_by": f.CreatedBy,
		"created_at": f.CreatedAt.Format(time.RFC3339),
		"updated_at": f.UpdatedAtStr(),
	}
}
