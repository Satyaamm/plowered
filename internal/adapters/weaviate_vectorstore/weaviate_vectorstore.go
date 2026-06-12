// Package weaviate_vectorstore wires Weaviate into the vectorstore
// seam. Uses Weaviate's REST v1 API at {Endpoint}/v1/objects and
// /v1/graphql for nearVector search.
//
// Register from cmd/plowered/main.go:
//
//	import _ "github.com/Satyaamm/plowered/internal/adapters/weaviate_vectorstore"
//
// Reference: https://weaviate.io/developers/weaviate/api/rest
package weaviate_vectorstore

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
	vectorstore.SetWeaviateDriver(driver{})
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(cfg.Endpoint, "/")+"/v1/meta", nil)
	if err != nil {
		return err
	}
	if len(secret) > 0 {
		req.Header.Set("Authorization", "Bearer "+string(secret))
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("weaviate: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return errFromHTTP(resp)
	}
	return nil
}

func require(cfg *vectorstore.Config) error {
	if cfg == nil {
		return errors.New("weaviate: nil config")
	}
	if cfg.Endpoint == "" {
		return errors.New("weaviate: endpoint required")
	}
	if cfg.ClassName == "" {
		return errors.New("weaviate: class_name required")
	}
	return nil
}

type store struct {
	cfg    *vectorstore.Config
	apiKey string
	http   *http.Client
}

func (s *store) Name() string { return "weaviate:" + s.cfg.ClassName }

func (s *store) setHeaders(r *http.Request) {
	r.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		r.Header.Set("Authorization", "Bearer "+s.apiKey)
	}
}

func (s *store) Upsert(ctx context.Context, vecs []vectorstore.Vector) error {
	// Weaviate's batch-objects endpoint expects {"objects":[...]}.
	type wireObj struct {
		Class      string            `json:"class"`
		ID         string            `json:"id,omitempty"`
		Properties map[string]string `json:"properties,omitempty"`
		Vector     []float32         `json:"vector"`
	}
	objs := make([]wireObj, 0, len(vecs))
	for _, v := range vecs {
		objs = append(objs, wireObj{
			Class:      s.cfg.ClassName,
			ID:         v.ID,
			Properties: v.Metadata,
			Vector:     v.Values,
		})
	}
	body, _ := json.Marshal(map[string]any{"objects": objs})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(s.cfg.Endpoint, "/")+"/v1/batch/objects",
		bytes.NewReader(body))
	if err != nil {
		return err
	}
	s.setHeaders(req)
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("weaviate: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return errFromHTTP(resp)
	}
	return nil
}

func (s *store) Search(ctx context.Context, opts vectorstore.SearchOpts) ([]vectorstore.SearchHit, error) {
	// Use Weaviate's GraphQL Get { Class(nearVector:{vector:…}) {...} }.
	vecJSON, _ := json.Marshal(opts.Vector)
	query := fmt.Sprintf(`{
		Get {
			%s(
				nearVector: { vector: %s }
				limit: %d
			) {
				_additional { id distance }
			}
		}
	}`, s.cfg.ClassName, string(vecJSON), opts.K)
	body, _ := json.Marshal(map[string]any{"query": query})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(s.cfg.Endpoint, "/")+"/v1/graphql",
		bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	s.setHeaders(req)
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("weaviate: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, errFromHTTP(resp)
	}
	var out struct {
		Data struct {
			Get map[string][]struct {
				Additional struct {
					ID       string  `json:"id"`
					Distance float32 `json:"distance"`
				} `json:"_additional"`
			} `json:"Get"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	rows := out.Data.Get[s.cfg.ClassName]
	hits := make([]vectorstore.SearchHit, 0, len(rows))
	for _, r := range rows {
		// Distance → similarity flip so callers can sort descending
		// consistently. Cosine distance is in [0,2]; 1-d gives [-1,1].
		hits = append(hits, vectorstore.SearchHit{
			ID:    r.Additional.ID,
			Score: 1 - r.Additional.Distance,
		})
	}
	return hits, nil
}

func (s *store) Delete(ctx context.Context, ids []string) error {
	// Weaviate's REST delete is per-id; batch via a goroutine pool
	// could come later if needed.
	for _, id := range ids {
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
			fmt.Sprintf("%s/v1/objects/%s/%s",
				strings.TrimRight(s.cfg.Endpoint, "/"), s.cfg.ClassName, id),
			nil)
		if err != nil {
			return err
		}
		s.setHeaders(req)
		resp, err := s.http.Do(req)
		if err != nil {
			return fmt.Errorf("weaviate: %w", err)
		}
		resp.Body.Close()
		if resp.StatusCode >= 400 && resp.StatusCode != http.StatusNotFound {
			return errFromHTTP(resp)
		}
	}
	return nil
}

func errFromHTTP(resp *http.Response) error {
	buf := make([]byte, 512)
	n, _ := resp.Body.Read(buf)
	body := strings.TrimSpace(string(buf[:n]))
	if body == "" {
		body = resp.Status
	}
	return fmt.Errorf("weaviate upstream %d: %s", resp.StatusCode, body)
}
