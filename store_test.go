package main

import (
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
	if err := store.SaveDay("2026-11-15", "2026-11-15", nil, done); err != nil {
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
	if err := store.SaveDay("2026-11-15", "2026-11-15", &badRating, nil); err == nil {
		t.Fatal("expected invalid rating to fail")
	}
	if err := store.SaveDay("2026-11-16", "2026-11-15", nil, nil); err == nil {
		t.Fatal("expected future day to fail")
	}
	if err := store.SaveDay("not-a-date", "2026-11-15", nil, nil); err == nil {
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
	if err := store.SaveDay("2026-11-15", "2026-11-15", nil, map[int64]bool{day.Habits[0].ID: true}); err != nil {
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

func TestExportIncludesRatingOnlyDay(t *testing.T) {
	store := testStore(t)
	rating := 8
	if err := store.SaveDay("2026-11-14", "2026-11-15", &rating, nil); err != nil {
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
