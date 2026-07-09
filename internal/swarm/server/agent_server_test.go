package server

import (
	"context"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/gateway_push"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// newTestAgentServer 创建测试用 AgentServer 实例。
func newTestAgentServer() *AgentServer {
	cfg, _ := config.New("")
	transport := gateway_push.NewChannelTransport()
	return NewAgentServer(cfg, transport)
}

// TestNewAgentServer 测试创建 AgentServer 后初始状态。
func TestNewAgentServer(t *testing.T) {
	s := newTestAgentServer()

	if s.ServerReady() {
		t.Error("新建 AgentServer 不应已就绪")
	}
	s.runningMu.RLock()
	running := s.running
	s.runningMu.RUnlock()
	if running {
		t.Error("新建 AgentServer 不应正在运行")
	}
}

// TestAgentServer_Start_ServerReady 测试启动后 ServerReady 为 true 且 WaitServerReady 不阻塞。
func TestAgentServer_Start_ServerReady(t *testing.T) {
	s := newTestAgentServer()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 在后台启动 AgentServer
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = s.Start(ctx)
	}()

	// 等待就绪
	readyCtx, readyCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer readyCancel()

	if !s.WaitServerReady(readyCtx) {
		t.Fatal("AgentServer 应在超时前就绪")
	}
	if !s.ServerReady() {
		t.Error("AgentServer 启动后 ServerReady 应为 true")
	}

	// 停止 AgentServer
	cancel()
	<-done
}

// TestAgentServer_WaitServerReady_Timeout 测试未启动时 WaitServerReady 在 ctx 超时后返回 false。
func TestAgentServer_WaitServerReady_Timeout(t *testing.T) {
	s := newTestAgentServer()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if s.WaitServerReady(ctx) {
		t.Error("未启动的 AgentServer 不应就绪")
	}
}

// TestAgentServer_Stop 测试启动后 Stop 不报错。
func TestAgentServer_Stop(t *testing.T) {
	s := newTestAgentServer()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 启动 AgentServer
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = s.Start(ctx)
	}()

	// 等待就绪
	readyCtx, readyCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer readyCancel()
	if !s.WaitServerReady(readyCtx) {
		t.Fatal("AgentServer 应在超时前就绪")
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

	cancel()
	<-done
}

// TestAgentServer_StreamTaskTracking 测试流式任务的注册和取消。
func TestAgentServer_StreamTaskTracking(t *testing.T) {
	s := newTestAgentServer()

	// 注册两个流式任务
	cancelled1 := false
	cancel1 := func() { cancelled1 = true }
	cancelled2 := false
	cancel2 := func() { cancelled2 = true }

	s.registerStreamTask("sess-1", cancel1)
	s.registerStreamTask("sess-2", cancel2)

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
	transport := gateway_push.NewChannelTransport()
	s := NewAgentServer(cfg, transport)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 启动 AgentServer
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = s.Start(ctx)
	}()

	// 等待就绪
	readyCtx, readyCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer readyCancel()
	if !s.WaitServerReady(readyCtx) {
		t.Fatal("AgentServer 应在超时前就绪")
	}

	// 发送信封
	envelope := e2a.NewE2AEnvelope()
	envelope.RequestID = "test-req-1"
	envelope.Channel = "test-channel"
	envelope.Method = "chat.send"

	sendCtx, sendCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer sendCancel()
	if err := transport.Send(sendCtx, envelope); err != nil {
		t.Fatalf("发送信封失败: %v", err)
	}

	// 从 RecvCh 读取响应
	recvCh, err := transport.Recv()
	if err != nil {
		t.Fatalf("获取接收通道失败: %v", err)
	}

	select {
	case resp := <-recvCh:
		if resp == nil {
			t.Error("响应不应为 nil")
		}
	case <-time.After(2 * time.Second):
		t.Error("超时：未收到响应")
	}

	cancel()
	<-done
}
