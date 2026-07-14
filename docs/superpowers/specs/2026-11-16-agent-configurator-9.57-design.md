# 9.57 AgentConfigurator + SpawnPayloadBuilder + TeamHarness 设计

## 概述

实现 AgentConfigurator（Agent 配置器）、SpawnPayloadBuilder（跨进程载荷构造器）
和 TeamHarness（DeepAgent 团队适配层），完全对齐 Python 实现。

采用**策略 B：骨架 + 依赖打桩**——完整流程结构和所有方法签名对齐 Python，
未实现的依赖步骤用 `TODO(#9.xx)` 注释占位，结构体字段用 `any` 类型占位。

同时回填 9.55 TeamAgent 中与 Configurator 直接相关的方法。

## 流程位置与作用

```
TeamAgent 启动
  │
  ├─ AgentConfigurator.Configure(spec, ctx)
  │    │
  │    ├─ Phase 1: SetupInfra()  ─── 基础设施层
  │    │    ├─ ResolveAgentSpec()         → 按 role/memberName 解析 AgentSpec
  │    │    ├─ _resolveLanguage()         → 解析语言偏好
  │    │    ├─ 构建 TeamAgentBlueprint    → 静态蓝图（冻结配置）
  │    │    ├─ 构建 SpawnPayloadBuilder   → 跨进程 wire 格式构造器
  │    │    ├─ CreateMessager()           → 消息总线
  │    │    ├─ CreateWorkspaceManager()   → 团队工作空间
  │    │    ├─ BuildModelAllocator()      → 模型池分配器（仅 leader）
  │    │    ├─ SetupTeamBackend()         → TeamBackend（DB + Task/Message Manager）
  │    │    └─ CreateWorktreeManager()    → 工作树管理器（仅非 leader）
  │    │
  │    └─ Phase 2: SetupAgent()  ─── 运行时装配层
  │         ├─ workspace 路径解析 + symlink
  │         ├─ 构造 Rails (Tool/Policy/Workspace/Approval/PlanMode)
  │         ├─ TeamHarness.Build()         → 组装 DeepAgent + 所有 Rails
  │         ├─ BuildMemoryManager()        → 团队共享记忆
  │         └─ RunAgentCustomizer()        → 用户自定义钩子
  │
  └─ TeamAgent 进入运行态（等待交互/被唤醒）
```

**AgentConfigurator 是 TeamAgent 的"总装配线"**，负责：
1. Spec 解析（按角色解析 AgentSpec）
2. 基础设施搭建（Messager、Workspace、Allocator、Backend、Worktree）
3. DeepAgent 组装（Rails + TeamHarness）
4. 记忆管理器构建
5. 属性代理（field forwarding 到 Infra/Resources/Blueprint）

## 循环依赖分析

Python 中 `harness.py` 位于 `agent_teams/` 根目录，`agent_configurator.py` 位于 `agent_teams/agent/`。
Go 中 `agent_teams/agent/` 是 `package agent`，无法 import 父包 `package agent_teams`。

**解决方案**：TeamHarness 放在 `agent` 包下（`agent/harness.go`），与 AgentConfigurator 同包。
Python 中 TeamHarness 和 AgentConfigurator 本来就在同一模块的不同文件中引用，
Go 中放在同一包的不同文件，语义等价且无循环依赖。

依赖方向：
```
agent/agent_configurator.go → agentcore/harness/interfaces (DeepAgentInterface)
agent/harness.go            → agentcore/harness/interfaces (DeepAgentInterface)
agent/team_agent.go         → agent (同包，无 import)
```
无循环。

## 新增文件

| 文件 | 对齐 Python | 职责 |
|------|------------|------|
| `agent/agent_configurator.go` | `agent_configurator.py` | AgentConfigurator + resolveTeamMode |
| `agent/payload.go` | `payload.py` | SpawnPayloadBuilder |
| `agent/harness.go` | `harness.py` | TeamHarness + MountedRails + AgentCustomizer |

## 修改文件

| 文件 | 修改内容 |
|------|---------|
| `agent/team_agent.go` | 回填 configurator 字段类型 + 属性代理方法 + Configure() |
| `agent/infra.go` | 更新注释格式：⤵️ → TODO(#9.xx) |
| `agent/resources.go` | 更新注释格式：⤵️ → TODO(#9.xx) |
| `agent/doc.go` | 添加 agent_configurator.go / payload.go / harness.go 条目 |

---

## 1. agent_configurator.go 设计

### 结构体

```go
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
```

### 非导出函数

```go
// resolveTeamMode 解析团队模式。
// 对齐 Python: _resolve_team_mode(spec)
//
// 如果 spec.TeamMode 已设置则直接返回；
// 否则检查非人类预定义成员，存在时返回 "hybrid"，否则 "default"。
func resolveTeamMode(spec atschema.TeamAgentSpec) string
```

### 导出方法清单

| 方法 | 对齐 Python | 实现策略 |
|------|------------|---------|
| `NewAgentConfigurator(card) *AgentConfigurator` | `__init__(card)` | ✅ 完整实现 |
| `Configure(spec, ctx) *TeamHarness` | `configure(spec, ctx)` | 调 SetupInfra + SetupAgent |
| `SetupInfra(spec, ctx, ...SetupInfraOption)` | `setup_infra(spec, ctx, ...)` | 完整流程骨架 + TODO |
| `SetupAgent(spec, ctx) *TeamHarness` | `setup_agent(spec, ctx)` | 完整流程骨架 + TODO |
| `ResolveAgentSpec(spec, role, memberName) DeepAgentSpec` | `resolve_agent_spec(...)` | ✅ 完整实现（静态方法） |
| `SetupTeamBackend(spec, ctx, messager, ...SetupTeamBackendOption) any` | `setup_team_backend(...)` | 骨架 + TODO(#9.58) |
| `CreateWorkspaceManager(spec, ctx) any` | `create_workspace_manager(...)` | 骨架 + TODO(#9.66) |
| `CreateWorktreeManager(spec) any` | `create_worktree_manager(spec)` | 骨架 + TODO(#9.66) |
| `BuildMemoryManager(spec, ctx, agentSpec, language, memberName) any` | `_build_memory_manager(...)` | 骨架 + TODO(#9.64) |
| `UpdateModelPool(newPool)` | `update_model_pool(new_pool)` | 骨架 + TODO(#9.64) |
| `AttachModelAllocator(allocator, leaderAllocation)` | `attach_model_allocator(...)` | ✅ 可完整实现 |
| `RestoreAllocatorState(state)` | `restore_allocator_state(state)` | 部分实现 + TODO(#9.64) |
| `BuildSpawnPayload(ctx, initialMessage) map[string]any` | 代理到 SpawnPayloadBuilder | ✅ |
| `BuildMemberContext(memberSpec) TeamRuntimeContext` | 代理到 SpawnPayloadBuilder | ✅ |
| `BuildMemberMessagerConfig(memberName) any` | 代理到 SpawnPayloadBuilder | TODO(#9.65) |
| `BuildSpawnConfig(ctx) any` | 代理到 SpawnPayloadBuilder | TODO(#9.58) |

### Getter/Setter（属性代理）

对齐 Python @property/@x.setter，代理到 infra/resources/blueprint 内部字段：

| Getter | Setter | 代理目标 |
|--------|--------|---------|
| `Infra() *TeamInfra` | — | 自身字段 |
| `Resources() *PrivateAgentResources` | — | 自身字段 |
| `Blueprint() *TeamAgentBlueprint` | — | 自身字段 |
| `Messager() any` | `SetMessager(v any)` | infra.Messager |
| `TeamBackend() any` | `SetTeamBackend(v any)` | infra.TeamBackend |
| `WorkspaceManager() any` | `SetWorkspaceManager(v any)` | infra.WorkspaceManager |
| `WorkspaceInitialized() bool` | `SetWorkspaceInitialized(v bool)` | infra.WorkspaceInitialized |
| `TaskManager() any` | `SetTaskManager(v any)` | infra.TaskManager |
| `MessageManager() any` | `SetMessageManager(v any)` | infra.MessageManager |
| `Harness() *TeamHarness` | `SetHarness(v *TeamHarness)` | resources.Harness |
| `WorktreeManager() any` | `SetWorktreeManager(v any)` | resources.WorktreeManager |
| `MemoryManager() any` | `SetMemoryManager(v any)` | resources.MemoryManager |
| `FirstIterGate() any` | `SetFirstIterGate(v any)` | resources.FirstIterGate |
| `ModelAllocator() any` | `SetModelAllocator(v any)` | resources.ModelAllocator |
| `Spec() *atschema.TeamAgentSpec` | — | blueprint.Spec |
| `RuntimeContext() *atschema.TeamRuntimeContext` | — | blueprint.Ctx |
| `RolePolicy() string` | — | blueprint.RolePolicy |
| `TeamSpec() *atschema.TeamSpec` | — | blueprint.Ctx.TeamSpec |
| `Role() atschema.TeamRole` | — | blueprint.Ctx.Role |
| `Lifecycle() string` | — | blueprint.Spec.Lifecycle |
| `MemberName() string` | — | blueprint.Ctx.MemberName |
| `TeamName() string` | — | blueprint.Ctx.TeamSpec.TeamName |

### SetupInfra 流程详解（Phase 1）

```go
func (c *AgentConfigurator) SetupInfra(
    spec atschema.TeamAgentSpec,
    ctx atschema.TeamRuntimeContext,
    opts ...SetupInfraOption,
) {
    // 1. agentSpec = ResolveAgentSpec(spec, ctx.Role, ctx.MemberName)
    //    ✅ 可完整实现

    // 2. resolvedLanguage = resolveLanguage(agentSpec.Language)
    //    TODO(#9.53): resolveLanguage 实现

    // 3. 构建 blueprint = &TeamAgentBlueprint{...}
    //    rolePolicy = rolePolicy(ctx.Role, resolvedLanguage)
    //    TODO(#9.69): rolePolicy 实现
    //    ✅ blueprint 结构体构造可完整实现

    // 4. spawnPayloadBuilder = NewSpawnPayloadBuilder(spec, ctx)
    //    ✅ 可完整实现

    // 5. 应用 opts（onTeammateCreated / onTeamCleaned / onTeamBuilt）
    //    ✅ 可完整实现

    // 6. messagerConfig 调整 + CreateMessager
    //    TODO(#9.65): CreateMessager 实现

    // 7. if spec.Workspace.Enabled:
    //    workspaceManager = CreateWorkspaceManager(spec, ctx)
    //    c.SetWorkspaceManager(workspaceManager)
    //    TODO(#9.66): CreateWorkspaceManager 实现

    // 8. if role == LEADER && c.ModelAllocator() == nil:
    //    c.SetModelAllocator(BuildModelAllocator(spec, teamSpec))
    //    TODO(#9.64): BuildModelAllocator 实现

    // 9. SetupTeamBackend(spec, ctx, messager, ...)
    //    TODO(#9.58): TeamBackend 实现

    // 10. if role != LEADER && spec.Worktree != nil && spec.Worktree.Enabled:
    //     c.SetWorktreeManager(CreateWorktreeManager(spec))
    //     TODO(#9.66): CreateWorktreeManager 实现
}
```

### SetupAgent 流程详解（Phase 2）

```go
func (c *AgentConfigurator) SetupAgent(
    spec atschema.TeamAgentSpec,
    ctx atschema.TeamRuntimeContext,
) *TeamHarness {
    // 1. agentSpec = ResolveAgentSpec(spec, ctx.Role, ctx.MemberName)
    //    ✅ 可完整实现

    // 2. resolvedLanguage = blueprint.Language 或 resolveLanguage(agentSpec.Language)

    // 3. workspace 路径解析 + symlink
    //    TODO(#9.66): workspace 管理器

    // 4. if teamBackend && wsSpec.RootPath:
    //    teamBackend.RegisterCleanupPath(wsSpec.RootPath)
    //    TODO(#9.58)

    // 5. if workspaceManager && wsSpec.RootPath:
    //    workspaceManager.MountIntoWorkspace(wsSpec.RootPath)
    //    TODO(#9.66)

    // 6. modelConfig = ctx.MemberModel 或 agentSpec.Model

    // 7. sysOperationSpec 构造（默认 LOCAL mode）
    //    ✅ 可完整实现

    // 8. buildSpec = agentSpec 深拷贝 + 覆盖 card/model/workspace/sysOperation/tools/enableSkillDiscovery/enableTaskLoop
    //    TODO(#9.56): DeepAgentSpec 深拷贝方法

    // 9. teamToolRail = &TeamToolRail{...}
    //    TODO(#9.68): TeamToolRail 实现

    // 10. teamPolicyRail = &TeamPolicyRail{...}
    //     TODO(#9.68): TeamPolicyRail 实现

    // 11. firstIterGate（非 HUMAN_AGENT 角色时创建）
    //     TODO(#9.68): FirstIterationGate 实现

    // 12. teamWorkspaceRail
    //     TODO(#9.66+#9.68)

    // 13. toolApprovalRail（coordinated teammate 且有 approval_required_tools）
    //     TODO(#9.68): TeamToolApprovalRail 实现

    // 14. teamPlanModeRail（leader 且 team_plan 启用时）
    //     TODO(#9.68+#9.runtime): isTeamPlanEnabled 实现

    // 15. harness = TeamHarness.Build(...)
    //     TODO(#9.57子项): TeamHarness.Build 完整实现
    //     骨架：调用 TeamHarness.Build，传入所有 Rails 参数

    // 16. c.SetHarness(harness)

    // 17. memoryManager = BuildMemoryManager(...)
    //     TODO(#9.64)

    // 18. if spec.AgentCustomizer:
    //     harness.RunAgentCustomizer(spec.AgentCustomizer)
    //     TODO(#9.68)

    // return harness
}
```

### SetupInfraOption 可选参数

```go
// SetupInfraOption SetupInfra 的可选参数。
type SetupInfraOption func(*setupInfraConfig)

type setupInfraConfig struct {
    onTeammateCreated any  // TODO(#9.58): 回调类型
    onTeamCleaned     any  // TODO(#9.58): 回调类型
    onTeamBuilt       any  // TODO(#9.58): 回调类型
}

// WithOnTeammateCreated 设置队友创建回调。
func WithOnTeammateCreated(cb any) SetupInfraOption { ... }

// WithOnTeamCleaned 设置团队清理回调。
func WithOnTeamCleaned(cb any) SetupInfraOption { ... }

// WithOnTeamBuilt 设置团队构建回调。
func WithOnTeamBuilt(cb any) SetupInfraOption { ... }
```

### SetupTeamBackendOption 可选参数

```go
// SetupTeamBackendOption SetupTeamBackend 的可选参数。
type SetupTeamBackendOption func(*setupTeamBackendConfig)

type setupTeamBackendConfig struct {
    onTeamCleaned any  // TODO(#9.58): 回调类型
    onTeamBuilt   any  // TODO(#9.58): 回调类型
}

// WithBackendOnTeamCleaned 设置团队清理回调。
func WithBackendOnTeamCleaned(cb any) SetupTeamBackendOption { ... }

// WithBackendOnTeamBuilt 设置团队构建回调。
func WithBackendOnTeamBuilt(cb any) SetupTeamBackendOption { ... }
```

---

## 2. payload.go 设计

### 结构体

```go
// SpawnPayloadBuilder 跨进程 spawn 载荷构造器。
// 对齐 Python: SpawnPayloadBuilder (openjiuwen/agent_teams/agent/payload.py)
//
// 集中管理 spawn teammate 时的跨进程 wire 格式。
// 输出键是 TeamAgent.FromSpawnPayload 的公共契约——
// 改这里的字段要同步改子进程入口。
type SpawnPayloadBuilder struct {
    // spec 团队 Agent 规格
    spec atschema.TeamAgentSpec
    // ctx 运行时上下文
    ctx atschema.TeamRuntimeContext
    // memberPortMap 成员名到端口的稳定映射
    memberPortMap map[string]int
    // teammatePortCounter 队友端口计数器
    teammatePortCounter int
}
```

### 方法清单

| 方法 | 对齐 Python | 实现策略 |
|------|------------|---------|
| `NewSpawnPayloadBuilder(spec, ctx) *SpawnPayloadBuilder` | `__init__(spec, ctx)` | ✅ 完整实现 |
| `BuildSpawnPayload(ctx, initialMessage) map[string]any` | `build_spawn_payload(ctx, initial_message)` | ✅ 可实现（构造 map） |
| `BuildMemberContext(memberSpec) atschema.TeamRuntimeContext` | `build_member_context(member_spec)` | ✅ 可实现 |
| `BuildMemberMessagerConfig(memberName) any` | `build_member_messager_config(member_name)` | TODO(#9.65): MessagerTransportConfig 深拷贝 |
| `BuildSpawnConfig(ctx) any` | `build_spawn_config(ctx)` | TODO(#9.58): SpawnAgentConfig 类型 |

### BuildSpawnPayload 输出契约

```go
// 输出 schema（公共 wire 契约，必须保留每个键）：
// {
//   "coordination": {
//     "team_name":          string,
//     "display_name":       string,
//     "leader_member_name": string | nil,
//     "member_name":        string,
//     "role":               string,
//     "persona":            string,
//     "transport":          map | nil,
//   },
//   "query": string,
// }
```

### BuildMemberContext 实现

```go
func (b *SpawnPayloadBuilder) BuildMemberContext(
    memberSpec atschema.TeamMemberSpec,
) atschema.TeamRuntimeContext {
    return atschema.TeamRuntimeContext{
        Role:          memberSpec.RoleType,
        MemberName:    memberSpec.MemberName,
        Persona:       memberSpec.Persona,
        TeamSpec:      b.ctx.TeamSpec,
        MessagerConfig: b.BuildMemberMessagerConfig(memberSpec.MemberName),  // TODO(#9.65): 返回 any
        DBConfig:      b.ctx.DBConfig,
    }
}
```

---

## 3. harness.go 设计

### 结构体

```go
// MountedRails 已挂载的团队侧 Rails 句柄。
// 对齐 Python: _MountedRails (openjiuwen/agent_teams/harness.py)
type MountedRails struct {
    // TeamTool 团队工具轨
    // TODO(#9.68): TeamToolRail 类型
    TeamTool any
    // TeamPolicy 团队策略轨
    // TODO(#9.68): TeamPolicyRail 类型
    TeamPolicy any
    // FirstIterGate 首轮迭代门控
    // TODO(#9.68): FirstIterationGate 类型
    FirstIterGate any
    // TeamWorkspace 团队工作空间轨
    // TODO(#9.66+#9.68): TeamWorkspaceRail 类型
    TeamWorkspace any
    // ToolApproval 工具审批轨
    // TODO(#9.68): TeamToolApprovalRail 类型
    ToolApproval any
    // TeamPlanMode 团队计划模式轨
    // TODO(#9.68): TeamPlanModeRail 类型
    TeamPlanMode any
}

// AgentCustomizer 用户自定义钩子签名。
// 对齐 Python: AgentCustomizer = Callable[[Any, Optional[str], str], None]
//
// 参数：deepAgent, memberName, roleValue
type AgentCustomizer func(deepAgent any, memberName string, roleValue string)

// TeamHarness TeamAgent 与底层 DeepAgent 之间的唯一适配器。
// 对齐 Python: TeamHarness (openjiuwen/agent_teams/harness.py)
//
// 所有对 DeepAgent 的访问（配置、模型、工作空间、Rails、流式）
// 必须通过此对象。替换 DeepAgent 只需重新实现此模块。
type TeamHarness struct {
    // deepAgent 内层 DeepAgent 实例
    // TODO(#9.57): 改为 DeepAgentInterface 类型
    deepAgent any
    // rails 已挂载的 Rails 句柄
    rails *MountedRails
    // role 团队角色
    role atschema.TeamRole
    // memberName 成员名
    memberName string
    // initialPlanMode 初始计划模式
    initialPlanMode bool
    // initialPlanModeSeeded 初始计划模式是否已种子化
    initialPlanModeSeeded bool
    // activeAgentSession 活跃的 Agent 会话
    // TODO(#9.session): AgentSession 类型
    activeAgentSession any
}
```

### 方法清单

| 方法 | 对齐 Python | 实现策略 |
|------|------------|---------|
| `NewTeamHarness(deepAgent, rails, role, memberName, initialPlanMode) *TeamHarness` | `__init__(...)` | ✅ 完整实现 |
| `TeamHarness.Build(agentSpec, role, memberName, ...RailParams) *TeamHarness` | `TeamHarness.build(...)` | 骨架 + TODO(#9.68): Rails mount |
| `RunAgentCustomizer(customizer)` | `run_agent_customizer(customizer)` | ✅ 可完整实现 |
| `DeepConfig() any` | `deep_config` property | 骨架 + TODO |
| `Workspace() any` | `workspace` property | 骨架 + TODO |
| `SysOperation() any` | `sys_operation` property | 骨架 + TODO |
| `Model() any` | `model` property | 骨架 + TODO |
| `HasPendingInterrupt() bool` | `has_pending_interrupt()` | 骨架 + TODO |
| `InitCwdForRound()` | `init_cwd_for_round()` | 骨架 + TODO |
| `Steer(ctx, content) error` | `steer(content)` | 骨架 + TODO |
| `FollowUp(ctx, content) error` | `follow_up(content)` | 骨架 + TODO |
| `Abort(ctx) error` | `abort()` | 骨架 + TODO |
| `RunStreaming(ctx, inputs, ...) (any, error)` | `run_streaming(...)` | 骨架 + TODO(#9.runner) |
| `FindRails(railType) []any` | `find_rails(rail_type)` | 骨架 + TODO |
| `RegisterRail(ctx, rail) error` | `register_rail(rail)` | 骨架 + TODO |
| `UnregisterRail(ctx, rail) error` | `unregister_rail(rail)` | 骨架 + TODO |
| `RegisterMemberTools(memoryManager)` | `register_member_tools(...)` | 骨架 + TODO(#9.64) |
| `InjectMemberMemory(ctx, memoryManager, query) error` | `inject_member_memory(...)` | 骨架 + TODO(#9.64) |
| `InnerAgent() any` | `inner_agent` property | 骨架（仅测试用） |
| `Rails() *MountedRails` | `rails` property | ✅ 完整实现 |

### Build 方法流程

```go
func TeamHarnessBuild(
    agentSpec any,       // TODO(#9.56): DeepAgentSpec 类型
    role atschema.TeamRole,
    memberName string,
    teamToolRail any,    // TODO(#9.68)
    teamPolicyRail any,  // TODO(#9.68)
    firstIterGate any,   // TODO(#9.68)
    teamWorkspaceRail any, // TODO(#9.66+#9.68)
    toolApprovalRail any,  // TODO(#9.68)
    teamPlanModeRail any,  // TODO(#9.68)
    initialPlanMode bool,
) *TeamHarness {
    // 1. deepAgent = agentSpec.Build()
    //    TODO(#9.56): DeepAgentSpec.Build()

    // 2. deepAgent.AddRail(teamToolRail)
    //    teamToolRail.SetSysOperation(deepAgent.DeepConfig().SysOperation())
    //    teamToolRail.SetWorkspace(deepAgent.DeepConfig().Workspace())
    //    teamToolRail.Init(deepAgent)
    //    TODO(#9.68)

    // 3. deepAgent.AddRail(teamPolicyRail)
    //    TODO(#9.68)

    // 4. if firstIterGate != nil: deepAgent.AddRail(firstIterGate)
    // 5. if teamWorkspaceRail != nil: deepAgent.AddRail(teamWorkspaceRail)
    // 6. if toolApprovalRail != nil: deepAgent.AddRail(toolApprovalRail)
    // 7. if teamPlanModeRail != nil: deepAgent.AddRail(teamPlanModeRail)

    // 8. rails = &MountedRails{...}
    // 9. return NewTeamHarness(deepAgent, rails, role, memberName, initialPlanMode)
}
```

---

## 4. TeamAgent 回填（9.55）

### 字段类型升级

```go
// 修改前：
configurator any

// 修改后：
configurator *AgentConfigurator
```

### 属性代理回填

| TeamAgent 方法 | 回填内容 |
|----------------|---------|
| `NewTeamAgent(card)` | 添加 `configurator: NewAgentConfigurator(card)` |
| `Blueprint()` | `if c.configurator != nil { return c.configurator.Blueprint() }; return nil` |
| `Infra()` | `if c.configurator != nil { return c.configurator.Infra() }; return nil` |
| `Resources()` | `if c.configurator != nil { return c.configurator.Resources() }; return nil` |
| `Harness()` | `if c.configurator != nil { return c.configurator.Harness() }; return nil` |
| `Spec()` | `if c.configurator != nil { return c.configurator.Spec() }; return nil` |
| `RuntimeContext()` | `if c.configurator != nil { return c.configurator.RuntimeContext() }; return nil` |
| `Role()` | `if c.configurator != nil { return c.configurator.Role() }; return 默认值` |
| `Lifecycle()` | `if c.configurator != nil { return c.configurator.Lifecycle() }; return ""` |
| `TeamSpec()` | `if c.configurator != nil { return c.configurator.TeamSpec() }; return nil` |
| `MemberName()` | `if c.configurator != nil { return c.configurator.MemberName() }; return ""` |
| `MessageManager()` | `if c.configurator != nil { return c.configurator.MessageManager() }; return nil` |
| `TaskManager()` | `if c.configurator != nil { return c.configurator.TaskManager() }; return nil` |
| `TeamBackend()` | `if c.configurator != nil { return c.configurator.TeamBackend() }; return nil` |
| `TeamName()` | `if c.configurator != nil { return c.configurator.TeamName() }; return ""` |
| `IsAgentReady()` | `return c.configurator != nil && c.configurator.Harness() != nil` |
| `Configure()` | 调用 `configurator.SetupInfra()` + `configurator.SetupAgent()` + create_member_handle + coordination.setup |
| `BuildSpawnPayload()` | `return c.configurator.BuildSpawnPayload(...)` |
| `BuildMemberContext()` | `return c.configurator.BuildMemberContext(...)` |
| `BuildSpawnConfig()` | `return c.configurator.BuildSpawnConfig(...)` |
| `AttachModelAllocator()` | `c.configurator.AttachModelAllocator(...)` |
| `RestoreAllocatorState()` | `c.configurator.RestoreAllocatorState(...)` |
| `UpdateModelPool()` | `c.configurator.UpdateModelPool(...)` |
| `RegisterRail()` | `c.configurator.Harness().RegisterRail(...)` |
| `UnregisterRail()` | `c.configurator.Harness().UnregisterRail(...)` |

### Configure() 回填

```go
func (a *TeamAgent) Configure(ctx context.Context, spec atschema.TeamAgentSpec,
    runtimeCtx atschema.TeamRuntimeContext) *TeamAgent {
    // Phase 1: 基础设施搭建
    a.configurator.SetupInfra(spec, runtimeCtx,
        WithOnTeammateCreated(nil),  // TODO(#9.55): a.onTeammateCreated
        WithOnTeamCleaned(nil),      // TODO(#9.55): a.markTeamCleaned
        WithOnTeamBuilt(nil),        // TODO(#9.55): a.markTeamBuilt
    )

    // Phase 2: Agent 组装
    a.configurator.SetupAgent(spec, runtimeCtx)

    // 构建 TeamMember 句柄
    if runtimeCtx.MemberName != "" {
        a.state.TeamMember = CreateMemberHandle(
            runtimeCtx.MemberName,
            a.configurator.Blueprint(),
            a.configurator.Infra(),
            a.card,
        )
    }

    // TODO(#9.62): coordination.setup(role=ctx.Role)
    // TODO(#9.55): a.registerTeamCompletionCallbacks()

    logger.Info(logComponent).Str("member_name", runtimeCtx.MemberName).
        Str("role", string(runtimeCtx.Role)).Msg("TeamAgent.Configure")
    return a
}
```

---

## 5. 注释格式统一

现有 `⤵️ 回填: 9.xx` 格式统一改为 `TODO(#9.xx)` 格式：

```go
// 修改前：
// ⤵️ 回填: 9.65 — Messager 类型

// 修改后：
// TODO(#9.65): Messager 类型
```

受影响文件：`infra.go`、`resources.go`、`member.go`

---

## 6. TODO 汇总

本次实现引入的 TODO 占位及其对应待实现章节：

| TODO 标记 | 依赖组件 | 说明 |
|-----------|---------|------|
| `TODO(#9.53)` | resolveLanguage | 语言偏好解析 |
| `TODO(#9.55)` | TeamAgent 回调 | onTeammateCreated/onTeamCleaned/onTeamBuilt |
| `TODO(#9.56)` | DeepAgentSpec.Build | DeepAgent 构建方法 |
| `TODO(#9.58)` | TeamBackend + SpawnAgentConfig | 团队后端 + 生成配置 |
| `TODO(#9.62)` | CoordinationKernel | 协调内核 |
| `TODO(#9.64)` | ModelAllocator + TeamMemoryManager | 模型分配器 + 团队记忆 |
| `TODO(#9.65)` | Messager 运行时 | CreateMessager + MessagerTransportConfig 深拷贝 |
| `TODO(#9.66)` | TeamWorkspaceManager + WorktreeManager | 工作空间管理器 |
| `TODO(#9.68)` | Rails (Tool/Policy/Approval/PlanMode/FirstIterGate) | 团队 Rails |
| `TODO(#9.69)` | rolePolicy | 角色策略提示词 |
| `TODO(#9.runner)` | Runner.runAgentStreaming | 流式运行器 |

---

## 7. 测试策略

### agent_configurator_test.go

| 测试用例 | 覆盖范围 |
|---------|---------|
| `TestNewAgentConfigurator` | 构造函数验证 |
| `TestResolveAgentSpec` | 静态方法：按 role/memberName 解析 |
| `TestResolveTeamMode` | 非导出函数：default/hybrid/predefined |
| `TestAgentConfigurator_GetterSetter` | 所有属性代理的读写 |
| `TestAgentConfigurator_Configure` | Configure 调用 SetupInfra + SetupAgent |
| `TestAgentConfigurator_SetupInfra_骨架` | 验证流程步骤执行（TODO 占位步骤用 assert nil） |
| `TestAgentConfigurator_SetupAgent_骨架` | 验证流程步骤执行 |
| `TestAgentConfigurator_AttachModelAllocator` | 完整实现的方法 |
| `TestAgentConfigurator_RestoreAllocatorState` | 完整实现的方法 |

### payload_test.go

| 测试用例 | 覆盖范围 |
|---------|---------|
| `TestNewSpawnPayloadBuilder` | 构造函数 |
| `TestSpawnPayloadBuilder_BuildSpawnPayload` | 输出 map 键完整性 |
| `TestSpawnPayloadBuilder_BuildMemberContext` | TeamRuntimeContext 构造 |
| `TestSpawnPayloadBuilder_BuildMemberMessagerConfig_未实现` | 验证返回 nil（TODO） |
| `TestSpawnPayloadBuilder_BuildSpawnConfig_未实现` | 验证返回 nil（TODO） |

### harness_test.go

| 测试用例 | 覆盖范围 |
|---------|---------|
| `TestNewTeamHarness` | 构造函数 |
| `TestTeamHarness_Rails` | Rails() 返回值 |
| `TestTeamHarness_RunAgentCustomizer` | 自定义钩子调用 |
| `TestTeamHarness_RunAgentCustomizer_异常` | 异常吞掉 + 日志 |
| `TestTeamHarness_Build_骨架` | 验证 Build 流程结构 |

### team_agent 回填测试

| 测试用例 | 覆盖范围 |
|---------|---------|
| `TestTeamAgent_NewTeamAgent_WithConfigurator` | 验证 configurator 非空 |
| `TestTeamAgent_PropertyProxy` | 验证属性代理到 configurator |
| `TestTeamAgent_Configure_回填` | 验证 Configure 调用链 |
