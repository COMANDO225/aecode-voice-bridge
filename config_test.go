package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Actualizar el ejecutable tiene que mover el puente al servidor nuevo. El caso real:
// la laptop tiene un config.json escrito por una versión anterior, cuando el valor de
// fábrica era localhost y la clave iba vacía. Si ese fichero gana, el operador ve el
// panel normal, enciende el envío, y el audio no sale de la máquina — sin un solo error.
func TestConfig_ElEjecutableNuevoManda_SobreUnConfigViejo(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "aecode-voice-bridge"), 0o755); err != nil {
		t.Fatal(err)
	}
	viejo := `{"url":"ws://localhost:8787/ingest","event":"","room":"sala-b",` +
		`"key":"","roomsUrl":"","device":"mi-micro","autostart":true}`
	if err := os.WriteFile(filepath.Join(dir, "aecode-voice-bridge", "config.json"), []byte(viejo), 0o644); err != nil {
		t.Fatal(err)
	}

	bakedURL, bakedEvent, bakedKey, bakedRoomsURL = "ws://prod:8787/ingest", "coneic-cusco-2026", "k1", "https://api/rooms"
	defer func() { bakedURL, bakedEvent, bakedKey, bakedRoomsURL = "", "", "", "" }()

	c := loadConfig()

	if c.URL != "ws://prod:8787/ingest" || c.Event != "coneic-cusco-2026" || c.Key != "k1" || c.RoomsURL != "https://api/rooms" {
		t.Errorf("las coordenadas del servidor deben venir del ejecutable, salió %+v", c)
	}
	// Lo que eligió el operador en ESA laptop es suyo y sobrevive a la actualización.
	if c.Room != "sala-b" || c.Device != "mi-micro" || !c.Autostart {
		t.Errorf("sala/micrófono/autostart son del operador y no deben perderse, salió %+v", c)
	}
}

// Sin inyectar nada (compilación de desarrollo) el fichero manda, como siempre.
func TestConfig_SinInyectar_ElFicheroManda(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "aecode-voice-bridge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "aecode-voice-bridge", "config.json"),
		[]byte(`{"url":"ws://mi-pc:8787/ingest","event":"pruebas"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if c := loadConfig(); c.URL != "ws://mi-pc:8787/ingest" || c.Event != "pruebas" {
		t.Errorf("en desarrollo el config local debe mandar, salió %+v", c)
	}
}

// Primer arranque de un .exe recién descargado: sin fichero, todo sale de la compilación
// y la sala queda vacía a propósito (el puente no deja emitir hasta que se elige una).
func TestConfig_PrimerArranque_TraeElServidorPeroNoLaSala(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	bakedURL, bakedEvent, bakedKey = "ws://prod:8787/ingest", "coneic-cusco-2026", "k1"
	defer func() { bakedURL, bakedEvent, bakedKey = "", "", "" }()

	c := loadConfig()
	if c.URL != "ws://prod:8787/ingest" || c.Event != "coneic-cusco-2026" {
		t.Errorf("un .exe recién bajado debe apuntar a producción solo, salió %+v", c)
	}
	if c.Room != "" {
		t.Errorf("la sala debe quedar vacía para que nadie entre a una ajena, salió %q", c.Room)
	}
}
