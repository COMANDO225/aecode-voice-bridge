package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config is the bridge's persisted settings. The URL is set once (by whoever
// prepares the laptop); the operator only picks the microphone from the tray.
type Config struct {
	URL   string `json:"url"`   // ingest base, e.g. wss://host/ingest
	Event string `json:"event"` // slug del evento
	Room  string `json:"room"`  // slug de la sala: lo elige el operador en el panel
	Key   string `json:"key"`   // clave compartida del /ingest
	// RoomsURL: de dónde se descarga la lista de salas del evento.
	RoomsURL string `json:"roomsUrl"`
	// Rooms: última lista buena, cacheada. Si el servicio de eventos no responde el
	// día del congreso, el operador sigue teniendo su desplegable — la traducción
	// NUNCA debe depender de que el backend de eventos esté vivo.
	Rooms     []RoomChoice `json:"rooms"`
	Device    string       `json:"device"`    // input device name substring; "" = system default
	Autostart bool         `json:"autostart"` // start with the OS (Windows)
}

// RoomChoice es una sala del desplegable.
type RoomChoice struct {
	Slug  string `json:"slug"`
	Label string `json:"label"`
}

// defaultConfig deja Event y Room VACÍOS a propósito.
//
// Antes traía "summit-2026"/"main" de fábrica, y esa era la línea más peligrosa del
// sistema: una laptop sin configurar entraba a una sala real y se mezclaba con quien
// estuviera transmitiendo. Sin sala elegida el puente no deja ni encender el envío.
func defaultConfig() Config {
	return Config{URL: "ws://localhost:8787/ingest"}
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
