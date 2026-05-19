package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Satyaamm/plowered/internal/core/email"
)

func TestSlackChannelPostsTextPayload(t *testing.T) {
	var got slackPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch := NewSlackChannel()
	cfg := &ChannelConfig{
		ID:     "ch-1",
		Kind:   "slack",
		Config: map[string]any{"webhook_url": srv.URL},
	}
	d := Delivery{
		ID:      "d1",
		Subject: "Migration failed",
		Body:    "rows_read=10 rows_written=5",
	}
	if err := ch.Deliver(context.Background(), cfg, d); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if !strings.Contains(got.Text, "Migration failed") {
		t.Errorf("text missing subject: %q", got.Text)
	}
	if !strings.Contains(got.Text, "rows_read=10") {
		t.Errorf("text missing body: %q", got.Text)
	}
}

func TestSlackChannelRejectsMissingURL(t *testing.T) {
	ch := NewSlackChannel()
	cfg := &ChannelConfig{ID: "ch-2", Config: map[string]any{}}
	err := ch.Deliver(context.Background(), cfg, Delivery{Subject: "x"})
	if err == nil || !strings.Contains(err.Error(), "webhook_url") {
		t.Errorf("expected webhook_url error, got %v", err)
	}
}

func TestSlackChannelSurfacesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	ch := NewSlackChannel()
	err := ch.Deliver(context.Background(), &ChannelConfig{
		ID: "ch", Config: map[string]any{"webhook_url": srv.URL},
	}, Delivery{Subject: "x"})
	if err == nil || !strings.Contains(err.Error(), "transient") {
		t.Errorf("502 should be transient, got %v", err)
	}
}

// captureSender records the last message sent so the email channel
// tests can inspect the From/To/Subject/Body without involving Resend.
type captureSender struct {
	last email.Message
	err  error
}

func (c *captureSender) Send(_ context.Context, m email.Message) error {
	if c.err != nil {
		return c.err
	}
	c.last = m
	return nil
}

func TestEmailChannelReadsCSVRecipients(t *testing.T) {
	cap := &captureSender{}
	ch := &EmailChannel{Sender: cap, DefaultFrom: "alerts@plowered.io"}
	cfg := &ChannelConfig{
		ID: "ch-3", Kind: "email",
		Config: map[string]any{"to": "ops@example.com, sre@example.com"},
	}
	if err := ch.Deliver(context.Background(), cfg, Delivery{Subject: "Migration ok", Body: "all good"}); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if len(cap.last.To) != 2 || cap.last.To[0] != "ops@example.com" || cap.last.To[1] != "sre@example.com" {
		t.Errorf("recipients: %v", cap.last.To)
	}
	if cap.last.From != "alerts@plowered.io" {
		t.Errorf("from should fall back to DefaultFrom, got %q", cap.last.From)
	}
	if cap.last.Subject != "Migration ok" || cap.last.Text != "all good" {
		t.Errorf("subject/body: %q / %q", cap.last.Subject, cap.last.Text)
	}
}

func TestEmailChannelHonorsPerChannelFrom(t *testing.T) {
	cap := &captureSender{}
	ch := &EmailChannel{Sender: cap, DefaultFrom: "alerts@plowered.io"}
	cfg := &ChannelConfig{
		ID: "ch-4",
		Config: map[string]any{
			"to":   []any{"data@example.com"},
			"from": "custom@plowered.io",
		},
	}
	if err := ch.Deliver(context.Background(), cfg, Delivery{Subject: "x"}); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if cap.last.From != "custom@plowered.io" {
		t.Errorf("from override ignored: %q", cap.last.From)
	}
}

func TestEmailChannelRejectsEmptyRecipients(t *testing.T) {
	ch := &EmailChannel{Sender: &captureSender{}, DefaultFrom: "x@y"}
	err := ch.Deliver(context.Background(), &ChannelConfig{ID: "c", Config: map[string]any{"to": ""}}, Delivery{Subject: "x"})
	if err == nil || !strings.Contains(err.Error(), "no recipients") {
		t.Errorf("expected no-recipients error, got %v", err)
	}
}

func TestWebhookChannelReadsConfigURL(t *testing.T) {
	var got webhookPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	ch := NewWebhookChannel()
	cfg := &ChannelConfig{
		ID:     "wh",
		Config: map[string]any{"url": srv.URL},
	}
	err := ch.Deliver(context.Background(), cfg, Delivery{
		ID: "d", IdempotencyKey: "k", EventID: "e", Subject: "s", Body: "b",
	})
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if got.Subject != "s" || got.Body != "b" || got.IdempotencyKey != "k" {
		t.Errorf("payload not propagated: %+v", got)
	}
}
