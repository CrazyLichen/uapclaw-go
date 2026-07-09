# 10.3.19-20 SkillManager(Server) + SkillDev 管道 设计文档

## 概述

本设计文档覆盖实现计划中 **10.3.19** (SkillManager Server 侧) 和 **10.3.20** (SkillDev 管道) 两个子步骤，以及 **10.3.2 中 6 处 skill 相关 stub 的回填** 和 **JiuWenClaw → UapClaw 全量重命名**。

## 1. 流程位置与作用

### 1.1 Agent 会话流程中的位置

```
Gateway (前端 WebSocket)
  → ChannelTransport.SendCh()
    → AgentServer.startConsumeLoop()
      → handleEnvelope() → switch ReqMethod
        → default 分支 → handleUnary / handleStream
          → UapClaw.ProcessMessage() / ProcessMessageStream()
            ├── ① Session/Chat 分支（已实现）
            ├── ② Skills 分支（10.3.19 实现）← 【这里】
            ├── ③ SkillDev 分支（10.3.20 实现）← 【这里】
            └── ④ Plugins 分支（10.3.19 实现）← 【这里】
```

Skills/Plugins/SkillDev 请求在 `ProcessMessage` / `ProcessMessageStream` 中于常规对话**之前**拦截，**短路返回**，不走 ReAct Agent 对话流程。

### 1.2 作用

| 小节 | 组件 | 作用 |
|------|------|------|
| **10.3.19** | **SkillManager (Server)** | 服务端技能管理器，处理 30 个 `skills.*` + 6 个 `plugins.*` RPC 方法，负责技能的注册/安装/卸载/搜索/marketplace/evolution 等生命周期管理 |
| **10.3.20** | **SkillDev 管道** | 技能开发模式的状态机管道，处理 7 个 `skilldev.*` RPC 方法，提供 Skill 创建/修改/评测/优化的全流程能力，包含 10 个阶段处理器 + 3 个挂起点 |

## 2. Python 对齐分析

### 2.1 Python SkillManager (Server)

- **文件**: `jiuwenswarm/server/runtime/skill/skill_manager.py` (3834 行)
- **独立实现**: 不导入 `openjiuwen.core.single_agent.skills.SkillManager`，与 agentcore 侧 SkillManager 无依赖关系
- **路由机制**: `_SKILL_ROUTES` / `_PLUGIN_ROUTES` 字典 → `getattr(skill_manager, handler_name)` 动态调用
- **36 个 handler 方法**: skills.* 30 个 + plugins.* 6 个
- **关键能力**: 本地/内置/marketplace 扫描、SkillNet 异步安装、ClawHub 下载、TeamSkillsHub CRUD、evolution 管理、git clone/pull、状态持久化

### 2.2 Python SkillDev 管道

- **文件**: `jiuwenswarm/server/runtime/skill/skilldev/` 目录下 10 个文件
- **状态机**: 14 个阶段 (INIT → PLAN → PLAN_CONFIRM(挂起) → GENERATE → VALIDATE → TEST_DESIGN → TEST_RUN → EVALUATE → REVIEW(挂起) → IMPROVE → PACKAGE → DESC_OPTIMIZE_CONFIRM(挂起) → DESC_OPTIMIZE → COMPLETED)，另有 ERROR 异常终态
- **3 个挂起点**: PLAN_CONFIRM / REVIEW / DESC_OPTIMIZE_CONFIRM
- **11 种事件类型**: STAGE_CHANGED / PROGRESS / ERROR / AGENT_THINKING / TEST_PROGRESS / CONFIRM_REQUEST / TODOS_UPDATE / ARTIFACT_READY / EVAL_READY / VALIDATE_RESULT / DESC_OPT_READY
- **Agent 调用**: `SkillDevContext.create_stage_agent()` 尚未实现 (NotImplementedError)，所有阶段处理器的 Agent 调用均为 stub 占位
- **已完整实现的阶段**: INIT / VALIDATE / PACKAGE（不依赖 Agent）
- **部分实现的阶段**: EVALUATE（Step2 Benchmark 聚合纯计算逻辑已实现，Grader/Analyst Agent 为 stub）

### 2.3 Python RPC 方法分发

- **skills.*** 和 **plugins.*** 走表驱动路由，通过 `JiuWenClaw._handle_skills_request` / `_handle_plugins_request` 分发到 SkillManager
- **skilldev.*** 走前缀匹配 + 懒初始化 SkillDevService，通过 `JiuWenClaw._handle_skilldev_request` 分发
- 部分 skills/plugins 方法（install/uninstall/toggle 等）处理完后触发 `create_instance()` 重建 Agent 实例

## 3. Go 实现设计

### 3.1 SkillManager (Server) — 10.3.19

#### 3.1.1 包结构

```
internal/swarm/server/runtime/skill/
├── doc.go               # 包文档
├── skill_manager.go     # Server 侧 SkillManager（独立实现）
├── skill_routes.go      # _skillRoutes / _pluginRoutes 函数 map 路由
└── state_utils.go       # 状态工具函数
```

#### 3.1.2 核心类型

```go
// SkillManager 服务端技能管理器。
//
// 独立实现，不内嵌 agentcore SkillManager。
// 对齐 Python: jiuwenswarm/server/runtime/skill/skill_manager.py
type SkillManager struct {
    skillsDir      string                    // 技能目录
    agentRootDir   string                    // Agent 根目录
    marketplaceDir string                    // marketplace 缓存目录
    stateFile      string                    // 状态文件路径 (skills_state.json)
    state          map[string]any            // 运行时状态
    skillnetInstallJobs map[string]*SkillNetInstallJob  // SkillNet 异步安装任务
    mu             sync.RWMutex              // 保护并发访问
}
```

#### 3.1.3 函数 map 路由

```go
// skillRoutes skills.* 方法的函数 map 路由。
var skillRoutes = map[schema.ReqMethod]func(ctx context.Context, sm *SkillManager, params map[string]any) (map[string]any, error){
    schema.ReqMethodSkillsList:              (*SkillManager).handleSkillsList,
    schema.ReqMethodSkillsGet:               (*SkillManager).handleSkillsGet,
    schema.ReqMethodSkillsInstalled:         (*SkillManager).handleSkillsInstalled,
    schema.ReqMethodSkillsToggle:            (*SkillManager).handleSkillsToggle,
    // ... 其余核心方法
}

// pluginRoutes plugins.* 方法的函数 map 路由。
var pluginRoutes = map[schema.ReqMethod]func(ctx context.Context, sm *SkillManager, params map[string]any) (map[string]any, error){
    schema.ReqMethodPluginsList:      (*SkillManager).handlePluginsList,
    // ... 其余 plugins 方法
}
```

#### 3.1.4 实现范围（核心优先）

**首期实现（核心功能）：**

| 方法 | 说明 |
|------|------|
| `handleSkillsList` | 返回所有可用 skill（本地 + builtin + marketplace） |
| `handleSkillsInstalled` | 返回已安装 marketplace 插件列表 |
| `handleSkillsGet` | 获取单个 skill 详情 |
| `handleSkillsToggle` | 切换已安装 skill 的 enabled 状态 |
| `handleSkillsInstall` | 安装 marketplace 中的 skill（git clone/pull + copytree） |
| `handleSkillsInstallBuiltin` | 安装内置技能 |
| `handleSkillsUninstall` | 卸载 skill |
| `handleSkillsImportLocal` | 从本地路径导入 skill |
| `handleSkillsMarketplaceList` | 列出已配置的 marketplace 源 |
| `handleSkillsMarketplaceAdd` | 添加 marketplace 源 |
| `handleSkillsMarketplaceRemove` | 删除 marketplace 源 |
| `handleSkillsMarketplaceToggle` | 启用/禁用 marketplace 源 |
| `handleSkillsEvolutionStatus` | 检查 evolutions.json 是否存在 |
| `handleSkillsEvolutionGet` | 获取 evolutions.json 内容 |
| `handleSkillsEvolutionSave` | 保存 evolutions.json 条目 |
| `handlePluginsList` | 列出所有已安装插件 |
| `handlePluginsInstall` | 安装插件 |
| `handlePluginsUninstall` | 卸载插件 |
| `handlePluginsEnable` | 启用插件 |
| `handlePluginsDisable` | 禁用插件 |
| `handlePluginsReload` | 重载插件 |

**后续补充（外部平台集成）：**

| 方法组 | 说明 | 原因 |
|--------|------|------|
| SkillNet (search/install/install_status/evaluate) | 在线技能市场 | 依赖外部 SkillNet API |
| ClawHub (get_token/set_token/search/download) | ClawHub 技能市场 | 依赖外部 ClawHub 服务 |
| TeamSkillsHub (init/validate/pack/info/search/install/publish/delete) | 团队技能仓库 | 依赖外部 Team Hub 服务 |

后续补充的 handler 在路由 map 中注册，返回 `"not implemented"` 错误。

#### 3.1.5 关键设计决策

- **独立实现**：Server 侧 SkillManager 不内嵌 agentcore SkillManager，与 Python 一致
- **函数 map 路由**：避免 Go 的 getattr 反射，用 `map[ReqMethod]func` 做类型安全的表驱动路由
- **状态持久化**：`skills_state.json` 存储已安装/禁用/配置等状态
- **marketplace 支持**：git clone/pull + copytree，对齐 Python 的 install 流程

### 3.2 SkillDev 管道 — 10.3.20

#### 3.2.1 包结构

```
internal/swarm/server/runtime/skill/skilldev/
├── doc.go                    # 包文档
├── schema.go                 # 枚举/状态/事件/挂起点/评测数据模型
├── deps.go                   # SkillDevDeps 依赖定义
├── store.go                  # StateStore 持久化
├── workspace.go              # WorkspaceProvider 工作区管理
├── context.go                # SkillDevContext 执行上下文
├── pipeline.go               # SkillDevPipeline 状态机编排
├── service.go                # SkillDevService 服务入口 + method dispatch
└── stages/
    ├── doc.go                # 阶段处理器包文档
    ├── base.go               # StageHandler 接口 + StageResult
    ├── init_stage.go         # INIT 阶段（✅ 完整实现）
    ├── plan_stage.go         # PLAN 阶段（Agent stub + 占位 plan）
    ├── generate_stage.go     # GENERATE 阶段（Agent stub + 占位文件）
    ├── validate_stage.go     # VALIDATE 阶段（✅ 完整实现）
    ├── test_design_stage.go  # TEST_DESIGN 阶段（Agent stub + 占位 evals）
    ├── test_run_stage.go     # TEST_RUN 阶段（Agent stub + 占位结果）
    ├── evaluate_stage.go     # EVALUATE 阶段（Grader/Analyst stub + ✅ Benchmark 聚合）
    ├── improve_stage.go      # IMPROVE 阶段（Agent stub + iteration+1）
    ├── package_stage.go      # PACKAGE 阶段（✅ 完整实现）
    └── desc_optimize_stage.go # DESC_OPTIMIZE 阶段（Agent stub + 跳到 COMPLETED）
```

#### 3.2.2 核心类型

**schema.go:**

```go
// SkillDevStage 技能开发阶段枚举。
type SkillDevStage string

const (
    SkillDevStageInit                SkillDevStage = "init"
    SkillDevStagePlan                SkillDevStage = "plan"
    SkillDevStagePlanConfirm         SkillDevStage = "plan_confirm"
    SkillDevStageGenerate            SkillDevStage = "generate"
    SkillDevStageValidate            SkillDevStage = "validate"
    SkillDevStageTestDesign          SkillDevStage = "test_design"
    SkillDevStageTestRun             SkillDevStage = "test_run"
    SkillDevStageEvaluate            SkillDevStage = "evaluate"
    SkillDevStageReview              SkillDevStage = "review"
    SkillDevStageImprove             SkillDevStage = "improve"
    SkillDevStagePackage             SkillDevStage = "package"
    SkillDevStageDescOptimizeConfirm SkillDevStage = "desc_optimize_confirm"
    SkillDevStageDescOptimize        SkillDevStage = "desc_optimize"
    SkillDevStageCompleted           SkillDevStage = "completed"
    SkillDevStageError               SkillDevStage = "error"
)

// SkillDevEventType 技能开发事件类型。
type SkillDevEventType string

const (
    SkillDevEventTypeStageChanged   SkillDevEventType = "skilldev.stage_changed"
    SkillDevEventTypeProgress       SkillDevEventType = "skilldev.progress"
    SkillDevEventTypeError          SkillDevEventType = "skilldev.error"
    SkillDevEventTypeAgentThinking  SkillDevEventType = "skilldev.agent_thinking"
    SkillDevEventTypeTestProgress   SkillDevEventType = "skilldev.test_progress"
    SkillDevEventTypeConfirmRequest SkillDevEventType = "skilldev.confirm_request"
    SkillDevEventTypeTodosUpdate    SkillDevEventType = "skilldev.todos_update"
    SkillDevEventTypeArtifactReady  SkillDevEventType = "skilldev.artifact_ready"
    SkillDevEventTypeEvalReady      SkillDevEventType = "skilldev.eval_ready"
    SkillDevEventTypeValidateResult SkillDevEventType = "skilldev.validate_result"
    SkillDevEventTypeDescOptReady   SkillDevEventType = "skilldev.desc_opt_ready"
)

// SkillDevState 运行时状态。
type SkillDevState struct {
    TaskID            string         `json:"task_id"`
    Stage             SkillDevStage  `json:"stage"`
    Mode              SkillDevTaskMode `json:"mode"`
    Iteration         int            `json:"iteration"`
    Input             map[string]any `json:"input"`
    ReferenceTexts    []string       `json:"reference_texts"`
    ExistingSkillMD   *string        `json:"existing_skill_md,omitempty"`
    Plan              map[string]any `json:"plan,omitempty"`
    PlanConfirmedAt   *string        `json:"plan_confirmed_at,omitempty"`
    Evals             map[string]any `json:"evals,omitempty"`
    EvalResults       map[string]any `json:"eval_results,omitempty"`
    FeedbackHistory   []map[string]any `json:"feedback_history"`
    DescOptimizeResult map[string]any `json:"desc_optimize_result,omitempty"`
    ZipPath           string         `json:"zip_path,omitempty"`
    ZipSize           int64          `json:"zip_size,omitempty"`
}
```

**deps.go:**

```go
// SkillDevDeps SkillDev 最小外部依赖定义。
type SkillDevDeps struct {
    ModelName         string
    ModelClientConfig map[string]any
    MCPToolsFactory   func() []any
    SysOpConfig       any
    StateStore        *StateStore
    WorkspaceProvider *WorkspaceProvider
}
```

**context.go:**

```go
// SkillDevContext 阶段执行上下文。
type SkillDevContext struct {
    TaskID     string
    Deps       *SkillDevDeps
    State      *SkillDevState
    Workspace  string
    eventQueue chan *SkillDevEvent
}
```

**pipeline.go:**

```go
// SkillDevPipeline 确定性状态机编排器。
type SkillDevPipeline struct {
    taskID string
    state  *SkillDevState
    deps   *SkillDevDeps
}
```

**service.go:**

```go
// SkillDevService 无状态请求处理器。
type SkillDevService struct {
    deps *SkillDevDeps
}

// methodDispatch 方法分发表。
var methodDispatch = map[schema.ReqMethod]func(...) (...){...}
```

#### 3.2.3 阶段处理器实现状态

| 阶段 | 实现状态 | 说明 |
|------|---------|------|
| INIT | ✅ 完整实现 | 解析资源文件、判断任务模式，不依赖 Agent |
| PLAN | Agent stub | 返回占位 plan dict + `logger.Warn` 标记待实现 |
| GENERATE | Agent stub | 创建占位文件 `# filepath\n<!-- 待实现 -->` + `logger.Warn` |
| VALIDATE | ✅ 完整实现 | 校验 SKILL.md YAML frontmatter 格式，不依赖 Agent |
| TEST_DESIGN | Agent stub | 返回占位 evals + `logger.Warn` |
| TEST_RUN | Agent stub | 写入占位 result.json/timing.json + `logger.Warn` |
| EVALUATE | Grader/Analyst stub + ✅ Benchmark 聚合 | Step1 Grader 全 FAIL + Step2 Benchmark 聚合完整实现 + Step3 Analyst 返回占位 notes |
| IMPROVE | Agent stub | 基于 feedback 历史 iteration+1 + `logger.Warn` |
| PACKAGE | ✅ 完整实现 | zip 打包 skill/ 目录，不依赖 Agent |
| DESC_OPTIMIZE | Agent stub | 直接跳到 COMPLETED + `logger.Warn` |

#### 3.2.4 ReqMethod 枚举补充

在 `internal/swarm/schema/req_method.go` 中新增 7 个 `skilldev.*` 常量：

```go
ReqMethodSkilldevStart    ReqMethod = "skilldev.start"
ReqMethodSkilldevRespond  ReqMethod = "skilldev.respond"
ReqMethodSkilldevStatus   ReqMethod = "skilldev.status"
ReqMethodSkilldevDownload ReqMethod = "skilldev.download"
ReqMethodSkilldevCancel   ReqMethod = "skilldev.cancel"
ReqMethodSkilldevFileList ReqMethod = "skilldev.file.list"
ReqMethodSkilldevFileRead ReqMethod = "skilldev.file.read"
```

#### 3.2.5 关键设计决策

- **对齐 Python 骨架**：状态机/schema/pipeline/service 完整实现，Agent 调用用 stub 占位
- **骨架可运行**：每个阶段都有 `execute(ctx) -> StageResult`，能正常流转阶段、checkpoint、推送事件、处理挂起点恢复
- **纯计算逻辑完整实现**：EVALUATE 的 Benchmark 聚合、VALIDATE 的格式校验、PACKAGE 的 zip 打包等不依赖 Agent 的逻辑全部完整实现
- **后续接入 Agent**：只需将 `logger.Warn` 占位替换为真实 `createStageAgent()` + Agent 调用

### 3.3 UapClaw 重命名 + Stub 回填 — 10.3.2

#### 3.3.1 JiuWenClaw → UapClaw 重命名

**Go 标识符重命名（全量）：**

| 原名 | 新名 |
|------|------|
| `JiuWenClaw` struct | `UapClaw` |
| `NewJiuWenClaw()` | `NewUapClaw()` |
| `(jw *JiuWenClaw)` 方法接收者 | `(uc *UapClaw)` |
| `jiowenclaw.go` | `uapclaw.go` |
| `jiowenclaw_test.go` | `uapclaw_test.go` |
| 日志消息 `"JiuWenClaw ..."` | `"UapClaw ..."` |
| doc.go 注释中 `JiuWenClaw` | `UapClaw` |

**不改的注释：** `// 对应 Python: JiuWenClaw...` 和 `// 对应 Python: JiuWenClawDeepAdapter...` 保留原样（Python 参考注释）

**影响文件：**

| 文件 | 修改范围 |
|------|---------|
| `runtime/jiowenclaw.go` → `runtime/uapclaw.go` | struct 名、构造函数、方法接收者 `jw→uc`、日志消息、注释 |
| `runtime/jiowenclaw_test.go` → `runtime/uapclaw_test.go` | 所有测试函数名和调用 |
| `runtime/agent_manager.go` | `stubAgent *UapClaw`、`NewUapClaw()`、返回类型 |
| `runtime/build_inputs.go` | 方法接收者 `(uc *UapClaw)` |
| `runtime/build_inputs_test.go` | 测试函数名和调用 |
| `runtime/doc.go` | 注释中 JiuWenClaw → UapClaw |
| `server/doc.go` | 注释中 JiuWenClaw → UapClaw |
| `adapter/interface.go` | 注释中 JiuWenClaw → UapClaw |
| `adapter/code_adapter.go` | 注释中 JiuWenClawDeepAdapter → UapClawDeepAdapter（非 Python 参考注释） |

#### 3.3.2 Stub 回填（6 处）

**字段回填：**

```go
// 原：skillManager interface{}
// 改：
skillManager *skill.SkillManager

// 原：skilldevService interface{}
// 改：
skilldevService *skilldev.SkillDevService
```

**方法回填 — ProcessMessage 中 Skills/Plugins/SkillDev 分支：**

```go
// 5. Skills 分支
if resp, err := uc.handleSkillsRequest(ctx, request); resp != nil {
    return resp, err
}
// 6. SkillDev 分支（非流式）
if resp, err := uc.handleSkillDevRequest(ctx, request); resp != nil {
    return resp, err
}
// 7. Plugins 分支
if resp, err := uc.handlePluginsRequest(ctx, request); resp != nil {
    return resp, err
}
```

**方法回填 — ProcessMessageStream 中 SkillDev 流式分支：**

```go
// 1. SkillDev 流式分支
if uc.isSkillDevMethod(request) {
    return uc.handleSkillDevStreamRequest(ctx, request)
}
```

**方法回填 — ensureAdapter 中 SkillManager 注入：**

```go
// 若 adapter 有 SetSkillManager 方法，注入 skillManager
if setter, ok := a.(interface{ SetSkillManager(*skill.SkillManager) }); ok {
    setter.SetSkillManager(uc.skillManager)
}
// 设置 skillManager 的 skillnet_install_complete_hook
```

**构造函数回填 — NewUapClaw：**

```go
func NewUapClaw() *UapClaw {
    return &UapClaw{
        sessionManager: NewSessionManager(),
        skillManager:   skill.NewSkillManager(/* workspaceDir */),
    }
}
```

#### 3.3.3 触发 Agent 实例重建的方法

以下 skills/plugins 方法处理完后需要调用 `CreateInstance()` 重建 Agent 实例：

- skills: `install`, `uninstall`, `import_local`, `toggle`, `skillnet_install`, `clawhub_download`, `teamskillshub_install`
- plugins: `install`, `uninstall`, `reload`

## 4. 实现顺序

1. **ReqMethod 枚举补充** — 新增 7 个 `skilldev.*` 常量
2. **SkillManager (Server)** — skill_manager.go + skill_routes.go + state_utils.go + doc.go
3. **SkillDev schema** — schema.go（枚举/状态/事件/挂起点/评测模型）
4. **SkillDev 基础设施** — deps.go + store.go + workspace.go + context.go
5. **SkillDev 阶段处理器** — stages/ 下 10 个阶段
6. **SkillDev pipeline + service** — pipeline.go + service.go
7. **SkillDev doc.go** — 包文档
8. **JiuWenClaw → UapClaw 重命名** — 全量重命名所有 Go 标识符和文件名
9. **10.3.2 Stub 回填** — 6 处 stub 替换为实际实现
10. **测试** — 各包单元测试

## 5. 对应 Python 代码映射

| Go 包/文件 | Python 路径 |
|-----------|------------|
| `skill/skill_manager.go` | `jiuwenswarm/server/runtime/skill/skill_manager.py` |
| `skill/skill_routes.go` | `jiuwenswarm/server/runtime/agent_adapter/interface.py` 中 `_SKILL_ROUTES` / `_PLUGIN_ROUTES` |
| `skill/state_utils.go` | `jiuwenswarm/server/runtime/skill/skilldev/state_utils.py` |
| `skill/skilldev/schema.go` | `jiuwenswarm/server/runtime/skill/skilldev/schema.py` |
| `skill/skilldev/deps.go` | `jiuwenswarm/server/runtime/skill/skilldev/deps.py` |
| `skill/skilldev/store.go` | `jiuwenswarm/server/runtime/skill/skilldev/store.py` |
| `skill/skilldev/workspace.go` | `jiuwenswarm/server/runtime/skill/skilldev/workspace.py` |
| `skill/skilldev/context.go` | `jiuwenswarm/server/runtime/skill/skilldev/context.py` |
| `skill/skilldev/pipeline.go` | `jiuwenswarm/server/runtime/skill/skilldev/pipeline.py` |
| `skill/skilldev/service.go` | `jiuwenswarm/server/runtime/skill/skilldev/service.py` |
| `skill/skilldev/stages/*.go` | `jiuwenswarm/server/runtime/skill/skilldev/stages/*.py` |

## 6. 禁止的行为

- ❌ `gateway/` 下任何包 import `server/` 下任何包（项目分层规则 6.1）
- ❌ Server SkillManager 内嵌 agentcore SkillManager（独立实现决策）
- ❌ SkillDev 阶段处理器中直接 import agentcore 的 ReActAgent（Agent 调用走 stub，后续通过 SkillDevDeps 注入）
- ❌ SkillManager 单文件拆分（1:1 对齐 Python 决策）
- ❌ 修改 `// 对应 Python: JiuWenClaw...` 形式的 Python 参考注释
