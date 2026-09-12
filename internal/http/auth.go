package http

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"minicloud/internal/auth"
	"minicloud/internal/avatar"
	"minicloud/internal/catalog"
	"minicloud/internal/mail"
)

type authHandlers struct {
	catalog *catalog.DB
	avatars *avatar.Store
	secret  string
	mail    mail.Mailer
	now     func() time.Time
}

type authRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Code     string `json:"code"`
	Purpose  string `json:"purpose"`
}

var emailRE = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

func (h authHandlers) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now()
}

func (h authHandlers) registerStart(w http.ResponseWriter, r *http.Request) {
	limitBody(w, r, 1<<16)
	var req authRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "send email and password as JSON")
		return
	}
	email, password, ok := normalizeCreds(req.Email, req.Password)
	if !ok {
		writeError(w, http.StatusBadRequest, "need a real email and a password of at least 8 characters")
		return
	}

	_, err := h.catalog.UserByEmail(r.Context(), email)
	if err == nil {
		writeError(w, http.StatusConflict, "that email is already registered")
		return
	}
	if !errors.Is(err, catalog.ErrUserNotFound) {
		writeError(w, http.StatusInternalServerError, "could not start registration")
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save password")
		return
	}
	if err := h.issueCode(w, r, email, catalog.PurposeRegister, hash); err != nil {
		return
	}
	h.writeCodeSent(w, "We sent a 6-digit code to confirm this email.")
}

func (h authHandlers) registerVerify(w http.ResponseWriter, r *http.Request) {
	limitBody(w, r, 1<<16)
	var req authRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "send email and code as JSON")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	extra, ok := h.consumeCode(w, r, email, catalog.PurposeRegister, req.Code)
	if !ok {
		return
	}
	if extra == "" {
		writeError(w, http.StatusBadRequest, "start registration again")
		return
	}

	user, err := h.catalog.CreateUser(r.Context(), email, extra)
	if errors.Is(err, catalog.ErrEmailTaken) {
		writeError(w, http.StatusConflict, "that email is already registered")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create account")
		return
	}
	_ = h.catalog.EnsureInviteNotifications(r.Context(), user)
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
	_ = h.catalog.EnsureInviteNotifications(r.Context(), user)
	h.respondToken(w, user, http.StatusOK)
}

func (h authHandlers) forgot(w http.ResponseWriter, r *http.Request) {
	limitBody(w, r, 1<<16)
	var req authRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "send email as JSON")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !emailRE.MatchString(email) {
		writeError(w, http.StatusBadRequest, "need a real email")
		return
	}

	user, err := h.catalog.UserByEmail(r.Context(), email)
	if errors.Is(err, catalog.ErrUserNotFound) {
		h.writeCodeSent(w, "If that email has an account, we sent a reset code.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start password reset")
		return
	}
	if err := h.issueCode(w, r, user.Email, catalog.PurposeReset, ""); err != nil {
		return
	}
	h.writeCodeSent(w, "If that email has an account, we sent a reset code.")
}

func (h authHandlers) reset(w http.ResponseWriter, r *http.Request) {
	limitBody(w, r, 1<<16)
	var req authRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "send email, code, and new password as JSON")
		return
	}
	email, password, ok := normalizeCreds(req.Email, req.Password)
	if !ok {
		writeError(w, http.StatusBadRequest, "need a real email and a password of at least 8 characters")
		return
	}
	if _, ok := h.consumeCode(w, r, email, catalog.PurposeReset, req.Code); !ok {
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save password")
		return
	}
	if err := h.catalog.UpdatePassword(r.Context(), email, hash); err != nil {
		if errors.Is(err, catalog.ErrUserNotFound) {
			writeError(w, http.StatusNotFound, "no account for that email")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not reset password")
		return
	}
	_ = h.catalog.ClearVault(r.Context(), email)
	user, err := h.catalog.UserByEmail(r.Context(), email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "password was reset, but sign-in failed")
		return
	}
	h.respondToken(w, user, http.StatusOK)
}

func (h authHandlers) resend(w http.ResponseWriter, r *http.Request) {
	limitBody(w, r, 1<<16)
	var req authRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "send email and purpose as JSON")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	purpose, err := catalog.NormalizePurpose(req.Purpose)
	if err != nil || !emailRE.MatchString(email) {
		writeError(w, http.StatusBadRequest, "need a real email and a purpose of register or reset")
		return
	}

	extra, err := h.catalog.EmailCodeExtra(r.Context(), email, purpose)
	if errors.Is(err, catalog.ErrCodeNotFound) {
		if purpose == catalog.PurposeReset {
			h.writeCodeSent(w, "If a reset is still pending, we sent another code.")
			return
		}
		writeError(w, http.StatusBadRequest, "start registration again")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not resend code")
		return
	}
	if err := h.issueCode(w, r, email, purpose, extra); err != nil {
		return
	}
	h.writeCodeSent(w, "We sent another code.")
}

func (h authHandlers) issueCode(w http.ResponseWriter, r *http.Request, email, purpose, extra string) error {
	code, err := auth.RandomCode()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create a code")
		return err
	}
	hash := auth.HashCode(h.secret, email, purpose, code)
	if err := h.catalog.PutEmailCode(r.Context(), email, purpose, hash, extra, h.clock()); err != nil {
		if errors.Is(err, catalog.ErrCodeCooldown) {
			writeError(w, http.StatusTooManyRequests, "wait a moment before requesting another code")
			return err
		}
		writeError(w, http.StatusInternalServerError, "could not save the code")
		return err
	}
	if err := h.mail.SendCode(email, purpose, code); err != nil {
		writeError(w, http.StatusBadGateway, "could not send the email. check SMTP settings")
		return err
	}
	return nil
}

func (h authHandlers) consumeCode(w http.ResponseWriter, r *http.Request, email, purpose, code string) (string, bool) {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		writeError(w, http.StatusBadRequest, "enter the 6-digit code from the email")
		return "", false
	}
	hash := auth.HashCode(h.secret, email, purpose, code)
	extra, err := h.catalog.ConsumeEmailCode(r.Context(), email, purpose, hash, h.clock())
	if err == nil {
		return extra, true
	}
	switch {
	case errors.Is(err, catalog.ErrCodeNotFound), errors.Is(err, catalog.ErrCodeExpired):
		writeError(w, http.StatusBadRequest, "that code is missing or expired. request a new one")
	case errors.Is(err, catalog.ErrTooManyAttempts):
		writeError(w, http.StatusTooManyRequests, "too many wrong codes. request a new one")
	case errors.Is(err, catalog.ErrCodeWrong):
		writeError(w, http.StatusUnauthorized, "that code is wrong")
	default:
		writeError(w, http.StatusInternalServerError, "could not check the code")
	}
	return "", false
}

func (h authHandlers) writeCodeSent(w http.ResponseWriter, message string) {
	delivery := "log"
	if h.mail != nil {
		delivery = h.mail.Delivery()
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"ok":       "true",
		"message":  message,
		"delivery": delivery,
	})
}

func (h authHandlers) respondToken(w http.ResponseWriter, user *catalog.User, status int) {
	token, err := auth.IssueToken(h.secret, user.ID, 7*24*time.Hour)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create session")
		return
	}
	writeJSON(w, status, map[string]any{
		"token":        token,
		"email":        user.Email,
		"display_name": user.DisplayName,
		"theme":        user.Theme,
		"accent":       user.Accent,
		"vault_salt":   user.VaultSalt,
		"vault_wrap":   user.VaultWrap,
	})
}

func normalizeCreds(email, password string) (string, string, bool) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !emailRE.MatchString(email) || len(password) < 8 {
		return "", "", false
	}
	return email, password, true
}
