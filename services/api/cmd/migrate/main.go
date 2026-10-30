package main

import (
	"fmt"
	"os"

	"image-infrastructure-platform/services/api/internal/database"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required")
		os.Exit(1)
	}
	path := "migrations"
	if len(os.Args) > 1 && os.Args[1] != "" {
		path = os.Args[1]
	}
	if err := database.RunMigrations(dbURL, path); err != nil {
		fmt.Fprintln(os.Stderr, "migrate failed:", err)
		os.Exit(1)
	}
	fmt.Println("migrations OK")
}
