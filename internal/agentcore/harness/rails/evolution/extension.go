package evolution

import (
	"context"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// noOpExtension EvolutionExtension 的默认空实现。
// TrajectoryRail 直接使用此实例；子类可嵌入后只覆写需要的方法。
type noOpExtension struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// EvolutionTriggerPoint 演化触发时机枚举。
// 对齐 Python: EvolutionTriggerPoint
type EvolutionTriggerPoint string

const (
	// TriggerAfterInvoke 在 invoke 完成后触发
	TriggerAfterInvoke EvolutionTriggerPoint = "after_invoke"
	// TriggerAfterModelCall 在每次模型调用后触发
	TriggerAfterModelCall EvolutionTriggerPoint = "after_model_call"
	// TriggerAfterToolCall 在每次工具调用后触发
	TriggerAfterToolCall EvolutionTriggerPoint = "after_tool_call"
	// TriggerAfterTaskIteration 在每次任务循环迭代后触发
	TriggerAfterTaskIteration EvolutionTriggerPoint = "after_task_iteration"
	// TriggerNone 不自动触发（子类手动调用 run_evolution）
	TriggerNone EvolutionTriggerPoint = "none"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 接口 ────────────────────────────

// EvolutionExtension 演化轨道扩展点接口。
//
// EvolutionRail 的 4 个 final 回调内部完成轨迹收集后，
// 通过此接口将控制权交给子类实现的自定义逻辑。
// Go 嵌入无虚方法分派，必须通过接口实现多态。
//
// 对齐 Python: EvolutionRail._on_xxx + run_evolution 系列扩展点。
type EvolutionExtension interface {
	// OnBeforeInvoke 在每次 invoke 开始时调用（轨迹 builder 已初始化或复用）。
	// 对齐 Python: _on_before_invoke(ctx)
	OnBeforeInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error

	// OnAfterModelCall 在每次模型调用后调用（LLM 步骤已记录到 builder）。
	// 对齐 Python: _on_after_model_call(ctx)
	OnAfterModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error

	// OnAfterToolCall 在每次工具调用后调用（Tool 步骤已记录到 builder）。
	// 对齐 Python: _on_after_tool_call(ctx)
	OnAfterToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error

	// OnAfterInvoke 在每次 invoke 结束时调用（轨迹已保存，builder 仍可用）。
	// 对齐 Python: _on_after_invoke(ctx)
	OnAfterInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error

	// OnAfterTaskIteration 在每次任务循环迭代后调用。
	// 对齐 Python: _on_after_task_iteration(ctx)
	OnAfterTaskIteration(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error

	// OnAfterEvolutionTriggered 在 after_invoke 演化触发完成后调用。
	// 子类可覆写此方法消费对 AllowEvolutionTrigger 和快照可见的状态。
	// 对齐 Python: _on_after_evolution_triggered(trajectory, ctx)
	OnAfterEvolutionTriggered(ctx context.Context, traj *trajectory.Trajectory, cbc *agentinterfaces.AgentCallbackContext) error

	// AllowEvolutionTrigger 返回当前触发点是否允许启动演化。
	// 对齐 Python: _allow_evolution_trigger(trigger_point, ctx) -> bool
	AllowEvolutionTrigger(trigger EvolutionTriggerPoint, cbc *agentinterfaces.AgentCallbackContext) bool

	// SnapshotForEvolution 同步捕获快照（cbc 仍活跃），供后台演化任务使用。
	// 对齐 Python: _snapshot_for_evolution(trajectory, ctx) -> Optional[dict]
	SnapshotForEvolution(ctx context.Context, traj *trajectory.Trajectory, cbc *agentinterfaces.AgentCallbackContext) *EvolutionSnapshot

	// RunEvolution 执行演化逻辑。
	// 异步模式下 cbc 不可用，数据来自 snapshot。
	// 对齐 Python: run_evolution(trajectory, ctx=None, *, snapshot=None)
	RunEvolution(ctx context.Context, traj *trajectory.Trajectory, snapshot *EvolutionSnapshot) error
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func (noOpExtension) OnBeforeInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

func (noOpExtension) OnAfterModelCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

func (noOpExtension) OnAfterToolCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

func (noOpExtension) OnAfterInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

func (noOpExtension) OnAfterTaskIteration(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

func (noOpExtension) OnAfterEvolutionTriggered(_ context.Context, _ *trajectory.Trajectory, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

func (noOpExtension) AllowEvolutionTrigger(_ EvolutionTriggerPoint, _ *agentinterfaces.AgentCallbackContext) bool {
	return true
}

func (noOpExtension) SnapshotForEvolution(_ context.Context, traj *trajectory.Trajectory, _ *agentinterfaces.AgentCallbackContext) *EvolutionSnapshot {
	messages := collectMessagesFromTrajectory(traj)
	return &EvolutionSnapshot{Trajectory: traj, Messages: messages}
}

func (noOpExtension) RunEvolution(_ context.Context, _ *trajectory.Trajectory, _ *EvolutionSnapshot) error {
	return nil
}
