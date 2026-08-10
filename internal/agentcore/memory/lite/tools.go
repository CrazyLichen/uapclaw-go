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
		logger.Info(logger.ComponentAgentCore).Msg("Memory system is disabled")
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
		logger.Error(logger.ComponentAgentCore).Err(err).Msg("Failed to initialize memory manager")
		return nil, err
	}
	logger.Info(logger.ComponentAgentCore).Str("memory_dir", memoryDir).Msg("Memory manager initialized")
	return mgr, nil
}

// InitCodingMemoryManagerAsync 初始化编程记忆管理器。对齐 Python coding_memory_tools.init_memory_manager_async
func InitCodingMemoryManagerAsync(ctx context.Context, ws *workspace.Workspace, agentID string, embeddingConfig *embedding.EmbeddingConfig, sysOp sysop.SysOperation) (MemoryIndexManager, error) {
	if !IsMemoryEnabled() {
		logger.Info(logger.ComponentAgentCore).Msg("Memory system is disabled")
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
		logger.Error(logger.ComponentAgentCore).Err(err).Msg("Failed to initialize Coding Memory manager")
		return nil, fmt.Errorf("初始化 Coding Memory 管理器失败: %w", err)
	}
	logger.Info(logger.ComponentAgentCore).Str("cm_dir", cmDir).Msg("Coding Memory manager initialized")
	return mgr, nil
}
