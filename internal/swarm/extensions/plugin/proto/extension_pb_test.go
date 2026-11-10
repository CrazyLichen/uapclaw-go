package extproto

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// allMessages 返回本文件定义的全部消息零值实例。
func allMessages() []proto.Message {
	return []proto.Message{
		&RegisterRequest{},
		&ExtensionDecl{},
		&EventSubscription{},
		&RegisterResponse{},
		&SetDirRequest{},
		&SetDirResponse{},
		&GetMetadataRequest{},
		&GetMetadataResponse{},
		&ExtensionMetadataProto{},
		&ShutdownRequest{},
		&ShutdownResponse{},
		&HandleEventRequest{},
		&HandleEventResponse{},
		&TransportSendRequest{},
		&TransportSendResponse{},
		&TransportRecvRequest{},
		&TransportRecvResponse{},
		&TransportCloseRequest{},
		&TransportCloseResponse{},
		&EncryptRequest{},
		&EncryptResponse{},
		&DecryptRequest{},
		&DecryptResponse{},
	}
}

// callGetters 反射调用消息的全部 Get* 方法（含 nil 安全分支）。
func callGetters(t *testing.T, msg proto.Message) int {
	t.Helper()
	rv := reflect.ValueOf(msg)
	called := 0
	for i := 0; i < rv.NumMethod(); i++ {
		method := rv.Type().Method(i)
		if len(method.Name) < 4 || method.Name[:3] != "Get" || method.Type.NumIn() != 1 {
			continue
		}
		rv.Method(i).Call(nil)
		called++
	}
	return called
}

// ──────────────────────────── 用例 ────────────────────────────

// TestMessages_零值往返与Getter 全部消息可序列化、反序列化，Getter 可安全调用。
func TestMessages_零值往返与Getter(t *testing.T) {
	totalGetters := 0
	for _, msg := range allMessages() {
		name := reflect.TypeOf(msg).Elem().Name()

		raw, err := proto.Marshal(msg)
		require.NoError(t, err, "%s 序列化失败", name)

		decoded := msg.ProtoReflect().Type().New().Interface()
		require.NoError(t, proto.Unmarshal(raw, decoded), "%s 反序列化失败", name)
		assert.True(t, proto.Equal(msg, decoded), "%s 零值往返应等价", name)

		if stringer, ok := msg.(interface{ String() string }); ok {
			_ = stringer.String()
		}
		if descriptor, ok := msg.(interface{ Descriptor() ([]byte, []int) }); ok {
			_, _ = descriptor.Descriptor()
		}

		totalGetters += callGetters(t, msg)

		if resetter, ok := msg.(interface{ Reset() }); ok {
			resetter.Reset()
		}
		assert.True(t, proto.Equal(msg, msg.ProtoReflect().Type().New().Interface()), "%s Reset 后应为零值", name)
	}
	assert.Greater(t, totalGetters, 0, "必须覆盖到 Get* 方法")
}

// TestRegisterRequest_字段往返 元数据与订阅列表编码后可还原。
func TestRegisterRequest_字段往返(t *testing.T) {
	req := &RegisterResponse{
		Decls: []*ExtensionDecl{
			{
				Metadata: &ExtensionMetadataProto{
					Id:           "ext-1",
					Name:         "demo",
					Version:      "1.0.0",
					Dependencies: map[string]string{"base": "0.1.0"},
				},
				EventSubs: []*EventSubscription{
					{Event: "gateway:before_chat_request", Priority: 100},
				},
				ProvidesTransport: true,
			},
		},
	}

	raw, err := proto.Marshal(req)
	require.NoError(t, err)

	var decoded RegisterResponse
	require.NoError(t, proto.Unmarshal(raw, &decoded))
	require.Len(t, decoded.GetDecls(), 1)

	decl := decoded.GetDecls()[0]
	assert.True(t, decl.GetProvidesTransport())
	assert.Equal(t, "ext-1", decl.GetMetadata().GetId())
	assert.Equal(t, "demo", decl.GetMetadata().GetName())
	assert.Equal(t, map[string]string{"base": "0.1.0"}, decl.GetMetadata().GetDependencies())
	require.Len(t, decl.GetEventSubs(), 1)
	assert.Equal(t, "gateway:before_chat_request", decl.GetEventSubs()[0].GetEvent())
	assert.Equal(t, int32(100), decl.GetEventSubs()[0].GetPriority())
}

// TestHandleEventRequest_上下文往返 structpb 上下文可无损往返。
func TestHandleEventRequest_上下文往返(t *testing.T) {
	ctxPayload, err := structpb.NewStruct(map[string]any{"channel": "web", "count": float64(2)})
	require.NoError(t, err)

	req := &HandleEventRequest{Event: "gateway:after_chat_response", Context: ctxPayload}
	raw, err := proto.Marshal(req)
	require.NoError(t, err)

	var decoded HandleEventRequest
	require.NoError(t, proto.Unmarshal(raw, &decoded))
	assert.Equal(t, "gateway:after_chat_response", decoded.GetEvent())
	require.NotNil(t, decoded.GetContext())
	assert.Equal(t, "web", decoded.GetContext().GetFields()["channel"].GetStringValue())
}

// TestEncryptDecrypt_字段往返 加解密请求/响应字段可无损往返。
func TestEncryptDecrypt_字段往返(t *testing.T) {
	encReq := &EncryptRequest{Plaintext: "plain"}
	raw, err := proto.Marshal(encReq)
	require.NoError(t, err)
	var encDecoded EncryptRequest
	require.NoError(t, proto.Unmarshal(raw, &encDecoded))
	assert.Equal(t, "plain", encDecoded.GetPlaintext())

	decResp := &DecryptResponse{Plaintext: "plain"}
	raw, err = proto.Marshal(decResp)
	require.NoError(t, err)
	var decDecoded DecryptResponse
	require.NoError(t, proto.Unmarshal(raw, &decDecoded))
	assert.Equal(t, "plain", decDecoded.GetPlaintext())
}

// TestTransportMessages_字段往返 传输层四类消息字段可无损往返。
func TestTransportMessages_字段往返(t *testing.T) {
	send := &TransportSendRequest{Data: []byte("payload")}
	raw, err := proto.Marshal(send)
	require.NoError(t, err)
	var sendDecoded TransportSendRequest
	require.NoError(t, proto.Unmarshal(raw, &sendDecoded))
	assert.Equal(t, []byte("payload"), sendDecoded.GetData())

	recv := &TransportRecvResponse{Data: []byte("inbound")}
	raw, err = proto.Marshal(recv)
	require.NoError(t, err)
	var recvDecoded TransportRecvResponse
	require.NoError(t, proto.Unmarshal(raw, &recvDecoded))
	assert.Equal(t, []byte("inbound"), recvDecoded.GetData())

	closeReq := &TransportCloseRequest{}
	raw, err = proto.Marshal(closeReq)
	require.NoError(t, err)
	var closeDecoded TransportCloseRequest
	require.NoError(t, proto.Unmarshal(raw, &closeDecoded))
	assert.True(t, proto.Equal(closeReq, &closeDecoded))
}
