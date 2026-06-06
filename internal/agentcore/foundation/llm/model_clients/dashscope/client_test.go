package dashscope

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 辅助函数 ────────────────────────────

// newTestClientConfig 创建测试用的客户端配置
func newTestClientConfig(provider, apiKey, apiBase string) *llmschema.ModelClientConfig {
	return llmschema.NewModelClientConfig(provider, apiKey, apiBase, llmschema.WithVerifySSL(false))
}

// newTestModelConfig 创建测试用的模型请求配置
func newTestModelConfig() *llmschema.ModelRequestConfig {
	return llmschema.NewModelRequestConfig(llmschema.WithModelName("qwen-plus"))
}

// decodeRequestBody 从 HTTP 请求解码 JSON 请求体
func decodeRequestBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("读取请求体失败: %v", err)
	}
	var reqBody map[string]any
	if err := json.Unmarshal(body, &reqBody); err != nil {
		t.Fatalf("解析请求体失败: %v", err)
	}
	return reqBody
}

// ──────────────────────────── NewDashScopeModelClient 测试 ────────────────────────────

func TestNewDashScopeModelClient_ValidConfig(t *testing.T) {
	// 有效配置创建客户端成功
	client, err := NewDashScopeModelClient(
		newTestModelConfig(),
		newTestClientConfig("DashScope", "test-key", "https://dashscope.aliyuncs.com/compatible-mode/v1"),
	)
	if err != nil {
		t.Fatalf("NewDashScopeModelClient 返回错误: %v", err)
	}
	if client == nil {
		t.Fatal("client 不应为 nil")
	}
	if client.GetClientName() != "DashScope client" {
		t.Errorf("ClientName = %q, want %q", client.GetClientName(), "DashScope client")
	}
}

func TestNewDashScopeModelClient_NoAPIKey(t *testing.T) {
	// 缺少 API Key 应失败
	client, err := NewDashScopeModelClient(
		newTestModelConfig(),
		newTestClientConfig("DashScope", "", "https://dashscope.aliyuncs.com/compatible-mode/v1"),
	)
	if err == nil {
		t.Error("缺少 API Key 时应返回错误")
	}
	if client != nil {
		t.Error("缺少 API Key 时 client 应为 nil")
	}
}

func TestNewDashScopeModelClient_NoAPIBase(t *testing.T) {
	// 缺少 API Base 应失败
	client, err := NewDashScopeModelClient(
		newTestModelConfig(),
		newTestClientConfig("DashScope", "test-key", ""),
	)
	if err == nil {
		t.Error("缺少 API Base 时应返回错误")
	}
	if client != nil {
		t.Error("缺少 API Base 时 client 应为 nil")
	}
}

func TestRelease_NotSupported(t *testing.T) {
	// Release 应返回 false 和不支持错误
	client, err := NewDashScopeModelClient(
		newTestModelConfig(),
		newTestClientConfig("DashScope", "test-key", "https://dashscope.aliyuncs.com/compatible-mode/v1"),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	ok, err := client.Release(context.Background())
	if ok {
		t.Error("Release 应返回 false")
	}
	if err == nil {
		t.Fatal("Release 应返回错误")
	}
	if !strings.Contains(err.Error(), "does not support KV cache release") {
		t.Errorf("错误消息应包含 'does not support KV cache release', got %q", err.Error())
	}
}

// ──────────────────────────── 接口合规性测试 ────────────────────────────

func TestDashScopeModelClient_ImplementsBaseModelClient(t *testing.T) {
	// 验证 DashScopeModelClient 实现了 BaseModelClient 接口
	var _ model_clients.BaseModelClient = (*DashScopeModelClient)(nil)
}

// ──────────────────────────── validateImageMessages 测试 ────────────────────────────

func TestValidateImageMessages_NoMessages(t *testing.T) {
	// 空消息列表应报错
	_, err := validateImageMessages(nil)
	if err == nil {
		t.Error("空消息列表应返回错误")
	}
}

func TestValidateImageMessages_TooManyMessages(t *testing.T) {
	// 超过 1 条消息应报错
	msgs := []*llmschema.UserMessage{
		llmschema.NewUserMessage("hello"),
		llmschema.NewUserMessage("world"),
	}
	_, err := validateImageMessages(msgs)
	if err == nil {
		t.Error("超过 1 条消息应返回错误")
	}
}

func TestValidateImageMessages_PlainText(t *testing.T) {
	// 纯文本消息 → contentList 应包含 {"text": "..."}
	msgs := []*llmschema.UserMessage{llmschema.NewUserMessage("一只可爱的猫")}
	contentList, err := validateImageMessages(msgs)
	if err != nil {
		t.Fatalf("纯文本验证失败: %v", err)
	}
	if len(contentList) != 1 {
		t.Fatalf("contentList 长度 = %d, want 1", len(contentList))
	}
	textVal, _ := contentList[0]["text"].(string)
	if textVal != "一只可爱的猫" {
		t.Errorf("contentList[0][\"text\"] = %q, want %q", textVal, "一只可爱的猫")
	}
}

func TestValidateImageMessages_MultiModalContent(t *testing.T) {
	// 多模态内容（text + image_url）→ 解析为 DashScope 格式
	msgs := []*llmschema.UserMessage{
		llmschema.NewUserMessage("", llmschema.WithMultiModalContent(
			llmschema.ContentPart{Type: "text", Text: "描述这张图"},
			llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "https://example.com/cat.jpg"}},
		)),
	}
	contentList, err := validateImageMessages(msgs)
	if err != nil {
		t.Fatalf("多模态验证失败: %v", err)
	}
	if len(contentList) != 2 {
		t.Fatalf("contentList 长度 = %d, want 2", len(contentList))
	}
	// 第一个应为 text
	textVal, _ := contentList[0]["text"].(string)
	if textVal != "描述这张图" {
		t.Errorf("contentList[0][\"text\"] = %q, want %q", textVal, "描述这张图")
	}
	// 第二个应为 image
	imgVal, _ := contentList[1]["image"].(string)
	if imgVal != "https://example.com/cat.jpg" {
		t.Errorf("contentList[1][\"image\"] = %q, want image URL", imgVal)
	}
}

func TestValidateImageMessages_NoText(t *testing.T) {
	// 没有文本提示词应报错（只有图片）
	msgs := []*llmschema.UserMessage{
		llmschema.NewUserMessage("", llmschema.WithMultiModalContent(
			llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "https://example.com/cat.jpg"}},
		)),
	}
	_, err := validateImageMessages(msgs)
	if err == nil {
		t.Error("没有文本提示词应返回错误")
	}
}

func TestValidateImageMessages_TooManyImages(t *testing.T) {
	// 超过 3 张图片应报错
	msgs := []*llmschema.UserMessage{
		llmschema.NewUserMessage("", llmschema.WithMultiModalContent(
			llmschema.ContentPart{Type: "text", Text: "描述"},
			llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "https://example.com/1.jpg"}},
			llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "https://example.com/2.jpg"}},
			llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "https://example.com/3.jpg"}},
			llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "https://example.com/4.jpg"}},
		)),
	}
	_, err := validateImageMessages(msgs)
	if err == nil {
		t.Error("超过 3 张图片应返回错误")
	}
}

// ──────────────────────────── validateSpeechMessages 测试 ────────────────────────────

func TestValidateSpeechMessages_NoMessages(t *testing.T) {
	_, err := validateSpeechMessages(nil)
	if err == nil {
		t.Error("空消息列表应返回错误")
	}
}

func TestValidateSpeechMessages_TooManyMessages(t *testing.T) {
	msgs := []*llmschema.UserMessage{
		llmschema.NewUserMessage("hello"),
		llmschema.NewUserMessage("world"),
	}
	_, err := validateSpeechMessages(msgs)
	if err == nil {
		t.Error("超过 1 条消息应返回错误")
	}
}

func TestValidateSpeechMessages_EmptyContent(t *testing.T) {
	msgs := []*llmschema.UserMessage{llmschema.NewUserMessage("  ")}
	_, err := validateSpeechMessages(msgs)
	if err == nil {
		t.Error("空内容应返回错误")
	}
}

func TestValidateSpeechMessages_ValidText(t *testing.T) {
	msgs := []*llmschema.UserMessage{llmschema.NewUserMessage("你好世界")}
	text, err := validateSpeechMessages(msgs)
	if err != nil {
		t.Fatalf("有效文本验证失败: %v", err)
	}
	if text != "你好世界" {
		t.Errorf("text = %q, want %q", text, "你好世界")
	}
}

func TestValidateSpeechMessages_MultiModalContent(t *testing.T) {
	// 多模态内容应提取文本部分
	msgs := []*llmschema.UserMessage{
		llmschema.NewUserMessage("", llmschema.WithMultiModalContent(
			llmschema.ContentPart{Type: "text", Text: "你好"},
			llmschema.ContentPart{Type: "text", Text: "世界"},
		)),
	}
	text, err := validateSpeechMessages(msgs)
	if err != nil {
		t.Fatalf("多模态文本验证失败: %v", err)
	}
	if !strings.Contains(text, "你好") || !strings.Contains(text, "世界") {
		t.Errorf("text = %q, 应包含 '你好' 和 '世界'", text)
	}
}

// ──────────────────────────── validateVideoMessages 测试 ────────────────────────────

func TestValidateVideoMessages_NoMessages(t *testing.T) {
	_, err := validateVideoMessages(nil)
	if err == nil {
		t.Error("空消息列表应返回错误")
	}
}

func TestValidateVideoMessages_TooManyMessages(t *testing.T) {
	msgs := []*llmschema.UserMessage{
		llmschema.NewUserMessage("hello"),
		llmschema.NewUserMessage("world"),
	}
	_, err := validateVideoMessages(msgs)
	if err == nil {
		t.Error("超过 1 条消息应返回错误")
	}
}

func TestValidateVideoMessages_EmptyContent(t *testing.T) {
	msgs := []*llmschema.UserMessage{llmschema.NewUserMessage("")}
	_, err := validateVideoMessages(msgs)
	if err == nil {
		t.Error("空内容应返回错误")
	}
}

func TestValidateVideoMessages_ValidText(t *testing.T) {
	msgs := []*llmschema.UserMessage{llmschema.NewUserMessage("一只猫在奔跑")}
	text, err := validateVideoMessages(msgs)
	if err != nil {
		t.Fatalf("有效文本验证失败: %v", err)
	}
	if text != "一只猫在奔跑" {
		t.Errorf("text = %q, want %q", text, "一只猫在奔跑")
	}
}

func TestValidateVideoMessages_MultiModalContent(t *testing.T) {
	msgs := []*llmschema.UserMessage{
		llmschema.NewUserMessage("", llmschema.WithMultiModalContent(
			llmschema.ContentPart{Type: "text", Text: "视频描述"},
		)),
	}
	text, err := validateVideoMessages(msgs)
	if err != nil {
		t.Fatalf("多模态文本验证失败: %v", err)
	}
	if text != "视频描述" {
		t.Errorf("text = %q, want %q", text, "视频描述")
	}
}

// ──────────────────────────── buildImageParameters 测试 ────────────────────────────

func TestBuildImageParameters_Defaults(t *testing.T) {
	// 默认参数
	params := model_clients.NewGenerateImageParams()
	result := buildImageParameters(params)

	if result["result_format"] != "message" {
		t.Errorf("result_format = %v, want message", result["result_format"])
	}
	if result["size"] != "1664*928" {
		t.Errorf("size = %v, want 1664*928", result["size"])
	}
	if result["n"] != 1 {
		t.Errorf("n = %v, want 1", result["n"])
	}
	if result["prompt_extend"] != true {
		t.Errorf("prompt_extend = %v, want true", result["prompt_extend"])
	}
	if result["watermark"] != false {
		t.Errorf("watermark = %v, want false", result["watermark"])
	}
	// seed=0 不应出现在 parameters 中
	if _, ok := result["seed"]; ok {
		t.Error("seed=0 不应出现在 parameters 中")
	}
	// negative_prompt 为空不应出现
	if _, ok := result["negative_prompt"]; ok {
		t.Error("空 negative_prompt 不应出现在 parameters 中")
	}
}

func TestBuildImageParameters_WithAllOptions(t *testing.T) {
	// 所有选项都设置
	params := model_clients.NewGenerateImageParams(
		model_clients.WithImageSeed(42),
		model_clients.WithImageNegativePrompt("blurry, low quality"),
		model_clients.WithImageExtra(map[string]any{"custom_key": "custom_value"}),
	)
	result := buildImageParameters(params)

	if result["seed"] != 42 {
		t.Errorf("seed = %v, want 42", result["seed"])
	}
	if result["negative_prompt"] != "blurry, low quality" {
		t.Errorf("negative_prompt = %v, want 'blurry, low quality'", result["negative_prompt"])
	}
	if result["custom_key"] != "custom_value" {
		t.Errorf("custom_key = %v, want 'custom_value'", result["custom_key"])
	}
}

// ──────────────────────────── extractImageURLs 测试 ────────────────────────────

func TestExtractImageURLs_Success(t *testing.T) {
	output := MultiModalOutput{
		Choices: []MultiModalChoice{
			{
				FinishReason: "stop",
				Message: &MultiModalMessage{
					Role: "assistant",
					Content: []ContentItem{
						{Image: "https://example.com/img1.png"},
						{Image: "https://example.com/img2.png"},
					},
				},
			},
		},
	}
	outputBytes, _ := json.Marshal(output)
	resp := &DashScopeResponse{Output: outputBytes}

	urls, err := extractImageURLs(resp)
	if err != nil {
		t.Fatalf("extractImageURLs 失败: %v", err)
	}
	if len(urls) != 2 {
		t.Fatalf("urls 长度 = %d, want 2", len(urls))
	}
	if urls[0] != "https://example.com/img1.png" {
		t.Errorf("urls[0] = %q, want img1 URL", urls[0])
	}
}

func TestExtractImageURLs_NoImages(t *testing.T) {
	// 响应中没有图片应报错
	output := MultiModalOutput{
		Choices: []MultiModalChoice{
			{
				FinishReason: "stop",
				Message: &MultiModalMessage{
					Role:    "assistant",
					Content: []ContentItem{{Text: "没有图片"}},
				},
			},
		},
	}
	outputBytes, _ := json.Marshal(output)
	resp := &DashScopeResponse{Output: outputBytes}

	_, err := extractImageURLs(resp)
	if err == nil {
		t.Error("没有图片应返回错误")
	}
}

func TestExtractImageURLs_InvalidJSON(t *testing.T) {
	// Output 不是有效 JSON
	resp := &DashScopeResponse{Output: []byte("invalid json")}
	_, err := extractImageURLs(resp)
	if err == nil {
		t.Error("无效 JSON 应返回错误")
	}
}

// ──────────────────────────── extractAudioInfo 测试 ────────────────────────────

func TestExtractAudioInfo_WithURL(t *testing.T) {
	// 音频 URL 模式 + 格式推断
	output := AudioOutput{URL: "https://example.com/audio.wav"}
	outputBytes, _ := json.Marshal(output)
	resp := &DashScopeResponse{Output: outputBytes}

	audioURL, audioData, audioFormat, err := extractAudioInfo(resp)
	if err != nil {
		t.Fatalf("extractAudioInfo 失败: %v", err)
	}
	if audioURL != "https://example.com/audio.wav" {
		t.Errorf("audioURL = %q, want wav URL", audioURL)
	}
	if audioFormat != "wav" {
		t.Errorf("audioFormat = %q, want wav", audioFormat)
	}
	if len(audioData) != 0 {
		t.Errorf("audioData 不应有数据")
	}
}

func TestExtractAudioInfo_WithMP3URL(t *testing.T) {
	output := AudioOutput{URL: "https://example.com/audio.mp3"}
	outputBytes, _ := json.Marshal(output)
	resp := &DashScopeResponse{Output: outputBytes}

	_, _, audioFormat, err := extractAudioInfo(resp)
	if err != nil {
		t.Fatalf("extractAudioInfo 失败: %v", err)
	}
	if audioFormat != "mp3" {
		t.Errorf("audioFormat = %q, want mp3", audioFormat)
	}
}

func TestExtractAudioInfo_WithPCMURL(t *testing.T) {
	output := AudioOutput{URL: "https://example.com/audio.pcm"}
	outputBytes, _ := json.Marshal(output)
	resp := &DashScopeResponse{Output: outputBytes}

	_, _, audioFormat, err := extractAudioInfo(resp)
	if err != nil {
		t.Fatalf("extractAudioInfo 失败: %v", err)
	}
	if audioFormat != "pcm" {
		t.Errorf("audioFormat = %q, want pcm", audioFormat)
	}
}

func TestExtractAudioInfo_WithData(t *testing.T) {
	// Base64 数据模式
	output := AudioOutput{Data: "dGVzdA=="}
	outputBytes, _ := json.Marshal(output)
	resp := &DashScopeResponse{Output: outputBytes}

	audioURL, audioData, _, err := extractAudioInfo(resp)
	if err != nil {
		t.Fatalf("extractAudioInfo 失败: %v", err)
	}
	if audioURL != "" {
		t.Errorf("audioURL 应为空, got %q", audioURL)
	}
	if len(audioData) == 0 {
		t.Error("audioData 不应为空")
	}
}

func TestExtractAudioInfo_NoData(t *testing.T) {
	// 既没有 URL 也没有数据应报错
	output := AudioOutput{}
	outputBytes, _ := json.Marshal(output)
	resp := &DashScopeResponse{Output: outputBytes}

	_, _, _, err := extractAudioInfo(resp)
	if err == nil {
		t.Error("没有音频数据应返回错误")
	}
}

func TestExtractAudioInfo_InvalidJSON(t *testing.T) {
	resp := &DashScopeResponse{Output: []byte("invalid json")}
	_, _, _, err := extractAudioInfo(resp)
	if err == nil {
		t.Error("无效 JSON 应返回错误")
	}
}

// ──────────────────────────── extractVideoInfo 测试 ────────────────────────────

func TestExtractVideoInfo_Success(t *testing.T) {
	output := VideoOutput{VideoURL: "https://example.com/video.mp4"}
	outputBytes, _ := json.Marshal(output)
	usage := VideoUsage{Duration: float64(5), Size: "1280*720"}
	usageBytes, _ := json.Marshal(usage)
	resp := &DashScopeResponse{Output: outputBytes, Usage: usageBytes}

	videoURL, duration, resolution, err := extractVideoInfo(resp)
	if err != nil {
		t.Fatalf("extractVideoInfo 失败: %v", err)
	}
	if videoURL != "https://example.com/video.mp4" {
		t.Errorf("videoURL = %q, want video URL", videoURL)
	}
	if duration != 5 {
		t.Errorf("duration = %f, want 5", duration)
	}
	if resolution != "1280*720" {
		t.Errorf("resolution = %q, want 1280*720", resolution)
	}
}

func TestExtractVideoInfo_NoVideoURL(t *testing.T) {
	// 没有 video_url 应报错
	output := VideoOutput{VideoURL: ""}
	outputBytes, _ := json.Marshal(output)
	resp := &DashScopeResponse{Output: outputBytes}

	_, _, _, err := extractVideoInfo(resp)
	if err == nil {
		t.Error("没有视频 URL 应返回错误")
	}
}

func TestExtractVideoInfo_InvalidOutputJSON(t *testing.T) {
	resp := &DashScopeResponse{Output: []byte("invalid json")}
	_, _, _, err := extractVideoInfo(resp)
	if err == nil {
		t.Error("无效 JSON 应返回错误")
	}
}

// ──────────────────────────── GenerateImage 请求体字段解析测试 ────────────────────────────

func TestGenerateImage_RequestBodyFields(t *testing.T) {
	// 验证发送到 DashScope 的请求体字段完全正确
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody = decodeRequestBody(t, r)

		resp := DashScopeResponse{StatusCode: 200, RequestID: "req-001"}
		output := MultiModalOutput{
			Choices: []MultiModalChoice{{
				FinishReason: "stop",
				Message:      &MultiModalMessage{Role: "assistant", Content: []ContentItem{{Image: "https://example.com/img.png"}}},
			}},
		}
		outputBytes, _ := json.Marshal(output)
		resp.Output = outputBytes
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewDashScopeModelClient(
		llmschema.NewModelRequestConfig(llmschema.WithModelName("wanx-v1")),
		llmschema.NewModelClientConfig("DashScope", "test-key", server.URL, llmschema.WithVerifySSL(false)),
	)

	msgs := []*llmschema.UserMessage{llmschema.NewUserMessage("一只猫")}
	_, _ = client.GenerateImage(context.Background(), msgs,
		model_clients.WithImageSize("1024*1024"),
		model_clients.WithImageN(2),
		model_clients.WithImageSeed(42),
		model_clients.WithImageNegativePrompt("blurry"),
		model_clients.WithImagePromptExtend(true),
		model_clients.WithImageWatermark(true),
	)

	// 验证请求体顶层字段
	if receivedBody["model"] != "wanx-v1" {
		t.Errorf("model = %v, want wanx-v1", receivedBody["model"])
	}

	// 验证 input.messages
	input, _ := receivedBody["input"].(map[string]any)
	if input == nil {
		t.Fatal("input 不应为 nil")
	}
	messages, _ := input["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("messages 长度 = %d, want 1", len(messages))
	}
	msg0, _ := messages[0].(map[string]any)
	if msg0["role"] != "user" {
		t.Errorf("messages[0].role = %v, want user", msg0["role"])
	}
	content, _ := msg0["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("content 长度 = %d, want 1", len(content))
	}
	contentItem, _ := content[0].(map[string]any)
	if contentItem["text"] != "一只猫" {
		t.Errorf("content[0].text = %v, want 一只猫", contentItem["text"])
	}

	// 验证 parameters
	params, _ := receivedBody["parameters"].(map[string]any)
	if params == nil {
		t.Fatal("parameters 不应为 nil")
	}
	if params["result_format"] != "message" {
		t.Errorf("parameters.result_format = %v, want message", params["result_format"])
	}
	if params["size"] != "1024*1024" {
		t.Errorf("parameters.size = %v, want 1024*1024", params["size"])
	}
	if params["n"] != float64(2) {
		t.Errorf("parameters.n = %v, want 2", params["n"])
	}
	if params["seed"] != float64(42) {
		t.Errorf("parameters.seed = %v, want 42", params["seed"])
	}
	if params["negative_prompt"] != "blurry" {
		t.Errorf("parameters.negative_prompt = %v, want blurry", params["negative_prompt"])
	}
	if params["prompt_extend"] != true {
		t.Errorf("parameters.prompt_extend = %v, want true", params["prompt_extend"])
	}
	if params["watermark"] != true {
		t.Errorf("parameters.watermark = %v, want true", params["watermark"])
	}
}

// ──────────────────────────── GenerateSpeech 请求体字段解析测试 ────────────────────────────

func TestGenerateSpeech_RequestBodyFields(t *testing.T) {
	// 验证语音生成请求体字段
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody = decodeRequestBody(t, r)

		resp := DashScopeResponse{StatusCode: 200, RequestID: "req-002"}
		output := AudioOutput{URL: "https://example.com/audio.mp3"}
		outputBytes, _ := json.Marshal(output)
		resp.Output = outputBytes
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewDashScopeModelClient(
		llmschema.NewModelRequestConfig(llmschema.WithModelName("cosyvoice-v1")),
		llmschema.NewModelClientConfig("DashScope", "test-key", server.URL, llmschema.WithVerifySSL(false)),
	)

	msgs := []*llmschema.UserMessage{llmschema.NewUserMessage("你好世界")}
	_, _ = client.GenerateSpeech(context.Background(), msgs,
		model_clients.WithSpeechVoice("Serena"),
		model_clients.WithSpeechLanguageType("Chinese"),
	)

	// 验证请求体
	if receivedBody["model"] != "cosyvoice-v1" {
		t.Errorf("model = %v, want cosyvoice-v1", receivedBody["model"])
	}

	input, _ := receivedBody["input"].(map[string]any)
	messages, _ := input["messages"].([]any)
	msg0, _ := messages[0].(map[string]any)
	content, _ := msg0["content"].([]any)
	contentItem, _ := content[0].(map[string]any)
	if contentItem["text"] != "你好世界" {
		t.Errorf("content[0].text = %v, want 你好世界", contentItem["text"])
	}

	params, _ := receivedBody["parameters"].(map[string]any)
	if params["voice"] != "Serena" {
		t.Errorf("parameters.voice = %v, want Serena", params["voice"])
	}
	if params["language_type"] != "Chinese" {
		t.Errorf("parameters.language_type = %v, want Chinese", params["language_type"])
	}
}

// ──────────────────────────── GenerateVideo 请求体字段解析测试 ────────────────────────────

func TestGenerateVideo_RequestBodyFields_T2V(t *testing.T) {
	// 文生视频请求体字段验证
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody = decodeRequestBody(t, r)

		resp := DashScopeResponse{StatusCode: 200, RequestID: "req-003"}
		output := VideoOutput{VideoURL: "https://example.com/video.mp4"}
		outputBytes, _ := json.Marshal(output)
		resp.Output = outputBytes
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewDashScopeModelClient(
		llmschema.NewModelRequestConfig(llmschema.WithModelName("wan2.6-t2v")),
		llmschema.NewModelClientConfig("DashScope", "test-key", server.URL, llmschema.WithVerifySSL(false)),
	)

	msgs := []*llmschema.UserMessage{llmschema.NewUserMessage("奔跑的猫")}
	_, _ = client.GenerateVideo(context.Background(), msgs,
		model_clients.WithVideoSize("1280*720"),
		model_clients.WithVideoDuration(5),
	)

	// 验证 input
	input, _ := receivedBody["input"].(map[string]any)
	if input["prompt"] != "奔跑的猫" {
		t.Errorf("input.prompt = %v, want 奔跑的猫", input["prompt"])
	}
	// 文生视频不应有 img_url
	if _, ok := input["img_url"]; ok {
		t.Error("文生视频不应包含 img_url")
	}

	// 验证 parameters
	params, _ := receivedBody["parameters"].(map[string]any)
	if params["size"] != "1280*720" {
		t.Errorf("parameters.size = %v, want 1280*720", params["size"])
	}
	if params["duration"] != float64(5) {
		t.Errorf("parameters.duration = %v, want 5", params["duration"])
	}
}

func TestGenerateVideo_RequestBodyFields_I2V(t *testing.T) {
	// 图生视频请求体字段验证
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody = decodeRequestBody(t, r)

		resp := DashScopeResponse{StatusCode: 200, RequestID: "req-004"}
		output := VideoOutput{VideoURL: "https://example.com/video.mp4"}
		outputBytes, _ := json.Marshal(output)
		resp.Output = outputBytes
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewDashScopeModelClient(
		llmschema.NewModelRequestConfig(llmschema.WithModelName("wan2.6-i2v")),
		llmschema.NewModelClientConfig("DashScope", "test-key", server.URL, llmschema.WithVerifySSL(false)),
	)

	msgs := []*llmschema.UserMessage{llmschema.NewUserMessage("让它动起来")}
	_, _ = client.GenerateVideo(context.Background(), msgs,
		model_clients.WithVideoImgURL("https://example.com/first-frame.jpg"),
		model_clients.WithVideoResolution("1080P"),
		model_clients.WithVideoNegativePrompt("blurry"),
	)

	// 验证 input 包含 img_url
	input, _ := receivedBody["input"].(map[string]any)
	if input["prompt"] != "让它动起来" {
		t.Errorf("input.prompt = %v, want 让它动起来", input["prompt"])
	}
	if input["img_url"] != "https://example.com/first-frame.jpg" {
		t.Errorf("input.img_url = %v, want first-frame URL", input["img_url"])
	}

	// 图生视频应使用 resolution 而非 size
	params, _ := receivedBody["parameters"].(map[string]any)
	if params["resolution"] != "1080P" {
		t.Errorf("parameters.resolution = %v, want 1080P", params["resolution"])
	}
	if params["negative_prompt"] != "blurry" {
		t.Errorf("parameters.negative_prompt = %v, want blurry", params["negative_prompt"])
	}
}

func TestGenerateVideo_RequestBodyFields_WithSeed(t *testing.T) {
	// 视频 seed 参数
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody = decodeRequestBody(t, r)

		resp := DashScopeResponse{StatusCode: 200, RequestID: "req-005"}
		output := VideoOutput{VideoURL: "https://example.com/video.mp4"}
		outputBytes, _ := json.Marshal(output)
		resp.Output = outputBytes
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewDashScopeModelClient(
		llmschema.NewModelRequestConfig(llmschema.WithModelName("wan2.6-t2v")),
		llmschema.NewModelClientConfig("DashScope", "test-key", server.URL, llmschema.WithVerifySSL(false)),
	)

	msgs := []*llmschema.UserMessage{llmschema.NewUserMessage("测试视频")}
	_, _ = client.GenerateVideo(context.Background(), msgs,
		model_clients.WithVideoSeed(123),
	)

	params, _ := receivedBody["parameters"].(map[string]any)
	if params["seed"] != float64(123) {
		t.Errorf("parameters.seed = %v, want 123", params["seed"])
	}
}

// ──────────────────────────── GenerateVideo 使用 VideoUsage 解析测试 ────────────────────────────

func TestGenerateVideo_WithUsageInResponse(t *testing.T) {
	// 响应包含 usage 信息 → 解析 duration 和 resolution
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := DashScopeResponse{StatusCode: 200, RequestID: "req-006"}
		output := VideoOutput{VideoURL: "https://example.com/video.mp4"}
		outputBytes, _ := json.Marshal(output)
		resp.Output = outputBytes
		usage := map[string]any{
			"duration": float64(10),
			"size":     "1920*1080",
		}
		usageBytes, _ := json.Marshal(usage)
		resp.Usage = usageBytes
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewDashScopeModelClient(
		llmschema.NewModelRequestConfig(llmschema.WithModelName("wan2.6-t2v")),
		llmschema.NewModelClientConfig("DashScope", "test-key", server.URL, llmschema.WithVerifySSL(false)),
	)

	msgs := []*llmschema.UserMessage{llmschema.NewUserMessage("测试")}
	result, err := client.GenerateVideo(context.Background(), msgs)
	if err != nil {
		t.Fatalf("GenerateVideo 失败: %v", err)
	}
	if result.Duration == nil || *result.Duration != 10 {
		t.Errorf("Duration = %v, want 10", result.Duration)
	}
	if result.Resolution != "1920*1080" {
		t.Errorf("Resolution = %q, want 1920*1080", result.Resolution)
	}
}

// ──────────────────────────── GenerateImage 多模态内容请求体测试 ────────────────────────────

func TestGenerateImage_MultiModalRequestBody(t *testing.T) {
	// 多模态内容（text + image_url）→ 验证请求体 content 格式
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody = decodeRequestBody(t, r)

		resp := DashScopeResponse{StatusCode: 200, RequestID: "req-007"}
		output := MultiModalOutput{
			Choices: []MultiModalChoice{{
				FinishReason: "stop",
				Message:      &MultiModalMessage{Role: "assistant", Content: []ContentItem{{Image: "https://example.com/gen.png"}}},
			}},
		}
		outputBytes, _ := json.Marshal(output)
		resp.Output = outputBytes
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewDashScopeModelClient(
		llmschema.NewModelRequestConfig(llmschema.WithModelName("qwen-image-max")),
		llmschema.NewModelClientConfig("DashScope", "test-key", server.URL, llmschema.WithVerifySSL(false)),
	)

	msgs := []*llmschema.UserMessage{
		llmschema.NewUserMessage("", llmschema.WithMultiModalContent(
			llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "https://example.com/cat.jpg"}},
			llmschema.ContentPart{Type: "text", Text: "描述这只猫"},
		)),
	}
	_, err := client.GenerateImage(context.Background(), msgs)
	if err != nil {
		t.Fatalf("GenerateImage 多模态失败: %v", err)
	}

	// 验证请求体中的 content 包含 image 和 text
	input, _ := receivedBody["input"].(map[string]any)
	messages, _ := input["messages"].([]any)
	msg0, _ := messages[0].(map[string]any)
	content, _ := msg0["content"].([]any)

	// 第一个应是 image
	item0, _ := content[0].(map[string]any)
	if _, ok := item0["image"]; !ok {
		t.Errorf("content[0] 应包含 image 键, got %v", item0)
	}
	// 第二个应是 text
	item1, _ := content[1].(map[string]any)
	if item1["text"] != "描述这只猫" {
		t.Errorf("content[1][\"text\"] = %v, want 描述这只猫", item1["text"])
	}
}

// ──────────────────────────── GenerateImage API 错误测试 ────────────────────────────

func TestGenerateImage_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := DashScopeResponse{
			StatusCode: 400,
			Code:       "InvalidParameter",
			Message:    "model is required",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewDashScopeModelClient(
		newTestModelConfig(),
		llmschema.NewModelClientConfig("DashScope", "test-key", server.URL, llmschema.WithVerifySSL(false)),
	)

	msgs := []*llmschema.UserMessage{llmschema.NewUserMessage("test")}
	result, err := client.GenerateImage(context.Background(), msgs)
	if err == nil {
		t.Error("API 错误应返回错误")
	}
	if result != nil {
		t.Error("错误时结果应为 nil")
	}
	errMsg := fmt.Sprintf("%v", err)
	if !strings.Contains(errMsg, "InvalidParameter") {
		t.Errorf("错误消息应包含 'InvalidParameter', got: %s", errMsg)
	}
}

func TestGenerateImage_NoImagesInResponse(t *testing.T) {
	// 响应成功但没有图片
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := DashScopeResponse{StatusCode: 200, RequestID: "req-008"}
		output := MultiModalOutput{
			Choices: []MultiModalChoice{{
				FinishReason: "stop",
				Message:      &MultiModalMessage{Role: "assistant", Content: []ContentItem{{Text: "无法生成图片"}}},
			}},
		}
		outputBytes, _ := json.Marshal(output)
		resp.Output = outputBytes
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewDashScopeModelClient(
		newTestModelConfig(),
		llmschema.NewModelClientConfig("DashScope", "test-key", server.URL, llmschema.WithVerifySSL(false)),
	)

	msgs := []*llmschema.UserMessage{llmschema.NewUserMessage("test")}
	_, err := client.GenerateImage(context.Background(), msgs)
	if err == nil {
		t.Error("响应没有图片应返回错误")
	}
}

func TestGenerateSpeech_NoAudioInResponse(t *testing.T) {
	// 响应成功但没有音频
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := DashScopeResponse{StatusCode: 200, RequestID: "req-009"}
		output := AudioOutput{}
		outputBytes, _ := json.Marshal(output)
		resp.Output = outputBytes
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewDashScopeModelClient(
		newTestModelConfig(),
		llmschema.NewModelClientConfig("DashScope", "test-key", server.URL, llmschema.WithVerifySSL(false)),
	)

	msgs := []*llmschema.UserMessage{llmschema.NewUserMessage("test")}
	_, err := client.GenerateSpeech(context.Background(), msgs)
	if err == nil {
		t.Error("响应没有音频应返回错误")
	}
}

func TestGenerateVideo_NoVideoInResponse(t *testing.T) {
	// 响应成功但没有视频 URL
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := DashScopeResponse{StatusCode: 200, RequestID: "req-010"}
		output := VideoOutput{VideoURL: ""}
		outputBytes, _ := json.Marshal(output)
		resp.Output = outputBytes
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewDashScopeModelClient(
		newTestModelConfig(),
		llmschema.NewModelClientConfig("DashScope", "test-key", server.URL, llmschema.WithVerifySSL(false)),
	)

	msgs := []*llmschema.UserMessage{llmschema.NewUserMessage("test")}
	_, err := client.GenerateVideo(context.Background(), msgs)
	if err == nil {
		t.Error("响应没有视频 URL 应返回错误")
	}
}

// ──────────────────────────── GenerateVideo EmptyContent 测试 ────────────────────────────

func TestGenerateVideo_Validation_EmptyContent(t *testing.T) {
	client, _ := NewDashScopeModelClient(
		newTestModelConfig(),
		newTestClientConfig("DashScope", "test-key", "https://dashscope.aliyuncs.com/compatible-mode/v1"),
	)

	msgs := []*llmschema.UserMessage{llmschema.NewUserMessage("  ")}
	_, err := client.GenerateVideo(context.Background(), msgs)
	if err == nil {
		t.Error("空内容应返回错误")
	}
}

// ──────────────────────────── GenerateVideo I2V 使用 Size 而非 Resolution ────────────────────────────

func TestGenerateVideo_I2V_WithSizeFallback(t *testing.T) {
	// 图生视频模式，只提供 size 不提供 resolution → 应使用 size
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody = decodeRequestBody(t, r)

		resp := DashScopeResponse{StatusCode: 200, RequestID: "req-011"}
		output := VideoOutput{VideoURL: "https://example.com/video.mp4"}
		outputBytes, _ := json.Marshal(output)
		resp.Output = outputBytes
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewDashScopeModelClient(
		llmschema.NewModelRequestConfig(llmschema.WithModelName("wan2.6-i2v")),
		llmschema.NewModelClientConfig("DashScope", "test-key", server.URL, llmschema.WithVerifySSL(false)),
	)

	msgs := []*llmschema.UserMessage{llmschema.NewUserMessage("test")}
	_, _ = client.GenerateVideo(context.Background(), msgs,
		model_clients.WithVideoImgURL("https://example.com/frame.jpg"),
		model_clients.WithVideoSize("1280*720"),
	)

	params, _ := receivedBody["parameters"].(map[string]any)
	if params["size"] != "1280*720" {
		t.Errorf("parameters.size = %v, want 1280*720", params["size"])
	}
}

// ──────────────────────────── GenerateVideo T2V 使用 Resolution 而非 Size ────────────────────────────

func TestGenerateVideo_T2V_WithResolutionFallback(t *testing.T) {
	// 文生视频模式，只提供 resolution 不提供 size → 应使用 resolution
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody = decodeRequestBody(t, r)

		resp := DashScopeResponse{StatusCode: 200, RequestID: "req-012"}
		output := VideoOutput{VideoURL: "https://example.com/video.mp4"}
		outputBytes, _ := json.Marshal(output)
		resp.Output = outputBytes
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewDashScopeModelClient(
		llmschema.NewModelRequestConfig(llmschema.WithModelName("wan2.6-t2v")),
		llmschema.NewModelClientConfig("DashScope", "test-key", server.URL, llmschema.WithVerifySSL(false)),
	)

	msgs := []*llmschema.UserMessage{llmschema.NewUserMessage("test")}
	_, _ = client.GenerateVideo(context.Background(), msgs,
		model_clients.WithVideoResolution("1080P"),
	)

	params, _ := receivedBody["parameters"].(map[string]any)
	if params["resolution"] != "1080P" {
		t.Errorf("parameters.resolution = %v, want 1080P", params["resolution"])
	}
}

// ──────────────────────────── GenerateVideo 带 AudioURL ────────────────────────────

func TestGenerateVideo_WithAudioURL(t *testing.T) {
	// 视频生成带音频 URL
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody = decodeRequestBody(t, r)

		resp := DashScopeResponse{StatusCode: 200, RequestID: "req-013"}
		output := VideoOutput{VideoURL: "https://example.com/video.mp4"}
		outputBytes, _ := json.Marshal(output)
		resp.Output = outputBytes
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewDashScopeModelClient(
		llmschema.NewModelRequestConfig(llmschema.WithModelName("wan2.6-t2v")),
		llmschema.NewModelClientConfig("DashScope", "test-key", server.URL, llmschema.WithVerifySSL(false)),
	)

	msgs := []*llmschema.UserMessage{llmschema.NewUserMessage("test")}
	_, _ = client.GenerateVideo(context.Background(), msgs,
		model_clients.WithVideoAudioURL("https://example.com/bgm.mp3"),
	)

	input, _ := receivedBody["input"].(map[string]any)
	if input["audio_url"] != "https://example.com/bgm.mp3" {
		t.Errorf("input.audio_url = %v, want bgm URL", input["audio_url"])
	}
}

// ──────────────────────────── GenerateSpeech 带 Extra 参数 ────────────────────────────

func TestGenerateSpeech_WithExtraParams(t *testing.T) {
	// 语音生成带额外参数
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody = decodeRequestBody(t, r)

		resp := DashScopeResponse{StatusCode: 200, RequestID: "req-014"}
		output := AudioOutput{URL: "https://example.com/audio.mp3"}
		outputBytes, _ := json.Marshal(output)
		resp.Output = outputBytes
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := NewDashScopeModelClient(
		llmschema.NewModelRequestConfig(llmschema.WithModelName("cosyvoice-v1")),
		llmschema.NewModelClientConfig("DashScope", "test-key", server.URL, llmschema.WithVerifySSL(false)),
	)

	msgs := []*llmschema.UserMessage{llmschema.NewUserMessage("test")}
	_, _ = client.GenerateSpeech(context.Background(), msgs,
		model_clients.WithSpeechExtra(map[string]any{"format": "wav", "sample_rate": 16000}),
	)

	params, _ := receivedBody["parameters"].(map[string]any)
	if params["format"] != "wav" {
		t.Errorf("parameters.format = %v, want wav", params["format"])
	}
	if params["sample_rate"] != float64(16000) {
		t.Errorf("parameters.sample_rate = %v, want 16000", params["sample_rate"])
	}
}
