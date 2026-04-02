package mongo

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type ExecutionResult struct {
	ExecutionID string    `bson:"execution_id"`
	RoomID      string    `bson:"room_id"`
	FileID      string    `bson:"file_id"`
	Output      string    `bson:"output"`
	ExitCode    int       `bson:"exit_code"`
	DurationMs  int64     `bson:"duration_ms"`
	Truncated   bool      `bson:"truncated"`
	ExecutedAt  time.Time `bson:"executed_at"`
}

type ExecutionRepository struct {
	col *mongo.Collection
}

func NewExecutionRepository(db *mongo.Database) *ExecutionRepository {
	return &ExecutionRepository{col: db.Collection("executions")}
}

// SaveExecution upserts the latest execution result for a file.
// Only one document per file_id — always the most recent.
func (r *ExecutionRepository) SaveExecution(ctx context.Context, result ExecutionResult) error {
	filter := bson.M{"file_id": result.FileID}
	update := bson.M{"$set": result}
	opts := options.Update().SetUpsert(true)
	_, err := r.col.UpdateOne(ctx, filter, update, opts)
	if err != nil {
		return fmt.Errorf("save execution: %w", err)
	}
	return nil
}

// GetExecutionsForRoom returns the latest execution for each file in the room.
// Used for late-join execution_history.
func (r *ExecutionRepository) GetExecutionsForRoom(ctx context.Context, roomID string) ([]ExecutionResult, error) {
	cursor, err := r.col.Find(ctx, bson.M{"room_id": roomID})
	if err != nil {
		return nil, fmt.Errorf("get executions: %w", err)
	}
	defer cursor.Close(ctx)

	var results []ExecutionResult
	if err := cursor.All(ctx, &results); err != nil {
		return nil, fmt.Errorf("decode executions: %w", err)
	}
	return results, nil
}

func (r *ExecutionRepository) GetLastExecution(ctx context.Context, fileID string) (*ExecutionResult, error) {
	var result ExecutionResult
	err := r.col.FindOne(ctx, bson.M{"file_id": fileID}).Decode(&result)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get last execution: %w", err)
	}
	return &result, nil
}