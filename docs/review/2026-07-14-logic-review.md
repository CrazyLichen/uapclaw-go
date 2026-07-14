# 逻辑审查报告 — 2026-07-14

> 审查范围：48小时内提交的代码，对照 Python 参考项目检查 Go 移植差异
> 涉及章节：9.32-35 SysOperation、9.55-56 TeamAgent/Blueprint、10.3.4-7 模式适配器、10.3.7 SysOpBuilder/RecapPrompts/handleCommand、11.3 MessageHandler、11.5 WebSocketAgentServerClient、11.14 Web Channel

## 统计概览

| 级别 | 数量 | 说明 |
|------|------|------|
| 🔴 严重 | 27 | 功能逻辑缺失或行为不一致，影响正确性 |
| 🟡 一般 | 30 | 逻辑偏差或遗漏，可能影响特定场景 |
| 🔵 提示 | 16 | 日志/文本/类型差异，不影响主流程 |

---

## 一、模式适配器 (10.3.4-7)

### S1 [严重] CodeAdapter.CreateInstance 未创建 DeepAgent 实例

**Python 代码** (`interface_code.py:273-292`):
```python
self._instance = create_deep_agent(
    model=model, card=agent_card, system_prompt=build_code_system_prompt(), ...
)
await self._instance.ensure_initialized()
```

**Go 代码** (`code_adapter.go:103-230`):
```go
func (c *CodeAdapter) CreateInstance(ctx context.Context, config map[string]any, mode string, subMode string) error {
    c.deep.model = c.deep.createModel(configBase)
    // 步骤 19-24: 全部是 ⤵️ 占位符，没有调用 CreateDeepAgent
    return nil
}
```

**差异说明**：CodeAdapter.CreateInstance 执行完步骤 12（创建 model）后，步骤 14-24 全部标记为 ⤵️ 占位符。Python 中步骤 19 调用 create_deep_agent() 创建 self._instance，步骤 20 调用 ensure_initialized()，这些是核心初始化逻辑。没有 _instance，后续所有请求（ProcessMessage/Stream/Interrupt）都会因 d.instance == nil 而失败。

**修复方案**：在 CodeAdapter.CreateInstance 中补全步骤 14-24，调用 harness.CreateDeepAgent() 并设置 c.deep.instance。

---

### S2 [严重] CodeAdapter.CreateInstance 未设置 workspaceDir 的两段式逻辑

**Python 代码** (`interface_code.py:243-250`):
```python
self._workspace_dir = (
    self._project_dir
    or config.get("workspace_dir")
    or str(get_agent_workspace_dir())
)
self._agent_workspace_dir = str(get_agent_workspace_dir())
```

**Go 代码** (`code_adapter.go:168-177`):
```go
if c.deep.projectDir != "" {
    c.deep.workspaceDir = c.deep.projectDir
} else if v, ok := c.deep.configCache["workspace_dir"]; ok {
    // ...
}
if c.deep.workspaceDir == "" {
    c.deep.workspaceDir = workspace.AgentRootDir()
}
```

**差异说明**：Python 有两个独立字段 `_workspace_dir` 和 `_agent_workspace_dir`，Go 只有一个 `workspaceDir`。Python 中 `_workspace_dir` 优先用 `project_dir`（用于 LspTool sandbox 校验），`_agent_workspace_dir` 始终指向系统 workspace（用于 coding_memory、todo 等数据存储）。Go 中没有区分这两个路径，coding_memory 等数据可能写入用户项目目录而非系统 workspace。

**修复方案**：在 DeepAdapter 或 CodeAdapter 中增加 `agentWorkspaceDir` 字段，CodeAdapter.CreateInstance 中独立设置。coding_memory 目录等数据存储应使用 `agentWorkspaceDir`。

---

### S3 [严重] ProcessInterrupt 的 cancel 分支缺少 rail.abort 和 reset_for_new_task

**Python 代码** (`interface_deep.py:3368-3374`):
```python
if self._stream_event_rail is not None:
    self._stream_event_rail.abort(request.session_id)
    self._stream_event_rail.collect_cancelled_tool_updates(request.session_id)
    cancelled_tool_results = self._stream_event_rail.get_cancelled_tool_results(request.session_id)
    self._stream_event_rail.clear_cancelled_tool_results(request.session_id)
    self._stream_event_rail.reset_for_new_task(request.session_id)
```

**Go 代码** (`deep_adapter.go:1000-1008`):
```go
case "cancel":
    if sessionActive && d.instance != nil && d.otherActiveSessions(normalizedSID) == 0 {
        d.instance.Abort(ctx)
    }
    d.unmarkSessionActive(normalizedSID)
    // ⤵️ 10.6.3-10: rail.abort(sessionID) + rail.reset_for_new_task(sessionID)
```

**差异说明**：Python cancel 分支先调用 `streamEventRail.abort(session_id)`（per-session），再调用 `collect/get/clear_cancelled_tool_results` 收集中断工具信息，最后调用 `reset_for_new_task` 解除 pause 阻塞。Go 中这些全部标记为 ⤵️ 占位符。缺少 `rail.abort` 会导致 cancel 时无法在 checkpoint 处注入 CancelledError 打断执行；缺少 `reset_for_new_task` 会导致 pause 状态残留。

**修复方案**：待 StreamEventRail 实现后，在 cancel 分支补全上述 5 个调用。

---

### S4 [严重] ProcessInterrupt 的 supplement 分支缺少 rail.abort 和 _cancel_scheduler_running_tasks

**Python 代码** (`interface_deep.py:3330-3348`):
```python
if self._stream_event_rail is not None:
    self._stream_event_rail.abort(request.session_id)
    self._stream_event_rail.collect_cancelled_tool_updates(request.session_id)
    cancelled_tool_results = self._stream_event_rail.get_cancelled_tool_results(request.session_id)
    self._stream_event_rail.clear_cancelled_tool_results(request.session_id)
if self._instance is not None:
    other_count = self._other_active_sessions(_normalized_sid)
    if other_count == 0:
        await self._instance.abort()
        self._cancel_scheduler_running_tasks()
```

**Go 代码** (`deep_adapter.go:989-998`):
```go
case "supplement":
    if newInput != nil {
        d.markSessionActive(normalizedSID)
    }
    // ⤵️ 10.6.3-10: rail.abort(sessionID)
    if sessionActive && d.instance != nil && d.otherActiveSessions(normalizedSID) == 0 {
        d.instance.Abort(ctx)
    }
```

**差异说明**：Python supplement 分支先调用 `streamEventRail.abort + collect + get + clear`，然后才判断 `other_active_sessions` 是否为 0 来决定 `instance.abort`。Go 中 `rail.abort` 全部为 ⤵️ 占位符，且缺少 `_cancel_scheduler_running_tasks()` 调用。

**修复方案**：待 StreamEventRail 实现后补全 `rail.abort/collect/get/clear` 序列，以及 `_cancel_scheduler_running_tasks` 调用。

---

### S5 [严重] ProcessInterrupt 缺少 cancelled_tool_results 和 updated_todos 附加到 payload

**Python 代码** (`interface_deep.py:3432-3468`):
```python
payload = {
    "event_type": "chat.interrupt_result",
    "intent": intent,
    "success": success,
    "message": message,
}
if new_input:
    payload["new_input"] = new_input
if intent not in ("pause", "resume", "supplement") and updated_todos is not None:
    payload["todos"] = updated_todos
if cancelled_tool_results:
    payload["cancelled_tools"] = cancelled_tool_results
    for tool_info in cancelled_tool_results:
        append_history_record(...)
```

**Go 代码** (`deep_adapter.go:1017-1037`):
```go
payload := map[string]any{
    "event_type": "chat.interrupt_result",
    "intent":     intent,
    "success":    true,
    "message":    interruptMsg,
}
if newInput != nil {
    payload["new_input"] = newInput
}
// ⤵️ 10.6.3-10: todos 和 cancelled_tools 依赖 StreamEventRail 实现
```

**差异说明**：Python 在 cancel 时将 `updated_todos` 和 `cancelled_tool_results` 附加到 payload，并写入历史记录。Go 中这部分完全标记为 ⤵️，导致前端无法收到被取消的工具状态和更新后的 todo 列表，刷新后工具状态会不正确。

**修复方案**：待 StreamEventRail 和 TodoModifyTool 实现后，在 cancel 分支收集 `cancelled_tool_results` 和 `updated_todos`，附加到 payload 并写入历史。

---

### S6 [严重] ProcessInterrupt 的 cancel 分支缺少 _cancel_pending_todos 调用

**Python 代码** (`interface_deep.py:3401-3406`):
```python
updated_todos = None
if request.session_id:
    try:
        updated_todos = await self._cancel_pending_todos(request.session_id)
    except Exception as exc:
        logger.warning("[JiuWenClawDeepAdapter] 标记 todo cancelled 失败: %s", exc)
```

**Go 代码** (`deep_adapter.go:1000-1008`):
```go
case "cancel":
    // ...
    // ⤵️ 11.10: _cancel_scheduler_running_tasks()
    // ⤵️ 10.6.3-10: _cancel_pending_todos(sessionID)
```

**差异说明**：Python cancel 时调用 `_cancel_pending_todos` 将未完成的 todo 标记为 cancelled。Go 中标记为 ⤵️ 占位符。缺少此调用会导致取消后 todo 列表仍显示进行中状态。

**修复方案**：待 TodoModifyTool 实现后补全 `_cancel_pending_todos`。

---

### S7 [严重] ProcessInterrupt 缺少 auto_harness cancel 逻辑

**Python 代码** (`interface_deep.py:3408-3421`):
```python
try:
    if self._auto_harness_service is not None \
        and self._auto_harness_service.has_active_run(request.session_id):
        self._auto_harness_service.cancel_session_run(request.session_id)
except Exception as exc:
    logger.warning("[JiuWenClawDeepAdapter] Failed to cancel auto_harness run: %s", exc)
```

**Go 代码**：无对应实现（`autoHarnessService` 字段标记为 ⤵️）。

**差异说明**：Python 在 cancel 时检查并取消 `auto_harness` 活跃运行。Go 完全缺失此逻辑。如果 `auto_harness` 运行中用户发起 cancel，Go 不会终止 `auto_harness` 运行。

**修复方案**：待 `autoHarnessService` 实现后补全 cancel 逻辑。

---

### S8 [严重] _resolve_prompt_language 实现差异 — 字段名和 fallback 逻辑不一致

**Python 代码** (`interface_deep.py`):
```python
@staticmethod
def _resolve_prompt_language() -> str:
    config_base = get_config()
    return str(config_base.get("preferred_language", "zh")).strip().lower()
```

**Go 代码** (`deep_adapter_config.go:254-261`):
```go
func (d *DeepAdapter) resolvePromptLanguage() string {
    if v, ok := d.configCache["prompt_language"]; ok {
        if s, ok := v.(string); ok && s != "" {
            return s
        }
    }
    return d.resolveRuntimeLanguage()
}
```

**差异说明**：(1) Python 从全局 `get_config()` 读取 `preferred_language` 字段，Go 从 `d.configCache`（react 配置段）读 `prompt_language`，字段名不同；(2) Python fallback 固定 "zh"，Go 回退到 `resolveRuntimeLanguage()`。

**修复方案**：`resolvePromptLanguage` 应该从 `configBase` 顶层读取 `preferred_language` 字段，默认 "zh"，不对齐到 runtime language。

---

### S9 [严重] _resolve_runtime_language 缺少 resolve_language() 标准化映射

**Python 代码** (`interface_deep.py`):
```python
def _resolve_runtime_language(self) -> str:
    return resolve_language(self._resolve_prompt_language())
```

**Go 代码** (`deep_adapter_config.go:243-250`):
```go
func (d *DeepAdapter) resolveRuntimeLanguage() string {
    if v, ok := d.configCache["language"]; ok {
        if s, ok := v.(string); ok && s != "" {
            return s
        }
    }
    return "zh"
}
```

**差异说明**：Python 的 `_resolve_runtime_language` 调用 `_resolve_prompt_language()` 获取语言字符串，再通过 `resolve_language()` 做标准化映射（如 "chinese" → "zh"）。Go 直接从 `configCache` 读 `language` 字段，跳过了 `resolve_language()` 的标准化步骤。如果用户配置了 "chinese" 或 "en" 等非标准值，Go 不会做映射。

**修复方案**：`resolveRuntimeLanguage` 应该调用 `prompts.ResolveLanguage(d.resolvePromptLanguage())` 来保证标准化映射一致。

---

### S10 [严重] _filesystem_rail_enabled_for_profile 逻辑差异 — 判断条件完全不同

**Python 代码** (`interface_deep.py`):
```python
def _filesystem_rail_enabled_for_profile(self) -> bool:
    raw = self._instance_overrides.get("enable_filesystem_rail", True)
    return bool(raw)
```

**Go 代码** (`deep_adapter_config.go:236-239`):
```go
func (d *DeepAdapter) filesystemRailEnabledForProfile(instanceOverrides map[string]any) bool {
    return !d.isAcpToolProfile(instanceOverrides)
}
```

**差异说明**：Python 检查 `instance_overrides` 中的 `enable_filesystem_rail` 字段，默认为 True。Go 的逻辑是 `!isAcpToolProfile`，完全不同的判断条件。Python 允许用户通过 `enable_filesystem_rail=False` 在非 ACP 模式下禁用 filesystem rail，Go 不支持这个配置。

**修复方案**：`filesystemRailEnabledForProfile` 应该先检查 `instanceOverrides["enable_filesystem_rail"]`，默认 True，与 Python 对齐。

---

### S11 [严重] createModel 中 _build_model_from_entry 差异 — Go 额外设置了 topP=0.1

**Python 代码** (`interface_deep.py`):
```python
def _build_model_from_entry(mcc: dict, mco: dict) -> Model:
    name = mcc.get("model_name", "")
    m_config = ModelRequestConfig(
        model=name,
        temperature=mco.get("temperature", 0.95),
    )
```

**Go 代码** (`deep_adapter.go:1313-1387`):
```go
topP := 0.1  // ← 额外默认值
if v, ok := mco["top_p"]; ok {
    if f, ok := v.(float64); ok {
        topP = f
    }
}
mConfig := llmschema.NewModelRequestConfig(
    llmschema.WithModelName(name),
    llmschema.WithTemperature(temperature),
    llmschema.WithTopP(topP),  // ← 额外字段
)
```

**差异说明**：Python 的 `ModelRequestConfig` 只设置 `model` 和 `temperature`，Go 额外设置了 `topP=0.1`。这可能导致模型行为差异。

**修复方案**：移除 topP 的默认值设置，或仅在 mco 中明确配置了 top_p 时才设置。

---

### S12 [严重] _apply_model_to_react_agent 差异 — 缺少 config 和 modelRequestConfig 同步更新

**Python 代码** (`interface_deep.py`):
```python
def _apply_model_to_react_agent(self, model: Model) -> None:
    react_agent = getattr(self._instance, "_react_agent", None)
    if react_agent is None:
        return
    if callable(getattr(react_agent, "set_llm", None)):
        react_agent.set_llm(model)
    config = getattr(react_agent, "_config", None)
    if config is not None:
        config.model_name = model.model_config.model_name
        config.model_client_config = model.model_client_config
        config.model_config_obj = model.model_config
    self._model_request_config = model.model_config
```

**Go 代码** (`deep_adapter.go:664-668`):
```go
if resolvedModel != nil && d.instance != nil {
    if reactAgent := d.instance.ReactAgent(); reactAgent != nil {
        reactAgent.SetLLM(resolvedModel)
    }
}
```

**差异说明**：Python 在 `set_llm` 后还同步更新 `react_agent._config` 的 `model_name/model_client_config/model_config_obj` 三个字段，并且更新 `self._model_request_config`。Go 只调了 `SetLLM`，缺少对 config 和 `d.modelRequestConfig` 的同步更新。这会导致后续请求的模型名称解析不正确（`resolveModelName()` 仍返回旧模型名）。

**修复方案**：在 `SetLLM` 后同步更新 `d.modelRequestConfig` 和 `d.modelClientConfig`，且确保 ReactAgent 的 config 也同步更新。

---

### S13 [严重] _has_valid_model_config 逻辑过于简化 — Go 只查 modelCache，Python 有四层 fallback

**Python 代码** (`interface_deep.py:3540-3577`):
```python
def _has_valid_model_config(self, requested_model_name: str = "") -> bool:
    # 1. 优先检查请求中指定的模型
    # 2. 检查默认模型
    # 3. 回退：检查 cache 中是否有任意一个有效模型
    # 4. 最后从 config.yaml 重新解析
```

**Go 代码** (`deep_adapter.go:1601-1607`):
```go
func (d *DeepAdapter) hasValidModelConfig(requestedModelName string) bool {
    if requestedModelName == "" {
        return true
    }
    _, ok := d.modelCache[requestedModelName]
    return ok
}
```

**差异说明**：(1) Go 空字符串直接返回 true，Python 空字符串时继续检查默认模型和 fallback；(2) Go 不检查模型的 `api_key/api_base` 是否可用（`_mcc_looks_usable`），只检查 key 是否存在；(3) Go 没有 Python 的三层 fallback 逻辑。

**修复方案**：对齐 Python 的四层检查逻辑，至少需要添加 model `api_key/api_base` 的可用性验证。

---

### S14 [严重] CreateInstance 缺少 context_engine_config 和 completion_timeout 参数

**Python 代码** (`interface_deep.py:2599-2605`):
```python
self._instance = create_deep_agent(
    **common_kwargs,
    context_engine_config=_deep_agent_context_engine_config(config),
    completion_timeout=config.get("completion_timeout", 3600.0),
)
```

**Go 代码** (`deep_adapter.go:417-435`):
```go
params := harness_config.CreateDeepAgentParams{
    // ... 无 context_engine_config
    // ... 无 completion_timeout
}
```

**差异说明**：Go 的 `CreateDeepAgentParams` 缺少 `context_engine_config`（控制 KV cache release 开关）和 `completion_timeout`（默认 3600.0s）。

**修复方案**：在 `CreateDeepAgentParams` 中添加 `ContextEngineConfig` 和 `CompletionTimeout` 字段，并在 CreateInstance 中传值。

---

### S15 [严重] CreateInstance 缺少 auto_create_workspace=False 参数

**Python 代码** (`interface_deep.py:2596`):
```python
common_kwargs = dict(
    ...
    auto_create_workspace=False
)
```

**Go 代码**：缺少此参数。

**差异说明**：Python 显式传 `auto_create_workspace=False`，Go 未传。如果 Go 的 `CreateDeepAgentParams` 默认值为 true，则在 workspace 目录不存在时会自动创建，与 Python 行为不一致。

**修复方案**：在 `CreateDeepAgentParams` 中添加 `AutoCreateWorkspace: false`。

---

### S16 [严重] CreateInstance 缺少 _jiuwenswarm_project_dir setattr

**Python 代码** (`interface_deep.py:2609`):
```python
setattr(self._instance, "_jiuwenswarm_project_dir", self._project_dir or self._workspace_dir)
```

**Go 代码**：无对应代码。

**差异说明**：Python 在 instance 上设置了 `_jiuwenswarm_project_dir` 属性，供其他模块（如 StreamEventRail、evolution helpers）读取项目目录。Go 缺少此设置。

**修复方案**：在 CreateInstance 步骤 21 后设置 instance 的 `projectDir` 属性（或在 DeepAgent 结构体中添加对应字段）。

---

### S17 [严重] MCP 配置路径差异 — Python 嵌套结构 vs Go 扁平结构

**Python 代码** (`interface_deep.py:1020-1036`):
```python
mcp_cfg = config_base.get("mcp", {})
servers = mcp_cfg.get("servers", [])
```

**Go 代码** (`deep_adapter_mcp.go:132`):
```go
mcpSection, _ := configBase["mcp_servers"].(map[string]any)
```

**差异说明**：Python 从 `config_base["mcp"]["servers"]` 读取（嵌套结构），Go 从 `config_base["mcp_servers"]` 读取（扁平结构）。如果配置文件使用 Python 的格式，Go 无法读取到任何 MCP 配置。

**修复方案**：将路径改为 `config_base["mcp"]["servers"]`，与 Python 对齐。

---

### S18 [严重] /switch 模式映射逻辑完全丢失

**Python 代码** (`message_handler.py:755-820`):
```python
if parsed.action is ParsedControlAction.SWITCH_OK:
    switch_str = parsed.switch_subcommand or ""
    target_mode: ChannelMode | None = None
    if switch_str == "plan":
        if state.mode in (ChannelMode.AGENT_PLAN, ChannelMode.AGENT_FAST):
            target_mode = ChannelMode.AGENT_PLAN
        elif state.mode in (ChannelMode.CODE_PLAN, ChannelMode.CODE_NORMAL, ChannelMode.CODE_TEAM):
            target_mode = ChannelMode.CODE_PLAN
    elif switch_str == "fast":
        if state.mode in (ChannelMode.AGENT_PLAN, ChannelMode.AGENT_FAST):
            target_mode = ChannelMode.AGENT_FAST
    # ...
    if target_mode is None:
        # 发送 "非法指令"
        return True
```

**Go 代码** (`slash_cmd.go:120-161`):
```go
case command_parser.ActionSwitchOK:
    switch parsed.SwitchSubcommand {
    case "plan":
        newMode = ChannelModeAgentPlan  // 始终为 agent.plan，不管当前模式
    case "fast":
        newMode = ChannelModeAgentFast  // 始终为 agent.fast
    case "normal":
        newMode = ChannelModeCodeNormal // 始终为 code.normal
    case "team":
        newMode = ChannelModeTeam       // 始终为 team
    }
```

**差异说明**：Go 的 `/switch` 实现完全忽略了当前 mode 状态来决定目标模式。Python 中 `/switch plan` 在 code 模式下应该切换到 `code.plan`，在 agent 模式下切换到 `agent.plan`；`/switch fast` 仅在 agent 模式下有效；`/switch team` 仅在 code 模式下有效（映射到 `code.team`）。Go 中这些逻辑全部丢失，且当 `/switch fast` 在 code 模式下应该返回"非法指令"时，Go 会错误地切换到 `agent.fast`。此外，Python 对 `/switch` 还有一个 `target_mode is None` 时返回"非法指令"的分支，Go 中完全没有。

**修复方案**：在 `ActionSwitchOK` 分支中根据 `state.Mode` 判断当前模式家族（agent/code），再决定 target_mode；当 switch 在当前模式下不可用时，发送"非法指令"通知。

**复杂问题流程示例**：
```
用户当前模式: code.normal
用户输入: /switch plan

Python 流程:
1. 解析到 SWITCH_OK, switch_subcommand="plan"
2. 检查当前 state.mode = CODE_NORMAL ∈ (CODE_PLAN, CODE_NORMAL, CODE_TEAM)
3. 设置 target_mode = CODE_PLAN
4. 发送模式切换通知 → code.plan

Go 流程 (当前错误):
1. 解析到 ActionSwitchOK, SwitchSubcommand="plan"
2. 直接设置 newMode = ChannelModeAgentPlan
3. 发送模式切换通知 → agent.plan  ← 错误！应该是 code.plan

修复后 Go 流程:
1. 解析到 ActionSwitchOK, SwitchSubcommand="plan"
2. 检查当前 state.Mode 属于哪个模式家族
3. code 家族 → newMode = ChannelModeCodePlan
4. agent 家族 → newMode = ChannelModeAgentPlan
5. 发送模式切换通知 → code.plan ✓
```

---

### S19 [严重] extractTextFromParams 只提取 "content" 字段，遗漏 "query" 字段

**Python 代码** (`message_handler.py:629-631`):
```python
params = msg.params or {}
text = str(params.get("query") or params.get("content") or "").strip()
```

**Go 代码** (`slash_cmd.go:434-448`):
```go
func extractTextFromParams(params json.RawMessage) string {
    if content, ok := paramsMap["content"]; ok {
        if s, isStr := content.(string); isStr { return s }
    }
    return ""
}
```

**差异说明**：Python 优先提取 `params["query"]`，其次 `params["content"]`。Go 的 `extractTextFromParams` 只检查 `params["content"]`，完全忽略了 `params["query"]`。在受控 IM 渠道中，消息通常以 `query` 字段传入文本，`content` 字段通常为空或不存在。这意味着 slash 命令（如 `/new_session`、`/mode agent` 等）在 `query` 字段中时将完全无法被识别。

**修复方案**：修改 `extractTextFromParams`，先检查 `paramsMap["query"]`，再 fallback 到 `paramsMap["content"]`，与 Python 一致。

---

### S20 [严重] rememberUserQueryContext 缺少 is_supplement 和 req_method 过滤

**Python 代码** (`message_handler.py:223-234`):
```python
def _remember_user_query_context(self, msg: "Message") -> None:
    if not self._is_chat_send_message(msg):
        return
    if not isinstance(msg.params, dict) or msg.params.get("is_supplement") is True:
        return
```

**Go 代码** (`message_handler.go:290-319`):
```go
func (mh *MessageHandler) rememberUserQueryContext(msg *schema.Message) {
    if msg == nil || msg.SessionID == "" { return }
    if len(msg.Params) == 0 { return }
    // ... 直接解析并记录，没有检查 is_supplement 和 req_method
}
```

**差异说明**：Python 中 `_remember_user_query_context` 有两个前置过滤：(1) 只记录 `chat.send` 消息；(2) 跳过 `is_supplement=True` 的消息。Go 实现两个过滤都没有。这会导致非 chat.send 消息的 params 中的 query/content 也会被记录，覆盖真正有用的用户查询。

**修复方案**：添加 `msg.ReqMethod == schema.ReqMethodChatSend` 的检查，以及解析 params 检查 `is_supplement != true` 的过滤。

---

### S21 [严重] WebChannel.Send() 中 full-payload 路径直接修改了 msg.Payload，存在并发副作用

**Python 代码** (`web_connect.py:367-371`):
```python
if event_name in (...):
    payload = {**msg.payload}  # 浅拷贝，不修改原始 payload
    if "session_id" not in payload and msg.session_id:
        payload["session_id"] = msg.session_id
```

**Go 代码** (`web_connect.go:371-378`):
```go
if isFullPayloadEvent(eventName) {
    payload := msg.Payload
    if _, ok := payload["session_id"]; !ok {
        payload["session_id"] = msg.SessionID  // 直接修改了 msg.Payload！
    }
```

**差异说明**：Python 使用 `{**msg.payload}` 浅拷贝后再修改，而 Go 直接用 `payload := msg.Payload` 只是引用赋值，后续修改直接修改了原始 `msg.Payload`，在并发广播场景下会产生副作用。

**修复方案**：改为浅拷贝，与 res 帧路径的浅拷贝处理一致。

---

### S22 [严重] WebChannel.Send() 缺少 payload 非 dict 时的 pure-text 回退路径

**Python 代码** (`web_connect.py:391-397`):
```python
else:
    content = str((msg.params or {}).get("content", "") or "")
    payload = {
        "session_id": msg.session_id,
        "content": content,
    }
```

**Go 代码** (`web_connect.go:380-383`):
```go
} else {
    payload := extractPureTextPayload(msg, eventName)
    // 当 msg.Payload 为 nil 时不会提取 content from params
}
```

**差异说明**：Python 在 payload 非 dict 时从 `msg.params` 提取 content。Go 完全缺少 `msg.Payload` 为非 dict 类型时的 params 回退路径。

**修复方案**：在 `extractPureTextPayload` 或 `Send()` 中增加 payload 为 nil 或非 dict 时从 `msg.Params` 提取 content 的逻辑。

---

### S23 [严重] AgentClient.Disconnect() 关闭 channel 后仍可能被 receiverLoop 写入导致 panic

**Python 代码** (`agent_client.py:389-405`):
```python
async with self._queue_lock:
    queue = self._message_queues.get(rid)
    if queue is None:
        return
    self._cancelled_request_ids.add(rid)
    del self._message_queues[rid]
```

**Go 代码** (`agent_client.go:159-163`):
```go
ac.messageQueuesMu.Lock()
for rid, q := range ac.messageQueues {
    close(q)  // 关闭 channel
    delete(ac.messageQueues, rid)
}
ac.messageQueuesMu.Unlock()
```

**差异说明**：Go 的 `Disconnect()` 直接 `close(q)` 清理队列，但 `receiverLoop` 可能仍在运行并调用 `routeToQueue`，向已关闭的 channel 写入会 panic。Python 先停止 receiver_task（cancel），再清理队列。

**修复方案**：Disconnect() 中应先确保 receiverLoop 已退出（等待 goroutine 结束），再清理队列；或在 `routeToQueue` 中用 `recover` 防恐慌，或在 `close(q)` 之前先从 messageQueues 删除，避免 receiverLoop 找到已关闭的 queue。

---

### S24 [严重] generate_isolation_key_template 在 CUSTOM scope 且 customID 为空时的行为不一致

**Python 代码** (`sys_operation.py:54-58`):
```python
elif container_scope == ContainerScope.CUSTOM:
    if custom_id:
        identity = custom_id
    else:
        raise ValueError("container_scope is CUSTOM but custom_id is None")
```

**Go 代码** (`sys_operation_card.go:163-166`):
```go
case ContainerScopeCustom:
    identity = customID
default:
    identity = customID
```

**差异说明**：Python 在 ContainerScope=CUSTOM 且 custom_id 为空时抛出 ValueError，Go 静默使用空字符串作为 identity。这会导致生成格式错误的隔离键模板（如 `custom_pre_deploy_aio__`），且不报错，使问题难以排查。

**修复方案**：在 ContainerScopeCustom 分支中，当 customID 为空时返回错误或 panic，与 Python 行为对齐。

---

## 二、一般问题 (G1-G26)

### G1 [一般] _build_agent_rails 的 Rail 构建顺序与 Python 不一致

**Python 代码** (`interface_deep.py:2130-2156`)：
```
1. _runtime_prompt_rail → 2. _filesystem_rail → 3. _skill_rail → 4. _response_prompt_rail
→ 5. _stream_event_rail → 6. _task_planning_rail → 7. _security_rail → 8. _heartbeat_rail
→ 9. _avatar_rail → 10. _subagent_rail → 11. _permission_rail → 12. _context_processor_rail
→ 13. UserHookRail
```

**Go 代码** (`deep_adapter_rails.go:17-170`)：
```
1. heartbeatRail → 2. taskPlanningRail → 3. filesystemRail → 4. agentModeRail ← Python无
→ 5. mcpRail ← Python无 → 6. progressiveToolRail ← Python无 → 7. skillRail
→ 8. skillEvolutionRail ← 不在冷启动 → 9. skillCreateRail ← 不在冷启动
→ 10. streamEventRail → 11. subagentRail → 12. securityRail → 13. memoryRail ← 不在冷启动
→ 14. externalMemoryRail ← 不在冷启动 → 15. avatarRail → 16. runtimePromptRail
→ 17. responsePromptRail → 18. contextAssembleRail ← 不在冷启动
→ 19. contextProcessorRail → 20. permissionRail
```

**差异说明**：Python 的 runtime_prompt_rail 排在最前（先拦截），Go 排在倒数第 5 位；Rails 执行顺序影响 before_model_call/after_model_call 的调用链，顺序不一致可能导致拦截逻辑差异。此外 SkillEvolutionRail/MemoryRail 等不应在冷启动时挂载。

**修复方案**：对齐 Python 的 Rail 列表和顺序，冷启动时只构建 Python 指定的 Rails。

---

### G2 [一般] CodeAdapter.CreateInstance 缺少 _refresh_multimodal_configs 调用

**Python 代码** (`interface_code.py:232`)：
```python
self._refresh_multimodal_configs(config_base)
```

**Go 代码** (`code_adapter.go:131`)：
```go
// 步骤 4: ⤵️ 10.3.7-11: _refresh_multimodal_configs(configBase)
```

**修复方案**：在 CodeAdapter.CreateInstance 中补全 `c.deep.refreshMultimodalConfigs(configBase)` 调用。

---

### G3 [一般] DeepAdapter.CreateInstance 缺少 load_dotenv override=True 语义

**Python 代码** (`interface_deep.py:227`)：
```python
load_dotenv(dotenv_path=get_env_file(), override=True)
```

**Go 代码** (`deep_adapter.go:308-309`)：
```go
if err := dotenv.Load(workspace.EnvFile()); err != nil {
    logger.Warn(logComponent).Err(err).Msg("load_dotenv 失败")
}
```

**差异说明**：Python 调用 `load_dotenv` 时传入 `override=True`，Go 的 `dotenv.Load` 默认行为需确认是否也 override。

**修复方案**：确认 Go `dotenv.Load` 是否支持 override 语义，如不支持需补充。

---

### G4 [一般] _build_configured_subagents 中 general_agent 判断逻辑不一致

**Python 代码** (`interface_deep.py:894-896`)：
```python
general_agent_cfg = subagents_cfg.get("general_agent")
if self._is_subagent_enabled(general_agent_cfg):
    should_add_general_purpose = True
```

**Go 代码** (`deep_adapter_config.go:127`)：
```go
shouldAddGeneralAgent := (d.subMode == "plan") || (d.mode != "" && strings.HasPrefix(d.mode, "agent"))
```

**差异说明**：Python 由 `subagents.general_agent.enabled` 配置决定，Go 硬编码了模式判断。两者判断条件完全不同。

**修复方案**：从 config 中读取 `subagents.general_agent.enabled` 配置来决定。

---

### G5 [一般] _is_subagent_enabled 实现语义不一致 — Python 严格模式 vs Go 默认启用

**Python 代码** (`interface_deep.py:868-869`)：
```python
@staticmethod
def _is_subagent_enabled(subagent_cfg: Any) -> bool:
    return isinstance(subagent_cfg, dict) and bool(subagent_cfg.get("enabled", False))
```

**Go 代码** (`deep_adapter_config.go:139-150`)：Go 的 `isSubagentEnabled` 在配置不存在时 fallback 到 `isSubagentDefaultEnabled`（explore/plan 默认 True）。

**差异说明**：Python 对所有 subagent 默认禁用（需要显式 `enabled: true`），Go 对 explore/plan 默认启用。

**修复方案**：对齐 Python 的严格语义，配置不存在或 `enabled` 非显式 true 时返回 false。

---

### G6 [一般] _build_configured_subagents 子代理列表差异 — Go 包含 explore/plan/code，Python 只有 research/browser

**Python 代码** (`interface_deep.py:878-970`)：只构建 research_agent 和 browser_agent + custom agents。

**Go 代码** (`deep_adapter_config.go:85-135`)：额外添加了 explore/plan/code 三个子代理。

**修复方案**：移除 explore/plan/code 子代理的构建逻辑，与 Python 对齐只构建 research/browser + custom agents。

---

### G7 [一般] create_instance 中 sys_operation nil 检查缺失

**Python 代码** (`interface_deep.py:2569-2571`)：
```python
sys_operation = self._create_sys_operation()
if sys_operation is None:
    raise RuntimeError("sys_operation is not available, maybe task is not running")
```

**Go 代码** (`deep_adapter.go:404`)：
```go
sysOpInstance, _ := d.createSysOperation(configBase)
```

**修复方案**：添加 nil 检查，sysOpInstance 为 nil 时返回错误。

---

### G8 [一般] CreateInstance 中 tool_cards 未存储到 d.toolCards

**Python 代码** (`interface_deep.py:2562`)：
```python
tool_cards = await self._get_tool_cards(agent_card.id)
self._tool_cards = tool_cards
```

**Go 代码** (`deep_adapter.go:397`)：
```go
toolCards := d.getToolCards(agentCard.ID)
// 未赋值给 d.toolCards
```

**修复方案**：添加 `d.toolCards = toolCards`。

---

### G9 [一般] ReloadAgentConfig 缺少 clear_config_cache/clear_memory_manager_cache 调用

**Python 代码** (`interface_deep.py:2662-2663`)：
```python
clear_config_cache()
clear_memory_manager_cache()
```

**Go 代码**：无对应调用。

**修复方案**：在 ReloadAgentConfig 开始时添加缓存清理逻辑。

---

### G10 [一般] ReloadAgentConfig 缺少 resolve_env_vars(config_base) 处理

**Python 代码** (`interface_deep.py:2679`)：
```python
config_base = resolve_env_vars(config_base)
```

**修复方案**：添加环境变量解析逻辑。

---

### G11 [一般] ReloadAgentConfig 缺少 ACP filesystem rail 卸载逻辑

**Python 代码** (`interface_deep.py:2693-2700`)：
```python
if not self._filesystem_rail_enabled_for_profile() and self._filesystem_rail is not None:
    await self._instance.unregister_rail(self._filesystem_rail)
    self._filesystem_rail = None
```

**修复方案**：在 ReloadAgentConfig 中添加 filesystem rail 卸载检查。

---

### G12 [一般] _resolve_enable_task_loop 逻辑差异 — Go 缺少 skill_create 联动

**Python 代码** (`interface_deep.py:2231-2243`)：
```python
skill_create_enabled = _get_skill_create_enabled(config_base)
configured_value = config.get("enable_task_loop", True)
if skill_create_enabled:
    return True  # force enable
return configured_value
```

**Go 代码** (`deep_adapter.go:1727-1734`)：
```go
func (d *DeepAdapter) resolveEnableTaskLoop(config map[string]any, configBase map[string]any) bool {
    if v, ok := config["enable_task_loop"]; ok {
        if b, ok := v.(bool); ok { return b }
    }
    return true
}
```

**修复方案**：添加 `_get_skill_create_enabled` 的对等检查逻辑。

---

### G13 [一般] CancelAgentWorkForSession 缺少错误分支中的 interrupt_result 通知

**Python 代码** (`message_handler.py:470-480`)：
```python
try:
    resp = await self._agent_client.send_request(env_interrupt)
except Exception as exc:
    await _cancel_tasks(tasks_to_cancel)
    if publish_interrupt_result:
        await self._send_interrupt_result_notification(
            msg.id, msg.channel_id, sid_for_agent, "cancel",
            message=f"任务终止失败: {exc}", success=False,
        )
    return
```

**Go 代码** (`cancel.go:44-55`)：`respErr != nil` 时只记录日志，不提前返回，不发送 `success=False` 通知。

**修复方案**：在 `respErr != nil` 时，取消网关侧流式任务，若 `publishInterruptResult` 则发送 `success=False` 的 interrupt_result 通知，然后 `return`。

---

### G14 [一般] CancelAgentWorkForSession 的"非预期响应"分支缺少错误消息提取

**Python 代码** (`message_handler.py:512-528`)：
```python
error_message = "任务终止失败"
if isinstance(payload, dict):
    raw_error = payload.get("error") or payload.get("message")
    if isinstance(raw_error, str) and raw_error.strip():
        error_message = raw_error.strip()
```

**Go 代码** (`cancel.go:77-85`)：硬编码 "取消失败"，不从 payload 提取 error/message 字段。

**修复方案**：从 `resp.Payload` 提取 `error`/`message` 字段构造错误消息。

---

### G15 [一般] shouldEmitProcessingStatusForStream 的判断条件不同

**Python 代码** (`message_handler.py:1866-1869`)：
```python
return msg.req_method == ReqMethod.CHAT_SEND
```

**Go 代码** (`dispatch.go:30-33`)：
```go
return string(channelType) != string(channel_manager.ChannelTypeWeb)
```

**差异说明**：Python 按 req_method 判断，Go 按渠道类型判断，是完全不同的维度。

**修复方案**：将判断条件改为 `msg.ReqMethod == schema.ReqMethodChatSend`，与 Python 一致。

---

### G16 [一般] handlePauseResume 中 env 变量构造后未使用

**Go 代码** (`forward_loop.go:439-463`)：
```go
_ = e2a.MessageToE2AOrFallback(agentMsg)  // ← 计算了 env 但丢弃
go mh.sendInterruptToAgent(ctx, msg, intent)  // ← 内部重新 buildCancelMessage
```

**修复方案**：将 `e2a.MessageToE2AOrFallback(agentMsg)` 的结果传入 `sendInterruptToAgent`，或者移除无用的 `_ = ...` 行。

---

### G17 [一般] handleSupplement 中缺少 await asyncio.gather（网关侧流式任务取消后未等待完成）

**Python 代码** (`message_handler.py:2284-2300`)：
```python
if tasks_to_cancel:
    await asyncio.gather(*tasks_to_cancel, return_exceptions=True)
```

**Go 代码** (`forward_loop.go:352-361`)：
```go
for _, reqID := range requestIDs {
    mh.cancelStreamTask(reqID)
    // 只是调了 cancel()，没有等待 goroutine 退出
}
```

**修复方案**：考虑增加 WaitGroup 或 channel 来等待流式 goroutine 退出。

---

### G18 [一般] CancelAgentSessionsOnDisconnect 缺少 mode 注入到 cancel 消息

**Python 代码** (`message_handler.py:544-553`)：
```python
disconnect_params = {"intent": "cancel", "session_id": sid}
if disconnect_state is not None:
    disconnect_params["mode"] = disconnect_state.mode.value
```

**Go 代码** (`disconnect.go:39-47`)：cancelMsg 的 Params 为空，没有注入 mode。

**修复方案**：从 channelStates 提取 mode 并注入到 cancelMsg 的 Params 中。

---

### G19 [一般] config.get 异常兜底缺少 skill_create 和 evolution_auto_scan 的 setdefault

**Python 代码** (`app_web_handlers.py:647-648`)：
```python
payload.setdefault("skill_create", "false")
payload.setdefault("evolution_auto_scan", "false")
```

**Go 代码** (`web_handlers.go:586-601`)：异常兜底分支缺少这两个字段。

**修复方案**：在异常兜底分支中添加。

---

### G20 [一般] config.get 异常兜底缺少 free_search_ddg_enabled/bing_enabled

**Python 代码** (`app_web_handlers.py:651-652`)：
```python
payload.setdefault("free_search_ddg_enabled", "false")
payload.setdefault("free_search_bing_enabled", "false")
```

**修复方案**：在异常兜底分支中添加。

---

### G21 [一般] session.delete 缺少 session_dir.is_dir() 检查

**Python 代码** (`app_web_handlers.py:1358-1362`)：
```python
if not session_dir.is_dir():
    await channel.send_response(ws, req_id, ok=False, error="session is not a directory")
    return
```

**Go 代码** (`web_handlers.go:1121`)：
```go
if err := os.RemoveAll(sessionDir); err != nil {
```

**修复方案**：在 `os.RemoveAll` 前增加 `IsDir()` 检查。

---

### G22 [一般] OperationRegistry.Register 幂等性检查逻辑不一致

**Python 代码** (`registry.py:83-86`)：比较整个 OperationDef 的等价性，不等价就覆盖。

**Go 代码** (`registry.go:62-64`)：只要同名称+模式已存在就跳过，不检查是否相同。

**修复方案**：在已存在时检查新旧定义是否相同，相同才跳过，不同则覆盖或记录日志。

---

### G23 [一般] ShellProcessRegistry 的 WriteStdinForSession 广播语义与 Python 不一致

**Python 代码**：write_stdin 通过 ShellOperation 直接获取进程的 stdin pipe 写入（一次只写一个进程）。

**Go 代码** (`shell_process_registry.go:156-193`)：`WriteStdinForSession` 向一个 session 下所有进程广播。

**修复方案**：确认广播语义是否是有意设计。如果 Python 只针对特定进程，Go 也应支持指定进程写入。

---

### G24 [一般] CreateSandboxSysOpCard 日志对齐 — Go 缺少大量 Python 日志字段

**Python 代码** (`sysop_builder.py:730-763`)：日志包含 idle_check_interval、preserve_file_sharing_mode、filesystem_policy.files/directories/read_write/read_only、preserve_files_upload 等字段。

**Go 代码** (`builder.go:118-130`)：只记录了部分字段。

**修复方案**：补充缺失的日志字段，与 Python 逐一对照。

---

### G25 [一般] CreateSandboxSysOpCard 的 bindMountsCount 计算可能失败（类型断言不匹配）

**Go 代码** (`builder.go:112-117`)：
```go
if bmSlice, ok := bm.([]map[string]any); ok {
    bindMountsCount = len(bmSlice)
}
```

**差异说明**：经过 JSON 序列化/反序列化后，类型会变成 `[]any`（每个元素是 `map[string]any`），直接断言为 `[]map[string]any` 会失败。

**修复方案**：改为先断言为 `[]any`，再遍历计算长度。

---

### G26 [一般] i18n key 错误：blueput.default_persona 应为 blueprint.default_persona

**Python 代码** (`i18n.py:37`)：`"blueprint.default_persona": "天才项目管理专家"`

**Go 代码** (`i18n.go:44`)：`"blueput.default_persona": "天才项目管理专家"`

**修复方案**：将 Go i18n.go 中 `"blueput.default_persona"` 改为 `"blueprint.default_persona"`，同时修改 blueprint.go:207 中的调用。

---

## 三、提示问题 (T1-T14)

### T1 [提示] handleSlashCommand 缺少 _ensure_evolution_rail_for_slash 检查

**Python 代码** (`interface_deep.py:4319-4347`)：每个 `/evolve*` 命令前先检查 evolution rail 可用性。

**Go 代码** (`deep_adapter_slash.go:23-36`)：没有检查。

**修复方案**：在 handleSlashCommand 的每个 `/evolve*` 分支前增加 `ensureEvolutionRailForSlash` 检查。

---

### T2 [提示] buildRecapPrompt 未对齐 Python 的 RECENT_MESSAGE_WINDOW 常量

**Python 代码** (`recap_prompts.py:5`)：`RECENT_MESSAGE_WINDOW = 30`

**修复方案**：在 Go 中定义 `RecentMessageWindow = 30` 常量和 `BuildRecapPrompt` 函数，模板文字一比一复刻 Python 原文。

---

### T3 [提示] rewindSlashConfirmPrompt 提示文本不完整

**Python 代码** (`message_handler.py:1041-1045`)：4 行确认提示，包含不可逆警告和注意事项。

**Go 代码** (`slash_cmd.go:346`)：只有 1 行简短提示。

**修复方案**：补全提示文本，一比一复刻 Python 的提示文本。

---

### T4 [提示] rewindSlashNotice 缺少本地 fallback

**Python 代码** (`message_handler.py:1075-1127`)：E2A-first + 本地 fallback 两层。

**Go 代码** (`slash_cmd.go:353-431`)：只有 E2A-first 路径。

**修复方案**：待本地 `rewind_session` 实现后补充 fallback 路径，当前标注 TODO。

---

### T5 [提示] publishStreamCancelledFinal 的消息格式与 Python 不同

**Python 代码** (`message_handler.py:1811-1828`)：`type="event"`, `payload` 含 `"event_type": "chat.final"` 和 `"is_complete": true`。

**Go 代码** (`cancel.go:299-317`)：`type="res"`, `payload` 含 `"is_cancelled": true` 而无 `"event_type"` 和 `"is_complete"`。

**修复方案**：将 Go 对齐 Python 格式：type 改为 event，payload 中加入 `"event_type": "chat.final"` 和 `"is_complete": true`。

---

### T6 [提示] sendInterruptResultNotification 的 has_active_task 字段类型不一致

**Python 代码**：`None` 时字段不存在。

**Go 代码** (`cancel.go:224-232`)：`*bool` 为 nil 时 JSON 序列化为 `"has_active_task": null`。

**修复方案**：当 `hasActiveTask == nil` 时，不在 payload 中设置 `has_active_task` 键。

---

### T7 [提示] Go 的 ApplyChannelState 缺少 SessionMap 集成

**Python 代码** (`message_handler.py:1144-1166`)：对 feishu_enterprise 等渠道使用 SessionMap 根据 identity_tuple 解析 session_id。

**Go 代码** (`channel_state.go:84-111`)：已标注 TODO（等 11.7 回填）。

**修复方案**：待 11.7 SessionMap 回填时实现。

---

### T8 [提示] WebChannel.broadcastRaw 并发写入无串行保护

**Python 代码** (`web_connect.py:549-553`)：asyncio 单线程协作式，不存在并发问题。

**Go 代码** (`web_connect.go:446-457`)：gorilla/websocket 的 WriteMessage 不是并发安全的。

**修复方案**：为每个连接添加写锁，或在 WebChannel 层面添加广播串行化 mutex。

---

### T9 [提示] MessageHandler.forwardLoop 缺少 _KNOWN_JIUWENSWARM_SESSION_PREFIXES 的 session_id 校验

**Python 代码** (`message_handler.py:35-48`)：定义了 `sess_`, `tui_`, `acp_`, `cron_`, `feishu_` 等前缀用于 session 校验。

**Go 代码**：未找到对应的 session prefix 校验逻辑。

**修复方案**：后续回填 session prefix 校验逻辑。

---

### T10 [提示] resolveProjectDir 未解析符号链接，Python 使用 Path.resolve()

**Python 代码** (`sysop_builder.py:277`)：
```python
resolved = cand.expanduser().resolve()
```

**Go 代码** (`policy.go:316`)：
```go
resolved, err := filepath.Abs(cand)
```

**修复方案**：使用 `filepath.EvalSymlinks` 解析符号链接。

---

### T11 [提示] SandboxGatewayConfig.TimeoutSeconds 类型差异：Python 用 int，Go 用 float64

**Python 代码** (`config.py:112`)：`timeout_seconds: int = Field(default=30, ...)`

**Go 代码** (`config.go:80`)：`TimeoutSeconds float64`

**修复方案**：如果与 Python 互通的 JSON 格式需要严格对齐，改为 int。

---

### T12 [提示] dispatchShellMethod 缺少 environment/options 参数传递

**Python 代码**：execute_cmd 签名包含 `environment: Optional[Dict[str, str]] = None` 和 `options: Optional[Dict[str, Any]] = None`。

**Go 代码** (`tool_adapter.go:158-168`)：只提取了 cwd/timeout/shell_type。

**修复方案**：补充 environment 和 options 参数的提取和传递。

---

### T13 [提示] HandleHeartbeat 的 query 注入用 JSON 字符串拼接可能有注入风险

**Go 代码** (`deep_adapter.go:1116`)：直接拼接字符串到 JSON 中。

**修复方案**：使用 `json.Marshal` 构造完整 params dict。

---

### T14 [提示] ReloadAgentConfig 缺少 _agent_name 重新解析

**Python 代码** (`interface_deep.py:2688`)：
```python
self._agent_name = self._instance_overrides.get("agent_name", config.get("agent_name", "main_agent"))
```

**修复方案**：在 ReloadAgentConfig 中添加 `_agent_name` 重新解析。

---

## 四、agent_teams 额外问题 (9.55-9.56)

### AT1 [严重] TeamMemberSpec 的 model_name/prompt_hint 使用 string 而非 *string

**Python 代码** (`team.py:94-96`)：`model_name: Optional[str] = None`, `prompt_hint: Optional[str] = None`

**Go 代码** (`team.go:23-24`)：`ModelName string`, `PromptHint string`

**差异说明**：Python 的 `None` 表示"未指定"，Go 的空字符串 `""` 无法区分"未设置"和"空字符串"。`model_name=None` 在 Python 中意味着使用 per-agent model 或 router 默认，空字符串是无效值——语义不同。

**修复方案**：将 `ModelName` 和 `PromptHint` 改为 `*string` 类型，用 `nil` 表示未设置。

---

### AT2 [严重] TeamAgentSpec.ModelPool 和 ModelRouter 使用 any 类型，丢失类型安全

**Python 代码** (`blueprint.py:176, 190`)：`model_pool: list[ModelPoolEntry]`, `model_router: Optional[ModelRouterConfig]`

**Go 代码** (`blueprint.go:150-151`)：`ModelPool any`, `ModelRouter any`

**修复方案**：将 `ModelPool` 改为 `[]models.ModelPoolEntry`，`ModelRouter` 改为 `*models.ModelRouterConfig`。

---

### AT3 [严重] TeamSpec.ModelPool 使用 any 类型

同 AT2，`team.go:35` 的 `ModelPool any` 应改为 `[]models.ModelPoolEntry`。

---

### AT4 [严重] TeamRuntimeContext.MessagerConfig 和 DBConfig 使用 any 类型

**Python 代码** (`team.py:159-160`)：`messager_config: Optional[MessagerTransportConfig]`, `db_config: DatabaseConfig | MemoryDatabaseConfig`

**Go 代码** (`team.go:45-46`)：`MessagerConfig any`, `DBConfig any`

**修复方案**：改为具体类型。

---

### AT5 [一般] validateReservedNames 中 HITT human_agent 的豁免逻辑与 Python 不一致

**Python 代码** (`blueprint.py:522-532`)：按 `role_type == TeamRole.HUMAN_AGENT` 豁免。

**Go 代码** (`blueprint.go:312-322`)：按 `MemberName == "human_agent" && EnableHITT` 豁免。

**修复方案**：Go 应改为与 Python 一致——按 `m.RoleType == TeamRoleHumanAgent` 豁免。

---

### AT6 [一般] Validate() 方法不调用 validateReservedNames 和 validateHittConsistency

**Python 代码** (`blueprint.py:374-375`)：`build()` 中调用了 `_validate_reserved_names()` 和 `_validate_hitt_consistency()`。

**Go 代码** (`blueprint.go:257-263`)：`Validate()` 只调了 `validatePoolRouterExclusive()` 和 `defaultTransportForSpawnMode()`。

**修复方案**：在 `Validate()` 中加入 `validateReservedNames()` 和 `validateHittConsistency()` 调用。

---

### AT7 [一般] CreateMemberHandle 缺少 TeamName/DB/Messager 字段赋值

**Python 代码** (`member_factory.py:51-57`)：赋值了 `team_name`, `db`, `messager`。

**Go 代码** (`member_factory.go:31-36`)：缺少这三个字段。

**修复方案**：从 `infra.TeamBackend` 获取并赋值。

---

### AT8 [一般] ValidateLeaderModelResolved 错误信息过于简略

**Python 代码** (`blueprint.py:448-506`)：根据 strategy 和 leader_name 提供不同的具体错误信息和修复建议。

**Go 代码** (`blueprint.go:286`)：通用错误信息。

**修复方案**：对齐 Python 的分支逻辑，根据 `model_pool_strategy` 提供不同错误原因。

---

### AT9 [严重] TeamAgentSpec.build() 核心构建逻辑完全缺失

**Python 代码** (`blueprint.py:365-446`):
```python
def build(self) -> "TeamAgent":
    leader_agent = self.agents.get("leader")
    if leader_agent is None:
        raise ValueError("agents dict must contain a 'leader' key")
    self._validate_reserved_names()
    self._validate_hitt_consistency()
    resolved_language = resolve_language(self.language)
    # ... model_router/model_pool expansion → TeamSpec → ModelAllocator
    # ... TeamRuntimeContext → TeamAgent(card).configure(spec, context)
    return agent
```

**Go 代码** (`schema/blueprint.go:276`):
```go
func (s *TeamAgentSpec) Build() (any, error) { return nil, nil }
```

**差异说明**：Python 的 `build()` 是 TeamAgentSpec 最核心的方法，包含：(1) 校验 leader 存在；(2) 校验保留名；(3) 校验 HITT 一致性；(4) 解析语言；(5) model_router → model_pool 展开；(6) 构建 TeamSpec；(7) 构建 ModelAllocator 并分配 leader；(8) 构建 TeamRuntimeContext；(9) 创建 TeamAgent 并 configure。Go 全部缺失。

**修复方案**：实现 `Build()` 的完整逻辑（标注 ⤵️ 9.57 回填）。

---

### AT10 [严重] StorageSpec.Build() 的 db_type 映射差异 — Python 自动合并 db_type，Go 不合并

**Python 代码** (`blueprint.py:116-122`):
```python
def build(self) -> BaseModel:
    config_cls = _STORAGE_REGISTRY.get(self.type)
    merged = {"db_type": self.type, **self.params}
    return config_cls.model_validate(merged)
```

**Go 代码** (`schema/blueprint.go:228-235`):
```go
func (s StorageSpec) Build() (any, error) {
    b, ok := storageRegistry[s.Type]
    if !ok {
        return nil, fmt.Errorf("未注册的存储类型: %q", s.Type)
    }
    return b(s.Params)
}
```

**差异说明**：Python 在构建存储配置时合并了 `db_type: self.type`，即 `StorageSpec.type` 会自动设置到配置的 `db_type` 字段。Go 的 builder 函数需要手动在闭包中设置。但 Python 允许 `params` 覆盖 `db_type`（因为 `**self.params` 在后面），Go 不支持此覆盖语义。

**修复方案**：在 `Build()` 中自动合并 `db_type: s.Type` 到 params，或确保 builder 函数支持覆盖语义。

---

### G27 [一般] ResolveDBConfig 缺少 SQLite connection_string 自动填充

**Python 代码** (`blueprint.py:348-363`):
```python
def resolve_db_config(self):
    db_config = self.storage.build() if self.storage else _DatabaseConfig()
    if db_config.db_type == "sqlite" and not db_config.connection_string:
        from openjiuwen.agent_teams.paths import get_agent_teams_home
        db_config.connection_string = str(get_agent_teams_home() / "team.db")
    return db_config
```

**Go 代码** (`schema/blueprint.go:266-273`):
```go
func (s *TeamAgentSpec) ResolveDBConfig() any {
    if s.Storage != nil {
        if cfg, err := s.Storage.Build(); err == nil {
            return cfg
        }
    }
    return database.NewDatabaseConfig()
}
```

**差异说明**：Python 在 SQLite 模式下自动填充 `connection_string` 为 `get_agent_teams_home() / "team.db"`。Go 缺少此自动填充逻辑，SQLite 模式下可能使用空连接字符串。

**修复方案**：添加 SQLite connection_string 自动填充逻辑。

---

### G28 [一般] TeamAgentBlueprint 应为不可变 — Python 用 frozen=True dataclass，Go struct 可变

**Python 代码** (`blueprint.py:25`):
```python
@dataclass(frozen=True, slots=True)
class TeamAgentBlueprint:
```

**Go 代码** (`agent/blueprint.go:13`):
```go
type TeamAgentBlueprint struct {
```

**差异说明**：Python 的 TeamAgentBlueprint 是 frozen dataclass，所有字段构造后不可修改。Go 的 struct 字段可被外部直接修改，没有不可变性保证。

**修复方案**：将字段设为小写（不导出），只通过方法暴露读取，或通过文档约定不可变。

---

### G29 [一般] TeamAgent.Role() 硬编码返回 TeamRoleLeader — 应从 blueprint 获取

**Go 代码** (`agent/team_agent.go:171-174`):
```go
func (a *TeamAgent) Role() atschema.TeamRole {
    // ⤵️ 回填: 9.57 — return configurator.role
    return atschema.TeamRoleLeader
}
```

**修复方案**：在回填完成前，从 state 或其他来源读取角色。

---

### T15 [提示] NewDeepAgentSpec 默认值差异 — Go 设 CompletionTimeout: 600.0，Python 用 3600.0

**Go 代码** (`schema/blueprint.go:198-200`):
```go
func NewDeepAgentSpec() DeepAgentSpec {
    return DeepAgentSpec{MaxIterations: 15, AutoCreateWorkspace: true, CompletionTimeout: 600.0}
}
```

**差异说明**：Python 在 `create_deep_agent` 中使用 `config.get("completion_timeout", 3600.0)`。Go 默认值 600.0 与 Python 的 3600.0 不一致。

**修复方案**：确认默认值是否应该对齐，改为 3600.0。

---

### T16 [提示] TeamAgent 大量方法标注 ⤵️ 回填 — 整体为骨架代码

**差异说明**：TeamAgent 的绝大部分方法（Blueprint/Infra/Resources/Harness/Spec/Configure/Invoke/Stream/Interact/Spawn/DeliverInput/StartAgent/FollowUp/Steer/CancelAgent/ResumeInterrupt/ShutdownSelf 等）都标注为 ⤵️ 回填，实际返回 nil/false/空值。这意味着整个 TeamAgent 目前是不可用的骨架代码，需要 9.57-9.63 回填才能功能完整。

**修复方案**：标注为已知限制，待后续章节回填。

---

## 五、修复优先级建议

### P0 — 必须立即修复（影响核心功能正确性）

| 编号 | 问题 | 影响 |
|------|------|------|
| S18 | /switch 模式映射逻辑丢失 | 用户切换模式会到错误模式 |
| S19 | extractTextFromParams 遗漏 query 字段 | IM 渠道 slash 命令完全无法识别 |
| S20 | rememberUserQueryContext 缺少过滤 | 用户查询上下文被覆盖 |
| S21 | WebChannel.Send() 并发修改 msg.Payload | 并发广播数据污染 |
| S22 | WebChannel.Send() 缺少 pure-text 回退路径 | 非 dict payload 丢失内容 |
| S23 | AgentClient.Disconnect() panic 风险 | 断连时可能 panic |
| S24 | generate_isolation_key_template CUSTOM scope 不报错 | 生成无效隔离键 |
| G26 | i18n key blueput 拼写错误 | Leader 默认 persona 查找失败 |
| AT9 | TeamAgentSpec.Build() 核心构建逻辑缺失 | TeamAgent 完全无法构建 |
| AT10 | StorageSpec.Build() 不合并 db_type | 存储配置 db_type 字段缺失 |

### P1 — 尽快修复（影响功能完整性）

| 编号 | 问题 | 影响 |
|------|------|------|
| S3-S7 | ProcessInterrupt 缺少 rail/todo/auto_harness 逻辑 | cancel/supplement 行为不完整 |
| S8-S10 | 语言/配置解析逻辑差异 | 提示词语言和 filesystem rail 行为不一致 |
| S11-S12 | 模型配置同步缺失 | 模型切换后状态不一致 |
| S17 | MCP 配置路径不同 | MCP 服务器配置无法读取 |
| AT1-AT4 | agent_teams 类型安全问题 | 骨架回填时会遇到运行时错误 |

### P2 — 计划修复（功能偏差，但不阻塞）

| 编号 | 问题 | 影响 |
|------|------|------|
| S1-S2 | CodeAdapter.CreateInstance 不完整 | Code 模式无法使用 |
| S13-S16 | CreateInstance 参数缺失 | Deep 模式部分功能缺失 |
| G1 | Rail 列表和顺序不一致 | 拦截逻辑可能异常 |
| G4-G6 | 子代理配置逻辑不一致 | 子代理行为与 Python 不同 |
| G13-G14 | Cancel 错误处理不完整 | 错误信息不明确 |
| AT5-AT6 | validateReservedNames/HITT 校验未调用 | 无效配置可能通过验证 |
| G27 | ResolveDBConfig 缺少 SQLite 自动填充 | SQLite 空连接字符串 |

### P3 — 后续修复（日志/提示文本/类型对齐）

| 编号 | 问题 |
|------|------|
| T1-T16 | 日志字段、提示文本、类型差异、骨架回填等 |
| G22-G25 | Registry 幂等性、日志对齐等 |
| G28-G29 | TeamAgentBlueprint 不可变性、Role 硬编码 |
