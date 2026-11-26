package model_clients

import "context"

// KVCacheActionParams KV cache 管理动作（evict/offload/prefetch）的统一入参。
// Python: model.py:416-495 evict_kvc/offload_kvc/prefetch_kvc 的关键字参数集合。
type KVCacheActionParams struct {
	// SessionID 目标缓存会话标识（必填）
	SessionID string `json:"session_id"`
	// ParentSessionID 父会话标识（可选，默认同 SessionID）
	ParentSessionID string `json:"parent_session_id,omitempty"`
	// Target 作用目标，默认 "session"（Python model.py:421 target: str = "session"）
	Target string `json:"target,omitempty"`
	// Messages 消息定位（string | []BaseMessage | []dict，原样透传）
	Messages any `json:"messages,omitempty"`
	// Tools 工具定位（[]ToolInfo | []dict，原样透传）
	Tools any `json:"tools,omitempty"`
	// Model 模型名（可选）
	Model string `json:"model,omitempty"`
	// MsgStart 消息区间起点（可选）
	MsgStart *int `json:"msg_start,omitempty"`
	// MsgEnd 消息区间终点（可选）
	MsgEnd *int `json:"msg_end,omitempty"`
	// ToolsStart 工具区间起点（可选）
	ToolsStart *int `json:"tools_start,omitempty"`
	// ToolsEnd 工具区间终点（可选）
	ToolsEnd *int `json:"tools_end,omitempty"`
	// IncludeTools 是否包含工具消息
	IncludeTools bool `json:"include_tools"`
	// TurnNum 轮次（可选）
	TurnNum *int `json:"turn_num,omitempty"`
	// Timeout 超时秒数（可选）
	Timeout *float64 `json:"timeout,omitempty"`
}

// KVCActionClient 支持 KV cache 管理动作（evict/offload/prefetch）的客户端可选能力接口。
// Model 门面通过类型断言鸭子式探测（Python: model.py:402 getattr(self._client, f"{action}_kvc")），
// 未实现该接口的客户端静默返回 false。
//
// 已知偏差 D-24.15-1：Python 客户端侧实现为 affinity 协议（agent_hint/context_management
// 注入 chat.completions，openai_model_client.py:850-955 + :766-845），Go 侧协议链未移植，
// 挂账专章实施；本接口仅提供 Model 层鸭子式缝。
type KVCActionClient interface {
	// EvictKvc 逐出 KV cache
	EvictKvc(ctx context.Context, params *KVCacheActionParams) (bool, error)
	// OffloadKvc 移出 KV cache
	OffloadKvc(ctx context.Context, params *KVCacheActionParams) (bool, error)
	// PrefetchKvc 预取 KV cache
	PrefetchKvc(ctx context.Context, params *KVCacheActionParams) (bool, error)
}
