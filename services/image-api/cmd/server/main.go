package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
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
	defer db.Close()

	redisOpts, err := redis.ParseURL(cfg.RedisAddr())
	if err != nil {
		slogLogger.Error("invalid redis url", "error", err)
		os.Exit(1)
	}
	rdb := redis.NewClient(redisOpts)
	defer rdb.Close()

	if err := middleware.PingRedis(context.Background(), rdb); err != nil {
		slogLogger.Error("redis connection failed", "error", err)
		os.Exit(1)
	}

	s3Client, err := storage.NewS3Client(context.Background(), cfg)
	if err != nil {
		slogLogger.Error("s3 client initialization failed", "error", err)
		os.Exit(1)
	}

	imageService := service.NewImageService(db, s3Client)
	imageHandler := handler.NewImageHandler(imageService)

	uploadLimiter := middleware.RateLimitMiddleware(rdb, 10, time.Minute, "upload")
	variantLimiter := middleware.RateLimitMiddleware(rdb, 60, time.Minute, "variants")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.Handle("POST /api/v1/images", uploadLimiter(http.HandlerFunc(imageHandler.UploadHandler)))
	mux.Handle("GET /api/v1/images/{id}/variants/{preset}", variantLimiter(http.HandlerFunc(imageHandler.GetVariantHandler)))

	root := middleware.CORSMiddleware(middleware.RequestIDMiddleware(mux))

	slogLogger.Info("image-api starting",
		"env", cfg.Server.AppEnv,
		"port", cfg.Server.Port,
	)

	addr := ":" + cfg.Server.Port
	if err := http.ListenAndServe(addr, root); err != nil {
		slogLogger.Error("server stopped", "error", err)
		os.Exit(1)
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
