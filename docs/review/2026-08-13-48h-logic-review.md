# 48h 逻辑审查报告

**审查日期**：2026-08-13
**审查范围**：48小时内提交（00546c8）及关联章节 7.2/7.5/10.3.2/10.3.3/9.65-1
**Python 参考项目**：`openjiuwen` (agent-core) + `jiuwenswarm`

---

## 提交概览

| 提交 | 日期 | 章节 | 描述 |
|------|------|------|------|
| 00546c8 | 08-11 | 7.2/7.5 | 修正两个实现偏差 — snapshotMemoryFiles 改用 sys_operation、ExtractBody 改为非导出 |

48小时内仅 1 个提交，但关联章节覆盖了最近一周内实现的核心模块。本次审查将范围扩展到 7 天内所有相关章节。

---

## 严重问题

### S01: CodingMemoryWrite 创建模式缺少 SKIP（冗余跳过）逻辑

**章节**：7.2 `coding_memory_tool_ops.py`

**Python 样例**：

```python
# coding_memory_tool_ops.py L395-408
if not file_exists:
    old_memories = await _search_similar(ctx, body, basename, top_k=5, threshold=0.75)
    actions = None
    if old_memories and ctx.manager and ctx.manager.llm:
        actions = await _run_checker(ctx.manager, basename, body, old_memories)

    # REDUNDANT handling — 关键：如果新记忆不在 actions 中，则 SKIP
    if actions and not any(a.id == basename for a in actions):
        return WriteResult(
            success=True, path=resolved, mode=WriteMode.SKIP,
            note="Content is redundant with existing memories",
            type=fm.get("type")
        ).to_dict()
```

**Go 问题**：

```go
// coding_memory_tool_ops.go L180-198
if !fileExists {
    conflictResult = map[string]any{"conflict_detected": false, "conflicting_files": []string{}}
    similarFiles := searchSimilar(toolCtx, body, basename, 5, 0.75)
    if len(similarFiles) > 0 {
        // ... 仅做了冲突检测，缺少 SKIP 逻辑
    }
    // ⤵️ 回填: 7.8 MemUpdateChecker — LLM 冗余判断，当前跳过 SKIP 逻辑
}
```

Go 中创建模式完全跳过了 SKIP 逻辑，即使 `runChecker` 返回空列表（当前实现），也无法触发 SKIP。Python 中当 `actions` 不为空且 `basename` 不在 `actions` 中时返回 `WriteMode.SKIP`，避免冗余写入。Go 缺少此功能意味着**每次写入都会创建新文件，即使内容完全冗余**。

**修复方案**：

1. 在 `runChecker` 实现后（7.8），添加 SKIP 判断逻辑
2. 当前可先添加 `searchSimilar` 的 SKIP 阈值降级：当相似度超过 0.9 时直接 SKIP
3. 在 `CodingMemoryWriteWithContext` 创建模式分支中补充：

```go
if !fileExists {
    oldMemories := searchSimilar(toolCtx, body, basename, 5, 0.75)
    actions := runChecker(toolCtx.Manager, basename, body, oldMemories)
    // 对齐 Python: REDUNDANT handling
    if len(actions) > 0 {
        found := false
        for _, a := range actions {
            if am, ok := a.(map[string]any); ok && am["id"] == basename {
                found = true
                break
            }
        }
        if !found {
            return (&WriteResult{
                Success: true, Path: resolved, Mode: WriteModeSkip,
                Note: "Content is redundant with existing memories",
                Type: fm["type"],
            }).ToDict()
        }
    }
    // ... 冲突检测
}
```

---

### S02: CodingMemoryWrite 追加模式缺少 SKIP 逻辑

**章节**：7.2 `coding_memory_tool_ops.py`

**Python 样例**：

```python
# coding_memory_tool_ops.py L422-426
else:
    result = await _prepare_append_mode(ctx, resolved, basename, body, fm)
    if result.get("mode") == WriteMode.SKIP.value:
        return result  # 直接返回 SKIP 结果
```

**Go 问题**：

```go
// coding_memory_tool_ops.go L199-203
} else {
    conflictResult = prepareAppendMode(toolCtx, resolved, basename, body, fm)
    // 检查是否 SKIP（当前 searchSimilar 不做 SKIP，仅 conflict 检测）
}
```

`prepareAppendMode` 返回的 `map[string]any` 没有包含 `mode` 字段，Go 无法判断是否应 SKIP。Python 中 `_prepare_append_mode` 返回 `WriteResult.to_dict()` 含 `mode=skip`，Go 的 `prepareAppendMode` 仅返回冲突信息字典。

**修复方案**：

修改 `prepareAppendMode` 返回类型，使其能表达 SKIP 状态：

```go
func prepareAppendMode(...) (map[string]any, bool) {
    // ... 返回 (result, shouldSkip)
    // 如果 shouldSkip=true，调用方直接返回 WriteModeSkip 结果
}
```

---

### S03: CodingMemoryReadWithContext 缺少顶层异常保护

**章节**：7.2 `coding_memory_tool_ops.py`

**Python 样例**：

```python
# coding_memory_tool_ops.py L105-147
async def coding_memory_read_with_context(...) -> Dict[str, Any]:
    try:
        # ... 所有逻辑
    except Exception as e:
        logger.error(f"Read failed: {e}")
        return {"success": False, "path": path, "content": "", "error": str(e)}
```

**Go 问题**：

```go
// coding_memory_tool_ops.go L61-118
func CodingMemoryReadWithContext(ctx context.Context, ...) map[string]any {
    // 没有 defer/recover 保护
    // 如果 sysOp.Fs().ReadFile 返回意外 panic，会直接崩溃
}
```

Go 的 `CodingMemoryReadWithContext`、`CodingMemoryWriteWithContext`、`CodingMemoryEditWithContext` 都没有顶层异常保护。Python 中三个函数都有 `try/except` 包裹。Go 中虽然有 `error` 返回，但 `sysOp.Fs().ReadFile` 内部可能 panic（如空指针解引用），导致整个 Agent 进程崩溃。

**修复方案**：

在三个核心函数中添加 `defer/recover`：

```go
func CodingMemoryReadWithContext(ctx context.Context, ...) (result map[string]any) {
    defer func() {
        if r := recover(); r != nil {
            logger.Error(logger.ComponentAgentCore).Any("panic", r).Msg("CodingMemoryReadWithContext panic")
            result = map[string]any{"success": false, "path": "", "content": "", "error": fmt.Sprintf("internal error: %v", r)}
        }
    }()
    // ... 原有逻辑
}
```

---

### S04: CodingMemoryWriteWithContext 缺少顶层异常保护

**章节**：7.2 `coding_memory_tool_ops.py`

**Python 样例**：

```python
# coding_memory_tool_ops.py L359-494
async def coding_memory_write_with_context(...) -> Dict[str, Any]:
    try:
        # ... 所有逻辑
    except Exception as e:
        logger.error(f"coding_memory_write failed: {e}")
        return {"success": False, "path": path, "error": str(e)}
```

**Go 问题**：同 S03，`CodingMemoryWriteWithContext` 缺少 `defer/recover`。

**修复方案**：同 S03。

---

### S05: CodingMemoryEditWithContext 缺少顶层异常保护

**章节**：7.2 `coding_memory_tool_ops.py`

**Python 样例**：

```python
# coding_memory_tool_ops.py L497-552
async def coding_memory_edit_with_context(...) -> Dict[str, Any]:
    try:
        # ... 所有逻辑
    except Exception as e:
        logger.error(f"coding_memory_edit failed: {e}")
        return {"success": False, "error": str(e)}
```

**Go 问题**：同 S03。

**修复方案**：同 S03。

---


## 一般问题

### G01: snapshotMemoryFiles 返回类型与 Python 不一致 — Go 用 []string，Python 用 frozenset

**章节**：7.2 `coding_memory_tool_ops.py`

**Python 样例**：

```python
# coding_memory_tool_ops.py L283-308
async def _snapshot_memory_files(ctx, memory_dir) -> frozenset:
    # ...
    return frozenset(names)  # 不可变集合，O(1) 查找
```

**Go 问题**：

```go
// coding_memory_tool_ops.go L473-500
func snapshotMemoryFiles(toolCtx *CodingMemoryToolContext, memoryDir string) []string {
    // 返回 []string，后续用 for 循环遍历判断 fileExists
}
```

Python 用 `frozenset` 做 `in` 操作（O(1)），Go 用 `[]string` 遍历（O(n)）。虽然功能等价，但 `snapshotEqual` 的比较和 `fileExists` 的判断效率较低。更重要的是，`snapshotEqual` 使用 map 集合比较，而 Python 直接用 `frozenset != frozenset`（集合相等比较）。

**修复方案**：

将 `snapshotMemoryFiles` 返回类型改为 `map[string]bool`（或 `map[string]struct{}`），`snapshotEqual` 改为直接比较 map 长度和键：

```go
func snapshotMemoryFiles(toolCtx *CodingMemoryToolContext, memoryDir string) map[string]bool {
    names := make(map[string]bool)
    // ...
    names[name] = true
    return names
}

func snapshotEqual(a, b map[string]bool) bool {
    if len(a) != len(b) { return false }
    for k := range a {
        if !b[k] { return false }
    }
    return true
}
```

---

### G02: CodingMemoryWrite 创建模式中 searchSimilar 返回旧记忆但未调用 runChecker

**章节**：7.2 `coding_memory_tool_ops.py`

**Python 样例**：

```python
# coding_memory_tool_ops.py L397-401
old_memories = await _search_similar(ctx, body, basename, top_k=5, threshold=0.75)
actions = None
if old_memories and ctx.manager and ctx.manager.llm:
    actions = await _run_checker(ctx.manager, basename, body, old_memories)
```

**Go 问题**：

```go
// coding_memory_tool_ops.go L183
similarFiles := searchSimilar(toolCtx, body, basename, 5, 0.75)
// 直接用 similarFiles 判断冲突，没有调用 runChecker
```

Python 中先搜索相似文件，再通过 `_run_checker`（MemUpdateChecker + LLM）判断是否真正冗余或冲突。Go 跳过了 `runChecker` 调用，直接将 `searchSimilar` 的结果作为冲突判断依据。这导致**假阳性冲突**：相似度 > 0.75 的文件不一定真正冲突，只是语义相近。

**修复方案**：

在 `runChecker` 实现前（7.8），在注释中明确标注此降级行为，并降低冲突检测的激进程度。当前可改为：仅当相似度 > 0.9 时才标记冲突，0.75-0.9 之间仅记录 `note` 提示但不阻止写入。

---

### G03: providerKey 格式与 Python 不一致

**章节**：7.1 `manager.py`

**Python 样例**：

```python
# manager.py L372
self.provider_key = f"{self.provider.id}:{self.provider.model}"
# 例如 "openai:text-embedding-3-small"
```

**Go 问题**：

```go
// manager_impl.go L287
m.providerKey = fmt.Sprintf("provider:%s", m.settings.Model)
// 例如 "provider:text-embedding-3-small"
```

Python 的 `provider_key` 格式是 `{provider.id}:{provider.model}`，Go 的格式是 `provider:{model}`。这导致 embedding 缓存查询时无法命中（因为 provider_key 不同），且与 Python 共享数据库时缓存不兼容。

**修复方案**：

```go
m.providerKey = fmt.Sprintf("%s:%s", m.provider.ID(), m.settings.Model)
```

需要在 `EmbeddingProvider` 接口上暴露 `ID()` 方法。

---

### G04: CodingMemoryWrite 超过重试次数后降级写入缺少冲突结果日志中的详细字段

**章节**：7.2 `coding_memory_tool_ops.py`

**Python 样例**：

```python
# coding_memory_tool_ops.py L463-471
logger.warning(
    f"Exceeded max conflict retries ({_MAX_CONFLICT_RETRIES}), "
    f"writing without snapshot validation; "
    f"last conflict detection result preserved: "
    f"conflict_detected={result.get('conflict_detected', False)}, "
    f"conflicting_files={result.get('conflicting_files', [])}"
)
```

**Go 问题**：

```go
// coding_memory_tool_ops.go L269-271
logger.Warn(logger.ComponentAgentCore).
    Int("max_retries", maxConflictRetries).
    Msg("Exceeded max conflict retries, writing without snapshot validation")
```

Go 的日志缺少 `conflict_detected` 和 `conflicting_files` 字段，不利于排查问题。

**修复方案**：

```go
logger.Warn(logger.ComponentAgentCore).
    Int("max_retries", maxConflictRetries).
    Bool("conflict_detected", conflictResult != nil && conflictResult["conflict_detected"] == true).
    Interface("conflicting_files", func() []string {
        if conflictResult == nil { return nil }
        if cf, ok := conflictResult["conflicting_files"].([]string); ok { return cf }
        return nil
    }()).
    Msg("Exceeded max conflict retries, writing without snapshot validation")
```

---

### G05: WriteMemoryWithContext 中 append 模式下 Python 传 append=True + prepend_newline=True，Go 传 WithFsAppend(true) + WithFsPrependNewline(true)

**章节**：7.2 `memory_tool_ops.py`

**Python 样例**：

```python
# memory_tool_ops.py L138-145
write_result = await sys_op.fs().write_file(
    resolved_path,
    content=content,
    create_if_not_exist=True,
    prepend_newline=append,  # append=True 时 prepend_newline=True
    append=True,             # append=True
)
```

**Go 问题**：

```go
// tool_ops.go L198-205
fsOpts := []sysop.FsOption{
    sysop.WithFsCreateIfNotExist(true),
    sysop.WithFsAppend(appendMode),
}
if appendMode {
    prepend := true
    fsOpts = append(fsOpts, sysop.WithFsPrependNewline(prepend))
}
```

此处逻辑实际一致，Go 正确对齐了 Python 的 `append=True + prepend_newline=True` 行为。**无需修复**。

---

### G06: MemorySearchWithContext 缺少 session_key 参数对 search 的传递

**章节**：7.2 `memory_tool_ops.py`

**Python 样例**：

```python
# memory_tool_ops.py L73
if session_key is not None:
    opts["session_key"] = session_key
```

**Go 问题**：

```go
// tool_ops.go L80-82
if sessionKey != "" {
    opts["session_key"] = sessionKey
}
```

此处逻辑一致，Go 用空字符串判断等价于 Python 的 `is not None`。**无需修复**。

---

## 提示问题

### T01: CodingMemoryReadWithContext 中 ReadFile 后的双重行切片逻辑

**章节**：7.2 `coding_memory_tool_ops.py`

**Python 样例**：

```python
# coding_memory_tool_ops.py L123-129
if offset is not None and limit is not None:
    fs_lines = (offset, offset + limit - 1)
elif offset is not None:
    fs_lines = (offset, -1)
else:
    fs_lines = None
payload = await sys_op.fs().read_file(full_path, line_range=fs_lines)
data = (payload.data.content or "")
```

**Go 问题**：

```go
// coding_memory_tool_ops.go L77-117
fsOpts := []sysop.FsOption{}
if offset != nil {
    if limit != nil {
        fsOpts = append(fsOpts, sysop.WithFsLineRange(*offset, *offset+*limit-1))
    } else {
        fsOpts = append(fsOpts, sysop.WithFsLineRange(*offset, -1))
    }
}
readResult, err := sysOp.Fs().ReadFile(ctx, fullPath, fsOpts...)
// ...
rows := strings.Split(content, "\n")
n := len(rows)
fromIdx := 0
if offset != nil {
    fromIdx = *offset - 1
    // ...
}
```

Go 中先通过 `FsOption` 传递 `line_range` 给底层读取（Python 也这样做），然后又对返回内容做一次行切片。这导致**双重切片**：底层已经按行范围读取了，上层又做了一次切片。Python 中也有同样的双重切片（`read_file(line_range=...)` + 后续 `rows[from_idx:to_idx]`），所以这是与 Python 一致的行为，但效率较低。

**修复方案**：建议在 `ReadFile` 返回 `line_range` 已处理的内容时，跳过上层切片。但需与 Python 行为保持一致，暂不修改。

---

### T02: frontmatter.go 中 ValidateFrontmatter 错误信息使用中文

**章节**：7.5 `frontmatter.py`

**Python 样例**：

```python
# frontmatter.py L30-32
return (False, f"Missing required field: {field}")
# ...
return (False, f"type must be one of: {VALID_TYPES}")
```

**Go 问题**：

```go
// frontmatter.go L43-44
return false, "缺少必填字段: " + field
// ...
return false, "type 必须是以下之一: user, feedback, project, reference"
```

Python 的错误信息是英文，Go 是中文。虽然项目编码规范要求中文注释，但错误信息应与 Python 保持一致（一比一复刻），因为 LLM 工具可能依赖这些错误字符串做判断。

**修复方案**：

```go
return false, fmt.Sprintf("Missing required field: %s", field)
// ...
return false, "type must be one of: user, feedback, project, reference"
```

---

### T03: upsertMemoryIndex 中缺少 fm["name"]/fm["description"] 为空时的日志

**章节**：7.2 `coding_memory_tool_ops.py`

**Python 样例**：

```python
# coding_memory_tool_ops.py L72-75
async with _memory_index_lock:
    sys_op = ctx.sys_operation
    if not sys_op:
        return
    # Python 没有额外检查 name/description 是否为空
```

**Go 问题**：

```go
// coding_memory_tool_ops.go L542-543
if fm["name"] == "" || fm["description"] == "" {
    return  // 静默返回，没有日志
}
```

Go 中额外检查了 `name` 和 `description` 是否为空，Python 中没有此检查。虽然这是一个防御性检查，但缺少日志导致调试困难。

**修复方案**：

```go
if fm["name"] == "" || fm["description"] == "" {
    logger.Debug(logger.ComponentAgentCore).Str("path", filename).Msg("upsertMemoryIndex: name or description empty, skipping index update")
    return
}
```

---

### T04: CodingMemoryEditWithContext 返回值中缺少 Python 的 "path" 字段差异

**章节**：7.2 `coding_memory_tool_ops.py`

**Python 样例**：

```python
# coding_memory_tool_ops.py L548
return {"success": True, "path": resolved, "new_content": new_content}
```

**Go 问题**：

```go
// coding_memory_tool_ops.go L372
return map[string]any{"success": true, "path": resolved, "new_content": newContent}
```

此处一致，**无需修复**。

---

### T05: WriteMemoryWithContext 中缺少 Python 的 "no available sys_operation" 分支返回

**章节**：7.2 `memory_tool_ops.py`

**Python 样例**：

```python
# memory_tool_ops.py L155-159
logger.error("Memory write failed, no available sys_operation")
# ... 没有 return，函数继续执行到末尾
return {"success": False, "path": path, "error": "Memory write failed, no available sys_operation"}
```

**Go 问题**：

```go
// tool_ops.go L194-196
if sysOp == nil {
    logger.Error(logger.ComponentAgentCore).Msg("Memory write failed, no available sys_operation")
    return map[string]any{"success": false, "path": path, "error": "Memory write failed, no available sys_operation."}
}
```

Go 在 `sysOp == nil` 时直接返回，Python 中先 logger.error 后没有 return，继续执行到末尾才返回。Go 的处理更合理（提前返回），但 Python 的末尾有额外的 `return` 语句。**Go 的实现更优，无需修复**。

---

### T06: CodingMemoryToolContext 缺少 Python 的 llm 字段

**章节**：7.3 `coding_memory_tool_context.py`

**Python 样例**：

```python
# Python 中 CodingMemoryToolContext 继承 LiteMemoryToolContextBase
# LiteMemoryToolContextBase.manager 有 llm 属性
# coding_memory_tool_ops.py 中通过 ctx.manager.llm 访问
```

**Go 问题**：

```go
// coding_memory_tool_context.go
type CodingMemoryToolContext struct {
    LiteMemoryToolContextBase
    CodingMemoryDir string
}

// LiteMemoryToolContextBase 中 Manager 是 MemoryIndexManager 接口
// 但 MemoryIndexManager 接口不暴露 LLM 字段
```

Go 的 `MemoryIndexManager` 接口没有 `LLM` 方法，而 Python 中 `MemoryIndexManager` 有 `llm` 属性。`_run_checker` 和冲突检测需要 `ctx.manager.llm` 来判断是否启用 LLM 冗余检测。当前 Go 的 `memoryIndexManager` 有 `llm any` 字段但未通过接口暴露。

**修复方案**：

在 `MemoryIndexManager` 接口上添加 `LLM() any` 方法（或 `HasLLM() bool`），使 `runChecker` 可以判断是否需要 LLM 冗余检测。

---

### T07: CodingMemoryWrite 降级写入后缺少 error 返回的日志

**章节**：7.2

**Go 问题**：

```go
// coding_memory_tool_ops.go L286
_, _ = sysOp.Fs().WriteFile(ctx, resolved, content, sysop.WithFsCreateIfNotExist(true))
// 降级写入时忽略了错误
```

Python 中降级写入也会检查错误（通过 `try/except` 包裹），Go 中降级写入时直接忽略了 `WriteFile` 的错误。

**修复方案**：

```go
if _, err := sysOp.Fs().WriteFile(ctx, resolved, content, sysop.WithFsCreateIfNotExist(true)); err != nil {
    logger.Error(logger.ComponentAgentCore).Err(err).Str("path", resolved).Msg("降级写入失败")
    return map[string]any{"success": false, "path": path, "error": err.Error()}
}
```

---

### S06: CodeAdapter 缺少 _build_configured_subagents 覆写 — code 模式缺少核心子代理

**章节**：10.3.3 `interface_code.py`

**Python 样例**：

```python
# interface_code.py — CodeAdapter._build_configured_subagents
# 固定添加 explore_agent + plan_agent + code_agent + browser_agent
explore_config = build_explore_agent_config(model, config_base, ...)
plan_config = build_plan_agent_config(model, config_base, ...)
code_config = build_code_agent_config(model, config_base, ...)
browser_config = build_browser_agent_config(model, config_base, ...)
```

**Go 问题**：

Go 的 `CodeAdapter.CreateInstance` 调用 `c.deep.buildConfiguredSubagents()`（DeepAdapter 版本），只构建 research/browser/custom 子代理，**完全缺少 explore/plan/code 子代理**。这些是 code 模式的核心子代理，缺失导致 code 模式功能完全不可用。

**修复方案**：

在 `code_adapter.go` 中覆写 `buildConfiguredSubagents`，添加 explore/plan/code 子代理构建逻辑。需要先实现 `BuildExploreAgentConfig`/`BuildPlanAgentConfig`/`BuildCodeAgentConfig` 工厂函数。

---

### S07: CodeAdapter 缺少 _get_tool_cards 覆写 — code 模式无法加载专用工具

**章节**：10.3.3 `interface_code.py`

**Python 样例**：

```python
# interface_code.py — CodeAdapter._get_tool_cards
# 调用 build_code_tool_cards()，从 config.yaml::modes.code.tools 读取工具列表
tool_cards = await build_code_tool_cards(agent_card_id, config_base)
# 支持 web_free_search, web_fetch_webpage, web_paid_search, user_todos, skill_toolkit 等
```

**Go 问题**：

Go 的 `CodeAdapter.CreateInstance` 使用 DeepAdapter 的 `getToolCards()`，完全缺少 code 模式专用工具构建逻辑。code 模式下无法从配置加载 web_search、web_fetch、skill_toolkit 等工具。

**修复方案**：

在 `code_adapter.go` 中覆写 `getToolCards`，实现 `buildCodeToolCards` 函数，从 `config.yaml::modes.code.tools` 读取工具列表并构建对应 ToolCard。

---

### S08: CodeAdapter 缺少 build_code_system_prompt — 使用通用 prompt 替代

**章节**：10.3.3 `interface_code.py`

**Python 样例**：

```python
# interface_code.py — CreateInstance 步骤 19
create_deep_agent(system_prompt=build_code_system_prompt(), ...)
```

**Go 问题**：

```go
// code_adapter.go — 使用通用 prompt
systemPrompt := c.deep.buildAgentIdentityPrompt("en")
```

Python 使用 `build_code_system_prompt()` 生成 code 模式专用系统提示词，Go 使用通用的 `buildAgentIdentityPrompt("en")`。code 模式的系统提示词与通用模式不同，缺失会影响 Agent 的行为和输出质量。

**修复方案**：

实现 `buildCodeSystemPrompt()` 函数，对齐 Python 的 `build_code_system_prompt()` 提示词内容。

---

### S09: UapClaw Team 后续请求绕过 Session 队列逻辑完全缺失

**章节**：10.3.2 `interface.py`

**Python 样例**：

```python
# interface.py L966-1012 — ProcessMessageStream
is_team_first_request = (
    team_manager.active_session_id != session_id
    and team_manager.pending_session_id != session_id
    and not team_manager.has_stream_task(session_id)
)
if is_team_first_request:
    # 首次请求：走正常 Session 队列
    ...
else:
    # 后续请求：直接执行，不排队
    ...
```

**Go 问题**：

```go
// uapclaw.go L513
// ⤵️ 10.6.19-23: Team 后续请求绕过 Session 队列（等待 TeamManager）
```

Go 中 `is_team_first_request` 判断逻辑完全缺失，Team 模式下后续请求会错误地排队等待，而非直接执行。

**修复方案**：依赖 10.6.19-23 TeamManager 实现，当前为已知缺失。

---

### S10: UapClaw Team pause/cancel 硬编码为 false — 功能完全无效

**章节**：10.3.2 `interface.py`

**Python 样例**：

```python
# interface.py — _process_team_interrupt
if interrupt_type == "pause":
    paused = team_manager.pause_session_runtime(session_id, reason=reason)
elif interrupt_type == "cancel":
    cancelled = team_manager.cancel_session_runtime(session_id, reason=reason)
```

**Go 问题**：

```go
// uapclaw.go L773/792
paused = false   // 硬编码
cancelled = false // 硬编码
```

Team 模式的 pause/cancel 完全无效，`paused` 和 `cancelled` 始终为 `false`，用户会收到"当前没有可暂停/取消的团队任务"的错误消息。

**修复方案**：依赖 10.6.19-23 TeamManager 实现。

---

### S11: MemoryIndexManager.Search 中 min_score 默认值与 Python 不一致

**章节**：7.1 `manager.py`

**Python 样例**：

```python
# manager.py L876-877
min_score = opts.get("min_score") if opts and "min_score" in opts else \
    self.settings.query.get("min_score", 0.7)  # 默认 0.7
```

**Go 问题**：

```go
// manager_impl.go L735-740
minScore := 0.3  // 默认 0.3
if v, ok := opts["min_score"].(float64); ok {
    minScore = v
} else if v, ok := m.settings.Query["min_score"].(float64); ok {
    minScore = v
}
```

Python 的 `min_score` 默认值为 `0.7`，Go 为 `0.3`。这导致 Go 的搜索会返回更多低质量结果（大量 score 在 0.3-0.7 之间的噪声结果）。

**修复方案**：

将 `CreateMemorySettings` 中的 `Query["min_score"]` 默认值改为 `0.7`，或在 `Search` 方法中改为 `0.7`。

---

### S12: MemoryIndexManager.Search 中 has_vector 判断逻辑与 Python 不一致

**章节**：7.1 `manager.py`

**Python 样例**：

```python
# manager.py L892
has_vector = any(v != 0 for v in query_vec)  # 检查是否有非零值
```

**Go 问题**：

```go
// manager_impl.go L778-779
queryVec, _ := m.provider.EmbedQuery(ctx, cleaned)
hasVector := len(queryVec) > 0  // 只检查长度
```

Python 检查向量中是否有非零值，Go 只检查长度。如果 embedding 返回全零向量（API 错误但未报错），Go 会尝试向量搜索并返回无意义结果，Python 会跳过。

**修复方案**：

```go
hasVector := len(queryVec) > 0
if hasVector {
    allZero := true
    for _, v := range queryVec {
        if v != 0 { allZero = false; break }
    }
    if allZero { hasVector = false }
}
```

---

### S13: InProcessMessager.Publish 持有 Mutex 锁调用 handler — 存在死锁风险

**章节**：9.65-1 `inprocess.py`

**Python 样例**：

```python
# inprocess.py L46-54
async def publish(self, topic: str, message: EventMessage) -> None:
    subs = self._topic_subs.get(topic)
    if not subs:
        return
    for agent_id, handler in list(subs.items()):  # 创建快照，避免迭代修改
        try:
            await handler(message)  # 在 asyncio 单线程中调用，无需锁
        except Exception as exc:
            team_logger.error(...)
```

**Go 问题**：

```go
// inprocess.go L92-106
b := getBus()
b.mu.Lock()
defer b.mu.Unlock()  // 持有锁
subs, ok := b.topicSubs[topicID]
if !ok {
    return nil
}
for aid, handler := range subs {
    if err := handler(ctx, message); err != nil {  // 在持有锁的情况下调用 handler
        logger.Error(...)
    }
}
```

Go 在持有 `b.mu.Lock()` 的情况下调用 `handler(ctx, message)`。如果 handler 内部调用了 Bus 的任何方法（Publish、Subscribe、Unsubscribe、Send、RegisterDirectMessageHandler 等），这些方法都会尝试获取 `b.mu.Lock()`，导致**死锁**。

对比：同文件的 `Send` 方法（`inprocess.go:149-152`）在调用 handler 之前释放了锁，这是正确的做法。但 `Publish` 方法没有这样做。

**修复方案**：

先复制 handler 列表，释放锁后再调用 handler，与 `Send` 方法保持一致：

```go
func (m *InProcessMessager) Publish(ctx context.Context, topicID string, message *EventMessage) error {
    if message.SenderID == "" {
        message.SenderID = m.agentID
    }
    b := getBus()
    b.mu.Lock()
    subs, ok := b.topicSubs[topicID]
    if !ok {
        b.mu.Unlock()
        return nil
    }
    // 复制 handler 列表，释放锁后再调用
    handlers := make(map[string]MessagerHandler, len(subs))
    for aid, h := range subs {
        handlers[aid] = h
    }
    b.mu.Unlock()

    for aid, handler := range handlers {
        if err := handler(ctx, message); err != nil {
            logger.Error(logger.ComponentChannel).Err(err).Str("agent_id", aid).Msg("publish handler error")
        }
    }
    return nil
}
```

---

## 一般问题（续）

### G07: CodeAdapter buildCodeAgentRails 中 14 个 Rails 仅 3 个真正有实现

**章节**：10.3.3

Python 的 code 模式固定 Rails 有 14 个，Go 中只有 FileSystemRail、AgentModeRail、ContextProcessorRail 真正实现，CodeAgentRail 条件实现。其余 10 个（RuntimePromptRail、ResponsePromptRail、StreamEventRail、SecurityRail、LspRail、ProjectMemoryRail、PermissionInterruptRail、CodingMemoryRail、StructuredAskUserRail、ConfirmInterruptRail）全部返回 nil。

**修复方案**：逐步实现各 Rail，优先级：CodingMemoryRail > ProjectMemoryRail > SecurityRail > PermissionInterruptRail。

---

### G08: CodeAdapter 缺少 _resolve_prompt_language / _resolve_runtime_language 覆写

**章节**：10.3.3

**Python 样例**：

```python
# interface_code.py — CodeAdapter._resolve_prompt_language()
def _resolve_prompt_language(self) -> str:
    return "en"  # code 模式强制英文
```

**Go 问题**：Go 的 CodeAdapter 没有覆写 `resolvePromptLanguage()`，使用 DeepAdapter 的版本（从 configCache 读取 `preferred_language`），不会强制英文。code 模式下系统提示词可能不是英文，与 Python 行为不一致。

**修复方案**：在 `code_adapter.go` 中覆写 `resolvePromptLanguage()`，强制返回 `"en"`。

---

### G09: MemoryIndexManager 缺少 _embed_query_with_timeout — 搜索无超时保护

**章节**：7.1 `manager.py`

**Python 样例**：

```python
# manager.py L1113-1126
async def _embed_query_with_timeout(self, query: str) -> List[float]:
    try:
        timeout = 60.0
        return await asyncio.wait_for(
            self.provider.embed_query(query),
            timeout=timeout
        )
    except asyncio.TimeoutError:
        logger.warning("Embedding query timed out")
        return []
```

**Go 问题**：Go 的 `Search` 方法直接调用 `m.provider.EmbedQuery(ctx, cleaned)`，没有超时保护。如果 embedding API 挂起，Go 侧会无限等待。

**修复方案**：

```go
ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
defer cancel()
queryVec, err := m.provider.EmbedQuery(ctx, cleaned)
```

---

### G10: MemoryIndexManager 缺少 _get_base_dir_for_file — USER.md 相对路径计算不一致

**章节**：7.1 `manager.py`

**Python 样例**：

```python
# manager.py L670-675
def _get_base_dir_for_file(self, filepath: str) -> str:
    user_md_path = self.workspace.get_node_path("USER.md")
    if user_md_path and os.path.normpath(filepath) == os.path.normpath(str(user_md_path)):
        return str(self.workspace.root_path)
    return self.memory_dir
```

**Go 问题**：Go 中 `syncMemoryFiles` 统一使用 `m.memoryDir` 作为 baseDir，不区分 USER.md。USER.md 位于 workspace 根目录，相对路径计算可能不一致。

**修复方案**：在 `syncMemoryFiles` 中添加 USER.md 特殊处理，与 Python `_get_base_dir_for_file` 对齐。

---

### G11: EmbeddingProvider 接口缺少 ID()/Model()/Dims() 方法

**章节**：7.1 `embeddings.py`

**Python 样例**：

```python
# embeddings.py — EmbeddingProvider 有 id, model, dims 属性
class EmbeddingProvider:
    id: str
    model: str
    dims: int
```

**Go 问题**：Go 的 `EmbeddingProvider` 接口只有 `EmbedQuery` 方法，缺少 `ID()`、`Model()`、`Dims()` 方法。这导致 `_should_full_reindex` 和 `_get_embedding` 中无法用 `provider.id`，缓存查询条件不一致。

**修复方案**：在 `EmbeddingProvider` 接口上添加 `ID() string`、`Model() string`、`Dims() int` 方法。

---

### G12: InitCodingMemoryManagerAsync 不接受 llm 参数

**章节**：7.2 `coding_memory_tools.py`

**Python 样例**：

```python
# coding_memory_tools.py — init_memory_manager_async
async def init_memory_manager_async(
    workspace, agent_id="default",
    embedding_config=None, sys_operation=None,
    llm=None,  # ← 接受 llm 参数
) -> Optional[MemoryIndexManager]:
    ...
    if manager:
        if llm:
            manager.llm = llm  # ← 设置到 manager
```

**Go 问题**：Go 的 `InitCodingMemoryManagerAsync` 不接受 `llm` 参数，无法将 LLM 实例传入 manager。MemUpdateChecker 需要 LLM 来判断冗余/冲突，此缺失导致 7.8 无法正确回填。

**修复方案**：在 `InitCodingMemoryManagerAsync` 和 `InitMemoryManagerAsync` 中添加 `llm any` 参数，设置到 `memoryIndexManager.llm`。

---

### G13: MemoryIndexManager 缺少 _is_recent_session_file — session 文件新鲜度判断

**章节**：7.1 `manager.py`

**Python 样例**：

```python
# manager.py L79-107
def _is_recent_session_file(filename: str) -> bool:
    """判断文件是否为今天/昨天的 session 记录"""
    match = re.match(r'^(\d{4}-\d{2}-\d{2})\.md$', filename)
    # ...
    return file_date in (today, yesterday)
```

**Go 问题**：Go 中没有 session 文件新鲜度判断，Python 用此函数区分最近 session 和历史 session。

**修复方案**：在 `manager_impl.go` 中添加 `isRecentSessionFile` 函数。

---

### G14: MemoryIndexManager 缺少 session 增量状态追踪

**章节**：7.1 `manager.py`

Python 有 `sessions_dirty`/`sessions_dirty_files`/`session_warm`/`_session_timer`/`_session_pending_files` 等字段和逻辑，Go 完全没有。这导致 session 同步不如 Python 精细。

**修复方案**：后续逐步回填 session 增量状态追踪。

---

### G15: 文件监听不覆盖 workspace root

**章节**：7.1 `manager.py`

**Python 样例**：

```python
# manager.py L469-471
workspace_root = str(self.workspace.root_path) if self.workspace.root_path else None
if workspace_root and os.path.isdir(workspace_root):
    watch_paths.add(workspace_root)
```

**Go 问题**：Go 的 `setupFileWatcher` 不监听 workspace 根目录，Python 中监听。USER.md 等根目录下的文件变更不会被监听。

**修复方案**：在 `setupFileWatcher` 中添加 workspace root 监听路径。

---

### G16: ChunkMarkdown 行号起始值与 Python 不一致

**章节**：7.1 `internal.py`

**Python 样例**：`start_line = 1`（1-based），`end_line = len(lines)`（exclusive end）

**Go 问题**：`StartLine = 0`（0-based），`EndLine = len(lines) - 1`（inclusive end）

这导致搜索结果中引用的行号与 Python 不一致，可能影响 LLM 工具对行号的引用。

**修复方案**：将 `ChunkMarkdown` 的 `StartLine` 改为 1-based，`EndLine` 改为 exclusive end，对齐 Python。

---

### G17: UapClaw MemoryHookContext 的 agent_name 为空字符串

**章节**：10.3.2 `interface.py`

**Python 样例**：`MemoryHookContext(agent_name="main_agent", ...)`

**Go 问题**：`AgentName: ""`（空字符串），可能影响记忆钩子的过滤/路由逻辑。

**修复方案**：将 `AgentName` 改为 `"main_agent"`。

---

### G18: UapClaw MemoryHookContext 的 workspace_dir 路径不同

**章节**：10.3.2 `interface.py`

**Python 样例**：`workspace_dir=str(get_agent_home_dir())` → `~/.uapclaw/`

**Go 问题**：`WorkspaceDir: workspace.AgentWorkspaceDir()` → `~/.uapclaw/agent/workspace/`

路径不同，可能导致记忆模块存储位置不一致。

**修复方案**：对齐 Python 使用 `get_agent_home_dir()` 等价路径。

---

### G19: InProcessMessager.Publish 的 SenderID 原地修改 vs Python 的 model_copy

**章节**：9.65-1 `inprocess.py`

**Python 样例**：

```python
# inprocess.py L123-132
async def publish(self, topic_id: str, message: EventMessage) -> None:
    if hasattr(message, "sender_id") and not message.sender_id:
        message = message.model_copy(update={"sender_id": self._agent_id})  # 创建新副本
    await self._bus.publish(topic_id, message)
```

**Go 问题**：

```go
// inprocess.go L86-91
func (m *InProcessMessager) Publish(ctx context.Context, topicID string, message *schema.EventMessage) error {
    agentID := m.agentID()
    // Stamp SenderID：直接设置消息的 SenderID
    if message.SenderID == "" {
        message.SenderID = agentID  // 原地修改，影响所有持有该指针的调用方
    }
```

Python 使用 `model_copy(update={...})` 创建消息副本，原始消息对象不受影响。Go 直接原地修改 `message.SenderID`，如果调用方在发布后还持有该消息对象（如同时发布到多个 topic），会看到已被修改的 `SenderID`。当前场景下因为只有一个 topic 调用，问题不大，但与 Python 语义不一致。

**修复方案**：

创建消息副本再修改：

```go
if message.SenderID == "" {
    msgCopy := *message  // 值拷贝
    msgCopy.SenderID = agentID
    message = &msgCopy
}
```

---

## 提示问题（续）

### T08: embedding_cache 查询中 provider 字段硬编码为 "provider"

**章节**：7.1

**Python 样例**：`WHERE provider = ? AND model = ? AND provider_key = ? AND hash = ?`，参数 `(self.provider.id, self.provider.model, self.provider_key, text_hash)`

**Go 问题**：参数 `("provider", m.settings.Model, m.providerKey, textHash)`，provider 字段硬编码为 `"provider"`，Python 用 `self.provider.id`。缓存查询条件不一致。

**修复方案**：与 G03/G11 一并修复，用 `m.provider.ID()` 替代硬编码。

---

### T09: CodeAdapter 缺少 configure_team_member_agent 方法

**章节**：10.3.3

Python CodeAdapter 实现了完整的 `configure_team_member_agent` 方法，包括 skill_manager 设置、runtime_language_override、config_base 加载、model 创建、tool_cards 构建、rails 构建、subagents 构建、mcps 合并等。Go 完全无此方法，team 模式下 code 成员 Agent 无法正确配置。

**修复方案**：依赖 10.6.19-23 TeamManager 实现。

---

### T10: CodeAdapter agent_history 路径修正缺失

**章节**：10.3.3

Python 在 `ensure_initialized()` 之后，遍历所有 rail 的 tools，将 `_workspace_path` 修正为 `_agent_workspace_dir`。Go 代码中步骤 21.3 标记为 ⤵️，未实现。`.agent_history` 文件可能写入用户项目目录而非 agent 系统 workspace。

**修复方案**：在 CreateInstance 步骤 21 后添加 agent_history 路径修正逻辑。

---

### T11: InProcessMessager.subscribedTopics 注释误导 — 声明"用于 stop 清理"但 Stop() 为空

**章节**：9.65-1 `inprocess.py`

**Python 样例**：

```python
# inprocess.py L111
self._subscribed_topics: list[str] = []  # Python 中也仅用于记录，stop() 同样为空
```

**Go 问题**：

```go
// inprocess.go L33-34
subscribedTopics []string  // 已订阅的主题列表（用于 stop 时清理）
```

注释说"用于 stop 时清理"，但 `Stop()` 方法为空实现，没有任何清理逻辑。`subscribedTopics` 实际上仅用于 `Unsubscribe` 时从列表中移除条目，与 Python 一致。注释容易误导开发者以为 Stop 应该清理订阅。

**修复方案**：

修正注释：

```go
subscribedTopics []string  // 已订阅的主题列表（用于 Unsubscribe 时移除条目）
```

---

## ⤵️ 占位代码确认

| 位置 | 标记 | 状态 | 说明 |
|------|------|------|------|
| `coding_memory_tool_ops.go:196` | ⤵️ 7.8 MemUpdateChecker | **未实现** | 创建模式 SKIP 逻辑缺失，依赖 7.8 |
| `coding_memory_tool_ops.go:386` | ⤵️ 7.8 MemUpdateChecker | **未实现** | searchSimilar 仅做搜索，不做 LLM 冗余判断 |
| `coding_memory_tool_ops.go:422` | ⤵️ 7.8 MemUpdateChecker | **未实现** | runChecker 返回空列表，占位正确 |
| `coding_memory_tool_ops.go:453` | ⤵️ 7.8 MemUpdateChecker | **未实现** | 追加模式 LLM 冗余判断缺失 |
| `swarm/server/runtime/uapclaw.go:513` | ⤵️ 10.6.19-23 | **未实现** | Team 后续请求绕过，依赖 TeamManager |
| `swarm/server/runtime/uapclaw.go:773` | ⤵️ 10.6.19-23 | **未实现** | team_manager.pause_session_runtime |
| `swarm/server/runtime/uapclaw.go:792` | ⤵️ 10.6.19-23 | **未实现** | team_manager.cancel_session_runtime |
| `swarm/server/runtime/uapclaw.go:826` | ⤵️ 10.6.19-23 | **未实现** | get_team_manager + terminate_session_runtime |
| `agent_teams/messager/base.go:25` | ⤵️ 9.65-2 | **未实现** | PyZmq 后端 |
| `code_adapter.go:182` | ⤵️ 10.6.24 | **未实现** | _refresh_multimodal_configs |
| `code_adapter.go:269` | ⤵️ 10.6.3-10 | **未实现** | build_code_system_prompt |
| `code_adapter.go:311-319` | ⤵️ 10.6.3-10 | **未实现** | adapter_mode/workspace.set_directory/agent_history |
| `code_adapter.go:331` | ⤵️ 10.6.3-10 | **未实现** | load_user_rails |
| `code_adapter.go:454-501` | ⤵️ 多个 Rail | **未实现** | LspRail/ProjectMemoryRail/CodingMemoryRail 等返回 nil |

所有 ⤵️ 标记确实尚未实现，指向了正确的未来章节。

---

## 问题汇总

| 编号 | 严重度 | 章节 | 简述 |
|------|--------|------|------|
| S01 | 严重 | 7.2 | 创建模式缺少 SKIP（冗余跳过）逻辑 |
| S02 | 严重 | 7.2 | 追加模式缺少 SKIP 逻辑 |
| S03 | 严重 | 7.2 | CodingMemoryReadWithContext 缺少顶层异常保护 |
| S04 | 严重 | 7.2 | CodingMemoryWriteWithContext 缺少顶层异常保护 |
| S05 | 严重 | 7.2 | CodingMemoryEditWithContext 缺少顶层异常保护 |
| S06 | 严重 | 10.3.3 | CodeAdapter 缺少 _build_configured_subagents 覆写，code 模式缺少核心子代理 |
| S07 | 严重 | 10.3.3 | CodeAdapter 缺少 _get_tool_cards 覆写，code 模式无法加载专用工具 |
| S08 | 严重 | 10.3.3 | CodeAdapter 缺少 build_code_system_prompt，使用通用 prompt 替代 |
| S09 | 严重 | 10.3.2 | Team 后续请求绕过 Session 队列逻辑完全缺失 |
| S10 | 严重 | 10.3.2 | Team pause/cancel 硬编码为 false，功能完全无效 |
| S11 | 严重 | 7.1 | Search min_score 默认值 0.3 vs Python 0.7，返回过多低质量结果 |
| S12 | 严重 | 7.1 | has_vector 判断只检查长度不检查非零值，全零向量会误走向量搜索 |
| S13 | 严重 | 9.65-1 | InProcessMessager.Publish 持有 Mutex 锁调用 handler，存在死锁风险 |
| G01 | 一般 | 7.2 | snapshotMemoryFiles 返回类型与 Python 不一致 |
| G02 | 一般 | 7.2 | 创建模式 searchSimilar 未调用 runChecker 导致假阳性冲突 |
| G03 | 一般 | 7.1 | providerKey 格式与 Python 不一致 |
| G04 | 一般 | 7.2 | 降级写入日志缺少冲突结果字段 |
| G07 | 一般 | 10.3.3 | buildCodeAgentRails 14 个 Rails 仅 3 个真正有实现 |
| G08 | 一般 | 10.3.3 | CodeAdapter 缺少 prompt_language/runtime_language 覆写，不强制英文 |
| G09 | 一般 | 7.1 | Search 缺少 _embed_query_with_timeout，embedding 无超时保护 |
| G10 | 一般 | 7.1 | 缺少 _get_base_dir_for_file，USER.md 相对路径计算不一致 |
| G11 | 一般 | 7.1 | EmbeddingProvider 接口缺少 ID()/Model()/Dims() 方法 |
| G12 | 一般 | 7.2 | InitCodingMemoryManagerAsync 不接受 llm 参数 |
| G13 | 一般 | 7.1 | 缺少 _is_recent_session_file，session 文件新鲜度判断 |
| G14 | 一般 | 7.1 | 缺少 session 增量状态追踪 |
| G15 | 一般 | 7.1 | 文件监听不覆盖 workspace root |
| G16 | 一般 | 7.1 | ChunkMarkdown 行号起始值与 Python 不一致（0-based vs 1-based） |
| G17 | 一般 | 10.3.2 | MemoryHookContext 的 agent_name 为空字符串 |
| G18 | 一般 | 10.3.2 | MemoryHookContext 的 workspace_dir 路径不同 |
| G19 | 一般 | 9.65-1 | InProcessMessager.Publish SenderID 原地修改 vs Python model_copy |
| T01 | 提示 | 7.2 | ReadWithContext 双重行切片（与 Python 一致） |
| T02 | 提示 | 7.5 | ValidateFrontmatter 错误信息中英不一致 |
| T03 | 提示 | 7.2 | upsertMemoryIndex 空字段静默返回缺少日志 |
| T06 | 提示 | 7.3 | MemoryIndexManager 接口不暴露 LLM 字段 |
| T07 | 提示 | 7.2 | 降级写入后忽略 WriteFile 错误 |
| T08 | 提示 | 7.1 | embedding_cache 查询中 provider 字段硬编码为 "provider" |
| T09 | 提示 | 10.3.3 | CodeAdapter 缺少 configure_team_member_agent |
| T10 | 提示 | 10.3.3 | CodeAdapter agent_history 路径修正缺失 |
| T11 | 提示 | 9.65-1 | subscribedTopics 注释误导 — 声明"stop 清理"但 Stop() 为空 |

**统计**：严重 13 项 · 一般 19 项 · 提示 11 项 · 合计 43 项

**按章节分布**：

| 章节 | 严重 | 一般 | 提示 | 合计 |
|------|------|------|------|------|
| 7.1 MemoryIndexManager | 2 | 9 | 1 | 12 |
| 7.2 CodingMemoryTools | 5 | 3 | 4 | 12 |
| 7.3 ToolContext | 0 | 0 | 1 | 1 |
| 7.5 Frontmatter | 0 | 0 | 1 | 1 |
| 9.65-1 InProcessMessager | 1 | 1 | 1 | 3 |
| 10.3.2 UapClaw | 2 | 2 | 0 | 4 |
| 10.3.3 CodeAdapter | 3 | 2 | 2 | 7 |
| 7.8 MemUpdateChecker（⤵️占位） | 0 | 2* | 0 | 2* |

*注：7.8 相关问题（G02 假阳性冲突、G12 llm 参数缺失）已归入对应章节，但根因均为 7.8 未实现。
