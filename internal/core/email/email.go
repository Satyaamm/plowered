// Package email is the outbound transactional email layer. The Sender
// interface is what the rest of the codebase depends on; concrete
// implementations live alongside (Resend in resend.go, Logger for tests
// + dev mode without an API key).
//
// We only need transactional today (verify-email, password-reset,
// invite). Marketing email is explicitly out of scope.
package email

import (
	"context"
	"errors"
	"log/slog"
)

// Message is one outbound email. HTML is preferred — Text is the
// plain-text fallback Resend will use if the recipient's mail client
// doesn't render HTML.
type Message struct {
	From    string
	To      []string
	Subject string
	HTML    string
	Text    string
	Tag     string // optional — tags appear in the Resend dashboard for grouping
}

// Sender is the abstraction over a transactional-email provider. Always
// validate at the boundary: returning an error here is treated as a
// soft-fail by the handler (the user still gets a 202; ops gets an alert).
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// LogSender writes the email to slog instead of delivering it. Useful in
// dev when no Resend key is configured — the link is in the container
// logs, the user copy-pastes it into the browser, the flow still works.
type LogSender struct {
	Logger *slog.Logger
}

func (l LogSender) Send(_ context.Context, m Message) error {
	if l.Logger == nil {
		return errors.New("email: LogSender missing logger")
	}
	l.Logger.Info("email.send",
		"from", m.From,
		"to", m.To,
		"subject", m.Subject,
		"tag", m.Tag,
		"text", m.Text,
	)
	return nil
}

// FailSender always returns the configured error. Used in tests that
// want to assert handler behaviour when the provider is unreachable.
type FailSender struct{ Err error }

func (f FailSender) Send(context.Context, Message) error { return f.Err }
