package proxy

import (
	"encoding/json"
	"testing"
)

func TestMapGeminiFinishReasonToAnthropic(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"STOP", "end_turn"},
		{"MAX_TOKENS", "max_tokens"},
		{"SAFETY", "stop"},
		{"RECITATION", "stop"},
		{"OTHER", "stop"},
		{"BLOCKLIST", "stop"},
		{"MALFORMED_FUNCTION_CALL", "stop"},
		{"UNKNOWN_REASON", "stop"},
		{"", "stop"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := mapGeminiFinishReasonToAnthropic(tt.input)
			if result != tt.expected {
				t.Errorf("mapGeminiFinishReasonToAnthropic(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestTranslateAnthropicToolChoiceAuto(t *testing.T) {
	tc := &AnthropicToolChoice{Type: "auto"}
	config := translateAnthropicToolChoice(tc)
	if config.FunctionCallingConfig.Mode != "AUTO" {
		t.Errorf("expected AUTO, got %s", config.FunctionCallingConfig.Mode)
	}
}

func TestTranslateAnthropicToolChoiceAny(t *testing.T) {
	tc := &AnthropicToolChoice{Type: "any"}
	config := translateAnthropicToolChoice(tc)
	if config.FunctionCallingConfig.Mode != "ANY" {
		t.Errorf("expected ANY, got %s", config.FunctionCallingConfig.Mode)
	}
}

func TestTranslateAnthropicToolChoiceNone(t *testing.T) {
	tc := &AnthropicToolChoice{Type: "none"}
	config := translateAnthropicToolChoice(tc)
	if config.FunctionCallingConfig.Mode != "NONE" {
		t.Errorf("expected NONE, got %s", config.FunctionCallingConfig.Mode)
	}
}

func TestTranslateAnthropicToolChoiceTool(t *testing.T) {
	tc := &AnthropicToolChoice{Type: "tool", Name: "get_weather"}
	config := translateAnthropicToolChoice(tc)
	if config.FunctionCallingConfig.Mode != "ANY" {
		t.Errorf("expected ANY, got %s", config.FunctionCallingConfig.Mode)
	}
	if len(config.FunctionCallingConfig.AllowedFunctionNames) != 1 {
		t.Fatalf("expected 1 allowed function name, got %d", len(config.FunctionCallingConfig.AllowedFunctionNames))
	}
	if config.FunctionCallingConfig.AllowedFunctionNames[0] != "get_weather" {
		t.Errorf("expected 'get_weather', got %s", config.FunctionCallingConfig.AllowedFunctionNames[0])
	}
}

func TestTranslateAnthropicToolChoiceDefault(t *testing.T) {
	tc := &AnthropicToolChoice{Type: "unknown"}
	config := translateAnthropicToolChoice(tc)
	if config.FunctionCallingConfig.Mode != "AUTO" {
		t.Errorf("expected AUTO for unknown type, got %s", config.FunctionCallingConfig.Mode)
	}
}

func TestTranslateFromGeminiToAnthropicText(t *testing.T) {
	resp := &GeminiResponse{
		Candidates: []GeminiCandidate{
			{
				Content: GeminiContent{
					Role:  "model",
					Parts: []GeminiPart{{Text: "Hello from Gemini!"}},
				},
				FinishReason: "STOP",
			},
		},
		UsageMetadata: &GeminiUsageMetadata{
			PromptTokenCount:     10,
			CandidatesTokenCount: 5,
		},
	}

	anthropicResp := translateFromGeminiToAnthropic(resp, "gemini-2.5-flash", "msg_test123", false)

	if anthropicResp.ID != "msg_test123" {
		t.Errorf("expected ID 'msg_test123', got '%s'", anthropicResp.ID)
	}
	if anthropicResp.Type != "message" {
		t.Errorf("expected type 'message', got '%s'", anthropicResp.Type)
	}
	if anthropicResp.Role != "assistant" {
		t.Errorf("expected role 'assistant', got '%s'", anthropicResp.Role)
	}
	if anthropicResp.Model != "gemini-2.5-flash" {
		t.Errorf("expected model 'gemini-2.5-flash', got '%s'", anthropicResp.Model)
	}
	if anthropicResp.StopReason != "end_turn" {
		t.Errorf("expected stop_reason 'end_turn', got '%s'", anthropicResp.StopReason)
	}
	if len(anthropicResp.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(anthropicResp.Content))
	}
	textBlock, ok := anthropicResp.Content[0].(*AnthropicRespTextBlock)
	if !ok {
		t.Fatalf("expected *AnthropicRespTextBlock, got %T", anthropicResp.Content[0])
	}
	if textBlock.Text != "Hello from Gemini!" {
		t.Errorf("expected text 'Hello from Gemini!', got '%s'", textBlock.Text)
	}
	if anthropicResp.Usage == nil {
		t.Fatal("expected usage")
	}
	if anthropicResp.Usage.InputTokens != 10 {
		t.Errorf("expected input_tokens 10, got %d", anthropicResp.Usage.InputTokens)
	}
	if anthropicResp.Usage.OutputTokens != 5 {
		t.Errorf("expected output_tokens 5, got %d", anthropicResp.Usage.OutputTokens)
	}
}

func TestTranslateFromGeminiToAnthropicWithThinking(t *testing.T) {
	resp := &GeminiResponse{
		Candidates: []GeminiCandidate{
			{
				Content: GeminiContent{
					Role: "model",
					Parts: []GeminiPart{
						{Text: "Let me think...", Thought: true},
						{Text: "The answer is 42."},
					},
				},
				FinishReason: "STOP",
			},
		},
	}

	anthropicResp := translateFromGeminiToAnthropic(resp, "gemini-2.5-flash", "msg_test", true)

	if len(anthropicResp.Content) != 2 {
		t.Fatalf("expected 2 content blocks (thinking + text), got %d", len(anthropicResp.Content))
	}
	thinkingBlock, ok := anthropicResp.Content[0].(*AnthropicRespThinkingBlock)
	if !ok {
		t.Fatalf("expected *AnthropicRespThinkingBlock, got %T", anthropicResp.Content[0])
	}
	if thinkingBlock.Thinking != "Let me think..." {
		t.Errorf("expected thinking 'Let me think...', got '%s'", thinkingBlock.Thinking)
	}
	textBlock, ok := anthropicResp.Content[1].(*AnthropicRespTextBlock)
	if !ok {
		t.Fatalf("expected *AnthropicRespTextBlock, got %T", anthropicResp.Content[1])
	}
	if textBlock.Text != "The answer is 42." {
		t.Errorf("expected text 'The answer is 42.', got '%s'", textBlock.Text)
	}
}

func TestTranslateFromGeminiToAnthropicThinkingSuppressed(t *testing.T) {
	resp := &GeminiResponse{
		Candidates: []GeminiCandidate{
			{
				Content: GeminiContent{
					Role: "model",
					Parts: []GeminiPart{
						{Text: "internal thought", Thought: true},
						{Text: "visible answer"},
					},
				},
				FinishReason: "STOP",
			},
		},
	}

	anthropicResp := translateFromGeminiToAnthropic(resp, "gemini-2.5-flash", "msg_test", false)

	if len(anthropicResp.Content) != 1 {
		t.Fatalf("expected 1 content block (thinking suppressed), got %d", len(anthropicResp.Content))
	}
	textBlock, ok := anthropicResp.Content[0].(*AnthropicRespTextBlock)
	if !ok {
		t.Fatalf("expected *AnthropicRespTextBlock, got %T", anthropicResp.Content[0])
	}
	if textBlock.Text != "visible answer" {
		t.Errorf("expected text 'visible answer', got '%s'", textBlock.Text)
	}
}

func TestTranslateFromGeminiToAnthropicToolCall(t *testing.T) {
	resp := &GeminiResponse{
		Candidates: []GeminiCandidate{
			{
				Content: GeminiContent{
					Role: "model",
					Parts: []GeminiPart{
						{
							FunctionCall: &GeminiFuncCall{
								Name: "get_weather",
								Args: json.RawMessage(`{"location":"NYC"}`),
							},
						},
					},
				},
				FinishReason: "STOP",
			},
		},
	}

	anthropicResp := translateFromGeminiToAnthropic(resp, "gemini-2.5-flash", "msg_test", false)

	if anthropicResp.StopReason != "tool_use" {
		t.Errorf("expected stop_reason 'tool_use', got '%s'", anthropicResp.StopReason)
	}
	if len(anthropicResp.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(anthropicResp.Content))
	}
	toolBlock, ok := anthropicResp.Content[0].(*AnthropicRespToolUseBlock)
	if !ok {
		t.Fatalf("expected *AnthropicRespToolUseBlock, got %T", anthropicResp.Content[0])
	}
	if toolBlock.Name != "get_weather" {
		t.Errorf("expected name 'get_weather', got '%s'", toolBlock.Name)
	}
	if toolBlock.ID == "" {
		t.Fatal("expected non-empty tool ID")
	}
	if toolBlock.Input["location"] != "NYC" {
		t.Errorf("expected location 'NYC', got %v", toolBlock.Input["location"])
	}
}

func TestTranslateFromGeminiToAnthropicEmptyCandidates(t *testing.T) {
	resp := &GeminiResponse{
		Candidates: []GeminiCandidate{},
	}

	anthropicResp := translateFromGeminiToAnthropic(resp, "gemini-2.5-flash", "msg_test", false)

	if len(anthropicResp.Content) != 0 {
		t.Errorf("expected 0 content blocks, got %d", len(anthropicResp.Content))
	}
	if anthropicResp.Content == nil {
		t.Error("expected Content to be non-nil empty slice, not nil")
	}
	if anthropicResp.StopReason != "" {
		t.Errorf("expected empty stop_reason, got '%s'", anthropicResp.StopReason)
	}
	if anthropicResp.Usage == nil {
		t.Fatal("expected Usage to be set, got nil")
	}
}

func TestTranslateFromGeminiToAnthropicEmptyParts(t *testing.T) {
	resp := &GeminiResponse{
		Candidates: []GeminiCandidate{
			{
				Content:      GeminiContent{Role: "model", Parts: []GeminiPart{}},
				FinishReason: "STOP",
			},
		},
	}

	anthropicResp := translateFromGeminiToAnthropic(resp, "gemini-2.5-flash", "msg_test", false)

	if anthropicResp.Content == nil {
		t.Error("expected Content to be non-nil empty slice, got nil")
	}
	if len(anthropicResp.Content) != 0 {
		t.Errorf("expected 0 content blocks, got %d", len(anthropicResp.Content))
	}
	if anthropicResp.Usage == nil {
		t.Error("expected Usage to be set")
	}
	if anthropicResp.StopReason != "end_turn" {
		t.Errorf("expected stop_reason 'end_turn', got '%s'", anthropicResp.StopReason)
	}
}

func TestTranslateFromGeminiToAnthropicSafetyFinish(t *testing.T) {
	resp := &GeminiResponse{
		Candidates: []GeminiCandidate{
			{
				Content:      GeminiContent{Role: "model", Parts: []GeminiPart{{Text: "filtered"}}},
				FinishReason: "SAFETY",
			},
		},
	}

	anthropicResp := translateFromGeminiToAnthropic(resp, "gemini-2.5-flash", "msg_test", false)
	if anthropicResp.StopReason != "stop" {
		t.Errorf("expected stop_reason 'stop' for SAFETY, got '%s'", anthropicResp.StopReason)
	}
}

func TestTranslateFromGeminiToAnthropicToolCallThoughtSignature(t *testing.T) {
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
							ThoughtSignature: "sig_abc_123",
						},
					},
				},
				FinishReason: "STOP",
			},
		},
	}

	anthropicResp := translateFromGeminiToAnthropic(resp, "gemini-2.5-flash", "msg_test", false)

	toolBlock := anthropicResp.Content[0].(*AnthropicRespToolUseBlock)
	val, ok := thoughtSignatureCache.Load(toolBlock.ID)
	if !ok {
		t.Fatal("expected thought signature to be cached")
	}
	if val != "sig_abc_123" {
		t.Errorf("expected signature 'sig_abc_123', got '%s'", val)
	}
}

func TestParseAnthropicContentText(t *testing.T) {
	content := AnthropicContent("Hello world")
	toolUseMap := map[string]string{}
	parts := parseAnthropicContent(content, toolUseMap)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].Text != "Hello world" {
		t.Errorf("expected 'Hello world', got '%s'", parts[0].Text)
	}
}

func TestParseAnthropicContentEmptyString(t *testing.T) {
	content := AnthropicContent("")
	toolUseMap := map[string]string{}
	parts := parseAnthropicContent(content, toolUseMap)
	if len(parts) != 0 {
		t.Errorf("expected 0 parts for empty string, got %d", len(parts))
	}
}

func TestParseAnthropicContentTextBlock(t *testing.T) {
	content := AnthropicContent([]interface{}{
		map[string]interface{}{"type": "text", "text": "Hello"},
	})
	toolUseMap := map[string]string{}
	parts := parseAnthropicContent(content, toolUseMap)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].Text != "Hello" {
		t.Errorf("expected 'Hello', got '%s'", parts[0].Text)
	}
}

func TestParseAnthropicContentToolUse(t *testing.T) {
	content := AnthropicContent([]interface{}{
		map[string]interface{}{
			"type":  "tool_use",
			"id":    "toolu_123",
			"name":  "get_weather",
			"input": map[string]interface{}{"location": "NYC"},
		},
	})
	toolUseMap := map[string]string{"toolu_123": "get_weather"}
	parts := parseAnthropicContent(content, toolUseMap)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].FunctionCall == nil {
		t.Fatal("expected FunctionCall")
	}
	if parts[0].FunctionCall.Name != "get_weather" {
		t.Errorf("expected 'get_weather', got '%s'", parts[0].FunctionCall.Name)
	}
}

func TestParseAnthropicContentToolResult(t *testing.T) {
	content := AnthropicContent([]interface{}{
		map[string]interface{}{
			"type":       "tool_result",
			"tool_use_id": "toolu_123",
			"content":    "result text",
		},
	})
	toolUseMap := map[string]string{"toolu_123": "get_weather"}
	parts := parseAnthropicContent(content, toolUseMap)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].FunctionResponse == nil {
		t.Fatal("expected FunctionResponse")
	}
	if parts[0].FunctionResponse.Name != "get_weather" {
		t.Errorf("expected 'get_weather', got '%s'", parts[0].FunctionResponse.Name)
	}
}

func TestParseAnthropicContentToolResultWithError(t *testing.T) {
	content := AnthropicContent([]interface{}{
		map[string]interface{}{
			"type":        "tool_result",
			"tool_use_id": "toolu_123",
			"content":     "something went wrong",
			"is_error":    true,
		},
	})
	toolUseMap := map[string]string{"toolu_123": "get_weather"}
	parts := parseAnthropicContent(content, toolUseMap)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	resp := parts[0].FunctionResponse
	if resp == nil {
		t.Fatal("expected FunctionResponse")
	}
	resultVal, ok := resp.Response["result"]
	if !ok {
		t.Fatal("expected 'result' key in response")
	}
	errMap, ok := resultVal.(map[string]interface{})
	if !ok {
		t.Fatalf("expected error map, got %T: %v", resultVal, resultVal)
	}
	if errMap["error"] != "something went wrong" {
		t.Errorf("expected 'something went wrong', got '%v'", errMap["error"])
	}
}

func TestParseAnthropicContentToolResultArrayContent(t *testing.T) {
	content := AnthropicContent([]interface{}{
		map[string]interface{}{
			"type":        "tool_result",
			"tool_use_id": "toolu_123",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "line 1"},
				map[string]interface{}{"type": "text", "text": "line 2"},
			},
		},
	})
	toolUseMap := map[string]string{"toolu_123": "get_weather"}
	parts := parseAnthropicContent(content, toolUseMap)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	resp := parts[0].FunctionResponse
	if resp == nil {
		t.Fatal("expected FunctionResponse")
	}
	result, ok := resp.Response["result"].(string)
	if !ok {
		t.Fatalf("expected result string, got %T", resp.Response["result"])
	}
	if result != "line 1\nline 2" {
		t.Errorf("expected 'line 1\\nline 2', got '%s'", result)
	}
}

func TestParseAnthropicContentThinking(t *testing.T) {
	content := AnthropicContent([]interface{}{
		map[string]interface{}{
			"type":     "thinking",
			"thinking": "internal reasoning",
		},
	})
	toolUseMap := map[string]string{}
	parts := parseAnthropicContent(content, toolUseMap)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if !parts[0].Thought {
		t.Error("expected Thought=true")
	}
	if parts[0].Text != "internal reasoning" {
		t.Errorf("expected 'internal reasoning', got '%s'", parts[0].Text)
	}
}

func TestParseAnthropicContentImageBase64(t *testing.T) {
	content := AnthropicContent([]interface{}{
		map[string]interface{}{
			"type": "image",
			"source": map[string]interface{}{
				"type":       "base64",
				"media_type": "image/png",
				"data":       "abc123base64",
			},
		},
	})
	toolUseMap := map[string]string{}
	parts := parseAnthropicContent(content, toolUseMap)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].InlineData == nil {
		t.Fatal("expected InlineData")
	}
	if parts[0].InlineData.MimeType != "image/png" {
		t.Errorf("expected mimeType 'image/png', got '%s'", parts[0].InlineData.MimeType)
	}
	if parts[0].InlineData.Data != "abc123base64" {
		t.Errorf("expected data 'abc123base64', got '%s'", parts[0].InlineData.Data)
	}
}

func TestParseAnthropicContentImageURLSkipped(t *testing.T) {
	content := AnthropicContent([]interface{}{
		map[string]interface{}{
			"type": "image",
			"source": map[string]interface{}{
				"type": "url",
				"url":  "https://example.com/img.png",
			},
		},
	})
	toolUseMap := map[string]string{}
	parts := parseAnthropicContent(content, toolUseMap)
	if len(parts) != 0 {
		t.Errorf("expected 0 parts for URL image (unsupported), got %d", len(parts))
	}
}

func TestParseAnthropicContentAudioBase64(t *testing.T) {
	content := AnthropicContent([]interface{}{
		map[string]interface{}{
			"type": "audio",
			"source": map[string]interface{}{
				"type":       "base64",
				"media_type": "audio/wav",
				"data":       "base64audiodata",
			},
		},
	})
	toolUseMap := map[string]string{}
	parts := parseAnthropicContent(content, toolUseMap)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].InlineData == nil {
		t.Fatal("expected InlineData")
	}
	if parts[0].InlineData.MimeType != "audio/wav" {
		t.Errorf("expected mimeType 'audio/wav', got '%s'", parts[0].InlineData.MimeType)
	}
	if parts[0].InlineData.Data != "base64audiodata" {
		t.Errorf("expected data 'base64audiodata', got '%s'", parts[0].InlineData.Data)
	}
}

func TestParseAnthropicContentAudioURLSkipped(t *testing.T) {
	content := AnthropicContent([]interface{}{
		map[string]interface{}{
			"type": "audio",
			"source": map[string]interface{}{
				"type": "url",
				"url":  "https://example.com/audio.wav",
			},
		},
	})
	toolUseMap := map[string]string{}
	parts := parseAnthropicContent(content, toolUseMap)
	if len(parts) != 0 {
		t.Errorf("expected 0 parts for URL audio (fetch failed), got %d", len(parts))
	}
}

func TestParseAnthropicContentMixedWithAudio(t *testing.T) {
	content := AnthropicContent([]interface{}{
		map[string]interface{}{"type": "text", "text": "Listen to this"},
		map[string]interface{}{
			"type": "audio",
			"source": map[string]interface{}{
				"type":       "base64",
				"media_type": "audio/mp3",
				"data":       "mp3data",
			},
		},
		map[string]interface{}{"type": "text", "text": "and describe it"},
	})
	toolUseMap := map[string]string{}
	parts := parseAnthropicContent(content, toolUseMap)
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(parts))
	}
	if parts[0].Text != "Listen to this" {
		t.Errorf("expected 'Listen to this', got '%s'", parts[0].Text)
	}
	if parts[1].InlineData == nil || parts[1].InlineData.MimeType != "audio/mp3" {
		t.Errorf("expected audio/mp3 inline data")
	}
	if parts[2].Text != "and describe it" {
		t.Errorf("expected 'and describe it', got '%s'", parts[2].Text)
	}
}

func TestParseAnthropicContentMixedBlocks(t *testing.T) {
	content := AnthropicContent([]interface{}{
		map[string]interface{}{"type": "text", "text": "Hello"},
		map[string]interface{}{
			"type":     "thinking",
			"thinking": "reasoning here",
		},
		map[string]interface{}{"type": "text", "text": "World"},
	})
	toolUseMap := map[string]string{}
	parts := parseAnthropicContent(content, toolUseMap)
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(parts))
	}
	if parts[0].Text != "Hello" {
		t.Errorf("expected 'Hello', got '%s'", parts[0].Text)
	}
	if !parts[1].Thought {
		t.Error("expected Thought=true for thinking block")
	}
	if parts[2].Text != "World" {
		t.Errorf("expected 'World', got '%s'", parts[2].Text)
	}
}

func TestParseAnthropicContentUnknownType(t *testing.T) {
	content := AnthropicContent([]interface{}{
		map[string]interface{}{"type": "unknown_type", "data": "something"},
	})
	toolUseMap := map[string]string{}
	parts := parseAnthropicContent(content, toolUseMap)
	if len(parts) != 0 {
		t.Errorf("expected 0 parts for unknown type, got %d", len(parts))
	}
}

func TestTranslateAnthropicToGeminiWithTools(t *testing.T) {
	req := &AnthropicRequest{
		Model: "gemini-2.5-flash",
		Messages: []AnthropicMessage{
			{Role: "user", Content: AnthropicContent("What is the weather?")},
		},
		MaxTokens: 1024,
		Tools: []AnthropicTool{
			{
				Name:        "get_weather",
				Description: "Get weather info",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}}}`),
			},
		},
		ToolChoice: &AnthropicToolChoice{Type: "any"},
	}

	geminiReq, err := translateAnthropicToGemini(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(geminiReq.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(geminiReq.Tools))
	}
	if geminiReq.Tools[0].FunctionDeclarations[0].Name != "get_weather" {
		t.Errorf("expected 'get_weather', got '%s'", geminiReq.Tools[0].FunctionDeclarations[0].Name)
	}

	if geminiReq.ToolConfig == nil {
		t.Fatal("expected tool config")
	}
	if geminiReq.ToolConfig.FunctionCallingConfig.Mode != "ANY" {
		t.Errorf("expected ANY, got %s", geminiReq.ToolConfig.FunctionCallingConfig.Mode)
	}
}

func TestTranslateAnthropicToGeminiWithTopPTopK(t *testing.T) {
	topP := 0.9
	topK := 40
	req := &AnthropicRequest{
		Model: "gemini-2.5-flash",
		Messages: []AnthropicMessage{
			{Role: "user", Content: AnthropicContent("Hello")},
		},
		MaxTokens: 1024,
		TopP:      &topP,
		TopK:      &topK,
	}

	geminiReq, err := translateAnthropicToGemini(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if geminiReq.GenerationConfig == nil {
		t.Fatal("expected generation config")
	}
	if geminiReq.GenerationConfig.TopP == nil {
		t.Fatal("expected top_p")
	}
	if *geminiReq.GenerationConfig.TopP != 0.9 {
		t.Errorf("expected top_p 0.9, got %f", *geminiReq.GenerationConfig.TopP)
	}
	if geminiReq.GenerationConfig.TopK == nil {
		t.Fatal("expected top_k")
	}
	if *geminiReq.GenerationConfig.TopK != 40 {
		t.Errorf("expected top_k 40, got %d", *geminiReq.GenerationConfig.TopK)
	}
}

func TestTranslateAnthropicToGeminiSystemString(t *testing.T) {
	sysJSON, _ := json.Marshal("You are a helpful assistant")
	req := &AnthropicRequest{
		Model: "gemini-2.5-flash",
		System: sysJSON,
		Messages: []AnthropicMessage{
			{Role: "user", Content: AnthropicContent("Hello")},
		},
		MaxTokens: 1024,
	}

	geminiReq, err := translateAnthropicToGemini(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if geminiReq.SystemInstruction == nil {
		t.Fatal("expected system instruction")
	}
	if geminiReq.SystemInstruction.Parts[0].Text != "You are a helpful assistant" {
		t.Errorf("expected 'You are a helpful assistant', got '%s'", geminiReq.SystemInstruction.Parts[0].Text)
	}
}

func TestTranslateAnthropicToGeminiSystemArray(t *testing.T) {
	sysJSON, _ := json.Marshal([]AnthropicTextBlock{
		{Type: "text", Text: "Block 1"},
		{Type: "text", Text: "Block 2"},
	})
	req := &AnthropicRequest{
		Model: "gemini-2.5-flash",
		System: sysJSON,
		Messages: []AnthropicMessage{
			{Role: "user", Content: AnthropicContent("Hello")},
		},
		MaxTokens: 1024,
	}

	geminiReq, err := translateAnthropicToGemini(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if geminiReq.SystemInstruction == nil {
		t.Fatal("expected system instruction")
	}
	if len(geminiReq.SystemInstruction.Parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(geminiReq.SystemInstruction.Parts))
	}
}

func TestTranslateAnthropicToGeminiMultiTurn(t *testing.T) {
	req := &AnthropicRequest{
		Model: "gemini-2.5-flash",
		Messages: []AnthropicMessage{
			{Role: "user", Content: AnthropicContent("Hello")},
			{Role: "assistant", Content: AnthropicContent([]interface{}{
				map[string]interface{}{"type": "thinking", "thinking": "reasoning"},
				map[string]interface{}{"type": "text", "text": "Hi there!"},
			})},
			{Role: "user", Content: AnthropicContent("How are you?")},
		},
		MaxTokens: 1024,
		Thinking: &AnthropicThinking{
			Type:         "enabled",
			BudgetTokens: intPtr(1024),
		},
	}

	geminiReq, err := translateAnthropicToGemini(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(geminiReq.Contents) != 3 {
		t.Fatalf("expected 3 contents, got %d", len(geminiReq.Contents))
	}
	if geminiReq.Contents[0].Role != "user" {
		t.Errorf("expected 'user', got '%s'", geminiReq.Contents[0].Role)
	}
	if geminiReq.Contents[1].Role != "model" {
		t.Errorf("expected 'model', got '%s'", geminiReq.Contents[1].Role)
	}
	if geminiReq.Contents[2].Role != "user" {
		t.Errorf("expected 'user', got '%s'", geminiReq.Contents[2].Role)
	}

	if geminiReq.GenerationConfig == nil {
		t.Fatal("expected generation config")
	}
	if geminiReq.GenerationConfig.ThinkingConfig == nil {
		t.Fatal("expected thinking config")
	}
	if geminiReq.GenerationConfig.ThinkingConfig.ThinkingBudget != 1024 {
		t.Errorf("expected thinking budget 1024, got %d", geminiReq.GenerationConfig.ThinkingConfig.ThinkingBudget)
	}
}

func TestTranslateAnthropicToGeminiGemmaNoThinking(t *testing.T) {
	budget := 1024
	req := &AnthropicRequest{
		Model: "gemma-4-31b-it",
		Messages: []AnthropicMessage{
			{Role: "user", Content: AnthropicContent("Hello")},
		},
		MaxTokens: 1024,
		Thinking: &AnthropicThinking{
			Type:         "enabled",
			BudgetTokens: &budget,
		},
	}

	geminiReq, err := translateAnthropicToGemini(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if geminiReq.GenerationConfig.ThinkingConfig != nil {
		t.Error("expected no thinking config for gemma model")
	}
}

func TestTranslateAnthropicToGeminiToolResultRoleIsUser(t *testing.T) {
	req := &AnthropicRequest{
		Model: "gemini-2.5-flash",
		Messages: []AnthropicMessage{
			{Role: "user", Content: AnthropicContent("What is the weather?")},
			{Role: "assistant", Content: AnthropicContent([]interface{}{
				map[string]interface{}{
					"type":  "tool_use",
					"id":    "toolu_abc",
					"name":  "get_weather",
					"input": map[string]interface{}{"location": "NYC"},
				},
			})},
			{Role: "user", Content: AnthropicContent([]interface{}{
				map[string]interface{}{
					"type":        "tool_result",
					"tool_use_id": "toolu_abc",
					"content":     `{"temp": 72}`,
				},
			})},
		},
		MaxTokens: 1024,
	}

	geminiReq, err := translateAnthropicToGemini(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find the content block with the function response
	foundFuncResponse := false
	for _, c := range geminiReq.Contents {
		for _, p := range c.Parts {
			if p.FunctionResponse != nil {
				foundFuncResponse = true
				// Verify the role is "user", not "function"
				if c.Role != "user" {
					t.Errorf("expected role 'user' for function response content, got '%s'", c.Role)
				}
			}
		}
	}

	if !foundFuncResponse {
		t.Fatal("expected to find a function response in contents")
	}
}

func intPtr(i int) *int {
	return &i
}
