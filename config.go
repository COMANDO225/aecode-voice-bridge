package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config is the bridge's persisted settings. The URL is set once (by whoever
// prepares the laptop); the operator only picks the microphone from the tray.
type Config struct {
	URL       string `json:"url"`   // ingest base, e.g. wss://host/ingest
	Event     string `json:"event"` // event id (default namespace)
	Room      string `json:"room"`
	Device    string `json:"device"`    // input device name substring; "" = system default
	Autostart bool   `json:"autostart"` // start with the OS (Windows)
}

func defaultConfig() Config {
	return Config{
		URL:   "ws://localhost:8787/ingest",
		Event: "summit-2026",
		Room:  "main",
	}
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "aecode-voice-bridge", "config.json"), nil
}

// loadConfig reads the config, writing a starter file on first run.
func loadConfig() Config {
	c := defaultConfig()
	p, err := configPath()
	if err != nil {
		return c
	}
	b, err := os.ReadFile(p)
	if err != nil {
		_ = saveConfig(c)
		return c
	}
	_ = json.Unmarshal(b, &c)
	return c
}

func saveConfig(c Config) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(p, b, 0o644)
}
