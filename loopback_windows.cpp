// Captura de audio POR PROCESO en Windows (WASAPI process loopback).
//
// Adaptado del sample oficial de Microsoft (Windows-classic-samples/ApplicationLoopback):
// ActivateAudioInterfaceAsync + AUDIOCLIENT_ACTIVATION_TYPE_PROCESS_LOOPBACK en modo
// INCLUDE sobre el PID (y su árbol de hijos) → capta SOLO el audio de esa app, aunque
// otra cosa suene en paralelo. El engine convierte al formato que pedimos: 16 kHz / 16
// bit / mono, listo para el pipeline. Sin drivers ni cable virtual.
//
// Este archivo solo se compila en Windows (sufijo _windows). El lado Go
// (capture_process_windows.go) lo maneja como un `source`.

#include <windows.h>
#include <mmdeviceapi.h>
#include <audioclient.h>
#include <audiopolicy.h>
#include <mmreg.h>
#include <propidl.h>
#include <combaseapi.h>
#include <wchar.h>
#include <string.h>
#include <stdlib.h>

// --- Params de process-loopback. En el SDK de Windows 11 viven en
// <audioclientactivationparams.h>; el MinGW de CI puede no traerlo, así que se
// incluye si existe y, si no, se declaran a mano (mismo layout binario). ---
#if defined(__has_include)
#  if __has_include(<audioclientactivationparams.h>)
#    include <audioclientactivationparams.h>
#    define AECODE_HAVE_ACTPARAMS 1
#  endif
#endif

#ifndef AECODE_HAVE_ACTPARAMS
typedef enum {
    AUDIOCLIENT_ACTIVATION_TYPE_DEFAULT = 0,
    AUDIOCLIENT_ACTIVATION_TYPE_PROCESS_LOOPBACK = 1
} AUDIOCLIENT_ACTIVATION_TYPE;

typedef enum {
    PROCESS_LOOPBACK_MODE_INCLUDE_TARGET_PROCESS_TREE = 0,
    PROCESS_LOOPBACK_MODE_EXCLUDE_TARGET_PROCESS_TREE = 1
} PROCESS_LOOPBACK_MODE;

typedef struct {
    DWORD TargetProcessId;
    PROCESS_LOOPBACK_MODE ProcessLoopbackMode;
} AUDIOCLIENT_PROCESS_LOOPBACK_PARAMS;

typedef struct {
    AUDIOCLIENT_ACTIVATION_TYPE ActivationType;
    union {
        AUDIOCLIENT_PROCESS_LOOPBACK_PARAMS ProcessLoopbackParams;
    };
} AUDIOCLIENT_ACTIVATION_PARAMS;
#endif

#ifndef VIRTUAL_AUDIO_DEVICE_PROCESS_LOOPBACK
#define VIRTUAL_AUDIO_DEVICE_PROCESS_LOOPBACK L"VAD\\Process_Loopback"
#endif

// ---------------------------------------------------------------------------
// Ring buffer (PCM s16le mono) — el hilo de captura escribe, el lado Go drena.
// ---------------------------------------------------------------------------
static CRITICAL_SECTION g_cs;
static BYTE* g_ring = nullptr;
static int   g_cap = 0, g_tail = 0, g_head = 0, g_size = 0;
static int   g_inited = 0;

static void ring_write(const BYTE* p, int n) {
    EnterCriticalSection(&g_cs);
    for (int i = 0; i < n; i++) {
        g_ring[g_head] = p[i];
        g_head = (g_head + 1) % g_cap;
        if (g_size < g_cap) g_size++;
        else g_tail = (g_tail + 1) % g_cap; // se pisa lo más viejo: seguimos en vivo
    }
    LeaveCriticalSection(&g_cs);
}

static int ring_read(BYTE* out, int max) {
    EnterCriticalSection(&g_cs);
    int n = g_size < max ? g_size : max;
    for (int i = 0; i < n; i++) {
        out[i] = g_ring[g_tail];
        g_tail = (g_tail + 1) % g_cap;
    }
    g_size -= n;
    LeaveCriticalSection(&g_cs);
    return n;
}

// ---------------------------------------------------------------------------
// Handler de completado para ActivateAudioInterfaceAsync (COM).
// ---------------------------------------------------------------------------
class ActivateHandler : public IActivateAudioInterfaceCompletionHandler {
    LONG m_ref = 1;
public:
    HANDLE done = nullptr;
    HRESULT hr = E_FAIL;
    IAudioClient* client = nullptr;

    ActivateHandler() { done = CreateEventW(nullptr, TRUE, FALSE, nullptr); }
    ~ActivateHandler() {
        if (client) client->Release();
        if (done) CloseHandle(done);
    }

    HRESULT STDMETHODCALLTYPE QueryInterface(REFIID riid, void** ppv) override {
        if (riid == __uuidof(IUnknown) ||
            riid == __uuidof(IActivateAudioInterfaceCompletionHandler) ||
            riid == __uuidof(IAgileObject)) {
            *ppv = static_cast<IActivateAudioInterfaceCompletionHandler*>(this);
            AddRef();
            return S_OK;
        }
        *ppv = nullptr;
        return E_NOINTERFACE;
    }
    ULONG STDMETHODCALLTYPE AddRef() override { return InterlockedIncrement(&m_ref); }
    ULONG STDMETHODCALLTYPE Release() override {
        LONG r = InterlockedDecrement(&m_ref);
        if (r == 0) delete this;
        return r;
    }
    HRESULT STDMETHODCALLTYPE ActivateCompleted(IActivateAudioInterfaceAsyncOperation* op) override {
        HRESULT act = E_FAIL;
        IUnknown* punk = nullptr;
        HRESULT h = op->GetActivateResult(&act, &punk);
        if (SUCCEEDED(h) && SUCCEEDED(act) && punk) {
            punk->QueryInterface(__uuidof(IAudioClient), (void**)&client);
            hr = client ? S_OK : E_NOINTERFACE;
        } else {
            hr = SUCCEEDED(h) ? act : h;
        }
        if (punk) punk->Release();
        SetEvent(done);
        return S_OK;
    }
};

// ---------------------------------------------------------------------------
// Estado de captura.
// ---------------------------------------------------------------------------
static IAudioClient*        g_client  = nullptr;
static IAudioCaptureClient* g_capture = nullptr;
static HANDLE               g_thread  = nullptr;
static volatile LONG        g_run     = 0;

static DWORD WINAPI capture_thread(LPVOID) {
    CoInitializeEx(nullptr, COINIT_MULTITHREADED);
    const int frameBytes = 2; // s16 mono
    static BYTE zeros[4096];
    while (g_run) {
        UINT32 packet = 0;
        if (FAILED(g_capture->GetNextPacketSize(&packet))) break;
        if (packet == 0) { Sleep(8); continue; }
        while (packet > 0 && g_run) {
            BYTE* data = nullptr;
            UINT32 frames = 0;
            DWORD flags = 0;
            if (FAILED(g_capture->GetBuffer(&data, &frames, &flags, nullptr, nullptr))) break;
            int bytes = (int)frames * frameBytes;
            if (flags & AUDCLNT_BUFFERFLAGS_SILENT) {
                int left = bytes;
                memset(zeros, 0, sizeof(zeros));
                while (left > 0) { int c = left > (int)sizeof(zeros) ? (int)sizeof(zeros) : left; ring_write(zeros, c); left -= c; }
            } else if (data && bytes > 0) {
                ring_write(data, bytes);
            }
            g_capture->ReleaseBuffer(frames);
            if (FAILED(g_capture->GetNextPacketSize(&packet))) break;
        }
    }
    CoUninitialize();
    return 0;
}

extern "C" void proc_stop(void);

// proc_start engancha el audio del proceso `pid` (y su árbol). 0 = ok, negativo = error.
extern "C" int proc_start(unsigned long pid) {
    proc_stop();
    CoInitializeEx(nullptr, COINIT_MULTITHREADED);

    AUDIOCLIENT_ACTIVATION_PARAMS ap = {};
    ap.ActivationType = AUDIOCLIENT_ACTIVATION_TYPE_PROCESS_LOOPBACK;
    ap.ProcessLoopbackParams.TargetProcessId = (DWORD)pid;
    ap.ProcessLoopbackParams.ProcessLoopbackMode = PROCESS_LOOPBACK_MODE_INCLUDE_TARGET_PROCESS_TREE;

    PROPVARIANT pv = {};
    pv.vt = VT_BLOB;
    pv.blob.cbSize = sizeof(ap);
    pv.blob.pBlobData = (BYTE*)&ap;

    ActivateHandler* handler = new ActivateHandler();
    IActivateAudioInterfaceAsyncOperation* op = nullptr;
    HRESULT hr = ActivateAudioInterfaceAsync(
        VIRTUAL_AUDIO_DEVICE_PROCESS_LOOPBACK, __uuidof(IAudioClient), &pv, handler, &op);
    if (FAILED(hr)) { if (op) op->Release(); handler->Release(); return -1; }
    WaitForSingleObject(handler->done, 5000);
    if (op) op->Release();
    if (FAILED(handler->hr) || !handler->client) { handler->Release(); return -2; }
    g_client = handler->client;
    g_client->AddRef();       // el handler suelta su ref en el destructor
    handler->Release();

    WAVEFORMATEX fmt = {};
    fmt.wFormatTag = WAVE_FORMAT_PCM;
    fmt.nChannels = 1;
    fmt.nSamplesPerSec = 16000;
    fmt.wBitsPerSample = 16;
    fmt.nBlockAlign = fmt.nChannels * fmt.wBitsPerSample / 8; // 2
    fmt.nAvgBytesPerSec = fmt.nSamplesPerSec * fmt.nBlockAlign; // 32000
    fmt.cbSize = 0;

    hr = g_client->Initialize(
        AUDCLNT_SHAREMODE_SHARED,
        AUDCLNT_STREAMFLAGS_LOOPBACK | AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM | AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY,
        2000000, // 200 ms
        0, &fmt, nullptr);
    if (FAILED(hr)) { proc_stop(); return -3; }

    hr = g_client->GetService(__uuidof(IAudioCaptureClient), (void**)&g_capture);
    if (FAILED(hr)) { proc_stop(); return -4; }

    g_cap = 16000 * 2 * 2; // ~2 s de PCM 16k mono s16
    g_ring = (BYTE*)malloc(g_cap);
    if (!g_ring) { proc_stop(); return -6; }
    g_head = g_tail = g_size = 0;
    InitializeCriticalSection(&g_cs);
    g_inited = 1;

    if (FAILED(g_client->Start())) { proc_stop(); return -5; }
    g_run = 1;
    g_thread = CreateThread(nullptr, 0, capture_thread, nullptr, 0, nullptr);
    return 0;
}

// proc_read drena hasta maxBytes de PCM. Devuelve bytes copiados (0 si no hay).
extern "C" int proc_read(void* buf, int maxBytes) {
    if (!g_inited || maxBytes <= 0) return 0;
    return ring_read((BYTE*)buf, maxBytes);
}

extern "C" void proc_stop(void) {
    if (g_run) {
        g_run = 0;
        if (g_thread) { WaitForSingleObject(g_thread, 2000); CloseHandle(g_thread); g_thread = nullptr; }
    }
    if (g_capture) { g_capture->Release(); g_capture = nullptr; }
    if (g_client) { g_client->Stop(); g_client->Release(); g_client = nullptr; }
    if (g_inited) { DeleteCriticalSection(&g_cs); g_inited = 0; }
    if (g_ring) { free(g_ring); g_ring = nullptr; g_cap = g_head = g_tail = g_size = 0; }
}

// proc_list enumera las apps que están sonando (sesiones de audio del render por
// defecto). Rellena pids[], names[] (basename del exe, UTF-8) y labels[] (nombre
// visible o exe), cada string en su slot de `stride` bytes. Devuelve cuántas.
extern "C" int proc_list(unsigned long* pids, char* names, char* labels, int stride, int max) {
    int count = 0;
    CoInitializeEx(nullptr, COINIT_MULTITHREADED);

    IMMDeviceEnumerator* devEnum = nullptr;
    if (FAILED(CoCreateInstance(__uuidof(MMDeviceEnumerator), nullptr, CLSCTX_ALL,
                                __uuidof(IMMDeviceEnumerator), (void**)&devEnum)))
        return 0;

    IMMDevice* dev = nullptr;
    if (SUCCEEDED(devEnum->GetDefaultAudioEndpoint(eRender, eConsole, &dev))) {
        IAudioSessionManager2* mgr = nullptr;
        if (SUCCEEDED(dev->Activate(__uuidof(IAudioSessionManager2), CLSCTX_ALL, nullptr, (void**)&mgr))) {
            IAudioSessionEnumerator* se = nullptr;
            if (SUCCEEDED(mgr->GetSessionEnumerator(&se))) {
                int n = 0;
                se->GetCount(&n);
                for (int i = 0; i < n && count < max; i++) {
                    IAudioSessionControl* sc = nullptr;
                    if (FAILED(se->GetSession(i, &sc))) continue;
                    IAudioSessionControl2* sc2 = nullptr;
                    if (SUCCEEDED(sc->QueryInterface(__uuidof(IAudioSessionControl2), (void**)&sc2))) {
                        DWORD spid = 0;
                        sc2->GetProcessId(&spid);
                        if (spid != 0 && sc2->IsSystemSoundsSession() != S_OK) {
                            char exe[260] = {0}, disp[260] = {0};
                            HANDLE ph = OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, FALSE, spid);
                            if (ph) {
                                WCHAR wpath[512];
                                DWORD sz = 512;
                                if (QueryFullProcessImageNameW(ph, 0, wpath, &sz)) {
                                    WCHAR* base = wcsrchr(wpath, L'\\');
                                    base = base ? base + 1 : wpath;
                                    WideCharToMultiByte(CP_UTF8, 0, base, -1, exe, sizeof(exe), nullptr, nullptr);
                                }
                                CloseHandle(ph);
                            }
                            LPWSTR dn = nullptr;
                            if (SUCCEEDED(sc2->GetDisplayName(&dn)) && dn && dn[0])
                                WideCharToMultiByte(CP_UTF8, 0, dn, -1, disp, sizeof(disp), nullptr, nullptr);
                            if (dn) CoTaskMemFree(dn);

                            if (exe[0]) {
                                int dup = 0;
                                for (int k = 0; k < count; k++)
                                    if (_stricmp(names + (size_t)k * stride, exe) == 0) { dup = 1; break; }
                                if (!dup) {
                                    pids[count] = spid;
                                    strncpy(names + (size_t)count * stride, exe, stride - 1);
                                    const char* lbl = disp[0] ? disp : exe;
                                    strncpy(labels + (size_t)count * stride, lbl, stride - 1);
                                    count++;
                                }
                            }
                        }
                        sc2->Release();
                    }
                    sc->Release();
                }
                se->Release();
            }
            mgr->Release();
        }
        dev->Release();
    }
    devEnum->Release();
    return count;
}
