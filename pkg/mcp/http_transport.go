package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// HTTPTransport adapts the JSON-RPC 2.0 wire protocol to a standard
// `POST /mcp` HTTP handler. One HTTP request carries one JSON-RPC
// request body, and the response body holds the matching reply. Server
// notifications are not supported on this transport — that's fine for
// the read-only tool surface Plowered exposes.
//
// The transport runs the entire dispatch + reply lifecycle for each
// request in a single shot, so Server.Serve doesn't apply here. Use
// Server's lower-level helpers via the exported HandleRequest method
// instead.
type HTTPTransport struct {
	server *Server
	mu     sync.Mutex
	queue  chan *Message // server-initiated frames awaiting flush (unused for HTTP)
}

// NewHTTPTransport returns a transport bound to a server.
func NewHTTPTransport(s *Server) *HTTPTransport {
	return &HTTPTransport{server: s, queue: make(chan *Message, 8)}
}

// Read blocks until a server-initiated frame is available; HTTP doesn't
// produce these, so it just blocks on context cancellation.
func (h *HTTPTransport) Read(ctx context.Context) (*Message, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case m := <-h.queue:
		return m, nil
	}
}

// Write enqueues a frame for the next read. Currently unused by the
// HTTP-style flow since dispatch returns directly through the response.
func (h *HTTPTransport) Write(_ context.Context, m *Message) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	select {
	case h.queue <- m:
		return nil
	default:
		return errors.New("mcp: http transport queue full")
	}
}

func (h *HTTPTransport) Close() error { return nil }

// ServeHTTP turns one HTTP POST into one JSON-RPC round trip. It runs
// independent of Server.Serve; each request gets a freshly initialized
// Server-style dispatch path.
func (h *HTTPTransport) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Single-request dispatch only (no batches in v0).
	var msg Message
	if err := json.Unmarshal(body, &msg); err != nil {
		http.Error(w, fmt.Sprintf("parse: %v", err), http.StatusBadRequest)
		return
	}

	rt := newReplyCapture()
	// Lazily mark the server as initialized; HTTP clients are typically
	// short-lived (one request → one response) so the initialize/
	// initialized handshake doesn't carry meaning across requests.
	h.server.mu.Lock()
	h.server.initialized = true
	h.server.mu.Unlock()

	h.server.dispatch(r.Context(), rt, &msg)
	reply := rt.Captured()
	if reply == nil {
		// Notifications get no response.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	_ = enc.Encode(reply)
}

// replyCapture is a single-shot Transport that records the first frame
// the server writes and discards the rest. The HTTP handler then
// returns the captured frame as the response body.
type replyCapture struct {
	mu   sync.Mutex
	out  *Message
	done bool
}

func newReplyCapture() *replyCapture { return &replyCapture{} }

func (r *replyCapture) Read(ctx context.Context) (*Message, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (r *replyCapture) Write(_ context.Context, m *Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return nil
	}
	r.out = m
	r.done = true
	return nil
}

func (r *replyCapture) Close() error { return nil }

func (r *replyCapture) Captured() *Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.out
}
