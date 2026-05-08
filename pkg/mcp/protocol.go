// Package mcp implements the server side of the Model Context Protocol.
// MCP rides on JSON-RPC 2.0; messages are exchanged as line-delimited JSON
// objects over a Transport (typically stdio for local agents, SSE/HTTP for
// remote ones).
//
// This package implements the subset Plowered needs for v0:
//
//	initialize           — handshake; returns server info + capabilities
//	notifications/initialized — client confirms readiness
//	tools/list           — enumerate callable tools
//	tools/call           — invoke a tool by name
//	shutdown             — graceful close
//
// Resources, prompts, sampling, and roots will be added as the MCP surface
// expands.
package mcp

import "encoding/json"

const (
	ProtocolVersion = "2024-11-05"
	JSONRPCVersion  = "2.0"
)

// Message is a JSON-RPC 2.0 envelope. Either Method (request/notification)
// or Result/Error (response) is set.
type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Error matches JSON-RPC 2.0 error objects.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// InitializeParams is what the client sends in `initialize`.
type InitializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ClientInfo      ClientInfo     `json:"clientInfo"`
}

type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult is what the server replies with.
type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      ServerInfo         `json:"serverInfo"`
}

type ServerCapabilities struct {
	Tools     *ToolsCapability `json:"tools,omitempty"`
	Resources *ResCapability   `json:"resources,omitempty"`
}

type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type ResCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Tool describes a single callable tool surfaced by tools/list.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// CallToolParams is the body of tools/call.
type CallToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// CallToolResult is the response body. Content is a list of typed parts —
// for v0 we only emit text parts.
type CallToolResult struct {
	Content []ContentPart `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type ContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}
