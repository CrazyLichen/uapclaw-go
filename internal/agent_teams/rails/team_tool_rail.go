package rails

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	harnessrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
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
	workspaceManager any // TODO(#9.66): TeamWorkspaceManager 类型（避免循环导入）
	// worktreeManager 工作树管理器
	worktreeManager any // TODO(#9.66a): WorktreeManager 类型（避免循环导入）
	// qualifyIDs 是否后缀工具 ID
	qualifyIDs bool
	// teamName 团队名
	teamName string
	// memberName 成员名
	memberName string
	// registeredTools 已注册的工具实例列表
	registeredTools []tool.Tool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 TeamToolRail 满足 DeepAgentRailProvider 接口
var _ harnessrails.DeepAgentRailProvider = (*TeamToolRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamToolRail 创建 TeamToolRail 实例。
// Python: TeamToolRail.__init__(team_backend, role, ...)
func NewTeamToolRail(opts ...TeamToolRailOption) *TeamToolRail {
	r := &TeamToolRail{
		teamName: "default",
	}
	for _, opt := range opts {
		opt(r)
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
	if err := r.DeepAgentRail.Init(ctx, agent); err != nil {
		return err
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
	// TODO(#9.66): 追加 WorkspaceMetaTool
	// 需要导入 team_workspace 包，当前用 any 占位，后续回填
	_ = r.workspaceManager

	// Python: if self._worktree_manager is not None
	// TODO(#9.66a): 追加 EnterWorktreeTool / ExitWorktreeTool
	// 需要导入 worktree 包，当前用 any 占位，后续回填
	_ = r.worktreeManager

	// Python: if self._qualify_ids
	if r.qualifyIDs {
		tools.QualifyTeamToolIDs(toolList, r.teamName, r.memberName)
	}

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

	am := agent.AbilityManager()
	for _, tl := range r.registeredTools {
		card := tl.Card()
		if am != nil && card != nil && card.Name != "" {
			am.Remove(card.Name)
		}
		// Python: Runner.resource_mgr.remove_tool(tool_id) — Go 侧暂无对应
	}

	r.registeredTools = nil
	return nil
}

// RegisteredTools 返回已注册的工具列表（仅用于测试）。
func (r *TeamToolRail) RegisteredTools() []tool.Tool {
	return r.registeredTools
}

// TeamToolRailOption TeamToolRail 构造选项。
type TeamToolRailOption func(*TeamToolRail)

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
func WithWorkspaceManager(mgr any) TeamToolRailOption {
	return func(r *TeamToolRail) { r.workspaceManager = mgr }
}

// WithWorktreeManager 设置工作树管理器。
func WithWorktreeManager(mgr any) TeamToolRailOption {
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

// ──────────────────────────── 非导出函数 ────────────────────────────
