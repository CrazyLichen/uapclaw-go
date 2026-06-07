# 领域 3.13 — AbilityManager 工具/Workflow/Agent 注册与调度

> 对应 Python 源码：`openjiuwen/core/single_agent/ability_manager.py`
> 依赖：`openjiuwen/core/foundation/tool/base.py` (ToolCard)、`openjiuwen/core/workflow/base.py` (WorkflowCard)、`openjiuwen/core/single_agent/schema/agent_card.py` (AgentCard)、`openjiuwen/core/foundation/tool/mcp/base.py` (McpServerConfig)

## 1. 概述

AbilityManager 是 Agent 的能力注册与调度中心，管理四类 Ability（Tool / Workflow / Agent / McpServer）的完整生命周期：

- **注册管理**：添加、移除、查询 Ability Card，按 name 去重（重复时保留旧的）
- **LLM 工具描述生成**：将各类 Card 转为 ToolInfo 供 LLM function calling 消费
- **并行执行**：接收 LLM 返回的多个 ToolCall，并行调度执行，收集全部结果
- **JSON 参数修复**：处理 LLM 输出的畸形 JSON（补全缺失的闭合括号）
- **路由分发**：根据 tool_name 查找对应类型的 Card，从 ResourceManager 获取实例后执行

AbilityManager 只存储 Card 元数据，不持有 Tool/Workflow/Agent 实例。实例获取通过 ResourceManager 接口委托。

## 2. 核心决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| ResourceManager 依赖 | 在 3.13 包内定义接口 + NoopResourceManager 空实现 | 3.13 只关注注册/查询/JSON 修复/并行调度，实例获取留到领域六/九回填 |
| Rail 生命周期 | 只定义 ToolRail 接口，不实现 @rail 装饰器 | @rail 装饰器和 AgentCallbackContext 实现留到 6.4-6.10，execute 内预留钩子调用点 |
| Ability 联合类型 | 在 common/schema 提前定义完整 WorkflowCard 和 AgentCard | 3.13 直接引用，领域六实现时无需重新定义；Ability 接口用 AbilityKind 枚举区分类型 |
| 并行执行模型 | sync.WaitGroup + channel 收集 | 与 Python `asyncio.gather(return_exceptions=True)` 语义一致：所有任务都执行完，错误作为结果的一部分返回 |
| MCP 懒加载 | 预留接口，当前跳过 | ResourceManager.GetMcpToolInfos 定义为接口方法，NoopResourceManager 返回空；list_tool_info 中 MCP 懒加载分支标注 ⤵️ 留待回填 |
| paid_search 优先 | 完整实现，对齐 Python | 搜索场景核心排序策略，paid_search 必须排在 free_search 前面 |

## 3. 包结构

```
internal/agentcore/single_agent/
├── doc.go                    # 包文档
├── ability_manager.go        # AbilityManager 核心结构 + 注册/查询/执行
├── ability_types.go          # Ability 接口 + AddAbilityResult + AbilityExecutionError + AbilityKind
├── json_repair.go            # RepairToolArgumentsJSON + ParseToolArguments
├── resource_manager.go       # ResourceManager 接口 + NoopResourceManager
├── ability_manager_test.go   # AbilityManager 核心测试
├── ability_types_test.go     # Ability 类型测试
├── json_repair_test.go       # JSON 修复测试
└── resource_manager_test.go  # ResourceManager 接口测试
```

## 4. 模块一：Ability 联合类型

### 4.1 common/schema 新增类型

在 `common/schema/card.go` 中新增 WorkflowCard 和 AgentCard：

```go
// WorkflowCard 工作流配置卡片，嵌入 BaseCard，增加版本号和输入参数定义。
//
// 对应 Python: openjiuwen/core/workflow/base.py (WorkflowCard)
type WorkflowCard struct {
    BaseCard
    // Version 工作流版本号
    Version string `json:"version,omitempty"`
    // InputParams 输入参数定义（JSON Schema 格式）
    InputParams map[string]any `json:"input_params,omitempty"`
}

// AgentCard Agent 配置卡片，嵌入 BaseCard，增加输入/输出参数和接口 URL。
//
// 对应 Python: openjiuwen/core/single_agent/schema/agent_card.py (AgentCard)
type AgentCard struct {
    BaseCard
    // InputParams 输入参数定义（JSON Schema 格式）
    InputParams map[string]any `json:"input_params,omitempty"`
    // OutputParams 输出参数定义（JSON Schema 格式）
    OutputParams map[string]any `json:"output_params,omitempty"`
    // InterfaceURL A2A JSON-RPC 基础 URL
    InterfaceURL string `json:"interface_url,omitempty"`
}
```

### 4.2 Ability 接口

在 `ability_types.go` 中定义：

```go
// AbilityKind 能力类型枚举
type AbilityKind int

const (
    // AbilityKindTool 工具能力
    AbilityKindTool AbilityKind = iota
    // AbilityKindWorkflow 工作流能力
    AbilityKindWorkflow
    // AbilityKindAgent Agent 能力
    AbilityKindAgent
    // AbilityKindMcpServer MCP 服务器能力
    AbilityKindMcpServer
)

// Ability 四类能力的统一接口，ToolCard/WorkflowCard/AgentCard/McpServerConfig 均实现此接口。
type Ability interface {
    // AbilityName 返回能力名称
    AbilityName() string
    // AbilityID 返回能力唯一标识
    AbilityID() string
    // AbilityKind 返回能力类型
    AbilityKind() AbilityKind
}
```

为四种 Card 类型实现 Ability 接口的方法（定义在各 Card 所属包中）。

### 4.3 AddAbilityResult

```go
// AddAbilityResult 添加能力的返回结果
type AddAbilityResult struct {
    // Name 能力名称
    Name string
    // Added 是否成功添加
    Added bool
    // Reason 未添加的原因（如 "duplicate_tool"）
    Reason string
}
```

### 4.4 AbilityExecutionError

```go
// AbilityExecutionError 能力执行统一异常，嵌入 BaseError 并关联 ToolMessage。
//
// 对应 Python: AbilityExecutionError
type AbilityExecutionError struct {
    *exception.BaseError
    // ToolMessage 关联的工具返回消息
    ToolMessage *llmschema.ToolMessage
}
```

## 5. 模块二：AbilityManager 核心

### 5.1 结构体

```go
// AbilityManager Agent 能力注册与调度中心
//
// 职责：
//   - 存储可用 Ability Card（仅元数据，不持有实例）
//   - 提供 add/remove/query 接口
//   - 将 Card 转为 ToolInfo 供 LLM 使用
//   - 执行 Ability 调用（从 ResourceManager 获取实例）
type AbilityManager struct {
    tools         map[string]*tool.ToolCard
    workflows     map[string]*schema.WorkflowCard
    agents        map[string]*schema.AgentCard
    mcpServers    map[string]*mcp.McpServerConfig
    contextEngine ContextEngine     // ⤵️ 预留，领域五回填
    resourceMgr   ResourceManager
}
```

### 5.2 注册接口

```go
// Add 添加一个或多个能力。单个返回 AddAbilityResult，批量返回 []AddAbilityResult。
// 重复 name 时保留已有的，记录 Warn 日志，返回 Added=false。
func (am *AbilityManager) Add(ability Ability) AddAbilityResult
func (am *AbilityManager) AddMany(abilities []Ability) []AddAbilityResult

// Remove 按名称移除能力，返回被移除的 Ability（未找到返回 nil）。
// 移除 McpServer 时同时移除其关联工具。
func (am *AbilityManager) Remove(name string) Ability
func (am *AbilityManager) RemoveMany(names []string) []Ability

// Get 按名称查询能力（依次查找 tools → workflows → agents → mcpServers）
func (am *AbilityManager) Get(name string) Ability

// List 列出所有已注册能力
func (am *AbilityManager) List() []Ability

// ReorderTools 按给定名称顺序重排 tools 注册表
func (am *AbilityManager) ReorderTools(orderedNames []string)
```

### 5.3 ListToolInfo

```go
// ListToolInfo 获取 ToolInfo 列表供 LLM function calling 消费。
// names 非空时只返回指定名称的工具；为空时返回全部。
func (am *AbilityManager) ListToolInfo(ctx context.Context, names []string) ([]*schema.ToolInfo, error)
```

内部逻辑：

1. **ToolCards → ToolInfo**：遍历 `_prioritizePaidSearch` 排序后的 tools，排除 MCP 服务器下的工具（`_isToolInMcpServer`），调用 `ToolCard.ToolInfo()`
2. **WorkflowCards → ToolInfo**：遍历 workflows，直接用 name/description/InputParams 构造 ToolInfo
3. **AgentCards → ToolInfo**：遍历 agents，根据 InputParams 构造 ToolInfo（nil 时用空 object schema）
4. **MCP 懒加载**：`⤵️ 预留`，遍历 mcpServers，通过 `resourceMgr.GetMcpToolInfos()` 获取工具列表并注册到 tools

#### 5.3.1 prioritizePaidSearch

```go
// toolItem 内部辅助类型，用于 prioritizePaidSearch 的输入
type toolItem struct {
    name string
    card *tool.ToolCard
}

// prioritizePaidSearch 当 paid_search 和 free_search 同时存在时，
// 确保 paid_search 排在 free_search 前面。
func prioritizePaidSearch(items []toolItem) []toolItem
```

逻辑：找到 paid_search 和 free_search 的索引，若 paid 在 free 后面则将 paid 移到 free 前面。

### 5.4 Execute 并行执行

```go
// ExecuteResult 单个工具调用的执行结果
type ExecuteResult struct {
    // Result 执行结果
    Result any
    // ToolMsg 返回给 LLM 的 ToolMessage
    ToolMsg *llmschema.ToolMessage
    // Err 执行错误（如有）
    Err error
}

// Execute 并行执行多个 ToolCall，返回每个调用的结果。
// 使用 WaitGroup + channel 收集，与 Python asyncio.gather(return_exceptions=True) 语义一致：
// 所有任务都执行完毕，错误作为 ExecuteResult.Err 返回。
func (am *AbilityManager) Execute(
    ctx context.Context,
    toolCalls []*llmschema.ToolCall,
    session Session,  // ⤵️ 预留，领域五回填
    tag string,
) []ExecuteResult
```

内部流程：

1. `_normalizeToolCalls`：将输入归一化为 `[]*ToolCall`
2. 为每个 ToolCall 创建独立执行上下文
3. 每个调用在一个 goroutine 中执行 `_railedExecuteSingleToolCall`
4. WaitGroup 等待全部完成，channel 收集结果
5. 结果处理：
   - `ToolInterruptException` → 透传异常
   - `context.Cancelled` → 构造中断 ToolMessage
   - `AbilityExecutionError` → 提取 ToolMessage
   - 其他异常 → 构造错误 ToolMessage
6. force_finish 信号传播：`⤵️ 预留`（等 6.4-6.10 Rail 系统就绪后回填）

### 5.5 _railedExecuteSingleToolCall

```go
// railedExecuteSingleToolCall 在 Rail 生命周期内执行单个工具调用。
// 当前阶段直接调用 executeSingleToolCall，Rail 钩子调用点预留。
func (am *AbilityManager) railedExecuteSingleToolCall(
    ctx context.Context,
    toolCall *llmschema.ToolCall,
    session Session,
    tag string,
) ExecuteResult
```

Rail 预留：

```go
// ⤵️ 预留：BeforeToolCall Rail 钩子
// if am.rail != nil { ... }

result := am.executeSingleToolCall(ctx, toolCall, session, tag)

// ⤵️ 预留：AfterToolCall Rail 钩子
// if am.rail != nil { ... }
```

### 5.6 executeSingleToolCall

```go
// executeSingleToolCall 执行单个工具调用。
// 路由逻辑：按 tool_name 查找 Card → 从 ResourceManager 获取实例 → 执行。
func (am *AbilityManager) executeSingleToolCall(
    ctx context.Context,
    toolCall *llmschema.ToolCall,
    session Session,
    tag string,
) ExecuteResult
```

路由分支：

1. **tools 中存在** → `resourceMgr.GetTool(toolID)` → `tool.Invoke(toolArgs)`
2. **workflows 中存在** → `resourceMgr.GetWorkflow(workflowID)` → `workflow.Execute(toolArgs)` ⤵️ 预留 workflow 执行
3. **agents 中存在** → `resourceMgr.GetAgent(agentID)` → `agent.Invoke(toolArgs)` ⤵️ 预留 agent 执行
4. **mcpServers 中存在** → 返回 `AbilityExecutionError`（MCP 工具执行暂未实现）
5. **均未匹配** → 兜底：`resourceMgr.GetTool(toolName)` → `tool.Invoke(toolArgs)`

所有分支的错误统一包装为 `AbilityExecutionError`，包含 ToolMessage。

## 6. 模块三：JSON 参数修复

### 6.1 RepairToolArgumentsJSON

```go
// RepairToolArgumentsJSON 尝试修复畸形的 JSON 字符串，通过补全缺失的闭合括号。
// 返回修复后的字符串；无法修复时返回 nil。
//
// 对应 Python: AbilityManager._repair_tool_arguments_json
func RepairToolArgumentsJSON(arguments string) *string
```

算法：
1. 遍历字符串，跟踪 `in_string` / `escape` / `stack`（`{` 和 `[`）
2. 字符串内的括号不计入栈
3. 遍历结束后：
   - 仍在字符串内 → 返回 nil（无法修复）
   - 栈为空 → 原样返回
   - 栈非空 → 按栈逆序补全闭合符号

### 6.2 ParseToolArguments

```go
// ParseToolArguments 将工具调用参数解析为 map[string]any。
// 先尝试 json.Unmarshal；失败后尝试 Repair + 再次 Unmarshal；
// 仍失败则返回 ValueError，包含原始 JSON 和错误信息。
//
// 对应 Python: AbilityManager._parse_tool_arguments
func ParseToolArguments(arguments string) (map[string]any, error)
```

## 7. 模块四：ResourceManager 接口

### 7.1 接口定义

```go
// ResourceManager 实例获取接口，AbilityManager 通过此接口获取 Tool/Workflow/Agent 实例。
// 具体实现由领域六/九提供，3.13 阶段使用 NoopResourceManager。
type ResourceManager interface {
    // GetTool 按 ID 获取工具实例
    GetTool(toolID string, opts ...ResourceOption) (tool.Tool, error)
    // GetWorkflow 按 ID 获取工作流实例
    GetWorkflow(workflowID string, opts ...ResourceOption) (Workflow, error)
    // GetAgent 按 ID 获取 Agent 实例
    GetAgent(agentID string, opts ...ResourceOption) (Agent, error)
    // GetMcpToolInfos 获取 MCP 服务器的工具描述列表
    GetMcpToolInfos(serverID string) ([]*schema.ToolInfo, error)
}
```

### 7.2 最小依赖接口

Workflow 和 Agent 在 3.13 阶段只需最小接口定义：

```go
// Workflow 工作流执行接口（最小定义，领域八扩展）
type Workflow interface {
    // Execute 执行工作流
    Execute(ctx context.Context, inputs map[string]any, opts ...WorkflowOption) (any, error)
}

// Agent Agent 执行接口（最小定义，领域六扩展）
type Agent interface {
    // Invoke 调用 Agent
    Invoke(ctx context.Context, inputs map[string]any, opts ...AgentOption) (any, error)
}
```

### 7.3 NoopResourceManager

```go
// NoopResourceManager ResourceManager 的空实现，3.13 阶段使用。
// 所有方法返回 NotFound 错误。
type NoopResourceManager struct{}
```

### 7.4 ResourceOption

```go
// ResourceOption 实例获取选项函数
type ResourceOption func(*ResourceOptions)

// ResourceOptions 实例获取选项
type ResourceOptions struct {
    // Tag 资源标签
    Tag string
    // Session 会话实例 ⤵️ 预留
    Session Session
}
```

## 8. 辅助类型与接口

### 8.1 ContextEngine 预留

```go
// ContextEngine 上下文引擎接口（预留，领域五回填）
type ContextEngine interface {
    // CreateContext 创建上下文
    CreateContext(ctx context.Context, contextID string, session Session) (any, error)
}
```

### 8.2 Session 预留

```go
// Session 会话接口（预留，领域五回填）
type Session interface {
    // GetSessionID 获取会话 ID
    GetSessionID() string
    // CreateWorkflowSession 创建工作流子会话 ⤵️ 预留
    CreateWorkflowSession() Session
    // GetState 获取会话状态
    GetState(key string) any
    // UpdateState 更新会话状态
    UpdateState(state map[string]any)
}
```

### 8.3 ToolRail 接口预留

```go
// ToolRail 工具调用生命周期钩子接口（3.13 只定义，6.4-6.10 实现）
type ToolRail interface {
    // BeforeToolCall 工具调用前触发
    BeforeToolCall(ctx context.Context, callCtx *ToolCallContext) (*ToolCallContext, error)
    // AfterToolCall 工具调用后触发
    AfterToolCall(ctx context.Context, callCtx *ToolCallContext, result *ToolCallResult) (*ToolCallResult, error)
    // OnToolException 工具调用异常时触发
    OnToolException(ctx context.Context, callCtx *ToolCallContext, err error) error
}

// ToolCallContext 工具调用上下文（预留，6.5 回填）
type ToolCallContext struct {
    ToolCall  *llmschema.ToolCall
    ToolName  string
    ToolArgs  map[string]any
    ToolResult any
    ToolMsg   *llmschema.ToolMessage
    // ⤵️ 预留字段：force_finish / steering_queue / skip_tool
}

// ToolCallResult 工具调用结果（预留）
type ToolCallResult struct {
    Result  any
    ToolMsg *llmschema.ToolMessage
}
```

## 9. BuildToolMessageContent 辅助方法

```go
// BuildToolMessageContent 从执行结果中提取 ToolMessage 的 content 字段。
//
// 提取逻辑（对齐 Python _build_tool_message_content）：
// 1. 结果有 data.content 字段 → 返回 content
// 2. 结果 success=false 且有 error → 返回 error
// 3. 其他 → 字符串化结果
func BuildToolMessageContent(result any) string
```

## 10. 日志规范

按照项目日志同步规则，对齐 Python 源码中的所有 logger 调用：

| Python 日志点 | Go 日志 | 级别 | 组件 |
|--------------|---------|------|------|
| `Duplicate tool ability detected` | `.Str("ability_name", ...).Str("existing_id", ...).Str("new_id", ...).Msg("检测到重复工具能力，保留已有能力，跳过新增")` | Warn | ComponentAgentCore |
| `Duplicate workflow ability detected` | 同上模式 | Warn | ComponentAgentCore |
| `Duplicate agent ability detected` | 同上模式 | Warn | ComponentAgentCore |
| `Duplicate MCP server ability detected` | 同上模式 | Warn | ComponentAgentCore |
| `Unknown ability type` | `.Str("ability_type", ...).Msg("未知能力类型")` | Warn | ComponentAgentCore |
| `Recovered malformed tool arguments` | `.Msg("通过补全闭合括号修复了畸形的工具参数")` | Warn | ComponentAgentCore |
| `Tool got malformed arguments` | `.Str("tool_name", ...).Err(err).Msg("工具收到畸形参数")` | Error | ComponentAgentCore |
| `Tool execution error` | `.Str("tool_name", ...).Err(err).Msg("工具执行错误")` | Error | ComponentAgentCore |
| `Workflow execution error` | `.Str("workflow_name", ...).Err(err).Msg("工作流执行错误")` | Error | ComponentAgentCore |
| `Agent execution error` | `.Str("agent_name", ...).Err(err).Msg("Agent 执行错误")` | Error | ComponentAgentCore |
| `[Interrupted] Tool execution was cancelled` | `.Str("tool_name", ...).Msg("工具执行被用户取消")` | Warn | ComponentAgentCore |
| `Ability execution error` | `.Err(err).Msg("能力执行错误")` | Error | ComponentAgentCore |

## 11. 测试策略

### 11.1 JSON 修复测试（json_repair_test.go）

- 正常 JSON 原样返回
- 缺失尾部 `}` → 补全
- 缺失尾部 `]` → 补全
- 嵌套缺失 `}]` → 补全
- 字符串内的括号不计入
- 转义引号正确处理
- 仍在字符串内 → 返回 nil
- 空字符串 → 返回 nil
- ParseToolArguments 正常解析
- ParseToolArguments 修复后解析
- ParseToolArguments 无法修复时返回详细错误

### 11.2 Ability 类型测试（ability_types_test.go）

- AddAbilityResult 序列化
- AbilityExecutionError 构造和字段
- 四种 Card 类型的 Ability 接口实现

### 11.3 AbilityManager 核心测试（ability_manager_test.go）

使用 fakeResourceManager（非 Noop，提供可控制的 fake Tool 实例）：

- Add 单个/批量能力
- Add 重复 name 时保留旧的
- Remove 单个/批量
- Remove McpServer 时级联删除关联工具
- Get 按名称查找（各类型）
- List 列出全部
- ReorderTools 重排
- ListToolInfo 各类型转换
- prioritizePaidSearch 排序
- Execute 并行执行成功
- Execute 并行执行部分失败
- Execute 参数解析失败
- Execute 能力未找到
- BuildToolMessageContent 各分支

### 11.4 ResourceManager 测试（resource_manager_test.go）

- NoopResourceManager 各方法返回 NotFound 错误
- ResourceOption 函数式选项

## 12. 回填点汇总

| 回填点 | 目标领域 | 说明 |
|--------|---------|------|
| ContextEngine 实现 | 领域五 | AbilityManager.contextEngine 字段，workflow 执行时创建上下文 |
| Session 实现 | 领域五 | execute/session 参数，workflow/agent 子会话创建 |
| ResourceManager 实现 | 领域六/九 | 真正的实例获取逻辑（GetTool/GetWorkflow/GetAgent） |
| MCP 懒加载 | 领域九 | list_tool_info 中通过 resourceMgr.GetMcpToolInfos 动态获取 |
| @rail 装饰器 + AgentCallbackContext | 6.4-6.10 | railedExecuteSingleToolCall 的 BEFORE/AFTER/ON_EXCEPTION 钩子 |
| force_finish 信号传播 | 6.4-6.10 | execute 中从子 context 向父 context 传播 force_finish |
| ToolInterruptException | 6.14-6.16 | execute 中的中断异常处理 |
| Workflow 执行 | 领域八 | executeSingleToolCall 中 workflow 分支的完整执行逻辑 |
| Agent 执行 | 领域六 | executeSingleToolCall 中 agent 分支的完整执行逻辑 |
