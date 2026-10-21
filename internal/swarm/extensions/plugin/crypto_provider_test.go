package plugin

import (
	"testing"

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

	enc, err := p.Encrypt("k1")
	if err != nil {
		t.Fatalf("Encrypt 失败: %v", err)
	}
	if enc != "enc:k1" {
		t.Errorf("Encrypt = %q, 期望 enc:k1", enc)
	}
	dec, err := p.Decrypt("enc:k1")
	if err != nil {
		t.Fatalf("Decrypt 失败: %v", err)
	}
	if dec != "k1" {
		t.Errorf("Decrypt = %q, 期望 k1", dec)
	}
}
