package single_dim

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/dataset"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SingleDimUpdater 单维更新器，委托内部 BaseOptimizer 的 backward→step 链路。
//
// 将 signals 传递给 optimizer 生成梯度（backward），
// 再由 step 返回更新映射，由 Trainer 统一应用。
//
// 对应 Python: openjiuwen/agent_evolving/updater/single_dim.py SingleDimUpdater
type SingleDimUpdater struct {
	// opt 内部优化器实例
	opt optimizer.BaseOptimizer
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSingleDimUpdater 创建 SingleDimUpdater 实例。
//
// 对应 Python: SingleDimUpdater(optimizer=BaseOptimizer)
func NewSingleDimUpdater(opt optimizer.BaseOptimizer) *SingleDimUpdater {
	return &SingleDimUpdater{opt: opt}
}

// Bind 绑定 Operator 注册表，过滤可优化的 Operator。
// 返回匹配数量；0 触发 Trainer 软退出。
// 委托给内部优化器的 Bind 方法。
//
// 当 targets 为 nil 时，从 config["targets"] 中获取有效目标列表，
// 对齐 Python: effective_targets = targets or config.get("targets")。
//
// 对应 Python: SingleDimUpdater.bind(operators, targets, **config)
func (u *SingleDimUpdater) Bind(operators map[string]operator.Operator, targets []string, config map[string]any) int {
	effectiveTargets := targets
	if effectiveTargets == nil && config != nil {
		if t, ok := config["targets"]; ok {
			if ts, ok := t.([]string); ok {
				effectiveTargets = ts
			}
		}
	}
	return u.opt.Bind(operators, effectiveTargets, config)
}

// RequiresForwardData 判断是否需要前向推理数据，委托给内部优化器。
//
// 对应 Python: SingleDimUpdater.requires_forward_data()
func (u *SingleDimUpdater) RequiresForwardData() bool {
	return u.opt.RequiresForwardData()
}

// Process 信号优先入口：写入轨迹 → 执行 backward → 返回 step 结果。
//
// 对应 Python: SingleDimUpdater.process(trajectories, signals, config)
func (u *SingleDimUpdater) Process(ctx context.Context, trajectories []*trajectory.Trajectory, signals []*signal.EvolutionSignal, config map[string]any) (map[schema.UpdateKey]any, error) {
	// 写入轨迹
	// 对齐 Python: for traj in trajectories: self._opt.add_trajectory(traj)
	for _, traj := range trajectories {
		u.opt.AddTrajectory(traj)
	}

	// 执行 backward
	// 对齐 Python: await self._opt.backward(signals)
	if err := u.opt.Backward(ctx, signals); err != nil {
		return nil, err
	}

	// 执行 step
	// 对齐 Python: return self._opt.step()
	return u.opt.Step(), nil
}

// Update 离线兼容入口，将 EvaluatedCase 转换为 EvolutionSignal 后调用 Process。
//
// 对齐 Python:
//
//	signals = []
//	for case in evaluated_cases:
//	    signal = from_evaluated_case(case, score_threshold=score_threshold)
//	    if signal is not None:
//	        signals.append(signal)
//	return await self.process(trajectories, signals, config)
//
// 对应 Python: SingleDimUpdater.update(trajectories, evaluated_cases, config)
func (u *SingleDimUpdater) Update(ctx context.Context, trajectories []*trajectory.Trajectory, evaluatedCases []*dataset.EvaluatedCase, config map[string]any) (map[schema.UpdateKey]any, error) {
	// 从 config 中提取 score_threshold
	// 对齐 Python: score_threshold = config.get("score_threshold")
	var scoreThreshold *float64
	if config != nil {
		if st, ok := config["score_threshold"]; ok {
			if f, ok := st.(float64); ok {
				scoreThreshold = &f
			}
		}
	}

	signals := signal.FromEvaluatedCases(evaluatedCases, "", scoreThreshold)
	return u.Process(ctx, trajectories, signals, config)
}

// GetState 获取 Updater 可序列化状态。
// 当前 BaseOptimizer 无稳定可恢复状态，返回空 map。
//
// 对齐 Python: @staticmethod def get_state() -> Dict[str, Any]: return {}
//
// 对应 Python: SingleDimUpdater.get_state()
func (u *SingleDimUpdater) GetState() map[string]any {
	return map[string]any{}
}

// LoadState 从检查点恢复状态，当前为无操作。
//
// 对齐 Python: @staticmethod def load_state(state: Dict[str, Any]) -> None: return None
//
// 对应 Python: SingleDimUpdater.load_state(state)
func (u *SingleDimUpdater) LoadState(_ map[string]any) {
	// 当前 BaseOptimizer 无稳定可恢复状态，no-op
}

// ──────────────────────────── 非导出函数 ────────────────────────────
