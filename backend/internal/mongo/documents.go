package mongo

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type YjsDocument struct {
	FileID    string    `bson:"file_id"`
	RoomID    string    `bson:"room_id"`
	YjsState  []byte    `bson:"yjs_state"`
	Version   int       `bson:"version"`
	UpdatedAt time.Time `bson:"updated_at"`
}

type DocumentRepository struct {
	col *mongo.Collection
}

func NewDocumentRepository(db *mongo.Database) *DocumentRepository {
	return &DocumentRepository{col: db.Collection("documents")}
}

// CreateDocument inserts an empty Yjs document for a new file.
func (r *DocumentRepository) CreateDocument(ctx context.Context, fileID, roomID string) error {
	doc := YjsDocument{
		FileID:    fileID,
		RoomID:    roomID,
		YjsState:  []byte{},
		Version:   0,
		UpdatedAt: time.Now(),
	}
	_, err := r.col.InsertOne(ctx, doc)
	if err != nil {
		return fmt.Errorf("create document: %w", err)
	}
	return nil
}

// LoadDocument fetches the Yjs state for a file.
// Returns nil state if no document exists yet.
func (r *DocumentRepository) LoadDocument(ctx context.Context, fileID string) (*YjsDocument, error) {
	var doc YjsDocument
	err := r.col.FindOne(ctx, bson.M{"file_id": fileID}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load document: %w", err)
	}
	return &doc, nil
}

// SaveDocument upserts the Yjs state for a file and increments the version.
func (r *DocumentRepository) SaveDocument(ctx context.Context, fileID string, state []byte) (int, error) {
    filter := bson.M{"file_id": fileID}
    update := bson.M{
        "$set": bson.M{"yjs_state": state, "updated_at": time.Now()},
        "$inc": bson.M{"version": 1},
    }
    // ReturnDocument After means we get the document state AFTER the increment
    // so version reflects the new value not the old one
    opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)
    var updated YjsDocument
    err := r.col.FindOneAndUpdate(ctx, filter, update, opts).Decode(&updated)
    if err != nil {
        return 0, fmt.Errorf("save document: %w", err)
    }
    return updated.Version, nil
}
// DeleteDocument deletes the Yjs document for a single file.
// Used when permanently deleting a file.
func (r *DocumentRepository) DeleteDocument(ctx context.Context, fileID string) error {
	_, err := r.col.DeleteOne(ctx, bson.M{"file_id": fileID})
	if err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	return nil
}

// DeleteDocumentsByRoom deletes all Yjs documents for a room from MongoDB.
func (r *DocumentRepository) DeleteDocumentsByRoom(ctx context.Context, roomID string) error {
	_, err := r.col.DeleteMany(ctx, bson.M{"room_id": roomID})
	if err != nil {
		return fmt.Errorf("delete documents by room: %w", err)
	}
	return nil
}
