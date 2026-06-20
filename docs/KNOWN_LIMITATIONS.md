# Known Limitations & Accepted Technical Debt

Documented during the Translation Layer Compatibility Audit (2026-06-20).
Updated after addressing audit findings (2026-06-20).

---

## Fixed Issues (2026-06-20)

The following issues from the original audit have been addressed:

### Reasoning Item Duplication in Responses Streaming (Bug #9)
- **Fixed in**: `responses.go`
- **Change**: Added `reasoningCompleted` flag to track whether reasoning was ever
  streamed. The final `response.completed` output now always includes reasoning
  items if reasoning was present, even if the reasoning block was closed before
  the finish reason arrived.

### Output Index Edge Case in Responses Streaming (Bug #3)
- **Fixed in**: `responses.go`
- **Change**: Added proper item transition handling. When a function_call arrives
  while a text item is in progress, the text item is properly closed (via
  `content_part.done` and `output_item.done` events) before the function_call
  item starts. This prevents output index collisions between text and function
  call items.

### Audio/Video/Document Content Support (OpenAI Path)
- **Fixed in**: `openai.go`
- **Change**: Added support for `input_audio` content type (OpenAI's audio format
  with base64 data and format field). Also added support for `audio_url`,
  `video_url`, `document_url`, and `file` content types that use the `image_url`
  field structure for URL-based content. All non-text content is forwarded to
  Gemini as `InlineData` with the appropriate MIME type.

### Audio Content Support (Anthropic Path)
- **Fixed in**: `anthropic.go`
- **Change**: Added `audio` content block type handling in `parseAnthropicContent`.
  Supports both base64 source (direct InlineData) and URL source (fetch + encode).

### Metadata Passthrough (Responses API)
- **Fixed in**: `responses.go`
- **Change**: Added `Metadata` field to `ResponsesRequest` and forward it to the
  `Response` object in both streaming and non-streaming paths.

### New Tests
- Added 10 new test cases covering audio content, metadata passthrough, and mixed
  content types. Total test count: 122 (up from 78).

---

## Remaining Accepted Limitations

### 1. Tool Call ID Encoding Is Fragile (Bug #1, #4, #8)

**Files**: `openai.go:833-834`, `openai.go:958-973`

**What**: Tool call IDs are constructed as `call_<name>_<requestID>_<index>` during
response translation, then reverse-engineered back to function names when translating
tool result messages. The extraction relies on stripping the `call_` prefix and a
trailing `_<digits>` suffix via `strings.LastIndex("_")`.

**Why not fixed**: The current approach works correctly for the existing ID format
because `requestID` is a hex string (contains a-f characters) that will never be
pure digits, so the `isDigits` check always succeeds for the rightmost segment.
Refactoring to an explicit name map would require thread-safe state management
across request/response cycles, adding complexity for no practical gain. The
Anthropic handler already uses a `toolUseIDToName` map, but that works because
Anthropic protocol naturally provides both the ID and name in the same request —
OpenAI does not.

**Risk**: Low. If the ID format ever changes, this could break. The `isDigits`
guard provides sufficient safety for the current hex-based request IDs.

**When to revisit**: If tool call IDs are ever exposed to external consumers who
might construct their own, or if the ID generation format changes.

---

### 2. `function_call_arguments.done` Delta Contains Full Arguments (Bug #6)

**File**: `responses.go:1080-1092`

**What**: The `response.function_call_arguments.done` event sets `Delta` to the
full arguments string. The OpenAI Responses API spec is ambiguous on whether `delta`
should carry the full arguments on the done event or be empty.

**Why not fixed**: OpenAI SDK clients typically accumulate deltas from
`function_call_arguments.delta` events and treat `done` as a signal. The full
arguments in `delta` on the done event is harmless — clients that accumulate will
see the same final result. Clients that only look at `delta` on `done` will also
work correctly. No known SDK breaks on this behavior.

**Risk**: Negligible. Behavior is compatible with all known client implementations.

---

### 3. `top_k` Unavailable Through OpenAI Interface (Bug #10)

**File**: `openai.go` (OpenAIRequest struct)

**What**: The OpenAI Chat Completions API does not expose a `top_k` parameter.
Gemini supports `top_k` for controlling nucleus sampling. Users of the OpenAI
interface cannot control this parameter.

**Why not fixed**: This is an inherent protocol limitation, not a bug. OpenAI's
API design does not include `top_k` — adding a non-standard extension would break
protocol compatibility and confuse clients that validate against the OpenAI schema.

**Impact**: Users who need `top_k` control must use the native Gemini endpoint
(`/v1beta/`) or the Anthropic endpoint (which does support `top_k`).

---

### 4. `n > 1` Response Handling Uses Only First Candidate (All Paths)

**Files**: `openai.go:1150`, `anthropic.go:1152`, `responses.go:488`

**What**: When `n > 1` (multiple candidates requested), the translation layer
maps `n` to Gemini's `candidateCount`, but only the first candidate is translated
back to the target protocol response.

**Why not fixed**: The target protocols (OpenAI, Anthropic) support `n > 1` in
theory, but most clients only use `n = 1`. Translating all candidates would
require restructuring the response translation to emit multiple choices/items,
which significantly increases complexity. The first candidate is the most likely
response and matches user expectations.

**Risk**: Low. Clients requesting `n > 1` will receive only one choice. This is
documented behavior for translation proxies. Most real-world usage is `n = 1`.

**When to revisit**: If users request multi-candidate support.

---

### 5. `strict` Mode on JSON Schema Not Forwarded to Gemini

**Files**: `openai.go:55-59`, `responses.go:53-58`

**What**: OpenAI's `response_format.json_schema.strict` parameter enforces that
the model's output exactly matches the provided schema. Gemini has a different
mechanism for schema enforcement (via `responseSchema`), and there is no direct
equivalent of `strict` mode.

**Why not fixed**: Gemini's `responseSchema` already provides schema-constrained
output when paired with `responseMimeType: "application/json"`. The `strict`
parameter from OpenAI is parsed and stored but not forwarded because Gemini does
not accept it. Forwarding it would cause an API error.

**Risk**: Low. Gemini enforces schema via `responseSchema` which is already
forwarded. The practical difference between OpenAI `strict` and Gemini schema
enforcement is minimal for most use cases.

---

### 6. Cache Token Accounting Hardcoded to Zero (Anthropic Path)

**File**: `anthropic.go:1206-1215`

**What**: `cache_creation_input_tokens` and `cache_read_input_tokens` are hardcoded
to `0` in the Anthropic response usage. Gemini's `usageMetadata` does not provide
cache-specific token counts.

**Why not fixed**: Gemini's `usageMetadata` only provides `promptTokenCount`,
`candidatesTokenCount`, and `totalTokenCount` — no cache breakdown. There is no
source data to map from. Returning `0` is correct and non-misleading.

**Risk**: None. Clients that check cache tokens will see `0`, which is accurate —
Gemini does not expose this information.

---

### 7. `stop_sequence` Not Mapped in Anthropic Response

**File**: `anthropic.go:1155`

**What**: When a stop sequence is triggered, Anthropic's API returns the matched
`stop_sequence` in the response. Gemini does not provide this information in its
response.

**Why not fixed**: No source data. Gemini's `finishReason` indicates that stopping
occurred (`STOP`) but does not specify which stop sequence was matched. Returning
`nil` is the only correct behavior.

**Risk**: None. Clients that need stop sequence information cannot get it through
this translation layer due to upstream limitations.

---

### 8. Stateless Mode Limitations (Responses API)

**File**: `responses.go`

**What**: The following Responses API features are not implemented due to the
stateless proxy design:
- `previous_response_id` — logged and ignored
- `store` — parsed but not used
- `conversation` object — not implemented
- `include` parameter — parsed but not forwarded
- `background` mode — not forwarded
- `truncation` — not forwarded

**Why not fixed**: These features require server-side state management (conversation
history, stored responses, background processing). The proxy is designed to be
stateless for simplicity and horizontal scalability.

**Risk**: Low. Clients using these features will receive responses but without the
stateful behavior they expect. This is documented behavior for a stateless proxy.

**When to revisit**: If stateful proxy mode is implemented.

---

### 9. `logprobs` / `refusal` / `moderation` Not Forwarded (OpenAI Path)

**File**: `openai.go`

**What**: OpenAI's `logprobs`, `refusal`, and `moderation` response fields are
not forwarded because Gemini does not provide equivalent data.

**Why not fixed**: No source data. Gemini's response format does not include log
probabilities, refusal messages, or moderation results.

**Risk**: None. Clients that check these fields will receive null/empty values,
which is correct behavior.

---

### 10. `reasoning_tokens` / `cached_tokens` Hardcoded to Zero (Responses Path)

**File**: `responses.go`

**What**: `usage.output_tokens_details.reasoning_tokens` and
`usage.input_tokens_details.cached_tokens` are hardcoded to `0` in the Responses
API usage. Gemini's `usageMetadata` does not provide these breakdowns.

**Why not fixed**: No source data. Gemini only provides aggregate token counts.

**Risk**: None. Clients that check these fields will see `0`, which is accurate.
