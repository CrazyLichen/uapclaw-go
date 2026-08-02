package multimodal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	modelclients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── AudioTranscriptionTool 测试 ────────────────────────────

func TestNewAudioTranscriptionTool_Invoke成功(t *testing.T) {
	mockClient := &mockAudioClient{
		transcriptionText: "hello world transcription",
	}
	config := newTestAudioConfig()

	transcriptionTool := NewAudioTranscriptionTool(mockClient, config, "cn", "test-agent")

	// 使用本地文件
	tmpDir := t.TempDir()
	audioFile := filepath.Join(tmpDir, "test.mp3")
	_ = os.WriteFile(audioFile, []byte("fake audio data"), 0644)

	result, err := transcriptionTool.Invoke(context.Background(), map[string]any{
		"audio_path_or_url": audioFile,
	})
	if err != nil {
		t.Fatalf("AudioTranscriptionTool Invoke 返回错误: %v", err)
	}
	if result["text"] != "hello world transcription" {
		t.Errorf("text = %v, 期望 'hello world transcription'", result["text"])
	}
	if result["model"] != "whisper-1" {
		t.Errorf("model = %v, 期望 'whisper-1'", result["model"])
	}
}

func TestNewAudioTranscriptionTool_Invoke失败(t *testing.T) {
	mockClient := &mockAudioClient{
		transcriptionErr: exception.NewBaseError(exception.StatusModelCallFailed, exception.WithMsg("transcription error")),
	}
	config := newTestAudioConfig()

	transcriptionTool := NewAudioTranscriptionTool(mockClient, config, "cn", "test-agent")

	_, err := transcriptionTool.Invoke(context.Background(), map[string]any{
		"audio_path_or_url": "https://example.com/audio.mp3",
	})
	if err == nil {
		t.Error("TranscriptionTool 失败场景应返回错误")
	}
}

func TestNewAudioTranscriptionTool_Sandbox路径(t *testing.T) {
	mockClient := &mockAudioClient{
		transcriptionText: "should not reach",
	}
	config := newTestAudioConfig()

	transcriptionTool := NewAudioTranscriptionTool(mockClient, config, "cn", "test-agent")

	_, err := transcriptionTool.Invoke(context.Background(), map[string]any{
		"audio_path_or_url": "/home/user/sandbox/audio.mp3",
	})
	if err == nil {
		t.Error("sandbox 路径应返回错误")
	}
}

func TestNewAudioTranscriptionTool_配置无效(t *testing.T) {
	mockClient := &mockAudioClient{transcriptionText: "test"}
	// nil config
	transcriptionTool := NewAudioTranscriptionTool(mockClient, nil, "cn", "test-agent")

	_, err := transcriptionTool.Invoke(context.Background(), map[string]any{
		"audio_path_or_url": "/path/audio.mp3",
	})
	if err == nil {
		t.Error("nil config 应返回错误")
	}
}

// ──────────────────────────── AudioQATool 测试 ────────────────────────────

func TestNewAudioQATool_Invoke成功(t *testing.T) {
	mockClient := &mockAudioClient{
		invokeResponses: []mockVisionResponse{{text: "The audio says hello"}},
	}
	config := newTestAudioConfig()

	qaTool := NewAudioQATool(mockClient, config, "cn", "test-agent")

	tmpDir := t.TempDir()
	audioFile := filepath.Join(tmpDir, "test.mp3")
	_ = os.WriteFile(audioFile, []byte("fake audio"), 0644)

	result, err := qaTool.Invoke(context.Background(), map[string]any{
		"audio_path_or_url": audioFile,
		"question":          "What does the audio say?",
	})
	if err != nil {
		t.Fatalf("AudioQATool Invoke 返回错误: %v", err)
	}
	if result["answer"] != "The audio says hello" {
		t.Errorf("answer = %v, 期望 'The audio says hello'", result["answer"])
	}
	if result["model"] != "gpt-4o-audio-preview" {
		t.Errorf("model = %v, 期望 'gpt-4o-audio-preview'", result["model"])
	}
	// duration_seconds 应存在
	if result["duration_seconds"] == nil {
		t.Error("duration_seconds 不应为 nil")
	}
}

func TestNewAudioQATool_Invoke失败(t *testing.T) {
	mockClient := &mockAudioClient{
		invokeResponses: []mockVisionResponse{
			{err: exception.NewBaseError(exception.StatusModelCallFailed, exception.WithMsg("model error"))},
		},
	}
	config := newTestAudioConfig()

	qaTool := NewAudioQATool(mockClient, config, "cn", "test-agent")

	_, err := qaTool.Invoke(context.Background(), map[string]any{
		"audio_path_or_url": "https://example.com/audio.mp3",
		"question":          "What?",
	})
	if err == nil {
		t.Error("AudioQATool 失败场景应返回错误")
	}
}

func TestNewAudioQATool_配置无效(t *testing.T) {
	mockClient := &mockAudioClient{invokeResponses: []mockVisionResponse{{text: "ok"}}}
	// 空 base_url
	config := &hschema.AudioModelConfig{APIKey: "test"}

	qaTool := NewAudioQATool(mockClient, config, "cn", "test-agent")

	_, err := qaTool.Invoke(context.Background(), map[string]any{
		"audio_path_or_url": "/path/audio.mp3",
		"question":          "What?",
	})
	if err == nil {
		t.Error("空 base_url 应返回错误")
	}
}

// ──────────────────────────── AudioMetadataTool 测试 ────────────────────────────

func TestNewAudioMetadataTool_时长和ACR未配置(t *testing.T) {
	mockClient := &mockAudioClient{}
	config := newTestAudioConfig()
	// 不设置 ACR 凭证
	config.ACRAccessKey = ""
	config.ACRAccessSecret = ""

	metadataTool := NewAudioMetadataTool(mockClient, config, "cn", "test-agent")

	tmpDir := t.TempDir()
	audioFile := filepath.Join(tmpDir, "test.wav")
	_ = os.WriteFile(audioFile, constructSimpleWAV(44100, 16, 1, 44100), 0644)

	result, err := metadataTool.Invoke(context.Background(), map[string]any{
		"audio_path_or_url": audioFile,
	})
	if err != nil {
		t.Fatalf("AudioMetadataTool 返回错误: %v", err)
	}
	if result["identified"] != false {
		t.Errorf("identified = %v, 期望 false (无 ACR)", result["identified"])
	}
	if result["note"] == nil {
		t.Error("note 不应为 nil (应有未配置提示)")
	}
	note, ok := result["note"].(string)
	if !ok || !strings.Contains(note, "ACR credentials") {
		t.Errorf("note = %v, 期望包含 'ACR credentials'", result["note"])
	}
}

func TestNewAudioMetadataTool_时长超过15秒(t *testing.T) {
	mockClient := &mockAudioClient{}
	config := newTestAudioConfig()
	config.ACRAccessKey = "test-key"
	config.ACRAccessSecret = "test-secret"

	metadataTool := NewAudioMetadataTool(mockClient, config, "cn", "test-agent")

	tmpDir := t.TempDir()
	// 创建 20 秒 WAV 文件 (44100 * 20 = 882000 samples)
	audioFile := filepath.Join(tmpDir, "long.wav")
	_ = os.WriteFile(audioFile, constructSimpleWAV(44100, 16, 1, 882000), 0644)

	result, err := metadataTool.Invoke(context.Background(), map[string]any{
		"audio_path_or_url": audioFile,
	})
	if err != nil {
		t.Fatalf("AudioMetadataTool 返回错误: %v", err)
	}
	if result["identified"] != false {
		t.Errorf("identified = %v, 期望 false (时长>15s)", result["identified"])
	}
	note, ok := result["note"].(string)
	if !ok || !strings.Contains(note, "15 seconds") {
		t.Errorf("note = %v, 期望包含 '15 seconds'", result["note"])
	}
}

// ──────────────────────────── CreateAudioTools 测试 ────────────────────────────

func TestCreateAudioTools(t *testing.T) {
	mockClient := &mockAudioClient{transcriptionText: "test"}
	config := newTestAudioConfig()

	toolList := CreateAudioTools(mockClient, config, "cn", "test-agent")
	if len(toolList) != 3 {
		t.Fatalf("CreateAudioTools 应返回 3 个工具, got %d", len(toolList))
	}

	for i, tl := range toolList {
		card := tl.Card()
		if card == nil {
			t.Errorf("tool[%d] Card 不应为 nil", i)
		}
	}
}

// ──────────────────────────── 工具接口合规性测试 ────────────────────────────

func TestAudioTranscriptionTool_ImplementsToolInterface(t *testing.T) {
	mockClient := &mockAudioClient{transcriptionText: "ok"}
	config := newTestAudioConfig()
	audioTool := NewAudioTranscriptionTool(mockClient, config, "cn", "test-agent")
	var _ tool.Tool = audioTool
}

func TestAudioQATool_ImplementsToolInterface(t *testing.T) {
	mockClient := &mockAudioClient{invokeResponses: []mockVisionResponse{{text: "ok"}}}
	config := newTestAudioConfig()
	audioTool := NewAudioQATool(mockClient, config, "cn", "test-agent")
	var _ tool.Tool = audioTool
}

func TestAudioMetadataTool_ImplementsToolInterface(t *testing.T) {
	mockClient := &mockAudioClient{}
	config := newTestAudioConfig()
	audioTool := NewAudioMetadataTool(mockClient, config, "cn", "test-agent")
	var _ tool.Tool = audioTool
}

func TestMockAudioClient_ImplementsBaseModelClient(t *testing.T) {
	var _ modelclients.BaseModelClient = (*mockAudioClient)(nil)
}

// ──────────────────────────── roundDuration 测试 ────────────────────────────

func TestRoundDuration(t *testing.T) {
	if roundDuration(1.234) != 1.23 {
		t.Errorf("roundDuration(1.234) = %.2f, 期望 1.23", roundDuration(1.234))
	}
	if roundDuration(1.235) != 1.24 {
		t.Errorf("roundDuration(1.235) = %.2f, 期望 1.24", roundDuration(1.235))
	}
	if roundDuration(0) != 0 {
		t.Errorf("roundDuration(0) = %.2f, 期望 0", roundDuration(0))
	}
}

// ──────────────────────────── audioQASystemPrompt 测试 ────────────────────────────

func TestAudioQASystemPrompt(t *testing.T) {
	if audioQASystemPrompt == "" {
		t.Error("audioQASystemPrompt 不应为空")
	}
	if !strings.Contains(audioQASystemPrompt, "audio") {
		t.Error("audioQASystemPrompt 应包含 'audio'")
	}
}

// ──────────────────────────── 常量测试 ────────────────────────────

func TestDefaultUserAgent(t *testing.T) {
	if defaultUserAgent == "" {
		t.Error("defaultUserAgent 不应为空")
	}
}
