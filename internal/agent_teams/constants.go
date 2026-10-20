package agent_teams

import "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// HumanAgentMemberName 保留的 Human-in-the-Team 成员名。
// 已迁移到 schema 包，此处保留常量以兼容现有调用方。
const HumanAgentMemberName = schema.HumanAgentMemberName

// UserPseudoMemberName 伪成员名，代表外部调用者（非团队成员）。
// 已迁移到 schema 包，此处保留常量以兼容现有调用方。
const UserPseudoMemberName = schema.UserPseudoMemberName

// DefaultLeaderMemberName 默认 Leader 成员名。
// 已迁移到 schema 包，此处保留常量以兼容现有调用方。
const DefaultLeaderMemberName = schema.DefaultLeaderMemberName

// ──────────────────────────── 全局变量 ────────────────────────────

// ReservedMemberNames 保留的成员名集合，用户声明成员不得使用。
// 已迁移到 schema 包，此处保留引用以兼容现有调用方。
var ReservedMemberNames = schema.ReservedMemberNames
