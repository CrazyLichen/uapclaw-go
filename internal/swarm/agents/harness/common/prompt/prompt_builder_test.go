package prompt

import (
	"github.com/stretchr/testify/assert"
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

// TestInputPrompt_双语 对齐 Python _input_prompt（prompt_builder.py:92-156）。
func TestInputPrompt_双语(t *testing.T) {
	cn := InputPrompt("cn")
	assert.Equal(t, "input", cn.Name)
	assert.Equal(t, 60, cn.Priority)
	assert.Contains(t, cn.Content["cn"], "# 输入说明")
	assert.Contains(t, cn.Content["cn"], "## 用户消息")
	assert.Contains(t, cn.Content["cn"], "系统消息类型说明")
	assert.Contains(t, cn.Content["cn"], "- cron：定时任务")
	assert.Contains(t, cn.Content["cn"], "- notify：系统通知")
	// cn 模式 en 键也填中文（Python content={language: content}）
	assert.Equal(t, cn.Content["cn"], cn.Content["cn"])

	en := InputPrompt("en")
	assert.Equal(t, 60, en.Priority)
	assert.Contains(t, en.Content["en"], "# Input Instructions")
	assert.Contains(t, en.Content["en"], "System message types")
	assert.NotContains(t, en.Content["en"], "输入说明")
}

// TestOutputPrompt_文件投递分叉 对齐 Python _output_prompt send_file_enabled 分支。
func TestOutputPrompt_文件投递分叉(t *testing.T) {
	// cn + 开启：含 send_file_to_user 规则与 SVG 围栏
	cnOn := OutputPrompt("cn", true)
	assert.Equal(t, "output", cnOn.Name)
	assert.Equal(t, 65, cnOn.Priority)
	assert.Contains(t, cnOn.Content["cn"], "# 输出规则")
	assert.Contains(t, cnOn.Content["cn"], "send_file_to_user")
	assert.Contains(t, cnOn.Content["cn"], "SVG 卡片源码须在**最后一条无工具调用的消息**里")
	assert.Contains(t, cnOn.Content["cn"], "同一产物不要既内联又发文件")

	// cn + 关闭：禁止承诺发文件
	cnOff := OutputPrompt("cn", false)
	assert.Contains(t, cnOff.Content["cn"], "当前渠道不支持文件投递")
	assert.NotContains(t, cnOff.Content["cn"], "send_file_to_user`")
	assert.Contains(t, cnOff.Content["cn"], "不要为该产物生成附件或承诺文件投递")
	assert.Contains(t, cnOff.Content["cn"], "## 模型名称回答")

	// en + 开启 / 关闭
	enOn := OutputPrompt("en", true)
	assert.Contains(t, enOn.Content["en"], "# Output Rules")
	assert.Contains(t, enOn.Content["en"], "send_file_to_user")
	enOff := OutputPrompt("en", false)
	assert.Contains(t, enOff.Content["en"], "This channel cannot deliver files")
	assert.Contains(t, enOff.Content["en"], "## Model Name Answers")
}
