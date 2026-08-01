package agent

import (
	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/memory"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PrivateAgentResources 单个 TeamAgent 的实例级运行时资源。
// 四象限分解的第四象限：每实例独占的资源。
// 与 TeamInfra（进程级共享）不同，每个 TeamAgent 有自己的 harness/worktree/memory 等。
// 对齐 Python: PrivateAgentResources (openjiuwen/agent_teams/agent/resources.py)
type PrivateAgentResources struct {
	// Harness 底层 DeepAgent 运行时的 Harness
	Harness *agentteams.TeamHarness
	// WorktreeManager Worktree 管理器
	// TODO(#9.66): WorktreeManager 类型
	WorktreeManager any
	// MemoryManager 团队记忆管理器。⤴️ 9.64 回填完成
	MemoryManager *memory.TeamMemoryManager
	// FirstIterGate 首轮迭代门控
	// TODO(#9.68): FirstIterationGate 类型
	FirstIterGate any
	// ModelAllocator 模型分配器（仅 Leader）。⤴️ 9.64 回填完成
	ModelAllocator models.ModelAllocator
}
