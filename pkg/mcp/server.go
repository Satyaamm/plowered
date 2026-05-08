package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
)

// Server runs a single MCP session over a Transport. One Server instance
// equals one client connection; multi-client serving is the Transport's
// concern.
type Server struct {
	Info  ServerInfo
	Tools *ToolRegistry

	// Logger is used for protocol-level diagnostics. It MUST NOT write to the
	// stdio Transport's stream, otherwise log lines will corrupt the wire
	// protocol. Default is a stderr handler.
	Logger *slog.Logger

	mu          sync.Mutex
	initialized bool
	shutdown    bool
}

// NewServer returns a Server preconfigured with an empty tool registry.
func NewServer(info ServerInfo) *Server {
	return &Server{
		Info:   info,
		Tools:  NewToolRegistry(),
		Logger: slog.Default(),
	}
}

// Transport reads and writes JSON-RPC 2.0 frames. The stdio impl uses
// newline-delimited JSON; SSE/HTTP impls would frame differently.
type Transport interface {
	Read(ctx context.Context) (*Message, error)
	Write(ctx context.Context, msg *Message) error
	Close() error
}

// Serve reads messages from the transport and dispatches them. It exits
// cleanly on EOF, on ctx cancel, or after a `shutdown` request.
func (s *Server) Serve(ctx context.Context, t Transport) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		msg, err := t.Read(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("mcp: read: %w", err)
		}
		if msg.Method == "" && msg.Result == nil && msg.Error == nil {
			continue // ignore malformed
		}
		s.dispatch(ctx, t, msg)
		s.mu.Lock()
		done := s.shutdown
		s.mu.Unlock()
		if done {
			return nil
		}
	}
}

func (s *Server) dispatch(ctx context.Context, t Transport, msg *Message) {
	// Notifications have no `id` and expect no reply.
	isNotification := len(msg.ID) == 0

	switch msg.Method {
	case "initialize":
		s.respond(ctx, t, msg, s.handleInitialize(msg.Params))
	case "notifications/initialized":
		s.mu.Lock()
		s.initialized = true
		s.mu.Unlock()
	case "tools/list":
		s.respond(ctx, t, msg, s.handleToolsList())
	case "tools/call":
		s.respond(ctx, t, msg, s.handleToolsCall(ctx, msg.Params))
	case "shutdown":
		s.mu.Lock()
		s.shutdown = true
		s.mu.Unlock()
		s.respond(ctx, t, msg, struct{}{})
	default:
		if !isNotification {
			s.respondError(ctx, t, msg, CodeMethodNotFound, "method not found: "+msg.Method)
		}
	}
}

// ----- handlers -----

func (s *Server) handleInitialize(params json.RawMessage) any {
	var p InitializeParams
	_ = json.Unmarshal(params, &p) // tolerant: missing fields → zero values
	return InitializeResult{
		ProtocolVersion: ProtocolVersion,
		Capabilities: ServerCapabilities{
			Tools: &ToolsCapability{ListChanged: false},
		},
		ServerInfo: s.Info,
	}
}

func (s *Server) handleToolsList() any {
	return map[string]any{"tools": s.Tools.List()}
}

func (s *Server) handleToolsCall(ctx context.Context, params json.RawMessage) any {
	var p CallToolParams
	if err := json.Unmarshal(params, &p); err != nil {
		return ErrorResult("invalid params: %v", err)
	}
	res, err := s.Tools.Call(ctx, p.Name, p.Arguments)
	if err != nil {
		return ErrorResult("%v", err)
	}
	return res
}

// ----- wire helpers -----

func (s *Server) respond(ctx context.Context, t Transport, req *Message, result any) {
	if len(req.ID) == 0 {
		return // notification — no response
	}
	body, err := json.Marshal(result)
	if err != nil {
		s.respondError(ctx, t, req, CodeInternalError, err.Error())
		return
	}
	resp := &Message{JSONRPC: JSONRPCVersion, ID: req.ID, Result: body}
	if err := t.Write(ctx, resp); err != nil {
		s.Logger.Error("mcp: write response", "method", req.Method, "err", err)
	}
}

func (s *Server) respondError(ctx context.Context, t Transport, req *Message, code int, msg string) {
	if len(req.ID) == 0 {
		return
	}
	resp := &Message{
		JSONRPC: JSONRPCVersion,
		ID:      req.ID,
		Error:   &Error{Code: code, Message: msg},
	}
	if err := t.Write(ctx, resp); err != nil {
		s.Logger.Error("mcp: write error response", "code", code, "err", err)
	}
}

// ----- StdioTransport -----

// StdioTransport reads newline-delimited JSON from `in` and writes to `out`.
// Use os.Stdin / os.Stdout in cmd/plowered-mcp.
type StdioTransport struct {
	in  *bufio.Reader
	out io.Writer
	mu  sync.Mutex
}

func NewStdioTransport(in io.Reader, out io.Writer) *StdioTransport {
	return &StdioTransport{in: bufio.NewReader(in), out: out}
}

func (s *StdioTransport) Read(ctx context.Context) (*Message, error) {
	type result struct {
		msg *Message
		err error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := s.in.ReadBytes('\n')
		if err != nil && len(line) == 0 {
			ch <- result{nil, err}
			return
		}
		var m Message
		if err := json.Unmarshal(line, &m); err != nil {
			ch <- result{nil, fmt.Errorf("parse: %w", err)}
			return
		}
		ch <- result{&m, nil}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		return r.msg, r.err
	}
}

func (s *StdioTransport) Write(_ context.Context, msg *Message) error {
	if msg.JSONRPC == "" {
		msg.JSONRPC = JSONRPCVersion
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.out.Write(body); err != nil {
		return err
	}
	_, err = s.out.Write([]byte{'\n'})
	return err
}

func (s *StdioTransport) Close() error { return nil }
