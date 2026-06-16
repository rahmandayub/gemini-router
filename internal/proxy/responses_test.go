package proxy

import (
	"encoding/json"
	"testing"
)

func TestParseInputString(t *testing.T) {
	input := json.RawMessage(`"Hello, world!"`)
	str, items, err := parseInput(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if str != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got '%s'", str)
	}
	if items != nil {
		t.Errorf("expected nil items, got %v", items)
	}
}

func TestParseInputArray(t *testing.T) {
	input := json.RawMessage(`[{"role": "user", "content": "Hello"}]`)
	str, items, err := parseInput(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if str != "" {
		t.Errorf("expected empty string, got '%s'", str)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Role != "user" {
		t.Errorf("expected role 'user', got '%s'", items[0].Role)
	}
}

func TestParseInputEmpty(t *testing.T) {
	input := json.RawMessage(``)
	str, items, err := parseInput(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if str != "" {
		t.Errorf("expected empty string, got '%s'", str)
	}
	if items != nil {
		t.Errorf("expected nil items, got %v", items)
	}
}

func TestParseInputInvalid(t *testing.T) {
	input := json.RawMessage(`{"invalid": true}`)
	_, _, err := parseInput(input)
	if err == nil {
		t.Fatal("expected error for invalid input")
	}
}

func TestParseToolChoiceString(t *testing.T) {
	input := json.RawMessage(`"none"`)
	choiceType, choiceName, err := parseToolChoice(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if choiceType != "none" {
		t.Errorf("expected 'none', got '%s'", choiceType)
	}
	if choiceName != "" {
		t.Errorf("expected empty name, got '%s'", choiceName)
	}
}

func TestParseToolChoiceObject(t *testing.T) {
	input := json.RawMessage(`{"type": "function", "name": "get_weather"}`)
	choiceType, choiceName, err := parseToolChoice(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if choiceType != "function" {
		t.Errorf("expected 'function', got '%s'", choiceType)
	}
	if choiceName != "get_weather" {
		t.Errorf("expected 'get_weather', got '%s'", choiceName)
	}
}

func TestTranslateResponsesToGemini(t *testing.T) {
	req := &ResponsesRequest{
		Model:        "gemini-2.5-flash",
		Instructions: "You are a helpful assistant",
		Input:        json.RawMessage(`"What is 2+2?"`),
		Temperature:  float64Ptr(0.7),
	}

	geminiReq, err := translateResponsesToGemini(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if geminiReq.SystemInstruction == nil {
		t.Fatal("expected system instruction")
	}
	if geminiReq.SystemInstruction.Parts[0].Text != "You are a helpful assistant" {
		t.Errorf("unexpected system instruction: %v", geminiReq.SystemInstruction.Parts[0].Text)
	}

	if len(geminiReq.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(geminiReq.Contents))
	}
	if geminiReq.Contents[0].Role != "user" {
		t.Errorf("expected role 'user', got '%s'", geminiReq.Contents[0].Role)
	}
	if geminiReq.Contents[0].Parts[0].Text != "What is 2+2?" {
		t.Errorf("unexpected text: %v", geminiReq.Contents[0].Parts[0].Text)
	}

	if geminiReq.GenerationConfig == nil {
		t.Fatal("expected generation config")
	}
	if geminiReq.GenerationConfig.Temperature == nil {
		t.Fatal("expected temperature")
	}
	if *geminiReq.GenerationConfig.Temperature != 0.7 {
		t.Errorf("expected temperature 0.7, got %f", *geminiReq.GenerationConfig.Temperature)
	}
}

func TestTranslateResponsesToGeminiWithTools(t *testing.T) {
	req := &ResponsesRequest{
		Model: "gemini-2.5-flash",
		Input: json.RawMessage(`"What is the weather?"`),
		Tools: []ResponseTool{
			{
				Type:        "function",
				Name:        "get_weather",
				Description: "Get weather info",
				Parameters:  json.RawMessage(`{"type": "object", "properties": {"location": {"type": "string"}}}`),
			},
		},
		ToolChoice: json.RawMessage(`"auto"`),
	}

	geminiReq, err := translateResponsesToGemini(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(geminiReq.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(geminiReq.Tools))
	}
	if len(geminiReq.Tools[0].FunctionDeclarations) != 1 {
		t.Fatalf("expected 1 function declaration, got %d", len(geminiReq.Tools[0].FunctionDeclarations))
	}
	if geminiReq.Tools[0].FunctionDeclarations[0].Name != "get_weather" {
		t.Errorf("expected function name 'get_weather', got '%s'", geminiReq.Tools[0].FunctionDeclarations[0].Name)
	}

	if geminiReq.ToolConfig == nil {
		t.Fatal("expected tool config")
	}
	if geminiReq.ToolConfig.FunctionCallingConfig.Mode != "AUTO" {
		t.Errorf("expected mode 'AUTO', got '%s'", geminiReq.ToolConfig.FunctionCallingConfig.Mode)
	}
}

func TestTranslateResponsesToGeminiWithAssistantMessage(t *testing.T) {
	req := &ResponsesRequest{
		Model: "gemini-2.5-flash",
		Input: json.RawMessage(`[{"role": "user", "content": "What is 2+2?"}, {"role": "assistant", "content": "4"}, {"role": "user", "content": "And 3+3?"}]`),
	}

	geminiReq, err := translateResponsesToGemini(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(geminiReq.Contents) != 3 {
		t.Fatalf("expected 3 contents, got %d", len(geminiReq.Contents))
	}
	if geminiReq.Contents[0].Role != "user" {
		t.Errorf("expected role 'user', got '%s'", geminiReq.Contents[0].Role)
	}
	if geminiReq.Contents[1].Role != "model" {
		t.Errorf("expected role 'model', got '%s'", geminiReq.Contents[1].Role)
	}
	if geminiReq.Contents[2].Role != "user" {
		t.Errorf("expected role 'user', got '%s'", geminiReq.Contents[2].Role)
	}
}

func TestTranslateGeminiToResponse(t *testing.T) {
	geminiResp := &GeminiResponse{
		Candidates: []GeminiCandidate{
			{
				Content: GeminiContent{
					Role:  "model",
					Parts: []GeminiPart{{Text: "Hello!"}},
				},
				FinishReason: "STOP",
			},
		},
		UsageMetadata: &GeminiUsageMetadata{
			PromptTokenCount:     10,
			CandidatesTokenCount: 5,
			TotalTokenCount:      15,
		},
	}

	resp := translateGeminiToResponse(geminiResp, "gemini-2.5-flash", nil)

	if resp.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if resp.Object != "response" {
		t.Errorf("expected object 'response', got '%s'", resp.Object)
	}
	if resp.Status != "completed" {
		t.Errorf("expected status 'completed', got '%s'", resp.Status)
	}
	if resp.Model != "gemini-2.5-flash" {
		t.Errorf("expected model 'gemini-2.5-flash', got '%s'", resp.Model)
	}

	if len(resp.Output) != 1 {
		t.Fatalf("expected 1 output item, got %d", len(resp.Output))
	}
	if resp.Output[0].Type != "message" {
		t.Errorf("expected type 'message', got '%s'", resp.Output[0].Type)
	}
	if resp.Output[0].Role != "assistant" {
		t.Errorf("expected role 'assistant', got '%s'", resp.Output[0].Role)
	}

	var content []ResponseMessageContent
	if err := json.Unmarshal(resp.Output[0].Content, &content); err != nil {
		t.Fatalf("failed to unmarshal content: %v", err)
	}
	if len(content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(content))
	}
	if content[0].Text != "Hello!" {
		t.Errorf("expected text 'Hello!', got '%s'", content[0].Text)
	}

	if resp.Usage == nil {
		t.Fatal("expected usage")
	}
	if resp.Usage.InputTokens != 10 {
		t.Errorf("expected input tokens 10, got %d", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 5 {
		t.Errorf("expected output tokens 5, got %d", resp.Usage.OutputTokens)
	}
}

func TestTranslateGeminiToResponseWithFunctionCall(t *testing.T) {
	geminiResp := &GeminiResponse{
		Candidates: []GeminiCandidate{
			{
				Content: GeminiContent{
					Role: "model",
					Parts: []GeminiPart{
						{
							FunctionCall: &GeminiFuncCall{
								Name: "get_weather",
								Args: json.RawMessage(`{"location": "NYC"}`),
							},
						},
					},
				},
				FinishReason: "STOP",
			},
		},
	}

	resp := translateGeminiToResponse(geminiResp, "gemini-2.5-flash", nil)

	if len(resp.Output) != 1 {
		t.Fatalf("expected 1 output item, got %d", len(resp.Output))
	}
	if resp.Output[0].Type != "function_call" {
		t.Errorf("expected type 'function_call', got '%s'", resp.Output[0].Type)
	}
	if resp.Output[0].Name != "get_weather" {
		t.Errorf("expected name 'get_weather', got '%s'", resp.Output[0].Name)
	}
	if resp.Output[0].CallID == "" {
		t.Fatal("expected non-empty call_id")
	}
}

func TestTranslateGeminiToResponseWithReasoning(t *testing.T) {
	geminiResp := &GeminiResponse{
		Candidates: []GeminiCandidate{
			{
				Content: GeminiContent{
					Role: "model",
					Parts: []GeminiPart{
						{Text: "thinking process...", Thought: true},
						{Text: "final answer"},
					},
				},
				FinishReason: "STOP",
			},
		},
	}

	resp := translateGeminiToResponse(geminiResp, "gemini-2.5-flash", nil)

	if len(resp.Output) != 2 {
		t.Fatalf("expected 2 output items, got %d", len(resp.Output))
	}
	if resp.Output[0].Type != "reasoning" {
		t.Errorf("expected type 'reasoning', got '%s'", resp.Output[0].Type)
	}
	if resp.Output[1].Type != "message" {
		t.Errorf("expected type 'message', got '%s'", resp.Output[1].Type)
	}
}

func TestTranslateGeminiToResponseEmptyCandidates(t *testing.T) {
	geminiResp := &GeminiResponse{
		Candidates: []GeminiCandidate{},
	}

	resp := translateGeminiToResponse(geminiResp, "gemini-2.5-flash", nil)

	if resp.Status != "incomplete" {
		t.Errorf("expected status 'incomplete', got '%s'", resp.Status)
	}
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != "empty_candidates" {
		t.Errorf("expected error code 'empty_candidates', got '%s'", resp.Error.Code)
	}
}

func TestMapFinishReason(t *testing.T) {
	tests := []struct {
		finishReason       string
		expectedStatus     string
		expectedIncomplete string
	}{
		{"STOP", "completed", ""},
		{"MAX_TOKENS", "incomplete", "max_output_tokens"},
		{"SAFETY", "incomplete", "content_filter"},
		{"RECITATION", "incomplete", "content_filter"},
		{"OTHER", "incomplete", "content_filter"},
		{"", "completed", ""},
	}

	for _, tt := range tests {
		t.Run(tt.finishReason, func(t *testing.T) {
			status, incomplete := mapFinishReason(tt.finishReason)
			if status != tt.expectedStatus {
				t.Errorf("expected status '%s', got '%s'", tt.expectedStatus, status)
			}
			if tt.expectedIncomplete == "" {
				if incomplete != nil {
					t.Errorf("expected nil incomplete, got %v", incomplete)
				}
			} else {
				if incomplete == nil {
					t.Fatal("expected incomplete details")
				}
				if incomplete.Reason != tt.expectedIncomplete {
					t.Errorf("expected incomplete reason '%s', got '%s'", tt.expectedIncomplete, incomplete.Reason)
				}
			}
		})
	}
}

func TestTranslateToolChoice(t *testing.T) {
	tests := []struct {
		choiceType    string
		choiceName    string
		expectedMode  string
		expectedNames []string
	}{
		{"auto", "", "AUTO", nil},
		{"none", "", "NONE", nil},
		{"required", "", "ANY", nil},
		{"function", "get_weather", "ANY", []string{"get_weather"}},
	}

	for _, tt := range tests {
		t.Run(tt.choiceType, func(t *testing.T) {
			config := translateToolChoice(tt.choiceType, tt.choiceName)
			if config.FunctionCallingConfig.Mode != tt.expectedMode {
				t.Errorf("expected mode '%s', got '%s'", tt.expectedMode, config.FunctionCallingConfig.Mode)
			}
			if tt.expectedNames == nil {
				if config.FunctionCallingConfig.AllowedFunctionNames != nil {
					t.Errorf("expected nil allowed names, got %v", config.FunctionCallingConfig.AllowedFunctionNames)
				}
			} else {
				if len(config.FunctionCallingConfig.AllowedFunctionNames) != len(tt.expectedNames) {
					t.Errorf("expected %d allowed names, got %d", len(tt.expectedNames), len(config.FunctionCallingConfig.AllowedFunctionNames))
				}
			}
		})
	}
}

func TestGenerateResponseID(t *testing.T) {
	id := generateResponseID()
	if len(id) < 6 {
		t.Fatal("ID too short")
	}
	if id[:5] != "resp_" {
		t.Errorf("expected prefix 'resp_', got '%s'", id[:5])
	}
}

func TestGenerateItemID(t *testing.T) {
	tests := []struct {
		prefix   string
		expected string
	}{
		{"msg_", "msg_"},
		{"fc_", "fc_"},
		{"rs_", "rs_"},
		{"call_", "call_"},
	}

	for _, tt := range tests {
		t.Run(tt.prefix, func(t *testing.T) {
			id := generateItemID(tt.prefix)
			if len(id) < len(tt.prefix)+4 {
				t.Fatal("ID too short")
			}
			if id[:len(tt.prefix)] != tt.expected {
				t.Errorf("expected prefix '%s', got '%s'", tt.expected, id[:len(tt.prefix)])
			}
		})
	}
}

func TestExtractTextFromString(t *testing.T) {
	text := extractTextFromContent("Hello")
	if text != "Hello" {
		t.Errorf("expected 'Hello', got '%s'", text)
	}
}

func TestExtractTextFromArray(t *testing.T) {
	content := []interface{}{
		map[string]interface{}{"type": "text", "text": "Hello"},
		map[string]interface{}{"type": "text", "text": "World"},
	}
	text := extractTextFromContent(content)
	if text != "Hello\nWorld" {
		t.Errorf("expected 'Hello\\nWorld', got '%s'", text)
	}
}

func TestExtractTextNil(t *testing.T) {
	text := extractTextFromContent(nil)
	if text != "" {
		t.Errorf("expected empty string, got '%s'", text)
	}
}

func TestTranslateInputItemToContentFunctionOutput(t *testing.T) {
	item := ResponseInputItem{
		Type:   "function_call_output",
		Name:   "get_weather",
		CallID: "call_123",
		Output: `{"temp": 72}`,
	}

	content, err := translateInputItemToContent(item)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content == nil {
		t.Fatal("expected content")
	}
	if content.Role != "user" {
		t.Errorf("expected role 'user', got '%s'", content.Role)
	}
	if content.Parts[0].FunctionResponse == nil {
		t.Fatal("expected function response")
	}
	if content.Parts[0].FunctionResponse.Name != "get_weather" {
		t.Errorf("expected function name 'get_weather', got '%s'", content.Parts[0].FunctionResponse.Name)
	}
}

func TestTranslateInputItemToContentFunctionOutputMissingName(t *testing.T) {
	item := ResponseInputItem{
		Type:   "function_call_output",
		CallID: "call_123",
		Output: `{"temp": 72}`,
	}

	_, err := translateInputItemToContent(item)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestTranslateInputItemToContentReasoning(t *testing.T) {
	item := ResponseInputItem{
		Type: "reasoning",
		ID:   "rs_123",
	}

	content, err := translateInputItemToContent(item)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != nil {
		t.Errorf("expected nil content for reasoning item, got %v", content)
	}
}

func float64Ptr(f float64) *float64 {
	return &f
}
