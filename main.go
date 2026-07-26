// Command aecode-voice-bridge is the native, headless equivalent of the /speak
// web page: it captures audio from a chosen input device (a USB sound card fed by
// the event's UHF receiver) and streams it as raw PCM s16le 16 kHz mono to the
// cloud /ingest WebSocket. Unlike a browser tab it is NOT throttled when minimized
// or idle — it runs in the system tray, keeps capturing as long as the device is
// present, and auto-reconnects.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	console := flag.Bool("console", false, "run headless (no tray), logging to stdout")
	list := flag.Bool("list", false, "list input devices and exit")
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

	if err := cap.start(cfg.Device); err != nil {
		log.Printf("no pude abrir el micrófono (%v) — elegí otro en el menú", err)
	}

	up := newUplink(cap.frames)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go up.run(ctx, cfg.URL, cfg.Event, cfg.Room)
	log.Printf("bridge: capturando @16kHz → %s (device=%q)", buildURL(cfg.URL, cfg.Event, cfg.Room), cfg.Device)

	if *console {
		runConsole(cap, up)
		return
	}
	logToFile() // tray build has no console; keep a log next to config for debugging
	runTray(ctx, cancel, cfg, cap, up)
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
			st := "reconectando…"
			if up.status() == statusConnected {
				st = "conectado ✓"
			}
			log.Printf("%s · nivel %.3f · frames %d · descartados %d", st, cap.level(), cap.sent.Load(), cap.dropped.Load())
		}
	}
}

// logToFile redirects the log to <configdir>/bridge.log so the background tray app
// still leaves a trail to diagnose.
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
