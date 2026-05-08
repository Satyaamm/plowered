package middleware

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Satyaamm/plowered/internal/core/auth"
)

// Logging emits one structured log line per request.
//
// Fields: method, code, duration_ms, request_id, tenant_id, user_id.
// Errors are logged at WARN; codes.Internal and Unknown at ERROR.
func Logging(logger *slog.Logger) grpc.UnaryServerInterceptor {
	if logger == nil {
		logger = slog.Default()
	}
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		dur := time.Since(start)

		code := status.Code(err)
		level := slog.LevelInfo
		switch code {
		case codes.OK:
			// stays Info
		case codes.Internal, codes.Unknown, codes.DataLoss:
			level = slog.LevelError
		default:
			level = slog.LevelWarn
		}

		attrs := []slog.Attr{
			slog.String("method", info.FullMethod),
			slog.String("code", code.String()),
			slog.Int64("duration_ms", dur.Milliseconds()),
			slog.String("request_id", RequestIDFromContext(ctx)),
		}
		if p, ok := principalIfPresent(ctx); ok {
			attrs = append(attrs,
				slog.String("tenant_id", p.TenantID),
				slog.String("user_id", p.ID),
			)
		}
		if err != nil {
			attrs = append(attrs, slog.String("err", err.Error()))
		}
		logger.LogAttrs(ctx, level, "grpc", attrs...)
		return resp, err
	}
}

func principalIfPresent(ctx context.Context) (auth.Principal, bool) {
	p, err := auth.PrincipalFromContext(ctx)
	if err != nil {
		return auth.Principal{}, false
	}
	return p, true
}
