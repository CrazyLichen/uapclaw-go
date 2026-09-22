package reme

import (
	"math"
	"testing"
)

// TestParseJSONExperienceResponse_JSONBlockArray 测试从 ```json 代码块中解析数组
func TestParseJSONExperienceResponse_JSONBlockArray(t *testing.T) {
	input := "some text```json\n[{\"experience\": \"exp1\", \"when_to_use\": \"trigger1\"}]\n```more text"
	result := ParseJSONExperienceResponse(input)
	if len(result) != 1 {
		t.Fatalf("期望 1 条经验，得到 %d", len(result))
	}
	if result[0]["experience"] != "exp1" {
		t.Errorf("期望 experience=exp1，得到 %v", result[0]["experience"])
	}
}

// TestParseJSONExperienceResponse_JSONBlockObject 测试从 ```json 代码块中解析单个对象
func TestParseJSONExperienceResponse_JSONBlockObject(t *testing.T) {
	input := "```json\n{\"experience\": \"exp2\", \"condition\": \"cond2\"}\n```"
	result := ParseJSONExperienceResponse(input)
	if len(result) != 1 {
		t.Fatalf("期望 1 条经验，得到 %d", len(result))
	}
	if result[0]["experience"] != "exp2" {
		t.Errorf("期望 experience=exp2，得到 %v", result[0]["experience"])
	}
}

// TestParseJSONExperienceResponse_PlainJSON 测试直接解析纯 JSON
func TestParseJSONExperienceResponse_PlainJSON(t *testing.T) {
	input := `[{"experience": "exp3", "when_to_use": "trigger3"}]`
	result := ParseJSONExperienceResponse(input)
	if len(result) != 1 {
		t.Fatalf("期望 1 条经验，得到 %d", len(result))
	}
	if result[0]["experience"] != "exp3" {
		t.Errorf("期望 experience=exp3，得到 %v", result[0]["experience"])
	}
}

// TestParseJSONExperienceResponse_InvalidJSON 测试畸形 JSON 返回空
func TestParseJSONExperienceResponse_InvalidJSON(t *testing.T) {
	input := "this is not json at all"
	result := ParseJSONExperienceResponse(input)
	if len(result) != 0 {
		t.Fatalf("期望 0 条经验，得到 %d", len(result))
	}
}

// TestParseJSONExperienceResponse_NoJSONBlock 测试无 JSON 代码块时尝试直接解析
func TestParseJSONExperienceResponse_NoJSONBlock(t *testing.T) {
	input := `{"experience": "exp4", "when_to_use": "trigger4"}`
	result := ParseJSONExperienceResponse(input)
	if len(result) != 1 {
		t.Fatalf("期望 1 条经验，得到 %d", len(result))
	}
	if result[0]["experience"] != "exp4" {
		t.Errorf("期望 experience=exp4，得到 %v", result[0]["experience"])
	}
}

// TestCalculateCosineSimilarity_相同向量 测试相同向量返回 1.0
func TestCalculateCosineSimilarity_相同向量(t *testing.T) {
	vec := []float64{1.0, 2.0, 3.0}
	sim := CalculateCosineSimilarity(vec, vec)
	if math.Abs(sim-1.0) > 1e-9 {
		t.Errorf("期望 1.0，得到 %f", sim)
	}
}

// TestCalculateCosineSimilarity_正交向量 测试正交向量返回 0.0
func TestCalculateCosineSimilarity_正交向量(t *testing.T) {
	a := []float64{1.0, 0.0}
	b := []float64{0.0, 1.0}
	sim := CalculateCosineSimilarity(a, b)
	if math.Abs(sim) > 1e-9 {
		t.Errorf("期望 0.0，得到 %f", sim)
	}
}

// TestCalculateCosineSimilarity_零向量 测试零向量返回 0.0
func TestCalculateCosineSimilarity_零向量(t *testing.T) {
	a := []float64{0.0, 0.0, 0.0}
	b := []float64{1.0, 2.0, 3.0}
	sim := CalculateCosineSimilarity(a, b)
	if sim != 0.0 {
		t.Errorf("期望 0.0，得到 %f", sim)
	}
}

// TestCalculateCosineSimilarity_一般情况 测试一般情况的余弦相似度
func TestCalculateCosineSimilarity_一般情况(t *testing.T) {
	a := []float64{1.0, 2.0, 3.0}
	b := []float64{4.0, 5.0, 6.0}
	// dot=32, normA=sqrt(14), normB=sqrt(77), sim=32/sqrt(14*77)=32/sqrt(1078)
	expected := 32.0 / math.Sqrt(14.0*77.0)
	sim := CalculateCosineSimilarity(a, b)
	if math.Abs(sim-expected) > 1e-9 {
		t.Errorf("期望 %f，得到 %f", expected, sim)
	}
}

// TestIsValidExperience 测试 isValidExperience 各种边界
func TestIsValidExperience(t *testing.T) {
	tests := []struct {
		name    string
		data    map[string]any
		isValid bool
	}{
		{
			name:    "experience + when_to_use",
			data:    map[string]any{"experience": "exp", "when_to_use": "trigger"},
			isValid: true,
		},
		{
			name:    "experience + condition",
			data:    map[string]any{"experience": "exp", "condition": "cond"},
			isValid: true,
		},
		{
			name:    "只有 experience",
			data:    map[string]any{"experience": "exp"},
			isValid: false,
		},
		{
			name:    "只有 when_to_use",
			data:    map[string]any{"when_to_use": "trigger"},
			isValid: false,
		},
		{
			name:    "空 map",
			data:    map[string]any{},
			isValid: false,
		},
		{
			name:    "experience + when_to_use + condition",
			data:    map[string]any{"experience": "exp", "when_to_use": "t", "condition": "c"},
			isValid: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidExperience(tt.data)
			if got != tt.isValid {
				t.Errorf("isValidExperience(%v) = %v，期望 %v", tt.data, got, tt.isValid)
			}
		})
	}
}
