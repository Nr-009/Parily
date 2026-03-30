package mongo

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// FileSnapshot stores the plain text of a file at a specific version.
// Mirrors the YjsDocument pattern — file_id, room_id, version, and a timestamp.
// Written every time SaveState detects a content change — text is already
// decoded from the Yjs blob during the dedup check so this costs nothing extra.
type FileSnapshot struct {
	FileID  string    `bson:"file_id"`
	RoomID  string    `bson:"room_id"`
	Version int       `bson:"version"`
	Text    string    `bson:"text"`
	SavedAt time.Time `bson:"saved_at"`
}

type SnapshotRepository struct {
	col *mongo.Collection
}

func NewSnapshotRepository(db *mongo.Database) *SnapshotRepository {
	return &SnapshotRepository{col: db.Collection("file_snapshots")}
}

// SaveSnapshot writes the plain text for a specific (file_id, version) pair.
// Upsert on (file_id, version) — if the same version arrives twice we just
// overwrite with identical data, no duplicate error.
func (r *SnapshotRepository) SaveSnapshot(ctx context.Context, fileID, roomID string, version int, text string) error {
	filter := bson.M{"file_id": fileID, "version": version}
	update := bson.M{
		"$set": bson.M{
			"room_id":  roomID,
			"text":     text,
			"saved_at": time.Now().UTC(),
		},
	}
	opts := options.Update().SetUpsert(true)
	_, err := r.col.UpdateOne(ctx, filter, update, opts)
	if err != nil {
		return fmt.Errorf("save snapshot: %w", err)
	}
	return nil
}

// GetSnapshot retrieves the plain text for a specific (file_id, version) pair.
// Returns nil if no snapshot exists for that version — caller falls back to Kafka replay.
func (r *SnapshotRepository) GetSnapshot(ctx context.Context, fileID string, version int) (*FileSnapshot, error) {
	var snap FileSnapshot
	err := r.col.FindOne(ctx, bson.M{"file_id": fileID, "version": version}).Decode(&snap)
	if err == mongo.ErrNoDocuments {
		// cache miss — not an error, caller will replay from Kafka
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get snapshot: %w", err)
	}
	return &snap, nil
}

// DeleteSnapshotsByFile removes all snapshots for a file.
// Called when a file is permanently deleted so we don't leave orphaned data.
func (r *SnapshotRepository) DeleteSnapshotsByFile(ctx context.Context, fileID string) error {
	_, err := r.col.DeleteMany(ctx, bson.M{"file_id": fileID})
	if err != nil {
		return fmt.Errorf("delete snapshots by file: %w", err)
	}
	return nil
}

// DeleteSnapshotsByRoom removes all snapshots for every file in a room.
// Same pattern as DeleteDocumentsByRoom — keyed directly on room_id, no
// need to fetch file IDs from Postgres first.
func (r *SnapshotRepository) DeleteSnapshotsByRoom(ctx context.Context, roomID string) error {
	_, err := r.col.DeleteMany(ctx, bson.M{"room_id": roomID})
	if err != nil {
		return fmt.Errorf("delete snapshots by room: %w", err)
	}
	return nil
}