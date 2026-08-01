package tools

import "time"

// ──────────────────────────── 结构体 ────────────────────────────

// TeamInfo 团队信息模型。⤵️ 回填: 9.65a
type TeamInfo struct {
	// TeamName 团队名称（主键）
	TeamName string
	// DisplayName 显示名称
	DisplayName string
	// LeaderMemberName Leader 成员名
	LeaderMemberName string
	// Desc 团队描述
	Desc string
	// Created 创建时间
	Created time.Time
	// UpdatedAt 更新时间
	UpdatedAt time.Time
}

// TeamMemberInfo 团队成员模型。⤵️ 回填: 9.65a
type TeamMemberInfo struct {
	// MemberName 成员名称（主键）
	MemberName string
	// TeamName 团队名称（主键）
	TeamName string
	// DisplayName 显示名称
	DisplayName string
	// Desc 成员描述
	Desc string
	// AgentCard Agent 卡片 JSON
	AgentCard string
	// Status 成员状态
	Status string
	// Role 角色
	Role string
	// ModelRefJSON 模型引用 JSON
	ModelRefJSON string
	// UpdatedAt 更新时间
	UpdatedAt time.Time
}
