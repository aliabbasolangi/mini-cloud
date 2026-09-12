package mail

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"minicloud/internal/catalog"
	"minicloud/internal/config"
)

func TestNewPrefersResendOverSMTP(t *testing.T) {
	m := New(config.Config{
		ResendAPIKey: "re_test",
		SMTPHost:     "smtp.gmail.com",
		SMTPPass:     "x",
	})
	if m.Delivery() != "resend" {
		t.Fatalf("delivery %q", m.Delivery())
	}
}

func TestComboFallsBackToLogWhenSMTPFails(t *testing.T) {
	m := comboMailer{inner: failMail{}, log: true}
	if err := m.SendCode("a@b.com", catalog.PurposeRegister, "123456"); err != nil {
		t.Fatalf("log fallback should succeed: %v", err)
	}
}

func TestResendPostsCode(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer re_test" {
			t.Fatalf("auth %q", r.Header.Get("Authorization"))
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"1"}`))
	}))
	t.Cleanup(srv.Close)

	m := resendMailer{key: "re_test", from: "Mini Cloud <onboarding@resend.dev>", api: srv.URL}
	if err := m.SendCode("ali@example.com", catalog.PurposeRegister, "654321"); err != nil {
		t.Fatal(err)
	}
	text, _ := got["text"].(string)
	if !strings.Contains(text, "654321") {
		t.Fatalf("body %+v", got)
	}
}

type failMail struct{}

func (failMail) SendCode(string, string, string) error { return errors.New("blocked") }
func (failMail) Delivery() string                      { return "smtp" }