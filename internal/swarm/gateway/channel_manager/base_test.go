package channel_manager

import (
	"testing"
)

// ──────────────────────────── ChannelType 枚举测试 ────────────────────────────

// TestAllChannelTypes_返回全部枚举 测试 AllChannelTypes 返回值数量
func TestAllChannelTypes_返回全部枚举(t *testing.T) {
	types := AllChannelTypes()
	expected := 11 // acp + web + feishu + xiaoyi + dingtalk + telegram + discord + whatsapp + wecom + wechat + tui
	if len(types) != expected {
		t.Errorf("AllChannelTypes() 返回 %d 个, 期望 %d 个", len(types), expected)
	}
}

// TestChannelType_字符串值 测试各 ChannelType 常量的字符串值
func TestChannelType_字符串值(t *testing.T) {
	cases := map[ChannelType]string{
		ChannelTypeACP:      "acp",
		ChannelTypeWeb:      "web",
		ChannelTypeFeishu:   "feishu",
		ChannelTypeXiaoyi:   "xiaoyi",
		ChannelTypeDingTalk: "dingtalk",
		ChannelTypeTelegram: "telegram",
		ChannelTypeDiscord:  "discord",
		ChannelTypeWhatsApp: "whatsapp",
		ChannelTypeWeCom:    "wecom",
		ChannelTypeWeChat:   "wechat",
		ChannelTypeCLI:      "tui",
	}
	for ct, expected := range cases {
		if string(ct) != expected {
			t.Errorf("ChannelType %q 的字符串值 = %q, 期望 %q", ct, string(ct), expected)
		}
	}
}

// TestParseChannelType_合法值 测试 ParseChannelType 对合法值的解析
func TestParseChannelType_合法值(t *testing.T) {
	ct, err := ParseChannelType("web")
	if err != nil {
		t.Fatalf("ParseChannelType(\"web\") 返回错误: %v", err)
	}
	if ct != ChannelTypeWeb {
		t.Errorf("ParseChannelType(\"web\") = %v, 期望 %v", ct, ChannelTypeWeb)
	}
}

// TestParseChannelType_非法值 测试 ParseChannelType 对非法值的处理
func TestParseChannelType_非法值(t *testing.T) {
	_, err := ParseChannelType("nonexistent")
	if err == nil {
		t.Error("ParseChannelType(\"nonexistent\") 应返回错误，但返回 nil")
	}
}

// TestIsValidChannelType_合法值 测试 IsValidChannelType 对合法值的判断
func TestIsValidChannelType_合法值(t *testing.T) {
	if !IsValidChannelType("feishu") {
		t.Error("IsValidChannelType(\"feishu\") 应返回 true")
	}
}

// TestIsValidChannelType_非法值 测试 IsValidChannelType 对非法值的判断
func TestIsValidChannelType_非法值(t *testing.T) {
	if IsValidChannelType("unknown") {
		t.Error("IsValidChannelType(\"unknown\") 应返回 false")
	}
}

// TestChannelType_String 测试 String 方法
func TestChannelType_String(t *testing.T) {
	if ChannelTypeWeb.String() != "web" {
		t.Errorf("ChannelTypeWeb.String() = %q, 期望 %q", ChannelTypeWeb.String(), "web")
	}
}

// TestChannelType_GoString 测试 GoString 方法
func TestChannelType_GoString(t *testing.T) {
	expected := `channel_manager.ChannelType("web")`
	if ChannelTypeWeb.GoString() != expected {
		t.Errorf("ChannelTypeWeb.GoString() = %q, 期望 %q", ChannelTypeWeb.GoString(), expected)
	}
}

// ──────────────────────────── ChannelMetadata 测试 ────────────────────────────

// TestChannelMetadata_字段赋值 测试 ChannelMetadata 各字段赋值
func TestChannelMetadata_字段赋值(t *testing.T) {
	md := ChannelMetadata{
		ChannelID: "feishu-001",
		Source:    "feishu",
		UserID:    "user-123",
		Extra:     map[string]any{"app_id": "cli_xxx"},
	}
	if md.ChannelID != "feishu-001" {
		t.Errorf("ChannelID = %q, 期望 %q", md.ChannelID, "feishu-001")
	}
	if md.Source != "feishu" {
		t.Errorf("Source = %q, 期望 %q", md.Source, "feishu")
	}
	if md.UserID != "user-123" {
		t.Errorf("UserID = %q, 期望 %q", md.UserID, "user-123")
	}
	if md.Extra["app_id"] != "cli_xxx" {
		t.Errorf("Extra[\"app_id\"] = %v, 期望 %v", md.Extra["app_id"], "cli_xxx")
	}
}

// TestChannelMetadata_可选字段零值 测试 ChannelMetadata 可选字段零值
func TestChannelMetadata_可选字段零值(t *testing.T) {
	md := ChannelMetadata{
		ChannelID: "web-001",
		Source:    "web",
	}
	if md.UserID != "" {
		t.Errorf("UserID 零值应为空串, 实际 = %q", md.UserID)
	}
	if md.Extra != nil {
		t.Errorf("Extra 零值应为 nil, 实际 = %v", md.Extra)
	}
}

// ──────────────────────────── IsAllowed 测试 ────────────────────────────

// TestIsAllowed_空白名单允许所有人 测试 allowFrom 为空时允许所有人
func TestIsAllowed_空白名单允许所有人(t *testing.T) {
	if !IsAllowed("anyone", nil) {
		t.Error("IsAllowed(\"anyone\", nil) 应返回 true")
	}
	if !IsAllowed("anyone", []string{}) {
		t.Error("IsAllowed(\"anyone\", []) 应返回 true")
	}
}

// TestIsAllowed_在名单中 测试发送者在白名单中
func TestIsAllowed_在名单中(t *testing.T) {
	allow := []string{"user1", "user2"}
	if !IsAllowed("user1", allow) {
		t.Error("IsAllowed(\"user1\", [user1, user2]) 应返回 true")
	}
}

// TestIsAllowed_不在名单中 测试发送者不在白名单中
func TestIsAllowed_不在名单中(t *testing.T) {
	allow := []string{"user1", "user2"}
	if IsAllowed("user3", allow) {
		t.Error("IsAllowed(\"user3\", [user1, user2]) 应返回 false")
	}
}

// TestIsAllowed_竖线分隔匹配 测试 senderID 含 "|" 时逐段匹配
func TestIsAllowed_竖线分隔匹配(t *testing.T) {
	allow := []string{"user1", "user2"}
	if !IsAllowed("user1|user3", allow) {
		t.Error("IsAllowed(\"user1|user3\", [user1, user2]) 应返回 true（user1 命中）")
	}
	if IsAllowed("user3|user4", allow) {
		t.Error("IsAllowed(\"user3|user4\", [user1, user2]) 应返回 false（无命中）")
	}
}
