package agent

import (
	"context"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/memory"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/spawn"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	runnerspawn "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/spawn"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
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
	// leaderAllocation Leader 分配结果。⤴️ 9.64 回填完成
	leaderAllocation *models.Allocation
	// onTeammateCreated 队友创建回调
	onTeammateCreated func(memberName string)
}

// setupInfraConfig SetupInfra 可选参数配置
type setupInfraConfig struct {
	// onTeammateCreated 队友创建回调
	onTeammateCreated func(memberName string)
	// onTeamCleaned 团队清理回调
	onTeamCleaned func(memberName string)
	// onTeamBuilt 团队构建回调
	onTeamBuilt func(memberName string)
}

// SetupInfraOption SetupInfra 的可选参数。
type SetupInfraOption func(*setupInfraConfig)

// setupTeamBackendConfig SetupTeamBackend 可选参数配置
type setupTeamBackendConfig struct {
	// db 数据库实例（可选，默认使用 getSharedDB）
	db database.TeamDatabase
	// modelConfigAllocator 模型分配回调
	modelConfigAllocator func(modelName string) *models.Allocation
	// leaderAllocation Leader 模型分配结果
	leaderAllocation *models.Allocation
	// onTeamCleaned 团队清理回调
	onTeamCleaned func(ctx context.Context) error
	// onTeamBuilt 团队构建回调
	onTeamBuilt func(ctx context.Context) error
}

// SetupTeamBackendOption SetupTeamBackend 的可选参数。
type SetupTeamBackendOption func(*setupTeamBackendConfig)

// ──────────────────────────── 枚举 ────────────────────────────

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
func WithOnTeammateCreated(cb func(memberName string)) SetupInfraOption {
	return func(cfg *setupInfraConfig) { cfg.onTeammateCreated = cb }
}

// WithOnTeamCleaned 设置团队清理回调。
func WithOnTeamCleaned(cb func(memberName string)) SetupInfraOption {
	return func(cfg *setupInfraConfig) { cfg.onTeamCleaned = cb }
}

// WithOnTeamBuilt 设置团队构建回调。
func WithOnTeamBuilt(cb func(memberName string)) SetupInfraOption {
	return func(cfg *setupInfraConfig) { cfg.onTeamBuilt = cb }
}

// WithBackendOnTeamCleaned 设置团队清理回调。
func WithBackendOnTeamCleaned(cb func(ctx context.Context) error) SetupTeamBackendOption {
	return func(cfg *setupTeamBackendConfig) { cfg.onTeamCleaned = cb }
}

// WithBackendOnTeamBuilt 设置团队构建回调。
func WithBackendOnTeamBuilt(cb func(ctx context.Context) error) SetupTeamBackendOption {
	return func(cfg *setupTeamBackendConfig) { cfg.onTeamBuilt = cb }
}

// WithBackendDB 设置数据库实例。
func WithBackendDB(db database.TeamDatabase) SetupTeamBackendOption {
	return func(cfg *setupTeamBackendConfig) { cfg.db = db }
}

// WithBackendModelAllocator 设置模型分配回调。
func WithBackendModelAllocator(fn func(modelName string) *models.Allocation) SetupTeamBackendOption {
	return func(cfg *setupTeamBackendConfig) { cfg.modelConfigAllocator = fn }
}

// WithBackendLeaderAllocation 设置 Leader 模型分配结果。
func WithBackendLeaderAllocation(a *models.Allocation) SetupTeamBackendOption {
	return func(cfg *setupTeamBackendConfig) { cfg.leaderAllocation = a }
}

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
	// TODO(#9.53): 解析语言 resolvedLanguage = resolveLanguage(agentSpec.Language)
	resolvedLanguage := agentSpec.Language

	// 3. 构建 Blueprint
	// TODO(#9.69): 角色策略 rolePolicy = rolePolicy(ctx.Role, resolvedLanguage)
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
	// 待实现：c.SetMessager(createMessager(messagerConfig))

	// 6. 工作空间管理器
	if spec.Workspace != nil && spec.Workspace.Enabled {
		_ = agentSpec // 避免 unused 警告
		// TODO(#9.66): 设置工作空间管理器 c.SetWorkspaceManager(c.CreateWorkspaceManager(spec, ctx))
	}

	// 7. 模型分配器（仅 leader）
	// TODO(#9.64): 设置模型分配器 BuildModelAllocator(spec, teamSpec)
	_ = ctx.Role // 避免空分支警告，待实现后移除

	// 8. 团队后端
	// TODO(#9.58): 设置团队后端 c.SetupTeamBackend(spec, ctx, messager, ...)

	// 9. 工作树管理器（仅非 leader）
	// TODO(#9.66): 设置工作树管理器 c.CreateWorktreeManager(spec)
}

// SetupAgent Phase 2：构建 prompt，通过 TeamHarness 创建 DeepAgent，设置协调。
// 对齐 Python: AgentConfigurator.setup_agent(spec, ctx)
func (c *AgentConfigurator) SetupAgent(spec atschema.TeamAgentSpec, ctx atschema.TeamRuntimeContext) *agentteams.TeamHarness {
	// 1. 解析 AgentSpec
	_ = ResolveAgentSpec(spec, ctx.Role, ctx.MemberName)

	// 2. 解析语言
	// TODO(#9.53): 从 blueprint 或 resolveLanguage 获取

	// 3. workspace 路径解析 + symlink
	// TODO(#9.66): workspace 管理器

	// 4. 团队后端注册清理路径
	// TODO(#9.58): 团队后端工作空间路径 if teamBackend && wsSpec.RootPath

	// 5. 工作空间管理器挂载路径
	// TODO(#9.66): 工作空间管理器路径 if workspaceManager && wsSpec.RootPath

	// 6. modelConfig = ctx.MemberModel 或 agentSpec.Model
	// 7. sysOperationSpec 构造（默认 LOCAL mode）

	// 8. buildSpec = agentSpec 深拷贝 + 覆盖字段
	// TODO(#9.56): DeepAgentSpec 深拷贝方法

	// 9-14. 构造 Rails
	// TODO(#9.68): 团队工具和策略 Rail teamToolRail, teamPolicyRail, ...
	// ⚠️ 回填时必须调用 resolveTeamMode(spec)：
	//   - 构造 TeamToolRail 时: exclude_tools = {"spawn_member"} if resolveTeamMode(spec) == "predefined" else None
	//   - 构造 TeamPolicyRail 时: team_mode = resolveTeamMode(spec)
	// 对齐 Python: agent_configurator.py 第 354 行和第 378 行

	// 15. 构建团队线束
	harness := agentteams.BuildTeamHarness(
		nil, // TODO(#9.56): 构建规格
		string(ctx.Role),
		ctx.MemberName,
		nil,   // TODO(#9.68): 团队工具Rail
		nil,   // TODO(#9.68): 团队策略Rail
		nil,   // TODO(#9.68): 首轮门控
		nil,   // TODO(#9.66+#9.68): 团队工作空间Rail
		nil,   // TODO(#9.68): 工具审批Rail
		nil,   // TODO(#9.68): 团队规划模式Rail
		false, // TODO(#9.runtime): 是否启用团队规划模式
	)
	c.SetHarness(harness)

	// 16. 记忆管理器
	// TODO(#9.64): 设置记忆管理器 c.SetMemoryManager(...)

	// 17. 自定义配置器
	// TODO(#9.68): 运行自定义配置器 if spec.AgentCustomizer { ... }

	return harness
}

// SetupTeamBackend 构造 TeamBackend 并注册 cleanup 路径。
// 对齐 Python: AgentConfigurator.setup_team_backend(spec, ctx, messager, ...)
//
// Python 步骤：
//
//	Python: 1. team_name = (ctx.team_spec.team_name if ctx.team_spec else None) or "default"
//	Python: 2. db = get_shared_db(ctx.db_config)
//	Python: 3. is_leader = ctx.role == TeamRole.LEADER
//	Python: 4. current_member_name = ctx.member_name or ctx.team_spec.leader_member_name
//	Python: 5. agent_team = TeamBackend(team_name=..., member_name=..., is_leader=..., db=..., messager=...,
//	   Python: teammate_mode=..., predefined_members=..., model_config_allocator=..., leader_allocation=...,
//	   Python: enable_hitt=..., on_team_cleaned=..., on_team_built=..., leader_member_name=...)
//	Python: 6. self.team_backend = agent_team
//	Python: 7. self.task_manager = agent_team.task_manager
//	Python: 8. self.message_manager = agent_team.message_manager
//	Python: 9. if self.workspace_manager: agent_team.register_cleanup_path(ws.workspace_path)
//	Python: 10. agent_team.register_cleanup_path(str(team_home(team_name)))
func (c *AgentConfigurator) SetupTeamBackend(spec atschema.TeamAgentSpec, ctx atschema.TeamRuntimeContext, msg messager.Messager, opts ...SetupTeamBackendOption) *tools.TeamBackend {
	cfg := &setupTeamBackendConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// 对齐 Python 步骤 1: team_name
	teamName := "default"
	if ctx.TeamSpec != nil && ctx.TeamSpec.TeamName != "" {
		teamName = ctx.TeamSpec.TeamName
	}

	// 对齐 Python 步骤 2: db = get_shared_db(ctx.db_config)
	db := cfg.db
	if db == nil {
		// 尝试从 spawn.GetSharedDB 获取，若为 nil 则降级为内存数据库
		if sharedDB := spawn.GetSharedDB(ctx.DBConfig); sharedDB != nil {
			db = sharedDB
		}
		if db == nil {
			logger.Info(logComponent).Str("team_name", teamName).
				Msg("SetupTeamBackend: 使用内存数据库（sharedDB 不可用）")
			db = database.NewInMemoryTeamDatabase()
		}
	}

	// 对齐 Python 步骤 3-4: is_leader + current_member_name
	isLeader := ctx.Role == atschema.TeamRoleLeader
	currentMemberName := ctx.MemberName
	if currentMemberName == "" && ctx.TeamSpec != nil {
		currentMemberName = ctx.TeamSpec.LeaderMemberName
	}

	// 对齐 Python 步骤 5: 构造 TeamBackend
	tbOpts := []tools.TeamBackendOption{
		tools.WithTeammateMode(string(spec.TeammateMode)),
	}
	if len(spec.PredefinedMembers) > 0 {
		tbOpts = append(tbOpts, tools.WithPredefinedMembers(spec.PredefinedMembers))
	}
	if cfg.modelConfigAllocator != nil {
		tbOpts = append(tbOpts, tools.WithModelConfigAllocator(cfg.modelConfigAllocator))
	}
	if cfg.leaderAllocation != nil && isLeader {
		tbOpts = append(tbOpts, tools.WithLeaderAllocation(cfg.leaderAllocation))
	}
	if spec.EnableHITT {
		tbOpts = append(tbOpts, tools.WithEnableHITT(spec.EnableHITT))
	}
	if cfg.onTeamCleaned != nil {
		tbOpts = append(tbOpts, tools.WithOnTeamCleaned(cfg.onTeamCleaned))
	}
	if cfg.onTeamBuilt != nil {
		tbOpts = append(tbOpts, tools.WithOnTeamBuilt(cfg.onTeamBuilt))
	}
	if ctx.TeamSpec != nil && ctx.TeamSpec.LeaderMemberName != "" {
		tbOpts = append(tbOpts, tools.WithLeaderMemberName(ctx.TeamSpec.LeaderMemberName))
	}

	tb := tools.NewTeamBackend(teamName, currentMemberName, isLeader, db, msg, tbOpts...)

	// 对齐 Python 步骤 6-8: 设置到 infra
	c.SetTeamBackend(tb)
	c.SetTaskManager(tb.TaskManager())
	c.SetMessageManager(tb.MessageManager())

	// 对齐 Python 步骤 9: workspace_manager cleanup path
	// TODO(#9.66): WorkspaceManager 类型回填后调用 tb.RegisterCleanupPath(ws.workspace_path)

	// 对齐 Python 步骤 10: team_home cleanup path
	tb.RegisterCleanupPath(agentteams.TeamHome(teamName))

	logger.Info(logComponent).Str("team_name", teamName).Str("member_name", currentMemberName).
		Bool("is_leader", isLeader).Msg("SetupTeamBackend: 团队后端已创建")

	return tb
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
// ⤴️ 9.64 回填完成
func (c *AgentConfigurator) BuildMemoryManager(spec atschema.TeamAgentSpec, ctx atschema.TeamRuntimeContext, agentSpec atschema.DeepAgentSpec, language string, memberName string) *memory.TeamMemoryManager {
	// 记忆配置从 spec 中获取，当前为默认配置
	memCfg := atschema.NewTeamMemoryConfig()

	// 对齐 Python: team_memory_dir = team_memory_dir(team_name)
	teamMemoryDir := agentteams.DefaultTeamMemoryDir(c.TeamName())

	// 对齐 Python: read_only_source = ctx.get("read_only_source_workspace")
	var readOnlySource *string

	// 对齐 Python: enable_auto_extract = (spec.memory.auto_extract and spec.lifecycle == "persistent")
	autoExtract := memCfg.AutoExtract && c.Lifecycle() == string(memory.TeamLifecyclePersistent)

	// 对齐 Python: db = self.team_backend.db if self.team_backend else None
	var db database.TeamDatabase
	if c.TeamBackend() != nil {
		db = c.TeamBackend().DB()
	}

	params := memory.TeamMemoryManagerParams{
		MemberName:              memberName,
		TeamName:                c.TeamName(),
		Role:                    memory.TeamRole(c.Role()),
		Lifecycle:               memory.TeamLifecycle(c.Lifecycle()),
		Scenario:                memory.TeamScenario(memCfg.Scenario),
		EmbeddingConfig:         memory.ResolveEmbeddingConfig(&memCfg),
		Language:                memory.TeamLanguage(language),
		PromptMode:              memory.PromptMode(memCfg.MemberMemoryPromptMode),
		EnableAutoExtract:       autoExtract,
		SharedMemory:            memCfg.SharedMemory,
		TimezoneOffsetHours:     memCfg.TimezoneOffsetHours,
		Workspace:               c.Harness().Workspace(),
		SysOperation:            c.Harness().SysOperation(),
		TeamMemoryDir:           &teamMemoryDir,
		ReadOnlySourceWorkspace: readOnlySource,
		DB:                      db,
		TaskManager:             c.TaskManager(),
		ExtractionModel:         c.Harness().Model(),
	}
	return memory.NewTeamMemoryManager(params)
}

// UpdateModelPool 更新模型池。
// 对齐 Python: AgentConfigurator.update_model_pool(new_pool)
// ⤴️ 9.64 回填完成
func (c *AgentConfigurator) UpdateModelPool(newPool []models.ModelPoolEntry) {
	if c.resources == nil {
		return
	}
	// 继承池 ID：从当前池继承到新池
	teamName := c.TeamName()
	strategy := "" // 从 TeamSpec 中获取 strategy
	if c.blueprint != nil && c.blueprint.Ctx.TeamSpec != nil && c.blueprint.Ctx.TeamSpec.ModelPoolStrategy != "" {
		strategy = c.blueprint.Ctx.TeamSpec.ModelPoolStrategy
	}
	allocator := models.BuildModelAllocatorForPool(newPool, strategy, teamName)
	if allocator != nil {
		c.SetModelAllocator(allocator)
	}
}

// AttachModelAllocator 附加模型分配器。
// 对齐 Python: AgentConfigurator.attach_model_allocator(allocator, leader_allocation)
func (c *AgentConfigurator) AttachModelAllocator(allocator models.ModelAllocator, leaderAllocation *models.Allocation) {
	c.SetModelAllocator(allocator)
	c.leaderAllocation = leaderAllocation
}

// RestoreAllocatorState 恢复分配器状态。
// 对齐 Python: AgentConfigurator.restore_allocator_state(state)
// ⤴️ 9.64 回填完成
func (c *AgentConfigurator) RestoreAllocatorState(state map[string]any) {
	if c.resources == nil || c.resources.ModelAllocator == nil {
		return
	}
	c.resources.ModelAllocator.LoadStateDict(state)
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
func (c *AgentConfigurator) BuildSpawnConfig(ctx atschema.TeamRuntimeContext) runnerspawn.SpawnAgentConfig {
	if c.spawnPayloadBuilder == nil {
		return runnerspawn.SpawnAgentConfig{}
	}
	return c.spawnPayloadBuilder.BuildSpawnConfig(ctx)
}

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
func (c *AgentConfigurator) Messager() messager.Messager { return c.infra.Messager }

// SetMessager 设置消息总线。
func (c *AgentConfigurator) SetMessager(v messager.Messager) { c.infra.Messager = v }

// TeamBackend 返回团队后端。
// 对齐 Python: AgentConfigurator.team_backend property
func (c *AgentConfigurator) TeamBackend() *tools.TeamBackend { return c.infra.TeamBackend }

// SetTeamBackend 设置团队后端。
func (c *AgentConfigurator) SetTeamBackend(v *tools.TeamBackend) { c.infra.TeamBackend = v }

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
func (c *AgentConfigurator) TaskManager() *tools.TeamTaskManager { return c.infra.TaskManager }

// SetTaskManager 设置任务管理器。
func (c *AgentConfigurator) SetTaskManager(v *tools.TeamTaskManager) { c.infra.TaskManager = v }

// MessageManager 返回消息管理器。
// 对齐 Python: AgentConfigurator.message_manager property
func (c *AgentConfigurator) MessageManager() *tools.TeamMessageManager { return c.infra.MessageManager }

// SetMessageManager 设置消息管理器。
func (c *AgentConfigurator) SetMessageManager(v *tools.TeamMessageManager) {
	c.infra.MessageManager = v
}

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

// MemoryManager 返回团队记忆管理器。⤴️ 9.64 回填完成
// 对齐 Python: AgentConfigurator.memory_manager property
func (c *AgentConfigurator) MemoryManager() *memory.TeamMemoryManager {
	return c.resources.MemoryManager
}

// SetMemoryManager 设置团队记忆管理器。
func (c *AgentConfigurator) SetMemoryManager(v *memory.TeamMemoryManager) {
	c.resources.MemoryManager = v
}

// FirstIterGate 返回首轮迭代门控。
// 对齐 Python: AgentConfigurator.first_iter_gate property
func (c *AgentConfigurator) FirstIterGate() any { return c.resources.FirstIterGate }

// SetFirstIterGate 设置首轮迭代门控。
func (c *AgentConfigurator) SetFirstIterGate(v any) { c.resources.FirstIterGate = v }

// ModelAllocator 返回模型分配器。⤴️ 9.64 回填完成
// 对齐 Python: AgentConfigurator.model_allocator property
func (c *AgentConfigurator) ModelAllocator() models.ModelAllocator { return c.resources.ModelAllocator }

// SetModelAllocator 设置模型分配器。
func (c *AgentConfigurator) SetModelAllocator(v models.ModelAllocator) {
	c.resources.ModelAllocator = v
}

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
