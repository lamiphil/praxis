package main

import (
	"strconv"
	"time"
)

// View helpers called from templates as methods, so pages get French labels
// without template funcs.

var (
	frenchWeekdays = [...]string{"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"}
	frenchMonths   = [...]string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"}
)

func parseISODate(value string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02", value)
	return t, err == nil
}

// frenchDayMonth formats "27 novembre", or "1er novembre".
func frenchDayMonth(t time.Time) string {
	day := strconv.Itoa(t.Day())
	if t.Day() == 1 {
		day = "1er"
	}
	return day + " " + frenchMonths[t.Month()-1]
}

// DateLabel is the day page title, for example "jeudi 27 novembre".
func (p dayPage) DateLabel() string {
	t, ok := parseISODate(p.Day.Date)
	if !ok {
		return p.Day.Date
	}
	return frenchWeekdays[t.Weekday()] + " " + frenchDayMonth(t)
}

// DateEyebrow places the date relative to today.
func (p dayPage) DateEyebrow() string {
	t, ok := parseISODate(p.Day.Date)
	today, okToday := parseISODate(p.Today)
	if !ok || !okToday {
		return ""
	}
	switch today.Sub(t) / (24 * time.Hour) {
	case 0:
		return "Aujourd’hui"
	case 1:
		return "Hier"
	}
	return strconv.Itoa(t.Year())
}

// DoneCount is the number of checked habits.
func (p dayPage) DoneCount() int {
	count := 0
	for _, habit := range p.Day.Habits {
		if habit.Done {
			count++
		}
	}
	return count
}

// Label is the accessible name of a calendar day.
func (c calendarCell) Label() string {
	t, ok := parseISODate(c.Date)
	if !ok {
		return c.Date
	}
	label := frenchWeekdays[t.Weekday()] + " " + frenchDayMonth(t)
	if c.Today {
		label = "Aujourd’hui, " + label
	}
	switch c.Status {
	case "rated":
		return label + ", notée"
	case "unrated":
		return label + ", enregistrée sans note"
	}
	return label + ", non enregistrée"
}
