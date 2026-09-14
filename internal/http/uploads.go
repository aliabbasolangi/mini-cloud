package http

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"minicloud/internal/catalog"
	"minicloud/internal/thumb"
)

func (h objectHandlers) chunkBytes() int64 {
	const eight = 8 << 20
	if h.maxUpload > 0 && h.maxUpload < eight {
		return h.maxUpload
	}
	return eight
}

func (h objectHandlers) sessionPath(id string) string {
	return filepath.Join(h.uploadDir, id+".bin")
}

func (h objectHandlers) startUpload(w http.ResponseWriter, r *http.Request) {
	limitBody(w, r, 1<<16)
	var req struct {
		Key      string `json:"key"`
		Size     int64  `json:"size"`
		EncVer   int    `json:"enc_ver"`
		EncWrap  string `json:"enc_wrap"`
		FolderID string `json:"folder_id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "send key and size as JSON")
		return
	}
	key, ok := normalizeKey(req.Key)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad file name")
		return
	}
	if req.Size < 1 || req.Size > h.maxStorage {
		writeError(w, http.StatusRequestEntityTooLarge, "file is too large for this vault")
		return
	}

	owner := userIDFrom(r.Context())
	folderID := strings.TrimSpace(req.FolderID)
	storeOwner := owner
	if folderID != "" {
		folder, err := h.catalog.CollabAccess(r.Context(), owner, folderID)
		if errors.Is(err, catalog.ErrCollabNotFound) || errors.Is(err, catalog.ErrCollabDenied) {
			writeError(w, http.StatusNotFound, "shared folder not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not open shared folder")
			return
		}
		if !catalog.CanCollabWrite(folder.Role) {
			writeError(w, http.StatusForbidden, "viewers can look, but they cannot upload")
			return
		}
		storeOwner = folder.OwnerID
		key = strings.TrimPrefix(key, "/")
		req.EncVer = 0
		req.EncWrap = ""
	} else if catalog.IsCollabKey(key) {
		writeError(w, http.StatusBadRequest, "bad file name")
		return
	}

	objectKey := key
	if folderID != "" {
		objectKey = catalog.CollabObjectKey(folderID, key)
	}
	if err := h.ensureQuota(r.Context(), storeOwner, objectKey, req.Size); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "not enough space left in the vault")
		return
	}

	if req.EncVer == 1 || req.EncVer == 2 {
		if strings.TrimSpace(req.EncWrap) == "" {
			writeError(w, http.StatusBadRequest, "encrypted upload is missing its key wrap")
			return
		}
	} else {
		req.EncVer = 0
		req.EncWrap = ""
	}

	sess, err := h.catalog.CreateUploadSession(r.Context(), owner, folderID, key, req.Size, req.EncVer, req.EncWrap)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start upload")
		return
	}
	if err := os.MkdirAll(h.uploadDir, 0o755); err != nil {
		_ = h.catalog.DeleteUploadSession(r.Context(), sess.ID, owner)
		writeError(w, http.StatusInternalServerError, "could not start upload")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         sess.ID,
		"chunk_size": h.chunkBytes(),
		"size":       sess.Size,
	})
}

func (h objectHandlers) putUploadPart(w http.ResponseWriter, r *http.Request) {
	owner := userIDFrom(r.Context())
	sess, err := h.catalog.UploadSession(r.Context(), chi.URLParam(r, "id"), owner)
	if errors.Is(err, catalog.ErrUploadNotFound) || errors.Is(err, catalog.ErrUploadDenied) {
		writeError(w, http.StatusNotFound, "upload not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not open upload")
		return
	}
	want, err := strconv.Atoi(chi.URLParam(r, "n"))
	if err != nil || want != sess.Parts {
		writeError(w, http.StatusBadRequest, "send the next chunk in order")
		return
	}

	limitBody(w, r, h.maxUpload+4096)
	left := sess.Size - sess.Received
	if left < 1 {
		writeError(w, http.StatusBadRequest, "this upload is already full")
		return
	}
	if err := os.MkdirAll(h.uploadDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "could not store chunk")
		return
	}
	f, err := os.OpenFile(h.sessionPath(sess.ID), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not store chunk")
		return
	}
	n, copyErr := io.Copy(f, io.LimitReader(r.Body, left))
	_ = f.Close()
	if copyErr != nil {
		var maxErr *http.MaxBytesError
		if errors.As(copyErr, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "file is too large")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not store chunk")
		return
	}
	if n < 1 {
		writeError(w, http.StatusBadRequest, "empty chunk")
		return
	}
	updated, err := h.catalog.AddUploadBytes(r.Context(), sess.ID, owner, n)
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not accept that chunk")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"received": updated.Received,
		"size":     updated.Size,
		"parts":    updated.Parts,
	})
}

func (h objectHandlers) completeUpload(w http.ResponseWriter, r *http.Request) {
	owner := userIDFrom(r.Context())
	sess, err := h.catalog.UploadSession(r.Context(), chi.URLParam(r, "id"), owner)
	if errors.Is(err, catalog.ErrUploadNotFound) || errors.Is(err, catalog.ErrUploadDenied) {
		writeError(w, http.StatusNotFound, "upload not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not finish upload")
		return
	}
	if sess.Received != sess.Size {
		writeError(w, http.StatusBadRequest, "upload is not finished yet")
		return
	}

	storeOwner := owner
	rel := sess.Key
	objectKey := sess.Key
	if sess.FolderID != "" {
		folder, err := h.catalog.CollabAccess(r.Context(), owner, sess.FolderID)
		if err != nil || !catalog.CanCollabWrite(folder.Role) {
			writeError(w, http.StatusForbidden, "shared folder not found")
			return
		}
		storeOwner = folder.OwnerID
		objectKey = catalog.CollabObjectKey(sess.FolderID, sess.Key)
	}

	if err := h.ensureQuota(r.Context(), storeOwner, objectKey, sess.Size); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "not enough space left in the vault")
		return
	}

	result, err := h.blobs.Ingest(r.Context(), h.sessionPath(sess.ID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not store file")
		return
	}
	obj, err := h.catalog.UpsertObjectEnc(r.Context(), storeOwner, objectKey, result.SHA256, result.Size, sess.EncVer, sess.EncWrap)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save file name")
		return
	}
	_ = h.catalog.DeleteUploadSession(r.Context(), sess.ID, owner)
	if sess.EncVer == 0 && thumb.LooksLikeImage(rel) {
		_ = h.thumbs.Ensure(r.Context(), h.blobs, result.SHA256)
	}
	writeJSON(w, http.StatusCreated, fileRowJSON(*obj, rel))
}

func (h objectHandlers) abortUpload(w http.ResponseWriter, r *http.Request) {
	owner := userIDFrom(r.Context())
	id := chi.URLParam(r, "id")
	_ = os.Remove(h.sessionPath(id))
	err := h.catalog.DeleteUploadSession(r.Context(), id, owner)
	if errors.Is(err, catalog.ErrUploadNotFound) || errors.Is(err, catalog.ErrUploadDenied) {
		writeError(w, http.StatusNotFound, "upload not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not cancel upload")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
