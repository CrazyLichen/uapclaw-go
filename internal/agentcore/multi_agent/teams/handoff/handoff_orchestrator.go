package handoff

import (
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// handoffResult 交接编排结果，区分正常结果和错误。
//
// 对齐 Python asyncio.Future 的 set_result/set_exception 双通道语义：
//   - 正常完成：result 有值，err 为 nil
//   - 异常完成：result 为 nil，err 有值
//
// 消费方通过 hr.err != nil 判断是否出错。
type handoffResult struct {
	// result 正常结果
	result map[string]any
	// err 异常错误
	err error
}

// HandoffOrchestrator 每会话交接协调器。
//
// 管理交接路由、当前活跃 Agent、交接次数计数，
// 并提供完成/错误通道用于通知编排循环结束。
//
// 对应 Python: HandoffOrchestrator (handoff_orchestrator.py)
type HandoffOrchestrator struct {
	// maxHandoffs 最大交接次数
	maxHandoffs int
	// terminationCondition 可选终止条件
	terminationCondition func(*HandoffOrchestrator) bool
	// handoffCount 已完成交接次数
	handoffCount int
	// currentAgentID 当前活跃 Agent ID
	currentAgentID string
	// routeGraph 路由邻接表
	routeGraph map[string]map[string]struct{}
	// doneCh 完成通道（缓冲 1）
	doneCh chan handoffResult
	// doneOnce 保证只发送一次
	doneOnce sync.Once
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// CoordinatorStateKey 协调器状态持久化键
	CoordinatorStateKey = "__handoff_coordinator__"
	// HandoffHistoryKey 交接历史持久化键
	HandoffHistoryKey = "__handoff_history__"
	// defaultMaxHandoffs 默认最大交接次数
	defaultMaxHandoffs = 10
	// logComponent 日志组件标识
	logComponent = logger.ComponentChannel
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewHandoffOrchestrator 创建交接协调器实例。
//
// 从 config 提取 maxHandoffs/terminationCondition，
// 构建 routeGraph，初始化 doneCh。
//
// 对应 Python: HandoffOrchestrator.__init__(start_agent_id, registered_agents, config)
func NewHandoffOrchestrator(startAgentID string, registeredAgents []string, config *HandoffConfig) *HandoffOrchestrator {
	maxHandoffs := defaultMaxHandoffs
	var terminationCondition func(*HandoffOrchestrator) bool
	var routes []HandoffRoute

	if config != nil {
		if config.MaxHandoffs > 0 {
			maxHandoffs = config.MaxHandoffs
		}
		terminationCondition = config.TerminationCondition
		routes = config.Routes
	}

	routeGraph := BuildRouteGraph(registeredAgents, routes)

	coord := &HandoffOrchestrator{
		maxHandoffs:          maxHandoffs,
		terminationCondition: terminationCondition,
		handoffCount:         0,
		currentAgentID:       startAgentID,
		routeGraph:           routeGraph,
		doneCh:               make(chan handoffResult, 1),
	}

	logger.Info(logComponent).
		Str("action", "new_handoff_orchestrator").
		Str("start_agent_id", startAgentID).
		Int("max_handoffs", maxHandoffs).
		Int("registered_agents_count", len(registeredAgents)).
		Msg("创建交接协调器")

	return coord
}

// BuildRouteGraph 构建路由邻接表。
//
// 空 routes → 全互联（每个 Agent 可交接给其他所有 Agent）。
// 有 routes → 只允许显式路由。
//
// 对应 Python: HandoffOrchestrator.build_route_graph(agents, routes)
func BuildRouteGraph(agents []string, routes []HandoffRoute) map[string]map[string]struct{} {
	graph := make(map[string]map[string]struct{}, len(agents))
	for _, a := range agents {
		graph[a] = make(map[string]struct{})
	}

	if len(routes) == 0 {
		// 全互联：每个 Agent 可交接给其他所有 Agent
		for _, src := range agents {
			for _, tgt := range agents {
				if src != tgt {
					graph[src][tgt] = struct{}{}
				}
			}
		}
	} else {
		// 显式路由
		for _, r := range routes {
			if _, ok := graph[r.Source]; !ok {
				graph[r.Source] = make(map[string]struct{})
			}
			graph[r.Source][r.Target] = struct{}{}
		}
	}

	logger.Info(logComponent).
		Str("action", "build_route_graph").
		Int("agents_count", len(agents)).
		Int("routes_count", len(routes)).
		Msg("构建路由邻接表")

	return graph
}

// RequestHandoff 请求交接到目标 Agent。
//
// 检查 maxHandoffs、terminationCondition、路由允许，
// 全部通过时更新 handoffCount 和 currentAgentID。
//
// 对应 Python: HandoffOrchestrator.request_handoff(target_id, reason)
func (o *HandoffOrchestrator) RequestHandoff(targetID string, reason string) bool {
	// 检查最大交接次数
	if o.handoffCount >= o.maxHandoffs {
		logger.Warn(logComponent).
			Str("action", "request_handoff_rejected").
			Str("reason", "max_handoffs_exceeded").
			Str("current_agent_id", o.currentAgentID).
			Str("target_id", targetID).
			Int("handoff_count", o.handoffCount).
			Int("max_handoffs", o.maxHandoffs).
			Msg("交接请求被拒绝：超过最大交接次数")
		return false
	}

	// 检查终止条件
	if o.terminationCondition != nil {
		if o.terminationCondition(o) {
			logger.Warn(logComponent).
				Str("action", "request_handoff_rejected").
				Str("reason", "termination_condition_met").
				Str("current_agent_id", o.currentAgentID).
				Str("target_id", targetID).
				Msg("交接请求被拒绝：终止条件已满足")
			return false
		}
	}

	// 检查路由允许
	allowed, exists := o.routeGraph[o.currentAgentID]
	if !exists {
		logger.Warn(logComponent).
			Str("action", "request_handoff_rejected").
			Str("reason", "source_not_in_route_graph").
			Str("current_agent_id", o.currentAgentID).
			Str("target_id", targetID).
			Msg("交接请求被拒绝：源 Agent 不在路由图中")
		return false
	}
	if _, ok := allowed[targetID]; !ok {
		logger.Warn(logComponent).
			Str("action", "request_handoff_rejected").
			Str("reason", "route_not_allowed").
			Str("current_agent_id", o.currentAgentID).
			Str("target_id", targetID).
			Msg("交接请求被拒绝：路由不允许")
		return false
	}

	// 批准交接
	o.handoffCount++
	o.currentAgentID = targetID

	logger.Info(logComponent).
		Str("action", "request_handoff_approved").
		Str("target_id", targetID).
		Str("reason", reason).
		Int("handoff_count", o.handoffCount).
		Msg("交接请求已批准")

	return true
}

// Complete 标记编排完成，发送结果到 doneCh。
//
// doneOnce 保证只发送一次。
//
// 对应 Python: HandoffOrchestrator.complete(result) — 调用 done_future.set_result(result)
func (o *HandoffOrchestrator) Complete(result map[string]any) {
	o.doneOnce.Do(func() {
		o.doneCh <- handoffResult{result: result}
		close(o.doneCh)
		logger.Info(logComponent).
			Str("action", "handoff_orchestrator_complete").
			Msg("交接编排完成，结果已发送到 doneCh")
	})
}

// Error 标记编排错误，发送错误到 doneCh。
//
// 对齐 Python asyncio.Future.set_exception(exception)：
// 错误通过 handoffResult.err 字段传递，消费方通过 hr.err != nil 判断。
//
// 对应 Python: HandoffOrchestrator.error(exception) — 调用 done_future.set_exception(exception)
func (o *HandoffOrchestrator) Error(err error) {
	o.doneOnce.Do(func() {
		o.doneCh <- handoffResult{err: err}
		close(o.doneCh)
		logger.Error(logComponent).Err(err).
			Str("action", "handoff_orchestrator_error").
			Msg("交接编排发生错误，错误已发送到 doneCh")
	})
}

// Close 关闭 doneCh，确保 channel 被关闭。
//
// 在 _run_chain 的 defer 中调用。
func (o *HandoffOrchestrator) Close() {
	o.doneOnce.Do(func() {
		close(o.doneCh)
	})
}

// DoneCh 返回只读完成通道。
func (o *HandoffOrchestrator) DoneCh() <-chan handoffResult {
	return o.doneCh
}

// HandoffCount 返回已完成交接次数。
func (o *HandoffOrchestrator) HandoffCount() int {
	return o.handoffCount
}

// CurrentAgentID 返回当前活跃 Agent ID。
func (o *HandoffOrchestrator) CurrentAgentID() string {
	return o.currentAgentID
}

// SaveToSession 将协调器状态持久化到会话。
//
// 对应 Python: HandoffOrchestrator.save_to_session(session)
func (o *HandoffOrchestrator) SaveToSession(sess *session.AgentTeamSession) {
	sess.UpdateState(map[string]any{
		CoordinatorStateKey: map[string]any{
			"current_agent_id": o.currentAgentID,
			"handoff_count":    o.handoffCount,
		},
	})

	logger.Info(logComponent).
		Str("action", "save_to_session").
		Str("current_agent_id", o.currentAgentID).
		Int("handoff_count", o.handoffCount).
		Msg("协调器状态已持久化到会话")
}

// RestoreFromSession 从会话恢复协调器状态。
//
// 对应 Python: HandoffOrchestrator.restore_from_session(session, start_agent_id, registered_agents, config)
func RestoreFromSession(
	sess *session.AgentTeamSession,
	startAgentID string,
	registeredAgents []string,
	config *HandoffConfig,
) *HandoffOrchestrator {
	coord := NewHandoffOrchestrator(startAgentID, registeredAgents, config)

	snapshot, err := sess.GetState(state.StringKey(CoordinatorStateKey))
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Str("action", "restore_from_session").
			Msg("获取协调器状态失败，使用默认值")
		return coord
	}
	if snapshot == nil {
		logger.Info(logComponent).
			Str("action", "restore_from_session").
			Msg("无协调器状态快照，使用默认值")
		return coord
	}

	snapMap, ok := snapshot.(map[string]any)
	if !ok {
		logger.Warn(logComponent).
			Str("action", "restore_from_session").
			Msg("协调器状态快照类型异常，使用默认值")
		return coord
	}

	if currentID, ok := snapMap["current_agent_id"].(string); ok {
		coord.currentAgentID = currentID
	}
	if count, ok := snapMap["handoff_count"].(float64); ok {
		coord.handoffCount = int(count)
	} else if count, ok := snapMap["handoff_count"].(int); ok {
		coord.handoffCount = count
	}

	logger.Info(logComponent).
		Str("action", "restore_from_session").
		Str("current_agent_id", coord.currentAgentID).
		Int("handoff_count", coord.handoffCount).
		Msg("从会话恢复协调器状态")

	return coord
}
