package schema

import (
	"context"
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
	// 对齐 Python: enable_memory 默认 true
	if !pc.EnableMemory {
		t.Error("EnableMemory 应为 true（默认值）")
	}
	if pc.AvatarPrincipalName != "" {
		t.Errorf("AvatarPrincipalName 应为空，实际 %q", pc.AvatarPrincipalName)
	}
	if pc.AvatarMode {
		t.Error("AvatarMode 应为 false")
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
		WithPermissionEnableMemory(false),
		WithPermissionAvatarPrincipalName("张三"),
		WithPermissionAvatarMode(true),
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
		WithPermissionEnableMemory(false),
		WithPermissionAvatarPrincipalName("张三"),
		WithPermissionAvatarMode(true),
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
	if d["enable_memory"] != false {
		t.Errorf("ToDict()[\"enable_memory\"] = %v, 期望 false", d["enable_memory"])
	}
	if d["avatar_principal_name"] != "张三" {
		t.Errorf("ToDict()[\"avatar_principal_name\"] = %v, 期望 \"张三\"", d["avatar_principal_name"])
	}
	if d["avatar_mode"] != true {
		t.Errorf("ToDict()[\"avatar_mode\"] = %v, 期望 true", d["avatar_mode"])
	}
}

// TestNewPermissionContextFromDict 验证反序列化往返
func TestNewPermissionContextFromDict(t *testing.T) {
	data := map[string]any{
		"principal_user_id":     "user-1",
		"triggering_user_id":    "sender-1",
		"channel_id":            "web",
		"group_digital_avatar":  true,
		"web_user_id":           "web-user-1",
		"enable_memory":         false,
		"avatar_principal_name": "张三",
		"avatar_mode":           true,
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
	// 对齐 Python: meta.get("enable_memory", True) — 缺失时默认 true
	if !pc.EnableMemory {
		t.Error("EnableMemory 缺失时应为 true（默认值）")
	}
	if pc.AvatarMode {
		t.Error("AvatarMode 缺失时应为 false（零值）")
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
		WithPermissionEnableMemory(true),
		WithPermissionAvatarPrincipalName(""),
		WithPermissionAvatarMode(false),
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
	if roundtrip.EnableMemory != original.EnableMemory {
		t.Errorf("EnableMemory 往返不一致: %v vs %v", roundtrip.EnableMemory, original.EnableMemory)
	}
	if roundtrip.AvatarPrincipalName != original.AvatarPrincipalName {
		t.Errorf("AvatarPrincipalName 往返不一致: %q vs %q", roundtrip.AvatarPrincipalName, original.AvatarPrincipalName)
	}
	if roundtrip.AvatarMode != original.AvatarMode {
		t.Errorf("AvatarMode 往返不一致: %v vs %v", roundtrip.AvatarMode, original.AvatarMode)
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
		PrincipalUserID:     "user-1",
		TriggeringUserID:    "sender-1",
		ChannelID:           "web",
		GroupDigitalAvatar:  true,
		WebUserID:           "web-user-1",
		EnableMemory:        false,
		AvatarPrincipalName: "张三",
		AvatarMode:          true,
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
	if decoded.EnableMemory != original.EnableMemory {
		t.Errorf("EnableMemory: got %v, want %v", decoded.EnableMemory, original.EnableMemory)
	}
	if decoded.AvatarPrincipalName != original.AvatarPrincipalName {
		t.Errorf("AvatarPrincipalName: got %q, want %q", decoded.AvatarPrincipalName, original.AvatarPrincipalName)
	}
	if decoded.AvatarMode != original.AvatarMode {
		t.Errorf("AvatarMode: got %v, want %v", decoded.AvatarMode, original.AvatarMode)
	}
}

// ──────────────────────────── Context 工具函数测试 ────────────────────────────

// TestWithToolPermissionChannelID 验证 channelID 注入和读取
func TestWithToolPermissionChannelID(t *testing.T) {
	ctx := context.Background()
	ctx = WithToolPermissionChannelID(ctx, "web")
	if got := ToolPermissionChannelIDFromCtx(ctx); got != "web" {
		t.Errorf("ToolPermissionChannelIDFromCtx = %q, want %q", got, "web")
	}
}

// TestToolPermissionChannelIDFromCtx_空值 验证空 context 返回空字符串
func TestToolPermissionChannelIDFromCtx_空值(t *testing.T) {
	ctx := context.Background()
	if got := ToolPermissionChannelIDFromCtx(ctx); got != "" {
		t.Errorf("空 context 应返回空字符串，got %q", got)
	}
}

// TestWithToolPermissionChannelID_覆盖 验证后设置的值覆盖前值
func TestWithToolPermissionChannelID_覆盖(t *testing.T) {
	ctx := context.Background()
	ctx = WithToolPermissionChannelID(ctx, "web")
	ctx = WithToolPermissionChannelID(ctx, "feishu")
	if got := ToolPermissionChannelIDFromCtx(ctx); got != "feishu" {
		t.Errorf("覆盖后应返回新值，got %q", got)
	}
}

// TestWithPermissionContextValue 验证 PermissionContext 注入和读取
func TestWithPermissionContextValue(t *testing.T) {
	pc := NewPermissionContext(
		WithPermissionChannelID("web"),
		WithPermissionPrincipalUserID("user1"),
	)
	ctx := context.Background()
	ctx = WithPermissionContextValue(ctx, pc)
	got := PermissionContextFromCtx(ctx)
	if got == nil {
		t.Fatal("PermissionContextFromCtx 返回 nil")
	}
	if got.ChannelID != "web" {
		t.Errorf("ChannelID = %q, want %q", got.ChannelID, "web")
	}
	if got.PrincipalUserID != "user1" {
		t.Errorf("PrincipalUserID = %q, want %q", got.PrincipalUserID, "user1")
	}
}

// TestPermissionContextFromCtx_空值 验证空 context 返回 nil
func TestPermissionContextFromCtx_空值(t *testing.T) {
	ctx := context.Background()
	if got := PermissionContextFromCtx(ctx); got != nil {
		t.Errorf("空 context 应返回 nil，got %v", got)
	}
}
