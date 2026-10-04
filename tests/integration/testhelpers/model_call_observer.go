package testhelpers

import (
	"context"
	"strings"
	"sync"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ModelCallRecord 记录一次模型调用的消息。
type ModelCallRecord struct {
	// Messages 模型调用的消息列表
	Messages []llmschema.BaseMessage
}

// ModelCallObserver 模型调用观测器，对齐 Python _ModelCallObserver。
//
// 钩入 BeforeModelCall，记录每次模型调用的消息列表。
// 用于验证 [STEERING] 文本是否出现在模型消息中。
//
// Python: tests/system_tests/harness/test_steer_inner_loop.py _ModelCallObserver
type ModelCallObserver struct {
	agentinterfaces.BaseRail
	// mu 保护并发访问
	mu sync.Mutex
	// records 模型调用记录列表
	records []ModelCallRecord
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewModelCallObserver 创建模型调用观测器。
func NewModelCallObserver() *ModelCallObserver {
	return &ModelCallObserver{}
}

// BeforeModelCall 记录模型调用消息。
func (o *ModelCallObserver) BeforeModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if cbc == nil || cbc.Inputs() == nil {
		return nil
	}
	modelInputs, ok := cbc.Inputs().(*agentinterfaces.ModelCallInputs)
	if !ok || modelInputs == nil || modelInputs.Messages == nil {
		return nil
	}
	o.mu.Lock()
	o.records = append(o.records, ModelCallRecord{
		Messages: modelInputs.Messages,
	})
	o.mu.Unlock()
	return nil
}

// CallCount 返回模型调用次数。
func (o *ModelCallObserver) CallCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.records)
}

// GetMessages 返回第 i 次（0-indexed）模型调用的消息列表。
func (o *ModelCallObserver) GetMessages(i int) []llmschema.BaseMessage {
	o.mu.Lock()
	defer o.mu.Unlock()
	if i < 0 || i >= len(o.records) {
		return nil
	}
	return o.records[i].Messages
}

// SteerSeenInMessages 检查 [STEERING] 是否出现在任一模型调用消息中。
// 对齐 Python LoopObserveRail.steer_seen_in_model_messages。
func (o *ModelCallObserver) SteerSeenInMessages() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, record := range o.records {
		for _, msg := range record.Messages {
			content := msg.GetContent()
			if content.IsText() {
				text := content.Text()
				if text != "" && strings.Contains(text, "[STEERING]") {
					return true
				}
			}
		}
	}
	return false
}

// ──────────────────────────── 非导出函数 ────────────────────────────
