package notify

import (
	"context"
	"fmt"

	"github.com/Satyaamm/plowered/internal/core/email"
)

// EmailChannel sends one notification as a transactional email through
// the existing email.Sender (Resend in prod, LogSender in dev). Recipients
// come from ChannelConfig.Config["to"] — either a comma-separated string
// or a []any/[]string of addresses. The From header falls back to the
// channel's DefaultFrom when not overridden per-channel.
type EmailChannel struct {
	Sender      email.Sender
	DefaultFrom string
}

func (*EmailChannel) Kind() string { return "email" }

func (c *EmailChannel) Deliver(ctx context.Context, cfg *ChannelConfig, d Delivery) error {
	if cfg == nil {
		return fmt.Errorf("email: nil channel config")
	}
	if c.Sender == nil {
		return fmt.Errorf("email: no Sender configured on channel impl")
	}
	to := extractAddresses(cfg.Config["to"])
	if len(to) == 0 {
		return fmt.Errorf("email: channel %q has no recipients in config.to", cfg.ID)
	}
	from, _ := cfg.Config["from"].(string)
	if from == "" {
		from = c.DefaultFrom
	}
	if from == "" {
		return fmt.Errorf("email: no from address (channel %q config.from + DefaultFrom both empty)", cfg.ID)
	}
	return c.Sender.Send(ctx, email.Message{
		From:    from,
		To:      to,
		Subject: d.Subject,
		Text:    d.Body,
		Tag:     "notify",
	})
}

// extractAddresses accepts string ("a@b.com, c@d.com"), []string, or
// []any (after JSON round-trips through map[string]any) and normalises
// to a slice. Empty / whitespace entries are dropped.
func extractAddresses(v any) []string {
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		return splitAndTrim(x)
	case []string:
		out := make([]string, 0, len(x))
		for _, s := range x {
			if s = trim(s); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(x))
		for _, s := range x {
			if str, ok := s.(string); ok {
				if str = trim(str); str != "" {
					out = append(out, str)
				}
			}
		}
		return out
	}
	return nil
}

func splitAndTrim(csv string) []string {
	out := make([]string, 0, 4)
	start := 0
	for i := 0; i <= len(csv); i++ {
		if i == len(csv) || csv[i] == ',' {
			if s := trim(csv[start:i]); s != "" {
				out = append(out, s)
			}
			start = i + 1
		}
	}
	return out
}

func trim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 {
		c := s[len(s)-1]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}
