package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Recovery converts panics in handlers into gRPC Internal errors and logs
// the stack. Without this, a panic crashes the server process.
func Recovery() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				slog.ErrorContext(ctx, "panic in grpc handler",
					"method", info.FullMethod,
					"panic", fmt.Sprintf("%v", r),
					"stack", string(debug.Stack()),
				)
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return handler(ctx, req)
	}
}

// StreamRecovery is the streaming-RPC counterpart.
func StreamRecovery() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if r := recover(); r != nil {
				slog.ErrorContext(ss.Context(), "panic in grpc stream handler",
					"method", info.FullMethod,
					"panic", fmt.Sprintf("%v", r),
					"stack", string(debug.Stack()),
				)
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return handler(srv, ss)
	}
}
