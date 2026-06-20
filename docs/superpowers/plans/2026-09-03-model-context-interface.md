# 5.15 ModelContext 接口实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现上下文引擎核心接口（ModelContext + ContextStats + ContextWindow + ContextEngine + TokenCounter），并回填 single_agent 包中的预留接口为真实类型。

**Architecture:** 新建 `context_engine` 包和 `token` 子包，定义核心接口和数据结构。同时回填 `single_agent` 包中的预留 `ContextEngine`/`Session` 接口，替换为 `context_engine.ContextEngine` 和 `*session.Session` 真实类型。

**Tech Stack:** Go 1.x，依赖已有的 llm/schema（消息类型）、common/schema（ToolInfo）、foundation/tool（Tool 接口）、session（Session 门面类）

---

## 文件结构

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 创建 | `internal/agentcore/context_engine/doc.go` | 包文档 |
| 创建 | `internal/agentcore/context_engine/base.go` | ModelContext 接口 + ContextStats + ContextWindow + ContextEngine 接口 |
| 创建 | `internal/agentcore/context_engine/base_test.go` | ContextWindow 方法测试 |
| 创建 | `internal/agentcore/context_engine/token/doc.go` | Token 子包文档 |
| 创建 | `internal/agentcore/context_engine/token/base.go` | TokenCounter 接口骨架 |
| 修改 | `internal/agentcore/single_agent/resource_manager.go` | 删除预留 ContextEngine/Session 接口，改用真实类型 |
| 修改 | `internal/agentcore/single_agent/resource_manager_test.go` | 更新 mockSession 和测试 |
| 修改 | `internal/agentcore/single_agent/ability_manager.go` | contextEngine 字段和 Session 参数改用真实类型 |
| 修改 | `internal/agentcore/single_agent/ability_manager_test.go` | 更新 SetContextEngine 测试 |

---

### Task 1: 创建 context_engine/token 子包（TokenCounter 接口骨架）

ModelContext.TokenCounter() 返回 TokenCounter，所以先创建此接口。

**Files:**
- Create: `internal/agentcore/context_engine/token/doc.go`
- Create: `internal/agentcore/context_engine/token/base.go`

- [ ] **Step 1: 创建 token/doc.go**

```go
// Package token 提供上下文引擎的 Token 计数能力。
//
// 定义 TokenCounter 抽象接口，供 ModelContext 统计消息和工具定义的 Token 数量。
// 具体实现（如 TiktokenCounter）在后续步骤中提供。
//
// 文件目录：
//
//	token/
//	├── doc.go     # 包文档
//	└── base.go    # TokenCounter 接口定义
//
// 对应 Python 代码：openjiuwen/core/context_engine/token/
package token
```

- [ ] **Step 2: 创建 token/base.go**

```go
package token

import (
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TokenCounter Token 计数器抽象接口。
//
// 提供文本、消息列表和工具定义的 Token 计数能力，
// 供 ContextStats 统计和 ContextWindow 构建时使用。
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

- [ ] **Step 3: 验证编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/context_engine/token/...`
Expected: 编译成功

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/context_engine/token/doc.go internal/agentcore/context_engine/token/base.go
git commit -m "feat(context_engine): 添加 TokenCounter 接口骨架 (5.15/5.20)"
```

---

### Task 2: 创建 context_engine/base.go（核心类型定义）

**Files:**
- Create: `internal/agentcore/context_engine/base.go`

- [ ] **Step 1: 创建 base.go**

```go
package context_engine

import (
	"context"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ContextStats 上下文统计快照，记录消息数量、Token 数量和对话轮次。
//
// 对应 Python: openjiuwen/core/context_engine/base.py (ContextStats)
type ContextStats struct {
	// TotalMessages 消息总数
	TotalMessages int `json:"total_messages"`
	// TotalTokens Token 总数
	TotalTokens int `json:"total_tokens"`
	// TotalDialogues 对话轮次数
	TotalDialogues int `json:"total_dialogues"`
	// SystemMessages 系统消息数
	SystemMessages int `json:"system_messages"`
	// UserMessages 用户消息数
	UserMessages int `json:"user_messages"`
	// AssistantMessages 助手消息数
	AssistantMessages int `json:"assistant_messages"`
	// ToolMessages 工具消息数
	ToolMessages int `json:"tool_messages"`
	// Tools 注入工具数
	Tools int `json:"tools"`
	// SystemMessageTokens 系统消息 Token 数
	SystemMessageTokens int `json:"system_message_tokens"`
	// UserMessageTokens 用户消息 Token 数
	UserMessageTokens int `json:"user_message_tokens"`
	// AssistantMessageTokens 助手消息 Token 数
	AssistantMessageTokens int `json:"assistant_message_tokens"`
	// ToolMessageTokens 工具消息 Token 数
	ToolMessageTokens int `json:"tool_message_tokens"`
	// ToolTokens 工具定义 Token 数
	ToolTokens int `json:"tool_tokens"`
}

// ContextWindow LLM 推理上下文窗口快照，包含系统消息、上下文消息和工具定义。
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

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// GetMessages 合并系统消息和上下文消息，返回完整消息列表。
//
// 对应 Python: ContextWindow.get_messages()
func (w *ContextWindow) GetMessages() []*llm_schema.BaseMessage {
	result := make([]*llm_schema.BaseMessage, 0, len(w.SystemMessages)+len(w.ContextMessages))
	result = append(result, w.SystemMessages...)
	result = append(result, w.ContextMessages...)
	return result
}

// GetTools 返回工具列表。
//
// 对应 Python: ContextWindow.get_tools()
func (w *ContextWindow) GetTools() []*schema.ToolInfo {
	return w.Tools
}

// ──────────────────────────── 非导出函数 ────────────────────────────

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
	// size 限制返回数量，nil 表示不限制
	// withHistory 控制是否包含历史消息
	GetMessages(size *int, withHistory bool) []*llm_schema.BaseMessage
	// SetMessages 替换消息列表
	// withHistory 控制是否替换历史消息
	SetMessages(messages []*llm_schema.BaseMessage, withHistory bool)
	// PopMessages 从尾部弹出消息
	// withHistory 控制是否从历史消息中弹出
	PopMessages(size int, withHistory bool) []*llm_schema.BaseMessage
	// ClearMessages 清空消息
	// withHistory 控制是否清空历史消息
	ClearMessages(ctx context.Context, withHistory bool) error
	// AddMessages 添加消息
	// message 接受 *BaseMessage（单条）或 []*BaseMessage（列表）
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

// ContextEngine 上下文引擎门面接口。
//
// 管理上下文池、处理器注册、会话状态持久化。
// 属于 Agent 级别组件（不在 Session 中），通过 agent.contextEngine 访问。
//
// 对应 Python: openjiuwen/core/context_engine/context_engine.py (ContextEngine)
type ContextEngine interface {
	// CreateContext 创建上下文
	CreateContext(ctx context.Context, contextID string, sess *session.Session) (ModelContext, error)
	// GetContext 获取上下文
	GetContext(contextID string, sessionID string) ModelContext
	// CompressContext 压缩上下文
	CompressContext(ctx context.Context, contextID string, sess *session.Session) (string, error)
	// ClearContext 清空上下文
	ClearContext(ctx context.Context, contextID string, sessionID string) error
	// SaveContexts 保存上下文状态
	SaveContexts(ctx context.Context, sess *session.Session, contextIDs []string) error
	// RegisterProcessor 注册处理器
	RegisterProcessor(processorType string, processor any)
}
```

注意：按照项目编码规范，接口（type interface）归类到结构体区块，排在结构体之后。但 Go 中接口通常定义在文件顶部以便阅读。此处将接口放在非导出函数区块后面（文件末尾），因为它们是核心抽象定义，且规范要求接口归类到结构体区块。实际应放在结构体区块，紧跟在 ContextWindow 之后。让我修正：

- [ ] **Step 2: 修正 base.go — 将接口移到结构体区块**

按编码规范，接口归类到结构体区块，排在结构体之前。修正后的完整文件：

```go
package context_engine

import (
	"context"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

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
	// size 限制返回数量，nil 表示不限制
	// withHistory 控制是否包含历史消息
	GetMessages(size *int, withHistory bool) []*llm_schema.BaseMessage
	// SetMessages 替换消息列表
	// withHistory 控制是否替换历史消息
	SetMessages(messages []*llm_schema.BaseMessage, withHistory bool)
	// PopMessages 从尾部弹出消息
	// withHistory 控制是否从历史消息中弹出
	PopMessages(size int, withHistory bool) []*llm_schema.BaseMessage
	// ClearMessages 清空消息
	// withHistory 控制是否清空历史消息
	ClearMessages(ctx context.Context, withHistory bool) error
	// AddMessages 添加消息
	// message 接受 *BaseMessage（单条）或 []*BaseMessage（列表）
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

// ContextEngine 上下文引擎门面接口。
//
// 管理上下文池、处理器注册、会话状态持久化。
// 属于 Agent 级别组件（不在 Session 中），通过 agent.contextEngine 访问。
//
// 对应 Python: openjiuwen/core/context_engine/context_engine.py (ContextEngine)
type ContextEngine interface {
	// CreateContext 创建上下文
	CreateContext(ctx context.Context, contextID string, sess *session.Session) (ModelContext, error)
	// GetContext 获取上下文
	GetContext(contextID string, sessionID string) ModelContext
	// CompressContext 压缩上下文
	CompressContext(ctx context.Context, contextID string, sess *session.Session) (string, error)
	// ClearContext 清空上下文
	ClearContext(ctx context.Context, contextID string, sessionID string) error
	// SaveContexts 保存上下文状态
	SaveContexts(ctx context.Context, sess *session.Session, contextIDs []string) error
	// RegisterProcessor 注册处理器
	RegisterProcessor(processorType string, processor any)
}

// ContextStats 上下文统计快照，记录消息数量、Token 数量和对话轮次。
//
// 对应 Python: openjiuwen/core/context_engine/base.py (ContextStats)
type ContextStats struct {
	// TotalMessages 消息总数
	TotalMessages int `json:"total_messages"`
	// TotalTokens Token 总数
	TotalTokens int `json:"total_tokens"`
	// TotalDialogues 对话轮次数
	TotalDialogues int `json:"total_dialogues"`
	// SystemMessages 系统消息数
	SystemMessages int `json:"system_messages"`
	// UserMessages 用户消息数
	UserMessages int `json:"user_messages"`
	// AssistantMessages 助手消息数
	AssistantMessages int `json:"assistant_messages"`
	// ToolMessages 工具消息数
	ToolMessages int `json:"tool_messages"`
	// Tools 注入工具数
	Tools int `json:"tools"`
	// SystemMessageTokens 系统消息 Token 数
	SystemMessageTokens int `json:"system_message_tokens"`
	// UserMessageTokens 用户消息 Token 数
	UserMessageTokens int `json:"user_message_tokens"`
	// AssistantMessageTokens 助手消息 Token 数
	AssistantMessageTokens int `json:"assistant_message_tokens"`
	// ToolMessageTokens 工具消息 Token 数
	ToolMessageTokens int `json:"tool_message_tokens"`
	// ToolTokens 工具定义 Token 数
	ToolTokens int `json:"tool_tokens"`
}

// ContextWindow LLM 推理上下文窗口快照，包含系统消息、上下文消息和工具定义。
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

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// GetMessages 合并系统消息和上下文消息，返回完整消息列表。
//
// 对应 Python: ContextWindow.get_messages()
func (w *ContextWindow) GetMessages() []*llm_schema.BaseMessage {
	result := make([]*llm_schema.BaseMessage, 0, len(w.SystemMessages)+len(w.ContextMessages))
	result = append(result, w.SystemMessages...)
	result = append(result, w.ContextMessages...)
	return result
}

// GetTools 返回工具列表。
//
// 对应 Python: ContextWindow.get_tools()
func (w *ContextWindow) GetTools() []*schema.ToolInfo {
	return w.Tools
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 3: 验证编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/context_engine/...`
Expected: 编译成功

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/context_engine/base.go
git commit -m "feat(context_engine): 添加 ModelContext/ContextEngine 接口和 ContextStats/ContextWindow 结构体 (5.15+5.16)"
```

---

### Task 3: 创建 context_engine/doc.go

**Files:**
- Create: `internal/agentcore/context_engine/doc.go`

- [ ] **Step 1: 创建 doc.go**

```go
// Package context_engine 提供上下文引擎的核心抽象和数据结构。
//
// 上下文引擎负责管理 Agent 会话中的对话消息生命周期、
// 构建 LLM 推理所需的上下文窗口、以及消息压缩和卸载等处理。
// 它是 Session 和 LLM 之间的桥梁：Session 管理会话状态，
// ModelContext 管理 LLM 看到的"上下文视图"。
//
// 文件目录：
//
//	context_engine/
//	├── doc.go           # 包文档
//	├── base.go          # ModelContext 接口 + ContextStats + ContextWindow + ContextEngine 接口
//	└── token/
//	    ├── doc.go       # Token 子包文档
//	    └── base.go      # TokenCounter 接口定义
//
// 对应 Python 代码：openjiuwen/core/context_engine/
package context_engine
```

- [ ] **Step 2: Commit**

```bash
git add internal/agentcore/context_engine/doc.go
git commit -m "docs(context_engine): 添加包文档 (5.15)"
```

---

### Task 4: 编写 base.go 的单元测试

**Files:**
- Create: `internal/agentcore/context_engine/base_test.go`

- [ ] **Step 1: 创建 base_test.go**

```go
package context_engine

import (
	"testing"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestContextWindow_GetMessages_空窗口 测试空窗口的 GetMessages
func TestContextWindow_GetMessages_空窗口(t *testing.T) {
	w := &ContextWindow{}
	msgs := w.GetMessages()
	if len(msgs) != 0 {
		t.Errorf("空窗口应返回 0 条消息，实际 %d", len(msgs))
	}
}

// TestContextWindow_GetMessages_合并系统消息和上下文消息 测试消息合并
func TestContextWindow_GetMessages_合并系统消息和上下文消息(t *testing.T) {
	sysMsg := llm_schema.NewBaseMessage(llm_schema.RoleTypeSystem, "系统提示")
	userMsg := llm_schema.NewBaseMessage(llm_schema.RoleTypeUser, "用户输入")

	w := &ContextWindow{
		SystemMessages:  []*llm_schema.BaseMessage{sysMsg},
		ContextMessages: []*llm_schema.BaseMessage{userMsg},
	}

	msgs := w.GetMessages()
	if len(msgs) != 2 {
		t.Fatalf("应返回 2 条消息，实际 %d", len(msgs))
	}
	if msgs[0].Role != llm_schema.RoleTypeSystem {
		t.Errorf("第 1 条消息角色应为 system，实际 %v", msgs[0].Role)
	}
	if msgs[1].Role != llm_schema.RoleTypeUser {
		t.Errorf("第 2 条消息角色应为 user，实际 %v", msgs[1].Role)
	}
}

// TestContextWindow_GetMessages_仅系统消息 测试只有系统消息的情况
func TestContextWindow_GetMessages_仅系统消息(t *testing.T) {
	sysMsg := llm_schema.NewBaseMessage(llm_schema.RoleTypeSystem, "系统提示")

	w := &ContextWindow{
		SystemMessages: []*llm_schema.BaseMessage{sysMsg},
	}

	msgs := w.GetMessages()
	if len(msgs) != 1 {
		t.Fatalf("应返回 1 条消息，实际 %d", len(msgs))
	}
	if msgs[0].Role != llm_schema.RoleTypeSystem {
		t.Errorf("消息角色应为 system，实际 %v", msgs[0].Role)
	}
}

// TestContextWindow_GetTools_空工具 测试空工具列表
func TestContextWindow_GetTools_空工具(t *testing.T) {
	w := &ContextWindow{}
	tools := w.GetTools()
	if tools != nil {
		t.Errorf("空窗口应返回 nil 工具列表，实际 %v", tools)
	}
}

// TestContextWindow_GetTools_有工具 测试工具列表返回
func TestContextWindow_GetTools_有工具(t *testing.T) {
	toolInfo := &schema.ToolInfo{
		Type:        "function",
		Name:        "test_tool",
		Description: "测试工具",
	}

	w := &ContextWindow{
		Tools: []*schema.ToolInfo{toolInfo},
	}

	tools := w.GetTools()
	if len(tools) != 1 {
		t.Fatalf("应返回 1 个工具，实际 %d", len(tools))
	}
	if tools[0].Name != "test_tool" {
		t.Errorf("工具名称应为 test_tool，实际 %s", tools[0].Name)
	}
}

// TestContextStats_零值 测试 ContextStats 零值
func TestContextStats_零值(t *testing.T) {
	var stats ContextStats
	if stats.TotalMessages != 0 {
		t.Errorf("零值 TotalMessages 应为 0，实际 %d", stats.TotalMessages)
	}
	if stats.TotalTokens != 0 {
		t.Errorf("零值 TotalTokens 应为 0，实际 %d", stats.TotalTokens)
	}
	if stats.TotalDialogues != 0 {
		t.Errorf("零值 TotalDialogues 应为 0，实际 %d", stats.TotalDialogues)
	}
}

// TestContextWindow_Statistic为Nil 测试 Statistic 为 nil 时 GetMessages 不受影响
func TestContextWindow_Statistic为Nil(t *testing.T) {
	w := &ContextWindow{
		SystemMessages:  []*llm_schema.BaseMessage{llm_schema.NewBaseMessage(llm_schema.RoleTypeSystem, "hi")},
		ContextMessages: []*llm_schema.BaseMessage{llm_schema.NewBaseMessage(llm_schema.RoleTypeUser, "hello")},
	}

	if w.Statistic != nil {
		t.Error("Statistic 应为 nil")
	}
	msgs := w.GetMessages()
	if len(msgs) != 2 {
		t.Errorf("即使 Statistic 为 nil，GetMessages 也应返回 2 条消息，实际 %d", len(msgs))
	}
}
```

- [ ] **Step 2: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_engine/... -v`
Expected: 全部 PASS

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/context_engine/base_test.go
git commit -m "test(context_engine): 添加 ContextWindow 和 ContextStats 单元测试 (5.15)"
```

---

### Task 5: 回填 resource_manager.go — 删除预留接口，改用真实类型

**Files:**
- Modify: `internal/agentcore/single_agent/resource_manager.go`

- [ ] **Step 1: 修改 resource_manager.go**

需要做以下变更：

1. 删除预留的 `ContextEngine` 接口（第 56-60 行）
2. 删除预留的 `Session` 接口（第 62-72 行）
3. `ResourceOptions.Session` 字段类型从 `Session` 改为 `*session.Session`
4. `WithResourceSession` 参数类型从 `Session` 改为 `*session.Session`
5. 添加 import：`"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"` 和 `"github.com/uapclaw/uapclaw-go/internal/agentcore/session"`
6. `ContextEngine` 引用改为 `context_engine.ContextEngine`（如果 resource_manager.go 中有引用的话 — 当前没有，AbilityManager 中有）

具体编辑操作：

a) 修改 import 块，添加两个新导入：
```go
import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)
```

b) 删除以下代码块（第 56-60 行）：
```go
// ContextEngine 上下文引擎接口（预留，领域五回填）。
type ContextEngine interface {
	// CreateContext 创建上下文
	CreateContext(ctx context.Context, contextID string, session Session) (any, error)
}
```

c) 删除以下代码块（第 62-72 行）：
```go
// Session 会话接口（预留，领域五回填）。
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

d) 修改 `ResourceOptions` 结构体中的 `Session` 字段（第 78-79 行）：
```go
// ResourceOptions 实例获取选项。
type ResourceOptions struct {
	// Tag 资源标签
	Tag string
	// Session 会话实例
	Session *session.Session
}
```

e) 修改 `WithResourceSession` 函数（第 114-117 行）：
```go
// WithResourceSession 设置会话实例。
func WithResourceSession(sess *session.Session) ResourceOption {
	return func(o *ResourceOptions) { o.Session = sess }
}
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/single_agent/...`
Expected: 编译成功（注意：ability_manager.go 中仍有 `Session` 和 `ContextEngine` 引用，需在 Task 6 一起修改才能编译通过。此步骤应与 Task 6 合并验证）

- [ ] **Step 3: Commit（与 Task 6 合并提交）**

暂不单独提交，等 Task 6 完成后一起验证编译和提交。

---

### Task 6: 回填 ability_manager.go — 改用真实类型

**Files:**
- Modify: `internal/agentcore/single_agent/ability_manager.go`

- [ ] **Step 1: 修改 ability_manager.go**

需要做以下变更：

1. 添加 import：`"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"` 和 `"github.com/uapclaw/uapclaw-go/internal/agentcore/session"`
2. `contextEngine` 字段类型从 `ContextEngine` 改为 `context_engine.ContextEngine`
3. `SetContextEngine` 参数类型从 `ContextEngine` 改为 `context_engine.ContextEngine`
4. 所有 `session Session` 参数改为 `session *session.Session`
5. 清除 `⤵️ 预留，领域五回填` 标记

具体编辑操作：

a) 修改 import 块：
```go
import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)
```

b) 修改 `contextEngine` 字段（第 39-40 行）：
```go
	// contextEngine 上下文引擎
	contextEngine context_engine.ContextEngine
```

c) 修改 `SetContextEngine` 方法（第 71-73 行）：
```go
// SetContextEngine 设置上下文引擎。
func (am *AbilityManager) SetContextEngine(ce context_engine.ContextEngine) {
	am.contextEngine = ce
}
```

d) 修改 `Execute` 方法签名（第 357 行）中 `session Session` → `session *session.Session`

e) 修改 `railedExecuteSingleToolCall` 方法签名（第 391 行）中 `session Session` → `session *session.Session`

f) 修改 `executeSingleToolCall` 方法签名（第 410 行）中 `session Session` → `session *session.Session`

g) 修改 `executeTool` 方法签名（第 461 行）中 `session Session` → `session *session.Session`

h) 修改 `executeWorkflow` 方法签名（第 518 行）中 `session Session` → `session *session.Session`

i) 修改 `executeAgent` 方法签名（第 566 行）中 `session Session` → `session *session.Session`

j) 修改 `executeFallbackTool` 方法签名（第 614 行）中 `session Session` → `session *session.Session`

注意：由于 `session` 现在是包名，所有方法参数中的变量名 `session` 会与包名冲突。需将参数变量名改为 `sess`。

完整的参数替换：`session Session` → `sess *session.Session`

- [ ] **Step 2: 验证编译（Task 5 + Task 6 合并）**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/single_agent/...`
Expected: 编译成功

- [ ] **Step 3: 运行 single_agent 包测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/single_agent/... -v -count=1`
Expected: 全部 PASS

- [ ] **Step 4: Commit（Task 5 + Task 6 合并）**

```bash
git add internal/agentcore/single_agent/resource_manager.go internal/agentcore/single_agent/ability_manager.go
git commit -m "refactor(single_agent): 回填预留接口，ContextEngine 和 Session 改用真实类型 (5.15)"
```

---

### Task 7: 更新 resource_manager_test.go

**Files:**
- Modify: `internal/agentcore/single_agent/resource_manager_test.go`

- [ ] **Step 1: 修改测试文件**

删除 `mockSession`（不再需要，`*session.Session` 是具体类型），修改 `TestWithResourceSession` 测试用例：

```go
package single_agent

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNoopResourceManager_获取工具(t *testing.T) {
	mgr := &NoopResourceManager{}
	_, err := mgr.GetTool("test_tool")
	if err == nil {
		t.Fatal("应返回错误")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误类型应为 *BaseError，实际 %T", err)
	}
	if baseErr.Code() != exception.StatusAbilityNotFound.Code() {
		t.Errorf("Code = %d, want %d", baseErr.Code(), exception.StatusAbilityNotFound.Code())
	}
}

func TestNoopResourceManager_获取工作流(t *testing.T) {
	mgr := &NoopResourceManager{}
	_, err := mgr.GetWorkflow("test_wf")
	if err == nil {
		t.Fatal("应返回错误")
	}
}

func TestNoopResourceManager_获取Agent(t *testing.T) {
	mgr := &NoopResourceManager{}
	_, err := mgr.GetAgent("test_agent")
	if err == nil {
		t.Fatal("应返回错误")
	}
}

func TestNoopResourceManager_获取MCP工具信息(t *testing.T) {
	mgr := &NoopResourceManager{}
	infos, err := mgr.GetMcpToolInfos("server1")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if infos != nil {
		t.Errorf("应返回 nil，实际 %v", infos)
	}
}

func TestNewResourceOptions_资源选项(t *testing.T) {
	opts := NewResourceOptions(
		WithResourceTag("my_tag"),
	)
	if opts.Tag != "my_tag" {
		t.Errorf("Tag = %q, want my_tag", opts.Tag)
	}
}

// TestWithResourceSession 验证 WithResourceSession 设置 Session。
func TestWithResourceSession(t *testing.T) {
	sess := session.NewSession()
	opts := NewResourceOptions(WithResourceSession(sess))
	if opts.Session != sess {
		t.Error("WithResourceSession 未正确设置 Session")
	}
}
```

- [ ] **Step 2: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/single_agent/... -v -run TestWithResourceSession -count=1`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/single_agent/resource_manager_test.go
git commit -m "test(single_agent): 更新 WithResourceSession 测试改用真实 Session 类型 (5.15)"
```

---

### Task 8: 更新 ability_manager_test.go

**Files:**
- Modify: `internal/agentcore/single_agent/ability_manager_test.go`

- [ ] **Step 1: 修改 SetContextEngine 测试**

当前测试代码（第 481-484 行）：
```go
func TestAbilityManager_SetContextEngine(t *testing.T) {
	am := NewAbilityManager(nil)
	am.SetContextEngine(nil) // 不应 panic
}
```

改为：
```go
func TestAbilityManager_SetContextEngine(t *testing.T) {
	am := NewAbilityManager(nil)
	am.SetContextEngine(nil) // 不应 panic，nil 满足 context_engine.ContextEngine 接口
}
```

逻辑不变，只需确认编译通过即可。

- [ ] **Step 2: 验证编译和测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/single_agent/... -v -count=1`
Expected: 全部 PASS

- [ ] **Step 3: Commit（如有改动）**

```bash
git add internal/agentcore/single_agent/ability_manager_test.go
git commit -m "test(single_agent): 更新 SetContextEngine 测试注释 (5.15)"
```

---

### Task 9: 更新 IMPLEMENTATION_PLAN.md 状态标记

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 5.15 和 5.16 状态**

5.15: `☐` → `✅`
5.16: `☐` → `✅`（ContextStats + ContextWindow 已在 base.go 中实现）

同时清除 resource_manager.go 和 ability_manager.go 中的 `⤵️ 预留，领域五回填` 标记（代码中已清除，计划文件中对应行也应标注完成）。

- [ ] **Step 2: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 5.15+5.16 实现状态为已完成"
```

---

### Task 10: 全量编译验证

- [ ] **Step 1: 检查残留编译进程**

Run: `pgrep -f 'go (build|test)'`
Expected: 无残留进程（有则 kill）

- [ ] **Step 2: 全量编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译成功

- [ ] **Step 3: 运行受影响包的测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_engine/... ./internal/agentcore/single_agent/... -v -count=1`
Expected: 全部 PASS

---

## 自审清单

**1. Spec 覆盖：**
- ✅ ModelContext 接口（11 方法）→ Task 2
- ✅ ContextStats 结构体 → Task 2
- ✅ ContextWindow 结构体 + GetMessages/GetTools → Task 2, Task 4
- ✅ ContextEngine 接口 → Task 2
- ✅ TokenCounter 接口骨架 → Task 1
- ✅ doc.go → Task 3
- ✅ resource_manager.go 回填 → Task 5
- ✅ ability_manager.go 回填 → Task 6
- ✅ 测试更新 → Task 4, Task 7, Task 8
- ✅ IMPLEMENTATION_PLAN.md 更新 → Task 9

**2. Placeholder 扫描：** 无 TBD/TODO/未完成步骤

**3. 类型一致性：**
- `*session.Session` 在所有文件中一致使用
- `context_engine.ContextEngine` 在 ability_manager.go 中一致使用
- `token.TokenCounter` 在 ModelContext 接口中返回类型正确
- Task 6 中参数变量名统一为 `sess`（避免与 `session` 包名冲突）
