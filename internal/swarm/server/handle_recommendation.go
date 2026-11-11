package server

// 推荐面板 RPC：AgentServer 执行，网关仅转发。
// Python: jiuwenswarm/server/runtime/recommendation_rpc.py（本节只接
// prompt_suggestion_get / prompt_suggestion_event 两个 action；其余 8 个
// action 属 topic 流水线，spec 10.7.2 D-2）。

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/recommendation"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// suggestionReadyPusher 建议就绪推送：ready 事件经 AgentServer.SendPush 送达网关。
// Python: prompt_suggestion.py:639-670 _push_checked_suggestions 内
// WebSocketGatewayPushTransport().send_push
type suggestionReadyPusher struct{}

var _ recommendation.SuggestionReadyPusher = (*suggestionReadyPusher)(nil)

// PushReady 把 ready 事件编码为 server_push 语义消息推送。
func (s *suggestionReadyPusher) PushReady(ctx context.Context, event recommendation.SuggestionReadyEvent) {
	payload := map[string]any{
		"event_type":    recommendation.ReadyEvent,
		"session_id":    event.SessionID,
		"request_id":    event.RequestID,
		"suggestion_id": event.SuggestionID,
		"text":          event.Text,
	}
	server := GetInstance()
	if server == nil {
		logger.Warn(logger.ComponentAgentServer).Str("session_id", event.SessionID).
			Msg("prompt suggestion ready push: AgentServer singleton not initialized")
		return
	}
	if err := server.SendPush(ctx, map[string]any{
		"channel_id": "",
		"session_id": event.SessionID,
		"request_id": event.RequestID,
		"payload":    payload,
	}); err != nil {
		logger.Warn(logger.ComponentAgentServer).Err(err).Str("session_id", event.SessionID).
			Msg("prompt suggestion ready push failed")
	}
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// promptStoreMu 保护 promptStoreCache
	promptStoreMu sync.Mutex
	// promptStoreCache 推荐账本库缓存（路径变了重建，对齐 Python host.prompt_store）
	promptStoreCache *recommendation.TopicStarterStore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// promptStore 取（带缓存的）推荐账本库。
// Python: recommendation_rpc.py:40-51 _prompt_store
func promptStore() (*recommendation.TopicStarterStore, error) {
	promptStoreMu.Lock()
	defer promptStoreMu.Unlock()
	path := filepath.Join(workspace.AgentWorkspaceDir(), recommendation.ConsentDBName)
	if promptStoreCache != nil && promptStoreCache.Path() == path {
		return promptStoreCache, nil
	}
	store, err := recommendation.NewTopicStarterStore(path)
	if err != nil {
		return nil, err
	}
	if promptStoreCache != nil {
		_ = promptStoreCache.Close()
	}
	promptStoreCache = store
	return store, nil
}

// handleRecommendation RECOMMENDATION RPC 分发（两个 action）。
// Python: recommendation_rpc.py:61-111 dispatch_recommendation 的 prompt_suggestion 分支
func (s *AgentServer) handleRecommendation(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	params := map[string]any{}
	if len(request.Params) > 0 {
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return recommendationFail("params must be object", "BAD_REQUEST"), nil
		}
	}
	action, _ := params["action"].(string)
	switch action {
	case "prompt_suggestion_get":
		targetSession, _ := params["session_id"].(string)
		if targetSession == "" && request.SessionID != nil {
			targetSession = *request.SessionID
		}
		targetSession = strings.TrimSpace(targetSession)
		if targetSession == "" {
			return recommendationFail("session_id is required", "BAD_REQUEST"), nil
		}
		store, err := promptStore()
		if err != nil {
			return recommendationFail("failed to open recommendation store", "INTERNAL_ERROR"), nil
		}
		payload := recommendation.CurrentSuggestions(recommendation.Forks, store, targetSession)
		return recommendationOK(payload), nil
	case "prompt_suggestion_event":
		return s.handlePromptSuggestionEvent(ctx, request, params)
	default:
		// Python: return _fail("unknown recommendation action", "BAD_REQUEST")
		return recommendationFail("unknown recommendation action", "BAD_REQUEST"), nil
	}
}

// handlePromptSuggestionEvent 页面回执与打字抢跑取消。
// Python: recommendation_rpc.py:114-139 _prompt_suggestion_event
func (s *AgentServer) handlePromptSuggestionEvent(ctx context.Context, request *schema.AgentRequest,
	params map[string]any,
) (*schema.AgentResponse, error) {
	if !recommendation.TopicRecommendationEnabled(s.config) {
		return recommendationOK(map[string]any{"recorded": false, "reason": "disabled"}), nil
	}
	if eventType, _ := params["event_type"].(string); eventType == "input_changed" {
		targetSession, _ := params["session_id"].(string)
		if targetSession == "" && request.SessionID != nil {
			targetSession = *request.SessionID
		}
		targetSession = strings.TrimSpace(targetSession)
		if targetSession == "" {
			return recommendationFail("session_id is required", "BAD_REQUEST"), nil
		}
		cancelled := false
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.Warn(logger.ComponentAgentServer).Any("recover", recovered).
						Str("session_id", targetSession).Msg("prompt suggestion cancel failed")
				}
			}()
			cancelled = recommendation.Forks.CancelSuggestion(targetSession)
		}()
		return recommendationOK(map[string]any{"cancelled": cancelled}), nil
	}
	store, err := promptStore()
	if err != nil {
		return recommendationFail("failed to open recommendation store", "INTERNAL_ERROR"), nil
	}
	recorded := recommendation.RecordPageOutcome(store, params)
	if recorded == nil {
		return recommendationFail("unknown suggestion or outcome", "BAD_REQUEST"), nil
	}
	return recommendationOK(map[string]any{"recorded": *recorded}), nil
}

// handlePromptSuggestionCancel 取消一条正在生成的建议。
// Python: agent_ws_server.py:2929-2949 PROMPT_SUGGESTION_CANCEL 分支
func (s *AgentServer) handlePromptSuggestionCancel(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	sessionID := ""
	if request.SessionID != nil {
		sessionID = strings.TrimSpace(*request.SessionID)
	}
	if sessionID == "" {
		return recommendationFail("session_id is required", "BAD_REQUEST"), nil
	}
	return recommendationOK(map[string]any{"cancelled": recommendation.Forks.CancelSuggestion(sessionID)}), nil
}

// WireSuggestionReadyPusher server 启动时把推送通道接到全局 forks。
// （Python 在 _fork 内部即时构造 transport；Go 经窄接口依赖倒置，spec 10.7.2 §3.1）
func WireSuggestionReadyPusher() {
	recommendation.Forks.SetSuggestionReadyPusher(&suggestionReadyPusher{})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// recommendationOK 成功响应。
func recommendationOK(payload map[string]any) *schema.AgentResponse {
	return &schema.AgentResponse{OK: true, Payload: payload}
}

// recommendationFail 失败响应（payload.error/code 为协议键，禁本地化）。
func recommendationFail(message, code string) *schema.AgentResponse {
	return &schema.AgentResponse{OK: false, Payload: map[string]any{"error": message, "code": code}}
}
