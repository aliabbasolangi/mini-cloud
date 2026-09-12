package main

import (
	"log"
	"net/http"

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

	cat, err := catalog.Open(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("could not open the catalog: %v", err)
	}
	defer cat.Close()

	log.Printf("listening on http://%s", cfg.HTTPAddr)
	log.Printf("files (bytes)   -> %s", cfg.BlobDir)
	log.Printf("previews        -> %s", cfg.ThumbDir)
	log.Printf("catalog (names) -> %s", cfg.DatabasePath)
	log.Printf("jwt secret      -> %s", cfg.JWTSecretSource)
	log.Printf("max upload      -> %d MB", cfg.MaxUploadBytes/1024/1024)
	log.Printf("open http://127.0.0.1:8080 in your browser")

	if err := http.ListenAndServe(cfg.HTTPAddr, apihttp.New(store, cat, thumbs, cfg)); err != nil {
		log.Fatal(err)
	}
}
