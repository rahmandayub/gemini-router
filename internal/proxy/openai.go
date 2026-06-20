package proxy

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/rahmandayub/gemini-router/internal/key"
)

type OpenAIHandler struct {
	baseURL string
	pool    *key.Pool
}

func NewOpenAIHandler(baseURL string, pool *key.Pool) *OpenAIHandler {
	return &OpenAIHandler{
		baseURL: strings.TrimRight(baseURL, "/"),
		pool:    pool,
	}
}

type OpenAIRequest struct {
	Model            string              `json:"model"`
	Messages         []OpenAIMessage     `json:"messages"`
	Tools            []OpenAITool        `json:"tools,omitempty"`
	ToolChoice       json.RawMessage     `json:"tool_choice,omitempty"`
	Temperature      *float64            `json:"temperature,omitempty"`
	TopP             *float64            `json:"top_p,omitempty"`
	MaxTokens        *int                `json:"max_tokens,omitempty"`
	Stop             []string            `json:"stop,omitempty"`
	Stream           bool                `json:"stream,omitempty"`
	ResponseFormat   *OpenAIResponseFmt  `json:"response_format,omitempty"`
	StreamOptions    *OpenAIStreamOpts   `json:"stream_options,omitempty"`
	N                *int                `json:"n,omitempty"`
	FrequencyPenalty *float64            `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64            `json:"presence_penalty,omitempty"`
	Seed             *int                `json:"seed,omitempty"`
}

type OpenAIResponseFmt struct {
	Type      string          `json:"type"`
	JSONSchema *OpenAIJSONSchema `json:"json_schema,omitempty"`
}

type OpenAIJSONSchema struct {
	Name   string          `json:"name"`
	Strict *bool           `json:"strict,omitempty"`
	Schema json.RawMessage `json:"schema"`
}

type OpenAIStreamOpts struct {
	IncludeUsage *bool `json:"include_usage,omitempty"`
}

type OpenAIMessage struct {
	Role             string           `json:"role"`
	Content          json.RawMessage  `json:"content"`
	ReasoningContent string           `json:"reasoning_content,omitempty"`
	ToolCalls        []OpenAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string           `json:"tool_call_id,omitempty"`
}

type OpenAIMessageContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL *struct {
		URL    string `json:"url"`
		Detail string `json:"detail,omitempty"`
	} `json:"image_url,omitempty"`
}

func parseOpenAIContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var parts []OpenAIMessageContentPart
	if err := json.Unmarshal(raw, &parts); err == nil {
		var texts []string
		for _, p := range parts {
			if p.Type == "text" && p.Text != "" {
				texts = append(texts, p.Text)
			}
		}
		return strings.Join(texts, "\n")
	}
	return ""
}

func parseOpenAIContentParts(raw json.RawMessage) []OpenAIMessageContentPart {
	if len(raw) == 0 {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return nil
	}
	var parts []OpenAIMessageContentPart
	if err := json.Unmarshal(raw, &parts); err == nil {
		return parts
	}
	return nil
}

func extractGeminiPartsFromContent(raw json.RawMessage) []GeminiPart {
	parts := parseOpenAIContentParts(raw)
	if len(parts) == 0 {
		text := parseOpenAIContent(raw)
		if text != "" {
			return []GeminiPart{{Text: text}}
		}
		return nil
	}
	var geminiParts []GeminiPart
	for _, p := range parts {
		switch p.Type {
		case "text":
			if p.Text != "" {
				geminiParts = append(geminiParts, GeminiPart{Text: p.Text})
			}
		case "image_url":
			if p.ImageURL != nil && p.ImageURL.URL != "" {
				if strings.HasPrefix(p.ImageURL.URL, "data:") {
					parts := strings.SplitN(p.ImageURL.URL, ",", 2)
					if len(parts) == 2 {
						header := parts[0]
						mimeType := strings.TrimPrefix(header, "data:")
						mimeType = strings.TrimSuffix(mimeType, ";base64")
						geminiParts = append(geminiParts, GeminiPart{
							InlineData: &GeminiInlineData{
								MimeType: mimeType,
								Data:     parts[1],
							},
						})
					}
				} else if strings.HasPrefix(p.ImageURL.URL, "http://") || strings.HasPrefix(p.ImageURL.URL, "https://") {
					if mimeType, data, err := fetchAndEncodeImage(p.ImageURL.URL); err == nil {
						geminiParts = append(geminiParts, GeminiPart{
							InlineData: &GeminiInlineData{
								MimeType: mimeType,
								Data:     data,
							},
						})
					} else {
						log.Printf("[proxy/openai] failed to fetch image URL %s: %v", p.ImageURL.URL, err)
					}
				}
			}
		}
	}
	return geminiParts
}

func fetchAndEncodeImage(url string) (mimeType string, base64Data string, err error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("image fetch returned status %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	} else {
		if idx := strings.Index(contentType, ";"); idx != -1 {
			contentType = contentType[:idx]
		}
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("failed to read image data: %w", err)
	}

	return contentType, base64.StdEncoding.EncodeToString(data), nil
}

type OpenAIDelta struct {
	Role             string           `json:"role,omitempty"`
	Content          string           `json:"content"`
	ReasoningContent string           `json:"reasoning_content,omitempty"`
	ToolCalls        []OpenAIToolCall `json:"tool_calls,omitempty"`
}

type OpenAITool struct {
	Type     string         `json:"type"`
	Function OpenAIFunction `json:"function"`
}

type OpenAIFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type OpenAIToolCall struct {
	Index    *int             `json:"index,omitempty"`
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function OpenAIToolCallFn `json:"function"`
}

type OpenAIToolCallFn struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type GeminiRequest struct {
	Contents          []GeminiContent         `json:"contents,omitempty"`
	SystemInstruction *GeminiContent          `json:"system_instruction,omitempty"`
	Tools             []GeminiTool            `json:"tools,omitempty"`
	ToolConfig        *GeminiToolConfig       `json:"toolConfig,omitempty"`
	GenerationConfig  *GeminiGenerationConfig `json:"generationConfig,omitempty"`
}

type GeminiToolConfig struct {
	FunctionCallingConfig *GeminiFunctionCallingConfig `json:"functionCallingConfig"`
}

type GeminiFunctionCallingConfig struct {
	Mode                 string   `json:"mode,omitempty"`
	AllowedFunctionNames []string `json:"allowedFunctionNames,omitempty"`
}

type GeminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []GeminiPart `json:"parts"`
}

type GeminiPart struct {
	Text             string              `json:"text,omitempty"`
	Thought          bool                `json:"thought,omitempty"`
	FunctionCall     *GeminiFuncCall     `json:"functionCall,omitempty"`
	FunctionResponse *GeminiFuncResponse `json:"functionResponse,omitempty"`
	InlineData       *GeminiInlineData   `json:"inlineData,omitempty"`
	ThoughtSignature string              `json:"thoughtSignature,omitempty"`
}

type GeminiInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type GeminiFuncCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type GeminiTool struct {
	FunctionDeclarations []GeminiFuncDecl `json:"functionDeclarations"`
}

type GeminiFuncDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type GeminiThinkingConfig struct {
	ThinkingBudget int `json:"thinkingBudget"`
}

type GeminiGenerationConfig struct {
	Temperature      *float64              `json:"temperature,omitempty"`
	TopP             *float64              `json:"topP,omitempty"`
	TopK             *int                  `json:"topK,omitempty"`
	MaxOutputTokens  *int                  `json:"maxOutputTokens,omitempty"`
	StopSequences    []string              `json:"stopSequences,omitempty"`
	ThinkingConfig   *GeminiThinkingConfig `json:"thinkingConfig,omitempty"`
	ResponseMimeType string                `json:"responseMimeType,omitempty"`
	ResponseSchema   json.RawMessage       `json:"responseSchema,omitempty"`
	FrequencyPenalty *float64              `json:"frequencyPenalty,omitempty"`
	PresencePenalty  *float64              `json:"presencePenalty,omitempty"`
	Seed             *int                  `json:"seed,omitempty"`
}

type GeminiResponse struct {
	Candidates    []GeminiCandidate    `json:"candidates"`
	UsageMetadata *GeminiUsageMetadata `json:"usageMetadata,omitempty"`
}

type GeminiCandidate struct {
	Content      GeminiContent `json:"content"`
	FinishReason string        `json:"finishReason,omitempty"`
}

type GeminiUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

type GeminiFuncResponse struct {
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response"`
}

type OpenAIResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []OpenAIChoice `json:"choices"`
	Usage   *OpenAIUsage   `json:"usage,omitempty"`
}

type OpenAIChoice struct {
	Index        int            `json:"index"`
	Message      *OpenAIMessage `json:"message,omitempty"`
	Delta        *OpenAIDelta   `json:"delta,omitempty"`
	FinishReason *string        `json:"finish_reason"`
}

type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type GeminiModelsResponse struct {
	Models []GeminiModel `json:"models"`
}

type GeminiModel struct {
	Name             string `json:"name"`
	DisplayName      string `json:"displayName"`
	Description      string `json:"description"`
	InputTokenLimit  int    `json:"inputTokenLimit"`
	OutputTokenLimit int    `json:"outputTokenLimit"`
}

type OpenAIModelsResponse struct {
	Object string        `json:"object"`
	Data   []OpenAIModel `json:"data"`
}

type OpenAIModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

func generateID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return fmt.Sprintf("%s%d", OpenAIIDPrefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s%x", OpenAIIDPrefix, b)
}

func (h *OpenAIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	var openAIReq OpenAIRequest
	if err := json.Unmarshal(body, &openAIReq); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	geminiReq, err := translateToGemini(&openAIReq)
	if err != nil {
		log.Printf("[proxy/openai] translation error: %v", err)
		http.Error(w, fmt.Sprintf("translation error: %v", err), http.StatusBadRequest)
		return
	}

	geminiBody, err := json.Marshal(geminiReq)
	if err != nil {
		log.Printf("[proxy/openai] marshal error: %v", err)
		http.Error(w, "failed to marshal gemini request", http.StatusInternalServerError)
		return
	}

	endpoint := "generateContent"
	if openAIReq.Stream {
		endpoint = "streamGenerateContent?alt=sse"
	}

	upstreamURL := fmt.Sprintf("%s/v1beta/models/%s:%s", h.baseURL, openAIReq.Model, endpoint)

	id := generateID()
	created := time.Now().Unix()

	var resp *http.Response
	var lastErr error
	maxRetries := 3

	if openAIReq.Stream {
		// Write headers early to establish connection
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if ok {
			// Write first chunk immediately
			firstChunk := OpenAIResponse{
				ID:      id,
				Object:  "chat.completion.chunk",
				Created: created,
				Model:   openAIReq.Model,
				Choices: []OpenAIChoice{
					{
						Index: 0,
						Delta: &OpenAIDelta{
							Role:    "assistant",
							Content: "",
						},
					},
				},
			}
			chunkData, _ := json.Marshal(firstChunk)
			w.Write([]byte("data: "))
			w.Write(chunkData)
			w.Write([]byte("\n\n"))
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
					w.Write([]byte(": keepalive\n\n"))
					if flusher, ok := w.(http.Flusher); ok {
						flusher.Flush()
					}
				case <-stopKeepAlive:
					return
				case <-r.Context().Done():
					return
				}
			}
		}()

		// Run retry loop
		for attempt := 0; attempt < maxRetries; attempt++ {
			apiKey := h.pool.Next()

			var req *http.Request
			req, err = http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(geminiBody))
			if err != nil {
				log.Printf("[proxy/openai] failed to create upstream request: %v", err)
				close(stopKeepAlive)
				return
			}

			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("x-goog-api-key", apiKey)

			log.Printf("[proxy/openai] POST /v1/chat/completions (attempt %d) -> model=%s stream=true", attempt+1, openAIReq.Model)

			resp, err = UpstreamClient.Do(req)
			if err != nil {
				lastErr = err
				log.Printf("[proxy/openai] request failed (attempt %d): %v", attempt+1, err)
				if r.Context().Err() != nil {
					break
				}
				time.Sleep(50 * time.Millisecond)
				continue
			}

			if resp.StatusCode == http.StatusInternalServerError || resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusTooManyRequests {
				bodyBytes, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				log.Printf("[proxy/openai] upstream returned status %d (attempt %d): %s", resp.StatusCode, attempt+1, string(bodyBytes))
				lastErr = fmt.Errorf("upstream status %d: %s", resp.StatusCode, string(bodyBytes))
				resp = nil
				time.Sleep(50 * time.Millisecond)
				continue
			}

			break
		}

		close(stopKeepAlive)

		if resp == nil {
			log.Printf("[proxy/openai] all retries failed. Last error: %v", lastErr)

			// Send error as assistant text content to render in chat
			errText := fmt.Sprintf("\n\n[Proxy Error: failed to forward request to upstream: %v]", lastErr)
			textChunk := OpenAIResponse{
				ID:      id,
				Object:  "chat.completion.chunk",
				Created: created,
				Model:   openAIReq.Model,
				Choices: []OpenAIChoice{
					{
						Index: 0,
						Delta: &OpenAIDelta{
							Content: errText,
						},
					},
				},
			}
			chunkBytes, _ := json.Marshal(textChunk)
			w.Write([]byte("data: "))
			w.Write(chunkBytes)
			w.Write([]byte("\n\n"))

			errPayload := map[string]interface{}{
				"error": map[string]interface{}{
					"message": fmt.Sprintf("failed to forward request to upstream: %v", lastErr),
					"type":    "api_error",
					"code":    "internal_error",
				},
			}
			errBytes, _ := json.Marshal(errPayload)
			w.Write([]byte("data: "))
			w.Write(errBytes)
			w.Write([]byte("\n\n"))
			w.Write([]byte("data: [DONE]\n\n"))
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			return
		}
		defer resp.Body.Close()

		h.handleStreamResponse(w, resp, openAIReq.Model, id, created, true)
	} else {
		// Non-stream flow (normal retry loop)
		for attempt := 0; attempt < maxRetries; attempt++ {
			apiKey := h.pool.Next()

			var req *http.Request
			req, err = http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(geminiBody))
			if err != nil {
				log.Printf("[proxy/openai] failed to create upstream request: %v", err)
				http.Error(w, "failed to create upstream request", http.StatusInternalServerError)
				return
			}

			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("x-goog-api-key", apiKey)

			log.Printf("[proxy/openai] POST /v1/chat/completions (attempt %d) -> model=%s stream=false", attempt+1, openAIReq.Model)

			resp, err = UpstreamClient.Do(req)
			if err != nil {
				lastErr = err
				log.Printf("[proxy/openai] request failed (attempt %d): %v", attempt+1, err)
				if r.Context().Err() != nil {
					break
				}
				time.Sleep(50 * time.Millisecond)
				continue
			}

			if resp.StatusCode == http.StatusInternalServerError || resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusTooManyRequests {
				bodyBytes, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				log.Printf("[proxy/openai] upstream returned status %d (attempt %d): %s", resp.StatusCode, attempt+1, string(bodyBytes))
				lastErr = fmt.Errorf("upstream status %d: %s", resp.StatusCode, string(bodyBytes))
				resp = nil
				time.Sleep(50 * time.Millisecond)
				continue
			}

			break
		}

		if resp == nil {
			log.Printf("[proxy/openai] all retries failed. Last error: %v", lastErr)
			http.Error(w, fmt.Sprintf("failed to forward request to upstream: %v", lastErr), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		h.handleNonStreamResponse(w, resp, openAIReq.Model, id, created)
	}
}

func (h *OpenAIHandler) handleNonStreamResponse(w http.ResponseWriter, resp *http.Response, model string, id string, created int64) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "failed to read upstream response", http.StatusBadGateway)
		return
	}

	if resp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		errObj := translateGeminiErrorToOpenAI(body)
		errBytes, _ := json.Marshal(errObj)
		w.Write(errBytes)
		return
	}

	var geminiResp GeminiResponse
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		http.Error(w, "failed to parse upstream response", http.StatusBadGateway)
		return
	}

	openAIResp := translateFromGemini(&geminiResp, model, id, created)
	respBody, err := json.Marshal(openAIResp)
	if err != nil {
		log.Printf("[proxy/openai] marshal response error: %v", err)
		http.Error(w, "failed to marshal response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(respBody)
}

func (h *OpenAIHandler) handleStreamResponse(w http.ResponseWriter, resp *http.Response, model string, id string, created int64, headersWritten bool) {
	log.Printf("[proxy/stream] handleStreamResponse called, status=%d", resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[proxy/stream] OpenAI upstream returned non-OK status: %d, body: %s", resp.StatusCode, string(body))
		if !headersWritten {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(resp.StatusCode)
			errObj := translateGeminiErrorToOpenAI(body)
			errBytes, _ := json.Marshal(errObj)
			w.Write(errBytes)
		} else {
			// Send error as assistant text content to render in chat
			errText := fmt.Sprintf("\n\n[Proxy Error: upstream returned error: %s]", string(body))
			textChunk := OpenAIResponse{
				ID:      id,
				Object:  "chat.completion.chunk",
				Created: created,
				Model:   model,
				Choices: []OpenAIChoice{
					{
						Index: 0,
						Delta: &OpenAIDelta{
							Content: errText,
						},
					},
				},
			}
			chunkBytes, _ := json.Marshal(textChunk)
			w.Write([]byte("data: "))
			w.Write(chunkBytes)
			w.Write([]byte("\n\n"))

			// Write OpenAI-compliant error chunk
			errPayload := map[string]interface{}{
				"error": map[string]interface{}{
					"message": string(body),
					"type":    "api_error",
					"code":    "internal_error",
				},
			}
			errBytes, _ := json.Marshal(errPayload)
			w.Write([]byte("data: "))
			w.Write(errBytes)
			w.Write([]byte("\n\n"))
			w.Write([]byte("data: [DONE]\n\n"))
		}
		return
	}

	if !headersWritten {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		io.Copy(w, resp.Body)
		return
	}

	reader := bufio.NewReader(resp.Body)
	sentAny := false
	hasToolCall := false
	isFirst := !headersWritten
	globalToolCallIdx := 0

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				log.Printf("[proxy/stream] error reading stream: %v", err)
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
				log.Printf("[proxy/stream] upstream returned mid-stream error: %d - %s", errResp.Error.Code, errResp.Error.Message)
				errMsg = errResp.Error.Message
			} else {
				log.Printf("[proxy/stream] upstream returned raw JSON mid-stream: %s", fullJSON)
			}

			// Send error as assistant text content to render in chat
			errText := fmt.Sprintf("\n\n[Proxy Error: %s]", errMsg)
			textChunk := OpenAIResponse{
				ID:      id,
				Object:  "chat.completion.chunk",
				Created: created,
				Model:   model,
				Choices: []OpenAIChoice{
					{
						Index: 0,
						Delta: &OpenAIDelta{
							Content: errText,
						},
					},
				},
			}
			chunkBytes, _ := json.Marshal(textChunk)
			w.Write([]byte("data: "))
			w.Write(chunkBytes)
			w.Write([]byte("\n\n"))

			// Send OpenAI-compliant error chunk
			errPayload := map[string]interface{}{
				"error": map[string]interface{}{
					"message": errMsg,
					"type":    "api_error",
					"code":    "internal_error",
				},
			}
			errBytes, _ := json.Marshal(errPayload)
			w.Write([]byte("data: "))
			w.Write(errBytes)
			w.Write([]byte("\n\n"))
			flusher.Flush()

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
			log.Printf("[proxy/stream] parse error: %v raw=%s", err, data)
			continue
		}

		if len(geminiResp.Candidates) == 0 {
			continue
		}

		// Check if this chunk contains a tool call
		candidate := geminiResp.Candidates[0]
		localIdx := globalToolCallIdx
		for _, part := range candidate.Content.Parts {
			if part.FunctionCall != nil {
				hasToolCall = true
				if part.ThoughtSignature != "" {
					toolCallID := fmt.Sprintf("%s%s_%s_%d", OpenAICallPrefix, part.FunctionCall.Name, id, localIdx)
					thoughtSignatureCache.Store(toolCallID, part.ThoughtSignature)
				log.Printf("[proxy/stream] Stored stream thought signature for tool call %s", toolCallID)
				}
				localIdx++
			}
		}

		openAIChunk := translateStreamChunk(&geminiResp, model, isFirst, id, created)
		isFirst = false

		if hasToolCall {
			for i := range openAIChunk.Choices {
				if openAIChunk.Choices[i].FinishReason != nil && *openAIChunk.Choices[i].FinishReason == "stop" {
					tc := "tool_calls"
					openAIChunk.Choices[i].FinishReason = &tc
				}
				if openAIChunk.Choices[i].Delta != nil && len(openAIChunk.Choices[i].Delta.ToolCalls) > 0 {
					for j := range openAIChunk.Choices[i].Delta.ToolCalls {
						val := globalToolCallIdx
						openAIChunk.Choices[i].Delta.ToolCalls[j].Index = &val
						openAIChunk.Choices[i].Delta.ToolCalls[j].ID = fmt.Sprintf("%s_%s_%d", openAIChunk.Choices[i].Delta.ToolCalls[j].ID, id, val)
						globalToolCallIdx++
					}
				}
			}
		}

		chunkData, err := json.Marshal(openAIChunk)
		if err != nil {
			continue
		}

		log.Printf("[proxy/stream] sent chunk: %s", string(chunkData))

		w.Write([]byte("data: "))
		w.Write(chunkData)
		w.Write([]byte("\n\n"))
		flusher.Flush()
		sentAny = true
	}

	if !sentAny {
		log.Printf("[proxy/stream] warning: no chunks sent to client")
	}

	w.Write([]byte("data: [DONE]\n\n"))
	flusher.Flush()
}

func translateToGemini(req *OpenAIRequest) (*GeminiRequest, error) {
	geminiReq := &GeminiRequest{}

	var systemParts []GeminiPart
	var contents []GeminiContent

	for _, msg := range req.Messages {
		switch msg.Role {
		case "system", "developer":
			text := parseOpenAIContent(msg.Content)
			if text != "" {
				systemParts = append(systemParts, GeminiPart{Text: text})
			}
		case "user":
			geminiParts := extractGeminiPartsFromContent(msg.Content)
			if len(geminiParts) > 0 {
				contents = append(contents, GeminiContent{
					Role:  "user",
					Parts: geminiParts,
				})
			}
		case "assistant":
			var parts []GeminiPart
			text := parseOpenAIContent(msg.Content)
			if text != "" {
				parts = append(parts, GeminiPart{Text: text})
			}
			if len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					var args json.RawMessage
					if tc.Function.Arguments != "" {
						args = json.RawMessage(tc.Function.Arguments)
					}
					thoughtSig, _ := thoughtSignatureCache.Load(tc.ID)
					parts = append(parts, GeminiPart{
						FunctionCall: &GeminiFuncCall{
							Name: tc.Function.Name,
							Args: args,
						},
						ThoughtSignature: thoughtSig,
					})
				}
			}
			if len(parts) > 0 {
				contents = append(contents, GeminiContent{
					Role:  "model",
					Parts: parts,
				})
			}
		case "tool":
			var args interface{}
			content := parseOpenAIContent(msg.Content)
			if content != "" {
				var parsedJSON interface{}
				if err := json.Unmarshal([]byte(content), &parsedJSON); err == nil {
					args = parsedJSON
				} else {
					args = content
				}
			} else {
				args = ""
			}

			name := msg.ToolCallID
			name = strings.TrimPrefix(name, OpenAICallPrefix)
			if idx := strings.LastIndex(name, "_"); idx != -1 {
				suffix := name[idx+1:]
				isDigits := true
				for _, r := range suffix {
					if r < '0' || r > '9' {
						isDigits = false
						break
					}
				}
				if isDigits && len(suffix) > 0 {
					name = name[:idx]
				}
			}

			part := GeminiPart{
				FunctionResponse: &GeminiFuncResponse{
					Name: name,
					Response: map[string]interface{}{
						"result": args,
					},
				},
			}

			if len(contents) > 0 && contents[len(contents)-1].Role == "function" {
				contents[len(contents)-1].Parts = append(contents[len(contents)-1].Parts, part)
			} else {
				contents = append(contents, GeminiContent{
					Role:  "function",
					Parts: []GeminiPart{part},
				})
			}
		}
	}

	if len(systemParts) > 0 {
		geminiReq.SystemInstruction = &GeminiContent{
			Role:  "system",
			Parts: systemParts,
		}
	}

	geminiReq.Contents = contents

	if len(req.Tools) > 0 {
		tools := make([]GeminiTool, 0, len(req.Tools))
		for _, t := range req.Tools {
			cleaned, err := cleanSchema(t.Function.Parameters)
			if err != nil {
				cleaned = t.Function.Parameters
			}
			decl := GeminiFuncDecl{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  cleaned,
			}
			tools = append(tools, GeminiTool{
				FunctionDeclarations: []GeminiFuncDecl{decl},
			})
		}
		geminiReq.Tools = tools
	}

	toolChoiceType, toolChoiceName, _ := parseToolChoice(req.ToolChoice)
	if len(req.Tools) > 0 && (toolChoiceType != "" || toolChoiceName != "") {
		geminiReq.ToolConfig = translateToolChoice(toolChoiceType, toolChoiceName)
	}

	genConfig := &GeminiGenerationConfig{}
	if req.Temperature != nil {
		genConfig.Temperature = req.Temperature
	}
	if req.TopP != nil {
		genConfig.TopP = req.TopP
	}
	if req.MaxTokens != nil {
		genConfig.MaxOutputTokens = req.MaxTokens
	}
	if len(req.Stop) > 0 {
		genConfig.StopSequences = req.Stop
	}

	if req.ResponseFormat != nil {
		switch req.ResponseFormat.Type {
		case "json_object":
			genConfig.ResponseMimeType = "application/json"
		case "json_schema":
			genConfig.ResponseMimeType = "application/json"
			if req.ResponseFormat.JSONSchema != nil {
				schema := req.ResponseFormat.JSONSchema.Schema
				cleaned, err := cleanSchema(schema)
				if err == nil {
					schema = cleaned
				}
				genConfig.ResponseSchema = schema
			}
		}
	}

	if req.FrequencyPenalty != nil {
		genConfig.FrequencyPenalty = req.FrequencyPenalty
	}
	if req.PresencePenalty != nil {
		genConfig.PresencePenalty = req.PresencePenalty
	}
	if req.Seed != nil {
		genConfig.Seed = req.Seed
	}

	if genConfig.Temperature != nil || genConfig.TopP != nil || genConfig.MaxOutputTokens != nil || genConfig.ResponseMimeType != "" || len(genConfig.StopSequences) > 0 || genConfig.FrequencyPenalty != nil || genConfig.PresencePenalty != nil || genConfig.Seed != nil {
		geminiReq.GenerationConfig = genConfig
	}

	return geminiReq, nil
}

var unsupportedSchemaProps = map[string]bool{
	"$comment":             true,
	"$schema":              true,
	"additionalProperties": true,
	"enumDescriptions":     true,
}

func cleanSchema(raw json.RawMessage) (json.RawMessage, error) {
	var schema interface{}
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, err
	}
	cleanNode(schema)
	return json.Marshal(schema)
}

func cleanNode(node interface{}) {
	switch v := node.(type) {
	case map[string]interface{}:
		for key := range v {
			if unsupportedSchemaProps[key] {
				delete(v, key)
			} else {
				cleanNode(v[key])
			}
		}
	case []interface{}:
		for _, item := range v {
			cleanNode(item)
		}
	}
}

func translateFromGemini(resp *GeminiResponse, model string, id string, created int64) *OpenAIResponse {
	openAIResp := &OpenAIResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   model,
	}

	if len(resp.Candidates) == 0 {
		return openAIResp
	}

	candidate := resp.Candidates[0]
	choice := OpenAIChoice{
		Index: 0,
	}

	finishReason := mapGeminiFinishReason(candidate.FinishReason)
	choice.FinishReason = &finishReason

	if len(candidate.Content.Parts) > 0 {
		msg := OpenAIMessage{
			Role: "assistant",
		}

		var textParts []string
		var reasoningParts []string
		var toolCalls []OpenAIToolCall

		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				if part.Thought {
					reasoningParts = append(reasoningParts, part.Text)
				} else {
					textParts = append(textParts, part.Text)
				}
			}
			if part.FunctionCall != nil {
				args, _ := json.Marshal(part.FunctionCall.Args)
				toolCallID := fmt.Sprintf("%s%s_%s_%d", OpenAICallPrefix, part.FunctionCall.Name, id, len(toolCalls))
				toolCalls = append(toolCalls, OpenAIToolCall{
					ID:   toolCallID,
					Type: "function",
					Function: OpenAIToolCallFn{
						Name:      part.FunctionCall.Name,
						Arguments: string(args),
					},
				})
				if part.ThoughtSignature != "" {
				thoughtSignatureCache.Store(toolCallID, part.ThoughtSignature)
				log.Printf("[proxy] Stored non-stream thought signature for tool call %s", toolCallID)
			}
			}
		}

		if len(textParts) > 0 {
			contentJSON, _ := json.Marshal(strings.Join(textParts, ""))
			msg.Content = contentJSON
		}
		if len(reasoningParts) > 0 {
			msg.ReasoningContent = strings.Join(reasoningParts, "")
		}
		if len(toolCalls) > 0 {
			msg.ToolCalls = toolCalls
			if len(msg.Content) == 0 {
				msg.Content = json.RawMessage(`""`)
			}
			finishReason = "tool_calls"
		}

		choice.Message = &msg
	}

	openAIResp.Choices = []OpenAIChoice{choice}

	if resp.UsageMetadata != nil {
		openAIResp.Usage = &OpenAIUsage{
			PromptTokens:     resp.UsageMetadata.PromptTokenCount,
			CompletionTokens: resp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      resp.UsageMetadata.TotalTokenCount,
		}
	}

	return openAIResp
}

func translateStreamChunk(resp *GeminiResponse, model string, isFirst bool, id string, created int64) *OpenAIResponse {
	openAIResp := &OpenAIResponse{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
	}

	if len(resp.Candidates) == 0 {
		return openAIResp
	}

	candidate := resp.Candidates[0]
	choice := OpenAIChoice{
		Index: 0,
		Delta: &OpenAIDelta{},
	}
	if isFirst {
		choice.Delta.Role = "assistant"
	}

	if candidate.FinishReason != "" {
		finishReason := mapGeminiFinishReason(candidate.FinishReason)
		choice.FinishReason = &finishReason
	}

	if len(candidate.Content.Parts) > 0 {
		var textParts []string
		var reasoningParts []string
		var toolCalls []OpenAIToolCall

		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				if part.Thought {
					reasoningParts = append(reasoningParts, part.Text)
				} else {
					textParts = append(textParts, part.Text)
				}
			}
			if part.FunctionCall != nil {
				idx := len(toolCalls)
				args, _ := json.Marshal(part.FunctionCall.Args)
				toolCalls = append(toolCalls, OpenAIToolCall{
					Index: &idx,
					ID:    fmt.Sprintf("%s%s", OpenAICallPrefix, part.FunctionCall.Name),
					Type:  "function",
					Function: OpenAIToolCallFn{
						Name:      part.FunctionCall.Name,
						Arguments: string(args),
					},
				})
			}
		}

		if len(textParts) > 0 {
			choice.Delta.Content = strings.Join(textParts, "")
		}
		if len(reasoningParts) > 0 {
			choice.Delta.ReasoningContent = strings.Join(reasoningParts, "")
		}
		if len(toolCalls) > 0 {
			choice.Delta.ToolCalls = toolCalls
		}
	}

	openAIResp.Choices = []OpenAIChoice{choice}
	return openAIResp
}

func mapGeminiFinishReason(reason string) string {
	switch reason {
	case "STOP":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "SAFETY", "RECITATION", "OTHER", "BLOCKLIST":
		return "content_filter"
	case "MALFORMED_FUNCTION_CALL":
		return "stop"
	default:
		return "stop"
	}
}
