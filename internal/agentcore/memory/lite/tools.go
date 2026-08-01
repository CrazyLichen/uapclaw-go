package lite

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// InitMemoryManagerAsync 初始化通用记忆管理器。⤵️ 回填: 7.2
func InitMemoryManagerAsync(ctx context.Context, ws *workspace.Workspace, agentID string, embeddingConfig *embedding.EmbeddingConfig, sysOp sysop.SysOperation) (MemoryIndexManager, error) {
	return nil, nil
}
