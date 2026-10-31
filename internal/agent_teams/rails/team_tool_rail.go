package rails

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/team_workspace"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/locales"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	harnessrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	worktree "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/worktree"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamToolRail 注册团队协调工具到 DeepAgent。
// Python: TeamToolRail(DeepAgentRail) (rails/team_tool_rail.py)
//
// Rail 的 Init 构建角色相关的团队工具（协调、消息、任务、可选工作空间
// 和工作树扩展），并将它们的 ToolCard 注册到 Agent 的 ability_manager。
// Uninit 清理时从 ability_manager 移除。
//
// 当 team 运行在 inprocess spawn 模式时，qualify_ids 将每个 ToolCard.id
// 后缀加上 {team_name}.{member_name}，避免多个 in-process 成员在共享
// Runner.resource_mgr 上的 ID 冲突。
type TeamToolRail struct {
	harnessrails.DeepAgentRail
	// teamBackend 团队后端门面
	teamBackend *tools.TeamBackend
	// role 团队角色
	role string
	// teammateMode 队友模式（build_mode / plan_mode）
	teammateMode string
	// lifecycle 生命周期（temporary / persistent）
	lifecycle string
	// language 语言
	language string
	// onTeammateCreated 队友创建回调
	onTeammateCreated func(ctx context.Context, memberName string) error
	// modelConfigAlloc 模型配置分配器
	modelConfigAlloc func(modelName string) *models.Allocation
	// excludeTools 排除的工具名集合
	excludeTools map[string]struct{}
	// workspaceManager 工作空间管理器
	workspaceManager *team_workspace.TeamWorkspaceManager
	// worktreeManager 工作树管理器
	worktreeManager *worktree.WorktreeManager
	// qualifyIDs 是否后缀工具 ID
	qualifyIDs bool
	// teamName 团队名
	teamName string
	// memberName 成员名
	memberName string
	// registeredTools 已注册的工具实例列表
	registeredTools []tool.Tool
}

// TeamToolRailOption TeamToolRail 构造选项。
type TeamToolRailOption func(*TeamToolRail)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件
const logComponent = logger.ComponentTeam

// teamToolRailPriority TeamToolRail 优先级，对齐 Python priority=90
const teamToolRailPriority = 90

// ──────────────────────────── 全局变量 ────────────────────────────

// _ 编译时验证：确保 TeamToolRail 满足 DeepAgentRailProvider 接口
var _ harnessrails.DeepAgentRailProvider = (*TeamToolRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamToolRail 创建 TeamToolRail 实例。
// Python: TeamToolRail.__init__(team_backend, role, ...)
func NewTeamToolRail(opts ...TeamToolRailOption) *TeamToolRail {
	r := &TeamToolRail{
		DeepAgentRail: *harnessrails.NewDeepAgentRail(),
		teamName:      "default",
	}
	r.WithPriority(teamToolRailPriority)
	for _, opt := range opts {
		opt(r)
	}
	// Python: self._team_name = team_name or "default" / self._member_name = member_name or "unknown"
	// 选项可能传入空串，此处统一归一化，避免工具 ID 限定与 workspace 锁 owner 出现空段
	if r.teamName == "" {
		r.teamName = "default"
	}
	if r.memberName == "" {
		r.memberName = "unknown"
	}
	return r
}

// Init 构建团队工具并注册到 Agent。
// Python: TeamToolRail.init(agent)
//
// 幂等：调用者可以在配置期间急切调用 Init，
// DeepAgent 的延迟 rail-init 将跳过已注册的工具。
func (r *TeamToolRail) Init(ctx context.Context, agent agentinterfaces.BaseAgent) error {
	if r.registeredTools != nil {
		return nil
	}

	// Python: tools = create_team_tools(...)
	toolList := tools.CreateTeamTools(
		r.teamBackend,
		r.role,
		r.teammateMode,
		r.lifecycle,
		r.language,
		r.onTeammateCreated,
		r.modelConfigAlloc,
		r.excludeTools,
	)

	// Python: if self._workspace_manager is not None
	//     tools.append(WorkspaceMetaTool(self._workspace_manager, ws_t))
	if r.workspaceManager != nil {
		wsT := locales.MakeTranslator(atschema.Language(r.language))
		wsTool := team_workspace.NewWorkspaceMetaTool(r.workspaceManager, team_workspace.ToolTranslator(wsT), r.memberName, "")
		toolList = append(toolList, wsTool)
	}

	// Python: if self._worktree_manager is not None
	//     tools.append(EnterWorktreeTool(self._worktree_manager, language=...))
	//     tools.append(ExitWorktreeTool(self._worktree_manager, language=...))
	if r.worktreeManager != nil {
		enterTool, err := worktree.NewEnterWorktreeTool(r.worktreeManager, r.language, "")
		if err != nil {
			logger.Warn(logComponent).Err(err).Msg("failed to create EnterWorktreeTool")
		} else {
			toolList = append(toolList, enterTool)
		}
		exitTool, err := worktree.NewExitWorktreeTool(r.worktreeManager, r.language, "")
		if err != nil {
			logger.Warn(logComponent).Err(err).Msg("failed to create ExitWorktreeTool")
		} else {
			toolList = append(toolList, exitTool)
		}
	}

	// Python: if self._qualify_ids
	if r.qualifyIDs {
		tools.QualifyTeamToolIDs(toolList, r.teamName, r.memberName)
	}

	// Python: try: Runner.resource_mgr.add_tool(tools, refresh=True)
	// Python: except Exception: team_logger.debug("Runner.resource_mgr not available, skipping...")
	// Go 侧等价：使用 recover 静默吞错
	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Debug(logComponent).Any("recover", r).Msg("Runner.resource_mgr unavailable, skipping tool registration")
			}
		}()
		rm := runner.GetResourceMgr()
		if rm != nil {
			for _, t := range toolList {
				if err := rm.AddTool(t, resources_manager.WithRefresh()); err != nil {
					logger.Debug(logComponent).Err(err).Msg("resource_mgr.add_tool failed")
				}
			}
		}
	}()

	// Python: ability_manager = getattr(agent, "ability_manager", None)
	am := agent.AbilityManager()
	if am != nil {
		for _, tl := range toolList {
			card := tl.Card()
			if card != nil {
				am.Add(card)
			}
		}
	}

	r.registeredTools = toolList
	return nil
}

// Uninit 从 Agent 和共享资源管理器移除团队工具。
// Python: TeamToolRail.uninit(agent)
func (r *TeamToolRail) Uninit(agent agentinterfaces.BaseAgent) error {
	if r.registeredTools == nil {
		return nil
	}

	// 收集工具 ID 列表，用于 ResourceMgr 批量移除
	var toolIDs []string
	am := agent.AbilityManager()
	for _, tl := range r.registeredTools {
		card := tl.Card()
		if am != nil && card != nil && card.Name != "" {
			am.Remove(card.Name)
		}
		if card != nil && card.ID != "" {
			toolIDs = append(toolIDs, card.ID)
		}
	}

	// Python: for tool in self._tools:
	//     try: Runner.resource_mgr.remove_tool(tool_id)
	//     except Exception: team_logger.debug("removal failed for {}", tool_id)
	if len(toolIDs) > 0 {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Debug(logComponent).Any("recover", rec).Msg("Runner.resource_mgr unavailable, skipping tool removal")
				}
			}()
			rm := runner.GetResourceMgr()
			if rm != nil {
				if _, err := rm.RemoveTool(toolIDs); err != nil {
					logger.Debug(logComponent).Err(err).Msg("resource_mgr.remove_tool failed")
				}
			}
		}()
	}

	r.registeredTools = nil
	return nil
}

// RegisteredTools 返回已注册的工具列表（仅用于测试）。
func (r *TeamToolRail) RegisteredTools() []tool.Tool {
	return r.registeredTools
}

// WithTeamBackend 设置团队后端。
func WithTeamBackend(tb *tools.TeamBackend) TeamToolRailOption {
	return func(r *TeamToolRail) { r.teamBackend = tb }
}

// WithRole 设置角色。
func WithRole(role string) TeamToolRailOption {
	return func(r *TeamToolRail) { r.role = role }
}

// WithTeammateMode 设置队友模式。
func WithTeammateMode(mode string) TeamToolRailOption {
	return func(r *TeamToolRail) { r.teammateMode = mode }
}

// WithLifecycle 设置生命周期。
func WithLifecycle(lifecycle string) TeamToolRailOption {
	return func(r *TeamToolRail) { r.lifecycle = lifecycle }
}

// WithLanguage 设置语言。
func WithLanguage(lang string) TeamToolRailOption {
	return func(r *TeamToolRail) { r.language = lang }
}

// WithOnTeammateCreated 设置队友创建回调。
func WithOnTeammateCreated(fn func(ctx context.Context, memberName string) error) TeamToolRailOption {
	return func(r *TeamToolRail) { r.onTeammateCreated = fn }
}

// WithModelConfigAllocator 设置模型配置分配器。
func WithModelConfigAllocator(fn func(modelName string) *models.Allocation) TeamToolRailOption {
	return func(r *TeamToolRail) { r.modelConfigAlloc = fn }
}

// WithExcludeTools 设置排除的工具名集合。
func WithExcludeTools(names map[string]struct{}) TeamToolRailOption {
	return func(r *TeamToolRail) { r.excludeTools = names }
}

// WithWorkspaceManager 设置工作空间管理器。
func WithWorkspaceManager(mgr *team_workspace.TeamWorkspaceManager) TeamToolRailOption {
	return func(r *TeamToolRail) { r.workspaceManager = mgr }
}

// WithWorktreeManager 设置工作树管理器。
func WithWorktreeManager(mgr *worktree.WorktreeManager) TeamToolRailOption {
	return func(r *TeamToolRail) { r.worktreeManager = mgr }
}

// WithQualifyIDs 设置是否后缀工具 ID。
func WithQualifyIDs(v bool) TeamToolRailOption {
	return func(r *TeamToolRail) { r.qualifyIDs = v }
}

// WithTeamName 设置团队名。
func WithTeamName(name string) TeamToolRailOption {
	return func(r *TeamToolRail) { r.teamName = name }
}

// WithMemberName 设置成员名。
func WithMemberName(name string) TeamToolRailOption {
	return func(r *TeamToolRail) { r.memberName = name }
}
