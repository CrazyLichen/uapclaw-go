package recommendation

import (
	"time"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// answerWithOutputTokens 构造一条带 output token 用量的回答。
func answerWithOutputTokens(outputTokens int) *llmschema.AssistantMessage {
	return &llmschema.AssistantMessage{
		UsageMetadata: &llmschema.UsageMetadata{OutputTokens: outputTokens},
	}
}

// toolCallAnswer 构造一条带工具调用的回答。
func toolCallAnswer() *llmschema.AssistantMessage {
	return &llmschema.AssistantMessage{
		ToolCalls: []*llmschema.ToolCall{{ID: "call-1", Name: "some_tool"}},
	}
}

// hasTurnForTest 判断会话是否登记了在跑的轮。
func (f *PromptSuggestionForks) hasTurnForTest(sessionID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.turns[sessionID]
	return ok
}

// TurnForTest 返回会话当前登记的主调用（nil 表示尚未记录）。
func (f *PromptSuggestionForks) TurnForTest(sessionID string) *MainCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.turns[sessionID]
}

// setTaskForTest 直接塞一个伪在飞任务。
func (f *PromptSuggestionForks) setTaskForTest(sessionID string, onCancel func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks[sessionID] = onCancel
}

// inFlightCountForTest 返回在飞任务数。
func (f *PromptSuggestionForks) inFlightCountForTest() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.tasks)
}

// setAnsweredForTest 直接塞一条最近成功轮记录。
func (f *PromptSuggestionForks) setAnsweredForTest(sessionID, requestID string, call *MainCall) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answered[sessionID] = answeredCall{requestID: requestID, call: call}
}

// lastSuppressedReasonForTest 返回最近一次抑制原因。
func (f *PromptSuggestionForks) lastSuppressedReasonForTest() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastSuppressedReason
}

// waitInFlightForTest 等在飞任务收敛，超时即返回。
func (f *PromptSuggestionForks) waitInFlightForTest(timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if f.inFlightCountForTest() == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}
