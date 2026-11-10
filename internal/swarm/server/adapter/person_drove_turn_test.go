package adapter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPersonDroveTurn_八分支 逐条对齐 interface.py:467-479 与其依赖的两个判定。
func TestPersonDroveTurn_八分支(t *testing.T) {
	cases := []struct {
		name          string
		params        map[string]any
		channelID     string
		requestMethod string
		isTeamMode    bool
		want          bool
	}{
		{"团队轮不算", map[string]any{}, "web", "chat.send", true, false},
		{"跨会话消息不算", map[string]any{sessionMessageInternalKey: map[string]any{}}, "web", "chat.send", false, false},
		{"回答提问算", map[string]any{"_interrupt_resume": true}, "web", "chat.send", false, true},
		{"心跳 automation 不算", map[string]any{"automation": map[string]any{"kind": "Heartbeat"}}, "web", "chat.send", false, false},
		{"log_as_user=false 不算", map[string]any{"log_as_user": false}, "web", "chat.send", false, false},
		{"attach_goal 不算", map[string]any{"attach_goal": true}, "web", "chat.send", false, false},
		{"cron 通道不算", map[string]any{}, "cron", "chat.send", false, false},
		{"command.goal 不算", map[string]any{}, "web", reqMethodCommandGoal, false, false},
		{"带 source 不算", map[string]any{"source": "recommendation"}, "web", "chat.send", false, false},
		{"普通用户消息算", map[string]any{"query": "hi"}, "web", "chat.send", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := personDroveTurn(c.params, c.channelID, c.requestMethod, nil, c.isTeamMode)
			assert.Equal(t, c.want, got)
		})
	}
}

// TestPersonDroveTurn_嵌套metadata也拦 对齐「合并 metadata 无法抹掉标记」。
func TestPersonDroveTurn_嵌套metadata也拦(t *testing.T) {
	params := map[string]any{
		"metadata": map[string]any{sessionMessageInternalKey: map[string]any{"from": "s2"}},
	}
	assert.False(t, personDroveTurn(params, "web", "chat.send", nil, false),
		"params.metadata 里的跨会话标记同样拦截")
}

// TestTerminalStatusOf 终态映射。
func TestTerminalStatusOf(t *testing.T) {
	assert.Equal(t, "success", terminalStatusOf(nil))
}
