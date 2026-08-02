package multimodal

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	modelclients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 辅助函数 ────────────────────────────

// mockVisionClient 用于测试的 mock 视觉模型客户端
type mockVisionClient struct {
	// responses 每次调用返回的结果序列（支持模拟重试场景）
	responses []mockVisionResponse
	// callCount 已调用次数
	callCount int
}

type mockVisionResponse struct {
	text  string
	err   error
}

func (m *mockVisionClient) Invoke(_ context.Context, _ modelclients.MessagesParam, _ ...modelclients.InvokeOption) (*llmschema.AssistantMessage, error) {
	idx := m.callCount
	m.callCount++
	if idx >= len(m.responses) {
		idx = len(m.responses) - 1
	}
	resp := m.responses[idx]
	if resp.err != nil {
		return nil, resp.err
	}
	msg := llmschema.NewAssistantMessage(resp.text)
	return msg, nil
}

func (m *mockVisionClient) Stream(_ context.Context, _ modelclients.MessagesParam, _ ...modelclients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, exception.NewBaseError(exception.StatusModelCallFailed, exception.WithMsg("mock does not support stream"))
}

func (m *mockVisionClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...modelclients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, exception.NewBaseError(exception.StatusModelCallFailed, exception.WithMsg("mock does not support image generation"))
}

func (m *mockVisionClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...modelclients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, exception.NewBaseError(exception.StatusModelCallFailed, exception.WithMsg("mock does not support speech generation"))
}

func (m *mockVisionClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...modelclients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, exception.NewBaseError(exception.StatusModelCallFailed, exception.WithMsg("mock does not support video generation"))
}

func (m *mockVisionClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, exception.NewBaseError(exception.StatusModelCallFailed, exception.WithMsg("mock does not support audio transcription"))
}

func (m *mockVisionClient) Release(_ context.Context, _ ...modelclients.ReleaseOption) (bool, error) {
	return false, nil
}

func (m *mockVisionClient) SupportsKVCacheRelease() bool {
	return false
}

// newTestVisionConfig 创建测试用的视觉模型配置
func newTestVisionConfig() *hschema.VisionModelConfig {
	return &hschema.VisionModelConfig{
		APIKey:     "test-api-key",
		BaseURL:    "https://api.openai.com/v1",
		Model:      "gpt-4o",
		MaxRetries: 3,
	}
}

// ──────────────────────────── BuildImageContent 测试 ────────────────────────────

func TestBuildImageContent_HTTPURL(t *testing.T) {
	content, err := BuildImageContent("https://example.com/image.png")
	if err != nil {
		t.Fatalf("BuildImageContent HTTP URL 返回错误: %v", err)
	}
	if content.Type != "image_url" {
		t.Errorf("Type = %q, 期望 %q", content.Type, "image_url")
	}
	if content.ImageURL == nil {
		t.Fatal("ImageURL 不应为 nil")
	}
	if content.ImageURL.URL != "https://example.com/image.png" {
		t.Errorf("URL = %q, 期望 %q", content.ImageURL.URL, "https://example.com/image.png")
	}
}

func TestBuildImageContent_本地文件(t *testing.T) {
	// 创建临时 PNG 文件（使用有效的 PNG 头部）
	tmpDir := t.TempDir()
	pngFile := filepath.Join(tmpDir, "test.png")
	// PNG 签名: 89504e470d0a1a0a
	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	padding := make([]byte, 504)
	pngData := append(pngHeader, padding...) // 至少 512 bytes for DetectContentType
	if err := os.WriteFile(pngFile, pngData, 0644); err != nil {
		t.Fatalf("创建临时 PNG 文件失败: %v", err)
	}

	content, err := BuildImageContent(pngFile)
	if err != nil {
		t.Fatalf("BuildImageContent 本地文件返回错误: %v", err)
	}
	if content.Type != "image_url" {
		t.Errorf("Type = %q, 期望 %q", content.Type, "image_url")
	}
	if content.ImageURL == nil {
		t.Fatal("ImageURL 不应为 nil")
	}
	if !strings.HasPrefix(content.ImageURL.URL, "data:image/png;base64,") {
		t.Errorf("URL 应以 'data:image/png;base64,' 开头, got %q", content.ImageURL.URL[:50])
	}
	// 验证 base64 内容可解码
	encoded := strings.TrimPrefix(content.ImageURL.URL, "data:image/png;base64,")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("base64 解码失败: %v", err)
	}
	if len(decoded) != len(pngData) {
		t.Errorf("解码后大小 = %d, 期望 %d", len(decoded), len(pngData))
	}
}

func TestBuildImageContent_Sandbox路径(t *testing.T) {
	_, err := BuildImageContent("/home/user/sandbox/image.png")
	if err == nil {
		t.Error("sandbox 路径应返回错误")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误应为 BaseError 类型, got %T", err)
	}
	if !strings.Contains(baseErr.Error(), "sandbox-only") {
		t.Errorf("错误消息应包含 'sandbox-only', got %q", baseErr.Error())
	}
}

func TestBuildImageContent_文件不存在(t *testing.T) {
	_, err := BuildImageContent("/nonexistent/image.png")
	if err == nil {
		t.Error("文件不存在应返回错误")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误应为 BaseError 类型, got %T", err)
	}
	if !strings.Contains(baseErr.Error(), "does not exist") {
		t.Errorf("错误消息应包含 'does not exist', got %q", baseErr.Error())
	}
}

func TestBuildImageContent_大小超限(t *testing.T) {
	// 创建超过 20MB 的文件（用 t.TempDir）
	tmpDir := t.TempDir()
	bigFile := filepath.Join(tmpDir, "big.png")
	// 写入一个超过 maxImageFileSize 的空文件
	bigData := make([]byte, maxImageFileSize+1)
	if err := os.WriteFile(bigFile, bigData, 0644); err != nil {
		t.Fatalf("创建大文件失败: %v", err)
	}

	_, err := BuildImageContent(bigFile)
	if err == nil {
		t.Error("超过大小限制应返回错误")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误应为 BaseError 类型, got %T", err)
	}
	if !strings.Contains(baseErr.Error(), "大小限制") {
		t.Errorf("错误消息应包含 '大小限制', got %q", baseErr.Error())
	}
}

// ──────────────────────────── CallVisionModel 测试 ────────────────────────────

func TestCallVisionModel_成功(t *testing.T) {
	imageContent := llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "https://img.png"}}
	mockClient := &mockVisionClient{
		responses: []mockVisionResponse{{text: "OCR result text"}},
	}
	config := newTestVisionConfig()

	text, model, err := CallVisionModel(context.Background(), mockClient, imageContent, defaultOCRPrompt, config)
	if err != nil {
		t.Fatalf("CallVisionModel 返回错误: %v", err)
	}
	if text != "OCR result text" {
		t.Errorf("text = %q, 期望 %q", text, "OCR result text")
	}
	if model != "gpt-4o" {
		t.Errorf("model = %q, 期望 %q", model, "gpt-4o")
	}
}

func TestCallVisionModel_配置无效(t *testing.T) {
	imageContent := llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "https://img.png"}}
	mockClient := &mockVisionClient{}

	// nil config
	_, _, err := CallVisionModel(context.Background(), mockClient, imageContent, "prompt", nil)
	if err == nil {
		t.Error("nil config 应返回错误")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误应为 BaseError 类型, got %T", err)
	}
	if baseErr.Status() != exception.StatusToolMultimodalVisionConfigInvalid {
		t.Errorf("Status = %v, 期望 %v", baseErr.Status(), exception.StatusToolMultimodalVisionConfigInvalid)
	}

	// 空 APIKey
	config := &hschema.VisionModelConfig{BaseURL: "https://api.openai.com/v1", Model: "gpt-4o"}
	_, _, err = CallVisionModel(context.Background(), mockClient, imageContent, "prompt", config)
	if err == nil {
		t.Error("空 APIKey 应返回错误")
	}
}

func TestCallVisionModel_空响应(t *testing.T) {
	imageContent := llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "https://img.png"}}
	mockClient := &mockVisionClient{
		responses: []mockVisionResponse{{text: ""}}, // 返回空文本
	}
	config := newTestVisionConfig()

	_, _, err := CallVisionModel(context.Background(), mockClient, imageContent, "prompt", config)
	if err == nil {
		t.Error("空响应应返回错误")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误应为 BaseError 类型, got %T", err)
	}
	if baseErr.Status() != exception.StatusToolMultimodalVisionInvokeFailed {
		t.Errorf("Status = %v, 期望 %v", baseErr.Status(), exception.StatusToolMultimodalVisionInvokeFailed)
	}
}

func TestCallVisionModel_重试成功(t *testing.T) {
	imageContent := llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "https://img.png"}}
	// 前2次返回 429 错误，第3次成功
	mockClient := &mockVisionClient{
		responses: []mockVisionResponse{
			{err: exception.NewBaseError(exception.StatusModelCallFailed, exception.WithMsg("OpenAI API HTTP 429: rate limit"))},
			{err: exception.NewBaseError(exception.StatusModelCallFailed, exception.WithMsg("OpenAI API HTTP 429: rate limit"))},
			{text: "success result"},
		},
	}
	config := newTestVisionConfig()

	text, model, err := CallVisionModel(context.Background(), mockClient, imageContent, "prompt", config)
	if err != nil {
		t.Fatalf("CallVisionModel 重试后应成功: %v", err)
	}
	if text != "success result" {
		t.Errorf("text = %q, 期望 %q", text, "success result")
	}
	if model != "gpt-4o" {
		t.Errorf("model = %q, 期望 %q", model, "gpt-4o")
	}
	if mockClient.callCount != 3 {
		t.Errorf("callCount = %d, 期望 3（2次重试 + 1次成功）", mockClient.callCount)
	}
}

func TestCallVisionModel_不可重试错误(t *testing.T) {
	imageContent := llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "https://img.png"}}
	// 返回 401 错误（不可重试）
	mockClient := &mockVisionClient{
		responses: []mockVisionResponse{
			{err: exception.NewBaseError(exception.StatusModelCallFailed, exception.WithMsg("OpenAI API HTTP 401: unauthorized"))},
		},
	}
	config := newTestVisionConfig()

	_, _, err := CallVisionModel(context.Background(), mockClient, imageContent, "prompt", config)
	if err == nil {
		t.Error("不可重试错误应返回错误")
	}
	if mockClient.callCount != 1 {
		t.Errorf("callCount = %d, 期望 1（不重试）", mockClient.callCount)
	}
}

// ──────────────────────────── ExtractResponseText 测试 ────────────────────────────

func TestExtractResponseText_纯文本(t *testing.T) {
	msg := llmschema.NewAssistantMessage("Hello World")
	text := ExtractResponseText(msg)
	if text != "Hello World" {
		t.Errorf("text = %q, 期望 %q", text, "Hello World")
	}
}

func TestExtractResponseText_空消息(t *testing.T) {
	text := ExtractResponseText(nil)
	if text != "" {
		t.Errorf("text = %q, 期望空字符串", text)
	}
}

func TestExtractResponseText_带前后空白(t *testing.T) {
	msg := llmschema.NewAssistantMessage("  padded text  ")
	text := ExtractResponseText(msg)
	if text != "padded text" {
		t.Errorf("text = %q, 期望 %q", text, "padded text")
	}
}

// ──────────────────────────── isHTTPURL 测试 ────────────────────────────

func TestIsHTTPURL_HTTP(t *testing.T) {
	if !isHTTPURL("http://example.com") {
		t.Error("http URL 应返回 true")
	}
}

func TestIsHTTPURL_HTTPS(t *testing.T) {
	if !isHTTPURL("https://example.com/path") {
		t.Error("https URL 应返回 true")
	}
}

func TestIsHTTPURL_本地路径(t *testing.T) {
	if isHTTPURL("/local/path/image.png") {
		t.Error("本地路径应返回 false")
	}
}

func TestIsHTTPURL_空字符串(t *testing.T) {
	if isHTTPURL("") {
		t.Error("空字符串应返回 false")
	}
}

// ──────────────────────────── guessImageMIMEType 测试 ────────────────────────────

func TestGuessImageMIMEType_PNG数据(t *testing.T) {
	// PNG 签名字节（89504e470d0a1a0a）+ 填充至 512+
	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	padding := make([]byte, 504)
	data := append(pngHeader, padding...)
	result := guessImageMIMEType("test.png", data)
	if result != "image/png" {
		t.Errorf("result = %q, 期望 %q", result, "image/png")
	}
}

func TestGuessImageMIMEType_JPEG数据(t *testing.T) {
	// JPEG 签名字节（ffd8ff）
	jpegHeader := []byte{0xFF, 0xD8, 0xFF, 0xE0}
	padding := make([]byte, 508)
	data := append(jpegHeader, padding...)
	result := guessImageMIMEType("test.jpg", data)
	if result != "image/jpeg" {
		t.Errorf("result = %q, 期望 %q", result, "image/jpeg")
	}
}

func TestGuessImageMIMEType_扩展名降级(t *testing.T) {
	// 非图片数据 → 降级到扩展名
	data := make([]byte, 512) // 全零数据 → http.DetectContentType 返回 application/octet-stream
	result := guessImageMIMEType("test.gif", data)
	if result != "image/gif" {
		t.Errorf("result = %q, 期望 %q (扩展名降级)", result, "image/gif")
	}
}

func TestGuessImageMIMEType_未知扩展名(t *testing.T) {
	data := make([]byte, 100) // 不够 512 字节，走扩展名降级，未知扩展名 → image/jpeg
	result := guessImageMIMEType("test.xyz", data)
	if result != "image/jpeg" {
		t.Errorf("result = %q, 期望 %q (最终降级)", result, "image/jpeg")
	}
}

// ──────────────────────────── isRetryableError 测试 ────────────────────────────

func TestIsRetryableError_429(t *testing.T) {
	if !isRetryableError("HTTP 429 rate limit") {
		t.Error("429 应为可重试错误")
	}
}

func TestIsRetryableError_500(t *testing.T) {
	if !isRetryableError("HTTP 500 internal server error") {
		t.Error("500 应为可重试错误")
	}
}

func TestIsRetryableError_401(t *testing.T) {
	if isRetryableError("HTTP 401 unauthorized") {
		t.Error("401 不应为可重试错误")
	}
}

func TestIsRetryableError_无状态码(t *testing.T) {
	if isRetryableError("connection refused") {
		t.Error("无状态码不应为可重试错误")
	}
}
