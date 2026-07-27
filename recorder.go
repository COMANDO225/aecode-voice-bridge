package main

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

// El puente guarda en disco el MISMO audio que envía.
//
// Son ~115 MB por hora, nada para una laptop. A cambio convierte cualquier fallo del
// pipeline en un retraso en vez de una pérdida: si se cae el wifi, se reinicia el
// servidor o el proveedor de IA falla, esa ponencia se reprocesa por la noche desde el
// archivo y la transcripción queda completa. Cubre los tres a la vez, que es más de lo
// que consigue cualquier buffer de reenvío.
type recorder struct {
	dir string

	mu   sync.Mutex
	f    *os.File
	room string
}

func newRecorder() *recorder {
	p, err := configPath()
	if err != nil {
		return &recorder{}
	}
	dir := filepath.Join(filepath.Dir(p), "grabaciones")
	if os.MkdirAll(dir, 0o755) != nil {
		return &recorder{}
	}
	return &recorder{dir: dir}
}

// write vuelca el frame al archivo de la sala, abriendo uno nuevo si cambió.
// Nunca devuelve error: un disco lleno no puede tumbar la transmisión en vivo.
func (r *recorder) write(room string, frame []byte, now time.Time) {
	if r.dir == "" || room == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil || r.room != room {
		r.closeLocked()
		name := room + "-" + now.Format("2006-01-02_15-04-05") + ".pcm"
		f, err := os.Create(filepath.Join(r.dir, name))
		if err != nil {
			return
		}
		r.f, r.room = f, room
	}
	_, _ = r.f.Write(frame)
}

func (r *recorder) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeLocked()
}

func (r *recorder) closeLocked() {
	if r.f != nil {
		_ = r.f.Close()
		r.f, r.room = nil, ""
	}
}
