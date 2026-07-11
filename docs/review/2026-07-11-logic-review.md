# 代码逻辑审查报告 — 2026-07-11

> 审查范围：48 小时内提交记录对应的实现计划章节
> 审查方法：逐一对标 Python 参考项目，分析方法签名、关键逻辑步骤、日志、错误处理的一致性
> 问题分类：严重（功能问题）、一般（逻辑偏差/健壮性不足）、提示（日志/格式等不影响流程）

---

## 审查章节概览

| 章节 | Python 参考路径 | 严重 | 一般 | 提示 | 状态 |
|------|----------------|------|------|------|------|
| 9.20 InterruptRail | `openjiuwen/harness/rails/interrupt/` | 1 | 5 | 4 | ✅ |
| 9.18 SysOperationRail | `openjiuwen/harness/rails/sys_operation_rail.py` | 1 | 3 | 6 | ✅ |
| 9.32/9.33 SysOperation | `openjiuwen/core/sys_operation/` | 4 | 7 | 2 | ✅ |
| 9.13/9.14 TaskPlanning/AgentMode | `openjiuwen/harness/rails/` | 0 | 2 | 8 | ✅ |
| 10.3.1 AgentServer | `jiuwenswarm/server/agent_ws_server.py` | 1 | 6 | 4 | ✅ |
| 10.3.2 UapClaw Facade | `jiuwenswarm/server/runtime/agent_adapter/interface.py` | 1 | 16 | 9 | ✅ |
| 10.3.19-20 SkillManager/SkillDev | `jiuwenswarm/server/runtime/skill/` | 0 | 8 | 9 | ✅ |
| 11.x Gateway | `jiuwenswarm/gateway/` | 5 | 9 | 5 | ✅ |
| **合计** | | **13** | **56** | **47** | |

---

## 🔴 严重问题（共 13 个）

### S1. [9.20 InterruptRail] `buildAskRequest` 未传递 `questions` 字段

**影响**：前端无法获取结构化问题数据，AskUserRail 交互体验严重降级。

**Python 样例**：
```python
def _build_ask_request(self, tool_call: Optional[ToolCall]) -> AskUserRequest:
    args = self._parse_tool_args(tool_call)
    return AskUserRequest(
        message="",
        payload_schema=AskUserPayload.to_schema(),
        questions=args.get("questions", []),
    )
```

**Go 问题**：
```go
// internal/agentcore/harness/rails/interrupt/ask_user_rail.go
func (r *AskUserRail) buildAskRequest(toolCall *llmschema.ToolCall) *saschema.InterruptRequest {
    _ = parseToolArgs(toolCall)
    // TODO: 对齐 Python AskUserRequest.questions 字段后，将 args["questions"] 传入 InterruptRequest
    return &saschema.InterruptRequest{
        Message:        "",
        PayloadSchema:  askUserPayloadSchema(),
        AutoConfirmKey: "",
        UIOptions:      nil,
    }
}
```

**修复方案**：
1. `AskUserRequest` 应嵌入 `InterruptRequest` 并扩展 `Questions` 字段
2. `buildAskRequest` 应解析 toolArgs 提取 `questions` 字段，构造 `AskUserRequest`
3. 修改 `Interrupt()` 方法支持返回 `AskUserRequest`

---

### S2. [9.18 SysOperationRail] 缺少单元测试文件

**影响**：违反项目编码规范（覆盖率 ≥ 85%），无法验证 Init/Uninit 的工具注册/注销正确性。

**Python 样例**：Python 有独立测试覆盖。

**Go 问题**：`internal/agentcore/harness/rails/sys_operation_test.go` 不存在。

**修复方案**：创建 `sys_operation_test.go`，至少覆盖：
- `NewSysOperationRail` 默认值
- `Init` 方法：验证工具列表正确（readOnly vs 非readOnly）
- `Uninit` 方法：验证工具清理
- `BeforeInvoke` / `AfterInvoke` 返回 nil

---

### S3. [9.32 SysOperation] 缺少 `_check_allowlist` 白名单检查

**影响**：Shell 命令绕过白名单校验，可能导致安全风险——用户可通过非白名单命令执行危险操作。

**Python 样例**：
```python
# shell_operation.py
if not self._check_allowlist(command):
    return _create_exec_cmd_err(error_msg="command not allowed by allowlist",
                                data=ExecuteCmdData(command=command, cwd=str(actual_cwd)))

def _check_allowlist(self, command: str) -> bool:
    if not hasattr(self._run_config, 'shell_allowlist') or not self._run_config.shell_allowlist:
        return True
    cmd_prefix = command.split()[0] if command.strip() else ""
    return any(cmd_prefix == allowed or cmd_prefix.endswith(os.sep + allowed)
               for allowed in self._run_config.shell_allowlist)
```

**Go 问题**：
```go
// internal/agentcore/sys_operation/shell_operation.go ExecuteCmd
if blocked := s.checkCommandSafety(command); blocked != "" {
    return createErrResult(...)
}
// ❌ 缺少 allowlist 检查！直接执行
```

**修复方案**：
1. 在 `ShellOperation` 上增加 `checkAllowlist(command string) bool` 方法
2. 在 `ExecuteCmd`/`ExecuteCmdStream`/`ExecuteCmdBackground` 的安全检查之后增加白名单校验
3. `ShellOperation` 需保存 `runConfig` 以便读取 `ShellAllowlist`

---

### S4. [9.32 SysOperation] `SandboxGatewayConfig` 结构严重不对齐

**影响**：沙箱模式配置丢失，sandbox 模式真正启用时会导致关键配置（auth_headers、isolation、launcher_config）无法传递。

**Python 样例**：
```python
class SandboxGatewayConfig(BaseModel):
    isolation: SandboxIsolationConfig = Field(default_factory=SandboxIsolationConfig)
    launcher_config: Optional[Union[PreDeployLauncherConfig, SandboxLauncherConfig]] = Field(default=None)
    timeout_seconds: int = Field(default=30)
    auth_headers: Dict[str, str] = Field(default_factory=dict)
    auth_query_params: Dict[str, str] = Field(default_factory=dict)
```

**Go 问题**：
```go
type SandboxGatewayConfig struct {
    GatewayURL     string  // 扁平化
    GatewayToken   string  // 扁平化
    LauncherType   string  // 扁平化
    SandboxType    string  // 扁平化
    SandboxImage   string  // 扁平化
    TimeoutSeconds float64 // 扁平化
    // ❌ 缺少 isolation, launcher_config, auth_headers, auth_query_params 等嵌套结构
}
```

**修复方案**：重构为嵌套结构，对齐 Python 的 `SandboxIsolationConfig` + `SandboxLauncherConfig` + `PreDeployLauncherConfig` + `SandboxGatewayConfig`。

---

### S5. [9.32 SysOperation] `NewShellOperation` 工厂函数丢弃 `runConfig`

**影响**：ShellOperation 实例无法访问运行配置（白名单、沙箱根目录等），与 S3 联动——即使实现了 `checkAllowlist`，也无配置可读。

**Python 样例**：
```python
class ShellOperation(BaseShellOperation):
    def __init__(self, name, mode, description, run_config):
        super().__init__(name, mode, description, run_config)
        # run_config 保存到 self._run_config
```

**Go 问题**：
```go
func NewShellOperation(runConfig any) sysop.SysSubOperation {
    op := &ShellOperation{}
    op.initPatterns()
    return op  // ❌ runConfig 完全丢弃
}
```

**修复方案**：`ShellOperation` 应保存 `runConfig` 到 `BaseShellOperation.BaseOperation`，确保实例可访问配置。

---

### S6. [9.32 SysOperation] `ExecuteCmd` 缺少 Shell 进程注册/注销

**影响**：Shell 进程无法被追踪，Ctrl+C 或 session 结束时无法清理残留子进程，导致进程泄漏。

**Python 样例**：
```python
proc = await self._create_subprocess(command, actual_cwd, exec_env, shell_type=shell_type_enum)
track_sid = _track_shell_process(proc)
try:
    invoke_data = await process_handler.invoke()
finally:
    _untrack_shell_process(track_sid, proc)
```

**Go 问题**：
```go
// internal/agentcore/sys_operation/shell_operation.go ExecuteCmd
handler := NewAsyncProcessHandler(cmd, ...)
invokeData, err := handler.Invoke(ctx)
// ❌ 缺少 trackShellProcess / untrackShellProcess 调用
```

**修复方案**：
1. 从 `AsyncProcessHandler` 暴露底层 `cmd.Process`
2. 在 `ExecuteCmd`/`ExecuteCmdStream` 中添加 `trackShellProcess`/`untrackShellProcess` 调用
3. 在 defer 中确保 untrack

---

### S7. [10.3 AgentServer] `handleCancel` 未对齐 Python 三级 fallback 策略

**影响**：中断请求可能无法正确找到目标 agent，导致取消操作静默失败。

**Python 样例**：
```python
async def _handle_cancel(self, ws, request, send_lock):
    # 1. 尝试按 params 中的 mode 查找已有 agent
    if mode_param:
        agent = self._agent_manager.get_agent_nowait(channel_id, mode=agent_mode, ...)
    else:
        agent = None
    # 2. 如果按 mode 没找到，用 get_agent_nowait 找任何已有 agent
    if agent is None:
        agent = self._agent_manager.get_agent_nowait(channel_id, project_dir=project_dir)
    # 3. 仍然没找到时 fallback 到 get_agent
    if agent is None:
        agent = await self._agent_manager.get_agent(...)
    resp = await agent.process_message(request)
```

**Go 问题**：
```go
// internal/swarm/server/handle_envelope.go
func (s *AgentServer) handleCancel(ctx context.Context, request *schema.AgentRequest) {
    s.cancelStreamTask(sessionID)
    mode, subMode := applyResolvedModeToRequest(request)
    agent := s.agentManager.GetAgentNoWait(request.ChannelID, mode, projectDir, subMode)
    if agent != nil {
        resp, err := agent.ProcessInterrupt(ctx, request)
    }
    // ❌ 找不到就直接返回 ok=true，不 fallback
}
```

**修复方案**：对齐 Python 三级 fallback 策略，最后调用 `agent.ProcessMessage` 而非 `ProcessInterrupt`。

---

### S8. [10.3.2 UapClaw] ProcessMessage 中 Skills/SkillDev 分支顺序反了

**影响**：SkillDev 请求可能被错误路由到 Skills handler，导致 SkillDev 操作（创建/编辑技能）无法正确执行。

**Python 样例**：
```python
# interface.py:801-811 (process_message)
skilldev_response = await self._handle_skilldev_request(request)
if skilldev_response is not None:
    return skilldev_response

skills_response = await self._handle_skills_request(request)
if skills_response is not None:
    return skills_response
```

**Go 问题**：
```go
// internal/swarm/server/runtime/uapclaw.go:81-87
// 5. Skills 分支（❌ 应在 SkillDev 之后）
if resp, err := uc.handleSkillsRequest(ctx, request); resp != nil {
    return resp, err
}
// 6. SkillDev 分支（❌ 应在 Skills 之前）
if resp, err := uc.handleSkillDevRequest(ctx, request); resp != nil {
    return resp, err
}
```

**修复方案**：交换步骤 5 和 6 的顺序，先判断 SkillDev，再判断 Skills。

---

### S9. [11.x Gateway] `HandleMessage` 不调用 `rememberUserQueryContext`

**影响**：用户最近一次查询上下文丢失，影响 cancel/resume 等后续操作对原始请求的关联。

**Python 样例**：
```python
def handle_message(self, msg: "Message") -> None:
    self._remember_user_query_context(msg)
    self._user_messages.put_nowait(msg)
```

**Go 问题**：
```go
// internal/swarm/gateway/message_handler/message_handler.go
func (mh *MessageHandler) HandleMessage(msg *schema.Message) {
    if msg == nil { return }
    select {
    case mh.userMessages <- msg:
        // ❌ 未调用 rememberUserQueryContext
    }
}
```

**修复方案**：在入队前调用 `mh.rememberUserQueryContext(msg.SessionID, query)`，并实现完整逻辑（`_is_chat_send_message` 检查、`session_last_user_query` 存储等）。

---

### S10. [11.x Gateway] `HandleMessage` 入队日志级别为 Debug，应为 Info

**影响**：生产环境无法追踪消息入队事件，排障困难。

**Python 样例**：
```python
logger.info(
    "[MessageHandler] _user_messages 入队: id=%s channel_id=%s session_id=%s",
    msg.id, msg.channel_id, msg.session_id,
)
```

**Go 问题**：
```go
logger.Debug(logComponent).   // ❌ 应为 Info
    Str("event_type", "handle_inbound").
    Str("msg_id", msg.ID).
    Str("session_id", msg.SessionID).
    Msg("入站消息已入队")
```

**修复方案**：将 `logger.Debug` 改为 `logger.Info`，补充 `channel_id` 字段。

---

### S11. [11.x Gateway] `forwardLoop` 缺少 chat 类请求串行保证和 hook 触发

**影响**：多个 chat 请求可能并发执行导致消息交错；before_chat_request hook 未触发导致扩展无法介入。

**Python 样例**：
```python
# _forward_loop 中
if cls._non_stream_rpc_may_run_parallel(env):
    asyncio.create_task(cls._process_non_stream_request(msg, env))
    continue
# else: chat 类请求串行执行
# 触发 _trigger_before_chat_request_hook
# 发送 _should_emit_processing_status_for_stream
```

**Go 问题**：
```go
func (mh *MessageHandler) forwardToAgent(ctx context.Context, msg *schema.Message) {
    // ❌ 无串行/并行区分，一律 go
    if msg.IsStream {
        go mh.processStream(ctx, msg, envelope)
    } else {
        go mh.processNonStreamRequest(ctx, msg, envelope)
    }
}
```

**修复方案**：
1. 补充 `_nonStreamRPCMayRunParallel` 判断，对 chat 类请求串行执行
2. 补充 `_triggerBeforeChatRequestHook` 和 `_shouldEmitProcessingStatusForStream`

---

### S12. [11.x Gateway] 入站消息缺少归一化逻辑

**影响**：`content` → `query` 映射缺失导致 Agent 无法获取用户输入；`chat.resume` → `chat.cancel` 重映射缺失导致 resume 操作无法触发。

**Python 样例**：
```python
def _normalize_gateway_message(msg):
    params = dict(msg.params or {})
    if "query" not in params and "content" in params:
        params["query"] = params["content"]
    if req_method == ReqMethod.CHAT_RESUME:
        req_method = ReqMethod.CHAT_CANCEL
        params.setdefault("intent", "resume")
    is_stream = bool(msg.is_stream or method_val in (ReqMethod.CHAT_SEND.value, ReqMethod.HISTORY_GET.value))
    return Message(..., params=params, req_method=req_method, is_stream=is_stream, ...)
```

**Go 问题**：
```go
// internal/swarm/gateway/app_gateway.go
onMessageCb := func(msg *schema.Message) {
    msgHandler.HandleMessage(msg)  // ❌ 无归一化
}
```

**修复方案**：在 `onMessageCb` 中增加 `normalizeGatewayMessage` 逻辑，或创建独立函数。

---

### S13. [11.x Gateway] 缺少 `_on_config_saved` 的 ValidationError 检查

**影响**：配置验证错误（如字段缺失）会导致配置热重载抛异常并可能触发不必要的进程重启。

**Python 样例**：
```python
if any(kw in err_str for kw in ("ValidationError", "validation error", "Field required")):
    logger.warning("[App] agent.reload_config validation error (non-fatal): %s", err_str)
    return False
raise RuntimeError(f"agent.reload_config rejected: {err_msg}")
```

**Go 问题**：
```go
if !resp.OK {
    // ❌ 任何非 OK 都视为严重错误
    return fmt.Errorf("AgentServer 拒绝配置重载: %v", errPayload)
}
```

**修复方案**：增加 ValidationError 关键词检查，匹配时仅 warn 并返回 nil（不触发 restart）。

---

## 🟡 一般问题（共 56 个）

### 9.20 InterruptRail（5 个）

#### G1. `_get_user_input` 日志级别和字段不一致
- **Python**：`logger.info` 记录 keys 列表和匹配值 repr 截断
- **Go**：`logger.Debug`，仅记录 tool_call_id
- **修复**：改为 `Info`，添加 keys 和 value 字段

#### G2. `AskUserRail.formatToolResult` 缺少后半句
- **Python**：`"You can now continue with the user's answers in mind."`
- **Go**：`"你现在可以继续。"` 缺少 "带着用户的回答"
- **修复**：改为 `"用户已回答你的问题: %s。你现在可以带着用户的回答继续。"`

#### G3. `AskUserRail.resolve_interrupt` 缺少异常兜底
- **Python**：`try/except Exception` 回退到 interrupt
- **Go**：无 recover 机制
- **修复**：添加 `defer recover()` 兜底

#### G4. `ConfirmInterruptRail.resolve_interrupt` approved 字段缺失行为不一致
- **Python**：`ConfirmPayload.approved` 为 required，缺失抛异常回退 interrupt
- **Go**：缺失 `approved` 返回 `Approved=false`，不回退
- **修复**：对缺失 `approved` 的 dict 返回 interrupt

#### G5. 缺少 `StructuredAskUserRail`（jiuwenswarm 扩展）
- **Python**：jiuwenswarm 扩展了 `StructuredAskUserRail`
- **Go**：未实现
- **修复**：后续迭代实现

---

### 9.18 SysOperationRail（3 个）

#### G6. Go 增加 Python 不存在的 permission_mode/deny_patterns/allow_patterns 选项
- **说明**：合理 Go 扩展（Python BashTool 也支持），建议注释标注差异

#### G7. Init/Uninit 日志 Python 无对应
- **说明**：Go 合理补充，注释从 "对齐 Python" 改为 "Go 补充"

#### G8. 工厂未透传 withCodeTool/readOnly 选项
- **说明**：当前与 Python 行为一致（均用默认值），后续如 Python 增加透传需同步

---

### 9.32/9.33 SysOperation（7 个）

#### G9. `_resolve_execution_plan` 非 Windows AUTO 分支缺少 `_looks_like_posix` 检测
- **修复**：在 Windows AUTO 分支添加 `LooksLikePosix` 检测路径

#### G10. pkill/killall 危险模式正则缺少 `(?!-tui)` 负向前瞻
- **修复**：匹配成功后额外检查是否以 `-tui` 结尾，若是则放行

#### G11. `ExecuteCmdStream` 缺少完整校验链（allowlist/超时上限/TUI日志）
- **修复**：补全与 `ExecuteCmd` 一致的校验链

#### G12. `ExecuteCmdStream` 环境变量未使用 `PrepareEnvironment` 合并
- **Python**：`exec_env = OperationUtils.prepare_environment(environment)`
- **Go**：`cmd.Env = os.Environ()` 忽略用户传入的 `environment`
- **修复**：使用 `opUtils.PrepareEnvironment(o.Environment)`

#### G13. `ExecuteCmdBackground` 同样缺少 allowlist + env 合并
- **修复**：同 G11/G12

#### G14. `LocalWorkConfig.shell_allowlist` 默认值不一致
- **修复**：对齐 Python 默认列表（Go 扩展的命令可保留但基础列表应一致）

#### G15. `SysOperation.__init__` sandbox 模式 `isolation_key_template` 生成未调用
- **修复**：在 sandbox 模式初始化时调用 `generateIsolationKeyTemplate`

---

### 9.13/9.14 TaskPlanning/AgentMode（2 个）

#### G16. `buildEnterPlanModeStatus` 中文路径分支多输出了 "Plan 文件："
- **Python**：agent+session 存在时中文分支不含 Plan 文件路径
- **Go**：始终包含路径
- **修复**：中文分支改为 `"enter_plan_mode 已调用完成。请继续工作流。"`

#### G17. `TaskPlanningRail.Uninit` 缺少 recover 保护
- **Python**：try/except 保护
- **Go**：无保护，而 AgentModeRail.Uninit 有 recover
- **修复**：添加 `defer recover()` 保护

---

### 10.3 AgentServer（6 个）

#### G18. `applyResolvedModeToRequest` 未对齐 Python `resolve_agent_request_mode` 的完整逻辑
- **缺失**：team 模式特殊处理（`team.plan` → `code`）、sub_mode 白名单校验、canonical_mode 回写
- **修复**：完整对齐 Python 逻辑

#### G19. `resolveRequestProjectDir` 字段名不一致
- **Python**：5 级 fallback：`project_dir → metadata.project_dir → cwd → metadata.cwd → trusted_dirs[0]`
- **Go**：仅读 `workspace_dir`
- **修复**：对齐 Python 的 5 级 fallback

#### G20. `handleUnary` 缺少 `auto_harness` → `agent` 模式转换
- **修复**：`if mode == "auto_harness" { mode = "agent" }`

#### G21. `handleStream` 缺少 `auto_harness` 模式转换
- **修复**：同 G20

#### G22. `handleCancel` 缺少 intent 判断逻辑
- **Python**：仅 `cancel`/`supplement` 时取消流式任务
- **Go**：无条件取消
- **修复**：读取 params.intent，按意图分支处理

#### G23. `handleInitialize` 未对齐完整 capabilities 返回
- **修复**：后续对齐 Python 返回完整 ACP capabilities

---

### 10.3.2 UapClaw Facade（16 个，列出关键项）

#### G24. ProcessMessage/Stream 缺少请求日志
- **修复**：添加 `logger.Info` 记录 request_id/channel_id/session_id/sdk

#### G25. ProcessMessage 中缺少 `mode`/`channel_metadata` 字段传递给 history
- **修复**：从 request params 提取 mode，将 request.Metadata 传入 AppendHistoryRecord

#### G26. `adapterModeForRequest` 逻辑与 Python 不一致
- **缺失**：`strings.TrimSpace` + `strings.ToLower`，`team.plan` → `code` 映射
- **修复**：补全 strip/lower 和 team.plan 特殊映射

#### G27. ProcessMessageStream 缺少 Team 模式原始 query 逻辑
- **修复**：实现 Team 模式判断，当 isTeamMode 时替换 inputs["query"] 为 rawQuery

#### G28. ProcessMessageStream 缺少 Team/AutoHarness 绕过 Session 队列逻辑
- **修复**：实现判断逻辑，决定是否绕过 Session 队列

#### G29. ProcessMessageStream 缺少 CancelledError 处理
- **修复**：增加 context 取消检测和错误 history 记录

#### G30. `handleSkillsRequest` 缺少 skillnet_install pending 判断
- **修复**：pending 为 true 时跳过 CreateInstance

#### G31. `handleSkillDevRequest` SkillDevService 为 nil 时应初始化而非静默忽略
- **修复**：添加懒初始化逻辑

#### G32. `GetContextUsage`/`CompressContext`/`GenerateRecap`/`SwitchMode` 为 stub
- **修复**：委托给 adapter 实现

#### G33. `ensureAdapter` 缺少 `skillnet_install_complete_hook` 设置
- **修复**：调用 `uc.skillManager.SetSkillnetInstallCompleteHook(uc.CreateInstance)`

#### G34. `CreateInstance` 缺少 dreaming 后台任务启动
- **修复**：实现 TryStartDreaming 接口调用

#### G35. `ReloadAgentConfig` 缺少 dreaming 停止/重启
- **修复**：实现 dreaming 停止/重启逻辑

#### G36. `CancelInflightWork` 缺少 `AbortOnGatewayDisconnect` 调用
- **修复**：通过接口断言或可选接口调用

#### G37. `ProcessInterrupt` 缺少 Team 模式分流和 cancel_team_work 调用
- **修复**：实现 Team 模式判断和分流逻辑

#### G38. `NewUapClaw` 中 SkillManager 缺少 workspace_dir 初始化
- **Python**：`SkillManager(workspace_dir=str(get_agent_workspace_dir()))`
- **Go**：`skill.NewSkillManager("")` 传空串
- **修复**：使用 `workspace.AgentWorkspaceDir()` 获取路径传入

#### G39. Assistant 历史记录缺少 `extra` 和 `mode` 字段
- **修复**：从 payload 提取 extra fields（排除 event_type 和 content），传入 AppendHistoryRecord

---

### 10.3.19-20 SkillManager/SkillDev（8 个，列出关键项）

#### G40. `handle_skills_get` 本地技能缺少 `is_builtin` 和 `is_builtin_source` 字段
- **修复**：在 findSkillInDir 的 else 分支中添加字段计算

#### G41. `normalizePlugin` 为空实现
- **修复**：补全规范化逻辑（确保 version/commit/installed_at 等字段有默认值）

#### G42. `GetStateFile` 使用字符串拼接而非 `filepath.Join`
- **修复**：改为 `filepath.Join(getAgentSkillsDir(), stateFileName)`

#### G43. Pipeline.Run goroutine 事件转发存在潜在竞态
- **修复**：考虑使用 `sync.Mutex` 保护 `p.events`

#### G44. `handleStart` 中 suspended 事件 `is_complete` 语义不一致
- **修复**：为所有中间事件添加 `"is_complete": false`

#### G45. `methodDispatch` 包级变量多次赋值风险
- **修复**：改为 SkillDevService 字段或 init() 初始化一次

#### G46. VALIDATE 回退 GENERATE 可能无限循环
- **修复**：增加 `validate_retries` 计数器，超阈值进入 ERROR 阶段

#### G47. `desc_optimize_stage.go` 中 `filepathJoin` 使用字符串拼接
- **修复**：改为 `filepath.Join(parts...)`

---

### 11.x Gateway（9 个，列出关键项）

#### G48. `notifyCronDeliveryError` 缺少 Timestamp 字段
- **修复**：添加 `Timestamp: schema.NowTimestamp()` 和 `EventType: schema.EventTypeChatError`

#### G49. `processStream` 缺少 cancelled final 消息
- **Python**：流式取消时补发 `chat.final`（带 `is_complete`）
- **Go**：直接 return 不发结束帧
- **修复**：在 streamCtx.Done() 分支发送 cancelled final 消息

#### G50. AgentClient `Disconnect` 中 `close(q)` 可能导致 panic
- **修复**：增加对 closed channel 的 recover 或检查

#### G51. 缺少 `_connect_with_retry` 重连逻辑
- **Python**：20 次重试（间隔 3s）
- **Go**：只尝试一次
- **修复**：增加重连循环

#### G52. 缺少 `_schedule_restart` 兜底逻辑
- **修复**：在异常分支增加进程重启调度

#### G53. `HandleServerPush` 中 cron payload 判断逻辑不一致
- **Python**：检查 `payload.event_type == "cron.response"`
- **Go**：检查 metadata/body 中的 cron 标识
- **修复**：改为检查 `payload.event_type == "cron.response"`

#### G54. 缺少 `_register_web_handlers` 的 forward_methods 过滤
- **Python**：仅转发 `_FORWARD_REQ_METHODS` 中的方法
- **Go**：所有消息无条件转发
- **修复**：增加 forward_methods 过滤

#### G55. `slash_cmd.go` 中 `sendChannelNotice` 事件类型不一致
- **Python**：`EventType.CHAT_FINAL`
- **Go**：`EventTypeChatProcessingStatus`
- **修复**：改为 `schema.EventTypeChatFinal`

#### G56. 缺少 ACP session alias 解析逻辑
- **修复**：标注 TODO，后续实现 ACP 桥接时补充

---

## 🔵 提示问题（共 47 个）

> 提示级别问题不影响核心功能流程，主要为日志字段缺失、格式差异、国际化差异、Go 惯用写法与 Python 的自然差异等。以下按章节分类汇总：

### 9.20 InterruptRail（4 个）
- ConfirmRequest 默认消息语言差异（中/英）
- 拒绝反馈格式差异（缺少换行符）
- `ApproveResult.NewArgs` 空字符串 vs Python None 语义差异
- `AskUserPayload` Schema 缺少 required 字段（低优先级）

### 9.18 SysOperationRail（6 个）
- Go Uninit 使用 recover 防御（合理）
- Python uninit hasattr vs Go nil 检查（等价）
- 9 个工具完整性确认 ✅
- Go Init 返回 nil error（符合接口约定）
- before/after_invoke async vs sync（惯用差异）
- Go 扩展 permission_mode 选项（合理增强）

### 9.32/9.33 SysOperation（2 个）
- TUI 检测 Warning 日志缺失
- `OperationRegistry` 幂等性检查逻辑不同（Go 设计选择）

### 9.13/9.14 TaskPlanning/AgentMode（8 个）
- Python `before_model_call` 同步设置 config.model_name（需确认 Go SwitchModel 内部是否已同步）
- after_tool_call 进度提醒注入方式差异（功能等价）
- init 中工具查找方式差异（名称匹配 vs isinstance，等价）
- build_plan_mode_section 调用拆分方式差异（等价）
- _build_plan_file_info Path.exists vs os.Stat（等价）
- init 工具注册确认一致 ✅
- buildAvailableAgents SpecName fallback（Go 防御性增强）
- 日志格式差异（结构化 vs 格式化字符串，等价）

### 10.3 AgentServer（4 个）
- `session.restore_files` 方法枚举已定义但 switch 无 case
- `handleHistoryGet` 缺少过滤和流式版本
- 缺少 before/after_chat_request hook 触发
- 缺少 `_bootstrap_internal_jiuwenbox` 沙箱自动启动

### 10.3.2 UapClaw Facade（9 个）
- 终止 chunk 格式需确认 NewTerminalChunk 实现
- 流式 history 缺少 mode 字段
- 非 OK 响应 history 记录行为确认
- build_inputs 缺少 interaction_context debug 日志
- ProcessMessageStream 缺少 chat.error history 记录
- context_compression_state 历史记录缺失
- TEAM_MESSAGE event_type 历史记录缺失
- ensureAdapter 缺少 _sdk_name 存储
- GetContextUsage 等 adapter nil 检查缺失

### 10.3.19-20 SkillManager/SkillDev（9 个）
- handle_skills_toggle 保存行为差异（Go 更健壮）
- state_utils 导出方式差异（等价）
- Pipeline asyncio.Queue vs channel+slice 混合模式
- SuspensionConfig NextStage/NextStageFunc 互斥缺校验
- StateStore.LoadState 返回值差异（等价）
- SkillDevContext.CreateStageAgent 返回 any（待实现，一致）
- SkillDevDeps 缺少 skill_manager 引用（设计一致）
- WorkspaceProvider.EnsureLocal 返回 string vs Path（等价）
- generateUUID 使用时间戳而非标准 UUID

### 11.x Gateway（5 个）
- ChannelManager ABC vs Go interface（设计合理）
- MessageHandler 单例模式（Go 隐式保证）
- AgentClient 首帧时序差异（等效）
- ensureConnected 用 panic 而非返回 error
- updatedKeys 类型 []string vs set[str]（缺去重）

---

## 修复优先级建议

### P0 — 立即修复（影响核心功能/安全）

| 编号 | 章节 | 问题 | 预估工时 |
|------|------|------|---------|
| S3 | 9.32 | Shell allowlist 检查缺失 | 2h |
| S5 | 9.32 | NewShellOperation 丢弃 runConfig | 1h |
| S6 | 9.32 | ExecuteCmd 缺进程注册/注销 | 2h |
| S8 | 10.3.2 | Skills/SkillDev 分支顺序反了 | 0.5h |
| S12 | 11.x | 入站消息缺归一化逻辑 | 3h |
| S11 | 11.x | forwardLoop 缺串行/hook 逻辑 | 4h |

### P1 — 本迭代修复（功能正确性）

| 编号 | 章节 | 问题 | 预估工时 |
|------|------|------|---------|
| S1 | 9.20 | buildAskRequest 缺 questions 字段 | 2h |
| S7 | 10.3 | handleCancel fallback 策略缺失 | 2h |
| S13 | 11.x | _on_config_saved ValidationError 检查 | 1h |
| S9 | 11.x | rememberUserQueryContext 缺失 | 2h |
| S10 | 11.x | 入队日志级别 | 0.5h |
| S4 | 9.32 | SandboxGatewayConfig 嵌套结构 | 3h |
| G18 | 10.3 | applyResolvedModeToRequest 不完整 | 2h |
| G19 | 10.3 | resolveRequestProjectDir fallback 链 | 1h |
| G26 | 10.3.2 | adapterModeForRequest 逻辑 | 1h |

### P2 — 下迭代修复（健壮性/完整性）

其余一般问题和提示问题，按章节逐步修复。
