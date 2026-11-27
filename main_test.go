package main

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestServiceWorkerUsesRootScope(t *testing.T) {
	app := &App{}
	response := httptest.NewRecorder()
	app.serviceWorker(response, httptest.NewRequest(http.MethodGet, "/sw.js", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("service worker returned %d", response.Code)
	}
	if got := response.Header().Get("Service-Worker-Allowed"); got != "/" {
		t.Fatalf("service worker scope header = %q", got)
	}
	if response.Body.Len() == 0 {
		t.Fatal("service worker body is empty")
	}
}

func TestSettingsRouteContainsReminderAndTrendsDoesNot(t *testing.T) {
	store := testStore(t)
	templates, err := template.ParseFS(assets, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	location, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	app := &App{
		store:           store,
		templates:       templates,
		location:        location,
		trustedHeader:   "X-Authentik-Username",
		vapidPublicKey:  "public-key",
		vapidPrivateKey: "private-key",
		now:             func() time.Time { return time.Date(2026, 11, 25, 12, 0, 0, 0, location) },
	}
	mux, err := app.routes()
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		path            string
		wantReminder    bool
		wantCurrentLink string
		wantPushScript  bool
	}{
		{"/settings", true, `aria-current="page" href="/settings"`, true},
		{"/trends", false, `aria-current="page" href="/trends"`, false},
	} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		request.Header.Set("X-Authentik-Username", "lamiphil")
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		body := response.Body.String()
		if response.Code != http.StatusOK {
			t.Errorf("GET %s returned %d", test.path, response.Code)
		}
		if strings.Contains(body, "Notification à 20 h") != test.wantReminder {
			t.Errorf("GET %s reminder presence mismatch", test.path)
		}
		// The reminder is global since v0.4.0: Paramètres must no longer tell the
		// user to opt measurements in from the Mesures page.
		if test.wantReminder && strings.Contains(body, "Choisis les mesures") {
			t.Errorf("GET %s still carries the per-measurement reminder copy", test.path)
		}
		if !strings.Contains(body, `<span>Réglages</span>`) || !strings.Contains(body, test.wantCurrentLink) {
			t.Errorf("GET %s navigation is incomplete", test.path)
		}
		if strings.Contains(body, "/static/push.js") != test.wantPushScript {
			t.Errorf("GET %s push script presence mismatch", test.path)
		}
	}
}

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

func TestParseDecimal(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want *float64
		ok   bool
	}{
		{"", nil, true},
		{" 7.25 ", floatPointer(7.25), true},
		{"7,5", floatPointer(7.5), true},
		{"abc", nil, false},
		{"NaN", nil, false},
		{"Inf", nil, false},
	} {
		got, err := parseDecimal(test.raw)
		if (err == nil) != test.ok || (got == nil) != (test.want == nil) || got != nil && *got != *test.want {
			t.Errorf("parseDecimal(%q) = %v, %v", test.raw, got, err)
		}
	}
	sleep := Measurement{Step: 0.25, Min: 0, Max: 24}
	for value, want := range map[float64]bool{0: true, 7.25: true, 24: true, 7.1: false, 24.25: false, -0.25: false} {
		if sleep.ValidValue(value) != want {
			t.Errorf("ValidValue(%v) != %v", value, want)
		}
	}
	tenth := Measurement{Step: 0.1, Min: 0, Max: 1}
	if !tenth.ValidValue(0.3) || tenth.ValidValue(0.35) {
		t.Error("tenth precision validation is wrong")
	}
}

func testApp(t *testing.T) (*App, *http.ServeMux) {
	t.Helper()
	templates, err := template.ParseFS(assets, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	location, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	app := &App{
		store:         testStore(t),
		templates:     templates,
		location:      location,
		trustedHeader: "X-Authentik-Username",
		now:           func() time.Time { return time.Date(2026, 11, 15, 12, 0, 0, 0, location) },
	}
	mux, err := app.routes()
	if err != nil {
		t.Fatal(err)
	}
	return app, mux
}

func serve(t *testing.T, mux *http.ServeMux, method, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var request *http.Request
	if form != nil {
		request = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		request = httptest.NewRequest(method, path, nil)
	}
	request.Header.Set("X-Authentik-Username", "lamiphil")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	return response
}

func TestMeasurementRoutes(t *testing.T) {
	app, mux := testApp(t)
	response := serve(t, mux, http.MethodPost, "/measurements", url.Values{
		"name": {"Eau </script>"}, "unit": {"L"}, "step": {"0.25"}, "min": {"0"}, "max": {"6"}, "target_min": {"1,5"}, "target_max": {""},
	})
	if response.Code != http.StatusSeeOther {
		t.Fatalf("create measurement returned %d: %s", response.Code, response.Body)
	}
	water := measurementByKey(t, app.store, "eau-script")
	if water.TargetMin == nil || *water.TargetMin != 1.5 || water.TargetMax != nil || water.Reminder {
		t.Fatalf("unexpected created measurement: %+v", water)
	}
	if response := serve(t, mux, http.MethodPost, "/measurements", url.Values{"name": {"x"}, "step": {"1"}, "min": {"5"}, "max": {"1"}}); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid measurement returned %d", response.Code)
	}

	page := serve(t, mux, http.MethodGet, "/measurements", nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `aria-current="true" href="/settings"`) || !strings.Contains(page.Body.String(), "eau-script") {
		t.Fatalf("measurements page: %d", page.Code)
	}

	sleep := measurementByKey(t, app.store, "sommeil")
	id := strconv.FormatInt(water.ID, 10)
	sleepID := strconv.FormatInt(sleep.ID, 10)
	day := serve(t, mux, http.MethodPost, "/day", url.Values{
		"date": {"2026-11-15"}, "measurement": {id, sleepID}, "m_" + id: {"2,25"}, "m_" + sleepID: {"7.5"},
	})
	if day.Code != http.StatusSeeOther {
		t.Fatalf("save day returned %d: %s", day.Code, day.Body)
	}
	bad := serve(t, mux, http.MethodPost, "/day", url.Values{"date": {"2026-11-15"}, "measurement": {id}, "m_" + id: {"2.3"}})
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid value returned %d", bad.Code)
	}

	dayPage := serve(t, mux, http.MethodGet, "/day?date=2026-11-15", nil).Body.String()
	if !strings.Contains(dayPage, `name="m_`+id+`"`) || !strings.Contains(dayPage, `value="2.25"`) {
		t.Fatal("day page does not render the generic measurement")
	}
	trends := serve(t, mux, http.MethodGet, "/trends", nil).Body.String()
	if !strings.Contains(trends, `data-chart="eau-script"`) || !strings.Contains(trends, `Eau \u003c/script\u003e`) {
		t.Fatal("trends page does not safely render the generic measurement")
	}
	if strings.Count(trends, "</script>") != strings.Count(trends, "<script") {
		t.Fatal("user-defined name broke out of a script element")
	}

	csvBody := serve(t, mux, http.MethodGet, "/measurements.csv", nil).Body.String()
	if !strings.HasPrefix(csvBody, "date,mesure,nom,unite,valeur\n") || !strings.Contains(csvBody, "2026-11-15,sommeil,Sommeil,h,7.5\n") || !strings.Contains(csvBody, "2026-11-15,eau-script,Eau </script>,L,2.25\n") {
		t.Fatalf("unexpected measurements CSV:\n%s", csvBody)
	}
	legacy := serve(t, mux, http.MethodGet, "/export.csv", nil).Body.String()
	if !strings.HasPrefix(legacy, "date,note,habitude,faite,sommeil_heures,cafes\n") || !strings.Contains(legacy, ",7.5,\n") {
		t.Fatalf("unexpected legacy CSV:\n%s", legacy)
	}

	if response := serve(t, mux, http.MethodPost, "/measurements/delete", url.Values{"id": {id}}); response.Code != http.StatusBadRequest {
		t.Fatalf("deleting a measurement with values returned %d", response.Code)
	}
}

func TestTrendsRatingAndArchived(t *testing.T) {
	app, mux := testApp(t)

	// A measurement that will stay active, and one we will archive.
	for _, form := range []url.Values{
		{"name": {"Pas"}, "unit": {""}, "step": {"1"}, "min": {"0"}, "max": {"50000"}},
		{"name": {"Cafés"}, "unit": {""}, "step": {"1"}, "min": {"0"}, "max": {"10"}},
	} {
		if response := serve(t, mux, http.MethodPost, "/measurements", form); response.Code != http.StatusSeeOther {
			t.Fatalf("create measurement returned %d: %s", response.Code, response.Body)
		}
	}
	steps := measurementByKey(t, app.store, "pas")
	cafes := measurementByKey(t, app.store, "cafes")

	// Save a day on 2026-11-14 with rating 8, steps = 9000 and 2 coffees.
	stepsID := strconv.FormatInt(steps.ID, 10)
	cafesID := strconv.FormatInt(cafes.ID, 10)
	if response := serve(t, mux, http.MethodPost, "/day", url.Values{
		"date":        {"2026-11-14"},
		"rating":      {"8"},
		"measurement": {stepsID, cafesID},
		"m_" + stepsID: {"9000"},
		"m_" + cafesID: {"2"},
	}); response.Code != http.StatusSeeOther {
		t.Fatalf("save day returned %d: %s", response.Code, response.Body)
	}
	// Archive "Cafés" through the SaveMeasurement store (the form path keeps
	// the values; here we go straight to the store to keep the test short).
	cafes.Archived = true
	if err := app.store.SaveMeasurement(cafes); err != nil {
		t.Fatalf("archive cafes: %v", err)
	}

	body := serve(t, mux, http.MethodGet, "/trends", nil).Body.String()
	if !strings.Contains(body, `data-chart="__rating__"`) {
		t.Fatal("trends is missing the rating chart")
	}
	if !strings.Contains(body, `"rating":{"values":[`) {
		t.Fatal("trends data has no rating series")
	}
	if strings.Contains(body, `data-chart="cafes"`) {
		t.Fatal("trends shows archived measurement without the toggle")
	}
	if !strings.Contains(body, `href="/trends?range=30&amp;archived=1"`) {
		t.Fatal("trends is missing the archived toggle link")
	}

	withArchived := serve(t, mux, http.MethodGet, "/trends?archived=1", nil).Body.String()
	if !strings.Contains(withArchived, `data-chart="cafes"`) {
		t.Fatal("trends ?archived=1 does not render the archived measurement")
	}
	if !strings.Contains(withArchived, `archivée`) {
		t.Fatal("archived label is missing from the summary")
	}
	if !strings.Contains(withArchived, `aria-pressed="true"`) {
		t.Fatal("archived toggle is not shown as pressed")
	}

	seven := serve(t, mux, http.MethodGet, "/trends?range=7", nil).Body.String()
	if !strings.Contains(seven, `7 DERNIERS JOURS`) {
		t.Fatal("trends ?range=7 did not switch the range")
	}
	if strings.Count(seven, `"2026-11-`) < 7 {
		t.Fatal("trends ?range=7 has fewer than 7 dates")
	}

	bad := serve(t, mux, http.MethodGet, "/trends?range=12", nil).Body.String()
	if !strings.Contains(bad, `30 DERNIERS JOURS`) {
		t.Fatal("an unknown range should fall back to 30 days")
	}

	// No rating recorded in the 7 day window ending on 2026-11-07 yet, so the
	// chart hides itself to avoid a flat line of gaps.
	app.now = func() time.Time { return time.Date(2026, 11, 7, 12, 0, 0, 0, app.location) }
	empty := serve(t, mux, http.MethodGet, "/trends?range=7", nil).Body.String()
	if strings.Contains(empty, `data-chart="__rating__"`) {
		t.Fatal("rating chart was drawn with no recorded rating")
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
			if message.URL != "/day?date=2026-11-15" || message.Body != "Il reste à enregistrer: Sommeil et Cafés." {
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
