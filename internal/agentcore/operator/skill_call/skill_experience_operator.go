package skill_call

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillExperienceOperator 技能经验预览参数句柄。
//
// 仅预览的参数句柄，管理技能经验记录。
// 仅拥有 "updates_generated → local_apply_completed" 阶段；
// 暂存审批由 ExperienceManager 负责，持久化由 EvolutionStore 负责。
//
// 对应 Python: openjiuwen/core/operator/skill_call/base.py SkillExperienceOperator
type SkillExperienceOperator struct {
	// skillName 技能名称
	skillName string
	// onParameterUpdated 参数变更回调
	onParameterUpdated operator.ParameterUpdatedCallback
}

// SkillExperienceOperatorOption SkillExperienceOperator 构造选项函数。
type SkillExperienceOperatorOption func(*SkillExperienceOperator)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// experiencesTarget 经验目标名，一比一复刻 Python protocols.EXPERIENCES_TARGET。
	// 对应 Python: EXPERIENCES_TARGET = "experiences"
	experiencesTarget = "experiences"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// SkillCallOperator 是 SkillExperienceOperator 的向后兼容别名。
// 对应 Python: SkillCallOperator = SkillExperienceOperator
type SkillCallOperator = SkillExperienceOperator

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSkillExperienceOperator 创建 SkillExperienceOperator 实例。
//
// 对应 Python: SkillExperienceOperator.__init__(skill_name, on_parameter_updated)
func NewSkillExperienceOperator(skillName string, opts ...SkillExperienceOperatorOption) *SkillExperienceOperator {
	op := &SkillExperienceOperator{
		skillName: skillName,
	}

	for _, opt := range opts {
		opt(op)
	}

	return op
}

// OperatorID 返回操作器标识。
// 格式: "skill_experience_{skill_name}"
//
// 对应 Python: SkillExperienceOperator.operator_id (property)
func (op *SkillExperienceOperator) OperatorID() string {
	return fmt.Sprintf("skill_experience_%s", op.skillName)
}

// GetTunables 获取可调参数。
//
// 对应 Python: SkillExperienceOperator.get_tunables()
func (op *SkillExperienceOperator) GetTunables() map[string]operator.TunableSpec {
	return map[string]operator.TunableSpec{
		experiencesTarget: {
			Name:       experiencesTarget,
			Kind:       operator.TunableKindSkillExperience,
			Path:       "content",
			Constraint: map[string]any{"type": "record"},
		},
	}
}

// SetParameter 设置参数值。
// 仅接受 target="experiences" 且 value 非 nil，通知消费者。
//
// 对应 Python: SkillExperienceOperator.set_parameter(target, value)
func (op *SkillExperienceOperator) SetParameter(target string, value any) {
	if target != experiencesTarget || value == nil {
		return
	}
	items := toSlice(value)
	if op.onParameterUpdated != nil {
		op.onParameterUpdated(target, items)
	}
}

// PreviewUpdate 应用本地预览更新，不进入暂存或持久化。
//
// 仅支持 target="experiences" + effect=pending_change + mode 为 append 或 merge。
// 其他组合返回 applied=False。
//
// 对应 Python: SkillExperienceOperator.preview_update(target, update)
func (op *SkillExperienceOperator) PreviewUpdate(target string, update schema.UpdateValue) schema.ApplyResult {
	if target != experiencesTarget {
		return schema.ApplyResultWithErrors(
			op.OperatorID(), target,
			update.Mode, update.Effect, update.Payload,
			fmt.Sprintf("unsupported target for SkillExperienceOperator: %s", target),
		)
	}

	if update.Effect != schema.UpdateEffectPendingChange ||
		(update.Mode != schema.UpdateModeAppend && update.Mode != schema.UpdateModeMerge) {
		return schema.ApplyResultWithErrors(
			op.OperatorID(), target,
			update.Mode, update.Effect, update.Payload,
			fmt.Sprintf(
				"unsupported update mode/effect for SkillExperienceOperator: %s/%s",
				update.Mode, update.Effect,
			),
		)
	}

	records := toSlice(update.Payload)
	stage := schema.LocalApplyCompleted

	return schema.ApplyResult{
		OperatorID: op.OperatorID(),
		Target:     target,
		Applied:    len(records) > 0,
		Mode:       update.Mode,
		Effect:     update.Effect,
		Value:      update.Payload,
		Records:    records,
		ChangeType: update.ChangeType,
		LifecycleStage: &stage,
		Errors:     []string{},
		Metadata: func() map[string]any {
			m := schema.MetadataClone(update.Metadata)
			m["skill_name"] = op.skillName
			return m
		}(),
	}
}

// GetState 获取当前状态（空操作）。
//
// 对应 Python: SkillExperienceOperator.get_state() → {}
func (op *SkillExperienceOperator) GetState() map[string]any {
	return map[string]any{}
}

// LoadState 从检查点恢复状态（空操作）。
//
// 对应 Python: SkillExperienceOperator.load_state(state) → None
func (op *SkillExperienceOperator) LoadState(_ map[string]any) {
	// 空操作
}

// ApplyUpdate 应用结构化演化更新。
// 重写默认行为，路由到 PreviewUpdate。
//
// 对应 Python: PreviewableOperator.apply_update → self.preview_update
func (op *SkillExperienceOperator) ApplyUpdate(target string, update schema.UpdateValue) schema.ApplyResult {
	return op.PreviewUpdate(target, update)
}

// WithSkillOnParameterUpdated 设置参数变更回调选项。
func WithSkillOnParameterUpdated(cb operator.ParameterUpdatedCallback) SkillExperienceOperatorOption {
	return func(op *SkillExperienceOperator) { op.onParameterUpdated = cb }
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toSlice 将 value 转为 []any。
// 对应 Python: items = value if isinstance(value, list) else [value]
func toSlice(value any) []any {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case []any:
		return v
	default:
		return []any{v}
	}
}
