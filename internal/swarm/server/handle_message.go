package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
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

// handleMessage 处理单个 E2A 请求信封，对齐 Python AgentWebSocketServer._handle_message。
//
// 流程：E2AEnvelope → AgentRequest → switch 分发 → writeResponse 写入 RecvCh。
func (s *AgentServer) handleMessage(ctx context.Context, envelope *e2a.E2AEnvelope) {
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
	case schema.ReqMethodSessionPlanStatus:
		resp, err = s.handleSessionPlanStatus(ctx, request)
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

// writeResponse 构造 E2AResponse wire 通过 sendToGateway 发送。
//
// 返回 false 表示编码失败或 transport 已关闭/缓冲满（对齐 Python
// _send_runtime_event 的 "发送失败即停止流" 语义）。
func (s *AgentServer) writeResponse(requestID, channelID string, resp *schema.AgentResponse) bool {
	wire := e2a.EncodeAgentResponseForWire(resp, requestID, 0)
	data, err := json.Marshal(wire)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("request_id", requestID).Msg("response JSON coding failed")
		return false
	}
	return s.sendToGateway(data)
}

// writeChunk 构造 E2AResponse chunk wire 通过 sendToGateway 发送。
func (s *AgentServer) writeChunk(requestID, channelID string, chunk *schema.AgentResponseChunk, sequence int, isStream bool) bool {
	wire := e2a.EncodeAgentChunkForWire(chunk, requestID, sequence, isStream)
	data, err := json.Marshal(wire)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("request_id", requestID).Int("sequence", sequence).Msg("streaming chunk JSON encode failed")
		return false
	}
	return s.sendToGateway(data)
}

// sendKeepalive 构造 keepalive chunk 通过 sendToGateway 发送。
func (s *AgentServer) sendKeepalive(requestID, channelID string) {
	chunk := schema.NewAgentResponseChunk(requestID, channelID,
		map[string]any{
			"event_type": "keepalive",
		},
	)
	s.writeChunk(requestID, channelID, chunk, -1, true)
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
