// Package ability 组件能力枚举叶包。
//
// 从 workflow/components/base.go 抽出的破环迁移（偏差 D-17-8，PendingNode 迁
// pregel / AgentsBridge 包环桥接先例同款）：graph/stream_actor 与 workflow 包
// （NodeSpec.Abilities）在 workflow/components 引入 session/graph 依赖后无法
// 再经 components 间接引用该枚举，故将枚举本体沉至本叶包；
// components/base.go 经类型别名/常量别名保持 components.ComponentAbility
// 锚点不变（别名不产生新类型，两处引用同一类型身份）。
//
// 文件目录：
//
//	ability/
//	└── ability.go      # ComponentAbility 枚举与 Name/Desc/AbilityValues
//
// 对应 Python 代码：openjiuwen/core/workflow/components/base.py（ComponentAbility 部分）
package ability

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
