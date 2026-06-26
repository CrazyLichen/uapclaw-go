package schema

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── 工厂函数测试 ────────────────────────────

// TestNewPermissionContext 验证工厂函数默认值
func TestNewPermissionContext(t *testing.T) {
	pc := NewPermissionContext()
	if pc.PrincipalUserID != "" {
		t.Errorf("PrincipalUserID 应为空，实际 %q", pc.PrincipalUserID)
	}
	if pc.TriggeringUserID != "" {
		t.Errorf("TriggeringUserID 应为空，实际 %q", pc.TriggeringUserID)
	}
	if pc.ChannelID != "" {
		t.Errorf("ChannelID 应为空，实际 %q", pc.ChannelID)
	}
	if pc.GroupDigitalAvatar {
		t.Error("GroupDigitalAvatar 应为 false")
	}
	if pc.WebUserID != "" {
		t.Errorf("WebUserID 应为空，实际 %q", pc.WebUserID)
	}
}

// TestNewPermissionContext_使用Option 验证通过 Option 设置各字段
func TestNewPermissionContext_使用Option(t *testing.T) {
	pc := NewPermissionContext(
		WithPermissionPrincipalUserID("user-1"),
		WithPermissionTriggeringUserID("sender-1"),
		WithPermissionChannelID("web"),
		WithPermissionGroupDigitalAvatar(true),
		WithPermissionWebUserID("web-user-1"),
	)
	if pc.PrincipalUserID != "user-1" {
		t.Errorf("PrincipalUserID = %q, 期望 \"user-1\"", pc.PrincipalUserID)
	}
	if pc.TriggeringUserID != "sender-1" {
		t.Errorf("TriggeringUserID = %q, 期望 \"sender-1\"", pc.TriggeringUserID)
	}
	if pc.ChannelID != "web" {
		t.Errorf("ChannelID = %q, 期望 \"web\"", pc.ChannelID)
	}
	if !pc.GroupDigitalAvatar {
		t.Error("GroupDigitalAvatar 应为 true")
	}
	if pc.WebUserID != "web-user-1" {
		t.Errorf("WebUserID = %q, 期望 \"web-user-1\"", pc.WebUserID)
	}
}

// ──────────────────────────── Scene 方法测试 ────────────────────────────

// TestPermissionContext_Scene_web 验证 channel_id="web" 时返回 "web"
func TestPermissionContext_Scene_web(t *testing.T) {
	pc := NewPermissionContext(WithPermissionChannelID("web"))
	if got := pc.Scene(); got != "web" {
		t.Errorf("Scene() = %q, 期望 \"web\"", got)
	}
}

// TestPermissionContext_Scene_groupDigitalAvatar 验证数字分身场景
func TestPermissionContext_Scene_groupDigitalAvatar(t *testing.T) {
	pc := NewPermissionContext(
		WithPermissionChannelID("feishu"),
		WithPermissionGroupDigitalAvatar(true),
	)
	if got := pc.Scene(); got != "group_digital_avatar" {
		t.Errorf("Scene() = %q, 期望 \"group_digital_avatar\"", got)
	}
}

// TestPermissionContext_Scene_normalIM 验证默认为普通 IM 场景
func TestPermissionContext_Scene_normalIM(t *testing.T) {
	pc := NewPermissionContext(WithPermissionChannelID("feishu"))
	if got := pc.Scene(); got != "normal_im" {
		t.Errorf("Scene() = %q, 期望 \"normal_im\"", got)
	}
}

// TestPermissionContext_Scene_web优先级高于数字分身 验证 web 渠道优先级
func TestPermissionContext_Scene_web优先级高于数字分身(t *testing.T) {
	pc := NewPermissionContext(
		WithPermissionChannelID("web"),
		WithPermissionGroupDigitalAvatar(true),
	)
	if got := pc.Scene(); got != "web" {
		t.Errorf("当 channel_id=web 且 group_digital_avatar=true 时，Scene() = %q, 期望 \"web\"", got)
	}
}

// ──────────────────────────── OwnerScopeKey 测试 ────────────────────────────

// TestPermissionContext_OwnerScopeKey 验证返回 [channel_id, principal_user_id]
func TestPermissionContext_OwnerScopeKey(t *testing.T) {
	pc := NewPermissionContext(
		WithPermissionPrincipalUserID("user-1"),
		WithPermissionChannelID("feishu"),
	)
	key := pc.OwnerScopeKey()
	if key[0] != "feishu" {
		t.Errorf("OwnerScopeKey()[0] = %q, 期望 \"feishu\"", key[0])
	}
	if key[1] != "user-1" {
		t.Errorf("OwnerScopeKey()[1] = %q, 期望 \"user-1\"", key[1])
	}
}

// ──────────────────────────── ToDict / FromDict 测试 ────────────────────────────

// TestPermissionContext_ToDict 验证序列化完整字段
func TestPermissionContext_ToDict(t *testing.T) {
	pc := NewPermissionContext(
		WithPermissionPrincipalUserID("user-1"),
		WithPermissionTriggeringUserID("sender-1"),
		WithPermissionChannelID("web"),
		WithPermissionGroupDigitalAvatar(true),
		WithPermissionWebUserID("web-user-1"),
	)
	d := pc.ToDict()
	if d["principal_user_id"] != "user-1" {
		t.Errorf("ToDict()[\"principal_user_id\"] = %v, 期望 \"user-1\"", d["principal_user_id"])
	}
	if d["triggering_user_id"] != "sender-1" {
		t.Errorf("ToDict()[\"triggering_user_id\"] = %v, 期望 \"sender-1\"", d["triggering_user_id"])
	}
	if d["channel_id"] != "web" {
		t.Errorf("ToDict()[\"channel_id\"] = %v, 期望 \"web\"", d["channel_id"])
	}
	if d["group_digital_avatar"] != true {
		t.Errorf("ToDict()[\"group_digital_avatar\"] = %v, 期望 true", d["group_digital_avatar"])
	}
	if d["web_user_id"] != "web-user-1" {
		t.Errorf("ToDict()[\"web_user_id\"] = %v, 期望 \"web-user-1\"", d["web_user_id"])
	}
}

// TestNewPermissionContextFromDict 验证反序列化往返
func TestNewPermissionContextFromDict(t *testing.T) {
	data := map[string]any{
		"principal_user_id":    "user-1",
		"triggering_user_id":   "sender-1",
		"channel_id":           "web",
		"group_digital_avatar": true,
		"web_user_id":          "web-user-1",
	}
	pc := NewPermissionContextFromDict(data)
	if pc.PrincipalUserID != "user-1" {
		t.Errorf("PrincipalUserID = %q, 期望 \"user-1\"", pc.PrincipalUserID)
	}
	if pc.TriggeringUserID != "sender-1" {
		t.Errorf("TriggeringUserID = %q, 期望 \"sender-1\"", pc.TriggeringUserID)
	}
	if pc.ChannelID != "web" {
		t.Errorf("ChannelID = %q, 期望 \"web\"", pc.ChannelID)
	}
	if !pc.GroupDigitalAvatar {
		t.Error("GroupDigitalAvatar 应为 true")
	}
	if pc.WebUserID != "web-user-1" {
		t.Errorf("WebUserID = %q, 期望 \"web-user-1\"", pc.WebUserID)
	}
}

// TestNewPermissionContextFromDict_缺失字段用零值 验证缺失字段用零值填充
func TestNewPermissionContextFromDict_缺失字段用零值(t *testing.T) {
	data := map[string]any{
		"principal_user_id": "user-1",
	}
	pc := NewPermissionContextFromDict(data)
	if pc.PrincipalUserID != "user-1" {
		t.Errorf("PrincipalUserID = %q, 期望 \"user-1\"", pc.PrincipalUserID)
	}
	if pc.TriggeringUserID != "" {
		t.Errorf("TriggeringUserID 应为空，实际 %q", pc.TriggeringUserID)
	}
	if pc.GroupDigitalAvatar {
		t.Error("GroupDigitalAvatar 应为 false（零值）")
	}
}

// TestPermissionContext_ToDictFromDict往返 验证 ToDict → FromDict 往返一致
func TestPermissionContext_ToDictFromDict往返(t *testing.T) {
	original := NewPermissionContext(
		WithPermissionPrincipalUserID("user-1"),
		WithPermissionTriggeringUserID("sender-1"),
		WithPermissionChannelID("feishu"),
		WithPermissionGroupDigitalAvatar(false),
		WithPermissionWebUserID(""),
	)
	roundtrip := NewPermissionContextFromDict(original.ToDict())
	if roundtrip.PrincipalUserID != original.PrincipalUserID {
		t.Errorf("PrincipalUserID 往返不一致: %q vs %q", roundtrip.PrincipalUserID, original.PrincipalUserID)
	}
	if roundtrip.TriggeringUserID != original.TriggeringUserID {
		t.Errorf("TriggeringUserID 往返不一致: %q vs %q", roundtrip.TriggeringUserID, original.TriggeringUserID)
	}
	if roundtrip.ChannelID != original.ChannelID {
		t.Errorf("ChannelID 往返不一致: %q vs %q", roundtrip.ChannelID, original.ChannelID)
	}
	if roundtrip.GroupDigitalAvatar != original.GroupDigitalAvatar {
		t.Errorf("GroupDigitalAvatar 往返不一致: %v vs %v", roundtrip.GroupDigitalAvatar, original.GroupDigitalAvatar)
	}
	if roundtrip.WebUserID != original.WebUserID {
		t.Errorf("WebUserID 往返不一致: %q vs %q", roundtrip.WebUserID, original.WebUserID)
	}
}

// ──────────────────────────── Validate 测试 ────────────────────────────

// TestPermissionContext_Validate_正常 验证正常数据通过校验
func TestPermissionContext_Validate_正常(t *testing.T) {
	pc := NewPermissionContext(WithPermissionPrincipalUserID("user-1"))
	if err := pc.Validate(); err != nil {
		t.Errorf("正常数据 Validate 返回错误: %v", err)
	}
}

// TestPermissionContext_Validate_校验失败 验证缺少必填字段返回错误
func TestPermissionContext_Validate_校验失败(t *testing.T) {
	pc := NewPermissionContext()
	if err := pc.Validate(); err == nil {
		t.Error("principal_user_id 为空时期望返回错误")
	}
}

// ──────────────────────────── JSON 往返测试 ────────────────────────────

// TestPermissionContext_JSON往返 验证 JSON marshal/unmarshal 往返一致
func TestPermissionContext_JSON往返(t *testing.T) {
	original := &PermissionContext{
		PrincipalUserID:    "user-1",
		TriggeringUserID:   "sender-1",
		ChannelID:          "web",
		GroupDigitalAvatar: true,
		WebUserID:          "web-user-1",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	var decoded PermissionContext
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}

	if decoded.PrincipalUserID != original.PrincipalUserID {
		t.Errorf("PrincipalUserID: got %q, want %q", decoded.PrincipalUserID, original.PrincipalUserID)
	}
	if decoded.TriggeringUserID != original.TriggeringUserID {
		t.Errorf("TriggeringUserID: got %q, want %q", decoded.TriggeringUserID, original.TriggeringUserID)
	}
	if decoded.ChannelID != original.ChannelID {
		t.Errorf("ChannelID: got %q, want %q", decoded.ChannelID, original.ChannelID)
	}
	if decoded.GroupDigitalAvatar != original.GroupDigitalAvatar {
		t.Errorf("GroupDigitalAvatar: got %v, want %v", decoded.GroupDigitalAvatar, original.GroupDigitalAvatar)
	}
	if decoded.WebUserID != original.WebUserID {
		t.Errorf("WebUserID: got %q, want %q", decoded.WebUserID, original.WebUserID)
	}
}
