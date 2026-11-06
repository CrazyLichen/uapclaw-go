package iface

import (
	"testing"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// assistantWithUsage 构造带用量的 assistant 消息。
func assistantWithUsage(text string, totalTokens int) *llm_schema.AssistantMessage {
	msg := llm_schema.NewAssistantMessage(text)
	msg.UsageMetadata = &llm_schema.UsageMetadata{TotalTokens: totalTokens, InputTokens: totalTokens}
	return msg
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestHasValidUsageMetadata_四种情况 覆盖有效/失效/无用 assistant/非 assistant。
func TestHasValidUsageMetadata_四种情况(t *testing.T) {
	valid := assistantWithUsage("ok", 100)
	if !HasValidUsageMetadata(valid) {
		t.Fatal("带正用量且未标记应为有效")
	}

	InvalidateUsageMetadata([]llm_schema.BaseMessage{valid})
	if HasValidUsageMetadata(valid) {
		t.Fatal("打过期标记后应失效")
	}

	zero := llm_schema.NewAssistantMessage("zero")
	zero.UsageMetadata = &llm_schema.UsageMetadata{TotalTokens: 0}
	if HasValidUsageMetadata(zero) {
		t.Fatal("总量为 0 应视为无效")
	}

	if HasValidUsageMetadata(llm_schema.NewUserMessage("user")) {
		t.Fatal("非 assistant 消息不应有有效用量")
	}
}

// TestInvalidateUsageMetadata_只标记有用量的assistant 验证不改动其它消息。
func TestInvalidateUsageMetadata_只标记有用量的assistant(t *testing.T) {
	plain := llm_schema.NewAssistantMessage("plain")
	withUsage := assistantWithUsage("usage", 50)
	user := llm_schema.NewUserMessage("u")

	InvalidateUsageMetadata([]llm_schema.BaseMessage{plain, withUsage, user})

	if metadata := plain.GetMetadata(); metadata != nil {
		if stale, _ := metadata[ContextUsageStaleKey].(bool); stale {
			t.Fatal("无用量的 assistant 不应被打标记")
		}
	}
	if stale, _ := withUsage.GetMetadata()[ContextUsageStaleKey].(bool); !stale {
		t.Fatal("有用量的 assistant 应被打标记")
	}
	if metadata := user.GetMetadata(); metadata != nil {
		if stale, _ := metadata[ContextUsageStaleKey].(bool); stale {
			t.Fatal("用户消息不应被打标记")
		}
	}
}

// TestStatMessages_过期基线回落到逐条计数 验证早退分支被跳过（B-06 回归）。
func TestStatMessages_过期基线回落到逐条计数(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("hello world"),
		assistantWithUsage("answer", 5000),
	}

	stats := &ContextStats{}
	stats.StatMessages(messages, nil)
	if stats.TotalTokens != 5000 {
		t.Fatalf("有效基线应被采用，got %d", stats.TotalTokens)
	}

	InvalidateUsageMetadata(messages)
	after := &ContextStats{}
	after.StatMessages(messages, nil)
	if after.TotalTokens == 5000 {
		t.Fatalf("基线过期后不得沿用旧值，got %d", after.TotalTokens)
	}
	if after.TotalTokens <= 0 {
		t.Fatalf("过期后应回落到逐条计数（>0），got %d", after.TotalTokens)
	}
	if after.SystemMessageTokens+after.UserMessageTokens+after.AssistantMessageTokens+after.ToolMessageTokens == 0 {
		t.Fatal("回落路径应填充分类 token")
	}
}

// TestCountCategoryTokens_不依赖基线 验证即使存在有效基线也照常分类计数。
func TestCountCategoryTokens_不依赖基线(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewSystemMessage("你是助手"),
		llm_schema.NewUserMessage("问题"),
		assistantWithUsage("答复", 9999),
	}
	system, tools, conversation := CountCategoryTokens(messages, nil, nil)
	if system <= 0 {
		t.Fatalf("系统提示词 token 应 >0，got %d", system)
	}
	if conversation <= 0 {
		t.Fatalf("消息 token 应 >0，got %d", conversation)
	}
	if tools != 0 {
		t.Fatalf("未传工具时工具类别应为 0，got %d", tools)
	}
	if conversation == 9999 {
		t.Fatal("分类计数不得复用 usage 基线数字")
	}
}
