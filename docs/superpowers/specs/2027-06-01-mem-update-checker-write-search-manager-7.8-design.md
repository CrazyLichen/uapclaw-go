# 7.8 WriteManager / SearchManager / MemUpdateChecker 实现设计

## 1. 概述

本设计覆盖 `IMPLEMENTATION_PLAN.md` 中 7.8 章节的三个组件：
- **MemUpdateChecker** — LLM 驱动的记忆冲突检查器（当前 stub，需回填完整逻辑）
- **WriteManager** — 写入操作统一路由器（新实现）
- **SearchManager** — 搜索操作统一路由器（新实现）

按已确认的实现顺序：**先 MemUpdateChecker，后 WriteManager + SearchManager**。

### 1.1 在 Agent 会话流程中的位置

```
Agent 会话 → LongTermMemory.add_messages()
               │
               ├── Generator.gen_all_memory()     ← LLM 提取记忆（7.18）
               │   └── SearchManager.search()     ← 搜索旧记忆做语义验证
               │
               └── WriteManager.add_memories()     ← 统一写入路由
                   ├── FragmentMemoryManager.add_memories()
                   │   ├── getRelatedOldMemories()        ← 搜索相关旧记忆（top5, score>0.75）
                   │   │   └── memoryIndex.Search()       ← 已实现 ✅
                   │   ├── MemUpdateChecker.Check()       ← ⤵️ 当前 stub → 7.8 回填
                   │   │   └── PromptApplier + LLM + JsonOutputParser
                   │   └── memoryIndex.AddMemories/DeleteMemories()
                   ├── VariableManager.add_memories()
                   └── SummaryManager.add_memories()
```

### 1.2 Python 对应路径

| Go 组件 | Python 路径 |
|---------|-------------|
| MemUpdateChecker | `openjiuwen/core/memory/manage/update/mem_update_checker.py` |
| PromptApplier | `openjiuwen/core/memory/prompts/prompt_applier.py` |
| WriteManager | `openjiuwen/core/memory/manage/index/write_manager.py` |
| SearchManager | `openjiuwen/core/memory/manage/search/search_manager.py` |
| 提示词模板 | `openjiuwen/core/memory/prompts/*.md`（4 个文件） |

---

## 2. PromptApplier（运行时读文件 + 缓存）

### 2.1 设计决策

对齐 Python `PromptApplier` 单例模式，运行时从文件系统读取 `.md` 模板文件并缓存，**不做翻译，1:1 复制 Python 模板文件**。

### 2.2 文件结构

```
internal/agentcore/memory/prompts/
├── doc.go                         # 包文档
├── prompt_applier.go              # PromptApplier 单例
├── prompt_applier_test.go         # 测试
├── fragment_memory_prompt.md       # 从 Python 1:1 复制
├── memory_analysis_prompt.md       # 从 Python 1:1 复制
├── memory_update_check.md          # 从 Python 1:1 复制
└── semantic_validation.md          # 从 Python 1:1 复制
```

### 2.3 PromptApplier 实现

```go
type PromptApplier struct {
    cache     sync.Map           // file_prefix → *prompt.PromptTemplate
    promptDir string             // 模板目录路径
}
```

- **单例模式**：`sync.Once` + `DefaultApplier` 全局变量
- **构造**：`NewPromptApplier(dir string) *PromptApplier`，支持自定义目录
- **默认目录**：`DefaultApplier` 通过 `runtime.Caller(0)` 获取当前文件所在目录作为 `promptDir`（对齐 Python `Path(__file__).parent`）
- **核心方法**：

```go
// Apply 加载模板并替换变量，返回填充后的字符串。
// 对齐 Python: PromptApplier.apply(file_prefix, variables)
func (a *PromptApplier) Apply(filePrefix string, variables map[string]any) (string, error)

// GetTemplate 获取已缓存的 PromptTemplate，未缓存则加载。
// 对齐 Python: PromptApplier.get_template(file_prefix)
func (a *PromptApplier) GetTemplate(filePrefix string) (*prompt.PromptTemplate, error)

// ClearCache 清除缓存。
// 对齐 Python: PromptApplier.clear_cache(file_prefix)
func (a *PromptApplier) ClearCache(filePrefix ...string)
```

**Apply 流程**：
1. 缓存命中 → `template.Format(variables)` → 返回 `.Content` 字符串
2. 缓存未命中 → 读取 `{promptDir}/{filePrefix}.md` → `prompt.NewPromptTemplate(filePrefix, content)` → 存入缓存 → Format → 返回
3. 文件不存在 → 返回 error

### 2.4 日志同步

对齐 Python `memory_logger` 调用：

| Python 日志 | Go 日志 |
|------------|---------|
| `memory_logger.info("PromptApplier singleton initialized")` | `logger.Info(logComponent).Msg("PromptApplier 单例初始化")` |
| `memory_logger.debug(f"Using cached prompt template: {file_prefix}")` | `logger.Debug(logComponent).Str("file_prefix", filePrefix).Msg("使用缓存的提示词模板")` |
| `memory_logger.info(f"Loaded and cached prompt template: {file_prefix}")` | `logger.Info(logComponent).Str("file_prefix", filePrefix).Msg("加载并缓存提示词模板")` |
| `memory_logger.debug(f"Applied prompt template: {file_prefix}")` | `logger.Debug(logComponent).Str("file_prefix", filePrefix).Msg("已应用提示词模板")` |
| `memory_logger.info("Cleared all prompt template cache")` | `logger.Info(logComponent).Msg("清除所有提示词模板缓存")` |
| `memory_logger.info(f"Cleared prompt template cache: {file_prefix}")` | `logger.Info(logComponent).Str("file_prefix", filePrefix).Msg("清除提示词模板缓存")` |

---

## 3. MemUpdateChecker 完整实现

### 3.1 当前 stub 状态

文件：`internal/agentcore/memory/manage/update/update_checker.go`

已有类型定义（完整）：
- `MemoryActionItem` — ID / Content / Status
- `MemCheckItem` — InfoID / InfoText / Result / RelatedInfos
- `CheckResult` 枚举 — Redundant / Conflicting / None
- `MemoryStatus` 枚举 — Add / Delete
- `checkConfig` — model / retries
- `CheckOption` / `WithModel` / `WithRetries` — 可选参数

**需修改**：`Check()` 方法签名需增加 `ctx context.Context` 参数（LLM 调用需要 context）。

### 3.2 修改后签名

```go
func (c *MemUpdateChecker) Check(ctx context.Context, newMemories map[string]string, oldMemories map[string]string, opts ...CheckOption) ([]*MemoryActionItem, error)
```

### 3.3 Check() 完整流程（对齐 Python `MemUpdateChecker.check()`）

```
1. cfg = 应用 CheckOption（model、retries=3）

2. 若 cfg.model == nil → 直接返回所有新记忆为 ADD
   （对齐 Python: if not base_chat_model）
   日志: Debug "无 LLM 模型，跳过记忆冲突检查" + new_count + old_count

3. 检查新旧记忆 ID 重复
   日志: Debug "发现重复记忆 ID" + duplicate_ids

4. formatInput(newMemories, oldMemories) → newInfoStr, oldInfoStr
   - 新记忆行按 id 倒序排列（对齐 Python _format_input: new_info_lines[::-1]）
   - 旧记忆行按 id 正序
   - 格式："id: content\n"

5. applier.Apply("memory_update_check", {old_information, new_information}) → userPrompt

6. 构造 messages:
   - formatted = prompt.NewPromptTemplate("memory_update_check_user", userPrompt)
   - messages, _ = formatted.ToMessages()
   - msgsParam = model_clients.NewMessagesParam(messages...)

7. LLM 调用 + JSON 解析（最多 retries 次重试）:
   parser = output_parsers.NewJsonOutputParser()
   for attempt := 0; attempt < cfg.retries; attempt++ {
       response, err := cfg.model.Invoke(ctx, msgsParam, model_clients.WithInvokeOutputParser(parser))
       parsedResult := response.ParserContent  // any 类型

       // 若 parsedResult 为 map（单对象），包装为 slice
       // 若 parsedResult 不为 slice，继续重试

       // 将每个 item 映射为 MemCheckItem
       // 成功则 break
   }
   // 全部重试失败 → fallback: 所有新记忆 ADD
   日志: Warning "记忆冲突检查重试失败" + exception

8. mapCheckItemsToActionItems(checkItems, newMemories) → []*MemoryActionItem
   - REDUNDANT → 跳过（不加入结果）
     日志: Debug "记忆冗余，跳过" + new_id
   - CONFLICTING → 新记忆 ADD + 关联旧记忆 DELETE
   - NONE → 新记忆 ADD

9. 日志: Debug "记忆冲突检查完成" + action_count

10. 返回 actionItems
```

### 3.4 formatInput 辅助函数

对齐 Python `_format_input()`：

```go
// formatInput 格式化新旧记忆字典为提示词输入文本。
// 对齐 Python: _format_input(new_memories, old_memories)
func formatInput(newMemories map[string]string, oldMemories map[string]string) (string, string)
```

逻辑：
- 新记忆：遍历 `newMemories`，格式化为 `"id: content"` 行，**倒序排列**
- 旧记忆：遍历 `oldMemories`，格式化为 `"id: content"` 行，正序排列
- 各行用 `\n` 连接

### 3.5 mapCheckItemsToActionItems 辅助函数

对齐 Python `check()` 方法中的结果映射逻辑：

```go
// mapCheckItemsToActionItems 将 LLM 检查结果映射为动作项列表。
func mapCheckItemsToActionItems(checkItems []*MemCheckItem, newMemories map[string]string) []*MemoryActionItem
```

逻辑：
- 遍历 checkItems
- REDUNDANT → 跳过
- CONFLICTING → 新记忆 ADD（content 取 `newMemories[id]`，fallback `info_text`）+ 关联旧记忆 DELETE（仅当 `old_id in oldMemories`）
- NONE → 新记忆 ADD

### 3.6 日志同步

| Python 日志 | Go 日志 |
|------------|---------|
| `memory_logger.debug("No need to check memories - no old memories or no model", metadata={new_count, old_count})` | `logger.Debug(logComponent).Int("new_count", ...).Int("old_count", ...).Msg("无 LLM 模型，跳过记忆冲突检查")` |
| `memory_logger.debug(f"Found {len(duplicate_ids)} duplicate memory IDs", metadata={duplicate_ids})` | `logger.Debug(logComponent).Int("duplicate_count", ...).Msg("发现重复记忆 ID")` |
| `memory_logger.debug("Start checking memory conflicts", metadata={input_messages})` | `logger.Debug(logComponent).Msg("开始记忆冲突检查")` |
| `memory_logger.debug(f"Succeeded to check memories, got {len(check_results)} results", metadata={result_count})` | `logger.Debug(logComponent).Int("result_count", ...).Msg("记忆冲突检查 LLM 返回成功")` |
| `memory_logger.warning(f"Memory check parse error, retrying ({attempt+1}/{retries}): {e}", exception=str(e))` | `logger.Warn(logComponent).Int("attempt", ...).Int("retries", ...).Err(err).Msg("记忆冲突检查解析错误，重试中")` |
| `memory_logger.error("Memory check failed after retries", exception=str(e))` | `logger.Error(logComponent).Err(err).Msg("记忆冲突检查重试全部失败")` |
| `memory_logger.debug(f"Memory {new_id} is redundant, skipping")` | `logger.Debug(logComponent).Str("mem_id", ...).Msg("记忆冗余，跳过")` |
| `memory_logger.debug(f"Memory check completed, returning {len(action_items)} action items", metadata={action_count})` | `logger.Debug(logComponent).Int("action_count", ...).Msg("记忆冲突检查完成")` |

---

## 4. FragmentMemoryManager 回填

### 4.1 processConflictInfo — 不需要实现

Python `FragmentMemoryManager._process_conflict_info()` **只有定义，没有被调用**（经全项目搜索确认）。
这是遗留的旧版代码。当前 `MemUpdateChecker.check()` 的输入 `new_memories` / `old_memories` 以真实 mem_id 为 key，
LLM 返回的 `info_id` 和 `related_infos` 的 key 也是真实的 mem_id，不需要整数→字符串的映射。

Go 侧之前标注的 `⤵️ 回填: 7.8 — Python _process_conflict_info 将 LLM 返回的数字 id 映射回 mem_id`
基于对 Python 代码的误解，**不需要实现此方法**。

### 4.2 AddMemories 第 3 步回填

当前 `fragment_manager.go` 第 146-163 行：

```go
// 步骤 3：MemUpdateChecker 冲突检查 ← ⤵️ 回填: 7.8
```

**回填修改**：
1. `checker.Check()` 调用增加 `ctx` 参数
2. 删除 `⤵️ 回填: 7.8` 注释
3. 完整对齐 Python `add_memories()` 中的 Step 3-4 逻辑

### 4.3 lite/coding_memory_tool_ops.go 回填

当前 `runChecker()` 空壳（第 445-450 行）：

```go
func runChecker(manager MemoryIndexManager, newID string, newBody string, oldMemories map[string]string) []any {
    // TODO: 7.8 实现 MemUpdateChecker 后替换
    return nil
}
```

**回填修改**：
- 签名需增加 `ctx context.Context` 和 `model *llm.Model` 参数
- 调用 `update.MemUpdateChecker{}.Check(ctx, {newID: newBody}, oldMemories, update.WithModel(model))`
- 将 `[]*update.MemoryActionItem` 转为 `[]any` 返回
- 调用方需传入 ctx 和 model

同时修改第 218-220 行的 `⤵️ 回填: 7.8` 注释处，实现 SKIP 逻辑：
- 调用 `runChecker()` 获取 actions
- 若新记忆 ID 不在 actions 中（即 REDUNDANT），返回 `WriteResult{Mode: WriteModeSkip}`

---

## 5. WriteManager

### 5.1 文件结构

```
internal/agentcore/memory/manage/index/
└── write_manager.go               # 新增
```

### 5.2 实现

对齐 Python `openjiuwen/core/memory/manage/index/write_manager.py`：

```go
// WriteManager 写入操作统一路由器。
// 根据记忆类型分发到对应子 Manager；按 ID 操作时先从 memory_index 查类型再路由。
//
// 对应 Python: openjiuwen/core/memory/manage/index/write_manager.py (WriteManager)
type WriteManager struct {
    managers    map[string]BaseMemoryManager  // mem_type → Manager 实例
    memoryIndex index.BaseMemoryIndex
}
```

**核心方法**：

| 方法 | 对齐 Python | 逻辑 |
|------|------------|------|
| `NewWriteManager(managers, memoryIndex)` | `__init__(managers, memory_index)` | 构造 |
| `AddMemories(ctx, userID, scopeID, memories, llmModel...)` | `add_memories(user_id, scope_id, memories, llm)` | 遍历 `managers` **去重**后调用各 Manager 的 `AddMemories()` |
| `UpdateMemByID(ctx, userID, scopeID, memID, newMemory)` | `update_mem_by_id(user_id, scope_id, mem_id, memory)` | 先从 `memoryIndex.GetByID()` 查 mem_type，再路由到对应 Manager 的 `Update()` |
| `DeleteMemByID(ctx, userID, scopeID, memID)` | `delete_mem_by_id(user_id, scope_id, mem_id)` | 同上，路由到 `Delete()` |
| `DeleteMemByUserID(ctx, userID, scopeID)` | `delete_mem_by_user_id(user_id, scope_id)` | 遍历所有 Manager 调用 `DeleteByUserID()` |

**Python 关键细节**：
- `add_memories` 中 `set(self.managers.values())` 去重——因为三种 Fragment 类型共享同一个 `FragmentMemoryManager` 实例，不去重会重复调用
- `update_mem_by_id` / `delete_mem_by_id` 的 `__get_mem_type_from_index` 私有方法从 index 查 mem_type

### 5.3 日志同步

Python WriteManager 无额外日志（纯路由器），Go 侧同样不添加额外日志。

---

## 6. SearchManager

### 6.1 文件结构

```
internal/agentcore/memory/manage/search/
├── doc.go                         # 包文档
└── search_manager.go              # SearchManager 实现
```

### 6.2 实现

对齐 Python `openjiuwen/core/memory/manage/search/search_manager.py`：

```go
// SearchParams 搜索参数。
// 对应 Python: SearchParams(BaseModel)
type SearchParams struct {
    UserID     string
    ScopeID    string
    Query      string
    TopK       int
    Threshold  float64
    SearchType []string   // 可选，指定搜索的记忆类型
}

// SearchManager 搜索操作统一路由器。
// 语义搜索按 search_type 分发到各 Manager；列表/分页直接走 memory_index。
//
// 对应 Python: openjiuwen/core/memory/manage/search/search_manager.py (SearchManager)
type SearchManager struct {
    managers   map[string]BaseMemoryManager
    cryptoKey  []byte
    memoryIndex index.BaseMemoryIndex
}
```

**核心方法**：

| 方法 | 对齐 Python | 逻辑 |
|------|------------|------|
| `NewSearchManager(managers, cryptoKey, memoryIndex)` | `__init__(managers, crypto_key, memory_index)` | 构造 |
| `Search(ctx, params)` | `search(params, **kwargs)` | 按 `search_type` 分发到对应 Manager 的 `Search()`；无类型时遍历所有；结果按 score 排序截断 top_k，过滤 threshold |
| `ListUserMem(ctx, userID, scopeID, nums, pages, memType)` | `list_user_mem(user_id, scope_id, nums, pages, mem_type)` | 直接调用 `memoryIndex.ListMemories()` 分页 |
| `ListUserProfile(ctx, userID, scopeID)` | `list_user_profile(user_id, scope_id)` | 委托 FragmentMemoryManager.ListFragmentMemories() |
| `ListUserSummary(ctx, userID, scopeID)` | `list_user_summary(user_id, scope_id)` | 委托 SummaryManager.ListUserSummary() |
| `GetUserVariable(ctx, userID, scopeID, varName)` | `get_user_variable(user_id, scope_id, var_name)` | 委托 VariableManager.QueryVariable(name=...) |
| `GetAllUserVariable(ctx, userID, scopeID)` | `get_all_user_variable(user_id, scope_id)` | 委托 VariableManager.QueryVariable() |

**Python 关键细节**：
- `search()` 的 `all_mem_manager_list` 类属性来自 `MemoryType` 枚举所有值
- `search()` 中 `used_types` 按 `search_type` 过滤，再遍历各 Manager
- 结果 `sorted(res, key=lambda x: x[1], reverse=True)[:top_k]` 按 score 降序截断
- `threshold` 过滤：`if score >= threshold`

### 6.3 日志同步

Python SearchManager 无额外日志（纯路由器），Go 侧同样不添加额外日志。

---

## 7. doc.go 更新

### 7.1 manage/doc.go

更新文件目录，添加 `search/` 子目录和 `write_manager.go`：

```
manage/
├── doc.go                  # 包文档
├── index/
│   ├── doc.go
│   ├── base_manager.go
│   ├── fragment_manager.go
│   ├── summary_manager.go
│   ├── variable_manager.go
│   └── write_manager.go    # 新增
├── mem_model/
│   ├── doc.go
│   └── memory_unit.go
├── search/                 # 新增
│   ├── doc.go
│   └── search_manager.go
└── update/
    ├── doc.go
    └── update_checker.go
```

### 7.2 prompts/doc.go

新增包文档。

---

## 8. IMPLEMENTATION_PLAN.md 状态修正

7.10 标记为 `☐`，但实际 `BaseMemoryIndex` + `SimpleMemoryIndex` + 所有子 Manager 已完整实现。修正为 `✅`。

---

## 9. 测试策略

### 9.1 PromptApplier 测试

- `TestNewPromptApplier` — 构造测试
- `TestPromptApplier_Apply_缓存命中` — 验证缓存行为
- `TestPromptApplier_Apply_文件不存在` — 验证错误处理
- `TestPromptApplier_ClearCache_全部清除` — 清除所有缓存
- `TestPromptApplier_ClearCache_指定前缀` — 清除单个缓存
- `TestPromptApplier_GetTemplate` — 获取模板测试
- `TestDefaultApplier_单例` — 验证全局单例

### 9.2 MemUpdateChecker 测试

- `TestMemUpdateChecker_Check_无模型` — model=nil 时返回全部 ADD
- `TestMemUpdateChecker_Check_格式化输入` — formatInput 新旧记忆格式
- `TestMemUpdateChecker_Check_LLM返回冗余` — 模拟 LLM 返回 REDUNDANT，验证跳过
- `TestMemUpdateChecker_Check_LLM返回冲突` — 模拟 LLM 返回 CONFLICTING，验证 ADD+DELETE
- `TestMemUpdateChecker_Check_LLM返回共存` — 模拟 LLM 返回 NONE，验证仅 ADD
- `TestMemUpdateChecker_Check_LLM解析失败_重试` — 模拟解析失败，验证重试
- `TestMemUpdateChecker_Check_LLM全部重试失败_降级` — 验证 fallback 返回全部 ADD
- `TestMapCheckItemsToActionItems` — 结果映射单元测试

LLM 调用通过 fakeModelClient mock（使用 `httptest` 不需要 build tag）。

### 9.3 WriteManager 测试

- `TestWriteManager_AddMemories_去重` — 验证多个 Fragment 类型共享 Manager 时只调用一次
- `TestWriteManager_UpdateMemByID_路由` — 验证按 mem_type 路由
- `TestWriteManager_DeleteMemByID_路由` — 同上
- `TestWriteManager_DeleteMemByUserID_遍历` — 验证遍历所有 Manager

### 9.4 SearchManager 测试

- `TestSearchManager_Search_指定类型` — 按 search_type 路由
- `TestSearchManager_Search_全部类型` — 无 search_type 时遍历所有 Manager
- `TestSearchManager_Search_排序截断` — 验证 score 降序 + top_k 截断 + threshold 过滤
- `TestSearchManager_ListUserMem` — 分页列表
- `TestSearchManager_ListUserProfile` — 委托 FragmentMemoryManager
- `TestSearchManager_ListUserSummary` — 委托 SummaryManager
- `TestSearchManager_GetUserVariable` — 委托 VariableManager
- `TestSearchManager_GetAllUserVariable` — 委托 VariableManager

---

## 10. 依赖关系

```
prompts/ (PromptApplier)
    ↑
    │
update/ (MemUpdateChecker)  ← 依赖 prompts + foundation/llm
    ↑
    │
index/ (FragmentMemoryManager 回填) ← 依赖 update
    ↑
    │
index/ (WriteManager) + search/ (SearchManager) ← 依赖 index 子 Manager + foundation/store/index
```

**无前置依赖阻塞**：7.10 Memory Index 已实现（SimpleMemoryIndex），FragmentMemoryManager.Search() 可用。

---

## 11. 回填点汇总

| 回填点 | 位置 | 当前状态 | 7.8 修改 |
|--------|------|---------|---------|
| `MemUpdateChecker.Check()` LLM 逻辑 | `update/update_checker.go` | stub 返回全部 ADD | 实现 LLM 驱动检查 |
| `FragmentMemoryManager.AddMemories()` 步骤 3 | `index/fragment_manager.go:146` | 标注 `⤵️ 回填: 7.8` | 增加 ctx 参数，删除回填标记（含 processConflictInfo 回填标记也一并删除，该方法不需要实现） |
| `lite/runChecker()` | `lite/coding_memory_tool_ops.go:445` | 空壳返回 nil | 实现调用 MemUpdateChecker |
| `lite/步骤 7a SKIP 逻辑` | `lite/coding_memory_tool_ops.go:218` | 标注 `⤵️ 回填: 7.8` | 实现 SKIP 判断 |
