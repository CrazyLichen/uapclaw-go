package plugin

import (
	"context"
	"net"
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
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	srv := grpc.NewServer()
	extproto.RegisterExtensionServer(srv, &extensionServer{impl: impl, transportImpl: transport})
	go func() { _ = srv.Serve(lis) }()
	return lis.Addr().String(), srv.Stop
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
