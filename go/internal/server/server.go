// Package server serves the dashboard, its JSON API and a Server-Sent Events
// stream of new samples.
package server

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"pipulse/internal/store"
)

const (
	historyPoints     = 360
	heartbeatInterval = 25 * time.Second
)

// Store is the read side of the sample store.
type Store interface {
	Latest(ctx context.Context) (*store.Sample, error)
	History(ctx context.Context, maxPoints int) ([]store.Point, error)
	Ping(ctx context.Context) error
}

// Server handles every HTTP route.
type Server struct {
	store              Store
	assets             fs.FS
	interval           time.Duration
	freshnessThreshold time.Duration

	mu          sync.Mutex
	subscribers map[chan []byte]struct{}
}

// New returns a Server. assets must contain index.html and the static/ files.
func New(s Store, assets fs.FS, interval, freshnessThreshold time.Duration) *Server {
	return &Server{
		store:              s,
		assets:             assets,
		interval:           interval,
		freshnessThreshold: freshnessThreshold,
		subscribers:        map[chan []byte]struct{}{},
	}
}

// Handler returns the routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.Handle("GET /static/", http.FileServerFS(s.assets))
	mux.HandleFunc("GET /api/dashboard", s.dashboard)
	mux.HandleFunc("GET /events", s.events)
	mux.HandleFunc("GET /up", s.up)
	return securityHeaders(mux)
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFileFS(w, r, s.assets, "index.html")
}

type dashboardResponse struct {
	IntervalSeconds           float64       `json:"interval_seconds"`
	FreshnessThresholdSeconds float64       `json:"freshness_threshold_seconds"`
	RetentionHours            float64       `json:"retention_hours"`
	Latest                    *store.Sample `json:"latest"`
	History                   []store.Point `json:"history"`
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	latest, err := s.store.Latest(r.Context())
	if err != nil {
		s.fail(w, "reading latest sample", err)
		return
	}
	history, err := s.store.History(r.Context(), historyPoints)
	if err != nil {
		s.fail(w, "reading history", err)
		return
	}
	if history == nil {
		history = []store.Point{}
	}
	writeJSON(w, dashboardResponse{
		IntervalSeconds:           s.interval.Seconds(),
		FreshnessThresholdSeconds: s.freshnessThreshold.Seconds(),
		RetentionHours:            store.Retention.Hours(),
		Latest:                    latest,
		History:                   history,
	})
}

type liveMessage struct {
	Sample store.Sample `json:"sample"`
	Point  *store.Point `json:"point"`
}

// Publish sends a new sample to every connected dashboard. Slow clients that
// fall behind miss updates instead of blocking the collector.
func (s *Server) Publish(sample store.Sample) {
	message := liveMessage{Sample: sample}
	if sample.Status == "ok" {
		point := sample.Point()
		message.Point = &point
	}
	payload, err := json.Marshal(message)
	if err != nil {
		slog.Error("encoding live sample failed", "error", err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for subscriber := range s.subscribers {
		select {
		case subscriber <- payload:
		default:
		}
	}
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	updates := make(chan []byte, 4)
	s.mu.Lock()
	s.subscribers[updates] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subscribers, updates)
		s.mu.Unlock()
	}()

	// Tell the browser to reconnect after one interval if the stream drops.
	w.Write([]byte("retry: " + strconv.FormatInt(s.interval.Milliseconds(), 10) + "\n\n"))
	flusher.Flush()

	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case payload := <-updates:
			w.Write([]byte("data: "))
			w.Write(payload)
			w.Write([]byte("\n\n"))
			flusher.Flush()
		case <-heartbeat.C:
			w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		}
	}
}

func (s *Server) up(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Write([]byte("ok\n"))
}

func (s *Server) fail(w http.ResponseWriter, action string, err error) {
	slog.Error(action+" failed", "error", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("encoding response failed", "error", err)
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers := w.Header()
		headers.Set("X-Content-Type-Options", "nosniff")
		headers.Set("X-Frame-Options", "DENY")
		headers.Set("Referrer-Policy", "same-origin")
		headers.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self' https://cdn.jsdelivr.net; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}
