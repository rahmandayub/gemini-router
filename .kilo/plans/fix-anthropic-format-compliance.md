# Fix Anthropic API Format Compliance

## Goal
Fix all identified issues in `internal/proxy/anthropic.go` to make the Anthropic Messages API proxy fully compliant with the official Anthropic API specification.

## Issues to Fix

### 1. CRITICAL: Tool result function name extraction (lines 691-706)

**Problem:** The code tries to extract function names from `tool_use_id` using a heuristic (`toolu_<random_hex>`), but Anthropic's `tool_use_id` does NOT contain the function name. This means tool results get wrong function names, breaking tool-use flows entirely.

**Fix:** Build a `toolUseIDToName` map during message parsing. When processing assistant messages, collect all `tool_use` blocks and map their `id` → `name`. Pass this map to `parseAnthropicContent` so `tool_result` blocks can look up the correct function name.

**Changes:**
- Add a `toolUseIDToName map[string]string` parameter to `parseAnthropicContent`
- In `translateAnthropicToGemini`, pre-scan ALL assistant messages (not just the immediately preceding one) to populate the map
- Pass the map when calling `parseAnthropicContent`
- In the `tool_result` case, look up the function name from the map instead of parsing from the ID
- If a `tool_result` references an ID not in the map (malformed client request), fall back gracefully — use the raw `tool_use_id` as the name rather than panicking

### 2. MODERATE: SSE missing `event:` line (stream.go:32-38)

**Problem:** The `WriteSSE` function only writes `data: <json>\n\n` but the official Anthropic SSE format includes `event: <type>\n` before each `data:` line.

**Fix:** Add a new `WriteSSEEvent(w, eventType, data)` function that writes `event: <type>\ndata: <json>\n\n`. Keep the existing `WriteSSE` for backward compatibility (used by OpenAI handler). Use the new function in all Anthropic streaming code.

**Changes to `stream.go`:**
```go
func WriteSSEEvent(w http.ResponseWriter, eventType string, data []byte) {
    w.Write([]byte("event: "))
    w.Write([]byte(eventType))
    w.Write([]byte("\ndata: "))
    w.Write(data)
    w.Write([]byte("\n\n"))
    if flusher, ok := w.(http.Flusher); ok {
        flusher.Flush()
    }
}
```

**Changes to `anthropic.go`:** Replace all `WriteSSE(w, eventData)` + `flusher.Flush()` with `WriteSSEEvent(w, eventType, eventData)`. Event type strings must match the Anthropic spec exactly:
- `message_start`
- `content_block_start`
- `content_block_delta`
- `content_block_stop`
- `message_delta`
- `message_stop`
- `ping`
- `error`

### 3. MINOR: Double flush in streaming

**Problem:** After each `WriteSSE(w, eventData)` call, there's an explicit `flusher.Flush()` — but `WriteSSE` already flushes internally.

**Fix:** Resolved by issue #2 — the new `WriteSSEEvent` function handles flushing, so remove all redundant `flusher.Flush()` calls in `handleStreamResponse`.

### 4. MINOR: Missing `stop_sequence` in message_delta (lines 521-530)

**Problem:** The `stop_sequence` field is always nil. The Anthropic API format expects `stop_sequence: null` explicitly.

**Fix:** This already serializes as `null` in JSON since Go's zero value for `*string` is nil. No code change needed — verified that `json:",omitempty"` is NOT on the `StopSequence` field (it isn't, so it's fine).

### 5. MINOR: Missing cache usage fields

**Problem:** Anthropic API v2024+ includes `cache_creation_input_tokens` and `cache_read_input_tokens` in usage. The Anthropic API always returns them, so clients may expect them to be present.

**Fix:** Add these fields to `AnthropicUsage` struct WITHOUT `omitempty` so they serialize as `0` consistently (matching Anthropic's behavior).

**Changes:**
```go
type AnthropicUsage struct {
    InputTokens              int `json:"input_tokens"`
    OutputTokens             int `json:"output_tokens"`
    CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
    CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}
```

### 6. MINOR: Missing content block types in input parsing (lines 660-721)

**Problem:** `parseAnthropicContent` only handles `text` and `tool_result` blocks. Drops `image`, `tool_use` (in assistant messages for multi-turn), `thinking`, and `document` blocks silently.

**Fix for `tool_use` blocks:** These appear in assistant messages. In `translateAnthropicToGemini`, when processing assistant messages, convert `tool_use` blocks to Gemini `FunctionCall` parts (similar to how the OpenAI handler does it at lines 439-456).

**Fix for `thinking` blocks:** Convert to Gemini `GeminiPart` with `Thought: true`. The `GeminiPart` struct already supports this field (verified in openai.go:92). When round-tripping thinking content from Gemini → Anthropic response, it's already handled in `translateFromGeminiToAnthropic` (line 747) and `handleStreamResponse` (lines 397-431).

**Fix for `image` blocks:** Convert to Gemini `inlineData` format (base64 inline data). Need to add `InlineData` field to `GeminiPart` struct. Anthropic's image blocks support two source types: `type: "base64"` (supported) and `type: "url"` (not supported). For URL sources, log a warning and skip the block rather than producing a malformed `GeminiPart`.

**Changes to `parseAnthropicContent`:**
- Add `tool_use` case → produce `GeminiPart` with `FunctionCall`
- Add `thinking` case → produce `GeminiPart` with `Thought: true` (drop `signature` field as it's opaque to the proxy)
- Add `image` case → produce `GeminiPart` with `InlineData` for `type: "base64"`, log warning and skip for `type: "url"`

**Changes to `GeminiPart` (in openai.go):**
```go
type GeminiPart struct {
    Text             string              `json:"text,omitempty"`
    Thought          bool                `json:"thought,omitempty"`
    FunctionCall     *GeminiFuncCall     `json:"functionCall,omitempty"`
    FunctionResponse *GeminiFuncResponse `json:"functionResponse,omitempty"`
    InlineData       *GeminiInlineData   `json:"inlineData,omitempty"`
}
```

**New struct:**
```go
type GeminiInlineData struct {
    MimeType string `json:"mimeType"`
    Data     string `json:"data"`
}
```

### 7. NEW: Error event format

**Problem:** Anthropic SSE errors use `event: error` with a specific JSON body. If error paths just write raw text or use `WriteSSE`, they'll be non-compliant.

**Fix:** In `handleStreamResponse`, when the upstream returns a non-200 status code (lines 302-308), send a proper Anthropic error SSE event. Headers must be set BEFORE any `WriteHeader` call to avoid panics:

```go
if resp.StatusCode != http.StatusOK {
    body, _ := io.ReadAll(resp.Body)
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    w.WriteHeader(http.StatusOK)  // SSE stream is always 200

    errorEvent := map[string]interface{}{
        "type": "error",
        "error": map[string]interface{}{
            "type":    "api_error",
            "message": string(body),
        },
    }
    eventData, _ := json.Marshal(errorEvent)
    WriteSSEEvent(w, "error", eventData)
    return
}
```

**Important:** Verify no other code path in the handler calls `w.WriteHeader` before this point. The `handleStreamResponse` function must be the first place where `WriteHeader` is called for streaming requests.

### 8. NEW: `ping` keepalive event

**Problem:** The Anthropic spec sends `event: ping\ndata: {"type":"ping"}` after `message_start`. Some clients may timeout without it.

**Fix:** Add a single `ping` event after `message_start` in the stream. Do NOT implement a periodic ticker — that adds goroutine lifecycle complexity for minimal benefit. One ping after stream open is sufficient.

## File Changes

### `internal/proxy/stream.go`
- Add `WriteSSEEvent(w http.ResponseWriter, eventType string, data []byte)` function

### `internal/proxy/openai.go`
- Add `InlineData *GeminiInlineData` field to `GeminiPart` struct
- Add `GeminiInlineData` struct definition

### `internal/proxy/anthropic.go`
- Add `CacheCreationInputTokens` and `CacheReadInputTokens` fields to `AnthropicUsage` (without `omitempty`)
- Add `AnthropicImageSource` and `AnthropicImageBlock` structs for image block parsing
- Update `translateAnthropicToGemini` to build `toolUseIDToName` map and pass it to `parseAnthropicContent`
- Update `parseAnthropicContent` signature to accept `toolUseIDToName map[string]string`
- Fix `tool_result` name lookup to use the map, with graceful fallback
- Add `tool_use`, `thinking`, and `image` block handling in `parseAnthropicContent` (image: support base64, log warning for URL sources)
- Replace all `WriteSSE(w, eventData)` + `flusher.Flush()` with `WriteSSEEvent(w, eventType, eventData)` in `handleStreamResponse`
- Remove redundant `flusher.Flush()` calls after `WriteSSEEvent`
- Add `ping` event after `message_start` in streaming
- Fix error handling in `handleStreamResponse` to send proper `error` SSE event

## Verification

1. **Build:** `go build ./...` to ensure no compilation errors
2. **Tests:** `go test ./...` to run existing tests
3. **Manual test scenarios:**
   - Tool use flow: Send a request with tools, verify tool_use blocks have correct names in streaming and non-streaming
   - Tool result flow: Send a multi-turn conversation with tool_results, verify function names are correctly mapped from assistant's prior tool_use blocks
   - Multi-turn tool use (non-adjacent): Send a 4+ turn conversation where tool_use blocks appear in turn 2 and tool_results appear in turn 4 (not adjacent), verify toolUseIDToName map scopes correctly across ALL messages, not per-turn
   - Tool result graceful fallback: Send a tool_result with unknown tool_use_id, verify no panic and graceful handling
   - Streaming SSE format: Capture raw SSE output and verify `event:` lines are present before each `data:` line
   - Streaming error format: Send a request with invalid model, verify `event: error` SSE event is returned
   - Thinking blocks: Send a request with thinking enabled, verify thinking content appears correctly in both streaming and non-streaming
   - Image blocks: Send a request with base64 image content, verify it's forwarded correctly to Gemini
   - Usage fields: Verify `cache_creation_input_tokens` and `cache_read_input_tokens` appear as `0` in responses
   - Ping events: Verify `event: ping` appears in stream output
