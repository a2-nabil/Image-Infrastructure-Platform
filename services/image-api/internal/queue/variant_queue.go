package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const VariantQueueKey = "image-api:queue:variant-jobs"

// VariantJob is an event published after raw upload for async processing.
type VariantJob struct {
	ImageID      string   `json:"image_id"`
	StoragePath  string   `json:"storage_path"`
	UserID       string   `json:"user_id,omitempty"`
	MediaKind    string   `json:"media_kind,omitempty"`
	Presets      []string `json:"presets,omitempty"`
	Quality      int      `json:"quality,omitempty"`
	CustomWidth  int      `json:"custom_width,omitempty"`
	CustomHeight int      `json:"custom_height,omitempty"`
	EnqueuedAt   string   `json:"enqueued_at"`
}

// VariantQueue is a Redis list-backed job queue (event-driven worker pattern).
type VariantQueue struct {
	rdb *redis.Client
	key string
}

func NewVariantQueue(rdb *redis.Client) *VariantQueue {
	return &VariantQueue{rdb: rdb, key: VariantQueueKey}
}

func (q *VariantQueue) Enqueue(ctx context.Context, job VariantJob) error {
	if q == nil || q.rdb == nil {
		return fmt.Errorf("variant queue is not initialized")
	}
	if job.ImageID == "" || job.StoragePath == "" {
		return fmt.Errorf("image_id and storage_path are required")
	}
	if job.EnqueuedAt == "" {
		job.EnqueuedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	payload, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal job: %w", err)
	}
	if err := q.rdb.LPush(ctx, q.key, payload).Err(); err != nil {
		return fmt.Errorf("enqueue variant job: %w", err)
	}
	return nil
}

// Dequeue blocks until a job is available or ctx is cancelled.
func (q *VariantQueue) Dequeue(ctx context.Context, block time.Duration) (*VariantJob, error) {
	if q == nil || q.rdb == nil {
		return nil, fmt.Errorf("variant queue is not initialized")
	}
	res, err := q.rdb.BRPop(ctx, block, q.key).Result()
	if err != nil {
		if err == redis.Nil || err == context.Canceled || err == context.DeadlineExceeded {
			return nil, err
		}
		return nil, fmt.Errorf("dequeue variant job: %w", err)
	}
	if len(res) < 2 {
		return nil, fmt.Errorf("unexpected brpop payload")
	}
	var job VariantJob
	if err := json.Unmarshal([]byte(res[1]), &job); err != nil {
		return nil, fmt.Errorf("unmarshal job: %w", err)
	}
	return &job, nil
}
