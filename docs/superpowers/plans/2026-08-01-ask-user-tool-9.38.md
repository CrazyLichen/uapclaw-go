# AskUserTool 空壳工具实现计划 (9.38)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增 `harness/tools/ask_user/` 子包（AskUserTool 空壳 + NewAskUserTool 工厂），修正提示词换行差异，重构 AskUserRail.Init 从子包创建工具。

**Architecture:** AskUserTool 是空壳工具（invoke/stream 返回空 map{}），真正逻辑在 AskUserRail 中通过中断机制完成。将 inline 创建逻辑提取到独立子包，对齐 Python `openjiuwen/harness/tools/ask_user.py` 的文件组织。

**Tech Stack:** Go, tool.MapFunction, hprompts.BuildToolCard, logger.ComponentAgentCore

---

## 文件结构

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 创建 | `internal/agentcore/harness/tools/ask_user/doc.go` | 包文档（目录结构 + Python 对应路径） |
| 创建 | `internal/agentcore/harness/tools/ask_user/ask_user.go` | AskUserTool struct + NewAskUserTool 工厂函数 |
| 创建 | `internal/agentcore/harness/tools/ask_user/ask_user_test.go` | 5 个单元测试 |
| 修改 | `internal/agentcore/harness/rails/interrupt/ask_user_rail.go` | Init 改为调用 askuser.NewAskUserTool，删除 inline 创建 |
| 修改 | `internal/agentcore/harness/prompts/tools/ask_user.go` | 修正 cn/en 描述换行差异 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 9.38-49: AskUser → ✅AskUser |

---

### Task 1: 创建 ask_user 子包 — doc.go

**Files:**
- Create: `internal/agentcore/harness/tools/ask_user/doc.go`

- [ ] **Step 1: 创建目录**

```bash
mkdir -p internal/agentcore/harness/tools/ask_user
```

- [ ] **Step 2: 写 doc.go**

```go
// Package ask_user 提供向用户提问的空壳工具（AskUserTool）。
//
// AskUserTool 的 invoke/stream 方法返回空 map{}，
// 真正的用户交互逻辑在 AskUserRail（harness/rails/interrupt）中通过中断机制完成。
// 此包仅负责工具注册（ToolCard + 空壳 MapFunction），供 AskUserRail.Init 调用创建。
//
// 文件目录：
//
//	ask_user/
//	├── doc.go            # 包文档
//	├── ask_user.go       # AskUserTool 空壳工具 + NewAskUserTool 工厂函数
//	└── ask_user_test.go  # 单元测试
//
// 对应 Python 代码：openjiuwen/harness/tools/ask_user.py
package ask_user
```

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/harness/tools/ask_user/doc.go
git commit -m "feat(9.38): 添加 ask_user 工具子包 doc.go"
```

---

### Task 2: 创建 ask_user.go — AskUserTool 空壳 + NewAskUserTool

**Files:**
- Create: `internal/agentcore/harness/tools/ask_user/ask_user.go`

- [ ] **Step 1: 写 ask_user.go**

```go
package ask_user

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AskUserTool 向用户提问的空壳工具。
// invoke/stream 返回空 map{}，真正逻辑在 AskUserRail 中通过中断机制完成。
//
// 对齐 Python: AskUserTool(Tool) — openjiuwen/harness/tools/ask_user.py
// Python 中 AskUserTool.invoke(query, **kwargs) return {} / stream(query, **kwargs) yield {}
type AskUserTool struct{}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAskUserTool 创建 AskUserTool 空壳实例。
// 从 prompts/tools 注册表获取 ToolCard，用 MapFunction 包装空壳 invoke 函数。
//
// 对齐 Python: AskUserTool.__init__(language, agent_id)
//   super().__init__(build_tool_card(name="ask_user", tool_id="ask_user", language=language, agent_id=agent_id))
func NewAskUserTool(language, agentID string) (tool.Tool, error) {
	card, err := hprompts.BuildToolCard("ask_user", "ask_user", language, nil, agentID)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "ask_user_tool_create").
			Err(err).
			Msg("构建 ask_user ToolCard 失败")
		return nil, fmt.Errorf("构建 ask_user ToolCard 失败: %w", err)
	}

	// 空壳 invoke：返回空 map（对齐 Python AskUserTool.invoke → return {}）
	askUserTool, err := tool.NewMapFunction(
		card,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{}, nil
		},
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("创建 AskUserTool 失败: %w", err)
	}

	logger.Info(logComponent).
		Str("event_type", "ask_user_tool_create").
		Str("tool_id", card.ID).
		Msg("AskUserTool 创建成功")

	return askUserTool, nil
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/agentcore/harness/tools/ask_user/ask_user.go
git commit -m "feat(9.38): 添加 AskUserTool 空壳工具 + NewAskUserTool 工厂函数"
```

---

### Task 3: 创建 ask_user_test.go — 单元测试

**Files:**
- Create: `internal/agentcore/harness/tools/ask_user/ask_user_test.go`

- [ ] **Step 1: 写测试文件**

```go
package ask_user

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewAskUserTool 验证默认 cn 语言创建成功
func TestNewAskUserTool(t *testing.T) {
	askUserTool, err := NewAskUserTool("cn", "test_agent")
	require.NoError(t, err)
	assert.NotNil(t, askUserTool)
	assert.Equal(t, "ask_user", askUserTool.Card().Name)
}

// TestNewAskUserTool_en语言 验证 en 语言创建成功
func TestNewAskUserTool_en语言(t *testing.T) {
	askUserTool, err := NewAskUserTool("en", "test_agent")
	require.NoError(t, err)
	assert.NotNil(t, askUserTool)
	assert.Equal(t, "ask_user", askUserTool.Card().Name)
}

// TestNewAskUserTool_ToolCard属性 验证 ToolCard 属性和 input_params 结构
func TestNewAskUserTool_ToolCard属性(t *testing.T) {
	askUserTool, err := NewAskUserTool("cn", "test_agent")
	require.NoError(t, err)

	card := askUserTool.Card()
	assert.Equal(t, "ask_user", card.Name)
	assert.Contains(t, card.ID, "ask_user")
	assert.NotNil(t, card.InputParams)
	assert.NotEmpty(t, card.Description)
}

// TestNewAskUserTool_Invoke空壳 验证 invoke 返回空 map{}
// 对齐 Python: AskUserTool.invoke(query, **kwargs) → return {}
func TestNewAskUserTool_Invoke空壳(t *testing.T) {
	askUserTool, err := NewAskUserTool("cn", "test_agent")
	require.NoError(t, err)

	result, err := askUserTool.Invoke(context.TODO(), map[string]any{"questions": "test"})
	assert.NoError(t, err)
	assert.Equal(t, map[string]any{}, result)
}

// TestNewAskUserTool_Stream不支持 验证 stream 返回错误
// MapFunction 的 streamFn 为 nil，调用 Stream 应返回 ErrStreamNotSupported
func TestNewAskUserTool_Stream不支持(t *testing.T) {
	askUserTool, err := NewAskUserTool("cn", "test_agent")
	require.NoError(t, err)

	_, err = askUserTool.Stream(context.TODO(), map[string]any{"questions": "test"})
	assert.Error(t, err)
}
```

- [ ] **Step 2: 运行测试确认通过**

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/tools/ask_user/ -v
```

预期：5 个测试全部 PASS。

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/harness/tools/ask_user/ask_user_test.go
git commit -m "test(9.38): 添加 AskUserTool 5 个单元测试"
```

---

### Task 4: 修改 AskUserRail.Init — 从子包创建工具

**Files:**
- Modify: `internal/agentcore/harness/rails/interrupt/ask_user_rail.go:3-15,80-134`

- [ ] **Step 1: 修改 import 块**

将 `ask_user_rail.go` 的 import 从：

```go
import (
	"context"
	"encoding/json"
	"fmt"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)
```

改为：

```go
import (
	"context"
	"encoding/json"
	"fmt"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	askuser "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/ask_user"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)
```

变更：删除 `hprompts` import，新增 `askuser` import。`context` 和 `tool` import 仍需保留（`context` 被 resolveAskUserInterrupt 使用，`tool.Tool` 被 AskUserRail.tools 字段和 Uninit 使用）。

- [ ] **Step 2: 修改 Init 方法**

将 `ask_user_rail.go` 第 80-134 行的 Init 方法从：

```go
func (r *AskUserRail) Init(agent agentinterfaces.BaseAgent) error {
	var language string
	var agentID string

	sb := agent.SystemPromptBuilder()
	if sb != nil {
		language = sb.Language()
	} else {
		language = "cn"
	}
	if card := agent.Card(); card != nil {
		agentID = card.ID
	}

	// 构建 AskUserTool 的 ToolCard
	toolCard, err := hprompts.BuildToolCard("ask_user", "ask_user", language, nil, agentID)
	if err != nil {
		logger.Warn(askUserRailLogComponent).
			Str("event_type", "ask_user_rail_init").
			Err(err).
			Msg("构建 ask_user ToolCard 失败")
		return fmt.Errorf("构建 ask_user ToolCard 失败: %w", err)
	}

	// 创建 MapFunction 空壳工具（逻辑在 Rail 中，工具本身不执行）
	askUserTool, err := tool.NewMapFunction(
		toolCard,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{}, nil
		},
		nil,
	)
	if err != nil {
		return fmt.Errorf("创建 AskUserTool 失败: %w", err)
	}
	r.tools = []tool.Tool{askUserTool}

	// 注册到 AbilityManager + ResourceMgr
	am := agent.AbilityManager()
	resourceMgr := runner.GetResourceMgr()
	for _, t := range r.tools {
		if am != nil {
			am.Add(t.Card())
		}
		if resourceMgr != nil {
			_ = resourceMgr.AddTool(t)
		}
	}

	logger.Info(askUserRailLogComponent).
		Str("event_type", "ask_user_rail_init").
		Msg("AskUserRail 已注册 ask_user 工具")

	return nil
}
```

改为：

```go
func (r *AskUserRail) Init(agent agentinterfaces.BaseAgent) error {
	var language string
	var agentID string

	sb := agent.SystemPromptBuilder()
	if sb != nil {
		language = sb.Language()
	} else {
		language = "cn"
	}
	if card := agent.Card(); card != nil {
		agentID = card.ID
	}

	// 从 ask_user 子包创建空壳工具（对齐 Python AskUserTool.__init__）
	askUserTool, err := askuser.NewAskUserTool(language, agentID)
	if err != nil {
		logger.Warn(askUserRailLogComponent).
			Str("event_type", "ask_user_rail_init").
			Err(err).
			Msg("创建 AskUserTool 失败")
		return fmt.Errorf("创建 AskUserTool 失败: %w", err)
	}
	r.tools = []tool.Tool{askUserTool}

	// 注册到 AbilityManager + ResourceMgr
	am := agent.AbilityManager()
	resourceMgr := runner.GetResourceMgr()
	for _, t := range r.tools {
		if am != nil {
			am.Add(t.Card())
		}
		if resourceMgr != nil {
			_ = resourceMgr.AddTool(t)
		}
	}

	logger.Info(askUserRailLogComponent).
		Str("event_type", "ask_user_rail_init").
		Msg("AskUserRail 已注册 ask_user 工具")

	return nil
}
```

- [ ] **Step 3: 运行已有测试确认重构无破坏**

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/interrupt/ -v -run "TestAskUser|TestNewAskUser|TestBaseInterrupt"
```

预期：18 个 AskUserRail 单元测试 + 2 个 BaseInterruptRail 测试全部 PASS。

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/harness/rails/interrupt/ask_user_rail.go
git commit -m "refactor(9.38): AskUserRail.Init 改为从 ask_user 子包创建工具"
```

---

### Task 5: 修正提示词换行差异

**Files:**
- Modify: `internal/agentcore/harness/prompts/tools/ask_user.go:11-26`

Python 源码（`openjiuwen/harness/prompts/tools/ask_user.py`）中：

- cn 版本：第 18-20 行三句直接拼接（Python 隐式字符串拼接），结果为同一行无换行分隔
- en 版本：第 29-31 行三句直接拼接，每行末尾有空格，结果为同一行空格分隔

- [ ] **Step 1: 修改 cn 描述**

将 `ask_user.go` 第 12-18 行的 cn 描述从：

```go
	"cn": `向用户提问以收集信息、澄清歧义或做出决策。支持1-4个问题，每个问题2-4个选项。

何时主动使用：需求模糊、多种方案可选、涉及用户偏好时，应主动询问而非假设。

【禁止】选项中添加'其他'、'自定义'等兜底选项，系统已自动提供。
【推荐】将推荐选项放第一位，label末尾加'（推荐）'。
preview字段仅用于单选问题的视觉比较场景。`,
```

改为（三句必须在同一物理行，无换行分隔，严格对齐 Python 第 18-20 行拼接结果）：

```go
	"cn": `向用户提问以收集信息、澄清歧义或做出决策。支持1-4个问题，每个问题2-4个选项。

何时主动使用：需求模糊、多种方案可选、涉及用户偏好时，应主动询问而非假设。

【禁止】选项中添加'其他'、'自定义'等兜底选项，系统已自动提供。【推荐】将推荐选项放第一位，label末尾加'（推荐）'。preview字段仅用于单选问题的视觉比较场景。`,
```

关键：`【禁止】`、`【推荐】`、`preview` 三句必须写在 raw string 的同一物理行上，之间无任何换行。

- [ ] **Step 2: 修改 en 描述**

将 `ask_user.go` 第 19-25 行的 en 描述从：

```go
	"en": `Ask user questions to gather info, clarify ambiguity, or make decisions. Supports 1-4 questions, each with 2-4 options.

When to use proactively: Ask when requirements are vague, multiple approaches exist, or user preferences matter. Don't assume.

FORBIDDEN: Adding 'Other', 'Custom' etc. as options — system provides this automatically.
RECOMMENDED: Place recommended option first, append '(Recommended)' to its label.
Preview field is only for single-select questions with visual comparison needs.`,
```

改为（三句必须在同一物理行，空格分隔，严格对齐 Python 第 29-31 行拼接结果）：

```go
	"en": `Ask user questions to gather info, clarify ambiguity, or make decisions. Supports 1-4 questions, each with 2-4 options.

When to use proactively: Ask when requirements are vague, multiple approaches exist, or user preferences matter. Don't assume.

FORBIDDEN: Adding 'Other', 'Custom' etc. as options — system provides this automatically. RECOMMENDED: Place recommended option first, append '(Recommended)' to its label. Preview field is only for single-select questions with visual comparison needs.`,
```

关键：`FORBIDDEN`、`RECOMMENDED`、`Preview` 三句必须写在 raw string 的同一物理行上，之间用空格分隔（而非换行）。

- [ ] **Step 3: 运行 prompts/tools 测试确认描述正确**

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/prompts/tools/ -v -run "TestAskUser|TestToolMetadata"
```

预期：AskUserMetadataProvider 相关测试 PASS。

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/harness/prompts/tools/ask_user.go
git commit -m "fix(9.38): 修正 ask_user 提示词换行差异，严格一比一复刻 Python"
```

---

### Task 6: 全量测试验证

**Files:** 无文件改动，仅运行测试

- [ ] **Step 1: 运行 ask_user 子包测试**

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/tools/ask_user/ -v
```

预期：5 个测试 PASS。

- [ ] **Step 2: 运行 AskUserRail 测试**

```bash
go test ./internal/agentcore/harness/rails/interrupt/ -v -run "TestAskUser|TestNewAskUser|TestBaseInterrupt"
```

预期：20 个测试 PASS（18 AskUserRail + 2 BaseInterrupt）。

- [ ] **Step 3: 运行 prompts/tools 测试**

```bash
go test ./internal/agentcore/harness/prompts/tools/ -v
```

预期：所有工具元数据测试 PASS，包含 AskUserMetadataProvider 描述验证。

- [ ] **Step 4: 检查覆盖率**

```bash
go test -cover ./internal/agentcore/harness/tools/ask_user/ ./internal/agentcore/harness/prompts/tools/
```

预期：覆盖率 ≥ 85%。

---

### Task 7: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 9.38-49 合并行**

将 IMPLEMENTATION_PLAN.md 中 9.38-49 行的 `AskUser` 改为 `✅AskUser`（与 `✅Cron` 格式一致）。

找到包含 `Shell/文件系统/代码/MCP/Worktree/浏览器/✅Cron/TODO/AskUser/Memory/AgentMode/多模态` 的行，改为 `Shell/文件系统/代码/MCP/Worktree/浏览器/✅Cron/TODO/✅AskUser/Memory/AgentMode/多模态`。

- [ ] **Step 2: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs(9.38): 更新实现计划 — AskUser 标记为 ✅"
```

---

## Self-Review

### 1. Spec coverage

逐项对照设计文档：

| 设计要求 | 对应 Task |
|---------|----------|
| 新增 ask_user/ 子包（doc.go + ask_user.go + ask_user_test.go） | Task 1, 2, 3 |
| AskUserRail.Init 改为从子包创建 | Task 4 |
| 修正 cn 描述换行差异 | Task 5 |
| 修正 en 描述换行差异 | Task 5 |
| IMPLEMENTATION_PLAN.md AskUser → ✅AskUser | Task 7 |
| 已有测试验证 | Task 6 |

所有设计要求均有对应 Task，无遗漏。

### 2. Placeholder scan

无 TBD/TODO/未实现步骤。所有代码块包含完整内容，所有命令有具体预期输出。

### 3. Type consistency

- `NewAskUserTool(language, agentID string) (tool.Tool, error)` — Task 2 定义，Task 4 调用，签名一致
- `askuser` import alias — Task 4 定义，与子包路径一致
- `tool.Tool` 类型 — Task 2 返回，Task 4 赋值到 `r.tools []tool.Tool`，类型一致
