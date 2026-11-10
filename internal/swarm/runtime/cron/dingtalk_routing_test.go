package cron

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// ──────────────────────────── staffId 判定 ────────────────────────────

// TestIsUsableDingtalkStaffID_合法与非法 空/绑定/内部前缀均不可用。
// Python: dingtalk_routing.py:31-40
func TestIsUsableDingtalkStaffID_合法与非法(t *testing.T) {
	require.True(t, IsUsableDingtalkStaffID("staff-123"))
	require.True(t, IsUsableDingtalkStaffID("  u001  "))

	require.False(t, IsUsableDingtalkStaffID(""))
	require.False(t, IsUsableDingtalkStaffID("   "))
	require.False(t, IsUsableDingtalkStaffID("dingtalk::c1::u1::1"))
	for _, prefix := range internalSessionPrefixes {
		require.False(t, IsUsableDingtalkStaffID(prefix+"abc"), "prefix=%s", prefix)
	}
}

// ──────────────────────────── 编码与解析 ────────────────────────────

// TestEncodeDingtalkCronSessionID_格式 编码为 dingtalk::conv::sender::type。
// Python: dingtalk_routing.py:43-58
func TestEncodeDingtalkCronSessionID_格式(t *testing.T) {
	got := EncodeDingtalkCronSessionID("u1", "c1", "2")
	require.Equal(t, "dingtalk::c1::u1::2", got)
}

// TestEncodeDingtalkCronSessionID_默认会话类型 空类型默认 1（私聊）。
func TestEncodeDingtalkCronSessionID_默认会话类型(t *testing.T) {
	require.Equal(t, "dingtalk::c1::u1::1", EncodeDingtalkCronSessionID("u1", "c1", ""))
}

// TestEncodeDingtalkCronSessionID_非法sender返回空 staffId 不可用时不编码。
func TestEncodeDingtalkCronSessionID_非法sender返回空(t *testing.T) {
	require.Equal(t, "", EncodeDingtalkCronSessionID("dingtalk_abc", "c1", "1"))
	require.Equal(t, "", EncodeDingtalkCronSessionID("", "c1", "1"))
}

// TestParseDingtalkCronSessionID_解析绑定 四字段齐全。
// Python: dingtalk_routing.py:61-88
func TestParseDingtalkCronSessionID_解析绑定(t *testing.T) {
	got, ok := ParseDingtalkCronSessionID("dingtalk::c1::u1::2")
	require.True(t, ok)
	require.Equal(t, "u1", got["dingtalk_sender_id"])
	require.Equal(t, "c1", got["dingtalk_chat_id"])
	require.Equal(t, "c1", got["conversation_id"])
	require.Equal(t, "2", got["conversation_type"])
}

// TestParseDingtalkCronSessionID_缺类型段默认私聊 三段时 type 默认 1。
func TestParseDingtalkCronSessionID_缺类型段默认私聊(t *testing.T) {
	got, ok := ParseDingtalkCronSessionID("dingtalk::c1::u1")
	require.True(t, ok)
	require.Equal(t, "1", got["conversation_type"])
}

// TestParseDingtalkCronSessionID_非绑定返回false 内部 id 与裸 staffId 都拒绝。
// Python: dingtalk_routing.py:74-75
func TestParseDingtalkCronSessionID_非绑定返回false(t *testing.T) {
	for _, value := range []string{"", "dingtalk_abc", "u1", "feishu::c::u::1"} {
		_, ok := ParseDingtalkCronSessionID(value)
		require.False(t, ok, "value=%q", value)
	}
}

// TestParseDingtalkCronSessionID_段数不足返回false 只有前缀时拒绝。
func TestParseDingtalkCronSessionID_段数不足返回false(t *testing.T) {
	_, ok := ParseDingtalkCronSessionID("dingtalk::")
	require.False(t, ok)
}

// TestParseDingtalkCronSessionID_内部id当staff被拒 误编码的内部会话 id 不通过。
// Python: dingtalk_routing.py:78-81
func TestParseDingtalkCronSessionID_内部id当staff被拒(t *testing.T) {
	_, ok := ParseDingtalkCronSessionID("dingtalk::c1::dingtalk_abc::1")
	require.False(t, ok)
}

// ──────────────────────────── 会话类型映射 ────────────────────────────

// TestDingtalkChatTypeFromMetadata_映射 chat_type 优先，其次 conversation_type。
// Python: dingtalk_routing.py:91-104
func TestDingtalkChatTypeFromMetadata_映射(t *testing.T) {
	group := DingtalkChatTypeFromMetadata(map[string]any{"conversation_type": "2"})
	require.NotNil(t, group)
	require.Equal(t, "group", *group)

	p2p := DingtalkChatTypeFromMetadata(map[string]any{"conversation_type": "1"})
	require.NotNil(t, p2p)
	require.Equal(t, "p2p", *p2p)

	explicit := DingtalkChatTypeFromMetadata(map[string]any{
		"chat_type": "group", "conversation_type": "1",
	})
	require.NotNil(t, explicit)
	require.Equal(t, "group", *explicit, "chat_type 优先")
}

// TestDingtalkChatTypeFromMetadata_无映射返回nil 未知类型与 nil 元数据。
func TestDingtalkChatTypeFromMetadata_无映射返回nil(t *testing.T) {
	require.Nil(t, DingtalkChatTypeFromMetadata(nil))
	require.Nil(t, DingtalkChatTypeFromMetadata(map[string]any{}))
	require.Nil(t, DingtalkChatTypeFromMetadata(map[string]any{"conversation_type": "9"}))
}

// ──────────────────────────── 上下文构造 ────────────────────────────

// TestBuildDingtalkCronSessionIDFromContext_元数据优先 metadata 的 sender 被采用。
// Python: dingtalk_routing.py:107-133
func TestBuildDingtalkCronSessionIDFromContext_元数据优先(t *testing.T) {
	got := BuildDingtalkCronSessionIDFromContext(nil, map[string]any{
		"dingtalk_sender_id": "u1", "conversation_id": "c1", "conversation_type": "2",
	})
	require.NotNil(t, got)
	require.Equal(t, "dingtalk::c1::u1::2", *got)
}

// TestBuildDingtalkCronSessionIDFromContext_回退sessionID metadata 无 sender 时用 sessionID。
func TestBuildDingtalkCronSessionIDFromContext_回退sessionID(t *testing.T) {
	sessionID := "u2"
	got := BuildDingtalkCronSessionIDFromContext(&sessionID, map[string]any{})
	require.NotNil(t, got)
	require.Contains(t, *got, "u2")
}

// TestBuildDingtalkCronSessionIDFromContext_内部id不编码 内部会话 id 无法作为 sender。
func TestBuildDingtalkCronSessionIDFromContext_内部id不编码(t *testing.T) {
	sessionID := "dingtalk_abc"
	got := BuildDingtalkCronSessionIDFromContext(&sessionID, map[string]any{})
	require.Nil(t, got, "Gateway 内部 id 绝不编码进投递绑定")
}

// TestBuildDingtalkCronSessionIDFromContext_无sender返回nil 全空时返回 nil。
func TestBuildDingtalkCronSessionIDFromContext_无sender返回nil(t *testing.T) {
	require.Nil(t, BuildDingtalkCronSessionIDFromContext(nil, nil))
}

// ──────────────────────────── 推送元数据解析 ────────────────────────────

// TestResolveDingtalkPushMetadata_绑定 绑定形态返回完整元数据。
// Python: dingtalk_routing.py:136-152
func TestResolveDingtalkPushMetadata_绑定(t *testing.T) {
	got := ResolveDingtalkPushMetadata("dingtalk::c1::u1::2")
	require.NotNil(t, got)
	require.Equal(t, "u1", got["dingtalk_sender_id"])
	require.Equal(t, "2", got["conversation_type"])
}

// TestResolveDingtalkPushMetadata_裸staff私聊 裸 staffId 视为私聊。
func TestResolveDingtalkPushMetadata_裸staff私聊(t *testing.T) {
	got := ResolveDingtalkPushMetadata("u1")
	require.NotNil(t, got)
	require.Equal(t, "u1", got["dingtalk_sender_id"])
	require.Equal(t, "1", got["conversation_type"])
}

// TestResolveDingtalkPushMetadata_内部id返回nil 内部会话 id 无法解析。
func TestResolveDingtalkPushMetadata_内部id返回nil(t *testing.T) {
	require.Nil(t, ResolveDingtalkPushMetadata("dingtalk_abc"))
	require.Nil(t, ResolveDingtalkPushMetadata(""))
}
