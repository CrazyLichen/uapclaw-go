package extproto

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/structpb"
)

// ──────────────────────────── 测试用结构体 ────────────────────────────

// echoExtensionServer 测试用扩展服务端，所有方法回显入参。
type echoExtensionServer struct {
	UnimplementedExtensionServer
}

// Register 返回固定声明。
func (s *echoExtensionServer) Register(context.Context, *RegisterRequest) (*RegisterResponse, error) {
	return &RegisterResponse{Decls: []*ExtensionDecl{{Metadata: &ExtensionMetadataProto{Id: "ext-1"}}}}, nil
}

// SetExtensionDir 回显目录。
func (s *echoExtensionServer) SetExtensionDir(_ context.Context, in *SetDirRequest) (*SetDirResponse, error) {
	return &SetDirResponse{}, nil
}

// GetMetadata 返回固定元数据。
func (s *echoExtensionServer) GetMetadata(context.Context, *GetMetadataRequest) (*GetMetadataResponse, error) {
	return &GetMetadataResponse{Metadata: &ExtensionMetadataProto{Id: "ext-1", Version: "1.0.0"}}, nil
}

// Shutdown 返回空响应。
func (s *echoExtensionServer) Shutdown(context.Context, *ShutdownRequest) (*ShutdownResponse, error) {
	return &ShutdownResponse{}, nil
}

// HandleEvent 回显上下文。
func (s *echoExtensionServer) HandleEvent(_ context.Context, in *HandleEventRequest) (*HandleEventResponse, error) {
	return &HandleEventResponse{Context: in.GetContext()}, nil
}

// TransportSend 回显接收字节数。
func (s *echoExtensionServer) TransportSend(_ context.Context, in *TransportSendRequest) (*TransportSendResponse, error) {
	return &TransportSendResponse{}, nil
}

// TransportRecv 推送一条数据后结束流。
func (s *echoExtensionServer) TransportRecv(_ *TransportRecvRequest, stream grpc.ServerStreamingServer[TransportRecvResponse]) error {
	return stream.Send(&TransportRecvResponse{Data: []byte("chunk")})
}

// TransportClose 返回空响应。
func (s *echoExtensionServer) TransportClose(context.Context, *TransportCloseRequest) (*TransportCloseResponse, error) {
	return &TransportCloseResponse{}, nil
}

// Encrypt 反转明文模拟加密。
func (s *echoExtensionServer) Encrypt(_ context.Context, in *EncryptRequest) (*EncryptResponse, error) {
	return &EncryptResponse{Ciphertext: "enc:" + in.GetPlaintext()}, nil
}

// Decrypt 去掉前缀模拟解密。
func (s *echoExtensionServer) Decrypt(_ context.Context, in *DecryptRequest) (*DecryptResponse, error) {
	return &DecryptResponse{Plaintext: in.GetCiphertext()}, nil
}

// ──────────────────────────── 测试辅助 ────────────────────────────

// startExtensionServer 起 bufconn 服务端并返回客户端连接。
func startExtensionServer(t *testing.T) ExtensionClient {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	RegisterExtensionServer(srv, &echoExtensionServer{})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return NewExtensionClient(conn)
}

// ──────────────────────────── 用例 ────────────────────────────

// TestExtensionClient_全方法往返 十类 RPC 经 bufconn 往返成功。
func TestExtensionClient_全方法往返(t *testing.T) {
	client := startExtensionServer(t)
	ctx := context.Background()

	reg, err := client.Register(ctx, &RegisterRequest{})
	require.NoError(t, err)
	require.Len(t, reg.GetDecls(), 1)
	assert.Equal(t, "ext-1", reg.GetDecls()[0].GetMetadata().GetId())

	_, err = client.SetExtensionDir(ctx, &SetDirRequest{Path: "/tmp/ext"})
	require.NoError(t, err)

	meta, err := client.GetMetadata(ctx, &GetMetadataRequest{})
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", meta.GetMetadata().GetVersion())

	_, err = client.Shutdown(ctx, &ShutdownRequest{})
	require.NoError(t, err)

	payload, err := structpb.NewStruct(map[string]any{"k": "v"})
	require.NoError(t, err)
	handled, err := client.HandleEvent(ctx, &HandleEventRequest{Event: "gateway:before_chat_request", Context: payload})
	require.NoError(t, err)
	assert.Equal(t, "v", handled.GetContext().GetFields()["k"].GetStringValue())

	_, err = client.TransportSend(ctx, &TransportSendRequest{Data: []byte("out")})
	require.NoError(t, err)

	stream, err := client.TransportRecv(ctx, &TransportRecvRequest{})
	require.NoError(t, err)
	chunk, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, []byte("chunk"), chunk.GetData())

	_, err = client.TransportClose(ctx, &TransportCloseRequest{})
	require.NoError(t, err)

	enc, err := client.Encrypt(ctx, &EncryptRequest{Plaintext: "secret"})
	require.NoError(t, err)
	assert.Equal(t, "enc:secret", enc.GetCiphertext())

	dec, err := client.Decrypt(ctx, &DecryptRequest{Ciphertext: "enc:secret"})
	require.NoError(t, err)
	assert.Equal(t, "enc:secret", dec.GetPlaintext())
}
