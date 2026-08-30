package lite

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/update"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// maxIndexLines MEMORY.md 索引最大行数。对齐 Python MAX_INDEX_LINES
	maxIndexLines = 200
	// maxConflictRetries 乐观并发最大重试次数。对齐 Python _MAX_CONFLICT_RETRIES
	maxConflictRetries = 2
)

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentCore

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
		return false, "Workspace 未初始化"
	}
	if strings.Contains(path, "..") || strings.HasPrefix(path, "/") {
		return false, "无效路径：不允许目录遍历"
	}
	if !strings.HasSuffix(path, ".md") {
		return false, "路径必须以 .md 结尾"
	}
	memoryDir := ""
	if nodePath := ws.GetNodePath("coding_memory"); nodePath != nil {
		memoryDir = *nodePath
	}
	if memoryDir == "" {
		return false, "coding_memory 节点未配置"
	}
	resolved := filepath.Join(memoryDir, filepath.Base(path))
	return true, resolved
}

// CodingMemoryReadWithContext 读取 coding_memory 文件。对齐 Python coding_memory_read_with_context
func CodingMemoryReadWithContext(ctx context.Context, toolCtx *CodingMemoryToolContext, path string, offset *int, limit *int) (result *CodingReadResult) {
	// 对齐 Python: try/except 顶层异常保护
	defer func() {
		if r := recover(); r != nil {
			logger.Error(logComponent).Any("panic", r).Str("path", path).Msg("CodingMemoryReadWithContext 发生 panic")
			result = &CodingReadResult{Success: false, Path: path, Error: fmt.Sprintf("内部错误: %v", r)}
		}
	}()

	ws := toolCtx.Workspace
	if ws == nil {
		logger.Warn(logComponent).Str("path", path).Msg("CodingMemoryReadWithContext: Workspace 未初始化")
		return &CodingReadResult{Success: false, Path: path, Error: "Workspace 未初始化"}
	}
	isValid, resolvedPath := ValidateCodingMemoryPath(path, ws)
	if !isValid {
		logger.Warn(logComponent).Str("path", path).Str("reason", resolvedPath).Msg("CodingMemoryReadWithContext: 路径校验失败")
		return &CodingReadResult{Success: false, Path: path, Error: resolvedPath}
	}
	fullPath := resolvedPath
	sysOp := toolCtx.SysOperation
	if sysOp == nil {
		logger.Error(logComponent).Msg("读取记忆失败，无可用 sys_operation")
		return &CodingReadResult{Success: false, Path: path, Error: "读取失败，无可用 sys_operation"}
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
		logger.Warn(logComponent).Err(err).Str("path", path).Msg("CodingMemoryReadWithContext: 读取文件失败")
		return &CodingReadResult{Success: false, Path: path, Error: err.Error()}
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
	return &CodingReadResult{
		Success:    true,
		Path:       fullPath,
		Content:    strings.Join(rows[fromIdx:toIdx], "\n"),
		TotalLines: n,
		StartLine:  fromIdx + 1,
		EndLine:    toIdx,
		Truncated:  limit != nil && toIdx < n,
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
func CodingMemoryWriteWithContext(ctx context.Context, toolCtx *CodingMemoryToolContext, path string, content string) (result map[string]any) {
	// 对齐 Python: try/except 顶层异常保护
	defer func() {
		if r := recover(); r != nil {
			logger.Error(logComponent).Any("panic", r).Str("path", path).Msg("CodingMemoryWriteWithContext 发生 panic")
			result = map[string]any{"success": false, "path": path, "error": fmt.Sprintf("内部错误: %v", r)}
		}
	}()

	if toolCtx == nil {
		logger.Warn(logComponent).Str("path", path).Msg("CodingMemoryWriteWithContext: toolCtx 为 nil")
		return map[string]any{"success": false, "path": path, "error": "Workspace 未初始化"}
	}
	ws := toolCtx.Workspace
	if ws == nil {
		logger.Warn(logComponent).Str("path", path).Msg("CodingMemoryWriteWithContext: Workspace 未初始化")
		return map[string]any{"success": false, "path": path, "error": "Workspace 未初始化"}
	}
	isValid, resolved := ValidateCodingMemoryPath(path, ws)
	if !isValid {
		logger.Warn(logComponent).Str("path", path).Str("reason", resolved).Msg("CodingMemoryWriteWithContext: 路径校验失败")
		return map[string]any{"success": false, "path": path, "error": resolved}
	}
	// 对齐 Python step 2: frontmatter 解析验证
	fm := ParseFrontmatter(content)
	if fm == nil {
		logger.Warn(logComponent).Str("path", path).Msg("CodingMemoryWriteWithContext: frontmatter 解析失败")
		return map[string]any{"success": false, "path": path, "error": "必须包含 frontmatter(name/description/type)"}
	}
	if ok, err := ValidateFrontmatter(fm); !ok {
		logger.Warn(logComponent).Str("path", path).Str("error", err).Msg("CodingMemoryWriteWithContext: frontmatter 校验失败")
		return map[string]any{"success": false, "path": path, "error": err}
	}
	// 对齐 Python step 3-4: 丰富 frontmatter 并重建内容
	fm = EnrichFrontmatter(fm, false)
	content = RebuildContentWithFrontmatter(content, fm)
	// 对齐 Python step 5: 提取 body
	body := ExtractBody(content)
	if body == "" {
		logger.Warn(logComponent).Str("path", path).Msg("CodingMemoryWriteWithContext: 无内容体")
		return map[string]any{"success": false, "path": path, "error": "无内容体"}
	}

	basename := filepath.Base(resolved)
	memoryDir := resolveMemoryDir(toolCtx, resolved)

	// 对齐 Python step 6-9: 乐观并发 — 冲突检测在锁外运行，快照验证在锁内
	var conflict ConflictResult
	for attempt := 0; attempt < maxConflictRetries; attempt++ {
		// 对齐 Python step 6: 快照目录文件列表
		snapshot := snapshotMemoryFiles(toolCtx, memoryDir)
		fileExists := snapshot[basename]

		if !fileExists {
			// 对齐 Python step 7a: 创建模式 — 搜索相似文件
			conflict = ConflictResult{}
			similarFiles := searchSimilar(toolCtx, body, basename, 5, 0.75)

			// MemUpdateChecker 冗余/冲突判断（对齐 Python: if old_memories and manager and manager.llm: actions = _run_checker(...)）
			actions := runChecker(basename, body, similarFiles)

			// REDUNDANT: 新记忆不在 actions 中 → 跳过写入（对齐 Python: if actions and not any(a.id == basename for a in actions)）
			if len(actions) > 0 && !containsActionForID(actions, basename) {
				return (&WriteResult{
					Success: true,
					Path:    resolved,
					Mode:    WriteModeSkip,
					Type:    fm["type"],
					Note:    "Content is redundant with existing memories",
				}).ToDict()
			}

			// 收集冲突信息（对齐 Python: conflicting = [a.id for a in actions if a.id != basename and a.status == MemoryStatus.DELETE]）
			if len(actions) > 0 {
				var conflicting []string
				for _, a := range actions {
					if a.ID != basename && a.Status == update.MemoryStatusDelete {
						conflicting = append(conflicting, a.ID)
					}
				}
				if len(conflicting) > 0 {
					conflict.ConflictDetected = true
					conflict.ConflictingFiles = conflicting
					conflict.Note = fmt.Sprintf(
						"Conflicts with: %s. Use coding_memory_read to review, then coding_memory_edit to update.",
						strings.Join(conflicting, ", "),
					)
				}
			} else if len(similarFiles) > 0 {
				// 无 LLM 结果时，基于 searchSimilar 结果判断冲突
				conflicting := make([]string, 0, len(similarFiles))
				for name := range similarFiles {
					conflicting = append(conflicting, name)
				}
				conflict.ConflictDetected = true
				conflict.ConflictingFiles = conflicting
				conflict.Note = fmt.Sprintf(
					"与 %s 冲突。请使用 coding_memory_read 查看，然后 coding_memory_edit 更新。",
					strings.Join(conflicting, ", "),
				)
			}
		} else {
			// 对齐 Python step 7b: 追加模式 — 搜索自身 + 相似文件
			conflict = *prepareAppendMode(toolCtx, resolved, basename, body, fm)
			// 对齐 Python: if result.get("mode") == WriteMode.SKIP.value: return result
			if conflict.Skip {
				return (&WriteResult{
					Success: true,
					Path:    resolved,
					Mode:    WriteModeSkip,
					Type:    fm["type"],
					Note:    "Content is redundant with existing memories",
				}).ToDict()
			}
		}

		// 对齐 Python step 8-9: 文件级锁保护实际写入 + 快照验证
		fileLock := getFileLock(resolved)
		snapshotStale := false
		fileLock.Lock()
		currentSnapshot := snapshotMemoryFiles(toolCtx, memoryDir)
		if !snapshotEqual(currentSnapshot, snapshot) {
			// 对齐 Python: 快照过期，并发写入产生了新文件，重试冲突检测
			logger.Info(logComponent).
				Int("attempt", attempt+1).
				Msg("快照过期，重试冲突检测")
			snapshotStale = true
		} else {
			// 对齐 Python step 10: 实际写入
			sysOp := toolCtx.SysOperation
			if sysOp == nil {
				fileLock.Unlock()
				return map[string]any{"success": false, "path": path, "error": "无可用 sys_operation"}
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
			Success:          true,
			Path:             resolved,
			Mode:             writeMode,
			Type:             fm["type"],
			ConflictDetected: conflict.ConflictDetected,
			ConflictingFiles: conflict.ConflictingFiles,
			Note:             conflict.Note,
		}
		return wr.ToDict()
	}

	// 对齐 Python: 超过重试次数，降级为无快照验证写入
	logger.Warn(logComponent).
		Int("max_retries", maxConflictRetries).
		Bool("conflict_detected", conflict.ConflictDetected).
		Interface("conflicting_files", conflict.ConflictingFiles).
		Msg("超过最大冲突重试次数，跳过快照验证直接写入")

	fileLock := getFileLock(resolved)
	fileLock.Lock()
	currentSnapshot := snapshotMemoryFiles(toolCtx, memoryDir)
	fileExistsNow := currentSnapshot[basename]
	sysOp := toolCtx.SysOperation
	if sysOp != nil {
		if !fileExistsNow {
			// T07: 对齐 Python — 降级写入时也检查错误
			if _, err := sysOp.Fs().WriteFile(ctx, resolved, content, sysop.WithFsCreateIfNotExist(true)); err != nil {
				logger.Error(logComponent).Err(err).Str("path", resolved).Msg("降级写入失败")
				return map[string]any{"success": false, "path": path, "error": err.Error()}
			}
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
		Success:          true,
		Path:             resolved,
		Mode:             writeMode,
		Type:             fm["type"],
		ConflictDetected: conflict.ConflictDetected,
		ConflictingFiles: conflict.ConflictingFiles,
		Note:             conflict.Note,
	}
	return wr.ToDict()
}

// CodingMemoryEditWithContext 编辑 coding_memory 文件。对齐 Python coding_memory_edit_with_context
func CodingMemoryEditWithContext(ctx context.Context, toolCtx *CodingMemoryToolContext, path string, oldText string, newText string) (result *CodingEditResult) {
	// 对齐 Python: try/except 顶层异常保护
	defer func() {
		if r := recover(); r != nil {
			logger.Error(logComponent).Any("panic", r).Str("path", path).Msg("CodingMemoryEditWithContext 发生 panic")
			result = &CodingEditResult{Error: fmt.Sprintf("内部错误: %v", r)}
		}
	}()

	if oldText == "" {
		logger.Warn(logComponent).Str("path", path).Msg("CodingMemoryEditWithContext: old_text 为空")
		return &CodingEditResult{Error: "old_text 不能为空"}
	}
	if toolCtx == nil {
		logger.Warn(logComponent).Str("path", path).Msg("CodingMemoryEditWithContext: toolCtx 为 nil")
		return &CodingEditResult{Error: "Workspace 未初始化"}
	}
	ws := toolCtx.Workspace
	if ws == nil {
		logger.Warn(logComponent).Str("path", path).Msg("CodingMemoryEditWithContext: Workspace 未初始化")
		return &CodingEditResult{Error: "Workspace 未初始化"}
	}
	isValid, resolved := ValidateCodingMemoryPath(path, ws)
	if !isValid {
		logger.Warn(logComponent).Str("path", path).Str("reason", resolved).Msg("CodingMemoryEditWithContext: 路径校验失败")
		return &CodingEditResult{Error: resolved}
	}
	memoryDir := resolveMemoryDir(toolCtx, resolved)
	sysOp := toolCtx.SysOperation
	if sysOp == nil {
		logger.Warn(logComponent).Str("path", path).Msg("CodingMemoryEditWithContext: 无可用 sys_operation")
		return &CodingEditResult{Error: "无可用 sys_operation"}
	}

	// 对齐 Python: 文件级锁保护 read-then-write，防止与其他 write/edit 协程竞争
	fileLock := getFileLock(resolved)
	fileLock.Lock()
	readResult, err := sysOp.Fs().ReadFile(ctx, resolved)
	if err != nil || readResult == nil || readResult.Data == nil {
		fileLock.Unlock()
		logger.Warn(logComponent).Err(err).Str("path", path).Msg("CodingMemoryEditWithContext: 读取文件失败")
		return &CodingEditResult{Error: fmt.Sprintf("读取文件失败: %s", path)}
	}
	content := readResult.Data.Content
	occurrences := strings.Count(content, oldText)
	if occurrences == 0 {
		fileLock.Unlock()
		logger.Warn(logComponent).Str("path", path).Msg("CodingMemoryEditWithContext: old_text 未找到")
		return &CodingEditResult{Error: "old_text 在文件中未找到"}
	}
	if occurrences > 1 {
		fileLock.Unlock()
		logger.Warn(logComponent).Str("path", path).Int("occurrences", occurrences).Msg("CodingMemoryEditWithContext: old_text 出现多次")
		return &CodingEditResult{Error: fmt.Sprintf("old_text 出现 %d 次，请更精确地指定", occurrences)}
	}
	newContent := strings.Replace(content, oldText, newText, 1)
	_, err = sysOp.Fs().WriteFile(ctx, resolved, newContent, sysop.WithFsCreateIfNotExist(true))
	fileLock.Unlock()
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("coding_memory_edit 编辑失败")
		return &CodingEditResult{Error: err.Error()}
	}

	// 对齐 Python: 更新 MEMORY.md 索引（索引有自己的锁，不需要在文件锁内运行）
	fm := ParseFrontmatter(newContent)
	if fm != nil {
		if ok, _ := ValidateFrontmatter(fm); ok {
			upsertMemoryIndex(ctx, toolCtx, memoryDir, filepath.Base(resolved), fm)
		}
	}

	return &CodingEditResult{Success: true, Path: resolved, NewContent: newContent}
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
		if r.Score <= threshold || r.Path == "MEMORY.md" || r.Path == excludePath {
			continue
		}
		// 读取文件内容
		sysOp := toolCtx.SysOperation
		if sysOp == nil {
			continue
		}
		fullPath := filepath.Join(memoryDir, r.Path)
		readResult, err := sysOp.Fs().ReadFile(context.Background(), fullPath)
		if err != nil || readResult == nil || readResult.Data == nil {
			continue
		}
		oldBody := ExtractBody(readResult.Data.Content)
		if oldBody != "" {
			oldMemories[r.Path] = oldBody
		}
	}
	return oldMemories
}

// runChecker 调用 MemUpdateChecker 执行 LLM 冲突检测。
// 对齐 Python: coding_memory_tool_ops.py 中 runChecker 调用 MemUpdateChecker。
// 当前 CodingMemoryToolContext 不携带 ctx 和 model，故返回 nil（退化为不检查冗余）。
// 后续集成 LongTermMemory 门面后，可通过 Context 注入 model 启用 LLM 冗余判断。
func runChecker(newID string, newBody string, oldMemories map[string]string) []*update.MemoryActionItem {
	// 当前无 LLM 模型可用，返回 nil（对齐 MemUpdateChecker.Check model=nil 时的 fallback 行为）
	return nil
}

// prepareAppendMode 准备追加模式并返回冲突检测结果。对齐 Python _prepare_append_mode
func prepareAppendMode(toolCtx *CodingMemoryToolContext, resolved string, basename string, body string, fm map[string]string) *ConflictResult {
	result := &ConflictResult{}

	// 构建旧记忆：自身文件 + 其他相似文件（对齐 Python: old_memories）
	oldMemories := make(map[string]string)

	// 读取自身文件（对齐 Python: existing_body → old_memories["__self__"]）
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

	// 其他相似文件（对齐 Python: other = await _search_similar(...)）
	other := searchSimilar(toolCtx, body, basename, 5, 0.75)
	for k, v := range other {
		oldMemories[k] = v
	}

	// MemUpdateChecker 冗余/冲突判断（对齐 Python: if old_memories and manager and manager.llm: actions = _run_checker(...)）
	// 当前 runChecker 返回 nil（无 LLM 模型），仅基于 searchSimilar 结果判断冲突
	actions := runChecker(basename, body, oldMemories)

	// REDUNDANT: 新记忆不在 actions 中 → 跳过写入（对齐 Python: if actions and not any(a.id == basename for a in actions)）
	if len(actions) > 0 && !containsActionForID(actions, basename) {
		result.Skip = true
		return result
	}

	// 收集冲突信息（对齐 Python: conflicting = [a.id for a in actions if a.id != basename and a.status == MemoryStatus.DELETE]）
	if len(actions) > 0 {
		var conflicting []string
		for _, a := range actions {
			if a.ID != basename && a.Status == update.MemoryStatusDelete {
				// 对齐 Python: basename if a.id == "__self__" else a.id
				if a.ID == "__self__" {
					conflicting = append(conflicting, basename)
				} else {
					conflicting = append(conflicting, a.ID)
				}
			}
		}
		if len(conflicting) > 0 {
			result.ConflictDetected = true
			result.ConflictingFiles = conflicting
			result.Note = fmt.Sprintf(
				"Conflicts with: %s. Use coding_memory_read to review, then coding_memory_edit to update.",
				strings.Join(conflicting, ", "),
			)
		}
	} else if len(other) > 0 {
		// 无 LLM 结果时，基于 searchSimilar 结果判断冲突
		conflicting := make([]string, 0, len(other))
		for name := range other {
			conflicting = append(conflicting, name)
		}
		result.ConflictDetected = true
		result.ConflictingFiles = conflicting
		result.Note = fmt.Sprintf(
			"与 %s 冲突。请使用 coding_memory_read 查看，然后 coding_memory_edit 更新。",
			strings.Join(conflicting, ", "),
		)
	}

	return result
}

// snapshotMemoryFiles 快照目录下的 .md 文件列表（排除 MEMORY.md）。对齐 Python _snapshot_memory_files
// 使用 sys_operation.Fs().ListFiles() 读取目录，对齐 Python ctx.sys_operation.fs().list_files()
// 返回 map[string]bool 对齐 Python frozenset 语义（无序、去重、O(1) 查找）
func snapshotMemoryFiles(toolCtx *CodingMemoryToolContext, memoryDir string) map[string]bool {
	if memoryDir == "" {
		return nil
	}
	sysOp := toolCtx.SysOperation
	if sysOp == nil {
		return nil
	}
	listResult, err := sysOp.Fs().ListFiles(context.Background(), memoryDir)
	if err != nil || listResult == nil || listResult.Data == nil {
		return nil
	}
	names := make(map[string]bool)
	for _, item := range listResult.Data.ListItems {
		if item.IsDirectory {
			continue
		}
		name := item.Name
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		if strings.EqualFold(name, "memory.md") {
			continue
		}
		names[name] = true
	}
	return names
}

// snapshotEqual 比较两个快照是否相等。对齐 Python frozenset != frozenset
func snapshotEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
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
		logger.Debug(logComponent).Str("path", filename).Msg("upsertMemoryIndex: name 或 description 为空，跳过索引更新")
		return
	}

	indexPath := filepath.Join(memoryDir, "MEMORY.md")
	newEntry := fmt.Sprintf("- [%s](%s) — %s", fm["name"], filename, fm["description"])

	var lines []string
	readResult, err := sysOp.Fs().ReadFile(ctx, indexPath)
	if err == nil && readResult != nil && readResult.Data != nil {
		lines = strings.Split(readResult.Data.Content, "\n")
	} else if err != nil {
		// 对齐 Python: Failed to read memory index 时记录 warning
		logger.Warn(logComponent).Err(err).Str("path", indexPath).Msg("upsertMemoryIndex: 读取 MEMORY.md 索引失败")
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
	_, _ = sysOp.Fs().WriteFile(ctx, indexPath, newContent, sysop.WithFsCreateIfNotExist(true), sysop.WithFsPrependNewline(false))
}

// appendToExistingFile 追加内容到已有文件并更新 frontmatter。对齐 Python _append_to_existing_file
func appendToExistingFile(ctx context.Context, toolCtx *CodingMemoryToolContext, resolved string, body string, fm map[string]string) {
	sysOp := toolCtx.SysOperation
	if sysOp == nil {
		logger.Error(logComponent).Msg("appendToExistingFile: sys_operation 为 nil")
		return
	}

	// 对齐 Python: 追加 body
	_, err := sysOp.Fs().WriteFile(ctx, resolved, "\n\n"+body,
		sysop.WithFsAppend(true),
		sysop.WithFsCreateIfNotExist(false),
	)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("path", resolved).Msg("追加内容失败")
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

// containsActionForID 检查 action 列表中是否包含指定 ID 的 ADD 操作。
func containsActionForID(actions []*update.MemoryActionItem, id string) bool {
	for _, a := range actions {
		if a.ID == id && a.Status == update.MemoryStatusAdd {
			return true
		}
	}
	return false
}
