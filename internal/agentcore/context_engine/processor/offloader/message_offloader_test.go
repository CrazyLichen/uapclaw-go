package offloader

import (
	"context"
	"strings"
	"testing"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeModelContext 用于测试的 ModelContext 模拟实现
type fakeModelContext struct {
	messages   []llm_schema.BaseMessage
	sessionID  string
	tokenCount int
	tokenErr   error
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestMessageOffloaderConfig_Validate_正常配置(t *testing.T) {
	threshold := 100
	keep := 5
	cfg := &MessageOffloaderConfig{
		MessagesThreshold:     &threshold,
		TokensThreshold:       20000,
		LargeMessageThreshold: 1000,
		TrimSize:              100,
		MessagesToKeep:        &keep,
		KeepLastRound:         true,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("期望验证通过，实际错误: %v", err)
	}
}

func TestMessageOffloaderConfig_Validate_TrimSize过大(t *testing.T) {
	cfg := &MessageOffloaderConfig{
		TokensThreshold:       20000,
		LargeMessageThreshold: 100,
		TrimSize:              200, // > LargeMessageThreshold
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("期望 TrimSize >= LargeMessageThreshold 时报错，实际通过")
	}
}

func TestMessageOffloaderConfig_Validate_MessagesToKeep过大(t *testing.T) {
	threshold := 10
	keep := 15
	cfg := &MessageOffloaderConfig{
		MessagesThreshold:     &threshold,
		TokensThreshold:       20000,
		LargeMessageThreshold: 1000,
		TrimSize:              100,
		MessagesToKeep:        &keep,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("期望 MessagesToKeep >= MessagesThreshold 时报错，实际通过")
	}
}

func TestMessageOffloaderConfig_Validate_默认值(t *testing.T) {
	cfg := &MessageOffloaderConfig{
		TokensThreshold:       20000,
		LargeMessageThreshold: 1000,
		TrimSize:              100,
		KeepLastRound:         true,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("期望验证通过，实际错误: %v", err)
	}
	if len(cfg.OffloadMessageTypes) == 0 || cfg.OffloadMessageTypes[0] != "tool" {
		t.Errorf("期望 OffloadMessageTypes 默认为 [tool]，实际: %v", cfg.OffloadMessageTypes)
	}
	if len(cfg.ProtectedToolNames) == 0 || cfg.ProtectedToolNames[0] != "reload_original_context_messages" {
		t.Errorf("期望 ProtectedToolNames 默认值，实际: %v", cfg.ProtectedToolNames)
	}
}

func TestMessageOffloader_shouldOffloadMessage_角色不匹配(t *testing.T) {
	mo := newTestMessageOffloader()
	msg := llm_schema.NewUserMessage(strings.Repeat("a", 2000))
	// OffloadMessageTypes 默认 ["tool"]，UserMessage 不匹配
	if mo.shouldOffloadMessage(msg, nil, nil) {
		t.Fatal("UserMessage 不在 OffloadMessageTypes 中，期望返回 false")
	}
}

func TestMessageOffloader_shouldOffloadMessage_内容太短(t *testing.T) {
	mo := newTestMessageOffloader()
	// 创建短内容的 ToolMessage
	msg := llm_schema.NewToolMessage("call_1", "short content")
	if mo.shouldOffloadMessage(msg, nil, nil) {
		t.Fatal("内容长度 <= LargeMessageThreshold，期望返回 false")
	}
}

func TestMessageOffloader_shouldOffloadMessage_已卸载消息(t *testing.T) {
	mo := newTestMessageOffloader()
	msg := schema.NewOffloadToolMessage("call_1", strings.Repeat("a", 2000), "handle123", "filesystem")
	if mo.shouldOffloadMessage(msg, nil, nil) {
		t.Fatal("已卸载消息不应重复卸载")
	}
}

func TestMessageOffloader_shouldOffloadMessage_受保护工具消息(t *testing.T) {
	mo := newTestMessageOffloader()
	// 构造 ToolMessage + 对应的 AssistantMessage(含 ToolCall)
	tc := &llm_schema.ToolCall{ID: "call_1", Name: "reload_original_context_messages", Arguments: ""}
	am := llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls([]*llm_schema.ToolCall{tc}))
	tm := llm_schema.NewToolMessage("call_1", strings.Repeat("a", 2000))
	messages := []llm_schema.BaseMessage{am, tm}

	if mo.shouldOffloadMessage(tm, messages, nil) {
		t.Fatal("受保护工具消息不应被卸载")
	}
}

func TestMessageOffloader_shouldOffloadMessage_符合卸载条件(t *testing.T) {
	mo := newTestMessageOffloader()
	tc := &llm_schema.ToolCall{ID: "call_1", Name: "grep", Arguments: ""}
	am := llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls([]*llm_schema.ToolCall{tc}))
	tm := llm_schema.NewToolMessage("call_1", strings.Repeat("a", 2000))
	messages := []llm_schema.BaseMessage{am, tm}

	if !mo.shouldOffloadMessage(tm, messages, nil) {
		t.Fatal("普通工具的大消息应该被卸载")
	}
}

func TestMessageOffloader_isProtectedToolMessage_非ToolMessage(t *testing.T) {
	mo := newTestMessageOffloader()
	msg := llm_schema.NewUserMessage("hello")
	if mo.isProtectedToolMessage(msg, nil) {
		t.Fatal("UserMessage 不是 ToolMessage，期望返回 false")
	}
}

func TestMessageOffloader_isProtectedToolMessage_精确匹配(t *testing.T) {
	mo := newTestMessageOffloader()
	tc := &llm_schema.ToolCall{ID: "call_1", Name: "reload_original_context_messages"}
	am := llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls([]*llm_schema.ToolCall{tc}))
	tm := llm_schema.NewToolMessage("call_1", "result")
	messages := []llm_schema.BaseMessage{am, tm}

	if !mo.isProtectedToolMessage(tm, messages) {
		t.Fatal("工具名精确匹配受保护列表，期望返回 true")
	}
}

func TestMessageOffloader_isProtectedToolMessage_通配符匹配(t *testing.T) {
	cfg := &MessageOffloaderConfig{
		TokensThreshold:       20000,
		LargeMessageThreshold: 1000,
		TrimSize:              100,
		ProtectedToolNames:    []string{"read_file:*.md"},
		KeepLastRound:         true,
	}
	_ = cfg.Validate()
	bp := processor.NewBaseProcessor(cfg)
	mo := &MessageOffloader{BaseProcessor: bp, config: cfg}

	tc := &llm_schema.ToolCall{ID: "call_1", Name: "read_file", Arguments: `{"file_path": "/tmp/test.md"}`}
	am := llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls([]*llm_schema.ToolCall{tc}))
	tm := llm_schema.NewToolMessage("call_1", "result")
	messages := []llm_schema.BaseMessage{am, tm}

	if !mo.isProtectedToolMessage(tm, messages) {
		t.Fatal("read_file + *.md 通配符匹配 file_path=/tmp/test.md，期望返回 true")
	}
}

func TestMessageOffloader_isProtectedToolMessage_通配符不匹配(t *testing.T) {
	cfg := &MessageOffloaderConfig{
		TokensThreshold:       20000,
		LargeMessageThreshold: 1000,
		TrimSize:              100,
		ProtectedToolNames:    []string{"read_file:*.md"},
		KeepLastRound:         true,
	}
	_ = cfg.Validate()
	bp := processor.NewBaseProcessor(cfg)
	mo := &MessageOffloader{BaseProcessor: bp, config: cfg}

	tc := &llm_schema.ToolCall{ID: "call_1", Name: "read_file", Arguments: `{"file_path": "/tmp/test.go"}`}
	am := llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls([]*llm_schema.ToolCall{tc}))
	tm := llm_schema.NewToolMessage("call_1", "result")
	messages := []llm_schema.BaseMessage{am, tm}

	if mo.isProtectedToolMessage(tm, messages) {
		t.Fatal("read_file + *.md 不匹配 file_path=/tmp/test.go，期望返回 false")
	}
}

func TestMessageOffloader_isProtectedToolMessage_不匹配(t *testing.T) {
	mo := newTestMessageOffloader()
	tc := &llm_schema.ToolCall{ID: "call_1", Name: "grep"}
	am := llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls([]*llm_schema.ToolCall{tc}))
	tm := llm_schema.NewToolMessage("call_1", "result")
	messages := []llm_schema.BaseMessage{am, tm}

	if mo.isProtectedToolMessage(tm, messages) {
		t.Fatal("grep 不在受保护列表中，期望返回 false")
	}
}

func TestMessageOffloader_getOffloadRange_KeepLastRound(t *testing.T) {
	mo := newTestMessageOffloader()
	// 构造消息：user, assistant(无tool_calls), user, assistant(有tool_calls), tool
	um1 := llm_schema.NewUserMessage("q1")
	am1 := llm_schema.NewAssistantMessage("a1") // 无 tool_calls
	um2 := llm_schema.NewUserMessage("q2")
	tc := &llm_schema.ToolCall{ID: "c1", Name: "grep"}
	am2 := llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls([]*llm_schema.ToolCall{tc}))
	tm := llm_schema.NewToolMessage("c1", "result")
	messages := []llm_schema.BaseMessage{um1, am1, um2, am2, tm}

	// KeepLastRound=true，最后不含 tool_calls 的 AI 消息在 index=1
	offloadRange := mo.getOffloadRange(messages)
	if offloadRange != 1 {
		t.Fatalf("期望 offloadRange=1（最后无 tool_calls 的 AI 消息），实际=%d", offloadRange)
	}
}

func TestMessageOffloader_getOffloadRange_不保留最后一轮(t *testing.T) {
	cfg := &MessageOffloaderConfig{
		TokensThreshold:       20000,
		LargeMessageThreshold: 1000,
		TrimSize:              100,
		KeepLastRound:         false,
	}
	_ = cfg.Validate()
	bp := processor.NewBaseProcessor(cfg)
	mo := &MessageOffloader{BaseProcessor: bp, config: cfg}

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("q1"),
		llm_schema.NewAssistantMessage("a1"),
	}
	offloadRange := mo.getOffloadRange(messages)
	if offloadRange != 2 {
		t.Fatalf("KeepLastRound=false 时，offloadRange 应等于 len(messages)=2，实际=%d", offloadRange)
	}
}

func TestMessageOffloader_getOffloadRange_MessagesToKeep(t *testing.T) {
	keep := 2
	cfg := &MessageOffloaderConfig{
		TokensThreshold:       20000,
		LargeMessageThreshold: 1000,
		TrimSize:              100,
		MessagesToKeep:        &keep,
		KeepLastRound:         false,
	}
	_ = cfg.Validate()
	bp := processor.NewBaseProcessor(cfg)
	mo := &MessageOffloader{BaseProcessor: bp, config: cfg}

	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("q1"),
		llm_schema.NewAssistantMessage("a1"),
		llm_schema.NewUserMessage("q2"),
		llm_schema.NewAssistantMessage("a2"),
		llm_schema.NewUserMessage("q3"),
	}
	offloadRange := mo.getOffloadRange(messages)
	// len=5, keep=2 → offloadRange = 5-2 = 3
	if offloadRange != 3 {
		t.Fatalf("期望 offloadRange=3（5-2），实际=%d", offloadRange)
	}
}

func TestMatchPattern_匹配(t *testing.T) {
	args := map[string]any{"file_path": "/tmp/test.md"}
	if !matchPattern(args, "*.md") {
		t.Fatal("期望 *.md 匹配 /tmp/test.md")
	}
}

func TestMatchPattern_不匹配(t *testing.T) {
	args := map[string]any{"file_path": "/tmp/test.go"}
	if matchPattern(args, "*.md") {
		t.Fatal("期望 *.md 不匹配 /tmp/test.go")
	}
}

func TestMatchPattern_空参数(t *testing.T) {
	args := map[string]any{}
	if matchPattern(args, "*.md") {
		t.Fatal("期望空参数不匹配")
	}
}

func TestExtractToolArgs_JSON格式(t *testing.T) {
	tc := &llm_schema.ToolCall{ID: "c1", Name: "read_file", Arguments: `{"file_path": "/tmp/test.md"}`}
	args := extractToolArgs(tc)
	if args["file_path"] != "/tmp/test.md" {
		t.Fatalf("期望 file_path=/tmp/test.md，实际: %v", args)
	}
}

func TestExtractToolArgs_空参数(t *testing.T) {
	tc := &llm_schema.ToolCall{ID: "c1", Name: "grep", Arguments: ""}
	args := extractToolArgs(tc)
	if len(args) != 0 {
		t.Fatalf("期望空 map，实际: %v", args)
	}
}

func TestExtractToolArgs_无效JSON(t *testing.T) {
	tc := &llm_schema.ToolCall{ID: "c1", Name: "grep", Arguments: "invalid"}
	args := extractToolArgs(tc)
	if len(args) != 0 {
		t.Fatalf("期望空 map（无效 JSON），实际: %v", args)
	}
}

func TestExtractToolArgs_nil(t *testing.T) {
	args := extractToolArgs(nil)
	if len(args) != 0 {
		t.Fatalf("期望空 map（nil ToolCall），实际: %v", args)
	}
}

func TestMessageOffloader_TriggerAddMessages_消息数超阈值(t *testing.T) {
	threshold := 5
	cfg := &MessageOffloaderConfig{
		MessagesThreshold:     &threshold,
		TokensThreshold:       999999,
		LargeMessageThreshold: 1000,
		TrimSize:              100,
		KeepLastRound:         true,
	}
	_ = cfg.Validate()
	mo, _ := NewMessageOffloader(cfg)

	// 构造 6 条消息（超过阈值 5），其中包含大 ToolMessage
	tc := &llm_schema.ToolCall{ID: "c1", Name: "grep"}
	am := llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls([]*llm_schema.ToolCall{tc}))
	tm := llm_schema.NewToolMessage("c1", strings.Repeat("a", 2000))

	existingMessages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("q1"),
		am,
		tm,
		llm_schema.NewAssistantMessage("a1"),
	}
	messagesToAdd := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("q2"),
		llm_schema.NewAssistantMessage("a2"),
	}

	mc := &fakeModelContext{messages: existingMessages, sessionID: "test-session"}
	triggered, err := mo.TriggerAddMessages(context.Background(), mc, messagesToAdd)
	if err != nil {
		t.Fatalf("TriggerAddMessages 返回错误: %v", err)
	}
	if !triggered {
		t.Fatal("消息数超过阈值且有候选，期望触发")
	}
}

func TestMessageOffloader_TriggerAddMessages_未达阈值(t *testing.T) {
	threshold := 100
	cfg := &MessageOffloaderConfig{
		MessagesThreshold:     &threshold,
		TokensThreshold:       999999,
		LargeMessageThreshold: 1000,
		TrimSize:              100,
		KeepLastRound:         true,
	}
	_ = cfg.Validate()
	mo, _ := NewMessageOffloader(cfg)

	mc := &fakeModelContext{
		messages:   []llm_schema.BaseMessage{llm_schema.NewUserMessage("q")},
		sessionID:  "test-session",
		tokenCount: 0,
	}
	triggered, err := mo.TriggerAddMessages(context.Background(), mc, []llm_schema.BaseMessage{llm_schema.NewAssistantMessage("a")})
	if err != nil {
		t.Fatalf("TriggerAddMessages 返回错误: %v", err)
	}
	if triggered {
		t.Fatal("消息数未达阈值，期望不触发")
	}
}

func TestMessageOffloader_TriggerAddMessages_Token数超阈值(t *testing.T) {
	cfg := &MessageOffloaderConfig{
		TokensThreshold:       100,
		LargeMessageThreshold: 1000,
		TrimSize:              100,
		KeepLastRound:         true,
	}
	_ = cfg.Validate()
	mo, _ := NewMessageOffloader(cfg)

	tc := &llm_schema.ToolCall{ID: "c1", Name: "grep"}
	am := llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls([]*llm_schema.ToolCall{tc}))
	tm := llm_schema.NewToolMessage("c1", strings.Repeat("a", 2000))

	existingMessages := []llm_schema.BaseMessage{am, tm}
	messagesToAdd := []llm_schema.BaseMessage{llm_schema.NewAssistantMessage("done")}

	mc := &fakeModelContext{
		messages:   existingMessages,
		sessionID:  "test-session",
		tokenCount: 200, // 超过 TokensThreshold=100
	}
	triggered, err := mo.TriggerAddMessages(context.Background(), mc, messagesToAdd)
	if err != nil {
		t.Fatalf("TriggerAddMessages 返回错误: %v", err)
	}
	if !triggered {
		t.Fatal("Token 数超过阈值且有候选，期望触发")
	}
}

func TestMessageOffloader_OnAddMessages_卸载大消息(t *testing.T) {
	cfg := &MessageOffloaderConfig{
		TokensThreshold:       999999,
		LargeMessageThreshold: 100,
		TrimSize:              10,
		KeepLastRound:         false,
	}
	_ = cfg.Validate()
	mo, _ := NewMessageOffloader(cfg)

	// 构造：大 ToolMessage 应被卸载
	tc := &llm_schema.ToolCall{ID: "c1", Name: "grep"}
	am := llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls([]*llm_schema.ToolCall{tc}))
	largeContent := strings.Repeat("a", 500)
	tm := llm_schema.NewToolMessage("c1", largeContent)

	existingMessages := []llm_schema.BaseMessage{am, tm}
	messagesToAdd := []llm_schema.BaseMessage{llm_schema.NewAssistantMessage("done")}

	mc := &fakeModelContext{messages: existingMessages, sessionID: "test-session"}
	event, _, err := mo.OnAddMessages(context.Background(), mc, messagesToAdd)
	if err != nil {
		t.Fatalf("OnAddMessages 返回错误: %v", err)
	}
	if event == nil {
		t.Fatal("期望返回 ContextEvent，实际为 nil")
	}
	if event.EventType != "MessageOffloader" {
		t.Fatalf("期望 EventType=MessageOffloader，实际=%s", event.EventType)
	}
	if len(event.MessagesToModify) == 0 {
		t.Fatal("期望有消息被修改")
	}

	// 验证消息列表中原来的 ToolMessage 被替换为 OffloadMessage
	updatedContext, _ := mc.GetMessages(0, true)
	for _, msg := range updatedContext {
		if msg.GetRole() == llm_schema.RoleTypeTool {
			if schema.IsOffloaded(msg) {
				// 被卸载的 ToolMessage 的 content 应该被截断
				content := msg.GetContent().Text()
				if !strings.Contains(content, omitString) {
					t.Fatalf("卸载后的消息内容应包含省略标记，实际: %s", content)
				}
			}
		}
	}
}

func TestMessageOffloader_OnAddMessages_无需卸载(t *testing.T) {
	cfg := &MessageOffloaderConfig{
		TokensThreshold:       999999,
		LargeMessageThreshold: 10000, // 很高，不会有消息超限
		TrimSize:              100,
		KeepLastRound:         false,
	}
	_ = cfg.Validate()
	mo, _ := NewMessageOffloader(cfg)

	existingMessages := []llm_schema.BaseMessage{llm_schema.NewUserMessage("q1")}
	messagesToAdd := []llm_schema.BaseMessage{llm_schema.NewAssistantMessage("a1")}

	mc := &fakeModelContext{messages: existingMessages, sessionID: "test-session"}
	event, result, err := mo.OnAddMessages(context.Background(), mc, messagesToAdd)
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

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestMessageOffloader 创建测试用 MessageOffloader 实例
func newTestMessageOffloader() *MessageOffloader {
	cfg := &MessageOffloaderConfig{
		TokensThreshold:       20000,
		LargeMessageThreshold: 1000,
		TrimSize:              100,
		KeepLastRound:         true,
	}
	_ = cfg.Validate() // 应用默认值
	bp := processor.NewBaseProcessor(cfg)
	return &MessageOffloader{
		BaseProcessor: bp,
		config:        cfg,
	}
}

// fakeModelContext 方法实现

func (f *fakeModelContext) Len() int                                           { return len(f.messages) }
func (f *fakeModelContext) GetMessages(_ int, _ bool) ([]llm_schema.BaseMessage, error) { return f.messages, nil }
func (f *fakeModelContext) SetMessages(msgs []llm_schema.BaseMessage, _ bool)  { f.messages = msgs }
func (f *fakeModelContext) PopMessages(_ int, _ bool) []llm_schema.BaseMessage { return nil }
func (f *fakeModelContext) ClearMessages(_ context.Context, _ bool, _ ...iface.Option) error {
	return nil
}
func (f *fakeModelContext) AddMessages(_ context.Context, _ llm_schema.BaseMessage, _ ...iface.Option) ([]llm_schema.BaseMessage, error) {
	return nil, nil
}
func (f *fakeModelContext) GetContextWindow(_ context.Context, _ []llm_schema.BaseMessage,
	_ []*commonschema.ToolInfo, _ int, _ int, _ ...iface.Option) (*iface.ContextWindow, error) {
	return nil, nil
}
func (f *fakeModelContext) Statistic() *iface.ContextStats                       { return nil }
func (f *fakeModelContext) SessionID() string                                    { return f.sessionID }
func (f *fakeModelContext) ContextID() string                                    { return "" }
func (f *fakeModelContext) TokenCounter() token.TokenCounter                     { return f }
func (f *fakeModelContext) ReloaderTool() tool.Tool                              { return nil }
func (f *fakeModelContext) WorkspaceDir() string                                 { return "" }
func (f *fakeModelContext) SetSessionRef(_ sessioninterfaces.SessionFacade)      {}
func (f *fakeModelContext) GetSessionRef() sessioninterfaces.SessionFacade       { return nil }
func (f *fakeModelContext) OffloadMessages(_ string, _ []llm_schema.BaseMessage) {}
func (f *fakeModelContext) SaveState() map[string]any                            { return nil }
func (f *fakeModelContext) LoadState(_ map[string]any)                           {}
func (f *fakeModelContext) CompressContext(_ context.Context, _ ...iface.CompressContextOption) (string, error) {
	return "", nil
}

// 实现 token.TokenCounter 接口
func (f *fakeModelContext) Count(text string, model string) (int, error) {
	return f.tokenCount, f.tokenErr
}
func (f *fakeModelContext) CountMessages(messages []llm_schema.BaseMessage, model string) (int, error) {
	return f.tokenCount, f.tokenErr
}
func (f *fakeModelContext) CountTools(tools []*commonschema.ToolInfo, model string) (int, error) {
	return 0, nil
}
