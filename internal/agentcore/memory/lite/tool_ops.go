package lite

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// dailyMemoryPattern 日期文件名正则。对齐 Python: re.match(r"^\d{4}-\d{2}-\d{2}\.md$", basename)
var dailyMemoryPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.md$`)

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateMemoryPath 验证路径在 memory 目录内。对齐 Python validate_memory_path
func ValidateMemoryPath(path string, ws *workspace.Workspace) (bool, string) {
	if ws == nil {
		return false, "Workspace not initialized"
	}
	if strings.Contains(path, "..") || strings.HasPrefix(path, "/") {
		return false, "Invalid path: directory traversal not allowed"
	}
	basename := filepath.Base(path)
	memoryDir := ""
	if nodePath := ws.GetNodePath("memory"); nodePath != nil {
		memoryDir = *nodePath
	}
	var resolvedPath string
	switch {
	case basename == "USER.md":
		if userPath := ws.GetNodePath("USER.md"); userPath != nil {
			resolvedPath = *userPath
		}
	case basename == "MEMORY.md":
		memoryRel := ws.GetDirectory("MEMORY.md")
		if memoryDir != "" && memoryRel != "" {
			resolvedPath = filepath.Join(memoryDir, memoryRel)
		}
	case dailyMemoryPattern.MatchString(basename):
		dailyRel := ws.GetDirectory("daily_memory")
		if memoryDir != "" && dailyRel != "" {
			resolvedPath = filepath.Join(memoryDir, dailyRel, basename)
		}
	default:
		if memoryDir != "" {
			resolvedPath = filepath.Join(memoryDir, basename)
		}
	}
	if resolvedPath == "" {
		return false, fmt.Sprintf("Cannot resolve path: %s", path)
	}
	return true, resolvedPath
}

// MemorySearchWithContext 语义搜索记忆。对齐 Python memory_search_with_context
func MemorySearchWithContext(ctx context.Context, toolCtx *MemoryToolContext, query string, maxResults *int, minScore *float64, sessionKey string) map[string]any {
	if toolCtx == nil {
		return map[string]any{"results": []map[string]any{}, "disabled": true, "error": "Memory manager not available"}
	}
	if !toolCtx.EnsureManager() {
		return map[string]any{"results": []map[string]any{}, "disabled": true, "error": "Memory manager not available"}
	}
	if toolCtx.Manager == nil {
		return map[string]any{"results": []map[string]any{}, "disabled": true, "error": "Memory manager not initialized"}
	}
	opts := make(map[string]any)
	if maxResults != nil {
		opts["max_results"] = *maxResults
	}
	if minScore != nil {
		opts["min_score"] = *minScore
	}
	if sessionKey != "" {
		opts["session_key"] = sessionKey
	}
	results, err := toolCtx.Manager.Search(ctx, query, opts)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Msg("记忆搜索失败")
		return map[string]any{"results": []map[string]any{}, "disabled": true, "error": err.Error()}
	}
	// 添加 citation 字段。对齐 Python: r["citation"] = f"{r['path']}#L{r['start_line']}"
	for _, r := range results {
		startLine, _ := r["start_line"].(int)
		endLine, _ := r["end_line"].(int)
		path, _ := r["path"].(string)
		if path != "" {
			if startLine == endLine {
				r["citation"] = fmt.Sprintf("%s#L%d", path, startLine)
			} else {
				r["citation"] = fmt.Sprintf("%s#L%d-L%d", path, startLine, endLine)
			}
		}
	}
	status := toolCtx.Manager.Status()
	return map[string]any{
		"results":  results,
		"provider": status["provider"],
		"model":    status["model"],
		"disabled": false,
	}
}

// MemoryGetWithContext 获取记忆文件内容。对齐 Python memory_get_with_context
func MemoryGetWithContext(ctx context.Context, toolCtx *MemoryToolContext, path string, fromLine *int, lines *int) map[string]any {
	ws := toolCtx.Workspace
	if ws == nil {
		return map[string]any{"path": path, "text": "", "disabled": true, "error": "Workspace not initialized"}
	}
	isValid, result := ValidateMemoryPath(path, ws)
	if !isValid {
		return map[string]any{"path": path, "text": "", "disabled": true, "error": result}
	}
	resolvedPath := result
	if !toolCtx.EnsureManager() {
		return map[string]any{"path": resolvedPath, "text": "", "disabled": true, "error": "Memory manager not available"}
	}
	if toolCtx.Manager == nil {
		return map[string]any{"path": resolvedPath, "text": "", "disabled": true, "error": "Memory manager not initialized"}
	}
	rf, err := toolCtx.Manager.ReadFile(ctx, resolvedPath, fromLine, lines)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Msg("记忆获取失败")
		return map[string]any{"path": resolvedPath, "text": "", "disabled": true, "error": err.Error()}
	}
	rf["disabled"] = false
	return rf
}

// ReadMemoryWithContext 读取记忆文件。对齐 Python read_memory_with_context
func ReadMemoryWithContext(ctx context.Context, toolCtx *MemoryToolContext, path string, offset *int, limit *int) map[string]any {
	ws := toolCtx.Workspace
	if ws == nil {
		return map[string]any{"success": false, "path": path, "content": "", "error": "Workspace not initialized"}
	}
	isValid, result := ValidateMemoryPath(path, ws)
	if !isValid {
		return map[string]any{"success": false, "path": path, "content": "", "error": result}
	}
	fullPath := result
	sysOp := toolCtx.SysOperation
	if sysOp == nil {
		logger.Error(logger.ComponentAgentCore).Msg("读取记忆失败，无可用 sys_operation")
		return map[string]any{"success": false, "path": path, "error": "Read failed, no available sys_operation."}
	}
	// 对齐 Python: 使用 line_range 读取
	fsOpts := []sysop.FsOption{}
	if offset != nil {
		if limit != nil {
			fsOpts = append(fsOpts, sysop.WithFsLineRange(*offset, *offset+*limit-1))
		} else {
			fsOpts = append(fsOpts, sysop.WithFsLineRange(*offset, -1))
		}
	}
	readResult, err := sysOp.Fs().ReadFile(ctx, fullPath, fsOpts...)
	if err != nil {
		return map[string]any{"success": false, "path": path, "content": "", "error": err.Error()}
	}
	var content string
	if readResult != nil && readResult.Data != nil {
		content = readResult.Data.Content
	}
	lineList := strings.Split(content, "\n")
	body, nTotal, startIdx, endIdx, truncated := viewLines(lineList, offset, limit)
	return map[string]any{
		"success":    true,
		"path":       fullPath,
		"content":    body,
		"totalLines": nTotal,
		"start_line": startIdx + 1,
		"end_line":   endIdx,
		"truncated":  truncated,
	}
}

// WriteMemoryWithContext 写入/追加记忆文件。对齐 Python write_memory_with_context
func WriteMemoryWithContext(ctx context.Context, toolCtx *MemoryToolContext, path string, content string, appendMode bool) map[string]any {
	ws := toolCtx.Workspace
	if ws == nil {
		return map[string]any{"success": false, "path": path, "error": "Workspace not initialized"}
	}
	isValid, result := ValidateMemoryPath(path, ws)
	if !isValid {
		return map[string]any{"success": false, "path": path, "error": result}
	}
	resolvedPath := result
	sysOp := toolCtx.SysOperation
	if sysOp == nil {
		logger.Error(logger.ComponentAgentCore).Msg("记忆写入失败，无可用 sys_operation")
		return map[string]any{"success": false, "path": path, "error": "Memory write failed, no available sys_operation."}
	}
	fsOpts := []sysop.FsOption{
		sysop.WithFsCreateIfNotExist(true),
		sysop.WithFsAppend(appendMode),
	}
	if appendMode {
		prepend := true
		fsOpts = append(fsOpts, sysop.WithFsPrependNewline(prepend))
	}
	writeResult, err := sysOp.Fs().WriteFile(ctx, resolvedPath, content, fsOpts...)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Str("path", resolvedPath).Msg("写入失败")
		return map[string]any{"success": false, "path": path, "error": err.Error()}
	}
	fileExisted := writeResult != nil && writeResult.Data != nil && writeResult.Data.Size > 0
	action := "Wrote"
	if appendMode {
		action = "Appended to"
	}
	logger.Info(logger.ComponentAgentCore).Str("action", action).Str("path", resolvedPath).Msg("记忆文件已写入")
	return map[string]any{
		"success":     true,
		"path":        resolvedPath,
		"fullPath":    resolvedPath,
		"appended":    appendMode,
		"fileExisted": fileExisted,
	}
}

// EditMemoryWithContext 编辑记忆文件。对齐 Python edit_memory_with_context
func EditMemoryWithContext(ctx context.Context, toolCtx *MemoryToolContext, path string, oldText string, newText string) map[string]any {
	ws := toolCtx.Workspace
	if ws == nil {
		return map[string]any{"success": false, "path": path, "error": "Workspace not initialized"}
	}
	isValid, result := ValidateMemoryPath(path, ws)
	if !isValid {
		return map[string]any{"success": false, "path": path, "error": result}
	}
	resolvedPath := result
	sysOp := toolCtx.SysOperation
	if sysOp == nil {
		logger.Error(logger.ComponentAgentCore).Msg("编辑失败，无可用 sys_operation")
		return map[string]any{"success": false, "path": path, "error": "Edit failed, no available sys_operation."}
	}
	readResult, err := sysOp.Fs().ReadFile(ctx, resolvedPath)
	if err != nil || readResult == nil || readResult.Data == nil {
		return map[string]any{"success": false, "path": path, "error": fmt.Sprintf("failed to read file: %s", path)}
	}
	content := readResult.Data.Content
	if !strings.Contains(content, oldText) {
		return map[string]any{
			"success": false,
			"path":    path,
			"error":   "old_text not found in file. Use read_memory tool to check exact content.",
		}
	}
	occurrences := strings.Count(content, oldText)
	if occurrences > 1 {
		return map[string]any{
			"success": false,
			"path":    path,
			"error":   fmt.Sprintf("old_text appears %d times in file. Be more specific.", occurrences),
		}
	}
	newContent := strings.Replace(content, oldText, newText, 1)
	_, err = sysOp.Fs().WriteFile(ctx, resolvedPath, newContent,
		sysop.WithFsCreateIfNotExist(true),
		sysop.WithFsPrependNewline(false),
		sysop.WithFsAppendNewline(false),
	)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Str("path", resolvedPath).Msg("编辑写入失败")
		return map[string]any{"success": false, "path": path, "error": err.Error()}
	}
	logger.Info(logger.ComponentAgentCore).Str("path", resolvedPath).Msg("文件已编辑")
	return map[string]any{
		"success":  true,
		"path":     resolvedPath,
		"replaced": oldText,
		"new_text": newText,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// lineRangeToFsRead 映射 offset/limit 到 FsOperation 的 LineRange。对齐 Python _line_range_to_fs_read
func lineRangeToFsRead(firstLine *int, lineCap *int) *[2]int {
	if firstLine == nil {
		return nil
	}
	if lineCap != nil {
		return &[2]int{*firstLine, *firstLine + *lineCap - 1}
	}
	return &[2]int{*firstLine, -1}
}

// viewLines 行视图切片。对齐 Python _view_lines
// firstLine 是 1-based；返回 (excerpt, total, start_idx, end_idx, truncated)
func viewLines(allLines []string, firstLine *int, lineCap *int) (string, int, int, int, bool) {
	total := len(allLines)
	startIdx := 0
	if firstLine != nil {
		startIdx = *firstLine - 1
		if startIdx < 0 {
			startIdx = 0
		}
	}
	endIdx := total
	if lineCap != nil {
		endIdx = startIdx + *lineCap
		if endIdx > total {
			endIdx = total
		}
	}
	if startIdx > total {
		startIdx = total
	}
	if endIdx > total {
		endIdx = total
	}
	text := strings.Join(allLines[startIdx:endIdx], "\n")
	truncated := lineCap != nil && endIdx < total
	return text, total, startIdx, endIdx, truncated
}
