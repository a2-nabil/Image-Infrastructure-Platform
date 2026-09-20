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
	"image-infrastructure-platform/services/image-api/internal/middleware"
	"image-infrastructure-platform/services/image-api/internal/service"
	"image-infrastructure-platform/services/image-api/internal/storage"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Printf("failed to load configuration: %v", err)
		os.Exit(1)
	}

	slogLogger := logger.New(cfg.Server.AppEnv, cfg.Logging.Level)
	dbURL := buildDatabaseURL(cfg.Database)

	if cfg.Server.AppEnv == "development" {
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
	userService := service.NewUserService(db)
	imageHandler := handler.NewImageHandler(imageService, cfg.EffectiveCDNBaseURL(), cfg.Server.BaseURL)
	userHandler := handler.NewUserHandler(imageService)
	authHandler := handler.NewAuthHandler(userService, cfg.Auth.JWTSecret, cfg.Auth.GoogleClientID, cfg.Server.AppEnv)

	optionalAuth := middleware.OptionalAuthMiddleware(cfg.Auth.JWTSecret)
	uploadLimiter := middleware.RateLimitMiddleware(rdb, 10, time.Minute, "upload")
	variantLimiter := middleware.RateLimitMiddleware(rdb, 60, time.Minute, "variants")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handler.HealthzHandler)
	mux.HandleFunc("GET /readyz", handler.ReadyzHandler(db, rdb, s3Client))
	mux.HandleFunc("POST /api/v1/auth/google", authHandler.GoogleAuthHandler)
	mux.HandleFunc("POST /api/v1/auth/dev-token", authHandler.DevTokenHandler)
	mux.Handle("POST /api/v1/images", uploadLimiter(optionalAuth(http.HandlerFunc(imageHandler.UploadHandler))))
	mux.Handle("GET /api/v1/images/{id}/variants/{preset}", variantLimiter(http.HandlerFunc(imageHandler.GetVariantHandler)))
	mux.Handle("POST /api/v1/users/claim-guest-media", optionalAuth(middleware.RequireAuth(http.HandlerFunc(userHandler.ClaimGuestMediaHandler))))

	root := middleware.CORSMiddleware(middleware.RequestIDMiddleware(mux))

	srv := &http.Server{
		Addr:              ":" + cfg.Server.Port,
		Handler:           root,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slogLogger.Info("image-api starting",
			"env", cfg.Server.AppEnv,
			"port", cfg.Server.Port,
		)
		errCh <- srv.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			slogLogger.Error("server stopped", "error", err)
			_ = rdb.Close()
			_ = db.Close()
			os.Exit(1)
		}
	case sig := <-sigCh:
		slog.Info("server shutting down...", "signal", sig.String())

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
