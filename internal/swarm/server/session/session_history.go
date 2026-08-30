package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// historyWriteItem 写入队列项。
type historyWriteItem struct {
	// sessionID 会话标识
	sessionID string
	// record 待写入记录
	record map[string]any
	// flushDone 哨兵项专用：FlushHistoryQueue 发送后 worker 确认的通道
	flushDone chan struct{}
}

// TruncateResult 截断结果，对齐 Python truncate_history_records 返回 dict
type TruncateResult struct {
	// RemainingRecords 保留记录数
	RemainingRecords int
	// RemovedRecords 删除记录数
	RemovedRecords int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// historyQueueSize 写入队列容量，对齐 Python _WRITE_QUEUE maxsize=20000
	historyQueueSize = 20000
	// teamHistoryReadMaxRetries 读取重试次数，对齐 Python read_team_history_records 最多 5 次
	teamHistoryReadMaxRetries = 5
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// historyWriteQueue 异步写入队列。
	historyWriteQueue chan historyWriteItem
	// historyFileMu 文件锁（read-modify-write 期间持锁）。
	historyFileMu sync.Mutex
	// historyWorkerOnce 保证 worker 只启动一次。
	historyWorkerOnce sync.Once
	// teamRelevantEventTypes team 相关事件类型集合
	// 对齐 Python _TEAM_RELEVANT_EVENT_TYPES
	teamRelevantEventTypes = map[string]bool{
		"team.message":      true,
		"chat.tool_call":    true,
		"chat.tracer_agent": true,
		"chat.final":        true,
		"chat.tool_result":  true,
		"chat.file":         true,
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// FlushHistoryQueue 等待 history 异步写入队列刷盘（供测试使用）。
//
// 发送哨兵项到队列，等待 worker 确认所有之前的写入已完成。
func FlushHistoryQueue() {
	// 如果 worker 还没启动，无需等待
	if historyWriteQueue == nil {
		return
	}
	// 发送哨兵项，worker 处理后会向 flushDone 发送确认
	flushDone := make(chan struct{})
	historyWriteQueue <- historyWriteItem{sessionID: "", record: nil, flushDone: flushDone}
	// 等待 worker 确认，确保所有之前的写入已完成
	select {
	case <-flushDone:
	case <-time.After(5 * time.Second):
		logger.Warn(logComponent).Msg("FlushHistoryQueue 超时")
	}
}

// ResetHistoryWorker 关闭并重置 history worker（供测试使用）。
//
// 调用前应先调用 FlushHistoryQueue 确保所有写入已完成。
// 重置后下一个 AppendHistoryRecord 调用会重新启动 worker。
func ResetHistoryWorker() {
	// 关闭队列，worker goroutine 会退出 range 循环
	if historyWriteQueue != nil {
		close(historyWriteQueue)
	}
	// 重置 Once，使下次 ensureHistoryWorker 重新创建队列和 worker
	historyWorkerOnce = sync.Once{}
	historyWriteQueue = nil
}

// AppendHistoryRecord 向指定 session 的 history.json 异步追加一条记录。
//
// 对齐 Python: append_history_record(session_id, request_id, channel_id, role, content, timestamp, event_type, extra, channel_metadata, mode)
// 入队成功后联动更新元数据（UpdateSessionMetadata + SetSessionDeliveryContext），联动失败仅 Warn。
func AppendHistoryRecord(sessionID, requestID, channelID, role, content string,
	timestamp float64, eventType string, extra map[string]any,
	channelMetadata map[string]any, mode string) {
	// 规范化
	sid := NormalizeSessionID(sessionID)
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

	// extra 字段使用 SerializeValue 递归序列化后展开到顶层
	serializedExtra := SerializeValue(extra)
	if m, ok := serializedExtra.(map[string]any); ok && len(m) > 0 {
		for k, v := range m {
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

	// 对齐 Python append_history_record 内部的元数据联动（第 176-200 行）
	// 联动失败仅 log.Warn，不影响主流程
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Warn(logComponent).Any("recover", r).Msg("会话元数据联动 panic 恢复")
			}
		}()
		userContent := contentText
		if roleNorm != "user" {
			userContent = ""
		}
		UpdateSessionMetadata(SessionMetadataUpdate{
			SessionID:             sid,
			ChannelID:             PtrStr(cid),
			IncrementMessageCount: true,
			UserContent:           PtrStr(userContent),
			ChannelMetadata:       channelMetadata,
			Mode:                  PtrStr(mode),
		})
		if roleNorm == "user" {
			SetSessionDeliveryContext(sid, PtrStr(cid), PtrStr(rid), channelMetadata)
		}
	}()
}

// AppendCompactHistoryRecords 写入 context compact 的 boundary + summary 记录。
//
// 对齐 Python: append_compact_history_records(session_id, request_id, channel_id, summary, timestamp, trigger, stats, mode)
func AppendCompactHistoryRecords(sessionID, requestID, channelID, summary string,
	timestamp float64, trigger string, stats map[string]any, mode string) {
	// 1. 写入 context.compact_boundary 记录
	metadata := map[string]any{
		"compact_metadata": map[string]any{
			"trigger": trigger,
		},
	}
	if stats != nil {
		cm := metadata["compact_metadata"].(map[string]any)
		for k, v := range stats {
			cm[k] = v
		}
	}
	AppendHistoryRecord(sessionID, requestID, channelID, "assistant",
		"Conversation compacted", timestamp, "context.compact_boundary",
		metadata, nil, mode)

	// 2. 如果 summary 非空，写入 context.compact_summary 记录
	cleanSummary := strings.TrimSpace(summary)
	if cleanSummary == "" {
		return
	}
	summaryMetadata := map[string]any{}
	for k, v := range metadata {
		summaryMetadata[k] = v
	}
	summaryMetadata["is_compact_summary"] = true
	summaryMetadata["transcript_only"] = true
	AppendHistoryRecord(sessionID, requestID, channelID, "assistant",
		cleanSummary, timestamp+0.001, "context.compact_summary",
		summaryMetadata, nil, mode)
}

// AppendCompactHistoryFromPayload 从 payload 中提取 compact 信息并写入 history。
//
// 对齐 Python: _append_compact_history_from_payload(payload, session_id, request_id, channel_id, mode)
func AppendCompactHistoryFromPayload(payload map[string]any, sessionID, requestID, channelID, mode string) {
	summaryText := ""
	if s, ok := payload["compact_summary"]; ok {
		summaryText = strings.TrimSpace(fmt.Sprint(s))
	}
	if summaryText == "" || !isSuccessfulCompactionPayload(payload) {
		return
	}
	AppendCompactHistoryRecords(sessionID, requestID, channelID,
		summaryText, CurrentTimestamp(), "auto",
		compactStatsFromPayload(payload), mode)
}

// ReadHistoryRecords 读取指定 session 的全部 history 记录。
//
// 对齐 Python: read_history_records(session_id)
func ReadHistoryRecords(sessionID string) ([]map[string]any, error) {
	sid := NormalizeSessionID(sessionID)
	fpath := historyFilePath(sid)

	historyFileMu.Lock()
	defer historyFileMu.Unlock()

	return readHistoryFile(fpath)
}

// TruncateHistoryRecords 截断 history 到指定位置索引（rewind 使用）。
//
// 对齐 Python: truncate_history_records(session_id, cut_index: int) → dict
// 先等异步队列刷盘（FlushHistoryQueue 哨兵机制等价 Python _WRITE_QUEUE.join()），再截断到 cutIndex。
func TruncateHistoryRecords(sessionID string, cutIndex int) (TruncateResult, error) {
	// 对齐 Python: _WRITE_QUEUE.join()，先等异步队列刷盘再截断
	FlushHistoryQueue()

	sid := NormalizeSessionID(sessionID)
	fpath := historyFilePath(sid)

	historyFileMu.Lock()
	defer historyFileMu.Unlock()

	records, err := readHistoryFile(fpath)
	if err != nil {
		if os.IsNotExist(err) {
			return TruncateResult{}, nil
		}
		return TruncateResult{}, err
	}

	total := len(records)
	if cutIndex < 0 {
		cutIndex = 0
	}
	if cutIndex > total {
		cutIndex = total
	}

	truncated := records[:cutIndex]
	if err := writeHistoryFile(fpath, truncated); err != nil {
		return TruncateResult{}, err
	}
	return TruncateResult{
		RemainingRecords: len(truncated),
		RemovedRecords:   total - len(truncated),
	}, nil
}

// IsTeamRelevant 判断记录是否为 team 相关事件。
//
// 对齐 Python _is_team_relevant(item)
func IsTeamRelevant(item map[string]any) bool {
	et, ok := item["event_type"].(string)
	if !ok || !teamRelevantEventTypes[et] {
		return false
	}
	switch et {
	case "chat.tool_call", "chat.tracer_agent":
		mode, _ := item["mode"].(string)
		return strings.TrimSpace(strings.ToLower(mode)) == "team"
	case "chat.final", "chat.tool_result", "chat.file":
		role, _ := item["role"].(string)
		return strings.TrimSpace(strings.ToLower(role)) == "teammate"
	case "team.message":
		return true
	default:
		return false
	}
}

// ReadTeamHistoryRecords 读取指定会话的 team 相关历史记录。
//
// 对齐 Python read_team_history_records(session_id)
// 带 5 次递增间隔重试（0.2s × attempt），防止读到截断窗口空文件。
func ReadTeamHistoryRecords(sessionID string) ([]map[string]any, error) {
	sid := NormalizeSessionID(sessionID)
	fpath := historyFilePath(sid)

	allRecords, err := ReadHistoryRecords(sid)
	if err != nil {
		return nil, err
	}

	// 重试：空结果且文件存在时触发
	if len(allRecords) == 0 {
		if _, statErr := os.Stat(fpath); statErr == nil {
			for attempt := 1; attempt <= teamHistoryReadMaxRetries; attempt++ {
				time.Sleep(time.Duration(200*attempt) * time.Millisecond)
				allRecords, err = ReadHistoryRecords(sid)
				if err != nil {
					return nil, err
				}
				if len(allRecords) > 0 {
					logger.Info(logComponent).
						Int("attempt", attempt).
						Msg("ReadTeamHistoryRecords 重试成功")
					break
				}
			}
			if len(allRecords) == 0 {
				logger.Warn(logComponent).Msg("ReadTeamHistoryRecords 重试耗尽，文件可能为空")
			}
		}
	}

	// 过滤 team 相关记录
	result := make([]map[string]any, 0)
	for _, item := range allRecords {
		if IsTeamRelevant(item) {
			result = append(result, item)
		}
	}
	return result, nil
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
		// 嗨兵项：FlushHistoryQueue 发送的空项，跳过写入但确认完成
		if item.sessionID == "" && item.record == nil {
			if item.flushDone != nil {
				item.flushDone <- struct{}{}
			}
			continue
		}
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
// 对齐 Python: _history_file(session_id) 使用 get_agent_sessions_dir()
func historyFilePath(sessionID string) string {
	dir := filepath.Join(GetSessionsDir(), sessionID)
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
		// 对齐 Python _read_history: 读取失败时 Warn 日志 + 返回空列表，不阻断主流程
		logger.Warn(logComponent).Err(err).Str("path", fpath).Msg("读取 history.json 失败，已忽略并重建")
		return []map[string]any{}, nil
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

// isSuccessfulCompactionPayload 判断 payload 是否表示成功的压缩。
//
// 对齐 Python: _is_successful_compaction_payload(payload)
func isSuccessfulCompactionPayload(payload map[string]any) bool {
	if v, ok := payload["error"]; ok && v != nil {
		return false
	}
	status := ""
	if s, ok := payload["status"]; ok {
		status = strings.TrimSpace(strings.ToLower(fmt.Sprint(s)))
	}
	return status != "error" && status != "failed" && status != "skipped"
}

// compactStatsFromPayload 从 payload 中提取压缩统计字段。
//
// 对齐 Python: _compact_stats_from_payload(payload)
func compactStatsFromPayload(payload map[string]any) map[string]any {
	stats := make(map[string]any)
	for _, key := range []string{"status", "phase", "processor", "model", "before", "after", "saved", "duration_ms"} {
		if v, ok := payload[key]; ok {
			stats[key] = v
		}
	}
	return stats
}
