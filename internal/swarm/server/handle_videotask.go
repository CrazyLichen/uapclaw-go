package server

import (
	"context"
	"encoding/json"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions/duplex"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	swarmsession "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/videotask"
)

// videotaskLogComponent 语音任务 RPC 日志组件。
const videotaskLogComponent = logger.ComponentAgentServer

// videotaskAdapter 语音任务 RPC 适配器（AgentServer 单例）。
// Python: video_task/server_adapter.py 实例由 runtime 装配；Go 以包级单例表达。
var videotaskAdapter = &videotask.VoiceTaskServerAdapter{}

// handleVoiceTaskCheckpointAck 处理语音任务检查点 ACK。
// 对齐 server_adapter.py:19-29（VOICE_TASK_CHECKPOINT_ACK 分支）。
func (s *AgentServer) handleVoiceTaskCheckpointAck(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	params := map[string]any{}
	if len(request.Params) > 0 {
		_ = json.Unmarshal(request.Params, &params)
	}
	userID := ""
	if request.UserID != nil {
		userID = *request.UserID
	}
	sessionID := ""
	if request.SessionID != nil {
		sessionID = *request.SessionID
	}
	payload, err := videotaskAdapter.Handle(string(schema.ReqMethodVoiceTaskCheckpointAck), params,
		request.RequestID, sessionID, userID)
	if err != nil {
		logger.Warn(videotaskLogComponent).Err(err).Msg("语音任务检查点 ACK 处理失败")
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false), schema.WithPayload(map[string]any{"error": err.Error()})), nil
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(payload)), nil
}

// handleVoiceTaskFiles 处理语音任务产物文件查询。
// 对齐 server_adapter.py:30-52（VOICE_TASK_FILES 分支：guard/归属校验/等落盘/聚合 chat.file）。
func (s *AgentServer) handleVoiceTaskFiles(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	params := map[string]any{}
	if len(request.Params) > 0 {
		_ = json.Unmarshal(request.Params, &params)
	}
	userID := ""
	if request.UserID != nil {
		userID = *request.UserID
	}
	sessionID := ""
	if request.SessionID != nil {
		sessionID = *request.SessionID
	}
	payload, err := videotaskAdapter.Handle(string(schema.ReqMethodVoiceTaskFiles), params,
		request.RequestID, sessionID, userID)
	if err != nil {
		logger.Warn(videotaskLogComponent).Err(err).Msg("语音任务产物查询失败")
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false), schema.WithPayload(map[string]any{"error": err.Error()})), nil
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(payload)), nil
}

// injectVideotaskAssembly 注入语音任务装配缝（AgentServer 启动接线）。
// Python 侧无对应实现：Python 直接 import 会话模块与 send_runtime_push 单例，
// Go 跨包经显式注入（与 cronbridge/SetSendFilePushFunc 启动接线对称）。
func (s *AgentServer) injectVideotaskAssembly() {
	// push 缝：SendRuntimePush 复用 SendPush 通道（delivered=false 语义 → 返回 false）
	videotask.SendRuntimePush = func(msg map[string]any) bool {
		return s.SendPush(context.Background(), msg) == nil
	}
	// 历史源缝：真实 session 包 + duplex 附件归一
	videotask.FileHistorySource.GetSessionMetadata = func(sessionID string, cacheBust bool) map[string]any {
		return swarmsession.GetSessionMetadata(sessionID)
	}
	videotask.FileHistorySource.FlushPendingWrites = func(timeout float64) bool {
		return swarmsession.FlushHistoryQueue(timeout)
	}
	videotask.FileHistorySource.LoadHistoryRecords = func(sessionID string) ([]map[string]any, error) {
		return swarmsession.ReadHistoryRecords(sessionID)
	}
	videotask.FileHistorySource.NormalizeFileItems = func(files any) []map[string]any {
		return duplex.NormalizeFileItems(files)
	}
}
