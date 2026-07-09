package filesystem

import (
	"context"
	"sort"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ListDirInput 列出目录工具的输入参数。
// 对齐 Python: ListDirTool inputs (filesystem.py L1500)
type ListDirInput struct {
	// Path 目录路径，默认 "."
	Path string `json:"path"`
	// ShowHidden 显示隐藏文件
	ShowHidden bool `json:"show_hidden"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewListDirTool 创建 ListDirTool 实例。
// 对齐 Python: ListDirTool (filesystem.py L1493)
func NewListDirTool(op sys_operation.SysOperation, language, agentID string) tool.Tool {
	card, _ := tools.BuildToolCard("list_files", "ListDirTool", language, nil, agentID)

	fn := func(ctx context.Context, input ListDirInput, opts ...tool.ToolOption) (map[string]any, error) {
		// path 默认 "."
		// 对齐 Python L1501
		path := input.Path
		if path == "" {
			path = "."
		}

		// 对齐 Python L1504-1505
		filesRes, filesErr := op.Fs().ListFiles(ctx, path)
		dirsRes, dirsErr := op.Fs().ListDirectories(ctx, path)

		if filesErr != nil {
			logger.Error(logComponent).
				Str("path", path).
				Err(filesErr).
				Msg("ListDirTool ListFiles 调用失败")
			return map[string]any{
				"success": false,
				"error":   "列出文件失败: " + filesErr.Error(),
			}, nil
		}
		if dirsErr != nil {
			logger.Error(logComponent).
				Str("path", path).
				Err(dirsErr).
				Msg("ListDirTool ListDirectories 调用失败")
			return map[string]any{
				"success": false,
				"error":   "列出目录失败: " + dirsErr.Error(),
			}, nil
		}

		// 检查返回结果 Code
		// 对齐 Python L1507-1510
		if !filesRes.IsSuccess() {
			logger.Error(logComponent).
				Str("path", path).
				Str("message", filesRes.Message).
				Msg("ListDirTool ListFiles 返回失败")
			return map[string]any{
				"success": false,
				"error":   "列出文件失败: " + filesRes.Message,
			}, nil
		}
		if !dirsRes.IsSuccess() {
			logger.Error(logComponent).
				Str("path", path).
				Str("message", dirsRes.Message).
				Msg("ListDirTool ListDirectories 返回失败")
			return map[string]any{
				"success": false,
				"error":   "列出目录失败: " + dirsRes.Message,
			}, nil
		}

		// 提取文件名和目录名
		// 对齐 Python L1512-1513
		var files []string
		if filesRes.Data != nil {
			for _, item := range filesRes.Data.ListItems {
				files = append(files, item.Name)
			}
		}
		var dirs []string
		if dirsRes.Data != nil {
			for _, item := range dirsRes.Data.ListItems {
				dirs = append(dirs, item.Name)
			}
		}

		// show_hidden=false 时过滤 . 开头
		// 对齐 Python L1515-1517
		if !input.ShowHidden {
			filteredFiles := make([]string, 0, len(files))
			for _, f := range files {
				if !isHidden(f) {
					filteredFiles = append(filteredFiles, f)
				}
			}
			files = filteredFiles

			filteredDirs := make([]string, 0, len(dirs))
			for _, d := range dirs {
				if !isHidden(d) {
					filteredDirs = append(filteredDirs, d)
				}
			}
			dirs = filteredDirs
		}

		// 排序
		// 对齐 Python L1519-1520
		sort.Strings(files)
		sort.Strings(dirs)

		logger.Info(logComponent).
			Str("path", path).
			Int("file_count", len(files)).
			Int("dir_count", len(dirs)).
			Msg("ListDirTool 列出目录成功")

		// 对齐 Python L1522-1528
		return map[string]any{
			"success": true,
			"data": map[string]any{
				"files": files,
				"dirs":  dirs,
			},
		}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isHidden 判断文件名是否为隐藏文件（以 . 开头）。
func isHidden(name string) bool {
	return len(name) > 0 && name[0] == '.'
}
