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
}

func newUplink(frames <-chan []byte) *uplink { return &uplink{frames: frames} }

func (u *uplink) status() status  { return status(u.st.Load()) }
func (u *uplink) sending() bool   { return u.enabled.Load() }
func (u *uplink) setEnabled(b bool) { u.enabled.Store(b) }

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

func (u *uplink) run(ctx context.Context, base, event, room string) {
	full := buildURL(base, event, room)
	var conn *websocket.Conn
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
			if !u.enabled.Load() {
				closeConn()
				u.st.Store(int32(statusOff))
				continue
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

// buildURL appends ?event=&room= to the ingest base, preserving any existing query.
func buildURL(base, event, room string) string {
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
	u.RawQuery = q.Encode()
	return u.String()
}
