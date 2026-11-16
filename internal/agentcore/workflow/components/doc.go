// Package components 提供工作流组件的共享类型定义，对齐 openjiuwen/core/workflow/components 包。
//
// 当前仅含 ComponentAbility 枚举（8.10 前置件，8.11 Vertex 的事件 ability 字段
// 与组件分派消费）。同 Python 文件中的 WorkflowComponentMetadata/ComponentConfig/
// ComponentState 属 8.17 段范围，届时随 StartComp/EndComp 一并落地，不在本包提前实现。
//
// 文件目录：
//
//	components/
//	├── doc.go      # 包文档
//	└── base.go    # ComponentAbility 枚举
//
// 对应 Python 代码：openjiuwen/core/workflow/components/base.py
package components
