package openai

import (
	"testing"
)

// ──────────────────────────── SanitizeHeaders 测试 ────────────────────────────

func TestSanitizeHeaders_NilInput(t *testing.T) {
	// nil 输入应返回 nil
	result := SanitizeHeaders(nil)
	if result != nil {
		t.Errorf("SanitizeHeaders(nil) = %v, want nil", result)
	}
}

func TestSanitizeHeaders_EmptyInput(t *testing.T) {
	// 空字典应返回 nil
	result := SanitizeHeaders(map[string]any{})
	if result != nil {
		t.Errorf("SanitizeHeaders(empty) = %v, want nil", result)
	}
}

func TestSanitizeHeaders_ProtectedHeaders(t *testing.T) {
	// 受保护头部应被过滤
	input := map[string]any{
		"Authorization": "Bearer token",
		"Content-Type":  "application/json",
		"Host":          "api.openai.com",
		"X-Custom":      "value",
	}
	result := SanitizeHeaders(input)
	if _, ok := result["Authorization"]; ok {
		t.Error("Authorization 不应出现在结果中")
	}
	if _, ok := result["Content-Type"]; ok {
		t.Error("Content-Type 不应出现在结果中")
	}
	if _, ok := result["Host"]; ok {
		t.Error("Host 不应出现在结果中")
	}
	if result["X-Custom"] != "value" {
		t.Errorf("X-Custom = %q, want %q", result["X-Custom"], "value")
	}
}

func TestSanitizeHeaders_NormalHeaders(t *testing.T) {
	// 正常头部应被保留，值转为字符串
	input := map[string]any{
		"X-Request-ID": "abc-123",
		"X-Retry-Count": 3,
	}
	result := SanitizeHeaders(input)
	if len(result) != 2 {
		t.Errorf("len(result) = %d, want 2", len(result))
	}
	if result["X-Request-ID"] != "abc-123" {
		t.Errorf("X-Request-ID = %q, want %q", result["X-Request-ID"], "abc-123")
	}
	if result["X-Retry-Count"] != "3" {
		t.Errorf("X-Retry-Count = %q, want %q", result["X-Retry-Count"], "3")
	}
}

func TestSanitizeHeaders_EmptyValue(t *testing.T) {
	// 空值头部应被过滤
	input := map[string]any{
		"X-Empty": "",
		"X-Valid": "ok",
	}
	result := SanitizeHeaders(input)
	if _, ok := result["X-Empty"]; ok {
		t.Error("空值头部应被过滤")
	}
	if result["X-Valid"] != "ok" {
		t.Errorf("X-Valid = %q, want %q", result["X-Valid"], "ok")
	}
}

func TestSanitizeHeaders_AllFiltered(t *testing.T) {
	// 所有头部都被过滤时应返回 nil
	input := map[string]any{
		"Authorization": "Bearer token",
		"Content-Type":  "application/json",
	}
	result := SanitizeHeaders(input)
	if result != nil {
		t.Errorf("SanitizeHeaders(all protected) = %v, want nil", result)
	}
}

// ──────────────────────────── MergeHeaders 测试 ────────────────────────────

func TestMergeHeaders_BaseOnly(t *testing.T) {
	// 只有 base headers
	base := map[string]string{
		"X-Base": "base-value",
	}
	result := MergeHeaders(base, nil)
	if result["X-Base"] != "base-value" {
		t.Errorf("X-Base = %q, want %q", result["X-Base"], "base-value")
	}
}

func TestMergeHeaders_RequestOverride(t *testing.T) {
	// 请求级覆盖配置级
	base := map[string]string{
		"X-Header": "base-value",
	}
	request := map[string]string{
		"X-Header": "request-value",
	}
	result := MergeHeaders(base, request)
	if result["X-Header"] != "request-value" {
		t.Errorf("X-Header = %q, want %q", result["X-Header"], "request-value")
	}
}

func TestMergeHeaders_CaseInsensitive(t *testing.T) {
	// 大小写不敏感合并，保留首次出现的 key 大小写
	base := map[string]string{
		"X-Custom-Header": "base",
	}
	request := map[string]string{
		"x-custom-header": "request",
	}
	result := MergeHeaders(base, request)
	// 保留 base 的 key 大小写，但值被 request 覆盖
	if result["X-Custom-Header"] != "request" {
		t.Errorf("X-Custom-Header = %q, want %q", result["X-Custom-Header"], "request")
	}
	// 不应有小写版本的 key
	if _, ok := result["x-custom-header"]; ok {
		t.Error("不应存在小写版本的 key")
	}
}

func TestMergeHeaders_BothEmpty(t *testing.T) {
	// 两者均为空
	result := MergeHeaders(nil, nil)
	if len(result) != 0 {
		t.Errorf("MergeHeaders(nil, nil) = %v, want empty", result)
	}
}

func TestMergeHeaders_NewKeyFromRequest(t *testing.T) {
	// 请求级新增 key
	base := map[string]string{
		"X-Base": "base",
	}
	request := map[string]string{
		"X-New": "new-value",
	}
	result := MergeHeaders(base, request)
	if result["X-Base"] != "base" {
		t.Errorf("X-Base = %q, want %q", result["X-Base"], "base")
	}
	if result["X-New"] != "new-value" {
		t.Errorf("X-New = %q, want %q", result["X-New"], "new-value")
	}
}

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
