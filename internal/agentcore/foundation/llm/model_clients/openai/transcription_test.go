package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 辅助函数 ────────────────────────────

// createTempAudioFile 创建测试用的临时音频文件
func createTempAudioFile(t *testing.T, content string) string {
	t.Helper()
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test_audio.mp3")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("创建临时音频文件失败: %v", err)
	}
	return tmpFile
}

// ──────────────────────────── TranscribeAudio 测试 ────────────────────────────

// TestOpenAIModelClient_TranscribeAudio_成功 测试音频转写成功场景
func TestOpenAIModelClient_TranscribeAudio_成功(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求方法
		if r.Method != http.MethodPost {
			t.Errorf("请求方法 = %s, 期望 POST", r.Method)
		}
		// 验证请求路径
		if r.URL.Path != "/audio/transcriptions" {
			t.Errorf("请求路径 = %s, 期望 /audio/transcriptions", r.URL.Path)
		}
		// 验证 Authorization
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("Authorization = %q, 期望 %q", auth, "Bearer test-key")
		}
		// 验证 Content-Type 是 multipart/form-data
		contentType := r.Header.Get("Content-Type")
		if !strings.HasPrefix(contentType, "multipart/form-data") {
			t.Errorf("Content-Type = %q, 期望 multipart/form-data", contentType)
		}
		// 解析 multipart form 验证字段
		reader, err := r.MultipartReader()
		if err != nil {
			t.Fatalf("解析 multipart 失败: %v", err)
		}
		hasModel := false
		modelValue := ""
		hasFile := false
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("读取 multipart part 失败: %v", err)
			}
			if part.FormName() == "model" {
				value, _ := io.ReadAll(part)
				modelValue = string(value)
				hasModel = true
			}
			if part.FormName() == "file" {
				hasFile = true
			}
		}
		if !hasModel {
			t.Error("multipart 请求缺少 model 字段")
		}
		// ModelConfig.ModelName="gpt-4" → 不传模型参数时降级为配置值
		if modelValue != "gpt-4" {
			t.Errorf("model = %q, 期望降级到配置值 %q", modelValue, "gpt-4")
		}
		if !hasFile {
			t.Error("multipart 请求缺少 file 字段")
		}

		// 返回成功响应
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "hello world"})
	}))
	defer server.Close()

	client := newTestClientWithServer(server, nil)
	audioPath := createTempAudioFile(t, "fake audio content")

	resp, err := client.TranscribeAudio(context.Background(), audioPath)
	if err != nil {
		t.Fatalf("TranscribeAudio 返回错误: %v", err)
	}
	if resp.Text != "hello world" {
		t.Errorf("Text = %q, 期望 %q", resp.Text, "hello world")
	}
}

// TestOpenAIModelClient_TranscribeAudio_指定模型 测试指定模型名称
func TestOpenAIModelClient_TranscribeAudio_指定模型(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reader, err := r.MultipartReader()
		if err != nil {
			t.Fatalf("解析 multipart 失败: %v", err)
		}
		modelValue := ""
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if part.FormName() == "model" {
				value, _ := io.ReadAll(part)
				modelValue = string(value)
			}
		}
		if modelValue != "custom-model" {
			t.Errorf("model = %q, 期望 %q", modelValue, "custom-model")
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "custom result"})
	}))
	defer server.Close()

	client := newTestClientWithServer(server, nil)
	audioPath := createTempAudioFile(t, "fake audio")

	resp, err := client.TranscribeAudio(context.Background(), audioPath,
		llmschema.WithTranscriptionModel("custom-model"),
	)
	if err != nil {
		t.Fatalf("TranscribeAudio 返回错误: %v", err)
	}
	if resp.Text != "custom result" {
		t.Errorf("Text = %q, 期望 %q", resp.Text, "custom result")
	}
}

// TestOpenAIModelClient_TranscribeAudio_指定语言 测试指定语言参数
func TestOpenAIModelClient_TranscribeAudio_指定语言(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reader, err := r.MultipartReader()
		if err != nil {
			t.Fatalf("解析 multipart 失败: %v", err)
		}
		hasLanguage := false
		languageValue := ""
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if part.FormName() == "language" {
				value, _ := io.ReadAll(part)
				languageValue = string(value)
				hasLanguage = true
			}
		}
		if !hasLanguage {
			t.Error("multipart 请求缺少 language 字段")
		}
		if languageValue != "zh" {
			t.Errorf("language = %q, 期望 %q", languageValue, "zh")
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "中文内容"})
	}))
	defer server.Close()

	client := newTestClientWithServer(server, nil)
	audioPath := createTempAudioFile(t, "fake audio")

	resp, err := client.TranscribeAudio(context.Background(), audioPath,
		llmschema.WithTranscriptionLanguage("zh"),
	)
	if err != nil {
		t.Fatalf("TranscribeAudio 返回错误: %v", err)
	}
	if resp.Text != "中文内容" {
		t.Errorf("Text = %q, 期望 %q", resp.Text, "中文内容")
	}
}

// TestOpenAIModelClient_TranscribeAudio_API错误 测试 API 返回 HTTP 错误
func TestOpenAIModelClient_TranscribeAudio_API错误(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":{"message":"Incorrect API key","type":"invalid_request_error","code":"invalid_api_key"}}`)
	}))
	defer server.Close()

	client := newTestClientWithServer(server, nil)
	audioPath := createTempAudioFile(t, "fake audio")

	result, err := client.TranscribeAudio(context.Background(), audioPath)
	if err == nil {
		t.Error("TranscribeAudio HTTP 401 应返回错误")
	}
	if result != nil {
		t.Error("TranscribeAudio HTTP 401 结果应为 nil")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误应为 BaseError 类型, got %T", err)
	}
	if !strings.Contains(baseErr.Error(), "401") {
		t.Errorf("错误消息应包含 401, got %q", baseErr.Error())
	}
}

// TestOpenAIModelClient_TranscribeAudio_空响应 测试转写返回空文本
func TestOpenAIModelClient_TranscribeAudio_空响应(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"text": ""})
	}))
	defer server.Close()

	client := newTestClientWithServer(server, nil)
	audioPath := createTempAudioFile(t, "fake audio")

	result, err := client.TranscribeAudio(context.Background(), audioPath)
	if err == nil {
		t.Error("空文本应返回错误")
	}
	if result != nil {
		t.Error("空文本结果应为 nil")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误应为 BaseError 类型, got %T", err)
	}
	if !strings.Contains(baseErr.Error(), "空内容") {
		t.Errorf("错误消息应包含 '空内容', got %q", baseErr.Error())
	}
}

// TestOpenAIModelClient_TranscribeAudio_文件不存在 测试音频文件不存在
func TestOpenAIModelClient_TranscribeAudio_文件不存在(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 不应到达此 handler
		t.Error("不应发送请求（文件不存在）")
	}))
	defer server.Close()

	client := newTestClientWithServer(server, nil)

	result, err := client.TranscribeAudio(context.Background(), "/nonexistent/audio.mp3")
	if err == nil {
		t.Error("文件不存在时应返回错误")
	}
	if result != nil {
		t.Error("文件不存在时结果应为 nil")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误应为 BaseError 类型, got %T", err)
	}
	if !strings.Contains(baseErr.Error(), "打开音频文件失败") {
		t.Errorf("错误消息应包含 '打开音频文件失败', got %q", baseErr.Error())
	}
}

// TestOpenAIModelClient_TranscribeAudio_无效JSON 测试响应不是有效 JSON
func TestOpenAIModelClient_TranscribeAudio_无效JSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "this is not json")
	}))
	defer server.Close()

	client := newTestClientWithServer(server, nil)
	audioPath := createTempAudioFile(t, "fake audio")

	result, err := client.TranscribeAudio(context.Background(), audioPath)
	if err == nil {
		t.Error("无效 JSON 应返回错误")
	}
	if result != nil {
		t.Error("无效 JSON 结果应为 nil")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误应为 BaseError 类型, got %T", err)
	}
	if !strings.Contains(baseErr.Error(), "解析失败") {
		t.Errorf("错误消息应包含 '解析失败', got %q", baseErr.Error())
	}
}

// TestOpenAIModelClient_TranscribeAudio_HTTP500 测试服务器返回 500 错误
func TestOpenAIModelClient_TranscribeAudio_HTTP500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "internal server error")
	}))
	defer server.Close()

	client := newTestClientWithServer(server, nil)
	audioPath := createTempAudioFile(t, "fake audio")

	result, err := client.TranscribeAudio(context.Background(), audioPath)
	if err == nil {
		t.Error("HTTP 500 应返回错误")
	}
	if result != nil {
		t.Error("HTTP 500 结果应为 nil")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误应为 BaseError 类型, got %T", err)
	}
	if !strings.Contains(baseErr.Error(), "500") {
		t.Errorf("错误消息应包含 500, got %q", baseErr.Error())
	}
}

// TestOpenAIModelClient_TranscribeAudio_配置降级模型 测试模型名称降级逻辑
func TestOpenAIModelClient_TranscribeAudio_配置降级模型(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reader, err := r.MultipartReader()
		if err != nil {
			t.Fatalf("解析 multipart 失败: %v", err)
		}
		modelValue := ""
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if part.FormName() == "model" {
				value, _ := io.ReadAll(part)
				modelValue = string(value)
			}
		}
		// 客户端配置了 ModelConfig.ModelName = "gpt-4o-mini"，不传模型参数时应降级到配置值
		if modelValue != "gpt-4o-mini" {
			t.Errorf("model = %q, 期望降级到配置值 %q", modelValue, "gpt-4o-mini")
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "降级模型结果"})
	}))
	defer server.Close()

	// 创建使用 ModelConfig 的客户端（ModelName 非空）
	client, err := NewOpenAIModelClient(
		llmschema.NewModelRequestConfig(llmschema.WithModelName("gpt-4o-mini")),
		newTestClientConfig("OpenAI", "test-key", server.URL),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	audioPath := createTempAudioFile(t, "fake audio")
	// 不传 WithTranscriptionModel → 应降级到 ModelConfig.ModelName
	resp, err := client.TranscribeAudio(context.Background(), audioPath)
	if err != nil {
		t.Fatalf("TranscribeAudio 返回错误: %v", err)
	}
	if resp.Text != "降级模型结果" {
		t.Errorf("Text = %q, 期望 %q", resp.Text, "降级模型结果")
	}
}

// TestOpenAIModelClient_TranscribeAudio_默认whisper1 测试无配置模型名时默认使用 whisper-1
func TestOpenAIModelClient_TranscribeAudio_默认whisper1(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reader, err := r.MultipartReader()
		if err != nil {
			t.Fatalf("解析 multipart 失败: %v", err)
		}
		modelValue := ""
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if part.FormName() == "model" {
				value, _ := io.ReadAll(part)
				modelValue = string(value)
			}
		}
		if modelValue != "whisper-1" {
			t.Errorf("model = %q, 期望默认值 %q", modelValue, "whisper-1")
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "whisper 结果"})
	}))
	defer server.Close()

	// 创建 ModelConfig.ModelName 为空的客户端 → 应降级到 whisper-1
	client, err := NewOpenAIModelClient(
		llmschema.NewModelRequestConfig(), // 默认 ModelName 为空
		newTestClientConfig("OpenAI", "test-key", server.URL),
	)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	audioPath := createTempAudioFile(t, "fake audio")
	resp, err := client.TranscribeAudio(context.Background(), audioPath)
	if err != nil {
		t.Fatalf("TranscribeAudio 返回错误: %v", err)
	}
	if resp.Text != "whisper 结果" {
		t.Errorf("Text = %q, 期望 %q", resp.Text, "whisper 结果")
	}
}
