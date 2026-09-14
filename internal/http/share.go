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

type shareRequest struct {
	Key      string `json:"key"`
	FolderID string `json:"folder_id"`
	Role     string `json:"role"`
	TTLHours int    `json:"ttl_hours"`
	Never    bool   `json:"never"`
}

func (h objectHandlers) createShare(w http.ResponseWriter, r *http.Request) {
	var req shareRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "send the share options as JSON")
		return
	}
	h.writeCreatedShare(w, r, req)
}

func (h objectHandlers) collabShare(w http.ResponseWriter, r *http.Request) {
	folder := h.requireCollabWrite(w, r)
	if folder == nil {
		return
	}
	var req shareRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "send the share options as JSON")
		return
	}
	req.FolderID = folder.ID
	h.writeCreatedShare(w, r, req)
}

func (h objectHandlers) writeCreatedShare(w http.ResponseWriter, r *http.Request, req shareRequest) {
	actor := userIDFrom(r.Context())
	in := catalog.ShareInput{
		Role:     req.Role,
		TTLHours: req.TTLHours,
		Never:    req.Never,
	}
	ownerID := actor
	var encVer int

	if req.FolderID != "" {
		folder, err := h.catalog.CollabAccess(r.Context(), actor, req.FolderID)
		if errors.Is(err, catalog.ErrCollabNotFound) || errors.Is(err, catalog.ErrCollabDenied) {
			writeError(w, http.StatusNotFound, "shared folder not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not open shared folder")
			return
		}
		if !catalog.CanCollabWrite(folder.Role) {
			writeError(w, http.StatusForbidden, "viewers can look, but they cannot create links")
			return
		}
		ownerID = folder.OwnerID
		key := strings.TrimSpace(req.Key)
		if key == "" {
			in.FolderID = folder.ID
		} else {
			rel, ok := normalizeKey(key)
			if !ok {
				writeError(w, http.StatusBadRequest, "bad file name")
				return
			}
			in.Key = catalog.CollabObjectKey(folder.ID, rel)
		}
	} else {
		key, ok := normalizeKey(req.Key)
		if !ok {
			writeError(w, http.StatusBadRequest, "bad file name")
			return
		}
		in.Key = key
	}

	share, err := h.catalog.CreateShare(r.Context(), ownerID, in)
	if errors.Is(err, catalog.ErrObjectNotFound) {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	if errors.Is(err, catalog.ErrCollabNotFound) {
		writeError(w, http.StatusNotFound, "shared folder not found")
		return
	}
	if errors.Is(err, catalog.ErrBadShareTTL) {
		writeError(w, http.StatusBadRequest, "pick 1 hour, 1 day, 7 days, 30 days, or never")
		return
	}
	if errors.Is(err, catalog.ErrBadCollabRole) {
		writeError(w, http.StatusBadRequest, "anyone with the link must be a viewer or an editor")
		return
	}
	if errors.Is(err, catalog.ErrBadShare) {
		writeError(w, http.StatusBadRequest, "choose a file or a folder")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create link")
		return
	}

	if share.Kind == catalog.ShareKindFile {
		if obj, _ := h.catalog.ObjectByKey(r.Context(), share.OwnerID, share.ObjectKey); obj != nil {
			encVer = obj.EncVer
		}
	}
	writeJSON(w, http.StatusCreated, shareCreatedJSON(r, share, encVer))
}

func shareCreatedJSON(r *http.Request, share *catalog.Share, encVer int) map[string]any {
	out := map[string]any{
		"token":   share.Token,
		"url":     publicShareURL(r, share.Token),
		"kind":    share.Kind,
		"role":    share.Role,
		"never":   share.NeverExpires(),
		"enc_ver": encVer,
	}
	if share.NeverExpires() {
		out["expires"] = nil
	} else {
		out["expires"] = share.ExpiresAt.Format("2006-01-02T15:04:05Z")
	}
	return out
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

func (h objectHandlers) joinShare(w http.ResponseWriter, r *http.Request) {
	folder, err := h.catalog.JoinViaShare(r.Context(), userIDFrom(r.Context()), chi.URLParam(r, "token"))
	if errors.Is(err, catalog.ErrShareExpired) {
		writeError(w, http.StatusGone, "this link has expired")
		return
	}
	if errors.Is(err, catalog.ErrShareNotFolder) {
		writeError(w, http.StatusBadRequest, "this link is for a file")
		return
	}
	if errors.Is(err, catalog.ErrShareNotFound) || errors.Is(err, catalog.ErrCollabNotFound) {
		writeError(w, http.StatusNotFound, "link not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not open this folder")
		return
	}
	writeJSON(w, http.StatusOK, collabFolderJSON(folder))
}

func (h objectHandlers) publicPage(w http.ResponseWriter, r *http.Request) {
	serveHTML("web/share.html")(w, r)
}

func (h objectHandlers) lookupShare(w http.ResponseWriter, r *http.Request) *catalog.Share {
	share, err := h.catalog.ValidShare(r.Context(), chi.URLParam(r, "token"))
	if errors.Is(err, catalog.ErrShareExpired) {
		writeError(w, http.StatusGone, "this link has expired")
		return nil
	}
	if errors.Is(err, catalog.ErrShareNotFound) || err != nil {
		writeError(w, http.StatusNotFound, "link not found")
		return nil
	}
	return share
}

func (h objectHandlers) publicInfo(w http.ResponseWriter, r *http.Request) {
	share := h.lookupShare(w, r)
	if share == nil {
		return
	}
	out := map[string]any{
		"kind":    share.Kind,
		"role":    share.Role,
		"never":   share.NeverExpires(),
		"expires": nil,
	}
	if !share.NeverExpires() {
		out["expires"] = share.ExpiresAt.Format("2006-01-02T15:04:05Z")
	}

	if share.Kind == catalog.ShareKindFolder {
		folder, err := h.catalog.CollabFolderByID(r.Context(), share.FolderID)
		if errors.Is(err, catalog.ErrCollabNotFound) {
			writeError(w, http.StatusNotFound, "folder is gone")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not open folder")
			return
		}
		prefix := catalog.CollabKeyPrefix + folder.ID + "/"
		items, err := h.catalog.ListObjectsByPrefix(r.Context(), folder.OwnerID, prefix)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not list files")
			return
		}
		markers, err := h.catalog.ListCollabMarkers(r.Context(), folder.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not list folders")
			return
		}
		files := make([]map[string]any, 0, len(items))
		for _, o := range items {
			rel := strings.TrimPrefix(o.Key, prefix)
			if rel == "" || rel == o.Key {
				continue
			}
			files = append(files, map[string]any{
				"key":     rel,
				"size":    o.Size,
				"created": o.CreatedAt.Format("2006-01-02T15:04:05Z"),
			})
		}
		out["name"] = folder.Name
		out["files"] = files
		out["prefixes"] = markers
		writeJSON(w, http.StatusOK, out)
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
	out["name"] = path.Base(obj.Key)
	out["size"] = obj.Size
	out["enc_ver"] = obj.EncVer
	writeJSON(w, http.StatusOK, out)
}

func (h objectHandlers) publicGet(w http.ResponseWriter, r *http.Request) {
	share := h.lookupShare(w, r)
	if share == nil {
		return
	}
	if share.Kind == catalog.ShareKindFolder {
		writeError(w, http.StatusBadRequest, "this link is a folder")
		return
	}
	h.serveSharedObject(w, r, share.OwnerID, share.ObjectKey)
}

func (h objectHandlers) publicFolderGet(w http.ResponseWriter, r *http.Request) {
	share := h.lookupShare(w, r)
	if share == nil {
		return
	}
	if share.Kind != catalog.ShareKindFolder || share.FolderID == "" {
		writeError(w, http.StatusNotFound, "link not found")
		return
	}
	rel, ok := normalizeKey(chi.URLParam(r, "*"))
	if !ok {
		writeError(w, http.StatusBadRequest, "bad file name")
		return
	}
	h.serveSharedObject(w, r, share.OwnerID, catalog.CollabObjectKey(share.FolderID, rel))
}

func (h objectHandlers) serveSharedObject(w http.ResponseWriter, r *http.Request, ownerID, key string) {
	obj, err := h.catalog.ObjectByKey(r.Context(), ownerID, key)
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
