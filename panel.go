package main

import (
	"context"
	_ "embed"
	"net"
	"net/http"
	"time"

	"github.com/coder/websocket"
	json "encoding/json"
)

//go:embed panel.html
var panelHTML []byte

// startPanel serves the local control panel (HTML + WebSocket) on 127.0.0.1 and
// returns its URL. The WS streams the live waveform + status ~30 fps and receives
// the switch (setSending) and microphone (setDevice) commands.
func startPanel(cap *capture, up *uplink, serverURL string, getDevice func() string, setDevice func(string)) (string, error) {
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

		send(map[string]any{
			"type": "init", "devices": cap.inputDevices(), "device": getDevice(),
			"url": serverURL, "sending": up.sending(), "status": up.statusStr(),
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
					up.setEnabled(m.On)
				case "setDevice":
					setDevice(m.Name)
				}
			}
		}()

		t := time.NewTicker(33 * time.Millisecond) // ~30 fps
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if !send(map[string]any{
					"type": "tick", "wave": cap.waveform(), "level": cap.level(),
					"status": up.statusStr(), "sending": up.sending(),
				}) {
					return
				}
			}
		}
	})
	go func() { _ = (&http.Server{Handler: mux}).Serve(ln) }()
	return "http://" + ln.Addr().String(), nil
}
