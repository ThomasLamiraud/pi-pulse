package monitor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"gate/internal/config"
)

func TestCheckMarksAppsUpOrDown(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/up" {
			t.Errorf("health path = %q, want /up", r.URL.Path)
		}
	}))
	defer healthy.Close()
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer failing.Close()

	urls := map[string]string{"Healthy": healthy.URL + "/up", "Failing": failing.URL + "/", "Stopped": "http://127.0.0.1:1/"}
	m := New(config.Config{Apps: []config.App{
		{Name: "Healthy", Port: 1, Health: "/up"}, {Name: "Failing", Port: 2}, {Name: "Stopped", Port: 3},
	}})
	m.resolveApp = func(app config.App) string { return urls[app.Name] }

	if got := m.Status().Apps[0].Status; got != "unknown" {
		t.Errorf("status before the first check = %q, want unknown", got)
	}
	m.Check(context.Background())

	apps := m.Status().Apps
	want := map[string]string{"Healthy": "up", "Failing": "down", "Stopped": "down"}
	for _, app := range apps {
		if app.Status != want[app.Name] {
			t.Errorf("%s = %s, want %s", app.Name, app.Status, want[app.Name])
		}
	}
	if apps[0].LatencyMS == nil || apps[2].LatencyMS != nil {
		t.Error("latency should be set only when the app answered")
	}
}

func TestReadsUptimeAndModel(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "device-tree"), 0o755)
	os.WriteFile(filepath.Join(root, "uptime"), []byte("523408.71 1987654.32\n"), 0o644)
	os.WriteFile(filepath.Join(root, "device-tree", "model"), []byte("Raspberry Pi Model B Rev 2\x00"), 0o644)

	if uptime := readUptime(root); uptime == nil || *uptime != 523408 {
		t.Errorf("uptime = %v", uptime)
	}
	if model := readModel(root); model != "Raspberry Pi Model B Rev 2" {
		t.Errorf("model = %q", model)
	}
}

func TestTitleDefaultsToHostname(t *testing.T) {
	m := New(config.Config{})
	m.hostname = "homemonitor"
	if title := m.Status().Title; title != "homemonitor" {
		t.Errorf("title = %q", title)
	}
}
