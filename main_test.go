package main

import (
	"context"
	"encoding/json"
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

func TestMetricParsing(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want *int
		ok   bool
	}{
		{"", nil, true},
		{"0", intPointer(0), true},
		{"7.25", intPointer(435), true},
		{"24", intPointer(1440), true},
		{"7.1", nil, false},
		{"24.25", nil, false},
	} {
		got, err := parseSleepHours(test.raw)
		if (err == nil) != test.ok || !equalOptionalInt(got, test.want) {
			t.Errorf("parseSleepHours(%q) = %v, %v; want %v, ok=%v", test.raw, got, err, test.want, test.ok)
		}
	}
}

func TestDueReminderSendsOncePerSubscription(t *testing.T) {
	store := testStore(t)
	if err := store.UpsertPushSubscription(PushSubscription{Endpoint: "https://push.example/one", P256DH: "key", Auth: "auth"}); err != nil {
		t.Fatal(err)
	}
	location, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	var sent int
	app := &App{
		store:    store,
		location: location,
		sendPush: func(_ context.Context, _ PushSubscription, payload []byte) (int, error) {
			var message pushPayload
			if err := json.Unmarshal(payload, &message); err != nil {
				t.Fatal(err)
			}
			if message.URL != "/day?date=2026-11-15" || message.Body == "" {
				t.Fatalf("unexpected payload: %+v", message)
			}
			sent++
			return http.StatusCreated, nil
		},
	}
	now := time.Date(2026, 11, 15, 21, 0, 0, 0, location)
	if err := app.deliverDueReminders(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if err := app.deliverDueReminders(context.Background(), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if sent != 1 {
		t.Fatalf("sent %d reminders, want 1", sent)
	}
}

func intPointer(value int) *int { return &value }

func equalOptionalInt(left, right *int) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func TestTrustedHeaderAuth(t *testing.T) {
	app := &App{
		sessionKey:    []byte("test-key"),
		trustedHeader: http.CanonicalHeaderKey("X-authentik-username"),
		now:           func() time.Time { return time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC) },
	}

	withHeader := httptest.NewRequest("GET", "/day", nil)
	withHeader.Header.Set("X-Authentik-Username", "lamiphil")
	if !app.authenticated(withHeader) {
		t.Fatal("trusted header with a non-empty value was rejected")
	}

	empty := httptest.NewRequest("GET", "/day", nil)
	empty.Header.Set("X-Authentik-Username", "")
	if app.authenticated(empty) {
		t.Fatal("empty trusted header was accepted")
	}

	missing := httptest.NewRequest("GET", "/day", nil)
	if app.authenticated(missing) {
		t.Fatal("missing trusted header was accepted without a cookie")
	}

	disabled := &App{sessionKey: []byte("test-key"), now: app.now}
	spoofed := httptest.NewRequest("GET", "/day", nil)
	spoofed.Header.Set("X-Authentik-Username", "lamiphil")
	if disabled.authenticated(spoofed) {
		t.Fatal("disabled trusted header still authenticated a header-only request")
	}
}
