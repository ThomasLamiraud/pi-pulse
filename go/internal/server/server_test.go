package server

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pipulse/internal/metrics"
	"pipulse/internal/store"
	"pipulse/web"
)

type fakeStore struct{ latest *store.Sample }

func (s fakeStore) Latest(context.Context) (*store.Sample, error) { return s.latest, nil }
func (s fakeStore) History(context.Context, int) ([]store.Point, error) {
	if s.latest == nil {
		return nil, nil
	}
	return []store.Point{s.latest.Point()}, nil
}
func (s fakeStore) Ping(context.Context) error { return nil }

func newTestServer(t *testing.T, latest *store.Sample) (*Server, *httptest.Server) {
	t.Helper()
	srv := New(fakeStore{latest: latest}, web.Assets, 15*time.Second, 45*time.Second)
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(httpServer.Close)
	return srv, httpServer
}

func TestServesThePageAndAssets(t *testing.T) {
	_, httpServer := newTestServer(t, nil)
	for path, want := range map[string]string{"/": "PiPulse", "/static/app.js": "EventSource", "/up": "ok"} {
		response, err := http.Get(httpServer.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body := new(strings.Builder)
		bufio.NewReader(response.Body).WriteTo(body)
		response.Body.Close()
		if response.StatusCode != http.StatusOK || !strings.Contains(body.String(), want) {
			t.Errorf("GET %s = %d, body missing %q", path, response.StatusCode, want)
		}
	}
}

func TestDashboardReturnsTheLatestSampleAndHistory(t *testing.T) {
	cpu := 42.5
	latest := &store.Sample{ID: 7, SampledAt: time.Now(), Status: "ok", Snapshot: metrics.Snapshot{CPUUsage: &cpu}}
	_, httpServer := newTestServer(t, latest)

	response, err := http.Get(httpServer.URL + "/api/dashboard")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body struct {
		FreshnessThresholdSeconds float64 `json:"freshness_threshold_seconds"`
		Latest                    struct {
			ID       int64   `json:"id"`
			CPUUsage float64 `json:"cpu_usage"`
		} `json:"latest"`
		History []map[string]any `json:"history"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Latest.ID != 7 || body.Latest.CPUUsage != 42.5 || len(body.History) != 1 || body.FreshnessThresholdSeconds != 45 {
		t.Errorf("dashboard = %+v", body)
	}
}

func TestPublishStreamsNewSamples(t *testing.T) {
	srv, httpServer := newTestServer(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/events", nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	if line, _ := reader.ReadString('\n'); !strings.HasPrefix(line, "retry: 15000") {
		t.Fatalf("first line = %q, want a retry hint", line)
	}

	// The subscription is registered before the retry hint is flushed.
	srv.Publish(store.Sample{ID: 3, SampledAt: time.Now(), Status: "error", ErrorMessage: "boom"})
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if data, found := strings.CutPrefix(line, "data: "); found {
			var message struct {
				Sample store.Sample `json:"sample"`
				Point  *store.Point `json:"point"`
			}
			if err := json.Unmarshal([]byte(data), &message); err != nil {
				t.Fatal(err)
			}
			if message.Sample.ID != 3 || message.Point != nil {
				t.Errorf("message = %+v; error samples have no chart point", message)
			}
			return
		}
	}
}
