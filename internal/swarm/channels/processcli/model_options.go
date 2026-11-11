package processcli

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	configpkg "github.com/uapclaw/uapclaw-go/internal/common/config"
	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
)

// 本文件是 channels/process_cli/model_options.py 的 Go 对应物。
//
// Python: channels/process_cli/model_options.py（158 行）
//
// 职责：普通命令的模型选择与临时连接配置。密钥只从环境变量读取，
// 只把无凭证的选择器放进执行请求。

// ──────────────────────────── 结构体 ────────────────────────────

// ModelOptionsError 启动 Runtime 之前展示的无凭证配置错误。
// Python: model_options.py:19-20 ModelOptionsError
type ModelOptionsError struct {
	// Message 用户可见的错误文案
	Message string
}

// Error 实现 error 接口。
func (e *ModelOptionsError) Error() string { return e.Message }

// ──────────────────────────── 常量 ────────────────────────────

const (
	// apiKeyEnvPattern 环境变量名合法模式
	// Python: model_options.py:88 r"[A-Za-z_][A-Za-z0-9_]*"
	apiKeyEnvPattern = `^[A-Za-z_][A-Za-z0-9_]*$`
)

// ──────────────────────────── 全局变量 ────────────────────────────

// apiKeyEnvRegex 编译后的环境变量名正则。
var apiKeyEnvRegex = regexp.MustCompile(apiKeyEnvPattern)

// authenticationHeaderNames 换端点时必须清掉的认证头。
// Python: model_options.py:137-142 authentication_headers
var authenticationHeaderNames = map[string]struct{}{
	"authorization":       {},
	"api-key":             {},
	"x-api-key":           {},
	"proxy-authorization": {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateModelOptions 校验 flag 组合，并在 dotenv 加载之前捕获密钥。
// Python: model_options.py:49-108 validate_model_options
//
// 返回值只交给调用方，不挂到 AppArgs 上。
func ValidateModelOptions(args *AppArgs) (string, error) {
	model := args.Model
	connectionProvided := args.APIBase != "" || args.APIKeyEnv != "" || args.ModelProvider != ""

	// Python: :60-61 全空 → 无需校验
	if model == "" && !connectionProvided {
		return "", nil
	}
	// Python: :62-63 连接参数只支持带任务文本的非交互命令
	if args.Prompt == "" || args.InteractiveWorker {
		return "", &ModelOptionsError{Message: "模型连接参数仅支持带任务文本的非交互式命令。"}
	}
	// Python: :64-65 必须同时指定非空 model
	if strings.TrimSpace(model) == "" {
		return "", &ModelOptionsError{Message: "模型连接参数需要同时指定非空的 --model。"}
	}

	if args.APIBase != "" {
		// Python: :66-85 api-base 必须是无认证信息的 HTTP(S) 基础地址
		if !validAPIBase(args.APIBase) {
			return "", &ModelOptionsError{
				Message: "--api-base 必须是 HTTP(S) 服务基础地址，不能包含认证信息、查询参数或片段。",
			}
		}
		if args.APIKeyEnv == "" {
			return "", &ModelOptionsError{Message: "指定 --api-base 时必须同时指定 --api-key-env。"}
		}
	}
	// Python: :86-87 model-provider 依附 api-base
	if args.ModelProvider != "" && args.APIBase == "" {
		return "", &ModelOptionsError{Message: "指定 --model-provider 时必须同时指定 --api-base。"}
	}
	// Python: :89-90 无 api-key-env 即结束
	if args.APIKeyEnv == "" {
		return "", nil
	}
	// Python: :91-92 环境变量名合法（不是密钥本身）
	if !apiKeyEnvRegex.MatchString(args.APIKeyEnv) {
		return "", &ModelOptionsError{Message: "--api-key-env 应填写环境变量名称，而不是密钥内容。"}
	}
	key := os.Getenv(args.APIKeyEnv)
	if strings.TrimSpace(key) == "" {
		return "", &ModelOptionsError{Message: "--api-key-env 指定的环境变量未设置或为空。"}
	}
	// Python: :95-96 密钥不得含换行
	if strings.ContainsAny(key, "\r\n") {
		return "", &ModelOptionsError{Message: "--api-key-env 指定的密钥不能包含换行。"}
	}
	return strings.TrimSpace(key), nil
}

// PrepareModelOptions 解析已配置选择器，或在内存中构造新连接。
// Python: model_options.py:110-158 prepare_model_options
//
// 只有无凭证的选择器加入执行请求。返回 nil 表示沿用已有配置。
func PrepareModelOptions(args *AppArgs, apiKey string) (map[string]any, error) {
	if args.Model == "" {
		return nil, nil
	}

	entries := configuredModelEntries()
	requested := strings.TrimSpace(args.Model)
	catalog := swruntime.BuildModelCatalogFromEntries(entries, "")
	selected, selectErr := swruntime.ResolveModelSelection(catalog, requested)

	if selectErr != nil {
		// Python: :125-133 未找到且没有 api-base → 提示新模型需要连接参数
		if args.APIBase == "" || selectErr != swruntime.ErrModelNotFound {
			return nil, &ModelOptionsError{
				Message: "--model 未找到唯一的已配置模型；请使用模型名称、唯一别名或 name#索引。" +
					"新模型需要同时指定 --api-base 和 --api-key-env。",
			}
		}
		if strings.Contains(requested, "#") {
			return nil, &ModelOptionsError{Message: "新的模型名称不能使用 name#索引 选择器。"}
		}
	}

	// Python: :135-140 无 api-base 且无密钥 → 只校验已配置条目
	if args.APIBase == "" && apiKey == "" {
		index := selectionIndex(selected.SelectionKey)
		if index < 0 || index >= len(entries) {
			return nil, &ModelOptionsError{Message: "所选模型索引越界。"}
		}
		if err := validateConnection(entries[index]); err != nil {
			return nil, err
		}
		args.ModelSelection = selected.SelectionKey
		return nil, nil
	}

	// Python: :142-158 构造新连接
	index := -1
	if selectErr == nil {
		index = selectionIndex(selected.SelectionKey)
	}
	var entry map[string]any
	if index >= 0 && index < len(entries) {
		entry = copyAnyMap(entries[index])
	} else {
		entry = map[string]any{}
	}

	client, _ := entry["model_client_config"].(map[string]any)
	client = copyAnyMap(client)
	modelName := requested
	if selectErr == nil {
		modelName = selected.ModelName
	}

	if args.APIBase != "" {
		// Python: :144-152 换目标时不得转发旧凭证、额外头或方言设置
		provider := args.ModelProvider
		if provider == "" {
			if existing, ok := client["client_provider"].(string); ok {
				provider = existing
			}
		}
		if provider == "" {
			provider = "OpenAI"
		}
		client = map[string]any{
			"client_provider": provider,
			"api_base":        args.APIBase,
			"verify_ssl":      true,
		}
	} else if headers, ok := client["custom_headers"].(map[string]any); ok {
		// Python: :153-165 保留非认证头，清掉认证头
		filtered := make(map[string]any, len(headers))
		for name, value := range headers {
			if _, isAuth := authenticationHeaderNames[strings.ToLower(name)]; isAuth {
				continue
			}
			filtered[name] = value
		}
		client["custom_headers"] = filtered
	}

	client["model_name"] = modelName
	client["api_key"] = apiKey
	entry["model_client_config"] = client
	entry["is_default"] = true

	if err := validateConnection(entry); err != nil {
		return nil, err
	}
	args.ModelSelection = modelName
	return entry, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// validateConnection 试建一次连接；失败时抛不含凭证的固定文案。
// Python: model_options.py:34-46 _validate_connection
//
// 原始异常可能含凭证，一律不透传（上游 except Exception 后转固定文案）。
func validateConnection(entry map[string]any) error {
	mcc, _ := entry["model_client_config"].(map[string]any)
	mco, _ := entry["model_config_obj"].(map[string]any)
	if _, err := adapter.BuildModelFromEntry(copyAnyMap(mcc), copyAnyMap(mco)); err != nil {
		return &ModelOptionsError{
			Message: "所选模型连接配置无效；请检查 API 地址、密钥来源、提供方和模型配置。",
		}
	}
	return nil
}

// validAPIBase 校验 api-base 是干净的服务基础地址。
// Python: model_options.py:67-84 urlsplit 七项检查
func validAPIBase(apiBase string) bool {
	if strings.ContainsAny(apiBase, " \t\r\n") {
		return false
	}
	parsed, err := url.Parse(apiBase)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	if parsed.Hostname() == "" {
		return false
	}
	if parsed.Port() == "0" {
		return false
	}
	if parsed.User != nil {
		return false
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	return true
}

// configuredModelEntries 读 models.defaults 条目。
// Python: model_options.py:119 get_default_models()
func configuredModelEntries() []map[string]any {
	return configDefaultModels()
}

// selectionIndex 从 "name#index" 取尾部下标；无 # 或解析失败返回 -1。
// Python: model_options.py:136 int(selected.selection_key.rsplit("#", 1)[1])
func selectionIndex(selectionKey string) int {
	index := strings.LastIndex(selectionKey, "#")
	if index < 0 {
		return -1
	}
	value := 0
	for _, r := range selectionKey[index+1:] {
		if r < '0' || r > '9' {
			return -1
		}
		value = value*10 + int(r-'0')
	}
	return value
}

// fmtUnused 保留 fmt 引用（错误文案构造用）。
var _ = fmt.Sprintf

// configDefaultModels 读取 models.defaults 配置条目（空配置时返回空切片）。
// Python: model_options.py:119 from ...common.config import get_default_models
func configDefaultModels() []map[string]any {
	return configpkg.GetDefaultModels(nil)
}
