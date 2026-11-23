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

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
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

// HasInteractiveWaiter 是否存在非 exclusive 的消费者持有会话响应。
// Python: has_interactive_waiter (team_manager.py L562-570)——
// any(rid != exclusive_request_id)：除 exclusive（可被自动化请求接管）外，
// 任何登记（persistent owner 与 attach 订阅）都算交互持有。
// 用途：_team_waiter_kind 的防双投判定（已有持有者时新普通轮 → NONE）。
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
	terminal := eventType == "chat.processing_status" &&
		payload["is_processing"] == false && payload["is_complete"] == true

	m.mu.Lock()
	// 回合账本段（Python broadcast_event :717-751）：
	// unarmed terminal 丢弃——持久 Runner 流在上回合释放后吐出的幽灵终态帧
	// 会误关新回合的 exclusive waiter，未武装即丢弃（:718-728）。
	if current := m.activeRounds[sessionID]; current != nil {
		if terminal && !current.TerminalArmed {
			logger.Info(logComponent).
				Str("session_id", sessionID).Str("request_id", current.RequestID).
				Msg("ignored unarmed terminal")
			m.mu.Unlock()
			return false
		}
		// 非终态事件武装回合（:733-735）
		if !terminal {
			current.TerminalArmed = true
		}
		// flow 终态见证即记录（:738-741）——无论有没有 waiter
		if m.isWorkflowTerminalEventLocked(eventType, payload) {
			current.SawFlowTerminal = true
		}
		// team.task 归档守卫记账（:726-737）
		if eventType == "team.task" {
			if task, ok := payload["event"].(map[string]any); ok {
				if taskID, _ := task["task_id"].(string); taskID != "" {
					status, _ := task["status"].(string)
					taskType, _ := task["type"].(string)
					if status == "completed" || status == "cancelled" || status == "failed" ||
						strings.HasSuffix(taskType, ".completed") ||
						strings.HasSuffix(taskType, ".cancelled") ||
						strings.HasSuffix(taskType, ".verified") {
						delete(current.OpenArchiveTasks, taskID)
					} else if status != "" || strings.HasSuffix(taskType, ".created") ||
						strings.HasSuffix(taskType, ".started") ||
						strings.HasSuffix(taskType, ".claimed") {
						current.OpenArchiveTasks[taskID] = true
					}
				}
			}
		}
		// flow 终态后的 tool_call/ask_user 视为「flow 后仍有执行」（:743-751）
		if current.SawFlowTerminal && (eventType == "chat.tool_call" || eventType == "chat.ask_user_question") {
			current.ContinuedAfterFlow = true
		}
	}
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

// SessionWaiterCount 会话当前 waiter 数（监控/诊断面）。
func (m *TeamManager) SessionWaiterCount(sessionID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.pendingWaitersTM[sessionID])
}

// SessionWaiterIDs 会话当前 waiter 的 requestID 列表（监控/诊断面）。
func (m *TeamManager) SessionWaiterIDs(sessionID string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.pendingWaitersTM[sessionID]))
	for _, w := range m.pendingWaitersTM[sessionID] {
		ids = append(ids, w.RequestID)
	}
	return ids
}

// ──────────────────────────── 回合账本（25.6 批三回填，D-25.2b-15） ────────────────────────────

// ActiveTeamRound 一个已提交团队交互回合的进程内所有权账本。
// Python: _ActiveTeamRound (team_manager.py:259-271)。
type ActiveTeamRound struct {
	// RequestID 本回合绑定的请求标识
	RequestID string
	// Generation 会话代次（绑定时快照）
	Generation int
	// DeferTerminalRelease 终态事件暂缓释放回合（cron 移交窗口等）
	DeferTerminalRelease bool
	// TerminalArmed 武装开关：本回合见证过任何非终态事件后置位；
	// 之后的终态帧才被接受。回合开始即 unarmed——持久 Runner 流在上一
	// 回合释放后吐出的幽灵终态帧（armed=False）会被丢弃，防止误关
	// 新回合的 exclusive waiter。
	// Python: terminal_armed (team_manager.py:260)。
	TerminalArmed bool
	// SawFlowTerminal 本回合见证过 swarmflow/workflow 终态事件
	// Python: saw_flow_terminal (team_manager.py:262-264)
	SawFlowTerminal bool
	// ContinuedAfterFlow flow 终态之后仍有执行（终态重放不得掩盖后续执行）
	// Python: continued_after_flow (team_manager.py:265)
	ContinuedAfterFlow bool
	// OpenArchiveTasks 归档守卫跟踪的未完结任务 id 集合
	// Python: open_archive_tasks (team_manager.py:266)
	OpenArchiveTasks map[string]bool
	// ReleaseAdmission 回合释放时的准入归还回调（nil 跳过）
	// Python: release_admission (team_manager.py:261)。Go 为同步形态
	// （Python Awaitable——当前调用方均无异步归还需求）。
	ReleaseAdmission func()
}

// BeginRound 绑定回合所有权到请求。
// Python: begin_round (team_manager.py:1003-1035)。已活跃回合存在时报错
// 语义在 Go 以返回 error 承载（Python RuntimeError）。
func (m *TeamManager) BeginRound(sessionID, requestID string, generation *int, releaseAdmission func(), deferTerminalRelease, terminalArmed bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if generation != nil {
		m.sessionGenerations[sessionID] = *generation
	}
	if current := m.activeRounds[sessionID]; current != nil {
		return fmt.Errorf("team round already active for session %s: %s", sessionID, current.RequestID)
	}
	gen := 0
	if generation != nil {
		gen = *generation
	} else {
		gen = m.sessionGenerations[sessionID]
	}
	m.activeRounds[sessionID] = &ActiveTeamRound{
		RequestID:            requestID,
		Generation:           gen,
		ReleaseAdmission:     releaseAdmission,
		DeferTerminalRelease: deferTerminalRelease,
		TerminalArmed:        terminalArmed,
		OpenArchiveTasks:     map[string]bool{},
	}
	return nil
}

// FinishRound 运行时终态事件收尾当前回合。
// Python: finish_round (team_manager.py:1038-1043)。
func (m *TeamManager) FinishRound(sessionID string) bool {
	m.mu.Lock()
	current := m.activeRounds[sessionID]
	deferTerminal := current != nil && current.DeferTerminalRelease
	var requestID string
	if current != nil {
		requestID = current.RequestID
	}
	m.mu.Unlock()
	if current == nil || deferTerminal {
		return false
	}
	return m.ReleaseRound(sessionID, requestID, nil)
}

// ReleaseRound 幂等释放一个回合及其准入租约。
// Python: release_round (team_manager.py:1046-1082)。
func (m *TeamManager) ReleaseRound(sessionID, requestID string, generation *int) bool {
	m.mu.Lock()
	current := m.activeRounds[sessionID]
	if current == nil || current.RequestID != requestID {
		m.mu.Unlock()
		return false
	}
	if generation != nil && current.Generation != *generation {
		m.mu.Unlock()
		return false
	}
	delete(m.activeRounds, sessionID)
	// 回合终态同时结束该 turn 的在飞窗口（归档闸门不再把已完结会话当运行中）
	m.endRequestLocked(sessionID, requestID, generation)
	// 流还在时记录"回合已结束的请求"——挂在其持久流上的 handler 据此
	// 被归档闸门豁免；流已不在时保留会泄漏并可能被误认为未来回合。
	if _, hasTask := m.streamTasks[sessionID]; hasTask {
		if m.roundEndedRequests[sessionID] == nil {
			m.roundEndedRequests[sessionID] = map[string]bool{}
		}
		m.roundEndedRequests[sessionID][requestID] = true
	}
	releaseAdmission := current.ReleaseAdmission
	m.mu.Unlock()

	if releaseAdmission != nil {
		releaseAdmission()
	}
	return true
}

// ReleaseCurrentRound 释放持有刚终止流的那个回合。
// Python: release_current_round (team_manager.py:1084-1090)。
func (m *TeamManager) ReleaseCurrentRound(sessionID string) bool {
	m.mu.Lock()
	current := m.activeRounds[sessionID]
	var requestID string
	if current != nil {
		requestID = current.RequestID
	}
	m.mu.Unlock()
	if current == nil {
		return false
	}
	return m.ReleaseRound(sessionID, requestID, nil)
}

// IsRoundActive 会话是否有活跃回合（区别于传输层活跃）。
// Python: is_round_active (team_manager.py:1092-1094)。
func (m *TeamManager) IsRoundActive(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.activeRounds[sessionID]
	return ok
}

// RoundCanFinishAfterFlow 回合是否见过 flow 终态且无未完结/后续工作。
// Python: round_can_finish_after_flow (team_manager.py:1096-1103)。
func (m *TeamManager) RoundCanFinishAfterFlow(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.activeRounds[sessionID]
	return current != nil && current.SawFlowTerminal &&
		!current.ContinuedAfterFlow && len(current.OpenArchiveTasks) == 0
}

// IsRoundOwner 回合是否由指定请求持有。
// Python: is_round_owner (team_manager.py:1105-1108)。
func (m *TeamManager) IsRoundOwner(sessionID, requestID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.activeRounds[sessionID]
	return current != nil && current.RequestID == requestID
}

// IsRoundEndedRequest 该请求的团队回合是否已释放。
// Python: is_round_ended_request (team_manager.py:1110-1116)。
func (m *TeamManager) IsRoundEndedRequest(sessionID, requestID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.roundEndedRequests[sessionID][requestID]
}

// BeginRequest 标记一个在飞团队 turn（适配器在 begin_round 前即调用——
// spec 组装/MCP 预检/运行时激活窗口内会话不能看起来空闲）。
// Python: begin_request (team_manager.py:515-533)。
func (m *TeamManager) BeginRequest(sessionID, requestID string, generation *int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.beginRequestLocked(sessionID, requestID, generation)
}

// beginRequestLocked BeginRequest 的锁内实现。
func (m *TeamManager) beginRequestLocked(sessionID, requestID string, generation *int) {
	if generation != nil {
		m.sessionGenerations[sessionID] = *generation
	}
	normalized := requestID
	if m.inflightRequests[sessionID] == nil {
		m.inflightRequests[sessionID] = map[string]bool{}
	}
	m.inflightRequests[sessionID][normalized] = true
	if generation != nil {
		m.requestGenerations[[2]string{sessionID, normalized}] = *generation
	}
}

// EndRequest 幂等释放一个在飞团队 turn。
// Python: end_request (team_manager.py:535-551)。
func (m *TeamManager) EndRequest(sessionID, requestID string, generation *int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.endRequestLocked(sessionID, requestID, generation)
}

// endRequestLocked EndRequest 的锁内实现。
func (m *TeamManager) endRequestLocked(sessionID, requestID string, generation *int) {
	normalized := requestID
	key := [2]string{sessionID, normalized}
	if generation != nil {
		if expected, ok := m.requestGenerations[key]; ok && expected != *generation {
			return
		}
	}
	requests := m.inflightRequests[sessionID]
	if requests == nil {
		return
	}
	delete(requests, normalized)
	delete(m.requestGenerations, key)
	if len(requests) == 0 {
		delete(m.inflightRequests, sessionID)
	}
}

// HasInflightRequest 会话是否有在飞团队 turn。
// Python: has_inflight_request (team_manager.py:553-554)。
func (m *TeamManager) HasInflightRequest(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.inflightRequests[sessionID]) > 0
}

// MarkSawFlowTerminal 记录本回合见证 flow 终态（broadcast_event 内调用）。
// Python: broadcast_event 内 current_round.saw_flow_terminal 置位（:738-741）。
func (m *TeamManager) MarkSawFlowTerminal(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if current := m.activeRounds[sessionID]; current != nil {
		current.SawFlowTerminal = true
	}
}

// MarkContinuedAfterFlow 记录 flow 终态后仍有执行。
// Python: broadcast_event 内 continued_after_flow 置位（:744-751）。
func (m *TeamManager) MarkContinuedAfterFlow(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if current := m.activeRounds[sessionID]; current != nil {
		current.ContinuedAfterFlow = true
	}
}

// TrackArchiveTask 归档守卫任务状态记账（completed/cancelled/failed 出集，
// created/started/claimed 入集）。
// Python: broadcast_event 内 open_archive_tasks 维护（:726-737）。
func (m *TeamManager) TrackArchiveTask(sessionID, taskID, status, taskType string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.activeRounds[sessionID]
	if current == nil || taskID == "" {
		return
	}
	if status == "completed" || status == "cancelled" || status == "failed" ||
		strings.HasSuffix(taskType, ".completed") ||
		strings.HasSuffix(taskType, ".cancelled") ||
		strings.HasSuffix(taskType, ".verified") {
		delete(current.OpenArchiveTasks, taskID)
		return
	}
	if status != "" || strings.HasSuffix(taskType, ".created") ||
		strings.HasSuffix(taskType, ".started") ||
		strings.HasSuffix(taskType, ".claimed") {
		current.OpenArchiveTasks[taskID] = true
	}
}

// ExpectedRequestID 当前活跃回合的请求标识（无回合时空串）。
// StopSessionRuntime expected_request_id guard 的读取面
// （team_manager_lifecycle.go :420/:496 两个等待点）。
func (m *TeamManager) ExpectedRequestID(sessionID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if current := m.activeRounds[sessionID]; current != nil {
		return current.RequestID
	}
	return ""
}

// isWorkflowTerminalEventLocked 事件是否为携带终态状态的 workflow.updated。
// Python: _is_workflow_terminal_event (team_manager.py:869-877)。
func (m *TeamManager) isWorkflowTerminalEventLocked(eventType string, event map[string]any) bool {
	if eventType != "workflow.updated" && event["event_type"] != "workflow.updated" {
		return false
	}
	status := ""
	if workflow, ok := event["workflow"].(map[string]any); ok {
		status, _ = workflow["status"].(string)
	} else {
		status, _ = event["status"].(string)
	}
	switch status {
	case "stopped", "paused", "completed", "failed":
		return true
	}
	return false
}
