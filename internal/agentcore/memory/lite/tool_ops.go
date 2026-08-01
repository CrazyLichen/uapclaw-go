package lite

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateMemoryPath 验证路径在 memory 目录内。⤵️ 回填: 7.2
func ValidateMemoryPath(path string, ws *workspace.Workspace) (bool, string) { return false, "" }

// MemorySearchWithContext 语义搜索记忆。⤵️ 回填: 7.2
func MemorySearchWithContext(ctx context.Context, toolCtx *MemoryToolContext, query string, maxResults *int, minScore *float64, sessionKey string) map[string]any {
	return nil
}

// MemoryGetWithContext 获取记忆文件内容。⤵️ 回填: 7.2
func MemoryGetWithContext(ctx context.Context, toolCtx *MemoryToolContext, path string, fromLine *int, lines *int) map[string]any {
	return nil
}

// ReadMemoryWithContext 读取记忆文件。⤵️ 回填: 7.2
func ReadMemoryWithContext(ctx context.Context, toolCtx *MemoryToolContext, path string, offset *int, limit *int) map[string]any {
	return nil
}

// WriteMemoryWithContext 写入/追加记忆文件。⤵️ 回填: 7.2
func WriteMemoryWithContext(ctx context.Context, toolCtx *MemoryToolContext, path string, content string, appendMode bool) map[string]any {
	return nil
}

// EditMemoryWithContext 编辑记忆文件。⤵️ 回填: 7.2
func EditMemoryWithContext(ctx context.Context, toolCtx *MemoryToolContext, path string, oldText string, newText string) map[string]any {
	return nil
}
