package harnessobservability

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"

	obs "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/observability"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	sessionstate "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
)

// rail_test.go AgentObservabilityRail 生命周期单测。
// Python 对照: tests/unit_tests/harness/observability/（rail.py 行为）。
// 层级断言目标（Task 5 集成口径的单元版）：
//
//	root → agent.X.task_iteration.1 → execute_tool.xxx
//	iteration 收口 / 孤儿清理 / 无根跳过。

// ──────────────────────────── 测试基建 ────────────────────────────

// newRailTestEnv 建带 tracker 的 provider 与 rail（注入 tracer）。
func newRailTestEnv(t *testing.T) *AgentObservabilityRail {
	t.Helper()
	tracker := obs.NewActiveSpanTracker()
	obs.SetActiveSpanTracker(tracker)
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(sdkresource.NewWithAttributes(
			semconv.SchemaURL, semconv.ServiceNameKey.String("rail-test"))),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(tracker),
	)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		obs.SetActiveSpanTracker(nil)
	})
	return NewAgentObservabilityRail(provider.Tracer(railTracerNameRail))
}

// openTestRunRoot 开测试运行根并注册（宿主 OpenAgentRunSpan 职责的模拟）。
func openTestRunRoot(t *testing.T, sessionID string) (trace.Span, func()) {
	t.Helper()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	_, root := tp.Tracer("root-test").Start(
		context.Background(), fmt.Sprintf("agent.run.%s", sessionID),
		trace.WithAttributes(
			attribute.Bool(obs.OJTraceRoot, true),
			attribute.String(obs.GenAIConversationID, sessionID),
		),
	)
	obs.SetRootSpan(root, sessionID)
	return root, func() {
		root.End()
		obs.ClearRootSpan(sessionID, root)
		_ = tp.Shutdown(context.Background())
	}
}

// fakeAgent 最小 BaseAgent 桩（嵌入 nil 接口，仅覆盖 Card）。
type fakeAgent struct {
	agentinterfaces.BaseAgent
	name string
}

func (f *fakeAgent) Card() *agentschema.AgentCard {
	return agentschema.NewAgentCard(agentschema.WithAgentName(f.name))
}

// AbilityManager 覆盖内嵌 nil 接口（否则空指针 panic 被 recoverRail 吞掉，
// 测试表现为静默跳过）。
func (f *fakeAgent) AbilityManager() agentinterfaces.AbilityManagerInterface {
	return nil
}

// fakeSession 最小 SessionFacade 桩（嵌入 nil 接口，仅覆盖用到的三个方法）。
type fakeSession struct {
	sessioninterfaces.SessionFacade
	id string
}

func (s *fakeSession) GetSessionID() string                        { return s.id }
func (s *fakeSession) UpdateState(map[string]any)                  {}
func (s *fakeSession) GetState(sessionstate.StateKey) (any, error) { return nil, nil }

// spanName / spanAttr* ReadOnlySpan 属性读取助手。
func spanName(t *testing.T, span trace.Span) string {
	t.Helper()
	ro, ok := span.(sdktrace.ReadOnlySpan)
	require.True(t, ok)
	return ro.Name()
}

func spanAttrString(span trace.Span, key string) string {
	ro, ok := span.(sdktrace.ReadOnlySpan)
	if !ok {
		return ""
	}
	for _, kv := range ro.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.AsString()
		}
	}
	return ""
}

func spanAttrBool(span trace.Span, key string) bool {
	ro, ok := span.(sdktrace.ReadOnlySpan)
	if !ok {
		return false
	}
	for _, kv := range ro.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.AsBool()
		}
	}
	return false
}

func spanAttrInt64(span trace.Span, key string) int64 {
	ro, ok := span.(sdktrace.ReadOnlySpan)
	if !ok {
		return 0
	}
	for _, kv := range ro.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.AsInt64()
		}
	}
	return 0
}

// spanParentID 返回父 span id（hex），无父为空。
func spanParentID(span trace.Span) string {
	ro, ok := span.(sdktrace.ReadOnlySpan)
	if !ok || !ro.Parent().IsValid() {
		return ""
	}
	return ro.Parent().SpanID().String()
}

// ──────────────────────────── 用例 ────────────────────────────

// TestRail_迭代span嵌套根下 before_task_iteration 开的 span 必须是运行根的子，
// 且带 agent 名/迭代号/操作名属性；after 收口。
// Python: before/after_task_iteration（rail.py:513-646）。
func TestRail_迭代span嵌套根下(t *testing.T) {
	rail := newRailTestEnv(t)
	root, closeRoot := openTestRunRoot(t, "sess-1")
	defer closeRoot()

	state := obs.InitSpanState()
	ctx := obs.WithSpanState(context.Background(), state)
	cbc := agentinterfaces.NewAgentCallbackContext(&fakeAgent{name: "writer"},
		&agentinterfaces.TaskIterationInputs{Iteration: 1, Query: "帮我写周报"}, &fakeSession{id: "sess-1"})

	require.NoError(t, rail.BeforeTaskIteration(ctx, cbc))
	scope := CurrentAgentSpanScope(cbc)
	require.NotNil(t, scope, "scope 应挂上 cbc")
	span := scope.Span
	assert.True(t, span.IsRecording())
	assert.Equal(t, "agent.writer.task_iteration.1", spanName(t, span))
	assert.Equal(t, root.SpanContext().SpanID().String(), spanParentID(span),
		"迭代 span 父应为运行根")
	assert.Equal(t, "invoke_agent", spanAttrString(span, obs.GenAIOperationName))
	assert.Equal(t, "writer", spanAttrString(span, obs.GenAIAgentName))
	assert.Equal(t, int64(1), spanAttrInt64(span, obs.DATaskIteration))
	// query 镜像（config 为 nil → 原样）
	assert.Equal(t, "帮我写周报", spanAttrString(span, obs.OJSpanInput))

	require.NoError(t, rail.AfterTaskIteration(ctx, cbc))
	assert.False(t, span.IsRecording(), "after_task_iteration 应收口迭代 span")
	assert.Nil(t, CurrentAgentSpanScope(cbc))
}

// TestRail_工具span嵌套迭代span下 before_tool_call 的工具 span 父 = 当前
// agent span；after 正常收口并带结果与 OK 状态。
// Python: before/after_tool_call（rail.py:931-1010）+ ToolSpanScope.close。
func TestRail_工具span嵌套迭代span下(t *testing.T) {
	rail := newRailTestEnv(t)
	_, closeRoot := openTestRunRoot(t, "sess-2")
	defer closeRoot()

	state := obs.InitSpanState()
	ctx := obs.WithSpanState(context.Background(), state)
	agent := &fakeAgent{name: "writer"}
	cbc := agentinterfaces.NewAgentCallbackContext(agent,
		&agentinterfaces.TaskIterationInputs{Iteration: 1}, &fakeSession{id: "sess-2"})
	require.NoError(t, rail.BeforeTaskIteration(ctx, cbc))
	iterationSpan := CurrentAgentSpanScope(cbc).Span

	toolCbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ToolCallInputs{
		ToolName: "search",
		ToolCall: &llmschema.ToolCall{ID: "call-1"},
		ToolArgs: map[string]any{"q": "go test"},
	}, nil)
	require.NoError(t, rail.BeforeToolCall(ctx, toolCbc))
	toolScope := DetachToolSpanScope(toolCbc)
	require.NotNil(t, toolScope)
	toolSpan := toolScope.Span
	assert.Equal(t, "execute_tool search", spanName(t, toolSpan))
	assert.True(t, spanAttrBool(toolSpan, obs.OJToolAuthoritative))
	assert.Equal(t, "call-1", spanAttrString(toolSpan, obs.GenAIToolCallID))
	assert.Equal(t, iterationSpan.SpanContext().SpanID().String(), spanParentID(toolSpan),
		"工具 span 父应为当前迭代 span")

	AttachToolSpanScope(toolCbc, toolScope)
	toolCbc.SetInputs(&agentinterfaces.ToolCallInputs{
		ToolName:   "search",
		ToolResult: map[string]any{"success": true, "data": "ok"},
	})
	require.NoError(t, rail.AfterToolCall(ctx, toolCbc))
	assert.False(t, toolSpan.IsRecording())
}

// TestRail_工具reportedFailure 记为 ERROR 状态而非 OK。
// Python: ToolSpanScope.close 的 tool_failure_reason 分支。
func TestRail_工具reportedFailure(t *testing.T) {
	rail := newRailTestEnv(t)
	_, closeRoot := openTestRunRoot(t, "sess-2b")
	defer closeRoot()

	state := obs.InitSpanState()
	ctx := obs.WithSpanState(context.Background(), state)
	agent := &fakeAgent{name: "writer"}
	cbc := agentinterfaces.NewAgentCallbackContext(agent,
		&agentinterfaces.TaskIterationInputs{Iteration: 1}, &fakeSession{id: "sess-2b"})
	require.NoError(t, rail.BeforeTaskIteration(ctx, cbc))

	toolCbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ToolCallInputs{
		ToolName: "bash",
	}, nil)
	require.NoError(t, rail.BeforeToolCall(ctx, toolCbc))
	toolScope := DetachToolSpanScope(toolCbc)

	toolCbc.SetInputs(&agentinterfaces.ToolCallInputs{
		ToolName:   "bash",
		ToolResult: map[string]any{"success": false, "error": "exit code 1"},
	})
	AttachToolSpanScope(toolCbc, toolScope)
	require.NoError(t, rail.AfterToolCall(ctx, toolCbc))
	ro, ok := toolScope.Span.(sdktrace.ReadOnlySpan)
	require.True(t, ok)
	assert.Equal(t, codes.Error, ro.Status().Code, "success=false 应记 ERROR")
}

// TestRail_孤儿agentspan被清理 同 agent 同迹线的残留 span 在下一迭代前被收口。
// Python: _drain_or_clear_stale（rail.py:1066-1120）。
func TestRail_孤儿agentspan被清理(t *testing.T) {
	rail := newRailTestEnv(t)
	_, closeRoot := openTestRunRoot(t, "sess-3")
	defer closeRoot()

	state := obs.InitSpanState()
	ctx := obs.WithSpanState(context.Background(), state)
	agent := &fakeAgent{name: "writer"}
	cbc := agentinterfaces.NewAgentCallbackContext(agent,
		&agentinterfaces.TaskIterationInputs{Iteration: 1}, &fakeSession{id: "sess-3"})

	// 第一迭代：开但不收口（模拟上一轮崩溃残留）
	require.NoError(t, rail.BeforeTaskIteration(ctx, cbc))
	orphan := CurrentAgentSpanScope(cbc).Span
	require.True(t, orphan.IsRecording())
	DetachAgentSpanScope(cbc) // 手工摘 scope 但不收口（after 未跑）

	// 第二迭代：孤儿被强制收口
	require.NoError(t, rail.BeforeTaskIteration(ctx, cbc))
	assert.False(t, orphan.IsRecording(), "同 agent 同迹线的孤儿应被强制收口")
	assert.True(t, CurrentAgentSpanScope(cbc).Span.IsRecording(), "新 span 正常在位")
}

// TestRail_无运行根时跳过 没有宿主根 span 时不造孤儿根。
// Python: before_task_iteration 的 root_span None 分支。
func TestRail_无运行根时跳过(t *testing.T) {
	rail := newRailTestEnv(t)
	cbc := agentinterfaces.NewAgentCallbackContext(&fakeAgent{name: "solo"},
		&agentinterfaces.TaskIterationInputs{Iteration: 1}, nil)
	require.NoError(t, rail.BeforeTaskIteration(context.Background(), cbc))
	assert.Nil(t, CurrentAgentSpanScope(cbc), "无根时应跳过不挂 scope")
}

// TestMaybeAgentObservabilityRail_未初始化返回nil 观测关闭时守卫返回 nil。
// Python: maybe_agent_observability_rail（rail.py:1237-1250）。
func TestMaybeAgentObservabilityRail_未初始化返回nil(t *testing.T) {
	assert.Nil(t, MaybeAgentObservabilityRail())
}
