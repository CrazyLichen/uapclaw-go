package agent

import (
	harness "github.com/uapclaw/uapclaw-go/internal/agent_teams/harness"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/memory"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tracker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/worktree"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PrivateAgentResources 单个 TeamAgent 的实例级运行时资源。
// 四象限分解的第四象限：每实例独占的资源。
// 与 TeamInfra（进程级共享）不同，每个 TeamAgent 有自己的 harness/worktree/memory 等。
// Python: PrivateAgentResources (openjiuwen/agent_teams/agent/resources.py)
type PrivateAgentResources struct {
	// Harness 底层 DeepAgent 运行时的 Harness
	Harness *harness.TeamHarness
	// WorktreeManager Worktree 管理器。⤴️ 9.66a 回填完成
	WorktreeManager *worktree.WorktreeManager
	// MemoryManager 团队记忆管理器。⤴️ 9.64 回填完成
	MemoryManager *memory.TeamMemoryManager
	// ModelAllocator 模型分配器（仅 Leader）。⤴️ 9.64 回填完成
	ModelAllocator models.ModelAllocator
	// TaskEventTracker 任务通知器（非 leader 才有，⤴️ 25.0c）
	// 由 TeamAgent 持有、TeamPolicyRail 借用——基线因此超出 rail 生命周期。
	TaskEventTracker tracker.TaskEventTracker
}
