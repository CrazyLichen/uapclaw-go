package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidateSessionID_合法取值 覆盖正常 ID 形态。
func TestValidateSessionID_合法取值(t *testing.T) {
	valid := []string{
		"abc",
		"ABC123",
		"__cron__",
		"session-2026-11-09",
		"a.b.c",
		"_x_",
		strings.Repeat("a", 80),
	}
	for _, id := range valid {
		if err := ValidateSessionID(id); err != nil {
			t.Errorf("ValidateSessionID(%q) 应通过，实际错误: %v", id, err)
		}
	}
}

// TestValidateSessionID_非法取值 覆盖白名单的四条判据。
func TestValidateSessionID_非法取值(t *testing.T) {
	cases := []struct {
		name string
		id   string
		want string
	}{
		{"空串", "", "invalid session_id"},
		{"超长81", strings.Repeat("a", 81), "session_id exceeds maximum length 80"},
		{"首尾空白", " abc", "invalid session_id"},
		{"路径遍历", "../../etc/passwd", "invalid session_id"},
		{"斜杠", "a/b", "invalid session_id"},
		{"点开头", ".hidden", "invalid session_id"},
		{"横线开头", "-x", "invalid session_id"},
		{"点结尾", "x.", "invalid session_id"},
		{"横线结尾", "x-", "invalid session_id"},
		{"中文字符", "会话", "invalid session_id"},
		{"空字符", "a\x00b", "invalid session_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSessionID(tc.id)
			if err == nil {
				t.Fatalf("ValidateSessionID(%q) 应报错", tc.id)
			}
			if err.Error() != tc.want {
				t.Errorf("错误消息不符: got %q want %q", err.Error(), tc.want)
			}
		})
	}
}

// TestValidateSessionID_错误消息不含原值 确认 value-safe 约定。
func TestValidateSessionID_错误消息不含原值(t *testing.T) {
	const secret = "../../very-secret-path"
	err := ValidateSessionID(secret)
	if err == nil {
		t.Fatal("应报错")
	}
	if strings.Contains(err.Error(), "very-secret") {
		t.Errorf("错误消息泄漏了原值: %q", err.Error())
	}
}

// TestIsValidSessionID 包一层布尔判定。
func TestIsValidSessionID(t *testing.T) {
	if !IsValidSessionID("ok_id") {
		t.Error("IsValidSessionID(ok_id) 应为 true")
	}
	if IsValidSessionID("bad id") {
		t.Error("IsValidSessionID(bad id) 应为 false")
	}
}

// TestValidateWorkflowRunID 长度与控制字符两条判据。
func TestValidateWorkflowRunID(t *testing.T) {
	if err := ValidateWorkflowRunID("run-123"); err != nil {
		t.Errorf("合法 run id 应通过: %v", err)
	}
	if err := ValidateWorkflowRunID(strings.Repeat("r", 257)); err == nil {
		t.Error("超长 run id 应报错")
	}
	if err := ValidateWorkflowRunID("run\nid"); err == nil {
		t.Error("含控制字符的 run id 应报错")
	}
	if err := ValidateWorkflowRunID("run\x7fid"); err == nil {
		t.Error("含 DEL 的 run id 应报错")
	}
}

// TestResolveSessionDir_挡路径遍历 确认拼出的路径必须仍在 sessions 根内。
func TestResolveSessionDir_挡路径遍历(t *testing.T) {
	root := t.TempDir()

	_, _, err := ResolveSessionDir("../../etc", root)
	if err == nil {
		t.Fatal("路径遍历应被白名单挡住")
	}

	got, exists, err := ResolveSessionDir("sess-1", root)
	if err != nil {
		t.Fatalf("合法 ID 不应报错: %v", err)
	}
	if exists {
		t.Error("目录不存在时 exists 应为 false")
	}
	if !strings.HasPrefix(got, root) {
		t.Errorf("解析结果必须在 sessions 根内: %q", got)
	}
}

// TestResolveSessionDir_归档回退 活动目录不存在时回退到 sessions_archived。
func TestResolveSessionDir_归档回退(t *testing.T) {
	dataRoot := t.TempDir()
	sessionsRoot := filepath.Join(dataRoot, "sessions")
	archivedRoot := filepath.Join(dataRoot, "sessions_archived")
	if err := os.MkdirAll(sessionsRoot, 0o755); err != nil {
		t.Fatalf("建 sessions 失败: %v", err)
	}
	archivedDir := filepath.Join(archivedRoot, "sess-arch")
	if err := os.MkdirAll(archivedDir, 0o755); err != nil {
		t.Fatalf("建归档目录失败: %v", err)
	}

	got, exists, err := ResolveSessionDir("sess-arch", sessionsRoot)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !exists {
		t.Fatal("归档目录存在时 exists 应为 true")
	}
	if got != archivedDir {
		t.Errorf("应回退到归档目录: got %q want %q", got, archivedDir)
	}
}
