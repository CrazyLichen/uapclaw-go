package e2a

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// E2AToAgentRequest 将规范化成功的 E2A 转为 AgentRequest。
// 若 envelope 含 Gateway 兜底标记，返回 error，须由调用方先分支处理 legacy。
// 对应 Python: e2a_to_agent_request(env)
func E2AToAgentRequest(env *E2AEnvelope) (*schema.AgentRequest, error) {
	ctx := make(map[string]any)
	if env.ChannelContext != nil {
		for k, v := range env.ChannelContext {
			ctx[k] = v
		}
	}

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

	var metadata map[string]any
	if len(ctx) > 0 {
		metadata = ctx
	}

	methodStr := env.Method
	var reqMethod schema.ReqMethod
	if methodStr != "" {
		rm, err := schema.ParseReqMethod(methodStr)
		if err != nil {
			logger.Error(logComponent).
				Str("method", methodStr).
				Str("request_id", env.RequestID).
				Msg("未知 E2A 方法")
			return nil, fmt.Errorf("未知 E2A 方法 %q: %w", methodStr, err)
		}
		reqMethod = rm
	}

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

	channelID := env.Channel
	if channelID == "" {
		channelID = "web"
	}

	return &schema.AgentRequest{
		RequestID: env.RequestID,
		ChannelID: channelID,
		SessionID: sessionID,
		ChatID:    chatID,
		ReqMethod: reqMethod,
		Params:    params,
		IsStream:  env.IsStream,
		Timestamp: e2aTimestampToFloat(env.Timestamp),
		Metadata:  metadata,
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// e2aTimestampToFloat 将 ISO 8601 时间戳转为 Unix 秒浮点数。
// 空串 → 0.0，解析失败 → 0.0。
// 对应 Python: _e2a_timestamp_to_float(ts)
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
