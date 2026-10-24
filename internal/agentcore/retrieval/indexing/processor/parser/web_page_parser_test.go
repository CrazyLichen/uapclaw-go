package parser

import (
	"crypto/tls"
	"testing"
)

// ──────────────────────────── verify 密封 sum type 测试（13.14） ────────────────────────────

// TestVerifyMode_默认单例 两次 NewVerifyDefault 返回同值。
func TestVerifyMode_默认单例(t *testing.T) {
	a, b := NewVerifyDefault(), NewVerifyDefault()
	if a != b {
		t.Error("NewVerifyDefault 应返回单例")
	}
}

// TestVerifyMode_四变体类型 构造器返回对应变体。
func TestVerifyMode_四变体类型(t *testing.T) {
	if _, ok := NewVerifyDefault().(verifyDefault); !ok {
		t.Error("NewVerifyDefault 应返回 verifyDefault")
	}
	if _, ok := NewVerifySkip().(verifySkip); !ok {
		t.Error("NewVerifySkip 应返回 verifySkip")
	}
	if _, ok := NewVerifyCAPath("/tmp/ca.pem").(verifyCAPath); !ok {
		t.Error("NewVerifyCAPath 应返回 verifyCAPath")
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if _, ok := NewVerifyTLSConfig(cfg).(verifyTLSConfig); !ok {
		t.Error("NewVerifyTLSConfig 应返回 verifyTLSConfig")
	}
	vp := NewVerifyCAPath("/x")
	if p := vp.(verifyCAPath); p.path != "/x" {
		t.Errorf("path = %q, want /x", p.path)
	}
	vt := NewVerifyTLSConfig(cfg).(verifyTLSConfig)
	if vt.cfg != cfg {
		t.Error("cfg 应为同一指针")
	}
}

// TestVerifyMode_四变体均实现密封接口 包内四变体可赋值 verifyMode。
func TestVerifyMode_四变体均实现密封接口(t *testing.T) {
	var modes []verifyMode = []verifyMode{
		verifyDefault{}, verifySkip{}, verifyCAPath{}, verifyTLSConfig{},
	}
	for i, m := range modes {
		if m == nil {
			t.Errorf("变体 %d 不应为 nil", i)
		}
	}
}

// TestIsWeChatArticleURL_微信URL判定 正则模式逐分支。
func TestIsWeChatArticleURL_微信URL判定(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"https://mp.weixin.qq.com/s/abc", true},
		{"http://mp.weixin.qq.com/s/x?y=1", true},
		{"https://any.weixin.qq.com/s/abc", true},  // .*?.weixin.qq.com
		{"HTTPS://MP.WEIXIN.QQ.COM/S/ABC", true},   // IGNORECASE
		{"https://mp.weixin.qq.com/other", false},  // /s 后必须 \b（无 /s 前缀）
		{"https://example.com/s/abc", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isWeChatArticleURL(c.in); got != c.want {
			t.Errorf("isWeChatArticleURL(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestHTTPURLPattern_http匹配 http(s) 模式（含 IGNORECASE）。
func TestHTTPURLPattern_http匹配(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"http://a.com", true},
		{"https://a.com/x", true},
		{"HTTP://A.COM", true},
		{"ftp://a.com", false},
		{"a.com", false},
	}
	for _, c := range cases {
		if got := httpURLPattern.MatchString(c.in); got != c.want {
			t.Errorf("httpURLPattern(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
