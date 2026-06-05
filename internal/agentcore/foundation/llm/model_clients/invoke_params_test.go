package model_clients

import (
	"testing"
)

// TestNewInvokeParams_Defaults 测试 InvokeParams 默认值。
func TestNewInvokeParams_Defaults(t *testing.T) {
	p := NewInvokeParams()
	if p.Temperature != nil {
		t.Error("默认 Temperature 应为 nil")
	}
	if p.TopP != nil {
		t.Error("默认 TopP 应为 nil")
	}
	if p.Model != "" {
		t.Error("默认 Model 应为空")
	}
	if p.MaxTokens != nil {
		t.Error("默认 MaxTokens 应为 nil")
	}
	if p.Stop != nil {
		t.Error("默认 Stop 应为 nil")
	}
	if p.OutputParser != nil {
		t.Error("默认 OutputParser 应为 nil")
	}
	if p.Timeout != nil {
		t.Error("默认 Timeout 应为 nil")
	}
	if len(p.Tools) != 0 {
		t.Error("默认 Tools 应为空")
	}
}

// TestNewInvokeParams_WithOpts 测试 InvokeParams 组合 Options。
func TestNewInvokeParams_WithOpts(t *testing.T) {
	temp := 0.7
	topP := 0.9
	maxTokens := 1024
	stop := "[END]"
	timeout := 30.0

	p := NewInvokeParams(
		WithInvokeTemperature(temp),
		WithInvokeTopP(topP),
		WithInvokeModel("gpt-4"),
		WithInvokeMaxTokens(maxTokens),
		WithInvokeStop(stop),
		WithInvokeTimeout(timeout),
	)

	if p.Temperature == nil || *p.Temperature != temp {
		t.Errorf("Temperature = %v, 期望 %v", p.Temperature, temp)
	}
	if p.TopP == nil || *p.TopP != topP {
		t.Errorf("TopP = %v, 期望 %v", p.TopP, topP)
	}
	if p.Model != "gpt-4" {
		t.Errorf("Model = %q, 期望 %q", p.Model, "gpt-4")
	}
	if p.MaxTokens == nil || *p.MaxTokens != maxTokens {
		t.Errorf("MaxTokens = %v, 期望 %v", p.MaxTokens, maxTokens)
	}
	if p.Stop == nil || *p.Stop != stop {
		t.Errorf("Stop = %v, 期望 %v", p.Stop, stop)
	}
	if p.Timeout == nil || *p.Timeout != timeout {
		t.Errorf("Timeout = %v, 期望 %v", p.Timeout, timeout)
	}
}

// TestNewStreamParams_Defaults 测试 StreamParams 默认值。
func TestNewStreamParams_Defaults(t *testing.T) {
	p := NewStreamParams()
	if p.Temperature != nil {
		t.Error("默认 Temperature 应为 nil")
	}
	if p.Model != "" {
		t.Error("默认 Model 应为空")
	}
}

// TestNewGenerateImageParams_Defaults 测试 GenerateImageParams 默认值。
func TestNewGenerateImageParams_Defaults(t *testing.T) {
	p := NewGenerateImageParams()
	if p.Size != "1664*928" {
		t.Errorf("Size = %q, 期望 %q", p.Size, "1664*928")
	}
	if p.N != 1 {
		t.Errorf("N = %d, 期望 1", p.N)
	}
	if !p.PromptExtend {
		t.Error("PromptExtend 默认应为 true")
	}
	if p.Watermark {
		t.Error("Watermark 默认应为 false")
	}
}

// TestNewGenerateImageParams_WithOpts 测试 GenerateImageParams Options。
func TestNewGenerateImageParams_WithOpts(t *testing.T) {
	p := NewGenerateImageParams(
		WithImageModel("dall-e-3"),
		WithImageSize("1024*1024"),
		WithImageN(2),
		WithImageSeed(42),
	)
	if p.Model != "dall-e-3" {
		t.Errorf("Model = %q, 期望 %q", p.Model, "dall-e-3")
	}
	if p.Size != "1024*1024" {
		t.Errorf("Size = %q, 期望 %q", p.Size, "1024*1024")
	}
	if p.N != 2 {
		t.Errorf("N = %d, 期望 2", p.N)
	}
	if p.Seed != 42 {
		t.Errorf("Seed = %d, 期望 42", p.Seed)
	}
}

// TestNewGenerateSpeechParams_Defaults 测试 GenerateSpeechParams 默认值。
func TestNewGenerateSpeechParams_Defaults(t *testing.T) {
	p := NewGenerateSpeechParams()
	if p.Voice != "Cherry" {
		t.Errorf("Voice = %q, 期望 %q", p.Voice, "Cherry")
	}
	if p.LanguageType != "Auto" {
		t.Errorf("LanguageType = %q, 期望 %q", p.LanguageType, "Auto")
	}
}

// TestNewGenerateSpeechParams_WithOpts 测试 GenerateSpeechParams Options。
func TestNewGenerateSpeechParams_WithOpts(t *testing.T) {
	p := NewGenerateSpeechParams(
		WithSpeechModel("tts-1"),
		WithSpeechVoice("alloy"),
	)
	if p.Model != "tts-1" {
		t.Errorf("Model = %q, 期望 %q", p.Model, "tts-1")
	}
	if p.Voice != "alloy" {
		t.Errorf("Voice = %q, 期望 %q", p.Voice, "alloy")
	}
}

// TestNewGenerateVideoParams_Defaults 测试 GenerateVideoParams 默认值。
func TestNewGenerateVideoParams_Defaults(t *testing.T) {
	p := NewGenerateVideoParams()
	if p.Duration != 5 {
		t.Errorf("Duration = %d, 期望 5", p.Duration)
	}
	if !p.PromptExtend {
		t.Error("PromptExtend 默认应为 true")
	}
}

// TestNewGenerateVideoParams_WithOpts 测试 GenerateVideoParams Options。
func TestNewGenerateVideoParams_WithOpts(t *testing.T) {
	seed := 123
	p := NewGenerateVideoParams(
		WithVideoModel("video-gen"),
		WithVideoDuration(10),
		WithVideoSeed(seed),
		WithVideoImgURL("https://example.com/img.png"),
	)
	if p.Model != "video-gen" {
		t.Errorf("Model = %q, 期望 %q", p.Model, "video-gen")
	}
	if p.Duration != 10 {
		t.Errorf("Duration = %d, 期望 10", p.Duration)
	}
	if p.Seed == nil || *p.Seed != 123 {
		t.Errorf("Seed = %v, 期望 123", p.Seed)
	}
	if p.ImgURL != "https://example.com/img.png" {
		t.Errorf("ImgURL = %q, 期望 %q", p.ImgURL, "https://example.com/img.png")
	}
}
