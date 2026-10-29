package e2a

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── EnvelopeFromACPJsonrpc ACP 入站信封构造 ────────────────────────────

// TestEnvelopeFromACPJsonrpc_ACP入站基础 基本四字段 provenance + 信封字段。
func TestEnvelopeFromACPJsonrpc_ACP入站基础(t *testing.T) {
	env := EnvelopeFromACPJsonrpc("session/prompt", map[string]any{"content": "hi"}, &EnvelopeFromACPOptions{
		JSONRPCID: "rpc-1",
		SessionID: "s1",
		Channel:   "acp",
	})
	if env.Method != "session/prompt" {
		t.Errorf("Method = %q, want session/prompt", env.Method)
	}
	if env.JSONRPCID != "rpc-1" {
		t.Errorf("JSONRPCID = %v, want rpc-1", env.JSONRPCID)
	}
	if env.SessionID != "s1" {
		t.Errorf("SessionID = %q, want s1", env.SessionID)
	}
	if env.Channel != "acp" {
		t.Errorf("Channel = %q, want acp", env.Channel)
	}
	if env.Params["content"] != "hi" {
		t.Errorf("Params[content] = %v, want hi", env.Params["content"])
	}
	prov := env.Provenance
	if prov.SourceProtocol != E2ASourceProtocolACP {
		t.Errorf("SourceProtocol = %q, want %q", prov.SourceProtocol, E2ASourceProtocolACP)
	}
	if prov.Converter != converterACP {
		t.Errorf("Converter = %q, want %q", prov.Converter, converterACP)
	}
	if prov.ConvertedAt == "" {
		t.Error("ConvertedAt 为空")
	}
	if prov.Details["kind"] != "jsonrpc_request" {
		t.Errorf("Details[kind] = %v, want jsonrpc_request", prov.Details["kind"])
	}
	if prov.Details["jsonrpc_method"] != "session/prompt" {
		t.Errorf("Details[jsonrpc_method] = %v, want session/prompt", prov.Details["jsonrpc_method"])
	}
	if prov.Details["jsonrpc_id"] != "rpc-1" {
		t.Errorf("Details[jsonrpc_id] = %v, want rpc-1", prov.Details["jsonrpc_id"])
	}
}

// TestEnvelopeFromACPJsonrpc_jsonrpcID缺省 details 不含 jsonrpc_id 键。
func TestEnvelopeFromACPJsonrpc_jsonrpcID缺省(t *testing.T) {
	env := EnvelopeFromACPJsonrpc("initialize", nil, nil)
	if _, has := env.Provenance.Details["jsonrpc_id"]; has {
		t.Error("Details 不应含 jsonrpc_id 键")
	}
}

// TestEnvelopeFromACPJsonrpc_会话兜底链 SessionID 三级兜底：opts 优先 → params 查询 → 非 str 置空。
func TestEnvelopeFromACPJsonrpc_会话兜底链(t *testing.T) {
	// opts 优先（params 里也有但被覆盖）
	env1 := EnvelopeFromACPJsonrpc("m", map[string]any{"session_id": "from-params"}, &EnvelopeFromACPOptions{SessionID: "from-opts"})
	if env1.SessionID != "from-opts" {
		t.Errorf("SessionID = %q, want from-opts", env1.SessionID)
	}
	// opts 空 → params 兜底
	env2 := EnvelopeFromACPJsonrpc("m", map[string]any{"session_id": "from-params"}, nil)
	if env2.SessionID != "from-params" {
		t.Errorf("SessionID = %q, want from-params", env2.SessionID)
	}
	// params 里是数字 → 置空
	env3 := EnvelopeFromACPJsonrpc("m", map[string]any{"session_id": 42}, nil)
	if env3.SessionID != "" {
		t.Errorf("SessionID = %q, want 空串", env3.SessionID)
	}
	// 两者都空
	env4 := EnvelopeFromACPJsonrpc("m", nil, nil)
	if env4.SessionID != "" {
		t.Errorf("SessionID = %q, want 空串", env4.SessionID)
	}
}

// TestEnvelopeFromACPJsonrpc_身份来源默认与显式。
func TestEnvelopeFromACPJsonrpc_身份来源(t *testing.T) {
	env1 := EnvelopeFromACPJsonrpc("m", nil, nil)
	if env1.IdentityOrigin != IdentityOriginUser {
		t.Errorf("默认 IdentityOrigin = %q, want user", env1.IdentityOrigin)
	}
	env2 := EnvelopeFromACPJsonrpc("m", nil, &EnvelopeFromACPOptions{IdentityOrigin: IdentityOriginAgent})
	if env2.IdentityOrigin != IdentityOriginAgent {
		t.Errorf("显式 IdentityOrigin = %q, want agent", env2.IdentityOrigin)
	}
}

// TestEnvelopeFromACPJsonrpc_converter默认与显式。
func TestEnvelopeFromACPJsonrpc_converter(t *testing.T) {
	env1 := EnvelopeFromACPJsonrpc("m", nil, nil)
	if env1.Provenance.Converter != converterACP {
		t.Errorf("默认 Converter = %q, want %q", env1.Provenance.Converter, converterACP)
	}
	env2 := EnvelopeFromACPJsonrpc("m", nil, &EnvelopeFromACPOptions{Converter: "custom.conv"})
	if env2.Provenance.Converter != "custom.conv" {
		t.Errorf("显式 Converter = %q, want custom.conv", env2.Provenance.Converter)
	}
}

// TestEnvelopeFromACPJsonrpc_extra覆盖既有键 extra_provenance_details 的 dict.update 语义。
func TestEnvelopeFromACPJsonrpc_extra覆盖既有键(t *testing.T) {
	env := EnvelopeFromACPJsonrpc("m", nil, &EnvelopeFromACPOptions{
		ExtraProvenanceDetails: map[string]any{"kind": "overridden", "custom": "v"},
	})
	if env.Provenance.Details["kind"] != "overridden" {
		t.Errorf("Details[kind] = %v, want overridden（extra 可覆盖）", env.Provenance.Details["kind"])
	}
	if env.Provenance.Details["custom"] != "v" {
		t.Errorf("Details[custom] = %v, want v", env.Provenance.Details["custom"])
	}
	if env.Provenance.Details["jsonrpc_method"] != "m" {
		t.Errorf("Details[jsonrpc_method] = %v, want m", env.Provenance.Details["jsonrpc_method"])
	}
}

// TestEnvelopeFromACPJsonrpc_params拷贝 传入 params 不被外部修改污染（dict(params or {}) 拷贝语义）。
func TestEnvelopeFromACPJsonrpc_params拷贝(t *testing.T) {
	src := map[string]any{"k": "v"}
	env := EnvelopeFromACPJsonrpc("m", src, nil)
	src["k"] = "mutated"
	if env.Params["k"] != "v" {
		t.Errorf("Params 应是拷贝，k = %v, want v", env.Params["k"])
	}
}

// TestEnvelopeFromACPJsonrpc_params为nil容错。
func TestEnvelopeFromACPJsonrpc_params为nil(t *testing.T) {
	env := EnvelopeFromACPJsonrpc("m", nil, nil)
	if env.Params == nil || len(env.Params) != 0 {
		t.Errorf("params nil 时应得空 map，实际 %v", env.Params)
	}
}

// ──────────────────────────── EnvelopeFromA2ASendMessage A2A 入站信封构造 ────────────────────────────

// TestEnvelopeFromA2ASendMessage_A2A入站基础 method 恒 session/prompt + provenance + 字段透传。
func TestEnvelopeFromA2ASendMessage_A2A入站基础(t *testing.T) {
	env := EnvelopeFromA2ASendMessage("task-1", "ctx-1", map[string]any{"role": "user", "parts": []any{}},
		&EnvelopeFromA2ASendMessageOptions{
			Metadata:      map[string]any{"k": "v"},
			Configuration: map[string]any{"acceptedOutputModes": []any{"text", "file"}},
			Channel:       "a2a",
		})
	if env.Method != "session/prompt" {
		t.Errorf("Method = %q, want session/prompt（恒定）", env.Method)
	}
	if env.TaskID != "task-1" || env.ContextID != "ctx-1" {
		t.Errorf("TaskID/ContextID = %q/%q, want task-1/ctx-1", env.TaskID, env.ContextID)
	}
	if env.Channel != "a2a" {
		t.Errorf("Channel = %q, want a2a", env.Channel)
	}
	if env.Provenance.SourceProtocol != E2ASourceProtocolA2A {
		t.Errorf("SourceProtocol = %q, want %q", env.Provenance.SourceProtocol, E2ASourceProtocolA2A)
	}
	if env.Provenance.Converter != converterA2A {
		t.Errorf("Converter = %q, want %q", env.Provenance.Converter, converterA2A)
	}
	if env.Provenance.Details["kind"] != "a2a_send_message" {
		t.Errorf("Details[kind] = %v, want a2a_send_message", env.Provenance.Details["kind"])
	}
	if env.Provenance.Details["abstract_operation"] != "SendMessage" {
		t.Errorf("Details[abstract_operation] = %v, want SendMessage", env.Provenance.Details["abstract_operation"])
	}
	// ExpectedOutputModes 提取
	if len(env.ExpectedOutputModes) != 2 || env.ExpectedOutputModes[0] != "text" || env.ExpectedOutputModes[1] != "file" {
		t.Errorf("ExpectedOutputModes = %v, want [text file]", env.ExpectedOutputModes)
	}
	// params 双键
	msg, _ := env.Params["message"].(map[string]any)
	if msg == nil || msg["role"] != "user" {
		t.Errorf("Params[message] = %v, want 含 role=user", env.Params["message"])
	}
	cfg, _ := env.Params["configuration"].(map[string]any)
	if cfg == nil || cfg["acceptedOutputModes"] == nil {
		t.Error("Params[configuration] 缺失")
	}
	// a2a_metadata
	if env.A2AMetadata["k"] != "v" {
		t.Errorf("A2AMetadata[k] = %v, want v", env.A2AMetadata["k"])
	}
}

// TestEnvelopeFromA2ASendMessage_配置三态 acceptedOutputModes 提取。
func TestEnvelopeFromA2ASendMessage_配置三态(t *testing.T) {
	// 非 list（字符串）→ 忽略，ExpectedOutputModes 为空
	env1 := EnvelopeFromA2ASendMessage("", "", map[string]any{},
		&EnvelopeFromA2ASendMessageOptions{Configuration: map[string]any{"acceptedOutputModes": "text"}})
	if len(env1.ExpectedOutputModes) != 0 {
		t.Errorf("非 list 时 ExpectedOutputModes 应空，实际 %v", env1.ExpectedOutputModes)
	}
	// configuration nil → 空 map 进 params、ExpectedOutputModes 空
	env2 := EnvelopeFromA2ASendMessage("", "", map[string]any{}, nil)
	if env2.Params["configuration"] == nil {
		t.Error("Params[configuration] 应恒存在（configuration or {}）")
	}
	cfg2, _ := env2.Params["configuration"].(map[string]any)
	if cfg2 == nil || len(cfg2) != 0 {
		t.Errorf("configuration nil 时 Params[configuration] 应空 map，实际 %v", env2.Params["configuration"])
	}
	// list 含非字符串元素 → str() 强转
	env3 := EnvelopeFromA2ASendMessage("", "", map[string]any{},
		&EnvelopeFromA2ASendMessageOptions{Configuration: map[string]any{"acceptedOutputModes": []any{42, true}}})
	if len(env3.ExpectedOutputModes) != 2 || env3.ExpectedOutputModes[0] != "42" || env3.ExpectedOutputModes[1] != "true" {
		t.Errorf("str() 强转 ExpectedOutputModes = %v, want [42 true]", env3.ExpectedOutputModes)
	}
}

// TestEnvelopeFromA2ASendMessage_metadata为nil空map。
func TestEnvelopeFromA2ASendMessage_metadata为nil(t *testing.T) {
	env := EnvelopeFromA2ASendMessage("", "", map[string]any{}, nil)
	if env.A2AMetadata == nil || len(env.A2AMetadata) != 0 {
		t.Errorf("metadata nil → a2a_metadata 应空 map，实际 %v", env.A2AMetadata)
	}
}

// TestEnvelopeFromA2ASendMessage_options为nil全默认。
func TestEnvelopeFromA2ASendMessage_options为nil(t *testing.T) {
	env := EnvelopeFromA2ASendMessage("", "", map[string]any{}, nil)
	if env.IdentityOrigin != IdentityOriginUser {
		t.Errorf("IdentityOrigin = %q, want user（默认）", env.IdentityOrigin)
	}
	if env.Provenance.Converter != converterA2A {
		t.Errorf("Converter = %q, want %q", env.Provenance.Converter, converterA2A)
	}
	if env.Channel != "" {
		t.Errorf("Channel = %q, want 空串", env.Channel)
	}
}

// TestEnvelopeFromA2ASendMessage_taskContext透传 空串保留。
func TestEnvelopeFromA2ASendMessage_taskContext透传(t *testing.T) {
	env := EnvelopeFromA2ASendMessage("", "ctx-9", map[string]any{}, nil)
	if env.TaskID != "" || env.ContextID != "ctx-9" {
		t.Errorf("TaskID/ContextID = %q/%q, want 空串/ctx-9", env.TaskID, env.ContextID)
	}
}

// TestEnvelopeFromA2ASendMessage_converter显式与身份来源显式。
func TestEnvelopeFromA2ASendMessage_converter显式(t *testing.T) {
	env := EnvelopeFromA2ASendMessage("", "", map[string]any{},
		&EnvelopeFromA2ASendMessageOptions{Converter: "cc", IdentityOrigin: IdentityOriginAgent})
	if env.Provenance.Converter != "cc" {
		t.Errorf("Converter = %q, want cc", env.Provenance.Converter)
	}
	if env.IdentityOrigin != IdentityOriginAgent {
		t.Errorf("IdentityOrigin = %q, want agent", env.IdentityOrigin)
	}
}

// TestEnvelopeFromA2ASendMessage_extra覆盖既有键。
func TestEnvelopeFromA2ASendMessage_extra覆盖既有键(t *testing.T) {
	env := EnvelopeFromA2ASendMessage("", "", map[string]any{},
		&EnvelopeFromA2ASendMessageOptions{ExtraProvenanceDetails: map[string]any{"kind": "x", "extra_key": 1}})
	if env.Provenance.Details["kind"] != "x" {
		t.Errorf("Details[kind] = %v, want x（可覆盖）", env.Provenance.Details["kind"])
	}
	if env.Provenance.Details["extra_key"] != 1 {
		t.Errorf("Details[extra_key] = %v, want 1", env.Provenance.Details["extra_key"])
	}
}

// ──────────────────────────── EnvelopeToACPJsonrpcCall 出站 JSON-RPC 调用 ────────────────────────────

// TestEnvelopeToACPJsonrpcCall_基础 输出恒四键 + method/params 映射。
func TestEnvelopeToACPJsonrpcCall_基础(t *testing.T) {
	env := EnvelopeFromACPJsonrpc("session/prompt", map[string]any{"content": "hi"}, &EnvelopeFromACPOptions{JSONRPCID: 7})
	out := EnvelopeToACPJsonrpcCall(env)
	if out["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc = %v, want 2.0", out["jsonrpc"])
	}
	if out["id"] != 7 {
		t.Errorf("id = %v, want 7（any 透传）", out["id"])
	}
	if out["method"] != "session/prompt" {
		t.Errorf("method = %v, want session/prompt", out["method"])
	}
	params, _ := out["params"].(map[string]any)
	if params == nil {
		t.Fatal("params 应为 map")
	}
	// session/prompt 走 MergeParamsToACPPrompt：content → prompt 块（返回类型 []map[string]any）
	prompt, _ := params["prompt"].([]map[string]any)
	if prompt == nil || len(prompt) != 1 || prompt[0]["type"] != "text" || prompt[0]["text"] != "hi" {
		t.Errorf("params[prompt] = %v, want 单块 text=hi", params["prompt"])
	}
}

// TestEnvelopeToACPJsonrpcCall_ext方法替换。
func TestEnvelopeToACPJsonrpcCall_ext方法替换(t *testing.T) {
	env := &E2AEnvelope{Method: "ext", ExtMethod: "custom/method", JSONRPCID: "id1"}
	out := EnvelopeToACPJsonrpcCall(env)
	if out["method"] != "custom/method" {
		t.Errorf("method = %v, want custom/method", out["method"])
	}
}

// TestEnvelopeToACPJsonrpcCall_非ext非prompt method 原样、params 拷贝。
func TestEnvelopeToACPJsonrpcCall_非ext非prompt(t *testing.T) {
	env := &E2AEnvelope{Method: "chat.send", Params: map[string]any{"query": "q"}}
	out := EnvelopeToACPJsonrpcCall(env)
	if out["method"] != "chat.send" {
		t.Errorf("method = %v, want chat.send", out["method"])
	}
	params, _ := out["params"].(map[string]any)
	if params == nil || params["query"] != "q" {
		t.Errorf("params = %v, want 含 query=q", out["params"])
	}
}

// TestEnvelopeToACPJsonrpcCall_ext无extMethod回退method。
func TestEnvelopeToACPJsonrpcCall_ext无extMethod回退(t *testing.T) {
	env := &E2AEnvelope{Method: "ext", ExtMethod: ""}
	out := EnvelopeToACPJsonrpcCall(env)
	if out["method"] != "ext" {
		t.Errorf("method = %v, want ext（ext_method 空回退）", out["method"])
	}
}

// ──────────────────────────── E2AResponseToACPJsonrpcResponse 响应转 JSON-RPC ────────────────────────────

// TestE2AResponseToACPJsonrpcResponse_projections快路径 已组装响应原样返回副本。
func TestE2AResponseToACPJsonrpcResponse_projections快路径(t *testing.T) {
	resp := &E2AResponse{
		ResponseKind: E2AResponseKindACPPromptResult,
		JSONRPCID:    "rid",
		Projections: map[string]any{
			"acp": map[string]any{"jsonrpc": "2.0", "id": "x", "result": map[string]any{"stop": map[string]any{"reason": "end_turn"}}},
		},
	}
	out := E2AResponseToACPJsonrpcResponse(resp)
	proj, _ := out["result"].(map[string]any)
	if proj == nil || proj["stop"] == nil {
		t.Error("projections 快路径应原样返回")
	}
	if out["id"] != "x" {
		t.Errorf("id = %v, want x", out["id"])
	}
	// 副本语义：改 out 不影响 resp.Projections
	out["result"] = "mutated"
	if resp.Projections["acp"].(map[string]any)["result"] == "mutated" {
		t.Error("应返回副本")
	}
}

// TestE2AResponseToACPJsonrpcResponse_projections缺id补全 setdefault("id")。
func TestE2AResponseToACPJsonrpcResponse_projections缺id补全(t *testing.T) {
	resp := &E2AResponse{
		ResponseKind: E2AResponseKindACPPromptResult,
		JSONRPCID:    "rid",
		Projections: map[string]any{
			"acp": map[string]any{"jsonrpc": "2.0", "result": "ok"},
		},
	}
	out := E2AResponseToACPJsonrpcResponse(resp)
	if out["id"] != "rid" {
		t.Errorf("id = %v, want rid（setdefault）", out["id"])
	}
}

// TestE2AResponseToACPJsonrpcResponse_projections无效走body projections 缺 result/error 或 jsonrpc 值错。
func TestE2AResponseToACPJsonrpcResponse_projections无效走body(t *testing.T) {
	// jsonrpc 值非 "2.0"
	resp1 := &E2AResponse{
		ResponseKind: E2AResponseKindACPPromptResult,
		JSONRPCID:    "r1",
		Body:         map[string]any{"final": "b1"},
		Projections:  map[string]any{"acp": map[string]any{"jsonrpc": "1.0", "result": "x"}},
	}
	out1 := E2AResponseToACPJsonrpcResponse(resp1)
	if out1 == nil {
		t.Fatal("应走 body 路径返回非 nil")
	}
	if b, _ := out1["result"].(map[string]any); b == nil || b["final"] != "b1" {
		t.Errorf("无效 projections 应走 body 路径，result = %v", out1["result"])
	}
	// projections.acp 非 dict
	resp2 := &E2AResponse{
		ResponseKind: E2AResponseKindACPPromptResult,
		JSONRPCID:    "r2",
		Body:         map[string]any{"final": "b2"},
		Projections:  map[string]any{"acp": "not-a-dict"},
	}
	out2 := E2AResponseToACPJsonrpcResponse(resp2)
	if out2 == nil {
		t.Fatal("应走 body 路径返回非 nil")
	}
	if b, _ := out2["result"].(map[string]any); b == nil || b["final"] != "b2" {
		t.Errorf("projections.acp 非 dict 应走 body 路径，result = %v", out2["result"])
	}
	// projections.acp 是 dict 且 jsonrpc=2.0 但既无 result 也无 error → 走 body
	resp3 := &E2AResponse{
		ResponseKind: E2AResponseKindACPPromptResult,
		JSONRPCID:    "r3",
		Body:         map[string]any{"final": "b3"},
		Projections:  map[string]any{"acp": map[string]any{"jsonrpc": "2.0"}},
	}
	out3 := E2AResponseToACPJsonrpcResponse(resp3)
	if b, _ := out3["result"].(map[string]any); b == nil || b["final"] != "b3" {
		t.Errorf("无 result/error 键应走 body 路径，result = %v", out3["result"])
	}
}

// TestE2AResponseToACPJsonrpcResponse_promptResultKind。
func TestE2AResponseToACPJsonrpcResponse_promptResultKind(t *testing.T) {
	resp := &E2AResponse{
		ResponseKind: E2AResponseKindACPPromptResult,
		JSONRPCID:    99,
		Body:         map[string]any{"stop": map[string]any{"reason": "end_turn"}},
	}
	out := E2AResponseToACPJsonrpcResponse(resp)
	if out["jsonrpc"] != "2.0" || out["id"] != 99 {
		t.Errorf("jsonrpc/id = %v/%v, want 2.0/99", out["jsonrpc"], out["id"])
	}
	stopWrap, _ := out["result"].(map[string]any)
	if stopWrap == nil {
		t.Fatalf("result = %v, want map", out["result"])
	}
	stop, _ := stopWrap["stop"].(map[string]any)
	if stop == nil || stop["reason"] != "end_turn" {
		t.Errorf("result.stop = %v, want reason=end_turn", stopWrap["stop"])
	}
}

// TestE2AResponseToACPJsonrpcResponse_jsonrpcErrorKind。
func TestE2AResponseToACPJsonrpcResponse_jsonrpcErrorKind(t *testing.T) {
	resp := &E2AResponse{
		ResponseKind: E2AResponseKindACPJSONRPCError,
		JSONRPCID:    "e1",
		Body:         map[string]any{"code": -32600, "message": "bad"},
	}
	out := E2AResponseToACPJsonrpcResponse(resp)
	errObj, _ := out["error"].(map[string]any)
	if errObj == nil || errObj["code"] != -32600 || errObj["message"] != "bad" {
		t.Errorf("error = %v, want code=-32600 message=bad", out["error"])
	}
	if _, has := out["result"]; has {
		t.Error("error 分支不应有 result 键")
	}
}

// TestE2AResponseToACPJsonrpcResponse_e2aErrorCode链 code 判定链（路线 X + isInt 标志）。
func TestE2AResponseToACPJsonrpcResponse_e2aErrorCode链(t *testing.T) {
	// int 直接用，不写 e2a_code
	resp1 := &E2AResponse{
		ResponseKind: E2AResponseKindE2AError,
		JSONRPCID:    "c1",
		Body:         map[string]any{"code": -32000, "message": "m1"},
	}
	out1 := E2AResponseToACPJsonrpcResponse(resp1)
	err1, _ := out1["error"].(map[string]any)
	if err1["code"] != -32000 {
		t.Errorf("int code = %v, want -32000", err1["code"])
	}
	if data, _ := err1["data"].(map[string]any); data != nil {
		if _, has := data["e2a_code"]; has {
			t.Error("int code 不应写 e2a_code")
		}
	}
	// 合法 int -32603 本身：code=-32603 但不写 e2a_code（isInt 挡）
	resp2 := &E2AResponse{
		ResponseKind: E2AResponseKindE2AError,
		JSONRPCID:    "c2",
		Body:         map[string]any{"code": -32603, "message": "m2"},
	}
	out2 := E2AResponseToACPJsonrpcResponse(resp2)
	err2, _ := out2["error"].(map[string]any)
	if err2["code"] != -32603 {
		t.Errorf("int -32603 code = %v, want -32603", err2["code"])
	}
	if data, _ := err2["data"].(map[string]any); data != nil {
		if _, has := data["e2a_code"]; has {
			t.Error("合法 int -32603 不应写 e2a_code（isInt 判定，非 code 值反推）")
		}
	}
	// float64 整数值（Go JSON 解码）→ 视为 int 命中（路线 X）
	resp3 := &E2AResponse{
		ResponseKind: E2AResponseKindE2AError,
		JSONRPCID:    "c3",
		Body:         map[string]any{"code": -32700.0, "message": "m3"},
	}
	out3 := E2AResponseToACPJsonrpcResponse(resp3)
	err3, _ := out3["error"].(map[string]any)
	if err3["code"] != -32700 {
		t.Errorf("float64 整数 code = %v, want -32700（路线 X）", err3["code"])
	}
	// float64 非整数 → -32603 + e2a_code
	resp4 := &E2AResponse{
		ResponseKind: E2AResponseKindE2AError,
		JSONRPCID:    "c4",
		Body:         map[string]any{"code": 1.5, "message": "m4"},
	}
	out4 := E2AResponseToACPJsonrpcResponse(resp4)
	err4, _ := out4["error"].(map[string]any)
	if err4["code"] != -32603 {
		t.Errorf("float64 非整数 code = %v, want -32603", err4["code"])
	}
	if data, _ := err4["data"].(map[string]any); data == nil || data["e2a_code"] != 1.5 {
		t.Errorf("float64 非整数应写 e2a_code=1.5, data = %v", err4["data"])
	}
	// bool → 0/1（Python bool 是 int 子类），不写 e2a_code
	resp5 := &E2AResponse{
		ResponseKind: E2AResponseKindE2AError,
		JSONRPCID:    "c5",
		Body:         map[string]any{"code": true, "message": "m5"},
	}
	out5 := E2AResponseToACPJsonrpcResponse(resp5)
	err5, _ := out5["error"].(map[string]any)
	if err5["code"] != 1 {
		t.Errorf("bool code = %v, want 1", err5["code"])
	}
	if data, _ := err5["data"].(map[string]any); data != nil {
		if _, has := data["e2a_code"]; has {
			t.Error("bool（Python int 子类）不应写 e2a_code")
		}
	}
	// code 缺失（nil）→ -32603，不写 e2a_code（code_raw is not None 挡）
	resp6 := &E2AResponse{
		ResponseKind: E2AResponseKindE2AError,
		JSONRPCID:    "c6",
		Body:         map[string]any{"message": "m6"},
	}
	out6 := E2AResponseToACPJsonrpcResponse(resp6)
	err6, _ := out6["error"].(map[string]any)
	if err6["code"] != -32603 {
		t.Errorf("nil code = %v, want -32603", err6["code"])
	}
	if data, _ := err6["data"].(map[string]any); data != nil {
		if _, has := data["e2a_code"]; has {
			t.Error("nil code 不应写 e2a_code")
		}
	}
	// int64（上游结构体构造场景）→ int 命中，不写 e2a_code
	resp7 := &E2AResponse{
		ResponseKind: E2AResponseKindE2AError,
		JSONRPCID:    "c7",
		Body:         map[string]any{"code": int64(-32001), "message": "m7"},
	}
	out7 := E2AResponseToACPJsonrpcResponse(resp7)
	err7, _ := out7["error"].(map[string]any)
	if err7["code"] != -32001 {
		t.Errorf("int64 code = %v, want -32001", err7["code"])
	}
	if data, _ := err7["data"].(map[string]any); data != nil {
		if _, has := data["e2a_code"]; has {
			t.Error("int64 code 不应写 e2a_code")
		}
	}

	// bool false（Python False 是 int 子类）→ 0 命中，不写 e2a_code
	resp8 := &E2AResponse{
		ResponseKind: E2AResponseKindE2AError,
		JSONRPCID:    "c8",
		Body:         map[string]any{"code": false, "message": "m8"},
	}
	out8 := E2AResponseToACPJsonrpcResponse(resp8)
	err8, _ := out8["error"].(map[string]any)
	if err8["code"] != 0 {
		t.Errorf("bool false code = %v, want 0", err8["code"])
	}
	if data, _ := err8["data"].(map[string]any); data != nil {
		if _, has := data["e2a_code"]; has {
			t.Error("bool false code 不应写 e2a_code")
		}
	}
}

// TestE2AResponseToACPJsonrpcResponse_e2aErrorData键 details/external 有无可选。
func TestE2AResponseToACPJsonrpcResponse_e2aErrorData键(t *testing.T) {
	// 无 details/external → 不含 data 键
	resp1 := &E2AResponse{
		ResponseKind: E2AResponseKindE2AError,
		JSONRPCID:    "d1",
		Body:         map[string]any{"code": 1, "message": "m"},
	}
	out1 := E2AResponseToACPJsonrpcResponse(resp1)
	err1, _ := out1["error"].(map[string]any)
	if _, has := err1["data"]; has {
		t.Error("无 details/external 时不应有 data 键")
	}
	// 有 details + external + 字符串 code → data.details/data.external/data.e2a_code
	resp2 := &E2AResponse{
		ResponseKind: E2AResponseKindE2AError,
		JSONRPCID:    "d2",
		Body: map[string]any{
			"code":     "E_STR",
			"message":  "m",
			"details":  map[string]any{"x": 1},
			"external": map[string]any{"y": 2},
		},
	}
	out2 := E2AResponseToACPJsonrpcResponse(resp2)
	err2, _ := out2["error"].(map[string]any)
	if err2["code"] != -32603 {
		t.Errorf("字符串 code → -32603, 实际 %v", err2["code"])
	}
	data, _ := err2["data"].(map[string]any)
	if data == nil || data["e2a_code"] != "E_STR" || data["details"] == nil || data["external"] == nil {
		t.Errorf("data = %v, want e2a_code=E_STR + details + external", err2["data"])
	}
	// details/external 值为 null（JSON null）→ 不写 data（Python det is not None 语义；
	// Go map 取值缺失与 null 同为零值 nil，值判定与 Python get() 天然等价——禁止键存在性判定）
	resp3 := &E2AResponse{
		ResponseKind: E2AResponseKindE2AError,
		JSONRPCID:    "d3",
		Body:         map[string]any{"code": 1, "message": "m", "details": nil, "external": nil},
	}
	out3 := E2AResponseToACPJsonrpcResponse(resp3)
	err3, _ := out3["error"].(map[string]any)
	if _, has := err3["data"]; has {
		t.Errorf("details/external 为 null 时不应有 data 键, data = %v", err3["data"])
	}
}

// TestE2AResponseToACPJsonrpcResponse_未知kind返回nil。
func TestE2AResponseToACPJsonrpcResponse_未知kind(t *testing.T) {
	resp := &E2AResponse{ResponseKind: "unknown.kind", JSONRPCID: "u"}
	if out := E2AResponseToACPJsonrpcResponse(resp); out != nil {
		t.Errorf("未知 kind 应返回 nil, 实际 %v", out)
	}
}

// ──────────────────────────── E2AResponseToA2AStreamPayload 响应转流式载荷 ────────────────────────────

// TestE2AResponseToA2AStreamPayload_非streamEvent返回nil。
func TestE2AResponseToA2AStreamPayload_非streamEvent(t *testing.T) {
	resp := &E2AResponse{ResponseKind: E2AResponseKindACPPromptResult}
	if out := E2AResponseToA2AStreamPayload(resp); out != nil {
		t.Errorf("非 stream_event 应 nil, 实际 %v", out)
	}
}

// TestE2AResponseToA2AStreamPayload_projections单键快路径。
func TestE2AResponseToA2AStreamPayload_projections单键快路径(t *testing.T) {
	resp := &E2AResponse{
		ResponseKind: E2AResponseKindA2AStreamEvent,
		Projections: map[string]any{
			"a2a": map[string]any{"statusUpdate": map[string]any{"state": "working"}},
		},
	}
	out := E2AResponseToA2AStreamPayload(resp)
	su, _ := out["statusUpdate"].(map[string]any)
	if su == nil || su["state"] != "working" {
		t.Errorf("statusUpdate = %v, want state=working", out)
	}
	// 副本语义
	out["statusUpdate"] = "mutated"
	if resp.Projections["a2a"].(map[string]any)["statusUpdate"] == "mutated" {
		t.Error("应返回副本")
	}
}

// TestE2AResponseToA2AStreamPayload_projections多键走body。
func TestE2AResponseToA2AStreamPayload_projections多键走body(t *testing.T) {
	resp := &E2AResponse{
		ResponseKind: E2AResponseKindA2AStreamEvent,
		Projections: map[string]any{
			"a2a": map[string]any{"task": map[string]any{"a": 1}, "message": map[string]any{"b": 2}},
		},
		Body: map[string]any{"branch": "status_update", "payload": map[string]any{"state": "working"}},
	}
	out := E2AResponseToA2AStreamPayload(resp)
	su, _ := out["statusUpdate"].(map[string]any)
	if su == nil || su["state"] != "working" {
		t.Errorf("多键 projections 走 body, statusUpdate = %v", out)
	}
}

// TestE2AResponseToA2AStreamPayload_四分支映射。
func TestE2AResponseToA2AStreamPayload_四分支映射(t *testing.T) {
	cases := []struct {
		branch string
		outer  string
	}{
		{"task", "task"},
		{"message", "message"},
		{"status_update", "statusUpdate"},
		{"artifact_update", "artifactUpdate"},
	}
	for _, tc := range cases {
		resp := &E2AResponse{
			ResponseKind: E2AResponseKindA2AStreamEvent,
			Body:         map[string]any{"branch": tc.branch, "payload": map[string]any{"k": "v"}},
		}
		out := E2AResponseToA2AStreamPayload(resp)
		if out == nil || out[tc.outer] == nil {
			t.Errorf("branch=%s → 外键 %s 缺失: %v", tc.branch, tc.outer, out)
		}
	}
}

// TestE2AResponseToA2AStreamPayload_branch非法或payload非map返回nil。
func TestE2AResponseToA2AStreamPayload_branch非法或payload非map(t *testing.T) {
	resp1 := &E2AResponse{
		ResponseKind: E2AResponseKindA2AStreamEvent,
		Body:         map[string]any{"branch": "bogus", "payload": map[string]any{}},
	}
	if out := E2AResponseToA2AStreamPayload(resp1); out != nil {
		t.Errorf("非法 branch 应 nil, 实际 %v", out)
	}
	resp2 := &E2AResponse{
		ResponseKind: E2AResponseKindA2AStreamEvent,
		Body:         map[string]any{"branch": "task", "payload": "not-a-map"},
	}
	if out := E2AResponseToA2AStreamPayload(resp2); out != nil {
		t.Errorf("payload 非 map 应 nil, 实际 %v", out)
	}
	resp3 := &E2AResponse{
		ResponseKind: E2AResponseKindA2AStreamEvent,
		Body:         map[string]any{"payload": map[string]any{}},
	}
	if out := E2AResponseToA2AStreamPayload(resp3); out != nil {
		t.Errorf("branch 缺失应 nil, 实际 %v", out)
	}
}

// TestE2AResponseToA2AStreamPayload_projections单键白名单外走body。
func TestE2AResponseToA2AStreamPayload_projections白名单外(t *testing.T) {
	resp := &E2AResponse{
		ResponseKind: E2AResponseKindA2AStreamEvent,
		Projections: map[string]any{
			"a2a": map[string]any{"bogus": map[string]any{"x": 1}},
		},
		Body: map[string]any{"branch": "task", "payload": map[string]any{"y": 2}},
	}
	out := E2AResponseToA2AStreamPayload(resp)
	if out == nil || out["task"] == nil {
		t.Errorf("白名单外单键走 body, out = %v", out)
	}
}

// TestE2AResponseToA2AStreamPayload_projections非dict走body。
func TestE2AResponseToA2AStreamPayload_projections非dict(t *testing.T) {
	resp := &E2AResponse{
		ResponseKind: E2AResponseKindA2AStreamEvent,
		Projections:  map[string]any{"a2a": "not-a-dict"},
		Body:         map[string]any{"branch": "task", "payload": map[string]any{"z": 3}},
	}
	out := E2AResponseToA2AStreamPayload(resp)
	if out == nil || out["task"] == nil {
		t.Errorf("projections 非 dict 走 body, out = %v", out)
	}
}

// TestE2AResponseToA2AStreamPayload_body缺失走projections body 无 branch 时 projections 单键生效。
func TestE2AResponseToA2AStreamPayload_body缺失(t *testing.T) {
	resp := &E2AResponse{
		ResponseKind: E2AResponseKindA2AStreamEvent,
		Projections: map[string]any{
			"a2a": map[string]any{"task": map[string]any{"id": "t1"}},
		},
	}
	out := E2AResponseToA2AStreamPayload(resp)
	task, _ := out["task"].(map[string]any)
	if task == nil || task["id"] != "t1" {
		t.Errorf("body 缺失时 projections 快路径, out = %v", out)
	}
}

// ──────────────────────────── BuildACPToolResponseMessage ACP 工具响应消息 ────────────────────────────

// TestBuildACPToolResponseMessage_全字段。
func TestBuildACPToolResponseMessage_全字段(t *testing.T) {
	msg := BuildACPToolResponseMessage("rpc-9", map[string]any{"result": "ok"}, "s1", "acp")
	if !strings.HasPrefix(msg.ID, "acp_tool_resp_") || len(msg.ID) != len("acp_tool_resp_")+12 {
		t.Errorf("ID = %q, want 前缀+12hex", msg.ID)
	}
	if msg.Type != schema.MessageTypeReq {
		t.Errorf("Type = %v, want req", msg.Type)
	}
	if msg.ChannelID != "acp" || msg.SessionID != "s1" {
		t.Errorf("ChannelID/SessionID = %q/%q", msg.ChannelID, msg.SessionID)
	}
	if !msg.OK {
		t.Error("OK 应为 true")
	}
	if msg.IsStream {
		t.Error("IsStream 应为 false")
	}
	if !msg.EnableStreaming {
		t.Error("EnableStreaming 应为 true（Python dataclass 默认 True）")
	}
	if msg.ReqMethod != schema.ReqMethodACPToolResponse {
		t.Errorf("ReqMethod = %v, want acp.tool_response", msg.ReqMethod)
	}
	var params map[string]any
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		t.Fatalf("Params 非法 JSON: %v", err)
	}
	if params["jsonrpc_id"] != "rpc-9" {
		t.Errorf("params[jsonrpc_id] = %v, want rpc-9", params["jsonrpc_id"])
	}
	resp, _ := params["response"].(map[string]any)
	if resp == nil || resp["result"] != "ok" {
		t.Errorf("params[response] = %v, want result=ok", params["response"])
	}
	if params["session_id"] != "s1" {
		t.Errorf("params[session_id] = %v, want s1", params["session_id"])
	}
	if msg.Timestamp <= 0 {
		t.Error("Timestamp 应为正")
	}
	meta, _ := msg.Metadata["acp"].(map[string]any)
	if meta == nil || meta["jsonrpc_id"] != "rpc-9" || meta["kind"] != "tool_response" {
		t.Errorf("Metadata[acp] = %v, want jsonrpc_id+kind=tool_response", msg.Metadata["acp"])
	}
}

// TestBuildACPToolResponseMessage_会话空串 sessionID 为空串时照常构造。
func TestBuildACPToolResponseMessage_会话空串(t *testing.T) {
	msg := BuildACPToolResponseMessage("rpc-1", map[string]any{}, "", "acp")
	if msg.SessionID != "" {
		t.Errorf("SessionID = %q, want 空串", msg.SessionID)
	}
	var params map[string]any
	_ = json.Unmarshal(msg.Params, &params)
	if params["session_id"] != "" {
		t.Errorf("params[session_id] = %v, want 空串", params["session_id"])
	}
}

// TestBuildACPToolResponseMessage_channel透传。
func TestBuildACPToolResponseMessage_channel透传(t *testing.T) {
	msg := BuildACPToolResponseMessage("r", map[string]any{}, "s", "web")
	if msg.ChannelID != "web" {
		t.Errorf("ChannelID = %q, want web", msg.ChannelID)
	}
}

// TestBuildACPToolResponseMessage_channel空串默认acp channel_id 零值回退默认（Python channel_id: str = "acp"）。
func TestBuildACPToolResponseMessage_channel空串默认acp(t *testing.T) {
	msg := BuildACPToolResponseMessage("r", map[string]any{}, "s", "")
	if msg.ChannelID != "acp" {
		t.Errorf("ChannelID = %q, want acp（零值回退，同 IdentityOrigin §9-5 模式）", msg.ChannelID)
	}
}
