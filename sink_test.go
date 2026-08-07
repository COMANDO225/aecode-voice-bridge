package main

import (
	"math"
	"testing"
)

// TestBandsPeak: una senoidal de 1 kHz a 16 kHz debe dar el pico del espectro en la
// banda ~18 (log-espaciada, bin 64 de 512). Verifica ventana+FFT+agrupado.
func TestBandsPeak(t *testing.T) {
	s := newFrameSink()
	buf := make([]byte, fftN*4) // ~2 ventanas de muestras s16le
	for i := 0; i < len(buf)/2; i++ {
		v := math.Sin(2 * math.Pi * 1000 * float64(i) / 16000)
		u := uint16(int16(v * 30000))
		buf[i*2] = byte(u)
		buf[i*2+1] = byte(u >> 8)
	}
	s.pushSamples(buf)

	b := s.bands()
	if len(b) != nBands {
		t.Fatalf("bands() dio %d bandas, esperaba %d", len(b), nBands)
	}
	mi := 0
	for i := range b {
		if b[i] > b[mi] {
			mi = i
		}
	}
	if mi < 15 || mi > 21 {
		t.Fatalf("pico en banda %d (val %.2f); 1 kHz debería caer ~banda 18", mi, b[mi])
	}
	if b[mi] < 0.5 {
		t.Fatalf("pico débil (%.2f) para un tono puros de 1 kHz", b[mi])
	}
}
