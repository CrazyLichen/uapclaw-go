package processor

import (
	"context"
	"fmt"
	"testing"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 配置测试 ────────────────────────────

func TestNewMicroCompactProcessorConfig(t *testing.T) {
	t.Run("默认值", func(t *testing.T) {
		cfg := NewMicroCompactProcessorConfig()
		if cfg.TriggerThreshold != 5 {
			t.Errorf("TriggerThreshold = %d, want 5", cfg.TriggerThreshold)
		}
		if cfg.KeepRecentPerTool != 15 {
			t.Errorf("KeepRecentPerTool = %d, want 15", cfg.KeepRecentPerTool)
		}
		if cfg.ClearedMarker != "[Old tool result Content cleared]" {
			t.Errorf("ClearedMarker = %q, want default marker", cfg.ClearedMarker)
		}
		expectedTools := []string{"grep", "glob", "read_file", "web_search", "web_fetch"}
		if len(cfg.CompactableToolNames) != len(expectedTools) {
			t.Fatalf("CompactableToolNames len = %d, want %d", len(cfg.CompactableToolNames), len(expectedTools))
		}
		for i, name := range cfg.CompactableToolNames {
			if name != expectedTools[i] {
				t.Errorf("CompactableToolNames[%d] = %q, want %q", i, name, expectedTools[i])
			}
		}
	})

	t.Run("Validate通过", func(t *testing.T) {
		cfg := NewMicroCompactProcessorConfig()
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() error = %v", err)
		}
	})

	t.Run("TriggerThreshold为0", func(t *testing.T) {
		cfg := NewMicroCompactProcessorConfig()
		cfg.TriggerThreshold = 0
		if err := cfg.Validate(); err == nil {
			t.Error("Validate() should return error when TriggerThreshold=0")
		}
	})

	t.Run("KeepRecentPerTool为负数", func(t *testing.T) {
		cfg := NewMicroCompactProcessorConfig()
		cfg.KeepRecentPerTool = -1
		if err := cfg.Validate(); err == nil {
			t.Error("Validate() should return error when KeepRecentPerTool<0")
		}
	})

	t.Run("ClearedMarker为空", func(t *testing.T) {
		cfg := NewMicroCompactProcessorConfig()
		cfg.ClearedMarker = ""
		if err := cfg.Validate(); err == nil {
			t.Error("Validate() should return error when ClearedMarker is empty")
		}
	})

	t.Run("CompactableToolNames为空", func(t *testing.T) {
		cfg := NewMicroCompactProcessorConfig()
		cfg.CompactableToolNames = nil
		if err := cfg.Validate(); err == nil {
			t.Error("Validate() should return error when CompactableToolNames is empty")
		}
	})
}

// ──────────────────────────── 核心方法测试 ────────────────────────────

func TestMicroCompactProcessor_ProcessorType(t *testing.T) {
	cfg := NewMicroCompactProcessorConfig()
	mcp, err := NewMicroCompactProcessor(cfg)
	if err != nil {
		t.Fatalf("NewMicroCompactProcessor() error = %v", err)
	}
	if mcp.ProcessorType() != "MicroCompactProcessor" {
		t.Errorf("ProcessorType() = %q, want %q", mcp.ProcessorType(), "MicroCompactProcessor")
	}
}

func TestMicroCompactProcessor_SaveLoadState(t *testing.T) {
	cfg := NewMicroCompactProcessorConfig()
	mcp, err := NewMicroCompactProcessor(cfg)
	if err != nil {
		t.Fatalf("NewMicroCompactProcessor() error = %v", err)
	}

	state := mcp.SaveState()
	if len(state) != 0 {
		t.Errorf("SaveState() = %v, want empty map", state)
	}

	mcp.LoadState(map[string]any{"key": "value"})
	// LoadState 是空操作，无返回值可验证
}

// ──────────────────────────── 辅助方法测试 ────────────────────────────

func TestMicroCompactProcessor_collectCompactableIndicesByTool(t *testing.T) {
	cfg := NewMicroCompactProcessorConfig()
	cfg.CompactableToolNames = []string{"grep", "glob"}
	mcp, _ := NewMicroCompactProcessor(cfg)

	t.Run("按工具名分组", func(t *testing.T) {
		assistantMsg := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{ID: "call_1", Name: "grep"},
				{ID: "call_2", Name: "glob"},
				{ID: "call_3", Name: "read_file"},
			},
		}
		toolMsg1 := llm_schema.NewToolMessage("call_1", "grep result")
		toolMsg2 := llm_schema.NewToolMessage("call_2", "glob result")
		toolMsg3 := llm_schema.NewToolMessage("call_3", "read_file result")

		messages := []llm_schema.BaseMessage{assistantMsg, toolMsg1, toolMsg2, toolMsg3}
		result := mcp.collectCompactableIndicesByTool(messages)

		if len(result["grep"]) != 1 || result["grep"][0] != 1 {
			t.Errorf("grep indices = %v, want [1]", result["grep"])
		}
		if len(result["glob"]) != 1 || result["glob"][0] != 2 {
			t.Errorf("glob indices = %v, want [2]", result["glob"])
		}
		// read_file 不在 CompactableToolNames 中
		if _, exists := result["read_file"]; exists {
			t.Error("read_file should not be in result")
		}
	})

	t.Run("已清除的跳过", func(t *testing.T) {
		assistantMsg := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{ID: "call_1", Name: "grep"},
			},
		}
		toolMsg1 := llm_schema.NewToolMessage("call_1", cfg.ClearedMarker) // 已清除

		messages := []llm_schema.BaseMessage{assistantMsg, toolMsg1}
		result := mcp.collectCompactableIndicesByTool(messages)

		if _, exists := result["grep"]; exists {
			t.Error("已清除的 ToolMessage 不应被收集")
		}
	})
}

func TestMicroCompactProcessor_hasAnyToolExceedThreshold(t *testing.T) {
	cfg := NewMicroCompactProcessorConfig()
	cfg.TriggerThreshold = 2
	cfg.KeepRecentPerTool = 1
	cfg.CompactableToolNames = []string{"grep"}
	mcp, _ := NewMicroCompactProcessor(cfg)

	t.Run("未超限返回false", func(t *testing.T) {
		// threshold=2, keep=1, 总共需要 >3 条才超限
		messages := buildToolMessages("grep", 3)
		if mcp.hasAnyToolExceedThreshold(messages) {
			t.Error("3 条不超限（需要 >3），应返回 false")
		}
	})

	t.Run("恰好等于阈值返回false", func(t *testing.T) {
		messages := buildToolMessages("grep", 3)
		if mcp.hasAnyToolExceedThreshold(messages) {
			t.Error("3 条恰好等于阈值，应返回 false")
		}
	})

	t.Run("超过阈值返回true", func(t *testing.T) {
		messages := buildToolMessages("grep", 4)
		if !mcp.hasAnyToolExceedThreshold(messages) {
			t.Error("4 条超过阈值 3，应返回 true")
		}
	})
}

func TestMicroCompactProcessor_collectFlatIndicesForCompact(t *testing.T) {
	cfg := NewMicroCompactProcessorConfig()
	cfg.TriggerThreshold = 2
	cfg.KeepRecentPerTool = 1
	cfg.CompactableToolNames = []string{"grep"}
	mcp, _ := NewMicroCompactProcessor(cfg)

	t.Run("超限保留尾部", func(t *testing.T) {
		// 4 条 grep，threshold=2+1=3，keep=1，清除前 3 条
		messages := buildToolMessages("grep", 4)
		indices := mcp.collectFlatIndicesForCompact(messages, false)
		if len(indices) != 3 {
			t.Errorf("collectFlatIndicesForCompact() = %d indices, want 3", len(indices))
		}
	})

	t.Run("force=true阈值降低", func(t *testing.T) {
		// 2 条 grep，force 时 threshold=1，清除前 1 条
		messages := buildToolMessages("grep", 2)
		indices := mcp.collectFlatIndicesForCompact(messages, true)
		if len(indices) != 1 {
			t.Errorf("collectFlatIndicesForCompact(force=true) = %d indices, want 1", len(indices))
		}
	})

	t.Run("KeepRecentPerTool=0全部清除", func(t *testing.T) {
		cfg0 := NewMicroCompactProcessorConfig()
		cfg0.TriggerThreshold = 1
		cfg0.KeepRecentPerTool = 0
		cfg0.CompactableToolNames = []string{"grep"}
		mcp0, _ := NewMicroCompactProcessor(cfg0)

		messages := buildToolMessages("grep", 3)
		indices := mcp0.collectFlatIndicesForCompact(messages, false)
		if len(indices) != 3 {
			t.Errorf("KeepRecentPerTool=0 时应全部清除，got %d indices", len(indices))
		}
	})
}

// ──────────────────────────── Trigger/OnAddMessages 测试 ────────────────────────────

func TestMicroCompactProcessor_TriggerAddMessages(t *testing.T) {
	cfg := NewMicroCompactProcessorConfig()
	cfg.TriggerThreshold = 2
	cfg.KeepRecentPerTool = 1
	cfg.CompactableToolNames = []string{"grep"}
	mcp, _ := NewMicroCompactProcessor(cfg)

	t.Run("不构成APIround返回false", func(t *testing.T) {
		mc := &fakeModelContextForMicro{messages: []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("hello"),
		}}
		messagesToAdd := []llm_schema.BaseMessage{
			llm_schema.NewToolMessage("call_1", "result"),
		}
		got, err := mcp.TriggerAddMessages(context.Background(), mc, messagesToAdd)
		if err != nil {
			t.Fatalf("TriggerAddMessages() error = %v", err)
		}
		if got {
			t.Error("TriggerAddMessages() = true, want false（不构成 API round）")
		}
	})

	t.Run("构成APIround但无超限工具返回false", func(t *testing.T) {
		// 构建完整 API round：user → assistant(tool_call) → tool
		assistantMsg := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls:      []*llm_schema.ToolCall{{ID: "call_1", Name: "grep"}},
		}
		mc := &fakeModelContextForMicro{messages: []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("hello"),
			assistantMsg,
		}}
		messagesToAdd := []llm_schema.BaseMessage{
			llm_schema.NewToolMessage("call_1", "result"),
		}
		got, err := mcp.TriggerAddMessages(context.Background(), mc, messagesToAdd)
		if err != nil {
			t.Fatalf("TriggerAddMessages() error = %v", err)
		}
		if got {
			t.Error("TriggerAddMessages() = true, want false（工具数量未超限）")
		}
	})

	t.Run("构成APIround且超限工具返回true", func(t *testing.T) {
		// threshold=2, keep=1, 需要 >3 条 grep
		assistantMsg := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{ID: "call_1", Name: "grep"},
				{ID: "call_2", Name: "grep"},
				{ID: "call_3", Name: "grep"},
				{ID: "call_4", Name: "grep"},
			},
		}
		mcMessages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("hello"),
			assistantMsg,
			llm_schema.NewToolMessage("call_1", "result1"),
			llm_schema.NewToolMessage("call_2", "result2"),
			llm_schema.NewToolMessage("call_3", "result3"),
		}
		// 添加第 4 条 tool message + final assistant 构成 API round
		finalAssistant := llm_schema.NewAssistantMessage("done")
		mc := &fakeModelContextForMicro{messages: mcMessages}
		messagesToAdd := []llm_schema.BaseMessage{
			llm_schema.NewToolMessage("call_4", "result4"),
			finalAssistant,
		}
		got, err := mcp.TriggerAddMessages(context.Background(), mc, messagesToAdd)
		if err != nil {
			t.Fatalf("TriggerAddMessages() error = %v", err)
		}
		if !got {
			t.Error("TriggerAddMessages() = false, want true（工具数量超限且构成 API round）")
		}
	})
}

func TestMicroCompactProcessor_OnAddMessages(t *testing.T) {
	cfg := NewMicroCompactProcessorConfig()
	cfg.TriggerThreshold = 2
	cfg.KeepRecentPerTool = 1
	cfg.CompactableToolNames = []string{"grep"}
	marker := cfg.ClearedMarker

	t.Run("无需清除时透传", func(t *testing.T) {
		mcp, _ := NewMicroCompactProcessor(cfg)
		mc := &fakeModelContextForMicro{messages: []llm_schema.BaseMessage{}}
		messagesToAdd := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
		event, result, err := mcp.OnAddMessages(context.Background(), mc, messagesToAdd)
		if err != nil {
			t.Fatalf("OnAddMessages() error = %v", err)
		}
		if event != nil {
			t.Error("OnAddMessages() event should be nil when nothing to clear")
		}
		if len(result) != len(messagesToAdd) {
			t.Errorf("OnAddMessages() result len = %d, want %d", len(result), len(messagesToAdd))
		}
	})

	t.Run("有需清除的ToolMessage替换content", func(t *testing.T) {
		mcp, _ := NewMicroCompactProcessor(cfg)
		// 4 条 grep，threshold=2+1=3，keep=1，清除前 3 条
		assistantMsg := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{ID: "call_1", Name: "grep"},
				{ID: "call_2", Name: "grep"},
				{ID: "call_3", Name: "grep"},
				{ID: "call_4", Name: "grep"},
			},
		}
		mcMessages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("hello"),
			assistantMsg,
			llm_schema.NewToolMessage("call_1", "result1"),
			llm_schema.NewToolMessage("call_2", "result2"),
			llm_schema.NewToolMessage("call_3", "result3"),
		}
		mc := &fakeModelContextForMicro{messages: mcMessages}
		messagesToAdd := []llm_schema.BaseMessage{
			llm_schema.NewToolMessage("call_4", "result4"),
		}

		event, _, err := mcp.OnAddMessages(context.Background(), mc, messagesToAdd)
		if err != nil {
			t.Fatalf("OnAddMessages() error = %v", err)
		}
		if event == nil {
			t.Fatal("OnAddMessages() event should not be nil when messages are cleared")
		}
		if event.EventType != "MicroCompactProcessor" {
			t.Errorf("EventType = %q, want %q", event.EventType, "MicroCompactProcessor")
		}
		if len(event.MessagesToModify) == 0 {
			t.Error("MessagesToModify should not be empty")
		}

		// 验证消息内容被替换
		updatedMessages := mc.GetMessages(nil, true)
		clearedCount := 0
		keptCount := 0
		for _, msg := range updatedMessages {
			tm, ok := msg.(*llm_schema.ToolMessage)
			if !ok {
				continue
			}
			content := tm.GetContent().Text()
			if content == marker {
				clearedCount++
			} else if content != "" && content != marker {
				keptCount++
			}
		}
		if clearedCount != 3 {
			t.Errorf("cleared ToolMessage count = %d, want 3", clearedCount)
		}
		if keptCount != 1 {
			t.Errorf("kept ToolMessage count = %d, want 1", keptCount)
		}
	})

	t.Run("已是marker的不重复替换", func(t *testing.T) {
		mcp, _ := NewMicroCompactProcessor(cfg)
		// 需要 5 条 grep（1 条已是 marker + 4 条非 marker）
		// collectCompactableIndicesByTool 只收集非 marker 的：4 条
		// threshold=2+1=3, 4 > 3, 触发。keep=1, 清除前 3 条
		assistantMsg := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{ID: "call_0", Name: "grep"},
				{ID: "call_1", Name: "grep"},
				{ID: "call_2", Name: "grep"},
				{ID: "call_3", Name: "grep"},
				{ID: "call_4", Name: "grep"},
			},
		}
		// call_0 已被清除（已是 marker，不会被 collectCompactableIndicesByTool 收集）
		toolMsg0 := llm_schema.NewToolMessage("call_0", marker)
		mcMessages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("hello"),
			assistantMsg,
			toolMsg0,
			llm_schema.NewToolMessage("call_1", "result1"),
			llm_schema.NewToolMessage("call_2", "result2"),
			llm_schema.NewToolMessage("call_3", "result3"),
		}
		mc := &fakeModelContextForMicro{messages: mcMessages}
		messagesToAdd := []llm_schema.BaseMessage{
			llm_schema.NewToolMessage("call_4", "result4"),
		}

		event, _, err := mcp.OnAddMessages(context.Background(), mc, messagesToAdd)
		if err != nil {
			t.Fatalf("OnAddMessages() error = %v", err)
		}
		if event == nil {
			t.Fatal("OnAddMessages() event should not be nil")
		}
		// 非marker的4条grep：call_1(3), call_2(4), call_3(5), call_4(6)
		// 保留1条，清除3条（call_1, call_2, call_3）
		if len(event.MessagesToModify) != 3 {
			t.Errorf("MessagesToModify len = %d, want 3 (call_1, call_2, call_3)", len(event.MessagesToModify))
		}
		// call_0 不应在 modifiedIndices 中（它已经是 marker 了）
		for _, idx := range event.MessagesToModify {
			msg := mc.GetMessages(nil, true)[idx]
			tm, ok := msg.(*llm_schema.ToolMessage)
			if !ok {
				continue
			}
			if tm.ToolCallID == "call_0" {
				t.Error("call_0 不应在 modifiedIndices 中，它已经是 marker")
			}
		}
	})

	t.Run("非ToolMessage在索引中时跳过", func(t *testing.T) {
		// collectFlatIndicesForCompact 只收集 ToolMessage 索引，
		// OnAddMessages 内部也会通过类型断言跳过非 ToolMessage。
		// 构造一个消息列表，其中 UserMessage 不应被修改。
		mcp, _ := NewMicroCompactProcessor(cfg)
		assistantMsg := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{ID: "call_1", Name: "grep"},
				{ID: "call_2", Name: "grep"},
				{ID: "call_3", Name: "grep"},
				{ID: "call_4", Name: "grep"},
			},
		}
		mcMessages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("hello"),
			assistantMsg,
			llm_schema.NewToolMessage("call_1", "result1"),
			llm_schema.NewToolMessage("call_2", "result2"),
			llm_schema.NewToolMessage("call_3", "result3"),
		}
		mc := &fakeModelContextForMicro{messages: mcMessages}
		messagesToAdd := []llm_schema.BaseMessage{
			llm_schema.NewToolMessage("call_4", "result4"),
		}

		event, _, err := mcp.OnAddMessages(context.Background(), mc, messagesToAdd)
		if err != nil {
			t.Fatalf("OnAddMessages() error = %v", err)
		}
		if event == nil {
			t.Fatal("OnAddMessages() event should not be nil")
		}
		// 验证 modifiedIndices 中不包含 UserMessage 的索引
		for _, idx := range event.MessagesToModify {
			msg := mc.GetMessages(nil, true)[idx]
			if _, ok := msg.(*llm_schema.UserMessage); ok {
				t.Errorf("UserMessage 不应在 modifiedIndices 中，索引 %d", idx)
			}
		}
	})
}

// ──────────────────────────── 测试辅助 ────────────────────────────

// buildToolMessages 构建包含指定数量 tool ToolMessage 的消息列表
func buildToolMessages(toolName string, count int) []llm_schema.BaseMessage {
	var toolCalls []*llm_schema.ToolCall
	var messages []llm_schema.BaseMessage

	for i := 0; i < count; i++ {
		callID := fmt.Sprintf("call_%d", i)
		toolCalls = append(toolCalls, &llm_schema.ToolCall{ID: callID, Name: toolName})
		messages = append(messages, llm_schema.NewToolMessage(callID, fmt.Sprintf("%s result %d", toolName, i)))
	}

	assistantMsg := &llm_schema.AssistantMessage{
		DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
		ToolCalls:      toolCalls,
	}

	result := []llm_schema.BaseMessage{assistantMsg}
	result = append(result, messages...)
	return result
}

// fakeModelContextForMicro 测试用 ModelContext 模拟（MicroCompactProcessor 测试专用）
type fakeModelContextForMicro struct {
	messages     []llm_schema.BaseMessage
	tokenCounter token.TokenCounter
}

func (f *fakeModelContextForMicro) Len() int { return len(f.messages) }
func (f *fakeModelContextForMicro) GetMessages(_ *int, _ bool) []llm_schema.BaseMessage {
	return f.messages
}
func (f *fakeModelContextForMicro) SetMessages(messages []llm_schema.BaseMessage, _ bool) {
	f.messages = messages
}
func (f *fakeModelContextForMicro) PopMessages(_ int, _ bool) []llm_schema.BaseMessage { return nil }
func (f *fakeModelContextForMicro) ClearMessages(_ context.Context, _ bool) error      { return nil }
func (f *fakeModelContextForMicro) AddMessages(_ context.Context, _ any) ([]llm_schema.BaseMessage, error) {
	return nil, nil
}
func (f *fakeModelContextForMicro) GetContextWindow(_ context.Context, _ []llm_schema.BaseMessage,
	_ []*schema.ToolInfo, _ *int, _ *int) (*iface.ContextWindow, error) {
	return nil, nil
}
func (f *fakeModelContextForMicro) Statistic() *iface.ContextStats      { return nil }
func (f *fakeModelContextForMicro) SessionID() string                   { return "test-session" }
func (f *fakeModelContextForMicro) ContextID() string                   { return "test-context" }
func (f *fakeModelContextForMicro) TokenCounter() token.TokenCounter    { return f.tokenCounter }
func (f *fakeModelContextForMicro) ReloaderTool() tool.Tool             { return nil }
