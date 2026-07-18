package optimizer

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TextualParameter operator_id 的梯度容器，存储 target→梯度值和可选描述。
// 不再持有 Operator 引用。
//
// 对应 Python: TextualParameter
type TextualParameter struct {
	// OperatorID 所属 Operator 标识
	OperatorID string
	// Gradients 梯度映射 target → gradient value (string 或 []string)
	Gradients map[string]any
	// Description 可选描述
	Description string
}

// BaseOptimizerMixin 优化器公共逻辑嵌入结构体。
//
// 子优化器嵌入此结构体，获得公共字段和辅助方法（Bind/AddTrajectory/ValidateParameters 等），
// 然后自己实现 BaseOptimizer 接口的全部方法。
//
// 典型子优化器实现模式：
//   - Domain()/RequiresForwardData()/DefaultTargets() — 返回维度常量
//   - Bind() — 委托 o.BaseOptimizerMixin.Bind()
//   - AddTrajectory()/GetTrajectories()/ClearTrajectories() — 委托 Mixin
//   - Backward() — 调用 Mixin.ValidateParameters() + SelectSignals() + 子类逻辑 + 错误包装
//   - Step() — 调用 Mixin.ValidateParameters() + 子类逻辑 + ClearTrajectories()
//   - Parameters()/SelectSignals() — 委托 Mixin
type BaseOptimizerMixin struct {
	// operators 绑定的 Operator 映射
	operators map[string]operator.Operator
	// parameters 梯度容器映射
	parameters map[string]*TextualParameter
	// targets 优化目标列表
	targets []string
	// trajectories 缓存的执行轨迹列表
	trajectories []*trajectory.Trajectory
	// selectedSignals 选中的演化信号列表
	selectedSignals []*signal.EvolutionSignal
}

// BaseOptimizer 维度优化器的公共接口。
//
// 定义优化器的生命周期：
//  1. Bind() — 过滤并绑定可优化的 Operator，返回匹配数量
//  2. AddTrajectory() — 缓存 Trajectory 供 backward 查询
//  3. Backward() — 从信号计算梯度
//  4. Step() — 从梯度生成更新映射，由 Trainer.apply_updates 统一应用
//
// 对应 Python: BaseOptimizer
type BaseOptimizer interface {
	// Domain 返回优化器域（llm/tool/memory/skill_experience）。
	Domain() string

	// RequiresForwardData 是否需要框架执行前向推理。
	// 返回 false 的黑盒优化器（如 tool_optimizer）在内部生成/执行/评估，
	// 不依赖框架的前向推理数据。
	RequiresForwardData() bool

	// DefaultTargets 返回此维度的默认目标列表。
	DefaultTargets() []string

	// Bind 过滤并绑定可优化的 Operator，返回匹配数量；0 触发上层软退出。
	Bind(operators map[string]operator.Operator, targets []string, config map[string]any) int

	// AddTrajectory 缓存 Trajectory 供 backward 阶段查询。
	AddTrajectory(traj *trajectory.Trajectory)

	// GetTrajectories 返回当前缓存的轨迹列表（副本）。
	GetTrajectories() []*trajectory.Trajectory

	// ClearTrajectories 清空轨迹缓存。
	ClearTrajectories()

	// Backward 反向传播：从信号计算梯度。
	Backward(ctx context.Context, signals []*signal.EvolutionSignal) error

	// Step 生成更新映射，由 Trainer.apply_updates 统一应用。
	Step() map[schema.UpdateKey]any

	// Parameters 返回梯度容器的副本。
	Parameters() map[string]*TextualParameter

	// SelectSignals 选择此优化器可消费的信号。
	// 默认保留全部信号，失败驱动语义的优化器应显式覆盖此方法。
	SelectSignals(signals []*signal.EvolutionSignal) []*signal.EvolutionSignal
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent optimizer 包日志组件常量
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTextualParameter 创建 TextualParameter 实例。
//
// 对应 Python: TextualParameter(operator_id=op_id)
func NewTextualParameter(operatorID string) *TextualParameter {
	return &TextualParameter{
		OperatorID: operatorID,
		Gradients:  map[string]any{},
	}
}

// SetGradient 设置目标梯度值。
//
// 对应 Python: TextualParameter.set_gradient(name, gradient)
func (p *TextualParameter) SetGradient(name string, gradient any) {
	p.Gradients[name] = gradient
}

// GetGradient 获取目标梯度值。
//
// 对应 Python: TextualParameter.get_gradient(name)
func (p *TextualParameter) GetGradient(name string) any {
	return p.Gradients[name]
}

// SetDescription 设置描述。
//
// 对应 Python: TextualParameter.set_description(description)
func (p *TextualParameter) SetDescription(description string) {
	p.Description = description
}

// GetDescription 获取描述。
//
// 对应 Python: TextualParameter.get_description()
func (p *TextualParameter) GetDescription() string {
	return p.Description
}

// Bind 过滤并绑定可优化的 Operator，返回匹配数量；0 触发上层软退出。
//
// 对齐 Python:
//
//	if operators is None: operators = {}
//	self._targets = list(targets or self.default_targets())
//	self._operators = self.filter_operators(operators, self._targets)
//	self._parameters = {op_id: TextualParameter(operator_id=op_id) for op_id in self._operators}
//	self._trajectories = []
//	self._selected_signals = []
//	if not self._operators:
//	    logger.error("[optimizer] no operator matches targets=%s; will soft-exit", self._targets)
//	return len(self._operators)
//
// 对应 Python: BaseOptimizer.bind()
func (m *BaseOptimizerMixin) Bind(operators map[string]operator.Operator, targets []string, config map[string]any) int {
	m.targets = targets
	m.operators = FilterOperators(operators, m.targets)
	m.parameters = make(map[string]*TextualParameter, len(m.operators))
	for opID := range m.operators {
		m.parameters[opID] = NewTextualParameter(opID)
	}
	m.trajectories = nil
	m.selectedSignals = nil
	if len(m.operators) == 0 {
		logger.Error(logComponent).
			Str("method", "Bind").
			Strs("targets", m.targets).
			Msg("[optimizer] no operator matches targets; will soft-exit")
	}
	return len(m.operators)
}

// AddTrajectory 缓存 Trajectory 供 backward 阶段查询。
//
// 对应 Python: BaseOptimizer.add_trajectory(trajectory)
func (m *BaseOptimizerMixin) AddTrajectory(traj *trajectory.Trajectory) {
	m.trajectories = append(m.trajectories, traj)
}

// GetTrajectories 返回当前缓存的轨迹列表（副本）。
//
// 对应 Python: BaseOptimizer.get_trajectories()
func (m *BaseOptimizerMixin) GetTrajectories() []*trajectory.Trajectory {
	result := make([]*trajectory.Trajectory, len(m.trajectories))
	copy(result, m.trajectories)
	return result
}

// ClearTrajectories 清空轨迹缓存。
//
// 对应 Python: BaseOptimizer.clear_trajectories()
func (m *BaseOptimizerMixin) ClearTrajectories() {
	m.trajectories = nil
}

// Parameters 返回梯度容器的副本。
//
// 对应 Python: BaseOptimizer.parameters()
func (m *BaseOptimizerMixin) Parameters() map[string]*TextualParameter {
	result := make(map[string]*TextualParameter, len(m.parameters))
	for k, v := range m.parameters {
		result[k] = v
	}
	return result
}

// SelectSignals 选择此优化器可消费的信号。默认保留全部信号。
//
// 对齐 Python:
//
//	return list(signals)
//
// 对应 Python: BaseOptimizer._select_signals(signals)
func (m *BaseOptimizerMixin) SelectSignals(signals []*signal.EvolutionSignal) []*signal.EvolutionSignal {
	result := make([]*signal.EvolutionSignal, len(signals))
	copy(result, signals)
	return result
}

// ValidateParameters 空参数校验，参数为空时抛异常。
//
// 对齐 Python:
//
//	if not self._parameters:
//	    raise build_error(StatusCode.TOOLCHAIN_AGENT_PARAM_ERROR, error_msg="cannot optimize empty parameters")
//
// 对应 Python: BaseOptimizer._validate_parameters()
func (m *BaseOptimizerMixin) ValidateParameters() {
	if len(m.parameters) == 0 {
		panic(exception.NewBaseError(
			exception.NewStatusCode("TOOLCHAIN_AGENT_PARAM_ERROR", 170000, ""),
			exception.WithMsg("cannot optimize empty parameters"),
		))
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// FilterOperators 过滤暴露任何 target 的 Operator。对不匹配的记录警告，不中断。
//
// 对齐 Python:
//
//	for op_id, op in (operators or {}).items():
//	    tunables = op.get_tunables()
//	    matched = [t for t in targets if t in tunables]
//	    if not matched:
//	        logger.warning("[optimizer] operator %s has no tunables in targets=%s", op_id, targets)
//	        continue
//	    out[op_id] = op
//
// 对应 Python: BaseOptimizer.filter_operators()
func FilterOperators(operators map[string]operator.Operator, targets []string) map[string]operator.Operator {
	out := make(map[string]operator.Operator)
	for opID, op := range operators {
		tunables := op.GetTunables()
		matched := false
		for _, t := range targets {
			if _, exists := tunables[t]; exists {
				matched = true
				break
			}
		}
		if !matched {
			logger.Warn(logComponent).
				Str("method", "FilterOperators").
				Str("operator_id", opID).
				Strs("targets", targets).
				Msg("[optimizer] operator has no tunables in targets")
			continue
		}
		out[opID] = op
	}
	return out
}
