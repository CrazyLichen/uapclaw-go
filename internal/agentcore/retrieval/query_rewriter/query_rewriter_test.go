package query_rewriter

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// stubQRClient 惰性 mock 客户端（13.17 stubTripleClient 先例）。
// 按 messages 首条 SystemMessage 内容分派：rewrite 提示词（含
// "当前用户输入"或 "Current user input"）返回 rewriteResp，否则返回
// compressResp；invokeErr 非空时返回错误。所有字段并发安全（mu 保护）。
type stubQRClient struct {
	mu           sync.Mutex
	rewriteResp  string
	compressResp string
	invokeErr    error
	compressCnt  int
	rewriteCnt   int
}

// Invoke 按 Python mock_invoke 的 last_content 判据分派响应。
func (s *stubQRClient) Invoke(_ context.Context, messages model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	content := ""
	if msgs := messages.Messages(); len(msgs) > 0 {
		content = msgs[0].GetContent().Text()
	}
	if s.invokeErr != nil {
		return nil, s.invokeErr
	}
	if strings.Contains(content, "当前用户输入") || strings.Contains(content, "Current user input") {
		s.rewriteCnt++
		return llmschema.NewAssistantMessage(s.rewriteResp), nil
	}
	s.compressCnt++
	return llmschema.NewAssistantMessage(s.compressResp), nil
}

// Stream 流式调用薄桩。
func (s *stubQRClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, errors.New("not implemented")
}

// GenerateImage 图像生成薄桩。
func (s *stubQRClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, errors.New("not implemented")
}

// GenerateSpeech 语音生成薄桩。
func (s *stubQRClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, errors.New("not implemented")
}

// GenerateVideo 视频生成薄桩。
func (s *stubQRClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, errors.New("not implemented")
}

// TranscribeAudio 语音转写薄桩。
func (s *stubQRClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, errors.New("not implemented")
}

// Release KV 缓存释放薄桩。
func (s *stubQRClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}

// SupportsKVCacheRelease KV 缓存释放能力薄桩。
func (s *stubQRClient) SupportsKVCacheRelease() bool { return false }

// fakeModelContext 测试用 ModelContext（GetMessages/SetMessages/Len/AddMessages 真逻辑，
// 其余薄桩；react_agent_test.go 先例）。
type fakeModelContext struct {
	messages []llmschema.BaseMessage
}

// Len 返回消息数。
func (f *fakeModelContext) Len() int { return len(f.messages) }

// GetMessages 取尾部 size 条（size<=0 全量），对齐 SessionModelContext.GetBack 语义。
func (f *fakeModelContext) GetMessages(size int, _ bool) ([]llmschema.BaseMessage, error) {
	if size <= 0 || size > len(f.messages) {
		return append([]llmschema.BaseMessage{}, f.messages...), nil
	}
	return append([]llmschema.BaseMessage{}, f.messages[len(f.messages)-size:]...), nil
}

// SetMessages 整体替换。
func (f *fakeModelContext) SetMessages(msgs []llmschema.BaseMessage, _ bool) {
	f.messages = append([]llmschema.BaseMessage{}, msgs...)
}

// PopMessages 从尾部弹出。
func (f *fakeModelContext) PopMessages(size int, _ bool) []llmschema.BaseMessage {
	if size <= 0 || size > len(f.messages) {
		return nil
	}
	popped := append([]llmschema.BaseMessage{}, f.messages[len(f.messages)-size:]...)
	f.messages = f.messages[:len(f.messages)-size]
	return popped
}

// ClearMessages 清空薄桩。
func (f *fakeModelContext) ClearMessages(_ context.Context, _ bool, _ ...ceinterface.Option) error {
	f.messages = nil
	return nil
}

// AddMessages 追加消息。
func (f *fakeModelContext) AddMessages(_ context.Context, msgs []llmschema.BaseMessage, _ ...ceinterface.Option) ([]llmschema.BaseMessage, error) {
	f.messages = append(f.messages, msgs...)
	return msgs, nil
}

// GetContextWindow 构建窗口薄桩。
func (f *fakeModelContext) GetContextWindow(_ context.Context, sys []llmschema.BaseMessage, _ []cschema.ToolInfoInterface, _, _ int, _ ...ceinterface.Option) (*ceinterface.ContextWindow, error) {
	return &ceinterface.ContextWindow{SystemMessages: sys, ContextMessages: append([]llmschema.BaseMessage{}, f.messages...)}, nil
}

// Statistic 统计薄桩。
func (f *fakeModelContext) Statistic() *ceinterface.ContextStats { return &ceinterface.ContextStats{} }

// SessionID 会话 ID 桩。
func (f *fakeModelContext) SessionID() string { return "qr_test_session" }

// ContextID 上下文 ID 桩。
func (f *fakeModelContext) ContextID() string { return "qr_test_context" }

// TokenCounter token 计数器薄桩。
func (f *fakeModelContext) TokenCounter() token.TokenCounter { return nil }

// ReloaderTool 重载工具薄桩。
func (f *fakeModelContext) ReloaderTool() tool.Tool { return nil }

// WorkspaceDir 工作目录薄桩。
func (f *fakeModelContext) WorkspaceDir() string { return "" }

// SetSessionRef 设置会话引用薄桩。
func (f *fakeModelContext) SetSessionRef(_ interfaces.SessionFacade) {}

// GetSessionRef 获取会话引用薄桩。
func (f *fakeModelContext) GetSessionRef() interfaces.SessionFacade { return nil }

// OffloadMessages 卸载消息薄桩。
func (f *fakeModelContext) OffloadMessages(_ string, _ []llmschema.BaseMessage) {}

// SaveState 保存状态薄桩。
func (f *fakeModelContext) SaveState() map[string]any { return nil }

// LoadState 恢复状态薄桩。
func (f *fakeModelContext) LoadState(_ map[string]any) {}

// CompressContext 主动压缩薄桩。
func (f *fakeModelContext) CompressContext(_ context.Context, _ ...ceinterface.CompressContextOption) (*ceinterface.CompressContextResult, error) {
	return nil, errors.New("not implemented")
}

// newQRTestRewriter 构造带 stub 的 QueryRewriter（WithModelClientOption 注入）。
func newQRTestRewriter(t *testing.T, stub *stubQRClient, fmc *fakeModelContext, opts ...QueryRewriterOption) *QueryRewriter {
	t.Helper()
	clientCfg, err := llmschema.NewModelClientConfig("openai", "test-key", "https://mock.test", llmschema.WithVerifySSL(false))
	assert.NoError(t, err)
	model, err := llm.NewModel(clientCfg, llmschema.NewModelRequestConfig(), llm.WithClient(stub))
	assert.NoError(t, err)
	opts = append([]QueryRewriterOption{WithModelClientOption(model)}, opts...)
	qr, err := NewQueryRewriter(testModelConfig(), fmc, opts...)
	assert.NoError(t, err)
	return qr
}

// testModelConfig 构造测试 ModelConfig（verify_ssl 走 Extra 同生产路径）。
func testModelConfig() *llmschema.ModelConfig {
	mi := llmschema.NewBaseModelInfo("https://mock.test",
		llmschema.WithAPIKey("test-key"),
		llmschema.WithModelNameForInfo("gpt-4"),
		llmschema.WithTemperatureForInfo(0.0),
		llmschema.WithTopPForInfo(0.1),
		llmschema.WithTimeoutForInfo(60),
		llmschema.WithInfoExtra(map[string]any{"verify_ssl": false}),
	)
	return llmschema.NewModelConfig("OpenAI", *mi)
}

// makeCompressResponse 构造合法压缩响应（Python _make_compress_response）。
func makeCompressResponse() string {
	return `{"theme": ["主题"], "summary": "摘要内容"}`
}

// makeFullRewriteResponse 构造合法改写响应（Python _make_full_rewrite_response）。
func makeFullRewriteResponse(standaloneQuery string) string {
	return fmt.Sprintf(`{
		"before": %q, "intention": "用户咨询", "standalone_query": %q,
		"references": {}, "missing": [], "typo": [], "gibberish": [],
		"from_history": ""}`, standaloneQuery, standaloneQuery)
}

// TestLoadTemplate_加载现有模板（Python test_load_existing_template）
func TestLoadTemplate_加载现有模板(t *testing.T) {
	qr := newQRTestRewriter(t, &stubQRClient{}, &fakeModelContext{})
	content, err := qr.LoadTemplate("intention_completion")
	assert.NoError(t, err)
	assert.True(t, strings.Contains(content, "standalone_query") || strings.Contains(content, "角色"))
}

// TestLoadTemplate_二次调用走缓存（Python test_load_template_cached_on_second_call）
func TestLoadTemplate_二次调用走缓存(t *testing.T) {
	qr := newQRTestRewriter(t, &stubQRClient{}, &fakeModelContext{})
	first, err := qr.LoadTemplate("intention_completion")
	assert.NoError(t, err)
	second, err := qr.LoadTemplate("intention_completion")
	assert.NoError(t, err)
	assert.Equal(t, first, second)
}

// TestLoadTemplate_语言不存在 → 155603（Python test_prompt_not_found_raises）
func TestLoadTemplate_语言不存在(t *testing.T) {
	qr := newQRTestRewriter(t, &stubQRClient{}, &fakeModelContext{}, WithPromptLang("nonexistent_lang"))
	_, err := qr.LoadTemplate("intention_completion")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155603")
}

// TestMsg2Text_显式消息（Python test_msg_2_text_with_messages）
func TestMsg2Text_显式消息(t *testing.T) {
	qr := newQRTestRewriter(t, &stubQRClient{}, &fakeModelContext{})
	messages := []llmschema.BaseMessage{
		llmschema.NewUserMessage("今天天气如何？"),
		llmschema.NewAssistantMessage("晴天。"),
	}
	text, err := qr.Msg2Text(messages)
	assert.NoError(t, err)
	assert.Contains(t, text, "user: 今天天气如何？")
	assert.Contains(t, text, "assistant: 晴天。")
}

// TestMsg2Text_空消息读上下文（Python test_msg_2_text_from_context_when_none）
func TestMsg2Text_空消息读上下文(t *testing.T) {
	fmc := &fakeModelContext{}
	_, err := fmc.AddMessages(context.Background(), []llmschema.BaseMessage{
		llmschema.NewUserMessage("你好"), llmschema.NewAssistantMessage("你好！"),
	})
	assert.NoError(t, err)
	qr := newQRTestRewriter(t, &stubQRClient{}, fmc)
	text, err := qr.Msg2Text(nil)
	assert.NoError(t, err)
	assert.Contains(t, text, "user: 你好")
	assert.Contains(t, text, "assistant: 你好！")
}
