//go:build windows

package main

import (
	"os"
	"os/exec"
)

const runKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
const autostartName = "AECODEVoiceBridge"

// enableAutostart registers the exe to launch at login (per-user Run key).
func enableAutostart() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return exec.Command("reg", "add", runKey, "/v", autostartName, "/t", "REG_SZ", "/d", exe, "/f").Run()
}

func disableAutostart() error {
	return exec.Command("reg", "delete", runKey, "/v", autostartName, "/f").Run()
}
