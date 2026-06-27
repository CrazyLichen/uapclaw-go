package schema

import (
	"encoding/json"
	"fmt"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentRequest Agent 请求（Gateway → AgentServer）。
//
// 作为 Gateway 向 AgentServer 发起的标准化请求模型，承载 RPC 方法名、
// 请求参数、流式标识、权限上下文等。E2A 协议解码后交由 AgentServer 处理。
//
// 对应 Python: jiuwenswarm/common/schema/agent.py (AgentRequest)
type AgentRequest struct {
	// ─── 必填字段 ───

	// RequestID 请求唯一标识
	RequestID string `json:"request_id"`
	// ChannelID 来源渠道标识
	ChannelID string `json:"channel_id"`

	// ─── 可选字段（指针 + omitempty 表达 Python T | None） ───

	// SessionID 会话标识
	SessionID *string `json:"session_id,omitempty"`
	// ChatID IM 聊天标识
	ChatID *string `json:"chat_id,omitempty"`
	// ReqMethod 请求方法
	ReqMethod ReqMethod `json:"req_method,omitempty"`

	// ─── 可选字段（json.RawMessage / map / bool 指针） ───

	// Params 请求参数（延迟解析，与 Message.Params 一致）
	Params json.RawMessage `json:"params,omitempty"`
	// IsStream 是否流式请求
	IsStream bool `json:"is_stream"`
	// Timestamp Unix 秒时间戳（含小数精度，对齐 Python time.time()）
	Timestamp float64 `json:"timestamp"`
	// Metadata 扩展元数据
	Metadata map[string]any `json:"metadata,omitempty"`
	// EnableMemory 是否启用记忆（三态：nil/true/false，对齐 Python bool | None）
	EnableMemory *bool `json:"enable_memory,omitempty"`
	// PermissionContext 权限上下文
	PermissionContext *PermissionContext `json:"permission_context,omitempty"`
}

// AgentResponse Agent 响应（AgentServer → Gateway，非流式完整响应）。
//
// 作为 AgentServer 向 Gateway 返回的完整响应模型，承载执行结果、
// 响应负载、元数据等。
//
// 对应 Python: jiuwenswarm/common/schema/agent.py (AgentResponse)
type AgentResponse struct {
	// RequestID 对应请求的唯一标识
	RequestID string `json:"request_id"`
	// ChannelID 来源渠道标识
	ChannelID string `json:"channel_id"`
	// OK 是否成功（工厂函数默认 true，对齐 Python ok=True）
	OK bool `json:"ok"`
	// Payload 响应负载（对齐 Python payload: dict | None）
	Payload map[string]any `json:"payload,omitempty"`
	// Metadata 扩展元数据
	Metadata map[string]any `json:"metadata,omitempty"`
}

// AgentResponseChunk Agent 响应片段（AgentServer → Gateway，流式）。
//
// 作为 AgentServer 向 Gateway 返回的流式响应块，承载增量负载和完成标识。
// payload 为 map[string]any，业务语义（event_type 等）由使用侧负责构造，
// 不在 Schema 层定义 payload 内部结构（对齐 Python AgentResponseChunk.payload: dict | None）。
//
// 流式协议约定：
//   - is_complete=false：流中间的业务 chunk
//   - is_complete=true 且 payload 含 event_type/content/error 等业务字段：携带业务的结束 chunk
//   - is_complete=true 且 payload 为空/仅含 {"is_complete":true}：终止哨兵，流结束标记
//
// 终止哨兵推荐使用 NewTerminalChunk() 工厂创建，避免手动构造出错。
//
// 对应 Python: jiuwenswarm/common/schema/agent.py (AgentResponseChunk)
type AgentResponseChunk struct {
	// RequestID 对应请求的唯一标识
	RequestID string `json:"request_id"`
	// ChannelID 来源渠道标识
	ChannelID string `json:"channel_id"`
	// Payload 响应负载片段（对齐 Python payload: dict | None）
	Payload map[string]any `json:"payload,omitempty"`
	// IsComplete 是否为最后一个片段
	IsComplete bool `json:"is_complete"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentRequest 创建 Agent 请求实例。
//
// 自动生成 Timestamp（当前时间），设置 RequestID 和 ChannelID。
// 工厂函数保证：IsStream=false（零值），EnableMemory=nil（未设置）。
func NewAgentRequest(requestID, channelID string, reqMethod ReqMethod, params json.RawMessage, opts ...AgentRequestOption) *AgentRequest {
	req := &AgentRequest{
		RequestID: requestID,
		ChannelID: channelID,
		ReqMethod: reqMethod,
		Params:    params,
		Timestamp: float64(time.Now().UnixNano()) / 1e9,
	}
	for _, opt := range opts {
		opt(req)
	}
	return req
}

// NewAgentResponse 创建 Agent 响应实例。
//
// 默认 OK=true（对齐 Python AgentResponse.ok=True）。
func NewAgentResponse(requestID, channelID string, opts ...AgentResponseOption) *AgentResponse {
	resp := &AgentResponse{
		RequestID: requestID,
		ChannelID: channelID,
		OK:        true,
	}
	for _, opt := range opts {
		opt(resp)
	}
	return resp
}

// NewAgentResponseChunk 创建 Agent 响应片段实例。
//
// 工厂函数保证：IsComplete=false（零值，流中间 chunk）。
// 如需创建终止哨兵 chunk，请使用 NewTerminalChunk()。
func NewAgentResponseChunk(requestID, channelID string, payload map[string]any, opts ...AgentResponseChunkOption) *AgentResponseChunk {
	chunk := &AgentResponseChunk{
		RequestID:  requestID,
		ChannelID:  channelID,
		Payload:    payload,
		IsComplete: false,
	}
	for _, opt := range opts {
		opt(chunk)
	}
	return chunk
}

// NewTerminalChunk 创建终止哨兵 chunk。
//
// 终止哨兵是流结束标记：is_complete=true，payload 为 {"is_complete":true}。
// 消费侧通过 IsTerminal() 识别终止哨兵，不再下发业务事件。
//
// 对齐 Python 中两处终止哨兵形态：
//   - payload=None, is_complete=True（interface_deep / team_helpers / auto_harness）
//   - payload={"is_complete": True}, is_complete=True（gateway_normalize / interface）
//
// Go 统一使用形态 B（payload={"is_complete":true}），因为：
//   - 形态 B 信息自包含，消费侧无需额外判断
func NewTerminalChunk(requestID, channelID string) *AgentResponseChunk {
	return &AgentResponseChunk{
		RequestID:  requestID,
		ChannelID:  channelID,
		Payload:    map[string]any{"is_complete": true},
		IsComplete: true,
	}
}

// AgentRequestOption Agent 请求可选配置函数。
type AgentRequestOption func(*AgentRequest)

// WithAgentSessionID 设置会话标识。
func WithAgentSessionID(id string) AgentRequestOption {
	return func(req *AgentRequest) { req.SessionID = &id }
}

// WithAgentChatID 设置 IM 聊天标识。
func WithAgentChatID(id string) AgentRequestOption {
	return func(req *AgentRequest) { req.ChatID = &id }
}

// WithAgentIsStream 设置是否流式。
func WithAgentIsStream(v bool) AgentRequestOption {
	return func(req *AgentRequest) { req.IsStream = v }
}

// WithAgentMetadata 设置扩展元数据。
func WithAgentMetadata(m map[string]any) AgentRequestOption {
	return func(req *AgentRequest) { req.Metadata = m }
}

// WithAgentEnableMemory 设置是否启用记忆（三态：nil=未设置，true/false=显式设置）。
func WithAgentEnableMemory(v bool) AgentRequestOption {
	return func(req *AgentRequest) { req.EnableMemory = &v }
}

// WithAgentPermissionContext 设置权限上下文。
func WithAgentPermissionContext(pc *PermissionContext) AgentRequestOption {
	return func(req *AgentRequest) { req.PermissionContext = pc }
}

// AgentResponseOption Agent 响应可选配置函数。
type AgentResponseOption func(*AgentResponse)

// WithResponseOK 设置是否成功。
func WithResponseOK(v bool) AgentResponseOption {
	return func(resp *AgentResponse) { resp.OK = v }
}

// WithPayload 设置响应负载。
func WithPayload(p map[string]any) AgentResponseOption {
	return func(resp *AgentResponse) { resp.Payload = p }
}

// WithResponseMetadata 设置扩展元数据。
func WithResponseMetadata(m map[string]any) AgentResponseOption {
	return func(resp *AgentResponse) { resp.Metadata = m }
}

// AgentResponseChunkOption Agent 响应片段可选配置函数。
type AgentResponseChunkOption func(*AgentResponseChunk)

// WithChunkIsComplete 设置是否为最后片段。
func WithChunkIsComplete(v bool) AgentResponseChunkOption {
	return func(chunk *AgentResponseChunk) { chunk.IsComplete = v }
}

// WithChunkPayload 设置响应负载片段。
func WithChunkPayload(p map[string]any) AgentResponseChunkOption {
	return func(chunk *AgentResponseChunk) { chunk.Payload = p }
}

// Validate 校验 AgentRequest 必填字段。
//
// 校验规则（对齐 Python 实际使用）：
//   - request_id 非空
//   - channel_id 非空
//   - req_method 非零值
func (r *AgentRequest) Validate() error {
	if r.RequestID == "" {
		return fmt.Errorf("request_id 不能为空")
	}
	if r.ChannelID == "" {
		return fmt.Errorf("channel_id 不能为空")
	}
	if r.ReqMethod == "" {
		return fmt.Errorf("req_method 不能为空")
	}
	return nil
}

// Validate 校验 AgentResponse 必填字段。
//
// 校验规则：
//   - request_id 非空
//   - channel_id 非空
func (r *AgentResponse) Validate() error {
	if r.RequestID == "" {
		return fmt.Errorf("request_id 不能为空")
	}
	if r.ChannelID == "" {
		return fmt.Errorf("channel_id 不能为空")
	}
	return nil
}

// Validate 校验 AgentResponseChunk 必填字段。
//
// 校验规则（对齐 Python AgentResponseChunk 必填）：
//   - request_id 非空
//   - channel_id 非空
func (c *AgentResponseChunk) Validate() error {
	if c.RequestID == "" {
		return fmt.Errorf("request_id 不能为空")
	}
	if c.ChannelID == "" {
		return fmt.Errorf("channel_id 不能为空")
	}
	return nil
}

// IsTerminal 判断是否为终止哨兵 chunk。
//
// 终止哨兵是流结束标记，消费侧不应将其作为业务事件下发。
//
// 判断逻辑（对齐 Python _is_terminal_stream_chunk）：
//   - is_complete 必须为 true
//   - payload 为空，或仅含 {"is_complete":true} 且无 event_type/content/error 等业务字段
func (c *AgentResponseChunk) IsTerminal() bool {
	if !c.IsComplete {
		return false
	}
	// payload 为 nil → 终止哨兵（形态 A：payload=None, is_complete=True）
	if c.Payload == nil {
		return true
	}
	// 含 event_type → 非终止哨兵，是带业务的结束 chunk
	if _, ok := c.Payload["event_type"]; ok {
		return false
	}
	// 含 content 非空 → 非终止哨兵
	if v, ok := c.Payload["content"]; ok && v != nil {
		if s, isStr := v.(string); isStr && s != "" {
			return false
		} else if !isStr {
			return false
		}
	}
	// 含 error 非空 → 非终止哨兵
	if v, ok := c.Payload["error"]; ok && v != nil {
		if s, isStr := v.(string); isStr && s != "" {
			return false
		} else if !isStr {
			return false
		}
	}
	// 仅含 is_complete=true 且无其他业务键 → 终止哨兵（形态 B）
	if v, ok := c.Payload["is_complete"]; ok {
		if b, isBool := v.(bool); isBool && b {
			if len(c.Payload) == 1 {
				return true
			}
		}
	}
	return false
}

// ──────────────────────────── 非导出函数 ────────────────────────────
