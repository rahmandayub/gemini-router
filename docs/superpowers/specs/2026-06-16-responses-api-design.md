# OpenAI Responses API Support for gemini-router

## Overview

Menambahkan endpoint `/v1/responses` yang menerjemahkan OpenAI Responses API format ke Gemini API format, termasuk reasoning support dan function calling.

## Goals

- Support OpenAI Responses API endpoint (`POST /v1/responses`)
- Support reasoning/thinking models (Gemma, Gemini)
- Support function calling dengan call_id correlation
- Support streaming dengan Responses API event format
- Stateless implementation (client manages state via previous_response_id)

## Architecture

```
Client (VS Code Copilot, dll)
    ↓
POST /v1/responses
    ↓
ResponsesHandler
    ↓
translateResponsesToGemini() → GeminiRequest
    ↓
Gemini API (generativelanguage.googleapis.com)
    ↓
GeminiResponse
    ↓
translateGeminiToResponse() → Response
    ↓
OpenAI Responses API Response Format
```

## Request Translation (OpenAI Responses → Gemini)

### Input Mapping

| Responses API | Gemini API |
|---------------|------------|
| `instructions` | `system_instruction.parts[0].text` |
| `input` (string) | `contents[{role:"user", parts:[{text}]}]` |
| `input[{role:"user", content:"text"}]` | `contents[{role:"user", parts:[{text}]}]` |
| `input[{role:"assistant", content:"text"}]` | `contents[{role:"model", parts:[{text}]}]` |
| `input[{type:"function_call_output", call_id, output}]` | `contents[{role:"user", parts:[{functionResponse:{name, response}}]}]` |
| `tools[{type:"function", name, parameters}]` | `tools[{functionDeclarations:[{name, parameters}]}]` |
| `temperature` | `generationConfig.temperature` |
| `max_output_tokens` | `generationConfig.maxOutputTokens` |
| `tool_choice` | `toolConfig` |

### Tool Definition Mapping

**OpenAI Responses:**
```json
{
  "type": "function",
  "name": "get_weather",
  "description": "Get weather",
  "parameters": {
    "type": "object",
    "properties": {"location": {"type": "string"}},
    "required": ["location"]
  },
  "strict": true
}
```

**Gemini:**
```json
{
  "functionDeclarations": [{
    "name": "get_weather",
    "description": "Get weather",
    "parameters": {
      "type": "object",
      "properties": {"location": {"type": "string"}},
      "required": ["location"]
    }
  }]
}
```

## Response Translation (Gemini → OpenAI Responses)

### Text Response

**Gemini:**
```json
{
  "candidates": [{
    "content": {
      "parts": [{"text": "Hello!"}],
      "role": "model"
    },
    "finishReason": "STOP"
  }],
  "usageMetadata": {
    "promptTokenCount": 10,
    "candidatesTokenCount": 5,
    "totalTokenCount": 15
  }
}
```

**OpenAI Responses:**
```json
{
  "id": "resp_abc123",
  "object": "response",
  "status": "completed",
  "created_at": 1741476777,
  "completed_at": 1741476778,
  "model": "gemini-2.5-flash",
  "output": [{
    "type": "message",
    "id": "msg_xyz789",
    "status": "completed",
    "role": "assistant",
    "content": [{
      "type": "output_text",
      "text": "Hello!",
      "annotations": []
    }]
  }],
  "usage": {
    "input_tokens": 10,
    "output_tokens": 5,
    "total_tokens": 15,
    "input_tokens_details": {"cached_tokens": 0},
    "output_tokens_details": {"reasoning_tokens": 0}
  }
}
```

### Function Call Response

**Gemini:**
```json
{
  "candidates": [{
    "content": {
      "parts": [{
        "functionCall": {
          "name": "get_weather",
          "args": {"location": "NYC"}
        }
      }],
      "role": "model"
    },
    "finishReason": "STOP"
  }]
}
```

**OpenAI Responses:**
```json
{
  "id": "resp_abc123",
  "object": "response",
  "status": "completed",
  "created_at": 1741476777,
  "model": "gemini-2.5-flash",
  "output": [{
    "type": "function_call",
    "id": "fc_123",
    "call_id": "call_abc123",
    "name": "get_weather",
    "arguments": "{\"location\": \"NYC\"}",
    "status": "completed"
  }],
  "usage": {...}
}
```

### Reasoning Response (Gemma/Gemini thinking models)

**Gemini:**
```json
{
  "candidates": [{
    "content": {
      "parts": [
        {"text": "thinking process...", "thought": true},
        {"text": "final answer"}
      ],
      "role": "model"
    }
  }]
}
```

**OpenAI Responses:**
```json
{
  "output": [
    {
      "type": "reasoning",
      "id": "rs_123",
      "summary": [{"type": "summary_text", "text": "thinking process..."}],
      "content": [{"type": "reasoning_text", "text": "thinking process..."}],
      "status": "completed"
    },
    {
      "type": "message",
      "id": "msg_123",
      "status": "completed",
      "role": "assistant",
      "content": [{"type": "output_text", "text": "final answer", "annotations": []}]
    }
  ]
}
```

## Streaming Events Translation

| Responses API Event | Description |
|---------------------|-------------|
| `response.created` | First event with response object |
| `response.output_item.added` | When content starts |
| `response.content_part.added` | When content part starts |
| `response.output_text.delta` | Each text chunk |
| `response.function_call_arguments.delta` | Function call args delta |
| `response.function_call_arguments.done` | Function call args complete |
| `response.content_part.done` | Content part complete |
| `response.output_item.done` | Item complete |
| `response.completed` | Final event with complete response |

**Streaming Format:**
```
data: {"type":"response.created","response":{...},"sequence_number":0}

data: {"type":"response.output_item.added","output_index":0,"item":{"id":"msg_123","type":"message","status":"in_progress","role":"assistant","content":[]},"sequence_number":1}

data: {"type":"response.content_part.added","item_id":"msg_123","output_index":0,"content_index":0,"part":{"type":"output_text","text":""},"sequence_number":2}

data: {"type":"response.output_text.delta","item_id":"msg_123","output_index":0,"content_index":0,"delta":"Hello","sequence_number":3}

data: {"type":"response.content_part.done","item_id":"msg_123","output_index":0,"content_index":0,"part":{"type":"output_text","text":"Hello!","annotations":[]},"sequence_number":4}

data: {"type":"response.output_item.done","output_index":0,"item":{"id":"msg_123","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"Hello!","annotations":[]}]},"sequence_number":5}

data: {"type":"response.completed","response":{...},"sequence_number":6}

data: [DONE]
```

## File Structure

```
internal/proxy/
├── responses.go          # New: ResponsesHandler + types + translation
├── responses_test.go     # New: Tests
└── router.go             # Modified: Add /v1/responses route
```

## Type Definitions

```go
// Request
type ResponsesRequest struct {
    Model              string           `json:"model"`
    Input              json.RawMessage  `json:"input"` // string or []InputItem
    Instructions       string           `json:"instructions,omitempty"`
    Tools              []ResponsesTool  `json:"tools,omitempty"`
    ToolChoice         interface{}      `json:"tool_choice,omitempty"`
    Temperature        *float64         `json:"temperature,omitempty"`
    MaxOutputTokens    *int             `json:"max_output_tokens,omitempty"`
    Stream             bool             `json:"stream,omitempty"`
    Store              *bool            `json:"store,omitempty"`
    ParallelToolCalls  *bool            `json:"parallel_tool_calls,omitempty"`
    PreviousResponseID string           `json:"previous_response_id,omitempty"`
    Include            []string         `json:"include,omitempty"`
}

// Output Items
type ResponseOutputItem struct {
    Type       string      `json:"type"`
    ID         string      `json:"id,omitempty"`
    Status     string      `json:"status,omitempty"`
    Role       string      `json:"role,omitempty"`
    Content    interface{} `json:"content,omitempty"`
    Summary    interface{} `json:"summary,omitempty"`
    CallID     string      `json:"call_id,omitempty"`
    Name       string      `json:"name,omitempty"`
    Arguments  string      `json:"arguments,omitempty"`
}
```

## Translation Functions

```
translateResponsesToGemini(req *ResponsesRequest) (*GeminiRequest, error)
  - Convert input items to Gemini contents
  - Convert tools to Gemini toolDeclarations
  - Handle function_call_output as functionResponse

translateGeminiToResponse(geminiResp *GeminiResponse, model string) *Response
  - Convert candidates to output items
  - Handle thought parts as reasoning items
  - Map finishReason to status

generateResponseID() string
  - Generate "resp_" prefixed ID

generateItemID(prefix string) string
  - Generate "msg_", "fc_", "rs_" prefixed IDs
```

## Edge Cases & Error Handling

### 1. Input Type Ambiguity

`input` field bisa berupa string atau array. Discriminator:
```go
func parseInput(raw json.RawMessage) (string, []InputItem, error) {
    // Coba parse sebagai string dulu
    var s string
    if err := json.Unmarshal(raw, &s); err == nil {
        return s, nil, nil
    }
    // Fallback ke array
    var items []InputItem
    if err := json.Unmarshal(raw, &items); err == nil {
        return "", items, nil
    }
    return "", nil, fmt.Errorf("invalid input format")
}
```

### 2. function_call_output Name Resolution

OpenAI spec mewajibkan `name` di `function_call_output`. Client harus kirim:
```json
{
  "type": "function_call_output",
  "call_id": "call_abc123",
  "name": "get_weather",
  "output": "{\"temp\": 72}"
}
```

### 3. Multi-candidate Handling

Gunakan `candidates[0]` saja. Jika `candidates` kosong:
```json
{
  "error": {
    "message": "No candidates returned (possibly safety filter)",
    "type": "server_error",
    "code": "empty_candidates"
  }
}
```

### 4. finishReason Mapping

| Gemini `finishReason` | Responses API `status` | Notes |
|---|---|---|
| `STOP` | `completed` | Normal completion |
| `MAX_TOKENS` | `incomplete` | `incomplete_details.reason = "max_output_tokens"` |
| `SAFETY` | `incomplete` | `incomplete_details.reason = "content_filter"` |
| `RECITATION` | `incomplete` | `incomplete_details.reason = "content_filter"` |
| `OTHER` | `incomplete` | `incomplete_details.reason = "content_filter"` |
| Empty/missing | `completed` | Default jika tidak ada finishReason |

### 5. Streaming Error Handling

Jika error terjadi setelah `response.created`:
```
data: {"type":"response.error","error":{"message":"Upstream error","type":"server_error","code":"upstream_error"},"sequence_number":N}
```

### 6. previous_response_id Behavior

Field di--ignore (stateless implementation). Client manage state sendiri dengan pass output items kembali ke input.

### 7. tool_choice Mapping

| Responses API | Gemini `toolConfig.functionCallingConfig.mode` |
|---|---|
| `"auto"` | `AUTO` |
| `"none"` | `NONE` |
| `"required"` | `ANY` |
| `{type: "function", name: "..."}` | `ANY` + `allowedFunctionNames: ["..."]` |

### 8. strict Field Handling

Gemini tidak punya equivalent. Field di-ignore saat translate.

### 9. Error Passthrough

Gemini errors di-transform ke Responses API error format:
```json
{
  "error": {
    "message": "Rate limit exceeded",
    "type": "rate_limit_error",
    "code": "rate_limit_exceeded"
  }
}
```

## Testing

1. Unit tests untuk translation functions
2. Test text response translation
3. Test function call translation
4. Test reasoning response translation
5. Test streaming events
6. Test `input` sebagai plain string (bukan array)
7. Test error cases: empty candidates, safety block, malformed input
8. Test `tool_choice` mapping
9. Test `function_call_output` dengan name field
10. Test `finishReason` mapping untuk semua cases
11. Integration test dengan actual Gemini API
