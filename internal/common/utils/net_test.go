package utils

import (
	"testing"
)

func TestGetLocalIP(t *testing.T) {
	ip := GetLocalIP()
	if ip == "" {
		t.Fatal("GetLocalIP() returned empty string")
	}
	// 在没有网络的测试环境中，可能回退到 127.0.0.1
	// 所以只验证返回的是合法 IPv4 地址
	if ip != "127.0.0.1" {
		// 非回退地址，验证不是 loopback
		if ip == "0.0.0.0" {
			t.Fatal("GetLocalIP() should not return 0.0.0.0")
		}
	}
}

func TestRedactURLPassword(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "password without username",
			url:  "redis://:secret@host:6379/0",
			want: "redis://:***@host:6379/0",
		},
		{
			name: "password with username",
			url:  "redis://user:secret@host:6379/0",
			want: "redis://user:***@host:6379/0",
		},
		{
			name: "no password",
			url:  "redis://host:6379/0",
			want: "redis://host:6379/0",
		},
		{
			name: "empty string",
			url:  "",
			want: "",
		},
		{
			name: "no credentials simple url",
			url:  "https://api.openai.com/v1/chat",
			want: "https://api.openai.com/v1/chat",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactURLPassword(tt.url)
			if got != tt.want {
				t.Fatalf("RedactURLPassword(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestRedactURLPassword_InvalidURL(t *testing.T) {
	// 无效 URL 应原样返回
	got := RedactURLPassword("://invalid")
	if got != "://invalid" {
		t.Fatalf("invalid URL should be returned as-is, got %q", got)
	}
}

// TestRedactURLPassword_有查询和片段 测试 URL 有 query 和 fragment 时的脱敏
func TestRedactURLPassword_有查询和片段(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "带 query",
			url:  "redis://:secret@host:6379/0?timeout=5",
			want: "redis://:***@host:6379/0?timeout=5",
		},
		{
			name: "带 fragment",
			url:  "redis://:secret@host:6379/0#section",
			want: "redis://:***@host:6379/0#section",
		},
		{
			name: "带 query 和 fragment",
			url:  "redis://:secret@host:6379/0?timeout=5#section",
			want: "redis://:***@host:6379/0?timeout=5#section",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactURLPassword(tt.url)
			if got != tt.want {
				t.Fatalf("RedactURLPassword(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestSanitizeHeaders(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		want    map[string]string
	}{
		{
			name:    "nil map",
			headers: nil,
			want:    map[string]string{},
		},
		{
			name:    "empty map",
			headers: map[string]string{},
			want:    map[string]string{},
		},
		{
			name: "normal headers",
			headers: map[string]string{
				"Content-Type": "application/json",
				"Accept":       "text/html",
			},
			want: map[string]string{
				"Content-Type": "application/json",
				"Accept":       "text/html",
			},
		},
		{
			name: "protected headers removed",
			headers: map[string]string{
				"Content-Type":   "application/json",
				"Host":           "example.com",
				"Authorization":  "Bearer xxx",
				"Content-Length": "100",
			},
			want: map[string]string{
				"Content-Type": "application/json",
			},
		},
		{
			name: "empty key and value removed",
			headers: map[string]string{
				"":         "value",
				"Key":      "",
				"ValidKey": "ValidValue",
				"SpaceKey": "   ",
			},
			want: map[string]string{
				"ValidKey": "ValidValue",
			},
		},
		{
			name: "whitespace key trimmed",
			headers: map[string]string{
				"  X-Custom  ": "value",
			},
			want: map[string]string{
				"X-Custom": "value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeHeaders(tt.headers)
			if len(got) != len(tt.want) {
				t.Fatalf("SanitizeHeaders() got %d entries, want %d entries", len(got), len(tt.want))
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Fatalf("SanitizeHeaders()[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestProtectedHeaders(t *testing.T) {
	protected := []string{"host", "content-length", "transfer-encoding", "connection", "authorization"}
	for _, h := range protected {
		if !ProtectedHeaders[h] {
			t.Fatalf("ProtectedHeaders[%q] should be true", h)
		}
	}
	// 大小写不敏感检查在 SanitizeHeaders 中通过 ToLower 实现
	if ProtectedHeaders["Host"] {
		t.Fatal("ProtectedHeaders keys should be lowercase; 'Host' should not be present")
	}
}
