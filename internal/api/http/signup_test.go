package http_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	apihttp "github.com/Satyaamm/plowered/internal/api/http"
	"github.com/Satyaamm/plowered/internal/api/middleware"
)

func newSignupServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/signup", apihttp.SignupHandler(apihttp.SignupConfig{
		AuthConfig: middleware.AuthConfig{
			HS256Secret: []byte("test-secret-must-be-non-empty"),
			Issuer:      "plowered",
			Audience:    "plowered",
		},
	}))
	return httptest.NewServer(mux)
}

func postJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	return resp
}

func TestSignupHappyPath(t *testing.T) {
	s := newSignupServer(t)
	defer s.Close()

	resp := postJSON(t, s.URL+"/v1/signup", apihttp.SignupRequest{
		Email:      "alice@example.com",
		TenantName: "Acme",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body apihttp.SignupResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Token == "" || body.TenantID == "" || body.UserID == "" {
		t.Errorf("incomplete response: %+v", body)
	}
	if body.ExpiresAt == 0 {
		t.Error("expires_at must be set")
	}
}

func TestSignupRejectsBadEmail(t *testing.T) {
	s := newSignupServer(t)
	defer s.Close()
	resp := postJSON(t, s.URL+"/v1/signup", apihttp.SignupRequest{Email: "not-an-email"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestSignupRefusesWithoutSigningKey(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/signup", apihttp.SignupHandler(apihttp.SignupConfig{
		AuthConfig: middleware.AuthConfig{}, // no key
	}))
	s := httptest.NewServer(mux)
	defer s.Close()

	resp := postJSON(t, s.URL+"/v1/signup", apihttp.SignupRequest{Email: "a@b.com"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}
