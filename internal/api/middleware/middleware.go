// Package middleware provides gRPC server interceptors for cross-cutting
// concerns: panic recovery, request IDs, structured logging, JWT-based
// authentication, tenant extraction, and per-tenant rate limiting.
//
// The interceptors are independent and composable. The recommended outer→
// inner order is:
//
//	Recovery → RequestID → Logging → RateLimit → Auth → Tenant → handler
//
// Use grpc.ChainUnaryInterceptor / grpc.ChainStreamInterceptor to install
// them on the server. Recovery is first so it catches panics in everything
// downstream; Tenant is last so handlers see a fully-authenticated context.
package middleware
