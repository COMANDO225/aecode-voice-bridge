# aecode-voice-bridge

Puente nativo de audio para la traducción en vivo de AECODE/CONEIC. Corre en la
**bandeja del sistema** (tipo Steam) en la laptop del evento: capta el audio del
ponente desde una tarjeta de sonido USB y lo envía a la nube (`/ingest`). A
diferencia de la página web `/speak`, **no lo afecta minimizar ni que la laptop
quede idle**, y **reconecta solo**.

## Hardware (cadena en el evento)

```
Micrófono UHF → Receptor UHF (MIX OUT 6.5mm) → cable 6.5→3.5 (o →RCA)
    → tarjeta de sonido USB CON ENTRADA de micrófono/línea → USB → laptop
```

⚠️ La tarjeta USB debe tener **entrada** (no solo salida). Opciones:
- **Tarjeta USB 7.1 de 2 jacks** (verde=salida, rosa/🎤=entrada) → el UHF va al de micrófono.
- **Behringer UCA202** (entrada de línea RCA) → mejor calidad, sin problemas de nivel (necesita cable 6.5→RCA).
- **Divisor TRRS** + el jack de la propia laptop → el UHF va al lado del micrófono.

Los adaptadores de **un solo jack** (HOCO / UGREEN combo) suelen ser solo salida → **no sirven**.
Si el sonido satura, **bajá el volumen de salida del receptor UHF** (el UHF sale "fuerte", la entrada de mic espera señal débil).

## Uso

1. Doble-clic al `.exe` → aparece el ícono en la bandeja (sin ventana).
2. Clic derecho → **Micrófono** → elegí la tarjeta USB.
3. Verificá que **Nivel** se mueva cuando alguien habla por el UHF.
4. **Estado: Conectado ✓** = está enviando a la nube. (Reconecta solo si se corta.)
5. Opcional: **Iniciar con el sistema** para que arranque solo al prender la laptop.

La **URL del servidor** se deja una vez en el config (persona técnica, antes de mandar la laptop):
**Abrir carpeta de configuración** → editar `config.json`:

```json
{ "url": "wss://TU-SERVIDOR/ingest", "event": "summit-2026", "room": "main", "device": "", "autostart": false }
```

## Build

Requiere Go 1.25+ y un compilador C (cgo).

```sh
# Nativo (Linux/Mac):
go build -o aecode-voice-bridge .

# Windows .exe (cross-compile desde Linux; requiere mingw-w64):
./build-windows.sh
```

Flags útiles (modo consola / diagnóstico):

```sh
./aecode-voice-bridge -list                                   # ver dispositivos de entrada
./aecode-voice-bridge -console -url ws://localhost:8787/ingest # correr sin bandeja, log a stdout
```

Log del modo bandeja: `<carpeta de config>/bridge.log`.
