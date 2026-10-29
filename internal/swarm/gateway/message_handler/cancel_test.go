package message_handler

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestCancelAgentWorkForSession_完整流程 测试完整取消流程
// S-02 修复后：注册真实 goroutine（select 等 ctx 取消退出），
// 无"预 Done"workaround——cancelStreamTask 的 Wait 依赖 streamFinalCleanup
// 的无条件 Done，本用例即死锁回归测试（若修复回退则 2 秒超时失败）。
func TestCancelAgentWorkForSession_完整流程(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	// 注册多个流式任务（真实 goroutine：cancel 信号到达后退出并 Done）
	cancelled1 := false
	cancelled2 := false
	entry1 := &streamTaskEntry{}
	ch1 := make(chan struct{})
	entry1.cancel = func() { close(ch1) }
	entry1.wg.Add(1)
	entry2 := &streamTaskEntry{}
	ch2 := make(chan struct{})
	entry2.cancel = func() { close(ch2) }
	entry2.wg.Add(1)
	mh.registerStreamTask("req-1", "sess-1", map[string]any{"key": "val"}, entry1, true, "plan")
	mh.registerStreamTask("req-2", "sess-1", nil, entry2, true, "plan")
	mh.registerStreamTask("req-3", "sess-2", nil, &streamTaskEntry{cancel: func() {}}, true, "plan")
	go func() {
		defer entry1.wg.Done()
		<-ch1
		cancelled1 = true
	}()
	go func() {
		defer entry2.wg.Done()
		<-ch2
		cancelled2 = true
	}()

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatCancel, json.RawMessage(`{}`))
	// 死锁守卫：CancelAgentWorkForSession 内部 cancelStreamTask 对每个
	// rid 先 delete 再 cancel 再 Wait——Done 由 goroutine 退出时执行，
	// 若守卫式 Done 回归则此处挂死 → goroutine + 超时断言
	done := make(chan struct{})
	go func() {
		mh.CancelAgentWorkForSession(context.Background(), msg, "sess-1", true)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("CancelAgentWorkForSession 挂死：cancelStreamTask Wait 未等到 Done（S-02 死锁回归）")
	}

	// 验证 sess-1 的任务被取消
	assert.True(t, cancelled1, "req-1 应被取消")
	assert.True(t, cancelled2, "req-2 应被取消")

	// 验证 sess-2 的任务不受影响
	mh.streamMu.RLock()
	_, exists := mh.streamTasks["req-3"]
	mh.streamMu.RUnlock()
	assert.True(t, exists, "sess-2 的任务不应被取消")
}

// TestSendInterruptResultNotification 测试发送中断结果通知
func TestSendInterruptResultNotification(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	hasActive := true
	mh.sendInterruptResultNotification("req-1", "feishu_test", "sess-1", "cancel", "任务已取消", true, hasActive)

	// 从 robotMessages 读取
	select {
	case msg := <-mh.robotMessages:
		assert.Equal(t, schema.MessageTypeEvent, msg.Type)
		assert.Equal(t, schema.EventTypeChatInterruptResult, msg.EventType)
		assert.Equal(t, "sess-1", msg.SessionID)
		assert.Equal(t, "req-1", msg.ID)
	default:
		t.Fatal("未收到 interrupt_result 通知")
	}
}

// TestSendStreamCancelledNotification 测试流式取消通知
func TestSendStreamCancelledNotification(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	mh.sendStreamCancelledNotification("req-1", "feishu_test", "sess-1")
}

// TestPublishStreamCancelledFinal 测试发布流式取消最终消息
func TestPublishStreamCancelledFinal(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	mh.publishStreamCancelledFinal("req-1", "feishu_test", "sess-1", map[string]any{"key": "val"})
}

// TestCancelStreamTask 测试取消单个流式任务
// S-02 修复后：注册真实 goroutine，Wait 依赖无条件 Done，无 workaround。
func TestCancelStreamTask(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	cancelled := false
	goroutineDone := make(chan struct{})
	entry := &streamTaskEntry{}
	entry.cancel = func() { close(goroutineDone); cancelled = true }
	entry.wg.Add(1)
	mh.registerStreamTask("req-1", "sess-1", nil, entry, true, "plan")
	go func() {
		defer entry.wg.Done()
		<-goroutineDone
	}()

	// 死锁守卫：cancelStreamTask 先 delete 再 cancel 再 Wait——Done 由
	// goroutine 退出时执行，若守卫式 Done 回归则此处挂死
	cancelDone := make(chan struct{})
	go func() {
		mh.cancelStreamTask("req-1")
		close(cancelDone)
	}()
	select {
	case <-cancelDone:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelStreamTask 挂死：Wait 未等到 Done（S-02 死锁回归）")
	}
	assert.True(t, cancelled, "cancel 函数应被调用")

	// 取消不存在的任务不应 panic
	mh.cancelStreamTask("req-unknown")
}

// TestBuildErrorOutMessage 测试构造错误响应消息
func TestBuildErrorOutMessage(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	_ = mh // 避免 unused 警告

	origMsg := &schema.Message{
		ID:        "req-1",
		Type:      schema.MessageTypeReq,
		ChannelID: "feishu_test",
		SessionID: "sess-1",
		Metadata:  map[string]any{"key": "val"},
	}

	errMsg := mh.buildErrorOutMessage(origMsg, assert.AnError)
	assert.Equal(t, "req-1", errMsg.ID)
	assert.Equal(t, schema.MessageTypeRes, errMsg.Type)
	assert.Equal(t, "feishu_test", errMsg.ChannelID)
	assert.Equal(t, "sess-1", errMsg.SessionID)
	assert.False(t, errMsg.OK)
	assert.Contains(t, errMsg.Payload["error"].(string), "assert.AnError")
}

// TestBuildToolResultMessage 测试构造工具结果消息
func TestBuildToolResultMessage(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	_ = mh

	toolInfo := map[string]any{
		"tool_name":    "read_file",
		"tool_call_id": "call-1",
		"result":       "file content",
		"status":       "cancelled",
	}

	msg := mh.buildToolResultMessage("feishu_test", "sess-1", toolInfo, map[string]any{"key": "val"})
	assert.Equal(t, schema.MessageTypeEvent, msg.Type)
	assert.Equal(t, schema.EventTypeChatToolResult, msg.EventType)
	assert.Equal(t, "feishu_test", msg.ChannelID)
	assert.Equal(t, "sess-1", msg.SessionID)
	assert.Contains(t, msg.ID, "tool_result_")

	toolResult, ok := msg.Payload["tool_result"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "read_file", toolResult["tool_name"])
	assert.Equal(t, "call-1", toolResult["tool_call_id"])
	assert.Equal(t, "cancelled", toolResult["status"])
}

// TestSendCancelledToolResults 测试发送已取消工具结果
func TestSendCancelledToolResults(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	payload := map[string]any{
		"cancelled_tools": []any{
			map[string]any{"tool_name": "read_file", "tool_call_id": "call-1", "result": "", "status": "cancelled"},
			map[string]any{"tool_name": "write_file", "tool_call_id": "call-2", "result": "", "status": "cancelled"},
		},
	}
	mh.sendCancelledToolResults("feishu_test", "sess-1", payload, nil)
}

// TestSendProcessingStatus_新签名 测试新签名的 processing_status
func TestSendProcessingStatus_新签名(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	mh.sendProcessingStatus("req-1", "sess-1", "feishu_test", true)
	mh.sendProcessingStatus("req-1", "sess-1", "feishu_test", false)
}

// TestCancelStreamTask_死锁回归 S-02 专项：注册真实 stream goroutine
// （内部 select 等 ctx 取消后退出、退出路径走 streamFinalCleanup 的
// 无条件 Done），调用 cancelStreamTask，断言 2 秒内返回。
// 回归背景：旧实现 streamFinalCleanup 用守卫式 Done（查 map 里有 entry
// 才 Done），而 cancelStreamTask 先 delete map 再 Wait——delete 后 Done
// 被跳过、Wait 永久挂死，生产路径 chat.interrupt 无响应/断连清理泄漏。
func TestCancelStreamTask_死锁回归(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	entry := &streamTaskEntry{}
	ctx, cancel := context.WithCancel(context.Background())
	entry.cancel = cancel
	entry.wg.Add(1)
	mh.registerStreamTask("deadlock-req-1", "sess-dl", nil, entry, true, "plan")

	// 真实 stream goroutine：select ctx.Done 退出，退出走 streamFinalCleanup
	go mh.processStreamCore(ctx, cancel, entry,
		&schema.Message{ID: "deadlock-req-1", ChannelID: "feishu_test", SessionID: "sess-dl"},
		&e2a.E2AEnvelope{RequestID: "deadlock-req-1", IsStream: true}, true)

	// 取消并限时等待：若守卫式 Done 回归则永久挂死
	done := make(chan struct{})
	go func() {
		mh.cancelStreamTask("deadlock-req-1")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelStreamTask 死锁回归失败：Wait 未在 2 秒内等到 Done")
	}

	// goroutine 退出后 map 全清
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mh.streamMu.RLock()
		_, exists := mh.streamTasks["deadlock-req-1"]
		mh.streamMu.RUnlock()
		if !exists {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("stream goroutine 退出后任务未清理")
}

// TestRegisterStreamTask_并发注册与清理 S-01 专项：注册（持锁写五 map）
// 与 streamFinalCleanup（持锁 delete）并发执行，race 检测下不触发
// runtime fatal（旧实现 emitsProcessingStatus/streamModes 两行锁外裸写）。
// -race -count=30 复现法对齐项目既有惯例。
func TestRegisterStreamTask_并发注册与清理(t *testing.T) {
	for i := 0; i < 30; i++ {
		mh := createTestMessageHandlerWithTransport()
		var wg sync.WaitGroup

		// 并发注册方：不同 rid 的注册流
		for j := 0; j < 8; j++ {
			rid := fmt.Sprintf("race-reg-%d", j)
			entry := &streamTaskEntry{cancel: func() {}}
			entry.wg.Add(1)
			wg.Add(1)
			go func() {
				defer wg.Done()
				mh.registerStreamTask(rid, "sess-race", nil, entry, true, "plan")
			}()
		}

		// 并发清理方：streamFinalCleanup 对同批 rid 反复清理
		for j := 0; j < 8; j++ {
			rid := fmt.Sprintf("race-reg-%d", j)
			wg.Add(1)
			go func() {
				defer wg.Done()
				mh.streamFinalCleanup(rid, "feishu_test", "sess-race", true, &streamFinalState{}, nil)
			}()
		}
		wg.Wait()
	}
}
