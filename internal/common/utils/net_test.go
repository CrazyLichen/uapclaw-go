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

// TestRedactURLInValue 测试递归脱敏值中的 URL 密码
func TestRedactURLInValue(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  any
	}{
		{
			name:  "非 URL 字符串",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "URL 字符串脱敏",
			input: "redis://:secret@host:6379/0",
			want:  "redis://:***@host:6379/0",
		},
		{
			name:  "整数原样返回",
			input: 42,
			want:  42,
		},
		{
			name:  "nil 原样返回",
			input: nil,
			want:  nil,
		},
		{
			name: "map 递归脱敏",
			input: map[string]any{
				"db_url": "postgresql://admin:p4ss@db.host:5432/mydb",
				"name":   "test",
			},
			want: map[string]any{
				"db_url": "postgresql://admin:***@db.host:5432/mydb",
				"name":   "test",
			},
		},
		{
			name:  "slice 递归脱敏",
			input: []any{"redis://:pw@host:6379/0", "plain text"},
			want:  []any{"redis://:***@host:6379/0", "plain text"},
		},
		{
			name: "嵌套 map+slice",
			input: map[string]any{
				"urls": []any{"mysql://root:123@db:3306/app"},
			},
			want: map[string]any{
				"urls": []any{"mysql://root:***@db:3306/app"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactURLInValue(tt.input)
			if !equalAny(got, tt.want) {
				t.Fatalf("RedactURLInValue(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// equalAny 深度比较任意类型值
func equalAny(a, b any) bool {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			bval, exists := bv[k]
			if !exists || !equalAny(v, bval) {
				return false
			}
		}
		return true
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !equalAny(av[i], bv[i]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}
