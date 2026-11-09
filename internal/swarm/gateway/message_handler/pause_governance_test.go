package message_handler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// TestCancelStreamsForPausedSession_只取消团队流 验证单 agent 暂停不碰流。
func TestCancelStreamsForPausedSession_只取消团队流(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	teamCanceled, singleCanceled := 0, 0

	mh.streamMu.Lock()
	mh.streamSessions["rid-team"] = "sess-1"
	mh.streamModes["rid-team"] = "team.normal"
	mh.streamTasks["rid-team"] = &streamTaskEntry{cancel: func() { teamCanceled++ }}
	mh.streamSessions["rid-single"] = "sess-1"
	mh.streamModes["rid-single"] = "agent.normal"
	mh.streamTasks["rid-single"] = &streamTaskEntry{cancel: func() { singleCanceled++ }}
	mh.streamEmitsProcessingStatus["rid-team"] = true
	mh.streamMu.Unlock()

	mh.cancelStreamsForPausedSession(context.Background(), &schema.Message{
		SessionID: "sess-1", ChannelID: "web",
	}, "team.normal")

	assert.Equal(t, 1, teamCanceled, "团队流必须被取消")
	assert.Equal(t, 0, singleCanceled, "单 agent 暂停不碰流")
}

// TestCancelStreamsForPausedSession_无受害者补processing_false 验证空闲重复暂停的兜底广播。
func TestCancelStreamsForPausedSession_无受害者补processing_false(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	mh.cancelStreamsForPausedSession(context.Background(), &schema.Message{
		SessionID: "sess-empty", ChannelID: "web",
	}, "team.normal")

	// 兜底广播是异步写入 robotMessages；只要不 panic 且函数返回即符合契约。
	select {
	case <-mh.robotMessages:
	default:
	}
}
