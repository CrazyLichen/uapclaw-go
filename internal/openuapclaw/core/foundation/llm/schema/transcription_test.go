package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

// ────────────────────── TranscriptionResponse 测试 ──────────────────────

// TestTranscriptionResponse_JSON序列化 验证 TranscriptionResponse 序列化。
func TestTranscriptionResponse_JSON序列化(t *testing.T) {
	resp := &TranscriptionResponse{Text: "hello world"}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if !strings.Contains(string(data), "hello world") {
		t.Errorf("序列化结果应包含 text 内容, got %s", data)
	}
}

// TestTranscriptionResponse_JSON反序列化 验证 TranscriptionResponse 反序列化。
func TestTranscriptionResponse_JSON反序列化(t *testing.T) {
	jsonStr := `{"text":"transcribed text"}`
	var resp TranscriptionResponse
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if resp.Text != "transcribed text" {
		t.Errorf("Text = %q, want %q", resp.Text, "transcribed text")
	}
}

// ────────────────────── TranscriptionParams 测试 ──────────────────────

// TestNewTranscriptionParams 验证 TranscriptionParams 构造。
func TestNewTranscriptionParams(t *testing.T) {
	params := NewTranscriptionParams(
		WithTranscriptionModel("whisper-1"),
		WithTranscriptionLanguage("zh"),
		WithTranscriptionTimeout(30.0),
	)
	if params.Model != "whisper-1" {
		t.Errorf("Model = %q, want %q", params.Model, "whisper-1")
	}
	if params.Language != "zh" {
		t.Errorf("Language = %q, want %q", params.Language, "zh")
	}
	if params.Timeout != 30.0 {
		t.Errorf("Timeout = %v, want %v", params.Timeout, 30.0)
	}
}

// TestNewTranscriptionParams_默认值 验证无选项时为零值。
func TestNewTranscriptionParams_默认值(t *testing.T) {
	params := NewTranscriptionParams()
	if params.Model != "" {
		t.Errorf("默认 Model 应为空, got %q", params.Model)
	}
	if params.Language != "" {
		t.Errorf("默认 Language 应为空, got %q", params.Language)
	}
	if params.Timeout != 0 {
		t.Errorf("默认 Timeout 应为 0, got %v", params.Timeout)
	}
	if params.Extra != nil {
		t.Errorf("默认 Extra 应为 nil, got %v", params.Extra)
	}
}

// TestWithTranscriptionExtra 验证 Extra 参数设置。
func TestWithTranscriptionExtra(t *testing.T) {
	extra := map[string]any{"key": "value"}
	params := NewTranscriptionParams(WithTranscriptionExtra(extra))
	if params.Extra == nil {
		t.Fatal("Extra 应不为 nil")
	}
	if params.Extra["key"] != "value" {
		t.Errorf("Extra[key] = %v, want %q", params.Extra["key"], "value")
	}
}
