package plugin

import (
	"context"
	"errors"
	"fmt"
	"sync"
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

// slowFeedTransport 测试用 transport：按间隔持续产出帧（模拟高频收帧），
// 用于制造 pump 发送与 Close 并发的竞态窗口
type slowFeedTransport struct {
	recvCh   chan []byte
	interval time.Duration
	stop     chan struct{}
	stopOnce sync.Once
}

func newSlowFeedTransport(interval time.Duration) *slowFeedTransport {
	s := &slowFeedTransport{
		recvCh:   make(chan []byte, 8),
		interval: interval,
		stop:     make(chan struct{}),
	}
	go s.feed()
	return s
}

func (s *slowFeedTransport) feed() {
	i := 0
	for {
		select {
		case <-s.stop:
			return
		case <-time.After(s.interval):
			s.recvCh <- []byte(fmt.Sprintf(`{"n":%d}`, i))
			i++
		}
	}
}

func (s *slowFeedTransport) Send(ctx context.Context, data []byte) error { return nil }

func (s *slowFeedTransport) Recv() (<-chan []byte, error) { return s.recvCh, nil }

func (s *slowFeedTransport) Close() error {
	s.stopOnce.Do(func() { close(s.stop) })
	return nil
}

// TestGRPCPluginTransport_高频收帧时Close无Panic S-01 回归测试：
// pump 高频收帧（持续向 recvCh 发送）与 Close 并发。
// 旧实现 Close 锁外 close(recvCh) 与 pump 锁外 send 竞态 →
// send on closed channel panic；新实现 Close 只取消 context，
// 通道由 pump 的 defer 统一收口。本测试在旧实现下高概率 panic。
func TestGRPCPluginTransport_高频收帧时Close无Panic(t *testing.T) {
	addr, stop := startTestExtensionServer(t, &fakeExtension{}, newSlowFeedTransport(500*time.Microsecond))
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

	// 慢消费端：消费一帧后停摆，制造 recvCh 满背压
	//（pump 反复进入 select 发送分支，与 Close 竞争最激烈）
	<-recvCh

	// 并发 Close：pump 正在发送时触发
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := tr.Close(); err != nil {
			t.Errorf("Close 失败: %v", err)
		}
	}()

	// 等待 Close 完成 + pump 收口（通道关闭即证明 pump 退出且无 panic）
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case _, ok := <-recvCh:
			if !ok {
				<-done
				return // 通道已收口，无 panic，符合预期
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Fatal("等待 recvCh 收口超时（pump 未退出或 Close 卡死）")
}

// TestGRPCPluginTransport_pump阻塞时Close立即收口 覆盖 pump 阻塞场景：
// 消费端停摆使 recvCh 满载 → pump 阻塞在发送分支 → Close 取消
// context 应立即唤醒 pump 并收口通道（不依赖 5s stall 超时）
func TestGRPCPluginTransport_pump阻塞时Close立即收口(t *testing.T) {
	// interval 极短：持续灌帧使 recvCh（容量 128）迅速满载
	addr, stop := startTestExtensionServer(t, &fakeExtension{}, newSlowFeedTransport(100*time.Microsecond))
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

	// 不消费任何帧，等 recvCh 满、pump 进入发送等待
	time.Sleep(200 * time.Millisecond)

	start := time.Now()
	if err := tr.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	// 收口应在亚秒级完成（远小于 recvStallTimeout=5s），
	// 证明 context 取消唤醒了阻塞的 pump
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := <-recvCh; !ok {
			elapsed := time.Since(start)
			if elapsed > recvStallTimeout {
				t.Errorf("收口耗时 %v 超过 stall 超时，疑似依赖丢弃帧路径退出", elapsed)
			}
			return
		}
	}
	t.Fatal("等待 recvCh 收口超时（pump 未被 context 取消唤醒）")
}
