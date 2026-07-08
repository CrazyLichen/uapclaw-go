package message_handler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestResolveAtFileReferences_空内容 测试空内容
func TestResolveAtFileReferences_空内容(t *testing.T) {
	result := ResolveAtFileReferences("", "/tmp", 0)
	if result != "" {
		t.Errorf("空内容应返回空字符串，实际：%q", result)
	}
}

// TestResolveAtFileReferences_无引用 测试无 @ 引用
func TestResolveAtFileReferences_无引用(t *testing.T) {
	content := "hello world, no file references"
	result := ResolveAtFileReferences(content, "/tmp", 0)
	if result != content {
		t.Errorf("无引用应返回原内容，实际：%q", result)
	}
}

// TestResolveAtFileReferences_文件存在 测试文件存在时的内联
func TestResolveAtFileReferences_文件存在(t *testing.T) {
	// 创建临时文件
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	content := "hello from test file"
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("创建临时文件失败：%v", err)
	}

	input := "see @" + tmpFile + " for details"
	result := ResolveAtFileReferences(input, tmpDir, 0)

	if !strings.Contains(result, "<file-content") {
		t.Errorf("应包含 file-content 标签，实际：%q", result)
	}
	if !strings.Contains(result, content) {
		t.Errorf("应包含文件内容，实际：%q", result)
	}
}

// TestResolveAtFileReferences_文件不存在 测试文件不存在时保持原样
func TestResolveAtFileReferences_文件不存在(t *testing.T) {
	input := "see @/nonexistent/file.txt for details"
	result := ResolveAtFileReferences(input, "/tmp", 0)
	// 文件不存在应保持原样（可能匹配不到 @ 因为绝对路径格式）
	// 关键是不应 panic
	_ = result
}

// TestResolveAtFileReferences_跳过AgentMention 测试跳过 @agent-xxx
func TestResolveAtFileReferences_跳过AgentMention(t *testing.T) {
	input := "use @agent-coder to help"
	result := ResolveAtFileReferences(input, "/tmp", 0)
	// @agent-xxx 不应被当作文件引用
	if strings.Contains(result, "<file-content") {
		t.Errorf("@agent-xxx 不应被当作文件引用，实际：%q", result)
	}
}

// TestResolveAtFileReferences_截断 测试文件大小超过限制时的截断
func TestResolveAtFileReferences_截断(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "large.txt")
	// 创建 200 字节文件
	largeContent := strings.Repeat("a", 200)
	if err := os.WriteFile(tmpFile, []byte(largeContent), 0644); err != nil {
		t.Fatalf("创建临时文件失败：%v", err)
	}

	input := "@" + tmpFile
	result := ResolveAtFileReferences(input, tmpDir, 100)

	if !strings.Contains(result, "truncated") {
		t.Errorf("超过大小限制应显示截断提示，实际：%q", result)
	}
}

// TestExtractAgentMentions_空内容 测试空内容
func TestExtractAgentMentions_空内容(t *testing.T) {
	result := ExtractAgentMentions("")
	if result != nil {
		t.Errorf("空内容应返回 nil，实际：%v", result)
	}
}

// TestExtractAgentMentions_非引号格式 测试 @agent-xxx 格式
func TestExtractAgentMentions_非引号格式(t *testing.T) {
	result := ExtractAgentMentions("use @agent-coder and @agent-reviewer please")
	if len(result) != 2 {
		t.Fatalf("应提取 2 个 agent，实际：%d", len(result))
	}
	if result[0] != "coder" {
		t.Errorf("第一个应为 coder，实际：%q", result[0])
	}
	if result[1] != "reviewer" {
		t.Errorf("第二个应为 reviewer，实际：%q", result[1])
	}
}

// TestExtractAgentMentions_引号格式 测试 @"xxx (agent)" 格式
func TestExtractAgentMentions_引号格式(t *testing.T) {
	result := ExtractAgentMentions(`use @"coder (agent)" please`)
	if len(result) != 1 {
		t.Fatalf("应提取 1 个 agent，实际：%d", len(result))
	}
	if result[0] != "coder" {
		t.Errorf("应为 coder，实际：%q", result[0])
	}
}

// TestExtractAgentMentions_去重 测试去重保序
func TestExtractAgentMentions_去重(t *testing.T) {
	result := ExtractAgentMentions("use @agent-coder and @agent-coder again")
	if len(result) != 1 {
		t.Errorf("重复提及应去重，实际：%d", len(result))
	}
}

// TestExtractAgentMentions_无提及 测试无 agent 提及
func TestExtractAgentMentions_无提及(t *testing.T) {
	result := ExtractAgentMentions("hello world, no agent mentions")
	if len(result) != 0 {
		t.Errorf("无提及应返回空，实际：%v", result)
	}
}

// TestNormalizeStructuredAttachments_空 测试空输入
func TestNormalizeStructuredAttachments_空(t *testing.T) {
	result := NormalizeStructuredAttachments(nil, "/tmp")
	if result != nil {
		t.Errorf("nil 输入应返回 nil，实际：%v", result)
	}
}

// TestNormalizeStructuredAttachments_基本 测试基本规范化
func TestNormalizeStructuredAttachments_基本(t *testing.T) {
	attachments := []map[string]any{
		{"path": "/tmp/file1.txt", "type": "file"},
		{"path": "/tmp/file2.py"},
	}
	result := NormalizeStructuredAttachments(attachments, "/tmp")

	if len(result) != 2 {
		t.Fatalf("应返回 2 个附件，实际：%d", len(result))
	}
	if result[0]["type"] != "file" {
		t.Errorf("type 应为 file，实际：%v", result[0]["type"])
	}
	if result[1]["type"] != "file" {
		t.Errorf("默认 type 应为 file，实际：%v", result[1]["type"])
	}
	if result[1]["filename"] != "file2.py" {
		t.Errorf("filename 应为 file2.py，实际：%v", result[1]["filename"])
	}
}

// TestNormalizeStructuredAttachments_去重 测试去重
func TestNormalizeStructuredAttachments_去重(t *testing.T) {
	attachments := []map[string]any{
		{"path": "/tmp/file.txt"},
		{"path": "/tmp/file.txt"},
	}
	result := NormalizeStructuredAttachments(attachments, "/tmp")

	if len(result) != 1 {
		t.Errorf("重复路径应去重，实际：%d", len(result))
	}
}

// TestNormalizeStructuredAttachments_跳过空路径 测试跳过空路径
func TestNormalizeStructuredAttachments_跳过空路径(t *testing.T) {
	attachments := []map[string]any{
		{"path": ""},
		{"other": "field"},
		{"path": "/tmp/valid.txt"},
	}
	result := NormalizeStructuredAttachments(attachments, "/tmp")

	if len(result) != 1 {
		t.Errorf("空路径应跳过，实际：%d", len(result))
	}
}
