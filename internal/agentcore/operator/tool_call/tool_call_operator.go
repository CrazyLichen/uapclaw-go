package tool_call

import (
	"fmt"
	"maps"

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
	descriptions map[string]string
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
		descriptions: map[string]string{},
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
// 仅接受 target="tool_description" 且 value 为 map[string]string 或 map[string]any 类型。
// 不合法类型静默忽略（对齐 Python 行为），不更新内部状态。
//
// 对应 Python: ToolCallOperator.set_parameter(target, value)
func (op *ToolCallOperator) SetParameter(target string, value any) {
	if target != TargetToolDescription {
		return
	}
	descs := toDescriptions(value)
	if descs == nil {
		return
	}
	op.descriptions = descs

	if op.onParameterUpdated != nil {
		op.onParameterUpdated(target, maps.Clone(op.descriptions))
	}
}

// GetState 获取当前状态，用于检查点。
//
// 对应 Python: ToolCallOperator.get_state()
func (op *ToolCallOperator) GetState() map[string]any {
	return map[string]any{
		TargetToolDescription: maps.Clone(op.descriptions),
	}
}

// LoadState 从检查点恢复状态。
// 触发 onParameterUpdated 回调。
// 直接赋值，对齐 Python state["tool_description"].copy() 行为。
//
// 对应 Python: ToolCallOperator.load_state(state)
func (op *ToolCallOperator) LoadState(state map[string]any) {
	if td, ok := state[TargetToolDescription]; ok {
		descs := toDescriptions(td)
		if descs != nil {
			op.descriptions = descs

			if op.onParameterUpdated != nil {
				op.onParameterUpdated(TargetToolDescription, maps.Clone(op.descriptions))
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
func WithDescriptions(descriptions map[string]string) ToolCallOperatorOption {
	return func(op *ToolCallOperator) {
		if descriptions != nil {
			op.descriptions = maps.Clone(descriptions)
		}
	}
}

// WithToolCallOnParameterUpdated 设置参数变更回调选项。
func WithToolCallOnParameterUpdated(cb operator.ParameterUpdatedCallback) ToolCallOperatorOption {
	return func(op *ToolCallOperator) { op.onParameterUpdated = cb }
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toDescriptions 将 any 值转为 map[string]string。
// 支持 map[string]string 和 map[string]any 两种输入，
// map[string]any 中的值会被转为字符串（对齐 Python Dict[str, str]）。
// 不合法类型返回 nil。
func toDescriptions(value any) map[string]string {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case map[string]string:
		return maps.Clone(v)
	case map[string]any:
		result := make(map[string]string, len(v))
		for k, val := range v {
			result[k] = toString(val)
		}
		return result
	default:
		return nil
	}
}

// toString 将 any 转为 string。
func toString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	default:
		return fmt.Sprintf("%v", val)
	}
}
