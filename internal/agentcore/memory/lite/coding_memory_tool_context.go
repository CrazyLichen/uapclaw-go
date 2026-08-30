package lite

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodingMemoryToolContext 编程记忆工具上下文。对齐 Python CodingMemoryToolContext
type CodingMemoryToolContext struct {
	LiteMemoryToolContextBase
	// CodingMemoryDir 编程记忆目录路径
	CodingMemoryDir string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCodingMemoryToolContext 创建编程记忆工具上下文
func NewCodingMemoryToolContext() *CodingMemoryToolContext {
	return &CodingMemoryToolContext{
		LiteMemoryToolContextBase: LiteMemoryToolContextBase{
			NodeName: "coding_memory",
		},
	}
}

// WithWorkspace 设置工作空间
func (c *CodingMemoryToolContext) WithWorkspace(ws *workspace.Workspace) *CodingMemoryToolContext {
	c.Workspace = ws
	return c
}

// WithSettings 设置记忆配置
func (c *CodingMemoryToolContext) WithSettings(s *MemorySettings) *CodingMemoryToolContext {
	c.Settings = s
	return c
}

// WithAgentID 设置 Agent 标识
func (c *CodingMemoryToolContext) WithAgentID(id string) *CodingMemoryToolContext {
	c.AgentID = id
	return c
}

// WithEmbeddingConfig 设置嵌入配置
func (c *CodingMemoryToolContext) WithEmbeddingConfig(cfg *embedding.EmbeddingConfig) *CodingMemoryToolContext {
	c.EmbeddingConfig = cfg
	return c
}

// WithSysOperation 设置系统操作接口
func (c *CodingMemoryToolContext) WithSysOperation(op sysop.SysOperation) *CodingMemoryToolContext {
	c.SysOperation = op
	return c
}

// WithManager 设置记忆索引管理器
func (c *CodingMemoryToolContext) WithManager(mgr MemoryIndexManager) *CodingMemoryToolContext {
	c.Manager = mgr
	return c
}

// WithCodingMemoryDir 设置编程记忆目录
func (c *CodingMemoryToolContext) WithCodingMemoryDir(dir string) *CodingMemoryToolContext {
	c.CodingMemoryDir = dir
	return c
}

// ──────────────────────────── 非导出函数 ────────────────────────────
