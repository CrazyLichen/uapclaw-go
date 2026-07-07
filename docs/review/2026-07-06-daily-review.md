# 代码审查报告 — 2026-07-06 Daily Review

> 审查范围：24小时内提交记录（41 commits）
> 审查领域：9.1 DeepAgent、9.6 TaskLoopEventExecutor、9.7 SessionSpawnExecutor、Multi-Agent 对齐修复、harness 辅助模块
> 审查方式：Go 实现代码 vs Python 参考代码逐行对照

---

## 一、审查范围

### 24小时提交概览

| 类别 | 提交数 | 主要内容 |
|------|--------|---------|
| 9.1 DeepAgent | 4 | 完整实现（2149行，83方法）+ 偏差修复 |
| 9.6-9.7 TaskLoop/SessionSpawn | 10 | TaskLoopEventExecutor + SessionSpawnExecutor + subagent 工具 |
| Multi-Agent 对齐修复 | 2 | 12项 review 问题修复 + timeout 变量清理 |
| Harness 重构 | 6 | DeepAgentProvider→DeepAgentInterface + schema迁移 + interfaces包新建 |
| CI/Lint 修复 | 19 | gofmt/errcheck/staticcheck/编译错误修复 |

### 涉及的关键文件

**9.1 DeepAgent:**
- `internal/agentcore/harness/deep_agent.go` (2149行)

**9.6-9.7 TaskLoop:**
- `internal/agentcore/harness/task_loop/executor.go`
- `internal/agentcore/harness/task_loop/handler.go`
- `internal/agentcore/harness/task_loop/session_spawn_executor.go`
- `internal/agentcore/harness/tools/subagent/session_tools.go`

**Multi-Agent:**
- `internal/agentcore/multi_agent/teams/handoff/` (全目录)
- `internal/agentcore/multi_agent/teams/hierarchical_msgbus/` (全目录)
- `internal/agentcore/multi_agent/teams/hierarchical_tools/` (全目录)
- `internal/agentcore/multi_agent/team_runtime/` (全目录)

**Harness 辅助:**
- `internal/agentcore/harness/interfaces/` (全目录)
- `internal/agentcore/harness/harness_config/` (全目录)
- `internal/agentcore/harness/schema/` (全目录)
- `internal/agentcore/harness/task_loop/controller.go`
- `internal/agentcore/harness/task_loop/loop_queues.go`

---

## 二、问题汇总

### 统计

| 严重程度 | 数量 | 说明 |
|---------|------|------|
| 🔴 严重 | 16 | 运行时会崩溃、数据竞争、功能缺失 |
| 🟡 一般 | 27 | 行为不一致、安全隐患、缺失校验 |
| 🔵 提示 | 16 | 代码规范、可维护性、小改进 |

---

## 三、🔴 严重问题（16个）

### S-01: DeepAgent.runTaskLoopInvoke 中 err 变量遮蔽

- **位置**: `harness/deep_agent.go` — `runTaskLoopInvoke` 方法
- **问题**: 内层 `if err := ...; err != nil` 的 err 变量遮蔽了外层的 err，导致外层 err 始终为 nil，错误被静默吞掉
- **影响**: 任务循环错误无法传播给调用方，Agent 会在出错后继续执行而非返回错误
- **Python对比**: Python 中 `except Exception as e` 在同一作用域内正确传播异常

### S-02: DeepAgent.ensureInitialized 的 TOCTOU 并发竞态

- **位置**: `harness/deep_agent.go` — `ensureInitialized` 方法
- **问题**: 先检查 `isInitialized` 再加锁初始化，典型的 Time-Of-Check-Time-Of-Use 竞态。两个并发调用者可能都通过检查，导致重复初始化
- **影响**: 并发调用 DeepAgent 时可能创建重复的 controller/session/rails，产生资源泄露和状态不一致
- **Python对比**: Python asyncio 单线程模型无此问题

### S-03: completeSessionSpawn 无锁读取 sessionToolkit/interactionQueues

- **位置**: `harness/task_loop/handler.go:485, 512`
- **问题**: `completeSessionSpawn` 直接读取 `h.sessionToolkit` 和 `h.interactionQueues` 而未持有 `h.mu`，而 `SetSessionToolkit` 和 `SetInteractionQueues` 在锁保护下写入，存在数据竞争
- **影响**: 并发读写可能导致 panic 或读到不一致的数据
- **Python对比**: Python 通过 GIL 天然避免

### S-04: PrepareRound 对 channel close 可能 panic

- **位置**: `harness/task_loop/handler.go:69-71`
- **问题**: `close(h.currentCh)` 只检查了 `!= nil`，如果 PrepareRound 被连续调用两次，第二次 close 时 channel 已被 close 过会 panic。当前流程依赖调用方保证每轮只调用一次，属于脆弱设计
- **影响**: 调用方约束违反时运行时 panic
- **Python对比**: Python 使用 `asyncio.Future.cancel()` + 重新创建 Future

### S-05: HandoffTeam.Send()/Publish() 缺少自动启动（_ensure_started）

- **位置**: `multi_agent/team_runtime/team_runtime.go:337-391`
- **问题**: Python `send()`/`publish()` 在发送前调用 `await self._ensure_started()` 自动启动 runtime，Go 只检查 `IsRunning()` 返回 error
- **影响**: 用户必须显式调用 Start()，否则 Send/Publish 报错，与 Python 的懒启动行为严重不一致
- **Python对比**: `team_runtime.py:386, 436` — `await self._ensure_started()`

### S-06: HandoffTeam.coordinatorRegistry 无并发保护

- **位置**: `multi_agent/teams/handoff/handoff_team.go:52, 329, 541+`
- **问题**: `coordinatorRegistry` 是 `map[string]*HandoffOrchestrator`，在 `runChain()` 中读写，在 `lookupCoordinator()` 中读取，无锁保护
- **影响**: 多会话并发调用 Invoke 时 map 并发读写会 panic
- **Python对比**: Python asyncio 单线程模型无此问题

### S-07: HandoffTeam.resetInternalAgents() 重置 sync.Once 存在数据竞争

- **位置**: `multi_agent/teams/handoff/handoff_team.go:453-457`
- **问题**: `resetInternalAgents()` 重置 `sync.Once{}` 不在 `initLock` 与 `ensureInternalAgents()` 的同一临界区内。如果 `ensureInternalAgents()` 正在执行期间 `AddAgent()` 重置了 Once，可能导致初始化状态不一致
- **影响**: 并发 AddAgent 时可能导致内部 Agent 列表状态损坏
- **Python对比**: Python 通过 asyncio 单线程 + 非重入锁保护

### S-08: DeepAgentInterface 方法覆盖率严重不足

- **位置**: `harness/interfaces/deep_agent.go:17-43`
- **问题**: Go 接口只覆盖了 Python DeepAgent 约 30% 的公开 API，缺失：configure、状态管理(save/clear_state)、Rail 管理、配置热加载、上下文 API、模式切换等核心方法
- **影响**: 外部消费者无法通过接口驱动 DeepAgent 的完整生命周期
- **Python对比**: Python DeepAgent 有 60+ 个公开方法/属性

### S-09: DeepAgentInterface.LoadState 参数类型与 Python 不对齐

- **位置**: `harness/interfaces/deep_agent.go:27`
- **问题**: Go 接口 `LoadState(sess SessionFacade)` 使用 SessionFacade，Python 接受完整 Session 对象。SessionFacade 可能缺少 `get_state`/`update_state` 等方法
- **影响**: load_state 的完整功能（读缓存→读持久化→写缓存）可能无法通过 SessionFacade 完成
- **Python对比**: Python `load_state(self, session: Session) -> DeepAgentState`

### S-10: DeepAgentInterface.LoopController() 返回类型范围过大

- **位置**: `harness/interfaces/deep_agent.go:23`
- **问题**: 返回 `controller.ControllerInterface`（通用控制器），但 Python 返回 `Optional[TaskLoopController]`，消费者需要 TaskLoopController 特有方法（submit_round、drain_follow_up 等）
- **影响**: 通过接口获取的 Controller 无法调用 TaskLoop 特有方法
- **Python对比**: Python `loop_controller: Optional[TaskLoopController]`

### S-11: LoopCoordinatorInterface 过度精简，缺失 should_continue() 等核心方法

- **位置**: `harness/interfaces/deep_agent.go:48-53`
- **问题**: Go 只定义了 `Iteration()` 和 `RequestAbort()`，Python LoopCoordinator 有 12+ 个方法包括 `should_continue()`（核心循环控制）、`reset()`、`increment_iteration()` 等
- **影响**: 消费者无法通过接口驱动循环决策
- **Python对比**: Python `LoopCoordinator`（loop_coordinator.py:24-215）

### S-12: DeepAgentConfig.Subagents 丢失 DeepAgent 实例支持

- **位置**: `harness/schema/config.go:178`
- **问题**: Python `subagents: Optional[List[SubAgentConfig | DeepAgent]]` 允许列表包含已配置的 DeepAgent 实例，Go `Subagents []SubAgentConfig` 只接受 SubAgentConfig
- **影响**: 无法通过 DeepAgent 实例匹配/创建子 Agent，共享已配置子 Agent 的路径完全缺失
- **Python对比**: Python `subagents: Optional[List[SubAgentConfig | DeepAgent]] = None`

### S-13: SubagentSpec 接口过于简陋

- **位置**: `harness/interfaces/deep_agent.go:56-61`
- **问题**: `SubagentSpec` 只有 `SpecName()` 方法，Python 的 `_find_subagent_spec` 返回后消费者访问 `spec.model`、`spec.agent_card`、`spec.system_prompt` 等大量字段
- **影响**: 无法通过 SubagentSpec 获取创建子 Agent 所需的完整属性
- **Python对比**: Python `create_subagent` 直接访问 spec 的多个字段

### S-14: DeepAgentInterface 引入对具体实现包的依赖

- **位置**: `harness/interfaces/deep_agent.go:3-9`
- **问题**: 接口方法返回 `*agents.ReActAgent`（具体指针）、`modules.EventHandler`、`controller.ControllerInterface`、`*hschema.DeepAgentConfig`（具体指针），依赖了 5 个子包
- **影响**: 违背"接口包应最小化依赖"原则，增加编译依赖图，实现包修改触发接口包重编译
- **Python对比**: Python 通过 TYPE_CHECKING 延迟导入

### S-15: _find_subagent_spec 无法匹配 DeepAgent 实例

- **位置**: `harness/schema/config.go:178` + `harness/interfaces/deep_agent.go:56-61`
- **问题**: 由于 S-12（Subagents 类型不支持 DeepAgent）和 S-13（SubagentSpec 方法不足），Go 的子 Agent 查找逻辑无法匹配 DeepAgent 实例，而 Python 同时搜索 SubAgentConfig 和 DeepAgent
- **影响**: 配置中通过 DeepAgent 实例引用的子 Agent 无法被找到
- **Python对比**: Python `_find_subagent_spec` 遍历 subagents 并检查两种 isinstance

### S-16: TeamRuntime.Send() recipient 不存在时错误码不一致

- **位置**: `multi_agent/team_runtime/team_runtime.go:337-365`
- **问题**: sender 不存在时返回 `StatusAgentTeamAgentNotFound`，但 recipient 不存在时仅返回普通 error，Python 中两者都使用 `AGENT_TEAM_AGENT_NOT_FOUND` 错误码
- **影响**: 调用方无法通过统一错误码判断 Agent 不存在的原因
- **Python对比**: Python `team.py:228-232` — `raise build_error(StatusCode.AGENT_TEAM_AGENT_NOT_FOUND, ...)`

---

## 四、🟡 一般问题（27个）

### G-01: DeepAgent 缺少 Stream 方法的流式输出实现
- **位置**: `harness/deep_agent.go` — Stream 方法
- **问题**: Stream 实现为调用 Invoke 后包装为两步流式输出，丢失了任务循环中间过程的实时输出
- **Python对比**: Python `_inner_stream` 实现真正的流式（逐轮 emit chunk）

### G-02: SessionSpawnExecutor 子Agent invoke 未传 Session
- **位置**: `harness/task_loop/session_spawn_executor.go:115`
- **问题**: `subAgent.ReactAgent().Invoke(ctx, effective)` 未传入 `agentinterfaces.WithSession(sess)`
- **Python对比**: Python `subagent.invoke({"query": query, "conversation_id": cid})` 隐式传递 session

### G-03: SessionsSpawnTool 获取 TaskManager 路径与 Python 不一致
- **位置**: `harness/tools/subagent/session_tools.go:245-258`
- **问题**: 通过 `provider.LoopController().TaskManager()` 获取，Python 从 `handler.task_manager` 获取

### G-04: HandleTaskCompletion SessionSpawn 分支同步调用可能阻塞
- **位置**: `harness/task_loop/handler.go:279`
- **问题**: `completeSessionSpawn` 是同步方法，如果 ScheduleAutoInvokeOnSpawnDone 内有异步操作可能阻塞
- **Python对比**: Python `await self._complete_session_spawn(...)`

### G-05: joinLines 空切片会 panic
- **位置**: `harness/tools/subagent/session_tools.go:476`
- **问题**: `result := lines[0]` 空切片访问
- **Python对比**: Python `"\n".join(lines)` 天然安全

### G-06: SessionSpawnExecutor GetTask 错误返回模式不一致
- **位置**: `harness/task_loop/session_spawn_executor.go:61`
- **问题**: GetTask 错误时返回 `(ch, nil)`，而 TaskLoopEventExecutor 返回 `(ch, err)`

### G-07: SessionSpawnExecutor 同步错误路径先写 channel 再 close
- **位置**: `harness/task_loop/session_spawn_executor.go:59-61`
- **问题**: 同步路径 `ch <-` 后 `close(ch)`，与异步路径 `defer close(ch)` 模式不一致

### G-08: HandoffTeam.Send() 缺少 recipient 空字符串校验
- **位置**: `multi_agent/team_runtime/team_runtime.go:337-365`
- **Python对比**: Python `if not recipient: raise build_error(...)`

### G-09: HandoffTeam.Publish() 缺少 topic_id 空字符串校验
- **位置**: `multi_agent/team_runtime/team_runtime.go:370-391`
- **Python对比**: Python `if not topic_id: raise build_error(...)`

### G-10: Subscribe()/Unsubscribe() 缺少参数校验
- **位置**: `multi_agent/team_runtime/team_runtime.go:396-411`
- **Python对比**: Python 校验 `if not agent_id: raise` 和 `if not topic: raise`

### G-11: BaseTeam 创建默认 RuntimeConfig 时未传递 max_concurrent_messages/message_timeout
- **位置**: `teams/handoff/handoff_team.go:92-95`, `teams/hierarchical_msgbus/hierarchical_team.go:70-73`
- **问题**: MessageBus 使用默认 queue size (1000) 而非 TeamConfig 中用户设置的值
- **Python对比**: Python `MessageBusConfig(max_queue_size=self.config.max_concurrent_messages, ...)`

### G-12: HandoffTeam.Stream() 未实现真正的流式输出
- **位置**: `teams/handoff/handoff_team.go:140-157`
- **问题**: 调用 Invoke() 后包装为两步流式，丢失交接过程中中间输出
- **Python对比**: Python 使用 `standalone_stream_context`

### G-13: HandoffTeam.agentProviders 无并发保护
- **位置**: `teams/handoff/handoff_team.go:44, 198, 229, 471`
- **问题**: map 并发读写风险

### G-14: HandoffOrchestrator.RequestHandoff() 无并发保护
- **位置**: `teams/handoff/handoff_orchestrator.go:151-211`
- **问题**: `handoffCount` 和 `currentAgentID` 无 mutex 保护

### G-15: MessageRouter.RoutePubsubMessage() goroutine 泄露风险
- **位置**: `multi_agent/team_runtime/message_router.go:119-176`
- **问题**: 如果 RunAgent 不尊重 ctx 取消，goroutine 会泄露

### G-16: HierarchicalToolsTeam.pendingChildren 无并发保护
- **位置**: `teams/hierarchical_tools/hierarchical_team.go:39, 302, 457`
- **问题**: map 并发读写风险

### G-17: CommunicableAgent.Runtime() 返回 nil 而非报错
- **位置**: `multi_agent/team_runtime/communicable_agent.go:126-128`
- **问题**: Go 返回 nil，Python raise error。后续使用 nil 会导致 panic
- **Python对比**: Python `if self._runtime is None: raise build_error(...)`

### G-18: DeepAgentInterface.EventHandler() 返回具体类型而非窄接口
- **位置**: `harness/interfaces/deep_agent.go:25`
- **问题**: 返回 `modules.EventHandler`（具体接口），增加耦合

### G-19: DeepAgentConfig.Skills 类型丢失 Union[str, List[str]] 支持
- **位置**: `harness/schema/config.go:186`
- **问题**: Go `Skills []string`，Python `skills: Optional[Union[str, List[str]]]`
- **Python对比**: Python 支持单个 string 表示单个技能目录

### G-20: DeepAgentConfig.ModelSelection 切片允许重复 Model
- **位置**: `harness/schema/config.go:58-63, 212`
- **问题**: Python 用 Dict[Model, str] 保证唯一性，Go 切片允许重复

### G-21: DeepAgentConfig JSON 反序列化零值与 Python 默认值不一致
- **位置**: `harness/schema/config.go:356-361`
- **问题**: Go 零值 0 与 Python 默认值（max_iterations=15, completion_timeout=600.0 等）不一致，JSON 反序列化无法区分"显式设置 0"和"未设置"

### G-22: HarnessConfigRegistry 缺少 inspect() 和 entry_point 扫描
- **位置**: `harness/harness_config/registry.go:29-188`
- **问题**: Go 无 Python entry_point 等价机制，Discover() 语义不同

### G-23: LoopQueues 满队列时静默丢弃而非抛异常
- **位置**: `harness/task_loop/loop_queues.go:43-52, 57-66`
- **问题**: Python `put_nowait` 满时抛 QueueFull，Go 静默丢弃并记录日志，可能导致消息丢失
- **Python对比**: Python `self.steering.put_nowait(msg)` 满时抛 `QueueFull`

### G-24: registry.go 声明顺序违规
- **位置**: `harness/harness_config/registry.go:47-62`
- **问题**: 导出函数和非导出函数交叉排列

### G-25: controller.go 声明顺序违规
- **位置**: `harness/task_loop/controller.go:14, 25, 164-167`
- **问题**: 全局变量出现在非导出函数之后，结构体分隔注释重复

### G-26: schema/config.go 方法声明位置不符合规范
- **位置**: `harness/schema/config.go:108-113, 397-399`
- **问题**: `SubAgentConfig.SpecName()` 和 `EffectiveRestrictToWorkDir()` 未放在导出函数区块

### G-27: schema/task.go 导出函数区块出现两次
- **位置**: `harness/schema/task.go:83, 340`
- **问题**: 应合并为一个导出函数区块

---

## 五、🔵 提示问题（16个）

### T-01: DeepAgent 缺少 is_initialized 属性等只读属性
- **位置**: `harness/deep_agent.go`
- **Python对比**: Python DeepAgent 有 is_initialized、loop_session、react_config 等属性

### T-02: HandleInput is_follow_up 类型断言静默失败
- **位置**: `harness/task_loop/handler.go:162-164`
- **问题**: `isFollowUp, _ = v.(bool)` 如果 metadata 中为 int/string 会静默失败
- **Python对比**: Python `bool()` 对 truthy 值都能正确处理

### T-03: ExecuteAbility goroutine 捕获的 state 可能过期
- **位置**: `harness/task_loop/executor.go:219`
- **问题**: state 在 goroutine 启动前计算，执行时可能已过期（Python 同样存在）

### T-04: SessionsCancelTool cancel error 语义需确认
- **位置**: `harness/tools/subagent/session_tools.go:376-382`
- **Python对比**: Python `scheduler.cancel_task` 只返回 bool

### T-05: CreateSubagent 返回 DeepAgentInterface 粒度偏粗
- **位置**: `harness/interfaces/deep_agent.go:42`
- **问题**: 子 Agent executor 只需 ReactAgent()，却要求实现整个 DeepAgentInterface（Python 同样偏粗）

### T-06: executor.go 声明顺序违反项目规范
- **位置**: `harness/task_loop/executor.go:385-432`
- **问题**: 导出函数与非导出函数交叉，全局变量位置不对

### T-07: formatSessionSpawnSteer 硬编码不易扩展
- **位置**: `harness/task_loop/handler.go:590-604`
- **问题**: if/else 硬编码 vs Python dict 结构易扩展

### T-08: SessionsSpawnTool Session 获取静默降级而非报错
- **位置**: `harness/tools/subagent/session_tools.go:269-273`
- **Python对比**: Python 在 session 无效时直接抛错

### T-09: HierarchicalToolsTeam timeout 传递 TODO 未完成
- **位置**: `teams/hierarchical_tools/hierarchical_team.go:178`
- **问题**: `_ = teamOpts.Timeout // TODO: 将 timeout 传递给 StandaloneStreamContext`

### T-10: CommunicableAgent 缺少 AgentID() 导出方法
- **位置**: `multi_agent/team_runtime/communicable_agent.go`
- **Python对比**: Python 有 `agent_id` 属性

### T-11: HarnessConfig Python extra="allow" 保留额外字段 vs Go 丢弃
- **位置**: `harness/harness_config/schema.go:114-137`
- **问题**: 语义差异：Python 保留额外字段，Go 丢弃

### T-12: HarnessConfigInfo Version/PackageName/ConfigPath Optional vs string
- **位置**: `harness/harness_config/registry.go:14-27`
- **问题**: Go 必填 string 无法区分"未设置"和"设置为空"

### T-13: task_type.go 使用字符串常量而非命名类型
- **位置**: `harness/schema/task_type.go:7,9`
- **建议**: 定义 `type TaskType string` 获得编译时类型安全

### T-14: LoopQueues 默认容量 64 vs Python 无界队列
- **位置**: `harness/task_loop/loop_queues.go:23`
- **问题**: Go channel 必须指定缓冲区大小，64 缺乏依据

### T-15: interfaces/deep_agent_test.go 仅为编译检查，无实质测试
- **位置**: `harness/interfaces/deep_agent_test.go:1-15`
- **问题**: `var _ = (DeepAgentInterface)(nil)` 不验证运行时行为

### T-16: wrapProvider 不检查 AgentProvider 签名与 TeamAgentProvider 的一致性
- **位置**: `multi_agent/team_runtime/team_runtime.go:505`
- **问题**: 两者签名完全一致但类型不同，未来签名分叉会静默出错

---

## 六、Review 修复验证

以下 2025-07-15 review 发现的问题已确认修复：

| # | 修复项 | 状态 |
|---|--------|------|
| 1 | HandoffOrchestrator.Error() 使用 handoffResult 传递错误 | ✅ 已修复 |
| 2 | HandoffTeam.runChain() 等待 coordinator 完成带超时 | ✅ 已修复 |
| 3 | HandoffOrchestrator.Complete()/Error() 使用 sync.Once | ✅ 已修复 |
| 4 | ExtractHandoffSignal 两层提取策略 | ✅ 已修复 |
| 5 | ContainerAgent 中断信号处理 | ✅ 已修复 |
| 6 | SubscriptionManager 使用 fnmatch 通配符 | ✅ 已修复 |
| 7 | TeamRuntime.wrapProvider 自动绑定 RuntimeBindable | ✅ 已修复 |
| 8 | MessageBus 双检锁 ensureSubscription | ✅ 已修复 |
| 9 | CommunicableAgent.BindRuntime 幂等和重绑定警告 | ✅ 已修复 |
| 10 | HandoffTeam filterInterruptHistory 过滤中断项 | ✅ 已修复 |
| 11 | HierarchicalTeam assertReady 校验 | ✅ 已修复 |
| 12 | resetInternalAgents() 在 initLock 保护下重置 | ⚠️ 部分修复（ensureInternalAgents 未获锁） |

---

## 七、优先修复建议

### P0 — 必须立即修复（运行时会崩溃/数据损坏）

1. **S-01** DeepAgent.runTaskLoopInvoke err 变量遮蔽 — 错误被静默吞掉
2. **S-02** DeepAgent.ensureInitialized TOCTOU 竞态 — 并发初始化问题
3. **S-03** completeSessionSpawn 无锁读取 — 数据竞争
4. **S-06** HandoffTeam.coordinatorRegistry 无并发保护 — map 并发读写 panic
5. **S-07** HandoffTeam.resetInternalAgents sync.Once 重置竞态

### P1 — 近期修复（功能缺失/行为严重不一致）

6. **S-05** TeamRuntime.Send()/Publish() 缺少自动启动
7. **S-08** DeepAgentInterface 方法覆盖率不足
8. **S-11** LoopCoordinatorInterface 缺失核心方法
9. **S-12** DeepAgentConfig.Subagents 丢失 DeepAgent 实例支持
10. **S-14** DeepAgentInterface 引入具体实现包依赖

### P2 — 计划修复（行为差异/缺失校验）

11. **S-04** PrepareRound channel close 安全性
12. **G-02** SessionSpawnExecutor 子Agent 未传 Session
13. **G-12** HandoffTeam.Stream() 真正流式输出
14. **G-23** LoopQueues 满队列行为

---

## 八、总结

本次审查覆盖了 9.1 DeepAgent、9.6-9.7 TaskLoop/SessionSpawn、Multi-Agent 对齐修复、harness 辅助模块四个领域，共发现 **59 个问题**（16 严重 / 27 一般 / 16 提示）。

**核心风险**：
1. **并发安全**是最大隐患 — 从 Python asyncio 迁移到 Go 时，GIL 保护消失后多处 map/struct 并发访问未加锁，5 个严重问题与此相关
2. **接口抽象不足** — DeepAgentInterface/LoopCoordinatorInterface/SubagentSpec 覆盖率过低，导致外部消费者无法通过接口驱动完整生命周期
3. **错误处理不一致** — 多处 err 变量遮蔽/返回模式不统一，可能导致错误被静默吞掉

**建议修复顺序**：先修 P0（5个并发安全问题），再修 P1（5个接口/功能缺失），最后补齐 P2。
