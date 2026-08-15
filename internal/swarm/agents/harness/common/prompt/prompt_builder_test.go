package prompt

import (
	"os"
	"path/filepath"
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestBuildResponseSection(t *testing.T) {
	section := BuildResponseSection("cn")

	if section.Name != "response" {
		t.Errorf("Name = %q, want %q", section.Name, "response")
	}
	if section.Priority != 60 {
		t.Errorf("Priority = %d, want %d", section.Priority, 60)
	}
	if _, ok := section.Content["cn"]; !ok {
		t.Error("缺少 cn 内容")
	}
	if _, ok := section.Content["en"]; !ok {
		t.Error("缺少 en 内容")
	}
}

func TestBuildResponseSection_中文内容(t *testing.T) {
	section := BuildResponseSection("cn")
	cn := section.Content["cn"]

	checks := []string{"消息说明", "用户消息", "系统消息", "cron", "heartbeat", "notify"}
	for _, want := range checks {
		if !contains(cn, want) {
			t.Errorf("中文内容缺少 %q", want)
		}
	}
}

func TestBuildResponseSection_英文内容(t *testing.T) {
	section := BuildResponseSection("cn")
	en := section.Content["en"]

	checks := []string{"Message Format", "User Message", "System Message", "cron", "heartbeat", "notify"}
	for _, want := range checks {
		if !contains(en, want) {
			t.Errorf("英文内容缺少 %q", want)
		}
	}
}

func TestBuildAgentIdentityPrompt(t *testing.T) {
	result := BuildAgentIdentityPrompt("cn")

	if result == "" {
		t.Error("BuildAgentIdentityPrompt 返回空字符串")
	}
	// 验证包含身份节的关键内容
	if !contains(result, "私人智能体") && !contains(result, "personal agent") {
		t.Error("缺少身份节内容")
	}
}

func TestBuildAgentIdentityPrompt_语言解析(t *testing.T) {
	// 空字符串应默认解析为 "cn"
	result := BuildAgentIdentityPrompt("")
	if result == "" {
		t.Error("BuildAgentIdentityPrompt(\"\") 返回空字符串")
	}
	// 默认 cn 应包含中文内容
	if !contains(result, "私人智能体") {
		t.Error("默认语言应为 cn，但缺少中文内容")
	}
}

func TestBuildAgentIdentityPrompt_英文(t *testing.T) {
	result := BuildAgentIdentityPrompt("en")
	if result == "" {
		t.Error("BuildAgentIdentityPrompt(\"en\") 返回空字符串")
	}
	if !contains(result, "personal agent") {
		t.Error("英文模式缺少 personal agent 内容")
	}
}

func TestReadWorkspaceFile(t *testing.T) {
	t.Run("正常读取", func(t *testing.T) {
		tmpDir := t.TempDir()
		fpath := filepath.Join(tmpDir, "test.txt")
		content := "hello world"
		if err := os.WriteFile(fpath, []byte(content), 0644); err != nil {
			t.Fatalf("写入测试文件失败: %v", err)
		}
		got := readWorkspaceFile(fpath)
		if got != content {
			t.Errorf("readWorkspaceFile = %q, want %q", got, content)
		}
	})

	t.Run("文件不存在", func(t *testing.T) {
		got := readWorkspaceFile("/nonexistent/file.txt")
		if got != "" {
			t.Errorf("readWorkspaceFile 不存在的文件应返回空字符串，got %q", got)
		}
	})

	t.Run("空路径", func(t *testing.T) {
		got := readWorkspaceFile("")
		if got != "" {
			t.Errorf("readWorkspaceFile 空路径应返回空字符串，got %q", got)
		}
	})

	t.Run("空文件", func(t *testing.T) {
		tmpDir := t.TempDir()
		fpath := filepath.Join(tmpDir, "empty.txt")
		if err := os.WriteFile(fpath, []byte(""), 0644); err != nil {
			t.Fatalf("写入测试文件失败: %v", err)
		}
		got := readWorkspaceFile(fpath)
		if got != "" {
			t.Errorf("readWorkspaceFile 空文件应返回空字符串，got %q", got)
		}
	})
}

// contains 检查字符串是否包含子串
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || containsSubstr(s, sub))
}

// containsSubstr 简单的子串查找
func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
