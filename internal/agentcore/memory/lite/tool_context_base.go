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

// WithWorkspace 设置工作空间
func (b *LiteMemoryToolContextBase) WithWorkspace(ws *workspace.Workspace) *LiteMemoryToolContextBase {
	b.Workspace = ws
	return b
}

// WithSettings 设置记忆配置
func (b *LiteMemoryToolContextBase) WithSettings(s *MemorySettings) *LiteMemoryToolContextBase {
	b.Settings = s
	return b
}

// WithAgentID 设置 Agent 标识
func (b *LiteMemoryToolContextBase) WithAgentID(id string) *LiteMemoryToolContextBase {
	b.AgentID = id
	return b
}

// WithEmbeddingConfig 设置嵌入配置
func (b *LiteMemoryToolContextBase) WithEmbeddingConfig(cfg *embedding.EmbeddingConfig) *LiteMemoryToolContextBase {
	b.EmbeddingConfig = cfg
	return b
}

// WithSysOperation 设置系统操作接口
func (b *LiteMemoryToolContextBase) WithSysOperation(op sysop.SysOperation) *LiteMemoryToolContextBase {
	b.SysOperation = op
	return b
}

// WithManager 设置记忆索引管理器
func (b *LiteMemoryToolContextBase) WithManager(mgr MemoryIndexManager) *LiteMemoryToolContextBase {
	b.Manager = mgr
	return b
}

// WithNodeName 设置节点名称
func (b *LiteMemoryToolContextBase) WithNodeName(name string) *LiteMemoryToolContextBase {
	b.NodeName = name
	return b
}

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
