// schema.go 提供 member 优化候选角色的数据契约（17.4b1 切片）。
// Python: openjiuwen/rsi/harness_rsi/member_optimizer/schema.py 的
// MemberRoleCandidate（evaluation_result_analyzer 经 loader 依赖的唯一类型）。
// schema.py 其余类型（RoleIssueAttribution/UnassignedMemberIssue 等）⤴️ 17.4c。

package memberoptimizer

// ──────────────────────────── 结构体 ────────────────────────────

// MemberRoleCandidate 从 harness refs 解析出的 member 优化候选角色。
// Python: schema.py:73-81 MemberRoleCandidate（frozen dataclass → Go struct，
// 偏差 D-5 见 17.4b1 spec §6）。
type MemberRoleCandidate struct {
	// Role 角色名
	Role string
	// HarnessRefPath harness 引用文件路径
	HarnessRefPath string
	// MemberName 成员名（可空，默认与 role 相同由解析方填）
	MemberName string
	// Description 描述
	Description string
	// Metadata 元数据
	Metadata map[string]any
}
