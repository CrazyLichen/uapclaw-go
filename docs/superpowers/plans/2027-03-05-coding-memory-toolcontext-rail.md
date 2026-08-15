# 7.3 CodingMemoryToolContext + CodingMemoryRail + MemoryRail 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 CodingMemoryToolContext 被正式消费（7.3），同时实现 CodingMemoryRail + MemoryRail（9.19-23），并回填所有相关回填点。

**Architecture:** CodingMemoryRail 在 Init 中创建 CodingMemoryToolContext + 3 个 Tool 实例并注册到 agent；BeforeInvoke 启动 goroutine 预取记忆；BeforeModelCall 将召回结果注入 system prompt。MemoryRail 结构类似但无 auto_recall。MemberMemoryToolkit 根据 scenario 选择 coding/general 工具。上层 code_agent_factory 和 code_adapter 回填 CodingMemoryRail 注入。

**Tech Stack:** Go 1.22+, 依赖已有的 memory/lite 包、harness/rails 包、tool 接口

**Design Spec:** `docs/superpowers/specs/2027-03-05-coding-memory-toolcontext-rail-design.md`

---

## 文件结构

```
新建：
  harness/tools/coding_memory/doc.go
  harness/tools/coding_memory/coding_memory_tool.go
  harness/tools/coding_memory/coding_memory_tool_test.go
  harness/tools/memory/doc.go
  harness/tools/memory/memory_tool.go
  harness/tools/memory/memory_tool_test.go
  harness/rails/memory/doc.go
  harness/rails/memory/coding_memory_rail.go
  harness/rails/memory/coding_memory_rail_test.go
  harness/rails/memory/memory_rail.go
  harness/rails/memory/memory_rail_test.go

修改：
  internal/agentcore/memory/lite/coding_memory_tool_context.go  — 添加 WithXxx Builder
  internal/agentcore/memory/lite/tool_context.go                — 添加 WithXxx Builder
  internal/agentcore/memory/lite/tool_context_base.go           — 添加 WithXxx Builder
  internal/agent_teams/memory/member_memory_toolkit.go          — ctx any→接口, 实现 Initialize/GetTools/Close
  internal/agentcore/harness/code_agent_factory.go              — 注入 CodingMemoryRail
  internal/swarm/server/adapter/code_adapter.go                 — buildCodingMemoryRail 真实实现
  IMPLEMENTATION_PLAN.md                                         — 更新状态
```

---

### Task 1: CodingMemoryToolContext + MemoryToolContext + LiteMemoryToolContextBase 添加 Builder 方法

**Files:**
- Modify: `internal/agentcore/memory/lite/tool_context_base.go`
- Modify: `internal/agentcore/memory/lite/tool_context.go`
- Modify: `internal/agentcore/memory/lite/coding_memory_tool_context.go`
- Test: `internal/agentcore/memory/lite/tool_context_base_test.go`（新建）
- Test: `internal/agentcore/memory/lite/tool_context_test.go`（新建）
- Test: `internal/agentcore/memory/lite/coding_memory_tool_context_test.go`（新建）

- [ ] **Step 1: 给 LiteMemoryToolContextBase 添加 WithXxx 方法**

在 `tool_context_base.go` 的 `导出函数` 区块，`EnsureManager` 之前添加：

```go
// WithWorkspace 设置工作空间
func (b *LiteMemoryToolContextBase) WithWorkspace(ws *workspace.Workspace) *LiteMemoryToolContextBase {
	b.Workspace = ws
	return b
}

// WithSettings 设置记忆配置
func (b *LiteMemoryToolContextBase) WithSettings(s *MemorySettings) *LiteMemoryToolContextBase {
	b.Settings = s
	return b
}

// WithAgentID 设置 Agent 标识
func (b *LiteMemoryToolContextBase) WithAgentID(id string) *LiteMemoryToolContextBase {
	b.AgentID = id
	return b
}

// WithEmbeddingConfig 设置嵌入配置
func (b *LiteMemoryToolContextBase) WithEmbeddingConfig(cfg *embedding.EmbeddingConfig) *LiteMemoryToolContextBase {
	b.EmbeddingConfig = cfg
	return b
}

// WithSysOperation 设置系统操作接口
func (b *LiteMemoryToolContextBase) WithSysOperation(op sysop.SysOperation) *LiteMemoryToolContextBase {
	b.SysOperation = op
	return b
}

// WithManager 设置记忆索引管理器
func (b *LiteMemoryToolContextBase) WithManager(mgr MemoryIndexManager) *LiteMemoryToolContextBase {
	b.Manager = mgr
	return b
}

// WithNodeName 设置节点名称
func (b *LiteMemoryToolContextBase) WithNodeName(name string) *LiteMemoryToolContextBase {
	b.NodeName = name
	return b
}
```

- [ ] **Step 2: 给 MemoryToolContext 添加 WithXxx 方法**

在 `tool_context.go` 的 `导出函数` 区块添加：

```go
// WithWorkspace 设置工作空间
func (c *MemoryToolContext) WithWorkspace(ws *workspace.Workspace) *MemoryToolContext {
	c.Workspace = ws
	return c
}

// WithSettings 设置记忆配置
func (c *MemoryToolContext) WithSettings(s *MemorySettings) *MemoryToolContext {
	c.Settings = s
	return c
}

// WithAgentID 设置 Agent 标识
func (c *MemoryToolContext) WithAgentID(id string) *MemoryToolContext {
	c.AgentID = id
	return c
}

// WithEmbeddingConfig 设置嵌入配置
func (c *MemoryToolContext) WithEmbeddingConfig(cfg *embedding.EmbeddingConfig) *MemoryToolContext {
	c.EmbeddingConfig = cfg
	return c
}

// WithSysOperation 设置系统操作接口
func (c *MemoryToolContext) WithSysOperation(op sysop.SysOperation) *MemoryToolContext {
	c.SysOperation = op
	return c
}

// WithManager 设置记忆索引管理器
func (c *MemoryToolContext) WithManager(mgr MemoryIndexManager) *MemoryToolContext {
	c.Manager = mgr
	return c
}
```

- [ ] **Step 3: 给 CodingMemoryToolContext 添加 WithXxx 方法**

在 `coding_memory_tool_context.go` 的 `导出函数` 区块添加：

```go
// WithWorkspace 设置工作空间
func (c *CodingMemoryToolContext) WithWorkspace(ws *workspace.Workspace) *CodingMemoryToolContext {
	c.Workspace = ws
	return c
}

// WithSettings 设置记忆配置
func (c *CodingMemoryToolContext) WithSettings(s *MemorySettings) *CodingMemoryToolContext {
	c.Settings = s
	return c
}

// WithAgentID 设置 Agent 标识
func (c *CodingMemoryToolContext) WithAgentID(id string) *CodingMemoryToolContext {
	c.AgentID = id
	return c
}

// WithEmbeddingConfig 设置嵌入配置
func (c *CodingMemoryToolContext) WithEmbeddingConfig(cfg *embedding.EmbeddingConfig) *CodingMemoryToolContext {
	c.EmbeddingConfig = cfg
	return c
}

// WithSysOperation 设置系统操作接口
func (c *CodingMemoryToolContext) WithSysOperation(op sysop.SysOperation) *CodingMemoryToolContext {
	c.SysOperation = op
	return c
}

// WithManager 设置记忆索引管理器
func (c *CodingMemoryToolContext) WithManager(mgr MemoryIndexManager) *CodingMemoryToolContext {
	c.Manager = mgr
	return c
}

// WithCodingMemoryDir 设置编程记忆目录
func (c *CodingMemoryToolContext) WithCodingMemoryDir(dir string) *CodingMemoryToolContext {
	c.CodingMemoryDir = dir
	return c
}
```

- [ ] **Step 4: 编写 Builder 方法测试**

创建 `internal/agentcore/memory/lite/coding_memory_tool_context_test.go`：

```go
package lite

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// TestNewCodingMemoryToolContext_默认值 测试默认值
func TestNewCodingMemoryToolContext_默认值(t *testing.T) {
	ctx := NewCodingMemoryToolContext()
	if ctx.NodeName != "coding_memory" {
		t.Errorf("NodeName = %q, want %q", ctx.NodeName, "coding_memory")
	}
	if ctx.CodingMemoryDir != "" {
		t.Errorf("CodingMemoryDir = %q, want empty", ctx.CodingMemoryDir)
	}
}

// TestCodingMemoryToolContext_WithXxx 测试 Builder 链式调用
func TestCodingMemoryToolContext_WithXxx(t *testing.T) {
	ws := &workspace.Workspace{}
	settings := CreateMemorySettings("/tmp/cm", nil)
	embCfg := &embedding.EmbeddingConfig{}
	var op sysop.SysOperation

	ctx := NewCodingMemoryToolContext().
		WithWorkspace(ws).
		WithSettings(settings).
		WithAgentID("test-agent").
		WithEmbeddingConfig(embCfg).
		WithSysOperation(op).
		WithCodingMemoryDir("/tmp/cm")

	if ctx.Workspace != ws {
		t.Error("WithWorkspace 未设置")
	}
	if ctx.Settings != settings {
		t.Error("WithSettings 未设置")
	}
	if ctx.AgentID != "test-agent" {
		t.Errorf("AgentID = %q, want %q", ctx.AgentID, "test-agent")
	}
	if ctx.EmbeddingConfig != embCfg {
		t.Error("WithEmbeddingConfig 未设置")
	}
	if ctx.CodingMemoryDir != "/tmp/cm" {
		t.Errorf("CodingMemoryDir = %q, want %q", ctx.CodingMemoryDir, "/tmp/cm")
	}
}
```

- [ ] **Step 5: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/lite/... -run "TestNewCodingMemoryToolContext|TestCodingMemoryToolContext_WithXxx" -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/memory/lite/tool_context_base.go internal/agentcore/memory/lite/tool_context.go internal/agentcore/memory/lite/coding_memory_tool_context.go internal/agentcore/memory/lite/coding_memory_tool_context_test.go
git commit -m "feat: 添加 CodingMemoryToolContext/MemoryToolContext/LiteMemoryToolContextBase Builder 方法"
```

---

### Task 2: 创建 coding_memory Tool 包（3 个 Tool + CreateCodingMemoryTools）

**Files:**
- Create: `internal/agentcore/harness/tools/coding_memory/doc.go`
- Create: `internal/agentcore/harness/tools/coding_memory/coding_memory_tool.go`
- Create: `internal/agentcore/harness/tools/coding_memory/coding_memory_tool_test.go`

- [ ] **Step 1: 创建包目录**

Run: `mkdir -p /home/opensource/uapclaw-gateway/internal/agentcore/harness/tools/coding_memory`

- [ ] **Step 2: 创建 doc.go**

```go
// Package coding_memory 提供编程记忆工具集。
//
// 包含 3 个 Tool 实现（CodingMemoryReadTool / CodingMemoryWriteTool / CodingMemoryEditTool）
// 和工厂函数 CreateCodingMemoryTools。
//
// 每个 Tool 持有 *lite.CodingMemoryToolContext 引用，
// Invoke 内部调用 coding_memory_tool_ops.XxxWithContext。
//
// 文件目录：
//
//	coding_memory/
//	├── doc.go                    # 包文档
//	├── coding_memory_tool.go     # 3 个 Tool 结构体 + CreateCodingMemoryTools 工厂
//	└── coding_memory_tool_test.go # 单元测试
//
// 对应 Python 代码：openjiuwen/harness/tools/coding_memory.py
package coding_memory
```

- [ ] **Step 3: 创建 coding_memory_tool.go**

完整实现 3 个 Tool 结构体，每个持有 `*lite.CodingMemoryToolContext`，`Invoke` 调用 `coding_memory_tool_ops.XxxWithContext`，`Stream` 返回 `tool.NewErrStreamNotSupported`。

`CreateCodingMemoryTools` 对齐 Python `create_coding_memory_tools`：
1. 如果 `ctx.Workspace != nil`，设置 `CodingMemoryDir` 和 `Settings`
2. 设置 `NodeName = "coding_memory"`
3. 通过 `htools.BuildToolCard` 创建 ToolCard
4. 创建 3 个 Tool 并返回

关键：使用 `htools.BuildToolCard("coding_memory_read", "CodingMemoryReadTool", language, nil, agentID)` 建卡，与 `prompts/tools/coding_memory.go` 中注册的 MetadataProvider 对齐。

- [ ] **Step 4: 编写测试**

创建 `coding_memory_tool_test.go`，测试：
- `TestCreateCodingMemoryTools_默认` — 验证返回 3 个 Tool
- `TestCodingMemoryReadTool_Invoke_路径缺失` — path 为空返回错误
- `TestCodingMemoryWriteTool_Invoke_路径缺失` — path 为空返回错误
- `TestCodingMemoryEditTool_Invoke_路径缺失` — path/old_text/new_text 为空返回错误

使用 `lite.NewCodingMemoryToolContext()` 构造空 ctx（Manager 为 nil 时不调用 ops，仅测试参数校验）。

- [ ] **Step 5: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/harness/tools/coding_memory/... -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/harness/tools/coding_memory/
git commit -m "feat: 创建 coding_memory Tool 包（3 个 Tool + CreateCodingMemoryTools）"
```

---

### Task 3: 创建 memory Tool 包（5 个 Tool + CreateMemoryTools）

**Files:**
- Create: `internal/agentcore/harness/tools/memory/doc.go`
- Create: `internal/agentcore/harness/tools/memory/memory_tool.go`
- Create: `internal/agentcore/harness/tools/memory/memory_tool_test.go`

- [ ] **Step 1: 创建包目录**

Run: `mkdir -p /home/opensource/uapclaw-gateway/internal/agentcore/harness/tools/memory`

- [ ] **Step 2: 创建 doc.go**

```go
// Package memory 提供通用记忆工具集。
//
// 包含 5 个 Tool 实现（MemorySearchTool / MemoryGetTool / WriteMemoryTool / EditMemoryTool / ReadMemoryTool）
// 和工厂函数 CreateMemoryTools。
//
// 每个 Tool 持有 *lite.MemoryToolContext 引用，
// Invoke 内部调用 memory_tool_ops.XxxWithContext。
//
// 文件目录：
//
//	memory/
//	├── doc.go              # 包文档
//	├── memory_tool.go      # 5 个 Tool 结构体 + CreateMemoryTools 工厂
//	└── memory_tool_test.go # 单元测试
//
// 对应 Python 代码：openjiuwen/harness/tools/memory.py
package memory
```

- [ ] **Step 3: 创建 memory_tool.go**

完整实现 5 个 Tool 结构体，每个持有 `*lite.MemoryToolContext`，`Invoke` 调用 `tool_ops.XxxWithContext`，`Stream` 返回 `tool.NewErrStreamNotSupported`。

`CreateMemoryTools` 对齐 Python `create_memory_tools`：始终返回 5 个 Tool（search/get/write/edit/read）。readOnly 模式通过 prompt 注入控制，不裁剪工具。

- [ ] **Step 4: 编写测试**

创建 `memory_tool_test.go`，测试：
- `TestCreateMemoryTools_默认` — 验证返回 5 个 Tool
- `TestMemorySearchTool_Invoke_查询缺失` — query 为空返回错误
- `TestMemoryGetTool_Invoke_路径缺失` — path 为空返回错误
- `TestWriteMemoryTool_Invoke_路径缺失` — path 为空返回错误
- `TestEditMemoryTool_Invoke_路径缺失` — path/old_text/new_text 为空返回错误
- `TestReadMemoryTool_Invoke_路径缺失` — path 为空返回错误

- [ ] **Step 5: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/harness/tools/memory/... -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/harness/tools/memory/
git commit -m "feat: 创建 memory Tool 包（5 个 Tool + CreateMemoryTools）"
```

---

### Task 4: 创建 MemoryRail 包 + CodingMemoryRail

**Files:**
- Create: `internal/agentcore/harness/rails/memory/doc.go`
- Create: `internal/agentcore/harness/rails/memory/coding_memory_rail.go`
- Create: `internal/agentcore/harness/rails/memory/coding_memory_rail_test.go`

- [ ] **Step 1: 创建包目录**

Run: `mkdir -p /home/opensource/uapclaw-gateway/internal/agentcore/harness/rails/memory`

- [ ] **Step 2: 创建 doc.go**

```go
// Package memory 提供记忆护栏 Rail 实现。
//
// 包含 CodingMemoryRail（编程记忆护栏，含自动召回）和 MemoryRail（通用记忆护栏）。
//
// 文件目录：
//
//	memory/
//	├── doc.go                    # 包文档
//	├── coding_memory_rail.go     # CodingMemoryRail 编程记忆护栏
//	├── coding_memory_rail_test.go # 单元测试
//	├── memory_rail.go            # MemoryRail 通用记忆护栏
//	└── memory_rail_test.go       # 单元测试
//
// 对应 Python 代码：openjiuwen/harness/rails/memory/
package memory
```

- [ ] **Step 3: 创建 coding_memory_rail.go**

完整实现 CodingMemoryRail，包含：

**结构体**：`CodingMemoryRail`（嵌入 `rails.DeepAgentRail`），字段：`codingMemoryDir`、`embeddingConfig`、`language`、`manager`、`managerInitialized`、`toolCtx`、`systemPromptBuilder`、`agentID`、`recallResult`、`recallDone`、`ownedToolNames`、`ownedToolIDs`

**常量**：`codingMemoryRailPriority = 80`、`maxRecallResults = 5`、`maxRecallTotalBytes = 10240`

**构造函数**：`NewCodingMemoryRail(codingMemoryDir, embeddingConfig, language)` — 设置优先级 80

**Init**：对齐 Python `_register_coding_memory_tools`
1. 获取 `agentID`（从 `agent.Card().ID`）
2. 获取 `systemPromptBuilder`（从 `agent.SystemPromptBuilder()`）
3. 获取 `language`（从 `systemPromptBuilder.Language()`，fallback 到 `r.language`）
4. 创建 `CodingMemoryToolContext`（Builder 链式调用）
5. 调用 `coding_memory.CreateCodingMemoryTools(r.toolCtx, language, agentID)`
6. 恢复 `CodingMemoryDir`（对齐 Python 注释：create 会覆盖 coding_memory_dir）
7. 遍历 tools，注册到 `runner.GetResourceMgr()` + `agent.AbilityManager()`
8. 记录 `ownedToolNames` + `ownedToolIDs`

**Uninit**：对齐 Python `uninit`
1. 从 `abilityManager` 移除 `ownedToolNames`
2. 从 `resourceMgr` 移除 `ownedToolIDs`
3. 清理 `systemPromptBuilder` 中的 "memory" section
4. 清空状态

**BeforeInvoke**：对齐 Python `before_invoke`
1. 首次初始化 Manager（调用 `lite.InitCodingMemoryManagerAsync`）
2. 设置 `toolCtx.Manager`
3. 重置召回状态（`recallResult = nil`，`recallDone = make(chan struct{}, 1)`）
4. 检查是否只读模式（`InvokeInputs.IsCron()` 或 `IsHeartbeat()`）
5. 提取用户查询（`extractLastUserQuery`）
6. 非只读 && 有 manager && 有查询 → 启动 goroutine 执行 `autoRecall`

**BeforeModelCall**：对齐 Python `before_model_call`
1. 获取 `systemPromptBuilder`，移除旧 "memory" section
2. 构建基础 section（`sections.BuildCodingMemorySection`）
3. 只读模式 → 仅注入 MEMORY.md 索引
4. 非阻塞检查预取结果（`select` + `recallDone`）
5. 互斥注入：有召回 → 注入全文 + footer；无召回 → 降级注入索引
6. 添加 section 到 `systemPromptBuilder`

**GetCallbacks**：声明 4 个钩子（Init/Uninit/BeforeInvoke/BeforeModelCall）

**辅助方法**：
- `extractLastUserQuery(cbc)` — 从 `InvokeInputs.Query` 提取最后一条用户消息
- `autoRecall(ctx, query)` — 对齐 Python `_auto_recall`，执行搜索 + 组装结果
- `readMemoryIndex(ctx)` — 读取 MEMORY.md 索引
- `countMemoryFiles(ctx, dir)` — 统计 .md 记忆文件数
- `isReadOnly(cbc)` — 检查是否 cron/heartbeat

- [ ] **Step 4: 编写测试**

创建 `coding_memory_rail_test.go`，测试：
- `TestNewCodingMemoryRail_默认值` — 验证优先级 80
- `TestCodingMemoryRail_Priority` — 验证 Priority() 返回 80
- `TestCodingMemoryRail_Init_无AbilityManager` — agent 无 ability_manager 时不 panic
- `TestCodingMemoryRail_ExtractLastUserQuery` — 验证查询提取
- `TestCodingMemoryRail_IsReadOnly` — 验证 cron/heartbeat 判断

使用 mock `BaseAgent`（`AbilityManager()` 返回 nil）和 mock `SystemPromptBuilderInterface`。

- [ ] **Step 5: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/harness/rails/memory/... -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/harness/rails/memory/doc.go internal/agentcore/harness/rails/memory/coding_memory_rail.go internal/agentcore/harness/rails/memory/coding_memory_rail_test.go
git commit -m "feat: 创建 CodingMemoryRail 编程记忆护栏"
```

---

### Task 5: 创建 MemoryRail

**Files:**
- Create: `internal/agentcore/harness/rails/memory/memory_rail.go`
- Create: `internal/agentcore/harness/rails/memory/memory_rail_test.go`

- [ ] **Step 1: 创建 memory_rail.go**

完整实现 MemoryRail，对齐 Python `memory_rail.py`。比 CodingMemoryRail 更简单（无 auto_recall）。

**结构体**：`MemoryRail`（嵌入 `rails.DeepAgentRail`），字段：`embeddingConfig`、`isProactive`、`initialized`、`managerInitialized`、`toolCtx`、`systemPromptBuilder`、`isReadOnly`、`ownedToolNames`、`ownedToolIDs`

**构造函数**：`NewMemoryRail(embeddingConfig, isProactive)` — 设置优先级 80

**Init**：对齐 Python `_register_memory_tools`
1. 获取 `agentID` + `language`
2. 创建 `MemoryToolContext`（Builder 链式调用）
3. 调用 `memory.CreateMemoryTools(r.toolCtx, language, agentID)`
4. 遍历 tools，注册到 `resourceMgr` + `abilityManager`
5. 记录 `ownedToolNames` + `ownedToolIDs`

**Uninit**：对齐 Python `uninit`
1. 从 `abilityManager` 移除 `ownedToolNames`
2. 从 `resourceMgr` 移除 `ownedToolIDs`
3. 清理 `systemPromptBuilder` 中的 "memory" section
4. 清空状态

**BeforeInvoke**：对齐 Python `before_invoke`
1. 首次初始化 Manager（调用 `lite.InitMemoryManagerAsync`）
2. 设置 `toolCtx.Manager`
3. 检查是否只读模式（`isReadOnly`）

**BeforeModelCall**：对齐 Python `before_model_call`
1. 获取 `systemPromptBuilder`，移除旧 "memory" section
2. 根据 `isReadOnly` + `isProactive` 选择 mode（"proactive"/"inactive"/"read_only"）
3. 调用 `sections.BuildMemorySection(mode, todayDate, language)`
4. 添加 section 到 `systemPromptBuilder`

**GetCallbacks**：声明 4 个钩子（Init/Uninit/BeforeInvoke/BeforeModelCall）

- [ ] **Step 2: 编写测试**

创建 `memory_rail_test.go`，测试：
- `TestNewMemoryRail_默认值` — 验证优先级 80
- `TestMemoryRail_Priority` — 验证 Priority() 返回 80
- `TestMemoryRail_Init_无AbilityManager` — agent 无 ability_manager 时不 panic

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/harness/rails/memory/... -v`
Expected: PASS

- [ ] **Step 4: 更新 rails/doc.go**

在 `harness/rails/doc.go` 中添加 memory 子包说明：

```
//	└── memory/            # 记忆护栏子包
//	    ├── doc.go              # 包文档
//	    ├── coding_memory_rail.go # CodingMemoryRail 编程记忆护栏（自动召回+工具注册）
//	    └── memory_rail.go       # MemoryRail 通用记忆护栏（工具注册+prompt 注入）
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/rails/memory/memory_rail.go internal/agentcore/harness/rails/memory/memory_rail_test.go internal/agentcore/harness/rails/doc.go
git commit -m "feat: 创建 MemoryRail 通用记忆护栏"
```

---

### Task 6: MemberMemoryToolkit 回填

**Files:**
- Modify: `internal/agent_teams/memory/member_memory_toolkit.go`

- [ ] **Step 1: 修改 ctx 类型从 any 到接口**

定义 `liteToolCtx` 接口（替代 `any`），`CodingMemoryToolContext` 和 `MemoryToolContext` 通过嵌入 `LiteMemoryToolContextBase` 自动满足此接口：

```go
// liteToolCtx 记忆工具上下文最小接口
type liteToolCtx interface {
	EnsureManager() bool
	IsClosed() bool
}
```

将 `ctx any` 改为 `ctx liteToolCtx`。

- [ ] **Step 2: 实现 Initialize 方法**

对齐 Python `MemberMemoryToolkit.initialize()`：

```go
func (t *MemberMemoryToolkit) Initialize(ctx context.Context) (bool, error) {
	// 1. 已初始化 + manager 未关闭 → 返回 true
	if t.initialized && t.manager != nil && !t.manager.IsClosed() {
		return true, nil
	}
	// 2. 检查 isMemoryEnabled
	if !lite.IsMemoryEnabled() {
		return false, nil
	}
	// 3. 构造 agentID = "{teamName}.{memberName}"
	agentID := fmt.Sprintf("%s.%s", t.teamName, t.memberName)
	// 4. 根据 scenario 确定 nodeName
	nodeName := "memory"
	if t.scenario == TeamScenarioCoding {
		nodeName = "coding_memory"
	}
	// 5. 获取 MemoryIndexManager
	memoryDir := ""
	if t.workspace != nil {
		if nodePath := t.workspace.GetNodePath(nodeName); nodePath != nil {
			memoryDir = *nodePath
		}
	}
	settings := lite.CreateMemorySettings(memoryDir, nil)
	params := lite.MemoryManagerParams{
		AgentID:         agentID,
		Workspace:       t.workspace,
		Settings:        settings,
		EmbeddingConfig: t.embeddingConfig,
		SysOperation:    t.sysOperation,
		NodeName:        nodeName,
	}
	mgr, err := lite.GetMemoryIndexManager(params)
	if err != nil || mgr == nil {
		return false, fmt.Errorf("初始化记忆管理器失败: %w", err)
	}
	t.manager = mgr
	// 6. 根据 scenario 创建上下文和工具
	if t.scenario == TeamScenarioCoding {
		toolCtx := lite.NewCodingMemoryToolContext().
			WithWorkspace(t.workspace).
			WithSettings(settings).
			WithAgentID(agentID).
			WithEmbeddingConfig(t.embeddingConfig).
			WithSysOperation(t.sysOperation).
			WithManager(mgr).
			WithCodingMemoryDir(memoryDir)
		t.ctx = toolCtx
		t.tools = createCodingMemoryTools(toolCtx, t.readOnly)
	} else {
		toolCtx := lite.NewMemoryToolContext().
			WithWorkspace(t.workspace).
			WithSettings(settings).
			WithAgentID(agentID).
			WithEmbeddingConfig(t.embeddingConfig).
			WithSysOperation(t.sysOperation).
			WithManager(mgr)
		t.ctx = toolCtx
		t.tools = createGeneralMemoryTools(toolCtx, t.readOnly)
	}
	t.initialized = true
	return true, nil
}
```

- [ ] **Step 3: 实现 GetTools / GetToolCards / Close**

```go
func (t *MemberMemoryToolkit) GetTools() []tool.Tool { return t.tools }

func (t *MemberMemoryToolkit) GetToolCards() []tool.ToolCard {
	cards := make([]tool.ToolCard, len(t.tools))
	for i, t := range t.tools {
		cards[i] = *t.Card()
	}
	return cards
}

func (t *MemberMemoryToolkit) Close(_ context.Context) error {
	if t.manager != nil {
		t.manager.Close()
	}
	t.ctx = nil
	t.tools = nil
	t.initialized = false
	return nil
}
```

- [ ] **Step 4: 添加 createCodingMemoryTools 和 createGeneralMemoryTools 辅助函数**

这两个函数对齐 Python 的 `_create_coding_tools` 和 `_create_general_tools`，内部调用 Task 2/3 的 `coding_memory.CreateCodingMemoryTools` 和 `memory.CreateMemoryTools`。

注意：MemberMemoryToolkit 的工具需要带 `teamName.memberName` 前缀的 ToolCard ID（对齐 Python `pfx = f"coding_memory.{toolkit.team_name}.{toolkit.member_name}"`）。

- [ ] **Step 5: 移除所有 ⤵️ 回填标记**

移除 `member_memory_toolkit.go` 中所有 `⤵️ 回填: 7.1`、`⤵️ 回填: 7.2`、`⤵️ 回填: 7.3` 标记。

- [ ] **Step 6: 运行编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agent_teams/memory/...`
Expected: 编译通过

- [ ] **Step 7: 提交**

```bash
git add internal/agent_teams/memory/member_memory_toolkit.go
git commit -m "feat: MemberMemoryToolkit 回填——ctx 类型具体化 + Initialize/GetTools/Close 实现"
```

---

### Task 7: 回填 code_agent_factory.go + code_adapter.go

**Files:**
- Modify: `internal/agentcore/harness/code_agent_factory.go`
- Modify: `internal/swarm/server/adapter/code_adapter.go`

- [ ] **Step 1: 修改 code_agent_factory.go**

1. 添加 import：`memoryrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/memory"`
2. 取消注释第 65-69 行，替换为真实实现：

```go
// 注入 CodingMemoryRail
if params.EmbeddingConfig != nil {
	codingMemoryDir := resolveCodingMemoryDir(params.Workspace)
	finalRails = append(finalRails, memoryrail.NewCodingMemoryRail(
		codingMemoryDir, params.EmbeddingConfig, language,
	))
}
```

3. 添加 `resolveCodingMemoryDir` 辅助函数：

```go
// resolveCodingMemoryDir 解析编程记忆目录路径
func resolveCodingMemoryDir(ws *workspace.Workspace) string {
	if ws == nil {
		return ""
	}
	if nodePath := ws.GetNodePath("coding_memory"); nodePath != nil {
		return *nodePath
	}
	return ""
}
```

4. 移除 `⤵️ 9.19-23` 标记

- [ ] **Step 2: 修改 code_adapter.go**

1. 添加 import：`memoryrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/memory"`
2. 实现 `buildCodingMemoryRail()`：

```go
func (c *CodeAdapter) buildCodingMemoryRail() sainterfaces.AgentRail {
	// 获取 embeddingConfig
	embCfg := c.getEmbeddingConfig()
	if embCfg == nil {
		return nil
	}
	// 获取 codingMemoryDir
	codingMemoryDir := ""
	if c.deep.workspace != nil {
		if nodePath := c.deep.workspace.GetNodePath("coding_memory"); nodePath != nil {
			codingMemoryDir = *nodePath
		}
	}
	language := c.resolveLanguage()
	return memoryrail.NewCodingMemoryRail(codingMemoryDir, embCfg, language)
}
```

3. 移除 `⤵️ 10.6.3-10: CodingMemoryRail` 标记

- [ ] **Step 3: 运行编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/harness/... && go build ./internal/swarm/server/adapter/...`
Expected: 编译通过

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/harness/code_agent_factory.go internal/swarm/server/adapter/code_adapter.go
git commit -m "feat: 回填 CodingMemoryRail 注入——code_agent_factory + code_adapter"
```

---

### Task 8: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 7.3 状态**

将 `| 7.3 | ☐ | CodingMemoryToolContext |` 改为 `| 7.3 | ✅ | CodingMemoryToolContext |`

- [ ] **Step 2: 更新 9.19-23 状态**

将 `| 9.19-23 | ☐ | 其他 Rails | Security(☐)/Interrupt(✅)/Skill(☐)/ContextEngine(✅)/Memory(☐)/Verification(⤴️9.29✅)/Subagent(⤴️9.29✅) Rails |` 改为 `| 9.19-23 | ☐ | 其他 Rails | Security(☐)/Interrupt(✅)/Skill(☐)/ContextEngine(✅)/Memory(✅)/Verification(⤴️9.29✅)/Subagent(⤴️9.29✅) Rails |`

- [ ] **Step 3: 更新 9.27 回填点**

将 `| 9.27 | ✅ | CodeAgent | 编码子 Agent（...⤵️ 9.19-23 CodingMemoryRail 回填） |` 改为 `| 9.27 | ✅ | CodeAgent | 编码子 Agent |`（移除 ⤵️ 回填标记）

- [ ] **Step 4: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 IMPLEMENTATION_PLAN.md — 7.3 ✅ + 9.19-23 Memory ✅"
```

---

### Task 9: 全量编译 + 测试验证

- [ ] **Step 1: 检查残留进程**

Run: `pgrep -f 'go (build|test)'`

如有残留，kill 后再继续。

- [ ] **Step 2: 全量编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./...`
Expected: 编译通过

- [ ] **Step 3: 运行相关测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/lite/... ./internal/agentcore/harness/tools/coding_memory/... ./internal/agentcore/harness/tools/memory/... ./internal/agentcore/harness/rails/memory/... ./internal/agent_teams/memory/... -v -count=1`
Expected: 全部 PASS

- [ ] **Step 4: 确认无 ⤵️ 回填标记残留**

Run: `grep -rn "⤵️ 回填: 7\." internal/agentcore/harness/ internal/agent_teams/memory/ internal/swarm/server/adapter/ || echo "无残留回填标记"`
Expected: 无残留

- [ ] **Step 5: 最终提交**

```bash
git add -A
git commit -m "chore: 7.3 + CodingMemoryRail + MemoryRail 实现完成"
```
