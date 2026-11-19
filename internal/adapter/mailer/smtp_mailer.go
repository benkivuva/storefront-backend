package mailer

import (
	"context"
	"fmt"
	"net/smtp"

	"github.com/benkivuva/storefront-backend/internal/domain"
)

type SMTPMailer struct {
	addr      string
	fromEmail string
}

func NewSMTPMailer(addr, fromEmail string) *SMTPMailer {
	return &SMTPMailer{
		addr:      addr,
		fromEmail: fromEmail,
	}
}

func (m *SMTPMailer) Send(ctx context.Context, msg domain.MailMessage) error {
	headers := make(map[string]string)
	headers["From"] = m.fromEmail
	headers["To"] = msg.To
	headers["Subject"] = msg.Subject
	headers["MIME-Version"] = "1.0"
	headers["Content-Type"] = "text/html; charset=\"UTF-8\""

	message := ""
	for k, v := range headers {
		message += fmt.Sprintf("%s: %s\r\n", k, v)
	}
	message += "\r\n" + msg.Body

	// Mailpit and many local relays don't require auth.
	// For production (e.g., SendGrid), you would add smtp.PlainAuth here.
	var auth smtp.Auth

	err := smtp.SendMail(m.addr, auth, m.fromEmail, []string{msg.To}, []byte(message))
	if err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	return nil
}
