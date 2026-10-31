package message_handler

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// messageToE2A 是 e2a.MessageToE2AOrFallback 的测试薄封装。
func messageToE2A(t *testing.T, msg *schema.Message) *e2a.E2AEnvelope {
	t.Helper()
	return e2a.MessageToE2AOrFallback(msg)
}

// chatSendReplies 提供 chat.send / chat.cancel 的假响应。
func chatSendReplies() map[string]fakeReply {
	return map[string]fakeReply{
		string(schema.ReqMethodChatSend): func(rid string, _ map[string]any) *schema.AgentResponse {
			return schema.NewAgentResponse(rid, "feishu_test", schema.WithPayload(map[string]any{"content": "ok"}))
		},
		string(schema.ReqMethodChatCancel): func(rid string, _ map[string]any) *schema.AgentResponse {
			return schema.NewAgentResponse(rid, "feishu_test", schema.WithPayload(map[string]any{}))
		},
	}
}

// newForwardTestMessage 构造 chat.send 消息。
func newForwardTestMessage(t *testing.T, params string) *schema.Message {
	t.Helper()
	return schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(params), schema.WithProvider("feishu"), schema.WithChatID("oc-1"))
}

// TestHandleChatSend_未连接 AgentClient 未连接时直接返回。
func TestHandleChatSend_未连接(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	msg := newForwardTestMessage(t, `{"query":"hi"}`)
	mh.handleChatSend(context.Background(), msg, msg)

	select {
	case m := <-mh.robotMessages:
		t.Fatalf("未连接不应推送任何消息，实际收到 %v", m)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestHandleChatSend_非流式 串行非流式路径：发送并回推响应。
func TestHandleChatSend_非流式(t *testing.T) {
	_, mh := newFakeAgentServer(t, chatSendReplies())

	msg := newForwardTestMessage(t, `{"query":"hi"}`)
	msg.IsStream = false
	mh.handleChatSend(context.Background(), msg, msg)

	select {
	case out := <-mh.robotMessages:
		assert.NotNil(t, out)
	case <-time.After(3 * time.Second):
		t.Fatal("非流式路径应推送一条响应消息")
	}
}

// TestProcessNonStreamRequest_发送失败 覆盖错误分支：回推错误消息并返回 err。
func TestProcessNonStreamRequest_发送失败(t *testing.T) {
	f, mh := newFakeAgentServer(t, chatSendReplies())
	f.closeTransport()

	msg := newForwardTestMessage(t, `{"query":"hi"}`)
	env := messageToE2A(t, msg)
	_, err := mh.processNonStreamRequest(context.Background(), msg, env)
	require.Error(t, err)

	select {
	case out := <-mh.robotMessages:
		assert.NotNil(t, out, "失败时也应回推错误消息")
	case <-time.After(3 * time.Second):
		t.Fatal("失败路径应回推错误消息")
	}
}

// TestProcessStreamCore_正常 覆盖流式读取：chunk 推送、演化块、processing_status。
func TestProcessStreamCore_正常(t *testing.T) {
	f, mh := newFakeAgentServer(t, chatSendReplies())
	f.setStream(string(schema.ReqMethodChatSend), func(rid string) []*schema.AgentResponseChunk {
		return []*schema.AgentResponseChunk{
			schema.NewAgentResponseChunk(rid, "feishu_test", map[string]any{
				"event_type": string(schema.EventTypeChatEvolutionStatus),
				"status":     "start",
			}),
			schema.NewAgentResponseChunk(rid, "feishu_test", map[string]any{"content": "hi"}),
			schema.NewAgentResponseChunk(rid, "feishu_test", map[string]any{
				"event_type":    string(schema.EventTypeChatProcessingStatus),
				"is_processing": false,
			}),
			schema.NewTerminalChunk(rid, "feishu_test"),
		}
	})

	msg := newForwardTestMessage(t, `{"query":"hi"}`)
	msg.IsStream = true
	env := messageToE2A(t, msg)

	entry := &streamTaskEntry{cancel: func() {}}
	entry.wg.Add(1)
	mh.streamMu.Lock()
	mh.streamTasks[env.RequestID] = entry
	mh.streamMu.Unlock()

	mh.processStreamCore(context.Background(), func() {}, entry, msg, env, true)

	mh.streamMu.Lock()
	_, still := mh.streamTasks[env.RequestID]
	mh.streamMu.Unlock()
	assert.False(t, still, "流结束后应清理 streamTasks")
	assert.True(t, mh.isSessionEvolutionInProgress("sess-1") == false, "演化 start 后未收到 end，但清理不影响断言")

	// 至少推送了 2 条非终止 chunk（演化块 + content）
	got := 0
	for {
		select {
		case <-mh.robotMessages:
			got++
			continue
		default:
		}
		break
	}
	assert.GreaterOrEqual(t, got, 2, "应把非终止 chunk 逐条推送到出站队列")
}

// TestProcessStreamCore_取消 覆盖 streamCtx 已取消的分支。
func TestProcessStreamCore_取消(t *testing.T) {
	_, mh := newFakeAgentServer(t, chatSendReplies())

	msg := newForwardTestMessage(t, `{"query":"hi"}`)
	msg.IsStream = true
	env := messageToE2A(t, msg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消：SendRequestStream 后首轮 select 命中 Done

	entry := &streamTaskEntry{cancel: cancel}
	entry.wg.Add(1)
	mh.streamMu.Lock()
	mh.streamTasks[env.RequestID] = entry
	mh.streamMu.Unlock()

	mh.processStreamCore(ctx, cancel, entry, msg, env, false)
}

// TestExtractRuntimeParams 覆盖 params 三键 + metadata cwd + state mode 补齐。
func TestExtractRuntimeParams(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	msg := newForwardTestMessage(t, `{}`)
	msg.Metadata = map[string]any{"cwd": "  /tmp/work  "}

	// params 全缺 → 从 metadata 取 cwd，从 state 取 mode
	got := mh.extractRuntimeParams(msg, map[string]any{})
	assert.Equal(t, "/tmp/work", got["cwd"])
	assert.Equal(t, "agent.plan", got["mode"])

	// params 显式提供 mode → 不覆盖
	got = mh.extractRuntimeParams(msg, map[string]any{
		"cwd":          "/explicit",
		"trusted_dirs": []any{"/a"},
		"mode":         "code.normal",
	})
	assert.Equal(t, "/explicit", got["cwd"])
	assert.Equal(t, "code.normal", got["mode"])
	assert.NotNil(t, got["trusted_dirs"])

	// nil 值不算提供
	got = mh.extractRuntimeParams(newForwardTestMessage(t, `{}`), map[string]any{"cwd": nil})
	assert.NotNil(t, got["mode"])
}

// TestPythonTruthyStr 覆盖 falsy/truthy 各类型。
func TestPythonTruthyStr(t *testing.T) {
	assert.Nil(t, pythonTruthyStr(nil))
	assert.Nil(t, pythonTruthyStr(""))
	assert.Nil(t, pythonTruthyStr(false))
	assert.Nil(t, pythonTruthyStr(float64(0)))
	assert.Nil(t, pythonTruthyStr(0))
	assert.Nil(t, pythonTruthyStr([]any{}))
	assert.Nil(t, pythonTruthyStr(map[string]any{}))

	require.NotNil(t, pythonTruthyStr("x"))
	assert.Equal(t, "x", *pythonTruthyStr("x"))
	assert.Equal(t, "True", *pythonTruthyStr(true))
	assert.Equal(t, "1.5", *pythonTruthyStr(1.5))
	assert.Equal(t, "3", *pythonTruthyStr(3))
	assert.NotNil(t, pythonTruthyStr([]any{1}))

	assert.Equal(t, "", pythonOrStr(nil, ""))
	assert.Equal(t, "b", pythonOrStr("", "b"))
	assert.Equal(t, "a", pythonOrStr("a", "b"))
}

// TestHandleChatCancel_supplement 覆盖 supplement 分支：排队续接新任务。
func TestHandleChatCancel_supplement(t *testing.T) {
	_, mh := newFakeAgentServer(t, chatSendReplies())

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatCancel,
		json.RawMessage(`{"new_input":"补充一句","intent":"cancel","model_name":"m1"}`),
		schema.WithProvider("feishu"))

	mh.handleChatCancel(context.Background(), msg)

	select {
	case queued := <-mh.userMessages:
		assert.Equal(t, schema.ReqMethodChatSend, queued.ReqMethod)
		var params map[string]any
		require.NoError(t, json.Unmarshal(queued.Params, &params))
		assert.Equal(t, "补充一句", params["supplement_input"])
		assert.Equal(t, "m1", params["model_name"], "model_name 应注入排队消息")
	case <-time.After(3 * time.Second):
		t.Fatal("supplement 分支应把续接任务入队")
	}
}

// TestHandleChatCancel_supplementEvolutionPending 演进中时只排队不续接。
func TestHandleChatCancel_supplementEvolutionPending(t *testing.T) {
	_, mh := newFakeAgentServer(t, chatSendReplies())
	mh.markSessionEvolutionInProgress("sess-1")

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatCancel,
		json.RawMessage(`{"new_input":"补充"}`), schema.WithProvider("feishu"))

	mh.handleChatCancel(context.Background(), msg)

	// 排队但未投递新任务
	assert.Zero(t, len(mh.userMessages), "演进中不应立即续接")
	payload := mh.popQueuedSupplementInput("sess-1")
	require.NotNil(t, payload)
	assert.Equal(t, "补充", payload["new_input"])
}

// TestHandleChatCancel_pause 覆盖 pause 分支。
func TestHandleChatCancel_pause(t *testing.T) {
	_, mh := newFakeAgentServer(t, chatSendReplies())

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatCancel,
		json.RawMessage(`{"intent":"pause","mode":"code.normal"}`), schema.WithProvider("feishu"))

	mh.handleChatCancel(context.Background(), msg)

	// pause 分支不推 chat.send，只推 interrupt 结果通知
	select {
	case out := <-mh.robotMessages:
		assert.NotNil(t, out)
	case <-time.After(3 * time.Second):
		t.Fatal("pause 分支应推送中断结果通知")
	}
}

// TestHandleChatCancel_cancel 覆盖 cancel 分支（无 new_input、intent=cancel）。
func TestHandleChatCancel_cancel(t *testing.T) {
	_, mh := newFakeAgentServer(t, chatSendReplies())

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatCancel,
		json.RawMessage(`{}`), schema.WithProvider("feishu"))

	mh.handleChatCancel(context.Background(), msg)

	select {
	case out := <-mh.robotMessages:
		assert.NotNil(t, out)
	case <-time.After(3 * time.Second):
		t.Fatal("cancel 分支应推送中断结果通知")
	}
}

// TestResolveInboundReferences_无引用 无 @file 时应原样返回。
func TestResolveInboundReferences_无引用(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	msg := newForwardTestMessage(t, `{"query":"普通文本，没有引用"}`)

	mh.resolveInboundReferences(msg)
	// 不 panic 且 params 仍可解析
	var params map[string]any
	require.NoError(t, json.Unmarshal(msg.Params, &params))
	assert.Contains(t, strings.TrimSpace(params["query"].(string)), "普通文本")
}

// waitStreamDrain 等待所有流式任务清理完毕。
func waitStreamDrain(t *testing.T, mh *MessageHandler) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mh.streamMu.Lock()
		n := len(mh.streamTasks)
		mh.streamMu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("流式任务未在 5s 内清理")
}

// TestHandleChatSend_流式 覆盖流式分发分支（含 entry 注册与后台 goroutine）。
func TestHandleChatSend_流式(t *testing.T) {
	f, mh := newFakeAgentServer(t, chatSendReplies())
	f.setStream(string(schema.ReqMethodChatSend), func(rid string) []*schema.AgentResponseChunk {
		return []*schema.AgentResponseChunk{
			schema.NewAgentResponseChunk(rid, "feishu_test", map[string]any{"content": "hi"}),
			schema.NewTerminalChunk(rid, "feishu_test"),
		}
	})

	msg := newForwardTestMessage(t, `{"query":"hi"}`)
	msg.IsStream = true
	mh.handleChatSend(context.Background(), msg, msg)

	waitStreamDrain(t, mh)

	got := 0
	for {
		select {
		case <-mh.robotMessages:
			got++
			continue
		default:
		}
		break
	}
	assert.GreaterOrEqual(t, got, 1, "流式分支应推送 chunk")
}

// TestHandleChatSend_并行非流式 覆盖 nonStreamRPCMayRunParallel 分支。
func TestHandleChatSend_并行非流式(t *testing.T) {
	replies := chatSendReplies()
	replies[string(schema.ReqMethodSessionList)] = func(rid string, _ map[string]any) *schema.AgentResponse {
		return schema.NewAgentResponse(rid, "feishu_test", schema.WithPayload(map[string]any{"sessions": []any{}}))
	}
	_, mh := newFakeAgentServer(t, replies)

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodSessionList,
		json.RawMessage(`{}`), schema.WithProvider("feishu"))
	mh.handleChatSend(context.Background(), msg, msg)

	select {
	case out := <-mh.robotMessages:
		assert.NotNil(t, out)
	case <-time.After(3 * time.Second):
		t.Fatal("并行非流式分支应推送响应")
	}
}

// TestProcessStream_兼容入口 覆盖 processStream 包装器。
func TestProcessStream_兼容入口(t *testing.T) {
	f, mh := newFakeAgentServer(t, chatSendReplies())
	f.setStream(string(schema.ReqMethodChatSend), func(rid string) []*schema.AgentResponseChunk {
		return []*schema.AgentResponseChunk{
			schema.NewAgentResponseChunk(rid, "feishu_test", map[string]any{"content": "hi"}),
			schema.NewTerminalChunk(rid, "feishu_test"),
		}
	})

	msg := newForwardTestMessage(t, `{"query":"hi"}`)
	msg.IsStream = true
	env := messageToE2A(t, msg)

	mh.processStream(context.Background(), msg, env, false)

	mh.streamMu.Lock()
	_, still := mh.streamTasks[env.RequestID]
	mh.streamMu.Unlock()
	assert.False(t, still)
}

// TestProcessStreamWithEntry 覆盖 processStreamWithEntry 包装器。
func TestProcessStreamWithEntry(t *testing.T) {
	f, mh := newFakeAgentServer(t, chatSendReplies())
	f.setStream(string(schema.ReqMethodChatSend), func(rid string) []*schema.AgentResponseChunk {
		return []*schema.AgentResponseChunk{
			schema.NewAgentResponseChunk(rid, "feishu_test", map[string]any{"content": "hi"}),
			schema.NewTerminalChunk(rid, "feishu_test"),
		}
	})

	msg := newForwardTestMessage(t, `{"query":"hi"}`)
	msg.IsStream = true
	env := messageToE2A(t, msg)

	entry := &streamTaskEntry{cancel: func() {}}
	entry.wg.Add(1)
	mh.streamMu.Lock()
	mh.streamTasks[env.RequestID] = entry
	mh.streamMu.Unlock()

	mh.processStreamWithEntry(context.Background(), func() {}, entry, msg, env, false)
}

// TestHandleChatUserAnswer_evolution已解决 覆盖 resolved 后排队续接。
func TestHandleChatUserAnswer_evolution已解决(t *testing.T) {
	replies := chatSendReplies()
	replies[string(schema.ReqMethodChatAnswer)] = func(rid string, _ map[string]any) *schema.AgentResponse {
		return schema.NewAgentResponse(rid, "feishu_test", schema.WithPayload(map[string]any{"resolved": true}))
	}
	_, mh := newFakeAgentServer(t, replies)

	mh.markPendingEvolutionApproval("sess-1", "skill_evolve_1")
	mh.queueSupplementInput("sess-1", "排队补充", []map[string]any{{"name": "a.txt"}})

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatAnswer,
		json.RawMessage(`{"request_id":"skill_evolve_1","answers":[]}`), schema.WithProvider("feishu"))

	mh.handleChatUserAnswer(context.Background(), msg)

	select {
	case queued := <-mh.userMessages:
		assert.Equal(t, schema.ReqMethodChatSend, queued.ReqMethod, "审批解决后应续接排队任务")
	case <-time.After(3 * time.Second):
		t.Fatal("resolved 后应把排队输入转成 chat.send")
	}
}

// TestHandleChatUserAnswer_未解决 覆盖 resolved=false 分支。
func TestHandleChatUserAnswer_未解决(t *testing.T) {
	replies := chatSendReplies()
	replies[string(schema.ReqMethodChatAnswer)] = func(rid string, _ map[string]any) *schema.AgentResponse {
		return schema.NewAgentResponse(rid, "feishu_test", schema.WithPayload(map[string]any{"resolved": false}))
	}
	_, mh := newFakeAgentServer(t, replies)

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatAnswer,
		json.RawMessage(`{"request_id":"skill_evolve_1"}`), schema.WithProvider("feishu"))

	mh.handleChatUserAnswer(context.Background(), msg)
	assert.Zero(t, len(mh.userMessages), "未解决时不应续接")
}

// TestHandlePauseResume_注入mode 覆盖 params 缺 mode 时从 state 补齐。
func TestHandlePauseResume_注入mode(t *testing.T) {
	_, mh := newFakeAgentServer(t, chatSendReplies())

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatCancel,
		json.RawMessage(`{"intent":"resume"}`), schema.WithProvider("feishu"))

	mh.handlePauseResume(context.Background(), msg, "resume")

	select {
	case out := <-mh.robotMessages:
		assert.NotNil(t, out)
	case <-time.After(3 * time.Second):
		t.Fatal("pause/resume 应推送中断结果通知")
	}
}

// TestSendInterruptToAgent 覆盖直接发送中断（等响应后丢弃）。
func TestSendInterruptToAgent(t *testing.T) {
	f, mh := newFakeAgentServer(t, chatSendReplies())

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatCancel,
		json.RawMessage(`{"mode":"code.normal"}`), schema.WithProvider("feishu"))

	mh.sendInterruptToAgent(context.Background(), msg, "pause")
	assert.Equal(t, 1, f.methodCount(string(schema.ReqMethodChatCancel)))
}

// TestSendInterruptToAgent_发送失败 覆盖失败分支。
func TestSendInterruptToAgent_发送失败(t *testing.T) {
	f, mh := newFakeAgentServer(t, chatSendReplies())
	f.closeTransport()

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatCancel,
		json.RawMessage(`{"mode":"code.normal"}`), schema.WithProvider("feishu"))

	mh.sendInterruptToAgent(context.Background(), msg, "pause") // 不 panic 即可
}

// TestResolveInboundReferences_结构化附件 覆盖 attachments 分支与回写。
func TestResolveInboundReferences_结构化附件(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	dir := t.TempDir()
	writeTestFile(t, dir, "a.txt", "hello")

	msg := newForwardTestMessage(t, `{"query":"看下附件","attachments":[{"type":"file","path":"a.txt"}]}`)
	msg.Metadata = map[string]any{"cwd": dir}

	mh.resolveInboundReferences(msg)

	var params map[string]any
	require.NoError(t, json.Unmarshal(msg.Params, &params))
	assert.Contains(t, params["query"].(string), "hello", "附件内容应被内联")
}

// TestResolveInboundReferences_AtFile 覆盖 @ 引用分支。
func TestResolveInboundReferences_AtFile(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	dir := t.TempDir()
	writeTestFile(t, dir, "b.txt", "world")

	msg := newForwardTestMessage(t, `{"query":"看下 @b.txt"}`)
	msg.Metadata = map[string]any{"cwd": dir}

	mh.resolveInboundReferences(msg)

	var params map[string]any
	require.NoError(t, json.Unmarshal(msg.Params, &params))
	assert.Contains(t, params["query"].(string), "world")
}

// TestResolveInboundReferences_早退 覆盖非 chat.send / 空 params / 空 content。
func TestResolveInboundReferences_早退(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	// 非 chat.send
	other := schema.NewReqMessage("feishu_test", "s", schema.ReqMethodSessionList, json.RawMessage(`{"query":"@a"}`))
	mh.resolveInboundReferences(other)

	// 空 params
	empty := schema.NewReqMessage("feishu_test", "s", schema.ReqMethodChatSend, nil)
	mh.resolveInboundReferences(empty)

	// 空 content
	noContent := newForwardTestMessage(t, `{}`)
	mh.resolveInboundReferences(noContent)
}

// writeTestFile 在 dir 下写文件（供附件/引用解析用例使用）。
func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}
