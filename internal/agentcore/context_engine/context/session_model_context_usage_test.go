package context

import (
	"testing"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestStatMessages_取TotalTokens 验证统计口径为输入+输出总量
func TestStatMessages_取TotalTokens(t *testing.T) {
	mc := newTestSessionModelContext() // 同文件已有的测试构造助手

	am := llm_schema.NewAssistantMessage("hi")
	am.UsageMetadata = &llm_schema.UsageMetadata{InputTokens: 1200, OutputTokens: 80, TotalTokens: 1280}
	msgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello"), am}

	stat := &iface.ContextStats{}
	mc.statMessages(stat, msgs)
	// 本轮输出下一轮会作为历史重发，占用应算输入+输出（total_tokens）
	if stat.TotalTokens != 1280 {
		t.Errorf("应使用 TotalTokens=1280，实际 %d", stat.TotalTokens)
	}
}

// TestStatMessages_无总量时回退本地计数 验证 TotalTokens 缺失时走逐条计数
func TestStatMessages_无总量时回退本地计数(t *testing.T) {
	mc := newTestSessionModelContext()

	am := llm_schema.NewAssistantMessage("hi")
	am.UsageMetadata = &llm_schema.UsageMetadata{InputTokens: 2800}
	msgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello"), am}

	stat := &iface.ContextStats{}
	mc.statMessages(stat, msgs)
	// 只有输入侧、没有 total_tokens 的 provider：视为无 usage，改由 TokenCounter 估算
	if stat.TotalTokens == 2800 {
		t.Error("缺少 total_tokens 时不应直接采用 InputTokens")
	}
	if stat.TotalTokens <= 0 {
		t.Errorf("回退计数应得到正数，实际 %d", stat.TotalTokens)
	}
}
