package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr        string
	BlobDir         string
	ThumbDir        string
	DatabasePath    string
	JWTSecret       string
	JWTSecretSource string
	MaxUploadBytes  int64
}

func Load() (Config, error) {
	maxMB := envInt("MAX_UPLOAD_MB", 32)
	if maxMB < 1 {
		maxMB = 1
	}
	if maxMB > 2048 {
		maxMB = 2048
	}

	cfg := Config{
		HTTPAddr:       env("HTTP_ADDR", "0.0.0.0:8080"),
		BlobDir:        env("BLOB_DIR", "./data/blobs"),
		ThumbDir:       env("THUMB_DIR", "./data/thumbs"),
		DatabasePath:   env("DATABASE_PATH", "./data/minicloud.db"),
		MaxUploadBytes: int64(maxMB) * 1024 * 1024,
	}

	secret, source, err := resolveSecret(
		os.Getenv("JWT_SECRET"),
		env("JWT_SECRET_FILE", "./data/.jwt-secret"),
	)
	if err != nil {
		return Config{}, err
	}
	cfg.JWTSecret = secret
	cfg.JWTSecretSource = source
	return cfg, nil
}

func resolveSecret(fromEnv, filePath string) (secret, source string, err error) {
	if s := strings.TrimSpace(fromEnv); s != "" {
		if s == "dev-only-change-me" {
			return "", "", fmt.Errorf("JWT_SECRET is the old placeholder; set a long random value")
		}
		if len(s) < 16 {
			return "", "", fmt.Errorf("JWT_SECRET must be at least 16 characters")
		}
		return s, "environment", nil
	}

	if b, readErr := os.ReadFile(filePath); readErr == nil {
		s := strings.TrimSpace(string(b))
		if len(s) >= 16 {
			return s, filePath, nil
		}
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	s := hex.EncodeToString(raw)
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(filePath, []byte(s+"\n"), 0o600); err != nil {
		return "", "", err
	}
	return s, filePath + " (created)", nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
