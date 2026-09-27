# 9.68-69 Team Rails / Prompts 设计文档

## 概述

9.68-69 实现 TeamAgent 的团队级 Rails 和提示词系统，是 Agent 从"裸模型"变成"团队协作成员"的关键转换层。

**实现顺序**：9.69 Prompts → 9.68 Rails（因为 TeamPolicyRail 依赖 sections.py 的 section builder）

**在 Agent 会话流程中的位置**：AgentConfigurator.SetupAgent() 第 9-14 步，位于 DeepAgent 构建之后、Harness 创建之前。

**作用**：Rails 在 DeepAgent 的生命周期钩子中注入团队级逻辑，控制 Agent 能做什么（TeamToolRail）、知道怎么做事（TeamPolicyRail）、何时被外部感知（FirstIterationGate）、何时需要审批（TeamToolApprovalRail）、以及 plan 模式下的特化行为（TeamPlanModeRail）。

---

## Python 对应源码

| 组件 | Python 路径 |
|------|------------|
| Rails 包 | `openjiuwen/agent_teams/rails/` |
| Prompts 包 | `openjiuwen/agent_teams/prompts/` |
| Rail 组装入口 | `openjiuwen/agent_teams/agent/agent_configurator.py` |
| 中文模板 | `openjiuwen/agent_teams/prompts/cn/*.md` |
| 英文模板 | `openjiuwen/agent_teams/prompts/en/*.md` |
| 共享模板 | `openjiuwen/agent_teams/prompts/system_prompt.md` |

---

## 决策记录

| 决策 | 选择 | 理由 |
|------|------|------|
| 实现顺序 | 9.69 Prompts → 9.68 Rails | TeamPolicyRail 依赖 sections.py |
| TODO 清理 + create_team_tools | 纳入 9.68-69 内部步骤 | 不单独分章节，减少流程开销 |
| MtimeSectionCache | 完整对齐 Python | TeamPolicyRail 动态 section 需要探针缓存避免每次查库 |
| 团队工具代码组织 | 集中放 tools/team_tools.go | 一目了然，方便角色过滤逻辑 |
| 模板加载 | go:embed 嵌入二进制 | 零外部依赖、部署简单、Go 项目主流做法 |
| sections 文件组织 | 一比一对齐 Python，单文件 sections.go | 与 Python 结构完全一致，对照方便 |

---

## 9.69 Prompts 包设计

### 包结构

```
internal/agent_teams/prompts/
├── doc.go                    # 包文档
├── loader.go                 # go:embed 模板加载器 + PromptTemplate
├── section_cache.go          # MtimeSectionCache（对齐 Python）
├── policy.go                 # role_policy + build_system_prompt
├── sections.go               # 8 个 builder + TeamSectionName + _LABELS + HITT
├── team_plan_agent.go        # apply_team_plan_agent_prompt + 常量
├── team_plan_mode.go         # build_team_plan_mode_section + 常量
├── cn/                       # 9 个中文 .md 模板
│   ├── leader_policy.md
│   ├── teammate_policy.md
│   ├── leader_workflow.md
│   ├── leader_workflow_predefined.md
│   ├── leader_workflow_hybrid.md
│   ├── lifecycle_temporary.md
│   ├── lifecycle_persistent.md
│   ├── team_plan_mode.md
│   └── team_plan_agent.md
├── en/                       # 9 个英文 .md 模板
│   └── (同上 9 个)
└── system_prompt.md          # 语言无关顶层模板
```

### loader.go — 模板加载器

对齐 Python `prompts/loader.py`。

```go
//go:embed cn/*.md en/*.md system_prompt.md
var promptsFS embed.FS

// PromptTemplate 提示词模板（对齐 Python PromptTemplate）
type PromptTemplate struct {
    template string
}

// LoadTemplate 加载语言相关模板
// Python: load_template(name, language) -> PromptTemplate
// 从 prompts/<lang>/<name>.md 加载
func LoadTemplate(name, language string) *PromptTemplate

// LoadSharedTemplate 加载语言无关模板
// Python: load_shared_template(name) -> PromptTemplate
// 从 prompts/<name>.md 加载
func LoadSharedTemplate(name string) *PromptTemplate

// Render 渲染模板（Go 的 text/template 或 strings.Replace 方式）
func (t *PromptTemplate) Render(vars map[string]string) string
```

**注意**：Python 的 PromptTemplate 使用 `{var}` 占位符做 `.format()` 渲染，Go 侧使用 `strings.Replace` 或简单字符串替换对齐，避免引入 Go template 语法（`{{}}`）与 Python 模板格式不兼容。

### section_cache.go — MtimeSectionCache

对齐 Python `prompts/section_cache.py`。

```go
// MtimeSectionCache 基于 mtime 探针的 PromptSection 缓存
// Python: MtimeSectionCache(probe, fetch_and_build)
//
// PromptSection 类型来自 agentcore/single_agent/prompts 包（同 memory/manager.go 的 saprompt 别名约定）
type MtimeSectionCache struct {
    probe         func(ctx context.Context) int64                     // 探针：返回单调递增整数（DB 的 updated_at）
    fetchAndBuild func(ctx context.Context) *saprompt.PromptSection   // 全量拉取+构建
    cached        *saprompt.PromptSection
    cachedMtime   int64
    initialized   bool
    mu            sync.Mutex
}

// Refresh 刷新缓存：探针值不变返回缓存，变化则重新 fetch_and_build
// Python: async refresh() -> Optional[PromptSection]
func (c *MtimeSectionCache) Refresh(ctx context.Context) *PromptSection

// Invalidate 强制下次 refresh 重新拉取
// Python: invalidate()
func (c *MtimeSectionCache) Invalidate()
```

Go 与 Python 差异：Python 用 `asyncio` + `await`，Go 用 `sync.Mutex` + 同步调用。probe 和 fetchAndBuild 的签名接收 `ctx context.Context`。

### sections.go — Section Builder

对齐 Python `prompts/sections.py`（768 行），一比一复刻。

**TeamSectionName 常量**：

```go
// TeamSectionName 团队 Section 名称常量
// Python: TeamSectionName (prompts/sections.py)
type TeamSectionName string

const (
    SectionRole       TeamSectionName = "team_role"       // P:11
    SectionHITT       TeamSectionName = "team_hitt"       // P:12
    SectionWorkflow   TeamSectionName = "team_workflow"   // P:13
    SectionLifecycle  TeamSectionName = "team_lifecycle"  // P:14
    SectionPersona    TeamSectionName = "team_persona"    // P:15
    SectionExtra      TeamSectionName = "team_extra"      // P:16
    SectionInfo       TeamSectionName = "team_info"       // P:65
    SectionMembers    TeamSectionName = "team_members"    // P:66
)
```

**8 个 Builder 函数**：

```go
func BuildTeamRoleSection(role, memberName, teammateMode, language string) *saprompt.PromptSection       // P:11
func BuildTeamHITTSection(role string, humanAgentNames []string, language, selfMemberName string, exposeToTeammates bool) *saprompt.PromptSection // P:12
func BuildTeamWorkflowSection(role, teamMode, language string) *saprompt.PromptSection                    // P:13，仅 LEADER
func BuildTeamLifecycleSection(role, lifecycle, language string) *saprompt.PromptSection                  // P:14，仅 LEADER
func BuildTeamPersonaSection(persona, language string) *saprompt.PromptSection                            // P:15
func BuildTeamExtraSection(basePrompt, language string) *saprompt.PromptSection                           // P:16
func BuildTeamInfoSection(teamInfo *TeamInfo, workspaceMount, workspacePath, language string) *saprompt.PromptSection // P:65
func BuildTeamMembersSection(members []TeamMember, selfMemberName, language string) *saprompt.PromptSection // P:66
```

**双语标签字典 `_LABELS`**：中英文各一套 heading 和 label，对齐 Python `_LABELS`。

**HITT 内部函数**（8 个，中英文各 4 角色变体）：

```go
func hittSectionLeaderCN(names []string) string
func hittSectionLeaderEN(names []string) string
func hittSectionTeammateCN() string         // 匿名版，不暴露人类成员名
func hittSectionTeammateEN() string
func hittSectionTeammateLegacyCN(names []string) string  // 遗留版，expose=True 时使用
func hittSectionTeammateLegacyEN(names []string) string
func hittSectionHumanAgentCN(names []string, selfName string) string
func hittSectionHumanAgentEN(names []string, selfName string) string
```

### policy.go — 角色策略

对齐 Python `prompts/policy.py`。

```go
// RolePolicy 返回角色策略模板文本
// Python: role_policy(role, language) -> str
func RolePolicy(role, language string) string

// BuildSystemPrompt 构建完整系统提示
// Python: build_system_prompt(...) -> str
func BuildSystemPrompt(memberName, role, language, persona, lifecycle, teamMode string,
    teamInfo *TeamInfo, members []TeamMember, basePrompt, workspaceMount, workspacePath string) string
```

### team_plan_agent.go — Plan Agent 特化

对齐 Python `prompts/team_plan_agent.py`。

```go
// TeamPlanAgentDesc 中英文描述
var TeamPlanAgentDesc = map[string]string{"cn": "...", "en": "..."}

// ApplyTeamPlanAgentPrompt 将内置 plan_agent 的提示词替换为团队规划专用版本
// Python: apply_team_plan_agent_prompt(subagents, language) -> bool
// 遍历 subagents，找到 name=="plan_agent" 且使用默认提示词的，替换 prompt + description
func ApplyTeamPlanAgentPrompt(subagents []SubagentSpec, language string) bool

// BuildTeamPlanAgentCard 构建 plan_agent 的 AgentCard
// Python: build_team_plan_agent_card(language) -> AgentCard
func BuildTeamPlanAgentCard(language string) *AgentCard
```

**回填点**：此函数实现后，需回填到 9.28 PlanAgent 的 `⤵️ 9.68-69 team.plan 特化` 标记处。

### team_plan_mode.go — Plan Mode Section

对齐 Python `prompts/team_plan_mode.py`。

```go
// BuildTeamPlanModeSection 构建 team.plan Leader 模式提示 section
// Python: build_team_plan_mode_section(language, agent, session) -> PromptSection
// 返回 PromptSection(name=MODE_INSTRUCTIONS, priority=85)
func BuildTeamPlanModeSection(language string, agent DeepAgentInterface, session any) *PromptSection
```

---

## 9.68 Rails 包设计

### 包结构

```
internal/agent_teams/rails/
├── doc.go                    # 包文档
├── first_iteration_gate.go   # FirstIterationGate（channel 替代 asyncio.Event）
├── team_tool_rail.go         # TeamToolRail（priority=90）+ qualify_team_tool_ids
├── team_policy_rail.go       # TeamPolicyRail（priority=12）+ 8 个 PromptSection 注入
├── tool_approval_rail.go     # TeamToolApprovalRail（继承 ConfirmInterruptRail）
└── team_plan_mode_rail.go    # TeamPlanModeRail（priority=84）
```

### first_iteration_gate.go — 首次迭代门控

对齐 Python `rails/first_iteration_gate.py`。

```go
// FirstIterationGate 首次迭代信号门
// Python: FirstIterationGate(AgentRail)
// Go 用 channel 替代 Python asyncio.Event
type FirstIterationGate struct {
    agentinterfaces.BaseRail
    ch       chan struct{}
    once     sync.Once
    closed   bool
}

// NewFirstIterationGate 创建首次迭代门控
func NewFirstIterationGate() *FirstIterationGate

// Wait 阻塞直到首次迭代开始
// Python: async wait()
func (g *FirstIterationGate) Wait(ctx context.Context) error

// IsReady 返回门是否已打开
// Python: is_ready -> bool
func (g *FirstIterationGate) IsReady() bool

// BeforeTaskIteration 首次迭代时打开门
// Python: async before_task_iteration(ctx)
func (g *FirstIterationGate) BeforeTaskIteration(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error

// Reset 重置门用于新一轮
// Python: reset()
func (g *FirstIterationGate) Reset()
```

**Go-Python 差异**：
- Python 用 `asyncio.Event.set()/wait()/clear()`，Go 用 `chan struct{}` + `sync.Once`
- `Wait()` 接收 `ctx` 支持超时/取消

### team_tool_rail.go — 团队工具轨

对齐 Python `rails/team_tool_rail.py`。

```go
// TeamToolRail 将团队协调工具注册到 DeepAgent
// Python: TeamToolRail(DeepAgentRail), priority=90
type TeamToolRail struct {
    hrails.DeepAgentRail
    teamBackend           *tools.TeamBackend
    role                  string
    teammateMode          string
    lifecycle             string
    language              string
    onTeammateCreated     func(memberName string)
    modelConfigAllocator  func(modelName string) *models.Allocation
    excludeTools          map[string]struct{}
    workspaceManager      *team_workspace.TeamWorkspaceManager
    worktreeManager       *worktree.WorktreeManager
    qualifyIDs            bool
    teamName              string
    memberName            string
    registeredTools       []tool.Tool
}

// NewTeamToolRail 创建团队工具轨
func NewTeamToolRail(opts ...TeamToolRailOption) *TeamToolRail

// Init 幂等初始化：构建工具列表 + 注册到 ability_manager
// Python: init(agent)
func (r *TeamToolRail) Init(ctx context.Context, agent agentinterfaces.BaseAgent) error

// Uninit 反注册所有工具
// Python: uninit(agent)
func (r *TeamToolRail) Uninit(agent agentinterfaces.BaseAgent) error
```

**init 核心流程**：
1. 调用 `CreateTeamTools()` 按角色过滤生成工具列表
2. 有 `workspaceManager` 时追加 `WorkspaceMetaTool`
3. 有 `worktreeManager` 时追加 `EnterWorktreeTool` / `ExitWorktreeTool`
4. `qualifyIDs=true` 时调用 `QualifyTeamToolIDs()` 给 tool ID 加 `{teamName}.{memberName}` 后缀
5. 注册到 `Runner.resource_mgr` + `agent.ability_manager`

**QualifyTeamToolIDs 函数**：

```go
// QualifyTeamToolIDs 给 tool ID 加 {teamName}.{memberName} 后缀
// Python: qualify_team_tool_ids(team_tools, team_name, member_name)
// inprocess 模式下防进程内多成员冲突
func QualifyTeamToolIDs(teamTools []tool.Tool, teamName, memberName string)
```

### team_policy_rail.go — 团队策略轨

对齐 Python `rails/team_policy_rail.py`。

```go
// TeamPolicyRail 将团队策略注入 SystemPromptBuilder
// Python: TeamPolicyRail(DeepAgentRail), priority=12
type TeamPolicyRail struct {
    hrails.DeepAgentRail
    role                     string
    persona                  string
    memberName               string
    lifecycle                string
    teammateMode             string
    language                 string
    teamMode                 string
    basePrompt               string
    teamWorkspaceMount       string
    teamWorkspacePath        string
    teamBackend              *tools.TeamBackend
    exposeHumanAgentsToTeammates bool
    staticSections           []prompts.PromptSection           // 6 个静态 section
    infoCache                *prompts.MtimeSectionCache        // 动态: team_info
    membersCache             *prompts.MtimeSectionCache        // 动态: team_members
    promptBuilder            prompts.SystemPromptBuilderInterface
}

// NewTeamPolicyRail 创建团队策略轨
func NewTeamPolicyRail(opts ...TeamPolicyRailOption) *TeamPolicyRail

// Init 缓存 system_prompt_builder
// Python: init(agent)
func (r *TeamPolicyRail) Init(ctx context.Context, agent agentinterfaces.BaseAgent) error

// BeforeModelCall 每次模型调用前添加所有 section
// Python: before_model_call(ctx)
func (r *TeamPolicyRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error

// Uninit 移除所有 section
// Python: uninit(agent)
func (r *TeamPolicyRail) Uninit(agent agentinterfaces.BaseAgent) error
```

**Section 布局**（对齐 Python）：

| 优先级 | Section 名 | 内容 | 类型 |
|--------|-----------|------|------|
| P:11 | team_role | 角色 + 成员名 + 执行模式 | 静态 |
| P:12 | team_hitt | HITT 人类成员协作规则 | 静态 |
| P:13 | team_workflow | Leader 工作流（仅 LEADER） | 静态 |
| P:14 | team_lifecycle | 团队生命周期（仅 LEADER） | 静态 |
| P:15 | team_persona | 当前人设 | 静态 |
| P:16 | team_extra | 用户自定义提示 | 静态 |
| P:65 | team_info | 团队元数据（从 DB 动态读取） | 动态 |
| P:66 | team_members | 成员关系（从 DB 动态读取） | 动态 |

**动态 Section 刷新**：
- `infoCache` 绑定 `teamBackend.GetTeamUpdatedAt` 作为 probe
- `membersCache` 绑定 `teamBackend.GetMembersMaxUpdatedAt` 作为 probe
- `BeforeModelCall` 中调用 `infoCache.Refresh()` 和 `membersCache.Refresh()`

### tool_approval_rail.go — 工具审批轨

对齐 Python `rails/tool_approval_rail.py`。

```go
// TeamToolApprovalRail teammate 工具调用审批
// Python: TeamToolApprovalRail(ConfirmInterruptRail), priority=90
type TeamToolApprovalRail struct {
    interrupt.BaseInterruptRail
    teamName         string
    memberName       string
    db               database.TeamDatabase
    messager         messager.Messager
    leaderMemberName string
    messageManager   *tools.TeamMessageManager
}

// NewTeamToolApprovalRail 创建工具审批轨
func NewTeamToolApprovalRail(teamName, memberName string, db database.TeamDatabase,
    msg messager.Messager, leaderMemberName string, toolNames []string) *TeamToolApprovalRail
```

**resolveInterrupt 核心流程**（对齐 Python）：

1. **首次调用**（userInput == nil）：
   - 检查 `autoConfirmConfig`，若已自动批准 → `ApproveResult`
   - 通过 `messageManager.SendMessage()` 向 leader 发送审批请求
   - 发送成功 → `InterruptResult`（中断等待 leader 回复）
   - 发送失败 → `RejectResult`

2. **恢复调用**（userInput 有值）：
   - 解析 `ConfirmPayload`（支持 map/struct 格式）
   - `Approved == true` → `ApproveResult`
   - `Approved == false` → `RejectResult`（含 feedback）
   - 解析失败 → 重新 `InterruptResult`

**仅 TEAMMATE + 有 TeamBackend + 有 Messager + 有审批工具列表时创建**。

### team_plan_mode_rail.go — 团队规划模式轨

对齐 Python `rails/team_plan_mode_rail.py`。

```go
// TeamPlanModeRail team.plan leader 提示词叠加
// Python: TeamPlanModeRail(DeepAgentRail), priority=84
// 优先级 84 在 AgentModeRail(85) 之后运行
type TeamPlanModeRail struct {
    hrails.DeepAgentRail
    languageOverride string
    promptBuilder    prompts.SystemPromptBuilderInterface
}

// NewTeamPlanModeRail 创建团队规划模式轨
func NewTeamPlanModeRail(language string) *TeamPlanModeRail

// Init 缓存 prompt builder + 调用 specializePlanAgent()
// Python: init(agent)
func (r *TeamPlanModeRail) Init(ctx context.Context, agent agentinterfaces.BaseAgent) error

// BeforeModelCall plan 模式下替换 MODE_INSTRUCTIONS section
// Python: before_model_call(ctx)
func (r *TeamPlanModeRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error

// Uninit 移除 MODE_INSTRUCTIONS section
// Python: uninit(agent)
func (r *TeamPlanModeRail) Uninit(agent agentinterfaces.BaseAgent) error
```

**Init 核心流程**：
1. 缓存 `agent.system_prompt_builder`
2. 调用 `_specializePlanAgent()` → `prompts.ApplyTeamPlanAgentPrompt(subagents, language)`

**BeforeModelCall 核心流程**：
1. 读取 agent 状态 `plan_mode.mode`
2. 非 `"plan"` → 移除 `MODE_INSTRUCTIONS` section
3. `"plan"` → 重新 specialize + 添加 `prompts.BuildTeamPlanModeSection(language, agent, session)`

**仅 LEADER + team.plan 启用时创建**。

---

## 团队工具（tools/team_tools.go）

### 新增文件

```
internal/agent_teams/tools/team_tools.go
```

### 工具清单

| Tool ID | 名称 | 对应后端方法 | 角色过滤 |
|---------|------|------------|---------|
| `send_message` | SendMessage | TeamMessageManager.SendMessage | 所有 |
| `broadcast_message` | BroadcastMessage | TeamMessageManager.BroadcastMessage | LEADER |
| `add_task` | AddTask | TeamTaskManager.Add | LEADER |
| `add_batch` | AddBatchTasks | TeamTaskManager.AddBatch | LEADER |
| `list_tasks` | ListTasks | TeamTaskManager.ListTasksWithDeps | 所有 |
| `claim_task` | ClaimTask | TeamTaskManager.Claim | TEAMMATE |
| `assign_task` | AssignTask | TeamTaskManager.Assign | LEADER |
| `complete_task` | CompleteTask | TeamTaskManager.Complete | 所有 |
| `cancel_task` | CancelTask | TeamTaskManager.Cancel | LEADER |
| `update_task` | UpdateTask | TeamTaskManager.UpdateTask | LEADER |
| `spawn_member` | SpawnMember | TeamBackend.SpawnMember | LEADER（predefined 模式排除） |
| `approve_plan` | ApprovePlan | TeamTaskManager.ApprovePlan | LEADER |

### CreateTeamTools 函数

```go
// CreateTeamTools 按角色创建团队协作工具列表
// Python: create_team_tools(role, teammate_mode, lifecycle, language, ...)
func CreateTeamTools(teamBackend *TeamBackend, role, teammateMode, lifecycle, language string,
    opts ...CreateTeamToolsOption) []tool.Tool

// CreateTeamToolsOption 可选参数
type CreateTeamToolsOption func(*createTeamToolsConfig)
func WithOnTeammateCreated(cb func(memberName string)) CreateTeamToolsOption
func WithModelConfigAllocator(fn func(modelName string) *models.Allocation) CreateTeamToolsOption
func WithExcludeTools(tools map[string]struct{}) CreateTeamToolsOption
```

---

## 回填清单

### harness.go 回填

| 当前 | 回填后 |
|------|--------|
| `TeamTool any` | `TeamTool *rails.TeamToolRail` |
| `TeamPolicy any` | `TeamPolicy *rails.TeamPolicyRail` |
| `FirstIterGate any` | `FirstIterGate *rails.FirstIterationGate` |
| `ToolApproval any` | `ToolApproval *rails.TeamToolApprovalRail` |
| `TeamPlanMode any` | `TeamPlanMode *rails.TeamPlanModeRail` |
| `BuildTeamHarness` 5 个 `any` 参数 | 具体类型 + `deepAgent.AddRail()` 调用 |

### agent_configurator.go 回填

| 位置 | 回填内容 |
|------|---------|
| 第 184 行 `TODO(#9.69)` | 调用 `prompts.RolePolicy()` |
| 第 264 行 `TODO(#9.68)` | 构造 6 个 Rail 实例 |
| 第 271-280 行 nil 参数 | 传入具体 Rail 实例 |
| 第 289 行 `TODO(#9.68)` | 运行自定义配置器 `harness.RunAgentCustomizer(customizer)` |

### agent_configurator.go 残留 TODO 清理

| TODO 标记 | 状态 | 清理动作 |
|-----------|------|---------|
| `TODO(#9.53)` ×2 | 9.51-53 ✅ | 删除 TODO，实现已到位 |
| `TODO(#9.56)` ×2 | 9.56 ✅ | 删除 TODO，实现已到位 |
| `TODO(#9.58)` ×2 | 9.58 ✅ | 删除 TODO，实现已到位 |
| `TODO(#9.64)` ×2 | 9.64 ✅ | 删除 TODO，实现已到位 |

### resources.go 回填

| 当前 | 回填后 |
|------|--------|
| `FirstIterGate any` | `FirstIterGate *rails.FirstIterationGate` |
| `TODO(#9.68)` 注释 | 删除 |

### doc.go 更新

| 当前 | 回填后 |
|------|--------|
| `rails/ ⤵️ 回填: 9.68 团队级 Rails` | `rails/ # 团队级 Rails（9.68）` |
| `prompts/ ⤵️ 回填: 9.69 团队提示词` | `prompts/ # 团队提示词（9.69）` |

### 9.28 PlanAgent 回填

| 标记 | 回填内容 |
|------|---------|
| `⤵️ 9.68-69 team.plan 特化` | 实现 `prompts.ApplyTeamPlanAgentPrompt()` 调用 |

---

## 模板文件清单（18+1 个 .md 文件）

从 Python 一比一复制，不得自行翻译或改写（对齐项目规则：提示词一比一同步 Python）。

### 中文模板（cn/）

| 文件 | Python 源 |
|------|----------|
| `leader_policy.md` | `openjiuwen/agent_teams/prompts/cn/leader_policy.md` |
| `teammate_policy.md` | `openjiuwen/agent_teams/prompts/cn/teammate_policy.md` |
| `leader_workflow.md` | `openjiuwen/agent_teams/prompts/cn/leader_workflow.md` |
| `leader_workflow_predefined.md` | `openjiuwen/agent_teams/prompts/cn/leader_workflow_predefined.md` |
| `leader_workflow_hybrid.md` | `openjiuwen/agent_teams/prompts/cn/leader_workflow_hybrid.md` |
| `lifecycle_temporary.md` | `openjiuwen/agent_teams/prompts/cn/lifecycle_temporary.md` |
| `lifecycle_persistent.md` | `openjiuwen/agent_teams/prompts/cn/lifecycle_persistent.md` |
| `team_plan_mode.md` | `openjiuwen/agent_teams/prompts/cn/team_plan_mode.md` |
| `team_plan_agent.md` | `openjiuwen/agent_teams/prompts/cn/team_plan_agent.md` |

### 英文模板（en/）

同上 9 个，从 Python `en/` 目录一比一复制。

### 共享模板

| 文件 | Python 源 |
|------|----------|
| `system_prompt.md` | `openjiuwen/agent_teams/prompts/system_prompt.md` |

---

## 测试策略

### 9.69 Prompts 测试

| 文件 | 测试内容 |
|------|---------|
| `loader_test.go` | 模板加载（中英文）+ 不存在的模板返回错误 + 缓存验证 |
| `section_cache_test.go` | MtimeSectionCache 刷新/缓存命中/Invalidate |
| `sections_test.go` | 8 个 builder 函数各角色/语言组合 + HITT 各变体 |
| `policy_test.go` | RolePolicy 各角色 + BuildSystemPrompt 完整组装 |
| `team_plan_agent_test.go` | ApplyTeamPlanAgentPrompt 替换/不替换场景 + BuildTeamPlanAgentCard |
| `team_plan_mode_test.go` | BuildTeamPlanModeSection 构建 |

### 9.68 Rails 测试

| 文件 | 测试内容 |
|------|---------|
| `first_iteration_gate_test.go` | Wait/IsReady/BeforeTaskIteration/Reset |
| `team_tool_rail_test.go` | Init 注册工具 + Uninit 反注册 + QualifyTeamToolIDs + 角色过滤 |
| `team_policy_rail_test.go` | Init/BeforeModelCall/Uninit + 静态 section 注入 + 动态 section 缓存刷新 |
| `tool_approval_rail_test.go` | resolveInterrupt 首次/恢复/自动批准/拒绝/发送失败场景 |
| `team_plan_mode_rail_test.go` | Init + BeforeModelCall plan/非plan 分支 + Uninit |

### tools/team_tools.go 测试

| 文件 | 测试内容 |
|------|---------|
| `team_tools_test.go` | CreateTeamTools 各角色工具列表 + exclude_tools 过滤 |

---

## 依赖关系

### 9.69 Prompts 依赖（全部已 ✅）

- `internal/agentcore/single_agent/prompts` — PromptSection / SystemPromptBuilderInterface
- `internal/agent_teams/tools/database` — Team / TeamMember 数据模型（section 参数类型）
- `internal/agent_teams/tools` — TeamBackend（动态 section 的数据源）

### 9.68 Rails 依赖

- 9.69 Prompts — TeamPolicyRail 和 TeamPlanModeRail 消费 section builder
- `internal/agentcore/harness/rails` — DeepAgentRail 基类
- `internal/agentcore/harness/rails/interrupt` — ConfirmInterruptRail / BaseInterruptRail / InterruptDecision
- `internal/agent_teams/tools` — TeamBackend / TeamTaskManager / TeamMessageManager
- `internal/agent_teams/tools/database` — TeamDatabase
- `internal/agent_teams/messager` — Messager
- `internal/agent_teams/team_workspace` — TeamWorkspaceManager / TeamWorkspaceRail
- `internal/agentcore/harness/tools/worktree` — WorktreeManager

### 被 9.68-69 阻塞的下游

- **9.55 TeamAgent** — 需要 6 个 Rail 类型 + 首轮门控 + 自定义配置器
- **9.28 PlanAgent 团队特化** — 需要 ApplyTeamPlanAgentPrompt
