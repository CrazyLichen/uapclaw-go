package adapter

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// TestIsHostPermissionUpdateInput_非CHAT_SEND返回false
// Python: interface_deep.py:3767-3768 if request.req_method != ReqMethod.CHAT_SEND: return False
func TestIsHostPermissionUpdateInput_非CHAT_SEND返回false(t *testing.T) {
	req := &schema.AgentRequest{
		RequestID: "r1",
		ChannelID: "web",
		ReqMethod: schema.ReqMethodChatCancel,
	}
	require.False(t, isHostPermissionUpdateInput(req))
}

// TestIsHostPermissionUpdateInput_skipA2UI返回false
// Python: interface_deep.py:3769-3770 if metadata.get("skip_a2ui") is True: return False
func TestIsHostPermissionUpdateInput_skipA2UI返回false(t *testing.T) {
	req := &schema.AgentRequest{
		RequestID: "r1",
		ChannelID: "web",
		ReqMethod: schema.ReqMethodChatSend,
		Metadata:  map[string]any{"skip_a2ui": true},
	}
	require.False(t, isHostPermissionUpdateInput(req))
}

// TestIsHostPermissionUpdateInput_团队模式返回false
// Python: interface_deep.py:3771-3773 is_team_params(params) or runtime_mode == "auto_harness"
func TestIsHostPermissionUpdateInput_团队模式返回false(t *testing.T) {
	req := &schema.AgentRequest{
		RequestID: "r1",
		ChannelID: "web",
		ReqMethod: schema.ReqMethodChatSend,
		Params:    json.RawMessage(`{"mode":"team.normal"}`),
	}
	require.False(t, isHostPermissionUpdateInput(req))
}

// TestIsHostPermissionUpdateInput_autoHarness返回false
func TestIsHostPermissionUpdateInput_autoHarness返回false(t *testing.T) {
	req := &schema.AgentRequest{
		RequestID: "r1",
		ChannelID: "web",
		ReqMethod: schema.ReqMethodChatSend,
		Params:    json.RawMessage(`{"mode":"auto_harness"}`),
	}
	require.False(t, isHostPermissionUpdateInput(req))
}

// TestIsHostPermissionUpdateInput_普通用户消息返回true
// Python: interface_deep.py:3778-3783 委托 is_external_user_authored_dispatch
func TestIsHostPermissionUpdateInput_普通用户消息返回true(t *testing.T) {
	req := &schema.AgentRequest{
		RequestID: "r1",
		ChannelID: "web",
		ReqMethod: schema.ReqMethodChatSend,
		Params:    json.RawMessage(`{"mode":"agent.code.normal"}`),
	}
	require.True(t, isHostPermissionUpdateInput(req))
}

// TestIsHostPermissionUpdateInput_nil请求返回false
func TestIsHostPermissionUpdateInput_nil请求返回false(t *testing.T) {
	require.False(t, isHostPermissionUpdateInput(nil))
}
