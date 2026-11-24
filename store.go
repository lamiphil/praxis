package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type Habit struct {
	ID          int64
	Name        string
	Criterion   string
	Position    int
	ActiveFrom  string
	ActiveUntil string
	Done        bool
}

type Day struct {
	Date         string
	Rating       *int
	SleepMinutes *int
	CoffeeCount  *int
	Saved        bool
	Habits       []Habit
}

type DayStatus struct {
	Saved bool
	Rated bool
}

type ExportRow struct {
	Date         string
	Rating       *int
	Habit        string
	Done         *bool
	SleepMinutes *int
	CoffeeCount  *int
}

type TrendDay struct {
	Date         string
	SleepMinutes *int
	CoffeeCount  *int
}

type PushSubscription struct {
	ID       int64
	Endpoint string
	P256DH   string
	Auth     string
}

type seedHabit struct {
	Name       string `json:"name"`
	Criterion  string `json:"criterion"`
	ActiveFrom string `json:"active_from"`
}

func OpenStore(dbPath, seedPath, today string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("configure sqlite: %w", err)
		}
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.seed(seedPath, today); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *Store) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS habits (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    criterion TEXT NOT NULL DEFAULT '',
    position INTEGER NOT NULL,
    active_from TEXT NOT NULL,
    active_until TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (active_until IS NULL OR active_until >= active_from)
);
CREATE TABLE IF NOT EXISTS days (
    date TEXT PRIMARY KEY,
    rating INTEGER,
    sleep_minutes INTEGER,
    coffee_count INTEGER,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (rating IS NULL OR rating IN (1, 2, 3, 5, 8, 13)),
    CHECK (sleep_minutes IS NULL OR (sleep_minutes BETWEEN 0 AND 1440 AND sleep_minutes % 15 = 0)),
    CHECK (coffee_count IS NULL OR coffee_count BETWEEN 0 AND 20)
);
CREATE TABLE IF NOT EXISTS habit_values (
    day_date TEXT NOT NULL REFERENCES days(date) ON DELETE CASCADE,
    habit_id INTEGER NOT NULL REFERENCES habits(id),
    done INTEGER NOT NULL CHECK (done IN (0, 1)),
    PRIMARY KEY (day_date, habit_id)
);
CREATE TABLE IF NOT EXISTS push_subscriptions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    endpoint TEXT NOT NULL UNIQUE,
    p256dh TEXT NOT NULL,
    auth TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS push_deliveries (
    day_date TEXT NOT NULL,
    subscription_id INTEGER NOT NULL REFERENCES push_subscriptions(id) ON DELETE CASCADE,
    sent_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (day_date, subscription_id)
);`
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	for _, migration := range []struct {
		name string
		sql  string
	}{
		{"sleep_minutes", "ALTER TABLE days ADD COLUMN sleep_minutes INTEGER CHECK (sleep_minutes IS NULL OR (sleep_minutes BETWEEN 0 AND 1440 AND sleep_minutes % 15 = 0))"},
		{"coffee_count", "ALTER TABLE days ADD COLUMN coffee_count INTEGER CHECK (coffee_count IS NULL OR coffee_count BETWEEN 0 AND 20)"},
	} {
		var exists int
		if err := s.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('days') WHERE name = ?", migration.name).Scan(&exists); err != nil {
			return fmt.Errorf("inspect database schema: %w", err)
		}
		if exists == 0 {
			if _, err := s.db.Exec(migration.sql); err != nil {
				return fmt.Errorf("add %s: %w", migration.name, err)
			}
		}
	}
	return nil
}

func (s *Store) seed(path, today string) error {
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM habits").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read seed file: %w", err)
	}
	var habits []seedHabit
	if err := json.Unmarshal(data, &habits); err != nil {
		return fmt.Errorf("parse seed file: %w", err)
	}
	if len(habits) == 0 {
		return errors.New("seed file must contain at least one habit")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, habit := range habits {
		name := strings.TrimSpace(habit.Name)
		if name == "" {
			return fmt.Errorf("seed habit %d has no name", i+1)
		}
		activeFrom := habit.ActiveFrom
		if activeFrom == "" {
			activeFrom = today
		}
		if !validISODate(activeFrom) {
			return fmt.Errorf("seed habit %q has invalid active_from", name)
		}
		if _, err := tx.Exec(
			"INSERT INTO habits (name, criterion, position, active_from) VALUES (?, ?, ?, ?)",
			name, strings.TrimSpace(habit.Criterion), i+1, activeFrom,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Day(date string) (Day, error) {
	day := Day{Date: date}
	var rating, sleepMinutes, coffeeCount sql.NullInt64
	err := s.db.QueryRow("SELECT rating, sleep_minutes, coffee_count FROM days WHERE date = ?", date).Scan(&rating, &sleepMinutes, &coffeeCount)
	switch {
	case err == nil:
		day.Saved = true
		if rating.Valid {
			value := int(rating.Int64)
			day.Rating = &value
		}
		if sleepMinutes.Valid {
			value := int(sleepMinutes.Int64)
			day.SleepMinutes = &value
		}
		if coffeeCount.Valid {
			value := int(coffeeCount.Int64)
			day.CoffeeCount = &value
		}
	case errors.Is(err, sql.ErrNoRows):
	default:
		return day, err
	}

	rows, err := s.db.Query(`
SELECT h.id, h.name, h.criterion, h.position, h.active_from,
       COALESCE(h.active_until, ''), COALESCE(v.done, 0)
FROM habits h
LEFT JOIN habit_values v ON v.habit_id = h.id AND v.day_date = ?
WHERE h.active_from <= ? AND (h.active_until IS NULL OR h.active_until >= ?)
ORDER BY h.position, h.id`, date, date, date)
	if err != nil {
		return day, err
	}
	defer rows.Close()
	for rows.Next() {
		var habit Habit
		if err := rows.Scan(&habit.ID, &habit.Name, &habit.Criterion, &habit.Position, &habit.ActiveFrom, &habit.ActiveUntil, &habit.Done); err != nil {
			return day, err
		}
		day.Habits = append(day.Habits, habit)
	}
	return day, rows.Err()
}

func (s *Store) SaveDay(date, today string, rating, sleepMinutes, coffeeCount *int, doneIDs map[int64]bool) error {
	if !validISODate(date) || date > today {
		return errors.New("invalid day date")
	}
	if rating != nil && !validRating(*rating) {
		return errors.New("invalid rating")
	}
	if sleepMinutes != nil && !validSleepMinutes(*sleepMinutes) {
		return errors.New("invalid sleep duration")
	}
	if coffeeCount != nil && !validCoffeeCount(*coffeeCount) {
		return errors.New("invalid coffee count")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`
SELECT id FROM habits
WHERE active_from <= ? AND (active_until IS NULL OR active_until >= ?)
ORDER BY position, id`, date, date)
	if err != nil {
		return err
	}
	var activeIDs []int64
	active := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		activeIDs = append(activeIDs, id)
		active[id] = true
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for id := range doneIDs {
		if !active[id] {
			return fmt.Errorf("habit %d is not active on %s", id, date)
		}
	}

	var ratingValue, sleepValue, coffeeValue any
	if rating != nil {
		ratingValue = *rating
	}
	if sleepMinutes != nil {
		sleepValue = *sleepMinutes
	}
	if coffeeCount != nil {
		coffeeValue = *coffeeCount
	}
	if _, err := tx.Exec(`
INSERT INTO days (date, rating, sleep_minutes, coffee_count, updated_at)
VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(date) DO UPDATE SET
  rating = excluded.rating,
  sleep_minutes = excluded.sleep_minutes,
  coffee_count = excluded.coffee_count,
  updated_at = CURRENT_TIMESTAMP`, date, ratingValue, sleepValue, coffeeValue); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM habit_values WHERE day_date = ?", date); err != nil {
		return err
	}
	for _, id := range activeIDs {
		if _, err := tx.Exec(
			"INSERT INTO habit_values (day_date, habit_id, done) VALUES (?, ?, ?)",
			date, id, doneIDs[id],
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Habits() ([]Habit, error) {
	rows, err := s.db.Query(`
SELECT id, name, criterion, position, active_from, COALESCE(active_until, '')
FROM habits ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var habits []Habit
	for rows.Next() {
		var habit Habit
		if err := rows.Scan(&habit.ID, &habit.Name, &habit.Criterion, &habit.Position, &habit.ActiveFrom, &habit.ActiveUntil); err != nil {
			return nil, err
		}
		habits = append(habits, habit)
	}
	return habits, rows.Err()
}

func (s *Store) SaveHabit(habit Habit) error {
	habit.Name = strings.TrimSpace(habit.Name)
	habit.Criterion = strings.TrimSpace(habit.Criterion)
	if habit.Name == "" {
		return errors.New("habit name is required")
	}
	if !validISODate(habit.ActiveFrom) {
		return errors.New("invalid active_from date")
	}
	if habit.ActiveUntil != "" {
		if !validISODate(habit.ActiveUntil) || habit.ActiveUntil < habit.ActiveFrom {
			return errors.New("invalid active_until date")
		}
	}
	if habit.Position < 1 {
		habit.Position = 1
	}

	var activeUntil any
	if habit.ActiveUntil != "" {
		activeUntil = habit.ActiveUntil
	}
	if habit.ID == 0 {
		_, err := s.db.Exec(`
INSERT INTO habits (name, criterion, position, active_from, active_until)
VALUES (?, ?, ?, ?, ?)`, habit.Name, habit.Criterion, habit.Position, habit.ActiveFrom, activeUntil)
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var conflictingValues int
	if err := tx.QueryRow(`
SELECT COUNT(*) FROM habit_values
WHERE habit_id = ? AND (day_date < ? OR (? IS NOT NULL AND day_date > ?))`,
		habit.ID, habit.ActiveFrom, activeUntil, activeUntil,
	).Scan(&conflictingValues); err != nil {
		return err
	}
	if conflictingValues > 0 {
		return errors.New("la période exclut des données déjà enregistrées")
	}
	result, err := tx.Exec(`
UPDATE habits SET name = ?, criterion = ?, position = ?, active_from = ?, active_until = ?
WHERE id = ?`, habit.Name, habit.Criterion, habit.Position, habit.ActiveFrom, activeUntil, habit.ID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func (s *Store) ReorderHabits(ids []int64) error {
	if len(ids) == 0 {
		return errors.New("habit order is empty")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRow("SELECT COUNT(*) FROM habits").Scan(&count); err != nil {
		return err
	}
	if len(ids) != count {
		return errors.New("habit order is incomplete")
	}
	seen := make(map[int64]bool, len(ids))
	for position, id := range ids {
		if id < 1 || seen[id] {
			return errors.New("habit order is invalid")
		}
		seen[id] = true
		result, err := tx.Exec("UPDATE habits SET position = ? WHERE id = ?", position+1, id)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed != 1 {
			return errors.New("habit order contains an unknown habit")
		}
	}
	return tx.Commit()
}

func (s *Store) DayStatuses(from, until string) (map[string]DayStatus, error) {
	rows, err := s.db.Query(
		"SELECT date, rating IS NOT NULL FROM days WHERE date >= ? AND date <= ? ORDER BY date",
		from, until,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	statuses := make(map[string]DayStatus)
	for rows.Next() {
		var date string
		var rated bool
		if err := rows.Scan(&date, &rated); err != nil {
			return nil, err
		}
		statuses[date] = DayStatus{Saved: true, Rated: rated}
	}
	return statuses, rows.Err()
}

func (s *Store) ExportRows() ([]ExportRow, error) {
	rows, err := s.db.Query(`
SELECT d.date, d.rating, COALESCE(h.name, ''), v.done, d.sleep_minutes, d.coffee_count
FROM days d
LEFT JOIN habit_values v ON v.day_date = d.date
LEFT JOIN habits h ON h.id = v.habit_id
ORDER BY d.date, h.position, h.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ExportRow
	for rows.Next() {
		var row ExportRow
		var rating, sleepMinutes, coffeeCount sql.NullInt64
		var done sql.NullBool
		if err := rows.Scan(&row.Date, &rating, &row.Habit, &done, &sleepMinutes, &coffeeCount); err != nil {
			return nil, err
		}
		if rating.Valid {
			value := int(rating.Int64)
			row.Rating = &value
		}
		if done.Valid {
			value := done.Bool
			row.Done = &value
		}
		if sleepMinutes.Valid {
			value := int(sleepMinutes.Int64)
			row.SleepMinutes = &value
		}
		if coffeeCount.Valid {
			value := int(coffeeCount.Int64)
			row.CoffeeCount = &value
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func (s *Store) TrendDays(from, until string) ([]TrendDay, error) {
	rows, err := s.db.Query(`
SELECT date, sleep_minutes, coffee_count
FROM days
WHERE date BETWEEN ? AND ? AND (sleep_minutes IS NOT NULL OR coffee_count IS NOT NULL)
ORDER BY date`, from, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []TrendDay
	for rows.Next() {
		var day TrendDay
		var sleepMinutes, coffeeCount sql.NullInt64
		if err := rows.Scan(&day.Date, &sleepMinutes, &coffeeCount); err != nil {
			return nil, err
		}
		if sleepMinutes.Valid {
			value := int(sleepMinutes.Int64)
			day.SleepMinutes = &value
		}
		if coffeeCount.Valid {
			value := int(coffeeCount.Int64)
			day.CoffeeCount = &value
		}
		result = append(result, day)
	}
	return result, rows.Err()
}

func (s *Store) UpsertPushSubscription(subscription PushSubscription) error {
	if subscription.Endpoint == "" || subscription.P256DH == "" || subscription.Auth == "" {
		return errors.New("invalid push subscription")
	}
	_, err := s.db.Exec(`
INSERT INTO push_subscriptions (endpoint, p256dh, auth) VALUES (?, ?, ?)
ON CONFLICT(endpoint) DO UPDATE SET p256dh = excluded.p256dh, auth = excluded.auth, updated_at = CURRENT_TIMESTAMP`,
		subscription.Endpoint, subscription.P256DH, subscription.Auth)
	return err
}

func (s *Store) DeletePushSubscription(endpoint string) error {
	_, err := s.db.Exec("DELETE FROM push_subscriptions WHERE endpoint = ?", endpoint)
	return err
}

func (s *Store) PushSubscriptions() ([]PushSubscription, error) {
	rows, err := s.db.Query("SELECT id, endpoint, p256dh, auth FROM push_subscriptions ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []PushSubscription
	for rows.Next() {
		var subscription PushSubscription
		if err := rows.Scan(&subscription.ID, &subscription.Endpoint, &subscription.P256DH, &subscription.Auth); err != nil {
			return nil, err
		}
		result = append(result, subscription)
	}
	return result, rows.Err()
}

func (s *Store) PushDelivered(date string, subscriptionID int64) (bool, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM push_deliveries WHERE day_date = ? AND subscription_id = ?", date, subscriptionID).Scan(&count)
	return count > 0, err
}

func (s *Store) MarkPushDelivered(date string, subscriptionID int64) error {
	_, err := s.db.Exec("INSERT OR IGNORE INTO push_deliveries (day_date, subscription_id) VALUES (?, ?)", date, subscriptionID)
	return err
}

func validISODate(value string) bool {
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
}

func validRating(value int) bool {
	for _, allowed := range []int{1, 2, 3, 5, 8, 13} {
		if value == allowed {
			return true
		}
	}
	return false
}

func validSleepMinutes(value int) bool {
	return value >= 0 && value <= 24*60 && value%15 == 0
}

func validCoffeeCount(value int) bool {
	return value >= 0 && value <= 20
}
