package components

import "github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components/ability"

// ComponentAbility 组件能力枚举：本体在 ability 叶包（破环迁移，偏差 D-17-8），
// 此处别名保持 components.ComponentAbility 锚点（别名不产生新类型，
// 与 ability.ComponentAbility 为同一类型身份）。
// Python: openjiuwen/core/workflow/components/base.py ComponentAbility(Enum)
type ComponentAbility = ability.ComponentAbility

// 能力成员常量别名（本体见 ability 包）
const (
	// ComponentAbilityInvoke 同步批处理（batch in, batch out）
	ComponentAbilityInvoke = ability.ComponentAbilityInvoke
	// ComponentAbilityStream 流式输出（batch in, stream out）
	ComponentAbilityStream = ability.ComponentAbilityStream
	// ComponentAbilityCollect 聚合输出（stream in, batch out）
	ComponentAbilityCollect = ability.ComponentAbilityCollect
	// ComponentAbilityTransform 流到流（stream in, stream out）
	ComponentAbilityTransform = ability.ComponentAbilityTransform
)

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

// ──────────────────────────── 导出函数 ────────────────────────────

// AbilityValues 返回全部能力成员（委托 ability 叶包，锚点保持）
func AbilityValues() []ComponentAbility {
	return ability.AbilityValues()
}
