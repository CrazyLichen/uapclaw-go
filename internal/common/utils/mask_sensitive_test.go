package utils

import (
	"strings"
	"testing"
)

// TestMaskSensitive_KV 正则 KV 场景（token=/api_key:/authorization=）。
func TestMaskSensitive_KV(t *testing.T) {
	out := MaskSensitive("token=abc123 api_key: sk-abcdef12 authorization = supersecret")
	if strings.Contains(out, "abc123") || strings.Contains(out, "sk-abcdef12") ||
		strings.Contains(out, "supersecret") {
		t.Fatalf("明文泄露: %s", out)
	}
	if !strings.Contains(out, "******") {
		t.Fatalf("应有掩码: %s", out)
	}
	// 前缀变体不被误伤键名边界：nonkeytoken= 不应命中 token 键
	out = MaskSensitive("notatoken=keepme")
	if !strings.Contains(out, "keepme") {
		t.Fatalf("键名边界误伤: %s", out)
	}
}

// TestMaskSensitive_BearerPrefix Bearer 前缀保留。
func TestMaskSensitive_BearerPrefix(t *testing.T) {
	out := MaskSensitive("Bearer AbcDef123456")
	if !strings.Contains(out, "Bearer ") {
		t.Fatalf("Bearer 前缀应保留: %s", out)
	}
	if strings.Contains(out, "AbcDef123456") {
		t.Fatalf("令牌未脱敏: %s", out)
	}
}

// TestMaskSensitive_指纹一致性 同值同指纹、二次脱敏幂等。
func TestMaskSensitive_指纹一致性(t *testing.T) {
	first := maskedWithFP("secret-value")
	if first != "******(fp:"+fingerprint("secret-value")+")" {
		t.Fatalf("指纹形态不符: %s", first)
	}
	if again := maskedWithFP(first); again != first {
		t.Fatalf("二次脱敏应幂等: %s -> %s", first, again)
	}
}

// TestMaskSensitive_PII 邮箱/手机/身份证纯掩码。
func TestMaskSensitive_PII(t *testing.T) {
	out := MaskSensitive("mail a@b.com phone 13800138000")
	if strings.Contains(out, "a@b.com") || strings.Contains(out, "13800138000") {
		t.Fatalf("PII 泄露: %s", out)
	}
	if !strings.Contains(out, "******") {
		t.Fatalf("应有掩码: %s", out)
	}
	// 边界：19 位长数字不应命中 18 位身份证/11 位手机（lookbehind/lookahead 等价）
	out = MaskSensitive("id 1138001380001112222")
	if strings.Contains(out, "******") {
		t.Fatalf("边界误伤: %s", out)
	}
}

// TestMaskSensitive_引号KV 键名含敏感词的引号值脱敏 + 未闭合引号只吞当前行。
func TestMaskSensitive_引号KV(t *testing.T) {
	out := MaskSensitive(`{"api_key":"sk-secret-value"}`)
	if strings.Contains(out, "sk-secret-value") {
		t.Fatalf("引号值泄露: %s", out)
	}
	out = MaskSensitive(`{"api_key": "unclosed-secret\nnext "token":"fine"`)
	if strings.Contains(out, "unclosed-secret") {
		t.Fatalf("未闭合引号泄露: %s", out)
	}
}

// TestMaskSensitive_JSON值脱敏 JSON 段整体按值脱敏（token 计数键豁免）。
func TestMaskSensitive_JSON值脱敏(t *testing.T) {
	out := MaskSensitive(`{"input_tokens":12345678901234567890,"user_token":"s3cret"}`)
	if strings.Contains(out, "12345678901234567890") == false {
		t.Fatalf("计量数字不应被破坏: %s", out)
	}
	if strings.Contains(out, "s3cret") {
		t.Fatalf("JSON 值未脱敏: %s", out)
	}
}

// TestMaskSensitive_鉴权结果免脱敏 authorization_outcome 枚举值保留。
func TestMaskSensitive_鉴权结果免脱敏(t *testing.T) {
	out := MaskSensitive(`"authorization_outcome":"allow"`)
	if !strings.Contains(out, "allow") {
		t.Fatalf("枚举值应保留: %s", out)
	}
}

// TestMaskSensitive_空值与空串 nil→空串、空串原样。
func TestMaskSensitive_空值与空串(t *testing.T) {
	if MaskSensitive("") != "" {
		t.Fatalf("空串应原样")
	}
	if MaskSensitive("plain") != "plain" {
		t.Fatalf("无敏感内容应原样: %s", MaskSensitive("plain"))
	}
}
