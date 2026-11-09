package adapter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseTypedChunkMap_团队工具无result键时回退整体 关闭 F-03 的"result 为空"悬案。
//
// 团队工具统一返回 `toolSuccess(data)` = {"success": true, "data": {...}}，
// **没有 `result` 键**。上游 `result_info.get("result", str(result_info))` 的
// 语义是：键存在即采用（空串也是合法结果），键缺失才回退整体字符串。
// 因此团队工具的 chat.tool_result 里 result 应是非空的整体文本，
// 而不是空串——F-02 观测到的 `result:""` 在当前实现下不复现。
func TestParseTypedChunkMap_团队工具无result键时回退整体(t *testing.T) {
	ret := parseTypedChunkMap(map[string]any{
		"type": "tool_result",
		"payload": map[string]any{
			"tool_result": map[string]any{
				"success":   true,
				"tool_name": "team.build_team",
				"data":      map[string]any{"team_name": "t1"},
			},
		},
	})

	require.NotNil(t, ret)
	assert.Equal(t, "chat.tool_result", ret["event_type"])
	assert.Equal(t, "team.build_team", ret["tool_name"])
	result, ok := ret["result"].(string)
	require.True(t, ok, "result 应是字符串")
	assert.NotEmpty(t, result, "无 result 键时必须回退整体文本，不能是空串")
	assert.Contains(t, result, "success", "回退文本应包含工具返回的结构")
}

// TestParseTypedChunkMap_工具显式空result时保留空串 键存在即采用（含空串）。
//
// 这是与上一条配对的反向契约：工具**显式**返回 result:""（如执行失败）时，
// 必须保留空串而不是回退成 map 文本——否则内部结构会泄漏到前端。
func TestParseTypedChunkMap_工具显式空result时保留空串(t *testing.T) {
	ret := parseTypedChunkMap(map[string]any{
		"type": "tool_result",
		"payload": map[string]any{
			"tool_result": map[string]any{
				"result":    "",
				"tool_name": "team.spawn_member",
			},
		},
	})

	require.NotNil(t, ret)
	assert.Equal(t, "", ret["result"], "显式空串必须保留，不得回退成 map 文本")
}
