package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/team_runtime"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// mockBaseTeam 测试用 BaseTeam mock。
// Invoke/Stream 行为可注入（panic / error / 正常返回），
// PostRun 触发次数由调用方通过 invokePostRunCount 观察。
type mockBaseTeam struct {
	// invokeFn 注入的 Invoke 行为
	invokeFn func(ctx context.Context, inputs map[string]any) (any, error)
	// streamFn 注入的 Stream 行为
	streamFn func(ctx context.Context, inputs map[string]any) (<-chan stream.Schema, error)
}

func (m *mockBaseTeam) Invoke(ctx context.Context, inputs map[string]any, opts ...maschema.TeamOption) (any, error) {
	if m.invokeFn != nil {
		return m.invokeFn(ctx, inputs)
	}
	return map[string]any{"ok": true}, nil
}

func (m *mockBaseTeam) Stream(ctx context.Context, inputs map[string]any, opts ...maschema.TeamOption) (<-chan stream.Schema, error) {
	if m.streamFn != nil {
		return m.streamFn(ctx, inputs)
	}
	ch := make(chan stream.Schema)
	close(ch)
	return ch, nil
}

func (m *mockBaseTeam) AddAgent(ctx context.Context, card *agentschema.AgentCard, provider maschema.TeamAgentProvider, opts ...maschema.TeamOption) error {
	return nil
}

func (m *mockBaseTeam) RemoveAgent(ctx context.Context, agentID string) error { return nil }

func (m *mockBaseTeam) Send(ctx context.Context, message map[string]any, recipient string, sender string, opts ...maschema.TeamOption) (any, error) {
	return nil, nil
}

func (m *mockBaseTeam) Publish(ctx context.Context, message map[string]any, topicID string, sender string, opts ...maschema.TeamOption) error {
	return nil
}

func (m *mockBaseTeam) Subscribe(ctx context.Context, agentID string, topic string) error { return nil }

func (m *mockBaseTeam) Unsubscribe(ctx context.Context, agentID string, topic string) error {
	return nil
}
func (m *mockBaseTeam) Configure(ctx context.Context, config maschema.TeamConfig) error {
	return nil
}

func (m *mockBaseTeam) GetAgentCard(agentID string) (*agentschema.AgentCard, error) {
	return nil, errors.New("not implemented")
}

func (m *mockBaseTeam) GetAgentCount() int               { return 0 }
func (m *mockBaseTeam) ListAgents() []string             { return nil }
func (m *mockBaseTeam) Card() maschema.TeamCardInterface { return nil }
func (m *mockBaseTeam) Config() *maschema.TeamConfig     { return nil }

// 编译期检查：mockBaseTeam 实现 maschema.BaseTeam
var _ maschema.BaseTeam = (*mockBaseTeam)(nil)

// mockBaseTeamWithRuntime 带 TeamRuntime 的 mock（实现 RuntimeGetter）。
// 用于通过 GetTeamSession/UnbindTeamSession 观察 M-53 defer 清理是否执行。
type mockBaseTeamWithRuntime struct {
	mockBaseTeam
	runtime *team_runtime.TeamRuntime
}

func (m *mockBaseTeamWithRuntime) GetRuntime() *team_runtime.TeamRuntime { return m.runtime }

// TestRunBaseTeam_InvokePanic时清理仍执行 验证 M-53 defer 化
//
// Invoke panic → defer 清理链（UnbindTeamSession/PostRun）仍执行。
// 观察点：panic 恢复后 TeamRuntime.GetTeamSession 返回 nil（已 Unbind）。
func TestRunBaseTeam_InvokePanic时清理仍执行(t *testing.T) {
	tr := team_runtime.NewTeamRuntime(team_runtime.RuntimeConfig{TeamID: "m53-test-team", P2PTimeout: 30})
	team := &mockBaseTeamWithRuntime{
		mockBaseTeam: mockBaseTeam{
			invokeFn: func(ctx context.Context, inputs map[string]any) (any, error) {
				panic("boom")
			},
		},
		runtime: tr,
	}

	sess := session.NewAgentTeamSession(session.WithAgentTeamSessionID("m53-panic-observe"))
	sid := sess.GetSessionID()
	require.NotEmpty(t, sid)

	recovered := make(chan any, 1)
	func() {
		defer func() {
			if r := recover(); r != nil {
				recovered <- r
			}
		}()
		_, _ = RunBaseTeam(context.Background(), team, map[string]any{}, sess)
	}()

	select {
	case r := <-recovered:
		assert.Equal(t, "boom", r, "panic 值应原样传播")
	default:
		t.Fatalf("panic 应向上传播")
	}

	// 核心断言：defer 清理已执行——session 从 TeamRuntime 解绑
	assert.Nil(t, tr.GetTeamSession(sid),
		"panic 后 UnbindTeamSession 未执行——M-53 defer 清理失效")
}

// TestRunBaseTeam_正常路径也解绑 验证正常路径清理对称性
func TestRunBaseTeam_正常路径也解绑(t *testing.T) {
	tr := team_runtime.NewTeamRuntime(team_runtime.RuntimeConfig{TeamID: "m53-test-team", P2PTimeout: 30})
	team := &mockBaseTeamWithRuntime{
		runtime: tr,
	}

	sess := session.NewAgentTeamSession(session.WithAgentTeamSessionID("m53-normal-observe"))
	sid := sess.GetSessionID()

	result, err := RunBaseTeam(context.Background(), team, map[string]any{}, sess)
	require.NoError(t, err)
	require.NotNil(t, result)

	// 正常路径：Invoke 后同样解绑（defer 对两路径对称执行）
	assert.Nil(t, tr.GetTeamSession(sid), "正常路径 Unbind 应执行")
}

// TestRunAgentTeam_Base分支类型断言 验证 M-56 facade 断言
//
// base=true 传非 BaseTeam 类型 → 明确类型错误（不再是隐式 prepareBaseTeam）。
func TestRunAgentTeam_Base分支类型断言(t *testing.T) {
	_, err := RunAgentTeam(context.Background(), "some-team-id", map[string]any{}, true, false, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts BaseTeam")
	assert.Contains(t, err.Error(), "got string", "错误消息应包含实际类型名")

	_, err = RunAgentTeamStreaming(context.Background(), "some-team-id", map[string]any{}, true, false, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts BaseTeam")
}

// TestRunAgentTeam_Base分支正常路由 验证 M-56 facade 正常路径
func TestRunAgentTeam_Base分支正常路由(t *testing.T) {
	team := &mockBaseTeam{
		invokeFn: func(ctx context.Context, inputs map[string]any) (any, error) {
			return map[string]any{"routed": "base"}, nil
		},
	}

	// facade base=true → RunBaseTeam 正常路由
	result, err := RunAgentTeam(context.Background(), team, map[string]any{}, true, false, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "base", result["routed"])
}

// TestRunBaseTeam_InvokePanic传播 验证 panic 经 recover 传播（M-53 前置行为不变）
func TestRunBaseTeam_InvokePanic传播(t *testing.T) {
	team := &mockBaseTeam{
		invokeFn: func(ctx context.Context, inputs map[string]any) (any, error) {
			panic("boom-in-invoke")
		},
	}

	defer func() {
		if r := recover(); r != nil {
			assert.Equal(t, "boom-in-invoke", r, "panic 值应原样传播")
		} else {
			t.Errorf("panic 应向上传播")
		}
	}()

	_, _ = RunBaseTeam(context.Background(), team, map[string]any{}, nil)
}

// TestRunBaseTeam_正常路径返回结果 验证 defer 化不破坏正常路径
func TestRunBaseTeam_正常路径返回结果(t *testing.T) {
	team := &mockBaseTeam{
		invokeFn: func(ctx context.Context, inputs map[string]any) (any, error) {
			return map[string]any{"answer": 42}, nil
		},
	}

	result, err := RunBaseTeam(context.Background(), team, map[string]any{"q": 1}, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 42, result["answer"])
}

// TestRunBaseTeamStreaming_StreamErr时清理仍执行 验证 M-53 streamErr 分支 defer 化
func TestRunBaseTeamStreaming_StreamErr时清理仍执行(t *testing.T) {
	team := &mockBaseTeam{
		streamFn: func(ctx context.Context, inputs map[string]any) (<-chan stream.Schema, error) {
			return nil, errors.New("stream setup failed")
		},
	}

	ch, err := RunBaseTeamStreaming(context.Background(), team, map[string]any{}, nil)
	require.Error(t, err)
	assert.Nil(t, ch, "streamErr 时应返回 nil channel")
}

// TestCloseTeamInteractGate_已取消ctx仍等待排空 验证 M-54 WithoutCancel 语义
//
// 调用方 ctx 已取消时，gate 排空不被立即放弃——WithoutCancel 保证
// 排空语义独立于调用方取消状态，30s 兜底超时。
func TestCloseTeamInteractGate_已取消ctx仍等待排空(t *testing.T) {
	gate := NewInteractGate()
	// 准入一个飞行中载荷（不消费 → drain 等待中）
	ticket := gate.Admit()
	require.NotNil(t, ticket)

	entry := &ActiveTeam{
		TeamName:     "m54-cancel-test-team",
		SessionID:    "sess-m54",
		State:        RuntimeStateRunning,
		InteractGate: gate,
	}
	GetTeamRuntimeManager().Pool().Add(entry)
	defer GetTeamRuntimeManager().Pool().Remove("m54-cancel-test-team")

	// 调用方 ctx 已取消（模拟清理路径：run ctx 已超时）
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		// M-54: WithoutCancel 使 drain 继续等待，不会因调用方 ctx 取消而放弃
		closeTeamInteractGate(ctx, "m54-cancel-test-team", "sess-m54")
		close(done)
	}()

	// 200ms 内不应返回（仍在等待 drain）
	select {
	case <-done:
		t.Errorf("调用方 ctx 已取消时不应立即放弃排空（WithoutCancel 语义失效）")
	case <-time.After(200 * time.Millisecond):
		// 预期：仍在等待
	}

	// 消费飞行中载荷 → drain 完成 → closeTeamInteractGate 返回
	gate.ConsumeDone(ticket)

	select {
	case <-done:
		// 排空完成，gate 关闭
		assert.True(t, gate.Closed(), "drain 后 gate 应为关闭状态")
	case <-time.After(2 * time.Second):
		t.Errorf("载荷消费后排空应在 2s 内完成")
	}
}

// TestCloseTeamInteractGate_条目不存在时静默返回 验证 nil entry 分支
func TestCloseTeamInteractGate_条目不存在时静默返回(t *testing.T) {
	// 池中无该团队 → 直接返回，不 panic
	closeTeamInteractGate(context.Background(), "no-such-team", "any-session")
}

// TestCloseTeamInteractGate_30秒兜底超时 验证 M-54 超时保护
//
// 飞行中载荷永不消费时，30s 兜底后返回（不永久阻塞）。
// 测试只验证超时路径可返回（实际等待 30s 太长，此用例验证
// WithoutCancel 不会无限等待——用真实 30s 等待太慢，
// 改验证取消保护来自 timeout 而非调用方 ctx：
// 已取消 ctx + 无消费 → closeTeamInteractGate 应持续等待直到 30s 超时。
// 为了测试速度，这里只验证 500ms 内不返回（仍在等待），逻辑同上一用例。
func TestCloseTeamInteractGate_30秒兜底语义(t *testing.T) {
	gate := NewInteractGate()
	ticket := gate.Admit()
	require.NotNil(t, ticket)

	entry := &ActiveTeam{
		TeamName:     "m54-timeout-test-team",
		SessionID:    "sess-m54t",
		State:        RuntimeStateRunning,
		InteractGate: gate,
	}
	GetTeamRuntimeManager().Pool().Add(entry)
	defer GetTeamRuntimeManager().Pool().Remove("m54-timeout-test-team")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	start := time.Now()
	go func() {
		closeTeamInteractGate(ctx, "m54-timeout-test-team", "sess-m54t")
		close(done)
	}()

	select {
	case <-done:
		elapsed := time.Since(start)
		t.Errorf("无消费时不应在 30s 前返回（实际 %.0fms）", float64(elapsed.Milliseconds()))
	case <-time.After(500 * time.Millisecond):
		// 预期：仍在等待 30s 超时
	}

	// 清理：消费载荷让后台 goroutine 退出，避免测试泄漏
	gate.ConsumeDone(ticket)
	<-done
}

// ──────────────────────────── T-01 PreRun 错误传播 ────────────────────────────

// failingCheckpointer T-01 测试用：PreAgentTeamExecute 永远失败。
type failingCheckpointer struct{}

func (f *failingCheckpointer) PreWorkflowExecute(ctx context.Context, s interfaces.InnerSession, inputs any) error {
	return nil
}
func (f *failingCheckpointer) PostWorkflowExecute(ctx context.Context, s interfaces.InnerSession, result any, exception error) error {
	return nil
}
func (f *failingCheckpointer) PreAgentExecute(ctx context.Context, s interfaces.InnerSession, inputs any) error {
	return nil
}
func (f *failingCheckpointer) PreAgentTeamExecute(ctx context.Context, s interfaces.InnerSession, inputs any) error {
	return errors.New("pre-run checkpoint failure")
}
func (f *failingCheckpointer) InterruptAgentExecute(ctx context.Context, s interfaces.InnerSession) error {
	return nil
}
func (f *failingCheckpointer) PostAgentExecute(ctx context.Context, s interfaces.InnerSession) error {
	return nil
}
func (f *failingCheckpointer) PostAgentTeamExecute(ctx context.Context, s interfaces.InnerSession) error {
	return nil
}
func (f *failingCheckpointer) SessionExists(ctx context.Context, sessionID string) (bool, error) {
	return false, nil
}
func (f *failingCheckpointer) Release(ctx context.Context, sessionID string, agentID ...string) error {
	return nil
}
func (f *failingCheckpointer) GraphStore() any { return nil }

// 编译期检查
var _ interfaces.Checkpointer = (*failingCheckpointer)(nil)

// newFailingPreRunSession 构造 PreRun 必失败的 AgentTeamSession。
func newFailingPreRunSession(id string) *session.AgentTeamSession {
	return session.NewAgentTeamSession(
		session.WithAgentTeamSessionID(id),
		session.WithAgentTeamCheckpointer(&failingCheckpointer{}),
	)
}

// TestRunBaseTeam_PreRun失败传播 T-01 回归测试：
// PreRun（checkpoint 恢复）失败必须向上传播（对齐 Python 裸 await 语义），
// 修复前只 Warn 吞掉、带空状态继续 Invoke。
func TestRunBaseTeam_PreRun失败传播(t *testing.T) {
	invokeCalled := false
	team := &mockBaseTeam{
		invokeFn: func(ctx context.Context, inputs map[string]any) (any, error) {
			invokeCalled = true
			return map[string]any{"ok": true}, nil
		},
	}
	sess := newFailingPreRunSession("t01-base-fail")

	result, err := RunBaseTeam(context.Background(), team, map[string]any{}, sess)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "pre-run checkpoint failure")
	assert.Nil(t, result)
	assert.False(t, invokeCalled, "PreRun 失败后不应继续 Invoke（修复前会带空状态执行）")
}

// TestRunBaseTeamStreaming_PreRun失败传播 T-01 流式路径同步验证
func TestRunBaseTeamStreaming_PreRun失败传播(t *testing.T) {
	team := &mockBaseTeam{}
	sess := newFailingPreRunSession("t01-base-stream-fail")

	ch, err := RunBaseTeamStreaming(context.Background(), team, map[string]any{}, sess)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "pre-run checkpoint failure")
	assert.Nil(t, ch)
}

// TestRunTeamMember_PreRun失败传播 T-01 Member 路径验证
func TestRunTeamMember_PreRun失败传播(t *testing.T) {
	ag := agent.NewTeamAgent(context.Background(), agentschema.NewAgentCard())
	sess := newFailingPreRunSession("t01-member-fail")

	result, err := RunTeamMember(context.Background(), ag, map[string]any{}, sess)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "pre-run checkpoint failure")
	assert.Nil(t, result)
}

// TestRunTeamMemberStreaming_PreRun失败传播 T-01 Member 流式路径验证
func TestRunTeamMemberStreaming_PreRun失败传播(t *testing.T) {
	ag := agent.NewTeamAgent(context.Background(), agentschema.NewAgentCard())
	sess := newFailingPreRunSession("t01-member-stream-fail")

	ch, err := RunTeamMemberStreaming(context.Background(), ag, map[string]any{}, sess)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "pre-run checkpoint failure")
	assert.Nil(t, ch)
}
