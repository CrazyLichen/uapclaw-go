# CodeAgentRail (10.3.7) 实现设计

> Code 模式下自定义 Agent 调度枢纽，让主 Agent 通过 `Agent` 工具调用用户定义的自定义子 Agent。

## 1. 背景与动机

### 1.1 两套自定义 Agent 调度路径

Python 中自定义 Agent 有两种调度方式：

| 模式 | 调度方式 | 管理者 | Go 实现状态 |
|------|---------|--------|------------|
| agent.plan / agent.fast | SubagentRail（每个子 Agent 独立工具名） | `_load_custom_subagents()` | ✅ 已实现 |
| code | CodeAgentRail（统一 `Agent` 工具 + `subagent_type` 参数） | `CodeAgentRail` + `AgentTool` | ❌ 未实现 |

**Agent 模式**下自定义 Agent 已可工作（走 `DeepAdapter.loadCustomSubagents()` → SubagentRail），但 **Code 模式**下缺失 CodeAgentRail，用户通过 `/agents` 创建的自定义 Agent 无法被主 Agent 调用。

### 1.2 为什么 Code 模式不用 SubagentRail

- Code 模式下主 Agent 自身是编码 Agent，子 Agent（explore/plan）是辅助的
- 自定义 Agent 通过统一的 `Agent` 工具调度，避免工具列表爆炸
- 自定义 Agent 的工具集需要按定义动态过滤（`tools`/`disallowed_tools`），SubagentRail 的 task_tool 不支持这种细粒度控制
- 自定义 Agent 支持热重载（用户修改 Agent 定义后无需重启），SubagentRail 不支持

### 1.3 依赖就绪情况

| 依赖 | Go 实现 | 状态 |
|------|---------|------|
| `AgentConfigService.ListCustomAgents()` | `runtime.AgentConfigService` | ✅ |
| `agentDefToSubagentConfig()` | `adapter.agentDefToSubagentConfig()` | ✅ |
| `CreateDeepAgent()` | `harness.CreateDeepAgent()` | ✅ |
| `ResourceMgr` / `AbilityManager` | agentcore | ✅ |
| `DeepAgentRail` 基类 | `harness/rails.DeepAgentRail` | ✅ |
| `Workspace` | agentcore | ✅ |
| `Tool` 接口 / `ToolCard` | agentcore | ✅ |
| `GatewayPushTransport`（异步推送） | `server/gateway_push` | ✅ |

## 2. 架构设计

### 2.1 组件总览

```
swarm/server/adapter/
├── code_agent_rail.go    # CodeAgentRail + 常量 + 辅助函数
├── agent_tool.go         # AgentTool（实现 tool.Tool 接口）
└── code_adapter.go       # 步骤 16 回填：构建 CodeAgentRail
```

### 2.2 调用链

```
LLM 选择调用 Agent 工具
  → AgentTool.Invoke(ctx, inputs)
    ├─ 解析 inputs: subagent_type, prompt, description, model, background
    ├─ 从 customAgents 查找 AgentDefinition
    ├─ agentDefToSubagentConfig() 转换
    ├─ filterToolCards() 过滤工具集
    ├─ CreateDeepAgent() 创建子 Agent
    ├─ [同步] subAgent.Invoke(ctx, inputs, WithToolSession(parentSession)) → 等结果 → 返回
    └─ [异步] go subAgent.Invoke(...) → 立即返回 {"status": "async_launched"}
              后台 goroutine 中子 Agent 的流式输出通过 parentSession delivery 推送
```

### 2.3 生命周期

```
CodeAdapter.CreateInstance()
  └─ 步骤 16: buildCodeAgentRail()
       └─ NewCodeAgentRail(workspaceDir, configLister)
            → [暂不注册，等 Init 钩子]

DeepAgent 实例化后
  └─ RegisterRail(codeAgentRail)
       └─ CodeAgentRail.Init(agent)
            ├─ loadCustomAgents() 从 AgentConfigService 加载
            ├─ buildAgentToolCard() 动态构建 ToolCard
            ├─ NewAgentTool(card, parentAgent, customAgents)
            ├─ ResourceMgr.AddTool(agentTool)
            └─ AbilityManager.Add(agentTool.Card())

配置热重载时
  └─ CodeAgentRail.Reload()
       ├─ Uninit(agent) — 注销旧 AgentTool
       └─ Init(agent)   — 重新加载并注册新 AgentTool

Agent 销毁时
  └─ UnregisterRail(codeAgentRail)
       └─ CodeAgentRail.Uninit(agent)
            ├─ AbilityManager.Remove("Agent")
            └─ ResourceMgr.RemoveTool(toolID)
```

## 3. 详细设计

### 3.1 常量与映射

```go
// code_agent_rail.go

// ──────────────────────────── 常量 ────────────────────────────

// disallowedForSubagents 禁止传递给子 Agent 的工具名集合。
// 对齐 Python: DISALLOWED_FOR_SUBAGENTS
var disallowedForSubagents = map[string]bool{
    "Agent": true, "task": true, "enter_plan_mode": true,
    "exit_plan_mode": true, "ask_user_question": true,
    "task_stop": true, "switch_mode": true,
}

// ──────────────────────────── 全局变量 ────────────────────────────

// displayToInternal 显示名→内部名映射。
// 对齐 Python: _DISPLAY_TO_INTERNAL
var displayToInternal = map[string]string{
    "Read": "read_file", "Write": "write_file", "Edit": "edit_file",
    "Bash": "bash", "Grep": "grep", "Glob": "glob",
    "LS": "ls", "ListDir": "ls",
    "TodoWrite": "todo_create", "TodoList": "todo_list",
    "WebSearch": "web_search", "WebFetch": "web_fetch",
    "ImageOCR": "image_ocr", "VisionQA": "visual_question_answering",
    "AudioTranscribe": "audio_transcription",
    "AudioQA": "audio_question_answering",
    "AudioMetadata": "audio_metadata",
}

// toolGroups 工具分组（用于 Agent 定义 UI）。
// 对齐 Python: TOOL_GROUPS
var toolGroups = map[string][]string{
    "核心":   {"Read", "Write", "Edit", "Bash", "LS"},
    "搜索":   {"Grep", "Glob", "WebSearch", "WebFetch"},
    "代码智能": {"LSP", "TodoWrite", "TodoList"},
    "高级":   {"MemorySearch", "MemoryGet", "WriteMemory", "EditMemory", "CronCreate", "CronList", "CronDelete", "SkillTool"},
    "可视化":  {"VisionQA", "ImageOCR", "AudioTranscribe"},
}
```

### 3.2 辅助函数

```go
// filterToolCards 按允许/禁止列表过滤 ToolCard。
// 对齐 Python: _filter_tool_cards(all_tool_cards, allowed_tools, disallowed_tools)
func filterToolCards(
    allToolCards []*tool.ToolCard,
    allowedTools []string,
    disallowedTools []string,
) []*tool.ToolCard

// buildAgentToolCard 动态构建 Agent 工具的 ToolCard。
// 对齐 Python: _build_agent_tool_card(custom_agents, agent_id)
func buildAgentToolCard(customAgents []*types.AgentDefinition, agentID string) *tool.ToolCard
```

**`filterToolCards` 逻辑**：
- `allowedTools == ["*"]` → 返回全部
- 否则按显示名和内部名双匹配过滤
- `disallowedTools` 再从结果中移除

**`buildAgentToolCard` 逻辑**：
- 遍历 customAgents，生成描述行（`- name: when_to_use (Tools: ...)`)
- 构建 ToolCard，name="Agent"，含 5 个输入参数（description, prompt, subagent_type, model, background）
- required: ["description", "prompt", "subagent_type"]

### 3.3 AgentTool

```go
// agent_tool.go

// AgentTool 自定义 Agent 调度工具。
// 对齐 Python: AgentTool(Tool)
// 实现 tool.Tool 接口，invoke 时创建子 DeepAgent 执行任务。
type AgentTool struct {
    card         *tool.ToolCard
    parentAgent  hinterfaces.DeepAgentInterface
    customAgents map[string]*types.AgentDefinition  // name → AgentDefinition
    modelCache   map[string]*llm.Model
}
```

**`Invoke` 方法**：

```go
func (t *AgentTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
    // 1. 从 opts 提取 SessionFacade（对齐 Python kwargs["session"]）
    // 2. 解析 inputs: subagent_type, prompt, description, model, background
    // 3. 校验 subagent_type + prompt 必填
    // 4. 从 customAgents 查找 AgentDefinition
    // 5. 构建 subSessionID: "{parentSessionID}_custom_{subagentType}_{randomHex8}"
    // 6. 调用 createSubAgent() 创建子 DeepAgent
    // 7. 根据 background 标志：
    //    - false（同步）：subAgent.Invoke → 返回 {"output": output, "agent_id": subagentType}
    //    - true（异步）：go subAgent.Invoke → 立即返回 {"status": "async_launched", "agent_id": ..., "prompt": ...}
}
```

**`createSubAgent` 方法**（对齐 Python `_create_sub_agent`）：

```go
func (t *AgentTool) createSubAgent(agentDef *types.AgentDefinition, subSessionID string) (*harness.DeepAgent, error) {
    // 1. agentDefToSubagentConfig() 转换（已有函数）
    // 2. 从 parentAgent.ability_manager 获取 ToolCard 列表，过滤 disallowedForSubagents
    // 3. filterToolCards() 按定义的 tools/disallowed_tools 再过滤
    // 4. 构建 Workspace（复用父 workspace root_path）
    // 5. 构建 CreateDeepAgentParams：
    //    - model, card, system_prompt, tools, workspace, max_iterations
    //    - enable_task_loop=true, subagents=nil, add_general_purpose_agent=false
    //    - restrict_to_work_dir=true, auto_create_workspace=false
    //    - sys_operation=nil（子 Agent 不继承 sys_operation）
    // 6. CreateDeepAgent(ctx, params)
}
```

**`Stream` 方法**：返回 `ErrStreamNotSupported`（对齐 Python `stream(self, inputs, **kwargs): pass`）

### 3.4 CodeAgentRail

```go
// code_agent_rail.go

// CodeAgentRail Code 模式下的自定义 Agent 护栏。
// 对齐 Python: CodeAgentRail(DeepAgentRail) priority=90
//
// 管理 /agents 创建的自定义 Agent，通过 AgentTool 注册为统一 "Agent" 工具。
// 与 SubagentRail 共存，只管理自定义 Agent，不触碰内置 Agent。
type CodeAgentRail struct {
    rails.DeepAgentRail
    workspaceDir  string
    configLister  AgentConfigLister
    agentTool     *AgentTool
}
```

**`Init` 方法**（对齐 Python `CodeAgentRail.init`）：

```go
func (r *CodeAgentRail) Init(agent sainterfaces.BaseAgent) error {
    // 1. loadCustomAgents() — 从 AgentConfigService 加载 enabled 的非 builtin Agent
    // 2. 无自定义 Agent → 跳过注册，日志记录
    // 3. buildAgentToolCard(customAgents, agent.Card().ID)
    // 4. NewAgentTool(card, parentAgent, customAgents, modelCache)
    // 5. ResourceMgr.AddTool(agentTool)
    // 6. AbilityManager.Add(agentTool.Card())
}
```

**`Uninit` 方法**（对齐 Python `CodeAgentRail.uninit`）：

```go
func (r *CodeAgentRail) Uninit(agent sainterfaces.BaseAgent) error {
    // 1. agentTool == nil → 直接返回
    // 2. AbilityManager.Remove("Agent")
    // 3. ResourceMgr.RemoveTool(toolID)
    // 4. agentTool = nil
}
```

**`Reload` 方法**（热重载，对齐 Python `_get_current_agent_rails` 覆写）：

```go
func (r *CodeAgentRail) Reload(agent sainterfaces.BaseAgent) error {
    // 1. Uninit(agent) — 注销旧 AgentTool
    // 2. Init(agent)   — 重新加载并注册新 AgentTool
}
```

**`loadCustomAgents` 方法**：

```go
func (r *CodeAgentRail) loadCustomAgents() []*types.AgentDefinition {
    // 对齐 Python: _load_custom_agents()
    // 从 r.configLister.ListCustomAgents() 获取
    // 过滤条件：source != "builtin" && enabled == true
    // 异常时 warn + 返回空列表
}
```

### 3.5 CodeAdapter 集成

在 `CodeAdapter.CreateInstance()` 步骤 16 中添加：

```go
// 步骤 16: _build_agent_rails(config, configBase, mode="code")
// ... 其他 rails ...
// CodeAgentRail:
if c.deep.configLister != nil {
    rail := NewCodeAgentRail(c.deep.workspaceDir, c.deep.configLister)
    c.codeAgentRail = rail
    railsList = append(railsList, rail)
}
```

**注意**：Code 模式的 `_buildConfiguredSubagents` 中**不加入自定义 Agent**（对齐 Python interface_code.py:761-764 注释），自定义 Agent 完全由 CodeAgentRail 的 AgentTool 管理。

### 3.6 热重载触发

对齐 Python `interface_code.py:839-848` `_get_current_agent_rails()` 覆写：

当 `ReloadAgentConfig` 被调用时（配置热重载），如果 `codeAgentRail` 不在 DeepAdapter 的默认热重载 rail 列表中，需要显式调用 `codeAgentRail.Reload(agent)` 更新自定义 Agent 定义。

具体实现：在 `DeepAdapter.ReloadAgentConfig` 或 `CodeAdapter.ReloadAgentConfig` 中，遍历需要重载的 rail 列表，检测 `codeAgentRail` 是否需要重载。

## 4. AgentConfigService.ListAvailableTools 回填

当前 `AgentConfigService.ListAvailableTools()` 中 `disallowedForSubagents` 和工具分组是硬编码的（标注了 `⤵️ 10.3.7-11`）。CodeAgentRail 实现后，这些常量提取为共享变量，`ListAvailableTools` 改为引用：

```go
// 之前（硬编码）：
disallowedForSubagents := []string{"Agent", "task", ...}

// 之后（引用共享变量）：
disallowedForSubagents := disallowedForSubagentsSlice()
```

## 5. 测试策略

### 5.1 单元测试

| 测试文件 | 测试函数 |
|---------|---------|
| `code_agent_rail_test.go` | `TestFilterToolCards_通配符` / `TestFilterToolCards_指定名` / `TestFilterToolCards_含disallowed` |
| `code_agent_rail_test.go` | `TestBuildAgentToolCard_基本` / `TestBuildAgentToolCard_空列表` |
| `code_agent_rail_test.go` | `TestCodeAgentRail_Init_无自定义Agent` / `TestCodeAgentRail_Init_有自定义Agent` |
| `code_agent_rail_test.go` | `TestCodeAgentRail_Uninit_无工具` / `TestCodeAgentRail_Uninit_有工具` |
| `code_agent_rail_test.go` | `TestCodeAgentRail_Reload` |
| `agent_tool_test.go` | `TestAgentTool_Invoke_同步` / `TestAgentTool_Invoke_异步` |
| `agent_tool_test.go` | `TestAgentTool_Invoke_缺少subagentType` / `TestAgentTool_Invoke_未找到Agent` |
| `agent_tool_test.go` | `TestAgentTool_Invoke_创建子Agent失败` |
| `agent_tool_test.go` | `TestAgentTool_Stream_不支持` |
| `agent_tool_test.go` | `TestAgentTool_BuildSubSessionID` |

### 5.2 Mock 策略

- `AgentConfigLister` 接口：已有，直接 mock
- `BaseAgent` 接口：使用 fakeAgent（已有模式）
- `DeepAgentInterface`：使用 fakeDeepAgent
- `AbilityManagerInterface`：使用 fakeAbilityManager
- 子 Agent 创建：通过 `CreateDeepAgentParams` 验证参数正确性，不真正创建 DeepAgent

## 6. 实现步骤

| 步骤 | 内容 | 依赖 |
|------|------|------|
| 1 | 创建 `code_agent_rail.go`：常量 + 映射 + `filterToolCards` + `buildAgentToolCard` + `CodeAgentRail` 结构体 | 无 |
| 2 | 创建 `agent_tool.go`：`AgentTool` 结构体 + `Invoke`/`Stream` + `createSubAgent` | 步骤 1 |
| 3 | 创建 `code_agent_rail_test.go` + `agent_tool_test.go` | 步骤 1, 2 |
| 4 | 修改 `code_adapter.go`：步骤 16 回填 `buildCodeAgentRail` | 步骤 1 |
| 5 | 修改 `agent_config.go`：`ListAvailableTools` 引用共享常量 | 步骤 1 |
| 6 | 修改 `IMPLEMENTATION_PLAN.md`：10.3.7 标记 ✅，10.3.13 标记 ✅ | 步骤 4 |
| 7 | 编译验证 + 测试通过 | 步骤 1-6 |

## 7. 对应 Python 文件

| Go 文件 | Python 文件 | 行数 |
|---------|------------|------|
| `adapter/code_agent_rail.go` | `jiuwenswarm/server/runtime/agent_adapter/code_agent_rail.py` | 428 行 |
| `adapter/agent_tool.go` | 同上（`AgentTool` 类 170-348 行） | — |
| `adapter/code_adapter.go`（修改） | `jiuwenswarm/server/runtime/agent_adapter/interface_code.py`（826-848 行） | — |
| `runtime/agent_config.go`（修改） | `jiuwenswarm/server/runtime/agent_config_service.py` | — |
