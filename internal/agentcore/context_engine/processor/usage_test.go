package processor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestExtractUsageMetadata_nil消息 验证 nil 消息返回 nil
func TestExtractUsageMetadata_nil消息(t *testing.T) {
	result := ExtractUsageMetadata(nil)
	if result != nil {
		t.Error("nil 消息应返回 nil")
	}
}

// TestExtractUsageMetadata_无UsageMetadata 验证无 UsageMetadata 返回 nil
func TestExtractUsageMetadata_无UsageMetadata(t *testing.T) {
	msg := llm_schema.NewAssistantMessage("hello")
	result := ExtractUsageMetadata(msg)
	if result != nil {
		t.Error("无 UsageMetadata 应返回 nil")
	}
}

// TestExtractUsageMetadata_正常提取 验证正常提取用量
func TestExtractUsageMetadata_正常提取(t *testing.T) {
	msg := llm_schema.NewAssistantMessage("hello",
		llm_schema.WithAssistantUsageMetadata(&llm_schema.UsageMetadata{
			InputTokens:  100,
			OutputTokens: 50,
			TotalTokens:  150,
			CacheTokens:  30,
			InputCost:    0.01,
			OutputCost:   0.005,
			TotalCost:    0.015,
			ModelName:    "qwen-max",
		}),
	)
	result := ExtractUsageMetadata(msg)
	if result == nil {
		t.Fatal("提取结果不应为 nil")
	}
	if result["calls"] != 1 {
		t.Errorf("calls = %v, want 1", result["calls"])
	}
	if result["input_tokens"] != 100 {
		t.Errorf("input_tokens = %v, want 100", result["input_tokens"])
	}
	if result["output_tokens"] != 50 {
		t.Errorf("output_tokens = %v, want 50", result["output_tokens"])
	}
	if result["total_tokens"] != 150 {
		t.Errorf("total_tokens = %v, want 150", result["total_tokens"])
	}
	if result["cache_tokens"] != 30 {
		t.Errorf("cache_tokens = %v, want 30", result["cache_tokens"])
	}
	if result["model_name"] != "qwen-max" {
		t.Errorf("model_name = %v, want qwen-max", result["model_name"])
	}
	details, ok := result["details"].([]map[string]any)
	if !ok || len(details) != 1 {
		t.Fatalf("details 应为长度1的切片，实际 %v", result["details"])
	}
	if details[0]["model_name"] != "qwen-max" {
		t.Errorf("details[0].model_name = %v, want qwen-max", details[0]["model_name"])
	}
}

// TestMergeCompressionUsage_左nil 验证左参数为 nil
func TestMergeCompressionUsage_左nil(t *testing.T) {
	right := map[string]any{"calls": 1, "total_tokens": 100}
	result := MergeCompressionUsage(nil, right)
	if result["calls"] != 1 {
		t.Errorf("calls = %v, want 1", result["calls"])
	}
}

// TestMergeCompressionUsage_右nil 验证右参数为 nil
func TestMergeCompressionUsage_右nil(t *testing.T) {
	left := map[string]any{"calls": 2, "total_tokens": 200}
	result := MergeCompressionUsage(left, nil)
	if result["calls"] != 2 {
		t.Errorf("calls = %v, want 2", result["calls"])
	}
}

// TestMergeCompressionUsage_双方nil 验证双方为 nil
func TestMergeCompressionUsage_双方nil(t *testing.T) {
	result := MergeCompressionUsage(nil, nil)
	if result != nil {
		t.Error("双方 nil 应返回 nil")
	}
}

// TestMergeCompressionUsage_累加合并 验证累加合并逻辑
func TestMergeCompressionUsage_累加合并(t *testing.T) {
	left := map[string]any{
		"calls":         1,
		"input_tokens":  100,
		"output_tokens": 50,
		"total_tokens":  150,
		"cache_tokens":  30,
		"input_cost":    0.01,
		"output_cost":   0.005,
		"total_cost":    0.015,
		"model_name":    "qwen-max",
		"details":       []map[string]any{{"total_tokens": 150}},
	}
	right := map[string]any{
		"calls":         1,
		"input_tokens":  200,
		"output_tokens": 80,
		"total_tokens":  280,
		"cache_tokens":  40,
		"input_cost":    0.02,
		"output_cost":   0.008,
		"total_cost":    0.028,
		"model_name":    "qwen-plus",
		"details":       []map[string]any{{"total_tokens": 280}},
	}
	result := MergeCompressionUsage(left, right)
	if result["calls"] != 2 {
		t.Errorf("calls = %v, want 2", result["calls"])
	}
	if result["input_tokens"] != 300 {
		t.Errorf("input_tokens = %v, want 300", result["input_tokens"])
	}
	if result["total_tokens"] != 430 {
		t.Errorf("total_tokens = %v, want 430", result["total_tokens"])
	}
	if result["model_name"] != "qwen-max" {
		t.Errorf("model_name = %v, want qwen-max（取 left 非空值）", result["model_name"])
	}
	details, ok := result["details"].([]map[string]any)
	if !ok || len(details) != 2 {
		t.Fatalf("details 应为长度2的切片，实际 %v", result["details"])
	}
}

// TestMergeCompressionUsage_modelName左空取右 验证 model_name 左空时取右
func TestMergeCompressionUsage_modelName左空取右(t *testing.T) {
	left := map[string]any{
		"calls":        1,
		"input_tokens": 100,
		"model_name":   "",
	}
	right := map[string]any{
		"calls":        1,
		"input_tokens": 50,
		"model_name":   "qwen-plus",
	}
	result := MergeCompressionUsage(left, right)
	if result["model_name"] != "qwen-plus" {
		t.Errorf("model_name = %v, want qwen-plus（left 为空时取 right）", result["model_name"])
	}
}

// TestResetCompressionUsage 验证重置用量
func TestResetCompressionUsage(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	msg := llm_schema.NewAssistantMessage("hello",
		llm_schema.WithAssistantUsageMetadata(&llm_schema.UsageMetadata{
			InputTokens: 100,
			TotalTokens: 150,
		}),
	)
	p.RecordCompressionUsage(msg)
	if p.CurrentCompressionUsage() == nil {
		t.Fatal("RecordCompressionUsage 后用量不应为 nil")
	}
	p.ResetCompressionUsage()
	if p.CurrentCompressionUsage() != nil {
		t.Error("ResetCompressionUsage 后用量应为 nil")
	}
}

// TestRecordCompressionUsage_记录并获取 验证记录用量后获取快照
func TestRecordCompressionUsage_记录并获取(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	msg := llm_schema.NewAssistantMessage("hello",
		llm_schema.WithAssistantUsageMetadata(&llm_schema.UsageMetadata{
			InputTokens: 100,
			TotalTokens: 150,
			ModelName:   "qwen-max",
		}),
	)
	p.RecordCompressionUsage(msg)
	usage := p.CurrentCompressionUsage()
	if usage == nil {
		t.Fatal("CurrentCompressionUsage 不应为 nil")
	}
	if usage["input_tokens"] != 100 {
		t.Errorf("input_tokens = %v, want 100", usage["input_tokens"])
	}
	// 验证返回的是副本
	usage["input_tokens"] = 999
	if p.CurrentCompressionUsage()["input_tokens"] == 999 {
		t.Error("CurrentCompressionUsage 应返回副本，不应影响内部状态")
	}
}

// TestRecordCompressionUsage_nil响应 验证 nil 响应不报错
func TestRecordCompressionUsage_nil响应(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	p.RecordCompressionUsage(nil)
	if p.CurrentCompressionUsage() != nil {
		t.Error("nil 响应后用量应为 nil")
	}
}

// TestRecordCompressionUsage_无UsageMetadata 验证无 UsageMetadata 不记录
func TestRecordCompressionUsage_无UsageMetadata(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	msg := llm_schema.NewAssistantMessage("hello")
	p.RecordCompressionUsage(msg)
	if p.CurrentCompressionUsage() != nil {
		t.Error("无 UsageMetadata 时用量应为 nil")
	}
}

// TestCurrentCompressionUsage_nil时返回nil 验证无用量时返回 nil
func TestCurrentCompressionUsage_nil时返回nil(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	if p.CurrentCompressionUsage() != nil {
		t.Error("未记录时用量应为 nil")
	}
}

// TestMergeCompressionUsage_int64类型 验证 int64 类型值也能累加
func TestMergeCompressionUsage_int64类型(t *testing.T) {
	left := map[string]any{
		"calls":        int64(1),
		"input_tokens": int64(100),
	}
	right := map[string]any{
		"calls":        int64(2),
		"input_tokens": int64(50),
	}
	result := MergeCompressionUsage(left, right)
	if result["calls"] != 3 {
		t.Errorf("calls = %v, want 3", result["calls"])
	}
	if result["input_tokens"] != 150 {
		t.Errorf("input_tokens = %v, want 150", result["input_tokens"])
	}
}

// TestMergeCompressionUsage_float64cost 验证 float64 费用累加
func TestMergeCompressionUsage_float64cost(t *testing.T) {
	left := map[string]any{
		"calls":       1,
		"input_cost":  float64(0.01),
		"output_cost": float64(0.005),
		"total_cost":  float64(0.015),
	}
	right := map[string]any{
		"calls":       1,
		"input_cost":  float64(0.02),
		"output_cost": float64(0.008),
		"total_cost":  float64(0.028),
	}
	result := MergeCompressionUsage(left, right)
	if result["input_cost"] != 0.03 {
		t.Errorf("input_cost = %v, want 0.03", result["input_cost"])
	}
}

// TestMergeCompressionUsage_modelName左nil取右 验证 model_name 左 nil 时取右
func TestMergeCompressionUsage_modelName左nil取右(t *testing.T) {
	left := map[string]any{
		"calls":        1,
		"input_tokens": 100,
		"model_name":   nil,
	}
	right := map[string]any{
		"calls":        1,
		"input_tokens": 50,
		"model_name":   "qwen-plus",
	}
	result := MergeCompressionUsage(left, right)
	if result["model_name"] != "qwen-plus" {
		t.Errorf("model_name = %v, want qwen-plus（left 为 nil 时取 right）", result["model_name"])
	}
}

// TestToFloat64 将 any 转为 float64
func TestToFloat64(t *testing.T) {
	t.Run("float64", func(t *testing.T) {
		assert.Equal(t, 3.14, toFloat64(3.14))
	})
	t.Run("int", func(t *testing.T) {
		assert.Equal(t, float64(42), toFloat64(42))
	})
	t.Run("int64", func(t *testing.T) {
		assert.Equal(t, float64(100), toFloat64(int64(100)))
	})
	t.Run("其他类型", func(t *testing.T) {
		assert.Equal(t, float64(0), toFloat64("not a number"))
	})
	t.Run("nil", func(t *testing.T) {
		assert.Equal(t, float64(0), toFloat64(nil))
	})
}

// TestToSliceOfMaps 将 any 转为 []map[string]any
func TestToSliceOfMaps(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		assert.Nil(t, toSliceOfMaps(nil))
	})
	t.Run("正确类型", func(t *testing.T) {
		input := []map[string]any{{"a": 1}, {"b": 2}}
		assert.Equal(t, input, toSliceOfMaps(input))
	})
	t.Run("[]any类型", func(t *testing.T) {
		input := []any{
			map[string]any{"a": 1},
			map[string]any{"b": 2},
		}
		result := toSliceOfMaps(input)
		assert.Len(t, result, 2)
		assert.Equal(t, 1, result[0]["a"])
	})
	t.Run("[]any含非map元素", func(t *testing.T) {
		input := []any{
			map[string]any{"a": 1},
			"not a map",
		}
		result := toSliceOfMaps(input)
		assert.Len(t, result, 1)
	})
	t.Run("其他类型", func(t *testing.T) {
		assert.Nil(t, toSliceOfMaps("not a slice"))
	})
}

// TestToInt 将 any 转为 int
func TestToInt_覆盖(t *testing.T) {
	t.Run("int", func(t *testing.T) {
		assert.Equal(t, 42, toInt(42))
	})
	t.Run("int64", func(t *testing.T) {
		assert.Equal(t, 100, toInt(int64(100)))
	})
	t.Run("float64", func(t *testing.T) {
		assert.Equal(t, 3, toInt(3.7))
	})
	t.Run("其他类型", func(t *testing.T) {
		assert.Equal(t, 0, toInt("not a number"))
	})
}
