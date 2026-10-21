package plugin

import (
	"context"
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
