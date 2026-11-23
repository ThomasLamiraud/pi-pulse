// Package config loads the gate's YAML configuration: the apps to list.
package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	defaultPort     = 80
	defaultInterval = 10 * time.Second
	defaultIcon     = "app-window"
	minimumInterval = 2 * time.Second
)

var iconName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Config is the whole configuration file.
type Config struct {
	// Title replaces the hostname in the page header when set.
	Title string `yaml:"title"`
	// Port is the HTTP port the gate listens on.
	Port int `yaml:"port"`
	// Interval is how often apps are checked.
	Interval time.Duration `yaml:"interval"`
	Apps     []App         `yaml:"apps"`
}

// App is one launcher card.
type App struct {
	Name string `yaml:"name"`
	// Icon is a Tabler icon name, such as "temperature" or "activity".
	Icon string `yaml:"icon"`
	// Host is where the app runs. Empty means this Pi. Use an IP address for
	// other machines: the gate cannot resolve .local names.
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
	// Path is appended to the link, for example /admin for Pi-hole.
	Path string `yaml:"path"`
	// Health is the path requested to check the app is up.
	Health string `yaml:"health"`
}

// Load reads and validates the configuration file at path.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	return Parse(data)
}

// Parse validates YAML configuration and fills in defaults.
func Parse(data []byte) (Config, error) {
	var config Config
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	if config.Port == 0 {
		config.Port = defaultPort
	}
	if config.Interval == 0 {
		config.Interval = defaultInterval
	}
	config.Interval = max(config.Interval, minimumInterval)

	var problems []error
	if config.Port < 1 || config.Port > 65535 {
		problems = append(problems, fmt.Errorf("port %d is out of range", config.Port))
	}
	for index := range config.Apps {
		app := &config.Apps[index]
		label := fmt.Sprintf("app %d", index+1)
		if app.Name = strings.TrimSpace(app.Name); app.Name == "" {
			problems = append(problems, fmt.Errorf("%s has no name", label))
		} else {
			label = fmt.Sprintf("app %q", app.Name)
		}
		if app.Port < 1 || app.Port > 65535 {
			problems = append(problems, fmt.Errorf("%s needs a port between 1 and 65535", label))
		}
		if app.Icon == "" {
			app.Icon = defaultIcon
		} else if !iconName.MatchString(app.Icon) {
			problems = append(problems, fmt.Errorf("%s has an invalid icon name %q", label, app.Icon))
		}
		if strings.ContainsAny(app.Host, "/:@ ") {
			problems = append(problems, fmt.Errorf("%s host must be a hostname or IPv4 address, not %q", label, app.Host))
		}
		app.Path = normalizePath(app.Path)
		app.Health = normalizePath(app.Health)
		if app.Health == "/" && app.Path != "/" {
			app.Health = app.Path
		}
	}
	if len(problems) > 0 {
		return Config{}, errors.Join(problems...)
	}
	return config, nil
}

func normalizePath(path string) string {
	path = strings.TrimSpace(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}
