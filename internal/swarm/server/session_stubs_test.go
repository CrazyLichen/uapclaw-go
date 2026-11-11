package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
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

// ──────────────────────────── requestUserID（9.90e 改源）────────────────────────────

// TestRequestUserID_从metadata读 验证首选来源。
//
// 9.90e 前读 req.PermissionContext.PrincipalUserID；该字段删除后改为
// 与 runtime.requestUserID 同源：metadata 优先、params 兜底。
func TestRequestUserID_从metadata读(t *testing.T) {
	req := &schema.AgentRequest{
		RequestID: "r1",
		Metadata:  map[string]any{"user_id": "u-meta"},
	}
	if got := requestUserID(req); got != "u-meta" {
		t.Errorf("got %q, want u-meta", got)
	}
}

// TestRequestUserID_从params兜底 验证 metadata 缺失时读 params。
func TestRequestUserID_从params兜底(t *testing.T) {
	req := &schema.AgentRequest{
		RequestID: "r1",
		Params:    json.RawMessage(`{"user_id":"u-param"}`),
	}
	if got := requestUserID(req); got != "u-param" {
		t.Errorf("got %q, want u-param", got)
	}
}

// TestRequestUserID_metadata优先于params 验证读取顺序。
func TestRequestUserID_metadata优先于params(t *testing.T) {
	req := &schema.AgentRequest{
		RequestID: "r1",
		Metadata:  map[string]any{"user_id": "u-meta"},
		Params:    json.RawMessage(`{"user_id":"u-param"}`),
	}
	if got := requestUserID(req); got != "u-meta" {
		t.Errorf("got %q, want u-meta（metadata 优先）", got)
	}
}

// TestRequestUserID_空白值不采用 验证 trim 后为空则继续兜底。
func TestRequestUserID_空白值不采用(t *testing.T) {
	req := &schema.AgentRequest{
		Metadata: map[string]any{"user_id": "   "},
		Params:   json.RawMessage(`{"user_id":"u-param"}`),
	}
	if got := requestUserID(req); got != "u-param" {
		t.Errorf("got %q, want u-param（空白 metadata 值应跳过）", got)
	}
}

// TestRequestUserID_无标识返回空串 验证缺省。
func TestRequestUserID_无标识返回空串(t *testing.T) {
	if got := requestUserID(nil); got != "" {
		t.Errorf("nil got %q", got)
	}
	if got := requestUserID(&schema.AgentRequest{}); got != "" {
		t.Errorf("空请求 got %q", got)
	}
}
