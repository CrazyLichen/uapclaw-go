package memory_call

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryCallOperator 记忆参数句柄。
//
// 管理 enabled 和 maxRetries 参数。
// 参数变更通过 onParameterUpdated 回调推送给消费者。
//
// 更新入口：
//   - SetParameter(): 演化更新
//   - LoadState(): 检查点恢复
//
// 对应 Python: openjiuwen/core/operator/memory_call/base.py MemoryCallOperator
type MemoryCallOperator struct {
	// operatorID 操作器标识
	operatorID string
	// enabled 是否启用记忆
	enabled bool
	// maxRetries 最大重试次数（0-5）
	maxRetries int
	// onParameterUpdated 参数变更回调
	onParameterUpdated operator.ParameterUpdatedCallback
}

// MemoryCallOperatorOption MemoryCallOperator 构造选项函数。
type MemoryCallOperatorOption func(*MemoryCallOperator)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// TargetEnabled 启用状态目标名。
	// 对应 Python: "enabled"
	TargetEnabled = "enabled"
	// TargetMaxRetries 最大重试次数目标名。
	// 对应 Python: "max_retries"
	TargetMaxRetries = "max_retries"
	// 默认操作器标识
	defaultMemoryOperatorID = "memory_call"
	// maxRetries 下限
	minMaxRetries = 0
	// maxRetries 上限
	maxMaxRetries = 5
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMemoryCallOperator 创建 MemoryCallOperator 实例。
//
// 对应 Python: MemoryCallOperator.__init__(operator_id, on_parameter_updated)
func NewMemoryCallOperator(opts ...MemoryCallOperatorOption) *MemoryCallOperator {
	op := &MemoryCallOperator{
		operatorID: defaultMemoryOperatorID,
		enabled:    true, // Python 默认 enabled=True
		maxRetries: 0,   // Python 默认 max_retries=0
	}

	for _, opt := range opts {
		opt(op)
	}

	return op
}

// OperatorID 返回操作器标识。
//
// 对应 Python: MemoryCallOperator.operator_id (property)
func (op *MemoryCallOperator) OperatorID() string {
	return op.operatorID
}

// GetTunables 获取可调参数。
//
// 对应 Python: MemoryCallOperator.get_tunables()
func (op *MemoryCallOperator) GetTunables() map[string]operator.TunableSpec {
	return map[string]operator.TunableSpec{
		TargetEnabled: {
			Name:       TargetEnabled,
			Kind:       operator.TunableKindDiscrete,
			Path:       TargetEnabled,
			Constraint: map[string]any{"type": "bool"},
		},
		TargetMaxRetries: {
			Name:       TargetMaxRetries,
			Kind:       operator.TunableKindDiscrete,
			Path:       TargetMaxRetries,
			Constraint: map[string]any{"type": "int", "min": 0, "max": 5},
		},
	}
}

// SetParameter 设置可调参数值。
// 触发 onParameterUpdated 回调。
//
// 对应 Python: MemoryCallOperator.set_parameter(target, value)
func (op *MemoryCallOperator) SetParameter(target string, value any) {
	if target == TargetEnabled {
		op.enabled = toBool(value)
		if op.onParameterUpdated != nil {
			op.onParameterUpdated(TargetEnabled, op.enabled)
		}
	} else if target == TargetMaxRetries {
		op.maxRetries = clampMaxRetries(toInt(value))
		if op.onParameterUpdated != nil {
			op.onParameterUpdated(TargetMaxRetries, op.maxRetries)
		}
	}
}

// GetState 获取当前状态，用于检查点。
//
// 对应 Python: MemoryCallOperator.get_state()
func (op *MemoryCallOperator) GetState() map[string]any {
	return map[string]any{
		TargetEnabled:    op.enabled,
		TargetMaxRetries: op.maxRetries,
	}
}

// LoadState 从检查点恢复状态。
// 触发 onParameterUpdated 回调。
//
// 对应 Python: MemoryCallOperator.load_state(state)
func (op *MemoryCallOperator) LoadState(state map[string]any) {
	if e, ok := state[TargetEnabled]; ok {
		op.enabled = toBool(e)
		if op.onParameterUpdated != nil {
			op.onParameterUpdated(TargetEnabled, op.enabled)
		}
	}
	if mr, ok := state[TargetMaxRetries]; ok {
		op.maxRetries = clampMaxRetries(toInt(mr))
		if op.onParameterUpdated != nil {
			op.onParameterUpdated(TargetMaxRetries, op.maxRetries)
		}
	}
}

// ApplyUpdate 应用结构化演化更新。
// 使用 DefaultApplyUpdate 提供的默认兼容行为。
//
// 对应 Python: Operator.apply_update 默认实现
func (op *MemoryCallOperator) ApplyUpdate(target string, update schema.UpdateValue) schema.ApplyResult {
	return operator.DefaultApplyUpdate(op, target, update)
}

// WithMemoryOperatorID 设置操作器标识选项。
func WithMemoryOperatorID(id string) MemoryCallOperatorOption {
	return func(op *MemoryCallOperator) { op.operatorID = id }
}

// WithMemoryOnParameterUpdated 设置参数变更回调选项。
func WithMemoryOnParameterUpdated(cb operator.ParameterUpdatedCallback) MemoryCallOperatorOption {
	return func(op *MemoryCallOperator) { op.onParameterUpdated = cb }
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// clampMaxRetries 将 maxRetries 钳位到 [0, 5]。
func clampMaxRetries(v int) int {
	if v < minMaxRetries {
		return minMaxRetries
	}
	if v > maxMaxRetries {
		return maxMaxRetries
	}
	return v
}

// toBool 将 any 转为 bool。
func toBool(v any) bool {
	switch val := v.(type) {
	case bool:
		return val
	case int:
		return val != 0
	case float64:
		return val != 0
	case string:
		return val == "true" || val == "1"
	default:
		return false
	}
}

// toInt 将 any 转为 int。
func toInt(v any) int {
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	case bool:
		if val {
			return 1
		}
		return 0
	case string:
		// 简单处理，不解析字符串
		return 0
	default:
		return 0
	}
}
