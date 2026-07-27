package main

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// La lista de salas y la ponencia que suena en cada una. El puente la usa para dos
// cosas: llenar el desplegable, y —más importante— mostrarle al operador QUÉ cree que
// está transmitiendo. Si el de Sala B lee "Ceremonia de Clausura · Auditorio Principal",
// el error de configuración salta a la cara en dos segundos. Un desplegable sin ese
// rótulo solo mueve el error de sitio.
type roomInfo struct {
	Slug  string `json:"slug"`
	Label string `json:"label"`
	Now   *struct {
		Title    string `json:"title"`
		StartsAt string `json:"startsAt"`
		EndsAt   string `json:"endsAt"`
	} `json:"now"`
}

type roomsFeed struct {
	url  string
	set  func([]RoomChoice)
	mu   sync.Mutex
	last []roomInfo
}

func newRoomsFeed(url string, set func([]RoomChoice)) *roomsFeed {
	return &roomsFeed{url: url, set: set}
}

// rooms devuelve la última lista descargada (vacía si nunca hubo una buena).
func (f *roomsFeed) rooms() []roomInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last
}

// nowIn devuelve el rótulo de lo que suena en una sala: "Diseño Sísmico · 09:30–10:30".
func (f *roomsFeed) nowIn(slug string) string {
	for _, r := range f.rooms() {
		if r.Slug != slug || r.Now == nil {
			continue
		}
		if hm := hhmmRange(r.Now.StartsAt, r.Now.EndsAt); hm != "" {
			return r.Now.Title + " · " + hm
		}
		return r.Now.Title
	}
	return ""
}

// run refresca cada 30 s. Un fallo NUNCA borra la caché: es preferible una lista de
// hace una hora que un desplegable vacío en mitad del congreso.
func (f *roomsFeed) run(ctx context.Context) {
	if f.url == "" {
		return
	}
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	f.fetch(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			f.fetch(ctx)
		}
	}
}

func (f *roomsFeed) fetch(ctx context.Context) {
	rctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, f.url, nil)
	if err != nil {
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var body struct {
		Rooms []roomInfo `json:"rooms"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil || len(body.Rooms) == 0 {
		return
	}
	f.mu.Lock()
	f.last = body.Rooms
	f.mu.Unlock()

	choices := make([]RoomChoice, 0, len(body.Rooms))
	for _, r := range body.Rooms {
		choices = append(choices, RoomChoice{Slug: r.Slug, Label: r.Label})
	}
	f.set(choices)
}

// hhmmRange: "2026-08-11T09:30:00-05:00" + fin → "09:30–10:30".
func hhmmRange(startsAt, endsAt string) string {
	s := hhmm(startsAt)
	if s == "" {
		return ""
	}
	if e := hhmm(endsAt); e != "" {
		return s + "–" + e
	}
	return s
}

func hhmm(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return ""
	}
	return t.Format("15:04")
}
