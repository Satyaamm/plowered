package mcp_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Satyaamm/plowered/pkg/mcp"
)

// memoryTransport is a synchronous Transport for tests: writes append to
// outbound, reads pop from inbound. Closing inbound returns io.EOF.
type memoryTransport struct {
	inbound  []*mcp.Message
	outbound []*mcp.Message
	closed   bool
}

func (m *memoryTransport) Read(ctx context.Context) (*mcp.Message, error) {
	if len(m.inbound) == 0 {
		m.closed = true
		return nil, errEOF
	}
	msg := m.inbound[0]
	m.inbound = m.inbound[1:]
	return msg, nil
}

func (m *memoryTransport) Write(_ context.Context, msg *mcp.Message) error {
	m.outbound = append(m.outbound, msg)
	return nil
}

func (m *memoryTransport) Close() error { return nil }

var errEOF = io.EOF

// import without cycle by aliasing
import "io"

func TestInitializeAndToolsList(t *testing.T) {
	srv := mcp.NewServer(mcp.ServerInfo{Name: "plowered-mcp", Version: "test"})
	srv.Tools.MustRegister(mcp.Tool{
		Name:        "search_assets",
		Description: "Search the catalog by keyword",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
	}, func(_ context.Context, _ json.RawMessage) (mcp.CallToolResult, error) {
		return mcp.TextResult("ok"), nil
	})

	tr := &memoryTransport{
		inbound: []*mcp.Message{
			rpc(1, "initialize", `{"protocolVersion":"2024-11-05","clientInfo":{"name":"test","version":"0"}}`),
			rpc(2, "tools/list", `{}`),
			rpc(3, "tools/call", `{"name":"search_assets","arguments":{"query":"orders"}}`),
			rpc(4, "shutdown", `{}`),
		},
	}
	if err := srv.Serve(context.Background(), tr); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if len(tr.outbound) != 4 {
		t.Fatalf("outbound = %d, want 4: %+v", len(tr.outbound), tr.outbound)
	}

	// initialize
	var initRes mcp.InitializeResult
	if err := json.Unmarshal(tr.outbound[0].Result, &initRes); err != nil {
		t.Fatalf("init result: %v", err)
	}
	if initRes.ServerInfo.Name != "plowered-mcp" {
		t.Errorf("server info = %+v", initRes.ServerInfo)
	}

	// tools/list
	var listRes struct {
		Tools []mcp.Tool `json:"tools"`
	}
	if err := json.Unmarshal(tr.outbound[1].Result, &listRes); err != nil {
		t.Fatalf("list result: %v", err)
	}
	if len(listRes.Tools) != 1 || listRes.Tools[0].Name != "search_assets" {
		t.Errorf("tools list = %+v", listRes.Tools)
	}

	// tools/call
	var callRes mcp.CallToolResult
	if err := json.Unmarshal(tr.outbound[2].Result, &callRes); err != nil {
		t.Fatalf("call result: %v", err)
	}
	if len(callRes.Content) != 1 || callRes.Content[0].Text != "ok" {
		t.Errorf("call content = %+v", callRes.Content)
	}
}

func TestUnknownToolReturnsError(t *testing.T) {
	srv := mcp.NewServer(mcp.ServerInfo{Name: "x", Version: "0"})
	tr := &memoryTransport{
		inbound: []*mcp.Message{
			rpc(1, "tools/call", `{"name":"missing","arguments":{}}`),
			rpc(2, "shutdown", `{}`),
		},
	}
	if err := srv.Serve(context.Background(), tr); err != nil {
		t.Fatal(err)
	}
	var res mcp.CallToolResult
	if err := json.Unmarshal(tr.outbound[0].Result, &res); err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Error("expected IsError = true for unknown tool")
	}
}

func TestUnknownMethodReturnsRPCError(t *testing.T) {
	srv := mcp.NewServer(mcp.ServerInfo{Name: "x", Version: "0"})
	tr := &memoryTransport{
		inbound: []*mcp.Message{
			rpc(1, "totally/unknown", `{}`),
			rpc(2, "shutdown", `{}`),
		},
	}
	if err := srv.Serve(context.Background(), tr); err != nil {
		t.Fatal(err)
	}
	if tr.outbound[0].Error == nil || tr.outbound[0].Error.Code != mcp.CodeMethodNotFound {
		t.Errorf("want method-not-found error, got %+v", tr.outbound[0].Error)
	}
}

func rpc(id int, method, params string) *mcp.Message {
	idBytes, _ := json.Marshal(id)
	return &mcp.Message{
		JSONRPC: mcp.JSONRPCVersion,
		ID:      idBytes,
		Method:  method,
		Params:  json.RawMessage(params),
	}
}
