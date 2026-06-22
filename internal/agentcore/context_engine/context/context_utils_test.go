package context

import (
	"context"
	"strings"
	"testing"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockProcessor 模拟处理器，用于测试 IsCompressionProcessor
type mockProcessor struct {
	// processorType 处理器类型标识
	processorType string
}

// OnAddMessages 测试用空实现
func (m *mockProcessor) OnAddMessages(_ context.Context, _ iface.ModelContext, _ []llm_schema.BaseMessage, _ ...iface.Option) (*iface.ContextEvent, []llm_schema.BaseMessage, error) {
	return nil, nil, nil
}

// OnGetContextWindow 测试用空实现
func (m *mockProcessor) OnGetContextWindow(_ context.Context, _ iface.ModelContext, _ iface.ContextWindow, _ ...iface.Option) (*iface.ContextEvent, iface.ContextWindow, error) {
	return nil, iface.ContextWindow{}, nil
}

// TriggerAddMessages 测试用空实现
func (m *mockProcessor) TriggerAddMessages(_ context.Context, _ iface.ModelContext, _ []llm_schema.BaseMessage, _ ...iface.Option) (bool, error) {
	return false, nil
}

// TriggerGetContextWindow 测试用空实现
func (m *mockProcessor) TriggerGetContextWindow(_ context.Context, _ iface.ModelContext, _ iface.ContextWindow, _ ...iface.Option) (bool, error) {
	return false, nil
}

// SaveState 导出处理器内部状态（测试用空实现）
func (m *mockProcessor) SaveState() map[string]any { return nil }

// LoadState 从 map 恢复处理器内部状态（测试用空实现）
func (m *mockProcessor) LoadState(_ map[string]any) {}

// ProcessorType 返回处理器类型标识
func (m *mockProcessor) ProcessorType() string { return m.processorType }

// ──────────────────────────── 导出函数 ────────────────────────────

// TestValidateMessages_有效消息 验证有效消息列表不返回错误
func TestValidateMessages_有效消息(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
	}
	if err := ValidateMessages(messages); err != nil {
		t.Errorf("有效消息列表不应返回错误，实际: %v", err)
	}
}

// TestValidateMessages_空列表 验证空消息列表不返回错误
func TestValidateMessages_空列表(t *testing.T) {
	if err := ValidateMessages(nil); err != nil {
		t.Errorf("空消息列表不应返回错误，实际: %v", err)
	}
}

// TestValidateMessages_含nil消息 验证含 nil 元素的消息列表返回错误
func TestValidateMessages_含nil消息(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		nil,
		llm_schema.NewAssistantMessage("你好！"),
	}
	err := ValidateMessages(messages)
	if err == nil {
		t.Fatal("含 nil 消息的列表应返回错误")
	}
	if !strings.Contains(err.Error(), "索引 1") {
		t.Errorf("错误信息应包含 '索引 1'，实际: %v", err)
	}
}

// TestEnsureContextMessageIDs_已有ID 验证已有 ID 的消息不会被修改
func TestEnsureContextMessageIDs_已有ID(t *testing.T) {
	originalID := "existing-id-123"
	msg := llm_schema.NewUserMessage("你好",
		llm_schema.WithMetadata(map[string]any{ContextMessageIDKey: originalID}),
	)
	messages := []llm_schema.BaseMessage{msg}

	result := EnsureContextMessageIDs(messages)
	metadata := result[0].GetMetadata()
	if metadata[ContextMessageIDKey] != originalID {
		t.Errorf("已有 ID 不应被覆盖，期望 %s，实际 %v", originalID, metadata[ContextMessageIDKey])
	}
}

// TestEnsureContextMessageIDs_缺失ID 验证缺失 ID 时自动生成 UUID
func TestEnsureContextMessageIDs_缺失ID(t *testing.T) {
	msg := llm_schema.NewUserMessage("你好",
		llm_schema.WithMetadata(map[string]any{}),
	)
	messages := []llm_schema.BaseMessage{msg}

	result := EnsureContextMessageIDs(messages)
	metadata := result[0].GetMetadata()
	id, ok := metadata[ContextMessageIDKey].(string)
	if !ok || id == "" {
		t.Errorf("缺失 ID 时应自动生成 UUID，实际: %v", metadata[ContextMessageIDKey])
	}
}

// TestEnsureContextMessageIDs_缺失元数据 验证 metadata 为 nil 时自动创建并生成 ID
func TestEnsureContextMessageIDs_缺失元数据(t *testing.T) {
	msg := llm_schema.NewUserMessage("你好")
	// 确认初始 metadata 为 nil
	if msg.GetMetadata() != nil {
		t.Fatalf("初始 metadata 应为 nil，实际: %v", msg.GetMetadata())
	}

	messages := []llm_schema.BaseMessage{msg}
	result := EnsureContextMessageIDs(messages)
	metadata := result[0].GetMetadata()
	if metadata == nil {
		t.Fatal("缺失元数据时应自动创建 metadata")
	}
	id, ok := metadata[ContextMessageIDKey].(string)
	if !ok || id == "" {
		t.Errorf("缺失元数据时应生成 context_message_id，实际: %v", metadata[ContextMessageIDKey])
	}
}

// TestValidateAndFixContextWindow_空消息 验证空消息列表不做修改
func TestValidateAndFixContextWindow_空消息(t *testing.T) {
	window := iface.NewContextWindow()
	ValidateAndFixContextWindow(window)
	if len(window.ContextMessages) != 0 {
		t.Errorf("空消息列表应保持不变，实际长度 %d", len(window.ContextMessages))
	}
}

// TestValidateAndFixContextWindow_非Tool开头 验证非 Tool 消息开头不做修改
func TestValidateAndFixContextWindow_非Tool开头(t *testing.T) {
	window := &iface.ContextWindow{
		ContextMessages: []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("你好"),
			llm_schema.NewAssistantMessage("你好！"),
		},
	}
	ValidateAndFixContextWindow(window)
	if len(window.ContextMessages) != 2 {
		t.Errorf("非 Tool 开头应保持不变，实际长度 %d", len(window.ContextMessages))
	}
}

// TestValidateAndFixContextWindow_Tool开头 验证开头连续 Tool 消息被截掉
func TestValidateAndFixContextWindow_Tool开头(t *testing.T) {
	window := &iface.ContextWindow{
		ContextMessages: []llm_schema.BaseMessage{
			llm_schema.NewToolMessage("call_1", "结果1"),
			llm_schema.NewToolMessage("call_2", "结果2"),
			llm_schema.NewUserMessage("你好"),
			llm_schema.NewAssistantMessage("你好！"),
		},
	}
	ValidateAndFixContextWindow(window)
	if len(window.ContextMessages) != 2 {
		t.Fatalf("开头 Tool 消息应被截掉，实际长度 %d", len(window.ContextMessages))
	}
	if window.ContextMessages[0].GetRole() != llm_schema.RoleTypeUser {
		t.Errorf("截掉后首条消息应为 user，实际 %v", window.ContextMessages[0].GetRole())
	}
}

// TestValidateAndFixContextWindow_全部Tool 验证全部为 Tool 消息时清空
func TestValidateAndFixContextWindow_全部Tool(t *testing.T) {
	window := &iface.ContextWindow{
		ContextMessages: []llm_schema.BaseMessage{
			llm_schema.NewToolMessage("call_1", "结果1"),
			llm_schema.NewToolMessage("call_2", "结果2"),
		},
	}
	ValidateAndFixContextWindow(window)
	if len(window.ContextMessages) != 0 {
		t.Errorf("全部 Tool 消息应清空，实际长度 %d", len(window.ContextMessages))
	}
}

// TestResolveContextMax_fallback优先级 验证 fallback 参数优先级最高
func TestResolveContextMax_fallback优先级(t *testing.T) {
	result := ResolveContextMax("glm-4", 50000, nil)
	if result != 50000 {
		t.Errorf("fallback > 0 时应直接返回 fallback，期望 50000，实际 %d", result)
	}
}

// TestResolveContextMax_自定义映射 验证自定义映射优先于内置映射
func TestResolveContextMax_自定义映射(t *testing.T) {
	customMap := map[string]int{"glm-4": 99999}
	result := ResolveContextMax("glm-4", 0, customMap)
	if result != 99999 {
		t.Errorf("自定义映射应优先于内置映射，期望 99999，实际 %d", result)
	}
}

// TestResolveContextMax_内置映射 验证内置映射生效
func TestResolveContextMax_内置映射(t *testing.T) {
	result := ResolveContextMax("glm-4", 0, nil)
	if result != 128000 {
		t.Errorf("内置映射应返回 glm-4 的默认值 128000，实际 %d", result)
	}
}

// TestResolveContextMax_默认值 验证无匹配时返回默认值
func TestResolveContextMax_默认值(t *testing.T) {
	result := ResolveContextMax("unknown-model", 0, nil)
	if result != DefaultContextMaxTokens {
		t.Errorf("无匹配时应返回默认值 %d，实际 %d", DefaultContextMaxTokens, result)
	}
}

// TestResolveContextMax_空模型名 验证空模型名返回默认值
func TestResolveContextMax_空模型名(t *testing.T) {
	result := ResolveContextMax("", 0, nil)
	if result != DefaultContextMaxTokens {
		t.Errorf("空模型名应返回默认值 %d，实际 %d", DefaultContextMaxTokens, result)
	}
}

// TestIsCompressionProcessor_compressor类型 验证 compressor 类型返回 true
func TestIsCompressionProcessor_compressor类型(t *testing.T) {
	p := &mockProcessor{processorType: "DialogueCompressor"}
	if !IsCompressionProcessor(p) {
		t.Error("DialogueCompressor 应为压缩类型")
	}
}

// TestIsCompressionProcessor_compact类型 验证 compact 类型返回 true
func TestIsCompressionProcessor_compact类型(t *testing.T) {
	p := &mockProcessor{processorType: "ContextCompactor"}
	if !IsCompressionProcessor(p) {
		t.Error("ContextCompactor 包含 compact 应为压缩类型")
	}
}

// TestIsCompressionProcessor_非压缩类型 验证非压缩类型返回 false
func TestIsCompressionProcessor_非压缩类型(t *testing.T) {
	p := &mockProcessor{processorType: "MessageOffloader"}
	if IsCompressionProcessor(p) {
		t.Error("MessageOffloader 不应为压缩类型")
	}
}

// TestFormatReloadedMessages_有消息 验证格式化输出包含消息内容
func TestFormatReloadedMessages_有消息(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
	}
	result := FormatReloadedMessages("handle-123", messages)
	if !strings.Contains(result, "handle=handle-123") {
		t.Errorf("输出应包含 handle，实际: %s", result)
	}
	if !strings.Contains(result, "消息 1:") {
		t.Errorf("输出应包含 '消息 1:'，实际: %s", result)
	}
	if !strings.Contains(result, "消息 2:") {
		t.Errorf("输出应包含 '消息 2:'，实际: %s", result)
	}
	if !strings.Contains(result, "user") {
		t.Errorf("输出应包含 'user'，实际: %s", result)
	}
}

// TestFormatReloadedMessages_空消息 验证空消息列表格式化输出
func TestFormatReloadedMessages_空消息(t *testing.T) {
	result := FormatReloadedMessages("handle-456", nil)
	if !strings.Contains(result, "handle=handle-456") {
		t.Errorf("输出应包含 handle，实际: %s", result)
	}
	if strings.Contains(result, "消息 1:") {
		t.Errorf("空消息列表不应包含 '消息 1:'，实际: %s", result)
	}
}

// TestFindLastNDialogueRound_有对话轮次 验证查找倒数第 n 轮对话
func TestFindLastNDialogueRound_有对话轮次(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("第一轮"),
		llm_schema.NewAssistantMessage("第一轮回答"),
		llm_schema.NewUserMessage("第二轮"),
		llm_schema.NewAssistantMessage("第二轮回答"),
	}
	// 查找倒数第 1 轮（最新轮次），userIdx 应为 2
	idx := FindLastNDialogueRound(messages, 1)
	if idx != 2 {
		t.Errorf("倒数第 1 轮 userIdx 应为 2，实际 %d", idx)
	}
	// 查找倒数第 2 轮（最老轮次），userIdx 应为 0
	idx = FindLastNDialogueRound(messages, 2)
	if idx != 0 {
		t.Errorf("倒数第 2 轮 userIdx 应为 0，实际 %d", idx)
	}
}

// TestFindLastNDialogueRound_空消息 验证空消息返回 -1
func TestFindLastNDialogueRound_空消息(t *testing.T) {
	idx := FindLastNDialogueRound(nil, 1)
	if idx != -1 {
		t.Errorf("空消息应返回 -1，实际 %d", idx)
	}
}

// TestFindLastAIAbsentToolCall_无ToolCall的助手消息 验证查找不含 tool_calls 的助手消息
func TestFindLastAIAbsentToolCall_无ToolCall的助手消息(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
		llm_schema.NewUserMessage("查询天气"),
		llm_schema.NewAssistantMessage("查询中",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				llm_schema.NewToolCall("call_1", "get_weather", `{"city":"北京"}`),
			}),
		),
		llm_schema.NewToolMessage("call_1", "晴天"),
		llm_schema.NewAssistantMessage("北京晴天"),
	}
	idx := FindLastAIAbsentToolCall(messages)
	if idx != 5 {
		t.Errorf("最后一条不含 tool_calls 的助手消息索引应为 5，实际 %d", idx)
	}
}

// TestFindLastAIAbsentToolCall_仅含ToolCall 验证全部助手消息含 tool_calls 时返回 -1
func TestFindLastAIAbsentToolCall_仅含ToolCall(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("查询"),
		llm_schema.NewAssistantMessage("查询中",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				llm_schema.NewToolCall("call_1", "search", `{}`),
			}),
		),
	}
	idx := FindLastAIAbsentToolCall(messages)
	if idx != -1 {
		t.Errorf("全部助手消息含 tool_calls 应返回 -1，实际 %d", idx)
	}
}

// TestFindLastAIAbsentToolCall_空消息 验证空消息返回 -1
func TestFindLastAIAbsentToolCall_空消息(t *testing.T) {
	idx := FindLastAIAbsentToolCall(nil)
	if idx != -1 {
		t.Errorf("空消息应返回 -1，实际 %d", idx)
	}
}

// TestFindMessageIndexByContextMessageID_找到 验证根据 ID 查找消息索引
func TestFindMessageIndexByContextMessageID_找到(t *testing.T) {
	targetID := "msg-id-456"
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("第一条",
			llm_schema.WithMetadata(map[string]any{ContextMessageIDKey: "msg-id-123"}),
		),
		func() llm_schema.BaseMessage {
			msg := llm_schema.NewAssistantMessage("第二条")
			msg.SetMetadata(map[string]any{ContextMessageIDKey: targetID})
			return msg
		}(),
	}
	idx := FindMessageIndexByContextMessageID(messages, targetID)
	if idx != 1 {
		t.Errorf("应找到索引 1，实际 %d", idx)
	}
}

// TestFindMessageIndexByContextMessageID_未找到 验证 ID 不存在时返回 -1
func TestFindMessageIndexByContextMessageID_未找到(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好",
			llm_schema.WithMetadata(map[string]any{ContextMessageIDKey: "msg-id-123"}),
		),
	}
	idx := FindMessageIndexByContextMessageID(messages, "non-existent-id")
	if idx != -1 {
		t.Errorf("不存在的 ID 应返回 -1，实际 %d", idx)
	}
}

// TestFindMessageIndexByContextMessageID_nil元数据 验证 metadata 为 nil 时跳过
func TestFindMessageIndexByContextMessageID_nil元数据(t *testing.T) {
	msg := llm_schema.NewUserMessage("你好")
	// metadata 默认为 nil
	messages := []llm_schema.BaseMessage{msg}
	idx := FindMessageIndexByContextMessageID(messages, "any-id")
	if idx != -1 {
		t.Errorf("nil 元数据的消息应跳过，返回 -1，实际 %d", idx)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
