package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// ToolHandler is the function that implements a single MCP tool. It receives
// the raw JSON arguments (validated against InputSchema by the runtime) and
// returns either a success result or an error. Returning an error makes the
// runtime emit a CallToolResult with IsError=true.
type ToolHandler func(ctx context.Context, args json.RawMessage) (CallToolResult, error)

// ToolRegistration ties a Tool definition to its handler.
type ToolRegistration struct {
	Tool    Tool
	Handler ToolHandler
}

// ToolRegistry is a thread-safe map of name → registration.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]ToolRegistration
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]ToolRegistration)}
}

func (r *ToolRegistry) Register(t Tool, h ToolHandler) error {
	if t.Name == "" || h == nil {
		return errors.New("mcp: tool name and handler required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[t.Name]; exists {
		return fmt.Errorf("mcp: tool %q already registered", t.Name)
	}
	r.tools[t.Name] = ToolRegistration{Tool: t, Handler: h}
	return nil
}

func (r *ToolRegistry) MustRegister(t Tool, h ToolHandler) {
	if err := r.Register(t, h); err != nil {
		panic(err)
	}
}

// List returns tool definitions in lexical order by name.
func (r *ToolRegistry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.tools))
	for _, reg := range r.tools {
		out = append(out, reg.Tool)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Call invokes a tool by name. Returns ErrUnknownTool when not found.
func (r *ToolRegistry) Call(ctx context.Context, name string, args json.RawMessage) (CallToolResult, error) {
	r.mu.RLock()
	reg, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return CallToolResult{}, fmt.Errorf("%w: %s", ErrUnknownTool, name)
	}
	return reg.Handler(ctx, args)
}

var ErrUnknownTool = errors.New("mcp: unknown tool")

// TextResult is a convenience constructor for the common case: a single
// text content part, success.
func TextResult(text string) CallToolResult {
	return CallToolResult{
		Content: []ContentPart{{Type: "text", Text: text}},
	}
}

// ErrorResult is a convenience constructor for tool-side failures.
func ErrorResult(format string, args ...any) CallToolResult {
	return CallToolResult{
		Content: []ContentPart{{Type: "text", Text: fmt.Sprintf(format, args...)}},
		IsError: true,
	}
}
