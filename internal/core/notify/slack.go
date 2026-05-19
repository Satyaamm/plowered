package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// SlackChannel posts a single rendered notification to a Slack
// incoming-webhook URL. Wire format is the minimal `{"text": "..."}`
// payload Slack accepts on every webhook variant. URL lives in
// ChannelConfig.Config["webhook_url"] — Slack hands the operator the
// full secret URL when they install the app, so we don't store the
// secret separately.
//
// Why a separate type from WebhookChannel: Slack rejects the rich
// {subject, body, ...} JSON our generic webhook posts; it only renders
// the "text" field. Keeping the bodies separate also lets us add Slack
// blocks formatting later without compromising the generic webhook
// contract.
type SlackChannel struct {
	HTTPClient *http.Client
}

func NewSlackChannel() *SlackChannel {
	return &SlackChannel{HTTPClient: &http.Client{Timeout: 10 * time.Second}}
}

func (*SlackChannel) Kind() string { return "slack" }

type slackPayload struct {
	Text string `json:"text"`
}

func (c *SlackChannel) Deliver(ctx context.Context, cfg *ChannelConfig, d Delivery) error {
	if cfg == nil {
		return fmt.Errorf("slack: nil channel config")
	}
	url, _ := cfg.Config["webhook_url"].(string)
	if url == "" {
		return fmt.Errorf("slack: channel %q missing config.webhook_url", cfg.ID)
	}
	text := d.Subject
	if d.Body != "" {
		text = d.Subject + "\n```\n" + d.Body + "```"
	}
	body, _ := json.Marshal(slackPayload{Text: text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("slack: %w (transient)", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return fmt.Errorf("slack: client error %d", resp.StatusCode)
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("slack: server error %d (transient)", resp.StatusCode)
	}
	return nil
}
