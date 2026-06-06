package dashscope

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── DashScopeResponse 解析测试 ────────────────────────────

func TestDashScopeResponse_Unmarshal_Success(t *testing.T) {
	// 成功响应解析
	data := `{
		"status_code": 200,
		"request_id": "test-req-123",
		"code": "",
		"message": "",
		"output": {"choices": [{"finish_reason": "stop", "message": {"role": "assistant", "content": [{"text": "hello"}]}}]},
		"usage": {}
	}`

	var resp DashScopeResponse
	if err := json.Unmarshal([]byte(data), &resp); err != nil {
		t.Fatalf("解析成功响应失败: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
	if resp.RequestID != "test-req-123" {
		t.Errorf("RequestID = %q, want %q", resp.RequestID, "test-req-123")
	}
	if resp.Code != "" {
		t.Errorf("Code = %q, want empty", resp.Code)
	}
	if len(resp.Output) == 0 {
		t.Error("Output 不应为空")
	}
}

func TestDashScopeResponse_Unmarshal_Error(t *testing.T) {
	// 错误响应解析
	data := `{
		"status_code": 400,
		"request_id": "test-req-456",
		"code": "InvalidParameter",
		"message": "model is required"
	}`

	var resp DashScopeResponse
	if err := json.Unmarshal([]byte(data), &resp); err != nil {
		t.Fatalf("解析错误响应失败: %v", err)
	}
	if resp.StatusCode != 400 {
		t.Errorf("StatusCode = %d, want 400", resp.StatusCode)
	}
	if resp.Code != "InvalidParameter" {
		t.Errorf("Code = %q, want %q", resp.Code, "InvalidParameter")
	}
	if resp.Message != "model is required" {
		t.Errorf("Message = %q, want %q", resp.Message, "model is required")
	}
}

// ──────────────────────────── MultiModalOutput 解析测试 ────────────────────────────

func TestMultiModalOutput_Unmarshal_Image(t *testing.T) {
	// 图片生成输出解析
	data := `{
		"choices": [
			{
				"finish_reason": "stop",
				"message": {
					"role": "assistant",
					"content": [
						{"image": "https://example.com/image1.png"},
						{"image": "https://example.com/image2.png"}
					]
				}
			}
		]
	}`

	var output MultiModalOutput
	if err := json.Unmarshal([]byte(data), &output); err != nil {
		t.Fatalf("解析图片输出失败: %v", err)
	}
	if len(output.Choices) != 1 {
		t.Fatalf("Choices 长度 = %d, want 1", len(output.Choices))
	}
	if len(output.Choices[0].Message.Content) != 2 {
		t.Errorf("Content 长度 = %d, want 2", len(output.Choices[0].Message.Content))
	}
	if output.Choices[0].Message.Content[0].Image != "https://example.com/image1.png" {
		t.Errorf("Content[0].Image = %q, want image URL", output.Choices[0].Message.Content[0].Image)
	}
}

func TestMultiModalOutput_Unmarshal_Text(t *testing.T) {
	// 文本输出解析
	data := `{
		"choices": [
			{
				"finish_reason": "stop",
				"message": {
					"role": "assistant",
					"content": [{"text": "这是回复文本"}]
				}
			}
		]
	}`

	var output MultiModalOutput
	if err := json.Unmarshal([]byte(data), &output); err != nil {
		t.Fatalf("解析文本输出失败: %v", err)
	}
	if output.Choices[0].Message.Content[0].Text != "这是回复文本" {
		t.Errorf("Content[0].Text = %q, want %q", output.Choices[0].Message.Content[0].Text, "这是回复文本")
	}
}

// ──────────────────────────── VideoOutput 解析测试 ────────────────────────────

func TestVideoOutput_Unmarshal(t *testing.T) {
	// 视频输出解析
	data := `{"video_url": "https://example.com/video.mp4"}`

	var output VideoOutput
	if err := json.Unmarshal([]byte(data), &output); err != nil {
		t.Fatalf("解析视频输出失败: %v", err)
	}
	if output.VideoURL != "https://example.com/video.mp4" {
		t.Errorf("VideoURL = %q, want video URL", output.VideoURL)
	}
}

// ──────────────────────────── AudioOutput 解析测试 ────────────────────────────

func TestAudioOutput_Unmarshal_URL(t *testing.T) {
	// 音频输出 - URL 模式
	data := `{"url": "https://example.com/audio.wav", "data": ""}`

	var output AudioOutput
	if err := json.Unmarshal([]byte(data), &output); err != nil {
		t.Fatalf("解析音频输出失败: %v", err)
	}
	if output.URL != "https://example.com/audio.wav" {
		t.Errorf("URL = %q, want audio URL", output.URL)
	}
}

func TestAudioOutput_Unmarshal_Data(t *testing.T) {
	// 音频输出 - Base64 数据模式
	data := `{"url": "", "data": "dGVzdCBhdWRpbyBkYXRh"}`

	var output AudioOutput
	if err := json.Unmarshal([]byte(data), &output); err != nil {
		t.Fatalf("解析音频数据输出失败: %v", err)
	}
	if output.Data != "dGVzdCBhdWRpbyBkYXRh" {
		t.Errorf("Data = %q, want Base64 data", output.Data)
	}
}

// ──────────────────────────── VideoUsage 解析测试 ────────────────────────────

func TestVideoUsage_Unmarshal(t *testing.T) {
	// 视频用量信息解析
	data := `{"duration": 5, "size": "1280*720"}`

	var usage VideoUsage
	if err := json.Unmarshal([]byte(data), &usage); err != nil {
		t.Fatalf("解析视频用量信息失败: %v", err)
	}
	if usage.Size != "1280*720" {
		t.Errorf("Size = %q, want %q", usage.Size, "1280*720")
	}
}

// ──────────────────────────── 常量测试 ────────────────────────────

func TestDashScopeVoices_NotEmpty(t *testing.T) {
	// 语音列表不为空
	if len(DashScopeVoices) == 0 {
		t.Error("DashScopeVoices 不应为空")
	}
	// 包含默认语音
	found := false
	for _, v := range DashScopeVoices {
		if v == "Cherry" {
			found = true
			break
		}
	}
	if !found {
		t.Error("DashScopeVoices 应包含默认语音 'Cherry'")
	}
}

func TestDashScopeLanguageTypes_NotEmpty(t *testing.T) {
	// 语言类型列表不为空
	if len(DashScopeLanguageTypes) == 0 {
		t.Error("DashScopeLanguageTypes 不应为空")
	}
	// 包含默认语言
	found := false
	for _, l := range DashScopeLanguageTypes {
		if l == "Chinese" {
			found = true
			break
		}
	}
	if !found {
		t.Error("DashScopeLanguageTypes 应包含 'Chinese'")
	}
}
