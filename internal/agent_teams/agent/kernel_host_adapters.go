package agent

import (
	"context"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/types"
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/memory"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/team_workspace"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
)

// ──────────────────────────── 结构体 ────────────────────────────

// sessionControllerAdapter 适配 SessionManager → types.SessionController。
type sessionControllerAdapter struct {
	mgr *SessionManager
}

// teamBackendAccessorAdapter 适配 *tools.TeamBackend → types.TeamBackendAccessor。
type teamBackendAccessorAdapter struct {
	*tools.TeamBackend
}

// workspaceAccessorAdapter 适配 *team_workspace.TeamWorkspaceManager → types.WorkspaceAccessor。
type workspaceAccessorAdapter struct {
	*team_workspace.TeamWorkspaceManager
}

// memoryAccessorAdapter 适配 *memory.TeamMemoryManager → types.MemoryAccessor。
type memoryAccessorAdapter struct {
	*memory.TeamMemoryManager
}

// harnessAccessorAdapter 适配 *agentteams.TeamHarness → types.HarnessAccessor。
type harnessAccessorAdapter struct {
	*agentteams.TeamHarness
}

// dbAccessorImpl 适配 database.TeamDatabase → types.DBAccessor。
type dbAccessorImpl struct {
	db database.TeamDatabase
}

// teamTableAccessorImpl 适配 database.TeamDao → types.TeamTableAccessor。
type teamTableAccessorImpl struct {
	dao database.TeamDao
}

// ──────────────────────────── 枚 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// ── sessionControllerAdapter ──

// BindSession 绑定会话。
func (a *sessionControllerAdapter) BindSession(ctx context.Context, session any) (context.Context, error) {
	if a.mgr == nil {
		return ctx, nil
	}
	return a.mgr.BindSession(ctx, session)
}

// ReleaseSession 释放会话。
func (a *sessionControllerAdapter) ReleaseSession(ctx context.Context) error {
	if a.mgr == nil {
		return nil
	}
	a.mgr.ReleaseSession()
	return nil
}

// TeamSession 返回当前团队会话。
func (a *sessionControllerAdapter) TeamSession() any {
	if a.mgr == nil {
		return nil
	}
	return a.mgr.TeamSession()
}

// ── teamBackendAccessorAdapter ──

// DB 返回数据库访问器。
func (a *teamBackendAccessorAdapter) DB() types.DBAccessor {
	if a.TeamBackend == nil {
		return nil
	}
	db := a.TeamBackend.DB()
	if db == nil {
		return nil
	}
	return &dbAccessorImpl{db: db}
}

// IsLeader 是否是 Leader。
// 委托给嵌入的 TeamBackend.IsLeader()。
func (a *teamBackendAccessorAdapter) IsLeader() bool {
	if a.TeamBackend == nil {
		return false
	}
	return a.TeamBackend.IsLeader()
}

// CleanTeam 清理团队。
func (a *teamBackendAccessorAdapter) CleanTeam(ctx context.Context) error {
	if a.TeamBackend == nil {
		return nil
	}
	_, err := a.TeamBackend.CleanTeam(ctx)
	return err
}

// ListMembers 列出非 Leader 成员。
func (a *teamBackendAccessorAdapter) ListMembers(ctx context.Context) ([]any, error) {
	if a.TeamBackend == nil {
		return nil, nil
	}
	members, err := a.TeamBackend.ListMembers(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]any, len(members))
	for i, m := range members {
		result[i] = m
	}
	return result, nil
}

// ── workspaceAccessorAdapter ──

// Initialize 初始化工作空间。
func (a *workspaceAccessorAdapter) Initialize(ctx context.Context, remoteURL string) error {
	if a.TeamWorkspaceManager == nil {
		return nil
	}
	return a.TeamWorkspaceManager.Initialize(ctx, remoteURL)
}

// ── memoryAccessorAdapter ──

// InitToolkit 初始化记忆工具包。
func (a *memoryAccessorAdapter) InitToolkit(ctx context.Context) (bool, error) {
	if a.TeamMemoryManager == nil {
		return false, nil
	}
	return a.TeamMemoryManager.InitToolkit(ctx)
}

// SetExtractionModel 设置抽取模型。
func (a *memoryAccessorAdapter) SetExtractionModel(model any) {
	if a.TeamMemoryManager == nil {
		return
	}
	if m, ok := model.(*llm.Model); ok {
		a.TeamMemoryManager.SetExtractionModel(m)
	}
}

// ExtractionModel 返回抽取模型。
func (a *memoryAccessorAdapter) ExtractionModel() any {
	if a.TeamMemoryManager == nil {
		return nil
	}
	return a.TeamMemoryManager.ExtractionModel()
}

// Close 关闭记忆管理器。
func (a *memoryAccessorAdapter) Close() {
	if a.TeamMemoryManager == nil {
		return
	}
	_ = a.TeamMemoryManager.Close(context.Background())
}

// ── harnessAccessorAdapter ──

// RegisterMemberTools 注册成员工具。
func (a *harnessAccessorAdapter) RegisterMemberTools(memMgr types.MemoryAccessor) {
	if a.TeamHarness == nil || memMgr == nil {
		return
	}
	// 将窄接口转换回具体类型以调用 RegisterMemberTools
	if concrete, ok := memMgr.(*memoryAccessorAdapter); ok && concrete.TeamMemoryManager != nil {
		a.TeamHarness.RegisterMemberTools(concrete.TeamMemoryManager)
	}
}

// InjectMemberMemory 注入成员记忆。
func (a *harnessAccessorAdapter) InjectMemberMemory(ctx context.Context, memMgr types.MemoryAccessor) error {
	if a.TeamHarness == nil || memMgr == nil {
		return nil
	}
	if concrete, ok := memMgr.(*memoryAccessorAdapter); ok && concrete.TeamMemoryManager != nil {
		return a.TeamHarness.InjectMemberMemory(ctx, concrete.TeamMemoryManager, "")
	}
	return nil
}

// Model 返回当前模型。
func (a *harnessAccessorAdapter) Model() any {
	if a.TeamHarness == nil {
		return nil
	}
	return a.TeamHarness.Model()
}

// ── dbAccessorImpl ──

// Initialize 初始化数据库。
func (d *dbAccessorImpl) Initialize(ctx context.Context) error {
	if d.db == nil {
		return nil
	}
	return d.db.Initialize(ctx)
}

// Team 返回团队表访问器。
func (d *dbAccessorImpl) Team() types.TeamTableAccessor {
	if d.db == nil {
		return nil
	}
	return &teamTableAccessorImpl{dao: d.db.Team()}
}

// ── teamTableAccessorImpl ──

// GetTeam 获取团队信息。
func (t *teamTableAccessorImpl) GetTeam(ctx context.Context, teamName string) (any, error) {
	if t.dao == nil {
		return nil, nil
	}
	return t.dao.GetTeam(ctx, teamName)
}
