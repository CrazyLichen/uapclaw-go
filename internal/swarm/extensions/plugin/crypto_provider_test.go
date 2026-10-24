package plugin

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/uapclaw/uapclaw-go/internal/common/crypto"
)

// TestGRPCPluginCryptoProvider_接口满足 编译期断言满足 crypto.CryptoProvider
func TestGRPCPluginCryptoProvider_接口满足(t *testing.T) {
	var _ crypto.CryptoProvider = (*GRPCPluginCryptoProvider)(nil)
}

// TestGRPCPluginCryptoProvider_往返 进程内 gRPC 代理加解密
func TestGRPCPluginCryptoProvider_往返(t *testing.T) {
	addr, stop := startTestProtoServer(t, &extensionServer{impl: &fakeExtension{}, cryptoImpl: &fakeCryptoImpl{}})
	defer stop()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	pc := NewPluginClientFromConn(conn)
	p := NewGRPCPluginCryptoProvider(pc)

	ctx := context.Background()
	enc, err := p.Encrypt(ctx, "k1")
	if err != nil {
		t.Fatalf("Encrypt 失败: %v", err)
	}
	if enc != "enc:k1" {
		t.Errorf("Encrypt = %q, 期望 enc:k1", enc)
	}
	dec, err := p.Decrypt(ctx, "enc:k1")
	if err != nil {
		t.Fatalf("Decrypt 失败: %v", err)
	}
	if dec != "k1" {
		t.Errorf("Decrypt = %q, 期望 k1", dec)
	}
}

// TestGRPCPluginCryptoProvider_超时兜底 M-04：入参无 deadline 时施加 5s 兜底，
// 已带 deadline 的 ctx 原样透传（不叠加）
func TestGRPCPluginCryptoProvider_超时兜底(t *testing.T) {
	// Background：无 deadline → 兜底后带 deadline
	ctx1, cancel1 := withCryptoTimeout(context.Background())
	defer cancel1()
	if _, ok := ctx1.Deadline(); !ok {
		t.Error("Background ctx 应被施加兜底 deadline")
	}

	// 已带 deadline：原样返回（同一 ctx 实例）
	deadline := time.Now().Add(3 * time.Second)
	ctx2, cancel2 := context.WithDeadline(context.Background(), deadline)
	defer cancel2()
	ctx2Out, noop := withCryptoTimeout(ctx2)
	defer noop()
	if _, ok := ctx2Out.Deadline(); !ok {
		t.Error("已有 deadline 的 ctx 不应被移除")
	}
	if d, _ := ctx2Out.Deadline(); !d.Equal(deadline) {
		t.Errorf("已有 deadline 不应被改写，期望 %v，实际 %v", deadline, d)
	}
}
