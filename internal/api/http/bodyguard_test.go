package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func bodyGuardEcho(t *testing.T, max int64) http.Handler {
	t.Helper()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v map[string]any
		if err := decodeJSON(r, &v); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_json", err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, v)
	})
	return BodyGuardMW(max)(inner)
}

func TestBodyGuardAcceptsValidJSON(t *testing.T) {
	h := bodyGuardEcho(t, 1024)
	req := httptest.NewRequest("POST", "/x", strings.NewReader(`{"a":1}`))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body)
	}
}

func TestBodyGuardRejectsWrongMediaType(t *testing.T) {
	// A "file upload" disguised as anything but JSON is refused before
	// a byte is read — covers corrupted files, executables, multipart.
	for _, ct := range []string{
		"application/octet-stream",
		"multipart/form-data; boundary=x",
		"text/html",
		"image/png",
	} {
		req := httptest.NewRequest("POST", "/x", strings.NewReader("MZ\x90\x00garbage"))
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		bodyGuardEcho(t, 1024).ServeHTTP(rec, req)
		if rec.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("ct=%q: want 415, got %d", ct, rec.Code)
		}
	}
}

func TestBodyGuardRejectsDeclaredOversize(t *testing.T) {
	big := strings.Repeat("a", 2048)
	req := httptest.NewRequest("POST", "/x", strings.NewReader(`{"a":"`+big+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	bodyGuardEcho(t, 64).ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d", rec.Code)
	}
}

func TestBodyGuardCapsChunkedOversize(t *testing.T) {
	// No Content-Length (chunked): the MaxBytesReader backstop trips
	// mid-decode and surfaces as a clean 400, not an OOM.
	big := strings.Repeat("a", 2048)
	req := httptest.NewRequest("POST", "/x", strings.NewReader(`{"a":"`+big+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = -1
	rec := httptest.NewRecorder()
	bodyGuardEcho(t, 64).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "exceeds") {
		t.Fatalf("want size-limit message, got %s", rec.Body)
	}
}

func TestDecodeJSONRejectsTrailingGarbage(t *testing.T) {
	req := httptest.NewRequest("POST", "/x", strings.NewReader(`{"a":1}{"b":2}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	bodyGuardEcho(t, 1024).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestBodyGuardDropsBodyOnGET(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v map[string]any
		err := json.NewDecoder(r.Body).Decode(&v)
		// The body must be unreadable (capped at zero bytes).
		if err == nil {
			t.Fatal("GET body should be capped to zero")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest("GET", "/x", strings.NewReader(`{"a":1}`))
	rec := httptest.NewRecorder()
	BodyGuardMW(1024)(inner).ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d", rec.Code)
	}
}
