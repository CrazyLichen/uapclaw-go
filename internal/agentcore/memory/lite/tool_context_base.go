package lite

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LiteMemoryToolContextBase 记忆工具上下文基类。⤵️ 回填: 7.3
type LiteMemoryToolContextBase struct {
	// Workspace 工作空间
	Workspace *workspace.Workspace
	// Settings 记忆配置
	Settings *MemorySettings
	// AgentID Agent 标识
	AgentID string
	// EmbeddingConfig 嵌入配置
	EmbeddingConfig *embedding.EmbeddingConfig
	// SysOperation 系统操作接口
	SysOperation sysop.SysOperation
	// Manager 记忆索引管理器
	Manager MemoryIndexManager
	// NodeName 节点名称
	NodeName string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// EnsureManager 懒加载 manager。⤵️ 回填: 7.3 — 当前返回 false
func (b *LiteMemoryToolContextBase) EnsureManager() bool { return false }
