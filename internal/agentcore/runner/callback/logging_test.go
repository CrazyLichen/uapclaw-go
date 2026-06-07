package callback

import (
	"context"
	"fmt"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

func TestLoggingLLMCallback_AllFields(t *testing.T) {
	temp := 0.8
	topP := 0.95
	maxTokens := 4096
	usage := llmschema.NewUsageMetadata()
	usage.InputTokens = 100
	usage.OutputTokens = 200
	usage.TotalTokens = 300

	tests := []struct {
		name  string
		event LLMCallEventType
		data  *LLMCallEventData
	}{
		{
			name:  "LLMCallStarted-完整字段",
			event: LLMCallStarted,
			data: &LLMCallEventData{
				Event:         LLMCallStarted,
				ModelName:     "gpt-4",
				ModelProvider: "OpenAI",
				Temperature:   &temp,
				TopP:          &topP,
				MaxTokens:     &maxTokens,
				Messages:      []string{"hello"},
				Tools:         []string{"tool1"},
				IsStream:      false,
				Extra:         map[string]any{"session_id": "abc"},
			},
		},
		{
			name:  "LLMCallError-完整字段",
			event: LLMCallError,
			data: &LLMCallEventData{
				Event:         LLMCallError,
				ModelName:     "gpt-4",
				ModelProvider: "OpenAI",
				Error:         fmt.Errorf("timeout"),
				Messages:      []string{"hello"},
				Tools:         []string{"tool1"},
				IsStream:      true,
				Extra:         map[string]any{"retry": 3},
			},
		},
		{
			name:  "LLMInvokeOutput-带Usage",
			event: LLMInvokeOutput,
			data: &LLMCallEventData{
				Event:         LLMInvokeOutput,
				ModelName:     "gpt-4",
				ModelProvider: "OpenAI",
				Usage:         usage,
				Response:      llmschema.NewAssistantMessage("hi"),
				IsStream:      false,
				Extra:         map[string]any{"key": "val"},
			},
		},
		{
			name:  "LLMResponseReceived-无可选字段",
			event: LLMResponseReceived,
			data: &LLMCallEventData{
				Event:         LLMResponseReceived,
				ModelName:     "gpt-4",
				ModelProvider: "OpenAI",
				IsStream:      false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			LoggingLLMCallback(context.Background(), tt.data)
		})
	}
}
