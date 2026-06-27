package e2a

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// E2AEnvelope E2A 统一信封：单结构兼容多协议入口，由网关或适配层解析后调用 Agent。
//
// 基础字段：
//   - ProtocolVersion：E2A 载荷版本。
//   - Provenance：出处（原生 e2a 或由 acp / a2a 等转换）。
//   - RequestID：网关↔AgentServer 主请求 id（流式 chunk 关联）。
//   - JSONRPCID / CorrelationID：JSON-RPC id、分布式追踪等（可与 RequestID 并存）。
//   - TaskID / ContextID / SessionID / MessageID：对齐 A2A / ACP 侧概念。
//   - IsStream：是否流式响应。
//
// 事件语义：
//   - Method：网关 RPC（如 chat.send）或 ACP 转入时的 JSON-RPC method；ext + ExtMethod 用于自定义。
//   - Params：唯一业务参数字典（JSON-RPC params、用户正文、content_blocks、附件列表等均放此处）。
//
// 通道与互操作：
//   - ChannelContext：可选溢出；主路径上通道侧信息应在网关入口映射为规范化字段。
//   - A2AMetadata / ACPMeta：与 A2A/ACP 互操作时使用。
//
// 对应 Python: jiuwenswarm/common/e2a/models.py (E2AEnvelope)
type E2AEnvelope struct {
	// ─── 基础 / 关联 ───
	// ProtocolVersion E2A 载荷版本（默认 "1.0"）
	ProtocolVersion string `json:"protocol_version"`
	// Provenance 出处（原生 e2a 或由 acp / a2a 等转换）
	Provenance E2AProvenance `json:"provenance"`
	// RequestID 网关↔AgentServer 主请求 id（流式 chunk 关联）
	RequestID string `json:"request_id"`
	// JSONRPCID JSON-RPC id（str / int / None）
	JSONRPCID any `json:"jsonrpc_id"`
	// CorrelationID 分布式追踪 id
	CorrelationID string `json:"correlation_id"`
	// TaskID 对齐 A2A task id
	TaskID string `json:"task_id"`
	// ContextID 对齐 A2A context id
	ContextID string `json:"context_id"`
	// SessionID 会话 id
	SessionID string `json:"session_id"`
	// MessageID 消息 id
	MessageID string `json:"message_id"`

	// ─── 时间戳 ───
	// Timestamp RFC 3339 UTC 字符串；from_dict 可将历史 float 纪元秒规范化
	Timestamp string `json:"timestamp"`

	// ─── 身份与入口 ───
	// IdentityOrigin 身份来源（默认 user）
	IdentityOrigin IdentityOrigin `json:"identity_origin"`
	// Channel 渠道标识
	Channel string `json:"channel"`
	// UserID 用户 id
	UserID string `json:"user_id"`
	// ChatID 聊天 id
	ChatID string `json:"chat_id"`
	// SourceAgentID 来源 Agent id
	SourceAgentID string `json:"source_agent_id"`

	// ─── 网关 RPC ───
	// Method 网关 RPC 方法名（原 req_method）；ACP 转入时同字段承载 JSON-RPC method
	Method string `json:"method"`
	// Params 唯一业务参数字典
	Params map[string]any `json:"params"`
	// ExtMethod 扩展方法名
	ExtMethod string `json:"ext_method"`
	// SessionUpdateKind 会话更新类型
	SessionUpdateKind string `json:"session_update_kind"`
	// IsStream 是否流式响应
	IsStream bool `json:"is_stream"`

	// ─── 期望输出 ───
	// ExpectedOutputModes 对齐 A2A acceptedOutputModes
	ExpectedOutputModes []string `json:"expected_output_modes"`

	// ─── 鉴权 ───
	// Auth 身份鉴权信息
	Auth E2AAuth `json:"auth"`

	// ─── 扩展槽 ───
	// ChannelContext 通道上下文
	ChannelContext map[string]any `json:"channel_context"`
	// A2AMetadata A2A 互操作元数据
	A2AMetadata map[string]any `json:"a2a_metadata"`
	// ACPMeta ACP 互操作元数据
	ACPMeta map[string]any `json:"acp_meta"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// E2AProtocolVersion E2A 协议版本（对应 Python E2A_PROTOCOL_VERSION）
const E2AProtocolVersion = "1.0"

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// UTCNowISO 返回当前 UTC 时刻的 RFC 3339 字符串。
// 对应 Python: utc_now_iso()
func UTCNowISO() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// NewE2AEnvelope 创建 E2AEnvelope 实例，设置默认值。
// 默认值：ProtocolVersion="1.0", Provenance=NewE2AProvenance(), IdentityOrigin=USER
func NewE2AEnvelope() *E2AEnvelope {
	return &E2AEnvelope{
		ProtocolVersion: E2AProtocolVersion,
		Provenance:      *NewE2AProvenance(),
		IdentityOrigin:  IdentityOriginUser,
	}
}

// EnsureTimestamp 若未设置 Timestamp，则填当前 UTC ISO8601。
// 对应 Python: E2AEnvelope.ensure_timestamp()
func (e *E2AEnvelope) EnsureTimestamp() {
	if e.Timestamp == "" {
		e.Timestamp = UTCNowISO()
	}
}

// ToMap 序列化为 JSON 友好 map（枚举转为值）。
// 对应 Python: E2AEnvelope.to_dict() → _dataclass_to_json_dict()
func (e *E2AEnvelope) ToMap() map[string]any {
	return structToMap(e)
}

// EnvelopeFromMap 从 map 反序列化为 E2AEnvelope。
// 对应 Python: E2AEnvelope.from_dict(data) → _envelope_from_dict(data)
func EnvelopeFromMap(data map[string]any) *E2AEnvelope {
	// 1. provenance 解析 + legacy binding 迁移
	prov := provenanceFromMap(data["provenance"])
	prov = migrateLegacyBinding(data, prov)

	// 2. identity_origin 解析（默认 USER）
	origin := IdentityOriginUser
	if raw, exists := data["identity_origin"]; exists {
		if s, ok := raw.(string); ok && IsValidIdentityOrigin(s) {
			origin, _ = ParseIdentityOrigin(s)
		}
	}

	// 3. params 合并（含 legacy payload）
	params := paramsWithOptionalLegacyPayload(data)

	// 4. auth 解析（None / E2AAuth / map → E2AAuth）
	var auth E2AAuth
	if authRaw, exists := data["auth"]; exists && authRaw != nil {
		if a, ok := authRaw.(E2AAuth); ok {
			auth = a
		} else if m, ok := authRaw.(map[string]any); ok {
			auth = E2AAuth{
				MethodID:      getString(m, "method_id"),
				BearerToken:   getString(m, "bearer_token"),
				APIKeyRef:     getString(m, "api_key_ref"),
				CredentialRef: getString(m, "credential_ref"),
				ExtraHeaders:  getMapString(m, "extra_headers"),
				Meta:          getMapAny(m, "_meta"),
			}
		}
	}

	// 5. channel_context 合并顶层 metadata
	channelContext := getMapAny(data, "channel_context")
	if metaTop, ok := data["metadata"]; ok {
		if m, ok := metaTop.(map[string]any); ok && len(m) > 0 {
			for k, v := range m {
				if _, already := channelContext[k]; !already {
					channelContext[k] = v
				}
			}
		}
	}

	// 6. channel 兼容 channel_id
	ch := getString(data, "channel")
	if ch == "" {
		ch = getString(data, "channel_id")
	}

	// 7. method 兼容 req_method
	rawMethod := getString(data, "method")
	if rawMethod == "" {
		if rm, exists := data["req_method"]; exists {
			if s, ok := rm.(string); ok {
				rawMethod = s
			}
		}
	}

	// 8. 构造 E2AEnvelope
	return &E2AEnvelope{
		ProtocolVersion:     getStringWithDefault(data, "protocol_version", E2AProtocolVersion),
		Provenance:          prov,
		RequestID:           getString(data, "request_id"),
		JSONRPCID:           data["jsonrpc_id"],
		CorrelationID:       getString(data, "correlation_id"),
		TaskID:              getString(data, "task_id"),
		ContextID:           getString(data, "context_id"),
		SessionID:           getString(data, "session_id"),
		MessageID:           getString(data, "message_id"),
		Timestamp:           normalizeTimestampValue(data["timestamp"]),
		IdentityOrigin:      origin,
		Channel:             ch,
		UserID:              getString(data, "user_id"),
		ChatID:              getString(data, "chat_id"),
		SourceAgentID:       getString(data, "source_agent_id"),
		Method:              rawMethod,
		Params:              params,
		ExtMethod:           getString(data, "ext_method"),
		SessionUpdateKind:   getString(data, "session_update_kind"),
		IsStream:            getBool(data, "is_stream", false),
		ExpectedOutputModes: getStringSlice(data, "expected_output_modes"),
		Auth:                auth,
		ChannelContext:      channelContext,
		A2AMetadata:         getMapAny(data, "a2a_metadata"),
		ACPMeta:             getMapAny(data, "acp_meta"),
	}
}

// MergeParamsToACPPrompt 当 Method == "session/prompt" 时，从 Params 补全 ACP 所需 prompt，返回新参数字典。
//
// 优先级：
//  1. 已有 params.prompt 则不修改。
//  2. 否则若有 params.content_blocks（非空 list），用作 prompt。
//  3. 否则用 params.text、params.content、params.query 中第一个非空字符串生成单条 text ContentBlock。
//
// 随后按需补 session_id、params._meta（来自 ACPMeta）。
//
// 对应 Python: merge_params_to_acp_prompt(envelope)
func MergeParamsToACPPrompt(env *E2AEnvelope) map[string]any {
	p := make(map[string]any)
	for k, v := range env.Params {
		p[k] = v
	}
	if env.Method != "session/prompt" {
		return p
	}
	if _, has := p["prompt"]; has {
		return p
	}

	// 尝试 content_blocks → prompt
	var blocks []map[string]any
	if cb, ok := p["content_blocks"]; ok {
		if list, ok := cb.([]any); ok && len(list) > 0 {
			for _, item := range list {
				if m, ok := item.(map[string]any); ok {
					blocks = append(blocks, m)
				}
			}
		}
	}

	if len(blocks) == 0 {
		// 尝试 text / content / query
		text := ""
		if v, ok := p["text"]; ok {
			if s, ok := v.(string); ok {
				text = s
			}
		}
		if text == "" {
			if v, ok := p["content"]; ok {
				if s, ok := v.(string); ok {
					text = s
				}
			}
		}
		if text == "" {
			if v, ok := p["query"]; ok {
				if s, ok := v.(string); ok {
					text = s
				}
			}
		}
		if text != "" {
			blocks = append(blocks, map[string]any{"type": "text", "text": text})
		}
	}

	if len(blocks) > 0 {
		p["prompt"] = blocks
	}

	if env.SessionID != "" {
		if _, has := p["session_id"]; !has {
			p["session_id"] = env.SessionID
		}
	}
	if len(env.ACPMeta) > 0 {
		meta, _ := p["_meta"].(map[string]any)
		if meta == nil {
			meta = make(map[string]any)
		}
		for k, v := range env.ACPMeta {
			meta[k] = v
		}
		p["_meta"] = meta
	}
	return p
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// structToMap 将结构体递归转换为 map[string]any（枚举转为值）。
// 使用 json.Marshal → json.Unmarshal 中转，比反射递归更简洁可靠。
// 对应 Python: _dataclass_to_json_dict(obj)
func structToMap(v any) map[string]any {
	data, err := json.Marshal(v)
	if err != nil {
		return map[string]any{}
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return map[string]any{}
	}
	return result
}

// provenanceFromMap 从 raw 解析 E2AProvenance。
// 对应 Python: _provenance_from_dict(raw)
func provenanceFromMap(raw any) E2AProvenance {
	if raw == nil {
		return *NewE2AProvenance()
	}
	if p, ok := raw.(E2AProvenance); ok {
		return p
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return *NewE2AProvenance()
	}
	return E2AProvenance{
		SourceProtocol: getStringWithDefault(m, "source_protocol", E2ASourceProtocolE2A),
		Converter:      getString(m, "converter"),
		ConvertedAt:    getString(m, "converted_at"),
		Details:        getMapAny(m, "details"),
	}
}

// normalizeTimestampValue 规范为 RFC 3339 UTC 字符串；接受 str 或历史 float/int 纪元秒。
// 对应 Python: _normalize_timestamp_value(raw)
func normalizeTimestampValue(raw any) string {
	if raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return v
	case float64:
		return time.Unix(int64(v), 0).UTC().Format(time.RFC3339)
	case int:
		return time.Unix(int64(v), 0).UTC().Format(time.RFC3339)
	case int64:
		return time.Unix(v, 0).UTC().Format(time.RFC3339)
	default:
		return fmt.Sprintf("%v", raw)
	}
}

// migrateLegacyBinding 旧版 binding 字段迁入 provenance.details，避免丢失信息。
// 对应 Python: _migrate_legacy_binding(data, prov)
func migrateLegacyBinding(data map[string]any, prov E2AProvenance) E2AProvenance {
	legacy, exists := data["binding"]
	if !exists || legacy == nil {
		return prov
	}
	if _, migrated := prov.Details["migrated_from_binding"]; migrated {
		return prov
	}

	// 如果 binding 是 dict 且有 "value" 键，取 value
	if m, ok := legacy.(map[string]any); ok {
		if v, hasVal := m["value"]; hasVal {
			legacy = v
		}
	}

	legacyS := fmt.Sprintf("%v", legacy)
	d := make(map[string]any)
	for k, v := range prov.Details {
		d[k] = v
	}
	d["migrated_from_binding"] = legacyS

	sp := prov.SourceProtocol
	if sp == E2ASourceProtocolE2A {
		switch legacyS {
		case E2ASourceProtocolACP:
			sp = E2ASourceProtocolACP
		case E2ASourceProtocolA2A:
			sp = E2ASourceProtocolA2A
		case "internal", "hybrid":
			sp = E2ASourceProtocolE2A
		}
	}

	return E2AProvenance{
		SourceProtocol: sp,
		Converter:      prov.Converter,
		ConvertedAt:    prov.ConvertedAt,
		Details:        d,
	}
}

// paramsWithOptionalLegacyPayload 以 params 为真源；若存在顶层 payload 对象，将其键合并进 params（不覆盖已有键）。
// 对应 Python: _params_with_optional_legacy_payload(data)
func paramsWithOptionalLegacyPayload(data map[string]any) map[string]any {
	p := getMapAny(data, "params")
	raw, exists := data["payload"]
	if !exists {
		return p
	}
	m, ok := raw.(map[string]any)
	if !ok || len(m) == 0 {
		return p
	}
	for k, v := range m {
		if _, already := p[k]; already {
			continue
		}
		if v == nil {
			continue
		}
		if isEmptySliceOrMap(v) {
			continue
		}
		p[k] = v
	}
	return p
}

// getString 安全取字符串，不存在返回 ""
func getString(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// getStringWithDefault 带默认值取字符串
func getStringWithDefault(m map[string]any, key string, def string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return def
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// getStringOrEmpty 取字符串，nil→""
func getStringOrEmpty(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// getBool 取布尔值
func getBool(m map[string]any, key string, def bool) bool {
	v, ok := m[key]
	if !ok || v == nil {
		return def
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}

// getInt 取整数
func getInt(m map[string]any, key string, def int) int {
	v, ok := m[key]
	if !ok || v == nil {
		return def
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		if f, err := strconv.ParseFloat(fmt.Sprintf("%v", v), 64); err == nil {
			return int(f)
		}
		return def
	}
}

// getMapAny 取通用 map，nil→空 map
func getMapAny(m map[string]any, key string) map[string]any {
	v, ok := m[key]
	if !ok || v == nil {
		return map[string]any{}
	}
	if m2, ok := v.(map[string]any); ok {
		return m2
	}
	return map[string]any{}
}

// getMapString 取字符串 map
func getMapString(m map[string]any, key string) map[string]string {
	v, ok := m[key]
	if !ok || v == nil {
		return map[string]string{}
	}
	if m2, ok := v.(map[string]any); ok {
		result := make(map[string]string, len(m2))
		for k, val := range m2 {
			if s, ok := val.(string); ok {
				result[k] = s
			}
		}
		return result
	}
	return map[string]string{}
}

// getStringSlice 取字符串切片
func getStringSlice(m map[string]any, key string) []string {
	v, ok := m[key]
	if !ok || v == nil {
		return []string{}
	}
	if slice, ok := v.([]any); ok {
		result := make([]string, 0, len(slice))
		for _, item := range slice {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	if slice, ok := v.([]string); ok {
		return slice
	}
	return []string{}
}

// isEmptySliceOrMap 判断空切片/空 map（对应 Python: v == [] or v == {}）
func isEmptySliceOrMap(v any) bool {
	switch val := v.(type) {
	case []any:
		return len(val) == 0
	case map[string]any:
		return len(val) == 0
	}
	return false
}
