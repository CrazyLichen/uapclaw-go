package processcli

import (
	"errors"
	"testing"
)

// TestValidateModelOptions_七类拒绝。
func TestValidateModelOptions_七类拒绝(t *testing.T) {
	// 全空 → 无错无密钥
	if key, err := ValidateModelOptions(&AppArgs{}); err != nil || key != "" {
		t.Errorf("全空 = %q/%v", key, err)
	}
	// 有连接参数但 prompts 空
	if _, err := ValidateModelOptions(&AppArgs{Model: "m", APIBase: "https://x/v1"}); err == nil {
		t.Error("无 prompt 应拒绝连接参数")
	}
	// api-base 无 model
	if _, err := ValidateModelOptions(&AppArgs{Prompt: "x", APIBase: "https://x/v1", APIKeyEnv: "K"}); err == nil {
		t.Error("api-base 无 model 应拒绝")
	}
	// api-base 非法 scheme
	if _, err := ValidateModelOptions(&AppArgs{
		Prompt: "x", Model: "m", APIBase: "ftp://x/v1", APIKeyEnv: "K",
	}); err == nil {
		t.Error("ftp scheme 应拒绝")
	}
	// api-base 含认证信息
	if _, err := ValidateModelOptions(&AppArgs{
		Prompt: "x", Model: "m", APIBase: "https://u:p@x/v1", APIKeyEnv: "K",
	}); err == nil {
		t.Error("含认证信息应拒绝")
	}
	// api-base 有但 api-key-env 缺
	if _, err := ValidateModelOptions(&AppArgs{
		Prompt: "x", Model: "m", APIBase: "https://x/v1",
	}); err == nil {
		t.Error("api-base 无 api-key-env 应拒绝")
	}
	// model-provider 有但 api-base 缺
	if _, err := ValidateModelOptions(&AppArgs{
		Prompt: "x", Model: "m", ModelProvider: "OpenAI",
	}); err == nil {
		t.Error("model-provider 无 api-base 应拒绝")
	}
	// api-key-env 名字非法
	if _, err := ValidateModelOptions(&AppArgs{
		Prompt: "x", Model: "m", APIBase: "https://x/v1", APIKeyEnv: "1bad-name",
	}); err == nil {
		t.Error("非法 env 名应拒绝")
	}
	// api-key-env 环境变量未设
	t.Setenv("UAPCLAW_TEST_MISSING_KEY", "")
	if _, err := ValidateModelOptions(&AppArgs{
		Prompt: "x", Model: "m", APIBase: "https://x/v1", APIKeyEnv: "UAPCLAW_TEST_MISSING_KEY",
	}); err == nil {
		t.Error("空密钥应拒绝")
	}
	// 密钥含换行
	t.Setenv("UAPCLAW_TEST_NL_KEY", "line1\nline2")
	if _, err := ValidateModelOptions(&AppArgs{
		Prompt: "x", Model: "m", APIBase: "https://x/v1", APIKeyEnv: "UAPCLAW_TEST_NL_KEY",
	}); err == nil {
		t.Error("含换行密钥应拒绝")
	}
}

// TestValidateModelOptions_合法返回密钥。
func TestValidateModelOptions_合法返回密钥(t *testing.T) {
	t.Setenv("UAPCLAW_TEST_KEY", " sk-abc ")
	key, err := ValidateModelOptions(&AppArgs{
		Prompt: "x", Model: "m", APIBase: "https://x/v1", APIKeyEnv: "UAPCLAW_TEST_KEY",
	})
	if err != nil {
		t.Fatalf("合法输入报错：%v", err)
	}
	if key != "sk-abc" {
		t.Errorf("key = %q, 期望已 trim 的 sk-abc", key)
	}
}

// TestValidateModelOptions_worker豁免 worker 模式下连接参数也拒绝。
func TestValidateModelOptions_worker拒绝(t *testing.T) {
	if _, err := ValidateModelOptions(&AppArgs{
		Prompt: "x", Model: "m", InteractiveWorker: true,
	}); err == nil {
		t.Error("worker 模式应拒绝连接参数")
	}
}

// TestValidAPIBase 七项检查逐条。
// Python: model_options.py:67-84
func TestValidAPIBase(t *testing.T) {
	valid := []string{
		"https://api.openai.com/v1",
		"http://localhost:8080",
	}
	for _, value := range valid {
		if !validAPIBase(value) {
			t.Errorf("%q 应合法", value)
		}
	}
	invalid := []string{
		"ftp://x/v1",              // scheme
		"https://",                // 无 host
		"https://x:0/v1",          // port 0
		"https://u:p@x/v1",        // 认证
		"https://x/v1?q=1",        // query
		"https://x/v1#frag",       // fragment
		"https://x/v1 with space", // 空白
	}
	for _, value := range invalid {
		if validAPIBase(value) {
			t.Errorf("%q 应非法", value)
		}
	}
}

// TestSelectionIndex 解析 name#index。
func TestSelectionIndex(t *testing.T) {
	cases := map[string]int{
		"gpt-4#0":  0,
		"gpt-4#12": 12,
		"gpt-4":    -1,
		"gpt-4#x":  -1,
		"":         -1,
	}
	for in, want := range cases {
		if got := selectionIndex(in); got != want {
			t.Errorf("selectionIndex(%q) = %d, 期望 %d", in, got, want)
		}
	}
}

// TestModelOptionsError_Error 错误文案直出。
func TestModelOptionsError_Error(t *testing.T) {
	err := &ModelOptionsError{Message: "文案"}
	if err.Error() != "文案" {
		t.Errorf("Error() = %q", err.Error())
	}
	var target *ModelOptionsError
	if !errors.As(error(err), &target) {
		t.Error("errors.As 应能识别 ModelOptionsError")
	}
}
