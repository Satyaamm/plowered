// Package pinecone_vectorstore wires Pinecone into the vectorstore
// seam. Pinecone exposes a stable REST API at
// {Endpoint}/vectors/{upsert,query,delete}; the SDK is a thin wrapper
// over that surface, so we hit REST directly and avoid the dep.
//
// Register from cmd/plowered/main.go via:
//
//	import _ "github.com/Satyaamm/plowered/internal/adapters/pinecone_vectorstore"
//
// Reference: https://docs.pinecone.io/reference
package pinecone_vectorstore

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
	vectorstore.SetPineconeDriver(driver{})
}

type driver struct{}

func (driver) Build(cfg *vectorstore.Config, secret []byte) (vectorstore.Store, error) {
	if err := require(cfg, secret); err != nil {
		return nil, err
	}
	return &store{cfg: cfg, apiKey: string(secret), http: defaultClient()}, nil
}

func (driver) Test(ctx context.Context, cfg *vectorstore.Config, secret []byte) error {
	if err := require(cfg, secret); err != nil {
		return err
	}
	s := &store{cfg: cfg, apiKey: string(secret), http: defaultClient()}
	// Lightest probe: describe-index-stats. Returns dimension + count
	// without scanning data.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(cfg.Endpoint, "/")+"/describe_index_stats",
		bytes.NewReader([]byte("{}")))
	if err != nil {
		return err
	}
	s.setHeaders(req)
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("pinecone: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return errFromHTTP(resp)
	}
	return nil
}

func require(cfg *vectorstore.Config, secret []byte) error {
	if cfg == nil {
		return errors.New("pinecone: nil config")
	}
	if cfg.Endpoint == "" {
		return errors.New("pinecone: endpoint required (host of the index)")
	}
	if len(secret) == 0 {
		return errors.New("pinecone: api key required")
	}
	return nil
}

func defaultClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

type store struct {
	cfg    *vectorstore.Config
	apiKey string
	http   *http.Client
}

func (s *store) Name() string { return "pinecone:" + s.cfg.IndexName }

func (s *store) setHeaders(r *http.Request) {
	r.Header.Set("Api-Key", s.apiKey)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
}

func (s *store) Upsert(ctx context.Context, vecs []vectorstore.Vector) error {
	if len(vecs) == 0 {
		return nil
	}
	type wire struct {
		ID       string            `json:"id"`
		Values   []float32         `json:"values"`
		Metadata map[string]string `json:"metadata,omitempty"`
	}
	rows := make([]wire, 0, len(vecs))
	for _, v := range vecs {
		rows = append(rows, wire{ID: v.ID, Values: v.Values, Metadata: v.Metadata})
	}
	body, _ := json.Marshal(map[string]any{
		"vectors":   rows,
		"namespace": s.cfg.TenantID, // per-tenant namespace isolates blast radius
	})
	return s.post(ctx, "/vectors/upsert", body, nil)
}

func (s *store) Search(ctx context.Context, opts vectorstore.SearchOpts) ([]vectorstore.SearchHit, error) {
	body, _ := json.Marshal(map[string]any{
		"vector":          opts.Vector,
		"topK":            opts.K,
		"includeMetadata": true,
		"namespace":       s.cfg.TenantID,
		"filter":          filterToPinecone(opts.Filter),
	})
	var out struct {
		Matches []struct {
			ID       string            `json:"id"`
			Score    float32           `json:"score"`
			Metadata map[string]any    `json:"metadata"`
		} `json:"matches"`
	}
	if err := s.post(ctx, "/query", body, &out); err != nil {
		return nil, err
	}
	hits := make([]vectorstore.SearchHit, 0, len(out.Matches))
	for _, m := range out.Matches {
		hits = append(hits, vectorstore.SearchHit{
			ID:       m.ID,
			Score:    m.Score,
			Metadata: mapToString(m.Metadata),
		})
	}
	return hits, nil
}

func (s *store) Delete(ctx context.Context, ids []string) error {
	body, _ := json.Marshal(map[string]any{
		"ids":       ids,
		"namespace": s.cfg.TenantID,
	})
	return s.post(ctx, "/vectors/delete", body, nil)
}

func (s *store) post(ctx context.Context, path string, body []byte, decode any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(s.cfg.Endpoint, "/")+path,
		bytes.NewReader(body))
	if err != nil {
		return err
	}
	s.setHeaders(req)
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("pinecone: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return errFromHTTP(resp)
	}
	if decode != nil {
		return json.NewDecoder(resp.Body).Decode(decode)
	}
	return nil
}

// filterToPinecone wraps simple equality filters into Pinecone's
// $eq operator syntax. The platform's SearchOpts.Filter is a flat
// string→string map today; we'll widen to ranges + $in if a caller
// needs it.
func filterToPinecone(in map[string]string) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = map[string]any{"$eq": v}
	}
	return out
}

func mapToString(in map[string]any) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		if s, ok := v.(string); ok {
			out[k] = s
		} else {
			out[k] = fmt.Sprint(v)
		}
	}
	return out
}

func errFromHTTP(resp *http.Response) error {
	buf := make([]byte, 512)
	n, _ := resp.Body.Read(buf)
	body := strings.TrimSpace(string(buf[:n]))
	if body == "" {
		body = resp.Status
	}
	return fmt.Errorf("pinecone upstream %d: %s", resp.StatusCode, body)
}
