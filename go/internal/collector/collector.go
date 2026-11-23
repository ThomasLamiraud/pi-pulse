// Package collector samples metrics on an interval, stores them and
// publishes each new sample to the live dashboard.
package collector

import (
	"context"
	"log/slog"
	"time"

	"pipulse/internal/metrics"
	"pipulse/internal/store"
)

const (
	pruneInterval    = 10 * time.Minute
	maxErrorLength   = 500
	defaultInterval  = 15 * time.Second
	minimumInterval  = time.Second
	maximumInterval  = time.Minute
	freshnessSamples = 3
)

// Reader takes one metrics reading.
type Reader interface {
	Read(previous *metrics.Counters, elapsed time.Duration) (metrics.Snapshot, metrics.Counters, error)
}

// Store persists samples.
type Store interface {
	Insert(ctx context.Context, sample store.Sample) (store.Sample, error)
	Prune(ctx context.Context) (int64, error)
}

// Collector turns successive readings into stored samples.
type Collector struct {
	reader  Reader
	store   Store
	publish func(store.Sample)
	now     func() time.Time

	previous   *metrics.Counters
	previousAt time.Time
	lastPruned time.Time
}

// New returns a Collector. publish is called with every stored sample.
func New(reader Reader, s Store, publish func(store.Sample)) *Collector {
	return &Collector{reader: reader, store: s, publish: publish, now: time.Now}
}

// Run samples immediately, then every interval until ctx is cancelled.
func (c *Collector) Run(ctx context.Context, interval time.Duration) {
	slog.Info("collector started", "interval", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		c.Sample(ctx)
		select {
		case <-ctx.Done():
			slog.Info("collector stopped")
			return
		case <-ticker.C:
		}
	}
}

// Sample takes, stores and publishes one sample. A failed reading is stored
// as an error sample so the dashboard can show it.
func (c *Collector) Sample(ctx context.Context) {
	now := c.now()
	var elapsed time.Duration
	if c.previous != nil {
		elapsed = now.Sub(c.previousAt)
	}

	sample := store.Sample{SampledAt: now.UTC(), Status: "ok"}
	snapshot, counters, err := c.reader.Read(c.previous, elapsed)
	if err != nil {
		slog.Error("metric collection failed", "error", err)
		sample.Status = "error"
		sample.ErrorMessage = truncate(err.Error(), maxErrorLength)
	} else {
		sample.Snapshot = snapshot
		c.previous, c.previousAt = &counters, now
	}

	stored, err := c.store.Insert(ctx, sample)
	if err != nil {
		slog.Error("storing sample failed", "error", err)
		return
	}
	if c.publish != nil {
		c.publish(stored)
	}
	c.pruneIfDue(ctx, now)
}

func (c *Collector) pruneIfDue(ctx context.Context, now time.Time) {
	if !c.lastPruned.IsZero() && now.Sub(c.lastPruned) < pruneInterval {
		return
	}
	if _, err := c.store.Prune(ctx); err != nil {
		slog.Error("pruning old samples failed", "error", err)
		return
	}
	c.lastPruned = now
}

// Interval parses METRIC_INTERVAL in seconds, clamped to 1–60 like the Rails
// version. Invalid or empty values use the 15 second default.
func Interval(value string) time.Duration {
	seconds, err := time.ParseDuration(value + "s")
	if value == "" || err != nil || seconds <= 0 {
		return defaultInterval
	}
	return min(max(seconds, minimumInterval), maximumInterval)
}

// FreshnessThreshold is how old the latest sample may be before the
// dashboard calls the data stale.
func FreshnessThreshold(interval time.Duration) time.Duration {
	return interval * freshnessSamples
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit-1]) + "…"
}
