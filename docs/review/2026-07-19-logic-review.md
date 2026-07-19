# 2026-07-19 逻辑审查报告

> 审查范围：48小时内提交记录（`7b0c633..03e01ed`，共45+个提交）
> 涉及章节：9.26 BrowserAgent、9.60 StreamController、9.58 SpawnManager、9.59 SessionManager、9.25 ResearchAgent、9.70c Updater Protocol、9.72e BaseOptimizer、9.73 SignalDetector、9.77 Trajectory、10.3.7 CodeAgentRail、10.3.13 AgentConfigService
> 对照标准：Python 参考项目方法签名和步骤一致性
> 审查方法：逐文件对比 Python 源码与 Go 实现，检查方法签名、逻辑步骤、占位代码

---

## 严重问题 (S)

### S01: ResearchAgent 缺少默认 SysOperationRail 注入

**严重性**：严重 — Python `create_research_agent` 明确注入 `SysOperationRail()` 作为默认 Rail，Go 的 `BuildResearchAgentConfig` 没有注入，导致 ResearchAgent 无系统操作权限护栏，功能缺失

**Python 样例**：

```python
# openjiuwen/harness/subagents/research_agent.py:155-156
def create_research_agent(model, *, rails=None, ...):
    # Full override rule: if user passes tools/rails explicitly, do not inject defaults.
    final_rails = rails if rails is not None else [SysOperationRail()]

    return create_deep_agent(
        ...,
        rails=final_rails,
        ...
    )
```

**Go 问题**：

```go
// internal/agentcore/harness/subagents/research_agent.go:72
cfg.Rails = params.Rails  // ❌ 直接透传，rails 为 nil 时不注入默认 SysOperationRail
// 缺少 Python 的 final_rails = rails if rails is not None else [SysOperationRail()]
```

**修复方案**：

在 `BuildResearchAgentConfig` 中增加默认 Rail 注入逻辑，对齐 Python 的 "Full override rule"：

```go
cfg.Rails = params.Rails
if cfg.Rails == nil {
    cfg.Rails = []rail.AgentRail{rails.NewSysOperationRail()}  // 对齐 Python
}
```

**流程示例**：

```
Python: create_research_agent(model, rails=None)
  → final_rails = [SysOperationRail()]  ✅ 有系统操作权限护栏
  → create_deep_agent(rails=final_rails)

Go: BuildResearchAgentConfig(model, params)
  → cfg.Rails = nil  ❌ 无系统操作权限护栏
  → DeepAgent 创建时无任何 Rail 保护
```

---

### S02: BrowserService 缺少 inflight task 追踪机制

**严重性**：严重 — Python `BrowserService` 有完整的 `_inflight_tasks` 字典 + `_register_inflight_task`/`_unregister_inflight_task` 方法，用于追踪正在执行的任务并在取消时取消任务。Go 完全缺失此机制，导致 `RequestCancel` 无法真正取消正在执行的浏览器任务

**Python 样例**：

```python
# openjiuwen/harness/tools/browser_move/playwright_runtime/service.py:221-234
def _register_inflight_task(self, session_id, request_id, task):
    keys = (self._inflight_key(session_id), self._inflight_key(session_id, request_id))
    for key in keys:
        self._inflight_tasks.setdefault(key, set()).add(task)

def _unregister_inflight_task(self, session_id, request_id, task):
    keys = (self._inflight_key(session_id), self._inflight_key(session_id, request_id))
    for key in keys:
        tasks = self._inflight_tasks.get(key)
        if not tasks:
            continue
        tasks.discard(task)
        if not tasks:
            self._inflight_tasks.pop(key, None)

# request_cancel 中取消 inflight tasks:
# service.py:547-549
for key in keys:
    for task in list(self._inflight_tasks.get(key, set())):
        if not task.done():
            task.cancel()
```

**Go 问题**：

```go
// internal/agentcore/harness/tools/browser_move/service.go
// ❌ BrowserService 结构体中没有 inflightTasks 字段
// ❌ 没有 registerInflightTask/unregisterInflightTask 方法
// ❌ RequestCancel 只是设置 KV store 标记，无法真正取消运行中的 goroutine
```

**修复方案**：

1. 在 `BrowserService` 结构体中添加 `inflightTasks map[string]map[context.CancelFunc]struct{}`
2. 实现 `registerInflightTask`/`unregisterInflightTask` 方法
3. `RequestCancel` 中遍历 inflight tasks 并调用 cancel 函数
4. `RunTask` 中注册/注销 inflight task

---

### S03: BrowserService.EnsureRuntimeReady/EnsureStarted/runTaskOnceWithTimeout 均为空实现

**严重性**：严重 — BrowserService 三个核心方法全部返回空/占位结果，无法实际启动浏览器运行时或执行任务。虽然标记了 ⤵️ 9.38-49，但这意味着 BrowserAgent 的核心功能完全不可用

**Python 样例**：

```python
# openjiuwen/harness/tools/browser_move/playwright_runtime/service.py:578-627
async def ensure_runtime_ready(self):
    if self.started:
        browser_rebound = await self._ensure_managed_driver_started()
        ...
    # 注册 MCP server、启动 heartbeat 等

async def ensure_started(self):
    await self.ensure_runtime_ready()
    if self._browser_agent is not None:
        return
    self._browser_agent = build_browser_worker_agent(...)  # 构建 Worker Agent
```

**Go 问题**：

```go
// service.go:375-378
func (s *BrowserService) EnsureRuntimeReady(_ context.Context) error {
    // TODO: ⤵️ 9.38-49 回填 ManagedBrowserDriver 逻辑
    return nil  // ❌ 直接返回 nil，无任何实际初始化
}

// service.go:384-390
func (s *BrowserService) EnsureStarted(ctx context.Context) error {
    if err := s.EnsureRuntimeReady(ctx); err != nil { return err }
    // TODO: ⤵️ 9.38-49 回填 BuildBrowserWorkerAgent
    return nil  // ❌ 永远不创建 Worker Agent
}

// service.go:747-770
func (s *BrowserService) runTaskOnceWithTimeout(...) (map[string]any, error) {
    ...
    return map[string]any{
        "ok": false,
        "error": "BrowserService.runTaskOnce not implemented yet",  // ❌ 占位返回
    }, nil
}
```

**修复方案**：

按优先级在 9.38-49 回填时实现：
1. `EnsureRuntimeReady` — MCP server 注册 + managed driver 启动
2. `EnsureStarted` — Worker Agent 构建（`build_browser_worker_agent` 对齐）
3. `runTaskOnceWithTimeout` — 调用 `Runner.RunAgent` 执行浏览器任务

---

### S04: BrowserService 缺少心跳检测机制

**严重性**：严重 — Python 有完整的心跳循环 `_heartbeat_loop` + `_check_connection`，定期检查浏览器连接健康状态。Go 完全缺失，无法检测浏览器连接断开

**Python 样例**：

```python
# service.py:629-671
def _start_heartbeat(self):
    if self._heartbeat_task is None or self._heartbeat_task.done():
        self._heartbeat_task = asyncio.create_task(self._heartbeat_loop(), name="browser-heartbeat")

async def _heartbeat_loop(self):
    while True:
        await asyncio.sleep(self._heartbeat_interval)
        try:
            await self._check_connection()
            self._connection_healthy = True
            self._last_heartbeat_ok = asyncio.get_event_loop().time()
        except Exception as exc:
            self._connection_healthy = False
```

**Go 问题**：

```go
// service.go 中无 StartHeartbeat / heartbeatLoop / checkConnection 方法
// connectionHealthy 和 lastHeartbeatOK 字段存在但从未更新
```

**修复方案**：

添加 `startHeartbeat`/`heartbeatLoop`/`checkConnection` 方法，在 `EnsureRuntimeReady` 成功后调用 `startHeartbeat`

---

### S05: BrowserService.Shutdown 未停止 MCP server 和 Runner

**严重性**：严重 — Python `shutdown` 会调用 `Runner.stop()` 停止 MCP 子进程，Go 的 `Shutdown` 仅清理内存状态，不停止底层资源

**Python 样例**：

```python
# service.py:1482-1494
async def shutdown(self):
    if self._heartbeat_task is not None and not self._heartbeat_task.done():
        self._heartbeat_task.cancel()
    try:
        if self.started:
            await Runner.stop()  # ← 停止 MCP 子进程
        self.started = False
    finally:
        await self._stop_managed_driver()
```

**Go 问题**：

```go
// service.go:395-403
func (s *BrowserService) Shutdown(_ context.Context) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.started = false
    s.connectionHealthy = false
    s.managedDriver = nil
    s.browserAgent = nil
    return nil  // ❌ 未停止 MCP server / Runner / managed driver
}
```

**修复方案**：

在 `Shutdown` 中添加：
1. 停止心跳 goroutine
2. 移除已注册的 MCP server
3. 停止 managed browser driver
4. 清理 browserAgent 引用

---

### S06: NormalizeScreenshotValue 缺少本地文件→data URL 转换

**严重性**：严重 — Python 有完整的本地截图文件处理流程（解析路径→拷贝到截图文件夹→base64编码→data URL），Go 直接返回原始路径，导致多模态 API 无法识别本地截图

**Python 样例**：

```python
# service.py:438-470
def _normalize_screenshot_value(self, screenshot):
    ...
    local_path = self._resolve_local_screenshot_path(local_path_str)
    local_path = self._ensure_screenshot_in_folder(local_path)
    mime_type, _ = mimetypes.guess_type(str(local_path))
    encoded = base64.b64encode(local_path.read_bytes()).decode("ascii")
    return f"data:{mime_type};base64,{encoded}"
```

**Go 问题**：

```go
// service.go:291-311
func NormalizeScreenshotValue(screenshot any) any {
    ...
    // TODO: ⤵️ 9.38-49 回填：本地路径解析、截图文件夹拷贝、data URL 转换
    return raw  // ❌ 直接返回原始路径字符串
}
```

**修复方案**：

实现完整的本地截图文件处理：解析路径→拷贝到 screenshots/ 目录→读取文件→base64编码→构造 data URL

---

### S07: BrowserService.RunTask 缺少 inflight task 注册/注销

**严重性**：严重 — Python `run_task` 在 `async with self._locks[sid]` 中注册 inflight task，在 finally 中注销。Go 的 `RunTask` 完全缺少此逻辑，导致取消操作无法触达正在执行的任务

**Python 样例**：

```python
# service.py:1278-1480
async with self._locks[sid]:
    current_task = asyncio.current_task()
    if current_task is not None:
        self._register_inflight_task(sid, rid, current_task)
    try:
        ...  # 执行任务
    finally:
        if current_task is not None:
            self._unregister_inflight_task(sid, rid, current_task)
```

**Go 问题**：

```go
// service.go:438-441
lock := s.getLock(sid)
lock.Lock()
defer lock.Unlock()
// ❌ 无 inflight task 注册/注销
```

**修复方案**：

在 `RunTask` 中增加 inflight task 的注册（获取锁后）和注销（defer 中），关联 S02 的 inflight 追踪机制

---

### S08: BrowserService.RunTask 传输错误重启后缺少 break 保护

**严重性**：严重 — Python 在 `_restart` 失败时设置 `last_error` 并 break 退出循环，Go 的 `restart` 方法始终返回 nil，即使重启失败也不会中断重试循环

**Python 样例**：

```python
# service.py:1373-1376
if should_restart:
    try:
        await self._restart()
    except Exception as restart_exc:
        last_error = f"restart_failed: {restart_exc!r}"
        break  # ← 重启失败时立即退出
```

**Go 问题**：

```go
// service.go:515-518
if attemptIdx < attempts {
    // TODO: ⤵️ 9.38-49 回填 _restart 调用 ManagedBrowserDriver
    _ = s.restart(ctx)  // ❌ restart 永远返回 nil，无法感知重启失败
    continue
}
```

**修复方案**：

1. 实现 `restart` 方法（当前是空实现返回 nil）
2. 在 restart 失败时设置 lastError 并 break

---

### S09: CodeAgentRail — agentDefToSubagentConfig 计算了 tools 但未传给 SubAgentConfig

**严重性**：严重 — `agentDefToSubagentConfig` 内部做了 allowed_tools/disallowed_tools 合并计算（L226-241），但合并后的 tools 未赋值到 `SubAgentConfig.Tools`，导致子 Agent 可能获得不正确的工具集

**Python 样例**：

```python
# jiuwenswarm/server/runtime/agent_adapter/interface_deep.py L5999-6003
def _agent_def_to_subagent_config(spec, ...):
    tools = _filter_tool_cards(all_tools, allowed_tools=spec.tools, disallowed_tools=spec.disallowed_tools)
    return SubAgentConfig(
        ...
        tools=tools,  # ← 合并后的 tools 正确传入
    )
```

**Go 问题**：

```go
// internal/swarm/server/adapter/deep_adapter_config.go L256-264
func agentDefToSubagentConfig(...) *hschema.SubAgentConfig {
    // L226-241: 计算了 toolNames 和 disallowedToolNames
    // ...
    // ❌ 但 cfg.Tools 字段未设置，计算结果被丢弃
    cfg.Tools = nil  // 未从合并后的 tools 赋值
}
```

**修复方案**：

将合并后的 toolNames 转为 `[]*tool.ToolCard` 并赋值到 `cfg.Tools`

---

### S10: AgentConfigService 目录名 `.uapclaw` vs Python 的 `.jiuwenswarm` 不一致

**严重性**：严重 — Go 使用 `.uapclaw/agents` 而 Python 使用 `.jiuwenswarm/agents`，如果用户在 Python 版本中创建了自定义 Agent，Go 版本无法找到它们

**Python 样例**：

```python
# jiuwenswarm/server/runtime/agent_config_service.py
def _get_project_agents_dir(self):
    return self._workspace_dir / ".jiuwenswarm" / "agents"

def _get_local_agents_dir(self):
    return self._workspace_dir / ".jiuwenswarm" / "agents-local"
```

**Go 问题**：

```go
// internal/swarm/server/runtime/agent_config.go
func (s *AgentConfigService) projectAgentsDir() string {
    return filepath.Join(s.workspaceDir, ".uapclaw", "agents")  // ❌ 不兼容 Python
}

func (s *AgentConfigService) localAgentsDir() string {
    return filepath.Join(s.workspaceDir, ".uapclaw", "agents-local")  // ❌ 不兼容 Python
}
```

**修复方案**：

这是一个**设计决策**问题。如果项目统一改名，则 `.uapclaw` 是正确的；如果需要兼容 Python 版数据，则应添加 fallback 查找 `.jiuwenswarm` 目录。建议：
1. 优先查找 `.uapclaw` 目录
2. 如果不存在则 fallback 查找 `.jiuwenswarm` 目录
3. 或在文档中明确说明迁移路径

---

### S11: BuildBrowserWorkerAgent 完全未实现

**严重性**：严重 — `BuildBrowserWorkerAgent()` 仅调用 `BuildBrowserWorkerSystemPrompt` 后直接返回错误，Worker Agent 永远无法创建，BrowserAgent 核心功能不可用

**Python 样例**：

```python
# openjiuwen/harness/tools/browser_move/playwright_runtime/agents.py L301-355
def build_browser_worker_agent(provider, api_key, ..., tool_result_observer=None):
    agent = ReActAgent(...)
    agent.ability_manager.add(mcp_cfg, ...)  # 注册 MCP 工具
    agent = ensure_execute_signature_compat(agent, ...)  # 工具超时包装 + observer
    return agent
```

**Go 问题**：

```go
// internal/agentcore/harness/tools/browser_move/agents.go L126-146
func BuildBrowserWorkerAgent(...) (*agents.ReActAgent, error) {
    _, err := BuildBrowserWorkerSystemPrompt(...)
    if err != nil { return nil, err }
    return nil, fmt.Errorf("BuildBrowserWorkerAgent: ReActAgent 完整配置待 9.38-49 回填")  // ❌ 始终返回错误
}
```

**修复方案**：

在 9.38-49 回填时实现完整 Worker Agent 构建：创建 ReActAgent + 注册 MCP 工具 + 注入 observer 回调

---

### S12: BrowserAgentRuntime.EnsureRuntimeReady/EnsureStarted 关键初始化未完成

**严重性**：严重 — Python 的 `ensure_runtime_ready` 调用 `ensure_browser_runtime_client_patch()`、创建 `_direct_code_executor`、`bind_code_executor`、`register_builtin_actions`。Go 的实现中 `codeExecutor` 初始化和 `controller.RegisterBuiltinActions()` 未调用，runtime tools 无法注册到 Runner 资源管理器

**Python 样例**：

```python
# openjiuwen/harness/tools/browser_move/playwright_runtime/runtime.py L311-370
async def ensure_runtime_ready(self):
    ensure_browser_runtime_client_patch()
    self._direct_code_executor = DirectCodeExecutor(...)
    bind_code_executor(self._direct_code_executor)
    register_builtin_actions(self._direct_code_executor)

async def ensure_started(self):
    await self.ensure_runtime_ready()
    tools = [
        BrowserCustomActionTool(self, language),
        BrowserListActionsTool(self),
        BrowserProbeInteractivesTool(self, language),
        BrowserProbeCardsTool(self, language),
    ]
    Runner.resource_mgr.add(tool, tag="browser.runtime")  # 注册到资源管理器
```

**Go 问题**：

```go
// internal/agentcore/harness/tools/browser_move/runtime.go L159-228
func (r *BrowserAgentRuntime) EnsureRuntimeReady() error {
    // TODO: codeExecutor 初始化 + RegisterBuiltinActions 未调用
}

func (r *BrowserAgentRuntime) EnsureStarted(language string) error {
    // 创建了工具实例，但 _register_runtime_tool 和 ability_manager.add 均有 TODO 占位
}
```

**修复方案**：

9.38-49 回填时完成 codeExecutor 初始化 + runtime tools 注册

---

### S13: site_profiles.go — defaultCachePath 不展开 `~` 路径

**严重性**：一般（降级自严重） — Go 的 `os.ExpandEnv` 不展开 `~`，如果环境变量 `OPENJIUWEN_BROWSER_SELECTOR_CACHE` 值为 `~/cache.json`，Go 不会展开 `~` 而 Python 的 `Path(raw).expanduser()` 会

**Python 样例**：

```python
# openjiuwen/harness/tools/browser_move/playwright_runtime/site_profiles.py L109-114
def _default_cache_path() -> Path:
    raw = os.environ.get("OPENJIUWEN_BROWSER_SELECTOR_CACHE", "").strip()
    if raw:
        return Path(raw).expanduser()  # ← 支持 ~ 展开
```

**Go 问题**：

```go
// internal/agentcore/harness/tools/browser_move/site_profiles.go L484-495
func defaultCachePath() string {
    raw := strings.TrimSpace(os.Getenv("OPENJIUWEN_BROWSER_SELECTOR_CACHE"))
    if raw != "" {
        expanded := os.ExpandEnv(raw)  // ❌ 不展开 ~
        return expanded
    }
}
```

**修复方案**：

在 `os.ExpandEnv` 后添加 `expandHomeDir` 处理，替换 `~` 为 `os.UserHomeDir()`

---

### S14: BaseOptimizerMixin.Bind 缺少 DefaultTargets fallback

**严重性**：严重 — Python 中当 `targets` 为 None 时回退到 `self.default_targets()`，Go 直接存 nil，导致 `FilterOperators` 用 nil targets 过滤时匹配不到任何 operator，绑定结果始终为 0

**Python 样例**：

```python
# openjiuwen/agent_evolving/optimizer/base.py L91
self._targets = list(targets or self.default_targets())
```

**Go 问题**：

```go
// internal/evolving/optimizer/base.go L166
m.targets = targets  // ❌ targets 为 nil 时无 DefaultTargets() 回退
```

**修复方案**：

```go
m.targets = targets
if len(m.targets) == 0 {
    m.targets = m.DefaultTargets()  // 对齐 Python: targets or self.default_targets()
}
```

**流程示例**：

```
Python: optimizer.Bind(operators={"llm": op}, targets=None)
  → self._targets = list(None or self.default_targets()) = ["system_prompt"]
  → FilterOperators 匹配 "system_prompt" → 返回 1

Go: mixin.Bind(operators={"llm": op}, targets=nil)
  → m.targets = nil
  → FilterOperators(operators, nil) → range nil → 返回 0  ❌
```

---

### S15: BaseOptimizer 缺少 backward/step 模板方法包装

**严重性**：严重 — Python 的 `backward()` 和 `step()` 是模板方法，统一调用 `_validate_parameters()` + `_select_signals()` + 子类 `_backward()`/`_step()` + 错误包装 + `clear_trajectories()`。Go 把 backward/step 做成纯接口方法，子类需重复实现所有横切逻辑，易遗漏

**Python 样例**：

```python
# openjiuwen/agent_evolving/optimizer/base.py L119-148
async def backward(self, signals):
    self._validate_parameters()
    self._selected_signals = self._select_signals(signals)
    try:
        await self._backward(signals)
    except Exception as e:
        raise build_error(TOOLCHAIN_OPTIMIZER_BACKWARD_EXECUTION_ERROR, ..., cause=e)

def step(self):
    self._validate_parameters()
    try:
        updates = self._step()
        self.clear_trajectories()  # ← 正常路径清理
        return updates or {}
    except Exception as e:
        self.clear_trajectories()  # ← 异常路径也清理
        raise build_error(TOOLCHAIN_OPTIMIZER_UPDATE_EXECUTION_ERROR, ..., cause=e)
```

**Go 问题**：

```go
// internal/evolving/optimizer/base.go
// BaseOptimizer 是纯接口，Backward/Step 方法无统一模板逻辑
// 子优化器需自行：1) 调用 ValidateParameters 2) 调用 SelectSignals
// 3) 处理错误包装 4) 在 step 后调用 ClearTrajectories
```

**修复方案**：

在 `BaseOptimizerMixin` 中实现 `Backward` 和 `Step` 模板方法：

```go
func (m *BaseOptimizerMixin) Backward(ctx context.Context, signals []*signal.EvolutionSignal) error {
    if err := m.ValidateParameters(); err != nil { return err }
    m.SetSelectedSignals(m.SelectSignals(signals))
    if err := m.doBackward(ctx, signals); err != nil {
        return exception.NewBaseError(TOOLCHAIN_OPTIMIZER_BACKWARD_EXECUTION_ERROR, ...)
    }
    return nil
}

func (m *BaseOptimizerMixin) Step() (map[schema.UpdateKey]any, error) {
    if err := m.ValidateParameters(); err != nil { return nil, err }
    updates, err := m.doStep()
    m.ClearTrajectories()  // ← 正常和异常路径都清理
    if err != nil {
        return nil, exception.NewBaseError(TOOLCHAIN_OPTIMIZER_UPDATE_EXECUTION_ERROR, ...)
    }
    return updates, nil
}
```

子优化器只需实现 `doBackward`/`doStep`（对应 Python 的 `_backward`/`_step`）

---

### S16: Updater Protocol 的 Update/Process 返回类型丢失多候选集能力

**严重性**：严重 — Python 返回 `Union[Dict[tuple[str,str], Any], List[Dict[tuple[str,str], Any]]]`，支持返回单个映射或映射列表（多维归因场景）。Go 只返回 `map[UpdateKey]any`，丢失了多候选集返回能力

**Python 样例**：

```python
# openjiuwen/agent_evolving/updater/protocol.py L46-57
async def update(self, trajectories, evaluated_cases, config
) -> Union[Dict[tuple[str, str], Any], List[Dict[tuple[str, str], Any]]]:
    ...
```

**Go 问题**：

```go
// internal/evolving/updater/protocol.go L43-46
Update(...) (map[schema.UpdateKey]any, error)  // ❌ 只支持单个映射
Process(...) (map[schema.UpdateKey]any, error)  // ❌ 同上
```

**修复方案**：

修改返回类型为 `UpdateResult`：

```go
// UpdateResult 更新结果，支持单映射或多候选集
type UpdateResult struct {
    Single   map[schema.UpdateKey]any     // 单映射结果
    Multiple []map[schema.UpdateKey]any   // 多候选集结果
}

func (r *UpdateResult) IsMultiple() bool { return len(r.Multiple) > 0 }
```

---

### S17: StreamController.runOneRound — CancelledError 处理错误 + 缺少 SHUTDOWN_REQUESTED 检查

**严重性**：严重 — Python 在 `run_one_round` 中对 `CancelledError` 的处理是：如果不是 `SHUTDOWN_REQUESTED`，则设置 `MemberStatusError`；如果是 `SHUTDOWN_REQUESTED`，则走正常关闭路径。Go 把取消一律设为 `MemberStatusError`，且缺少 `SHUTDOWN_REQUESTED` → `CloseStream` 路径，导致正常关闭被误报为错误

**Python 样例**：

```python
# openjiuwen/agent_teams/agent/stream_controller.py
async def _run_one_round(self, ...):
    try:
        ...
        if self._shutdown_requested.is_set():
            await self.close_stream()
            return MemberStatus.IDLE, ExecutionStatus.SHUTDOWN
    except asyncio.CancelledError:
        if not self._shutdown_requested.is_set():
            self._member_status = MemberStatus.ERROR  # ← 仅非主动关闭时才设 ERROR
            raise
        # SHUTDOWN_REQUESTED → 正常关闭，不设 ERROR
```

**Go 问题**：

```go
// internal/agent_teams/agent/stream_controller.go
case <-ctx.Done():
    agentRef.SetMemberStatus(schema.MemberStatusError)  // ❌ 一律设为 ERROR
    // ❌ 缺少 SHUTDOWN_REQUESTED → CloseStream 路径
```

**修复方案**：

```go
case <-ctx.Done():
    if !agentRef.IsShutdownRequested() {
        agentRef.SetMemberStatus(schema.MemberStatusError)
    } else {
        agentRef.CloseStream()
        agentRef.SetMemberStatus(schema.MemberStatusIdle)
    }
```

**流程示例**：

```
Python: Leader 主动关闭 teammate
  → SHUTDOWN_REQUESTED = True
  → CancelledError 被捕获
  → 不设 ERROR，走 CloseStream → IDLE ✅

Go: Leader 主动关闭 teammate
  → ctx.Done() 触发
  → SetMemberStatus(ERROR) ❌ 误报错误
  → 无 CloseStream 调用，流不关闭
```

---

### S18: StreamController.runOneRound finally 块缺少 SHUTDOWN_REQUESTED → CloseStream 路径

**严重性**：严重 — Python 的 `run_one_round` 在 finally 块中检查 `_shutdown_requested`，如果为 True 则调用 `close_stream()` 并返回 SHUTDOWN 状态。Go 的 finally 块（defer）无此逻辑

**Python 样例**：

```python
# stream_controller.py — finally 块
finally:
    if self._shutdown_requested.is_set():
        await self.close_stream()
        self._member_status = MemberStatus.IDLE
        self._execution_status = ExecutionStatus.SHUTDOWN
```

**Go 问题**：

```go
// stream_controller.go — defer 块
defer func() {
    agentRef.SetMemberStatus(schema.MemberStatusIdle)
    agentRef.SetExecutionStatus(schema.ExecutionStatusCompleted)
    // ❌ 缺少 SHUTDOWN_REQUESTED 检查和 CloseStream 调用
}()
```

**修复方案**：

在 defer 中添加 SHUTDOWN_REQUESTED 分支：

```go
defer func() {
    if agentRef.IsShutdownRequested() {
        agentRef.CloseStream()
        agentRef.SetMemberStatus(schema.MemberStatusIdle)
        agentRef.SetExecutionStatus(schema.ExecutionStatusShutdown)
    } else {
        agentRef.SetMemberStatus(schema.MemberStatusIdle)
        agentRef.SetExecutionStatus(schema.ExecutionStatusCompleted)
    }
}()
```

---

### S19: StreamController.detectTaskFailed — 无 errorCode 的 task_failed 被静默忽略

**严重性**：严重 — Python 的 `_detect_task_failed` 对 `task_failed` 事件一视同仁，只要有 `task_failed` 就返回 True。Go 的 `detectTaskFailed` 在 `errorCode == nil` 时走成功路径，导致没有错误码的 task_failed 事件被忽略，Leader 无法感知 teammate 任务失败

**Python 样例**：

```python
# stream_controller.py
def _detect_task_failed(self, event):
    return event.get("type") == "task_failed"
    # ← 不检查 error_code，只要 type == "task_failed" 就是失败
```

**Go 问题**：

```go
// stream_controller.go
func detectTaskFailed(event map[string]any) bool {
    eventType, _ := event["type"].(string)
    if eventType != "task_failed" { return false }
    errorCode, _ := event["error_code"].(string)
    if errorCode == "" { return false }  // ❌ 无 errorCode 的 task_failed 被忽略
    return true
}
```

**修复方案**：

```go
func detectTaskFailed(event map[string]any) bool {
    eventType, _ := event["type"].(string)
    return eventType == "task_failed"  // 对齐 Python：只看 type
}
```

**流程示例**：

```
Python: teammate 返回 {"type": "task_failed"} (无 error_code)
  → _detect_task_failed → True → Leader 收到 FAILED 通知 ✅

Go: teammate 返回 {"type": "task_failed"} (无 error_code)
  → detectTaskFailed → errorCode=="" → False ❌
  → Leader 不知道 teammate 失败
```

---

### S20: StreamController.RemoveChunkObserver/AddChunkObserver 缺少互斥锁保护

**严重性**：严重 — `AddChunkObserver` 和 `RemoveChunkObserver` 对 `chunkObservers` 切片的读写没有加锁，并发调用时可能 panic（slice 并发读写）或丢失 observer

**Python 样例**：

```python
# stream_controller.py — Python 使用 asyncio（单线程事件循环），无需显式加锁
def add_chunk_observer(self, observer):
    if observer not in self._chunk_observers:
        self._chunk_observers.append(observer)

def remove_chunk_observer(self, observer):
    try:
        self._chunk_observers.remove(observer)
    except ValueError:
        pass
```

**Go 问题**：

```go
// stream_controller.go
func (sc *StreamController) AddChunkObserver(observer ChunkObserver) {
    sc.chunkObservers = append(sc.chunkObservers, observer)  // ❌ 无锁
}

func (sc *StreamController) RemoveChunkObserver(observer ChunkObserver) {
    for i, obs := range sc.chunkObservers {
        if obs == observer {
            sc.chunkObservers = append(sc.chunkObservers[:i], sc.chunkObservers[i+1:]...)
            return
        }
    }  // ❌ 无锁
}
```

**修复方案**：

为 `chunkObservers` 操作添加互斥锁：

```go
func (sc *StreamController) AddChunkObserver(observer ChunkObserver) {
    sc.observersMu.Lock()
    defer sc.observersMu.Unlock()
    sc.chunkObservers = append(sc.chunkObservers, observer)
}

func (sc *StreamController) RemoveChunkObserver(observer ChunkObserver) {
    sc.observersMu.Lock()
    defer sc.observersMu.Unlock()
    for i, obs := range sc.chunkObservers {
        if obs == observer {
            sc.chunkObservers = append(sc.chunkObservers[:i], sc.chunkObservers[i+1:]...)
            return
        }
    }
}
```

---

### S21: SpawnManager.CleanupTeammate 未调用 RemoveChunkObserver

**严重性**：严重 — Python `cleanup_teammate` 先 `remove_chunk_observer(forward)` 再 `handle.chunk_forward = None`，确保观察者从 StreamController 的列表中摘除。Go 只 `SetChunkForward(nil)` 但未调用 `RemoveChunkObserver`，观察者仍挂在列表中，teammate 的 goroutine 继续产生 chunk 时会回调已清理的 forwardCb

**Python 样例**：

```python
# openjiuwen/agent_teams/agent/spawn_manager.py
async def _cleanup_teammate(self, member_name):
    handle = self._handles.get(member_name)
    if handle is None:
        return
    ...
    forward = getattr(handle, "chunk_forward", None)
    if forward is not None:
        agent_ref = handle.agent_ref
        if agent_ref is not None:
            agent_ref.stream_controller.remove_chunk_observer(forward)  # ← 先摘除
    handle.chunk_forward = None  # ← 再清空引用
```

**Go 问题**：

```go
// internal/agent_teams/agent/spawn_manager.go
func (m *SpawnManager) CleanupTeammate(memberName string) {
    ...
    inproc := handle.(*InProcessSpawnHandle)
    inproc.SetChunkForward(nil)  // ❌ 只清空引用，未从 teammateSC 的观察者列表中摘除
    // 缺少: agentRef.StreamController().RemoveChunkObserver(forward)
}
```

**修复方案**：

```go
func (m *SpawnManager) CleanupTeammate(memberName string) {
    ...
    inproc := handle.(*InProcessSpawnHandle)
    forward := inproc.ChunkForward()
    if forward != nil {
        if agentRef := inproc.AgentRef(); agentRef != nil {
            agentRef.StreamController().RemoveChunkObserver(forward)  // ← 先摘除
        }
    }
    inproc.SetChunkForward(nil)  // ← 再清空引用
}
```

---

### S22: SpawnManager.ShutdownAllHandles 同样缺少 RemoveChunkObserver

**严重性**：严重 — 与 S21 同源，`ShutdownAllHandles` 也只做 `SetChunkForward(nil)` 未调用 `RemoveChunkObserver`

**Python 样例**：

```python
# Python 的 shutdown_all_handles 路由到 cleanup_teammate，自然包含 remove_chunk_observer
async def _shutdown_all_handles(self):
    for member_name in list(self._handles.keys()):
        await self._cleanup_teammate(member_name)  # ← 包含 RemoveChunkObserver
```

**Go 问题**：

```go
func (m *SpawnManager) ShutdownAllHandles() {
    ...
    inproc.SetChunkForward(nil)  // ❌ 缺少 RemoveChunkObserver
}
```

**修复方案**：

与 S21 一致，在 `ShutdownAllHandles` 中添加 `RemoveChunkObserver` 调用

---

### S23: SpawnManager.RestartTeammate 硬编码空 initialMessage 和 nil spawnCfg

**严重性**：严重 — Python 的 `restart_teammate` 从 DB 获取 `teammate.prompt` 作为 `initial_message`，并构建特定的 `spawn_config`（health_check_timeout=30, health_check_interval=50）。Go 硬编码空字符串和 nil，导致重启后的 teammate 收到默认消息 "Join the team..." 而非原始 prompt

**Python 样例**：

```python
# openjiuwen/agent_teams/agent/spawn_manager.py
async def _restart_teammate(self, member_name):
    teammate = await team_backend.get_member(member_name)
    initial_message = teammate.prompt if teammate else None
    spawn_config = SpawnConfig(health_check_timeout=30, health_check_interval=50)
    await self._spawn_teammate(ctx, initial_message=initial_message,
                                session=get_session_id() or None,
                                spawn_config=spawn_config)
```

**Go 问题**：

```go
// internal/agent_teams/agent/spawn_manager.go
func (m *SpawnManager) RestartTeammate(ctx context.Context, runtimeCtx *schema.RuntimeContext,
    memberName string) error {
    ...
    return m.SpawnTeammate(ctx, runtimeCtx, "", "", nil)  // ❌ 空消息、空 session、nil config
}
```

**修复方案**：

1. 从 DB 获取 teammate 的 prompt 作为 initialMessage
2. 构造带特殊健康检查配置的 SpawnConfig
3. 传入正确的 session ID

---

### S24: SpawnManager.RestartTeammate 失败后未更新 DB 状态为 ERROR

**严重性**：严重 — Python 在 restart 失败后更新 DB 中 member 状态为 ERROR，Go 只有 TODO 注释，DB 中 teammate 状态永远停在 RESTARTING

**Python 样例**：

```python
# spawn_manager.py
if team_backend:
    team_name = self._configurator.team_name
    if team_name:
        await team_backend.db.member.update_member_status(
            member_name, team_name, MemberStatus.ERROR.value)
return False
```

**Go 问题**：

```go
// spawn_manager.go
// TODO(#9.64): 更新 DB 状态为 ERROR  ← 未实现
return fmt.Errorf("restart_teammate failed: %w", err)
```

**修复方案**：

在 restart 失败路径中调用 DB 更新：

```go
if err != nil {
    if m.configurator.TeamBackend() != nil {
        m.configurator.TeamBackend().DB().Member().UpdateMemberStatus(
            memberName, m.configurator.TeamName(), schema.MemberStatusError)
    }
    return fmt.Errorf("restart_teammate failed: %w", err)
}
```

---

### S25: SpawnManager.OnTeammateUnhealthy 未标记 RESTARTING 状态

**严重性**：严重 — Python 的 `on_teammate_unhealthy` 先 `cleanup_teammate`，再标记 DB 为 `RESTARTING`，然后 `restart_teammate`。Go 跳过了 DB 状态标记，DB 状态与实际运行状态不一致

**Python 样例**：

```python
# spawn_manager.py
async def _on_teammate_unhealthy(self, member_name):
    team_logger.warning(...)
    await self._cleanup_teammate(member_name)
    team_backend = self._configurator.team_backend
    team_name = self._configurator.team_name
    if team_backend and team_name:
        await team_backend.db.member.update_member_status(
            member_name, team_name, MemberStatus.RESTARTING.value)
    await self._restart_teammate(member_name)
```

**Go 问题**：

```go
// spawn_manager.go
func (m *SpawnManager) OnTeammateUnhealthy(memberName string) {
    go func() {
        _ = m.RestartTeammate(ctx, runtimeCtx, memberName)
        // ❌ 缺少: 1) 先调 CleanupTeammate  2) 标记 DB 为 RESTARTING
    }()
}
```

**修复方案**：

```go
func (m *SpawnManager) OnTeammateUnhealthy(memberName string) {
    go func() {
        m.CleanupTeammate(memberName)
        if tb := m.configurator.TeamBackend(); tb != nil {
            tb.DB().Member().UpdateMemberStatus(
                memberName, m.configurator.TeamName(), schema.MemberStatusRestarting)
        }
        _ = m.RestartTeammate(ctx, runtimeCtx, memberName)
    }()
}
```

---

### S26: SpawnManager.spawnSubprocess 中 payload 未传入 SpawnAgent

**严重性**：严重 — Python `Runner.spawn_agent` 同时传入 `build_spawn_config(ctx)` 和 `build_spawn_payload(ctx, initial_message)`。Go 构建了 payload 但未传入，`_ = payload` 丢弃，subprocess 模式下 teammate 无法收到正确的初始化数据

**Python 样例**：

```python
# spawn_manager.py
await Runner.spawn_agent(
    build_spawn_config(ctx),
    build_spawn_payload(ctx, initial_message=initial_message),
    session=session,
    spawn_config=spawn_config,
)
```

**Go 问题**：

```go
// spawn_manager.go
payload := m.configurator.BuildSpawnPayload(runtimeCtx, initialMessage)
_ = payload  // ❌ 构建后丢弃
runner.SpawnAgent(ctx, config, "", spawnCfg)  // ← payload 未传入
```

**修复方案**：

将 payload 作为参数传入 `SpawnAgent`

---

### S27: SessionManager.BindSession 缺少步骤 4（DB 表创建）和步骤 5（Leader 配置持久化）

**严重性**：严重 — Python `bind_session` 在步骤 4 调用 `team_backend.db.create_cur_session_tables()`，步骤 5 调用 `recovery_manager.persist_leader_config(session)`。Go 两步均为 TODO，DB 表不创建、Leader 配置不持久化

**Python 样例**：

```python
# openjiuwen/agent_teams/agent/session_manager.py
# 步骤 4
team_backend = self._configurator.team_backend
if team_backend:
    await team_backend.db.create_cur_session_tables()
# 步骤 5
spec = self._configurator.spec
if spec and self._configurator.role == TeamRole.LEADER:
    self._recovery_manager.persist_leader_config(session)
```

**Go 问题**：

```go
// internal/agent_teams/agent/session_manager.go
// 步骤 4: TODO(#9.61)
// 步骤 5: TODO(#9.61)
```

**修复方案**：

在 9.61 回填时实现 DB 表创建和 Leader 配置持久化

---

### S28: SessionManager.ResumeForNewSession/RecoverForExistingSession 核心逻辑完全缺失

**严重性**：严重 — Python 有完整的会话切换逻辑（`collect_live_teammates_for_session_switch` → `bind_session` → `restart_for_session_switch`），Go 所有 RecoveryManager 调用都是 TODO，会话切换时 teammate 不会被重启

**Python 样例**：

```python
# session_manager.py
async def _resume_for_new_session(self, session):
    recoverable_members = await self._recovery_manager.collect_live_teammates_for_session_switch()
    await self._bind_session(session)
    if self._configurator.role != TeamRole.LEADER or not team_backend:
        return
    await self._recovery_manager.restart_for_session_switch(
        recoverable_members, cleanup_first=True)
```

**Go 问题**：

```go
// session_manager.go
func (m *SessionManager) ResumeForNewSession(...) {
    // TODO(#9.61): RecoveryManager 调用
    _ = m.BindSession(ctx, session)  // ← 仅 BindSession 有效
}
```

**修复方案**：

在 9.61 回填时实现 RecoveryManager 的全部调用链

---

## 一般问题 (G)

### G01: BrowserService 缺少 _is_retryable_transport_error 方法

**严重性**：一般 — Python 有 `_is_retryable_transport_error` 类方法（同时检查异常类名和消息文本），Go 只导出了 `IsRetryableTransportMessage`，缺少对 error 对象的类型检查

**Python 样例**：

```python
# service.py:491-494
@classmethod
def _is_retryable_transport_error(cls, exc: Exception) -> bool:
    name = type(exc).__name__.lower()
    text = str(exc).lower()
    return cls._is_retryable_transport_message(name) or cls._is_retryable_transport_message(text)
```

**Go 问题**：

```go
// service.go 中只有 IsRetryableTransportMessage(text string) bool
// ❌ 缺少 IsRetryableTransportError(err error) bool
```

**修复方案**：

添加 `IsRetryableTransportError(err error) bool`，同时检查错误类型名和消息文本

---

### G02: BrowserService 缺少 mcpCwd / screenshotsDir / artifactsDir / profileStore 等字段

**严重性**：一般 — Python `BrowserService.__init__` 初始化了 MCP 工作目录、截图目录、artifacts 目录、profile 存储等字段，Go 缺少这些字段

**Python 样例**：

```python
# service.py:144-155
self._mcp_cwd = self._resolve_mcp_cwd()
self._screenshots_dir = self._mcp_cwd / self._screenshot_subdir
self._artifacts_dir = self._mcp_cwd / self._artifacts_subdir
self._profile_store = BrowserProfileStore(self._resolve_profile_store_path())
self._profile_name = (os.getenv("BROWSER_PROFILE_NAME") or "jiuwenclaw").strip() or "jiuwenclaw"
self._driver_mode = self._resolve_driver_mode()
self._active_profile: Optional[BrowserProfile] = None
self._managed_driver: Optional[ManagedBrowserDriver] = None
self._registered_cdp_endpoint: str = ""
```

**Go 问题**：

```go
// service.go:21-58 — BrowserService 结构体中缺少上述字段
// 仅有: started, managedDriver(any), browserAgent, sessions, locksPerSession,
//       cancelStore, progressBySession, failureContextBySession, connectionHealthy, lastHeartbeatOK
```

**修复方案**：

在 `BrowserService` 结构体中添加缺失字段，并在 `NewBrowserService` 中初始化

---

### G03: BrowserService.RunTask 传输层错误判断不完整

**严重性**：一般 — Python 的 `run_task` 中 Exception 分支有更完整的传输错误判断逻辑：先检查 `str(exc)` 是否为空（某些传输错误只有类型名），再调用 `_is_retryable_transport_error`。Go 的判断简化了

**Python 样例**：

```python
# service.py:1443-1448
except Exception as exc:
    attempt_idx += 1
    last_error = str(exc) or repr(exc)
    if attempt_idx >= attempts:
        break
    # Restart before retry on known transport/session failures.
    if (not str(exc)) or self._is_retryable_transport_error(exc):
        try:
            await self._restart()
        except Exception as restart_exc:
            last_error = f"restart_failed: {restart_exc!r}"
            break
```

**Go 问题**：

```go
// service.go:513-518
if IsRetryableTransportMessage(lastError) {
    if attemptIdx < attempts {
        _ = s.restart(ctx)
        continue
    }
}
// ❌ 缺少 not str(exc) 分支（空错误消息时也尝试重启）
```

**修复方案**：

添加 `len(lastError) == 0` 的空错误消息分支判断

---

### G04: BrowserService 缺少 _resolve_existing_cdp_profile / _build_managed_profile 等方法

**严重性**：一般 — Python 有完整的 CDP 配置文件解析和 managed driver profile 构建（约120行代码），Go 完全缺失

**Python 样例**：

```python
# service.py:257-325
def _resolve_existing_cdp_profile(self) -> Optional[BrowserProfile]:
    candidates: list[BrowserProfile] = []
    for store_path in self._iter_profile_store_paths():
        store = BrowserProfileStore(store_path)
        selected = store.selected_profile()
        ...

def _build_managed_profile(self) -> BrowserProfile:
    host = (os.getenv("BROWSER_MANAGED_HOST") or "127.0.0.1").strip() or "127.0.0.1"
    port_raw = (os.getenv("BROWSER_MANAGED_PORT") or "9333").strip()
    ...
```

**Go 问题**：

Go 的 `BrowserService` 完全没有这些方法。这些方法标记为 ⤵️ 9.38-49 回填

**修复方案**：

在 9.38-49 回填时实现 CDP profile 管理方法

---

### G05: BrowserService 缺少 _observe_worker_tool_result (ContextVar observer)

**严重性**：一般 — Python 使用 `contextvars.ContextVar` 在 Worker Agent 执行期间传递 session_id/request_id，通过 `_observe_worker_tool_result` observer 回调自动更新进度。Go 没有等价机制

**Python 样例**：

```python
# service.py:33-40
_ctx_observer_session_id: contextvars.ContextVar[str] = contextvars.ContextVar(
    "playwright_runtime_observer_session_id", default="",
)
_ctx_observer_request_id: contextvars.ContextVar[str] = contextvars.ContextVar(
    "playwright_runtime_observer_request_id", default="",
)

# service.py:854-864
async def _observe_worker_tool_result(self, tool_name, tool_result):
    session_id = _ctx_observer_session_id.get().strip()
    request_id = _ctx_observer_request_id.get().strip()
    self._update_progress_from_tool_observation(...)
```

**Go 问题**：

Go 没有实现 ContextVar 等价机制，`RecordToolProgress` 需要手动传入 sessionID/requestID，无法在 Worker Agent 内部自动捕获

**修复方案**：

考虑使用 `context.Value` 或 goroutine-local storage 模拟 ContextVar 行为，或者调整 observer 回调签名显式传递 session_id/request_id

---

### G06: BrowserService.RunTask 缺少 RunTaskOnce 公开方法

**严重性**：一般 — Python 有 `run_task_once` 公开方法，Go 只有私有的 `runTaskOnceWithTimeout`，缺少公开的单次执行入口

**Python 样例**：

```python
# service.py:1163-1164
async def run_task_once(self, task, session_id, request_id):
    return await self._run_task_once(task=task, session_id=session_id, request_id=request_id)
```

**Go 问题**：

Go 没有公开的 `RunTaskOnce` 方法

**修复方案**：

添加 `RunTaskOnce(ctx, task, sessionID, requestID string)` 公开方法

---

### G07: BrowserService 缺少 build_browser_worker_agent 集成

**严重性**：一般 — Python `ensure_started` 调用 `build_browser_worker_agent` 构建带 observer 的 Worker Agent，Go 缺少此集成

**Python 样例**：

```python
# service.py:617-627
self._browser_agent = build_browser_worker_agent(
    provider=self.provider, api_key=self.api_key, api_base=self.api_base,
    model_name=self.model_name, mcp_cfg=self.mcp_cfg,
    max_steps=self.guardrails.max_steps,
    screenshot_subdir=self._screenshot_subdir,
    artifacts_subdir=self._artifacts_subdir,
    tool_result_observer=self._observe_worker_tool_result,
)
```

**Go 问题**：

Go 的 `EnsureStarted` 中 TODO 占位未调用 `bm.BuildBrowserWorkerAgent`

**修复方案**：

在 9.38-49 回填时调用 `bm.BuildBrowserWorkerAgent` 并注入 observer 回调

---

### G08: AgentConfigService 的 YAML 同步缺少 Python 的 reload 回调

**严重性**：一般 — Python `AgentConfigService` 在 YAML 文件变更时有 hot-reload 回调机制，Go 的实现需要确认是否完整

**Python 样例**：

```python
# jiuwenswarm/server/runtime/agent_config_service.py
# 使用 fsnotify 或类似机制监听 YAML 变更并重新加载
```

**Go 问题**：

需要确认 Go 的 `AgentConfigService` 是否实现了 YAML 文件监听和热重载

**修复方案**：

检查 Go 实现中 `SyncFromYAML`/`WatchYAMLChanges` 等方法是否完整

---

### G09: CodeAgentRail — AgentTool.createSubAgent 缺失 Mcps/Backend/PromptMode 字段传递

**严重性**：一般 — Python `AgentTool._create_sub_agent()` 传递完整字段（mcps, subagents, enable_async_subagent, prompt_mode, backend 等），Go 的 `createSubAgent` 缺失这些字段

**Python 样例**：

```python
# jiuwenswarm/server/runtime/agent_adapter/code_agent_rail.py L234-252
def _create_sub_agent(self, ...):
    return create_deep_agent(
        ...
        mcps=spec.mcps or [],
        subagents=None,
        enable_async_subagent=False,
        add_general_purpose_agent=False,
        prompt_mode=prompt_mode,
        backend=backend,
        language=language,
    )
```

**Go 问题**：

```go
// internal/swarm/server/adapter/agent_tool.go L304-320
// CreateDeepAgentParams 缺失 Mcps、Backend、PromptMode、EnableTaskPlanning 字段
```

**修复方案**：

在 `CreateDeepAgentParams` 中补充缺失字段，并在 `createSubAgent` 中传递

---

### G10: AgentConfigService — ListAvailableTools 的 InternalName 全部等于 Name

**严重性**：一般 — Python `list_available_tools()` 从 `_TOOL_DISPLAY_NAMES` 映射获取 internal_name（如 `"read_file"`），Go 的 `InternalName` 全部等于 `Name`（如 `"Read"`），前端依赖 internal_name 调用工具时可能找不到

**Python 样例**：

```python
# jiuwenswarm/server/runtime/agent_config_service.py
_TOOL_DISPLAY_NAMES = {
    "read_file": "Read",
    "write_file": "Write",
    ...
}
# list_available_tools 返回 internal_name="read_file", name="Read"
```

**Go 问题**：

```go
// internal/swarm/server/runtime/agent_config.go L378-402
InternalName: "Read",  // ❌ 应为 "read_file"
Name:        "Read",
```

**修复方案**：

添加 `_TOOL_DISPLAY_NAMES` 等价映射表，InternalName 使用内部工具名

---

### G11: CreateResearchAgent 缺失 subagents 参数传递

**严重性**：一般 — Python `create_research_agent` 接受 `subagents` 参数，Go 的 `CreateResearchAgent` 未传递此能力

**Python 样例**：

```python
# openjiuwen/harness/subagents/research_agent.py L104
def create_research_agent(model, *, subagents=None, ...):
    return create_deep_agent(..., subagents=subagents, ...)
```

**Go 问题**：

```go
// CreateDeepAgentParams 中 Subagents: nil，未从 params 传递
```

**修复方案**：

在 `CreateResearchAgent` 的 `CreateDeepAgentParams` 中传递 `Subagents` 字段

---

### G12: loadCustomSubagents 缺失 factory_kwargs 设置

**严重性**：一般 — Python `_load_custom_subagents()` 设置 `custom_spec.factory_kwargs = {"auto_create_workspace": False}`，Go 的 `loadCustomSubagents` 未设置，自定义 Agent 创建时 workspace 自动创建行为可能与 Python 不一致

**Python 样例**：

```python
# jiuwenswarm/server/runtime/agent_adapter/interface_deep.py L6044
custom_spec.factory_kwargs = {"auto_create_workspace": False}
```

**Go 问题**：

```go
// internal/swarm/server/adapter/deep_adapter_config.go L178-209
// loadCustomSubagents 中未设置 FactoryKwargs
```

**修复方案**：

在 `loadCustomSubagents` 中设置 `FactoryKwargs: map[string]any{"auto_create_workspace": false}`

---

### G13: BrowserAgentRuntime 缺少 ensure_execute_signature_compat 及工具超时包装

**严重性**：一般 — Python `ensure_execute_signature_compat` 包装 `ability_manager.execute`，添加工具执行超时（`anyio.fail_after(tool_timeout_s)`）和 tool_result_observer 回调。Go 完全缺失

**Python 样例**：

```python
# openjiuwen/harness/tools/browser_move/playwright_runtime/agents.py L244-297
def ensure_execute_signature_compat(agent, ..., tool_result_observer=None):
    original_execute = agent.ability_manager.execute
    async def patched_execute(*args, **kwargs):
        async with anyio.fail_after(tool_timeout_s):
            result = await original_execute(*args, **kwargs)
        if tool_result_observer and result:
            await tool_result_observer(tool_name, result)
        return result
    agent.ability_manager.execute = patched_execute
```

**Go 问题**：

Go 的 Worker Agent 无超时保护，且 observer 无法在内部自动触发

**修复方案**：

9.38-49 回填时实现 `EnsureExecuteSignatureCompat`，包装 ability_manager 的 Execute 方法

---

### G14: BrowserAgentRuntime 缺少 _build_dialogue_compressor_config

**严重性**：一般 — Python `build_browser_worker_agent` 调用 `_build_dialogue_compressor_config` 为 worker agent 创建对话压缩配置，Go 缺失，长对话无法压缩

**Python 样例**：

```python
# openjiuwen/harness/tools/browser_move/playwright_runtime/agents.py L28-49
def _build_dialogue_compressor_config(max_steps):
    return DialogueCompressorConfig(...)
```

**Go 问题**：

Go 的 `BuildBrowserWorkerAgent` 中无对话压缩配置

**修复方案**：

9.38-49 回填时在 Worker Agent 配置中添加对话压缩

---

### G15: ResolveModelSettings 不验证 provider 有效性

**严重性**：一般 — Python `resolve_model_settings()` 当 `provider_mode` 不在 `SUPPORTED_MODEL_PROVIDERS` 中时 `raise ValueError`，Go 静默降级为空 provider

**Python 样例**：

```python
# openjiuwen/harness/tools/browser_move/utils/env.py L157-163
def resolve_model_settings():
    if provider_mode not in SUPPORTED_MODEL_PROVIDERS:
        raise ValueError(f"Unsupported model provider: {provider_mode}")
```

**Go 问题**：

```go
// internal/agentcore/harness/tools/browser_move/env.go L201-205
// 不验证 provider，静默降级
```

**修复方案**：

添加 provider 校验逻辑，不支持时返回错误

---

### G16: ParseCommandArgs 使用简化版分割而非 shlex

**严重性**：一般 — Python 使用 `shlex.split(value)` 做完整的 shell 风格分割，Go 使用自定义 `splitArgs` 简化版，不支持转义引号等 shell 语法

**Python 样例**：

```python
# openjiuwen/harness/tools/browser_move/utils/env.py L49-60
def parse_command_args(value: str) -> list[str]:
    return shlex.split(value)
```

**Go 问题**：

Go 的 `ParseCommandArgs` 使用自定义 `splitArgs`，对于复杂参数（含引号嵌套）可能解析错误

**修复方案**：

引入 Go 版 shlex 库或完善 `splitArgs` 支持转义引号

---

### G17: StreamController.executeRound 缺少 TimeoutError → TIMED_OUT 状态路径

**严重性**：一般 — Python 的 `execute_round` 捕获 `TimeoutError` 并设置 `ExecutionStatus.TIMED_OUT`，Go 缺少此分支

**Python 样例**：

```python
# stream_controller.py
async def _execute_round(self, ...):
    try:
        ...
    except TimeoutError:
        self._execution_status = ExecutionStatus.TIMED_OUT
```

**Go 问题**：

```go
// stream_controller.go — 无 TimeoutError → TIMED_OUT 分支
```

**修复方案**：

在 `executeRound` 中添加超时检测，设置 `ExecutionStatusTimedOut`

---

### G18: StreamController.executeRound 外部取消走 FAILED 而非 CANCELLED

**严重性**：一般 — Python 区分外部取消（`CancelledError`）走 CANCELLED 状态、超时走 TIMED_OUT、其他异常走 FAILED。Go 的外部取消走 FAILED，不区分取消和失败

**Python 样例**：

```python
# stream_controller.py
except asyncio.CancelledError:
    self._execution_status = ExecutionStatus.CANCELLED
except TimeoutError:
    self._execution_status = ExecutionStatus.TIMED_OUT
except Exception:
    self._execution_status = ExecutionStatus.FAILED
```

**Go 问题**：

Go 统一走 FAILED，缺少 CANCELLED/TIMED_OUT 状态区分

**修复方案**：

在 `executeRound` 中通过 `ctx.Err()` 区分取消和失败，设置对应状态

---

### G19: StreamController.streamOneRound 中 sessionID/teamSession 硬编码空字符串

**严重性**：一般 — Python 的 `stream_one_round` 传入 `session_id` 和 `team_session` 参数，Go 的 `streamOneRound` 硬编码空字符串

**Python 样例**：

```python
# stream_controller.py
async def _stream_one_round(self, ...):
    result = await runner.run_agent(
        ...,
        session_id=session_id,
        team_session=team_session,
    )
```

**Go 问题**：

```go
// stream_controller.go
sessionID := ""    // ❌ 硬编码空
teamSession := ""  // ❌ 硬编码空
```

**修复方案**：

从 agentRef 或 context 中获取实际的 sessionID 和 teamSession 值

---

### G20: StreamController.wireInprocessChunkForward 非阻塞写入在队列满时丢弃 chunk

**严重性**：一般 — Python 的 `wire_inprocess_chunk_forward` 使用 `put_nowait` 在队列满时抛出 `QueueFull` 异常（被上层捕获处理），Go 使用 `select { case ch <- chunk: default: }` 静默丢弃，可能丢失关键 chunk

**Python 样例**：

```python
# stream_controller.py
def _on_chunk(self, chunk, *, forward_fn, **kwargs):
    forward_fn(chunk)  # QueueFull 异常会传播
```

**Go 问题**：

```go
// stream_controller.go
select {
case ch <- chunk:
default:
    // ❌ 队列满时静默丢弃，无日志
}
```

**修复方案**：

在丢弃时至少记录警告日志，或使用带阻塞的写入 + 超时

---

### G21: SpawnManager.CleanupTeammate 跳过了 StopHealthCheck

**严重性**：一般 — Python 的 `cleanup_teammate` 先调用 `await handle.stop_health_check()` 再 `force_kill()`，Go 直接 `ForceKill()` 跳过了 `StopHealthCheck()`

**Python 样例**：

```python
# spawn_manager.py
async def _cleanup_teammate(self, member_name):
    handle = self._handles.get(member_name)
    ...
    await handle.stop_health_check()
    if handle.is_alive:
        await handle.force_kill()
```

**Go 问题**：

```go
// spawn_manager.go
func (m *SpawnManager) CleanupTeammate(memberName string) {
    ...
    _ = handle.ForceKill()  // ❌ 跳过 StopHealthCheck()
}
```

**修复方案**：

在 `ForceKill` 前调用 `handle.StopHealthCheck()`（对 InProcessSpawnHandle 是 no-op，对 subprocess handle 可防止健康检查 goroutine 泄漏）

---

### G22: SpawnManager.CleanupTeammate 未尝试优雅关闭

**严重性**：一般 — Python 的 `force_kill` 内部也会先 `stop_health_check()`，且有些 handle 支持 `shutdown()` 优雅关闭。Go 只做 `ForceKill()`，丢失了优雅关闭语义

**Python 样例**：

```python
# spawn_manager.py — SpawnedProcessHandle.force_kill
async def force_kill(self):
    await self.stop_health_check()
    ...
```

**Go 问题**：

Go 的 `CleanupTeammate` 直接 `ForceKill()`，未先尝试 `Shutdown()` 优雅关闭

**修复方案**：

添加先 `Shutdown()` 再 `ForceKill()` 的两步关闭逻辑，设超时保护

---

### G23: SpawnManager.CancelRecoveryTasks 不等待任务完成

**严重性**：一般 — Python 的 `cancel_recovery_tasks` 对每个 task 调用 `cancel()` 后 `await task` 等待完成。Go 的 `CancelRecoveryTasks` 只调用 cancel 不等待，恢复任务可能仍在运行

**Python 样例**：

```python
# spawn_manager.py
async def _cancel_recovery_tasks(self):
    for task in list(self._recovery_tasks):
        task.cancel()
    for task in list(self._recovery_tasks):
        try:
            await task
        except asyncio.CancelledError:
            pass
```

**Go 问题**：

```go
// spawn_manager.go
func (m *SpawnManager) CancelRecoveryTasks() {
    m.recoveryMu.Lock()
    defer m.recoveryMu.Unlock()
    for _, cancel := range m.recoveryCancel {
        cancel()  // ❌ 只调 cancel，不等待 goroutine 退出
    }
    m.recoveryCancel = make(map[string]context.CancelFunc)
}
```

**修复方案**：

使用 `sync.WaitGroup` 或 channel 等待所有恢复 goroutine 退出

---

### G24: SpawnManager.InProcessSpawn goroutine 缺少 panic recovery

**严重性**：一般 — Python 的 `_run()` 中 `except Exception: logger.error(..., exc_info=True); raise`，异常会传播到 asyncio.Task。Go 的 goroutine 只有 `logger.Info` 退出日志，缺少 panic recovery 和 error 级别日志

**Python 样例**：

```python
# spawn_manager.py — InProcessSpawn._run
except Exception:
    team_logger.error(..., exc_info=True)
    raise
```

**Go 问题**：

```go
// spawn_manager.go — goroutine 中
// ⤵️ 预留：TeamRunner（9.85）实现后回填
// 当前只有 logger.Info 退出日志
```

**修复方案**：

回填时添加 `defer func() { if r := recover(); r != nil { logger.Error(...) } }()` 和 error 级别日志

---

### G25: StreamController — CleanupTeammate 缺少 RemoveChunkObserver 清理（与 S20 交叉）

**严重性**：一般 — 当 `CleanupTeammate` 不调用 `RemoveChunkObserver`（S21），且 `RemoveChunkObserver` 本身也无锁保护（S20），两重问题叠加：观察者泄漏 + 并发 panic

**修复方案**：

先修复 S20（加锁），再修复 S21（调用 RemoveChunkObserver）

---

## 提示问题 (T)

### T01: BrowserService.managedDriver 类型为 any

**严重性**：提示 — `managedDriver` 字段类型为 `any`，应使用具体类型 `*ManagedBrowserDriver`（标记 ⤵️ 9.38-49 回填）

**Go 问题**：

```go
// service.go:38
managedDriver any  // ⤵️ 9.38-49 回填
```

**修复方案**：

9.38-49 回填时改为具体类型

---

### T02: BrowserService.NormalizeScreenshotValue 中 file:// 前缀处理

**严重性**：提示 — Python 对 `file://` 前缀做了 `raw[7:]` 剥离，Go 也有类似逻辑但放在 TODO 后面

**Python 样例**：

```python
# service.py:456-457
local_path_str = raw[7:] if lowered.startswith("file://") else raw
```

**Go 问题**：

Go 代码在 TODO 之前没有处理 `file://` 前缀。等 9.38-49 回填时一并实现

---

### T03: BrowserService 缺少 _trim_text 静态方法导出

**严重性**：提示 — Python 的 `_trim_text` 是静态方法，Go 导出为 `TrimText` 但命名与项目规范（中文注释）不一致

**修复方案**：

确认 `trimText` 非导出函数是否足够，如需外部使用则导出

---

### T04: BrowserService 缺少 artifacts_subdir 属性

**严重性**：提示 — Python 有 `artifacts_subdir` 属性，Go 没有对应的导出方法

**Python 样例**：

```python
# service.py:171-173
@property
def artifacts_subdir(self) -> str:
    return self._artifacts_subdir
```

**Go 问题**：

Go 的 `BrowserService` 没有 `ArtifactsSubdir()` 方法

**修复方案**：

添加 `ArtifactsSubdir() string` 方法，返回 `s.artifactsSubdir`

---

### T05: BrowserService 缺少 BrowserAgent 属性 setter

**严重性**：提示 — Python 有 `browser_agent` 的 getter + setter，Go 只有字段访问

**Python 样例**：

```python
# service.py:163-168
@property
def browser_agent(self) -> Optional[ReActAgent]:
    return self._browser_agent

@browser_agent.setter
def browser_agent(self, value: Optional[ReActAgent]) -> None:
    self._browser_agent = value
```

**Go 问题**：

Go 直接暴露 `browserAgent` 字段，缺少线程安全的 getter/setter

**修复方案**：

添加 `GetBrowserAgent()`/`SetBrowserAgent(agent)` 方法，使用读写锁保护

---

### T06: ResearchAgent EN 描述换行符语义一致性

**严重性**：提示 — Python 三引号字符串内换行是真实换行符，Go `\n` 在双引号字符串中也是真实换行符，语义一致

**无需修复**，仅记录观察

---

### T07: BrowserService 缺少 profile 相关方法

**严重性**：提示 — Python 有 `_resolve_profile_store_path`、`_legacy_profile_store_path`、`_iter_profile_store_paths` 等方法，Go 完全缺失（标记 ⤵️ 9.38-49）

**修复方案**：

9.38-49 回填时实现 profile 存储路径解析方法

---

### T08: BrowserService 缺少 _inject_cdp_endpoint 方法

**严重性**：提示 — Python 有 `_inject_cdp_endpoint` 方法将 CDP endpoint 注入 MCP 配置，Go 缺失

**Python 样例**：

```python
# service.py:327-334
def _inject_cdp_endpoint(self, endpoint):
    params = dict(getattr(self.mcp_cfg, "params", {}) or {})
    env_map = dict(params.get("env", {}) or {})
    env_map["PLAYWRIGHT_MCP_CDP_ENDPOINT"] = endpoint
    env_map.setdefault("PLAYWRIGHT_MCP_BROWSER", "chrome")
    env_map.pop("PLAYWRIGHT_MCP_DEVICE", None)
    params["env"] = env_map
    self.mcp_cfg.params = params
```

**修复方案**：

9.38-49 回填时实现 `InjectCDPEndpoint` 方法

---

### T09: SessionManager.RecoveryManager 类型为 any，阻碍后续回填

**严重性**：提示 — Python `RecoveryManager` 是具体类型，有完整方法。Go 的 `recoveryManager any` 类型无法调用任何方法，即使 TODO 逻辑也无法实现

**Python 样例**：

```python
# session_manager.py
self._recovery_manager = RecoveryManager(configurator=self._configurator)
# 可直接调用: self._recovery_manager.persist_leader_config(session)
```

**Go 问题**：

```go
// session_manager.go
recoveryManager any  // ❌ 无法调用任何方法
```

**修复方案**：

定义 `RecoveryManager` 接口或具体类型，替换 `any`

---

### T10: SessionManager — session 参数类型仍为 any

**严重性**：提示 — `session any` 参数未定义 `AgentTeamSession` 接口，`extractSessionID` 使用类型断言，不具备类型安全

**Go 问题**：

```go
// session_manager.go:102
session any, // TODO(#9.59): AgentTeamSession 接口
// session_manager.go:242
// TODO(#9.59): 定义 AgentTeamSession 接口后替换为接口方法调用
```

**修复方案**：

在 9.59 回填时定义 `AgentTeamSession` 接口并替换 `any`

---

### T11: StreamController.wireInprocessChunkForward 缺少 Observer 模式日志

**严重性**：提示 — Python 在 wire 时记录 observer 注册日志，Go 无对应日志，调试困难

**修复方案**：

在 `wireInprocessChunkForward` 中添加 Debug 级别日志记录 observer 注册

---

## 问题统计

| 严重程度 | 数量 | 编号 |
|----------|------|------|
| 严重 (S) | 28 | S01-S28 |
| 一般 (G) | 25 | G01-G25 |
| 提示 (T) | 11 | T01-T11 |
| **合计** | **64** | |

### 按章节分布

| 章节 | 严重 | 一般 | 提示 | 主要问题 |
|------|------|------|------|---------|
| 9.26 BrowserAgent | 6 | 8 | 8 | 核心方法空实现、inflight task 缺失、截图转换缺失 |
| 9.60 StreamController | 4 | 4 | 1 | CancelledError 误处理、task_failed 静默忽略、observer 无锁 |
| 9.58 SpawnManager | 6 | 4 | 0 | RemoveChunkObserver 缺失、RestartTeammate 硬编码、DB 状态不更新 |
| 9.59 SessionManager | 2 | 0 | 2 | DB 表创建/Leader 持久化缺失、会话切换逻辑空 |
| 9.25 ResearchAgent | 1 | 1 | 1 | 缺少 SysOperationRail 默认注入 |
| 10.3.7 CodeAgentRail | 1 | 2 | 0 | tools 计算结果丢弃、缺失字段传递 |
| 10.3.13 AgentConfigService | 1 | 1 | 0 | 目录名不兼容、InternalName 映射缺失 |
| 9.72e BaseOptimizer | 2 | 0 | 0 | DefaultTargets fallback 缺失、模板方法缺失 |
| 9.70c Updater Protocol | 1 | 0 | 0 | 多候选集返回能力丢失 |
| 其他 (Evolving/env) | 4 | 5 | 0 | Evolving 系统逐步逻辑缺失 |

## 严重问题修复优先级

| 优先级 | 编号 | 问题 | 影响范围 |
|--------|------|------|---------|
| P0 | S03 | BrowserService 核心方法空实现 | BrowserAgent 完全不可用 |
| P0 | S02 | 缺少 inflight task 追踪 | 取消操作无效 |
| P0 | S06 | 截图 data URL 转换缺失 | 多模态 API 无法识别本地截图 |
| P0 | S19 | detectTaskFailed 忽略无 errorCode 的 task_failed | Leader 无法感知 teammate 失败 |
| P0 | S20 | RemoveChunkObserver/AddChunkObserver 无锁 | 并发 panic |
| P1 | S01 | ResearchAgent 缺少 SysOperationRail | 无系统操作护栏 |
| P1 | S05 | Shutdown 未停止 MCP/Runner | 资源泄漏 |
| P1 | S07 | RunTask 缺少 inflight 注册 | 取消操作无效 |
| P1 | S08 | 重启失败缺少 break 保护 | 无限重试 |
| P1 | S17 | runOneRound CancelledError 误设 ERROR | 正常关闭被误报错误 |
| P1 | S18 | runOneRound finally 缺 SHUTDOWN 路径 | 流不关闭 |
| P1 | S21 | CleanupTeammate 未 RemoveChunkObserver | 观察者泄漏 |
| P1 | S22 | ShutdownAllHandles 未 RemoveChunkObserver | 观察者泄漏 |
| P1 | S23 | RestartTeammate 硬编码空消息和 nil config | 重启后行为不一致 |
| P1 | S26 | spawnSubprocess payload 未传入 | 子进程无法初始化 |
| P2 | S04 | 缺少心跳检测 | 无法检测连接断开 |
| P2 | S09 | agentDefToSubagentConfig tools 丢弃 | 子 Agent 工具集不正确 |
| P2 | S10 | 目录名 .uapclaw vs .jiuwenswarm | Python 版数据不兼容 |
| P2 | S11 | BuildBrowserWorkerAgent 未实现 | Worker Agent 永远无法创建 |
| P2 | S12 | BrowserAgentRuntime 关键初始化未完成 | Runtime tools 无法注册 |
| P2 | S14 | BaseOptimizer.Bind 缺 DefaultTargets fallback | Bind 结果始终为 0 |
| P2 | S15 | BaseOptimizer 缺 backward/step 模板方法 | 子类重复实现易遗漏 |
| P2 | S16 | Updater Protocol 丢失多候选集能力 | 多维归因场景不可用 |
| P2 | S24 | RestartTeammate 失败未更新 DB 为 ERROR | DB 状态永远停 RESTARTING |
| P2 | S25 | OnTeammateUnhealthy 未标记 RESTARTING | DB 与运行状态不一致 |
| P2 | S27 | BindSession 缺 DB 表创建和 Leader 持久化 | 跨会话恢复不可能 |
| P2 | S28 | ResumeForNewSession/RecoverForExistingSession 空实现 | 会话切换时 teammate 不重启 |

## ⤵️ 占位代码确认清单

以下是审查范围内确认未实现的 ⤵️ 占位代码，需在对应章节回填时完成：

| 文件 | 占位内容 | 目标章节 |
|------|---------|---------|
| `browser_move/service.go` | EnsureRuntimeReady/EnsureStarted/runTaskOnceWithTimeout 空实现 | 9.38-49 |
| `browser_move/agents.go` | BuildBrowserWorkerAgent 始终返回错误 | 9.38-49 |
| `browser_move/runtime.go` | codeExecutor 初始化 + RegisterBuiltinActions | 9.38-49 |
| `browser_move/service.go` | NormalizeScreenshotValue 本地路径转换 | 9.38-49 |
| `browser_move/service.go` | 心跳循环 + 连接检测 | 9.38-49 |
| `browser_move/service.go` | restart 方法空实现 | 9.38-49 |
| `spawn/inprocess_spawn.go` | TeamRunner 调用 | 9.85 |
| `spawn/shared_resources.go` | TeamRuntime/TeamDatabase/Messager 全为 any+nil | 9.85+ |
| `spawn/shared_resources.go` | GetSharedRuntime/GetSharedDB 返回 nil | 9.85+ |
| `spawn/shared_resources.go` | CleanupInprocessBus 未调用 | 9.85+ |
| `agent/spawn_manager.go` | BuildContextFromDB 返回空结构体 | 9.64 |
| `agent/spawn_manager.go` | PublishRestartEvent no-op | 9.64 |
| `agent/spawn_manager.go` | payload 传入 SpawnAgent | 9.64 |
| `agent/session_manager.go` | AgentTeamSession 接口未定义 | 9.59 |
| `agent/session_manager.go` | DB 表创建 + Leader 配置持久化 | 9.61 |
| `agent/session_manager.go` | RecoveryManager 全部调用 | 9.61 |

## 建议后续行动

1. **P0 问题**（S03/S02/S06/S19/S20）：核心功能缺失或数据丢失风险，必须优先修复
   - S03/S02/S06：标记为 ⤵️ 9.38-49 的回填项，应在 Harness 工具集章节实现时一并完成
   - S19：简单修复，可立即处理
   - S20：简单修复，可立即处理
2. **P1 问题**（S01/S05/S07/S08/S17/S18/S21-S23/S26）：功能逻辑错误，可在当前迭代中修复
   - S17/S18：StreamController 状态机修复
   - S21/S22：SpawnManager observer 泄漏修复（需 S20 先完成）
   - S23/S26：SpawnManager 参数传递修复
3. **P2 问题**（S04/S09-S16/S24-S28）：功能缺失但影响范围有限，可排入后续迭代
4. 所有标记 ⤵️ 的代码需在 IMPLEMENTATION_PLAN.md 中精确追踪回填进度
