package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"minicloud/internal/auth"
	"minicloud/internal/catalog"
)

type memMail struct {
	to, purpose, code string
}

func (m *memMail) SendCode(to, purpose, code string) error {
	m.to, m.purpose, m.code = to, purpose, code
	return nil
}

func (m *memMail) Delivery() string { return "log" }

func testAuth(t *testing.T) (authHandlers, *catalog.DB, *memMail) {
	t.Helper()
	db, err := catalog.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mailer := &memMail{}
	h := authHandlers{catalog: db, secret: "test-secret-16ch", mail: mailer}
	return h, db, mailer
}

func postJSON(h http.HandlerFunc, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h(w, req)
	return w
}

func TestRegisterNeedsEmailCode(t *testing.T) {
	h, _, mailer := testAuth(t)

	start := postJSON(h.registerStart, map[string]string{
		"email":    "ali@example.com",
		"password": "password1",
	})
	if start.Code != http.StatusOK {
		t.Fatalf("start %d %s", start.Code, start.Body.String())
	}
	if mailer.code == "" {
		t.Fatal("expected a code to be emailed")
	}

	bad := postJSON(h.registerVerify, map[string]string{
		"email": "ali@example.com",
		"code":  "000000",
	})
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("wrong code %d", bad.Code)
	}

	ok := postJSON(h.registerVerify, map[string]string{
		"email": "ali@example.com",
		"code":  mailer.code,
	})
	if ok.Code != http.StatusCreated {
		t.Fatalf("verify %d %s", ok.Code, ok.Body.String())
	}

	login := postJSON(h.login, map[string]string{
		"email":    "ali@example.com",
		"password": "password1",
	})
	if login.Code != http.StatusOK {
		t.Fatalf("login %d %s", login.Code, login.Body.String())
	}
}

func TestForgotPasswordReset(t *testing.T) {
	h, db, mailer := testAuth(t)
	if _, err := db.CreateUser(t.Context(), "ali@example.com", mustHash(t, "oldpass12")); err != nil {
		t.Fatal(err)
	}

	forgot := postJSON(h.forgot, map[string]string{"email": "ali@example.com"})
	if forgot.Code != http.StatusOK || mailer.code == "" {
		t.Fatalf("forgot %d code %q", forgot.Code, mailer.code)
	}

	reset := postJSON(h.reset, map[string]string{
		"email":    "ali@example.com",
		"code":     mailer.code,
		"password": "newpass99",
	})
	if reset.Code != http.StatusOK {
		t.Fatalf("reset %d %s", reset.Code, reset.Body.String())
	}

	old := postJSON(h.login, map[string]string{"email": "ali@example.com", "password": "oldpass12"})
	if old.Code != http.StatusUnauthorized {
		t.Fatalf("old password still worked: %d", old.Code)
	}
	fresh := postJSON(h.login, map[string]string{"email": "ali@example.com", "password": "newpass99"})
	if fresh.Code != http.StatusOK {
		t.Fatalf("new login %d %s", fresh.Code, fresh.Body.String())
	}
}

func mustHash(t *testing.T, password string) string {
	t.Helper()
	h, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	return h
}
