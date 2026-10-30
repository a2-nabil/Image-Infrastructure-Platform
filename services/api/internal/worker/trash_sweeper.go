package worker

import (
	"context"
	"log/slog"
	"time"

	"image-infrastructure-platform/services/api/internal/service"
)

const (
	defaultTrashSweepInterval = 1 * time.Hour
	defaultTrashSweepBatch    = 50
	defaultTrashRetention     = 30 * 24 * time.Hour
)

// TrashSweeper permanently deletes trashed images after retention (Google Photos style).
type TrashSweeper struct {
	images    *service.ImageService
	logger    *slog.Logger
	interval  time.Duration
	batch     int
	retention time.Duration
}

func NewTrashSweeper(images *service.ImageService, logger *slog.Logger, interval, retention time.Duration, batch int) *TrashSweeper {
	if interval <= 0 {
		interval = defaultTrashSweepInterval
	}
	if retention <= 0 {
		retention = defaultTrashRetention
	}
	if batch <= 0 {
		batch = defaultTrashSweepBatch
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &TrashSweeper{
		images:    images,
		logger:    logger,
		interval:  interval,
		batch:     batch,
		retention: retention,
	}
}

func (s *TrashSweeper) Start(ctx context.Context) {
	go s.loop(ctx)
	s.logger.Info("trash sweeper started",
		"interval", s.interval.String(),
		"retention", s.retention.String(),
		"batch", s.batch,
	)
}

func (s *TrashSweeper) loop(ctx context.Context) {
	s.runOnce(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.logger.Info("trash sweeper stopping")
			return
		case <-ticker.C:
			s.runOnce(ctx)
		}
	}
}

func (s *TrashSweeper) runOnce(ctx context.Context) {
	if s == nil || s.images == nil {
		return
	}
	start := time.Now()
	purged, err := s.images.PurgeExpiredTrash(ctx, s.retention, s.batch, s.logger)
	if err != nil {
		s.logger.Error("trash sweep failed", "error", err)
		return
	}
	if purged > 0 {
		s.logger.Info("trash sweep completed",
			"purged", purged,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}
}
