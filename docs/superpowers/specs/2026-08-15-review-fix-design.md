# 2026-08-13 审查问题修复设计

**日期**：2026-08-15
**来源**：docs/review/2026-08-13-48h-logic-review.md
**范围**：43 个问题中 20 个本次修复，8 个跳过等后续章节，7 个已不存在，4 个无需修复，2 个部分存在

---

## 一、已确认不存在的问题（7个）

代码已修复，无需处理：

| 编号 | 说明 |
|------|------|
| S11 | min_score 默认值已一致（0.7） |
| S12 | has_vector 已检查非零值 |
| S13 | Publish 已在锁内复制 handler 列表，锁外调用 |
| G03 | providerKey 格式已使用 `{id}:{model}` |
| G15 | setupFileWatcher 已监听 workspace root |
| G16 | ChunkMarkdown 已使用 1-based 行号 |
| T08 | embedding_cache 已使用 `m.provider.ID()` |

## 二、无需修复（4个）

| 编号 | 说明 |
|------|------|
| G05 | append 模式 prepend_newline 逻辑已一致 |
| G06 | session_key 传递逻辑已一致 |
| T01 | 双重行切片与 Python 一致 |
| T04 | EditWithContext 返回值已一致 |

## 三、跳过，等后续章节（8个）

| 编号 | 原因 | 处理方式 |
|------|------|---------|
| S01 | 创建模式 SKIP 逻辑等 7.8 MemUpdateChecker | 保留 ⤵️ 标注 |
| S02 | 追加模式 SKIP 逻辑等 7.8 MemUpdateChecker | 保留 ⤵️ 标注 |
| G02 | runChecker 未调用等 7.8 MemUpdateChecker | 保留 ⤵️ 标注 |
| G12 | InitCodingMemoryManagerAsync 缺 llm 参数等 7.8 | 保留 ⤵️ 标注 |
| S09 | Team 后续请求绕过依赖 10.6.19-23 TeamManager | **加注释标注** |
| S10 | Team pause/cancel 硬编码依赖 10.6.19-23 TeamManager | **加注释标注** |
| G07 | 14 个 Rails 等 10.6.3-10 回填 | 保留 ⤵️ 标注 |
| T09 | configure_team_member_agent 依赖 10.6.19-23 TeamManager | **加注释标注** |

## 四、本次修复（20个）

### 4.1 异常保护（S03/S04/S05）

**文件**：`internal/agentcore/memory/lite/coding_memory_tool_ops.go`

**方案**：在 `CodingMemoryReadWithContext`、`CodingMemoryWriteWithContext`、`CodingMemoryEditWithContext` 三个函数中添加 `defer/recover`，对齐 Python 的 `try/except`。

```go
func CodingMemoryReadWithContext(ctx context.Context, ...) (result *CodingReadResult) {
    defer func() {
        if r := recover(); r != nil {
            logger.Error(logger.ComponentAgentCore).Any("panic", r).Msg("CodingMemoryReadWithContext panic")
            result = &CodingReadResult{Success: false, Error: fmt.Sprintf("internal error: %v", r)}
        }
    }()
    // ... 原有逻辑
}
```

Write 和 Edit 类似，返回 `map[string]any{"success": false, "error": ...}`。

### 4.2 CodeAdapter buildConfiguredSubagents 覆写（S06）

**文件**：`internal/swarm/server/adapter/code_adapter.go`

**方案**：在 CodeAdapter 中覆写 `buildConfiguredSubagents`，固定添加 explore/plan/code 子代理。依赖已全部就绪（`BuildExploreAgentConfig`/`BuildPlanAgentConfig`/`BuildCodeAgentConfig`）。CodingMemoryRail 未实现，用 nil 占位。

对齐 Python `interface_code.py` 的 `_build_configured_subagents`：
- explore_agent：固定挂载
- plan_agent：固定挂载
- code_agent：按配置启用，CodingMemoryRail 条件注入（nil 占位）
- browser_agent：按配置启用（继承 DeepAdapter）

### 4.3 CodeAdapter getToolCards 覆写（S07）

**文件**：`internal/swarm/server/adapter/code_adapter.go`

**方案**：在 CodeAdapter 中覆写 `getToolCards`，实现 `buildCodeToolCards` 函数，从 `config.yaml::modes.code.tools` 读取工具列表并构建对应 ToolCard。

对齐 Python `_get_tool_cards`，支持 web_free_search、web_fetch_webpage、web_paid_search、user_todos、skill_toolkit 等 code 模式专属工具。

### 4.4 实现 buildCodeSystemPrompt（S08）

**文件**：`internal/swarm/server/adapter/code_adapter.go`（或新建 `code_prompt.go`）

**方案**：实现 `buildCodeSystemPrompt()` 函数，对齐 Python 的 `build_code_system_prompt()` 提示词内容。一比一复刻 Python 原文。

### 4.5 snapshotMemoryFiles 返回类型改为 map[string]bool（G01）

**文件**：`internal/agentcore/memory/lite/coding_memory_tool_ops.go`

**方案**：
- `snapshotMemoryFiles` 返回 `map[string]bool`
- `snapshotEqual` 直接比较 map 长度和键
- 调用方 `fileExists` 判断改为 `snapshot[basename]`

### 4.6 降级写入日志添加冲突字段（G04）

**文件**：`internal/agentcore/memory/lite/coding_memory_tool_ops.go`

**方案**：在降级写入日志中添加 `conflict_detected` 和 `conflicting_files` 字段，对齐 Python。

### 4.7 resolvePromptLanguage 强制返回 "en"（G08）

**文件**：`internal/swarm/server/adapter/code_adapter.go`

**方案**：在 CodeAdapter 中覆写 `resolvePromptLanguage()`，强制返回 "en"。约5行代码。

### 4.8 EmbedQuery 错误处理和日志（G09）

**文件**：`internal/agentcore/memory/lite/manager_impl.go`

**方案**：在 Search 方法中添加 EmbedQuery 的错误处理，超时时记录 warning 日志，异常时记录 error 日志并返回空结果。对齐 Python 的 `_embed_query_with_timeout`。

### 4.9 提取 getBaseDirForFile 独立方法（G10）

**文件**：`internal/agentcore/memory/lite/manager_impl.go`

**方案**：将 `syncMemoryFiles` 中内联的 USER.md 特殊处理逻辑提取为独立方法 `getBaseDirForFile`，对齐 Python 的 `_get_base_dir_for_file`。

### 4.10 EmbeddingProvider 接口添加 Dims()（G11）

**文件**：`internal/agentcore/memory/lite/embeddings.go`

**方案**：在 `EmbeddingProvider` 接口上添加 `Dims() int` 方法，所有实现者补充实现。

### 4.11 添加 isRecentSessionFile（G13）

**文件**：`internal/agentcore/memory/lite/manager_impl.go`

**方案**：添加 `isRecentSessionFile` 函数，对齐 Python 的 `_is_recent_session_file`，只索引今天/昨天的 session 记录。

### 4.12 实现 session 增量状态追踪（G14）

**文件**：`internal/agentcore/memory/lite/manager_impl.go`

**方案**：利用已有的 `SessionDeltaState` 结构体和 `sessionDeltas` 字段，实现 session 增量状态追踪逻辑。对齐 Python 的 `sessions_dirty`/`session_warm`/`_session_pending_files` 等。

### 4.13 AgentName 改为 "main_agent"（G17）

**文件**：`internal/swarm/server/runtime/uapclaw.go`

**方案**：将 4 处 `AgentName: ""` 改为 `AgentName: "main_agent"`。

### 4.14 WorkspaceDir 对齐 Python（G18）

**文件**：`internal/swarm/server/runtime/uapclaw.go`

**方案**：将 `WorkspaceDir: workspace.AgentWorkspaceDir()` 改为对齐 Python 的 `get_agent_home_dir()` 等价路径。需确认 Go 中对应的函数。

### 4.15 Publish 创建消息副本再修改 SenderID（G19）

**文件**：`internal/agent_teams/messager/inprocess.go`

**方案**：在 Publish 方法中，先值拷贝 message 再修改 SenderID，对齐 Python 的 model_copy。

### 4.16 错误信息改为英文（T02）

**文件**：`internal/agentcore/memory/lite/frontmatter.go`

**方案**：将 "缺少必填字段" 改为 "Missing required field"，将 "type 必须是以下之一" 改为 "type must be one of"。

### 4.17 upsertMemoryIndex 添加 Debug 日志（T03）

**文件**：`internal/agentcore/memory/lite/coding_memory_tool_ops.go`

**方案**：在空字段静默返回前添加 Debug 日志。

### 4.18 接口添加 HasLLM()（T06）

**文件**：`internal/agentcore/memory/lite/manager.go`

**方案**：在 `MemoryIndexManager` 接口上添加 `HasLLM() bool` 方法，实现中检查 `llm` 字段是否为 nil。

### 4.19 降级写入错误处理（T07）

**文件**：`internal/agentcore/memory/lite/coding_memory_tool_ops.go`

**方案**：降级写入时检查 WriteFile 错误，失败时记录 Error 日志并返回错误字典。

### 4.20 agent_history 路径修正（T10）

**文件**：`internal/swarm/server/adapter/code_adapter.go`

**方案**：在 CreateInstance 步骤 21 后添加 agent_history 路径修正逻辑，遍历 rail 的 tools 修正 workspace_path。

### 4.21 修正 subscribedTopics 注释（T11）

**文件**：`internal/agent_teams/messager/inprocess.go`

**方案**：将注释从"用于 stop 时清理"改为"用于 Unsubscribe 时移除条目"。

### 4.22 S09/S10/T09 加注释标注

**文件**：`internal/swarm/server/runtime/uapclaw.go`

**方案**：在 S09（is_team_first_request 缺失）、S10（pause/cancel 硬编码）、T09（configure_team_member_agent 缺失）的代码位置添加明确的注释标注，说明这些功能依赖 10.6.19-23 TeamManager 实现。

---

## 五、修改文件清单

| 文件 | 修改项 |
|------|--------|
| `internal/agentcore/memory/lite/coding_memory_tool_ops.go` | S03/S04/S05, G01, G04, T03, T07 |
| `internal/agentcore/memory/lite/manager_impl.go` | G09, G10, G13, G14 |
| `internal/agentcore/memory/lite/manager.go` | T06 |
| `internal/agentcore/memory/lite/embeddings.go` | G11 |
| `internal/agentcore/memory/lite/frontmatter.go` | T02 |
| `internal/swarm/server/adapter/code_adapter.go` | S06, S07, S08, G08, T10 |
| `internal/swarm/server/runtime/uapclaw.go` | G17, G18, S09/S10/T09 注释标注 |
| `internal/agent_teams/messager/inprocess.go` | G19, T11 |
