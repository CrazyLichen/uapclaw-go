package components

// ──────────────────────────── 枚举 ────────────────────────────

// ComponentAbility 组件能力，表示组件执行支持的 I/O 模式。
// Python: openjiuwen/core/workflow/components/base.py ComponentAbility(Enum)
// 成员为 (name, desc) 元组：name 供事件/trace 使用，desc 为人类可读说明。
type ComponentAbility int

const (
	// ComponentAbilityInvoke 同步批处理（batch in, batch out）
	ComponentAbilityInvoke ComponentAbility = iota
	// ComponentAbilityStream 流式输出（batch in, stream out）
	ComponentAbilityStream
	// ComponentAbilityCollect 聚合输出（stream in, batch out）
	ComponentAbilityCollect
	// ComponentAbilityTransform 流到流（stream in, stream out）
	ComponentAbilityTransform
)

// Name 返回能力内部名。
// Python: ComponentAbility.name 属性
func (a ComponentAbility) Name() string {
	switch a {
	case ComponentAbilityInvoke:
		return "invoke"
	case ComponentAbilityStream:
		return "stream"
	case ComponentAbilityCollect:
		return "collect"
	case ComponentAbilityTransform:
		return "transform"
	default:
		return ""
	}
}

// Desc 返回能力说明。
// Python: ComponentAbility.desc 属性
func (a ComponentAbility) Desc() string {
	switch a {
	case ComponentAbilityInvoke:
		return "batch in, batch out"
	case ComponentAbilityStream:
		return "batch in, stream out"
	case ComponentAbilityCollect:
		return "stream in, batch out"
	case ComponentAbilityTransform:
		return "stream in, stream out"
	default:
		return ""
	}
}
