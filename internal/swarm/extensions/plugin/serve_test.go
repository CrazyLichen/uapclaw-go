package plugin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"

	extproto "github.com/uapclaw/uapclaw-go/internal/swarm/extensions/plugin/proto"
)

// fakeExtension 用于测试的扩展实现
type fakeExtension struct {
	decls []Declaration
}

func (f *fakeExtension) Register(ctx context.Context) ([]Declaration, error) {
	return f.decls, nil
}

func (f *fakeExtension) Shutdown(ctx context.Context) error { return nil }

func (f *fakeExtension) HandleEvent(ctx context.Context, event string, ctxData map[string]any) (map[string]any, error) {
	if ctxData == nil {
		ctxData = map[string]any{}
	}
	ctxData["touched"] = true
	return ctxData, nil
}

// TestDeclaration_MetadataDefaults 测试 Declaration 构造零值
func TestDeclaration_MetadataDefaults(t *testing.T) {
	d := Declaration{}
	if d.ProvidesTransport {
		t.Error("ProvidesTransport 零值应为 false")
	}
	if len(d.EventSubs) != 0 {
		t.Errorf("EventSubs 零值应为空, 实际 %d", len(d.EventSubs))
	}
}

// TestEventSubscription_Fields 测试事件订阅结构
func TestEventSubscription_Fields(t *testing.T) {
	sub := EventSubscription{Event: "gateway:before_chat_request", Priority: 100}
	if sub.Event != "gateway:before_chat_request" || sub.Priority != 100 {
		t.Errorf("EventSubscription 字段不符: %+v", sub)
	}
}

// TestFakeExtension_接口满足 验证 fakeExtension 满足 ExtensionImpl 接口
func TestFakeExtension_接口满足(t *testing.T) {
	var _ ExtensionImpl = (*fakeExtension)(nil)
}

// ──────────────────────────── 以下为覆盖率补测 ────────────────────────────

// fakeRecvErrTransport Recv 返回错误的 transport 实现
type fakeRecvErrTransport struct{}

func (f *fakeRecvErrTransport) Send(ctx context.Context, data []byte) error   { return nil }
func (f *fakeRecvErrTransport) Recv() (<-chan []byte, error)                  { return nil, errors.New("recv-boom") }
func (f *fakeRecvErrTransport) Close() error                                  { return nil }

// fakeNoImplServer 仅嵌入 Unimplemented 的服务（所有 RPC 走默认"未实现"）
type fakeNoImplServer struct {
	extproto.UnimplementedExtensionServer
}

// fakeRecvTransport 可控 Recv 通道的 transport 实现
type fakeRecvTransport struct {
	recvCh    chan []byte
	closeOnce sync.Once
}

func (f *fakeRecvTransport) Send(ctx context.Context, data []byte) error { return nil }
func (f *fakeRecvTransport) Recv() (<-chan []byte, error) {
	return f.recvCh, nil
}
func (f *fakeRecvTransport) Close() error {
	f.closeOnce.Do(func() { close(f.recvCh) })
	return nil
}

// TestExtensionGRPCPlugin_GRPCServer 覆盖 GRPCServer 注册路径
func TestExtensionGRPCPlugin_GRPCServer(t *testing.T) {
	p := &ExtensionGRPCPlugin{impl: &extensionServer{impl: &fakeExtension{}}}
	srv := grpc.NewServer()
	defer srv.Stop()
	if err := p.GRPCServer(nil, srv); err != nil {
		t.Fatalf("GRPCServer 失败: %v", err)
	}
	// 通过服务信息证明已注册（不能二次注册——grpc 会 log.Fatalf）
	info, ok := srv.GetServiceInfo()["extension.v1.Extension"]
	if !ok {
		t.Fatal("GRPCServer 后应注册 extension.v1.Extension 服务")
	}
	if len(info.Methods) == 0 {
		t.Error("注册的服务应含方法列表")
	}
}

// TestExtensionGRPCPlugin_GRPCClient 返回 connDialer 包装
func TestExtensionGRPCPlugin_GRPCClient(t *testing.T) {
	p := &ExtensionGRPCPlugin{}
	raw, err := p.GRPCClient(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("GRPCClient 失败: %v", err)
	}
	dialer, ok := raw.(interface{ Conn() *grpc.ClientConn })
	if !ok {
		t.Fatalf("GRPCClient 应返回 connDialer, 实际 %T", raw)
	}
	if dialer.Conn() != nil {
		t.Error("未传 conn 时 Conn() 应为 nil")
	}
}

// TestExtensionServer_RegisterTransport降级 覆盖声明 ProvidesTransport 但 transportImpl==nil 的降级分支
func TestExtensionServer_RegisterTransport降级(t *testing.T) {
	srv := &extensionServer{impl: &fakeExtension{decls: []Declaration{{
		Metadata:          Metadata{ID: "tx-decl"},
		ProvidesTransport: true,
	}}}, transportImpl: nil}
	resp, err := srv.Register(context.Background(), &extproto.RegisterRequest{})
	if err != nil {
		t.Fatalf("Register 失败: %v", err)
	}
	if len(resp.GetDecls()) != 1 || resp.GetDecls()[0].GetProvidesTransport() {
		t.Errorf("transportImpl 缺失时声明应降级 false, 实际: %v", resp.GetDecls())
	}
}

// TestExtensionServer_ErrNoTransport三件套 覆盖 transportImpl==nil 时三个 transport RPC 的 ErrNoTransport 分支
func TestExtensionServer_ErrNoTransport三件套(t *testing.T) {
	srv := &extensionServer{impl: &fakeExtension{}, transportImpl: nil}

	if _, err := srv.TransportSend(context.Background(), &extproto.TransportSendRequest{Data: []byte("x")}); !errors.Is(err, ErrNoTransport) {
		t.Errorf("TransportSend 应返回 ErrNoTransport, 实际: %v", err)
	}
	if _, err := srv.TransportClose(context.Background(), &extproto.TransportCloseRequest{}); !errors.Is(err, ErrNoTransport) {
		t.Errorf("TransportClose 应返回 ErrNoTransport, 实际: %v", err)
	}

	// TransportRecv 是 server-streaming RPC，直接调用需实现 stream 接口
	var capturedErr error
	recvSrv := &extensionServer{impl: &fakeExtension{}, transportImpl: nil}
	stream := &fakeTransportRecvStream{onSend: func(resp *extproto.TransportRecvResponse) error { return nil }}
	capturedErr = recvSrv.TransportRecv(&extproto.TransportRecvRequest{}, stream)
	if !errors.Is(capturedErr, ErrNoTransport) {
		t.Errorf("TransportRecv 应返回 ErrNoTransport, 实际: %v", capturedErr)
	}
}

// fakeTransportRecvStream 测试用 TransportRecv 流（嵌入 grpc.ServerStream，仅实现 Send）
type fakeTransportRecvStream struct {
	grpc.ServerStream
	onSend func(*extproto.TransportRecvResponse) error
}

func (f *fakeTransportRecvStream) Send(resp *extproto.TransportRecvResponse) error {
	return f.onSend(resp)
}

// TestExtensionServer_TransportRecv正常流 覆盖流循环：通道数据逐条下发 + 通道关闭后正常返回
func TestExtensionServer_TransportRecv正常流(t *testing.T) {
	tr := &fakeRecvTransport{recvCh: make(chan []byte, 4)}
	srv := &extensionServer{impl: &fakeExtension{}, transportImpl: tr}

	var got [][]byte
	stream := &fakeTransportRecvStream{onSend: func(resp *extproto.TransportRecvResponse) error {
		got = append(got, resp.GetData())
		return nil
	}}
	done := make(chan error, 1)
	go func() { done <- srv.TransportRecv(&extproto.TransportRecvRequest{}, stream) }()

	// 写入两帧后关闭通道
	tr.recvCh <- []byte("frame-1")
	tr.recvCh <- []byte("frame-2")
	close(tr.recvCh)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("通道关闭后 TransportRecv 应正常返回, 实际: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("TransportRecv 未随通道关闭返回")
	}
	if len(got) != 2 || string(got[0]) != "frame-1" || string(got[1]) != "frame-2" {
		t.Errorf("流帧 = %v, 期望两帧按序", got)
	}
}

// TestExtensionServer_TransportRecv发送失败 覆盖 stream.Send 出错分支
func TestExtensionServer_TransportRecv发送失败(t *testing.T) {
	tr := &fakeRecvTransport{recvCh: make(chan []byte, 4)}
	srv := &extensionServer{impl: &fakeExtension{}, transportImpl: tr}

	stream := &fakeTransportRecvStream{onSend: func(resp *extproto.TransportRecvResponse) error {
		return errors.New("stream-send-boom")
	}}
	done := make(chan error, 1)
	go func() { done <- srv.TransportRecv(&extproto.TransportRecvRequest{}, stream) }()

	tr.recvCh <- []byte("x")
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "stream-send-boom") {
			t.Errorf("stream.Send 失败应传播, 实际: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stream.Send 失败未返回")
	}
	close(tr.recvCh)
}

// TestExtensionServer_TransportRecv失败 覆盖 Recv() 出错分支
func TestExtensionServer_TransportRecv失败(t *testing.T) {
	srv := &extensionServer{impl: &fakeExtension{}, transportImpl: &fakeRecvErrTransport{}}
	stream := &fakeTransportRecvStream{onSend: func(resp *extproto.TransportRecvResponse) error { return nil }}
	if err := srv.TransportRecv(&extproto.TransportRecvRequest{}, stream); !strings.Contains(fmt.Sprint(err), "recv-boom") {
		t.Errorf("Recv 失败应传播, 实际: %v", err)
	}
}

// TestExtensionServer_GetMetadata空声明 覆盖 len(decls)==0 分支（返回空响应）
func TestExtensionServer_GetMetadata空声明(t *testing.T) {
	srv := &extensionServer{impl: &fakeExtension{decls: nil}}
	resp, err := srv.GetMetadata(context.Background(), &extproto.GetMetadataRequest{})
	if err != nil {
		t.Fatalf("GetMetadata 失败: %v", err)
	}
	if resp.GetMetadata() != nil {
		t.Errorf("空声明时 Metadata 应为 nil, 实际: %v", resp.GetMetadata())
	}
}

// TestExtensionServer_默认未实现方法 验证嵌入 Unimplemented 后全部 RPC 可达（防御性）
func TestExtensionServer_默认未实现方法(t *testing.T) {
	var srv extproto.ExtensionServer = &fakeNoImplServer{}
	ctx := context.Background()
	if _, err := srv.Register(ctx, &extproto.RegisterRequest{}); !strings.Contains(fmt.Sprint(err), "Unimplemented") {
		t.Errorf("未覆盖 Register 应返回 Unimplemented, 实际: %v", err)
	}
	if _, err := srv.GetMetadata(ctx, &extproto.GetMetadataRequest{}); !strings.Contains(fmt.Sprint(err), "Unimplemented") {
		t.Errorf("未覆盖 GetMetadata 应返回 Unimplemented, 实际: %v", err)
	}
	if _, err := srv.HandleEvent(ctx, &extproto.HandleEventRequest{Event: "e"}); !strings.Contains(fmt.Sprint(err), "Unimplemented") {
		t.Errorf("未覆盖 HandleEvent 应返回 Unimplemented, 实际: %v", err)
	}
}

// TestMetadataToProto_DependenciesNil 覆盖 Dependencies 为 nil 时补空 map 分支
func TestMetadataToProto_DependenciesNil(t *testing.T) {
	pb := metadataToProto(Metadata{ID: "dep-nil"})
	if pb.GetDependencies() == nil {
		t.Error("Dependencies nil 时应补空 map")
	}
	if len(pb.GetDependencies()) != 0 {
		t.Errorf("补的空 map 应为空, 实际: %v", pb.GetDependencies())
	}
}
