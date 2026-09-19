package main

import (
	"fmt"
	"log"
	"os"

	"image-infrastructure-platform/services/image-api/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Printf("failed to load configuration: %v", err)
		os.Exit(1)
	}

	// Safe non-secret startup signal only.
	fmt.Printf("image-api starting env=%s port=%s\n", cfg.Server.AppEnv, cfg.Server.Port)
}
