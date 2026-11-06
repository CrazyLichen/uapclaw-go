package context

import (
	"context"
	"testing"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// usageAssistant 构造带用量的 assistant 消息并分配 context_message_id。
func usageAssistant(text string, totalTokens int) *llm_schema.AssistantMessage {
	msg := llm_schema.NewAssistantMessage(text)
	msg.UsageMetadata = &llm_schema.UsageMetadata{TotalTokens: totalTokens, InputTokens: totalTokens}
	return msg
}

// withBaseLineContext 构造带一条有效基线的上下文（用户问题 + 带用量答复）。
func withBaseLineContext(t *testing.T, totalTokens int) *SessionModelContext {
	t.Helper()
	mc := newTestSessionModelContext()
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"),
		usageAssistant("答复", totalTokens),
	}
	EnsureContextMessageIDs(messages)
	mc.SetMessages(messages, true)
	// SetMessages 会把刚写入的基线标记为过期，这里重建场景：先恢复有效状态
	for _, message := range messages {
		if assistant, ok := message.(*llm_schema.AssistantMessage); ok && assistant.UsageMetadata != nil {
			assistant.SetMetadata(map[string]any{ContextMessageIDKey: GetContextMessageID(assistant)})
		}
	}
	mc.SetMessages(messages, true)
	clearUsageStale(messages)
	return mc
}

// clearUsageStale 去掉过期标记，模拟"上一轮模型刚返回、基线仍然有效"。
func clearUsageStale(messages []llm_schema.BaseMessage) {
	for _, message := range messages {
		assistant, ok := message.(*llm_schema.AssistantMessage)
		if !ok || assistant.UsageMetadata == nil {
			continue
		}
		metadata := assistant.GetMetadata()
		if metadata == nil {
			continue
		}
		delete(metadata, "context_usage_stale")
		assistant.SetMetadata(metadata)
	}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestStatistic_压缩后总量回落 回归 B-06：改写上下文后不得沿用压缩前的用量数字。
func TestStatistic_压缩后总量回落(t *testing.T) {
	mc := withBaseLineContext(t, 50000)
	if got := mc.Statistic().TotalTokens; got != 50000 {
		t.Fatalf("基线有效时应采用 50000，got %d", got)
	}

	// 模拟压缩：保留用户问题 + 摘要消息替换原答复
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"),
		llm_schema.NewUserMessage("[SUMMARY] 压缩摘要"),
	}
	mc.SetMessages(messages, true)

	after := mc.Statistic().TotalTokens
	if after == 50000 {
		t.Fatalf("压缩后不得沿用压缩前的 50000，got %d", after)
	}
	if after <= 0 {
		t.Fatalf("压缩后应回落到现算值（>0），got %d", after)
	}
}

// TestPopMessages_使基线失效 验证弹出消息后用量作废。
func TestPopMessages_使基线失效(t *testing.T) {
	mc := withBaseLineContext(t, 40000)
	if got := mc.Statistic().TotalTokens; got != 40000 {
		t.Fatalf("前置：基线应有效，got %d", got)
	}
	mc.PopMessages(1, true)
	if got := mc.Statistic().TotalTokens; got == 40000 {
		t.Fatalf("弹出后不得沿用旧基线，got %d", got)
	}
}

// TestAddMessages_纯追加保留基线 验证纯追加不作废（尾部增量另行计入）。
func TestAddMessages_纯追加保留基线(t *testing.T) {
	mc := withBaseLineContext(t, 30000)
	before := mc.Statistic().TotalTokens

	_, err := mc.AddMessages(context.Background(), []llm_schema.BaseMessage{llm_schema.NewUserMessage("下一问")})
	if err != nil {
		t.Fatalf("AddMessages 失败: %v", err)
	}
	if got := mc.Statistic().TotalTokens; got != before {
		t.Fatalf("纯追加不应改动基线（got %d, want %d）", got, before)
	}
}

// TestLoadState_使基线失效 验证恢复快照后用量作废。
func TestLoadState_使基线失效(t *testing.T) {
	mc := newTestSessionModelContext()
	state := map[string]any{
		"messages": []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("问题"),
			usageAssistant("答复", 12345),
		},
	}
	mc.LoadState(state)
	if got := mc.Statistic().TotalTokens; got == 12345 {
		t.Fatalf("恢复快照后不得沿用旧基线，got %d", got)
	}
}

// TestUpdateModelContext_使基线失效 验证换模型绑定后用量作废。
func TestUpdateModelContext_使基线失效(t *testing.T) {
	mc := withBaseLineContext(t, 20000)
	if got := mc.Statistic().TotalTokens; got != 20000 {
		t.Fatalf("前置：基线应有效，got %d", got)
	}
	mc.UpdateModelContext("another-model", 128000)
	if got := mc.Statistic().TotalTokens; got == 20000 {
		t.Fatalf("换模型后不得沿用旧绑定下的基线，got %d", got)
	}
}

// TestRetainedMessagePrefixPreserved 验证前缀判定（长度缩短/元素替换都判否）。
func TestRetainedMessagePrefixPreserved(t *testing.T) {
	first := llm_schema.NewUserMessage("a")
	second := llm_schema.NewUserMessage("b")
	EnsureContextMessageIDs([]llm_schema.BaseMessage{first, second})

	if !retainedMessagePrefixPreserved([]llm_schema.BaseMessage{first}, []llm_schema.BaseMessage{first, second}) {
		t.Fatal("未改动前缀应判为保留")
	}
	if retainedMessagePrefixPreserved([]llm_schema.BaseMessage{first, second}, []llm_schema.BaseMessage{first}) {
		t.Fatal("长度缩短应判为改变")
	}
	replaced := llm_schema.NewUserMessage("c")
	EnsureContextMessageIDs([]llm_schema.BaseMessage{replaced})
	if retainedMessagePrefixPreserved([]llm_schema.BaseMessage{first}, []llm_schema.BaseMessage{replaced}) {
		t.Fatal("元素被替换应判为改变")
	}
}
