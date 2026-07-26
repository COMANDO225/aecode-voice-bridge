package main

import (
	"math"
	"strings"
	"sync/atomic"

	"github.com/gen2brain/malgo"
)

// capture reads audio from an input device at 16 kHz mono s16le (miniaudio
// resamples from the hardware format internally) and pushes frames to a channel.
// The device can be switched at runtime without dropping the frames channel, so
// the uplink never has to reconnect just because the mic changed.
type capture struct {
	ctx     *malgo.AllocatedContext
	dev     *malgo.Device
	frames  chan []byte
	sent    atomic.Uint64
	dropped atomic.Uint64
	lvlBits atomic.Uint64 // last frame RMS as float64 bits
}

func newCapture() (*capture, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(string) {})
	if err != nil {
		return nil, err
	}
	return &capture{ctx: ctx, frames: make(chan []byte, 256)}, nil
}

func (c *capture) inputDevices() []string {
	infos, err := c.ctx.Devices(malgo.Capture)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(infos))
	for _, d := range infos {
		names = append(names, d.Name())
	}
	return names
}

// start opens the input device whose name contains match (default input if empty)
// and begins capturing into c.frames. Any previously open device is stopped first.
func (c *capture) start(match string) error {
	c.stop()
	cfg := malgo.DefaultDeviceConfig(malgo.Capture)
	cfg.Capture.Format = malgo.FormatS16
	cfg.Capture.Channels = 1
	cfg.SampleRate = 16000
	cfg.Alsa.NoMMap = 1
	if match != "" {
		if id, ok := c.findID(match); ok {
			cfg.Capture.DeviceID = id.Pointer()
		}
	}
	onFrames := func(_, in []byte, _ uint32) {
		c.lvlBits.Store(math.Float64bits(rms(in)))
		buf := make([]byte, len(in))
		copy(buf, in)
		select {
		case c.frames <- buf:
			c.sent.Add(1)
		default:
			c.dropped.Add(1) // uplink behind → drop, stay real-time
		}
	}
	dev, err := malgo.InitDevice(c.ctx.Context, cfg, malgo.DeviceCallbacks{Data: onFrames})
	if err != nil {
		return err
	}
	if err := dev.Start(); err != nil {
		dev.Uninit()
		return err
	}
	c.dev = dev
	return nil
}

func (c *capture) stop() {
	if c.dev != nil {
		c.dev.Uninit()
		c.dev = nil
	}
}

func (c *capture) level() float64 { return math.Float64frombits(c.lvlBits.Load()) }

func (c *capture) findID(match string) (malgo.DeviceID, bool) {
	infos, err := c.ctx.Devices(malgo.Capture)
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

func (c *capture) close() {
	c.stop()
	_ = c.ctx.Uninit()
	c.ctx.Free()
}

// rms is the normalized (0..1) RMS of a PCM s16le mono frame — the level meter.
func rms(b []byte) float64 {
	if len(b) < 2 {
		return 0
	}
	var sum float64
	n := len(b) / 2
	for i := 0; i+1 < len(b); i += 2 {
		s := float64(int16(uint16(b[i])|uint16(b[i+1])<<8)) / 32768
		sum += s * s
	}
	return math.Sqrt(sum / float64(n))
}
