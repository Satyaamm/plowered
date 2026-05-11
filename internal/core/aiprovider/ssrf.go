package aiprovider

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"
)

// SSRF guard for BYOM base_url. Without it a tenant admin could point
// the platform at:
//
//	http://127.0.0.1:5432        — internal Postgres
//	http://169.254.169.254       — cloud metadata (steals IAM creds!)
//	http://10.0.0.5/admin        — internal corp networks
//	http://[::1]:6379            — IPv6 loopback / private services
//
// We guard at two layers:
//
//  1. Up-front URL validation (cheap rejection of "obviously bad" hosts).
//  2. A custom DialContext that re-resolves and re-validates on every
//     connection so a hostile DNS server can't return a public IP at
//     validation time and a private IP at dial time (DNS rebinding).
//
// The env override PLOWERED_ALLOW_PRIVATE_AI_HOSTS=1 disables the guard
// so a developer can hit an Ollama at localhost:11434. Production
// deployments leave it off.

// ErrBlockedHost is the sentinel error returned when an upstream URL
// resolves to a forbidden IP. The HTTP layer translates it to a 400.
var ErrBlockedHost = errors.New("aiprovider: upstream host is on the blocklist (private / loopback / metadata)")

// allowPrivateHosts mirrors the env var. Read once at process start —
// flipping it mid-flight isn't a use case worth supporting.
var allowPrivateHosts = os.Getenv("PLOWERED_ALLOW_PRIVATE_AI_HOSTS") == "1"

// ValidateBaseURL parses raw, resolves its host, and checks every
// returned IP. Returns ErrBlockedHost if any IP is forbidden. Used by
// the create/update HTTP handler so a bad config never lands in the DB.
func ValidateBaseURL(ctx context.Context, raw string) error {
	if allowPrivateHosts {
		return nil
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https, got %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("url is missing a hostname")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", host, err)
	}
	for _, ip := range ips {
		addr, ok := netip.AddrFromSlice(ip.IP)
		if !ok {
			continue
		}
		if forbidden(addr) {
			return ErrBlockedHost
		}
	}
	return nil
}

// forbidden reports whether the given IP falls into any blocked range.
// Centralised so the up-front check and the dial-time check stay in
// lockstep.
func forbidden(addr netip.Addr) bool {
	addr = addr.Unmap() // collapse ::ffff:127.0.0.1 to 127.0.0.1
	switch {
	case addr.IsLoopback():
		return true
	case addr.IsPrivate(): // RFC 1918 + RFC 4193 (fc00::/7)
		return true
	case addr.IsLinkLocalUnicast(), addr.IsLinkLocalMulticast():
		return true
	case addr.IsMulticast():
		return true
	case addr.IsUnspecified(): // 0.0.0.0, ::
		return true
	}
	// AWS / GCP / Azure instance metadata endpoint.
	if addr == netip.MustParseAddr("169.254.169.254") {
		return true
	}
	// Carrier-grade NAT (RFC 6598) — sometimes used internally by ISPs
	// and providers; not a public network. Guard the As4() call: it
	// panics on a non-v4 address, so the Is4() check has to come first.
	if addr.Is4() {
		v4 := addr.As4()
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return true
		}
	}
	return false
}

// SafeHTTPClient returns an http.Client whose Dialer re-validates every
// resolved IP at connect time. Use it for outbound requests against a
// tenant-supplied base_url so a hostile DNS server can't bypass the
// up-front ValidateBaseURL check by returning a public IP first then a
// private IP a second later.
func SafeHTTPClient(timeout time.Duration) *http.Client {
	if allowPrivateHosts {
		return &http.Client{Timeout: timeout}
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				naddr, ok := netip.AddrFromSlice(ip.IP)
				if !ok {
					continue
				}
				if forbidden(naddr) {
					return nil, ErrBlockedHost
				}
			}
			// Hand the first IP to the underlying dialer so DNS isn't
			// re-resolved by the OS resolver between our check and
			// the syscall.
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        10,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
	}
	return &http.Client{Transport: tr, Timeout: timeout}
}
