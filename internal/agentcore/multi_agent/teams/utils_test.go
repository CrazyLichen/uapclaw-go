package teams

import (
	"context"
	"errors"
	"testing"

	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/team_runtime"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestMakeTeamSession_有ConversationID 测试 message 包含 conversation_id 时使用它作为 sessionID
func TestMakeTeamSession_有ConversationID(t *testing.T) {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("test_team"),
		maschema.WithTeamCardName("测试团队"),
	)
	message := map[string]any{
		"conversation_id": "existing-session-123",
	}

	teamSession := MakeTeamSession(card, message)

	if teamSession == nil {
		t.Fatal("MakeTeamSession 返回 nil")
	}
	if teamSession.GetSessionID() != "existing-session-123" {
		t.Errorf("GetSessionID() = %q, want %q", teamSession.GetSessionID(), "existing-session-123")
	}
	if teamSession.GetTeamID() != "test_team" {
		t.Errorf("GetTeamID() = %q, want %q", teamSession.GetTeamID(), "test_team")
	}
}

// TestMakeTeamSession_无ConversationID 测试 message 无 conversation_id 时生成新 UUID
func TestMakeTeamSession_无ConversationID(t *testing.T) {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("test_team"),
	)
	message := map[string]any{
		"other_key": "other_value",
	}

	teamSession := MakeTeamSession(card, message)

	if teamSession == nil {
		t.Fatal("MakeTeamSession 返回 nil")
	}
	sid := teamSession.GetSessionID()
	if sid == "" {
		t.Error("GetSessionID() 不应为空")
	}
	if len(sid) < 10 {
		t.Errorf("GetSessionID() = %q, 看起来不像有效 UUID", sid)
	}
}

// TestMakeTeamSession_空Message 测试 message 为 nil 时生成新 UUID
func TestMakeTeamSession_空Message(t *testing.T) {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("test_team"),
	)

	teamSession := MakeTeamSession(card, nil)

	if teamSession == nil {
		t.Fatal("MakeTeamSession 返回 nil")
	}
	sid := teamSession.GetSessionID()
	if sid == "" {
		t.Error("GetSessionID() 不应为空")
	}
}

// TestMakeTeamSession_ConversationID非字符串 测试 conversation_id 不是字符串时生成新 UUID
func TestMakeTeamSession_ConversationID非字符串(t *testing.T) {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("test_team"),
	)
	message := map[string]any{
		"conversation_id": 12345,
	}

	teamSession := MakeTeamSession(card, message)

	if teamSession == nil {
		t.Fatal("MakeTeamSession 返回 nil")
	}
	sid := teamSession.GetSessionID()
	if sid == "" {
		t.Error("GetSessionID() 不应为空")
	}
}

// TestStandaloneInvokeContext_有外部Session 测试有外部 session 时直接使用
func TestStandaloneInvokeContext_有外部Session(t *testing.T) {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("test_team"),
	)
	rtCfg := team_runtime.NewRuntimeConfig(
		team_runtime.WithRuntimeTeamID("test_team"),
	)
	rt := team_runtime.NewTeamRuntime(*rtCfg)

	externalSession := session.CreateAgentTeamSession("external-session-001", nil, "test_team")

	var capturedSessionID string
	result, err := StandaloneInvokeContext(
		context.Background(),
		rt,
		card,
		nil,
		externalSession,
		func(ts *session.AgentTeamSession, sid string) (map[string]any, error) {
			capturedSessionID = sid
			return map[string]any{"status": "ok"}, nil
		},
	)

	if err != nil {
		t.Fatalf("StandaloneInvokeContext 返回错误: %v", err)
	}
	if capturedSessionID != "external-session-001" {
		t.Errorf("sessionID = %q, want %q", capturedSessionID, "external-session-001")
	}
	if result["status"] != "ok" {
		t.Errorf("result[status] = %v, want ok", result["status"])
	}

	// 外部会话不应被清理，仍可在运行时查到（或至少不被 UnbindTeamSession 删除）
	// 此处验证 BindTeamSession 后未 UnbindTeamSession
	rt.BindTeamSession(externalSession)
	got := rt.GetTeamSession("external-session-001")
	if got == nil {
		t.Error("外部会话不应被 UnbindTeamSession 删除")
	}
}

// TestStandaloneInvokeContext_无外部Session 测试无外部 session 时创建新 session 并管理生命周期
func TestStandaloneInvokeContext_无外部Session(t *testing.T) {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("test_team"),
	)
	rtCfg := team_runtime.NewRuntimeConfig(
		team_runtime.WithRuntimeTeamID("test_team"),
	)
	rt := team_runtime.NewTeamRuntime(*rtCfg)

	var capturedSessionID string
	result, err := StandaloneInvokeContext(
		context.Background(),
		rt,
		card,
		map[string]any{"conversation_id": "auto-session-002"},
		nil,
		func(ts *session.AgentTeamSession, sid string) (map[string]any, error) {
			capturedSessionID = sid
			return map[string]any{"status": "ok"}, nil
		},
	)

	if err != nil {
		t.Fatalf("StandaloneInvokeContext 返回错误: %v", err)
	}
	if capturedSessionID != "auto-session-002" {
		t.Errorf("sessionID = %q, want %q", capturedSessionID, "auto-session-002")
	}
	if result["status"] != "ok" {
		t.Errorf("result[status] = %v, want ok", result["status"])
	}

	// 无外部会话时应被清理（UnbindTeamSession）
	got := rt.GetTeamSession("auto-session-002")
	if got != nil {
		t.Error("自动创建的会话应被 UnbindTeamSession 清理")
	}
}

// TestStandaloneInvokeContext_业务逻辑失败 测试业务逻辑返回错误时仍正确清理
func TestStandaloneInvokeContext_业务逻辑失败(t *testing.T) {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("test_team"),
	)
	rtCfg := team_runtime.NewRuntimeConfig(
		team_runtime.WithRuntimeTeamID("test_team"),
	)
	rt := team_runtime.NewTeamRuntime(*rtCfg)

	_, err := StandaloneInvokeContext(
		context.Background(),
		rt,
		card,
		map[string]any{"conversation_id": "fail-session-003"},
		nil,
		func(ts *session.AgentTeamSession, sid string) (map[string]any, error) {
			return nil, errors.New("业务错误")
		},
	)

	if err == nil {
		t.Fatal("期望返回错误，得到 nil")
	}
	if err.Error() != "业务错误" {
		t.Errorf("错误 = %q, want %q", err.Error(), "业务错误")
	}

	// 即使业务逻辑失败，也应清理会话
	got := rt.GetTeamSession("fail-session-003")
	if got != nil {
		t.Error("失败后会话应被 UnbindTeamSession 清理")
	}
}

// TestStandaloneStreamContext_基本流程 测试流式上下文基本流程
func TestStandaloneStreamContext_基本流程(t *testing.T) {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("test_team"),
	)
	rtCfg := team_runtime.NewRuntimeConfig(
		team_runtime.WithRuntimeTeamID("test_team"),
	)
	rt := team_runtime.NewTeamRuntime(*rtCfg)

	ch, err := StandaloneStreamContext(
		context.Background(),
		rt,
		card,
		map[string]any{"conversation_id": "stream-session-004"},
		nil,
		func(ts *session.AgentTeamSession, sid string) error {
			return nil
		},
	)

	if err != nil {
		t.Fatalf("StandaloneStreamContext 返回错误: %v", err)
	}
	if ch == nil {
		t.Fatal("返回的通道不应为 nil")
	}

	// 等待通道关闭（runFn 立即返回）
	_, ok := <-ch
	if ok {
		t.Error("通道应已关闭")
	}

	// 会话应被清理
	got := rt.GetTeamSession("stream-session-004")
	if got != nil {
		t.Error("流式会话应被 UnbindTeamSession 清理")
	}
}

// TestStandaloneStreamContext_有外部Session 测试流式上下文使用外部 session
func TestStandaloneStreamContext_有外部Session(t *testing.T) {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("test_team"),
	)
	rtCfg := team_runtime.NewRuntimeConfig(
		team_runtime.WithRuntimeTeamID("test_team"),
	)
	rt := team_runtime.NewTeamRuntime(*rtCfg)

	externalSession := session.CreateAgentTeamSession("ext-stream-005", nil, "test_team")

	ch, err := StandaloneStreamContext(
		context.Background(),
		rt,
		card,
		nil,
		externalSession,
		func(ts *session.AgentTeamSession, sid string) error {
			return nil
		},
	)

	if err != nil {
		t.Fatalf("StandaloneStreamContext 返回错误: %v", err)
	}

	// 等待通道关闭
	_, ok := <-ch
	if ok {
		t.Error("通道应已关闭")
	}

	// 外部会话不应被清理
	rt.BindTeamSession(externalSession)
	got := rt.GetTeamSession("ext-stream-005")
	if got == nil {
		t.Error("外部会话不应被清理")
	}
}

// TestExtractConversationID 测试 extractConversationID 各场景
func TestExtractConversationID(t *testing.T) {
	tests := []struct {
		name    string
		message map[string]any
		want    string
	}{
		{"nil消息", nil, ""},
		{"空消息", map[string]any{}, ""},
		{"有conversation_id", map[string]any{"conversation_id": "sid-123"}, "sid-123"},
		{"conversation_id为空字符串", map[string]any{"conversation_id": ""}, ""},
		{"conversation_id非字符串", map[string]any{"conversation_id": 42}, ""},
		{"conversation_id为nil", map[string]any{"conversation_id": nil}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractConversationID(tt.message)
			if got != tt.want {
				t.Errorf("extractConversationID() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// verifyStreamClosed 验证流通道已关闭
func verifyStreamClosed(t *testing.T, ch <-chan stream.Schema) {
	t.Helper()
	_, ok := <-ch
	if ok {
		t.Error("通道应已关闭")
	}
}
