package agent

import (
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentConfigurator Agent 配置器，负责配置、设置和初始化。
// 对齐 Python: AgentConfigurator (openjiuwen/agent_teams/agent/agent_configurator.py)
//
// 职责：
//   - Spec 和上下文管理
//   - Workspace 和 Worktree 设置
//   - 工具注册
//   - 模型分配
//   - DeepAgent 构建
type AgentConfigurator struct {
	// card Agent 身份卡片
	card *agentschema.AgentCard
	// blueprint 静态装配蓝图（Configure 时赋值）
	blueprint *TeamAgentBlueprint
	// spawnPayloadBuilder 跨进程载荷构造器（Configure 时赋值）
	spawnPayloadBuilder *SpawnPayloadBuilder
	// infra 每进程基础设施
	infra *TeamInfra
	// resources 每实例运行时资源
	resources *PrivateAgentResources
	// leaderAllocation Leader 分配结果
	// TODO(#9.64): Allocation 类型
	leaderAllocation any
	// onTeammateCreated 队友创建回调
	// TODO(#9.58): 回调类型
	onTeammateCreated any
}

// setupInfraConfig SetupInfra 可选参数配置
type setupInfraConfig struct {
	// onTeammateCreated 队友创建回调
	// TODO(#9.58): 回调类型
	onTeammateCreated any
	// onTeamCleaned 团队清理回调
	// TODO(#9.58): 回调类型
	onTeamCleaned any
	// onTeamBuilt 团队构建回调
	// TODO(#9.58): 回调类型
	onTeamBuilt any
}

// SetupInfraOption SetupInfra 的可选参数。
type SetupInfraOption func(*setupInfraConfig)

// setupTeamBackendConfig SetupTeamBackend 可选参数配置
type setupTeamBackendConfig struct {
	// onTeamCleaned 团队清理回调
	// TODO(#9.58): 回调类型
	onTeamCleaned any
	// onTeamBuilt 团队构建回调
	// TODO(#9.58): 回调类型
	onTeamBuilt any
}

// SetupTeamBackendOption SetupTeamBackend 的可选参数。
type SetupTeamBackendOption func(*setupTeamBackendConfig)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentConfigurator 创建新的 AgentConfigurator 实例。
// 对齐 Python: AgentConfigurator.__init__(card)
func NewAgentConfigurator(card *agentschema.AgentCard) *AgentConfigurator {
	return &AgentConfigurator{
		card:      card,
		infra:     &TeamInfra{},
		resources: &PrivateAgentResources{},
	}
}

// ResolveAgentSpec 按角色和成员名解析 AgentSpec。
// 对齐 Python: AgentConfigurator.resolve_agent_spec(spec, role, member_name)
//
// 优先级：memberName 精确匹配 → role 值匹配 → "teammate" → "leader"
func ResolveAgentSpec(spec atschema.TeamAgentSpec, role atschema.TeamRole, memberName string) atschema.DeepAgentSpec {
	if memberName != "" {
		if agentSpec, ok := spec.Agents[memberName]; ok {
			return agentSpec
		}
	}
	if agentSpec, ok := spec.Agents[string(role)]; ok {
		return agentSpec
	}
	if agentSpec, ok := spec.Agents["teammate"]; ok {
		return agentSpec
	}
	return spec.Agents["leader"]
}

// WithOnTeammateCreated 设置队友创建回调。
func WithOnTeammateCreated(cb any) SetupInfraOption {
	return func(cfg *setupInfraConfig) { cfg.onTeammateCreated = cb }
}

// WithOnTeamCleaned 设置团队清理回调。
func WithOnTeamCleaned(cb any) SetupInfraOption {
	return func(cfg *setupInfraConfig) { cfg.onTeamCleaned = cb }
}

// WithOnTeamBuilt 设置团队构建回调。
func WithOnTeamBuilt(cb any) SetupInfraOption {
	return func(cfg *setupInfraConfig) { cfg.onTeamBuilt = cb }
}

// WithBackendOnTeamCleaned 设置团队清理回调。
func WithBackendOnTeamCleaned(cb any) SetupTeamBackendOption {
	return func(cfg *setupTeamBackendConfig) { cfg.onTeamCleaned = cb }
}

// WithBackendOnTeamBuilt 设置团队构建回调。
func WithBackendOnTeamBuilt(cb any) SetupTeamBackendOption {
	return func(cfg *setupTeamBackendConfig) { cfg.onTeamBuilt = cb }
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveTeamMode 解析团队模式。
// 对齐 Python: _resolve_team_mode(spec)
//
// 如果 spec.TeamMode 已设置则直接返回；
// 否则检查非人类预定义成员，存在时返回 "hybrid"，否则 "default"。
func resolveTeamMode(spec atschema.TeamAgentSpec) string {
	if spec.TeamMode != "" {
		return spec.TeamMode
	}
	for _, m := range spec.PredefinedMembers {
		if m.RoleType != atschema.TeamRoleHumanAgent {
			return "hybrid"
		}
	}
	return "default"
}

// ──────────────────────────── 导出方法 ────────────────────────────

// Configure 主入口：配置基础设施并构建 Harness。
// 对齐 Python: AgentConfigurator.configure(spec, ctx)
func (c *AgentConfigurator) Configure(spec atschema.TeamAgentSpec, ctx atschema.TeamRuntimeContext) *agentteams.TeamHarness {
	c.SetupInfra(spec, ctx)
	return c.SetupAgent(spec, ctx)
}

// SetupInfra Phase 1：设置 spec/context，创建 messager、workspace manager、准备 team backend。
// 对齐 Python: AgentConfigurator.setup_infra(spec, ctx, ...)
func (c *AgentConfigurator) SetupInfra(spec atschema.TeamAgentSpec, ctx atschema.TeamRuntimeContext, opts ...SetupInfraOption) {
	// 应用可选参数
	cfg := &setupInfraConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	c.onTeammateCreated = cfg.onTeammateCreated

	// 1. 解析 AgentSpec
	agentSpec := ResolveAgentSpec(spec, ctx.Role, ctx.MemberName)

	// 2. 解析语言偏好
	// TODO(#9.53): resolvedLanguage = resolveLanguage(agentSpec.Language)
	resolvedLanguage := agentSpec.Language

	// 3. 构建 Blueprint
	// TODO(#9.69): rolePolicy = rolePolicy(ctx.Role, resolvedLanguage)
	rolePolicyStr := ""
	c.blueprint = &TeamAgentBlueprint{
		Card:       c.card,
		Spec:       spec,
		Ctx:        ctx,
		RolePolicy: rolePolicyStr,
		Language:   resolvedLanguage,
	}

	// 4. 构建 SpawnPayloadBuilder
	c.spawnPayloadBuilder = NewSpawnPayloadBuilder(spec, ctx)

	// 5. MessagerConfig 调整 + CreateMessager
	// TODO(#9.65): messagerConfig 节点 ID 调整 + CreateMessager(messagerConfig)
	// c.SetMessager(createMessager(messagerConfig))

	// 6. Workspace Manager
	if spec.Workspace != nil && spec.Workspace.Enabled {
		_ = agentSpec // 避免 unused 警告
		// TODO(#9.66): c.SetWorkspaceManager(c.CreateWorkspaceManager(spec, ctx))
	}

	// 7. Model Allocator（仅 leader）
	if ctx.Role == atschema.TeamRoleLeader && c.ModelAllocator() == nil {
		// TODO(#9.64): c.SetModelAllocator(BuildModelAllocator(spec, teamSpec))
	}

	// 8. Team Backend
	// TODO(#9.58): c.SetupTeamBackend(spec, ctx, messager, ...)

	// 9. Worktree Manager（仅非 leader）
	if ctx.Role != atschema.TeamRoleLeader && spec.Worktree != nil && spec.Worktree.Enabled {
		// TODO(#9.66): c.SetWorktreeManager(c.CreateWorktreeManager(spec))
	}
}

// SetupAgent Phase 2：构建 prompt，通过 TeamHarness 创建 DeepAgent，设置协调。
// 对齐 Python: AgentConfigurator.setup_agent(spec, ctx)
func (c *AgentConfigurator) SetupAgent(spec atschema.TeamAgentSpec, ctx atschema.TeamRuntimeContext) *agentteams.TeamHarness {
	// 1. 解析 AgentSpec
	_ = ResolveAgentSpec(spec, ctx.Role, ctx.MemberName)

	// 2. resolvedLanguage
	// TODO(#9.53): 从 blueprint 或 resolveLanguage 获取

	// 3. workspace 路径解析 + symlink
	// TODO(#9.66): workspace 管理器

	// 4. teamBackend.RegisterCleanupPath
	// TODO(#9.58): if teamBackend && wsSpec.RootPath

	// 5. workspaceManager.MountIntoWorkspace
	// TODO(#9.66): if workspaceManager && wsSpec.RootPath

	// 6. modelConfig = ctx.MemberModel 或 agentSpec.Model
	// 7. sysOperationSpec 构造（默认 LOCAL mode）

	// 8. buildSpec = agentSpec 深拷贝 + 覆盖字段
	// TODO(#9.56): DeepAgentSpec 深拷贝方法

	// 9-14. 构造 Rails
	// TODO(#9.68): teamToolRail, teamPolicyRail, firstIterGate, teamWorkspaceRail, toolApprovalRail, teamPlanModeRail

	// 15. TeamHarness.Build
	harness := agentteams.BuildTeamHarness(
		nil, // TODO(#9.56): buildSpec
		string(ctx.Role),
		ctx.MemberName,
		nil, // TODO(#9.68): teamToolRail
		nil, // TODO(#9.68): teamPolicyRail
		nil, // TODO(#9.68): firstIterGate
		nil, // TODO(#9.66+#9.68): teamWorkspaceRail
		nil, // TODO(#9.68): toolApprovalRail
		nil, // TODO(#9.68): teamPlanModeRail
		false, // TODO(#9.runtime): isTeamPlanEnabled(spec)
	)
	c.SetHarness(harness)

	// 16. Memory Manager
	// TODO(#9.64): c.SetMemoryManager(c.BuildMemoryManager(spec, ctx, agentSpec, resolvedLanguage, ctx.MemberName))

	// 17. AgentCustomizer
	// TODO(#9.68): if spec.AgentCustomizer { harness.RunAgentCustomizer(spec.AgentCustomizer) }

	return harness
}

// SetupTeamBackend 构造 TeamBackend 并注册 cleanup 路径。
// 对齐 Python: AgentConfigurator.setup_team_backend(spec, ctx, messager, ...)
//
// TODO(#9.58): TeamBackend 实现后替换
func (c *AgentConfigurator) SetupTeamBackend(spec atschema.TeamAgentSpec, ctx atschema.TeamRuntimeContext, messager any, opts ...SetupTeamBackendOption) any {
	cfg := &setupTeamBackendConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	// TODO(#9.58): TeamBackend 构造和注册
	// teamName := (ctx.TeamSpec.TeamName if ctx.TeamSpec else nil) or "default"
	// db = getSharedDB(ctx.DBConfig)
	// teamBackend = TeamBackend{...}
	// c.SetTeamBackend(teamBackend)
	// c.SetTaskManager(teamBackend.TaskManager)
	// c.SetMessageManager(teamBackend.MessageManager)
	// if c.WorkspaceManager() != nil { teamBackend.RegisterCleanupPath(...) }
	// teamBackend.RegisterCleanupPath(teamHome(teamName))
	return nil
}

// CreateWorkspaceManager 创建团队工作空间管理器。
// 对齐 Python: AgentConfigurator.create_workspace_manager(spec, ctx)
//
// TODO(#9.66): TeamWorkspaceManager 实现后替换
func (c *AgentConfigurator) CreateWorkspaceManager(spec atschema.TeamAgentSpec, ctx atschema.TeamRuntimeContext) any {
	// TODO(#9.66): TeamWorkspaceManager 构造
	return nil
}

// CreateWorktreeManager 创建工作树管理器。
// 对齐 Python: AgentConfigurator.create_worktree_manager(spec)
//
// TODO(#9.66): WorktreeManager 实现后替换
func (c *AgentConfigurator) CreateWorktreeManager(spec atschema.TeamAgentSpec) any {
	// TODO(#9.66): WorktreeManager 构造 + 事件镜像
	return nil
}

// BuildMemoryManager 构建团队共享记忆管理器。
// 对齐 Python: AgentConfigurator._build_memory_manager(spec, ctx, ...)
//
// TODO(#9.64): TeamMemoryManager 实现后替换
func (c *AgentConfigurator) BuildMemoryManager(spec atschema.TeamAgentSpec, ctx atschema.TeamRuntimeContext, agentSpec atschema.DeepAgentSpec, language string, memberName string) any {
	// TODO(#9.64): TeamMemoryManager 构造
	return nil
}

// UpdateModelPool 更新模型池。
// 对齐 Python: AgentConfigurator.update_model_pool(new_pool)
func (c *AgentConfigurator) UpdateModelPool(newPool any) {
	// TODO(#9.64): inheritPoolIds + buildModelAllocator
}

// AttachModelAllocator 附加模型分配器。
// 对齐 Python: AgentConfigurator.attach_model_allocator(allocator, leader_allocation)
func (c *AgentConfigurator) AttachModelAllocator(allocator any, leaderAllocation any) {
	c.SetModelAllocator(allocator)
	c.leaderAllocation = leaderAllocation
}

// RestoreAllocatorState 恢复分配器状态。
// 对齐 Python: AgentConfigurator.restore_allocator_state(state)
func (c *AgentConfigurator) RestoreAllocatorState(state map[string]any) {
	if c.ModelAllocator() == nil {
		return
	}
	// TODO(#9.64): c.ModelAllocator().LoadStateDict(state)
}

// BuildSpawnPayload 构建生成载荷（代理到 SpawnPayloadBuilder）。
// 对齐 Python: AgentConfigurator.build_spawn_payload(ctx, initial_message)
func (c *AgentConfigurator) BuildSpawnPayload(ctx atschema.TeamRuntimeContext, initialMessage string) map[string]any {
	if c.spawnPayloadBuilder == nil {
		return nil
	}
	return c.spawnPayloadBuilder.BuildSpawnPayload(ctx, initialMessage)
}

// BuildMemberContext 构建成员上下文（代理到 SpawnPayloadBuilder）。
// 对齐 Python: AgentConfigurator.build_member_context(member_spec)
func (c *AgentConfigurator) BuildMemberContext(memberSpec atschema.TeamMemberSpec) atschema.TeamRuntimeContext {
	if c.spawnPayloadBuilder == nil {
		return atschema.TeamRuntimeContext{}
	}
	return c.spawnPayloadBuilder.BuildMemberContext(memberSpec)
}

// BuildMemberMessagerConfig 构建成员消息配置（代理到 SpawnPayloadBuilder）。
// 对齐 Python: AgentConfigurator.build_member_messager_config(member_name)
func (c *AgentConfigurator) BuildMemberMessagerConfig(memberName string) any {
	if c.spawnPayloadBuilder == nil {
		return nil
	}
	return c.spawnPayloadBuilder.BuildMemberMessagerConfig(memberName)
}

// BuildSpawnConfig 构建生成配置（代理到 SpawnPayloadBuilder）。
// 对齐 Python: AgentConfigurator.build_spawn_config(ctx)
func (c *AgentConfigurator) BuildSpawnConfig(ctx atschema.TeamRuntimeContext) any {
	if c.spawnPayloadBuilder == nil {
		return nil
	}
	return c.spawnPayloadBuilder.BuildSpawnConfig(ctx)
}

// ──────────────────────────────────────────────────────────────
// Properties — 代理到 infra / resources / blueprint
// ──────────────────────────────────────────────────────────────

// Infra 返回每进程团队基础设施容器。
// 对齐 Python: AgentConfigurator.infra property
func (c *AgentConfigurator) Infra() *TeamInfra { return c.infra }

// Resources 返回每实例运行时资源容器。
// 对齐 Python: AgentConfigurator.resources property
func (c *AgentConfigurator) Resources() *PrivateAgentResources { return c.resources }

// Blueprint 返回静态装配蓝图，configure() 前为 nil。
// 对齐 Python: AgentConfigurator.blueprint property
func (c *AgentConfigurator) Blueprint() *TeamAgentBlueprint { return c.blueprint }

// Messager 返回消息总线。
// 对齐 Python: AgentConfigurator.messager property
func (c *AgentConfigurator) Messager() any { return c.infra.Messager }

// SetMessager 设置消息总线。
func (c *AgentConfigurator) SetMessager(v any) { c.infra.Messager = v }

// TeamBackend 返回团队后端。
// 对齐 Python: AgentConfigurator.team_backend property
func (c *AgentConfigurator) TeamBackend() any { return c.infra.TeamBackend }

// SetTeamBackend 设置团队后端。
func (c *AgentConfigurator) SetTeamBackend(v any) { c.infra.TeamBackend = v }

// WorkspaceManager 返回工作空间管理器。
// 对齐 Python: AgentConfigurator.workspace_manager property
func (c *AgentConfigurator) WorkspaceManager() any { return c.infra.WorkspaceManager }

// SetWorkspaceManager 设置工作空间管理器。
func (c *AgentConfigurator) SetWorkspaceManager(v any) { c.infra.WorkspaceManager = v }

// WorkspaceInitialized 返回工作空间是否已初始化。
func (c *AgentConfigurator) WorkspaceInitialized() bool { return c.infra.WorkspaceInitialized }

// SetWorkspaceInitialized 设置工作空间初始化状态。
func (c *AgentConfigurator) SetWorkspaceInitialized(v bool) { c.infra.WorkspaceInitialized = v }

// TaskManager 返回任务管理器。
// 对齐 Python: AgentConfigurator.task_manager property
func (c *AgentConfigurator) TaskManager() any { return c.infra.TaskManager }

// SetTaskManager 设置任务管理器。
func (c *AgentConfigurator) SetTaskManager(v any) { c.infra.TaskManager = v }

// MessageManager 返回消息管理器。
// 对齐 Python: AgentConfigurator.message_manager property
func (c *AgentConfigurator) MessageManager() any { return c.infra.MessageManager }

// SetMessageManager 设置消息管理器。
func (c *AgentConfigurator) SetMessageManager(v any) { c.infra.MessageManager = v }

// Harness 返回 TeamHarness。
// 对齐 Python: AgentConfigurator.harness property
func (c *AgentConfigurator) Harness() *agentteams.TeamHarness {
	return c.resources.Harness
}

// SetHarness 设置 TeamHarness。
func (c *AgentConfigurator) SetHarness(v *agentteams.TeamHarness) { c.resources.Harness = v }

// WorktreeManager 返回工作树管理器。
// 对齐 Python: AgentConfigurator.worktree_manager property
func (c *AgentConfigurator) WorktreeManager() any { return c.resources.WorktreeManager }

// SetWorktreeManager 设置工作树管理器。
func (c *AgentConfigurator) SetWorktreeManager(v any) { c.resources.WorktreeManager = v }

// MemoryManager 返回团队记忆管理器。
// 对齐 Python: AgentConfigurator.memory_manager property
func (c *AgentConfigurator) MemoryManager() any { return c.resources.MemoryManager }

// SetMemoryManager 设置团队记忆管理器。
func (c *AgentConfigurator) SetMemoryManager(v any) { c.resources.MemoryManager = v }

// FirstIterGate 返回首轮迭代门控。
// 对齐 Python: AgentConfigurator.first_iter_gate property
func (c *AgentConfigurator) FirstIterGate() any { return c.resources.FirstIterGate }

// SetFirstIterGate 设置首轮迭代门控。
func (c *AgentConfigurator) SetFirstIterGate(v any) { c.resources.FirstIterGate = v }

// ModelAllocator 返回模型分配器。
// 对齐 Python: AgentConfigurator.model_allocator property
func (c *AgentConfigurator) ModelAllocator() any { return c.resources.ModelAllocator }

// SetModelAllocator 设置模型分配器。
func (c *AgentConfigurator) SetModelAllocator(v any) { c.resources.ModelAllocator = v }

// Spec 返回 TeamAgentSpec。
// 对齐 Python: AgentConfigurator.spec property
func (c *AgentConfigurator) Spec() *atschema.TeamAgentSpec {
	if c.blueprint == nil {
		return nil
	}
	return &c.blueprint.Spec
}

// RuntimeContext 返回 TeamRuntimeContext。
// 对齐 Python: AgentConfigurator.ctx property
func (c *AgentConfigurator) RuntimeContext() *atschema.TeamRuntimeContext {
	if c.blueprint == nil {
		return nil
	}
	return &c.blueprint.Ctx
}

// RolePolicy 返回角色策略。
// 对齐 Python: AgentConfigurator.role_policy property
func (c *AgentConfigurator) RolePolicy() string {
	if c.blueprint == nil {
		return ""
	}
	return c.blueprint.RolePolicy
}

// TeamSpec 返回 TeamSpec。
// 对齐 Python: AgentConfigurator.team_spec property
func (c *AgentConfigurator) TeamSpec() *atschema.TeamSpec {
	if c.blueprint == nil {
		return nil
	}
	return c.blueprint.Ctx.TeamSpec
}

// Role 返回团队角色。
// 对齐 Python: AgentConfigurator.role property
func (c *AgentConfigurator) Role() atschema.TeamRole {
	if c.blueprint == nil {
		return atschema.TeamRoleLeader
	}
	return c.blueprint.Ctx.Role
}

// Lifecycle 返回生命周期模式。
// 对齐 Python: AgentConfigurator.lifecycle property
func (c *AgentConfigurator) Lifecycle() string {
	if c.blueprint == nil {
		return "temporary"
	}
	return string(c.blueprint.Spec.Lifecycle)
}

// MemberName 返回成员名。
// 对齐 Python: AgentConfigurator.member_name property
func (c *AgentConfigurator) MemberName() string {
	if c.blueprint == nil {
		return ""
	}
	return c.blueprint.Ctx.MemberName
}

// TeamName 返回团队名。
// 对齐 Python: AgentConfigurator.team_name property
func (c *AgentConfigurator) TeamName() string {
	if c.blueprint == nil || c.blueprint.Ctx.TeamSpec == nil {
		return ""
	}
	return c.blueprint.Ctx.TeamSpec.TeamName
}
