package config

import (
	"strings"
	"testing"
	"time"
)

func TestParseFillsDefaults(t *testing.T) {
	config, err := Parse([]byte(`
apps:
  - name: HomeMonitor
    port: 8000
    icon: temperature
  - name: Pi-hole
    host: 192.168.1.25
    port: 80
    path: admin
`))
	if err != nil {
		t.Fatal(err)
	}
	if config.Port != 80 || config.Interval != 10*time.Second {
		t.Errorf("defaults = port %d, interval %v", config.Port, config.Interval)
	}
	home, pihole := config.Apps[0], config.Apps[1]
	if home.Path != "/" || home.Health != "/" || home.Icon != "temperature" {
		t.Errorf("HomeMonitor = %+v", home)
	}
	if pihole.Path != "/admin" || pihole.Health != "/admin" || pihole.Icon != "app-window" {
		t.Errorf("Pi-hole = %+v, want its health check to follow its path", pihole)
	}
}

func TestParseReportsEveryProblem(t *testing.T) {
	_, err := Parse([]byte(`
apps:
  - port: 8000
  - name: Bad
    port: 70000
    icon: "Not An Icon"
    host: http://example
`))
	if err == nil {
		t.Fatal("expected validation errors")
	}
	for _, want := range []string{"has no name", "between 1 and 65535", "invalid icon", "host must be"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q is missing %q", err, want)
		}
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	if _, err := Parse([]byte("apps:\n  - name: A\n    port: 1\n    prot: 2\n")); err == nil {
		t.Error("a misspelled field should be rejected")
	}
}

func TestIntervalHasAFloor(t *testing.T) {
	config, err := Parse([]byte("interval: 100ms\n"))
	if err != nil || config.Interval != 2*time.Second {
		t.Errorf("interval = %v, %v; want 2s", config.Interval, err)
	}
}
