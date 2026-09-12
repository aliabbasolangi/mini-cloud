package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"minicloud/internal/blob"
	"minicloud/internal/catalog"
	"minicloud/internal/config"
	"minicloud/internal/thumb"
)

func New(store blob.Store, cat *catalog.DB, thumbs *thumb.Store, cfg config.Config) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	authH := authHandlers{catalog: cat, secret: cfg.JWTSecret}
	objH := objectHandlers{blobs: store, catalog: cat, thumbs: thumbs, maxUpload: cfg.MaxUploadBytes}

	r.Get("/healthz", health)
	r.Get("/s/{token}", objH.publicGet)
	r.Post("/v1/auth/register", authH.register)
	r.Post("/v1/auth/login", authH.login)

	r.Group(func(r chi.Router) {
		r.Use(requireAuth(cfg.JWTSecret))
		r.Get("/v1/objects", objH.list)
		r.Post("/v1/move", objH.move)
		r.Get("/v1/thumbs/*", objH.preview)
		r.Put("/v1/objects/*", objH.put)
		r.Get("/v1/objects/*", objH.get)
		r.Delete("/v1/objects/*", objH.del)
		r.Post("/v1/shares", objH.createShare)
		r.Delete("/v1/shares/{token}", objH.revokeShare)
	})

	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		http.ServeFile(w, req, "web/index.html")
	})
	return r
}
