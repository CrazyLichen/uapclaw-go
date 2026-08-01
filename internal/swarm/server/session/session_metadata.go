package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionMetadataUpdate 会话元数据增量更新参数。
//
// 对齐 Python: jiuwenswarm/server/runtime/session/session_metadata.py update_session_metadata()
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
	sessionID string
	metadata  map[string]any
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// metadataFileName 元数据文件名
	metadataFileName = "metadata.json"
	// deliveryContextKind 推送类型，对齐 Python _DELIVERY_KIND_SERVER_PUSH
	deliveryContextKind = "server_push"
	// metadataQueueSize 异步写入队列大小，对齐 Python _METADATA_QUEUE maxsize=5000
	metadataQueueSize = 5000
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// deliveryContextCache 内存缓存，解决异步写入时读取到陈旧数据的竞态
	// 对齐 Python: _METADATA_CACHE
	deliveryContextCache = make(map[string]map[string]any)
	// deliveryContextMu 保护缓存的读写锁
	// 对齐 Python: _CACHE_LOCK
	deliveryContextMu sync.RWMutex
	// metadataQueue 异步写入队列，对齐 Python _METADATA_QUEUE
	metadataQueue chan metadataWriteItem
	// metadataQueueOnce 确保 worker 只启动一次
	metadataQueueOnce sync.Once
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SetSessionDeliveryContext 刷新 session 级 delivery context，
// 供异步 server_push 恢复路由上下文。
//
// 对齐 Python: set_session_delivery_context()
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

	// 对齐 Python: channel_metadata 仅在首次为空时补充写入（不覆盖）
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

	sessionsDir := GetSessionsDir()
	if err := WriteSessionMetadata(sessionsDir, sessionID, meta); err != nil {
		logger.Warn(logComponent).Str("session_id", sessionID).Err(err).Msg("写入会话元数据失败")
	}

	return DeepCopyMap(deliveryContext)
}

// GetSessionDeliveryContext 读取 session 级 delivery context。
//
// 对齐 Python: get_session_delivery_context()
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
//
// 对齐 Python: build_server_push_message()
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
//
// 对齐 Python: init_session_metadata(session_id, channel_id, user_id, title, mode, team_name)
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
	WriteSessionMetadata(GetSessionsDir(), sessionID, metadata)
}

// GetSessionMetadata 获取会话元数据（优先缓存）。
//
// 对齐 Python: get_session_metadata(session_id)
func GetSessionMetadata(sessionID string) map[string]any {
	return ReadSessionMetadataWithCache(sessionID)
}

// UpdateSessionMetadata 更新会话元数据（异步写入，不阻塞调用方）。
//
// 对齐 Python: update_session_metadata()
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
//
// 对齐 Python: increment_session_round_count()
func IncrementSessionRoundCount(sessionID string) (int, error) {
	return IncrementSessionRoundCountWithDir(GetSessionsDir(), sessionID)
}

// RemoveSessionMetadataCache 清除指定会话的内存缓存。
//
// 对齐 Python: remove_session_metadata_cache(session_id)
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

// FlushMetadataQueue 等待 metadata 异步写入队列刷盘（供测试使用）。
//
// 向队列发送一个空哨兵项，等待它被消费即代表之前所有写入已完成。
func FlushMetadataQueue() {
	ensureMetadataWorker()
	// 发送一个 nil 哨兵项，worker 检测到后跳过写入但仍然消费了队列
	metadataQueue <- metadataWriteItem{sessionID: "", metadata: nil}
}

// ReadSessionMetadata 读取会话元数据文件。
//
// 不产生副作用：session 目录不存在时返回 nil 而非创建目录，
// 对齐 Python _read_metadata 的"读路径不应产生副作用"原则。
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
			Msg("读取 metadata.json 失败")
		return nil
	}
	return meta
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
// 对齐 Python _read_metadata 的缓存优先策略。
func ReadSessionMetadataWithCache(sessionID string) map[string]any {
	deliveryContextMu.RLock()
	if cached, ok := deliveryContextCache[sessionID]; ok {
		deliveryContextMu.RUnlock()
		return DeepCopyMap(cached)
	}
	deliveryContextMu.RUnlock()

	return ReadSessionMetadata(GetSessionsDir(), sessionID)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

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

// ensureMetadataWorker 确保 metadata 异步写入 worker 已启动。
// 对齐 Python: _ensure_worker_started()，懒启动后台 goroutine
func ensureMetadataWorker() {
	metadataQueueOnce.Do(func() {
		metadataQueue = make(chan metadataWriteItem, metadataQueueSize)
		go metadataWriteWorker()
	})
}

// metadataWriteWorker 异步写入 worker，消费队列并写入磁盘。
// 对齐 Python: _worker() 后台线程
func metadataWriteWorker() {
	for item := range metadataQueue {
		// 哨兵项：FlushMetadataQueue 发送的空项，跳过写入但消费了队列位置
		if item.sessionID == "" || item.metadata == nil {
			continue
		}
		if err := WriteSessionMetadata(GetSessionsDir(), item.sessionID, item.metadata); err != nil {
			logger.Warn(logComponent).
				Str("session_id", item.sessionID).
				Err(err).
				Msg("metadata 异步写入失败")
		}
	}
}

// EnqueueMetadataWrite 将写入操作放入异步队列，队列满时退化为同步写。
// 对齐 Python: _enqueue_write()
func EnqueueMetadataWrite(sessionID string, metadata map[string]any) {
	// 先更新缓存，确保后续读取能看到最新状态
	deliveryContextMu.Lock()
	deliveryContextCache[sessionID] = DeepCopyMap(metadata)
	deliveryContextMu.Unlock()

	ensureMetadataWorker()
	select {
	case metadataQueue <- metadataWriteItem{sessionID: sessionID, metadata: metadata}:
	default:
		// 队列满，退化为同步写入
		logger.Warn(logComponent).
			Str("session_id", sessionID).
			Msg("metadata 写入队列已满，退化为同步写入")
		_ = WriteSessionMetadata(GetSessionsDir(), sessionID, metadata)
	}
}
