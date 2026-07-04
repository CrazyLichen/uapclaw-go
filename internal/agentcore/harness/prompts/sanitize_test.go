package prompts

import (
	"strings"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSanitizePath_注入字符 测试路径中的注入字符被移除
func TestSanitizePath_注入字符(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"尖括号", "/path/<script>", "/path/script"},
		{"花括号", "/path/{var}", "/path/var"},
		{"方括号", "/path/[0]", "/path/0"},
		{"反引号", "/path/`cmd`", "/path/cmd"},
		{"美元符", "/path/$HOME", "/path/HOME"},
		{"省略号", "/path/.../file", "/path//file"},
		{"换行符", "/path/\\n/file", "/path//file"},
		{"回车符", "/path/\\r/file", "/path//file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizePath(tt.input)
			if result != tt.want {
				t.Errorf("SanitizePath(%q) = %q, 期望 %q", tt.input, result, tt.want)
			}
		})
	}
}

// TestSanitizePath_正常路径 测试正常路径不受影响
func TestSanitizePath_正常路径(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"简单路径", "/home/user/file.txt"},
		{"相对路径", "./dir/file"},
		{"带连字符", "/path/to/my-file"},
		{"带下划线", "/path/to/my_file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizePath(tt.input)
			if result != tt.input {
				t.Errorf("正常路径不应被修改: SanitizePath(%q) = %q", tt.input, result)
			}
		})
	}
}

// TestSanitizeUserContent_注入字符 测试用户内容中的注入字符被移除
func TestSanitizeUserContent_注入字符(t *testing.T) {
	input := `Hello <b>world</b> {template} [array] \` + "cmd" + ` $var...`
	result := SanitizeUserContent(input, 2000)
	// 不应包含注入字符
	if strings.Contains(result, "<") || strings.Contains(result, ">") {
		t.Error("结果不应包含尖括号")
	}
	if strings.Contains(result, "{") || strings.Contains(result, "}") {
		t.Error("结果不应包含花括号")
	}
	if strings.Contains(result, "[") || strings.Contains(result, "]") {
		t.Error("结果不应包含方括号")
	}
}

// TestSanitizeUserContent_截断 测试用户内容超出 maxLen 时截断
func TestSanitizeUserContent_截断(t *testing.T) {
	input := "这是一段很长的内容，用于测试截断功能是否正常工作"
	maxLen := 10
	result := SanitizeUserContent(input, maxLen)
	if len(result) > maxLen {
		t.Errorf("截断后长度不应超过 %d，实际 %d", maxLen, len(result))
	}
}

// TestSanitizeUserContent_正常内容 测试正常内容不受影响
func TestSanitizeUserContent_正常内容(t *testing.T) {
	input := "这是一段正常的内容"
	result := SanitizeUserContent(input, 2000)
	if result != input {
		t.Errorf("正常内容不应被修改: SanitizeUserContent(%q) = %q", input, result)
	}
}
