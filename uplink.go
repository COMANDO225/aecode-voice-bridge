package main

import (
	"context"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

type status int32

const (
	statusOff          status = iota // switch apagado: no se envía
	statusReconnecting               // switch prendido pero sin conexión
	statusConnected                  // enviando al backend
)

// uplink streams captured frames to the /ingest WebSocket — but ONLY while the
// switch is enabled. It always drains the frames channel (so it never blocks the
// capture) and connects/sends only when enabled, reconnecting on failure.
type uplink struct {
	frames  <-chan []byte
	st      atomic.Int32
	enabled atomic.Bool
	muted   atomic.Bool
	// target: la URL completa de ingest. Atómica y NO capturada al arrancar, que era el
	// motivo de que la sala solo se pudiera cambiar editando un archivo y reiniciando.
	// Vacía = sin sala elegida: no se disca nada.
	target atomic.Pointer[string]
	// rec: copia local a disco de lo que se envía. El seguro contra cortes de red,
	// reinicios del servidor y caídas del proveedor.
	rec  *recorder
	room atomic.Pointer[string]
}

func newUplink(frames <-chan []byte, rec *recorder) *uplink {
	return &uplink{frames: frames, rec: rec}
}

// setTarget apunta el puente a otra sala. Cierra la conexión en curso para que el
// siguiente frame reconecte al destino nuevo.
func (u *uplink) setTarget(base, event, room, key string) {
	full := ""
	if room != "" && event != "" {
		full = buildURL(base, event, room, key)
	}
	u.target.Store(&full)
	u.room.Store(&room)
}

func (u *uplink) targetURL() string {
	if p := u.target.Load(); p != nil {
		return *p
	}
	return ""
}

func (u *uplink) status() status    { return status(u.st.Load()) }
func (u *uplink) sending() bool     { return u.enabled.Load() }
func (u *uplink) setEnabled(b bool) { u.enabled.Store(b) }

// MUTE (silencio suave, ≠ switch): manda silencio pero NO cierra la conexión. El
// canal sigue vivo; al desmutear el audio real vuelve al instante, sin reconectar.
func (u *uplink) isMuted() bool   { return u.muted.Load() }
func (u *uplink) setMuted(b bool) { u.muted.Store(b) }

func (u *uplink) statusStr() string {
	switch u.status() {
	case statusConnected:
		return "connected"
	case statusReconnecting:
		return "connecting"
	default:
		return "off"
	}
}

func (u *uplink) run(ctx context.Context) {
	var conn *websocket.Conn
	var connTarget string // a qué URL está conectada `conn`
	var lastDial time.Time
	closeConn := func() {
		if conn != nil {
			conn.CloseNow()
			conn = nil
		}
	}
	defer closeConn()

	for {
		select {
		case <-ctx.Done():
			return
		case f := <-u.frames: // always drain, even when off
			full := u.targetURL()
			if u.muted.Load() { // MUTE: silencio, pero el canal NO se cierra
				f = make([]byte, len(f))
			}
			// Se graba lo que se ENVÍA (switch prendido), no lo que se capta: el
			// archivo debe corresponder con lo que el servidor debería haber recibido.
			if u.enabled.Load() && u.rec != nil {
				if p := u.room.Load(); p != nil {
					u.rec.write(*p, f, time.Now())
				}
			}
			if !u.enabled.Load() || full == "" {
				closeConn()
				u.st.Store(int32(statusOff))
				continue
			}
			// Cambió la sala mientras estaba conectado: se corta y se redisca al nuevo.
			if conn != nil && connTarget != full {
				closeConn()
			}
			if conn == nil {
				if time.Since(lastDial) < 2*time.Second { // throttle redials
					u.st.Store(int32(statusReconnecting))
					continue
				}
				lastDial = time.Now()
				dctx, cancel := context.WithTimeout(ctx, 8*time.Second)
				nc, _, err := websocket.Dial(dctx, full, nil)
				cancel()
				if err != nil {
					u.st.Store(int32(statusReconnecting))
					continue
				}
				conn = nc
				connTarget = full
				u.st.Store(int32(statusConnected))
			}
			wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
			err := conn.Write(wctx, websocket.MessageBinary, f)
			wcancel()
			if err != nil {
				closeConn()
				u.st.Store(int32(statusReconnecting))
			}
		}
	}
}

// buildURL appends ?event=&room=&key= to the ingest base, preserving any existing query.
func buildURL(base, event, room, key string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	q := u.Query()
	if event != "" {
		q.Set("event", event)
	}
	if room != "" {
		q.Set("room", room)
	}
	if key != "" {
		q.Set("key", key)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
