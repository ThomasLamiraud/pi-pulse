package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"pipulse/internal/metrics"
)

var now = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return now }
	t.Cleanup(func() { s.Close() })
	return s
}

func okSample(at time.Time, cpu float64) Sample {
	return Sample{SampledAt: at, Status: "ok", Snapshot: metrics.Snapshot{CPUUsage: &cpu}}
}

func TestStoresAndReadsTheLatestSample(t *testing.T) {
	s, ctx := openTestStore(t), context.Background()
	if latest, err := s.Latest(ctx); err != nil || latest != nil {
		t.Fatalf("latest on empty store = %v, %v", latest, err)
	}

	s.Insert(ctx, okSample(now.Add(-time.Minute), 10))
	s.Insert(ctx, Sample{SampledAt: now, Status: "error", ErrorMessage: "boom"})

	latest, err := s.Latest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Status != "error" || latest.ErrorMessage != "boom" || latest.CPUUsage != nil {
		t.Errorf("latest = %+v, want the error sample", latest)
	}
}

func TestHistoryDownsamplesAndKeepsTheLatestPoint(t *testing.T) {
	s, ctx := openTestStore(t), context.Background()
	for index := range 1000 {
		s.Insert(ctx, okSample(now.Add(-time.Duration(1000-index)*time.Minute/2), float64(index)))
	}

	points, err := s.History(ctx, 360)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) > 361 || len(points) < 300 {
		t.Errorf("history has %d points, want about 360", len(points))
	}
	if last := points[len(points)-1]; *last.CPUUsage != 999 {
		t.Errorf("last point CPU = %v, want the latest sample", *last.CPUUsage)
	}
	for index := 1; index < len(points); index++ {
		if points[index].SampledAt.Before(points[index-1].SampledAt) {
			t.Fatal("history is not chronological")
		}
	}
}

func TestPruneDeletesSamplesOutsideRetention(t *testing.T) {
	s, ctx := openTestStore(t), context.Background()
	s.Insert(ctx, okSample(now.Add(-25*time.Hour), 1))
	s.Insert(ctx, okSample(now.Add(-time.Hour), 2))

	deleted, err := s.Prune(ctx)
	if err != nil || deleted != 1 {
		t.Fatalf("prune deleted %d, %v; want 1", deleted, err)
	}
	points, _ := s.History(ctx, 360)
	if len(points) != 1 || *points[0].CPUUsage != 2 {
		t.Errorf("history after prune = %+v", points)
	}
}
