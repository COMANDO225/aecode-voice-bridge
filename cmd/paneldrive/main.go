// paneldrive simula al operador: se conecta al panel local, lee lo que vería en
// pantalla y elige una sala del desplegable.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/coder/websocket"
)

func main() {
	base := flag.String("panel", "", "URL del panel")
	pick := flag.String("pick", "", "slug de sala a elegir")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(*base, "http") + "/ws"
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		fmt.Println("DIAL_ERR", err)
		return
	}
	defer c.CloseNow()

	read := func() map[string]any {
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return nil
			}
			var m map[string]any
			if json.Unmarshal(data, &m) != nil {
				continue
			}
			if m["type"] == "init" || m["type"] == "tick" {
				return m
			}
		}
	}
	show := func(tag string, m map[string]any) {
		if m == nil {
			fmt.Println(tag, "sin respuesta")
			return
		}
		rooms := ""
		if rs, ok := m["rooms"].([]any); ok {
			for _, r := range rs {
				if rm, ok := r.(map[string]any); ok {
					rooms += fmt.Sprintf("%v ", rm["slug"])
				}
			}
		}
		fmt.Printf("%s sala=%q ahora=%q enviando=%v estado=%v desplegable=[%s]\n",
			tag, m["room"], m["now"], m["sending"], m["status"], strings.TrimSpace(rooms))
	}

	show("  ANTES ", read())
	if *pick != "" {
		_ = c.Write(ctx, websocket.MessageText,
			[]byte(fmt.Sprintf(`{"cmd":"setRoom","name":%q}`, *pick)))
		time.Sleep(1500 * time.Millisecond)
		show("  DESPUÉS", read())
	}
}
