package database

import (
	"context"

	"gorm.io/gorm"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SQLMemberDao MemberDao 的 SQL 实现。
// 对齐 Python: MemberDao (openjiuwen/agent_teams/tools/database/member_dao.py)
// 操作静态表 team_member。
type SQLMemberDao struct {
	// db GORM 数据库实例
	db *gorm.DB
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// withTx 返回绑定指定事务的 DAO 实例。
func (d *SQLMemberDao) withTx(tx *gorm.DB) *SQLMemberDao {
	return &SQLMemberDao{db: tx}
}

// CreateMember 创建成员。返回 true 表示成功，false 表示成员已存在。
// 对齐 Python: create_member() → bool（IntegrityError → False）
func (d *SQLMemberDao) CreateMember(ctx context.Context, memberName, teamName, displayName, agentCard, status, role, desc, executionStatus, mode, prompt, modelRefJSON string) bool {
	now := GetCurrentTime()
	member := &TeamMember{
		MemberName:      memberName,
		TeamName:        teamName,
		DisplayName:     displayName,
		AgentCard:       agentCard,
		Status:          status,
		Role:            role,
		Desc:            desc,
		ExecutionStatus: executionStatus,
		Mode:            mode,
		Prompt:          prompt,
		ModelRefJSON:    modelRefJSON,
		UpdatedAt:       now,
	}
	result := d.db.WithContext(ctx).Create(member)
	if result.Error != nil {
		logger.Error(logComponent).Str("member_name", memberName).Str("team_name", teamName).Err(result.Error).Msg("成员已存在")
		return false
	}
	return true
}

// GetMember 获取成员信息。返回 nil 表示不存在。
// 对齐 Python: get_member() → Optional[TeamMember]
func (d *SQLMemberDao) GetMember(ctx context.Context, memberName, teamName string) (*TeamMember, error) {
	var member TeamMember
	result := d.db.WithContext(ctx).Table("team_member").
		Where("member_name = ? AND team_name = ?", memberName, teamName).First(&member)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, result.Error
	}
	return &member, nil
}

// GetTeamMembers 获取团队成员列表，可选按 status 过滤。
// 对齐 Python: get_team_members(team, status=None)
func (d *SQLMemberDao) GetTeamMembers(ctx context.Context, teamName string, status string) ([]*TeamMember, error) {
	query := d.db.WithContext(ctx).Table("team_member").Where("team_name = ?", teamName)
	if status != "" {
		query = query.Where("status = ?", status)
	}
	var members []*TeamMember
	result := query.Find(&members)
	if result.Error != nil {
		return nil, result.Error
	}
	return members, nil
}

// UpdateMemberStatus 更新成员状态（含 FSM 校验）。
// 对齐 Python: update_member_status() → bool
func (d *SQLMemberDao) UpdateMemberStatus(ctx context.Context, memberName, teamName, status string) bool {
	// 对齐 Python: 先查再改 + is_valid_transition 校验
	var member TeamMember
	result := d.db.WithContext(ctx).Table("team_member").
		Where("member_name = ? AND team_name = ?", memberName, teamName).First(&member)
	if result.Error != nil {
		return false
	}
	if !IsValidMemberTransition(member.Status, status) {
		// 对齐 Python: team_logger.error("Invalid state transition for member %s: %s -> %s", ...)
		logger.Error(logComponent).
			Str("member_name", memberName).
			Str("from", member.Status).
			Str("to", status).
			Msg("成员状态转换不合法")
		return false
	}
	d.db.WithContext(ctx).Table("team_member").
		Where("member_name = ? AND team_name = ?", memberName, teamName).
		Update("status", status)
	return true
}

// TryTransitionMemberStatus CAS 原子状态转换。
// 对齐 Python: try_transition_member_status() → bool
// 仅当当前状态 == fromStatus 时才更新为 toStatus，否则返回 false。
// 使用单条 UPDATE WHERE status = from_status + rowcount 判断。
func (d *SQLMemberDao) TryTransitionMemberStatus(ctx context.Context, memberName, teamName, fromStatus, toStatus string) bool {
	result := d.db.WithContext(ctx).Table("team_member").
		Where("member_name = ? AND team_name = ? AND status = ?", memberName, teamName, fromStatus).
		Update("status", toStatus)
	return result.RowsAffected == 1
}

// ListHumanAgentNames 获取 human_agent 角色的成员名列表（HITT 名册重建）。
// 对齐 Python: list_human_agent_names() → List[str]
func (d *SQLMemberDao) ListHumanAgentNames(ctx context.Context, teamName string) ([]string, error) {
	var names []string
	result := d.db.WithContext(ctx).Table("team_member").
		Select("member_name").
		Where("team_name = ? AND role = ?", teamName, "human_agent").
		Find(&names)
	if result.Error != nil {
		return nil, result.Error
	}
	return names, nil
}

// GetMembersMaxUpdatedAt 获取 MAX(updated_at)（成员变更检测）。
// 对齐 Python: get_members_max_updated_at() → int
func (d *SQLMemberDao) GetMembersMaxUpdatedAt(ctx context.Context, teamName string) int64 {
	var maxVal *int64
	row := d.db.WithContext(ctx).Table("team_member").
		Select("MAX(updated_at)").
		Where("team_name = ?", teamName).
		Row()
	if err := row.Scan(&maxVal); err != nil {
		return 0
	}
	if maxVal == nil {
		return 0
	}
	return *maxVal
}

// UpdateMemberExecutionStatus 更新执行状态（含 FSM 校验）。
// 对齐 Python: update_member_execution_status() → bool
func (d *SQLMemberDao) UpdateMemberExecutionStatus(ctx context.Context, memberName, teamName, executionStatus string) bool {
	var member TeamMember
	result := d.db.WithContext(ctx).Table("team_member").
		Where("member_name = ? AND team_name = ?", memberName, teamName).First(&member)
	if result.Error != nil {
		return false
	}
	if !IsValidExecutionTransition(member.ExecutionStatus, executionStatus) {
		logger.Error(logComponent).
			Str("member_name", memberName).
			Str("from", member.ExecutionStatus).
			Str("to", executionStatus).
			Msg("执行状态转换不合法")
		return false
	}
	d.db.WithContext(ctx).Table("team_member").
		Where("member_name = ? AND team_name = ?", memberName, teamName).
		Update("execution_status", executionStatus)
	return true
}
