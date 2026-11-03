package agents

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeStreamClient 立即返回一个已关闭的 chunk 通道，模拟"流结束"。
//
// 必须实现完整 BaseModelClient 接口，才能注册进 ClientRegistry。
type fakeStreamClient struct {
	// chunks 预置输出片段
	chunks []*llmschema.AssistantMessageChunk
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestCallLLMStream_CanceledMidStream 防 D2b 回归：
// 流已结束但 ctx 处于取消态时，必须返回 error，不能把半截内容当正常回答。
//
// 对齐 Python：取消发生在 await 点时抛 CancelledError，不会产出 answer。
func TestCallLLMStream_CanceledMidStream(t *testing.T) {
	a := &ReActAgent{}
	ctx, cancel := context.WithCancel(context.Background())
	model := newFakeLLMModel(t, &fakeStreamClient{chunks: []*llmschema.AssistantMessageChunk{
		llmschema.NewAssistantMessageChunk("半截"),
	}})

	// 先取消，再走一次完整流：模拟"取消发生在流进行中"
	cancel()
	msg, err := a.callLLMStream(ctx, model, "mock-model", nil, nil, nil, nil)
	if err == nil {
		t.Fatalf("取消态下应返回 error，实际返回消息: %+v", msg)
	}
}

// TestCallLLMStream_NotCanceledReturnsMessage 未取消时正常返回累积消息（反向用例）。
func TestCallLLMStream_NotCanceledReturnsMessage(t *testing.T) {
	a := &ReActAgent{}
	model := newFakeLLMModel(t, &fakeStreamClient{chunks: []*llmschema.AssistantMessageChunk{
		llmschema.NewAssistantMessageChunk("完整"),
	}})

	msg, err := a.callLLMStream(context.Background(), model, "mock-model", nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("未取消不应报错: %v", err)
	}
	if msg == nil || msg.Content.Text() != "完整" {
		t.Fatalf("应返回累积内容，实际 %+v", msg)
	}
}

// TestCallLLMStream_流中断上抛 验证中断哨兵 chunk 会终止本轮并返回错误：
// 否则半截内容（尤其是半截 tool_call）会被当成完整回答继续执行。
//
// 对齐 Python：异步生成器在流读取异常时 raise MODEL_CALL_FAILED。
func TestCallLLMStream_流中断上抛(t *testing.T) {
	cause := errors.New("unexpected EOF")
	model := newFakeLLMModel(t, &fakeStreamClient{chunks: []*llmschema.AssistantMessageChunk{
		llmschema.NewAssistantMessageChunk("半截"),
		llmschema.NewAssistantMessageChunk("", llmschema.WithChunkStreamError(cause)),
	}})

	msg, err := (&ReActAgent{}).callLLMStream(context.Background(), model, "mock-model", nil, nil, nil, nil)
	if err == nil {
		t.Fatalf("流中断应返回错误，实际 msg=%+v", msg)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("错误应可 Is 到原始原因, 实际 %v", err)
	}
}

// TestLogLLMResponse_非法工具参数可诊断 验证非法参数分支可执行且不 panic，
// 同时覆盖 nil 入参与带 usage 的响应（日志本身不做断言，靠行级覆盖托底）。
func TestLogLLMResponse_非法工具参数可诊断(t *testing.T) {
	logLLMResponse(nil)

	msg := llmschema.NewAssistantMessage("")
	msg.FinishReason = "length"
	msg.ToolCalls = []*llmschema.ToolCall{
		llmschema.NewToolCall("call-1", "write_file", `{"file_path": "C:\\a.html", "content": "<html>`),
	}
	logLLMResponse(msg)

	usage := &llmschema.UsageMetadata{InputTokens: 10, OutputTokens: 20}
	msg.UsageMetadata = usage
	logLLMResponse(msg)

	valid := llmschema.NewAssistantMessage("")
	valid.ToolCalls = []*llmschema.ToolCall{
		llmschema.NewToolCall("call-2", "read_file", `{"file_path": "C:\\a.html"}`),
	}
	logLLMResponse(valid)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// Invoke 非流式（本用例不用）。
func (f *fakeStreamClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return llmschema.NewAssistantMessage(""), nil
}

// Stream 返回预置 chunk 后关闭通道。
func (f *fakeStreamClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	ch := make(chan *llmschema.AssistantMessageChunk, len(f.chunks))
	for _, c := range f.chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

// GenerateImage 不支持图片生成。
func (f *fakeStreamClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, fmt.Errorf("fakeStreamClient: unsupported")
}

// GenerateSpeech 不支持语音生成。
func (f *fakeStreamClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, fmt.Errorf("fakeStreamClient: unsupported")
}

// GenerateVideo 不支持视频生成。
func (f *fakeStreamClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, fmt.Errorf("fakeStreamClient: unsupported")
}

// TranscribeAudio 不支持转写。
func (f *fakeStreamClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, fmt.Errorf("fakeStreamClient: unsupported")
}

// Release 释放模型资源（no-op）。
func (f *fakeStreamClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return true, nil
}

// SupportsKVCacheRelease 不支持 KV Cache 释放。
func (f *fakeStreamClient) SupportsKVCacheRelease() bool { return false }

// newFakeLLMModel 通过 ClientRegistry 注入 fake client，构造 *llm.Model。
func newFakeLLMModel(t *testing.T, client *fakeStreamClient) *llm.Model {
	t.Helper()
	provider := fmt.Sprintf("fake_cancel_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(provider, "llm",
		func(_ *llmschema.ModelRequestConfig, _ *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
			return client, nil
		})
	t.Cleanup(func() { _ = model_clients.GetClientRegistry().Unregister(provider, "llm") })

	m, err := llm.NewModel(
		&llmschema.ModelClientConfig{ClientProvider: provider, ClientID: "test", APIKey: "k"},
		llmschema.NewModelRequestConfig(),
	)
	if err != nil {
		t.Fatalf("构造 llm.Model 失败: %v", err)
	}
	return m
}
