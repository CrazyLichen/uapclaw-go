// Package callback 提供统一的回调框架，支持事件注册与触发。
//
// 本包是所有领域（LLM、Tool、Session、Agent、Workflow 等）共享的回调基础设施，
// 与 Python 中 openjiuwen/core/runner/callback/ 对应。
//
// 2.14 节实现 LLM 相关事件的回调框架最小子集。
// 5.3 节扩展 Session 事件维度（OnSession/OffSession/TriggerSession）。
// 5.19 节扩展 Context 事件维度（OnContext/OffContext/TriggerContext）。
// SW-31/32/33 扩展自定义事件维度（OnCustom/OffCustom/OffAllCustom/TriggerCustom），
// 支持动态事件名（如 sessionID+"write_stream"），对应 Python 的 trigger(event, **kwargs)。
// 后续 Workflow 等领域扩展时，在同一框架中新增事件类型。
// 完整能力（过滤器/熔断器/链式执行/装饰器/transform_io）在 6.24 节实现。
// 6.6 节引入 CallbackInfo[F] 泛型包装，统一所有域的回调函数类型签名；
// 引入 Functional Options 模式（CallbackOption）配置回调行为；
// 引入 PerAgent 域（OnPerAgent/OffPerAgent/TriggerPerAgent 等），支持 per-Agent 实例级回调。
//
// 事件体系：
//
//	LLMCallEventType      — LLM 调用生命周期事件（9 种），预定义枚举事件名
//	ToolCallEventType     — Tool 调用生命周期事件（11 种），预定义枚举事件名
//	SessionCallEventType  — Session 生命周期事件（2 种），预定义枚举事件名
//	ContextCallEventType  — Context 生命周期事件（5 种），预定义枚举事件名
//	GlobalAgentEventType  — Agent 调用全局事件（5 种），预定义枚举事件名
//	CustomCallbackFunc    — 自定义事件（自由字符串事件名 + map[string]any 数据）
//
// 设计说明：
//
//	Python 的 AsyncCallbackFramework 只有一个 _callbacks: Dict[str, List]，
//	所有事件（包括 "abc-123write_stream" 这类动态事件名）共用同一注册表。
//	Go 将其拆分为六个独立 map：
//	  - LLM/Tool/Session/Context/Agent 域使用预定义枚举事件名和固定数据结构
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
//	├── callback_info.go      # CallbackInfo[F] 泛型包装
//	├── options.go            # Functional Options 模式（CallbackOption）
//	├── events.go             # 事件类型定义（LLM + Tool + Session + Context + Agent）
//	├── framework.go          # CallbackFramework 核心（含自定义事件 + PerAgent 域）
//	└── logging.go            # 默认日志回调
//
// 核心类型/接口索引：
//
//	CallbackInfo[F]          — 泛型回调包装，统一所有域的回调函数类型签名
//	CallbackOption           — Functional Option 配置回调行为
//	PerAgentCallbackFunc     — PerAgent 域回调函数类型
//	triggerStrategy          — 回调触发策略枚举（顺序执行/并行执行）
//	CallbackFramework        — 核心回调框架（多域注册/触发/注销）
//
// 对应 Python 代码：openjiuwen/core/runner/callback/
package callback
