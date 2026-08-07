// Command aecode-voice-bridge is the native, headless equivalent of the /speak
// web page: it captures audio from a chosen input device (a USB sound card fed by
// the event's UHF receiver) and streams it as raw PCM s16le 16 kHz mono to the
// cloud /ingest WebSocket. It runs in the system tray with a control panel
// (live waveform + a switch to start/stop sending). Unlike a browser tab it is NOT
// throttled when minimized or idle, and auto-reconnects.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

func main() {
	console := flag.Bool("console", false, "run headless (no tray/panel window), logging to stdout")
	list := flag.Bool("list", false, "list input devices and exit")
	send := flag.Bool("send", false, "console: start sending immediately (for testing)")
	urlFlag := flag.String("url", "", "override the ingest URL (else config.json)")
	deviceFlag := flag.String("device", "", "override the input device substring (else config.json)")
	flag.Parse()

	cap, err := newCapture()
	if err != nil {
		log.Fatalf("audio init: %v", err)
	}
	defer cap.close()

	if *list {
		fmt.Println("Dispositivos de ENTRADA (micrófono / línea):")
		for _, n := range cap.inputDevices() {
			fmt.Printf("  • %s\n", n)
		}
		return
	}

	cfg := loadConfig()
	if *urlFlag != "" {
		cfg.URL = *urlFlag
	}
	if *deviceFlag != "" {
		cfg.Device = *deviceFlag
	}

	// Single source of truth for config; panel and tray mutate it through helpers.
	var cfgMu sync.Mutex

	// applySource arranca el backend de captura que pide la config: mic/entrada,
	// salida entera (loopback) o un programa concreto. Si "program" no está
	// disponible (no-Windows, la app no está viva, o falla la activación) cae a la
	// salida entera para que el panel siga mostrando sonido en vez de quedar mudo.
	applySource := func(c Config) {
		var err error
		switch c.Source {
		case "system":
			err = cap.setActive(&deviceSource{ctx: cap.ctx, loopback: true, match: c.Device})
		case "program":
			if err = cap.setActive(&procSource{match: c.Program}); err != nil {
				log.Printf("captura por programa no disponible (%v) — cae a audio del sistema", err)
				err = cap.setActive(&deviceSource{ctx: cap.ctx, loopback: true})
			}
		default: // "mic"
			err = cap.setActive(&deviceSource{ctx: cap.ctx, loopback: false, match: c.Device})
		}
		if err != nil {
			log.Printf("no pude abrir la fuente %q: %v — elegí otra en el panel", c.Source, err)
		}
	}

	getDevice := func() string { cfgMu.Lock(); defer cfgMu.Unlock(); return cfg.Device }
	setDevice := func(name string) {
		cfgMu.Lock()
		cfg.Device = name
		c := cfg
		cfgMu.Unlock()
		applySource(c)
		_ = saveConfig(c)
	}
	getRoom := func() string { cfgMu.Lock(); defer cfgMu.Unlock(); return cfg.Room }
	getRooms := func() []RoomChoice { cfgMu.Lock(); defer cfgMu.Unlock(); return cfg.Rooms }
	getSource := func() string {
		cfgMu.Lock()
		defer cfgMu.Unlock()
		switch cfg.Source {
		case "system", "program":
			return cfg.Source
		}
		return "mic"
	}
	// setSource cambia entre micrófono, "audio del sistema" (salida entera) y "un
	// programa". Resetea el dispositivo porque una entrada no aplica a una salida; el
	// programa elegido se conserva.
	setSource := func(src string) {
		switch src {
		case "system", "program":
		default:
			src = "mic"
		}
		cfgMu.Lock()
		cfg.Source = src
		cfg.Device = ""
		c := cfg
		cfgMu.Unlock()
		applySource(c)
		_ = saveConfig(c)
	}
	getProgram := func() string { cfgMu.Lock(); defer cfgMu.Unlock(); return cfg.Program }
	getPrograms := func() []programChoice { return cap.programs() }
	// setProgram elige la app a capturar (y pasa la fuente a "program").
	setProgram := func(match string) {
		cfgMu.Lock()
		cfg.Program = match
		cfg.Source = "program"
		c := cfg
		cfgMu.Unlock()
		applySource(c)
		_ = saveConfig(c)
	}
	setAutostart := func(b bool) {
		cfgMu.Lock()
		cfg.Autostart = b
		c := cfg
		cfgMu.Unlock()
		_ = saveConfig(c)
	}

	applySource(cfg) // capture always runs; sending is separate

	rec := newRecorder()
	defer rec.close()
	up := newUplink(cap.sink.frames, rec)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	up.setTarget(cfg.URL, cfg.Event, cfg.Room, cfg.Key)
	go up.run(ctx) // won't send until the switch is ON *and* a room is picked

	// setRoom es la operación del día del evento: elegir sala desde el panel, sin
	// editar archivos ni reiniciar. Apagar el envío al cambiar es deliberado — cambiar
	// de sala a mitad de una ponencia siempre es un error, y el operador debe volver a
	// pulsar el switch a conciencia.
	setRoom := func(slug string) {
		cfgMu.Lock()
		cfg.Room = slug
		c := cfg
		cfgMu.Unlock()
		up.setEnabled(false)
		up.setTarget(c.URL, c.Event, slug, c.Key)
		_ = saveConfig(c)
		log.Printf("sala = %q", slug)
	}

	feed := newRoomsFeed(cfg.RoomsURL, func(rs []RoomChoice) {
		cfgMu.Lock()
		cfg.Rooms = rs
		c := cfg
		cfgMu.Unlock()
		_ = saveConfig(c) // cachear en disco: el día del evento puede no haber backend
	})
	go feed.run(ctx)

	if *send {
		if cfg.Room == "" {
			log.Print("-send ignorado: no hay sala elegida (elígela en el panel)")
		} else {
			up.setEnabled(true)
		}
	}

	panelURL, err := startPanel(cap, up, feed, getDevice, setDevice, getRoom, getRooms, setRoom, getSource, setSource, getProgram, setProgram, getPrograms)
	if err != nil {
		log.Printf("panel: %v", err)
	}
	log.Printf("bridge: panel=%s · sala=%q · device=%q", panelURL, cfg.Room, cfg.Device)

	if *console {
		runConsole(cap, up)
		return
	}

	logToFile() // tray build has no console; keep a log next to config
	if panelURL != "" {
		openPanel(panelURL) // show the panel on launch
	}
	runTray(ctx, cancel, panelURL, cfg.Autostart, setAutostart)
}

// runConsole loops printing status + level until Ctrl+C (headless / test mode).
func runConsole(cap *capture, up *uplink) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-sig:
			return
		case <-t.C:
			log.Printf("%s · nivel %.3f · frames %d · descartados %d", up.statusStr(), cap.level(), cap.sink.sent.Load(), cap.sink.dropped.Load())
		}
	}
}

// logToFile redirects the log to <configdir>/bridge.log for the background tray app.
func logToFile() {
	p, err := configPath()
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(filepath.Dir(p), "bridge.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	log.SetOutput(f)
}
