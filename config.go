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

// Valores grabados al compilar con -ldflags "-X main.bakedURL=... -X main.bakedEvent=...".
// Sin ellos el ejecutable descargado apuntaría a localhost, sin clave y sin lista de
// salas: se abriría con el desplegable vacío y nada explicaría por qué. Quien prepara
// la laptop no debería tener que escribir un JSON a mano para que funcione.
//
// bakedKey viaja DENTRO del ejecutable, así que solo frena la emisión accidental —un
// puente olvidado con autostart— no a quien de verdad quiera inyectar audio. Si el
// enlace de descarga es público, la clave es pública: el control real es quién puede
// bajarlo.
var (
	bakedURL      string
	bakedEvent    string
	bakedKey      string
	bakedRoomsURL string
)

// defaultConfig deja Room VACÍA a propósito.
//
// Antes traía "summit-2026"/"main" de fábrica, y esa era la línea más peligrosa del
// sistema: una laptop sin configurar entraba a una sala real y se mezclaba con quien
// estuviera transmitiendo. Sin sala elegida el puente no deja ni encender el envío.
// El evento sí viene puesto: es el mismo para todas las laptops del congreso, y
// equivocarlo no mezcla a nadie — simplemente no llega el audio a ninguna parte.
func defaultConfig() Config {
	c := Config{URL: bakedURL, Event: bakedEvent, Key: bakedKey, RoomsURL: bakedRoomsURL}
	if c.URL == "" {
		c.URL = "ws://localhost:8787/ingest"
	}
	return c
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
	// Las coordenadas del servidor son identidad del despliegue, no preferencia del
	// operador: quien actualiza el ejecutable espera que apunte a donde apunta esa
	// versión. Sin esto, un config.json de una versión anterior —escrito cuando el
	// valor de fábrica era localhost y la clave estaba vacía— seguiría mandando tras
	// actualizar, y el puente se quedaría emitiendo al vacío sin decir nada.
	// La sala, el micrófono y el autostart SÍ son del operador y no se tocan.
	if bakedURL != "" {
		c.URL, c.Event, c.Key, c.RoomsURL = bakedURL, bakedEvent, bakedKey, bakedRoomsURL
	}
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
