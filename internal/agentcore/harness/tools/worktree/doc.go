// Package worktree 提供 Git Worktree 隔离能力，让每个 Agent 在独立的工作树中执行任务，
// 避免并发操作对主仓库的干扰。
//
// 本包实现完整的 Worktree 生命周期管理：创建/恢复、进入/退出、变更检测、批量清理、
// session 状态持久化、事件分发和 lifecycle hook 体系。
// 对齐 Python openjiuwen/harness/tools/worktree/。
//
// 文件目录：
//
//	worktree/
//	├── doc.go           # 包文档
//	├── models.go        # 核心数据结构与枚举定义
//	├── slug.go          # Slug 校验 + 分支名/路径生成
//	├── events.go        # harness 层事件类型 + WorktreeEventHandler
//	├── session.go       # WorktreeSessionState（context.Value 指针模式）
//	├── git.go           # Git 命令同步封装
//	├── backend.go       # WorktreeBackend 接口 + GitBackend + 注册表
//	├── manager.go       # WorktreeManager 核心逻辑
//	├── notice.go        # BuildWorktreeNotice 上下文提示
//	├── cleanup.go       # CleanupStaleWorktrees 过期清理
//	├── tools.go         # worktreeToolBase + Enter/ExitWorktreeTool
//	└── rails.go         # WorktreeRail + WorktreeLifecycleRail + AutoSetupRail + DiffSummaryRail
//
// 对应 Python 代码：openjiuwen/harness/tools/worktree/
package worktree
