package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRootRedirectsToDataJSON(t *testing.T) {
	h := New(func() Node { return Node{Text: "host"} })

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusTemporaryRedirect)
	}
	if got := rr.Header().Get("Location"); got != "/data.json" {
		t.Fatalf("location = %q, want %q", got, "/data.json")
	}
}

func TestDataJSONServesJSON(t *testing.T) {
	h := New(func() Node { return Node{Text: "host"} })

	req := httptest.NewRequest(http.MethodGet, "/data.json", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if got := rr.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type = %q, want %q", got, "application/json")
	}
}

func TestUnknownPathReturnsNotFound(t *testing.T) {
	h := New(func() Node { return Node{Text: "host"} })

	req := httptest.NewRequest(http.MethodGet, "/nope", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}
