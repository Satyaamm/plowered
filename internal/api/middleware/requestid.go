package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type requestIDKey struct{}

const requestIDHeader = "x-request-id"

// RequestID assigns each incoming request a unique ID. If the client supplied
// `x-request-id` metadata, that value is honored; otherwise a fresh ID is
// generated. The ID is placed on the context for downstream handlers and
// logging interceptors.
func RequestID() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx = ensureRequestID(ctx)
		return handler(ctx, req)
	}
}

func StreamRequestID() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		_ = ensureRequestID(ss.Context())
		return handler(srv, ss)
	}
}

func ensureRequestID(ctx context.Context) context.Context {
	if id := RequestIDFromContext(ctx); id != "" {
		return ctx
	}
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get(requestIDHeader); len(vals) > 0 && vals[0] != "" {
			return context.WithValue(ctx, requestIDKey{}, vals[0])
		}
	}
	return context.WithValue(ctx, requestIDKey{}, newRequestID())
}

// RequestIDFromContext returns the request ID, or "" if none was set.
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey{}).(string); ok {
		return v
	}
	return ""
}

func newRequestID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "req-fallback"
	}
	return hex.EncodeToString(b[:])
}
