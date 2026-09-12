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
	AvatarDir       string
	DatabasePath    string
	JWTSecret       string
	JWTSecretSource string
	MaxUploadBytes  int64
	MaxStorageBytes int64
	SMTPHost        string
	SMTPPort        int
	SMTPUser        string
	SMTPPass        string
	SMTPFrom        string
	MailLog         bool
}

func Load() (Config, error) {
	loadDotEnv(".env")

	maxMB := envInt("MAX_UPLOAD_MB", 32)
	if maxMB < 1 {
		maxMB = 1
	}
	if maxMB > 2048 {
		maxMB = 2048
	}

	storageGB := envInt("MAX_STORAGE_GB", 5)
	if storageGB < 1 {
		storageGB = 1
	}
	if storageGB > 1024 {
		storageGB = 1024
	}

	smtpHost := strings.TrimSpace(os.Getenv("SMTP_HOST"))
	smtpPort := envInt("SMTP_PORT", 587)
	if smtpPort < 1 || smtpPort > 65535 {
		smtpPort = 587
	}

	cfg := Config{
		HTTPAddr:        listenAddr(),
		BlobDir:         env("BLOB_DIR", "./data/blobs"),
		ThumbDir:        env("THUMB_DIR", "./data/thumbs"),
		AvatarDir:       env("AVATAR_DIR", "./data/avatars"),
		DatabasePath:    env("DATABASE_PATH", "./data/minicloud.db"),
		MaxUploadBytes:  int64(maxMB) * 1024 * 1024,
		MaxStorageBytes: int64(storageGB) * 1024 * 1024 * 1024,
		SMTPHost:        smtpHost,
		SMTPPort:        smtpPort,
		SMTPUser:        os.Getenv("SMTP_USER"),
		SMTPPass:        os.Getenv("SMTP_PASS"),
		SMTPFrom:        env("SMTP_FROM", os.Getenv("SMTP_USER")),
		MailLog:         envBool("MAIL_LOG", smtpHost == ""),
	}
	if strings.TrimSpace(cfg.SMTPPass) == "" {
		cfg.SMTPHost = ""
		cfg.MailLog = true
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

// listenAddr prefers HTTP_ADDR, then PaaS PORT (Railway, Fly, Render), then 8080.
func listenAddr() string {
	if v := strings.TrimSpace(os.Getenv("HTTP_ADDR")); v != "" {
		return v
	}
	if p := strings.TrimSpace(os.Getenv("PORT")); p != "" {
		if strings.Contains(p, ":") {
			return p
		}
		return "0.0.0.0:" + p
	}
	return "0.0.0.0:8080"
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

func envBool(key string, fallback bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
