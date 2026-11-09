package harness

import (
	"context"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	agentcore "github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PhaseSnapshotRail 跟踪内层循环相位、捕获边界快照、驱动协作停止。
// Python: PhaseSnapshotRail (snapshot_rail.py L105-240)
//
// 与旧 SnapshotRail 的差别：旧实现经 ContextVar 读活动轮，而内层循环跑在
// TaskScheduler 的执行协程里，ContextVar 读不到；本轨直连 harness 反向引用。
type PhaseSnapshotRail struct {
	agentinterfaces.BaseRail
	// harness 反向引用（任意协程可见）
	harness *NativeHarness
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// cooperativeStopType 协作停止的 result_type。
	// Python: COOPERATIVE_STOP_TYPE = "cooperative_stop"（snapshot_rail.py L101）
	cooperativeStopType = "cooperative_stop"
	// reactContextID 内层 ReAct 循环实际写入的上下文标识。
	// 注意：不是 DeepAgent.CreateNewContextEngine 使用的 "default_context_id"。
	reactContextID = "default_context"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// cooperativeStopResult 内层循环在干净迭代边界强制收尾的载荷。
var cooperativeStopResult = map[string]any{"result_type": cooperativeStopType}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetCallbacks 声明本 rail 覆盖的事件。
func (r *PhaseSnapshotRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]callback.PerAgentCallbackFunc {
	return r.BuildCallbacks(
		r.CallbackFrom(agentinterfaces.CallbackBeforeModelCall, r.wrapBeforeModelCall),
		r.CallbackFrom(agentinterfaces.CallbackAfterModelCall, r.wrapAfterModelCall),
		r.CallbackFrom(agentinterfaces.CallbackBeforeToolCall, r.wrapBeforeToolCall),
		r.CallbackFrom(agentinterfaces.CallbackAfterReactIteration, r.wrapAfterReactIteration),
		r.CallbackFrom(agentinterfaces.CallbackAfterTaskIteration, r.wrapAfterTaskIteration),
	)
}

// BeforeModelCall 进入 MODEL 相位；武装后强制收尾。
// Python: before_model_call (snapshot_rail.py L144-160)
func (r *PhaseSnapshotRail) BeforeModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	active := r.harness.st.Active()
	if active == nil {
		return nil
	}
	active.SetPhase(RoundPhaseModel)
	active.ModelCallInFlight.Store(true)
	if active.PauseRequested.Load() || active.GracefulAbort.Load() {
		// before 钩子收尾会完全跳过模型体：最干净的边界。
		cbc.RequestForceFinish(cooperativeStopResult)
	}
	return nil
}

// AfterModelCall 离开 MODEL 相位；仅暂停在此收尾。
// Python: after_model_call (snapshot_rail.py L162-184)
func (r *PhaseSnapshotRail) AfterModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	active := r.harness.st.Active()
	if active == nil {
		return nil
	}
	active.ModelCallInFlight.Store(false)
	if active.PauseRequested.Load() {
		// 暂停语义：丢弃在飞迭代并回退到上一边界。优雅中止刻意不在此收尾——
		// 它要求已开始的迭代（含工具）跑完，在下一个模型调用边界退出。
		cbc.RequestForceFinish(cooperativeStopResult)
	}
	return nil
}

// BeforeToolCall 进入 TOOL 相位；永不强制收尾。
// Python: before_tool_call (snapshot_rail.py L186-196)
func (r *PhaseSnapshotRail) BeforeToolCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	active := r.harness.st.Active()
	if active == nil {
		return nil
	}
	active.SetPhase(RoundPhaseTool)
	active.ToolStarted.Store(true)
	return nil
}

// AfterReactIteration 迭代边界：复位相位并捕获 last_iter_snapshot。
// Python: after_react_iteration (snapshot_rail.py L198-223)
func (r *PhaseSnapshotRail) AfterReactIteration(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	active := r.harness.st.Active()
	if active == nil {
		return nil
	}
	active.SetPhase(RoundPhaseBoundary)
	active.ToolStarted.Store(false)
	sess := cbc.Session()
	if sess == nil {
		logger.Warn(logComponent).
			Str("event_type", "HARNESS_SNAPSHOT_NO_SESSION").
			Int("round_id", active.RoundID).
			Msg("no session on callback ctx; skipping iteration snapshot")
		return nil
	}
	active.LastIterSnapshot = captureSnapshot(r.harness.agent, sess, active.LastIterSnapshot, nil)
	return nil
}

// AfterTaskIteration 轮边界：捕获 last_safe_snapshot。
// Python: after_task_iteration (snapshot_rail.py L225-240)
func (r *PhaseSnapshotRail) AfterTaskIteration(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	active := r.harness.st.Active()
	if active == nil {
		return nil
	}
	sess := cbc.Session()
	if sess == nil {
		return nil
	}
	active.LastSafeSnapshot = captureSnapshot(r.harness.agent, sess, active.LastSafeSnapshot, nil)
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newPhaseSnapshotRail 创建快照轨。
func newPhaseSnapshotRail(h *NativeHarness) *PhaseSnapshotRail {
	return &PhaseSnapshotRail{
		BaseRail: *agentinterfaces.NewBaseRail().WithPriority(1000),
		harness:  h,
	}
}

// wrapBeforeModelCall 把回调载荷断言为 AgentCallbackContext 后转调。
func (r *PhaseSnapshotRail) wrapBeforeModelCall(ctx context.Context, railCtx any) error {
	if cbc, ok := railCtx.(*agentinterfaces.AgentCallbackContext); ok {
		return r.BeforeModelCall(ctx, cbc)
	}
	return nil
}

// wrapAfterModelCall 把回调载荷断言为 AgentCallbackContext 后转调。
func (r *PhaseSnapshotRail) wrapAfterModelCall(ctx context.Context, railCtx any) error {
	if cbc, ok := railCtx.(*agentinterfaces.AgentCallbackContext); ok {
		return r.AfterModelCall(ctx, cbc)
	}
	return nil
}

// wrapBeforeToolCall 把回调载荷断言为 AgentCallbackContext 后转调。
func (r *PhaseSnapshotRail) wrapBeforeToolCall(ctx context.Context, railCtx any) error {
	if cbc, ok := railCtx.(*agentinterfaces.AgentCallbackContext); ok {
		return r.BeforeToolCall(ctx, cbc)
	}
	return nil
}

// wrapAfterReactIteration 把回调载荷断言为 AgentCallbackContext 后转调。
func (r *PhaseSnapshotRail) wrapAfterReactIteration(ctx context.Context, railCtx any) error {
	if cbc, ok := railCtx.(*agentinterfaces.AgentCallbackContext); ok {
		return r.AfterReactIteration(ctx, cbc)
	}
	return nil
}

// wrapAfterTaskIteration 把回调载荷断言为 AgentCallbackContext 后转调。
func (r *PhaseSnapshotRail) wrapAfterTaskIteration(ctx context.Context, railCtx any) error {
	if cbc, ok := railCtx.(*agentinterfaces.AgentCallbackContext); ok {
		return r.AfterTaskIteration(ctx, cbc)
	}
	return nil
}

// captureSnapshot 捕获当前轮上下文消息 + DeepAgentState。
// Python: capture_snapshot (snapshot_rail.py L48-92)
//
// with_history=false 只取本轮消息段——与 rollback 的恢复口径一致；
// 用默认（含历史）会让每次回滚重复追加持久化的历史段。
func captureSnapshot(
	agent *agentcore.DeepAgent,
	sess interfaces.SessionFacade,
	previous *SafeStateSnapshot,
	index *int,
) *SafeStateSnapshot {
	var messages []llmschema.BaseMessage
	if agent != nil && sess != nil {
		if react := agent.ReactAgent(); react != nil {
			if ce := react.ContextEngine(); ce != nil {
				if mc := ce.GetContext(reactContextID, sess.GetSessionID()); mc != nil {
					if msgs, err := mc.GetMessages(0, false); err == nil {
						messages = cloneSnapshotMessages(msgs)
					}
				} else {
					logger.Warn(logComponent).
						Str("event_type", "HARNESS_SNAPSHOT_NO_CONTEXT").
						Str("session_id", sess.GetSessionID()).
						Msg("snapshot context not found; capture degraded to empty")
				}
			}
		}
	}
	state := map[string]any{}
	if agent != nil && sess != nil {
		state = agent.LoadState(sess).ToSessionDict()
	}
	return &SafeStateSnapshot{
		ContextMessages: messages,
		DeepAgentState:  state,
		IterationIndex:  nextIterationIndex(previous, index),
	}
}

// cloneSnapshotMessages 复制消息切片，避免快照与上下文共享底层数组。
func cloneSnapshotMessages(msgs []llmschema.BaseMessage) []llmschema.BaseMessage {
	return append([]llmschema.BaseMessage(nil), msgs...)
}

// nextIterationIndex 计算快照的迭代序号：显式优先，其次链式 +1，默认 1。
// Python: capture_snapshot 的 index 逻辑（snapshot_rail.py L86-87）
func nextIterationIndex(previous *SafeStateSnapshot, index *int) int {
	if index != nil {
		return *index
	}
	if previous != nil {
		return previous.IterationIndex + 1
	}
	return 1
}
