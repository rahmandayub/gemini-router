# Gemini Router

Proxy server yang menerjemahkan permintaan dalam format **OpenAI**, **Anthropic**, dan **OpenAI Responses API** ke **Google Gemini API** — sehingga kamu bisa menjalankan aplikasi yang awalnya dikembangkan untuk OpenAI atau Anthropic tanpa mengubah kode klien sama sekali.

```
┌────────────────┐          ┌────────────────┐          ┌──────────────────────┐
│   OpenAI SDK   ├─────────>│                │          │                      │
├────────────────┤          │                │          │                      │
│ Anthropic SDK  ├─────────>│ MyGeminiRouter │─────────>│      Gemini API      │
├────────────────┤          │    (Proxy)     │          │ (generativelanguage  │
│     curl /     ├─────────>│                │          │   .googleapis.com)   │
│  HTTP Client   │          │                │          │                      │
└────────────────┘          └────────────────┘          └──────────────────────┘
                                Port 18080
```

---

## Table of Contents

- [Fitur](#fitur)
- [Arsitektur](#arsitektur)
- [Struktur Proyek](#struktur-proyek)
- [Requirements](#requirements)
- [Instalasi](#instalasi)
- [Konfigurasi](#konfigurasi)
- [Penggunaan](#penggunaan)
- [Endpoint API](#endpoint-api)
- [Contoh Penggunaan](#contoh-penggunaan)
- [Troubleshooting](#troubleshooting)
- [License](#license)

---

## Fitur

| Fitur | Deskripsi |
|---|---|
| **OpenAI Chat Completions** | Endpoint `/v1/chat/completions` — terjemahkan format OpenAI ke Gemini |
| **OpenAI Responses API** | Endpoint `/v1/responses` — terjemahkan format Responses API ke Gemini |
| **Anthropic Messages API** | Endpoint `/v1/messages` — terjemahkan format Anthropic ke Gemini |
| **Direct Gemini Passthrough** | Endpoint `/v1beta/*` — teruskan permintaan Gemini apa adanya |
| **Streaming (SSE)** | Dukungan penuh untuk streaming di semua format klien |
| **Tool / Function Calling** | Konversi tool calling dari semua format klien ke format Gemini |
| **Reasoning / Thinking** | Dukungan reasoning content untuk OpenAI, thinking blocks untuk Anthropic |
| **Round-Robin Key Pool** | Rotasi otomatis beberapa API key Gemini secara bergantian |
| **Health Check** | Endpoint `/health` untuk monitoring status layanan |
| **Systemd Service** | Instalasi otomatis sebagai user-level systemd service |
| **Request Logging** | Middleware logging untuk setiap request yang masuk |
| **Debug Mode** | Toggle verbose payload logging via environment variable |

---

## Arsitektur

Gemini Router dirancang sebagai **stateless HTTP reverse proxy** dengan lapisan terjemahan format (translation layer). Setiap permintaan dari klien diterjemahkan ke format Gemini yang sesuai, dikirim ke upstream, lalu responsenya diterjemahkan balik ke format yang diminta klien.

### Alur Request

```
Client Request
      │
      ▼
┌─────────────┐
│  Middleware   │  ← Request logging
│  (logging)   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│   Router     │  ← Route ke handler berdasarkan path
│  (mux)       │
└──────┬──────┘
       │
       ├── /v1/chat/completions ──▶ OpenAIHandler
       ├── /v1/responses        ──▶ ResponsesHandler
       ├── /v1/messages         ──▶ AnthropicHandler
       ├── /v1/models           ──▶ modelsHandler
       ├── /v1beta/*            ──▶ GeminiHandler (passthrough)
       └── /health              ──▶ healthHandler
                                     │
         ┌───────────────────────────┘
         ▼
┌─────────────────────┐
│  Translation Layer   │  ← Konversi format klien ↔ format Gemini
│  (translateToGemini/ │
│   translateFromGemini)│
└──────┬──────────────┘
       │
       ▼
┌─────────────────────┐
│    Key Pool          │  ← Round-robin pemilihan API key
│    (next key)        │
└──────┬──────────────┘
       │
       ▼
┌─────────────────────┐
│  UpstreamClient      │  ← HTTP client (HTTP/2, connection pooling)
│  (net/http)          │
└──────┬──────────────┘
       │
       ▼
  Gemini API
  (generativelanguage.googleapis.com)
```

### Komponen Inti

| Komponen | Package | Tanggung Jawab |
|---|---|---|
| **Entry Point** | `cmd/gemini-router` | Parse flags, load config, bootstrap server |
| **Config** | `internal/config` | Load & validasi YAML config, apply defaults |
| **Key Pool** | `internal/key` | Penyimpanan & rotasi round-robin API keys |
| **Router** | `internal/proxy/router` | HTTP mux, dispatch ke handler sesuai path |
| **OpenAI Handler** | `internal/proxy/openai` | Terjemahkan Chat Completions → Gemini |
| **Responses Handler** | `internal/proxy/responses` | Terjemahkan Responses API → Gemini |
| **Anthropic Handler** | `internal/proxy/anthropic` | Terjemahkan Messages API → Gemini |
| **Gemini Handler** | `internal/proxy/gemini` | Passthrough langsung ke Gemini API |
| **Client** | `internal/proxy/client` | HTTP client global dengan connection pooling |
| **SSE Helpers** | `internal/proxy/stream` | Utilitas untuk menulis Server-Sent Events |
| **ID Generator** | `internal/proxy/ids` | Konstanta prefix untuk ID sintetis |
| **Debug Logger** | `internal/proxy/log` | Toggle & utilitas logging payload debug |
| **Logging Middleware** | `internal/middleware` | Log method, path, status code, durasi |

---

## Struktur Proyek

```
gemini-router/
├── cmd/
│   └── gemini-router/
│       └── main.go                 # Entry point
├── configs/
│   └── config.yaml                 # Contoh file konfigurasi
├── internal/
│   ├── config/
│   │   └── config.go               # Load, defaults, validasi config
│   ├── key/
│   │   └── pool.go                 # Round-robin API key pool
│   ├── middleware/
│   │   └── logging.go              # HTTP request logging middleware
│   └── proxy/
│       ├── router.go               # HTTP mux & route definitions
│       ├── openai.go               # OpenAI Chat Completions handler
│       ├── responses.go            # OpenAI Responses API handler
│       ├── anthropic.go            # Anthropic Messages API handler
│       ├── gemini.go               # Gemini passthrough handler
│       ├── client.go               # Shared HTTP client config
│       ├── stream.go               # SSE write helpers
│       ├── ids.go                  # ID prefix constants
│       └── log.go                  # Debug logging utilities
├── Makefile                        # Build, install, uninstall
├── go.mod                          # Go module definition
├── go.sum                          # Dependency checksums
└── .gitignore
```

---

## Requirements

### Build Requirements

| Kebutuhan | Versi | Catatan |
|---|---|---|
| **Go** | ≥ 1.22.2 | Didefinisikan di `go.mod` |
| **make** | Any | Untuk menjalankan build/install via Makefile |
| **Internet** | — | Untuk `go mod download` saat build pertama kali |
| **Linux (opsional)** | — | Diperlukan untuk instalasi systemd service |

### Runtime Requirements

| Kebutuhan | Catatan |
|---|---|
| **Linux dengan systemd** | Untuk `make install` (user-level systemd service) |
| **Google Gemini API Key** | Minimal 1 key dari [Google AI Studio](https://aistudio.google.com/) |
| **Port tersedia** | Default `18080`, bisa dikonfigurasi |

### Dependency

Proyek ini hanya memiliki **satu dependency eksternal**:

```
gopkg.in/yaml.v3  (v3.0.1)
```

Semua komponen lainnya menggunakan standard library Go.

---

## Instalasi

### Build Saja

```bash
make build
```

Binary akan dihasilkan di direktori proyek sebagai `gemini-router`.

### Build & Install (Linux / systemd)

```bash
make install
```

Perintah ini akan:

1. **Build binary** → `./gemini-router`
2. **Copy binary** ke `~/.local/bin/gemini-router`
3. **Buat file konfigurasi** di `~/.config/gemini-router/config.yaml`
   - Akan ditanya port (default: `18080`)
   - Akan ditanya API key(s) — minimal 1
4. **Buat systemd user service** di `~/.config/systemd/user/gemini-router.service`
5. **Enable & start service** secara otomatis

### Uninstall

```bash
make uninstall
```

> Catatan: File konfigurasi di `~/.config/gemini-router/` **tidak dihapus** secara otomatis. Hapus manual jika diperlukan:
> ```bash
> rm -rf ~/.config/gemini-router
> ```

### Clean

```bash
make clean
```

Hapus binary hasil build dari direktori proyek.

---

## Konfigurasi

File konfigurasi menggunakan format YAML. Default path: `configs/config.yaml` (dev) atau `~/.config/gemini-router/config.yaml` (produksi).

### Referensi Konfigurasi

```yaml
server:
    host: '127.0.0.1'    # Alamat bind server
    port: 18080           # Port server

gemini:
    base_url: 'https://generativelanguage.googleapis.com'  # Gemini API base URL
    api_keys:              # Daftar API key (round-robin)
        - 'AIzaSy...'
        - 'AIzaSy...'
        - 'AIzaSy...'

logging:
    level: 'info'          # Log level: debug, info, warn, error
```

### Parameter

| Parameter | Tipe | Default | Deskripsi |
|---|---|---|---|
| `server.host` | string | `127.0.0.1` | Alamat IP untuk bind server |
| `server.port` | int | `18080` | Port untuk listen |
| `gemini.base_url` | string | *(required)* | Base URL Gemini API |
| `gemini.api_keys` | []string | *(required)* | Minimal 1 API key Gemini |
| `logging.level` | string | `info` | Level logging (`debug`, `info`, `warn`, `error`) |

### Debug Mode

Untuk verbose payload logging (request & response body), set environment variable:

```bash
export GEMINI_ROUTER_DEBUG=1
```

Atau:

```bash
export GEMINI_ROUTER_DEBUG=true
```

---

## Penggunaan

### Jalankan secara Manual

```bash
./gemini-router -config configs/config.yaml
```

### Jalankan via Systemd (setelah install)

```bash
# Status
systemctl --user status gemini-router

# Restart
systemctl --user restart gemini-router

# Lihat log
journalctl --user -u gemini-router -f
```

### Jalankan dengan Custom Config

```bash
./gemini-router -config /path/to/custom-config.yaml
```

---

## Endpoint API

| Endpoint | Method | Format | Deskripsi |
|---|---|---|---|
| `/v1/chat/completions` | POST | OpenAI Chat Completions | Terjemahkan ke Gemini, kembalikan format OpenAI |
| `/v1/responses` | POST | OpenAI Responses API | Terjemahkan ke Gemini, kembalikan format Responses |
| `/v1/messages` | POST | Anthropic Messages API | Terjemahkan ke Gemini, kembalikan format Anthropic |
| `/v1/models` | GET | OpenAI Models | Daftar model Gemini dalam format OpenAI |
| `/v1beta/*` | * | Gemini API | Passthrough langsung ke Gemini API |
| `/health` | GET | JSON | Health check & jumlah key aktif |

### Format Request yang Didukung

#### OpenAI Chat Completions (`/v1/chat/completions`)

- Multi-turn conversation
- System message
- Tool / function calling
- Streaming (`"stream": true`)
- Reasoning content (extended thinking)

#### OpenAI Responses API (`/v1/responses`)

- Input items (message, function call output)
- Instructions (system prompt)
- Tool definitions & tool choice
- Streaming
- Reasoning

#### Anthropic Messages API (`/v1/messages`)

- Multi-turn conversation
- System prompt
- Tool definitions & tool choice
- Streaming (all event types: `message_start`, `content_block_start/delta`, `message_delta`, `message_stop`)
- Extended thinking (`thinking` content blocks)
- Tool use blocks

---

## Contoh Penggunaan

### 1. OpenAI Python SDK

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://127.0.0.1:18080/v1",
    api_key="any-value",  # ignored, key dari config
)

response = client.chat.completions.create(
    model="gemini-2.0-flash",
    messages=[
        {"role": "system", "content": "You are a helpful assistant."},
        {"role": "user", "content": "Hello!"},
    ],
)

print(response.choices[0].message.content)
```

### 2. OpenAI Streaming

```python
stream = client.chat.completions.create(
    model="gemini-2.0-flash",
    messages=[{"role": "user", "content": "Tell me a story"}],
    stream=True,
)

for chunk in stream:
    if chunk.choices[0].delta.content:
        print(chunk.choices[0].delta.content, end="", flush=True)
```

### 3. Anthropic Python SDK

```python
import anthropic

client = anthropic.Anthropic(
    base_url="http://127.0.0.1:18080",
    api_key="any-value",  # ignored, key dari config
)

message = client.messages.create(
    model="gemini-2.0-flash",
    max_tokens=1024,
    messages=[
        {"role": "user", "content": "What is the capital of France?"}
    ],
)

print(message.content[0].text)
```

### 4. curl — OpenAI Format

```bash
curl http://127.0.0.1:18080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-2.0-flash",
    "messages": [
      {"role": "user", "content": "Hello!"}
    ]
  }'
```

### 5. curl — Anthropic Format

```bash
curl http://127.0.0.1:18080/v1/messages \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "gemini-2.0-flash",
    "max_tokens": 1024,
    "messages": [
      {"role": "user", "content": "Hello!"}
    ]
  }'
```

### 6. curl — Direct Gemini Passthrough

```bash
curl "http://127.0.0.1:18080/v1beta/models/gemini-2.0-flash:generateContent?key=YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [{"parts": [{"text": "Hello!"}]}]
  }'
```

### 7. Health Check

```bash
curl http://127.0.0.1:18080/health
```

Response:
```json
{
  "status": "ok",
  "keys_count": 3
}
```

### 8. List Models

```bash
curl http://127.0.0.1:18080/v1/models
```

Response:
```json
{
  "object": "list",
  "data": [
    {
      "id": "gemini-2.0-flash",
      "object": "model",
      "created": 1718544000,
      "owned_by": "google"
    }
  ]
}
```

---

## Troubleshooting

### Server tidak mau start

- Pastikan file konfigurasi valid dan minimal ada 1 API key:
  ```bash
  ./gemini-router -config configs/config.yaml
  ```
- Periksa apakah port sudah digunakan:
  ```bash
  ss -tlnp | grep 18080
  ```

### Error 401 / 403 dari Gemini API

- Pastikan API key valid di [Google AI Studio](https://aistudio.google.com/)
- Beberapa model mungkin memerlukan billing aktif

### Error 502 Bad Gateway

- Periksa koneksi internet dari server ke `generativelanguage.googleapis.com`
- Coba increase timeout atau periksa firewall

### Lihat log debug

```bash
GEMINI_ROUTER_DEBUG=1 ./gemini-router -config configs/config.yaml
```

### Systemd service tidak jalan

```bash
# Cek status
systemctl --user status gemini-router

# Cek log
journalctl --user -u gemini-router --no-pager -n 50

# Restart
systemctl --user restart gemini-router
```

---

## License

[MIT](https://opensource.org/licenses/MIT)
