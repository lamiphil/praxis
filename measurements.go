package main

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Measurement is a user-defined numeric daily value. Values are stored as
// REAL and must be a multiple of Step between Min and Max.
type Measurement struct {
	ID         int64
	Key        string
	Name       string
	Unit       string
	Step       float64
	Min        float64
	Max        float64
	TargetMin  *float64
	TargetMax  *float64
	Hint       string
	Position   int
	Reminder   bool
	Archived   bool
	ValueCount int
}

type DayMeasurement struct {
	Measurement
	Value *float64
}

type MeasurementValue struct {
	Date          string
	MeasurementID int64
	Value         float64
}

type MeasurementExportRow struct {
	Date  string
	Key   string
	Name  string
	Unit  string
	Value float64
}

var allowedSteps = []float64{1, 0.5, 0.25, 0.1, 0.01}

const measurementColumns = `m.id, m.key, m.name, m.unit, m.step, m.min_value, m.max_value,
       m.target_min, m.target_max, m.hint, m.position, m.reminder, m.archived_at IS NOT NULL`

const measurementSchema = `
CREATE TABLE IF NOT EXISTS measurements (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    key TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    unit TEXT NOT NULL DEFAULT '',
    step REAL NOT NULL CHECK (step > 0),
    min_value REAL NOT NULL,
    max_value REAL NOT NULL,
    target_min REAL,
    target_max REAL,
    hint TEXT NOT NULL DEFAULT '',
    position INTEGER NOT NULL,
    reminder INTEGER NOT NULL DEFAULT 0 CHECK (reminder IN (0, 1)),
    archived_at TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (min_value < max_value)
);
CREATE TABLE IF NOT EXISTS measurement_values (
    day_date TEXT NOT NULL REFERENCES days(date) ON DELETE CASCADE,
    measurement_id INTEGER NOT NULL REFERENCES measurements(id),
    value REAL NOT NULL,
    PRIMARY KEY (day_date, measurement_id)
);
CREATE INDEX IF NOT EXISTS measurement_values_by_measurement ON measurement_values(measurement_id, day_date);
CREATE TABLE IF NOT EXISTS schema_markers (
    name TEXT PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);`

// Legacy sleep and coffee columns stay in days so that v0.2.x binaries keep
// working after a rollback. They mirror the "sommeil" and "cafes" values.
const legacyBackfill = `
INSERT OR IGNORE INTO measurement_values (day_date, measurement_id, value)
SELECT d.date, m.id, d.sleep_minutes / 60.0 FROM days d JOIN measurements m ON m.key = 'sommeil'
WHERE d.sleep_minutes IS NOT NULL;
INSERT OR IGNORE INTO measurement_values (day_date, measurement_id, value)
SELECT d.date, m.id, d.coffee_count FROM days d JOIN measurements m ON m.key = 'cafes'
WHERE d.coffee_count IS NOT NULL;`

const legacyMirror = `
UPDATE days SET
  sleep_minutes = (
    SELECT CAST(ROUND(v.value * 60) AS INTEGER)
    FROM measurement_values v JOIN measurements m ON m.id = v.measurement_id
    WHERE m.key = 'sommeil' AND v.day_date = days.date AND v.value BETWEEN 0 AND 24
      AND ABS(v.value * 60 - ROUND(v.value * 60)) < 1e-6
      AND CAST(ROUND(v.value * 60) AS INTEGER) % 15 = 0),
  coffee_count = (
    SELECT CAST(v.value AS INTEGER)
    FROM measurement_values v JOIN measurements m ON m.id = v.measurement_id
    WHERE m.key = 'cafes' AND v.day_date = days.date AND v.value BETWEEN 0 AND 20
      AND v.value = CAST(v.value AS INTEGER))
WHERE date = ?`

func (s *Store) migrateMeasurements() error {
	if _, err := s.db.Exec(measurementSchema); err != nil {
		return fmt.Errorf("migrate measurements: %w", err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var seeded int
	if err := tx.QueryRow("SELECT COUNT(*) FROM schema_markers WHERE name = 'seed_measurements'").Scan(&seeded); err != nil {
		return err
	}
	if seeded == 0 {
		seven, nine, three := 7.0, 9.0, 3.0
		for _, m := range []Measurement{
			{Key: "sommeil", Name: "Sommeil", Unit: "h", Step: 0.25, Min: 0, Max: 24, TargetMin: &seven, TargetMax: &nine, Hint: "Sommeil de nuit, attribué au jour du réveil.", Reminder: true},
			{Key: "cafes", Name: "Cafés", Unit: "café(s)", Step: 1, Min: 0, Max: 20, TargetMax: &three, Hint: "Une boisson au café caféinée compte pour une.", Reminder: true},
		} {
			if _, err := tx.Exec(`
INSERT OR IGNORE INTO measurements (key, name, unit, step, min_value, max_value, target_min, target_max, hint, position, reminder)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(position), 0) + 1 FROM measurements), ?)`,
				m.Key, m.Name, m.Unit, m.Step, m.Min, m.Max, nullableFloat(m.TargetMin), nullableFloat(m.TargetMax), m.Hint, m.Reminder); err != nil {
				return fmt.Errorf("seed measurements: %w", err)
			}
		}
		if _, err := tx.Exec("INSERT INTO schema_markers (name) VALUES ('seed_measurements')"); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(legacyBackfill); err != nil {
		return fmt.Errorf("backfill measurements: %w", err)
	}
	return tx.Commit()
}

type rowScanner interface{ Scan(...any) error }

func scanMeasurement(row rowScanner, extra ...any) (Measurement, error) {
	var m Measurement
	var targetMin, targetMax sql.NullFloat64
	dest := append([]any{&m.ID, &m.Key, &m.Name, &m.Unit, &m.Step, &m.Min, &m.Max,
		&targetMin, &targetMax, &m.Hint, &m.Position, &m.Reminder, &m.Archived}, extra...)
	if err := row.Scan(dest...); err != nil {
		return m, err
	}
	if targetMin.Valid {
		m.TargetMin = &targetMin.Float64
	}
	if targetMax.Valid {
		m.TargetMax = &targetMax.Float64
	}
	return m, nil
}

// Measurements returns every definition, archived ones included, in display order.
func (s *Store) Measurements() ([]Measurement, error) {
	rows, err := s.db.Query(`SELECT ` + measurementColumns + `,
       (SELECT COUNT(*) FROM measurement_values v WHERE v.measurement_id = m.id)
FROM measurements m ORDER BY m.position, m.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Measurement
	for rows.Next() {
		var count int
		m, err := scanMeasurement(rows, &count)
		if err != nil {
			return nil, err
		}
		m.ValueCount = count
		result = append(result, m)
	}
	return result, rows.Err()
}

// ActiveMeasurements returns non-archived definitions in display order.
func (s *Store) ActiveMeasurements() ([]Measurement, error) {
	rows, err := s.db.Query(`SELECT ` + measurementColumns + `
FROM measurements m WHERE m.archived_at IS NULL ORDER BY m.position, m.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Measurement
	for rows.Next() {
		m, err := scanMeasurement(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// dayMeasurements lists active definitions plus archived ones holding a value
// on date, so editing an old day never hides recorded data.
func (s *Store) dayMeasurements(date string) ([]DayMeasurement, error) {
	rows, err := s.db.Query(`SELECT `+measurementColumns+`, v.value
FROM measurements m
LEFT JOIN measurement_values v ON v.measurement_id = m.id AND v.day_date = ?
WHERE m.archived_at IS NULL OR v.value IS NOT NULL
ORDER BY m.position, m.id`, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []DayMeasurement
	for rows.Next() {
		var value sql.NullFloat64
		m, err := scanMeasurement(rows, &value)
		if err != nil {
			return nil, err
		}
		item := DayMeasurement{Measurement: m}
		if value.Valid {
			item.Value = &value.Float64
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// saveDayMeasurements writes submitted values inside the SaveDay transaction.
// A nil value clears the measurement for that date; omitted ids are untouched.
func saveDayMeasurements(tx *sql.Tx, date string, values map[int64]*float64) error {
	for id, value := range values {
		m, err := scanMeasurement(tx.QueryRow(`SELECT `+measurementColumns+` FROM measurements m WHERE m.id = ?`, id))
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("mesure %d inconnue", id)
		}
		if err != nil {
			return err
		}
		if m.Archived {
			var existing int
			if err := tx.QueryRow("SELECT COUNT(*) FROM measurement_values WHERE measurement_id = ? AND day_date = ?", id, date).Scan(&existing); err != nil {
				return err
			}
			if existing == 0 {
				return fmt.Errorf("la mesure %s est archivée", m.Name)
			}
		}
		if value == nil {
			if _, err := tx.Exec("DELETE FROM measurement_values WHERE measurement_id = ? AND day_date = ?", id, date); err != nil {
				return err
			}
			continue
		}
		if !m.ValidValue(*value) {
			return fmt.Errorf("valeur invalide pour %s", m.Name)
		}
		if _, err := tx.Exec(`
INSERT INTO measurement_values (day_date, measurement_id, value) VALUES (?, ?, ?)
ON CONFLICT(day_date, measurement_id) DO UPDATE SET value = excluded.value`, date, id, *value); err != nil {
			return err
		}
	}
	_, err := tx.Exec(legacyMirror, date)
	return err
}

func (s *Store) SaveMeasurement(m Measurement) error {
	m.Name = strings.TrimSpace(m.Name)
	m.Unit = strings.TrimSpace(m.Unit)
	m.Hint = strings.TrimSpace(m.Hint)
	if err := m.validateDefinition(); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if m.ID == 0 {
		key, err := uniqueMeasurementKey(tx, m.Name)
		if err != nil {
			return err
		}
		// The reminder column is left at its DEFAULT (0) since v0.4.0: the reminder is now global.
		if _, err := tx.Exec(`
INSERT INTO measurements (key, name, unit, step, min_value, max_value, target_min, target_max, hint, position, archived_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(position), 0) + 1 FROM measurements), CASE WHEN ? THEN CURRENT_TIMESTAMP END)`,
			key, m.Name, m.Unit, m.Step, m.Min, m.Max, nullableFloat(m.TargetMin), nullableFloat(m.TargetMax), m.Hint, m.Archived); err != nil {
			return err
		}
		return tx.Commit()
	}

	rows, err := tx.Query("SELECT value FROM measurement_values WHERE measurement_id = ?", m.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var value float64
		if err := rows.Scan(&value); err != nil {
			rows.Close()
			return err
		}
		if !m.ValidValue(value) {
			rows.Close()
			return errors.New("le type ou les bornes excluent des valeurs déjà enregistrées")
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	// The reminder column is not updated since v0.4.0: the reminder is now global. Existing values stay for rollback.
	result, err := tx.Exec(`
UPDATE measurements SET name = ?, unit = ?, step = ?, min_value = ?, max_value = ?, target_min = ?, target_max = ?,
  hint = ?, archived_at = CASE WHEN ? THEN COALESCE(archived_at, CURRENT_TIMESTAMP) END
WHERE id = ?`, m.Name, m.Unit, m.Step, m.Min, m.Max, nullableFloat(m.TargetMin), nullableFloat(m.TargetMax),
		m.Hint, m.Archived, m.ID)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil {
		return err
	} else if changed == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

// DeleteMeasurement removes a definition only while it has no recorded value.
func (s *Store) DeleteMeasurement(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRow("SELECT COUNT(*) FROM measurement_values WHERE measurement_id = ?", id).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return errors.New("cette mesure a des valeurs: archive-la plutôt")
	}
	result, err := tx.Exec("DELETE FROM measurements WHERE id = ?", id)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil {
		return err
	} else if changed == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func (s *Store) ReorderMeasurements(ids []int64) error { return s.reorder("measurements", ids) }

// MeasurementValues returns values of active measurements between two dates.
func (s *Store) MeasurementValues(from, until string) ([]MeasurementValue, error) {
	rows, err := s.db.Query(`
SELECT v.day_date, v.measurement_id, v.value
FROM measurement_values v JOIN measurements m ON m.id = v.measurement_id
WHERE m.archived_at IS NULL AND v.day_date BETWEEN ? AND ?
ORDER BY v.day_date`, from, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []MeasurementValue
	for rows.Next() {
		var value MeasurementValue
		if err := rows.Scan(&value.Date, &value.MeasurementID, &value.Value); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

// MeasurementExportRows returns every recorded value, archived measurements included.
func (s *Store) MeasurementExportRows() ([]MeasurementExportRow, error) {
	rows, err := s.db.Query(`
SELECT v.day_date, m.key, m.name, m.unit, v.value
FROM measurement_values v JOIN measurements m ON m.id = v.measurement_id
ORDER BY v.day_date, m.position, m.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []MeasurementExportRow
	for rows.Next() {
		var row MeasurementExportRow
		if err := rows.Scan(&row.Date, &row.Key, &row.Name, &row.Unit, &row.Value); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// MissingReminderMeasurements lists active measurements without a value on date.
// The per-measurement reminder opt-in column is frozen since v0.4.0: the reminder is now global.
func (s *Store) MissingReminderMeasurements(date string) ([]string, error) {
	rows, err := s.db.Query(`
SELECT m.name FROM measurements m
LEFT JOIN measurement_values v ON v.measurement_id = m.id AND v.day_date = ?
WHERE m.archived_at IS NULL AND v.value IS NULL
ORDER BY m.position, m.id`, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func (m Measurement) validateDefinition() error {
	switch {
	case m.Name == "" || len([]rune(m.Name)) > 80:
		return errors.New("nom de mesure requis (80 caractères max.)")
	case len([]rune(m.Unit)) > 20:
		return errors.New("unité trop longue (20 caractères max.)")
	case len([]rune(m.Hint)) > 200:
		return errors.New("aide trop longue (200 caractères max.)")
	case !validStep(m.Step):
		return errors.New("précision invalide")
	case !finite(m.Min) || !finite(m.Max) || m.Min >= m.Max:
		return errors.New("le minimum doit être inférieur au maximum")
	case !alignedToStep(m.Min, m.Step) || !alignedToStep(m.Max, m.Step):
		return errors.New("les bornes doivent respecter la précision")
	}
	for _, target := range []*float64{m.TargetMin, m.TargetMax} {
		if target != nil && (!finite(*target) || *target < m.Min || *target > m.Max) {
			return errors.New("l’objectif doit être entre le minimum et le maximum")
		}
	}
	if m.TargetMin != nil && m.TargetMax != nil && *m.TargetMin > *m.TargetMax {
		return errors.New("l’objectif minimum dépasse l’objectif maximum")
	}
	return nil
}

func (m Measurement) ValidValue(value float64) bool {
	const epsilon = 1e-9
	return finite(value) && value >= m.Min-epsilon && value <= m.Max+epsilon && alignedToStep(value, m.Step)
}

// TargetState returns "missing", "goal", "outside", or "recorded" when no target is set.
func (m Measurement) TargetState(value *float64) string {
	switch {
	case value == nil:
		return "missing"
	case m.TargetMin == nil && m.TargetMax == nil:
		return "recorded"
	case (m.TargetMin == nil || *value >= *m.TargetMin-1e-9) && (m.TargetMax == nil || *value <= *m.TargetMax+1e-9):
		return "goal"
	default:
		return "outside"
	}
}

func (m Measurement) Integer() bool { return m.Step == 1 }

func validStep(step float64) bool {
	for _, allowed := range allowedSteps {
		if step == allowed {
			return true
		}
	}
	return false
}

func alignedToStep(value, step float64) bool {
	quotient := value / step
	return math.Abs(quotient-math.Round(quotient)) < 1e-6
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func nullableFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

// parseDecimal accepts "7.25" or "7,25"; empty means no value.
func parseDecimal(raw string) (*float64, error) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, ",", "."))
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || !finite(value) {
		return nil, errors.New("nombre invalide")
	}
	return &value, nil
}

func formatNumber(value float64) string {
	return strconv.FormatFloat(math.Round(value*1e6)/1e6, 'f', -1, 64)
}

func formatOptionalNumber(value *float64) string {
	if value == nil {
		return ""
	}
	return formatNumber(*value)
}

var keyReplacer = strings.NewReplacer(
	"à", "a", "â", "a", "ä", "a", "á", "a", "ç", "c", "é", "e", "è", "e", "ê", "e", "ë", "e",
	"î", "i", "ï", "i", "í", "i", "ô", "o", "ö", "o", "ó", "o", "ù", "u", "û", "u", "ü", "u", "ú", "u",
	"ÿ", "y", "œ", "oe", "æ", "ae",
)

// measurementKey turns a name into a stable ASCII slug used by CSV and Grafana.
func measurementKey(name string) string {
	var builder strings.Builder
	dash := false
	for _, r := range keyReplacer.Replace(strings.ToLower(name)) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			builder.WriteRune(r)
			dash = false
		} else if builder.Len() > 0 && !dash {
			builder.WriteByte('-')
			dash = true
		}
	}
	key := strings.TrimSuffix(builder.String(), "-")
	if len(key) > 40 {
		key = strings.TrimSuffix(key[:40], "-")
	}
	if key == "" {
		key = "mesure"
	}
	return key
}

func uniqueMeasurementKey(tx *sql.Tx, name string) (string, error) {
	base := measurementKey(name)
	for suffix := 1; ; suffix++ {
		key := base
		if suffix > 1 {
			key = base + "-" + strconv.Itoa(suffix)
		}
		var count int
		if err := tx.QueryRow("SELECT COUNT(*) FROM measurements WHERE key = ?", key).Scan(&count); err != nil {
			return "", err
		}
		if count == 0 {
			return key, nil
		}
	}
}
