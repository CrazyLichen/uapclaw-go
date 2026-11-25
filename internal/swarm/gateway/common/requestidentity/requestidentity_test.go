package requestidentity

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNormalizeRoutingIdentity 对齐 wire 层头归一化：去空白、丢空值。
func TestNormalizeRoutingIdentity(t *testing.T) {
	got := NormalizeRoutingIdentity(map[string]string{
		"user_id":    " u1 ",
		"group_id":   "  ",
		"bot_id":     "b1",
		"gateway_id": "",
	})
	require.Equal(t, map[string]string{"user_id": "u1", "bot_id": "b1"}, got)
	require.Len(t, NormalizeRoutingIdentity(map[string]string{}), 0)
}

// TestApplyRoutingMetadata user_id 摊顶层、三元组收进 routing。
func TestApplyRoutingMetadata(t *testing.T) {
	meta := ApplyRoutingMetadata(
		map[string]any{"method": "chat.send"},
		map[string]string{"user_id": "u1", "group_id": "g1", "bot_id": "b1", "gateway_id": ""},
	)
	require.Equal(t, "u1", meta["user_id"])
	require.Equal(t, map[string]any{"group_id": "g1", "bot_id": "b1"}, meta["routing"])
	require.Equal(t, "chat.send", meta["method"])
}

// TestApplyRoutingMetadata空身份不写键 全空身份不产生 routing 键。
func TestApplyRoutingMetadata空身份不写键(t *testing.T) {
	meta := ApplyRoutingMetadata(map[string]any{}, map[string]string{})
	_, hasUser := meta["user_id"]
	_, hasRouting := meta["routing"]
	require.False(t, hasUser)
	require.False(t, hasRouting)
}

// TestWebRoutingIdentity 读全含顶层 user_id。
func TestWebRoutingIdentity(t *testing.T) {
	got := WebRoutingIdentity(map[string]any{
		"user_id": "u1",
		"routing": map[string]any{"group_id": "g1", "bot_id": "b1", "gateway_id": "gw"},
	})
	require.Equal(t, map[string]string{
		"user_id": "u1", "group_id": "g1", "bot_id": "b1", "gateway_id": "gw",
	}, got)
	require.Len(t, WebRoutingIdentity(nil), 0)
}

// TestMergeRoutingIntoParams override 两分支。
func TestMergeRoutingIntoParams(t *testing.T) {
	meta := map[string]any{
		"user_id": "u1",
		"routing": map[string]any{"bot_id": "b1"},
	}
	merged := MergeRoutingIntoParams(map[string]any{"user_id": "old", "k": 1}, meta, true)
	require.Equal(t, "u1", merged["user_id"])
	require.Equal(t, "b1", merged["bot_id"])

	merged2 := MergeRoutingIntoParams(map[string]any{"user_id": "old"}, meta, false)
	require.Equal(t, "old", merged2["user_id"]) // override=false 不覆盖已有
}
