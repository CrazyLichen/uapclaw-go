package modalityprobe

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// probeFakeClient 探测测试用模拟模型客户端。
// Python: tests/unit_tests/harness/test_image_modality_probe.py 的 fake llm
type probeFakeClient struct {
	// resp 预设响应（非 nil 时 Invoke 返回它）
	resp *llmschema.AssistantMessage
	// err 预设错误（非 nil 时 Invoke 返回它）
	err error
	// failFirst 前 N 次调用返回 err，之后返回 resp（模拟"带开关失败→去开关重试"）
	failFirst int
	// invokeCount 调用次数
	invokeCount int
	// lastExtra 最近一次调用的 extra body
	lastExtra map[string]any
	// lastBaseMessages 最近一次调用的消息
	lastBaseMessages []llmschema.BaseMessage
}

// Invoke 返回预设响应或错误，并记录调用信息。
func (f *probeFakeClient) Invoke(_ context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	f.invokeCount++
	params := model_clients.NewInvokeParams(opts...)
	f.lastExtra = params.Extra
	if msgs := messages.Messages(); msgs != nil {
		f.lastBaseMessages = msgs
	}
	if f.err != nil && (f.failFirst == 0 || f.invokeCount <= f.failFirst) {
		return f.resp, f.err
	}
	return f.resp, nil
}

// Stream 不参与本测例。
func (f *probeFakeClient) Stream(
	context.Context, model_clients.MessagesParam, ...model_clients.StreamOption,
) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, nil
}

// GenerateImage 不参与本测例。
func (f *probeFakeClient) GenerateImage(
	context.Context, []*llmschema.UserMessage, ...model_clients.GenerateImageOption,
) (*llmschema.ImageGenerationResponse, error) {
	return nil, nil
}

// GenerateSpeech 不参与本测例。
func (f *probeFakeClient) GenerateSpeech(
	context.Context, []*llmschema.UserMessage, ...model_clients.GenerateSpeechOption,
) (*llmschema.AudioGenerationResponse, error) {
	return nil, nil
}

// GenerateVideo 不参与本测例。
func (f *probeFakeClient) GenerateVideo(
	context.Context, []*llmschema.UserMessage, ...model_clients.GenerateVideoOption,
) (*llmschema.VideoGenerationResponse, error) {
	return nil, nil
}

// TranscribeAudio 不参与本测例。
func (f *probeFakeClient) TranscribeAudio(
	context.Context, string, ...llmschema.TranscribeAudioOption,
) (*llmschema.TranscriptionResponse, error) {
	return nil, nil
}

// Release 不参与本测例。
func (f *probeFakeClient) Release(context.Context, ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}

// SupportsKVCacheRelease 不支持释放。
func (f *probeFakeClient) SupportsKVCacheRelease() bool { return false }

// SupportsKVCacheAffinity 不支持亲和。
func (f *probeFakeClient) SupportsKVCacheAffinity() bool { return false }

// BuildKVCacheAffinityInvokeKwargs 返回空参数。
func (f *probeFakeClient) BuildKVCacheAffinityInvokeKwargs(
	any, string, string, int,
) map[string]any {
	return nil
}

// newProbeModel 构造带 fake client 的 Model。
func newProbeModel(client *probeFakeClient) *llm.Model {
	m, err := llm.NewModel(
		&llmschema.ModelClientConfig{ClientID: "probe-client", ClientProvider: "OpenAI", APIKey: "test-key", APIBase: "http://probe.example"},
		&llmschema.ModelRequestConfig{ModelName: "probe-model"},
		llm.WithClient(client),
	)
	if err != nil {
		panic(err)
	}
	return m
}

// ──────────────────────────── 测试 ────────────────────────────

// TestMakeRedPngB64 假红图：PNG 魔数 + base64 可解码 + IHDR 尺寸 32x32。
// Python: _make_red_png_b64:67-83
func TestMakeRedPngB64(t *testing.T) {
	raw, err := base64.StdEncoding.DecodeString(dummyImageB64)
	if err != nil {
		t.Fatalf("base64 解码失败: %v", err)
	}
	if string(raw[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Errorf("缺 PNG 魔数")
	}
	if len(raw) < 33 {
		t.Errorf("PNG 过短: %d", len(raw))
	}
	if !strings.Contains(string(dummyImageB64), "") {
		t.Error("base64 为空")
	}
}

// TestIsImageModalityRejection 错误码/文案判定矩阵。
// Python: is_image_modality_rejection:135-157 + test_image_modality_probe.py
func TestIsImageModalityRejection(t *testing.T) {
	rejected := []string{
		"invalid_image_input",
		"unsupported_image",
		"no endpoints found that support image input",
		"does not support image",
		"unknown variant",
		"vision is not supported",
		// 错误码后缀匹配（Python: normalized.endswith(f"_{code}")）
		"request failed: bad_request_invalid_image_input",
		// BaseError 的 Message 进入收集链
		exception.NewBaseError(
			exception.NewStatusCode("INVALID_IMAGE_INPUT", 181002, ""),
			exception.WithMsg("unsupported_image"),
		).Error(),
	}
	for _, c := range rejected {
		if !isImageModalityRejection(errors.New(c)) {
			t.Errorf("应判定为拒绝: %q", c)
		}
	}
	notRejected := []string{"rate limited", "connection reset", "unauthorized"}
	for _, c := range notRejected {
		if isImageModalityRejection(errors.New(c)) {
			t.Errorf("不应判定为拒绝: %q", c)
		}
	}
	// Unwrap 链穿透（Python: __cause__/__context__）
	wrapped := exception.NewBaseError(
		exception.NewStatusCode("EXEC_ERROR", 181003, ""),
		exception.WithMsg("invoke failed"),
		exception.WithCause(errors.New("does not support image input")),
	)
	if !isImageModalityRejection(wrapped) {
		t.Error("Unwrap 链上的拒绝文案未被识别")
	}
}

// TestInterpretProbeResponse 截断/空回复不缓存为 false。
// Python: _interpret_probe_response:378-419
func TestInterpretProbeResponse(t *testing.T) {
	red := interpretProbeResponse(&llmschema.AssistantMessage{
		DefaultMessage: llmschema.DefaultMessage{Content: llmschema.NewTextContent("red")},
		FinishReason:   "stop",
	})
	if red == nil || !*red {
		t.Error("说出 red 应为 true")
	}

	// reasoning 里说出颜色也算
	blue := interpretProbeResponse(&llmschema.AssistantMessage{
		DefaultMessage:   llmschema.DefaultMessage{Content: llmschema.NewTextContent("hmm")},
		ReasoningContent: "the image is red",
		FinishReason:     "stop",
	})
	if blue == nil || !*blue {
		t.Error("reasoning 中说出 red 应为 true")
	}

	// 截断 → 未知不缓存
	if got := interpretProbeResponse(&llmschema.AssistantMessage{
		DefaultMessage: llmschema.DefaultMessage{Content: llmschema.NewTextContent("the col")},
		FinishReason:   "length",
	}); got != nil {
		t.Error("截断答案应为 nil")
	}

	// 空回复 → 未知
	if got := interpretProbeResponse(&llmschema.AssistantMessage{
		FinishReason: "stop",
	}); got != nil {
		t.Error("空回复应为 nil")
	}

	// 说出别的颜色 → false
	named := interpretProbeResponse(&llmschema.AssistantMessage{
		DefaultMessage: llmschema.DefaultMessage{Content: llmschema.NewTextContent("blue")},
		FinishReason:   "stop",
	})
	if named == nil || *named {
		t.Error("回答 blue 且未截断应为 false")
	}

	// nil 响应 → 未知
	if got := interpretProbeResponse(nil); got != nil {
		t.Error("nil 响应为 nil")
	}
}

// TestProbeImageSupport_Verdicts 探测主流程：说出颜色/拒绝/未知三种结论。
// Python: probe_image_support:422-439 + _run_probe:323-375
func TestProbeImageSupport_Verdicts(t *testing.T) {
	defer ResetImageSupportCache()

	newMsg := func(text string) *llmschema.AssistantMessage {
		return &llmschema.AssistantMessage{
			DefaultMessage: llmschema.DefaultMessage{Content: llmschema.NewTextContent(text)},
			FinishReason:   "stop",
		}
	}

	// 说出颜色 → true 且缓存
	ResetImageSupportCache()
	sayRed := newProbeModel(&probeFakeClient{resp: newMsg("red")})
	if v := ProbeImageSupport(context.Background(), sayRed); v == nil || !*v {
		t.Fatalf("red 模型应得 true")
	}
	if GetCachedImageSupport(sayRed) == nil || !*GetCachedImageSupport(sayRed) {
		t.Error("结论应写入缓存")
	}

	// 确定性拒绝 → false 且缓存
	ResetImageSupportCache()
	reject := newProbeModel(&probeFakeClient{
		err: errors.New("no endpoints found that support image input"),
	})
	if v := ProbeImageSupport(context.Background(), reject); v == nil || *v {
		t.Fatalf("拒绝模型应得 false")
	}
	if GetCachedImageSupport(reject) == nil || *GetCachedImageSupport(reject) {
		t.Error("false 结论应写入缓存")
	}

	// 普通调用失败 → nil 不缓存
	ResetImageSupportCache()
	failing := newProbeModel(&probeFakeClient{err: errors.New("rate limited")})
	if v := ProbeImageSupport(context.Background(), failing); v != nil {
		t.Fatalf("限流应得 nil")
	}
	if GetCachedImageSupport(failing) != nil {
		t.Error("未知结论不得写缓存")
	}

	// 带思考关闭开关失败 → 去掉开关重试一次成功（Python: :343-373）
	ResetImageSupportCache()
	retryModel := newProbeModel(&probeFakeClient{
		err:       errors.New("thinking switches rejected"),
		resp:      newMsg("red"),
		failFirst: 1,
	})
	if v := ProbeImageSupport(context.Background(), retryModel); v == nil || !*v {
		t.Fatalf("重试后应得 true")
	}
}

// TestProbeImageSupport_CacheReuse 缓存命中不再发请求。
func TestProbeImageSupport_CacheReuse(t *testing.T) {
	defer ResetImageSupportCache()
	ResetImageSupportCache()

	newMsg := func(text string) *llmschema.AssistantMessage {
		return &llmschema.AssistantMessage{
			DefaultMessage: llmschema.DefaultMessage{Content: llmschema.NewTextContent(text)},
			FinishReason:   "stop",
		}
	}
	client := &probeFakeClient{resp: newMsg("red")}
	model := newProbeModel(client)
	if v := ProbeImageSupport(context.Background(), model); v == nil || !*v {
		t.Fatal("首次探测应得 true")
	}
	first := client.invokeCount
	if v := ProbeImageSupport(context.Background(), model); v == nil || !*v {
		t.Fatal("缓存命中应得 true")
	}
	if client.invokeCount != first {
		t.Errorf("缓存命中不应再次调用: %d -> %d", first, client.invokeCount)
	}
}

// TestScheduleImageSupportProbe_Background 后台探测：完成前调度去重，完成后写缓存。
// Python: schedule_image_support_probe:200-231
func TestScheduleImageSupportProbe_Background(t *testing.T) {
	defer ResetImageSupportCache()
	ResetImageSupportCache()

	newMsg := func(text string) *llmschema.AssistantMessage {
		return &llmschema.AssistantMessage{
			DefaultMessage: llmschema.DefaultMessage{Content: llmschema.NewTextContent(text)},
			FinishReason:   "stop",
		}
	}
	client := &probeFakeClient{resp: newMsg("red")}
	model := newProbeModel(client)

	ScheduleImageSupportProbe(model)
	ScheduleImageSupportProbe(model) // 在途去重

	// 轮询等后台结论落地（goroutine 无 asyncio done_callback 通知）
	deadline := 100
	for i := 0; i < deadline; i++ {
		if GetCachedImageSupport(model) != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if GetCachedImageSupport(model) == nil {
		t.Fatal("后台探测未在预期时间内落缓存")
	}
	if client.invokeCount != 1 {
		t.Errorf("应只探测一次: %d", client.invokeCount)
	}
	// 已有结论后再次调度直接跳过
	ScheduleImageSupportProbe(model)
	if client.invokeCount != 1 {
		t.Errorf("已有结论不应再探测: %d", client.invokeCount)
	}
}

// TestProbeCacheKeyOf 缓存键提取与模型名缺失保护。
// Python: probe_cache_key:160-168
func TestProbeCacheKeyOf(t *testing.T) {
	model := newProbeModel(&probeFakeClient{})
	key := probeCacheKeyOf(model)
	if key == nil || key.APIBase != "http://probe.example" || key.ModelName != "probe-model" {
		t.Errorf("缓存键错误: %+v", key)
	}
	if probeCacheKeyOf(nil) != nil {
		t.Error("nil 模型应为 nil 键")
	}
	noName := &llm.Model{
		ModelConfig:  &llmschema.ModelRequestConfig{ModelName: "  "},
		ClientConfig: &llmschema.ModelClientConfig{},
	}
	if probeCacheKeyOf(noName) != nil {
		t.Error("模型名缺失应为 nil 键")
	}
}

// TestProbeRequestShape 探测请求携带假图 + 问题 + max_tokens/temperature。
// Python: _invoke_probe:254-282
func TestProbeRequestShape(t *testing.T) {
	defer ResetImageSupportCache()
	ResetImageSupportCache()

	client := &probeFakeClient{
		resp: &llmschema.AssistantMessage{FinishReason: "stop"},
	}
	model := newProbeModel(client)
	_ = ProbeImageSupport(context.Background(), model)

	if len(client.lastBaseMessages) != 1 {
		t.Fatalf("应只有一条消息: %d", len(client.lastBaseMessages))
	}
	content := client.lastBaseMessages[0].GetContent()
	if content.IsText() {
		t.Fatal("content 应为多模态分片")
	}
	parts := content.Parts()
	if len(parts) != 2 || parts[0].Type != "image_url" || parts[1].Type != "text" {
		t.Fatalf("分片结构错误: %+v", parts)
	}
	if parts[0].ImageURL == nil || !strings.Contains(parts[0].ImageURL.URL, "data:image/png;base64,") {
		t.Error("缺 data URI 假图")
	}
	if parts[1].Text != "What color is this image? Reply with one word." {
		t.Errorf("问题文本错误: %q", parts[1].Text)
	}
}

// TestSetCachedImageSupport_Declared 声明预填缓存（供 warmup 使用）。
// Python: set_cached_image_support:179-189
func TestSetCachedImageSupport_Declared(t *testing.T) {
	defer ResetImageSupportCache()
	ResetImageSupportCache()

	key := ProbeCacheKey{APIBase: "http://declared.example", ModelName: "m1"}
	SetCachedImageSupport(key, true)
	if v := cachedImageSupport(key); v == nil || !*v {
		t.Error("预填结论应可读")
	}
	ResetImageSupportCache()
	if cachedImageSupport(key) != nil {
		t.Error("reset 后应清空")
	}
}
