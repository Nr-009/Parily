package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type File struct {
	ID        string
	RoomID    string
	Name      string
	Language  string
	ParentID  *string
	IsFolder  bool
	IsActive  bool
	CreatedBy string
	CreatedAt time.Time
	UpdatedAt *time.Time
}

func (f *File) UpdatedAtStr() string {
	if f.UpdatedAt == nil {
		return ""
	}
	return f.UpdatedAt.Format(time.RFC3339)
}

func GetFilesForRoom(ctx context.Context, db *pgxpool.Pool, roomID string) ([]File, error) {
	rows, err := db.Query(ctx, `
		SELECT id, room_id, name, language, parent_id, is_folder, is_active,
		       created_by, created_at, updated_at
		FROM files
		WHERE room_id = $1
		ORDER BY is_folder DESC, name ASC
	`, roomID)
	if err != nil {
		return nil, fmt.Errorf("get files for room: %w", err)
	}
	defer rows.Close()

	var files []File
	for rows.Next() {
		var f File
		if err := rows.Scan(
			&f.ID, &f.RoomID, &f.Name, &f.Language,
			&f.ParentID, &f.IsFolder, &f.IsActive,
			&f.CreatedBy, &f.CreatedAt, &f.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan file: %w", err)
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func GetFileByID(ctx context.Context, db *pgxpool.Pool, fileID string) (*File, error) {
	f := &File{}
	err := db.QueryRow(ctx, `
		SELECT id, room_id, name, language, parent_id, is_folder, is_active,
		       created_by, created_at, updated_at
		FROM files
		WHERE id = $1
	`, fileID).Scan(
		&f.ID, &f.RoomID, &f.Name, &f.Language,
		&f.ParentID, &f.IsFolder, &f.IsActive,
		&f.CreatedBy, &f.CreatedAt, &f.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get file by id: %w", err)
	}
	return f, nil
}

type CreateFileParams struct {
	RoomID    string
	Name      string
	Language  string
	ParentID  *string
	IsFolder  bool
	CreatedBy string
}

func CreateFile(ctx context.Context, db *pgxpool.Pool, p CreateFileParams) (*File, error) {
	f := &File{}
	err := db.QueryRow(ctx, `
		INSERT INTO files (room_id, name, language, parent_id, is_folder, is_active, created_by)
		VALUES ($1, $2, $3, $4, $5, TRUE, $6)
		RETURNING id, room_id, name, language, parent_id, is_folder, is_active,
		          created_by, created_at, updated_at
	`, p.RoomID, p.Name, p.Language, p.ParentID, p.IsFolder, p.CreatedBy).Scan(
		&f.ID, &f.RoomID, &f.Name, &f.Language,
		&f.ParentID, &f.IsFolder, &f.IsActive,
		&f.CreatedBy, &f.CreatedAt, &f.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create file: %w", err)
	}
	return f, nil
}

type UpdateFileParams struct {
	FileID   string
	Name     string
	Language string
	ParentID *string
}

func UpdateFile(ctx context.Context, db *pgxpool.Pool, p UpdateFileParams) (*File, error) {
	f := &File{}
	err := db.QueryRow(ctx, `
		UPDATE files
		SET name       = $2,
		    language   = $3,
		    parent_id  = CASE WHEN $4::uuid IS NULL THEN parent_id ELSE $4 END,
		    updated_at = NOW()
		WHERE id = $1 AND is_folder = FALSE AND is_active = TRUE
		RETURNING id, room_id, name, language, parent_id, is_folder, is_active,
		          created_by, created_at, updated_at
	`, p.FileID, p.Name, p.Language, p.ParentID).Scan(
		&f.ID, &f.RoomID, &f.Name, &f.Language,
		&f.ParentID, &f.IsFolder, &f.IsActive,
		&f.CreatedBy, &f.CreatedAt, &f.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("update file: %w", err)
	}
	return f, nil
}

type UpdateFolderParams struct {
	FolderID string
	Name     string
	ParentID *string
}

func UpdateFolder(ctx context.Context, db *pgxpool.Pool, p UpdateFolderParams) (*File, error) {
	f := &File{}
	err := db.QueryRow(ctx, `
		UPDATE files
		SET name       = CASE WHEN $2 = '' THEN name ELSE $2 END,
		    parent_id  = $3,
		    updated_at = NOW()
		WHERE id = $1 AND is_folder = TRUE AND is_active = TRUE
		RETURNING id, room_id, name, language, parent_id, is_folder, is_active,
		          created_by, created_at, updated_at
	`, p.FolderID, p.Name, p.ParentID).Scan(
		&f.ID, &f.RoomID, &f.Name, &f.Language,
		&f.ParentID, &f.IsFolder, &f.IsActive,
		&f.CreatedBy, &f.CreatedAt, &f.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("update folder: %w", err)
	}
	return f, nil
}

// ToggleFile flips is_active for a single file.
// parent_id is set to null on delete so a restored file lands at root.
// On restore parent_id stays null — user moves it back manually.
func ToggleFile(ctx context.Context, db *pgxpool.Pool, fileID string) (*File, error) {
	f := &File{}
	err := db.QueryRow(ctx, `
		UPDATE files
		SET is_active  = NOT is_active,
		    parent_id  = NULL,
		    updated_at = NOW()
		WHERE id = $1 AND is_folder = FALSE
		RETURNING id, room_id, name, language, parent_id, is_folder, is_active,
		          created_by, created_at, updated_at
	`, fileID).Scan(
		&f.ID, &f.RoomID, &f.Name, &f.Language,
		&f.ParentID, &f.IsFolder, &f.IsActive,
		&f.CreatedBy, &f.CreatedAt, &f.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("toggle file: %w", err)
	}
	return f, nil
}

// ToggleFolder flips is_active for a folder AND all its descendants recursively.
// parent_id is NEVER modified — structure is preserved in both directions.
// This means deleted folders restore with their full original structure intact.
func ToggleFolder(ctx context.Context, db *pgxpool.Pool, folderID string) ([]File, error) {
	rows, err := db.Query(ctx, `
		WITH RECURSIVE subtree AS (
			SELECT id FROM files WHERE id = $1
			UNION ALL
			SELECT f.id FROM files f
			INNER JOIN subtree s ON f.parent_id = s.id
		),
		target_state AS (
			SELECT NOT is_active AS new_state FROM files WHERE id = $1
		)
		UPDATE files
		SET is_active  = (SELECT new_state FROM target_state),
		    updated_at = NOW()
		WHERE id IN (SELECT id FROM subtree)
		RETURNING id, room_id, name, language, parent_id, is_folder, is_active,
		          created_by, created_at, updated_at
	`, folderID)
	if err != nil {
		return nil, fmt.Errorf("toggle folder: %w", err)
	}
	defer rows.Close()

	var files []File
	for rows.Next() {
		var f File
		if err := rows.Scan(
			&f.ID, &f.RoomID, &f.Name, &f.Language,
			&f.ParentID, &f.IsFolder, &f.IsActive,
			&f.CreatedBy, &f.CreatedAt, &f.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan toggled file: %w", err)
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func MoveFolder(ctx context.Context, db *pgxpool.Pool, folderID string, parentID *string) (*File, error) {
	f := &File{}
	err := db.QueryRow(ctx, `
		UPDATE files
		SET parent_id  = $2,
		    updated_at = NOW()
		WHERE id = $1 AND is_folder = TRUE AND is_active = TRUE
		RETURNING id, room_id, name, language, parent_id, is_folder, is_active,
		          created_by, created_at, updated_at
	`, folderID, parentID).Scan(
		&f.ID, &f.RoomID, &f.Name, &f.Language,
		&f.ParentID, &f.IsFolder, &f.IsActive,
		&f.CreatedBy, &f.CreatedAt, &f.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("move folder: %w", err)
	}
	return f, nil
}
