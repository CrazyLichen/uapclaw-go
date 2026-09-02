# 7.8 实现偏差修复设计

## 1. 概述

7.8 实现完成后审查发现 4 个偏差/问题，本设计覆盖全部修复：

1. **`runChecker` 签名 + `MemoryIndexManager.LLM()` 接口** — 启用 LLM 冗余判断
2. **`ListUserMem` 返回值** — 补齐 `user_id` / `scope_id`
3. **`llm any` → `*llm.Model`** — 消除 `any` 规避循环依赖
4. **残留回填标注** — 清理过期注释

---

## 2. 偏差 #1：`runChecker` 签名 + `MemoryIndexManager.LLM()` 接口

### 2.1 问题

`coding_memory_tool_ops.go` 中 `runChecker` 当前是空壳：

```go
func runChecker(newID string, newBody string, oldMemories map[string]string) []*update.MemoryActionItem {
    // 当前无 LLM 模型可用，返回 nil
    return nil
}
```

设计文档要求签名加 `ctx context.Context`，并通过 `MemoryIndexManager` 获取 LLM model（对齐 Python `ctx.manager.llm`）。

### 2.2 修复方案

#### 2.2.1 `MemoryIndexManager` 接口变更

文件：`lite/manager.go`

```go
type MemoryIndexManager interface {
    Initialize(ctx context.Context) error
    Sync(ctx context.Context, reason string, force bool) error
    Search(ctx context.Context, query string, opts map[string]any) ([]SearchResult, error)
    ReadFile(ctx context.Context, relPath string, fromLine *int, lines *int) (*ReadFileResult, error)
    Status() *StatusResult
    LLM() *llm.Model    // 替代 HasLLM()，对齐 Python ctx.manager.llm
    Close() error
}
```

- **删除** `HasLLM() bool`（被 `LLM() != nil` 替代）
- **新增** `LLM() *llm.Model`（返回 LLM 实例，nil 表示不可用）

需新增 import：`"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"`

#### 2.2.2 `memoryIndexManager` 实现变更

文件：`lite/manager_impl.go`

- 字段 `llm any` → `llm *llm.Model`
- 方法 `HasLLM() bool` → `LLM() *llm.Model`
- 删除 TODO 注释 `// TODO: 7.2 实现时替换为具体接口...`
- 新增 import：`"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"`

```go
// LLM 返回 LLM 实例。对齐 Python ctx.manager.llm
func (m *memoryIndexManager) LLM() *llm.Model {
    return m.llm
}
```

**循环依赖验证**：`lite → foundation/llm` 单向，`foundation/llm` 不导入 `memory`，无循环。

#### 2.2.3 `InitCodingMemoryManagerAsync` 参数变更

文件：`lite/tools.go`

- 参数 `llm any` → `llm *llm.Model`
- 删除类型断言 hack `if impl, ok := mgr.(*memoryIndexManager); ok && llm != nil { impl.llm = llm }`
- 改为通过接口方法设置：需在 `MemoryIndexManager` 接口或 `MemoryManagerParams` 上提供设置途径

**设计选择**：Python 中 `manager.llm = llm` 是直接赋值属性。Go 的接口不能赋值，有两条路：

- **选项 A**：接口加 `SetLLM(model *llm.Model)` — 对齐 Python 属性赋值语义
- **选项 B**：`MemoryManagerParams` 加 `LLM *llm.Model` 字段，构造时传入

选择 **选项 B**：`MemoryManagerParams` 加 `LLM` 字段。理由：
1. 构造时传入比构造后赋值更符合 Go 习惯
2. `SetLLM` 会让接口变胖，且只有 coding_memory 场景需要
3. `InitMemoryManagerAsync` 也可以受益（当前未传 llm，但未来可能需要）

```go
type MemoryManagerParams struct {
    AgentID         string
    Workspace       *workspace.Workspace
    Settings        *MemorySettings
    EmbeddingConfig *embedding.EmbeddingConfig
    SysOperation    sysop.SysOperation
    NodeName        string
    LLM             *llm.Model    // 新增，对齐 Python manager.llm
}
```

`getMemoryIndexManager` 构造时把 `params.LLM` 赋给 `memoryIndexManager.llm`，`InitCodingMemoryManagerAsync` 通过 `params.LLM = llm` 传入。

#### 2.2.4 `runChecker` 签名和实现变更

文件：`lite/coding_memory_tool_ops.go`

```go
// runChecker 调用 MemUpdateChecker 执行 LLM 冲突检测。
// 对齐 Python: _run_checker(coding_memory_manager, basename, body, old_memories)
func runChecker(ctx context.Context, manager MemoryIndexManager, newID string, newBody string, oldMemories map[string]string) []*update.MemoryActionItem {
    model := manager.LLM()
    if model == nil {
        return nil
    }
    checker := &update.MemUpdateChecker{}
    items, err := checker.Check(ctx, map[string]string{newID: newBody}, oldMemories, update.WithModel(model))
    if err != nil {
        logger.Warn(logComponent).Err(err).Str("new_id", newID).Msg("runChecker 冲突检查失败")
        return nil
    }
    return items
}
```

调用处变更（创建模式 + 追加模式）：

对齐 Python: `if old_memories and coding_memory_manager and coding_memory_manager.llm:`

```go
// 创建模式
if len(similarFiles) > 0 && toolCtx.Manager != nil && toolCtx.Manager.LLM() != nil {
    actions := runChecker(ctx, toolCtx.Manager, basename, body, similarFiles)
    if len(actions) > 0 && !containsActionForID(actions, basename) {
        return (&WriteResult{...Mode: WriteModeSkip...}).ToDict()
    }
    // 收集 MemoryStatusDelete 冲突
}

// 追加模式（prepareAppendMode）
if len(oldMemories) > 0 && toolCtx.Manager != nil && toolCtx.Manager.LLM() != nil {
    actions := runChecker(ctx, toolCtx.Manager, basename, body, oldMemories)
    // ...
}
```

#### 2.2.5 其他 `HasLLM()` 调用方迁移

搜索所有 `HasLLM()` 调用，改为 `LLM() != nil`。

---

## 3. 偏差 #2：`ListUserMem` 返回值

### 3.1 问题

Python `list_user_mem` 返回 `list[dict]` 含 `user_id`/`scope_id`，Go 返回 `[]*storeindex.MemoryDoc` 缺少这两个字段。

### 3.2 修复方案

新增 `ListUserMemResult` 结构体，嵌入 `MemoryDoc` 并补齐字段：

文件：`search/search_manager.go`

```go
// ListUserMemResult 分页列表结果。对齐 Python list_user_mem 返回的 dict
type ListUserMemResult struct {
    // UserID 用户 ID
    UserID string
    // ScopeID 范围 ID
    ScopeID string
    // Doc 记忆文档
    Doc *storeindex.MemoryDoc
}
```

方法签名变更：

```go
func (s *SearchManager) ListUserMem(ctx context.Context, userID string, scopeID string, nums int, pages int, memType string) ([]*ListUserMemResult, error)
```

实现中包装结果：

```go
docs, err := s.memoryIndex.ListMemories(ctx, userID, scopeID, start, nums, memTypes)
if err != nil {
    return nil, err
}
results := make([]*ListUserMemResult, 0, len(docs))
for _, doc := range docs {
    results = append(results, &ListUserMemResult{
        UserID:  userID,
        ScopeID: scopeID,
        Doc:     doc,
    })
}
return results, nil
```

---

## 4. 偏差 #3：`llm any` → `*llm.Model`

### 4.1 问题

`memoryIndexManager.llm` 字段和 `InitCodingMemoryManagerAsync` 参数使用 `any` 规避循环依赖。

### 4.2 修复方案

随偏差 #1 一并修复：
- `llm any` → `llm *llm.Model`（在 2.2.2 中已覆盖）
- `InitCodingMemoryManagerAsync` 参数 `llm any` → `llm *llm.Model`（在 2.2.3 中已覆盖）
- 删除类型断言 hack（在 2.2.3 中已覆盖）

---

## 5. 偏差 #4：残留回填标注

### 5.1 问题

`fragment_manager.go:450` 有过期标注 "7.8 回填时可能增加更多字段，届时评估是否引入 typed struct"。

### 5.2 修复方案

删除该行。7.8 已完成，`Fields` 保持 `map[string]any` 是最终决策。

---

## 6. 改动文件汇总

| 文件 | 修改 |
|------|------|
| `lite/manager.go` | 接口：`HasLLM()` → `LLM() *llm.Model`；`MemoryManagerParams` 加 `LLM` 字段；新增 import `foundation/llm` |
| `lite/manager_impl.go` | 字段 `llm any` → `llm *llm.Model`；方法 `HasLLM()` → `LLM()`；构造中 `params.LLM` 赋值；删除 TODO；新增 import |
| `lite/tools.go` | `InitCodingMemoryManagerAsync` 参数 `llm any` → `llm *llm.Model`；删除类型断言 hack；改为 `params.LLM = llm`；新增 import |
| `lite/coding_memory_tool_ops.go` | `runChecker` 加 `ctx` + `manager` 参数；实现 LLM 调用；创建/追加模式加守卫；import 变更 |
| `lite/manager_impl_test.go` | `HasLLM` 测试 → `LLM` 测试 |
| `search/search_manager.go` | 新增 `ListUserMemResult`；`ListUserMem` 返回类型变更 |
| `search/search_manager_test.go` | 适配 `ListUserMemResult` |
| `manage/index/fragment_manager.go` | 删除第 450 行残留标注 |
| `agent_teams/memory/member_memory_toolkit.go` | 无 `HasLLM()` 调用（已验证），无需修改 |

---

## 7. 测试策略

- `lite/manager_test.go`：验证 `LLM()` 返回值、`params.LLM` 传入
- `lite/coding_memory_tool_ops_test.go`：验证 `runChecker` 空模型返回 nil、有模型调用 Check
- `search/search_manager_test.go`：验证 `ListUserMemResult` 包含 UserID/ScopeID
