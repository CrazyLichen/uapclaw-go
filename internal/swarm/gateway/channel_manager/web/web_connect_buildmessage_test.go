package web

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

func TestBuildUserMessage_基本构造(t *testing.T) {
	params := map[string]any{"query": "你好", "session_id": "sess-1"}
	msg := BuildUserMessage("req-1", "chat.send", params, "sess-1", nil)

	assert.Equal(t, "req-1", msg.ID)
	assert.Equal(t, schema.MessageTypeReq, msg.Type)
	assert.Equal(t, "web", msg.ChannelID)
	assert.Equal(t, "sess-1", msg.SessionID)
	assert.Equal(t, schema.ReqMethodChatSend, msg.ReqMethod)
	assert.True(t, msg.OK)
}

// TestForwardReqMethods_核心方法 测试核心方法在集合中

func TestParseMode_有mode(t *testing.T) {
	assert.Equal(t, schema.ModeAgentWorkPlan, parseMode("agent.work.plan"))
	assert.Equal(t, schema.ModeAgentWorkNormal, parseMode("agent.work.normal"))
}

// TestParseMode_legacy归一 验证旧 canonical 归一（agent.plan → agent.work.plan）

func TestParseMode_legacy归一(t *testing.T) {
	assert.Equal(t, schema.ModeAgentWorkPlan, parseMode("agent.plan"))
	assert.Equal(t, schema.ModeAgentWorkNormal, parseMode("agent"))
}

// TestParseMode_缺省与非法 验证缺省/非法值回落 agent.work.normal（不再是 plan）

func TestParseMode_缺省与非法(t *testing.T) {
	assert.Equal(t, schema.ModeAgentWorkNormal, parseMode(nil))
	assert.Equal(t, schema.ModeAgentWorkNormal, parseMode(""))
	assert.Equal(t, schema.ModeAgentWorkNormal, parseMode("BUILD"))
	assert.Equal(t, schema.ModeAgentWorkNormal, parseMode([]any{"agent"}))
}

// TestBuildUserMessage_带Query 测试带 query 参数构造

func TestBuildUserMessage_带Query(t *testing.T) {
	params := map[string]any{"query": "你好"}
	query := map[string][]string{"mode": {"BUILD"}}
	msg := BuildUserMessage("req-1", "chat.send", params, "sess-1", query)
	assert.Equal(t, "req-1", msg.ID)
	assert.Equal(t, query, msg.Metadata["query"])
}
