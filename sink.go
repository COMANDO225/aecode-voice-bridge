package main

import (
	"math"
	"sync"
	"sync/atomic"
)

// wfLen is how many peak points the live waveform ring holds (~2s of history).
const wfLen = 256

// FFT para el visualizador de espectro del panel: ventana de fftN muestras → nBands
// bandas log. fftN debe ser potencia de 2 (radix-2). A 16 kHz, 1024 muestras = 64 ms.
const (
	fftN   = 1024
	nBands = 32
)

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

	spMu sync.Mutex
	sp   [fftN]float64 // ring de muestras crudas (-1..1) para el FFT del espectro
	spN  int           // next write index
}

func newFrameSink() *frameSink { return &frameSink{frames: make(chan []byte, 256)} }

// push feeds one PCM s16le mono frame from the active source. Non-blocking: if the
// uplink falls behind, the frame is dropped to stay real-time. The frame is copied
// before being queued, so callers may reuse their buffer.
func (s *frameSink) push(pcm []byte) {
	s.lvlBits.Store(math.Float64bits(rms(pcm)))
	s.pushWaveform(pcm)
	s.pushSamples(pcm)
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

// pushSamples guarda las muestras crudas (normalizadas a -1..1) en un ring, para que
// bands() calcule el espectro cuando el panel lo pide (~30 fps), sin FFT por frame.
func (s *frameSink) pushSamples(b []byte) {
	s.spMu.Lock()
	for i := 0; i+1 < len(b); i += 2 {
		s.sp[s.spN%fftN] = float64(int16(uint16(b[i])|uint16(b[i+1])<<8)) / 32768
		s.spN++
	}
	s.spMu.Unlock()
}

// bands devuelve nBands magnitudes (0..1) log-espaciadas del espectro de la ventana
// más reciente: Hann + FFT + agrupado en dB. Alimenta el visualizador reactor.
func (s *frameSink) bands() []float32 {
	re := make([]float64, fftN)
	im := make([]float64, fftN)
	s.spMu.Lock()
	for i := 0; i < fftN; i++ {
		re[i] = s.sp[(s.spN+i)%fftN] // orden cronológico (más viejo → más nuevo)
	}
	s.spMu.Unlock()

	for i := 0; i < fftN; i++ { // ventana de Hann (menos fuga espectral)
		re[i] *= 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(fftN-1))
	}
	fftRadix2(re, im)

	out := make([]float32, nBands)
	const binLo = 4.0             // ~62 Hz (por debajo casi no hay voz)
	binHi := float64(fftN / 2)    // Nyquist = 8 kHz
	for b := 0; b < nBands; b++ { // bandas espaciadas en log
		f0 := binLo * math.Pow(binHi/binLo, float64(b)/nBands)
		f1 := binLo * math.Pow(binHi/binLo, float64(b+1)/nBands)
		i0, i1 := int(f0), int(f1)
		if i1 <= i0 {
			i1 = i0 + 1
		}
		if i1 > fftN/2 {
			i1 = fftN / 2
		}
		var sum float64
		for i := i0; i < i1; i++ {
			sum += math.Hypot(re[i], im[i])
		}
		mag := sum / float64(i1-i0) / (float64(fftN) / 2) // promedio, normalizado
		db := 20 * math.Log10(mag+1e-9)
		v := (db + 70) / 70 // -70 dB → 0, 0 dB → 1
		if v < 0 {
			v = 0
		} else if v > 1 {
			v = 1
		}
		out[b] = float32(v)
	}
	return out
}

// fftRadix2 es una FFT iterativa Cooley-Tukey in-place (len potencia de 2).
func fftRadix2(re, im []float64) {
	n := len(re)
	for i, j := 1, 0; i < n; i++ { // bit-reversal
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			re[i], re[j] = re[j], re[i]
			im[i], im[j] = im[j], im[i]
		}
	}
	for length := 2; length <= n; length <<= 1 {
		ang := -2 * math.Pi / float64(length)
		wr, wi := math.Cos(ang), math.Sin(ang)
		for i := 0; i < n; i += length {
			cr, ci := 1.0, 0.0
			for k := 0; k < length/2; k++ {
				a, b := i+k, i+k+length/2
				tr := cr*re[b] - ci*im[b]
				ti := cr*im[b] + ci*re[b]
				re[b], im[b] = re[a]-tr, im[a]-ti
				re[a], im[a] = re[a]+tr, im[a]+ti
				cr, ci = cr*wr-ci*wi, cr*wi+ci*wr
			}
		}
	}
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
