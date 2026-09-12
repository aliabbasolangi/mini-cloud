package http

import (
	"errors"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"

	"minicloud/internal/catalog"
)

func (h objectHandlers) createShare(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key string `json:"key"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "send the file name as JSON")
		return
	}
	key, ok := normalizeKey(req.Key)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad file name")
		return
	}

	share, err := h.catalog.CreateShare(r.Context(), userIDFrom(r.Context()), key)
	if errors.Is(err, catalog.ErrObjectNotFound) {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create link")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"token":   share.Token,
		"url":     publicShareURL(r, share.Token),
		"expires": share.ExpiresAt.Format("2006-01-02T15:04:05Z"),
	})
}

func (h objectHandlers) revokeShare(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	err := h.catalog.RevokeShare(r.Context(), userIDFrom(r.Context()), token)
	if errors.Is(err, catalog.ErrShareNotFound) {
		writeError(w, http.StatusNotFound, "link not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not revoke link")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h objectHandlers) publicGet(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	share, err := h.catalog.ValidShare(r.Context(), token)
	if errors.Is(err, catalog.ErrShareExpired) {
		writeError(w, http.StatusGone, "this link has expired")
		return
	}
	if errors.Is(err, catalog.ErrShareNotFound) || err != nil {
		writeError(w, http.StatusNotFound, "link not found")
		return
	}

	obj, err := h.catalog.ObjectByKey(r.Context(), share.OwnerID, share.ObjectKey)
	if errors.Is(err, catalog.ErrObjectNotFound) {
		writeError(w, http.StatusNotFound, "file is gone")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not look up file")
		return
	}

	f, err := h.blobs.Get(r.Context(), obj.BlobSHA)
	if err != nil {
		writeError(w, http.StatusNotFound, "file bytes are missing")
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+path.Base(obj.Key)+`"`)
	_, _ = io.Copy(w, f)
}

func publicShareURL(r *http.Request, token string) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/s/" + token
}
