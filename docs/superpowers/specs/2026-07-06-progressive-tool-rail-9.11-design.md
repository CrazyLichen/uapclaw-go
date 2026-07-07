# 9.11 ProgressiveToolRail 渐进式工具权限 — 实现设计

## 1. 概述

ProgressiveToolRail 解决**工具数量过多**时 LLM 上下文爆炸问题。LLM 初始只看到少量"导航工具"（`search_tools` / `load_tools`）和默认可见工具，通过 search 发现、load 按需加载，避免一次性把数百个工具的 schema 塞入 prompt。

### 流程位置

ProgressiveToolRail 处于 DeepAgent 的 **before_model_call** 阶段（每次 LLM 调用前），优先级为 90：

```
DeepAgent.invoke()
  ├── before_invoke          ← 缓存全量工具清单 + 初始化 session 可见工具
  │
  └── TaskLoop (每轮迭代)
        ├── before_task_iteration
        │
        └── ReActLoop (每轮迭代)
              ├── before_model_call    ← ★ ProgressiveToolRail 在此注入
              │   ├── 注入导航节 + 规则节到 SystemPromptBuilder
              │   └── 过滤 callable tools（只保留 visible + meta + baseline）
              │
              ├── LLM 调用（模型只能看到过滤后的工具）
              │
              └── 工具执行
                    ├── search_tools → 发现工具（返回不可调用，仅展示）
                    └── load_tools  → 加载工具（标记为可调用）
```

### 对应 Python 代码

| Python 文件 | 职责 | 行数 |
|---|---|---|
| `harness/rails/base.py` (DeepAgentRail) | 扩展 AgentRail，加 workspace + sys_operation + task-loop hooks | 108 |
| `harness/rails/progressive_tool_rail.py` | 渐进式工具权限核心逻辑 | 635 |
| `harness/tools/tool_discovery/search_tools.py` | search_tools 元工具 | 107 |
| `harness/tools/tool_discovery/load_tools.py` | load_tools 元工具 | 77 |

## 2. 设计决策

### 2.1 BaseRail.GetCallbacks 分层

**决策**：AgentRail 接口保留 10 个方法声明（含 BeforeTaskIteration/AfterTaskIteration），但 BaseRail.GetCallbacks() 只提取 8 个基础事件映射。DeepAgentRail.GetCallbacks() 合并 8 + 2（task-iteration）。

**原因**：与 Python 完全对齐。Python 中 AgentRail 接口有 10 个方法，但 `get_callbacks()` 只通过 `EVENT_METHOD_MAP`（8个）提取基础 hooks；DeepAgentRail 通过 `DEEP_EVENT_METHOD_MAP`（2个）额外合并 task-iteration hooks。

### 2.2 DeepAgentRail 放 harness/rails/ 包

**决策**：新建 `internal/agentcore/harness/rails/` 包，`base.go` 放 DeepAgentRail。

**原因**：DeepAgentRail 引用 harness 层的 Workspace 和 SysOperation，不能放 core 层（single_agent/rail）。与 Python 的 `openjiuwen/harness/rails/` 对齐。

### 2.3 一次性完整实现

**决策**：一次性实现全部链路：DeepAgentRail 基类 → BaseRail.GetCallbacks 拆分 → ProgressiveToolRail 核心 → SearchToolsTool + LoadToolsTool → deep_agent.go 回填 → 测试。

### 2.4 元工具双重注册

**决策**：SearchToolsTool / LoadToolsTool 同时注册到 ResourceMgr 和 AbilityManager，与 Python 完全对齐。

### 2.5 RailAgent 接口扩充

**决策**：RailAgent 接口新增 `AbilityManager()` 和 `SystemPromptBuilder()` 两个方法。

**原因**：
- ProgressiveToolRail.Init() 需要通过 agent 获取 AbilityManager 注册元工具
- ProgressiveToolRail.BeforeModelCall() 需要获取 SystemPromptBuilder 注入提示词节
- Python 中 Rail 直接通过 `agent.ability_manager` / `ctx.agent.system_prompt_builder` 访问，Go 版应在接口层对齐
- DeepAgent、ReActAgent 等主要 Agent 都已有这两个方法的实现，零适配成本

### 2.6 SystemPromptBuilder 统一接口

**决策**：在 `single_agent/prompts` 包定义 `SystemPromptBuilderInterface` 最小接口，RailAgent 接口返回此接口类型。

**最小接口方法**（Rail 实际用到的）：
```go
type SystemPromptBuilderInterface interface {
    AddSection(section PromptSection) SystemPromptBuilderInterface
    RemoveSection(name string) SystemPromptBuilderInterface
    Language() string  // 注意：当前是公开字段，需改为方法
    GetSection(name string) *PromptSection
    HasSection(name string) bool
}
```

**适配**：`saprompt.SystemPromptBuilder` 和 `hprompts.SystemPromptBuilder`（嵌入 base）都隐式满足此接口。

**注意**：当前 `Language` 是 `SystemPromptBuilder` 的公开字段，需改为 getter 方法 `Language() string` 以满足接口。所有直接访问 `.Language` 的代码需改为 `.Language()` 调用。

### 2.7 删除 syncBuilderToActiveRails 类型断言注入

**决策**：删除 `syncBuilderToActiveRails` 中 `SetSystemPromptBuilder` 类型断言注入逻辑。

**原因**：
- Python 的 ProgressiveToolRail **不在自己身上存 builder 引用**，每次通过 `ctx.agent.system_prompt_builder` 实时获取
- Go 版应采用同样模式：通过 `cbc.Agent().SystemPromptBuilder()` 实时获取，不存在引用失效问题
- 当前**无任何 Rail 消费** `SetSystemPromptBuilder`（搜索确认），删除零影响
- DeepAgent 暴露 `SystemPromptBuilder()` 导出方法后，Rail 通过 RailAgent 接口直接访问

**保留 `_sync_builder_to_active_rails` 的 ReActAgent 同步逻辑**（将 builder 同步给内层 ReActAgent），只删除 Rail 类型断言注入部分。

## 3. 实现层次

### 3.1 层次1：core 层 GetCallbacks 分层

**改动文件**：

- `single_agent/rail/event.go`：
  - 新增 `AllBaseCallbackEvents()` 返回 8 个基础事件
  - 新增 `AllDeepCallbackEvents()` 返回 2 个 task-iteration 事件
  - 原 `AllCallbackEvents()` 保留，返回全部 10 个（向后兼容）
  - 新增 `DeepEventMethodMap`：`map[AgentCallbackEvent]string{CallbackBeforeTaskIteration: "BeforeTaskIteration", CallbackAfterTaskIteration: "AfterTaskIteration"}`

- `single_agent/rail/rail.go`：
  - `BaseRail.GetCallbacks()` 只提取 8 个基础事件映射（通过 `BaseEventMethodMap`）
  - 新增 `BaseEventMethodMap` 常量（8 个基础事件→方法名映射）

- `single_agent/rail/event_test.go`、`single_agent/rail/rail_test.go`：适配

### 3.2 层次2：DeepAgentRail 基类

**新文件**：`internal/agentcore/harness/rails/base.go`

```go
type DeepAgentRail struct {
    rail.BaseRail
    workspace    *workspace.Workspace
    sysOperation sysop.SysOperation
}

// 方法：
// - SetWorkspace(w *workspace.Workspace)
// - SetSysOperation(op sysop.SysOperation)
// - Workspace() *workspace.Workspace
// - SysOperation() sysop.SysOperation
// - BeforeTaskIteration() no-op（覆盖 BaseRail 的 no-op）
// - AfterTaskIteration() no-op（覆盖 BaseRail 的 no-op）
// - GetCallbacks() → 合并 BaseRail.GetCallbacks() + DeepEventMethodMap 中被覆盖的 task-iteration hooks
// - isDeepBase(methodName) → 检测子类是否覆盖了 DeepAgentRail 的 no-op
```

**新文件**：`internal/agentcore/harness/rails/doc.go`

### 3.3 层次3：元工具 SearchToolsTool / LoadToolsTool

**新目录**：`internal/agentcore/harness/tools/tool_discovery/`

**search_tools.go**：
```go
type SearchToolsInput struct {
    Query       string
    Limit       int    // 默认 10，上限 20
    DetailLevel int    // 1=name+desc, 2=+param_summary, 3=+full_params
}

type SearchToolsTool struct {
    // 嵌入 Tool 基类
    // 持有 searchToolsFn / appendTraceFn 回调
}

// Invoke() → 调用 searchToolsFn → 返回 matches + callability_note + next_step_hint
// Stream() → 不支持，返回空
```

**load_tools.go**：
```go
type LoadToolsInput struct {
    ToolNames []string
    Replace   bool
}

type LoadToolsTool struct {
    // 嵌入 Tool 基类
    // 持有 loadToolsFn 回调
}

// Invoke() → 调用 loadToolsFn → 返回 loaded_tools + visible_tools + skipped_tools
// Stream() → 不支持，返回空
```

**doc.go**：包文档

### 3.4 层次4：ProgressiveToolRail

**新文件**：`internal/agentcore/harness/rails/progressive.go`

核心结构：

```go
type ProgressiveToolRail struct {
    DeepAgentRail
    config          *DeepAgentConfig
    defaultVisible  map[string]struct{}  // 配置的默认可见工具
    alwaysVisible   map[string]struct{}  // 始终可见的工具
    maxLoadedTools  int                  // 最大加载数
    metaToolNames   map[string]struct{}  // search_tools / load_tools
    ownedToolNames  map[string]struct{}  // 已注册到 ability_manager 的工具名
    ownedToolIDs    map[string]struct{}  // 已注册到 resource_mgr 的工具 ID
    cachedAllTools  []ToolInfoInterface  // 全量工具缓存
}
```

生命周期方法：

| 方法 | 对应 Python | 职责 |
|------|------------|------|
| `Init(agent)` | `init()` | 注册 SearchToolsTool + LoadToolsTool 到 resource_mgr + ability_manager |
| `Uninit(agent)` | `uninit()` | 从 ability_manager 移除元工具 |
| `BeforeInvoke(ctx)` | `before_invoke()` | 缓存全量工具清单 + 初始化 session 可见工具 |
| `BeforeModelCall(ctx)` | `before_model_call()` | 注入导航节+规则节 + 过滤 callable tools |

session state 存储（通过 SessionFacade）：

| key | 类型 | 用途 |
|-----|------|------|
| `__progressive_visible_tool_names__` | `[]string` | 当前 session 可调用的工具名列表 |
| `__progressive_tool_discovery_trace__` | `[]map[string]any` | 工具发现轨迹记录 |

搜索评分算法（与 Python 完全一致）：

| 条件 | 加分 |
|------|------|
| 完全匹配 name | +100 |
| name 包含 query | +40 |
| description 包含 query | +25 |
| haystack 包含 query | +10 |
| 分词匹配（每个 token） | +3 |

工具分组与排序：

| 分组 | 排序值 | 中文 |
|------|--------|------|
| skill | 0 | 技能 |
| runtime | 1 | 运行时 |
| document | 2 | 文档 |
| spreadsheet | 3 | 表格 |
| general | 9 | 通用 |

辅助方法：

| 方法 | 职责 |
|------|------|
| `searchTools()` | 搜索工具（模糊匹配+评分排序） |
| `loadTools()` | 加载工具到 session 可见列表 |
| `buildNavigationSection()` | 构建导航节 |
| `buildProgressiveToolRulesSection()` | 构建规则节 |
| `buildNavigationEntries()` | 构建导航条目列表 |
| `getVisibleTools()` / `setVisibleTools()` / `initVisibleTools()` | session 可见工具读写 |
| `appendTrace()` | 追加发现轨迹 |
| `listToolInfos()` | 从 ability_manager 获取工具清单 |
| `buildToolSummary()` | 构建工具摘要（按 detail_level） |
| `toolGroupForNavigation()` | 推断工具分组 |
| `toolGroupRank()` | 分组排序值 |
| `toolGroupToCN()` | 分组中文翻译 |

### 3.5 层次5：回填点

`deep_agent.go` 中 6 处 ⤵️ 回填处理：

| 位置 | 当前内容 | 回填为 |
|------|---------|--------|
| L1251-1252 | `⤵️ 9.11 回填：ProgressiveToolRail 创建` | `rail := NewProgressiveToolRail(d.deepConfig); d.pendingRails = append(d.pendingRails, rail)` |
| L1256-1258 | `⤵️ 9.11 回填：TaskCompletionRail 创建` | 保留 ⤵️（9.12 范畴） |
| L1262-1263 | `⤵️ 9.11 回填：build_permission_interrupt_rail` | 保留 ⤵️ |
| L77 | `⤵️ 9.11 回填：TaskCompletionRail 具体类型` | 保留 ⤵️（9.12 范畴） |
| L2290 | `⤵️ 9.11 回填：TaskCompletionRail 类型检查` | 保留 ⤵️（9.12 范畴） |
| L1925/1945 | `⤵️ 9.11 回填：taskCompletionRail.buildEvaluators()` | 保留 ⤵️（9.12 范畴） |

## 4. 新增文件清单

```
internal/agentcore/harness/rails/
├── doc.go                    # 包文档
├── base.go                   # DeepAgentRail 基类
├── base_test.go              # DeepAgentRail 测试
├── progressive.go            # ProgressiveToolRail
└── progressive_test.go       # ProgressiveToolRail 测试

internal/agentcore/harness/tools/tool_discovery/
├── doc.go                    # 包文档
├── search_tools.go           # SearchToolsTool
├── search_tools_test.go      # SearchToolsTool 测试
├── load_tools.go             # LoadToolsTool
└── load_tools_test.go        # LoadToolsTool 测试
```

## 5. 修改文件清单

```
internal/agentcore/single_agent/rail/event.go            # 新增 AllBaseCallbackEvents/AllDeepCallbackEvents/DeepEventMethodMap
internal/agentcore/single_agent/rail/event_test.go       # 适配
internal/agentcore/single_agent/rail/rail.go             # BaseRail.GetCallbacks 分层 + BaseEventMethodMap
internal/agentcore/single_agent/rail/rail_test.go        # 适配
internal/agentcore/single_agent/rail/context.go          # RailAgent 接口加 AbilityManager() + SystemPromptBuilder()
internal/agentcore/single_agent/rail/context_test.go     # fakeRailAgent 适配
internal/agentcore/single_agent/prompts/builder.go       # Language 字段→方法 + 定义 SystemPromptBuilderInterface
internal/agentcore/single_agent/prompts/builder_test.go  # Language 改造适配
internal/agentcore/harness/prompts/builder.go            # 适配 Language 改造
internal/agentcore/harness/deep_agent.go                 # 回填 + 暴露 SystemPromptBuilder() + 删除 syncBuilderToActiveRails 类型断言注入
internal/agentcore/harness/deep_agent_test.go            # 适配测试
```

## 6. 已有可复用组件

| 组件 | 文件 | 状态 |
|------|------|------|
| 提示词节 | `harness/prompts/sections/progressive_tool_rail.go` | ✅ 已实现 |
| SectionName 常量 | `harness/prompts/sections/section_name.go` | ✅ 已实现 |
| 配置字段 | `harness/schema/config.go` (ProgressiveTool*) | ✅ 已实现 |
| BaseRail + AgentRail 接口 | `single_agent/rail/rail.go` | ✅ 需拆分 GetCallbacks |
| AbilityManager | `single_agent/ability/ability_manager.go` | ✅ 已实现 |
| ResourceMgr | `runner/resources_manager/resource_manager.go` | ✅ 已实现 |
| SessionFacade state | `session/interfaces/` | ✅ 已实现 |

## 7. 关键依赖确认

### 7.1 SystemPromptBuilder

- ✅ 已实现，`AddSection()` / `RemoveSection()` 可用
- **统一接口**：定义 `SystemPromptBuilderInterface` 最小接口，RailAgent 返回此接口类型
- **Language 字段改造**：当前 `Language` 是公开字段，需改为 `Language() string` getter 方法以满足接口
- **DeepAgent 暴露**：新增 `SystemPromptBuilder()` 导出方法（当前是私有字段）
- **Rail 不存引用**：ProgressiveToolRail 不在自己身上存 builder 字段，每次通过 `cbc.Agent().SystemPromptBuilder()` 实时获取（与 Python ProgressiveToolRail 一致）
- **删除 `syncBuilderToActiveRails` 中 `SetSystemPromptBuilder` 类型断言注入**（当前无消费者）

### 7.2 Tool 基类

- ✅ `tool.Tool` 接口：`Card()` / `Invoke()` / `Stream()`
- `tool.NewToolCard()` 构造 ToolCard
- `tool.ToolCard` 嵌入 `schema.BaseCard`，有 `InputParams`、`Properties`
- SearchToolsTool / LoadToolsTool 需实现 `tool.Tool` 接口
- Python 中 Tool 构造时传入 `build_tool_card()` 生成的 card，Go 版用 `tool.NewToolCard()` + `GetSearchToolsMetadataProviderInputParams()` 构建

### 7.3 AbilityManager

- ✅ 已实现，`AbilityManagerInterface` 有 `Add` / `Remove` / `ListToolInfo`
- DeepAgent 暴露 `AbilityManager()` 方法
- ProgressiveToolRail.Init() 需要通过 agent 获取 AbilityManager 来注册元工具

### 7.4 ResourceMgr

- ✅ `AddTool()` / `GetTool()` / `RemoveTool()` 可用
- 通过 `runner.GetResourceMgr()` 全局访问

### 7.5 SessionFacade state

- ✅ `GetState(key StateKey)` / `UpdateState(data map[string]any)` 已实现
- Python 的 `session.get_state(key)` → Go 的 `sess.GetState(state.StateKey(key))`
- Python 的 `session.update_state({key: value})` → Go 的 `sess.UpdateState(map[string]any{key: value})`

### 7.6 RailAgent 接口与 Agent 访问

- **扩充 RailAgent 接口**：新增 `AbilityManager()` + `SystemPromptBuilder()` 两个方法
- DeepAgent、ReActAgent 等主要 Agent 已有实现，零适配成本
- ProgressiveToolRail.Init() 通过 `agent.AbilityManager()` 注册元工具
- ProgressiveToolRail.BeforeModelCall() 通过 `cbc.Agent().SystemPromptBuilder()` 实时获取 builder
- 测试中的 fakeRailAgent 需适配新增的两个方法

## 8. 测试策略

- **DeepAgentRail**：测试 GetCallbacks 合并（8+2）、SetWorkspace/SetSysOperation
- **ProgressiveToolRail**：测试搜索评分、工具过滤、session 可见工具管理、导航节构建、max_loaded_tools 溢出
- **SearchToolsTool**：测试 Invoke 返回格式、limit 上限、detail_level
- **LoadToolsTool**：测试 Invoke 返回格式、replace 模式、overflow 处理
- 所有测试通过 mock 接口，不依赖真实 LLM/外部服务
