// model_load.go load_member_optimizer_model 的父包落点（D-c2-3 包环移位）。

package memberoptimizer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	llmschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/foundation/llm/schema"
	"gopkg.in/yaml.v3"
)

// LoadMemberOptimizerModel 载入独立 member-optimizer 模型配置文件并构建模型。
// Python: factory.py:82-131 load_member_optimizer_model（原属 agents/factory.py；
// 因父包 finalize 需直连且 agents→父包方向不可引，移至父包——偏差 D-c2-3）。
func LoadMemberOptimizerModel(modelConfigRef string) (*BudgetedRsiModel, error) {
	// Python: if not str(model_config_ref or "").strip(): raise RuntimeError(...)
	if strings.TrimSpace(modelConfigRef) == "" {
		return nil, fmt.Errorf("model_config_ref is required for member optimization")
	}
	path, _ := expandUser(modelConfigRef)
	if abs, absErr := filepath.Abs(path); absErr == nil {
		path = abs
	}
	if info, statErr := os.Stat(path); statErr != nil || info.IsDir() {
		return nil, fmt.Errorf("model_config_ref not found: %s", modelConfigRef)
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil {
		return nil, fmt.Errorf("failed to load model_config_ref %s: %w", path, readErr)
	}
	var raw map[string]any
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(content, &raw); err != nil {
			return nil, fmt.Errorf("failed to load model_config_ref %s: %w", path, err)
		}
	default:
		if err := json.Unmarshal(content, &raw); err != nil {
			return nil, fmt.Errorf("failed to load model_config_ref %s: %w", path, err)
		}
	}
	if raw == nil {
		raw = map[string]any{}
	}

	// Python: model_data = raw.get("model", raw)
	modelData, exists := raw["model"]
	if !exists {
		modelData = raw
	}
	modelMap, isMap := modelData.(map[string]any)
	if !isMap {
		return nil, fmt.Errorf("model_config_ref model block must be a mapping: %s", path)
	}

	// Python: data = without_inner_sdk_retries(with_rsi_reasoning_policy(
	//           with_rsi_output_budget(_expand_env_vars(model_data))))
	data := WithoutInnerSDKRetries(
		WithRSIReasoningPolicy(
			WithRSIOutputBudget(expandEnvVars(modelMap).(map[string]any))))
	// Python: client_data = data.get("model_client_config"); if not isinstance(dict): raise
	clientData, ok := data["model_client_config"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("model_config_ref must contain a mapping field: model_client_config")
	}
	// Python: request_data = data.get("model_request_config"); 非映射时报错
	requestData, hasRequest := data["model_request_config"]
	if hasRequest && requestData != nil {
		if _, isMap := requestData.(map[string]any); !isMap {
			return nil, fmt.Errorf("model_request_config must be a mapping when provided")
		}
	}

	// Python: ModelClientConfig.model_validate(client_data)
	clientConfig, err := ValidateClientConfigJSON(clientData)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize model from %s: %w", path, err)
	}
	// Python: ModelRequestConfig.model_validate(request_data) if request_data is not None else None
	var requestConfig *llmschema.ModelRequestConfig
	if hasRequest && requestData != nil {
		requestConfig, err = ValidateRequestConfigJSON(requestData.(map[string]any))
		if err != nil {
			return nil, fmt.Errorf("failed to initialize model from %s: %w", path, err)
		}
	}
	// Python: return BudgetedRsiModel(model_client_config=..., model_config=...)
	model, err := NewBudgetedRsiModel(clientConfig, requestConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize model from %s: %w", path, err)
	}
	return model, nil
}
