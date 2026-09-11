# 48 小时逻辑审查 — 2026-09-11

> 审查范围：48 小时内提交（`0b6d02e7..bd375c95`）涉及的 Go 代码，与 Python 参考实现逐行对比。
> 重点：ParseStreamChunk 对齐、DeepAdapter hasStreamedContent、9.7 审查修复验证、agent_teams 空壳方法。

---

## 审查提交列表

| 提交 | 描述 |
|------|------|
| `bd375c95` | fix(ci): 修复 Test undefined context 和 gofmt 格式问题 |
| `e7f153f4` | fix: 2026-09-07 审查 35 项修复（4 模块） |
| `8188d276` | fix(lint): 将 reflect.Ptr 替换为 reflect.Pointer |
| `aac9d8db` | fix(ci): 修复 CI 流水线 Lint 超时和测试断言错误 |
| `c518f31e` | style: 补充遗漏的格式修正 |
| `45b446c5` | docs: 新增审查文档、设计规格、实现计划和 migration doc.go |
| `2405f1e9` | refactor: 声明顺序重排 + 注释修正 + 依赖升级 |
| `db225890` | style: 全局声明顺序与格式修正 |
| `da5559e9` | feat(adapter): DeepAdapter 传入 hasStreamedContent 参数对齐 Python |
| `babd228d` | feat(server/utils): ParseStreamChunk 一比一对齐 Python |
| `3b57e6d1` | refactor(server/utils): 删除 4 个无调用方的 stub 函数 |
| `ded847d7` | fix(shell): 删除 bash.go 重复调用 DetectAndRecordDeletions |
| `c47ba1a4` | docs: 10.3.25 审查修复实现计划 |
| `0b6d02e7` | docs: 10.3.25 审查修复设计 |

---

## 一、严重问题

### S-01：DeepAdapter hasStreamedContent 始终为 false

**文件**：`internal/swarm/server/adapter/deep_adapter.go` L973-981

**Python 行为**（`interface_deep.py` L4636-4814）：
```python
has_streamed_content = False
...
if chunk_type == "llm_output":
    ...
    has_streamed_content = True     # ← 设置为 True
    ...
if chunk_type == "answer":
    if has_streamed_content:
        parsed = self._parse_stream_chunk(chunk, _has_streamed_content=True)
    else:
        parsed = self._parse_stream_chunk(chunk)
```

**Go 问题代码**：
```go
// L965: llm_output 分支
case "llm_output":
    accumulatedText += textContent    // ← 只追加文本，未设置 hasStreamedContent 变量
...

// L973-981: default 分支
default:
    if accumulatedText != "" {
        accumulatedText = ""          // ← 先清空
    }
    // Python: _has_streamed_content 在流式场景下为 true
    parsed := utils.ParseStreamChunk(output, usage, emittedAskUserIDs, d.interactionConverter, accumulatedText != "")
    //                                       accumulatedText 已清空 ^^^^^^^^^^^^^^^^ 始终 false
```

**Bug 机制**：`accumulatedText` 在 L974 被重置为 `""`，随后 L981 的 `accumulatedText != ""` **始终为 false**，导致 `answer` 类型 chunk 永远不会返回 `chat.final` 事件，而是返回 `chat.delta`。

**影响**：前端在流式输出场景下无法区分"中间增量内容"和"最终完整回答"，导致 UI 状态异常。

**修复方案**：
```go
hasStreamedContent := false  // 新增变量

for raw := range rawCh {
    switch chunkType {
    case "llm_output":
        accumulatedText += textContent
        hasStreamedContent = true  // ← 设置标记
        ...
    default:
        if accumulatedText != "" {
            accumulatedText = ""
        }
        parsed := utils.ParseStreamChunk(output, usage, emittedAskUserIDs,
            d.interactionConverter, hasStreamedContent)  // ← 传变量
    }
}
```

---

### S-02：`context.compression_state` event_type 前缀错误 + payload 结构错误

**文件**：`internal/swarm/server/utils/stream_utils.go` L250-255

**Python 行为**（`stream_utils.py` L113-131）：
```python
if chunk_type == "context.compression_state":
    ...
    return {
        "event_type": "context.compression_state",    # ← 无 chat. 前缀
        "status": payload_dict.get("status", ""),
        "phase": payload_dict.get("phase", ""),
        "processor": payload_dict.get("processor", ""),
        "summary": payload_dict.get("summary", ""),
        "operation_id": payload_dict.get("operation_id", ""),
    }
```

**Go 问题代码**：
```go
case "context.compression_state":
    return map[string]any{
        "event_type":        "chat.context_compression_state",  // ← 错误：多了 chat. 前缀
        "compression_state": payload,                           // ← 错误：嵌套而非展开
    }
```

**双重错误**：
1. **event_type**：Go = `"chat.context_compression_state"`，Python = `"context.compression_state"`。前端监听 `context.compression_state`，Go 发出的事件无法被消费。
2. **payload 结构**：Python 将 `status/phase/processor/summary/operation_id` 字段**展开到顶层**；Go 将整个 payload 嵌套在 `compression_state` 键下。前端无法在顶层找到这些字段。

**对比**：同文件中 `context.usage`（L243）正确使用了 `"context.usage"` 无前缀，说明 `context.compression_state` 是遗漏。

**修复方案**：
```go
case "context.compression_state":
    status, _ := payload["status"].(string)
    phase, _ := payload["phase"].(string)
    processor, _ := payload["processor"].(string)
    summary, _ := payload["summary"].(string)
    operationID, _ := payload["operation_id"].(string)
    return map[string]any{
        "event_type":   "context.compression_state",
        "status":       status,
        "phase":        phase,
        "processor":    processor,
        "summary":      summary,
        "operation_id": operationID,
    }
```

---

### S-03：`__interaction__` 类型缺少 `activate_confirm` 分支

**文件**：`internal/swarm/server/utils/stream_utils.go` L271-272, L317-325

**Python 行为**（`stream_utils.py` L356-375）：
```python
def _parse_interaction_payload(payload):
    if isinstance(payload, dict) and payload.get("interaction_type") == "activate_confirm":
        return {
            "event_type": "harness.activate_interaction",
            "interaction_type": "activate_confirm",
            "interaction_id": payload.get("interaction_id", ""),
            "extension_name": payload.get("extension_name", ""),
            "runtime_path": payload.get("runtime_path", ""),
            "session_runtime_path": payload.get("session_runtime_path", ""),
            "extension_runtime_path": payload.get("extension_runtime_path", ...),
            "options": payload.get("options", ["accept", "reject"]),
        }
    return convert_interactions_to_ask_user_question([payload])
```

**Go 问题代码**：
```go
case "__interaction__":
    return ParseInteractionPayload(payload, converter)

func ParseInteractionPayload(payload map[string]any, converter InteractionConverterFunc) map[string]any {
    if converter != nil {
        return converter(payload)   // ← converter 是 ConvertInteractionsToAskUserQuestion，不处理 activate_confirm
    }
    return map[string]any{
        "event_type":  "chat.interaction",   // ← fallback 不产生 harness.activate_interaction
        "interaction": payload,
    }
}
```

**影响**：当 `interaction_type == "activate_confirm"` 时，Go 不会发出 `harness.activate_interaction` 事件，导致前端无法显示扩展确认交互界面（如审批弹窗）。

**修复方案**：
```go
func ParseInteractionPayload(payload map[string]any, converter InteractionConverterFunc) map[string]any {
    // 先检查 activate_confirm
    if payload["interaction_type"] == "activate_confirm" {
        extRuntimePath, _ := payload["extension_runtime_path"].(string)
        if extRuntimePath == "" {
            extRuntimePath, _ = payload["runtime_path"].(string)
        }
        options := []string{"accept", "reject"}
        if opts, ok := payload["options"].([]any); ok && len(opts) > 0 {
            options = make([]string, 0, len(opts))
            for _, o := range opts {
                if s, ok := o.(string); ok {
                    options = append(options, s)
                }
            }
        }
        return map[string]any{
            "event_type":              "harness.activate_interaction",
            "interaction_type":         "activate_confirm",
            "interaction_id":           ExtractStringFromPayload(payload, "interaction_id"),
            "extension_name":           ExtractStringFromPayload(payload, "extension_name"),
            "runtime_path":             ExtractStringFromPayload(payload, "runtime_path"),
            "session_runtime_path":     ExtractStringFromPayload(payload, "session_runtime_path"),
            "extension_runtime_path":   extRuntimePath,
            "options":                  options,
        }
    }
    if converter != nil {
        return converter(payload)
    }
    return map[string]any{
        "event_type":  "chat.interaction",
        "interaction": payload,
    }
}
```

---

### S-04：`message` 类型 event_type 不匹配 + 缺少 `stage` 字段

**文件**：`internal/swarm/server/utils/stream_utils.go` L274-279

**Python 行为**（`interface_deep.py` L5182-5198）：
```python
if chunk_type == "message":
    content = payload.get("content", "") if isinstance(payload, dict) else str(payload)
    stage_from_payload = payload.get("stage", "") if isinstance(payload, dict) else ""
    result = {
        "event_type": "harness.message",    # ← 非 chat.message
        "content": content,
        "stage": stage_from_payload or _stage,
    }
```

**Go 问题代码**：
```go
case "message", "stage_result", "extension_ready", "harness_session_finished", "activate_testing_guide":
    return map[string]any{
        "event_type": "chat." + chunkType,  // ← 产出 "chat.message"
        "content":    payload,              // ← 缺少 stage 字段
    }
```

**双重差异**：
1. **event_type**：Python = `"harness.message"`，Go = `"chat.message"`。前端对 auto-harness 消息的监听事件类型不同。
2. **stage 字段**：Go 完全缺失 `stage` 字段，Python 从 payload 或 `_stage` 参数提取。

**修复方案**：
```go
case "message":
    stage, _ := payload["stage"].(string)
    content := fmt.Sprintf("%v", payload["content"])
    if content == "" {
        content = fmt.Sprintf("%v", payload)
    }
    return map[string]any{
        "event_type": "harness.message",
        "content":    content,
        "stage":      stage,
    }
case "stage_result", "extension_ready", "harness_session_finished", "activate_testing_guide":
    return map[string]any{
        "event_type": "chat." + chunkType,
        "content":    payload,
    }
```

---

### S-05：`ask_user_question` payload 嵌套而非展开

**文件**：`internal/swarm/server/utils/stream_utils.go` L257-269

**Python 行为**（`stream_utils.py` L316-320）：
```python
if chunk_type == "chat.ask_user_question":
    return {
        "event_type": "chat.ask_user_question",
        **(payload if isinstance(payload, dict) else {}),  # ← 字段展开到顶层
    }
```

**Go 问题代码**：
```go
case "ask_user_question":
    ...
    return map[string]any{
        "event_type":        "chat.ask_user_question",
        "ask_user_question": payload,  // ← 嵌套在 ask_user_question 键下
    }
```

**影响**：前端期望 `request_id`、`questions` 等字段在顶层，Go 将它们包在 `ask_user_question` 子对象中，前端无法正确渲染用户提问弹窗。

**修复方案**：
```go
case "ask_user_question":
    requestID, _ := payload["request_id"].(string)
    if requestID != "" && emittedAskUserIDs[requestID] {
        return nil
    }
    if requestID != "" {
        emittedAskUserIDs[requestID] = true
    }
    result := map[string]any{
        "event_type": "chat.ask_user_question",
    }
    for k, v := range payload {
        result[k] = SerializeValue(v)
    }
    return result
```

---

### S-06：SkillUseRail.Uninit 未从 ResourceMgr 注销工具

**文件**：`internal/agentcore/harness/rails/skills/skill_use_rail.go` L324-343

**Python 行为**（`skill_use_rail.py` uninit 方法）：
```python
def uninit(self, agent):
    # 1. 从 AbilityManager 注销
    for name in self._owned_tool_names:
        agent.ability_manager.remove(name)
    # 2. 从 ResourceMgr 注销
    for tool_id in self._owned_tool_ids:
        self._resource_mgr.remove_tool([tool_id])
    self._owned_tool_names.clear()
    self._owned_tool_ids.clear()
```

**Go 问题代码**：
```go
func (r *SkillUseRail) Uninit(agent agentinterfaces.BaseAgent) error {
    am := agent.AbilityManager()
    if am != nil {
        for toolName := range r.ownedToolNames {
            removed := am.Remove(toolName)
            // ...
        }
    }
    // ← 缺少 resourceMgr.RemoveTool 循环
    r.ownedToolNames = make(map[string]struct{})
    r.ownedToolIDs = make(map[string]struct{})  // ← ownedToolIDs 被清空但从未用于注销
    return nil
}
```

**影响**：重复 Init/Uninit 循环后，ResourceMgr 中会残留已注销的工具条目，导致工具名称冲突或陈旧引用。

**修复方案**：
```go
func (r *SkillUseRail) Uninit(agent agentinterfaces.BaseAgent) error {
    am := agent.AbilityManager()
    if am != nil {
        for toolName := range r.ownedToolNames {
            _ = am.Remove(toolName)
        }
    }
    // 从 ResourceMgr 注销
    if r.resourceMgr != nil {
        var toolIDs []string
        for id := range r.ownedToolIDs {
            toolIDs = append(toolIDs, id)
        }
        if len(toolIDs) > 0 {
            _, _ = r.resourceMgr.RemoveTool(toolIDs)
        }
    }
    r.ownedToolNames = make(map[string]struct{})
    r.ownedToolIDs = make(map[string]struct{})
    return nil
}
```

---

### S-07：web_handlers.go 权限 RPC stub 遮蔽真实实现

**文件**：`internal/swarm/gateway/channel_manager/web/web_handlers.go` L297-304

**Python 行为**：Python 通过 `app_web_handlers.py` 注册真实 RPC handler，无 stub 遮蔽问题。

**Go 问题代码**：
```go
permStub := stubHandler("permissions", map[string]any{})
d.Register("permissions.owner_scopes.get", permStub)  // ← 返回空 map
d.Register("permissions.owner_scopes.set", permStub)  // ← 返回空 map
d.Register("permissions.rules.set", permStub)          // ← Python 无此 RPC 方法
d.Register("permissions.approval_overrides.get", permStub) // ← 可能遮蔽真实实现
```

而 `permissions_config_rpc.go` 中有 `owner_scopes.get`/`owner_scopes.set` 的**真实实现**。

**影响**：
1. `permissions.owner_scopes.get`/`set` 的 stub 返回空 map，可能优先于真实 RPC 实现。如果 web 通道请求分发先经过 web_handlers 注册表，调用方将收到空响应而非真实数据。
2. `permissions.rules.set` 在 Python 中不存在此 RPC 方法，Go 注册了 stub 可能导致前端误以为此方法可用。

**修复方案**：删除 `permStub` 注册中已在 `permissions_config_rpc.go` 实现的方法，仅保留真正未实现的 stub。

---

### S-08：PersistToOwnerScope 写盘后未刷新配置缓存

**文件**：`internal/swarm/agents/harness/common/rails/permissions/owner_scopes.go` L300-361

**Python 行为**（`owner_scopes.py` L234-259）：
```python
def persist_to_owner_scope(...):
    ...
    # 写盘
    with open(config_path, "w") as f:
        yaml.dump(perm_cfg, f)
    # Python 的 config 热重载（fsnotify）会自动检测文件变更
    # 但对于内存中的 permCfg 引用，写盘后调用方直接使用更新后的 permCfg dict
```

**Go 问题**：`PersistToOwnerScope` 写盘成功后，内存中的 permissions config 缓存未刷新。后续读取权限配置时，如果从缓存读取，会返回过时数据（不含新持久化的 owner_scope 规则）。

**影响**：用户通过前端授权工具后，权限规则持久化到磁盘，但当前会话内的权限检查仍使用旧缓存，导致新授权的工具无法立即使用。

**修复方案**：在 `PersistToOwnerScope` 写盘成功后，调用配置缓存刷新方法（需新增或复用现有热重载机制）。

---

## 二、一般问题

### M-01：`tool_result` 缺少 `result` 字段的 fallback

**文件**：`internal/swarm/server/utils/stream_utils.go` L172-209

**Python 行为**（`stream_utils.py` L252-282）：
```python
result_payload = {
    "result": (
        result_info.get("result", str(result_info))  # ← dict 无 result 键时用 str(result_info) fallback
        if isinstance(result_info, dict)
        else str(result_info)
    ),
}
```

**Go 问题代码**：
```go
result := map[string]any{
    "result": ExtractStringFromPayload(resultInfo, "result"),  // ← 缺失时返回 ""
}
```

**差异**：Python 在 `result_info` 字典中没有 `"result"` 键时，用 `str(result_info)` 序列化整个字典作为 fallback；Go 返回空字符串 `""`。

**影响**：前端在某些异常场景下无法获取到工具结果的任何信息。

**修复方案**：
```go
resultVal := ExtractStringFromPayload(resultInfo, "result")
if resultVal == "" {
    resultVal = fmt.Sprintf("%v", resultInfo)
}
result := map[string]any{"result": resultVal}
```

---

### M-02：`answer` 类型错误默认消息不一致

**文件**：`internal/swarm/server/utils/stream_utils.go` L122-126

**Python**：`"error": payload.get("output", "未知错误")` — 默认 `"未知错误"`
**Go**：`"error": ExtractStringFromPayload(payload, "output")` — 默认 `""` (空字符串)

**修复方案**：
```go
errorMsg := ExtractStringFromPayload(payload, "output")
if errorMsg == "" {
    errorMsg = "未知错误"
}
```

---

### M-03：`SerializeValue` 缺少 Enum 处理

**文件**：`internal/swarm/server/utils/stream_utils.go` L388-395

**Python**（`stream_utils.py` L472-483）：
```python
def _serialize_value(value):
    if isinstance(value, Enum):
        return value.value    # ← 枚举类型取值
    ...
```

**Go**：仅处理 `time.Time`，无 Enum 处理。

**影响**：Go 的 `iota` 枚举值（int/string）在序列化时会以原始类型传递，对于 `string` 枚举可能正确，对于 `int` 枚举可能导致前端收到数字而非字符串。实际影响取决于 payload 中是否包含枚举值。

---

### M-04：`FindInteractionPayloads` 缺少对象属性遍历

**文件**：`internal/swarm/server/utils/stream_utils.go` L417-458

**Python**（`stream_utils.py` L378-432）支持：
- `hasattr(obj, "type")` 对象属性访问
- `obj.model_dump()` Pydantic 序列化后递归
- `for attr_name in ("payload", "data", "value", "result"): getattr(obj, attr_name)` 属性遍历

**Go** 仅处理 `map[string]any` 和 `[]any`。

**影响**：由于 Go 中 `OutputSchema.Payload` 在 JSON 反序列化后已经是 `map[string]any`，此差异在当前架构下不会造成功能问题，但如果未来 payload 包含未序列化的结构体对象，则可能遗漏交互载荷。

---

### M-05：`controller_output` 中交互搜索行为不一致

**文件**：`internal/swarm/server/utils/stream_utils.go` L75-82

**Python `stream_utils.py`**（L150-153）在 `controller_output` 中**会**搜索 `__interaction__`。
**Python `interface_deep.py`** 在 `_parse_stream_chunk` 中**不会**搜索 `__interaction__`。

**Go** 跟随 `stream_utils.py` 的行为（搜索交互），而非 `interface_deep.py` 的行为。

**影响**：Go 可能在 `controller_output` 中发现 Python DeepAdapter 不会发现的交互事件，导致前端收到多余的用户提问弹窗。

---

### M-06：getSkillByName 线性搜索而非 map 查找

**文件**：`internal/agentcore/harness/tools/skills/skill_tool.go` L171-185

**Python**（`skill_manager.py`）：
```python
def _get_skill_by_name(skill_name):
    name_to_skill = {s.name: s for s in self.get_skills()}  # ← 构建 map
    return name_to_skill.get(skill_name)
```

**Go**：
```go
func (t *SkillTool) getSkillByName(name string) *skillpkg.Skill {
    for _, s := range allSkills {   // ← O(n) 线性搜索
        if s.Name == name { return s }
    }
    return nil
}
```

**影响**：性能差异在技能列表较小时可忽略，但注释声称"构建 name→skill 映射"，实际实现与注释不符。

---

### M-07：TeamAgent 核心方法为空壳（9.62/9.61 未实现）

**涉及文件**：
- `internal/agent_teams/agent/team_agent.go` — Invoke/Stream/Interact/Broadcast/HumanAgentSay/AutoStartMember/FromSpawnPayload/RecoverFromSession/DestroyTeam 等
- `internal/agent_teams/agent/session_manager.go` — BindSession/ResumeForNewSession/RecoverForExistingSession 缺少 DB 表创建等步骤
- `internal/agent_teams/agent/spawn_manager.go` — BuildContextFromDB/PublishRestartEvent 为空壳

**Python 参考**：`openjiuwen/agent_teams/agent/team_agent.py` 完整实现。

**状态**：这些方法均已标注 `TODO(#9.62)`/`TODO(#9.61)` 等占位标记，属于实现计划中明确的"待回填"项。此处列出是为了标记确认。

**影响**：TeamAgent 的所有协调、恢复和持久化功能不可用。当前仅代码结构就位，运行时调用这些方法将返回 nil/零值。

---

### M-08：AgentConfigurator.SetupAgent 缺少 6 个 Rail 构造

**文件**：`internal/agent_teams/agent/agent_configurator.go`

**Python**（`agent_configurator.py` L354-425）构造了：
1. TeamToolRail
2. TeamPolicyRail
3. FirstIterationGate
4. TeamWorkspaceRail（已实现但未集成）
5. TeamToolApprovalRail
6. TeamPlanModeRail

**Go**：全部传 nil 或跳过（`TODO(#9.68)`）。

**影响**：TeamAgent 缺少团队级工具审批、策略、计划模式等护栏，功能不完整。属于实现计划中的回填项。

---

## 三、提示

### T-01：`answer` 分支缺少 Python 中 `answer` 作为流循环特殊分支

**Python**（`interface_deep.py` L4793-4820）在流循环中对 `answer` 类型有特殊处理：
- 先 flush `accumulated_text` 和 `accumulated_reasoning`
- 条件传入 `_has_streamed_content=True`

**Go** 将 `answer` 放入 `default` 分支，flush 逻辑在 `default` 入口处统一处理。功能上等价（因为 flush 逻辑相同），但代码组织方式不同。当前代码可工作，但阅读时需注意差异。

---

### T-02：bash.go Stream 路径缺少 DetectAndRecordDeletions 调用

**文件**：`internal/agentcore/harness/tools/shell/bash.go`

**Python**（`_tool.py`）在 Invoke（L232）和 Stream（L323）两个路径都调用了 `_detect_and_record_deletions`。

**Go** 仅在 Invoke 路径调用。Stream 路径尚未实现。

**影响**：当前无影响（Stream 未实现），但未来添加 Stream 路径时需补上此调用。

---

### T-03：web_handlers.go 中 `permissions.rules.set` 是多余 stub

**文件**：`internal/swarm/gateway/channel_manager/web/web_handlers.go` L303

Python 的 `app_web_handlers.py` 中没有 `permissions.rules.set` RPC 方法。Go 注册了此 stub，前端可能误认为此方法可用。

---

### T-04：owner_scopes PermissionContext 类型在两处重复定义

**文件**：
- `internal/swarm/agents/harness/common/rails/permissions/owner_scopes.go`
- `internal/swarm/schema/`

两处都定义了 `OwnerScopesPermissionContext`，应统一到 schema 包。

---

## 四、9.7 审查修复验证总结

| 审查项 | 状态 | 说明 |
|--------|------|------|
| **团队工作区 M-01~M-04** | ✅ 已修复 | Init/maybePull/publishEvent/Windows junction 均已实现 |
| **团队工作区 S-01~S-03, M-05~M-06** | ☐ P2 延迟 | 分布式字段/锁请求/初始化条件 — 审查文档标记 Phase 3 |
| **权限 S-04~S-06, M-07~M-14, T-01/T-03** | ✅ 已修复 | 组件/返回值/normalizeToolArgs/recover/锁/Clone/Persist/Dispatch |
| **权限 S-07** | ⚠️ 部分修复 | `owner_scopes.get/set` 真实实现已加入，但 web_handlers stub 遮蔽未清（见 S-07） |
| **权限 M-10** | ☐ 未修复 | PersistToOwnerScope 写盘后缓存未刷新（见 S-08） |
| **权限 T-02** | ☐ 未修复 | OwnerScopesPermissionContext 重复定义（见 T-04） |
| **EvolutionRail S-08~S-11, M-16~M-20, T-04~T-06** | ✅ 已修复 | 超时/saveTrajectory/timed_out/defaultMemberRole/Drain/LLMCallDetail/toolCallID/SetTrajectorySink |
| **SkillUseRail S-12, M-21~M-26, T-07** | ✅ 已修复 | loadYAML/ExpandHome/BeforeInvoke/BuildSkillLine/BuildSkillsSection/fetchEvolutionTexts |
| **SkillUseRail S-13** | ☐ 未修复 | Uninit 未从 ResourceMgr 注销（见 S-06） |
| **SkillUseRail S-14** | ☐ 未修复 | getSkillByName 线性搜索（见 M-06） |
| **SkillUseRail S-15** | ☐ 未修复 | 缺少 BuildAllModePrompt 纯文本方法 |
| **SkillUseRail M-22** | ☐ 未修复 | loadYAML 返回 nil yamlData 而非空 map |

---

## 五、问题统计

| 严重度 | 数量 | 编号 |
|--------|------|------|
| **严重** | 8 | S-01 ~ S-08 |
| **一般** | 8 | M-01 ~ M-08 |
| **提示** | 4 | T-01 ~ T-04 |
| **合计** | 20 | |

---

## 六、优先修复建议

**第一优先级（功能断裂，前端无法工作）**：
1. **S-01** hasStreamedContent 始终 false — `answer` 事件类型逻辑完全失效
2. **S-02** context.compression_state event_type 错误 — 前端无法接收压缩状态事件
3. **S-03** activate_confirm 分支缺失 — 扩展审批弹窗无法触发
4. **S-05** ask_user_question payload 嵌套 — 前端无法渲染用户提问

**第二优先级（功能异常，需尽快修复）**：
5. **S-04** message 类型 event_type 不匹配 — auto-harness 消息丢失
6. **S-06** SkillUseRail Uninit 资源泄露
7. **S-07** web_handlers stub 遮蔽真实 RPC
8. **S-08** PersistToOwnerScope 缓存过期
