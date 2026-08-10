package lite

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LiteMemoryToolContextBase 记忆工具上下文基类。对齐 Python LiteMemoryToolContextBase
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

// EnsureManager 懒加载 manager。对齐 Python LiteMemoryToolContextBase.ensure_manager
func (b *LiteMemoryToolContextBase) EnsureManager() bool {
	if b.Manager != nil {
		// 检查 manager 是否已关闭
		if closed, ok := b.Manager.(interface{ IsClosed() bool }); ok && closed.IsClosed() {
			// 已关闭，需要重新初始化
		} else {
			return true
		}
	}
	if b.Workspace == nil {
		return false
	}
	if b.Settings == nil {
		b.Settings = CreateMemorySettings("", nil)
	}
	params := MemoryManagerParams{
		AgentID:         b.AgentID,
		Workspace:       b.Workspace,
		Settings:        b.Settings,
		EmbeddingConfig: b.EmbeddingConfig,
		SysOperation:    b.SysOperation,
		NodeName:        b.NodeName,
	}
	mgr, err := GetMemoryIndexManager(params)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("node_name", b.NodeName).
			Err(err).
			Msg("初始化记忆管理器失败")
		return false
	}
	b.Manager = mgr
	return b.Manager != nil
}

// IsClosed 检查 manager 是否已关闭。用于 context 检查
func (b *LiteMemoryToolContextBase) IsClosed() bool {
	if b.Manager == nil {
		return true
	}
	if closed, ok := b.Manager.(interface{ IsClosed() bool }); ok {
		return closed.IsClosed()
	}
	return false
}
