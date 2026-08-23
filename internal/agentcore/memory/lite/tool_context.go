package lite

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryToolContext 通用记忆工具上下文。对齐 Python MemoryToolContext
type MemoryToolContext struct {
	LiteMemoryToolContextBase
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────
// ──────────────────────────── 导出函数 ────────────────────────────

// NewMemoryToolContext 创建通用记忆工具上下文
func NewMemoryToolContext() *MemoryToolContext {
	return &MemoryToolContext{
		LiteMemoryToolContextBase: LiteMemoryToolContextBase{
			NodeName: "memory",
		},
	}
}

// WithWorkspace 设置工作空间
func (c *MemoryToolContext) WithWorkspace(ws *workspace.Workspace) *MemoryToolContext {
	c.Workspace = ws
	return c
}

// WithSettings 设置记忆配置
func (c *MemoryToolContext) WithSettings(s *MemorySettings) *MemoryToolContext {
	c.Settings = s
	return c
}

// WithAgentID 设置 Agent 标识
func (c *MemoryToolContext) WithAgentID(id string) *MemoryToolContext {
	c.AgentID = id
	return c
}

// WithEmbeddingConfig 设置嵌入配置
func (c *MemoryToolContext) WithEmbeddingConfig(cfg *embedding.EmbeddingConfig) *MemoryToolContext {
	c.EmbeddingConfig = cfg
	return c
}

// WithSysOperation 设置系统操作接口
func (c *MemoryToolContext) WithSysOperation(op sysop.SysOperation) *MemoryToolContext {
	c.SysOperation = op
	return c
}

// WithManager 设置记忆索引管理器
func (c *MemoryToolContext) WithManager(mgr MemoryIndexManager) *MemoryToolContext {
	c.Manager = mgr
	return c
}

// ──────────────────────────── 非导出函数 ────────────────────────────
