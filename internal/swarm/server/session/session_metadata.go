package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionMetadataUpdate 会话元数据增量更新参数。
// Python: jiuwenswarm/server/runtime/session/session_metadata.py update_session_metadata()
type SessionMetadataUpdate struct {
	// SessionID 会话标识（必填）
	SessionID string
	// ChannelID 渠道标识
	ChannelID *string
	// UserID 用户标识
	UserID *string
	// Title 标题（nil=不修改，非空=设置，空串=忽略防御意外覆盖）
	Title *string
	// ClearTitle 显式清除标题
	ClearTitle bool
	// IncrementMessageCount 递增消息计数
	IncrementMessageCount bool
	// SetMessageCount 直接设置消息计数
	SetMessageCount *int
	// UserContent 用户消息内容，用于自动生成标题
	UserContent *string
	// ChannelMetadata 渠道元数据（首次补充写入，不覆盖）
	ChannelMetadata map[string]any
	// Mode 模式
	Mode *string
	// TeamName 团队名称
	TeamName *string
}

// metadataWriteItem 异步写入队列条目
type metadataWriteItem struct {
	// sessionID 会话标识
	sessionID string
	// metadata 元数据映射
	metadata map[string]any
	// flushDone 刷盘确认通道（非 nil 时，worker 处理完此条目后发送信号）
	flushDone chan struct{}
	// processed worker 处理完此条目（无论是否写盘）后关闭，供重置流程等待旧 worker 退出
	processed chan struct{}
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// metadataFileName 元数据文件名
	metadataFileName = "metadata.json"
	// deliveryContextKind 推送类型，对齐 Python _DELIVERY_KIND_SERVER_PUSH
	deliveryContextKind = "server_push"
	// metadataQueueSize 异步写入队列大小，对齐 Python _METADATA_QUEUE maxsize=5000
	metadataQueueSize = 5000
	// resetDrainTimeout 重置 worker 前等待已入队条目处理完的上限
	resetDrainTimeout = time.Second
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// deliveryContextCache 内存缓存，解决异步写入时读取到陈旧数据的竞态
	// Python: _METADATA_CACHE
	deliveryContextCache = make(map[string]map[string]any)
	// deliveryContextMu 保护缓存的读写锁
	// Python: _CACHE_LOCK
	deliveryContextMu sync.RWMutex
	// metadataQueue 异步写入队列，对齐 Python _METADATA_QUEUE
	metadataQueue chan *metadataWriteItem
	// metadataQueueOnce 确保 worker 只启动一次
	metadataQueueOnce sync.Once
	// metadataWorkerMu 保护队列指针与 Once 的读写；
	// worker goroutine 也持锁读取队列指针，重置（ResetMetadataWorker）不会与它竞争
	metadataWorkerMu sync.RWMutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SetSessionDeliveryContext 刷新 session 级 delivery context，
// 供异步 server_push 恢复路由上下文。
// Python: set_session_delivery_context()
func SetSessionDeliveryContext(
	sessionID string,
	channelID *string,
	sourceRequestID *string,
	routeMetadata map[string]any,
	deliveryKind ...string,
) map[string]any {
	meta := ReadSessionMetadataWithCache(sessionID)
	currentContextRaw := meta["delivery_context"]
	currentContext := map[string]any{}
	if raw, ok := currentContextRaw.(map[string]any); ok {
		currentContext = DeepCopyMap(raw)
	}

	// 归一化 channel_id
	normalizedChannelID := ""
	if channelID != nil && strings.TrimSpace(*channelID) != "" {
		normalizedChannelID = strings.TrimSpace(*channelID)
	} else if cid, ok := currentContext["channel_id"].(string); ok && strings.TrimSpace(cid) != "" {
		normalizedChannelID = strings.TrimSpace(cid)
	} else if cid, ok := meta["channel_id"].(string); ok && strings.TrimSpace(cid) != "" {
		normalizedChannelID = strings.TrimSpace(cid)
	}

	// 归一化 source_request_id
	normalizedRequestID := ""
	if sourceRequestID != nil && strings.TrimSpace(*sourceRequestID) != "" {
		normalizedRequestID = strings.TrimSpace(*sourceRequestID)
	} else if rid, ok := currentContext["source_request_id"].(string); ok {
		normalizedRequestID = strings.TrimSpace(rid)
	}

	// 归一化 route_metadata
	previousRouteMetadata, _ := currentContext["route_metadata"].(map[string]any)
	var normalizedRouteMetadata map[string]any
	if len(routeMetadata) > 0 {
		normalizedRouteMetadata = DeepCopyMap(routeMetadata)
	} else if previousRouteMetadata != nil {
		normalizedRouteMetadata = DeepCopyMap(previousRouteMetadata)
	}

	// Python: channel_metadata 仅在首次为空时补充写入（不覆盖）
	channelMetadata, _ := routeMetadata["channel_metadata"].(map[string]any)
	if channelMetadata != nil && meta["channel_metadata"] == nil {
		meta["channel_metadata"] = DeepCopyMap(channelMetadata)
	}

	if len(meta) == 0 {
		meta = map[string]any{
			"session_id":      sessionID,
			"channel_id":      normalizedChannelID,
			"user_id":         "",
			"created_at":      CurrentTimestamp(),
			"last_message_at": CurrentTimestamp(),
			"title":           "",
			"message_count":   0,
			"mode":            "unknown",
			"team_name":       "",
			"round_id":        0,
		}
	} else {
		if normalizedChannelID != "" {
			meta["channel_id"] = normalizedChannelID
		}
		meta["last_message_at"] = CurrentTimestamp()
	}

	// 确定 delivery_kind
	kind := deliveryContextKind
	if len(deliveryKind) > 0 && strings.TrimSpace(deliveryKind[0]) != "" {
		kind = strings.TrimSpace(deliveryKind[0])
	}

	deliveryContext := map[string]any{
		"delivery_kind":     kind,
		"session_id":        sessionID,
		"channel_id":        normalizedChannelID,
		"source_request_id": normalizedRequestID,
		"updated_at":        CurrentTimestamp(),
	}
	if normalizedRouteMetadata != nil {
		deliveryContext["route_metadata"] = normalizedRouteMetadata
	}

	meta["delivery_context"] = deliveryContext

	// 更新缓存并写入文件
	deliveryContextMu.Lock()
	deliveryContextCache[sessionID] = DeepCopyMap(meta)
	deliveryContextMu.Unlock()

	// Python: _enqueue_write(metadata) — 异步写入队列
	EnqueueMetadataWrite(sessionID, meta)

	return DeepCopyMap(deliveryContext)
}

// GetSessionDeliveryContext 读取 session 级 delivery context。
// Python: get_session_delivery_context()
func GetSessionDeliveryContext(sessionID string) map[string]any {
	// 优先从内存缓存读取
	deliveryContextMu.RLock()
	if cached, ok := deliveryContextCache[sessionID]; ok {
		deliveryContextMu.RUnlock()
		if dc, ok := cached["delivery_context"].(map[string]any); ok {
			return DeepCopyMap(dc)
		}
		return nil
	}
	deliveryContextMu.RUnlock()

	// 从 metadata.json 读取
	meta := ReadSessionMetadata(GetSessionsDir(), sessionID)
	context, ok := meta["delivery_context"]
	if !ok {
		return nil
	}
	dc, ok := context.(map[string]any)
	if !ok {
		return nil
	}
	return DeepCopyMap(dc)
}

// BuildServerPushMessage 基于 session delivery context 构造 server_push 消息。
// Python: build_server_push_message()
// 被 evolution_helpers 和其他推送场景调用。
func BuildServerPushMessage(
	sessionID, requestID string,
	payload map[string]any,
	fallbackChannelID ...string,
) map[string]any {
	deliveryCtx := GetSessionDeliveryContext(sessionID)
	channelID := "default"
	if deliveryCtx != nil {
		if cid, ok := deliveryCtx["channel_id"].(string); ok && strings.TrimSpace(cid) != "" {
			channelID = strings.TrimSpace(cid)
		}
	}
	if len(fallbackChannelID) > 0 && fallbackChannelID[0] != "" && (channelID == "default" || channelID == "") {
		channelID = fallbackChannelID[0]
	}

	message := map[string]any{
		"request_id": requestID,
		"channel_id": channelID,
		"session_id": sessionID,
		"payload":    payload,
	}
	if deliveryCtx != nil {
		if rm, ok := deliveryCtx["route_metadata"].(map[string]any); ok && len(rm) > 0 {
			message["metadata"] = DeepCopyMap(rm)
		}
	}
	return message
}

// GetSessionsDir 返回全局会话目录路径。
func GetSessionsDir() string {
	return workspace.AgentSessionsDir()
}

// InitSessionMetadata 初始化会话元数据（同步写，确保创建后立即可读）。
// Python: init_session_metadata(session_id, channel_id, user_id, title, mode, team_name)
func InitSessionMetadata(sessionID, channelID, userID, title, mode, teamName string) {
	metadata := map[string]any{
		"session_id":      sessionID,
		"channel_id":      channelID,
		"user_id":         userID,
		"created_at":      CurrentTimestamp(),
		"last_message_at": CurrentTimestamp(),
		"title":           title,
		"message_count":   0,
		"mode":            mode,
		"team_name":       teamName,
		"round_id":        0,
	}
	_ = WriteSessionMetadata(GetSessionsDir(), sessionID, metadata)
}

// GetSessionMetadata 获取会话元数据（优先缓存）。
// Python: get_session_metadata(session_id)
func GetSessionMetadata(sessionID string) map[string]any {
	return ReadSessionMetadataWithCache(sessionID)
}

// UpdateSessionMetadata 更新会话元数据（异步写入，不阻塞调用方）。
// Python: update_session_metadata()
// title 语义（保持历史防御契约）：
//   - title=nil  → 不修改（默认）
//   - title="x"  → 设置为 "x"
//   - title=""   → 忽略（防御意外空值覆盖已有标题）
//   - 若需显式清除标题，请设置 ClearTitle=true
func UpdateSessionMetadata(update SessionMetadataUpdate) {
	metadata := ReadSessionMetadataWithCache(update.SessionID)

	if metadata == nil {
		// 元数据不存在，创建新的（外部渠道隐式创建 session 的兜底）
		// 自动生成标题：当 title 为空且提供了用户消息内容时，对齐 Python _auto_title
		autoTitle := ""
		userContentStr := DerefStr(update.UserContent, "")
		if update.Title == nil && userContentStr != "" {
			autoTitle = AutoTitle(userContentStr)
		}
		metadata = map[string]any{
			"session_id":      update.SessionID,
			"channel_id":      DerefStr(update.ChannelID, ""),
			"user_id":         DerefStr(update.UserID, ""),
			"created_at":      CurrentTimestamp(),
			"last_message_at": CurrentTimestamp(),
			"title":           DerefStr(update.Title, autoTitle),
			"message_count":   0,
			"mode":            DerefStr(update.Mode, "unknown"),
			"team_name":       DerefStr(update.TeamName, ""),
			"round_id":        0,
		}
		if update.IncrementMessageCount {
			metadata["message_count"] = 1
		}
		if update.ChannelMetadata != nil {
			metadata["channel_metadata"] = update.ChannelMetadata
		}
	} else {
		// 更新现有元数据
		if update.ChannelID != nil {
			metadata["channel_id"] = *update.ChannelID
		}
		if update.UserID != nil {
			metadata["user_id"] = *update.UserID
		}
		if update.Mode != nil {
			metadata["mode"] = *update.Mode
		}
		if update.TeamName != nil {
			metadata["team_name"] = *update.TeamName
		}
		// 显式清除优先级高于 title 入参
		if update.ClearTitle {
			metadata["title"] = ""
		} else if update.Title != nil && *update.Title != "" {
			metadata["title"] = *update.Title
		}
		if update.IncrementMessageCount {
			count := 0
			if v, ok := metadata["message_count"]; ok {
				switch n := v.(type) {
				case int:
					count = n
				case float64:
					count = int(n)
				}
			}
			metadata["message_count"] = count + 1
		}
		if update.SetMessageCount != nil {
			metadata["message_count"] = *update.SetMessageCount
		}

		// 自动生成标题：当 title 为空且提供了用户消息内容时，对齐 Python _auto_title
		currentTitle, _ := metadata["title"].(string)
		if currentTitle == "" && update.UserContent != nil && *update.UserContent != "" {
			metadata["title"] = AutoTitle(*update.UserContent)
		}

		// channel_metadata 仅在首次为空时补充写入（不覆盖）
		if update.ChannelMetadata != nil {
			if _, ok := metadata["channel_metadata"]; !ok {
				metadata["channel_metadata"] = update.ChannelMetadata
			}
		}

		// 总是更新最后消息时间
		metadata["last_message_at"] = CurrentTimestamp()
	}

	EnqueueMetadataWrite(update.SessionID, metadata)
}

// IncrementSessionRoundCount 递增并持久化 session 的 round_id，返回递增后的值。
// Python: increment_session_round_count()
func IncrementSessionRoundCount(sessionID string) (int, error) {
	return IncrementSessionRoundCountWithDir(GetSessionsDir(), sessionID)
}

// RemoveSessionMetadataCache 清除指定会话的内存缓存。
// Python: remove_session_metadata_cache(session_id)
func RemoveSessionMetadataCache(sessionID string) {
	deliveryContextMu.Lock()
	delete(deliveryContextCache, sessionID)
	deliveryContextMu.Unlock()
}

// ClearAllSessionMetadataCache 清除所有会话的内存缓存（供测试使用）。
func ClearAllSessionMetadataCache() {
	deliveryContextMu.Lock()
	deliveryContextCache = make(map[string]map[string]any)
	deliveryContextMu.Unlock()
}

// ResetMetadataWorker 关闭并重置 metadata worker（供测试使用）。
//
// 重置前先把已入队的条目处理完（不足 1 秒），避免它们被丢掉——用例之间
// 共享包级 worker 时，上一个用例的写入若被丢弃，下一个用例可能读到旧目录。
// 随后关闭队列并等旧 worker 真正退出，保证同一时刻只有一个 worker 消费。
// 重置后下一次 metadata 写入会重新启动 worker。
func ResetMetadataWorker() {
	metadataWorkerMu.Lock()
	queue := metadataQueue
	// 先清指针，防止重置期间又有新条目入队后又随旧队列一起被关闭
	metadataQueue = nil
	metadataQueueOnce = sync.Once{}
	metadataWorkerMu.Unlock()
	if queue == nil {
		return
	}

	// 排空已入队条目：哨兵排在队尾，处理到它就说明前面的都写完了
	drained := make(chan struct{}, 1)
	select {
	case queue <- &metadataWriteItem{flushDone: drained, processed: make(chan struct{})}:
		timer := time.NewTimer(resetDrainTimeout)
		select {
		case <-drained:
		case <-timer.C:
		}
		timer.Stop()
	default:
		// 队列已满（极端情况）：直接关，不排空
	}

	// 关闭队列，worker 处理完剩余条目后退出 range 循环
	close(queue)
}

// FlushMetadataQueue 等待 metadata 异步写入队列刷盘。
// timeoutSeconds 为等待上限；返回是否在超时前全部写完。
// Python: session_metadata.py:945 flush_pending_writes(timeout: float = 10) -> bool
func FlushMetadataQueue(timeoutSeconds float64) bool {
	// 取队列快照：重置（测试用）可能把队列指针置空，不能直接用全局变量发送
	queue := metadataWorkerQueue()
	// 发送哨兵项，worker 处理后会向 flushDone 发送确认。
	// 容量 1：worker 用非阻塞投递确认，无缓冲时若确认先于等待方进入 select 就会丢
	flushDone := make(chan struct{}, 1)
	queue <- &metadataWriteItem{flushDone: flushDone, processed: make(chan struct{})}
	// 等待 worker 确认，确保所有之前的写入已完成
	timer := time.NewTimer(time.Duration(timeoutSeconds * float64(time.Second)))
	defer timer.Stop()
	select {
	case <-flushDone:
		return true
	case <-timer.C:
		logger.Warn(logComponent).Float64("timeout_seconds", timeoutSeconds).
			Msg("FlushMetadataQueue timed out")
		return false
	}
}

// ReadSessionMetadata 读取会话元数据文件。
// 不产生副作用：session 目录不存在时返回 nil 而非创建目录，
// Python: _read_metadata 的"读路径不应产生副作用"原则。
func ReadSessionMetadata(sessionsDir, sessionID string) map[string]any {
	metaPath := filepath.Join(sessionsDir, sessionID, metadataFileName)
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil
	}
	var meta map[string]any
	if err := json.Unmarshal(data, &meta); err != nil {
		logger.Warn(logComponent).
			Err(err).
			Str("session_id", sessionID).
			Msg("failed to read metadata.json")
		return nil
	}
	return meta
}

// UpdateSessionModelSelection 只更新 model_selection 字段（合并式，其余字段不动）。
// Python: session_metadata.py:801-810 update_session_model_selection —— 经共享的
// 生命周期/原子写边界 _write_metadata_sync（merge_fields={"model_selection"}），
// 写后清除该会话的元数据缓存。selection 为 nil 表示清除选择。
func UpdateSessionModelSelection(sessionID string, selection map[string]any) error {
	sessionsDir := GetSessionsDir()
	current := ReadSessionMetadata(sessionsDir, sessionID)
	if current == nil {
		current = map[string]any{"session_id": sessionID}
	}
	current["model_selection"] = selection
	if err := WriteSessionMetadata(sessionsDir, sessionID, current); err != nil {
		return err
	}
	RemoveSessionMetadataCache(sessionID)
	return nil
}

// WriteSessionMetadata 写入会话元数据文件。
func WriteSessionMetadata(sessionsDir, sessionID string, meta map[string]any) error {
	sessionDir := filepath.Join(sessionsDir, sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return err
	}
	metaPath := filepath.Join(sessionDir, metadataFileName)
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, data, 0o644)
}

// ReadSessionMetadataWithCache 优先从内存缓存读取 metadata，否则从磁盘读取。
// Python: _read_metadata 的缓存优先策略。
func ReadSessionMetadataWithCache(sessionID string) map[string]any {
	deliveryContextMu.RLock()
	if cached, ok := deliveryContextCache[sessionID]; ok {
		deliveryContextMu.RUnlock()
		return DeepCopyMap(cached)
	}
	deliveryContextMu.RUnlock()

	return ReadSessionMetadata(GetSessionsDir(), sessionID)
}

// IncrementSessionRoundCountWithDir 递增并持久化 session 的 round_id，返回递增后的值（指定 sessionsDir）。
func IncrementSessionRoundCountWithDir(sessionsDir, sessionID string) (int, error) {
	meta := ReadSessionMetadataWithCache(sessionID)
	if meta == nil {
		meta = make(map[string]any)
	}
	currentRound := 0
	if v, ok := meta["round_id"]; ok {
		switch n := v.(type) {
		case int:
			currentRound = n
		case float64:
			currentRound = int(n)
		case json.Number:
			if i, err := n.Int64(); err == nil {
				currentRound = int(i)
			}
		}
	}
	newRound := currentRound + 1
	meta["round_id"] = newRound
	meta["last_message_at"] = CurrentTimestamp()

	// 更新缓存
	deliveryContextMu.Lock()
	deliveryContextCache[sessionID] = DeepCopyMap(meta)
	deliveryContextMu.Unlock()

	if err := WriteSessionMetadata(sessionsDir, sessionID, meta); err != nil {
		return 0, err
	}
	return newRound, nil
}

// EnqueueMetadataWrite 将写入操作放入异步队列，队列满时退化为同步写。
// Python: _enqueue_write()
func EnqueueMetadataWrite(sessionID string, metadata map[string]any) {
	// 先更新缓存，确保后续读取能看到最新状态
	deliveryContextMu.Lock()
	deliveryContextCache[sessionID] = DeepCopyMap(metadata)
	deliveryContextMu.Unlock()

	queue := metadataWorkerQueue()
	select {
	case queue <- &metadataWriteItem{sessionID: sessionID, metadata: metadata, processed: make(chan struct{})}:
	default:
		// 队列满，退化为同步写入
		logger.Warn(logComponent).
			Str("session_id", sessionID).
			Msg("metadata write queue full, degrading to synchronous write")
		_ = WriteSessionMetadata(GetSessionsDir(), sessionID, metadata)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// metadataWorkerQueue 启动（如尚未启动）并返回当前写入队列。
func metadataWorkerQueue() chan *metadataWriteItem {
	ensureMetadataWorker()
	metadataWorkerMu.RLock()
	defer metadataWorkerMu.RUnlock()
	return metadataQueue
}

// ensureMetadataWorker 确保 metadata 异步写入 worker 已启动。
// Python: _ensure_worker_started()，懒启动后台 goroutine
func ensureMetadataWorker() {
	metadataWorkerMu.Lock()
	defer metadataWorkerMu.Unlock()
	metadataQueueOnce.Do(func() {
		metadataQueue = make(chan *metadataWriteItem, metadataQueueSize)
		// 队列以参数传入，worker 不再读全局变量，重置后不会与新队列混淆
		go metadataWriteWorker(metadataQueue)
	})
}

// metadataWriteWorker 异步写入 worker，消费队列并写入磁盘。
// Python: _worker() 后台线程
func metadataWriteWorker(queue chan *metadataWriteItem) {
	for item := range queue {
		// 无论走哪条分支，处理完都要通知"本条目已处理"（重置流程据此等待 worker 收尾）
		func() {
			defer close(item.processed)
			metadataWorkerHandle(item)
		}()
	}
}

// metadataWorkerHandle 处理队列中的一条：哨兵只回执刷盘确认，普通条目写盘。
func metadataWorkerHandle(item *metadataWriteItem) {
	// 哨兵项：FlushMetadataQueue 发送的空项，跳过写入但消费了队列位置。
	// 判据只看 flushDone 字段：普通写入即使 metadata 为 nil（如会话元数据被清空）也必须落盘。
	if item.flushDone != nil {
		// flushDone 由 FlushMetadataQueue 创建：无缓冲，且等待方有超时。
		// 等待方超时后无人接收，直接发送会让整个 worker 永久阻塞、
		// 后续所有 metadata 写入与 flush 全部失效，故用非阻塞投递（语义等价：
		// 有等待方则收到，无等待方则丢弃确认）。
		select {
		case item.flushDone <- struct{}{}:
		default:
		}
		return
	}
	if err := WriteSessionMetadata(GetSessionsDir(), item.sessionID, item.metadata); err != nil {
		logger.Warn(logComponent).
			Str("session_id", item.sessionID).
			Err(err).
			Msg("metadata async write failed")
	}
}
