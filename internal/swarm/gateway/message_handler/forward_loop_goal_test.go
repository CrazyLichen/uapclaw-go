package message_handler

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// TestHasActiveEmitProcessingStatusTaskForSession_goal长流挡住false
// command.goal 按上游 emit=False，但必须挡住别人的 processing_status=false。
// Python: message_handler.py:4275-4310 _session_has_streams_blocking_processing_false
func TestHasActiveEmitProcessingStatusTaskForSession_goal长流挡住false(t *testing.T) {
	mh := newGoalTestMessageHandler()

	// emit=False 的 command.goal 长流 → 必须挡住
	mh.registerStreamTask("rid-goal", "sess-g", nil, &streamTaskEntry{cancel: func() {}}, false, "agent")
	mh.markStreamMethod("rid-goal", string(schema.ReqMethodCommandGoal))
	require.True(t, mh.hasActiveEmitProcessingStatusTaskForSession("sess-g"),
		"goal 长流必须挡住 processing_status=false")
}

// TestHasActiveEmitProcessingStatusTaskForSession_普通流按emit判定 emit=true 才算。
func TestHasActiveEmitProcessingStatusTaskForSession_普通流按emit判定(t *testing.T) {
	mh := newGoalTestMessageHandler()
	mh.registerStreamTask("rid-chat", "sess-c", nil, &streamTaskEntry{cancel: func() {}}, true, "agent")
	mh.markStreamMethod("rid-chat", string(schema.ReqMethodChatSend))
	require.True(t, mh.hasActiveEmitProcessingStatusTaskForSession("sess-c"))

	// 短只读流（emit=false 且非 goal）不计入
	mh2 := newGoalTestMessageHandler()
	mh2.registerStreamTask("rid-hist", "sess-h", nil, &streamTaskEntry{cancel: func() {}}, false, "agent")
	mh2.markStreamMethod("rid-hist", string(schema.ReqMethodHistoryGet))
	require.False(t, mh2.hasActiveEmitProcessingStatusTaskForSession("sess-h"))
}

// TestHasActiveEmitProcessingStatusTaskForSession_其它会话不干扰 只认本会话。
func TestHasActiveEmitProcessingStatusTaskForSession_其它会话不干扰(t *testing.T) {
	mh := newGoalTestMessageHandler()
	mh.registerStreamTask("rid-g2", "sess-other", nil, &streamTaskEntry{cancel: func() {}}, false, "agent")
	mh.markStreamMethod("rid-g2", string(schema.ReqMethodCommandGoal))
	require.False(t, mh.hasActiveEmitProcessingStatusTaskForSession("sess-mine"))
}

// TestUnregisterStreamTask_清掉方法名追踪 注销后不得残留。
func TestUnregisterStreamTask_清掉方法名追踪(t *testing.T) {
	mh := newGoalTestMessageHandler()
	mh.registerStreamTask("rid-x", "sess-x", nil, &streamTaskEntry{cancel: func() {}}, false, "agent")
	mh.markStreamMethod("rid-x", string(schema.ReqMethodCommandGoal))
	require.True(t, mh.hasActiveEmitProcessingStatusTaskForSession("sess-x"))

	mh.unregisterStreamTask("rid-x")
	require.False(t, mh.hasActiveEmitProcessingStatusTaskForSession("sess-x"))
	require.NotContains(t, mh.streamMethods, "rid-x")
}

// newGoalTestMessageHandler 构造一个 map 已初始化的 MessageHandler（仅测试用）。
func newGoalTestMessageHandler() *MessageHandler {
	return &MessageHandler{
		streamTasks:                 make(map[string]*streamTaskEntry),
		streamSessions:              make(map[string]string),
		streamMetadata:              make(map[string]map[string]any),
		streamModes:                 make(map[string]string),
		streamEmitsProcessingStatus: make(map[string]bool),
		streamMethods:               make(map[string]string),
	}
}
