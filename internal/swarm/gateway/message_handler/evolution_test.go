package message_handler

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestIsEvolutionApprovalRequestID 覆盖 skill/team_skill 两种前缀与普通 id。
func TestIsEvolutionApprovalRequestID(t *testing.T) {
	assert.True(t, IsEvolutionApprovalRequestID("skill_evolve_1"))
	assert.True(t, IsEvolutionApprovalRequestID("team_skill_evolve_1"))
	assert.False(t, IsEvolutionApprovalRequestID("skill_create_1"))
	assert.False(t, IsEvolutionApprovalRequestID(""))
}

// TestBuildSupplementContinuationQuery 覆盖有/无原始请求与 8000 字符截断。
func TestBuildSupplementContinuationQuery(t *testing.T) {
	// 无原始请求
	got := BuildSupplementContinuationQuery("  加个日志  ", "")
	assert.Contains(t, got, "加个日志")
	assert.NotContains(t, got, "原始任务请求如下")

	// 有原始请求
	got = BuildSupplementContinuationQuery("补充", "原始任务")
	assert.Contains(t, got, "原始任务请求如下")
	assert.Contains(t, got, "原始任务")

	// 超过 8000 字符按 rune 截断
	long := strings.Repeat("字", 9000)
	got = BuildSupplementContinuationQuery("x", long)
	assert.NotContains(t, got, strings.Repeat("字", 8001))
	assert.Contains(t, got, strings.Repeat("字", 8000))
}

// TestBuildQueuedChatSendMessage 覆盖 attachments 有无两条分支。
func TestBuildQueuedChatSendMessage(t *testing.T) {
	src := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatCancel,
		json.RawMessage(`{}`), schema.WithProvider("feishu"))

	// 无附件
	msg := BuildQueuedChatSendMessage(src, "补充输入", nil, "原始请求")
	assert.Equal(t, schema.MessageTypeReq, msg.Type)
	assert.Equal(t, schema.ReqMethodChatSend, msg.ReqMethod)
	assert.True(t, msg.IsStream)
	assert.Equal(t, "feishu", msg.Provider)
	assert.Contains(t, msg.ID, src.ID, "request id 应含原消息 id 作后缀")

	var params map[string]any
	require.NoError(t, json.Unmarshal(msg.Params, &params))
	assert.Equal(t, "补充输入", params["supplement_input"])
	assert.Equal(t, "原始请求", params["original_request"])
	assert.Equal(t, true, params["is_supplement"])
	_, hasAtt := params["attachments"]
	assert.False(t, hasAtt, "无附件时不应带 attachments 键")

	// 有附件
	msg = BuildQueuedChatSendMessage(src, "x", []map[string]any{{"name": "a.txt"}}, "")
	require.NoError(t, json.Unmarshal(msg.Params, &params))
	_, hasAtt = params["attachments"]
	assert.True(t, hasAtt)
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestQueueSupplementInputAndPop 覆盖入队/出队与空 sessionID 早退。
func TestQueueSupplementInputAndPop(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	mh.queueSupplementInput("", "x", nil) // 空 sessionID 不入队
	assert.Nil(t, mh.popQueuedSupplementInput(""))
	assert.Nil(t, mh.popQueuedSupplementInput("nope"))

	mh.queueSupplementInput("s1", "输入", []map[string]any{{"name": "a"}})
	payload := mh.popQueuedSupplementInput("s1")
	require.NotNil(t, payload)
	assert.Equal(t, "输入", payload["new_input"])
	_, hasAtt := payload["attachments"]
	assert.True(t, hasAtt)

	// 取出后即删除
	assert.Nil(t, mh.popQueuedSupplementInput("s1"))
}

// TestMarkAndClearPendingEvolutionApproval 覆盖标记/清除与空 sessionID。
func TestMarkAndClearPendingEvolutionApproval(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	mh.markPendingEvolutionApproval("", "skill_evolve_1") // 空 sessionID 不标记
	mh.clearPendingEvolutionApproval("")

	mh.markPendingEvolutionApproval("s1", "not_evolution") // 非 evolution id 不标记
	mh.evolutionMu.RLock()
	_, exists := mh.pendingEvolutionApproval["s1"]
	mh.evolutionMu.RUnlock()
	assert.False(t, exists)

	mh.markPendingEvolutionApproval("s1", "skill_evolve_1")
	mh.evolutionMu.RLock()
	got := mh.pendingEvolutionApproval["s1"]
	mh.evolutionMu.RUnlock()
	assert.Equal(t, "skill_evolve_1", got)

	mh.clearPendingEvolutionApproval("s1")
	mh.evolutionMu.RLock()
	_, exists = mh.pendingEvolutionApproval["s1"]
	mh.evolutionMu.RUnlock()
	assert.False(t, exists)
}

// TestFinishEvolutionApprovalIfCurrent 覆盖命中/陈旧/参数缺失三态。
func TestFinishEvolutionApprovalIfCurrent(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	// 参数缺失
	assert.Nil(t, mh.finishEvolutionApprovalIfCurrent("", "r1"))
	assert.Nil(t, mh.finishEvolutionApprovalIfCurrent("s1", ""))

	// 无 pending → 陈旧
	assert.Nil(t, mh.finishEvolutionApprovalIfCurrent("s1", "skill_evolve_1"))

	// pending 与回答不一致 → 陈旧且保留 pending
	mh.markPendingEvolutionApproval("s1", "skill_evolve_1")
	mh.markSessionEvolutionInProgress("s1")
	assert.Nil(t, mh.finishEvolutionApprovalIfCurrent("s1", "skill_evolve_2"))
	mh.evolutionMu.RLock()
	_, exists := mh.pendingEvolutionApproval["s1"]
	mh.evolutionMu.RUnlock()
	assert.True(t, exists, "陈旧回答不应清除 pending")
	assert.True(t, mh.isSessionEvolutionInProgress("s1"))

	// 命中 → 清 pending + in_progress，返回排队输入
	mh.queueSupplementInput("s1", "排队输入", nil)
	payload := mh.finishEvolutionApprovalIfCurrent("s1", "skill_evolve_1")
	require.NotNil(t, payload)
	assert.Equal(t, "排队输入", payload["new_input"])
	assert.False(t, mh.isSessionEvolutionInProgress("s1"))
	mh.evolutionMu.RLock()
	_, exists = mh.pendingEvolutionApproval["s1"]
	mh.evolutionMu.RUnlock()
	assert.False(t, exists)
}

// TestSessionEvolutionInProgress 覆盖标记/查询/清除。
func TestSessionEvolutionInProgress(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	mh.markSessionEvolutionInProgress("")
	assert.False(t, mh.isSessionEvolutionInProgress(""))
	assert.False(t, mh.isSessionEvolutionInProgress("s1"))

	mh.markSessionEvolutionInProgress("s1")
	assert.True(t, mh.isSessionEvolutionInProgress("s1"))

	mh.clearSessionEvolutionInProgress("")
	mh.clearSessionEvolutionInProgress("s1")
	assert.False(t, mh.isSessionEvolutionInProgress("s1"))
}

// TestHandleEvolutionChunk 覆盖空 payload / status 事件 / 审批事件三条路径。
func TestHandleEvolutionChunk(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	// 空 payload
	mh.handleEvolutionChunk(&schema.AgentResponseChunk{}, "s1", nil)

	// evolution_status=start
	mh.handleEvolutionChunk(&schema.AgentResponseChunk{
		RequestID: "r1",
		Payload: map[string]any{
			"event_type": string(schema.EventTypeChatEvolutionStatus),
			"status":     " START ",
		},
	}, "s1", nil)
	assert.True(t, mh.isSessionEvolutionInProgress("s1"), "status 应大小写/空格归一后命中 start")

	// evolution_status=end
	mh.handleEvolutionChunk(&schema.AgentResponseChunk{
		RequestID: "r1",
		Payload: map[string]any{
			"event_type": string(schema.EventTypeChatEvolutionStatus),
			"status":     "end",
		},
	}, "s1", nil)
	assert.False(t, mh.isSessionEvolutionInProgress("s1"))

	// 非 evolution 的 ask_user_question 不标记
	mh.handleEvolutionChunk(&schema.AgentResponseChunk{
		ChannelID: "feishu_test",
		Payload: map[string]any{
			"event_type": string(schema.EventTypeChatAskUserQuestion),
			"request_id": "plain_question",
		},
	}, "s1", nil)
	mh.evolutionMu.RLock()
	_, exists := mh.pendingEvolutionApproval["s1"]
	mh.evolutionMu.RUnlock()
	assert.False(t, exists)

	// evolution 审批 → 标记 pending
	mh.handleEvolutionChunk(&schema.AgentResponseChunk{
		ChannelID: "feishu_test",
		Payload: map[string]any{
			"event_type": string(schema.EventTypeChatAskUserQuestion),
			"request_id": "skill_evolve_9",
		},
	}, "s1", map[string]any{"k": "v"})
	mh.evolutionMu.RLock()
	got := mh.pendingEvolutionApproval["s1"]
	mh.evolutionMu.RUnlock()
	assert.Equal(t, "skill_evolve_9", got)
}

// TestBuildAutoAcceptEvolutionAnswer 覆盖自动接受回答消息的字段。
func TestBuildAutoAcceptEvolutionAnswer(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	msg := mh.buildAutoAcceptEvolutionAnswer("feishu_test", "s1", "skill_evolve_1", map[string]any{"k": "v"})

	assert.Equal(t, schema.MessageTypeReq, msg.Type)
	assert.Equal(t, schema.ReqMethodChatAnswer, msg.ReqMethod)
	assert.False(t, msg.IsStream)
	assert.Equal(t, "feishu_test", msg.ChannelID)
	assert.Equal(t, "s1", msg.SessionID)
	assert.Equal(t, "v", msg.Metadata["k"])

	var params map[string]any
	require.NoError(t, json.Unmarshal(msg.Params, &params))
	assert.Equal(t, "skill_evolve_1", params["request_id"])
	assert.Equal(t, "auto_accept", params["source"])
	assert.NotNil(t, params["answers"])
}

// TestMaybeAutoAcceptReplacedEvolutionApproval 覆盖替换/同 id/无 pending 三态。
func TestMaybeAutoAcceptReplacedEvolutionApproval(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	// 参数缺失
	mh.maybeAutoAcceptReplacedEvolutionApproval("", "skill_evolve_1", "feishu_test", nil)
	mh.maybeAutoAcceptReplacedEvolutionApproval("s1", "", "feishu_test", nil)

	// 无 pending → 不动作
	mh.maybeAutoAcceptReplacedEvolutionApproval("s1", "skill_evolve_2", "feishu_test", nil)
	assert.Zero(t, len(mh.userMessages), "无 pending 不应入队自动接受")

	// 同 id → 不动作
	mh.markPendingEvolutionApproval("s1", "skill_evolve_1")
	mh.maybeAutoAcceptReplacedEvolutionApproval("s1", "skill_evolve_1", "feishu_test", nil)
	assert.Zero(t, len(mh.userMessages))

	// 新 id 替换旧 id → 自动接受旧的在途审批
	mh.maybeAutoAcceptReplacedEvolutionApproval("s1", "skill_evolve_2", "feishu_test", map[string]any{"k": "v"})
	require.Equal(t, 1, len(mh.userMessages), "替换旧审批时应入队一条自动接受消息")
	autoAnswer := <-mh.userMessages
	assert.Equal(t, schema.ReqMethodChatAnswer, autoAnswer.ReqMethod)

	var params map[string]any
	require.NoError(t, json.Unmarshal(autoAnswer.Params, &params))
	assert.Equal(t, "skill_evolve_1", params["request_id"], "自动接受的应是旧 request_id")
}

// TestClearSessionEvolutionStates 覆盖三态一次清空。
func TestClearSessionEvolutionStates(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	mh.markPendingEvolutionApproval("s1", "skill_evolve_1")
	mh.markSessionEvolutionInProgress("s1")
	mh.queueSupplementInput("s1", "x", nil)

	mh.clearSessionEvolutionStates("s1")

	assert.False(t, mh.isSessionEvolutionInProgress("s1"))
	mh.evolutionMu.RLock()
	_, exists := mh.pendingEvolutionApproval["s1"]
	mh.evolutionMu.RUnlock()
	assert.False(t, exists)
	assert.Nil(t, mh.popQueuedSupplementInput("s1"))
}
