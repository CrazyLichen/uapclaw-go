package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// historyWriteItem 写入队列项。
type historyWriteItem struct {
	// sessionID 会话标识
	sessionID string
	// record 待写入记录
	record map[string]any
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// historyQueueSize 写入队列容量，对齐 Python _WRITE_QUEUE maxsize=20000
	historyQueueSize = 20000
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// historyWriteQueue 异步写入队列。
	historyWriteQueue chan historyWriteItem
	// historyFileMu 文件锁（read-modify-write 期间持锁）。
	historyFileMu sync.Mutex
	// historyWorkerOnce 保证 worker 只启动一次。
	historyWorkerOnce sync.Once
)

// ──────────────────────────── 导出函数 ────────────────────────────

// AppendHistoryRecord 向指定 session 的 history.json 异步追加一条记录。
//
// 对齐 Python: append_history_record(session_id, request_id, channel_id, role, content, timestamp, event_type, extra, channel_metadata, mode)
func AppendHistoryRecord(sessionID, requestID, channelID, role, content string,
	timestamp float64, eventType string, extra map[string]any,
	channelMetadata map[string]any, mode string) {
	// 规范化
	sid := normalizeSessionID(sessionID)
	rid := requestID
	cid := channelID
	roleNorm := "assistant"
	if role != "assistant" {
		roleNorm = "user"
	}
	contentText := content

	// 构建记录项
	item := map[string]any{
		"id":         rid + ":" + roleNorm,
		"role":       roleNorm,
		"request_id": rid,
		"channel_id": cid,
		"timestamp":  timestamp,
		"content":    contentText,
	}

	// event_type：仅在 assistant 且非空时写入
	if roleNorm == "assistant" && eventType != "" {
		item["event_type"] = eventType
	}

	// extra 字段展开到顶层
	if len(extra) > 0 {
		for k, v := range extra {
			item[k] = v
		}
	}

	// mode：非空时写入
	if mode != "" {
		item["mode"] = mode
	}

	// 确保 worker 已启动
	ensureHistoryWorker()

	// 尝试入队
	select {
	case historyWriteQueue <- historyWriteItem{sessionID: sid, record: item}:
	default:
		// 队列满时退化为同步写，避免丢失记录
		writeHistoryItem(sid, item)
	}
}

// ReadHistoryRecords 读取指定 session 的全部 history 记录。
//
// 对齐 Python: read_history_records(session_id)
func ReadHistoryRecords(sessionID string) ([]map[string]any, error) {
	sid := normalizeSessionID(sessionID)
	fpath := historyFilePath(sid)

	historyFileMu.Lock()
	defer historyFileMu.Unlock()

	return readHistoryFile(fpath)
}

// TruncateHistoryRecords 截断 history 到指定 request_id（rewind 使用）。
//
// 对齐 Python: truncate_history_records(session_id, request_id)
func TruncateHistoryRecords(sessionID string, requestID string) error {
	sid := normalizeSessionID(sessionID)
	fpath := historyFilePath(sid)

	historyFileMu.Lock()
	defer historyFileMu.Unlock()

	records, err := readHistoryFile(fpath)
	if err != nil {
		return err
	}

	// 找到 request_id 对应的最后一个索引，保留到该位置
	truncateIdx := -1
	for i, r := range records {
		if rid, ok := r["request_id"].(string); ok && rid == requestID {
			truncateIdx = i
		}
	}

	if truncateIdx < 0 {
		// 未找到，不截断
		return nil
	}

	truncated := records[:truncateIdx+1]
	return writeHistoryFile(fpath, truncated)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ensureHistoryWorker 启动异步写入 worker（单 goroutine，sync.Once 保证）。
func ensureHistoryWorker() {
	historyWorkerOnce.Do(func() {
		historyWriteQueue = make(chan historyWriteItem, historyQueueSize)
		go historyWorker()
	})
}

// historyWorker 写入队列消费者。
func historyWorker() {
	for item := range historyWriteQueue {
		writeHistoryItem(item.sessionID, item.record)
	}
}

// writeHistoryItem 同步写入单条记录（持文件锁）。
func writeHistoryItem(sessionID string, record map[string]any) {
	fpath := historyFilePath(sessionID)

	historyFileMu.Lock()
	defer historyFileMu.Unlock()

	records, _ := readHistoryFile(fpath)
	records = append(records, record)

	if err := writeHistoryFile(fpath, records); err != nil {
		logger.Error(logComponent).Err(err).Str("session_id", sessionID).Msg("history 写入失败")
	}
}

// historyFilePath 返回 history.json 的完整路径。
func historyFilePath(sessionID string) string {
	dir := filepath.Join(workspace.AgentSessionsDir(), sessionID)
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, "history.json")
}

// readHistoryFile 读取 history.json 全量记录。
func readHistoryFile(fpath string) ([]map[string]any, error) {
	data, err := os.ReadFile(fpath)
	if err != nil {
		if os.IsNotExist(err) {
			return []map[string]any{}, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return []map[string]any{}, nil
	}
	var records []map[string]any
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, err
	}
	return records, nil
}

// writeHistoryFile 写入 history.json 全量记录。
func writeHistoryFile(fpath string, records []map[string]any) error {
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(fpath, data, 0o644)
}

// normalizeSessionID 规范化 sessionID，空串→"default"。
func normalizeSessionID(sessionID string) string {
	sid := sessionID
	if sid == "" {
		return "default"
	}
	return sid
}
