package server

import (
	"strings"
	"testing"
)

// TestResolveSessionDirForProvisioner_挡路径遍历 走白名单。
func TestResolveSessionDirForProvisioner_挡路径遍历(t *testing.T) {
	dir, exists := resolveSessionDirForProvisioner("../../etc")
	if exists {
		t.Error("路径遍历不应判定存在")
	}
	if strings.Contains(dir, "..") {
		t.Errorf("不应拼出含 .. 的路径: %q", dir)
	}
}

// TestResolveSessionDirForProvisioner_非法字符 非法 ID 返回空路径。
func TestResolveSessionDirForProvisioner_非法字符(t *testing.T) {
	cases := []string{"bad id", "a/b", "会话", strings.Repeat("x", 81)}
	for _, sessionID := range cases {
		dir, exists := resolveSessionDirForProvisioner(sessionID)
		if exists || dir != "" {
			t.Errorf("非法 ID %q 应返回空路径，实际 dir=%q exists=%v", sessionID, dir, exists)
		}
	}
}

// TestResolveSessionDirForProvisioner_合法ID 合法 ID 返回 sessions 根下路径。
func TestResolveSessionDirForProvisioner_合法ID(t *testing.T) {
	dir, exists := resolveSessionDirForProvisioner("sess-not-exist-123")
	if exists {
		t.Error("不存在的目录应 exists=false")
	}
	if dir == "" {
		t.Fatal("合法 ID 应返回拼接后的路径")
	}
	if !strings.Contains(dir, "sess-not-exist-123") {
		t.Errorf("路径应含会话标识: %q", dir)
	}
}
