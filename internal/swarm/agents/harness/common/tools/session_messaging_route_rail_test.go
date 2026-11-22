package tools

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	toolpkg "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
)

// fakeRouteService 服务面假实现（只覆盖 PendingContext）。
type fakeRouteService struct {
	pendingCalled bool
	pendingErr    error
}

func (s *fakeRouteService) ListTargets(context.Context, *SessionMessageSource, string, int, int) (map[string]any, error) {
	return nil, nil
}
func (s *fakeRouteService) SendMessage(context.Context, *SessionMessageSource, string, string, string) (map[string]any, error) {
	return nil, nil
}
func (s *fakeRouteService) ListMessages(context.Context, *SessionMessageSource, string, int, int) (map[string]any, error) {
	return nil, nil
}
func (s *fakeRouteService) ContinueQueued(context.Context, *SessionMessageSource, string) (map[string]any, error) {
	return nil, nil
}
func (s *fakeRouteService) ReadSession(context.Context, *SessionMessageSource, string, string, int, int) (map[string]any, error) {
	return nil, nil
}
func (s *fakeRouteService) ResolveUnknown(context.Context, *SessionMessageSource, string, string) (map[string]any, error) {
	return nil, nil
}
func (s *fakeRouteService) PendingContext(context.Context, *SessionMessageSource) (map[string]any, error) {
	s.pendingCalled = true
	if s.pendingErr != nil {
		return nil, s.pendingErr
	}
	return map[string]any{"queues": []any{}}, nil
}

// fakePromptBuilder 系统提示词构建器假实现。
type fakePromptBuilder struct {
	removed []string
	// added 记录 AddSection 的节（名称 → 优先级）
	added map[string]int
}

func newFakePromptBuilder() *fakePromptBuilder {
	return &fakePromptBuilder{added: map[string]int{}}
}

func (f *fakePromptBuilder) AddSection(s saprompt.PromptSection) *saprompt.SystemPromptBuilder {
	f.added[s.Name] = s.Priority
	return nil
}
func (f *fakePromptBuilder) RemoveSection(name string) *saprompt.SystemPromptBuilder {
	f.removed = append(f.removed, name)
	return nil
}
func (f *fakePromptBuilder) Language() string   { return "cn" }
func (f *fakePromptBuilder) SetLanguage(string) {}
func (f *fakePromptBuilder) GetSection(string) *saprompt.PromptSection {
	return nil
}
func (f *fakePromptBuilder) HasSection(string) bool { return false }

// fakeToolCallbackContext 最小回调上下文（Extra + Session）。
type fakeToolCallbackContext struct {
	extra map[string]any
}

func (f *fakeToolCallbackContext) Extra() map[string]any                    { return f.extra }
func (f *fakeToolCallbackContext) Session() sessioninterfaces.SessionFacade { return nil }

// newTestRail 构造带服务的 rail。
func newTestRail(service SessionMessagingServicePort) *SessionMessagingRouteRail {
	r := NewSessionMessagingRouteRail()
	r.SetService(service)
	return r
}

// newRouteCBC 构造带 extra 与工具输入的回调上下文。
func newRouteCBC(t *testing.T, toolName, toolCallID string, extra map[string]any) *agentinterfaces.AgentCallbackContext {
	t.Helper()
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	for k, v := range extra {
		cbc.SetExtra(k, v)
	}
	cbc.SetInputs(&agentinterfaces.ToolCallInputs{
		ToolName: toolName,
		ToolCall: &schema.ToolCall{ID: toolCallID, Name: toolName},
	})
	return cbc
}

// TestSessionMessagingRouteRail_BeforeToolCall_BindsRoute 工具名命中集合时绑定路由与调用标识。
// Python: session_messaging_toolkit.py:314-330 before_tool_call
func TestSessionMessagingRouteRail_BeforeToolCall_BindsRoute(t *testing.T) {
	r := newTestRail(&fakeRouteService{})
	routeWire := map[string]any{"session_id": "sess-A", "request_id": "req-1", "hop_count": 1}
	cbc := newRouteCBC(t, "session_send_message", "tc-1", map[string]any{
		sessionInputMessageRouteExtraKey: routeWire,
	})

	if err := r.BeforeToolCall(context.Background(), cbc); err != nil {
		t.Fatalf("BeforeToolCall 返回错误: %v", err)
	}
	raw, ok := cbc.GetExtra(sessionMessagingRouteTokenExtraKey)
	if !ok {
		t.Fatal("路由未写入调用级 extra")
	}
	wire, isMap := raw.(map[string]any)
	if !isMap || wire["session_id"] != "sess-A" {
		t.Fatalf("路由内容不符: %v", raw)
	}
	callID, ok := cbc.GetExtra(sessionMessagingToolCallIDExtraKey)
	if !ok || callID != "tc-1" {
		t.Fatalf("调用标识不符: %v", callID)
	}
}

// TestSessionMessagingRouteRail_BeforeToolCall_NonTargetToolSkipped 非集合内工具名不绑定。
func TestSessionMessagingRouteRail_BeforeToolCall_NonTargetToolSkipped(t *testing.T) {
	r := newTestRail(&fakeRouteService{})
	cbc := newRouteCBC(t, "bash", "tc-2", map[string]any{
		sessionInputMessageRouteExtraKey: map[string]any{"session_id": "sess-A", "request_id": "req-1"},
	})
	if err := r.BeforeToolCall(context.Background(), cbc); err != nil {
		t.Fatalf("BeforeToolCall 返回错误: %v", err)
	}
	if _, ok := cbc.GetExtra(sessionMessagingRouteTokenExtraKey); ok {
		t.Fatal("非目标工具不应绑定路由")
	}
}

// TestSessionMessagingRouteRail_BeforeToolCall_IncompleteRouteSkipped 路由缺 session/request 时不绑定。
// Python: :326-328 route is None or not route.session_id or not route.request_id
func TestSessionMessagingRouteRail_BeforeToolCall_IncompleteRouteSkipped(t *testing.T) {
	r := newTestRail(&fakeRouteService{})
	cbc := newRouteCBC(t, "session_send_message", "tc-3", map[string]any{
		sessionInputMessageRouteExtraKey: map[string]any{"session_id": "sess-A"},
	})
	if err := r.BeforeToolCall(context.Background(), cbc); err != nil {
		t.Fatalf("BeforeToolCall 返回错误: %v", err)
	}
	if _, ok := cbc.GetExtra(sessionMessagingRouteTokenExtraKey); ok {
		t.Fatal("不完整路由不应绑定")
	}
}

// TestSessionMessagingRouteRail_AfterToolCall_ResetsExtras after 复位调用级 extra。
// Python: :332-341 after_tool_call（token reset）
func TestSessionMessagingRouteRail_AfterToolCall_ResetsExtras(t *testing.T) {
	r := newTestRail(&fakeRouteService{})
	cbc := newRouteCBC(t, "session_send_message", "tc-4", map[string]any{
		sessionInputMessageRouteExtraKey: map[string]any{"session_id": "sess-A", "request_id": "req-1"},
	})
	_ = r.BeforeToolCall(context.Background(), cbc)
	if err := r.AfterToolCall(context.Background(), cbc); err != nil {
		t.Fatalf("AfterToolCall 返回错误: %v", err)
	}
	if _, ok := cbc.GetExtra(sessionMessagingRouteTokenExtraKey); ok {
		t.Fatal("路由 extra 未复位")
	}
	if _, ok := cbc.GetExtra(sessionMessagingToolCallIDExtraKey); ok {
		t.Fatal("调用标识 extra 未复位")
	}
}

// TestSessionMessagingRouteRail_RouteFromCBC_RunContextFallback run_context.extra 回退读取。
// Python: :291-309
func TestSessionMessagingRouteRail_RouteFromCBC_RunContextFallback(t *testing.T) {
	r := newTestRail(&fakeRouteService{})
	runCtx := map[string]any{
		"context": map[string]any{
			"extra": map[string]any{
				SessionMessagingRouteExtraKey: map[string]any{
					"session_id": "sess-B", "request_id": "req-2", "hop_count": 2,
				},
			},
		},
	}
	cbc := newRouteCBC(t, "session_read", "tc-5", map[string]any{"run_context": runCtx})
	if err := r.BeforeToolCall(context.Background(), cbc); err != nil {
		t.Fatalf("BeforeToolCall 返回错误: %v", err)
	}
	raw, ok := cbc.GetExtra(sessionMessagingRouteTokenExtraKey)
	if !ok {
		t.Fatal("run_context 回退路径未绑定路由")
	}
	wire := raw.(map[string]any)
	if wire["session_id"] != "sess-B" || wire["hop_count"] != 2 {
		t.Fatalf("回退路由内容不符: %v", wire)
	}
}

// TestSessionMessagingRouteRail_BeforeModelCall_InjectsPromptSection
// 模型调用前移除旧节并注入 session_tools 快照节。
// Python: :201-234 before_model_call
func TestSessionMessagingRouteRail_BeforeModelCall_InjectsPromptSection(t *testing.T) {
	svc := &fakeRouteService{}
	r := newTestRail(svc)
	builder := newFakePromptBuilder()
	r.promptBuilder = builder
	cbc := newRouteCBC(t, "session_read", "tc-6", map[string]any{
		sessionInputMessageRouteExtraKey: map[string]any{"session_id": "sess-A", "request_id": "req-1"},
	})
	if err := r.BeforeModelCall(context.Background(), cbc); err != nil {
		t.Fatalf("BeforeModelCall 返回错误: %v", err)
	}
	if !svc.pendingCalled {
		t.Fatal("pending_context 未被调用")
	}
	if _, injected := builder.added["session_tools"]; !injected {
		t.Fatalf("session_tools 节未注入: %v", builder.added)
	}
	if builder.added["session_tools"] != sessionMessagingToolsPriority {
		t.Fatalf("节优先级不符: %d", builder.added["session_tools"])
	}
	if len(builder.removed) == 0 || builder.removed[0] != "session_tools" {
		t.Fatalf("旧节未先移除: %v", builder.removed)
	}
}

// TestSessionMessagingRouteRail_BeforeModelCall_ServiceErrorFallback 服务异常时占位快照。
// Python: :210-212 except → state_unavailable
func TestSessionMessagingRouteRail_BeforeModelCall_ServiceErrorFallback(t *testing.T) {
	svc := &fakeRouteService{pendingErr: &SessionMessagingError{Code: "PERSISTENCE_UNAVAILABLE", Msg: "boom"}}
	r := newTestRail(svc)
	builder := newFakePromptBuilder()
	r.promptBuilder = builder
	cbc := newRouteCBC(t, "session_read", "tc-7", map[string]any{
		sessionInputMessageRouteExtraKey: map[string]any{"session_id": "sess-A", "request_id": "req-1"},
	})
	if err := r.BeforeModelCall(context.Background(), cbc); err != nil {
		t.Fatalf("BeforeModelCall 返回错误: %v", err)
	}
	if _, injected := builder.added["session_tools"]; !injected {
		t.Fatal("异常路径仍应注入占位节")
	}
}

// TestSessionMessagingInvokeTool_InjectsRouteIntoCtx 载体工具从 extra 注入 ctx。
func TestSessionMessagingInvokeTool_InjectsRouteIntoCtx(t *testing.T) {
	fnCalled := false
	tk := &sessionMessagingInvokeTool{
		card: toolpkg.NewToolCard("session_read", "test", nil, nil),
		fn: func(ctx context.Context, _ map[string]any) (map[string]any, error) {
			fnCalled = true
			got := CurrentSessionMessagingRoute(ctx)
			if got == nil || got.SessionID != "sess-A" {
				t.Fatalf("ctx 未携带路由: %v", got)
			}
			if CurrentSessionMessagingToolCallID(ctx) != "tc-8" {
				t.Fatalf("ctx 未携带调用标识")
			}
			return map[string]any{"ok": true}, nil
		},
	}
	if !tk.AcceptsToolCallbackContext() {
		t.Fatal("载体未声明 CallbackContextAcceptor")
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	cbc.SetExtra(sessionMessagingRouteTokenExtraKey, map[string]any{
		"session_id": "sess-A", "request_id": "req-1",
	})
	cbc.SetExtra(sessionMessagingToolCallIDExtraKey, "tc-8")
	if _, err := tk.Invoke(context.Background(), nil,
		toolpkg.WithToolCallbackContext(&fakeToolCallbackContext{extra: cbcExtraOf(cbc)})); err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
	if !fnCalled {
		t.Fatal("invokeFn 未被调用")
	}
}

// cbcExtraOf 读取回调上下文 extra（测试助手：cbc 无导出 extra 访问器时经 GetExtra 逐键取）。
func cbcExtraOf(cbc *agentinterfaces.AgentCallbackContext) map[string]any {
	out := map[string]any{}
	for _, key := range []string{
		sessionMessagingRouteTokenExtraKey,
		sessionMessagingToolCallIDExtraKey,
	} {
		if v, ok := cbc.GetExtra(key); ok {
			out[key] = v
		}
	}
	return out
}
