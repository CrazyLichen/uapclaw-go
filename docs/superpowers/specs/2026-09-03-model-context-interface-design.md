# 5.15 ModelContext 接口 + 回填设计方案

## 概述

实现领域五上下文引擎的第一步：定义 ModelContext 核心接口、ContextStats/ContextWindow 数据结构、ContextEngine 门面接口，以及 TokenCounter 接口骨架。同时回填 single_agent 包中的预留接口，替换为真实类型。

对应 Python 源码：`openjiuwen/core/context_engine/base.py`、`openjiuwen/core/context_engine/context_engine.py`、`openjiuwen/core/context_engine/token/base.py`

## 决策记录

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 接口拆分 | 单一大接口 | 忠实映射 Python ModelContext 抽象基类，便于对照 |
| 文件组织 | 5.15+5.16 合并在 base.go | Python 中三个类同文件，Go 也同文件 |
| ContextEngine 接口 | 也放 base.go | 与 Python base.py 对齐 |
| 回填策略 | 顺带回填所有可回填的 ⤵️ 标记 | 一步到位，消除临时预留 |
| Session 参数类型 | 直接用 `*session.Session` | Go 端已完整实现 Session 门面类，与 Python 对齐 |
| 异步方法 | async 加 ctx，同步不加 | 忠实映射 Python |
| 接口完整性 | 包含全部 11 个方法 | 与 Python 1:1 |
| TokenCounter | 同步创建接口骨架 | 5.15 依赖 TokenCounter 返回类型 |

## 新建文件

```
internal/agentcore/context_engine/
├── doc.go                    # 包文档
├── base.go                   # ModelContext + ContextStats + ContextWindow + ContextEngine
└── token/
    ├── doc.go                # 子包文档
    └── base.go               # TokenCounter 接口骨架
```

## 核心类型定义

### ModelContext 接口

```go
// ModelContext 上下文引擎的核心抽象接口，管理对话消息和上下文窗口。
//
// 职责：
//   - 消息生命周期管理（增删改查）
//   - 上下文窗口构建（供 LLM 推理使用）
//   - 统计与监控（消息数/Token数/对话轮次）
//
// 对应 Python: openjiuwen/core/context_engine/base.py (ModelContext)
type ModelContext interface {
    // Len 返回上下文消息数量
    Len() int
    // GetMessages 获取消息列表
    // size: 限制返回数量，nil 表示不限制
    // withHistory: 是否包含历史消息
    GetMessages(size *int, withHistory bool) []*llm_schema.BaseMessage
    // SetMessages 替换消息列表
    SetMessages(messages []*llm_schema.BaseMessage, withHistory bool)
    // PopMessages 从尾部弹出消息
    PopMessages(size int, withHistory bool) []*llm_schema.BaseMessage
    // ClearMessages 清空消息
    ClearMessages(ctx context.Context, withHistory bool) error
    // AddMessages 添加消息（支持单条 *BaseMessage 或列表 []*BaseMessage）
    AddMessages(ctx context.Context, message any) ([]*llm_schema.BaseMessage, error)
    // GetContextWindow 构建上下文窗口供模型推理使用
    GetContextWindow(ctx context.Context, systemMessages []*llm_schema.BaseMessage,
        tools []*schema.ToolInfo, windowSize *int, dialogueRound *int) (*ContextWindow, error)
    // Statistic 计算上下文统计信息
    Statistic() *ContextStats
    // SessionID 返回会话 ID
    SessionID() string
    // ContextID 返回上下文 ID
    ContextID() string
    // TokenCounter 返回 Token 计数器
    TokenCounter() token.TokenCounter
    // ReloaderTool 返回重载卸载消息的工具
    ReloaderTool() tool.Tool
}
```

### Python ↔ Go 方法映射

| Python | Go | 差异说明 |
|--------|-----|---------|
| `__len__()` | `Len() int` | Go 用方法替代魔术方法 |
| `get_messages(size, with_history)` | `GetMessages(size *int, withHistory bool)` | size 用指针表示可选 |
| `set_messages(messages, with_history)` | `SetMessages(messages, withHistory bool)` | |
| `pop_messages(size, with_history)` | `PopMessages(size int, withHistory bool)` | |
| `clear_messages(with_history)` async | `ClearMessages(ctx, withHistory) error` | 加 ctx |
| `add_messages(message)` async | `AddMessages(ctx, message any)` | 加 ctx，any 支持单条/列表 |
| `get_context_window(...)` async | `GetContextWindow(ctx, ...) error` | 加 ctx |
| `statistic()` | `Statistic() *ContextStats` | |
| `session_id()` | `SessionID() string` | |
| `context_id()` | `ContextID() string` | |
| `token_counter()` | `TokenCounter() TokenCounter` | |
| `reloader_tool()` | `ReloaderTool() tool.Tool` | |

### ContextStats 结构体

```go
// ContextStats 上下文统计快照。
//
// 对应 Python: openjiuwen/core/context_engine/base.py (ContextStats)
type ContextStats struct {
    TotalMessages          int `json:"total_messages"`
    TotalTokens            int `json:"total_tokens"`
    TotalDialogues         int `json:"total_dialogues"`
    SystemMessages         int `json:"system_messages"`
    UserMessages           int `json:"user_messages"`
    AssistantMessages      int `json:"assistant_messages"`
    ToolMessages           int `json:"tool_messages"`
    Tools                  int `json:"tools"`
    SystemMessageTokens    int `json:"system_message_tokens"`
    UserMessageTokens      int `json:"user_message_tokens"`
    AssistantMessageTokens int `json:"assistant_message_tokens"`
    ToolMessageTokens      int `json:"tool_message_tokens"`
    ToolTokens             int `json:"tool_tokens"`
}
```

### ContextWindow 结构体

```go
// ContextWindow LLM 推理上下文窗口快照。
//
// 对应 Python: openjiuwen/core/context_engine/base.py (ContextWindow)
type ContextWindow struct {
    // SystemMessages 系统消息
    SystemMessages []*llm_schema.BaseMessage `json:"system_messages"`
    // ContextMessages 上下文消息
    ContextMessages []*llm_schema.BaseMessage `json:"context_messages"`
    // Tools 工具定义
    Tools []*schema.ToolInfo `json:"tools"`
    // Statistic 统计信息
    Statistic *ContextStats `json:"statistic"`
}

// GetMessages 合并系统消息和上下文消息，返回完整消息列表
func (w *ContextWindow) GetMessages() []*llm_schema.BaseMessage

// GetTools 返回工具列表
func (w *ContextWindow) GetTools() []*schema.ToolInfo
```

### ContextEngine 接口

```go
// ContextEngine 上下文引擎门面接口。
//
// 管理上下文池、处理器注册、会话状态持久化。
// 属于 Agent 级别组件（不在 Session 中），通过 agent.contextEngine 访问。
//
// 对应 Python: openjiuwen/core/context_engine/context_engine.py (ContextEngine)
type ContextEngine interface {
    // CreateContext 创建上下文
    CreateContext(ctx context.Context, contextID string, session *session.Session) (ModelContext, error)
    // GetContext 获取上下文
    GetContext(contextID string, sessionID string) ModelContext
    // CompressContext 压缩上下文
    CompressContext(ctx context.Context, contextID string, session *session.Session) (string, error)
    // ClearContext 清空上下文
    ClearContext(ctx context.Context, contextID string, sessionID string) error
    // SaveContexts 保存上下文状态
    SaveContexts(ctx context.Context, session *session.Session, contextIDs []string) error
    // RegisterProcessor 注册处理器
    RegisterProcessor(processorType string, processor any)
}
```

### TokenCounter 接口

```go
// TokenCounter Token 计数器抽象接口。
//
// 对应 Python: openjiuwen/core/context_engine/token/base.py (TokenCounter)
type TokenCounter interface {
    // Count 计算文本的 Token 数量
    Count(text string, model string) int
    // CountMessages 计算消息列表的 Token 数量
    CountMessages(messages []*llm_schema.BaseMessage, model string) int
    // CountTools 计算工具定义的 Token 数量
    CountTools(tools []*schema.ToolInfo, model string) int
}
```

## 回填变更

### resource_manager.go

1. **删除**预留的 `ContextEngine` 接口（第 56-60 行）
2. **删除**预留的 `Session` 接口（第 62-72 行）
3. **修改** `ResourceOptions.Session` 字段类型：从预留 `Session` 改为 `*session.Session`
4. **修改** `WithResourceSession` 参数类型：从预留 `Session` 改为 `*session.Session`
5. **新增** import：`context_engine` 包和 `session` 包

### ability_manager.go

1. **修改** `contextEngine` 字段类型：从预留 `ContextEngine` 改为 `context_engine.ContextEngine`
2. **修改** `SetContextEngine` 参数类型：从预留 `ContextEngine` 改为 `context_engine.ContextEngine`
3. **修改**所有 `Session` 参数类型（Execute/railedExecuteSingleToolCall/executeSingleToolCall/executeTool/executeWorkflow/executeAgent/executeFallbackTool）：从预留 `Session` 改为 `*session.Session`
4. **新增** import：`context_engine` 包和 `session` 包

### ⤵️ 标记清除

| 文件 | 原标记 | 处理 |
|------|--------|------|
| resource_manager.go | `⤵️ 预留，领域五回填` | 删除预留接口，改用真实类型，标记清除 |
| ability_manager.go | `⤵️ 预留，领域五回填` | contextEngine 字段改用真实类型，标记清除 |

## 依赖关系

```
context_engine/base.go
  ├── import llm_schema (消息类型)
  ├── import schema (ToolInfo)
  ├── import tool (Tool 接口，reloader_tool 返回值)
  ├── import session (Session 门面类，ContextEngine 参数)
  └── import token (TokenCounter 接口)

context_engine/token/base.go
  ├── import llm_schema (消息类型)
  └── import schema (ToolInfo)
```

## 与后续步骤的关系

| 步骤 | 与 5.15 的关系 |
|------|---------------|
| 5.16 | 同文件实现（已合并） |
| 5.17 | ContextEngineConfig 将在 config.go 中定义，ContextEngine.CreateContext 接收配置 |
| 5.18 | Offload 消息类型，ModelContext.AddMessages 的消息可能包含 Offload 类型 |
| 5.19 | ContextEvent，处理器链的事件模型 |
| 5.20 | TokenCounter 实现（接口骨架在 5.15 创建） |
| 5.21-5.29 | ContextProcessor 插件链，ModelContext 触发处理器 |
| 5.30 | ContextEngine 具体实现 |
| 5.31 | SessionModelContext 具体实现 ModelContext 接口 |

## 测试策略

1. **base.go 测试**：ContextWindow.GetMessages / GetTools 方法测试
2. **接口满足性测试**：编译时检查（确保接口定义完整、import 正确）
3. **回填测试**：确保 single_agent 包编译通过、所有 Session 引用替换正确
4. **无需 mock**：本步骤仅定义接口和数据结构，无外部依赖需要 mock
