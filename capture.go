package main

import (
	"strings"
	"sync"

	"github.com/gen2brain/malgo"
)

// source is one audio backend feeding the sink. Exactly one is active at a time;
// switching source stops the old one and starts the new one against the SAME sink,
// so the frames channel (and the uplink connection) survive the switch.
//
// Implementations: deviceSource (malgo mic / whole-output loopback) and procSource
// (per-program capture — Windows process loopback; a no-op stub elsewhere).
type source interface {
	start(sink *frameSink) error
	stop()
}

// programChoice is one entry of the "capture a specific program" picker.
type programChoice struct {
	Value string `json:"value"` // stable match persisted in config (exe name)
	Label string `json:"label"` // shown to the operator (e.g. "Zoom")
}

// capture is the audio engine: it owns the miniaudio context (for device
// enumeration and the malgo backends) and the single frameSink, and swaps the
// active source on demand. Everything downstream reads the sink, never the source.
type capture struct {
	ctx  *malgo.AllocatedContext
	sink *frameSink

	mu     sync.Mutex
	active source
}

func newCapture() (*capture, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(string) {})
	if err != nil {
		return nil, err
	}
	return &capture{ctx: ctx, sink: newFrameSink()}, nil
}

func (c *capture) level() float64      { return c.sink.level() }
func (c *capture) waveform() []float32 { return c.sink.waveform() }
func (c *capture) bands() []float32    { return c.sink.bands() }

// setActive stops whatever is capturing and starts src against the shared sink.
// On error the engine is left stopped (src did not start).
func (c *capture) setActive(src source) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active != nil {
		c.active.stop()
		c.active = nil
	}
	if err := src.start(c.sink); err != nil {
		return err
	}
	c.active = src
	return nil
}

func (c *capture) close() {
	c.mu.Lock()
	if c.active != nil {
		c.active.stop()
		c.active = nil
	}
	c.mu.Unlock()
	_ = c.ctx.Uninit()
	c.ctx.Free()
}

func (c *capture) inputDevices() []string  { return c.deviceNames(malgo.Capture) }
func (c *capture) outputDevices() []string { return c.deviceNames(malgo.Playback) }

func (c *capture) deviceNames(kind malgo.DeviceType) []string {
	infos, err := c.ctx.Devices(kind)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(infos))
	for _, d := range infos {
		names = append(names, d.Name())
	}
	return names
}

// programs lists the apps currently producing sound (Windows only; empty elsewhere).
func (c *capture) programs() []programChoice { return listPrograms() }

// deviceSource captures a miniaudio device: a mic/USB input (loopback=false) or a
// whole OUTPUT endpoint via WASAPI loopback (loopback=true) — "everything playing
// on that speaker". For a single program use procSource instead.
type deviceSource struct {
	ctx      *malgo.AllocatedContext
	loopback bool
	match    string
	dev      *malgo.Device
}

func (d *deviceSource) start(sink *frameSink) error {
	dt := malgo.Capture
	if d.loopback {
		dt = malgo.Loopback // WASAPI (Windows): capta la SALIDA — "lo que suena"
	}
	cfg := malgo.DefaultDeviceConfig(dt)
	cfg.Capture.Format = malgo.FormatS16
	cfg.Capture.Channels = 1
	cfg.SampleRate = 16000
	cfg.Alsa.NoMMap = 1
	if d.match != "" {
		if id, ok := findDeviceID(d.ctx, d.loopback, d.match); ok {
			cfg.Capture.DeviceID = id.Pointer()
		}
	}
	onFrames := func(_, in []byte, _ uint32) { sink.push(in) }
	dev, err := malgo.InitDevice(d.ctx.Context, cfg, malgo.DeviceCallbacks{Data: onFrames})
	if err != nil {
		return err
	}
	if err := dev.Start(); err != nil {
		dev.Uninit()
		return err
	}
	d.dev = dev
	return nil
}

func (d *deviceSource) stop() {
	if d.dev != nil {
		d.dev.Uninit()
		d.dev = nil
	}
}

// findDeviceID resolves a device name substring to its ID. loopback picks among
// OUTPUT (Playback) devices, otherwise among INPUT (Capture) devices.
func findDeviceID(ctx *malgo.AllocatedContext, loopback bool, match string) (malgo.DeviceID, bool) {
	kind := malgo.Capture
	if loopback {
		kind = malgo.Playback // loopback = capturar una SALIDA
	}
	infos, err := ctx.Devices(kind)
	if err != nil {
		return malgo.DeviceID{}, false
	}
	m := strings.ToLower(match)
	for i := range infos {
		if strings.Contains(strings.ToLower(infos[i].Name()), m) {
			return infos[i].ID, true
		}
	}
	return malgo.DeviceID{}, false
}
