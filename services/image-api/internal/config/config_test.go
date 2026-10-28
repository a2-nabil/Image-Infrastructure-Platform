package config

import (
	"strings"
	"testing"
)

func TestValidateRequiresJWTAndCORSInProduction(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			AppEnv:      "production",
			CORSOrigins: nil,
		},
		Database: DatabaseConfig{
			Host:     "db",
			User:     "u",
			Password: "p",
			Name:     "n",
			SSLMode:  "require",
		},
		AWS: AWSConfig{
			Region:          "ap-southeast-1",
			AccessKeyID:     "AKIA",
			SecretAccessKey: "secret",
			S3DevBucket:     "bucket",
		},
		Logging: LoggingConfig{Level: "info"},
	}

	err := cfg.validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "JWT_SECRET") {
		t.Fatalf("expected JWT_SECRET error, got %q", msg)
	}
	if !strings.Contains(msg, "CORS_ALLOWED_ORIGINS") {
		t.Fatalf("expected CORS_ALLOWED_ORIGINS error, got %q", msg)
	}
}

func TestValidateRejectsDisableSSLInStaging(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			AppEnv:      "staging",
			CORSOrigins: []string{"https://staging.example.com"},
		},
		Database: DatabaseConfig{
			Host:     "db",
			User:     "u",
			Password: "p",
			Name:     "n",
			SSLMode:  "disable",
		},
		AWS: AWSConfig{
			Region:          "ap-southeast-1",
			AccessKeyID:     "AKIA",
			SecretAccessKey: "secret",
			S3DevBucket:     "bucket",
		},
		Auth:    AuthConfig{JWTSecret: "super-secret"},
		Logging: LoggingConfig{Level: "info"},
	}
	err := cfg.validate()
	if err == nil || !strings.Contains(err.Error(), "POSTGRES_SSLMODE") {
		t.Fatalf("expected POSTGRES_SSLMODE error, got %v", err)
	}
}

func TestValidateDevelopmentAllowsLocalDefaults(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{AppEnv: "development"},
		Database: DatabaseConfig{
			Host:     "localhost",
			User:     "postgres",
			Password: "postgres",
			Name:     "image_platform_dev",
			SSLMode:  "disable",
		},
		AWS: AWSConfig{
			Region:          "ap-southeast-1",
			AccessKeyID:     "AKIA",
			SecretAccessKey: "secret",
			S3DevBucket:     "bucket",
		},
		Logging: LoggingConfig{Level: "debug"},
	}
	if err := cfg.validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateProductionAllowsIRSAWithoutStaticKeys(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			AppEnv:      "production",
			CORSOrigins: []string{"https://app.example.com"},
		},
		Database: DatabaseConfig{
			Host:     "db",
			User:     "u",
			Password: "p",
			Name:     "n",
			SSLMode:  "require",
		},
		AWS: AWSConfig{
			Region:      "ap-southeast-1",
			S3DevBucket: "bucket",
			// No static keys — IRSA / default credential chain.
		},
		Auth:    AuthConfig{JWTSecret: "super-secret"},
		Logging: LoggingConfig{Level: "info"},
	}
	if err := cfg.validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateDevelopmentRequiresStaticKeys(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{AppEnv: "development"},
		Database: DatabaseConfig{
			Host:     "localhost",
			User:     "postgres",
			Password: "postgres",
			Name:     "image_platform_dev",
			SSLMode:  "disable",
		},
		AWS: AWSConfig{
			Region:      "ap-southeast-1",
			S3DevBucket: "bucket",
		},
		Logging: LoggingConfig{Level: "info"},
	}
	err := cfg.validate()
	if err == nil || !strings.Contains(err.Error(), "AWS_ACCESS_KEY_ID") {
		t.Fatalf("expected AWS_ACCESS_KEY_ID error, got %v", err)
	}
}

func TestSplitCSV(t *testing.T) {
	got := splitCSV(" https://a.example.com/ ,https://b.example.com ")
	if len(got) != 2 || got[0] != "https://a.example.com" || got[1] != "https://b.example.com" {
		t.Fatalf("unexpected: %#v", got)
	}
}
