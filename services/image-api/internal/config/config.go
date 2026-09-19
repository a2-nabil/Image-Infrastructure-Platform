package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config holds immutable runtime configuration loaded from the environment.
type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Redis    RedisConfig
	AWS      AWSConfig
	Logging  LoggingConfig
}

type ServerConfig struct {
	Port   string
	AppEnv string
}

type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
	SSLMode  string
}

type RedisConfig struct {
	URL string
}

type AWSConfig struct {
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	S3DevBucket     string
}

type LoggingConfig struct {
	Level string
}

var validAppEnvs = map[string]struct{}{
	"development": {},
	"staging":     {},
	"production":  {},
}

var validLogLevels = map[string]struct{}{
	"debug": {},
	"info":  {},
	"warn":  {},
	"error": {},
}

// Load reads configuration from the OS environment (optionally seeding from a
// local .env file when present) and fails fast if mandatory values are missing.
func Load() (*Config, error) {
	loadDotEnvIfPresent()

	cfg := &Config{
		Server: ServerConfig{
			Port:   getenvDefault("PORT", "8080"),
			AppEnv: getenvDefault("APP_ENV", "development"),
		},
		Database: DatabaseConfig{
			Host:     os.Getenv("POSTGRES_HOST"),
			Port:     getenvDefault("POSTGRES_PORT", "5432"),
			User:     os.Getenv("POSTGRES_USER"),
			Password: os.Getenv("POSTGRES_PASSWORD"),
			Name:     os.Getenv("POSTGRES_DB"),
			SSLMode:  getenvDefault("POSTGRES_SSLMODE", "disable"),
		},
		Redis: RedisConfig{
			URL: os.Getenv("REDIS_URL"),
		},
		AWS: AWSConfig{
			Region:          os.Getenv("AWS_REGION"),
			AccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
			SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
			S3DevBucket:     os.Getenv("AWS_S3_DEV_BUCKET"),
		},
		Logging: LoggingConfig{
			Level: getenvDefault("LOG_LEVEL", "info"),
		},
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) validate() error {
	var missing []string

	if c.Database.Host == "" {
		missing = append(missing, "POSTGRES_HOST")
	}
	if c.Database.User == "" {
		missing = append(missing, "POSTGRES_USER")
	}
	if c.Database.Password == "" {
		missing = append(missing, "POSTGRES_PASSWORD")
	}
	if c.Database.Name == "" {
		missing = append(missing, "POSTGRES_DB")
	}
	if c.AWS.Region == "" {
		missing = append(missing, "AWS_REGION")
	}
	if c.AWS.AccessKeyID == "" {
		missing = append(missing, "AWS_ACCESS_KEY_ID")
	}
	if c.AWS.SecretAccessKey == "" {
		missing = append(missing, "AWS_SECRET_ACCESS_KEY")
	}
	if c.AWS.S3DevBucket == "" {
		missing = append(missing, "AWS_S3_DEV_BUCKET")
	}

	var errs []string
	if len(missing) > 0 {
		errs = append(errs, fmt.Sprintf("missing required configuration: %s", strings.Join(missing, ", ")))
	}
	if _, ok := validAppEnvs[c.Server.AppEnv]; !ok {
		errs = append(errs, fmt.Sprintf("invalid APP_ENV %q (allowed: development, staging, production)", c.Server.AppEnv))
	}
	if _, ok := validLogLevels[c.Logging.Level]; !ok {
		errs = append(errs, fmt.Sprintf("invalid LOG_LEVEL %q (allowed: debug, info, warn, error)", c.Logging.Level))
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func getenvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// loadDotEnvIfPresent loads key=value pairs from a nearby .env file without
// overriding variables already present in the OS environment.
func loadDotEnvIfPresent() {
	path, ok := findDotEnv()
	if !ok {
		return
	}

	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}

		value = strings.TrimSpace(value)
		value = unquote(value)
		_ = os.Setenv(key, value)
	}
}

func findDotEnv() (string, bool) {
	candidates := []string{".env"}

	if wd, err := os.Getwd(); err == nil {
		dir := wd
		for i := 0; i < 6; i++ {
			candidates = append(candidates, filepath.Join(dir, ".env"))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	seen := make(map[string]struct{})
	for _, path := range candidates {
		if path == "" {
			continue
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}

		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			continue
		}
		return abs, true
	}
	return "", false
}

func unquote(v string) string {
	if len(v) < 2 {
		return v
	}
	if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
		return v[1 : len(v)-1]
	}
	return v
}
