// Package store keeps metric samples in SQLite for the retention window.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"pipulse/internal/metrics"

	_ "modernc.org/sqlite"
)

// Retention is how long raw samples are kept.
const Retention = 24 * time.Hour

// Sample is one stored collection attempt: a snapshot, or an error.
type Sample struct {
	ID           int64     `json:"id"`
	SampledAt    time.Time `json:"sampled_at"`
	Status       string    `json:"status"`
	ErrorMessage string    `json:"error_message,omitempty"`
	metrics.Snapshot
}

// Point is the subset of a sample drawn on the dashboard charts.
type Point struct {
	SampledAt          time.Time `json:"sampled_at"`
	CPUUsage           *float64  `json:"cpu_usage"`
	MemoryUsage        *float64  `json:"memory_usage"`
	SwapUsage          *float64  `json:"swap_usage"`
	TemperatureCelsius *float64  `json:"temperature_celsius"`
	LoadOne            *float64  `json:"load_one"`
	LoadFive           *float64  `json:"load_five"`
	NetworkReceived    *int64    `json:"network_received"`
	NetworkTransmitted *int64    `json:"network_transmitted"`
}

// Point returns the chart values of a sample. Error samples have no values,
// which leaves a gap in the charts.
func (s Sample) Point() Point {
	return Point{
		SampledAt:          s.SampledAt,
		CPUUsage:           s.CPUUsage,
		MemoryUsage:        s.MemoryUsage,
		SwapUsage:          s.SwapUsage,
		TemperatureCelsius: s.TemperatureCelsius,
		LoadOne:            s.LoadOne,
		LoadFive:           s.LoadFive,
		NetworkReceived:    s.NetworkReceivedBytesPerSecond,
		NetworkTransmitted: s.NetworkTransmittedBytesPerSecond,
	}
}

// Store wraps the SQLite database.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

const schema = `
CREATE TABLE IF NOT EXISTS metric_samples (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	sampled_at INTEGER NOT NULL,
	status TEXT NOT NULL CHECK (status IN ('ok', 'error')),
	error_message TEXT NOT NULL DEFAULT '',
	data TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS index_metric_samples_on_sampled_at ON metric_samples (sampled_at);
`

// Open opens or creates the database at path. WAL mode and NORMAL sync keep
// SD card writes low.
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("prepare database %s: %w", path, err)
	}
	return &Store{db: db, now: time.Now}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Ping checks that the database answers.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// Insert stores a sample and returns it with its ID.
func (s *Store) Insert(ctx context.Context, sample Sample) (Sample, error) {
	data := []byte("{}")
	if sample.Status == "ok" {
		var err error
		if data, err = json.Marshal(sample.Snapshot); err != nil {
			return Sample{}, err
		}
	}
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO metric_samples (sampled_at, status, error_message, data) VALUES (?, ?, ?, ?)`,
		sample.SampledAt.UnixMilli(), sample.Status, sample.ErrorMessage, string(data))
	if err != nil {
		return Sample{}, err
	}
	sample.ID, err = result.LastInsertId()
	return sample, err
}

// Latest returns the most recent sample, or nil when there is none.
func (s *Store) Latest(ctx context.Context) (*Sample, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, sampled_at, status, error_message, data FROM metric_samples ORDER BY sampled_at DESC, id DESC LIMIT 1`)
	if err != nil {
		return nil, err
	}
	samples, err := scan(rows)
	if err != nil || len(samples) == 0 {
		return nil, err
	}
	return &samples[0], nil
}

// History returns at most maxPoints chart points over the retention window,
// keeping every n-th sample and always the most recent one.
func (s *Store) History(ctx context.Context, maxPoints int) ([]Point, error) {
	since := s.now().Add(-Retention).UnixMilli()
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM metric_samples WHERE sampled_at >= ?`, since).Scan(&count); err != nil {
		return nil, err
	}
	stride := max(1, int(math.Ceil(float64(count)/float64(maxPoints))))

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, sampled_at, status, error_message, data FROM metric_samples
		WHERE sampled_at >= ? AND id % ? = 0
		ORDER BY sampled_at, id LIMIT ?`, since, stride, maxPoints)
	if err != nil {
		return nil, err
	}
	samples, err := scan(rows)
	if err != nil {
		return nil, err
	}

	latest, err := s.Latest(ctx)
	if err != nil {
		return nil, err
	}
	if latest != nil && latest.SampledAt.UnixMilli() >= since &&
		(len(samples) == 0 || samples[len(samples)-1].ID != latest.ID) {
		samples = append(samples, *latest)
	}

	points := make([]Point, len(samples))
	for index, sample := range samples {
		points[index] = sample.Point()
	}
	return points, nil
}

// Prune deletes samples older than the retention window.
func (s *Store) Prune(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM metric_samples WHERE sampled_at < ?`, s.now().Add(-Retention).UnixMilli())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func scan(rows *sql.Rows) ([]Sample, error) {
	defer rows.Close()
	var samples []Sample
	for rows.Next() {
		var sample Sample
		var sampledAt int64
		var data string
		if err := rows.Scan(&sample.ID, &sampledAt, &sample.Status, &sample.ErrorMessage, &data); err != nil {
			return nil, err
		}
		sample.SampledAt = time.UnixMilli(sampledAt).UTC()
		if err := json.Unmarshal([]byte(data), &sample.Snapshot); err != nil {
			return nil, fmt.Errorf("decode sample %d: %w", sample.ID, err)
		}
		samples = append(samples, sample)
	}
	return samples, rows.Err()
}
