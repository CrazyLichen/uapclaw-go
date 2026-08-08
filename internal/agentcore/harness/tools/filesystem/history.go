package filesystem

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OpHistoryEntry 文件操作历史条目。
// 对齐 Python: _append_op_history / _detect_and_record_deletions 中每条记录的字段。
// 此类型同时被 swarm/server/utils/diff_service.go 复用（readAgentHistory 解析后转为 OpHistoryEntry）。
type OpHistoryEntry struct {
	// Action 操作类型："write"、"edit"、"delete"
	Action string `json:"action"`
	// Timestamp 操作时间戳（ISO 8601）
	Timestamp string `json:"timestamp"`
	// OldContent 操作前的文件内容（nil 表示文件不存在或为空）
	OldContent *string `json:"old_content"`
	// NewContent 操作后的文件内容（nil 表示文件被删除）
	NewContent *string `json:"new_content"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// MaxHistoryPerFile 每个文件最大历史条目数。
	// 对齐 Python: MAX_HISTORY_PER_FILE = 100
	MaxHistoryPerFile = 100
)

// ──────────────────────────── 全局变量 ────────────────────────────

// historyMu 文件操作历史写入互斥锁。
// 对齐 Python: _HISTORY_LOCK = asyncio.Lock()
var historyMu sync.Mutex

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildHistoryPath 构建 .agent_history/file_ops_<agentID>_<sessionID>.json 文件路径。
//
// 对齐 Python: _build_history_path(session) — 合并 4 处 Python 重复实现：
//   - filesystem.py WriteFileTool._build_history_path (line 846)
//   - filesystem.py EditFileTool._build_history_path (line 1016)
//   - shell/bash/_tool.py BashTool._build_history_path (line 146)
//   - shell/powershell/_tool.py PowerShellTool._build_history_path (line 122)
//
// Python 中的实现为 session 对象方法，Go 统一为纯函数参数。
func BuildHistoryPath(baseDir, agentID, sessionID string) string {
	if baseDir == "" {
		baseDir = "."
	}
	if agentID == "" {
		agentID = "default"
	}
	return filepath.Join(baseDir, ".agent_history",
		fmt.Sprintf("file_ops_%s_%s.json", agentID, sessionID))
}

// AppendOpHistory 将一次写/编辑/删除操作追加到历史 JSON 文件。
//
// 对齐 Python: _append_op_history(history_path, file_path, action, old_content, new_content)
// (filesystem.py line 73-103)
//
// 并发安全：通过 historyMu 互斥锁保护文件读写（Python 用 asyncio.Lock）。
// JSON 文件是唯一数据源，没有内存缓存。
func AppendOpHistory(historyPath, filePath, action string, oldContent, newContent *string) {
	entry := OpHistoryEntry{
		Action:     action,
		Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
		OldContent: oldContent,
		NewContent: newContent,
	}

	historyMu.Lock()
	defer historyMu.Unlock()

	history := make(map[string][]OpHistoryEntry)
	if data, err := os.ReadFile(historyPath); err == nil {
		_ = json.Unmarshal(data, &history)
	}

	entries := history[filePath]
	entries = append(entries, entry)

	// 截断超过上限的条目
	if len(entries) > MaxHistoryPerFile {
		entries = entries[len(entries)-MaxHistoryPerFile:]
	}
	history[filePath] = entries

	// 写入临时文件后原子替换
	if err := os.MkdirAll(filepath.Dir(historyPath), 0o755); err != nil {
		logger.Warn(logComponent).
			Str("history_path", historyPath).
			Err(err).
			Msg("创建历史目录失败")
		return
	}

	tmpPath := historyPath + ".tmp"
	data, err := json.Marshal(history)
	if err != nil {
		logger.Warn(logComponent).
			Str("history_path", historyPath).
			Err(err).
			Msg("序列化历史数据失败")
		return
	}
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		logger.Warn(logComponent).
			Str("history_path", historyPath).
			Err(err).
			Msg("写入临时历史文件失败")
		return
	}
	if err := os.Rename(tmpPath, historyPath); err != nil {
		logger.Warn(logComponent).
			Str("history_path", historyPath).
			Err(err).
			Msg("替换历史文件失败")
	}
}

// DetectAndRecordDeletions 在 bash 执行后，扫描历史文件检测并记录已删除的文件。
//
// 对齐 Python: _detect_and_record_deletions(history_path)
// (filesystem.py line 205-239)
//
// 对于每个文件路径的最后一条记录，如果最后操作不是 delete
// 且文件不再存在，则追加一条 delete 记录。
func DetectAndRecordDeletions(historyPath string) {
	historyMu.Lock()
	defer historyMu.Unlock()

	data, err := os.ReadFile(historyPath)
	if err != nil {
		return // 文件不存在，无需处理
	}

	history := make(map[string][]OpHistoryEntry)
	if err := json.Unmarshal(data, &history); err != nil {
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	deletionsAdded := false

	for filePath, entries := range history {
		if len(entries) == 0 {
			continue
		}

		// 取最后一条记录
		lastEntry := entries[len(entries)-1]

		// 如果最后操作已经是 delete，跳过
		if lastEntry.Action == "delete" {
			continue
		}

		// 检查文件是否还存在
		if _, err := os.Stat(filePath); err != nil && os.IsNotExist(err) {
			entries = append(entries, OpHistoryEntry{
				Action:     "delete",
				Timestamp:  now,
				OldContent: lastEntry.NewContent,
				NewContent: nil,
			})
			if len(entries) > MaxHistoryPerFile {
				entries = entries[len(entries)-MaxHistoryPerFile:]
			}
			history[filePath] = entries
			deletionsAdded = true
		}
	}

	if !deletionsAdded {
		return
	}

	// 写入更新后的历史文件
	if err := os.MkdirAll(filepath.Dir(historyPath), 0o755); err != nil {
		logger.Warn(logComponent).
			Str("history_path", historyPath).
			Err(err).
			Msg("创建历史目录失败")
		return
	}

	tmpPath := historyPath + ".tmp"
	outData, err := json.Marshal(history)
	if err != nil {
		logger.Warn(logComponent).
			Str("history_path", historyPath).
			Err(err).
			Msg("序列化历史数据失败")
		return
	}
	if err := os.WriteFile(tmpPath, outData, 0o644); err != nil {
		logger.Warn(logComponent).
			Str("history_path", historyPath).
			Err(err).
			Msg("写入临时历史文件失败")
		return
	}
	if err := os.Rename(tmpPath, historyPath); err != nil {
		logger.Warn(logComponent).
			Str("history_path", historyPath).
			Err(err).
			Msg("替换历史文件失败")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// appendHistoryFromOpts 从 ToolOption 列表中提取 session 信息，
// 构建 history path 并追加操作记录。
// 对齐 Python: _session = get_current_session(); if _session: ...
func appendHistoryFromOpts(opts []tool.ToolOption, agentID, filePath, action string, oldContent, newContent *string) error {
	// 解析 opts 获取 ToolCallOptions
	callOpts := &tool.ToolCallOptions{}
	for _, opt := range opts {
		opt(callOpts)
	}

	// 检查是否有 session
	if callOpts.Session == nil {
		return nil // 无 session，不记录历史（对齐 Python: if _session is None: skip）
	}

	sessionFacade := callOpts.Session
	if sessionFacade == nil {
		return nil
	}

	sessionID := sessionFacade.GetSessionID()
	if sessionID == "" {
		return nil
	}

	// 构建 history path
	// 对齐 Python: base_dir = get_workspace() or cwd
	baseDir := workspace.WorkspaceDir()
	historyPath := BuildHistoryPath(baseDir, agentID, sessionID)

	AppendOpHistory(historyPath, filePath, action, oldContent, newContent)
	return nil
}
