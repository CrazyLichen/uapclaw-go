package iface

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── ContextEngineOptions 测试 ────────────────────────────

// TestNewContextEngineOptions 无选项时返回零值
func TestNewContextEngineOptions(t *testing.T) {
	o := NewContextEngineOptions()
	if o == nil {
		t.Fatal("NewContextEngineOptions() 返回 nil")
	}
	if o.Workspace != nil {
		t.Error("Workspace 应为 nil")
	}
	if o.SysOperation != nil {
		t.Error("SysOperation 应为 nil")
	}
}

// TestNewContextEngineOptions_带选项 测试选项函数生效
func TestNewContextEngineOptions_带选项(t *testing.T) {
	o := NewContextEngineOptions(
		WithWorkspace(nil),
		WithEngineSysOperation(nil),
	)
	if o == nil {
		t.Fatal("NewContextEngineOptions() 返回 nil")
	}
}

// TestWithWorkspace 设置工作空间
func TestWithWorkspace(t *testing.T) {
	o := &ContextEngineOptions{}
	WithWorkspace(nil)(o)
	if o.Workspace != nil {
		t.Error("Workspace 应为 nil（显式设置 nil）")
	}
}

// TestWithEngineSysOperation 设置系统操作接口
func TestWithEngineSysOperation(t *testing.T) {
	o := &ContextEngineOptions{}
	WithEngineSysOperation(nil)(o)
	if o.SysOperation != nil {
		t.Error("SysOperation 应为 nil（显式设置 nil）")
	}
}

// ──────────────────────────── CreateContextOptions 测试 ────────────────────────────

// TestNewCreateContextOptions 无选项时返回零值
func TestNewCreateContextOptions(t *testing.T) {
	o := NewCreateContextOptions()
	if o == nil {
		t.Fatal("NewCreateContextOptions() 返回 nil")
	}
	if o.Processors != nil {
		t.Error("Processors 应为 nil")
	}
}

// TestNewCreateContextOptions_带选项 测试选项函数生效
func TestNewCreateContextOptions_带选项(t *testing.T) {
	specs := []ProcessorSpec{{Type: "test", Config: nil}}
	o := NewCreateContextOptions(WithProcessors(specs))
	if len(o.Processors) != 1 {
		t.Errorf("Processors 长度 = %d, 期望 1", len(o.Processors))
	}
	if o.Processors[0].Type != "test" {
		t.Errorf("Processors[0].Type = %q, 期望 %q", o.Processors[0].Type, "test")
	}
}

// TestWithHistoryMessages 设置历史消息
func TestWithHistoryMessages(t *testing.T) {
	msgs := []llm_schema.BaseMessage{llm_schema.NewSystemMessage("hello")}
	o := &CreateContextOptions{}
	WithHistoryMessages(msgs)(o)
	if len(o.HistoryMessages) != 1 {
		t.Errorf("HistoryMessages 长度 = %d, 期望 1", len(o.HistoryMessages))
	}
}

// TestWithTokenCounter 设置Token计数器
func TestWithTokenCounter(t *testing.T) {
	o := &CreateContextOptions{}
	WithTokenCounter(nil)(o)
	if o.TokenCounter != nil {
		t.Error("TokenCounter 应为 nil（显式设置 nil）")
	}
}

// ──────────────────────────── CompressContextOptions 测试 ────────────────────────────

// TestNewCompressContextOptions 无选项时返回零值
func TestNewCompressContextOptions(t *testing.T) {
	o := NewCompressContextOptions()
	if o == nil {
		t.Fatal("NewCompressContextOptions() 返回 nil")
	}
}

// TestNewCompressContextOptions_带选项 测试选项函数生效
func TestNewCompressContextOptions_带选项(t *testing.T) {
	o := NewCompressContextOptions(
		WithProcessorTypes([]string{"compressor"}),
		WithModelName("qwen-max"),
		WithCompressSessionID("sess-1"),
	)
	if len(o.ProcessorTypes) != 1 || o.ProcessorTypes[0] != "compressor" {
		t.Errorf("ProcessorTypes = %v, 期望 [compressor]", o.ProcessorTypes)
	}
	if o.ModelName != "qwen-max" {
		t.Errorf("ModelName = %q, 期望 %q", o.ModelName, "qwen-max")
	}
	if o.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, 期望 %q", o.SessionID, "sess-1")
	}
}

// TestWithCompressSysOperation 设置压缩时的系统操作接口
func TestWithCompressSysOperation(t *testing.T) {
	o := &CompressContextOptions{}
	WithCompressSysOperation(nil)(o)
	if o.SysOperation != nil {
		t.Error("SysOperation 应为 nil")
	}
}

// ──────────────────────────── ClearContextOptions 测试 ────────────────────────────

// TestNewClearContextOptions 无选项时返回零值
func TestNewClearContextOptions(t *testing.T) {
	o := NewClearContextOptions()
	if o == nil {
		t.Fatal("NewClearContextOptions() 返回 nil")
	}
}

// TestNewClearContextOptions_带选项 测试选项函数生效
func TestNewClearContextOptions_带选项(t *testing.T) {
	o := NewClearContextOptions(
		WithSessionID("sess-1"),
		WithContextID("ctx-1"),
	)
	if o.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, 期望 %q", o.SessionID, "sess-1")
	}
	if o.ContextID != "ctx-1" {
		t.Errorf("ContextID = %q, 期望 %q", o.ContextID, "ctx-1")
	}
}

// ──────────────────────────── ContextWindow 测试 ────────────────────────────

// TestNewContextWindow 创建上下文窗口实例
func TestNewContextWindow(t *testing.T) {
	cw := NewContextWindow()
	if cw == nil {
		t.Fatal("NewContextWindow() 返回 nil")
	}
	if cw.SystemMessages == nil {
		t.Error("SystemMessages 应为空切片，不是 nil")
	}
	if cw.ContextMessages == nil {
		t.Error("ContextMessages 应为空切片，不是 nil")
	}
	if cw.Tools == nil {
		t.Error("Tools 应为空切片，不是 nil")
	}
	if len(cw.SystemMessages) != 0 {
		t.Errorf("SystemMessages 长度 = %d, 期望 0", len(cw.SystemMessages))
	}
}

// TestContextWindow_GetMessages 合并系统消息和上下文消息
func TestContextWindow_GetMessages(t *testing.T) {
	cw := NewContextWindow()
	msgs := cw.GetMessages()
	if len(msgs) != 0 {
		t.Errorf("空窗口 GetMessages() 长度 = %d, 期望 0", len(msgs))
	}
}

// TestContextWindow_GetMessages_有消息 测试合并消息
func TestContextWindow_GetMessages_有消息(t *testing.T) {
	sysMsg := llm_schema.NewSystemMessage("system")
	userMsg := llm_schema.NewUserMessage("hello")
	cw := &ContextWindow{
		SystemMessages:  []llm_schema.BaseMessage{sysMsg},
		ContextMessages: []llm_schema.BaseMessage{userMsg},
	}
	msgs := cw.GetMessages()
	if len(msgs) != 2 {
		t.Fatalf("GetMessages() 长度 = %d, 期望 2", len(msgs))
	}
	if msgs[0].GetRole() != llm_schema.RoleTypeSystem {
		t.Errorf("第一条消息角色 = %v, 期望 System", msgs[0].GetRole())
	}
	if msgs[1].GetRole() != llm_schema.RoleTypeUser {
		t.Errorf("第二条消息角色 = %v, 期望 User", msgs[1].GetRole())
	}
}

// TestContextWindow_GetTools 返回工具列表
func TestContextWindow_GetTools(t *testing.T) {
	cw := NewContextWindow()
	tools := cw.GetTools()
	if len(tools) != 0 {
		t.Errorf("空窗口 GetTools() 长度 = %d, 期望 0", len(tools))
	}
}

// ──────────────────────────── ContextStats 测试 ────────────────────────────

// TestContextStats_StatMessages_空消息 测试空消息列表
func TestContextStats_StatMessages_空消息(t *testing.T) {
	s := &ContextStats{}
	s.StatMessages(nil, nil)
	if s.TotalMessages != 0 {
		t.Errorf("TotalMessages = %d, 期望 0", s.TotalMessages)
	}
	if s.TotalTokens != 0 {
		t.Errorf("TotalTokens = %d, 期望 0", s.TotalTokens)
	}
}

// TestContextStats_StatMessages_各角色消息 测试按角色统计消息
func TestContextStats_StatMessages_各角色消息(t *testing.T) {
	s := &ContextStats{}
	messages := []llm_schema.BaseMessage{
		llm_schema.NewSystemMessage("系统提示"),
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("回复"),
		llm_schema.NewToolMessage("tc-1", "工具结果"),
	}
	s.StatMessages(messages, nil)
	if s.TotalMessages != 4 {
		t.Errorf("TotalMessages = %d, 期望 4", s.TotalMessages)
	}
	if s.SystemMessages != 1 {
		t.Errorf("SystemMessages = %d, 期望 1", s.SystemMessages)
	}
	if s.UserMessages != 1 {
		t.Errorf("UserMessages = %d, 期望 1", s.UserMessages)
	}
	if s.AssistantMessages != 1 {
		t.Errorf("AssistantMessages = %d, 期望 1", s.AssistantMessages)
	}
	if s.ToolMessages != 1 {
		t.Errorf("ToolMessages = %d, 期望 1", s.ToolMessages)
	}
	// 无 TokenCounter 时使用 fallback（字符串长度/4）
	if s.TotalTokens <= 0 {
		t.Error("TotalTokens 应大于 0")
	}
}

// TestContextStats_StatMessages_UsageMetadata优先 测试 AssistantMessage 的 UsageMetadata 优先
func TestContextStats_StatMessages_UsageMetadata优先(t *testing.T) {
	s := &ContextStats{}
	am := llm_schema.NewAssistantMessage("回复",
		llm_schema.WithAssistantUsageMetadata(&llm_schema.UsageMetadata{TotalTokens: 100}),
	)
	messages := []llm_schema.BaseMessage{am}
	s.StatMessages(messages, nil)
	if s.TotalTokens != 100 {
		t.Errorf("TotalTokens = %d, 期望 100（从 UsageMetadata 获取）", s.TotalTokens)
	}
}

// TestContextStats_StatTools_空工具 测试空工具列表
func TestContextStats_StatTools_空工具(t *testing.T) {
	s := &ContextStats{}
	s.StatTools(nil, nil)
	if s.Tools != 0 {
		t.Errorf("Tools = %d, 期望 0", s.Tools)
	}
}

// TestContextStats_StatTools_有工具 测试工具统计
func TestContextStats_StatTools_有工具(t *testing.T) {
	s := &ContextStats{}
	tools := []schema.ToolInfoInterface{&fakeToolInfo{name: "test_tool", description: "测试工具"}}
	s.StatTools(tools, nil)
	if s.Tools != 1 {
		t.Errorf("Tools = %d, 期望 1", s.Tools)
	}
	if s.ToolTokens <= 0 {
		t.Error("ToolTokens 应大于 0")
	}
	if s.TotalTokens <= 0 {
		t.Error("TotalTokens 应大于 0（累加 ToolTokens）")
	}
}

// ──────────────────────────── ProcessorOption 测试 ────────────────────────────

// TestNewProcessorOption 无选项时返回零值
func TestNewProcessorOption(t *testing.T) {
	po := NewProcessorOption()
	if po == nil {
		t.Fatal("NewProcessorOption() 返回 nil")
	}
}

// TestNewProcessorOption_带选项 测试选项函数生效
func TestNewProcessorOption_带选项(t *testing.T) {
	po := NewProcessorOption(
		WithOffloadHandle("handle-1"),
		WithOffloadType("filesystem"),
		WithOffloadPath("/tmp/offload"),
		WithToolCallID("tc-1"),
		WithName("agent"),
		WithProcessorModelName("qwen-max"),
	)
	if po.OffloadHandle != "handle-1" {
		t.Errorf("OffloadHandle = %q, 期望 %q", po.OffloadHandle, "handle-1")
	}
	if po.OffloadType != "filesystem" {
		t.Errorf("OffloadType = %q, 期望 %q", po.OffloadType, "filesystem")
	}
	if po.OffloadPath != "/tmp/offload" {
		t.Errorf("OffloadPath = %q, 期望 %q", po.OffloadPath, "/tmp/offload")
	}
	if po.ToolCallID != "tc-1" {
		t.Errorf("ToolCallID = %q, 期望 %q", po.ToolCallID, "tc-1")
	}
	if po.Name != "agent" {
		t.Errorf("Name = %q, 期望 %q", po.Name, "agent")
	}
	if po.ModelName != "qwen-max" {
		t.Errorf("ModelName = %q, 期望 %q", po.ModelName, "qwen-max")
	}
}

// TestWithExtra 设置额外参数
func TestWithExtra(t *testing.T) {
	po := &ProcessorOption{}
	WithExtra("key1", "value1")(po)
	if po.Extra == nil {
		t.Fatal("Extra 应为非 nil")
	}
	if po.Extra["key1"] != "value1" {
		t.Errorf("Extra[\"key1\"] = %v, 期望 %v", po.Extra["key1"], "value1")
	}
}

// TestWithMetadata 设置附加元数据
func TestWithMetadata(t *testing.T) {
	po := &ProcessorOption{}
	md := map[string]any{"k": "v"}
	WithMetadata(md)(po)
	if po.Metadata["k"] != "v" {
		t.Errorf("Metadata[\"k\"] = %v, 期望 %v", po.Metadata["k"], "v")
	}
}

// ──────────────────────────── ProcessorSpec 测试 ────────────────────────────

// TestProcessorSpec 字段赋值
func TestProcessorSpec(t *testing.T) {
	spec := ProcessorSpec{Type: "compressor", Config: nil}
	if spec.Type != "compressor" {
		t.Errorf("Type = %q, 期望 %q", spec.Type, "compressor")
	}
}

// ──────────────────────────── ContextEvent 测试 ────────────────────────────

// TestContextEvent 字段赋值
func TestContextEvent(t *testing.T) {
	evt := &ContextEvent{
		EventType:        "compressor",
		MessagesToModify: []int{0, 1},
		CompactSummary:   "摘要",
		CompressionUsage: map[string]any{"tokens": 50},
	}
	if evt.EventType != "compressor" {
		t.Errorf("EventType = %q, 期望 %q", evt.EventType, "compressor")
	}
	if len(evt.MessagesToModify) != 2 {
		t.Errorf("MessagesToModify 长度 = %d, 期望 2", len(evt.MessagesToModify))
	}
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestGetLastAssistantUsageTokens_无AssistantMessage 测试消息列表无 AssistantMessage
func TestGetLastAssistantUsageTokens_无AssistantMessage(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("hello"),
	}
	result := getLastAssistantUsageTokens(messages)
	if result != 0 {
		t.Errorf("getLastAssistantUsageTokens() = %d, 期望 0", result)
	}
}

// TestGetLastAssistantUsageTokens_有UsageMetadata 测试有 UsageMetadata 的 AssistantMessage
func TestGetLastAssistantUsageTokens_有UsageMetadata(t *testing.T) {
	am := llm_schema.NewAssistantMessage("回复",
		llm_schema.WithAssistantUsageMetadata(&llm_schema.UsageMetadata{TotalTokens: 200}),
	)
	messages := []llm_schema.BaseMessage{am}
	result := getLastAssistantUsageTokens(messages)
	if result != 200 {
		t.Errorf("getLastAssistantUsageTokens() = %d, 期望 200", result)
	}
}

// TestGetLastAssistantUsageTokens_多条AssistantMessage取最后一条 测试取最后一条
func TestGetLastAssistantUsageTokens_多条AssistantMessage取最后一条(t *testing.T) {
	am1 := llm_schema.NewAssistantMessage("回复1",
		llm_schema.WithAssistantUsageMetadata(&llm_schema.UsageMetadata{TotalTokens: 100}),
	)
	am2 := llm_schema.NewAssistantMessage("回复2",
		llm_schema.WithAssistantUsageMetadata(&llm_schema.UsageMetadata{TotalTokens: 300}),
	)
	messages := []llm_schema.BaseMessage{am1, am2}
	result := getLastAssistantUsageTokens(messages)
	if result != 300 {
		t.Errorf("getLastAssistantUsageTokens() = %d, 期望 300（取最后一条）", result)
	}
}

// TestCountSingleMessageTokens_nilTokenCounter 测试 TokenCounter 为 nil 时的 fallback
func TestCountSingleMessageTokens_nilTokenCounter(t *testing.T) {
	msg := llm_schema.NewUserMessage("hello world")
	result := countSingleMessageTokens(msg, nil)
	// fallback: len("hello world") / 4 = 11 / 4 = 2
	if result != 2 {
		t.Errorf("countSingleMessageTokens() = %d, 期望 2", result)
	}
}

// ──────────────────────────── fakeToolInfo 测试辅助 ────────────────────────────

// fakeToolInfo 用于测试的模拟工具信息
type fakeToolInfo struct {
	name        string
	description string
	parameters  map[string]any
}

func (f *fakeToolInfo) GetType() string               { return "function" }
func (f *fakeToolInfo) GetName() string               { return f.name }
func (f *fakeToolInfo) GetDescription() string        { return f.description }
func (f *fakeToolInfo) GetParameters() map[string]any { return f.parameters }

// ──────────────────────────── TokenCounter mock 测试辅助 ────────────────────────────

// fakeTokenCounter 用于测试的模拟 Token 计数器
type fakeTokenCounter struct {
	count    int
	countErr error
}

func (f *fakeTokenCounter) Count(_ string, _ string) (int, error) {
	return f.count, f.countErr
}

func (f *fakeTokenCounter) CountMessages(_ []llm_schema.BaseMessage, _ string) (int, error) {
	return f.count, f.countErr
}

func (f *fakeTokenCounter) CountTools(_ []schema.ToolInfoInterface, _ string) (int, error) {
	return f.count, f.countErr
}

// 确保 fake 类型满足接口
var _ token.TokenCounter = (*fakeTokenCounter)(nil)
var _ schema.ToolInfoInterface = (*fakeToolInfo)(nil)
