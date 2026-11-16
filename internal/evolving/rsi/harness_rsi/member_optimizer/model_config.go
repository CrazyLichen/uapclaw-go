// model_config.go 提供 member 优化 agent 的模型配置载入与变换助手。
// Python: openjiuwen/rsi/harness_rsi/member_optimizer/model_config.py（全量对齐）。

package memberoptimizer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	workspace "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BudgetedRsiModel 预算化 RSI 模型包装的最小占位。
// Python: member_optimizer/budget_model.py BudgetedRsiModel（17.4c 落真身）。
// ⤴️ 17.4c：真身承载请求预算核算；本节桩仅承载变换后的模型配置字段。
type BudgetedRsiModel struct {
	// ModelClientConfig 模型客户端配置
	ModelClientConfig llmschema.ModelClientConfig
	// ModelRequestConfig 模型请求配置（可选）
	ModelRequestConfig *llmschema.ModelRequestConfig
}

// BuildDeepAgentConfig build_deep_agent_from_model_config 桩的返回载体。
// Python 真身返回 DeepAgent(config=DeepAgentConfig(model, workspace))；
// 桩阶段以配置结构承载同样两字段，DeepAgent 装配 ⤴️ 17.4c。
type BuildDeepAgentConfig struct {
	// Model 预算化模型包装（无 model 键时为 nil）
	Model *BudgetedRsiModel
	// Workspace 工作空间
	Workspace *workspace.Workspace
}

// ──────────────────────────── 常量 ────────────────────────────

// envVarRE 匹配 ${VAR} 形式的环境变量占位符。
// Python: model_config.py:16 _ENV_VAR_RE
var envVarRE = regexp.MustCompile(`\$\{(\w+)}`)

// rsiReasoningKeys RSI 禁用推理时需移除的冲突线控键。
// Python: model_config.py:31-32 keys
var rsiReasoningKeys = []string{
	"reasoning", "reasoning_effort", "thinking", "enable_thinking",
	"thinking_budget", "thinking_strategy", "reasoning_level",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// WithRSIReasoningPolicy 使 RSI 无视模型选择的前端设置、一律禁用推理。
// Python: model_config.py:19-47 with_rsi_reasoning_policy
func WithRSIReasoningPolicy(configData map[string]any) map[string]any {
	// Python: data = deepcopy(config_data)
	data := deepCopyAny(configData).(map[string]any)
	// Python: model = data.get("model", data)
	model, ok := data["model"]
	if !ok {
		model = data
	}
	modelMap, isMap := model.(map[string]any)
	if !isMap {
		return data
	}
	// Python: request = model.setdefault("model_request_config", {})
	request, exists := modelMap["model_request_config"]
	if !exists || request == nil {
		request = map[string]any{}
		modelMap["model_request_config"] = request
	}
	requestMap, isMap := request.(map[string]any)
	if !isMap {
		return data
	}
	// 移除冲突线控键；Core 将中性策略映射到各 provider。
	// Python: for values in (request, request.get("extra_body")): ...
	targets := []any{requestMap, requestMap["extra_body"]}
	for _, target := range targets {
		values, isMap := target.(map[string]any)
		if !isMap {
			continue
		}
		for _, key := range rsiReasoningKeys {
			delete(values, key)
		}
		// Python: template = values.get("chat_template_kwargs"); if isinstance(dict): template["enable_thinking"] = False
		if template, isMap := values["chat_template_kwargs"].(map[string]any); isMap {
			template["enable_thinking"] = false
		}
	}
	// Python: request["reasoning"] = {"mode": "disabled"}
	requestMap["reasoning"] = map[string]any{"mode": "disabled"}
	// Python: template = request.pop("chat_template_kwargs", None)
	template, hadTemplate := requestMap["chat_template_kwargs"]
	delete(requestMap, "chat_template_kwargs")
	if !hadTemplate {
		template = nil
	}
	if templateMap, isMap := template.(map[string]any); isMap {
		// Python: extra = request.setdefault("extra_body", {})
		extra, exists := requestMap["extra_body"]
		if !exists || extra == nil {
			extra = map[string]any{}
			requestMap["extra_body"] = extra
		}
		if extraMap, isMap := extra.(map[string]any); isMap {
			extraMap["chat_template_kwargs"] = templateMap
		}
	}
	return data
}

// WithRSIOutputBudget 拷贝 RSI 配置并保留显式配置的输出限额。
// 缺失或 None 限额交由 provider 控制；RSI 不得抹除已配置的
// max_tokens / max_completion_tokens（含 extra_body）。
// Python: model_config.py:50-60 with_rsi_output_budget
func WithRSIOutputBudget(configData map[string]any) map[string]any {
	data := deepCopyAny(configData).(map[string]any)
	// Python: model_data = data.get("model", data)
	modelData, ok := data["model"]
	if !ok {
		modelData = data
	}
	modelMap, isMap := modelData.(map[string]any)
	if !isMap {
		return data
	}
	// Python: if isinstance(model_data.get("model_request_config"), dict): ...
	if request, isMap := modelMap["model_request_config"].(map[string]any); isMap {
		modelMap["model_request_config"] = RSIOutputOptions(request)
	}
	return data
}

// RSIOutputOptions 保留显式 token 限额、线上省略 None 控制项。
// Python: model_config.py:63-74 rsi_output_options
func RSIOutputOptions(options map[string]any) map[string]any {
	// Python: data = dict(options)（浅拷贝）
	data := make(map[string]any, len(options))
	for key, value := range options {
		data[key] = value
	}
	// Python: for values in (data, data.get("extra_body")): ...
	targets := []any{any(data), data["extra_body"]}
	for i, target := range targets {
		values, isMap := target.(map[string]any)
		if !isMap {
			continue
		}
		if i > 0 {
			// Python: values = data["extra_body"] = dict(values)（复制后替换）
			copied := make(map[string]any, len(values))
			for key, value := range values {
				copied[key] = value
			}
			data["extra_body"] = copied
			values = copied
		}
		for _, key := range []string{"max_tokens", "max_completion_tokens"} {
			if value, exists := values[key]; exists && value == nil {
				delete(values, key)
			}
		}
	}
	return data
}

// LoadModelConfigRef 载入 YAML/JSON 模型配置并展开 ${VAR} 占位符。
// Python: model_config.py:77-89 load_model_config_ref
func LoadModelConfigRef(modelConfigRef string) (map[string]any, error) {
	// Python: path = Path(model_config_ref).expanduser().resolve()
	path, err := expandUser(modelConfigRef)
	if err != nil {
		return nil, err
	}
	if path, err = filepath.Abs(path); err != nil {
		return nil, err
	}
	// Python: if not path.is_file(): raise FileNotFoundError(...)
	if info, statErr := os.Stat(path); statErr != nil || info.IsDir() {
		return nil, fmt.Errorf("model_config_ref not found: %s", modelConfigRef)
	}
	// Python: with open(path, encoding="utf-8") as file: ...（按后缀分派 YAML/JSON）
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var data any
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml":
		// Python: yaml.safe_load(file) or {}
		if err := yaml.Unmarshal(content, &data); err != nil {
			return nil, err
		}
		if data == nil {
			data = map[string]any{}
		}
	default:
		if err := json.Unmarshal(content, &data); err != nil {
			return nil, err
		}
	}
	// Python: if not isinstance(data, dict): raise ValueError(...)
	dataMap, isMap := data.(map[string]any)
	if !isMap {
		return nil, fmt.Errorf("model_config_ref must decode to a mapping: %s", path)
	}
	// Python: return with_rsi_reasoning_policy(with_rsi_output_budget(_expand_env_vars(data)))
	budgeted := WithRSIOutputBudget(expandEnvVars(dataMap).(map[string]any))
	return WithRSIReasoningPolicy(budgeted), nil
}

// BuildDeepAgentFromModelConfig 从载入的 member 优化模型配置构建 DeepAgent。
// Python: model_config.py:92-119 build_deep_agent_from_model_config
//
// ⤴️ 17.4c 回填：Python 真身返回 BudgetedRsiModel 包装的 DeepAgent；本节按拍板
// 方案②落桩——BudgetedRsiModel 以最小占位承载变换后的模型配置（字段语义见
// BuildDeepAgentConfig），DeepAgent 装配（BudgetedRsiModel 真身 + harness 工厂
// 接线）在 17.4c 完成。Python 全仓该函数当前零调用（死代码保留纪律）。
func BuildDeepAgentFromModelConfig(configData map[string]any) (*BuildDeepAgentConfig, error) {
	// Python: config_data = with_rsi_reasoning_policy(with_rsi_output_budget(config_data))
	transformed := WithRSIReasoningPolicy(WithRSIOutputBudget(configData))
	// Python: model = None; if "model" in config_data: ...
	var budgeted *BudgetedRsiModel
	if modelData, exists := transformed["model"]; exists {
		// Python: built = TeamModelConfig.model_validate(without_inner_sdk_retries(config_data["model"])).build()
		built, err := validateTeamModelConfig(WithoutInnerSDKRetries(modelData.(map[string]any)))
		if err != nil {
			return nil, err
		}
		// Python: model = BudgetedRsiModel(model_client_config=built.model_client_config, model_config=built.model_config)
		budgeted = &BudgetedRsiModel{
			ModelClientConfig:  built.ModelClientConfig,
			ModelRequestConfig: built.ModelRequestConfig,
		}
	}

	// Python: workspace = Workspace(root_path="./", language="en")
	rootPath := "./"
	language := "en"
	// Python: if "workspace" in config_data and isinstance(dict): ...
	if workspaceData, exists := transformed["workspace"]; exists {
		if workspaceMap, isMap := workspaceData.(map[string]any); isMap {
			if root, ok := workspaceMap["root_path"]; ok {
				rootPath, _ = root.(string)
			}
			if lang, ok := workspaceMap["language"]; ok {
				language, _ = lang.(string)
			}
		}
	}

	return &BuildDeepAgentConfig{
		Model:     budgeted,
		Workspace: workspace.NewWorkspace(rootPath, language),
	}, nil
}

// WithoutInnerSDKRetries 返回禁用 SDK 级重试的模型配置拷贝。
// auto coordinating harness 通过 run_model_call_with_retries 自持重试预算；
// 兼容 OpenAI 的 SDK 内部重试会让单次可见尝试阻塞多个超时窗口，因此
// ACH 载入的模型配置一律以 max_retries=0 进入底层 client。
// Python: model_config.py:122-139 without_inner_sdk_retries
func WithoutInnerSDKRetries(configData map[string]any) map[string]any {
	// Python: data = deepcopy(config_data)
	data := deepCopyAny(configData).(map[string]any)
	if data == nil {
		return data
	}
	// Python: _disable_client_retries(data)
	disableClientRetries(data)
	// Python: model_data = data.get("model"); if isinstance(dict): _disable_client_retries(model_data)
	if modelData, isMap := data["model"].(map[string]any); isMap {
		disableClientRetries(modelData)
	}
	return data
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// deepCopyAny 复刻 Python copy.deepcopy 对 JSON 兼容值的深拷贝语义。
func deepCopyAny(value any) any {
	switch v := value.(type) {
	case map[string]any:
		copied := make(map[string]any, len(v))
		for key, item := range v {
			copied[key] = deepCopyAny(item)
		}
		return copied
	case []any:
		copied := make([]any, len(v))
		for i, item := range v {
			copied[i] = deepCopyAny(item)
		}
		return copied
	default:
		return value
	}
}

// disableClientRetries 将 model_client_config.max_retries 置 0。
// Python: model_config.py:142-145 _disable_client_retries
func disableClientRetries(modelData map[string]any) {
	if clientData, isMap := modelData["model_client_config"].(map[string]any); isMap {
		clientData["max_retries"] = 0
	}
}

// expandEnvVars 递归展开字符串值中的 ${VAR} 占位符（未知变量保留原样）。
// Python: model_config.py:148-158 _expand_env_vars
func expandEnvVars(value any) any {
	switch v := value.(type) {
	case string:
		return envVarRE.ReplaceAllStringFunc(v, func(match string) string {
			groups := envVarRE.FindStringSubmatch(match)
			if envValue, ok := os.LookupEnv(groups[1]); ok {
				return envValue
			}
			return match
		})
	case map[string]any:
		expanded := make(map[string]any, len(v))
		for key, item := range v {
			expanded[key] = expandEnvVars(item)
		}
		return expanded
	case []any:
		expanded := make([]any, len(v))
		for i, item := range v {
			expanded[i] = expandEnvVars(item)
		}
		return expanded
	default:
		return value
	}
}

// expandUser 复刻 Python Path.expanduser 的 ~ 展开。
func expandUser(path string) (string, error) {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			return home, nil
		}
		if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
			return filepath.Join(home, path[2:]), nil
		}
	}
	return path, nil
}

// validateTeamModelConfig 等价 Python TeamModelConfig.model_validate 的校验构建。
// 注意：Python pydantic 校验失败抛 ValidationError；Go 经 JSON 解码失败返回 error。
func validateTeamModelConfig(modelData map[string]any) (hschema.TeamModelConfig, error) {
	var config hschema.TeamModelConfig
	raw, err := json.Marshal(modelData)
	if err != nil {
		return config, err
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return config, err
	}
	return config, nil
}

// ValidateTeamModelConfig 校验构建 TeamModelConfig（导出包装）。
// Python: agent_runtime.py:105 TeamModelConfig.model_validate(without_inner_sdk_retries(model_data))——
// 17.4b2 诊断 agent 运行时消费；内部实现即 validateTeamModelConfig。
func ValidateTeamModelConfig(modelData map[string]any) (hschema.TeamModelConfig, error) {
	return validateTeamModelConfig(modelData)
}
