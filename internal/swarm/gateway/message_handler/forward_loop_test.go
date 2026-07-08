package message_handler

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestForwardLoop_消息处理 测试 forwardLoop 处理消息
func TestForwardLoop_消息处理(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 启动 forwardLoop
	go mh.forwardLoop(ctx)

	// 发送消息到 userMessages
	msg := schema.NewReqMessage("web", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"hello"}`),
		schema.WithIsStream(true),
	)

	select {
	case mh.userMessages <- msg:
	default:
		t.Fatal("userMessages channel 满了")
	}

	// 给 forwardLoop 一点时间处理
	time.Sleep(50 * time.Millisecond)
}

// TestProcessInbound_ChatSend 测试 chat.send 入站处理
func TestProcessInbound_ChatSend(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	msg := schema.NewReqMessage("web", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"hello"}`),
		schema.WithIsStream(true),
	)
	mh.processInbound(context.Background(), msg)
}

// TestProcessInbound_ChatCancel 测试 chat.cancel 入站处理
func TestProcessInbound_ChatCancel(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	msg := schema.NewReqMessage("web", "sess-1", schema.ReqMethodChatCancel,
		json.RawMessage(`{"intent":"cancel"}`),
	)
	mh.processInbound(context.Background(), msg)
}

// TestProcessInbound_ChatResume 测试 chat.resume 入站处理
func TestProcessInbound_ChatResume(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	msg := schema.NewReqMessage("web", "sess-1", schema.ReqMethodChatResume,
		json.RawMessage(`{}`),
	)
	mh.processInbound(context.Background(), msg)
}

// TestProcessInbound_ChatUserAnswer 测试 chat.user_answer 入站处理
func TestProcessInbound_ChatUserAnswer(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	msg := schema.NewReqMessage("web", "sess-1", schema.ReqMethodChatAnswer,
		json.RawMessage(`{"request_id":"req-1","answer":"yes"}`),
	)
	mh.processInbound(context.Background(), msg)
}

// TestProcessInbound_其他方法 测试其他方法入站处理
func TestProcessInbound_其他方法(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	msg := schema.NewReqMessage("web", "sess-1", schema.ReqMethodCommandAddDir,
		json.RawMessage(`{}`),
	)
	mh.processInbound(context.Background(), msg)
}

// TestMessageToE2A 测试 Message → E2A 转换
func TestMessageToE2A(t *testing.T) {
	_ = createTestMessageHandlerWithTransport() // 确保 transport 可用

	msg := schema.NewReqMessage("web", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"hello"}`),
		schema.WithSessionID("sess-1"),
	)

	envelope := e2a.MessageToE2AOrFallback(msg)
	assert.NotNil(t, envelope)
	assert.Equal(t, "sess-1", envelope.SessionID)
}

// TestOutboundLoop_退出 测试 outboundLoop 退出
func TestOutboundLoop_退出(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		mh.outboundLoop(ctx)
		close(done)
	}()

	// 取消上下文
	cancel()

	select {
	case <-done:
		// 正常退出
	case <-time.After(time.Second):
		t.Fatal("outboundLoop 未在超时内退出")
	}
}
