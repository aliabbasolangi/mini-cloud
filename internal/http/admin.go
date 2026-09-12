package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"minicloud/internal/catalog"
)

func (h authHandlers) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.catalog.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list accounts")
		return
	}
	pending, _ := h.catalog.PendingCount(r.Context())
	out := make([]map[string]any, 0, len(users))
	for _, u := range users {
		out = append(out, map[string]any{
			"id":           u.ID,
			"email":        u.Email,
			"display_name": u.DisplayName,
			"approved":     u.Approved,
			"is_admin":     u.IsAdmin,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"users":   out,
		"pending": pending,
	})
}

func (h authHandlers) approveUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	user, err := h.catalog.SetApproved(r.Context(), id, true)
	if err == catalog.ErrUserNotFound {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not approve this account")
		return
	}
	_ = h.catalog.AddNotification(r.Context(), catalog.Notification{
		UserID: user.ID,
		Kind:   catalog.NotifApproval,
		Title:  "You're in",
		Body:   "The owner approved your SafeKeeping account. You can upload files now.",
		Unread: true,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"id":       user.ID,
		"approved": user.Approved,
	})
}

func (h authHandlers) denyUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	user, err := h.catalog.UserByID(r.Context(), id)
	if err == catalog.ErrUserNotFound {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not look up this account")
		return
	}
	if user.IsAdmin || user.ID == userIDFrom(r.Context()) {
		writeError(w, http.StatusBadRequest, "you cannot remove the owner account")
		return
	}
	if h.avatars != nil {
		_ = h.avatars.Remove(id)
	}
	if err := h.catalog.DeleteUser(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "could not remove this account")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}
