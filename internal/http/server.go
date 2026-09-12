package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"minicloud/internal/avatar"
	"minicloud/internal/blob"
	"minicloud/internal/catalog"
	"minicloud/internal/config"
	"minicloud/internal/mail"
	"minicloud/internal/thumb"
)

func New(store blob.Store, cat *catalog.DB, thumbs *thumb.Store, avatars *avatar.Store, cfg config.Config) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	authH := authHandlers{catalog: cat, avatars: avatars, secret: cfg.JWTSecret, mail: mail.New(cfg)}
	objH := objectHandlers{
		blobs:      store,
		catalog:    cat,
		thumbs:     thumbs,
		maxUpload:  cfg.MaxUploadBytes,
		maxStorage: cfg.MaxStorageBytes,
	}

	r.Get("/healthz", health)
	r.Get("/s/{token}/raw", objH.publicGet)
	r.Get("/s/{token}", objH.publicPage)
	r.Post("/v1/auth/register/start", authH.registerStart)
	r.Post("/v1/auth/register/verify", authH.registerVerify)
	r.Post("/v1/auth/login", authH.login)
	r.Post("/v1/auth/forgot", authH.forgot)
	r.Post("/v1/auth/reset", authH.reset)
	r.Post("/v1/auth/resend", authH.resend)

	r.Group(func(r chi.Router) {
		r.Use(requireAuth(cfg.JWTSecret))
		r.Get("/v1/objects", objH.list)
		r.Post("/v1/move", objH.move)
		r.Get("/v1/thumbs/*", objH.preview)
		r.Put("/v1/objects/*", objH.put)
		r.Get("/v1/objects/*", objH.get)
		r.Delete("/v1/objects/*", objH.del)
		r.Post("/v1/folders/delete", objH.deleteFolder)
		r.Post("/v1/shares", objH.createShare)
		r.Delete("/v1/shares/{token}", objH.revokeShare)
		r.Get("/v1/me", authH.me)
		r.Patch("/v1/me", authH.updateMe)
		r.Put("/v1/me/vault", authH.putVault)
		r.Delete("/v1/me", authH.deleteMe)
		r.Get("/v1/me/avatar", authH.getAvatar)
		r.Put("/v1/me/avatar", authH.putAvatar)
		r.Delete("/v1/me/avatar", authH.deleteAvatar)
		r.Get("/v1/notifications", authH.listNotifications)
		r.Post("/v1/notifications/read-all", authH.readAllNotifications)
		r.Post("/v1/notifications/{id}/read", authH.readNotification)
		r.Post("/v1/convert", objH.convert)
		r.Post("/v1/convert/raw", objH.convertRaw)

		r.Post("/v1/collab/folders", objH.createCollabFolder)
		r.Get("/v1/collab/folders", objH.listCollabFolders)
		r.Get("/v1/collab/invites", objH.listCollabInvites)
		r.Post("/v1/collab/invites/{id}/accept", objH.acceptCollabInvite)
		r.Post("/v1/collab/invites/{id}/decline", objH.declineCollabInvite)
		r.Delete("/v1/collab/folders/{id}", objH.deleteCollabFolder)
		r.Post("/v1/collab/folders/{id}/leave", objH.leaveCollabFolder)
		r.Post("/v1/collab/folders/{id}/invites", objH.inviteCollab)
		r.Get("/v1/collab/folders/{id}/members", objH.listCollabMembers)
		r.Get("/v1/collab/folders/{id}/objects", objH.listCollabObjects)
		r.Post("/v1/collab/folders/{id}/move", objH.collabMove)
		r.Post("/v1/collab/folders/{id}/folders", objH.collabMkdir)
		r.Post("/v1/collab/folders/{id}/folders/delete", objH.collabRmDir)
		r.Post("/v1/collab/folders/{id}/shares", objH.collabShare)
		r.Put("/v1/collab/folders/{id}/objects/*", objH.collabPut)
		r.Get("/v1/collab/folders/{id}/objects/*", objH.collabGet)
		r.Delete("/v1/collab/folders/{id}/objects/*", objH.collabDel)
		r.Get("/v1/collab/folders/{id}/thumbs/*", objH.collabPreview)
	})

	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		http.ServeFile(w, req, "web/index.html")
	})
	return r
}
