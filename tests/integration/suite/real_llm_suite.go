//go:build integration

package suite

import (
	"fmt"
	"os"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RealLLMSuite 包含真实 LLM API 调用能力的集成测试套件。
// 对齐 Python system_tests 中 @pytest.mark.skipif(not API_KEY) 模式：
// 环境变量 LLM_API_KEY 存在时使用真实 LLM，否则 Skip。
//
// 环境变量：
//   - LLM_API_KEY: API 密钥（必填，为空则 Skip）
//   - LLM_API_BASE: API 基础 URL（默认 https://ark.cn-beijing.volces.com/api/coding/v3）
//   - LLM_MODEL_NAME: 模型名称（默认 minimax-m3）
//   - LLM_MODEL_PROVIDER: 服务商标识（默认 OpenAI）
//
// 运行方式: go test -tags="sqlite_fts5 test integration llm" ./tests/integration/real_llm/...
type RealLLMSuite struct {
	RunnerSuite
	// RealClientConfig 真实 LLM 的客户端配置
	RealClientConfig *llmschema.ModelClientConfig
	// RealModelConfig 真实 LLM 的模型请求配置
	RealModelConfig *llmschema.ModelRequestConfig
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SetupSuite 初始化真实 LLM 测试环境。
// 读取环境变量构建真实 LLM 配置，同时保留 MockLLM 作为兜底。
func (s *RealLLMSuite) SetupSuite() {
	s.RunnerSuite.SetupSuite()

	apiKey := os.Getenv("LLM_API_KEY")
	apiBase := envWithDefault("LLM_API_BASE", "https://ark.cn-beijing.volces.com/api/coding/v3")
	modelName := envWithDefault("LLM_MODEL_NAME", "minimax-m3")
	provider := envWithDefault("LLM_MODEL_PROVIDER", "OpenAI")

	if apiKey != "" {
		s.RealClientConfig = &llmschema.ModelClientConfig{
			ClientProvider: provider,
			ClientID:       "real_llm_test",
			APIKey:         apiKey,
			APIBase:        apiBase,
			VerifySSL:      false,
			Timeout:        120,
		}
		s.RealModelConfig = &llmschema.ModelRequestConfig{
			ModelName: modelName,
		}
	}
}

// IsRealLLMAvailable 检查真实 LLM 是否可用（环境变量已设置）。
func (s *RealLLMSuite) IsRealLLMAvailable() bool {
	return s.RealClientConfig != nil
}

// SkipIfNoRealLLM 在真实 LLM 不可用时跳过测试。
// 每个测试函数开头调用：s.SkipIfNoRealLLM()
func (s *RealLLMSuite) SkipIfNoRealLLM() {
	if !s.IsRealLLMAvailable() {
		s.T().Skip("需要设置 LLM_API_KEY 环境变量，跳过真实 LLM 测试")
	}
}

// NewRealModel 创建真实 LLM 的 Model 实例。
// 调用前应先调用 SkipIfNoRealLLM() 确保环境变量已设置。
// 对齐 Python: create_model() → Model(model_client_config=..., model_config=...)
func (s *RealLLMSuite) NewRealModel() (*llm.Model, error) {
	if s.RealClientConfig == nil {
		return nil, fmt.Errorf("真实 LLM 配置不可用，请设置 LLM_API_KEY 环境变量")
	}
	return llm.NewModel(s.RealClientConfig, s.RealModelConfig)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// envWithDefault 读取环境变量，为空时返回默认值。
func envWithDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
