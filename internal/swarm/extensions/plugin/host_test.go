package plugin

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"

	extproto "github.com/uapclaw/uapclaw-go/internal/swarm/extensions/plugin/proto"
)

// startTestExtensionServer 起本地 gRPC 服务并返回 conn 目标地址
func startTestExtensionServer(t *testing.T, impl ExtensionImpl, transport TransportImpl) (string, func()) {
	t.Helper()
	return startTestProtoServer(t, &extensionServer{impl: impl, transportImpl: transport})
}

// startTestProtoServer 起本地 gRPC 服务注册任意 proto 服务实现（测试辅助）
func startTestProtoServer(t *testing.T, srv extproto.ExtensionServer) (string, func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	grpcSrv := grpc.NewServer()
	extproto.RegisterExtensionServer(grpcSrv, srv)
	go func() { _ = grpcSrv.Serve(lis) }()
	return lis.Addr().String(), grpcSrv.Stop
}

// TestPluginClient_RegisterRoundtrip 测试 Register RPC 往返
func TestPluginClient_RegisterRoundtrip(t *testing.T) {
	addr, stop := startTestExtensionServer(t, &fakeExtension{decls: []Declaration{{
		Metadata:          Metadata{ID: "ext-1", Name: "测试扩展", Version: "0.1.0"},
		EventSubs:         []EventSubscription{{Event: "gateway:before_chat_request", Priority: 100}},
		ProvidesTransport: false,
	}}}, nil)
	defer stop()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	pc := NewPluginClientFromConn(conn)
	decls, err := pc.Register(context.Background())
	if err != nil {
		t.Fatalf("Register 失败: %v", err)
	}
	if len(decls) != 1 {
		t.Fatalf("声明数 = %d, 期望 1", len(decls))
	}
	if decls[0].Metadata.ID != "ext-1" || decls[0].Metadata.Name != "测试扩展" {
		t.Errorf("元数据不符: %+v", decls[0].Metadata)
	}
	if len(decls[0].EventSubs) != 1 || decls[0].EventSubs[0].Event != "gateway:before_chat_request" {
		t.Errorf("事件订阅不符: %+v", decls[0].EventSubs)
	}
}

// TestPluginClient_HandleEventRoundtrip 测试事件桥往返
func TestPluginClient_HandleEventRoundtrip(t *testing.T) {
	addr, stop := startTestExtensionServer(t, &fakeExtension{}, nil)
	defer stop()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	pc := NewPluginClientFromConn(conn)
	result, err := pc.HandleEvent(context.Background(), "agent_server:memory_before_chat", map[string]any{"memory_blocks": []any{}})
	if err != nil {
		t.Fatalf("HandleEvent 失败: %v", err)
	}
	if result["touched"] != true {
		t.Errorf("结果应含 touched=true, 实际 %v", result)
	}
}

// TestPluginClient_ShutdownAndSetDir 测试生命周期 RPC
func TestPluginClient_ShutdownAndSetDir(t *testing.T) {
	addr, stop := startTestExtensionServer(t, &fakeExtension{}, nil)
	defer stop()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	pc := NewPluginClientFromConn(conn)
	if err := pc.SetExtensionDir(context.Background(), "/tmp/ext-1"); err != nil {
		t.Errorf("SetExtensionDir 失败: %v", err)
	}
	if err := pc.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown 失败: %v", err)
	}
}

// TestPluginClient_MetadataRPC 测试 GetMetadata RPC
func TestPluginClient_MetadataRPC(t *testing.T) {
	addr, stop := startTestExtensionServer(t, &fakeExtension{decls: []Declaration{{
		Metadata: Metadata{ID: "ext-meta", Name: "元数据", Version: "1.2.3"},
	}}}, nil)
	defer stop()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	pc := NewPluginClientFromConn(conn)
	m, err := pc.GetMetadata(context.Background())
	if err != nil {
		t.Fatalf("GetMetadata 失败: %v", err)
	}
	if m.ID != "ext-meta" || m.Version != "1.2.3" {
		t.Errorf("元数据不符: %+v", m)
	}
}

// TestStructRoundtrip structpb 往返验证（防御性）
func TestStructRoundtrip(t *testing.T) {
	m := map[string]any{"memory_blocks": []any{"a", "b"}, "params": map[string]any{"k": "v"}}
	st, err := structpb.NewStruct(m)
	if err != nil {
		t.Fatalf("NewStruct 失败: %v", err)
	}
	back := st.AsMap()
	blocks, ok := back["memory_blocks"].([]any)
	if !ok || len(blocks) != 2 || blocks[0] != "a" {
		t.Errorf("memory_blocks 往返不符: %v", back["memory_blocks"])
	}
	_ = time.Now // 保持 time import（超时配置用）
}

// ──────────────────────────── 以下为覆盖率补测 ────────────────────────────

// errExtension 所有方法返回错误的扩展实现（覆盖错误分支）
type errExtension struct {
	errs []error
}

func (e *errExtension) Register(ctx context.Context) ([]Declaration, error) {
	e.errs = append(e.errs, nil)
	return nil, errors.New("register-boom")
}

func (e *errExtension) Shutdown(ctx context.Context) error { return errors.New("shutdown-boom") }

func (e *errExtension) HandleEvent(ctx context.Context, event string, ctxData map[string]any) (map[string]any, error) {
	return nil, errors.New("handleevent-boom")
}

// TestPluginClient_RegisterError 覆盖 host.go Register 错误分支 + serve.go Register 错误分支
func TestPluginClient_RegisterError(t *testing.T) {
	addr, stop := startTestExtensionServer(t, &errExtension{}, nil)
	defer stop()

	conn, err := grpcDialTest(addr)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	pc := NewPluginClientFromConn(conn)
	if _, err := pc.Register(context.Background()); err == nil || !strings.Contains(err.Error(), "register-boom") {
		t.Errorf("Register 应返回 register-boom 错误, 实际: %v", err)
	}
}

// TestPluginClient_GetMetadataError 覆盖 GetMetadata 两条错误分支：
// impl.Register 出错返回空响应 + host 侧 RPC 错误分支
func TestPluginClient_GetMetadataError(t *testing.T) {
	addr, stop := startTestExtensionServer(t, &errExtension{}, nil)
	defer stop()

	conn, err := grpcDialTest(addr)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	pc := NewPluginClientFromConn(conn)
	// impl.Register 返回 error → 服务端返回空响应 + err → host 侧拿到 RPC error
	if _, err := pc.GetMetadata(context.Background()); err == nil {
		t.Error("GetMetadata 应返回错误（impl.Register 出错）")
	}
}

// TestPluginClient_ShutdownError 覆盖 Shutdown RPC 错误分支
func TestPluginClient_ShutdownError(t *testing.T) {
	addr, stop := startTestExtensionServer(t, &errExtension{}, nil)
	defer stop()

	conn, err := grpcDialTest(addr)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	pc := NewPluginClientFromConn(conn)
	if err := pc.Shutdown(context.Background()); err == nil || !strings.Contains(err.Error(), "shutdown-boom") {
		t.Errorf("Shutdown 应返回 shutdown-boom 错误, 实际: %v", err)
	}
}

// TestPluginClient_HandleEventErrors 覆盖 HandleEvent 错误分支：
// ① 连接断开导致 RPC 错误；② NewStruct 非法类型错误；③ 服务端 impl 错误传播
func TestPluginClient_HandleEventErrors(t *testing.T) {
	// 场景①：RPC 错误（连到没有服务的地址）
	badConn, err := grpcDialTest("127.0.0.1:1")
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer func() { _ = badConn.Close() }()
	badPC := NewPluginClientFromConn(badConn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := badPC.HandleEvent(ctx, "agent_server:memory_before_chat", map[string]any{}); err == nil {
		t.Error("连接断开时 HandleEvent 应返回错误")
	}

	// 场景②：NewStruct 非法值（chan 不可序列化为 structpb）
	addr, stop := startTestExtensionServer(t, &fakeExtension{}, nil)
	defer stop()
	conn, err := grpcDialTest(addr)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer func() { _ = conn.Close() }()
	pc := NewPluginClientFromConn(conn)
	if _, err := pc.HandleEvent(context.Background(), "e", map[string]any{"bad": make(chan int)}); err == nil {
		t.Error("NewStruct 非法类型时 HandleEvent 应返回错误")
	}

	// 场景③：服务端 impl.HandleEvent 出错（错误经 RPC 传播回宿主）
	addr2, stop2 := startTestExtensionServer(t, &errExtension{}, nil)
	defer stop2()
	conn2, err := grpcDialTest(addr2)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer func() { _ = conn2.Close() }()
	pc2 := NewPluginClientFromConn(conn2)
	if _, err := pc2.HandleEvent(context.Background(), "e", map[string]any{}); err == nil ||
		!strings.Contains(err.Error(), "handleevent-boom") {
		t.Errorf("服务端 impl 出错时应传播, 实际: %v", err)
	}
}

// TestPluginClient_RPC连接断开错误 覆盖 Register/GetMetadata 连接断开分支
func TestPluginClient_RPC连接断开错误(t *testing.T) {
	// 起服务再立即停掉：得到"已失效地址"
	addr, stop := startTestExtensionServer(t, &fakeExtension{}, nil)
	stop()

	badConn, err := grpcDialTest(addr)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer func() { _ = badConn.Close() }()
	pc := NewPluginClientFromConn(badConn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := pc.Register(ctx); err == nil {
		t.Error("连接断开时 Register 应返回错误")
	}
	if _, err := pc.GetMetadata(ctx); err == nil {
		t.Error("连接断开时 GetMetadata 应返回错误")
	}
	if err := pc.SetExtensionDir(ctx, "/tmp/x"); err == nil {
		t.Error("连接断开时 SetExtensionDir 应返回错误")
	}
}

// TestLoadPluginClient_入口不存在 覆盖 os.Stat 错误分支
func TestLoadPluginClient_入口不存在(t *testing.T) {
	_, err := LoadPluginClient(t.TempDir())
	if err == nil {
		t.Fatal("入口不存在时应返回错误")
	}
	if !strings.Contains(err.Error(), "ext-plugin") {
		t.Errorf("错误应含入口文件名, 实际: %v", err)
	}
}

// TestLoadPluginClient_握手失败 覆盖 dialPlugin 失败分支 + client.Kill 回收
func TestLoadPluginClient_握手失败(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(root, EntryFilename)
	// 假二进制：echo stdout（非 go-plugin 握手协议）→ 握手失败/超时
	if err := os.WriteFile(entry, []byte("#!/bin/sh\necho not-a-plugin\n"), 0o755); err != nil {
		t.Fatalf("写假入口失败: %v", err)
	}

	start := time.Now()
	pc, err := LoadPluginClient(root)
	if err == nil {
		if pc != nil {
			pc.Kill()
		}
		t.Fatal("假二进制握手应失败")
	}
	// go-plugin 握手失败自带超时（1 分钟级），限制测试时长上限 90 秒
	if elapsed := time.Since(start); elapsed > 90*time.Second {
		t.Errorf("握手失败耗时过长: %v", elapsed)
	}
}

// TestPluginClient_Kill空客户端 Kill 在无 client/conn 时不 panic
func TestPluginClient_Kill空客户端(t *testing.T) {
	pc := &PluginClient{}
	pc.Kill() // 不应 panic
}
