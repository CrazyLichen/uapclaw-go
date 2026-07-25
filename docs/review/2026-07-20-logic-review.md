# 代码逻辑审查报告 2026-07-20

> 审查范围：48小时内提交的代码（107 个 commit），覆盖章节 9.25-9.30, 9.58-9.60, 9.70c-9.77, 10.3.7-10.3.13
>
> 审查方法：逐文件对照 Python 参考项目，检查方法签名/步骤一致性、待回填占位代码、逻辑差异
>
> 问题分类标准：
> - **严重**：功能逻辑缺陷，影响运行时行为或数据正确性
> - **一般**：接口不一致、代码风格偏差，不影响核心功能但有隐患
> - **提示**：日志、注释、命名等不影响流程的改进建议

---

## 问题汇总

| # | 级别 | 章节 | 问题标题 |
|---|------|------|----------|
| 1 | 严重 | 9.29 | 缺少 CreateVerificationAgent 工厂函数 |
| 2 | 严重 | 9.26 | BrowserAgent settings 传递断裂（Build + Create 两层） |
| 3 | 严重 | 9.30 | BuildExploreAgentConfig 缺少默认 Rails（SysOperationRail(read_only=True)） |
| 4 | 严重 | 9.28 | BuildPlanAgentConfig 缺少默认 Rails（SysOperationRail()） |
| 5 | 严重 | 9.58 | SpawnManager.CleanupTeammate 缺少 RemoveChunkObserver 和 StopHealthCheck |
| 6 | 严重 | 9.58 | SpawnManager.ShutdownAllHandles 缺少 RemoveChunkObserver 和 StopHealthCheck |
| 7 | 严重 | 9.59b | Runtime Manager 核心5个生命周期方法全部为 stub |
| 8 | 严重 | 9.59b | Runtime 层缺少 dispatch.py 调度决策实现 |
| 9 | 严重 | 9.60 | StreamController.runOneRound 取消/异常路径与 Python 不一致 |
| 10 | 严重 | 9.60 | StreamController.executeRound 中 CancelledError 不传播 |
| 11 | 严重 | 9.72b | ToolOptimizerBase.Step()/Bind() 签名与 BaseOptimizer 接口不匹配 |
| 12 | 严重 | 9.77 | ToolCallDetail.CallResult 类型过度限制（map[string]any 应为 any） |
| 13 | 严重 | 10.3.12 | AgentManager.GetAgent 未处理 ACP 通道配置合并 |
| 14 | 严重 | 10.3.12 | AgentManager.Initialize 完全为空实现 |
| 15 | 严重 | 10.3.12 | AgentManager.ReloadAgentsConfig envOverrides nil 时不更新 latestEnvOverrides |
| 16 | 严重 | 10.3.13 | AgentConfigService.userAgentsDir 使用 UserHomeDir 而非 WorkspaceDir |
| 17 | 严重 | 10.3.7 | agentDefToSubagentConfig 未设置 Tools 字段到 SubAgentConfig |
| 18 | 一般 | 9.26 | BuildBrowserRuntimeTools 缺少 language 参数 |
| 19 | 一般 | 9.30/9.28 | CreateExploreAgent/CreatePlanAgent 默认 Rail 用 WithReadOnly(true) 与 Python create 路径不一致 |
| 20 | 一般 | 9.58 | InProcessSpawnHandle.Shutdown 对已关闭 handle 返回 false + error（应为幂等 true） |
| 21 | 一般 | 9.58 | InProcessSpawnHandle.WaitForCompletion 不区分正常/异常退出 |
| 22 | 一般 | 9.59 | SessionManager.BindSession 缺少类型检查，步骤4-5为 TODO |
| 23 | 一般 | 9.59 | SessionManager.Resume/Recover 核心逻辑为 TODO |
| 24 | 一般 | 9.59b | Interaction 层 UserInbox/HumanAgentInbox/DeliverDirect 为 stub |
| 25 | 一般 | 9.59b | Runtime 层缺少 metadata.py 对应实现 |
| 26 | 一般 | 9.60 | StreamController 缺少 SHUTDOWN_REQUESTED 检查 |
| 27 | 一般 | 9.60 | StreamController.streamOneRun 中 sessionID 和 teamSession 未传递给 RunStreaming |
| 28 | 一般 | 9.72e | BaseOptimizerMixin.Bind() 未使用 DefaultTargets 作为 fallback |
| 29 | 一般 | 9.72e | TextualParameter.Gradients 类型限制为 string，与 Python Any 不一致 |
| 30 | 一般 | 9.73 | ConversationSignalDetector.Detect() 不支持 Trajectory 输入 |
| 31 | 一般 | 9.77 | Trajectory 缺少 builder/extractor/store/registry/aggregator 子模块 |
| 32 | 一般 | 10.3.12 | AgentManager.CreateSession 未处理 ACP 通道 session ID 生成 |
| 33 | 一般 | 10.3.13 | projectAgentsDir/localAgentsDir 使用 .uapclaw（品牌重命名确认） |
| 34 | 一般 | 10.3.13 | ListAvailableTools 中 groups 按字母排序与 Python 插入顺序不一致 |
| 35 | 一般 | 10.3.7 | AgentTool.Invoke 缺少 session 校验 |
| 36 | 提示 | 9.29 | VerificationAgent 描述 fallback 语言应为 "en" 而非 "cn" |
| 37 | 提示 | 9.58 | InProcessSpawn goroutine 核心逻辑为空（⤵️ 9.85） |
| 38 | 提示 | 9.58 | SharedResources.GetSharedDB 完全为空（⤵️ 9.64） |
| 39 | 提示 | 9.59 | SessionManager.recoveryManager 字段为 any 类型 |
| 40 | 提示 | 9.72e | LLMResilience 中 defer recover 与 context.WithTimeout 的交互 |
| 41 | 提示 | 9.73 | DetectUserIntent 中 is_feedback 不存在时的 fallback 行为与 Python 不一致 |
| 42 | 提示 | 9.60 | StreamController.logRoundPanic 不记录完整堆栈 |
| 43 | 提示 | 9.60 | StreamController 日志组件使用 ComponentCommon 而非 ComponentAgentCore |
| 44 | 提示 | 10.3.12 | createAgent 中 normalizeProjectDir 硬编码空字符串（代码意图不清） |

---

## 严重问题详细说明

### 问题 1: 缺少 CreateVerificationAgent 工厂函数

- **章节**：9.29
- **Python 参考代码**：
  ```python
  # verification_agent.py:318-363
  def create_verification_agent(
      model: Model, *, card=..., system_prompt=..., tools=..., mcps=...,
      subagents=..., rails=..., max_iterations=40, ..., **config_kwargs,
  ) -> DeepAgent:
      resolved_language = resolve_language(language)
      return create_deep_agent(
          model=model,
          card=card or AgentCard(name="verification_agent", description=...),
          system_prompt=system_prompt or (...),
          tools=tools, mcps=mcps, subagents=subagents,
          rails=rails if rails is not None else [SysOperationRail(), VerificationRail()],
          enable_task_loop=enable_task_loop, max_iterations=max_iterations,
          workspace=workspace, skills=skills, backend=backend,
          sys_operation=sys_operation, language=resolved_language,
          prompt_mode=prompt_mode, **config_kwargs,
      )
  ```
- **Go 问题代码**：
  不存在 `verification_agent_factory.go`。`deep_agent.go` 的 `createSubagentFromFactory` switch 中没有 `"verification_agent"` case。
- **问题描述**：Go 只有 `BuildVerificationAgentConfig`（配置层），缺少 `CreateVerificationAgent`（运行时创建层）。通过 `factory_name` 路径无法触发创建 verification_agent。当 adapter 通过 `factory_name` 路径创建子代理时，verification_agent 会走到默认路径，不注入 `SysOperationRail() + VerificationRail()` 默认 Rails。
- **修复方案**：创建 `internal/agentcore/harness/verification_agent_factory.go`，实现 `CreateVerificationAgent` 函数，参照 `research_agent_factory.go` 模式，默认注入 `[SysOperationRail(), VerificationRail()]`。同时在 `deep_agent.go` 的 `createSubagentFromFactory` switch 中添加 `"verification_agent"` case。

---

### 问题 2: BrowserAgent settings 传递断裂

- **章节**：9.26
- **Python 参考代码**：
  ```python
  # browser_agent.py:165 (build)
  resolved_settings = _resolve_runtime_settings(model, settings)

  # browser_agent.py:217 (create)
  resolved_settings = _resolve_runtime_settings(model, settings)
  ```
  `build_browser_agent_config` 接受 `settings: Optional[RuntimeSettings] = None` 参数并传给 `_resolve_runtime_settings`。
- **Go 问题代码**：
  ```go
  // browser_agent.go:152 (Build)
  resolvedSettings := bm.ResolveRuntimeSettings(model, nil)  // 硬编码 nil

  // browser_agent_factory.go:38 (Create)
  settings := bm.ResolveRuntimeSettings(params.Model, nil)  // 硬编码 nil
  ```
- **问题描述**：Build 和 Create 两层都硬编码传 `nil` 给 `ResolveRuntimeSettings`。虽然 Build 层将 `resolvedSettings` 存入了 `FactoryKwargs`，但 Create 层不从 `FactoryKwargs` 读取，而是重新调用 `ResolveRuntimeSettings(params.Model, nil)`。用户自定义 RuntimeSettings 完全丢失。
- **修复方案**：在 `SubagentCreateParams` 中添加 `RuntimeSettings *bm.RuntimeSettings` 字段，在 Build 和 Create 层都从 params 读取 settings 传给 `ResolveRuntimeSettings`。

---

### 问题 3: BuildExploreAgentConfig 缺少默认 Rails

- **章节**：9.30
- **Python 参考代码**：
  ```python
  # explore_agent.py:192 (build_explore_agent_config)
  rails=rails if rails is not None else [SysOperationRail(read_only=True)],
  ```
- **Go 问题代码**：
  ```go
  // explore_agent.go:143 (BuildExploreAgentConfig)
  cfg.Rails = params.Rails  // 缺少默认 Rails 逻辑
  ```
- **问题描述**：Python 的 `build_explore_agent_config` 在 rails 为 None 时默认注入 `[SysOperationRail(read_only=True)]`。Go 的 `BuildExploreAgentConfig` 仅 `cfg.Rails = params.Rails`，当 params.Rails 为 nil 时不会注入默认 Rails。这意味着通过配置层路径（延迟实例化）创建的 ExploreAgent 没有 SysOperationRail 保护，可以执行写操作——违反了 ExploreAgent 的只读语义。
- **修复方案**：在 `BuildExploreAgentConfig` 中添加与 verification_agent.go 相同的 nil Rails 默认注入逻辑：
  ```go
  if params.Rails == nil {
      cfg.Rails = []sainterfaces.AgentRail{
          rails.NewSysOperationRail(rails.WithReadOnly(true)),
      }
  } else {
      cfg.Rails = params.Rails
  }
  ```

---

### 问题 4: BuildPlanAgentConfig 缺少默认 Rails

- **章节**：9.28
- **Python 参考代码**：
  ```python
  # plan_agent.py:119 (build_plan_agent_config)
  rails=rails if rails is not None else [SysOperationRail()],
  ```
- **Go 问题代码**：
  ```go
  // plan_agent.go:122 (BuildPlanAgentConfig)
  cfg.Rails = params.Rails  // 缺少默认 Rails 逻辑
  ```
- **问题描述**：同问题 3，Python 的 `build_plan_agent_config` 在 rails 为 None 时默认注入 `[SysOperationRail()]`。Go 缺少此逻辑。PlanAgent 的提示词明确声明只读模式，但没有 SysOperationRail 的强制保护，Agent 可能通过工具执行写操作。
- **修复方案**：在 `BuildPlanAgentConfig` 中添加默认 Rails 注入逻辑：
  ```go
  if params.Rails == nil {
      cfg.Rails = []sainterfaces.AgentRail{rails.NewSysOperationRail()}
  } else {
      cfg.Rails = params.Rails
  }
  ```

---

### 问题 5: SpawnManager.CleanupTeammate 缺少 RemoveChunkObserver 和 StopHealthCheck

- **章节**：9.58
- **Python 参考代码**：
  ```python
  # spawn_manager.py:146-164
  async def cleanup_teammate(self, member_name: str) -> None:
      handle = self.spawned_handles.pop(member_name, None)
      if handle is None:
          return
      # 1. 断开 chunk forwarder（先移除观察者，再置空引用）
      forward = getattr(handle, "chunk_forward", None)
      agent_ref = getattr(handle, "agent_ref", None)
      if forward is not None and agent_ref is not None:
          with contextlib.suppress(Exception):
              agent_ref.stream_controller.remove_chunk_observer(forward)
          handle.chunk_forward = None
      try:
          # 2. 停止健康检查
          await handle.stop_health_check()
          # 3. 强制终止
          if handle.is_alive:
              await handle.force_kill()
      except Exception as e:
          team_logger.error("Error cleaning up teammate {}: {}", member_name, e)
  ```
- **Go 问题代码**：
  ```go
  // spawn_manager.go:150-173
  // 断开 chunk_forward 观察者
  if inproc, ok := handle.(*spawn.InProcessSpawnHandle); ok {
      inproc.SetChunkForward(nil)  // 仅设为 nil，未调用 RemoveChunkObserver！
  }
  // 强制终止
  _ = handle.ForceKill()  // 缺少: handle.StopHealthCheck()
  ```
- **问题描述**：两个关键步骤缺失：
  1. **未从 teammate 的 StreamController 上 RemoveChunkObserver**：仅 SetChunkForward(nil) 把引用置空，但观察者回调仍然注册在 teammate 的 StreamController 上。teammate 被清理后，chunk 回调仍会触发，尝试向已关闭的 streamQueue 写入，可能导致 panic 或数据泄漏。
  2. **缺少 StopHealthCheck()**：Python 先停健康检查再终止，Go 直接 ForceKill。健康检查协程可能在 ForceKill 后仍在运行。
- **修复方案**：
  ```go
  // 在 SetChunkForward(nil) 之前：
  if forwardCb := inproc.ChunkForward(); forwardCb != nil {
      if teammateSC := inproc.StreamController(); teammateSC != nil {
          teammateSC.RemoveChunkObserver(forwardCb)
      }
  }
  inproc.SetChunkForward(nil)

  // 在 ForceKill 之前：
  inproc.StopHealthCheck()
  ```

---

### 问题 6: SpawnManager.ShutdownAllHandles 缺少 RemoveChunkObserver 和 StopHealthCheck

- **章节**：9.58
- **Python 参考代码**：
  ```python
  # spawn_manager.py:295-304
  async def shutdown_all_handles(self) -> None:
      for member_name in list(self.spawned_handles.keys()):
          try:
              await self.cleanup_teammate(member_name)  # 复用 cleanup_teammate
          except Exception as e:
              team_logger.error(...)
      self.spawned_handles.clear()
  ```
- **Go 问题代码**：
  ```go
  // spawn_manager.go:275-294
  for memberName, handle := range handles {
      if inproc, ok := handle.(*spawn.InProcessSpawnHandle); ok {
          inproc.SetChunkForward(nil)  // 同问题5：缺少 RemoveChunkObserver
      }
      _ = handle.ForceKill()  // 缺少 StopHealthCheck
  }
  ```
- **问题描述**：Python 的 `shutdown_all_handles` 复用 `cleanup_teammate`，保证完整清理。Go 自己内联处理，存在与问题5相同的缺陷。且 Go 在循环前就清空了 map，如果有 panic 会导致句柄泄漏。
- **修复方案**：将清理逻辑改为调用 `CleanupTeammate`，或至少补上 `RemoveChunkObserver` 和 `StopHealthCheck`。

---

### 问题 7: Runtime Manager 核心5个生命周期方法全部为 stub

- **章节**：9.59b
- **Python 参考代码**：
  ```python
  # runtime/manager.py
  async def activate(self, ...):    # L108-169 — 完整调度逻辑
  async def finalize(self, ...):    # L171-229 — pause vs stop 决策
  async def pause(self, ...):       # L311-334 — 暂停处理
  async def stop_team(self, ...):   # L513-543 — 停止团队
  async def delete_team(self, ...): # L570-668 — 多步清理（DB→checkpoint→team→filesystem）
  ```
- **Go 问题代码**：
  ```go
  // runtime/manager.go:139-176
  func (m *TeamRuntimeManager) Activate(...) error { return nil }
  func (m *TeamRuntimeManager) Finalize(...) error { return nil }
  func (m *TeamRuntimeManager) Pause(...) (bool, error) { return false, nil }
  func (m *TeamRuntimeManager) StopTeam(...) (bool, error) { return false, nil }
  func (m *TeamRuntimeManager) DeleteTeam(...) (bool, error) { return false, nil }
  ```
- **问题描述**：全部5个方法为空 stub，仅记录日志后返回。Python 实现了完整的调度、决策、清理逻辑。唯一有实质实现的方法是 `Interact`。这些方法在 IMPLEMENTATION_PLAN.md 中标记为 ✅，但核心功能并未实现。
- **修复方案**：待 CoordinationKernel (#9.62) 和 TeamBackend (#9.55) 实现后回填。当前应将 IMPLEMENTATION_PLAN.md 中相关状态从 ✅ 改为 🔄 或添加 ⤵️ 标记。

---

### 问题 8: Runtime 层缺少 dispatch.py 调度决策实现

- **章节**：9.59b
- **Python 参考代码**：
  ```python
  # runtime/dispatch.py
  class RunActionKind(Enum):
      CREATE = "create"
      NEW_TEAM_IN_SESSION = "new_team_in_session"
      COLD_RECOVER = "cold_recover"
      RESUME_FROM_PAUSE = "resume_from_pause"
      REJECT_RUNNING = "reject_running"
      REJECT_ORPHANED = "reject_orphaned"
      REJECT_INCONSISTENT = "reject_inconsistent"

  @dataclass
  class RunAction:
      kind: RunActionKind
      reason: str = ""

  def decide_run_action(team_in_db, team_in_session, pool_entry, session_id) -> RunAction:
      """根据四元组状态决定运行路径"""
  ```
- **Go 问题代码**：Go `runtime/` 目录下没有 `dispatch.go` 文件。
- **问题描述**：`decide_run_action` 是 runtime manager 的核心调度逻辑，Python 中是一个纯函数，根据 (team_in_db, team_in_session, pool_entry, session_id) 四元组查真值表决定运行路径（7种决策）。Go 完全缺少这个文件，Activate 无法实现完整的调度决策。
- **修复方案**：新建 `runtime/dispatch.go`，实现 `RunActionKind` 枚举、`RunAction` 结构体和 `DecideRunAction` 函数，一比一复刻 Python 的真值表逻辑。此函数是纯逻辑，不依赖外部组件，可立即实现。

---

### 问题 9: StreamController.runOneRound 取消/异常路径与 Python 不一致

- **章节**：9.60
- **Python 参考代码**：
  ```python
  # stream_controller.py:338-354
  try:
      await self._execute_round(message)
      team_member = self._state.team_member
      if team_member is None or await team_member.status() != MemberStatus.SHUTDOWN_REQUESTED:
          await self._update_status(MemberStatus.READY)
  except asyncio.CancelledError:
      cancelled = True
      raise  # 重新抛出
  except BaseException as e:
      team_logger.error("Failed to execute deep agent, {}", e, exc_info=True)
      await self._update_status(MemberStatus.ERROR)
  finally:
      self.agent_task = None
  ```
- **Go 问题代码**：
  ```go
  // stream_controller.go:520-527
  if ctx.Err() != nil {
      cancelled = true
      _ = sc.updateStatus(ctx, atschema.MemberStatusError)  // 错误：取消时应保持当前状态
      return
  }
  sc.executeRound(ctx, message)
  _ = sc.updateStatus(ctx, atschema.MemberStatusReady)  // 错误：无条件设 READY
  ```
- **问题描述**：三条路径全部与 Python 不一致：
  1. **取消路径**：Go 设 `MemberStatusError`，Python 仅设 `cancelled=True` 不改状态
  2. **正常完成路径**：Go 无条件设 `MemberStatusReady`，Python 先检查 SHUTDOWN_REQUESTED
  3. **异常路径**：Go 没有对 executeRound 内部 panic 的恢复（Python 用 BaseException 捕获）
- **修复方案**：重构异常处理路径：
  ```go
  defer func() {
      sc.agentTask = nil
      if r := recover(); r != nil {
          _ = sc.updateStatus(ctx, atschema.MemberStatusError)
          logger.Error(scLogComponent).Any("panic", r).Msg("executeRound panic")
      }
  }()

  if ctx.Err() != nil {
      cancelled = true
      return  // 不改状态
  }

  sc.executeRound(ctx, message)

  // 正常完成：检查 SHUTDOWN_REQUESTED
  member := sc.state.TeamMember
  if member == nil || member.Status() != atschema.MemberStatusShutdownRequested {
      _ = sc.updateStatus(ctx, atschema.MemberStatusReady)
  }
  ```

---

### 问题 10: StreamController.executeRound 中 CancelledError 不传播

- **章节**：9.60
- **Python 参考代码**：
  ```python
  # stream_controller.py:486-488
  except asyncio.CancelledError:
      await self._update_execution(ExecutionStatus.CANCELLED)
      raise  # 重新抛出！
  ```
- **Go 问题代码**：
  ```go
  // stream_controller.go:537-541
  if ctx.Err() != nil {
      _ = sc.updateExecution(ctx, atschema.ExecutionStatusCancelled)
      _ = sc.updateExecution(ctx, atschema.ExecutionStatusIdle)
      return  // 不传播取消信号
  }
  ```
- **问题描述**：Python 在 CancelledError 路径设 CANCELLED 后**重新抛出**，让 `_run_one_round` 的 except CancelledError 分支捕获。Go 不传播取消信号，如果取消发生在 `runRetryingStream` 执行期间（而非入口），Go 可能走到 err != nil 分支错误标记为 FAILED。
- **修复方案**：在 executeRound 中区分 context 取消和其他错误。context 取消时设 CANCELLED 并通过返回值或 channel 传播信号到 runOneRound。

---

### 问题 11: ToolOptimizerBase.Step()/Bind() 签名与 BaseOptimizer 接口不匹配

- **章节**：9.72b
- **Python 参考代码**：
  ```python
  # optimizer/base.py:129
  def step(self) -> Dict[tuple[str, str], Any]:

  # optimizer/tool_call/base.py:22
  class ToolOptimizerBase(BaseOptimizer):
  ```
- **Go 问题代码**：
  ```go
  // optimizer/tool_call/base.go:257
  func (b *ToolOptimizerBase) Step() map[string]any {
      return map[string]any{}
  }

  // optimizer/tool_call/base.go:262
  func (b *ToolOptimizerBase) Bind(operators map[string]any, targets []string, config map[string]any) int {
  ```
- **问题描述**：`BaseOptimizer` 接口定义 `Step() map[schema.UpdateKey]any` 和 `Bind(operators map[string]operator.Operator, ...)`，但 `ToolOptimizerBase` 的方法签名不一致：Step 返回 `map[string]any`，Bind 接受 `map[string]any`。这意味着 `ToolOptimizerBase` **实际上没有实现 BaseOptimizer 接口**，编译时不会报错（因为没有显式声明 `var _ BaseOptimizer = (*ToolOptimizerBase)(nil)`），但运行时无法作为 BaseOptimizer 使用。
- **修复方案**：
  1. 将 `Step()` 返回类型改为 `map[schema.UpdateKey]any`
  2. 将 `Bind()` 的 operators 参数类型改为 `map[string]operator.Operator`
  3. 添加接口满足检查：`var _ BaseOptimizer = (*ToolOptimizerBase)(nil)`

---

### 问题 12: ToolCallDetail.CallResult 类型过度限制

- **章节**：9.77
- **Python 参考代码**：
  ```python
  # trajectory/types.py:57-58
  @dataclass
  class ToolCallDetail:
      call_args: Any = None
      call_result: Any = None
  ```
- **Go 问题代码**：
  ```go
  // trajectory/types.go:43
  CallResult map[string]any
  ```
- **问题描述**：Python 的 `call_result` 类型是 `Any`，可以是字符串、字典、列表等。Go 定义为 `map[string]any`，但实际使用中 `call_result` 可能是：
  - 字符串错误信息（如 `"Error: timeout"`）
  - JSON 字符串（如 `'{"key": "value"}'`）
  - `nil`/`None`

  Go 代码中多处用 `fmt.Sprintf("%v", toolDetail.CallResult)` 把它当字符串用（from_conv.go:923, from_conv.go:412），协作信号检测中需要 content 是 string。定义为 `map[string]any` 过于限制，非 map 类型的 CallResult 会被丢失。
- **修复方案**：将 `CallResult` 类型改为 `any`，消费处做类型断言或 `fmt.Sprintf("%v", ...)` 转换：
  ```go
  CallResult any  // 对齐 Python: Optional[Any]
  ```

---

### 问题 13: AgentManager.GetAgent 未处理 ACP 通道配置合并

- **章节**：10.3.12
- **Python 参考代码**：
  ```python
  # agent_manager.py:250-254
  if channel_key == "acp":
      config = {
          **config,
          **_build_acp_agent_config()
      }
  ```
- **Go 问题代码**：
  ```go
  // agent_manager.go:165-166
  // ⤵️ ACP: channel_key=="acp" 时合并 _build_acp_agent_config
  // 完全跳过，仅有注释占位
  ```
- **问题描述**：ACP 通道的 agent 不会获得 `agent_name: "acp_agent"`, `channel_id: "acp"`, `tool_profile: "acp"`, `enable_filesystem_rail: True` 等关键配置。ACP 通道下 agent 行为完全偏离。
- **修复方案**：实现 `_build_acp_agent_config` 的 Go 等价函数，并在 GetAgent 中 channel_key=="acp" 时合并配置。

---

### 问题 14: AgentManager.Initialize 完全为空实现

- **章节**：10.3.12
- **Python 参考代码**：
  ```python
  # agent_manager.py:160-182
  async def initialize(self, channel_id, extra_config=None):
      channel_key = _normalize_channel_id(channel_id)
      if channel_key == "acp":
          # 1. 记录 client_capabilities
          # 2. cleanup 已有 ACP agents
          # 3. _build_acp_agent_config(extra_config) → _create_agent("acp", "code", config)
          # 4. return ACP_DEFAULT_CAPABILITIES.copy()
      return None
  ```
- **Go 问题代码**：
  ```go
  // agent_manager.go:230-242
  func (am *AgentManager) Initialize(ctx context.Context, channelID string, extraConfig map[string]any) (map[string]any, error) {
      channelKey := normalizeChannelID(channelID)
      _ = channelKey
      _ = ctx
      _ = extraConfig
      return nil, nil
  }
  ```
- **问题描述**：Initialize 方法完全为空，仅用 `_ =` 忽略了所有参数。ACP 通道的 Initialize 执行了关键的初始化逻辑。虽然非 ACP 通道返回 nil 是对的，但 `_ = channelKey` 使得 ACP 通道的分支完全缺失。
- **修复方案**：实现 ACP 分支逻辑，或至少不用 `_ =` 忽略 channelKey，为后续实现保留入口。

---

### 问题 15: AgentManager.ReloadAgentsConfig envOverrides nil 时不更新 latestEnvOverrides

- **章节**：10.3.12
- **Python 参考代码**：
  ```python
  # agent_manager.py:310-316
  async def reload_agents_config(self, config, env):
      self._latest_env_overrides = dict(env) if isinstance(env, dict) else {}
      for env_key, env_value in self._latest_env_overrides.items():
  ```
- **Go 问题代码**：
  ```go
  // agent_manager.go:344-350
  if envOverrides != nil {
      am.mu.Lock()
      am.latestEnvOverrides = copyMap(envOverrides)
      am.mu.Unlock()
  }
  for key, val := range envOverrides {
  ```
- **问题描述**：Python 总是用新值覆盖 `_latest_env_overrides`（即使是空 dict）。Go 只在 `envOverrides != nil` 时更新，意味着传 nil 时 `latestEnvOverrides` 保留旧值，但 L350 仍会遍历旧的 envOverrides（`range nil` 是安全的所以不会报错）。行为不一致：Python 显式清空旧 overrides，Go 保留旧值。
- **修复方案**：去掉 `if envOverrides != nil` 保护，始终更新 `latestEnvOverrides`，对齐 Python `self._latest_env_overrides = dict(env) if isinstance(env, dict) else {}`：
  ```go
  am.mu.Lock()
  if envOverrides != nil {
      am.latestEnvOverrides = copyMap(envOverrides)
  } else {
      am.latestEnvOverrides = make(map[string]any)
  }
  am.mu.Unlock()
  ```

---

### 问题 16: AgentConfigService.userAgentsDir 使用 UserHomeDir 而非 WorkspaceDir

- **章节**：10.3.13
- **Python 参考代码**：
  ```python
  # agent_config_service.py:194-195
  @staticmethod
  def _get_user_agents_dir() -> Path:
      return get_user_workspace_dir() / "agents"
  ```
- **Go 问题代码**：
  ```go
  // agent_config.go:429-431
  func (s *AgentConfigService) userAgentsDir() string {
      return filepath.Join(pathutil.UserHomeDir(), ".uapclaw", "agents")
  }
  ```
- **问题描述**：Python 的 `get_user_workspace_dir()` 返回 `~/.jiuwenswarm`（或 `JIUWENSWARM_DATA_DIR` 环境变量覆盖值），Go 的 `UserHomeDir()` + `.uapclaw` 不等价于 `WorkspaceDir()`。`WorkspaceDir()` 已包含 `.uapclaw` 后缀，且支持 `UAPCLAW_HOME` 环境变量覆盖。使用 `UserHomeDir() + ".uapclaw"` 会绕过 WorkspaceDir 的环境变量逻辑。
- **修复方案**：改为使用 `pathutil.WorkspaceDir()`：
  ```go
  func (s *AgentConfigService) userAgentsDir() string {
      return filepath.Join(pathutil.WorkspaceDir(), "agents")
  }
  ```

---

### 问题 17: agentDefToSubagentConfig 未设置 Tools 字段

- **章节**：10.3.7
- **Python 参考代码**：
  ```python
  # interface_deep.py:5999-6007
  return SubAgentConfig(
      agent_card=card,
      system_prompt=agent_def.prompt,
      tools=tools,  # ← 合并后的工具列表
      model=resolved_model,
      skills=agent_def.skills,
      max_iterations=agent_def.max_iterations,
      enable_task_loop=True,
  )
  ```
- **Go 问题代码**：
  ```go
  // deep_adapter_config.go:256-264
  return &hschema.SubAgentConfig{
      AgentCard:      card,
      SystemPrompt:   agentDef.Prompt,
      Model:          resolvedModel,
      Skills:         agentDef.Skills,
      MaxIterations:  maxIter,
      EnableTaskLoop: true,
      FactoryName:    "custom_" + agentDef.Name,
      // 缺少: Tools: tools
  }
  ```
- **问题描述**：Python 将合并后的 `tools` 列表设置到 `SubAgentConfig.tools`。Go 虽然在 L226-241 计算了 `tools` 变量，但没有将其设置到返回的 `SubAgentConfig` 中。这导致自定义 Agent 的工具配置丢失。虽然 `createSubAgent` 中会再次从 `agentDef.Tools` 计算工具过滤，但 `SubAgentConfig` 本身缺少 tools 信息会影响其他使用该 config 的代码路径。
- **修复方案**：在返回的 `SubAgentConfig` 中添加 `Tools: tools` 字段。

---

## 一般问题简要说明

| # | 章节 | 问题 | 修复方案 |
|---|------|------|----------|
| 18 | 9.26 | `BuildBrowserRuntimeTools` 缺少 `language` 参数（Python 当前 `del language` 未使用，但接口预留了多语言能力） | 添加 `language string` 参数，当前可忽略 |
| 19 | 9.30/9.28 | `CreateExploreAgent`/`CreatePlanAgent` 默认 Rail 用 `WithReadOnly(true)`，Python 的 `create_xxx_agent` 用 `SysOperationRail()`（无 read_only）。Go 标注为"统一增强" | 明确标注为有意偏离，或对齐 Python |
| 20 | 9.58 | `InProcessSpawnHandle.Shutdown` 对已关闭 handle 返回 `(false, error)`，Python 幂等返回 `True` | 改为 `return true, nil` |
| 21 | 9.58 | `WaitForCompletion` 不区分正常/异常退出（Go 总返回 0，Python 异常返回 -1） | 增加 `exitCode` 字段传播退出状态 |
| 22 | 9.59 | `BindSession` 缺少类型检查，步骤4-5（DB表创建、Leader配置持久化）为 TODO | 待 #9.61 回填 |
| 23 | 9.59 | `ResumeForNewSession`/`RecoverForExistingSession` 核心逻辑为 TODO | 待 #9.61 回填 |
| 24 | 9.59b | Interaction 层 `UserInbox`/`HumanAgentInbox`/`DeliverDirect` 消息发送为 stub | 待 #9.55 回填 |
| 25 | 9.59b | Runtime 层缺少 `metadata.py` 对应实现（团队命名空间读写） | 新建 `runtime/metadata.go` |
| 26 | 9.60 | StreamController 缺少 SHUTDOWN_REQUESTED 检查，shutdown 请求的成员继续运行 | 在完成路径中添加状态检查 |
| 27 | 9.60 | `streamOneRun` 中 sessionID 和 teamSession 传空字符串/nil | 从 SessionState 读取并传递 |
| 28 | 9.72e | `BaseOptimizerMixin.Bind()` 未使用 DefaultTargets 作为 fallback | 在 Bind 中添加空 targets fallback |
| 29 | 9.72e | `TextualParameter.Gradients` 类型为 `map[string]string`，Python 为 `Dict[str, Any]` | 确认当前业务只需字符串，否则改为 `map[string]any` |
| 30 | 9.73 | `ConversationSignalDetector.Detect()` 不支持 Trajectory 输入 | 已有 `DetectTrajectorySignals()` 替代，文档说明即可 |
| 31 | 9.77 | Trajectory 缺少 builder/extractor/store/registry/aggregator 子模块 | 按优先级逐步补充 |
| 32 | 10.3.12 | `CreateSession` 未处理 ACP 通道 session ID 生成 | 添加 `acp_{uuid[:8]}` 格式 |
| 33 | 10.3.13 | `projectAgentsDir`/`localAgentsDir` 使用 `.uapclaw` 而非 `.jiuwenswarm` | 确认品牌重命名策略 |
| 34 | 10.3.13 | `ListAvailableTools` groups 按字母排序与 Python 插入顺序不一致 | 使用硬编码列表或有序切片 |
| 35 | 10.3.7 | `AgentTool.Invoke` 缺少 session 校验 | 添加 session nil 检查 |

---

## 提示问题简要说明

| # | 章节 | 问题 | 修复方案 |
|---|------|------|----------|
| 36 | 9.29 | VerificationAgent 描述 fallback 应为 `"en"` 非 `"cn"`（Python: `VERIFICATION_AGENT_DESC.get(lang, DESC["en"])`) | 改 fallback 为 `"en"` |
| 37 | 9.58 | InProcessSpawn goroutine 核心逻辑为空 | 待 #9.85 回填 |
| 38 | 9.58 | SharedResources.GetSharedDB 返回 nil | 待 #9.64 回填 |
| 39 | 9.59 | SessionManager.recoveryManager 为 any 类型 | 待 #9.61 替换为具体类型 |
| 40 | 9.72e | LLMResilience 中 defer recover 不必要（Go context 超时不会 panic） | 评估移除 |
| 41 | 9.73 | DetectUserIntent 中 is_feedback 键不存在时走 fallback，Python 返回空列表 | 改为返回 nil |
| 42 | 9.60 | logRoundPanic 不记录完整堆栈 | 使用 `runtime.Stack` 获取堆栈 |
| 43 | 9.60 | StreamController 日志组件使用 ComponentCommon 而非 ComponentAgentCore | 统一为 ComponentAgentCore |
| 44 | 10.3.12 | createAgent 中 normalizeProjectDir 硬编码空字符串 | 改为 `projectDir := ""` 后从 config 赋值 |

---

## 统计

| 级别 | 数量 |
|------|------|
| 严重 | 17 |
| 一般 | 18 |
| 提示 | 9 |
| **总计** | **44** |

### 按章节分布

| 章节 | 严重 | 一般 | 提示 |
|------|------|------|------|
| 9.25-9.30 Subagents | 2 | 2 | 1 |
| 9.58 SpawnManager | 2 | 2 | 2 |
| 9.59 SessionManager | 0 | 3 | 1 |
| 9.59b Interaction+Runtime | 2 | 3 | 0 |
| 9.60 StreamController | 2 | 2 | 2 |
| 9.70c-9.77 Evolving | 2 | 4 | 2 |
| 10.3.7 CodeAgentRail | 1 | 1 | 0 |
| 10.3.12 AgentManager | 3 | 1 | 1 |
| 10.3.13 AgentConfigService | 1 | 2 | 0 |

### 建议优先修复顺序

1. **立即修复**（影响功能正确性）：问题 1, 3, 4, 11, 12, 16, 17
2. **尽快修复**（影响资源泄漏/异常处理）：问题 5, 6, 9, 10
3. **版本内修复**（ACP 通道、调度逻辑）：问题 7, 8, 13, 14, 15
4. **后续迭代**（stub 回填）：问题 22, 23, 24, 25, 31, 37, 38
