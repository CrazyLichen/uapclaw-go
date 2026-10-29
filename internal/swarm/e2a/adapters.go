// adapters.go 提供 E2A 与 ACP/A2A 外协议形状互转的纯函数层。
//
// 对应 Python jiuwenswarm/common/e2a/adapters.py：入站写 provenance 溯源
// （source_protocol=acp/a2a + converter + converted_at + details）；出站把
// E2AResponse 投影为外协议 JSON（不适用时返回 nil）；回流把客户端 JSON-RPC
// 响应构造为内部 Message（req_method=acp.tool_response）。
package e2a

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EnvelopeFromACPOptions 对应 Python envelope_from_acp_jsonrpc 的 keyword-only 参数。
type EnvelopeFromACPOptions struct {
	// JSONRPCID JSON-RPC 标识（str/int/None）——保留 any 透传（协议保真：客户端数字 id 原样回传）
	JSONRPCID any
	// SessionID 会话 id（空则查 params["session_id"]；非 str 置空）
	SessionID string
	// Channel 渠道
	Channel string
	// IdentityOrigin 身份来源（零值时取 IdentityOriginUser，Python 默认 USER）
	IdentityOrigin IdentityOrigin
	// Converter converter 串（零值时用 converterACP）
	Converter string
	// ExtraProvenanceDetails 合入 provenance details（可覆盖既有键，对齐 dict.update）
	ExtraProvenanceDetails map[string]any
}

// EnvelopeFromA2ASendMessageOptions 对应 Python envelope_from_a2a_send_message 的 keyword-only 参数。
type EnvelopeFromA2ASendMessageOptions struct {
	// Metadata 对应 E2A 的 a2a_metadata（A2A 规范侧，与通道 metadata/channel_context 无关）
	Metadata map[string]any
	// Configuration A2A configuration（acceptedOutputModes 提取为 ExpectedOutputModes）
	Configuration map[string]any
	// Channel 渠道
	Channel string
	// IdentityOrigin 身份来源（零值时取 IdentityOriginUser，Python 默认 USER）
	IdentityOrigin IdentityOrigin
	// Converter converter 串（零值时用 converterA2A）
	Converter string
	// ExtraProvenanceDetails 合入 provenance details
	ExtraProvenanceDetails map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// converterACP ACP 入站 converter 标识（Go 化格式，gateway_normalize 惯例）
	converterACP = "e2a.adapters:EnvelopeFromACPJsonrpc"
	// converterA2A A2A 入站 converter 标识（Go 化格式，gateway_normalize 惯例）
	converterA2A = "e2a.adapters:EnvelopeFromA2ASendMessage"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// EnvelopeFromACPJsonrpc 由 ACP JSON-RPC 调用构造 E2A；provenance 标明来源为 acp。
// Python: adapters.py envelope_from_acp_jsonrpc
func EnvelopeFromACPJsonrpc(method string, params map[string]any, opts *EnvelopeFromACPOptions) *E2AEnvelope {
	if opts == nil {
		opts = &EnvelopeFromACPOptions{}
	}
	// p = dict(params or {})：拷贝防外部污染
	p := make(map[string]any, len(params))
	for k, v := range params {
		p[k] = v
	}
	// sid = session_id or p.get("session_id")；sid if isinstance(sid, str) else None
	sid := opts.SessionID
	if sid == "" {
		if raw, ok := p["session_id"]; ok {
			if s, ok := raw.(string); ok {
				sid = s
			}
		}
	}
	// details：kind → jsonrpc_method → jsonrpc_id（非 nil 才加）→ extra.update
	details := map[string]any{
		"kind":           "jsonrpc_request",
		"jsonrpc_method": method,
	}
	if opts.JSONRPCID != nil {
		details["jsonrpc_id"] = opts.JSONRPCID
	}
	for k, v := range opts.ExtraProvenanceDetails {
		details[k] = v
	}
	return &E2AEnvelope{
		Provenance: E2AProvenance{
			SourceProtocol: E2ASourceProtocolACP,
			Converter:      orDefaultString(opts.Converter, converterACP),
			ConvertedAt:    UTCNowISO(),
			Details:        details,
		},
		JSONRPCID:      opts.JSONRPCID,
		Method:         method,
		Params:         p,
		SessionID:      sid,
		Channel:        opts.Channel,
		IdentityOrigin: orDefaultIdentityOrigin(opts.IdentityOrigin),
	}
}

// EnvelopeFromA2ASendMessage 将 A2A SendMessage 语义转为 E2A；provenance 标明来源为 a2a。
// 默认 method 为 session/prompt，完整 message/configuration 保留在 params 与 a2a_metadata。
// Python: adapters.py envelope_from_a2a_send_message
func EnvelopeFromA2ASendMessage(taskID, contextID string, messageBody map[string]any, opts *EnvelopeFromA2ASendMessageOptions) *E2AEnvelope {
	if opts == nil {
		opts = &EnvelopeFromA2ASendMessageOptions{}
	}
	// meta = dict(metadata or {})
	meta := make(map[string]any, len(opts.Metadata))
	for k, v := range opts.Metadata {
		meta[k] = v
	}
	// acceptedOutputModes：isinstance(acc, list) 才提取，逐个 str()
	accepted := make([]string, 0)
	if opts.Configuration != nil {
		if acc, ok := opts.Configuration["acceptedOutputModes"].([]any); ok {
			for _, x := range acc {
				accepted = append(accepted, fmt.Sprintf("%v", x))
			}
		}
	}
	// details：kind → abstract_operation → extra.update
	details := map[string]any{
		"kind":                "a2a_send_message",
		"abstract_operation": "SendMessage",
	}
	for k, v := range opts.ExtraProvenanceDetails {
		details[k] = v
	}
	// params={"message": body, "configuration": configuration or {}}
	paramsOut := map[string]any{"message": messageBody}
	if opts.Configuration != nil {
		paramsOut["configuration"] = opts.Configuration
	} else {
		paramsOut["configuration"] = map[string]any{}
	}
	return &E2AEnvelope{
		Provenance: E2AProvenance{
			SourceProtocol: E2ASourceProtocolA2A,
			Converter:      orDefaultString(opts.Converter, converterA2A),
			ConvertedAt:    UTCNowISO(),
			Details:        details,
		},
		Method:              "session/prompt",
		TaskID:              taskID,
		ContextID:           contextID,
		Channel:             opts.Channel,
		IdentityOrigin:      orDefaultIdentityOrigin(opts.IdentityOrigin),
		ExpectedOutputModes: accepted,
		Params:              paramsOut,
		A2AMetadata:         meta,
	}
}

// EnvelopeToACPJsonrpcCall 将信封转为 JSON-RPC 风格单条调用描述（日志或下游 ACP 端点）。
// 若 envelope.Method 为网关 RPC（如 chat.send），输出中的 method 对纯 ACP 端可能无效，
// 需在业务层先映射为 ACP method（如 session/prompt）再发送。
// Python: adapters.py envelope_to_acp_jsonrpc_call
func EnvelopeToACPJsonrpcCall(env *E2AEnvelope) map[string]any {
	method := env.Method
	if env.Method == "ext" && env.ExtMethod != "" {
		method = env.ExtMethod
	}
	var params map[string]any
	if env.Method == "session/prompt" {
		params = MergeParamsToACPPrompt(env)
	} else {
		params = make(map[string]any, len(env.Params))
		for k, v := range env.Params {
			params[k] = v
		}
	}
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      env.JSONRPCID,
		"method":  method,
		"params":  params,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// orDefaultString 空串回退默认值（Python `converter or _CONVERTER_ACP` 的 or 语义）。
func orDefaultString(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// orDefaultIdentityOrigin 零值回退 IdentityOriginUser（Python 默认 IdentityOrigin.USER）。
func orDefaultIdentityOrigin(o IdentityOrigin) IdentityOrigin {
	if o == "" {
		return IdentityOriginUser
	}
	return o
}

// randomHex12 12 位随机 hex（uuid4().hex[:12] 等价；24d randomAcpHex12 同款逻辑，
// 包内私有实现不跨包复用，避免 e2a→tools 反向依赖）。
func randomHex12() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
