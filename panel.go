package main

import (
	"context"
	_ "embed"
	"net"
	"net/http"
	"time"

	json "encoding/json"
	"github.com/coder/websocket"
)

//go:embed panel.html
var panelHTML []byte

// startPanel serves the local control panel (HTML + WebSocket) on 127.0.0.1 and
// returns its URL. The WS streams the live waveform + status ~30 fps and receives
// the switch (setSending) and microphone (setDevice) commands.
func startPanel(
	cap *capture, up *uplink, feed *roomsFeed,
	getDevice func() string, setDevice func(string),
	getRoom func() string, getRooms func() []RoomChoice, setRoom func(string),
	getSource func() string, setSource func(string),
) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(panelHTML)
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx := r.Context()

		send := func(v any) bool {
			b, _ := json.Marshal(v)
			wctx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			return c.Write(wctx, websocket.MessageText, b) == nil
		}
		// input (mic) o output (loopback) según la fuente elegida.
		devicesFor := func() []string {
			if getSource() == "system" {
				return cap.outputDevices()
			}
			return cap.inputDevices()
		}

		send(map[string]any{
			"type": "init", "devices": devicesFor(), "device": getDevice(),
			"source": getSource(), "muted": up.isMuted(),
			"url": up.targetURL(), "sending": up.sending(), "status": up.statusStr(),
			"rooms": getRooms(), "room": getRoom(), "now": feed.nowIn(getRoom()),
		})

		go func() { // commands from the panel
			for {
				_, data, err := c.Read(ctx)
				if err != nil {
					return
				}
				var m struct {
					Cmd, Name string
					On        bool
				}
				if json.Unmarshal(data, &m) != nil {
					continue
				}
				switch m.Cmd {
				case "setSending":
					// Sin sala no se transmite: es lo que impide que una laptop
					// mal configurada entre a un auditorio que no le toca.
					if m.On && getRoom() == "" {
						continue
					}
					up.setEnabled(m.On)
				case "setDevice":
					setDevice(m.Name)
				case "setRoom":
					setRoom(m.Name)
				case "setSource":
					setSource(m.Name)
				case "setMuted":
					up.setMuted(m.On)
				}
			}
		}()

		t := time.NewTicker(33 * time.Millisecond) // ~30 fps
		defer t.Stop()
		var lastSrc string
		var cachedDevices []string // enumerar dispositivos es caro → solo al cambiar la fuente
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				room, src := getRoom(), getSource()
				if src != lastSrc || cachedDevices == nil {
					lastSrc = src
					cachedDevices = devicesFor()
				}
				if !send(map[string]any{
					"type": "tick", "wave": cap.waveform(), "level": cap.level(),
					"status": up.statusStr(), "sending": up.sending(), "muted": up.isMuted(),
					"source": src, "device": getDevice(), "devices": cachedDevices,
					"room": room, "rooms": getRooms(), "now": feed.nowIn(room),
					"url": up.targetURL(),
				}) {
					return
				}
			}
		}
	})
	go func() { _ = (&http.Server{Handler: mux}).Serve(ln) }()
	return "http://" + ln.Addr().String(), nil
}
