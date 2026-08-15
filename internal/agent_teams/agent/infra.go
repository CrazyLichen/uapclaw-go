package agent

import (
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamInfra 每进程团队基础设施。
// 四象限分解的第三象限：每进程可达的基础设施资源。
// Leader 和 Teammate 在不同进程运行，"共享"是进程级范围而非跨实例单例。
// 对齐 Python: TeamInfra (openjiuwen/agent_teams/agent/infra.py)
type TeamInfra struct {
	// Messager 消息总线
	Messager messager.Messager
	// TeamBackend 团队后端（DB + task/message managers）
	TeamBackend *tools.TeamBackend
	// WorkspaceManager 团队工作空间管理器
	// TODO(#9.66): TeamWorkspaceManager 类型
	WorkspaceManager any
	// WorkspaceInitialized 工作空间是否已初始化
	WorkspaceInitialized bool
	// TaskManager 任务管理器（概念上从 TeamBackend 派生，显式保留以便测试注入）
	TaskManager *tools.TeamTaskManager
	// MessageManager 消息管理器（概念上从 TeamBackend 派生，显式保留以便测试注入）
	MessageManager *tools.TeamMessageManager
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
