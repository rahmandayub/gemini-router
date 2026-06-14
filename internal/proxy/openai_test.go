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
