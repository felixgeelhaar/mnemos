package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateLeadEmail(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"happy path", "alice@example.com", false},
		{"normalises case", "ALICE@EXAMPLE.COM", false},
		{"trims whitespace", "  bob@example.com  ", false},
		{"rejects empty", "", true},
		{"rejects whitespace only", "   ", true},
		{"rejects no domain", "alice", true},
		{"rejects no @", "alice.example.com", true},
		{"rejects CRLF (log injection)", "alice@example.com\r\nfake_log", true},
		{"rejects LF (log injection)", "alice@example.com\nfake", true},
		{"rejects embedded tab", "alice\t@example.com", true},
		{"rejects null byte", "alice@example.com\x00", true},
		{"rejects DEL byte", "alice@example.com\x7f", true},
		{"rejects over-long input", strings.Repeat("a", 250) + "@example.com", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := validateLeadEmail(c.raw)
			if c.wantErr && err == nil {
				t.Fatalf("expected error for %q", c.raw)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", c.raw, err)
			}
		})
	}
}

func TestLeadsRateLimit_Throttles(t *testing.T) {
	srv := httptest.NewServer(newServerMux(newServerTestStore_conn(t)))
	defer srv.Close()

	body := `{"email":"alice@example.com"}`
	// Burst budget = 10. Eleventh request should be throttled.
	var sawThrottled bool
	for i := 0; i < 12; i++ {
		resp, err := http.Post(srv.URL+"/v1/leads", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("post %d: %v", i, err)
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			sawThrottled = true
			if got := resp.Header.Get("Retry-After"); got == "" {
				t.Errorf("throttled response missing Retry-After header")
			}
			_ = resp.Body.Close()
			break
		}
		_ = resp.Body.Close()
	}
	if !sawThrottled {
		t.Fatalf("expected 11th+ request to be throttled but all succeeded")
	}
}

func TestLeadsHandler_RejectsInvalidEmail(t *testing.T) {
	srv := httptest.NewServer(newServerMux(newServerTestStore_conn(t)))
	defer srv.Close()

	cases := []struct {
		name string
		body string
	}{
		{"missing email", `{"email":""}`},
		{"no @", `{"email":"plainstring"}`},
		{"crlf injection", `{"email":"a@b.com\r\nfake"}`},
		{"unknown field", `{"email":"a@b.com","spam":"yes"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp, err := http.Post(srv.URL+"/v1/leads", "application/json", strings.NewReader(c.body))
			if err != nil {
				t.Fatalf("post: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (%s)", resp.StatusCode, c.name)
			}
		})
	}
}

func TestLeadsHandler_RejectsNonPost(t *testing.T) {
	srv := httptest.NewServer(newServerMux(newServerTestStore_conn(t)))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/leads")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}
