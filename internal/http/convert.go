package http

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	"minicloud/internal/blob"
	"minicloud/internal/catalog"
	"minicloud/internal/convert"
)

func (h objectHandlers) convert(w http.ResponseWriter, r *http.Request) {
	limitBody(w, r, 1<<16)
	var req struct {
		Key      string `json:"key"`
		To       string `json:"to"`
		SaveAs   string `json:"save_as"`
		FolderID string `json:"folder_id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "send key and to as JSON")
		return
	}
	srcKey, ok := normalizeKey(req.Key)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad file name")
		return
	}
	to := convert.NormalizeTo(req.To)
	if to == "" {
		writeError(w, http.StatusBadRequest, "say what format to convert to")
		return
	}

	owner := userIDFrom(r.Context())
	storeOwner := owner
	srcStore := srcKey
	if id := strings.TrimSpace(req.FolderID); id != "" {
		folder, err := h.catalog.CollabAccess(r.Context(), owner, id)
		if errors.Is(err, catalog.ErrCollabNotFound) || errors.Is(err, catalog.ErrCollabDenied) {
			writeError(w, http.StatusNotFound, "shared folder not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not open shared folder")
			return
		}
		storeOwner = folder.OwnerID
		srcStore = catalog.CollabObjectKey(id, srcKey)
	}
	obj, err := h.objectByKey(r.Context(), storeOwner, srcStore)
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
	src, err := io.ReadAll(io.LimitReader(f, h.maxUpload+1))
	_ = f.Close()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read file")
		return
	}
	if int64(len(src)) > h.maxUpload {
		writeError(w, http.StatusRequestEntityTooLarge, "file is too large to convert")
		return
	}

	out, err := convert.Convert(src, obj.Key, to)
	if errors.Is(err, convert.ErrUnsupported) {
		writeError(w, http.StatusBadRequest, "that conversion is not supported yet — images, text, Word, and PDF work")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not convert this file")
		return
	}

	saveAs := strings.TrimSpace(req.SaveAs)
	if saveAs == "" {
		rel := srcKey
		if strings.TrimSpace(req.FolderID) != "" {
			rel = srcKey
		}
		dir := path.Dir(rel)
		name := out.Name
		if dir != "." && dir != "" {
			saveAs = dir + "/" + name
		} else {
			saveAs = name
		}
	}
	saveRel, ok := normalizeKey(saveAs)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad output file name")
		return
	}
	saveStore := saveRel
	if id := strings.TrimSpace(req.FolderID); id != "" {
		saveStore = catalog.CollabObjectKey(id, saveRel)
	}

	if err := h.ensureQuota(r.Context(), storeOwner, saveStore, int64(len(out.Bytes))); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "not enough space left in the owner's vault")
		return
	}

	stored, err := h.blobs.Put(r.Context(), bytes.NewReader(out.Bytes))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not store converted file")
		return
	}
	saved, err := h.catalog.UpsertObject(r.Context(), storeOwner, saveStore, stored.SHA256, stored.Size)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save converted file")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"key":     saveRel,
		"size":    saved.Size,
		"from":    srcKey,
		"to":      to,
		"created": saved.CreatedAt,
	})
}

func (h objectHandlers) convertRaw(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(h.maxUpload + 1<<20); err != nil {
		writeError(w, http.StatusBadRequest, "send the file as multipart form data")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "attach the file to convert")
		return
	}
	defer file.Close()

	src, err := io.ReadAll(io.LimitReader(file, h.maxUpload+1))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read file")
		return
	}
	if int64(len(src)) > h.maxUpload {
		writeError(w, http.StatusRequestEntityTooLarge, "file is too large to convert")
		return
	}

	to := convert.NormalizeTo(r.FormValue("to"))
	if to == "" {
		writeError(w, http.StatusBadRequest, "say what format to convert to")
		return
	}
	srcName := strings.TrimSpace(r.FormValue("name"))
	if srcName == "" && header != nil {
		srcName = header.Filename
	}
	if srcName == "" {
		srcName = "file"
	}

	out, err := convert.Convert(src, srcName, to)
	if errors.Is(err, convert.ErrUnsupported) {
		writeError(w, http.StatusBadRequest, "that conversion is not supported yet — images, text, Word, and PDF work")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not convert this file")
		return
	}

	ctype := out.ContentType
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, out.Name))
	w.Header().Set("X-Converted-Name", out.Name)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out.Bytes)
}
