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

// 暂留 import 引用（Task 5 消费；Go 未使用 import 编译报错，先以 var _ 锚定）
var (
	_ = strings.TrimSpace
	_ = json.Marshal
	_ = schema.MessageTypeReq
)
