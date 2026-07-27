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
	getDevice := func() string { cfgMu.Lock(); defer cfgMu.Unlock(); return cfg.Device }
	setDevice := func(name string) {
		cfgMu.Lock()
		cfg.Device = name
		c := cfg
		cfgMu.Unlock()
		if err := cap.start(name); err != nil {
			log.Printf("no pude abrir %q: %v", name, err)
		}
		_ = saveConfig(c)
	}
	setAutostart := func(b bool) {
		cfgMu.Lock()
		cfg.Autostart = b
		c := cfg
		cfgMu.Unlock()
		_ = saveConfig(c)
	}

	if err := cap.start(cfg.Device); err != nil { // capture always runs; sending is separate
		log.Printf("no pude abrir el micrófono (%v) — elegí otro en el panel", err)
	}

	up := newUplink(cap.frames)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go up.run(ctx, cfg.URL, cfg.Event, cfg.Room) // won't send until the switch is ON
	if *send {
		up.setEnabled(true)
	}

	panelURL, err := startPanel(cap, up, buildURL(cfg.URL, cfg.Event, cfg.Room), getDevice, setDevice)
	if err != nil {
		log.Printf("panel: %v", err)
	}
	log.Printf("bridge: panel=%s · server=%s · device=%q", panelURL, buildURL(cfg.URL, cfg.Event, cfg.Room), cfg.Device)

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
			log.Printf("%s · nivel %.3f · frames %d · descartados %d", up.statusStr(), cap.level(), cap.sent.Load(), cap.dropped.Load())
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
