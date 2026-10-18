package agent

import (
	"context"
	"errors"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamMember 团队成员状态管理。
// Python: TeamMember (openjiuwen/agent_teams/agent/member.py)
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
	DB database.TeamDatabase
	// Messager 消息总线实例
	Messager messagerBus
	// Prompt 启动提示
	Prompt string
	// Desc 人设描述
	Desc string
}

// messagerBus TeamMember 发布事件所需的消息总线窄接口。
// 签名对齐 messager.Messager 具体接口的 Publish 方法。
type messagerBus interface {
	// Publish 向主题发布事件消息
	Publish(ctx context.Context, topicID string, message *events.EventMessage) error
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识
const logComponent = logger.ComponentTeam

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ErrMemberNotRegistered 成员行未注册哨兵错误。
	// S-34: 对齐 Python status() 返回 None 语义——调用方需显式处理"未注册"状态
	ErrMemberNotRegistered = errors.New("member not registered")
)

// ──────────────────────────── 导出函数 ────────────────────────────

// Status 获取当前成员状态。
// Python: TeamMember.status() → MemberStatus(member_data.status) if member_data else None
// S-34: 从 DB 读取真实状态；成员行不存在时返回 ErrMemberNotRegistered
func (m *TeamMember) Status(ctx context.Context) (atschema.MemberStatus, error) {
	if m.DB == nil {
		return atschema.MemberStatusReady, ErrMemberNotRegistered
	}
	member, err := m.DB.Member().GetMember(ctx, m.MemberName, m.TeamName)
	if err != nil {
		return atschema.MemberStatusReady, err
	}
	if member == nil {
		return atschema.MemberStatusReady, ErrMemberNotRegistered
	}
	return atschema.MemberStatus(member.Status), nil
}

// ExecutionStatus 获取当前执行状态。
// Python: TeamMember.execution_status() → ExecutionStatus(member_data.execution_status) if member_data else None
// S-34: 从 DB 读取真实状态；成员行不存在时返回 ErrMemberNotRegistered
func (m *TeamMember) ExecutionStatus(ctx context.Context) (atschema.ExecutionStatus, error) {
	if m.DB == nil {
		return atschema.ExecutionStatusIdle, ErrMemberNotRegistered
	}
	member, err := m.DB.Member().GetMember(ctx, m.MemberName, m.TeamName)
	if err != nil {
		return atschema.ExecutionStatusIdle, err
	}
	if member == nil {
		return atschema.ExecutionStatusIdle, ErrMemberNotRegistered
	}
	return atschema.ExecutionStatus(member.ExecutionStatus), nil
}

// UpdateStatus 更新成员状态（带校验）。
// Python: TeamMember.update_status(new_status) → bool
//
// 等值短路（跳过 DB 写入和状态变更事件）；成员行未注册时静默返回 false
// （Leader 的行在 BuildTeamTool 后才物化）；写 DB 含 FSM 校验；
// 成功后发布 MemberStatusChangedEvent（发布失败仅 Error 日志，不影响返回值）。
func (m *TeamMember) UpdateStatus(ctx context.Context, newStatus atschema.MemberStatus) (bool, error) {
	if m.DB == nil {
		logger.Debug(logComponent).Str("member_name", m.MemberName).
			Str("new_status", string(newStatus)).
			Msg("member not registered yet; skipping status update")
		return false, nil
	}
	// 对齐 Python: old_status = await self.status(); if old_status is None: return False（静默）
	member, err := m.DB.Member().GetMember(ctx, m.MemberName, m.TeamName)
	if err != nil {
		return false, err
	}
	if member == nil {
		logger.Debug(logComponent).Str("member_name", m.MemberName).
			Str("new_status", string(newStatus)).
			Msg("member not registered yet; skipping status update")
		return false, nil
	}
	oldStatus := atschema.MemberStatus(member.Status)
	if oldStatus == newStatus {
		// 对齐 Python: 等值短路——无事件无写库，幂等重申状态
		return true, nil
	}
	success := m.DB.Member().UpdateMemberStatus(ctx, m.MemberName, m.TeamName, string(newStatus))
	if !success {
		logger.Error(logComponent).Str("member_name", m.MemberName).
			Str("new_status", string(newStatus)).
			Msg("failed to update member status")
		return false, nil
	}
	logger.Debug(logComponent).Str("member_name", m.MemberName).
		Str("old_status", string(oldStatus)).
		Str("new_status", string(newStatus)).
		Msg("member status updated")
	// 发布 member_status_changed 事件（发布失败仅 Error 日志，对齐 Python except 只打日志）
	m.publishMemberEvent(ctx, &events.MemberStatusChangedEvent{
		BaseEventMessage: events.BaseEventMessage{
			TeamName:   m.TeamName,
			MemberName: m.MemberName,
		},
		OldStatus: string(oldStatus),
		NewStatus: string(newStatus),
	}, events.TeamTopicTeam)
	return true, nil
}

// UpdateExecutionStatus 更新执行状态（带校验）。
// Python: TeamMember.update_execution_status(new_status) → bool
//
// 成员行未注册时静默返回 false（同 UpdateStatus 语义）；成功后发布
// MemberExecutionChangedEvent（发布失败仅 Error 日志）。
func (m *TeamMember) UpdateExecutionStatus(ctx context.Context, newStatus atschema.ExecutionStatus) (bool, error) {
	if m.DB == nil {
		logger.Debug(logComponent).Str("member_name", m.MemberName).
			Str("new_status", string(newStatus)).
			Msg("member not registered yet; skipping execution status update")
		return false, nil
	}
	// 对齐 Python: old_status = await self.execution_status(); if old_status is None: return False（静默）
	member, err := m.DB.Member().GetMember(ctx, m.MemberName, m.TeamName)
	if err != nil {
		return false, err
	}
	if member == nil {
		logger.Debug(logComponent).Str("member_name", m.MemberName).
			Str("new_status", string(newStatus)).
			Msg("member not registered yet; skipping execution status update")
		return false, nil
	}
	oldStatus := atschema.ExecutionStatus(member.ExecutionStatus)
	success := m.DB.Member().UpdateMemberExecutionStatus(ctx, m.MemberName, m.TeamName, string(newStatus))
	if !success {
		logger.Error(logComponent).Str("member_name", m.MemberName).
			Str("new_status", string(newStatus)).
			Msg("failed to update member execution status")
		return false, nil
	}
	logger.Debug(logComponent).Str("member_name", m.MemberName).
		Str("old_status", string(oldStatus)).
		Str("new_status", string(newStatus)).
		Msg("member execution status updated")
	// 发布 member_execution_changed 事件（发布失败仅 Error 日志）
	m.publishMemberEvent(ctx, &events.MemberExecutionChangedEvent{
		BaseEventMessage: events.BaseEventMessage{
			TeamName:   m.TeamName,
			MemberName: m.MemberName,
		},
		OldStatus: string(oldStatus),
		NewStatus: string(newStatus),
	}, events.TeamTopicTeam)
	return true, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// publishMemberEvent 发布成员状态事件到 TEAM 主题。
// 对齐 Python: messager.publish(TeamTopic.TEAM.build(get_session_id(), team_name), ...)
// 发布失败仅 Error 日志（对齐 Python except 只打日志不上抛）。
func (m *TeamMember) publishMemberEvent(ctx context.Context, event events.TypedEvent, _ events.TeamTopic) {
	if m.Messager == nil {
		return
	}
	msg := events.EventMessageFromEvent(event)
	topicID := events.TeamTopicTeam.Build(agentteams.GetSessionID(ctx), m.TeamName)
	if err := m.Messager.Publish(ctx, topicID, msg); err != nil {
		logger.Error(logComponent).Err(err).
			Str("member_name", m.MemberName).
			Str("event_type", event.EventTypeName()).
			Msg("failed to publish member status event")
	}
}
