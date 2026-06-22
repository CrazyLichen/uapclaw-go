package offloader

import (
	"context"
	"strings"
	"testing"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestToolResultBudgetProcessorConfig_默认值(t *testing.T) {
	cfg := &ToolResultBudgetProcessorConfig{}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("零值 Config 期望通过验证（自动填充默认值），实际错误: %v", err)
	}
	if cfg.TokensThreshold != 50000 {
		t.Errorf("TokensThreshold 期望 50000, 实际 %d", cfg.TokensThreshold)
	}
	if cfg.LargeMessageThreshold != 10000 {
		t.Errorf("LargeMessageThreshold 期望 10000, 实际 %d", cfg.LargeMessageThreshold)
	}
	if cfg.TrimSize != 3000 {
		t.Errorf("TrimSize 期望 3000, 实际 %d", cfg.TrimSize)
	}
	if cfg.OffloadFilePrefix != "ToolResultBudgetProcessor" {
		t.Errorf("OffloadFilePrefix 期望 ToolResultBudgetProcessor, 实际 %s", cfg.OffloadFilePrefix)
	}
	if len(cfg.OffloadMessageTypes) == 0 || cfg.OffloadMessageTypes[0] != "tool" {
		t.Errorf("OffloadMessageTypes 期望 [tool], 实际 %v", cfg.OffloadMessageTypes)
	}
}

func TestToolResultBudgetProcessorConfig_自定义值(t *testing.T) {
	cfg := &ToolResultBudgetProcessorConfig{
		TokensThreshold:       100000,
		LargeMessageThreshold: 5000,
		TrimSize:              500,
		ToolNameAllowlist:     []string{"grep", "read_file"},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("期望验证通过，实际错误: %v", err)
	}
	if cfg.TokensThreshold != 100000 {
		t.Errorf("TokensThreshold 期望 100000, 实际 %d", cfg.TokensThreshold)
	}
}

func TestToolResultBudgetProcessorConfig_Validate_负值报错(t *testing.T) {
	cfg := &ToolResultBudgetProcessorConfig{
		TokensThreshold: -1,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("TokensThreshold=-1 期望报错，实际通过")
	}
}

func TestNewToolResultBudgetProcessor_正常创建(t *testing.T) {
	cfg := &ToolResultBudgetProcessorConfig{}
	trbp, err := NewToolResultBudgetProcessor(cfg)
	if err != nil {
		t.Fatalf("期望创建成功，实际错误: %v", err)
	}
	if trbp.ProcessorType() != "ToolResultBudgetProcessor" {
		t.Errorf("ProcessorType 期望 ToolResultBudgetProcessor, 实际 %s", trbp.ProcessorType())
	}
}

func TestToolResultBudgetProcessor_SaveLoadState(t *testing.T) {
	cfg := &ToolResultBudgetProcessorConfig{}
	trbp, _ := NewToolResultBudgetProcessor(cfg)
	state := trbp.SaveState()
	if len(state) != 0 {
		t.Errorf("SaveState 期望空 map, 实际 %v", state)
	}
	trbp.LoadState(map[string]any{"key": "value"}) // 不应 panic
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestTRBP 创建测试用 ToolResultBudgetProcessor 实例
func newTestTRBP() *ToolResultBudgetProcessor {
	cfg := &ToolResultBudgetProcessorConfig{
		TokensThreshold:       50000,
		LargeMessageThreshold: 100,
		TrimSize:              20,
	}
	_ = cfg.Validate()
	bp := processor.NewBaseProcessor(cfg)
	return &ToolResultBudgetProcessor{BaseProcessor: bp, config: cfg}
}

func TestTRBP_shouldOffloadMessage_角色不匹配(t *testing.T) {
	p := newTestTRBP()
	msg := llm_schema.NewUserMessage("hello")
	if p.shouldOffloadMessage(msg, nil, nil) {
		t.Fatal("UserMessage 不在 OffloadMessageTypes 中，期望返回 false")
	}
}

func TestTRBP_shouldOffloadMessage_已卸载消息(t *testing.T) {
	p := newTestTRBP()
	msg := schema.NewOffloadToolMessage("tc-1", strings.Repeat("x", 500), "handle", "filesystem")
	if p.shouldOffloadMessage(msg, nil, nil) {
		t.Fatal("已卸载消息不应重复卸载")
	}
}

func TestTRBP_shouldOffloadMessage_非纯文本内容(t *testing.T) {
	p := newTestTRBP()
	// 空内容 → IsText()=true 但 Text()=""
	msg := llm_schema.NewToolMessage("tc-1", "")
	if p.shouldOffloadMessage(msg, nil, nil) {
		t.Fatal("空文本内容的 ToolMessage 不应卸载")
	}
}

func TestTRBP_shouldOffloadMessage_白名单工具(t *testing.T) {
	cfg := &ToolResultBudgetProcessorConfig{
		TokensThreshold:       50000,
		LargeMessageThreshold: 100,
		TrimSize:              20,
		ToolNameAllowlist:     []string{"important_tool"},
	}
	_ = cfg.Validate()
	bp := processor.NewBaseProcessor(cfg)
	p := &ToolResultBudgetProcessor{BaseProcessor: bp, config: cfg}

	tc := &llm_schema.ToolCall{ID: "tc-1", Name: "important_tool", Arguments: ""}
	am := llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls([]*llm_schema.ToolCall{tc}))
	tm := llm_schema.NewToolMessage("tc-1", strings.Repeat("x", 500))
	messages := []llm_schema.BaseMessage{am, tm}

	if p.shouldOffloadMessage(tm, messages, nil) {
		t.Fatal("白名单工具消息不应被卸载")
	}
}

func TestTRBP_shouldOffloadMessage_符合卸载条件(t *testing.T) {
	p := newTestTRBP()
	tc := &llm_schema.ToolCall{ID: "tc-1", Name: "grep", Arguments: ""}
	am := llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls([]*llm_schema.ToolCall{tc}))
	tm := llm_schema.NewToolMessage("tc-1", strings.Repeat("x", 500))
	messages := []llm_schema.BaseMessage{am, tm}

	if !p.shouldOffloadMessage(tm, messages, nil) {
		t.Fatal("普通工具的大消息应该被卸载")
	}
}

func TestTRBP_isAlreadyOffloaded(t *testing.T) {
	p := newTestTRBP()
	offloaded := schema.NewOffloadToolMessage("tc-x", "<persisted-output>...", "fake-handle", "filesystem")
	if !p.isAlreadyOffloaded(offloaded) {
		t.Fatal("OffloadToolMessage 应被识别为已卸载")
	}
	normal := llm_schema.NewToolMessage("tc-y", "normal content")
	if p.isAlreadyOffloaded(normal) {
		t.Fatal("普通 ToolMessage 不应被识别为已卸载")
	}
}

func TestTRBP_TriggerAddMessages_低于阈值不触发(t *testing.T) {
	cfg := &ToolResultBudgetProcessorConfig{
		TokensThreshold:       100000,
		LargeMessageThreshold: 100,
		TrimSize:              20,
	}
	trbp, _ := NewToolResultBudgetProcessor(cfg)
	mc := &fakeModelContext{
		messages: []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("short"),
			llm_schema.NewToolMessage("tc-1", "short"),
		},
	}
	triggered, err := trbp.TriggerAddMessages(context.Background(), mc, nil)
	if err != nil {
		t.Fatalf("TriggerAddMessages 返回错误: %v", err)
	}
	if triggered {
		t.Fatal("低于阈值期望不触发")
	}
}

func TestTRBP_TriggerAddMessages_超过阈值触发(t *testing.T) {
	cfg := &ToolResultBudgetProcessorConfig{
		TokensThreshold:       100,
		LargeMessageThreshold: 50,
		TrimSize:              20,
	}
	trbp, _ := NewToolResultBudgetProcessor(cfg)
	tc := &llm_schema.ToolCall{ID: "tc-1", Name: "grep", Arguments: ""}
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("task"),
		llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls([]*llm_schema.ToolCall{tc})),
		llm_schema.NewToolMessage("tc-1", strings.Repeat("x", 600)), // 200 tokens by estimate
		llm_schema.NewAssistantMessage("done"),
	}
	mc := &fakeModelContext{messages: messages}
	triggered, err := trbp.TriggerAddMessages(context.Background(), mc, []llm_schema.BaseMessage{}, iface.WithSysOperation(nil))
	if err != nil {
		t.Fatalf("TriggerAddMessages 返回错误: %v", err)
	}
	if !triggered {
		t.Fatal("超过阈值期望触发")
	}
}

func TestTRBP_OnAddMessages_单轮超预算卸载(t *testing.T) {
	cfg := &ToolResultBudgetProcessorConfig{
		TokensThreshold:       100,
		LargeMessageThreshold: 50,
		TrimSize:              20,
	}
	trbp, _ := NewToolResultBudgetProcessor(cfg)
	tc := &llm_schema.ToolCall{ID: "tc-1", Name: "grep", Arguments: ""}
	contextMessages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("task"),
	}
	messagesToAdd := []llm_schema.BaseMessage{
		llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls([]*llm_schema.ToolCall{tc})),
		llm_schema.NewToolMessage("tc-1", strings.Repeat("x", 600)), // 200 tokens
		llm_schema.NewAssistantMessage("done"),
	}
	mc := &fakeModelContext{messages: contextMessages}
	event, result, err := trbp.OnAddMessages(context.Background(), mc, messagesToAdd, iface.WithSysOperation(nil))
	if err != nil {
		t.Fatalf("OnAddMessages 返回错误: %v", err)
	}
	if event == nil {
		t.Fatal("期望有 ContextEvent，实际为 nil")
	}
	if len(event.MessagesToModify) == 0 {
		t.Fatal("期望有消息被修改")
	}
	// 验证 ToolMessage 被替换为 OffloadMessage
	for _, msg := range result {
		if msg.GetRole() == llm_schema.RoleTypeTool && schema.IsOffloaded(msg) {
			content := msg.GetContent().Text()
			if !strings.Contains(content, PersistedOutputTag) {
				t.Errorf("卸载后的消息内容应包含 %s, 实际: %s", PersistedOutputTag, content[:min(200, len(content))])
			}
		}
	}
}

func TestTRBP_OnAddMessages_无需卸载(t *testing.T) {
	cfg := &ToolResultBudgetProcessorConfig{
		TokensThreshold:       999999,
		LargeMessageThreshold: 10000,
		TrimSize:              20,
	}
	trbp, _ := NewToolResultBudgetProcessor(cfg)
	mc := &fakeModelContext{
		messages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("q1")},
	}
	messagesToAdd := []llm_schema.BaseMessage{llm_schema.NewAssistantMessage("a1")}
	event, result, err := trbp.OnAddMessages(context.Background(), mc, messagesToAdd)
	if err != nil {
		t.Fatalf("OnAddMessages 返回错误: %v", err)
	}
	if event != nil {
		t.Fatal("无需卸载时，期望 ContextEvent 为 nil")
	}
	if len(result) != 1 {
		t.Fatalf("期望透传 1 条消息，实际: %d", len(result))
	}
}

func TestBuildPersistedOutputMessage_有更多内容(t *testing.T) {
	result := buildPersistedOutputMessage(50000, "handle123", "preview text", true)
	if !strings.Contains(result, PersistedOutputTag) {
		t.Error("缺少开始标签")
	}
	if !strings.Contains(result, PersistedOutputClosingTag) {
		t.Error("缺少结束标签")
	}
	if !strings.Contains(result, "50000") {
		t.Error("缺少原始大小")
	}
	if !strings.Contains(result, "handle123") {
		t.Error("缺少 offload handle")
	}
	if !strings.Contains(result, "preview text") {
		t.Error("缺少预览内容")
	}
	if !strings.Contains(result, "...") {
		t.Error("has_more=true 时应包含省略号")
	}
}

func TestBuildPersistedOutputMessage_无更多内容(t *testing.T) {
	result := buildPersistedOutputMessage(100, "handle456", "short", false)
	if strings.Contains(result, "...") {
		t.Error("has_more=false 时不应包含省略号")
	}
}
