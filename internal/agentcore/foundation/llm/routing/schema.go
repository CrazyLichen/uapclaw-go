// Package routing 将上层解析后的模型选择编译为 LLM 运行时配置，并把
// intelli_router 失败翻译为框架错误（失败四分类）。
//
// 对应 Python 代码：openjiuwen/core/foundation/llm/routing/
// （schema.py 87 行 / compiler.py 491 行 / intelli_router_errors.py 562 行）。
//
// 文件目录：
//
//	routing/
//	├── doc.go          # 包文档
//	├── schema.go       # 8 个 DTO 与 RoutingStrategyName 枚举
//	├── compiler.go     # CompileModelSelection / GetModelGroupModels 编译器
//	├── providers.go    # provider 归一映射与 _router_provider
//	├── errors.go       # ToModelError 失败四分类与候选描述
//	└── failures.go    # RouteFailure 记录/分类/解析
package routing

import (
	"encoding/json"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ModelSelection 持久化或用户提供的模型选择引用。
// Python: routing/schema.py:13-16 ModelSelection
type ModelSelection struct {
	// Type 选择类型，"model" 或 "model_group"
	// Python: schema.py:16 type: Literal["model", "model_group"]
	Type string `json:"type"`
	// ID 模型或模型组 ID
	// Python: schema.py:17 id
	ID string `json:"id"`
}

// ResolvedModel 上层解析出的一个可调用模型。
// Python: routing/schema.py:20-33 ResolvedModel
type ResolvedModel struct {
	// ModelID 业务模型 ID
	// Python: schema.py:23 model_id
	ModelID string `json:"model_id"`
	// ModelName provider 面向的模型名
	// Python: schema.py:24 model_name
	ModelName string `json:"model_name"`
	// Provider 服务商名
	// Python: schema.py:25 provider
	Provider string `json:"provider"`
	// APIKey API 密钥，缺省空串
	// Python: schema.py:26 api_key: str = ""
	APIKey string `json:"api_key"`
	// APIBase API 基础 URL，缺省空串
	// Python: schema.py:27 api_base: str = ""
	APIBase string `json:"api_base"`
	// Source 解析来源，缺省 "defaults"
	// Python: schema.py:28 source: str = "defaults"
	Source string `json:"source"`
	// EndpointProfile 端点配置档名（可选）
	// Python: schema.py:29 endpoint_profile: Optional[str]
	EndpointProfile *string `json:"endpoint_profile"`
	// FallbackTag tag-filtered 路由使用的正式模型名（可选）
	// Python: schema.py:30 fallback_tag: Optional[str]
	FallbackTag *string `json:"fallback_tag"`
	// ModelDescription 用户提供的模型能力描述（可选）
	// Python: schema.py:31 model_description: Optional[str]
	ModelDescription *string `json:"model_description"`
	// ClientOptions 客户端附加选项
	// Python: schema.py:32 client_options: dict[str, Any]
	ClientOptions map[string]any `json:"client_options"`
	// RequestDefaults 请求默认参数
	// Python: schema.py:33 request_defaults: dict[str, Any]
	RequestDefaults map[string]any `json:"request_defaults"`
}

// ResolvedRoute 模型组内一条候选路由。
// Python: routing/schema.py:36-45 ResolvedRoute
type ResolvedRoute struct {
	// RouteID 路由 ID
	// Python: schema.py:39 route_id
	RouteID string `json:"route_id"`
	// Model 路由指向的模型
	// Python: schema.py:40 model: ResolvedModel
	Model *ResolvedModel `json:"model"`
	// Enabled 是否启用，缺省 true
	// Python: schema.py:41 enabled: bool = True
	Enabled bool `json:"enabled"`
	// RequestOverrides 请求参数覆盖
	// Python: schema.py:42 request_overrides: dict[str, Any]
	RequestOverrides map[string]any `json:"request_overrides"`
	// TPM 每分钟 token 容量提示（可选）
	// Python: schema.py:43 tpm: Optional[int]
	TPM *int `json:"tpm"`
	// RPM 每分钟请求容量提示（可选）
	// Python: schema.py:44 rpm: Optional[int]
	RPM *int `json:"rpm"`
	// Timeout 路由级超时（可选）
	// Python: schema.py:45 timeout: Optional[float]
	Timeout *float64 `json:"timeout"`
}

// RoutingStrategyName 模型组支持的路由策略。
// Python: routing/schema.py:48-53 RoutingStrategyName（str Enum）
type RoutingStrategyName string

const (
	// RoutingStrategyOrderedFailover 顺序失败切换
	// Python: schema.py:51 OrderedFailover = "ordered-failover"
	RoutingStrategyOrderedFailover RoutingStrategyName = "ordered-failover"
	// RoutingStrategyTagFiltered 按 tag 过滤
	// Python: schema.py:52 TagFiltered = "tag-filtered"
	RoutingStrategyTagFiltered RoutingStrategyName = "tag-filtered"
)

// ResolvedRoutingConfig 模型组选定的路由策略。
// Python: routing/schema.py:55-66 ResolvedRoutingConfig（extra="allow"，
// Go 用 Extra map 保留多余键）
type ResolvedRoutingConfig struct {
	// Strategy 策略名（可选；nil 即 no-routing 短路）
	// Python: schema.py:58 strategy: Optional[RoutingStrategyName]
	Strategy *RoutingStrategyName `json:"strategy"`
	// NumRetries 重试预算（可选）
	// Python: schema.py:59 num_retries: Optional[int]
	NumRetries *int `json:"num_retries"`
	// StrategyKwargs 策略参数
	// Python: schema.py:60 strategy_kwargs: dict[str, Any]
	StrategyKwargs map[string]any `json:"strategy_kwargs"`
	// TimeoutSeconds 路由器超时（可选）
	// Python: schema.py:61 timeout_seconds: Optional[float]
	TimeoutSeconds *float64 `json:"timeout_seconds"`
	// EnableHealthCheck 是否启用健康检查（可选）
	// Python: schema.py:62 enable_health_check: Optional[bool]
	EnableHealthCheck *bool `json:"enable_health_check"`
	// HealthCheckIntervalSeconds 健康检查间隔（可选）
	// Python: schema.py:63 health_check_interval_seconds: Optional[float]
	HealthCheckIntervalSeconds *float64 `json:"health_check_interval_seconds"`
	// EnableObservability 是否启用可观测（可选）
	// Python: schema.py:64 enable_observability: Optional[bool]
	EnableObservability *bool `json:"enable_observability"`
	// Extra extra="allow" 的多余键（Python: model_config extra allow）
	Extra map[string]any `json:"-"`
}

// ResolvedModelGroup 待核心侧编译的已解析模型组。
// Python: routing/schema.py:69-75 ResolvedModelGroup
type ResolvedModelGroup struct {
	// ModelGroupID 模型组 ID
	// Python: schema.py:72 model_group_id
	ModelGroupID string `json:"model_group_id"`
	// Routes 候选路由列表
	// Python: schema.py:73 routes: list[ResolvedRoute]
	Routes []*ResolvedRoute `json:"routes"`
	// Routing 路由策略配置
	// Python: schema.py:74 routing: ResolvedRoutingConfig
	Routing *ResolvedRoutingConfig `json:"routing"`
	// RequestConfig 组级请求配置
	// Python: schema.py:75 request_config: dict[str, Any]
	RequestConfig map[string]any `json:"request_config"`
}

// CompiledModelSelection 编译产物：Model 需要的两份配置。
// Python: routing/schema.py:78-84 CompiledModelSelection
type CompiledModelSelection struct {
	// ModelClientConfig 模型客户端配置
	// Python: schema.py:81 model_client_config
	ModelClientConfig *schema.ModelClientConfig `json:"model_client_config"`
	// ModelRequestConfig 模型请求配置
	// Python: schema.py:82 model_request_config
	ModelRequestConfig *schema.ModelRequestConfig `json:"model_request_config"`
	// SelectedType 选择类型，"model" 或 "model_group"
	// Python: schema.py:83 selected_type
	SelectedType string `json:"selected_type"`
	// SelectedID 选中的模型/模型组 ID
	// Python: schema.py:84 selected_id
	SelectedID string `json:"selected_id"`
}

// ModelGroupModelCandidate 从一条启用路由解析出的可调用模型候选。
// Python: routing/compiler.py:77-88 ModelGroupModelCandidate（frozen dataclass）。
// Python 的 model 字段持有 Model 实例；Go 侧此处持有 ModelRequestConfig，
// 由调用方以 ClientConfig+RequestConfig 组合构造 Model（Python 侧无对应拆分，
// 属 Model 门面构造位置差异，语义等价）。
type ModelGroupModelCandidate struct {
	// Model 请求配置（Python: model: Model）
	Model *schema.ModelRequestConfig
	// ClientConfig 客户端配置（Python: Model 内嵌的 model_client_config）
	ClientConfig *schema.ModelClientConfig
	// RouteID 路由 ID（Python: route_id）
	RouteID string
	// ModelID 业务模型 ID（Python: model_id）
	ModelID string
	// ModelName 模型名（Python: model_name）
	ModelName string
	// ClientProvider 服务商名（Python: client_provider）
	ClientProvider string
	// ProviderKey provider 键（Python: provider_key）
	ProviderKey string
	// FallbackTag tag-filtered 正式模型名（可选，Python: fallback_tag）
	FallbackTag *string
	// ModelDescription 模型描述（可选，Python: model_description）
	ModelDescription *string
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// UnmarshalJSON 反序列化 ResolvedRoute，enabled 缺省 true
// （对应 Python schema.py:41 enabled: bool = True 的 pydantic 缺省语义）。
func (r *ResolvedRoute) UnmarshalJSON(data []byte) error {
	type Alias ResolvedRoute
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if _, exists := raw["enabled"]; !exists {
		raw["enabled"] = true
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	var alias Alias
	if err := json.Unmarshal(b, &alias); err != nil {
		return err
	}
	*r = ResolvedRoute(alias)
	if r.RequestOverrides == nil {
		r.RequestOverrides = map[string]any{}
	}
	return nil
}

// UnmarshalJSON 反序列化 ResolvedRoutingConfig，多余键保留进 Extra
// （对应 Python model_config = {"extra": "allow"}）。
func (c *ResolvedRoutingConfig) UnmarshalJSON(data []byte) error {
	type Alias ResolvedRoutingConfig
	var aux struct {
		Alias
		Extra map[string]any `json:"-"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*c = ResolvedRoutingConfig(aux.Alias)
	if c.StrategyKwargs == nil {
		c.StrategyKwargs = map[string]any{}
	}
	// 收集未知键到 Extra（Python 侧无对应显式步骤，属 extra=allow 语义）
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	known := map[string]struct{}{
		"strategy": {}, "num_retries": {}, "strategy_kwargs": {},
		"timeout_seconds": {}, "enable_health_check": {},
		"health_check_interval_seconds": {}, "enable_observability": {},
	}
	for k, v := range raw {
		if _, ok := known[k]; !ok {
			if c.Extra == nil {
				c.Extra = map[string]any{}
			}
			c.Extra[k] = v
		}
	}
	return nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewResolvedModel 创建对齐 Python 字段缺省值的 ResolvedModel。
// Python: schema.py:20-33（Field default）
func NewResolvedModel() *ResolvedModel {
	return &ResolvedModel{
		Source:          "defaults",
		ClientOptions:   map[string]any{},
		RequestDefaults: map[string]any{},
	}
}

// NewResolvedRoute 创建对齐 Python 字段缺省值的 ResolvedRoute
// （enabled=True、request_overrides 空字典）。
// Python: schema.py:36-45（Field default）
func NewResolvedRoute() *ResolvedRoute {
	return &ResolvedRoute{
		Enabled:          true,
		RequestOverrides: map[string]any{},
	}
}

// NewResolvedModelGroup 创建对齐 Python 字段缺省值的 ResolvedModelGroup
// （routing 缺省空配置、request_config 空字典）。
// Python: schema.py:69-75（Field default）
func NewResolvedModelGroup() *ResolvedModelGroup {
	return &ResolvedModelGroup{
		Routing:       &ResolvedRoutingConfig{StrategyKwargs: map[string]any{}},
		RequestConfig: map[string]any{},
	}
}
