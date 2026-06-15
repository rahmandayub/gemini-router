package proxy

import (
	"testing"
)

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
	if opt.Choices[0].Message.Content != "Hello, how can I help you today?" {
		t.Errorf("expected Content to be 'Hello, how can I help you today?', got '%s'", opt.Choices[0].Message.Content)
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
	if opt2.Choices[0].Message.Content != "Hello!" {
		t.Errorf("expected Content to be 'Hello!', got '%s'", opt2.Choices[0].Message.Content)
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
			{Role: "user", Content: "Call some tools"},
			{Role: "assistant", Content: "Thinking...", ToolCalls: []OpenAIToolCall{
				{ID: "call_tool1_0", Function: OpenAIToolCallFn{Name: "tool1", Arguments: `{"arg":1}`}},
				{ID: "call_tool2_1", Function: OpenAIToolCallFn{Name: "tool2", Arguments: `{"arg":2}`}},
			}},
			{Role: "tool", ToolCallID: "call_tool1_0", Content: `{"result":1}`},
			{Role: "tool", ToolCallID: "call_tool2_1", Content: `{"result":2}`},
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
	if toolBlock.Role != "function" {
		t.Errorf("expected block 2 role to be 'function', got '%s'", toolBlock.Role)
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
	if val.(string) != "opaque_signature_abc_123" {
		t.Errorf("expected cached signature to be 'opaque_signature_abc_123', got '%v'", val)
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
