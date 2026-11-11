package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// newTestAgentServer 创建测试用 AgentServer 实例。
func newTestAgentServer() *AgentServer {
	cfg, _ := config.New("")
	transport := transport.NewChannelTransport()
	return NewAgentServer(cfg, transport)
}

// TestNewAgentServer 测试创建 AgentServer 后初始状态。
func TestNewAgentServer(t *testing.T) {
	s := newTestAgentServer()

	s.runningMu.RLock()
	running := s.running
	s.runningMu.RUnlock()
	if running {
		t.Error("新建 AgentServer 不应正在运行")
	}
}

// TestAgentServer_Start_发送ConnectionAck 测试启动后 recvCh 收到 connection.ack 事件帧 JSON。
func TestAgentServer_Start_发送ConnectionAck(t *testing.T) {
	cfg, _ := config.New("")
	transport := transport.NewChannelTransport()
	s := NewAgentServer(cfg, transport)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start() 现在是非阻塞的
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}

	// 从 RecvCh 读取 connection.ack
	recvCh, err := transport.Recv()
	if err != nil {
		t.Fatalf("获取接收通道失败: %v", err)
	}

	select {
	case data := <-recvCh:
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("connection.ack JSON 解码失败: %v", err)
		}
		if m["type"] != "event" {
			t.Errorf("type = %v, 期望 event", m["type"])
		}
		if m["event"] != "connection.ack" {
			t.Errorf("event = %v, 期望 connection.ack", m["event"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("超时：未收到 connection.ack")
	}

	// 停止 AgentServer
	if err := s.Stop(); err != nil {
		t.Errorf("Stop 失败: %v", err)
	}
}

// TestAgentServer_Stop 测试启动后 Stop 不报错。
func TestAgentServer_Stop(t *testing.T) {
	s := newTestAgentServer()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start() 非阻塞
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}

	// 等待 connection.ack 确认启动完成
	recvCh, err := s.transport.Recv()
	if err != nil {
		t.Fatalf("获取接收通道失败: %v", err)
	}
	select {
	case <-recvCh:
		// 收到 connection.ack，启动完成
	case <-time.After(2 * time.Second):
		t.Fatal("超时：未收到 connection.ack")
	}

	// 停止
	if err := s.Stop(); err != nil {
		t.Errorf("Stop 不应返回错误，得到: %v", err)
	}

	s.runningMu.RLock()
	running := s.running
	s.runningMu.RUnlock()
	if running {
		t.Error("停止后 running 应为 false")
	}
}

// TestAgentServer_StreamTaskTracking 测试流式任务的注册和取消。
func TestAgentServer_StreamTaskTracking(t *testing.T) {
	s := newTestAgentServer()

	// 注册两个流式任务
	cancelled1 := false
	cancel1 := func() { cancelled1 = true }
	cancelled2 := false
	cancel2 := func() { cancelled2 = true }

	s.registerStreamTask("sess-1", "req-1", cancel1)
	s.registerStreamTask("sess-2", "req-2", cancel2)

	// 取消单个任务
	s.cancelStreamTask("sess-1")
	if !cancelled1 {
		t.Error("sess-1 的 cancel 函数应被调用")
	}
	if cancelled2 {
		t.Error("sess-2 的 cancel 函数不应被调用")
	}

	// 验证 sess-1 已从 map 中移除
	s.sessionStreamTasksMu.RLock()
	_, exists := s.sessionStreamTasks["sess-1"]
	s.sessionStreamTasksMu.RUnlock()
	if exists {
		t.Error("sess-1 应从 sessionStreamTasks 中移除")
	}

	// cancelAllStreamTasks 取消剩余任务
	s.cancelAllStreamTasks()
	if !cancelled2 {
		t.Error("sess-2 的 cancel 函数应被调用")
	}

	// 验证 map 已清空
	s.sessionStreamTasksMu.RLock()
	count := len(s.sessionStreamTasks)
	s.sessionStreamTasksMu.RUnlock()
	if count != 0 {
		t.Errorf("cancelAllStreamTasks 后 map 应为空，实际大小: %d", count)
	}
}

// TestAgentServer_ConsumeEnvelope 测试消费循环能处理信封并写入响应。
func TestAgentServer_ConsumeEnvelope(t *testing.T) {
	cfg, _ := config.New("")
	transport := transport.NewChannelTransport()
	s := NewAgentServer(cfg, transport)
	// 注入 mock Agent 工厂，避免 createAgent → CreateInstance → LLM nil panic
	s.SetAgentFactoryForTest(func(config map[string]any, mode, subMode string, _ map[string]any) (*runtime.UapClawSwarm, error) {
		return runtime.NewUapClaw(), nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start() 非阻塞
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}

	// 等待 connection.ack 确认启动完成
	recvCh, err := transport.Recv()
	if err != nil {
		t.Fatalf("获取接收通道失败: %v", err)
	}
	select {
	case <-recvCh:
		// 收到 connection.ack，启动完成
	case <-time.After(2 * time.Second):
		t.Fatal("超时：未收到 connection.ack")
	}

	// 发送信封（JSON 字节）
	envelope := e2a.NewE2AEnvelope()
	envelope.RequestID = "test-req-1"
	envelope.Channel = "test-channel"
	envelope.Method = "chat.send"
	envelopeData, err := json.Marshal(envelope.ToMap())
	if err != nil {
		t.Fatalf("信封 JSON 编码失败: %v", err)
	}

	sendCtx, sendCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer sendCancel()
	if err := transport.Send(sendCtx, envelopeData); err != nil {
		t.Fatalf("发送信封失败: %v", err)
	}

	// 从 RecvCh 读取响应
	select {
	case data := <-recvCh:
		if data == nil {
			t.Error("响应不应为 nil")
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Errorf("响应 JSON 解码失败: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("超时：未收到响应")
	}

	// 停止 AgentServer
	if err := s.Stop(); err != nil {
		t.Errorf("Stop 失败: %v", err)
	}
}

// ──────────────────────────── 访问器与写出通道 ────────────────────────────

// fakeSendTransport 非 ChannelTransport 的传输桩（走 Send 路径）。
type fakeSendTransport struct {
	// sent 发送次数
	sent int
}

// Send 记录发送次数
func (f *fakeSendTransport) Send(_ context.Context, _ []byte) error {
	f.sent++
	return nil
}

// Recv 返回空接收通道
func (f *fakeSendTransport) Recv() (<-chan []byte, error) {
	return make(chan []byte), nil
}

// Close 无操作
func (f *fakeSendTransport) Close() error { return nil }

// TestAgentServer_PlanControllerAccessor 服务端持有的计划模式控制器/managers 可被取用。
func TestAgentServer_PlanControllerAccessor(t *testing.T) {
	s := newTestAgentServer()
	if s.PlanController() == nil {
		t.Fatal("PlanController() 不应返回 nil")
	}
	if s.PlanController() != s.planController {
		t.Fatal("PlanController() 应返回服务端持有的同一实例")
	}
	if s.AgentManager() == nil {
		t.Fatal("AgentManager() 不应返回 nil")
	}
	if s.AgentManager() != s.agentManager {
		t.Fatal("AgentManager() 应返回服务端持有的同一实例")
	}
}

// TestSendToGateway_无transport 无传输时不 panic 且返回 false。
func TestSendToGateway_无transport(t *testing.T) {
	s := &AgentServer{}
	if s.sendToGateway([]byte(`{}`)) {
		t.Fatal("无 transport 时发送应返回 false")
	}
}

// TestSendToGateway_非通道传输走Send 非 ChannelTransport 走 Send 接口并返回 true。
func TestSendToGateway_非通道传输走Send(t *testing.T) {
	ft := &fakeSendTransport{}
	s := &AgentServer{transport: ft}
	if !s.sendToGateway([]byte(`{}`)) {
		t.Fatal("Send 成功时应返回 true")
	}
	if ft.sent != 1 {
		t.Fatalf("Send 调用次数 = %d，want 1", ft.sent)
	}
}
