package main

import (
	"os/exec"
	"runtime"
)

// openPanel opens the local panel URL as a chromeless "app" window (Edge/Chrome
// --app) so it looks like a native window; falls back to the default browser.
func openPanel(url string) {
	for _, c := range panelCommands(url) {
		if c.Start() == nil {
			return
		}
	}
}

func panelCommands(url string) []*exec.Cmd {
	switch runtime.GOOS {
	case "windows":
		edge := `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`
		return []*exec.Cmd{
			exec.Command("msedge", "--app="+url),
			exec.Command(edge, "--app="+url),
			exec.Command("chrome", "--app="+url),
			exec.Command("cmd", "/c", "start", "", url),
		}
	case "darwin":
		return []*exec.Cmd{
			exec.Command("open", "-na", "Google Chrome", "--args", "--app="+url),
			exec.Command("open", "-na", "Microsoft Edge", "--args", "--app="+url),
			exec.Command("open", url),
		}
	default: // linux
		return []*exec.Cmd{
			exec.Command("google-chrome", "--app="+url),
			exec.Command("chromium", "--app="+url),
			exec.Command("microsoft-edge", "--app="+url),
			exec.Command("xdg-open", url),
		}
	}
}
