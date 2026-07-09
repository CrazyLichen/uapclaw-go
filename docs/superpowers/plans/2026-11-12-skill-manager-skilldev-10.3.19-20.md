# 10.3.19-20 SkillManager(Server) + SkillDev 管道 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 AgentServer 侧的技能管理器（SkillManager）和技能开发管道（SkillDev），回填 UapClaw 中的 6 处 skill stub，并完成 JiuWenClaw → UapClaw 全量重命名。

**Architecture:** SkillManager 独立实现（不内嵌 agentcore SkillManager），通过函数 map 路由分发 skills.*/plugins.* 请求。SkillDev 管道为确定性状态机，14 阶段 + 3 挂起点，Agent 推理部分用 stub 占位（对齐 Python），纯计算逻辑完整实现。UapClaw 门面中 skills/skilldev/plugins 分支短路返回，不走 ReAct Agent 对话流程。

**Tech Stack:** Go 1.22+, 标准库 archive/zip（打包）, encoding/json, os/path/filepath, sync, crypto/rand（task_id）

---

## 文件结构

### 新增文件

| 文件 | 职责 |
|------|------|
| `internal/swarm/server/runtime/skill/doc.go` | 包文档 |
| `internal/swarm/server/runtime/skill/skill_manager.go` | Server 侧 SkillManager 核心结构 + handler 方法 |
| `internal/swarm/server/runtime/skill/skill_routes.go` | _skillRoutes / _pluginRoutes 函数 map 路由 |
| `internal/swarm/server/runtime/skill/state_utils.go` | 状态工具函数（enabled/disable/normalize） |
| `internal/swarm/server/runtime/skill/skill_manager_test.go` | SkillManager 单元测试 |
| `internal/swarm/server/runtime/skill/skill_routes_test.go` | 路由 map 测试 |
| `internal/swarm/server/runtime/skill/state_utils_test.go` | state_utils 测试 |
| `internal/swarm/server/runtime/skill/skilldev/doc.go` | SkillDev 包文档 |
| `internal/swarm/server/runtime/skill/skilldev/schema.go` | 枚举/状态/事件/挂起点/评测数据模型 |
| `internal/swarm/server/runtime/skill/skilldev/deps.go` | SkillDevDeps 依赖定义 |
| `internal/swarm/server/runtime/skill/skilldev/store.go` | StateStore 持久化 |
| `internal/swarm/server/runtime/skill/skilldev/workspace.go` | WorkspaceProvider 工作区管理 |
| `internal/swarm/server/runtime/skill/skilldev/context.go` | SkillDevContext 执行上下文 |
| `internal/swarm/server/runtime/skill/skilldev/pipeline.go` | SkillDevPipeline 状态机编排 |
| `internal/swarm/server/runtime/skill/skilldev/service.go` | SkillDevService 服务入口 |
| `internal/swarm/server/runtime/skill/skilldev/schema_test.go` | schema 测试 |
| `internal/swarm/server/runtime/skill/skilldev/store_test.go` | store 测试 |
| `internal/swarm/server/runtime/skill/skilldev/workspace_test.go` | workspace 测试 |
| `internal/swarm/server/runtime/skill/skilldev/pipeline_test.go` | pipeline 测试 |
| `internal/swarm/server/runtime/skill/skilldev/service_test.go` | service 测试 |
| `internal/swarm/server/runtime/skill/skilldev/stages/doc.go` | 阶段处理器包文档 |
| `internal/swarm/server/runtime/skill/skilldev/stages/base.go` | StageHandler 接口 + StageResult |
| `internal/swarm/server/runtime/skill/skilldev/stages/init_stage.go` | INIT 阶段（✅ 完整实现） |
| `internal/swarm/server/runtime/skill/skilldev/stages/plan_stage.go` | PLAN 阶段（Agent stub） |
| `internal/swarm/server/runtime/skill/skilldev/stages/generate_stage.go` | GENERATE 阶段（Agent stub） |
| `internal/swarm/server/runtime/skill/skilldev/stages/validate_stage.go` | VALIDATE 阶段（✅ 完整实现） |
| `internal/swarm/server/runtime/skill/skilldev/stages/test_design_stage.go` | TEST_DESIGN 阶段（Agent stub） |
| `internal/swarm/server/runtime/skill/skilldev/stages/test_run_stage.go` | TEST_RUN 阶段（Agent stub） |
| `internal/swarm/server/runtime/skill/skilldev/stages/evaluate_stage.go` | EVALUATE 阶段（Grader/Analyst stub + Benchmark ✅） |
| `internal/swarm/server/runtime/skill/skilldev/stages/improve_stage.go` | IMPROVE 阶段（Agent stub） |
| `internal/swarm/server/runtime/skill/skilldev/stages/package_stage.go` | PACKAGE 阶段（✅ 完整实现） |
| `internal/swarm/server/runtime/skill/skilldev/stages/desc_optimize_stage.go` | DESC_OPTIMIZE 阶段（Agent stub） |
| `internal/swarm/server/runtime/skill/skilldev/stages/stages_test.go` | 阶段处理器统一测试 |

### 修改文件

| 文件 | 修改范围 |
|------|---------|
| `internal/swarm/schema/req_method.go` | 新增 7 个 skilldev.* 常量 + AllReqMethods 补充 |
| `internal/swarm/server/runtime/jiowenclaw.go` → `uapclaw.go` | JiuWenClaw→UapClaw 重命名 + 6 处 stub 回填 |
| `internal/swarm/server/runtime/jiowenclaw_test.go` → `uapclaw_test.go` | 重命名 + 测试适配 |
| `internal/swarm/server/runtime/agent_manager.go` | JiuWenClaw→UapClaw 类型引用 |
| `internal/swarm/server/runtime/build_inputs.go` | 方法接收者 jw→uc |
| `internal/swarm/server/runtime/build_inputs_test.go` | 测试函数名和调用 |
| `internal/swarm/server/runtime/doc.go` | 注释更新 |
| `internal/swarm/server/doc.go` | 注释更新 |
| `internal/swarm/server/adapter/interface.go` | 注释更新 |
| `internal/swarm/server/adapter/code_adapter.go` | 注释更新 |
| `IMPLEMENTATION_PLAN.md` | 10.3.19-20 状态更新 |

---

## Task 1: ReqMethod 枚举补充 skilldev.* 常量

**Files:**
- Modify: `internal/swarm/schema/req_method.go`

- [ ] **Step 1: 在 const 块中新增 `// ─── 技能开发 ───` 分组，添加 7 个 skilldev.* 常量**

在 `// ─── 插件 ───` 分组之后添加：

```go
// ─── 技能开发 ───

// ReqMethodSkilldevStart 启动技能开发任务
ReqMethodSkilldevStart ReqMethod = "skilldev.start"
// ReqMethodSkilldevRespond 响应挂起点确认
ReqMethodSkilldevRespond ReqMethod = "skilldev.respond"
// ReqMethodSkilldevStatus 查询技能开发状态
ReqMethodSkilldevStatus ReqMethod = "skilldev.status"
// ReqMethodSkilldevDownload 下载技能开发产物
ReqMethodSkilldevDownload ReqMethod = "skilldev.download"
// ReqMethodSkilldevCancel 取消技能开发任务
ReqMethodSkilldevCancel ReqMethod = "skilldev.cancel"
// ReqMethodSkilldevFileList 获取工作区文件列表
ReqMethodSkilldevFileList ReqMethod = "skilldev.file.list"
// ReqMethodSkilldevFileRead 读取工作区文件内容
ReqMethodSkilldevFileRead ReqMethod = "skilldev.file.read"
```

- [ ] **Step 2: 在 `AllReqMethods()` 返回切片中追加对应常量**

在插件分组之后添加：

```go
// 技能开发
ReqMethodSkilldevStart,
ReqMethodSkilldevRespond,
ReqMethodSkilldevStatus,
ReqMethodSkilldevDownload,
ReqMethodSkilldevCancel,
ReqMethodSkilldevFileList,
ReqMethodSkilldevFileRead,
```

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/schema/... -run TestReqMethod -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/schema/req_method.go
git commit -m "feat(schema): 新增 7 个 skilldev.* ReqMethod 枚举常量"
```

---

## Task 2: SkillManager (Server) — state_utils.go

**Files:**
- Create: `internal/swarm/server/runtime/skill/state_utils.go`
- Create: `internal/swarm/server/runtime/skill/state_utils_test.go`

- [ ] **Step 1: 创建 state_utils.go**

对齐 Python: `jiuwenswarm/server/runtime/skill/skilldev/state_utils.py`

包含以下纯函数（无 SkillManager 依赖）：
- `GetStateFile() string` — 返回 skills_state.json 路径
- `NormalizeSkillConfigs(rawConfigs any) map[string]map[string]bool` — 归一化 per-skill 配置
- `GetRegisteredSkillNames(state map[string]any) map[string]struct{}` — 返回所有已注册 skill 名称
- `GetSkillEnabled(state map[string]any, skillName string) bool` — 读取 enabled 标志（默认 true）
- `SetSkillEnabled(state map[string]any, skillName string, enabled bool)` — 写入 enabled 标志
- `ListDisabledSkills(state map[string]any) []string` — 返回已禁用 skill 名称列表
- `ListExecutionDisabledSkills(state map[string]any) []string` — 返回已安装且已禁用的 skill 名称列表
- `LoadExecutionDisabledSkills() []string` — 从 skills_state.json 加载并返回禁用列表
- `FilterVisibleSkillNames(names []string) []string` — 过滤掉已禁用的 skill 名称

- [ ] **Step 2: 创建 state_utils_test.go**

覆盖：NormalizeSkillConfigs、GetSkillEnabled/SetSkillEnabled、ListDisabledSkills、FilterVisibleSkillNames 的基本场景。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/runtime/skill/... -run TestStateUtils -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/server/runtime/skill/state_utils.go internal/swarm/server/runtime/skill/state_utils_test.go
git commit -m "feat(skill): 实现 state_utils 状态工具函数"
```

---

## Task 3: SkillManager (Server) — skill_manager.go 核心结构 + 核心优先 handler

**Files:**
- Create: `internal/swarm/server/runtime/skill/skill_manager.go`
- Create: `internal/swarm/server/runtime/skill/skill_manager_test.go`

- [ ] **Step 1: 创建 skill_manager.go**

**SkillManager struct** 字段：
- `skillsDir string` — 技能目录
- `agentRootDir string` — Agent 根目录
- `marketplaceDir string` — marketplace 缓存目录
- `stateFile string` — skills_state.json 路径
- `state map[string]any` — 运行时状态
- `skillnetInstallJobs map[string]*SkillNetInstallJob` — SkillNet 异步安装任务
- `skillnetInstallCompleteHook func(context.Context) error` — 安装完成回调
- `mu sync.RWMutex`

**构造函数**: `NewSkillManager(workspaceDir string) *SkillManager`

**核心优先 handler 方法（21 个）**：
- `handleSkillsList` / `handleSkillsInstalled` / `handleSkillsGet` / `handleSkillsToggle`
- `handleSkillsInstall` / `handleSkillsInstallBuiltin` / `handleSkillsUninstall` / `handleSkillsImportLocal`
- `handleSkillsMarketplaceList` / `handleSkillsMarketplaceAdd` / `handleSkillsMarketplaceRemove` / `handleSkillsMarketplaceToggle`
- `handleSkillsEvolutionStatus` / `handleSkillsEvolutionGet` / `handleSkillsEvolutionSave`
- `handlePluginsList` / `handlePluginsInstall` / `handlePluginsUninstall` / `handlePluginsEnable` / `handlePluginsDisable` / `handlePluginsReload`

**后续补充 handler（存根，返回 errNotImplemented）**：
- SkillNet: search / install / install_status / evaluate
- ClawHub: get_token / set_token / search / download
- TeamSkillsHub: init / validate / pack / info / search / install / publish / delete

**关键私有方法**：
- `loadState()` / `saveState()` — 状态持久化
- `scanLocalSkills()` / `scanBuiltinSkills()` / `scanMarketplaceSkills()` — 扫描技能
- `parseSkillMD()` — 解析 SKILL.md YAML frontmatter

方法签名统一为：`func (sm *SkillManager) handleXxx(ctx context.Context, params map[string]any) (map[string]any, error)`

- [ ] **Step 2: 创建 skill_manager_test.go**

覆盖：NewSkillManager、handleSkillsList（空目录）、handleSkillsGet（不存在）、handleSkillsToggle、handlePluginsList 等基本场景。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/runtime/skill/... -run TestSkillManager -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/server/runtime/skill/skill_manager.go internal/swarm/server/runtime/skill/skill_manager_test.go
git commit -m "feat(skill): 实现 Server 侧 SkillManager 核心结构与 handler"
```

---

## Task 4: SkillManager (Server) — skill_routes.go 函数 map 路由

**Files:**
- Create: `internal/swarm/server/runtime/skill/skill_routes.go`
- Create: `internal/swarm/server/runtime/skill/skill_routes_test.go`

- [ ] **Step 1: 创建 skill_routes.go**

```go
// skillRoutes skills.* 方法的函数 map 路由。
// 对齐 Python: JiuWenClaw._SKILL_ROUTES
var skillRoutes = map[schema.ReqMethod]func(ctx context.Context, sm *SkillManager, params map[string]any) (map[string]any, error){
    schema.ReqMethodSkillsList:              (*SkillManager).handleSkillsList,
    schema.ReqMethodSkillsInstalled:         (*SkillManager).handleSkillsInstalled,
    schema.ReqMethodSkillsGet:               (*SkillManager).handleSkillsGet,
    schema.ReqMethodSkillsToggle:            (*SkillManager).handleSkillsToggle,
    schema.ReqMethodSkillsInstall:           (*SkillManager).handleSkillsInstall,
    schema.ReqMethodSkillsUninstall:         (*SkillManager).handleSkillsUninstall,
    schema.ReqMethodSkillsImportLocal:       (*SkillManager).handleSkillsImportLocal,
    schema.ReqMethodSkillsMarketplaceList:   (*SkillManager).handleSkillsMarketplaceList,
    schema.ReqMethodSkillsMarketplaceAdd:    (*SkillManager).handleSkillsMarketplaceAdd,
    schema.ReqMethodSkillsMarketplaceRemove: (*SkillManager).handleSkillsMarketplaceRemove,
    schema.ReqMethodSkillsMarketplaceToggle: (*SkillManager).handleSkillsMarketplaceToggle,
    schema.ReqMethodSkillsEvolutionStatus:   (*SkillManager).handleSkillsEvolutionStatus,
    schema.ReqMethodSkillsEvolutionGet:      (*SkillManager).handleSkillsEvolutionGet,
    schema.ReqMethodSkillsEvolutionSave:     (*SkillManager).handleSkillsEvolutionSave,
    // 后续补充：SkillNet / ClawHub / TeamSkillsHub
}

// pluginRoutes plugins.* 方法的函数 map 路由。
// 对齐 Python: JiuWenClaw._PLUGIN_ROUTES
var pluginRoutes = map[schema.ReqMethod]func(ctx context.Context, sm *SkillManager, params map[string]any) (map[string]any, error){
    schema.ReqMethodPluginsList:      (*SkillManager).handlePluginsList,
    schema.ReqMethodPluginsInstall:   (*SkillManager).handlePluginsInstall,
    schema.ReqMethodPluginsUninstall: (*SkillManager).handlePluginsUninstall,
    schema.ReqMethodPluginsEnable:    (*SkillManager).handlePluginsEnable,
    schema.ReqMethodPluginsDisable:   (*SkillManager).handlePluginsDisable,
    schema.ReqMethodPluginsReload:    (*SkillManager).handlePluginsReload,
}

// skilldevMethods SkillDev 请求方法集合。
// 对齐 Python: JiuWenClaw._SKILLDEV_METHODS
var skilldevMethods = map[schema.ReqMethod]bool{
    schema.ReqMethodSkilldevStart:    true,
    schema.ReqMethodSkilldevRespond:  true,
    schema.ReqMethodSkilldevStatus:   true,
    schema.ReqMethodSkilldevDownload: true,
    schema.ReqMethodSkilldevCancel:   true,
    schema.ReqMethodSkilldevFileList: true,
    schema.ReqMethodSkilldevFileRead: true,
}
```

同时提供辅助函数：
- `IsSkillMethod(m schema.ReqMethod) bool` — 判断是否为 skills.* 方法
- `IsPluginMethod(m schema.ReqMethod) bool` — 判断是否为 plugins.* 方法
- `IsSkillDevMethod(m schema.ReqMethod) bool` — 判断是否为 skilldev.* 方法
- `NeedsRebuild(m schema.ReqMethod) bool` — 判断方法是否需要重建 Agent 实例

- [ ] **Step 2: 创建 skill_routes_test.go**

覆盖：skillRoutes/pluginRoutes map 完整性（所有 skills.*/plugins.* ReqMethod 都有对应路由）、IsSkillMethod/IsPluginMethod/IsSkillDevMethod 判断、NeedsRebuild 判断。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/runtime/skill/... -run TestSkillRoutes -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/server/runtime/skill/skill_routes.go internal/swarm/server/runtime/skill/skill_routes_test.go
git commit -m "feat(skill): 实现 skills/plugins/skilldev 函数 map 路由"
```

---

## Task 5: SkillManager — doc.go

**Files:**
- Create: `internal/swarm/server/runtime/skill/doc.go`

- [ ] **Step 1: 创建 doc.go**

包含包功能概述、文件目录树、对应 Python 代码路径、核心类型索引。

- [ ] **Step 2: 提交**

```bash
git add internal/swarm/server/runtime/skill/doc.go
git commit -m "docs(skill): 添加包文档"
```

---

## Task 6: SkillDev — schema.go 枚举/状态/事件/挂起点/评测模型

**Files:**
- Create: `internal/swarm/server/runtime/skill/skilldev/schema.go`
- Create: `internal/swarm/server/runtime/skill/skilldev/schema_test.go`

- [ ] **Step 1: 创建 schema.go**

对齐 Python: `jiuwenswarm/server/runtime/skill/skilldev/schema.py`

包含：
- **SkillDevStage** 枚举（15 个值：INIT/PLAN/PLAN_CONFIRM/GENERATE/VALIDATE/TEST_DESIGN/TEST_RUN/EVALUATE/REVIEW/IMPROVE/PACKAGE/DESC_OPTIMIZE_CONFIRM/DESC_OPTIMIZE/COMPLETED/ERROR）
- **SkillDevTaskMode** 枚举（3 个值：CREATE/CREATE_WITH_RESOURCES/MODIFY）
- **SkillDevEventType** 枚举（11 个值）
- **SkillDevEvent** struct（EventType/Payload/TaskID）
- **SkillDevState** struct（完整字段 + toCheckpointDict/fromCheckpointDict/toStatusDict 序列化方法）
- **SuspensionConfig** struct（ConfirmType/Title/Message/Actions/ExtractData/OnResume/NextStage）
- **SuspensionPoints** 全局变量（3 个挂起点配置）
- **StageGroup** struct + **stageGroups** 变量（6 个阶段分组）
- **ComputeTodos** 函数
- **GenerateTaskID** 函数
- **DetermineTaskMode** 函数
- **评测数据模型**：EvalCase/EvalSet/GradingExpectation/GradingResult/RunTiming/MetricStats/BenchmarkRun/Benchmark

- [ ] **Step 2: 创建 schema_test.go**

覆盖：SkillDevStage 字符串值、SkillDevState 序列化/反序列化、ComputeTodos 各阶段、GenerateTaskID 唯一性、DetermineTaskMode 三种模式、SuspensionPoints 完整性。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/runtime/skill/skilldev/... -run TestSchema -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/server/runtime/skill/skilldev/schema.go internal/swarm/server/runtime/skill/skilldev/schema_test.go
git commit -m "feat(skilldev): 实现 schema 枚举/状态/事件/挂起点/评测模型"
```

---

## Task 7: SkillDev — deps.go + store.go + workspace.go 基础设施

**Files:**
- Create: `internal/swarm/server/runtime/skill/skilldev/deps.go`
- Create: `internal/swarm/server/runtime/skill/skilldev/store.go`
- Create: `internal/swarm/server/runtime/skill/skilldev/workspace.go`
- Create: `internal/swarm/server/runtime/skill/skilldev/store_test.go`
- Create: `internal/swarm/server/runtime/skill/skilldev/workspace_test.go`

- [ ] **Step 1: 创建 deps.go**

```go
// SkillDevDeps SkillDev 最小外部依赖定义。
// 对齐 Python: skilldev/deps.py
type SkillDevDeps struct {
    ModelName         string
    ModelClientConfig map[string]any
    MCPToolsFactory   func() []any
    SysOpConfig       any
    StateStore        *StateStore
    WorkspaceProvider *WorkspaceProvider
}
```

- [ ] **Step 2: 创建 store.go**

对齐 Python: `skilldev/store.py`

包含 StateStore struct + 方法：SaveState / LoadState / ListTasks

- [ ] **Step 3: 创建 workspace.go**

对齐 Python: `skilldev/workspace.py`

包含 WorkspaceProvider struct + 方法：GetLocalPath / EnsureLocal / SyncToRemote（空操作）

- [ ] **Step 4: 创建 store_test.go + workspace_test.go**

覆盖：SaveState+LoadState 往返、EnsureLocal 目录创建。

- [ ] **Step 5: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/runtime/skill/skilldev/... -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/swarm/server/runtime/skill/skilldev/deps.go internal/swarm/server/runtime/skill/skilldev/store.go internal/swarm/server/runtime/skill/skilldev/workspace.go internal/swarm/server/runtime/skill/skilldev/store_test.go internal/swarm/server/runtime/skill/skilldev/workspace_test.go
git commit -m "feat(skilldev): 实现 deps/store/workspace 基础设施"
```

---

## Task 8: SkillDev — context.go 执行上下文

**Files:**
- Create: `internal/swarm/server/runtime/skill/skilldev/context.go`

- [ ] **Step 1: 创建 context.go**

对齐 Python: `skilldev/context.py`

包含：
- **SkillDevContext** struct（TaskID/Deps/State/Workspace/eventQueue）
- **Emit** 方法 — 向事件队列写入事件
- **CreateStageAgent** 方法 — stub，`logger.Warn` + `return nil, errNotImplemented`
- **RegisterTools** 方法 — stub，`logger.Warn` + `return`

- [ ] **Step 2: 提交**

```bash
git add internal/swarm/server/runtime/skill/skilldev/context.go
git commit -m "feat(skilldev): 实现 SkillDevContext 执行上下文"
```

---

## Task 9: SkillDev — stages/ 10 个阶段处理器

**Files:**
- Create: `internal/swarm/server/runtime/skill/skilldev/stages/doc.go`
- Create: `internal/swarm/server/runtime/skill/skilldev/stages/base.go`
- Create: `internal/swarm/server/runtime/skill/skilldev/stages/init_stage.go`
- Create: `internal/swarm/server/runtime/skill/skilldev/stages/plan_stage.go`
- Create: `internal/swarm/server/runtime/skill/skilldev/stages/generate_stage.go`
- Create: `internal/swarm/server/runtime/skill/skilldev/stages/validate_stage.go`
- Create: `internal/swarm/server/runtime/skill/skilldev/stages/test_design_stage.go`
- Create: `internal/swarm/server/runtime/skill/skilldev/stages/test_run_stage.go`
- Create: `internal/swarm/server/runtime/skill/skilldev/stages/evaluate_stage.go`
- Create: `internal/swarm/server/runtime/skill/skilldev/stages/improve_stage.go`
- Create: `internal/swarm/server/runtime/skill/skilldev/stages/package_stage.go`
- Create: `internal/swarm/server/runtime/skill/skilldev/stages/desc_optimize_stage.go`
- Create: `internal/swarm/server/runtime/skill/skilldev/stages/stages_test.go`

- [ ] **Step 1: 创建 base.go**

```go
// StageResult 阶段执行结果。
type StageResult struct {
    NextStage SkillDevStage
}

// StageHandler 阶段处理器接口。
type StageHandler interface {
    Execute(ctx context.Context, sctx *skilldevctx.SkillDevContext) (*StageResult, error)
}
```

- [ ] **Step 2: 创建 init_stage.go**（✅ 完整实现）

对齐 Python: `stages/init_stage.py`

逻辑：判断任务模式、解析资源文件（base64→文件）、解析已有 skill zip。

- [ ] **Step 3: 创建 plan_stage.go**（Agent stub）

对齐 Python: `stages/plan_stage.py`

逻辑框架：emit PROGRESS → 调 `_generatePlan`(stub) → 设置 state.Plan → emit PROGRESS → 返回 PLAN_CONFIRM。`_generatePlan` 返回占位 plan dict + `logger.Warn`。

- [ ] **Step 4: 创建 generate_stage.go**（Agent stub）

对齐 Python: `stages/generate_stage.py`

逻辑框架：emit PROGRESS → `_generateAllFiles`(stub) → emit ARTIFACT_READY → 返回 VALIDATE。`_generateAllFiles` 为每个文件写占位内容 + `logger.Warn`。

- [ ] **Step 5: 创建 validate_stage.go**（✅ 完整实现）

对齐 Python: `stages/validate_stage.py`

逻辑：校验 SKILL.md YAML frontmatter（name kebab-case≤64字符、description≤1024字符、无尖括号、仅允许的 key）。成功→TEST_DESIGN，失败→GENERATE。

- [ ] **Step 6: 创建 test_design_stage.go**（Agent stub）

对齐 Python: `stages/test_design_stage.py`

逻辑框架：Agent stub 返回占位 evals + `logger.Warn`，跳转 TEST_RUN。

- [ ] **Step 7: 创建 test_run_stage.go**（Agent stub）

对齐 Python: `stages/test_run_stage.py`

逻辑框架：并行占位（写入 placeholder result.json/timing.json）+ `logger.Warn`，跳转 EVALUATE。

- [ ] **Step 8: 创建 evaluate_stage.go**（Grader/Analyst stub + ✅ Benchmark 聚合）

对齐 Python: `stages/evaluate_stage.py`

三步：
1. Step1 Grader — stub，全 FAIL + `logger.Warn`
2. Step2 Benchmark 聚合 — **完整实现**（遍历 grading.json + timing.json，计算 mean/stddev/min/max + delta）
3. Step3 Analyst — stub，返回 `["评测分析 Agent 尚未接入"]` + `logger.Warn`

跳转 REVIEW。

- [ ] **Step 9: 创建 improve_stage.go**（Agent stub）

对齐 Python: `stages/improve_stage.py`

逻辑框架：基于 feedback 历史 iteration+1 + `logger.Warn`，跳转 TEST_RUN。

- [ ] **Step 10: 创建 package_stage.go**（✅ 完整实现）

对齐 Python: `stages/package_stage.py`

逻辑：将 skill/ 打包为 {skill_name}.skill（zip 格式，排除 __pycache__/node_modules/.git/evals/ 等），跳转 DESC_OPTIMIZE_CONFIRM。

- [ ] **Step 11: 创建 desc_optimize_stage.go**（Agent stub）

对齐 Python: `stages/desc_optimize_stage.py`

逻辑框架：Agent stub + `logger.Warn`，直接跳转 COMPLETED。

- [ ] **Step 12: 创建 doc.go + stages_test.go**

stages_test.go 覆盖：ValidateStage 格式校验、PackageStage zip 打包、InitStage 任务模式判断、其余阶段 stub 返回正确 NextStage。

- [ ] **Step 13: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/runtime/skill/skilldev/stages/... -v`
Expected: PASS

- [ ] **Step 14: 提交**

```bash
git add internal/swarm/server/runtime/skill/skilldev/stages/
git commit -m "feat(skilldev): 实现 10 个阶段处理器（完整+stub）"
```

---

## Task 10: SkillDev — pipeline.go + service.go

**Files:**
- Create: `internal/swarm/server/runtime/skill/skilldev/pipeline.go`
- Create: `internal/swarm/server/runtime/skill/skilldev/service.go`
- Create: `internal/swarm/server/runtime/skill/skilldev/pipeline_test.go`
- Create: `internal/swarm/server/runtime/skill/skilldev/service_test.go`

- [ ] **Step 1: 创建 pipeline.go**

对齐 Python: `skilldev/pipeline.py`

核心逻辑：
- **STAGE_HANDLERS** map — 10 个阶段→Handler 映射
- **Run()** — 从当前阶段执行到挂起点或终态，yield 事件
- **Resume(data)** — 从挂起点恢复，调用 onResume + 计算 nextStage → 继续执行 Run()
- **_emit()** — 向事件队列写入事件
- **_checkpoint()** — 持久化状态 + 同步工作区

- [ ] **Step 2: 创建 service.go**

对齐 Python: `skilldev/service.py`

核心逻辑：
- **SkillDevService** struct（deps 字段）
- **Handle(ctx, request)** — 统一入口，根据 methodDispatch 分发
- **methodDispatch** map — 7 个 skilldev.* 方法→handler 映射
- **_handleStart** — 创建 Pipeline → pipeline.Run()
- **_handleRespond** — 加载 state → Pipeline.Resume()
- **_handleStatus** — 查状态/列任务
- **_handleDownload** — 读取 zip 产物 → base64 编码返回
- **_handleCancel** — stub
- **_handleFileList** — 递归构建文件树
- **_handleFileRead** — 读取文件内容（路径安全校验）

- [ ] **Step 3: 创建 pipeline_test.go**

覆盖：Pipeline Init→Plan→PlanConfirm(挂起)、Resume PlanConfirm→Generate→Validate、Error 阶段处理。

- [ ] **Step 4: 创建 service_test.go**

覆盖：SkillDevService Handle 分发、_handleStatus、_handleFileList。

- [ ] **Step 5: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/runtime/skill/skilldev/... -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/swarm/server/runtime/skill/skilldev/pipeline.go internal/swarm/server/runtime/skill/skilldev/service.go internal/swarm/server/runtime/skill/skilldev/pipeline_test.go internal/swarm/server/runtime/skill/skilldev/service_test.go
git commit -m "feat(skilldev): 实现 pipeline 状态机编排 + service 服务入口"
```

---

## Task 11: SkillDev — doc.go

**Files:**
- Create: `internal/swarm/server/runtime/skill/skilldev/doc.go`

- [ ] **Step 1: 创建 doc.go**

包含包功能概述、文件目录树、对应 Python 代码路径、核心类型索引。

- [ ] **Step 2: 提交**

```bash
git add internal/swarm/server/runtime/skill/skilldev/doc.go
git commit -m "docs(skilldev): 添加包文档"
```

---

## Task 12: JiuWenClaw → UapClaw 全量重命名

**Files:**
- Rename: `internal/swarm/server/runtime/jiowenclaw.go` → `internal/swarm/server/runtime/uapclaw.go`
- Rename: `internal/swarm/server/runtime/jiowenclaw_test.go` → `internal/swarm/server/runtime/uapclaw_test.go`
- Modify: `internal/swarm/server/runtime/agent_manager.go`
- Modify: `internal/swarm/server/runtime/build_inputs.go`
- Modify: `internal/swarm/server/runtime/build_inputs_test.go`
- Modify: `internal/swarm/server/runtime/doc.go`
- Modify: `internal/swarm/server/doc.go`
- Modify: `internal/swarm/server/adapter/interface.go`
- Modify: `internal/swarm/server/adapter/code_adapter.go`

- [ ] **Step 1: 重命名文件**

```bash
git mv internal/swarm/server/runtime/jiowenclaw.go internal/swarm/server/runtime/uapclaw.go
git mv internal/swarm/server/runtime/jiowenclaw_test.go internal/swarm/server/runtime/uapclaw_test.go
```

- [ ] **Step 2: 在 uapclaw.go 中执行全量替换**

- `JiuWenClaw` → `UapClaw`（struct 名、类型引用、注释）
- `NewJiuWenClaw` → `NewUapClaw`（构造函数名）
- `(jw *JiuWenClaw)` → `(uc *UapClaw)`（所有方法接收者）
- `jw.` → `uc.`（所有方法体内引用）
- 日志消息中 `"JiuWenClaw ..."` → `"UapClaw ..."`
- **不改** `// 对应 Python: JiuWenClaw...` 形式的注释

- [ ] **Step 3: 在 uapclaw_test.go 中执行全量替换**

- `TestNewJiuWenClaw` → `TestNewUapClaw`
- `TestJiuWenClaw_` → `TestUapClaw_`
- `NewJiuWenClaw()` → `NewUapClaw()`
- `jw :=` → `uc :=`
- `jw.` → `uc.`

- [ ] **Step 4: 在 agent_manager.go 中替换**

- `*JiuWenClaw` → `*UapClaw`
- `NewJiuWenClaw()` → `NewUapClaw()`
- 注释中 `JiuWenClaw` → `UapClaw`

- [ ] **Step 5: 在 build_inputs.go / build_inputs_test.go 中替换**

- `(jw *JiuWenClaw)` → `(uc *UapClaw)`
- `NewJiuWenClaw()` → `NewUapClaw()`
- `TestJiuWenClaw_` → `TestUapClaw_`
- `jw :=` → `uc :=`

- [ ] **Step 6: 在 doc.go 文件中替换非 Python 参考注释**

- runtime/doc.go: `JiuWenClaw` → `UapClaw`
- server/doc.go: `JiuWenClaw` → `UapClaw`
- adapter/interface.go: `JiuWenClaw` → `UapClaw`（非 Python 参考注释）
- adapter/code_adapter.go: `JiuWenClawDeepAdapter` → `UapClawDeepAdapter`（非 Python 参考注释）

- [ ] **Step 7: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/... -v`
Expected: PASS

- [ ] **Step 8: 提交**

```bash
git add -A
git commit -m "refactor: JiuWenClaw → UapClaw 全量重命名"
```

---

## Task 13: UapClaw — 6 处 stub 回填

**Files:**
- Modify: `internal/swarm/server/runtime/uapclaw.go`

- [ ] **Step 1: 字段类型回填**

```go
// 原：skillManager interface{}
// 改：
skillManager *skill.SkillManager

// 原：skilldevService interface{}
// 改：
skilldevService *skilldev.SkillDevService
```

- [ ] **Step 2: 构造函数回填**

```go
func NewUapClaw() *UapClaw {
    return &UapClaw{
        sessionManager: NewSessionManager(),
        skillManager:   skill.NewSkillManager(""),
    }
}
```

- [ ] **Step 3: ProcessMessage 中 Skills/SkillDev/Plugins 分支回填**

替换第 78-81 行的注释 stub 为：

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

新增三个 handler 方法：
- `handleSkillsRequest` — 查 skillRoutes，调用对应 handler，若 NeedsRebuild 则 CreateInstance
- `handleSkillDevRequest` — 查 skilldevMethods，委托 SkillDevService
- `handlePluginsRequest` — 查 pluginRoutes，调用对应 handler，若 NeedsRebuild 则 CreateInstance

- [ ] **Step 4: ProcessMessageStream 中 SkillDev 流式分支回填**

替换第 128-129 行的注释 stub 为：

```go
// 1. SkillDev 流式分支
if skill.IsSkillDevMethod(request.ReqMethod) {
    return uc.handleSkillDevStreamRequest(ctx, request)
}
```

新增 `handleSkillDevStreamRequest` 方法。

- [ ] **Step 5: ensureAdapter 中 SkillManager 注入回填**

替换第 369-370 行的注释 stub 为：

```go
// 若 adapter 有 SetSkillManager 方法，注入 skillManager
if setter, ok := a.(interface{ SetSkillManager(*skill.SkillManager) }); ok {
    setter.SetSkillManager(uc.skillManager)
}
```

- [ ] **Step 6: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/runtime/... -v`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/swarm/server/runtime/uapclaw.go
git commit -m "feat(uapclaw): 回填 skills/skilldev/plugins 6 处 stub"
```

---

## Task 14: 全量编译 + 测试验证

**Files:**
- All

- [ ] **Step 1: 检查残留 go 编译进程**

Run: `pgrep -f 'go (build|test)'`

如有残留，kill 后再编译。

- [ ] **Step 2: 设置代理并全量编译**

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uapclaw-gateway && go build ./...
```

Expected: 编译成功，无错误

- [ ] **Step 3: 运行全量单元测试**

```bash
go test -cover ./internal/swarm/server/... ./internal/swarm/schema/...
```

Expected: 所有测试通过

- [ ] **Step 4: 提交最终状态**

```bash
git add -A
git commit -m "chore: 10.3.19-20 全量编译验证通过"
```

---

## Task 15: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 10.3.19-20 状态**

将 `10.3.19-20` 的 `☐` 改为 `✅`

- [ ] **Step 2: 更新 10.3.2 相关 stub 标记**

将 10.3.2 中与 skill 相关的 `⤵️ 10.3.2` 标记更新为已完成（因为 6 处 stub 已回填）

- [ ] **Step 3: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新实现计划 10.3.19-20 状态为 ✅"
```
