package e2a

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// E2AToAgentRequest 将规范化成功的 E2A 转为 AgentRequest。
// 若 envelope 含 Gateway 兜底标记，返回 error，须由调用方先分支处理 legacy。
// Python: e2a_to_agent_request(env)
func E2AToAgentRequest(env *E2AEnvelope) (*schema.AgentRequest, error) {
	ctx := make(map[string]any)
	if env.ChannelContext != nil {
		for k, v := range env.ChannelContext {
			ctx[k] = v
		}
	}

	// 网关兜底信封不能走这条路径：先识别标记，让调用方改用 legacy 分支
	internal, hasInternal := ctx[e2aInternalContextKey]
	if hasInternal {
		delete(ctx, e2aInternalContextKey)
		if internalMap, ok := internal.(map[string]any); ok {
			if failed, ok := internalMap[e2aFallbackFailedKey]; ok {
				if b, ok := failed.(bool); ok && b {
					return nil, fmt.Errorf("e2a_to_agent_request 在回退信封上调用；请使用 legacy 路径")
				}
			}
		}
	}

	// 渠道上下文整体作为元数据透传（权限上下文也从这里取）
	var metadata map[string]any
	if len(ctx) > 0 {
		metadata = ctx
	}

	// Python: setup_permission_context — 从 metadata 构建 PermissionContext
	var permCtx *schema.PermissionContext
	if len(metadata) > 0 {
		permCtx = schema.NewPermissionContextFromDict(metadata)
	}

	// 方法名必须是已知的请求方法，否则直接报错（不做猜测）
	methodStr := env.Method
	var reqMethod schema.ReqMethod
	if methodStr != "" {
		rm, err := schema.ParseReqMethod(methodStr)
		if err != nil {
			logger.Error(logComponent).
				Str("method", methodStr).
				Str("request_id", env.RequestID).
				Msg("unknown E2A method")
			return nil, fmt.Errorf("未知 E2A 方法 %q: %w", methodStr, err)
		}
		reqMethod = rm
	}

	// 参数重新序列化成 JSON；序列化失败时退化成空对象，保证后续解析不炸
	var params json.RawMessage
	if env.Params != nil {
		b, err := json.Marshal(env.Params)
		if err != nil {
			params = json.RawMessage(`{}`)
		} else {
			params = json.RawMessage(b)
		}
	} else {
		params = json.RawMessage(`{}`)
	}

	var sessionID *string
	if env.SessionID != "" {
		sessionID = &env.SessionID
	}

	var chatID *string
	if env.ChatID != "" {
		chatID = &env.ChatID
	}

	// 渠道缺省按 web 处理，对应原来只走 Web 端的历史行为
	channelID := env.Channel
	if channelID == "" {
		channelID = "web"
	}

	return &schema.AgentRequest{
		RequestID:         env.RequestID,
		ChannelID:         channelID,
		SessionID:         sessionID,
		ChatID:            chatID,
		ReqMethod:         reqMethod,
		Params:            params,
		IsStream:          env.IsStream,
		Timestamp:         e2aTimestampToFloat(env.Timestamp),
		Metadata:          metadata,
		PermissionContext: permCtx,
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// e2aTimestampToFloat 将 ISO 8601 时间戳转为 Unix 秒浮点数。
// 空串 → 0.0，解析失败 → 0.0。
// Python: _e2a_timestamp_to_float(ts)
func e2aTimestampToFloat(ts string) float64 {
	if ts == "" {
		return 0.0
	}
	s := ts
	if len(s) > 0 && s[len(s)-1] == 'Z' {
		s = s[:len(s)-1] + "+00:00"
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		// 尝试不带纳秒
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return 0.0
		}
	}
	return float64(t.UnixNano()) / 1e9
}
