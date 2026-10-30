package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"image-infrastructure-platform/services/image-api/internal/config"
	"image-infrastructure-platform/services/image-api/internal/database"
	"image-infrastructure-platform/services/image-api/internal/handler"
	"image-infrastructure-platform/services/image-api/internal/logger"
	"image-infrastructure-platform/services/image-api/internal/metrics"
	"image-infrastructure-platform/services/image-api/internal/middleware"
	"image-infrastructure-platform/services/image-api/internal/queue"
	"image-infrastructure-platform/services/image-api/internal/service"
	"image-infrastructure-platform/services/image-api/internal/storage"
	"image-infrastructure-platform/services/image-api/internal/worker"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Printf("failed to load configuration: %v", err)
		os.Exit(1)
	}

	slogLogger := logger.New(cfg.Server.AppEnv, cfg.Logging.Level)
	dbURL := buildDatabaseURL(cfg.Database)

	if cfg.Server.MigrateOnStart {
		migrationsPath := resolveMigrationsPath()
		if err := database.RunMigrations(dbURL, migrationsPath); err != nil {
			slogLogger.Error("database migrations failed", "error", err)
			os.Exit(1)
		}
	}

	db, err := database.Open(dbURL)
	if err != nil {
		slogLogger.Error("database connection failed", "error", err)
		os.Exit(1)
	}

	redisOpts, err := redis.ParseURL(cfg.RedisAddr())
	if err != nil {
		slogLogger.Error("invalid redis url", "error", err)
		_ = db.Close()
		os.Exit(1)
	}
	rdb := redis.NewClient(redisOpts)

	if err := middleware.PingRedis(context.Background(), rdb); err != nil {
		slogLogger.Error("redis connection failed", "error", err)
		_ = rdb.Close()
		_ = db.Close()
		os.Exit(1)
	}

	s3Client, err := storage.NewS3Client(context.Background(), cfg)
	if err != nil {
		slogLogger.Error("s3 client initialization failed", "error", err)
		_ = rdb.Close()
		_ = db.Close()
		os.Exit(1)
	}

	imageService := service.NewImageService(db, s3Client)
	variantQueue := queue.NewVariantQueue(rdb)
	imageService.SetVariantQueue(queue.ServicePublisher{Queue: variantQueue})

	workerCtx, stopWorkers := context.WithCancel(context.Background())
	defer stopWorkers()
	variantWorker := worker.NewVariantWorker(variantQueue, imageService, slogLogger, 2)
	variantWorker.Start(workerCtx)
	guestSweeper := worker.NewGuestExpirySweeper(imageService, slogLogger, 15*time.Minute, 50)
	guestSweeper.Start(workerCtx)
	trashSweeper := worker.NewTrashSweeper(imageService, slogLogger, time.Hour, 30*24*time.Hour, 50)
	trashSweeper.Start(workerCtx)

	userService := service.NewUserService(db)
	identifyService := service.NewIdentifyService(db, rdb)
	galleryAccessService := service.NewGalleryAccessService(db, cfg.Auth.GallerySuperAdminEmail)
	imageService.SetIdentifyCleanup(identifyService)
	variantSigner := service.NewVariantAccessSigner(cfg.Auth.JWTSecret, time.Hour)
	imageHandler := handler.NewImageHandler(
		imageService,
		cfg.EffectiveCDNBaseURL(),
		cfg.Server.BaseURL,
		cfg.AWS.PresignURLs,
		cfg.AWS.PresignTTLSeconds,
		variantSigner,
		cfg.Server.AppEnv == "staging" || cfg.Server.AppEnv == "production",
	)
	userHandler := handler.NewUserHandler(imageService, userService, imageHandler, identifyService, galleryAccessService)
	galleryHandler := handler.NewGalleryHandler(imageService)
	galleryAccessHandler := handler.NewGalleryAccessHandler(userService, galleryAccessService)
	requireGallery := middleware.RequireGalleryAccess(galleryAccessService)
	identifyHandler := handler.NewIdentifyHandler(identifyService)
	authHandler := handler.NewAuthHandler(userService, cfg.Auth.JWTSecret, cfg.Auth.GoogleClientID, cfg.Server.AppEnv)

	optionalAuth := middleware.OptionalAuthMiddleware(cfg.Auth.JWTSecret)
	uploadLimiter := middleware.RateLimitMiddleware(rdb, 10, time.Minute, "upload")
	// Per-user limits for Google Photos-style bulk / background sync.
	galleryInitLimiter := middleware.RateLimitUserMiddleware(rdb, 60, time.Minute, "gallery-init")
	galleryChunkLimiter := middleware.RateLimitUserMiddleware(rdb, 600, time.Minute, "gallery-chunk")
	variantLimiter := middleware.RateLimitMiddleware(rdb, 60, time.Minute, "variants")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handler.HealthzHandler)
	mux.HandleFunc("GET /readyz", handler.ReadyzHandler(db, rdb, s3Client))
	mux.HandleFunc("GET /metrics", metrics.Handler)
	mux.HandleFunc("POST /api/v1/auth/google", authHandler.GoogleAuthHandler)
	mux.HandleFunc("POST /api/v1/auth/dev-token", authHandler.DevTokenHandler)
	mux.Handle("POST /api/v1/images", uploadLimiter(http.HandlerFunc(imageHandler.UploadHandler)))
	mux.Handle("GET /api/v1/images/{id}", http.HandlerFunc(imageHandler.GetImageHandler))
	mux.Handle("GET /api/v1/images/{id}/original", variantLimiter(http.HandlerFunc(imageHandler.ServeOriginalHandler)))
	mux.Handle("GET /api/v1/images/{id}/variants/{preset}", variantLimiter(http.HandlerFunc(imageHandler.GetVariantHandler)))
	mux.Handle("GET /api/v1/users/me", middleware.RequireAuth(http.HandlerFunc(userHandler.MeHandler)))
	mux.Handle("GET /api/v1/users/me/images", middleware.RequireAuth(http.HandlerFunc(userHandler.ListMyImagesHandler)))
	mux.Handle("POST /api/v1/users/me/images/trash", middleware.RequireAuth(http.HandlerFunc(userHandler.TrashMyImagesHandler)))
	mux.Handle("POST /api/v1/users/me/images/restore", middleware.RequireAuth(http.HandlerFunc(userHandler.RestoreMyImagesHandler)))
	mux.Handle("POST /api/v1/users/me/images/purge", middleware.RequireAuth(http.HandlerFunc(userHandler.PurgeMyImagesHandler)))
	mux.Handle("DELETE /api/v1/users/me/images/{id}", middleware.RequireAuth(http.HandlerFunc(userHandler.DeleteMyImageHandler)))
	mux.Handle("DELETE /api/v1/users/me/images/{id}/variants/{preset}", middleware.RequireAuth(http.HandlerFunc(userHandler.DeleteMyVariantHandler)))
	mux.Handle("POST /api/v1/users/claim-guest-media", middleware.RequireAuth(http.HandlerFunc(userHandler.ClaimGuestMediaHandler)))

	// Personal gallery: super admin + allowlisted emails only (isolated from tools library).
	mux.Handle("GET /api/v1/users/me/gallery/media", requireGallery(http.HandlerFunc(userHandler.ListGalleryMediaHandler)))
	mux.Handle("POST /api/v1/users/me/uploads", requireGallery(galleryInitLimiter(http.HandlerFunc(galleryHandler.InitUploadHandler))))
	mux.Handle("PUT /api/v1/users/me/uploads/{id}/chunks/{index}", requireGallery(galleryChunkLimiter(http.HandlerFunc(galleryHandler.PutChunkHandler))))
	mux.Handle("POST /api/v1/users/me/uploads/{id}/complete", requireGallery(galleryInitLimiter(http.HandlerFunc(galleryHandler.CompleteUploadHandler))))
	mux.Handle("DELETE /api/v1/users/me/uploads/{id}", requireGallery(http.HandlerFunc(galleryHandler.AbortUploadHandler)))
	mux.Handle("GET /api/v1/users/me/albums", requireGallery(http.HandlerFunc(galleryHandler.ListAlbumsHandler)))
	mux.Handle("POST /api/v1/users/me/albums", requireGallery(http.HandlerFunc(galleryHandler.CreateAlbumHandler)))
	mux.Handle("PATCH /api/v1/users/me/albums/{id}", requireGallery(http.HandlerFunc(galleryHandler.UpdateAlbumHandler)))
	mux.Handle("DELETE /api/v1/users/me/albums/{id}", requireGallery(http.HandlerFunc(galleryHandler.DeleteAlbumHandler)))
	mux.Handle("POST /api/v1/users/me/albums/{id}/items", requireGallery(http.HandlerFunc(galleryHandler.AddAlbumItemsHandler)))
	mux.Handle("DELETE /api/v1/users/me/albums/{id}/items", requireGallery(http.HandlerFunc(galleryHandler.RemoveAlbumItemsHandler)))
	mux.Handle("GET /api/v1/admin/gallery-access", middleware.RequireAuth(http.HandlerFunc(galleryAccessHandler.ListAllowedEmailsHandler)))
	mux.Handle("POST /api/v1/admin/gallery-access", middleware.RequireAuth(http.HandlerFunc(galleryAccessHandler.AllowEmailHandler)))
	mux.Handle("POST /api/v1/admin/gallery-access/revoke", middleware.RequireAuth(http.HandlerFunc(galleryAccessHandler.RevokeEmailHandler)))

	mux.Handle("POST /api/v1/identify/lookup", http.HandlerFunc(identifyHandler.LookupHandler))
	mux.Handle("POST /api/v1/identify/store", optionalAuth(http.HandlerFunc(identifyHandler.StoreHandler)))

	// Order: request ID → optional auth (JWT/guest) → access logs/metrics → routes.
	root := middleware.CORSMiddleware(cfg.Server.CORSOrigins)(
		middleware.RequestIDMiddleware(
			optionalAuth(
				middleware.AccessLogMiddleware(slogLogger)(mux),
			),
		),
	)

	srv := &http.Server{
		Addr:              ":" + cfg.Server.Port,
		Handler:           root,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Minute, // large gallery chunks / bulk sync
		WriteTimeout:      15 * time.Minute,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slogLogger.Info("image-api starting",
			"env", cfg.Server.AppEnv,
			"port", cfg.Server.Port,
			"async_variants", true,
			"guest_expiry_sweeper", true,
			"trash_sweeper", true,
			"aws_credential_mode", s3Client.CredentialMode(),
			"presign_urls", cfg.AWS.PresignURLs,
			"ownership_enforced", true,
		)
		errCh <- srv.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			slogLogger.Error("server stopped", "error", err)
			stopWorkers()
			_ = rdb.Close()
			_ = db.Close()
			os.Exit(1)
		}
	case sig := <-sigCh:
		slog.Info("server shutting down...", "signal", sig.String())
		stopWorkers()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			slogLogger.Error("graceful shutdown failed", "error", err)
		}

		if err := rdb.Close(); err != nil {
			slogLogger.Error("redis close failed", "error", err)
		}
		if err := db.Close(); err != nil {
			slogLogger.Error("database close failed", "error", err)
		}

		slog.Info("server exited gracefully")
	}
}

func buildDatabaseURL(db config.DatabaseConfig) string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(db.User, db.Password),
		Host:   fmt.Sprintf("%s:%s", db.Host, db.Port),
		Path:   "/" + db.Name,
	}
	q := u.Query()
	q.Set("sslmode", db.SSLMode)
	u.RawQuery = q.Encode()
	return u.String()
}

func resolveMigrationsPath() string {
	candidates := []string{
		"migrations",
		"services/image-api/migrations",
	}
	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return path
		}
	}
	return "migrations"
}
