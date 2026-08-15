# 7.3 CodingMemoryToolContext + CodingMemoryRail + MemoryRail 设计

> 合并实现：7.3（CodingMemoryToolContext 正式接入）+ 9.19-23（CodingMemoryRail + MemoryRail）+ 回填点

## 1. 背景与动机

### 1.1 当前状态

- **7.1 CodingMemoryManager** ✅ 已完成 — `MemoryIndexManager` 接口 + SQLite+vec0+FTS5 实现
- **7.2 CodingMemoryTools** ✅ 已完成 — `coding_memory_tool_ops.go` 的 read/write/edit 操作
- **7.3 CodingMemoryToolContext** ☐ 骨架存在但未被消费 — `CodingMemoryToolContext` 结构体已定义，但 `MemberMemoryToolkit.ctx` 类型为 `any`，`Initialize()` 返回 false
- **9.19-23 CodingMemoryRail** ☐ 未实现 — `harness/rails/` 下无 memory 相关文件
- **MemoryRail（通用版）** ☐ 未实现 — Python 中与 CodingMemoryRail 并列的兄弟 Rail

### 1.2 完成标准

7.3 的真正完成标准不是"结构体定义存在"，而是**让 CodingMemoryToolContext 被实际消费**：

- **路径 A（单 Agent）**：CodingMemoryRail → 创建 Context → 传给 3 个 Tool → 传给 Ops
- **路径 B（Agent Team）**：MemberMemoryToolkit → scenario=="coding" 时创建 Context → 传给工具

### 1.3 在 Agent 会话流程中的位置

```
用户提问 → Agent 思考 → 调用工具 → 返回结果
              ↑
              │
    ┌─────────┴──────────┐
    │  CodingMemoryRail  │ ← 9.19-23
    │  BeforeInvoke()    │ → 初始化 CodingMemoryToolContext + MemoryIndexManager
    │  BeforeModelCall() │ → 召回记忆注入 system prompt
    └─────────┬──────────┘
              │ 创建
              ▼
    ┌─────────────────────────┐
    │  CodingMemoryToolContext │ ← 7.3
    │  状态聚合容器            │
    │  - Workspace             │
    │  - Settings              │
    │  - AgentID               │
    │  - EmbeddingConfig       │
    │  - SysOperation          │
    │  - Manager               │
    │  - CodingMemoryDir       │ ← 区别于 MemoryToolContext
    │  - NodeName              │
    └─────────┬────────────────┘
              │ 传递给
       ┌──────┼──────┐
       ▼      ▼      ▼
    Read   Write   Edit   ← 7.2 工具操作（已完成）
```

## 2. 文件目录结构（对齐 Python）

```
Python                                           Go
──────────────────────────────────────────────────────────────────────────
harness/tools/coding_memory.py            →  harness/tools/coding_memory/
                                              ├── doc.go
                                              └── coding_memory_tool.go

harness/tools/memory.py                   →  harness/tools/memory/
                                              ├── doc.go
                                              └── memory_tool.go

harness/rails/memory/                     →  harness/rails/memory/
├── __init__.py                            ├── doc.go
├── coding_memory_rail.py                 ├── coding_memory_rail.go
├── memory_rail.py                        └── memory_rail.go
└── external_memory_rail.py               （external_memory_rail 留到后续）
```

## 3. 新建文件清单

| # | Go 文件路径 | 内容 | Python 对齐 |
|---|------------|------|------------|
| 1 | `harness/tools/coding_memory/doc.go` | 包文档 | — |
| 2 | `harness/tools/coding_memory/coding_memory_tool.go` | 3 个 Tool 结构体 + `CreateCodingMemoryTools` | `harness/tools/coding_memory.py` |
| 3 | `harness/tools/memory/doc.go` | 包文档 | — |
| 4 | `harness/tools/memory/memory_tool.go` | 5 个 Tool 结构体 + `CreateMemoryTools` | `harness/tools/memory.py` |
| 5 | `harness/rails/memory/doc.go` | 包文档 | — |
| 6 | `harness/rails/memory/coding_memory_rail.go` | CodingMemoryRail 完整实现 | `harness/rails/memory/coding_memory_rail.py` |
| 7 | `harness/rails/memory/memory_rail.go` | MemoryRail 完整实现 | `harness/rails/memory/memory_rail.py` |

## 4. 修改文件清单

| # | Go 文件路径 | 修改内容 |
|---|------------|---------|
| 1 | `memory/lite/coding_memory_tool_context.go` | 添加 WithXxx Builder 方法 |
| 2 | `memory/lite/tool_context.go` | 添加 WithXxx Builder 方法 |
| 3 | `memory/lite/tool_context_base.go` | 添加 WithXxx Builder 方法 |
| 4 | `agent_teams/memory/member_memory_toolkit.go` | `ctx any` → 接口类型，实现 Initialize/GetTools/Close |
| 5 | `harness/code_agent_factory.go` | 取消注释 + 注入 CodingMemoryRail |
| 6 | `swarm/server/adapter/code_adapter.go` | `buildCodingMemoryRail()` 返回真实实例 |
| 7 | `IMPLEMENTATION_PLAN.md` | 更新 7.3、9.19-23 状态 |

## 5. 详细设计

### 5.1 CodingMemoryToolContext 完善

当前 Go 实现只有 `NewCodingMemoryToolContext()` 返回空壳。添加 Builder 方法：

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

同样给 `MemoryToolContext` 和 `LiteMemoryToolContextBase` 添加对应的 WithXxx 方法。

### 5.2 coding_memory Tool 结构体

文件：`harness/tools/coding_memory/coding_memory_tool.go`

对齐 Python `harness/tools/coding_memory.py`，每个 Tool 持有 `*CodingMemoryToolContext`：

```go
package coding_memory

// CodingMemoryReadTool 编程记忆读取工具。对齐 Python CodingMemoryReadTool
type CodingMemoryReadTool struct {
    card *tool.ToolCard
    ctx  *lite.CodingMemoryToolContext
}

// CodingMemoryWriteTool 编程记忆写入工具。对齐 Python CodingMemoryWriteTool
type CodingMemoryWriteTool struct {
    card *tool.ToolCard
    ctx  *lite.CodingMemoryToolContext
}

// CodingMemoryEditTool 编程记忆编辑工具。对齐 Python CodingMemoryEditTool
type CodingMemoryEditTool struct {
    card *tool.ToolCard
    ctx  *lite.CodingMemoryToolContext
}

// CreateCodingMemoryTools 创建编程记忆工具集。
// 对齐 Python create_coding_memory_tools(ctx, language, agent_id)
func CreateCodingMemoryTools(ctx *lite.CodingMemoryToolContext, language string, agentID string) []tool.Tool
```

每个 Tool 的 `Invoke()` 调用对应的 `coding_memory_tool_ops.XxxWithContext`，`Stream()` 返回 `ErrStreamNotSupported`。

`CreateCodingMemoryTools` 对齐 Python 逻辑：
1. 如果 `ctx.Workspace != nil`，设置 `CodingMemoryDir` 和 `Settings`
2. 设置 `NodeName = "coding_memory"`
3. 创建 3 个 Tool 并返回

### 5.3 memory Tool 结构体

文件：`harness/tools/memory/memory_tool.go`

对齐 Python `harness/tools/memory.py`，每个 Tool 持有 `*MemoryToolContext`：

```go
package memory

// MemorySearchTool 记忆搜索工具。对齐 Python MemorySearchTool
type MemorySearchTool struct { card *tool.ToolCard; ctx *lite.MemoryToolContext }

// MemoryGetTool 记忆获取工具。对齐 Python MemoryGetTool
type MemoryGetTool struct { card *tool.ToolCard; ctx *lite.MemoryToolContext }

// ReadMemoryTool 记忆读取工具。对齐 Python ReadMemoryTool
type ReadMemoryTool struct { card *tool.ToolCard; ctx *lite.MemoryToolContext }

// WriteMemoryTool 记忆写入工具。对齐 Python WriteMemoryTool
type WriteMemoryTool struct { card *tool.ToolCard; ctx *lite.MemoryToolContext }

// EditMemoryTool 记忆编辑工具。对齐 Python EditMemoryTool
type EditMemoryTool struct { card *tool.ToolCard; ctx *lite.MemoryToolContext }

// CreateMemoryTools 创建通用记忆工具集。
// 对齐 Python create_memory_tools(ctx, language, agent_id)
// 注意：Python 的 readOnly 逻辑在 MemberMemoryToolkit._create_general_tools 中，
// CreateMemoryTools 本身始终返回全部 5 个工具。
// readOnly 模式通过 BeforeModelCall 的 prompt 注入控制，而非工具裁剪。
func CreateMemoryTools(ctx *lite.MemoryToolContext, language string, agentID string) []tool.Tool
```

对齐 Python `create_memory_tools`：始终返回 5 个 Tool（search/get/write/edit/read）。

### 5.4 CodingMemoryRail

文件：`harness/rails/memory/coding_memory_rail.go`

对齐 Python `harness/rails/memory/coding_memory_rail.py`。

```go
package memory

// recallResult 预取结果
type recallResult struct {
    content string  // 召回的 markdown 内容
    total   int     // 总记忆数
}

// CodingMemoryRail 编程记忆护栏。
// 对齐 Python CodingMemoryRail (coding_memory_rail.py)
type CodingMemoryRail struct {
    rails.DeepAgentRail
    // codingMemoryDir 编程记忆目录
    codingMemoryDir string
    // embeddingConfig 嵌入配置
    embeddingConfig *embedding.EmbeddingConfig
    // language 语言
    language string
    // manager 记忆索引管理器
    manager lite.MemoryIndexManager
    // managerInitialized 管理器是否已初始化
    managerInitialized bool
    // toolCtx 工具上下文
    toolCtx *lite.CodingMemoryToolContext
    // systemPromptBuilder 系统提示词构建器引用
    systemPromptBuilder saprompt.SystemPromptBuilderInterface
    // agentID Agent 标识
    agentID string
    // 召回状态
    recallResult *recallResult
    recallDone   chan struct{}
    // 工具注册
    ownedToolNames map[string]struct{}
    ownedToolIDs   map[string]struct{}
}

// NewCodingMemoryRail 创建编程记忆护栏
func NewCodingMemoryRail(codingMemoryDir string, embeddingConfig *embedding.EmbeddingConfig, language string) *CodingMemoryRail

// Priority 返回优先级（80，与 Python priority=80 对齐）
func (r *CodingMemoryRail) Priority() int

// Init 注册工具。对齐 Python CodingMemoryRail.init()
func (r *CodingMemoryRail) Init(agent agentinterfaces.BaseAgent) error

// Uninit 注销工具。对齐 Python CodingMemoryRail.uninit()
func (r *CodingMemoryRail) Uninit(agent agentinterfaces.BaseAgent) error

// BeforeInvoke 初始化 Manager + 启动预取。对齐 Python CodingMemoryRail.before_invoke()
func (r *CodingMemoryRail) BeforeInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error

// BeforeModelCall 注入记忆节到 system prompt。对齐 Python CodingMemoryRail.before_model_call()
func (r *CodingMemoryRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error

// GetCallbacks 声明 4 个钩子
func (r *CodingMemoryRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc
```

#### 5.4.1 Init 流程（对齐 Python `_register_coding_memory_tools`）

```
1. 获取 agentID（从 agent.Card().ID）
2. 获取 language（从 systemPromptBuilder）
3. 创建 CodingMemoryToolContext（WithWorkspace/WithSettings/WithAgentID/...）
4. 调用 CreateCodingMemoryTools(ctx, language, agentID)
5. 恢复 CodingMemoryDir（对齐 Python 注释：create_coding_memory_tools 会覆盖 coding_memory_dir）
6. 遍历 tools，注册到 resourceMgr + abilityManager
7. 记录 ownedToolNames + ownedToolIDs
```

#### 5.4.2 BeforeInvoke 流程（对齐 Python `before_invoke`）

```
1. 首次初始化 Manager（调用 InitCodingMemoryManagerAsync）
2. 设置 managerInitialized = true
3. 重置召回状态（recallResult = nil, recallDone = new chan）
4. 检查是否只读模式（cron/heartbeat）
5. 提取用户查询
6. 非只读 && 有 manager && 有查询 → 启动 goroutine 执行 autoRecall
```

#### 5.4.3 autoRecall 流程（对齐 Python `_auto_recall`）

```
1. 执行 manager.Search(ctx, query, opts{maxResults: 5})
2. 统计总记忆数（countMemoryFiles）
3. 遍历结果：
   a. 跳过 MEMORY.md
   b. 读取文件内容（通过 SysOperation）
   c. 解析 frontmatter + 提取 body
   d. 检查大小限制（MAX_RECALL_TOTAL_BYTES = 10KB）
   e. 组装 "### {title} [{path}] (updated: {date})\n\n{body}"
4. 拼接结果（用 "---" 分隔）
5. 写入 recallResult + 发送 recallDone 信号
```

#### 5.4.4 BeforeModelCall 流程（对齐 Python `before_model_call`）

```
1. 获取 systemPromptBuilder，移除旧 "memory" section
2. 构建基础 section（调用 BuildCodingMemorySection）
3. 只读模式 → 仅注入 MEMORY.md 索引
4. 非阻塞检查预取结果：
   a. recallDone 已关闭 → 使用 recallResult
   b. recallDone 未关闭 → 本次降级
5. 互斥注入：
   a. 有召回 → 注入全文 + "（共 N 条记忆，用 coding_memory_read 读取其他。）"
   b. 无召回 → 降级注入 MEMORY.md 索引
6. 添加 section 到 systemPromptBuilder
```

### 5.5 MemoryRail

文件：`harness/rails/memory/memory_rail.go`

对齐 Python `harness/rails/memory/memory_rail.py`。比 CodingMemoryRail 更简单（无 auto_recall）。

```go
package memory

// MemoryRail 通用记忆护栏。
// 对齐 Python MemoryRail (memory_rail.py)
type MemoryRail struct {
    rails.DeepAgentRail
    // embeddingConfig 嵌入配置
    embeddingConfig *embedding.EmbeddingConfig
    // isProactive 是否主动模式
    isProactive bool
    // initialized 是否已初始化
    initialized bool
    // managerInitialized 管理器是否已初始化
    managerInitialized bool
    // toolCtx 工具上下文
    toolCtx *lite.MemoryToolContext
    // systemPromptBuilder 系统提示词构建器引用
    systemPromptBuilder saprompt.SystemPromptBuilderInterface
    // isReadOnly 是否只读模式
    isReadOnly bool
    // 工具注册
    ownedToolNames map[string]struct{}
    ownedToolIDs   map[string]struct{}
}

// NewMemoryRail 创建通用记忆护栏
func NewMemoryRail(embeddingConfig *embedding.EmbeddingConfig, isProactive bool) *MemoryRail

// Priority 返回优先级（80）
func (r *MemoryRail) Priority() int

// Init 注册工具。对齐 Python MemoryRail.init()
func (r *MemoryRail) Init(agent agentinterfaces.BaseAgent) error

// Uninit 注销工具。对齐 Python MemoryRail.uninit()
func (r *MemoryRail) Uninit(agent agentinterfaces.BaseAgent) error

// BeforeInvoke 初始化 Manager。对齐 Python MemoryRail.before_invoke()
func (r *MemoryRail) BeforeInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error

// BeforeModelCall 注入记忆节。对齐 Python MemoryRail.before_model_call()
func (r *MemoryRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error

// GetCallbacks 声明 4 个钩子
func (r *MemoryRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc
```

#### 5.5.1 Init 流程（对齐 Python `_register_memory_tools`）

```
1. 获取 agentID + language
2. 创建 MemoryToolContext（WithWorkspace/WithSettings/WithAgentID/...）
3. 调用 CreateMemoryTools(ctx, language, agentID)
4. 遍历 tools，注册到 resourceMgr + abilityManager
5. 记录 ownedToolNames + ownedToolIDs
```

#### 5.5.2 BeforeInvoke 流程（对齐 Python `before_invoke`）

```
1. 首次初始化 Manager（调用 InitMemoryManagerAsync）
2. 设置 managerInitialized = true
3. 检查是否只读模式（cron/heartbeat）
```

#### 5.5.3 BeforeModelCall 流程（对齐 Python `before_model_call`）

```
1. 获取 systemPromptBuilder，移除旧 "memory" section
2. 根据 isReadOnly + isProactive 选择 mode
3. 调用 BuildMemorySection(mode, todayDate, language)
4. 添加 section 到 systemPromptBuilder
```

### 5.6 MemberMemoryToolkit 回填

文件：`agent_teams/memory/member_memory_toolkit.go`

```go
// liteToolCtx 记忆工具上下文最小接口
type liteToolCtx interface {
    EnsureManager() bool
    IsClosed() bool
}

type MemberMemoryToolkit struct {
    // ...
    ctx    liteToolCtx  // 替换 any
    tools  []tool.Tool
    // ...
}

// Initialize 真实实现。对齐 Python MemberMemoryToolkit.initialize()
func (t *MemberMemoryToolkit) Initialize(ctx context.Context) (bool, error) {
    // 1. 检查是否已初始化（manager 非空且未关闭）
    // 2. 检查 isMemoryEnabled
    // 3. 构造 agentID = "{teamName}.{memberName}"
    // 4. 根据 scenario 确定 nodeName（"coding_memory" 或 "memory"）
    // 5. 获取 MemoryIndexManager
    // 6. scenario == "coding" → CodingMemoryToolContext + CreateCodingMemoryTools
    //    scenario == "general" → MemoryToolContext + CreateMemoryTools
    // 7. 标记 initialized = true
}

// GetTools 返回工具列表
func (t *MemberMemoryToolkit) GetTools() []tool.Tool { return t.tools }

// GetToolCards 返回工具卡片列表
func (t *MemberMemoryToolkit) GetToolCards() []tool.ToolCard { ... }

// Close 关闭工具集
func (t *MemberMemoryToolkit) Close(_ context.Context) error {
    // 1. 关闭 manager
    // 2. 清理 ctx + tools
    // 3. 标记 initialized = false
}
```

### 5.7 回填点

#### 5.7.1 code_agent_factory.go

```go
// 取消注释，注入 CodingMemoryRail
if params.EmbeddingConfig != nil {
    codingMemoryDir := resolveCodingMemoryDir(params.Workspace)
    finalRails = append(finalRails, memory.NewCodingMemoryRail(
        codingMemoryDir, params.EmbeddingConfig, language,
    ))
}
```

新增 `resolveCodingMemoryDir` 辅助函数：
```go
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

#### 5.7.2 code_adapter.go

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
    return memory.NewCodingMemoryRail(codingMemoryDir, embCfg, language)
}
```

## 6. Go 与 Python 的异步模型映射

| Python | Go |
|--------|-----|
| `asyncio.create_task(self._auto_recall(query))` | `go func() { r.autoRecall(ctx, query); r.recallDone <- struct{}{} }()` |
| `self._prefetch_task.done()` | `select { case <-r.recallDone: ... default: ... }` |
| `await self._manager.search(query, opts)` | `r.manager.Search(ctx, query, opts)` |
| `await self.sys_operation.fs().read_file(path)` | `r.SysOperation().FS().ReadFile(ctx, path)` |

并发安全：`r.recallResult` 只在 `BeforeModelCall` 中读取，且 `select` 的 happens-before 保证安全。

## 7. 测试计划

### 7.1 单元测试

| 文件 | 测试内容 |
|------|---------|
| `coding_memory_tool_context_test.go` | WithXxx Builder 方法 |
| `coding_memory_tool_test.go` | 3 个 Tool 的 Invoke 参数校验 + 调用 ops |
| `memory_tool_test.go` | 5 个 Tool 的 Invoke 参数校验 + 调用 ops |
| `coding_memory_rail_test.go` | Init/Uninit/BeforeInvoke/BeforeModelCall |
| `memory_rail_test.go` | Init/Uninit/BeforeInvoke/BeforeModelCall |
| `member_memory_toolkit_test.go` | Initialize 两个场景 + GetTools/Close |

### 7.2 Mock 策略

- `MemoryIndexManager` → 通过接口 mock
- `SysOperation` → 通过接口 mock
- `workspace.Workspace` → 使用真实 workspace（`t.TempDir()`）
- `SystemPromptBuilderInterface` → 通过接口 mock

## 8. IMPLEMENTATION_PLAN.md 状态更新

| 步骤 | 当前 | 目标 |
|------|------|------|
| 7.3 CodingMemoryToolContext | ☐ | ✅ |
| 9.19-23 其他 Rails Memory(☐) | ☐ | ✅（CodingMemoryRail + MemoryRail） |
| 9.27 CodeAgent ⤵️ 9.19-23 | ✅（带回填点） | ✅（回填完成） |
| 10.6.3-10 ⤵️ CodingMemoryRail | 回填点 | ✅（回填完成） |
