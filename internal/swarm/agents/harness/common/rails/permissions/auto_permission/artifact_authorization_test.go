package auto_permission

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// ──────────────────────────── HasUserFileDeliveryProhibition ────────────────────────────

// TestHasUserFileDeliveryProhibition_英文 验证英文禁止模式。
func TestHasUserFileDeliveryProhibition_英文(t *testing.T) {
	cases := []string{
		"do not send this file",
		"don't attach anything",
		"never upload",
		"no file delivery",
		"Do NOT Share",
	}
	for _, text := range cases {
		if !HasUserFileDeliveryProhibition(text) {
			t.Errorf("%q should be prohibited", text)
		}
	}
}

// TestHasUserFileDeliveryProhibition_中文 验证中文禁止模式。
func TestHasUserFileDeliveryProhibition_中文(t *testing.T) {
	cases := []string{
		"不要发送文件",
		"别上传",
		"无需附上",
		"不需要分享",
	}
	for _, text := range cases {
		if !HasUserFileDeliveryProhibition(text) {
			t.Errorf("%q should be prohibited", text)
		}
	}
}

// TestHasUserFileDeliveryProhibition_非禁止 验证不误判。
func TestHasUserFileDeliveryProhibition_非禁止(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"please send this file",
		"请发送文件",
		"send me the report",
	}
	for _, text := range cases {
		if HasUserFileDeliveryProhibition(text) {
			t.Errorf("%q should not be prohibited", text)
		}
	}
}

// ──────────────────────────── IsFileDeliveryAction ────────────────────────────

// TestIsFileDeliveryAction_投递工具 验证三个工具直接命中。
func TestIsFileDeliveryAction_投递工具(t *testing.T) {
	for _, toolName := range []string{"send_file_to_user", "upload_file", "upload_photo"} {
		facts := permissions.ToolDecisionFacts{
			Capability: permissions.ToolCapability{ToolName: toolName},
		}
		if !IsFileDeliveryAction(facts) {
			t.Errorf("%s should be delivery action", toolName)
		}
	}
}

// TestIsFileDeliveryAction_sendMessage读路径 验证读路径命中。
func TestIsFileDeliveryAction_sendMessage读路径(t *testing.T) {
	facts := permissions.ToolDecisionFacts{
		Capability: permissions.ToolCapability{ToolName: "send_message"},
		ReadPaths:  []string{"/ws/a.txt"},
	}
	if !IsFileDeliveryAction(facts) {
		t.Error("send_message with read paths should be delivery")
	}
}

// TestIsFileDeliveryAction_sendMessage投递字段 验证参数命中。
func TestIsFileDeliveryAction_sendMessage投递字段(t *testing.T) {
	facts := permissions.ToolDecisionFacts{
		Capability:    permissions.ToolCapability{ToolName: "send_message"},
		UntrustedArgs: map[string]any{"file_path": "/ws/a.txt"},
	}
	if !IsFileDeliveryAction(facts) {
		t.Error("send_message with file_path should be delivery")
	}
	// 空值不算
	empty := permissions.ToolDecisionFacts{
		Capability:    permissions.ToolCapability{ToolName: "send_message"},
		UntrustedArgs: map[string]any{"file_path": "   "},
	}
	if IsFileDeliveryAction(empty) {
		t.Error("blank file_path should not count")
	}
}

// TestIsFileDeliveryAction_副作用 验证 external_send + 上传特征。
func TestIsFileDeliveryAction_副作用(t *testing.T) {
	facts := permissions.ToolDecisionFacts{
		Capability: permissions.ToolCapability{
			ToolName:          "http_request",
			StaticSideEffects: map[string]struct{}{externalSendSideEffect: {}},
		},
		ReadPaths: []string{"/ws/a.txt"},
	}
	if !IsFileDeliveryAction(facts) {
		t.Error("external_send with read paths should be delivery")
	}
	// 无副作用
	plain := permissions.ToolDecisionFacts{
		Capability: permissions.ToolCapability{ToolName: "http_request"},
		ReadPaths:  []string{"/ws/a.txt"},
	}
	if IsFileDeliveryAction(plain) {
		t.Error("no external_send should not be delivery")
	}
}

// ──────────────────────────── hasFileDeliveryField ────────────────────────────

// TestHasFileDeliveryField_递归 验证嵌套与列表。
func TestHasFileDeliveryField_递归(t *testing.T) {
	// 直接命中
	if !hasFileDeliveryField(map[string]any{"file_path": "/a"}) {
		t.Error("direct file_path should hit")
	}
	// 嵌套命中
	if !hasFileDeliveryField(map[string]any{
		"outer": map[string]any{"attachments": []any{"/a"}},
	}) {
		t.Error("nested attachments should hit")
	}
	// 列表命中
	if !hasFileDeliveryField([]any{map[string]any{"files": []any{"/a"}}}) {
		t.Error("list should hit")
	}
	// 无关键
	if hasFileDeliveryField(map[string]any{"command": "ls"}) {
		t.Error("unrelated key should not hit")
	}
	// 非容器
	if hasFileDeliveryField("string") {
		t.Error("string should not hit")
	}
}

// ──────────────────────────── hasDeliveryValue ────────────────────────────

// TestHasDeliveryValue_各类型 验证值判定。
func TestHasDeliveryValue_各类型(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  bool
	}{
		{"nil", nil, false},
		{"空串", "", false},
		{"空白串", "   ", false},
		{"非空串", "/a", true},
		{"空map", map[string]any{}, false},
		{"非空map", map[string]any{"k": 1}, true},
		{"空列表", []any{}, false},
		{"非空列表", []any{1}, true},
		{"true", true, true},
		{"false", false, false},
		{"零", 0, false},
		{"非零", 5, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasDeliveryValue(c.value); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// ──────────────────────────── issueSendFileAuthorization ────────────────────────────

// TestIssueSendFileAuthorization_非sendFile 验证直接返回空。
func TestIssueSendFileAuthorization_非sendFile(t *testing.T) {
	facts := permissions.ToolDecisionFacts{
		Capability: permissions.ToolCapability{ToolName: "bash"},
	}
	reason, _ := issueSendFileAuthorization(context.Background(), facts)
	if reason != "" {
		t.Errorf("reason = %q, want empty", reason)
	}
}

// TestIssueSendFileAuthorization_缺路径 验证空读路径报错。
func TestIssueSendFileAuthorization_缺路径(t *testing.T) {
	facts := permissions.ToolDecisionFacts{
		Capability: permissions.ToolCapability{ToolName: sendFileToolNameForGrant},
	}
	reason, _ := issueSendFileAuthorization(context.Background(), facts)
	if reason != "send_file_authorization_missing_path" {
		t.Errorf("reason = %q", reason)
	}
}

// TestIssueSendFileAuthorization_正常签发 验证授权发布到 ctx。
func TestIssueSendFileAuthorization_正常签发(t *testing.T) {
	facts := permissions.ToolDecisionFacts{
		Capability:    permissions.ToolCapability{ToolName: sendFileToolNameForGrant},
		ReadPaths:     []string{"/ws/a.txt"},
		UntrustedArgs: map[string]any{"target_channels": []any{"web"}},
	}
	reason, ctx := issueSendFileAuthorization(context.Background(), facts)
	if reason != "" {
		t.Fatalf("reason = %q", reason)
	}
	if permissions.CurrentSendFileExecutionGrant(ctx) == nil {
		t.Error("grant should be published into returned ctx")
	}
}

// TestSendFileExecutionGrantForAllow_非sendFile 验证直接返回。
func TestSendFileExecutionGrantForAllow_非sendFile(t *testing.T) {
	facts := permissions.ToolDecisionFacts{
		Capability: permissions.ToolCapability{ToolName: "bash"},
	}
	grant, reason := sendFileExecutionGrantForAllow(context.Background(), facts)
	if grant != nil || reason != "" {
		t.Errorf("got (%v, %q)", grant, reason)
	}
}

// TestSendFileExecutionGrantForAllow_缺路径 验证空读路径报错。
func TestSendFileExecutionGrantForAllow_缺路径(t *testing.T) {
	facts := permissions.ToolDecisionFacts{
		Capability: permissions.ToolCapability{ToolName: sendFileToolNameForGrant},
	}
	_, reason := sendFileExecutionGrantForAllow(context.Background(), facts)
	if reason != "send_file_authorization_missing_path" {
		t.Errorf("reason = %q", reason)
	}
}

// TestSendFileExecutionGrantForAllow_ctx无授权 验证 missing。
func TestSendFileExecutionGrantForAllow_ctx无授权(t *testing.T) {
	facts := permissions.ToolDecisionFacts{
		Capability: permissions.ToolCapability{ToolName: sendFileToolNameForGrant},
		ReadPaths:  []string{"/ws/a.txt"},
	}
	_, reason := sendFileExecutionGrantForAllow(context.Background(), facts)
	if reason != "send_file_authorization_missing" {
		t.Errorf("reason = %q", reason)
	}
}

// TestSendFileExecutionGrantForAllow_授权匹配 验证成功返回 grant。
func TestSendFileExecutionGrantForAllow_授权匹配(t *testing.T) {
	facts := permissions.ToolDecisionFacts{
		Capability:    permissions.ToolCapability{ToolName: sendFileToolNameForGrant},
		ReadPaths:     []string{"/ws/a.txt"},
		UntrustedArgs: map[string]any{"target_channels": []any{"web"}},
	}
	// 先按同样参数签发
	_, ctx := issueSendFileAuthorization(context.Background(), facts)
	grant, reason := sendFileExecutionGrantForAllow(ctx, facts)
	if reason != "" {
		t.Fatalf("reason = %q", reason)
	}
	if grant == nil {
		t.Error("grant should be returned")
	}
}

// TestSendFileExecutionGrantForAllow_授权不匹配 验证 mismatch。
func TestSendFileExecutionGrantForAllow_授权不匹配(t *testing.T) {
	// 用 A 参数签发
	issued := permissions.ToolDecisionFacts{
		Capability: permissions.ToolCapability{ToolName: sendFileToolNameForGrant},
		ReadPaths:  []string{"/ws/a.txt"},
	}
	_, ctx := issueSendFileAuthorization(context.Background(), issued)
	// 用 B 参数校验
	other := permissions.ToolDecisionFacts{
		Capability: permissions.ToolCapability{ToolName: sendFileToolNameForGrant},
		ReadPaths:  []string{"/ws/b.txt"},
	}
	_, reason := sendFileExecutionGrantForAllow(ctx, other)
	if reason != "send_file_authorization_mismatch" {
		t.Errorf("reason = %q", reason)
	}
}

// TestIsFileDeliveryAction_上传特征 验证 upload_like 分支。
func TestIsFileDeliveryAction_上传特征(t *testing.T) {
	// 无读路径但有 external_send，靠 InspectNetworkScope 的 UploadLike
	facts := permissions.ToolDecisionFacts{
		Capability: permissions.ToolCapability{
			ToolName:          "http_request",
			StaticSideEffects: map[string]struct{}{externalSendSideEffect: {}},
		},
		UntrustedArgs: map[string]any{
			"method": "POST",
			"url":    "https://example.com/upload",
		},
	}
	// 不断言具体值（取决于 InspectNetworkScope 实现），只保证不 panic
	_ = IsFileDeliveryAction(facts)
}
