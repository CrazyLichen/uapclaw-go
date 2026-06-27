# 6.23 ResourceMgr 设计文档

## 1. 概述

### 1.1 流程位置

6.23 ResourceMgr 位于 **领域六 Runner 编排**（6.23-6.29）的第一步，是 Runner 单例（6.25）的核心依赖。Runner 在 `run_agent`/`run_workflow` 时通过 `resource_mgr.get_agent()`/`resource_mgr.get_workflow()` 按名称查找已注册的资源。

### 1.2 作用

ResourceMgr 是 **Agent/Tool/Workflow/Model/Prompt/SysOperation 的全局注册表**，提供：
- 按 ID 的 CRUD 操作（add/get/remove）
- 按 Tag 的分类查询和过滤
- Provider 延迟加载模式（注册工厂函数而非实例）
- MCP Server 管理（添加/移除/刷新/工具获取）
- 标签双向索引（resource↔tag）
- 系统操作（SysOperation）管理

### 1.3 Python 参考

`openjiuwen/core/runner/resources_manager/` 目录下 13 个 Python 文件。

## 2. 设计决策汇总

| # | 决策项 | 选择 |
|---|--------|------|
| 1 | 文件位置 | `runner/resources_manager/`（与Python一致） |
| 2 | 现有 single_agent/resource/ | 删除，AbilityManager 改依赖新包 |
| 3 | Result 类型 | Go 惯用 `(T, error)`，不引入 Ok/Error |
| 4 | Provider 模式 | 仅 Provider 模式，每种类型提供 ctx context.Context 参数 |
| 5 | PromptMgr 例外 | 直接存储实例，不用 Provider（与Python一致） |
| 6 | 并发安全 | `sync.RWMutex + map[K]V` 封装 ThreadSafeDict |
| 7 | Tag 系统 | 全部实现，与 Python 一一对照 |
| 8 | MCP Server | 完整实现 |
| 9 | AgentMgr 分布式 | 仅本地模式，标记 ⤵️ 预留 |
| 10 | trace 装饰 | 直接使用已有 decorator 包 |
| 11 | SysOperationMgr | 数据结构预留，核心逻辑标记 ⤵️ |
| 12 | AgentTeamMgr | 标记 ⤵️ 预留（TeamCard/BaseTeam 不存在） |
| 13 | 内部流转 | 完整复刻 4 个内部方法 + 4 组分派常量 |
| 14 | add 方法签名 | 保持与 Python 完全一致的异构签名 |
| 15 | remove 方法返回值 | 保持与 Python 完全一致的异构返回值 |
| 16 | get 方法返回 | 始终返回列表，按 ID 查找时返回 len≤1 切片 |
| 17 | 验证方法 | 完整实现所有验证方法 |
| 18 | add_sys_operation | 基础部分（card缓存+tag），工具注册标记 ⤵️ |
| 19 | MCP 方法参数 | Functional Options 模式 |
| 20 | release() | Release(ctx) error，忽略单个错误 |
| 21 | ToolMgr MCP 锁 | sync.Mutex 每 server_id 一把 |

## 3. 文件组织

```
runner/resources_manager/
├── doc.go                      # 包文档（对应 __init__.py）
├── base.go                     # 类型别名、Tag常量、枚举（对应 base.py）
├── base_test.go
├── thread_safe_dict.go         # 线程安全字典（对应 thread_safe_dict.py）
├── thread_safe_dict_test.go
├── abstract_manager.go         # 泛型抽象管理器（对应 abstract_manager.py）
├── abstract_manager_test.go
├── tag_manager.go              # 标签管理器，双向索引（对应 tag_manager.py）
├── tag_manager_test.go
├── resource_registry.go        # 聚合7个子管理器（对应 resource_registry.py）
├── resource_registry_test.go
├── agent_manager.go            # Agent管理器，本地模式+分布式预留（对应 agent_manager.py）
├── agent_manager_test.go
├── agent_team_manager.go       # AgentTeam管理器，预留⤵️（对应 agent_team_manager.py）
├── agent_team_manager_test.go
├── model_manager.go            # Model管理器+trace装饰（对应 model_manager.py）
├── model_manager_test.go
├── prompt_manager.go           # Prompt管理器，直接存储（对应 prompt_manager.py）
├── prompt_manager_test.go
├── tool_manager.go             # Tool管理器+MCP Server全套（对应 tool_manager.py）
├── tool_manager_test.go
├── workflow_manager.go         # Workflow管理器+trace装饰（对应 workflow_manager.py）
├── workflow_manager_test.go
├── sys_operation_manager.go    # SysOperation管理器，结构预留⤵️（对应 sys_operation_manager.py）
├── sys_operation_manager_test.go
├── resource_manager.go         # ResourceMgr门面类（对应 resource_manager.py）
└── resource_manager_test.go
```

## 4. 各文件详细设计

### 4.1 base.go — 类型定义

对应 Python `base.py`。

```go
// ──────────────────────────── 类型别名 ────────────────────────────

// AgentProvider Agent 资源提供者函数
type AgentProvider func(ctx context.Context, card *agentschema.AgentCard) (interfaces.BaseAgent, error)

// WorkflowProvider Workflow 资源提供者函数
type WorkflowProvider func(ctx context.Context, card *schema.WorkflowCard) (interfaces.Workflow, error)

// ModelProvider Model 资源提供者函数
type ModelProvider func(ctx context.Context, modelID string) (model_clients.BaseModelClient, error)

// Tag 标签类型
type Tag = string

// ──────────────────────────── 枚举 ────────────────────────────

// TagMatchStrategy 标签匹配策略
type TagMatchStrategy int
const (
    TagMatchAll TagMatchStrategy = iota  // 全匹配
    TagMatchAny                           // 任一匹配
)

// TagUpdateStrategy 标签更新策略
type TagUpdateStrategy int
const (
    TagUpdateMerge TagUpdateStrategy = iota  // 合并
    TagUpdateReplace                          // 替换
)

// ──────────────────────────── 常量 ────────────────────────────

const (
    TagAll    Tag = "*"            // 匹配所有资源
    TagGlobal Tag = "__global__"   // 全局标签
    TagActive Tag = "__active__"   // 活跃状态标签
    TagInactive Tag = "__inactive__" // 非活跃状态标签
)
```

**不实现**：`Ok[T]`/`Error[E]`/`Result` 类型，`AgentTeamProvider`（TeamCard/BaseTeam 不存在）。

### 4.2 thread_safe_dict.go — ThreadSafeDict[K, V]

对应 Python `thread_safe_dict.py`。

```go
type ThreadSafeDict[K comparable, V any] struct {
    mu   sync.RWMutex
    data map[K]V
}
```

方法：`NewThreadSafeDict`/`Get`/`Set`/`Delete`/`GetOrSet`/`GetOrCreate`/`Pop`/`SetDefault`/`Update`/`Clear`/`Keys`/`Values`/`Items`/`Len`/`Contains`

### 4.3 abstract_manager.go — AbstractManager[T]

对应 Python `abstract_manager.py`。

```go
type AbstractManager[T any] struct {
    providers ThreadSafeDict[string, func(context.Context) (T, error)]
}
```

方法：
- `registerProvider(resourceID string, provider func(context.Context) (T, error))` — 注册 provider，重复则返回 error
- `getResource(ctx context.Context, resourceID string) (T, error)` — 调用 provider 获取资源
- `unregisterProvider(resourceID string) (func(context.Context) (T, error), error)` — 注销 provider

Go 中不需要区分同步/异步 provider（无 `inspect.iscoroutinefunction`），统一为 `func(context.Context) (T, error)`。

### 4.4 tag_manager.go — TagMgr

对应 Python `tag_manager.py`（432行）。

```go
type TagMgr struct {
    resourceTags  map[string]map[Tag]struct{}  // 资源ID → 标签集合
    tagToResource map[Tag]map[string]struct{}  // 标签 → 资源ID集合
    mu            sync.RWMutex
}
```

完整方法列表（与 Python 一一对照）：

| Python 方法 | Go 方法 |
|------------|---------|
| `has_tag(tag)` | `HasTag(tag Tag) bool` |
| `list_tags()` | `ListTags() []Tag` |
| `has_resource(resource_id)` | `HasResource(resourceID string) bool` |
| `tag_resource(resource_id, tags)` | `TagResource(resourceID string, tags []Tag) []Tag` |
| `remove_resource(resource_id)` | `RemoveResource(resourceID string) []Tag` |
| `remove_resource_tags(resource_id, tags, skip_if_not_exists)` | `RemoveResourceTags(resourceID string, tags []Tag, skipIfNotExists bool) ([]Tag, error)` |
| `update_resource_tags(resource_id, tags, strategy)` | `UpdateResourceTags(resourceID string, tags []Tag, strategy TagUpdateStrategy) ([]Tag, error)` |
| `remove_tag(tag, skip_if_not_exists)` | `RemoveTag(tag Tag, skipIfNotExists bool) ([]string, error)` |
| `get_tag_resources(tag)` | `GetTagResources(tag Tag) []string` |
| `find_resources_by_tags(tags, strategy, skip_if_not_exists)` | `FindResourcesByTags(tags []Tag, strategy TagMatchStrategy, skipIfNotExists bool) ([]string, error)` |
| `has_resource_tag(resource_id, tag)` | `HasResourceTag(resourceID string, tag Tag) bool` |
| `get_resources_tags(resource_id)` | `GetResourcesTags(resourceID string) []Tag` |
| `display(enable_log)` | `Display(enableLog bool) string` |

内部方法：`setGlobalResource`/`addResourceTags`/`removeResource`/`removeResourceTags`/`replaceResourceTags`/`removeTag`/`findResourcesWithAllTags`/`normalizeTags`/`isBuiltinTag`

### 4.5 resource_registry.go — ResourceRegistry

对应 Python `resource_registry.py`。

```go
type ResourceRegistry struct {
    toolMgr          *ToolMgr
    workflowMgr      *WorkflowMgr
    promptMgr        *PromptMgr
    modelMgr         *ModelMgr
    agentMgr         *AgentMgr
    agentTeamMgr     *AgentTeamMgr
    sysOperationMgr  *SysOperationMgr
}
```

方法：`Tool()`/`Prompt()`/`Model()`/`Workflow()`/`Agent()`/`AgentTeam()`/`SysOperation()`/`RemoveByID(resourceID string)`

### 4.6 agent_manager.go — AgentMgr

对应 Python `agent_manager.py`。

```go
type AgentMgr struct {
    AbstractManager[interfaces.BaseAgent]
    // 远程 Agent 字典（分布式模式）
    // ⤵️ 预留：等 RunnerConfig（6.26）和分布式模式实现后回填
    // remoteAgents ThreadSafeDict[string, any]
}
```

方法：
- `AddAgent(agentID string, provider AgentProvider)` — 注册 Agent provider
- `RemoveAgent(agentID string) (AgentProvider, error)` — 移除 Agent
- `GetAgent(ctx context.Context, agentID string) (interfaces.BaseAgent, error)` — 获取 Agent

分布式相关（AgentAdapter/RemoteAgent/_is_remote_agent）标记 ⤵️ 预留。

### 4.7 agent_team_manager.go — AgentTeamMgr

对应 Python `agent_team_manager.py`。

**标记 ⤵️ 预留**：TeamCard 和 BaseTeam 类型不存在。

```go
type AgentTeamMgr struct {
    // ⤵️ 预留：等 multi_agent 领域实现 TeamCard/BaseTeam 后回填
    AbstractManager[any]  // 占位泛型参数
}
```

方法签名定义但标记 ⤵️。

### 4.8 model_manager.go — ModelMgr

对应 Python `model_manager.py`。

```go
type ModelMgr struct {
    AbstractManager[model_clients.BaseModelClient]
}
```

方法：
- `AddModel(modelID string, provider ModelProvider)` — 注册 Model provider
- `RemoveModel(modelID string) (ModelProvider, error)` — 移除 Model
- `GetModel(ctx context.Context, modelID string, session decorator.TracerSession) (model_clients.BaseModelClient, error)` — 获取 Model 并装饰 trace

GetModel 中调用 `decorator.DecorateModelWithTrace(model, session)` 添加 trace 装饰。

### 4.9 prompt_manager.go — PromptMgr

对应 Python `prompt_manager.py`。

**不继承 AbstractManager**，直接使用 ThreadSafeDict。

```go
type PromptMgr struct {
    repo ThreadSafeDict[string, *prompt.PromptTemplate]
}
```

方法：
- `AddPrompt(templateID string, template *prompt.PromptTemplate) error` — 验证非空 + 存储
- `AddPrompts(templates []PromptEntry)` — 批量添加
- `RemovePrompt(templateID string) (*prompt.PromptTemplate, error)` — 移除
- `GetPrompt(templateID string) (*prompt.PromptTemplate, error)` — 获取（验证非空）

辅助类型：`PromptEntry struct { ID string; Template *prompt.PromptTemplate }`

### 4.10 tool_manager.go — ToolMgr

对应 Python `tool_manager.py`（最复杂的子管理器）。

```go
// McpServerResource MCP 服务器资源
type McpServerResource struct {
    Config         *mcp.McpServerConfig
    Client         mcp.McpClient
    ToolIDs        []string
    LastUpdateTime time.Time
    ExpiryTime     *float64
}

// SysOpToolResource 系统操作工具资源
type SysOpToolResource struct {
    SysOpID        string
    ToolIDs        []string
    LastUpdateTime time.Time
}

type ToolMgr struct {
    tools               ThreadSafeDict[string, tool.Tool]
    mcpServerNameToIDs  map[string][]string                  // server_name → server_id 列表
    mcpServerResources  map[string]*McpServerResource         // server_id → McpServerResource
    sysOpResources      map[string]*SysOpToolResource         // sys_op_id → SysOpToolResource
    mcpServerLocks      map[string]*sync.Mutex                // server_id → 互斥锁
    mu                  sync.RWMutex                          // 保护 map 类型字段
}
```

方法列表：

| Python 方法 | Go 方法 |
|------------|---------|
| `add_tool(tool_id, tool)` | `AddTool(toolID string, t tool.Tool) error` |
| `get_tool(tool_id, session)` | `GetTool(toolID string, session decorator.TracerSession) (tool.Tool, error)` |
| `get_mcp_tool(tool_name, server_id, session)` | `GetMcpTool(toolName string, serverID string, session decorator.TracerSession) (tool.Tool, error)` |
| `get_mcp_tools(server_id, session)` | `GetMcpTools(serverID string, session decorator.TracerSession) ([]tool.Tool, error)` |
| `get_mcp_tool_id(server_id, tool_name)` | `GetMcpToolID(serverID string, toolName string) (string, error)` |
| `remove_tool(tool_id)` | `RemoveTool(toolID string) (tool.Tool, error)` |
| `generate_mcp_tool_id(server_id, server_name, tool_name)` | `GenerateMcpToolID(serverID, serverName, toolName string) string` (静态) |
| `add_tool_server(server_config, expiry_time)` | `AddToolServer(ctx, serverConfig, opts...WithExpiryTime) ([]*mcp.McpToolCard, error)` |
| `remove_tool_server(server_id, ignore_not_exist)` | `RemoveToolServer(ctx, serverID string, ignoreNotExist bool) ([]string, error)` |
| `add_sys_operation_tools(sys_op_id, tool_ids)` | `AddSysOperationTools(sysOpID string, toolIDs []string)` |
| `remove_sys_operation_tools(sys_op_id)` | `RemoveSysOperationTools(sysOpID string) []string` |
| `get_sys_operation_tool_ids(sys_op_id)` | `GetSysOperationToolIDs(sysOpID string) []string` |
| `refresh_tool_server(server_id, skip_not_exist, force)` | `RefreshToolServer(ctx, serverID string, skipNotExist, force bool) ([]*mcp.McpToolCard, error)` |
| `get_mcp_server_ids(server_name)` | `GetMcpServerIDs(serverName string) []string` |
| `get_mcp_client(server_id)` | `GetMcpClient(serverID string) (mcp.McpClient, error)` |
| `get_mcp_server_config(server_id)` | `GetMcpServerConfig(serverID string) (*mcp.McpServerConfig, error)` |
| `get_mcp_tool_ids(server_id)` | `GetMcpToolIDs(serverID string) []string` |
| `release()` | `Release(ctx context.Context) error` |

内部方法：`createClient`/`innerRefreshMcpTools`/`innerRemoveMcpTools`/`mcpServerLock`

`AddToolServer` 实现要点：
1. 获取 server_id 粒度的 Mutex 锁
2. 检查是否已存在（已存在则返回缓存的 tool cards）
3. 通过 `mcp.NewMcpClient(config)` 创建客户端
4. 调用 `client.Connect(ctx)` 连接
5. 调用 `innerRefreshMcpTools` 刷新工具列表
6. 更新 `mcpServerNameToIDs` 映射

### 4.11 workflow_manager.go — WorkflowMgr

对应 Python `workflow_manager.py`。

```go
type WorkflowMgr struct {
    AbstractManager[interfaces.Workflow]
}
```

方法：
- `AddWorkflow(workflowID string, provider WorkflowProvider)` — 注册 Workflow provider
- `AddWorkflows(workflows []WorkflowEntry)` — 批量添加
- `RemoveWorkflow(workflowID string) (WorkflowProvider, error)` — 移除
- `GetWorkflow(ctx context.Context, workflowID string, session decorator.TracerSession) (interfaces.Workflow, error)` — 获取并装饰 trace

辅助类型：`WorkflowEntry struct { ID string; Provider WorkflowProvider }`

### 4.12 sys_operation_manager.go — SysOperationMgr

对应 Python `sys_operation_manager.py`。

**核心逻辑标记 ⤵️**：SysOperation 类型不存在。

```go
type SysOperationMgr struct {
    // ⤵️ 预留：等 SysOperation 类型实现后回填
    // sysOperations ThreadSafeDict[string, any]
    // sandboxKeyOwnerMap map[string]string
    // mu sync.RWMutex
}
```

方法签名定义（`AddSysOperation`/`RemoveSysOperation`/`GetSysOperation`），标记 ⤵️。

### 4.13 resource_manager.go — ResourceMgr 门面类

对应 Python `resource_manager.py`（~2094行）。

```go
type ResourceMgr struct {
    registry *ResourceRegistry
    tagMgr   *TagMgr
    idToCard ThreadSafeDict[string, schema.BaseCard]
}
```

#### 4.13.1 模块级分派常量

```go
var (
    // registryAccessors 资源类型到注册表访问器名称的映射
    registryAccessors = map[string]string{
        "workflow":      "workflow",
        "agent":         "agent",
        "team":          "agent_team",
        "tool":          "tool",
        "prompt":        "prompt",
        "model":         "model",
        "sys_operation": "sys_operation",
    }
    // asyncGetTypes 需要增加 ctx 参数的 get 资源类型
    asyncGetTypes = map[string]bool{
        "workflow": true, "agent": true, "team": true, "model": true,
    }
    // sessionGetTypes get 时需要传 session 的资源类型
    sessionGetTypes = map[string]bool{
        "workflow": true, "model": true, "tool": true,
    }
    // idReturnTypes remove 时返回 ID 而非 Card 的资源类型
    idReturnTypes = map[string]bool{
        "tool": true, "prompt": true,
    }
)
```

#### 4.13.2 公开方法

**AgentTeam 操作**（标记 ⤵️ 预留）：
- `AddAgentTeam` / `RemoveAgentTeam` / `GetAgentTeam` / `GetAgentTeamsByTag`

**Agent 操作**：
- `AddAgent(card *AgentCard, provider AgentProvider, opts ...ResourceOption) error`
- `AddAgents(agents []AgentEntry, opts ...ResourceOption) error`
- `RemoveAgent(agentID string, opts ...ResourceOption) ([]*AgentCard, error)`
- `GetAgent(ctx context.Context, agentID string, opts ...ResourceOption) ([]interfaces.BaseAgent, error)`

**Workflow 操作**：
- `AddWorkflow(card *WorkflowCard, provider WorkflowProvider, opts ...ResourceOption) error`
- `AddWorkflows(workflows []WorkflowEntry, opts ...ResourceOption) error`
- `RemoveWorkflow(workflowID string, opts ...ResourceOption) ([]*WorkflowCard, error)`
- `GetWorkflow(ctx context.Context, workflowID string, opts ...ResourceOption) ([]interfaces.Workflow, error)`

**Tool 操作**：
- `AddTool(t tool.Tool, opts ...ResourceOption) error` — refresh 通过 WithRefresh() option
- `GetTool(toolID string, opts ...ResourceOption) ([]tool.Tool, error)`
- `RemoveTool(toolID string, opts ...ResourceOption) ([]string, error)` — 返回 ID

**Model 操作**：
- `AddModel(modelID string, provider ModelProvider, opts ...ResourceOption) error`
- `AddModels(models []ModelEntry, opts ...ResourceOption) error`
- `RemoveModel(modelID string, opts ...ResourceOption) ([]*schema.BaseCard, error)`
- `GetModel(ctx context.Context, modelID string, opts ...ResourceOption) ([]model_clients.BaseModelClient, error)`

**Prompt 操作**：
- `AddPrompt(promptID string, template *PromptTemplate, opts ...ResourceOption) error`
- `AddPrompts(prompts []PromptEntry, opts ...ResourceOption) error`
- `RemovePrompt(promptID string, opts ...ResourceOption) ([]string, error)` — 返回 ID
- `GetPrompt(promptID string, opts ...ResourceOption) ([]*prompt.PromptTemplate, error)`

**SysOperation 操作**（部分 ⤵️ 预留）：
- `AddSysOperation(card any, opts ...ResourceOption) error` — 基础部分实现，工具注册标记 ⤵️
- `RemoveSysOperation(sysOperationID string, opts ...ResourceOption) error`
- `GetSysOperation(sysOperationID string, opts ...ResourceOption) ([]any, error)`
- `GetSysOpToolCards(sysOperationID string, opts ...ResourceOption) ([]*schema.ToolInfo, error)` — 标记 ⤵️

**ToolInfo 操作**：
- `GetToolInfos(ctx context.Context, toolID string, opts ...ResourceOption) ([]*schema.ToolInfo, error)`

**MCP Server 操作**（Functional Options 模式）：
- `AddMcpServer(ctx context.Context, serverConfig *McpServerConfig, opts ...McpOption) ([]*McpToolCard, error)`
- `RefreshMcpServer(ctx context.Context, serverID string, opts ...McpOption) ([]*McpToolCard, error)`
- `RemoveMcpServer(ctx context.Context, serverID string, opts ...McpOption) ([]string, error)`
- `GetMcpTool(ctx context.Context, name string, serverID string, opts ...McpOption) ([]tool.Tool, error)`
- `GetMcpToolInfos(ctx context.Context, name string, serverID string, opts ...McpOption) ([]*schema.ToolInfo, error)`
- `GetMcpServerConfig(serverID string) (*McpServerConfig, error)`
- `GetMcpToolIDs(serverID string) []string`
- `ListMcpResources(ctx context.Context, serverID string) ([]any, error)`
- `ReadMcpResource(ctx context.Context, serverID string, uri string) (any, error)`

McpOption 包含：`WithMcpServerName`/`WithMcpTag`/`WithMcpExpiryTime`/`WithMcpSkipIfNotExists`/`WithMcpForce`/`WithMcpSession`

**Tag 操作**（委托 tagMgr）：
- `GetResourceByTag(tag Tag) []*schema.BaseCard`
- `ListTags() []Tag`
- `HasTag(tag Tag) bool`
- `RemoveTag(ctx context.Context, tag Tag, opts ...TagOption) ([]string, error)`
- `UpdateResourceTag(resourceID string, tag Tag) ([]Tag, error)`
- `AddResourceTag(resourceID string, tag Tag) ([]Tag, error)`
- `RemoveResourceTag(resourceID string, tag Tag, opts ...TagOption) ([]Tag, error)`
- `GetResourceTag(resourceID string) []Tag`
- `ResourceHasTag(resourceID string, tag Tag) bool`

**生命周期**：
- `Release(ctx context.Context) error` — 释放所有 MCP 连接，重建注册表/标签管理器/card缓存

#### 4.13.3 内部核心方法

```go
// getMgr 根据 resource_type 获取对应子管理器
func (rm *ResourceMgr) getMgr(resourceType string) any

// dispatchAdd 分发到子管理器的 add 方法
func (rm *ResourceMgr) dispatchAdd(resourceType, resourceID string, resource any)

// dispatchRemove 分发到子管理器的 remove 方法
func (rm *ResourceMgr) dispatchRemove(resourceType, resourceID string) any

// dispatchGet 分发到子管理器的 get 方法
func (rm *ResourceMgr) dispatchGet(ctx context.Context, resourceType, resourceID string, session any) (any, error)

// innerAddResource 核心添加逻辑：验证 → dispatch_add → 缓存card → tag标记 → 日志
func (rm *ResourceMgr) innerAddResource(resourceID, resourceType string, resource any, resourceCard *schema.BaseCard, tag Tag) error

// innerRemoveResources 核心移除逻辑：支持按ID/标签移除
func (rm *ResourceMgr) innerRemoveResources(resourceID, resourceType string, tag Tag, tagMatchStrategy TagMatchStrategy, skipIfTagNotExists bool) ([]any, error)

// innerFindResourceIDs 按ID或标签查找资源ID列表
func (rm *ResourceMgr) innerFindResourceIDs(resourceID, resourceType string, tag Tag, tagMatchStrategy TagMatchStrategy) ([]string, bool, error)

// innerGetResources 同步获取资源实例（tool/prompt/sys_operation）
func (rm *ResourceMgr) innerGetResources(resourceID, resourceType string, tag Tag, tagMatchStrategy TagMatchStrategy, session any) ([]any, error)

// innerGetResourcesByProvider 通过provider获取资源实例（agent/team/workflow/model）
func (rm *ResourceMgr) innerGetResourcesByProvider(ctx context.Context, resourceID, resourceType string, tag Tag, tagMatchStrategy TagMatchStrategy, session any) ([]any, error)
```

#### 4.13.4 验证方法

与 Python 完整一一对照：

```go
func (rm *ResourceMgr) innerValidateTag(tag Tag) error
func (rm *ResourceMgr) innerValidateResourceCard(card *schema.BaseCard, resourceType string, cardClassType reflect.Type) error
func (rm *ResourceMgr) innerValidateResourceID(resourceID, resourceType string) error
func (rm *ResourceMgr) innerValidateResourceIDs(resourceIDs []string, resourceType string) error
func (rm *ResourceMgr) innerValidateProvider(provider any, resourceType string) error
func (rm *ResourceMgr) innerValidateProviders(providers []any, resourceType string, cardClassType reflect.Type) error
func (rm *ResourceMgr) innerValidateResource(instance any, resourceType string, resourceClassType reflect.Type) error
func (rm *ResourceMgr) innerValidateServerConfig(serverConfig *mcp.McpServerConfig) error
func (rm *ResourceMgr) getCardType(card *schema.BaseCard) string
func (rm *ResourceMgr) innerGetServerIDs(serverID, serverName string, tag Tag, tagMatchStrategy TagMatchStrategy, skipIfNotExists bool, errorCode exception.StatusCode) ([]string, error)
```

## 5. Functional Options 定义

### 5.1 ResourceOption

```go
type ResourceOption func(*resourceOptions)

type resourceOptions struct {
    Tag                Tag
    TagMatchStrategy   TagMatchStrategy
    SkipIfTagNotExists bool
    Session            decorator.TracerSession
    Refresh            bool   // Tool 专用：是否覆盖已有注册
    InterfaceURL       string // Agent 专用：分布式接口URL
}

func WithTag(tag Tag) ResourceOption
func WithTagMatchStrategy(strategy TagMatchStrategy) ResourceOption
func WithSkipIfTagNotExists() ResourceOption
func WithSession(session decorator.TracerSession) ResourceOption
func WithRefresh() ResourceOption
func WithInterfaceURL(url string) ResourceOption
```

### 5.2 McpOption

```go
type McpOption func(*mcpOptions)

type mcpOptions struct {
    ServerName      string
    Tag             Tag
    ExpiryTime      float64
    SkipIfNotExists bool
    Force           bool
    Session         decorator.TracerSession
}

func WithMcpServerName(name string) McpOption
func WithMcpTag(tag Tag) McpOption
func WithMcpExpiryTime(seconds float64) McpOption
func WithMcpSkipIfNotExists() McpOption
func WithMcpForce() McpOption
func WithMcpSession(session decorator.TracerSession) McpOption
```

### 5.3 TagOption

```go
type TagOption func(*tagOptions)

type tagOptions struct {
    SkipIfNotExists bool
}

func WithSkipIfTagNotExists() TagOption
```

## 6. 对现有代码的影响

### 6.1 删除

| 文件/目录 | 原因 |
|-----------|------|
| `single_agent/resource/doc.go` | 占位代码，6.23 正式实现替代 |
| `single_agent/resource/resource_manager.go` | 同上 |
| `single_agent/resource/resource_manager_test.go` | 同上 |

### 6.2 修改

| 文件 | 修改内容 |
|------|---------|
| `single_agent/ability/ability_manager.go` | 导入改为 `runner/resources_manager`；`resourceMgr` 字段类型改为 `*resources_manager.ResourceMgr`；调用方法签名适配 |
| `single_agent/ability/ability_manager_test.go` | 更新 fakeResourceManager 为新接口 |
| `single_agent/base.go` | 删除 ResourceManager/NoopResourceManager/ResourceOptions/ResourceOption 的 re-export |
| `single_agent/agents/react_agent.go` | 删除 NoopResourceManager 引用，构造时传入 nil 或从全局获取 |
| `single_agent/doc.go` | 移除 resource/ 条目 |
| `runner/doc.go` | 添加 resources_manager/ 子目录描述 |

### 6.3 新增

`runner/resources_manager/` 下 14 个 Go 文件 + 14 个测试文件 + 1 个 doc.go = 29 个文件。

## 7. 依赖关系

```
resource_manager.go
  ├── resource_registry.go
  │     ├── agent_manager.go ── AbstractManager, ⤵️分布式
  │     ├── agent_team_manager.go ── ⤵️预留
  │     ├── workflow_manager.go ── AbstractManager, decorator
  │     ├── model_manager.go ── AbstractManager, decorator
  │     ├── prompt_manager.go ── ThreadSafeDict
  │     ├── tool_manager.go ── mcp.NewMcpClient, decorator
  │     └── sys_operation_manager.go ── ⤵️预留
  ├── tag_manager.go ── base(Tag常量/枚举), logger, exception
  ├── base.go ── Provider类型, Tag常量, 枚举
  ├── thread_safe_dict.go
  └── abstract_manager.go ── thread_safe_dict
```

外部依赖：
- `internal/agentcore/foundation/tool` — Tool, ToolCard
- `internal/agentcore/foundation/tool/mcp` — McpClient, McpServerConfig, MCPTool, McpToolCard
- `internal/agentcore/foundation/prompt` — PromptTemplate
- `internal/agentcore/foundation/llm/model_clients` — BaseModelClient
- `internal/agentcore/single_agent/interfaces` — BaseAgent, Workflow
- `internal/agentcore/single_agent/schema` — AgentCard
- `internal/agentcore/session/tracer/decorator` — DecorateModelWithTrace, DecorateToolWithTrace, DecorateWorkflowWithTrace
- `internal/common/schema` — BaseCard, WorkflowCard, ToolInfo
- `internal/common/exception` — BuildError, StatusCode
- `internal/common/logger` — 结构化日志
