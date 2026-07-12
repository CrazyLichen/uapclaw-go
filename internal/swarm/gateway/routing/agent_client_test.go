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
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
)

// ──────────────────────────── AgentClient 测试 ────────────────────────────

// mockTransport 不执行 Close 的 mock 传输（用于重连测试）。
type mockTransport struct {
	recvCh chan []byte
	sendCh chan []byte
}

func (m *mockTransport) Send(_ context.Context, data []byte) error {
	m.sendCh <- data
	return nil
}

func (m *mockTransport) Recv() (<-chan []byte, error) {
	return m.recvCh, nil
}

func (m *mockTransport) Close() error {
	// no-op：不关闭底层 channel，避免 send on closed channel
	return nil
}

// newTestAgentClientWithTransport 创建测试用 AgentClient 和 ChannelTransport。
func newTestAgentClientWithTransport() (*AgentClient, *gateway_push.ChannelTransport) {
	chTransport := gateway_push.NewChannelTransport()
	agentClient := NewAgentClient(chTransport)
	return agentClient, chTransport
}

// TestNewAgentClient 创建 AgentClient 实例
func TestNewAgentClient(t *testing.T) {
	chTransport := gateway_push.NewChannelTransport()
	ac := NewAgentClient(chTransport)
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
	ac, chTransport := newTestAgentClientWithTransport()

	// 在另一个 goroutine 中模拟 AgentServer 发送 connection.ack
	go func() {
		ackFrame := transport.BuildConnectionAckFrame()
		data, _ := json.Marshal(ackFrame)
		// 短暂等待确保 Connect 已启动 receiverLoop
		time.Sleep(50 * time.Millisecond)
		chTransport.RecvCh() <- data
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
	ac, chTransport := newTestAgentClientWithTransport()

	// 先发送 connection.ack 使 AgentClient 就绪
	go func() {
		time.Sleep(50 * time.Millisecond)
		ackFrame := transport.BuildConnectionAckFrame()
		ackData, _ := json.Marshal(ackFrame)
		chTransport.RecvCh() <- ackData
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
		case reqData := <-chTransport.SendCh():
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
			chTransport.RecvCh() <- respData
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
	ac, chTransport := newTestAgentClientWithTransport()

	go func() {
		time.Sleep(50 * time.Millisecond)
		ackFrame := transport.BuildConnectionAckFrame()
		ackData, _ := json.Marshal(ackFrame)
		chTransport.RecvCh() <- ackData
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
	ac, chTransport := newTestAgentClientWithTransport()

	// 先发送 connection.ack
	go func() {
		time.Sleep(50 * time.Millisecond)
		ackFrame := transport.BuildConnectionAckFrame()
		ackData, _ := json.Marshal(ackFrame)
		chTransport.RecvCh() <- ackData
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
	chTransport.RecvCh() <- pushData

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
	ac, chTransport := newTestAgentClientWithTransport()

	go func() {
		time.Sleep(50 * time.Millisecond)
		ackFrame := transport.BuildConnectionAckFrame()
		ackData, _ := json.Marshal(ackFrame)
		chTransport.RecvCh() <- ackData
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
	ac, chTransport := newTestAgentClientWithTransport()

	go func() {
		time.Sleep(50 * time.Millisecond)
		ackFrame := transport.BuildConnectionAckFrame()
		ackData, _ := json.Marshal(ackFrame)
		chTransport.RecvCh() <- ackData
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
		case reqData := <-chTransport.SendCh():
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
				chTransport.RecvCh() <- respData
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

// TestAgentClient_SetOrUpdateServerConfig 验证 SetOrUpdateServerConfig 为 no-op 不 panic
func TestAgentClient_SetOrUpdateServerConfig(t *testing.T) {
	ac, _ := newTestAgentClientWithTransport()
	// 调用 no-op 方法，不应 panic
	ac.SetOrUpdateServerConfig(map[string]any{"key": "value"}, map[string]string{"ENV": "test"})
}

// TestAgentClient_Connect_重连先Disconnect 连续两次 Connect 应先 Disconnect 再 Connect
func TestAgentClient_Connect_重连先Disconnect(t *testing.T) {
	ac, chTransport := newTestAgentClientWithTransport()

	// 第一次 Connect：发送 connection.ack
	firstAckSent := make(chan struct{})
	go func() {
		time.Sleep(50 * time.Millisecond)
		ackFrame := transport.BuildConnectionAckFrame()
		ackData, _ := json.Marshal(ackFrame)
		chTransport.RecvCh() <- ackData
		close(firstAckSent)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()

	err := ac.Connect(ctx)
	if err != nil {
		t.Fatalf("第一次 Connect() 返回错误: %v", err)
	}

	// 等待第一次 ack 发送完成
	<-firstAckSent
	time.Sleep(100 * time.Millisecond)

	if !ac.IsConnected() {
		t.Error("第一次 Connect() 后 IsConnected() 应为 true")
	}

	// 在第二次 Connect 之前替换 transport（Disconnect 会 Close 旧的）
	// 必须在 Connect 调用 Disconnect 之后、receiverLoop 启动之前完成。
	// 为简化测试：先手动 Disconnect，替换 transport，再 Connect
	ac.Disconnect()
	time.Sleep(100 * time.Millisecond)

	newTransport := gateway_push.NewChannelTransport()
	ac.transport = newTransport

	go func() {
		time.Sleep(50 * time.Millisecond)
		ackFrame := transport.BuildConnectionAckFrame()
		ackData, _ := json.Marshal(ackFrame)
		newTransport.RecvCh() <- ackData
	}()

	// 验证 Disconnect + Connect 能正常工作
	err = ac.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect() 返回错误: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	if !ac.IsConnected() {
		t.Error("Connect() 后 IsConnected() 应为 true")
	}

	ac.Disconnect()
}

// TestAgentClient_Connect_已运行时先Disconnect 验证 Connect 检测到 running==true 时先调 Disconnect
func TestAgentClient_Connect_已运行时先Disconnect(t *testing.T) {
	ac, chTransport := newTestAgentClientWithTransport()

	// 第一次 Connect
	go func() {
		time.Sleep(50 * time.Millisecond)
		ackFrame := transport.BuildConnectionAckFrame()
		ackData, _ := json.Marshal(ackFrame)
		chTransport.RecvCh() <- ackData
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()

	err := ac.Connect(ctx)
	if err != nil {
		t.Fatalf("第一次 Connect() 返回错误: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// 验证第一次连接正常
	if !ac.IsConnected() {
		t.Fatal("第一次 Connect() 后 IsConnected() 应为 true")
	}

	// 替换 transport（Disconnect 会 Close 旧的，新 Connect 需要新 transport）
	// 用 mockTransport 替代，其 Close 不关闭底层 channel，避免 send on closed channel
	mt := &mockTransport{
		recvCh: make(chan []byte, 128),
		sendCh: make(chan []byte, 64),
	}
	ac.transport = mt

	// 为新 transport 发送 connection.ack
	go func() {
		time.Sleep(100 * time.Millisecond)
		ackFrame := transport.BuildConnectionAckFrame()
		ackData, _ := json.Marshal(ackFrame)
		mt.recvCh <- ackData
	}()

	// 第二次 Connect：应先 Disconnect 再 Connect
	err = ac.Connect(ctx)
	if err != nil {
		t.Fatalf("第二次 Connect() 返回错误: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	if !ac.IsConnected() {
		t.Error("第二次 Connect() 后 IsConnected() 应为 true")
	}

	ac.Disconnect()
}
