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
func MemorySearchWithContext(ctx context.Context, toolCtx *MemoryToolContext, query string, maxResults *int, minScore *float64, sessionKey string) *MemorySearchResult {
	if toolCtx == nil {
		return &MemorySearchResult{Disabled: true, Error: "Memory manager not available"}
	}
	if !toolCtx.EnsureManager() {
		return &MemorySearchResult{Disabled: true, Error: "Memory manager not available"}
	}
	if toolCtx.Manager == nil {
		return &MemorySearchResult{Disabled: true, Error: "Memory manager not initialized"}
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
		logger.Error(logger.ComponentAgentCore).Err(err).Msg("Memory search failed")
		return &MemorySearchResult{Disabled: true, Error: err.Error()}
	}
	// 添加 citation 字段。对齐 Python: r["citation"] = f"{r['path']}#L{r['start_line']}"
	for i := range results {
		r := &results[i]
		if r.Path != "" {
			if r.StartLine == r.EndLine {
				r.Citation = fmt.Sprintf("%s#L%d", r.Path, r.StartLine)
			} else {
				r.Citation = fmt.Sprintf("%s#L%d-L%d", r.Path, r.StartLine, r.EndLine)
			}
		}
	}
	status := toolCtx.Manager.Status()
	return &MemorySearchResult{
		Results:  results,
		Disabled: false,
		Provider: status.Provider,
		Model:    status.Model,
	}
}

// MemoryGetWithContext 获取记忆文件内容。对齐 Python memory_get_with_context
func MemoryGetWithContext(ctx context.Context, toolCtx *MemoryToolContext, path string, fromLine *int, lines *int) *MemoryGetResult {
	ws := toolCtx.Workspace
	if ws == nil {
		return &MemoryGetResult{Path: path, Disabled: true, Error: "Workspace not initialized"}
	}
	isValid, result := ValidateMemoryPath(path, ws)
	if !isValid {
		return &MemoryGetResult{Path: path, Disabled: true, Error: result}
	}
	resolvedPath := result
	if !toolCtx.EnsureManager() {
		return &MemoryGetResult{Path: resolvedPath, Disabled: true, Error: "Memory manager not available"}
	}
	if toolCtx.Manager == nil {
		return &MemoryGetResult{Path: resolvedPath, Disabled: true, Error: "Memory manager not initialized"}
	}
	rf, err := toolCtx.Manager.ReadFile(ctx, resolvedPath, fromLine, lines)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Msg("Memory get failed")
		return &MemoryGetResult{Path: resolvedPath, Disabled: true, Error: err.Error()}
	}
	return &MemoryGetResult{
		Path:     rf.Path,
		Text:     rf.Text,
		Disabled: false,
	}
}

// ReadMemoryWithContext 读取记忆文件。对齐 Python read_memory_with_context
func ReadMemoryWithContext(ctx context.Context, toolCtx *MemoryToolContext, path string, offset *int, limit *int) *ReadMemoryResult {
	ws := toolCtx.Workspace
	if ws == nil {
		return &ReadMemoryResult{Success: false, Path: path, Error: "Workspace not initialized"}
	}
	isValid, result := ValidateMemoryPath(path, ws)
	if !isValid {
		return &ReadMemoryResult{Success: false, Path: path, Error: result}
	}
	fullPath := result
	sysOp := toolCtx.SysOperation
	if sysOp == nil {
		logger.Error(logger.ComponentAgentCore).Msg("Read memory failed, no available sys_operation")
		return &ReadMemoryResult{Success: false, Path: path, Error: "Read failed, no available sys_operation."}
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
		return &ReadMemoryResult{Success: false, Path: path, Error: err.Error()}
	}
	var content string
	if readResult != nil && readResult.Data != nil {
		content = readResult.Data.Content
	}
	lineList := strings.Split(content, "\n")
	body, nTotal, startIdx, endIdx, truncated := viewLines(lineList, offset, limit)
	return &ReadMemoryResult{
		Success:    true,
		Path:       fullPath,
		Content:    body,
		TotalLines: nTotal,
		StartLine:  startIdx + 1,
		EndLine:    endIdx,
		Truncated:  truncated,
	}
}

// WriteMemoryWithContext 写入/追加记忆文件。对齐 Python write_memory_with_context
func WriteMemoryWithContext(ctx context.Context, toolCtx *MemoryToolContext, path string, content string, appendMode bool) *WriteMemoryResult {
	ws := toolCtx.Workspace
	if ws == nil {
		return &WriteMemoryResult{Success: false, Path: path, Error: "Workspace not initialized"}
	}
	isValid, result := ValidateMemoryPath(path, ws)
	if !isValid {
		return &WriteMemoryResult{Success: false, Path: path, Error: result}
	}
	resolvedPath := result
	sysOp := toolCtx.SysOperation
	if sysOp == nil {
		logger.Error(logger.ComponentAgentCore).Msg("Memory write failed, no available sys_operation")
		return &WriteMemoryResult{Success: false, Path: path, Error: "Memory write failed, no available sys_operation."}
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
		logger.Error(logger.ComponentAgentCore).Err(err).Str("path", resolvedPath).Msg("Write failed")
		return &WriteMemoryResult{Success: false, Path: path, Error: err.Error()}
	}
	fileExisted := writeResult != nil && writeResult.Data != nil && writeResult.Data.Size > 0
	action := "Wrote"
	if appendMode {
		action = "Appended to"
	}
	logger.Info(logger.ComponentAgentCore).Str("action", action).Str("path", resolvedPath).Msg("Memory file written")
	return &WriteMemoryResult{
		Success:     true,
		Path:        resolvedPath,
		FullPath:    resolvedPath,
		Appended:    appendMode,
		FileExisted: fileExisted,
	}
}

// EditMemoryWithContext 编辑记忆文件。对齐 Python edit_memory_with_context
func EditMemoryWithContext(ctx context.Context, toolCtx *MemoryToolContext, path string, oldText string, newText string) *EditMemoryResult {
	ws := toolCtx.Workspace
	if ws == nil {
		return &EditMemoryResult{Success: false, Path: path, Error: "Workspace not initialized"}
	}
	isValid, result := ValidateMemoryPath(path, ws)
	if !isValid {
		return &EditMemoryResult{Success: false, Path: path, Error: result}
	}
	resolvedPath := result
	sysOp := toolCtx.SysOperation
	if sysOp == nil {
		logger.Error(logger.ComponentAgentCore).Msg("Edit failed, no available sys_operation")
		return &EditMemoryResult{Success: false, Path: path, Error: "Edit failed, no available sys_operation."}
	}
	readResult, err := sysOp.Fs().ReadFile(ctx, resolvedPath)
	if err != nil || readResult == nil || readResult.Data == nil {
		return &EditMemoryResult{Success: false, Path: path, Error: fmt.Sprintf("failed to read file: %s", path)}
	}
	content := readResult.Data.Content
	if !strings.Contains(content, oldText) {
		return &EditMemoryResult{
			Success: false,
			Path:    path,
			Error:   "old_text not found in file. Use read_memory tool to check exact content.",
		}
	}
	occurrences := strings.Count(content, oldText)
	if occurrences > 1 {
		return &EditMemoryResult{
			Success: false,
			Path:    path,
			Error:   fmt.Sprintf("old_text appears %d times in file. Be more specific.", occurrences),
		}
	}
	newContent := strings.Replace(content, oldText, newText, 1)
	_, err = sysOp.Fs().WriteFile(ctx, resolvedPath, newContent,
		sysop.WithFsCreateIfNotExist(true),
		sysop.WithFsPrependNewline(false),
		sysop.WithFsAppendNewline(false),
	)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Str("path", resolvedPath).Msg("Edit write failed")
		return &EditMemoryResult{Success: false, Path: path, Error: err.Error()}
	}
	logger.Info(logger.ComponentAgentCore).Str("path", resolvedPath).Msg("Edited file")
	return &EditMemoryResult{
		Success:  true,
		Path:     resolvedPath,
		Replaced: oldText,
		NewText:  newText,
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
