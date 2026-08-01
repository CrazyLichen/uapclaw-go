package memory_call

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer"
	cschema "github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryOptimizerBase 记忆维度优化器基类，固定 domain="memory"，
// 默认优化目标为 enabled 和 max_retries。
//
// 子优化器嵌入此结构体，获得记忆维度的公共字段和辅助方法，
// 然后自己实现 optimizer.BaseOptimizer 接口的全部方法。
//
// 对应 Python: openjiuwen/agent_evolving/optimizer/memory_call/base.py MemoryOptimizerBase
type MemoryOptimizerBase struct {
	optimizer.BaseOptimizerMixin
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// Domain 返回优化器域 "memory"。
//
// 对应 Python: MemoryOptimizerBase.domain = "memory"
func (b *MemoryOptimizerBase) Domain() string {
	return "memory"
}

// DefaultTargets 返回默认优化目标列表。
//
// 对齐 Python:
//
//	def default_targets(self) -> List[str]:
//	    return ["enabled", "max_retries"]
//
// 对应 Python: MemoryOptimizerBase.default_targets()
func (b *MemoryOptimizerBase) DefaultTargets() []string {
	return []string{"enabled", "max_retries"}
}

// RequiresForwardData 返回 true，记忆优化器需要框架执行前向推理。
//
// 对齐 Python:
//
//	@staticmethod
//	def requires_forward_data() -> bool:
//	    return True
//
// 对应 Python: BaseOptimizer.requires_forward_data() → True
func (b *MemoryOptimizerBase) RequiresForwardData() bool {
	return true
}

// Bind 过滤并绑定可优化的 Operator，返回匹配数量；0 触发上层软退出。
//
// 对齐 Python:
//
//	self._targets = list(targets or self.default_targets())
//	MemoryOptimizerBase.filter_operators() 委托 super().filter_operators()，
//	仅为文档目的显式声明，Go 中 Mixin.Bind 内部已调用 FilterOperators。
//
// 对应 Python: BaseOptimizer.bind()
func (b *MemoryOptimizerBase) Bind(operators map[string]operator.Operator, targets []string, config map[string]any) int {
	// 对齐 Python: targets or self.default_targets()
	if len(targets) == 0 {
		targets = b.DefaultTargets()
	}
	return b.BaseOptimizerMixin.Bind(operators, targets, config)
}

// AddTrajectory 缓存 Trajectory 供 backward 阶段查询。
func (b *MemoryOptimizerBase) AddTrajectory(traj *trajectory.Trajectory) {
	b.BaseOptimizerMixin.AddTrajectory(traj)
}

// GetTrajectories 返回当前缓存的轨迹列表（副本）。
func (b *MemoryOptimizerBase) GetTrajectories() []*trajectory.Trajectory {
	return b.BaseOptimizerMixin.GetTrajectories()
}

// ClearTrajectories 清空轨迹缓存。
func (b *MemoryOptimizerBase) ClearTrajectories() {
	b.BaseOptimizerMixin.ClearTrajectories()
}

// Backward 反向传播：从信号计算梯度。
//
// 对齐 Python:
//
//	async def _backward(self, signals: List["EvolutionSignal"]) -> None:
//	    """Subclasses implement memory-specific backward logic."""
//	    pass
//
// 对应 Python: MemoryOptimizerBase._backward(signals) → pass
func (b *MemoryOptimizerBase) Backward(_ context.Context, _ []*signal.EvolutionSignal) error {
	return nil
}

// Step 生成更新映射。空梯度自然产生空映射。
//
// 对齐 Python:
//
//	MemoryOptimizerBase._backward() 是 pass，不写梯度；
//	_step() 是 BaseOptimizer 的抽象方法，子类必须实现。
//	此处返回空 map，与 ToolOptimizerBase.Step() 模式一致。
//
// 对应 Python: BaseOptimizer._step() → 抽象（子类实现）
func (b *MemoryOptimizerBase) Step() map[cschema.UpdateKey]any {
	return map[cschema.UpdateKey]any{}
}

// Parameters 返回梯度容器的副本。
func (b *MemoryOptimizerBase) Parameters() map[string]*optimizer.TextualParameter {
	return b.BaseOptimizerMixin.Parameters()
}

// SelectSignals 选择此优化器可消费的信号。默认保留全部信号。
func (b *MemoryOptimizerBase) SelectSignals(signals []*signal.EvolutionSignal) []*signal.EvolutionSignal {
	return b.BaseOptimizerMixin.SelectSignals(signals)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
