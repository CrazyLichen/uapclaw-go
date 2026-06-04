package schema

import (
	"encoding/json"
	"testing"
)

// TestNewUsageMetadata 验证所有字段默认值为零值。
func TestNewUsageMetadata(t *testing.T) {
	um := NewUsageMetadata()
	if um.Code != 0 {
		t.Errorf("Code = %d, want 0", um.Code)
	}
	if um.ErrMsg != "" {
		t.Errorf("ErrMsg = %q, want empty", um.ErrMsg)
	}
	if um.Prompt != "" {
		t.Errorf("Prompt = %q, want empty", um.Prompt)
	}
	if um.TaskID != "" {
		t.Errorf("TaskID = %q, want empty", um.TaskID)
	}
	if um.ModelName != "" {
		t.Errorf("ModelName = %q, want empty", um.ModelName)
	}
	if um.TotalLatency != 0 {
		t.Errorf("TotalLatency = %f, want 0", um.TotalLatency)
	}
	if um.FirstTokenTime != "" {
		t.Errorf("FirstTokenTime = %q, want empty", um.FirstTokenTime)
	}
	if um.RequestStartTime != "" {
		t.Errorf("RequestStartTime = %q, want empty", um.RequestStartTime)
	}
	if um.InputTokens != 0 {
		t.Errorf("InputTokens = %d, want 0", um.InputTokens)
	}
	if um.OutputTokens != 0 {
		t.Errorf("OutputTokens = %d, want 0", um.OutputTokens)
	}
	if um.TotalTokens != 0 {
		t.Errorf("TotalTokens = %d, want 0", um.TotalTokens)
	}
	if um.CacheTokens != 0 {
		t.Errorf("CacheTokens = %d, want 0", um.CacheTokens)
	}
	if um.InputCost != 0 {
		t.Errorf("InputCost = %f, want 0", um.InputCost)
	}
	if um.OutputCost != 0 {
		t.Errorf("OutputCost = %f, want 0", um.OutputCost)
	}
	if um.TotalCost != 0 {
		t.Errorf("TotalCost = %f, want 0", um.TotalCost)
	}
}

// TestUsageMetadata_NonZeroValues 验证非零值赋值正确。
func TestUsageMetadata_NonZeroValues(t *testing.T) {
	um := &UsageMetadata{
		Code:             0,
		ErrMsg:           "timeout",
		ModelName:        "gpt-4",
		InputTokens:      100,
		OutputTokens:     50,
		TotalTokens:      150,
		TotalLatency:     1.23,
		InputCost:        0.01,
		OutputCost:       0.02,
		TotalCost:        0.03,
		FirstTokenTime:   "2025-01-01T00:00:00Z",
		RequestStartTime: "2025-01-01T00:00:00Z",
	}

	if um.ModelName != "gpt-4" {
		t.Errorf("ModelName = %q, want %q", um.ModelName, "gpt-4")
	}
	if um.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", um.InputTokens)
	}
	if um.TotalCost != 0.03 {
		t.Errorf("TotalCost = %f, want 0.03", um.TotalCost)
	}
}

// TestUsageMetadata_JSONRoundTrip 验证 JSON 序列化/反序列化一致性。
func TestUsageMetadata_JSONRoundTrip(t *testing.T) {
	original := &UsageMetadata{
		Code:         0,
		ErrMsg:       "",
		ModelName:    "deepseek-v3",
		InputTokens:  200,
		OutputTokens: 100,
		TotalTokens:  300,
		TotalLatency: 2.5,
		InputCost:    0.05,
		OutputCost:   0.1,
		TotalCost:    0.15,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored UsageMetadata
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if restored.ModelName != original.ModelName {
		t.Errorf("ModelName: got %q, want %q", restored.ModelName, original.ModelName)
	}
	if restored.InputTokens != original.InputTokens {
		t.Errorf("InputTokens: got %d, want %d", restored.InputTokens, original.InputTokens)
	}
	if restored.OutputTokens != original.OutputTokens {
		t.Errorf("OutputTokens: got %d, want %d", restored.OutputTokens, original.OutputTokens)
	}
	if restored.TotalTokens != original.TotalTokens {
		t.Errorf("TotalTokens: got %d, want %d", restored.TotalTokens, original.TotalTokens)
	}
	if restored.TotalLatency != original.TotalLatency {
		t.Errorf("TotalLatency: got %f, want %f", restored.TotalLatency, original.TotalLatency)
	}
	if restored.TotalCost != original.TotalCost {
		t.Errorf("TotalCost: got %f, want %f", restored.TotalCost, original.TotalCost)
	}
}

// TestUsageMetadata_EmptyJSON 验证空 JSON 反序列化后所有字段为零值。
func TestUsageMetadata_EmptyJSON(t *testing.T) {
	jsonStr := `{}`
	var um UsageMetadata
	if err := json.Unmarshal([]byte(jsonStr), &um); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if um.Code != 0 || um.InputTokens != 0 || um.TotalCost != 0 {
		t.Error("空 JSON 反序列化后字段应为零值")
	}
}
