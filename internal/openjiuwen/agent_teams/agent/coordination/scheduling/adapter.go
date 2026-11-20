// 本文件提供 scheduling 窄接口对具体类型的适配器。
//
// SchedulerTaskManager 由 *tools.TeamTaskManager 直接满足（方法集逐一对齐），
// LeaderMessageSender 由 *tools.TeamMessageManager 直接满足；仅 SchedulerBackend
// 需要组合适配（TeamBackend 的 shutdown 面 + MessageDao 的未读面分属两个对象）。
// Python: scheduler 直接持 self._infra.team_backend / self._infra.message_manager
// 协议对象——Go 侧两个对象不同源，用组合适配保持窄接口不变。
package scheduling

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/tools/database"
)

// schedulerBackendAdapter 把 *tools.TeamBackend + *database.InMemoryTeamDatabase /
// SqlTeamDatabase 的 Message 面适配为 SchedulerBackend 窄接口。
type schedulerBackendAdapter struct {
	backend *tools.TeamBackend
	db      database.TeamDatabase
}

// NewSchedulerBackend 构造后端探针适配器。db 取 backend 同源的 TeamDatabase
// （members_with_unread 的直发/广播两读取器都在 message_dao 上）。
// Python: self._infra.team_backend（协议对象，同源）。
func NewSchedulerBackend(backend *tools.TeamBackend, db database.TeamDatabase) SchedulerBackend {
	return &schedulerBackendAdapter{backend: backend, db: db}
}

// ListRetirableMembers 投影携带退役策略且空闲（READY）的成员（F_118）。
// Python: backend.list_retirable_members（team.py L2702 委派 member_dao L338）。
func (a *schedulerBackendAdapter) ListRetirableMembers(ctx context.Context) ([]database.RetirableMemberRow, error) {
	return a.db.Member().ListRetirableMembers(ctx, a.backend.TeamName())
}

// ListShutdownMembers SHUTDOWN 成员名集合。
// Python: backend.list_retired_members（team.py L2706 委派 member_dao list_shutdown_members）。
func (a *schedulerBackendAdapter) ListShutdownMembers(ctx context.Context) (map[string]bool, error) {
	return a.db.Member().ListShutdownMembers(ctx, a.backend.TeamName())
}

// ShutdownMember 发出常规 shutdown_member 请求。
// Python: backend.shutdown_member（team.py）——注意 Python 行是 request（成员
// 自行 settle），Go MemberOpResult 同语义。
func (a *schedulerBackendAdapter) ShutdownMember(ctx context.Context, memberName string) schema.MemberOpResult {
	return a.backend.ShutdownMember(ctx, memberName)
}

// MembersWithUnread 候选成员中仍有未读输入的子集（F_118 未读条件）。
// Python: message_manager.members_with_unread（message_manager.py L325 委派
// message_dao L483）——调度器侧经 message_dao 直取，同两个读取器。
func (a *schedulerBackendAdapter) MembersWithUnread(ctx context.Context, memberNames []string) (map[string]bool, error) {
	return a.db.Message().MembersWithUnread(ctx, a.backend.TeamName(), memberNames)
}

// TaskVerificationEnabled 验证开关探针（review 半区消费，25.2b）。
// Python: backend.task_verification_enabled（team.py）——Go 侧为
// TeamBackend.EnableTaskVerification 生效开关。
func (a *schedulerBackendAdapter) TaskVerificationEnabled(_ context.Context) (bool, error) {
	return a.backend.EnableTaskVerification(), nil
}
