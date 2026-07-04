# 每日代码 Review 报告

**日期**：2026-07-03
**审查范围**：最近 24 小时提交记录
**涉及章节**：8.35 HierarchicalTeam (msgbus)、8.36 HierarchicalTeam (tools)、9.5 LoopCoordinator + StopConditionEvaluator

---

## 审查概况

| 章节 | 严重 | 一般 | 提示 | 合计 |
|------|------|------|------|------|
| 8.35 HierarchicalTeam (msgbus) | 7 | 6 | 4 | 17 |
| 8.36 HierarchicalTeam (tools) | 3 | 7 | 4 | 14 |
| 9.5 LoopCoordinator | 2 | 6 | 5 | 13 |
| **合计** | **12** | **19** | **13** | **44** |

---

## 一、8.35 HierarchicalTeam (msgbus) — 17 个问题

### 1.1 严重问题（7 个）

#### S-8.35-01：max_agents 上限校验缺失

- **文件**：`hierarchical_msgbus/hierarchical_team.go` — AddAgent 方法
- **Python 参考**：`team.py:112-116` — `self.runtime.get_agent_count() >= self.config.max_agents`，超过上限抛 `AGENT_TEAM_ADD_RUNTIME_ERROR`
- **问题**：Go 版本 `AddAgent()` 完全缺失 `max_agents` 上限检查，无法限制团队中 Agent 数量，可能导致资源失控
- **建议修复**：在 `HasAgent` 检查通过后、`RegisterAgent` 之前加入 max_agents 校验

#### S-8.35-02：TeamCard.AgentCards 元数据未同步

- **文件**：`hierarchical_msgbus/hierarchical_team.go` — AddAgent 方法
- **Python 参考**：`team.py:119` — `self.card.agent_cards.append(card)`
- **问题**：Python 在注册 Agent 后会将 AgentCard 追加到 `TeamCard.agent_cards`，Go 版本缺失此步骤，导致无法通过 `team.Card().GetAgentCards()` 查询已注册的 Agent 列表
- **建议修复**：在 AddAgent 注册成功后追加 card 到 TeamCard 的 agent_cards 列表

#### S-8.35-03：sender 校验缺失

- **文件**：`hierarchical_msgbus/hierarchical_team.go` — Invoke/Stream 方法
- **Python 参考**：`team.py` 中 invoke/stream 会校验 sender 合法性
- **问题**：Go 版本在发送消息时未校验 sender 是否为已注册 Agent，任意 sender 都可发送消息
- **建议修复**：在 Invoke/Stream 中添加 sender 校验逻辑

#### S-8.35-04：CommunicableAgent 无并发安全保护

- **文件**：`team_runtime/communicable_agent.go:19-24, 58-73, 79-84`
- **问题**：`CommunicableAgent` 的 `runtime` 和 `agentID` 字段没有互斥保护。`BindRuntime` 写入字段，`IsBound`/`Send`/`Publish`/`Subscribe`/`Unsubscribe`/`Runtime` 读取。`BindRuntime` 与 `Send` 并发调用存在数据竞争；`IsBound` 的非原子读取（`c.runtime != nil && c.agentID != ""`）不是原子操作
- **Python 参考**：Python 有 GIL 保护，不存在此问题
- **建议修复**：为 `CommunicableAgent` 添加 `sync.RWMutex`，`BindRuntime` 用写锁，其他方法用读锁

#### S-8.35-05：TeamRuntime.p2pTimeout 无并发安全保护

- **文件**：`team_runtime/team_runtime.go:408-415`
- **问题**：`SetP2PTimeout` 和 `GetP2PTimeout`/`P2PTimeout` 直接读写 `tr.p2pTimeout` 字段，无锁保护。`p2pTimeout` 在 `Send` 方法中被读取，而 `SetP2PTimeout` 在 `HierarchicalTeam.AddAgent` 中被调用，两者可能在不同 goroutine 中并发执行
- **建议修复**：用 `tr.mu` 保护 `p2pTimeout` 的读写，或将 `p2pTimeout` 改为 `atomic.Float64`

#### S-8.35-06：P2PAbilityManager 缺少编译时接口满足检查

- **文件**：`hierarchical_msgbus/p2p_ability_manager.go`
- **问题**：`P2PAbilityManager` 声称实现了 `AbilityManagerInterface`，但源码中没有 `var _ AbilityManagerInterface = (*P2PAbilityManager)(nil)` 编译时断言。该检查仅存在于测试文件中，接口不满足时只有执行测试才能发现
- **建议修复**：在 `p2p_ability_manager.go` 中添加编译时接口断言

#### S-8.35-07：HierarchicalTeam (msgbus) pendingChildren 和 hierarchySetup 无锁保护

- **文件**：`hierarchical_msgbus/hierarchical_team.go:39-41`
- **问题**：`pendingChildren` (map) 和 `hierarchySetup` (bool) 字段被多个方法读写，在并发场景下存在数据竞争。Go 的 map 并发读写会直接 panic
- **Python 参考**：Python 版本因 GIL 和 async 单线程模型天然避免了此问题
- **建议修复**：添加 `sync.Mutex` 或 `sync.RWMutex` 保护这两个字段，或将 `hierarchySetup` 改为 `atomic.Bool`

### 1.2 一般问题（6 个）

#### G-8.35-01：AbilityManagerInterface 缺少 IsAgent 方法

- **文件**：`single_agent/interfaces/interface.go:24-51`
- **问题**：`P2PAbilityManager` 新增了 `IsAgent(name string) bool` 方法，但该方法未纳入 `AbilityManagerInterface`，通过接口引用无法调用 `IsAgent`
- **建议修复**：考虑将 `IsAgent(name string) bool` 加入 `AbilityManagerInterface`

#### G-8.35-02：P2PAbilityManager 参数解析失败后静默降级

- **文件**：`hierarchical_msgbus/p2p_ability_manager.go:177-180`
- **问题**：参数解析失败时仅 `toolArgs = map[string]any{}`，不记录任何日志。与 `AbilityManager` 的严格模式不一致（AbilityManager 使用 `StatusAbilityMalformedArguments` 错误码）
- **建议修复**：参数解析失败时记录 Warn 日志，与 AbilityManager 的严格模式保持一致

#### G-8.35-03：errorToP2PResult 缺少 event_type/method/model_provider 上下文字段

- **文件**：`hierarchical_msgbus/p2p_ability_manager.go:238-246`
- **问题**：按项目日志同步规则（规则 3.4），异常路径日志必须包含 `event_type=LLM_CALL_ERROR`、`method`、`model_provider` 等上下文字段，当前缺失
- **建议修复**：在 `errorToP2PResult` 中添加 `exception.WithParam("method", "P2PAbilityManager.Execute")` 等上下文参数

#### G-8.35-04：SupervisorAgent.Create 使用 panic 报告错误

- **文件**：`hierarchical_msgbus/supervisor_agent.go:91-100`
- **问题**：`Create` 函数在 `agentsList` 为空或包含 nil 元素时使用 `panic` 而非返回 `error`。Go 惯用法中 `panic` 仅用于"不可能发生"的编程错误
- **建议修复**：将 `panic` 改为返回 `error`，或至少在注释中说明 panic 的原因

#### G-8.35-05：IsAgent 存在 TOCTOU 风险

- **文件**：`hierarchical_msgbus/p2p_ability_manager.go:150-156`
- **问题**：`IsAgent` 返回 true 到 `executeSingleP2P` 执行之间，能力可能被并发 `Remove`，构成 Time-of-check to time-of-use 问题
- **建议修复**：在 `IsAgent` 注释中标注 TOCTOU 风险

#### G-8.35-06：SupervisorAgent 的 CommunicableAgent 和 ReActAgent 嵌入方法集约束未文档化

- **文件**：`hierarchical_msgbus/supervisor_agent.go:27-32`
- **问题**：如果未来 `ReActAgent` 新增了与 `CommunicableAgent` 同名的方法（如 `Runtime`），将产生编译时歧义错误，当前缺少文档约束
- **建议修复**：在 `SupervisorAgent` 的 doc comment 中明确列出两个嵌入体的方法集，标注"不可重叠"约束

### 1.3 提示问题（4 个）

#### T-8.35-01：AbilityManagerInterface.Execute 签名返回类型路径歧义

- **文件**：`single_agent/interfaces/interface.go:40-46`
- **问题**：接口返回 `agentschema.ExecuteResult`，实现者的 `ExecuteResult` 通过 `ability` 包的类型别名定义，两条路径最终指向同一类型，概念上有歧义但不影响编译
- **建议**：在接口注释中明确说明 `ExecuteResult` 的实际定义位置

#### T-8.35-02：Communicable 接口与 BaseTeam 接口缺少桥接文档

- **文件**：`team_runtime/communicable_interface.go`
- **问题**：`Communicable.Send` 的 `message any` 与 `BaseTeam.Send` 的 `map[string]any` 参数类型不同，语义合理但缺少文档说明
- **建议**：在接口注释中说明设计意图

#### T-8.35-03：AbilityManager.executeSingleToolCall 中路由判断与 IsAgent 逻辑不一致

- **文件**：`single_agent/ability/ability_manager.go`
- **问题**：`executeSingleToolCall` 使用 `if _, ok := am.agents[toolName]; ok` 直接访问私有 map，与 `IsAgent` 的 `AbilityKind()` 判断路径不一致
- **建议**：统一使用 `AbilityKind()` 判断

#### T-8.35-04：P2PAbilityManager 日志组件与同一调用链中其他模块不一致

- **文件**：`hierarchical_msgbus/p2p_ability_manager.go:48`
- **问题**：`p2pLogComponent = cschema.ComponentChannel`，而 `communicable_agent.go` 使用 `ComponentAgentCore`，同一调用链中日志组件标识不统一
- **建议**：统一 `hierarchical_msgbus` 包内所有文件的日志组件标识

---

## 二、8.36 HierarchicalTeam (tools) — 14 个问题

### 2.1 严重问题（3 个）

#### S-8.36-01：setupHierarchy 成功后未清理 pendingChildren

- **文件**：`hierarchical_tools/hierarchical_team.go:447-502`
- **Python 参考**：`hierarchical_team.py:113` — `self._pending_children.clear()`
- **问题**：Python 的 `_setup_hierarchy()` 在成功注册所有子 Agent 后会调用 `self._pending_children.clear()` 清空待注册队列。Go 版本成功后仅设置 `hierarchySetup = true`，未清理 `pendingChildren` map，残留数据浪费内存且可能导致重复注册
- **建议修复**：在 `setupHierarchy` 成功后清理 `pendingChildren`：`t.pendingChildren = make(map[string][]*agentschema.AgentCard)`

#### S-8.36-02：AddAgent 未做 max_agents 上限检查

- **文件**：`hierarchical_tools/hierarchical_team.go:248-285`
- **Python 参考**：`team.py:112-116` — max_agents 边界检查
- **问题**：Go 版本的 `AddAgent()` 完全缺失此检查，无法限制团队中 Agent 数量
- **建议修复**：在 `AddAgent` 中 `HasAgent` 检查通过后加入 max_agents 校验

#### S-8.36-03：pendingChildren 和 hierarchySetup 无锁保护

- **文件**：`hierarchical_tools/hierarchical_team.go:39-41`
- **问题**：并发场景下存在数据竞争，Go 的 map 并发读写会直接 panic
- **建议修复**：添加 `sync.Mutex` 或 `sync.RWMutex` 保护

### 2.2 一般问题（7 个）

#### G-8.36-01：AddAgent 未将 AgentCard 追加到 TeamCard.AgentCards

- **文件**：`hierarchical_tools/hierarchical_team.go:248-285`
- **Python 参考**：`team.py:119` — `self.card.agent_cards.append(card)`
- **问题**：无法通过 `team.Card().GetAgentCards()` 查询已注册的 Agent 列表
- **建议修复**：在 AddAgent 注册成功后追加 card 到 TeamCard 的 agent_cards 列表

#### G-8.36-02：RemoveAgent 未清理 TeamCard.AgentCards 和 pendingChildren

- **文件**：`hierarchical_tools/hierarchical_team.go:316-334`
- **Python 参考**：`team.py:133-136` — 从 `self.card.agent_cards` 中移除
- **问题**：移除的 Agent 仍可能出现在 TeamCard 的 agent_cards 列表和 pendingChildren 中
- **建议修复**：在 RemoveAgent 中同步清理 TeamCard 元数据和 pendingChildren 引用

#### G-8.36-03：AddAgent 未处理 opts 中的 ParentAgentID

- **文件**：`hierarchical_tools/hierarchical_team.go:248-285`
- **Python 参考**：`hierarchical_team.py:58-84` — `add_agent(card, provider, parent_agent_id=None)` 单一方法
- **问题**：`WithParentAgentID` Option 虽已定义但 `AddAgent` 方法中未处理，用户通过 `team.AddAgent(ctx, card, provider, maschema.WithParentAgentID("parent"))` 调用时父子关系不会被记录
- **建议修复**：在 AddAgent 方法中处理 opts 中的 ParentAgentID，若有则自动记录到 pendingChildren

#### G-8.36-04：Stream 方法缺少 Python 中的 try/except 错误恢复机制

- **文件**：`hierarchical_tools/hierarchical_team.go:215-231`
- **Python 参考**：`hierarchical_team.py:169-172` — `except Exception as e` + error_result
- **问题**：如果 `agent.Stream()` 返回的 channel 中包含错误或 panic，没有机制将错误信息写入流
- **建议修复**：在 range ch 循环外增加 `defer recover()` 或在 chunk 中检测错误类型

#### G-8.36-05：setupHierarchy 中 ResourceMgr 为 nil 时静默跳过

- **文件**：`hierarchical_tools/hierarchical_team.go:457-464`
- **Python 参考**：Python 中 ResourceMgr 未初始化会抛异常，不会静默跳过
- **问题**：如果有 pendingChildren 但 ResourceMgr 为空，层级关系不会被建立，但方法返回 nil error，调用者误以为正常
- **建议修复**：当 `len(t.pendingChildren) > 0` 但 ResourceMgr 为 nil 时返回 error

#### G-8.36-06：Stream 方法未传播 timeout 选项

- **文件**：`hierarchical_tools/hierarchical_team.go:165-240`
- **问题**：`Invoke()` 方法正确传递了 timeout，但 `Stream()` 方法未使用 timeout，对比 msgbus 版本已正确传递
- **建议修复**：参考 msgbus 实现，在 Stream 中提取并传递 timeout

#### G-8.36-07：日志组件使用 ComponentChannel，与项目规范不一致

- **文件**：`hierarchical_tools/hierarchical_team.go:48`
- **项目规范**：规则 3.2 — `ComponentAgentCore` 适用于 `agentcore/*` 下所有包
- **问题**：应使用 `ComponentAgentCore` 而非 `ComponentChannel`
- **建议修复**：改为 `toolsLogComponent = logger.ComponentAgentCore`

### 2.3 提示问题（4 个）

#### T-8.36-01：HierarchicalToolsTeamConfig 未强制要求 RootAgent

- **文件**：`hierarchical_tools/hierarchical_config.go:13-18`
- **Python 参考**：`hierarchical_config.py:17` — `root_agent: AgentCard = Field(...)`
- **问题**：Go 版本允许 RootAgent 为 nil，直到 `assertReady()` 时才报错，Python 在配置创建时就暴露问题
- **建议**：在文档中明确标注 RootAgent 是运行时必填项

#### T-8.36-02：GetRuntime() 方法不在 BaseTeam 接口中

- **文件**：`hierarchical_tools/hierarchical_team.go:412-414`
- **问题**：通过 `BaseTeam` 接口引用无法访问 Runtime，需做类型断言
- **建议**：考虑是否需要在 BaseTeam 接口中添加 GetRuntime()

#### T-8.36-03：assertReady 错误消息已对齐 Python，rootAgentID=="" 检查是合理增强

- **文件**：`hierarchical_tools/hierarchical_team.go:424-438`
- **说明**：此为正面确认，Go 版本多了一个 rootAgentID=="" 的额外检查，是合理的增强

#### T-8.36-04：inputs 仅接受 map[string]any，Python 接受 Any 类型

- **文件**：`hierarchical_tools/hierarchical_team.go:109, 165`
- **说明**：Go 类型系统的合理限制，功能上可通过 `map[string]any{"query": input}` 包装

---

## 三、9.5 LoopCoordinator + StopConditionEvaluator — 13 个问题

### 3.1 严重问题（2 个）

#### S-9.5-01：评估器 Name() 返回值与 Python 不一致

- **文件**：`task_loop/stop_condition.go:144, 165, 186, 208`
- **Python 参考**：`stop_condition.py:54-56` — `return self.__class__.__name__`
- **问题**：Go 版本返回小写下划线格式（`max_rounds`、`token_budget`），Python 返回类名格式（`MaxRoundsEvaluator`、`TokenBudgetEvaluator`）。这导致 `stop_reason` 和 `evaluator_states` 的 key 在 Python/Go 间不一致，跨语言状态持久化（checkpoint）会失败
- **建议修复**：将 Name() 返回值改为与 Python 一致的类名格式：`"MaxRoundsEvaluator"`、`"TokenBudgetEvaluator"`、`"TimeoutEvaluator"`、`"CompletionPromiseEvaluator"`

#### S-9.5-02：CompletionPromiseEvaluator 缺少并发保护（数据竞争）

- **文件**：`task_loop/stop_condition.go:73-84, 252-264, 200-204`
- **问题**：`NotifyFulfilled()`、`NotifyAbsent()` 和 `ShouldStop()` 都没有加锁。当 `LoopCoordinator.ShouldContinue()` 调用 `ev.ShouldStop()` 读取 `fulfilled` 字段时，另一个 goroutine 可能并发调用 `NotifyFulfilled()` 写入 `fulfilled` 和 `confirmationCount`，造成数据竞争
- **Python 参考**：Python 有 GIL 保护，不需要显式锁
- **建议修复**：为 `CompletionPromiseEvaluator` 添加 `sync.Mutex`，在 `ShouldStop()`、`NotifyFulfilled()`、`NotifyAbsent()`、`ExportState()`、`ImportState()`、`Reset()`、`Confirmations()` 中加锁

### 3.2 一般问题（6 个）

#### G-9.5-01：CustomPredicateEvaluator Name 默认值与 Python 不一致

- **文件**：`task_loop/stop_condition.go:131-133`
- **Python 参考**：`stop_condition.py:226-241` — name 固定为类名
- **问题**：Go 版本要求构造时传入 `name` 参数，默认行为与 Python 不同
- **建议修复**：提供 `NewDefaultCustomPredicateEvaluator(predicate)` 便捷构造函数使用默认名 `"CustomPredicateEvaluator"`

#### G-9.5-02：NewLoopCoordinator 初始化 startTime 的时机与 Python 不同

- **文件**：`task_loop/loop_coordinator.go:48-56`
- **Python 参考**：`loop_coordinator.py:48` — `self._start_time: float = 0.0`
- **问题**：Go 构造时直接设 `startTime = time.Now()`，Python 需显式调用 `reset()`。Go 做法更合理（避免忘记 reset），但行为有差异
- **建议修复**：在注释中明确说明这是与 Python 的行为差异

#### G-9.5-03：ShouldContinue 持锁期间调用评估器，评估器也可能持锁

- **文件**：`task_loop/loop_coordinator.go:63-95`
- **问题**：如果评估器也加锁（修复 S-9.5-02 后），持锁时间会很长。CustomPredicateEvaluator 的用户函数如果执行慢，会阻塞其他对 LoopCoordinator 的访问
- **建议修复**：先在锁内快照状态，然后释放锁，再在无锁状态下调用评估器

#### G-9.5-04：NewCustomPredicateEvaluator 未校验 predicate 为 nil

- **文件**：`task_loop/stop_condition.go:131-133, 283-285`
- **问题**：传入 `predicate = nil` 时 `ShouldStop()` 会 panic，被 recover 捕获后静默跳过，难以排查
- **建议修复**：在构造时加 nil 检查，给出明确错误信息

#### G-9.5-05：Python logger.warning 带 exc_info=True，Go 缺少堆栈信息

- **文件**：`task_loop/loop_coordinator.go:78-81`
- **Python 参考**：`loop_coordinator.py:133-137` — `exc_info=True`
- **问题**：Go 版本用 recover 捕获 panic 后只记录了 `Any("panic", r)`，缺少堆栈信息
- **建议修复**：补充 `.Stack()` 或 `.Caller()` 提供调用位置

#### G-9.5-06：ExportState 返回空 map vs Python 返回 None

- **文件**：`task_loop/stop_condition.go:148, 169, 190, 294` 和 `loop_coordinator.go:193`
- **Python 参考**：`stop_condition.py:80` — `return None`；`loop_coordinator.py:163` — `if s is not None`
- **问题**：无状态评估器的 `ExportState()` 返回 `map[string]any{}`（空 map），Python 返回 `None`。空 map 不等于 nil，会被包含在 `EvaluatorStates` 中，Python 不会包含无状态评估器
- **建议修复**：无状态评估器的 `ExportState()` 返回 `nil` 而非 `map[string]any{}`

#### G-9.5-07：缺少 TimeoutEvaluator 在 LoopCoordinator 中的集成测试

- **文件**：`task_loop/loop_coordinator_test.go`
- **Python 参考**：`test_loop_coordinator.py:68-74` — `test_timeout_stop` 使用 `timeout_seconds=0.0`
- **问题**：Go 测试使用 `TimeoutEvaluator(3600.0)` 超时值太大，没有触发超时的测试
- **建议修复**：添加超时触发测试，构造 `NewTimeoutEvaluator(0.0)` 验证立即停止

### 3.3 提示问题（5 个）

#### T-9.5-01：Python 测试中总是先调 reset()，Go 测试不需要

- **文件**：`task_loop/loop_coordinator_test.go`
- **说明**：Go 构造函数已设 startTime，不需要显式 reset，这是改进

#### T-9.5-02：buildEvalContext 中 LastResult 是浅引用

- **文件**：`task_loop/loop_coordinator.go:258`
- **说明**：与 Python 行为一致（直接引用），评估器可能修改内部状态，但 Python 也是同样行为

#### T-9.5-03：MaxRoundsEvaluator/TokenBudgetEvaluator 未校验负数/零值

- **文件**：`task_loop/stop_condition.go:100-102, 106-108`
- **说明**：Python 也不校验，0 值语义为"立即停止"，可在注释中说明

#### T-9.5-04：日志对齐良好，无需修改

- **文件**：`task_loop/loop_coordinator.go:88-90`
- **说明**：Go 版本使用结构化字段优于 Python 的格式化参数，已对齐

#### T-9.5-05：缺少 CustomPredicateEvaluator 在 LoopCoordinator 中的独立测试

- **文件**：`task_loop/loop_coordinator_test.go`
- **Python 参考**：`test_loop_coordinator.py:77-92`
- **建议**：添加独立测试用例

---

## 四、优先修复建议

### 最高优先级（严重问题，影响运行时正确性或安全性）

| 编号 | 问题 | 影响 |
|------|------|------|
| S-8.35-04 | CommunicableAgent 无并发安全保护 | `BindRuntime` 与 `Send` 并发时数据竞争，go race detector 会报告 |
| S-8.35-05 | TeamRuntime.p2pTimeout 无并发安全保护 | `SetP2PTimeout` 与 `Send` 并发时数据竞争 |
| S-8.35-07 | HierarchicalTeam (msgbus) pendingChildren 无锁保护 | map 并发读写直接 panic |
| S-8.36-03 | HierarchicalTeam (tools) pendingChildren 无锁保护 | 同上 |
| S-9.5-02 | CompletionPromiseEvaluator 缺少并发保护 | NotifyFulfilled 与 ShouldStop 并发时数据竞争 |
| S-8.35-01 | max_agents 上限校验缺失 (msgbus) | Agent 数量无上限保护 |
| S-8.36-02 | max_agents 上限校验缺失 (tools) | 同上 |
| S-9.5-01 | 评估器 Name() 与 Python 不一致 | 跨语言状态持久化失败 |
| S-8.35-02 | TeamCard.AgentCards 元数据未同步 (msgbus) | 无法查询已注册 Agent 列表 |
| S-8.35-03 | sender 校验缺失 (msgbus) | 任意 sender 可发送消息 |
| S-8.35-06 | P2PAbilityManager 缺少编译时接口断言 | 接口不匹配只能在测试时发现 |
| S-8.36-01 | pendingChildren 未清理 (tools) | 可能导致子 Agent 重复注册 |

### 高优先级（一般问题，影响功能正确性或规范合规性）

| 编号 | 问题 | 影响 |
|------|------|------|
| G-8.35-02 | P2PAbilityManager 参数解析失败静默降级 | 异常路径无日志，与 Python 严格模式不一致 |
| G-8.35-03 | errorToP2PResult 缺少上下文字段 | 违反日志同步规则 3.4 |
| G-8.36-01 | AddAgent 未追加 AgentCards (tools) | Python 测试验证的行为缺失 |
| G-8.36-03 | AddAgent 未处理 WithParentAgentID Option | WithParentAgentID 定义但无效 |
| G-8.36-06 | Stream 未传播 timeout (tools) | msgbus 版本正确传递，tools 版本遗漏 |
| G-9.5-06 | ExportState 返回空 map vs nil | 与 Python 的 evaluator_states 内容不一致 |
| G-9.5-05 | recover 缺少堆栈信息 | 违反日志同步规则 3.3 |

---

## 五、总体评价

### 功能符合度

- **8.35 HierarchicalTeam (msgbus)**：核心功能（P2PAbilityManager 分区派发、SupervisorAgent 双重嵌入、HierarchicalTeam 13 方法）与 Python 基本对齐，但缺少 max_agents 校验、sender 校验、TeamCard 元数据同步等边界处理
- **8.36 HierarchicalTeam (tools)**：工具委托模式核心逻辑与 Python 对齐，但 AddAgent 缺少多个 Python 中存在的后处理步骤（AgentCards 追加、pendingChildren 清理、ParentAgentID 处理）
- **9.5 LoopCoordinator**：循环协调逻辑与 Python 高度对齐，5 个评估器功能完整，但 Name() 返回值和 ExportState 返回值与 Python 不一致

### 实现质量

- **并发安全**：这是最大的系统性问题。Python 有 GIL 保护不需要显式锁，Go 的 goroutine 并发模型要求所有共享可变状态必须有同步保护。当前 3 个严重并发安全问题（CommunicableAgent、p2pTimeout、pendingChildren/CompletionPromiseEvaluator）需要优先修复
- **日志规范**：多处违反日志同步规则（规则 3.3/3.4），异常路径缺少上下文字段和堆栈信息
- **错误处理**：部分路径静默降级（P2PAbilityManager 参数解析、ResourceMgr 为 nil），应返回 error 或记录日志

### 建议修复顺序

1. **第一批**：修复 5 个并发安全问题（S-8.35-04, S-8.35-05, S-8.35-07, S-8.36-03, S-9.5-02）
2. **第二批**：修复 2 个 max_agents 校验缺失（S-8.35-01, S-8.36-02）+ 1 个 pendingChildren 清理（S-8.36-01）+ 1 个 Name() 对齐（S-9.5-01）
3. **第三批**：修复 TeamCard 元数据同步（S-8.35-02）+ sender 校验（S-8.35-03）+ 编译时断言（S-8.35-06）
4. **第四批**：修复一般问题中的日志规范和功能符合度问题
