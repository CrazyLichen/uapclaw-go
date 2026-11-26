// Package swarmsnapshot swarm 装配期捕获的工作区/会话上下文快照。
//
// 偏差 D-15.24c-3 的载荷叶包：providers（组装层）与 team（注册层）之间的
// 跨包数据承载。team 不可 import buildctx（buildctx→team 成环）、providers
// 不可 import team（team→swarm→providers 成环），双方共同 import 本叶包，
// 用具体类型替代 any 字段 + 类型断言（2026-11-26 审查轮用户裁决）。
//
// 对应 Python 代码：jiuwenswarm/agents/swarm/providers/evolution_rails.py
// bind_swarm_context 捕获段 + _build_team_workspace_info。
package swarmsnapshot

// ──────────────────────────── 结构体 ────────────────────────────

// Snapshot swarm 装配期捕获的上下文快照（值语义，构建期一次性填充）。
// Python: swarm 子类 bind_swarm_context 的 getattr 捕获集
// （evolution_rails.py:214-244/293-323/369-402）+ _build_team_workspace_info
// （:116-156）的全字段并集。
type Snapshot struct {
	// SessionID 持有 rail 的活跃会话 id
	SessionID string
	// Channel 已解析渠道键（缺省 "default"）
	Channel string
	// TeamID 团队名
	TeamID string
	// Role 团队角色（leader/teammate）
	Role string
	// Language 语言码
	Language string
	// MemberName 成员名
	MemberName string
	// ProjectDir 绑定项目目录（空 = projectless）
	ProjectDir string
	// TeamWsRoot 团队共享工作区根
	TeamWsRoot string
	// SkillsDir 唯一 Skill 库目录
	SkillsDir string
	// Config 已解析 config.yaml
	Config map[string]any
}
