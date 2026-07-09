package filesystem

import (
	"context"
	"path/filepath"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GlobInput glob 模式搜索工具的输入参数。
// 对齐 Python: GlobTool inputs (filesystem.py L1446)
type GlobInput struct {
	// Pattern glob 模式
	Pattern string `json:"pattern"`
	// Path 搜索目录（可选）
	Path string `json:"path"`
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultMaxResults glob 搜索默认最大结果数。
	// 对齐 Python: GlobTool.DEFAULT_MAX_RESULTS (filesystem.py L1401)
	defaultMaxResults = 100

	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGlobTool 创建 GlobTool 实例。
// 对齐 Python: GlobTool (filesystem.py L1400)
func NewGlobTool(op sys_operation.SysOperation, language, agentID string) tool.Tool {
	card, _ := tools.BuildToolCard("glob", "GlobTool", language, nil, agentID)

	fn := func(ctx context.Context, input GlobInput, opts ...tool.ToolOption) (map[string]any, error) {
		// 校验 pattern 非空
		// 对齐 Python L1447-1449
		if input.Pattern == "" {
			return map[string]any{
				"success": false,
				"error":   "pattern is required",
			}, nil
		}

		// 解析搜索路径
		// 对齐 Python L1451-1454
		searchPath, err := resolveSearchPath(ctx, input.Path)
		if err != nil {
			return map[string]any{
				"success": false,
				"error":   err.Error(),
			}, nil
		}

		startedAt := time.Now()

		// 展开花括号模式
		// 对齐 Python L1458
		expandedPatterns := ExpandBracePattern(input.Pattern)

		// 对每个展开后的 pattern 调用 SearchFiles
		// 对齐 Python L1462-1470
		var allMatchingFiles []string
		seen := make(map[string]struct{})

		for _, pat := range expandedPatterns {
			res, searchErr := op.Fs().SearchFiles(ctx, searchPath, pat)
			if searchErr != nil {
				logger.Error(logComponent).
					Str("search_path", searchPath).
					Str("pattern", pat).
					Err(searchErr).
					Msg("GlobTool SearchFiles 调用失败")
				return map[string]any{
					"success": false,
					"error":   searchErr.Error(),
				}, nil
			}
			if !res.IsSuccess() {
				logger.Error(logComponent).
					Str("search_path", searchPath).
					Str("pattern", pat).
					Str("message", res.Message).
					Msg("GlobTool SearchFiles 返回失败")
				return map[string]any{
					"success": false,
					"error":   res.Message,
				}, nil
			}
			if res.Data != nil {
				for _, item := range res.Data.MatchingFiles {
					if _, exists := seen[item.Path]; !exists {
						seen[item.Path] = struct{}{}
						allMatchingFiles = append(allMatchingFiles, item.Path)
					}
				}
			}
		}

		// 截断到 defaultMaxResults
		// 对齐 Python L1472-1474
		truncated := len(allMatchingFiles) > defaultMaxResults
		limitedFiles := allMatchingFiles
		if len(limitedFiles) > defaultMaxResults {
			limitedFiles = limitedFiles[:defaultMaxResults]
		}

		// 转为相对路径
		// 对齐 Python L1474
		filenames := RelativizePaths(searchPath, limitedFiles)

		durationMs := int(time.Since(startedAt).Milliseconds())

		logger.Info(logComponent).
			Str("search_path", searchPath).
			Str("pattern", input.Pattern).
			Int("num_files", len(filenames)).
			Bool("truncated", truncated).
			Int("duration_ms", durationMs).
			Msg("GlobTool 搜索完成")

		// 对齐 Python L1477-1487
		return map[string]any{
			"success": true,
			"data": map[string]any{
				"durationMs":     durationMs,
				"numFiles":       len(filenames),
				"filenames":      filenames,
				"truncated":      truncated,
				"matching_files": limitedFiles,
				"count":          len(filenames),
			},
		}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveSearchPath 解析搜索路径。
// 对齐 Python: GlobTool._resolve_search_path (filesystem.py L1408-1411)
func resolveSearchPath(ctx context.Context, path string) (string, error) {
	if path != "" {
		return ResolveToolFilePath(ctx, path), nil
	}
	// path 为空时默认 cwd
	wd := cwd.GetCwd(ctx)
	abs, err := filepath.Abs(wd)
	if err != nil {
		return wd, nil
	}
	return abs, nil
}
