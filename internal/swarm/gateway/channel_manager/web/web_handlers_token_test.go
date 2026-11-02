package web

import (
	"context"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestHandleModelsList_Token字段回填 验证两个 token 字段从 YAML 回填到扁平结构
func TestHandleModelsList_Token字段回填(t *testing.T) {
	writeTokenFixture(t)

	handler := handleModelsList()
	resp, err := handler(context.Background(), nil, "")
	if err != nil {
		t.Fatalf("handler 报错: %v", err)
	}
	models, _ := resp["models"].([]map[string]any)
	if len(models) != 1 {
		t.Fatalf("应返回 1 条，实际 %d", len(models))
	}
	if models[0]["max_input_tokens"] != 32000 {
		t.Errorf("max_input_tokens 应为 32000，实际 %v", models[0]["max_input_tokens"])
	}
	// fixture 未配置 max_output_tokens → 不回填，前端显示为空
	if _, exists := models[0]["max_output_tokens"]; exists {
		t.Errorf("未配置的 max_output_tokens 不应出现在结果中，实际 %v", models[0]["max_output_tokens"])
	}
}
