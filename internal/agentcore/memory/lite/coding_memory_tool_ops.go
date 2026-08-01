package lite

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateCodingMemoryPath 验证路径在 coding_memory 目录内。⤵️ 回填: 7.2
func ValidateCodingMemoryPath(path string, ws *workspace.Workspace) (bool, string) { return false, "" }

// CodingMemoryReadWithContext 读取 coding_memory 文件。⤵️ 回填: 7.2
func CodingMemoryReadWithContext(ctx context.Context, toolCtx *CodingMemoryToolContext, path string, offset *int, limit *int) map[string]any { return nil }

// CodingMemoryWriteWithContext 写入 coding_memory 文件。⤵️ 回填: 7.2
func CodingMemoryWriteWithContext(ctx context.Context, toolCtx *CodingMemoryToolContext, path string, content string) map[string]any { return nil }

// CodingMemoryEditWithContext 编辑 coding_memory 文件。⤵️ 回填: 7.2
func CodingMemoryEditWithContext(ctx context.Context, toolCtx *CodingMemoryToolContext, path string, oldText string, newText string) map[string]any { return nil }
