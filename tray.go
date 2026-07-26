package main

import (
	"context"
	"log"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"fyne.io/systray"
)

// runTray shows the background system-tray app (like Steam): an icon with a menu
// to pick the microphone, see status/level, toggle autostart, and quit. It keeps
// running when minimized/idle because capture + uplink are plain goroutines.
func runTray(ctx context.Context, cancel context.CancelFunc, cfg Config, cap *capture, up *uplink) {
	onReady := func() {
		systray.SetIcon(iconData)
		systray.SetTitle("")
		systray.SetTooltip("AECODE Voz — puente de audio")

		mStatus := systray.AddMenuItem("Estado: …", "")
		mStatus.Disable()
		mLevel := systray.AddMenuItem("Nivel: …", "")
		mLevel.Disable()
		systray.AddSeparator()

		mMic := systray.AddMenuItem("Micrófono", "Elegí el dispositivo de entrada")
		items := map[string]*systray.MenuItem{}
		for _, name := range cap.inputDevices() {
			it := mMic.AddSubMenuItemCheckbox(name, "", name == cfg.Device)
			items[name] = it
			go func(n string, mi *systray.MenuItem) {
				for range mi.ClickedCh {
					cfg.Device = n
					_ = saveConfig(cfg)
					if err := cap.start(n); err != nil {
						log.Printf("no pude abrir %q: %v", n, err)
					}
					for other, oi := range items {
						if other == n {
							oi.Check()
						} else {
							oi.Uncheck()
						}
					}
				}
			}(name, it)
		}

		systray.AddSeparator()
		mAuto := systray.AddMenuItemCheckbox("Iniciar con el sistema", "", cfg.Autostart)
		mOpen := systray.AddMenuItem("Abrir carpeta de configuración", "editar la URL del servidor")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Salir", "")

		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-mAuto.ClickedCh:
					cfg.Autostart = !cfg.Autostart
					if cfg.Autostart {
						if err := enableAutostart(); err != nil {
							log.Printf("autostart on: %v", err)
						}
						mAuto.Check()
					} else {
						if err := disableAutostart(); err != nil {
							log.Printf("autostart off: %v", err)
						}
						mAuto.Uncheck()
					}
					_ = saveConfig(cfg)
				case <-mOpen.ClickedCh:
					openConfigDir()
				case <-mQuit.ClickedCh:
					systray.Quit()
					return
				}
			}
		}()

		go func() {
			t := time.NewTicker(time.Second)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					if up.status() == statusConnected {
						mStatus.SetTitle("Estado: Conectado ✓")
					} else {
						mStatus.SetTitle("Estado: Reconectando…")
					}
					mLevel.SetTitle("Nivel: " + bars(cap.level()))
				}
			}
		}()
	}
	systray.Run(onReady, func() { cancel() })
}

// bars renders an RMS level (0..~0.1) as a single block char.
func bars(r float64) string {
	blocks := []rune(" ▁▂▃▄▅▆▇█")
	i := int(r*80) + 1
	if i < 1 {
		i = 1
	}
	if i >= len(blocks) {
		i = len(blocks) - 1
	}
	return string(blocks[i])
}

func openConfigDir() {
	p, err := configPath()
	if err != nil {
		return
	}
	dir := filepath.Dir(p)
	var c *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		c = exec.Command("explorer", dir)
	case "darwin":
		c = exec.Command("open", dir)
	default:
		c = exec.Command("xdg-open", dir)
	}
	_ = c.Start()
}
