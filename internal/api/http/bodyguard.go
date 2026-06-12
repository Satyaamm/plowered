package http

import (
	"net/http"
	"strings"
)

// BodyGuardMW is the request-body control gate. Every write request
// passes three checks before any handler reads a byte:
//
//  1. Size — Content-Length over the cap is rejected with 413 up
//     front; chunked/lying clients hit the same cap mid-read via
//     http.MaxBytesReader, which decodeJSON maps to a clean error
//     instead of an OOM.
//  2. Media type — bodies must declare application/json (an optional
//     charset suffix is fine). Anything else (multipart, octet-stream,
//     text/html, a virus-laden "image") is refused with 415 before
//     it's read. There are deliberately NO binary-upload endpoints on
//     this API today; when one lands, it gets an explicit carve-out
//     here plus content sniffing at the handler (magic bytes, not
//     extension or declared type).
//  3. Read-method hygiene — GET/HEAD/OPTIONS with a body smell like
//     request smuggling; the body is capped to zero so middleboxes
//     and handlers can't disagree about it.
//
// nginx enforces client_max_body_size at the edge; this is the
// in-process backstop so a direct hit on the container (or a dev
// deployment with no nginx) gets the same posture.
func BodyGuardMW(maxBytes int64) func(http.Handler) http.Handler {
	if maxBytes <= 0 {
		maxBytes = 2 << 20 // 2 MiB — generous for a JSON API
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				// No legitimate read-method request carries a body.
				r.Body = http.MaxBytesReader(w, r.Body, 0)
				next.ServeHTTP(w, r)
				return
			}

			// Cheap early reject when the client declares its size.
			if r.ContentLength > maxBytes {
				writeJSON(w, http.StatusRequestEntityTooLarge, errorBody{
					"body_too_large", "request body exceeds the limit",
				})
				return
			}

			// Content-type gate — only when a body is actually present.
			// ContentLength of -1 means chunked (unknown length): treat
			// as present.
			if r.ContentLength != 0 {
				ct := r.Header.Get("Content-Type")
				mediaType := strings.TrimSpace(strings.SplitN(ct, ";", 2)[0])
				if !strings.EqualFold(mediaType, "application/json") {
					writeJSON(w, http.StatusUnsupportedMediaType, errorBody{
						"unsupported_media_type", "this API accepts application/json bodies only",
					})
					return
				}
			}

			// Hard cap for chunked or mis-declared lengths. Reads past
			// the limit fail with *http.MaxBytesError, which decodeJSON
			// surfaces as a clean decode failure.
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}
