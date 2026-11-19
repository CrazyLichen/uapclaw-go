package components

// ──────────────────────────── 结构体 ────────────────────────────

// WorkflowComponentMetadata 组件元数据
// Python: components/base.py:43-46 (WorkflowComponentMetadata dataclass)
type WorkflowComponentMetadata struct {
	// NodeID 节点标识
	NodeID string
	// NodeType 节点类型
	NodeType string
	// NodeName 节点名称
	NodeName string
}

// ComponentConfig 组件配置
// Python: components/base.py:50-51 (ComponentConfig dataclass)
type ComponentConfig struct {
	// Metadata 组件元数据（可空）
	Metadata *WorkflowComponentMetadata
}

// ComponentState 组件运行状态
// Python: components/base.py:55-56 (ComponentState dataclass)
// 偏差 D-17-7：Python status 字段类型为 Enum 但上游未定义任何成员，
// Go 以 any 承载、零消费留档，待上游定义成员后收窄。
type ComponentState struct {
	// CompID 组件标识
	CompID string
	// Status 状态枚举（Python status: Enum，成员上游未定义，偏差 D-17-7）
	Status any
}

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

// AbilityValues 返回全部能力成员（对齐 Python 迭代 ComponentAbility 枚举，供源键反解）
func AbilityValues() []ComponentAbility {
	return []ComponentAbility{
		ComponentAbilityInvoke,
		ComponentAbilityStream,
		ComponentAbilityCollect,
		ComponentAbilityTransform,
	}
}
