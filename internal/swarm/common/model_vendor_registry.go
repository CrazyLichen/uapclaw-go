// 模型厂商预设注册表：纯数据表，(厂商, 套餐) 组合到自动填充配置的映射。
// 对应 Python 代码：gateway/common/model_vendor_registry.py:1-593（无双副本，gateway 侧为唯一源）。
// 数据来自"模型厂商汇总表.xlsx"，每个端点 URL 于 2026-08-05 经 curl 验证
// （401/403/400 = 端点存在需鉴权，200 = 公开，404 = 不存在）。
package common

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/reasoning"
)

// ──────────────────────────── 枚举 ────────────────────────────

// PlanKind 模型选择的套餐类别。
// Python: model_vendor_registry.py:63 PlanKind。
type PlanKind string

const (
	// PlanKindTokenPlan 预付费套餐/资源包/订阅
	PlanKindTokenPlan PlanKind = "token_plan"
	// PlanKindCodingPlan 编码场景
	PlanKindCodingPlan PlanKind = "coding_plan"
	// PlanKindCustomAPI 通用 Token（按量计费）
	PlanKindCustomAPI PlanKind = "custom_api"
)

// AnthropicClientProvider core 的 ProviderType.Anthropic 枚举值。
// Python: model_vendor_registry.py:101 ANTHROPIC_CLIENT_PROVIDER。
// 前端选 "Anthropic 格式" 时落库 client_provider 用该值（core 实例化
// AnthropicModelClient 走 /v1/messages）。Anthropic 格式可用的充要条件：
// 该预设的 anthropic_base 非空。
const AnthropicClientProvider = "Anthropic"


// ──────────────────────────── 结构体 ────────────────────────────

// VendorPreset 单个 (厂商, 套餐) 预设条目。
// Python: model_vendor_registry.py:71 VendorPreset。
// 选中即自动填充；用户只需填 api_key（可选从 model_options 选型或经
// models_endpoint 拉最新列表）。
type VendorPreset struct {
	// VendorKey 厂商标识（如 "alibaba"、"baidu"）
	VendorKey string
	// DisplayName 展示名（如 "阿里云百炼"）
	DisplayName string
	// Plan 所属套餐
	Plan PlanKind
	// ClientProvider 落库用（config.yaml models.defaults[].model_client_config），
	// 必须等于 ProviderType 枚举值
	ClientProvider string
	// APIBase 本套餐的 OpenAI 格式 base_url
	APIBase string
	// DefaultModel 该条目预选模型
	DefaultModel string
	// ModelOptions 下拉候选（已验证的常用模型）
	ModelOptions []string
	// IconKey 对齐前端 ModelProviderIcon PROVIDER_SPECS.key
	IconKey string
	// ModelsEndpoint GET /models 端点；空 = 不支持（vendors.fetch_models RPC）
	ModelsEndpoint string
	// ModelsNeedsKey fetch 是否需要 api_key
	ModelsNeedsKey bool
	// AnthropicBase 本套餐的 Anthropic 格式 base（custom_api 允许 OpenAI<->Anthropic 切换）；
	// 空 = 该厂商无 Anthropic 端点
	AnthropicBase string
	// EndpointProfile OpenAI 协议端点方言（deepseek/openrouter/siliconflow/dashscope/
	// openai_compatible/...）；空 = 不写、走默认 openai；Anthropic 协议时 core 忽略此字段
	EndpointProfile string
	// GenerationOnly 仅用于图片/视频生成的厂商（如自部署 vLLM-Omni）：
	// 不出现在对话模型选择中
	GenerationOnly bool
}

// ──────────────────────────── 全局变量 ────────────────────────────

// presets 预设总表，逐条照抄 Python :112-521（含 xlsx 勘误注释）。
var presets = []*VendorPreset{
	// ── Token Plan ──────────────────────────────────────────────────────────
	{
		VendorKey: "alibaba", DisplayName: "阿里云百炼", Plan: PlanKindTokenPlan,
		ClientProvider: "OpenAI",
		APIBase:        "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1",
		EndpointProfile: "dashscope",
		DefaultModel:   "qwen3.7-max",
		ModelOptions:   []string{"qwen3.8-max", "qwen3.7-max", "qwen3.7-plus", "qwen3.7-flash", "qwen3.6-max-preview"},
		IconKey:        "qwen",
		// token-plan key 只在 token-plan 域名有效，通用 dashscope 域名 401 invalid_api_key，
		// 所以拉列表也必须走 token-plan maas 域名（实测 200，12 个模型）。
		ModelsEndpoint: "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1/models",
		ModelsNeedsKey: true,
		AnthropicBase:  "https://token-plan.cn-beijing.maas.aliyuncs.com/apps/anthropic",
	},
	{
		VendorKey: "minimax", DisplayName: "MiniMax", Plan: PlanKindTokenPlan,
		ClientProvider: "OpenAI",
		APIBase:        "https://api.minimaxi.com/v1",
		DefaultModel:   "MiniMax-M2",
		ModelOptions:   []string{"MiniMax-M3", "MiniMax-M2.7", "MiniMax-M2.5", "MiniMax-M2", "MiniMax-M1"},
		IconKey:        "minimax",
		ModelsEndpoint: "https://api.minimaxi.com/v1/models",
		ModelsNeedsKey: true,
		AnthropicBase:  "https://api.minimaxi.com/anthropic",
	},
	{
		VendorKey: "maas", DisplayName: "Maas盘古", Plan: PlanKindTokenPlan,
		ClientProvider: "ModelArts",
		APIBase:        "https://api.modelarts-maas.com/plan/v2",
		EndpointProfile: "modelarts",
		DefaultModel:   "glm-5.1",
		ModelOptions:   []string{"glm-5.1", "kimi-k2.6", "deepseek-v4-flash"},
		IconKey:        "pangu",
		ModelsEndpoint: "https://api.modelarts-maas.com/v2/models",
		ModelsNeedsKey: true,
		AnthropicBase:  "https://api.modelarts-maas.com/plan/anthropic",
	},
	{
		VendorKey: "baidu", DisplayName: "百度智能云", Plan: PlanKindTokenPlan,
		ClientProvider: "OpenAI",
		APIBase:        "https://qianfan.baidubce.com/v2/tokenplan/personal", // personal；team 变体 /v2/tokenplan/team
		DefaultModel:   "ernie-5.1",
		ModelOptions:   []string{"ernie-5.1", "deepseek-v4-pro", "deepseek-v4-flash", "glm-5.2", "kimi-k2.6", "mimo-v2.5"},
		IconKey:        "baidu",
		// token plan key 走 tokenplan 端点；/v2/models 是通用端点。/v2/tokenplan/models 404，
		// personal 变体 401（端点存在，对齐 api_base）。
		ModelsEndpoint: "https://qianfan.baidubce.com/v2/tokenplan/personal/models",
		ModelsNeedsKey: true, // 实测无 key 返回 401；带 Bearer 才 200
		AnthropicBase:  "https://qianfan.baidubce.com/anthropic/tokenplan/personal",
	},
	{
		VendorKey: "mimo", DisplayName: "小米Mimo", Plan: PlanKindTokenPlan,
		ClientProvider: "MiMo",
		APIBase:        "https://token-plan-cn.xiaomimimo.com/v1", // 套餐调用走套餐域名（需套餐 key）
		EndpointProfile: "mimo",
		DefaultModel:   "mimo-v2.5-pro",
		ModelOptions:   []string{"mimo-v2.5-pro", "mimo-v2.5"},
		IconKey:        "mimo",
		// 拉列表也必须走套餐域名：通用 api.xiaomimimo.com 对套餐 key 返回 401 Invalid API Key，
		// 套餐域名 token-plan-cn.xiaomimimo.com/v1/models 才 200。
		ModelsEndpoint: "https://token-plan-cn.xiaomimimo.com/v1/models",
		ModelsNeedsKey: true,
		AnthropicBase:  "https://token-plan-cn.xiaomimimo.com/anthropic",
	},

	// ── Coding Plan ─────────────────────────────────────────────────────────
	{
		VendorKey: "alibaba", DisplayName: "阿里云百炼", Plan: PlanKindCodingPlan,
		ClientProvider: "OpenAI",
		APIBase:        "https://coding.dashscope.aliyuncs.com/v1", // xlsx 写 aliyun.com 是笔误，实测 aliyuncs.com 才存在（401）
		EndpointProfile: "dashscope",
		DefaultModel:   "qwen3-coder-next",
		ModelOptions:   []string{"qwen3-coder-next", "qwen3.7-max", "qwen3.6-max-preview"},
		IconKey:        "qwen",
		// coding key 只在 coding 域名有效，通用 dashscope 域名 401。coding 域名 /v1/models 实测 200（公开）。
		ModelsEndpoint: "https://coding.dashscope.aliyuncs.com/v1/models",
		ModelsNeedsKey: true,
		AnthropicBase:  "https://coding.dashscope.aliyuncs.com/apps/anthropic",
	},
	{
		VendorKey: "kimi", DisplayName: "Kimi(月之暗面)", Plan: PlanKindCodingPlan,
		ClientProvider: "OpenAI",
		APIBase:        "https://api.kimi.com/coding/v1",
		DefaultModel:   "kimi-k2.7-code",
		ModelOptions:   []string{"kimi-k3", "kimi-k2.7-code", "kimi-k2.6", "kimi-k2-thinking"},
		IconKey:        "kimi",
		// coding key 只在 api.kimi.com 有效，moonshot.cn 是通用域。coding 域 /coding/v1/models 实测 401（端点存在，非 404）。
		ModelsEndpoint: "https://api.kimi.com/coding/v1/models",
		ModelsNeedsKey: true,
		AnthropicBase:  "https://api.kimi.com/coding",
	},
	{
		VendorKey: "zhipu", DisplayName: "智谱GLM", Plan: PlanKindCodingPlan,
		ClientProvider: "OpenAI",
		APIBase:        "https://open.bigmodel.cn/api/coding/paas/v4",
		DefaultModel:   "glm-4.7",
		ModelOptions:   []string{"glm-5.2", "glm-5.1", "glm-5", "glm-4.7", "glm-4.7-flash", "codegeex-4"},
		IconKey:        "zhipu",
		// coding key 走 coding 路径；/api/paas/v4/models 是通用端点。/api/coding/paas/v4/models 实测 401（端点存在）。
		ModelsEndpoint: "https://open.bigmodel.cn/api/coding/paas/v4/models",
		ModelsNeedsKey: true,
		// 智谱当前会将普通 API key 的 Anthropic 请求错误路由到
		// Coding Plan，并在无有效套餐时返回 1309；暂不向前端开放。
		AnthropicBase: "",
	},
	{
		VendorKey: "volcengine", DisplayName: "火山引擎", Plan: PlanKindCodingPlan,
		ClientProvider: "OpenAI",
		APIBase:        ArkCodingAPIBase,
		DefaultModel:   "seed-2.0-mini",
		ModelOptions:   []string{"seed-2.0-mini", "seed-2.0-lite", "seed-1.6", "seed-1.6-flash"},
		IconKey:        "doubao",
		// coding key 走 coding 端点；/api/v3/models 是通用端点。/api/coding/v3/models 实测 401（端点存在）。
		ModelsEndpoint: ArkCodingAPIBase + "/models",
		ModelsNeedsKey: true,
		AnthropicBase:  ArkCodingAnthropicBase,
	},
	{
		VendorKey: "baidu", DisplayName: "百度智能云", Plan: PlanKindCodingPlan,
		ClientProvider: "OpenAI",
		APIBase:        "https://qianfan.baidubce.com/v2/coding",
		DefaultModel:   "deepseek-v4-pro",
		ModelOptions:   []string{"deepseek-v4-pro", "deepseek-v4-flash", "glm-5.1", "kimi-k2.5"},
		IconKey:        "baidu",
		// coding key 走 /v2/coding 端点；/v2/models 是通用端点。/v2/coding/models 实测 401（端点存在）。
		ModelsEndpoint: "https://qianfan.baidubce.com/v2/coding/models",
		ModelsNeedsKey: true, // 实测无 key 返回 401；带 Bearer 才 200
		AnthropicBase:  "https://qianfan.baidubce.com/anthropic/coding",
	},

	// ── 自定义 API（通用 Token / 按量计费）────────────────────────────────────
	{
		VendorKey: "alibaba", DisplayName: "阿里云百炼", Plan: PlanKindCustomAPI,
		ClientProvider: "OpenAI",
		APIBase:        "https://dashscope.aliyuncs.com/compatible-mode/v1",
		EndpointProfile: "dashscope",
		DefaultModel:   "qwen3.8-max",
		ModelOptions: []string{
			// 通义旗舰/通用
			"qwen3.8-max", "qwen3.8-max-0902", "qwen3.8-flash", "qwen3.7-plus",
			// 代码
			"qwen3-coder-next", "qwen3-coder-plus", "qwen3-coder-flash", "kimi-k2.7-code",
			// 第三方模型（百炼直供裸 ID，探活报告中全部实测 200）
			"MiniMax-M2.1", "MiniMax-M2.5",
			"deepseek-r1", "deepseek-r1-distill-llama-8b", "deepseek-r1-distill-qwen-1.5b",
			"deepseek-r1-distill-qwen-7b", "deepseek-r1-distill-qwen-14b", "deepseek-r1-distill-qwen-32b",
			"deepseek-v3", "deepseek-v3.1", "deepseek-v3.2",
			"deepseek-v4-pro", "deepseek-v4-pro-0813", "deepseek-v4-flash", "deepseek-v4-flash-0731",
			"kimi-k2-thinking", "kimi-k2.5", "kimi-k2.6", "kimi-k3",
			"glm-4.7", "glm-5", "glm-5.1", "glm-5.2", "glm-5.2-fast-preview",
			// 视觉/OCR
			"qwen3-vl-plus", "qwen3-vl-flash", "qwen-vl-ocr-latest", "qwen3.5-ocr",
			// 全模态
			"qwen3.5-omni-plus", "qwen3.5-omni-flash",
			// 通义开源
			"qwen3.8-2.4t-a95b", "qwen3.8-27b", "qwen3-next-80b-a3b-instruct", "qwen3-next-80b-a3b-thinking",
			// 数学/翻译/长文本
			"qwen-math-plus-latest", "qwen-mt-plus", "qwen-long",
			// 研究/搜索
			"qwen-deep-research-2025-12-15", "qwen-deep-search-planning",
		},
		IconKey:        "qwen",
		ModelsEndpoint: "https://dashscope.aliyuncs.com/compatible-mode/v1/models",
		ModelsNeedsKey: true,
		AnthropicBase:  "https://dashscope.aliyuncs.com/apps/anthropic",
	},
	{
		VendorKey: "deepseek", DisplayName: "DeepSeek", Plan: PlanKindCustomAPI,
		ClientProvider: "OpenAI",
		APIBase:        "https://api.deepseek.com",
		EndpointProfile: "deepseek",
		DefaultModel:   "deepseek-v4-pro",
		ModelOptions:   []string{"deepseek-v4-pro", "deepseek-v4-flash", "deepseek-v4-flash-latest"},
		IconKey:        "deepseek",
		ModelsEndpoint: "https://api.deepseek.com/models",
		ModelsNeedsKey: true,
		AnthropicBase:  "https://api.deepseek.com/anthropic",
	},
	{
		VendorKey: "kimi", DisplayName: "Kimi(月之暗面)", Plan: PlanKindCustomAPI,
		ClientProvider: "OpenAI",
		APIBase:        "https://api.moonshot.cn/v1",
		DefaultModel:   "kimi-k3",
		ModelOptions:   []string{"kimi-k3", "kimi-k2.7-code", "kimi-k2.6", "kimi-k2-thinking"},
		IconKey:        "kimi",
		ModelsEndpoint: "https://api.moonshot.cn/v1/models",
		ModelsNeedsKey: true,
		AnthropicBase:  "https://api.moonshot.cn/anthropic",
	},
	{
		VendorKey: "openrouter", DisplayName: "OpenRouter", Plan: PlanKindCustomAPI,
		ClientProvider: "OpenAI",
		APIBase:        "https://openrouter.ai/api/v1",
		EndpointProfile: "openrouter", // 归因头由 core 按 profile 注入
		DefaultModel:   "anthropic/claude-sonnet-4.5",
		ModelOptions: []string{
			"anthropic/claude-sonnet-4.5", "qwen/qwen3.8-max", "z-ai/glm-5.2",
			"deepseek/deepseek-v4-pro", "openai/gpt-5.6-terra", "google/gemini-2.5-flash",
		},
		IconKey:        "openai",
		ModelsEndpoint: "https://openrouter.ai/api/v1/models",
		ModelsNeedsKey: false, // 公开列出（实测 200，338 模型）
		// xlsx 标 Anthropic 格式与 OpenAI 同 url（实测 /v1/messages 也 401 存在）
		AnthropicBase: "https://openrouter.ai/api/v1",
	},
	{
		VendorKey: "zhipu", DisplayName: "智谱GLM", Plan: PlanKindCustomAPI,
		ClientProvider: "OpenAI",
		APIBase:        "https://open.bigmodel.cn/api/paas/v4",
		DefaultModel:   "glm-5.2",
		ModelOptions:   []string{"glm-5.2", "glm-5.1", "glm-5", "glm-4.7", "glm-4.7-flash", "glm-5-turbo", "glm-5v-turbo", "codegeex-4"},
		IconKey:        "zhipu",
		ModelsEndpoint: "https://open.bigmodel.cn/api/paas/v4/models",
		ModelsNeedsKey: true,
		AnthropicBase:  "",
	},
	{
		VendorKey: "minimax", DisplayName: "MiniMax", Plan: PlanKindCustomAPI,
		ClientProvider: "OpenAI",
		APIBase:        "https://api.minimaxi.com/v1",
		DefaultModel:   "MiniMax-M3",
		ModelOptions:   []string{"MiniMax-M3", "MiniMax-M2.7", "MiniMax-M2.5", "MiniMax-M2", "MiniMax-M2-HER", "MiniMax-M1"},
		IconKey:        "minimax",
		ModelsEndpoint: "https://api.minimaxi.com/v1/models",
		ModelsNeedsKey: true,
		AnthropicBase:  "https://api.minimaxi.com/anthropic",
	},
	{
		VendorKey: "maas", DisplayName: "Maas盘古", Plan: PlanKindCustomAPI,
		ClientProvider: "ModelArts",
		APIBase:        "https://api.modelarts-maas.com/v2",
		EndpointProfile: "modelarts",
		DefaultModel:   "openpangu-2.0-pro",
		ModelOptions:   []string{"openpangu-2.0-pro", "openpangu-2.0-flash", "glm-5.2", "glm-5.1", "kimi-k2.6", "deepseek-v4-pro", "deepseek-v4-flash"},
		IconKey:        "pangu",
		ModelsEndpoint: "https://api.modelarts-maas.com/v2/models",
		ModelsNeedsKey: true,
		AnthropicBase:  "https://api.modelarts-maas.com/anthropic",
	},
	{
		VendorKey: "volcengine", DisplayName: "火山引擎", Plan: PlanKindCustomAPI,
		ClientProvider: "OpenAI",
		APIBase:        ArkAPIBase,
		DefaultModel:   "seed-2.0-mini",
		ModelOptions:   []string{"seed-2.0-mini", "seed-2.0-lite", "seed-1.6", "seed-1.6-flash", "doubao-1.5-pro-32k"},
		IconKey:        "doubao",
		ModelsEndpoint: ArkAPIBase + "/models",
		ModelsNeedsKey: true,
		AnthropicBase:  ArkAnthropicBase,
	},
	{
		VendorKey: "baidu", DisplayName: "百度智能云", Plan: PlanKindCustomAPI,
		ClientProvider: "OpenAI",
		APIBase:        "https://qianfan.baidubce.com/v2",
		DefaultModel:   "ernie-5.1",
		ModelOptions:   []string{"ernie-5.1", "ernie-5.0", "deepseek-v4-pro", "deepseek-v4-flash", "glm-5.2", "kimi-k2.6", "mimo-v2.5", "qianfan-ocr-fast"},
		IconKey:        "baidu",
		ModelsEndpoint: "https://qianfan.baidubce.com/v2/models",
		ModelsNeedsKey: true, // 实测无 key 返回 403；带 Bearer 才 200
		AnthropicBase:  "https://qianfan.baidubce.com/anthropic",
	},
	{
		VendorKey: "mimo", DisplayName: "小米Mimo", Plan: PlanKindCustomAPI,
		ClientProvider: "MiMo",
		APIBase:        "https://api.xiaomimimo.com/v1",
		EndpointProfile: "mimo",
		DefaultModel:   "mimo-v2.5-pro",
		ModelOptions:   []string{"mimo-v2.5-pro", "mimo-v2.5"},
		IconKey:        "mimo",
		ModelsEndpoint: "https://api.xiaomimimo.com/v1/models",
		ModelsNeedsKey: true,
		AnthropicBase:  "https://api.xiaomimimo.com/anthropic",
	},
	// 自部署 vLLM-Omni 推理服务：没有固定地址，api_base 只是默认提示，由用户在设置里改。
	{
		VendorKey: "vllm-omni", DisplayName: "vLLM-Omni", Plan: PlanKindCustomAPI,
		ClientProvider: "OpenAI",
		APIBase:        "http://127.0.0.1:8091/v1",
		DefaultModel:   "",
		ModelOptions:   []string{},
		IconKey:        "vllm-omni",
		ModelsNeedsKey: false,
		GenerationOnly: true,
	},
}

// byPlan 按 plan 分组索引（Python: :524-526 _BY_PLAN 预建索引）。
var byPlan = func() map[PlanKind][]*VendorPreset {
	out := map[PlanKind][]*VendorPreset{
		PlanKindTokenPlan: {}, PlanKindCodingPlan: {}, PlanKindCustomAPI: {},
	}
	for _, p := range presets {
		out[p.Plan] = append(out[p.Plan], p)
	}
	return out
}()

// byVendorPlan (vendor, plan) → 预设索引。
var byVendorPlan = func() map[string]*VendorPreset {
	out := map[string]*VendorPreset{}
	for _, p := range presets {
		out[p.VendorKey + "|" + string(p.Plan)] = p
	}
	return out
}()

// byVendor 每厂商首个预设（图标查询用）。
var byVendor = func() map[string]*VendorPreset {
	out := map[string]*VendorPreset{}
	for _, p := range presets {
		if _, ok := out[p.VendorKey]; !ok {
			out[p.VendorKey] = p
		}
	}
	return out
}()

// ──────────────────────────── 导出函数 ────────────────────────────

// GetPresetsByPlan 给定套餐桶下的全部厂商条目（UI tab 渲染用）。
// Python: model_vendor_registry.py:529 get_presets_by_plan。
func GetPresetsByPlan(plan PlanKind) []*VendorPreset {
	src := byPlan[plan]
	out := make([]*VendorPreset, len(src))
	copy(out, src)
	return out
}

// GetPreset 特定 (vendor, plan) 预设；该套餐下无此厂商返回 nil。
// Python: model_vendor_registry.py:534 get_preset。
func GetPreset(vendorKey string, plan PlanKind) *VendorPreset {
	return byVendorPlan[vendorKey+"|"+string(plan)]
}

// GetPresetByVendorKey 厂商首个预设（图标展示 / 部分查询用）。
// Python: model_vendor_registry.py:539 get_preset_by_vendor_key。
func GetPresetByVendorKey(vendorKey string) *VendorPreset {
	return byVendor[vendorKey]
}

// GetAllPresets 全部预设平铺列表。
// Python: model_vendor_registry.py:542 get_all_presets。
func GetAllPresets() []*VendorPreset {
	out := make([]*VendorPreset, len(presets))
	copy(out, presets)
	return out
}

// reasoningCapabilities 逐模型查询思考能力。
// Python: model_vendor_registry.py:545 _reasoning_capabilities。
// 逐模型隔离：能力查询是静态表 lookup，正常不会抛，但 vendors.list 是
// 前端配置页入口 RPC，不能因单个模型异常导致整个厂商列表挂掉。失败时
// 跳过该模型，前端回落 reasoning_rules / model_fallbacks。
// Python 的 to_dict 由 Go 侧 capabilityMap 等价承载（options/recommended 两键）。
func reasoningCapabilities(preset *VendorPreset) map[string]map[string]map[string]any {
	capabilities := map[string]map[string]map[string]any{}
	for _, model := range preset.ModelOptions {
		protocols := map[string]map[string]any{}
		cap := reasoning.GetReasoningCapability(preset.ClientProvider, model, "openai", preset.APIBase, preset.EndpointProfile)
		protocols["openai"] = capabilityMap(cap)
		if preset.AnthropicBase != "" {
			capAnthropic := reasoning.GetReasoningCapability(AnthropicClientProvider, model, "anthropic", preset.AnthropicBase, "")
			protocols["anthropic"] = capabilityMap(capAnthropic)
		}
		capabilities[model] = protocols
	}
	return capabilities
}

// capabilityMap ReasoningCapability → Python to_dict 等价 map。
func capabilityMap(c reasoning.ReasoningCapability) map[string]any {
	options := c.Options
	if options == nil {
		options = []string{}
	}
	var recommended any
	if c.Recommended != nil {
		recommended = *c.Recommended
	}
	return map[string]any{"options": options, "recommended": recommended}
}

// ToFrontendPayload vendors.list RPC 的分组载荷（plan → 厂商卡片列表）。
// Python: model_vendor_registry.py:570 to_frontend_payload。
// 键名逐字照抄（前端契约）。
func ToFrontendPayload() map[string]any {
	out := map[string]any{
		"reasoning": reasoning.GetReasoningCapabilityCatalog(),
	}
	for _, plan := range []PlanKind{PlanKindTokenPlan, PlanKindCodingPlan, PlanKindCustomAPI} {
		cards := []map[string]any{}
		for _, p := range byPlan[plan] {
			card := map[string]any{
				"vendor_key":    p.VendorKey,
				"display_name":  p.DisplayName,
				"plan":          string(p.Plan),
				// OpenAI 格式（默认）：client_provider + api_base + endpoint_profile
				"client_provider":       p.ClientProvider,
				"api_base":              p.APIBase,
				"endpoint_profile":      p.EndpointProfile,
				"default_model":         p.DefaultModel,
				"model_options":         p.ModelOptions,
				"icon_key":              p.IconKey,
				"models_endpoint":       p.ModelsEndpoint,
				"models_needs_key":      p.ModelsNeedsKey,
				"generation_only":       p.GenerationOnly,
				"reasoning_capabilities": reasoningCapabilities(p),
				// Provider-scoped pattern 规则：给 models_endpoint 拉取到的、
				// 不在 model_options 精确表里的新模型用，避免前端退到跨厂商
				// model_fallbacks 时与后端 provider 级校验产生漂移。
				"reasoning_rules": reasoning.GetProviderReasoningRules(p.ClientProvider, p.APIBase, p.EndpointProfile),
				// Anthropic 格式（可选切换）：仅当 anthropic_base 非空时可用，
				// 切换后落库 client_provider=AnthropicClientProvider、
				// api_base=anthropic_base（core 用 AnthropicModelClient 走 /v1/messages）。
				"supports_anthropic":        p.AnthropicBase != "",
				"anthropic_base":            p.AnthropicBase,
				"anthropic_client_provider": nil,
			}
			if p.AnthropicBase != "" {
				card["anthropic_client_provider"] = AnthropicClientProvider
			}
			cards = append(cards, card)
		}
		out[string(plan)] = cards
	}
	return out
}
