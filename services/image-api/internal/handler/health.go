package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"

	"image-infrastructure-platform/services/image-api/internal/storage"
)

const readinessTimeout = 2 * time.Second

// HealthzHandler reports liveness without checking dependencies.
func HealthzHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// ReadyzHandler reports readiness of database, Redis, and S3 storage.
func ReadyzHandler(db *sql.DB, rdb *redis.Client, s3Client *storage.S3Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		checks := map[string]string{
			"database": "ok",
			"redis":    "ok",
			"storage":  "ok",
		}
		ready := true

		dbCtx, dbCancel := context.WithTimeout(r.Context(), readinessTimeout)
		defer dbCancel()
		if db == nil {
			checks["database"] = "unavailable"
			ready = false
		} else if err := db.PingContext(dbCtx); err != nil {
			checks["database"] = "error"
			ready = false
		}

		redisCtx, redisCancel := context.WithTimeout(r.Context(), readinessTimeout)
		defer redisCancel()
		if rdb == nil {
			checks["redis"] = "unavailable"
			ready = false
		} else if err := rdb.Ping(redisCtx).Err(); err != nil {
			checks["redis"] = "error"
			ready = false
		}

		storageCtx, storageCancel := context.WithTimeout(r.Context(), readinessTimeout)
		defer storageCancel()
		if s3Client == nil {
			checks["storage"] = "unavailable"
			ready = false
		} else if err := s3Client.Ready(storageCtx); err != nil {
			checks["storage"] = "error"
			ready = false
		}

		payload := map[string]string{
			"status":   "ready",
			"database": checks["database"],
			"redis":    checks["redis"],
			"storage":  checks["storage"],
		}

		statusCode := http.StatusOK
		if !ready {
			payload["status"] = "not_ready"
			statusCode = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(payload)
	}
}
