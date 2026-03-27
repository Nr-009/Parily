package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Member struct {
	UserID   string
	Email    string
	Name     string
	Role     string
	JoinedAt time.Time
}

// ListMembers returns all members of a room with their user info.
func ListMembers(ctx context.Context, db *pgxpool.Pool, roomID string) ([]Member, error) {
	rows, err := db.Query(ctx, `
		SELECT u.id, u.email, u.name, rm.role, rm.joined_at
		FROM room_members rm
		JOIN users u ON u.id = rm.user_id
		WHERE rm.room_id = $1
		ORDER BY rm.joined_at ASC
	`, roomID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	defer rows.Close()

	var members []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Email, &m.Name, &m.Role, &m.JoinedAt); err != nil {
			return nil, fmt.Errorf("scan member: %w", err)
		}
		members = append(members, m)
	}
	return members, nil
}

// GetRoomMemberIDs returns just the user IDs of all members in a room.
// Used to fan out notifications to all members efficiently.
func GetRoomMemberIDs(ctx context.Context, db *pgxpool.Pool, roomID string) ([]string, error) {
	rows, err := db.Query(ctx, `
		SELECT user_id FROM room_members WHERE room_id = $1
	`, roomID)
	if err != nil {
		return nil, fmt.Errorf("get room member ids: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan member id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// RemoveMember deletes a user from room_members.
func RemoveMember(ctx context.Context, db *pgxpool.Pool, roomID, userID string) error {
	_, err := db.Exec(ctx, `
		DELETE FROM room_members
		WHERE room_id = $1 AND user_id = $2
	`, roomID, userID)
	if err != nil {
		return fmt.Errorf("remove member: %w", err)
	}
	return nil
}

// UpdateMemberRole changes the role of a user in a room.
func UpdateMemberRole(ctx context.Context, db *pgxpool.Pool, roomID, userID, role string) error {
	_, err := db.Exec(ctx, `
		UPDATE room_members SET role = $1
		WHERE room_id = $2 AND user_id = $3
	`, role, roomID, userID)
	if err != nil {
		return fmt.Errorf("update member role: %w", err)
	}
	return nil
}
