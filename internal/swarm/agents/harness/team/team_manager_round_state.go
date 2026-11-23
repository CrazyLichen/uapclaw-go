package team

// team_manager_round_state.go R-7 回填（25.2）：TeamManager 四桩方法真实现。
//
// 上游：juiwenswarm/agents/harness/team/team_manager.py——L446
// set_round_state_reporter、L453 report_round_state、L480 generation_matches、
// L597 remove_waiter、L676 broadcast_event、L3008 is_generation_running
//（2026-12-04 快照）。
//
// Go 侧差异（偏差 D-25.2b-15）：上游 broadcast_event 的 active_rounds/
// terminal_armed/workflow 终态机制依赖回合账本（Go 无 _active_rounds 等价
// 面已移植），此处按计划 R-7「四行为」范围实现：reporter 转发（代次校验）、
// waiter 代次账本清理、事件分发到会话级等待者（channel 满丢弃）、代次运行
// 判定（activeSessionID 比对）。回收条件=回合账本面落地后升级 broadcast。

import (
	"fmt"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// tmPendingWaiter 会话级等待者（TeamManager 账本，与 adapter 层 channel 级
// pendingWaiters 分立——上游 team_manager._pending_waiters 与
// server 层是两套）。
type tmPendingWaiter struct {
	RequestID string
	Ch        chan map[string]any
	// Exclusive 排他等待者（chat stream 主等待者，Python _exclusive_waiters）。
	Exclusive bool
	// AttachOnly 仅观察（attach-only 订阅，Python _attach_waiter_ids）。
	AttachOnly bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SetRoundStateReporter 附加活跃/停泊团队轮的 Runtime 观察者。
// Python: set_round_state_reporter (team_manager.py L446-451)。
func (m *TeamManager) SetRoundStateReporter(reporter func(sessionID string, generation int, running bool)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.roundStateReporter = reporter
}

// ReportRoundState 向 Runtime 观察者上报当前观察到的轮活动。
// 执行附着时团队流已空闲则无轮迁移会自发触发 reporter——附着路径用它把
// 执行状态对齐到所观察的状态，否则 RUNNING 执行会让 Session 永远忙碌
// （不可删除）。generation 为 nil 表示无 Runtime 执行持有调用方，无需对齐。
// Python: report_round_state (team_manager.py L453-477)。
func (m *TeamManager) ReportRoundState(sessionID string, generation *int, running bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.roundStateReporter == nil || generation == nil {
		return
	}
	if !m.generationMatchesLocked(sessionID, *generation) {
		return
	}
	m.roundStateReporter(sessionID, *generation, running)
}

// GenerationMatches 回调是否属于当前 Session 代次。
// generation 为 nil 视为匹配（Python 同语义）。
// Python: generation_matches (team_manager.py L480-484)。
func (m *TeamManager) GenerationMatches(sessionID string, generation *int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.generationMatchesLocked(sessionID, derefGeneration(generation))
}

// SetSessionGeneration 记录会话当前代次（运行时附着/切换时调用）。
// Python: _session_generations 写入点。
func (m *TeamManager) SetSessionGeneration(sessionID string, generation int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionGenerations[sessionID] = generation
}

// HasInteractiveWaiter 是否存在非自动化的交互类消费者持有持久流。
// exclusive 本身不算（它可被心跳等自动化请求接管）；
// attach_only 纯观察也不算。
// Python: has_interactive_waiter (team_manager.py L562-570)。
func (m *TeamManager) HasInteractiveWaiter(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hasInteractiveWaiterLocked(sessionID)
}

// hasInteractiveWaiterLocked HasInteractiveWaiter 的锁内实现。
func (m *TeamManager) hasInteractiveWaiterLocked(sessionID string) bool {
	exclusiveID := m.exclusiveWaiters[sessionID]
	for _, waiter := range m.pendingWaitersTM[sessionID] {
		// Python: any(rid != exclusive_request_id ...)——非 exclusive 的登记
		// （含 persistent owner）即视为交互持有。
		if waiter.RequestID != exclusiveID || exclusiveID == "" {
			return true
		}
	}
	return false
}

// selectBroadcastWaitersLocked 择一选择本轮事件应投递的 waiter。
// 语义（Python _select_broadcast_waiters :641-674）：
//  1. exclusive 存在 → 只投 exclusive（轮期间 attach 不重复收）
//  2. 无 exclusive → 非 attach 的 input_waiters（persistent/request 类）优先
//  3. 全无 → 只投第一条 waiter（多观察订阅择一，防重复投递/重复落库）
//
// Gateway 层负责向"订阅该会话的每个客户端"扇出——waiter 层择一上游，
// 是 "fallback upstream observers, not additional fan-out destinations"。
func (m *TeamManager) selectBroadcastWaitersLocked(sessionID string) []tmPendingWaiter {
	waiters := m.pendingWaitersTM[sessionID]
	if len(waiters) == 0 {
		return nil
	}
	if exclusiveID := m.exclusiveWaiters[sessionID]; exclusiveID != "" {
		for _, waiter := range waiters {
			if waiter.RequestID == exclusiveID {
				return []tmPendingWaiter{waiter}
			}
		}
	}
	var inputWaiters []tmPendingWaiter
	for _, waiter := range waiters {
		if !waiter.AttachOnly {
			inputWaiters = append(inputWaiters, waiter)
		}
	}
	if len(inputWaiters) > 0 {
		return inputWaiters
	}
	return waiters[:1]
}

// RegisterSessionWaiter 注册一个会话级等待者（排他或仅观察）。
// Python: _pending_waiters/_exclusive_waiters/_attach_waiter_ids 写入点。
func (m *TeamManager) RegisterSessionWaiter(sessionID, requestID string, ch chan map[string]any, exclusive, attachOnly bool, generation int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := fmt.Sprintf("%s\x00%s", sessionID, requestID)
	m.waiterGenerations[key] = generation
	if exclusive {
		m.exclusiveWaiters[sessionID] = requestID
	}
	if attachOnly {
		if m.attachWaiterIDs[sessionID] == nil {
			m.attachWaiterIDs[sessionID] = map[string]bool{}
		}
		m.attachWaiterIDs[sessionID][requestID] = true
	}
	m.pendingWaitersTM[sessionID] = append(m.pendingWaitersTM[sessionID], tmPendingWaiter{
		RequestID: requestID, Ch: ch, Exclusive: exclusive, AttachOnly: attachOnly,
	})
}

// RemoveWaiter 按 request_id 移除等待者；清理空列表。
// generation 非 nil 且与登记代次不符时不动作。
// Python: remove_waiter (team_manager.py L597-624)。
func (m *TeamManager) RemoveWaiter(sessionID, requestID string, generation *int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := fmt.Sprintf("%s\x00%s", sessionID, requestID)
	expectedGeneration, recorded := m.waiterGenerations[key]
	if generation != nil && (!recorded || expectedGeneration != *generation) {
		return
	}
	delete(m.waiterGenerations, key)
	if m.exclusiveWaiters[sessionID] == requestID {
		delete(m.exclusiveWaiters, sessionID)
	}
	if attachIDs := m.attachWaiterIDs[sessionID]; attachIDs != nil {
		delete(attachIDs, requestID)
		if len(attachIDs) == 0 {
			delete(m.attachWaiterIDs, sessionID)
		}
	}
	waiters := m.pendingWaitersTM[sessionID]
	if waiters == nil {
		return
	}
	var remaining []tmPendingWaiter
	for _, waiter := range waiters {
		if waiter.RequestID != requestID {
			remaining = append(remaining, waiter)
		}
	}
	if len(remaining) > 0 {
		m.pendingWaitersTM[sessionID] = remaining
	} else {
		delete(m.pendingWaitersTM, sessionID)
	}
}

// BroadcastEvent 以背压向所有活跃等待者广播事件。
// channel 满时的短等待语义在 Go 以「丢弃 + debug 日志」承载（adapter 层
// broadcastEvent 同款既定差异）。返回是否至少一个等待者收到事件。
// Python: broadcast_event (team_manager.py L676-790+)。
func (m *TeamManager) BroadcastEvent(sessionID, eventType string, payload map[string]any, channelID string) bool {
	m.mu.Lock()
	// Python: waiters = self._select_broadcast_waiters(session_id)（:760）——
	// 择一上游（exclusive>input>attach[:1]），非全量 fan-out。
	waiters := m.selectBroadcastWaitersLocked(sessionID)
	m.mu.Unlock()
	if len(waiters) == 0 {
		// Python :739 waiters 空仍走到 :772 terminal fallback——不提前跳过。
		m.pushTerminalFallback(sessionID, eventType, payload, channelID)
		return false
	}
	delivered := false
	for _, waiter := range waiters {
		copied := make(map[string]any, len(payload)+1)
		for k, v := range payload {
			copied[k] = v
		}
		if eventType != "" {
			copied["event_type"] = eventType
		}
		select {
		case waiter.Ch <- copied:
			delivered = true
		default:
			// channel 满丢弃（Python 背压超时语义的 Go 承载，见文件头偏差注）。
		}
	}
	if !delivered {
		m.pushTerminalFallback(sessionID, eventType, payload, channelID)
	}
	return delivered
}

// pushTerminalFallback 无 waiter（或全部丢弃）的终态帧 → gateway server-push
// 侧通道兜底（Python :772-779 elif terminal → _push_event_without_waiter；
// workflow-terminal fallback 属 workflow 域，本处仅 chat.processing_status 终态帧）。
func (m *TeamManager) pushTerminalFallback(sessionID, eventType string, payload map[string]any, channelID string) {
	terminal := eventType == "chat.processing_status" &&
		payload["is_processing"] == false && payload["is_complete"] == true
	m.mu.Lock()
	fallback := m.pushFallback
	m.mu.Unlock()
	if fallback == nil || !terminal {
		return
	}
	ch := strings.TrimSpace(channelID)
	if ch == "" {
		// Python :907 str(channel_id or "").strip() or "web"
		ch = "web"
	}
	copied := make(map[string]any, len(payload)+1)
	for k, v := range payload {
		copied[k] = v
	}
	if _, has := copied["session_id"]; !has {
		copied["session_id"] = sessionID // Python :903 payload.setdefault
	}
	fallback(ch, sessionID, copied)
}

// SetPushFallback 注入无 waiter 终态帧的 server-push 兜底回调。
// Python: _push_event_without_waiter 经懒 import AgentWebSocketServer.send_push；
// Go 以回调注入承载（防 team→server 反向依赖，AgentServer.SendPush 经适配接入）。
// Python: team_manager.py:879-910（回调即该函数的注入等价物）。
func (m *TeamManager) SetPushFallback(fallback func(channelID, sessionID string, payload map[string]any)) {
	m.mu.Lock()
	m.pushFallback = fallback
	m.mu.Unlock()
}

// HasInputWaiters 是否存在属于输入持有流的等待者。
// 仅观察的订阅不拥有团队流——它们的存在不得把新用户轮翻成 follow-up
// （follow-up 路径会投递进已停止的 runtime 并停在 gate_closed）。
// Python: has_input_waiters (team_manager.py L628-646)。
func (m *TeamManager) HasInputWaiters(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	attachIDs := m.attachWaiterIDs[sessionID]
	waiters := m.pendingWaitersTM[sessionID]
	if len(attachIDs) == 0 {
		return len(waiters) > 0
	}
	for _, waiter := range waiters {
		if !attachIDs[waiter.RequestID] {
			return true
		}
	}
	return false
}

// IsGenerationRunning 当前代次是否拥有活跃团队工作。
// Python: is_generation_running (team_manager.py L3008-3012)。
func (m *TeamManager) IsGenerationRunning(sessionID string, generation int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.generationMatchesLocked(sessionID, generation) {
		return false
	}
	return m.activeSessionID == sessionID
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// generationMatchesLocked 代次校验（持锁调用；generation<=0 视为未登记代次，
// 匹配任意——对齐 Python generation=None 语义）。
func (m *TeamManager) generationMatchesLocked(sessionID string, generation int) bool {
	if generation <= 0 {
		return true
	}
	return m.sessionGenerations[sessionID] == generation
}

// derefGeneration 指针代次解引用（nil → 0）。
func derefGeneration(generation *int) int {
	if generation == nil {
		return 0
	}
	return *generation
}

// unusedStrings 保留 strings 引用（供后续扩展日志字段）。
var _ = strings.TrimSpace
