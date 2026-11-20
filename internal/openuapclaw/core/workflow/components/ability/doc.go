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
