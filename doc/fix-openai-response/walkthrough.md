# Walkthrough: Gemma-4-31b-it to OpenAI Format Translation Fixes

This document outlines the bugs identified and resolved in the `gemini-router` proxy to achieve perfect translation from Google's `gemma-4-31b-it` model responses (including internal reasoning steps and parallel tool calling) to standard OpenAI chat completion payloads.

---

## 1. Background & Context
`gemma-4-31b-it` is a dense, reasoning-focused model. Unlike standard models, it outputs:
1. **Internal reasoning segments** (labeled with `"thought": true` in the Gemini API `parts` array).
2. **Parallel tool calls** streamed across separate chunks.

Our task was to make the proxy route and translate these outputs into OpenAI's specifications without breaking strict client integrations (like VS Code Copilot).

---

## 2. Issues & Solutions

### A. Separation of Thinking Process (`reasoning_content`)
* **Problem**: Originally, the router concatenated all candidate parts into the `content` field. This resulted in the model's internal thinking process polluting the final visible text.
* **Solution**: 
  * Added the `Thought` field to `GeminiPart` to match Gemini's API.
  * Added the `ReasoningContent` field (`reasoning_content` in JSON) to `OpenAIMessage` and `OpenAIDelta`.
  * Updated [openai.go](file:///home/rahmandayub/Projects/gemini-router/internal/proxy/openai.go) to isolate segments with `thought: true` and map them to `reasoning_content`, keeping the final answer text in `content`.

### B. Client-side Crash on `content: null`
* **Problem**: In streaming chunks where the model is thinking, the `Content` field was a pointer (`*string`) and serialized to `"content": null`. Strict JS/TS client parsers (e.g. Copilot) crashed trying to read `content.length` or `content.concat`.
* **Solution**: Changed `Content` in `OpenAIDelta` back to `string` without `omitempty`. When empty (during reasoning or tool calling), it now serializes as `"content": ""` (empty string), which is safe and standard.

### C. SSE Streaming Latency (Buffering)
* **Problem**: The router was reading the entire response body upfront using `io.ReadAll(resp.Body)` before emitting chunks. This caused the connection to sit idle for 10-18 seconds, triggering client timeouts and losing the real-time typing effect.
* **Solution**: Replaced the buffering logic in `handleStreamResponse` with a real-time stream pipe using `bufio.Reader` to read lines incrementally.

### D. Duplicate Tool Call Indices & IDs
* **Problem**: When streaming parallel tool calls, the router had no session state, so it assigned `"index": 0` and the same ID (`call_read_file`) to every chunk. The client merged these calls, leading to corrupt arguments (e.g. appending paths).
* **Solution**: 
  * Maintained a `globalToolCallIdx` in the streaming loop.
  * Assigned incremental indices (`0`, `1`, `2`, ...) to parallel tool calls in the stream.
  * Appended the index to the ID (e.g. `call_read_file_0`, `call_read_file_1`) to guarantee uniqueness.

### E. Mismatched Tool Response Name Mapping
* **Problem**: When a client returned execution outputs, the router sent the unique ID (e.g. `call_read_file_0`) directly back to Gemini. Since Gemini expected the declared function name (`read_file`), it threw a validation error.
* **Solution**: Updated `translateToGemini`'s `tool` case to strip the `"call_"` prefix and the `_index` suffix (e.g., `_0`) so that function results map back to their original names. Additionally, if the tool output is not valid JSON, it falls back to a plain JSON-escaped string to prevent marshalling errors.

### F. Invalid Stream Choice Structs
* **Problem**: `OpenAIChoice.Message` was a value type (`OpenAIMessage`). In streaming chunks, this caused Go to serialize empty `"message":{"role":"","content":""}` fields alongside `"delta"`, violating OpenAI specifications and overriding client buffers.
* **Solution**: Converted `Message` in `OpenAIChoice` to a pointer (`*OpenAIMessage`). It is now completely omitted (set to `nil`) in stream chunks.

### G. Standardized request `id` and `created` Timestamps
* **Problem**: The proxy previously returned static fields `"chatcmpl-1234567890"` and `1234567890` for all completions.
* **Solution**: Implemented a helper `generateID()` using `crypto/rand` and dynamic timestamping via `time.Now().Unix()` so that completed responses look genuine.

---

## 3. How to Run & Verify

### Run Unit Tests
To verify translations are correct, run:
```bash
go test -v ./internal/proxy/...
```

### Restart Service
If you edit code locally, rebuild and restart the user service to test:
```bash
go build -o gemini-router ./cmd/gemini-router
systemctl --user stop gemini-router
cp gemini-router ~/.local/bin/gemini-router
systemctl --user start gemini-router
```
