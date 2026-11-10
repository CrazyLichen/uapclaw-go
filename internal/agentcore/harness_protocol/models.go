package harness_protocol

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
)

// ──────────────────────────── 枚举 ────────────────────────────

// 类型别名放在结构体之前（规则 2 的特例说明：type X = Y 归枚举区块）。

// JsonValue 一条 JSON 兼容值。
// Python: models.py:24-26 JsonValue: TypeAlias = str | int | float | bool | None |
// list["JsonValue"] | tuple["JsonValue", ...] | Mapping[str, "JsonValue"]
// Go 无递归类型别名能力，用 any + FreezeJsonValue 运行期校验等价（spec §6 B3）。
type JsonValue = any

// JsonObject 一个 JSON 对象（键必须是字符串）。
// Python: models.py:27 JsonObject = Mapping[str, JsonValue]
type JsonObject = map[string]JsonValue

// ──────────────────────────── 结构体 ────────────────────────────

// HarnessTelemetry 宿主提供的最小 provider-neutral 遥测服务。
// Python: models.py:70-76 HarnessTelemetry（@runtime_checkable Protocol，1 方法）
type HarnessTelemetry interface {
	// Record 记录一条已脱敏的遥测观测
	Record(name string, attributes JsonObject)
}

// HarnessCard 一个 harness 实现的静态身份与能力元数据。
// Python: models.py:144-195 HarnessCard
type HarnessCard struct {
	// Name 实现名
	Name string
	// ImplementationVersion 实现版本
	ImplementationVersion string
	// ProtocolVersion 本实现遵循的协议版本
	ProtocolVersion string
	// CompatibleProtocolVersions 兼容的协议版本集合（至少含 ProtocolVersion）
	CompatibleProtocolVersions map[string]bool
	// Capabilities 声明的可选能力
	Capabilities map[HarnessCapability]bool
	// RequiredHostCapabilities 必需宿主能力
	RequiredHostCapabilities map[HostCapability]bool
	// OptionalHostCapabilities 可选宿主能力
	OptionalHostCapabilities map[HostCapability]bool
	// ForkModes provider 能原生物化的 session fork 模式（full / before）；空表示不支持
	ForkModes map[string]bool
}

// CardOptions HarnessCard 的可选字段。
// Python: models.py:150-155（dataclass 默认值）
type CardOptions struct {
	// ProtocolVersion 协议版本，空则取 ProtocolVersion 常量
	ProtocolVersion string
	// CompatibleProtocolVersions 兼容版本集，nil 则取 {ProtocolVersion}
	CompatibleProtocolVersions map[string]bool
	// Capabilities 声明的能力
	Capabilities map[HarnessCapability]bool
	// RequiredHostCapabilities 必需宿主能力
	RequiredHostCapabilities map[HostCapability]bool
	// OptionalHostCapabilities 可选宿主能力
	OptionalHostCapabilities map[HostCapability]bool
	// ForkModes 原生 fork 模式
	ForkModes map[string]bool
}

// HarnessInput 外部 harness 接受的一条输入。
// Python: models.py:198-207
type HarnessInput struct {
	// Content 输入内容（已冻结）
	Content JsonValue
	// Metadata 输入元数据（已冻结）
	Metadata JsonObject
}

// SendReceipt 确认 harness 已接受一条输入命令。
// Python: models.py:210-222
type SendReceipt struct {
	// MessageID 消息标识
	MessageID string
	// TurnID 该消息关联的轮次标识
	TurnID string
	// AcceptedMode 实际接受的投递模式
	AcceptedMode DeliveryMode
}

// ModelSelection 下一轮应使用的模型与推理强度；nil 表示保持当前值。
// Python: models.py:225-244
type ModelSelection struct {
	// Model 模型名（nil 保持）
	Model *string
	// Effort provider 词汇表的推理强度（nil 保持）
	Effort *string
}

// ModelEndpoint 宿主提供的模型端点（区别于 CLI 自身登录）。
// APIKey 是凭据，永不落日志。
// Python: models.py:254-273
type ModelEndpoint struct {
	// APIBase 端点地址
	APIBase *string
	// APIKey 凭据（永不落日志）
	APIKey *string
	// Provider 端点线协议标签
	Provider *string
}

// ModelChoice 宿主希望 harness 创建在哪个模型上及其来源。
// Endpoint 为 nil 表示使用 vendor runtime 自身登录的内置模型。
// Python: models.py:276-299
type ModelChoice struct {
	// Model 模型名（nil 取 runtime 默认）
	Model *string
	// Effort 推理强度（nil 取 runtime 默认）
	Effort *string
	// Endpoint 宿主提供的端点（nil 为内置模型）
	Endpoint *ModelEndpoint
}

// ModelOption provider 自身目录报告的一个可选模型。
// Python: models.py:302-327
type ModelOption struct {
	// ModelID 模型标识
	ModelID string
	// DisplayName 展示名
	DisplayName string
	// Description 描述
	Description string
	// Efforts 可调推理强度；空表示不可调
	Efforts []string
	// DefaultEffort 默认强度（必须属于 Efforts）
	DefaultEffort *string
	// IsDefault 是否为目录默认模型
	IsDefault bool
	// Extensions vendor 扩展数据（已冻结）
	Extensions JsonObject
}

// HarnessContext 第三方 harness 启动时注入的 per-agent 运行上下文。
// Env 可能含密钥，禁止落日志或复制进事件。
// Python: models.py:330-373
type HarnessContext struct {
	// AgentName agent 名
	AgentName string
	// AgentID agent 标识
	AgentID string
	// HostSessionID 宿主会话标识
	HostSessionID string
	// SystemPrompt 系统提示词
	SystemPrompt string
	// ProtocolVersion 协议版本
	ProtocolVersion string
	// HostCapabilities 宿主能力集
	HostCapabilities map[HostCapability]bool
	// ResumePolicy 恢复策略
	ResumePolicy ResumePolicy
	// Cwd 工作目录（nil 表示未指定）
	Cwd *string
	// Env 环境变量（可能含密钥，禁止落日志）
	Env map[string]string
	// Checkpoint 起始 checkpoint（不透明，仅所属实现可解释）
	Checkpoint *HarnessCheckpoint
	// CheckpointSink 主动发布 checkpoint 的持久化通道
	CheckpointSink HarnessCheckpointSink
	// Tools 原生工具网关
	Tools ToolGateway
	// McpServers 暴露给外部 harness 的 MCP 服务
	McpServers []McpServerConfig
	// Hooks 生命周期策略回调
	Hooks HarnessHookDispatcher
	// Interactions 需要应答的控制面
	Interactions HarnessInteractionHandler
	// Telemetry 遥测服务
	Telemetry HarnessTelemetry
	// Metadata 附加元数据（已冻结）
	Metadata JsonObject
}

// ContextOptions HarnessContext 的可选字段。
// Python: models.py:344-356（dataclass 默认值）
type ContextOptions struct {
	// ProtocolVersion 协议版本，空则取 ProtocolVersion 常量
	ProtocolVersion string
	// HostCapabilities 宿主能力集
	HostCapabilities map[HostCapability]bool
	// ResumePolicy 恢复策略，零值视为 ResumePolicyNew
	ResumePolicy ResumePolicy
	// Cwd 工作目录
	Cwd *string
	// Env 环境变量
	Env map[string]string
	// Checkpoint 起始 checkpoint
	Checkpoint *HarnessCheckpoint
	// CheckpointSink 持久化通道
	CheckpointSink HarnessCheckpointSink
	// Tools 工具网关
	Tools ToolGateway
	// McpServers MCP 服务
	McpServers []McpServerConfig
	// Hooks 钩子分发器
	Hooks HarnessHookDispatcher
	// Interactions 交互处理器
	Interactions HarnessInteractionHandler
	// Telemetry 遥测服务
	Telemetry HarnessTelemetry
	// Metadata 附加元数据
	Metadata JsonObject
}

// ──────────────────────────── 枚举 ────────────────────────────

// HarnessCapability 外部 harness 可声明的可选能力。
// Python: models.py:79-100 HarnessCapability（13 项）
type HarnessCapability string

const (
	// HarnessCapabilitySteer 可在轮内转向
	HarnessCapabilitySteer HarnessCapability = "steer"
	// HarnessCapabilityGracefulAbort 可优雅中止
	HarnessCapabilityGracefulAbort HarnessCapability = "graceful_abort"
	// HarnessCapabilityForceAbort 可强制中止
	HarnessCapabilityForceAbort HarnessCapability = "force_abort"
	// HarnessCapabilityPauseResume 可暂停恢复
	HarnessCapabilityPauseResume HarnessCapability = "pause_resume"
	// HarnessCapabilityPersistentSession 具备持久会话
	HarnessCapabilityPersistentSession HarnessCapability = "persistent_session"
	// HarnessCapabilityCheckpoint 支持 checkpoint
	HarnessCapabilityCheckpoint HarnessCapability = "checkpoint"
	// HarnessCapabilityNativeTools 支持原生工具
	HarnessCapabilityNativeTools HarnessCapability = "native_tools"
	// HarnessCapabilityMcpTools 支持 MCP 工具
	HarnessCapabilityMcpTools HarnessCapability = "mcp_tools"
	// HarnessCapabilityHooks 支持钩子
	HarnessCapabilityHooks HarnessCapability = "hooks"
	// HarnessCapabilityModelSelection 可在会话中切换模型/推理强度
	HarnessCapabilityModelSelection HarnessCapability = "model_selection"
	// HarnessCapabilityModelDiscovery 可探测模型目录
	HarnessCapabilityModelDiscovery HarnessCapability = "model_discovery"
	// HarnessCapabilityEndpointModels 支持宿主提供的端点模型
	HarnessCapabilityEndpointModels HarnessCapability = "endpoint_models"
	// HarnessCapabilityAuthFallback 鉴权失败可切到 fallback 模型
	HarnessCapabilityAuthFallback HarnessCapability = "auth_fallback"
)

// HostCapability harness 实现可用的细粒度宿主服务。
// Python: models.py:103-118 HostCapability（11 项）
type HostCapability string

const (
	// HostCapabilityCheckpointSink 宿主提供 checkpoint 持久化
	HostCapabilityCheckpointSink HostCapability = "checkpoint_sink"
	// HostCapabilityNativeToolGateway 宿主提供原生工具网关
	HostCapabilityNativeToolGateway HostCapability = "native_tool_gateway"
	// HostCapabilityMcpServers 宿主提供 MCP 服务
	HostCapabilityMcpServers HostCapability = "mcp_servers"
	// HostCapabilityHooks 宿主提供钩子
	HostCapabilityHooks HostCapability = "hooks"
	// HostCapabilityToolApproval 宿主提供工具审批
	HostCapabilityToolApproval HostCapability = "tool_approval"
	// HostCapabilityUserInput 宿主提供用户输入
	HostCapabilityUserInput HostCapability = "user_input"
	// HostCapabilityMcpElicitation 宿主提供 MCP elicitation
	HostCapabilityMcpElicitation HostCapability = "mcp_elicitation"
	// HostCapabilityDynamicToolCall 宿主提供动态工具调用
	HostCapabilityDynamicToolCall HostCapability = "dynamic_tool_call"
	// HostCapabilityProviderInteraction 宿主提供 provider 交互
	HostCapabilityProviderInteraction HostCapability = "provider_interaction"
	// HostCapabilityTelemetry 宿主提供遥测
	HostCapabilityTelemetry HostCapability = "telemetry"
	// HostCapabilityModelRequestObservation 宿主消费 ModelRequestEvent
	HostCapabilityModelRequestObservation HostCapability = "model_request_observation"
)

// DeliveryMode 输入相对于在飞轮次的投递方式。
// Python: models.py:121-127 DeliveryMode（3 项）
type DeliveryMode string

const (
	// DeliveryModeAuto 自动判定
	DeliveryModeAuto DeliveryMode = "auto"
	// DeliveryModeSteer 转向当前轮
	DeliveryModeSteer DeliveryMode = "steer"
	// DeliveryModeFollowUp 排队等本轮结束
	DeliveryModeFollowUp DeliveryMode = "follow_up"
)

// AbortMode 中止在飞轮次的方式。
// Python: models.py:129-134 AbortMode（2 项）
type AbortMode string

const (
	// AbortModeGraceful 优雅中止
	AbortModeGraceful AbortMode = "graceful"
	// AbortModeForce 强制中止
	AbortModeForce AbortMode = "force"
)

// ResumePolicy harness 如何使用 start 时提供的 checkpoint。
// Python: models.py:136-142 ResumePolicy（3 项）
type ResumePolicy string

const (
	// ResumePolicyNew 新会话，忽略 checkpoint
	ResumePolicyNew ResumePolicy = "new"
	// ResumePolicyResumeIfAvailable 有则恢复
	ResumePolicyResumeIfAvailable ResumePolicy = "resume_if_available"
	// ResumePolicyRequireResume 必须恢复
	ResumePolicyRequireResume ResumePolicy = "require_resume"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ProtocolVersion 当前协议契约版本
	// Python: models.py:22 PROTOCOL_VERSION
	ProtocolVersion = "1.0"
	// AuthFallbackRequestType 切 fallback 模型前发出的 provider 交互类型
	// Python: models.py:249 AUTH_FALLBACK_REQUEST_TYPE
	AuthFallbackRequestType = "auth_fallback"
	// AuthFallbackEventType 宣告 fallback 模型开始服务的事件类型
	// Python: models.py:251 AUTH_FALLBACK_EVENT_TYPE
	AuthFallbackEventType = "auth_fallback_activated"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// FreezeJsonValue 校验并递归冻结一个 JSON 兼容值。
// 返回值不再与入参共享可变容器，调用方修改入参不影响结果。
// Python: models.py:30-48 freeze_json_value（5 个分支顺序一致）
func FreezeJsonValue(value JsonValue) (JsonValue, error) {
	// 分支 1：None（Go 的 nil）/ string / bool / 各类整数 原样返回
	// Python: models.py:33-34
	if value == nil {
		return nil, nil
	}
	switch v := value.(type) {
	case string, bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return v, nil
	case float64:
		// 分支 2：浮点必须有限
		// Python: models.py:35-38
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("JSON numbers must be finite")
		}
		return v, nil
	case float32:
		f := float64(v)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("JSON numbers must be finite")
		}
		return v, nil
	}

	// 分支 3：切片/数组递归（list 与 tuple 归一到同一 Go 路径）
	// Python: models.py:39-40
	rv := reflect.ValueOf(value)
	if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
		out := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			item, err := FreezeJsonValue(rv.Index(i).Interface())
			if err != nil {
				return nil, err
			}
			out[i] = item
		}
		return out, nil
	}

	// 分支 4：映射递归，键必须是 string
	// Python: models.py:41-47
	if rv.Kind() == reflect.Map {
		if rv.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("JSON object keys must be strings")
		}
		out := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			key := iter.Key().String()
			item, err := FreezeJsonValue(iter.Value().Interface())
			if err != nil {
				return nil, err
			}
			out[key] = item
		}
		return out, nil
	}

	// 分支 5：其余类型拒绝
	// Python: models.py:48
	return nil, fmt.Errorf("value of type %T is not JSON-compatible", value)
}

// FreezeJsonObject 校验、复制并递归冻结一个 JSON 对象。
// Python: models.py:51-57 freeze_json_object
func FreezeJsonObject(value JsonValue) (JsonObject, error) {
	frozen, err := FreezeJsonValue(value)
	if err != nil {
		return nil, err
	}
	if frozen == nil {
		// Python: models.py:56（isinstance(frozen, Mapping) 为 False → TypeError）
		return nil, fmt.Errorf("JSON object must be a mapping")
	}
	obj, ok := frozen.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("JSON object must be a mapping")
	}
	return obj, nil
}

// JsonValueToBuiltin 把冻结后的协议 JSON 值转回标准 JSON 容器。
// Python: models.py:60-67 json_value_to_builtin
func JsonValueToBuiltin(value JsonValue) JsonValue {
	switch v := value.(type) {
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = JsonValueToBuiltin(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = JsonValueToBuiltin(item)
		}
		return out
	}
	return value
}

// NewHarnessCard 用默认可选字段创建 card。
// Python: models.py:144-176 HarnessCard.__post_init__
func NewHarnessCard(name, implementationVersion string) (HarnessCard, error) {
	return NewHarnessCardWithOptions(name, implementationVersion, CardOptions{})
}

// NewHarnessCardWithOptions 带可选字段创建 card，校验顺序与上游 __post_init__ 一致。
func NewHarnessCardWithOptions(name, implementationVersion string, opts CardOptions) (HarnessCard, error) {
	// 步骤 1：4 个集合归一化为非 nil map
	// Python: models.py:164-168
	protocolVersion := opts.ProtocolVersion
	if protocolVersion == "" {
		protocolVersion = ProtocolVersion
	}
	compatible := copyStringSet(opts.CompatibleProtocolVersions)
	if compatible == nil {
		// Python: models.py:151（default_factory 取 PROTOCOL_VERSION）
		compatible = map[string]bool{ProtocolVersion: true}
	}
	capabilities := copyCapabilitySet(opts.Capabilities)
	required := copyHostCapabilitySet(opts.RequiredHostCapabilities)
	optional := copyHostCapabilitySet(opts.OptionalHostCapabilities)
	forkModes := copyStringSet(opts.ForkModes)
	if forkModes == nil {
		forkModes = map[string]bool{}
	}

	// 步骤 2：三标识非空
	// Python: models.py:169-170
	if name == "" || implementationVersion == "" || protocolVersion == "" {
		return HarnessCard{}, &HarnessProtocolError{Msg: "harness card identity and versions must not be empty"}
	}

	// 步骤 3：protocol_version 必须属于兼容集
	// Python: models.py:171-172
	if !compatible[protocolVersion] {
		return HarnessCard{}, &HarnessProtocolError{Msg: "card protocol_version must be included in compatible_protocol_versions"}
	}

	// 步骤 4：必需与可选能力不得重叠
	// Python: models.py:173-176
	overlap := make([]string, 0)
	for cap := range required {
		if optional[cap] {
			overlap = append(overlap, string(cap))
		}
	}
	if len(overlap) > 0 {
		sort.Strings(overlap)
		return HarnessCard{}, &HarnessProtocolError{
			Msg: "host capabilities cannot be both required and optional: " + strings.Join(overlap, ", "),
		}
	}

	return HarnessCard{
		Name:                       name,
		ImplementationVersion:      implementationVersion,
		ProtocolVersion:            protocolVersion,
		CompatibleProtocolVersions: compatible,
		Capabilities:               capabilities,
		RequiredHostCapabilities:   required,
		OptionalHostCapabilities:   optional,
		ForkModes:                  forkModes,
	}, nil
}

// Supports 返回实现是否声明了该能力。
// Python: models.py:178-180 supports()
func (c HarnessCard) Supports(capability HarnessCapability) bool {
	return c.Capabilities[capability]
}

// ValidateHost 在宿主无法满足本 provider 要求时快速失败。
// Python: models.py:182-195 validate_host
func (c HarnessCard) ValidateHost(protocolVersion string, hostCapabilities map[HostCapability]bool) error {
	// 步骤 1：协议版本兼容
	if !c.CompatibleProtocolVersions[protocolVersion] {
		return &HarnessProtocolError{
			Msg: "host protocol " + pyRepr(protocolVersion) + " is not supported by " + pyRepr(c.Name),
		}
	}
	// 步骤 2：必需能力齐备
	missing := make([]string, 0)
	for cap := range c.RequiredHostCapabilities {
		if !hostCapabilities[cap] {
			missing = append(missing, string(cap))
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return &HarnessProtocolError{Msg: "host is missing required capabilities: " + strings.Join(missing, ", ")}
	}
	return nil
}

// NewHarnessInput 创建输入并冻结 content / metadata。
// Python: models.py:198-207
func NewHarnessInput(content JsonValue, metadata JsonObject) (HarnessInput, error) {
	// 步骤 1：冻结 content
	frozenContent, err := FreezeJsonValue(content)
	if err != nil {
		return HarnessInput{}, err
	}
	// 步骤 2：冻结 metadata
	frozenMetadata, err := FreezeJsonObject(metadata)
	if err != nil {
		return HarnessInput{}, err
	}
	return HarnessInput{Content: frozenContent, Metadata: frozenMetadata}, nil
}

// NewSendReceipt 创建投递回执。
// Python: models.py:210-222
func NewSendReceipt(messageID, turnID string, acceptedMode DeliveryMode) (SendReceipt, error) {
	// 步骤 1：message_id 非空
	if messageID == "" {
		return SendReceipt{}, &HarnessProtocolError{Msg: "send receipt message_id must not be empty"}
	}
	// 步骤 2：turn_id 非空
	if turnID == "" {
		return SendReceipt{}, &HarnessProtocolError{Msg: "send receipt turn_id must not be empty"}
	}
	return SendReceipt{MessageID: messageID, TurnID: turnID, AcceptedMode: acceptedMode}, nil
}

// NewModelSelection 创建模型选择，要求 model / effort 至少一个且非空。
// Python: models.py:225-244
func NewModelSelection(model, effort *string) (ModelSelection, error) {
	// 步骤 1：至少一个
	if model == nil && effort == nil {
		return ModelSelection{}, &HarnessProtocolError{Msg: "model selection must set a model, an effort, or both"}
	}
	// 步骤 2：给出时必须非空串
	if model != nil && strings.TrimSpace(*model) == "" {
		return ModelSelection{}, &HarnessProtocolError{Msg: "model selection model must be a non-empty string when provided"}
	}
	if effort != nil && strings.TrimSpace(*effort) == "" {
		return ModelSelection{}, &HarnessProtocolError{Msg: "model selection effort must be a non-empty string when provided"}
	}
	return ModelSelection{Model: model, Effort: effort}, nil
}

// NewModelEndpoint 创建端点，要求 api_base / provider 至少一个且非空。
// Python: models.py:254-273
func NewModelEndpoint(apiBase, apiKey, provider *string) (ModelEndpoint, error) {
	// 步骤 1：至少一个目标
	if apiBase == nil && provider == nil {
		return ModelEndpoint{}, &HarnessProtocolError{Msg: "model endpoint must set a api_base, a provider, or both"}
	}
	// 步骤 2：给出时必须非空串
	if apiBase != nil && strings.TrimSpace(*apiBase) == "" {
		return ModelEndpoint{}, &HarnessProtocolError{Msg: "model endpoint api_base must be a non-empty string when provided"}
	}
	if apiKey != nil && strings.TrimSpace(*apiKey) == "" {
		return ModelEndpoint{}, &HarnessProtocolError{Msg: "model endpoint api_key must be a non-empty string when provided"}
	}
	if provider != nil && strings.TrimSpace(*provider) == "" {
		return ModelEndpoint{}, &HarnessProtocolError{Msg: "model endpoint provider must be a non-empty string when provided"}
	}
	return ModelEndpoint{APIBase: apiBase, APIKey: apiKey, Provider: provider}, nil
}

// NewModelChoice 创建模型来源选择。
// Python: models.py:276-299
func NewModelChoice(model, effort *string, endpoint *ModelEndpoint) (ModelChoice, error) {
	// 步骤 1：model / effort 给出时必须非空
	if model != nil && strings.TrimSpace(*model) == "" {
		return ModelChoice{}, &HarnessProtocolError{Msg: "model choice model must be a non-empty string when provided"}
	}
	if effort != nil && strings.TrimSpace(*effort) == "" {
		return ModelChoice{}, &HarnessProtocolError{Msg: "model choice effort must be a non-empty string when provided"}
	}
	return ModelChoice{Model: model, Effort: effort, Endpoint: endpoint}, nil
}

// IsBuiltin 返回该模型是否跑在 vendor runtime 自身登录上。
// Python: models.py:296-299 is_builtin property
func (c ModelChoice) IsBuiltin() bool { return c.Endpoint == nil }

// NewModelOption 创建一个目录模型项。
// Python: models.py:302-327
func NewModelOption(
	modelID, displayName, description string,
	efforts []string, defaultEffort *string, isDefault bool, extensions JsonObject,
) (ModelOption, error) {
	// 步骤 1：model_id 非空
	if modelID == "" {
		return ModelOption{}, &HarnessProtocolError{Msg: "model option model_id must not be empty"}
	}
	// 步骤 2：efforts 归一为切片（nil → 空）
	normalizedEfforts := make([]string, len(efforts))
	copy(normalizedEfforts, efforts)
	// 步骤 3：efforts 元素必须非空
	for _, item := range normalizedEfforts {
		if item == "" {
			return ModelOption{}, &HarnessProtocolError{Msg: "model option efforts must be non-empty strings"}
		}
	}
	// 步骤 4：default_effort 必须属于 efforts
	if defaultEffort != nil && len(normalizedEfforts) > 0 {
		found := false
		for _, item := range normalizedEfforts {
			if item == *defaultEffort {
				found = true
				break
			}
		}
		if !found {
			return ModelOption{}, &HarnessProtocolError{Msg: "model option default_effort must be one of its efforts"}
		}
	}
	// 步骤 5：冻结 extensions
	frozenExtensions, err := FreezeJsonObject(extensions)
	if err != nil {
		return ModelOption{}, err
	}
	return ModelOption{
		ModelID:       modelID,
		DisplayName:   displayName,
		Description:   description,
		Efforts:       normalizedEfforts,
		DefaultEffort: defaultEffort,
		IsDefault:     isDefault,
		Extensions:    frozenExtensions,
	}, nil
}

// NewHarnessContext 创建运行上下文（env 走 ContextOptions）。
// Python: models.py:330-373
func NewHarnessContext(agentName, agentID, hostSessionID, systemPrompt string, opts ContextOptions) (HarnessContext, error) {
	// 步骤 0：协议版本先回落默认值（对应 dataclass 默认值，非 __post_init__ 步骤）
	// Python: models.py:344 protocol_version: str = PROTOCOL_VERSION
	protocolVersion := opts.ProtocolVersion
	if protocolVersion == "" {
		protocolVersion = ProtocolVersion
	}
	// 步骤 1：4 个必填字段用同一个循环判定（顺序与上游 required dict 一致）
	// Python: models.py:359-367
	required := []struct {
		name  string
		value string
	}{
		{"agent_name", agentName},
		{"agent_id", agentID},
		{"host_session_id", hostSessionID},
		{"protocol_version", protocolVersion},
	}
	for _, f := range required {
		if f.value == "" {
			return HarnessContext{}, &HarnessProtocolError{Msg: "harness context " + f.name + " must not be empty"}
		}
	}
	// 步骤 2：env 必须 string→string
	// Python: models.py:368-369
	env := make(map[string]string, len(opts.Env))
	for k, v := range opts.Env {
		env[k] = v
	}
	// 步骤 3：4 处归一化（host_capabilities / env / mcp_servers / metadata）
	// Python: models.py:370-373
	hostCapabilities := copyHostCapabilitySet(opts.HostCapabilities)
	mcpServers := make([]McpServerConfig, len(opts.McpServers))
	copy(mcpServers, opts.McpServers)
	frozenMetadata, err := FreezeJsonObject(opts.Metadata)
	if err != nil {
		return HarnessContext{}, err
	}
	resumePolicy := opts.ResumePolicy
	if resumePolicy == "" {
		resumePolicy = ResumePolicyNew
	}
	return HarnessContext{
		AgentName:        agentName,
		AgentID:          agentID,
		HostSessionID:    hostSessionID,
		SystemPrompt:     systemPrompt,
		ProtocolVersion:  protocolVersion,
		HostCapabilities: hostCapabilities,
		ResumePolicy:     resumePolicy,
		Cwd:              opts.Cwd,
		Env:              env,
		Checkpoint:       opts.Checkpoint,
		CheckpointSink:   opts.CheckpointSink,
		Tools:            opts.Tools,
		McpServers:       mcpServers,
		Hooks:            opts.Hooks,
		Interactions:     opts.Interactions,
		Telemetry:        opts.Telemetry,
		Metadata:         frozenMetadata,
	}, nil
}

// NewHarnessContextWithEnv 用 JsonObject 形态的 env 创建上下文，类型不符即报错。
// 仅服务于「与上游 env 类型校验等价」的测试与反序列化入口。
// Python: models.py:368-369（env 键值必须都是 str）
func NewHarnessContextWithEnv(agentName, agentID, hostSessionID, systemPrompt string, env JsonObject) (HarnessContext, error) {
	typed := make(map[string]string, len(env))
	for k, v := range env {
		s, ok := v.(string)
		if !ok {
			return HarnessContext{}, &HarnessProtocolError{Msg: "harness context env must map strings to strings"}
		}
		typed[k] = s
	}
	return NewHarnessContext(agentName, agentID, hostSessionID, systemPrompt, ContextOptions{Env: typed})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// copyStringSet 深拷贝 string 集合，nil 返回 nil。
func copyStringSet(src map[string]bool) map[string]bool {
	if src == nil {
		return nil
	}
	out := make(map[string]bool, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// copyCapabilitySet 深拷贝能力集合，nil 返回 non-nil 空集。
// Python: models.py:165（frozenset(self.capabilities) 恒为集合）
func copyCapabilitySet(src map[HarnessCapability]bool) map[HarnessCapability]bool {
	out := make(map[HarnessCapability]bool, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// copyHostCapabilitySet 深拷贝宿主能力集合，nil 返回 non-nil 空集。
func copyHostCapabilitySet(src map[HostCapability]bool) map[HostCapability]bool {
	out := make(map[HostCapability]bool, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// pyRepr 复刻 Python repr(str) 的单引号形态，用于错误文案逐字对齐。
// Python: models.py:191,195 的 f"{protocol_version!r}"
func pyRepr(s string) string { return "'" + s + "'" }
