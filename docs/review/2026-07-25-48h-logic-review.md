# 48小时代码逻辑审查报告

> 审查日期：2026-07-25
> 审查范围：提交 `5a20e9a` (refactor: 代码规范对齐 + session 元数据增量更新功能)
> 对比基准：Python 参考项目源码（openjiuwen / jiuwenswarm）
> 审查重点：方法签名/步骤一致性、⤵️占位代码验证、逻辑正确性

## 审查章节概览

| 章节 | 内容 | 变更文件数 | 状态 |
|------|------|-----------|------|
| 10.3.15-18 | Session 元数据增量更新 | 2 | ✅ 已完成 |
| 9.70-9.80 | Evolving 模块（optimizer/signal/schema/tool_call） | 25 | ✅ 已完成 |
| 10.3.2-6 | Adapter 代码规范对齐 | 7 | ✅ 已完成 |
| 11.1-3 | Gateway MessageHandler 代码规范对齐 | 6 | ✅ 已完成 |
| 6.x | agentcore 代码规范对齐（operator/callback/resources_manager 等） | 30+ | ✅ 已完成 |
| 9.56-9.60 | Agent Teams 代码规范对齐 | 15 | ✅ 已完成 |

> 说明：本次提交以代码规范对齐为主，session 元数据增量更新为新功能逻辑。审查重点放在逻辑一致性和功能正确性上。

## 问题统计

| 严重程度 | 数量 |
|---------|------|
| 严重 | 20 |
| 一般 | 22 |
| 提示 | 15 |---

## 一、Session 元数据管理（2 严重 / 3 一般 / 2 提示）

### S-01：[严重] AppendHistoryRecord 不触发 metadata 更新

Python 的 `append_history_record` 在写入历史记录后会调用 `update_session_metadata`（递增 message_count、更新 last_message_at、自动生成标题）和 `set_session_delivery_context`。Go 的 `AppendHistoryRecord` 只写 history 记录，完全不同步更新 metadata。

**Python 参考代码** (`session_history.py:120-135`):
```python
def append_history_record(session_id, record):
    # ... 写入 history.jsonl ...
    # 同步更新 metadata
    update_session_metadata(
        session_id=session_id,
        increment_message_count=True,
        user_content=user_content,
    )
    set_session_delivery_context(
        session_id=session_id,
        channel_id=msg_channel_id,
        source_request_id=msg_request_id,
        route_metadata=route_metadata,
    )
```

**Go 问题代码** (`session_history.go:51-101`):
```go
func AppendHistoryRecord(sessionsDir, sessionID string, record HistoryRecord) error {
    // ... 只写 history.jsonl，没有调用 updateSessionMetadata 或 SetSessionDeliveryContext
    return nil
}
```

**修复方案**：在 `AppendHistoryRecord` 写入成功后，调用 `updateSessionMetadata` 和 `SetSessionDeliveryContext`，与 Python 对齐：
```go
// 写入成功后同步更新 metadata
updateSessionMetadata(SessionMetadataUpdate{
    SessionID:              sessionID,
    IncrementMessageCount:  true,
    UserContent:            userContent,
})
```

---

### S-02：[严重] handleSessionRename 初始化 metadata 缺少 user_id 字段

**Python 参考代码** (`session_metadata.py:148-160`):
```python
metadata = {
    "session_id": session_id,
    "channel_id": channel_id,
    "user_id": user_id,        # ← Python 显式设置 user_id
    "created_at": _current_timestamp(),
    "last_message_at": _current_timestamp(),
    "title": title,
    "message_count": 0,
    "mode": mode,
    "team_name": team_name,
    "round_id": 0,
}
```

**Go 问题代码** (`handle_session.go:441-453`):
```go
meta = map[string]any{
    "session_id":      target,
    "channel_id":      "",
    // ← 缺少 "user_id" 字段！
    "created_at":      currentTimestamp(),
    "last_message_at": currentTimestamp(),
    "title":           "",
    "message_count":   0,
    "mode":            "unknown",
    "team_name":       "",
    "round_id":        0,
}
```

**修复方案**：补充 `"user_id": ""` 字段。同时检查文件中所有手动构造 metadata 的地方（`SetSessionDeliveryContext`、`handleSessionCreate`）确保都包含 `user_id` 和 `team_name`。

---

### G-01：[一般] SetSessionDeliveryContext 使用同步写入，绕过异步队列

**Python 参考代码** (`session_metadata.py:230-232`):
```python
_enqueue_write(session_id, metadata)  # 异步队列写入
```

**Go 问题代码** (`handle_session.go:242-244`):
```go
if err := writeSessionMetadata(GetSessionsDir(), sessionID, meta); err != nil {  // 同步写入
    logger.Warn(logComponent).Str("session_id", sessionID).Err(err).Msg("写入会话元数据失败")
}
```

**修复方案**：将 `writeSessionMetadata(...)` 改为 `enqueueMetadataWrite(sessionID, meta)`，与 `updateSessionMetadata` 保持一致。

---

### G-02：[一般] handleSessionCreate 返回字段名与 Python 不一致

**Python 参考代码** (`agent_ws_server.py`):
```python
payload={"sessionId": session_id}  # camelCase
```

**Go 问题代码** (`handle_session.go:625`):
```go
schema.WithPayload(map[string]any{"session_id": sessionID})  // snake_case
```

**修复方案**：将 `"session_id"` 改为 `"sessionId"` 以保持前端兼容性，或确认前端已适配 snake_case。

---

### G-03：[一般] handleSessionDelete 缺少前置校验和运行时状态清理

**Python 参考代码** (`agent_ws_server.py`):
```python
# 1. 检查 session 目录是否存在
# 2. 检查是否为目录
# 3. 调用 Runner.release() 清理运行时状态
# 4. 成功后 shutil.rmtree + remove_session_metadata_cache
```

**Go 问题代码** (`handle_session.go:505-549`):
```go
// 直接 os.RemoveAll + delete cache，没有前置校验和运行时清理
if err := os.RemoveAll(sessionDir); err != nil { ... }
```

**修复方案**：补充目录存在性检查和运行时状态清理（`Runner.release` 等价逻辑），等 10.3.12 AgentManager 回填后补充。

---

### T-01：[提示] updateSessionMetadata 中 message_count 初始化冗余

**Go 问题代码** (`handle_session.go:806-822`):
```go
metadata = map[string]any{
    ...
    "message_count": 1,  // ← 硬编码 1，下面立即被覆盖
}
if update.IncrementMessageCount {
    metadata["message_count"] = 1
} else {
    metadata["message_count"] = 0
}
```

**修复方案**：删除第 812 行的硬编码 `"message_count": 1`，仅保留 if-else 块。

---

### T-02：[提示] incrementSessionRoundCount 使用同步写入

同 G-01 模式，`incrementSessionRoundCount` 直接调用 `writeSessionMetadata` 而非 `enqueueMetadataWrite`。

---

## 二、Evolving / Signal 模块（3 严重 / 3 一般 / 3 提示）

### S-03：[严重] matchFailureKeyword 负向前瞻替代逻辑有 bug

Python 用 `error(?!\s*=\s*None)` 正则负向前瞻，只排除紧跟 `= None` 的 `error`，不影响其他关键词（`exception`、`failed`、`timeout`）。Go 的实现是：如果整个 content 包含 `error = None`，就**完全否定所有关键词匹配**。

**Python 参考代码** (`from_conv.py:30-31`):
```python
_FAILURE_KEYWORDS = re.compile(
    r"error(?!\s*=\s*None)|exception|failed|timeout|..."
)
# error(?!\s*=\s*None) 只排除 "= None" 紧跟的 error
# "Exception: failed\nerror = None" → 匹配 "Exception" 和 "failed"
```

**Go 问题代码** (`from_conv.go:442-443`):
```go
func matchFailureKeyword(content string) bool {
    return failureKeywords.MatchString(content) && !errorEqualsNonePattern.MatchString(content)
}
// 如果 content 包含 "error = None"，即使同时有 "Exception" 或 "failed" 也返回 false
```

**修复方案**：改为逐个匹配验证，与 `findFailureKeywordIndex` 的逻辑对齐：
```go
func matchFailureKeyword(content string) bool {
    indices := findFailureKeywordIndex(content)
    return len(indices) > 0
}
```
或者将正则改为 Go 的 `regexp` 不支持负向前瞻，需要在匹配回调中过滤 `error=\s*None` 的匹配项。

**流程示例**：
```
输入: "Exception: db connection failed\nerror = None"
Python: _FAILURE_KEYWORDS.search() → 匹配 "Exception" → 返回 True
Go 当前: failureKeywords.MatchString()=true && errorEqualsNonePattern.MatchString()=true → 返回 False ❌
Go 修复后: findFailureKeywordIndex 逐个验证 → "Exception" 非 error 关键词 → 直接返回 True ✅
```

---

### S-04：[严重] TeamSignalDetector.DetectUserIntent 用户消息截取顺序错误

Python 先截取最近 10 条消息再过滤用户消息（最多 10 条中含若干用户消息），Go 先过滤所有用户消息再截取最近 10 条用户消息（最多 10 条纯用户消息）。

**Python 参考代码** (`team.py:359-371`):
```python
recent = messages[-10:]  # 先截最近 10 条
user_messages = [
    str(getattr(m, "content", "")).strip()
    for m in recent
    if str(getattr(m, "role", "")) == "user" and str(getattr(m, "content", "")).strip()
]
```

**Go 问题代码** (`team.go:383-395`):
```go
var userMessages []string
for _, m := range msgs {
    role := getField[string](m, "role", "")
    content := strings.TrimSpace(getField[string](m, "content", ""))
    if role == "user" && content != "" {
        userMessages = append(userMessages, content)
    }
}
if len(userMessages) > 10 {
    userMessages = userMessages[len(userMessages)-10:]
}
```

**修复方案**：改为先截取最近 10 条消息，再从中过滤用户消息：
```go
recentMsgs := msgs
if len(recentMsgs) > 10 {
    recentMsgs = recentMsgs[len(recentMsgs)-10:]
}
var userMessages []string
for _, m := range recentMsgs {
    // ... 过滤用户消息 ...
}
```

---

### S-05：[严重] GetTeamTrajectoryIssues 类型断言可能在 JSON 反序列化后失败

Go 的 `GetTeamTrajectoryIssues` 使用 `issues.([]map[string]string)` 类型断言，但 JSON 反序列化后 `map[string]string` 会变为 `map[string]any`，导致断言失败返回 nil。

**Go 问题代码** (`team.go:344-358`):
```go
func GetTeamTrajectoryIssues(signal *EvolutionSignal) []map[string]string {
    issues, ok := signal.Context["team_trajectory_issues"]
    if !ok { return nil }
    result, ok := issues.([]map[string]string)  // ← JSON 反序列化后为 []map[string]any，断言失败
    if !ok { return nil }
    return result
}
```

**修复方案**：改为 `[]map[string]any` 类型断言，然后逐个转换：
```go
func GetTeamTrajectoryIssues(signal *EvolutionSignal) []map[string]string {
    issues, ok := signal.Context["team_trajectory_issues"]
    if !ok { return nil }
    rawList, ok := issues.([]any)
    if !ok { return nil }
    var result []map[string]string
    for _, item := range rawList {
        if m, ok := item.(map[string]any); ok {
            entry := make(map[string]string, len(m))
            for k, v := range m {
                entry[k] = fmt.Sprintf("%v", v)
            }
            result = append(result, entry)
        }
    }
    return result
}
```

---

### G-04：[一般] detectSkillFromToolCalls 缺少 JSON 解析失败的 debug 日志

**Python 参考代码** (`from_conv.py:469-470`):
```python
except Exception as exc:
    logger.debug("[ConversationSignalDetector] failed to parse skill_tool arguments: %s", exc)
```

**Go 问题代码** (`from_conv.go:690`):
```go
_ = json.Unmarshal([]byte(argsStr), &args)  // 静默忽略错误
```

**修复方案**：补充 debug 日志：
```go
if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
    logger.Debug(logComponent).
        Str("method", "detectSkillFromToolCalls").
        Err(err).
        Msg("[ConversationSignalDetector] 解析 skill_tool 参数失败")
}
```

---

### G-05：[一般] make_team_trajectory_signal 的 excerpt 自行翻译为中文，违反一比一复刻规则

**Python 参考代码** (`team.py:300`):
```python
excerpt = "Detected team skill trajectory issues requiring evolution."
```

**Go 问题代码** (`team.go:331`):
```go
excerpt: "检测到团队技能轨迹问题，需要进行进化。",
```

**修复方案**：将中文改为 Python 原文 `"Detected team skill trajectory issues requiring evolution."`。

---

### G-06：[一般] FromEvaluatedCase 未使用 MakeEvolutionSignal 构造信号

**Python 参考代码** (`from_eval.py`):
```python
return make_evolution_signal(
    signal_type=..., section=..., excerpt=...,
    context={"source": "offline_evaluation", ...},
)
```

**Go 问题代码** (`from_eval.go:25-56`):
```go
return &EvolutionSignal{
    SignalType: ...,
    Section:    ...,
    Excerpt:    ...,
    Context:    map[string]any{"source": "offline_evaluation", ...},
}
```

**修复方案**：改用 `MakeEvolutionSignal()` 构造，确保统一的字段默认值和验证逻辑。

---

### T-03：[提示] NewTeamSignalDetector 构造函数使用 panic 而非 error 返回

**Python 参考代码** (`team.py:340-341`):
```python
if policy is None:
    raise ValueError("TeamSignalDetector requires at least one LLM policy")
```

**Go 问题代码** (`team.go:165-166`):
```go
panic("TeamSignalDetector requires at least one LLM policy")
```

**修复方案**：改为返回 error：`return nil, fmt.Errorf("TeamSignalDetector requires at least one LLM policy")`。

---

### T-04：[提示] SetSessionDeliveryContext 中额外的 channel_metadata 逻辑

Go 的 `SetSessionDeliveryContext` 在第 193-196 行额外添加了从 `routeMetadata["channel_metadata"]` 提取并写入 `meta["channel_metadata"]` 的逻辑，这在 Python 的 `set_session_delivery_context` 中不存在。Python 中 `channel_metadata` 仅在 `update_session_metadata` 中处理。

功能上不算错误（可能是增强），但与 Python 不一致。建议确认是否有意为之。

---

### T-05：[提示] session.list 缺少分页支持

Python 的 `get_all_sessions_metadata(limit, offset)` 支持分页，Go 的 `handleSessionList` 返回全量数据。会话数量多时可能有性能问题。

---

## 三、Evolving / Optimizer 模块（2 严重 / 3 一般 / 2 提示）

### S-06：[严重] CallbackFramework.applyFilters 三级管线中 MODIFY 修改不累积

Python 的 `_apply_filters()` 在每一级过滤器返回 `FilterAction.MODIFY` 时更新 `current_args` / `current_kwargs`，修改会在管线中累积传递。Go 的 `applyFilters` 在三级过滤器管线中遇到 MODIFY 时**直接跳过**，不更新 `data`，导致多级 MODIFY 的累积效果丢失。

**Python 参考代码** (`framework.py:935-942`):
```python
elif result.action == FilterAction.MODIFY:
    if result.modified_args is not None:
        current_args = result.modified_args      # ← 累积修改
    if result.modified_kwargs is not None:
        current_kwargs = result.modified_kwargs  # ← 累积修改
```

**Go 问题代码** (`framework.go:1606-1643`):
```go
func (fw *CallbackFramework) applyFilters(...) FilterResult {
    // 全局过滤器
    for _, f := range fw.globalFilters {
        result := f.Filter(ctx, event, callbackName, data)
        if result.Action == FilterActionStop || result.Action == FilterActionSkip {
            return result
        }
        // ← 缺少: if result.Action == FilterActionModify { data = result.ModifiedData }
    }
    // 事件级过滤器 — 同样缺少 MODIFY 处理
    // 回调级过滤器 — 同样缺少 MODIFY 处理
    return FilterResult{Action: FilterActionContinue}
}
```

**修复方案**：在每级过滤器的 MODIFY 分支中更新 `data`：
```go
if result.Action == FilterActionModify {
    if result.ModifiedData != nil {
        data = result.ModifiedData
    }
    continue
}
```

---

### S-07：[严重] WorkflowEventType 缺少 ComponentStreamOutput 常量

**Python 参考代码** (`events.py:WorkflowEvents`):
```python
class WorkflowEvents:
    COMPONENT_BATCH_INPUT = "_framework:component_batch_input"
    COMPONENT_BATCH_OUTPUT = "_framework:component_batch_output"
    COMPONENT_STREAM_INPUT = "_framework:component_stream_input"
    COMPONENT_STREAM_OUTPUT = "_framework:component_stream_output"  # ← Go 缺少
```

**Go 问题代码** (`events.go:429-434`):
```go
ComponentBatchInput  WorkflowEventType = "_framework:component_batch_input"
ComponentBatchOutput WorkflowEventType = "_framework:component_batch_output"
ComponentStreamInput WorkflowEventType = "_framework:component_stream_input"
// ← 缺少 ComponentStreamOutput
```

**修复方案**：补充常量定义：
```go
// ComponentStreamOutput 组件流式输出
ComponentStreamOutput WorkflowEventType = "_framework:component_stream_output"
```

---

### G-07：[一般] CallbackFramework.On* 方法不接受 filters 参数

Python 的 `register_sync` 接受 `filters` 参数并保存到 `self._callback_filters`。Go 的 `OnLLM` 等方法不接受 filters，`callbackFilters` map 存在但从未被填充。

**修复方案**：为 `On*` 方法增加可选的 `WithFilter(filter)` 选项，或将 filters 作为参数加入方法签名。

---

### G-08：[一般] CallbackFramework.OnChain 不添加回调/handler 到 chain

Python 的 `register_sync` 在提供 `rollback_handler` 或 `error_handler` 时自动创建 `CallbackChain` 并添加。Go 的 `OnChain` 只确保 chain 存在，不添加回调和 handler。

**修复方案**：在 `OnChain` 中添加 callback、rollbackHandler、errorHandler 到 chain。

---

### G-09：[一般] ToolOptimizerBase.Bind 返回 0（⤵️ 标记待回填 Operator 类型转换）

**Go 问题代码** (`base.go:262-265`):
```go
func (b *ToolOptimizerBase) Bind(operators map[string]any, targets []string, config map[string]any) int {
    // ⤵️ 9.70: 等待 Trainer 实现后回填 Operator 类型转换
    return 0
}
```

当前 ToolOptimizer 完全无法绑定 Operator，所有工具优化流程无法执行。

---

### T-06：[提示] CallbackFramework.triggerCallbacks 中 AbortError 和普通错误无日志

Python 在 `trigger()` 中对 `AbortError` 和普通异常都有详细日志，Go 的 `triggerCallbacks` 中无日志。

**修复方案**：在错误处理分支添加日志：
```go
logger.Error(logComponent).
    Str("event", event).
    Str("callback", callbackName).
    Err(err).
    Msg("回调执行失败")
```

---

### T-07：[提示] UpdateValue 可变 vs Python frozen

Python `UpdateValue` 是 `@dataclass(frozen=True)`，Go 是可变 struct。这是语言差异，但建议在代码注释中标注不可变约定。

---

## 四、Adapter / UapClaw 门面（3 严重 / 3 一般 / 2 提示）

### S-08：[严重] UapClaw 缺少 cloud memory hook 和 Team 模式分流

**Python 参考代码** (`uapclaw.py:834-845, 911-949`):
```python
# cloud memory hook
if memory_mode == "cloud":
    await self._trigger_hook(AgentServerHookEvents.MEMORY_BEFORE_CHAT, ...)

# Team 模式分流
if mode == "team":
    # 使用原始 query、后续请求绕过 Session 队列
    ...
```

**Go 问题代码** (`uapclaw.go:149, 183, 219-220`):
```go
// ⤵️ cloud memory hook 未实现
// ⤵️ Team 模式分流未实现
```

**修复方案**：在 `ProcessMessage` 中补充 cloud memory hook 调用；在 `ProcessMessageStream` 中补充 Team 模式分流逻辑（等 team 功能回填）。

---

### S-09：[严重] CodeAdapter.CreateInstance 缺少 coding_memory workspace 设置

**Python 参考代码** (`interface_code.py:314-330`):
```python
self._instance.deep_config.workspace.set_directory(
    DirectoryNode.CODING_MEMORY,
    coding_memory_dir,
)
```

**Go 问题代码**：CodeAdapter.CreateInstance 完全没有设置 coding_memory 目录结构。

**修复方案**：在 CodeAdapter.CreateInstance 中添加 workspace 目录设置逻辑，对齐 Python 的 coding_memory / project_memory 等目录配置。

---

### S-10：[严重] CodeAdapter 缺少 _build_agent_rails() 覆盖

Python CodeAdapter 有自己的 `_build_agent_rails`，注册了 Code 模式专属 Rails（LspRail / ProjectMemoryRail / CodingMemoryRail / CodeAgentRail / WorktreeRail 等）。Go 的 CodeAdapter 直接委托 DeepAdapter 的版本，缺少 Code 专属 Rails。

**修复方案**：为 CodeAdapter 实现 `buildCodeAgentRails()` 方法，注册 Code 模式专属 Rails。等 10.6.3-10 回填。

---

### G-10：[一般] CodeAdapter.CreateInstance 缺少 _refresh_multimodal_configs 调用

Python CodeAdapter 在步骤 4 调用 `self._refresh_multimodal_configs(config_base)`，Go 注释标注为步骤 4 但用 ⤵️ 标记为待实现。DeepAdapter.CreateInstance 有调用 `d.refreshMultimodalConfigs(configBase)`，CodeAdapter 跳过了此步骤。

---

### G-11：[一般] CodeAdapter.CreateInstance 缺少 setattr 设置 jiuwenswarm_adapter_mode 等属性

**Python 参考代码** (`interface_code.py:302-312`):
```python
setattr(self._instance, "_jiuwenswarm_adapter_mode", "code")
setattr(self._instance, "_jiuwenswarm_code_project_dir", ...)
setattr(self._instance, "_jiuwenswarm_project_dir", ...)
```

这些属性被 `configure_team_member_agent` 和 team_helpers 使用。Go 中没有对 instance 设置这些属性。

---

### G-12：[一般] UapClaw.CancelInflightWork 未调用 AbortOnGatewayDisconnect

**Python 参考代码** (`uapclaw.py:1226-1238`):
```python
def cancel_inflight_work(self, ...):
    adapter.abort_on_gateway_disconnect()
```

**Go 问题代码** (`uapclaw.go:509`):
```go
// ⤵️ 未实现
```

**修复方案**：在 `CancelInflightWork` 中调用 adapter 的断连中止逻辑。

---

### T-08：[提示] AgentCard ID 差异

Python: `AgentCard(name=..., id='jiuwenswarm')`，Go: `agentschema.WithAgentID("uapclaw")`。ID 不一致可能影响日志追踪和注册表查找。

---

### T-09：[提示] DeepAdapter 12+ 个 Rail Builder 返回 nil

`buildSkillRail` / `buildSkillEvolutionRail` / `buildMemoryRail` 等 12+ 个 Rail Builder 都用 ⤵️ 标记为待实现，当前返回 nil。已标注在 10.6.3-10 回填，不影响当前非生产使用。

---

## 五、Gateway MessageHandler（2 严重 / 2 一般 / 2 提示）

### S-11：[严重] ApplyChannelState 中 SessionMap 集成缺失

**Python 参考代码** (`message_handler.py:1144-1165`):
```python
if identity_key and channel_type in self._session_map_channel_types:
    session_id = self._session_map.get_session_id(*identity_key)
    msg.session_id = session_id
    state.session_id = session_id
```

**Go 问题代码** (`channel_state.go:84-111`):
```go
// 只有 if state.SessionID != "" { msg.SessionID = state.SessionID }
// 完全没有 SessionMap 逻辑
```

**影响**：飞书企业等使用 SessionMap 的渠道，session_id 不会根据用户身份动态生成，导致所有用户共享同一会话。

**修复方案**：等 11.7 SessionMap 实现后回填。当前标注 `TODO(#11.7)`。

---

### S-12：[严重] CancelAgentSessionsOnDisconnect 缺少 mode 注入

**Python 参考代码** (`message_handler.py:546-553`):
```python
disconnect_params = {
    "mode": state.get("mode"),
    "trusted_dirs": state.get("trusted_dirs"),
}
cancel_msg.params = disconnect_params
```

**Go 问题代码** (`disconnect.go:39-46`):
```go
cancelMsg := schema.NewMessage(...)
// ← 没有 params 字段（没有 mode 和 trusted_dirs）
```

**影响**：断连取消时 AgentServer 无法找到正确的 agent（因为缺少 mode），导致取消失败或取消了错误的 agent。

**修复方案**：从 channelState 获取 mode 并注入到 cancelMsg.Params 中。

---

### G-13：[一般] handleChannelControl 中渠道类型检查顺序错误

Python 先检查 `channel_type not in self._control_channel_types`（非受控渠道直接返回），然后才解析文本。Go 先解析 `ParseChannelControlText(text)`，然后才检查 `controlChannelTypes`。

---

### G-14：[一般] PublishRobotMessages 缺少 Outbound Pipeline 处理

**Python 参考代码** (`message_handler.py:1196-1201`):
```python
def publish_robot_messages(self, msg):
    self._outbound_pipeline.apply(msg)
    # ... 入队 ...
```

**Go 问题代码** (`forward_loop.go:35-44`):
```go
func PublishRobotMessages(msg *schema.Message, ...) {
    // 直接入队，没有 outbound pipeline 处理
}
```

**影响**：数字分身出站路由缺失，出站消息不会被 IM Pipeline 过滤/路由。

---

### T-10：[提示] handleChannelControl 多余的 req_method 检查

Go 的 `handleChannelControl` 额外添加了 `msg.Type != schema.MessageTypeReq` 和 `msg.ReqMethod != schema.ReqMethodChatSend` 的检查，Python 中没有。这可能导致某些场景下控制命令被跳过。

---

### T-11：[提示] sendInterruptResultNotification 缺少 session_id 字段

Python payload 中包含 `session_id` 字段，Go 的 payload dict 中没有。前端如果从 payload 而非顶层读 session_id 会丢失此信息。

---

## 六、Agent Teams 模块（4 严重 / 3 一般 / 2 提示）

### S-13：[严重] ResolveMemberModelFromPool 永远返回 nil

即使参数合法（pool 非空、modelName 非空），函数也永远返回 nil，导致模型池查找完全不可用。

**Python 参考代码** (`allocator.py`):
```python
def resolve_member_model(team_spec, *, model_name, model_index):
    if not team_spec.model_pool or not model_name:
        return None
    group = [e for e in team_spec.model_pool if e.model_name == model_name]
    if not group:
        return None
    idx = model_index if isinstance(model_index, int) and 0 <= model_index < len(group) else 0
    return group[idx].to_team_model_config()
```

**Go 问题代码** (`allocator.go:75-79`):
```go
func ResolveMemberModelFromPool(pool []ModelPoolEntry, modelName string, modelIndex int) *TeamModelConfig {
    if len(pool) == 0 || modelName == "" {
        return nil
    }
    return nil  // BUG: 永远返回 nil，缺少分组+索引查找逻辑
}
```

**修复方案**：实现完整的分组+索引查找逻辑：
```go
var group []ModelPoolEntry
for _, e := range pool {
    if e.ModelName == modelName {
        group = append(group, e)
    }
}
if len(group) == 0 {
    return nil
}
idx := 0
if modelIndex >= 0 && modelIndex < len(group) {
    idx = modelIndex
}
return group[idx].ToTeamModelConfig()
```

---

### S-14：[严重] StreamController.pendingInputs 数据竞争

`DeliverInput` 可以从任意 goroutine 调用（修改 `pendingInputs`），而 `runOneRound` 的 defer 也会读取和清空 `pendingInputs`，没有锁保护。

**Python 参考代码**：Python 使用 asyncio 单线程模型，不存在并发问题。

**Go 问题代码** (`stream_controller.go` + `team_agent.go`):
```go
// team_agent.go: DeliverInput 无锁修改
a.streamController.pendingInputs = append(a.streamController.pendingInputs, content)

// stream_controller.go: runOneRound 中检查+清空
sc.mu.Lock()
hasPending := len(sc.pendingInputs) > 0 && sc.streamQueue != nil
sc.mu.Unlock()  // ← 释放锁后，另一个 goroutine 可能修改 pendingInputs
// ... 然后再加锁读取 ...
```

**修复方案**：在 `DeliverInput` 中加锁保护 `pendingInputs` 的写入：
```go
func (a *TeamAgent) DeliverInput(ctx context.Context, content string) error {
    a.streamController.mu.Lock()
    a.streamController.pendingInputs = append(a.streamController.pendingInputs, content)
    a.streamController.mu.Unlock()
    return nil
}
```

---

### S-15：[严重] TeamAgent.ShutdownSelf 缺少状态更新和流关闭

**Python 参考代码** (`team_agent.py`):
```python
async def shutdown_self(self):
    await self._stream_controller.cooperative_cancel()
    if self._state.team_member is not None:
        await self._state.team_member.update_status(MemberStatus.SHUTDOWN)
    self._close_stream()
```

**Go 问题代码** (`team_agent.go`):
```go
func (a *TeamAgent) ShutdownSelf(ctx context.Context) error {
    if a.streamController != nil {
        _ = a.streamController.CooperativeCancel(ctx)
    }
    // 缺少: team_member.update_status(SHUTDOWN)
    // 缺少: _close_stream()
    return nil
}
```

**修复方案**：补充状态更新和流关闭逻辑。

---

### S-16：[严重] runOneRound 缺少 SHUTDOWN_REQUESTED 检查

当成员收到关闭请求时，Python 会检查 `SHUTDOWN_REQUESTED` 并关闭流。Go 中只有注释标记 `⤵️ 待 TeamMember 状态检查回填`，导致 Leader 流不终止。

**Python 参考代码** (`stream_controller.py`):
```python
team_member = self._state.team_member
if team_member and await team_member.status() == MemberStatus.SHUTDOWN_REQUESTED:
    self.close_stream()
```

**修复方案**：在 `runOneRound` 的 finally 块中添加 `SHUTDOWN_REQUESTED` 检查和 `CloseStream()` 调用。

---

### G-15：[一般] Allocator.Allocate 完全是 stub（⤵️ 9.64）

**Go 问题代码** (`allocator.go:57-79`):
```go
func (a *Allocator) Allocate(...) (any, error) {
    // ⤵️ 回填: 9.64 — allocator 运行时逻辑（RoundRobin/ByModelName/Router 分发）
    return nil, nil
}
```

当前 Agent Teams 的模型分配完全无法工作。

---

### G-16：[一般] TeamAgent.UpdateStatus / updateExecution 缺少实际持久化

**Python 参考代码** (`team_agent.py`):
```python
async def _update_status(self, status):
    if self._state.team_member:
        await self._state.team_member.update_status(status)
```

**Go 问题代码** (`team_agent.go`):
```go
func (a *TeamAgent) UpdateStatus(ctx context.Context, status atschema.MemberStatus) error {
    logger.Debug(logComponent)...Msg("UpdateStatus")
    return nil  // 只记录日志，不调用 team_member.update_status()
}
```

---

### G-17：[一般] LookupHumanAgentRuntime 缺少 is_human_agent 检查

**Python 参考代码** (`team_agent.py`):
```python
def lookup_human_agent_runtime(self, member_name):
    backend = self._configurator.team_backend
    if backend is None or not backend.is_human_agent(member_name):
        return None
    return self._spawn_manager.lookup_inprocess_agent(member_name)
```

**Go 问题代码**：缺少 `backend.is_human_agent(memberName)` 检查，只检查 spawnManager。

---

### T-12：[提示] harness.go 中多个 TODO 标注的 Rail 类型

标注在 9.66/9.68 回填，当前不影响主流程。

---

### T-13：[提示] HumanAgentNotEnabledError 错误消息不完整

Python 中错误消息包含解决方案提示（"create the team with enable_hitt=True..."），Go 中只有简短的 "no human-agent member is registered on this team"。

---

## 七、Evolving / Optimizer 详细审查（3 严重 / 4 一般 / 2 提示）

### S-17：[严重] BaseOptimizerMixin.Bind() 缺少 targets 回退到 DefaultTargets() 的逻辑

当 `targets` 为 nil/空时，Python 回退到 `self.default_targets()`，Go 直接赋值，不回退。

**Python 参考代码** (`base.py:91`):
```python
self._targets = list(targets or self.default_targets())
```

**Go 问题代码** (`base.go:167`):
```go
m.targets = targets  // 直接赋值，不回退到 DefaultTargets()
```

**修复方案**：
```go
if len(targets) == 0 {
    m.targets = m.DefaultTargets()  // 注意：需要子类实现
} else {
    m.targets = targets
}
```
由于 `BaseOptimizerMixin` 没有实现 `BaseOptimizer` 接口，`DefaultTargets()` 需要由子类提供。可通过在 `Bind` 中增加 `defaultTargets []string` 参数或改为由子类在调用 `Bind` 前处理回退。

---

### S-18：[严重] rits.go 缺少 temperature=1 设置

Python 的 `rits_response` 显式设置 `temperature=1`（高随机性），Go 未传递此参数，影响所有 LLM 生成的多样性和质量。

**Python 参考代码** (`rits.py:18`):
```python
@retry(wait=wait_random_exponential(min=1, max=5), stop=stop_after_attempt(2), reraise=True)
def rits_response(model_id, prompt, api_key, *,
                  max_attempts=15, include_stop_sequence=False,
                  stop_sequences=['<|eot_id|>','<|end_of_text|>','<|eom_id|>'],
                  verbose=None):
    config = ModelRequestConfig(model=model_id, temperature=1)
```

**Go 问题代码** (`rits.go`):
```go
// InvokeTextWithRetry 内部调用 Model.Invoke，未传递 temperature 参数
raw, err := llm_resilience.InvokeTextWithRetry(ctx, e.model, modelName, prompt, policy)
```

**修复方案**：在 `InvokeTextWithRetry` 调用前设置 `temperature=1` 的 Option：
```go
raw, err := llm_resilience.InvokeTextWithRetry(ctx, e.model, modelName, prompt, policy,
    llm.WithTemperature(1.0),
)
```

---

### S-19：[严重] api_wrapper_mcp.go 中 MCP 结果提取错误

Go 使用 `fmt.Sprintf("%v", callResult)` 而非精确提取 `callResult.Content[0].Text`，返回的不是工具的实际文本输出，而是 Go 结构体的字符串表示。

**Python 参考代码** (`callable_fortest.py:25`):
```python
result = await client.call_tool(tool_name, arguments)
return result.content[0].text  # 精确提取 MCP 协议文本内容
```

**Go 问题代码** (`api_wrapper_mcp.go:118`):
```go
callResult, err := client.CallTool(ctx, toolName, arguments)
responseStr := fmt.Sprintf("%v", callResult)  // ← 返回结构体的 Go 默认格式化字符串
```

**修复方案**：精确提取 MCP 协议 content 中的文本：
```go
callResult, err := client.CallTool(ctx, toolName, arguments)
if err != nil {
    return mcpErrorJSON(fmt.Sprintf("MCP 调用失败: %v", err)), 12
}
if len(callResult.Content) > 0 {
    if textContent, ok := callResult.Content[0].(mcp.TextContent); ok {
        responseStr = textContent.Text
    }
}
```

---

### G-18：[一般] TextualParameter.Gradients 类型从 Any 缩窄为 string

Python 的 `gradients` 值是 `Any`（可以是 str 或 list），Go 限定为 `string`。如果梯度值需要是列表（如多个候选描述），Go 无法存储。

---

### G-19：[一般] SimpleEval.Eval() 在 runs > 1 时结果结构不一致

Python 当 `runs > 1` 时返回嵌套结构 `[[result1], [result2]]`，Go 返回展平结构 `[result1, result2]`。当前 `runs=1` 不会触发，但未来扩展时会导致问题。

---

### G-20：[一般] rits.go 缺少 stop_sequences 支持

Python 中所有 `get_rits_response` 调用都传递了 `stop_sequences=['<|eot_id|>','<|end_of_text|>','<|eom_id|>']`，Go 的 `LLMInvokePolicy` 没有此参数，可能导致 LLM 输出过长或格式不符预期。

---

### G-21：[一般] eval.go 中缺少 api_wrapper 时不中断评估

**Python 参考代码** (`customized_eval.py`):
```python
if self.api_wrapper is None:
    raise ValueError(error_msg)  # 中断评估
```

**Go 问题代码** (`eval.go:373-382`):
```go
// 只添加 error 到 errors 列表，不返回 error 或 panic
// 后续 evaluateOutputEffectiveness 会因 executionError==nil 继续评估
```

**修复方案**：在 apiWrapper 为 nil 时，应跳过 output effectiveness 评估并直接返回 0.0。

---

### T-14：[提示] CritiqueInstruction score 存为 float64 而非 int

Python 中 score 是 `int`（`1`、`2`、`3`），Go 中存储为 `float64`（`1.0`、`2.0`、`3.0`）。JSON 序列化时产生 `3.0` 而非 `3`，可能影响下游消费方。

---

## 八、Session History 详细审查（1 严重 / 2 一般 / 1 提示）

### S-20：[严重] TruncateHistoryRecords 签名和逻辑与 Python 完全不同

**Python 参考代码** (`session_history.py`):
```python
def truncate_history_records(session_id, cut_index):
    # cut_index: 整数索引，截断位置
    # 等待异步写入完成: _WRITE_QUEUE.join()
    # 返回: {"remaining_records": n, "removed_records": n}
```

**Go 问题代码** (`session_history.go:172-199`):
```go
func TruncateHistoryRecords(sessionID, requestID string) error {
    // requestID: 字符串，通过请求ID查找截断位置
    // 不等待异步队列刷盘
    // 返回: error
}
```

**差异**：
1. 参数不同：Python 用 `cut_index`（int），Go 用 `requestID`（string）
2. 返回值不同：Python 返回截断统计 dict，Go 返回 error
3. Go 缺少等待异步队列刷盘的步骤

**修复方案**：改为使用 `cutIndex int` 参数，返回 `(remaining int, removed int, err error)`，在截断前等待异步队列刷盘。

---

### G-22：[一般] session_history 缺少 _is_team_relevant 过滤功能

Python 定义了 `_TEAM_RELEVANT_EVENT_TYPES` 集合和 `_is_team_relevant` 过滤函数，Go 中完全没有。Team 模式历史记录过滤功能缺失。

---

### T-16：[提示] session_history 缺少 _serialize_value 函数

Python 有 `_serialize_value(obj)` 递归序列化 datetime/date 等非 JSON 原生类型。Go 中无此处理，功能上影响较小。

---

## 九、⤵️ 标记验证汇总

| 位置 | 标记内容 | 是否确实未实现 | 回填章节 |
|------|---------|--------------|---------|
| `handle_session.go:103` | UserContent 自动标题 | ✅ 未实现 | 11.x |
| `handle_session.go:803` | autoTitle 逻辑 | ✅ 未实现 | 11.x |
| `handle_session.go:862` | else 分支 autoTitle | ✅ 未实现 | 11.x |
| `deep_adapter_rails.go:65` | 步骤 7-19 Rail Builder | ✅ 未实现 | 10.6.3-10 |
| `deep_adapter_rails.go:208-330` | 12+ 个 Rail Builder | ✅ 未实现 | 10.6.3-10 |
| `tool_call/base.go:263` | Operator 类型转换 | ✅ 未实现 | 9.70 |
| `allocator.go:57-79` | Allocate 运行时逻辑 | ✅ 未实现 | 9.64 |
| `allocator.go:75-79` | ResolveMemberModelFromPool | ✅ 未实现 | 9.64 |
| `team_model_config.go:32` | NewTeamModelConfig | ✅ 未实现 | 9.57 |
| `harness.go:19-34` | 6 个 Rail 类型 | ✅ 未实现 | 9.66/9.68 |
| `uapclaw.go:149,183,219` | cloud memory / Team 分流 | ✅ 未实现 | 后续回填 |
| `uapclaw.go:509` | AbortOnGatewayDisconnect | ✅ 未实现 | 后续回填 |
| `rits.go` | temperature=1 / stop_sequences | ✅ 未实现 | 功能遗漏 |
| `api_wrapper_mcp.go:118` | MCP 结果精确提取 | ✅ 未实现 | 功能遗漏 |

> 所有 ⤵️ 标记均经验证为确实未实现，标注正确。

---

## 十、修复优先级建议

### P0 — 必须立即修复（影响功能正确性，不依赖外部回填）

| 编号 | 问题 | 影响 |
|------|------|------|
| S-03 | matchFailureKeyword 负向前瞻逻辑 bug | 信号检测漏判，error=None 会压制其他关键词 |
| S-06 | applyFilters MODIFY 不累积 | 多级过滤器修改丢失，回调框架核心逻辑 bug |
| S-07 | ComponentStreamOutput 缺失 | 工作流流式输出事件丢失 |
| S-01 | AppendHistoryRecord 不更新 metadata | 会话消息计数/标题/时间戳不更新 |
| S-02 | handleSessionRename 缺少 user_id | 初始化 metadata 字段遗漏 |
| S-12 | disconnect 缺少 mode 注入 | 断连取消可能找错 agent |
| S-13 | ResolveMemberModelFromPool 永远返回 nil | 模型池查找完全不可用 |
| S-14 | StreamController.pendingInputs 数据竞争 | 并发场景下可能丢数据或崩溃 |
| S-18 | rits.go 缺少 temperature=1 | 影响 LLM 生成的多样性和质量 |
| S-19 | MCP 结果提取错误 | 返回结构体字符串而非实际文本 |

### P1 — 近期修复（影响部分场景正确性）

| 编号 | 问题 | 影响 |
|------|------|------|
| S-04 | TeamSignalDetector 用户消息截取顺序 | 可能取到过多用户消息传给 LLM |
| S-05 | GetTeamTrajectoryIssues 类型断言 | JSON 往返后返回 nil |
| S-15 | ShutdownSelf 缺少状态更新和流关闭 | 成员不会真正关闭 |
| S-16 | runOneRound 缺少 SHUTDOWN_REQUESTED 检查 | 流不会关闭 |
| S-17 | Bind() 缺少 targets 回退到 DefaultTargets() | 子优化器无法使用默认 targets |
| S-20 | TruncateHistoryRecords 签名与 Python 完全不同 | rewind 功能无法正确工作 |
| G-01 | SetSessionDeliveryContext 同步写入 | 性能和并发安全问题 |
| G-20 | rits.go 缺少 stop_sequences | LLM 输出可能过长 |
| G-21 | eval.go 缺少 api_wrapper 时不中断 | 评估结果可能不准确 |

### P2 — 后续修复（功能缺失但已有回填计划或影响范围有限）

| 编号 | 问题 | 影响 |
|------|------|------|
| S-08 | UapClaw 缺少 cloud memory / Team 分流 | 云记忆和 Team 模式不可用 |
| S-09/S-10 | CodeAdapter 缺少 workspace/Rails 设置 | Code 模式功能不完整 |
| S-11 | ApplyChannelState 缺 SessionMap | 飞书等多用户渠道会话串用 |
| G-07/G-08 | CallbackFramework filters/chain 不完整 | 回调过滤功能受限 |
| G-16/G-17 | TeamAgent 状态持久化/Lookup 缺失 | Agent Teams 运行时行为不一致 |
