package main

import (
	"log"
	"net/http"

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

	log.Printf("listening on http://%s", cfg.HTTPAddr)
	log.Printf("files (bytes)   -> %s", cfg.BlobDir)
	log.Printf("previews        -> %s", cfg.ThumbDir)
	log.Printf("avatars         -> %s", cfg.AvatarDir)
	log.Printf("catalog (names) -> %s", cfg.DatabasePath)
	log.Printf("jwt secret      -> %s", cfg.JWTSecretSource)
	if cfg.SMTPHost == "" {
		log.Printf("mail            -> server log (set SMTP_HOST to send real codes)")
	} else {
		log.Printf("mail            -> smtp %s:%d from %s", cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPFrom)
	}
	log.Printf("max upload      -> %d MB", cfg.MaxUploadBytes/1024/1024)
	log.Printf("vault limit     -> %d GB", cfg.MaxStorageBytes/1024/1024/1024)
	log.Printf("open http://127.0.0.1:8080 in your browser")

	if err := http.ListenAndServe(cfg.HTTPAddr, apihttp.New(store, cat, thumbs, avatars, cfg)); err != nil {
		log.Fatal(err)
	}
}
