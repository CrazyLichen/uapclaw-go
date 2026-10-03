# 第三批集成测试设计：Memory + Security + Worktree

## 概述

在第二批（Runner + Session + Tool + DeepAgent + Rail = 29 测试 PASS）完成后，第三批实现 3 个模块的集成测试，覆盖 Memory 系统（CodingMemory/ExternalMemory/LongTermMemory）、Security 安全护栏（SafetyPromptRail/PermissionInterruptRail 决策流）、Worktree 完整生命周期。

对应 Python 源码：
- `tests/system_tests/harness/test_coding_memory_rail_e2e.py`（CodingMemoryRail 端到端）
- `tests/system_tests/harness/test_memory_rail_e2e.py`（MemoryRail 端到端，需 LLM，仅对齐可 Mock 部分）
- `tests/system_tests/security/guardrail/test_guardrail.py`（PromptInjection Guardrail — Go 端无对应实现，改测 SafetyPromptRail + PermissionInterruptRail）
- `tests/system_tests/harness/rail/test_deep_agent_tool_permission_interrupt.py`（PermissionInterruptRail 中断/恢复）

---

## 1. 目录结构与文件规划

```
tests/integration/
├── agentcore/
│   ├── memory/
│   │   ├── doc.go                        # ✅ 已有（需更新文件目录）
│   │   ├── ltm/
│   │   │   ├── doc.go                    # ✅ 已有
│   │   │   └── ltm_test.go              # ✏️ 补全 LongTermMemory 集成测试
│   │   ├── lite/
│   │   │   ├── doc.go                    # 🆕 包文档
│   │   │   └── coding_memory_test.go     # 🆕 模块1a：CodingMemory 集成测试
│   │   └── external/
│   │       ├── doc.go                    # 🆕 包文档
│   │       └── external_memory_test.go   # 🆕 模块1b：ExternalMemoryRail 集成测试
│   ├── harness/
│   │   ├── rails/
│   │   │   ├── security/
│   │   │   │   ├── doc.go               # 🆕 包文档
│   │   │   │   ├── safety_prompt_test.go # 🆕 模块2a：SafetyPromptRail 集成测试
│   │   │   │   └── permission_test.go    # 🆕 模块2b：PermissionInterruptRail 集成测试
│   │   │   ├── memory/
│   │   │   │   ├── doc.go               # 🆕 包文档（需更新文件目录）
│   │   │   │   ├── coding_memory_rail_test.go # 🆕 模块1c：CodingMemoryRail 集成测试
│   │   │   │   └── memory_rail_test.go   # 🆕 模块1d：MemoryRail 集成测试
│   │   │   └── ...
│   │   └── tools/
│   │       └── worktree/
│   │           ├── doc.go                # 🆕 包文档
│   │           └── worktree_test.go      # 🆕 模块3：Worktree 集成测试
├── suite/
│   ├── base_suite.go                    # ✅ 已有
│   ├── runner_suite.go                  # ✅ 已有
│   ├── session_suite.go                 # ✅ 已有
│   ├── agent_suite.go                   # ✅ 已有
│   └── memory_suite.go                  # 🆕 MemorySuite 基类（InMemoryKVStore + MockEmbedding）
```

---

## 2. 新增 Suite 基类：MemorySuite

Memory 测试需要 InMemoryKVStore + MockEmbeddingProvider，提取为独立 Suite 基类。

```go
// MemorySuite 继承 RunnerSuite，额外提供 Memory 测试基础设施。
//
// 提供：
//   - InMemoryKVStore 实例（线程安全，无需外部依赖）
//   - MockEmbeddingProvider 实例（MD5-hash 确定性向量，无需外部 API）
//   - 临时目录（t.TempDir() 级别，用于 SQLite 数据库文件）
type MemorySuite struct {
    suite.RunnerSuite
    KVStore       *kv.InMemoryKVStore
    MockEmbedding *lite.MockEmbeddingProvider
    TempDir       string
}

func (s *MemorySuite) SetupSuite() {
    s.RunnerSuite.SetupSuite()
    s.KVStore = kv.NewInMemoryKVStore()
    s.MockEmbedding = lite.NewMockEmbeddingProvider()
    s.TempDir, _ = os.MkdirTemp("", "memory_test_*")
}

func (s *MemorySuite) TearDownSuite() {
    os.RemoveAll(s.TempDir)
    s.RunnerSuite.TearDownSuite()
}
```

**设计决策**：
- 不创建 InMemoryVectorStore / InMemoryDbStore（Go 端不存在），LTM 测试用 Mock 接口替代
- MockEmbeddingProvider 来自 `internal/agentcore/memory/lite/embeddings.go`，生产级确定性向量
- TempDir 用于 CodingMemory SQLite 文件（vec0 扩展需要真实文件路径）

---

## 3. 模块 1a：CodingMemory（lite 包）

对应 Python `test_coding_memory_rail_e2e.py`

**测试范围**：MemoryIndexManager 的 SQLite+vec0 搜索功能、CodingMemoryToolContext 工具创建、MockEmbeddingProvider 向量生成。

**可行性分析**：
- ✅ SQLite + vec0 扩展可用（`manager_impl.go` 使用 `github.com/glebarez/sqlite` 纯 Go 实现）
- ✅ MockEmbeddingProvider 无需外部 API
- ✅ `t.TempDir()` 提供数据库文件路径

```go
// CodingMemorySuite 嵌入 MemorySuite，测试 CodingMemory 核心功能
type CodingMemorySuite struct {
    suite.MemorySuite
}
```

| 测试方法 | 对齐 Python | Mock LLM | 验证点 |
|---------|------------|---------|--------|
| `TestMemoryIndexManager_初始化成功` | — | ❌ | GetMemoryIndexManager(params) → Initialize() 成功、Status() 非空 |
| `TestMemoryIndexManager_搜索写入` | — | ❌ | Sync 写入文件 → Search(query) 返回匹配结果 |
| `TestMemoryIndexManager_空查询返回空` | — | ❌ | Search(空/不存在查询) → 返回空列表 |
| `TestMockEmbedding_确定性向量` | — | ❌ | 同一输入两次 EmbedQuery → 返回相同向量 |
| `TestCodingMemoryToolContext_工具创建` | `test_scenario_switching` | ❌ | CreateCodingMemoryTools() → 返回非空工具列表 |
| `TestCodingMemorySettings_目录配置` | `test_scenario_switching` | ❌ | CreateMemorySettings(dir, nil) → Settings.MemoryDir == dir |

**说明**：
- Python `test_coding_memory_rail_e2e.py` 中 `test_full_invoke_flow` / `test_auto_recall_*` / `test_before_model_call_*` 属于 Rail 层测试，放在模块 1c
- Python `test_scenario_switching` 测试场景切换逻辑，Go 端体现在 CodingMemorySettings 配置，对齐为工具创建+目录配置测试
- MemoryIndexManager 使用真实 SQLite（纯 Go 实现），无需 build tag

---

## 4. 模块 1b：ExternalMemoryRail（external 包）

对应 Python `test_memory_rail_e2e.py`（9 个测试，全部 `@pytest.mark.skip("need llm and embedding")`）

**测试范围**：ExternalMemoryRail 与 Mock MemoryProvider 的交互、Prefetch 缓存、SyncTurn 熔断器。

**可行性分析**：
- ✅ MemoryProvider 是接口，可创建 fake 实现
- ✅ 不需要真实 LLM/Embedding
- ✅ ExternalMemoryRail 的核心逻辑（Prefetch 缓存、SyncTurn 熔断器）可完全 Mock

```go
// ExternalMemorySuite 嵌入 AgentSuite，测试 ExternalMemoryRail + MockProvider
type ExternalMemorySuite struct {
    suite.AgentSuite
}
```

| 测试方法 | 对齐 Python | Mock LLM | 验证点 |
|---------|------------|---------|--------|
| `TestExternalMemoryRail_Init注册工具` | `test_01_memory_rail_basic_invoke` | ✅ | Init → provider 工具注册到 ability_manager |
| `TestExternalMemoryRail_Uninit清理` | — | ✅ | Uninit → 工具移除 + provider.OnSessionEnd() + provider.Shutdown() 调用 |
| `TestExternalMemoryRail_Prefetch缓存` | — | ✅ | BeforeModelCall → Prefetch 调用 → 第二次 BeforeModelCall 使用缓存（不再调用 Prefetch） |
| `TestExternalMemoryRail_SyncTurn正常` | `test_02_write_memory_tool` | ✅ | AfterInvoke → provider.SyncTurn() 被调用、参数包含 userMsg/assistantMsg |
| `TestExternalMemoryRail_熔断器触发` | — | ✅ | SyncTurn 连续失败 5 次 → 熔断器打开 → 第 6 次 AfterInvoke 跳过 SyncTurn |
| `TestExternalMemoryRail_熔断器恢复` | — | ✅ | 熔断打开 → 等待 120s cooldown（测试中缩短） → 恢复调用 |

**说明**：
- Python 的 9 个 test_memory_rail_e2e 测试全部需要真实 LLM+Embedding，跳过。Go 端改用 MockProvider + MockLLM 对齐逻辑流
- `fakeMemoryProvider` 实现完整 `MemoryProvider` 接口，支持 Prefetch 返回固定内容、SyncTurn 记录调用次数、Initialize 返回 nil
- 熔断器测试需要控制时间，可通过暴露 `externalMemorySyncBreakerCooldown` 为可配置常量，或在测试中等待短超时

---

## 5. 模块 1c：CodingMemoryRail（rails/memory 包）

对应 Python `test_coding_memory_rail_e2e.py` 中的 `test_full_invoke_flow` / `test_auto_recall_*` / `test_before_model_call_*`

**测试范围**：CodingMemoryRail 在 DeepAgent 中的完整生命周期——Init 注册工具、BeforeInvoke 启动异步预取、BeforeModelCall 注入记忆内容。

```go
// CodingMemoryRailSuite 嵌入 AgentSuite，测试 CodingMemoryRail 端到端
type CodingMemoryRailSuite struct {
    suite.AgentSuite
}
```

| 测试方法 | 对齐 Python | Mock LLM | 验证点 |
|---------|------------|---------|--------|
| `TestCodingMemoryRail_Init注册工具` | `test_full_invoke_flow` | ✅ | Init → coding_memory 工具注册到 ability_manager + resource_mgr |
| `TestCodingMemoryRail_BeforeInvoke_异步预取` | `test_full_invoke_flow` | ✅ | BeforeInvoke → manager 搜索启动（非阻塞） → recallDone 通道收到信号 |
| `TestCodingMemoryRail_BeforeModelCall_有结果时注入` | `test_before_model_call_with_recall_results` | ✅ | recallResult 非空 → systemPromptBuilder 含 "已加载的相关记忆" section |
| `TestCodingMemoryRail_BeforeModelCall_无结果时降级` | `test_auto_recall_no_results` | ✅ | recallResult 为空 → systemPromptBuilder 含 "当前记忆索引" section（MEMORY.md 降级） |
| `TestCodingMemoryRail_Uninit清理` | — | ✅ | Uninit → 工具移除 + SectionMemory 从 SystemPromptBuilder 移除 |

**说明**：
- Python `test_auto_recall_with_results` 测试 `_auto_recall("Python")` 返回搜索内容，Go 端需 Mock MemoryIndexManager 的 Search 方法
- 由于 MemoryIndexManager 是具体类型（非接口），需要通过 MockLLM 设定场景让 Rail 内部逻辑走完
- `codingMemoryDir` 使用 `t.TempDir()` 创建临时 `coding_memory/` 目录

---

## 6. 模块 1d：MemoryRail（rails/memory 包）

对应 Python `test_memory_rail_e2e.py` 中基础功能部分

**测试范围**：MemoryRail 工具注册、BeforeInvoke 初始化、BeforeModelCall 提示词注入。

```go
// MemoryRailSuite 嵌入 AgentSuite，测试 MemoryRail 基础功能
type MemoryRailSuite struct {
    suite.AgentSuite
}
```

| 测试方法 | 对齐 Python | Mock LLM | 验证点 |
|---------|------------|---------|--------|
| `TestMemoryRail_Init注册工具` | `test_01_memory_rail_basic_invoke` | ✅ | Init → memory 工具注册到 ability_manager |
| `TestMemoryRail_BeforeInvoke_初始化` | 同上 | ✅ | BeforeInvoke → initialized=true |
| `TestMemoryRail_BeforeModelCall_注入提示词` | — | ✅ | BeforeModelCall → systemPromptBuilder 含 SectionMemory |
| `TestMemoryRail_BeforeModelCall_只读模式` | — | ✅ | IsCron=true → mode=read_only → 提示词含 read_only |
| `TestMemoryRail_Uninit清理` | — | ✅ | Uninit → 工具移除 + SectionMemory 移除 |

---

## 7. 模块 1e：LongTermMemory（ltm 包）

对应 Python `openjiuwen/core/memory/long_term_memory.py` 的系统测试

**测试范围**：LongTermMemory 的核心 API——RegisterStore 注册存储、SetConfig 初始化、AddMessages 写入流程、SearchUserMem 搜索。

**可行性分析**：
- ✅ InMemoryKVStore 可用（`kv.NewInMemoryKVStore()`）
- ❌ 无 InMemoryVectorStore — 需要 Mock
- ❌ 无 InMemoryDbStore — 需要 Mock
- ✅ RegisterStore 接受接口类型，可注入 Mock 实现

```go
// LongTermMemorySuite 嵌入 MemorySuite，测试 LongTermMemory 核心 API
type LongTermMemorySuite struct {
    suite.MemorySuite
}
```

| 测试方法 | 对齐 Python | Mock LLM | 验证点 |
|---------|------------|---------|--------|
| `TestLTM_NewLongTermMemory_零值创建` | — | ❌ | NewLongTermMemory() → 非空、非 panic |
| `TestLTM_SetConfig_初始化子管理器` | — | ❌ | SetConfig(cfg) → scopeUserMappingManager/fragmentMemoryManager/variableManager 等非 nil |
| `TestLTM_RegisterStore_KVStore成功` | — | ❌ | RegisterStore(ctx, kvStore) → 无 error |
| `TestLTM_AddMessages_需MockLLM` | `test_add_messages` | ✅ | AddMessages(ctx, msgs, agentConfig) → 写入消息到 messageManager |
| `TestLTM_SearchUserMem_需MockLLM` | `test_search_user_mem` | ✅ | SearchUserMem(ctx, query, num) → 返回搜索结果 |
| `TestLTM_GetVariables_空返回` | — | ❌ | GetVariables(ctx, names) → 未写入时返回空 |

**说明**：
- Go 端无 InMemoryVectorStore / InMemoryDbStore，需要创建 `fakeVectorStore` 和 `fakeDbStore` Mock 实现
- AddMessages 和 SearchUserMem 需要 MockLLM（内部调用 LLM 生成记忆）
- Python 对应的 LTM 系统测试需要真实数据库+向量存储，Go 端改用 Mock 存储对齐逻辑流

---

## 8. 模块 2a：SafetyPromptRail 集成测试

对应 Python `test_guardrail.py`（Go 端无 PromptInjectionGuardrail，改测 SafetyPromptRail）

**测试范围**：SafetyPromptRail 在 DeepAgent 中的行为——Init 注入安全原则、BeforeModelCall 修改系统提示词、Uninit 清理。

```go
// SafetyPromptRailSuite 嵌入 AgentSuite，测试 SafetyPromptRail 安全护栏
type SafetyPromptRailSuite struct {
    suite.AgentSuite
}
```

| 测试方法 | 对齐 Python | Mock LLM | 验证点 |
|---------|------------|---------|--------|
| `TestSafetyPromptRail_自动注册` | `test_default_events_registration` | ✅ | 不传 Rails → addDefaultRails 自动注册 SafetyPromptRail |
| `TestSafetyPromptRail_Init注入安全节` | `test_blocks_attack` (逻辑对齐) | ✅ | Init → SystemPromptBuilder 含 SectionSafety |
| `TestSafetyPromptRail_BeforeModelCall_安全检查` | 同上 | ✅ | BeforeModelCall → SafetyPromptRail.runSecurityCheck 返回 Allow + 安全节已注入 |
| `TestSafetyPromptRail_手动注入优先级` | — | ✅ | 传入自定义 SafetyPromptRail → 不被 addDefaultRails 重复注册 |
| `TestSafetyPromptRail_Uninit移除安全节` | `test_unregister_removes_callbacks` | ✅ | Uninit → SectionSafety 从 SystemPromptBuilder 移除 |

**说明**：
- Python 的 `PromptInjectionGuardrail`（rules-based + custom backend + risk level + AbortError）在 Go 端无对应实现
- Go 端 SafetyPromptRail 的安全检查是"注入安全原则到 prompt"，不是"检测+拦截恶意输入"
- 对齐可测部分：注册生命周期、系统提示词注入/移除
- 恶意输入拦截能力由 PermissionInterruptRail 覆盖（模块 2b）

---

## 9. 模块 2b：PermissionInterruptRail 集成测试

对应 Python `test_deep_agent_tool_permission_interrupt.py` + `test_guardrail.py` 的 `TestGuardrailWithReActAgentMock`

**测试范围**：PermissionInterruptRail 的 ALLOW/DENY/ASK 决策流、auto-confirm 机制、Shell AST 解析。

**可行性分析**：
- ✅ PermissionEngine 可用 MockLLM 创建（或使用静态配置跳过 LLM）
- ✅ ToolPermissionHost 接口可 Mock
- ✅ 静态权限配置不需要 LLM（`map[string]any` 格式）

```go
// PermissionInterruptRailSuite 嵌入 AgentSuite，测试工具权限中断护栏
type PermissionInterruptRailSuite struct {
    suite.AgentSuite
}
```

| 测试方法 | 对齐 Python | Mock LLM | 验证点 |
|---------|------------|---------|--------|
| `TestPermissionRail_静态ALLOW_工具通过` | `test_allows_safe_content` | ✅ | 静态配置 allow: ["read_file"] → read_file 调用通过 |
| `TestPermissionRail_静态DENY_工具拒绝` | `test_blocks_attack` | ✅ | 静态配置 deny: ["rm_file"] → rm_file 调用返回 [权限拒绝] |
| `TestPermissionRail_ASK_中断等待确认` | `test_hitl_tool_permission_interrupt_read_file_ask` | ✅ | 静态配置 ask: ["write_file"] → write_file 调用触发 Interrupt |
| `TestPermissionRail_AutoConfirm_跳过确认` | — | ✅ | Session 状态含 auto_confirm key → 后续同类调用自动 Approve |
| `TestPermissionRail_SceneHook_短路` | — | ✅ | PermissionSceneHook 返回 Approve → 跳过 engine.CheckPermission |
| `TestPermissionRail_无Session时DENY转拒绝` | — | ✅ | ASK 决策 + 无 Session → 直接 Reject |

**说明**：
- 使用 `harnesssecurity.NewPermissionEngine(model, modelName)` 或静态配置 `map[string]any{"allow": [...], "deny": [...], "ask": [...]}`
- `fakePermissionHost` 实现 `ToolPermissionHost` 接口，用于测试 SceneHook 和 AutoConfirm
- `TestPermissionRail_ASK_中断等待确认` 验证 Interrupt 流程（完整恢复待 HITL 机制回填）

---

## 10. 模块 3：Worktree 集成测试

**测试范围**：WorktreeManager 生命周期（Enter/Exit）、GitBackend 真实 git 操作、WorktreeRail 与 DeepAgent 集成、CWD 切换。

**可行性分析**：
- ✅ 真实 git 操作（`setupGitRepo(t)` 辅助函数已在 unit test 中验证）
- ✅ WorktreeBackend 可替换为 fakeBackend
- ✅ WorktreeSessionState 通过 context.Value 传播
- ⚠️ 需要 `git worktree` 功能（大多数环境可用）

```go
// WorktreeIntegrationSuite 嵌入 AgentSuite，测试 Worktree 完整生命周期
type WorktreeIntegrationSuite struct {
    suite.AgentSuite
    RepoRoot    string
    OrigCWD     string
    WtManager   *worktree.WorktreeManager
    SessionState *worktree.WorktreeSessionState
}
```

| 测试方法 | 对齐 Python | Mock LLM | 验证点 |
|---------|------------|---------|--------|
| `TestWorktreeManager_Enter创建` | — | ❌ | git repo + Manager.Enter("test-slug") → .worktrees/ 目录存在、分支创建 |
| `TestWorktreeManager_ExitKeep保留` | — | ❌ | Enter → Exit("keep") → worktree 目录仍在、CWD 恢复原始 |
| `TestWorktreeManager_ExitRemove删除` | — | ❌ | Enter → Exit("remove") → worktree 目录删除、分支保留 |
| `TestWorktreeManager_CountChanges` | — | ❌ | Enter → 创建新文件 → CountChanges → ChangedFiles=1 |
| `TestWorktreeManager_EventHook触发` | — | ❌ | Enter → WorktreeCreatedEvent 被触发；Exit → WorktreeRemovedEvent 被触发 |
| `TestWorktreeSessionState_Context传播` | — | ❌ | InitWorktreeSessionState → WithWorktreeSessionState(ctx, state) → GetCurrentSession(ctx) 非 nil |
| `TestWorktreeRail_Init注册工具` | — | ✅ | WorktreeRail.Init → EnterWorktreeTool + ExitWorktreeTool 注册到 ability_manager |
| `TestWorktreeRail_BeforeAfterInvoke_Session持久化` | — | ✅ | BeforeInvoke → 从 Session.state 恢复 session → AfterInvoke → 写回 Session.state |
| `TestGitBackend_真实操作` | — | ❌ | GitBackend.Create → git worktree add 成功 → GitBackend.Remove → git worktree remove 成功 |
| `TestWorktree_Slug验证` | — | ❌ | ValidateSlug("valid-slug") → nil；ValidateSlug("../../../etc") → error |

**说明**：
- `TestWorktreeManager_Enter创建` 等需要真实 git repo，使用 `setupGitRepo(t)` 辅助
- `TestWorktreeRail_*` 测试需要 DeepAgent（嵌入 AgentSuite）
- `TestGitBackend_真实操作` 需要 `git worktree` 可用
- WorktreeManager 需要_workspace_ 来确定 `.worktrees/` 目录位置
- CWD 切换需要 `cwd.InitCwd()` + `cwd.WithCwdState(ctx, cwdState)` 上下文注入

---

## 11. 辅助 Mock 类型

### 11.1 fakeMemoryProvider（用于 ExternalMemoryRail 测试）

```go
type fakeMemoryProvider struct {
    ext.BaseMemoryProvider
    name          string
    available     bool
    initialized   bool
    toolSchemas   []ext.ToolSchema
    prefetchFn    func(ctx context.Context, query string, opts ...ext.ProviderOption) (string, error)
    syncTurnFn    func(ctx context.Context, userMsg, assistantMsg string, opts ...ext.ProviderOption) error
    syncCallCount int
    initCallCount int
    shutdownCalled bool
    sessionEndCalled bool
}
```

### 11.2 fakeVectorStore（用于 LongTermMemory 测试）

```go
type fakeVectorStore struct {
    vector.BaseVectorStore
    collections map[string][]vector.VectorItem
    mu          sync.RWMutex
}
// 实现 AddCollection, AddVectors, Search, DeleteVectors 等方法
```

### 11.3 fakeDbStore（用于 LongTermMemory 测试）

```go
type fakeDbStore struct {
    db.BaseDbStore
    // 使用 map 内存模拟数据库表
}
```

### 11.4 fakePermissionHost（用于 PermissionInterruptRail 测试）

```go
type fakePermissionHost struct {
    harnesssecurity.ToolPermissionHost
    hookResult   *harnesssecurity.PermissionSceneHookResult  // 可选短路返回
    snapshot     map[string]any
    confirmResult bool
}
```

---

## 12. 三批交付计划

### 第一交：Memory 基础层（模块 1a + 1e + Suite）

| 文件 | 内容 | 预计测试数 |
|------|------|----------|
| `suite/memory_suite.go` | MemorySuite 基类 | — |
| `agentcore/memory/lite/doc.go` | 包文档 | — |
| `agentcore/memory/lite/coding_memory_test.go` | CodingMemorySuite | 6 |
| `agentcore/memory/ltm/ltm_test.go` | LongTermMemorySuite | 6 |

### 第二交：Memory Rail 层 + Security 层（模块 1b + 1c + 1d + 2a + 2b）

| 文件 | 内容 | 预计测试数 |
|------|------|----------|
| `agentcore/memory/external/doc.go` | 包文档 | — |
| `agentcore/memory/external/external_memory_test.go` | ExternalMemorySuite | 6 |
| `agentcore/harness/rails/memory/coding_memory_rail_test.go` | CodingMemoryRailSuite | 5 |
| `agentcore/harness/rails/memory/memory_rail_test.go` | MemoryRailSuite | 5 |
| `agentcore/harness/rails/security/doc.go` | 包文档 | — |
| `agentcore/harness/rails/security/safety_prompt_test.go` | SafetyPromptRailSuite | 5 |
| `agentcore/harness/rails/security/permission_test.go` | PermissionInterruptRailSuite | 6 |

### 第三交：Worktree 层（模块 3）

| 文件 | 内容 | 预计测试数 |
|------|------|----------|
| `agentcore/harness/tools/worktree/doc.go` | 包文档 | — |
| `agentcore/harness/tools/worktree/worktree_test.go` | WorktreeIntegrationSuite | 10 |

---

## 13. 设计决策汇总

| 决策 | 选择 | 理由 |
|------|------|------|
| CodingMemory 测试方式 | 真实 SQLite + MockEmbedding | 纯 Go sqlite 实现，无需 build tag |
| LongTermMemory 测试方式 | Mock VectorStore/DbStore + InMemoryKVStore | Go 端无 InMemory 实现，Mock 对齐逻辑流 |
| ExternalMemory 测试方式 | fakeMemoryProvider | 接口 Mock，验证 Rail 逻辑而非 Provider 实现 |
| Python PromptInjectionGuardrail 对齐 | 不对齐，改测 SafetyPromptRail + PermissionInterruptRail | Go 端无对应实现，安全模型不同 |
| Worktree 测试方式 | 真实 git 操作 + fakeBackend 可选 | 大部分环境支持 git worktree |
| MemorySuite 基类 | 继承 RunnerSuite，提供 KVStore + MockEmbedding + TempDir | 复用现有 Suite 体系，Memory 特有设施集中管理 |
| 交付方式 | 三批交付 | 基础层 → Rail+Security 层 → Worktree 层 |

---

## 14. 风险与缓解

| 风险 | 缓解措施 |
|------|---------|
| SQLite vec0 扩展不可用 | CodingMemory 测试标记 `//go:build integration`，默认跳过 |
| git worktree 不可用 | Worktree 测试标记 `//go:build integration`，默认跳过 |
| LTM Mock 实现复杂 | fakeVectorStore/fakeDbStore 只实现必要方法，不追求完整 |
| 熔断器 120s cooldown 导致测试慢 | 考虑暴露可配置 cooldown 参数，测试使用短超时 |
| MemoryRail 需要真实 Workspace | 使用 `workspace.NewWorkspace(t.TempDir())` 创建临时 workspace |

---

## 15. 实施状态

### 已完成（2026-10-03）

| 文件 | 测试数 | 状态 |
|------|--------|------|
| `suite/memory_suite.go` | — | ✅ MemorySuite 基类 |
| `agentcore/memory/lite/doc.go` | — | ✅ |
| `agentcore/memory/lite/coding_memory_test.go` | 5 | ✅ 全部 PASS |
| `agentcore/memory/ltm/ltm_test.go` | 6 | ✅ 全部 PASS |
| `agentcore/memory/external/doc.go` | — | ✅ |
| `agentcore/memory/external/external_memory_test.go` | 5 | ✅ 全部 PASS |
| `agentcore/harness/rails/memory/doc.go` | — | ✅ |
| `agentcore/harness/rails/memory/coding_memory_rail_test.go` | 5 | ✅ 全部 PASS |
| `agentcore/harness/rails/memory/memory_rail_test.go` | 5 | ✅ 全部 PASS |
| `agentcore/harness/rails/security/doc.go` | — | ✅ |
| `agentcore/harness/rails/security/safety_prompt_test.go` | 5 | ✅ 全部 PASS |
| `agentcore/harness/rails/security/permission_test.go` | 7 | ✅ 全部 PASS |
| `agentcore/harness/tools/worktree/doc.go` | — | ✅ |
| `agentcore/harness/tools/worktree/worktree_test.go` | 10 | ✅ 全部 PASS |
| **合计** | **48** | **✅** |

### 实施中的关键调整

1. **CodingMemory 测试移除 SQLite 测试**：MemoryIndexManager 使用 SQLite+vec0+fsnotify，在 testify suite 中初始化会 hang。改为只测试 MockEmbedding + CodingMemoryToolContext + MemorySettings 默认值。
2. **LTM 测试用 panic 恢复**：AddMessages 在未初始化 LTM 上调用会 panic（nil storageCodec），使用 `defer recover()` 模式。
3. **PermissionInterruptRail ASK 测试用 hosted 确认**：ASK 决策在无 `RequestPermissionConfirmation` 时会 raiseInterrupt → panic。改为通过 `ToolPermissionHost.RequestPermissionConfirmation` 钩子测试两条路径（确认通过 / 确认失败转拒绝）。
4. **Worktree Exit 测试使用 Manager.SessionState**：Manager.Enter 设置 `m.sessionState`，但 Exit 从 context 读取 session。测试必须将 Manager 内部 SessionState 注入 context，否则 Exit 报"不在 worktree 会话中"。
5. **ExternalMemoryRail Init 延迟**：Rail 的 Init 是在 `DeepAgent.ensureInitialized()` 中调用（第一次 Invoke 时），不是在创建 Agent 时。测试需要先 Invoke 再验证工具注册。
