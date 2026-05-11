package aiprovider

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestValidateBaseURL_BlockedRanges asserts the SSRF guard rejects
// every IP class an attacker would point at to reach internal services
// or steal cloud-metadata credentials.
func TestValidateBaseURL_BlockedRanges(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"loopback v4", "http://127.0.0.1:5432"},
		{"loopback v4 alt", "http://127.255.255.254"},
		{"loopback v6", "http://[::1]:6379"},
		{"loopback name", "http://localhost"},
		{"aws metadata", "http://169.254.169.254/latest/meta-data/iam/security-credentials/"},
		{"link-local v4", "http://169.254.1.1"},
		{"private 10.x", "http://10.0.0.5"},
		{"private 172.16.x", "http://172.16.0.1"},
		{"private 192.168.x", "http://192.168.1.1"},
		{"private v6 fc00", "http://[fc00::1]"},
		{"cgnat 100.64.x", "http://100.64.0.1"},
		{"unspecified v4", "http://0.0.0.0"},
		{"unspecified v6", "http://[::]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateBaseURL(context.Background(), tc.url)
			if err == nil {
				t.Fatalf("%s: expected ErrBlockedHost, got nil", tc.url)
			}
			if !errors.Is(err, ErrBlockedHost) && !strings.Contains(err.Error(), "blocklist") {
				// Hostname-based blocks (localhost) come through as a
				// resolve error if the resolver has no LAN; tolerate.
				if !strings.Contains(err.Error(), "blocklist") &&
					!strings.Contains(err.Error(), "resolve") {
					t.Fatalf("%s: expected blocklist or resolve error, got %v", tc.url, err)
				}
			}
		})
	}
}

func TestValidateBaseURL_BadScheme(t *testing.T) {
	for _, u := range []string{
		"file:///etc/passwd",
		"gopher://localhost",
		"javascript:alert(1)",
		"",
	} {
		if err := ValidateBaseURL(context.Background(), u); err == nil {
			t.Fatalf("expected rejection for %q, got nil", u)
		}
	}
}

func TestValidateBaseURL_PublicURL(t *testing.T) {
	// api.anthropic.com / api.openai.com both have public IPs. We don't
	// hit them — just resolve and check. The test relies on DNS being
	// available in CI, which is the case for go test on any reasonable
	// runner. Skip gracefully if DNS is offline.
	err := ValidateBaseURL(context.Background(), "https://api.anthropic.com")
	if err != nil {
		if strings.Contains(err.Error(), "resolve") {
			t.Skipf("DNS unavailable: %v", err)
		}
		t.Fatalf("public host rejected: %v", err)
	}
}
