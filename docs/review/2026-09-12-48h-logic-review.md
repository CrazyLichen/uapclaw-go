# 48 小时代码逻辑审查报告

> 审查时间：2026-09-12
> 审查范围：48 小时内 7 个提交（`e7f153f4..46a224d4`），946 文件变更
> Python 参考：`/home/opensource/agent-core/openjiuwen/` + `/home/opensource/jiuwenswarm-develop/jiuwenswarm/`

---

## 一、提交概览与涉及章节

| 提交 | 描述 | 涉及章节 | 变更文件数 |
|------|------|---------|-----------|
| `e7f153f4` | 2026-09-07 审查 35 项修复 | 9.66/10.6.5/9.24/10.6.3-10 | 30+ |
| `bd375c95` | CI Lint 超时和测试断言修复 | CI | 5 |
| `538364c3` | 残留 interface{} → any 替换 | 全局 | 20+ |
| `1ae5e700` | loadYAML ctx 传播 + 删除已完成审查文档 | 10.6.3-10 | 10+ |
| `59cbd188` | 对齐 Python 修复 2026-09-11 审查 10 项问题 | 10.3.2/10.3.25 | 6 |
| `218c82a9` | gofmt 格式问题 + 声明顺序/注释/any 遗留修正 | 全局 | 120+ |
| `46a224d4` | 修复 SA4006 config.New 返回的 err 未检查 | 10.6.5 | 1 |

**重点审查章节**：10.3.25（Stream Utils）、10.3.2（DeepAdapter）、10.6.5（Permissions）、9.38-49（SkillManager/SkillDev）、10.3.7-11（CodeAdapter）、agentcore/context_engine、evolving/trajectory、agent_teams

---

## 二、问题汇总

| 编号 | 严重级别 | 章节 | 文件 | 问题摘要 |
|------|---------|------|------|---------|
| **严重** | | | | |
| S-01 | 严重 | 10.3.25 | `stream_utils.go:266` | `ask_user_question` chunk_type 不匹配——应为 `chat.ask_user_question` |
| S-02 | 严重 | 10.3.2 | `deep_adapter.go:967` | `accumulatedText` 累加逻辑与 Python 不一致——Python 从不累加 |
| S-03 | 严重 | 10.3.2 | `deep_adapter.go:998-1004` | default 分支 flush 不 emit，已累积文本被静默丢弃 |
| S-04 | 严重 | 10.3.2 | `deep_adapter.go:1014-1020` | 流结束后残留 flush 使用 `chat.delta` 而非 `chat.final` |
| S-05 | 严重 | — | `skill_manager.go:2148` | `setPluginEnabled` 类型断言错误——`[]map[string]any` 对 JSON 反序列化后的数据永远失败 |
| S-06 | 严重 | — | `skill_manager.go:1858-1883` | `HandlePluginsEnable/Disable` 锁外调用 saveState，存在竞态 |
| S-07 | 严重 | — | `skill_manager.go:2290-2301` | `addLocalSkill` 缺少去重逻辑——同名 skill 会被重复添加 |
| S-08 | 严重 | — | `skill_manager.go:2226-2237` | `loadState` 缺少 `_normalize_state` 调用 |
| S-09 | 严重 | — | `remote_import.go:60` | 远程导入缺少 `_assert_import_local_download_url_allowed` 白名单校验，存在 SSRF 风险 |
| S-10 | 严重 | — | `skill_manager.go:1804-1831` | `HandleSkillsTeamSkillsHubDelete` API 端点和参数错误 |
| S-11 | 严重 | — | `skill_manager.go:1737-1799` | `HandleSkillsTeamSkillsHubPublish` 缺少鉴权和关键参数 |
| S-12 | 严重 | 9.24 | `evolution_rail.go:170-183` | `SetTrajectorySink` 未回退 `defaultMemberRole` |
| S-13 | 严重 | 9.24 | `evolution_rail.go:199-252` | `DrainPendingHostEvents` 缺少 timeout=None 时回退超时 |
| S-14 | 严重 | 9.55-9.65 | `spawn_manager.go:222-226` | `RestartTeammate` 缺少 `initialMessage` 和 `sessionID`，重启功能不可用 |
| S-15 | 严重 | agentcore | `task_scheduler.go:171-175` | `Sessions()` 返回内部 map 引用导致数据竞争 |
| S-16 | 严重 | agentcore | `controller.go:290-301` | `Stream` 中 defer 清理块在 Subscribe 失败时仍执行，导致 session 误删 |
| S-17 | 严重 | agentcore | `task_manager.go:766-783` | `notifyIfSubmitted` 在锁内调用回调，与 Python 锁外调用不一致 |
| S-18 | 严重 | context_engine | `session_model_context.go` | `CompressContext` 缺少 `skipped` 状态发射和 `history_start` 追踪 |
| S-19 | 严重 | context_engine | `session_model_context.go:730` | `CompressContext` 缺少 `changed` 判断，始终返回 `compressed` |
| S-20 | 严重 | context_engine | `session_model_context.go:1058` | `_count_single_message_tokens` 使用 `Count` 而非 `CountMessages`，token 统计偏低 |
| S-21 | 严重 | 10.6.5 | `permissions_config_rpc.go:62-69` | `DispatchPermissionsConfigRequest` defer/recover 吞掉 panic 不返回错误响应 |
| S-22 | 严重 | 10.6.5 | `web_handlers.go:1756-1779` | `handlePermissionsOwnerScopesSet` 无法清除 `deny_guidance_message`——空字符串被忽略 |
| S-23 | 严重 | 10.6.5 | `owner_scopes.go:281` | `severityMap` 对非法 key 返回 0（宽松），Python 返回 2（严格） |
| **一般** | | | | |
| M-01 | 一般 | 10.3.25 | `stream_utils.go:257-264` | `context.compression_state` 只提取 5 个固定字段，Python 展开所有键 |
| M-02 | 一般 | 10.3.25 | `stream_utils.go:299` | `message` 分支缺少 `_stage` 参数回退 |
| M-03 | 一般 | 10.3.25 | `stream_utils.go:325` | `stage_result` 分支缺少 `_stage` 参数回退 |
| M-04 | 一般 | 10.3.25 | `stream_utils.go:383-419` | default 分支未透传 payload 所有键，且缺少 `chat.tracer_agent` 处理 |
| M-05 | 一般 | — | `skill_manager.go:2277-2286` | `removeInstalledPlugin` 不调用 `saveState` |
| M-06 | 一般 | — | `skill_manager.go:660-701` | `HandleSkillsUninstall` 不调用 `removeLocalSkill` |
| M-07 | 一般 | — | `skill_manager.go:783-855` | `HandleSkillsMarketplaceAdd/Remove/Toggle` 缺少 `safePathName` 校验 |
| M-08 | 一般 | — | `skill_manager.go:1293-1311` | `HandleSkillsTeamSkillsHubInfo` 缺少 version 必填检查 |
| M-09 | 一般 | — | `skill_manager.go:705-779` | `HandleSkillsImportLocal` 不支持单文件导入 |
| M-10 | 一般 | 10.3.2 | `deep_adapter.go:975-995` | `answer` 分支缺少 `ask_user` 去重检查 |
| M-11 | 一般 | 10.3.2 | `deep_adapter.go:610-636` | `ReloadAgentConfig` 缺少 ACP filesystem rail 注销步骤 |
| M-12 | 一般 | 10.3.2 | `deep_adapter.go:912-1038` | `ProcessMessageStreamImpl` 缺少 evolution watcher 启动和异常捕获 |
| M-13 | 一般 | 10.3.7-11 | `code_adapter.go` | `_RAIL_BUILD_NAMES` 映射不完整——缺少 `LspRail`/`ProjectMemoryRail`/`CodingMemoryRail` 映射 |
| M-14 | 一般 | 10.3.7-11 | `code_adapter.go` | 缺少 `updateRailsForMode` 和 `updateRuntimeConfig` 完整实现 |
| M-15 | 一般 | 10.3.7-11 | `code_adapter.go` | 缺少 `configureTeamMemberAgent` 和 `mergeMemberMcpConfigs` |
| M-16 | 一般 | 9.24 | `evolution_rail.go:729-765` | `safeRunEvolution` 信号量获取不在超时保护下 |
| M-17 | 一般 | evolving | `trajectory/store.go:375-384` | `mapToToolCallDetail` 中 `CallResult` 字符串类型丢失 |
| M-18 | 一般 | 9.55-9.65 | `session_manager.go:122-128` | `BindSession` 缺少 DB 表创建和 Leader 配置持久化 |
| M-19 | 一般 | agentcore | `controller.go:363-371` | 非 OutputSchema 类型 chunk 被丢弃而非透传 |
| M-20 | 一般 | agentcore | `agent_manager.go:182-217` | `GetAgentNoWait` 未区分 mode 为空字符串和 mode 未指定 |
| M-21 | 一般 | context_engine | `session_model_context.go:806-813` | `runAddProcessors` 缺少 `manual`/`passive` trigger 标识 |
| M-22 | 一般 | context_engine | `processor_state_recorder.go:301-308` | `countMessageForStatistic` fallback 行为与 Python 不一致 |
| M-23 | 一般 | context_engine | `message_summary_offloader.go:500-510` | `msoMatchPattern` 使用 `filepath.Match` 与 Python `fnmatch` 不一致 |
| M-24 | 一般 | context_engine | `session_model_context.go:345-520` | `GetContextWindow` 未将 `windowSize`/`sysOperation` 注入 kwargs |
| M-25 | 一般 | 10.6.5 | `permissions_config_rpc.go:197-209` | `DispatchPermissionsConfigRequest` 多出 `owner_scopes.get/set`（Python 没有） |
| M-26 | 一般 | 10.6.5 | `web_handlers.go:1718-1745` | `handlePermissionsOwnerScopesGet` 失败时返回空 map 而非错误 |
| M-27 | 一般 | 10.6.5 | `web_handlers.go:1751-1786` | `handlePermissionsOwnerScopesSet` 与 `UpdatePermissionsOwnerScopesInConfig` 两条独立写入路径 |
| M-28 | 一般 | 10.6.5 | `owner_scopes.go:300-361` | `PersistToOwnerScope` 整体替换 permissions 节可能覆盖并发修改 |
| **提示** | | | | |
| T-01 | 提示 | 10.3.2 | `deep_adapter.go:942-950` | `usage_metadata` 缺少 `metadata` 和 `session_id` 字段 |
| T-02 | 提示 | 10.3.2 | `deep_adapter.go:1022-1034` | `usage_summary` 缺少 `session_id`/`usage`/`model`/`usage_percent` 等字段 |
| T-03 | 提示 | 10.3.2 | `deep_adapter.go:954` | `llm_reasoning` 分支缺少 `output` 字段回退 |
| T-04 | 提示 | 10.3.25 | `stream_utils.go:526-533` | `SerializeValue` 缺少 `date` 和 `Enum` 序列化 |
| T-05 | 提示 | agent_teams | `team_workspace/` | 缺少 `TeamWorkspaceRail` 对应实现 |
| T-06 | 提示 | — | `skill_manager.go:859-916` | `HandleSkillsMarketplaceToggle` 缺少 `_set_marketplace_last_updated` |
| T-07 | 提示 | agentcore | `task_scheduler.go:789-800` | `executeTaskWrapper` recover 缺少栈信息 |
| T-08 | 提示 | context_engine | `message_offloader.go:236-238` | `KeepLastRound` 默认值与 Python 相反 |
| T-09 | 提示 | 10.6.5 | `permissions_persist.go:48-51` | `PersistPermissionAllowRule` merge 失败时缺少 Warn 日志 |
| T-10 | 提示 | 10.6.5 | `owner_scopes.go:126` | `MatchWildcard` 参数顺序与 MatchCommand/MatchURL/MatchPath 不一致 |

---

## 三、严重问题详细分析

### S-01: `ask_user_question` chunk_type 不匹配

**文件**: `internal/swarm/server/utils/stream_utils.go:266`

**问题**: Python 中 `chunk_type == "chat.ask_user_question"`（带 `chat.` 前缀），Go 代码用 `case "ask_user_question"`（无前缀）。OutputSchema.Type 字段在 Python SDK 中值为 `"chat.ask_user_question"`，Go 代码将永远无法匹配到此分支，`ask_user_question` 类型会掉入 default 分支。

**Python 样例**:
```python
# interface_deep.py L5159
if chunk_type == "chat.ask_user_question":
    return {"event_type": "chat.ask_user_question", **(payload if isinstance(payload, dict) else {})}
```

**Go 问题代码**:
```go
// stream_utils.go L266
case "ask_user_question":  // ← 应为 "chat.ask_user_question"
```

**修复方案**: 将 `case "ask_user_question"` 改为 `case "chat.ask_user_question"`，同时更新测试中的 `makeOutput("ask_user_question", ...)` 为 `makeOutput("chat.ask_user_question", ...)`。

---

### S-02: `accumulatedText` 累加逻辑与 Python 不一致

**文件**: `internal/swarm/server/adapter/deep_adapter.go:967`

**问题**: Go 代码在 `llm_output` 分支执行 `accumulatedText += textContent`，Python 代码从未对 `accumulated_text` 执行 `+=` 操作——Python 中 `accumulated_text` 仅在声明时初始化为 `""`，后续只做 flush（yield 后置空），从不累加。Go 多了一个不存在的累积层，导致残留 flush 时产出重复内容。

**Python 样例**:
```python
# Python L4765-4791: llm_output 直接 yield，无 accumulated_text +=
if chunk_type == "llm_output":
    content = chunk.payload.get("content", "") ...
    has_streamed_content = True
    yield AgentResponseChunk(payload={"event_type": "chat.delta", "content": content})
    continue
```

**Go 问题代码**:
```go
accumulatedText += textContent  // ← Python 不存在此行
hasStreamedContent = true
outCh <- schema.NewAgentResponseChunk(..., map[string]any{"event_type": "chat.delta", "content": textContent})
```

**修复方案**: 移除 `accumulatedText += textContent`，`llm_output` 分支应和 Python 一样直接 emit，不累积。同理 `llm_reasoning` 中的 `accumulatedReasoning += reasoningContent` 也应移除。

---

### S-03: default 分支 flush 不 emit，已累积文本被静默丢弃

**文件**: `internal/swarm/server/adapter/deep_adapter.go:998-1004`

**问题**: Go 的 default 分支中 flush `accumulatedText` 和 `accumulatedReasoning` 时仅将变量置空，不产出 chunk。Python L4837-4852 会先 yield `chat.delta`/`chat.reasoning` 再清空。

**Python 样例**:
```python
if accumulated_text:
    yield AgentResponseChunk(payload={"event_type": "chat.delta", "content": accumulated_text})
    accumulated_text = ""
if accumulated_reasoning:
    yield AgentResponseChunk(payload={"event_type": "chat.reasoning", "content": accumulated_reasoning})
    accumulated_reasoning = ""
```

**Go 问题代码**:
```go
if accumulatedText != "" {
    accumulatedText = ""   // ← 直接置空，不 emit
}
if accumulatedReasoning != "" {
    accumulatedReasoning = ""  // ← 直接置空，不 emit
}
```

**修复方案**: flush 时先 emit 再置空：
```go
if accumulatedText != "" {
    outCh <- schema.NewAgentResponseChunk(req.RequestID, req.ChannelID, map[string]any{"event_type": "chat.delta", "content": accumulatedText})
    accumulatedText = ""
}
```

---

### S-04: 流结束后残留 flush 使用 `chat.delta` 而非 `chat.final`

**文件**: `internal/swarm/server/adapter/deep_adapter.go:1014-1020`

**问题**: Go 在 for-range 循环结束后 flush 残留 `accumulatedText`，使用 `event_type: "chat.delta"`。Python L4864-4870 使用 `event_type: "chat.final"`。`chat.final` 表示最终完整答案，前端依赖此区分判断流是否结束。

**Python 样例**:
```python
if accumulated_text:
    yield AgentResponseChunk(payload={"event_type": "chat.final", "content": accumulated_text}, is_complete=False)
```

**Go 问题代码**:
```go
if accumulatedText != "" {
    outCh <- schema.NewAgentResponseChunk(..., map[string]any{"event_type": "chat.delta", "content": accumulatedText})
}
```

**修复方案**: 将 `"chat.delta"` 改为 `"chat.final"`。

---

### S-05: `setPluginEnabled` 类型断言错误——永远返回 false

**文件**: `internal/swarm/server/runtime/skill/skill_manager.go:2148`

**问题**: `setPluginEnabled` 将 `state["installed_plugins"]` 断言为 `[]map[string]any`，但 state 从 JSON 反序列化后，切片元素的类型是 `[]any`（每个元素是 `map[string]any` 包在 `any` 里），直接断言 `[]map[string]any` 一定失败，函数永远返回 false。

**Python 样例**:
```python
def _set_plugin_enabled(self, name, enabled):
    plugins = self._state.get("installed_plugins", [])  # 直接遍历
    for p in plugins:
        if p.get("name") == name:
            p["enabled"] = bool(enabled)
            updated = True
            break
    if not updated: return False
    self._save_state()
    return True
```

**Go 问题代码**:
```go
func (sm *SkillManager) setPluginEnabled(name string, enabled bool) bool {
    plugins, _ := sm.state["installed_plugins"].([]map[string]any)  // ← 永远 false
    if plugins == nil { return false }
    ...
}
```

**修复方案**: 使用 `GetInstalledPlugins()` 替代直接类型断言（该方法内部已处理 `[]any` → `[]map[string]any` 转换），修改后的 plugins 写回 `state["installed_plugins"]`（通过 `mapSliceToAny` 转换）。

---

### S-06: `HandlePluginsEnable/Disable` 锁外调用 saveState

**文件**: `internal/swarm/server/runtime/skill/skill_manager.go:1858-1883`

**问题**: `HandlePluginsEnable` 在 `sm.mu.Unlock()` **之后**调用 `sm.saveState()`。Python 中 `_set_plugin_enabled` **自身内部**调用 `_save_state()`，且在遍历+修改完成后立即保存。Go 的实现把保存放到锁外，存在竞态。

**Python 样例**:
```python
def _set_plugin_enabled(self, name, enabled):
    # ... 修改 state
    self._save_state()  # ← 在修改完成后立即保存，Python 无锁但 asyncio 单线程
    return True
```

**Go 问题代码**:
```go
sm.mu.Lock()
ok := sm.setPluginEnabled(name, true)  // 修改 state 但不保存
sm.mu.Unlock()
sm.saveState()  // ← 锁外保存，存在竞态
```

**修复方案**: 将 `saveState()` 移到锁内，或在 `setPluginEnabled` 内部调用 `saveState()`（对齐 Python）。

---

### S-07: `addLocalSkill` 缺少去重逻辑

**文件**: `internal/swarm/server/runtime/skill/skill_manager.go:2290-2301`

**问题**: Go 的 `addLocalSkill` 只简单 append，不检查同名技能。Python 的 `_add_local_skill` 先遍历已有记录，存在同名则替换。

**Python 样例**:
```python
def _add_local_skill(self, skill):
    local = self._state.setdefault("local_skills", [])
    for i, s in enumerate(local):
        if s.get("name") == skill.get("name"):
            local[i] = skill  # ← 替换已有记录
            self._save_state()
            return
    local.append(skill)
    self._save_state()
```

**Go 问题代码**:
```go
func (sm *SkillManager) addLocalSkill(skill map[string]any) {
    // ...
    list = append(list, skill)  // ← 无去重，直接追加
    sm.state["local_skills"] = list
}
```

**修复方案**: append 前遍历 list，如果已有同名 name 则替换该条记录。

---

### S-08: `loadState` 缺少 `_normalize_state` 调用

**文件**: `internal/swarm/server/runtime/skill/skill_manager.go:2226-2237`

**问题**: Go 的 `loadState` 加载 JSON 后直接返回，不做任何规范化。Python 的 `_load_state` 调用 `_normalize_state(state)`，补全默认字段、过滤已删除技能、规范化 marketplaces/skill_configs。

**Python 样例**:
```python
def _normalize_state(self, state):
    state.setdefault("marketplaces", [])
    state.setdefault("installed_plugins", [])
    state.setdefault("local_skills", [])
    state.setdefault("skill_configs", {})
    state["marketplaces"] = self.normalize_marketplaces(state.get("marketplaces"))
    state["local_skills"] = normalize_local_skills(state.get("local_skills"), self._collect_existing_local_skill_names())
    state["skill_configs"] = normalize_skill_configs(state.get("skill_configs"))
```

**Go 问题代码**:
```go
func (sm *SkillManager) loadState() map[string]any {
    data, err := os.ReadFile(sm.stateFile)
    if err != nil { return make(map[string]any) }  // ← 空状态，缺少默认字段
    var state map[string]any
    json.Unmarshal(data, &state)
    return state  // ← 无规范化
}
```

**修复方案**: 实现 `normalizeState` 方法，在 `loadState` 成功后调用，补全 `marketplaces`/`installed_plugins`/`local_skills`/`skill_configs` 默认值，扫描磁盘目录过滤无效 local_skills。

---

### S-09: 远程导入缺少白名单校验——SSRF 风险

**文件**: `internal/swarm/server/runtime/skill/remote_import.go:60`

**问题**: Go 的 `importSkillFromRemoteArchive` 不检查下载 URL 是否在白名单中。Python 调用 `_assert_import_local_download_url_allowed`，要求 HTTPS 且主机名在白名单。

**Python 样例**:
```python
@staticmethod
def _assert_import_local_download_url_allowed(download_url):
    parsed = urlparse(download_url)
    if parsed.scheme != "https":
        raise RuntimeError("远程导入 URL 必须使用 HTTPS")
    host = (parsed.hostname or "").strip().lower()
    if not host:
        raise RuntimeError("远程导入 URL 缺少主机名")
    for rule in SkillManager._get_import_local_allowed_download_hosts():
        if rule.startswith("."):
            if host.endswith(rule): return
            continue
        if SkillManager._team_skills_hub_host_matches_rule(host, rule): return
    raise RuntimeError(f"远程导入 URL host 不在白名单: {host}")
```

**修复方案**: 添加 `assertImportLocalDownloadURLAllowed` 函数，在 `importSkillFromRemoteArchive` 下载前调用，实现 HTTPS 强制 + 主机名白名单匹配（支持后缀匹配和段级通配）。

---

### S-10: `HandleSkillsTeamSkillsHubDelete` API 端点和参数错误

**文件**: `internal/swarm/server/runtime/skill/skill_manager.go:1804-1831`

**问题**: Go 的 Delete 使用 `DELETE /api/v1/artifacts/{asset_id}`，参数为 `asset_id`。Python 使用 `DELETE /api/v1/plugins/{skill_id}/versions/{version}`，参数为 `skill_id` 和 `version`，且需要鉴权。

**Python 样例**:
```python
req_url = f"{base_url}/api/v1/plugins/{quote(skill_id, safe='')}/versions/{quote(version, safe='')}"
headers = {}
if system_token:
    headers["X-System-Token"] = system_token
else:
    headers["Authorization"] = f"Bearer {token}"
```

**Go 问题代码**:
```go
assetID := trimSpace(toString(params["asset_id"]))  // ← 应为 skill_id
req, _ := http.NewRequestWithContext(ctx, http.MethodDelete, baseURL+"/api/v1/artifacts/"+assetID, nil)
// ← 缺少 Authorization / X-System-Token 头
```

**修复方案**: 1) 参数改为 `skill_id`；2) 添加 `version` 参数（默认 `"all"`）；3) 修改 API 端点为 `/api/v1/plugins/{skill_id}/versions/{version}`；4) 添加鉴权头。

---

### S-11: `HandleSkillsTeamSkillsHubPublish` 缺少鉴权和关键参数

**文件**: `internal/swarm/server/runtime/skill/skill_manager.go:1737-1799`

**问题**: Go 的 Publish 缺少鉴权（`_resolve_teamskills_hub_auth`）、`skill_id`/`plugin_id`、`version_desc`、`force` 等参数。Python 使用 `POST /api/v1/plugins` 并传递完整 form data 和鉴权头。

**Python 样例**:
```python
data = {"force": "true" if force else "false", "version_desc": version_desc, "plugin_version": plugin_version}
if plugin_id: data["plugin_id"] = plugin_id
headers = {"X-Checksum-SHA256": checksum_sha256}
if system_token: headers["X-System-Token"] = system_token
else: headers["Authorization"] = f"Bearer {token}"
```

**修复方案**: 1) 实现鉴权解析；2) 提取 `skill_id`/`plugin_id`、`version_desc`、`force` 参数；3) 作为 form data 传递；4) 添加鉴权头。

---

### S-12: `SetTrajectorySink` 未回退 `defaultMemberRole`

**文件**: `internal/agentcore/harness/rails/evolution/evolution_rail.go:170-183`

**问题**: Python 的 `set_trajectory_sink` 在 `member_role is None` 时回退到 `self._DEFAULT_MEMBER_ROLE`（`SkillEvolutionRail` = "teammate"，`TeamSkillEvolutionRail` = "leader"）。Go 在 `memberRole` 省略或为空时完全跳过角色赋值。

**Python 样例**:
```python
role = self._DEFAULT_MEMBER_ROLE if member_role is None else member_role
self._member_role = _normalize_member_role(role)
```

**Go 问题代码**:
```go
if len(memberRole) > 0 && memberRole[0] != "" {
    role := normalizeMemberRole(memberRole[0])
    if role != nil { r.memberRole = *role }
}
// ⚠️ 当 memberRole 为空时，不会回退到 r.defaultMemberRole
```

**修复方案**:
```go
role := ""
if len(memberRole) > 0 && memberRole[0] != "" {
    role = memberRole[0]
} else {
    role = r.defaultMemberRole  // 回退
}
if normalized := normalizeMemberRole(role); normalized != nil {
    r.memberRole = *normalized
}
```

---

### S-13: `DrainPendingHostEvents` 缺少 timeout=None 回退

**文件**: `internal/agentcore/harness/rails/evolution/evolution_rail.go:199-252`

**问题**: Python 在 `wait=True` 且 `timeout=None` 时回退到 `self._get_evolution_total_timeout_secs()`。Go 在 `timeout` 为 nil 时直接走无限等待。

**Python 样例**:
```python
if wait and timeout is None:
    timeout = self._get_evolution_total_timeout_secs()
```

**修复方案**:
```go
if wait && timeout == nil {
    secs := r.ext.GetEvolutionTotalTimeoutSecs()
    if secs > 0 {
        d := time.Duration(secs * float64(time.Second))
        timeout = &d
    }
}
```

---

### S-14: `RestartTeammate` 缺少 `initialMessage` 和 `sessionID`

**文件**: `internal/agent_teams/agent/spawn_manager.go:222-226`

**问题**: `initialMessage` 和 `sessionID` 硬编码为空字符串，标记 `⤵️ 待回填`，但这是重启逻辑的关键参数。缺少 `initialMessage` 意味着重启后的 teammate 不会收到初始提示。

**Python 样例**:
```python
teammate = await team_backend.get_member(member_name)
initial_message = teammate.prompt if teammate else None
await self.spawn_teammate(ctx, initial_message=initial_message, session=get_session_id() or None, ...)
```

**Go 问题代码**:
```go
initialMessage := "" // ⤵️ 待回填：应传入 teammate.prompt
sessionID := ""      // ⤵️ 待回填：应传入 get_session_id()
```

**修复方案**: 实现 `BuildContextFromDB` 从 DB 读取 teammate 信息获取 prompt，sessionID 从 `SessionState` 获取。

---

### S-15: `Sessions()` 返回内部 map 引用导致数据竞争

**文件**: `internal/agentcore/controller/modules/task_scheduler.go:171-175`

**问题**: `Sessions()` 获取锁后返回 `s.sessions` 引用并立即释放锁。Controller 中 `c.taskScheduler.Sessions()[sessionID] = sess` 在锁释放后操作内部 map，与 `schedule()`/`PauseTask()` 等产生数据竞争。

**Python 样例**:
```python
# Python: asyncio 单线程，无需担心数据竞争
self._task_scheduler.sessions[session_id] = session
```

**Go 问题代码**:
```go
func (s *TaskScheduler) Sessions() map[string]sessioninterfaces.SessionFacade {
    s.mu.Lock()
    defer s.mu.Unlock()
    return s.sessions  // 返回内部引用！
}
// Controller 中无锁操作：
c.taskScheduler.Sessions()[sessionID] = sess  // 数据竞争
```

**修复方案**: 增加 `AddSession(sessionID, sess)` 和 `RemoveSession(sessionID)` 方法，内部持锁操作，替代暴露内部引用。

---

### S-16: `Stream` 中 defer 清理块在 Subscribe 失败时仍执行

**文件**: `internal/agentcore/controller/controller.go:290-301`

**问题**: `Stream()` 中步骤 2 注册 session 后立即设置了 defer 清理块。如果步骤 3 Subscribe 失败后 return，defer 仍执行 Unsubscribe 和 delete session，但此时 session 可能未正确初始化。

**Python 样例**:
```python
try:
    await self._event_queue.subscribe(agent_id, session_id)
    await self._event_queue.publish_event(agent_id, session, inputs)
except BaseError:
    raise
except Exception as e:
    raise build_error(...) from e
finally:
    await self._event_queue.unsubscribe(agent_id, session_id)
    if session_id in self._task_scheduler.sessions:
        del self._task_scheduler.sessions[session_id]
```

**Go 问题代码**:
```go
c.taskScheduler.Sessions()[sessionID] = sess
defer func() {
    _ = c.eventQueue.Unsubscribe(ctx, agentID, sessionID)  // 订阅失败时也会执行
    delete(c.taskScheduler.Sessions(), sessionID)
}()
if err := c.eventQueue.Subscribe(ctx, agentID, sessionID); err != nil {
    return  // defer 仍执行
}
```

**修复方案**: 将 defer 注册移到 Subscribe 成功之后，或增加 `subscribed` 标记控制 defer 中是否执行 Unsubscribe 和 delete。

---

### S-17: `notifyIfSubmitted` 在锁内调用回调

**文件**: `internal/agentcore/controller/modules/task_manager.go:766-783`

**问题**: `notifyIfSubmitted()` 在 `AddTask()` 持有 `tm.mu` 写锁时被调用。Python 在锁释放后调用。

**Python 样例**:
```python
async def add_task(self, task):
    async with self._lock:
        ...
    # Notify outside lock
    self._notify_if_submitted(tasks)
```

**Go 问题代码**:
```go
func (tm *TaskManager) AddTask(_ context.Context, task *schema.Task) error {
    tm.mu.Lock()
    defer tm.mu.Unlock()
    ...
    tm.notifyIfSubmitted([]*schema.Task{task})  // 锁内调用
    return nil
}
```

**修复方案**: 在 `AddTask()` 和 `UpdateTaskStatus()` 中，锁释放后再调用 `notifyIfSubmitted()`。

---

### S-18: `CompressContext` 缺少 `skipped` 状态发射和 `history_start` 追踪

**文件**: `internal/agentcore/context_engine/context/session_model_context.go`

**问题**: Python `compress_context` 在锁忙和"无匹配处理器"时发射 `status="skipped"` 状态，并用 `history_start` 追踪本次压缩开始位置。Go 完全缺失这两项。

**Python 样例**:
```python
history_start = len(self._processor_state_recorder.history())
await self._build_and_emit_compression_state(ContextProcessorStateInput(
    operation_id=uuid.uuid4().hex, status="skipped", phase="active_compress",
    trigger="manual", processor=None, reason="busy", ...))
return self._build_active_compression_result("busy", return_state, history_start=history_start)
```

**修复方案**: 1) 锁忙和"无匹配处理器"时调用 `mc.stateRecorder.Emit()` 发射 `skipped` 状态；2) 入口记录 `historyStart := len(mc.stateRecorder.History())`；3) 传入 `buildActiveCompressionResult` 只从 `history[historyStart:]` 中选取状态。

---

### S-19: `CompressContext` 缺少 `changed` 判断

**文件**: `internal/agentcore/context_engine/context/session_model_context.go:730`

**问题**: Python 在 `_run_add_processors` 后根据返回值 `changed` 决定返回 `"compressed"` 或 `"noop"`。Go 始终返回 `"compressed"`。

**Python 样例**:
```python
changed, _ = await self._run_add_processors(...)
if changed:
    return self._build_active_compression_result("compressed", ...)
return self._build_active_compression_result("noop", ...)
```

**修复方案**: `runAddProcessors` 应返回 `changed bool`，`CompressContext` 根据返回值决定结果。

---

### S-20: `_count_single_message_tokens` 使用 `Count` 而非 `CountMessages`

**文件**: `internal/agentcore/context_engine/context/session_model_context.go:1058-1069`

**问题**: Python 使用 `self._token_counter.count_messages([message])`，Go 使用 `mc.tokenCounter.Count(content, modelName)`。`Count` 只计算纯文本 token，`CountMessages` 会考虑消息结构开销，导致 Go 的 token 统计偏低。

**Python 样例**:
```python
def _count_single_message_tokens(self, message):
    if self._token_counter is not None:
        return self._token_counter.count_messages([message])
```

**Go 问题代码**:
```go
func (mc *SessionModelContext) countSingleMessageTokens(msg llm_schema.BaseMessage) int {
    content := msg.GetContent().Text()
    count, err := mc.tokenCounter.Count(content, mc.resolveContextModelName())  // ← Count 而非 CountMessages
```

**修复方案**: 改为 `mc.tokenCounter.CountMessages([]llm_schema.BaseMessage{msg}, modelName)`。

---

### S-21: `DispatchPermissionsConfigRequest` defer/recover 吞掉 panic 不返回错误响应

**文件**: `internal/swarm/agents/harness/common/rails/permissions/permissions_config_rpc.go:62-69`

**问题**: defer/recover 捕获 panic 后只记录日志，未返回错误 payload。recover 触发时返回 `(false, nil)`（Go 零值），调用方无法区分"未知方法"和"内部 panic"。Python 用 `except Exception` 返回 `_err(request, str(e), code="INTERNAL_ERROR")`。

**Python 样例**:
```python
except Exception as e:
    logger.exception("[%s] %s", tag, e)
    return _err(request, str(e), code="INTERNAL_ERROR")
```

**Go 问题代码**:
```go
defer func() {
    if rec := recover(); rec != nil {
        logger.Error(permRpcLogComponent).Any("recover", rec).Str("req_method", reqMethod).
            Msg("[permissions_config_rpc] dispatch 异常恢复")
        // 缺少：返回错误响应
    }
}()
```

**修复方案**: 将函数签名改为 named return `func DispatchPermissionsConfigRequest(...) (ok bool, payload map[string]any)`，在 recover 中设置 `ok = false; payload = map[string]any{"error": fmt.Sprintf("internal error: %v", rec), "code": "INTERNAL_ERROR"}`。

---

### S-22: `handlePermissionsOwnerScopesSet` 无法清除 `deny_guidance_message`

**文件**: `internal/swarm/gateway/channel_manager/web/web_handlers.go:1756-1779`

**问题**: Go 代码用 `denyGuidance != ""` 判断是否写入，客户端传空字符串想清除该字段时不会写入。Python 用 `if deny_guidance_message is not None` 判断，空字符串会写入并生效。

**Python 样例**:
```python
deny_guidance = params.get("deny_guidance_message")
if deny_guidance_message is not None:
    data["permissions"]["deny_guidance_message"] = deny_guidance_message
```

**Go 问题代码**:
```go
denyGuidance, _ := params["deny_guidance_message"].(string)
if denyGuidance != "" {
    permCfg["deny_guidance_message"] = denyGuidance
}
```

**修复方案**: 使用 key 存在性检查而非空字符串检查：
```go
if dg, ok := params["deny_guidance_message"]; ok {
    permCfg["deny_guidance_message"] = dg
}
```

---

### S-23: `severityMap` 对非法 key 返回 0（宽松），Python 返回 2（严格）

**文件**: `internal/swarm/agents/harness/common/rails/permissions/owner_scopes.go:281`

**问题**: Go 的 `severityMap[level]` 当 key 不存在时返回 int 零值 0，等价于 "allow"（最宽松）。Python 的 `_severity.get(level, 2)` 默认值为 2（"deny"，最严格）。如果 `level` 返回意外字符串，Go 取最宽松级别，Python 取最严格级别。

**Python 样例**:
```python
if global_level_value is not None and _severity.get(global_level_value, 2) > _severity.get(level, 2):
    final_level = global_level_value
```

**Go 问题代码**:
```go
globalSeverity, globalOk := severityMap[globalLevelStr]
if globalOk && globalSeverity > severityMap[level] {
```

**修复方案**: 使用带默认值的查找：
```go
severityOf := func(s string) int {
    if v, ok := severityMap[s]; ok { return v }
    return 2  // 对齐 Python 默认值
}
if globalOk && severityOf(globalLevelStr) > severityOf(level) {
```

---

## 四、一般问题详细分析

### M-01: `context.compression_state` 只提取 5 个固定字段

**文件**: `internal/swarm/server/utils/stream_utils.go:257-264`

Python `interface_deep.py` 使用 `**state_payload` 展开 payload 全部键，Go 只提取 5 个固定字段。

**修复方案**: 展开所有键到返回值：`for k, v := range payload { result[k] = SerializeValue(v) }`，确保 `event_type` 为 `"context.compression_state"`。

---

### M-02/M-03: `message`/`stage_result` 分支缺少 `_stage` 参数回退

**文件**: `stream_utils.go:299, 325`

Python 的 `_parse_stream_chunk` 有 `_stage` 参数，当 payload 中无 `stage` 字段时作为 fallback。Go 无此参数。

**修复方案**: 给 `ParseStreamChunk` 添加可选的 `stage ...string` variadic 参数。

---

### M-04: default 分支未透传 payload 所有键

**文件**: `stream_utils.go:383-419`

Python 的 default 分支对非 `team.` 事件将 payload 所有键透传（`**{k: _serialize_value(v) for k, v in payload.items()}`），Go 只提取 `content`/`output`。

**修复方案**: 对齐 Python — 当 payload 是 dict 且不含 team. event_type 时，返回 `{"event_type": "chat." + chunkType, ...payload}` 透传所有键。

---

### M-05: `removeInstalledPlugin` 不调用 `saveState`

**文件**: `skill_manager.go:2277-2286`

Python 的 `_remove_installed_plugin` 在移除后立即调用 `_save_state()`。Go 不保存。

**修复方案**: 在 `removeInstalledPlugin` 末尾添加 `sm.saveState()`。

---

### M-06: `HandleSkillsUninstall` 不调用 `removeLocalSkill`

**文件**: `skill_manager.go:660-701`

Python 的 `handle_skills_uninstall` 同时调用 `_remove_installed_plugin` 和 `_remove_local_skill`。Go 只调用了 `removeInstalledPlugin`。

**修复方案**: 先实现 `removeLocalSkill` 方法，然后在 `HandleSkillsUninstall` 中添加调用。

---

### M-07: Marketplace 相关方法缺少 `safePathName` 校验

**文件**: `skill_manager.go:783-916`

Python 的 `handle_skills_marketplace_add/remove` 都调用 `_safe_path_name(name, "marketplace")`。Go 缺少此校验。

**修复方案**: 在 `HandleSkillsMarketplaceAdd/Remove/Toggle` 中添加 `safePathName(name, "marketplace")` 校验。

---

### M-08/M-09: TeamSkillsHub 和 ImportLocal 缺少校验

- M-08: `HandleSkillsTeamSkillsHubInfo` 缺少 version 必填检查
- M-09: `HandleSkillsImportLocal` 不支持单文件导入

---

### M-10: `answer` 分支缺少 `ask_user` 去重

**文件**: `deep_adapter.go:975-995`

Python 在 `answer` 分支 `ParseStreamChunk` 后检查 `should_skip_duplicate_ask_user`，Go 缺少此逻辑。

**修复方案**: 在所有 `ParseStreamChunk` 调用后检查 `parsed["event_type"] == "chat.ask_user_question"` 并做 `emittedAskUserIDs` 去重。

---

### M-11/M-12: DeepAdapter 缺少 ReloadAgentConfig 步骤和异常捕获

- M-11: `ReloadAgentConfig` 缺少 ACP filesystem rail 注销
- M-12: `ProcessMessageStreamImpl` 缺少 evolution watcher 启动和 goroutine 顶层 recover

---

### M-13/M-14/M-15: CodeAdapter 缺失方法

- M-13: `_RAIL_BUILD_NAMES` 缺少 `LspRail`/`ProjectMemoryRail`/`CodingMemoryRail` 映射（Python 有 15 个，Go 只有 13 个）
- M-14: 缺少 `updateRailsForMode` 和 `updateRuntimeConfig` 完整实现（当前为占位 `⤵️`）
- M-15: 缺少 `configureTeamMemberAgent` 和 `mergeMemberMcpConfigs`

---

### M-16/M-17/M-18: Evolving/Agent_teams 问题

- M-16: `safeRunEvolution` 信号量获取不在超时保护下
- M-17: `mapToToolCallDetail` 中 `CallResult` 字符串类型丢失（`toMapAny` 返回 nil）
- M-18: `BindSession` 缺少 DB 表创建和 Leader 配置持久化

---

### M-19/M-20: Controller/AgentManager 问题

- M-19: 非 OutputSchema 类型 chunk 被丢弃而非透传
- M-20: `GetAgentNoWait` 未区分 mode 为空字符串和 mode 未指定

---

### M-21-M-24: Context_engine 问题

- M-21: `runAddProcessors` 缺少 `manual`/`passive` trigger 标识
- M-22: `countMessageForStatistic` fallback 行为与 Python 不一致（无 tokenCounter 时 Python 返回 0，Go 返回 `len/4`）
- M-23: `msoMatchPattern` 使用 `filepath.Match` 与 Python `fnmatch` 不一致
- M-24: `GetContextWindow` 未将 `windowSize`/`sysOperation` 注入 kwargs

### M-25-M-28: Permissions 问题

- M-25: `DispatchPermissionsConfigRequest` 多出 `owner_scopes.get/set`（Python 的 `_PERMISSIONS_CFG_METHODS` 不含 owner_scopes），导致 E2A 回退路径与 Web 直连路径锁行为不一致
- M-26: `handlePermissionsOwnerScopesGet` 失败时返回空 map 而非错误，客户端无法区分"配置为空"和"加载失败"
- M-27: `handlePermissionsOwnerScopesSet` 与 `UpdatePermissionsOwnerScopesInConfig` 两条独立写入路径（Web 用 `config.New/Save`，RPC 用 `readYAMLData/writeYAMLData`），应统一
- M-28: `PersistToOwnerScope` 使用 `WritePermissionsSectionToAgentConfigYAML` 整体替换 permissions 节，可能覆盖并发修改。Python 在 `persistLock` 内用 `get_config_raw()` 读取最新配置再就地修改

---

## 五、提示问题

| 编号 | 文件 | 问题 |
|------|------|------|
| T-01 | `deep_adapter.go:942-950` | `usage_metadata` 缺少 `metadata` 和 `session_id` 字段 |
| T-02 | `deep_adapter.go:1022-1034` | `usage_summary` 缺少 `session_id`/`usage`/`model`/`usage_percent` |
| T-03 | `deep_adapter.go:954` | `llm_reasoning` 缺少 `output` 字段回退 |
| T-04 | `stream_utils.go:526-533` | `SerializeValue` 缺少 `date` 和 `Enum` 序列化 |
| T-05 | `team_workspace/` | 缺少 `TeamWorkspaceRail` 对应实现（Python: before/after_tool_call） |
| T-06 | `skill_manager.go:859-916` | `HandleSkillsMarketplaceToggle` 缺少 `_set_marketplace_last_updated` |
| T-07 | `task_scheduler.go:789-800` | `executeTaskWrapper` recover 缺少栈信息 |
| T-08 | `message_offloader.go:236-238` | `KeepLastRound` 默认值与 Python 相反 |
| T-09 | `permissions_persist.go:48-51` | `PersistPermissionAllowRule` merge 失败时缺少 Warn 日志 |
| T-10 | `owner_scopes.go:126` | `MatchWildcard` 参数顺序与 MatchCommand/MatchURL/MatchPath 不一致 |

---

## 六、优先级建议

**P0 — 必须立即修复（影响核心功能）**：
- S-01（ask_user_question chunk_type 不匹配导致分支永远不命中）
- S-05（setPluginEnabled 类型断言错误导致插件操作永远失败）
- S-02/S-03/S-04（流式输出逻辑错误导致内容重复/丢失/事件类型错误）
- S-15（数据竞争——可能导致运行时 panic）
- S-21（permissions defer/recover 吞掉 panic 不返回错误）

**P1 — 尽快修复（功能缺失或安全风险）**：
- S-09（SSRF 风险——远程导入无白名单校验）
- S-10/S-11（TeamSkillsHub API 端点和鉴权错误）
- S-16（session 误删）
- S-17（锁内回调可能死锁）
- S-18/S-19（context_engine 压缩状态不完整）
- S-22（无法清除 deny_guidance_message）
- S-23（severityMap 非法 key 默认宽松，安全风险）

**P2 — 计划修复（逻辑不一致或待回填功能）**：
- S-06/S-07/S-08（skill_manager 状态管理问题）
- S-12/S-13/S-14（evolution/team 功能缺失）
- S-20（token 统计偏低）
- M-01~M-28（一般问题）

---

## 七、统计

| 严重级别 | 数量 |
|---------|------|
| 严重 | 23 |
| 一般 | 28 |
| 提示 | 10 |
| **总计** | **61** |
