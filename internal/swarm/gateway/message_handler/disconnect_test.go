package message_handler

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCancelAgentSessionsOnDisconnect_stub形态 T-01：断连取消 stub 的
// ws_drop_ 前缀 ID + intent/session_id params 注入——agentClient 未连接时
// 走通知路径，interrupt_result 的 request_id 即 stub ID（可观测性断言点）。
func TestCancelAgentSessionsOnDisconnect_stub形态(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	keys := [][2]string{
		{"tui", "sess-a"},
		{"tui", "sess-a"}, // 重复 → 去重
		{"feishu", "sess-b"},
		{"tui", "  "}, // 空白 → 跳过
	}
	mh.CancelAgentSessionsOnDisconnect(context.Background(), keys)

	got := map[string]bool{}
	for i := 0; i < 2; i++ {
		msg := <-mh.robotMessages
		assert.Equal(t, "chat.interrupt_result", string(msg.EventType))
		got[msg.SessionID] = true
		// T-01 核心断言：通知 ID 来自 ws_drop_ 前缀 stub（此前是裸 sid）
		assert.True(t, strings.HasPrefix(msg.ID, "ws_drop_"),
			"stub ID 应带 ws_drop_ 前缀, got %q", msg.ID)
		// 格式：ws_drop_{hex 毫秒}_{4 hex 随机}
		rest := strings.TrimPrefix(msg.ID, "ws_drop_")
		parts := strings.Split(rest, "_")
		if assert.Len(t, parts, 2) {
			assert.NotEmpty(t, parts[0])
			assert.Len(t, parts[1], 4, "随机段应为 4 个 hex 字符（token_hex(2)）")
		}
	}
	assert.True(t, got["sess-a"] && got["sess-b"], "应取消 sess-a 与 sess-b（去重后共 2 条）: %v", got)
}
