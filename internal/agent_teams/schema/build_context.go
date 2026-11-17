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

// DefaultBuildContextFromSeed 从种子重建基类 BuildContext（过渡默认工厂）。
//
// 偏差 D-15.13-k：Python 无默认注册，工厂由平台注册（jiuwenswarm
// registry.py:198 _build_swarm_context_from_seed → SwarmBuildContext.from_seed，
// 携带本地 config / trajectory_span_processor 等不可序列化句柄）；Go 平台子类
// 与 provider 注册面（15.16）尚未落地，先注册本基类工厂作为过渡——只恢复
// 基类可序列化字段，平台专属键忽略。平台子类落地后经
// RegisterBuildContextFactory 覆盖注册（后者覆盖前者）。
func DefaultBuildContextFromSeed(seed map[string]any) (BuildContextInterface, error) {
	return &BuildContext{
		Language:       seedString(seed, "language"),
		MemberName:     seedString(seed, "member_name"),
		Role:           seedString(seed, "role"),
		MemberCardID:   seedString(seed, "member_card_id"),
		ProjectDir:     seedString(seed, "project_dir"),
		TeamOutputsDir: seedString(seed, "team_outputs_dir"),
	}, nil
}

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

// seedString 读取种子中的字符串键（缺失/类型不符返回空串）。
func seedString(seed map[string]any, key string) string {
	if v, ok := seed[key].(string); ok {
		return v
	}
	return ""
}

// init 注册过渡默认种子工厂（偏差 D-15.13-k，见 DefaultBuildContextFromSeed）。
// 平台工厂（15.16）落地后经 RegisterBuildContextFactory 覆盖。
func init() {
	RegisterBuildContextFactory(DefaultBuildContextFromSeed)
}
