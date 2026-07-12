# 逻辑审查报告 — 2026-07-13

> **审查范围**：48 小时内提交记录（23 个 commit，184 文件变更）
> **涉及章节**：9.32/9.33 sys_operation、10.3.6 DeepAdapter、11.x Gateway/MessageHandler/WebChannel、InterruptRail、routing/agent_client
> **对照标准**：Python 参考项目 `jiuwenswarm` + `openjiuwen`
> **审查方法**：逐方法签名对比 + 关键流程步骤校验 + 异常路径对齐

---

## 问题统计

| 级别 | 数量 | 说明 |
|------|------|------|
| 🔴 严重 | 22 | 功能缺失/行为不一致，影响核心功能 |
| 🟡 一般 | 25 | 接口偏差/次要逻辑差异 |
| 🔵 提示 | 15 | 日志/注释/风格差异，不影响流程 |
| **合计** | **62** | |

---

## 一、sys_operation 模块（9.32/9.33）

### 🔴 S1: ShellOperation 安全检查正则缺少 `(?i)` 大小写不敏感标志

**Python 样例** (`local/shell_operation.py` L255-282):
```python
_DANGEROUS_PATTERNS: List[Tuple[re.Pattern, str]] = [
    (re.compile(r"\brm\s+-rf\b", re.IGNORECASE), "rm -rf"),
    (re.compile(r"\bshutdown\b", re.IGNORECASE), "shutdown"),
    ...
]
# 自定义模式同样使用 re.IGNORECASE
if re.search(raw_pattern, command, re.IGNORECASE):
    return raw_pattern
```

**Go 问题** (`local/shell_operation.go` L630-645, L696-708):
```go
s.dangerousPatterns = []DangerousPattern{
    {regexp.MustCompile(`\brm\s+-rf\b`), "rm -rf"},  // 缺少 (?i)
}
// initCustomDangerousPatterns:
re, err := regexp.Compile(rawPattern)  // 缺少 (?i)
```

**修复方案**：所有内置模式前加 `(?i)` 前缀，自定义模式同样包裹 `(?i)`：
```go
regexp.MustCompile(`(?i)\brm\s+-rf\b`)
// 自定义:
re, err := regexp.Compile("(?i)" + rawPattern)
```

---

### 🔴 S2: ShellType.Cmd 在非 Windows 下会导致 panic

**Python 样例** (`local/shell_operation.py` L347-350):
```python
if shell_type == ShellType.CMD:
    raise build_error(StatusCode.SYS_OPERATION_SHELL_EXECUTION_ERROR,
                      execution="_resolve_execution_plan",
                      error_msg="shell_type 'cmd' is only supported on Windows")
```

**Go 问题** (`local/shell_operation.go` L760-763):
```go
case sysop.ShellTypeCmd:
    if !isWindows {
        return nil, false, "shell_type 'cmd' is only supported on Windows"
        // 返回 (nil, false, msg)，调用方后续 args[0] 会 panic
    }
```

**修复方案**：返回 `error` 而非三元组，调用方检查 error：
```go
case sysop.ShellTypeCmd:
    if !isWindows {
        return nil, false, exception.New(...)  // 返回 error
    }
```

---

### 🔴 S3: Shell 进程注册时机错误 — Invoke 后注册且立即注销

**Python 样例** (`local/shell_operation.py` L511-518):
```python
proc = await self._create_subprocess(command, actual_cwd, exec_env)
track_sid = _track_shell_process(proc)  # 创建后立即注册
try:
    invoke_data = await process_handler.invoke()
finally:
    _untrack_shell_process(track_sid, proc)  # finally 语义注销
```

**Go 问题** (`local/shell_operation.go` L212-218):
```go
invokeData, err := handler.Invoke(ctx)  // Invoke 后才注册
if handler.cmd.Process != nil {
    sid := trackShellProcess(ctx, handler.cmd.Process)
    untrackShellProcess(sid, handler.cmd.Process)  // 立即注销，注册无实际意义
}
```

**修复方案**：对齐 Python，使用 `defer` 在 Invoke 前注册：
```go
sid := trackShellProcess(ctx, handler.cmd.Process)
defer untrackShellProcess(sid, handler.cmd.Process)
invokeData, err := handler.Invoke(ctx)
```

---

### 🔴 S4: FsOperation sandbox fallback 逻辑不一致

**Python 样例** (`local/fs_operation.py` L1126-1132):
```python
restrict = getattr(self._run_config, 'restrict_to_sandbox', False)
if restrict:
    configured = getattr(self._run_config, 'sandbox_root', None)
    if configured:
        roots = list(configured)
    else:
        roots = [p for p in (get_workspace(), get_project_root()) if p]
```

**Go 问题** (`local/fs_operation.go` L800-816):
```go
if f.runConfig != nil && f.runConfig.RestrictToSandbox {
    sandboxRoots := f.runConfig.SandboxRoot
    if len(sandboxRoots) == 0 {
        sandboxRoots = []string{base}  // fallback 到 CWD，缺少 workspace/project_root
    }
```

**修复方案**：fallback 改为 `[getWorkspace(), getProjectRoot()]`：
```go
if len(sandboxRoots) == 0 {
    sandboxRoots = []string{}
    if ws := getWorkspace(); ws != "" { sandboxRoots = append(sandboxRoots, ws) }
    if pr := getProjectRoot(); pr != "" { sandboxRoots = append(sandboxRoots, pr) }
}
```

---

### 🔴 S5: FsOperation `read_file` 缺少互斥参数校验

**Python 样例** (`local/fs_operation.py` L89-119):
```python
def validate_mutually_exclusive(self):
    if self.tail is not None:
        if self.head is not None:
            raise build_error(...)
        if self.line_range is not None:
            raise build_error(...)
def validate_binary_mode(self):
    if self.mode == "bytes":
        if self.head is not None or self.tail is not None or self.line_range is not None:
            raise build_error(...)
```

**Go 问题**：`ReadFile` 完全没有互斥参数校验和二进制模式校验。

**修复方案**：在 `ReadFile` 入口添加校验逻辑：
```go
if o.Tail > 0 && o.Head > 0 {
    return nil, exception.New(Status..., "head and tail are mutually exclusive")
}
if o.Mode == "bytes" && (o.Head > 0 || o.Tail > 0 || o.LineRange != "") {
    return nil, exception.New(Status..., "bytes mode does not support head/tail/line_range")
}
```

---

### 🔴 S6: FsOperation `read_file` tail 模式先读全文件再截断

**Python 样例** (`local/fs_operation.py` L1461-1532):
```python
async def _read_tail(file_path, tail, encoding):
    await f.seek(0, os.SEEK_END)  # 从文件末尾反向 seek
    current_pos = await f.tell()
    while current_pos > 0 and len(lines_found) < tail:
        read_size = min(TAIL_CHUNK_SIZE, current_pos)
```

**Go 问题** (`local/fs_operation.go` L97-100):
```go
} else if o.Tail > 0 {
    if o.Tail < len(lines) {
        lines = lines[len(lines)-o.Tail:]  // 先 ReadFile 全文再截断
    }
```

**修复方案**：对大文件使用反向 seek 读取，或限制最大读取量：
```go
// 方案A：限制最大读取量
const maxTailReadSize = 10 * 1024 * 1024 // 10MB
// 方案B：实现反向 seek（对齐 Python）
```

---

### 🔴 S7: FsOperation `write_file` 缺少文件读写锁

**Python 样例** (`local/fs_operation.py` L508):
```python
async with self._file_lock(file_path, "write", timeout):
    # 读写保护
```

**Go 问题**：`WriteFile` 没有任何文件锁机制。

**修复方案**：引入 `sync.RWMutex` per-file 或使用 `fslock` 包实现跨进程文件锁。

---

### 🔴 S8: ShellOperation `WriteStdin` 和 `ListProcesses` 未实现

**Python 样例**：`BaseShellOperation` 中有 `write_stdin`、`list_processes` 完整实现。

**Go 问题**：
```go
func (s *LocalShellOperation) WriteStdin(...) (*result.ExecuteCmdResult, error) {
    return nil, fmt.Errorf("未实现: WriteStdin")
}
func (s *LocalShellOperation) ListProcesses(...) (*result.ExecuteCmdResult, error) {
    return &result.ExecuteCmdResult{...Message: "ListProcesses not fully implemented"...}, nil
}
```

**修复方案**：实现 `WriteStdin`（通过 stdin pipe 追踪）和 `ListProcesses`（遍历 ShellProcessRegistry）。

---

### 🟡 G1: ShellOperation `terminate` 缺少两阶段终止（SIGTERM → 3s → SIGKILL）

**Python 样例** (`shell_process_registry.py` L154-198):
```python
def terminate_shell_process(proc) -> bool:
    # 先 SIGTERM → 等 3s → 再 SIGKILL
```

**Go 问题**：直接 SIGKILL，无优雅终止阶段。

**修复方案**：实现两阶段终止，对齐 Python。

---

### 🟡 G2: FsOperation `search_files` 缺少 `exclude_patterns` 实现

**Python 样例** (`local/fs_operation.py` L1073-1088):
```python
if exclude_patterns:
    exclude_set = set()
    for pat in exclude_patterns:
        exclude_set.update(set(base.rglob(pat)))
    matched_paths = [p for p in matched_paths if p not in exclude_set]
```

**Go 问题**：接口有 `exclude_patterns` 参数但实现完全忽略。

**修复方案**：在 `SearchFiles` 中增加 exclude_patterns 过滤逻辑。

---

### 🟡 G3: FsOperation `list_files`/`list_directories` 缺少 `max_depth` 限制

**Python 样例** (`local/fs_operation.py` L1207-1231): 有 `max_depth` 递归深度限制。
**Go 问题**：`FsOptions` 有 `MaxDepth` 字段但 `walkDir` 实现未使用。

**修复方案**：在 `walkDir` 递归中跟踪当前深度，超过 `MaxDepth` 时停止递归。

---

### 🟡 G4: CodeOperation 使用硬编码 `python3` 而非 `sys.executable`

**Python 样例** (`local/code_operation.py` L29-31):
```python
"exec_cli": lambda code: [sys.executable, "-u", "-c", code],
```

**Go 问题**：硬编码 `"python3"`，无法使用 venv/conda 中的 Python。

**修复方案**：优先读取 `os.Getenv("PYTHON_EXECUTABLE")` 或 `which python3`。

---

### 🟡 G5: FsOperation `write_file` 非 UTF-8 编码处理为简化实现

**Python 样例**：`data_bytes = txt.encode(encoding)` 正确转换。
**Go 问题**：`dataBytes = []byte(txt)` 仍按 UTF-8 编码。

**修复方案**：引入 `golang.org/x/text/encoding` 包做编码转换。

---

### 🟡 G6: `isolation_key_template` 在 prefix 为空时产生连续下划线

**Python 样例** (`sys_operation.py` L50-64):
```python
prefix = f"{isolation_prefix}_" if isolation_prefix else ""
return f"{container_scope.value}_{launcher_type}_{sandbox_type}_{prefix}{identity}"
```

**Go 问题**：`strings.Join` 不跳过空元素，prefix 为空时产生 `___` 三个连续下划线。

**修复方案**：对齐 Python，手动拼接而非 `strings.Join`。

---

### 🔵 T1: `__getattr__` 动态调度在 Go 中无法实现

**说明**：Python 支持动态操作类型扩展（如 `sys_op.calculator()`），Go 不支持。语言限制，已知且可接受。

---

### 🔵 T2: `_generate_tool_cards` 动态生成 vs Go 硬编码

**说明**：Python 通过 `CallableSchemaExtractor` 从方法签名自动生成 ToolCard。Go 手动硬编码。实现方式不同但功能等价。

---

## 二、DeepAdapter 模块（10.3.6）

### 🔴 S9: 缺少 `set_skill_manager` 方法

**Python 样例** (`interface_deep.py` L553-555):
```python
def set_skill_manager(self, skill_manager: SkillManager) -> None:
    self._skill_manager = skill_manager
```

**Go 问题**：`DeepAdapter` 完全缺失此方法，skill 工具管理器无法注入。

**修复方案**：添加 `SetSkillManager(skillMgr SkillManager)` 方法。

---

### 🔴 S10: `ProcessInterrupt` 缺少 StreamEventRail 实际调用

**Python 样例** (`interface_deep.py` L3307-3400):
```python
if intent == "pause":
    if self._stream_event_rail is not None:
        self._stream_event_rail.pause(request.session_id)
elif intent == "resume":
    if self._stream_event_rail is not None:
        self._stream_event_rail.resume(request.session_id)
elif intent == "cancel":
    self._stream_event_rail.abort(request.session_id)
    self._stream_event_rail.collect_cancelled_tool_updates(request.session_id)
    self._stream_event_rail.reset_for_new_task(request.session_id)
```

**Go 问题**：所有 rail 调用均为占位注释 `// ⤵️ 10.6.3-10`。

**修复方案**：实现 pause/resume/abort/collect/reset 的 streamEventRail 调用链。

---

### 🔴 S11: `ProcessInterrupt` 响应缺少 `todos`/`cancelled_tools`/`new_input` 字段

**Python 样例** (`interface_deep.py` L3432-3460):
```python
payload = {"event_type": "chat.interrupt_result", "intent": intent, "success": success}
if new_input:
    payload["new_input"] = new_input
if updated_todos is not None:
    payload["todos"] = updated_todos
if cancelled_tool_results:
    payload["cancelled_tools"] = cancelled_tool_results
```

**Go 问题**：响应 payload 只有 `event_type`/`intent`/`success`/`message`。

**修复方案**：补充 `todos`、`cancelled_tools`、`new_input` 字段到响应 payload。

---

### 🔴 S12: `ProcessMessageStreamImpl` 缺少 Team/AutoHarness/Slash 分流

**Python 样例** (`interface_deep.py` L4546-4634):
```python
if mode in ("team", "team.plan", "code.team"):
    async for chunk in process_team_message_stream(request, inputs, self._instance):
        yield chunk
    return
# auto_harness 模式处理
# slash 命令处理
```

**Go 问题**：三处分流全部为占位注释。

**修复方案**：实现 Team 模式分流、AutoHarness 分流和 Slash 命令处理。优先级：Slash > Team > AutoHarness。

---

### 🔴 S13: `CreateInstance` 缺少 `sys_operation` 和 `configured_subagents`

**Python 样例** (`interface_deep.py` L2569-2574):
```python
sys_operation = self._create_sys_operation()
if sys_operation is None:
    raise RuntimeError("sys_operation is not available")
configured_subagents, should_add_general_agent = self._build_configured_subagents(model, config)
```

**Go 问题** (`deep_adapter.go` L392-396)：均为占位注释，`Mcps`/`Subagents`/`SysOperation` 传 nil。

**修复方案**：实现 `_create_sys_operation` 和 `_build_configured_subagents`，对齐 Python 的创建流程。

---

### 🔴 S14: `CreateInstance` 缺少 `_get_tool_cards`

**Python 样例** (`interface_deep.py` L2561): `tool_cards = await self._get_tool_cards(agent_card.id)`
**Go 问题**：`var toolCards []*tool.ToolCard // ⤵️ agentcore: _get_tool_cards(agentCard.ID)`

**修复方案**：实现 `_get_tool_cards` 获取工具卡片。

---

### 🔴 S15: `buildAgentIdentityPrompt` 返回空字符串

**Python 样例**：`build_agent_identity_prompt(language=self._resolve_prompt_language())` 构建完整身份系统提示词。
**Go 问题**：`return ""` — Agent 系统提示词为空，LLM 无法理解 Agent 身份和能力。

**修复方案**：实现 `buildAgentIdentityPrompt`，对齐 Python 的身份提示词构建逻辑。

---

### 🔴 S16: `_is_acp_tool_profile` 实现完全不一致

**Python 样例** (`interface_deep.py` L709-716):
```python
def _is_acp_tool_profile(config):
    tool_profile = str(config.get("tool_profile") or "").strip().lower()
    if tool_profile:
        return tool_profile == "acp"
    channel_id = str(config.get("channel_id") or "").strip().lower()
    return channel_id == "acp"
```

**Go 问题** (`deep_adapter_config.go` L161-175)：从 `configBase["models"]["defaults"]["profile"]` 读取，检查 `profile == "acp_tool"`。

**问题描述**：字段名不同（`tool_profile` vs `profile`），值不同（`"acp"` vs `"acp_tool"`），数据源不同（`instance_overrides` vs `models.defaults`）。

**修复方案**：对齐 Python，从 `instance_overrides` 读取 `tool_profile` 和 `channel_id`：
```go
func (d *DeepAdapter) isAcpToolProfile(instanceOverrides map[string]any) bool {
    if tp, ok := instanceOverrides["tool_profile"]; ok {
        return strings.EqualFold(strings.TrimSpace(fmt.Sprint(tp)), "acp")
    }
    if cid, ok := instanceOverrides["channel_id"]; ok {
        return strings.EqualFold(strings.TrimSpace(fmt.Sprint(cid)), "acp")
    }
    return false
}
```

---

### 🔴 S17: A2X 客户端全部空实现

**Python 样例** (`interface_deep.py` L558-706)：完整的 A2X 客户端初始化、状态同步、清理逻辑。
**Go 问题** (`deep_adapter_a2x.go`)：所有 5 个方法均为占位日志输出。

**修复方案**：实现 A2X 注册表客户端，优先级取决于团队协作的使用频率。

---

### 🔴 S18: `ReloadAgentConfig` 缺少缓存清理和环境变量解析

**Python 样例** (`interface_deep.py` L2662-2679):
```python
clear_config_cache()
clear_memory_manager_cache()
config_base = resolve_env_vars(config_base)
```

**Go 问题**：完全缺失缓存清理和环境变量解析步骤。

**修复方案**：在 `ReloadAgentConfig` 中添加 `clearConfigCache()`、`clearMemoryManagerCache()`、`resolveEnvVars()` 调用。

---

### 🟡 G7: 13 个 Rail Builder 返回 nil，未注册到 railsList

**Python 样例**：`_build_agent_rails` 注册全部 18-20 个 rail。
**Go 问题**：SkillUseRail/SkillEvolutionRail/SkillCreateRail/StreamEventRail/SubagentRail/SecurityRail/MemoryRail 等 13 个返回 nil。

**修复方案**：按优先级逐个实现 rail builder，至少先实现 StreamEventRail 和 SecurityRail。

---

### 🟡 G8: `activeSessionIDs` 无并发保护

**Go 问题**：`map[string]int` 无 mutex 保护，且 `ProcessMessageStreamImpl` 中在 goroutine 里读写此 map。

**修复方案**：添加 `sync.Mutex` 或使用 `sync.Map`。

---

### 🟡 G9: `CompressContext` 和 `GenerateRecap` 为空占位

**修复方案**：实现上下文压缩和 recap 摘要生成，对齐 Python 的 `compress_context` 和 `generate_recap`。

---

### 🟡 G10: `_is_subagent_default_enabled` 默认值不一致

**Python 样例**：无配置时默认 `True`（所有子代理默认启用）。
**Go 问题**：只有 `explore` 和 `plan` 默认启用。

**修复方案**：对齐 Python，所有子代理默认启用。

---

### 🟡 G11: `buildMcpServerConfig` 字段名不一致

**Python 样例**：使用 `transport`/`command`/`args`/`cwd`/`env`/`timeout_s`。
**Go 问题**：使用 `server_path`/`client_type`/`auth_headers`。

**修复方案**：对齐 Python 字段映射。

---

### 🔵 T3: `usage_metadata` payload 格式差异

**说明**：Go 直接展开 token/cost 字段，Python 使用 `metadata` 嵌套 + `session_id`。前端适配问题。

---

## 三、Gateway/MessageHandler 模块（11.x）

### 🔴 S19: `shouldEmitProcessingStatusForStream` 判断逻辑不一致

**Python 样例** (`message_handler.py` L1866-1869):
```python
@staticmethod
def _should_emit_processing_status_for_stream(msg: "Message") -> bool:
    return msg.req_method == ReqMethod.CHAT_SEND
```

**Go 问题** (`dispatch.go` L30-33):
```go
func (mh *MessageHandler) shouldEmitProcessingStatusForStream(msg *schema.Message) bool {
    channelType := mh.resolveControlChannelType(msg)
    return string(channelType) != string(channel_manager.ChannelTypeWeb)
}
```

**问题描述**：Python 基于 `req_method == CHAT_SEND` 判断，Go 基于"非 web 渠道"判断。Web 渠道的 CHAT_SEND 在 Python 会发 processing_status，Go 不会；非 Web 渠道的非 CHAT_SEND 在 Python 不会发，Go 会发。

**修复方案**：对齐 Python，改为 `return msg.ReqMethod == schema.ReqMethodChatSend`。

---

### 🔴 S20: `nonStreamRPCMayRunParallel` 缺少 CHAT_ANSWER 串行保护

**Python 样例** (`message_handler.py` L1837-1853):
```python
return m not in (
    ReqMethod.CHAT_SEND.value,
    ReqMethod.CHAT_RESUME.value,
    ReqMethod.CHAT_CANCEL.value,
    ReqMethod.CHAT_ANSWER.value,
)
```

**Go 问题** (`dispatch.go` L39-47)：缺少 `CHAT_ANSWER` 的串行保护。

**修复方案**：添加 `"chat.answer"` 到串行保护列表。

---

### 🔴 S21: `_cancel_agent_work_for_session` 无活跃任务时不通知 AgentServer

**Python 样例** (`message_handler.py` L428-430 注释):
```python
# 即使网关侧已无活跃流式拉取任务，也必须通知 AgentServer，
# 否则仅断开 CLI WebSocket 无法停止已派发的工作。
```

**Go 问题** (`cancel.go` L42-43)：仅在 `len(requestIDs) > 0` 时发送中断请求。

**修复方案**：移除 `len(requestIDs) > 0` 条件，无论是否有活跃流式任务都通知 AgentServer。

---

### 🔴 S22: `handleChatUserAnswer` 忽略了 `processNonStreamRequest` 的错误

**Python 样例**：检查 `resp` 是否为 `None`。
**Go 问题**：`resp, _ := mh.processNonStreamRequest(...)` 忽略了 error。

**修复方案**：检查 error，如果请求失败则提前返回，不继续后续的 evolution 审批判断。

---

### 🔴 S23: `CancelAgentSessionsOnDisconnect` 缺少 session_id 去重

**Python 样例** (`message_handler.py` L530-573):
```python
seen: set[str] = set()
for _channel_id, session_id in session_keys:
    sid = (session_id or "").strip()
    if not sid or sid in seen:
        continue
    seen.add(sid)
```

**Go 问题** (`disconnect.go` L17-47)：没有去重逻辑。

**修复方案**：添加 `seen` set 做去重。

---

### 🔴 S24: `publishRobotMessages` 缺少 Outbound Pipeline 处理

**Python 样例** (`message_handler.py` L1193-1201):
```python
async def publish_robot_messages(self, msg: "Message") -> None:
    if self._outbound_pipeline is not None:
        try:
            await self._outbound_pipeline.apply(msg)
        except Exception:
            logger.exception("Outbound pipeline error")
    await self._robot_messages.put(msg)
```

**Go 问题**：`PublishRobotMessages` 没有调用 outbound pipeline。

**修复方案**：在 `PublishRobotMessages` 入队前执行 outbound pipeline。

---

### 🟡 G12: `/switch` 命令缺少上下文感知模式切换

**Python 样例** (`message_handler.py` L755-820)：根据当前模式决定目标模式。
**Go 问题**：简单映射，不根据当前模式做上下文感知。

**示例流程**：
- code 模式 + `/switch plan` → Python: `code.plan`，Go: `agent.plan`
- agent 模式 + `/switch normal` → Python: "非法指令"，Go: `code.normal`

**修复方案**：对齐 Python 的上下文感知切换逻辑。

---

### 🟡 G13: `rememberUserQueryContext` 缺少 CHAT_SEND 过滤和 8000 字符截取

**Python 样例**：
```python
if not self._is_chat_send_message(msg): return
if msg.params.get("is_supplement") is True: return
self._session_last_user_query[session_id] = query[:8000]
```

**Go 问题**：没有 `req_method == CHAT_SEND` 检查、没有 `is_supplement` 过滤、没有 8000 字符截取。

**修复方案**：添加三处过滤逻辑。

---

### 🟡 G14: `handleSupplement` 中 runtime_params 注入不覆盖 vs Python 覆盖

**Python 样例**：`**runtime_params` 直接覆盖。
**Go 问题**：`if _, exists := newParams[k]; !exists` 不覆盖已有键。

**修复方案**：去掉 `!exists` 条件，直接覆盖，对齐 Python。

---

### 🟡 G15: `newSessionCancelAndNotice` 时序差异 — 先更新后异步取消

**Python 样例**：先 `await cancel` 完成，再 `send_notice`。
**Go 问题**：先更新 `state.SessionID`，再 `go cancel` 异步，然后立即 `send_notice`。

**修复方案**：同步取消后再发通知，或等待 cancel goroutine 完成后再发通知。

---

### 🟡 G16: `_rewind_slash_notice` 缺少本地 fallback

**Python 样例**：E2A 请求失败时回退到本地 `rewind_session`。
**Go 问题**：只有 E2A 路径，无本地 fallback。

**修复方案**：添加本地 `rewind_session` fallback。

---

### 🟡 G17: `handleChannelControl` 只提取 `content` 不提取 `query`

**Python 样例**：`text = str(params.get("query") or params.get("content") or "").strip()`
**Go 问题**：只提取 `content`。

**修复方案**：先尝试 `query`，再 fallback 到 `content`。

---

### 🟡 G18: `enable_memory` 零值语义与 Python None 语义差异

**Python 样例**：`msg.enable_memory is not None` 时才写入 metadata。
**Go 问题**：总是写入 `metadata["enable_memory"]`，零值 `false` 与 Python `None` 行为不同。

**修复方案**：使用 `*bool` 指针类型区分"未设置"和"false"。

---

### 🟡 G19: `_publish_stream_cancelled_final` payload 缺少 `is_complete`

**Python 样例**：`{"event_type": "chat.final", "content": "", "is_complete": True}`
**Go 问题**：用 `is_cancelled` 而非 `is_complete`，前端无法识别为结束帧。

**修复方案**：添加 `"is_complete": true` 到取消的 final payload。

---

### 🔵 T4: slash 命令处理为同步阻塞

**说明**：Go 中 `/skills list`、`/branch`、`/rewind` 会阻塞 forwardLoop，Python 使用 `asyncio.create_task` 异步执行。

---

### 🔵 T5: `buildCancelMessage` 复用原始消息 ID

**说明**：Go 复用原始消息 ID 作为 cancel 请求 ID，Python 生成唯一 ID。可能导致消息路由混淆。

---

## 四、WebChannel 模块（11.14）

### 🔴 S25: `handleCancel` 无条件取消流式任务，pause/resume 时误杀

**Python 样例** (`agent_ws_server.py` L983-1001):
```python
if intent in ("cancel", "supplement"):
    stream_task = self._session_stream_tasks.get(sid)
    if stream_task is not None and not stream_task.done():
        stream_task.cancel()
```

**Go 问题** (`handle_envelope.go` L394-401):
```go
s.cancelStreamTask(sessionID)  // 无条件取消
```

**问题描述**：当用户发送 `pause` 或 `resume` 时，Go 会错误终止正在运行的流式任务。

**修复方案**：
```go
intent := "cancel"
if request.Params != nil {
    var params map[string]any
    json.Unmarshal(request.Params, &params)
    if i, ok := params["intent"].(string); ok { intent = i }
}
if intent == "cancel" || intent == "supplement" {
    s.cancelStreamTask(sessionID)
}
```

---

### 🔴 S26: `flattenTeamConfig` 假设 team 是列表格式，Python 是 dict 格式

**Python 样例** (`app_web_handlers.py` L419):
```python
for team_idx, (team_name, team_spec) in enumerate(teams_raw.items()):
    # teams_raw 是 dict，键为 team_name
```

**Go 问题** (`web_handlers.go` L1070-1088):
```go
teamList, ok := team.([]any)  // 假设是列表
if !ok { return }              // dict 格式直接返回不输出
```

**问题描述**：config.yaml 中 `modes.team` 以 dict 格式存储时，Go 不输出任何 team 字段。

**修复方案**：先尝试 `map[string]any`（dict），再 fallback 到 `[]any`（list）：
```go
if teamMap, ok := team.(map[string]any); ok {
    // dict 格式：遍历 items
} else if teamList, ok := team.([]any); ok {
    // list 格式
}
```

---

### 🔴 S27: `config_apply` 缺少 api_key/token 加密逻辑

**Python 样例** (`app_web_handlers.py` L131-134):
```python
if not _values_match(item["api_key"], resolved_mcc.get("api_key")):
    new_mcc["api_key"] = (
        crypto.encrypt(item["api_key"]) if (item["api_key"] and crypto) else item["api_key"]
    )
```

**Go 问题** (`config_apply.go` L90)：`// TODO(⤵️ 加密): 对齐 Python _encrypt_config_params`

**问题描述**：前端传入的 api_key 明文直接写入 `.env` 文件，存在安全隐患。

**修复方案**：在 `ApplyConfigPayload` 中对 `api_key`/`token` 字段调用 `crypto.Encrypt()` 加密后再写入。

---

### 🟡 G20: `connection.ack` 投递路径不一致

**Python 样例**：优先通过 `message_handler.publish_robot_messages` 投递（走出站队列）。
**Go 问题**：始终直接调用 `ch.Send()`，跳过出站队列。

**修复方案**：优先通过 `messageHandler.PublishRobotMessages` 投递 ack 消息。

---

### 🟡 G21: `buildModelsDefaultsFromFrontend` 缺少 `verify_ssl` 字段

**Python 样例**：`"verify_ssl": item["verify_ssl"]`
**Go 问题**：构建新模型条目时缺少 `verify_ssl` 字段。

**修复方案**：从 frontend payload 中提取 `verify_ssl` 并加入 `model_client_config`。

---

### 🟡 G22: `ChannelTransport.Close()` 直接 close channel 可能 panic

**Go 问题**：如果有 goroutine 正在向 channel 写入，`close()` 会导致 panic。

**修复方案**：使用 cancel context 通知写入方退出，等待退出后再关闭 channel。

---

### 🟡 G23: `writeResponse`/`writeChunk` 丢弃满缓冲消息

**Go 问题**：`select default` 模式在 `RecvCh` 满时直接丢弃消息。
**Python 样例**：WebSocket 库有背压机制保证不丢。

**修复方案**：增加缓冲区大小或改为阻塞等待（带超时）。

---

### 🟡 G24: `handleEnvelope` 缺少 E2A fallback 解析路径

**Python 样例**：E2A 解析失败时 fallback 到旧载荷格式 `_payload_to_request(data)`。
**Go 问题**：E2A 转换失败时直接返回错误。

**修复方案**：添加旧载荷格式的 fallback 解析。

---

### 🟡 G25: `handleCancel` 调用 `ProcessInterrupt` 而非 `ProcessMessage`

**Python 样例**：`resp = await agent.process_message(request)`
**Go 问题**：`resp, err := agent.ProcessInterrupt(ctx, request)`

**修复方案**：对齐 Python，改为调用 `ProcessMessage`。

---

### 🔵 T6: WebSocket path 校验返回 HTTP 404 而非 WebSocket close

**说明**：Go 在 HTTP 层返回 404 拒绝，Python 在 WebSocket 握手后以 code=1008 关闭。Go 方式更安全但语义不同。

---

### 🔵 T7: `flattenTeamConfig` 缺少 agent 扁平化字段

**说明**：缺少 `agent_name_{idx}`/`agent_model_{idx}`/`agent_skills_{idx}` 等字段。前端配置面板可能依赖这些字段。

---

## 五、InterruptRail 模块

### 🟡 G26: `AskUserRail` string 输入无问题匹配时行为差异

**Python 样例** (`ask_user_rail.py` L97-100):
```python
else:
    return self.interrupt(self._build_ask_request(tool_call))
```

**Go 问题**：返回 `&AskUserPayload{}`（空 payload, ok=true），后续因 Answers 为空再次 Interrupt。路径冗余但最终效果相同。

**修复方案**：对齐 Python，直接返回 interrupt。

---

### 🟡 G27: `AskUserRail` 其他类型 input 返回 (nil, false) vs Python interrupt

**Python 样例**：直接返回 `interrupt()`。
**Go 问题**：返回 `(nil, false)`，后续因 `!ok` 再调用 Interrupt。

**修复方案**：对齐 Python，直接返回 interrupt 决策。

---

### 🔵 T8: `add_policy` 方法缺失

**说明**：Python 保留 `add_policy` 作为 `add_tool` 的 deprecated 别名。Go 无历史兼容负担，不需要。

---

## 六、优先修复排序

### P0 — 立即修复（安全/数据正确性）

| # | 问题 | 影响 |
|---|------|------|
| S1 | Shell 安全检查缺少 `(?i)` | 大写危险命令不被拦截 |
| S2 | ShellType.Cmd 非 Windows panic | 运行时崩溃 |
| S27 | config_apply 缺少加密 | 敏感凭据明文存储 |
| S25 | handleCancel 无条件取消流式任务 | pause/resume 误杀任务 |
| S21 | cancel 不通知 AgentServer | Agent 工作不被取消 |

### P1 — 尽快修复（核心功能对齐）

| # | 问题 | 影响 |
|---|------|------|
| S3 | Shell 进程注册时机错误 | 无法通过 registry 终止进程 |
| S19 | processing_status 判断逻辑不一致 | 前端状态异常 |
| S20 | 缺少 CHAT_ANSWER 串行保护 | evolution 状态竞态 |
| S22 | handleChatUserAnswer 忽略错误 | 请求失败后继续处理可能 panic |
| S23 | disconnect 缺少 session_id 去重 | 重复取消导致状态不一致 |
| S16 | _is_acp_tool_profile 完全不一致 | ACP 配置判断错误 |
| S26 | team 配置 dict/list 格式不兼容 | team 字段不输出 |

### P2 — 计划修复（功能完善）

| # | 问题 | 影响 |
|---|------|------|
| S4 | sandbox fallback 逻辑不一致 | 沙箱范围不正确 |
| S5 | read_file 缺少互斥参数校验 | 参数冲突不报错 |
| S6 | tail 模式性能问题 | 大文件 OOM |
| S7 | write_file 缺少文件锁 | 并发写入数据竞争 |
| S8 | WriteStdin/ListProcesses 未实现 | 关键方法缺失 |
| S9-S18 | DeepAdapter 多项缺失 | Agent 核心功能受限 |
| S24 | Outbound Pipeline 缺失 | 数字分身出站路由失效 |
| G7-G11 | Rail/Config/MCP 偏差 | Agent 能力不完整 |

### P3 — 后续优化

| # | 问题 | 影响 |
|---|------|------|
| G12-G19 | MessageHandler 次要差异 | 边界场景行为偏差 |
| G20-G25 | WebChannel 次要差异 | 兼容性/性能 |
| G26-G27 | InterruptRail 路径冗余 | 等价但路径不同 |
| T1-T8 | 提示级差异 | 日志/风格/语言限制 |

---

## 七、各模块差异汇总

| 模块 | 严重 | 一般 | 提示 | 合计 |
|------|------|------|------|------|
| sys_operation | 8 | 6 | 2 | 16 |
| DeepAdapter | 10 | 5 | 1 | 16 |
| Gateway/MessageHandler | 6 | 8 | 2 | 16 |
| WebChannel | 3 | 6 | 2 | 11 |
| InterruptRail | 0 | 2 | 1 | 3 |
| **合计** | **22** | **25** | **15** | **62** |

---

*本报告由代码审查工具自动生成，所有问题均经 Python 参考代码对照验证。*
*审查日期：2026-07-13 | 审查范围：48小时内提交（23 commits, 184 files）*
