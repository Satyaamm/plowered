// Package qdrant_vectorstore wires Qdrant into the vectorstore seam.
// REST API at {Endpoint}/collections/{c}/points/{upsert,search,delete};
// the Go gRPC client adds 3MB of deps for endpoints we don't need, so
// REST is the right call here.
//
// Register from cmd/plowered/main.go:
//
//	import _ "github.com/Satyaamm/plowered/internal/adapters/qdrant_vectorstore"
//
// Reference: https://qdrant.tech/documentation/concepts/points
package qdrant_vectorstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Satyaamm/plowered/internal/core/vectorstore"
)

func init() {
	vectorstore.SetQdrantDriver(driver{})
}

type driver struct{}

func (driver) Build(cfg *vectorstore.Config, secret []byte) (vectorstore.Store, error) {
	if err := require(cfg); err != nil {
		return nil, err
	}
	return &store{cfg: cfg, apiKey: string(secret), http: &http.Client{Timeout: 30 * time.Second}}, nil
}

func (driver) Test(ctx context.Context, cfg *vectorstore.Config, secret []byte) error {
	if err := require(cfg); err != nil {
		return err
	}
	// Lightest probe: GET /collections/{c}. 200 ⇒ creds + collection
	// both exist. 404 ⇒ creds valid but collection missing — we surface
	// that explicitly so the operator knows to create it.
	url := fmt.Sprintf("%s/collections/%s",
		strings.TrimRight(cfg.Endpoint, "/"), cfg.Collection)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if len(secret) > 0 {
		req.Header.Set("api-key", string(secret))
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("qdrant: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("qdrant: collection %q not found at %s — create it before saving", cfg.Collection, cfg.Endpoint)
	}
	if resp.StatusCode >= 400 {
		return errFromHTTP(resp)
	}
	return nil
}

func require(cfg *vectorstore.Config) error {
	if cfg == nil {
		return errors.New("qdrant: nil config")
	}
	if cfg.Endpoint == "" {
		return errors.New("qdrant: endpoint required")
	}
	if cfg.Collection == "" {
		return errors.New("qdrant: collection required")
	}
	return nil
}

type store struct {
	cfg    *vectorstore.Config
	apiKey string
	http   *http.Client
}

func (s *store) Name() string { return "qdrant:" + s.cfg.Collection }

func (s *store) setHeaders(r *http.Request) {
	r.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		r.Header.Set("api-key", s.apiKey)
	}
}

func (s *store) baseURL() string {
	return fmt.Sprintf("%s/collections/%s",
		strings.TrimRight(s.cfg.Endpoint, "/"), s.cfg.Collection)
}

func (s *store) Upsert(ctx context.Context, vecs []vectorstore.Vector) error {
	type point struct {
		ID      string            `json:"id"`
		Vector  []float32         `json:"vector"`
		Payload map[string]string `json:"payload,omitempty"`
	}
	points := make([]point, 0, len(vecs))
	for _, v := range vecs {
		points = append(points, point{ID: v.ID, Vector: v.Values, Payload: v.Metadata})
	}
	body, _ := json.Marshal(map[string]any{"points": points})
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		s.baseURL()+"/points?wait=true", bytes.NewReader(body))
	if err != nil {
		return err
	}
	s.setHeaders(req)
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("qdrant: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return errFromHTTP(resp)
	}
	return nil
}

func (s *store) Search(ctx context.Context, opts vectorstore.SearchOpts) ([]vectorstore.SearchHit, error) {
	body, _ := json.Marshal(map[string]any{
		"vector":       opts.Vector,
		"limit":        opts.K,
		"with_payload": true,
		"filter":       filterToQdrant(opts.Filter),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.baseURL()+"/points/search", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	s.setHeaders(req)
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("qdrant: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, errFromHTTP(resp)
	}
	var out struct {
		Result []struct {
			ID      string            `json:"id"`
			Score   float32           `json:"score"`
			Payload map[string]string `json:"payload"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	hits := make([]vectorstore.SearchHit, 0, len(out.Result))
	for _, r := range out.Result {
		hits = append(hits, vectorstore.SearchHit{
			ID:       r.ID,
			Score:    r.Score,
			Metadata: r.Payload,
		})
	}
	return hits, nil
}

func (s *store) Delete(ctx context.Context, ids []string) error {
	body, _ := json.Marshal(map[string]any{"points": ids})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.baseURL()+"/points/delete?wait=true", bytes.NewReader(body))
	if err != nil {
		return err
	}
	s.setHeaders(req)
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("qdrant: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return errFromHTTP(resp)
	}
	return nil
}

// filterToQdrant converts the flat string→string filter map into
// Qdrant's "must":[{"key":...,"match":{"value":...}}] shape.
func filterToQdrant(in map[string]string) map[string]any {
	if len(in) == 0 {
		return nil
	}
	must := make([]map[string]any, 0, len(in))
	for k, v := range in {
		must = append(must, map[string]any{
			"key":   k,
			"match": map[string]any{"value": v},
		})
	}
	return map[string]any{"must": must}
}

func errFromHTTP(resp *http.Response) error {
	buf := make([]byte, 512)
	n, _ := resp.Body.Read(buf)
	body := strings.TrimSpace(string(buf[:n]))
	if body == "" {
		body = resp.Status
	}
	return fmt.Errorf("qdrant upstream %d: %s", resp.StatusCode, body)
}
