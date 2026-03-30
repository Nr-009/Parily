package kafka

import (
	"context"
	"encoding/json"
	"time"

	"github.com/segmentio/kafka-go"
)

// EditEvent is published to the edit-events topic on every Yjs snapshot save.
// The YjsBlob is the exact binary state saved to MongoDB — same bytes, just a copy.
// Version comes from MongoDB so Kafka offset maps to a known document version.
type EditEvent struct {
	RoomID  string    `json:"room_id"`
	FileID  string    `json:"file_id"`
	UserID  string    `json:"user_id"`
	YjsBlob []byte    `json:"yjs_blob"`
	Version int       `json:"version"`
	SavedAt time.Time `json:"saved_at"`
}

// ExecutionEvent is published to the execution-events topic after every code run.
// Pure audit log — never replayed, just queried for history display.
type ExecutionEvent struct {
	ExecutionID string    `json:"execution_id"`
	RoomID      string    `json:"room_id"`
	FileID      string    `json:"file_id"`
	Language    string    `json:"language"`
	Output      string    `json:"output"`
	ExitCode    int       `json:"exit_code"`
	DurationMs  int64     `json:"duration_ms"`
	ExecutedAt  time.Time `json:"executed_at"`
}

// Producer holds one writer per topic.
// kafka.Writer maintains a persistent connection to the broker and handles
// retries, batching, and leader discovery internally.
type Producer struct {
	editWriter      *kafka.Writer
	executionWriter *kafka.Writer
}

// NewProducer creates a Producer connected to the given broker.
// Both writers are configured with:
//   - Async=false so WriteMessages blocks until the broker acknowledges.
//     We run it in a goroutine from the caller so blocking here is fine and
//     gives us reliable error detection.
//   - RequiredAcks=RequireOne — broker leader must acknowledge the write.
//     Enough durability for a portfolio project; RequireAll would wait for
//     all replicas (we only have one replica anyway).
func NewProducer(broker string) *Producer {
	return &Producer{
		editWriter: &kafka.Writer{
			Addr:         kafka.TCP(broker),
			Topic:        "edit-events",
			Balancer:     &kafka.Hash{}, // routes by message Key — fileID goes to same partition
			RequiredAcks: kafka.RequireOne,
			Async:        false,
		},
		executionWriter: &kafka.Writer{
			Addr:         kafka.TCP(broker),
			Topic:        "execution-events",
			Balancer:     &kafka.Hash{},
			RequiredAcks: kafka.RequireOne,
			Async:        false,
		},
	}
}

// PublishEditEvent serializes the event and writes it to edit-events.
// Key is the fileID — guarantees all events for the same file land in the
// same partition in order, which is required for correct version replay.
//
// IMPORTANT: callers must run this in a goroutine with context.Background(),
// never with the HTTP request context (which cancels when the response is sent).
func (p *Producer) PublishEditEvent(ctx context.Context, event EditEvent) error {
	value, err := json.Marshal(event)
	if err != nil {
		return err
	}

	return p.editWriter.WriteMessages(ctx, kafka.Message{
		Key:   []byte(event.FileID),
		Value: value,
	})
}

// PublishExecutionEvent serializes the event and writes it to execution-events.
// Key is the fileID for consistent partition routing.
func (p *Producer) PublishExecutionEvent(ctx context.Context, event ExecutionEvent) error {
	value, err := json.Marshal(event)
	if err != nil {
		return err
	}

	return p.executionWriter.WriteMessages(ctx, kafka.Message{
		Key:   []byte(event.FileID),
		Value: value,
	})
}

// Close shuts down both writers cleanly.
// Call this in main() via defer after creating the producer.
func (p *Producer) Close() error {
	if err := p.editWriter.Close(); err != nil {
		return err
	}
	return p.executionWriter.Close()
}