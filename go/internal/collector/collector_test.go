package collector

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"pipulse/internal/metrics"
	"pipulse/internal/store"
)

type fakeReader struct {
	err      error
	elapsed  []time.Duration
	previous []*metrics.Counters
}

func (r *fakeReader) Read(previous *metrics.Counters, elapsed time.Duration) (metrics.Snapshot, metrics.Counters, error) {
	r.elapsed = append(r.elapsed, elapsed)
	r.previous = append(r.previous, previous)
	if r.err != nil {
		return metrics.Snapshot{}, metrics.Counters{}, r.err
	}
	return metrics.Snapshot{LoggedInUsers: 1}, metrics.Counters{}, nil
}

type fakeStore struct {
	samples []store.Sample
	prunes  int
}

func (s *fakeStore) Insert(_ context.Context, sample store.Sample) (store.Sample, error) {
	sample.ID = int64(len(s.samples) + 1)
	s.samples = append(s.samples, sample)
	return sample, nil
}

func (s *fakeStore) Prune(context.Context) (int64, error) {
	s.prunes++
	return 0, nil
}

func TestSamplesPassPreviousCountersAndElapsedTime(t *testing.T) {
	reader, s := &fakeReader{}, &fakeStore{}
	var published []store.Sample
	c := New(reader, s, func(sample store.Sample) { published = append(published, sample) })
	clock := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return clock }

	c.Sample(context.Background())
	clock = clock.Add(15 * time.Second)
	c.Sample(context.Background())

	if reader.previous[0] != nil || reader.elapsed[0] != 0 {
		t.Error("first reading should have no previous counters")
	}
	if reader.previous[1] == nil || reader.elapsed[1] != 15*time.Second {
		t.Errorf("second reading elapsed = %v, want 15s with counters", reader.elapsed[1])
	}
	if len(published) != 2 || published[1].ID != 2 || published[1].Status != "ok" {
		t.Errorf("published = %+v", published)
	}
	if s.prunes != 1 {
		t.Errorf("pruned %d times, want once within 10 minutes", s.prunes)
	}
}

func TestFailedReadingsAreStoredAsErrors(t *testing.T) {
	reader := &fakeReader{err: errors.New(strings.Repeat("x", 600))}
	s := &fakeStore{}
	New(reader, s, nil).Sample(context.Background())

	sample := s.samples[0]
	if sample.Status != "error" || len([]rune(sample.ErrorMessage)) != 500 {
		t.Errorf("sample = %q (%d runes)", sample.Status, len([]rune(sample.ErrorMessage)))
	}
}

func TestIntervalIsClamped(t *testing.T) {
	cases := map[string]time.Duration{
		"": 15 * time.Second, "abc": 15 * time.Second, "0": 15 * time.Second,
		"0.5": time.Second, "5": 5 * time.Second, "120": time.Minute,
	}
	for value, want := range cases {
		if got := Interval(value); got != want {
			t.Errorf("Interval(%q) = %v, want %v", value, got, want)
		}
	}
}
