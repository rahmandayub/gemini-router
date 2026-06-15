# Plan: Fix Validated Code Smells in gemini-router

## Context

Validasi temuan code smell pada gemini-router — proxy yang menerjemahkan API OpenAI/Anthropic ke format Gemini. Dari 8 temuan awal, **6 terkonfirmasi**, **2 dieliminasi** (stale/incorrect). Temuan tambahan: 2 dead code, 1 logging prefix belum normalisasi. Dari 6 task plan lama, **4 sudah dikerjakan** (client.go, pool.go guard, ids.go constants, scanner.Err). Sisa **2 perlu dikerjakan** + **4 temuan baru**.

---

## Verified Findings (6 confirmed, 2 eliminated, 2 additional)

### ✅ Confirmed
1. `panic()` di pool.go — line 19-21
2. Logging overhead — json.Marshal full payload setiap request (openai.go:250, anthropic.go:249)
3. handleStreamResponse complexity — anthropic.go:301-556 (255 lines, God function)
4. Duplicate routing — router.go mux registrations (32-36) + manual ServeHTTP (41-70)
5. Type assertion map — anthropic.go parseAnthropicContent (685-776) unsafe
6. Silent defaults — config.go validate() mutates cfg (lines 57-67)

### ❌ Eliminated (stale)
1. resp.Body leak — ALL already use `defer resp.Body.Close()` correctly
2. http.DefaultClient — ALREADY replaced with UpstreamClient everywhere

### 🔍 Additional
1. Dead code: PipeStreamingResponse — never called
2. Dead code: Mux registrations in router.go — never used (ServeHTTP does manual routing)
3. Log prefix `[ANTHROPIC]` not normalized to `[proxy/anthropic]`

---

## Previous Plan Status (4 of 6 tasks DONE)

| Task | Status |
|------|--------|
| 1. HTTP Client Kustom (client.go) | ✅ Done |
| 2. Guard clause pool.go | ✅ Done |
| 3. Magic strings → ids.go | ✅ Done |
| 4. scanner.Err stream.go | ✅ Done |
| 5. Fix logging key index | ⚠️ Partial (openai.go/gemini.go use keys_total) |
| 6. Normalize log tags | ⚠️ Partial (anthropic.go still `[ANTHROPIC]`) |

---

## Tasks (Remaining Work)

### Phase 1: Cleanup Dead Code
**Priority: High | Effort: Low**

1. **Hapus `PipeStreamingResponse` di stream.go** (line 10-35)
   - Tidak dipanggil dari mana pun di codebase
   - `WriteSSE` dan `WriteSSEEvent` tetap dipertahankan (dipakai anthropic.go)

2. **Konsolidasi routing di router.go**
   - Hapus manual routing di `ServeHTTP` (lines 42-69)
   - Ganti body `ServeHTTP` dengan: `r.mux.ServeHTTP(w, req)`
   - Mux registrations (lines 32-36) tetap dipakai sebagai satu-satunya routing mechanism

### Phase 2: Logging Standardization & Performance
**Priority: Medium | Effort: Low**

1. **Normalize log prefix** di anthropic.go:250
   - `[ANTHROPIC]` → `[proxy/anthropic]`

2. **Guard json.Marshal di hot path**
   - openai.go:250-251 dan anthropic.go:249-250
   - Bungkus dalam log level check atau lazy evaluation
   - Perbaiki error yang di-ignore (`reqJSON, _ := ...`)

### Phase 3: Config Refactoring
**Priority: Medium | Effort: Low**

1. **Pisahkan `validate()` → `SetDefaults()` + `Validate()`** di config.go
   - `SetDefaults()`: memutasikan config (port, host, level) — boleh mutate
   - `Validate()`: read-only, hanya return error
   - Panggil keduanya di `Load()`

### Phase 4: Robustness & Readability
**Priority: Medium | Effort: High**

1. **Refactor type assertions di anthropic.go**
   - Ganti `map[string]interface{}` pattern di `parseAnthropicContent` dengan typed structs
   - Contoh: `type anthropicRawBlock struct { Type string; Text string; ... }`
   - Atau gunakan `json.RawMessage` + custom unmarshal

2. **Dekomposisi `handleStreamResponse` di anthropic.go**
   - Current: 255 lines (lines 301-556)
   - Pecah menjadi helper functions:
     - `processContentPart(part, state) []SSEEvent`
     - `handleToolUse(part, state) []SSEEvent`
     - `writeBlockTransitions(state)`
   - Tujuan: masing-masing <50 lines

---

## File Changes Summary

| File | Action |
|------|--------|
| `internal/proxy/stream.go` | **EDIT** — hapus `PipeStreamingResponse` |
| `internal/proxy/router.go` | **EDIT** — hapus manual routing, delegasi ke mux |
| `internal/proxy/openai.go` | **EDIT** — guard json.Marshal logging |
| `internal/proxy/anthropic.go` | **EDIT** — normalize log prefix, guard json.Marshal, refactor parseAnthropicContent, decompose handleStreamResponse |
| `internal/config/config.go` | **EDIT** — split validate() into SetDefaults() + Validate() |

---

## Verification

1. `go build ./...` — kompilasi sukses
2. `go vet ./...` — tidak ada warning baru
3. `go test ./...` — test yang ada tetap pass
4. `grep -rn 'PipeStreamingResponse' internal/` — tidak ada hasil
5. `grep -rn '\[ANTHROPIC\]' internal/` — tidak ada hasil
6. `grep -rn 'http.DefaultClient' internal/` — tidak ada hasil (verify stale finding)
7. Review router.go — `ServeHTTP` hanya satu baris delegasi ke mux
8. Review config.go — `Validate()` tidak memutasikan input

## Decisions

- **Scope**: Hanya temuan yang divalidasi. `resp.Body` leak dan `http.DefaultClient` dikecualikan (sudah fixed).
- **Logging**: Pertahankan `log` package (small project). Migrasi ke `slog` bisa follow-up terpisah.
- **pool.go panic**: Dipertahankan — programming error, bukan runtime error. Config validation sudah mencegah empty pool di startup.
