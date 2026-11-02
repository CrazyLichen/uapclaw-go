package context

import (
	"testing"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestStatMessages_优先InputTokens 验证统计口径优先输入侧 token
func TestStatMessages_优先InputTokens(t *testing.T) {
	mc := newTestSessionModelContext() // 同文件已有的测试构造助手

	am := llm_schema.NewAssistantMessage("hi")
	am.UsageMetadata = &llm_schema.UsageMetadata{InputTokens: 1200, OutputTokens: 80, TotalTokens: 1280}
	msgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello"), am}

	stat := &iface.ContextStats{}
	mc.statMessages(stat, msgs)
	if stat.TotalTokens != 1200 {
		t.Errorf("应使用 InputTokens=1200，实际 %d", stat.TotalTokens)
	}
}

// TestStatMessages_回退TotalTokens 验证 InputTokens 缺失时回退总量
func TestStatMessages_回退TotalTokens(t *testing.T) {
	mc := newTestSessionModelContext()

	am := llm_schema.NewAssistantMessage("hi")
	am.UsageMetadata = &llm_schema.UsageMetadata{TotalTokens: 2800}
	msgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello"), am}

	stat := &iface.ContextStats{}
	mc.statMessages(stat, msgs)
	if stat.TotalTokens != 2800 {
		t.Errorf("应回退 TotalTokens=2800，实际 %d", stat.TotalTokens)
	}
}
