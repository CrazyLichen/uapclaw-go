package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// keepaliveInterval 流式心跳间隔
	keepaliveInterval = 10 * time.Second
	// defaultMode 默认模式
	defaultMode = "agent"
	// defaultSubMode 默认子模式
	defaultSubMode = "plan"
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
			Msg("E2AEnvelope 转换 AgentRequest 失败")
		s.writeErrorResponse(envelope.RequestID, envelope.Channel, err.Error(), "E2A_CONVERT_ERROR")
		return
	}

	// 2. ACP channel 特殊处理：注入 client_capabilities 到 metadata
	if request.ChannelID == acpChannelID {
		s.injectACPCapabilities(request, envelope)
	}

	// 3. before_chat_request hook（暂不实现，后续补充）

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
	// Team
	case schema.ReqMethodTeamDelete:
		resp, err = s.handleTeamDelete(ctx, request)
	case schema.ReqMethodTeamSnapshot:
		resp, err = s.handleTeamSnapshot(ctx, request)
	case schema.ReqMethodTeamHistoryGet:
		resp, err = s.handleTeamHistoryGet(ctx, request)
	// History
	case schema.ReqMethodHistoryGet:
		resp, err = s.handleHistoryGet(ctx, request)
	// Command
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
	// Browser
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
	// Harness
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
	// Schedule
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
	// Agents
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
			Msg("handler 返回错误")
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

	// 2. 解析 mode
	mode, subMode := applyResolvedModeToRequest(request)
	projectDir := resolveRequestProjectDir(request)

	// 3. 获取 Agent
	agent, err := s.agentManager.GetAgent(request.ChannelID, mode, projectDir, subMode)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("request_id", request.RequestID).
			Str("channel_id", request.ChannelID).
			Str("mode", mode).
			Msg("获取 Agent 失败")
		s.writeErrorResponse(request.RequestID, request.ChannelID, err.Error(), "GET_AGENT_ERROR")
		return
	}

	// 4. code 模式 switchMode（stub，不报错）
	if mode == "code" {
		_ = agent.SwitchMode(mode, subMode)
	}

	// 5. 调用 Agent
	resp, err := agent.ProcessMessage(ctx, request)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("request_id", request.RequestID).
			Str("mode", mode).
			Msg("Agent 处理请求失败")
		s.writeErrorResponse(request.RequestID, request.ChannelID, err.Error(), "AGENT_PROCESS_ERROR")
		return
	}

	// 6. writeResponse 写入 RecvCh
	s.writeResponse(request.RequestID, request.ChannelID, resp)
}

// handleStream 处理流式请求，对齐 Python _handle_stream。
//
// 创建子 context → 注册流式任务 → 心跳 goroutine → 逐 chunk 读取 → writeChunk 写入 RecvCh。
func (s *AgentServer) handleStream(ctx context.Context, request *schema.AgentRequest) {
	sessionID := ""
	if request.SessionID != nil {
		sessionID = *request.SessionID
	}

	// 1. 创建子 context + cancel
	streamCtx, cancel := context.WithCancel(ctx)

	// 2. 注册流式任务
	s.registerStreamTask(sessionID, cancel)
	defer s.cancelStreamTask(sessionID)

	// 3. 解析 mode + 获取 Agent
	mode, subMode := applyResolvedModeToRequest(request)
	projectDir := resolveRequestProjectDir(request)

	agent, err := s.agentManager.GetAgent(request.ChannelID, mode, projectDir, subMode)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("request_id", request.RequestID).
			Str("channel_id", request.ChannelID).
			Str("mode", mode).
			Msg("流式请求获取 Agent 失败")
		s.writeErrorResponse(request.RequestID, request.ChannelID, err.Error(), "GET_AGENT_ERROR")
		return
	}

	// 4. code 模式 switchMode
	if mode == "code" {
		_ = agent.SwitchMode(mode, subMode)
	}

	// 5. 启动心跳 goroutine
	heartbeatDone := make(chan struct{})
	heartbeatTrigger := make(chan struct{}, 1)
	go s.runKeepalive(streamCtx, request.RequestID, request.ChannelID, heartbeatDone, heartbeatTrigger)

	// 6. 调用 Agent 流式
	ch, err := agent.ProcessMessageStream(streamCtx, request)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("request_id", request.RequestID).
			Str("mode", mode).
			Msg("Agent 流式处理失败")
		// 停止心跳
		close(heartbeatTrigger)
		<-heartbeatDone
		s.writeErrorResponse(request.RequestID, request.ChannelID, err.Error(), "AGENT_STREAM_ERROR")
		return
	}

	// 7. 逐 chunk 从 ch 读取
	chunkCount := 0
	for chunk := range ch {
		chunkCount++
		// 通知心跳有真实数据
		select {
		case heartbeatTrigger <- struct{}{}:
		default:
		}
		// writeChunk 写入 RecvCh
		s.writeChunk(request.RequestID, request.ChannelID, chunk, chunkCount, true)
	}

	// 8. 停止心跳，清理
	close(heartbeatTrigger)
	<-heartbeatDone

	logger.Debug(logComponent).
		Str("request_id", request.RequestID).
		Int("chunk_count", chunkCount).
		Msg("流式处理完成")
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

	// 1. 取消流式 goroutine
	s.cancelStreamTask(sessionID)

	// 2. 获取 Agent（三级 fallback，对齐 Python _handle_cancel）
	mode, subMode := applyResolvedModeToRequest(request)
	projectDir := resolveRequestProjectDir(request)

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
			Msg("cancel: 按 mode 未找到已有 agent，尝试不限 mode 查找")
	}

	// 第3级：fallback 创建（阻塞），对齐 Python get_agent 兜底
	if agent == nil {
		logger.Warn(logComponent).
			Str("request_id", request.RequestID).
			Str("channel_id", request.ChannelID).
			Msg("cancel: 未找到已有 agent，fallback 创建")
		var err error
		agent, err = s.agentManager.GetAgent(request.ChannelID, mode, projectDir, subMode)
		if err != nil {
			logger.Error(logComponent).
				Err(err).
				Str("request_id", request.RequestID).
				Msg("cancel: fallback 创建 agent 失败")
			s.writeErrorResponse(request.RequestID, request.ChannelID, err.Error(), "AGENT_GET_ERROR")
			return
		}
	}

	// 3. 调用 ProcessInterrupt
	resp, err := agent.ProcessInterrupt(ctx, request)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("request_id", request.RequestID).
			Msg("Agent 中断处理失败")
		s.writeErrorResponse(request.RequestID, request.ChannelID, err.Error(), "AGENT_INTERRUPT_ERROR")
		return
	}
	s.writeResponse(request.RequestID, request.ChannelID, resp)
}

// writeResponse 构造 E2AResponse wire 写入 RecvCh。
func (s *AgentServer) writeResponse(requestID, channelID string, resp *schema.AgentResponse) {
	wire := e2a.EncodeAgentResponseForWire(resp, requestID, 0)
	data, err := json.Marshal(wire)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("request_id", requestID).Msg("响应 JSON 编码失败")
		return
	}
	select {
	case s.transport.RecvCh() <- data:
	default:
		logger.Warn(logComponent).
			Str("request_id", requestID).
			Msg("RecvCh 已满，丢弃响应")
	}
}

// writeChunk 构造 E2AResponse chunk wire 写入 RecvCh。
func (s *AgentServer) writeChunk(requestID, channelID string, chunk *schema.AgentResponseChunk, sequence int, isStream bool) {
	wire := e2a.EncodeAgentChunkForWire(chunk, requestID, sequence, isStream)
	data, err := json.Marshal(wire)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("request_id", requestID).Int("sequence", sequence).Msg("流式块 JSON 编码失败")
		return
	}
	select {
	case s.transport.RecvCh() <- data:
	default:
		logger.Warn(logComponent).
			Str("request_id", requestID).
			Int("sequence", sequence).
			Msg("RecvCh 已满，丢弃流式块")
	}
}

// sendKeepalive 构造 keepalive chunk 写入 RecvCh。
func (s *AgentServer) sendKeepalive(requestID, channelID string) {
	chunk := schema.NewAgentResponseChunk(requestID, channelID,
		map[string]any{
			"event_type": "keepalive",
		},
	)
	wire := e2a.EncodeAgentChunkForWire(chunk, requestID, -1, true)
	data, err := json.Marshal(wire)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("request_id", requestID).Msg("keepalive JSON 编码失败")
		return
	}
	select {
	case s.transport.RecvCh() <- data:
	default:
		logger.Warn(logComponent).
			Str("request_id", requestID).
			Msg("RecvCh 已满，丢弃 keepalive")
	}
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

// applyResolvedModeToRequest 从 params["mode"] 解析 mode 和 subMode。
//
// 格式 "code.normal" → mode="code", subMode="normal"，默认 "agent.plan"。
func applyResolvedModeToRequest(request *schema.AgentRequest) (mode, subMode string) {
	mode, subMode = defaultMode, defaultSubMode
	if request.Params == nil {
		return
	}

	var params map[string]any
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return
	}

	modeVal, ok := params["mode"]
	if !ok {
		return
	}
	modeStr, ok := modeVal.(string)
	if !ok || modeStr == "" {
		return
	}

	parts := strings.SplitN(modeStr, ".", 2)
	mode = parts[0]
	if len(parts) > 1 {
		subMode = parts[1]
	}
	return
}

// resolveRequestProjectDir 从 params["workspace_dir"] 或 metadata["workspace_dir"] 读取项目目录。
func resolveRequestProjectDir(request *schema.AgentRequest) string {
	// 优先从 params 读取
	if request.Params != nil {
		var params map[string]any
		if err := json.Unmarshal(request.Params, &params); err == nil {
			if v, ok := params["workspace_dir"]; ok {
				if s, ok := v.(string); ok && s != "" {
					return s
				}
			}
		}
	}

	// 其次从 metadata 读取
	if request.Metadata != nil {
		if v, ok := request.Metadata["workspace_dir"]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}

	return ""
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

// injectACPCapabilities 为 ACP 通道注入 client_capabilities 到 metadata。
func (s *AgentServer) injectACPCapabilities(request *schema.AgentRequest, envelope *e2a.E2AEnvelope) {
	if request.Metadata == nil {
		request.Metadata = make(map[string]any)
	}
	if envelope.Params != nil {
		if caps, ok := envelope.Params["client_capabilities"]; ok {
			request.Metadata["client_capabilities"] = caps
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
