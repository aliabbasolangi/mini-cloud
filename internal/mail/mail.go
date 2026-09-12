package mail

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/smtp"
	"strings"
	"time"

	"minicloud/internal/catalog"
	"minicloud/internal/config"
)

type Mailer interface {
	SendCode(to, purpose, code string) error
	Delivery() string
}

type smtpMailer struct {
	host string
	port int
	user string
	pass string
	from string
}

type logMailer struct{}

type comboMailer struct {
	inner Mailer
	log   bool
}

func New(cfg config.Config) Mailer {
	var inner Mailer
	if key := strings.TrimSpace(cfg.ResendAPIKey); key != "" {
		from := strings.TrimSpace(cfg.ResendFrom)
		if from == "" {
			from = "Mini Cloud <onboarding@resend.dev>"
		}
		inner = resendMailer{key: key, from: from}
	} else if strings.TrimSpace(cfg.SMTPHost) != "" {
		from := strings.TrimSpace(cfg.SMTPFrom)
		if from == "" {
			from = cfg.SMTPUser
		}
		inner = smtpMailer{
			host: cfg.SMTPHost,
			port: cfg.SMTPPort,
			user: cfg.SMTPUser,
			pass: cfg.SMTPPass,
			from: from,
		}
	}
	if inner == nil {
		return logMailer{}
	}
	if cfg.MailLog {
		return comboMailer{inner: inner, log: true}
	}
	return inner
}

func (logMailer) Delivery() string { return "log" }

func (logMailer) SendCode(to, purpose, code string) error {
	log.Printf("mail code [%s] to %s: %s", purpose, to, code)
	return nil
}

func (c comboMailer) Delivery() string {
	if c.log {
		return "log"
	}
	if c.inner != nil {
		return c.inner.Delivery()
	}
	return "log"
}

func (c comboMailer) SendCode(to, purpose, code string) error {
	if c.log {
		log.Printf("mail code [%s] to %s: %s", purpose, to, code)
	}
	if c.inner == nil {
		return nil
	}
	if err := c.inner.SendCode(to, purpose, code); err != nil {
		if c.log {
			log.Printf("mail send failed, using the log copy: %v", err)
			return nil
		}
		return err
	}
	return nil
}

func (s smtpMailer) Delivery() string { return "smtp" }

func (s smtpMailer) SendCode(to, purpose, code string) error {
	subject := "Your Mini Cloud verification code"
	why := "confirm your email and finish creating your account"
	if purpose == catalog.PurposeReset {
		subject = "Reset your Mini Cloud password"
		why = "reset your password"
	}

	body := fmt.Sprintf("Your Mini Cloud code is %s\n\nUse it to %s. It expires in 10 minutes.\nIf you did not ask for this, ignore the email.\n", code, why)
	fromAddr := envelopeAddr(s.from)
	msg := strings.Join([]string{
		"From: " + s.from,
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		body,
	}, "\r\n")

	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	auth := smtp.PlainAuth("", s.user, s.pass, s.host)
	return sendSMTP(addr, s.host, s.port, auth, fromAddr, to, []byte(msg))
}

// implicitTLSAuth lets smtp.PlainAuth run on port 465. Go only sets
// ServerInfo.TLS after STARTTLS, so implicit TLS looks "unencrypted" otherwise.
type implicitTLSAuth struct{ smtp.Auth }

func (a implicitTLSAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	server.TLS = true
	return a.Auth.Start(server)
}

func envelopeAddr(from string) string {
	from = strings.TrimSpace(from)
	if i := strings.LastIndex(from, "<"); i >= 0 {
		return strings.Trim(from[i+1:], "> ")
	}
	return from
}

const smtpTimeout = 15 * time.Second

func sendSMTP(addr, host string, port int, auth smtp.Auth, from, to string, msg []byte) error {
	dialer := &net.Dialer{Timeout: smtpTimeout}
	var conn net.Conn
	var err error
	if port == 465 {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(smtpTimeout))

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()

	if port != 465 {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
				return err
			}
		}
	}
	if auth != nil {
		if port == 465 {
			auth = implicitTLSAuth{auth}
		}
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}
