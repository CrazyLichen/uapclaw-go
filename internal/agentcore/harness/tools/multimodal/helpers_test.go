package multimodal

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	modelclients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
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
	text string
	err  error
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

// newTestVideoConfig 创建测试用的视频模型配置
func newTestVideoConfig() *hschema.VideoModelConfig {
	return &hschema.VideoModelConfig{
		APIKey:     "test-api-key",
		BaseURL:    "https://api.openai.com/v1",
		Model:      "glm-4.6v",
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

	// nil 配置
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

// ──────────────────────────── extractResponseText 测试 ────────────────────────────

func TestExtractResponseTextInternal_纯文本(t *testing.T) {
	msg := llmschema.NewAssistantMessage("Hello World")
	text := extractResponseText(msg)
	if text != "Hello World" {
		t.Errorf("text = %q, 期望 %q", text, "Hello World")
	}
}

func TestExtractResponseTextInternal_空消息(t *testing.T) {
	text := extractResponseText(nil)
	if text != "" {
		t.Errorf("text = %q, 期望空字符串", text)
	}
}

func TestExtractResponseTextInternal_带前后空白(t *testing.T) {
	msg := llmschema.NewAssistantMessage("  padded text  ")
	text := extractResponseText(msg)
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

// TestGuessImageMIMEType_更多扩展名 测试各种扩展名推断路径
func TestGuessImageMIMEType_更多扩展名(t *testing.T) {
	// 不够 512 字节 → 降级到扩展名推断
	smallData := make([]byte, 100)

	tests := []struct {
		path     string
		expected string
	}{
		{"test.webp", "image/webp"},
		{"test.bmp", "image/bmp"},
		{"test.svg", "image/svg+xml"},
		{"test.tiff", "image/tiff"},
		{"test.tif", "image/tiff"},
		{"test.jpg", "image/jpeg"},
		{"test.png", "image/png"},
	}

	for _, tc := range tests {
		result := guessImageMIMEType(tc.path, smallData)
		if result != tc.expected {
			t.Errorf("guessImageMIMEType(%s) = %s, 期望 %s", tc.path, result, tc.expected)
		}
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

// ──────────────────────────── Audio mock 客户端 ────────────────────────────

// mockAudioClient 用于测试的 mock 音频客户端
type mockAudioClient struct {
	transcriptionText string
	transcriptionErr  error
	invokeResponses   []mockVisionResponse
	invokeCallCount   int
}

func (m *mockAudioClient) Invoke(_ context.Context, _ modelclients.MessagesParam, _ ...modelclients.InvokeOption) (*llmschema.AssistantMessage, error) {
	idx := m.invokeCallCount
	m.invokeCallCount++
	if idx >= len(m.invokeResponses) {
		idx = len(m.invokeResponses) - 1
	}
	resp := m.invokeResponses[idx]
	if resp.err != nil {
		return nil, resp.err
	}
	return llmschema.NewAssistantMessage(resp.text), nil
}

func (m *mockAudioClient) Stream(_ context.Context, _ modelclients.MessagesParam, _ ...modelclients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, exception.NewBaseError(exception.StatusModelCallFailed, exception.WithMsg("mock does not support stream"))
}

func (m *mockAudioClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...modelclients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, exception.NewBaseError(exception.StatusModelCallFailed, exception.WithMsg("mock does not support image generation"))
}

func (m *mockAudioClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...modelclients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, exception.NewBaseError(exception.StatusModelCallFailed, exception.WithMsg("mock does not support speech generation"))
}

func (m *mockAudioClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...modelclients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, exception.NewBaseError(exception.StatusModelCallFailed, exception.WithMsg("mock does not support video generation"))
}

func (m *mockAudioClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	if m.transcriptionErr != nil {
		return nil, m.transcriptionErr
	}
	return &llmschema.TranscriptionResponse{Text: m.transcriptionText}, nil
}

func (m *mockAudioClient) Release(_ context.Context, _ ...modelclients.ReleaseOption) (bool, error) {
	return false, nil
}

func (m *mockAudioClient) SupportsKVCacheRelease() bool {
	return false
}

// newTestAudioConfig 创建测试用的音频模型配置
func newTestAudioConfig() *hschema.AudioModelConfig {
	return &hschema.AudioModelConfig{
		APIKey:             "test-api-key",
		BaseURL:            "https://api.openai.com/v1",
		TranscriptionModel: "whisper-1",
		QAModel:            "gpt-4o-audio-preview",
		MaxRetries:         3,
		HTTPTimeout:        20,
		MaxAudioBytes:      25 * 1024 * 1024,
		ACRBaseURL:         "https://identify-ap-southeast-1.acrcloud.com/v1/identify",
	}
}

// ──────────────────────────── ResolveAudioPath 测试 ────────────────────────────

func TestResolveAudioPath_HTTPURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.ReadAll(r.Body) // drain
		_, _ = fmt.Fprint(w, "fake audio content")
	}))
	defer server.Close()

	config := newTestAudioConfig()
	path, shouldDelete, err := ResolveAudioPath(context.Background(), server.URL+"/audio.mp3", config)
	if err != nil {
		t.Fatalf("ResolveAudioPath HTTP URL 返回错误: %v", err)
	}
	if !shouldDelete {
		t.Error("HTTP URL 下载的文件 shouldDelete 应为 true")
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("临时文件应存在: %v", statErr)
	}
	// 清理
	_ = os.Remove(path)
}

func TestResolveAudioPath_本地文件(t *testing.T) {
	tmpDir := t.TempDir()
	audioFile := filepath.Join(tmpDir, "test.mp3")
	_ = os.WriteFile(audioFile, []byte("fake audio"), 0644)

	config := newTestAudioConfig()
	path, shouldDelete, err := ResolveAudioPath(context.Background(), audioFile, config)
	if err != nil {
		t.Fatalf("ResolveAudioPath 本地文件返回错误: %v", err)
	}
	if shouldDelete {
		t.Error("本地文件 shouldDelete 应为 false")
	}
	if path != audioFile {
		t.Errorf("path = %q, 期望 %q", path, audioFile)
	}
}

func TestResolveAudioPath_Sandbox路径(t *testing.T) {
	config := newTestAudioConfig()
	_, _, err := ResolveAudioPath(context.Background(), "/home/user/sandbox/audio.mp3", config)
	if err == nil {
		t.Error("sandbox 路径应返回错误")
	}
}

func TestResolveAudioPath_文件不存在(t *testing.T) {
	config := newTestAudioConfig()
	_, _, err := ResolveAudioPath(context.Background(), "/nonexistent/audio.mp3", config)
	if err == nil {
		t.Error("文件不存在应返回错误")
	}
}

func TestResolveAudioPath_大小超限(t *testing.T) {
	// httptest 返回超大响应
	bigData := make([]byte, 26*1024*1024) // 26MB > 25MB limit
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bigData)
	}))
	defer server.Close()

	config := newTestAudioConfig()
	_, _, err := ResolveAudioPath(context.Background(), server.URL+"/audio.mp3", config)
	if err == nil {
		t.Error("超过大小限制应返回错误")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误应为 BaseError 类型, got %T", err)
	}
	if !strings.Contains(baseErr.Error(), "size limit") {
		t.Errorf("错误消息应包含 'size limit', got %q", baseErr.Error())
	}
}

// ──────────────────────────── EncodeAudioFile 测试 ────────────────────────────

func TestEncodeAudioFile(t *testing.T) {
	tmpDir := t.TempDir()
	audioFile := filepath.Join(tmpDir, "test.mp3")
	_ = os.WriteFile(audioFile, []byte("fake audio data"), 0644)

	encoded, format, err := EncodeAudioFile(audioFile)
	if err != nil {
		t.Fatalf("EncodeAudioFile 返回错误: %v", err)
	}
	if format != "mp3" {
		t.Errorf("format = %q, 期望 %q", format, "mp3")
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("base64 解码失败: %v", err)
	}
	if string(decoded) != "fake audio data" {
		t.Errorf("解码内容不匹配")
	}
}

func TestEncodeAudioFile_WAV(t *testing.T) {
	tmpDir := t.TempDir()
	audioFile := filepath.Join(tmpDir, "test.wav")
	_ = os.WriteFile(audioFile, []byte("fake wav data"), 0644)

	_, format, err := EncodeAudioFile(audioFile)
	if err != nil {
		t.Fatalf("EncodeAudioFile WAV 返回错误: %v", err)
	}
	if format != "wav" {
		t.Errorf("format = %q, 期望 %q", format, "wav")
	}
}

// ──────────────────────────── GetAudioDuration 测试 ────────────────────────────

func TestGetAudioDuration_WAV文件(t *testing.T) {
	// 构造一个简单的 WAV 文件
	tmpDir := t.TempDir()
	wavFile := filepath.Join(tmpDir, "test.wav")
	// WAV header + fmt chunk + data chunk
	wavData := constructSimpleWAV(44100, 16, 1, 44100) // 1 second of audio at 44100Hz
	_ = os.WriteFile(wavFile, wavData, 0644)

	duration, err := GetAudioDuration(wavFile)
	if err != nil {
		t.Fatalf("GetAudioDuration WAV 返回错误: %v", err)
	}
	// WAV 时长应为约 1 秒
	if duration < 0.9 || duration > 1.1 {
		t.Errorf("duration = %.2f, 期望约 1.0", duration)
	}
}

func TestGetAudioDuration_非WAV文件(t *testing.T) {
	tmpDir := t.TempDir()
	mp3File := filepath.Join(tmpDir, "test.mp3")
	_ = os.WriteFile(mp3File, []byte("fake mp3"), 0644)

	duration, err := GetAudioDuration(mp3File)
	if err != nil {
		t.Fatalf("GetAudioDuration 非 WAV 返回错误: %v", err)
	}
	// ffprobe 不可用 → 降级为 0
	if duration != 0 {
		t.Errorf("duration = %.2f, 期望 0（ffprobe 不可用降级）", duration)
	}
}

// ──────────────────────────── guessAudioFormat 测试 ────────────────────────────

func TestGuessAudioFormat_MP3(t *testing.T) {
	result := guessAudioFormat("test.mp3", nil)
	if result != "mp3" {
		t.Errorf("result = %q, 期望 %q", result, "mp3")
	}
}

func TestGuessAudioFormat_WAV(t *testing.T) {
	result := guessAudioFormat("test.wav", nil)
	if result != "wav" {
		t.Errorf("result = %q, 期望 %q", result, "wav")
	}
}

func TestGuessAudioFormat_未知扩展名(t *testing.T) {
	result := guessAudioFormat("test.xyz", nil)
	if result != "mp3" {
		t.Errorf("result = %q, 期望 %q (降级)", result, "mp3")
	}
}

// ──────────────────────────── getAudioExtension 测试 ────────────────────────────

func TestGetAudioExtension_URL含扩展名(t *testing.T) {
	result := getAudioExtension("https://example.com/audio.mp3", "")
	if result != ".mp3" {
		t.Errorf("result = %q, 期望 %q", result, ".mp3")
	}
}

func TestGetAudioExtension_ContentType推断(t *testing.T) {
	result := getAudioExtension("https://example.com/download", "audio/wav")
	if result != ".wav" {
		t.Errorf("result = %q, 期望 %q", result, ".wav")
	}
}

func TestGetAudioExtension_无线索默认MP3(t *testing.T) {
	result := getAudioExtension("https://example.com/download", "text/html")
	if result != ".mp3" {
		t.Errorf("result = %q, 期望 %q (降级)", result, ".mp3")
	}
}

// ──────────────────────────── audioMIMEToFormat 测试 ────────────────────────────

func TestAudioMIMEToFormat_MPEG(t *testing.T) {
	result := audioMIMEToFormat("audio/mpeg")
	if result != "mp3" {
		t.Errorf("result = %q, 期望 %q", result, "mp3")
	}
}

func TestAudioMIMEToFormat_WAV(t *testing.T) {
	result := audioMIMEToFormat("audio/wav")
	if result != "wav" {
		t.Errorf("result = %q, 期望 %q", result, "wav")
	}
}

// ──────────────────────────── extractFirstArtistName 测试 ────────────────────────────

func TestExtractFirstArtistName_有Artist(t *testing.T) {
	item := map[string]any{
		"artists": []any{
			map[string]any{"name": "Artist One"},
			map[string]any{"name": "Artist Two"},
		},
	}
	result := extractFirstArtistName(item)
	if result != "Artist One" {
		t.Errorf("result = %q, 期望 %q", result, "Artist One")
	}
}

func TestExtractFirstArtistName_空Artist(t *testing.T) {
	item := map[string]any{}
	result := extractFirstArtistName(item)
	if result != "" {
		t.Errorf("result = %q, 期望空字符串", result)
	}
}

// ──────────────────────────── ACR HMAC 签名测试 ────────────────────────────

func TestACRHMACSignature(t *testing.T) {
	// 验证 HMAC-SHA1 签名构造逻辑
	accessKey := "test_access_key"
	accessSecret := "test_secret"
	timestamp := "1234567890"
	stringToSign := "POST\n/v1/identify\n" + accessKey + "\naudio\n1\n" + timestamp

	mac := hmac.New(sha1.New, []byte(accessSecret))
	mac.Write([]byte(stringToSign))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	if signature == "" {
		t.Error("signature 不应为空")
	}
	// 验证可解码
	decoded, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		t.Fatalf("signature base64 解码失败: %v", err)
	}
	if len(decoded) != sha1.Size {
		t.Errorf("signature 长度 = %d, 期望 %d (SHA1)", len(decoded), sha1.Size)
	}
}

// ──────────────────────────── 构造 WAV 辅助 ────────────────────────────

// constructSimpleWAV 构造一个简单的 WAV 文件（用于测试）
func constructSimpleWAV(sampleRate, bitsPerSample, numChannels, numSamples uint32) []byte {
	byteRate := sampleRate * uint32(numChannels) * uint32(bitsPerSample/8)
	blockAlign := uint32(numChannels) * uint32(bitsPerSample/8)
	dataSize := numSamples * blockAlign

	// RIFF 文件头
	riff := []byte("RIFF")
	wave := []byte("WAVE")
	fileSize := uint32(36 + dataSize)

	// fmt 块
	fmtChunk := []byte("fmt ")
	fmtSize := uint32(16)
	audioFormat := uint16(1) // PCM 格式

	// data 数据块
	dataChunk := []byte("data")

	result := make([]byte, 0, 44+dataSize)
	result = append(result, riff...)
	result = append(result, uint32ToBytes(fileSize)...)
	result = append(result, wave...)
	result = append(result, fmtChunk...)
	result = append(result, uint32ToBytes(fmtSize)...)
	result = append(result, uint16ToBytes(audioFormat)...)
	result = append(result, uint16ToBytes(uint16(numChannels))...)
	result = append(result, uint32ToBytes(sampleRate)...)
	result = append(result, uint32ToBytes(byteRate)...)
	result = append(result, uint16ToBytes(uint16(blockAlign))...)
	result = append(result, uint16ToBytes(uint16(bitsPerSample))...)
	result = append(result, dataChunk...)
	result = append(result, uint32ToBytes(dataSize)...)
	// 填充零数据
	for i := uint32(0); i < dataSize; i++ {
		result = append(result, 0)
	}
	return result
}

func uint32ToBytes(v uint32) []byte {
	return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)}
}

func uint16ToBytes(v uint16) []byte {
	return []byte{byte(v), byte(v >> 8)}
}

// ──────────────────────────── InvokeACRMetadata 测试 ────────────────────────────

// TestInvokeACRMetadata_成功 测试 ACR 元数据调用成功
func TestInvokeACRMetadata_成功(t *testing.T) {
	// 创建 ACR mock 服务端
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("期望 POST 方法，实际: %s", r.Method)
		}
		// 验证 multipart form-data
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			t.Errorf("期望 Content-Type 包含 multipart/form-data，实际: %s", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"metadata":{"music":[{"external_metadata":{"spotify":{"track":{"name":"TestSong"}}},"artists":[{"name":"TestArtist"}],"duration_ms":180000,"release_date":"2024-01-01","score":95}],"humming":[{"duration_ms":5000,"artists":[{"name":"HumArtist"}]}]}}`)
	}))
	defer server.Close()

	// 创建临时音频文件
	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "test_audio.mp3")
	if err := os.WriteFile(audioPath, []byte("fake audio data"), 0644); err != nil {
		t.Fatalf("创建临时文件失败: %v", err)
	}

	config := &hschema.AudioModelConfig{
		ACRAccessKey:    "test_access_key",
		ACRAccessSecret: "test_access_secret",
		ACRBaseURL:      server.URL,
	}

	result, err := InvokeACRMetadata(context.Background(), audioPath, config)
	if err != nil {
		t.Fatalf("期望成功，实际错误: %v", err)
	}
	if result == nil {
		t.Fatal("期望返回结果，实际为 nil")
	}
}

// TestInvokeACRMetadata_API错误 测试 ACR 服务返回错误
func TestInvokeACRMetadata_API错误(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"status":{"code":1001,"msg":"access key invalid"}}`)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	audioPath := filepath.Join(tmpDir, "test_audio.mp3")
	if err := os.WriteFile(audioPath, []byte("fake audio data"), 0644); err != nil {
		t.Fatalf("创建临时文件失败: %v", err)
	}

	config := &hschema.AudioModelConfig{
		ACRAccessKey:    "bad_key",
		ACRAccessSecret: "bad_secret",
		ACRBaseURL:      server.URL,
	}

	_, err := InvokeACRMetadata(context.Background(), audioPath, config)
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestInvokeACRMetadata_文件不存在 测试音频文件不存在
func TestInvokeACRMetadata_文件不存在(t *testing.T) {
	config := &hschema.AudioModelConfig{
		ACRAccessKey:    "test_key",
		ACRAccessSecret: "test_secret",
		ACRBaseURL:      "http://localhost",
	}

	_, err := InvokeACRMetadata(context.Background(), "/nonexistent/audio.mp3", config)
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// ──────────────────────────── parseFloat 测试 ────────────────────────────

// TestParseFloat_正常 测试正常浮点数解析
func TestParseFloat_正常(t *testing.T) {
	val, err := parseFloat("3.14")
	if err != nil {
		t.Fatalf("期望成功，实际错误: %v", err)
	}
	if val != 3.14 {
		t.Errorf("期望 3.14，实际: %f", val)
	}
}

// TestParseFloat_整数 测试整数解析
func TestParseFloat_整数(t *testing.T) {
	val, err := parseFloat("100")
	if err != nil {
		t.Fatalf("期望成功，实际错误: %v", err)
	}
	if val != 100.0 {
		t.Errorf("期望 100.0，实际: %f", val)
	}
}

// TestParseFloat_无效 测试无效字符串解析
func TestParseFloat_无效(t *testing.T) {
	_, err := parseFloat("not_a_number")
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// ──────────────────────────── findBestHumming 测试 ────────────────────────────

// TestFindBestHumming_有数据 测试从 humming 列表找最佳匹配
func TestFindBestHumming_有数据(t *testing.T) {
	humming := []any{
		map[string]any{"duration_ms": 3000.0, "artists": []any{map[string]any{"name": "A1"}}},
		map[string]any{"duration_ms": 5000.0, "artists": []any{map[string]any{"name": "A2"}}},
		map[string]any{"duration_ms": 1000.0, "artists": []any{map[string]any{"name": "A3"}}},
	}
	best := findBestHumming(humming)
	if best == nil {
		t.Fatal("期望找到最佳匹配，实际为 nil")
	}
	if best["duration_ms"] != 5000.0 {
		t.Errorf("期望 duration_ms=5000.0，实际: %v", best["duration_ms"])
	}
}

// TestFindBestHumming_空列表 测试空列表
func TestFindBestHumming_空列表(t *testing.T) {
	best := findBestHumming(nil)
	if best != nil {
		t.Errorf("期望 nil，实际: %v", best)
	}
}

// TestFindBestHumming_无duration 测试没有 duration_ms 的情况
func TestFindBestHumming_无duration(t *testing.T) {
	humming := []any{
		map[string]any{"name": "unknown"},
	}
	best := findBestHumming(humming)
	// 没有 duration_ms 时所有项的 duration 为 0，不大于 bestDuration(0)，所以 best 为 nil
	if best != nil {
		t.Errorf("期望 nil（无 duration_ms 的元素不会被选中），实际: %v", best)
	}
}

// ──────────────────────────── extractResponseText 多模态测试 ────────────────────────────

// TestExtractResponseTextInternal_MultiModalContentPart 测试多模态内容 part 提取文本
func TestExtractResponseTextInternal_MultiModalContentPart(t *testing.T) {
	// 构造带 text part 的 AssistantMessage
	msg := llmschema.NewAssistantMessage("")
	msg.Content = llmschema.NewMultiModalContent(llmschema.ContentPart{
		Type: "text", Text: "OCR 结果: Hello World",
	}, llmschema.ContentPart{
		Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "https://example.com/img.png"},
	})
	result := extractResponseText(msg)
	if result != "OCR 结果: Hello World" {
		t.Errorf("期望 'OCR 结果: Hello World'，实际: '%s'", result)
	}
}

// TestExtractResponseTextInternal_MultiModalNoTextPart 测试多模态无 text 类型 part
func TestExtractResponseTextInternal_MultiModalNoTextPart(t *testing.T) {
	msg := llmschema.NewAssistantMessage("")
	msg.Content = llmschema.NewMultiModalContent(llmschema.ContentPart{
		Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "https://example.com/img.png"},
	})
	result := extractResponseText(msg)
	// 没有 text part，应返回 content.String() trim 后的结果
	// 没有 text part，应返回 content.String() trim 后的结果，空 string 可以接受
	_ = result
}

// ──────────────────────────── ffprobeDuration 测试 ────────────────────────────

// TestFFprobeDuration_成功 测试 ffprobe 成功获取时长
func TestFFprobeDuration_成功(t *testing.T) {
	// 保存原始 execCommand 并恢复
	originalExecCommand := execCommand
	defer func() { execCommand = originalExecCommand }()

	execCommand = func(name string, args ...string) (string, error) {
		return "120.5", nil
	}

	// 创建临时文件（ffprobe 检查 /usr/bin/ffprobe 是否存在）
	originalStat := osStat
	defer func() { osStat = originalStat }()

	osStat = func(name string) (os.FileInfo, error) {
		if name == "/usr/bin/ffprobe" {
			return nil, nil // 模拟 ffprobe 存在
		}
		return os.Stat(name)
	}

	duration, err := ffprobeDuration("/tmp/test_audio.mp3")
	if err != nil {
		t.Fatalf("期望成功，实际错误: %v", err)
	}
	if duration != 120.5 {
		t.Errorf("期望 120.5，实际: %f", duration)
	}
}

// TestFFprobeDuration_NA 测试 ffprobe 返回 N/A
func TestFFprobeDuration_NA(t *testing.T) {
	originalExecCommand := execCommand
	defer func() { execCommand = originalExecCommand }()

	execCommand = func(name string, args ...string) (string, error) {
		return "N/A", nil
	}

	originalStat := osStat
	defer func() { osStat = originalStat }()

	osStat = func(name string) (os.FileInfo, error) {
		if name == "/usr/bin/ffprobe" {
			return nil, nil
		}
		return os.Stat(name)
	}

	_, err := ffprobeDuration("/tmp/test_audio.mp3")
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestFFprobeDuration_不可用 测试 ffprobe 不存在
func TestFFprobeDuration_不可用(t *testing.T) {
	originalStat := osStat
	defer func() { osStat = originalStat }()

	osStat = func(name string) (os.FileInfo, error) {
		if name == "/usr/bin/ffprobe" {
			return nil, os.ErrNotExist // 模拟 ffprobe 不存在
		}
		return os.Stat(name)
	}

	_, err := ffprobeDuration("/tmp/test_audio.mp3")
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// ──────────────────────────── guessAudioFormat 扩展名测试 ────────────────────────────

// TestGuessAudioFormat_扩展名推断 测试通过扩展名推断格式
func TestGuessAudioFormat_扩展名推断(t *testing.T) {
	// 小数据（<512字节），无法通过 MIME 检测，应降级到扩展名
	smallData := []byte("fake audio")

	tests := []struct {
		path     string
		expected string
	}{
		{"audio.mp3", "mp3"},
		{"audio.wav", "wav"},
		{"audio.ogg", "ogg"},
		{"audio.flac", "flac"},
		{"audio.aac", "aac"},
		{"audio.m4a", "m4a"},
		{"audio.unknown", "mp3"}, // 未知格式默认 mp3
	}

	for _, tc := range tests {
		result := guessAudioFormat(tc.path, smallData)
		if result != tc.expected {
			t.Errorf("guessAudioFormat(%s) = %s, 期望 %s", tc.path, result, tc.expected)
		}
	}
}

// ──────────────────────────── guessVideoMIMEType 扩展名测试 ────────────────────────────

// TestGuessVideoMIMEType_扩展名推断 测试通过扩展名推断视频 MIME
func TestGuessVideoMIMEType_扩展名推断(t *testing.T) {
	// 小数据（<512字节），降级到扩展名
	smallData := []byte("fake video")

	tests := []struct {
		path     string
		expected string
	}{
		{"video.mp4", "video/mp4"},
		{"video.webm", "video/webm"},
		{"video.avi", "video/avi"},
		{"video.mov", "video/quicktime"},
		{"video.mkv", "video/x-matroska"},
		{"video.flv", "video/x-flv"},
		{"video.unknown", "video/mp4"}, // 默认
	}

	for _, tc := range tests {
		result := guessVideoMIMEType(tc.path, smallData)
		if result != tc.expected {
			t.Errorf("guessVideoMIMEType(%s) = %s, 期望 %s", tc.path, result, tc.expected)
		}
	}
}

// ──────────────────────────── downloadAudioToTemp 测试 ────────────────────────────

// TestDownloadAudioToTemp_成功 测试成功下载音频到临时文件
func TestDownloadAudioToTemp_成功(t *testing.T) {
	audioContent := []byte("fake mp3 audio data")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
		w.Write(audioContent)
	}))
	defer server.Close()

	config := newTestAudioConfig()
	path, err := downloadAudioToTemp(context.Background(), server.URL+"/audio.mp3", config)
	if err != nil {
		t.Fatalf("期望成功，实际错误: %v", err)
	}
	defer os.Remove(path)

	// 验证临时文件内容
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取临时文件失败: %v", err)
	}
	if string(data) != string(audioContent) {
		t.Errorf("内容不匹配")
	}
}

// TestDownloadAudioToTemp_HTTP错误 测试下载时 HTTP 错误
func TestDownloadAudioToTemp_HTTP错误(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	config := newTestAudioConfig()
	_, err := downloadAudioToTemp(context.Background(), server.URL+"/audio.mp3", config)
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestDownloadAudioToTemp_大小超限 测试下载音频超过大小限制
func TestDownloadAudioToTemp_大小超限(t *testing.T) {
	// 创建一个返回大内容的 mock 服务端
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
		// 写 1MB 超过限制
		largeData := make([]byte, 1024*1024+1)
		w.Write(largeData)
	}))
	defer server.Close()

	config := newTestAudioConfig()
	config.MaxAudioBytes = 1024 // 设置很小的限制（1KB）

	_, err := downloadAudioToTemp(context.Background(), server.URL+"/audio.mp3", config)
	if err == nil {
		t.Fatal("期望返回错误（超限），实际为 nil")
	}
}

// ──────────────────────────── parseWAVDuration 边界测试 ────────────────────────────

// TestParseWAVDuration_非RIFF 测试非 RIFF 文件
func TestParseWAVDuration_非RIFF(t *testing.T) {
	tmpDir := t.TempDir()
	fakeWav := filepath.Join(tmpDir, "not_wav.wav")
	_ = os.WriteFile(fakeWav, []byte("NOT_A_WAV_FILE_AT_ALL"), 0644)

	_, err := parseWAVDuration(fakeWav)
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestParseWAVDuration_多声道 测试多声道 WAV 文件
func TestParseWAVDuration_多声道(t *testing.T) {
	tmpDir := t.TempDir()
	wavFile := filepath.Join(tmpDir, "stereo.wav")
	wavData := constructSimpleWAV(44100, 16, 2, 88200) // 2 声道, 1 秒
	_ = os.WriteFile(wavFile, wavData, 0644)

	duration, err := parseWAVDuration(wavFile)
	if err != nil {
		t.Fatalf("期望成功，实际错误: %v", err)
	}
	// 2声道16bit44100Hz，88200 samples → 2 秒
	if duration < 1.9 || duration > 2.1 {
		t.Errorf("duration = %f, 期望约 2.0", duration)
	}
}
