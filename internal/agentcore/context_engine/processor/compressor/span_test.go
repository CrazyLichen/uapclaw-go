package compressor

import (
	"testing"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestAdjustKeepRecentForToolBoundaries 验证切点被前移到 assistant(tool_calls) 之前。
func TestAdjustKeepRecentForToolBoundaries(t *testing.T) {
	assistant := llm_schema.NewAssistantMessage("")
	assistant.ToolCalls = []*llm_schema.ToolCall{{ID: "call-1", Type: "function"}}
	toolResult := llm_schema.NewToolMessage("call-1", "结果")
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"),
		assistant,
		toolResult,
		llm_schema.NewAssistantMessage("答复"),
	}

	// keep=1 只保留最后一条 → 无跨切点配对，保持 1
	if got := adjustKeepRecentForToolBoundaries(messages, 1); got != 1 {
		t.Fatalf("keep=1 应保持 1，got %d", got)
	}

	// keep=2 会把 tool 结果留着、tool_calls 压掉 → 前移到 assistant 之前，保留 3 条
	if got := adjustKeepRecentForToolBoundaries(messages, 2); got != 3 {
		t.Fatalf("keep=2 应前移为 3，got %d", got)
	}

	// keep 超过消息数时按上限返回
	if got := adjustKeepRecentForToolBoundaries(messages, 99); got != len(messages) {
		t.Fatalf("超界应返回 %d，got %d", len(messages), got)
	}

	// 负数与非正保护
	if got := adjustKeepRecentForToolBoundaries(messages, 0); got != 0 {
		t.Fatalf("keep=0 应返回 0，got %d", got)
	}
	if got := adjustKeepRecentForToolBoundaries(nil, 3); got != 3 {
		t.Fatalf("空消息应原样返回 3，got %d", got)
	}
}

// TestFindLastRealUserMessageIndex 验证跳过内部注入的用户消息。
func TestFindLastRealUserMessageIndex(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("历史问题"),
		llm_schema.NewAssistantMessage("历史答复"),
		llm_schema.NewUserMessage("<memory_block_dialogue>\n摘要"),
		llm_schema.NewUserMessage("   \n<system-reminder>提醒"),
		llm_schema.NewUserMessage("本轮问题"),
		llm_schema.NewAssistantMessage("中途答复"),
	}
	if got := findLastRealUserMessageIndex(messages); got != 4 {
		t.Fatalf("应命中下标 4（本轮问题），got %d", got)
	}
	if got := findLastRealUserMessageIndex([]llm_schema.BaseMessage{llm_schema.NewAssistantMessage("x")}); got != -1 {
		t.Fatalf("无用户消息应返回 -1，got %d", got)
	}
}

// TestBuildDialogueSpan 验证对话跨度三段切分。
func TestBuildDialogueSpan(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("旧问题"),
		llm_schema.NewAssistantMessage("旧答复"),
		llm_schema.NewUserMessage("本轮问题"),
		llm_schema.NewAssistantMessage("本轮答复"),
	}
	span := buildDialogueSpan(messages, 2)
	if len(span.messagesToCompress) != 2 || len(span.protectedTail) != 2 || len(span.preservedPrefix) != 0 {
		t.Fatalf("切分错误: prefix=%d compress=%d tail=%d",
			len(span.preservedPrefix), len(span.messagesToCompress), len(span.protectedTail))
	}
	if !span.hasTarget() {
		t.Fatal("中段非空时 hasTarget 应为 true")
	}
	if empty := buildDialogueSpan(messages, 0); empty.hasTarget() {
		t.Fatal("中段为空时 hasTarget 应为 false")
	}
	// 越界下标应被钳制
	if clamped := buildDialogueSpan(messages, 99); len(clamped.messagesToCompress) != len(messages) {
		t.Fatalf("超界应钳制到全长，got %d", len(clamped.messagesToCompress))
	}
}

// TestBuildCurrentRoundSpan 验证当前轮跨度：前缀含本轮提问，中段为本轮中段。
func TestBuildCurrentRoundSpan(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("旧问题"),
		llm_schema.NewAssistantMessage("旧答复"),
		llm_schema.NewUserMessage("本轮问题"),
		llm_schema.NewAssistantMessage("本轮工具前答复"),
		llm_schema.NewAssistantMessage("本轮答复"),
	}
	span := buildCurrentRoundSpan(messages, 4)
	if len(span.preservedPrefix) != 3 || len(span.messagesToCompress) != 1 || len(span.protectedTail) != 1 {
		t.Fatalf("切分错误: prefix=%d compress=%d tail=%d",
			len(span.preservedPrefix), len(span.messagesToCompress), len(span.protectedTail))
	}

	// 切点在本轮提问之前 → 中段为空，全部进保护尾巴
	noop := buildCurrentRoundSpan(messages, 2)
	if noop.hasTarget() {
		t.Fatal("切点不晚于本轮提问时不应有可压缩中段")
	}

	// 无用户消息 → 中段为空
	if got := buildCurrentRoundSpan([]llm_schema.BaseMessage{llm_schema.NewAssistantMessage("x")}, 0); got.hasTarget() {
		t.Fatal("无用户消息时不应有可压缩中段")
	}
}

// TestIsRealUserMessage 覆盖多模态与内部前缀分支。
func TestIsRealUserMessage(t *testing.T) {
	multi := llm_schema.NewUserMessage("", llm_schema.WithMultiModalContent(llm_schema.ContentPart{Type: "image_url"}))
	if !isRealUserMessage(multi) {
		t.Fatal("多模态用户消息应视为真实输入")
	}
	if isRealUserMessage(llm_schema.NewUserMessage("[STATE_REINJECTION] 状态")) {
		t.Fatal("状态重放消息不应视为真实用户输入")
	}
	if isRealUserMessage(llm_schema.NewAssistantMessage("x")) {
		t.Fatal("assistant 消息不应视为用户消息")
	}
}
