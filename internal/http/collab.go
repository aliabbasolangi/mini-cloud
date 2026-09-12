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

func (h objectHandlers) requireCollab(w http.ResponseWriter, r *http.Request) *catalog.CollabFolder {
	id := chi.URLParam(r, "id")
	folder, err := h.catalog.CollabAccess(r.Context(), userIDFrom(r.Context()), id)
	if errors.Is(err, catalog.ErrCollabNotFound) || errors.Is(err, catalog.ErrCollabDenied) {
		writeError(w, http.StatusNotFound, "shared folder not found")
		return nil
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not open shared folder")
		return nil
	}
	return folder
}

func (h objectHandlers) createCollabFolder(w http.ResponseWriter, r *http.Request) {
	limitBody(w, r, 1<<16)
	var req struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "send a folder name as JSON")
		return
	}
	folder, err := h.catalog.CreateCollabFolder(r.Context(), userIDFrom(r.Context()), req.Name)
	if errors.Is(err, catalog.ErrCollabName) {
		writeError(w, http.StatusBadRequest, "use a simple folder name, no slashes")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create shared folder")
		return
	}
	writeJSON(w, http.StatusCreated, collabFolderJSON(folder))
}

func (h objectHandlers) listCollabFolders(w http.ResponseWriter, r *http.Request) {
	folders, err := h.catalog.ListCollabFolders(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list shared folders")
		return
	}
	out := make([]map[string]any, 0, len(folders))
	for i := range folders {
		out = append(out, collabFolderJSON(&folders[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": out})
}

func (h objectHandlers) deleteCollabFolder(w http.ResponseWriter, r *http.Request) {
	err := h.catalog.DeleteCollabFolder(r.Context(), userIDFrom(r.Context()), chi.URLParam(r, "id"))
	if errors.Is(err, catalog.ErrCollabNotFound) {
		writeError(w, http.StatusNotFound, "shared folder not found")
		return
	}
	if errors.Is(err, catalog.ErrCollabDenied) {
		writeError(w, http.StatusForbidden, "only the owner can delete this shared folder")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete shared folder")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h objectHandlers) leaveCollabFolder(w http.ResponseWriter, r *http.Request) {
	err := h.catalog.LeaveCollab(r.Context(), userIDFrom(r.Context()), chi.URLParam(r, "id"))
	if errors.Is(err, catalog.ErrCollabNotFound) || errors.Is(err, catalog.ErrCollabDenied) {
		writeError(w, http.StatusForbidden, "you cannot leave a folder you own")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not leave shared folder")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h objectHandlers) inviteCollab(w http.ResponseWriter, r *http.Request) {
	limitBody(w, r, 1<<16)
	if h.requireCollab(w, r) == nil {
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "send an email as JSON")
		return
	}
	inv, err := h.catalog.InviteToFolder(r.Context(), userIDFrom(r.Context()), chi.URLParam(r, "id"), req.Email)
	switch {
	case errors.Is(err, catalog.ErrCannotInviteSelf), errors.Is(err, catalog.ErrAlreadyMember):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, catalog.ErrUserNotFound):
		writeError(w, http.StatusBadRequest, "need a real email")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "could not send invite")
	default:
		writeJSON(w, http.StatusCreated, map[string]string{
			"id":      inv.ID,
			"email":   inv.Email,
			"folder":  inv.FolderName,
			"message": "They will see the invite the next time they open SafeKeeping.",
		})
	}
}

func (h objectHandlers) listCollabInvites(w http.ResponseWriter, r *http.Request) {
	me, err := h.catalog.UserByID(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load invites")
		return
	}
	invites, err := h.catalog.PendingInvites(r.Context(), me.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load invites")
		return
	}
	out := make([]map[string]string, 0, len(invites))
	for _, inv := range invites {
		out = append(out, map[string]string{
			"id":           inv.ID,
			"folder_id":    inv.FolderID,
			"folder_name":  inv.FolderName,
			"invited_by":   inv.InviterName,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"invites": out})
}

func (h objectHandlers) acceptCollabInvite(w http.ResponseWriter, r *http.Request) {
	me, err := h.catalog.UserByID(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not accept invite")
		return
	}
	folder, err := h.catalog.AcceptInvite(r.Context(), chi.URLParam(r, "id"), me.ID, me.Email)
	if errors.Is(err, catalog.ErrInviteNotFound) || errors.Is(err, catalog.ErrInviteWrongEmail) {
		writeError(w, http.StatusNotFound, "invite not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not accept invite")
		return
	}
	writeJSON(w, http.StatusOK, collabFolderJSON(folder))
}

func (h objectHandlers) declineCollabInvite(w http.ResponseWriter, r *http.Request) {
	me, err := h.catalog.UserByID(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not decline invite")
		return
	}
	err = h.catalog.DeclineInvite(r.Context(), chi.URLParam(r, "id"), me.Email)
	if errors.Is(err, catalog.ErrInviteNotFound) {
		writeError(w, http.StatusNotFound, "invite not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not decline invite")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h objectHandlers) listCollabMembers(w http.ResponseWriter, r *http.Request) {
	if h.requireCollab(w, r) == nil {
		return
	}
	members, err := h.catalog.ListCollabMembers(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list people")
		return
	}
	out := make([]map[string]any, 0, len(members))
	for _, m := range members {
		name := m.DisplayName
		if name == "" {
			name = m.Email
		}
		out = append(out, map[string]any{
			"user_id":      m.UserID,
			"email":        m.Email,
			"display_name": name,
			"is_owner":     m.IsOwner,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": out})
}

func (h objectHandlers) listCollabObjects(w http.ResponseWriter, r *http.Request) {
	folder := h.requireCollab(w, r)
	if folder == nil {
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
	out := make([]fileJSON, 0, len(items))
	for _, o := range items {
		rel := strings.TrimPrefix(o.Key, prefix)
		if rel == "" || rel == o.Key {
			continue
		}
		out = append(out, fileRowJSON(o, rel))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"folder":   collabFolderJSON(folder),
		"files":    out,
		"prefixes": markers,
	})
}

func (h objectHandlers) collabPut(w http.ResponseWriter, r *http.Request) {
	folder := h.requireCollab(w, r)
	if folder == nil {
		return
	}
	rel, ok := normalizeKey(chi.URLParam(r, "*"))
	if !ok {
		writeError(w, http.StatusBadRequest, "bad file name")
		return
	}
	key := catalog.CollabObjectKey(folder.ID, rel)

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
	if err := h.ensureQuota(r.Context(), folder.OwnerID, key, result.Size); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "not enough space left in the owner's vault")
		return
	}
	encVer, encWrap, encErr := parseEncHeaders(r)
	if encErr != nil {
		writeError(w, http.StatusBadRequest, "encrypted upload is missing its key wrap")
		return
	}
	obj, err := h.catalog.UpsertObjectEnc(r.Context(), folder.OwnerID, key, result.SHA256, result.Size, encVer, encWrap)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save file name")
		return
	}
	if encVer == 0 && thumb.LooksLikeImage(rel) {
		_ = h.thumbs.Ensure(r.Context(), h.blobs, result.SHA256)
	}
	writeJSON(w, http.StatusCreated, fileRowJSON(*obj, rel))
}

func (h objectHandlers) collabGet(w http.ResponseWriter, r *http.Request) {
	folder := h.requireCollab(w, r)
	if folder == nil {
		return
	}
	rel, ok := normalizeKey(chi.URLParam(r, "*"))
	if !ok {
		writeError(w, http.StatusBadRequest, "bad file name")
		return
	}
	obj, err := h.objectByKey(r.Context(), folder.OwnerID, catalog.CollabObjectKey(folder.ID, rel))
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
	w.Header().Set("Content-Disposition", `attachment; filename="`+path.Base(rel)+`"`)
	_, _ = io.Copy(w, f)
}

func (h objectHandlers) collabDel(w http.ResponseWriter, r *http.Request) {
	folder := h.requireCollab(w, r)
	if folder == nil {
		return
	}
	rel, ok := normalizeKey(chi.URLParam(r, "*"))
	if !ok {
		writeError(w, http.StatusBadRequest, "bad file name")
		return
	}
	obj, err := h.objectByKey(r.Context(), folder.OwnerID, catalog.CollabObjectKey(folder.ID, rel))
	if errors.Is(err, catalog.ErrObjectNotFound) {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete")
		return
	}
	if err := h.catalog.DeleteObject(r.Context(), folder.OwnerID, obj.Key); err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h objectHandlers) collabPreview(w http.ResponseWriter, r *http.Request) {
	folder := h.requireCollab(w, r)
	if folder == nil {
		return
	}
	rel, ok := normalizeKey(chi.URLParam(r, "*"))
	if !ok {
		writeError(w, http.StatusBadRequest, "bad file name")
		return
	}
	obj, err := h.objectByKey(r.Context(), folder.OwnerID, catalog.CollabObjectKey(folder.ID, rel))
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

func (h objectHandlers) collabMove(w http.ResponseWriter, r *http.Request) {
	folder := h.requireCollab(w, r)
	if folder == nil {
		return
	}
	var req struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "send from and to as JSON")
		return
	}
	fromRel, ok := normalizeKey(req.From)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad source name")
		return
	}
	toRel, ok := normalizeKey(req.To)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad destination name")
		return
	}
	err := h.catalog.RenameObject(r.Context(), folder.OwnerID, catalog.CollabObjectKey(folder.ID, fromRel), catalog.CollabObjectKey(folder.ID, toRel))
	if errors.Is(err, catalog.ErrObjectNotFound) {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	if errors.Is(err, catalog.ErrKeyTaken) {
		writeError(w, http.StatusConflict, "a file with that name already exists there")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not move file")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"from": fromRel, "to": toRel})
}

func (h objectHandlers) collabMkdir(w http.ResponseWriter, r *http.Request) {
	folder := h.requireCollab(w, r)
	if folder == nil {
		return
	}
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
	if err := h.catalog.AddCollabMarker(r.Context(), folder.ID, prefix); err != nil {
		writeError(w, http.StatusBadRequest, "bad folder name")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"prefix": prefix})
}

func (h objectHandlers) collabRmDir(w http.ResponseWriter, r *http.Request) {
	folder := h.requireCollab(w, r)
	if folder == nil {
		return
	}
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
	n, err := h.catalog.DeletePrefix(r.Context(), folder.OwnerID, catalog.CollabKeyPrefix+folder.ID+"/"+prefix)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete folder")
		return
	}
	_ = h.catalog.DeleteCollabMarkers(r.Context(), folder.ID, prefix)
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}

func (h objectHandlers) collabShare(w http.ResponseWriter, r *http.Request) {
	folder := h.requireCollab(w, r)
	if folder == nil {
		return
	}
	var req struct {
		Key string `json:"key"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "send the file name as JSON")
		return
	}
	rel, ok := normalizeKey(req.Key)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad file name")
		return
	}
	share, err := h.catalog.CreateShare(r.Context(), folder.OwnerID, catalog.CollabObjectKey(folder.ID, rel))
	if errors.Is(err, catalog.ErrObjectNotFound) {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create link")
		return
	}
	obj, _ := h.catalog.ObjectByKey(r.Context(), folder.OwnerID, catalog.CollabObjectKey(folder.ID, rel))
	encVer := 0
	if obj != nil {
		encVer = obj.EncVer
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"token":   share.Token,
		"url":     publicShareURL(r, share.Token),
		"expires": share.ExpiresAt.Format("2006-01-02T15:04:05Z"),
		"enc_ver": encVer,
	})
}

func collabFolderJSON(f *catalog.CollabFolder) map[string]any {
	return map[string]any{
		"id":          f.ID,
		"name":        f.Name,
		"role":        f.Role,
		"owner_email": f.OwnerEmail,
	}
}
