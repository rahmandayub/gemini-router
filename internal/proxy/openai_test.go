package proxy

import (
	"encoding/json"
	"testing"
)

func rawMsg(s string) json.RawMessage {
	return json.RawMessage(`"` + s + `"`)
}

func TestTranslateFromGemini(t *testing.T) {
	// 1. Standard text response
	resp := &GeminiResponse{
		Candidates: []GeminiCandidate{
			{
				Content: GeminiContent{
					Role: "model",
					Parts: []GeminiPart{
						{Text: "Hello, how can I help you today?"},
					},
				},
				FinishReason: "STOP",
			},
		},
		UsageMetadata: &GeminiUsageMetadata{
			PromptTokenCount:     10,
			CandidatesTokenCount: 15,
			TotalTokenCount:      25,
		},
	}

	opt := translateFromGemini(resp, "gemma-4-31b-it", "chatcmpl-1234567890", 1234567890)
	expectedContent, _ := json.Marshal("Hello, how can I help you today?")
	if string(opt.Choices[0].Message.Content) != string(expectedContent) {
		t.Errorf("expected Content to be 'Hello, how can I help you today?', got '%s'", string(opt.Choices[0].Message.Content))
	}
	if opt.Choices[0].Message.ReasoningContent != "" {
		t.Errorf("expected ReasoningContent to be empty, got '%s'", opt.Choices[0].Message.ReasoningContent)
	}

	// 2. Response with thinking process (thought: true)
	respWithThinking := &GeminiResponse{
		Candidates: []GeminiCandidate{
			{
				Content: GeminiContent{
					Role: "model",
					Parts: []GeminiPart{
						{Text: "Thinking: User is greeting me.", Thought: true},
						{Text: "Hello!"},
					},
				},
				FinishReason: "STOP",
			},
		},
	}

	opt2 := translateFromGemini(respWithThinking, "gemma-4-31b-it", "chatcmpl-1234567890", 1234567890)
	if opt2.Choices[0].Message.ReasoningContent != "Thinking: User is greeting me." {
		t.Errorf("expected ReasoningContent to be 'Thinking: User is greeting me.', got '%s'", opt2.Choices[0].Message.ReasoningContent)
	}
	expectedHello, _ := json.Marshal("Hello!")
	if string(opt2.Choices[0].Message.Content) != string(expectedHello) {
		t.Errorf("expected Content to be 'Hello!', got '%s'", string(opt2.Choices[0].Message.Content))
	}
}

func TestTranslateStreamChunk(t *testing.T) {
	// 1. Chunk with thinking
	resp := &GeminiResponse{
		Candidates: []GeminiCandidate{
			{
				Content: GeminiContent{
					Role: "model",
					Parts: []GeminiPart{
						{Text: "Let me think...", Thought: true},
					},
				},
			},
		},
	}

	chunk := translateStreamChunk(resp, "gemma-4-31b-it", true, "chatcmpl-1234567890", 1234567890)
	if chunk.Choices[0].Delta.ReasoningContent != "Let me think..." {
		t.Errorf("expected Delta.ReasoningContent to be 'Let me think...', got '%s'", chunk.Choices[0].Delta.ReasoningContent)
	}
	if chunk.Choices[0].Delta.Content != "" {
		t.Errorf("expected Delta.Content to be empty, got '%s'", chunk.Choices[0].Delta.Content)
	}

	// 2. Chunk with normal text
	resp2 := &GeminiResponse{
		Candidates: []GeminiCandidate{
			{
				Content: GeminiContent{
					Role: "model",
					Parts: []GeminiPart{
						{Text: "Here is the answer."},
					},
				},
			},
		},
	}

	chunk2 := translateStreamChunk(resp2, "gemma-4-31b-it", false, "chatcmpl-1234567890", 1234567890)
	if chunk2.Choices[0].Delta.Content != "Here is the answer." {
		t.Errorf("expected Delta.Content to be 'Here is the answer.', got '%s'", chunk2.Choices[0].Delta.Content)
	}
	if chunk2.Choices[0].Delta.ReasoningContent != "" {
		t.Errorf("expected Delta.ReasoningContent to be empty, got '%s'", chunk2.Choices[0].Delta.ReasoningContent)
	}
}

func TestTranslateToGemini(t *testing.T) {
	// Test grouping of tool result messages
	req := &OpenAIRequest{
		Model: "gemma-4-31b-it",
		Messages: []OpenAIMessage{
			{Role: "user", Content: rawMsg("Call some tools")},
			{Role: "assistant", Content: rawMsg("Thinking..."), ToolCalls: []OpenAIToolCall{
				{ID: "call_tool1_0", Function: OpenAIToolCallFn{Name: "tool1", Arguments: `{"arg":1}`}},
				{ID: "call_tool2_1", Function: OpenAIToolCallFn{Name: "tool2", Arguments: `{"arg":2}`}},
			}},
			{Role: "tool", ToolCallID: "call_tool1_0", Content: rawMsg(`{"result":1}`)},
			{Role: "tool", ToolCallID: "call_tool2_1", Content: rawMsg(`{"result":2}`)},
		},
	}

	geminiReq, err := translateToGemini(req)
	if err != nil {
		t.Fatalf("translateToGemini failed: %v", err)
	}

	if len(geminiReq.Contents) != 3 {
		t.Fatalf("expected 3 content blocks, got %d", len(geminiReq.Contents))
	}

	// 1. User block
	if geminiReq.Contents[0].Role != "user" {
		t.Errorf("expected block 0 role to be 'user', got '%s'", geminiReq.Contents[0].Role)
	}

	// 2. Assistant block (should merge text and tool calls)
	assistantBlock := geminiReq.Contents[1]
	if assistantBlock.Role != "model" {
		t.Errorf("expected block 1 role to be 'model', got '%s'", assistantBlock.Role)
	}
	if len(assistantBlock.Parts) != 3 {
		t.Errorf("expected assistant block to have 3 parts (1 text + 2 function calls), got %d", len(assistantBlock.Parts))
	}

	// 3. Tool block (should group both consecutive tool responses into a single function block)
	toolBlock := geminiReq.Contents[2]
	if toolBlock.Role != "user" {
		t.Errorf("expected block 2 role to be 'user', got '%s'", toolBlock.Role)
	}
	if len(toolBlock.Parts) != 2 {
		t.Errorf("expected tool block to group 2 tool responses, got %d", len(toolBlock.Parts))
	}
	if toolBlock.Parts[0].FunctionResponse.Name != "tool1" {
		t.Errorf("expected tool response 0 name to be 'tool1', got '%s'", toolBlock.Parts[0].FunctionResponse.Name)
	}
	if toolBlock.Parts[1].FunctionResponse.Name != "tool2" {
		t.Errorf("expected tool response 1 name to be 'tool2', got '%s'", toolBlock.Parts[1].FunctionResponse.Name)
	}
}

func TestThoughtSignatureCache(t *testing.T) {
	// Test caching during translateFromGemini
	resp := &GeminiResponse{
		Candidates: []GeminiCandidate{
			{
				Content: GeminiContent{
					Role: "model",
					Parts: []GeminiPart{
						{
							FunctionCall: &GeminiFuncCall{
								Name: "list_dir",
								Args: []byte(`{"path":"/"}`),
							},
							ThoughtSignature: "opaque_signature_abc_123",
						},
					},
				},
				FinishReason: "STOP",
			},
		},
	}

	reqID := "test-req-id"
	translateFromGemini(resp, "gemini-3.1-flash-lite", reqID, 1234567890)

	expectedToolCallID := "call_list_dir_test-req-id_0"
	val, ok := thoughtSignatureCache.Load(expectedToolCallID)
	if !ok {
		t.Fatalf("expected thought_signature to be cached under %s", expectedToolCallID)
	}
	if val != "opaque_signature_abc_123" {
		t.Errorf("expected cached signature to be 'opaque_signature_abc_123', got '%s'", val)
	}

	// Test retrieval during translateToGemini
	req := &OpenAIRequest{
		Model: "gemini-3.1-flash-lite",
		Messages: []OpenAIMessage{
			{
				Role: "assistant",
				ToolCalls: []OpenAIToolCall{
					{
						ID:       expectedToolCallID,
						Type:     "function",
						Function: OpenAIToolCallFn{Name: "list_dir", Arguments: `{"path":"/"}`},
					},
				},
			},
		},
	}

	geminiReq, err := translateToGemini(req)
	if err != nil {
		t.Fatalf("translateToGemini failed: %v", err)
	}

	if len(geminiReq.Contents) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(geminiReq.Contents))
	}
	parts := geminiReq.Contents[0].Parts
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].ThoughtSignature != "opaque_signature_abc_123" {
		t.Errorf("expected thoughtSignature to be injected as 'opaque_signature_abc_123', got '%s'", parts[0].ThoughtSignature)
	}
}

func TestMapGeminiFinishReasonExtended(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"STOP", "stop"},
		{"MAX_TOKENS", "length"},
		{"SAFETY", "content_filter"},
		{"RECITATION", "content_filter"},
		{"OTHER", "content_filter"},
		{"BLOCKLIST", "content_filter"},
		{"MALFORMED_FUNCTION_CALL", "tool_calls"},
		{"UNKNOWN", "stop"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := mapGeminiFinishReason(tt.input)
			if result != tt.expected {
				t.Errorf("mapGeminiFinishReason(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestTranslateToGeminiResponseFormat(t *testing.T) {
	schema := `{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`
	strict := true
	req := &OpenAIRequest{
		Model: "gemini-2.5-flash",
		Messages: []OpenAIMessage{
			{Role: "user", Content: rawMsg("Get me a person")},
		},
		ResponseFormat: &OpenAIResponseFmt{
			Type: "json_schema",
			JSONSchema: &OpenAIJSONSchema{
				Name:   "person",
				Strict: &strict,
				Schema: json.RawMessage(schema),
			},
		},
	}

	geminiReq, err := translateToGemini(req)
	if err != nil {
		t.Fatalf("translateToGemini failed: %v", err)
	}

	if geminiReq.GenerationConfig == nil {
		t.Fatal("expected generation config")
	}
	if geminiReq.GenerationConfig.ResponseMimeType != "application/json" {
		t.Errorf("expected responseMimeType 'application/json', got '%s'", geminiReq.GenerationConfig.ResponseMimeType)
	}
	if geminiReq.GenerationConfig.ResponseSchema == nil {
		t.Fatal("expected responseSchema")
	}
}

func TestTranslateToGeminiJSONMode(t *testing.T) {
	req := &OpenAIRequest{
		Model: "gemini-2.5-flash",
		Messages: []OpenAIMessage{
			{Role: "user", Content: rawMsg("Return JSON")},
		},
		ResponseFormat: &OpenAIResponseFmt{
			Type: "json_object",
		},
	}

	geminiReq, err := translateToGemini(req)
	if err != nil {
		t.Fatalf("translateToGemini failed: %v", err)
	}

	if geminiReq.GenerationConfig == nil {
		t.Fatal("expected generation config")
	}
	if geminiReq.GenerationConfig.ResponseMimeType != "application/json" {
		t.Errorf("expected responseMimeType 'application/json', got '%s'", geminiReq.GenerationConfig.ResponseMimeType)
	}
}

func TestTranslateToGeminiDeveloperRole(t *testing.T) {
	req := &OpenAIRequest{
		Model: "gemini-2.5-flash",
		Messages: []OpenAIMessage{
			{Role: "developer", Content: rawMsg("You are helpful")},
			{Role: "user", Content: rawMsg("Hello")},
		},
	}

	geminiReq, err := translateToGemini(req)
	if err != nil {
		t.Fatalf("translateToGemini failed: %v", err)
	}

	if geminiReq.SystemInstruction == nil {
		t.Fatal("expected system instruction from developer role")
	}
	if geminiReq.SystemInstruction.Parts[0].Text != "You are helpful" {
		t.Errorf("expected system instruction 'You are helpful', got '%s'", geminiReq.SystemInstruction.Parts[0].Text)
	}
}

func TestTranslateToGeminiMultimodalContent(t *testing.T) {
	content := json.RawMessage(`[{"type":"text","text":"What is this?"},{"type":"image_url","image_url":{"url":"data:image/png;base64,YWJjMTIz"}}]`)
	req := &OpenAIRequest{
		Model: "gemini-2.5-flash",
		Messages: []OpenAIMessage{
			{Role: "user", Content: content},
		},
	}

	geminiReq, err := translateToGemini(req)
	if err != nil {
		t.Fatalf("translateToGemini failed: %v", err)
	}

	if len(geminiReq.Contents) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(geminiReq.Contents))
	}

	parts := geminiReq.Contents[0].Parts
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if parts[0].Text != "What is this?" {
		t.Errorf("expected text 'What is this?', got '%s'", parts[0].Text)
	}
	if parts[1].InlineData == nil {
		t.Fatal("expected inlineData for image")
	}
	if parts[1].InlineData.MimeType != "image/png" {
		t.Errorf("expected mimeType 'image/png', got '%s'", parts[1].InlineData.MimeType)
	}
	if parts[1].InlineData.Data != "YWJjMTIz" {
		t.Errorf("expected data 'YWJjMTIz', got '%s'", parts[1].InlineData.Data)
	}
}

func TestTranslateToGeminiToolChoice(t *testing.T) {
	tcJSON := json.RawMessage(`{"type":"function","name":"get_weather"}`)
	req := &OpenAIRequest{
		Model: "gemini-2.5-flash",
		Messages: []OpenAIMessage{
			{Role: "user", Content: rawMsg("Weather?")},
		},
		Tools: []OpenAITool{
			{
				Type:     "function",
				Function: OpenAIFunction{Name: "get_weather", Description: "Get weather", Parameters: json.RawMessage(`{"type":"object"}`)},
			},
		},
		ToolChoice: tcJSON,
	}

	geminiReq, err := translateToGemini(req)
	if err != nil {
		t.Fatalf("translateToGemini failed: %v", err)
	}

	if geminiReq.ToolConfig == nil {
		t.Fatal("expected tool config")
	}
	if geminiReq.ToolConfig.FunctionCallingConfig.Mode != "ANY" {
		t.Errorf("expected ANY, got %s", geminiReq.ToolConfig.FunctionCallingConfig.Mode)
	}
	if len(geminiReq.ToolConfig.FunctionCallingConfig.AllowedFunctionNames) != 1 {
		t.Fatalf("expected 1 allowed name, got %d", len(geminiReq.ToolConfig.FunctionCallingConfig.AllowedFunctionNames))
	}
	if geminiReq.ToolConfig.FunctionCallingConfig.AllowedFunctionNames[0] != "get_weather" {
		t.Errorf("expected 'get_weather', got %s", geminiReq.ToolConfig.FunctionCallingConfig.AllowedFunctionNames[0])
	}
}

func TestTranslateToGeminiToolChoiceNone(t *testing.T) {
	tcJSON := json.RawMessage(`"none"`)
	req := &OpenAIRequest{
		Model: "gemini-2.5-flash",
		Messages: []OpenAIMessage{
			{Role: "user", Content: rawMsg("Hello")},
		},
		Tools: []OpenAITool{
			{Type: "function", Function: OpenAIFunction{Name: "fn", Description: "f", Parameters: json.RawMessage(`{"type":"object"}`)}},
		},
		ToolChoice: tcJSON,
	}

	geminiReq, err := translateToGemini(req)
	if err != nil {
		t.Fatalf("translateToGemini failed: %v", err)
	}

	if geminiReq.ToolConfig == nil {
		t.Fatal("expected tool config")
	}
	if geminiReq.ToolConfig.FunctionCallingConfig.Mode != "NONE" {
		t.Errorf("expected NONE, got %s", geminiReq.ToolConfig.FunctionCallingConfig.Mode)
	}
}

func TestTranslateToGeminiTopP(t *testing.T) {
	topP := 0.95
	req := &OpenAIRequest{
		Model: "gemini-2.5-flash",
		Messages: []OpenAIMessage{
			{Role: "user", Content: rawMsg("Hello")},
		},
		TopP: &topP,
	}

	geminiReq, err := translateToGemini(req)
	if err != nil {
		t.Fatalf("translateToGemini failed: %v", err)
	}

	if geminiReq.GenerationConfig == nil {
		t.Fatal("expected generation config")
	}
	if geminiReq.GenerationConfig.TopP == nil {
		t.Fatal("expected topP")
	}
	if *geminiReq.GenerationConfig.TopP != 0.95 {
		t.Errorf("expected topP 0.95, got %f", *geminiReq.GenerationConfig.TopP)
	}
}

func TestTranslateToGeminiNoToolsNoToolChoice(t *testing.T) {
	tcJSON := json.RawMessage(`"auto"`)
	req := &OpenAIRequest{
		Model: "gemini-2.5-flash",
		Messages: []OpenAIMessage{
			{Role: "user", Content: rawMsg("Hello")},
		},
		ToolChoice: tcJSON,
	}

	geminiReq, err := translateToGemini(req)
	if err != nil {
		t.Fatalf("translateToGemini failed: %v", err)
	}

	if geminiReq.ToolConfig != nil {
		t.Error("expected no tool config when no tools provided")
	}
}

func TestTranslateFromGeminiEmptyCandidates(t *testing.T) {
	resp := &GeminiResponse{
		Candidates: []GeminiCandidate{},
	}

	opt := translateFromGemini(resp, "gemini-2.5-flash", "chatcmpl-test", 1234567890)
	if len(opt.Choices) != 0 {
		t.Errorf("expected 0 choices for empty candidates, got %d", len(opt.Choices))
	}
}

func TestTranslateFromGeminiReasoningOnly(t *testing.T) {
	resp := &GeminiResponse{
		Candidates: []GeminiCandidate{
			{
				Content: GeminiContent{
					Role: "model",
					Parts: []GeminiPart{
						{Text: "internal reasoning process", Thought: true},
					},
				},
				FinishReason: "STOP",
			},
		},
	}

	opt := translateFromGemini(resp, "gemini-2.5-flash", "chatcmpl-test", 1234567890)

	if opt.Choices[0].Message.ReasoningContent != "internal reasoning process" {
		t.Errorf("expected reasoning content, got '%s'", opt.Choices[0].Message.ReasoningContent)
	}

	contentStr := string(opt.Choices[0].Message.Content)
	if contentStr != `""` {
		t.Errorf("expected Content to be empty string JSON, got '%s'", contentStr)
	}
}

func TestTranslateFromGeminiToolCalls(t *testing.T) {
	resp := &GeminiResponse{
		Candidates: []GeminiCandidate{
			{
				Content: GeminiContent{
					Role: "model",
					Parts: []GeminiPart{
						{Text: "Let me check that."},
						{
							FunctionCall: &GeminiFuncCall{
								Name: "get_weather",
								Args: json.RawMessage(`{"location":"Paris"}`),
							},
							ThoughtSignature: "sig_test",
						},
					},
				},
				FinishReason: "STOP",
			},
		},
	}

	reqID := "test-tool"
	opt := translateFromGemini(resp, "gemini-2.5-flash", reqID, 1234567890)

	if opt.Choices[0].Message == nil {
		t.Fatal("expected message")
	}
	if len(opt.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(opt.Choices[0].Message.ToolCalls))
	}
	tc := opt.Choices[0].Message.ToolCalls[0]
	if tc.Function.Name != "get_weather" {
		t.Errorf("expected 'get_weather', got '%s'", tc.Function.Name)
	}
	if tc.Function.Arguments != `{"location":"Paris"}` {
		t.Errorf("expected args, got '%s'", tc.Function.Arguments)
	}
	if tc.ID == "" {
		t.Fatal("expected non-empty tool call ID")
	}

	finishStr := *opt.Choices[0].FinishReason
	if finishStr != "tool_calls" {
		t.Errorf("expected finish_reason 'tool_calls', got '%s'", finishStr)
	}

	val, ok := thoughtSignatureCache.Load(tc.ID)
	if !ok {
		t.Fatal("expected thought signature cached")
	}
	if val != "sig_test" {
		t.Errorf("expected 'sig_test', got '%s'", val)
	}
}

func TestTranslateFromGeminiContentFilterFinish(t *testing.T) {
	tests := []struct {
		reason           string
		expectedFinishing string
	}{
		{"SAFETY", "content_filter"},
		{"RECITATION", "content_filter"},
		{"OTHER", "content_filter"},
		{"BLOCKLIST", "content_filter"},
	}

	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			resp := &GeminiResponse{
				Candidates: []GeminiCandidate{
					{
						Content:      GeminiContent{Role: "model", Parts: []GeminiPart{{Text: "filtered"}}},
						FinishReason: tt.reason,
					},
				},
			}
			opt := translateFromGemini(resp, "gemini-2.5-flash", "chatcmpl-test", 1234567890)
			if opt.Choices[0].FinishReason == nil {
				t.Fatal("expected finish_reason")
			}
			if *opt.Choices[0].FinishReason != tt.expectedFinishing {
				t.Errorf("expected '%s', got '%s'", tt.expectedFinishing, *opt.Choices[0].FinishReason)
			}
		})
	}
}

func TestTranslateStreamChunkToolCall(t *testing.T) {
	resp := &GeminiResponse{
		Candidates: []GeminiCandidate{
			{
				Content: GeminiContent{
					Role: "model",
					Parts: []GeminiPart{
						{
							FunctionCall: &GeminiFuncCall{
								Name: "search",
								Args: json.RawMessage(`{"q":"test"}`),
							},
						},
					},
				},
			},
		},
	}

	chunk := translateStreamChunk(resp, "gemini-2.5-flash", true, "chatcmpl-test", 1234567890)
	if len(chunk.Choices[0].Delta.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(chunk.Choices[0].Delta.ToolCalls))
	}
	tc := chunk.Choices[0].Delta.ToolCalls[0]
	if tc.Function.Name != "search" {
		t.Errorf("expected 'search', got '%s'", tc.Function.Name)
	}
	if tc.Function.Arguments != `{"q":"test"}` {
		t.Errorf("expected args, got '%s'", tc.Function.Arguments)
	}
}

func TestTranslateStreamChunkEmptyCandidates(t *testing.T) {
	resp := &GeminiResponse{
		Candidates: []GeminiCandidate{},
	}

	chunk := translateStreamChunk(resp, "gemini-2.5-flash", false, "chatcmpl-test", 1234567890)
	if len(chunk.Choices) != 0 {
		t.Errorf("expected 0 choices, got %d", len(chunk.Choices))
	}
}

func TestParseOpenAIContentString(t *testing.T) {
	raw := json.RawMessage(`"hello world"`)
	result := parseOpenAIContent(raw)
	if result != "hello world" {
		t.Errorf("expected 'hello world', got '%s'", result)
	}
}

func TestParseOpenAIContentArray(t *testing.T) {
	raw := json.RawMessage(`[{"type":"text","text":"part1"},{"type":"text","text":"part2"}]`)
	result := parseOpenAIContent(raw)
	if result != "part1\npart2" {
		t.Errorf("expected 'part1\\npart2', got '%s'", result)
	}
}

func TestParseOpenAIContentEmpty(t *testing.T) {
	result := parseOpenAIContent(nil)
	if result != "" {
		t.Errorf("expected empty, got '%s'", result)
	}
}

func TestExtractGeminiPartsFromContentTextOnly(t *testing.T) {
	raw := json.RawMessage(`"hello"`)
	parts := extractGeminiPartsFromContent(raw)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].Text != "hello" {
		t.Errorf("expected 'hello', got '%s'", parts[0].Text)
	}
}

func TestExtractGeminiPartsFromContentImage(t *testing.T) {
	raw := json.RawMessage(`[{"type":"image_url","image_url":{"url":"data:image/jpeg;base64,/9j/4AAQSkZJRg=="}}]`)
	parts := extractGeminiPartsFromContent(raw)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].InlineData == nil {
		t.Fatal("expected InlineData")
	}
	if parts[0].InlineData.MimeType != "image/jpeg" {
		t.Errorf("expected 'image/jpeg', got '%s'", parts[0].InlineData.MimeType)
	}
}

func TestExtractGeminiPartsFromContentMixed(t *testing.T) {
	raw := json.RawMessage(`[{"type":"text","text":"describe"},{"type":"image_url","image_url":{"url":"data:image/png;base64,YWJjMTIz"}}]`)
	parts := extractGeminiPartsFromContent(raw)
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if parts[0].Text != "describe" {
		t.Errorf("expected 'describe', got '%s'", parts[0].Text)
	}
	if parts[1].InlineData == nil {
		t.Fatal("expected InlineData for image")
	}
}

func TestTranslateToGeminiStopSequences(t *testing.T) {
	req := &OpenAIRequest{
		Model: "gemini-2.5-flash",
		Messages: []OpenAIMessage{
			{Role: "user", Content: rawMsg("Hello")},
		},
		Stop: []string{"STOP", "END"},
	}

	geminiReq, err := translateToGemini(req)
	if err != nil {
		t.Fatalf("translateToGemini failed: %v", err)
	}

	if geminiReq.GenerationConfig == nil {
		t.Fatal("expected generation config")
	}
	if len(geminiReq.GenerationConfig.StopSequences) != 2 {
		t.Fatalf("expected 2 stop sequences, got %d", len(geminiReq.GenerationConfig.StopSequences))
	}
}

func TestTranslateToGeminiTemperatureAndMaxTokens(t *testing.T) {
	temp := 0.7
	maxTokens := 2048
	req := &OpenAIRequest{
		Model: "gemini-2.5-flash",
		Messages: []OpenAIMessage{
			{Role: "user", Content: rawMsg("Hello")},
		},
		Temperature: &temp,
		MaxTokens:   &maxTokens,
	}

	geminiReq, err := translateToGemini(req)
	if err != nil {
		t.Fatalf("translateToGemini failed: %v", err)
	}

	if *geminiReq.GenerationConfig.Temperature != 0.7 {
		t.Errorf("expected temperature 0.7, got %f", *geminiReq.GenerationConfig.Temperature)
	}
	if *geminiReq.GenerationConfig.MaxOutputTokens != 2048 {
		t.Errorf("expected maxTokens 2048, got %d", *geminiReq.GenerationConfig.MaxOutputTokens)
	}
}

func TestCleanSchemaStripsUnsupportedProps(t *testing.T) {
	schema := json.RawMessage(`{
		"type": "object",
		"$schema": "http://json-schema.org/draft-07/schema#",
		"$comment": "test",
		"additionalProperties": false,
		"properties": {
			"name": {"type": "string"}
		}
	}`)

	cleaned, err := cleanSchema(schema)
	if err != nil {
		t.Fatalf("cleanSchema failed: %v", err)
	}

	var result map[string]interface{}
	json.Unmarshal(cleaned, &result)

	if _, ok := result["$schema"]; ok {
		t.Error("expected $schema to be stripped")
	}
	if _, ok := result["$comment"]; ok {
		t.Error("expected $comment to be stripped")
	}
	if _, ok := result["additionalProperties"]; ok {
		t.Error("expected additionalProperties to be stripped")
	}
	if _, ok := result["properties"]; !ok {
		t.Error("expected properties to be preserved")
	}
}

func TestTranslateToGeminiReasoningEffort(t *testing.T) {
	tests := []struct {
		effort        string
		expectedBudget int
	}{
		{"high", 8192},
		{"medium", 2048},
		{"low", 512},
		{"minimal", 128},
		{"none", 0},
	}

	for _, tt := range tests {
		t.Run(tt.effort, func(t *testing.T) {
			effort := tt.effort
			req := &OpenAIRequest{
				Model: "gemini-2.5-flash",
				Messages: []OpenAIMessage{
					{Role: "user", Content: rawMsg("Hello")},
				},
				ReasoningEffort: &effort,
			}

			geminiReq, err := translateToGemini(req)
			if err != nil {
				t.Fatalf("translateToGemini failed: %v", err)
			}

			if geminiReq.GenerationConfig == nil {
				t.Fatal("expected generation config")
			}
			if geminiReq.GenerationConfig.ThinkingConfig == nil {
				t.Fatal("expected thinking config")
			}
			if geminiReq.GenerationConfig.ThinkingConfig.ThinkingBudget != tt.expectedBudget {
				t.Errorf("expected thinking budget %d, got %d", tt.expectedBudget, geminiReq.GenerationConfig.ThinkingConfig.ThinkingBudget)
			}
		})
	}
}

func TestTranslateToGeminiReasoningEffortGemmaSkipped(t *testing.T) {
	effort := "high"
	req := &OpenAIRequest{
		Model: "gemma-4-31b-it",
		Messages: []OpenAIMessage{
			{Role: "user", Content: rawMsg("Hello")},
		},
		ReasoningEffort: &effort,
	}

	geminiReq, err := translateToGemini(req)
	if err != nil {
		t.Fatalf("translateToGemini failed: %v", err)
	}

	if geminiReq.GenerationConfig != nil && geminiReq.GenerationConfig.ThinkingConfig != nil {
		t.Error("expected no thinking config for gemma model")
	}
}

func TestTranslateToGeminiCandidateCount(t *testing.T) {
	n := 3
	req := &OpenAIRequest{
		Model: "gemini-2.5-flash",
		Messages: []OpenAIMessage{
			{Role: "user", Content: rawMsg("Hello")},
		},
		N: &n,
	}

	geminiReq, err := translateToGemini(req)
	if err != nil {
		t.Fatalf("translateToGemini failed: %v", err)
	}

	if geminiReq.GenerationConfig == nil {
		t.Fatal("expected generation config")
	}
	if geminiReq.GenerationConfig.CandidateCount == nil {
		t.Fatal("expected candidateCount")
	}
	if *geminiReq.GenerationConfig.CandidateCount != 3 {
		t.Errorf("expected candidateCount 3, got %d", *geminiReq.GenerationConfig.CandidateCount)
	}
}

func TestReasoningEffortToBudget(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"high", 8192},
		{"xhigh", 8192},
		{"medium", 2048},
		{"low", 512},
		{"minimal", 128},
		{"none", 0},
		{"unknown", 2048},
		{"", 2048},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := reasoningEffortToBudget(tt.input)
			if result != tt.expected {
				t.Errorf("reasoningEffortToBudget(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestExtractGeminiPartsFromContentInputAudio(t *testing.T) {
	raw := json.RawMessage(`[{"type":"input_audio","input_audio":{"data":"YXVkaW9kYXRh","format":"wav"}}]`)
	parts := extractGeminiPartsFromContent(raw)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].InlineData == nil {
		t.Fatal("expected InlineData")
	}
	if parts[0].InlineData.MimeType != "audio/wav" {
		t.Errorf("expected 'audio/wav', got '%s'", parts[0].InlineData.MimeType)
	}
	if parts[0].InlineData.Data != "YXVkaW9kYXRh" {
		t.Errorf("expected 'YXVkaW9kYXRh', got '%s'", parts[0].InlineData.Data)
	}
}

func TestExtractGeminiPartsFromContentInputAudioMP3(t *testing.T) {
	raw := json.RawMessage(`[{"type":"input_audio","input_audio":{"data":"bXAzZGF0YQ==","format":"mp3"}}]`)
	parts := extractGeminiPartsFromContent(raw)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].InlineData == nil {
		t.Fatal("expected InlineData")
	}
	if parts[0].InlineData.MimeType != "audio/mp3" {
		t.Errorf("expected 'audio/mp3', got '%s'", parts[0].InlineData.MimeType)
	}
	if parts[0].InlineData.Data != "bXAzZGF0YQ==" {
		t.Errorf("expected 'bXAzZGF0YQ==', got '%s'", parts[0].InlineData.Data)
	}
}

func TestExtractGeminiPartsFromContentAudioDataURI(t *testing.T) {
	raw := json.RawMessage(`[{"type":"audio_url","image_url":{"url":"data:audio/mp3;base64,YXVkaW9kYXRh"}}]`)
	parts := extractGeminiPartsFromContent(raw)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].InlineData == nil {
		t.Fatal("expected InlineData")
	}
	if parts[0].InlineData.MimeType != "audio/mp3" {
		t.Errorf("expected 'audio/mp3', got '%s'", parts[0].InlineData.MimeType)
	}
	if parts[0].InlineData.Data != "YXVkaW9kYXRh" {
		t.Errorf("expected 'YXVkaW9kYXRh', got '%s'", parts[0].InlineData.Data)
	}
}

func TestExtractGeminiPartsFromContentMixedAllTypes(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"text","text":"Describe this"},
		{"type":"image_url","image_url":{"url":"data:image/png;base64,YWJjMTIz"}},
		{"type":"input_audio","input_audio":{"data":"YXVkaW8xMjM=","format":"wav"}}
	]`)
	parts := extractGeminiPartsFromContent(raw)
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(parts))
	}
	if parts[0].Text != "Describe this" {
		t.Errorf("expected text 'Describe this', got '%s'", parts[0].Text)
	}
	if parts[1].InlineData == nil || parts[1].InlineData.MimeType != "image/png" {
		t.Errorf("expected image/png inline data")
	}
	if parts[2].InlineData == nil || parts[2].InlineData.MimeType != "audio/wav" {
		t.Errorf("expected audio/wav inline data")
	}
}
