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
	statusReconnecting status = iota
	statusConnected
)

// uplink streams captured frames to the /ingest WebSocket, reconnecting forever.
// The capture keeps running independently; only the network link retries.
type uplink struct {
	frames <-chan []byte
	st     atomic.Int32
}

func newUplink(frames <-chan []byte) *uplink { return &uplink{frames: frames} }

func (u *uplink) status() status { return status(u.st.Load()) }

func (u *uplink) run(ctx context.Context, base, event, room string) {
	full := buildURL(base, event, room)
	for ctx.Err() == nil {
		u.st.Store(int32(statusReconnecting))
		u.session(ctx, full)
		if ctx.Err() != nil {
			return
		}
		select { // brief backoff before redial
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
}

func (u *uplink) session(ctx context.Context, full string) {
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	c, _, err := websocket.Dial(dctx, full, nil)
	cancel()
	if err != nil {
		return
	}
	defer c.CloseNow()
	u.st.Store(int32(statusConnected))
	for {
		select {
		case <-ctx.Done():
			return
		case f := <-u.frames:
			wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
			err := c.Write(wctx, websocket.MessageBinary, f)
			wcancel()
			if err != nil {
				return
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
