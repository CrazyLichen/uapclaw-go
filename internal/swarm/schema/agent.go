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
	// Payload 响应负载（延迟解析，与 Message.Payload 一致）
	Payload json.RawMessage `json:"payload,omitempty"`
	// Metadata 扩展元数据
	Metadata map[string]any `json:"metadata,omitempty"`
}

// AgentResponseChunk Agent 响应片段（AgentServer → Gateway，流式）。
//
// 作为 AgentServer 向 Gateway 返回的流式响应块，承载增量负载和完成标识。
// 当前仅定义结构体骨架，工厂函数和 Validate 留给步骤 10.1.6 补全。
//
// 对应 Python: jiuwenswarm/common/schema/agent.py (AgentResponseChunk)
type AgentResponseChunk struct {
	// RequestID 对应请求的唯一标识
	RequestID string `json:"request_id"`
	// ChannelID 来源渠道标识
	ChannelID string `json:"channel_id"`
	// Payload 响应负载片段（延迟解析）
	Payload json.RawMessage `json:"payload,omitempty"`
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
func WithPayload(p json.RawMessage) AgentResponseOption {
	return func(resp *AgentResponse) { resp.Payload = p }
}

// WithResponseMetadata 设置扩展元数据。
func WithResponseMetadata(m map[string]any) AgentResponseOption {
	return func(resp *AgentResponse) { resp.Metadata = m }
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

// ──────────────────────────── 非导出函数 ────────────────────────────
