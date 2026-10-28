package worker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"image-infrastructure-platform/services/image-api/internal/metrics"
	"image-infrastructure-platform/services/image-api/internal/queue"
	"image-infrastructure-platform/services/image-api/internal/service"
)

// VariantWorker consumes Redis variant jobs and processes them asynchronously.
type VariantWorker struct {
	queue   *queue.VariantQueue
	images  *service.ImageService
	logger  *slog.Logger
	workers int
}

func NewVariantWorker(q *queue.VariantQueue, images *service.ImageService, logger *slog.Logger, workers int) *VariantWorker {
	if workers < 1 {
		workers = 2
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &VariantWorker{queue: q, images: images, logger: logger, workers: workers}
}

// Start launches background consumers until ctx is cancelled.
func (w *VariantWorker) Start(ctx context.Context) {
	for i := 0; i < w.workers; i++ {
		id := i + 1
		go w.loop(ctx, id)
	}
	w.logger.Info("variant workers started", "count", w.workers)
}

func (w *VariantWorker) loop(ctx context.Context, workerID int) {
	for {
		select {
		case <-ctx.Done():
			w.logger.Info("variant worker stopping", "worker", workerID)
			return
		default:
		}

		job, err := w.queue.Dequeue(ctx, 2*time.Second)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, redis.Nil) {
				continue
			}
			w.logger.Error("dequeue failed", "worker", workerID, "error", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if job == nil {
			continue
		}

		start := time.Now()
		if err := w.process(ctx, job); err != nil {
			metrics.Default.IncVariantJob(false)
			w.logger.Error("variant job failed",
				"worker", workerID,
				"image_id", job.ImageID,
				"error", err,
			)
			_ = w.images.MarkFailed(ctx, job.ImageID)
			continue
		}
		metrics.Default.IncVariantJob(true)
		w.logger.Info("variant job completed",
			"worker", workerID,
			"image_id", job.ImageID,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}
}

func (w *VariantWorker) process(ctx context.Context, job *queue.VariantJob) error {
	body, _, err := w.images.OpenRawObject(ctx, job.StoragePath)
	if err != nil {
		return err
	}
	defer body.Close()

	payload, err := io.ReadAll(body)
	if err != nil {
		return err
	}

	var userID *string
	if job.UserID != "" {
		uid := job.UserID
		userID = &uid
	}

	variants, err := w.images.GenerateVariants(ctx, job.ImageID, userID, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	if err := w.images.PersistVariants(ctx, variants); err != nil {
		return err
	}
	return w.images.MarkReady(ctx, job.ImageID)
}
