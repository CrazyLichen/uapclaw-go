# 领域 3.1 — Tool 接口与 ToolCard 设计

> 对应 Python 源码：`openjiuwen/core/foundation/tool/base.py` + `openjiuwen/core/foundation/tool/schema.py`

## 1. 概述

Tool 是 Agent 调用外部能力的统一抽象。LLM 返回 ToolCall 后，Agent 通过 Tool 接口执行工具调用并拿回结果。本设计覆盖 Tool 接口、ToolCard、LifecycleTool 包装器及子类框架。

## 2. 核心决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 回调生命周期 | Tool 接口纯业务 + LifecycleTool 包装器 + 注册时自动包装 | Go 无元类，包装器模式最自然；注册时自动包装对调用方透明 |
| Stream 不支持的处理 | 统一接口 + 返回 ErrStreamNotSupported | 和 Python 保持一致，简单直接 |
| inputs 参数类型 | `map[string]any` | LLM 返回的 ToolCall.Arguments 本身就是 JSON，直接用 map 传递最自然 |
| 扩展参数传递 | 函数式选项 `ToolOption` | 和项目已有模式（CardOption）一致，灵活可扩展 |
| input_params 类型 | `[]*schema.Param` | 利用项目已有类型，有类型安全和校验能力；ToolInfo() 时转为 JSON Schema map |
| 子类覆写机制 | 接口模式，子类独立实现 Tool 接口 | Go 没有 virtual dispatch，接口模式最 Go-idiomatic |
| 文件组织 | 完全镜像 Python 目录结构 | 和 Python 一一对应，查找方便 |

## 3. 类型定义

### 3.1 Tool 接口

```go
// Tool 工具接口，所有工具类型（LocalFunction/MCPTool/RestfulApi）的统一抽象
type Tool interface {
    // Card 返回工具的配置卡片
    Card() *ToolCard
    // Invoke 一次性执行工具，返回完整结果
    Invoke(ctx context.Context, inputs map[string]any, opts ...ToolOption) (map[string]any, error)
    // Stream 流式执行工具，逐步返回结果块
    Stream(ctx context.Context, inputs map[string]any, opts ...ToolOption) (<-chan StreamChunk, error)
}
```

### 3.2 ToolCard

```go
// ToolCard 工具配置卡片，嵌入 BaseCard
type ToolCard struct {
    schema.BaseCard
    // InputParams 输入参数定义，用于校验和生成 ToolInfo 传给 LLM
    InputParams []*schema.Param
    // Properties 扩展属性
    Properties map[string]any
}

// ToolInfo 从 ToolCard 生成工具描述信息（供 LLM function calling 消费）
func (c *ToolCard) ToolInfo() *schema.ToolInfo {
    // 将 []*Param 转为 JSON Schema map，构造 ToolInfo 返回
}
```

### 3.3 ToolOption 与 ToolCallOptions

```go
// ToolOption 工具调用选项函数
type ToolOption func(*ToolCallOptions)

// ToolCallOptions 工具调用的扩展选项
type ToolCallOptions struct {
    // SkipNoneValue 是否跳过 None 值（LocalFunction 使用）
    SkipNoneValue bool
    // SkipInputsValidate 是否跳过输入校验（LocalFunction 使用）
    SkipInputsValidate bool
    // Timeout 超时时间，单位秒（RestfulApi 使用）
    Timeout float64
    // MaxResponseBytes 最大响应字节数（RestfulApi 使用）
    MaxResponseBytes int
    // RaiseForStatus HTTP 错误是否抛异常（RestfulApi 使用）
    RaiseForStatus bool
}

// WithSkipNoneValue 设置是否跳过 None 值
func WithSkipNoneValue(skip bool) ToolOption { ... }

// WithSkipInputsValidate 设置是否跳过输入校验
func WithSkipInputsValidate(skip bool) ToolOption { ... }

// WithTimeout 设置超时时间
func WithTimeout(d float64) ToolOption { ... }

// WithMaxResponseBytes 设置最大响应字节数
func WithMaxResponseBytes(n int) ToolOption { ... }

// WithRaiseForStatus 设置 HTTP 错误是否抛异常
func WithRaiseForStatus(raise bool) ToolOption { ... }
```

### 3.4 StreamChunk

```go
// StreamChunk 流式执行的返回块
type StreamChunk struct {
    // Data 本块数据
    Data map[string]any
    // Error 非 nil 表示流结束且出错
    Error error
    // Done true 表示流正常结束（Data 为空）
    Done bool
}
```

### 3.5 Card 继承关系（Go 嵌入）

```
BaseCard (common/schema 已实现)
  └── ToolCard (嵌入 BaseCard + InputParams + Properties)
        ├── McpToolCard (嵌入 ToolCard + ServerName + ServerID)
        └── RestfulApiCard (嵌入 ToolCard + URL + Method + Headers + Queries + Paths + Timeout + ...)
```

## 4. LifecycleTool 包装器

### 4.1 设计思路

Python 中 `_ToolMeta` 元类在 Tool 实例化时自动注入 11 种 `ToolCallEvents` 生命周期回调。Go 中没有元类，采用包装器模式：

- **Tool 接口只定义纯业务方法**（Invoke/Stream/Card）
- **LifecycleTool 包装器**在调用前后触发回调事件
- **AbilityManager 注册时自动包装**，对调用方透明

### 4.2 LifecycleTool 结构

```go
// LifecycleTool 包装 Tool，在 Invoke/Stream 调用前后自动触发回调事件
type LifecycleTool struct {
    inner Tool
    fw    *callback.Framework
}

// NewLifecycleTool 创建带生命周期回调的工具包装器
func NewLifecycleTool(inner Tool, fw *callback.Framework) *LifecycleTool

// Card 委托给内部 Tool
func (t *LifecycleTool) Card() *ToolCard

// Invoke 包装生命周期：STARTED → 执行 → FINISHED/ERROR
func (t *LifecycleTool) Invoke(ctx context.Context, inputs map[string]any, opts ...ToolOption) (map[string]any, error)
// 内部流程：
// 1. 触发 TOOL_CALL_STARTED（tool_name, tool_id, inputs）
// 2. emit_before TOOL_INVOKE_INPUT
// 3. result, err := t.inner.Invoke(ctx, inputs, opts...)
// 4. if err → 触发 TOOL_CALL_ERROR（含 event_type=LLM_CALL_ERROR 上下文）
// 5. emit_after TOOL_INVOKE_OUTPUT
// 6. 触发 TOOL_CALL_FINISHED（tool_name, tool_id, inputs, result）

// Stream 包装生命周期：STARTED → 逐 chunk 触发 RESULT_RECEIVED → FINISHED/ERROR
func (t *LifecycleTool) Stream(ctx context.Context, inputs map[string]any, opts ...ToolOption) (<-chan StreamChunk, error)
// 内部流程：
// 1. 触发 TOOL_CALL_STARTED
// 2. emit_before TOOL_STREAM_INPUT
// 3. ch, err := t.inner.Stream(ctx, inputs, opts...)
// 4. 启动 goroutine 读取 ch，对每个 chunk：
//    - 触发 TOOL_RESULT_RECEIVED
//    - 转发到输出 channel
// 5. 流结束后触发 TOOL_CALL_FINISHED
// 6. 出错时触发 TOOL_CALL_ERROR
```

### 4.3 注册时自动包装

```go
// Register 注册工具，自动包装生命周期回调
func (m *AbilityManager) Register(tool Tool) error {
    wrapped := NewLifecycleTool(tool, m.callbackFramework)
    m.tools[tool.Card().Name] = wrapped
    return nil
}
```

### 4.4 事件分发策略

| Python 事件 | Go 中触发位置 | 说明 |
|-------------|--------------|------|
| TOOL_CALL_STARTED | LifecycleTool.Invoke/Stream 入口 | 工具调用开始 |
| TOOL_CALL_FINISHED | LifecycleTool.Invoke/Stream 成功出口 | 工具调用完成 |
| TOOL_CALL_ERROR | LifecycleTool.Invoke/Stream 异常分支 | 工具调用出错 |
| TOOL_RESULT_RECEIVED | LifecycleTool.Stream 逐 chunk | 流式结果到达 |
| TOOL_PARSE_STARTED | LocalFunction.Invoke/Stream 内部 | Schema 解析开始 |
| TOOL_PARSE_FINISHED | LocalFunction.Invoke/Stream 内部 | Schema 解析完成 |
| TOOL_INVOKE_INPUT | LifecycleTool.Invoke emit_before | Invoke 输入拦截 |
| TOOL_INVOKE_OUTPUT | LifecycleTool.Invoke emit_after | Invoke 输出拦截 |
| TOOL_STREAM_INPUT | LifecycleTool.Stream emit_before | Stream 输入拦截 |
| TOOL_STREAM_OUTPUT | LifecycleTool.Stream emit_after | Stream 输出拦截 |
| TOOL_AUTH | 后续 Auth 模块实现 | 认证回调 |

前 4 个事件由 LifecycleTool 统一触发；PARSE 事件由 LocalFunction 内部自行触发；INVOKE/STREAM INPUT/OUTPUT 由 LifecycleTool 的 emit_before/after 触发；TOOL_AUTH 留给后续 Auth 模块。

## 5. 子类实现概览

| 子类 | Card 类型 | Invoke | Stream | 特殊逻辑 |
|------|----------|--------|--------|---------|
| LocalFunction | ToolCard | ✅ 同步/异步函数调用 | ✅ 生成器函数 | Schema 校验、`*args` 支持、PARSE 事件 |
| MCPTool | McpToolCard | ✅ MCP 客户端调用 | ❌ ErrStreamNotSupported | server_name 路由 |
| RestfulApi | RestfulApiCard | ✅ HTTP 请求 | ❌ ErrStreamNotSupported | 五种参数位置映射、超时/限流 |

各子类的详细设计在后续小节（3.3~3.12）中展开。

## 6. 文件组织（镜像 Python 目录结构）

```
internal/agentcore/foundation/tool/
├── doc.go                    # 包文档
├── base.go                   # Tool 接口 + ToolCard + ToolOption + ToolCallOptions + StreamChunk
├── schema/
│   └── tool_info.go          # ToolInfo / McpToolInfo 扩展定义
├── function/
│   └── function.go           # LocalFunction 实现
├── mcp/
│   ├── base.go               # MCPTool + McpToolCard
│   └── client/               # MCP 客户端实现
├── service_api/
│   ├── restful_api.go        # RestfulApi + RestfulApiCard
│   ├── api_param_mapper.go   # API 参数位置映射
│   └── response_parser.go    # 响应解析器
├── form_handler/
│   └── form_handler_manager.go  # FormHandler 抽象 + DefaultFormHandler + FormHandlerManager
├── auth/
│   ├── auth.go               # ToolAuthConfig / ToolAuthResult
│   └── auth_callback.go      # 认证回调逻辑
├── utils/
│   ├── callable_schema.go    # 从函数签名提取参数 Schema
│   └── type_schema.go        # 从类型提取 Schema
├── lifecycle_tool.go         # LifecycleTool 包装器（回调生命周期）
└── tool.go                   # @tool 装饰器等价（便捷构造函数）
```

## 7. Python 对照

| Python | Go | 说明 |
|--------|-----|------|
| `BaseCard` | `schema.BaseCard` | 已实现于 common/schema |
| `ToolCard(BaseCard)` | `ToolCard` 嵌入 `BaseCard` | 增加 InputParams + Properties |
| `Tool(ABC)` + `_ToolMeta` | `Tool` 接口 + `LifecycleTool` 包装器 | 元类→包装器模式 |
| `Tool.invoke()` | `Tool.Invoke()` | 一次性执行 |
| `Tool.stream()` | `Tool.Stream()` | 流式执行，返回 channel |
| `ToolInfo` | `schema.ToolInfo` | 已有基础定义，此处扩展 |
| `McpToolInfo` | `schema.McpToolInfo` | 已有基础定义，此处扩展 |
| `Input/Output TypeVar` | `map[string]any` | Go 用 map 替代泛型 TypeVar |
| `**kwargs` | `...ToolOption` | 函数式选项替代可变关键字参数 |
| `_ToolMeta.__call__` 自动注入 | `NewLifecycleTool()` + 注册时包装 | 显式包装替代元类魔法 |
