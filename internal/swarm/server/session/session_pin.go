package session

import (
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session/lifecycle"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// sessionPinLock 置顶操作进程内全局锁（对齐 _SESSION_PIN_LOCK）。
var sessionPinLock sync.Mutex

// ──────────────────────────── 导出函数 ────────────────────────────

// SetSessionPinned 置顶/取消置顶会话，并对所有置顶会话紧凑重编号为 1..N。幂等。
// Python: session_metadata.py set_session_pinned（:1828-1943）。
//
// 整个操作在进程内全局锁内完成：
//  1. 读取目标会话状态；
//  2. 扫描全部会话，收集 pinned=true 的会话（跳过 lifecycle 已删除/已归档）；
//  3. 按 pin_order 升序稳定排序，重新分配 1..N（消除间隙）；
//  4. 仅同步写回状态或排序发生变化的会话。
//
// 新置顶的会话 pin_order 默认为 0，排序后置于最前；非置顶会话 pin_order 恒 0。
// 幂等：对已处于目标状态的会话再次操作视为成功。
//
// 返回 (操作后的 pinned, 操作后的 pin_order, 会话是否存在)。
func SetSessionPinned(sessionID string, pinned bool) (bool, int, bool) {
	sessionsDir := GetSessionsDir()
	sessionPinLock.Lock()
	defer sessionPinLock.Unlock()

	// 1. 读取目标会话状态（Python _read_metadata(session_id, cache_bust=True)）
	meta := ReadSessionMetadata(sessionsDir, sessionID)
	if meta == nil {
		logger.Info(logger.ComponentAgentServer).Str("session_id", sessionID).
			Str("outcome", "not_found").Msg("[session.pin]")
		return false, 0, false
	}

	currentOrder := intFromMeta(meta["pin_order"])
	if boolFromMeta(meta["pinned"]) == pinned &&
		((pinned && currentOrder > 0) || (!pinned && currentOrder == 0)) {
		// 幂等：重复点击/重试不重扫、不重写
		logger.Info(logger.ComponentAgentServer).Str("session_id", sessionID).
			Str("outcome", "unchanged").Msg("[session.pin]")
		return pinned, currentOrder, true
	}

	// 2. 扫描全部会话收集 pinned 会话（跳过 lifecycle 已删除/已归档）
	pinnedList := collectPinnedSessions(sessionsDir, sessionID, meta, pinned)

	// 3. 紧凑重编号 1..N
	sort.SliceStable(pinnedList, func(i, j int) bool {
		return pinnedList[i].order < pinnedList[j].order
	})

	// 4. 取消置顶先落目标态，再统一重编号；仅写变化的会话
	newOrder := 0
	if !pinned && (boolFromMeta(meta["pinned"]) || currentOrder != 0) {
		writePinState(sessionsDir, sessionID, false, 0)
	}
	for idx, item := range pinnedList {
		order := idx + 1
		if item.sessionID == sessionID {
			newOrder = order
		}
		if item.order == order && (item.sessionID != sessionID || pinned) {
			continue
		}
		writePinState(sessionsDir, item.sessionID, true, order)
	}
	logger.Info(logger.ComponentAgentServer).Str("session_id", sessionID).
		Bool("pinned", pinned).Str("outcome", "ok").Msg("[session.pin]")
	return pinned, newOrder, true
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// pinnedItem 收集态（对齐 Python pinned_list 元组）。
type pinnedItem struct {
	// sessionID 会话标识
	sessionID string
	// order 原 pin_order
	order int
}

// collectPinnedSessions 扫描会话目录收集置顶会话。
// Python: set_session_pinned 中段扫描（:1875-1899）：
// 跳过 health_check_/heartbeat_ 探活会话与 lifecycle 已删除/已归档会话；
// 目标会话仅在 pinned=true 时加入（加入尾部，排序后新置顶位于最前的语义由
// 其原 pin_order=0 保证）。
func collectPinnedSessions(sessionsDir, sessionID string, meta map[string]any, pinned bool) []pinnedItem {
	var pinnedList []pinnedItem
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return pinnedList
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sid := entry.Name()
		if sid == sessionID {
			continue
		}
		// Python _EPHEMERAL_PROBE_SESSION_PREFIXES = ("health_check_", "heartbeat_")
		if strings.HasPrefix(sid, "health_check_") || strings.HasPrefix(sid, "heartbeat_") {
			continue
		}
		state := lifecycle.State("session", sid)
		if lifecycle.LogicallyDeleted(state) || lifecycle.LogicallyArchived(state) {
			// 已接受但尚未移动/删除：已退出活动置顶排序
			continue
		}
		m := ReadSessionMetadata(sessionsDir, sid)
		if boolFromMeta(m["pinned"]) {
			pinnedList = append(pinnedList, pinnedItem{sessionID: sid, order: intFromMeta(m["pin_order"])})
		}
	}
	if pinned {
		pinnedList = append(pinnedList, pinnedItem{sessionID: sessionID, order: intFromMeta(meta["pin_order"])})
	}
	return pinnedList
}

// writePinState 同步写回单会话的 pinned/pin_order。
// Python: update_session_metadata(..., touch_last_message_at=False, sync_write=True)——
// 置顶不是消息，不刷新 last_message_at。
func writePinState(sessionsDir, sessionID string, pinned bool, pinOrder int) {
	meta := ReadSessionMetadata(sessionsDir, sessionID)
	if meta == nil {
		return
	}
	meta["pinned"] = pinned
	meta["pin_order"] = float64(pinOrder)
	if err := WriteSessionMetadata(sessionsDir, sessionID, meta); err != nil {
		logger.Warn(logger.ComponentAgentServer).Err(err).
			Str("session_id", sessionID).Msg("[session.pin] 写回失败")
	}
}

// intFromMeta 元数据 int 字段（JSON 解码值为 float64）。
func intFromMeta(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

// boolFromMeta 元数据 bool 字段。
func boolFromMeta(v any) bool {
	b, _ := v.(bool)
	return b
}
