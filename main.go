package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/csv"
	"errors"
	"html/template"
	"io/fs"
	"log"
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
)

//go:embed templates/*.html static/*
var assets embed.FS

type App struct {
	store        *Store
	templates    *template.Template
	location     *time.Location
	passwordHash [32]byte
	sessionKey   []byte
	secureCookie bool
	now          func() time.Time
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
		store:        store,
		templates:    templates,
		location:     location,
		passwordHash: sha256.Sum256([]byte(password)),
		sessionKey:   []byte(sessionKey),
		secureCookie: strings.EqualFold(os.Getenv("PRAXIS_SECURE_COOKIE"), "true"),
		now:          time.Now,
	}

	mux := http.NewServeMux()
	staticFS, err := fs.Sub(assets, "static")
	if err != nil {
		log.Fatal(err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticFS)))
	mux.HandleFunc("GET /healthz", app.health)
	mux.HandleFunc("GET /login", app.loginPage)
	mux.HandleFunc("POST /login", app.login)
	mux.HandleFunc("POST /logout", app.auth(app.logout))
	mux.HandleFunc("GET /", app.auth(app.home))
	mux.HandleFunc("GET /day", app.auth(app.dayPage))
	mux.HandleFunc("POST /day", app.auth(app.saveDay))
	mux.HandleFunc("GET /calendar", app.auth(app.calendar))
	mux.HandleFunc("GET /habits", app.auth(app.habits))
	mux.HandleFunc("POST /habits", app.auth(app.saveHabit))
	mux.HandleFunc("POST /habits/reorder", app.auth(app.reorderHabits))
	mux.HandleFunc("GET /export.csv", app.auth(app.exportCSV))

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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("arrêt du serveur: %v", err)
	}
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
	doneIDs := make(map[int64]bool)
	for _, raw := range r.Form["habit"] {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			http.Error(w, "habitude invalide", http.StatusBadRequest)
			return
		}
		doneIDs[id] = true
	}
	if err := a.store.SaveDay(date, a.today(), rating, doneIDs); err != nil {
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
	_ = writer.Write([]string{"date", "note", "habitude", "faite"})
	for _, row := range rows {
		rating := ""
		if row.Rating != nil {
			rating = strconv.Itoa(*row.Rating)
		}
		done := ""
		if row.Done != nil {
			done = strconv.FormatBool(*row.Done)
		}
		_ = writer.Write([]string{row.Date, rating, row.Habit, done})
	}
	writer.Flush()
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
