// Package schema 团队侧 schema 类型集合。
//
// 本文件是 core 侧 BuildContext 的薄转发，对齐上游
// openjiuwen/agent_teams/schema/build_context.py（25 行）的定位：
// 「BuildContext 与工厂助手现在住在 harness 层的 schema 包，是 harness 级
// 的真理来源；本模块保留为薄转发，让既有团队 import 路径继续工作，
// 并指向同一对象（is-identical）」。
//
// 对应 Python 代码：openjiuwen/agent_teams/schema/build_context.py
package schema

import (
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// ──────────────────────────── 类型别名 ────────────────────────────

// BuildContext core 侧构建上下文的团队侧别名（同一类型，非副本）。
type BuildContext = hschema.BuildContext

// BuildContextInterface core 侧构建上下文接口的团队侧别名。
type BuildContextInterface = hschema.BuildContextInterface

// BuildContextOverrides core 侧覆盖项的团队侧别名。
type BuildContextOverrides = hschema.BuildContextOverrides

// BuildContextFactory core 侧种子工厂类型的团队侧别名。
type BuildContextFactory = hschema.BuildContextFactory

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterBuildContextFactory 转发 core 侧种子工厂注册。
func RegisterBuildContextFactory(f hschema.BuildContextFactory) {
	hschema.RegisterBuildContextFactory(f)
}

// BuildContextFromSeed 转发 core 侧种子重建。
func BuildContextFromSeed(seed map[string]any) (hschema.BuildContextInterface, error) {
	return hschema.BuildContextFromSeed(seed)
}

// ParentSysOperation 转发 core 侧的父 SysOperation 读取。
func ParentSysOperation(context hschema.BuildContextInterface) any {
	return hschema.ParentSysOperation(context)
}
