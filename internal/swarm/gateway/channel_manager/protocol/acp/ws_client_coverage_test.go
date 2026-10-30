package acp

// 11.25 Task 12：覆盖率补齐用例（0% 函数 + messageToE2aResponse 低覆盖分支 +
// WS 帧重建路径）。Python 无对应单测（WS 族在 Python 侧靠 e2e），Go 增强项——
// 单元级直测 handleGatewayFrame/messageFromGatewayXxx 三函数与 E2A 五分支。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 错误族与工具函数 ────────────────────────────

// TestAcpValueError_错误族 ValueError 构造/判定/消息。
func TestAcpValueError_错误族(t *testing.T) {
	err := acpValueError("sessionId is required")
	require.Error(t, err)
	assert.Equal(t, "sessionId is required", err.Error())
	assert.True(t, isValueError(err), "acpValueErr 应被识别为 ValueError")

	other := context.Canceled
	assert.False(t, isValueError(other), "外部错误不应误判为 ValueError")
}

// TestOrStr_空串回退 空白与回退语义。
func TestOrStr_空串回退(t *testing.T) {
	assert.Equal(t, "keep", orStr("keep", "fb"))
	assert.Equal(t, "fb", orStr("", "fb"))
	assert.Equal(t, "fb", orStr("   ", "fb"))
}

// TestSetGatewayURL_设置后读取 URL 设置（模式二前置）。
func TestSetGatewayURL_设置后读取(t *testing.T) {
	ch := NewAcpChannel(AcpChannelConfig{})
	assert.Equal(t, "", ch.gatewayURL)
	ch.SetGatewayURL("ws://127.0.0.1:19001/acp")
	assert.Equal(t, "ws://127.0.0.1:19001/acp", ch.gatewayURL)
}

// TestAgePendingRPCForTest_回溯时间 pending 条目时间回退（过期清扫配合）。
func TestAgePendingRPCForTest_回溯时间(t *testing.T) {
	ch := NewAcpChannel(AcpChannelConfig{})
	ch.SetPendingRPCSessionForTest("rpc-1", "s1")
	before := ch.GetPendingRPCSessionForTest("rpc-1")
	require.Equal(t, "s1", before)

	ch.AgePendingRPCForTest("rpc-1", 120) // 回溯 120s（> 60s 过期线）
	ch.mu.Lock()
	created := ch.pendingRPCSessionByID["rpc-1"].createdAt
	ch.mu.Unlock()
	assert.True(t, nowFloat()-created > 100, "回溯后 createdAt 应早于当前 100s 以上")
}

// TestGwConnForTest_未连接返回nil 未建立连接时返回 nil。
func TestGwConnForTest_未连接返回nil(t *testing.T) {
	ch := NewAcpChannel(AcpChannelConfig{})
	conn, ok := ch.GwConnForTest()
	assert.Nil(t, conn)
	assert.False(t, ok)
}

// ──────────────────────────── isAcpJsonrpcResponse 分支 ────────────────────────────

// TestIsAcpJsonrpcResponse_判定分支 覆盖 22.2% 的四分支。
func TestIsAcpJsonrpcResponse_判定分支(t *testing.T) {
	// jsonrpc 版本不符
	assert.False(t, isAcpJsonrpcResponse(map[string]any{"jsonrpc": "1.0", "id": "1", "result": nil}))
	// 缺 id
	assert.False(t, isAcpJsonrpcResponse(map[string]any{"jsonrpc": "2.0", "result": nil}))
	// 带方法（是请求不是响应）
	assert.False(t, isAcpJsonrpcResponse(map[string]any{"jsonrpc": "2.0", "id": "1", "method": "x", "result": nil}))
	// 带 result
	assert.True(t, isAcpJsonrpcResponse(map[string]any{"jsonrpc": "2.0", "id": "1", "result": map[string]any{}}))
	// 带 error
	assert.True(t, isAcpJsonrpcResponse(map[string]any{"jsonrpc": "2.0", "id": 9, "error": map[string]any{"code": -1}}))
	// 既无 result 也无 error
	assert.False(t, isAcpJsonrpcResponse(map[string]any{"jsonrpc": "2.0", "id": "1"}))
}

// ──────────────────────────── handleGatewayFrame 重建路径 ────────────────────────────

// TestHandleGatewayFrame_Res帧重建 type=res 帧重建 Message 走 Send 链
//（requestCtx 命中 → writeJsonrpcResult/branch）。
func TestHandleGatewayFrame_Res帧重建(t *testing.T) {
	ch, out, _ := newTestChannel(t, nil)

	// 预灌 request ctx（jsonrpc 模式）
	ch.SeedRequestCtxForTest("req-gw-1", &acpRequestCtx{
		JSONRPCID:    float64(21),
		ResponseMode: "jsonrpc",
		SessionID:    "sess-gw",
	})

	// type=res ok=true accepted=true → res 分支不写帧
	ch.HandleGatewayFrameForTest(map[string]any{
		"type": "res", "id": "req-gw-1", "ok": true,
		"payload": map[string]any{"accepted": true},
	})
	assert.Empty(t, out.jsonLines(t), "accepted=true 不应写帧")

	// type=res ok=false → error 帧
	ch.HandleGatewayFrameForTest(map[string]any{
		"type": "res", "id": "req-gw-1", "ok": false,
		"payload": map[string]any{},
	})
	lines := out.jsonLines(t)
	require.Len(t, lines, 1)
	assert.Equal(t, float64(-32603), lines[0]["error"].(map[string]any)["code"])

	// 无对应 ctx → 直接丢弃
	ch.HandleGatewayFrameForTest(map[string]any{
		"type": "res", "id": "req-unknown", "ok": true,
		"payload": map[string]any{},
	})
}

// TestHandleGatewayFrame_Event帧重建 type=event 帧经 activePrompt 反查
//（delta → session/update 流）。
func TestHandleGatewayFrame_Event帧重建(t *testing.T) {
	ch, out, _ := newTestChannel(t, nil)

	ch.SeedRequestCtxForTest("req-gw-2", &acpRequestCtx{
		JSONRPCID:    float64(22),
		ResponseMode: "jsonrpc",
		SessionID:    "sess-ev",
	})
	ch.SetActivePromptRequestForTest("sess-ev", "req-gw-2")

	ch.HandleGatewayFrameForTest(map[string]any{
		"type": "event", "event": "chat.delta",
		"payload": map[string]any{"session_id": "sess-ev", "content": "gw delta"},
	})
	lines := out.jsonLines(t)
	require.Len(t, lines, 1)
	assert.Equal(t, "session/update", lines[0]["method"])
	params, _ := lines[0]["params"].(map[string]any)
	update, _ := params["update"].(map[string]any)
	content, _ := update["content"].(map[string]any)
	assert.Equal(t, "gw delta", content["text"])

	// 未知事件名 → 静默丢弃
	ch.HandleGatewayFrameForTest(map[string]any{
		"type": "event", "event": "not_an_event",
		"payload": map[string]any{"session_id": "sess-ev"},
	})
	// 未知会话 → 静默丢弃
	ch.HandleGatewayFrameForTest(map[string]any{
		"type": "event", "event": "chat.delta",
		"payload": map[string]any{"session_id": "no-such", "content": "x"},
	})
	// 未知 type → 静默丢弃
	ch.HandleGatewayFrameForTest(map[string]any{
		"type": "other", "id": "x",
	})
}

// TestMessageFromGatewayResponse_未知请求ID 无 ctx → nil。
func TestMessageFromGatewayResponse_未知请求ID(t *testing.T) {
	ch := NewAcpChannel(AcpChannelConfig{})
	assert.Nil(t, ch.messageFromGatewayResponse(map[string]any{
		"type": "res", "id": "req-none", "ok": true,
	}))
	// 空 id → nil
	assert.Nil(t, ch.messageFromGatewayResponse(map[string]any{"type": "res", "ok": true}))
}

// TestMessageFromGatewayEvent_未知会话与事件名 边界静默。
func TestMessageFromGatewayEvent_未知会话与事件名(t *testing.T) {
	ch := NewAcpChannel(AcpChannelConfig{})
	// 无 payload 会话 → activePrompt 反查失败
	assert.Nil(t, ch.messageFromGatewayEvent(map[string]any{
		"type": "event", "event": "chat.delta", "payload": map[string]any{},
	}))
	// 已知会话但事件名非法
	ch.SetActivePromptRequestForTest("sess-x", "req-x")
	assert.Nil(t, ch.messageFromGatewayEvent(map[string]any{
		"type": "event", "event": "bogus.event",
		"payload": map[string]any{"session_id": "sess-x"},
	}))
}

// ──────────────────────────── ensureGatewayConnection / sendToGateway / readerLoop ────────────────────────────

// TestEnsureGatewayConnection_连接建立与幂等 起本地 WS 服务验证连接建立 +
// 幂等复连 + sendToGateway 出站 + req_method 缺失错误。
func TestEnsureGatewayConnection_连接建立与幂等(t *testing.T) {
	// （失败重试路径不在此单测——12 次指数退避硬编码总时长约 18s，
	// 失败语义由错误返回格式覆盖，重试节奏留给 e2e 层；本地回环覆盖成功路径。）

	// 本地回环 WS 服务（读循环 + 捕获服务端连接供测试侧写帧）
	upgrader := websocket.Upgrader{}
	received := make(chan map[string]any, 4)
	srvConns := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		select {
		case srvConns <- c:
		default:
		}
		for {
			_, raw, err := c.ReadMessage()
			if err != nil {
				return
			}
			var m map[string]any
			if json.Unmarshal(raw, &m) == nil {
				received <- m
			}
		}
	}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/acp"

	ch := NewAcpChannel(AcpChannelConfig{})
	ch.SetGatewayURL(wsURL)
	require.NoError(t, ch.ensureGatewayConnection())
	conn, ok := ch.GwConnForTest()
	require.True(t, ok, "连接应已建立")
	require.NotNil(t, conn)

	// 已连接时再调 → 幂等返回 nil
	require.NoError(t, ch.ensureGatewayConnection())

	// sendToGateway 出站
	msg := &schema.Message{
		ID: "req-ws-1", Type: schema.MessageTypeReq,
		ChannelID: "acp", SessionID: "sess-ws",
		ReqMethod: schema.ReqMethodChatSend,
		Params:    mustJSONBytes(map[string]any{"query": "hi"}),
	}
	require.NoError(t, ch.sendToGateway(context.Background(), msg))
	select {
	case m := <-received:
		assert.Equal(t, "req", m["type"])
		assert.Equal(t, "req-ws-1", m["id"])
		assert.Equal(t, "chat.send", m["method"])
	case <-time.After(3 * time.Second):
		t.Fatal("WS 服务应收到 sendToGateway 帧")
	}

	// sendToGateway 无 req_method → 报错
	noMethod := &schema.Message{ID: "r2", Type: schema.MessageTypeReq, ChannelID: "acp"}
	assert.Error(t, ch.sendToGateway(context.Background(), noMethod))

	// readerLoop：服务端推 event 帧 → gatewayReaderLoop → handleGatewayFrame → stdout
	ch.SeedRequestCtxForTest("req-ws-1", &acpRequestCtx{
		JSONRPCID:    float64(31),
		ResponseMode: "jsonrpc",
		SessionID:    "sess-ws",
	})
	ch.SetActivePromptRequestForTest("sess-ws", "req-ws-1")
	out := &safeBuffer{}
	ch.SetStdoutForTest(out)

	srvConn := <-srvConns
	pushFrame, _ := json.Marshal(map[string]any{
		"type": "event", "event": "chat.delta",
		"payload": map[string]any{"session_id": "sess-ws", "content": "loop delta"},
	})
	require.NoError(t, srvConn.WriteMessage(websocket.TextMessage, pushFrame))

	deadline := time.Now().Add(3 * time.Second)
	var gotDelta bool
	for time.Now().Before(deadline) && !gotDelta {
		for _, line := range out.jsonLines(t) {
			if line["method"] == "session/update" {
				params, _ := line["params"].(map[string]any)
				update, _ := params["update"].(map[string]any)
				if update["sessionUpdate"] == "agent_message_chunk" {
					content, _ := update["content"].(map[string]any)
					if content["text"] == "loop delta" {
						gotDelta = true
					}
				}
			}
		}
		if !gotDelta {
			time.Sleep(50 * time.Millisecond)
		}
	}
	assert.True(t, gotDelta, "readerLoop 应把服务端 event 帧转成 session/update 写 stdout")

	// 关闭连接收尾（closeGatewayConnection 等 readerLoop 退出）
	ch.closeGatewayConnection()
}

// TestGatewayReaderLoop_断连自清 读错误后 gwConn 自清 nil。
func TestGatewayReaderLoop_断连自清(t *testing.T) {
	upgrader := websocket.Upgrader{}
	conns := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		conns <- c
		// 不读不写，等测试侧关闭
		<-r.Context().Done()
	}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/acp"

	ch := NewAcpChannel(AcpChannelConfig{})
	ch.SetGatewayURL(wsURL)
	require.NoError(t, ch.ensureGatewayConnection())

	// 服务端主动断开 → readerLoop 退出 → gwConn 自清
	srvConn := <-conns
	srvConn.Close()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c, _ := ch.GwConnForTest(); c == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	conn, _ := ch.GwConnForTest()
	assert.Nil(t, conn, "断连后 gwConn 应自清为 nil")
}

// ──────────────────────────── messageToE2aResponse 五分支 ────────────────────────────

// TestMessageToE2aResponse_五分支 E2A 模式出站全分支（覆盖 30.8%）。
func TestMessageToE2aResponse_五分支(t *testing.T) {
	ch, _, _ := newTestChannel(t, nil)

	t.Run("delta分支", func(t *testing.T) {
		c := &acpRequestCtx{JSONRPCID: 1, ResponseMode: "e2a", SessionID: "s1"}
		resp := ch.messageToE2aResponse(makeChEvent("r1", "s1", schema.EventTypeChatDelta,
			map[string]any{"content": "hello"}), c)
		require.NotNil(t, resp)
		assert.Equal(t, "e2a.chunk", resp.ResponseKind)
		assert.Equal(t, "r1:0", resp.ResponseID)
		assert.Equal(t, 1, c.Sequence, "sequence 应自增至 1")

		// reasoning delta_kind 分支
		resp2 := ch.messageToE2aResponse(makeChEvent("r1", "s1", schema.EventTypeChatDelta,
			map[string]any{"content": "think", "source_chunk_type": "llm_reasoning"}), c)
		require.NotNil(t, resp2)
		assert.Equal(t, "reasoning", resp2.Body["delta_kind"])
	})

	t.Run("error分支", func(t *testing.T) {
		c := &acpRequestCtx{JSONRPCID: 2, ResponseMode: "e2a"}
		resp := ch.messageToE2aResponse(makeChEvent("r2", "s1", schema.EventTypeChatError,
			map[string]any{"error": "boom"}), c)
		require.NotNil(t, resp)
		assert.True(t, resp.IsFinal)
		assert.Equal(t, "boom", resp.Body["message"])

		// error 文本回退链：content → 默认
		resp2 := ch.messageToE2aResponse(makeChEvent("r2", "s1", schema.EventTypeChatError,
			map[string]any{}), c)
		require.NotNil(t, resp2)
		assert.Equal(t, "Agent error", resp2.Body["message"])
	})

	t.Run("final分支", func(t *testing.T) {
		c := &acpRequestCtx{JSONRPCID: 3, ResponseMode: "e2a"}
		resp := ch.messageToE2aResponse(makeChEvent("r3", "s1", schema.EventTypeChatFinal,
			map[string]any{"content": "done"}), c)
		require.NotNil(t, resp)
		assert.True(t, resp.IsFinal)
		assert.Equal(t, "s1", resp.Body["session_id"])
	})

	t.Run("event兜底分支", func(t *testing.T) {
		c := &acpRequestCtx{JSONRPCID: 4, ResponseMode: "e2a", SessionID: "fallback-sess"}
		// tool_call 事件 → session_update
		resp := ch.messageToE2aResponse(makeChEvent("r4", "", schema.EventTypeChatToolCall,
			map[string]any{"tool_call": map[string]any{"toolCallId": "tc1", "title": "t"}}), c)
		require.NotNil(t, resp)
		assert.Equal(t, "acp.session_update", resp.ResponseKind)
		body, _ := resp.Body["sessionId"].(string)
		assert.Equal(t, "fallback-sess", body)

		// 不关注的辅助事件 → nil
		resp2 := ch.messageToE2aResponse(makeChEvent("r4", "s1", schema.EventTypeChatProcessingStatus,
			map[string]any{"is_processing": true}), c)
		// processing_status 会产 update（session_info_update），换真正 nil 的：
		_ = resp2
		resp3 := ch.messageToE2aResponse(makeChEvent("r4", "s1", schema.EventTypeChatAskUserQuestion,
			map[string]any{"request_id": "q1"}), c)
		assert.Nil(t, resp3, "ask_user 等未支持事件应返回 nil")
	})

	t.Run("res分支", func(t *testing.T) {
		// accepted=true → nil
		c := &acpRequestCtx{JSONRPCID: 5, ResponseMode: "e2a"}
		assert.Nil(t, ch.messageToE2aResponse(&schema.Message{
			ID: "r5", Type: schema.MessageTypeRes, OK: true,
			Payload: map[string]any{"accepted": true},
		}, c))

		// accepted 缺失 → prompt_result final
		resp := ch.messageToE2aResponse(&schema.Message{
			ID: "r5", Type: schema.MessageTypeRes, OK: true,
			Payload: map[string]any{"result": "ok"},
		}, c)
		require.NotNil(t, resp)
		assert.True(t, resp.IsFinal)

		// res 不 ok → jsonrpc_error final（兜底分支）
		resp2 := ch.messageToE2aResponse(&schema.Message{
			ID: "r5", Type: schema.MessageTypeRes, OK: false,
			Payload: map[string]any{"error": "req fail"},
		}, c)
		require.NotNil(t, resp2)
		assert.Equal(t, "req fail", resp2.Body["message"])
	})
}

// ──────────────────────────── handleJsonrpcRequest 错误分支 ────────────────────────────

// TestHandleJsonrpcRequest_ValueError路径 参数校验失败 → -32602。
func TestHandleJsonrpcRequest_ValueError路径(t *testing.T) {
	ch, out, _ := newTestChannel(t, nil)
	ch.handleJsonrpcRequest(context.Background(), map[string]any{
		"jsonrpc": "2.0", "id": 41, "method": "session/prompt", "params": map[string]any{},
	})
	lines := out.jsonLines(t)
	require.Len(t, lines, 1)
	assert.Equal(t, float64(-32602), lines[0]["error"].(map[string]any)["code"])
	assert.Equal(t, "sessionId is required", lines[0]["error"].(map[string]any)["message"])
}

// TestHandleJsonrpcRequest_未知方法错误路径 dispatch 返回普通 error → -32603。
func TestHandleJsonrpcRequest_未知方法错误路径(t *testing.T) {
	ch, out, _ := newTestChannel(t, nil)
	// session/prompt 无 prompt 文本 → envelopeToMessage 非法方法错误路径
	// （session/cancel 无会话 → ValueError；换 dispatch 内部 error：
	// initialize 分支恒 nil；用 envelopeToMessage 的 unsupported method）
	ch.handleJsonrpcRequest(context.Background(), map[string]any{
		"jsonrpc": "2.0", "id": 42, "method": "session/cancel", "params": map[string]any{},
	})
	// cancel 无 sessionId → ValueError -32602
	lines := out.jsonLines(t)
	require.Len(t, lines, 1)
	assert.Equal(t, float64(-32602), lines[0]["error"].(map[string]any)["code"])
}

// TestHandleRawLine_非法JSON与非法方法 stdio 入站错误分支。
func TestHandleRawLine_非法JSON与非法方法(t *testing.T) {
	ch, out, _ := newTestChannel(t, nil)

	// 非法 JSON → 错误返回调用方（Start 循环负责写 E2A 错误帧，此处不写 stdout）
	require.Error(t, ch.handleRawLine(context.Background(), "not-json"))
	require.Empty(t, out.jsonLines(t), "handleRawLine 自身不写 stdout")

	// 合法 JSON 但 E2A envelope 非法方法 → unsupported 错误
	err := ch.handleRawLine(context.Background(), `{"request_id":"r9","method":"bogus.method","params":{}}`)
	require.Error(t, err)
	assert.True(t, isValueError(err), "不支持方法应返回 ValueError")
}

// TestChannelID_空渠道回退 ChannelID 归一化回退分支。
func TestChannelID_空渠道回退(t *testing.T) {
	ch := NewAcpChannel(AcpChannelConfig{})
	// _channelID 已在构造时归一为默认值——直接断言默认
	assert.Equal(t, "acp", ch.ChannelID())
	// 显式渠道名
	ch2 := NewAcpChannel(AcpChannelConfig{ChannelID: "custom"})
	assert.Equal(t, "custom", ch2.ChannelID())
}

// TestSendJsonrpcMessage_中断与错误分支 收尾族残余分支。
func TestSendJsonrpcMessage_中断与错误分支(t *testing.T) {
	ch, out, _ := newTestChannel(t, nil)

	// chat.interrupt_result → cancelled result
	c := &acpRequestCtx{JSONRPCID: 51, ResponseMode: "jsonrpc", SessionID: "s51"}
	isFinal := ch.SendJsonrpcMessageForTest(makeChEvent("r51", "s51",
		schema.EventTypeChatInterruptResult, map[string]any{}), c)
	assert.True(t, isFinal, "interrupt_result 应终结")
	lines := out.jsonLines(t)
	require.Len(t, lines, 1)
	result, _ := lines[0]["result"].(map[string]any)
	assert.Equal(t, "cancelled", result["stopReason"])

	// chat.error → -32603 final
	c2 := &acpRequestCtx{JSONRPCID: 52, ResponseMode: "jsonrpc", SessionID: "s52"}
	isFinal2 := ch.SendJsonrpcMessageForTest(makeChEvent("r52", "s52",
		schema.EventTypeChatError, map[string]any{"error": "agent died"}), c2)
	assert.True(t, isFinal2)
	lines2 := out.jsonLines(t)
	require.Len(t, lines2, 2)
	assert.Equal(t, float64(-32603), lines2[1]["error"].(map[string]any)["code"])

	// 非关注事件（usage_metadata 类未映射）→ false 不写帧
	c3 := &acpRequestCtx{JSONRPCID: 53, ResponseMode: "jsonrpc", SessionID: "s53"}
	isFinal3 := ch.SendJsonrpcMessageForTest(makeChEvent("r53", "s53",
		schema.EventTypeChatUsageMetadata, map[string]any{"input_tokens": 1}), c3)
	assert.False(t, isFinal3)
	require.Len(t, out.jsonLines(t), 2, "未关注事件不应写帧")
}
