package openai

import (
	"testing"
)

// ──────────────────────────── AdjustParamsForOpenAI 测试 ────────────────────────────

func TestAdjustParamsForOpenAI_OpenAIBase(t *testing.T) {
	// openai.com API base：temperature 和 top_p 同时存在时删除 top_p
	params := map[string]any{
		"temperature": 0.7,
		"top_p":       0.9,
	}
	AdjustParamsForOpenAI(params, "https://api.openai.com/v1")
	if _, ok := params["top_p"]; ok {
		t.Error("openai.com 场景下，temperature 存在时 top_p 应被删除")
	}
	if _, ok := params["temperature"]; !ok {
		t.Error("temperature 应保留")
	}
}

func TestAdjustParamsForOpenAI_OpenAIBase_TopPOnly(t *testing.T) {
	// openai.com 场景下，只有 top_p 时保留
	params := map[string]any{
		"top_p": 0.9,
	}
	AdjustParamsForOpenAI(params, "https://api.openai.com/v1")
	if _, ok := params["top_p"]; !ok {
		t.Error("只有 top_p 时应保留 top_p")
	}
}

func TestAdjustParamsForOpenAI_NonOpenAIBase(t *testing.T) {
	// 非 openai.com：不做任何调整
	params := map[string]any{
		"temperature": 0.7,
		"top_p":       0.9,
	}
	AdjustParamsForOpenAI(params, "https://custom-api.example.com/v1")
	if _, ok := params["top_p"]; !ok {
		t.Error("非 openai.com 场景下 top_p 不应被删除")
	}
	if _, ok := params["temperature"]; !ok {
		t.Error("非 openai.com 场景下 temperature 应保留")
	}
}

func TestAdjustParamsForOpenAI_OpenRouterBase(t *testing.T) {
	// openrouter.com 含 "openai.com" 子串不匹配，但 openrouter 不含 openai.com
	params := map[string]any{
		"temperature": 0.7,
		"top_p":       0.9,
	}
	AdjustParamsForOpenAI(params, "https://openrouter.ai/api/v1")
	if _, ok := params["top_p"]; !ok {
		t.Error("openrouter.ai 不含 openai.com，top_p 不应被删除")
	}
}

// ──────────────────────────── HandleExtraBody 测试 ────────────────────────────

func TestHandleExtraBody_ReturnTokenIDs(t *testing.T) {
	// return_token_ids 应被移入 extra_body
	params := map[string]any{
		"model":            "gpt-4",
		"return_token_ids": true,
	}
	HandleExtraBody(params)
	if _, ok := params["return_token_ids"]; ok {
		t.Error("return_token_ids 应从顶级参数中移除")
	}
	extraBody, ok := params["extra_body"].(map[string]any)
	if !ok {
		t.Fatal("extra_body 应为 map[string]any 类型")
	}
	if extraBody["return_token_ids"] != true {
		t.Errorf("extra_body.return_token_ids = %v, want true", extraBody["return_token_ids"])
	}
}

func TestHandleExtraBody_NoReturnTokenIDs(t *testing.T) {
	// 无 return_token_ids 时不做任何操作
	params := map[string]any{
		"model": "gpt-4",
	}
	HandleExtraBody(params)
	if _, ok := params["extra_body"]; ok {
		t.Error("无 return_token_ids 时不应创建 extra_body")
	}
}

func TestHandleExtraBody_ExistingExtraBody(t *testing.T) {
	// 已有 extra_body 时追加 return_token_ids
	params := map[string]any{
		"model":            "gpt-4",
		"return_token_ids": true,
		"extra_body": map[string]any{
			"custom_field": "value",
		},
	}
	HandleExtraBody(params)
	extraBody, ok := params["extra_body"].(map[string]any)
	if !ok {
		t.Fatal("extra_body 应为 map[string]any 类型")
	}
	if extraBody["custom_field"] != "value" {
		t.Errorf("extra_body.custom_field = %v, want value", extraBody["custom_field"])
	}
	if extraBody["return_token_ids"] != true {
		t.Errorf("extra_body.return_token_ids = %v, want true", extraBody["return_token_ids"])
	}
}
