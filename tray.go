package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"

	"fyne.io/systray"
)

// runTray shows the background tray icon. Its main action opens the panel; the app
// keeps running until "Salir". Closing the panel window never quits the app.
func runTray(ctx context.Context, cancel context.CancelFunc, panelURL string, autostart bool, setAutostart func(bool)) {
	onReady := func() {
		systray.SetIcon(iconData)
		systray.SetTitle("")
		systray.SetTooltip("AECODE Voz — puente de audio")

		mOpen := systray.AddMenuItem("Abrir panel", "Ver la onda de sonido y el switch de envío")
		systray.AddSeparator()
		mAuto := systray.AddMenuItemCheckbox("Iniciar con el sistema", "", autostart)
		mCfg := systray.AddMenuItem("Abrir carpeta de configuración", "editar la URL del servidor")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Salir", "cerrar el puente por completo")

		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-mOpen.ClickedCh:
					if panelURL != "" {
						openPanel(panelURL)
					}
				case <-mAuto.ClickedCh:
					if mAuto.Checked() {
						mAuto.Uncheck()
						setAutostart(false)
						_ = disableAutostart()
					} else {
						mAuto.Check()
						setAutostart(true)
						_ = enableAutostart()
					}
				case <-mCfg.ClickedCh:
					openConfigDir()
				case <-mQuit.ClickedCh:
					systray.Quit()
					return
				}
			}
		}()
	}
	systray.Run(onReady, func() { cancel() })
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
