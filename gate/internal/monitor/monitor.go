// Package monitor checks that each app answers, for the launcher page.
package monitor

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"gate/internal/config"
)

const (
	checkTimeout = 3 * time.Second
	localHost    = "127.0.0.1"
)

// AppStatus is one app with the result of its last health check.
type AppStatus struct {
	Name      string `json:"name"`
	Icon      string `json:"icon"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Path      string `json:"path"`
	Status    string `json:"status"` // "up", "down" or "unknown"
	LatencyMS *int64 `json:"latency_ms"`
}

// Status is everything the page shows.
type Status struct {
	Title         string      `json:"title"`
	Hostname      string      `json:"hostname"`
	Model         string      `json:"model"`
	UptimeSeconds *int64      `json:"uptime_seconds"`
	Apps          []AppStatus `json:"apps"`
	CheckedAt     *time.Time  `json:"checked_at"`
}

// Monitor keeps the latest Status up to date.
type Monitor struct {
	config     config.Config
	client     *http.Client
	procRoot   string
	model      string
	hostname   string
	now        func() time.Time
	resolveApp func(config.App) string

	mu   sync.RWMutex
	apps []AppStatus
	at   *time.Time
}

// New returns a Monitor for cfg. Every app starts as "unknown".
func New(cfg config.Config) *Monitor {
	hostname, _ := os.Hostname()
	m := &Monitor{
		config:   cfg,
		client:   &http.Client{Timeout: checkTimeout},
		procRoot: "/proc",
		hostname: hostname,
		now:      time.Now,
	}
	m.resolveApp = m.appURL
	m.model = readModel(m.procRoot)
	m.apps = make([]AppStatus, len(cfg.Apps))
	for index, app := range cfg.Apps {
		m.apps[index] = AppStatus{
			Name: app.Name, Icon: app.Icon, Host: app.Host, Port: app.Port, Path: app.Path, Status: "unknown",
		}
	}
	return m
}

// Run checks everything immediately, then every interval until ctx ends.
func (m *Monitor) Run(ctx context.Context) {
	ticker := time.NewTicker(m.config.Interval)
	defer ticker.Stop()
	for {
		m.Check(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Check runs every health check in parallel.
func (m *Monitor) Check(ctx context.Context) {
	apps := make([]AppStatus, len(m.config.Apps))
	var wait sync.WaitGroup
	for index, app := range m.config.Apps {
		wait.Go(func() { apps[index] = m.checkApp(ctx, app) })
	}
	wait.Wait()

	at := m.now().UTC()
	m.mu.Lock()
	m.apps, m.at = apps, &at
	m.mu.Unlock()
}

// Status returns the latest results with a fresh uptime.
func (m *Monitor) Status() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	title := m.config.Title
	if title == "" {
		title = m.hostname
	}
	return Status{
		Title:         title,
		Hostname:      m.hostname,
		Model:         m.model,
		UptimeSeconds: readUptime(m.procRoot),
		Apps:          append([]AppStatus(nil), m.apps...),
		CheckedAt:     m.at,
	}
}

// checkApp counts any HTTP answer below 500 as up: a login page or a
// redirect still means the app is running.
func (m *Monitor) checkApp(ctx context.Context, app config.App) AppStatus {
	status := AppStatus{Name: app.Name, Icon: app.Icon, Host: app.Host, Port: app.Port, Path: app.Path, Status: "down"}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, m.resolveApp(app), nil)
	if err != nil {
		return status
	}
	started := m.now()
	response, err := m.client.Do(request)
	if err != nil {
		slog.Debug("app check failed", "app", app.Name, "error", err)
		return status
	}
	response.Body.Close()
	latency := m.now().Sub(started).Milliseconds()
	status.LatencyMS = &latency
	if response.StatusCode < http.StatusInternalServerError {
		status.Status = "up"
	}
	return status
}

func (m *Monitor) appURL(app config.App) string {
	host := app.Host
	if host == "" {
		host = localHost
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(app.Port)) + app.Health
}

func readUptime(procRoot string) *int64 {
	data, err := os.ReadFile(procRoot + "/uptime")
	if err != nil {
		return nil
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return nil
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return nil
	}
	whole := int64(seconds)
	return &whole
}

// readModel returns the board name, such as "Raspberry Pi Model B Rev 2".
func readModel(procRoot string) string {
	data, err := os.ReadFile(procRoot + "/device-tree/model")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.TrimRight(string(data), "\x00"))
}
