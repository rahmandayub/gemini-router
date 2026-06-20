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

type ResponsesHandler struct {
	baseURL string
	pool    *key.Pool
}

func NewResponsesHandler(baseURL string, pool *key.Pool) *ResponsesHandler {
	return &ResponsesHandler{
		baseURL: strings.TrimRight(baseURL, "/"),
		pool:    pool,
	}
}

// Responses API Request Types

type ResponsesRequest struct {
	Model              string            `json:"model"`
	Input              json.RawMessage   `json:"input"`
	Instructions       string            `json:"instructions,omitempty"`
	Tools              []ResponseTool    `json:"tools,omitempty"`
	ToolChoice         json.RawMessage   `json:"tool_choice,omitempty"`
	Temperature        *float64          `json:"temperature,omitempty"`
	TopP               *float64          `json:"top_p,omitempty"`
	MaxOutputTokens    *int              `json:"max_output_tokens,omitempty"`
	Stream             bool              `json:"stream,omitempty"`
	Store              *bool             `json:"store,omitempty"`
	ParallelToolCalls  *bool             `json:"parallel_tool_calls,omitempty"`
	PreviousResponseID string            `json:"previous_response_id,omitempty"`
	Include            []string          `json:"include,omitempty"`
	Text               *ResponseText     `json:"text,omitempty"`
	FrequencyPenalty   *float64          `json:"frequency_penalty,omitempty"`
	PresencePenalty    *float64          `json:"presence_penalty,omitempty"`
	Metadata           map[string]string `json:"metadata,omitempty"`
}

type ResponseText struct {
	Format *ResponseTextFormat `json:"format,omitempty"`
}

type ResponseTextFormat struct {
	Type       string          `json:"type"`
	Name       string          `json:"name,omitempty"`
	Strict     *bool           `json:"strict,omitempty"`
	Schema     json.RawMessage `json:"schema,omitempty"`
}

type ResponseTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

type ResponseInputItem struct {
	Type      string          `json:"type,omitempty"`
	Role      string          `json:"role,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	CallID    string          `json:"call_id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Output    string          `json:"output,omitempty"`
	ID        string          `json:"id,omitempty"`
	Summary   json.RawMessage `json:"summary,omitempty"`
	Arguments string          `json:"arguments,omitempty"`
}

// Responses API Response Types

type Response struct {
	ID                 string               `json:"id"`
	Object             string               `json:"object"`
	Status             string               `json:"status"`
	CreatedAt          int64                `json:"created_at"`
	CompletedAt        *int64               `json:"completed_at,omitempty"`
	Error              *ResponseError       `json:"error,omitempty"`
	IncompleteDetails  *IncompleteDetails   `json:"incomplete_details,omitempty"`
	Instructions       string               `json:"instructions,omitempty"`
	Model              string               `json:"model"`
	Output             []ResponseOutputItem `json:"output"`
	Usage              *ResponseUsage       `json:"usage,omitempty"`
	ParallelToolCalls  bool                 `json:"parallel_tool_calls"`
	PreviousResponseID string               `json:"previous_response_id,omitempty"`
	Store              bool                 `json:"store"`
	Temperature        float64              `json:"temperature,omitempty"`
	ToolChoice         json.RawMessage      `json:"tool_choice,omitempty"`
	Tools              []ResponseTool       `json:"tools,omitempty"`
	TopP               float64              `json:"top_p,omitempty"`
	Metadata           map[string]string    `json:"metadata,omitempty"`
}

type ResponseError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}

type IncompleteDetails struct {
	Reason string `json:"reason"`
}

type ResponseOutputItem struct {
	Type      string          `json:"type"`
	ID        string          `json:"id,omitempty"`
	Status    string          `json:"status,omitempty"`
	Role      string          `json:"role,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	Summary   json.RawMessage `json:"summary,omitempty"`
	CallID    string          `json:"call_id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Arguments string          `json:"arguments,omitempty"`
}

type ResponseMessageContent struct {
	Type        string          `json:"type"`
	Text        string          `json:"text,omitempty"`
	Annotations json.RawMessage `json:"annotations,omitempty"`
}

type ResponseReasoningContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type ResponseSummaryContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type ResponseUsage struct {
	InputTokens         int           `json:"input_tokens"`
	OutputTokens        int           `json:"output_tokens"`
	TotalTokens         int           `json:"total_tokens"`
	InputTokensDetails  *TokenDetails `json:"input_tokens_details,omitempty"`
	OutputTokensDetails *TokenDetails `json:"output_tokens_details,omitempty"`
}

type TokenDetails struct {
	CachedTokens    int `json:"cached_tokens,omitempty"`
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
}

// Streaming Event Types

type ResponseStreamEvent struct {
	Type           string         `json:"type"`
	Response       *Response      `json:"response,omitempty"`
	OutputIndex    int            `json:"output_index,omitempty"`
	ContentIndex   int            `json:"content_index,omitempty"`
	ItemID         string         `json:"item_id,omitempty"`
	Item           interface{}    `json:"item,omitempty"`
	Part           interface{}    `json:"part,omitempty"`
	Delta          string         `json:"delta,omitempty"`
	SequenceNumber int            `json:"sequence_number"`
	Error          *ResponseError `json:"error,omitempty"`
	LogProbs       interface{}    `json:"logprobs,omitempty"`
}

// ParseInput parses the input field which can be a string or array of items
func parseInput(raw json.RawMessage) (string, []ResponseInputItem, error) {
	if len(raw) == 0 {
		return "", nil, nil
	}

	// Try parsing as string first
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil, nil
	}

	// Try parsing as array of items
	var items []ResponseInputItem
	if err := json.Unmarshal(raw, &items); err == nil {
		return "", items, nil
	}

	return "", nil, fmt.Errorf("invalid input format: must be string or array of input items")
}

// ParseToolChoice parses the tool_choice field
func parseToolChoice(raw json.RawMessage) (string, string, error) {
	if len(raw) == 0 {
		return "auto", "", nil
	}

	// Try parsing as string first
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, "", nil
	}

	// Try parsing as object
	var tc struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &tc); err == nil {
		return tc.Type, tc.Name, nil
	}

	return "auto", "", nil
}

// translateResponsesToGemini translates an OpenAI Responses API request to Gemini format
func translateResponsesToGemini(req *ResponsesRequest) (*GeminiRequest, error) {
	geminiReq := &GeminiRequest{}

	// Parse instructions as system instruction
	if req.Instructions != "" {
		geminiReq.SystemInstruction = &GeminiContent{
			Parts: []GeminiPart{{Text: req.Instructions}},
		}
	}

	// Parse input
	inputStr, inputItems, err := parseInput(req.Input)
	if err != nil {
		return nil, fmt.Errorf("failed to parse input: %w", err)
	}

	// Convert input to contents
	if inputStr != "" {
		// Simple string input
		geminiReq.Contents = append(geminiReq.Contents, GeminiContent{
			Role:  "user",
			Parts: []GeminiPart{{Text: inputStr}},
		})
	} else {
		// Array of items
		for _, item := range inputItems {
			// Handle developer/system role items by extracting text into systemInstruction
			if item.Role == "developer" || item.Role == "system" {
				text := extractTextFromContent(item.Content)
				if text != "" {
					if geminiReq.SystemInstruction == nil {
						geminiReq.SystemInstruction = &GeminiContent{
							Parts: []GeminiPart{{Text: text}},
						}
					} else {
						geminiReq.SystemInstruction.Parts = append(geminiReq.SystemInstruction.Parts, GeminiPart{Text: text})
					}
				}
				continue
			}
			content, err := translateInputItemToContent(item)
			if err != nil {
				return nil, fmt.Errorf("failed to translate input item: %w", err)
			}
			if content != nil {
				geminiReq.Contents = append(geminiReq.Contents, *content)
			}
		}
	}

	// Convert tools
	if len(req.Tools) > 0 {
		declarations := make([]GeminiFuncDecl, 0, len(req.Tools))
		for _, tool := range req.Tools {
			if tool.Type == "function" {
				cleaned := tool.Parameters
				if len(cleaned) > 0 {
					if c, err := cleanSchema(cleaned); err == nil {
						cleaned = c
					}
				}
				decl := GeminiFuncDecl{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  cleaned,
				}
				declarations = append(declarations, decl)
			}
		}
		if len(declarations) > 0 {
			geminiReq.Tools = []GeminiTool{{FunctionDeclarations: declarations}}
		}
	}

	// Convert tool_choice to toolConfig
	toolChoiceType, toolChoiceName, _ := parseToolChoice(req.ToolChoice)
	if toolChoiceType != "" || toolChoiceName != "" {
		geminiReq.ToolConfig = translateToolChoice(toolChoiceType, toolChoiceName)
	}

	// Generation config
	genConfig := &GeminiGenerationConfig{}
	if req.Temperature != nil {
		genConfig.Temperature = req.Temperature
	}
	if req.TopP != nil {
		genConfig.TopP = req.TopP
	}
	if req.MaxOutputTokens != nil {
		genConfig.MaxOutputTokens = req.MaxOutputTokens
	}
	if req.FrequencyPenalty != nil {
		genConfig.FrequencyPenalty = req.FrequencyPenalty
	}
	if req.PresencePenalty != nil {
		genConfig.PresencePenalty = req.PresencePenalty
	}

	// Handle text format (structured output)
	if req.Text != nil && req.Text.Format != nil {
		switch req.Text.Format.Type {
		case "json_object":
			genConfig.ResponseMimeType = "application/json"
		case "json_schema":
			genConfig.ResponseMimeType = "application/json"
			if len(req.Text.Format.Schema) > 0 {
				cleaned, err := cleanSchema(req.Text.Format.Schema)
				if err == nil {
					genConfig.ResponseSchema = cleaned
				} else {
					genConfig.ResponseSchema = req.Text.Format.Schema
				}
			}
		}
	}

	if genConfig.Temperature != nil || genConfig.TopP != nil || genConfig.MaxOutputTokens != nil || genConfig.ResponseMimeType != "" || genConfig.FrequencyPenalty != nil || genConfig.PresencePenalty != nil {
		geminiReq.GenerationConfig = genConfig
	}

	return geminiReq, nil
}

// translateInputItemToContent translates a single input item to Gemini content
func translateInputItemToContent(item ResponseInputItem) (*GeminiContent, error) {
	switch item.Type {
	case "function_call":
		var args json.RawMessage
		if item.Arguments != "" {
			args = json.RawMessage(item.Arguments)
		}
		return &GeminiContent{
			Role: "model",
			Parts: []GeminiPart{{
				FunctionCall: &GeminiFuncCall{
					Name: item.Name,
					Args: args,
				},
			}},
		}, nil

	case "function_call_output":
		// Function call output needs name field
		if item.Name == "" {
			return nil, fmt.Errorf("function_call_output requires 'name' field")
		}

		// Parse output as JSON
		var output map[string]interface{}
		if err := json.Unmarshal([]byte(item.Output), &output); err != nil {
			output = map[string]interface{}{"result": item.Output}
		}

		return &GeminiContent{
			Role: "user",
			Parts: []GeminiPart{{
				FunctionResponse: &GeminiFuncResponse{
					Name:     item.Name,
					Response: output,
				},
			}},
		}, nil

	case "reasoning":
		// Reasoning items are output-only, skip in input
		return nil, nil

	default:
		// Message items (user, assistant, system, developer)
		role := item.Role
		if role == "developer" {
			role = "system"
		}
		if role == "" {
			role = "user"
		}

		// Map to Gemini role
		geminiRole := "user"
		if role == "assistant" {
			geminiRole = "model"
		}

		// Extract Gemini parts (text + images) from content
		if len(item.Content) > 0 {
			parts := extractGeminiPartsFromContent(item.Content)
			if len(parts) > 0 {
				return &GeminiContent{
					Role:  geminiRole,
					Parts: parts,
				}, nil
			}
		}

		// Fallback: extract text only
		text := extractTextFromContent(item.Content)
		if text == "" {
			return nil, nil
		}

		return &GeminiContent{
			Role:  geminiRole,
			Parts: []GeminiPart{{Text: text}},
		}, nil
	}
}

// extractTextFromContent extracts text from various content formats
func extractTextFromContent(content interface{}) string {
	if content == nil {
		return ""
	}

	switch v := content.(type) {
	case string:
		return v
	case json.RawMessage:
		// Try to parse as string first
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			return s
		}
		// Try to parse as array of content parts
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(v, &parts); err == nil {
			var texts []string
			for _, p := range parts {
				if p.Type == "text" && p.Text != "" {
					texts = append(texts, p.Text)
				}
			}
			return strings.Join(texts, "\n")
		}
		return ""
	case []interface{}:
		var texts []string
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if t, ok := m["text"].(string); ok {
					texts = append(texts, t)
				}
			}
		}
		return strings.Join(texts, "\n")
	}
	return ""
}

// translateToolChoice translates OpenAI tool_choice to Gemini toolConfig
func translateToolChoice(choiceType, choiceName string) *GeminiToolConfig {
	config := &GeminiToolConfig{
		FunctionCallingConfig: &GeminiFunctionCallingConfig{},
	}

	switch choiceType {
	case "none":
		config.FunctionCallingConfig.Mode = "NONE"
	case "required":
		config.FunctionCallingConfig.Mode = "ANY"
	case "function":
		config.FunctionCallingConfig.Mode = "ANY"
		if choiceName != "" {
			config.FunctionCallingConfig.AllowedFunctionNames = []string{choiceName}
		}
	default:
		config.FunctionCallingConfig.Mode = "AUTO"
	}

	return config
}

// translateGeminiToResponse translates a Gemini response to OpenAI Responses API format
func translateGeminiToResponse(geminiResp *GeminiResponse, model string, req *ResponsesRequest) *Response {
	now := time.Now().Unix()
	completedAt := now

	resp := &Response{
		ID:                generateResponseID(),
		Object:            "response",
		Status:            "completed",
		CreatedAt:         now,
		CompletedAt:       &completedAt,
		Model:             model,
		Output:            make([]ResponseOutputItem, 0),
		ParallelToolCalls: true,
		Store:             false,
	}

	if req != nil && len(req.Metadata) > 0 {
		resp.Metadata = req.Metadata
	}

	// Handle empty candidates
	if len(geminiResp.Candidates) == 0 {
		resp.Status = "incomplete"
		resp.Error = &ResponseError{
			Message: "No candidates returned",
			Type:    "server_error",
			Code:    "empty_candidates",
		}
		return resp
	}

	candidate := geminiResp.Candidates[0]

	// Map finishReason to status
	resp.Status, resp.IncompleteDetails = mapFinishReason(candidate.FinishReason)

	// Process content parts
	var reasoningTexts []string
	var contentTexts []string
	var functionCalls []GeminiFuncCall

	for _, part := range candidate.Content.Parts {
		if part.Thought {
			reasoningTexts = append(reasoningTexts, part.Text)
		} else if part.FunctionCall != nil {
			functionCalls = append(functionCalls, *part.FunctionCall)
		} else if part.Text != "" {
			contentTexts = append(contentTexts, part.Text)
		}
	}

	// Add reasoning items
	if len(reasoningTexts) > 0 {
		reasoningContent := strings.Join(reasoningTexts, "\n")
		resp.Output = append(resp.Output, ResponseOutputItem{
			Type:   "reasoning",
			ID:     generateItemID("rs_"),
			Status: resp.Status,
			Summary: mustMarshal([]ResponseSummaryContent{{
				Type: "summary_text",
				Text: reasoningContent,
			}}),
			Content: mustMarshal([]ResponseReasoningContent{{
				Type: "reasoning_text",
				Text: reasoningContent,
			}}),
		})
	}

	// Add function call items
	for _, fc := range functionCalls {
		argsJSON, _ := json.Marshal(fc.Args)
		resp.Output = append(resp.Output, ResponseOutputItem{
			Type:      "function_call",
			ID:        generateItemID("fc_"),
			Status:    resp.Status,
			CallID:    generateItemID("call_"),
			Name:      fc.Name,
			Arguments: string(argsJSON),
		})
	}

	// Add message items (content)
	if len(contentTexts) > 0 {
		text := strings.Join(contentTexts, "\n")
		resp.Output = append(resp.Output, ResponseOutputItem{
			Type:   "message",
			ID:     generateItemID("msg_"),
			Status: resp.Status,
			Role:   "assistant",
			Content: mustMarshal([]ResponseMessageContent{{
				Type:        "output_text",
				Text:        text,
				Annotations: json.RawMessage("[]"),
			}}),
		})
	}

	// Usage
	if geminiResp.UsageMetadata != nil {
		resp.Usage = &ResponseUsage{
			InputTokens:  geminiResp.UsageMetadata.PromptTokenCount,
			OutputTokens: geminiResp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:  geminiResp.UsageMetadata.TotalTokenCount,
			InputTokensDetails: &TokenDetails{
				CachedTokens: 0,
			},
			OutputTokensDetails: &TokenDetails{
				ReasoningTokens: 0,
			},
		}
	}

	return resp
}

// mapFinishReason maps Gemini finishReason to Responses API status
func mapFinishReason(finishReason string) (string, *IncompleteDetails) {
	switch finishReason {
	case "STOP":
		return "completed", nil
	case "MAX_TOKENS":
		return "incomplete", &IncompleteDetails{Reason: "max_output_tokens"}
	case "SAFETY", "RECITATION", "OTHER", "BLOCKLIST":
		return "incomplete", &IncompleteDetails{Reason: "content_filter"}
	case "MALFORMED_FUNCTION_CALL":
		return "failed", &IncompleteDetails{Reason: "tool_call_error"}
	default:
		return "completed", nil
	}
}

// generateResponseID generates a response ID with "resp_" prefix
func generateResponseID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return fmt.Sprintf("resp_%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("resp_%x", b)
}

// generateItemID generates an item ID with the given prefix
func generateItemID(prefix string) string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return fmt.Sprintf("%s%d", prefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s%x", prefix, b)
}

// mustMarshal marshals a value to json.RawMessage, returning empty array on error
func mustMarshal(v interface{}) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("[]")
	}
	return b
}

// ServeHTTP handles the /v1/responses endpoint
func (h *ResponsesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	var req ResponsesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if req.PreviousResponseID != "" {
		log.Printf("[proxy/responses] warning: previous_response_id is ignored (stateless mode): %s", req.PreviousResponseID)
	}

	geminiReq, err := translateResponsesToGemini(&req)
	if err != nil {
		log.Printf("[proxy/responses] translation error: %v", err)
		http.Error(w, fmt.Sprintf("translation error: %v", err), http.StatusBadRequest)
		return
	}

	geminiBody, err := json.Marshal(geminiReq)
	if err != nil {
		log.Printf("[proxy/responses] marshal error: %v", err)
		http.Error(w, "failed to marshal gemini request", http.StatusInternalServerError)
		return
	}

	endpoint := "generateContent"
	if req.Stream {
		endpoint = "streamGenerateContent?alt=sse"
	}

	upstreamURL := fmt.Sprintf("%s/v1beta/models/%s:%s", h.baseURL, req.Model, endpoint)

	var resp *http.Response
	var lastErr error
	maxRetries := 3

	if req.Stream {
		// Handle streaming
		h.handleStream(w, r, &req, geminiBody, upstreamURL, maxRetries)
		return
	}

	// Non-streaming with retries
	for attempt := 0; attempt < maxRetries; attempt++ {
		apiKey := h.pool.Next()

		upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(geminiBody))
		if err != nil {
			log.Printf("[proxy/responses] failed to create upstream request: %v", err)
			http.Error(w, "failed to create upstream request", http.StatusInternalServerError)
			return
		}

		upstreamReq.Header.Set("Content-Type", "application/json")
		upstreamReq.Header.Set("x-goog-api-key", apiKey)

		log.Printf("[proxy/responses] POST /v1/responses (attempt %d) -> model=%s", attempt+1, req.Model)

		resp, err = UpstreamClient.Do(upstreamReq)
		if err != nil {
			lastErr = err
			log.Printf("[proxy/responses] request failed (attempt %d): %v", attempt+1, err)
			if r.Context().Err() != nil {
				break
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}

		if resp.StatusCode == http.StatusInternalServerError || resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusTooManyRequests {
			bodyBytes, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			log.Printf("[proxy/responses] upstream returned status %d (attempt %d): %s", resp.StatusCode, attempt+1, string(bodyBytes))
			lastErr = fmt.Errorf("upstream status %d: %s", resp.StatusCode, string(bodyBytes))
			resp = nil
			time.Sleep(50 * time.Millisecond)
			continue
		}

		break
	}

	if resp == nil {
		log.Printf("[proxy/responses] all retries failed. Last error: %v", lastErr)
		errResp := &Response{
			ID:     generateResponseID(),
			Object: "response",
			Status: "failed",
			Error: &ResponseError{
				Message: fmt.Sprintf("upstream request failed: %v", lastErr),
				Type:    "server_error",
				Code:    "upstream_error",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(errResp)
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[proxy/responses] failed to read upstream response: %v", err)
		http.Error(w, "failed to read upstream response", http.StatusBadGateway)
		return
	}

	if resp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		errObj := translateGeminiErrorToResponses(respBody)
		json.NewEncoder(w).Encode(errObj)
		return
	}

	var geminiResp GeminiResponse
	if err := json.Unmarshal(respBody, &geminiResp); err != nil {
		log.Printf("[proxy/responses] failed to parse upstream response: %v", err)
		http.Error(w, "failed to parse upstream response", http.StatusBadGateway)
		return
	}

	response := translateGeminiToResponse(&geminiResp, req.Model, &req)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleStream handles streaming responses
func (h *ResponsesHandler) handleStream(w http.ResponseWriter, r *http.Request, req *ResponsesRequest, geminiBody []byte, upstreamURL string, maxRetries int) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	seqNum := 0

	// Send response.created event
	response := &Response{
		ID:                generateResponseID(),
		Object:            "response",
		Status:            "in_progress",
		CreatedAt:         time.Now().Unix(),
		Model:             req.Model,
		Output:            []ResponseOutputItem{},
		ParallelToolCalls: true,
		Store:             false,
	}
	if len(req.Metadata) > 0 {
		response.Metadata = req.Metadata
	}

	// Streaming event sequence for reasoning + function_call:
	// 1. response.created
	// 2. response.output_item.added (reasoning item, output_index=0)
	// 3. response.reasoning_summary_text.delta (reasoning text chunks)
	// 4. response.output_item.done (reasoning item)
	// 5. response.output_item.added (function_call item, output_index=1)
	// 6. response.function_call_arguments.done (function call args)
	// 7. response.output_item.done (function_call item)
	// 8. response.completed
	// Note: Current implementation skips reasoning in streaming (thought parts not forwarded).
	// Function calls are streamed directly when encountered.

	createdEvent := ResponseStreamEvent{
		Type:           "response.created",
		Response:       response,
		SequenceNumber: seqNum,
	}
	seqNum++

	eventData, _ := json.Marshal(createdEvent)
	w.Write([]byte("data: "))
	w.Write(eventData)
	w.Write([]byte("\n\n"))
	flusher.Flush()

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
	var resp *http.Response
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		apiKey := h.pool.Next()

		upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(geminiBody))
		if err != nil {
			log.Printf("[proxy/responses] failed to create upstream request: %v", err)
			close(stopKeepAlive)
			return
		}

		upstreamReq.Header.Set("Content-Type", "application/json")
		upstreamReq.Header.Set("x-goog-api-key", apiKey)

		log.Printf("[proxy/responses] POST /v1/responses (attempt %d) -> model=%s stream=true", attempt+1, req.Model)

		resp, err = UpstreamClient.Do(upstreamReq)
		if err != nil {
			lastErr = err
			log.Printf("[proxy/responses] request failed (attempt %d): %v", attempt+1, err)
			if r.Context().Err() != nil {
				break
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}

		if resp.StatusCode == http.StatusInternalServerError || resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusTooManyRequests {
			bodyBytes, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			log.Printf("[proxy/responses] upstream returned status %d (attempt %d): %s", resp.StatusCode, attempt+1, string(bodyBytes))
			lastErr = fmt.Errorf("upstream status %d: %s", resp.StatusCode, string(bodyBytes))
			resp = nil
			time.Sleep(50 * time.Millisecond)
			continue
		}

		break
	}

	close(stopKeepAlive)

	if resp == nil {
		log.Printf("[proxy/responses] all retries failed. Last error: %v", lastErr)
		errEvent := ResponseStreamEvent{
			Type: "response.error",
			Error: &ResponseError{
				Message: fmt.Sprintf("upstream request failed: %v", lastErr),
				Type:    "server_error",
				Code:    "upstream_error",
			},
			SequenceNumber: seqNum,
		}
		eventData, _ := json.Marshal(errEvent)
		w.Write([]byte("data: "))
		w.Write(eventData)
		w.Write([]byte("\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
		return
	}
	defer resp.Body.Close()

	// Process SSE stream
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var currentText strings.Builder
	var currentReasoning strings.Builder
	outputIndex := 0
	contentIndex := 0
	itemStarted := false
	reasoningStarted := false
	reasoningCompleted := false
	reasoningItemID := generateItemID("rs_")
	itemID := generateItemID("msg_")
	var usageMetadata *GeminiUsageMetadata
	var streamedFunctionCalls []ResponseOutputItem

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		// Parse Gemini SSE chunk
		var chunk GeminiResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if chunk.UsageMetadata != nil {
			usageMetadata = chunk.UsageMetadata
		}

		if len(chunk.Candidates) == 0 {
			continue
		}

		candidate := chunk.Candidates[0]

		for _, part := range candidate.Content.Parts {
			// Handle reasoning (thought) parts
			if part.Thought {
				if !reasoningStarted {
					addEvent := ResponseStreamEvent{
						Type:        "response.output_item.added",
						OutputIndex: outputIndex,
						Item: ResponseOutputItem{
							Type:   "reasoning",
							ID:     reasoningItemID,
							Status: "in_progress",
							Summary: mustMarshal([]ResponseSummaryContent{{
								Type: "summary_text",
								Text: "",
							}}),
						},
						SequenceNumber: seqNum,
					}
					eventData, _ := json.Marshal(addEvent)
					w.Write([]byte("data: "))
					w.Write(eventData)
					w.Write([]byte("\n\n"))
					flusher.Flush()
					seqNum++
					reasoningStarted = true
				}

				if part.Text != "" {
					currentReasoning.WriteString(part.Text)

					deltaEvent := ResponseStreamEvent{
						Type:           "response.reasoning_summary_text.delta",
						OutputIndex:    outputIndex,
						ContentIndex:   0,
						ItemID:         reasoningItemID,
						Delta:          part.Text,
						SequenceNumber: seqNum,
					}
					eventData, _ := json.Marshal(deltaEvent)
					w.Write([]byte("data: "))
					w.Write(eventData)
					w.Write([]byte("\n\n"))
					flusher.Flush()
					seqNum++
				}
				continue
			}

			// When transitioning from reasoning to non-reasoning, close reasoning and advance outputIndex
			if reasoningStarted {
				reasoningDoneEvent := ResponseStreamEvent{
					Type:        "response.output_item.done",
					OutputIndex: outputIndex,
					Item: ResponseOutputItem{
						Type:   "reasoning",
						ID:     reasoningItemID,
						Status: "completed",
						Summary: mustMarshal([]ResponseSummaryContent{{
							Type: "summary_text",
							Text: currentReasoning.String(),
						}}),
						Content: mustMarshal([]ResponseReasoningContent{{
							Type: "reasoning_text",
							Text: currentReasoning.String(),
						}}),
					},
					SequenceNumber: seqNum,
				}
				eventData, _ := json.Marshal(reasoningDoneEvent)
				w.Write([]byte("data: "))
				w.Write(eventData)
				w.Write([]byte("\n\n"))
				flusher.Flush()
				seqNum++
				outputIndex++
				reasoningStarted = false
				reasoningCompleted = true
			}

			// Handle function calls
			if part.FunctionCall != nil {
				// Close any in-progress text item before starting function call
				if itemStarted && currentText.Len() > 0 {
					partDoneEvent := ResponseStreamEvent{
						Type:         "response.content_part.done",
						OutputIndex:  outputIndex,
						ContentIndex: contentIndex,
						ItemID:       itemID,
						Part: ResponseMessageContent{
							Type:        "output_text",
							Text:        currentText.String(),
							Annotations: json.RawMessage("[]"),
						},
						SequenceNumber: seqNum,
					}
					eventData, _ := json.Marshal(partDoneEvent)
					w.Write([]byte("data: "))
					w.Write(eventData)
					w.Write([]byte("\n\n"))
					flusher.Flush()
					seqNum++

					itemDoneEvent := ResponseStreamEvent{
						Type:        "response.output_item.done",
						OutputIndex: outputIndex,
						Item: ResponseOutputItem{
							Type:   "message",
							ID:     itemID,
							Status: "completed",
							Role:   "assistant",
							Content: mustMarshal([]ResponseMessageContent{{
								Type:        "output_text",
								Text:        currentText.String(),
								Annotations: json.RawMessage("[]"),
							}}),
						},
						SequenceNumber: seqNum,
					}
					eventData, _ = json.Marshal(itemDoneEvent)
					w.Write([]byte("data: "))
					w.Write(eventData)
					w.Write([]byte("\n\n"))
					flusher.Flush()
					seqNum++
					outputIndex++
					itemStarted = false
				}

				fcItemID := generateItemID("fc_")
				callID := generateItemID("call_")
				if !itemStarted {
					addEvent := ResponseStreamEvent{
						Type:        "response.output_item.added",
						OutputIndex: outputIndex,
						Item: ResponseOutputItem{
							Type:   "function_call",
							ID:     fcItemID,
							Status: "in_progress",
						},
						SequenceNumber: seqNum,
					}
					eventData, _ := json.Marshal(addEvent)
					w.Write([]byte("data: "))
					w.Write(eventData)
					w.Write([]byte("\n\n"))
					flusher.Flush()
					seqNum++
					itemStarted = true
				}

				argsJSON, _ := json.Marshal(part.FunctionCall.Args)
				argsStr := string(argsJSON)
				argsLen := len(argsStr)
				const chunkSize = 32
				for i := 0; i < argsLen; i += chunkSize {
					end := i + chunkSize
					if end > argsLen {
						end = argsLen
					}
					deltaEvent := ResponseStreamEvent{
						Type:           "response.function_call_arguments.delta",
						OutputIndex:    outputIndex,
						ItemID:         fcItemID,
						Delta:          argsStr[i:end],
						SequenceNumber: seqNum,
					}
					eventData, _ := json.Marshal(deltaEvent)
					w.Write([]byte("data: "))
					w.Write(eventData)
					w.Write([]byte("\n\n"))
					flusher.Flush()
					seqNum++
				}

				fcDoneEvent := ResponseStreamEvent{
					Type:           "response.function_call_arguments.done",
					OutputIndex:    outputIndex,
					ItemID:         fcItemID,
					Delta:          argsStr,
					SequenceNumber: seqNum,
				}
				eventData, _ := json.Marshal(fcDoneEvent)
				w.Write([]byte("data: "))
				w.Write(eventData)
				w.Write([]byte("\n\n"))
				flusher.Flush()
				seqNum++

				// Send output_item.done for the function call
				fcItemDoneEvent := ResponseStreamEvent{
					Type:        "response.output_item.done",
					OutputIndex: outputIndex,
					Item: ResponseOutputItem{
						Type:      "function_call",
						ID:        fcItemID,
						Status:    "completed",
						CallID:    callID,
						Name:      part.FunctionCall.Name,
						Arguments: argsStr,
					},
					SequenceNumber: seqNum,
				}
				eventData, _ = json.Marshal(fcItemDoneEvent)
				w.Write([]byte("data: "))
				w.Write(eventData)
				w.Write([]byte("\n\n"))
				flusher.Flush()
				seqNum++

				// Track for final response
				streamedFunctionCalls = append(streamedFunctionCalls, ResponseOutputItem{
					Type:      "function_call",
					ID:        fcItemID,
					Status:    "completed",
					CallID:    callID,
					Name:      part.FunctionCall.Name,
					Arguments: argsStr,
				})

				outputIndex++
				itemStarted = false
				continue
			}

			// Handle text content
			if part.Text != "" {
				if !itemStarted {
					addEvent := ResponseStreamEvent{
						Type:        "response.output_item.added",
						OutputIndex: outputIndex,
						Item: ResponseOutputItem{
							Type:    "message",
							ID:      itemID,
							Status:  "in_progress",
							Role:    "assistant",
							Content: mustMarshal([]ResponseMessageContent{}),
						},
						SequenceNumber: seqNum,
					}
					eventData, _ := json.Marshal(addEvent)
					w.Write([]byte("data: "))
					w.Write(eventData)
					w.Write([]byte("\n\n"))
					flusher.Flush()
					seqNum++

					partAddEvent := ResponseStreamEvent{
						Type:         "response.content_part.added",
						OutputIndex:  outputIndex,
						ContentIndex: contentIndex,
						ItemID:       itemID,
						Part: ResponseMessageContent{
							Type: "output_text",
							Text: "",
						},
						SequenceNumber: seqNum,
					}
					eventData, _ = json.Marshal(partAddEvent)
					w.Write([]byte("data: "))
					w.Write(eventData)
					w.Write([]byte("\n\n"))
					flusher.Flush()
					seqNum++

					itemStarted = true
				}

				deltaEvent := ResponseStreamEvent{
					Type:           "response.output_text.delta",
					OutputIndex:    outputIndex,
					ContentIndex:   contentIndex,
					ItemID:         itemID,
					Delta:          part.Text,
					SequenceNumber: seqNum,
					LogProbs:       []interface{}{},
				}
				eventData, _ := json.Marshal(deltaEvent)
				w.Write([]byte("data: "))
				w.Write(eventData)
				w.Write([]byte("\n\n"))
				flusher.Flush()
				seqNum++

				currentText.WriteString(part.Text)
			}
		}

		// Check if candidate is complete
		if candidate.FinishReason != "" {
			// Close reasoning block if open
			if reasoningStarted {
				reasoningDoneEvent := ResponseStreamEvent{
					Type:        "response.output_item.done",
					OutputIndex: outputIndex,
					Item: ResponseOutputItem{
						Type:   "reasoning",
						ID:     reasoningItemID,
						Status: "completed",
						Summary: mustMarshal([]ResponseSummaryContent{{
							Type: "summary_text",
							Text: currentReasoning.String(),
						}}),
						Content: mustMarshal([]ResponseReasoningContent{{
							Type: "reasoning_text",
							Text: currentReasoning.String(),
						}}),
					},
					SequenceNumber: seqNum,
				}
				eventData, _ := json.Marshal(reasoningDoneEvent)
				w.Write([]byte("data: "))
				w.Write(eventData)
				w.Write([]byte("\n\n"))
				flusher.Flush()
				seqNum++
				outputIndex++
				reasoningCompleted = true
			}

			// Close text block if open
			if itemStarted && currentText.Len() > 0 {
				partDoneEvent := ResponseStreamEvent{
					Type:         "response.content_part.done",
					OutputIndex:  outputIndex,
					ContentIndex: contentIndex,
					ItemID:       itemID,
					Part: ResponseMessageContent{
						Type:        "output_text",
						Text:        currentText.String(),
						Annotations: json.RawMessage("[]"),
					},
					SequenceNumber: seqNum,
				}
				eventData, _ := json.Marshal(partDoneEvent)
				w.Write([]byte("data: "))
				w.Write(eventData)
				w.Write([]byte("\n\n"))
				flusher.Flush()
				seqNum++
			}

			if itemStarted {
				itemDoneEvent := ResponseStreamEvent{
					Type:        "response.output_item.done",
					OutputIndex: outputIndex,
					Item: ResponseOutputItem{
						Type:   "message",
						ID:     itemID,
						Status: "completed",
						Role:   "assistant",
						Content: mustMarshal([]ResponseMessageContent{{
							Type:        "output_text",
							Text:        currentText.String(),
							Annotations: json.RawMessage("[]"),
						}}),
					},
					SequenceNumber: seqNum,
				}
				eventData, _ := json.Marshal(itemDoneEvent)
				w.Write([]byte("data: "))
				w.Write(eventData)
				w.Write([]byte("\n\n"))
				flusher.Flush()
				seqNum++
			}

			completedAt := time.Now().Unix()
			response.Status, response.IncompleteDetails = mapFinishReason(candidate.FinishReason)
			response.CompletedAt = &completedAt

			if usageMetadata != nil {
				response.Usage = &ResponseUsage{
					InputTokens:  usageMetadata.PromptTokenCount,
					OutputTokens: usageMetadata.CandidatesTokenCount,
					TotalTokens:  usageMetadata.TotalTokenCount,
				}
			}

			var outputItems []ResponseOutputItem
			if reasoningCompleted || reasoningStarted {
				outputItems = append(outputItems, ResponseOutputItem{
					Type:   "reasoning",
					ID:     reasoningItemID,
					Status: response.Status,
					Summary: mustMarshal([]ResponseSummaryContent{{
						Type: "summary_text",
						Text: currentReasoning.String(),
					}}),
					Content: mustMarshal([]ResponseReasoningContent{{
						Type: "reasoning_text",
						Text: currentReasoning.String(),
					}}),
				})
			}
			outputItems = append(outputItems, streamedFunctionCalls...)
			if currentText.Len() > 0 {
				outputItems = append(outputItems, ResponseOutputItem{
					Type:   "message",
					ID:     itemID,
					Status: response.Status,
					Role:   "assistant",
					Content: mustMarshal([]ResponseMessageContent{{
						Type:        "output_text",
						Text:        currentText.String(),
						Annotations: json.RawMessage("[]"),
					}}),
				})
			}
			response.Output = outputItems

			completedEvent := ResponseStreamEvent{
				Type:           "response.completed",
				Response:       response,
				SequenceNumber: seqNum,
			}
			eventData, _ := json.Marshal(completedEvent)
			w.Write([]byte("data: "))
			w.Write(eventData)
			w.Write([]byte("\n\n"))
			w.Write([]byte("data: [DONE]\n\n"))
			flusher.Flush()
			return
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[proxy/responses] stream scanner error: %v", err)
	}

	// If we get here without a completion event, send one
	completedAt := time.Now().Unix()
	response.Status = "completed"
	response.CompletedAt = &completedAt
	if usageMetadata != nil {
		response.Usage = &ResponseUsage{
			InputTokens:  usageMetadata.PromptTokenCount,
			OutputTokens: usageMetadata.CandidatesTokenCount,
			TotalTokens:  usageMetadata.TotalTokenCount,
		}
	}
	var outputItems []ResponseOutputItem
	if reasoningCompleted || reasoningStarted {
		outputItems = append(outputItems, ResponseOutputItem{
			Type:   "reasoning",
			ID:     reasoningItemID,
			Status: "completed",
			Summary: mustMarshal([]ResponseSummaryContent{{
				Type: "summary_text",
				Text: currentReasoning.String(),
			}}),
			Content: mustMarshal([]ResponseReasoningContent{{
				Type: "reasoning_text",
				Text: currentReasoning.String(),
			}}),
		})
	}
	outputItems = append(outputItems, streamedFunctionCalls...)
	if currentText.Len() > 0 {
		outputItems = append(outputItems, ResponseOutputItem{
			Type:   "message",
			ID:     itemID,
			Status: "completed",
			Role:   "assistant",
			Content: mustMarshal([]ResponseMessageContent{{
				Type:        "output_text",
				Text:        currentText.String(),
				Annotations: json.RawMessage("[]"),
			}}),
		})
	}
	response.Output = outputItems

	completedEvent := ResponseStreamEvent{
		Type:           "response.completed",
		Response:       response,
		SequenceNumber: seqNum,
	}
	eventData, _ = json.Marshal(completedEvent)
	w.Write([]byte("data: "))
	w.Write(eventData)
	w.Write([]byte("\n\n"))
	w.Write([]byte("data: [DONE]\n\n"))
	flusher.Flush()
}
