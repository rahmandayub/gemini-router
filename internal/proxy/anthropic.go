package proxy

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/rahmandayub/gemini-router/internal/key"
)

// AnthropicHandler handles Anthropic Messages API requests and translates them to Gemini format.
type AnthropicHandler struct {
	geminiBaseURL string
	geminiKeys    *key.Pool
}

func NewAnthropicHandler(geminiBaseURL string, geminiKeys *key.Pool) *AnthropicHandler {
	return &AnthropicHandler{
		geminiBaseURL: strings.TrimRight(geminiBaseURL, "/"),
		geminiKeys:    geminiKeys,
	}
}

// Anthropic API types

type AnthropicRequest struct {
	Model         string               `json:"model"`
	Messages      []AnthropicMessage   `json:"messages"`
	System        json.RawMessage      `json:"system,omitempty"`
	MaxTokens     int                  `json:"max_tokens"`
	Temperature   *float64             `json:"temperature,omitempty"`
	TopP          *float64             `json:"top_p,omitempty"`
	TopK          *int                 `json:"top_k,omitempty"`
	StopSequences []string             `json:"stop_sequences,omitempty"`
	Stream        bool                 `json:"stream,omitempty"`
	Tools         []AnthropicTool      `json:"tools,omitempty"`
	ToolChoice    *AnthropicToolChoice `json:"tool_choice,omitempty"`
	Thinking      *AnthropicThinking   `json:"thinking,omitempty"`
}

type AnthropicMessage struct {
	Role    string           `json:"role"`
	Content AnthropicContent `json:"content"`
}

type AnthropicContent interface{}

type AnthropicTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type AnthropicToolUseBlock struct {
	Type  string                 `json:"type"`
	ID    string                 `json:"id"`
	Name  string                 `json:"name"`
	Input map[string]interface{} `json:"input"`
}

type AnthropicToolResultBlock struct {
	Type      string      `json:"type"`
	ToolUseID string      `json:"tool_use_id"`
	Content   interface{} `json:"content,omitempty"`
	IsError   bool        `json:"is_error,omitempty"`
}

type AnthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type AnthropicToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

type AnthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens *int   `json:"budget_tokens,omitempty"`
}

type AnthropicResponse struct {
	ID           string               `json:"id"`
	Type         string               `json:"type"`
	Role         string               `json:"role"`
	Content      []AnthropicRespBlock `json:"content"`
	Model        string               `json:"model"`
	StopReason   string               `json:"stop_reason,omitempty"`
	StopSequence *string              `json:"stop_sequence,omitempty"`
	Usage        *AnthropicUsage      `json:"usage"`
}

type AnthropicRespBlock interface{}

type AnthropicRespTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type AnthropicRespThinkingBlock struct {
	Type     string `json:"type"`
	Thinking string `json:"thinking"`
}

type AnthropicRespToolUseBlock struct {
	Type  string                 `json:"type"`
	ID    string                 `json:"id"`
	Name  string                 `json:"name"`
	Input map[string]interface{} `json:"input"`
}

type AnthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// Streaming types

type AnthropicStreamEvent struct {
	Type    string             `json:"type"`
	Index   int                `json:"index,omitempty"`
	Delta   interface{}        `json:"delta,omitempty"`
	Message *AnthropicResponse `json:"message,omitempty"`
}

type AnthropicStreamMessageStart struct {
	Type    string             `json:"type"`
	Message *AnthropicResponse `json:"message"`
}

type AnthropicStreamContentBlockStart struct {
	Type  string      `json:"type"`
	Index int         `json:"index"`
	Block interface{} `json:"content_block"`
}

type AnthropicStreamContentBlockDelta struct {
	Type  string      `json:"type"`
	Index int         `json:"index"`
	Delta interface{} `json:"delta"`
}

type AnthropicStreamTextDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type AnthropicStreamInputJSONDelta struct {
	Type        string `json:"type"`
	PartialJSON string `json:"partial_json"`
}

type AnthropicStreamThinkingDelta struct {
	Type     string `json:"type"`
	Thinking string `json:"thinking"`
}

type AnthropicStreamMessageDelta struct {
	Type  string                 `json:"type"`
	Delta *AnthropicMessageDelta `json:"delta"`
	Usage *AnthropicUsage        `json:"usage,omitempty"`
}

type AnthropicMessageDelta struct {
	StopReason   string  `json:"stop_reason,omitempty"`
	StopSequence *string `json:"stop_sequence,omitempty"`
}

type AnthropicStreamMessageStop struct {
	Type string `json:"type"`
}

func generateAnthropicID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return fmt.Sprintf("%s%d", AnthropicIDPrefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s%x", AnthropicIDPrefix, b)
}

func generateToolUseID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return fmt.Sprintf("%s%d", AnthropicToolPrefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s%x", AnthropicToolPrefix, b)
}

func (h *AnthropicHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var anthropicReq AnthropicRequest
	if err := json.Unmarshal(body, &anthropicReq); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	geminiReq, err := translateAnthropicToGemini(&anthropicReq)
	if err != nil {
		log.Printf("[proxy/anthropic] translation error: %v", err)
		http.Error(w, fmt.Sprintf("translation error: %v", err), http.StatusBadRequest)
		return
	}

	geminiBody, err := json.Marshal(geminiReq)
	if err != nil {
		log.Printf("[proxy/anthropic] marshal error: %v", err)
		http.Error(w, "failed to marshal gemini request", http.StatusInternalServerError)
		return
	}

	endpoint := "generateContent"
	if anthropicReq.Stream {
		endpoint = "streamGenerateContent?alt=sse"
	}

	upstreamURL := fmt.Sprintf("%s/v1beta/models/%s:%s", h.geminiBaseURL, anthropicReq.Model, endpoint)

	msgID := generateAnthropicID()
	clientSupportsThinking := anthropicReq.Thinking != nil && anthropicReq.Thinking.Type == "enabled"

	var resp *http.Response
	var lastErr error
	maxRetries := 3

	if anthropicReq.Stream {
		// Write headers early to establish connection
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("x-request-id", msgID)
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if ok {
			// Send message_start immediately
			msgStart := AnthropicStreamMessageStart{
				Type: "message_start",
				Message: &AnthropicResponse{
					ID:      msgID,
					Type:    "message",
					Role:    "assistant",
					Model:   anthropicReq.Model,
					Content: []AnthropicRespBlock{},
					Usage: &AnthropicUsage{
						InputTokens:              0,
						OutputTokens:             0,
						CacheCreationInputTokens: 0,
						CacheReadInputTokens:     0,
					},
				},
			}
			eventData, _ := json.Marshal(msgStart)
			WriteSSEEvent(w, "message_start", eventData)

			// Send initial ping keepalive
			pingEvent := map[string]interface{}{"type": "ping"}
			pingData, _ := json.Marshal(pingEvent)
			WriteSSEEvent(w, "ping", pingData)
			flusher.Flush()
		}

		// Start keepalive ticker
		stopKeepAlive := make(chan struct{})
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					pingEvent := map[string]interface{}{"type": "ping"}
					pingData, _ := json.Marshal(pingEvent)
					WriteSSEEvent(w, "ping", pingData)
				case <-stopKeepAlive:
					return
				case <-r.Context().Done():
					return
				}
			}
		}()

		// Run retry loop
		for attempt := 0; attempt < maxRetries; attempt++ {
			apiKey := h.geminiKeys.Next()

			var req *http.Request
			req, err = http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(geminiBody))
			if err != nil {
				log.Printf("[proxy/anthropic] failed to create upstream request: %v", err)
				close(stopKeepAlive)
				return
			}

			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("x-goog-api-key", apiKey)

			log.Printf("[proxy/anthropic] POST /v1/messages (attempt %d) -> model=%s stream=true", attempt+1, anthropicReq.Model)

			resp, err = UpstreamClient.Do(req)
			if err != nil {
				lastErr = err
				log.Printf("[proxy/anthropic] request failed (attempt %d): %v", attempt+1, err)
				if r.Context().Err() != nil {
					break
				}
				time.Sleep(50 * time.Millisecond)
				continue
			}

			if resp.StatusCode == http.StatusInternalServerError || resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusTooManyRequests {
				bodyBytes, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				log.Printf("[proxy/anthropic] upstream returned status %d (attempt %d): %s", resp.StatusCode, attempt+1, string(bodyBytes))
				lastErr = fmt.Errorf("upstream status %d: %s", resp.StatusCode, string(bodyBytes))
				resp = nil
				time.Sleep(50 * time.Millisecond)
				continue
			}

			break
		}

		close(stopKeepAlive)

		if resp == nil {
			log.Printf("[proxy/anthropic] all retries failed. Last error: %v", lastErr)

			// Send error as assistant text content to render in chat
			errText := fmt.Sprintf("\n\n[Proxy Error: failed to forward request to upstream: %v]", lastErr)
			WriteSSEEvent(w, "content_block_start", []byte(`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`))

			deltaEvent := AnthropicStreamContentBlockDelta{
				Type:  "content_block_delta",
				Index: 0,
				Delta: &AnthropicStreamTextDelta{
					Type: "text_delta",
					Text: errText,
				},
			}
			deltaBytes, _ := json.Marshal(deltaEvent)
			WriteSSEEvent(w, "content_block_delta", deltaBytes)
			WriteSSEEvent(w, "content_block_stop", []byte(`{"type":"content_block_stop","index":0}`))

			errorEvent := map[string]interface{}{
				"type": "error",
				"error": map[string]interface{}{
					"type":    "api_error",
					"message": fmt.Sprintf("failed to forward request to upstream: %v", lastErr),
				},
			}
			eventData, _ := json.Marshal(errorEvent)
			WriteSSEEvent(w, "error", eventData)
			WriteSSEEvent(w, "message_stop", []byte(`{"type":"message_stop"}`))
			return
		}
		defer resp.Body.Close()

		h.handleStreamResponse(w, resp, anthropicReq.Model, msgID, clientSupportsThinking, true)
	} else {
		// Non-stream flow (normal retry loop)
		for attempt := 0; attempt < maxRetries; attempt++ {
			apiKey := h.geminiKeys.Next()

			var req *http.Request
			req, err = http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(geminiBody))
			if err != nil {
				log.Printf("[proxy/anthropic] failed to create upstream request: %v", err)
				http.Error(w, "failed to create upstream request", http.StatusInternalServerError)
				return
			}

			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("x-goog-api-key", apiKey)

			log.Printf("[proxy/anthropic] POST /v1/messages (attempt %d) -> model=%s stream=false", attempt+1, anthropicReq.Model)

			resp, err = UpstreamClient.Do(req)
			if err != nil {
				lastErr = err
				log.Printf("[proxy/anthropic] request failed (attempt %d): %v", attempt+1, err)
				if r.Context().Err() != nil {
					break
				}
				time.Sleep(50 * time.Millisecond)
				continue
			}

			if resp.StatusCode == http.StatusInternalServerError || resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusTooManyRequests {
				bodyBytes, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				log.Printf("[proxy/anthropic] upstream returned status %d (attempt %d): %s", resp.StatusCode, attempt+1, string(bodyBytes))
				lastErr = fmt.Errorf("upstream status %d: %s", resp.StatusCode, string(bodyBytes))
				resp = nil
				time.Sleep(50 * time.Millisecond)
				continue
			}

			break
		}

		if resp == nil {
			log.Printf("[proxy/anthropic] all retries failed. Last error: %v", lastErr)
			http.Error(w, fmt.Sprintf("failed to forward request to upstream: %v", lastErr), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		h.handleNonStreamResponse(w, resp, anthropicReq.Model, msgID, clientSupportsThinking)
	}
}

func (h *AnthropicHandler) handleNonStreamResponse(w http.ResponseWriter, resp *http.Response, model string, msgID string, clientSupportsThinking bool) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "failed to read upstream response", http.StatusBadGateway)
		return
	}

	if resp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		errObj := translateGeminiErrorToAnthropic(body)
		errBytes, _ := json.Marshal(errObj)
		w.Write(errBytes)
		return
	}

	var geminiResp GeminiResponse
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		http.Error(w, "failed to parse upstream response", http.StatusBadGateway)
		return
	}

	anthropicResp := translateFromGeminiToAnthropic(&geminiResp, model, msgID, clientSupportsThinking)
	respBody, err := json.Marshal(anthropicResp)
	if err != nil {
		log.Printf("[proxy/anthropic] marshal response error: %v", err)
		http.Error(w, "failed to marshal response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("x-request-id", msgID)
	w.Write(respBody)
}

func (h *AnthropicHandler) handleStreamResponse(w http.ResponseWriter, resp *http.Response, model string, msgID string, clientSupportsThinking bool, headersWritten bool) {
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[proxy/stream] Anthropic upstream returned non-OK status: %d, body: %s", resp.StatusCode, string(body))
		if !headersWritten {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(resp.StatusCode)
			errObj := translateGeminiErrorToAnthropic(body)
			errBytes, _ := json.Marshal(errObj)
			w.Write(errBytes)
			return
		}

		// Send error as assistant text content to render in chat
		errText := fmt.Sprintf("\n\n[Proxy Error: upstream returned error: %s]", string(body))
		WriteSSEEvent(w, "content_block_start", []byte(`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`))

		deltaEvent := AnthropicStreamContentBlockDelta{
			Type:  "content_block_delta",
			Index: 0,
			Delta: &AnthropicStreamTextDelta{
				Type: "text_delta",
				Text: errText,
			},
		}
		deltaBytes, _ := json.Marshal(deltaEvent)
		WriteSSEEvent(w, "content_block_delta", deltaBytes)
		WriteSSEEvent(w, "content_block_stop", []byte(`{"type":"content_block_stop","index":0}`))

		errorEvent := map[string]interface{}{
			"type": "error",
			"error": map[string]interface{}{
				"type":    "api_error",
				"message": string(body),
			},
		}
		eventData, _ := json.Marshal(errorEvent)
		WriteSSEEvent(w, "error", eventData)
		if headersWritten {
			WriteSSEEvent(w, "message_stop", []byte(`{"type":"message_stop"}`))
		}
		return
	}

	if !headersWritten {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("x-request-id", msgID)
		w.WriteHeader(http.StatusOK)
	}

	reader := bufio.NewReader(resp.Body)
	sentMessageStart := headersWritten
	sentMessageStop := false
	sentAny := headersWritten
	blockIndex := 0
	currentBlockType := ""

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				log.Printf("[proxy/anthropic] error reading stream: %v", err)
			}
			break
		}

		line = strings.TrimRight(line, "\r\n")

		if strings.HasPrefix(strings.TrimSpace(line), "{") {
			rest, _ := io.ReadAll(reader)
			fullJSON := line + "\n" + string(rest)
			var errResp struct {
				Error struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
					Status  string `json:"status"`
				} `json:"error"`
			}
			errMsg := "Upstream error encountered mid-stream"
			if err := json.Unmarshal([]byte(fullJSON), &errResp); err == nil && errResp.Error.Message != "" {
				log.Printf("[proxy/anthropic] upstream returned mid-stream error: %d - %s", errResp.Error.Code, errResp.Error.Message)
				errMsg = errResp.Error.Message
			} else {
				log.Printf("[proxy/anthropic] upstream returned raw JSON mid-stream: %s", fullJSON)
			}

			// Ensure message_start is sent before sending the error event
			if !sentMessageStart {
				msgStart := AnthropicStreamMessageStart{
					Type: "message_start",
					Message: &AnthropicResponse{
						ID:      msgID,
						Type:    "message",
						Role:    "assistant",
						Model:   model,
						Content: []AnthropicRespBlock{},
						Usage: &AnthropicUsage{
							InputTokens:              0,
							OutputTokens:             0,
							CacheCreationInputTokens: 0,
							CacheReadInputTokens:     0,
						},
					},
				}
				eventData, _ := json.Marshal(msgStart)
				WriteSSEEvent(w, "message_start", eventData)
				sentMessageStart = true
			}

			// Close previous block if any
			if currentBlockType != "" {
				WriteSSEEvent(w, "content_block_stop", []byte(fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, blockIndex)))
				blockIndex++
			}

			// Send error as assistant text content to render in chat
			errText := fmt.Sprintf("\n\n[Proxy Error: %s]", errMsg)
			startEvent := AnthropicStreamContentBlockStart{
				Type:  "content_block_start",
				Index: blockIndex,
				Block: &AnthropicRespTextBlock{
					Type: "text",
				},
			}
			eventData, _ := json.Marshal(startEvent)
			WriteSSEEvent(w, "content_block_start", eventData)

			deltaEvent := AnthropicStreamContentBlockDelta{
				Type:  "content_block_delta",
				Index: blockIndex,
				Delta: &AnthropicStreamTextDelta{
					Type: "text_delta",
					Text: errText,
				},
			}
			deltaBytes, _ := json.Marshal(deltaEvent)
			WriteSSEEvent(w, "content_block_delta", deltaBytes)
			WriteSSEEvent(w, "content_block_stop", []byte(fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, blockIndex)))

			// Send error event
			errorEvent := map[string]interface{}{
				"type": "error",
				"error": map[string]interface{}{
					"type":    "api_error",
					"message": errMsg,
				},
			}
			eventData, _ = json.Marshal(errorEvent)
			WriteSSEEvent(w, "error", eventData)

			break
		}

		data := ""
		if strings.HasPrefix(line, "data: ") {
			data = strings.TrimPrefix(line, "data: ")
		} else if line == "" || strings.HasPrefix(line, ":") {
			continue
		} else {
			data = line
		}

		if data == "[DONE]" {
			break
		}

		if data == "" {
			continue
		}

		var geminiResp GeminiResponse
		if err := json.Unmarshal([]byte(data), &geminiResp); err != nil {
			log.Printf("[proxy/anthropic] stream parse error: %v raw=%s", err, data)
			continue
		}

		if len(geminiResp.Candidates) == 0 {
			continue
		}

		candidate := geminiResp.Candidates[0]

		// Send message_start if not sent yet
		if !sentMessageStart {
			msgStart := AnthropicStreamMessageStart{
				Type: "message_start",
				Message: &AnthropicResponse{
					ID:      msgID,
					Type:    "message",
					Role:    "assistant",
					Model:   model,
					Content: []AnthropicRespBlock{},
					Usage: &AnthropicUsage{
						InputTokens:              0,
						OutputTokens:             0,
						CacheCreationInputTokens: 0,
						CacheReadInputTokens:     0,
					},
				},
			}
			if geminiResp.UsageMetadata != nil {
				msgStart.Message.Usage.InputTokens = geminiResp.UsageMetadata.PromptTokenCount
			}
			eventData, _ := json.Marshal(msgStart)
			WriteSSEEvent(w, "message_start", eventData)
			sentMessageStart = true

			// Send ping keepalive after message_start
			pingEvent := map[string]interface{}{"type": "ping"}
			pingData, _ := json.Marshal(pingEvent)
			WriteSSEEvent(w, "ping", pingData)
		}

		// Process content parts
		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				if part.Thought {
					if !clientSupportsThinking {
						// Send a ping keepalive to prevent connection timeout while model is thinking
						pingEvent := map[string]interface{}{"type": "ping"}
						pingData, _ := json.Marshal(pingEvent)
						WriteSSEEvent(w, "ping", pingData)
						sentAny = true
						continue
					}
					// Handle thinking content
					if currentBlockType != "thinking" {
						// Close previous block if any
						if currentBlockType != "" {
							WriteSSEEvent(w, "content_block_stop", []byte(fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, blockIndex)))
							blockIndex++
						}
						// Start thinking block
						startEvent := AnthropicStreamContentBlockStart{
							Type:  "content_block_start",
							Index: blockIndex,
							Block: &AnthropicRespThinkingBlock{
								Type: "thinking",
							},
						}
						eventData, _ := json.Marshal(startEvent)
						WriteSSEEvent(w, "content_block_start", eventData)
						currentBlockType = "thinking"
					}
					// Send thinking delta
					delta := AnthropicStreamContentBlockDelta{
						Type:  "content_block_delta",
						Index: blockIndex,
						Delta: &AnthropicStreamThinkingDelta{
							Type:     "thinking_delta",
							Thinking: part.Text,
						},
					}
					eventData, _ := json.Marshal(delta)
					WriteSSEEvent(w, "content_block_delta", eventData)
					sentAny = true
				} else {
					// Handle text content
					if currentBlockType != "text" {
						// Close previous block if any
						if currentBlockType != "" {
							WriteSSEEvent(w, "content_block_stop", []byte(fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, blockIndex)))
							blockIndex++
						}
						// Start text block
						startEvent := AnthropicStreamContentBlockStart{
							Type:  "content_block_start",
							Index: blockIndex,
							Block: &AnthropicRespTextBlock{
								Type: "text",
							},
						}
						eventData, _ := json.Marshal(startEvent)
						WriteSSEEvent(w, "content_block_start", eventData)
						currentBlockType = "text"
					}
					// Send text delta
					delta := AnthropicStreamContentBlockDelta{
						Type:  "content_block_delta",
						Index: blockIndex,
						Delta: &AnthropicStreamTextDelta{
							Type: "text_delta",
							Text: part.Text,
						},
					}
					eventData, _ := json.Marshal(delta)
					WriteSSEEvent(w, "content_block_delta", eventData)
					sentAny = true
				}
			}

			if part.FunctionCall != nil {
				// Close previous block if any
				if currentBlockType != "" {
					WriteSSEEvent(w, "content_block_stop", []byte(fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, blockIndex)))
					blockIndex++
				}
				// Start tool_use block
				toolID := generateToolUseID()
				if part.ThoughtSignature != "" {
					thoughtSignatureCache.Store(toolID, part.ThoughtSignature)
				log.Printf("[proxy/anthropic] Stored stream thought signature for tool call %s", toolID)
				}
				startEvent := AnthropicStreamContentBlockStart{
					Type:  "content_block_start",
					Index: blockIndex,
					Block: &AnthropicRespToolUseBlock{
						Type:  "tool_use",
						ID:    toolID,
						Name:  part.FunctionCall.Name,
						Input: map[string]interface{}{},
					},
				}
				eventData, _ := json.Marshal(startEvent)
				WriteSSEEvent(w, "content_block_start", eventData)
				currentBlockType = "tool_use"

				// Send tool input as incremental partial JSON
				argsJSON, _ := json.Marshal(part.FunctionCall.Args)
				argsStr := string(argsJSON)
				argsLen := len(argsStr)
				const chunkSize = 32
				for i := 0; i < argsLen; i += chunkSize {
					end := i + chunkSize
					if end > argsLen {
						end = argsLen
					}
					delta := AnthropicStreamContentBlockDelta{
						Type:  "content_block_delta",
						Index: blockIndex,
						Delta: &AnthropicStreamInputJSONDelta{
							Type:        "input_json_delta",
							PartialJSON: argsStr[i:end],
						},
					}
					eventData, _ = json.Marshal(delta)
					WriteSSEEvent(w, "content_block_delta", eventData)
				}
				sentAny = true
			}
		}

		// Send message_delta with stop_reason if present
		if candidate.FinishReason != "" {
			// Close current block if any
			if currentBlockType != "" {
				WriteSSEEvent(w, "content_block_stop", []byte(fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, blockIndex)))
				blockIndex++
			}

			stopReason := mapGeminiFinishReasonToAnthropic(candidate.FinishReason)
			msgDelta := AnthropicStreamMessageDelta{
				Type: "message_delta",
				Delta: &AnthropicMessageDelta{
					StopReason:   stopReason,
					StopSequence: nil,
				},
				Usage: &AnthropicUsage{
					OutputTokens:             0,
					CacheCreationInputTokens: 0,
					CacheReadInputTokens:     0,
				},
			}
			if geminiResp.UsageMetadata != nil {
				msgDelta.Usage.OutputTokens = geminiResp.UsageMetadata.CandidatesTokenCount
			}
			eventData, _ := json.Marshal(msgDelta)
			WriteSSEEvent(w, "message_delta", eventData)

			// Send message_stop
			WriteSSEEvent(w, "message_stop", []byte(`{"type":"message_stop"}`))
			sentMessageStop = true
		}
	}

	if !sentAny {
		log.Printf("[proxy/anthropic] warning: no chunks sent to client")
	}

	// Ensure we send message_stop if not already sent
	if sentMessageStart && !sentMessageStop {
		WriteSSEEvent(w, "message_stop", []byte(`{"type":"message_stop"}`))
	}

}

func translateAnthropicToGemini(req *AnthropicRequest) (*GeminiRequest, error) {
	geminiReq := &GeminiRequest{}

	// Parse system prompt
	if len(req.System) > 0 {
		// System can be string or array of blocks
		var systemText string
		var systemBlocks []AnthropicTextBlock

		// Try parsing as string first
		if err := json.Unmarshal(req.System, &systemText); err == nil {
			geminiReq.SystemInstruction = &GeminiContent{
				Role: "system",
				Parts: []GeminiPart{
					{Text: systemText},
				},
			}
		} else if err := json.Unmarshal(req.System, &systemBlocks); err == nil {
			// Parse as array of blocks
			var parts []GeminiPart
			for _, block := range systemBlocks {
				if block.Type == "text" && block.Text != "" {
					parts = append(parts, GeminiPart{Text: block.Text})
				}
			}
			if len(parts) > 0 {
				geminiReq.SystemInstruction = &GeminiContent{
					Role:  "system",
					Parts: parts,
				}
			}
		}
	}

	// Convert messages
	var contents []GeminiContent

	// Pre-scan all assistant messages to build toolUseIDToName map
	toolUseIDToName := make(map[string]string)
	for _, msg := range req.Messages {
		if msg.Role == "assistant" {
			if blocks, ok := msg.Content.([]interface{}); ok {
				for _, item := range blocks {
					if block, ok := item.(map[string]interface{}); ok {
						if block["type"] == "tool_use" {
							if id, ok := block["id"].(string); ok {
								if name, ok := block["name"].(string); ok {
									toolUseIDToName[id] = name
								}
							}
						}
					}
				}
			}
		}
	}

	for _, msg := range req.Messages {
		switch msg.Role {
		case "user":
			content := parseAnthropicContent(msg.Content, toolUseIDToName)
			if len(content) > 0 {
				hasFuncResponse := false
				for _, p := range content {
					if p.FunctionResponse != nil {
						hasFuncResponse = true
						break
					}
				}

				role := "user"
				if hasFuncResponse {
					role = "function"
				}

				contents = append(contents, GeminiContent{
					Role:  role,
					Parts: content,
				})
			}
		case "assistant":
			content := parseAnthropicContent(msg.Content, toolUseIDToName)
			if len(content) > 0 {
				contents = append(contents, GeminiContent{
					Role:  "model",
					Parts: content,
				})
			}
		}
	}

	geminiReq.Contents = contents

	// Convert tools
	if len(req.Tools) > 0 {
		tools := make([]GeminiTool, 0, len(req.Tools))
		for _, t := range req.Tools {
			cleaned, err := cleanSchema(t.InputSchema)
			if err != nil {
				cleaned = t.InputSchema
			}
			decl := GeminiFuncDecl{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  cleaned,
			}
			tools = append(tools, GeminiTool{
				FunctionDeclarations: []GeminiFuncDecl{decl},
			})
		}
		geminiReq.Tools = tools
	}

	// Generation config
	genConfig := &GeminiGenerationConfig{}
	if req.Temperature != nil {
		genConfig.Temperature = req.Temperature
	}
	if req.TopP != nil {
		genConfig.TopP = req.TopP
	}
	if req.TopK != nil {
		genConfig.TopK = req.TopK
	}
	if req.MaxTokens > 0 {
		genConfig.MaxOutputTokens = &req.MaxTokens
	}
	if len(req.StopSequences) > 0 {
		genConfig.StopSequences = req.StopSequences
	}

	// Add thinking config if present (only for models that support it, i.e., not Gemma models, and only if type is "enabled")
	if req.Thinking != nil && req.Thinking.Type == "enabled" && req.Thinking.BudgetTokens != nil && !strings.Contains(strings.ToLower(req.Model), "gemma") {
		genConfig.ThinkingConfig = &GeminiThinkingConfig{
			ThinkingBudget: *req.Thinking.BudgetTokens,
		}
		// Map thinking budget to max output tokens if not set
		if genConfig.MaxOutputTokens == nil {
			genConfig.MaxOutputTokens = req.Thinking.BudgetTokens
		}
	}

	if genConfig.Temperature != nil || genConfig.TopP != nil || genConfig.TopK != nil || genConfig.MaxOutputTokens != nil {
		geminiReq.GenerationConfig = genConfig
	}

	// Translate tool_choice to toolConfig
	if req.ToolChoice != nil && len(req.Tools) > 0 {
		geminiReq.ToolConfig = translateAnthropicToolChoice(req.ToolChoice)
	}

	return geminiReq, nil
}

func parseAnthropicContent(content AnthropicContent, toolUseIDToName map[string]string) []GeminiPart {
	var parts []GeminiPart

	switch v := content.(type) {
	case string:
		if v != "" {
			parts = append(parts, GeminiPart{Text: v})
		}
	case []interface{}:
		for _, item := range v {
			if block, ok := item.(map[string]interface{}); ok {
				blockType, _ := block["type"].(string)
				switch blockType {
				case "text":
					if text, ok := block["text"].(string); ok && text != "" {
						parts = append(parts, GeminiPart{Text: text})
					}
				case "tool_use":
					id, _ := block["id"].(string)
					name, _ := block["name"].(string)
					input := block["input"]
					if input == nil {
						input = map[string]interface{}{}
					}
					argsJSON, _ := json.Marshal(input)
					var thoughtSig string
					if id != "" {
						thoughtSig, _ = thoughtSignatureCache.Load(id)
					}
					parts = append(parts, GeminiPart{
						FunctionCall: &GeminiFuncCall{
							Name: name,
							Args: json.RawMessage(argsJSON),
						},
						ThoughtSignature: thoughtSig,
					})
				case "tool_result":
					toolUseID, _ := block["tool_use_id"].(string)
					resultContent := block["content"]
					isError, _ := block["is_error"].(bool)

					var responseValue interface{}
					if isError {
						responseValue = map[string]interface{}{
							"error": resultContent,
						}
					} else {
						switch c := resultContent.(type) {
						case []interface{}:
							var texts []string
							for _, item := range c {
								if m, ok := item.(map[string]interface{}); ok {
									if t, ok := m["text"].(string); ok {
										texts = append(texts, t)
									}
								}
							}
							if len(texts) > 0 {
								responseValue = strings.Join(texts, "\n")
							} else {
								responseValue = resultContent
							}
						default:
							responseValue = resultContent
						}
					}

					// Look up function name from the map
					name := toolUseID
					if mappedName, ok := toolUseIDToName[toolUseID]; ok {
						name = mappedName
					}

					parts = append(parts, GeminiPart{
						FunctionResponse: &GeminiFuncResponse{
							Name: name,
							Response: map[string]interface{}{
								"result": responseValue,
							},
						},
					})
				case "thinking":
					if thinking, ok := block["thinking"].(string); ok && thinking != "" {
						parts = append(parts, GeminiPart{
							Text:    thinking,
							Thought: true,
						})
					}
				case "image":
					if source, ok := block["source"].(map[string]interface{}); ok {
						sourceType, _ := source["type"].(string)
						if sourceType == "base64" {
							mediaType, _ := source["media_type"].(string)
							data, _ := source["data"].(string)
							if mediaType != "" && data != "" {
								parts = append(parts, GeminiPart{
									InlineData: &GeminiInlineData{
										MimeType: mediaType,
										Data:     data,
									},
								})
							}
						} else if sourceType == "url" {
							url, _ := source["url"].(string)
							if url != "" {
								if mimeType, data, err := fetchAndEncodeImage(url); err == nil {
									parts = append(parts, GeminiPart{
										InlineData: &GeminiInlineData{
											MimeType: mimeType,
											Data:     data,
										},
									})
								} else {
									log.Printf("[proxy/anthropic] failed to fetch image URL %s: %v", url, err)
								}
							}
						} else {
							log.Printf("[proxy/anthropic] unsupported image source type: %s, skipping block", sourceType)
						}
					}
				}
			}
		}
	}

	return parts
}

func translateFromGeminiToAnthropic(resp *GeminiResponse, model string, msgID string, clientSupportsThinking bool) *AnthropicResponse {
	anthropicResp := &AnthropicResponse{
		ID:      msgID,
		Type:    "message",
		Role:    "assistant",
		Model:   model,
		Content: []AnthropicRespBlock{},
	}

	if len(resp.Candidates) == 0 {
		return anthropicResp
	}

	candidate := resp.Candidates[0]

	// Map stop reason
	anthropicResp.StopReason = mapGeminiFinishReasonToAnthropic(candidate.FinishReason)

	// Process content parts
	var contentBlocks []AnthropicRespBlock
	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			if part.Thought {
				if clientSupportsThinking {
					contentBlocks = append(contentBlocks, &AnthropicRespThinkingBlock{
						Type:     "thinking",
						Thinking: part.Text,
					})
				}
			} else {
				contentBlocks = append(contentBlocks, &AnthropicRespTextBlock{
					Type: "text",
					Text: part.Text,
				})
			}
		}
		if part.FunctionCall != nil {
			args, _ := json.Marshal(part.FunctionCall.Args)
			var inputMap map[string]interface{}
			json.Unmarshal(args, &inputMap)
			if inputMap == nil {
				inputMap = map[string]interface{}{}
			}

			toolID := generateToolUseID()
			if part.ThoughtSignature != "" {
				thoughtSignatureCache.Store(toolID, part.ThoughtSignature)
				log.Printf("[proxy/anthropic] Stored non-stream thought signature for tool call %s", toolID)
			}
			contentBlocks = append(contentBlocks, &AnthropicRespToolUseBlock{
				Type:  "tool_use",
				ID:    toolID,
				Name:  part.FunctionCall.Name,
				Input: inputMap,
			})

			// If there's a tool call, update stop_reason
			if anthropicResp.StopReason == "end_turn" {
				anthropicResp.StopReason = "tool_use"
			}
		}
	}

	anthropicResp.Content = contentBlocks

	// Usage
	if resp.UsageMetadata != nil {
		anthropicResp.Usage = &AnthropicUsage{
			InputTokens:  resp.UsageMetadata.PromptTokenCount,
			OutputTokens: resp.UsageMetadata.CandidatesTokenCount,
		}
	} else {
		anthropicResp.Usage = &AnthropicUsage{
			InputTokens:  0,
			OutputTokens: 0,
		}
	}

	return anthropicResp
}

func mapGeminiFinishReasonToAnthropic(reason string) string {
	switch reason {
	case "STOP":
		return "end_turn"
	case "MAX_TOKENS":
		return "max_tokens"
	case "SAFETY", "RECITATION", "OTHER", "BLOCKLIST":
		return "stop"
	case "MALFORMED_FUNCTION_CALL":
		return "stop"
	default:
		return "stop"
	}
}

func translateAnthropicToolChoice(tc *AnthropicToolChoice) *GeminiToolConfig {
	config := &GeminiToolConfig{
		FunctionCallingConfig: &GeminiFunctionCallingConfig{},
	}

	switch tc.Type {
	case "auto":
		config.FunctionCallingConfig.Mode = "AUTO"
	case "any":
		config.FunctionCallingConfig.Mode = "ANY"
	case "none":
		config.FunctionCallingConfig.Mode = "NONE"
	case "tool":
		config.FunctionCallingConfig.Mode = "ANY"
		if tc.Name != "" {
			config.FunctionCallingConfig.AllowedFunctionNames = []string{tc.Name}
		}
	default:
		config.FunctionCallingConfig.Mode = "AUTO"
	}

	return config
}
