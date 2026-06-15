# Walkthrough: Gemma-4-31b-it to OpenAI & Anthropic Format Translation Fixes

This document outlines the bugs identified and resolved in the `gemini-router` proxy to achieve perfect translation from Google's `gemma-4-31b-it` model responses (including internal reasoning steps and parallel tool calling) to standard OpenAI chat completion and Anthropic messages payloads.

---

## 1. Background & Context
`gemma-4-31b-it` is a dense, reasoning-focused model. Unlike standard models, it outputs:
1. **Internal reasoning segments** (labeled with `"thought": true` in the Gemini API `parts` array).
2. **Parallel tool calls** streamed across separate chunks.

Our task was to make the proxy route and translate these outputs into OpenAI's and Anthropic's specifications without breaking strict client integrations (like VS Code Copilot).

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

### H. Alternating Turn Roles Violations (Consecutive Roles)
* **Problem**: In `translateToGemini` (OpenAI handler), if a message had both `Content` and `ToolCalls`, the proxy generated two separate `"model"` blocks consecutively. Furthermore, if there were parallel tool responses, they were translated as consecutive `"function"` blocks. This violated Gemini's strict rule that consecutive turns must have alternating roles, causing Gemini to reject the request or hang.
* **Solution**: 
  * Merged assistant text and tool calls into a single `"model"` content block.
  * Added grouping logic to group all consecutive `"tool"` response messages into a single `"function"` block.

### I. Anthropic Tool Result Role Mapping Bug
* **Problem**: Anthropic client sends tool results with role `"user"` and block type `"tool_result"`. The proxy translated this into a Gemini block with role `"user"` but containing `FunctionResponse` parts. Gemini requires `FunctionResponse` parts to have the `"function"` role, causing validation errors/hangs.
* **Solution**: Updated `translateAnthropicToGemini` in [anthropic.go](file:///home/rahmandayub/Projects/gemini-router/internal/proxy/anthropic.go) to inspect user message parts and automatically change the Gemini content role to `"function"` if it contains any `FunctionResponse` parts.

#### J. High Availability & Key Failover/Retries
* **Problem**: 
  * Gemini models (especially reasoning models like `gemma-4-31b-it`) can intermittently return `500 Internal error` or `429 Too Many Requests`. Previously, if a key failed, the proxy forwarded the failure directly, causing client crashes.
  * In the initial implementation of the retry loop, a bug existed where failed attempts did not set `resp = nil` when continuing the loop. If all attempts failed with 500/503/429, the loop exited with `resp != nil` (pointing to the closed/failed response), causing the handlers to skip error checks and attempt to stream from a closed body.
* **Solution**: 
  * Implemented an automatic retry loop in both `ServeHTTP` handlers (OpenAI and Anthropic). The proxy will retry the upstream request up to 3 times on temporary/network errors (500, 503, 429), automatically switching to another API key from the pool on each attempt.
  * Fixed the bug by explicitly setting `resp = nil` inside the retry blocks, ensuring that all-attempts-failed scenarios are correctly intercepted and returned as `502 Bad Gateway` errors.

### K. Mid-Stream Raw JSON Error Handling & Client Stop Signals
* **Problem**: 
  * If the Gemini API returned a `500` error mid-stream, it wrote raw JSON error blocks to the connection instead of prefixing them with `data: `. This caused the stream parser to output cryptic `json.Unmarshal` errors line-by-line.
  * When the proxy exited the streaming loop prematurely on these errors, the connection closed abruptly without sending termination signals (`message_stop` for Anthropic, `[DONE]` for OpenAI). This left the client (VS Code Copilot) in a loading spinner state indefinitely, unaware that the stream had failed.
* **Solution**: 
  * Implemented detection for raw JSON objects (lines starting with `{`) in `handleStreamResponse`. If detected, the proxy consumes the rest of the stream, parses the full JSON error payload, logs the exact upstream failure, and gracefully terminates the stream.
  * Fixed the hanging spinner by ensuring the proxy writes an explicit compliant `error` event/chunk to the client (to display the error message) followed by the standard termination packets (`message_stop`/`[DONE]`) before closing the connection, cleanly stopping the client's loader.

### M. Anthropic Thinking Block Rendering Issue & Connection Timeouts
* **Problem**: 
  * If the client (like VS Code Copilot) does not support or request Claude 3.7's `thinking` block type, sending `thinking` blocks at index 0 makes the client ignore them. The client expects the final text response at index 0, resulting in the user seeing `"Sorry, no response was returned."`. Additionally, explicitly setting `thinkingBudget: 0` for models that do not support thinking budget configurations (like `gemma-4-31b-it`) throws a `400 Bad Request`.
  * When thinking blocks are filtered out (when client does not support thinking), the proxy sends nothing to the connection while the model is processing. For long-running reasoning tasks, this 60+ second silence causes the client to timeout the connection, producing a `502 Bad Gateway` (context canceled).
* **Solution**: 
  * Only map `ThinkingConfig` to Gemini if the client explicitly requests thinking via `"thinking": {"type": "enabled", ...}`.
  * Dynamically track if the client supports thinking (`clientSupportsThinking`). If false, we completely filter out and skip all `Thought: true` blocks in both the streaming and non-streaming responses, ensuring the visible text response starts at index 0.
  * When filtering/skipping thought blocks mid-stream, we send Anthropic-compliant `ping` events to the client. This transmits keep-alive bytes on the wire, resetting the client's idle timeout and keeping the connection active while the model works.

### N. Prevention of Indefinite Upstream Connection Hangs
* **Problem**: When Gemini is overloaded, it can sometimes take over 60 seconds to respond with initial HTTP headers for the stream. Without an explicit connection timeout, the request hung inside `UpstreamClient.Do(req)` until the client itself timed out and closed the connection, wasting retry opportunities.
* **Solution**: Configured the global `UpstreamClient` with `ResponseHeaderTimeout: 30 * time.Second` and custom keep-alives. If the upstream server fails to send headers within 30 seconds, the connection immediately times out, allowing the retry loop to quickly pivot to a different API key.

### O. Instant Connection Handshakes & Background Keepalive Tickers
* **Problem**: Even with a 15-second or 30-second connection retry timeout, waiting for multiple key retries and model queue latency (which can easily take 15-20+ seconds for large models like `gemma-4-31b-it`) still feels extremely slow to the client. The client's loading connection remains completely inactive, which can trigger strict client-side idle timeouts or keep the processing indicator hanging with no feedback.
* **Solution**: 
  * Restructured the streaming endpoints in both OpenAI and Anthropic proxy handlers. The proxy now immediately responds with `200 OK` headers and transmits the initial handshake chunk/packet (`message_start` for Anthropic, and the `role: assistant` chunk for OpenAI) at **0 seconds latency**.
  * Spawns a background keepalive ticker that writes keepalive signals (Anthropic `ping` events or OpenAI comment lines `: keepalive\n\n`) to the client connection every 5 seconds.
  * This keeps the connection fully active and resets the client's idle timeout while the main handler executes the retry loops and awaits the upstream Gemini connection behind the scenes.
  * Once the upstream starts responding, the background ticker is cleanly terminated, and the proxy streams the model's actual tokens to the client.

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
