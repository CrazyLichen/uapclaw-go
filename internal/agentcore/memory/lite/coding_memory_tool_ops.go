package lite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// maxIndexLines MEMORY.md 索引最大行数。对齐 Python MAX_INDEX_LINES
	maxIndexLines = 200
	// maxConflictRetries 乐观并发最大重试次数。对齐 Python _MAX_CONFLICT_RETRIES
	maxConflictRetries = 2
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// fileLocks 文件级锁注册表。对齐 Python _file_locks: Dict[str, asyncio.Lock]
	// 注意：与 Python asyncio.Lock 不同，Go 的 sync.Mutex 是进程级互斥，保护范围更强
	fileLocks = make(map[string]*sync.Mutex)
	// fileLocksMu 保护 fileLocks 注册表的互斥锁。对齐 Python _file_locks_init_lock
	fileLocksMu sync.Mutex
	// memoryIndexLock MEMORY.md 索引锁。对齐 Python _memory_index_lock
	memoryIndexLock sync.Mutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateCodingMemoryPath 验证路径在 coding_memory 目录内。对齐 Python validate_coding_memory_path
func ValidateCodingMemoryPath(path string, ws *workspace.Workspace) (bool, string) {
	if ws == nil {
		return false, "Workspace not initialized"
	}
	if strings.Contains(path, "..") || strings.HasPrefix(path, "/") {
		return false, "Invalid path: directory traversal not allowed"
	}
	if !strings.HasSuffix(path, ".md") {
		return false, "Path must end with .md"
	}
	memoryDir := ""
	if nodePath := ws.GetNodePath("coding_memory"); nodePath != nil {
		memoryDir = *nodePath
	}
	if memoryDir == "" {
		return false, "coding_memory node not configured"
	}
	resolved := filepath.Join(memoryDir, filepath.Base(path))
	return true, resolved
}

// CodingMemoryReadWithContext 读取 coding_memory 文件。对齐 Python coding_memory_read_with_context
func CodingMemoryReadWithContext(ctx context.Context, toolCtx *CodingMemoryToolContext, path string, offset *int, limit *int) map[string]any {
	ws := toolCtx.Workspace
	if ws == nil {
		return map[string]any{"success": false, "path": path, "content": "", "error": "Workspace not initialized"}
	}
	isValid, result := ValidateCodingMemoryPath(path, ws)
	if !isValid {
		return map[string]any{"success": false, "path": path, "content": "", "error": result}
	}
	fullPath := result
	sysOp := toolCtx.SysOperation
	if sysOp == nil {
		logger.Error(logger.ComponentAgentCore).Msg("Read memory failed, no available sys_operation")
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
	rows := strings.Split(content, "\n")
	n := len(rows)
	fromIdx := 0
	if offset != nil {
		fromIdx = *offset - 1
		if fromIdx < 0 {
			fromIdx = 0
		}
	}
	toIdx := n
	if limit != nil {
		toIdx = fromIdx + *limit
		if toIdx > n {
			toIdx = n
		}
	}
	return map[string]any{
		"success":    true,
		"path":       fullPath,
		"content":    strings.Join(rows[fromIdx:toIdx], "\n"),
		"totalLines": n,
		"start_line": fromIdx + 1,
		"end_line":   toIdx,
		"truncated":  limit != nil && toIdx < n,
	}
}

// CodingMemoryWriteWithContext 写入 coding_memory 文件。对齐 Python coding_memory_write_with_context
//
// 完整流程：
//  1. 校验路径 → ValidateCodingMemoryPath
//  2. 解析 frontmatter → ParseFrontmatter + ValidateFrontmatter
//  3. 丰富 frontmatter → EnrichFrontmatter (时间戳)
//  4. 重建内容 → RebuildContentWithFrontmatter
//  5. 提取 body → ExtractBody
//  6. 快照目录文件列表 → snapshotMemoryFiles
//  7. 冲突检测（分创建/追加两种模式）
//  8. 获取文件级锁 → getFileLock
//  9. 验证快照是否过期 → 乐观并发重试（最多 2 次）
//  10. 实际写入（创建/追加）
//  11. 更新 MEMORY.md 索引 → upsertMemoryIndex
//  12. 返回 WriteResult
func CodingMemoryWriteWithContext(ctx context.Context, toolCtx *CodingMemoryToolContext, path string, content string) map[string]any {
	if toolCtx == nil {
		return map[string]any{"success": false, "path": path, "error": "Workspace not initialized"}
	}
	ws := toolCtx.Workspace
	if ws == nil {
		return map[string]any{"success": false, "path": path, "error": "Workspace not initialized"}
	}
	isValid, resolved := ValidateCodingMemoryPath(path, ws)
	if !isValid {
		return map[string]any{"success": false, "path": path, "error": resolved}
	}
	// 对齐 Python step 2: frontmatter 解析验证
	fm := ParseFrontmatter(content)
	if fm == nil {
		return map[string]any{"success": false, "path": path, "error": "must contain frontmatter(name/description/type)"}
	}
	if ok, err := ValidateFrontmatter(fm); !ok {
		return map[string]any{"success": false, "path": path, "error": err}
	}
	// 对齐 Python step 3-4: 丰富 frontmatter 并重建内容
	fm = EnrichFrontmatter(fm, false)
	content = RebuildContentWithFrontmatter(content, fm)
	// 对齐 Python step 5: 提取 body
	body := ExtractBody(content)
	if body == "" {
		return map[string]any{"success": false, "path": path, "error": "no content body"}
	}

	basename := filepath.Base(resolved)
	memoryDir := resolveMemoryDir(toolCtx, resolved)

	// 对齐 Python step 6-9: 乐观并发 — 冲突检测在锁外运行，快照验证在锁内
	var conflictResult map[string]any
	for attempt := 0; attempt < maxConflictRetries; attempt++ {
		// 对齐 Python step 6: 快照目录文件列表
		snapshot := snapshotMemoryFiles(toolCtx, memoryDir)
		fileExists := false
		for _, name := range snapshot {
			if name == basename {
				fileExists = true
				break
			}
		}

		if !fileExists {
			// 对齐 Python step 7a: 创建模式 — 搜索相似文件
			conflictResult = map[string]any{"conflict_detected": false, "conflicting_files": []string{}}
			similarFiles := searchSimilar(toolCtx, body, basename, 5, 0.75)
			if len(similarFiles) > 0 {
				conflicting := make([]string, 0, len(similarFiles))
				for name := range similarFiles {
					conflicting = append(conflicting, name)
				}
				conflictResult["conflict_detected"] = true
				conflictResult["conflicting_files"] = conflicting
				conflictResult["note"] = fmt.Sprintf(
					"Conflicts with: %s. Use coding_memory_read to review, then coding_memory_edit to update.",
					strings.Join(conflicting, ", "),
				)
			}
			// ⤵️ 回填: 7.8 MemUpdateChecker — LLM 冗余判断，当前跳过 SKIP 逻辑
			// Python 中 runChecker 返回 actions 后，如果新记忆不在 actions 中（即 REDUNDANT），
			// 直接返回 WriteResult(mode=SKIP)。当前缺少 LLM 判断，无法判断冗余，故不做 SKIP。
		} else {
			// 对齐 Python step 7b: 追加模式 — 搜索自身 + 相似文件
			conflictResult = prepareAppendMode(toolCtx, resolved, basename, body, fm)
			// 检查是否 SKIP（当前 searchSimilar 不做 SKIP，仅 conflict 检测）
		}

		// 对齐 Python step 8-9: 文件级锁保护实际写入 + 快照验证
		fileLock := getFileLock(resolved)
		snapshotStale := false
		fileLock.Lock()
		currentSnapshot := snapshotMemoryFiles(toolCtx, memoryDir)
		if !snapshotEqual(currentSnapshot, snapshot) {
			// 对齐 Python: 快照过期，并发写入产生了新文件，重试冲突检测
			logger.Info(logger.ComponentAgentCore).
				Int("attempt", attempt+1).
				Msg("Snapshot stale, retrying conflict detection")
			snapshotStale = true
		} else {
			// 对齐 Python step 10: 实际写入
			sysOp := toolCtx.SysOperation
			if sysOp == nil {
				fileLock.Unlock()
				return map[string]any{"success": false, "path": path, "error": "no available sys_operation"}
			}
			if !fileExists {
				// 创建新文件
				_, err := sysOp.Fs().WriteFile(ctx, resolved, content, sysop.WithFsCreateIfNotExist(true))
				if err != nil {
					fileLock.Unlock()
					return map[string]any{"success": false, "path": path, "error": err.Error()}
				}
			} else {
				// 追加到已有文件
				appendToExistingFile(ctx, toolCtx, resolved, body, fm)
			}
		}
		fileLock.Unlock()

		if snapshotStale {
			continue
		}

		// 对齐 Python step 11: 更新 MEMORY.md 索引（索引有自己的锁，不需要在文件锁内运行）
		upsertMemoryIndex(ctx, toolCtx, memoryDir, basename, fm)

		// 对齐 Python step 12: 返回 WriteResult
		writeMode := WriteModeCreate
		if fileExists {
			writeMode = WriteModeAppend
		}
		wr := &WriteResult{
			Success: true,
			Path:    resolved,
			Mode:    writeMode,
			Type:    fm["type"],
		}
		// 合并冲突检测结果
		if cd, ok := conflictResult["conflict_detected"].(bool); ok && cd {
			wr.ConflictDetected = true
		}
		if cf, ok := conflictResult["conflicting_files"].([]string); ok {
			wr.ConflictingFiles = cf
		}
		if note, ok := conflictResult["note"].(string); ok {
			wr.Note = note
		}
		return wr.ToDict()
	}

	// 对齐 Python: 超过重试次数，降级为无快照验证写入
	logger.Warn(logger.ComponentAgentCore).
		Int("max_retries", maxConflictRetries).
		Msg("Exceeded max conflict retries, writing without snapshot validation")

	fileLock := getFileLock(resolved)
	fileLock.Lock()
	currentSnapshot := snapshotMemoryFiles(toolCtx, memoryDir)
	fileExistsNow := false
	for _, name := range currentSnapshot {
		if name == basename {
			fileExistsNow = true
			break
		}
	}
	sysOp := toolCtx.SysOperation
	if sysOp != nil {
		if !fileExistsNow {
			_, _ = sysOp.Fs().WriteFile(ctx, resolved, content, sysop.WithFsCreateIfNotExist(true))
		} else {
			appendToExistingFile(ctx, toolCtx, resolved, body, fm)
		}
	}
	fileLock.Unlock()

	upsertMemoryIndex(ctx, toolCtx, memoryDir, basename, fm)

	writeMode := WriteModeCreate
	if fileExistsNow {
		writeMode = WriteModeAppend
	}
	wr := &WriteResult{
		Success: true,
		Path:    resolved,
		Mode:    writeMode,
		Type:    fm["type"],
	}
	if conflictResult != nil {
		if cd, ok := conflictResult["conflict_detected"].(bool); ok && cd {
			wr.ConflictDetected = true
		}
		if cf, ok := conflictResult["conflicting_files"].([]string); ok {
			wr.ConflictingFiles = cf
		}
	}
	return wr.ToDict()
}

// CodingMemoryEditWithContext 编辑 coding_memory 文件。对齐 Python coding_memory_edit_with_context
func CodingMemoryEditWithContext(ctx context.Context, toolCtx *CodingMemoryToolContext, path string, oldText string, newText string) map[string]any {
	if oldText == "" {
		return map[string]any{"success": false, "error": "old_text cannot be empty"}
	}
	if toolCtx == nil {
		return map[string]any{"success": false, "error": "Workspace not initialized"}
	}
	ws := toolCtx.Workspace
	if ws == nil {
		return map[string]any{"success": false, "error": "Workspace not initialized"}
	}
	isValid, resolved := ValidateCodingMemoryPath(path, ws)
	if !isValid {
		return map[string]any{"success": false, "error": resolved}
	}
	memoryDir := resolveMemoryDir(toolCtx, resolved)
	sysOp := toolCtx.SysOperation
	if sysOp == nil {
		return map[string]any{"success": false, "error": "no available sys_operation"}
	}

	// 对齐 Python: 文件级锁保护 read-then-write，防止与其他 write/edit 协程竞争
	fileLock := getFileLock(resolved)
	fileLock.Lock()
	readResult, err := sysOp.Fs().ReadFile(ctx, resolved)
	if err != nil || readResult == nil || readResult.Data == nil {
		fileLock.Unlock()
		return map[string]any{"success": false, "error": fmt.Sprintf("failed to read file: %s", path)}
	}
	content := readResult.Data.Content
	occurrences := strings.Count(content, oldText)
	if occurrences == 0 {
		fileLock.Unlock()
		return map[string]any{"success": false, "error": "old_text not found in file"}
	}
	if occurrences > 1 {
		fileLock.Unlock()
		return map[string]any{"success": false, "error": fmt.Sprintf("old_text appears %d times, please be more specific", occurrences)}
	}
	newContent := strings.Replace(content, oldText, newText, 1)
	_, err = sysOp.Fs().WriteFile(ctx, resolved, newContent, sysop.WithFsCreateIfNotExist(true))
	fileLock.Unlock()
	if err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Msg("coding_memory_edit failed")
		return map[string]any{"success": false, "error": err.Error()}
	}

	// 对齐 Python: 更新 MEMORY.md 索引（索引有自己的锁，不需要在文件锁内运行）
	fm := ParseFrontmatter(newContent)
	if fm != nil {
		if ok, _ := ValidateFrontmatter(fm); ok {
			upsertMemoryIndex(ctx, toolCtx, memoryDir, filepath.Base(resolved), fm)
		}
	}

	return map[string]any{"success": true, "path": resolved, "new_content": newContent}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveMemoryDir 解析 coding_memory 目录。对齐 Python _resolve_memory_dir
func resolveMemoryDir(ctx *CodingMemoryToolContext, resolvedPath string) string {
	if ctx != nil && ctx.CodingMemoryDir != "" {
		return ctx.CodingMemoryDir
	}
	return filepath.Dir(resolvedPath)
}

// searchSimilar 语义搜索相似记忆。对齐 Python _search_similar
// ⤵️ 回填: 7.8 MemUpdateChecker — 当前仅基于搜索结果，不做 LLM 冗余判断
func searchSimilar(toolCtx *CodingMemoryToolContext, body string, excludePath string, topK int, threshold float64) map[string]string {
	oldMemories := make(map[string]string)
	if toolCtx == nil || toolCtx.Manager == nil {
		return oldMemories
	}
	results, err := toolCtx.Manager.Search(context.Background(), body, map[string]any{"max_results": topK})
	if err != nil {
		return oldMemories
	}
	memoryDir := toolCtx.CodingMemoryDir
	for _, r := range results {
		score, _ := r["score"].(float64)
		path, _ := r["path"].(string)
		if score <= threshold || path == "MEMORY.md" || path == excludePath {
			continue
		}
		// 读取文件内容
		sysOp := toolCtx.SysOperation
		if sysOp == nil {
			continue
		}
		fullPath := filepath.Join(memoryDir, path)
		readResult, err := sysOp.Fs().ReadFile(context.Background(), fullPath)
		if err != nil || readResult == nil || readResult.Data == nil {
			continue
		}
		oldBody := ExtractBody(readResult.Data.Content)
		if oldBody != "" {
			oldMemories[path] = oldBody
		}
	}
	return oldMemories
}

// runChecker 调用 MemUpdateChecker 执行 LLM 冲突检测。
// ⤵️ 回填: 7.8 MemUpdateChecker — 当前返回空列表，不做 LLM 判断
func runChecker(manager MemoryIndexManager, newID string, newBody string, oldMemories map[string]string) []any {
	// TODO: 7.8 实现 MemUpdateChecker 后替换
	return nil
}

// prepareAppendMode 准备追加模式并返回冲突检测结果。对齐 Python _prepare_append_mode
func prepareAppendMode(toolCtx *CodingMemoryToolContext, resolved string, basename string, body string, fm map[string]string) map[string]any {
	result := map[string]any{"conflict_detected": false, "conflicting_files": []string{}}

	// 构建旧记忆：自身文件 + 其他相似文件
	oldMemories := make(map[string]string)

	// 读取自身文件
	sysOp := toolCtx.SysOperation
	if sysOp != nil {
		readResult, err := sysOp.Fs().ReadFile(context.Background(), resolved)
		if err == nil && readResult != nil && readResult.Data != nil {
			existingBody := ExtractBody(readResult.Data.Content)
			if existingBody != "" {
				oldMemories["__self__"] = existingBody
			}
		}
	}

	// 其他相似文件
	other := searchSimilar(toolCtx, body, basename, 5, 0.75)
	for k, v := range other {
		oldMemories[k] = v
	}

	// ⤵️ 回填: 7.8 MemUpdateChecker — LLM 冗余/冲突判断
	// 当前仅基于 searchSimilar 结果判断冲突
	if len(other) > 0 {
		conflicting := make([]string, 0, len(other))
		for name := range other {
			conflicting = append(conflicting, name)
		}
		result["conflict_detected"] = true
		result["conflicting_files"] = conflicting
		result["note"] = fmt.Sprintf(
			"Conflicts with: %s. Use coding_memory_read to review, then coding_memory_edit to update.",
			strings.Join(conflicting, ", "),
		)
	}

	return result
}

// snapshotMemoryFiles 快照目录下的 .md 文件列表（排除 MEMORY.md）。对齐 Python _snapshot_memory_files
func snapshotMemoryFiles(toolCtx *CodingMemoryToolContext, memoryDir string) []string {
	if memoryDir == "" {
		return nil
	}
	// 对齐 Python: 使用 sys_op.fs().list_files() 读取目录
	// Go 中优先使用 os.ReadDir，如果 sysOp 可用则用 sysOp.Fs().ListFiles
	// 但 snapshotMemoryFiles 在 Python 中使用 sys_op.fs().list_files()，
	// 这里简化为 os.ReadDir，因为 coding_memory 目录在本地文件系统上
	entries, err := os.ReadDir(memoryDir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		if strings.EqualFold(name, "memory.md") {
			continue
		}
		names = append(names, name)
	}
	return names
}

// snapshotEqual 比较两个快照是否相等
func snapshotEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aMap := make(map[string]bool, len(a))
	for _, s := range a {
		aMap[s] = true
	}
	for _, s := range b {
		if !aMap[s] {
			return false
		}
	}
	return true
}

// getFileLock 获取文件级锁。对齐 Python _get_file_lock
// 使用双重检查锁定模式，确保每个文件路径只有一个锁对象
func getFileLock(path string) *sync.Mutex {
	fileLocksMu.Lock()
	defer fileLocksMu.Unlock()
	if lock, ok := fileLocks[path]; ok {
		return lock
	}
	lock := &sync.Mutex{}
	fileLocks[path] = lock
	return lock
}

// upsertMemoryIndex 增量更新 MEMORY.md 索引。对齐 Python _upsert_memory_index
// 使用 memoryIndexLock 保护并发修改，防止不同文件写入协程同时修改 MEMORY.md
func upsertMemoryIndex(ctx context.Context, toolCtx *CodingMemoryToolContext, memoryDir string, filename string, fm map[string]string) {
	memoryIndexLock.Lock()
	defer memoryIndexLock.Unlock()

	sysOp := toolCtx.SysOperation
	if sysOp == nil {
		return
	}
	if fm["name"] == "" || fm["description"] == "" {
		return
	}

	indexPath := filepath.Join(memoryDir, "MEMORY.md")
	newEntry := fmt.Sprintf("- [%s](%s) — %s", fm["name"], filename, fm["description"])

	var lines []string
	readResult, err := sysOp.Fs().ReadFile(ctx, indexPath)
	if err == nil && readResult != nil && readResult.Data != nil {
		lines = strings.Split(readResult.Data.Content, "\n")
	}

	// 对齐 Python: 如果文件名已存在则更新条目，否则在头部插入新条目
	found := false
	for i, line := range lines {
		if strings.Contains(line, fmt.Sprintf("](%s)", filename)) {
			lines[i] = newEntry
			found = true
			break
		}
	}
	if !found {
		lines = append([]string{newEntry}, lines...)
	}

	// 对齐 Python: 限制行数
	if len(lines) > maxIndexLines {
		lines = lines[:maxIndexLines]
	}
	newContent := strings.Join(lines, "\n")
	_, _ = sysOp.Fs().WriteFile(ctx, indexPath, newContent, sysop.WithFsCreateIfNotExist(true))
}

// appendToExistingFile 追加内容到已有文件并更新 frontmatter。对齐 Python _append_to_existing_file
func appendToExistingFile(ctx context.Context, toolCtx *CodingMemoryToolContext, resolved string, body string, fm map[string]string) {
	sysOp := toolCtx.SysOperation
	if sysOp == nil {
		logger.Error(logger.ComponentAgentCore).Msg("appendToExistingFile: sys_operation is nil")
		return
	}

	// 对齐 Python: 追加 body
	_, err := sysOp.Fs().WriteFile(ctx, resolved, "\n\n"+body,
		sysop.WithFsAppend(true),
		sysop.WithFsCreateIfNotExist(false),
	)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Str("path", resolved).Msg("追加内容失败")
		return
	}

	// 对齐 Python: 更新 frontmatter updated_at
	readResult, err := sysOp.Fs().ReadFile(ctx, resolved)
	if err != nil || readResult == nil || readResult.Data == nil {
		return
	}
	fmParsed := ParseFrontmatter(readResult.Data.Content)
	if fmParsed != nil {
		fmParsed = EnrichFrontmatter(fmParsed, true)
		updatedContent := RebuildContentWithFrontmatter(readResult.Data.Content, fmParsed)
		_, _ = sysOp.Fs().WriteFile(ctx, resolved, updatedContent, sysop.WithFsCreateIfNotExist(false))
	}
}

// readFileSafe 安全读取文件内容。对齐 Python _read_file_safe
func readFileSafe(ctx context.Context, toolCtx *CodingMemoryToolContext, filepath string) string {
	sysOp := toolCtx.SysOperation
	if sysOp == nil {
		return ""
	}
	readResult, err := sysOp.Fs().ReadFile(ctx, filepath)
	if err != nil || readResult == nil || readResult.Data == nil {
		return ""
	}
	return readResult.Data.Content
}
