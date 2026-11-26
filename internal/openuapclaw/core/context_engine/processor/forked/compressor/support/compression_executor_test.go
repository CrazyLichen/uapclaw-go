package support

import (
	"context"
	"errors"
	"strings"
	"testing"

	llm "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/stretchr/testify/require"
)

// fakeCompressionClient 可控失败的模型客户端
type fakeCompressionClient struct {
	invokeErr error
	response  *llm_schema.AssistantMessage
	lastMsgs  model_clients.MessagesParam
}

func (f *fakeCompressionClient) Invoke(_ context.Context, messages model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llm_schema.AssistantMessage, error) {
	f.lastMsgs = messages
	if f.invokeErr != nil {
		return nil, f.invokeErr
	}
	return f.response, nil
}

func (f *fakeCompressionClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llm_schema.AssistantMessageChunk, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeCompressionClient) GenerateImage(_ context.Context, _ []*llm_schema.UserMessage, _ ...model_clients.GenerateImageOption) (*llm_schema.ImageGenerationResponse, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeCompressionClient) GenerateSpeech(_ context.Context, _ []*llm_schema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llm_schema.AudioGenerationResponse, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeCompressionClient) GenerateVideo(_ context.Context, _ []*llm_schema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llm_schema.VideoGenerationResponse, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeCompressionClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, errors.New("not supported")
}

func (f *fakeCompressionClient) SupportsKVCacheRelease() bool  { return false }
func (f *fakeCompressionClient) SupportsKVCacheAffinity() bool { return false }
func (f *fakeCompressionClient) BuildKVCacheAffinityInvokeKwargs(_ any, _, _ string, _ int) map[string]any {
	return nil
}

func newTestCompressionModel(t *testing.T, client *fakeCompressionClient) *llm.Model {
	t.Helper()
	clientCfg, err := llm_schema.NewModelClientConfig("openai", "test-key", "https://mock.test", llm_schema.WithVerifySSL(false))
	require.NoError(t, err)
	m, err := llm.NewModel(clientCfg, nil, llm.WithClient(client))
	require.NoError(t, err)
	return m
}

// TestClassifyCompressionError_标记分类 测试错误标记 → 分类映射
// Python: compression_executor.py classify_compression_error + _classify_compression_error_kind
func TestClassifyCompressionError_标记分类(t *testing.T) {
	cases := []struct {
		msg  string
		kind CompressionErrorKind
	}{
		{"prompt is too long: 100000 > 8192", CompressionErrorKindContextOverflow},
		{"rate limit exceeded, too many requests", CompressionErrorKindRateLimit},
		{"invalid api key", CompressionErrorKindAuthentication},
		{"read timeout after 30s", CompressionErrorKindTimeout},
		{"internal server error", CompressionErrorKindServerUnstable},
		{"bad request", CompressionErrorKindAPIRequestError},
		{"mystery failure", CompressionErrorKindUnknown},
	}
	for _, tc := range cases {
		err := classifyCompressionError(errors.New(tc.msg))
		require.Equal(t, tc.kind, err.Kind, "message=%q", tc.msg)
	}
	// 状态码分支：_extract_status_code
	err := classifyCompressionError(errors.New("http 413 payload"))
	require.Equal(t, CompressionErrorKindContextOverflow, err.Kind)
	// is_context_overflow
	require.True(t, err.IsContextOverflow())
	require.False(t, classifyCompressionError(errors.New("boom")).IsContextOverflow())
}

// TestCompressionExecutorInvoke_成功路径 测试正常压缩调用与消息装配
// Python: compression_executor.py CompressionExecutor.invoke + build_messages
func TestCompressionExecutorInvoke_成功路径(t *testing.T) {
	client := &fakeCompressionClient{response: &llm_schema.AssistantMessage{}}
	model := newTestCompressionModel(t, client)
	executor := NewCompressionExecutor(model)
	req := &CompressionRequest{
		Prompt:         "summarize",
		SystemMessages: []llm_schema.BaseMessage{llm_schema.NewSystemMessage("sys")},
		ContextMessages: []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("hello"),
			llm_schema.NewUserMessage("world"),
		},
		ExcludeRecentMessages: 1,
	}
	result, err := executor.Invoke(context.Background(), req, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, executor.LastResponse())
	// build_messages: system + context（去掉最近 exclude_recent 条）+ prompt 用户消息
	require.Len(t, client.lastMsgs.Messages(), 3)
	last, ok := client.lastMsgs.Messages()[2].(*llm_schema.UserMessage)
	require.True(t, ok)
	require.True(t, strings.Contains(last.GetContent().String(), "summarize"))
}

// TestBuildContextMessages_排除最近消息 测试种子上下文构建
// Python: compression_executor.py build_context_messages
func TestBuildContextMessages_排除最近消息(t *testing.T) {
	req := &CompressionRequest{
		SystemMessages: []llm_schema.BaseMessage{llm_schema.NewSystemMessage("sys")},
		ContextMessages: []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("a"),
			llm_schema.NewUserMessage("b"),
			llm_schema.NewUserMessage("c"),
		},
		Prompt:                "summarize",
		ExcludeRecentMessages: 2,
	}
	got := BuildContextMessages(req)
	require.Len(t, got, 2) // sys + "a"
	msgs := BuildMessages(req)
	require.Len(t, msgs, 3)
	last, ok := msgs[len(msgs)-1].(*llm_schema.UserMessage)
	require.True(t, ok)
	require.True(t, strings.Contains(last.GetContent().String(), "summarize"))
}
