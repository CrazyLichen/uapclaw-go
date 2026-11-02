package logger

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSanitizer_KV键值对(t *testing.T) {
	s := NewSanitizer()

	tests := []struct {
		input       string
		shouldMatch bool   // 期望是否包含掩码
		exact       string // 精确匹配（如果提供）
	}{
		// password 键值对应被脱敏
		{"password=abc123", true, "password=" + SensitiveMask},
		// api_key 冒号形式应被脱敏
		{"api_key: sk-abc123", true, "api_key: " + SensitiveMask},
		// token 键值对应被脱敏
		{"token=my_secret_token", true, "token=" + SensitiveMask},
		// authorization=xxx（Bearer 部分由第三层 Bearer 正则处理）
		{"authorization = Bearer eyJhbGciOiJIUzI1NiJ9.abc.def", true, ""},
		// pwd 缩写键值对应被脱敏
		{"pwd=mypass123", true, "pwd=" + SensitiveMask},
		// 普通字段不应被脱敏
		{"username=john", false, "username=john"},
		// host 普通字段不应被脱敏
		{"host=localhost", false, "host=localhost"},
	}

	for _, tt := range tests {
		got := s.Sanitize(tt.input)
		if tt.shouldMatch {
			if !strings.Contains(got, SensitiveMask) {
				t.Errorf("Sanitize(%q) 期望包含掩码，实际 %q", tt.input, got)
			}
		} else {
			if strings.Contains(got, SensitiveMask) {
				t.Errorf("Sanitize(%q) 不应包含掩码，实际 %q", tt.input, got)
			}
		}
		if tt.exact != "" && got != tt.exact {
			t.Errorf("Sanitize(%q)\n  期望: %q\n  实际: %q", tt.input, tt.exact, got)
		}
	}
}

func TestSanitizer_命名KV(t *testing.T) {
	s := NewSanitizer()

	tests := []struct {
		input    string
		expected string
	}{
		// 单引号 JSON 形式 TOKEN 应被脱敏
		{`'TOKEN': 'my_secret_value'`, `'TOKEN': '` + SensitiveMask + `'`},
		// 双引号等号形式 api_key 应被脱敏
		{`"api_key"="sk-abc"`, `"api_key"="` + SensitiveMask + `"`},
		// 普通字段不应被脱敏
		{`'name': 'john'`, `'name': 'john'`},
	}

	for _, tt := range tests {
		got := s.Sanitize(tt.input)
		if got != tt.expected {
			t.Errorf("Sanitize(%q)\n  期望: %q\n  实际: %q", tt.input, tt.expected, got)
		}
	}
}

func TestSanitizer_Bearer令牌(t *testing.T) {
	s := NewSanitizer()

	// Bearer 令牌应该被脱敏
	input := "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.abc.def"
	got := s.Sanitize(input)
	if !strings.Contains(got, SensitiveMask) {
		t.Errorf("期望包含掩码，实际 %q", got)
	}
	// 不应包含原始令牌
	if strings.Contains(got, "eyJhbGciOiJIUzI1NiJ9") {
		t.Errorf("期望令牌被脱敏，实际 %q", got)
	}
}

func TestSanitizer_JWT(t *testing.T) {
	s := NewSanitizer()

	input := "jwt=eyJhbGciOiJIUzI1NiJ9.eyJuYW1lIjoiSm9obiJ9.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	got := s.Sanitize(input)
	if strings.Contains(got, "eyJhbGciOiJIUzI1NiJ9") {
		t.Errorf("期望 JWT 被脱敏，实际 %q", got)
	}
}

func TestSanitizer_OpenAIKey(t *testing.T) {
	s := NewSanitizer()

	// 使用 api_key 前缀（在 KV 模式敏感键名列表中）
	input := "api_key=sk-proj-abc123456789"
	got := s.Sanitize(input)
	if !strings.Contains(got, SensitiveMask) {
		t.Errorf("期望 API Key 被脱敏，实际 %q", got)
	}
	if strings.Contains(got, "sk-proj-abc123456789") {
		t.Errorf("期望 API Key 被脱敏，实际 %q", got)
	}
}

func TestSanitizer_GitHubPAT(t *testing.T) {
	s := NewSanitizer()

	input := "token=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	got := s.Sanitize(input)
	if strings.Contains(got, "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		t.Errorf("期望 GitHub PAT 被脱敏，实际 %q", got)
	}
}

func TestSanitizer_GitLabPAT(t *testing.T) {
	s := NewSanitizer()

	input := "token=glpat-ABCDEFGHIJKLMNOPQRST"
	got := s.Sanitize(input)
	if strings.Contains(got, "glpat-ABCDEFGHIJKLMNOPQRST") {
		t.Errorf("期望 GitLab PAT 被脱敏，实际 %q", got)
	}
}

func TestSanitizer_邮箱(t *testing.T) {
	s := NewSanitizer()

	input := "user_email=test@example.com"
	got := s.Sanitize(input)
	if strings.Contains(got, "test@example.com") {
		t.Errorf("期望邮箱被脱敏，实际 %q", got)
	}
}

func TestSanitizer_手机号(t *testing.T) {
	s := NewSanitizer()

	input := "phone=13812345678"
	got := s.Sanitize(input)
	if strings.Contains(got, "13812345678") {
		t.Errorf("期望手机号被脱敏，实际 %q", got)
	}
}

func TestSanitizer_身份证号(t *testing.T) {
	s := NewSanitizer()

	// 身份证号应该被脱敏
	// 注意：Go RE2 不支持 lookbehind，身份证号正则使用 (?:^|[^\d]) 前缀匹配
	input := "身份证号110101199001011234已验证"
	got := s.Sanitize(input)
	if strings.Contains(got, "110101199001011234") {
		t.Errorf("期望身份证号被脱敏，实际 %q", got)
	}
	if !strings.Contains(got, SensitiveMask) {
		t.Errorf("期望包含掩码，实际 %q", got)
	}
}

func TestSanitizer_空字符串(t *testing.T) {
	s := NewSanitizer()

	if got := s.Sanitize(""); got != "" {
		t.Errorf("期望空字符串返回空，实际 %q", got)
	}
}

func TestSanitizer_无敏感数据(t *testing.T) {
	s := NewSanitizer()

	input := "这是一条普通日志消息，没有敏感数据"
	if got := s.Sanitize(input); got != input {
		t.Errorf("期望普通文本不变，实际 %q", got)
	}
}

func TestSanitizer_多层正则组合(t *testing.T) {
	s := NewSanitizer()

	// 一条日志中包含多种敏感数据
	input := `config: password=mypass123, api_key: sk-abc123456789, email=admin@test.com`
	got := s.Sanitize(input)

	if strings.Contains(got, "mypass123") {
		t.Errorf("期望密码被脱敏，实际 %q", got)
	}
	if strings.Contains(got, "sk-abc123456789") {
		t.Errorf("期望 API Key 被脱敏，实际 %q", got)
	}
	if strings.Contains(got, "admin@test.com") {
		t.Errorf("期望邮箱被脱敏，实际 %q", got)
	}
	// 非敏感部分应保留
	if !strings.Contains(got, "config:") {
		t.Errorf("期望保留非敏感部分，实际 %q", got)
	}
}

func TestSanitizerWriter(t *testing.T) {
	s := NewSanitizer()
	var buf bytes.Buffer
	w := NewSanitizerWriter(&buf, s)

	input := []byte("password=secret123")
	n, err := w.Write(input)
	if err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	// io.Writer 契约：必须返回 len(p)，否则上层（zerolog）会判定 short write
	if n != len(input) {
		t.Errorf("期望返回 len(p)=%d，实际 %d", len(input), n)
	}

	got := buf.String()
	if strings.Contains(got, "secret123") {
		t.Errorf("期望密码被脱敏，实际 %q", got)
	}
	if !strings.Contains(got, SensitiveMask) {
		t.Errorf("期望包含掩码，实际 %q", got)
	}
}

func TestSanitizerWriter_普通文本(t *testing.T) {
	s := NewSanitizer()
	var buf bytes.Buffer
	w := NewSanitizerWriter(&buf, s)

	input := []byte("普通日志消息")
	n, err := w.Write(input)
	if err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	if n != len(input) {
		t.Errorf("期望写入 %d 字节，实际 %d", len(input), n)
	}
	if got := buf.String(); got != "普通日志消息" {
		t.Errorf("期望普通文本不变，实际 %q", got)
	}
}

// errWriter 始终返回错误的 writer，用于覆盖写入失败路径
type errWriter struct{}

// Write 始终失败
func (errWriter) Write([]byte) (int, error) { return 0, errors.New("boom") }

// TestSanitizerWriter_底层写失败 验证错误向上传播且返回 0
func TestSanitizerWriter_底层写失败(t *testing.T) {
	w := NewSanitizerWriter(errWriter{}, NewSanitizer())
	n, err := w.Write([]byte("password=secret123"))
	if err == nil {
		t.Fatal("期望返回错误")
	}
	if n != 0 {
		t.Errorf("期望 n=0，实际 %d", n)
	}
}

// TestSanitizerWriter_writeRaw 覆盖脱敏异常兜底路径
func TestSanitizerWriter_writeRaw(t *testing.T) {
	var buf bytes.Buffer
	w := NewSanitizerWriter(&buf, NewSanitizer()).(*sanitizerWriter)
	n, err := w.writeRaw([]byte("raw"))
	if err != nil || n != 3 || buf.String() != "raw" {
		t.Errorf("writeRaw 行为不符: n=%d err=%v buf=%q", n, err, buf.String())
	}

	w2 := NewSanitizerWriter(errWriter{}, NewSanitizer()).(*sanitizerWriter)
	if n, err := w2.writeRaw([]byte("raw")); err == nil || n != 0 {
		t.Errorf("底层失败时应返回 (0,err)，实际 n=%d err=%v", n, err)
	}
}
