package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

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
	Model       string            `json:"model"`
	Messages    []OpenAIMessage   `json:"messages"`
	Tools       []OpenAITool      `json:"tools,omitempty"`
	Temperature *float64          `json:"temperature,omitempty"`
	MaxTokens   *int              `json:"max_tokens,omitempty"`
	Stop        []string          `json:"stop,omitempty"`
	Stream      bool              `json:"stream,omitempty"`
}

type OpenAIMessage struct {
	Role       string          `json:"role"`
	Content    string          `json:"content,omitempty"`
	ToolCalls  []OpenAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

type OpenAITool struct {
	Type     string          `json:"type"`
	Function OpenAIFunction  `json:"function"`
}

type OpenAIFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type OpenAIToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function OpenAIToolCallFn `json:"function"`
}

type OpenAIToolCallFn struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type GeminiRequest struct {
	Contents         []GeminiContent        `json:"contents,omitempty"`
	SystemInstruction *GeminiContent        `json:"system_instruction,omitempty"`
	Tools            []GeminiTool           `json:"tools,omitempty"`
	GenerationConfig *GeminiGenerationConfig `json:"generationConfig,omitempty"`
}

type GeminiContent struct {
	Role  string        `json:"role,omitempty"`
	Parts []GeminiPart  `json:"parts"`
}

type GeminiPart struct {
	Text             string              `json:"text,omitempty"`
	FunctionCall     *GeminiFuncCall     `json:"functionCall,omitempty"`
	FunctionResponse *GeminiFuncResponse `json:"functionResponse,omitempty"`
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

type GeminiGenerationConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	MaxOutputTokens *int     `json:"maxOutputTokens,omitempty"`
	StopSequences   []string `json:"stopSequences,omitempty"`
}

type GeminiResponse struct {
	Candidates    []GeminiCandidate     `json:"candidates"`
	UsageMetadata *GeminiUsageMetadata  `json:"usageMetadata,omitempty"`
}

type GeminiCandidate struct {
	Content       GeminiContent `json:"content"`
	FinishReason  string        `json:"finishReason,omitempty"`
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
	ID      string            `json:"id"`
	Object  string            `json:"object"`
	Created int64             `json:"created"`
	Model   string            `json:"model"`
	Choices []OpenAIChoice    `json:"choices"`
	Usage   *OpenAIUsage      `json:"usage,omitempty"`
}

type OpenAIChoice struct {
	Index        int            `json:"index"`
	Message      OpenAIMessage  `json:"message,omitempty"`
	Delta        *OpenAIMessage `json:"delta,omitempty"`
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
		http.Error(w, fmt.Sprintf("translation error: %v", err), http.StatusBadRequest)
		return
	}

	geminiBody, err := json.Marshal(geminiReq)
	if err != nil {
		http.Error(w, "failed to marshal gemini request", http.StatusInternalServerError)
		return
	}

	endpoint := "generateContent"
	if openAIReq.Stream {
		endpoint = "streamGenerateContent?alt=sse"
	}

	upstreamURL := fmt.Sprintf("%s/v1beta/models/%s:%s", h.baseURL, openAIReq.Model, endpoint)

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(geminiBody))
	if err != nil {
		http.Error(w, "failed to create upstream request", http.StatusInternalServerError)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	apiKey := h.pool.Next()
	req.Header.Set("x-goog-api-key", apiKey)

	log.Printf("[OPENAI] POST /v1/chat/completions -> model=%s stream=%v key_idx=0", openAIReq.Model, openAIReq.Stream)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "failed to forward request to upstream", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if openAIReq.Stream {
		h.handleStreamResponse(w, resp, openAIReq.Model)
	} else {
		h.handleNonStreamResponse(w, resp, openAIReq.Model)
	}
}

func (h *OpenAIHandler) handleNonStreamResponse(w http.ResponseWriter, resp *http.Response, model string) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "failed to read upstream response", http.StatusBadGateway)
		return
	}

	if resp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return
	}

	var geminiResp GeminiResponse
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		http.Error(w, "failed to parse upstream response", http.StatusBadGateway)
		return
	}

	openAIResp := translateFromGemini(&geminiResp, model)
	respBody, err := json.Marshal(openAIResp)
	if err != nil {
		http.Error(w, "failed to marshal response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(respBody)
}

func (h *OpenAIHandler) handleStreamResponse(w http.ResponseWriter, resp *http.Response, model string) {
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		io.Copy(w, resp.Body)
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	sentAny := false
	for scanner.Scan() {
		line := scanner.Text()

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
			log.Printf("[STREAM] parse error: %v raw=%s", err, data)
			continue
		}

		if len(geminiResp.Candidates) == 0 {
			log.Printf("[STREAM] no candidates in chunk: %s", data)
			continue
		}

		openAIChunk := translateStreamChunk(&geminiResp, model)
		chunkData, err := json.Marshal(openAIChunk)
		if err != nil {
			continue
		}

		w.Write([]byte("data: "))
		w.Write(chunkData)
		w.Write([]byte("\n\n"))
		flusher.Flush()
		sentAny = true
	}

	if !sentAny {
		log.Printf("[STREAM] warning: no chunks sent to client")
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
		case "system":
			if msg.Content != "" {
				systemParts = append(systemParts, GeminiPart{Text: msg.Content})
			}
		case "user":
			contents = append(contents, GeminiContent{
				Role: "user",
				Parts: []GeminiPart{
					{Text: msg.Content},
				},
			})
		case "assistant":
			if msg.Content != "" {
				contents = append(contents, GeminiContent{
					Role: "model",
					Parts: []GeminiPart{
						{Text: msg.Content},
					},
				})
			}
			if len(msg.ToolCalls) > 0 {
				parts := make([]GeminiPart, 0, len(msg.ToolCalls))
				for _, tc := range msg.ToolCalls {
					var args json.RawMessage
					if tc.Function.Arguments != "" {
						args = json.RawMessage(tc.Function.Arguments)
					}
					parts = append(parts, GeminiPart{
						FunctionCall: &GeminiFuncCall{
							Name: tc.Function.Name,
							Args: args,
						},
					})
				}
				contents = append(contents, GeminiContent{
					Role:  "model",
					Parts: parts,
				})
			}
		case "tool":
			var args json.RawMessage
			if msg.Content != "" {
				args = json.RawMessage(msg.Content)
			}
			contents = append(contents, GeminiContent{
				Role: "function",
				Parts: []GeminiPart{
					{
						FunctionResponse: &GeminiFuncResponse{
							Name: msg.ToolCallID,
							Response: map[string]interface{}{
								"result": args,
							},
						},
					},
				},
			})
		}
	}

	if len(systemParts) > 0 {
		geminiReq.SystemInstruction = &GeminiContent{
			Parts: systemParts,
		}
	}

	geminiReq.Contents = contents

	if len(req.Tools) > 0 {
		tools := make([]GeminiTool, 0, len(req.Tools))
		for _, t := range req.Tools {
			decl := GeminiFuncDecl{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			}
			tools = append(tools, GeminiTool{
				FunctionDeclarations: []GeminiFuncDecl{decl},
			})
		}
		geminiReq.Tools = tools
	}

	genConfig := &GeminiGenerationConfig{}
	if req.Temperature != nil {
		genConfig.Temperature = req.Temperature
	}
	if req.MaxTokens != nil {
		genConfig.MaxOutputTokens = req.MaxTokens
	}
	if len(req.Stop) > 0 {
		genConfig.StopSequences = req.Stop
	}
	geminiReq.GenerationConfig = genConfig

	return geminiReq, nil
}

func translateFromGemini(resp *GeminiResponse, model string) *OpenAIResponse {
	openAIResp := &OpenAIResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", 1234567890),
		Object:  "chat.completion",
		Created: 1234567890,
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
		var toolCalls []OpenAIToolCall

		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				textParts = append(textParts, part.Text)
			}
			if part.FunctionCall != nil {
				args, _ := json.Marshal(part.FunctionCall.Args)
				toolCalls = append(toolCalls, OpenAIToolCall{
					ID:   fmt.Sprintf("call_%s", part.FunctionCall.Name),
					Type: "function",
					Function: OpenAIToolCallFn{
						Name:      part.FunctionCall.Name,
						Arguments: string(args),
					},
				})
			}
		}

		if len(textParts) > 0 {
			msg.Content = strings.Join(textParts, "")
		}
		if len(toolCalls) > 0 {
			msg.ToolCalls = toolCalls
			if msg.Content == "" {
				msg.Content = ""
			}
		}

		choice.Message = msg
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

func translateStreamChunk(resp *GeminiResponse, model string) *OpenAIResponse {
	openAIResp := &OpenAIResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", 1234567890),
		Object:  "chat.completion.chunk",
		Created: 1234567890,
		Model:   model,
	}

	if len(resp.Candidates) == 0 {
		return openAIResp
	}

	candidate := resp.Candidates[0]
	choice := OpenAIChoice{
		Index: 0,
		Delta: &OpenAIMessage{
			Role: "assistant",
		},
	}

	if candidate.FinishReason != "" {
		finishReason := mapGeminiFinishReason(candidate.FinishReason)
		choice.FinishReason = &finishReason
	}

	if len(candidate.Content.Parts) > 0 {
		var textParts []string
		var toolCalls []OpenAIToolCall

		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				textParts = append(textParts, part.Text)
			}
			if part.FunctionCall != nil {
				args, _ := json.Marshal(part.FunctionCall.Args)
				toolCalls = append(toolCalls, OpenAIToolCall{
					ID:   fmt.Sprintf("call_%s", part.FunctionCall.Name),
					Type: "function",
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
	case "SAFETY":
		return "content_filter"
	default:
		return "stop"
	}
}
