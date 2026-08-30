package permissions

import (
	"strings"
	"testing"
)

// ──────────────────────────── NewOwnerScopesPermissionContextFromDict 测试 ────────────────────────────

// TestNewOwnerScopesPermissionContextFromDict_完整字段 验证所有字段正确解析
func TestNewOwnerScopesPermissionContextFromDict_完整字段(t *testing.T) {
	data := map[string]any{
		"channel_id":            "feishu",
		"group_digital_avatar":  true,
		"principal_user_id":     "user-1",
		"triggering_user_id":    "sender-1",
		"enable_memory":         false,
		"avatar_principal_name": "张三",
		"avatar_mode":           true,
	}
	pc := NewOwnerScopesPermissionContextFromDict(data)
	if pc.ChannelID != "feishu" {
		t.Errorf("ChannelID = %q, 期望 \"feishu\"", pc.ChannelID)
	}
	if !pc.GroupDigitalAvatar {
		t.Error("GroupDigitalAvatar 应为 true")
	}
	if pc.PrincipalUserID != "user-1" {
		t.Errorf("PrincipalUserID = %q, 期望 \"user-1\"", pc.PrincipalUserID)
	}
	if pc.TriggeringUserID != "sender-1" {
		t.Errorf("TriggeringUserID = %q, 期望 \"sender-1\"", pc.TriggeringUserID)
	}
	if pc.EnableMemory {
		t.Error("EnableMemory 应为 false")
	}
	if pc.AvatarPrincipalName != "张三" {
		t.Errorf("AvatarPrincipalName = %q, 期望 \"张三\"", pc.AvatarPrincipalName)
	}
	if !pc.AvatarMode {
		t.Error("AvatarMode 应为 true")
	}
}

// TestNewOwnerScopesPermissionContextFromDict_默认值 验证缺失字段使用零值 + EnableMemory 默认 true
func TestNewOwnerScopesPermissionContextFromDict_默认值(t *testing.T) {
	data := map[string]any{
		"channel_id": "feishu",
	}
	pc := NewOwnerScopesPermissionContextFromDict(data)
	if pc.ChannelID != "feishu" {
		t.Errorf("ChannelID = %q, 期望 \"feishu\"", pc.ChannelID)
	}
	if pc.GroupDigitalAvatar {
		t.Error("GroupDigitalAvatar 缺失时应为 false")
	}
	if !pc.EnableMemory {
		t.Error("EnableMemory 缺失时应为 true（默认值）")
	}
	if pc.AvatarMode {
		t.Error("AvatarMode 缺失时应为 false")
	}
}

// TestNewOwnerScopesPermissionContextFromDict_空字典 验证空字典返回默认值
func TestNewOwnerScopesPermissionContextFromDict_空字典(t *testing.T) {
	pc := NewOwnerScopesPermissionContextFromDict(map[string]any{})
	if !pc.EnableMemory {
		t.Error("空字典时 EnableMemory 应为 true（默认值）")
	}
}

// ──────────────────────────── Scene 测试 ────────────────────────────

// TestOwnerScopesPermissionContext_Scene_数字分身优先 验证 GroupDigitalAvatar 优先于 web
func TestOwnerScopesPermissionContext_Scene_数字分身优先(t *testing.T) {
	pc := &OwnerScopesPermissionContext{
		ChannelID:          "web",
		GroupDigitalAvatar: true,
	}
	if got := pc.Scene(); got != "group_digital_avatar" {
		t.Errorf("当 group_digital_avatar=true 且 channel_id=web 时，Scene() = %q, 期望 \"group_digital_avatar\"", got)
	}
}

// TestOwnerScopesPermissionContext_Scene_web 验证非数字分身时 web 场景
func TestOwnerScopesPermissionContext_Scene_web(t *testing.T) {
	pc := &OwnerScopesPermissionContext{
		ChannelID:          "web",
		GroupDigitalAvatar: false,
	}
	if got := pc.Scene(); got != "web" {
		t.Errorf("Scene() = %q, 期望 \"web\"", got)
	}
}

// TestOwnerScopesPermissionContext_Scene_普通IM 验证默认场景
func TestOwnerScopesPermissionContext_Scene_普通IM(t *testing.T) {
	pc := &OwnerScopesPermissionContext{
		ChannelID:          "feishu",
		GroupDigitalAvatar: false,
	}
	if got := pc.Scene(); got != "normal_im" {
		t.Errorf("Scene() = %q, 期望 \"normal_im\"", got)
	}
}

// TestOwnerScopesPermissionContext_Scene_空格channelID 验证 TrimSpace 对齐 Python strip()
func TestOwnerScopesPermissionContext_Scene_空格channelID(t *testing.T) {
	pc := &OwnerScopesPermissionContext{
		ChannelID:          " web ",
		GroupDigitalAvatar: false,
	}
	if got := pc.Scene(); got != "web" {
		t.Errorf("带空格的 channel_id=\" web \" 时 Scene() = %q, 期望 \"web\"", got)
	}
}

// TestOwnerScopesPermissionContext_Scene_空格channelID数字分身仍优先 验证空格不影响数字分身优先级
func TestOwnerScopesPermissionContext_Scene_空格channelID数字分身仍优先(t *testing.T) {
	pc := &OwnerScopesPermissionContext{
		ChannelID:          " web ",
		GroupDigitalAvatar: true,
	}
	if got := pc.Scene(); got != "group_digital_avatar" {
		t.Errorf("带空格 channel_id + group_digital_avatar=true 时 Scene() = %q, 期望 \"group_digital_avatar\"", got)
	}
}

// ──────────────────────────── OwnerScopeKey 测试 ────────────────────────────

// TestOwnerScopesPermissionContext_OwnerScopeKey 验证返回 [channel_id, principal_user_id]
func TestOwnerScopesPermissionContext_OwnerScopeKey(t *testing.T) {
	pc := &OwnerScopesPermissionContext{
		ChannelID:       "feishu",
		PrincipalUserID: "user-1",
	}
	key := pc.OwnerScopeKey()
	if key[0] != "feishu" {
		t.Errorf("OwnerScopeKey()[0] = %q, 期望 \"feishu\"", key[0])
	}
	if key[1] != "user-1" {
		t.Errorf("OwnerScopeKey()[1] = %q, 期望 \"user-1\"", key[1])
	}
}

// ──────────────────────────── 依赖 strings 验证 ────────────────────────────

// TestStringsImportUsed 验证 strings 包被正确使用（编译时验证）
func TestStringsImportUsed(t *testing.T) {
	_ = strings.TrimSpace
}
