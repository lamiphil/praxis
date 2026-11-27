package main

import "testing"

func TestDayPageDateLabels(t *testing.T) {
	for _, test := range []struct {
		date, today, label, eyebrow string
	}{
		{"2026-11-27", "2026-11-27", "vendredi 27 novembre", "Aujourd’hui"},
		{"2026-11-26", "2026-11-27", "jeudi 26 novembre", "Hier"},
		{"2026-11-01", "2026-11-27", "dimanche 1er novembre", "2026"},
	} {
		page := dayPage{Day: Day{Date: test.date}, Today: test.today}
		if got := page.DateLabel(); got != test.label {
			t.Errorf("DateLabel(%s) = %q, want %q", test.date, got, test.label)
		}
		if got := page.DateEyebrow(); got != test.eyebrow {
			t.Errorf("DateEyebrow(%s) = %q, want %q", test.date, got, test.eyebrow)
		}
	}
}

func TestDayPageDoneCount(t *testing.T) {
	page := dayPage{Day: Day{Habits: []Habit{{Done: true}, {}, {Done: true}}}}
	if got := page.DoneCount(); got != 2 {
		t.Fatalf("DoneCount = %d, want 2", got)
	}
}

func TestCalendarCellLabel(t *testing.T) {
	for _, test := range []struct {
		cell calendarCell
		want string
	}{
		{calendarCell{Date: "2026-11-27", Status: "rated", Today: true}, "Aujourd’hui, vendredi 27 novembre, notée"},
		{calendarCell{Date: "2026-11-02", Status: "unrated"}, "lundi 2 novembre, enregistrée sans note"},
		{calendarCell{Date: "2026-11-03", Status: "missing"}, "mardi 3 novembre, non enregistrée"},
	} {
		if got := test.cell.Label(); got != test.want {
			t.Errorf("Label() = %q, want %q", got, test.want)
		}
	}
}
