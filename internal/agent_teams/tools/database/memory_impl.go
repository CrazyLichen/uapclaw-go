package database

import (
	"context"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InMemoryTeamDatabase 内存数据库替代实现。
// 对齐 Python: InMemoryTeamDatabase (openjiuwen/agent_teams/tools/memory_database.py)
//
// 单体结构体同时实现 TeamDatabase + TeamDao + MemberDao 接口，
// 对齐 Python 的 self.team = self / self.member = self 自引用设计。
// TaskDao 和 MessageDao 接口方法由 ⤵️ 9.65a-2/9.65a-3 回填。
type InMemoryTeamDatabase struct {
	// teams 团队数据，key=teamName
	teams map[string]*Team
	// members 成员数据，key=memberName+"\x00"+teamName（复合主键编码）
	members map[string]*TeamMember
	// initialized 是否已初始化
	initialized bool
	// mu 保护并发访问
	mu sync.Mutex
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInMemoryTeamDatabase 创建内存数据库实例。
func NewInMemoryTeamDatabase() *InMemoryTeamDatabase {
	return &InMemoryTeamDatabase{
		teams:   make(map[string]*Team),
		members: make(map[string]*TeamMember),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// memberKey 构造复合主键 key。
func memberKey(memberName, teamName string) string {
	return memberName + "\x00" + teamName
}

// ──────────────────────────── TeamDatabase 接口实现 ────────────────────────────

// Initialize 初始化（InMemory 无需操作，直接标记已初始化）。
func (db *InMemoryTeamDatabase) Initialize(_ context.Context) error {
	db.mu.Lock()
	db.initialized = true
	db.mu.Unlock()
	return nil
}

// CreateCurSessionTables InMemory 模式下为 no-op（无动态表）。
func (db *InMemoryTeamDatabase) CreateCurSessionTables(_ context.Context) error { return nil }

// DropCurSessionTables InMemory 模式下为 no-op。
func (db *InMemoryTeamDatabase) DropCurSessionTables(_ context.Context) error { return nil }

// CleanupAllRuntimeState 清空所有 map（对齐 Python 清空所有 dict）。
func (db *InMemoryTeamDatabase) CleanupAllRuntimeState(_ context.Context) ([]string, []string, error) {
	db.mu.Lock()
	db.teams = make(map[string]*Team)
	db.members = make(map[string]*TeamMember)
	db.mu.Unlock()
	return nil, nil, nil
}

// DropSessionTablesByID InMemory 模式下为 no-op。
func (db *InMemoryTeamDatabase) DropSessionTablesByID(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

// ForceDeleteTeamSession 跨表拆卸：删 team 行 + 删该 team 下所有成员。
// 对齐 Python: force_delete_team_session(team_name)
func (db *InMemoryTeamDatabase) ForceDeleteTeamSession(_ context.Context, teamName string) bool {
	db.mu.Lock()
	_, exists := db.teams[teamName]
	delete(db.teams, teamName)
	// 删除该 team 下所有成员（对齐 Python CASCADE on delete）
	for key, member := range db.members {
		if member.TeamName == teamName {
			delete(db.members, key)
		}
	}
	db.mu.Unlock()
	return exists
}

// Close 关闭数据库（清空所有数据）。
func (db *InMemoryTeamDatabase) Close() error {
	db.mu.Lock()
	db.teams = nil
	db.members = nil
	db.initialized = false
	db.mu.Unlock()
	return nil
}

// Team 返回 TeamDao（自引用：self.team = self）。
func (db *InMemoryTeamDatabase) Team() TeamDao { return db }

// Member 返回 MemberDao（自引用：self.member = self）。
func (db *InMemoryTeamDatabase) Member() MemberDao { return db }

// Task 返回 TaskDao（⤵️ 9.65a-2 回填后返回 db）。
func (db *InMemoryTeamDatabase) Task() TaskDao { return db }

// Message 返回 MessageDao（⤵️ 9.65a-3 回填后返回 db）。
func (db *InMemoryTeamDatabase) Message() MessageDao { return db }

// ──────────────────────────── TeamDao 接口实现 ────────────────────────────

// CreateTeam 创建团队。对齐 Python: TeamDao.create_team()
// 成功返回 true，团队已存在返回 false（对齐 Python IntegrityError → False）
func (db *InMemoryTeamDatabase) CreateTeam(_ context.Context, teamName, displayName, leaderMemberName, desc, prompt string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()
	if _, exists := db.teams[teamName]; exists {
		return false // 对齐 Python IntegrityError → False
	}
	ts := GetCurrentTime()
	db.teams[teamName] = &Team{
		TeamName:         teamName,
		DisplayName:      displayName,
		LeaderMemberName: leaderMemberName,
		Desc:             desc,
		Prompt:           prompt,
		Created:          ts,
		UpdatedAt:        ts,
	}
	return true
}

// GetTeam 获取团队信息。对齐 Python: TeamDao.get_team()
func (db *InMemoryTeamDatabase) GetTeam(_ context.Context, teamName string) (*Team, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	team, exists := db.teams[teamName]
	if !exists {
		return nil, nil // 对齐 Python Optional[Team] → None
	}
	return team, nil
}

// TeamExists 团队是否存在。对齐 Python: TeamDao.team_exists()
func (db *InMemoryTeamDatabase) TeamExists(_ context.Context, teamName string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()
	_, exists := db.teams[teamName]
	return exists
}

// DeleteTeam 删除团队（级联删除成员）。对齐 Python: TeamDao.delete_team()
func (db *InMemoryTeamDatabase) DeleteTeam(_ context.Context, teamName string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()
	_, exists := db.teams[teamName]
	if !exists {
		return false // 对齐 Python: team not found → False
	}
	delete(db.teams, teamName)
	// 级联删除成员（对齐 Python CASCADE on delete）
	for key, member := range db.members {
		if member.TeamName == teamName {
			delete(db.members, key)
		}
	}
	return true
}

// GetTeamUpdatedAt 获取团队 updated_at 毫秒时间戳。对齐 Python: TeamDao.get_team_updated_at()
func (db *InMemoryTeamDatabase) GetTeamUpdatedAt(_ context.Context, teamName string) int64 {
	db.mu.Lock()
	defer db.mu.Unlock()
	team, exists := db.teams[teamName]
	if !exists || team.UpdatedAt == 0 {
		return 0 // 对齐 Python: missing → 0
	}
	return team.UpdatedAt
}

// ──────────────────────────── MemberDao 接口实现 ────────────────────────────

// CreateMember 创建成员。对齐 Python: MemberDao.create_member()
// 成功返回 true，成员已存在返回 false（对齐 Python IntegrityError → False）
func (db *InMemoryTeamDatabase) CreateMember(_ context.Context, memberName, teamName, displayName, agentCard, status, role, desc, executionStatus, mode, prompt, modelRefJSON string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()
	key := memberKey(memberName, teamName)
	if _, exists := db.members[key]; exists {
		return false // 对齐 Python IntegrityError → False
	}
	db.members[key] = &TeamMember{
		MemberName:      memberName,
		TeamName:        teamName,
		DisplayName:     displayName,
		Desc:            desc,
		AgentCard:       agentCard,
		Status:          status,
		ExecutionStatus: executionStatus,
		Mode:            mode,
		Role:            role,
		Prompt:          prompt,
		ModelRefJSON:    modelRefJSON,
		UpdatedAt:       GetCurrentTime(), // 对齐 Python: updated_at = get_current_time()
	}
	return true
}

// GetMember 获取成员信息。对齐 Python: MemberDao.get_member()
func (db *InMemoryTeamDatabase) GetMember(_ context.Context, memberName, teamName string) (*TeamMember, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	member, exists := db.members[memberKey(memberName, teamName)]
	if !exists {
		return nil, nil // 对齐 Python Optional[TeamMember] → None
	}
	return member, nil
}

// GetTeamMembers 获取团队成员列表，可选按 status 过滤。对齐 Python: MemberDao.get_team_members(team, status=None)
func (db *InMemoryTeamDatabase) GetTeamMembers(_ context.Context, teamName string, status string) ([]*TeamMember, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	var result []*TeamMember
	for _, member := range db.members {
		if member.TeamName != teamName {
			continue
		}
		if status != "" && member.Status != status {
			continue // 对齐 Python: status 过滤
		}
		result = append(result, member)
	}
	return result, nil
}

// UpdateMemberStatus 更新成员状态（含 FSM 校验）。对齐 Python: MemberDao.update_member_status()
// 返回 true 表示成功，false 表示成员不存在或状态转换不合法
func (db *InMemoryTeamDatabase) UpdateMemberStatus(_ context.Context, memberName, teamName, status string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()
	key := memberKey(memberName, teamName)
	member, exists := db.members[key]
	if !exists {
		return false // 对齐 Python: member not found → False
	}
	// FSM 校验（对齐 Python: is_valid_transition）
	if !IsValidMemberTransition(member.Status, status) {
		return false // 对齐 Python: invalid transition → False
	}
	member.Status = status
	return true
}

// TryTransitionMemberStatus CAS 原子状态转换。对齐 Python: MemberDao.try_transition_member_status()
// 仅当当前状态 == fromStatus 时才更新为 toStatus，否则返回 false
func (db *InMemoryTeamDatabase) TryTransitionMemberStatus(_ context.Context, memberName, teamName, fromStatus, toStatus string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()
	key := memberKey(memberName, teamName)
	member, exists := db.members[key]
	if !exists {
		return false
	}
	if member.Status != fromStatus {
		return false // 对齐 Python: rowcount == 0 → False (CAS 失败)
	}
	member.Status = toStatus
	return true // 对齐 Python: rowcount == 1 → True (CAS 成功)
}

// ListHumanAgentNames 获取 human_agent 角色的成员名列表。对齐 Python: MemberDao.list_human_agent_names()
func (db *InMemoryTeamDatabase) ListHumanAgentNames(_ context.Context, teamName string) ([]string, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	var result []string
	for _, member := range db.members {
		if member.TeamName == teamName && member.Role == "human_agent" {
			result = append(result, member.MemberName)
		}
	}
	return result, nil
}

// GetMembersMaxUpdatedAt 获取 MAX(updated_at)。对齐 Python: MemberDao.get_members_max_updated_at()
func (db *InMemoryTeamDatabase) GetMembersMaxUpdatedAt(_ context.Context, teamName string) int64 {
	db.mu.Lock()
	defer db.mu.Unlock()
	var maxVal int64
	for _, member := range db.members {
		if member.TeamName == teamName && member.UpdatedAt > maxVal {
			maxVal = member.UpdatedAt
		}
	}
	return maxVal // 对齐 Python: 无数据返回 0
}

// UpdateMemberExecutionStatus 更新执行状态（含 FSM 校验）。
// 对齐 Python: MemberDao.update_member_execution_status()
func (db *InMemoryTeamDatabase) UpdateMemberExecutionStatus(_ context.Context, memberName, teamName, executionStatus string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()
	key := memberKey(memberName, teamName)
	member, exists := db.members[key]
	if !exists {
		return false
	}
	if !IsValidExecutionTransition(member.ExecutionStatus, executionStatus) {
		return false
	}
	member.ExecutionStatus = executionStatus
	return true
}

// ──────────────────────────── 编译期接口断言 ────────────────────────────

var (
	_ TeamDatabase = (*InMemoryTeamDatabase)(nil) // InMemoryTeamDatabase 必须满足 TeamDatabase 接口
	_ TeamDao      = (*InMemoryTeamDatabase)(nil) // InMemoryTeamDatabase 必须满足 TeamDao 接口
	_ MemberDao    = (*InMemoryTeamDatabase)(nil) // InMemoryTeamDatabase 必须满足 MemberDao 接口
)
