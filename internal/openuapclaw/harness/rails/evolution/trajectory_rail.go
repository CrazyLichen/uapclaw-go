// trajectory_rail.go 同步执行轨迹归档轨道。
// Python: openjiuwen/harness/rails/evolution/trajectory_rail.py（136 行全量对齐，
// 2026-11-21 17.6 基座审计缺口 6 修复——此前 Go 侧仅 30 行包装，归档语义由
// EvolutionRail.AfterInvoke 无条件 saveTrajectory 承载，属结构漂移，已删除）。
//
// 归档累积器与继承的 clean 演化窗口有意分离：归档接收全部可规范化增量
// （含带采集质量问题的增量——Python docstring 同句），并在最终 invoke 钩子
// 内同步至多落盘一次。
//
// 偏差登记：①Python trajectory_store=None 时构造抛 TypeError；Go 沿用
// NewEvolutionRail 缺省 InMemoryTrajectoryStore（既有拍板），不强制注入。
// ②Python 子类覆写 _record_execution_increment/_on_after_invoke/uninit；
// Go Final 回调设计下经 ext 包装（archiveExtension）+ 可选接口
// ExecutionIncrementRecorder 分派（与 EvolutionRail ext 设计同源）。

package evolution

import (
	"context"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TrajectoryRail 每个 Agent invoke 记录一份规范执行归档。
// Python: trajectory_rail.py:25 TrajectoryRail(EvolutionRail)。
type TrajectoryRail struct {
	*EvolutionRail
	// executionMu 归档累积锁（Python: _execution_lock RLock）
	executionMu sync.Mutex
	// executionAccumulators 归档累积器（键=采集登记，Python 键=subscription）
	executionAccumulators map[*invokeCapture]*trajectory.Trajectory
	// executionSeenSpans 已归档 span 身份集（Python: _execution_seen_spans）
	executionSeenSpans map[trajectory.SpanIdentity]struct{}
}

// archiveExtension TrajectoryRail 的 ext 包装：OnBeforeInvoke 清累积器、
// OnAfterInvoke 同步落盘，其余委托内层 ext。
type archiveExtension struct {
	inner EvolutionExtension
	rail  *TrajectoryRail
}

var _ EvolutionExtension = (*archiveExtension)(nil)
var _ ExecutionIncrementRecorder = (*archiveExtension)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTrajectoryRail 创建纯轨迹收集轨道。
// Python: trajectory_rail.py:37 __init__（trajectory_store 必填——Go 沿用
// 基类缺省 InMemory store，偏差登记见文件头）。
func NewTrajectoryRail(opts ...EvolutionRailOption) *TrajectoryRail {
	inner := NewEvolutionRail(noOpExtension{}, opts...)
	rail := &TrajectoryRail{
		EvolutionRail:         inner,
		executionAccumulators: map[*invokeCapture]*trajectory.Trajectory{},
		executionSeenSpans:    map[trajectory.SpanIdentity]struct{}{},
	}
	// ext 换装归档包装（evolution_trigger=NONE，Python __init__ 同参）。
	inner.SetExtension(&archiveExtension{inner: inner.GetExtension(), rail: rail})
	return rail
}

// Priority 返回优先级 10。
// Python: TrajectoryRail.priority = 10
func (r *TrajectoryRail) Priority() int { return 10 }

// RecordExecutionIncrement 归档累积：选择本流未归档过的 spans 并累积。
// Python: trajectory_rail.py:59 _record_execution_increment +
// :69 _select_new_execution_spans（调用方持有 _execution_lock，检查与预留
// 在并发 Agent invoke / Team run 下原子）。
func (r *TrajectoryRail) RecordExecutionIncrement(increment *trajectory.Trajectory) {
	if increment == nil {
		return
	}
	r.executionMu.Lock()
	defer r.executionMu.Unlock()
	selected := r.selectNewExecutionSpans(increment)
	if selected == nil {
		return
	}
	capture := r.currentCapture
	if capture == nil {
		return
	}
	previous := r.executionAccumulators[capture]
	if previous != nil {
		merged, err := trajectory.MergeTrajectories(
			previous.ToOTLP(), additionsToPayload(trajectory.IterSpans(selected.ToOTLP())))
		if err != nil {
			logger.Warn(logComponent).Err(err).Msg("execution accumulator merge failed")
			return
		}
		r.executionAccumulators[capture] = merged
		return
	}
	r.executionAccumulators[capture] = selected
}

// Uninit 清理归档状态（Python: trajectory_rail.py:112 uninit——清累积器与
// 已见身份集后委托基类）。
func (r *TrajectoryRail) Uninit(agent agentinterfaces.BaseAgent) error {
	r.executionMu.Lock()
	r.executionAccumulators = map[*invokeCapture]*trajectory.Trajectory{}
	r.executionSeenSpans = map[trajectory.SpanIdentity]struct{}{}
	r.executionMu.Unlock()
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// selectNewExecutionSpans 剔除本归档流已归档过的身份。
// Python: trajectory_rail.py:69 _select_new_execution_spans。
func (r *TrajectoryRail) selectNewExecutionSpans(increment *trajectory.Trajectory) *trajectory.Trajectory {
	payload := increment.ToOTLP()
	selected := false
	resourceSpans, _ := payload["resourceSpans"].([]any)
	for _, rsItem := range resourceSpans {
		resourceSpan, isMap := rsItem.(map[string]any)
		if !isMap {
			continue
		}
		scopeSpans, _ := resourceSpan["scopeSpans"].([]any)
		for _, ssItem := range scopeSpans {
			scopeSpan, isMap := ssItem.(map[string]any)
			if !isMap {
				continue
			}
			spans, _ := scopeSpan["spans"].([]any)
			kept := make([]any, 0, len(spans))
			for _, spanItem := range spans {
				span, isMap := spanItem.(map[string]any)
				if !isMap {
					continue
				}
				identity := trajectory.SpanIdentityOf(span)
				if identity != nil {
					if _, seen := r.executionSeenSpans[*identity]; seen {
						continue
					}
					r.executionSeenSpans[*identity] = struct{}{}
				}
				kept = append(kept, span)
				selected = true
			}
			scopeSpan["spans"] = kept
		}
	}
	if !selected {
		return nil
	}
	traj, err := trajectory.FromOTLP(payload)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("execution increment re-normalize failed")
		return nil
	}
	return traj
}

// saveExecutionArchive 应用规范元数据并同步落盘一份归档。
// Python: trajectory_rail.py:95 _save_execution_archive——Store.save 有意
// 同步且至多一次；后端失败与回调/演化流隔离（仅告警）。
func (r *TrajectoryRail) saveExecutionArchive(archive *trajectory.Trajectory, capture *invokeCapture) {
	metadata := trajectoryMetadata(capture.sessionID, capture.memberID, capture.teamID, r.memberRole)
	withMeta, err := withScopeMetadata(archive, metadata)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("execution archive metadata failed")
		withMeta = archive
	}
	store := r.TrajectoryStore()
	if store == nil {
		return
	}
	if err := store.Save(withMeta, ""); err != nil {
		// Python: logger.warning("failed to save execution trajectory")——后端
		// 特定错误不重试，与回调/演化流隔离。
		logger.Warn(logComponent).Err(err).Msg("failed to save execution trajectory")
	}
}

// archiveBeforeInvoke 清当前采集的归档累积器。
// Python: trajectory_rail.py:51 before_invoke——pop(capture.subscription)。
func (r *TrajectoryRail) archiveBeforeInvoke() {
	capture := r.currentCapture
	if capture == nil {
		return
	}
	r.executionMu.Lock()
	delete(r.executionAccumulators, capture)
	r.executionMu.Unlock()
}

// archiveAfterInvoke 弹出累积器并同步落盘（无累积器即无归档）。
// Python: trajectory_rail.py:80 _on_after_invoke。
func (r *TrajectoryRail) archiveAfterInvoke() {
	capture := r.currentCapture
	if capture == nil {
		return
	}
	r.executionMu.Lock()
	archive := r.executionAccumulators[capture]
	delete(r.executionAccumulators, capture)
	r.executionMu.Unlock()
	if archive == nil {
		return
	}
	r.saveExecutionArchive(archive, capture)
}

// ──────────────────────────── archiveExtension ────────────────────────────

// OnBeforeInvoke 清归档累积器后委托内层。
// Python: trajectory_rail.py:51 before_invoke。
func (e *archiveExtension) OnBeforeInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	e.rail.archiveBeforeInvoke()
	return e.inner.OnBeforeInvoke(ctx, cbc)
}

// RecordExecutionIncrement 归档累积委托（可选接口 ExecutionIncrementRecorder，
// 经 ext 包装分派——Python _record_execution_increment 覆写等价物）。
func (e *archiveExtension) RecordExecutionIncrement(increment *trajectory.Trajectory) {
	e.rail.RecordExecutionIncrement(increment)
}

// OnAfterModelCall 委托内层。
func (e *archiveExtension) OnAfterModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	return e.inner.OnAfterModelCall(ctx, cbc)
}

// OnAfterToolCall 委托内层。
func (e *archiveExtension) OnAfterToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	return e.inner.OnAfterToolCall(ctx, cbc)
}

// OnAfterTaskIteration 委托内层。
func (e *archiveExtension) OnAfterTaskIteration(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	return e.inner.OnAfterTaskIteration(ctx, cbc)
}

// OnAfterInvoke 同步落盘归档后委托内层。
// Python: trajectory_rail.py:80 _on_after_invoke。
func (e *archiveExtension) OnAfterInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	e.rail.archiveAfterInvoke()
	return e.inner.OnAfterInvoke(ctx, cbc)
}

// OnAfterEvolutionTriggered 委托内层。
func (e *archiveExtension) OnAfterEvolutionTriggered(ctx context.Context, traj *trajectory.Trajectory, cbc *agentinterfaces.AgentCallbackContext) error {
	return e.inner.OnAfterEvolutionTriggered(ctx, traj, cbc)
}

// AllowEvolutionTrigger 委托内层。
func (e *archiveExtension) AllowEvolutionTrigger(trigger EvolutionTriggerPoint, cbc *agentinterfaces.AgentCallbackContext) bool {
	return e.inner.AllowEvolutionTrigger(trigger, cbc)
}

// SnapshotForEvolution 委托内层。
func (e *archiveExtension) SnapshotForEvolution(ctx context.Context, traj *trajectory.Trajectory, cbc *agentinterfaces.AgentCallbackContext) *EvolutionSnapshot {
	return e.inner.SnapshotForEvolution(ctx, traj, cbc)
}

// RunEvolution 委托内层。
func (e *archiveExtension) RunEvolution(ctx context.Context, traj *trajectory.Trajectory, snapshot *EvolutionSnapshot) error {
	return e.inner.RunEvolution(ctx, traj, snapshot)
}

// GetEvolutionTotalTimeoutSecs 委托内层。
func (e *archiveExtension) GetEvolutionTotalTimeoutSecs() float64 {
	return e.inner.GetEvolutionTotalTimeoutSecs()
}
