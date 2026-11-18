package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLoopbackOrigins(t *testing.T) {
	for _, test := range []struct {
		origin string
		host   string
		want   bool
	}{
		{"http://127.0.0.1:8080", "127.0.0.1:8080", true},
		{"http://localhost:8080", "127.0.0.1:8080", true},
		{"http://[::1]:8080", "localhost:8080", true},
		{"null", "127.0.0.1:8080", true},
		{"null", "praxis.homelab", false},
		{"http://localhost:9000", "127.0.0.1:8080", false},
		{"https://example.com", "127.0.0.1:8080", false},
	} {
		if got := sameOriginHost(test.origin, test.host); got != test.want {
			t.Errorf("sameOriginHost(%q, %q) = %v, want %v", test.origin, test.host, got, test.want)
		}
	}
}

func TestSignedSession(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	app := &App{sessionKey: []byte("test-key"), now: func() time.Time { return now }}

	request := httptest.NewRequest("GET", "/day", nil)
	request.AddCookie(&http.Cookie{Name: "praxis_session", Value: app.signSession(now.Add(time.Hour))})
	if !app.authenticated(request) {
		t.Fatal("valid signed session was rejected")
	}

	expired := httptest.NewRequest("GET", "/day", nil)
	expired.AddCookie(&http.Cookie{Name: "praxis_session", Value: app.signSession(now.Add(-time.Hour))})
	if app.authenticated(expired) {
		t.Fatal("expired session was accepted")
	}

	tampered := httptest.NewRequest("GET", "/day", nil)
	tampered.AddCookie(&http.Cookie{Name: "praxis_session", Value: "9999999999.invalid"})
	if app.authenticated(tampered) {
		t.Fatal("tampered session was accepted")
	}
}
