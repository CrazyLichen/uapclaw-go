package runtime

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewJiuWenClaw(t *testing.T) {
	jw := NewJiuWenClaw()
	require.NotNil(t, jw)
}

func TestJiuWenClaw_ProcessMessage(t *testing.T) {
	jw := NewJiuWenClaw()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodChatSend, nil)
	resp, err := jw.ProcessMessage(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, resp.OK)
	assert.Equal(t, "req-1", resp.RequestID)
}

func TestJiuWenClaw_ProcessMessageStream(t *testing.T) {
	jw := NewJiuWenClaw()
	req := schema.NewAgentRequest("req-2", "web", schema.ReqMethodChatSend, nil)
	ch, err := jw.ProcessMessageStream(context.Background(), req)
	require.NoError(t, err)

	chunkCount := 0
	for chunk := range ch {
		chunkCount++
		assert.Equal(t, "req-2", chunk.RequestID)
	}
	// stub 发送 2 个 chunk（1 个内容 + 1 个 terminal）
	assert.Equal(t, 2, chunkCount)
}

func TestJiuWenClaw_ProcessInterrupt(t *testing.T) {
	jw := NewJiuWenClaw()
	req := schema.NewAgentRequest("req-3", "web", schema.ReqMethodChatCancel, nil)
	resp, err := jw.ProcessInterrupt(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, resp.OK)
}

func TestJiuWenClaw_GetContextUsage(t *testing.T) {
	jw := NewJiuWenClaw()
	usage, err := jw.GetContextUsage("sess-1")
	require.NoError(t, err)
	assert.Equal(t, 0, usage["usage"])
	assert.Equal(t, 0, usage["limit"])
}

func TestJiuWenClaw_CompressContext(t *testing.T) {
	jw := NewJiuWenClaw()
	result, err := jw.CompressContext("sess-1")
	require.NoError(t, err)
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, false, result["compressed"])
}

func TestJiuWenClaw_SwitchMode(t *testing.T) {
	jw := NewJiuWenClaw()
	err := jw.SwitchMode("sess-1", "code.normal")
	assert.NoError(t, err)
}
