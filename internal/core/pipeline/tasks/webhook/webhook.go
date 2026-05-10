// Package webhook implements the "webhook" task type — a single HTTP
// request to an arbitrary URL. Useful as a notify-on-success hook or to
// fan out work to external services.
//
// Config:
//
//	{
//	  "url":     "https://hooks.example.com/x",
//	  "method":  "POST",                       // default POST
//	  "headers": {"Authorization": "Bearer …"},
//	  "body":    {...} | "raw string",
//	  "timeout_seconds": 30,
//	  "expect_status": [200, 204]              // default: 2xx accepted
//	}
//
// Output:
//
//	{ "status_code": 200, "duration_ms": 87, "body_preview": "..." }
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Satyaamm/plowered/internal/core/pipeline"
)

const previewBytes = 512

type Executor struct {
	Client *http.Client
}

func New(c *http.Client) *Executor {
	if c == nil {
		c = &http.Client{Timeout: 30 * time.Second}
	}
	return &Executor{Client: c}
}

func (Executor) Type() pipeline.TaskType { return pipeline.TaskTypeWebhook }

func (e Executor) Execute(ctx context.Context, ec pipeline.ExecutionContext) (pipeline.Output, error) {
	cfg := ec.Task.Config
	url, _ := cfg["url"].(string)
	if url == "" {
		return pipeline.Output{}, errors.New("webhook: url is required")
	}
	method, _ := cfg["method"].(string)
	if method == "" {
		method = http.MethodPost
	}
	method = strings.ToUpper(method)

	timeout := readTimeout(cfg, 30*time.Second)
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body, contentType, err := encodeBody(cfg["body"])
	if err != nil {
		return pipeline.Output{}, err
	}

	req, err := http.NewRequestWithContext(reqCtx, method, url, body)
	if err != nil {
		return pipeline.Output{}, fmt.Errorf("build request: %w", err)
	}
	if contentType != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", contentType)
	}
	if hdrs, ok := cfg["headers"].(map[string]any); ok {
		for k, v := range hdrs {
			if s, ok := v.(string); ok {
				req.Header.Set(k, s)
			}
		}
	}

	ec.Log(ctx, "info", "webhook: %s %s", method, url)
	start := time.Now()
	resp, err := e.Client.Do(req)
	if err != nil {
		return pipeline.Output{}, fmt.Errorf("send: %w", err)
	}
	defer resp.Body.Close()

	preview, _ := io.ReadAll(io.LimitReader(resp.Body, previewBytes))
	ec.Log(ctx, "info", "webhook: status=%d in %dms", resp.StatusCode, time.Since(start).Milliseconds())

	if !acceptStatus(resp.StatusCode, cfg["expect_status"]) {
		return pipeline.Output{}, fmt.Errorf("webhook returned %d (body: %s)", resp.StatusCode, string(preview))
	}

	return pipeline.Output{
		Properties: map[string]any{
			"status_code":  resp.StatusCode,
			"duration_ms":  time.Since(start).Milliseconds(),
			"body_preview": string(preview),
		},
	}, nil
}

func encodeBody(raw any) (io.Reader, string, error) {
	switch v := raw.(type) {
	case nil:
		return nil, "", nil
	case string:
		return strings.NewReader(v), "text/plain; charset=utf-8", nil
	default:
		buf, err := json.Marshal(v)
		if err != nil {
			return nil, "", fmt.Errorf("encode body: %w", err)
		}
		return bytes.NewReader(buf), "application/json", nil
	}
}

func acceptStatus(code int, raw any) bool {
	if raw == nil {
		return code >= 200 && code < 300
	}
	arr, ok := raw.([]any)
	if !ok {
		return code >= 200 && code < 300
	}
	for _, v := range arr {
		switch n := v.(type) {
		case float64:
			if int(n) == code {
				return true
			}
		case int:
			if n == code {
				return true
			}
		}
	}
	return false
}

func readTimeout(cfg map[string]any, def time.Duration) time.Duration {
	switch v := cfg["timeout_seconds"].(type) {
	case float64:
		if v > 0 {
			return time.Duration(v) * time.Second
		}
	case int:
		if v > 0 {
			return time.Duration(v) * time.Second
		}
	}
	return def
}
