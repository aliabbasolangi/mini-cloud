package http

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"

	"minicloud/internal/blob"
	"minicloud/internal/catalog"
	"minicloud/internal/thumb"
)

type objectHandlers struct {
	blobs      blob.Store
	catalog    *catalog.DB
	thumbs     *thumb.Store
	maxUpload  int64
	maxStorage int64
	uploadDir  string
}

func unescapeKey(raw string) string {
	raw = strings.TrimSpace(strings.TrimPrefix(raw, "/"))
	for i := 0; i < 3; i++ {
		next, err := url.PathUnescape(raw)
		if err != nil || next == raw {
			break
		}
		raw = next
	}
	return strings.TrimSpace(raw)
}

func normalizeKey(raw string) (string, bool) {
	raw = unescapeKey(raw)
	if raw == "" || len(raw) > 512 || strings.Contains(raw, "..") {
		return "", false
	}
	return raw, true
}

func keyGuesses(raw string) []string {
	raw = strings.TrimSpace(strings.TrimPrefix(raw, "/"))
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	add(raw)
	cur := raw
	for i := 0; i < 3; i++ {
		next, err := url.PathUnescape(cur)
		if err != nil || next == cur {
			break
		}
		add(next)
		cur = next
	}
	add(strings.ReplaceAll(strings.ReplaceAll(raw, "%2F", "/"), "%2f", "/"))
	return out
}

func (h objectHandlers) objectByKey(ctx context.Context, ownerID, raw string) (*catalog.Object, error) {
	var last error
	for _, key := range keyGuesses(raw) {
		obj, err := h.catalog.ObjectByKey(ctx, ownerID, key)
		if err == nil {
			return obj, nil
		}
		last = err
		if !errors.Is(err, catalog.ErrObjectNotFound) {
			return nil, err
		}
	}
	if last == nil {
		return nil, catalog.ErrObjectNotFound
	}
	return nil, last
}

func (h objectHandlers) put(w http.ResponseWriter, r *http.Request) {
	key, ok := normalizeKey(chi.URLParam(r, "*"))
	if !ok || catalog.IsCollabKey(key) {
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

	owner := userIDFrom(r.Context())
	if err := h.ensureQuota(r.Context(), owner, key, result.Size); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "not enough space left in your vault")
		return
	}

	encVer, encWrap, encErr := parseEncHeaders(r)
	if encErr != nil {
		writeError(w, http.StatusBadRequest, "encrypted upload is missing its key wrap")
		return
	}

	obj, err := h.catalog.UpsertObjectEnc(r.Context(), owner, key, result.SHA256, result.Size, encVer, encWrap)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save file name")
		return
	}

	if encVer == 0 && thumb.LooksLikeImage(key) {
		_ = h.thumbs.Ensure(r.Context(), h.blobs, result.SHA256)
	}

	writeJSON(w, http.StatusCreated, fileRowJSON(*obj, obj.Key))
}

func (h objectHandlers) get(w http.ResponseWriter, r *http.Request) {
	raw := chi.URLParam(r, "*")
	if _, ok := normalizeKey(raw); !ok {
		writeError(w, http.StatusBadRequest, "bad file name")
		return
	}

	obj, err := h.objectByKey(r.Context(), userIDFrom(r.Context()), raw)
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
	items, err := h.catalog.ListPersonalObjects(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list files")
		return
	}
	out := make([]fileJSON, 0, len(items))
	for _, o := range items {
		out = append(out, fileRowJSON(o, o.Key))
	}
	used, err := h.catalog.UsageBytes(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not measure usage")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"files": out,
		"usage": map[string]int64{
			"used":       used,
			"limit":      h.maxStorage,
			"max_upload": h.maxUpload,
			"chunk":      h.chunkBytes(),
		},
	})
}

func (h objectHandlers) ensureQuota(ctx context.Context, ownerID, key string, incoming int64) error {
	used, err := h.catalog.UsageBytes(ctx, ownerID)
	if err != nil {
		return err
	}
	projected := used + incoming
	if existing, err := h.catalog.ObjectByKey(ctx, ownerID, key); err == nil {
		projected = used - existing.Size + incoming
	}
	if projected > h.maxStorage {
		return catalog.ErrQuotaExceeded
	}
	return nil
}

func (h objectHandlers) deleteFolder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Prefix string `json:"prefix"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "send the folder name as JSON")
		return
	}
	prefix, ok := normalizeKey(req.Prefix)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad folder name")
		return
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	n, err := h.catalog.DeletePrefix(r.Context(), userIDFrom(r.Context()), prefix)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete folder")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}

func (h objectHandlers) del(w http.ResponseWriter, r *http.Request) {
	raw := chi.URLParam(r, "*")
	if _, ok := normalizeKey(raw); !ok {
		writeError(w, http.StatusBadRequest, "bad file name")
		return
	}
	obj, err := h.objectByKey(r.Context(), userIDFrom(r.Context()), raw)
	if errors.Is(err, catalog.ErrObjectNotFound) {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete")
		return
	}
	err = h.catalog.DeleteObject(r.Context(), userIDFrom(r.Context()), obj.Key)
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
	raw := chi.URLParam(r, "*")
	if _, ok := normalizeKey(raw); !ok {
		writeError(w, http.StatusBadRequest, "bad file name")
		return
	}

	obj, err := h.objectByKey(r.Context(), userIDFrom(r.Context()), raw)
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
