package server

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions"
	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// keepaliveInterval 流式心跳间隔
	keepaliveInterval = 10 * time.Second
	// acpChannelID ACP 通道标识
	acpChannelID = "acp"
	// errCodeNotImplemented 未实现错误码
	errCodeNotImplemented = "NOT_IMPLEMENTED"
	// errMsgNotImplemented 未实现错误消息
	errMsgNotImplemented = "方法尚未实现"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleEnvelope 处理单个 E2A 请求信封，对齐 Python AgentWebSocketServer._handle_message。
//
// 流程：E2AEnvelope → AgentRequest → switch 分发 → writeResponse 写入 RecvCh。
func (s *AgentServer) handleEnvelope(ctx context.Context, envelope *e2a.E2AEnvelope) {
	// 1. 转为 AgentRequest
	request, err := e2a.E2AToAgentRequest(envelope)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("request_id", envelope.RequestID).
			Msg("E2AEnvelope to AgentRequest conversion failed")
		s.writeErrorResponse(envelope.RequestID, envelope.Channel, err.Error(), "E2A_CONVERT_ERROR")
		return
	}

	// 2. ACP channel 特殊处理：注入 client_capabilities 到 metadata（排除 INITIALIZE 方法）
	if request.ChannelID == acpChannelID && request.ReqMethod != schema.ReqMethodInitialize {
		s.injectACPCapabilities(request, envelope)
	}

	// 3. before_chat_request 钩子（对齐 Python: agent_ws_server.py _trigger_before_chat_request_hook L1042-1061）
	// 当 req_method 为 CHAT_SEND/CHAT_RESUME/CHAT_ANSWER 时触发；
	// 扩展可修改 params，FromMap 写回后重新序列化进 request.Params。
	// params 非 dict（JSON 数组/字符串等）时归一化为空 map 并强制写回（对齐 Python L1044-1046）。
	if request.ReqMethod == schema.ReqMethodChatSend ||
		request.ReqMethod == schema.ReqMethodChatResume ||
		request.ReqMethod == schema.ReqMethodChatAnswer {
		extReg, extErr := extensions.GetInstanceErr()
		if extErr == nil && extReg != nil {
			reqMethodStr := string(request.ReqMethod)
			var params map[string]any
			if len(request.Params) > 0 {
				_ = json.Unmarshal(request.Params, &params) // 延迟解析 params
			}
			if params == nil {
				// 非 dict 时清洗为空 map（对齐 Python: request.params = {}）
				params = map[string]any{}
				if raw, err := json.Marshal(params); err == nil {
					request.Params = raw
				}
			}
			hookCtx := &extensions.AgentServerChatHookContext{
				RequestID: request.RequestID,
				ChannelID: request.ChannelID,
				SessionID: request.SessionID,
				ReqMethod: &reqMethodStr,
				Params:    params,
			}
			data := hookCtx.ToMap()
			extReg.Trigger(ctx, extensions.AgentServerBeforeChatRequest, data)
			hookCtx.FromMap(data)
			if hookCtx.Params != nil {
				if raw, err := json.Marshal(hookCtx.Params); err == nil {
					request.Params = raw
				}
			}
		}
	}

	// 4. 按 request.ReqMethod switch 分发
	var resp *schema.AgentResponse
	switch request.ReqMethod {
	// Session 管理
	case schema.ReqMethodSessionList:
		resp, err = s.handleSessionList(ctx, request)
	case schema.ReqMethodSessionRename:
		resp, err = s.handleSessionRename(ctx, request)
	case schema.ReqMethodSessionSwitch:
		resp, err = s.handleSessionSwitch(ctx, request)
	case schema.ReqMethodSessionDelete:
		resp, err = s.handleSessionDelete(ctx, request)
	case schema.ReqMethodSessionRewind:
		resp, err = s.handleSessionRewind(ctx, request)
	case schema.ReqMethodSessionRewindAndRestore:
		resp, err = s.handleSessionRewindAndRestore(ctx, request)
	case schema.ReqMethodSessionRewindContext:
		resp, err = s.handleSessionRewindContext(ctx, request)
	case schema.ReqMethodHistoryListTurns:
		resp, err = s.handleHistoryListTurns(ctx, request)
	case schema.ReqMethodSessionRestoreFiles:
		resp, err = s.handleSessionRestoreFiles(ctx, request)
	// 团队
	case schema.ReqMethodTeamDelete:
		resp, err = s.handleTeamDelete(ctx, request)
	case schema.ReqMethodTeamSnapshot:
		resp, err = s.handleTeamSnapshot(ctx, request)
	case schema.ReqMethodTeamHistoryGet:
		resp, err = s.handleTeamHistoryGet(ctx, request)
	// 历史
	case schema.ReqMethodHistoryGet:
		if request.IsStream {
			chunks, chunkErr := s.handleHistoryGetStream(ctx, request)
			if chunkErr != nil {
				err = chunkErr
			} else {
				for i, chunk := range chunks {
					s.writeChunk(request.RequestID, request.ChannelID, chunk, i+1, true)
				}
				resp = nil // 流式已通过 writeChunk 发送
			}
		} else {
			resp, err = s.handleHistoryGet(ctx, request)
		}
	// 命令
	case schema.ReqMethodCommandAddDir:
		resp, err = s.handleCommandAddDir(ctx, request)
	case schema.ReqMethodCommandChrome:
		resp, err = s.handleCommandChrome(ctx, request)
	case schema.ReqMethodCommandCompact:
		resp, err = s.handleCommandCompact(ctx, request)
	case schema.ReqMethodCommandContext:
		resp, err = s.handleCommandContext(ctx, request)
	case schema.ReqMethodCommandRecap:
		resp, err = s.handleCommandRecap(ctx, request)
	case schema.ReqMethodCommandDiff:
		resp, err = s.handleCommandDiff(ctx, request)
	case schema.ReqMethodCommandModel:
		resp, err = s.handleCommandModel(ctx, request)
	case schema.ReqMethodCommandMCP:
		resp, err = s.handleCommandMCP(ctx, request)
	case schema.ReqMethodCommandSandbox:
		resp, err = s.handleCommandSandbox(ctx, request)
	case schema.ReqMethodCommandResume:
		resp, err = s.handleCommandResume(ctx, request)
	case schema.ReqMethodCommandSession:
		resp, err = s.handleCommandSession(ctx, request)
	case schema.ReqMethodCommandStatus:
		resp, err = s.handleCommandStatus(ctx, request)
	// 浏览器
	case schema.ReqMethodBrowserStart:
		resp, err = s.handleBrowserStart(ctx, request)
	case schema.ReqMethodBrowserRuntimeRestart:
		resp, err = s.handleBrowserRuntimeRestart(ctx, request)
	// 配置/Agent 重载
	case schema.ReqMethodConfigCacheClear:
		resp, err = s.handleConfigCacheClear(ctx, request)
	case schema.ReqMethodAgentReloadConfig:
		resp, err = s.handleAgentReloadConfig(ctx, request)
	// 扩展/钩子
	case schema.ReqMethodExtensionsList:
		resp, err = s.handleExtensionsList(ctx, request)
	case schema.ReqMethodExtensionsImport:
		resp, err = s.handleExtensionsImport(ctx, request)
	case schema.ReqMethodExtensionsDelete:
		resp, err = s.handleExtensionsDelete(ctx, request)
	case schema.ReqMethodExtensionsToggle:
		resp, err = s.handleExtensionsToggle(ctx, request)
	case schema.ReqMethodHooksList:
		resp, err = s.handleHooksList(ctx, request)
	// 编排器
	case schema.ReqMethodHarnessPackagesGet:
		resp, err = s.handleHarnessPackagesGet(ctx, request)
	case schema.ReqMethodHarnessPackagesScan:
		resp, err = s.handleHarnessPackagesScan(ctx, request)
	case schema.ReqMethodHarnessPackagesActivate:
		resp, err = s.handleHarnessPackagesActivate(ctx, request)
	case schema.ReqMethodHarnessPackagesDeactivate:
		resp, err = s.handleHarnessPackagesDeactivate(ctx, request)
	case schema.ReqMethodHarnessPackagesDelete:
		resp, err = s.handleHarnessPackagesDelete(ctx, request)
	// 调度
	case schema.ReqMethodScheduleCheckConfig:
		resp, err = s.handleScheduleCheckConfig(ctx, request)
	case schema.ReqMethodScheduleUpdateConfig:
		resp, err = s.handleScheduleUpdateConfig(ctx, request)
	case schema.ReqMethodScheduleCreate:
		resp, err = s.handleScheduleCreate(ctx, request)
	case schema.ReqMethodScheduleRun:
		resp, err = s.handleScheduleRun(ctx, request)
	case schema.ReqMethodScheduleList:
		resp, err = s.handleScheduleList(ctx, request)
	case schema.ReqMethodScheduleStatus:
		resp, err = s.handleScheduleStatus(ctx, request)
	case schema.ReqMethodScheduleLogs:
		resp, err = s.handleScheduleLogs(ctx, request)
	case schema.ReqMethodScheduleCancel:
		resp, err = s.handleScheduleCancel(ctx, request)
	case schema.ReqMethodScheduleDelete:
		resp, err = s.handleScheduleDelete(ctx, request)
	// Agent管理
	case schema.ReqMethodAgentsList:
		resp, err = s.handleAgentsList(ctx, request)
	case schema.ReqMethodAgentsGet:
		resp, err = s.handleAgentsGet(ctx, request)
	case schema.ReqMethodAgentsCreate:
		resp, err = s.handleAgentsCreate(ctx, request)
	case schema.ReqMethodAgentsUpdate:
		resp, err = s.handleAgentsUpdate(ctx, request)
	case schema.ReqMethodAgentsDelete:
		resp, err = s.handleAgentsDelete(ctx, request)
	case schema.ReqMethodAgentsEnable:
		resp, err = s.handleAgentsEnable(ctx, request)
	case schema.ReqMethodAgentsDisable:
		resp, err = s.handleAgentsDisable(ctx, request)
	case schema.ReqMethodAgentsToolsList:
		resp, err = s.handleAgentsToolsList(ctx, request)
	// Permissions（10 个）
	case schema.ReqMethodPermissionsToolsGet, schema.ReqMethodPermissionsToolsSet,
		schema.ReqMethodPermissionsToolsUpdate, schema.ReqMethodPermissionsToolsDelete,
		schema.ReqMethodPermissionsRulesGet, schema.ReqMethodPermissionsRulesCreate,
		schema.ReqMethodPermissionsRulesUpdate, schema.ReqMethodPermissionsRulesDelete,
		schema.ReqMethodPermissionsApprovalOverridesGet, schema.ReqMethodPermissionsApprovalOverridesDelete:
		resp, err = s.handlePermissionsConfig(ctx, request)
	// 聊天中断
	case schema.ReqMethodChatCancel:
		s.handleCancel(ctx, request)
		return // handleCancel 自己写响应
	// 兜底
	default:
		if request.IsStream {
			s.handleStream(ctx, request)
		} else {
			s.handleUnary(ctx, request)
		}
		return // handleUnary/handleStream 自己写响应
	}

	// 5. handler 返回 resp → writeResponse 写入 RecvCh
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("request_id", request.RequestID).
			Str("method", string(request.ReqMethod)).
			Msg("handler returned error")
		s.writeErrorResponse(request.RequestID, request.ChannelID, err.Error(), "HANDLER_ERROR")
		return
	}
	if resp != nil {
		s.writeResponse(request.RequestID, request.ChannelID, resp)
	}
}

// handleUnary 处理非流式请求，对齐 Python _handle_unary。
//
// 特殊方法拦截 → 解析 mode → 获取 Agent → 调用 ProcessMessage → writeResponse。
// Python: handleUnary 通过 AgentManager 获取 agent，然后直接调用 agent 方法。
func (s *AgentServer) handleUnary(ctx context.Context, request *schema.AgentRequest) {
	// 1. 特殊方法拦截
	switch request.ReqMethod {
	case schema.ReqMethodInitialize:
		resp, err := s.handleInitialize(ctx, request)
		if err != nil {
			s.writeErrorResponse(request.RequestID, request.ChannelID, err.Error(), "INITIALIZE_ERROR")
			return
		}
		s.writeResponse(request.RequestID, request.ChannelID, resp)
		return
	case schema.ReqMethodSessionCreate:
		resp, err := s.handleSessionCreate(ctx, request)
		if err != nil {
			s.writeErrorResponse(request.RequestID, request.ChannelID, err.Error(), "SESSION_CREATE_ERROR")
			return
		}
		s.writeResponse(request.RequestID, request.ChannelID, resp)
		return
	case schema.ReqMethodSessionFork:
		resp, err := s.handleSessionFork(ctx, request)
		if err != nil {
			s.writeErrorResponse(request.RequestID, request.ChannelID, err.Error(), "SESSION_FORK_ERROR")
			return
		}
		s.writeResponse(request.RequestID, request.ChannelID, resp)
		return
	case schema.ReqMethodACPToolResponse:
		resp, err := s.handleACPToolResponse(ctx, request)
		if err != nil {
			s.writeErrorResponse(request.RequestID, request.ChannelID, err.Error(), "ACP_TOOL_RESPONSE_ERROR")
			return
		}
		s.writeResponse(request.RequestID, request.ChannelID, resp)
		return
	}

	// 2-6. 交给 AgentRuntime（选 agent → ensure_state → 执行 → check_exit → 控制事件）
	// Python: agent_ws_server.py:3941 _handle_unary_impl → runtime.invoke(...)
	events, err := s.agentRuntime.Invoke(ctx, request)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("request_id", request.RequestID).
			Msg("agent failed to process request")
		s.writeErrorResponse(request.RequestID, request.ChannelID, err.Error(), "AGENT_PROCESS_ERROR")
		return
	}
	for _, ev := range events {
		if ev.Payload == nil {
			continue
		}
		s.writeResponse(request.RequestID, request.ChannelID, &schema.AgentResponse{
			RequestID: ev.RequestID,
			ChannelID: ev.ChannelID,
			OK:        ev.OK,
			Payload:   ev.Payload,
		})
	}
}

// handleStream 处理流式请求，对齐 Python _handle_stream。
//
// 创建子 context → 注册流式任务 → 心跳 goroutine → 获取 Agent → 逐 chunk 写入 RecvCh。
// Python: handleStream 通过 AgentManager 获取 agent，然后直接调用 agent 流式方法。
func (s *AgentServer) handleStream(ctx context.Context, request *schema.AgentRequest) {
	sessionID := ""
	if request.SessionID != nil {
		sessionID = *request.SessionID
	}

	// 1-3. 创建子 context + 注册流式任务，收尾统一走 finish
	// （先按身份清理登记项，再 cancel 回收本次子 ctx，见 beginStreamTask）。
	streamCtx, finish := s.beginStreamTask(ctx, sessionID, request.RequestID)
	defer finish()

	// 3-8. 交给 AgentRuntime（选 agent → ensure_state → 流式执行 → check_exit → 控制事件）
	// Python: agent_ws_server.py:4654 _handle_stream_impl → runtime.stream(...)
	heartbeatDone := make(chan struct{})
	heartbeatTrigger := make(chan struct{}, 1)
	go s.runKeepalive(streamCtx, request.RequestID, request.ChannelID, heartbeatDone, heartbeatTrigger)

	chunkCount := 0
	streamErr := s.agentRuntime.Stream(streamCtx, request, func(ev *swruntime.RuntimeEvent) bool {
		// 控制事件独立走 push 帧，不占 sequence、不重置 keepalive
		// Python: agent_ws_server.py:4714 `if event.event_type == PLAN_MODE_EXITED_EVENT_TYPE`
		if ev.EventType() == swruntime.PlanModeExitedEvent {
			_ = s.SendPush(streamCtx, map[string]any{
				"channel_id": ev.ChannelID,
				"session_id": ev.SessionID,
				"payload":    ev.Payload,
			})
			return true
		}
		chunkCount++
		select {
		case heartbeatTrigger <- struct{}{}:
		default:
		}
		s.writeChunk(request.RequestID, request.ChannelID, ev.ToChunk(), chunkCount, true)
		return true
	})

	close(heartbeatTrigger)
	<-heartbeatDone

	if streamErr != nil {
		logger.Error(logComponent).
			Err(streamErr).
			Str("request_id", request.RequestID).
			Msg("agent stream processing failed")
		s.writeErrorResponse(request.RequestID, request.ChannelID, streamErr.Error(), "AGENT_STREAM_ERROR")
		return
	}

	logger.Debug(logComponent).
		Str("request_id", request.RequestID).
		Int("chunk_count", chunkCount).
		Msg("stream processing completed")
}

// handleCancel 处理取消/中断请求，对齐 Python _handle_cancel。
//
// 三级 fallback 策略（对齐 Python _handle_cancel L1061-1110）：
// 1. GetAgentNoWait(channelID, mode) — 按 mode 查找已有 agent
// 2. GetAgentNoWait(channelID, "", projectDir) — 不限 mode，按 projectDir 查任何已有 agent
// 3. GetAgent(channelID, mode, projectDir) — 阻塞创建（fallback 兜底）
func (s *AgentServer) handleCancel(ctx context.Context, request *schema.AgentRequest) {
	sessionID := ""
	if request.SessionID != nil {
		sessionID = *request.SessionID
	}

	// 1. 取消流式 goroutine（对齐 Python：仅 cancel/supplement 时取消流式任务，pause/resume 不取消）
	// 提取 intent，对齐 Python agent_ws_server.py L983-1001: if intent in ("cancel", "supplement"): stream_task.cancel()
	// intent 为空时保持向后兼容，默认取消
	intent := ""
	var params map[string]any
	if request.Params != nil {
		if json.Unmarshal(request.Params, &params) == nil {
			if i, ok := params["intent"].(string); ok {
				intent = i
			}
		}
	}
	if intent == "cancel" || intent == "supplement" || intent == "" {
		s.cancelStreamTask(sessionID)
	}

	// 2. 获取 Agent（三级 fallback，对齐 Python _handle_cancel）
	// 主流路径：纯读取 mode（不修改 request），对齐 Python request.params.get("mode", "") + resolve_agent_request_mode
	mode, subMode := swruntime.ResolveAgentRequestModeFromRequest(request)
	projectDir := swruntime.ResolveCancelProjectDir(request)

	var agent *runtime.UapClaw

	// 第1级：按 mode 查找已有 agent
	if mode != "" {
		agent = s.agentManager.GetAgentNoWait(request.ChannelID, mode, projectDir, subMode)
	}

	// 第2级：按 projectDir 查找任何已有 agent（不限 mode）
	if agent == nil {
		agent = s.agentManager.GetAgentNoWait(request.ChannelID, "", projectDir, "")
		logger.Warn(logComponent).
			Str("request_id", request.RequestID).
			Str("channel_id", request.ChannelID).
			Str("mode", mode).
			Msg("cancel: no existing agent by mode, trying without mode filter")
	}

	// 第3级：fallback 创建（阻塞），对齐 Python _apply_resolved_mode_to_request 兜底
	// 此时才回写 canonical mode（上游 apply_resolved_mode_to_request 会改 params）
	if agent == nil {
		logger.Warn(logComponent).
			Str("request_id", request.RequestID).
			Str("channel_id", request.ChannelID).
			Msg("cancel: no existing agent found, fallback creation")
		mode, subMode = swruntime.ApplyResolvedModeToRequest(request, "")
		var err error
		agent, err = s.agentManager.GetAgent(ctx, request.ChannelID, mode, projectDir, subMode)
		if err != nil {
			logger.Error(logComponent).
				Err(err).
				Str("request_id", request.RequestID).
				Msg("cancel: fallback agent creation failed")
			s.writeErrorResponse(request.RequestID, request.ChannelID, err.Error(), "AGENT_GET_ERROR")
			return
		}
	}

	// 3. 调用 ProcessMessage（对齐 Python: agent.process_message(request) → CHAT_CANCEL 分支 → _process_interrupt）
	resp, err := agent.ProcessMessage(ctx, request)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("request_id", request.RequestID).
			Msg("agent interrupt processing failed")
		s.writeErrorResponse(request.RequestID, request.ChannelID, err.Error(), "AGENT_INTERRUPT_ERROR")
		return
	}
	s.writeResponse(request.RequestID, request.ChannelID, resp)
}

// writeResponse 构造 E2AResponse wire 通过 sendToGateway 发送。
func (s *AgentServer) writeResponse(requestID, channelID string, resp *schema.AgentResponse) {
	wire := e2a.EncodeAgentResponseForWire(resp, requestID, 0)
	data, err := json.Marshal(wire)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("request_id", requestID).Msg("response JSON coding failed")
		return
	}
	s.sendToGateway(data)
}

// writeChunk 构造 E2AResponse chunk wire 通过 sendToGateway 发送。
func (s *AgentServer) writeChunk(requestID, channelID string, chunk *schema.AgentResponseChunk, sequence int, isStream bool) {
	wire := e2a.EncodeAgentChunkForWire(chunk, requestID, sequence, isStream)
	data, err := json.Marshal(wire)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("request_id", requestID).Int("sequence", sequence).Msg("streaming chunk JSON encode failed")
		return
	}
	s.sendToGateway(data)
}

// sendKeepalive 构造 keepalive chunk 通过 sendToGateway 发送。
func (s *AgentServer) sendKeepalive(requestID, channelID string) {
	chunk := schema.NewAgentResponseChunk(requestID, channelID,
		map[string]any{
			"event_type": "keepalive",
		},
	)
	wire := e2a.EncodeAgentChunkForWire(chunk, requestID, -1, true)
	data, err := json.Marshal(wire)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("request_id", requestID).Msg("keepalive JSON coding failed")
		return
	}
	s.sendToGateway(data)
}

// runKeepalive 运行心跳 goroutine，每 keepaliveInterval 发送一次 keepalive。
// 收到 heartbeatTrigger 信号时重置计时器。关闭 heartbeatTrigger 时退出。
func (s *AgentServer) runKeepalive(ctx context.Context, requestID, channelID string, done chan<- struct{}, trigger <-chan struct{}) {
	defer close(done)
	timer := time.NewTimer(keepaliveInterval)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-trigger:
			if !ok {
				// trigger 通道关闭，退出
				return
			}
			// 收到真实数据，重置计时器
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(keepaliveInterval)
		case <-timer.C:
			// 发送 keepalive
			s.sendKeepalive(requestID, channelID)
			timer.Reset(keepaliveInterval)
		}
	}
}

// writeErrorResponse 构造错误 AgentResponse 写入 RecvCh。
func (s *AgentServer) writeErrorResponse(requestID, channelID, errMsg, code string) {
	resp := schema.NewAgentResponse(requestID, channelID,
		schema.WithResponseOK(false),
		schema.WithPayload(map[string]any{
			"error": map[string]any{
				"code":    code,
				"message": errMsg,
			},
		}),
	)
	s.writeResponse(requestID, channelID, resp)
}

// injectACPCapabilities 为 ACP 通道注入客户端能力到 metadata。
// 使用 setdefault 语义：不覆盖已有 acp_client_capabilities 值。
//
// Python: jiuwenswarm/server/agent_ws_server.py:803-810
// Python: metadata.setdefault("acp_client_capabilities",
//
//	ws_caps or agent_manager.get_client_capabilities("acp"))
//
// ws 层能力缓存经 envelope.Params["client_capabilities"] 等价传入
// （Go ChannelTransport 无 per-ws 状态，能力由请求参数携带）。
func (s *AgentServer) injectACPCapabilities(request *schema.AgentRequest, envelope *e2a.E2AEnvelope) {
	if request.Metadata == nil {
		request.Metadata = make(map[string]any)
	}
	// setdefault 语义：已有 acp_client_capabilities 则不覆盖
	if _, exists := request.Metadata["acp_client_capabilities"]; exists {
		return
	}
	if envelope.Params != nil {
		if caps, ok := envelope.Params["client_capabilities"]; ok {
			request.Metadata["acp_client_capabilities"] = caps
			return
		}
	}
	// fallback 到 agent_manager.get_client_capabilities("acp")
	if s.agentManager != nil {
		if caps := s.agentManager.GetClientCapabilities("acp"); len(caps) > 0 {
			request.Metadata["acp_client_capabilities"] = caps
		}
	}
}

// notImplementedResponse 返回 NOT_IMPLEMENTED 错误响应（供 stub handler 使用）。
func notImplementedResponse(request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(false),
		schema.WithPayload(map[string]any{
			"error": map[string]any{
				"code":    errCodeNotImplemented,
				"message": fmt.Sprintf("%s: %s", errMsgNotImplemented, request.ReqMethod),
			},
		}),
	), nil
}
