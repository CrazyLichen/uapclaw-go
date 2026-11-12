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
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
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
	// writeDone 单条写入回执：worker 写完即 close（nil 表示不需要回执）
	writeDone chan struct{}
}

// TruncateResult 截断结果，对齐 Python truncate_history_records 返回 dict
type TruncateResult struct {
	// RemainingRecords 保留记录数
	RemainingRecords int
	// RemovedRecords 删除记录数
	RemovedRecords int
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// historyQueueSize 写入队列容量，对齐 Python _WRITE_QUEUE maxsize=20000
	historyQueueSize = 20000
	// teamHistoryReadMaxRetries 读取重试次数，对齐 Python read_team_history_records 最多 5 次
	teamHistoryReadMaxRetries = 5
	// flushDrainTimeoutSeconds 重置 worker 后等待残留写入收尾的上限（秒）
	flushDrainTimeoutSeconds = 1.0
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// historyWriteQueue 异步写入队列。
	historyWriteQueue chan historyWriteItem
	// historyFileMu 文件锁（read-modify-write 期间持锁）。
	historyFileMu sync.Mutex
	// historyWorkerOnce 保证 worker 只启动一次。
	historyWorkerOnce sync.Once
	// historyWorkerMu 保护队列指针与 Once 的读写；
	// worker goroutine 也只读队列参数，重置不会与它竞争
	historyWorkerMu sync.RWMutex
	// teamRelevantEventTypes team 相关事件类型集合
	// Python: _TEAM_RELEVANT_EVENT_TYPES
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

// FlushHistoryQueue 等待 history 异步写入队列刷盘。
// timeoutSeconds 为等待上限；返回是否在超时前全部写完。
// Python: session_history.py:1384 flush_pending_writes(timeout: float = 10) -> bool
func FlushHistoryQueue(timeoutSeconds float64) bool {
	// 如果 worker 还没启动，无需等待
	queue := historyWorkerQueue()
	if queue == nil {
		return true
	}
	// 发送哨兵项，worker 处理后会向 flushDone 发送确认。
	// 容量 1：worker 用非阻塞投递确认，无缓冲时若确认先于等待方进入 select 就会丢，
	// 表现为偶发"队列空却刷盘超时"（-race 下放大）
	flushDone := make(chan struct{}, 1)
	queue <- historyWriteItem{flushDone: flushDone}
	// 等待 worker 确认，确保所有之前的写入已完成
	timer := time.NewTimer(time.Duration(timeoutSeconds * float64(time.Second)))
	defer timer.Stop()
	select {
	case <-flushDone:
		return true
	case <-timer.C:
		logger.Warn(logComponent).Float64("timeout_seconds", timeoutSeconds).
			Msg("FlushHistoryQueue timed out")
		return false
	}
}

// ResetHistoryWorker 关闭并重置 history worker（供测试使用）。
//
// 重置前先把已入队的记录写完（不足 1 秒），避免它们随旧队列一起被丢弃：
// 用例之间共享包级 worker，上一个用例的写入若被丢弃，下一个用例可能读到旧目录。
// 关闭队列后旧 worker 处理完剩余记录自行退出，重置后下一次写入会重启 worker。
func ResetHistoryWorker() {
	historyWorkerMu.Lock()
	queue := historyWriteQueue
	// 先清指针，防止重置期间又有新记录入队后又随旧队列一起被关闭
	historyWriteQueue = nil
	historyWorkerOnce = sync.Once{}
	historyWorkerMu.Unlock()
	if queue == nil {
		return
	}

	// 排空已入队记录：哨兵排在队尾，收到确认就说明前面的都写完了
	drained := make(chan struct{}, 1)
	select {
	case queue <- historyWriteItem{flushDone: drained}:
		timer := time.NewTimer(resetDrainTimeout)
		select {
		case <-drained:
		case <-timer.C:
		}
		timer.Stop()
	default:
		// 队列已满（极端情况）：直接关，不排空
	}

	// 关闭队列，worker 处理完剩余记录后退出 range 循环
	close(queue)
}

// ResetDrainedQueues 排空并重置包内两个异步写入 worker。
// 仅供测试使用：用例开始/结束时调用，保证下一个用例看到空的队列与新的 worker，
// 且不会有遗留写入落在已删除的临时目录上。
//
// 顺序不可颠倒：必须先 Flush 把已入队记录写完，再 Reset 关队列。
// 先 Reset 会把排队项连同旧队列一起丢弃，随后的 Flush 只是对新队列
// 立即返回，等于没排空。
func ResetDrainedQueues() {
	// 排空：哨兵排在已入队条目之后，收到确认即说明前面的都写完了
	FlushHistoryQueue(flushDrainTimeoutSeconds)
	FlushMetadataQueue(flushDrainTimeoutSeconds)
	ResetHistoryWorker()
	ResetMetadataWorker()
}

// sessionRequestTerminalStatuses 请求终态白名单。
// Python: session_history.py:40 SESSION_REQUEST_TERMINAL_STATUSES
var sessionRequestTerminalStatuses = map[string]bool{
	"success": true, "error": true, "cancelled": true,
	"failed": true, "waiting_user": true, "unknown": true,
}

// EnqueueHistoryRequestCompletion 在此前已入队的 history 之后追加一条请求终态完成行，
// 返回落盘回执（worker 写完即 close）。rid 为空或心跳前缀会话不入队（回执 nil）。
// Python: session_history.py:1592 enqueue_history_request_completion
func EnqueueHistoryRequestCompletion(sessionID, requestID, terminalStatus string) (<-chan struct{}, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		sid = "default"
	}
	rid := strings.TrimSpace(requestID)
	if rid == "" || strings.HasPrefix(sid, heartbeatSessionPrefix) {
		return nil, nil
	}
	status := strings.ToLower(strings.TrimSpace(terminalStatus))
	if !sessionRequestTerminalStatuses[status] {
		return nil, fmt.Errorf("unsupported request terminal status: %q", terminalStatus)
	}
	// 取队列快照：重置（测试用）可能把队列指针置空
	queue := historyWorkerQueue()
	receipt := make(chan struct{}, 1)
	queue <- historyWriteItem{
		sessionID: sid,
		record: map[string]any{
			"id":            rid + ":request_completed",
			"role":          "assistant",
			"request_id":    rid,
			"channel_id":    "",
			"timestamp":     float64(time.Now().UnixNano()) / 1e9,
			"content":       "",
			"event_type":    string(schema.EventTypeSessionRequestCompleted),
			"feedback_only": true,
			"status":        status,
		},
		writeDone: receipt,
	}
	return receipt, nil
}

// AppendHistoryRecord 向指定 session 的 history.json 异步追加一条记录。
// Python: append_history_record(session_id, request_id, channel_id, role, content, timestamp, event_type, extra, channel_metadata, mode)
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

	// 取队列快照（启动 worker 后取，重置期间为空不会 panic）
	queue := historyWorkerQueue()

	// 尝试入队
	select {
	case queue <- historyWriteItem{sessionID: sid, record: item}:
	default:
		// 队列满时退化为同步写，避免丢失记录
		writeHistoryItem(sid, item)
	}

	// Python: append_history_record 内部的元数据联动（第 176-200 行）
	// 同步调用，对齐 Python 行为。联动失败仅 log.Warn，不影响主流程
	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Warn(logComponent).Any("recover", r).Msg("session metadata linkage panic recovered")
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
// Python: append_compact_history_records(session_id, request_id, channel_id, summary, timestamp, trigger, stats, mode)
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
// Python: _append_compact_history_from_payload(payload, session_id, request_id, channel_id, mode)
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
// Python: read_history_records(session_id)
func ReadHistoryRecords(sessionID string) ([]map[string]any, error) {
	sid := NormalizeSessionID(sessionID)
	fpath := historyFilePath(sid)

	historyFileMu.Lock()
	defer historyFileMu.Unlock()

	return readHistoryFile(fpath)
}

// TruncateHistoryRecords 截断 history 到指定位置索引（rewind 使用）。
// Python: truncate_history_records(session_id, cut_index: int) → dict
// 先等异步队列刷盘（FlushHistoryQueue 哨兵机制等价 Python _WRITE_QUEUE.join()），再截断到 cutIndex。
func TruncateHistoryRecords(sessionID string, cutIndex int) (TruncateResult, error) {
	// Python: _WRITE_QUEUE.join()，先等异步队列刷盘再截断
	// 上游 join() 无超时上限；Go 保留 5 秒有界等待（既有行为，不因签名改造而变更）
	FlushHistoryQueue(5.0)

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
// Python: _is_team_relevant(item)
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
// Python: read_team_history_records(session_id)
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
						Str("session_id", sid).
						Msg("ReadTeamHistoryRecords retry succeeded")
					break
				}
			}
			if len(allRecords) == 0 {
				fileSize := int64(0)
				if fi, statErr2 := os.Stat(fpath); statErr2 == nil {
					fileSize = fi.Size()
				}
				logger.Warn(logComponent).
					Str("session_id", sid).
					Int64("file_size", fileSize).
					Msg("ReadTeamHistoryRecords retries exhausted, file may be empty")
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

// historyWorkerQueue 启动（如尚未启动）并返回当前写入队列。
func historyWorkerQueue() chan historyWriteItem {
	ensureHistoryWorker()
	historyWorkerMu.RLock()
	defer historyWorkerMu.RUnlock()
	return historyWriteQueue
}

// ensureHistoryWorker 启动异步写入 worker（单 goroutine，sync.Once 保证）。
func ensureHistoryWorker() {
	historyWorkerMu.Lock()
	defer historyWorkerMu.Unlock()
	historyWorkerOnce.Do(func() {
		historyWriteQueue = make(chan historyWriteItem, historyQueueSize)
		// 队列以参数传入，worker 不再读全局变量，重置后不会与新队列混淆
		go historyWorker(historyWriteQueue)
	})
}

// historyWorker 写入队列消费者。
func historyWorker(queue chan historyWriteItem) {
	for item := range queue {
		// 哨兵项：FlushHistoryQueue 发送的空项，跳过写入但确认完成
		if item.sessionID == "" && item.record == nil {
			if item.flushDone != nil {
				// flushDone 由 FlushHistoryQueue 创建：无缓冲，且等待方有超时。
				// 等待方超时后无人接收，直接发送会让整个 worker 永久阻塞，
				// 故用非阻塞投递（有等待方则收到，无等待方则丢弃确认）。
				select {
				case item.flushDone <- struct{}{}:
				default:
				}
			}
			continue
		}
		writeHistoryItem(item.sessionID, item.record)
		// 10.7.2: 单条落盘回执（fork 推送前等待完成行写盘）
		if item.writeDone != nil {
			close(item.writeDone)
		}
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
		logger.Error(logComponent).Err(err).Str("session_id", sessionID).Msg("history  write failed")
	}
}

// historyFilePath 返回 history.json 的完整路径。
// Python: _history_file(session_id) 使用 get_agent_sessions_dir()
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
		// Python: _read_history: 读取失败时 Warn 日志 + 返回空列表，不阻断主流程
		logger.Warn(logComponent).Err(err).Str("path", fpath).Msg("failed to read history.json, ignored and rebuilt")
		return []map[string]any{}, nil
	}
	return records, nil
}

// writeHistoryFile 写入 history.json 全量记录。
// 序列化对齐 Python json.dumps(history, ensure_ascii=False, indent=2)
// （session_history.py:103-105）：不转义 < > &（Go json.MarshalIndent 默认会把
// 它们写成 \u003c/\u003e/\u0026，导致 `</heartbeat_user_task>` 这类文案在文件里
// 变成转义串，人工排查/grep 都会踩坑）。统一走 utils.MarshalIndentNoEscape。
func writeHistoryFile(fpath string, records []map[string]any) error {
	data, err := utils.MarshalIndentNoEscape(records)
	if err != nil {
		return err
	}
	return os.WriteFile(fpath, data, 0o644)
}

// isSuccessfulCompactionPayload 判断 payload 是否表示成功的压缩。
// Python: _is_successful_compaction_payload(payload)
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
// Python: _compact_stats_from_payload(payload)
func compactStatsFromPayload(payload map[string]any) map[string]any {
	stats := make(map[string]any)
	for _, key := range []string{"status", "phase", "processor", "model", "before", "after", "saved", "duration_ms"} {
		if v, ok := payload[key]; ok {
			stats[key] = v
		}
	}
	return stats
}
