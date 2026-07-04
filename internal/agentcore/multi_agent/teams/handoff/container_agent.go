package handoff

import (
	"context"
	"fmt"
	"sort"
	"sync"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/team_runtime"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ContainerAgent 交接容器 Agent，包装目标 Agent 并注入交接工具和上下文历史管理。
//
// 职责：
//   - 懒初始化目标 Agent（targetProvider）
//   - 一次性注入 HandoffTool 到目标 Agent 的 AbilityManager + ResourceMgr
//   - 管理 Agent 上下文历史的保存和恢复
//   - 从执行结果提取交接信号/中断信号
//   - 通过 coordinator 控制编排流程（complete/request_handoff）
//
// 对应 Python: ContainerAgent (container_agent.py)
type ContainerAgent struct {
	team_runtime.CommunicableAgent // 嵌入，获得 Send/Publish/Subscribe/IsBound/Runtime 方法
	// targetCard 目标 Agent 的身份卡片
	targetCard *agentschema.AgentCard
	// targetProvider 目标 Agent 提供者函数
	targetProvider maschema.TeamAgentProvider
	// allowedTargets 允许交接的目标 Agent ID 集合
	allowedTargets map[string]struct{}
	// coordinatorLookup 协调器查找函数，sessionID → *HandoffOrchestrator
	coordinatorLookup func(sessionID string) *HandoffOrchestrator
	// targetInstance 懒初始化的目标 Agent 实例
	targetInstance agentinterfaces.BaseAgent
	// toolsInjected 工具是否已注入
	toolsInjected bool
	// mu 保护 targetInstance 和 toolsInjected 的并发安全
	mu sync.Mutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// HandoffRequestKey 交接请求在 inputs map 中的键名
	HandoffRequestKey = "__handoff_request__"
	// contextHistoryKey 上下文历史在 team session 中的持久化键
	contextHistoryKey = "__handoff_ctx_history__"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 确保 ContainerAgent 满足 BaseAgent 接口
var _ agentinterfaces.BaseAgent = (*ContainerAgent)(nil)

// 确保 HandoffTool 满足 Tool 接口（冗余验证，handoff_tool.go 中已有）
var _ tool.Tool = (*HandoffTool)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewContainerAgent 创建 ContainerAgent 实例。
//
// 参数：
//   - targetCard：目标 Agent 的身份卡片
//   - targetProvider：目标 Agent 提供者函数
//   - allowedTargets：允许交接的目标 Agent ID 集合
//   - coordinatorLookup：协调器查找函数（可选）
//
// 对应 Python: ContainerAgent(target_card, target_provider, allowed_targets, coordinator_lookup)
func NewContainerAgent(
	targetCard *agentschema.AgentCard,
	targetProvider maschema.TeamAgentProvider,
	allowedTargets map[string]struct{},
	coordinatorLookup func(sessionID string) *HandoffOrchestrator,
) *ContainerAgent {
	return &ContainerAgent{
		targetCard:        targetCard,
		targetProvider:    targetProvider,
		allowedTargets:    allowedTargets,
		coordinatorLookup: coordinatorLookup,
	}
}

// Card 返回目标 Agent 的身份卡片。
// 实现 BaseAgent 接口。
func (c *ContainerAgent) Card() *agentschema.AgentCard {
	return c.targetCard
}

// Config 返回 nil（ContainerAgent 无配置）。
// 实现 BaseAgent 接口。
func (c *ContainerAgent) Config() agentinterfaces.AgentConfig {
	return nil
}

// AbilityManager 返回 nil（ContainerAgent 不直接管理能力）。
// 实现 BaseAgent 接口。
func (c *ContainerAgent) AbilityManager() agentinterfaces.AbilityManagerInterface {
	return nil
}

// CallbackManager 返回 nil（ContainerAgent 不直接管理回调）。
// 实现 BaseAgent 接口。
func (c *ContainerAgent) CallbackManager() *rail.AgentCallbackManager {
	return nil
}

// Configure 空操作，返回 nil。
// 实现 BaseAgent 接口。
//
// 对应 Python: ContainerAgent.configure(config) → self
func (c *ContainerAgent) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}

// RegisterCallback 空操作，ContainerAgent 不支持回调注册。
// 实现 BaseAgent 接口。
func (c *ContainerAgent) RegisterCallback(_ context.Context, _ any, _ any, _ ...callback.CallbackOption) error {
	return nil
}

// RegisterRail 空操作，ContainerAgent 不支持 Rail 注册。
// 实现 BaseAgent 接口。
func (c *ContainerAgent) RegisterRail(_ context.Context, _ rail.AgentRail, _ ...callback.CallbackOption) error {
	return nil
}

// UnregisterRail 空操作，ContainerAgent 不支持 Rail 注销。
// 实现 BaseAgent 接口。
func (c *ContainerAgent) UnregisterRail(_ context.Context, _ rail.AgentRail) error {
	return nil
}

// Invoke 非流式调用 ContainerAgent，执行交接流程。
//
// 流程：
//  1. 从 inputs 提取 HandoffRequest
//  2. 查找协调器
//  3. 懒初始化目标 Agent + 注入交接工具
//  4. 构建目标 Agent 输入
//  5. 调用目标 Agent（有 team session 时走流式路径，否则直接调用）
//  6. 提取中断信号 → 处理中断
//  7. 提取交接信号 → 请求交接或完成编排
//
// 对应 Python: ContainerAgent.invoke(inputs, session)
func (c *ContainerAgent) Invoke(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (map[string]any, error) {
	// 1. 提取 HandoffRequest
	reqVal, ok := inputs[HandoffRequestKey]
	if !ok {
		return map[string]any{}, nil
	}
	req, ok := reqVal.(*HandoffRequest)
	if !ok || req == nil {
		return map[string]any{}, nil
	}

	sessionID := req.SessionID()

	// 2. 查找协调器
	var coordinator *HandoffOrchestrator
	if c.coordinatorLookup != nil {
		coordinator = c.coordinatorLookup(sessionID)
	}
	if coordinator == nil {
		errMsg := fmt.Sprintf("coordinator not found for session_id=%s", sessionID)
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "ContainerAgent.Invoke").
			Str("session_id", sessionID).
			Msg(errMsg)
		return nil, exception.BuildError(exception.StatusAgentTeamExecutionError,
			exception.WithParam("error_msg", errMsg),
		)
	}

	history := make([]HandoffHistoryEntry, len(req.History))
	copy(history, req.History)
	teamSession := req.Session

	var result map[string]any
	var signal *HandoffSignal

	// 3-5. 调用目标 Agent
	targetAgent, err := c.getTargetAgent(ctx)
	if err != nil {
		logger.Error(logComponent).Err(err).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "ContainerAgent.Invoke").
			Str("agent_id", c.targetCard.ID).
			Msg("获取目标 Agent 失败")
		coordinator.Error(err)
		return map[string]any{}, nil
	}

	c.injectToolsOnce(ctx, targetAgent)
	agentInput := c.buildAgentInput(req)

	var interruptSignal *TeamInterruptSignal

	if teamSession != nil {
		result, signal, err = c.invokeTargetWithStream(ctx, targetAgent, agentInput, teamSession)
		if err != nil {
			interruptSignal = ExtractInterruptSignal(nil, err)
			if interruptSignal == nil {
				logger.Error(logComponent).Err(err).
					Str("event_type", "LLM_CALL_ERROR").
					Str("method", "ContainerAgent.Invoke").
					Str("agent_id", c.targetCard.ID).
					Msg("目标 Agent 执行错误")
				coordinator.Error(err)
				return map[string]any{}, nil
			}
		}
	} else {
		agentSession := session.CreateAgentSession(sessionID, targetAgent.Card(), nil)
		agentOpts := []agentinterfaces.AgentOption{
			agentinterfaces.WithSession(agentSession),
		}
		result, err = targetAgent.Invoke(ctx, agentInput, agentOpts...)
		if err != nil {
			interruptSignal = ExtractInterruptSignal(nil, err)
			if interruptSignal == nil {
				logger.Error(logComponent).Err(err).
					Str("event_type", "LLM_CALL_ERROR").
					Str("method", "ContainerAgent.Invoke").
					Str("agent_id", c.targetCard.ID).
					Msg("目标 Agent 执行错误")
				coordinator.Error(err)
				return map[string]any{}, nil
			}
		}
		if interruptSignal == nil {
			interruptSignal = ExtractInterruptSignal(result, nil)
		}
		signal = ExtractHandoffSignal(result, agentSession)
	}

	if err == nil {
		history = append(history, HandoffHistoryEntry{
			AgentID: targetAgent.Card().ID,
			Output:  result,
		})
		interruptSignal = ExtractInterruptSignal(result, nil)
	}

	// 6. 处理中断信号
	if interruptSignal != nil {
		c.handleTeamInterrupt(ctx, interruptSignal, coordinator, history, req)
		return map[string]any{}, nil
	}

	// 7. 处理交接信号
	if signal == nil {
		coordinator.Complete(result)
	} else {
		allowed := coordinator.RequestHandoff(signal.Target, signal.Reason)
		if allowed {
			nextInput := signal.Message
			if nextInput == "" {
				// 消息为空时使用原始输入
				c.publishHandoff(ctx, req.InputMessage, history, req, signal, sessionID)
			} else {
				c.publishHandoff(ctx, map[string]any{"query": nextInput}, history, req, signal, sessionID)
			}
		} else {
			coordinator.Complete(result)
		}
	}

	return map[string]any{}, nil
}

// Stream 流式调用 ContainerAgent，一次性 yield 完整结果后关闭。
// 实现 BaseAgent 接口。
//
// 对应 Python: ContainerAgent.stream(inputs, session) → yield await self.invoke(...)
func (c *ContainerAgent) Stream(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	result, err := c.Invoke(ctx, inputs, opts...)
	if err != nil {
		return nil, err
	}

	ch := make(chan stream.Schema, 2)
	ch <- &stream.OutputSchema{Payload: result, IsLastSchema: false}
	ch <- &stream.OutputSchema{IsLastSchema: true}
	close(ch)
	return ch, nil
}

// stripHandoffMessages 过滤交接辅助消息，保留有意义的消息。
//
// 过滤规则：
//   - 移除 role="tool" 的消息
//   - 移除包含 tool_calls 的消息
//
// 对应 Python: ContainerAgent._strip_handoff_messages(messages)
func stripHandoffMessages(messages []any) []any {
	var cleaned []any
	for _, msg := range messages {
		msgMap, ok := msg.(map[string]any)
		if !ok {
			cleaned = append(cleaned, msg)
			continue
		}

		// 过滤 role="tool" 的消息
		if role, ok := msgMap["role"]; ok {
			if roleStr, ok := role.(string); ok && roleStr == "tool" {
				continue
			}
		}

		// 过滤包含 tool_calls 的消息
		if toolCalls, ok := msgMap["tool_calls"]; ok {
			switch tc := toolCalls.(type) {
			case []any:
				if len(tc) > 0 {
					continue
				}
			case nil:
				// nil tool_calls，保留
			default:
				// 非空 tool_calls（非 slice 类型），保留
			}
		}

		cleaned = append(cleaned, msg)
	}
	return cleaned
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getTargetAgent 懒初始化目标 Agent。
//
// 对应 Python: ContainerAgent._get_target_agent()
func (c *ContainerAgent) getTargetAgent(ctx context.Context) (agentinterfaces.BaseAgent, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.targetInstance != nil {
		return c.targetInstance, nil
	}

	agent, err := c.targetProvider(ctx, c.targetCard)
	if err != nil {
		logger.Error(logComponent).Err(err).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "ContainerAgent.getTargetAgent").
			Str("agent_id", c.targetCard.ID).
			Msg("目标 Agent 提供者调用失败")
		return nil, err
	}
	c.targetInstance = agent

	logger.Info(logComponent).
		Str("action", "get_target_agent").
		Str("agent_id", c.targetCard.ID).
		Msg("懒初始化目标 Agent")

	return c.targetInstance, nil
}

// injectToolsOnce 一次性注入 HandoffTool 到目标 Agent 的 AbilityManager 和 ResourceMgr。
//
// 对应 Python: ContainerAgent._inject_tools_once(target_agent)
func (c *ContainerAgent) injectToolsOnce(_ context.Context, targetAgent agentinterfaces.BaseAgent) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.toolsInjected {
		return
	}
	c.toolsInjected = true

	// 注入到 AbilityManager
	abilityMgr := targetAgent.AbilityManager()
	if abilityMgr == nil {
		return
	}

	// 获取 ResourceMgr
	resourceMgr := runner.GetResourceMgr()

	// 按 targetID 排序注入
	sortedTargets := make([]string, 0, len(c.allowedTargets))
	for targetID := range c.allowedTargets {
		sortedTargets = append(sortedTargets, targetID)
	}
	sort.Strings(sortedTargets)

	for _, targetID := range sortedTargets {
		// 获取目标 Agent 的描述
		// 对应 Python: card = self._runtime.get_agent_card(target_id) if self._runtime else None
		//              description = card.description if card else ""
		description := ""
		if rt := c.Runtime(); rt != nil {
			if card, cardErr := rt.GetAgentCard(targetID); cardErr == nil && card != nil {
				description = card.Description
			}
		}

		handoffTool := NewHandoffTool(targetID, description)
		abilityMgr.Add(handoffTool.Card())

		if resourceMgr != nil {
			if err := resourceMgr.AddTool(handoffTool, resources_manager.WithTag(resources_manager.Tag(targetAgent.Card().ID))); err != nil {
				logger.Warn(logComponent).Err(err).
					Str("action", "inject_tools_once").
					Str("target_id", targetID).
					Str("agent_id", targetAgent.Card().ID).
					Msg("注入 HandoffTool 到 ResourceMgr 失败")
			}
		}
	}

	logger.Info(logComponent).
		Str("action", "inject_tools_once").
		Str("agent_id", targetAgent.Card().ID).
		Int("injected_count", len(sortedTargets)).
		Msg("交接工具注入完成")
}

// buildAgentInput 构建目标 Agent 的输入，合并交接历史。
//
// 对应 Python: ContainerAgent._build_agent_input(inputs)
func (c *ContainerAgent) buildAgentInput(req *HandoffRequest) map[string]any {
	msg := req.InputMessage
	if len(req.History) == 0 {
		return msg
	}

	// 有历史时，合并 handoff_history
	// 构建历史摘要
	historyData := make([]map[string]any, 0, len(req.History))
	for _, entry := range req.History {
		historyData = append(historyData, map[string]any{
			"agent":  entry.AgentID,
			"output": entry.Output,
		})
	}

	result := make(map[string]any, len(msg)+1)
	for k, v := range msg {
		result[k] = v
	}
	result["handoff_history"] = historyData

	return result
}

// invokeTargetWithStream 在 team session 内调用目标 Agent，处理流式转发和上下文管理。
//
// 对应 Python: ContainerAgent._invoke_target_with_stream(target_agent, agent_input, team_session)
func (c *ContainerAgent) invokeTargetWithStream(
	ctx context.Context,
	targetAgent agentinterfaces.BaseAgent,
	agentInput map[string]any,
	teamSession *session.AgentTeamSession,
) (map[string]any, *HandoffSignal, error) {
	// 创建子 AgentSession
	agentSession := teamSession.CreateAgentSession(targetAgent.Card(), targetAgent.Card().ID, true)

	// 注入上下文历史
	c.injectContextHistory(agentSession, teamSession)

	// 调用目标 Agent
	agentOpts := []agentinterfaces.AgentOption{
		agentinterfaces.WithSession(agentSession),
	}
	result, err := targetAgent.Invoke(ctx, agentInput, agentOpts...)
	if err != nil {
		return nil, nil, err
	}

	// 流式转发到 team session
	c.writeResultToStream(ctx, result, teamSession)

	// 保存上下文
	c.saveAgentContext(ctx, targetAgent, agentSession)

	// 保存上下文历史到 team session
	c.saveContextToTeamSession(agentSession, teamSession)

	// 提取交接信号
	signal := ExtractHandoffSignal(result, agentSession)

	return result, signal, nil
}

// writeResultToStream 将结果写入 team session 的流。
//
// 支持 dict 和 list 两种结果类型：
//   - dict 时直接 WriteStream
//   - list 时逐个 dict 调用 WriteStream
//
// 对应 Python 中 result dict/list 分支写入 write_stream
func (c *ContainerAgent) writeResultToStream(ctx context.Context, result any, teamSession *session.AgentTeamSession) {
	if result == nil || teamSession == nil {
		return
	}

	switch v := result.(type) {
	case map[string]any:
		_ = teamSession.WriteStream(ctx, v)
	case []any:
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				_ = teamSession.WriteStream(ctx, m)
			}
		}
	}
}

// saveAgentContext 保存目标 Agent 的上下文引擎状态。
//
// 通过类型断言探测目标 Agent 是否有 ContextEngine，
// 有则调用 SaveContexts 持久化，无则跳过。
//
// 对应 Python: ContainerAgent._save_agent_context(target_agent, agent_session)
//
//	context_engine = getattr(target_agent, "context_engine", None)
func (c *ContainerAgent) saveAgentContext(ctx context.Context, targetAgent agentinterfaces.BaseAgent, agentSession *session.Session) {
	if targetAgent == nil || agentSession == nil {
		return
	}

	// 类型断言探测目标 Agent 是否有 ContextEngine
	// 对应 Python: context_engine = getattr(target_agent, "context_engine", None)
	type contextEngineHolder interface {
		ContextEngine() ceinterface.ContextEngine
	}
	holder, ok := targetAgent.(contextEngineHolder)
	if !ok {
		return
	}

	ce := holder.ContextEngine()
	if ce == nil {
		return
	}

	if _, err := ce.SaveContexts(ctx, agentSession, nil); err != nil {
		logger.Warn(logComponent).Err(err).
			Str("action", "save_agent_context").
			Str("agent_id", targetAgent.Card().ID).
			Msg("保存 Agent 上下文失败")
	}
}

// saveContextToTeamSession 将 Agent 上下文历史保存到 team session。
//
// 对应 Python: ContainerAgent._save_context_to_team_session(agent_session, team_session)
func (c *ContainerAgent) saveContextToTeamSession(agentSession sessioninterfaces.SessionFacade, teamSession *session.AgentTeamSession) {
	if agentSession == nil || teamSession == nil {
		return
	}

	// 获取 agent session 的 context 状态
	ctxState, err := agentSession.GetState(stateKeyContext)
	if err != nil || ctxState == nil {
		return
	}
	ctxStateMap, ok := ctxState.(map[string]any)
	if !ok {
		return
	}

	// 获取默认上下文的消息列表
	defaultCtx, ok := ctxStateMap[defaultContextID]
	if !ok {
		return
	}
	defaultCtxMap, ok := defaultCtx.(map[string]any)
	if !ok {
		return
	}

	newMessagesVal, ok := defaultCtxMap["messages"]
	if !ok {
		return
	}
	newMessages, ok := newMessagesVal.([]any)
	if !ok || len(newMessages) == 0 {
		return
	}

	// 过滤交接辅助消息
	cleaned := stripHandoffMessages(newMessages)
	if len(cleaned) == 0 {
		return
	}

	// 获取已有的上下文历史，去重追加
	existingVal, _ := teamSession.GetState(state.StringKey(contextHistoryKey))
	var existing []any
	if existingVal != nil {
		if e, ok := existingVal.([]any); ok {
			existing = e
		}
	}

	existingKeys := make(map[string]struct{}, len(existing))
	for _, m := range existing {
		if key := msgKey(m); key != "" {
			existingKeys[key] = struct{}{}
		}
	}

	var toAppend []any
	for _, m := range cleaned {
		if key := msgKey(m); key != "" {
			if _, found := existingKeys[key]; !found {
				toAppend = append(toAppend, m)
			}
		}
	}

	if len(toAppend) > 0 {
		updated := append(existing, toAppend...)
		teamSession.UpdateState(map[string]any{contextHistoryKey: updated})
	}
}

// injectContextHistory 将 team session 的上下文历史注入到 agent session。
//
// 对应 Python: ContainerAgent._inject_context_history(agent_session, team_session)
func (c *ContainerAgent) injectContextHistory(agentSession sessioninterfaces.SessionFacade, teamSession *session.AgentTeamSession) {
	if agentSession == nil || teamSession == nil {
		return
	}

	historyVal, _ := teamSession.GetState(state.StringKey(contextHistoryKey))
	if historyVal == nil {
		return
	}
	historyMessages, ok := historyVal.([]any)
	if !ok || len(historyMessages) == 0 {
		return
	}

	// 复制历史消息
	copied := make([]any, len(historyMessages))
	copy(copied, historyMessages)

	agentSession.UpdateState(map[string]any{
		"context": map[string]any{
			defaultContextID: map[string]any{
				"messages":         copied,
				"offload_messages": map[string]any{},
			},
		},
	})
}

// handleTeamInterrupt 处理团队中断信号。
//
// 对应 Python: ContainerAgent._handle_team_interrupt(signal, coordinator, history, inputs)
func (c *ContainerAgent) handleTeamInterrupt(
	ctx context.Context,
	signal *TeamInterruptSignal,
	coordinator *HandoffOrchestrator,
	history []HandoffHistoryEntry,
	req *HandoffRequest,
) {
	if req.Session != nil {
		coordinator.SaveToSession(req.Session)
		req.Session.UpdateState(map[string]any{
			HandoffHistoryKey: history,
		})
		_ = FlushTeamSession(ctx, req.Session)
	}
	coordinator.Complete(signal.Result)
}

// publishHandoff 发布交接消息到下一个 ContainerAgent。
//
// 对应 Python: ContainerAgent._publish_handoff(next_input, history, signal, session_id)
func (c *ContainerAgent) publishHandoff(
	ctx context.Context,
	inputMessage map[string]any,
	history []HandoffHistoryEntry,
	req *HandoffRequest,
	signal *HandoffSignal,
	sessionID string,
) {
	nextReq := &HandoffRequest{
		InputMessage: inputMessage,
		History:      history,
		Session:      req.Session,
	}

	if !c.IsBound() {
		logger.Warn(logComponent).
			Str("action", "publish_handoff").
			Str("target_id", signal.Target).
			Str("session_id", sessionID).
			Msg("CommunicableAgent 未绑定运行时，无法发布交接消息")
		return
	}

	topicID := containerTopicPrefix + signal.Target
	if err := c.Publish(ctx, map[string]any{"handoff_request": nextReq}, topicID,
		maschema.WithTeamSessionID(sessionID),
	); err != nil {
		logger.Warn(logComponent).Err(err).
			Str("action", "publish_handoff").
			Str("target_id", signal.Target).
			Str("topic_id", topicID).
			Str("session_id", sessionID).
			Msg("发布交接请求失败")
	}

	logger.Info(logComponent).
		Str("action", "publish_handoff").
		Str("target_id", signal.Target).
		Str("reason", signal.Reason).
		Str("session_id", sessionID).
		Msg("发布交接消息到下一个 ContainerAgent")
}

// msgKey 生成消息的去重键，基于 role + content + tool_calls + tool_call_id。
//
// 对应 Python: _msg_key(m) = (role, str(content), str(tool_calls), tool_call_id)
func msgKey(msg any) string {
	msgMap, ok := msg.(map[string]any)
	if !ok {
		return ""
	}

	role := ""
	if r, ok := msgMap["role"]; ok {
		if rs, ok := r.(string); ok {
			role = rs
		}
	}

	content := ""
	if c, ok := msgMap["content"]; ok {
		if cs, ok := c.(string); ok {
			content = cs
		}
	}

	// 对应 Python: str(getattr(m, "tool_calls", ""))
	toolCallsStr := ""
	if tc, ok := msgMap["tool_calls"]; ok {
		toolCallsStr = fmt.Sprintf("%v", tc)
	}

	// 对应 Python: getattr(m, "tool_call_id", "")
	toolCallID := ""
	if tci, ok := msgMap["tool_call_id"]; ok {
		if s, ok := tci.(string); ok {
			toolCallID = s
		}
	}

	return role + ":" + content + ":" + toolCallsStr + ":" + toolCallID
}
