package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"image-infrastructure-platform/services/image-api/internal/config"
	"image-infrastructure-platform/services/image-api/internal/logger"
	"image-infrastructure-platform/services/image-api/internal/middleware"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Printf("failed to load configuration: %v", err)
		os.Exit(1)
	}

	slogLogger := logger.New(cfg.Server.AppEnv, cfg.Logging.Level)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	handler := middleware.RequestIDMiddleware(mux)

	slogLogger.Info("image-api starting",
		"env", cfg.Server.AppEnv,
		"port", cfg.Server.Port,
	)

	addr := ":" + cfg.Server.Port
	if err := http.ListenAndServe(addr, handler); err != nil {
		slogLogger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
