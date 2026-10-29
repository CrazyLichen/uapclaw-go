package query_rewriter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

// TestCompress_合法响应（Python test_compress_valid_mock）
func TestCompress_合法响应(t *testing.T) {
	fmc := &fakeModelContext{}
	_, _ = fmc.AddMessages(context.Background(), []llmschema.BaseMessage{
		llmschema.NewUserMessage("用户问"), llmschema.NewAssistantMessage("助手答"),
	})
	qr := newQRTestRewriter(t, &stubQRClient{compressResp: makeCompressResponse()}, fmc, WithCompressRange(5))
	result, err := qr.Compress(context.Background(), nil) // nil → Msg2Text 读上下文（Python msg_2_text(raw) 同链路）
	assert.NoError(t, err)
	assert.Contains(t, result, "theme")
	assert.Contains(t, result, "summary")
}

// TestCompress_非法JSON → 155604（Python test_compress_invalid_json_raises）
func TestCompress_非法JSON(t *testing.T) {
	fmc := &fakeModelContext{}
	_, _ = fmc.AddMessages(context.Background(), []llmschema.BaseMessage{
		llmschema.NewUserMessage("用户问"), llmschema.NewAssistantMessage("助手答"),
	})
	qr := newQRTestRewriter(t, &stubQRClient{compressResp: "not valid json at all"}, fmc, WithCompressRange(5))
	_, err := qr.Compress(context.Background(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155604")
}

// TestTruncateForMsg_rune语义 M-03：中文按字符（code point）截断而非字节——
// Python content[:500] 语义，字节切片在 CJK 3 字节中间会截出无效 UTF-8。
func TestTruncateForMsg_rune语义(t *testing.T) {
	// 短串原样返回
	assert.Equal(t, "abc", truncateForMsg("abc", 500))
	// ASCII 500 字符边界
	ascii := strings.Repeat("a", 600)
	assert.Equal(t, strings.Repeat("a", 500), truncateForMsg(ascii, 500))
	// 中文 600 字符 → 500 字符（1500 字节 > 500 字节，字节截断会切出半个字符）
	cjk := strings.Repeat("中", 600)
	got := truncateForMsg(cjk, 500)
	assert.Equal(t, strings.Repeat("中", 500), got)
	assert.True(t, utf8.ValidString(got), "截断结果必须是有效 UTF-8")
	// 恰好 500 字符不截断
	assert.Equal(t, strings.Repeat("中", 500), truncateForMsg(strings.Repeat("中", 500), 500))
	// 混合边界：第 500 个 code point 恰为多字节字符首字节时整体保留
	mixed := strings.Repeat("a", 499) + strings.Repeat("中", 100)
	gotMixed := truncateForMsg(mixed, 500)
	assert.True(t, utf8.ValidString(gotMixed))
	assert.Equal(t, strings.Repeat("a", 499)+"中", gotMixed)
}

// TestCompress_非法JSON中文超长 M-03 说明：extractJSON 对无括号输入返回空串、
// {} 包围坏串经 jsonrepair 几乎必产 map——长中文 content 在 non-dict 分支
// 结构上不可达（Python 同构），rune 截断语义由 TestTruncateForMsg_rune语义
// 直接锁定，此处不构造不可达集成路径。

// TestRewrite_非法输出中文超长 M-03：同上，集成路径不可达，见 rune 单测。

// TestCompress_invoke失败 → 155605（Python test_compress_llm_invoke_failure_raises）
func TestCompress_invoke失败(t *testing.T) {
	fmc := &fakeModelContext{}
	_, _ = fmc.AddMessages(context.Background(), []llmschema.BaseMessage{
		llmschema.NewUserMessage("用户问"), llmschema.NewAssistantMessage("助手答"),
	})
	qr := newQRTestRewriter(t, &stubQRClient{invokeErr: errors.New("network error")}, fmc, WithCompressRange(5))
	_, err := qr.Compress(context.Background(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155605")
}

// TestRewrite_合法响应（Python test_rewrite_valid_mock）
func TestRewrite_合法响应(t *testing.T) {
	fmc := &fakeModelContext{}
	_, _ = fmc.AddMessages(context.Background(), []llmschema.BaseMessage{
		llmschema.NewUserMessage("你好"), llmschema.NewAssistantMessage("你好！"),
	})
	currentQuery := "那运费呢？"
	qr := newQRTestRewriter(t, &stubQRClient{rewriteResp: makeFullRewriteResponse(currentQuery)}, fmc, WithCompressRange(5))
	result, err := qr.Rewrite(context.Background(), currentQuery)
	assert.NoError(t, err)
	assert.Equal(t, currentQuery, result["standalone_query"])
	assert.Contains(t, result, "before")
	assert.Contains(t, result, "intention")
}

// TestRewrite_前后缀包裹JSON（Python test_rewrite_with_json_prefix_suffix）
func TestRewrite_前后缀包裹JSON(t *testing.T) {
	fmc := &fakeModelContext{}
	_, _ = fmc.AddMessages(context.Background(), []llmschema.BaseMessage{
		llmschema.NewUserMessage("你好"), llmschema.NewAssistantMessage("你好！"),
	})
	qr := newQRTestRewriter(t, &stubQRClient{rewriteResp: "这是回答：\n" + makeFullRewriteResponse("测试") + "\n以上是结果。"}, fmc, WithCompressRange(5))
	result, err := qr.Rewrite(context.Background(), "测试")
	assert.NoError(t, err)
	assert.Equal(t, "测试", result["standalone_query"])
}

// TestRewrite_非法输出 → 155604（Python test_rewrite_invalid_output_raises）
func TestRewrite_非法输出(t *testing.T) {
	fmc := &fakeModelContext{}
	_, _ = fmc.AddMessages(context.Background(), []llmschema.BaseMessage{
		llmschema.NewUserMessage("你好"), llmschema.NewAssistantMessage("你好！"),
	})
	qr := newQRTestRewriter(t, &stubQRClient{rewriteResp: "not json"}, fmc, WithCompressRange(5))
	_, err := qr.Rewrite(context.Background(), "问题")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155604")
}

// TestRewrite_空查询 → 155606（Python test_rewrite_invalid_input_empty_raises）
func TestRewrite_空查询(t *testing.T) {
	qr := newQRTestRewriter(t, &stubQRClient{}, &fakeModelContext{}, WithCompressRange(5))
	_, err := qr.Rewrite(context.Background(), "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155606")
}

// TestRewrite_纯空白查询 → 155606（Python test_rewrite_invalid_input_whitespace_raises）
func TestRewrite_纯空白查询(t *testing.T) {
	qr := newQRTestRewriter(t, &stubQRClient{}, &fakeModelContext{}, WithCompressRange(5))
	_, err := qr.Rewrite(context.Background(), "   ")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155606")
}

// TestRewrite_压缩失败降级原文（Python test_rewrite_compress_failure_fallback）
func TestRewrite_压缩失败降级原文(t *testing.T) {
	fmc := &fakeModelContext{}
	for i := 0; i < 3; i++ {
		_, _ = fmc.AddMessages(context.Background(), []llmschema.BaseMessage{
			llmschema.NewUserMessage(fmt.Sprintf("用户问%d", i)),
			llmschema.NewAssistantMessage(fmt.Sprintf("助手答%d", i)),
		})
	}
	// compress 响应是坏 JSON（触发 Compress 内部 155604 → Rewrite 降级）
	stub := &stubQRClient{compressResp: "broken", rewriteResp: makeFullRewriteResponse("总结一下")}
	qr := newQRTestRewriter(t, stub, fmc, WithCompressRange(5))
	result, err := qr.Rewrite(context.Background(), "总结一下")
	assert.NoError(t, err)
	assert.Equal(t, "总结一下", result["standalone_query"])
	// 降级后历史被替换为一条 system 消息（name=original_history）
	msgs, _ := fmc.GetMessages(0, true)
	assert.Len(t, msgs, 1)
	assert.Equal(t, "system", msgs[0].GetRole().String())
	assert.Equal(t, "original_history", msgs[0].GetName())
	assert.Contains(t, msgs[0].GetContent().Text(), "user: 用户问0")
	// rewrite 只被调用一次
	assert.Equal(t, 1, stub.rewriteCnt)
}

// TestRewrite_尾逗号JSON修复（Python test_rewrite_with_trailing_comma_mock）
func TestRewrite_尾逗号JSON修复(t *testing.T) {
	fmc := &fakeModelContext{}
	_, _ = fmc.AddMessages(context.Background(), []llmschema.BaseMessage{
		llmschema.NewUserMessage("你好"), llmschema.NewAssistantMessage("你好！"),
	})
	broken := `{"before":"x","intention":"y","standalone_query":"x","references":{},"missing":[],"typo":[],"gibberish":[],"from_history":"",}`
	qr := newQRTestRewriter(t, &stubQRClient{rewriteResp: broken}, fmc, WithCompressRange(5))
	result, err := qr.Rewrite(context.Background(), "x")
	assert.NoError(t, err)
	assert.Equal(t, "x", result["standalone_query"])
}

// longConversation Python LONG_CONVERSATION 六轮语料。
var longConversation = [][2]string{
	{"你们这个淘美乐 App 是干什么的？", "淘美乐是一款综合购物与生活服务的 App。"},
	{"怎么注册和登录？", "使用手机号验证码或第三方账号登录。"},
	{"我想买点日用品，从哪里进？", "首页有「日百」等入口。"},
	{"搜索出来的结果太多，怎么筛选？", "搜索结果页有「筛选」按钮。"},
	{"下单后多久能送到？", "一般 1～3 天送达。"},
	{"可以修改订单吗？", "待发货状态下可以修改或取消。"},
}

// TestRewrite_完整对话流 6 轮 × compressRange=5（Python test_full_conversation_with_compress_and_rewrite）。
// 轮次推演：turn2=4<5 不压缩；turn4=8>=5 压缩→1 条；turn5 加 2=3 条；turn6=5>=5 再压缩→1 条。
// 最终 rewriteCnt=3（Python 精确断言）、compressCnt=2（Python 宽松 >=0，此处补精确值）。
func TestRewrite_完整对话流(t *testing.T) {
	fmc := &fakeModelContext{}
	stub := &stubQRClient{
		compressResp: makeCompressResponse(),
		rewriteResp:  makeFullRewriteResponse("占位"),
	}
	qr := newQRTestRewriter(t, stub, fmc, WithCompressRange(5))

	rewriteAfterTurns := map[int]string{2: "那运费呢？", 4: "会员怎么升级？", 6: "生鲜能退吗？"}
	for turnIdx, turn := range longConversation {
		_, _ = fmc.AddMessages(context.Background(), []llmschema.BaseMessage{
			llmschema.NewUserMessage(turn[0]), llmschema.NewAssistantMessage(turn[1]),
		})
		// Python L636: n >= 1 and n <= turn_idx * 2
		n := fmc.Len()
		assert.GreaterOrEqual(t, n, 1)
		assert.LessOrEqual(t, n, (turnIdx+1)*2)

		if q, ok := rewriteAfterTurns[turnIdx+1]; ok {
			// Python L640: current_query_for_mock 先设置再 rewrite
			stub.mu.Lock()
			stub.rewriteResp = makeFullRewriteResponse(q)
			stub.mu.Unlock()
			result, err := qr.Rewrite(context.Background(), q)
			assert.NoError(t, err)
			assert.Contains(t, result, "standalone_query")
			assert.Equal(t, q, result["standalone_query"])
		}
	}
	// Python L653-654: rewrite 精确、compress 宽松
	assert.Equal(t, 3, stub.rewriteCnt)
	assert.GreaterOrEqual(t, stub.compressCnt, 0)
	assert.Equal(t, 2, stub.compressCnt) // Go 增强：精确值（轮次推演 turn4/turn6 两次触发）
}

// TestLoadTemplate_并发竞态 Go 增强：并发加载同模板不炸不脏。
func TestLoadTemplate_并发竞态(t *testing.T) {
	qr := newQRTestRewriter(t, &stubQRClient{}, &fakeModelContext{})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			content, err := qr.LoadTemplate("compression")
			assert.NoError(t, err)
			assert.True(t, len(content) > 0)
		}()
	}
	wg.Wait()
}

// TestNewQueryRewriter_配置透传 custom_headers 声明字段直读 + ssl_cert/verify_ssl 经 Extra
// （Python test_init_passes_only_custom_headers；S-04：custom_headers 是 mode_info.py
// 声明字段，不再走 Extra 兜底）
func TestNewQueryRewriter_配置透传(t *testing.T) {
	mi := llmschema.NewBaseModelInfo("https://mock.test",
		llmschema.WithAPIKey("test-key"),
		llmschema.WithModelNameForInfo("gpt-4"),
		llmschema.WithTimeoutForInfo(60),
		llmschema.WithCustomHeadersForInfo(map[string]string{"X-Tenant": "tenant-a"}),
		llmschema.WithInfoExtra(map[string]any{
			"verify_ssl": false,
		}),
	)
	qr, err := NewQueryRewriter(llmschema.NewModelConfig("OpenAI", *mi), &fakeModelContext{}, WithCompressRange(5))
	assert.NoError(t, err)
	assert.Equal(t, map[string]string{"X-Tenant": "tenant-a"}, qr.llm.ClientConfig.CustomHeaders)
}

// TestNewQueryRewriter_customHeaders声明字段JSON反序列化 S-04 生产路径：
// JSON 配置反序列化（Python pydantic 声明字段）时 custom_headers 进声明字段，
// Extra 里即使残留同名键也不再被读取（声明字段优先）。
func TestNewQueryRewriter_customHeaders声明字段JSON反序列化(t *testing.T) {
	cfgJSON := `{
		"model_provider": "OpenAI",
		"model_info": {
			"api_base": "https://mock.test",
			"api_key": "test-key",
			"model": "gpt-4",
			"timeout": 60,
			"custom_headers": {"X-Tenant": "tenant-a"}
		}
	}`
	var cfg llmschema.ModelConfig
	require.NoError(t, json.Unmarshal([]byte(cfgJSON), &cfg))
	qr, err := NewQueryRewriter(&cfg, &fakeModelContext{})
	assert.NoError(t, err)
	assert.Equal(t, map[string]string{"X-Tenant": "tenant-a"}, qr.llm.ClientConfig.CustomHeaders)
	assert.False(t, qr.llm.ClientConfig.VerifySSL) // 无 ssl_cert → 强制关闭
}

// TestNewQueryRewriter_verifySSL无证书强制关闭 Go 增强：Extra 边界。
func TestNewQueryRewriter_verifySSL无证书强制关闭(t *testing.T) {
	mi := llmschema.NewBaseModelInfo("https://mock.test",
		llmschema.WithAPIKey("test-key"),
		llmschema.WithModelNameForInfo("gpt-4"),
		llmschema.WithInfoExtra(map[string]any{"verify_ssl": true}), // 无 ssl_cert
	)
	qr, err := NewQueryRewriter(llmschema.NewModelConfig("OpenAI", *mi), &fakeModelContext{})
	assert.NoError(t, err)
	assert.False(t, qr.llm.ClientConfig.VerifySSL)
}

// TestNewQueryRewriter_verifySSL关闭有证书透传 Go 增强：Extra 边界（spec §6.3）。
func TestNewQueryRewriter_verifySSL关闭有证书透传(t *testing.T) {
	mi := llmschema.NewBaseModelInfo("https://mock.test",
		llmschema.WithAPIKey("test-key"),
		llmschema.WithModelNameForInfo("gpt-4"),
		llmschema.WithInfoExtra(map[string]any{"verify_ssl": false, "ssl_cert": "/tmp/ca.pem"}),
	)
	qr, err := NewQueryRewriter(llmschema.NewModelConfig("OpenAI", *mi), &fakeModelContext{})
	assert.NoError(t, err)
	assert.False(t, qr.llm.ClientConfig.VerifySSL)
	assert.Equal(t, "/tmp/ca.pem", qr.llm.ClientConfig.SSLCert)
}

// TestNewQueryRewriter_非法provider Go 增强：错误透传。
func TestNewQueryRewriter_非法provider(t *testing.T) {
	mi := llmschema.NewBaseModelInfo("https://mock.test",
		llmschema.WithAPIKey("test-key"),
		llmschema.WithModelNameForInfo("gpt-4"),
	)
	_, err := NewQueryRewriter(llmschema.NewModelConfig("NotAProvider", *mi), &fakeModelContext{})
	assert.Error(t, err)
}

// TestNewQueryRewriter_注入通道 Go 增强：WithModelClientOption 跳过内部构造。
func TestNewQueryRewriter_注入通道(t *testing.T) {
	clientCfg, err := llmschema.NewModelClientConfig("openai", "test-key", "https://mock.test", llmschema.WithVerifySSL(false))
	assert.NoError(t, err)
	model, err := llm.NewModel(clientCfg, llmschema.NewModelRequestConfig(), llm.WithClient(&stubQRClient{}))
	assert.NoError(t, err)
	// 非法 cfg（provider 错误）本应报错，但注入通道跳过构造 → 不报错
	mi := llmschema.NewBaseModelInfo("https://mock.test", llmschema.WithAPIKey("k"), llmschema.WithModelNameForInfo("m"))
	qr, err := NewQueryRewriter(llmschema.NewModelConfig("NotAProvider", *mi), &fakeModelContext{}, WithModelClientOption(model))
	assert.NoError(t, err)
	assert.Equal(t, model, qr.llm)
}

// TestRewrite_英文模板 Go 增强：prompt_lang=en 路由。
func TestRewrite_英文模板(t *testing.T) {
	fmc := &fakeModelContext{}
	_, _ = fmc.AddMessages(context.Background(), []llmschema.BaseMessage{
		llmschema.NewUserMessage("hello"), llmschema.NewAssistantMessage("hi!"),
	})
	qr := newQRTestRewriter(t, &stubQRClient{rewriteResp: makeFullRewriteResponse("test")}, fmc,
		WithPromptLang("en"), WithCompressRange(5))
	result, err := qr.Rewrite(context.Background(), "test")
	assert.NoError(t, err)
	assert.Equal(t, "test", result["standalone_query"])
}

// TestCompress_触发压缩写回上下文 Go 增强：compressed_history 消息形态。
func TestCompress_触发压缩写回上下文(t *testing.T) {
	fmc := &fakeModelContext{}
	for i := 0; i < 3; i++ {
		_, _ = fmc.AddMessages(context.Background(), []llmschema.BaseMessage{
			llmschema.NewUserMessage(fmt.Sprintf("q%d", i)), llmschema.NewAssistantMessage(fmt.Sprintf("a%d", i)),
		})
	}
	stub := &stubQRClient{compressResp: makeCompressResponse(), rewriteResp: makeFullRewriteResponse("x")}
	qr := newQRTestRewriter(t, stub, fmc, WithCompressRange(5))
	_, err := qr.Rewrite(context.Background(), "x")
	assert.NoError(t, err)
	msgs, _ := fmc.GetMessages(0, true)
	assert.Len(t, msgs, 1)
	assert.Equal(t, "system", msgs[0].GetRole().String())
	assert.Equal(t, "compressed_history", msgs[0].GetName())
	assert.Contains(t, msgs[0].GetContent().Text(), `"theme"`)
}

// TestMsg2Text_多模态content Go 增强：Parts JSON 序列化路径（Python list/dict content）。
func TestMsg2Text_多模态content(t *testing.T) {
	qr := newQRTestRewriter(t, &stubQRClient{}, &fakeModelContext{})
	// 构造带 Parts 的消息（parts 非 nil 即非 IsText）
	msg := llmschema.NewUserMessage("")
	msg.SetContent(llmschema.NewMultiModalContent(llmschema.ContentPart{Type: "text", Text: "图片描述"}))
	text, err := qr.Msg2Text([]llmschema.BaseMessage{msg})
	assert.NoError(t, err)
	assert.Contains(t, text, "user: ")
	assert.Contains(t, text, "图片描述")
}
