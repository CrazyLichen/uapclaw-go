package e2a

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── EnvelopeFromACPJsonrpc ────────────────────────────

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

// ──────────────────────────── EnvelopeFromA2ASendMessage ────────────────────────────

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

// ──────────────────────────── EnvelopeToACPJsonrpcCall ────────────────────────────

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

// 暂留 import 引用（Task 5 消费；Go 未使用 import 编译报错，先以 var _ 锚定）
var (
	_ = strings.TrimSpace
	_ = json.Marshal
	_ = schema.MessageTypeReq
)
