package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	seed := `[
  {"name":"Lire","criterion":"Dix pages"},
  {"name":"Bouger","criterion":"Trente minutes"}
]`
	seedPath := filepath.Join(dir, "seed.json")
	if err := os.WriteFile(seedPath, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(filepath.Join(dir, "praxis.db"), seedPath, "2026-11-15")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestSeedAndSaveDay(t *testing.T) {
	store := testStore(t)
	day, err := store.Day("2026-11-15")
	if err != nil {
		t.Fatal(err)
	}
	if day.Saved || len(day.Habits) != 2 {
		t.Fatalf("unexpected initial day: saved=%v habits=%d", day.Saved, len(day.Habits))
	}

	done := map[int64]bool{day.Habits[0].ID: true}
	if err := store.SaveDay("2026-11-15", "2026-11-15", nil, nil, nil, done); err != nil {
		t.Fatal(err)
	}

	saved, err := store.Day("2026-11-15")
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Saved || saved.Rating != nil {
		t.Fatalf("day should be saved without rating: %+v", saved)
	}
	if !saved.Habits[0].Done || saved.Habits[1].Done {
		t.Fatalf("checkbox values not preserved: %+v", saved.Habits)
	}
}

func TestSaveDayRejectsInvalidInput(t *testing.T) {
	store := testStore(t)
	badRating := 4
	if err := store.SaveDay("2026-11-15", "2026-11-15", &badRating, nil, nil, nil); err == nil {
		t.Fatal("expected invalid rating to fail")
	}
	if err := store.SaveDay("2026-11-16", "2026-11-15", nil, nil, nil, nil); err == nil {
		t.Fatal("expected future day to fail")
	}
	if err := store.SaveDay("not-a-date", "2026-11-15", nil, nil, nil, nil); err == nil {
		t.Fatal("expected malformed day to fail")
	}
}

func TestHabitDatesAreValidated(t *testing.T) {
	store := testStore(t)
	err := store.SaveHabit(Habit{
		Name: "Test", Position: 3, ActiveFrom: "2026-11-15", ActiveUntil: "2026-11-14",
	})
	if err == nil {
		t.Fatal("expected active_until before active_from to fail")
	}
}

func TestHabitRangeCannotHideRecordedValues(t *testing.T) {
	store := testStore(t)
	day, err := store.Day("2026-11-15")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDay("2026-11-15", "2026-11-15", nil, nil, nil, map[int64]bool{day.Habits[0].ID: true}); err != nil {
		t.Fatal(err)
	}
	habit := day.Habits[0]
	habit.ActiveFrom = "2026-11-16"
	if err := store.SaveHabit(habit); err == nil {
		t.Fatal("expected range excluding a recorded value to fail")
	}
	reloaded, err := store.Day("2026-11-15")
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.Habits[0].Done {
		t.Fatal("recorded value was not preserved")
	}
}

func TestReorderHabits(t *testing.T) {
	store := testStore(t)
	habits, err := store.Habits()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReorderHabits([]int64{habits[1].ID, habits[0].ID}); err != nil {
		t.Fatal(err)
	}
	reordered, err := store.Habits()
	if err != nil {
		t.Fatal(err)
	}
	if reordered[0].ID != habits[1].ID || reordered[0].Position != 1 || reordered[1].Position != 2 {
		t.Fatalf("unexpected habit order: %+v", reordered)
	}
	if err := store.ReorderHabits([]int64{habits[0].ID, habits[0].ID}); err == nil {
		t.Fatal("expected duplicate order to fail")
	}
}

func TestMetricsSaveValidateTrendAndExport(t *testing.T) {
	store := testStore(t)
	sleep := 7*60 + 15
	coffee := 0
	if err := store.SaveDay("2026-11-15", "2026-11-15", nil, &sleep, &coffee, nil); err != nil {
		t.Fatal(err)
	}
	day, err := store.Day("2026-11-15")
	if err != nil {
		t.Fatal(err)
	}
	if day.SleepMinutes == nil || *day.SleepMinutes != sleep || day.CoffeeCount == nil || *day.CoffeeCount != 0 {
		t.Fatalf("metrics not preserved: %+v", day)
	}
	trends, err := store.TrendDays("2026-11-01", "2026-11-30")
	if err != nil || len(trends) != 1 || trends[0].SleepMinutes == nil || *trends[0].SleepMinutes != sleep {
		t.Fatalf("metrics missing from trends: rows=%+v err=%v", trends, err)
	}
	rows, err := store.ExportRows()
	if err != nil || len(rows) != 2 {
		t.Fatalf("metrics missing from export: rows=%+v err=%v", rows, err)
	}
	for _, row := range rows {
		if row.SleepMinutes == nil || *row.SleepMinutes != sleep || row.CoffeeCount == nil || *row.CoffeeCount != 0 {
			t.Fatalf("metrics missing from export row: %+v", row)
		}
	}

	badSleep := 7*60 + 1
	if err := store.SaveDay("2026-11-15", "2026-11-15", nil, &badSleep, nil, nil); err == nil {
		t.Fatal("expected non-quarter-hour sleep to fail")
	}
	badCoffee := 21
	if err := store.SaveDay("2026-11-15", "2026-11-15", nil, nil, &badCoffee, nil); err == nil {
		t.Fatal("expected excessive coffee count to fail")
	}
}

func TestMigrationAddsMetricColumnsToExistingDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "praxis.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE habits (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, criterion TEXT NOT NULL DEFAULT '', position INTEGER NOT NULL, active_from TEXT NOT NULL, active_until TEXT, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE days (date TEXT PRIMARY KEY, rating INTEGER, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE habit_values (day_date TEXT NOT NULL REFERENCES days(date) ON DELETE CASCADE, habit_id INTEGER NOT NULL REFERENCES habits(id), done INTEGER NOT NULL, PRIMARY KEY (day_date, habit_id));
INSERT INTO habits (name, position, active_from) VALUES ('Lire', 1, '2026-11-01');
INSERT INTO days (date, rating) VALUES ('2026-11-14', 8);`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(dbPath, filepath.Join(dir, "unused.json"), "2026-11-15")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	day, err := store.Day("2026-11-14")
	if err != nil || !day.Saved || day.SleepMinutes != nil || day.CoffeeCount != nil {
		t.Fatalf("existing day changed by migration: day=%+v err=%v", day, err)
	}
	sleep, coffee := 8*60, 2
	if err := store.SaveDay("2026-11-14", "2026-11-15", day.Rating, &sleep, &coffee, nil); err != nil {
		t.Fatal(err)
	}
}

func TestPushSubscriptionsAndDelivery(t *testing.T) {
	store := testStore(t)
	subscription := PushSubscription{Endpoint: "https://push.example/one", P256DH: "key", Auth: "auth"}
	if err := store.UpsertPushSubscription(subscription); err != nil {
		t.Fatal(err)
	}
	subscriptions, err := store.PushSubscriptions()
	if err != nil || len(subscriptions) != 1 {
		t.Fatalf("unexpected subscriptions: %+v err=%v", subscriptions, err)
	}
	if err := store.MarkPushDelivered("2026-11-15", subscriptions[0].ID); err != nil {
		t.Fatal(err)
	}
	delivered, err := store.PushDelivered("2026-11-15", subscriptions[0].ID)
	if err != nil || !delivered {
		t.Fatalf("delivery not recorded: delivered=%v err=%v", delivered, err)
	}
	if err := store.DeletePushSubscription(subscription.Endpoint); err != nil {
		t.Fatal(err)
	}
	subscriptions, err = store.PushSubscriptions()
	if err != nil || len(subscriptions) != 0 {
		t.Fatalf("subscription not deleted: %+v err=%v", subscriptions, err)
	}
}

func TestExportIncludesRatingOnlyDay(t *testing.T) {
	store := testStore(t)
	rating := 8
	if err := store.SaveDay("2026-11-14", "2026-11-15", &rating, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	rows, err := store.ExportRows()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Date != "2026-11-14" || rows[0].Rating == nil || *rows[0].Rating != 8 || rows[0].Habit != "" || rows[0].Done != nil {
		t.Fatalf("rating-only day missing from export: %+v", rows)
	}
}
