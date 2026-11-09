package rails

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/team_workspace"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tracker"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/worktree"
)

// ──────────────────────────── 枚举 ────────────────────────────

// TeamHandleKey 团队活句柄在 BuildContext.Extras 里的键。
//
// 键值字符串是 checkpoint 契约（spec 可序列化持久化），逐字对齐上游，
// 不得本地化或改格式。
//
// Python: TeamHandleKey (openjiuwen/agent_teams/rails/team_context.py:62-81)
type TeamHandleKey string

// ──────────────────────────── 常量 ────────────────────────────

const (
	// TeamHandleTeamBackend 团队后端（DB + 任务管理器）
	TeamHandleTeamBackend TeamHandleKey = "team.team_backend"
	// TeamHandleWorkspaceManager 团队工作空间管理器
	TeamHandleWorkspaceManager TeamHandleKey = "team.workspace_manager"
	// TeamHandleModelAllocator 团队模型分配器
	TeamHandleModelAllocator TeamHandleKey = "team.model_allocator"
	// TeamHandleMessager 成员消息总线
	TeamHandleMessager TeamHandleKey = "team.messager"
	// TeamHandleSwarmflowModelResolver leader 的 swarmflow worker 模型解析器
	TeamHandleSwarmflowModelResolver TeamHandleKey = "team.swarmflow_model_resolver"
	// TeamHandleSwarmflowWorkerBaseSpec swarmflow worker 派生的基础 spec
	TeamHandleSwarmflowWorkerBaseSpec TeamHandleKey = "team.swarmflow_worker_base_spec"
	// TeamHandleSwarmflowHumanBaseSpec swarmflow human worker 派生的基础 spec
	TeamHandleSwarmflowHumanBaseSpec TeamHandleKey = "team.swarmflow_human_base_spec"
	// TeamHandleSwarmflowConcurrencyGovernor swarmflow 并发治理器
	TeamHandleSwarmflowConcurrencyGovernor TeamHandleKey = "team.swarmflow_concurrency_governor"
	// TeamHandleSwarmflowBudget swarmflow 共享预算账本
	TeamHandleSwarmflowBudget TeamHandleKey = "team.swarmflow_budget"
	// TeamHandleReliabilityComponents 可靠性核心组件（检测器/修复器/上报器）
	TeamHandleReliabilityComponents TeamHandleKey = "team.reliability_components"
	// TeamHandlePermissionsOverride 成员级权限收窄
	TeamHandlePermissionsOverride TeamHandleKey = "team.permissions_override"
	// TeamHandleWorktreeManager owner 作用域的 worktree 管理器
	TeamHandleWorktreeManager TeamHandleKey = "team.worktree_manager"
	// TeamHandleOnTeammateCreated 成员创建回调（spawn 落地后由 TeamManager 通知）
	//
	// 上游由 TeamBackend.autostart_unstarted() 自带 spawn 能力，无需回调；
	// Go 侧 TeamBackend.Startup(ctx, onTeammateCreated) 以参数接收，
	// 故必须经 build context 句柄传递，否则 autoStartMembers 直接返回、
	// 成员永不启动（F-02）。
	TeamHandleOnTeammateCreated TeamHandleKey = "team.on_teammate_created"
	// TeamHandleSwarmflowExternalCLIAgents 声明的外部 CLI agent 条目
	TeamHandleSwarmflowExternalCLIAgents TeamHandleKey = "team.swarmflow_external_cli_agents"
	// TeamHandleSwarmflowExpertSpecs swarmflow 可选用的专家表
	TeamHandleSwarmflowExpertSpecs TeamHandleKey = "team.swarmflow_expert_specs"
	// TeamHandleExpertCatalog 宿主的专家定位协议（leader only）
	TeamHandleExpertCatalog TeamHandleKey = "team.expert_catalog"
	// TeamHandleExpertBinding spawn_expert 所需的团队绑定信息
	TeamHandleExpertBinding TeamHandleKey = "team.expert_binding"
	// TeamHandleTaskEventTracker 成员的任务通知器（F-119/F-129）
	TeamHandleTaskEventTracker TeamHandleKey = "team.task_event_tracker"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamHandles 一次性注入的全部团队活句柄。
//
// 用结构体而非 17 个位置参数：上游 inject_team_handles 收 17 个关键字参数，
// Go 用结构体表达同一集合，且新增键不会破坏既有调用方。
//
// 句柄类型按用途区分：
//   - 有具体类型的（TeamBackend/WorkspaceManager/WorktreeManager）：getter 收窄到具体类型
//   - 接口类型的（ModelAllocator/Messager/TaskEventTracker）：getter 返回接口
//   - 尚未在 Go 侧落地的（swarmflow 系列/reliability/expert）：保持 any 透传
//     ⤵️ 各自归属节实现后再收窄
//
// Python: inject_team_handles 的 17 个关键字参数
// (openjiuwen/agent_teams/rails/team_context.py:84-166)
type TeamHandles struct {
	// TeamBackend 团队后端
	TeamBackend *tools.TeamBackend
	// WorkspaceManager 团队工作空间管理器
	WorkspaceManager *team_workspace.TeamWorkspaceManager
	// ModelAllocator 模型分配器
	ModelAllocator models.ModelAllocator
	// Messager 消息总线
	Messager messager.Messager
	// SwarmflowModelResolver swarmflow 模型解析器
	// ⤵️ swarmflow 域未迁，保持 any
	SwarmflowModelResolver any
	// SwarmflowWorkerBaseSpec swarmflow worker 基础 spec
	// ⤵️ swarmflow 域未迁，保持 any
	SwarmflowWorkerBaseSpec any
	// SwarmflowHumanBaseSpec swarmflow human 基础 spec
	// ⤵️ swarmflow 域未迁，保持 any
	SwarmflowHumanBaseSpec any
	// SwarmflowConcurrencyGovernor swarmflow 并发治理器
	// ⤵️ swarmflow 域未迁，保持 any
	SwarmflowConcurrencyGovernor any
	// SwarmflowBudget swarmflow 预算账本
	// ⤵️ swarmflow 域未迁，保持 any
	SwarmflowBudget any
	// ReliabilityComponents 可靠性核心组件
	// ⤵️ 可靠性域未迁（core.team.reliability 为占位 rail）
	ReliabilityComponents any
	// PermissionsOverride 成员级权限收窄
	PermissionsOverride map[string]string
	// WorktreeManager worktree 管理器
	WorktreeManager *worktree.WorktreeManager
	// SwarmflowExternalCLIAgents 外部 CLI agent 条目
	// ⤵️ swarmflow 域未迁，保持 any
	SwarmflowExternalCLIAgents any
	// SwarmflowExpertSpecs swarmflow 专家表
	SwarmflowExpertSpecs map[string]any
	// ExpertCatalog 专家定位协议
	// ⤵️ experts 域未迁，保持 any
	ExpertCatalog any
	// ExpertBinding 专家绑定信息
	// ⤵️ experts 域未迁，保持 any
	ExpertBinding any
	// TaskEventTracker 任务通知器
	TaskEventTracker tracker.TaskEventTracker
	// OnTeammateCreated 成员创建回调（Go 专有，见 TeamHandleOnTeammateCreated）
	OnTeammateCreated func(ctx context.Context, memberName string) error
}

// ──────────────────────────── 导出函数 ────────────────────────────

// InjectTeamHandles 把团队活句柄写进 ctx 的 Extras。
//
// 17 个键全部写入，值为 nil 也写（对齐上游 `extras[key] = None`）：
// 查询方据此区分"未注入该键"与"注入为 nil"。
//
// 两个例外与上游一致：专家表写 `dict(... or {})`（空 map 而非 nil）、
// 外部 CLI agent 条目不在这里兜底（getter 侧兜空切片）。
//
// 调用方必须传成员私有的 Extras（已 derive 且自行复制过）——
// 共享字典会让成员之间互相污染句柄。
//
// Python: inject_team_handles (team_context.py:84-166)
func InjectTeamHandles(ctx hschema.BuildContextInterface, h TeamHandles) {
	extras := ctx.GetExtras()
	extras[string(TeamHandleTeamBackend)] = h.TeamBackend
	extras[string(TeamHandleWorkspaceManager)] = h.WorkspaceManager
	extras[string(TeamHandleModelAllocator)] = h.ModelAllocator
	extras[string(TeamHandleMessager)] = h.Messager
	extras[string(TeamHandleSwarmflowModelResolver)] = h.SwarmflowModelResolver
	extras[string(TeamHandleSwarmflowWorkerBaseSpec)] = h.SwarmflowWorkerBaseSpec
	extras[string(TeamHandleSwarmflowHumanBaseSpec)] = h.SwarmflowHumanBaseSpec
	extras[string(TeamHandleSwarmflowConcurrencyGovernor)] = h.SwarmflowConcurrencyGovernor
	extras[string(TeamHandleSwarmflowBudget)] = h.SwarmflowBudget
	extras[string(TeamHandleReliabilityComponents)] = h.ReliabilityComponents
	extras[string(TeamHandlePermissionsOverride)] = h.PermissionsOverride
	extras[string(TeamHandleWorktreeManager)] = h.WorktreeManager
	extras[string(TeamHandleOnTeammateCreated)] = h.OnTeammateCreated
	extras[string(TeamHandleSwarmflowExternalCLIAgents)] = h.SwarmflowExternalCLIAgents
	// Python: extras[...] = dict(swarmflow_expert_specs or {})
	// —— nil 时写空 map，不写 None（下游 getter 依赖此语义）
	if h.SwarmflowExpertSpecs == nil {
		extras[string(TeamHandleSwarmflowExpertSpecs)] = map[string]any{}
	} else {
		extras[string(TeamHandleSwarmflowExpertSpecs)] = h.SwarmflowExpertSpecs
	}
	extras[string(TeamHandleExpertCatalog)] = h.ExpertCatalog
	extras[string(TeamHandleExpertBinding)] = h.ExpertBinding
	extras[string(TeamHandleTaskEventTracker)] = h.TaskEventTracker
}

// GetOnTeammateCreated 返回成员创建回调，缺失返回 nil。
//
// Python 无对应物：上游 TeamBackend 自带 spawn 能力（autostart_unstarted），
// Go 以回调形式经 build context 传递，见 TeamHandleOnTeammateCreated。
func GetOnTeammateCreated(ctx hschema.BuildContextInterface) func(ctx context.Context, memberName string) error {
	if v, ok := getHandle(ctx, TeamHandleOnTeammateCreated).(func(ctx context.Context, memberName string) error); ok {
		return v
	}
	return nil
}

// GetTeamBackend 返回团队后端句柄，缺失或类型不符返回 nil。
// Python: get_team_backend (team_context.py:179-181)
func GetTeamBackend(ctx hschema.BuildContextInterface) *tools.TeamBackend {
	if v, ok := getHandle(ctx, TeamHandleTeamBackend).(*tools.TeamBackend); ok {
		return v
	}
	return nil
}

// GetTeamWorkspaceManager 返回工作空间管理器，缺失返回 nil。
// Python: get_workspace_manager
func GetTeamWorkspaceManager(ctx hschema.BuildContextInterface) *team_workspace.TeamWorkspaceManager {
	if v, ok := getHandle(ctx, TeamHandleWorkspaceManager).(*team_workspace.TeamWorkspaceManager); ok {
		return v
	}
	return nil
}

// GetModelAllocator 返回模型分配器，缺失返回 nil。
// Python: get_model_allocator
//
// 注意：接口类型的 nil 判断要防 typed-nil——句柄本身可能在注入时就是
// nil 指针包在接口里，这里靠 `!= nil` 判断接口值而非底层指针。
func GetModelAllocator(ctx hschema.BuildContextInterface) models.ModelAllocator {
	raw := getHandle(ctx, TeamHandleModelAllocator)
	if raw == nil {
		return nil
	}
	if v, ok := raw.(models.ModelAllocator); ok {
		return v
	}
	return nil
}

// GetMessager 返回消息总线，缺失返回 nil。
// Python: get_messager
func GetMessager(ctx hschema.BuildContextInterface) messager.Messager {
	raw := getHandle(ctx, TeamHandleMessager)
	if raw == nil {
		return nil
	}
	if v, ok := raw.(messager.Messager); ok {
		return v
	}
	return nil
}

// GetWorktreeManager 返回 worktree 管理器，缺失返回 nil。
// Python: get_worktree_manager
func GetWorktreeManager(ctx hschema.BuildContextInterface) *worktree.WorktreeManager {
	if v, ok := getHandle(ctx, TeamHandleWorktreeManager).(*worktree.WorktreeManager); ok {
		return v
	}
	return nil
}

// GetTaskEventTracker 返回任务通知器，缺失返回 nil。
// Python: get_task_event_tracker
func GetTaskEventTracker(ctx hschema.BuildContextInterface) tracker.TaskEventTracker {
	raw := getHandle(ctx, TeamHandleTaskEventTracker)
	if raw == nil {
		return nil
	}
	if v, ok := raw.(tracker.TaskEventTracker); ok {
		return v
	}
	return nil
}

// GetSwarmflowModelResolver 返回 swarmflow 模型解析器，缺失返回 nil。
// Python: get_swarmflow_model_resolver
//
// ⤵️ swarmflow 域未迁，返回 any。
func GetSwarmflowModelResolver(ctx hschema.BuildContextInterface) any {
	return getHandle(ctx, TeamHandleSwarmflowModelResolver)
}

// GetSwarmflowWorkerBaseSpec 返回 worker 基础 spec，缺失返回 nil。
// Python: get_swarmflow_worker_base_spec
//
// ⤵️ swarmflow 域未迁，返回 any。
func GetSwarmflowWorkerBaseSpec(ctx hschema.BuildContextInterface) any {
	return getHandle(ctx, TeamHandleSwarmflowWorkerBaseSpec)
}

// GetSwarmflowHumanBaseSpec 返回 human worker 基础 spec，缺失返回 nil。
// Python: get_swarmflow_human_base_spec
//
// ⤵️ swarmflow 域未迁，返回 any。
func GetSwarmflowHumanBaseSpec(ctx hschema.BuildContextInterface) any {
	return getHandle(ctx, TeamHandleSwarmflowHumanBaseSpec)
}

// GetSwarmflowConcurrencyGovernor 返回并发治理器，缺失返回 nil。
// Python: get_swarmflow_concurrency_governor
//
// ⤵️ swarmflow 域未迁，返回 any。
func GetSwarmflowConcurrencyGovernor(ctx hschema.BuildContextInterface) any {
	return getHandle(ctx, TeamHandleSwarmflowConcurrencyGovernor)
}

// GetSwarmflowBudget 返回预算账本，缺失返回 nil。
// Python: get_swarmflow_budget
//
// ⤵️ swarmflow 域未迁，返回 any。
func GetSwarmflowBudget(ctx hschema.BuildContextInterface) any {
	return getHandle(ctx, TeamHandleSwarmflowBudget)
}

// GetReliabilityComponents 返回可靠性组件，缺失返回 nil。
// Python: get_reliability_components
//
// ⤵️ 可靠性域未迁，返回 any。
func GetReliabilityComponents(ctx hschema.BuildContextInterface) any {
	return getHandle(ctx, TeamHandleReliabilityComponents)
}

// GetPermissionsOverride 返回权限收窄，缺失返回 nil。
// Python: get_permissions_override
func GetPermissionsOverride(ctx hschema.BuildContextInterface) any {
	return getHandle(ctx, TeamHandlePermissionsOverride)
}

// GetSwarmflowExternalCLIAgents 返回外部 CLI agent 条目，缺失返回**空切片**。
//
// 对齐上游 `tuple(... or ())`：返回空 tuple 而非 None，让调用方可直接遍历。
//
// Python: get_swarmflow_external_cli_agents (team_context.py:204-206)
func GetSwarmflowExternalCLIAgents(ctx hschema.BuildContextInterface) any {
	raw := getHandle(ctx, TeamHandleSwarmflowExternalCLIAgents)
	if raw == nil {
		return []any{}
	}
	return raw
}

// GetSwarmflowExpertSpecs 返回专家表，缺失返回**空 map**。
//
// 对齐上游 `dict(... or {})`：返回空 dict 而非 None。
//
// Python: get_swarmflow_expert_specs (team_context.py:209-211)
func GetSwarmflowExpertSpecs(ctx hschema.BuildContextInterface) any {
	raw := getHandle(ctx, TeamHandleSwarmflowExpertSpecs)
	if raw == nil {
		return map[string]any{}
	}
	return raw
}

// GetExpertCatalog 返回专家定位协议，缺失返回 nil。
// Python: get_expert_catalog
//
// ⤵️ experts 域未迁，返回 any。
func GetExpertCatalog(ctx hschema.BuildContextInterface) any {
	return getHandle(ctx, TeamHandleExpertCatalog)
}

// GetExpertBinding 返回专家绑定信息，缺失返回 nil。
// Python: get_expert_binding
//
// ⤵️ experts 域未迁，返回 any。
func GetExpertBinding(ctx hschema.BuildContextInterface) any {
	return getHandle(ctx, TeamHandleExpertBinding)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getHandle 从 Extras 防御式取一个句柄（ctx/extras 为 nil 时返回 nil）。
//
// Python: _get (team_context.py:169-172)
func getHandle(ctx hschema.BuildContextInterface, key TeamHandleKey) any {
	if ctx == nil {
		return nil
	}
	extras := ctx.GetExtras()
	if extras == nil {
		return nil
	}
	return extras[string(key)]
}
