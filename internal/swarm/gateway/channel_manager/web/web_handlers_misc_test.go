package web

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── connection.status ────────────────────────────

// TestConnectionStatus_无AgentClient 未接 AgentClient 时 agent_ready=false，协议版本固定 1.0。
func TestConnectionStatus_无AgentClient(t *testing.T) {
	payload, err := callHandler(connectionStatus(newTestChannel(), nil), nil, "")
	require.NoError(t, err)
	assert.Equal(t, false, payload["agent_ready"])
	assert.Equal(t, "1.0", payload["protocol_version"])
}

// ──────────────────────────── commands.list ────────────────────────────

// TestCommandsList_五个内置命令 清单与上游 BUILTIN_COMMANDS_META 一致，description 取中文。
func TestCommandsList_五个内置命令(t *testing.T) {
	payload, err := callHandler(commandsList(newTestChannel()), nil, "")
	require.NoError(t, err)

	raw, ok := payload["commands"].([]any)
	require.True(t, ok, "commands 应为数组")
	require.Len(t, raw, 5, "上游内置命令共 5 个（fork/compact/plan/goal/persist）")

	names := make([]string, 0, len(raw))
	for _, item := range raw {
		cmd, isMap := item.(map[string]any)
		require.True(t, isMap)
		names = append(names, cmd["name"].(string))
		assert.NotEmpty(t, cmd["description"], "description 供旧客户端")
		descriptions, _ := cmd["description_i18n"].(map[string]any)
		require.NotNil(t, descriptions, "description_i18n 供新客户端")
		assert.Equal(t, descriptions["zh"], cmd["description"], "description 取中文文案")
		assert.Equal(t, "built-in", cmd["kind"])
	}
	assert.Equal(t, []string{"fork", "compact", "plan", "goal", "persist"}, names)
}

// TestCommandsList_带workMode 所有命令 available_modes 均为空，work_mode 不参与过滤。
func TestCommandsList_带workMode(t *testing.T) {
	payload, err := callHandler(commandsList(newTestChannel()), map[string]any{"work_mode": "agent"}, "")
	require.NoError(t, err)
	assert.Len(t, payload["commands"].([]any), 5)
}

// ──────────────────────────── config.moa_status ────────────────────────────

// TestConfigMoaStatus_默认未配置 catalog 未移植期固定回 configured=false。
func TestConfigMoaStatus_默认未配置(t *testing.T) {
	payload, err := callHandler(configMoaStatus(newTestChannel()), nil, "")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"configured": false}, payload)
}

// ──────────────────────────── chat.swarmflow_reply ────────────────────────────

// TestChatSwarmflowReply_空ack 形状为 {accepted:true, session_id: 连接会话}。
func TestChatSwarmflowReply_空ack(t *testing.T) {
	payload, err := callHandler(chatSwarmflowReply(newTestChannel()), map[string]any{}, "sess-sw")
	require.NoError(t, err)
	assert.Equal(t, true, payload["accepted"])
	assert.Equal(t, "sess-sw", payload["session_id"])
}
