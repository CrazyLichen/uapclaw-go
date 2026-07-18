package tool_call

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolCallOperator 工具描述参数句柄。
//
// 管理 tool_description 参数（map[tool_name]description）。
// 参数变更通过 onParameterUpdated 回调推送给消费者。
//
// 更新入口：
//   - SetParameter(): 演化更新
//   - LoadState(): 检查点恢复
//
// 对应 Python: openjiuwen/core/operator/tool_call/base.py ToolCallOperator
type ToolCallOperator struct {
	// operatorID 操作器标识
	operatorID string
	// descriptions 工具描述字典 map[tool_name]description
	descriptions map[string]any
	// onParameterUpdated 参数变更回调
	onParameterUpdated operator.ParameterUpdatedCallback
}

// ToolCallOperatorOption ToolCallOperator 构造选项函数。
type ToolCallOperatorOption func(*ToolCallOperator)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// TargetToolDescription 工具描述目标名。
	// 对应 Python: "tool_description"
	TargetToolDescription = "tool_description"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewToolCallOperator 创建 ToolCallOperator 实例。
//
// 对应 Python: ToolCallOperator.__init__(operator_id, descriptions, on_parameter_updated)
func NewToolCallOperator(operatorID string, opts ...ToolCallOperatorOption) *ToolCallOperator {
	op := &ToolCallOperator{
		operatorID:   operatorID,
		descriptions: make(map[string]any),
	}

	for _, opt := range opts {
		opt(op)
	}

	return op
}

// OperatorID 返回操作器标识。
//
// 对应 Python: ToolCallOperator.operator_id (property)
func (op *ToolCallOperator) OperatorID() string {
	return op.operatorID
}

// GetTunables 获取可调参数。
// 仅当 descriptions 非空时暴露 tool_description。
//
// 对应 Python: ToolCallOperator.get_tunables()
func (op *ToolCallOperator) GetTunables() map[string]operator.TunableSpec {
	if len(op.descriptions) == 0 {
		return map[string]operator.TunableSpec{}
	}

	return map[string]operator.TunableSpec{
		TargetToolDescription: {
			Name:       TargetToolDescription,
			Kind:       operator.TunableKindText,
			Path:       TargetToolDescription,
			Constraint: map[string]any{"type": "dict"},
		},
	}
}

// SetParameter 设置可调参数值（工具描述）。
// 仅接受 target="tool_description" 且 value 为 map 类型。
// 直接赋值，不做类型转换，对齐 Python value.copy() 行为。
//
// 对应 Python: ToolCallOperator.set_parameter(target, value)
func (op *ToolCallOperator) SetParameter(target string, value any) {
	if target != TargetToolDescription {
		return
	}
	descMap, ok := value.(map[string]any)
	if !ok {
		return
	}
	op.descriptions = cloneAnyMap(descMap)

	if op.onParameterUpdated != nil {
		op.onParameterUpdated(target, cloneAnyMap(op.descriptions))
	}
}

// GetState 获取当前状态，用于检查点。
//
// 对应 Python: ToolCallOperator.get_state()
func (op *ToolCallOperator) GetState() map[string]any {
	return map[string]any{
		TargetToolDescription: cloneAnyMap(op.descriptions),
	}
}

// LoadState 从检查点恢复状态。
// 触发 onParameterUpdated 回调。
// 直接赋值，不做类型转换，对齐 Python state["tool_description"].copy() 行为。
//
// 对应 Python: ToolCallOperator.load_state(state)
func (op *ToolCallOperator) LoadState(state map[string]any) {
	if td, ok := state[TargetToolDescription]; ok {
		if descMap, ok := td.(map[string]any); ok {
			op.descriptions = cloneAnyMap(descMap)

			if op.onParameterUpdated != nil {
				op.onParameterUpdated(TargetToolDescription, cloneAnyMap(op.descriptions))
			}
		}
	}
}

// ApplyUpdate 应用结构化演化更新。
// 使用 DefaultApplyUpdate 提供的默认兼容行为。
//
// 对应 Python: Operator.apply_update 默认实现
func (op *ToolCallOperator) ApplyUpdate(target string, update schema.UpdateValue) schema.ApplyResult {
	return operator.DefaultApplyUpdate(op, target, update)
}

// WithDescriptions 设置初始工具描述选项。
func WithDescriptions(descriptions map[string]any) ToolCallOperatorOption {
	return func(op *ToolCallOperator) {
		if descriptions != nil {
			op.descriptions = cloneAnyMap(descriptions)
		}
	}
}

// WithToolCallOnParameterUpdated 设置参数变更回调选项。
func WithToolCallOnParameterUpdated(cb operator.ParameterUpdatedCallback) ToolCallOperatorOption {
	return func(op *ToolCallOperator) { op.onParameterUpdated = cb }
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// cloneAnyMap 克隆 map[string]any。
func cloneAnyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}
