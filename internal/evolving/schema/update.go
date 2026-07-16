package schema

import (
	"maps"
)

// ──────────────────────────── 结构体 ────────────────────────────

// UpdateKey 更新键，标识 (operatorID, target) 二元组。
// [0] 为 operatorID，[1] 为 target。
//
// 对应 Python: UpdateKey = Tuple[str, str] (agent_evolving/trajectory/types.py)
type UpdateKey [2]string

// OperatorID 返回 UpdateKey 的 operatorID 部分。
func (k UpdateKey) OperatorID() string { return k[0] }

// Target 返回 UpdateKey 的 target 部分。
func (k UpdateKey) Target() string { return k[1] }

// UpdateMode 更新模式。
//
// 对应 Python: UpdateMode = Literal["replace", "append", "merge"]
type UpdateMode string

const (
	// UpdateModeReplace 替换模式。
	UpdateModeReplace UpdateMode = ReplaceMode // "replace"
	// UpdateModeAppend 追加模式。
	UpdateModeAppend UpdateMode = AppendMode // "append"
	// UpdateModeMerge 合并模式。
	UpdateModeMerge UpdateMode = MergeMode // "merge"
)

// UpdateEffect 更新效果。
//
// 对应 Python: UpdateEffect = Literal["state", "pending_change"]
type UpdateEffect string

const (
	// UpdateEffectState 直接状态更新效果。
	UpdateEffectState UpdateEffect = StateEffect // "state"
	// UpdateEffectPendingChange 暂存变更效果。
	UpdateEffectPendingChange UpdateEffect = PendingChangeEffect // "pending_change"
)

// UpdateValue 结构化更新契约，在线和离线应用路径共享。
//
// 对应 Python: openjiuwen/agent_evolving/types.py UpdateValue
//
// 注意：Go 的 struct 零值中 Mode 和 Effect 为空字符串，
// 请使用 NewUpdateValue 构造以确保默认值正确。
type UpdateValue struct {
	// Payload 更新载荷
	Payload any
	// Mode 更新模式（默认 "replace"）
	Mode UpdateMode
	// Effect 更新效果（默认 "state"）
	Effect UpdateEffect
	// ChangeType 变更类型
	ChangeType *string
	// Metadata 扩展元数据
	Metadata map[string]any
}

// NewUpdateValue 创建 UpdateValue 实例，设置默认 Mode=replace, Effect=state。
//
// 对应 Python: UpdateValue(payload, mode=REPLACE_MODE, effect=STATE_EFFECT, ...)
func NewUpdateValue(payload any, opts ...UpdateValueOption) UpdateValue {
	uv := UpdateValue{
		Payload:  payload,
		Mode:     UpdateModeReplace,
		Effect:   UpdateEffectState,
		Metadata: map[string]any{},
	}
	for _, opt := range opts {
		opt(&uv)
	}
	return uv
}

// UpdateValueOption UpdateValue 构造选项函数。
type UpdateValueOption func(*UpdateValue)

// WithUpdateMode 设置更新模式选项。
func WithUpdateMode(mode UpdateMode) UpdateValueOption {
	return func(uv *UpdateValue) { uv.Mode = mode }
}

// WithUpdateEffect 设置更新效果选项。
func WithUpdateEffect(effect UpdateEffect) UpdateValueOption {
	return func(uv *UpdateValue) { uv.Effect = effect }
}

// WithChangeType 设置变更类型选项。
func WithChangeType(changeType string) UpdateValueOption {
	return func(uv *UpdateValue) { uv.ChangeType = &changeType }
}

// WithUpdateMetadata 设置扩展元数据选项。
func WithUpdateMetadata(metadata map[string]any) UpdateValueOption {
	return func(uv *UpdateValue) {
		if metadata != nil {
			uv.Metadata = metadata
		}
	}
}

// ApplyResult 单个归一化更新应用到一个演化目标的结果。
//
// 对应 Python: openjiuwen/agent_evolving/types.py ApplyResult
type ApplyResult struct {
	// OperatorID 操作器标识
	OperatorID string
	// Target 目标参数名
	Target string
	// Applied 是否已应用
	Applied bool
	// Mode 更新模式
	Mode UpdateMode
	// Effect 更新效果
	Effect UpdateEffect
	// Value 更新值
	Value any
	// Records 应用产生的记录列表
	Records []any
	// ChangeType 变更类型
	ChangeType *string
	// LifecycleStage 生命周期阶段（"local_apply_completed" 或 nil）
	LifecycleStage *string
	// PendingChangeID 暂存变更标识
	PendingChangeID *string
	// Errors 错误列表
	Errors []string
	// Metadata 扩展元数据
	Metadata map[string]any
}

// Ok 返回应用结果是否成功（已应用且无错误）。
//
// 对应 Python: ApplyResult.ok property
func (r ApplyResult) Ok() bool {
	return r.Applied && len(r.Errors) == 0
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeUpdateValue 将遗留更新包装为结构化契约。
//
// 兼容规则：
//   - 已是 UpdateValue → 直接返回
//   - target 为 "experiences" → 追加 + 暂存变更
//   - 其他 → 替换 + 状态
//
// 对应 Python: normalize_update_value(value, *, target)
func NormalizeUpdateValue(value any, target string) UpdateValue {
	if uv, ok := value.(UpdateValue); ok {
		return uv
	}
	if target == ExperiencesTarget {
		return UpdateValue{
			Payload:    value,
			Mode:       UpdateModeAppend,
			Effect:     UpdateEffectPendingChange,
			ChangeType: strPtr(SkillExperienceEntry),
			Metadata:   map[string]any{"change_type": SkillExperienceEntry},
		}
	}
	return NewUpdateValue(value)
}

// NormalizeUpdates 将混合的遗留/结构化更新归一化为 UpdateValue 映射。
//
// 对应 Python: normalize_updates(updates)
func NormalizeUpdates(updates map[UpdateKey]any) map[UpdateKey]UpdateValue {
	result := make(map[UpdateKey]UpdateValue, len(updates))
	for key, value := range updates {
		result[key] = NormalizeUpdateValue(value, key.Target())
	}
	return result
}

// NewApplyResult 创建 ApplyResult 的辅助函数。
func NewApplyResult(operatorID, target string, applied bool, mode UpdateMode, effect UpdateEffect, value any) ApplyResult {
	return ApplyResult{
		OperatorID: operatorID,
		Target:     target,
		Applied:    applied,
		Mode:       mode,
		Effect:     effect,
		Value:      value,
		Records:    []any{},
		Errors:     []string{},
		Metadata:   map[string]any{},
	}
}

// ApplyResultWithErrors 创建带错误的 ApplyResult。
func ApplyResultWithErrors(operatorID, target string, mode UpdateMode, effect UpdateEffect, value any, errs ...string) ApplyResult {
	return ApplyResult{
		OperatorID: operatorID,
		Target:     target,
		Applied:    false,
		Mode:       mode,
		Effect:     effect,
		Value:      value,
		Records:    []any{},
		Errors:     errs,
		Metadata:   map[string]any{},
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// strPtr 返回字符串指针。
func strPtr(s string) *string { return &s }

// MetadataClone 克隆 metadata map。
func MetadataClone(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return maps.Clone(m)
}
