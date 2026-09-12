package http

import (
	"errors"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"

	"minicloud/internal/blob"
	"minicloud/internal/catalog"
	"minicloud/internal/thumb"
)

type objectHandlers struct {
	blobs     blob.Store
	catalog   *catalog.DB
	thumbs    *thumb.Store
	maxUpload int64
}

func normalizeKey(raw string) (string, bool) {
	raw = strings.TrimSpace(strings.TrimPrefix(raw, "/"))
	if raw == "" || len(raw) > 512 || strings.Contains(raw, "..") {
		return "", false
	}
	return raw, true
}

func (h objectHandlers) put(w http.ResponseWriter, r *http.Request) {
	key, ok := normalizeKey(chi.URLParam(r, "*"))
	if !ok {
		writeError(w, http.StatusBadRequest, "bad file name")
		return
	}

	limitBody(w, r, h.maxUpload)
	result, err := h.blobs.Put(r.Context(), r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "file is too large")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not store file")
		return
	}

	obj, err := h.catalog.UpsertObject(r.Context(), userIDFrom(r.Context()), key, result.SHA256, result.Size)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save file name")
		return
	}

	if thumb.LooksLikeImage(key) {
		_ = h.thumbs.Ensure(r.Context(), h.blobs, result.SHA256)
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"key":     obj.Key,
		"sha256":  obj.BlobSHA,
		"size":    obj.Size,
		"created": obj.CreatedAt,
	})
}

func (h objectHandlers) get(w http.ResponseWriter, r *http.Request) {
	key, ok := normalizeKey(chi.URLParam(r, "*"))
	if !ok {
		writeError(w, http.StatusBadRequest, "bad file name")
		return
	}

	obj, err := h.catalog.ObjectByKey(r.Context(), userIDFrom(r.Context()), key)
	if errors.Is(err, catalog.ErrObjectNotFound) {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not look up file")
		return
	}

	f, err := h.blobs.Get(r.Context(), obj.BlobSHA)
	if errors.Is(err, blob.ErrNotFound) {
		writeError(w, http.StatusNotFound, "file bytes are missing")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read file")
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+path.Base(obj.Key)+`"`)
	_, _ = io.Copy(w, f)
}

func (h objectHandlers) list(w http.ResponseWriter, r *http.Request) {
	items, err := h.catalog.ListObjects(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list files")
		return
	}
	type row struct {
		Key      string `json:"key"`
		SHA256   string `json:"sha256"`
		Size     int64  `json:"size"`
		Created  string `json:"created"`
		HasThumb bool   `json:"has_thumb"`
	}
	out := make([]row, 0, len(items))
	for _, o := range items {
		out = append(out, row{
			Key:      o.Key,
			SHA256:   o.BlobSHA,
			Size:     o.Size,
			Created:  o.CreatedAt.Format("2006-01-02T15:04:05Z"),
			HasThumb: thumb.LooksLikeImage(o.Key),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": out})
}

func (h objectHandlers) del(w http.ResponseWriter, r *http.Request) {
	key, ok := normalizeKey(chi.URLParam(r, "*"))
	if !ok {
		writeError(w, http.StatusBadRequest, "bad file name")
		return
	}
	err := h.catalog.DeleteObject(r.Context(), userIDFrom(r.Context()), key)
	if errors.Is(err, catalog.ErrObjectNotFound) {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h objectHandlers) preview(w http.ResponseWriter, r *http.Request) {
	key, ok := normalizeKey(chi.URLParam(r, "*"))
	if !ok {
		writeError(w, http.StatusBadRequest, "bad file name")
		return
	}

	obj, err := h.catalog.ObjectByKey(r.Context(), userIDFrom(r.Context()), key)
	if errors.Is(err, catalog.ErrObjectNotFound) {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not look up file")
		return
	}

	if err := h.thumbs.Ensure(r.Context(), h.blobs, obj.BlobSHA); err != nil {
		if errors.Is(err, thumb.ErrNotImage) || errors.Is(err, blob.ErrNotFound) {
			writeError(w, http.StatusNotFound, "no preview")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not make preview")
		return
	}

	f, err := h.thumbs.Open(obj.BlobSHA)
	if err != nil {
		writeError(w, http.StatusNotFound, "no preview")
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, _ = io.Copy(w, f)
}
