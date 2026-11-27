package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
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
	if err := store.SaveDay("2026-11-15", "2026-11-15", nil, nil, done); err != nil {
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
	if err := store.SaveDay("2026-11-15", "2026-11-15", &badRating, nil, nil); err == nil {
		t.Fatal("expected invalid rating to fail")
	}
	if err := store.SaveDay("2026-11-16", "2026-11-15", nil, nil, nil); err == nil {
		t.Fatal("expected future day to fail")
	}
	if err := store.SaveDay("not-a-date", "2026-11-15", nil, nil, nil); err == nil {
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
	if err := store.SaveDay("2026-11-15", "2026-11-15", nil, nil, map[int64]bool{day.Habits[0].ID: true}); err != nil {
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

func TestArchiveAndReactivateHabit(t *testing.T) {
	store := testStore(t)
	habits, err := store.Habits()
	if err != nil {
		t.Fatal(err)
	}
	id := habits[0].ID
	if err := store.ArchiveHabit(id, "2026-11-30"); err != nil {
		t.Fatalf("archive failed: %v", err)
	}
	reloaded, err := store.Habits()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded[0].ActiveUntil != "2026-11-30" {
		t.Fatalf("expected active_until to be set, got %q", reloaded[0].ActiveUntil)
	}
	if err := store.ReactivateHabit(id); err != nil {
		t.Fatalf("reactivate failed: %v", err)
	}
	reloaded, err = store.Habits()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded[0].ActiveUntil != "" {
		t.Fatalf("expected active_until cleared, got %q", reloaded[0].ActiveUntil)
	}
}

func TestArchiveHabitRejectsDateBeforeActiveFrom(t *testing.T) {
	store := testStore(t)
	habits, err := store.Habits()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ArchiveHabit(habits[0].ID, "2020-01-01"); err == nil {
		t.Fatal("expected archive before active_from to fail")
	}
}

func TestArchiveHabitRejectsHidingRecordedValues(t *testing.T) {
	store := testStore(t)
	day, err := store.Day("2026-11-15")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDay("2026-11-15", "2026-11-15", nil, nil, map[int64]bool{day.Habits[0].ID: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.ArchiveHabit(day.Habits[0].ID, "2026-11-14"); err == nil {
		t.Fatal("expected archive hiding a recorded value to fail")
	}
}

func TestArchiveUnknownHabit(t *testing.T) {
	store := testStore(t)
	if err := store.ArchiveHabit(999999, "2026-11-30"); err == nil {
		t.Fatal("expected archive of unknown habit to fail")
	}
	if err := store.ReactivateHabit(999999); err == nil {
		t.Fatal("expected reactivate of unknown habit to fail")
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

func TestEditHabitWithZeroPositionKeepsCurrentPosition(t *testing.T) {
	store := testStore(t)
	habits, err := store.Habits()
	if err != nil {
		t.Fatal(err)
	}
	// habits[0] should have Position 1 initially
	if habits[0].Position != 1 {
		t.Fatalf("expected initial position 1, got %d", habits[0].Position)
	}
	// Edit the habit with Position 0 (simulating missing form field)
	habit := habits[0]
	habit.Name = "Modified Name"
	habit.Position = 0 // Indicate "keep current position"
	if err := store.SaveHabit(habit); err != nil {
		t.Fatal(err)
	}
	// Check that position was preserved
	reloaded, err := store.Habits()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded[0].Position != 1 {
		t.Fatalf("expected position to remain 1, got %d", reloaded[0].Position)
	}
	if reloaded[0].Name != "Modified Name" {
		t.Fatalf("expected name to be updated to 'Modified Name', got %q", reloaded[0].Name)
	}
}

func measurementByKey(t *testing.T, store *Store, key string) Measurement {
	t.Helper()
	measurements, err := store.Measurements()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range measurements {
		if m.Key == key {
			return m
		}
	}
	t.Fatalf("measurement %q not found in %+v", key, measurements)
	return Measurement{}
}

func floatPointer(value float64) *float64 { return &value }

func TestSeededMeasurementsSaveValidateTrendAndExport(t *testing.T) {
	store := testStore(t)
	sleep := measurementByKey(t, store, "sommeil")
	coffee := measurementByKey(t, store, "cafes")
	if sleep.Step != 0.25 || sleep.Max != 24 || *sleep.TargetMin != 7 || *sleep.TargetMax != 9 || !sleep.Reminder || coffee.Step != 1 || coffee.TargetMin != nil || *coffee.TargetMax != 3 {
		t.Fatalf("unexpected seeds: %+v %+v", sleep, coffee)
	}
	values := map[int64]*float64{sleep.ID: floatPointer(7.25), coffee.ID: floatPointer(0)}
	if err := store.SaveDay("2026-11-15", "2026-11-15", nil, values, nil); err != nil {
		t.Fatal(err)
	}
	day, err := store.Day("2026-11-15")
	if err != nil {
		t.Fatal(err)
	}
	if len(day.Measurements) != 2 || *day.Measurements[0].Value != 7.25 || *day.Measurements[1].Value != 0 {
		t.Fatalf("measurements not preserved: %+v", day.Measurements)
	}
	trends, err := store.MeasurementValues("2026-11-01", "2026-11-30")
	if err != nil || len(trends) != 2 {
		t.Fatalf("measurements missing from trends: rows=%+v err=%v", trends, err)
	}
	rows, err := store.ExportRows()
	if err != nil || len(rows) != 2 {
		t.Fatalf("legacy export rows: %+v err=%v", rows, err)
	}
	for _, row := range rows {
		if row.SleepHours == nil || *row.SleepHours != 7.25 || row.Coffees == nil || *row.Coffees != 0 {
			t.Fatalf("legacy export row: %+v", row)
		}
	}
	var legacySleep, legacyCoffee sql.NullInt64
	if err := store.db.QueryRow("SELECT sleep_minutes, coffee_count FROM days WHERE date = '2026-11-15'").Scan(&legacySleep, &legacyCoffee); err != nil {
		t.Fatal(err)
	}
	if legacySleep.Int64 != 435 || !legacyCoffee.Valid || legacyCoffee.Int64 != 0 {
		t.Fatalf("legacy mirror not written: %v %v", legacySleep, legacyCoffee)
	}

	for _, bad := range []map[int64]*float64{
		{sleep.ID: floatPointer(7.1)},
		{sleep.ID: floatPointer(24.25)},
		{coffee.ID: floatPointer(21)},
		{coffee.ID: floatPointer(1.5)},
		{9999: floatPointer(1)},
	} {
		if err := store.SaveDay("2026-11-15", "2026-11-15", nil, bad, nil); err == nil {
			t.Fatalf("expected %v to fail", bad)
		}
	}

	if err := store.SaveDay("2026-11-15", "2026-11-15", nil, map[int64]*float64{sleep.ID: nil}, nil); err != nil {
		t.Fatal(err)
	}
	day, _ = store.Day("2026-11-15")
	if day.Measurements[0].Value != nil || day.Measurements[1].Value == nil {
		t.Fatalf("clearing one measurement changed another: %+v", day.Measurements)
	}
	if err := store.db.QueryRow("SELECT sleep_minutes FROM days WHERE date = '2026-11-15'").Scan(&legacySleep); err != nil || legacySleep.Valid {
		t.Fatalf("legacy sleep not cleared: %v %v", legacySleep, err)
	}
}

func TestMeasurementLifecycle(t *testing.T) {
	store := testStore(t)
	weight := Measurement{Name: "Poids élevé", Unit: "kg", Step: 0.1, Min: 30, Max: 200, TargetMax: floatPointer(80)}
	if err := store.SaveMeasurement(weight); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMeasurement(weight); err != nil {
		t.Fatal(err)
	}
	first := measurementByKey(t, store, "poids-eleve")
	second := measurementByKey(t, store, "poids-eleve-2")
	if first.Reminder || first.Position != 3 || second.Position != 4 {
		t.Fatalf("unexpected new measurements: %+v %+v", first, second)
	}

	for _, bad := range []Measurement{
		{Name: "", Step: 1, Min: 0, Max: 1},
		{Name: "x", Step: 0.3, Min: 0, Max: 1},
		{Name: "x", Step: 1, Min: 5, Max: 5},
		{Name: "x", Step: 1, Min: 0.5, Max: 5},
		{Name: "x", Step: 1, Min: 0, Max: 5, TargetMin: floatPointer(6)},
		{Name: "x", Step: 1, Min: 0, Max: 5, TargetMin: floatPointer(4), TargetMax: floatPointer(3)},
	} {
		if err := store.SaveMeasurement(bad); err == nil {
			t.Fatalf("expected invalid definition to fail: %+v", bad)
		}
	}

	if err := store.SaveDay("2026-11-14", "2026-11-15", nil, map[int64]*float64{first.ID: floatPointer(72.3)}, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteMeasurement(first.ID); err == nil {
		t.Fatal("expected deleting a measurement with values to fail")
	}
	if err := store.DeleteMeasurement(second.ID); err != nil {
		t.Fatal(err)
	}

	narrowed := first
	narrowed.Step = 1
	if err := store.SaveMeasurement(narrowed); err == nil {
		t.Fatal("expected a precision excluding recorded values to fail")
	}
	narrowed = first
	narrowed.Max = 70
	narrowed.TargetMax = nil
	if err := store.SaveMeasurement(narrowed); err == nil {
		t.Fatal("expected bounds excluding recorded values to fail")
	}

	archived := first
	archived.Archived = true
	if err := store.SaveMeasurement(archived); err != nil {
		t.Fatal(err)
	}
	active, err := store.ActiveMeasurements()
	if err != nil || len(active) != 2 {
		t.Fatalf("archived measurement still active: %+v err=%v", active, err)
	}
	day, err := store.Day("2026-11-14")
	if err != nil || len(day.Measurements) != 3 || !day.Measurements[2].Archived || *day.Measurements[2].Value != 72.3 {
		t.Fatalf("archived value hidden from its day: %+v err=%v", day.Measurements, err)
	}
	if err := store.SaveDay("2026-11-14", "2026-11-15", nil, map[int64]*float64{first.ID: floatPointer(72.5)}, nil); err != nil {
		t.Fatalf("editing an existing archived value failed: %v", err)
	}
	if err := store.SaveDay("2026-11-15", "2026-11-15", nil, map[int64]*float64{first.ID: floatPointer(72.5)}, nil); err == nil {
		t.Fatal("expected a new value for an archived measurement to fail")
	}
	today, _ := store.Day("2026-11-15")
	if len(today.Measurements) != 2 {
		t.Fatalf("archived measurement shown on a day without value: %+v", today.Measurements)
	}
	exported, err := store.MeasurementExportRows()
	if err != nil || len(exported) != 1 || exported[0].Key != "poids-eleve" || exported[0].Value != 72.5 {
		t.Fatalf("archived value missing from export: %+v err=%v", exported, err)
	}

	measurements, _ := store.Measurements()
	if err := store.ReorderMeasurements([]int64{measurements[2].ID, measurements[0].ID, measurements[1].ID}); err != nil {
		t.Fatal(err)
	}
	reordered, _ := store.Measurements()
	if reordered[0].ID != measurements[2].ID || reordered[0].ValueCount != 1 {
		t.Fatalf("unexpected order: %+v", reordered)
	}
	if err := store.ReorderMeasurements([]int64{measurements[0].ID}); err == nil {
		t.Fatal("expected incomplete order to fail")
	}
}

func TestMissingReminderMeasurements(t *testing.T) {
	store := testStore(t)
	sleep := measurementByKey(t, store, "sommeil")
	if err := store.SaveMeasurement(Measurement{Name: "Pas", Step: 1, Min: 0, Max: 100000}); err != nil {
		t.Fatal(err)
	}
	missing, err := store.MissingReminderMeasurements("2026-11-15")
	if err != nil || len(missing) != 2 || missing[0] != "Sommeil" || missing[1] != "Cafés" {
		t.Fatalf("unexpected missing reminders: %v err=%v", missing, err)
	}
	if err := store.SaveDay("2026-11-15", "2026-11-15", nil, map[int64]*float64{sleep.ID: floatPointer(8)}, nil); err != nil {
		t.Fatal(err)
	}
	missing, _ = store.MissingReminderMeasurements("2026-11-15")
	if len(missing) != 1 || missing[0] != "Cafés" {
		t.Fatalf("unexpected missing reminders after save: %v", missing)
	}
}

func TestMigrationBackfillsLegacySleepAndCoffee(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "praxis.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE habits (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, criterion TEXT NOT NULL DEFAULT '', position INTEGER NOT NULL, active_from TEXT NOT NULL, active_until TEXT, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE days (date TEXT PRIMARY KEY, rating INTEGER, sleep_minutes INTEGER, coffee_count INTEGER, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE habit_values (day_date TEXT NOT NULL REFERENCES days(date) ON DELETE CASCADE, habit_id INTEGER NOT NULL REFERENCES habits(id), done INTEGER NOT NULL, PRIMARY KEY (day_date, habit_id));
INSERT INTO habits (name, position, active_from) VALUES ('Lire', 1, '2026-11-01');
INSERT INTO days (date, rating, sleep_minutes, coffee_count) VALUES
  ('2026-11-12', 5, 435, 2), ('2026-11-13', NULL, NULL, 0), ('2026-11-14', 8, 480, NULL);`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for reopen := 0; reopen < 2; reopen++ {
		store, err := OpenStore(dbPath, filepath.Join(dir, "unused.json"), "2026-11-15")
		if err != nil {
			t.Fatal(err)
		}
		rows, err := store.MeasurementExportRows()
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, row := range rows {
			got = append(got, row.Date+" "+row.Key+" "+formatNumber(row.Value))
		}
		want := "2026-11-12 sommeil 7.25|2026-11-12 cafes 2|2026-11-13 cafes 0|2026-11-14 sommeil 8"
		if strings.Join(got, "|") != want {
			t.Fatalf("open %d backfill = %v", reopen+1, got)
		}
		measurements, _ := store.Measurements()
		if len(measurements) != 2 {
			t.Fatalf("open %d seeded %d measurements", reopen+1, len(measurements))
		}
		day, _ := store.Day("2026-11-12")
		if day.Rating == nil || *day.Rating != 5 {
			t.Fatalf("rating changed by migration: %+v", day)
		}
		store.Close()
	}
}

func TestDeletedSeedIsNotRecreated(t *testing.T) {
	dir := t.TempDir()
	seedPath := filepath.Join(dir, "seed.json")
	if err := os.WriteFile(seedPath, []byte(`[{"name":"Lire"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "praxis.db")
	store, err := OpenStore(dbPath, seedPath, "2026-11-15")
	if err != nil {
		t.Fatal(err)
	}
	coffee := measurementByKey(t, store, "cafes")
	if err := store.DeleteMeasurement(coffee.ID); err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = OpenStore(dbPath, seedPath, "2026-11-15")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	measurements, _ := store.Measurements()
	if len(measurements) != 1 || measurements[0].Key != "sommeil" {
		t.Fatalf("deleted seed was recreated: %+v", measurements)
	}
}

func TestMeasurementKey(t *testing.T) {
	for name, want := range map[string]string{
		"Cafés":              "cafes",
		"  Poids (kg) ":      "poids-kg",
		"Œufs mangés!":       "oeufs-manges",
		"!!!":                "mesure",
		"Fréquence au repos": "frequence-au-repos",
	} {
		if got := measurementKey(name); got != want {
			t.Errorf("measurementKey(%q) = %q, want %q", name, got, want)
		}
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
	if err := store.SaveDay("2026-11-14", "2026-11-15", &rating, nil, nil); err != nil {
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
