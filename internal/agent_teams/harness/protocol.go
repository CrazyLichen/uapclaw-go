package harness

import (
	"context"
	"errors"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HarnessProtocol 并发安全的多轮交互契约。
// Python: HarnessProtocol (agent_teams/harness/protocol.py，10 方法 + 2 属性)。
// 契约（写进接口而非实现）：
//  1. 所有方法可从任意 goroutine 调用，调用方无需外部加锁；
//  2. 外部只"入队并等应答"，状态只由 supervisor 单写者修改；
//  3. 订阅回调在 supervisor 协程内同步执行，必须便宜且不可阻塞。
type HarnessProtocol interface {
	// State 返回当前生命周期相位
	State() HarnessState
	// SessionID 返回拥有的会话标识（start 前为空串）
	SessionID() string
	// Start 初始化并启动 supervisor
	Start(ctx context.Context, sess *session.Session) error
	// Stop 停止 supervisor、取消在飞轮次、关闭输出
	Stop(ctx context.Context) error
	// Outputs 返回输出迭代器（单消费者）
	Outputs() *OutputIterator
	// Send 投递输入；immediate=true 转向当前轮，false 排队等本轮结束
	Send(ctx context.Context, content any, immediate bool) error
	// Abort 中断；immediate=true 立即取消调度任务，false 让本轮自然收尾
	Abort(ctx context.Context, immediate bool) error
	// Pause 暂停到可恢复边界（25.1c 前返回 ErrPauseUnsupported）
	Pause(ctx context.Context) error
	// Resume 从暂停处继续（25.1c 前返回 ErrPauseUnsupported）
	Resume(ctx context.Context, query any) error
	// Subscribe 订阅生命周期事件
	Subscribe(ctx context.Context, onState StateHandler, onRound RoundHandler) error
}

// HarnessStateEvent 相位变化事件。
// Python: harness.state 事件载荷（kwargs old/new/session_id）
type HarnessStateEvent struct {
	// Old 变化前相位
	Old HarnessState
	// New 变化后相位
	New HarnessState
	// SessionID 所属会话
	SessionID string
}

// HarnessRoundEvent 轮次变化事件。
// Python: harness.round 事件载荷（kwargs kind/round_id/result）
type HarnessRoundEvent struct {
	// Kind 取值 started/finished/aborted/paused/failed
	Kind string
	// RoundID 轮次序号
	RoundID int
	// Result 本轮结果（可为 nil）
	Result map[string]any
}

// StateHandler 相位事件回调。
type StateHandler func(ctx context.Context, ev HarnessStateEvent)

// RoundHandler 轮次事件回调。
type RoundHandler func(ctx context.Context, ev HarnessRoundEvent)

// eventBus 生命周期事件总线。
// Python: self._events = AsyncCallbackFramework(enable_metrics=False, enable_logging=False)
// Go 复用 agentcore/runner/callback.CallbackFramework 的自定义事件通道
// （OnCustom/TriggerCustom），事件名用 "harness.state"/"harness.round"，
// 命名空间 native_harness。上游的 metrics/logging 噪声开关在 Go 框架中不存在。
type eventBus struct {
	// fw 底层回调框架
	fw *callback.CallbackFramework
}

// ──────────────────────────── 枚举 ────────────────────────────

// 事件名常量。
const (
	// eventNameState 相位事件名
	eventNameState = "harness.state"
	// eventNameRound 轮次事件名
	eventNameRound = "harness.round"
	// eventNamespace 事件命名空间
	eventNamespace = "native_harness"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrPauseUnsupported 暂停/继续尚未实现（⤵️ 25.1c 换真实现）。
var ErrPauseUnsupported = errors.New("harness: pause/resume 尚未实现（25.1c）")

// ErrTemplateUnsupported AgentTemplateSpec 热加载尚未实现。
//
// ⤵️ 15.7-15.9：AgentTemplateSpec 依赖链（manifest 加载 / extension_binder /
// LoadRecord）体量约 2-3 倍，属模板与技能域。本版按 25.1a 的
// ErrPauseUnsupported 同款手法：接口面完整、调用链对齐、行为待补。
//
// Python: NativeHarness._prepare 里的 AgentTemplateSpec.model_validate +
// load_agent_template_spec (native_harness.py:293-300)
var ErrTemplateUnsupported = errors.New("harness: agent template hot-load is not supported yet")

// ──────────────────────────── 非导出函数 ────────────────────────────

// newEventBus 创建事件总线。
func newEventBus() *eventBus {
	return &eventBus{fw: callback.NewCallbackFramework()}
}

// subscribe 注册事件回调；回调在触发方协程内同步执行。
func (b *eventBus) subscribe(_ context.Context, onState StateHandler, onRound RoundHandler) error {
	if onState != nil {
		b.fw.OnCustom(eventNameState, func(ctx context.Context, data map[string]any) any {
			onState(ctx, HarnessStateEvent{
				Old:       toHarnessState(data["old"]),
				New:       toHarnessState(data["new"]),
				SessionID: toString(data["session_id"]),
			})
			return nil
		}, callback.WithNamespace(eventNamespace))
	}
	if onRound != nil {
		b.fw.OnCustom(eventNameRound, func(ctx context.Context, data map[string]any) any {
			onRound(ctx, HarnessRoundEvent{
				Kind:    toString(data["kind"]),
				RoundID: toInt(data["round_id"]),
				Result:  toMap(data["result"]),
			})
			return nil
		}, callback.WithNamespace(eventNamespace))
	}
	return nil
}

// emitState 触发相位事件。
func (b *eventBus) emitState(ctx context.Context, ev HarnessStateEvent) {
	b.fw.TriggerCustom(ctx, eventNameState, map[string]any{
		"old":        string(ev.Old),
		"new":        string(ev.New),
		"session_id": ev.SessionID,
	})
}

// emitRound 触发轮次事件。
func (b *eventBus) emitRound(ctx context.Context, ev HarnessRoundEvent) {
	b.fw.TriggerCustom(ctx, eventNameRound, map[string]any{
		"kind":     ev.Kind,
		"round_id": ev.RoundID,
		"result":   ev.Result,
	})
}

// toHarnessState 载荷转相位。
func toHarnessState(v any) HarnessState {
	if s, ok := v.(string); ok {
		return HarnessState(s)
	}
	if st, ok := v.(HarnessState); ok {
		return st
	}
	return HarnessStateIdle
}

// toString 载荷转字符串。
func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// toInt 载荷转整数。
func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// toMap 载荷转 map。
func toMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}
