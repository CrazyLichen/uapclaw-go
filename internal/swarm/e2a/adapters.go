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
	"encoding/json"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
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

// a2aStreamOuterKeys A2A 流外层键白名单（projections 快路径判定用）。
var a2aStreamOuterKeys = map[string]struct{}{
	"task":           {},
	"message":        {},
	"statusUpdate":   {},
	"artifactUpdate": {},
}

// a2aStreamBranchKeyMap branch → A2A 外层键映射。
var a2aStreamBranchKeyMap = map[string]string{
	"task":            "task",
	"message":         "message",
	"status_update":   "statusUpdate",
	"artifact_update": "artifactUpdate",
}

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
		"kind":               "a2a_send_message",
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

// E2AResponseToACPJsonrpcResponse 将 E2AResponse 转为单条 JSON-RPC 2.0 响应对象
// （仅 result 或 error，无 method）。不适用时返回 nil。
//
// 优先使用 projections.acp：若为已组装的完整响应（含 jsonrpc 与 result/error），原样返回副本。
// 否则按 response_kind 从 body 构造：
//   - acp.prompt_result → result = body
//   - acp.jsonrpc_error → error = body（须含 JSON-RPC 所需字段）
//   - e2a.error → error：code 非 int 时用 -32603，原值写入 data.e2a_code
//
// Python: adapters.py e2a_response_to_acp_jsonrpc_response
func E2AResponseToACPJsonrpcResponse(resp *E2AResponse) map[string]any {
	// projections 快路径：jsonrpc=="2.0" 且含 result 或 error → 副本 + setdefault("id")
	if projRaw, ok := resp.Projections["acp"]; ok {
		if proj, ok := projRaw.(map[string]any); ok {
			if s, _ := proj["jsonrpc"].(string); s == "2.0" {
				_, hasResult := proj["result"]
				_, hasError := proj["error"]
				if hasResult || hasError {
					return copyWithDefaultID(proj, resp.JSONRPCID)
				}
			}
		}
	}

	rpcID := resp.JSONRPCID
	kind := resp.ResponseKind
	// body = dict(response.body or {})
	body := make(map[string]any, len(resp.Body))
	for k, v := range resp.Body {
		body[k] = v
	}

	if kind == E2AResponseKindACPPromptResult {
		return map[string]any{"jsonrpc": "2.0", "id": rpcID, "result": body}
	}
	if kind == E2AResponseKindACPJSONRPCError {
		return map[string]any{"jsonrpc": "2.0", "id": rpcID, "error": body}
	}
	if kind == E2AResponseKindE2AError {
		codeRaw := body["code"]
		code, isInt := resolveJsonrpcErrorCode(codeRaw)
		message, _ := body["message"].(string)
		data := map[string]any{}
		if det, has := body["details"]; has {
			data["details"] = det
		}
		if ext, has := body["external"]; has {
			data["external"] = ext
		}
		// Python: if code_raw is not None and not isinstance(code_raw, int): data["e2a_code"] = code_raw
		// bool 是 int 子类不写；合法 int（含 -32603 本身）不写——用 isInt 标志判定，禁止 code 值反推
		if codeRaw != nil && !isInt {
			data["e2a_code"] = codeRaw
		}
		err := map[string]any{"code": code, "message": message}
		if len(data) > 0 {
			err["data"] = data
		}
		return map[string]any{"jsonrpc": "2.0", "id": rpcID, "error": err}
	}
	return nil
}

// E2AResponseToA2AStreamPayload 将 response_kind == "a2a.stream_event" 的 E2AResponse 转为
// A2A StreamResponse 形 JSON：外层键为 task/message/statusUpdate/artifactUpdate 之一。
// 若 projections.a2a 已为四选一单键对象，则原样返回副本。
// body.branch 须为 E2AA2AStreamBranches 之一；body.payload 为对应分支对象。
// 不适用时返回 nil。
// Python: adapters.py e2a_response_to_a2a_stream_payload
func E2AResponseToA2AStreamPayload(resp *E2AResponse) map[string]any {
	if resp.ResponseKind != E2AResponseKindA2AStreamEvent {
		return nil
	}
	// projections 快路径：单键且键在白名单 → 原样副本
	if projRaw, ok := resp.Projections["a2a"]; ok {
		if proj, ok := projRaw.(map[string]any); ok && len(proj) == 1 {
			for key := range proj {
				if _, ok := a2aStreamOuterKeys[key]; ok {
					return map[string]any{key: proj[key]}
				}
			}
		}
	}
	// body 路径：branch → 外键映射；payload 须为 map
	body := resp.Body
	branch, ok := body["branch"].(string)
	if !ok || !isA2AStreamBranch(branch) {
		return nil
	}
	payload, ok := body["payload"].(map[string]any)
	if !ok {
		return nil
	}
	return map[string]any{a2aStreamBranchKeyMap[branch]: payload}
}

// BuildACPToolResponseMessage 为客户端 JSON-RPC 响应（工具执行结果）构建内部 Message。
// 由 ACP 桥（WebSocket 模式）与 stdio 模式共享，避免重复的 Message 构造逻辑。
// Python: adapters.py build_acp_tool_response_message
func BuildACPToolResponseMessage(jsonrpcID string, responseData map[string]any, sessionID, channelID string) *schema.Message {
	params := map[string]any{
		"jsonrpc_id": jsonrpcID,
		"response":   copyMapAny(responseData),
		"session_id": sessionID,
	}
	raw, _ := json.Marshal(params)
	return &schema.Message{
		ID:              "acp_tool_resp_" + randomHex12(),
		Type:            schema.MessageTypeReq,
		ChannelID:       channelID,
		SessionID:       sessionID,
		Params:          raw,
		Timestamp:       schema.NowTimestamp(),
		OK:              true,
		ReqMethod:       schema.ReqMethodACPToolResponse,
		IsStream:        false,
		EnableStreaming: true, // Python Message dataclass 默认 enable_streaming=True
		Metadata: map[string]any{
			"acp": map[string]any{
				"jsonrpc_id": jsonrpcID,
				"kind":       "tool_response",
			},
		},
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

// copyWithDefaultID 复制 map 并在缺 id 键时补默认（Python dict(setdefault) 语义）。
func copyWithDefaultID(src map[string]any, defID any) map[string]any {
	out := make(map[string]any, len(src)+1)
	for k, v := range src {
		out[k] = v
	}
	if _, has := out["id"]; !has {
		out["id"] = defID
	}
	return out
}

// resolveJsonrpcErrorCode e2a.error 的 code 判定链（路线 X：Go JSON 解码数字统一 float64，
// 无法区分字面 1 与 1.0，float64 整数值视为 int 命中——Python json.loads("1")→int 一致）：
// int/int64 → 直接用；float64 整数值 → int；bool → 0/1（Python bool 是 int 子类）；
// 其余（float64 非整数/字符串/nil）→ -32603。
// 返回 (code, isInt)：isInt 表示 Python isinstance(code_raw, int) 命中，
// 调用方以 isInt 判定是否写 data.e2a_code（禁止用 code == -32603 反推——合法 int -32603 会误写）。
func resolveJsonrpcErrorCode(codeRaw any) (code int, isInt bool) {
	switch v := codeRaw.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		if v == float64(int64(v)) {
			return int(v), true
		}
		return -32603, false
	case bool:
		if v {
			return 1, true
		}
		return 0, true
	default:
		return -32603, false
	}
}

// isA2AStreamBranch 判定 branch 是否为合法 A2A 流分支（E2AA2AStreamBranches 查找）。
func isA2AStreamBranch(branch string) bool {
	for _, b := range E2AA2AStreamBranches {
		if b == branch {
			return true
		}
	}
	return false
}

// copyMapAny 浅拷贝 map（Python dict(...) 语义）。
func copyMapAny(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
