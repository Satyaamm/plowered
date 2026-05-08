package middleware

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/Satyaamm/plowered/internal/core/auth"
)

// AuthConfig configures the Auth interceptor.
type AuthConfig struct {
	// HS256Secret enables symmetric verification when non-empty. Dev only.
	HS256Secret []byte
	// RS256PublicKey enables RSA verification when non-nil.
	RS256PublicKey *rsa.PublicKey
	// Issuer and Audience are checked against `iss` / `aud` claims.
	Issuer   string
	Audience string
	// SkipMethods bypass auth entirely (e.g. health checks reflected over gRPC).
	SkipMethods map[string]bool
	// DevPrincipal, if set, is injected without any token verification. The
	// process refuses to start with this set when PLOWERED_ENV=production.
	DevPrincipal *auth.Principal
}

// LoadAuthConfigFromEnv builds AuthConfig from environment variables.
//
//	PLOWERED_JWT_HS256_SECRET   — dev/symmetric secret
//	PLOWERED_JWT_RS256_PUB_KEY  — PEM contents OR @/path/to/key.pem
//	PLOWERED_JWT_ISSUER         — expected `iss`
//	PLOWERED_JWT_AUDIENCE       — expected `aud` (default "plowered")
//	PLOWERED_AUTH_DEV_PRINCIPAL — JSON principal for local dev
func LoadAuthConfigFromEnv() (AuthConfig, error) {
	cfg := AuthConfig{
		Issuer:   os.Getenv("PLOWERED_JWT_ISSUER"),
		Audience: getenvDefault("PLOWERED_JWT_AUDIENCE", "plowered"),
	}
	if s := os.Getenv("PLOWERED_JWT_HS256_SECRET"); s != "" {
		cfg.HS256Secret = []byte(s)
	}
	if pem := os.Getenv("PLOWERED_JWT_RS256_PUB_KEY"); pem != "" {
		key, err := loadRSAPublicKey(pem)
		if err != nil {
			return cfg, fmt.Errorf("RS256 public key: %w", err)
		}
		cfg.RS256PublicKey = key
	}
	if dev := os.Getenv("PLOWERED_AUTH_DEV_PRINCIPAL"); dev != "" {
		if os.Getenv("PLOWERED_ENV") == "production" {
			return cfg, errors.New("PLOWERED_AUTH_DEV_PRINCIPAL refused in production")
		}
		var p auth.Principal
		if err := json.Unmarshal([]byte(dev), &p); err != nil {
			return cfg, fmt.Errorf("PLOWERED_AUTH_DEV_PRINCIPAL: %w", err)
		}
		cfg.DevPrincipal = &p
	}
	return cfg, nil
}

// Auth verifies the bearer token (or honors dev principal) and stores the
// resulting auth.Principal on the context.
func Auth(cfg AuthConfig) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if cfg.SkipMethods[info.FullMethod] {
			return handler(ctx, req)
		}
		if cfg.DevPrincipal != nil {
			slog.WarnContext(ctx, "DEV principal injected — do not run this in production",
				"method", info.FullMethod, "user", cfg.DevPrincipal.ID)
			ctx = auth.WithPrincipal(ctx, *cfg.DevPrincipal)
			return handler(ctx, req)
		}
		token, err := bearerFromMetadata(ctx)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, err.Error())
		}
		p, err := cfg.verify(token)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, err.Error())
		}
		ctx = auth.WithPrincipal(ctx, p)
		return handler(ctx, req)
	}
}

// VerifyToken parses and verifies a JWT against the supplied AuthConfig and
// returns the resulting Principal. Exposed so the HTTP middleware can reuse
// the same JWT semantics as the gRPC interceptor.
func VerifyToken(c AuthConfig, rawToken string) (auth.Principal, error) {
	return c.verify(rawToken)
}

func (c AuthConfig) verify(rawToken string) (auth.Principal, error) {
	keyfunc := func(t *jwt.Token) (any, error) {
		switch t.Method.Alg() {
		case "HS256":
			if len(c.HS256Secret) == 0 {
				return nil, errors.New("HS256 not configured")
			}
			return c.HS256Secret, nil
		case "RS256":
			if c.RS256PublicKey == nil {
				return nil, errors.New("RS256 not configured")
			}
			return c.RS256PublicKey, nil
		default:
			return nil, fmt.Errorf("unsupported alg: %s", t.Method.Alg())
		}
	}

	parsed, err := jwt.Parse(rawToken, keyfunc,
		jwt.WithValidMethods([]string{"HS256", "RS256"}),
		jwt.WithIssuer(c.Issuer),
		jwt.WithAudience(c.Audience),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return auth.Principal{}, fmt.Errorf("invalid token: %w", err)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok || !parsed.Valid {
		return auth.Principal{}, errors.New("invalid claims")
	}
	return principalFromClaims(claims)
}

func principalFromClaims(c jwt.MapClaims) (auth.Principal, error) {
	sub, _ := c["sub"].(string)
	if sub == "" {
		return auth.Principal{}, errors.New("`sub` claim required")
	}
	tid, _ := c["tid"].(string)
	if tid == "" {
		return auth.Principal{}, errors.New("`tid` claim required")
	}
	email, _ := c["email"].(string)

	roles := stringSlice(c["roles"])
	groups := stringSlice(c["groups"])

	return auth.Principal{
		ID:       sub,
		Email:    email,
		TenantID: tid,
		Roles:    roles,
		Groups:   groups,
	}, nil
}

func stringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func bearerFromMetadata(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", errors.New("missing metadata")
	}
	vals := md.Get("authorization")
	if len(vals) == 0 {
		return "", errors.New("missing authorization header")
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(vals[0], prefix) {
		return "", errors.New("authorization must be Bearer")
	}
	return strings.TrimPrefix(vals[0], prefix), nil
}

func loadRSAPublicKey(spec string) (*rsa.PublicKey, error) {
	var raw []byte
	if strings.HasPrefix(spec, "@") {
		b, err := os.ReadFile(strings.TrimPrefix(spec, "@"))
		if err != nil {
			return nil, err
		}
		raw = b
	} else {
		raw = []byte(spec)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("not a PEM-encoded key")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("not an RSA key")
	}
	return rsaPub, nil
}

func getenvDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
