package handoff

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// TestHandoffHistoryEntry_字段 测试 HandoffHistoryEntry 结构体字段赋值
func TestHandoffHistoryEntry_字段(t *testing.T) {
	entry := HandoffHistoryEntry{
		AgentID: "agent_1",
		Output:  map[string]any{"result": "ok"},
	}
	assert.Equal(t, "agent_1", entry.AgentID)
	assert.Equal(t, map[string]any{"result": "ok"}, entry.Output)
}

// TestHandoffRequest_字段 测试 HandoffRequest 结构体字段赋值
func TestHandoffRequest_字段(t *testing.T) {
	sess := session.NewAgentTeamSession()
	req := HandoffRequest{
		InputMessage: map[string]any{"prompt": "hello"},
		History: []HandoffHistoryEntry{
			{AgentID: "agent_1", Output: map[string]any{"step": 1}},
		},
		Session: sess,
	}
	assert.Equal(t, map[string]any{"prompt": "hello"}, req.InputMessage)
	assert.Len(t, req.History, 1)
	assert.Equal(t, "agent_1", req.History[0].AgentID)
	assert.NotNil(t, req.Session)
}

// TestHandoffRequest_SessionID_有session 测试有 session 时返回 sessionID
func TestHandoffRequest_SessionID_有session(t *testing.T) {
	sess := session.NewAgentTeamSession(
		session.WithAgentTeamSessionID("test-session-123"),
	)
	req := HandoffRequest{Session: sess}
	assert.Equal(t, "test-session-123", req.SessionID())
}

// TestHandoffRequest_SessionID_无session 测试无 session 时返回空字符串
func TestHandoffRequest_SessionID_无session(t *testing.T) {
	req := HandoffRequest{Session: nil}
	assert.Equal(t, "", req.SessionID())
}
