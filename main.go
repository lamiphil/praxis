package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"html/template"
	"io/fs"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

//go:embed templates/*.html static/*
var assets embed.FS

type App struct {
	store           *Store
	templates       *template.Template
	location        *time.Location
	passwordHash    [32]byte
	sessionKey      []byte
	secureCookie    bool
	trustedHeader   string
	vapidPublicKey  string
	vapidPrivateKey string
	vapidSubscriber string
	now             func() time.Time
	sendPush        func(context.Context, PushSubscription, []byte) (int, error)
}

type ratingOption struct {
	Value    int
	Selected bool
}

type dayPage struct {
	Day          Day
	Today        string
	Previous     string
	Next         string
	CanGoNext    bool
	NoneSelected bool
	Ratings      []ratingOption
	SleepHours   string
	SleepState   string
	CoffeeValue  string
	CoffeeState  string
}

type calendarCell struct {
	Day    int
	Date   string
	Status string
	Future bool
	Today  bool
}

type calendarPage struct {
	MonthLabel string
	Previous   string
	Next       string
	Cells      []calendarCell
}

type habitsPage struct {
	Habits       []Habit
	Today        string
	NextPosition int
}

type trendPoint struct {
	Date       string   `json:"date"`
	SleepHours *float64 `json:"sleepHours"`
	Coffee     *int     `json:"coffee"`
}

type trendsPage struct {
	Data          template.JS
	SleepAverage  string
	SleepDays     int
	CoffeeAverage string
	CoffeeDays    int
}

type settingsPage struct {
	PushAvailable  bool
	VAPIDPublicKey string
}

type pushRequest struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256DH string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

type pushPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
}

func main() {
	location, err := time.LoadLocation("America/Toronto")
	if err != nil {
		log.Fatal(err)
	}

	addr := envOr("PRAXIS_ADDR", "127.0.0.1:8080")
	password, passwordConfigured := os.LookupEnv("PRAXIS_PASSWORD")
	if !passwordConfigured {
		password = "praxis"
	}
	sessionKey, sessionConfigured := os.LookupEnv("PRAXIS_SESSION_KEY")
	if !sessionConfigured {
		sessionKey = "praxis-local-session-key"
	}
	if !isLoopbackAddress(addr) && (!passwordConfigured || !sessionConfigured) {
		log.Fatal("PRAXIS_PASSWORD and PRAXIS_SESSION_KEY are required outside localhost")
	}
	if !passwordConfigured {
		log.Print("local test password: praxis")
	}

	dbPath := envOr("PRAXIS_DB", filepath.Join("data", "praxis.db"))
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		log.Fatal(err)
	}
	today := time.Now().In(location).Format("2006-01-02")
	store, err := OpenStore(dbPath, envOr("PRAXIS_SEED_FILE", "seed.json"), today)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	templates, err := template.ParseFS(assets, "templates/*.html")
	if err != nil {
		log.Fatal(err)
	}
	app := &App{
		store:           store,
		templates:       templates,
		location:        location,
		passwordHash:    sha256.Sum256([]byte(password)),
		sessionKey:      []byte(sessionKey),
		secureCookie:    strings.EqualFold(os.Getenv("PRAXIS_SECURE_COOKIE"), "true"),
		trustedHeader:   http.CanonicalHeaderKey(os.Getenv("PRAXIS_TRUSTED_HEADER")),
		vapidPublicKey:  os.Getenv("PRAXIS_VAPID_PUBLIC_KEY"),
		vapidPrivateKey: os.Getenv("PRAXIS_VAPID_PRIVATE_KEY"),
		vapidSubscriber: envOr("PRAXIS_VAPID_SUBSCRIBER", "mailto:admin@lamiphil.dev"),
		now:             time.Now,
	}
	app.sendPush = app.sendWebPush
	if app.trustedHeader != "" {
		log.Printf("authentification via l'en-tête %q (bypass du mot de passe)", app.trustedHeader)
	}

	mux, err := app.routes()
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if app.pushConfigured() {
		go app.runReminders(ctx)
	} else {
		log.Print("notifications Web Push désactivées: clés VAPID absentes")
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           app.securityHeaders(app.sameOrigin(mux)),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		log.Printf("Praxis écoute sur http://%s", addr)
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("arrêt du serveur: %v", err)
	}
}

func (a *App) routes() (*http.ServeMux, error) {
	staticFS, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticFS)))
	mux.HandleFunc("GET /sw.js", a.serviceWorker)
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("GET /login", a.loginPage)
	mux.HandleFunc("POST /login", a.login)
	mux.HandleFunc("POST /logout", a.auth(a.logout))
	mux.HandleFunc("GET /", a.auth(a.home))
	mux.HandleFunc("GET /day", a.auth(a.dayPage))
	mux.HandleFunc("POST /day", a.auth(a.saveDay))
	mux.HandleFunc("GET /calendar", a.auth(a.calendar))
	mux.HandleFunc("GET /trends", a.auth(a.trends))
	mux.HandleFunc("GET /settings", a.auth(a.settings))
	mux.HandleFunc("POST /push/subscribe", a.auth(a.subscribePush))
	mux.HandleFunc("POST /push/unsubscribe", a.auth(a.unsubscribePush))
	mux.HandleFunc("GET /habits", a.auth(a.habits))
	mux.HandleFunc("POST /habits", a.auth(a.saveHabit))
	mux.HandleFunc("POST /habits/reorder", a.auth(a.reorderHabits))
	mux.HandleFunc("GET /export.csv", a.auth(a.exportCSV))
	return mux, nil
}

func (a *App) serviceWorker(w http.ResponseWriter, _ *http.Request) {
	content, err := assets.ReadFile("static/sw.js")
	if err != nil {
		a.serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Service-Worker-Allowed", "/")
	_, _ = w.Write(content)
}

func (a *App) health(w http.ResponseWriter, r *http.Request) {
	if err := a.store.Ping(r.Context()); err != nil {
		http.Error(w, "indisponible", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func (a *App) home(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/day", http.StatusSeeOther)
}

func (a *App) loginPage(w http.ResponseWriter, r *http.Request) {
	if a.authenticated(r) {
		http.Redirect(w, r, "/day", http.StatusSeeOther)
		return
	}
	a.render(w, "login.html", map[string]bool{"Error": r.URL.Query().Get("error") != ""})
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "requête invalide", http.StatusBadRequest)
		return
	}
	provided := sha256.Sum256([]byte(r.FormValue("password")))
	if subtle.ConstantTimeCompare(provided[:], a.passwordHash[:]) != 1 {
		http.Redirect(w, r, "/login?error=1", http.StatusSeeOther)
		return
	}
	expires := a.now().Add(30 * 24 * time.Hour)
	http.SetCookie(w, &http.Cookie{
		Name:     "praxis_session",
		Value:    a.signSession(expires),
		Path:     "/",
		Expires:  expires,
		MaxAge:   30 * 24 * 60 * 60,
		HttpOnly: true,
		Secure:   a.secureCookie,
		SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, "/day", http.StatusSeeOther)
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "praxis_session",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.secureCookie,
		SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *App) dayPage(w http.ResponseWriter, r *http.Request) {
	today := a.today()
	date := r.URL.Query().Get("date")
	if date == "" {
		date = today
	}
	parsed, err := time.ParseInLocation("2006-01-02", date, a.location)
	if err != nil || date > today {
		http.Error(w, "date invalide", http.StatusBadRequest)
		return
	}
	day, err := a.store.Day(date)
	if err != nil {
		a.serverError(w, err)
		return
	}
	ratings := make([]ratingOption, 0, 6)
	for _, value := range []int{1, 2, 3, 5, 8, 13} {
		ratings = append(ratings, ratingOption{Value: value, Selected: day.Rating != nil && *day.Rating == value})
	}
	next := parsed.AddDate(0, 0, 1).Format("2006-01-02")
	a.render(w, "day.html", dayPage{
		Day:          day,
		Today:        today,
		Previous:     parsed.AddDate(0, 0, -1).Format("2006-01-02"),
		Next:         next,
		CanGoNext:    next <= today,
		NoneSelected: day.Rating == nil,
		Ratings:      ratings,
		SleepHours:   formatSleepMinutes(day.SleepMinutes),
		SleepState:   sleepState(day.SleepMinutes),
		CoffeeValue:  formatOptionalInt(day.CoffeeCount),
		CoffeeState:  coffeeState(day.CoffeeCount),
	})
}

func (a *App) saveDay(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "requête invalide", http.StatusBadRequest)
		return
	}
	date := r.FormValue("date")
	var rating *int
	if raw := r.FormValue("rating"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			http.Error(w, "note invalide", http.StatusBadRequest)
			return
		}
		rating = &value
	}
	sleepMinutes, err := parseSleepHours(r.FormValue("sleep_hours"))
	if err != nil {
		http.Error(w, "durée de sommeil invalide", http.StatusBadRequest)
		return
	}
	coffeeCount, err := parseOptionalInt(r.FormValue("coffee_count"))
	if err != nil || (coffeeCount != nil && !validCoffeeCount(*coffeeCount)) {
		http.Error(w, "nombre de cafés invalide", http.StatusBadRequest)
		return
	}
	doneIDs := make(map[int64]bool)
	for _, raw := range r.Form["habit"] {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			http.Error(w, "habitude invalide", http.StatusBadRequest)
			return
		}
		doneIDs[id] = true
	}
	if err := a.store.SaveDay(date, a.today(), rating, sleepMinutes, coffeeCount, doneIDs); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/day?date="+url.QueryEscape(date), http.StatusSeeOther)
}

func (a *App) calendar(w http.ResponseWriter, r *http.Request) {
	today := a.today()
	month := r.URL.Query().Get("month")
	if month == "" {
		month = today[:7]
	}
	first, err := time.ParseInLocation("2006-01-02", month+"-01", a.location)
	if err != nil {
		http.Error(w, "mois invalide", http.StatusBadRequest)
		return
	}
	last := first.AddDate(0, 1, -1)
	statuses, err := a.store.DayStatuses(first.Format("2006-01-02"), last.Format("2006-01-02"))
	if err != nil {
		a.serverError(w, err)
		return
	}

	offset := (int(first.Weekday()) + 6) % 7
	cells := make([]calendarCell, offset, offset+last.Day()+7)
	for day := 1; day <= last.Day(); day++ {
		date := time.Date(first.Year(), first.Month(), day, 0, 0, 0, 0, a.location).Format("2006-01-02")
		status := "missing"
		if saved := statuses[date]; saved.Saved {
			status = "unrated"
			if saved.Rated {
				status = "rated"
			}
		}
		cells = append(cells, calendarCell{
			Day: day, Date: date, Status: status, Future: date > today, Today: date == today,
		})
	}
	for len(cells)%7 != 0 {
		cells = append(cells, calendarCell{})
	}
	months := [...]string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"}
	a.render(w, "calendar.html", calendarPage{
		MonthLabel: months[first.Month()-1] + " " + strconv.Itoa(first.Year()),
		Previous:   first.AddDate(0, -1, 0).Format("2006-01"),
		Next:       first.AddDate(0, 1, 0).Format("2006-01"),
		Cells:      cells,
	})
}

func (a *App) trends(w http.ResponseWriter, r *http.Request) {
	today, err := time.ParseInLocation("2006-01-02", a.today(), a.location)
	if err != nil {
		a.serverError(w, err)
		return
	}
	from := today.AddDate(0, 0, -29)
	rows, err := a.store.TrendDays(from.Format("2006-01-02"), today.Format("2006-01-02"))
	if err != nil {
		a.serverError(w, err)
		return
	}
	byDate := make(map[string]TrendDay, len(rows))
	for _, row := range rows {
		byDate[row.Date] = row
	}
	points := make([]trendPoint, 0, 30)
	var sleepTotal float64
	var coffeeTotal int
	var sleepDays, coffeeDays int
	for i := 0; i < 30; i++ {
		date := from.AddDate(0, 0, i).Format("2006-01-02")
		point := trendPoint{Date: date}
		if row, ok := byDate[date]; ok {
			if row.SleepMinutes != nil {
				hours := float64(*row.SleepMinutes) / 60
				point.SleepHours = &hours
				sleepTotal += hours
				sleepDays++
			}
			if row.CoffeeCount != nil {
				value := *row.CoffeeCount
				point.Coffee = &value
				coffeeTotal += value
				coffeeDays++
			}
		}
		points = append(points, point)
	}
	data, err := json.Marshal(points)
	if err != nil {
		a.serverError(w, err)
		return
	}
	page := trendsPage{
		Data:       template.JS(data), // JSON contains only validated dates and numbers.
		SleepDays:  sleepDays,
		CoffeeDays: coffeeDays,
	}
	if sleepDays > 0 {
		page.SleepAverage = strconv.FormatFloat(sleepTotal/float64(sleepDays), 'f', 2, 64) + " h"
	} else {
		page.SleepAverage = "—"
	}
	if coffeeDays > 0 {
		page.CoffeeAverage = strconv.FormatFloat(float64(coffeeTotal)/float64(coffeeDays), 'f', 2, 64)
	} else {
		page.CoffeeAverage = "—"
	}
	a.render(w, "trends.html", page)
}

func (a *App) settings(w http.ResponseWriter, _ *http.Request) {
	a.render(w, "settings.html", settingsPage{
		PushAvailable:  a.pushConfigured(),
		VAPIDPublicKey: a.vapidPublicKey,
	})
}

func (a *App) habits(w http.ResponseWriter, r *http.Request) {
	habits, err := a.store.Habits()
	if err != nil {
		a.serverError(w, err)
		return
	}
	a.render(w, "habits.html", habitsPage{Habits: habits, Today: a.today(), NextPosition: len(habits) + 1})
}

func (a *App) saveHabit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "requête invalide", http.StatusBadRequest)
		return
	}
	id, err := optionalInt64(r.FormValue("id"))
	if err != nil {
		http.Error(w, "identifiant invalide", http.StatusBadRequest)
		return
	}
	position, err := strconv.Atoi(r.FormValue("position"))
	if err != nil {
		http.Error(w, "ordre invalide", http.StatusBadRequest)
		return
	}
	habit := Habit{
		ID:          id,
		Name:        r.FormValue("name"),
		Criterion:   r.FormValue("criterion"),
		Position:    position,
		ActiveFrom:  r.FormValue("active_from"),
		ActiveUntil: r.FormValue("active_until"),
	}
	if err := a.store.SaveHabit(habit); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/habits", http.StatusSeeOther)
}

func (a *App) reorderHabits(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "requête invalide", http.StatusBadRequest)
		return
	}
	parts := strings.Split(r.FormValue("order"), ",")
	ids := make([]int64, 0, len(parts))
	for _, part := range parts {
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			http.Error(w, "ordre invalide", http.StatusBadRequest)
			return
		}
		ids = append(ids, id)
	}
	if err := a.store.ReorderHabits(ids); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/habits", http.StatusSeeOther)
}

func (a *App) exportCSV(w http.ResponseWriter, r *http.Request) {
	rows, err := a.store.ExportRows()
	if err != nil {
		a.serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=praxis.csv")
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"date", "note", "habitude", "faite", "sommeil_heures", "cafes"})
	for _, row := range rows {
		rating := ""
		if row.Rating != nil {
			rating = strconv.Itoa(*row.Rating)
		}
		done := ""
		if row.Done != nil {
			done = strconv.FormatBool(*row.Done)
		}
		_ = writer.Write([]string{
			row.Date,
			rating,
			row.Habit,
			done,
			formatSleepMinutes(row.SleepMinutes),
			formatOptionalInt(row.CoffeeCount),
		})
	}
	writer.Flush()
}

func (a *App) subscribePush(w http.ResponseWriter, r *http.Request) {
	if !a.pushConfigured() {
		http.Error(w, "notifications indisponibles", http.StatusServiceUnavailable)
		return
	}
	request, err := decodePushRequest(w, r)
	if err != nil || request.Keys.P256DH == "" || request.Keys.Auth == "" {
		http.Error(w, "abonnement invalide", http.StatusBadRequest)
		return
	}
	if err := a.store.UpsertPushSubscription(PushSubscription{
		Endpoint: request.Endpoint,
		P256DH:   request.Keys.P256DH,
		Auth:     request.Keys.Auth,
	}); err != nil {
		a.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) unsubscribePush(w http.ResponseWriter, r *http.Request) {
	request, err := decodePushRequest(w, r)
	if err != nil {
		http.Error(w, "abonnement invalide", http.StatusBadRequest)
		return
	}
	if err := a.store.DeletePushSubscription(request.Endpoint); err != nil {
		a.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodePushRequest(w http.ResponseWriter, r *http.Request) (pushRequest, error) {
	var request pushRequest
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		return request, err
	}
	endpoint, err := url.ParseRequestURI(request.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || len(request.Endpoint) > 4096 || len(request.Keys.P256DH) > 512 || len(request.Keys.Auth) > 512 {
		return request, errors.New("invalid push endpoint")
	}
	return request, nil
}

func (a *App) pushConfigured() bool {
	return a.vapidPublicKey != "" && a.vapidPrivateKey != ""
}

func (a *App) runReminders(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := a.deliverDueReminders(ctx, a.now()); err != nil {
			log.Printf("rappel Web Push: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *App) deliverDueReminders(ctx context.Context, now time.Time) error {
	local := now.In(a.location)
	if local.Hour() < 20 {
		return nil
	}
	date := local.Format("2006-01-02")
	day, err := a.store.Day(date)
	if err != nil {
		return err
	}
	var missing []string
	if day.SleepMinutes == nil {
		missing = append(missing, "le sommeil")
	}
	if day.CoffeeCount == nil {
		missing = append(missing, "les cafés")
	}
	if len(missing) == 0 {
		return nil
	}
	body := "Il reste à enregistrer " + strings.Join(missing, " et ") + "."
	payload, err := json.Marshal(pushPayload{Title: "Praxis", Body: body, URL: "/day?date=" + date})
	if err != nil {
		return err
	}
	subscriptions, err := a.store.PushSubscriptions()
	if err != nil {
		return err
	}
	for _, subscription := range subscriptions {
		delivered, err := a.store.PushDelivered(date, subscription.ID)
		if err != nil {
			return err
		}
		if delivered {
			continue
		}
		status, err := a.sendPush(ctx, subscription, payload)
		if err != nil {
			log.Printf("notification %d: %v", subscription.ID, err)
			continue
		}
		if status == http.StatusNotFound || status == http.StatusGone {
			if err := a.store.DeletePushSubscription(subscription.Endpoint); err != nil {
				return err
			}
			continue
		}
		if status < 200 || status >= 300 {
			log.Printf("notification %d: réponse HTTP %d", subscription.ID, status)
			continue
		}
		if err := a.store.MarkPushDelivered(date, subscription.ID); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) sendWebPush(ctx context.Context, subscription PushSubscription, payload []byte) (int, error) {
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	response, err := webpush.SendNotificationWithContext(requestCtx, payload, &webpush.Subscription{
		Endpoint: subscription.Endpoint,
		Keys: webpush.Keys{
			P256dh: subscription.P256DH,
			Auth:   subscription.Auth,
		},
	}, &webpush.Options{
		Subscriber:      a.vapidSubscriber,
		VAPIDPublicKey:  a.vapidPublicKey,
		VAPIDPrivateKey: a.vapidPrivateKey,
		TTL:             4 * 60 * 60,
		Topic:           "praxis-" + a.today(),
		Urgency:         webpush.UrgencyNormal,
	})
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	return response.StatusCode, nil
}

func (a *App) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.authenticated(r) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

func (a *App) authenticated(r *http.Request) bool {
	// Trusted-header auth (Authentik ForwardAuth). Only safe when the service
	// is reachable solely through Traefik, because Traefik's authResponseHeaders
	// overwrites any client-supplied value. A NetworkPolicy blocks in-cluster
	// bypass.
	if a.trustedHeader != "" {
		if value := r.Header.Get(a.trustedHeader); value != "" {
			return true
		}
	}
	cookie, err := r.Cookie("praxis_session")
	if err != nil {
		return false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return false
	}
	expires, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || a.now().Unix() > expires {
		return false
	}
	provided, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, a.sessionKey)
	_, _ = mac.Write([]byte(parts[0]))
	return hmac.Equal(provided, mac.Sum(nil))
}

func parseSleepHours(raw string) (*int, error) {
	if raw == "" {
		return nil, nil
	}
	hours, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil, err
	}
	minutesFloat := hours * 60
	minutes := int(math.Round(minutesFloat))
	if math.Abs(minutesFloat-float64(minutes)) > 1e-9 || !validSleepMinutes(minutes) {
		return nil, errors.New("invalid sleep duration")
	}
	return &minutes, nil
}

func parseOptionalInt(raw string) (*int, error) {
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func formatSleepMinutes(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.FormatFloat(float64(*value)/60, 'f', -1, 64)
}

func formatOptionalInt(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}

func sleepState(value *int) string {
	if value == nil {
		return "missing"
	}
	if *value >= 7*60 && *value <= 9*60 {
		return "goal"
	}
	return "outside"
}

func coffeeState(value *int) string {
	if value == nil {
		return "missing"
	}
	if *value <= 3 {
		return "goal"
	}
	return "outside"
}

func (a *App) signSession(expires time.Time) string {
	payload := strconv.FormatInt(expires.Unix(), 10)
	mac := hmac.New(sha256.New, a.sessionKey)
	_, _ = mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *App) today() string { return a.now().In(a.location).Format("2006-01-02") }

func (a *App) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.templates.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}

func (a *App) serverError(w http.ResponseWriter, err error) {
	log.Printf("server error: %v", err)
	http.Error(w, "erreur interne", http.StatusInternalServerError)
}

func (a *App) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		// no-referrer would make browsers send "Origin: null" on form posts.
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func (a *App) sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if origin := r.Header.Get("Origin"); origin != "" && !sameOriginHost(origin, r.Host) {
				log.Printf("origine refusée: origin=%q host=%q", origin, r.Host)
				http.Error(w, "origine interdite", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func sameOriginHost(origin, requestHost string) bool {
	parsedRequest, err := url.Parse("//" + requestHost)
	if err != nil {
		return false
	}
	if origin == "null" {
		return isLoopbackHost(parsedRequest.Hostname())
	}
	parsedOrigin, err := url.Parse(origin)
	if err != nil || parsedOrigin.Host == "" {
		return false
	}
	if strings.EqualFold(parsedOrigin.Host, requestHost) {
		return true
	}
	return parsedOrigin.Port() == parsedRequest.Port() &&
		isLoopbackHost(parsedOrigin.Hostname()) && isLoopbackHost(parsedRequest.Hostname())
}

func isLoopbackAddress(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	return err == nil && isLoopbackHost(host)
}

func isLoopbackHost(host string) bool {
	return strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func optionalInt64(value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	return strconv.ParseInt(value, 10, 64)
}
