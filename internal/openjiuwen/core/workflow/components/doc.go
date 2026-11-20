// Package components 提供工作流组件的共享类型与基类，对齐 openjiuwen/core/workflow/components 包。
//
// 含 ComponentAbility 枚举（本体在 ability 叶包，别名锚点，偏差 D-17-8）、
// 组件元数据三符号（8.17 补齐）、WorkflowComponent 组件基类（四钩子适配 +
// 会话校验 + 回调 Trigger 序列，8.17）。
//
// 文件目录：
//
//	components/
//	├── doc.go           # 包文档
//	├── base.go          # ComponentAbility 别名锚点 + 组件元数据三符号
//	├── component.go           # WorkflowComponent 双面合成体：四钩子 + ComponentComposable 接口/BaseComponentComposable 组合面基座（8.14 修订 D-17-1）
//	├── ability/         # ComponentAbility 枚举叶包（破环迁移本体，D-17-8）
//	└── flow/           # 流程组件子目录（Start/End，对应 Python components/flow/）
//
// 对应 Python 代码：openjiuwen/core/workflow/components/{base.py,component.py}
package components
