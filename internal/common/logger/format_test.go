package logger

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestFormatTimestamp 覆盖时间戳渲染的输入形态与非法值兜底
func TestFormatTimestamp(t *testing.T) {
	want := "[" + time.UnixMilli(1762000000123).Local().Format(timestampLayout) + "]"
	cases := []struct {
		name  string
		input any
		want  string
	}{
		{name: "json.Number", input: json.Number("1762000000123"), want: want},
		{name: "float64", input: float64(1762000000123), want: want},
		{name: "int64", input: int64(1762000000123), want: want},
		{name: "零值毫秒", input: json.Number("0"), want: "[" + time.UnixMilli(0).Local().Format(timestampLayout) + "]"},
		{name: "负值毫秒", input: int64(-1000), want: "[" + time.UnixMilli(-1000).Local().Format(timestampLayout) + "]"},
		{
			name:  "RFC3339字符串",
			input: "2026-11-02T10:31:20.123Z",
			want:  "[" + time.Date(2026, 11, 2, 10, 31, 20, 123000000, time.UTC).Local().Format(timestampLayout) + "]",
		},
		{name: "非法字符串", input: "not-a-time", want: ""},
		{name: "不支持的类型", input: true, want: ""},
		{name: "nil", input: nil, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatTimestamp(tc.input); got != tc.want {
				t.Errorf("formatTimestamp(%v) = %q, 期望 %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestFormatLevel 覆盖级别大写化、未知级别与着色开关
func TestFormatLevel(t *testing.T) {
	cases := []struct {
		name      string
		input     any
		withColor bool
		want      string
	}{
		{name: "info不带色", input: "info", withColor: false, want: "[INFO]"},
		{name: "warn不带色", input: "warn", withColor: false, want: "[WARN]"},
		{name: "debug不带色", input: "debug", withColor: false, want: "[DEBUG]"},
		{name: "error不带色", input: "error", withColor: false, want: "[ERROR]"},
		{name: "fatal不带色", input: "fatal", withColor: false, want: "[FATAL]"},
		{name: "未知级别", input: "custom", withColor: false, want: "[CUSTOM]"},
		{name: "info带色", input: "info", withColor: true, want: "\x1b[32m[INFO]\x1b[0m"},
		{name: "未知级别带色不着色", input: "custom", withColor: true, want: "[CUSTOM]"},
		{name: "空级别", input: "", withColor: false, want: "[UNKNOWN]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatLevel(tc.input, tc.withColor); got != tc.want {
				t.Errorf("formatLevel(%v,%v) = %q, 期望 %q", tc.input, tc.withColor, got, tc.want)
			}
		})
	}
}

// TestFormatCaller 覆盖绝对路径裁剪与降级
func TestFormatCaller(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  string
	}{
		{name: "绝对路径", input: "/home/u/uap-claw-go/internal/common/config/loader.go:88", want: "[config/loader.go:88]"},
		{name: "末两段", input: "internal/common/logger/logger.go:12", want: "[logger/logger.go:12]"},
		{name: "单段路径", input: "main.go:7", want: "[main.go:7]"},
		{name: "无行号", input: "/a/b/pkg/file.go", want: "[pkg/file.go]"},
		{name: "Windows分隔符", input: `C:\repo\internal\gateway\app.go:55`, want: "[gateway/app.go:55]"},
		{name: "空串", input: "", want: ""},
		{name: "非字符串", input: 42, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatCaller(tc.input); got != tc.want {
				t.Errorf("formatCaller(%v) = %q, 期望 %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestFormatMessage 覆盖控制字符转义，保证一行一条
func TestFormatMessage(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  string
	}{
		{name: "普通消息", input: "hello world", want: "hello world"},
		{name: "换行转义", input: "a\nb", want: `a\nb`},
		{name: "回车转义", input: "a\rb", want: `a\rb`},
		{name: "制表转义", input: "a\tb", want: `a\tb`},
		{name: "多控制符混合", input: "a\nb\tc\r", want: `a\nb\tc\r`},
		{name: "非字符串", input: 7, want: "7"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatMessage(tc.input); got != tc.want {
				t.Errorf("formatMessage(%v) = %q, 期望 %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestFormatPartValue 覆盖 component 栏渲染
func TestFormatPartValue(t *testing.T) {
	if got := formatPartValue("common", "component"); got != "[common]" {
		t.Errorf("字符串值渲染错误：%q", got)
	}
	if got := formatPartValue(nil, "component"); got != "" {
		t.Errorf("nil 应渲染为空串，实际 %q", got)
	}
	if got := formatPartValue(3, "component"); got != "[3]" {
		t.Errorf("非字符串值渲染错误：%q", got)
	}
}

// TestNewTextWriter_定制项 验证 PartsOrder 与字段排除装配正确
func TestNewTextWriter_定制项(t *testing.T) {
	w := newTextWriter(&strings.Builder{}, true)
	wantParts := []string{"time", "level", "component", "caller", "message"}
	if fmt.Sprint(w.PartsOrder) != fmt.Sprint(wantParts) {
		t.Errorf("PartsOrder = %v, 期望 %v", w.PartsOrder, wantParts)
	}
	if len(w.FieldsExclude) != 1 || w.FieldsExclude[0] != "component" {
		t.Errorf("FieldsExclude = %v, 期望 [component]", w.FieldsExclude)
	}
	if !w.NoColor {
		t.Error("noColor=true 时应禁用着色")
	}
}
