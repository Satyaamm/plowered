package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ResendSender posts via the Resend transactional-email API. We don't
// pull the official SDK (one more dependency for a five-field API) and
// instead speak HTTP directly.
//
// API ref: https://resend.com/docs/api-reference/emails/send-email
type ResendSender struct {
	APIKey   string
	BaseURL  string // override for tests; empty defaults to https://api.resend.com
	Client   *http.Client
}

// NewResendSender constructs a sender with sane defaults.
func NewResendSender(apiKey string) *ResendSender {
	return &ResendSender{
		APIKey:  apiKey,
		BaseURL: "https://api.resend.com",
		Client:  &http.Client{Timeout: 10 * time.Second},
	}
}

type resendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html,omitempty"`
	Text    string   `json:"text,omitempty"`
	Tags    []resendTag `json:"tags,omitempty"`
}

type resendTag struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type resendError struct {
	Name       string `json:"name"`
	Message    string `json:"message"`
	StatusCode int    `json:"statusCode"`
}

func (s *ResendSender) Send(ctx context.Context, m Message) error {
	if s.APIKey == "" {
		return fmt.Errorf("resend: missing API key")
	}
	if len(m.To) == 0 {
		return fmt.Errorf("resend: at least one recipient required")
	}
	body := resendRequest{
		From:    m.From,
		To:      m.To,
		Subject: m.Subject,
		HTML:    m.HTML,
		Text:    m.Text,
	}
	if m.Tag != "" {
		body.Tags = []resendTag{{Name: "category", Value: m.Tag}}
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("resend: marshal: %w", err)
	}
	base := s.BaseURL
	if base == "" {
		base = "https://api.resend.com"
	}
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/emails", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.APIKey)
	req.Header.Set("Content-Type", "application/json")

	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("resend: request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	respBody, _ := io.ReadAll(resp.Body)
	var apiErr resendError
	_ = json.Unmarshal(respBody, &apiErr)
	if apiErr.Message != "" {
		return fmt.Errorf("resend: %s (status %d)", apiErr.Message, resp.StatusCode)
	}
	return fmt.Errorf("resend: unexpected status %d: %s", resp.StatusCode, string(respBody))
}
