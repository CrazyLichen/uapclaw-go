// Package handlers 提供场景级协调事件处理器。
//
// 每个 handler 类拥有一个业务域：agent 生命周期、成员事件、消息、
// 任务板、过期任务清扫、团队完成。它们共享 DispatcherHost 契约，
// 通过 BaseCoordinationHandler.GetCallbacks 注册到 EventDispatcher。
//
// 文件目录：
//
//	handlers/
//	├── doc.go              # 包文档
//	├── base.go             # BaseCoordinationHandler 基类 + EventCallback 类型
//	├── agent_lifecycle.go  # AgentLifecycleHandler（USER_INPUT / STANDBY / CLEANED / TOOL_APPROVAL_RESULT）
//	├── member.go           # MemberHandler（MEMBER_* 6 种）
//	├── message.go          # MessageHandler（MESSAGE / BROADCAST / POLL_MAILBOX + MEMBER_SHUTDOWN fan-out）
//	├── task_board.go       # TaskBoardHandler（TASK_CLAIMED / TASK_* 5 种）
//	├── stale_task.go       # StaleTaskHandler（POLL_TASK）
//	└── team_completion.go  # TeamCompletionHandler（POLL_TASK / TASK_LIST_DRAINED / TEAM_COMPLETED）
//
// 对应 Python 代码：openjiuwen/agent_teams/agent/coordination/handlers/
package handlers
