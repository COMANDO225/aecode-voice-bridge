//go:build !windows

package main

import "errors"

// Captura por programa: solo Windows (WASAPI process loopback). En Linux/Mac de
// desarrollo el backend no existe todavía (macOS Core Audio taps y Linux PipeWire
// quedan para la Fase C), así que procSource es un stub que reporta "no soportado".
// main.go cae a la captura de salida entera cuando esto falla.

type procSource struct{ match string }

func (p *procSource) start(*frameSink) error {
	return errors.New("captura por programa: disponible solo en Windows")
}

func (p *procSource) stop() {}

// listPrograms no enumera nada fuera de Windows → el picker de programas queda vacío.
func listPrograms() []programChoice { return nil }
