package config

import (
	"strings"
	"testing"
)

func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("POSTGRES_HOST", "localhost")
	t.Setenv("POSTGRES_USER", "image_platform")
	t.Setenv("POSTGRES_PASSWORD", "secret")
	t.Setenv("POSTGRES_DB", "image_platform")
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_ACCESS_KEY_ID", "test-access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret-key")
	t.Setenv("AWS_S3_DEV_BUCKET", "dev-bucket")
}

func TestLoadSuccess(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("PORT", "9090")
	t.Setenv("APP_ENV", "staging")
	t.Setenv("POSTGRES_PORT", "5433")
	t.Setenv("POSTGRES_SSLMODE", "require")
	t.Setenv("REDIS_URL", "redis://localhost:6379/1")
	t.Setenv("LOG_LEVEL", "debug")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.Server.Port != "9090" {
		t.Errorf("Server.Port = %q, want %q", cfg.Server.Port, "9090")
	}
	if cfg.Server.AppEnv != "staging" {
		t.Errorf("Server.AppEnv = %q, want %q", cfg.Server.AppEnv, "staging")
	}
	if cfg.Database.Host != "localhost" {
		t.Errorf("Database.Host = %q, want %q", cfg.Database.Host, "localhost")
	}
	if cfg.Database.Port != "5433" {
		t.Errorf("Database.Port = %q, want %q", cfg.Database.Port, "5433")
	}
	if cfg.Database.User != "image_platform" {
		t.Errorf("Database.User = %q, want %q", cfg.Database.User, "image_platform")
	}
	if cfg.Database.Password != "secret" {
		t.Errorf("Database.Password = %q, want %q", cfg.Database.Password, "secret")
	}
	if cfg.Database.Name != "image_platform" {
		t.Errorf("Database.Name = %q, want %q", cfg.Database.Name, "image_platform")
	}
	if cfg.Database.SSLMode != "require" {
		t.Errorf("Database.SSLMode = %q, want %q", cfg.Database.SSLMode, "require")
	}
	if cfg.Redis.URL != "redis://localhost:6379/1" {
		t.Errorf("Redis.URL = %q, want %q", cfg.Redis.URL, "redis://localhost:6379/1")
	}
	if cfg.AWS.Region != "us-east-1" {
		t.Errorf("AWS.Region = %q, want %q", cfg.AWS.Region, "us-east-1")
	}
	if cfg.AWS.AccessKeyID != "test-access-key" {
		t.Errorf("AWS.AccessKeyID = %q, want %q", cfg.AWS.AccessKeyID, "test-access-key")
	}
	if cfg.AWS.SecretAccessKey != "test-secret-key" {
		t.Errorf("AWS.SecretAccessKey = %q, want %q", cfg.AWS.SecretAccessKey, "test-secret-key")
	}
	if cfg.AWS.S3DevBucket != "dev-bucket" {
		t.Errorf("AWS.S3DevBucket = %q, want %q", cfg.AWS.S3DevBucket, "dev-bucket")
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("Logging.Level = %q, want %q", cfg.Logging.Level, "debug")
	}
}

func TestLoadMissingRequired(t *testing.T) {
	// Clear required variables so fail-fast validation triggers.
	t.Setenv("POSTGRES_HOST", "")
	t.Setenv("POSTGRES_USER", "")
	t.Setenv("POSTGRES_PASSWORD", "")
	t.Setenv("POSTGRES_DB", "")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_S3_DEV_BUCKET", "")
	t.Setenv("APP_ENV", "development")
	t.Setenv("LOG_LEVEL", "info")

	cfg, err := Load()
	if err == nil {
		t.Fatal("Load() expected error for missing required configuration, got nil")
	}
	if cfg != nil {
		t.Fatal("Load() expected nil config on validation failure")
	}

	msg := err.Error()
	required := []string{
		"POSTGRES_HOST",
		"POSTGRES_USER",
		"POSTGRES_PASSWORD",
		"POSTGRES_DB",
		"AWS_REGION",
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
		"AWS_S3_DEV_BUCKET",
	}
	for _, name := range required {
		if !strings.Contains(msg, name) {
			t.Errorf("error missing %q: %s", name, msg)
		}
	}
}

func TestLoadDefaults(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("PORT", "")
	t.Setenv("APP_ENV", "")
	t.Setenv("POSTGRES_PORT", "")
	t.Setenv("POSTGRES_SSLMODE", "")
	t.Setenv("LOG_LEVEL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.Server.Port != "8080" {
		t.Errorf("Server.Port = %q, want default %q", cfg.Server.Port, "8080")
	}
	if cfg.Server.AppEnv != "development" {
		t.Errorf("Server.AppEnv = %q, want default %q", cfg.Server.AppEnv, "development")
	}
	if cfg.Database.Port != "5432" {
		t.Errorf("Database.Port = %q, want default %q", cfg.Database.Port, "5432")
	}
	if cfg.Database.SSLMode != "disable" {
		t.Errorf("Database.SSLMode = %q, want default %q", cfg.Database.SSLMode, "disable")
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("Logging.Level = %q, want default %q", cfg.Logging.Level, "info")
	}
}

func TestLoadInvalidAppEnv(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("APP_ENV", "local")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected error for invalid APP_ENV, got nil")
	}
	if !strings.Contains(err.Error(), "APP_ENV") {
		t.Errorf("error should mention APP_ENV: %v", err)
	}
}
