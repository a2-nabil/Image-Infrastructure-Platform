package worker

import (
	"context"
	"log/slog"
	"time"

	"image-infrastructure-platform/services/image-api/internal/metrics"
	"image-infrastructure-platform/services/image-api/internal/service"
)

const (
	defaultGuestSweepInterval = 15 * time.Minute
	defaultGuestSweepBatch    = 50
)

// GuestExpirySweeper periodically deletes expired guest media from S3 and Postgres.
type GuestExpirySweeper struct {
	images   *service.ImageService
	logger   *slog.Logger
	interval time.Duration
	batch    int
}

func NewGuestExpirySweeper(images *service.ImageService, logger *slog.Logger, interval time.Duration, batch int) *GuestExpirySweeper {
	if interval <= 0 {
		interval = defaultGuestSweepInterval
	}
	if batch <= 0 {
		batch = defaultGuestSweepBatch
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &GuestExpirySweeper{
		images:   images,
		logger:   logger,
		interval: interval,
		batch:    batch,
	}
}

// Start runs an immediate sweep, then repeats on interval until ctx is cancelled.
func (s *GuestExpirySweeper) Start(ctx context.Context) {
	go s.loop(ctx)
	s.logger.Info("guest expiry sweeper started",
		"interval", s.interval.String(),
		"batch", s.batch,
	)
}

func (s *GuestExpirySweeper) loop(ctx context.Context) {
	s.runOnce(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("guest expiry sweeper stopping")
			return
		case <-ticker.C:
			s.runOnce(ctx)
		}
	}
}

func (s *GuestExpirySweeper) runOnce(ctx context.Context) {
	if s == nil || s.images == nil {
		return
	}
	start := time.Now()
	purged, err := s.images.PurgeExpiredGuestImages(ctx, s.batch, s.logger)
	if err != nil {
		s.logger.Error("guest expiry sweep failed", "error", err)
		return
	}
	if purged > 0 {
		metrics.Default.AddGuestPurges(purged)
		s.logger.Info("guest expiry sweep completed",
			"purged", purged,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}
}
