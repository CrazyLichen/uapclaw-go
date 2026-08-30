package lite

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryIndexManager 记忆索引管理器接口。对齐 Python MemoryIndexManager
type MemoryIndexManager interface {
	// Initialize 初始化管理器（打开数据库、建 schema、初始化 provider）
	Initialize(ctx context.Context) error
	// Sync 同步索引（增量或全量 reindex）
	Sync(ctx context.Context, reason string, force bool) error
	// Search 混合搜索（向量 + FTS5 关键词）
	Search(ctx context.Context, query string, opts map[string]any) ([]SearchResult, error)
	// ReadFile 读取记忆文件内容
	ReadFile(ctx context.Context, relPath string, fromLine *int, lines *int) (*ReadFileResult, error)
	// Status 返回系统状态报告
	Status() *StatusResult
	// HasLLM 判断是否有 LLM 实例可用。对齐 Python ctx.manager.llm
	HasLLM() bool
	// Close 关闭管理器
	Close() error
}

// MemoryManagerParams 记忆管理器构造参数
type MemoryManagerParams struct {
	// AgentID Agent 标识
	AgentID string
	// Workspace 工作空间
	Workspace *workspace.Workspace
	// Settings 记忆配置
	Settings *MemorySettings
	// EmbeddingConfig 嵌入配置
	EmbeddingConfig *embedding.EmbeddingConfig
	// SysOperation 系统操作接口
	SysOperation sysop.SysOperation
	// NodeName 节点名称（"memory" 或 "coding_memory"）
	NodeName string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// GetMemoryIndexManager 幂等获取管理器实例。对齐 Python MemoryIndexManager.get
func GetMemoryIndexManager(params MemoryManagerParams) (MemoryIndexManager, error) {
	return getMemoryIndexManager(params)
}

// ClearMemoryManagerCache 清除缓存。对齐 Python clear_memory_manager_cache
func ClearMemoryManagerCache() {
	clearMemoryManagerCache()
}

// ──────────────────────────── 非导出函数 ────────────────────────────
