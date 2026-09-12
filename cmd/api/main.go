package main

import (
	"context"
	"log"
	"net/http"
	"strings"

	"minicloud/internal/auth"
	"minicloud/internal/avatar"
	"minicloud/internal/blob"
	"minicloud/internal/catalog"
	"minicloud/internal/config"
	apihttp "minicloud/internal/http"
	"minicloud/internal/thumb"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	store, err := blob.NewFS(cfg.BlobDir)
	if err != nil {
		log.Fatalf("could not open the file shelf: %v", err)
	}

	thumbs, err := thumb.New(cfg.ThumbDir)
	if err != nil {
		log.Fatalf("could not open the thumbnail shelf: %v", err)
	}

	avatars, err := avatar.New(cfg.AvatarDir)
	if err != nil {
		log.Fatalf("could not open the avatar shelf: %v", err)
	}

	cat, err := catalog.Open(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("could not open the catalog: %v", err)
	}
	defer cat.Close()

	if err := seedAdmin(cat, cfg); err != nil {
		log.Fatalf("admin: %v", err)
	}

	log.Printf("listening on http://%s", cfg.HTTPAddr)
	log.Printf("files (bytes)   -> %s", cfg.BlobDir)
	log.Printf("previews        -> %s", cfg.ThumbDir)
	log.Printf("avatars         -> %s", cfg.AvatarDir)
	log.Printf("catalog (names) -> %s", cfg.DatabasePath)
	log.Printf("jwt secret      -> %s", cfg.JWTSecretSource)
	switch {
	case cfg.ResendAPIKey != "":
		log.Printf("mail            -> resend from %s", cfg.ResendFrom)
	case cfg.SMTPHost != "":
		log.Printf("mail            -> smtp %s:%d from %s", cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPFrom)
	default:
		log.Printf("mail            -> server log (set RESEND_API_KEY or SMTP_HOST to send real codes)")
	}
	log.Printf("max upload      -> %d MB", cfg.MaxUploadBytes/1024/1024)
	log.Printf("vault limit     -> %d GB", cfg.MaxStorageBytes/1024/1024/1024)
	log.Printf("open http://127.0.0.1:8080 in your browser")

	if err := http.ListenAndServe(cfg.HTTPAddr, apihttp.New(store, cat, thumbs, avatars, cfg)); err != nil {
		log.Fatal(err)
	}
}

func seedAdmin(cat *catalog.DB, cfg config.Config) error {
	email := strings.TrimSpace(cfg.AdminEmail)
	if email == "" {
		return nil
	}
	hash := ""
	if pw := cfg.AdminPassword; strings.TrimSpace(pw) != "" {
		var err error
		hash, err = auth.HashPassword(pw)
		if err != nil {
			return err
		}
	}
	if err := cat.EnsureAdmin(context.Background(), email, hash); err != nil {
		return err
	}
	log.Printf("admin account is loaded from environment")
	return nil
}
