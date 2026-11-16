package memberoptimizer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestWithRSIReasoningPolicy_禁用推理配置 对照 Python with_rsi_reasoning_policy 逐分支断言。
// Python: model_config.py:19-47
func TestWithRSIReasoningPolicy_禁用推理配置(t *testing.T) {
	// ① request 与 extra_body 中的冲突键被移除，reasoning 置 disabled
	input := map[string]any{
		"model": map[string]any{
			"model_request_config": map[string]any{
				"reasoning":            "auto",
				"thinking_budget":      1024,
				"extra_body":           map[string]any{"enable_thinking": true, "keep": 1},
				"chat_template_kwargs": map[string]any{"enable_thinking": true, "lang": "zh"},
			},
		},
	}
	out := WithRSIReasoningPolicy(input)
	req := out["model"].(map[string]any)["model_request_config"].(map[string]any)
	if _, ok := req["reasoning_effort"]; ok {
		t.Fatal("reasoning_effort 应被移除")
	}
	if _, ok := req["thinking_budget"]; ok {
		t.Fatal("thinking_budget 应被移除")
	}
	r, ok := req["reasoning"].(map[string]any)
	if !ok || r["mode"] != "disabled" {
		t.Fatalf("reasoning 应为 {mode:disabled}, got %v", req["reasoning"])
	}
	eb := req["extra_body"].(map[string]any)
	// Python: enable_thinking 在 keys 清单中，extra_body 的该键被"移除"而非置 false；
	// 置 false 的是 request.chat_template_kwargs（移入 extra_body 后可见）。
	if _, ok := eb["enable_thinking"]; ok {
		t.Fatal("extra_body.enable_thinking 应被移除（在 keys 清单中）")
	}
	if eb["keep"] != 1 {
		t.Fatal("extra_body.keep 应保留")
	}
	// request 的 chat_template_kwargs 被移入 extra_body
	ctk, ok := eb["chat_template_kwargs"].(map[string]any)
	if !ok || ctk["enable_thinking"] != false || ctk["lang"] != "zh" {
		t.Fatalf("request.chat_template_kwargs 应移入 extra_body, got %v", eb["chat_template_kwargs"])
	}
	if _, ok := req["chat_template_kwargs"]; ok {
		t.Fatal("request.chat_template_kwargs 应被移除")
	}

	// ② 无 model 键时作用于 data 本身
	flat := map[string]any{"model_request_config": map[string]any{"thinking": true}}
	out2 := WithRSIReasoningPolicy(flat)
	req2 := out2["model_request_config"].(map[string]any)
	if v, ok := req2["reasoning"].(map[string]any); !ok || v["mode"] != "disabled" {
		t.Fatalf("无 model 键应作用于 data 本身, got %v", req2["reasoning"])
	}

	// ③ model 非 dict 时原样返回
	nonDict := map[string]any{"model": "text-model"}
	out3 := WithRSIReasoningPolicy(nonDict)
	if out3["model"] != "text-model" {
		t.Fatal("model 非 dict 应原样返回")
	}

	// ④ request 非 dict 时原样返回
	badReq := map[string]any{"model": map[string]any{"model_request_config": "oops"}}
	out4 := WithRSIReasoningPolicy(badReq)
	if out4["model"].(map[string]any)["model_request_config"] != "oops" {
		t.Fatal("request 非 dict 应原样返回")
	}

	// ⑤ 不修改入参（deepcopy 语义）
	if input["model"].(map[string]any)["model_request_config"].(map[string]any)["reasoning"] != "auto" {
		t.Fatal("入参不应被修改")
	}
}

// TestWithRSIOutputBudget_保留显式限额 对照 Python with_rsi_output_budget / rsi_output_options。
// Python: model_config.py:50-74
func TestWithRSIOutputBudget_保留显式限额(t *testing.T) {
	// ① None 限额被移除，显式限额保留
	input := map[string]any{
		"model": map[string]any{
			"model_request_config": map[string]any{
				"max_tokens":            nil,
				"max_completion_tokens": 4096,
				"extra_body":            map[string]any{"max_tokens": nil, "temperature": 0.7},
			},
		},
	}
	out := WithRSIOutputBudget(input)
	req := out["model"].(map[string]any)["model_request_config"].(map[string]any)
	if _, ok := req["max_tokens"]; ok {
		t.Fatal("None max_tokens 应被移除")
	}
	if req["max_completion_tokens"] != 4096 {
		t.Fatal("显式 max_completion_tokens 应保留")
	}
	eb := req["extra_body"].(map[string]any)
	if _, ok := eb["max_tokens"]; ok {
		t.Fatal("extra_body None max_tokens 应被移除")
	}
	if eb["temperature"] != 0.7 {
		t.Fatal("extra_body.temperature 应保留")
	}

	// ② 无 model_request_config 时原样返回
	noReq := map[string]any{"model": map[string]any{"other": 1}}
	out2 := WithRSIOutputBudget(noReq)
	if _, ok := out2["model"].(map[string]any)["model_request_config"]; ok {
		t.Fatal("不应凭空创建 model_request_config")
	}

	// ③ extra_body 不与入参共享引用（shallow copy 后替换）
	sharedExtra := map[string]any{}
	input3 := map[string]any{"model": map[string]any{
		"model_request_config": map[string]any{"max_tokens": nil, "extra_body": sharedExtra},
	}}
	_ = WithRSIOutputBudget(input3)
	if len(sharedExtra) != 0 {
		// extra_body 被复制替换，原 map 不应被改动
		if _, ok := sharedExtra["max_tokens"]; ok {
			t.Fatal("入参 extra_body 不应被修改")
		}
	}
}

// TestLoadModelConfigRef_读取与环境变量展开 对照 Python load_model_config_ref / _expand_env_vars。
// Python: model_config.py:77-89, 148-158
func TestLoadModelConfigRef_读取与环境变量展开(t *testing.T) {
	dir := t.TempDir()

	// ① YAML 读取 + 组合变换 + ${VAR} 展开
	t.Setenv("RSI_TEST_MODEL", "test-model-name")
	yamlPath := filepath.Join(dir, "cfg.yaml")
	yamlContent := `model:
  model_client_config:
    model_name: ${RSI_TEST_MODEL}
    timeout: 60
  model_request_config:
    reasoning: auto
    max_tokens: null
`
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := LoadModelConfigRef(yamlPath)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	model := data["model"].(map[string]any)
	mcc := model["model_client_config"].(map[string]any)
	if mcc["model_name"] != "test-model-name" {
		t.Fatalf("环境变量应展开, got %v", mcc["model_name"])
	}
	req := model["model_request_config"].(map[string]any)
	if _, ok := req["max_tokens"]; ok {
		t.Fatal("组合变换应包含 output budget（None max_tokens 移除）")
	}
	if r, ok := req["reasoning"].(map[string]any); !ok || r["mode"] != "disabled" {
		t.Fatal("组合变换应包含 reasoning policy")
	}

	// ② JSON 读取（非 yaml/yml 后缀走 json）
	jsonPath := filepath.Join(dir, "cfg.json")
	if err := os.WriteFile(jsonPath, []byte(`{"model":{"model_client_config":{"model_name":"m1"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	data2, err := LoadModelConfigRef(jsonPath)
	if err != nil {
		t.Fatalf("JSON 读取失败: %v", err)
	}
	if data2["model"].(map[string]any)["model_client_config"].(map[string]any)["model_name"] != "m1" {
		t.Fatal("JSON 内容不符")
	}

	// ③ 文件不存在 → FileNotFoundError 等价错误文案
	_, err = LoadModelConfigRef(filepath.Join(dir, "missing.yaml"))
	if err == nil || err.Error() != "model_config_ref not found: "+filepath.Join(dir, "missing.yaml") {
		t.Fatalf("缺失文件错误文案不符: %v", err)
	}

	// ④ 非 mapping → ValueError 等价错误文案
	listPath := filepath.Join(dir, "list.yaml")
	if err := os.WriteFile(listPath, []byte("- a\n- b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = LoadModelConfigRef(listPath)
	if err == nil {
		t.Fatal("非 mapping 应报错")
	}

	// ⑤ 空文件（safe_load → {}）：model 落到 data 本身，setdefault 会凭空创建
	//    model_request_config 并写入 reasoning disabled（Python 语义，非空 map）
	emptyPath := filepath.Join(dir, "empty.yaml")
	if err := os.WriteFile(emptyPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	data3, err := LoadModelConfigRef(emptyPath)
	if err != nil {
		t.Fatalf("空 YAML 不应报错: %v", err)
	}
	req3, ok := data3["model_request_config"].(map[string]any)
	if !ok {
		t.Fatalf("空 YAML 应凭空创建 model_request_config, got %v", data3)
	}
	if r, ok := req3["reasoning"].(map[string]any); !ok || r["mode"] != "disabled" {
		t.Fatalf("空 YAML 应写入 reasoning disabled, got %v", req3)
	}

	// ⑥ 未知 ${VAR} 保留原样
	unkPath := filepath.Join(dir, "unk.yaml")
	if err := os.WriteFile(unkPath, []byte("model: ${RSI_TEST_UNSET_VAR_XYZ}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	data4, err := LoadModelConfigRef(unkPath)
	if err != nil {
		t.Fatal(err)
	}
	if data4["model"] != "${RSI_TEST_UNSET_VAR_XYZ}" {
		t.Fatalf("未知变量应保留原样, got %v", data4["model"])
	}

	// ⑦ list 值中的 ${VAR} 递归展开（_expand_env_vars list 分支）
	listValPath := filepath.Join(dir, "listval.yaml")
	if err := os.WriteFile(listValPath, []byte("model:\n  fallback:\n    - ${RSI_TEST_MODEL}\n    - raw\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	data5, err := LoadModelConfigRef(listValPath)
	if err != nil {
		t.Fatal(err)
	}
	fallback := data5["model"].(map[string]any)["fallback"].([]any)
	if fallback[0] != "test-model-name" || fallback[1] != "raw" {
		t.Fatalf("list 内变量应递归展开, got %v", fallback)
	}

	// ⑧ ~ 展开（expandUser 分支）：内部路径展开用于 is_file 检查，
	//    但 Python 错误文案使用原始 ref（未展开），Go 保持一致
	_, err = LoadModelConfigRef("~/rsi_test_missing_cfg.yaml")
	if err == nil || err.Error() != "model_config_ref not found: ~/rsi_test_missing_cfg.yaml" {
		t.Fatalf("~ 路径缺失文件错误文案不符: %v", err)
	}
}

// TestWithoutInnerSDKRetries_禁用内层重试 对照 Python without_inner_sdk_retries / _disable_client_retries。
// Python: model_config.py:122-145
func TestWithoutInnerSDKRetries_禁用内层重试(t *testing.T) {
	// ① 顶层 model_client_config 置零
	input := map[string]any{
		"model_client_config": map[string]any{"max_retries": 3, "timeout": 60},
		"model": map[string]any{
			"model_client_config": map[string]any{"max_retries": 5},
		},
	}
	out := WithoutInnerSDKRetries(input)
	if out["model_client_config"].(map[string]any)["max_retries"] != 0 {
		t.Fatal("顶层 max_retries 应置 0")
	}
	if out["model"].(map[string]any)["model_client_config"].(map[string]any)["max_retries"] != 0 {
		t.Fatal("model.model_client_config.max_retries 应置 0")
	}
	if out["model_client_config"].(map[string]any)["timeout"] != 60 {
		t.Fatal("其他字段应保留")
	}
	// deepcopy：入参不被修改
	if input["model_client_config"].(map[string]any)["max_retries"] != 3 {
		t.Fatal("入参不应被修改")
	}

	// ② 无 model_client_config 键时不凭空创建
	noClient := map[string]any{"model": map[string]any{"other": 1}}
	out2 := WithoutInnerSDKRetries(noClient)
	if _, ok := out2["model_client_config"]; ok {
		t.Fatal("不应凭空创建 model_client_config")
	}
	if _, ok := out2["model"].(map[string]any)["model_client_config"]; ok {
		t.Fatal("不应凭空创建 model.model_client_config")
	}

	// ③ 非 dict 原样返回
	nonDict := map[string]any{"model_client_config": "oops"}
	out3 := WithoutInnerSDKRetries(nonDict)
	if out3["model_client_config"] != "oops" {
		t.Fatal("非 dict client 应原样保留")
	}
}

// TestBuildDeepAgentFromModelConfig 对齐 Python build_deep_agent_from_model_config 真身
//（17.4c1 回填：BudgetedRsiModel 真身 + DeepAgent 装配）。
// Python: model_config.py:92-119
func TestBuildDeepAgentFromModelConfig(t *testing.T) {
	// ① 有 model 键：先变换（reasoning policy + output budget）再校验构建，
	//    BudgetedRsiModel 桩承载 model_client_config / model_request_config
	input := map[string]any{
		"model": map[string]any{
			"model_client_config": map[string]any{"model_name": "m1", "client_provider": "llm_OpenAI", "client_id": "c1", "max_retries": 3},
			"model_request_config": map[string]any{
				"reasoning":  "auto",
				"max_tokens": nil,
			},
		},
	}
	cfg, err := BuildDeepAgentFromModelConfig(context.Background(), input)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if cfg.Model == nil {
		t.Fatal("真身应承载 BudgetedRsiModel")
	}
	if cfg.Model.ClientConfig.MaxRetries != 0 {
		t.Fatalf("模型配置应经过 without_inner_sdk_retries, max_retries=%v", cfg.Model.ClientConfig.MaxRetries)
	}
	if cfg.DeepAgent == nil {
		t.Fatal("真身应装配 DeepAgent")
	}

	// ② 无 model 键：model 为 nil，默认工作空间
	cfg2, err := BuildDeepAgentFromModelConfig(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("空配置构建失败: %v", err)
	}
	if cfg2.Model != nil {
		t.Fatal("无 model 键时 Model 应为 nil")
	}
	if cfg2.DeepAgent == nil {
		t.Fatal("无 model 键时仍应装配 DeepAgent（model=None 语义）")
	}
	if cfg2.Workspace == nil {
		t.Fatal("应创建默认工作空间")
	}

	// ③ workspace 配置生效
	cfg3, err := BuildDeepAgentFromModelConfig(context.Background(), map[string]any{
		"workspace": map[string]any{"root_path": "/tmp/ws", "language": "zh"},
	})
	if err != nil {
		t.Fatalf("workspace 构建失败: %v", err)
	}
	if cfg3.Workspace == nil || cfg3.Workspace.RootPath != "/tmp/ws" {
		t.Fatalf("workspace.root_path 应生效, got %+v", cfg3.Workspace)
	}
}
