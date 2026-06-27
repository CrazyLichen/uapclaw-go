// Package callback 提供统一的异步回调框架，支持事件注册、触发、过滤器、钩子、
// 链式执行、熔断器、指标记录、历史回放和 transform_io 变换。
//
// 本包是所有领域（LLM、Tool、Session、Agent、Workflow、Memory、TaskManager 等）共享的
// 回调基础设施，与 Python 中 openjiuwen/core/runner/callback/ 对应。
//
// 6.24 节已实现完整能力：
//   - 事件注册/触发/注销（11 个域：LLM/Tool/Session/Context/Agent/PerAgent/Workflow/
//     AgentTeam/Retrieval/Memory/TaskManager + 自定义事件）
//   - BEFORE/AFTER/ERROR/Cleanup 生命周期钩子（AddHook）
//   - 三级过滤器管线（全局 → 事件级 → 回调级，7 种过滤器实现）
//   - 链式执行（CallbackChain：顺序执行 + 回滚 + 重试 + 错误处理）
//   - 熔断器（AddCircuitBreaker + CircuitBreakerFilter）
//   - 执行指标记录（EnableMetrics + GetMetrics + GetSlowCallbacks）
//   - 事件历史回放（EnableEventHistory）
//   - CallbackInfo[F] 泛型包装 + Functional Options（WithPriority/WithOnce/WithMaxRetries 等）
//   - transform_io 变换（LLM/Agent/Tool 三层 IO 变换）
//   - 并发触发/条件触发/超时触发（TriggerParallel/TriggerUntil/TriggerWithTimeout）
//   - 全局单例 + 便捷触发（GetCallbackFramework/Trigger）
//
// 事件体系：
//
//	LLMCallEventType      — LLM 调用生命周期事件（9 种），预定义枚举事件名
//	ToolCallEventType     — Tool 调用生命周期事件（11 种），预定义枚举事件名
//	SessionCallEventType  — Session 生命周期事件（2 种），预定义枚举事件名
//	ContextCallEventType  — Context 生命周期事件（5 种），预定义枚举事件名
//	GlobalAgentEventType  — Agent 调用全局事件（5 种），预定义枚举事件名
//	WorkflowEventType     — Workflow 生命周期事件（16 种），预定义枚举事件名
//	AgentTeamEventType    — Agent 协作事件（2 种），预定义枚举事件名
//	RetrievalEventType    — 检索事件（1 种），预定义枚举事件名
//	MemoryEventType       — 记忆事件（5 种），预定义枚举事件名
//	TaskManagerEventType  — 任务管理事件（6 种），预定义枚举事件名
//	CustomCallbackFunc    — 自定义事件（自由字符串事件名 + map[string]any 数据）
//
// 设计说明：
//
//	Python 的 AsyncCallbackFramework 只有一个 _callbacks: Dict[str, List]，
//	所有事件（包括 "abc-123write_stream" 这类动态事件名）共用同一注册表。
//	Go 将其拆分为多个独立 map：
//	  - 各域使用预定义枚举事件名和固定数据结构
//	  - 自定义域使用自由字符串事件名和 map[string]any 数据
//	这样既保留了类型安全，又支持 Python 的动态事件名场景。
//
//	CallbackInfo[F] 泛型包装将所有域的回调函数统一为 CallbackInfo[FuncType] 结构，
//	每个域的注册/触发方法内部使用 CallbackInfo 包装后存入注册表。
//	PerAgent 域通过命名空间隔离（{agentID}_{event}）实现 per-Agent 实例级回调，
//	与 GlobalAgent 域的全局观测回调互补。
//
// 文件目录：
//
//	callback/
//	├── doc.go                # 包文档
//	├── framework.go          # CallbackFramework 核心（注册/触发/注销/钩子/指标/熔断器/历史）
//	├── enums.go              # FilterAction / ChainAction / HookType 枚举
//	├── models.go             # CallbackMetrics / FilterResult / ChainContext / ChainResult / CallbackInfo[F]
//	├── events.go             # 事件类型定义（scope + 所有域枚举 + EventData + 函数类型）
//	├── filters.go            # EventFilter 接口 + 7 种过滤器
//	├── chain.go              # CallbackChain（顺序执行+回滚+重试）
//	├── errors.go             # AbortError
//	├── utils.go              # 全局单例 + Trigger 便捷函数
//	└── options.go            # CallbackOption（Functional Options 模式）
//
// 核心类型/接口索引：
//
//	CallbackFramework        — 核心回调框架（多域注册/触发/注销/钩子/过滤器/指标/熔断器）
//	CallbackInfo[F]          — 泛型回调包装，统一所有域的回调函数类型签名
//	CallbackChain            — 链式回调执行链（回滚/重试/错误处理）
//	CallbackOption           — Functional Option 配置回调行为
//	CallbackMetrics          — 执行指标（调用次数/耗时/错误率）
//	EventFilter              — 过滤器接口（7 种实现：RateLimit/CircuitBreaker/Validation/Logging/Auth/ParamModify/Conditional）
//	AbortError               — 回调中止错误
//	FilterAction             — 过滤器动作枚举（Continue/Stop/Skip/Modify）
//	ChainAction              — 链式执行动作枚举（Continue/Break/Retry/Rollback）
//	HookType                 — 钩子类型枚举（Before/After/Error/Cleanup）
//	FilterResult             — 过滤器返回结果
//	ChainContext             — 链式执行上下文
//	ChainResult              — 链式执行结果
//	triggerStrategy          — 回调触发策略枚举（顺序执行/并行执行）
//
// 对应 Python 代码：openjiuwen/core/runner/callback/
package callback
