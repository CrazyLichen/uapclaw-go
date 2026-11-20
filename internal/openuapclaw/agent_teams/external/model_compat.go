package external

// 外部 CLI agent 共享的模型兼容原语：对齐 Python
// openjiuwen/agent_teams/external/cli_agent/model_compat.py（110 行全文）。
//
// 团队成员 spawn 路径（成员工具与外部 CLI spawn）与 swarmflow worker 路径共用，
// 使 provider/协议/模型配置映射只存在一份。

import (
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/models"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

// JiuwenExternalProvider Codex 的 OpenAI 兼容非官方端点使用的 provider 名。
// Python: _JIUWEN_EXTERNAL_PROVIDER (model_compat.py:16)
// JiuwenExternalProvider Codex 的 OpenAI 兼容非官方端点使用的 provider 名。
const JiuwenExternalProvider = "jiuwen"

// ──────────────────────────── 导出函数 ────────────────────────────

// IsAnthropicProvider provider 名是否 Anthropic 兼容。
// Python: is_anthropic_provider (model_compat.py:18)
func IsAnthropicProvider(provider string) bool {
	return strings.Contains(strings.ToLower(provider), "anthropic")
}

// ProviderFilterForCLI 返回该 CLI 种类的 provider 过滤器；未知种类返回 nil。
// 第三方 harness provider 要么声明 ENDPOINT_MODELS 经其 model_config 映射任意
// 端点（无过滤器），要么一个都不收——那种情况不给它任何池条目。
// Python: provider_filter_for_cli (model_compat.py:22)
func ProviderFilterForCLI(cliAgent string) func(string) bool {
	if cliAgent == "claude" {
		return IsAnthropicProvider
	}
	if cliAgent == "codex" {
		return func(p string) bool { return !IsAnthropicProvider(p) }
	}
	backend := BackendFor(cliAgent)
	if backend == nil || backend.Kind != BackendKindProvider {
		return nil
	}
	if ProviderTakesEndpointModels(cliAgent) {
		return nil
	}
	return func(_ string) bool { return false }
}

// CompatiblePoolModelNames 返回该 CLI 的 provider 过滤器接受的、去重排序的
// 池模型名列表。
// 模型解析器（plan_external_model 的池扫描半区；完整解析器见 model_plan.go）
// （ExternalMemberTurns 的 workaround 列表）共用，使"兼容池"概念只存在一份。
// Python: compatible_pool_model_names (model_compat.py:45)
func CompatiblePoolModelNames(cliAgent string, pool []models.ModelPoolEntry) []string {
	providerFilter := ProviderFilterForCLI(cliAgent)
	seen := make(map[string]struct{})
	for _, entry := range pool {
		name := entry.ModelName
		if name == "" {
			continue
		}
		if providerFilter == nil || providerFilter(entry.APIProvider) {
			seen[name] = struct{}{}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ModelAPIProtocol 返回模型 provider 的外部 CLI 协议标签。
// Python: model_api_protocol (model_compat.py:60)
func ModelApiProtocol(provider string) string {
	if IsAnthropicProvider(provider) {
		return "Anthropic"
	}
	return "OpenAI"
}

// ExternalCLIProviderName 解析模型客户端配置的 CLI 侧 provider 身份。
// Codex 风格的 CLI runtime 把服务端行为（远程压缩、请求体压缩）关在 provider
// 名与官方端点（"OpenAI"）匹配上。池条目的 client_provider 只是线协议标签
// ——多为 "OpenAI" 的 OpenAI 兼容网关——原样透传会让每个外部网关看起来都
// 是官方端点，破坏在未实现这些协议的端点上的受门控协议。
// Codex 的 OpenAI 兼容非官方端点用 jiuwen；其他 CLI 后端保留其配置的
// provider 身份。
//
// Python: _external_cli_provider_name (model_compat.py:65)
func ExternalCLIProviderName(clientConfig *llmschema.ModelClientConfig, cliAgent string) string {
	apiBase := strings.TrimSpace(clientConfig.APIBase)
	provider := strings.TrimSpace(clientConfig.ClientProvider)
	if cliAgent == "codex" && strings.EqualFold(provider, "openai") && apiBase != "" {
		return JiuwenExternalProvider
	}
	return provider
}

// TeamModelConfigToExternal 把池分配的 TeamModelConfig 转换为
// schema.ExternalCliModelConfig。
// Python: team_model_config_to_external (model_compat.py:90)
func TeamModelConfigToExternal(memberModel *hschema.TeamModelConfig, cliAgent string) *schema.ExternalCliModelConfig {
	if memberModel == nil {
		return nil
	}
	clientConfig := memberModel.ModelClientConfig
	requestConfig := memberModel.ModelRequestConfig
	// intelli_router 是客户端可靠路由——不是可下发给外部 CLI 的单一端点。
	if clientConfig.ClientProvider == "intelli_router" {
		return nil
	}
	provider := ExternalCLIProviderName(&clientConfig, cliAgent)
	model := ""
	if requestConfig != nil {
		model = requestConfig.ModelName
	}
	return &schema.ExternalCliModelConfig{
		Provider: EmptyToNil(provider),
		Model:    EmptyToNil(model),
		APIBase:  EmptyToNil(clientConfig.APIBase),
		APIKey:   EmptyToNil(clientConfig.APIKey),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// emptyToNil 承载 Python `x or None` 语义：Go 字段为 string（无 None 形态），
// 空串即"未设置"，经 json omitempty 序列化时与 Python None 等价——本函数
// 归一空串为未设置形状，非空串原样透传。
// EmptyToNil 空串转 nil（导出包装，model_plan 跨包消费）。
func EmptyToNil(s string) string {
	if s == "" {
		return ""
	}
	return s
}
