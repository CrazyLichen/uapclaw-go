package agent

import (
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/team_workspace"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamInfra 每进程团队基础设施。
// 四象限分解的第三象限：每进程可达的基础设施资源。
// Leader 和 Teammate 在不同进程运行，"共享"是进程级范围而非跨实例单例。
// Python: TeamInfra (openjiuwen/agent_teams/agent/infra.py)
//
// S-32: 字段私有化 + getter 暴露，使 *TeamInfra 直接满足
// coordination/types.DispatcherInfra 接口（具体类型真实签名），
// 消除 Setup(role, nil, nil) 的 nil 适配层。
type TeamInfra struct {
	// messager 消息总线
	messager messager.Messager
	// teamBackend 团队后端（DB + task/message managers）
	teamBackend *tools.TeamBackend
	// workspaceManager 团队工作空间管理器
	// ⤴️ 9.66 回填完成：从 any 替换为具体类型
	workspaceManager *team_workspace.TeamWorkspaceManager
	// workspaceInitialized 工作空间是否已初始化
	workspaceInitialized bool
	// taskManager 任务管理器（概念上从 TeamBackend 派生，显式保留以便测试注入）
	taskManager *tools.TeamTaskManager
	// messageManager 消息管理器（概念上从 TeamBackend 派生，显式保留以便测试注入）
	messageManager *tools.TeamMessageManager
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// Messager 返回消息总线。
func (t *TeamInfra) Messager() messager.Messager { return t.messager }

// SetMessager 设置消息总线。
func (t *TeamInfra) SetMessager(v messager.Messager) { t.messager = v }

// TeamBackend 返回团队后端。
func (t *TeamInfra) TeamBackend() *tools.TeamBackend { return t.teamBackend }

// SetTeamBackend 设置团队后端。
func (t *TeamInfra) SetTeamBackend(v *tools.TeamBackend) { t.teamBackend = v }

// WorkspaceManager 返回工作空间管理器。
func (t *TeamInfra) WorkspaceManager() *team_workspace.TeamWorkspaceManager {
	return t.workspaceManager
}

// SetWorkspaceManager 设置工作空间管理器。
func (t *TeamInfra) SetWorkspaceManager(v *team_workspace.TeamWorkspaceManager) {
	t.workspaceManager = v
}

// WorkspaceInitialized 工作空间是否已初始化。
func (t *TeamInfra) WorkspaceInitialized() bool { return t.workspaceInitialized }

// SetWorkspaceInitialized 标记工作空间是否已初始化。
func (t *TeamInfra) SetWorkspaceInitialized(v bool) { t.workspaceInitialized = v }

// TaskManager 返回任务管理器。
func (t *TeamInfra) TaskManager() *tools.TeamTaskManager { return t.taskManager }

// SetTaskManager 设置任务管理器。
func (t *TeamInfra) SetTaskManager(v *tools.TeamTaskManager) { t.taskManager = v }

// MessageManager 返回消息管理器。
func (t *TeamInfra) MessageManager() *tools.TeamMessageManager { return t.messageManager }

// SetMessageManager 设置消息管理器。
func (t *TeamInfra) SetMessageManager(v *tools.TeamMessageManager) { t.messageManager = v }

// ──────────────────────────── 非导出函数 ────────────────────────────
