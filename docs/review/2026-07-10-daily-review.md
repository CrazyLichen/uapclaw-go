# 代码逻辑审查报告 — 2026-07-10

> 审查范围：近 48 小时提交的代码，覆盖实现计划章节 9.1/9.14/9.18/9.32-9.33/10.3.1-10.3.2/10.3.19-20/11.x
>
> 审查方法：逐方法对比 Python 参考项目的方法签名和步骤，识别 Go 移植过程中丢失或不一致的逻辑

---

## 一、严重问题（功能缺陷，影响正确性）

### S-01: DeepAgent ensureInitialized 中 CWD 初始化缺少 os.getcwd() 兜底

| 项目 | 内容 |
|------|------|
| 章节 | 9.1 DeepAgent |
| Go 文件 | `internal/agentcore/harness/deep_agent.go:1572` |
| Python 参考 | `openjiuwen/harness/deep_agent.py:824` |

**Python 样例：**
```python
init_root = self._deep_config.workspace.root_path or os.getcwd()
init_cwd(init_root, workspace=self._deep_config.workspace.root_path)
```

**Go 问题：**
```go
if cfg != nil && cfg.Workspace != nil && cfg.Workspace.RootPath != "" {
    initRoot := cfg.Workspace.RootPath
    cwdState := cwd.InitCwd(initRoot, cwd.WithWorkspace(initRoot))
    ctx = cwd.WithCwdState(ctx, cwdState)
}
```
当 `RootPath` 为空字符串时，Go 不做任何 CWD 初始化，而 Python 用 `os.getcwd()` 兜底。后续 Agent 执行时 CWD 可能指向错误目录。

**修复方案：**
```go
initRoot := ""
if cfg != nil && cfg.Workspace != nil && cfg.Workspace.RootPath != "" {
    initRoot = cfg.Workspace.RootPath
}
if initRoot == "" {
    if wd, err := os.Getwd(); err == nil {
        initRoot = wd
    }
}
if initRoot != "" {
    cwdState := cwd.InitCwd(initRoot, cwd.WithWorkspace(initRoot))
    ctx = cwd.WithCwdState(ctx, cwdState)
}
```

---

### S-02: DeepAgent CreateSubagent 中 workspace 为 nil 时缺少兜底 Workspace

| 项目 | 内容 |
|------|------|
| 章节 | 9.1 DeepAgent |
| Go 文件 | `internal/agentcore/harness/deep_agent.go:2398-2404` |
| Python 参考 | `openjiuwen/harness/deep_agent.py:922-936` |

**Python 样例：**
```python
if not self._deep_config.workspace or isinstance(self._deep_config.workspace, str):
    workspace_path = (
        f"{self._deep_config.workspace}/{subsession_id}"
        if self._deep_config.workspace
        else f"./{subsession_id}"
    )
    workspace = Workspace(root_path=workspace_path, language=self._deep_config.language)
else:
    workspace = Workspace(
        root_path=Path(self._deep_config.workspace.root_path) / subsession_id,
        language=self._deep_config.language
    )
```

**Go 问题：** `cfg.Workspace == nil` 时 `ws` 为 nil，子 Agent 没有工作空间。Python 在此场景下创建 `Workspace(root_path=f"./{subsession_id}")`。

**修复方案：**
```go
var ws *workspace.Workspace
if cfg.Workspace != nil {
    ws = &workspace.Workspace{
        RootPath: filepath.Join(cfg.Workspace.RootPath, subSessionID),
        Language: cfg.Language,
    }
} else {
    ws = &workspace.Workspace{
        RootPath: filepath.Join(".", subSessionID),
        Language: cfg.Language,
    }
}
```

---

### S-03: DeepAgent hotReloadModel 缺少同步直接字段(api_base/api_key/model_provider)

| 项目 | 内容 |
|------|------|
| 章节 | 9.1 DeepAgent |
| Go 文件 | `internal/agentcore/harness/deep_agent.go:1296-1310` |
| Python 参考 | `openjiuwen/harness/deep_agent.py:306-315` |

**Python 样例：**
```python
# Sync direct fields so configure()'s LLM-reset condition fires correctly.
if config.model.model_client_config is not None:
    client_cfg = config.model.model_client_config
    new_react_config.api_base = client_cfg.api_base
    new_react_config.api_key = client_cfg.api_key
    new_react_config.model_provider = str(
        client_cfg.client_provider.value
        if hasattr(client_cfg.client_provider, "value")
        else client_cfg.client_provider
    )
```

**Go 问题：** `hotReloadModel` 只设置了 `ModelClientConfig` 和 `ModelRequestConfig`，没有同步 `api_base`、`api_key`、`model_provider` 直接字段。如果 `ReActAgent.Configure()` 依赖这些直接字段判断是否需要重建 LLM 客户端，热重载模型时可能无法正确触发 LLM 重置。

**修复方案：** 在 `hotReloadModel` 中补充直接字段同步：
```go
if clientCfg != nil {
    newConfig.APIBase = clientCfg.APIBase
    newConfig.APIKey = clientCfg.APIKey
    if clientCfg.ClientProvider != "" {
        newConfig.ModelProvider = string(clientCfg.ClientProvider)
    }
}
```

---

### S-04: SysOperation sandbox 模式验证/初始化逻辑缺失

| 项目 | 内容 |
|------|------|
| 章节 | 9.32/9.33 SysOperation |
| Go 文件 | `internal/agentcore/sys_operation/sys_operation.go:61-82, 156-198` |
| Python 参考 | `openjiuwen/core/sys_operation/sys_operation.py:170-184` |

**Python 样例：**
```python
if self._mode == OperationMode.SANDBOX:
    self._validate_sandbox_gateway_config(gateway_config)
    isolation_key_template = generate_isolation_key_template(...)
    self._sandbox_run_config = SandboxRunConfig(
        config=gateway_config,
        isolation_key_template=isolation_key_template,
    )
```

**Go 问题：** `NewSysOperation` sandbox 分支直接 fallback 到 `NewLocalSysOperation`，无任何 sandbox 特殊处理。缺少 `_validate_sandbox_gateway_config` 验证逻辑和 `SandboxRunConfig` 构造。

**修复方案：** 此为 9.34 SandboxSysOperation 未实现导致的已知缺失，需在实现 9.34 时补齐。当前应在 `NewSysOperation` sandbox 分支添加配置验证（即使仍 fallback），避免运行时静默使用 LocalSysOperation 产生错误行为。

---

### S-05: SysOperation 进程组终止 PGID 逻辑错误

| 项目 | 内容 |
|------|------|
| 章节 | 9.35 Shell Process Registry |
| Go 文件 | `internal/agentcore/sys_operation/shell_process_registry_unix.go:47-70` |
| Python 参考 | `openjiuwen/core/sys_operation/shell_process_registry.py:154-198` |

**Python 样例：**
```python
# POSIX: 使用 os.getpgid 获取实际进程组 ID
os.killpg(os.getpgid(proc.pid), signal.SIGTERM)
proc.wait(timeout=3)
os.killpg(os.getpgid(proc.pid), signal.SIGKILL)
proc.wait(timeout=1)
```

**Go 问题：**
```go
syscall.Kill(-pid, SIGTERM)  // 直接用 -pid，假设 PGID == PID
```
Go 直接用 `-proc.Pid`，假设进程的 PGID 等于其 PID（即进程是组长）。如果不成立（如进程被 shell 放入了不同的进程组），会杀错进程组或失败。

**修复方案：** 使用 `syscall.Getpgid` 获取实际 PGID：
```go
pgid, err := syscall.Getpgid(pid)
if err != nil {
    pgid = pid  // fallback
}
syscall.Kill(-pgid, SIGTERM)
```

---

### S-06: SkillManager ClawHub Token 存储路径不一致

| 项目 | 内容 |
|------|------|
| 章节 | 10.3.19 SkillManager |
| Go 文件 | `internal/swarm/server/runtime/skill/skill_manager.go:1172` |
| Python 参考 | `jiuwenswarm/server/runtime/skill/skill_manager.py:3820` |

**Python 样例：**
```python
token = state["clawhub"]["token"]  # 嵌套结构
```

**Go 问题：**
```go
token, _ := state["clawhub_token"]  # 扁平 key
```
跨语言互操作时（Python 已有 `skills_state.json` 中嵌套 `clawhub` 对象），Go 无法读取到 token，反之亦然。

**修复方案：** 统一为嵌套结构 `state["clawhub"]["token"]`，或在读取时同时检查两种格式（兼容旧数据）。

---

### S-07: SkillManager `_normalize_plugin` 逻辑缺失

| 项目 | 内容 |
|------|------|
| 章节 | 10.3.19 SkillManager |
| Go 文件 | `internal/swarm/server/runtime/skill/skill_manager.go:1165` |
| Python 参考 | `jiuwenswarm/server/runtime/skill/skill_manager.py:3793-3796` |

**Python 样例：**
```python
def _normalize_plugin(p):
    p.setdefault("enabled", True)
    return p
```

**Go 问题：** `normalizePlugin(p)` 直接返回 `p`，不做任何补全。如果插件记录中没有 `enabled` 字段，持久化状态写入时缺失该字段。

**修复方案：**
```go
func normalizePlugin(p map[string]any) map[string]any {
    if _, ok := p["enabled"]; !ok {
        p["enabled"] = true
    }
    return p
}
```

---

### S-08: SkillManager `HandleSkillsGet` 缺少 `is_builtin` / `is_builtin_source` 字段

| 项目 | 内容 |
|------|------|
| 章节 | 10.3.19 SkillManager |
| Go 文件 | `internal/swarm/server/runtime/skill/skill_manager.go:1377-1416` |
| Python 参考 | `jiuwenswarm/server/runtime/skill/skill_manager.py:403-412` |

**Python 样例：**
```python
meta["is_builtin"] = self._is_builtin_skill(skill_dir, name)
meta["is_builtin_source"] = builtin_skill_path.exists()
```

**Go 问题：** 本地 skill 路径查找时完全没有设置 `is_builtin` 和 `is_builtin_source` 字段。前端无法区分本地 skill 是否来自内置技能目录。

**修复方案：** 在 `findSkillInDir` 中补充：
```go
meta["is_builtin"] = sm.isBuiltinSkill(skillDir, name)
meta["is_builtin_source"] = sm.builtinSkillPathExists(name)
```

---

### S-09: SkillDev Service `methodDispatch` 是包级变量，多实例覆盖

| 项目 | 内容 |
|------|------|
| 章节 | 10.3.20 SkillDev |
| Go 文件 | `internal/swarm/server/runtime/skill/skilldev/service.go:50,60-68` |
| Python 参考 | `jiuwenswarm/server/runtime/skilldev/service.py` |

**Python 样例：**
```python
# 模块级 dict，值为方法名字符串
_METHOD_DISPATCH = {"start": "_handle_start", ...}
# 通过 getattr(self, handler_name) 动态查找实例方法
```

**Go 问题：** `methodDispatch` 是包级 `var`，在 `NewSkillDevService` 中初始化。多次创建实例会覆盖全局变量。

**修复方案：** 改为实例级变量或 `sync.Once` 初始化：
```go
type SkillDevService struct {
    methodDispatch map[string]func(...) ([]map[string]any, error)
    // ...
}
```

---

### S-10: SkillDev `handleStart` 中 `skilldev.started` 事件缺少 `is_complete` 字段

| 项目 | 内容 |
|------|------|
| 章节 | 10.3.20 SkillDev |
| Go 文件 | `internal/swarm/server/runtime/skill/skilldev/service.go:144-147` |
| Python 参考 | `jiuwenswarm/server/runtime/skilldev/service.py:104-109` |

**Python 样例：**
```python
yield {"event_type": "skilldev.started", "is_complete": False, ...}
```

**Go 问题：** `skilldev.started` 事件没有 `is_complete` 字段。前端可能依赖此字段判断流是否结束。

**修复方案：** 补齐 `is_complete` 字段：
```go
map[string]any{"event_type": "skilldev.started", "is_complete": false, ...}
```

---

### S-11: SkillDev TestRunStage 使用 `assertions` 而非 Python 的 `expectations`

| 项目 | 内容 |
|------|------|
| 章节 | 10.3.20 SkillDev |
| Go 文件 | `internal/swarm/server/runtime/skill/skilldev/stages/test_run_stage.go:99` |
| Python 参考 | `jiuwenswarm/server/runtime/skilldev/stages/test_run_stage.py` |

**Python 样例：**
```python
"expectations": case.get("expectations", [])
```

**Go 问题：**
```go
"assertions": getSliceFromAny(caseMap["assertions"])
```
字段名不一致。前端或其他消费方如果期望 `expectations` 字段，会读取不到数据。

**修复方案：** 统一字段名为 `expectations`：
```go
"expectations": getSliceFromAny(caseMap["expectations"])
```

---

### S-12: AgentModeRail `isTaskToolRegistered` 查询范围不同

| 项目 | 内容 |
|------|------|
| 章节 | 9.14 AgentModeRail |
| Go 文件 | `internal/agentcore/harness/rails/agent_mode.go:716-740` |
| Python 参考 | `openjiuwen/harness/rails/agent_mode_rail.py:346-356` |

**Python 样例：**
```python
tools = Runner.resource_mgr.get_tool()  # 查全局 ResourceMgr
return any(getattr(getattr(t, "card", None), "name", None) == "task_tool" for t in tools)
```

**Go 问题：** 通过 `r.agent.ReactAgent().AbilityManager().List()` 查询，而非全局 `ResourceMgr`。如果 task_tool 在其他路径注册到 ResourceMgr 但不在当前 ReactAgent 的 AbilityManager 中，Go 会误判。

**修复方案：** 改用全局 ResourceMgr 查询，对齐 Python：
```go
func (r *AgentModeRail) isTaskToolRegistered() bool {
    if r.ownsTaskTool { return true }
    resourceMgr := runner.GetResourceMgr()
    if resourceMgr == nil { return false }
    tools, err := resourceMgr.GetTool([]string{"task_tool"})
    return err == nil && len(tools) > 0
}
```

---

### S-13: ScheduleAutoInvokeOnSpawnDone 缺少 IsAutoInvokeScheduled 检查防护

| 项目 | 内容 |
|------|------|
| 章节 | 9.1 DeepAgent |
| Go 文件 | `internal/agentcore/harness/deep_agent.go:562` |
| Python 参考 | `openjiuwen/harness/deep_agent.py:579-583` |

**Python 样例：**
```python
if not agent.is_auto_invoke_scheduled:
    agent.set_auto_invoke_scheduled(True)
    asyncio.create_task(agent.schedule_auto_invoke_on_spawn_done(steer_text))
```

**Go 问题：** `ScheduleAutoInvokeOnSpawnDone` 无条件设置 `autoInvokeScheduled = true` 并启动 goroutine，外部直接调用可能创建多个并发的 auto-invoke goroutine。

**修复方案：**
```go
func (d *DeepAgent) ScheduleAutoInvokeOnSpawnDone(...) error {
    if d.IsAutoInvokeScheduled() {
        return nil  // 已调度，跳过
    }
    d.autoInvokeScheduled.Store(true)
    go func() { ... }()
    return nil
}
```

---

### S-14: ScheduleAutoInvokeOnSpawnDone 使用 10 分钟硬编码超时，Python 无超时

| 项目 | 内容 |
|------|------|
| 章节 | 9.1 DeepAgent |
| Go 文件 | `internal/agentcore/harness/deep_agent.go:582-583` |
| Python 参考 | `openjiuwen/harness/deep_agent.py:1959-1989` |

**Python 样例：**
```python
async def schedule_auto_invoke_on_spawn_done(self, query, delay=0.5):
    await asyncio.sleep(delay)
    # ... invoke 没有超时限制
    await self.invoke({"query": query}, session=self._loop_session)
```

**Go 问题：**
```go
invokeCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
```
长时间运行的任务可能被意外中断。Python 没有此超时，依赖 Agent 自身的循环退出机制。

**修复方案：** 移除硬编码超时，或改为可配置：
```go
invokeCtx, cancel := context.WithCancel(ctx)
// 不设置超时，与 Python 对齐
```

---

### S-16: HeartbeatRail GetCallbacks() 未覆盖，BeforeModelCall 永远不会被调用

| 项目 | 内容 |
|------|------|
| 章节 | 9.15 HeartbeatRail |
| Go 文件 | `internal/agentcore/harness/rails/heartbeat.go` |
| Python 参考 | `openjiuwen/harness/rails/heartbeat_rail.py` |

**Python 样例：** Python AgentRail 框架通过 `get_callbacks()` 自动检测子类覆盖的钩子方法（反射/inspect），所以 Python 不需要显式注册。

**Go 问题：** `HeartbeatRail` 没有覆盖 `GetCallbacks()` 方法。对比 `TaskPlanningRail`、`AgentModeRail`、`TaskCompletionRail`、`ProgressiveToolRail` 都覆盖了 `GetCallbacks()` 并注册了 `CallbackBeforeModelCall`。由于 `BaseRail.GetCallbacks()` 返回空 map，**BeforeModelCall 永远不会被回调框架调用**，心跳功能完全失效。

**修复方案：** 添加 `GetCallbacks()` 覆盖：
```go
func (r *HeartbeatRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
    callbacks := r.DeepAgentRail.GetCallbacks()
    callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
        return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
    }
    return callbacks
}
```

---

### S-17: AgentServer applyResolvedModeToRequest 缺少 team→code 和 auto_harness→agent 映射

| 项目 | 内容 |
|------|------|
| 章节 | 10.3.1 AgentServer |
| Go 文件 | `internal/swarm/server/handle_envelope.go:510-536` |
| Python 参考 | `jiuwenswarm/server/agent_ws_server.py:190-224` |

**Python 样例：**
```python
if mode == "team.plan":
    return ("code", "team", canonical)
if mode == "auto_harness":
    agent_mode = "agent"
```

**Go 问题：** 仅做 `strings.SplitN(modeStr, ".", 2)` 拆分。`team.plan` 模式返回 `"team"` 而非 `"code"`；`auto_harness` 未映射为 `"agent"`。

**修复方案：** 添加模式映射：
```go
if mode == "team" && subMode == "plan" {
    return "code", "team"
}
if mode == "auto_harness" {
    return "agent", subMode
}
```

---

### S-18: AgentServer resolveRequestProjectDir 缺少 cwd/trusted_dirs 回退链

| 项目 | 内容 |
|------|------|
| 章节 | 10.3.1 AgentServer |
| Go 文件 | `internal/swarm/server/handle_envelope.go:539-562` |
| Python 参考 | `jiuwenswarm/server/agent_ws_server.py:162-187` |

**Python 样例：**
```python
# 按优先级链解析：params.project_dir → metadata.project_dir → params.cwd → metadata.cwd → params.trusted_dirs[0] → None
```

**Go 问题：** 仅解析 `params.workspace_dir` → `metadata.workspace_dir`，缺少 `project_dir`、`cwd`、`trusted_dirs` 回退链。旧客户端仅发送 `cwd` 时 projectDir 解析为空串。

**修复方案：** 补齐回退链：
```go
func resolveRequestProjectDir(request *schema.AgentRequest) string {
    // 1. params.workspace_dir
    // 2. metadata.workspace_dir
    // 3. params.project_dir
    // 4. metadata.project_dir
    // 5. params.cwd
    // 6. metadata.cwd
    // 7. params.trusted_dirs[0]
}
```

---

### S-19: UapClaw ProcessMessage/Stream 历史记录缺少 channel_metadata 和 mode

| 项目 | 内容 |
|------|------|
| 章节 | 10.3.2 UapClaw |
| Go 文件 | `internal/swarm/server/runtime/uapclaw.go:97-99, 157-159, 199-213` |
| Python 参考 | `jiuwenswarm/server/runtime/agent_adapter/interface.py:815-824, 1061-1081` |

**Python 样例：**
```python
append_history_record(
    session_id=session_id, ..., role="user", content=query,
    channel_metadata=request.metadata,
    mode=request.params.get("mode", "unknown"),
)
```

**Go 问题：** `AppendHistoryRecord` 调用中 `channel_metadata` 传 nil（应传 request.Metadata），`mode` 传空串（应从 request.Params 提取 "mode"），流式记录中 `extra` 传 nil（Python 提取除 event_type/content 外的额外字段）。

**修复方案：** 从 request 提取 metadata 和 mode 传给 AppendHistoryRecord。

---

### S-20: WebChannel HandleWebSocket 缺少请求-客户端映射，多客户端消息串台

| 项目 | 内容 |
|------|------|
| 章节 | 11.x Gateway |
| Go 文件 | `internal/swarm/gateway/channel_manager/web/web_connect.go:126-253` |
| Python 参考 | `jiuwenswarm/gateway/app_gateway.py:638-643` |

**Python 样例：**
```python
# 收到 type=req 帧时，将 (channel_id, req_id) → ws 记录
self._request_to_client[(channel_id, req_id)] = ws
# send() 时精确找到目标客户端推送响应
```

**Go 问题：** 所有出站消息都通过 `broadcastEvent` 广播到所有连接客户端。多客户端场景下，每个客户端都会收到所有响应，导致消息串台。

**修复方案：** 添加 request→client 和 session→client 映射，`Send` 时根据 channelID+requestID 定向投递。

---

### S-21: fsnotify 热重载 updatedKeys 传 nil 导致 browser.runtime_restart 永不触发

| 项目 | 内容 |
|------|------|
| 章节 | 11.x Gateway |
| Go 文件 | `internal/swarm/gateway/app_gateway.go:176` |
| Python 参考 | `jiuwenswarm/gateway/app_gateway.py:964-976` |

**Python 样例：**
```python
updated_env_keys = env_updates.keys() | yaml_updated
if browserRuntimeKeys & updated_env_keys:
    await self._browser_runtime_restart()
```

**Go 问题：** `onConfigSavedImpl(nil, BuildEnvMap(), configData)` 第一个参数 `updatedKeys` 传了 `nil`。`ShouldBrowserRestart(nil)` 永远返回 false，fsnotify 触发的热重载永远不会触发 `browser.runtime_restart`。

**修复方案：** 在 fsnotify OnReload 回调中计算变更的 key 集合并传入。

---

### S-22: UapClaw ProcessMessageStream 流式消费者 goroutine 数据竞争

| 项目 | 内容 |
|------|------|
| 章节 | 10.3.2 UapClaw |
| Go 文件 | `internal/swarm/server/runtime/uapclaw.go:185-231` |
| Python 参考 | `jiuwenswarm/server/runtime/agent_adapter/interface.py:1017-1019` |

**Python 样例：**
```python
while not stream_done.is_set() or not stream_queue.empty():
    item = await asyncio.wait_for(stream_queue.get(), timeout=0.1)
```
Python 使用 `asyncio.Queue` + `wait_for` 超时轮询，无竞争风险。

**Go 问题：** 消费者 goroutine 使用 `select` 同时监听 `outCh` 和 `streamDone`，`streamDone` 到达时通过 `for len(outCh) > 0` 排空，但 `len(outCh)` 是非原子检查，在检查与消费之间可能有新 chunk 入队。

**修复方案：** 改进消费逻辑：生产者关闭 outCh 后消费者自然退出 range 循环，无需手动排空。

---

### S-15: SysOperationRail Init 幂等注册逻辑先 remove 后 add 可能导致中间状态

| 项目 | 内容 |
|------|------|
| 章节 | 9.18 SysOperationRail |
| Go 文件 | `internal/agentcore/harness/rails/sys_operation.go:183-201` |
| Python 参考 | `openjiuwen/harness/rails/sys_operation_rail.py:85-88` |

**Python 样例：**
```python
# Python 先 remove 再 add，但在同一个逻辑块中
Runner.resource_mgr.remove_tool([t.card.id for t in self.tools])
Runner.resource_mgr.add_tool(self.tools)
agent.ability_manager.add([t.card for t in self.tools])
```

**Go 问题：** Go 的实现先循环 remove 所有工具，再循环 add。在 remove 和 add 之间存在短暂窗口期，此时工具不可用。Python 的实现是先全部 remove 再全部 add，逻辑等价但 Python 的 `add_tool` 支持批量操作，保证原子性更好。

**修复方案：** 此差异影响较小，但建议在 ResourceMgr 中添加批量 AddTool 支持，减少中间状态。

---

## 二、一般问题（逻辑差异，可能影响边界场景）

### G-01: DeepAgent CreateSubagent 缺少 factory_kwargs 合并

| 项目 | 内容 |
|------|------|
| 章节 | 9.1 DeepAgent |
| Go 文件 | `internal/agentcore/harness/deep_agent.go:627-655` |
| Python 参考 | `openjiuwen/harness/deep_agent.py:991-1030` |

**Python 样例：**
```python
return create_deep_agent(**create_kwargs, **dict(spec.factory_kwargs or {}))
```

**Go 问题：** `CreateSubagent` 没有使用 `subCfg.FactoryKwargs`。Python 中通过 `factory_kwargs` 传递的子 Agent 覆盖参数在 Go 中被完全忽略。

**修复方案：** 在 `buildSubagentCreateKwargs` 或 `CreateDeepAgent` 调用前合并 `FactoryKwargs`。

---

### G-02: TaskLoop _streaming 传递逻辑与 Python 不一致

| 项目 | 内容 |
|------|------|
| 章节 | 9.4 TaskLoopController |
| Go 文件 | `internal/agentcore/harness/task_loop/executor.go:223-228` |
| Python 参考 | `openjiuwen/harness/task_loop/task_loop_event_executor.py:201-203` |

**Python 样例：**
```python
result = await agent.react_agent.invoke(effective, session, _streaming=True)
# 始终传 _streaming=True
```

**Go 问题：** Go 从 `task.Metadata` 中读取 `_streaming`，如果 metadata 中没有则不传。对非流式 invoke 调用，`_streaming` 可能缺失，而 Python 始终为 True。

**修复方案：** 默认 `_streaming=true`，对齐 Python：
```go
if _, ok := task.Metadata["_streaming"]; !ok {
    effective["_streaming"] = true
} else {
    effective["_streaming"] = task.Metadata["_streaming"]
}
```

---

### G-03: SysOperation dispatchShellMethod 未传递完整参数

| 项目 | 内容 |
|------|------|
| 章节 | 9.32/9.33 SysOperation |
| Go 文件 | `internal/agentcore/sys_operation/tool_adapter.go:154-221` |
| Python 参考 | `openjiuwen/core/sys_operation/tool_adapter.py:46-63` |

**Python 样例：**
```python
# Python 通过 getattr(sub_op, tool_card.name) 直接绑定方法，参数由调用方传递，不做截断
method = getattr(sub_op, tool_card.name)
result = method(**kwargs)
```

**Go 问题：**
- `execute_cmd_stream` 只传递 `cwd` 选项，缺少 `timeout`、`environment`、`shell_type`
- `execute_cmd_background` 只传递 `cwd` 选项，缺少 `environment`、`shell_type`，且无法传递 `grace`
- `dispatchFsMethod` 中 `write_file` 缺少 `mode`/`prepend_newline`/`append_newline`/`append`/`permissions`
- `read_file` 缺少 `head`/`tail`/`line_range`/`encoding`/`chunk_size`

**修复方案：** 逐方法补齐参数传递，从 tool args 中解析对应选项并传递。

---

### G-04: SysOperation LocalWorkConfig shell_allowlist 默认值与 Python 完全不同

| 项目 | 内容 |
|------|------|
| 章节 | 9.32/9.33 SysOperation |
| Go 文件 | `internal/agentcore/sys_operation/config.go:59-68` |
| Python 参考 | `openjiuwen/core/sys_operation/config.py:17-21` |

**Python 样例（30 项）：**
```python
["echo", "rg", "ls", "dir", "cd", "pwd", "python", "python3", "pip", "pip3",
 "npm", "node", "git", "cat", "type", "mkdir", "md", "rm", "rd", "cp", "copy",
 "mv", "move", "grep", "find", "curl", "wget", "ps", "df", "ping"]
```

**Go 问题（44 项）：** Go 包含更多构建工具（make/cmake/cargo/go/npx/yarn/pnpm）和容器工具（docker/kubectl/terraform），但缺少 Windows 别名（dir/cd/pwd/type/md/rd/copy/move）和系统命令（ps/df/ping）。Python 缺少 head/tail/awk/sed 等 Unix 文本处理工具。

**修复方案：** 合并两个列表，保留 Python 的 Windows 别名和系统命令，同时保留 Go 的构建工具。

---

### G-05: SysOperation IsolationKeyTemplate 空前缀格式差异

| 项目 | 内容 |
|------|------|
| 章节 | 9.32/9.33 SysOperation |
| Go 文件 | `internal/agentcore/sys_operation/sys_operation_card.go:147-168` |
| Python 参考 | `openjiuwen/core/sys_operation/sys_operation.py:22-64` |

**Python 样例：**
```python
f"{container_scope.value}_{launcher_type}_{sandbox_type}_{prefix}{identity}"
# prefix 为空时：session_pre_deploy_aio_{session_id}（无多余下划线）
```

**Go 问题：** 使用 `strings.Join(parts, "_")`，当 `isolationPrefix` 为空时产生 `session_pre_deploy_aio__{session_id}`（多一个下划线，因为 Join 对空串也加分隔符）。

**修复方案：** 空 `isolationPrefix` 时从 parts 中移除：
```go
parts := []string{containerScope.String(), launcherType, sandboxType}
if isolationPrefix != "" { parts = append(parts, isolationPrefix) }
parts = append(parts, identity)
return strings.Join(parts, "_")
```

---

### G-06: SysOperation CUSTOM scope 缺少 custom_id 校验

| 项目 | 内容 |
|------|------|
| 章节 | 9.32/9.33 SysOperation |
| Go 文件 | `internal/agentcore/sys_operation/sys_operation_card.go:155` |
| Python 参考 | `openjiuwen/core/sys_operation/sys_operation.py:55-58` |

**Python 样例：**
```python
if container_scope == ContainerScope.CUSTOM and custom_id is None:
    raise ValueError("container_scope is CUSTOM but custom_id is None")
```

**Go 问题：** CUSTOM 分支直接使用 customID（空串），无校验。

**修复方案：** 添加校验：
```go
if scope == ContainerScopeCustom && customID == "" {
    return "", fmt.Errorf("container_scope is CUSTOM but custom_id is empty")
}
```

---

### G-07: SkillManager `HandleSkillsToggle` enabled 类型校验宽松

| 项目 | 内容 |
|------|------|
| 章节 | 10.3.19 SkillManager |
| Go 文件 | `internal/swarm/server/runtime/skill/skill_manager.go:210` |
| Python 参考 | `jiuwenswarm/server/runtime/skill/skill_manager.py:447-448` |

**Python 样例：**
```python
if not isinstance(enabled, bool):
    return {"ok": False, "error": "enabled must be a boolean"}
```

**Go 问题：** `toBool(enabledVal)` 宽松转换，字符串 "false"、数字 0 等也会被转换，与 Python 行为不一致。

**修复方案：** 添加严格类型检查：
```go
if _, ok := enabledVal.(bool); !ok {
    return map[string]any{"ok": false, "error": "enabled must be a boolean"}
}
```

---

### G-08: AgentModeRail `_register_task_tool` 注册顺序与 Init 方法不一致

| 项目 | 内容 |
|------|------|
| 章节 | 9.14 AgentModeRail |
| Go 文件 | `internal/agentcore/harness/rails/agent_mode.go:654-663 vs 188-197` |

**Go 问题：** Init 方法中先 AbilityManager 后 ResourceMgr；`registerTaskTool` 中先 ResourceMgr 后 AbilityManager。内部顺序不一致。

**修复方案：** 统一为先 AbilityManager 后 ResourceMgr（与 Python 顺序一致）。

---

### G-09: SkillDev TestRunStage 用 `name` 而非 `id` 作为目录名

| 项目 | 内容 |
|------|------|
| 章节 | 10.3.20 SkillDev |
| Go 文件 | `internal/swarm/server/runtime/skill/skilldev/stages/test_run_stage.go:79-80` |

**Go 问题：** `evalName` 优先从 `caseMap["name"]` 读取，fallback 到 `"eval-0"`。Python 使用 eval 的 `name` 或 `id` 字段命名目录。如果 eval case 没有 `name` 但有 `id`，Go 会 fallback 到 `"eval-0"` 而非使用 `id`，导致多个 eval 写入同一目录。

**修复方案：** 增加 `id` fallback：
```go
evalName := getStrFromAny(caseMap["name"])
if evalName == "" { evalName = getStrFromAny(caseMap["id"]) }
if evalName == "" { evalName = fmt.Sprintf("eval-%d", i) }
```

---

### G-10: AgentServer `_handle_cancel` 缺少 Python 的 intent 细粒度处理

| 项目 | 内容 |
|------|------|
| 章节 | 10.3.1 AgentServer |
| Go 文件 | `internal/swarm/server/handle_envelope.go:382-416` |
| Python 参考 | `jiuwenswarm/server/agent_ws_server.py:983-1001` |

**Python 样例：**
```python
intent = request.params.get("intent", "cancel")
if intent in ("cancel", "supplement"):
    stream_task = self._session_stream_tasks.get(sid)
    if stream_task is not None and not stream_task.done():
        stream_task.cancel()
# pause/resume 不取消流式任务
```

**Go 问题：** `handleCancel` 始终取消流式任务，不区分 intent。Python 中 pause/resume 不取消（因为任务仍在运行）。

**修复方案：** 在 `handleCancel` 中提取 intent，仅在 cancel/supplement 时取消流式任务：
```go
intent := extractIntent(request)
if intent == "cancel" || intent == "supplement" {
    s.cancelStreamTask(sessionID)
}
```

---

### G-11: AgentManager ReloadAgentsConfig 缺少 agent reload 和 team evolution 回填

| 项目 | 内容 |
|------|------|
| 章节 | 10.3.12 AgentManager |
| Go 文件 | `internal/swarm/server/runtime/agent_manager.go:64-70` |
| Python 参考 | `jiuwenswarm/server/runtime/agent_manager.py:308-340` |

**Python 样例：**
```python
for channel_id, agents in self.agents.items():
    for _, agent in agents.items():
        await agent.reload_agent_config(config_base=config, env_overrides=env)
team_manager.update_evolution_config(team_config)
```

**Go 问题：** 代码中有 TODO 标记但未实现。env 注入后未遍历所有 agent 调用 `reload_agent_config`，也未更新 team evolution config。

**修复方案：** 在 10.3.12 AgentManager 完整实现时补齐。当前需确保 TODO 被跟踪。

---

## 三、提示问题（不影响流程，日志/风格差异）

### T-01: SysOperation ContainerScope 默认值差异

Go 零值为 `ContainerScopeSystem=0`，Python 默认 `ContainerScope.SESSION`。需在 `NewSysOperationCard` 中显式设置默认值。

### T-02: SysOperation ShellType.from_str 缺少大小写处理

Python `ShellType.from_str(value)` 调用 `value.strip().lower()`，Go `ParseShellType` 直接匹配。

### T-03: SysOperation resolve_shell_session_id fallback 路径缺失

Python fallback 到 `get_session_id()`（trace ID），Go fallback 返回空串。注释说明已知差异。

### T-04: SysOperation reset_shell_session_id 语义差异

Python 使用 ContextVar.reset(token) 恢复到之前绑定的值（支持嵌套），Go 用 context.WithValue 覆盖为空串。

### T-05: SkillManager generateUUID 使用时间戳而非标准 UUID

Python 使用 `uuid.uuid4()`，Go 使用 `fmt.Sprintf("%x", time.Now().UnixNano())`。高并发下可能重复。

### T-06: SkillDev BenchmarkRun 缺少 RunNumber 默认值

Python `run_number: int = 1`，Go 零值 0。Go 生成的 benchmark JSON 中 `run_number` 为 0。

### T-07: DeepAgent restoreModeAfterPlanExit pre_plan_mode 序列化差异

Python 设 `pre_plan_mode = None`，Go 设 `PrePlanMode = ""`。序列化/反序列化时跨语言状态可能不兼容。

### T-08: DeepAgent LoopQueues 有界 vs Python 无界队列

Go 使用 `make(chan string, 64)` 有界 channel，Python 使用 `asyncio.Queue` 无界。极端高并发下 Go 可能丢弃消息。

### T-09: SysOperation CWD resolve 不解析符号链接

Python `_resolve(path)` 使用 `Path(path).resolve()` 解析符号链接，Go 使用 `filepath.Abs` + `filepath.Clean` 不解析。

### T-10: SkillManager state_utils GetStateFile 路径拼接硬编码 `/`

应使用 `filepath.Join` 跨平台兼容。

---

## 四、已知未实现（标记 ⤵️，非本次 bug）

| 编号 | 章节 | 内容 | 影响范围 |
|------|------|------|---------|
| U-01 | 10.3.2 | UapClaw GetContextUsage/CompressContext/GenerateRecap/SwitchMode/GetInstance 为 stub | 上下文管理和模式切换不可用 |
| U-02 | 10.3.2 | UapClaw ProcessMessage/Stream 中 Team 模式分支、AutoHarness resume、cloud memory hook 为 stub | Team 和云记忆功能不可用 |
| U-03 | 10.3.12 | AgentManager 为 stub，不支持多实例和按 channel 隔离 | 多通道隔离不可用 |
| U-04 | 10.3.19 | `getBuiltinSkillsDir` 返回空字符串 | 内置技能安装/列表不可用 |
| U-05 | 10.3.19 | `gitClone`/`gitPull`/`gitGetCommit` 未实现 | marketplace 安装/同步不可用 |
| U-06 | 10.3.19 | `refreshAgentDataIndexes` 为空操作 | 安装/卸载技能后新技能不生效 |
| U-07 | 10.3.20 | SkillDev 所有阶段的 `CreateStageAgent` 未实现 | Pipeline 只能产出占位数据 |
| U-08 | 9.11 | PermissionInterruptRail 未实现 | 权限 Rail 创建被跳过 |

---

## 五、问题统计

| 严重级别 | 数量 | 关键问题 |
|---------|------|---------|
| 严重 | 22 | S-01~S-22 |
| 一般 | 11 | G-01~G-11 |
| 提示 | 10 | T-01~T-10 |
| 已知未实现 | 8 | U-01~U-08 |

---

## 六、优先修复建议

### 第一优先级（影响核心功能正确性）

1. **S-01**: CWD 初始化缺少 `os.getcwd()` 兜底 — 直接影响 Agent 执行目录
2. **S-03**: hotReloadModel 直接字段未同步 — 热重载模型时 LLM 可能不重建
3. **S-05**: 进程组终止 PGID 逻辑错误 — 可能杀错进程组
4. **S-13**: ScheduleAutoInvokeOnSpawnDone 缺少防护 — 可能创建多个并发 goroutine
5. **S-14**: 10 分钟硬编码超时 — 长任务被意外中断
6. **S-16**: HeartbeatRail GetCallbacks 未覆盖 — 心跳功能完全失效
7. **S-17**: AgentServer 缺少 team→code / auto_harness→agent 模式映射 — Agent 类型错误
8. **S-20**: WebChannel 缺少请求-客户端映射 — 多客户端消息串台

### 第二优先级（影响数据一致性/前端展示）

9. **S-06**: ClawHub token 存储路径不一致 — 跨语言互操作失败
10. **S-08**: 缺少 `is_builtin`/`is_builtin_source` 字段 — 前端无法区分内置技能
11. **S-10**: `skilldev.started` 缺少 `is_complete` — 前端流式响应异常
12. **S-11**: `assertions` vs `expectations` 字段名 — 数据格式不一致
13. **S-12**: `isTaskToolRegistered` 查询范围不同 — 可能误判 task_tool 注册状态
14. **S-18**: resolveRequestProjectDir 缺少回退链 — 工作目录可能错误
15. **S-19**: 历史记录缺少 channel_metadata/mode — 回放/审计数据不完整
16. **S-21**: fsnotify updatedKeys 传 nil — browser.runtime_restart 永不触发

### 第三优先级（边界场景/一般差异）

17. **S-02**: workspace 为 nil 时缺少兜底
18. **S-07**: normalizePlugin 缺失
19. **S-09**: methodDispatch 全局变量覆盖
20. **S-22**: ProcessMessageStream 流式消费者 goroutine 数据竞争
21. **G-02**: _streaming 传递逻辑不一致
22. **G-10**: cancel 缺少 intent 细粒度处理
