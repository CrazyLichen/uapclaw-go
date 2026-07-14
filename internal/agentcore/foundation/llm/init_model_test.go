package llm

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestInitModel_DefaultValues 测试 InitModel 默认值对齐 Python
func TestInitModel_DefaultValues(t *testing.T) {
	// 注意：InitModel 会调用 CreateModelClient，需要已注册的 provider
	// 这里只验证配置构建逻辑，不实际创建 Model

	cfg := &initModelConfig{
		temperature: defaultInitTemperature,
		topP:        defaultInitTopP,
		timeout:     defaultInitTimeout,
		maxRetries:  defaultInitMaxRetries,
		verifySSL:   defaultInitVerifySSL,
	}

	if cfg.temperature != 0.95 {
		t.Errorf("默认 temperature 期望 0.95，实际 %f", cfg.temperature)
	}
	if cfg.topP != 0.1 {
		t.Errorf("默认 topP 期望 0.1，实际 %f", cfg.topP)
	}
	if cfg.timeout != 60.0 {
		t.Errorf("默认 timeout 期望 60.0，实际 %f", cfg.timeout)
	}
	if cfg.maxRetries != 3 {
		t.Errorf("默认 maxRetries 期望 3，实际 %d", cfg.maxRetries)
	}
	if cfg.verifySSL != false {
		t.Errorf("默认 verifySSL 期望 false，实际 %v", cfg.verifySSL)
	}
}

// TestInitModelOption_Functions 测试各选项函数
func TestInitModelOption_Functions(t *testing.T) {
	cfg := &initModelConfig{
		temperature: defaultInitTemperature,
		topP:        defaultInitTopP,
		timeout:     defaultInitTimeout,
		maxRetries:  defaultInitMaxRetries,
		verifySSL:   defaultInitVerifySSL,
	}

	WithInitTemperature(0.5)(cfg)
	if cfg.temperature != 0.5 {
		t.Errorf("WithInitTemperature 期望 0.5，实际 %f", cfg.temperature)
	}

	WithInitTopP(0.8)(cfg)
	if cfg.topP != 0.8 {
		t.Errorf("WithInitTopP 期望 0.8，实际 %f", cfg.topP)
	}

	WithInitMaxTokens(100)(cfg)
	if cfg.maxTokens == nil || *cfg.maxTokens != 100 {
		t.Errorf("WithInitMaxTokens 期望 100，实际 %v", cfg.maxTokens)
	}

	WithInitTimeout(30.0)(cfg)
	if cfg.timeout != 30.0 {
		t.Errorf("WithInitTimeout 期望 30.0，实际 %f", cfg.timeout)
	}

	WithInitMaxRetries(5)(cfg)
	if cfg.maxRetries != 5 {
		t.Errorf("WithInitMaxRetries 期望 5，实际 %d", cfg.maxRetries)
	}

	WithInitVerifySSL(true)(cfg)
	if cfg.verifySSL != true {
		t.Errorf("WithInitVerifySSL 期望 true，实际 %v", cfg.verifySSL)
	}

	headers := map[string]string{"X-Custom": "value"}
	WithInitCustomHeaders(headers)(cfg)
	if cfg.customHeaders["X-Custom"] != "value" {
		t.Errorf("WithInitCustomHeaders 期望 X-Custom=value，实际 %v", cfg.customHeaders)
	}

	WithInitSSLCert("/path/to/cert.pem")(cfg)
	if cfg.sslCert != "/path/to/cert.pem" {
		t.Errorf("WithInitSSLCert 期望 /path/to/cert.pem，实际 %s", cfg.sslCert)
	}
}

// TestInitModel_VerifySSLDefault 测试 verify_ssl 默认值与 Python 一致
func TestInitModel_VerifySSLDefault(t *testing.T) {
	// Python init_model() 的 verify_ssl 默认为 False
	// 与 ModelClientConfig 的默认 True 不同
	if defaultInitVerifySSL != false {
		t.Errorf("InitModel 的 verifySSL 默认值应为 false（对齐 Python），实际 %v", defaultInitVerifySSL)
	}

	// 而 ModelClientConfig 的 VerifySSL 默认为 True
	config := llmschema.NewModelClientConfig("test", "key", "http://localhost")
	if config.VerifySSL != true {
		t.Errorf("ModelClientConfig 的 VerifySSL 默认值应为 true，实际 %v", config.VerifySSL)
	}
}

// ──────────────────────────── InitModel 集成测试 ────────────────────────────

// TestInitModel_Success 测试 InitModel 通过注册 provider 成功创建 Model
func TestInitModel_Success(t *testing.T) {
	const testProvider = "TestInitModelProvider"

	// 注册 mock 工厂
	registry := model_clients.GetClientRegistry()
	registry.Register(testProvider, "llm", func(mc *llmschema.ModelRequestConfig, cc *llmschema.ModelClientConfig) model_clients.BaseModelClient {
		return &mockModelClient{}
	})
	defer func() { _ = registry.Unregister(testProvider, "llm") }()

	model, err := InitModel(testProvider, "gpt-4", "test-key", "http://localhost")
	if err != nil {
		t.Fatalf("InitModel 不应返回错误: %v", err)
	}
	if model == nil {
		t.Fatal("InitModel 应返回非 nil Model")
	}
	if model.ClientConfig.ClientProvider != testProvider {
		t.Errorf("ClientProvider 期望 %s，实际 %s", testProvider, model.ClientConfig.ClientProvider)
	}
	if model.ModelConfig.ModelName != "gpt-4" {
		t.Errorf("ModelName 期望 gpt-4，实际 %s", model.ModelConfig.ModelName)
	}
}

// TestInitModel_WithOptions 测试 InitModel 使用选项覆盖默认值
func TestInitModel_WithOptions(t *testing.T) {
	const testProvider = "TestInitModelOptsProvider"

	registry := model_clients.GetClientRegistry()
	registry.Register(testProvider, "llm", func(mc *llmschema.ModelRequestConfig, cc *llmschema.ModelClientConfig) model_clients.BaseModelClient {
		return &mockModelClient{}
	})
	defer func() { _ = registry.Unregister(testProvider, "llm") }()

	model, err := InitModel(
		testProvider, "gpt-4", "test-key", "http://localhost",
		WithInitTemperature(0.5),
		WithInitTopP(0.9),
		WithInitMaxTokens(2048),
		WithInitTimeout(30.0),
		WithInitMaxRetries(5),
		WithInitVerifySSL(true),
		WithInitCustomHeaders(map[string]string{"X-Custom": "value"}),
		WithInitSSLCert("/path/to/cert.pem"),
	)
	if err != nil {
		t.Fatalf("InitModel 不应返回错误: %v", err)
	}

	// 验证选项是否生效
	if model.ModelConfig.Temperature != 0.5 {
		t.Errorf("Temperature 期望 0.5，实际 %f", model.ModelConfig.Temperature)
	}
	if model.ModelConfig.TopP == nil || *model.ModelConfig.TopP != 0.9 {
		t.Errorf("TopP 期望 0.9，实际 %v", model.ModelConfig.TopP)
	}
	if model.ModelConfig.MaxTokens == nil || *model.ModelConfig.MaxTokens != 2048 {
		t.Errorf("MaxTokens 期望 2048，实际 %v", model.ModelConfig.MaxTokens)
	}
	if model.ClientConfig.Timeout != 30.0 {
		t.Errorf("Timeout 期望 30.0，实际 %f", model.ClientConfig.Timeout)
	}
	if model.ClientConfig.MaxRetries != 5 {
		t.Errorf("MaxRetries 期望 5，实际 %d", model.ClientConfig.MaxRetries)
	}
	if model.ClientConfig.VerifySSL != true {
		t.Errorf("VerifySSL 期望 true，实际 %v", model.ClientConfig.VerifySSL)
	}
	if model.ClientConfig.CustomHeaders["X-Custom"] != "value" {
		t.Errorf("CustomHeaders[X-Custom] 期望 value，实际 %v", model.ClientConfig.CustomHeaders["X-Custom"])
	}
	if model.ClientConfig.SSLCert != "/path/to/cert.pem" {
		t.Errorf("SSLCert 期望 /path/to/cert.pem，实际 %s", model.ClientConfig.SSLCert)
	}
}

// TestInitModel_UnsupportedProvider 测试 InitModel 使用未注册的 provider
func TestInitModel_UnsupportedProvider(t *testing.T) {
	_, err := InitModel("NonExistentProvider", "model", "key", "http://localhost")
	if err == nil {
		t.Error("不支持的 provider 应返回错误")
	}
}

// TestInitModel_EmptyProvider 测试 InitModel 使用空 provider
func TestInitModel_EmptyProvider(t *testing.T) {
	_, err := InitModel("", "model", "key", "http://localhost")
	if err == nil {
		t.Error("空 provider 应返回错误")
	}
}

// TestInitModel_BlankImportRegistersProviders 测试 blank import 后真实 provider 可用
//
// model_clients_register.go 中 blank import 了所有 model_client 子包，
// 触发各包的 init() 注册到 ClientRegistry。
// 本测试验证注册表中包含所有内置 provider。
func TestInitModel_BlankImportRegistersProviders(t *testing.T) {
	registry := model_clients.GetClientRegistry()
	clients := registry.ListClients()

	// 期望的内置 provider 注册键名
	expectedProviders := []string{
		"llm_OpenAI",
		"llm_OpenRouter",
		"llm_DashScope",
		"llm_DeepSeek",
		"llm_SiliconFlow",
		"llm_InferenceAffinity",
		"llm_intelli_router",
	}

	for _, expected := range expectedProviders {
		found := false
		for _, name := range clients {
			if name == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("注册表中未找到 %q，已注册: %v", expected, clients)
		}
	}
}
