package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type User struct {
	ID           string
	Email        string
	Name         string
	PasswordHash string
	CreatedAt    time.Time
}

// CreateUser inserts a new user and returns the full record.
func CreateUser(ctx context.Context, db *pgxpool.Pool, email, name, passwordHash string) (*User, error) {
	u := &User{}
	err := db.QueryRow(ctx, `
		INSERT INTO users (email, name, password_hash)
		VALUES ($1, $2, $3)
		RETURNING id, email, name, password_hash, created_at
	`, email, name, passwordHash).
		Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

// GetUserByEmail looks up a user by email.
// Returns pgx.ErrNoRows if the email doesn't exist — callers must handle that.
func GetUserByEmail(ctx context.Context, db *pgxpool.Pool, email string) (*User, error) {
	u := &User{}
	err := db.QueryRow(ctx, `
		SELECT id, email, name, password_hash, created_at
		FROM users
		WHERE email = $1
	`, email).Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func GetUserByID(ctx context.Context, db *pgxpool.Pool, id string) (*User, error) {
	u := &User{}
	err := db.QueryRow(ctx, `
        SELECT id, email, name, password_hash, created_at
        FROM users WHERE id = $1
    `, id).Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}
