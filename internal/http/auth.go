package http

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"minicloud/internal/auth"
	"minicloud/internal/catalog"
)

type authHandlers struct {
	catalog *catalog.DB
	secret  string
}

type authRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h authHandlers) register(w http.ResponseWriter, r *http.Request) {
	limitBody(w, r, 1<<16)
	var req authRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "send email and password as JSON")
		return
	}
	email := strings.TrimSpace(req.Email)
	if !strings.Contains(email, "@") || len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "need a real email and a password of at least 8 characters")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save password")
		return
	}
	user, err := h.catalog.CreateUser(r.Context(), email, hash)
	if errors.Is(err, catalog.ErrEmailTaken) {
		writeError(w, http.StatusConflict, "that email is already registered")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create account")
		return
	}
	h.respondToken(w, user, http.StatusCreated)
}

func (h authHandlers) login(w http.ResponseWriter, r *http.Request) {
	limitBody(w, r, 1<<16)
	var req authRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "send email and password as JSON")
		return
	}
	user, err := h.catalog.UserByEmail(r.Context(), req.Email)
	if errors.Is(err, catalog.ErrUserNotFound) || (user != nil && !auth.CheckPassword(user.PasswordHash, req.Password)) {
		writeError(w, http.StatusUnauthorized, "email or password is wrong")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not log in")
		return
	}
	h.respondToken(w, user, http.StatusOK)
}

func (h authHandlers) respondToken(w http.ResponseWriter, user *catalog.User, status int) {
	token, err := auth.IssueToken(h.secret, user.ID, 7*24*time.Hour)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create session")
		return
	}
	writeJSON(w, status, map[string]string{
		"token": token,
		"email": user.Email,
	})
}
