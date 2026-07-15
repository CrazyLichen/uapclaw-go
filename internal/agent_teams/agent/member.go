package agent

import (
	"context"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamMember 团队成员状态管理。
// 对齐 Python: TeamMember (openjiuwen/agent_teams/agent/member.py)
type TeamMember struct {
	// MemberName 成员唯一标识（语义 slug）
	MemberName string
	// TeamName 团队标识
	TeamName string
	// DisplayName 人可读显示标签
	DisplayName string
	// AgentCard Agent 身份卡片
	AgentCard *agentschema.AgentCard
	// DB 团队数据库实例
	// TODO(#9.65): TeamDatabase 类型
	DB any
	// Messager 消息总线实例
	// TODO(#9.65): Messager 类型
	Messager any
	// Prompt 启动提示
	Prompt string
	// Desc 人设描述
	Desc string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识
const logComponent = logger.ComponentCommon

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// Status 获取当前成员状态。
// 对齐 Python: TeamMember.status()
func (m *TeamMember) Status(ctx context.Context) (atschema.MemberStatus, error) {
	// TODO(#9.65): 从 DB 读取成员状态
	return atschema.MemberStatusReady, nil
}

// ExecutionStatus 获取当前执行状态。
// 对齐 Python: TeamMember.execution_status()
func (m *TeamMember) ExecutionStatus(ctx context.Context) (atschema.ExecutionStatus, error) {
	// TODO(#9.65): 从 DB 读取执行状态
	return atschema.ExecutionStatusIdle, nil
}

// UpdateStatus 更新成员状态（带校验）。
// 对齐 Python: TeamMember.update_status(new_status)
//
// 当新状态等于当前状态时为空操作（跳过 DB 写入和状态变更事件）。
// 成员行未注册时静默返回 false（Leader 的行在 BuildTeamTool 后才物化）。
func (m *TeamMember) UpdateStatus(ctx context.Context, newStatus atschema.MemberStatus) (bool, error) {
	// TODO(#9.65): 读取旧状态 → 短路等值 → 写 DB → 发 MemberStatusChangedEvent
	logger.Info(logComponent).Str("member_name", m.MemberName).
		Str("new_status", string(newStatus)).Msg("TeamMember.UpdateStatus")
	return true, nil
}

// UpdateExecutionStatus 更新执行状态（带校验）。
// 对齐 Python: TeamMember.update_execution_status(new_status)
//
// 成员行未注册时静默返回 false（同 UpdateStatus 语义）。
func (m *TeamMember) UpdateExecutionStatus(ctx context.Context, newStatus atschema.ExecutionStatus) (bool, error) {
	// TODO(#9.65): 读取旧状态 → 写 DB → 发 MemberExecutionChangedEvent
	logger.Info(logComponent).Str("member_name", m.MemberName).
		Str("new_status", string(newStatus)).Msg("TeamMember.UpdateExecutionStatus")
	return true, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
