// Package server serves the launcher page and its status API.
package server

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"

	"gate/internal/monitor"
)

// StatusSource provides the data shown on the page.
type StatusSource interface {
	Status() monitor.Status
}

// Handler returns the routes: the page, its static files, /api/status and
// /up for health checks.
func Handler(source StatusSource, assets fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, assets, "index.html")
	})
	mux.Handle("GET /static/", http.FileServerFS(assets))
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if err := json.NewEncoder(w).Encode(source.Status()); err != nil {
			slog.Error("encoding status failed", "error", err)
		}
	})
	mux.HandleFunc("GET /up", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok\n"))
	})
	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers := w.Header()
		headers.Set("X-Content-Type-Options", "nosniff")
		headers.Set("X-Frame-Options", "DENY")
		headers.Set("Referrer-Policy", "same-origin")
		headers.Set("Content-Security-Policy",
			"default-src 'self'; style-src 'self' https://cdn.jsdelivr.net; font-src https://cdn.jsdelivr.net; img-src 'self' data:; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}
