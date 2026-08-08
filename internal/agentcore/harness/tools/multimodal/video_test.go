package multimodal

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── NormalizeVideoURL 测试 ────────────────────────────

func TestNormalizeVideoURL_HTTPURL(t *testing.T) {
	url, err := NormalizeVideoURL("https://example.com/video.mp4")
	if err != nil {
		t.Fatalf("NormalizeVideoURL HTTP URL 返回错误: %v", err)
	}
	if url != "https://example.com/video.mp4" {
		t.Errorf("url = %q, 期望 %q", url, "https://example.com/video.mp4")
	}
}

func TestNormalizeVideoURL_本地文件(t *testing.T) {
	tmpDir := t.TempDir()
	videoFile := filepath.Join(tmpDir, "test.mp4")
	videoData := make([]byte, 1000) // 小文件
	_ = os.WriteFile(videoFile, videoData, 0644)

	url, err := NormalizeVideoURL(videoFile)
	if err != nil {
		t.Fatalf("NormalizeVideoURL 本地文件返回错误: %v", err)
	}
	if !strings.HasPrefix(url, "data:video/mp4;base64,") {
		t.Errorf("url 应以 'data:video/mp4;base64,' 开头, got %q", url[:30])
	}
	// 验证 base64 可解码
	encoded := strings.TrimPrefix(url, "data:video/mp4;base64,")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("base64 解码失败: %v", err)
	}
	if len(decoded) != len(videoData) {
		t.Errorf("解码后大小 = %d, 期望 %d", len(decoded), len(videoData))
	}
}

func TestNormalizeVideoURL_文件不存在(t *testing.T) {
	_, err := NormalizeVideoURL("/nonexistent/video.mp4")
	if err == nil {
		t.Error("文件不存在应返回错误")
	}
}

func TestNormalizeVideoURL_空路径(t *testing.T) {
	_, err := NormalizeVideoURL("")
	if err == nil {
		t.Error("空路径应返回错误")
	}
}

// ──────────────────────────── VideoUnderstandingTool 测试 ────────────────────────────

func TestNewVideoUnderstandingTool_Invoke成功(t *testing.T) {
	mockClient := &mockVisionClient{
		responses: []mockVisionResponse{{text: "The video shows a cat playing"}},
	}
	config := newTestVideoConfig()

	videoTool := NewVideoUnderstandingTool(mockClient, config, "cn", "test-agent")

	result, err := videoTool.Invoke(context.Background(), map[string]any{
		"query":      "What is in this video?",
		"video_path": "https://example.com/video.mp4",
	})
	if err != nil {
		t.Fatalf("VideoUnderstandingTool Invoke 返回错误: %v", err)
	}
	if result["answer"] != "The video shows a cat playing" {
		t.Errorf("answer = %v, 期望 'The video shows a cat playing'", result["answer"])
	}
	if result["query"] != "What is in this video?" {
		t.Errorf("query = %v, 期望 'What is in this video?'", result["query"])
	}
	if result["model"] != "glm-4.6v" {
		t.Errorf("model = %v, 期望 'glm-4.6v'", result["model"])
	}
}

func TestNewVideoUnderstandingTool_Invoke失败(t *testing.T) {
	mockClient := &mockVisionClient{
		responses: []mockVisionResponse{
			{err: exception.NewBaseError(exception.StatusModelCallFailed, exception.WithMsg("model error"))},
		},
	}
	config := newTestVideoConfig()

	videoTool := NewVideoUnderstandingTool(mockClient, config, "cn", "test-agent")

	_, err := videoTool.Invoke(context.Background(), map[string]any{
		"query":      "What?",
		"video_path": "https://example.com/video.mp4",
	})
	if err == nil {
		t.Error("VideoUnderstandingTool 失败场景应返回错误")
	}
}

func TestNewVideoUnderstandingTool_配置无效(t *testing.T) {
	mockClient := &mockVisionClient{responses: []mockVisionResponse{{text: "ok"}}}
	// nil 配置
	videoTool := NewVideoUnderstandingTool(mockClient, nil, "cn", "test-agent")

	_, err := videoTool.Invoke(context.Background(), map[string]any{
		"query":      "What?",
		"video_path": "https://example.com/video.mp4",
	})
	if err == nil {
		t.Error("nil config 应返回错误")
	}
}

func TestNewVideoUnderstandingTool_空Query(t *testing.T) {
	mockClient := &mockVisionClient{responses: []mockVisionResponse{{text: "ok"}}}
	config := newTestVideoConfig()

	videoTool := NewVideoUnderstandingTool(mockClient, config, "cn", "test-agent")

	_, err := videoTool.Invoke(context.Background(), map[string]any{
		"query":      "",
		"video_path": "https://example.com/video.mp4",
	})
	if err == nil {
		t.Error("空 query 应返回错误")
	}
}

func TestNewVideoUnderstandingTool_空VideoPath(t *testing.T) {
	mockClient := &mockVisionClient{responses: []mockVisionResponse{{text: "ok"}}}
	config := newTestVideoConfig()

	videoTool := NewVideoUnderstandingTool(mockClient, config, "cn", "test-agent")

	_, err := videoTool.Invoke(context.Background(), map[string]any{
		"query":      "What?",
		"video_path": "",
	})
	if err == nil {
		t.Error("空 video_path 应返回错误")
	}
}

// ──────────────────────────── 参数裁剪测试 ────────────────────────────

func TestClampInt_默认值(t *testing.T) {
	result := clampInt(0, 128, 8192, 2048)
	if result != 2048 {
		t.Errorf("clampInt(0) = %d, 期望 2048", result)
	}
}

func TestClampInt_超上限(t *testing.T) {
	result := clampInt(10000, 128, 8192, 2048)
	if result != 8192 {
		t.Errorf("clampInt(10000) = %d, 期望 8192", result)
	}
}

func TestClampInt_低于下限(t *testing.T) {
	result := clampInt(50, 128, 8192, 2048)
	if result != 128 {
		t.Errorf("clampInt(50) = %d, 期望 128", result)
	}
}

func TestClampInt_正常值(t *testing.T) {
	result := clampInt(1000, 128, 8192, 2048)
	if result != 1000 {
		t.Errorf("clampInt(1000) = %d, 期望 1000", result)
	}
}

func TestClampFloat_默认值(t *testing.T) {
	result := clampFloat(0, 0.0, 2.0, 0.2)
	if result != 0.2 {
		t.Errorf("clampFloat(0) = %.2f, 期望 0.2", result)
	}
}

func TestClampFloat_超上限(t *testing.T) {
	result := clampFloat(3.0, 0.0, 2.0, 0.2)
	if result != 2.0 {
		t.Errorf("clampFloat(3.0) = %.2f, 期望 2.0", result)
	}
}

func TestClampFloat_低于下限(t *testing.T) {
	result := clampFloat(-0.5, 0.0, 2.0, 0.2)
	if result != 0.0 {
		t.Errorf("clampFloat(-0.5) = %.2f, 期望 0.0", result)
	}
}

func TestClampFloat_正常值(t *testing.T) {
	result := clampFloat(1.5, 0.0, 2.0, 0.2)
	if result != 1.5 {
		t.Errorf("clampFloat(1.5) = %.2f, 期望 1.5", result)
	}
}

// ──────────────────────────── 工具接口合规性测试 ────────────────────────────

func TestVideoUnderstandingTool_ImplementsToolInterface(t *testing.T) {
	mockClient := &mockVisionClient{responses: []mockVisionResponse{{text: "ok"}}}
	config := newTestVideoConfig()
	videoTool := NewVideoUnderstandingTool(mockClient, config, "cn", "test-agent")
	var _ tool.Tool = videoTool
}

// ──────────────────────────── guessVideoMIMEType 测试 ────────────────────────────

func TestGuessVideoMIMEType_MP4(t *testing.T) {
	result := guessVideoMIMEType("test.mp4", nil)
	if result != "video/mp4" {
		t.Errorf("result = %q, 期望 %q", result, "video/mp4")
	}
}

func TestGuessVideoMIMEType_WebM(t *testing.T) {
	result := guessVideoMIMEType("test.webm", nil)
	if result != "video/webm" {
		t.Errorf("result = %q, 期望 %q", result, "video/webm")
	}
}

func TestGuessVideoMIMEType_未知(t *testing.T) {
	result := guessVideoMIMEType("test.xyz", nil)
	if result != "video/mp4" {
		t.Errorf("result = %q, 期望 %q (降级)", result, "video/mp4")
	}
}

// ──────────────────────────── 视频常量测试 ────────────────────────────

func TestVideoConstants(t *testing.T) {
	if defaultVideoMaxTokens != 2048 {
		t.Errorf("defaultVideoMaxTokens = %d, 期望 2048", defaultVideoMaxTokens)
	}
	if defaultVideoTemperature != 0.2 {
		t.Errorf("defaultVideoTemperature = %.2f, 期望 0.2", defaultVideoTemperature)
	}
	if defaultVideoTimeoutSeconds != 120 {
		t.Errorf("defaultVideoTimeoutSeconds = %d, 期望 120", defaultVideoTimeoutSeconds)
	}
	if minVideoMaxTokens != 128 || maxVideoMaxTokens != 8192 {
		t.Errorf("max_tokens 范围应为 [128, 8192]")
	}
	if minVideoTemperature != 0.0 || maxVideoTemperature != 2.0 {
		t.Errorf("temperature 范围应为 [0.0, 2.0]")
	}
	if minVideoTimeout != 10 || maxVideoTimeout != 600 {
		t.Errorf("timeout 范围应为 [10, 600]")
	}
	if defaultVideoModel != "glm-4.6v" {
		t.Errorf("defaultVideoModel = %v, 期望 glm-4.6v", defaultVideoModel)
	}
}

// ──────────────────────────── ThinkingEnabled 测试 ────────────────────────────

func TestNewVideoUnderstandingTool_ThinkingEnabled(t *testing.T) {
	mockClient := &mockVisionClient{
		responses: []mockVisionResponse{{text: "视频内容分析结果"}},
	}
	config := newTestVideoConfig()
	config.ThinkingEnabled = true

	videoTool := NewVideoUnderstandingTool(mockClient, config, "cn", "test-agent")

	result, err := videoTool.Invoke(context.Background(), map[string]any{
		"query":            "视频中发生了什么？",
		"video_path":       "https://example.com/video.mp4",
		"thinking_enabled": true,
	})
	if err != nil {
		t.Fatalf("ThinkingEnabled Invoke 返回错误: %v", err)
	}
	if result["answer"] != "视频内容分析结果" {
		t.Errorf("answer = %v, 期望 '视频内容分析结果'", result["answer"])
	}
}

func TestNewVideoUnderstandingTool_ThinkingEnabledFalse(t *testing.T) {
	mockClient := &mockVisionClient{
		responses: []mockVisionResponse{{text: "结果"}},
	}
	config := newTestVideoConfig()
	// config.ThinkingEnabled 默认 false

	videoTool := NewVideoUnderstandingTool(mockClient, config, "cn", "test-agent")

	result, err := videoTool.Invoke(context.Background(), map[string]any{
		"query":      "视频中有什么？",
		"video_path": "https://example.com/video.mp4",
	})
	if err != nil {
		t.Fatalf("ThinkingEnabled=False Invoke 返回错误: %v", err)
	}
	if result["answer"] != "结果" {
		t.Errorf("answer = %v, 期望 '结果'", result["answer"])
	}
}

func TestNewVideoUnderstandingTool_默认模型GLM4v(t *testing.T) {
	mockClient := &mockVisionClient{
		responses: []mockVisionResponse{{text: "结果"}},
	}
	config := &hschema.VideoModelConfig{
		APIKey: "test-api-key",
		// Model 字段为空，应使用 defaultVideoModel = "glm-4.6v"
	}

	videoTool := NewVideoUnderstandingTool(mockClient, config, "cn", "test-agent")

	result, err := videoTool.Invoke(context.Background(), map[string]any{
		"query":      "视频描述",
		"video_path": "https://example.com/video.mp4",
	})
	if err != nil {
		t.Fatalf("默认模型 Invoke 返回错误: %v", err)
	}
	if result["model"] != "glm-4.6v" {
		t.Errorf("model = %v, 期望 glm-4.6v", result["model"])
	}
}
