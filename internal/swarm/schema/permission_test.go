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
	// Python: enable_memory 默认 true
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

// TestPermissionContext_Scene_数字分身优先级高于web 验证 group_digital_avatar 优先级高于 web
// Python: owner_scopes.PermissionContext.scene：先检查 group_digital_avatar，再检查 web
func TestPermissionContext_Scene_数字分身优先级高于web(t *testing.T) {
	pc := NewPermissionContext(
		WithPermissionChannelID("web"),
		WithPermissionGroupDigitalAvatar(true),
	)
	if got := pc.Scene(); got != "group_digital_avatar" {
		t.Errorf("当 channel_id=web 且 group_digital_avatar=true 时，Scene() = %q, 期望 \"group_digital_avatar\"", got)
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

// TestPermissionContext_OwnerScopeKey_Trimspace 验证 OwnerScopeKey 对字段 TrimSpace
func TestPermissionContext_OwnerScopeKey_Trimspace(t *testing.T) {
	pc := NewPermissionContext(
		WithPermissionPrincipalUserID("  user-1  "),
		WithPermissionChannelID("  web  "),
	)
	key := pc.OwnerScopeKey()
	if key[0] != "web" {
		t.Errorf("OwnerScopeKey()[0] = %q, 期望 \"web\"（TrimSpace 后）", key[0])
	}
	if key[1] != "user-1" {
		t.Errorf("OwnerScopeKey()[1] = %q, 期望 \"user-1\"（TrimSpace 后）", key[1])
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
	// Python: meta.get("enable_memory", True) — 缺失时默认 true
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

// ──────────────────────────── SetupPermissionContext 测试 ────────────────────────────

// TestSetupPermissionContext_数字分身模式 验证 avatar_mode 场景
func TestSetupPermissionContext_数字分身模式(t *testing.T) {
	metadata := map[string]any{
		"avatar_mode":           true,
		"group_digital_avatar":  true,
		"principal_user_id":     "user_123",
		"triggering_user_id":    "sender_456",
		"avatar_principal_name": "助手A",
	}
	pc := SetupPermissionContext("feishu_ch1", metadata)
	if pc == nil {
		t.Fatal("数字分身模式应返回非 nil")
	}
	if pc.Scene() != "group_digital_avatar" {
		t.Fatalf("scene=%s，期望 group_digital_avatar", pc.Scene())
	}
	if pc.PrincipalUserID != "user_123" {
		t.Fatalf("principal_user_id=%s，期望 user_123", pc.PrincipalUserID)
	}
	if pc.ChannelID != "feishu_ch1" {
		t.Fatalf("channel_id=%s，期望 feishu_ch1", pc.ChannelID)
	}
}

// TestSetupPermissionContext_非数字分身_禁用记忆 验证 enable_memory=false 场景
func TestSetupPermissionContext_非数字分身_禁用记忆(t *testing.T) {
	metadata := map[string]any{
		"enable_memory": false,
	}
	pc := SetupPermissionContext("web", metadata)
	if pc == nil {
		t.Fatal("禁用记忆应返回非 nil")
	}
	if pc.EnableMemory {
		t.Fatal("EnableMemory 应为 false")
	}
	if pc.AvatarMode {
		t.Fatal("AvatarMode 应为 false")
	}
	if pc.ChannelID != "web" {
		t.Fatalf("ChannelID=%s，期望 web", pc.ChannelID)
	}
}

// TestSetupPermissionContext_无metadata返回nil 验证无 avatar_mode 且非禁用记忆时返回 nil
func TestSetupPermissionContext_无metadata返回nil(t *testing.T) {
	pc := SetupPermissionContext("web", nil)
	if pc != nil {
		t.Fatal("无 avatar_mode 且非禁用记忆时应返回 nil")
	}

	pc = SetupPermissionContext("web", map[string]any{})
	if pc != nil {
		t.Fatal("空 metadata 应返回 nil")
	}

	pc = SetupPermissionContext("web", map[string]any{"enable_memory": true})
	if pc != nil {
		t.Fatal("enable_memory=true 非 avatar_mode 时应返回 nil")
	}
}

// TestSetupPermissionContext_channelID回填 验证 metadata 无 channel_id 时使用参数
func TestSetupPermissionContext_channelID回填(t *testing.T) {
	metadata := map[string]any{
		"avatar_mode":          true,
		"group_digital_avatar": true,
		"principal_user_id":    "user_123",
	}
	pc := SetupPermissionContext("feishu_ch1", metadata)
	if pc == nil {
		t.Fatal("数字分身模式应返回非 nil")
	}
	if pc.ChannelID != "feishu_ch1" {
		t.Fatalf("metadata 无 channel_id 时应使用参数，得到 %s", pc.ChannelID)
	}
}

// ──────────────────────────── principal_user_id 分支（9.90e Task 9）────────────────────────────

// TestSetupPermissionContext_非数字分身有principal_user_id 验证新增分支。
//
// Python: if principal_user_id or meta.get("enable_memory") is False:
//
// Go 原实现只检查了后半句，漏掉 principal_user_id——会导致非 avatar 模式下
// workspace provenance 与 owner_scopes 查找拿不到 owner token。
func TestSetupPermissionContext_非数字分身有principal_user_id(t *testing.T) {
	metadata := map[string]any{
		"principal_user_id":  "user_abc",
		"triggering_user_id": "sender_def",
	}
	pc := SetupPermissionContext("feishu_ch1", metadata)
	if pc == nil {
		t.Fatal("有 principal_user_id 时应返回非 nil（原实现漏此分支）")
	}
	if pc.PrincipalUserID != "user_abc" {
		t.Errorf("PrincipalUserID = %q, want user_abc", pc.PrincipalUserID)
	}
	if pc.TriggeringUserID != "sender_def" {
		t.Errorf("TriggeringUserID = %q, want sender_def（原实现丢弃该字段）", pc.TriggeringUserID)
	}
	if pc.ChannelID != "feishu_ch1" {
		t.Errorf("ChannelID = %q, want feishu_ch1", pc.ChannelID)
	}
	if pc.AvatarMode {
		t.Error("AvatarMode 应为 false")
	}
	if !pc.EnableMemory {
		t.Error("未显式禁用时 EnableMemory 应为 true")
	}
}

// TestSetupPermissionContext_非数字分身带完整字段 验证不丢字段。
func TestSetupPermissionContext_非数字分身带完整字段(t *testing.T) {
	metadata := map[string]any{
		"principal_user_id":     "user_abc",
		"triggering_user_id":    "sender_def",
		"avatar_principal_name": "助手B",
		"group_digital_avatar":  true,
	}
	pc := SetupPermissionContext("feishu_ch1", metadata)
	if pc == nil {
		t.Fatal("应返回非 nil")
	}
	if pc.AvatarPrincipalName != "助手B" {
		t.Errorf("AvatarPrincipalName = %q，原实现丢弃该字段", pc.AvatarPrincipalName)
	}
	if pc.AvatarMode {
		t.Error("AvatarMode 应为 false（metadata 未声明 avatar_mode）")
	}
}

// TestSetupPermissionContext_principal为空白串仍返回nil 验证 trim 后判定。
//
// Python: principal_user_id = str(meta.get("principal_user_id", "")).strip()
//
//	if principal_user_id or ... → 空白串 falsy
func TestSetupPermissionContext_principal为空白串仍返回nil(t *testing.T) {
	pc := SetupPermissionContext("web", map[string]any{
		"principal_user_id": "   ",
	})
	if pc != nil {
		t.Fatal("principal_user_id 为空白串时应返回 nil")
	}
}

// TestSetupPermissionContext_principal与非禁用记忆并存 验证两条件互不干扰。
func TestSetupPermissionContext_principal与非禁用记忆并存(t *testing.T) {
	pc := SetupPermissionContext("web", map[string]any{
		"principal_user_id": "user_abc",
		"enable_memory":     true,
	})
	if pc == nil {
		t.Fatal("有 principal_user_id 时应返回非 nil，即使 enable_memory=true")
	}
	if !pc.EnableMemory {
		t.Error("enable_memory=true 应保留")
	}
}

// ──────────────────────────── CurrentPermissionOwnerScope（9.90e Task 9）────────────────────────────

// TestCurrentPermissionOwnerScope_标准三段 验证正常格式。
func TestCurrentPermissionOwnerScope_标准三段(t *testing.T) {
	ctx := WithPermissionContextValue(context.Background(), NewPermissionContext(
		WithPermissionChannelID("feishu_ch1"),
		WithPermissionPrincipalUserID("user_abc"),
	))
	if got := CurrentPermissionOwnerScope(ctx); got != "principal:feishu_ch1:user_abc" {
		t.Errorf("got %q", got)
	}
}

// TestCurrentPermissionOwnerScope_空channel用default 验证占位。
//
// Python: f"principal:{channel_id or 'default'}:{principal_user_id}"
func TestCurrentPermissionOwnerScope_空channel用default(t *testing.T) {
	ctx := WithPermissionContextValue(context.Background(), NewPermissionContext(
		WithPermissionPrincipalUserID("user_abc"),
	))
	if got := CurrentPermissionOwnerScope(ctx); got != "principal:default:user_abc" {
		t.Errorf("got %q, want principal:default:user_abc", got)
	}
}

// TestCurrentPermissionOwnerScope_空白channel用default 验证 trim 后占位。
func TestCurrentPermissionOwnerScope_空白channel用default(t *testing.T) {
	ctx := WithPermissionContextValue(context.Background(), NewPermissionContext(
		WithPermissionChannelID("   "),
		WithPermissionPrincipalUserID("user_abc"),
	))
	if got := CurrentPermissionOwnerScope(ctx); got != "principal:default:user_abc" {
		t.Errorf("got %q", got)
	}
}

// TestCurrentPermissionOwnerScope_无principal返回空 验证空串。
func TestCurrentPermissionOwnerScope_无principal返回空(t *testing.T) {
	ctx := WithPermissionContextValue(context.Background(), NewPermissionContext(
		WithPermissionChannelID("feishu_ch1"),
	))
	if got := CurrentPermissionOwnerScope(ctx); got != "" {
		t.Errorf("got %q, want 空串", got)
	}
}

// TestCurrentPermissionOwnerScope_空白principal返回空 验证 trim 后判定。
func TestCurrentPermissionOwnerScope_空白principal返回空(t *testing.T) {
	ctx := WithPermissionContextValue(context.Background(), NewPermissionContext(
		WithPermissionChannelID("feishu_ch1"),
		WithPermissionPrincipalUserID("   "),
	))
	if got := CurrentPermissionOwnerScope(ctx); got != "" {
		t.Errorf("got %q, want 空串", got)
	}
}

// TestCurrentPermissionOwnerScope_无上下文返回空 验证 nil 上下文。
func TestCurrentPermissionOwnerScope_无上下文返回空(t *testing.T) {
	if got := CurrentPermissionOwnerScope(context.Background()); got != "" {
		t.Errorf("got %q, want 空串", got)
	}
}

// TestStringFieldOf_nil与类型不符 验证辅助函数兜底。
func TestStringFieldOf_nil与类型不符(t *testing.T) {
	if got := stringFieldOf(nil, "k"); got != "" {
		t.Errorf("nil map got %q", got)
	}
	if got := stringFieldOf(map[string]any{"k": 42}, "k"); got != "" {
		t.Errorf("非字符串 got %q", got)
	}
	if got := stringFieldOf(map[string]any{"k": "v"}, "k"); got != "v" {
		t.Errorf("got %q, want v", got)
	}
	if got := stringFieldOf(map[string]any{}, "missing"); got != "" {
		t.Errorf("缺键 got %q", got)
	}
}

// ──────────────────────────── Scene/OwnerScopeKey 的 trim 语义（从 owner_scopes 迁移）────────────────────────────

// TestPermissionContext_Scene_空格channelID 验证 TrimSpace 对齐 Python strip()。
//
// Python: if self.channel_id.strip() == "web": return "web"
// 原 owner_scopes 包同名用例，随重复结构体删除迁到此处。
func TestPermissionContext_Scene_空格channelID(t *testing.T) {
	pc := NewPermissionContext(WithPermissionChannelID("  web  "))
	if got := pc.Scene(); got != "web" {
		t.Errorf("Scene() = %q, 期望 \"web\"（应 trim 后判定）", got)
	}
}

// TestPermissionContext_Scene_空格channelID数字分身仍优先 验证空格不影响优先级。
func TestPermissionContext_Scene_空格channelID数字分身仍优先(t *testing.T) {
	pc := NewPermissionContext(
		WithPermissionChannelID("  web  "),
		WithPermissionGroupDigitalAvatar(true),
	)
	if got := pc.Scene(); got != "group_digital_avatar" {
		t.Errorf("Scene() = %q, 期望 \"group_digital_avatar\"", got)
	}
}

// TestPermissionContext_OwnerScopeKey_两侧trim 验证两字段都按 strip 处理。
//
// Python: return self.channel_id.strip(), self.principal_user_id.strip()
func TestPermissionContext_OwnerScopeKey_两侧trim(t *testing.T) {
	pc := NewPermissionContext(
		WithPermissionPrincipalUserID("  user-1  "),
		WithPermissionChannelID("  feishu  "),
	)
	key := pc.OwnerScopeKey()
	if key[0] != "feishu" {
		t.Errorf("OwnerScopeKey()[0] = %q, 期望 feishu（应 trim）", key[0])
	}
	if key[1] != "user-1" {
		t.Errorf("OwnerScopeKey()[1] = %q, 期望 user-1（应 trim）", key[1])
	}
}
