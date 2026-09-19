package middleware

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

var hexRequestID = regexp.MustCompile(`^[a-f0-9]{32}$`)

func TestRequestIDMiddlewarePreservesIncomingID(t *testing.T) {
	const incoming = "client-provided-request-id"

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := GetRequestID(r.Context())
		if got != incoming {
			t.Errorf("context request_id = %q, want %q", got, incoming)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-ID", incoming)
	rec := httptest.NewRecorder()

	RequestIDMiddleware(next).ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-ID"); got != incoming {
		t.Errorf("response X-Request-ID = %q, want %q", got, incoming)
	}
}

func TestRequestIDMiddlewareGeneratesWhenMissing(t *testing.T) {
	var contextID string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contextID = GetRequestID(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	RequestIDMiddleware(next).ServeHTTP(rec, req)

	headerID := rec.Header().Get("X-Request-ID")
	if headerID == "" {
		t.Fatal("expected auto-generated X-Request-ID in response header")
	}
	if !hexRequestID.MatchString(headerID) {
		t.Errorf("generated X-Request-ID %q is not a 32-char hex string", headerID)
	}
	if contextID != headerID {
		t.Errorf("context request_id = %q, want header value %q", contextID, headerID)
	}
}
