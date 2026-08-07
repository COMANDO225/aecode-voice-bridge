package main

import (
	"math"
	"sync"
	"sync/atomic"
)

// wfLen is how many peak points the live waveform ring holds (~2s of history).
const wfLen = 256

// frameSink is the single consumer-facing side of capture: whatever the active
// source produces (mic, system output, a specific program, or —later— PCM pushed
// in over the local /tap socket) is fed here as s16le/16k/mono and shows up
// identically in the level meter, the live waveform and the frames channel the
// uplink drains. Sources are swappable; this is not — so switching source never
// makes the uplink reconnect.
type frameSink struct {
	frames  chan []byte
	sent    atomic.Uint64
	dropped atomic.Uint64
	lvlBits atomic.Uint64 // last frame RMS as float64 bits

	wfMu sync.Mutex
	wf   [wfLen]float32 // ring of recent peak amplitudes (for the live waveform)
	wfN  int            // next write index
}

func newFrameSink() *frameSink { return &frameSink{frames: make(chan []byte, 256)} }

// push feeds one PCM s16le mono frame from the active source. Non-blocking: if the
// uplink falls behind, the frame is dropped to stay real-time. The frame is copied
// before being queued, so callers may reuse their buffer.
func (s *frameSink) push(pcm []byte) {
	s.lvlBits.Store(math.Float64bits(rms(pcm)))
	s.pushWaveform(pcm)
	buf := make([]byte, len(pcm))
	copy(buf, pcm)
	select {
	case s.frames <- buf:
		s.sent.Add(1)
	default:
		s.dropped.Add(1) // uplink behind → drop, stay real-time
	}
}

func (s *frameSink) level() float64 { return math.Float64frombits(s.lvlBits.Load()) }

// pushWaveform appends peak amplitudes (one per ~128 samples ≈ 8 ms) to the ring,
// giving the panel a scrolling oscilloscope of the live input.
func (s *frameSink) pushWaveform(b []byte) {
	const step = 128 // samples per peak
	s.wfMu.Lock()
	for i := 0; i+1 < len(b); {
		var peak float32
		end := i + step*2
		for ; i+1 < len(b) && i < end; i += 2 {
			v := float32(int16(uint16(b[i])|uint16(b[i+1])<<8)) / 32768
			if v < 0 {
				v = -v
			}
			if v > peak {
				peak = v
			}
		}
		s.wf[s.wfN%wfLen] = peak
		s.wfN++
	}
	s.wfMu.Unlock()
}

// waveform returns the ring in chronological order (oldest → newest).
func (s *frameSink) waveform() []float32 {
	out := make([]float32, wfLen)
	s.wfMu.Lock()
	for i := 0; i < wfLen; i++ {
		out[i] = s.wf[(s.wfN+i)%wfLen]
	}
	s.wfMu.Unlock()
	return out
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
