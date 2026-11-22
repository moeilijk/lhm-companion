package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRootRedirectsToDataJSON(t *testing.T) {
	h := New(func() Node { return Node{Text: "host"} }, nil)

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
	h := New(func() Node { return Node{Text: "host"} }, nil)

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
	h := New(func() Node { return Node{Text: "host"} }, nil)

	req := httptest.NewRequest(http.MethodGet, "/nope", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestSensorResetMinMax(t *testing.T) {
	const known = "/lpc/it8696e/0/temperature/0"
	for _, tc := range []struct {
		name, method, target string
		status               int
		result, message      string
		resets               int
	}{
		{name: "known sensor", method: http.MethodPost, target: "/Sensor?action=ResetMinMax&id=" + known, status: 200, result: "ok", resets: 1},
		{name: "unknown sensor", method: http.MethodPost, target: "/Sensor?action=ResetMinMax&id=/nope/0", status: 200, result: "fail", message: "Unknown id /nope/0 specified"},
		{name: "missing id", method: http.MethodPost, target: "/Sensor?action=ResetMinMax", status: 200, result: "fail", message: "No id provided"},
		{name: "missing action", method: http.MethodPost, target: "/Sensor?id=" + known, status: 200, result: "fail", message: "No action provided"},
		{name: "unknown action", method: http.MethodPost, target: "/Sensor?action=Set&id=" + known, status: 200, result: "fail", message: "Unknown action type Set"},
		{name: "GET is refused", method: http.MethodGet, target: "/Sensor?action=ResetMinMax&id=" + known, status: 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resets := 0
			h := New(func() Node { return Node{Text: "host"} }, func(id string) bool {
				if id != known {
					return false
				}
				resets++
				return true
			})
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, httptest.NewRequest(tc.method, tc.target, nil))
			if rr.Code != tc.status {
				t.Fatalf("status = %d, want %d", rr.Code, tc.status)
			}
			if resets != tc.resets {
				t.Fatalf("resets = %d, want %d", resets, tc.resets)
			}
			if tc.status != 200 {
				return
			}
			var reply struct{ Result, Message string }
			if err := json.Unmarshal(rr.Body.Bytes(), &reply); err != nil {
				t.Fatalf("reply is not JSON: %v (%s)", err, rr.Body.String())
			}
			if reply.Result != tc.result || reply.Message != tc.message {
				t.Fatalf("reply = %+v, want result %q message %q", reply, tc.result, tc.message)
			}
		})
	}
}

func TestSensorWithoutResetterFails(t *testing.T) {
	h := New(func() Node { return Node{Text: "host"} }, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/Sensor?action=ResetMinMax&id=/a/0", nil))
	if rr.Code != 200 || !json.Valid(rr.Body.Bytes()) {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	var reply struct{ Result string }
	_ = json.Unmarshal(rr.Body.Bytes(), &reply)
	if reply.Result != "fail" {
		t.Fatalf("result = %q, want fail", reply.Result)
	}
}
