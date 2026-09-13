// Package events 提供团队事件类型定义。
//
// 本包从 schema 包提取而来，包含所有团队事件的类型、接口、常量和 Topic 路由。
// 提取目的是打断 schema ←→ team_workspace 的循环依赖：
// schema/blueprint.go 导入 team_workspace.TeamWorkspaceConfig，
// 而 team_workspace 需要导入事件类型（TypedEvent、WorkspaceArtifactEvent 等）。
// 将事件类型独立为子包后，team_workspace 可安全导入 schema/events 而不形成循环。
//
// 本包零外部导入（仅使用 map[string]any 内建类型），不依赖 schema 父包。
//
// 文件目录：
//
//	events/
//	├── doc.go               # 包文档
//	├── base.go              # BaseEventMessage + TypedEvent 接口 + EventMessage + NewEventMessage + EventMessageFromEvent
//	├── topic.go             # TeamTopic 类型 + Build 方法
//	├── constants.go         # TeamEvent* 字符串常量
//	├── team_events.go       # 团队生命周期事件（TeamCreated/Cleaned/Standby/Completed）
//	├── member_events.go     # 成员生命周期事件（Spawned/Restarted/StatusChanged/ExecutionChanged/Shutdown/Canceled）
//	├── task_events.go       # 任务事件（Created/Claimed/Completed/Cancelled/Updated/Unblocked/ListDrained/PlanRequest/PlanResponse）
//	├── message_events.go    # 消息事件（MessageEvent + BroadcastEvent）
//	├── workspace_events.go  # 工作空间事件（Artifact/Conflict/LockRequest/LockResponse）
//	├── worktree_events.go   # Worktree 事件（Created/Removed）
//	└── approval_events.go   # 审批事件（PlanApproval + ToolApprovalResult）
//
// 对应 Python 代码：openjiuwen/agent_teams/schema/events.py
package events
