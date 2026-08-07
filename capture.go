package main

import (
	"math"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gen2brain/malgo"
)

// wfLen is how many peak points the live waveform ring holds (~2s of history).
const wfLen = 256

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

	wfMu sync.Mutex
	wf   [wfLen]float32 // ring of recent peak amplitudes (for the live waveform)
	wfN  int            // next write index
}

func newCapture() (*capture, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(string) {})
	if err != nil {
		return nil, err
	}
	return &capture{ctx: ctx, frames: make(chan []byte, 256)}, nil
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

// start opens the source and begins capturing into c.frames. loopback=true captures
// the OUTPUT (whatever is playing: browser, Zoom, media…) via WASAPI loopback on
// Windows; false captures an input device (mic/USB). Any previous device is stopped.
func (c *capture) start(loopback bool, match string) error {
	c.stop()
	dt := malgo.Capture
	if loopback {
		dt = malgo.Loopback // WASAPI (Windows): capta la SALIDA — "lo que suena"
	}
	cfg := malgo.DefaultDeviceConfig(dt)
	cfg.Capture.Format = malgo.FormatS16
	cfg.Capture.Channels = 1
	cfg.SampleRate = 16000
	cfg.Alsa.NoMMap = 1
	if match != "" {
		if id, ok := c.findID(loopback, match); ok {
			cfg.Capture.DeviceID = id.Pointer()
		}
	}
	onFrames := func(_, in []byte, _ uint32) {
		c.lvlBits.Store(math.Float64bits(rms(in)))
		c.pushWaveform(in)
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

// pushWaveform appends peak amplitudes (one per ~128 samples ≈ 8 ms) to the ring,
// giving the panel a scrolling oscilloscope of the live input.
func (c *capture) pushWaveform(b []byte) {
	const step = 128 // samples per peak
	c.wfMu.Lock()
	for i := 0; i+1 < len(b); {
		var peak float32
		end := i + step*2
		for ; i+1 < len(b) && i < end; i += 2 {
			s := float32(int16(uint16(b[i])|uint16(b[i+1])<<8)) / 32768
			if s < 0 {
				s = -s
			}
			if s > peak {
				peak = s
			}
		}
		c.wf[c.wfN%wfLen] = peak
		c.wfN++
	}
	c.wfMu.Unlock()
}

// waveform returns the ring in chronological order (oldest → newest).
func (c *capture) waveform() []float32 {
	out := make([]float32, wfLen)
	c.wfMu.Lock()
	for i := 0; i < wfLen; i++ {
		out[i] = c.wf[(c.wfN+i)%wfLen]
	}
	c.wfMu.Unlock()
	return out
}

func (c *capture) findID(loopback bool, match string) (malgo.DeviceID, bool) {
	kind := malgo.Capture
	if loopback {
		kind = malgo.Playback // loopback = capturar una SALIDA
	}
	infos, err := c.ctx.Devices(kind)
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
