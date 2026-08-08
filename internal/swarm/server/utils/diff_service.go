package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/filesystem"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DiffService 提供 turn-based diff 查询服务。
// 对齐 Python: DiffService (diff_service.py line 20-465)
type DiffService struct {
	// agentID Agent 标识，默认 "jiuwenswarm"
	agentID string
}

// TurnDiff 单轮对话的 diff 信息。
// 对齐 Python: _compute_turn_diffs 返回值中的每个 turn dict
type TurnDiff struct {
	// TurnIndex 轮次序号（1-based）
	TurnIndex int `json:"turnIndex"`
	// UserPromptPreview 用户消息预览（前 30 字符）
	UserPromptPreview string `json:"userPromptPreview"`
	// Timestamp 轮次时间戳（ISO 8601）
	Timestamp string `json:"timestamp"`
	// StartTimestamp 轮次起始时间戳
	StartTimestamp float64 `json:"start_timestamp"`
	// EndTimestamp 轮次结束时间戳（下一个 user 消息的时间）
	EndTimestamp float64 `json:"end_timestamp"`
	// Files 文件变更详情
	Files map[string]*FileDiff `json:"files"`
	// Stats 统计信息
	Stats TurnStats `json:"stats"`
}

// FileDiff 单个文件的 diff 信息。
type FileDiff struct {
	// FilePath 文件路径
	FilePath string `json:"filePath"`
	// Hunks diff hunks 列表
	Hunks []Hunk `json:"hunks"`
	// IsNewFile 是否是新建文件
	IsNewFile bool `json:"isNewFile"`
	// LinesAdded 新增行数
	LinesAdded int `json:"linesAdded"`
	// LinesRemoved 删除行数
	LinesRemoved int `json:"linesRemoved"`
	// LastEditTime 最后编辑时间
	LastEditTime string `json:"lastEditTime"`
}

// Hunk 一个 diff hunk。
type Hunk struct {
	// OldStart 旧文件起始行号
	OldStart int `json:"oldStart"`
	// OldLines 旧文件行数
	OldLines int `json:"oldLines"`
	// NewStart 新文件起始行号
	NewStart int `json:"newStart"`
	// NewLines 新文件行数
	NewLines int `json:"newLines"`
	// Lines diff 行列表（+/- 前缀）
	Lines []string `json:"lines"`
}

// TurnDiffStats 轮次统计信息。
type TurnStats struct {
	// FilesChanged 变更文件数
	FilesChanged int `json:"filesChanged"`
	// LinesAdded 新增行数
	LinesAdded int `json:"linesAdded"`
	// LinesRemoved 删除行数
	LinesRemoved int `json:"linesRemoved"`
}

// FileRestoreInfo 文件恢复信息。
type FileRestoreInfo struct {
	// RestoreContent 恢复内容（nil 表示应删除文件）
	RestoreContent *string `json:"restore_content"`
	// Action 操作类型："write" 或 "delete"
	Action string `json:"action"`
}

// historyRecord session history 记录
type historyRecord struct {
	Role      string  `json:"role"`
	Content   string  `json:"content"`
	Timestamp float64 `json:"timestamp"`
}

// opEntry 操作历史条目
type opEntry struct {
	Action     string  `json:"action"`
	Timestamp  string  `json:"timestamp"`
	OldContent *string `json:"old_content"`
	NewContent *string `json:"new_content"`
}

// opCode diff 操作码，对齐 Python difflib.SequenceMatcher.get_opcodes() 输出
type opCode struct {
	tag string // "equal", "delete", "insert", "replace"
	i1  int    // old 起始索引
	i2  int    // old 结束索引（exclusive）
	j1  int    // new 起始索引
	j2  int    // new 结束索引（exclusive）
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// diffServiceInstance DiffService 单例实例
var diffServiceInstance *DiffService

// diffServiceOnce 单例初始化 Once
var diffServiceOnce sync.Once

// ──────────────────────────── 导出函数 ────────────────────────────

// GetDiffService 获取 DiffService 单例实例。
// 对齐 Python: get_diff_service() (line 470-475)
func GetDiffService() *DiffService {
	diffServiceOnce.Do(func() {
		diffServiceInstance = &DiffService{
			agentID: "jiuwenswarm",
		}
	})
	return diffServiceInstance
}

// GetTurnDiffs 获取 session 的所有 turn diff（完整信息）。
// 对齐 Python: DiffService.get_turn_diffs(session_id, project_dir) (line 26-37)
//
// Returns: turn diff 列表，按时间倒序排列（most recent first）
func (ds *DiffService) GetTurnDiffs(sessionID string, projectDir string) []TurnDiff {
	turns := ds.computeTurnDiffs(sessionID, projectDir)
	// 对齐 Python: list(reversed(turns))
	reversed := make([]TurnDiff, len(turns))
	for i, t := range turns {
		reversed[len(turns)-1-i] = t
	}
	return reversed
}

// GetFilesToRestore 返回需要恢复的文件及其目标内容。
// 对齐 Python: DiffService.get_files_to_restore(session_id, turn_index, project_dir) (line 405-464)
//
// 对于在 turn_index 及之后所有 turn 中被修改的文件，
// 找到它们在 turn_index 开始前的状态（old_content of the first edit at/after the target turn），
// 以便恢复操作将文件写回。
func (ds *DiffService) GetFilesToRestore(sessionID string, turnIndex int, projectDir string) map[string]*FileRestoreInfo {
	history := ds.readHistory(sessionID)
	if len(history) == 0 {
		return nil
	}

	// 1. 找到目标 turn 的起始时间
	var targetTimestamp float64
	userCount := 0
	for _, record := range history {
		if record.Role == "user" {
			userCount++
			if userCount == turnIndex {
				targetTimestamp = record.Timestamp
				break
			}
		}
	}

	if targetTimestamp == 0 {
		return nil
	}

	// 2. 读取 file_ops 日志
	agentHistory := ds.readAgentHistory(sessionID, projectDir)

	// 3. 对于每个文件，找到第一条 timestamp >= target_timestamp 的 entry
	filesToRestore := make(map[string]*FileRestoreInfo)
	for filePath, entries := range agentHistory {
		for _, entry := range entries {
			editTime := isoToTimestamp(entry.Timestamp)
			if editTime >= targetTimestamp {
				if entry.OldContent != nil {
					filesToRestore[filePath] = &FileRestoreInfo{
						RestoreContent: entry.OldContent,
						Action:         "write",
					}
				} else {
					// 文件由 agent 创建，恢复时应删除
					filesToRestore[filePath] = &FileRestoreInfo{
						RestoreContent: nil,
						Action:         "delete",
					}
				}
				break // 只需要第一条匹配的 entry
			}
		}
	}

	return filesToRestore
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// splitLines 对齐 Python str.splitlines()（不带 keepends）。
// 按行分割，不保留行尾符，末尾空行不产生额外元素。
// 正确处理 \r\n 和 \r 行尾符。
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var lines []string
	start := 0
	for i := 0; i < len(s); {
		c := s[i]
		var lineEnd int
		var nextStart int
		switch c {
		case '\r':
			if i+1 < len(s) && s[i+1] == '\n' {
				lineEnd = i // 不含 \r\n
				nextStart = i + 2
			} else {
				lineEnd = i // 不含 \r
				nextStart = i + 1
			}
		case '\n':
			lineEnd = i // 不含 \n
			nextStart = i + 1
		default:
			i++
			continue
		}
		lines = append(lines, s[start:lineEnd])
		start = nextStart
		i = nextStart
	}
	// 末尾无换行符时，剩余内容作为最后一行
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// splitLinesKeepEnds 对齐 Python str.splitlines(keepends=True)。
// 按行分割，保留行尾符（\n、\r\n、\r），末尾空行不产生额外元素。
func splitLinesKeepEnds(s string) []string {
	if s == "" {
		return nil
	}
	var lines []string
	start := 0
	for i := 0; i < len(s); {
		c := s[i]
		var lineEnd int
		var nextStart int
		switch c {
		case '\r':
			if i+1 < len(s) && s[i+1] == '\n' {
				lineEnd = i + 2 // 含 \r\n
				nextStart = i + 2
			} else {
				lineEnd = i + 1 // 含 \r
				nextStart = i + 1
			}
		case '\n':
			lineEnd = i + 1 // 含 \n
			nextStart = i + 1
		default:
			i++
			continue
		}
		lines = append(lines, s[start:lineEnd])
		start = nextStart
		i = nextStart
	}
	// 末尾无换行符时，剩余内容作为最后一行
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// computeTurnDiffs 计算 turn-based diffs。
// 对齐 Python: _compute_turn_diffs (line 39-125)
func (ds *DiffService) computeTurnDiffs(sessionID string, projectDir string) []TurnDiff {
	history := ds.readHistory(sessionID)
	agentHistory := ds.readAgentHistory(sessionID, projectDir)

	if len(history) == 0 {
		return nil
	}

	var turns []TurnDiff

	for i, record := range history {
		if record.Role == "user" {
			turnStart := record.Timestamp
			turnEnd := ds.findNextUserTime(history, i)

			turn := TurnDiff{
				TurnIndex:         len(turns) + 1,
				UserPromptPreview: truncate(record.Content, 30),
				Timestamp:         timestampToISO(record.Timestamp),
				StartTimestamp:    turnStart,
				EndTimestamp:      turnEnd,
				Files:             make(map[string]*FileDiff),
				Stats:             TurnStats{},
			}
			turns = append(turns, turn)
		}
	}

	// 为每个 turn 匹配文件编辑
	for i := range turns {
		fileEdits := ds.findFileEditsByTimeRange(
			agentHistory,
			turns[i].StartTimestamp,
			turns[i].EndTimestamp,
		)

		for filePath, editInfo := range fileEdits {
			if turns[i].Files[filePath] == nil {
				turns[i].Files[filePath] = &FileDiff{
					FilePath: filePath,
				}
			}

			for _, op := range editInfo {
				hunks := computeHunks(op.OldContent, op.NewContent)
				turns[i].Files[filePath].Hunks = append(turns[i].Files[filePath].Hunks, hunks...)
				turns[i].Files[filePath].LastEditTime = op.Timestamp

				if op.Action == "write" && op.OldContent == nil {
					turns[i].Files[filePath].IsNewFile = true
				}

				for _, hunk := range hunks {
					for _, line := range hunk.Lines {
						if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
							turns[i].Files[filePath].LinesAdded++
						} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
							turns[i].Files[filePath].LinesRemoved++
						}
					}
				}
			}
		}

		turns[i].Stats.FilesChanged = len(turns[i].Files)
		turns[i].Stats.LinesAdded = sumLinesAdded(turns[i].Files)
		turns[i].Stats.LinesRemoved = sumLinesRemoved(turns[i].Files)
	}

	// 只保留有文件变更的 turn
	var turnsWithFiles []TurnDiff
	for _, t := range turns {
		if len(t.Files) > 0 {
			turnsWithFiles = append(turnsWithFiles, t)
		}
	}
	return turnsWithFiles
}

// findNextUserTime 查找下一次 user 消息的时间戳。
// 对齐 Python: _find_next_user_time (line 138-145)
func (ds *DiffService) findNextUserTime(history []historyRecord, userIndex int) float64 {
	for j := userIndex + 1; j < len(history); j++ {
		if history[j].Role == "user" {
			return history[j].Timestamp
		}
	}
	return 0
}

// readHistory 读取 session history。
// 对齐 Python: _read_history (line 148-156)
func (ds *DiffService) readHistory(sessionID string) []historyRecord {
	sessionsDir := workspace.AgentSessionsDir()
	historyFile := filepath.Join(sessionsDir, sessionID, "history.json")

	data, err := os.ReadFile(historyFile)
	if err != nil {
		return nil
	}

	var records []historyRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil
	}
	return records
}

// readAgentHistory 读取 .agent_history 目录下多个文件并合并。
// 对齐 Python: _read_agent_history (line 195-281)
func (ds *DiffService) readAgentHistory(sessionID string, projectDir string) map[string][]filesystem.OpHistoryEntry {
	result := make(map[string][]filesystem.OpHistoryEntry)

	// 收集所有 history 文件路径
	var paths []string

	// 1. 从 Agent Workspace 和 User Workspace 读取
	agentHistDir := filepath.Join(workspace.AgentWorkspaceDir(), ".agent_history")
	userHistDir := filepath.Join(workspace.WorkspaceDir(), ".agent_history")

	paths = append(paths,
		filepath.Join(agentHistDir, fmt.Sprintf("file_ops_%s.json", ds.agentID)),
		filepath.Join(userHistDir, fmt.Sprintf("file_ops_%s.json", ds.agentID)),
	)

	// 2. session-specific file_ops 文件
	for _, baseDir := range []string{workspace.AgentWorkspaceDir(), workspace.WorkspaceDir()} {
		histDir := filepath.Join(baseDir, ".agent_history")
		entries, err := os.ReadDir(histDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if ds.isValidFileOpsFile(entry.Name(), sessionID, true) {
				paths = append(paths, filepath.Join(histDir, entry.Name()))
			}
		}
	}

	// 3. 从项目目录读取
	if projectDir == "" && sessionID != "" {
		projectDir = ds.getProjectDirFromMetadata(sessionID)
	}
	if projectDir != "" {
		projectHistDir := filepath.Join(projectDir, ".agent_history")
		entries, err := os.ReadDir(projectHistDir)
		if err == nil {
			for _, entry := range entries {
				if ds.isValidFileOpsFile(entry.Name(), sessionID, false) {
					paths = append(paths, filepath.Join(projectHistDir, entry.Name()))
				}
			}
			// 全局 file_ops 文件
			globalFile := filepath.Join(projectHistDir, fmt.Sprintf("file_ops_%s.json", ds.agentID))
			if _, err := os.Stat(globalFile); err == nil {
				paths = append(paths, globalFile)
			}
		}
	}

	// 合并所有文件
	for _, historyFile := range paths {
		data, err := os.ReadFile(historyFile)
		if err != nil {
			continue
		}

		var fileData map[string]any
		if err := json.Unmarshal(data, &fileData); err != nil {
			logger.Warn(logger.ComponentGateway).
				Str("history_file", historyFile).
				Err(err).
				Msg("读取 agent history 文件失败")
			continue
		}

		for filePath, entriesRaw := range fileData {
			normalizedPath := normalizePath(filePath)
			entriesList, ok := entriesRaw.([]any)
			if !ok {
				continue
			}

			for _, entryRaw := range entriesList {
				entryMap, ok := entryRaw.(map[string]any)
				if !ok {
					continue
				}

				entry := parseOpEntry(entryMap)
				if entry == nil {
					continue
				}

				// 去重：检查时间戳相近（±2秒）的相同操作
				if !isDuplicateEntry(result[normalizedPath], entry) {
					result[normalizedPath] = append(result[normalizedPath], *entry)
				}
			}
		}
	}

	// 对每个文件的 entries 按时间排序
	for filePath, entries := range result {
		sort.Slice(entries, func(i, j int) bool {
			return isoToTimestamp(entries[i].Timestamp) < isoToTimestamp(entries[j].Timestamp)
		})
		result[filePath] = entries
	}

	return result
}

// findFileEditsByTimeRange 根据时间范围查找文件编辑记录。
// 对齐 Python: _find_file_edits_by_time_range (line 283-313)
func (ds *DiffService) findFileEditsByTimeRange(
	agentHistory map[string][]filesystem.OpHistoryEntry,
	startTime, endTime float64,
) map[string][]filesystem.OpHistoryEntry {
	fileEdits := make(map[string][]filesystem.OpHistoryEntry)

	for filePath, entries := range agentHistory {
		for _, entry := range entries {
			editTime := isoToTimestamp(entry.Timestamp)

			if editTime >= startTime {
				if endTime == 0 || editTime < endTime {
					fileEdits[filePath] = append(fileEdits[filePath], entry)
				}
			}
		}
	}

	return fileEdits
}

// isValidFileOpsFile 检查文件名是否是有效的 file_ops 文件。
// 对齐 Python: _is_valid_file_ops_file (line 183-193)
func (ds *DiffService) isValidFileOpsFile(name string, sessionID string, requireSession bool) bool {
	prefix := fmt.Sprintf("file_ops_%s_", ds.agentID)
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	if !strings.HasSuffix(name, ".json") {
		return false
	}
	if requireSession {
		return sessionID != "" && strings.Contains(name, sessionID)
	}
	return sessionID == "" || strings.Contains(name, sessionID)
}

// getProjectDirFromMetadata 从 session metadata.json 中读取项目目录。
// 对齐 Python: _get_project_dir_from_metadata (line 159-181)
func (ds *DiffService) getProjectDirFromMetadata(sessionID string) string {
	sessionsDir := workspace.AgentSessionsDir()
	metadataFile := filepath.Join(sessionsDir, sessionID, "metadata.json")

	data, err := os.ReadFile(metadataFile)
	if err != nil {
		return ""
	}

	var metadata map[string]any
	if err := json.Unmarshal(data, &metadata); err != nil {
		return ""
	}

	// 从 channel_metadata.cwd 或 delivery_context.route_metadata.cwd 获取
	channelMeta, ok := metadata["channel_metadata"].(map[string]any)
	if ok {
		if cwd, ok := channelMeta["cwd"].(string); ok && cwd != "" {
			return strings.TrimSpace(cwd)
		}
	}

	deliveryCtx, ok := metadata["delivery_context"].(map[string]any)
	if ok {
		routeMeta, ok := deliveryCtx["route_metadata"].(map[string]any)
		if ok {
			if cwd, ok := routeMeta["cwd"].(string); ok && cwd != "" {
				return strings.TrimSpace(cwd)
			}
		}
	}

	return ""
}

// computeHunks 计算结构化 diff hunks（line-level）。
// 对齐 Python: _compute_hunks (line 327-392)
// 使用简易行级 diff 算法（对齐 Python difflib.SequenceMatcher 的 line-level 输出）
func computeHunks(oldContent, newContent *string) []Hunk {
	// 处理删除文件的情况：newContent 为 nil
	if newContent == nil {
		if oldContent == nil {
			return nil
		}
		lines := splitLines(*oldContent)
		hunkLines := make([]string, len(lines))
		for i, line := range lines {
			hunkLines[i] = "-" + line
		}
		return []Hunk{
			{
				OldStart: 1,
				OldLines: len(lines),
				NewStart: 0,
				NewLines: 0,
				Lines:    hunkLines,
			},
		}
	}

	// 处理新建文件的情况：oldContent 为 nil
	if oldContent == nil {
		lines := splitLines(*newContent)
		hunkLines := make([]string, len(lines))
		for i, line := range lines {
			hunkLines[i] = "+" + line
		}
		return []Hunk{
			{
				OldStart: 0,
				OldLines: 0,
				NewStart: 1,
				NewLines: len(lines),
				Lines:    hunkLines,
			},
		}
	}

	// 行级 diff：对齐 Python difflib.SequenceMatcher(None, old_lines, new_lines)
	oldLines := splitLinesKeepEnds(*oldContent)
	newLines := splitLinesKeepEnds(*newContent)

	if len(oldLines) == 0 && len(newLines) == 0 {
		return nil
	}

	// 使用 LCS（最长公共子序列）计算 line-level opcodes
	opcodes := computeLineOpCodes(oldLines, newLines)

	var hunks []Hunk
	var currentHunk *Hunk

	for _, op := range opcodes {
		switch op.tag {
		case "equal":
			if currentHunk != nil {
				hunks = append(hunks, *currentHunk)
				currentHunk = nil
			}

		case "delete":
			if currentHunk == nil {
				currentHunk = &Hunk{
					OldStart: op.i1 + 1,
					NewStart: op.j1 + 1,
				}
			}
			for k := op.i1; k < op.i2; k++ {
				// 对齐 Python: line.rstrip() 去除行尾符
				currentHunk.Lines = append(currentHunk.Lines, "-"+strings.TrimRight(oldLines[k], "\r\n"))
			}
			currentHunk.OldLines += op.i2 - op.i1

		case "insert":
			if currentHunk == nil {
				currentHunk = &Hunk{
					OldStart: op.i1 + 1,
					NewStart: op.j1 + 1,
				}
			}
			for k := op.j1; k < op.j2; k++ {
				// 对齐 Python: line.rstrip() 去除行尾符
				currentHunk.Lines = append(currentHunk.Lines, "+"+strings.TrimRight(newLines[k], "\r\n"))
			}
			currentHunk.NewLines += op.j2 - op.j1

		case "replace":
			if currentHunk == nil {
				currentHunk = &Hunk{
					OldStart: op.i1 + 1,
					NewStart: op.j1 + 1,
				}
			}
			for k := op.i1; k < op.i2; k++ {
				// 对齐 Python: line.rstrip() 去除行尾符
				currentHunk.Lines = append(currentHunk.Lines, "-"+strings.TrimRight(oldLines[k], "\r\n"))
			}
			currentHunk.OldLines += op.i2 - op.i1
			for k := op.j1; k < op.j2; k++ {
				// 对齐 Python: line.rstrip() 去除行尾符
				currentHunk.Lines = append(currentHunk.Lines, "+"+strings.TrimRight(newLines[k], "\r\n"))
			}
			currentHunk.NewLines += op.j2 - op.j1
		}
	}

	if currentHunk != nil {
		hunks = append(hunks, *currentHunk)
	}

	return hunks
}

// isoToTimestamp 将 ISO 8601 字符串转换为 Unix timestamp。
// 对齐 Python: _iso_to_timestamp (line 316-319)
func isoToTimestamp(isoStr string) float64 {
	if isoStr == "" {
		return 0
	}
	// 尝试多种格式
	t, err := time.Parse(time.RFC3339Nano, isoStr)
	if err != nil {
		t, err = time.Parse(time.RFC3339, isoStr)
		if err != nil {
			return 0
		}
	}
	return float64(t.Unix()) + float64(t.Nanosecond())/1e9
}

// timestampToISO 将 Unix timestamp 转换为 ISO 8601 字符串。
// 对齐 Python: _timestamp_to_iso (line 322-325)
func timestampToISO(timestamp float64) string {
	sec := int64(timestamp)
	nsec := int64((timestamp - float64(sec)) * 1e9)
	t := time.Unix(sec, nsec).UTC()
	return t.Format(time.RFC3339Nano)
}

// normalizePath 规范化路径：统一大小写和斜杠方向
func normalizePath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return strings.ToLower(strings.ReplaceAll(p, "\\", "/"))
	}
	return abs
}

// parseOpEntry 从 map[string]any 解析操作历史条目
func parseOpEntry(m map[string]any) *filesystem.OpHistoryEntry {
	action, _ := m["action"].(string)
	timestamp, _ := m["timestamp"].(string)
	if action == "" || timestamp == "" {
		return nil
	}

	var oldContent, newContent *string
	if v, ok := m["old_content"]; ok && v != nil {
		s, ok := v.(string)
		if ok {
			oldContent = &s
		}
	}
	if v, ok := m["new_content"]; ok && v != nil {
		s, ok := v.(string)
		if ok {
			newContent = &s
		}
	}

	return &filesystem.OpHistoryEntry{
		Action:     action,
		Timestamp:  timestamp,
		OldContent: oldContent,
		NewContent: newContent,
	}
}

// isDuplicateEntry 检查是否是重复条目（时间戳相近 ±2秒，相同操作）
func isDuplicateEntry(existing []filesystem.OpHistoryEntry, newEntry *filesystem.OpHistoryEntry) bool {
	for _, e := range existing {
		if e.Action == newEntry.Action {
			t1 := isoToTimestamp(e.Timestamp)
			t2 := isoToTimestamp(newEntry.Timestamp)
			if abs(t1-t2) < 2 {
				return true
			}
		}
	}
	return false
}

// truncate 截断字符串到指定长度
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// abs 浮点绝对值
func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// sumLinesAdded 计算所有文件的 LinesAdded 总和
func sumLinesAdded(files map[string]*FileDiff) int {
	total := 0
	for _, f := range files {
		total += f.LinesAdded
	}
	return total
}

// sumLinesRemoved 计算所有文件的 LinesRemoved 总和
func sumLinesRemoved(files map[string]*FileDiff) int {
	total := 0
	for _, f := range files {
		total += f.LinesRemoved
	}
	return total
}

// computeLineOpCodes 使用 LCS 算法计算行级 diff 操作码。
// 对齐 Python: difflib.SequenceMatcher(None, old_lines, new_lines).get_opcodes()
func computeLineOpCodes(oldLines, newLines []string) []opCode {
	m := len(oldLines)
	n := len(newLines)

	// 构建 LCS 长度矩阵（标准 DP）
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if oldLines[i-1] == newLines[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	// 回溯 LCS 得到匹配对
	var matches [][2]int
	i, j := m, n
	for i > 0 && j > 0 {
		if oldLines[i-1] == newLines[j-1] {
			matches = append(matches, [2]int{i - 1, j - 1})
			i--
			j--
		} else if dp[i-1][j] >= dp[i][j-1] {
			i--
		} else {
			j--
		}
	}

	// 反转（回溯是从尾部开始的）
	for left, right := 0, len(matches)-1; left < right; left, right = left+1, right-1 {
		matches[left], matches[right] = matches[right], matches[left]
	}

	// 从匹配对生成 opcodes
	var codes []opCode
	oi, nj := 0, 0

	for _, match := range matches {
		mi, mj := match[0], match[1]

		// 先处理未匹配区间
		if mi > oi || mj > nj {
			if mi > oi && mj > nj {
				codes = append(codes, opCode{tag: "replace", i1: oi, i2: mi, j1: nj, j2: mj})
			} else if mi > oi {
				codes = append(codes, opCode{tag: "delete", i1: oi, i2: mi, j1: nj, j2: nj})
			} else {
				codes = append(codes, opCode{tag: "insert", i1: oi, i2: oi, j1: nj, j2: mj})
			}
		}

		// 等行（单个匹配行）
		codes = append(codes, opCode{tag: "equal", i1: mi, i2: mi + 1, j1: mj, j2: mj + 1})
		oi = mi + 1
		nj = mj + 1
	}

	// 尾部未匹配部分
	if oi < m || nj < n {
		if oi < m && nj < n {
			codes = append(codes, opCode{tag: "replace", i1: oi, i2: m, j1: nj, j2: n})
		} else if oi < m {
			codes = append(codes, opCode{tag: "delete", i1: oi, i2: m, j1: nj, j2: n})
		} else {
			codes = append(codes, opCode{tag: "insert", i1: oi, i2: m, j1: nj, j2: n})
		}
	}

	// 合并连续的 equal opcodes
	var merged []opCode
	for _, code := range codes {
		if len(merged) > 0 && merged[len(merged)-1].tag == "equal" && code.tag == "equal" &&
			merged[len(merged)-1].i2 == code.i1 && merged[len(merged)-1].j2 == code.j1 {
			merged[len(merged)-1].i2 = code.i2
			merged[len(merged)-1].j2 = code.j2
		} else {
			merged = append(merged, code)
		}
	}

	return merged
}
