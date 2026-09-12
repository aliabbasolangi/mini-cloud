package mail

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"minicloud/internal/catalog"
)

type resendMailer struct {
	key  string
	from string
	api  string
}

func (r resendMailer) Delivery() string { return "resend" }

func (r resendMailer) endpoint() string {
	if r.api != "" {
		return r.api
	}
	return "https://api.resend.com/emails"
}

func (r resendMailer) SendCode(to, purpose, code string) error {
	subject := "Your SafeKeeping verification code"
	why := "confirm your email and finish creating your account"
	if purpose == catalog.PurposeReset {
		subject = "Reset your SafeKeeping password"
		why = "reset your password"
	}
	body := fmt.Sprintf("Your SafeKeeping code is %s\n\nUse it to %s. It expires in 10 minutes.\nIf you did not ask for this, ignore the email.\n", code, why)
	payload, err := json.Marshal(map[string]any{
		"from":    r.from,
		"to":      []string{to},
		"subject": subject,
		"text":    body,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, r.endpoint(), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.key)
	req.Header.Set("Content-Type", "application/json")

	res, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
	if res.StatusCode >= 300 {
		return fmt.Errorf("resend %d: %s", res.StatusCode, bytes.TrimSpace(raw))
	}
	return nil
}
