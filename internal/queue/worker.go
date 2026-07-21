package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// TaskPayload represents a task message for the worker.
type TaskPayload struct {
	TaskID        string   `json:"task_id"`
	ProjectID     string   `json:"project_id"`
	UserID        string   `json:"user_id"`
	Prompt        string   `json:"prompt"`
	MaxTokens     int      `json:"max_tokens"`
	MaxIterations int      `json:"max_iterations"`
	Tags          []string `json:"tags,omitempty"`
	Priority      int      `json:"priority"`
}

// TaskHandler is the function signature for processing a task.
type TaskHandler func(ctx context.Context, payload TaskPayload) error

// TaskWorker consumes tasks from NATS JetStream with retry and dead-letter support.
type TaskWorker struct {
	nats      *NATS
	consumer  jetstream.Consumer
	stream    string
	subject   string
	handler   TaskHandler
	maxRetries int
}

// WorkerConfig configures the task worker.
type WorkerConfig struct {
	Stream     string
	Subject    string
	MaxRetries int
	AckWait    time.Duration
	MaxDeliver int
}

// DefaultWorkerConfig returns sensible defaults.
func DefaultWorkerConfig() WorkerConfig {
	return WorkerConfig{
		Stream:     "vigilagent",
		Subject:    "tasks.execute",
		MaxRetries: 3,
		AckWait:    60 * time.Second,
		MaxDeliver: 4,
	}
}

// NewTaskWorker creates a new task worker.
func NewTaskWorker(natsConn *NATS, cfg WorkerConfig, handler TaskHandler) (*TaskWorker, error) {
	ctx := context.Background()

	// Ensure stream exists
	_, err := natsConn.JS.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      cfg.Stream,
		Subjects:  []string{cfg.Stream + ".>"},
		Retention: jetstream.WorkQueuePolicy,
		MaxMsgs:   -1,
		Storage:   jetstream.FileStorage,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create stream: %w", err)
	}

	// Create consumer
	consumer, err := natsConn.JS.CreateOrUpdateConsumer(ctx, cfg.Stream, jetstream.ConsumerConfig{
		Durable:   "task-worker",
		AckPolicy: jetstream.AckExplicitPolicy,
		AckWait:   cfg.AckWait,
		MaxDeliver: cfg.MaxDeliver,
		FilterSubject: cfg.Stream + "." + cfg.Subject,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create consumer: %w", err)
	}

	return &TaskWorker{
		nats:       natsConn,
		consumer:   consumer,
		stream:     cfg.Stream,
		subject:    cfg.Subject,
		handler:    handler,
		maxRetries: cfg.MaxRetries,
	}, nil
}

// PublishTask publishes a task to the JetStream queue for processing.
func (w *TaskWorker) PublishTask(ctx context.Context, payload TaskPayload) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal task payload: %w", err)
	}

	_, err = w.nats.JS.Publish(ctx, w.stream+"."+w.subject, data)
	if err != nil {
		return fmt.Errorf("failed to publish task: %w", err)
	}

	slog.Info("task published to queue", "task_id", payload.TaskID, "subject", w.subject)
	return nil
}

// Start begins consuming tasks from the queue.
func (w *TaskWorker) Start(ctx context.Context) error {
	slog.Info("task worker started", "stream", w.stream, "subject", w.subject)

	for {
		select {
		case <-ctx.Done():
			slog.Info("task worker shutting down")
			return nil
		default:
		}

		batch, err := w.consumer.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			slog.Warn("failed to fetch messages", "error", err)
			time.Sleep(1 * time.Second)
			continue
		}

		for msg := range batch.Messages() {
			w.processMessage(ctx, msg)
		}
	}
}

func (w *TaskWorker) processMessage(ctx context.Context, msg jetstream.Msg) {
	var payload TaskPayload
	if err := json.Unmarshal(msg.Data(), &payload); err != nil {
		slog.Error("failed to unmarshal task payload", "error", err)
		_ = msg.Nak()
		return
	}

	slog.Info("processing task", "task_id", payload.TaskID, "retries", msg.Headers().Get("Nats-Retry"))

	if err := w.handler(ctx, payload); err != nil {
		slog.Error("task handler failed", "task_id", payload.TaskID, "error", err)
		_ = msg.NakWithDelay(5 * time.Second)
		return
	}

	_ = msg.Ack()
	slog.Info("task completed", "task_id", payload.TaskID)
}

// Stop gracefully stops the worker.
func (w *TaskWorker) Stop() {
	slog.Info("task worker stopped")
}
