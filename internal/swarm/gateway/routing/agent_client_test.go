package routing

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/gateway_push"
)

// ──────────────────────────── AgentClient 测试 ────────────────────────────

// newTestAgentClientWithTransport 创建测试用 AgentClient 和 ChannelTransport。
func newTestAgentClientWithTransport() (*AgentClient, *gateway_push.ChannelTransport) {
	transport := gateway_push.NewChannelTransport()
	agentClient := NewAgentClient(transport)
	return agentClient, transport
}

// TestNewAgentClient 创建 AgentClient 实例
func TestNewAgentClient(t *testing.T) {
	transport := gateway_push.NewChannelTransport()
	ac := NewAgentClient(transport)
	if ac == nil {
		t.Fatal("NewAgentClient() 返回 nil，期望非 nil")
	}
	if ac.ServerReady() {
		t.Error("ServerReady() 返回 true，期望 false")
	}
}

// TestAgentClient_ServerReady_初始为false 初始状态 serverReady 为 false
func TestAgentClient_ServerReady_初始为false(t *testing.T) {
	ac, _ := newTestAgentClientWithTransport()
	if ac.ServerReady() {
		t.Error("ServerReady() 初始应为 false")
	}
}

// TestAgentClient_Connect_收到ConnectionAck 收到 connection.ack 后 ServerReady 为 true
func TestAgentClient_Connect_收到ConnectionAck(t *testing.T) {
	ac, transport := newTestAgentClientWithTransport()

	// 在另一个 goroutine 中模拟 AgentServer 发送 connection.ack
	go func() {
		ackFrame := gateway_push.BuildConnectionAckFrame()
		data, _ := json.Marshal(ackFrame)
		// 短暂等待确保 Connect 已启动 receiverLoop
		time.Sleep(50 * time.Millisecond)
		transport.RecvCh() <- data
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := ac.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect() 返回错误: %v", err)
	}

	// 等待 serverReady 变为 true
	time.Sleep(100 * time.Millisecond)
	if !ac.ServerReady() {
		t.Error("Connect() 后 ServerReady() 应为 true")
	}

	ac.Disconnect()
}

// TestAgentClient_Connect_超时不报错 5s 内无 connection.ack，Connect 仅 warn 不报错
func TestAgentClient_Connect_超时不报错(t *testing.T) {
	ac, _ := newTestAgentClientWithTransport()

	// 不发送任何消息，让 connection.ack 超时
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()

	err := ac.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect() 超时应仅 warn 不报错，但返回: %v", err)
	}

	// 超时后 serverReady 仍为 false
	if ac.ServerReady() {
		t.Error("超时后 ServerReady() 应为 false")
	}

	ac.Disconnect()
}

// TestAgentClient_SendRequest_非流式 round-trip 非流式请求
func TestAgentClient_SendRequest_非流式(t *testing.T) {
	ac, transport := newTestAgentClientWithTransport()

	// 先发送 connection.ack 使 AgentClient 就绪
	go func() {
		time.Sleep(50 * time.Millisecond)
		ackFrame := gateway_push.BuildConnectionAckFrame()
		ackData, _ := json.Marshal(ackFrame)
		transport.RecvCh() <- ackData
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := ac.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect() 返回错误: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// 模拟 AgentServer 处理请求并返回响应
	go func() {
		// 从 sendCh 读取请求
		select {
		case reqData := <-transport.SendCh():
			var reqMap map[string]any
			if err := json.Unmarshal(reqData, &reqMap); err != nil {
				t.Logf("AgentServer 解码请求失败: %v", err)
				return
			}
			reqID, _ := reqMap["request_id"].(string)

			// 构建成功响应
			resp := &schema.AgentResponse{
				RequestID: reqID,
				ChannelID: "test-channel",
				OK:        true,
				Payload:   map[string]any{"content": "hello"},
			}
			wire := e2a.EncodeAgentResponseForWire(resp, reqID, 0)
			respData, _ := json.Marshal(wire)
			transport.RecvCh() <- respData
		case <-time.After(3 * time.Second):
			t.Log("AgentServer 等待请求超时")
		}
	}()

	// 构造请求信封
	envelope := e2a.E2AFromAgentFields("req-1",
		e2a.WithFieldChannelID("test-channel"),
		e2a.WithFieldSessionID("sess-1"),
		e2a.WithFieldParams(map[string]any{"message": "你好"}),
	)

	resp, err := ac.SendRequest(ctx, envelope)
	if err != nil {
		t.Fatalf("SendRequest() 返回错误: %v", err)
	}
	if resp.RequestID != "req-1" {
		t.Errorf("resp.RequestID=%s，期望 req-1", resp.RequestID)
	}
	if !resp.OK {
		t.Error("resp.OK 应为 true")
	}

	ac.Disconnect()
}

// TestAgentClient_SendRequest_重复requestID 同一 request_id 重复发送应报错
func TestAgentClient_SendRequest_重复requestID(t *testing.T) {
	ac, transport := newTestAgentClientWithTransport()

	go func() {
		time.Sleep(50 * time.Millisecond)
		ackFrame := gateway_push.BuildConnectionAckFrame()
		ackData, _ := json.Marshal(ackFrame)
		transport.RecvCh() <- ackData
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := ac.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect() 返回错误: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// 手动注册一个同 rid 的队列，模拟 in-flight 请求
	rid := "dup-req"
	ac.messageQueuesMu.Lock()
	ac.messageQueues[rid] = make(chan map[string]any, 1)
	ac.messageQueuesMu.Unlock()

	envelope := e2a.E2AFromAgentFields(rid,
		e2a.WithFieldChannelID("ch"),
		e2a.WithFieldSessionID("sess"),
	)

	_, err = ac.SendRequest(ctx, envelope)
	if err == nil {
		t.Error("重复 request_id 应返回错误")
	}

	// 清理
	ac.messageQueuesMu.Lock()
	delete(ac.messageQueues, rid)
	ac.messageQueuesMu.Unlock()

	ac.Disconnect()
}

// TestAgentClient_SetServerPushHandler server_push 消息通过 recvCh 到达回调
func TestAgentClient_SetServerPushHandler(t *testing.T) {
	ac, transport := newTestAgentClientWithTransport()

	// 先发送 connection.ack
	go func() {
		time.Sleep(50 * time.Millisecond)
		ackFrame := gateway_push.BuildConnectionAckFrame()
		ackData, _ := json.Marshal(ackFrame)
		transport.RecvCh() <- ackData
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := ac.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect() 返回错误: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// 注册 server_push handler
	var receivedMu sync.Mutex
	var receivedMsg map[string]any
	ac.SetServerPushHandler(func(msg map[string]any) {
		receivedMu.Lock()
		receivedMsg = msg
		receivedMu.Unlock()
	})

	// 模拟 AgentServer 发送 server_push 消息
	pushMsg := map[string]any{
		"request_id": "push-1",
		"metadata": map[string]any{
			e2a.E2AWireServerPushKey: true,
		},
		"body": map[string]any{"event": "status_update"},
	}
	pushData, _ := json.Marshal(pushMsg)
	transport.RecvCh() <- pushData

	// 等待回调触发
	time.Sleep(200 * time.Millisecond)

	receivedMu.Lock()
	if receivedMsg == nil {
		t.Error("server_push handler 未被调用")
	} else {
		body, _ := receivedMsg["body"].(map[string]any)
		if body == nil {
			t.Error("server_push 消息缺少 body")
		}
	}
	receivedMu.Unlock()

	ac.Disconnect()
}

// TestAgentClient_Disconnect 验证 receiverLoop 停止
func TestAgentClient_Disconnect(t *testing.T) {
	ac, transport := newTestAgentClientWithTransport()

	go func() {
		time.Sleep(50 * time.Millisecond)
		ackFrame := gateway_push.BuildConnectionAckFrame()
		ackData, _ := json.Marshal(ackFrame)
		transport.RecvCh() <- ackData
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := ac.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect() 返回错误: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	if !ac.ServerReady() {
		t.Error("Connect() 后 ServerReady() 应为 true")
	}

	// 断开连接
	ac.Disconnect()

	// 验证 running 为 false
	ac.runningMu.RLock()
	running := ac.running
	ac.runningMu.RUnlock()
	if running {
		t.Error("Disconnect() 后 running 应为 false")
	}
}

// TestAgentClient_SendRequestStream_流式 round-trip 流式请求
func TestAgentClient_SendRequestStream_流式(t *testing.T) {
	ac, transport := newTestAgentClientWithTransport()

	go func() {
		time.Sleep(50 * time.Millisecond)
		ackFrame := gateway_push.BuildConnectionAckFrame()
		ackData, _ := json.Marshal(ackFrame)
		transport.RecvCh() <- ackData
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := ac.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect() 返回错误: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// 模拟 AgentServer 处理流式请求
	go func() {
		select {
		case reqData := <-transport.SendCh():
			var reqMap map[string]any
			if err := json.Unmarshal(reqData, &reqMap); err != nil {
				t.Logf("AgentServer 解码请求失败: %v", err)
				return
			}
			reqID, _ := reqMap["request_id"].(string)

			// 发送 3 个 chunk
			for i := 0; i < 3; i++ {
				chunk := schema.NewAgentResponseChunk(reqID, "test-channel", map[string]any{
					"content":    "chunk-{i}",
					"event_type": "chat.delta",
				})
				if i == 2 {
					chunk.IsComplete = true
				}
				wire := e2a.EncodeAgentChunkForWire(chunk, reqID, i, true)
				respData, _ := json.Marshal(wire)
				transport.RecvCh() <- respData
			}
		case <-time.After(3 * time.Second):
			t.Log("AgentServer 等待请求超时")
		}
	}()

	envelope := e2a.E2AFromAgentFields("stream-1",
		e2a.WithFieldChannelID("test-channel"),
		e2a.WithFieldSessionID("sess-1"),
		e2a.WithFieldIsStream(true),
		e2a.WithFieldParams(map[string]any{"message": "流式测试"}),
	)

	chunkCh, err := ac.SendRequestStream(ctx, envelope)
	if err != nil {
		t.Fatalf("SendRequestStream() 返回错误: %v", err)
	}

	chunkCount := 0
	var lastChunk *schema.AgentResponseChunk
	for chunk := range chunkCh {
		chunkCount++
		lastChunk = chunk
	}

	if chunkCount != 3 {
		t.Errorf("收到 %d 个 chunk，期望 3", chunkCount)
	}
	if lastChunk == nil || !lastChunk.IsComplete {
		t.Error("最后一个 chunk 的 IsComplete 应为 true")
	}

	ac.Disconnect()
}
