//go:build integration

package externalcli_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/external"
	harness_protocol "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness_protocol"
	harnessproviders "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness_providers"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemberEventLoopSuite 15.16a 成员事件循环集成套件（黑盒）。
//
// 跨子系统：externalcli（ExternalHarnessMemberRuntime 事件泵）→
// harness_providers（HarnessIOAdapter pumpEvents 经 EventObserver 缝投递）→
// harness_protocol（事件信封/游标契约）→ schema（可靠性词汇，单测覆盖；
// reliability ctx 注入 ⤴️15.7/15.8，集成面收敛到可导出观测点）。
// 假 provider 经协议 HarnessProtocol 接口直接承担（Adapter 的真实消费循环），
// 绝不打真实 CLI 子进程。
type MemberEventLoopSuite struct {
	isuite.AgentSuite
}

// TestMemberEventLoopSuite 套件入口（审计硬要求：suite.Run 字面调用）。
func TestMemberEventLoopSuite(t *testing.T) {
	suite.Run(t, new(MemberEventLoopSuite))
}

// ──────────────────────────── 结构体（假 provider） ────────────────────────────

// scriptedHarnessProtocol 按脚本吐信封的假 provider。
type scriptedHarnessProtocol struct {
	events []harness_protocol.HarnessEvent
}

func (h *scriptedHarnessProtocol) Card() harness_protocol.HarnessCard {
	card, _ := harness_protocol.NewHarnessCard("fake-scripted", "0.1.0")
	return card
}

func (h *scriptedHarnessProtocol) State() harness_protocol.HarnessState {
	return harness_protocol.HarnessStateIdle
}

func (h *scriptedHarnessProtocol) ProviderSessionID() *string { return nil }

func (h *scriptedHarnessProtocol) EventBufferConfig() harness_protocol.EventBufferConfig {
	return harness_protocol.EventBufferConfig{}
}

func (h *scriptedHarnessProtocol) Start(_ context.Context, _ harness_protocol.HarnessContext) error {
	return nil
}

func (h *scriptedHarnessProtocol) Stop(_ context.Context) error { return nil }

func (h *scriptedHarnessProtocol) Events() harness_protocol.HarnessEventCursor {
	return &scriptedCursor{events: h.events}
}

func (h *scriptedHarnessProtocol) TurnEvents(_ *string) harness_protocol.HarnessEventCursor {
	return &scriptedCursor{}
}

func (h *scriptedHarnessProtocol) Send(_ context.Context, _ harness_protocol.HarnessInput,
	_ harness_protocol.DeliveryMode) (harness_protocol.SendReceipt, error) {
	return harness_protocol.SendReceipt{}, nil
}

func (h *scriptedHarnessProtocol) Abort(_ context.Context, _ harness_protocol.AbortMode) error {
	return nil
}

func (h *scriptedHarnessProtocol) Pause(_ context.Context) error { return nil }

func (h *scriptedHarnessProtocol) Resume(_ context.Context, _ *harness_protocol.HarnessInput) error {
	return nil
}

func (h *scriptedHarnessProtocol) ExportCheckpoint(_ context.Context) (*harness_protocol.HarnessCheckpoint, error) {
	return nil, nil
}

// scriptedCursor 预置序列的游标（耗尽返 ErrEventStreamDone）。
type scriptedCursor struct {
	events []harness_protocol.HarnessEvent
	idx    int
}

func (c *scriptedCursor) Iter() harness_protocol.HarnessEventCursor { return c }

func (c *scriptedCursor) Next(_ context.Context) (*harness_protocol.HarnessEvent, error) {
	if c.idx >= len(c.events) {
		return nil, harness_protocol.ErrEventStreamDone
	}
	ev := c.events[c.idx]
	c.idx++
	return &ev, nil
}

func (c *scriptedCursor) Close(_ context.Context) error { return nil }

// ──────────────────────────── 测试 ────────────────────────────

// TestEventLoop_全周期事件投影 假 provider 吐一整轮信封流，经 adapter 真实
// 消费循环断言：① tool_call/tool_result 对按 item_id 配对到达 activitySink
// （收窄后类型）；② round 回调收到轮次序与信封轮次 id；③ state 回调收到相位。
// 可靠性 finalize/retry 路径与轨迹记账由单测覆盖（ctx 注入 ⤴️15.7/15.8）。
//
// 对齐 spec §4.2 用例①②；Python: _on_event (member_runtime.py:676) +
// io_adapter.py:452-470 _pump_events 观察者先行投影。
func (s *MemberEventLoopSuite) TestEventLoop_全周期事件投影() {
	freeze := func(m map[string]any) harness_protocol.JsonObject {
		obj, err := harness_protocol.FreezeJsonObject(m)
		s.Require().NoError(err)
		return obj
	}
	itemDataStarted := freeze(map[string]any{"tool_name": "bash", "arguments": map[string]any{"cmd": "ls"}})
	itemDataDone := freeze(map[string]any{"tool_name": "bash", "result": "out"})

	scripted := &scriptedHarnessProtocol{events: []harness_protocol.HarnessEvent{
		{Event: harness_protocol.TurnLifecycleEvent{Kind: harness_protocol.TurnEventKindStarted}, TurnID: "t-1"},
		{Event: harness_protocol.ItemLifecycleEvent{Kind: harness_protocol.ItemEventKindStarted, ItemType: "tool", Data: itemDataStarted}, TurnID: "t-1", ItemID: "i-1"},
		{Event: harness_protocol.ItemLifecycleEvent{Kind: harness_protocol.ItemEventKindCompleted, ItemType: "tool", Data: itemDataDone}, TurnID: "t-1", ItemID: "i-1"},
		{Event: harness_protocol.StateChangedEvent{Old: harness_protocol.HarnessStateRunning, New: harness_protocol.HarnessStateIdle}, TurnID: "t-1"},
		{Event: harness_protocol.TurnLifecycleEvent{Kind: harness_protocol.TurnEventKindFinished}, TurnID: "t-1"},
	}}

	r, err := external.NewExternalHarnessMemberRuntime(scripted,
		func(_ any) (*harness_protocol.HarnessContext, error) {
			return &harness_protocol.HarnessContext{AgentID: "m-1", AgentName: "member"}, nil
		},
		external.ExternalHarnessMemberRuntimeOptions{AgentKind: "codex"})
	s.Require().NoError(err)

	var activities []*external.AgentActivityEvent
	var rounds []string
	var states []string
	r.BindActivitySink(func(ev *external.AgentActivityEvent) { activities = append(activities, ev) },
		"m-1", "exec", "label", "corr-1")
	r.Subscribe(
		func(_, new harness_protocol.HarnessState, _ string) {
			states = append(states, string(new))
		},
		func(kind string, _ int, turnID string, _ *harness_protocol.TurnResult) {
			rounds = append(rounds, kind+"@"+turnID)
		},
	)

	s.Require().NoError(r.Start(nil))

	// 泵是异步 goroutine：轮询等 round 回调收齐（finished 为末事件）
	s.Require().Eventually(func() bool {
		return len(rounds) >= 2
	}, 5*time.Second, 20*time.Millisecond)
	r.Stop()

	// ① activitySink：tool_call/tool_result 对，身份四元组随事件
	s.Require().Len(activities, 2)
	s.Equal("tool_call", activities[0].ActivityType)
	s.Equal("i-1", activities[0].ToolCallID)
	s.Equal("bash", *activities[0].ToolName)
	s.Equal("tool_result", activities[1].ActivityType)
	s.Require().NotNil(activities[1].Content)
	s.Equal("out", *activities[1].Content)
	s.Equal("m-1", activities[0].AgentID)
	s.Equal("corr-1", activities[0].CorrelationID)

	// ② round 回调：started/finished 各一次，带信封轮次 id
	s.Require().Len(rounds, 2)
	s.Equal("started@t-1", rounds[0])
	s.Equal("finished@t-1", rounds[1])
	s.Require().Len(states, 1)
	s.Equal(string(harness_protocol.HarnessStateIdle), states[0])
}

// TestEventLoop_轮次失败回调 假 provider 吐 FAILED 终态，断言 round 回调
// 收到 failed 与终态结果（D-15.16-a8 加宽的 result 形参在此首次到达消费者）。
// finalize 进可靠性的断言由单测覆盖。
//
// Python: _on_turn_event (member_runtime.py:714) FAILED 分支。
func (s *MemberEventLoopSuite) TestEventLoop_轮次失败回调() {
	scripted := &scriptedHarnessProtocol{events: []harness_protocol.HarnessEvent{
		{Event: harness_protocol.TurnLifecycleEvent{Kind: harness_protocol.TurnEventKindStarted}, TurnID: "t-2"},
		{Event: harness_protocol.TurnLifecycleEvent{
			Kind:   harness_protocol.TurnEventKindFailed,
			Result: &harness_protocol.TurnResult{Error: &harness_protocol.TurnError{Message: "boom"}},
		}, TurnID: "t-2"},
	}}
	r, err := external.NewExternalHarnessMemberRuntime(scripted,
		func(_ any) (*harness_protocol.HarnessContext, error) {
			return &harness_protocol.HarnessContext{}, nil
		},
		external.ExternalHarnessMemberRuntimeOptions{AgentKind: "claude"})
	s.Require().NoError(err)

	type roundRec struct {
		kind   string
		turnID string
		result *harness_protocol.TurnResult
	}
	var rounds []roundRec
	r.Subscribe(nil, func(kind string, _ int, turnID string, result *harness_protocol.TurnResult) {
		rounds = append(rounds, roundRec{kind, turnID, result})
	})

	s.Require().NoError(r.Start(nil))
	s.Require().Eventually(func() bool { return len(rounds) >= 2 }, 5*time.Second, 20*time.Millisecond)
	r.Stop()

	s.Require().Len(rounds, 2)
	s.Equal("failed", rounds[1].kind)
	s.Require().NotNil(rounds[1].result)
	s.Require().NotNil(rounds[1].result.Error)
	s.Equal("boom", rounds[1].result.Error.Message)
}

// TestEventLoop_启动失败上抛 adapter.Start 失败经 Start 返回（finalize 进
// 可靠性的断言由单测覆盖；实现类 ⤴️15.7/15.8）。
//
// Python: _start except 分支 (member_runtime.py:453-458)。
func (s *MemberEventLoopSuite) TestEventLoop_启动失败上抛() {
	boom := &failingStartHarness{}
	r, err := external.NewExternalHarnessMemberRuntime(boom,
		func(_ any) (*harness_protocol.HarnessContext, error) {
			return &harness_protocol.HarnessContext{}, nil
		},
		external.ExternalHarnessMemberRuntimeOptions{AgentKind: "codex"})
	s.Require().NoError(err)
	s.Error(r.Start(nil))
}

// failingStartHarness Start 恒失败的假 provider。
type failingStartHarness struct{ scriptedHarnessProtocol }

func (h *failingStartHarness) Start(_ context.Context, _ harness_protocol.HarnessContext) error {
	return &harnessproviders.ProviderStartupError{
		Msg: "spawn died",
		Err: harness_protocol.TurnError{Message: "no binary"},
	}
}

// 编译期锚：假 provider 满足协议接口。
var _ harness_protocol.HarnessProtocol = (*scriptedHarnessProtocol)(nil)
