// Package coordination 提供团队 Agent 的事件驱动唤醒层。
//
// 把传输层来的 EventMessage 与内部 poll 计时器产生的 InnerEventMessage
// 收成统一的 CoordinationEvent，按粗筛规则放行后由 CallbackFramework
// 分发到具体的场景 handler。自身不做业务决策——handler 通过三类 narrow
// protocol（AgentRoundController / TeamLifecycleController / PollController）
// 触发行为，最终驱动 DeepAgent + team tools。
//
// 三条铁律：
//  1. coordination 不做决策，只管 wake-up
//  2. 每个 handler 一个业务域，跨域协作走 framework fan-out
//  3. CallbackFramework.trigger() 吞普通异常（log + continue），仅 AbortError 上抛
//
// 文件目录：
//
//	coordination/
//	├── doc.go                 # 包文档
//	├── event_bus.go           # EventBus 事件入队 + 周期 poll timer + 生命周期
//	├── dispatcher.go          # EventDispatcher 粗筛 + CallbackFramework 分发 + 3 个 Protocol
//	├── kernel.go              # CoordinationKernel 协调子系统 facade
//	├── types/                 # 共享类型子包（打破 coordination ↔ handlers 循环依赖）
//	│   ├── events.go          # CoordinationEvent / InnerEventType / InnerEventMessage
//	│   ├── protocols.go       # AgentRoundController / TeamLifecycleController / PollController / DispatcherHost / Blueprint / Infra
//	│   └── callbacks.go       # EventCallbackFunc / CallbacksProvider
//	└── handlers/              # 场景 handler 子包
//	    ├── base.go            # BaseCoordinationHandler 基类
//	    ├── agent_lifecycle.go # Agent 生命周期事件（USER_INPUT / STANDBY / CLEANED / TOOL_APPROVAL_RESULT）
//	    ├── member.go          # 成员事件（MEMBER_* 6 种）
//	    ├── message.go         # 消息事件（MESSAGE / BROADCAST / POLL_MAILBOX + MEMBER_SHUTDOWN fan-out）
//	    ├── task_board.go      # 任务板事件（TASK_CLAIMED / TASK_* 5 种）
//	    ├── stale_task.go      # 过期任务轮询（POLL_TASK）
//	    └── team_completion.go # 团队完成（POLL_TASK / TASK_LIST_DRAINED / TEAM_COMPLETED）
//
// 对应 Python 代码：openjiuwen/agent_teams/agent/coordination/
package coordination
