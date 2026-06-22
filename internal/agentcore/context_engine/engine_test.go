package context_engine

import (
	"context"
	"testing"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	common_schema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockModelContext 测试用 ModelContext mock
type mockModelContext struct {
	sessionID  string
	contextID  string
	messages   []llm_schema.BaseMessage
	saveState  map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

func (m *mockModelContext) Len() int                                            { return len(m.messages) }
func (m *mockModelContext) GetMessages(_ *int, _ bool) []llm_schema.BaseMessage { return m.messages }
func (m *mockModelContext) SetMessages(msgs []llm_schema.BaseMessage, _ bool)   { m.messages = msgs }
func (m *mockModelContext) PopMessages(_ int, _ bool) []llm_schema.BaseMessage  { return nil }
func (m *mockModelContext) ClearMessages(_ context.Context, _ bool, _ ...iface.Option) error { m.messages = nil; return nil }
func (m *mockModelContext) AddMessages(_ context.Context, _ llm_schema.BaseMessage, _ ...iface.Option) ([]llm_schema.BaseMessage, error) {
	return m.messages, nil
}
func (m *mockModelContext) GetContextWindow(_ context.Context, _ []llm_schema.BaseMessage, _ []*common_schema.ToolInfo, _ *int, _ *int, _ ...iface.Option) (*iface.ContextWindow, error) {
	return iface.NewContextWindow(), nil
}
func (m *mockModelContext) Statistic() *iface.ContextStats   { return &iface.ContextStats{} }
func (m *mockModelContext) SessionID() string                { return m.sessionID }
func (m *mockModelContext) ContextID() string                { return m.contextID }
func (m *mockModelContext) TokenCounter() token.TokenCounter { return nil }
func (m *mockModelContext) ReloaderTool() tool.Tool          { return nil }
func (m *mockModelContext) WorkspaceDir() string             { return "" }
func (m *mockModelContext) SetSessionRef(_ *session.Session) {}
func (m *mockModelContext) OffloadMessages(_ string, _ []llm_schema.BaseMessage) {}
func (m *mockModelContext) SaveState() map[string]any        { return m.saveState }
func (m *mockModelContext) LoadState(_ map[string]any)       {}
func (m *mockModelContext) CompressContext(_ context.Context, _ ...iface.CompressContextOption) (string, error) {
	return compressResultNoop, nil
}

func TestNewContextEngine_默认配置(t *testing.T) {
	config := schema.NewContextEngineConfig()
	ce := NewContextEngine(config)
	if ce == nil {
		t.Fatal("期望返回非 nil ContextEngine")
	}
}

func TestNewContextEngine_WithOptions(t *testing.T) {
	config := schema.NewContextEngineConfig()
	ce := NewContextEngine(config,
		iface.WithWorkspace("test_workspace"),
		iface.WithEngineSysOperation("test_sys_op"),
	)
	if ce == nil {
		t.Fatal("期望返回非 nil ContextEngine")
	}
}

func TestContextEngine_GetContext_不存在(t *testing.T) {
	config := schema.NewContextEngineConfig()
	ce := NewContextEngine(config)
	mc := ce.GetContext("nonexistent", "session1")
	if mc != nil {
		t.Fatal("期望返回 nil")
	}
}

func TestProcessContextID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"with.dot", "with_dot"},
		{"a.b.c", "a_b_c"},
		{"nochange", "nochange"},
		{"", ""},
	}
	for _, tt := range tests {
		result := processContextID(tt.input)
		if result != tt.expected {
			t.Errorf("processContextID(%q) = %q, 期望 %q", tt.input, result, tt.expected)
		}
	}
}

func TestContextEngine_ClearContext_精确删除(t *testing.T) {
	config := schema.NewContextEngineConfig()
	ce := NewContextEngine(config).(*contextEngine)

	// 手动添加 mock 上下文到池
	mc1 := &mockModelContext{sessionID: "s1", contextID: "c1"}
	mc2 := &mockModelContext{sessionID: "s1", contextID: "c2"}
	mc3 := &mockModelContext{sessionID: "s2", contextID: "c1"}
	ce.contextPool["s1_c1"] = mc1
	ce.contextPool["s1_c2"] = mc2
	ce.contextPool["s2_c1"] = mc3

	// 精确删除 s1_c1
	err := ce.ClearContext(context.Background(),
		iface.WithSessionID("s1"),
		iface.WithContextID("c1"),
	)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if ce.GetContext("c1", "s1") != nil {
		t.Fatal("精确删除后 s1_c1 应为 nil")
	}
	if ce.GetContext("c2", "s1") == nil {
		t.Fatal("精确删除不应影响 s1_c2")
	}
	if ce.GetContext("c1", "s2") == nil {
		t.Fatal("精确删除不应影响 s2_c1")
	}
}

func TestContextEngine_ClearContext_按session删除(t *testing.T) {
	config := schema.NewContextEngineConfig()
	ce := NewContextEngine(config).(*contextEngine)

	mc1 := &mockModelContext{sessionID: "s1", contextID: "c1"}
	mc2 := &mockModelContext{sessionID: "s1", contextID: "c2"}
	mc3 := &mockModelContext{sessionID: "s2", contextID: "c1"}
	ce.contextPool["s1_c1"] = mc1
	ce.contextPool["s1_c2"] = mc2
	ce.contextPool["s2_c1"] = mc3

	err := ce.ClearContext(context.Background(),
		iface.WithSessionID("s1"),
	)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if ce.GetContext("c1", "s1") != nil {
		t.Fatal("按 session 删除后 s1_c1 应为 nil")
	}
	if ce.GetContext("c2", "s1") != nil {
		t.Fatal("按 session 删除后 s1_c2 应为 nil")
	}
	if ce.GetContext("c1", "s2") == nil {
		t.Fatal("按 session 删除不应影响 s2_c1")
	}
}

func TestContextEngine_ClearContext_清空所有(t *testing.T) {
	config := schema.NewContextEngineConfig()
	ce := NewContextEngine(config).(*contextEngine)

	mc1 := &mockModelContext{sessionID: "s1", contextID: "c1"}
	mc2 := &mockModelContext{sessionID: "s2", contextID: "c1"}
	ce.contextPool["s1_c1"] = mc1
	ce.contextPool["s2_c1"] = mc2

	err := ce.ClearContext(context.Background())
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if ce.GetContext("c1", "s1") != nil {
		t.Fatal("清空所有后 s1_c1 应为 nil")
	}
	if ce.GetContext("c1", "s2") != nil {
		t.Fatal("清空所有后 s2_c1 应为 nil")
	}
}

func TestContextEngine_ClearContext_未找到时无错误(t *testing.T) {
	config := schema.NewContextEngineConfig()
	ce := NewContextEngine(config).(*contextEngine)

	// 按 session 删除不存在的 session
	err := ce.ClearContext(context.Background(),
		iface.WithSessionID("nonexistent"),
	)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}

	// 精确删除不存在的 context
	err = ce.ClearContext(context.Background(),
		iface.WithSessionID("s1"),
		iface.WithContextID("nonexistent"),
	)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
}

func TestContextEngine_CompressContext_上下文不存在(t *testing.T) {
	config := schema.NewContextEngineConfig()
	ce := NewContextEngine(config)
	_, err := ce.CompressContext(context.Background(), "nonexistent", nil)
	if err == nil {
		t.Fatal("期望返回错误")
	}
}

func TestContextEngine_CompressContext_正常压缩(t *testing.T) {
	config := schema.NewContextEngineConfig()
	ce := NewContextEngine(config).(*contextEngine)

	sess := session.NewSession(session.WithSessionID("s1"))
	mc := &mockModelContext{sessionID: "s1", contextID: "ctx1"}
	ce.contextPool["s1_ctx1"] = mc

	result, err := ce.CompressContext(context.Background(), "ctx1", sess)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if result != compressResultNoop {
		t.Fatalf("期望 %q, 实际 %q", compressResultNoop, result)
	}
}

func TestContextEngine_SaveContexts_session为nil(t *testing.T) {
	config := schema.NewContextEngineConfig()
	ce := NewContextEngine(config)
	states, err := ce.SaveContexts(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if states != nil {
		t.Fatal("session 为 nil 时期望返回 nil states")
	}
}

func TestContextEngine_SaveContexts_正常保存(t *testing.T) {
	config := schema.NewContextEngineConfig()
	ce := NewContextEngine(config).(*contextEngine)

	sess := session.NewSession(session.WithSessionID("s1"))
	mc := &mockModelContext{
		sessionID: "s1",
		contextID: "ctx1",
		saveState: map[string]any{"messages": "test"},
	}
	ce.contextPool["s1_ctx1"] = mc

	states, err := ce.SaveContexts(context.Background(), sess, []string{"ctx1"})
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if states == nil {
		t.Fatal("期望返回非 nil states")
	}
	if _, ok := states["ctx1"]; !ok {
		t.Fatal("期望 states 包含 ctx1")
	}
}

func TestContextEngine_GetContext_存在时返回(t *testing.T) {
	config := schema.NewContextEngineConfig()
	ce := NewContextEngine(config).(*contextEngine)

	mc := &mockModelContext{sessionID: "s1", contextID: "ctx1"}
	ce.contextPool["s1_ctx1"] = mc

	result := ce.GetContext("ctx1", "s1")
	if result == nil {
		t.Fatal("期望返回非 nil ModelContext")
	}
	if result.ContextID() != "ctx1" {
		t.Fatalf("期望 contextID=ctx1, 实际=%s", result.ContextID())
	}
}

func TestContextEngine_GetContext_点号替换(t *testing.T) {
	config := schema.NewContextEngineConfig()
	ce := NewContextEngine(config).(*contextEngine)

	mc := &mockModelContext{sessionID: "s1", contextID: "ctx_1"}
	ce.contextPool["s1_ctx_1"] = mc

	result := ce.GetContext("ctx.1", "s1")
	if result == nil {
		t.Fatal("期望点号替换后能找到上下文")
	}
}

func TestContextEngine_CreateContext_531未实现(t *testing.T) {
	config := schema.NewContextEngineConfig()
	ce := NewContextEngine(config)

	sess := session.NewSession(session.WithSessionID("s1"))
	_, err := ce.CreateContext(context.Background(), "ctx1", sess)
	if err == nil {
		t.Fatal("5.31 未实现时期望返回错误")
	}
}

func TestContextEngine_createProcessor_未注册类型(t *testing.T) {
	config := schema.NewContextEngineConfig()
	ce := NewContextEngine(config).(*contextEngine)

	_, err := ce.createProcessor("nonexistent_type", nil)
	if err == nil {
		t.Fatal("未注册处理器类型时期望返回错误")
	}
}

func TestContextEngine_SaveContexts_按session收集(t *testing.T) {
	config := schema.NewContextEngineConfig()
	ce := NewContextEngine(config).(*contextEngine)

	sess := session.NewSession(session.WithSessionID("s1"))
	mc1 := &mockModelContext{
		sessionID: "s1",
		contextID: "ctx1",
		saveState: map[string]any{"key": "val1"},
	}
	mc2 := &mockModelContext{
		sessionID: "s1",
		contextID: "ctx2",
		saveState: map[string]any{"key": "val2"},
	}
	mc3 := &mockModelContext{
		sessionID: "s2",
		contextID: "ctx1",
		saveState: map[string]any{"key": "val3"},
	}
	ce.contextPool["s1_ctx1"] = mc1
	ce.contextPool["s1_ctx2"] = mc2
	ce.contextPool["s2_ctx1"] = mc3

	// contextIDs 为 nil 时自动收集 s1 下所有上下文
	states, err := ce.SaveContexts(context.Background(), sess, nil)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if len(states) != 2 {
		t.Fatalf("期望 2 个状态, 实际 %d", len(states))
	}
	if _, ok := states["ctx1"]; !ok {
		t.Fatal("期望 states 包含 ctx1")
	}
	if _, ok := states["ctx2"]; !ok {
		t.Fatal("期望 states 包含 ctx2")
	}
}

func TestContextEngine_SaveContexts_指定contextIDs(t *testing.T) {
	config := schema.NewContextEngineConfig()
	ce := NewContextEngine(config).(*contextEngine)

	sess := session.NewSession(session.WithSessionID("s1"))
	mc1 := &mockModelContext{
		sessionID: "s1",
		contextID: "ctx1",
		saveState: map[string]any{"key": "val1"},
	}
	mc2 := &mockModelContext{
		sessionID: "s1",
		contextID: "ctx2",
		saveState: map[string]any{"key": "val2"},
	}
	ce.contextPool["s1_ctx1"] = mc1
	ce.contextPool["s1_ctx2"] = mc2

	states, err := ce.SaveContexts(context.Background(), sess, []string{"ctx1"})
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if len(states) != 1 {
		t.Fatalf("期望 1 个状态, 实际 %d", len(states))
	}
	if _, ok := states["ctx1"]; !ok {
		t.Fatal("期望 states 包含 ctx1")
	}
}

func TestContextEngine_CreateContext_已有上下文返回缓存(t *testing.T) {
	config := schema.NewContextEngineConfig()
	ce := NewContextEngine(config).(*contextEngine)

	mc := &mockModelContext{sessionID: "s1", contextID: "ctx1"}
	ce.contextPool["s1_ctx1"] = mc

	sess := session.NewSession(session.WithSessionID("s1"))
	result, err := ce.CreateContext(context.Background(), "ctx1", sess)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if result != mc {
		t.Fatal("期望返回已缓存的上下文")
	}
}

func TestLoadStateFromSession_session为nil(t *testing.T) {
	mc := &mockModelContext{sessionID: "s1", contextID: "ctx1"}
	// 不应 panic
	loadStateFromSession(mc, nil, nil)
}

func TestSaveStateToSession_session为nil(t *testing.T) {
	// 不应 panic
	saveStateToSession(nil, map[string]any{"ctx1": map[string]any{}})
}
