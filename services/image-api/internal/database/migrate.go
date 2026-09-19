package database

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// RunMigrations applies all pending SQL migrations from migrationsPath against dbURL.
func RunMigrations(dbURL string, migrationsPath string) error {
	if dbURL == "" {
		return fmt.Errorf("database URL is required")
	}
	if migrationsPath == "" {
		return fmt.Errorf("migrations path is required")
	}

	sourceURL := fmt.Sprintf("file://%s", migrationsPath)
	slog.Info("running database migrations", "source", sourceURL)

	m, err := migrate.New(sourceURL, dbURL)
	if err != nil {
		return fmt.Errorf("create migrate instance: %w", err)
	}
	defer func() {
		sourceErr, dbErr := m.Close()
		if sourceErr != nil {
			slog.Warn("migrate source close error", "error", sourceErr)
		}
		if dbErr != nil {
			slog.Warn("migrate database close error", "error", dbErr)
		}
	}()

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			version, dirty, verErr := m.Version()
			if verErr != nil && !errors.Is(verErr, migrate.ErrNilVersion) {
				slog.Info("migrations already up to date")
				return nil
			}
			slog.Info("migrations already up to date", "version", version, "dirty", dirty)
			return nil
		}
		return fmt.Errorf("apply migrations: %w", err)
	}

	version, dirty, err := m.Version()
	if err != nil {
		return fmt.Errorf("read migration version: %w", err)
	}
	slog.Info("migrations applied successfully", "version", version, "dirty", dirty)
	return nil
}
