package multi_dim

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

// MultiDimUpdater 多维更新器，按 domain 分发 signals 到不同域优化器，
// 合并各域更新映射，由 Trainer 统一应用。
//
// 一致性约束：维度仅按 Operator domain 划分（llm/tool/memory/skill_experience），
// 用户只需配置 domain_optimizers 映射，每个域仅允许一个优化器。
// 同一 workflow 可有多个 LLMCall/ToolCall/MemoryCall，但同一 domain 内
// 所有 Operator 由同一 optimizer 管理，避免冲突。
//
// 当前 bind/process/get_state/load_state 为默认实现（返回零值），
// 后续具体子类实现时重写。
//
// 对应 Python: openjiuwen/agent_evolving/updater/multi_dim.py MultiDimUpdater
type MultiDimUpdater struct {
	// domainOptimizers domain → optimizer 映射
	domainOptimizers map[string]optimizer.BaseOptimizer
}

// MultiDimUpdaterOption MultiDimUpdater 构造选项函数。
type MultiDimUpdaterOption func(*MultiDimUpdater)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMultiDimUpdater 创建 MultiDimUpdater 实例。
//
// 对应 Python: MultiDimUpdater(domain_optimizers={...})
func NewMultiDimUpdater(opts ...MultiDimUpdaterOption) *MultiDimUpdater {
	u := &MultiDimUpdater{
		domainOptimizers: map[string]optimizer.BaseOptimizer{},
	}
	for _, opt := range opts {
		opt(u)
	}
	return u
}

// WithDomainOptimizers 设置域优化器映射。
// 对应 Python: MultiDimUpdater(domain_optimizers={...})
func WithDomainOptimizers(optimizers map[string]optimizer.BaseOptimizer) MultiDimUpdaterOption {
	return func(u *MultiDimUpdater) {
		if optimizers != nil {
			u.domainOptimizers = optimizers
		}
	}
}

// DomainOptimizers 返回当前域优化器映射（只读副本）。
func (u *MultiDimUpdater) DomainOptimizers() map[string]optimizer.BaseOptimizer {
	result := make(map[string]optimizer.BaseOptimizer, len(u.domainOptimizers))
	for k, v := range u.domainOptimizers {
		result[k] = v
	}
	return result
}

// Bind 绑定 Operator 注册表并过滤可优化的 Operator。
// 当前默认实现返回 0，后续具体子类重写。
//
// 对应 Python: MultiDimUpdater.bind(operators, targets, **config) — @abstractmethod
func (u *MultiDimUpdater) Bind(operators map[string]operator.Operator, targets []string, config map[string]any) int {
	return 0
}

// RequiresForwardData 检查是否有任何域优化器需要前向推理数据。
// 如果任意优化器的 RequiresForwardData 返回 true，则返回 true。
//
// 对齐 Python:
//
//	for opt in self._domain_optimizers.values():
//	    requires = getattr(opt, "requires_forward_data", None)
//	    if callable(requires) and requires():
//	        return True
//	return False
//
// 对应 Python: MultiDimUpdater.requires_forward_data()
func (u *MultiDimUpdater) RequiresForwardData() bool {
	for _, opt := range u.domainOptimizers {
		if opt.RequiresForwardData() {
			return true
		}
	}
	return false
}

// Process 信号优先入口，按 domain 分发 signals 到对应优化器，合并更新映射。
// 当前默认实现返回空 map，后续具体子类重写。
//
// 对应 Python: MultiDimUpdater.process(trajectories, signals, config) — @abstractmethod
func (u *MultiDimUpdater) Process(ctx context.Context, trajectories []*trajectory.Trajectory, signals []*signal.EvolutionSignal, config map[string]any) (map[schema.UpdateKey]any, error) {
	return map[schema.UpdateKey]any{}, nil
}

// Update 离线兼容入口，将 EvaluatedCase 转换为 EvolutionSignal 后调用 Process。
//
// 对齐 Python:
//
//	score_threshold = config.get("score_threshold")
//	signals = []
//	for case in evaluated_cases:
//	    signal = from_evaluated_case(case, score_threshold=score_threshold)
//	    if signal is not None:
//	        signals.append(signal)
//	return await self.process(trajectories, signals, config)
//
// 对应 Python: MultiDimUpdater.update(trajectories, evaluated_cases, config)
func (u *MultiDimUpdater) Update(ctx context.Context, trajectories []*trajectory.Trajectory, evaluatedCases []*dataset.EvaluatedCase, config map[string]any) (map[schema.UpdateKey]any, error) {
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
// 当前默认实现返回空 map，后续具体子类重写。
//
// 对应 Python: MultiDimUpdater.get_state() — @abstractmethod
func (u *MultiDimUpdater) GetState() map[string]any {
	return map[string]any{}
}

// LoadState 从检查点恢复状态，当前为无操作。
// 后续具体子类重写。
//
// 对应 Python: MultiDimUpdater.load_state(state) — @abstractmethod
func (u *MultiDimUpdater) LoadState(_ map[string]any) {
	// 默认 no-op，后续子类重写
}

// ──────────────────────────── 非导出函数 ────────────────────────────
