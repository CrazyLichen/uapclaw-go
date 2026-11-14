package adapter

import (
	"fmt"
	"strings"

	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	swcommon "github.com/uapclaw/uapclaw-go/internal/swarm/common"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultModelTemperature 模型条目未配置 temperature 时的默认值。
	// 对齐既有 deep_adapter 行为与上游应用层默认。
	defaultModelTemperature = 0.95
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildModelFromEntry 根据单个模型条目的 model_client_config / model_config_obj
// 构建 Model 实例。
// Python: server/runtime/agent_adapter/interface_deep.py:1262-1320
//
//	build_model_from_entry
//
// 模块级公开函数：除本适配器外，模型缓存构建与图像模态探测预热都要按同一规则
// 从 models.defaults 条目造 Model，共享这一份实现。
//
// 两段式（对齐上游）：
//  1. BuildReasoningModelRequestKwargs 算出 core 的完整 kwargs
//     （含 endpoint_profile 补全、reasoning 注入、采样覆盖、内部键清理）
//  2. 由 kwargs 构造 ModelRequestConfig，由 mcc 构造 ModelClientConfig
func BuildModelFromEntry(mcc, mco map[string]any) (*llm.Model, error) {
	name, _ := mcc["model_name"].(string)

	// ① mcc 去掉 model_name，其余字段留给 ModelClientConfig
	// Python: :1281-1282 name = mcc.get("model_name", "")
	mccFields := make(map[string]any, len(mcc))
	for k, v := range mcc {
		if k != "model_name" {
			mccFields[k] = v
		}
	}
	// Python: :1283-1284 client_provider 缺省 OpenAI
	if provider, _ := mccFields["client_provider"].(string); provider == "" {
		mccFields["client_provider"] = "OpenAI"
	}

	// ② 补 endpoint_profile（显式优先，否则由配置的 host 映射推断）
	// Python: :1286-1292 resolve_endpoint_profile_override(api_base or base_url)
	mccFields["endpoint_profile"] = swcommon.EffectiveEndpointProfile(
		firstNonEmpty(mccFields["api_base"], mccFields["base_url"]),
		firstNonEmpty(mccFields["endpoint_profile"]),
	)

	// ③ 共享 kwargs 构造
	// Python: :1313 request_kwargs = build_reasoning_model_request_kwargs(...)
	requestKwargs := swcommon.BuildReasoningModelRequestKwargs(mccFields, mco, name)

	// ④ 构造两个配置对象
	// Python: :1314-1319 ModelRequestConfig(**request_kwargs) / ModelClientConfig(**mcc_fields)
	mConfig := newModelRequestConfigFromKwargs(requestKwargs)
	clientConfig, err := newModelClientConfigFromFields(mccFields)
	if err != nil {
		return nil, err
	}
	return llm.NewModel(clientConfig, mConfig)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newModelRequestConfigFromKwargs 由 core kwargs 构造 ModelRequestConfig。
// Python: ModelRequestConfig(**request_kwargs)（pydantic extra="allow"）
//
// 已知字段逐项映射；其余落 RequestExtra（对齐 pydantic 的 extra 收集语义）。
func newModelRequestConfigFromKwargs(kwargs map[string]any) *llmschema.ModelRequestConfig {
	opts := make([]llmschema.ModelRequestConfigOption, 0, 8)

	// model å­æ®µæåï¼å¶ä½ kwargs è½ RequestExtraï¼
	if name, ok := kwargs["model"].(string); ok && name != "" {
		opts = append(opts, llmschema.WithModelName(name))
	}

	// temperature：缺失时用默认 0.95
	temperature := defaultModelTemperature
	if f, ok := config.Float(kwargs, "temperature"); ok {
		temperature = f
	}
	opts = append(opts, llmschema.WithTemperatureValue(temperature))

	// top_p：有才设（不设默认值）
	if f, ok := config.Float(kwargs, "top_p"); ok {
		opts = append(opts, llmschema.WithTopP(f))
	}

	// max_tokens：存在即设，含 0（0 在 OpenAI 语义里是合法的「不限制」）
	//
	// 走 config.IntValue（而非私有严格版）：上游 ModelRequestConfig(max_tokens=...)
	// 由 pydantic Optional[int] 负责强制转换，字符串 "128000" 在 lax 模式下
	// 会转换成功；config.IntValue 同样接受纯数字字符串，行为更贴上游。
	if raw, ok := kwargs["max_tokens"]; ok {
		if n, valid := config.IntValue(raw); valid {
			opts = append(opts, llmschema.WithMaxTokens(n))
		}
	}

	// stop：此前从未读取，导致用户配置静默失效
	if s, ok := kwargs["stop"].(string); ok && s != "" {
		opts = append(opts, llmschema.WithStop(s))
	}

	// context_window：已在 BuildReasoningModelRequestKwargs 归一为正整数
	if n, ok := config.ParsePositiveTokens(kwargs["context_window"]); ok {
		opts = append(opts, llmschema.WithContextWindow(n))
	}

	// reasoning：由 InjectReasoningParams 注入
	if reasoning, ok := kwargs["reasoning"]; ok {
		opts = append(opts, llmschema.WithReasoning(reasoning))
	}

	// 未识别字段落 Extra（对齐 pydantic extra="allow"）
	if extra := collectUnknownRequestKeys(kwargs); len(extra) > 0 {
		opts = append(opts, llmschema.WithRequestExtra(extra))
	}

	return llmschema.NewModelRequestConfig(opts...)
}

// newModelClientConfigFromFields 由 mcc 字段构造 ModelClientConfig。
// 从原 deepAdapter.buildModelFromEntry 的 client 部分搬入，字段处理保持等价，
// 唯一差异是 endpoint_profile 改走正式字段（此前落 Extra，ProviderKey 读不到）。
func newModelClientConfigFromFields(mcc map[string]any) (*llmschema.ModelClientConfig, error) {
	provider, _ := mcc["client_provider"].(string)
	if provider == "" {
		provider = "OpenAI"
	}
	apiKey, _ := mcc["api_key"].(string)
	apiBase, _ := mcc["api_base"].(string)

	clientOpts := make([]llmschema.ModelClientConfigOption, 0)
	// timeout 必须双类型兼容：YAML `timeout: 1800` 解出 int，只认 float64 会
	// 静默回落到默认 60s，长流式生成会被 http.Client 硬切断。
	if f, ok := config.Float(mcc, "timeout"); ok && f > 0 {
		clientOpts = append(clientOpts, llmschema.WithTimeout(f))
	}
	if v, ok := mcc["max_retries"]; ok {
		switch n := v.(type) {
		case float64:
			clientOpts = append(clientOpts, llmschema.WithMaxRetries(int(n)))
		case int:
			clientOpts = append(clientOpts, llmschema.WithMaxRetries(n))
		}
	}
	if b, ok := mcc["verify_ssl"].(bool); ok {
		clientOpts = append(clientOpts, llmschema.WithVerifySSL(b))
	}
	if s, ok := mcc["ssl_cert"].(string); ok {
		clientOpts = append(clientOpts, llmschema.WithSSLCert(s))
	}
	// endpoint_profile 必须走正式字段：reasoning.ProviderKey 读的是
	// clientConfig.EndpointProfile，落进 Extra 读不到（修复既有缺陷）。
	if s, ok := mcc["endpoint_profile"].(string); ok && s != "" {
		clientOpts = append(clientOpts, llmschema.WithEndpointProfile(s))
	}
	// custom_headers 透传：对齐 BuildEvolutionLLM 的收敛模式。
	if m, ok := mcc["custom_headers"].(map[string]any); ok && len(m) > 0 {
		headers := make(map[string]string, len(m))
		for k, hv := range m {
			if s, valid := hv.(string); valid {
				headers[k] = s
			}
		}
		if len(headers) > 0 {
			clientOpts = append(clientOpts, llmschema.WithCustomHeaders(headers))
		}
	}
	if extra := collectUnknownClientKeys(mcc); len(extra) > 0 {
		clientOpts = append(clientOpts, llmschema.WithConfigExtra(extra))
	}

	clientConfig, err := llmschema.NewModelClientConfig(provider, apiKey, apiBase, clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("创建 ModelClientConfig 失败: %w", err)
	}
	return clientConfig, nil
}

// collectUnknownRequestKeys 收集未映射到 ModelRequestConfig 正式字段的 kwargs。
func collectUnknownRequestKeys(kwargs map[string]any) map[string]any {
	extra := make(map[string]any)
	for k, v := range kwargs {
		switch k {
		case "model", "temperature", "top_p", "max_tokens", "stop",
			"context_window", "reasoning":
			continue
		default:
			extra[k] = v
		}
	}
	return extra
}

// collectUnknownClientKeys 收集未映射到 client 正式字段的 mcc 键。
// 沿用原实现的 knownKeys 排除表（含 max_input_tokens/max_output_tokens，
// 二者由本层显式消费，不能落进 Extra）。
func collectUnknownClientKeys(mcc map[string]any) map[string]any {
	known := map[string]struct{}{
		"model_name": {}, "client_provider": {}, "api_key": {}, "api_base": {},
		"timeout": {}, "max_retries": {}, "verify_ssl": {}, "ssl_cert": {},
		"custom_headers": {}, "client_id": {}, "endpoint_profile": {},
		"max_input_tokens": {}, "max_output_tokens": {},
	}
	extra := make(map[string]any)
	for k, v := range mcc {
		if _, ok := known[k]; !ok {
			extra[k] = v
		}
	}
	return extra
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(values ...any) string {
	for _, value := range values {
		if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
