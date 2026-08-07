//go:build windows

package main

/*
#cgo CXXFLAGS: -std=c++17 -fno-exceptions -fno-rtti
#cgo LDFLAGS: -lole32 -loleaut32 -lmmdevapi -luuid -static-libgcc -static-libstdc++

int  proc_start(unsigned long pid);
int  proc_read(void* buf, int maxBytes);
void proc_stop(void);
int  proc_list(unsigned long* pids, char* names, char* labels, int stride, int max);
*/
import "C"

import (
	"fmt"
	"strings"
	"time"
	"unsafe"
)

const (
	procNameStride = 260 // debe coincidir con los buffers del shim
	procMax        = 64
)

type procEntry struct {
	pid   uint32
	exe   string // basename del ejecutable (el "value" persistido)
	label string // nombre visible de la sesión, o el exe
}

// enumAudioApps lista las apps que están sonando ahora (una entrada por exe).
func enumAudioApps() []procEntry {
	pids := make([]C.ulong, procMax)
	names := make([]C.char, procMax*procNameStride)
	labels := make([]C.char, procMax*procNameStride)
	n := int(C.proc_list(&pids[0], &names[0], &labels[0], C.int(procNameStride), C.int(procMax)))
	out := make([]procEntry, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, procEntry{
			pid:   uint32(pids[i]),
			exe:   C.GoString(&names[i*procNameStride]),
			label: C.GoString(&labels[i*procNameStride]),
		})
	}
	return out
}

// listPrograms alimenta el picker del panel.
func listPrograms() []programChoice {
	var out []programChoice
	for _, e := range enumAudioApps() {
		label := e.label
		if label == "" || strings.EqualFold(strings.TrimSpace(label), e.exe) {
			label = strings.TrimSuffix(e.exe, ".exe")
			label = strings.TrimSuffix(label, ".EXE")
		}
		out = append(out, programChoice{Value: e.exe, Label: label})
	}
	return out
}

// procSource capta el audio de una sola app vía WASAPI process loopback.
type procSource struct {
	match string
	stopc chan struct{}
}

func (p *procSource) start(sink *frameSink) error {
	var pid uint32
	for _, e := range enumAudioApps() {
		if strings.EqualFold(e.exe, p.match) {
			pid = e.pid
			break
		}
	}
	if pid == 0 {
		return fmt.Errorf("la app %q no está sonando ahora — elegila cuando reproduzca audio", p.match)
	}
	if rc := int(C.proc_start(C.ulong(pid))); rc != 0 {
		return fmt.Errorf("no pude enganchar el audio de %q (código %d)", p.match, rc)
	}
	p.stopc = make(chan struct{})
	go p.readLoop(sink, p.stopc)
	return nil
}

// readLoop drena el ring del shim y empuja el PCM al sink (10 ms de cadencia).
func (p *procSource) readLoop(sink *frameSink, stopc chan struct{}) {
	buf := make([]byte, 3200) // 100 ms @ 16k mono s16
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-stopc:
			return
		case <-t.C:
			for {
				n := int(C.proc_read(unsafe.Pointer(&buf[0]), C.int(len(buf))))
				if n <= 0 {
					break
				}
				sink.push(buf[:n])
				if n < len(buf) {
					break
				}
			}
		}
	}
}

func (p *procSource) stop() {
	if p.stopc != nil {
		close(p.stopc)
		p.stopc = nil
	}
	C.proc_stop()
}
