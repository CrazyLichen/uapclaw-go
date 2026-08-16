package lite

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// InitMemoryManagerAsync 初始化通用记忆管理器。对齐 Python memory_tools.init_memory_manager_async
func InitMemoryManagerAsync(ctx context.Context, ws *workspace.Workspace, agentID string, embeddingConfig *embedding.EmbeddingConfig, sysOp sysop.SysOperation) (MemoryIndexManager, error) {
	if !IsMemoryEnabled() {
		logger.Info(logger.ComponentAgentCore).Msg("记忆系统已禁用")
		return nil, nil
	}
	memoryDir := ""
	if nodePath := ws.GetNodePath("memory"); nodePath != nil {
		memoryDir = *nodePath
	}
	settings := CreateMemorySettings(memoryDir, nil)
	params := MemoryManagerParams{
		AgentID:         agentID,
		Workspace:       ws,
		Settings:        settings,
		EmbeddingConfig: embeddingConfig,
		SysOperation:    sysOp,
		NodeName:        "memory",
	}
	mgr, err := GetMemoryIndexManager(params)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Msg("初始化记忆管理器失败")
		return nil, err
	}
	logger.Info(logger.ComponentAgentCore).Str("memory_dir", memoryDir).Msg("记忆管理器已初始化")
	return mgr, nil
}

// InitCodingMemoryManagerAsync 初始化编程记忆管理器。对齐 Python coding_memory_tools.init_memory_manager_async
// llm 参数用于 7.8 MemUpdateChecker 的 LLM 冲突判断，当前预留。
func InitCodingMemoryManagerAsync(ctx context.Context, ws *workspace.Workspace, agentID string, embeddingConfig *embedding.EmbeddingConfig, sysOp sysop.SysOperation, llm any) (MemoryIndexManager, error) {
	if !IsMemoryEnabled() {
		logger.Info(logger.ComponentAgentCore).Msg("记忆系统已禁用")
		return nil, nil
	}
	cmDir := ""
	if nodePath := ws.GetNodePath("coding_memory"); nodePath != nil {
		cmDir = *nodePath
	}
	settings := CreateMemorySettings(cmDir, nil)
	params := MemoryManagerParams{
		AgentID:         agentID,
		Workspace:       ws,
		Settings:        settings,
		EmbeddingConfig: embeddingConfig,
		SysOperation:    sysOp,
		NodeName:        "coding_memory",
	}
	mgr, err := GetMemoryIndexManager(params)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Msg("初始化编程记忆管理器失败")
		return nil, fmt.Errorf("初始化 Coding Memory 管理器失败: %w", err)
	}
	// 对齐 Python: manager.llm = llm — 设置 LLM 实例，用于 7.8 MemUpdateChecker
	if impl, ok := mgr.(*memoryIndexManager); ok && llm != nil {
		impl.llm = llm
	}
	logger.Info(logger.ComponentAgentCore).Str("cm_dir", cmDir).Msg("编程记忆管理器已初始化")
	return mgr, nil
}
