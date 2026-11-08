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

// InlineTaskMemberEnabled 任务级内联成员硬开关（F_118），本版本关闭。
//
// 关闭时 dispatch 模板的 {{inline_member}} 槽位恒为空，leader 不会被告知
// 一个它无法使用的字段（inline 声明 / lifetime / 休眠那整段散文一起消失）。
//
// 真值定义在 schema 包（prompts 包要读它，而 prompts 已依赖 schema），
// 此处转发以保持既有引用面不变。
//
// Python: INLINE_TASK_MEMBER_ENABLED: bool = False (constants.py L36)
const InlineTaskMemberEnabled = schema.InlineTaskMemberEnabled

// ──────────────────────────── 全局变量 ────────────────────────────

// ReservedMemberNames 保留的成员名集合，用户声明成员不得使用。
// 已迁移到 schema 包，此处保留引用以兼容现有调用方。
var ReservedMemberNames = schema.ReservedMemberNames
