package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeStreamClient 立即返回一个已关闭的 chunk 通道，模拟"流结束"。
//
// 必须实现完整 BaseModelClient 接口，才能注册进 ClientRegistry。
type fakeStreamClient struct {
	// chunks 预置输出片段
	chunks []*llmschema.AssistantMessageChunk
	// chanOverride 若非 nil，则直接返回该通道（用于断言"报错后是否排空"）
	chanOverride chan *llmschema.AssistantMessageChunk
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
	msg, err := a.callLLMStream(ctx, nil, model, "mock-model", nil, nil, nil, nil)
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

	msg, err := a.callLLMStream(context.Background(), nil, model, "mock-model", nil, nil, nil, nil)
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

	msg, err := (&ReActAgent{}).callLLMStream(context.Background(), nil, model, "mock-model", nil, nil, nil, nil)
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
	if f.chanOverride != nil {
		return f.chanOverride, nil
	}
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

// ──────────────────────────── A-06: 响应错误判定 ────────────────────────────

// TestRaiseForModelResponseError 校验响应错误判定的两段逻辑
func TestRaiseForModelResponseError(t *testing.T) {
	cases := []struct {
		name         string
		finishReason string
		content      string
		toolCalls    int
		usageCode    int
		wantErr      bool
	}{
		{name: "length 且空内容", finishReason: "length", content: "", wantErr: true},
		{name: "length 且空白内容", finishReason: "length", content: "   \n", wantErr: true},
		{name: "max_tokens 且空内容", finishReason: "max_tokens", content: "", wantErr: true},
		{name: "大写 LENGTH 且空内容", finishReason: "LENGTH", content: "", wantErr: true},
		{name: "length 但有正文", finishReason: "length", content: "部分答案", wantErr: false},
		{name: "length 但有工具调用", finishReason: "length", content: "", toolCalls: 1, wantErr: false},
		{name: "正常 stop 空内容", finishReason: "stop", content: "", wantErr: false},
		{name: "provider code 非 0", finishReason: "stop", content: "x", usageCode: 123, wantErr: true},
		{name: "finish_reason=error", finishReason: "error", content: "x", wantErr: true},
		{name: "finish_reason=failed", finishReason: "failed", content: "x", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := llmschema.NewAssistantMessage(tc.content)
			msg.FinishReason = tc.finishReason
			msg.UsageMetadata = &llmschema.UsageMetadata{Code: tc.usageCode}
			for i := 0; i < tc.toolCalls; i++ {
				msg.ToolCalls = append(msg.ToolCalls, &llmschema.ToolCall{ID: fmt.Sprintf("call_%d", i), Name: "read_file"})
			}
			err := raiseForModelResponseError(msg)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望返回错误，实际为 nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("期望成功，实际错误: %v", err)
			}
		})
	}
}

// TestRaiseForModelResponseError_错误文案 验证错误文案与 Python 逐字一致
func TestRaiseForModelResponseError_错误文案(t *testing.T) {
	msg := llmschema.NewAssistantMessage("")
	msg.FinishReason = "length"
	msg.UsageMetadata = &llmschema.UsageMetadata{OutputTokens: 4096}

	err := raiseForModelResponseError(msg)
	if err == nil {
		t.Fatal("期望返回错误")
	}
	assert.Contains(t, err.Error(),
		"model hit its output limit (finish_reason=length, output_tokens=4096) before producing an answer or a tool call")

	providerMsg := llmschema.NewAssistantMessage("boom")
	providerMsg.FinishReason = "error"
	err = raiseForModelResponseError(providerMsg)
	if err == nil {
		t.Fatal("期望返回 provider 错误")
	}
	assert.Contains(t, err.Error(), "provider response error: code=0, finish_reason=error, message=boom")
}

// TestRaiseForModelResponseError_结构化详情 验证结构化字段落在 details 而非 params。
//
// Python: raise_error(..., details={...}) (react_agent.py L2039/L2052) —— details 是
// rail 与前端可见的结构化详情容器，params 只参与模板消息占位符渲染，两者语义不同。
func TestRaiseForModelResponseError_结构化详情(t *testing.T) {
	// 输出上限命中：details 携带 finish_reason 与 output_tokens
	limitMsg := llmschema.NewAssistantMessage("")
	limitMsg.FinishReason = "length"
	limitMsg.UsageMetadata = &llmschema.UsageMetadata{OutputTokens: 4096}
	err := raiseForModelResponseError(limitMsg)
	be := &exception.BaseError{}
	if !errors.As(err, &be) {
		t.Fatalf("期望 *exception.BaseError，实际 %T", err)
	}
	limitDetails, ok := be.Details().(map[string]any)
	if !ok {
		t.Fatalf("details 期望 map[string]any，实际 %T", be.Details())
	}
	assert.Equal(t, "length", limitDetails["finish_reason"])
	assert.Equal(t, 4096, limitDetails["output_tokens"])

	// provider 错误：details 携带 provider_code / finish_reason / provider_message
	providerMsg := llmschema.NewAssistantMessage("boom")
	providerMsg.FinishReason = "error"
	err = raiseForModelResponseError(providerMsg)
	be = &exception.BaseError{}
	if !errors.As(err, &be) {
		t.Fatalf("期望 *exception.BaseError，实际 %T", err)
	}
	providerDetails, ok := be.Details().(map[string]any)
	if !ok {
		t.Fatalf("details 期望 map[string]any，实际 %T", be.Details())
	}
	assert.Equal(t, 0, providerDetails["provider_code"])
	assert.Equal(t, "error", providerDetails["finish_reason"])
	assert.Equal(t, "boom", providerDetails["provider_message"])
}

// TestRaiseForModelResponseError_中文错误文案按字符截断 验证 500 上限按字符计数。
//
// 上游 error_message[:500] 切的是字符；Go 按字节切会切断多字节字符产出乱码。
func TestRaiseForModelResponseError_中文错误文案按字符截断(t *testing.T) {
	// "模型调用超限" 是 6 个字符，重复 100 次 = 600 个字符，超过 500 上限
	input := strings.Repeat("模型调用超限", 100)
	msg := llmschema.NewAssistantMessage(input)
	msg.FinishReason = "failed"
	err := raiseForModelResponseError(msg)
	be := &exception.BaseError{}
	if !errors.As(err, &be) {
		t.Fatalf("期望 *exception.BaseError，实际 %T", err)
	}
	details, ok := be.Details().(map[string]any)
	if !ok {
		t.Fatalf("details 期望 map[string]any，实际 %T", be.Details())
	}
	shown, ok := details["provider_message"].(string)
	if !ok {
		t.Fatalf("provider_message 期望 string，实际 %T", details["provider_message"])
	}
	assert.Equal(t, 500, utf8.RuneCountInString(shown))
	assert.True(t, utf8.ValidString(shown), "截断结果必须是合法 UTF-8")
	// 前 500 个字符原样保留（不是前 500 字节，也不是补省略号）
	assert.Equal(t, string([]rune(input)[:500]), shown)
}

// ──────────────────────────── A-08-2: 拒图识别与降级 ────────────────────────────

// TestIsImageInputUnsupportedError 验证 6 个 code 与 14 条 pattern 的识别
func TestIsImageInputUnsupportedError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"文案 pattern", errors.New("OpenAI API HTTP 400: image input is not supported"), true},
		{"否定式 pattern", errors.New("the model does not support image"), true},
		{"结构化 code", exception.NewBaseError(exception.StatusModelCallFailed,
			exception.WithMsg("OpenAI API HTTP 400: bad request"),
			exception.WithParams(map[string]any{"provider_code": "invalid_image_input"})), true},
		{"code 后缀匹配", errors.New("error_provider_invalid_image_input"), true},
		{"openrouter 文案", errors.New("no endpoints found that support image input"), true},
		{"无关错误", errors.New("rate limit exceeded"), false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isImageInputUnsupportedError(tc.err); got != tc.want {
				t.Fatalf("isImageInputUnsupportedError=%v, want %v", got, tc.want)
			}
		})
	}
}

// TestMessagesContainImageInput 验证图片内容块识别
func TestMessagesContainImageInput(t *testing.T) {
	textOnly := []llmschema.BaseMessage{llmschema.NewUserMessage("hi")}
	if messagesContainImageInput(textOnly) {
		t.Fatal("纯文本消息不应判定含图片")
	}
	withImage := []llmschema.BaseMessage{llmschema.NewUserMessage("", llmschema.WithMultiModalContent(
		llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "data:image/png;base64,xx"}},
	))}
	if !messagesContainImageInput(withImage) {
		t.Fatal("含 image_url 的消息应判定含图片")
	}
}

// TestBuildImageInputUnsupportedMessage 验证降级提示文案
func TestBuildImageInputUnsupportedMessage(t *testing.T) {
	msg := buildImageInputUnsupportedMessage()
	assert.Contains(t, msg.Content.Text(), "[Vision Input Unsupported]")
	assert.Contains(t, msg.Content.Text(), "当前主模型或模型服务端点不支持图片输入")
	assert.Contains(t, msg.Content.Text(), "pypdf")
}

// TestExtractExceptionErrorValues_嵌套错误体 验证嵌套 map/slice 里的 code 能被抽出来
//
// 上游 _iter_exception_error_values 对 dict/list 递归（深度 ≤ 8），provider 的
// 错误体常是 {"error": {"code": "invalid_image_input"}} 这种嵌套结构。
func TestExtractExceptionErrorValues_嵌套错误体(t *testing.T) {
	err := exception.NewBaseError(exception.StatusModelCallFailed,
		exception.WithMsg("OpenAI API HTTP 400: bad request"),
		exception.WithDetails(map[string]any{
			"error": map[string]any{
				"code":    "invalid_image_input",
				"message": "this model does not accept images",
			},
		}))

	values := extractExceptionErrorValues(err)
	joined := strings.Join(values, "\n")
	if !strings.Contains(joined, "invalid_image_input") {
		t.Fatalf("嵌套 code 应被抽出，实际值: %v", values)
	}
	if !isImageInputUnsupportedError(err) {
		t.Fatal("嵌套错误体里的 code 应被识别为拒图")
	}
}

// TestExtractExceptionErrorValues_深度上限 验证超过 8 层的嵌套不再展开
func TestExtractExceptionErrorValues_深度上限(t *testing.T) {
	var nested any = "deep_marker"
	for i := 0; i < imageInputScanMaxDepth+3; i++ {
		nested = map[string]any{"child": nested}
	}
	values := iterExceptionErrorValues(nested, 0)
	for _, v := range values {
		if strings.Contains(v, "deep_marker") {
			t.Fatal("超过深度上限的值不应被展开")
		}
	}
}

// TestMessagesContainImageInput_宽口径 验证 type=image 与空 URL 的图片块同样命中
//
// 上游只认 "image_url"/"image" 键存在，不看 URL 是否为空。
func TestMessagesContainImageInput_宽口径(t *testing.T) {
	cases := []struct {
		name string
		part llmschema.ContentPart
		want bool
	}{
		{"type=image_url 且 URL 非空", llmschema.ContentPart{
			Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "data:image/png;base64,xx"}}, true},
		{"type=image_url 但 URL 为空", llmschema.ContentPart{
			Type: "image_url", ImageURL: &llmschema.ImageURL{}}, true},
		{"type=image", llmschema.ContentPart{Type: "image"}, true},
		{"纯文本块", llmschema.ContentPart{Type: "text", Text: "hi"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			msgs := []llmschema.BaseMessage{llmschema.NewUserMessage("", llmschema.WithMultiModalContent(c.part))}
			if got := messagesContainImageInput(msgs); got != c.want {
				t.Fatalf("messagesContainImageInput=%v, want %v", got, c.want)
			}
		})
	}
}

// TestIterExceptionErrorValues_所有分支 覆盖 string/error/嵌套容器/标量/超深
func TestIterExceptionErrorValues_所有分支(t *testing.T) {
	cases := []struct {
		name     string
		value    any
		wantLen  int
		contains string
	}{
		{"字符串", "invalid_image_input", 1, "invalid_image_input"},
		{"空字符串", "", 0, ""},
		{"错误值", errors.New("boom"), 1, "boom"},
		{"嵌套 map", map[string]any{"a": map[string]any{"b": "deep"}}, 1, "deep"},
		{"[]any", []any{"one", "two"}, 2, "two"},
		{"[]string", []string{"x", ""}, 1, "x"},
		{"标量", 400, 1, "400"},
		{"nil", nil, 0, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := iterExceptionErrorValues(c.value, 0)
			if len(got) != c.wantLen {
				t.Fatalf("got %v（长度 %d），want 长度 %d", got, len(got), c.wantLen)
			}
			if c.contains != "" {
				joined := strings.Join(got, "|")
				if !strings.Contains(joined, c.contains) {
					t.Fatalf("应包含 %q，实际 %v", c.contains, got)
				}
			}
		})
	}
}

// TestExtractExceptionErrorValues_Params与错误链 覆盖 Params 展开与 Unwrap 链
func TestExtractExceptionErrorValues_Params与错误链(t *testing.T) {
	inner := errors.New("inner: unsupported image")
	wrapped := fmt.Errorf("wrap: %w", inner)
	values := extractExceptionErrorValues(wrapped)
	joined := strings.Join(values, "\n")
	if !strings.Contains(joined, "wrap:") || !strings.Contains(joined, "unsupported image") {
		t.Fatalf("错误链两侧文本都应被抽出，实际 %v", values)
	}

	withParams := exception.NewBaseError(exception.StatusModelCallFailed,
		exception.WithMsg("bad request"),
		exception.WithParams(map[string]any{"provider_code": "image_input_unsupported"}))
	joined = strings.Join(extractExceptionErrorValues(withParams), "\n")
	if !strings.Contains(joined, "image_input_unsupported") {
		t.Fatalf("Params 值应被抽出，实际 %v", joined)
	}
	if !isImageInputUnsupportedError(withParams) {
		t.Fatal("Params 里的结构化 code 应被识别为拒图")
	}
}

// ──────────────────────────── 流式分片检查器 ────────────────────────────

// TestCallLLMStream_检查器逐chunk调用 验证检查器按分片逐个触发。
func TestCallLLMStream_检查器逐chunk调用(t *testing.T) {
	a := &ReActAgent{}
	client := &fakeStreamClient{chunks: []*llmschema.AssistantMessageChunk{
		llmschema.NewAssistantMessageChunk("a"),
		llmschema.NewAssistantMessageChunk("b"),
	}}
	model := newFakeLLMModel(t, client)
	cbc := interfaces.NewAgentCallbackContext(nil, nil, nil)

	seen := 0
	cbc.SetStreamChunkInspectors([]interfaces.StreamChunkInspectorFunc{
		func(_ context.Context, _ *interfaces.AgentCallbackContext, chunk *llmschema.AssistantMessageChunk) error {
			seen++
			if chunk.Content.Text() == "" {
				t.Errorf("检查器收到的 chunk 内容为空")
			}
			return nil
		},
	})

	if _, err := a.callLLMStream(context.Background(), cbc, model, "mock-model", nil, nil, nil, nil); err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if seen != 2 {
		t.Fatalf("检查器应被调用 2 次，实际 %d", seen)
	}
}

// TestCallLLMStream_检查器报错先排空再返回 验证检查器命中后必须排空 channel，
// 否则客户端 goroutine 会卡在 send 上、HTTP 连接不关。
func TestCallLLMStream_检查器报错先排空再返回(t *testing.T) {
	a := &ReActAgent{}
	ch := make(chan *llmschema.AssistantMessageChunk, 3)
	ch <- llmschema.NewAssistantMessageChunk("a")
	ch <- llmschema.NewAssistantMessageChunk("b")
	ch <- llmschema.NewAssistantMessageChunk("c")
	close(ch)
	client := &fakeStreamClient{chanOverride: ch}
	model := newFakeLLMModel(t, client)

	cbc := interfaces.NewAgentCallbackContext(nil, nil, nil)
	boom := errors.New("repeat detected")
	cbc.SetStreamChunkInspectors([]interfaces.StreamChunkInspectorFunc{
		func(_ context.Context, _ *interfaces.AgentCallbackContext, _ *llmschema.AssistantMessageChunk) error {
			return boom
		},
	})

	_, err := a.callLLMStream(context.Background(), cbc, model, "mock-model", nil, nil, nil, nil)
	if !errors.Is(err, boom) {
		t.Fatalf("应透传检查器错误，实际 %v", err)
	}
	if rest := len(ch); rest != 0 {
		t.Fatalf("报错后必须排空 channel，剩余 %d 个 chunk", rest)
	}
}

// TestCallLLMStream_流式输出置可见标志 验证每次流式调用复位、写出内容后置位。
func TestCallLLMStream_流式输出置可见标志(t *testing.T) {
	a := &ReActAgent{}
	model := newFakeLLMModel(t, &fakeStreamClient{chunks: []*llmschema.AssistantMessageChunk{
		llmschema.NewAssistantMessageChunk("hello"),
	}})
	cbc := interfaces.NewAgentCallbackContext(nil, nil, nil)
	cbc.SetModelVisibleOutputEmitted(true) // 模拟上一轮的残留

	atChunk := false
	cbc.SetStreamChunkInspectors([]interfaces.StreamChunkInspectorFunc{
		func(_ context.Context, c *interfaces.AgentCallbackContext, _ *llmschema.AssistantMessageChunk) error {
			atChunk = c.ModelVisibleOutputEmitted()
			return nil
		},
	})

	if _, err := a.callLLMStream(context.Background(), cbc, model, "mock-model", nil, nil, nil, nil); err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if atChunk {
		t.Fatal("每次流式调用开始时必须把可见输出标志复位为 false")
	}
	if !cbc.ModelVisibleOutputEmitted() {
		t.Fatal("写出可见内容后标志应为 true")
	}
}
