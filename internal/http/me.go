package http

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"

	"minicloud/internal/avatar"
	"minicloud/internal/catalog"
)

func (h authHandlers) profileJSON(ctx context.Context, u *catalog.User) map[string]any {
	unread := 0
	if h.catalog != nil {
		unread, _ = h.catalog.UnreadNotificationCount(ctx, u.ID)
	}
	return map[string]any{
		"email":         u.Email,
		"display_name":  u.DisplayName,
		"theme":         u.Theme,
		"accent":        u.Accent,
		"has_avatar":    h.avatars != nil && h.avatars.Exists(u.ID),
		"unread_count":  unread,
		"vault_salt":    u.VaultSalt,
		"vault_wrap":    u.VaultWrap,
	}
}

func (h authHandlers) putVault(w http.ResponseWriter, r *http.Request) {
	limitBody(w, r, 1<<16)
	var req struct {
		Salt string `json:"vault_salt"`
		Wrap string `json:"vault_wrap"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "send vault_salt and vault_wrap as JSON")
		return
	}
	user, err := h.catalog.SetVaultIfEmpty(r.Context(), userIDFrom(r.Context()), req.Salt, req.Wrap)
	if errors.Is(err, catalog.ErrBadProfile) {
		writeError(w, http.StatusBadRequest, "vault wrap looks invalid")
		return
	}
	if errors.Is(err, catalog.ErrUserNotFound) {
		writeError(w, http.StatusUnauthorized, "session expired, log in again")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save vault key")
		return
	}
	writeJSON(w, http.StatusOK, h.profileJSON(r.Context(), user))
}

func (h authHandlers) me(w http.ResponseWriter, r *http.Request) {
	user, err := h.catalog.UserByID(r.Context(), userIDFrom(r.Context()))
	if errors.Is(err, catalog.ErrUserNotFound) {
		writeError(w, http.StatusUnauthorized, "session expired, log in again")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load profile")
		return
	}
	writeJSON(w, http.StatusOK, h.profileJSON(r.Context(), user))
}

func (h authHandlers) updateMe(w http.ResponseWriter, r *http.Request) {
	limitBody(w, r, 1<<16)
	var req struct {
		DisplayName string `json:"display_name"`
		Theme       string `json:"theme"`
		Accent      string `json:"accent"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "send display_name, theme, and accent as JSON")
		return
	}
	user, err := h.catalog.UpdateProfile(r.Context(), userIDFrom(r.Context()), req.DisplayName, req.Theme, req.Accent)
	if errors.Is(err, catalog.ErrBadProfile) {
		writeError(w, http.StatusBadRequest, "use a short name, dark or light, and a #rrggbb color")
		return
	}
	if errors.Is(err, catalog.ErrUserNotFound) {
		writeError(w, http.StatusUnauthorized, "session expired, log in again")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save profile")
		return
	}
	writeJSON(w, http.StatusOK, h.profileJSON(r.Context(), user))
}

func (h authHandlers) putAvatar(w http.ResponseWriter, r *http.Request) {
	limitBody(w, r, 4<<20)
	if err := h.avatars.Save(userIDFrom(r.Context()), r.Body); err != nil {
		if errors.Is(err, avatar.ErrNotImage) {
			writeError(w, http.StatusBadRequest, "choose a photo (png, jpg, gif, or webp)")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not save photo")
		return
	}
	user, err := h.catalog.UserByID(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, http.StatusOK, "photo saved")
		return
	}
	writeJSON(w, http.StatusOK, h.profileJSON(r.Context(), user))
}

func (h authHandlers) getAvatar(w http.ResponseWriter, r *http.Request) {
	f, err := h.avatars.Open(userIDFrom(r.Context()))
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "no photo")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read photo")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=60")
	_, _ = io.Copy(w, f)
}

func (h authHandlers) deleteMe(w http.ResponseWriter, r *http.Request) {
	id := userIDFrom(r.Context())
	if h.avatars != nil {
		_ = h.avatars.Remove(id)
	}
	if err := h.catalog.DeleteUser(r.Context(), id); err != nil {
		if errors.Is(err, catalog.ErrUserNotFound) {
			writeError(w, http.StatusNotFound, "account not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not delete account")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h authHandlers) listNotifications(w http.ResponseWriter, r *http.Request) {
	id := userIDFrom(r.Context())
	me, err := h.catalog.UserByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session expired, log in again")
		return
	}
	_ = h.catalog.EnsureInviteNotifications(r.Context(), me)
	items, err := h.catalog.ListNotifications(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load notifications")
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, n := range items {
		out = append(out, map[string]any{
			"id":         n.ID,
			"kind":       n.Kind,
			"title":      n.Title,
			"body":       n.Body,
			"invite_id":  n.InviteID,
			"folder_id":  n.FolderID,
			"unread":     n.Unread,
			"created":    n.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	unread, _ := h.catalog.UnreadNotificationCount(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{"notifications": out, "unread_count": unread})
}

func (h authHandlers) readNotification(w http.ResponseWriter, r *http.Request) {
	if err := h.catalog.MarkNotificationRead(r.Context(), userIDFrom(r.Context()), chi.URLParam(r, "id")); err != nil {
		writeError(w, http.StatusInternalServerError, "could not update notification")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h authHandlers) readAllNotifications(w http.ResponseWriter, r *http.Request) {
	if err := h.catalog.MarkAllNotificationsRead(r.Context(), userIDFrom(r.Context())); err != nil {
		writeError(w, http.StatusInternalServerError, "could not update notifications")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h authHandlers) deleteAvatar(w http.ResponseWriter, r *http.Request) {
	if err := h.avatars.Remove(userIDFrom(r.Context())); err != nil {
		writeError(w, http.StatusInternalServerError, "could not remove photo")
		return
	}
	user, err := h.catalog.UserByID(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, h.profileJSON(r.Context(), user))
}
