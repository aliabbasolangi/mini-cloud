package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSecretCreatesAndReusesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".jwt-secret")

	first, src, err := resolveSecret("", path)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) < 32 {
		t.Fatalf("secret too short: %q", first)
	}
	if src == "environment" {
		t.Fatal("expected file source")
	}

	second, _, err := resolveSecret("", path)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("secret file should be stable across restarts")
	}
}

func TestResolveSecretPrefersEnv(t *testing.T) {
	got, src, err := resolveSecret("a-very-long-secret-value", "unused")
	if err != nil {
		t.Fatal(err)
	}
	if got != "a-very-long-secret-value" || src != "environment" {
		t.Fatalf("got %q %q", got, src)
	}
}

func TestResolveSecretRejectsPlaceholder(t *testing.T) {
	if _, _, err := resolveSecret("dev-only-change-me", ""); err == nil {
		t.Fatal("placeholder must be rejected")
	}
}

func TestLoadDefaultAddr(t *testing.T) {
	t.Setenv("JWT_SECRET", "a-very-long-secret-value")
	t.Setenv("HTTP_ADDR", "")
	os.Unsetenv("HTTP_ADDR")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != "0.0.0.0:8080" {
		t.Fatalf("addr %q", cfg.HTTPAddr)
	}
	if cfg.MaxUploadBytes != 32*1024*1024 {
		t.Fatalf("max %d", cfg.MaxUploadBytes)
	}
}
