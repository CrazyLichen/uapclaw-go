package gateway_push

import (
	"context"
	"testing"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestNewChannelTransport 测试创建 ChannelTransport 实例
func TestNewChannelTransport(t *testing.T) {
	ct := NewChannelTransport()
	if ct == nil {
		t.Fatal("NewChannelTransport 返回 nil")
	}
	if ct.sendCh == nil {
		t.Fatal("sendCh 未初始化")
	}
	if ct.recvCh == nil {
		t.Fatal("recvCh 未初始化")
	}
}

// TestNewChannelTransportWithBuffer 测试自定义缓冲大小
func TestNewChannelTransportWithBuffer(t *testing.T) {
	ct := NewChannelTransportWithBuffer(10, 20)
	if ct == nil {
		t.Fatal("NewChannelTransportWithBuffer 返回 nil")
	}
	if cap(ct.sendCh) != 10 {
		t.Fatalf("sendCh 缓冲大小错误: 期望 10, 实际 %d", cap(ct.sendCh))
	}
	if cap(ct.recvCh) != 20 {
		t.Fatalf("recvCh 缓冲大小错误: 期望 20, 实际 %d", cap(ct.recvCh))
	}
}

// TestNewChannelTransportWithBuffer_零值使用默认缓冲 测试零值参数回退默认值
func TestNewChannelTransportWithBuffer_零值使用默认缓冲(t *testing.T) {
	ct := NewChannelTransportWithBuffer(0, -1)
	if cap(ct.sendCh) != defaultSendBufferSize {
		t.Fatalf("sendCh 缓冲大小错误: 期望 %d, 实际 %d", defaultSendBufferSize, cap(ct.sendCh))
	}
	if cap(ct.recvCh) != defaultRecvBufferSize {
		t.Fatalf("recvCh 缓冲大小错误: 期望 %d, 实际 %d", defaultRecvBufferSize, cap(ct.recvCh))
	}
}

// TestChannelTransport_Send 测试发送 JSON 字节
func TestChannelTransport_Send(t *testing.T) {
	ct := NewChannelTransportWithBuffer(1, 1)
	defer func() { _ = ct.Close() }()

	data := []byte(`{"request_id":"test-req-001","method":"session/prompt"}`)

	err := ct.Send(context.Background(), data)
	if err != nil {
		t.Fatalf("Send 失败: %v", err)
	}

	// 验证 AgentServer 端能收到
	select {
	case received := <-ct.SendCh():
		if string(received) != string(data) {
			t.Fatalf("接收到的数据错误: 期望 %s, 实际 %s", string(data), string(received))
		}
	case <-time.After(time.Second):
		t.Fatal("超时：未收到发送的数据")
	}
}

// TestChannelTransport_Send_关闭后返回错误 测试关闭后发送返回错误
func TestChannelTransport_Send_关闭后返回错误(t *testing.T) {
	ct := NewChannelTransportWithBuffer(1, 1)
	_ = ct.Close()

	err := ct.Send(context.Background(), []byte("test"))
	if err != ErrTransportClosed {
		t.Fatalf("期望 ErrTransportClosed, 实际: %v", err)
	}
}

// TestChannelTransport_Send_上下文取消 测试上下文取消时发送返回错误
func TestChannelTransport_Send_上下文取消(t *testing.T) {
	ct := NewChannelTransportWithBuffer(1, 1)
	defer func() { _ = ct.Close() }()

	// 先填满 sendCh 缓冲，使下一次 Send 阻塞
	_ = ct.Send(context.Background(), []byte("fill"))

	// 创建已取消的上下文
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := ct.Send(ctx, []byte("test-cancel"))
	if err == nil {
		t.Fatal("期望返回上下文取消错误, 实际 nil")
	}
}

// TestChannelTransport_Recv 测试接收 JSON 字节
func TestChannelTransport_Recv(t *testing.T) {
	ct := NewChannelTransportWithBuffer(1, 1)
	defer func() { _ = ct.Close() }()

	recvCh, err := ct.Recv()
	if err != nil {
		t.Fatalf("Recv 失败: %v", err)
	}
	if recvCh == nil {
		t.Fatal("Recv 返回 nil 通道")
	}

	// 模拟 AgentServer 写入 JSON 字节
	data := []byte(`{"request_id":"test-req-001","status":"succeeded"}`)
	ct.RecvCh() <- data

	// Gateway 端读取
	select {
	case received := <-recvCh:
		if string(received) != string(data) {
			t.Fatalf("接收到的数据错误: 期望 %s, 实际 %s", string(data), string(received))
		}
	case <-time.After(time.Second):
		t.Fatal("超时：未收到响应")
	}
}

// TestChannelTransport_Recv_关闭后返回错误 测试关闭后接收返回错误
func TestChannelTransport_Recv_关闭后返回错误(t *testing.T) {
	ct := NewChannelTransportWithBuffer(1, 1)
	_ = ct.Close()

	_, err := ct.Recv()
	if err != ErrTransportClosed {
		t.Fatalf("期望 ErrTransportClosed, 实际: %v", err)
	}
}

// TestChannelTransport_Close 测试关闭传输
func TestChannelTransport_Close(t *testing.T) {
	ct := NewChannelTransportWithBuffer(1, 1)

	err := ct.Close()
	if err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
	if !ct.closed {
		t.Fatal("closed 标志未设置")
	}
}

// TestChannelTransport_Close_重复关闭不报错 测试重复关闭不报错
func TestChannelTransport_Close_重复关闭不报错(t *testing.T) {
	ct := NewChannelTransportWithBuffer(1, 1)

	err := ct.Close()
	if err != nil {
		t.Fatalf("第一次 Close 失败: %v", err)
	}
	err = ct.Close()
	if err != nil {
		t.Fatalf("第二次 Close 失败: %v", err)
	}
}

// TestChannelTransport_实现AgentTransport接口 测试 ChannelTransport 实现 AgentTransport 接口
func TestChannelTransport_实现AgentTransport接口(t *testing.T) {
	// 编译期接口断言
	var _ AgentTransport = (*ChannelTransport)(nil)
}

// TestChannelTransport_完整收发流程 测试完整收发流程
func TestChannelTransport_完整收发流程(t *testing.T) {
	ct := NewChannelTransportWithBuffer(4, 4)
	defer func() { _ = ct.Close() }()

	// Gateway 端发送请求 JSON
	reqData := []byte(`{"request_id":"flow-req-001","method":"session/prompt","session_id":"sess-001","is_stream":true}`)
	err := ct.Send(context.Background(), reqData)
	if err != nil {
		t.Fatalf("Send 失败: %v", err)
	}

	// AgentServer 端接收请求
	select {
	case received := <-ct.SendCh():
		if string(received) != string(reqData) {
			t.Fatalf("AgentServer 收到的数据错误: %s", string(received))
		}
	case <-time.After(time.Second):
		t.Fatal("AgentServer 超时：未收到请求")
	}

	// AgentServer 端写入流式响应 JSON
	recvCh, err := ct.Recv()
	if err != nil {
		t.Fatalf("Recv 失败: %v", err)
	}

	responses := [][]byte{
		[]byte(`{"request_id":"flow-req-001","sequence":0,"is_final":false}`),
		[]byte(`{"request_id":"flow-req-001","sequence":1,"is_final":false}`),
		[]byte(`{"request_id":"flow-req-001","sequence":2,"is_final":true}`),
	}
	for _, resp := range responses {
		ct.RecvCh() <- resp
	}

	// Gateway 端接收所有响应
	for i := 0; i < 3; i++ {
		select {
		case received := <-recvCh:
			if string(received) != string(responses[i]) {
				t.Fatalf("响应错误: 期望 %s, 实际 %s", string(responses[i]), string(received))
			}
		case <-time.After(time.Second):
			t.Fatalf("Gateway 超时：未收到第 %d 个响应", i)
		}
	}
}
