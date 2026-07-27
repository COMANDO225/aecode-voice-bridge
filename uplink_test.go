package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func query(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url inválida %q: %v", raw, err)
	}
	return u.Query()
}

func TestBuildURL_LlevaEventoSalaYClave(t *testing.T) {
	q := query(t, buildURL("wss://voz.example/ingest", "coneic-cusco-2026", "sala-b", "secreta"))

	if q.Get("event") != "coneic-cusco-2026" || q.Get("room") != "sala-b" || q.Get("key") != "secreta" {
		t.Errorf("faltan parámetros: %v", q)
	}
}

// Sin sala NO hay destino, y sin destino el uplink no disca. Es lo que impide que una
// laptop recién sacada de la caja entre a un auditorio real: antes traía "main" de
// fábrica y se mezclaba con quien estuviera transmitiendo.
func TestSetTarget_SinSala_NoHayDestino(t *testing.T) {
	u := newUplink(nil, nil)

	u.setTarget("ws://x/ingest", "ev", "", "k")
	if got := u.targetURL(); got != "" {
		t.Errorf("sin sala el destino debe quedar vacío, salió %q", got)
	}

	u.setTarget("ws://x/ingest", "ev", "sala-b", "k")
	if got := u.targetURL(); !strings.Contains(got, "room=sala-b") {
		t.Errorf("con sala debe haber destino, salió %q", got)
	}
}

// Cambiar de sala tiene que cambiar el destino sin reiniciar el proceso. Antes la URL
// se congelaba al arrancar, y por eso la sala solo se podía tocar editando un JSON.
func TestSetTarget_CambiarDeSalaCambiaElDestino(t *testing.T) {
	u := newUplink(nil, nil)
	u.setTarget("ws://x/ingest", "ev", "sala-b", "")
	primero := u.targetURL()

	u.setTarget("ws://x/ingest", "ev", "auditorio-principal", "")

	if u.targetURL() == primero {
		t.Error("el destino no cambió al elegir otra sala")
	}
	if !strings.Contains(u.targetURL(), "room=auditorio-principal") {
		t.Errorf("destino = %q", u.targetURL())
	}
}

func TestRoomsFeed_DescargaYNormaliza(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"rooms": []map[string]any{
			{"slug": "sala-b", "label": "Sala B", "now": map[string]string{
				"title": "BIM Vial", "startsAt": "2026-08-11T08:00:00-05:00", "endsAt": "2026-08-11T09:00:00-05:00",
			}},
		}})
	}))
	defer srv.Close()

	var got []RoomChoice
	f := newRoomsFeed(srv.URL, func(rs []RoomChoice) { got = rs })
	f.fetch(t.Context())

	if len(got) != 1 || got[0].Slug != "sala-b" || got[0].Label != "Sala B" {
		t.Fatalf("desplegable = %v", got)
	}
	// El rótulo es lo que delata que la laptop está en la sala equivocada.
	if want := "BIM Vial · 08:00–09:00"; f.nowIn("sala-b") != want {
		t.Errorf("rótulo = %q, quería %q", f.nowIn("sala-b"), want)
	}
	if f.nowIn("otra-sala") != "" {
		t.Error("una sala sin nada en curso no debe heredar el rótulo de la vecina")
	}
}

// El día del evento el servicio de eventos puede estar caído. Eso NO puede dejar al
// operador con un desplegable vacío: la traducción no depende de ese backend.
func TestRoomsFeed_ServidorCaido_ConservaLaCache(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"rooms": []map[string]any{
			{"slug": "sala-b", "label": "Sala B"},
		}})
	}))
	f := newRoomsFeed(ok.URL, func([]RoomChoice) {})
	f.fetch(t.Context())
	ok.Close() // se cae el backend

	f.fetch(t.Context()) // el refresco falla

	if len(f.rooms()) != 1 || f.rooms()[0].Slug != "sala-b" {
		t.Errorf("un fallo de red borró la caché: %v", f.rooms())
	}
}

// Una respuesta vacía tampoco debe vaciar el desplegable: es indistinguible de un
// backend a medio arrancar, y dejaría al operador sin poder elegir.
func TestRoomsFeed_RespuestaVacia_NoBorraLaCache(t *testing.T) {
	first := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if first {
			first = false
			_ = json.NewEncoder(w).Encode(map[string]any{"rooms": []map[string]any{{"slug": "sala-b", "label": "Sala B"}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"rooms": []map[string]any{}})
	}))
	defer srv.Close()

	f := newRoomsFeed(srv.URL, func([]RoomChoice) {})
	f.fetch(t.Context())
	f.fetch(t.Context())

	if len(f.rooms()) != 1 {
		t.Errorf("una respuesta vacía borró la caché: %v", f.rooms())
	}
}
