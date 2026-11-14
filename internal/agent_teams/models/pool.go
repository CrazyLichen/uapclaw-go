package models

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/routing"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	utils "github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ModelPoolEntry 模型池中的单个 LLM 端点条目。
// 描述一个可用的模型端点及其凭证和 Provider 信息。
// ModelAllocator 从池中提取条目并转换为 TeamModelConfig，
// 使每个团队成员可以与不同的端点通信，避免单端点速率限制争用。
// 两个标识符各有不同角色：
//   - model_id（自动 UUID）：运行时客户端身份，透传到 ModelClientConfig.client_id，
//     供 foundation 层资源管理器去重/缓存底层 HTTP 客户端。不持久化到 DB。
//   - (model_name, group_index)：语义持久身份，存储在 DB 中作为成员的池引用。
//
// Python: ModelPoolEntry (openjiuwen/agent_teams/models/pool.py)
type ModelPoolEntry struct {
	// ModelName 模型名称
	ModelName string `json:"model_name"`
	// APIKey API 密钥
	APIKey string `json:"api_key"`
	// APIBaseURL API 基础 URL
	APIBaseURL string `json:"api_base_url"`
	// APIProvider 服务商标识
	APIProvider string `json:"api_provider"`
	// Description 描述（可选）
	Description string `json:"description,omitempty"`
	// ModelID 进程本地客户端身份，自动生成 UUID，不持久化
	ModelID string `json:"model_id"`
	// Metadata 扩展载荷，合并到物化的 TeamModelConfig
	//
	// 两个保留子键供 ToTeamModelConfig 使用：
	//   - client: 合并到 ModelClientConfig（如 timeout、verify_ssl、ssl_cert、max_retries、custom_headers）
	//   - request: 合并到 ModelRequestConfig（如 temperature、top_p、max_tokens、stop）
	// 池条目的显式字段（api_key、api_base_url、api_provider、model_name）
	// 始终覆盖 metadata 中同名键。
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ModelRouterConfig 单端点路由器配置，多个模型名共享一组凭证。
// 当路由式后端（OpenRouter、LiteLLM proxy 等）通过一个 URL 和一个 API Key
// 服务多个模型名时使用。在 TeamAgentSpec.build() 时展平为 ModelPoolEntry 列表。
// Python: ModelRouterConfig (openjiuwen/agent_teams/models/pool.py)
type ModelRouterConfig struct {
	// APIBaseURL API 基础 URL
	APIBaseURL string `json:"api_base_url"`
	// APIKey API 密钥
	APIKey string `json:"api_key"`
	// APIProvider 服务商标识
	APIProvider string `json:"api_provider"`
	// ModelNames 路由端点服务的模型名列表，首个为默认，必须非空且唯一
	ModelNames []string `json:"model_names"`
	// Metadata 扩展载荷，深拷贝到每个展开条目
	Metadata map[string]any `json:"metadata,omitempty"`
}

// IntelliRouterConfig 整个团队的多部署可靠路由配置（team 侧 DTO）。
// ModelRouterConfig 描述「单端点服务多模型名」，这里描述「单客户端路由器背后
// 的多端点」：IntelliRouterModelClient 包装 intelli_router.ReliableRouter，
// 由后者负责重试、换台、健康检查与限流感知负载均衡。
// 两者处于不同层且相互组合。团队级分配仍决定成员请求哪个模型名；
// 路由器决定哪个物理部署服务每个请求以及失败时的行为。所以本配置展开的
// 池永远不会像 round_robin 那样把成员摊到端点上——每个成员共享同一份
// deployment 列表，可用性是路由器的职责，不是分配器的。
// 在 TeamAgentSpec.build() 时 to_pool_entries 把它展开为携带完整 deployment
// 列表的单条逻辑 "*" entry（metadata.client.intelli_router），并把
// model_pool_strategy 置 "intelli_router"——下游所有路径（resolve_member_model、
// inherit_pool_ids、update_model_pool）继续按扁平池视图工作，无 IntelliRouter
// 特例分支。与 TeamAgentSpec.model_pool / model_router 互斥。
// 注意：这是 team 侧 DTO，与 foundation 编译侧 schema.IntelliRouterConfig
// （schema/config.py:90）是两份同构结构，不合并（总 spec 偏差 D-6）。
// Python: IntelliRouterConfig (pool.py:329)
type IntelliRouterConfig struct {
	// Deployments 路由器可路由的物理部署，永不空——无路由目标的组每次请求必败
	// Python: pool.py:360 deployments: list[IntelliRouterDeployment] = Field(min_length=1)
	Deployments []*IntelliRouterDeployment `json:"deployments"`
	// ModelNames 可选逻辑 Team 名；设置时必须恰为 ["*"]
	// Python: pool.py:362-364 model_names: list[str] | None = None
	ModelNames []string `json:"model_names,omitempty"`
	// Strategy 传给 ReliableRouter 的路由策略名
	// Python: pool.py:366-368 strategy: str = "simple-shuffle"
	Strategy string `json:"strategy"`
	// NumRetries fallback 重试预算
	// Python: pool.py:369-370 num_retries: int = 3
	NumRetries int `json:"num_retries"`
	// Timeout 路由器级请求超时（秒）
	// Python: pool.py:371-372 timeout: float = 30.0
	Timeout float64 `json:"timeout"`
	// StrategyKwargs 策略专属旋钮，原样转发给 ReliableRouter
	// Python: pool.py:373-375 strategy_kwargs: dict
	StrategyKwargs map[string]any `json:"strategy_kwargs,omitempty"`
	// EnableHealthCheck 是否启用路由器健康检查
	// Python: pool.py:376-377 enable_health_check: bool = False
	EnableHealthCheck bool `json:"enable_health_check"`
	// HealthCheckInterval 健康检查间隔（秒）
	// Python: pool.py:378-379 health_check_interval: float = 300.0
	HealthCheckInterval float64 `json:"health_check_interval"`
	// EnableObservability 是否启用路由器可观测钩子
	// Python: pool.py:380-381 enable_observability: bool = False
	EnableObservability bool `json:"enable_observability"`
	// WebDashboardPort 指标 web 面板端口
	// Python: pool.py:382-383 web_dashboard_port: int = 0
	WebDashboardPort int `json:"web_dashboard_port"`
	// VerifySSL 路由器级 TLS 校验；部署级 verify_ssl 显式设置时优先
	// Python: pool.py:384-386 verify_ssl: bool = True
	VerifySSL bool `json:"verify_ssl"`
	// Metadata 可选 ModelPoolEntry.metadata 载荷，拷入每个展开条目。
	// 保留的 client/request 子键见 ModelPoolEntry.metadata 文档。生成的
	// 结构化 client.intelli_router 快照与路由器级 verify_ssl 优先于这里
	// 的同名值；遗留扁平 intelli_router_* metadata 被丢弃而非与规范快照并写。
	// Python: pool.py:385-389（文档串）
	Metadata map[string]any `json:"metadata,omitempty"`
}

// NewIntelliRouterConfig 创建 IntelliRouterConfig 并对齐 Python 字段缺省值。
// Python: pool.py:359-389（Field default）
func NewIntelliRouterConfig(deployments []*IntelliRouterDeployment) *IntelliRouterConfig {
	return &IntelliRouterConfig{
		Deployments:         deployments,
		Strategy:            "simple-shuffle",
		NumRetries:          3,
		Timeout:             30.0,
		StrategyKwargs:      map[string]any{},
		HealthCheckInterval: 300.0,
		WebDashboardPort:    0,
		VerifySSL:           true,
		Metadata:            map[string]any{},
	}
}

// Validate 校验 model_names 只能暴露唯一逻辑 Team 名 "*"。
// Python: _validate_model_names (pool.py:387)
func (c *IntelliRouterConfig) Validate() error {
	if c.ModelNames == nil {
		return nil
	}
	if len(c.ModelNames) != 1 || c.ModelNames[0] != INTELLI_ROUTER_UNIFIED_MODEL {
		return fmt.Errorf("IntelliRouterConfig exposes exactly one logical Team model name: '*'")
	}
	return nil
}

// ResolvedModelNames 返回唯一的逻辑 Team 模型名。
// Python: resolved_model_names (pool.py:395)
func (c *IntelliRouterConfig) ResolvedModelNames() []string {
	return []string{INTELLI_ROUTER_UNIFIED_MODEL}
}

// clientExtra 构建规范的结构化 IntelliRouter client 快照。
// 每个 deployment 渲染 wire dict 并补 route_id（缺省 route_{index}）；
// strategy_kwargs 深拷贝，防后续共享 map 变更污染快照。
// Python: _client_extra (pool.py:400)
func (c *IntelliRouterConfig) clientExtra() map[string]any {
	deployments := make([]any, 0, len(c.Deployments))
	for index, deployment := range c.Deployments {
		payload := deployment.ToDeploymentDict()
		routeID := payload["id"]
		delete(payload, "id")
		if routeID == nil || routeID == "" {
			routeID = fmt.Sprintf("route_%d", index)
		}
		if tags, ok := payload["tags"].([]string); ok && len(tags) > 0 {
			payload["fallback_tag"] = tags[0]
		}
		entry := map[string]any{"route_id": routeID}
		for k, v := range payload {
			entry[k] = v
		}
		deployments = append(deployments, entry)
	}
	router := map[string]any{
		"deployments":           deployments,
		"strategy":              c.Strategy,
		"num_retries":           c.NumRetries,
		"timeout":               c.Timeout,
		"strategy_kwargs":       deepCopyAny(c.StrategyKwargs),
		"enable_health_check":   c.EnableHealthCheck,
		"health_check_interval": c.HealthCheckInterval,
		"enable_observability":  c.EnableObservability,
		"web_dashboard_port":    c.WebDashboardPort,
	}
	return map[string]any{
		"intelli_router": router,
		"verify_ssl":     c.VerifySSL,
	}
}

// ToPoolEntries 展开为单条逻辑 "*" Team 模型条目。
// 条目携带完整 deployment 列表。物理部署名留在结构化 client 快照内，
// 永不暴露给 Team 分配：每个成员拿到同一条逻辑 entry，因此完整 deployment
// 列表与路由器旋钮产出同一个共享 ReliableRouter 缓存键——换台状态、健康检查、
// 各部署 tpm/rpm 预算因此对全团队全局；物理部署名绝不成为 Team 分配器条目。
// api_provider 固定 "intelli_router"，这是物化到 IntelliRouterModelClient 的
// 路由依据。条目自身 api_key/api_base_url 为空：凭证按部署存放，foundation
// 层对该 provider 不要求顶层凭证。
// Python: to_pool_entries (pool.py:438)
func (c *IntelliRouterConfig) ToPoolEntries() []ModelPoolEntry {
	metadata := deepCopyAny(c.Metadata).(map[string]any)
	clientMetadata := map[string]any{}
	if raw, ok := metadata["client"]; ok {
		if m, ok := raw.(map[string]any); ok {
			clientMetadata = m
		}
	}
	// 遗留扁平 intelli_router_* 键被丢弃，不与规范快照并写（pool.py:444-448）
	for key := range clientMetadata {
		if strings.HasPrefix(key, "intelli_router_") {
			delete(clientMetadata, key)
		}
	}
	for k, v := range c.clientExtra() {
		clientMetadata[k] = v
	}
	metadata["client"] = clientMetadata
	return []ModelPoolEntry{{
		ModelName:   INTELLI_ROUTER_UNIFIED_MODEL,
		APIKey:      "",
		APIBaseURL:  "",
		APIProvider: INTELLI_ROUTER_PROVIDER,
		Description: fmt.Sprintf("IntelliRouter unified routing across %d deployment(s)", len(c.Deployments)),
		ModelID:     uuid.New().String(),
		Metadata:    metadata,
	}}
}

// MaterializeModelGroup 把编译器产物物化为规范的 Team 模型组池。
// 编译器驱动的模型组被刻意表示为恰好一条逻辑 "*" 条目。物理部署留在结构化
// metadata.client.intelli_router 快照内，供 Foundation 路由器使用而不会被
// 误当成 Team 分配器端点。
// compiled 鸭子类型是为跨包复用；Go 直接取 23.4b 的 CompiledModelSelection
// 具体类型（无循环依赖），调用方通常还会传对应的 ModelSelection 做一致性校验。
// Python: materialize_model_group (pool.py:460)
func MaterializeModelGroup(compiled *routing.CompiledModelSelection, selection *routing.ModelSelection) ([]ModelPoolEntry, error) {
	if compiled == nil {
		return nil, fmt.Errorf("compiled model selection is required")
	}
	selectedType := compiled.SelectedType
	selectedID := compiled.SelectedID
	if selection != nil {
		if selection.Type != selectedType || selection.ID != selectedID {
			return nil, fmt.Errorf("model selection does not match compiled selection")
		}
	}
	if selectedType != "model_group" {
		return nil, fmt.Errorf("materialize_model_group requires selected_type='model_group'")
	}
	client := compiled.ModelClientConfig
	clientProvider := ""
	if client != nil {
		clientProvider = client.ClientProvider
	}
	if client == nil || clientProvider != INTELLI_ROUTER_PROVIDER {
		return nil, fmt.Errorf("model group must use client_provider='intelli_router'")
	}
	router := client.IntelliRouter
	if router == nil || len(router.Deployments) == 0 {
		return nil, fmt.Errorf("model group IntelliRouter configuration must contain deployments")
	}

	// 路由器快照：JSON 序列化后剔除 null（对齐 model_dump(exclude_none=True)）
	routerDump := dropNulls(mustMarshalMap(router))
	// 请求快照：model_dump(exclude_none=True, exclude_unset=True, by_alias=True)
	requestDump := map[string]any{}
	if request := compiled.ModelRequestConfig; request != nil {
		requestDump = dropNulls(mustMarshalMap(request))
	}
	// model 是逻辑 Team 字段；模型组的请求模型必须保持 unset，
	// 否则会覆盖 deployment 级默认值（pool.py:505-509 原文注释）。
	delete(requestDump, "model")
	delete(requestDump, "model_name")
	clientMetadata := map[string]any{"intelli_router": routerDump}
	// Go 的 ModelClientConfig.VerifySSL 是非指针 bool（schema 侧语义恒有值），
	// 不做 Python 的 nil 判断，恒写入快照
	clientMetadata["verify_ssl"] = client.VerifySSL
	return []ModelPoolEntry{{
		ModelName:   INTELLI_ROUTER_UNIFIED_MODEL,
		APIKey:      "",
		APIBaseURL:  "",
		APIProvider: INTELLI_ROUTER_PROVIDER,
		Description: fmt.Sprintf("IntelliRouter model group %s", selectedID),
		ModelID:     uuid.New().String(),
		Metadata: map[string]any{
			"client":  clientMetadata,
			"request": requestDump,
		},
	}}, nil
}

// ModelGroupLabel 命名模型组池背后的模型组；非组池返回空串。
// 在池的逻辑 "*" 模型名会泄露给阅读者的场合用它。优先宿主提供的
// metadata.model_group.display_name，其次路由器的 model_group_id。
// Python: model_group_label (pool.py:519)（None → 空串）
func ModelGroupLabel(pool []ModelPoolEntry) string {
	if len(pool) != 1 {
		return ""
	}
	entry := pool[0]
	if entry.ModelName != INTELLI_ROUTER_UNIFIED_MODEL || entry.APIProvider != INTELLI_ROUTER_PROVIDER {
		return ""
	}
	client, _ := entry.Metadata["client"].(map[string]any)
	router, _ := client["intelli_router"].(map[string]any)
	if router == nil {
		return ""
	}
	group, _ := entry.Metadata["model_group"].(map[string]any)
	displayName := ""
	if group != nil {
		displayName, _ = group["display_name"].(string)
	}
	if displayName != "" {
		return displayName
	}
	groupID, _ := router["model_group_id"].(string)
	if groupID != "" {
		return groupID
	}
	return INTELLI_ROUTER_UNIFIED_MODEL
}

// ModelPoolEntryOption ModelPoolEntry 构造选项函数。
type ModelPoolEntryOption func(*ModelPoolEntry)

// IntelliRouterDeployment IntelliRouter 背后的单个物理部署。
// 镜像 intelli_router.Deployment：单端点服务单模型，自带凭证与限流预算。
// 与 ModelPoolEntry（分配器逐成员下发）不同：deployments 永不单独分配，
// 整份列表交给每个成员的客户端，由路由器逐请求挑选、失败换台重试。
// Python: IntelliRouterDeployment (pool.py:241)
type IntelliRouterDeployment struct {
	// ModelName 部署服务的模型名
	ModelName string `json:"model_name"`
	// APIKey 部署 API Key
	APIKey string `json:"api_key"`
	// APIBase Provider **根** URL —— 不要带 /v1 后缀！
	// 这是唯一与本仓其余 api_base 约定不同的字段：ModelClientConfig.api_base
	// （进而 ModelPoolEntry.api_base_url）指向 OpenAI 兼容 *API* base，通常以
	// /v1 结尾；IntelliRouter 则由各 provider 适配器自行拼路径——OpenAI 适配器
	// 拼 f"{api_base}/v1/chat/completions"。传了 /v1 结尾的 base 会拼出
	// /v1/v1/chat/completions 且每个请求 404。但故障形态不像 404：路由器报
	// ResponseNotRead（错误路径未先消费流就读 response body）。
	// 正确示例：https://api.deepseek.com，而非 https://api.deepseek.com/v1。
	// Python: pool.py:262-276（原文教训，注释保留）
	APIBase string `json:"api_base"`
	// ID 稳定部署标识，出现在路由器统计与日志中（可选）
	// Python: pool.py:277-279 id: Optional[str]
	ID string `json:"id,omitempty"`
	// ModelID 编译快照中的业务模型标识（可选）
	// Python: pool.py:280-282 model_id: Optional[str]
	ModelID string `json:"model_id,omitempty"`
	// Provider intelli_router 解释的上游 provider 名（必填）
	// Python: pool.py:283-285 provider: str
	Provider string `json:"provider"`
	// EndpointProfile provider 端点方言（可选）
	EndpointProfile string `json:"endpoint_profile,omitempty"`
	// CustomHeaders 自定义请求头（可选）
	CustomHeaders map[string]string `json:"custom_headers,omitempty"`
	// FallbackTag 模型组成员点名（tag-filtered）用的标签（可选）
	FallbackTag string `json:"fallback_tag,omitempty"`
	// ModelDescription 用户提供的模型能力描述（可选）
	ModelDescription string `json:"model_description,omitempty"`
	// RequestDefaults 模型层编译出的部署级请求默认值
	RequestDefaults map[string]any `json:"request_defaults,omitempty"`
	// TPM 每分钟 token 预算（限流感知策略用，可选）
	TPM *int `json:"tpm,omitempty"`
	// RPM 每分钟请求数预算（可选）
	RPM *int `json:"rpm,omitempty"`
	// Tags 部署标签列表（ToDeploymentDict 时 tags[0] 兜底 FallbackTag）
	Tags []string `json:"tags,omitempty"`
	// Timeout 部署级超时；未设回退路由器级（可选）
	Timeout *float64 `json:"timeout,omitempty"`
	// VerifySSL 部署级 TLS 校验；未设回退路由器级——仅键缺失时客户端层才回退（可选）
	VerifySSL *bool `json:"verify_ssl,omitempty"`
}

// ToDeploymentDict 渲染 IntelliRouterModelClient 消费的 wire dict。
// 可选字段省略而非发 null，让客户端层应用自己的回退——尤其 verify_ssl，
// 只有键缺失时才回退路由器级值。
// Python: to_deployment_dict (pool.py:297)
func (d *IntelliRouterDeployment) ToDeploymentDict() map[string]any {
	payload := map[string]any{
		"model_name": d.ModelName,
		"api_key":    d.APIKey,
		"api_base":   d.APIBase,
		"provider":   d.Provider,
	}
	if len(d.Tags) > 0 {
		payload["fallback_tag"] = d.Tags[0]
	}
	optional := map[string]any{}
	if d.ID != "" {
		optional["id"] = d.ID
	}
	if d.ModelID != "" {
		optional["model_id"] = d.ModelID
	}
	if d.EndpointProfile != "" {
		optional["endpoint_profile"] = d.EndpointProfile
	}
	if len(d.CustomHeaders) > 0 {
		optional["custom_headers"] = d.CustomHeaders
	}
	if d.FallbackTag != "" {
		optional["fallback_tag"] = d.FallbackTag
	}
	if d.ModelDescription != "" {
		optional["model_description"] = d.ModelDescription
	}
	if d.RequestDefaults != nil {
		optional["request_defaults"] = d.RequestDefaults
	}
	if d.TPM != nil {
		optional["tpm"] = *d.TPM
	}
	if d.RPM != nil {
		optional["rpm"] = *d.RPM
	}
	if d.Timeout != nil {
		optional["timeout"] = *d.Timeout
	}
	if d.VerifySSL != nil {
		optional["verify_ssl"] = *d.VerifySSL
	}
	for k, v := range optional {
		payload[k] = v
	}
	return payload
}

// ──────────────────────────── 常量 ────────────────────────────

// INTELLI_ROUTER_PROVIDER 模型池条目的 api_provider 取值，标记该条目由 IntelliRouter 承载。
// 与 foundation 层 ProviderTypeIntelliRouter（23.3 落地）匹配——create_model_client
// 据此路由到 IntelliRouterModelClient。
// Python: INTELLI_ROUTER_PROVIDER (pool.py:221)
const INTELLI_ROUTER_PROVIDER = "intelli_router"

// INTELLI_ROUTER_UNIFIED_MODEL IntelliRouter 组暴露给 Team 的唯一逻辑模型名。
// IntelliRouterModelClient 把该通配符转发给底层 ReliableRouter；
// 分配器 Allocate 无 hint 返回该条目（IntelliRouterAllocator.pool[0]）。
// Python: INTELLI_ROUTER_UNIFIED_MODEL (pool.py:228)
const INTELLI_ROUTER_UNIFIED_MODEL = "*"

// ──────────────────────────── 导出函数 ────────────────────────────

// NewModelPoolEntry 创建 ModelPoolEntry 实例。
// ModelID 自动生成 UUID。可通过选项函数设置 Description、Metadata 等可选字段。
// Python: ModelPoolEntry(model_name=..., api_key=..., api_base_url=..., api_provider=...)
func NewModelPoolEntry(modelName, apiKey, apiBaseURL, apiProvider string, opts ...ModelPoolEntryOption) *ModelPoolEntry {
	entry := &ModelPoolEntry{
		ModelName:   modelName,
		APIKey:      apiKey,
		APIBaseURL:  apiBaseURL,
		APIProvider: apiProvider,
		ModelID:     uuid.New().String(),
	}
	for _, opt := range opts {
		opt(entry)
	}
	return entry
}

// WithDescription 设置描述。
func WithDescription(desc string) ModelPoolEntryOption {
	return func(e *ModelPoolEntry) { e.Description = desc }
}

// WithModelID 设置模型 ID（通常仅用于测试或继承场景）。
func WithModelID(id string) ModelPoolEntryOption {
	return func(e *ModelPoolEntry) { e.ModelID = id }
}

// WithMetadata 设置元数据。
func WithMetadata(metadata map[string]any) ModelPoolEntryOption {
	return func(e *ModelPoolEntry) { e.Metadata = metadata }
}

// ToTeamModelConfig 从池条目物化 TeamModelConfig。
// 保留的 metadata.client 和 metadata.request 子字典分别合并到
// ModelClientConfig 和 ModelRequestConfig。池条目的显式字段
// 始终覆盖 metadata 中同名键。
// Python: ModelPoolEntry.to_team_model_config()
func (e *ModelPoolEntry) ToTeamModelConfig() TeamModelConfig {
	// 提取 metadata 子字典
	var clientExtra map[string]any
	var requestExtra map[string]any
	if e.Metadata != nil {
		if v, ok := e.Metadata["client"]; ok {
			if m, ok := v.(map[string]any); ok {
				clientExtra = m
			}
		}
		if v, ok := e.Metadata["request"]; ok {
			if m, ok := v.(map[string]any); ok {
				requestExtra = m
			}
		}
	}
	if clientExtra == nil {
		clientExtra = make(map[string]any)
	}
	if requestExtra == nil {
		requestExtra = make(map[string]any)
	}

	// 构建 ModelClientConfig：metadata.client 作为基础，池条目显式字段覆盖
	clientCfg := buildModelClientConfig(clientExtra, e.ModelID, e.APIProvider, e.APIKey, e.APIBaseURL)

	// 构建 ModelRequestConfig：metadata.request 作为基础，model_name 覆盖
	requestCfg := buildModelRequestConfig(requestExtra, e.ModelName)

	return TeamModelConfig{
		ModelClientConfig:  *clientCfg,
		ModelRequestConfig: requestCfg,
	}
}

// Validate 校验 ModelRouterConfig 的 model_names 约量：
//   - 必须包含非空字符串（拒绝空白或纯空格条目）
//   - 条目必须唯一（拒绝重复）
//
// Python: ModelRouterConfig._validate_model_names()
func (r *ModelRouterConfig) Validate() error {
	if len(r.ModelNames) == 0 {
		return fmt.Errorf("model_names 不能为空")
	}

	// 检查空白条目
	var blanks []int
	for i, name := range r.ModelNames {
		if name == "" || strings.TrimSpace(name) == "" {
			blanks = append(blanks, i)
		}
	}
	if len(blanks) > 0 {
		return fmt.Errorf("model_names 必须包含非空字符串；空白位于索引: %v", blanks)
	}

	// 检查重复
	seen := make(map[string]int, len(r.ModelNames))
	var duplicates []string
	for _, name := range r.ModelNames {
		if count, exists := seen[name]; exists {
			if count == 1 {
				duplicates = append(duplicates, name)
			}
			seen[name] = count + 1
		} else {
			seen[name] = 1
		}
	}
	if len(duplicates) > 0 {
		sort.Strings(duplicates)
		return fmt.Errorf("model_names 必须唯一；重复项: %v", duplicates)
	}

	return nil
}

// ToPoolEntries 将路由器展开为每个模型名一个 ModelPoolEntry。
// 每个展开条目共享 api_key/api_base_url/api_provider，
// metadata 深拷贝以避免调用者意外交叉污染。
// Python: ModelRouterConfig.to_pool_entries()
func (r *ModelRouterConfig) ToPoolEntries() []ModelPoolEntry {
	entries := make([]ModelPoolEntry, 0, len(r.ModelNames))
	for _, name := range r.ModelNames {
		entry := ModelPoolEntry{
			ModelName:   name,
			APIKey:      r.APIKey,
			APIBaseURL:  r.APIBaseURL,
			APIProvider: r.APIProvider,
			ModelID:     uuid.New().String(),
			Metadata:    utils.DeepCopyMap(r.Metadata),
		}
		entries = append(entries, entry)
	}
	return entries
}

// InheritPoolIDs 从 current_pool 中继承 model_id 到 new_pool 的 bit-exact 匹配条目。
// ModelPoolEntry.model_id 透传为 ModelClientConfig.client_id，
// foundation 客户端缓存可能使用它来去重 HTTP 客户端。仅在新旧条目的
// 完整配置（除 model_id 外的所有字段）完全相同时才安全继承——
// 否则使用旧 api_key 构建的缓存客户端可能静默服务本应使用新凭证的请求。
// 按位精确签名匹配：除 model_id 外的每个字段必须相同。
// 多个同签名条目按池顺序一对一配对。无匹配的新条目保留自己的 model_id。
// Python: inherit_pool_ids()
func InheritPoolIDs(currentPool, newPool []ModelPoolEntry) []ModelPoolEntry {
	// 构建旧池签名→条目桶
	oldBySig := make(map[string][]ModelPoolEntry)
	for _, entry := range currentPool {
		sig := entrySignature(entry)
		oldBySig[sig] = append(oldBySig[sig], entry)
	}

	result := make([]ModelPoolEntry, 0, len(newPool))
	for _, newEntry := range newPool {
		sig := entrySignature(newEntry)
		bucket := oldBySig[sig]
		if len(bucket) > 0 {
			// 从桶头部取出匹配条目，继承其 model_id
			inheritedID := bucket[0].ModelID
			oldBySig[sig] = bucket[1:]
			// 复制新条目并覆盖 model_id
			inherited := newEntry
			inherited.ModelID = inheritedID
			result = append(result, inherited)
		} else {
			result = append(result, newEntry)
		}
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// deepCopyAny map[string]any 的深拷贝（strategy_kwargs 与 metadata 快照用）。
// 复用 common/utils 既有实现（对齐 Python copy.deepcopy 语义）。
func deepCopyAny(v any) any {
	if m, ok := v.(map[string]any); ok {
		return utils.DeepCopyMap(m)
	}
	return v
}

// mustMarshalMap 序列化为 map[string]any；失败视为不可达（类型由本包构造保证）。
func mustMarshalMap(v any) map[string]any {
	raw, err := json.Marshal(v)
	if err != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

// dropNulls 递归剔除 null 值键，对齐 Python model_dump(exclude_none=True)。
func dropNulls(m map[string]any) map[string]any {
	for k, v := range m {
		if v == nil {
			delete(m, k)
			continue
		}
		switch t := v.(type) {
		case map[string]any:
			dropNulls(t)
		case []any:
			for _, item := range t {
				if im, ok := item.(map[string]any); ok {
					dropNulls(im)
				}
			}
		}
	}
	return m
}

// entrySignature 计算条目的规范签名（排除 model_id）。
// 两个具有相同签名的条目描述相同的逻辑端点、相同的认证和请求参数。
// 任何差异（包括 api_key 轮换）产生不同签名，强制生成新的 model_id。
// Python: _entry_signature()
func entrySignature(entry ModelPoolEntry) string {
	// 构建排除 model_id 的 map
	sig := map[string]any{
		"model_name":   entry.ModelName,
		"api_key":      entry.APIKey,
		"api_base_url": entry.APIBaseURL,
		"api_provider": entry.APIProvider,
	}
	if entry.Description != "" {
		sig["description"] = entry.Description
	}
	if len(entry.Metadata) > 0 {
		meta := utils.DeepCopyMap(entry.Metadata)
		// client 身份不是配置：model_id 与 metadata.client.client_id 都不计入签名。
		// 宿主常把整个 ModelClientConfig dump 进 metadata.client，其 client_id
		// 每次都是新 uuid；计入签名会让同一选型的两次构建看起来像模型变更，
		// 暂停的团队因此在每次请求时被拆建重盘（pool.py:556-559 原文教训）。
		// 【23.4c 修复】此前 Go 实现漏了 client_id 剔除。
		if client, ok := meta["client"].(map[string]any); ok {
			delete(client, "client_id")
		}
		sig["metadata"] = meta
	}

	data, err := json.Marshal(sig)
	if err != nil {
		// 降级：用 fmt 格式化
		return fmt.Sprintf("%+v", sig)
	}
	return string(data)
}

// buildModelClientConfig 从 metadata.client 子字典构建 ModelClientConfig，
// 池条目显式字段覆盖同名键。
func buildModelClientConfig(clientExtra map[string]any, modelID, apiProvider, apiKey, apiBaseURL string) *llmschema.ModelClientConfig {
	cfg, err := llmschema.NewModelClientConfig(apiProvider, apiKey, apiBaseURL)
	if err != nil {
		// 池条目的 provider/key/base 应始终有效；校验失败视为配置错误
		return nil
	}
	cfg.ClientID = modelID

	// 合并 metadata.client 中的字段
	if v, ok := clientExtra["timeout"]; ok {
		if f, ok := toFloat64(v); ok {
			cfg.Timeout = f
		}
	}
	if v, ok := clientExtra["max_retries"]; ok {
		if i, ok := toInt(v); ok {
			cfg.MaxRetries = i
		}
	}
	if v, ok := clientExtra["verify_ssl"]; ok {
		if b, ok := toBool(v); ok {
			cfg.VerifySSL = b
		}
	}
	if v, ok := clientExtra["ssl_cert"]; ok {
		if s, ok := v.(string); ok {
			cfg.SSLCert = s
		}
	}
	if v, ok := clientExtra["custom_headers"]; ok {
		if m, ok := v.(map[string]any); ok {
			headers := make(map[string]string, len(m))
			for k, val := range m {
				if s, ok := val.(string); ok {
					headers[k] = s
				}
			}
			cfg.CustomHeaders = headers
		}
	}
	// 额外字段存入 Extra
	extra := make(map[string]any)
	knownClientKeys := map[string]struct{}{
		"client_id": {}, "client_provider": {}, "api_key": {}, "api_base": {},
		"timeout": {}, "max_retries": {}, "verify_ssl": {}, "ssl_cert": {}, "custom_headers": {},
	}
	for k, v := range clientExtra {
		if _, known := knownClientKeys[k]; !known {
			extra[k] = v
		}
	}
	if len(extra) > 0 {
		cfg.Extra = extra
	}

	return cfg
}

// buildModelRequestConfig 从 metadata.request 子字典构建 ModelRequestConfig，
// model_name 覆盖同名键。
func buildModelRequestConfig(requestExtra map[string]any, modelName string) *llmschema.ModelRequestConfig {
	cfg := llmschema.NewModelRequestConfig()
	cfg.ModelName = modelName

	// 合并 metadata.request 中的字段
	if v, ok := requestExtra["temperature"]; ok {
		if f, ok := toFloat64(v); ok {
			cfg.Temperature = &f
		}
	}
	if v, ok := requestExtra["top_p"]; ok {
		if f, ok := toFloat64(v); ok {
			cfg.TopP = &f
		}
	}
	if v, ok := requestExtra["max_tokens"]; ok {
		if i, ok := toInt(v); ok {
			cfg.MaxTokens = &i
		}
	}
	if v, ok := requestExtra["stop"]; ok {
		if s, ok := v.(string); ok {
			cfg.Stop = &s
		}
	}
	// 额外字段存入 Extra
	extra := make(map[string]any)
	knownRequestKeys := map[string]struct{}{
		"model": {}, "temperature": {}, "top_p": {}, "max_tokens": {}, "stop": {},
	}
	for k, v := range requestExtra {
		if _, known := knownRequestKeys[k]; !known {
			extra[k] = v
		}
	}
	if len(extra) > 0 {
		cfg.Extra = extra
	}

	return cfg
}

// toFloat64 将 any 转换为 float64。
func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	default:
		return 0, false
	}
}

// toInt 将 any 转换为 int。
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case int32:
		return int(n), true
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	default:
		return 0, false
	}
}

// toBool 将 any 转换为 bool。
func toBool(v any) (bool, bool) {
	b, ok := v.(bool)
	return b, ok
}
