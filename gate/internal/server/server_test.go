package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gate/internal/monitor"
	"gate/web"
)

type fakeSource struct{ status monitor.Status }

func (f fakeSource) Status() monitor.Status { return f.status }

func TestServesPageAssetsAndStatus(t *testing.T) {
	source := fakeSource{monitor.Status{
		Title: "homemonitor",
		Apps:  []monitor.AppStatus{{Name: "PiPulse", Icon: "activity", Port: 3000, Path: "/", Status: "up"}},
	}}
	server := httptest.NewServer(Handler(source, web.Assets))
	defer server.Close()

	for path, want := range map[string]string{"/": "gate.js", "/static/gate.js": "/api/status", "/static/gate.css": "--background", "/up": "ok"} {
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusOK || !strings.Contains(string(body), want) {
			t.Errorf("GET %s = %d, body missing %q", path, response.StatusCode, want)
		}
	}

	response, err := http.Get(server.URL + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var status monitor.Status
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status.Title != "homemonitor" || len(status.Apps) != 1 || status.Apps[0].Status != "up" {
		t.Errorf("status = %+v", status)
	}
}

func TestUnknownPathsAreNotFound(t *testing.T) {
	server := httptest.NewServer(Handler(fakeSource{}, web.Assets))
	defer server.Close()
	response, err := http.Get(server.URL + "/nope")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Errorf("GET /nope = %d, want 404", response.StatusCode)
	}
}
