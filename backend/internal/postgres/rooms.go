package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Room struct {
	ID        string
	Name      string
	OwnerID   string
	CreatedAt time.Time
}

type RoomWithRole struct {
	Room
	Role string
}

func CreateRoom(ctx context.Context, db *pgxpool.Pool, name, ownerID string) (*Room, string, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	r := &Room{}
	err = tx.QueryRow(ctx, `
		INSERT INTO rooms (name, owner_id)
		VALUES ($1, $2)
		RETURNING id, name, owner_id, created_at
	`, name, ownerID).Scan(&r.ID, &r.Name, &r.OwnerID, &r.CreatedAt)
	if err != nil {
		return nil, "", fmt.Errorf("insert room: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO room_members (room_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, r.ID, ownerID)
	if err != nil {
		return nil, "", fmt.Errorf("insert owner member: %w", err)
	}

	var fileID string
	err = tx.QueryRow(ctx, `
		INSERT INTO files (room_id, name, language, parent_id, is_folder, created_by)
		VALUES ($1, 'main.py', 'python', NULL, FALSE, $2)
		RETURNING id
	`, r.ID, ownerID).Scan(&fileID)
	if err != nil {
		return nil, "", fmt.Errorf("insert default file: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, "", fmt.Errorf("commit: %w", err)
	}
	return r, fileID, nil
}

func ListRoomsForUser(ctx context.Context, db *pgxpool.Pool, userID string) ([]RoomWithRole, error) {
	rows, err := db.Query(ctx, `
		SELECT r.id, r.name, r.owner_id, r.created_at, rm.role
		FROM rooms r
		JOIN room_members rm ON rm.room_id = r.id
		WHERE rm.user_id = $1
		ORDER BY r.created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list rooms: %w", err)
	}
	defer rows.Close()

	var rooms []RoomWithRole
	for rows.Next() {
		var rr RoomWithRole
		if err := rows.Scan(&rr.ID, &rr.Name, &rr.OwnerID, &rr.CreatedAt, &rr.Role); err != nil {
			return nil, fmt.Errorf("scan room: %w", err)
		}
		rooms = append(rooms, rr)
	}
	return rooms, nil
}

func GetMemberRole(ctx context.Context, db *pgxpool.Pool, roomID, userID string) (string, error) {
	var role string
	err := db.QueryRow(ctx, `
		SELECT role FROM room_members
		WHERE room_id = $1 AND user_id = $2
	`, roomID, userID).Scan(&role)
	if err != nil {
		return "", err
	}
	return role, nil
}

func AddMember(ctx context.Context, db *pgxpool.Pool, roomID, userID, role string) error {
	_, err := db.Exec(ctx, `
		INSERT INTO room_members (room_id, user_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (room_id, user_id) DO UPDATE SET role = EXCLUDED.role
	`, roomID, userID, role)
	if err != nil {
		return fmt.Errorf("add member: %w", err)
	}
	return nil
}

func DeleteRoom(ctx context.Context, db *pgxpool.Pool, roomID string) error {
	_, err := db.Exec(ctx, `DELETE FROM rooms WHERE id = $1`, roomID)
	if err != nil {
		return fmt.Errorf("delete room: %w", err)
	}
	return nil
}
