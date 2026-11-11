package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	recommendation "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/recommendation"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SetProactiveEngine 注入主动推荐引擎。
//
// 由启动流程（initProactiveEngine 装配完成后）调用；未注入时两个 RPC 返回
// 「ProactiveEngine not initialized」，不 panic。
//
// Python: server.set_proactive_engine(engine)
func (s *AgentServer) SetProactiveEngine(engine *recommendation.ProactiveEngine) {
	s.proactiveEngine = engine
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleProactiveTick 定时器触发一次主动推荐。
//
// Python: handlers/ops.py:57 handle_proactive_tick
func (s *AgentServer) handleProactiveTick(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	engine := s.proactiveEngine
	if engine == nil {
		return &schema.AgentResponse{
			RequestID: request.RequestID,
			ChannelID: request.ChannelID,
			OK:        false,
			Payload:   map[string]any{"error": "ProactiveEngine not initialized"},
		}, nil
	}

	params, err := decodeRequestParams(request)
	if err != nil {
		return &schema.AgentResponse{
			RequestID: request.RequestID,
			ChannelID: request.ChannelID,
			OK:        false,
			Payload:   map[string]any{"error": err.Error()},
		}, nil
	}
	targetChannel, _ := params["target_channel"].(string)

	success := engine.TickNow(ctx, targetChannel)

	status := "no_recommendation"
	if success {
		status = "tick_executed"
	}
	if lastTick := engine.LastTickAt(); lastTick > 0 {
		status = fmt.Sprintf("%s (last_tick_at=%.0f)", status, lastTick)
	}
	return &schema.AgentResponse{
		RequestID: request.RequestID,
		ChannelID: request.ChannelID,
		OK:        true,
		Payload:   map[string]any{"status": status, "success": success},
	}, nil
}

// handleProactiveFeedback 记下用户对一条主动推荐的喜欢或不喜欢。
//
// Python: handlers/ops.py:93 handle_proactive_feedback
func (s *AgentServer) handleProactiveFeedback(request *schema.AgentRequest) (*schema.AgentResponse, error) {
	params, err := decodeRequestParams(request)
	if err != nil {
		return &schema.AgentResponse{
			RequestID: request.RequestID,
			ChannelID: request.ChannelID,
			OK:        false,
			Payload:   map[string]any{"error": err.Error()},
		}, nil
	}

	recID, _ := params["rec_id"].(string)
	feedbackType, _ := params["feedback_type"].(string)

	// rec_type/rec_target 优先取 rec_* 键，回退到 proactive_* 键（前端两种都可能传）
	recType := firstStringParam(params, "rec_type", "proactive_type")
	recTarget := firstStringParam(params, "rec_target", "proactive_target")

	if recID == "" || (feedbackType != "explicit_like" && feedbackType != "explicit_dislike") {
		return &schema.AgentResponse{
			RequestID: request.RequestID,
			ChannelID: request.ChannelID,
			OK:        false,
			Payload: map[string]any{
				"error": "Invalid params: rec_id and feedback_type " +
					"(explicit_like|explicit_dislike) required",
			},
		}, nil
	}

	recommendation.RecordExplicitFeedback(recID, feedbackType, &recommendation.RecMeta{
		RecType:   recType,
		RecTarget: recTarget,
	})
	return &schema.AgentResponse{
		RequestID: request.RequestID,
		ChannelID: request.ChannelID,
		OK:        true,
		Payload:   map[string]any{"status": "feedback_recorded", "rec_id": recID},
	}, nil
}

// decodeRequestParams 解析请求的 params（缺失按空 map 处理）。
func decodeRequestParams(request *schema.AgentRequest) (map[string]any, error) {
	params := make(map[string]any)
	if len(request.Params) == 0 {
		return params, nil
	}
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	return params, nil
}

// firstStringParam 按顺序取首个非空字符串参数（两侧 trim 后判定）。
func firstStringParam(params map[string]any, keys ...string) string {
	for _, key := range keys {
		value, _ := params[key].(string)
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
