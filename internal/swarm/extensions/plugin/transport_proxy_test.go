package plugin

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// echoTransport 测试用 transport：Send 收到的数据原样发回 Recv
type echoTransport struct {
	recvCh chan []byte
	closed bool
}

func newEchoTransport() *echoTransport {
	return &echoTransport{recvCh: make(chan []byte, 8)}
}

func (e *echoTransport) Send(ctx context.Context, data []byte) error {
	e.recvCh <- data
	return nil
}

func (e *echoTransport) Recv() (<-chan []byte, error) { return e.recvCh, nil }

func (e *echoTransport) Close() error {
	if !e.closed {
		e.closed = true
		close(e.recvCh)
	}
	return nil
}

// TestGRPCPluginTransport_SendRecvLoop 测试本地 gRPC 上的 Send/Recv 回环
func TestGRPCPluginTransport_SendRecvLoop(t *testing.T) {
	addr, stop := startTestExtensionServer(t, &fakeExtension{}, newEchoTransport())
	defer stop()

	conn, err := grpcDialTest(addr)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	pc := NewPluginClientFromConn(conn)
	tr := NewGRPCPluginTransport(pc)

	if err := tr.Send(context.Background(), []byte(`{"ping":1}`)); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}
	recvCh, err := tr.Recv()
	if err != nil {
		t.Fatalf("Recv 失败: %v", err)
	}
	select {
	case data := <-recvCh:
		if string(data) != `{"ping":1}` {
			t.Errorf("回环数据不符: %s", data)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("等待回环数据超时")
	}
}

// TestGRPCPluginTransport_CloseIdempotent 测试 Close 幂等
func TestGRPCPluginTransport_CloseIdempotent(t *testing.T) {
	addr, stop := startTestExtensionServer(t, &fakeExtension{}, newEchoTransport())
	defer stop()

	conn, err := grpcDialTest(addr)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	tr := NewGRPCPluginTransport(NewPluginClientFromConn(conn))
	if err := tr.Close(); err != nil {
		t.Fatalf("第一次 Close 失败: %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Errorf("第二次 Close 应幂等, 实际: %v", err)
	}
}

// TestGRPCPluginTransport_RecvAfterClose 测试关闭后 Recv 返回错误
func TestGRPCPluginTransport_RecvAfterClose(t *testing.T) {
	addr, stop := startTestExtensionServer(t, &fakeExtension{}, newEchoTransport())
	defer stop()

	conn, err := grpcDialTest(addr)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	tr := NewGRPCPluginTransport(NewPluginClientFromConn(conn))
	if err := tr.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
	recvCh, err := tr.Recv()
	if err == nil {
		t.Error("关闭后 Recv 应返回错误")
		_ = recvCh
	}
}

// grpcDialTest 测试辅助：拨号本地 gRPC
func grpcDialTest(addr string) (*grpc.ClientConn, error) {
	return grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

// ──────────────────────────── 以下为覆盖率补测 ────────────────────────────

// TestGRPCPluginTransport_SendAfterClose 覆盖 Close 后 Send 返回 ErrPluginTransportClosed
func TestGRPCPluginTransport_SendAfterClose(t *testing.T) {
	addr, stop := startTestExtensionServer(t, &fakeExtension{}, newEchoTransport())
	defer stop()

	conn, err := grpcDialTest(addr)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	tr := NewGRPCPluginTransport(NewPluginClientFromConn(conn))
	if err := tr.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
	if err := tr.Send(context.Background(), []byte(`{}`)); !errors.Is(err, ErrPluginTransportClosed) {
		t.Errorf("Close 后 Send 应返回 ErrPluginTransportClosed, 实际: %v", err)
	}
}

// TestGRPCPluginTransport_Recv后Close 覆盖 Close 时 recvCh 非 nil 的关闭路径
func TestGRPCPluginTransport_Recv后Close(t *testing.T) {
	addr, stop := startTestExtensionServer(t, &fakeExtension{}, newEchoTransport())
	defer stop()

	conn, err := grpcDialTest(addr)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	tr := NewGRPCPluginTransport(NewPluginClientFromConn(conn))
	recvCh, err := tr.Recv()
	if err != nil {
		t.Fatalf("Recv 失败: %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
	// Recv 后 Close：通道应被关闭（读到零值/ok=false）
	select {
	case _, ok := <-recvCh:
		if ok {
			t.Error("Close 后通道不应再产出数据")
		}
	case <-time.After(2 * time.Second):
		// 通道关闭前缓冲里可能残留数据；这里应立即读到关闭信号
		t.Error("等待通道关闭信号超时")
	}
}

// TestGRPCPluginTransport_流断开后通道关闭 覆盖 pumpRecvLoop stream.Recv 出错分支：
// 服务端 Stop 后流断开 → goroutine 关闭 recvCh
func TestGRPCPluginTransport_流断开后通道关闭(t *testing.T) {
	addr, stop := startTestExtensionServer(t, &fakeExtension{}, newEchoTransport())
	defer stop()

	conn, err := grpcDialTest(addr)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	tr := NewGRPCPluginTransport(NewPluginClientFromConn(conn))
	recvCh, err := tr.Recv()
	if err != nil {
		t.Fatalf("Recv 失败: %v", err)
	}
	// 先发一帧证明链路通
	if err := tr.Send(context.Background(), []byte(`{"a":1}`)); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}
	select {
	case got := <-recvCh:
		if string(got) != `{"a":1}` {
			t.Errorf("回环数据不符: %s", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("等待回环数据超时")
	}

	// 停掉服务端：TransportRecv 流断开 → pumpRecvLoop 关闭 recvCh
	stop()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, ok := <-recvCh
		if !ok {
			return // 通道已关闭，符合预期
		}
	}
	t.Error("流断开后 recvCh 未关闭")
}
