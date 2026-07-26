//go:build !windows

package main

// Autostart is Windows-only for now (see plan follow-ups for macOS/Linux).
func enableAutostart() error  { return nil }
func disableAutostart() error { return nil }
