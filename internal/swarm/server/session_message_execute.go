// Package server 提供 mailbox 记录的目标会话执行回调（execute_internal_session_message）。
//
// 对应 Python 代码：jiuwenswarm/server/agent_ws_server.py:4287-4648
// （execute_internal_session_message / _run_internal_session_message）
// + :644-693（TurnOutcomeTracker）。
// [Go 偏差] 上游经 runtime.stream(background=...) 后台流 + WS push 帧逐事件
// 转发；Go 侧复用 AgentRuntime.Stream 与 AgentServer.SendPush，逐事件推送的
// cross_session 标记注入对齐 _with_cross_session_marker，但 push 信封字段
// （build_server_push_message）以 Go SendPush 既有线缆格式为准（偏差回收条件：
// web 前端契约对齐批次）。历史完成回执（enqueue_history_request_completion）在
// Go 侧暂无对应物，终态直接返回；登记 spec §5。
package server

// ──────────────────────────── 导入 ────────────────────────────

import (
	"context"
	"encoding/json"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	swsession "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sessionMessageOrigin 跨会话消息来源标记
	// Python: gateway/common/session_message.py:8 SESSION_MESSAGE_ORIGIN
	sessionMessageOrigin = "cross_session_agent"
)

// ──────────────────────────── 结构体 ────────────────────────────

// turnOutcomeTracker 仅在事件携带终态信号时分类。
// Python: agent_ws_server.py:644-693 TurnOutcomeTracker
type turnOutcomeTracker struct {
	sawChatFinal         bool
	sawRuntimeAccepted   bool
	sawTeamInputAccepted bool
	waitingUser          bool
	sawError             bool
	errorText            string
}

// observe 观察一个 Runtime 事件。
// Python: agent_ws_server.py:653-669 observe
func (t *turnOutcomeTracker) observe(ev *runtime.RuntimeEvent) {
	eventType := ""
	if ev.Payload != nil {
		eventType, _ = ev.Payload["event_type"].(string)
	}
	switch {
	case eventType == "chat.final":
		t.sawChatFinal = true
	case eventType == "runtime.accepted":
		t.sawRuntimeAccepted = true
	case eventType == "team.input.accepted" || eventType == "chat.processing_status_deferred":
		t.sawTeamInputAccepted = true
	default:
		if _, ok := runtime.UserInteractionEvents[eventType]; ok {
			t.waitingUser = true
		}
	}
	if !ev.OK || runtime.IsTerminalErrorEvent(ev) {
		t.fail(eventError(ev))
	}
}

// fail 记录失败（首个非空错误保留）。
// Python: agent_ws_server.py:671-675 fail
func (t *turnOutcomeTracker) fail(errorText string) {
	t.sawError = true
	if errorText != "" {
		t.errorText = errorText
	}
}

// outcome 终态归类。
// Python: agent_ws_server.py:677-686 outcome
func (t *turnOutcomeTracker) outcome() string {
	switch {
	case t.sawError:
		return StatusFailed
	case t.waitingUser:
		return StatusWaitingUser
	case t.sawChatFinal:
		return StatusSucceeded
	default:
		return StatusUnknown
	}
}

// unconfirmedError 未确认完成的错误描述。
// Python: agent_ws_server.py:688-692 unconfirmed_error
func (t *turnOutcomeTracker) unconfirmedError() string {
	if t.sawRuntimeAccepted {
		return "Runtime accepted the request without confirming completion"
	}
	return "Runtime stream ended without confirming completion"
}

// eventError 从事件负载提取错误描述。
// Python: agent_ws_server.py:694-699 _event_error
func eventError(ev *runtime.RuntimeEvent) string {
	if ev.Payload == nil {
		return ""
	}
	if s, ok := ev.Payload["error"].(string); ok && s != "" {
		return s
	}
	if s, ok := ev.Payload["message"].(string); ok {
		return s
	}
	return ""
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ExecuteInternalSessionMessage 在目标产品会话内执行一条已占用的信箱记录。
// Python: agent_ws_server.py:4287-4368 execute_internal_session_message
func (s *AgentServer) ExecuteInternalSessionMessage(record *SessionMessageRecord) (SessionMessageExecutionResult, error) {
	// 步骤 1: 目标元数据与支持性（Python :4292-4311）
	metadata, ok := s.sessionMessageMetadataProvider.GetSessionMetadata(record.TargetSessionID)
	mode := ""
	channelID := ""
	if ok && metadata != nil {
		mode = s.sessionMessageModeClassifier.DeprecateMode(strOf(metadata["mode"]))
		channelID = lowerCase(trimSpace(strOf(metadata["channel_id"])))
	}
	targetUnsupported := !ok || metadata == nil ||
		!(s.sessionMessageModeClassifier.IsSingleAgentMode(mode) || s.sessionMessageModeClassifier.IsTeamMode(mode)) ||
		(channelID != "web" && channelID != "tui") ||
		trimSpace(strOf(metadata["cron_id"])) != ""
	if targetUnsupported {
		return SessionMessageExecutionResult{Status: StatusFailed,
			ErrorCode: "UNSUPPORTED_TARGET",
			Error:     "target Session no longer supports Agent messaging"}, nil
	}
	// 步骤 2: 归属复核（执行前归属可能变更；Python :4313-4324）
	storedUserID := trimSpace(strOf(metadata["user_id"]))
	ownerMatches := storedUserID == ""
	if record.OwnerScopeID != "local" {
		ownerMatches = storedUserID == record.OwnerScopeID
	}
	if !ownerMatches {
		return SessionMessageExecutionResult{Status: StatusFailed,
			ErrorCode: "NOT_FOUND_OR_FORBIDDEN",
			Error:     "target Session ownership changed before execution"}, nil
	}
	// 步骤 3: cross_session 载荷（Python :4326-4336）
	crossSession := map[string]any{
		"message_id":          record.MessageID,
		"source_session_id":   record.SourceSessionID,
		"source_request_id":   record.SourceRequestID,
		"source_tool_call_id": record.SourceToolCallID,
		"source_title":        record.SourceTitleSnapshot,
		"chain_id":            record.ChainID,
		"parent_message_id":   record.ParentMessageID,
		"hop_count":           record.HopCount,
	}
	// 步骤 4: params（Python :4337-4350）
	params := map[string]any{
		"query":                     record.Content,
		"mode":                      mode,
		sessionMessageInternalKey(): crossSession,
	}
	if record.InputMode != "" {
		params["input_mode"] = record.InputMode
	}
	for _, key := range []string{"project_id", "project_dir", "work_mode"} {
		if v := strOf(metadata[key]); trimSpace(v) != "" {
			params[key] = v
		}
	}
	if modelName, ok := metadata["model"].(string); ok && trimSpace(modelName) != "" {
		params["model_name"] = trimSpace(modelName)
	}
	// 步骤 5: AgentRequest（Python :4352-4362）
	sessionID := record.TargetSessionID
	req := &schema.AgentRequest{
		RequestID: record.ExecutionRequestID,
		ChannelID: channelID,
		SessionID: &sessionID,
		UserID:    &storedUserID,
		ReqMethod: schema.ReqMethodChatSend,
		Params:    mustMarshalJSON(params),
		IsStream:  true,
		Timestamp: nowFloat(),
		Metadata:  map[string]any{sessionMessageInternalKey(): crossSession},
	}
	// 步骤 6: 执行（Python :4364-4368 execution_scope 略——Go 无对应作用域缝）
	return s.runInternalSessionMessage(record, req, channelID, crossSession)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sessionMessageMetadataProvider / sessionMessageModeClassifier 装配注入
// （AgentServer 持有；见 installSessionMessageService）。

// runInternalSessionMessage 消费 runtime 流并归类终态。
// Python: agent_ws_server.py:4370-4648 _run_internal_session_message
// [Go 偏差] supplemental 投递的 runtime.accepted 输入边界分派（chat/stream/
// team.input.accepted/deferred 四分支）依赖 runtime 流的 input_delivery 标记，
// Go RuntimeEvent 暂无该字段——supplemental 以流正常结束为 delivered（偏差
// 回收条件：RuntimeEvent 增加 input_delivery/input_boundary 字段）。
func (s *AgentServer) runInternalSessionMessage(record *SessionMessageRecord, req *schema.AgentRequest, channelID string, crossSession map[string]any) (SessionMessageExecutionResult, error) {
	tracker := &turnOutcomeTracker{}
	waiting := false
	processingFinished := false
	supplementalDelivery := record.InputMode == "steer"
	delivered := false
	deliveryErrorCode := ""

	// 非补充投递先推 processing 帧（Python :4409-4411 start_processing）
	startProcessing := func() {
		_ = s.SendPush(context.Background(), s.withCrossSessionMarker(map[string]any{
			"event_type":    "chat.processing_status",
			"session_id":    record.TargetSessionID,
			"is_processing": true,
			"is_complete":   false,
			"content":       record.Content,
		}, req.RequestID, record, crossSession))
	}
	if !supplementalDelivery {
		startProcessing()
	}
	events, streamErr := collectRuntimeEvents(func(cb func(*runtime.RuntimeEvent) bool) error {
		return s.agentRuntime.Stream(context.Background(), req, cb)
	})
	if streamErr != nil {
		tracker.fail(streamErr.Error())
	}
	for _, ev := range events {
		payload := ev.Payload
		if payload == nil {
			payload = map[string]any{}
		}
		eventType, _ := payload["event_type"].(string)
		// steer 被接受且开始回退 → 观察空闲回退（Python :4490-4495）
		if record.InputMode == "steer" && ev.OK && eventType == "runtime.accepted" &&
			s.sessionMessageService != nil {
			s.sessionMessageService.OnSteeringFallbackStarted(record)
		}
		tracker.observe(ev)
		if !ev.OK || runtime.IsTerminalErrorEvent(ev) {
			// 终态错误归一为 chat.error（Python :4477-4487）
			payload = map[string]any{
				"event_type": "chat.error",
				"error":      firstNonEmpty(eventError(ev), "Runtime execution failed"),
			}
			eventType = "chat.error"
		}
		// 用户交互 → mark_waiting 持久 HITL 关联（Python :4515-4541）
		if _, isInteraction := runtime.UserInteractionEvents[eventType]; isInteraction {
			interruptRequestID := trimSpace(strOf(payload["request_id"]))
			interruptSource := trimSpace(strOf(payload["source"]))
			if s.sessionMessageService == nil {
				tracker.fail("Session message service is unavailable")
			} else if interruptRequestID == "" {
				tracker.fail("Runtime emitted an uncorrelated user question")
			} else if !waiting {
				var err error
				waiting, err = s.sessionMessageService.MarkWaiting(record.MessageID,
					interruptRequestID, interruptSource)
				if err != nil || !waiting {
					tracker.fail("Failed to persist user-question correlation")
				}
			}
			if tracker.sawError {
				payload = map[string]any{"event_type": "chat.error", "error": tracker.errorText}
			}
		}
		// 推送（cross_session 标记注入；Python :4542-4555）
		marked := s.withCrossSessionMarker(payload, ev.RequestID, record, crossSession)
		if et, _ := marked["event_type"].(string); et == "chat.processing_status" {
			if v, _ := marked["is_processing"].(bool); !v {
				processingFinished = true
			}
		}
		_ = s.SendPush(context.Background(), marked)
	}
	// 处理帧未完成 → 补终帧（Python :4559-4581）
	if !supplementalDelivery && !processingFinished && !tracker.sawTeamInputAccepted {
		_ = s.SendPush(context.Background(), s.withCrossSessionMarker(map[string]any{
			"event_type":    "chat.processing_status",
			"session_id":    record.TargetSessionID,
			"is_processing": false,
			"is_complete":   true,
		}, req.RequestID, record, crossSession))
	}
	// 终态映射（Python :4621-4648；team 目标输入入队即 delivered，输出经团队广播）
	if tracker.sawTeamInputAccepted && s.sessionMessageModeClassifier.IsTeamMode(targetModeOf(req)) {
		return SessionMessageExecutionResult{Status: StatusDelivered}, nil
	}
	if supplementalDelivery {
		if tracker.outcome() == StatusFailed {
			status := StatusFailed
			if deliveryErrorCode == "SESSION_INPUT_DELIVERY_UNKNOWN" {
				status = StatusUnknown
			}
			code := deliveryErrorCode
			if code == "" {
				code = "DELIVERY_FAILED"
			}
			return SessionMessageExecutionResult{Status: status, ErrorCode: code,
				Error: firstNonEmpty(tracker.errorText, "Runtime rejected supplemental input")}, nil
		}
		if delivered {
			return SessionMessageExecutionResult{Status: StatusDelivered}, nil
		}
		return SessionMessageExecutionResult{Status: StatusUnknown,
			ErrorCode: "DELIVERY_NOT_CONFIRMED",
			Error:     "Runtime did not confirm supplemental delivery"}, nil
	}
	switch tracker.outcome() {
	case StatusFailed:
		return SessionMessageExecutionResult{Status: StatusFailed,
			ErrorCode: "EXECUTION_FAILED",
			Error:     firstNonEmpty(tracker.errorText, "Runtime execution failed")}, nil
	case StatusUnknown:
		return SessionMessageExecutionResult{Status: StatusUnknown,
			ErrorCode: "EXECUTION_NOT_COMPLETED",
			Error:     tracker.unconfirmedError()}, nil
	case StatusWaitingUser:
		return SessionMessageExecutionResult{Status: StatusWaitingUser}, nil
	default:
		return SessionMessageExecutionResult{Status: StatusDelivered}, nil
	}
}

// withCrossSessionMarker 注入轮次身份与跨会话标记（ask_user 的交互关联
// request_id 不覆盖；Python :4378-4396 _with_cross_session_marker）。
func (s *AgentServer) withCrossSessionMarker(payload map[string]any, requestID string, record *SessionMessageRecord, crossSession map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range payload {
		out[k] = v
	}
	if et, _ := out["event_type"].(string); et == "chat.input_received" {
		return out
	}
	if out["request_id"] == nil || out["request_id"] == "" {
		out["request_id"] = requestID
	}
	out["turn_request_id"] = requestID
	out["message_origin"] = sessionMessageOrigin
	out["session_message_id"] = record.MessageID
	publicCross := map[string]any{}
	for k, v := range crossSession {
		publicCross[k] = v
	}
	publicCross["content"] = record.Content
	out["cross_session"] = publicCross
	return out
}

// firstNonEmpty 首个非空串。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// mustMarshalJSON 参数包序列化（失败降级空对象——params 构造全为可控类型）。
func mustMarshalJSON(v map[string]any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("{}")
	}
	return data
}

// sessionMessageInternalKey 导出请求键（runtime/request.go 小写不可达）。
// Python: gateway/common/session_message.py SESSION_MESSAGE_INTERNAL_KEY
func sessionMessageInternalKey() string {
	return runtime.SessionMessageInternalKey
}

// collectRuntimeEvents 收集流事件（测试缝：sessionMessageStreamInvoker 可替换）。
// [测试缝偏差] Python 直接 runtime.stream(...)；Go 经可替换 invoker 以便单测
// 注入假流（生产装配默认走 AgentRuntime.Stream）。回收条件：单测基建可直造
// AgentRuntime 后移除。
func collectRuntimeEvents(invoke func(cb func(*runtime.RuntimeEvent) bool) error) ([]*runtime.RuntimeEvent, error) {
	if sessionMessageStreamInvoker != nil {
		return sessionMessageStreamInvoker()
	}
	var events []*runtime.RuntimeEvent
	err := invoke(func(ev *runtime.RuntimeEvent) bool {
		events = append(events, ev)
		return true
	})
	return events, err
}

// sessionMessageStreamInvoker 测试注入缝（nil=走真实 runtime.Stream）。
var sessionMessageStreamInvoker func() ([]*runtime.RuntimeEvent, error)

// schemaModeClassifier schema 包 mode_matrix 的接口适配。
type schemaModeClassifier struct{}

// DeprecateMode 折叠遗留别名。
func (schemaModeClassifier) DeprecateMode(mode string) string { return schema.DeprecateMode(mode) }

// IsSingleAgentMode 是否单 agent 模式。
func (schemaModeClassifier) IsSingleAgentMode(mode string) bool {
	return schema.IsSingleAgentMode(mode)
}

// IsTeamMode 是否 team 模式。
func (schemaModeClassifier) IsTeamMode(mode string) bool { return schema.IsTeamMode(mode) }

// IsPlanMode 是否计划模式。
func (schemaModeClassifier) IsPlanMode(mode string) bool { return schema.IsPlanMode(mode) }

// sessionMetadataAdapter session 子包元数据的接口适配。
type sessionMetadataAdapter struct{}

// GetSessionMetadata 单会话元数据（nil/缺失 → false）。
func (sessionMetadataAdapter) GetSessionMetadata(sessionID string) (map[string]any, bool) {
	m := swsession.GetSessionMetadata(sessionID)
	if m == nil {
		return nil, false
	}
	return m, true
}

// GetAllSessionsMetadata 全量元数据。
func (sessionMetadataAdapter) GetAllSessionsMetadata() ([]map[string]any, error) {
	rows, _ := swsession.GetAllSessionsMetadata(100000, 0)
	return rows, nil
}

// runtimeSessionMessageAdmission 默认准入适配（经 AgentRuntime 判定）。
// Python: heartbeat_runtime.admission（begin/end_session_message + 探针属性）。
// [偏差] Go 侧 ExecutionRegistry 会话消息道的 begin/end 账本暂缺——Begin/End
// 记 debug 日志直通，busy 探针接 runtime 真判定。回收条件：work_scheduler
// 会话消息道落地后接真账本（登记 spec §5）。
type runtimeSessionMessageAdmission struct {
	runtime *runtime.AgentRuntime
}

// BeginSessionMessage task 道准入登记。
func (a *runtimeSessionMessageAdmission) BeginSessionMessage(sessionID, runID string) error {
	logger.Debug(logger.ComponentAgentServer).
		Str("session_id", sessionID).Str("run_id", runID).
		Msg("session message admission begin")
	return nil
}

// EndSessionMessage 释放准入。
func (a *runtimeSessionMessageAdmission) EndSessionMessage(sessionID, runID string) error {
	logger.Debug(logger.ComponentAgentServer).
		Str("session_id", sessionID).Str("run_id", runID).
		Msg("session message admission end")
	return nil
}

// IsUserActive 用户活跃探针（Go 侧暂无会话活跃账本 → false）。
func (a *runtimeSessionMessageAdmission) IsUserActive(string) bool { return false }

// IsSessionMessageActive 本邮箱在执行探针。
func (a *runtimeSessionMessageAdmission) IsSessionMessageActive(sessionID string) bool {
	return a.runtime.SessionMessageActive(sessionID)
}

// IsHeartbeatActive 心跳活跃探针（⤴️25.6 批二 HeartbeatJobService 注入后接真值）。
func (a *runtimeSessionMessageAdmission) IsHeartbeatActive(string) bool { return false }

// IsSessionMessageBlocked 会话阻止探针。
func (a *runtimeSessionMessageAdmission) IsSessionMessageBlocked(sessionID string) bool {
	return a.runtime.SessionMessageBlocked(sessionID)
}

// InstallSessionMessageService 装配跨会话消息邮箱（server 面）。
// Python: agent_ws_server.py:1412-1433 _install_session_message_service
func (s *AgentServer) InstallSessionMessageService(ctx context.Context) {
	store := NewSessionMessageStore(workspace.WorkspaceDir()+"/session_messages.sqlite3", "WAL")
	s.sessionMessageMetadataProvider = sessionMetadataAdapter{}
	s.sessionMessageModeClassifier = schemaModeClassifier{}
	s.sessionMessageService = NewSessionMessageService(SessionMessageServiceParams{
		Store:             store,
		Admission:         &runtimeSessionMessageAdmission{runtime: s.agentRuntime},
		Execute:           s.ExecuteInternalSessionMessage,
		StatusCallback:    s.pushSessionMessageStatus,
		RequiresTaskQueue: s.agentRuntime.SessionMessageRequiresQueue,
		ModeClassifier:    s.sessionMessageModeClassifier,
		MetadataProvider:  s.sessionMessageMetadataProvider,
		Available:         true,
	})
	if err := s.sessionMessageService.Start(ctx); err != nil {
		logger.Error(logger.ComponentAgentServer).Err(err).
			Msg("session message service start failed")
	}
	// Python: self._runtime.set_session_message_service(service)——
	// Go 侧 runtime 经回调访问（见 AgentRuntime.SetSessionMessageService）
	s.agentRuntime.SetSessionMessageService(s.sessionMessageService)
}

// pushSessionMessageStatus 状态推送（WS 帧）。
// Python: _push_session_message_status（构造 chat.processing_status 推送）
func (s *AgentServer) pushSessionMessageStatus(record *SessionMessageRecord) error {
	return s.SendPush(context.Background(), map[string]any{
		"request_id": record.ExecutionRequestID,
		"session_id": record.TargetSessionID,
		"payload": map[string]any{
			"event_type":        "chat.session_message_status",
			"message_id":        record.MessageID,
			"target_session_id": record.TargetSessionID,
			"source_session_id": record.SourceSessionID,
			"status":            record.Status,
			"last_error_code":   record.LastErrorCode,
			"last_error":        record.LastError,
		},
	})
}

// targetModeOf 从请求 params 解析 mode（Python :4374 deprecate_mode(request.params["mode"])）。
func targetModeOf(req *schema.AgentRequest) string {
	var params map[string]any
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return ""
	}
	mode, _ := params["mode"].(string)
	return mode
}
