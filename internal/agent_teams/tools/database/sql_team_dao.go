package database

import (
	"context"

	"gorm.io/gorm"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SQLTeamDao TeamDao 的 SQL 实现。
// 对齐 Python: TeamDao (openjiuwen/agent_teams/tools/database/team_dao.py)
// 操作静态表 team_info。
type SQLTeamDao struct {
	// db GORM 数据库实例
	db *gorm.DB
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// withTx 返回绑定指定事务的 DAO 实例。
func (d *SQLTeamDao) withTx(tx *gorm.DB) *SQLTeamDao {
	return &SQLTeamDao{db: tx}
}

// CreateTeam 创建团队。返回 true 表示成功，false 表示团队已存在。
// 对齐 Python: create_team() → bool（IntegrityError → False）
func (d *SQLTeamDao) CreateTeam(ctx context.Context, teamName, displayName, leaderMemberName, desc, prompt string) bool {
	// 对齐 Python: ts = get_current_time()
	now := GetCurrentTime()
	team := &Team{
		TeamName:         teamName,
		DisplayName:      displayName,
		LeaderMemberName: leaderMemberName,
		Desc:             desc,
		Prompt:           prompt,
		Created:          now,
		UpdatedAt:        now,
	}
	result := d.db.WithContext(ctx).Create(team)
	if result.Error != nil {
		// 对齐 Python: except IntegrityError → False
		logger.Error(logComponent).Str("team_name", teamName).Err(result.Error).Msg("团队已存在")
		return false
	}
	// 对齐 Python: team_logger.info("Team %s created", team_name)
	logger.Info(logComponent).Str("team_name", teamName).Msg("团队创建成功")
	return true
}

// GetTeam 获取团队信息。返回 nil 表示团队不存在。
// 对齐 Python: get_team() → Optional[Team]
func (d *SQLTeamDao) GetTeam(ctx context.Context, teamName string) (*Team, error) {
	var team Team
	result := d.db.WithContext(ctx).Table("team_info").Where("team_name = ?", teamName).First(&team)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, result.Error
	}
	return &team, nil
}

// TeamExists 团队是否存在。对齐 Python: team_exists() → bool
func (d *SQLTeamDao) TeamExists(ctx context.Context, teamName string) bool {
	var count int64
	d.db.WithContext(ctx).Table("team_info").Where("team_name = ?", teamName).Count(&count)
	return count > 0
}

// DeleteTeam 删除团队（级联删成员）。返回 true 表示成功。
// 对齐 Python: delete_team() → bool（ORM cascade 删成员）
func (d *SQLTeamDao) DeleteTeam(ctx context.Context, teamName string) bool {
	// 静态表无 FK 级联，手动删成员
	d.db.WithContext(ctx).Table("team_member").Where("team_name = ?", teamName).Delete(nil)
	result := d.db.WithContext(ctx).Table("team_info").Where("team_name = ?", teamName).Delete(nil)
	return result.RowsAffected > 0
}

// GetTeamUpdatedAt 获取团队 updated_at 时间戳。
// 对齐 Python: get_team_updated_at() → int
func (d *SQLTeamDao) GetTeamUpdatedAt(ctx context.Context, teamName string) int64 {
	var team Team
	result := d.db.WithContext(ctx).Table("team_info").Select("updated_at").Where("team_name = ?", teamName).First(&team)
	if result.Error != nil {
		return 0
	}
	return team.UpdatedAt
}
