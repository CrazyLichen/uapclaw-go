package operator

import (
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TunableKind 可调参数类型。
//
// 对应 Python: TunableKind = str（约束靠文档，Go 用常量增强类型安全）
type TunableKind string

const (
	// TunableKindPrompt 提示词类型。
	TunableKindPrompt TunableKind = "prompt"
	// TunableKindContinuous 连续值类型。
	TunableKindContinuous TunableKind = "continuous"
	// TunableKindDiscrete 离散值类型。
	TunableKindDiscrete TunableKind = "discrete"
	// TunableKindToolSelector 工具选择器类型。
	TunableKindToolSelector TunableKind = "tool_selector"
	// TunableKindMemorySelector 记忆选择器类型。
	TunableKindMemorySelector TunableKind = "memory_selector"
	// TunableKindSkillExperience 技能经验类型。
	TunableKindSkillExperience TunableKind = "skill_experience"
	// TunableKindText 文本类型（ToolCallOperator 使用）。
	TunableKindText TunableKind = "text"
)

// TunableSpec 描述单个可调参数。
//
// 对应 Python: openjiuwen/core/operator/base.py TunableSpec
type TunableSpec struct {
	// Name 参数名称
	Name string
	// Kind 可调类型
	Kind TunableKind
	// Path 参数路径
	Path string
	// Constraint 可选约束，形如 {"type": "dict"} 或 {"type": "int", "min": 0, "max": 5}
	Constraint map[string]any
}

// ParameterUpdatedCallback 参数变更回调函数类型。
// 当 Operator 的参数被更新时触发，将变更推送给消费者（Agent/Rail）。
//
// 对应 Python: Callable[[str, Any], None]
type ParameterUpdatedCallback func(target string, value any)

// Operator 自演化参数句柄的基础接口。
//
// Operator 为演化框架提供统一接口：
//   - 通过 OperatorID 标识参数（用于轨迹归因和检查点）
//   - 通过 GetTunables 描述可调参数及其约束
//   - 通过 GetState 读取当前值（用于检查点/回滚）
//   - 通过 SetParameter 更新参数（演化更新入口，检查冻结标记）
//   - 通过 LoadState 从检查点恢复（不检查冻结标记）
//   - 通过 ApplyUpdate 应用结构化更新
//
// 对应 Python: openjiuwen/core/operator/base.py Operator(ABC)
type Operator interface {
	// OperatorID 返回唯一标识符，格式: {agent_id}/{kind}_{name}
	OperatorID() string

	// GetTunables 描述可调参数及其约束。
	// 冻结的参数不应包含在返回结果中。
	GetTunables() map[string]TunableSpec

	// GetState 获取当前参数值，用于检查点/回滚。
	GetState() map[string]any

	// SetParameter 设置参数值（演化更新入口）。
	// 约束：1. 检查目标参数是否冻结（冻结则跳过）2. 更新内部状态 3. 触发 onParameterUpdated 回调
	SetParameter(target string, value any)

	// ApplyUpdate 应用结构化演化更新。
	// 默认行为仅处理 replace/state 模式，具体实现可重写。
	ApplyUpdate(target string, update schema.UpdateValue) schema.ApplyResult

	// LoadState 从检查点恢复状态。
	// 约束：1. 不检查冻结标记（必须恢复完整状态）2. 逐字段更新 3. 每个字段触发回调
	LoadState(state map[string]any)
}

// PreviewableOperator 支持本地预览更新的 Operator 扩展接口。
//
// 预览更新仅产生本地应用结果，审批和持久化由调用方的生命周期管理器负责，
// 而非 Operator 自身。
//
// 对应 Python: openjiuwen/core/operator/base.py PreviewableOperator(Operator)
type PreviewableOperator interface {
	Operator
	// PreviewUpdate 应用本地预览更新，不进入暂存或持久化。
	PreviewUpdate(target string, update schema.UpdateValue) schema.ApplyResult
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// DefaultApplyUpdate 提供兼容 replace/state 的默认更新应用行为。
//
// 仅处理 Mode=replace + Effect=state 的更新，委托给 Operator.SetParameter；
// 其他模式/effect 组合返回 applied=False。
//
// 各具体 Operator 的 ApplyUpdate 方法内部应调用此函数。
// SkillExperienceOperator 除外——它重写 ApplyUpdate 路由到 PreviewUpdate。
//
// 对应 Python: Operator.apply_update 默认实现
func DefaultApplyUpdate(op Operator, target string, update schema.UpdateValue) schema.ApplyResult {
	if update.Mode != schema.UpdateModeReplace || update.Effect != schema.UpdateEffectState {
		return schema.ApplyResultWithErrors(
			op.OperatorID(), target,
			update.Mode, update.Effect, update.Payload,
			"unsupported update mode/effect for compatibility operator: "+string(update.Mode)+"/"+string(update.Effect),
		)
	}

	beforeState := op.GetState()
	op.SetParameter(target, update.Payload)
	afterState := op.GetState()
	applied := !stateEqual(beforeState, afterState)

	return schema.ApplyResult{
		OperatorID: op.OperatorID(),
		Target:     target,
		Applied:    applied,
		Mode:       update.Mode,
		Effect:     update.Effect,
		Value:      update.Payload,
		Records:    []any{},
		Errors:     []string{},
		Metadata:   schema.MetadataClone(update.Metadata),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// stateEqual 比较两个状态 map 是否相等。
func stateEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok {
			return false
		}
		if !valueEqual(va, vb) {
			return false
		}
	}
	return true
}

// valueEqual 递归比较两个 any 值是否相等。
func valueEqual(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	// 尝试 map[string]any 比较
	ma, okA := a.(map[string]any)
	mb, okB := b.(map[string]any)
	if okA && okB {
		return stateEqual(ma, mb)
	}
	// 尝试 map[string]string 比较
	msa, okA := a.(map[string]string)
	msb, okB := b.(map[string]string)
	if okA && okB {
		return mapStrStrEqual(msa, msb)
	}
	// 尝试 []any 比较
	sa, okA := a.([]any)
	sb, okB := b.([]any)
	if okA && okB {
		if len(sa) != len(sb) {
			return false
		}
		for i := range sa {
			if !valueEqual(sa[i], sb[i]) {
				return false
			}
		}
		return true
	}
	return a == b
}

// mapStrStrEqual 比较 map[string]string 是否相等。
func mapStrStrEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok || va != vb {
			return false
		}
	}
	return true
}
