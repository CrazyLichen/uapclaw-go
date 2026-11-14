package recommendation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ──────────────────────────── 常量 ────────────────────────────

// PROVIDER_REFUSALS 拒绝类失败集合：一旦发生，后续调用全部拒绝直到设置变更。
// 对齐 topic_model.py:165。
var PROVIDER_REFUSALS = map[string]bool{
	"invalid_key": true, "access_denied": true,
	"account_overdue": true, "model_unavailable": true,
}

// BackgroundConfigurationStatus 不暴露凭据地快照当前生效配置。
// 对齐 Python topic_model.py:118-162 background_configuration_status。
func BackgroundConfigurationStatus(config *ArkBackgroundConfig, environ map[string]string) map[string]any {
	get := func(name string) string { return strings.TrimSpace(environ[name]) }
	missing := []string{}
	for _, name := range []string{"ARK_API_KEY", "ARK_MODEL"} {
		if get(name) == "" {
			missing = append(missing, name)
		}
	}
	context := 0
	if raw := get("ARK_CONTEXT_WINDOW_TOKENS"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err != nil {
			context = 0
		} else {
			context = parsed
		}
	}
	if context <= 0 {
		missing = append(missing, "ARK_CONTEXT_WINDOW_TOKENS")
	}
	if config != nil {
		missing = []string{}
		if config.ContextWindowTokens > 0 {
			context = config.ContextWindowTokens
		} else {
			context = 0
		}
	}
	engines := map[string]bool{}
	for _, name := range []string{"ddg", "bing"} {
		// 对齐 :137-140 FREE_SEARCH_XXX_ENABLED == "true"
		engines[name] = strings.ToLower(get("FREE_SEARCH_"+strings.ToUpper(name)+"_ENABLED")) == "true"
	}
	// 搜索通道本身无需设置：未设置即选原生免费搜索通道；下方开关是设置页的用户开关
	searchMissing := []string{}
	for _, item := range []struct {
		setting string
		present bool
	}{
		{"FREE_SEARCH_DDG_ENABLED=true", engines["ddg"]},
		{"FREE_SEARCH_BING_ENABLED=true", engines["bing"]},
	} {
		if !item.present {
			searchMissing = append(searchMissing, item.setting)
		}
	}
	status := "ready"
	if len(missing) > 0 {
		status = "missing_configuration"
	} else if config == nil {
		status = "invalid_configuration"
	} else if len(searchMissing) > 0 {
		status = "native_search_unconfigured"
	}
	model := ""
	if config != nil {
		model = config.Model
	} else {
		model = get("ARK_MODEL")
	}
	contextValue := any(nil)
	if context > 0 {
		contextValue = context
	}
	return map[string]any{
		"provider":              "ark",
		"model":                 model,
		"context_window_tokens": contextValue,
		"configured":            config != nil,
		"missing_fields":        missing,
		"native_search":         engines,
		"native_search_missing": searchMissing,
		"status":                status,
		"last_failure":          nil,
	}
}

// PROVIDER_INVOCATION_EVENT_TYPE provider 调用账本事件类型（对齐 topic_model.py:393）
const PROVIDER_INVOCATION_EVENT_TYPE = "provider_invocation"

// PROVIDER_INVOCATION_POLICY_ID 调用小票策略（对齐 topic_model.py:409）
const PROVIDER_INVOCATION_POLICY_ID = "topic-starter-provider-invocation-v1"

// ──────────────────────────── 全局变量 ────────────────────────────

// _THINKING_DISABLED_EXTRA_BODY 所有模型共用一个 extra_body：Qwen 忽略 thinking.type
// 读 enable_thinking；reasoning_effort 不进本次请求（关 thinking 时 Doubao 会拒绝）。
// 对齐 topic_model.py:18-23。
var _THINKING_DISABLED_EXTRA_BODY = map[string]any{
	"thinking":        map[string]any{"type": "disabled"},
	"enable_thinking": false,
}

// _MAX_PENDING_VALIDATION_RECEIPTS 消费方在 record_validation 前失败的结果小票挂在这里；
// 上限调用方未调 aclose 时被引用的未消费结果数。对齐 topic_model.py:25-27。
const _MAX_PENDING_VALIDATION_RECEIPTS = 64

// currentBackgroundGenerationAttemptID 当前后台生成尝试 ID（topic_pool.py 提供）。
// ⤴️ 10.3.31b：topic_pool.go 落地后替换为真实实现并删占位。
var currentBackgroundGenerationAttemptID = func() string { return "" }

// ──────────────────────────── 结构体 ────────────────────────────

// ArkBackgroundConfig 一次后台调用；解码参数保持本模型给定的取值。
// 对齐 Python topic_model.py:30-115 @dataclass ArkBackgroundConfig。
type ArkBackgroundConfig struct {
	// APIKey 访问凭据（repr 屏蔽，对齐 api_key: field(repr=False)）
	APIKey string `json:"-"`
	// Model 模型名
	Model string `json:"model"`
	// BaseURL 端点
	BaseURL string `json:"base_url"`
	// Temperature 采样温度（0 与 0.0 在缓存身份中同义，对齐 __post_init__）
	Temperature float64 `json:"temperature"`
	// TopP 核采样
	TopP float64 `json:"top_p"`
	// ContextWindowTokens 上下文窗口 token 数（None 用 0 表示）
	ContextWindowTokens int `json:"context_window_tokens"`
	// ExpectedModel 期望模型（小票校验用）
	ExpectedModel string `json:"expected_model"`
}

// EffectiveTemperature 生效温度（对齐 :47-49 属性）
func (c *ArkBackgroundConfig) EffectiveTemperature() float64 { return c.Temperature }

// EffectiveTopP 生效核采样（对齐 :51-53 属性）
func (c *ArkBackgroundConfig) EffectiveTopP() float64 { return c.TopP }

// FromEnviron 只在端点也设置时才使用显式环境覆盖；缺 ARK_BASE_URL 不臆造厂商端点，
// 调用方继续用设置页选的对话模型。对齐 Python topic_model.py:55-94 from_environ。
func FromEnviron(environ map[string]string) *ArkBackgroundConfig {
	get := func(name string) string { return strings.TrimSpace(environ[name]) }
	apiKey := get("ARK_API_KEY")
	model := get("ARK_MODEL")
	baseURL := get("ARK_BASE_URL")
	contextWindowTokens := 0
	if raw := get("ARK_CONTEXT_WINDOW_TOKENS"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err != nil {
			contextWindowTokens = 0
		} else {
			contextWindowTokens = parsed
		}
	}
	if apiKey == "" || model == "" || baseURL == "" {
		return nil
	}
	if contextWindowTokens <= 0 {
		return nil
	}
	temperature := 0.0
	if raw := get("ARK_TEMPERATURE"); raw != "" {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil
		}
		temperature = parsed
	}
	topP := 1.0
	if raw := get("ARK_TOP_P"); raw != "" {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil
		}
		topP = parsed
	}
	expectedModel := get("ARK_EXPECTED_MODEL")
	return &ArkBackgroundConfig{
		APIKey:              apiKey,
		Model:               model,
		BaseURL:             baseURL,
		Temperature:         temperature,
		TopP:                topP,
		ContextWindowTokens: contextWindowTokens,
		ExpectedModel:       expectedModel,
	}
}

// SafeProvenance 不暴露凭据的配置快照。对齐 Python topic_model.py:96-115。
func (c *ArkBackgroundConfig) SafeProvenance() map[string]any {
	provenance := map[string]any{
		"model":                     c.Model,
		"base_url":                  c.BaseURL,
		"requested_temperature":     c.Temperature,
		"requested_top_p":           c.TopP,
		"effective_temperature":     c.EffectiveTemperature(),
		"effective_top_p":           c.EffectiveTopP(),
		"decoding_parameter_source": "request_parameters",
	}
	if c.ContextWindowTokens != 0 {
		provenance["context_window_tokens"] = c.ContextWindowTokens
		provenance["context_window_source"] = "ARK_CONTEXT_WINDOW_TOKENS"
	}
	if c.ExpectedModel != "" {
		provenance["expected_model"] = c.ExpectedModel
	}
	return provenance
}

// ProviderFailure 可归因的 provider 失败（对齐 Python 异常对象上的 code/status_code）。
type ProviderFailure interface {
	error
	// StatusCode HTTP 状态码（对齐 exc.status_code）
	StatusCode() int
	// Code 厂商错误码（对齐 exc.code）
	Code() string
	// Param response_format 校验失败时的参数名（对齐 exc.param）
	Param() string
}

// ProviderFailureError 测试与 HTTP 层共用的 ProviderFailure 实现。
type ProviderFailureError struct {
	Msg        string
	CodeStr    string
	HTTPStatus int
	ParamName  string
}

func (e *ProviderFailureError) Error() string   { return e.Msg }
func (e *ProviderFailureError) Code() string    { return e.CodeStr }
func (e *ProviderFailureError) StatusCode() int { return e.HTTPStatus }
func (e *ProviderFailureError) Param() string   { return e.ParamName }

// ChatClient OpenAI 兼容 chat completions 客户端（对齐 _default_client_factory 返回的
// AsyncOpenAI 的被使用面：chat.completions.create / close）。
type ChatClient interface {
	// CreateChatCompletion 对齐 client.chat.completions.create(**request)
	CreateChatCompletion(ctx context.Context, request map[string]any) (map[string]any, error)
	// Close 对齐 client.close()
	Close() error
}

// ChatClientFactory 构造客户端（对齐 client_factory 参数）。
type ChatClientFactory func(apiKey, baseURL string) (ChatClient, error)

// ArkStageInvoker 一个与请求无关、模型/解码显式的适配器。
// Python 以闭包 + setattr 附加 record_validation/invocation_receipts/aclose
// （:387-389）；Go 侧用结构体承载三者，属等价转换。
// 对齐 Python topic_model.py:195-390 build_ark_stage_invoker。

// TopicStageInvoker 阶段调用器消费面（TopicPipeline/TopicPreparePipeline
// 共用；*ArkStageInvoker 与 *v6ArkStageInvoker 均满足）。
type TopicStageInvoker interface {
	// Invoke 套系统提示词后调用阶段模型
	Invoke(ctx context.Context, stage, systemPrompt, payload string) (any, error)
	// RecordValidation 透传校验记录
	RecordValidation(response any, outcome string)
	// InvocationReceipts 调用小票
	InvocationReceipts() []map[string]any
	// AppendInvocationReceipt 追加一条调用小票（对齐 Python 对
	// invocation_receipts 列表的就地 append）
	AppendInvocationReceipt(receipt map[string]any)
}

type ArkStageInvoker struct {
	// client 聊天客户端
	client ChatClient
	// config 模型与解码配置
	config *ArkBackgroundConfig
	// invocationReceipts 调用小票（对齐 invocation_receipts 列表）
	invocationReceipts []map[string]any
	// validationReceipts 未消费回执（对齐 validation_receipts，键为结果身份）
	validationReceipts map[uintptr]pendingValidation
	// receiptSink 小票落库口（对齐 receipt_sink）
	receiptSink func(map[string]any)
	// responseSchemaForStage 各阶段响应 schema（对齐 response_schema_for_stage）
	responseSchemaForStage func(stage, payload string) map[string]any
	// requireResponseMetadata 要求响应元数据契约（对齐同名参数）
	requireResponseMetadata bool
	// maxTokensForStage 各阶段 max_tokens（对齐 max_tokens_for_stage）
	maxTokensForStage func(stage string) int
	// promptVersionForStage 各阶段提示词版本（对齐 prompt_version_for_stage）
	promptVersionForStage func(stage string) string
	// invocationLineageForStage 各阶段调用血缘（对齐 invocation_lineage_for_stage）
	invocationLineageForStage func(stage, payload string, result any) map[string]any
	// returnResponseMetadata 返回体带响应元数据（对齐同名参数）
	returnResponseMetadata bool
	// now 时钟（测试注入，等价 time.perf_counter）
	now func() float64
}

// pendingValidation 一条未消费校验回执（对齐 validation_receipts 的 tuple 值）。
type pendingValidation struct {
	result   any
	envelope map[string]any
}

// BuildArkStageInvoker 构造适配器（对齐 build_ark_stage_invoker 签名与默认值）。
func BuildArkStageInvoker(
	config *ArkBackgroundConfig,
	clientFactory ChatClientFactory,
	responseSchemaForStage func(stage, payload string) map[string]any,
	requireResponseMetadata bool,
	receiptSink func(map[string]any),
	maxTokensForStage func(stage string) int,
	promptVersionForStage func(stage string) string,
	invocationLineageForStage func(stage, payload string, result any) map[string]any,
	returnResponseMetadata bool,
) *ArkStageInvoker {
	client, _ := clientFactory(config.APIKey, config.BaseURL)
	return &ArkStageInvoker{
		client:                    client,
		config:                    config,
		validationReceipts:        make(map[uintptr]pendingValidation),
		receiptSink:               receiptSink,
		responseSchemaForStage:    responseSchemaForStage,
		requireResponseMetadata:   requireResponseMetadata,
		maxTokensForStage:         maxTokensForStage,
		promptVersionForStage:     promptVersionForStage,
		invocationLineageForStage: invocationLineageForStage,
		returnResponseMetadata:    returnResponseMetadata,
		now:                       func() float64 { return float64(time.Now().UnixNano()) / 1e9 },
	}
}

// InvocationReceipts 调用小票（对齐 setattr(invoke, "invocation_receipts", ...)）。
func (inv *ArkStageInvoker) InvocationReceipts() []map[string]any {
	return inv.invocationReceipts
}

// AppendInvocationReceipt 就地追加调用小票。
func (inv *ArkStageInvoker) AppendInvocationReceipt(receipt map[string]any) {
	inv.invocationReceipts = append(inv.invocationReceipts, receipt)
}

// Invoke 执行一次后台阶段调用。对齐闭包内 invoke(:214-377)：
// 步骤顺序、status 流转（started → received → refused/truncated/metadata_failed →
// json_failed → lineage_failed → parsed）、异常路径全覆盖。
func (inv *ArkStageInvoker) Invoke(ctx context.Context, stage, systemPrompt, payload string) (any, error) {
	// 对齐 :215-219 response schema
	var schema map[string]any
	if inv.responseSchemaForStage != nil {
		schema = inv.responseSchemaForStage(stage, payload)
	}
	// 对齐 :220-231 response_format
	responseFormat := map[string]any{"type": "json_object"}
	if schema != nil {
		name := stage
		if len(name) > 64 {
			name = name[:64]
		}
		responseFormat = map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   strings.ReplaceAll(name, "-", "_"),
				"strict": true,
				"schema": schema,
			},
		}
	}
	maxTokens := 0
	if inv.maxTokensForStage != nil {
		maxTokens = inv.maxTokensForStage(stage)
	}
	startedAt := inv.now()
	// 对齐 :234-245 请求体
	request := map[string]any{
		"model": inv.config.Model,
		"messages": []map[string]any{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": payload},
		},
		"temperature":     inv.config.EffectiveTemperature(),
		"top_p":           inv.config.EffectiveTopP(),
		"response_format": responseFormat,
	}
	if maxTokens > 0 {
		request["max_tokens"] = maxTokens
	}
	// 对齐 :247 extra_body（thinking 与 enable_thinking 非类型化参数，放顶层会 TypeError）
	request["extra_body"] = _THINKING_DISABLED_EXTRA_BODY
	// 对齐 :248-252 从 payload 提取 request_id
	requestID := ""
	var data map[string]any
	if err := json.Unmarshal([]byte(payload), &data); err == nil {
		if rid, ok := data["request_id"].(string); ok {
			requestID = rid
		}
	}
	// 对齐 :253-254 调用上下文（contextvar 快照 + provider/model）
	callContext := map[string]any{
		"provider": "ark",
		"model":    orDefault(inv.config.ExpectedModel, inv.config.Model),
	}
	for k, v := range providerInvocationIdentities(ctx) {
		callContext[k] = v
	}
	attemptID := fmt.Sprintf("ark-attempt-%s", uuid.New().String())
	status := "started"
	// 对齐 :261-264 structured_output_mode
	outputMode := "json_object"
	if schema != nil {
		outputMode = "json_schema_strict"
	}
	receipt := map[string]any{
		"stage":                  stage,
		"provider_response_id":   nil,
		"usage":                  nil,
		"structured_output_mode": outputMode,
	}
	if inv.promptVersionForStage != nil {
		if pv := inv.promptVersionForStage(stage); pv != "" {
			receipt["prompt_version"] = pv
		}
	}

	// save 对齐 :273-285：小票补时延与上下文后落库
	save := func() {
		receipt["latency_ms"] = roundMillis(inv.now() - startedAt)
		record := copyMap(receipt)
		record["attempt_id"] = attemptID
		record["request_id"] = orDefault(requestID, "")
		record["status"] = status
		for k, v := range callContext {
			record[k] = v
		}
		if inv.receiptSink != nil {
			inv.receiptSink(record)
		}
	}
	inv.invocationReceipts = append(inv.invocationReceipts, receipt)
	save()

	// 对齐 :289-366 成功路径（各阶段 status 状态机逐段对应）
	response, callErr := inv.client.CreateChatCompletion(ctx, request)
	if callErr != nil {
		// 对齐 :370-374
		if status == "started" {
			status = "request_failed"
			receipt["failure_reason"] = ProviderFailureReason(callErr)
		}
		save()
		return nil, callErr
	}
	usage := response["usage"]
	choices, _ := response["choices"].([]any)
	var choice map[string]any
	if len(choices) > 0 {
		choice, _ = choices[0].(map[string]any)
	}
	var message map[string]any
	if choice != nil {
		message, _ = choice["message"].(map[string]any)
	}
	receipt["provider_response_id"] = response["id"]
	receipt["response_object"] = response["object"]
	receipt["actual_model"] = response["model"]
	receipt["finish_reason"] = choice["finish_reason"]
	receipt["usage"] = usage
	status = "received"
	save()
	refusal := anyToString(message["refusal"])
	finishReason := anyToString(receipt["finish_reason"])
	status = "metadata_failed"
	if refusal != "" {
		status = "refused"
	} else if finishReason == "length" {
		status = "truncated"
	}
	// 对齐 :314-327 响应元数据契约
	if inv.requireResponseMetadata {
		expectedModel := orDefault(inv.config.ExpectedModel, inv.config.Model)
		providerResponseID, _ := receipt["provider_response_id"].(string)
		rejected := providerResponseID == "" ||
			anyToString(receipt["response_object"]) != "chat.completion" ||
			anyToString(receipt["actual_model"]) != expectedModel ||
			usage == nil ||
			finishReason != "stop"
		if rejected {
			save()
			return nil, errors.New("Ark response metadata contract failed")
		}
	}
	status = "json_failed"
	content := message["content"]
	switch v := content.(type) {
	case []any:
		var sb strings.Builder
		for _, part := range v {
			switch p := part.(type) {
			case string:
				sb.WriteString(p)
			case map[string]any:
				sb.WriteString(anyToString(p["text"]))
			}
		}
		content = sb.String()
	}
	if refusal != "" {
		status = "refused"
	}
	var result any
	if err := json.Unmarshal([]byte(anyToString(content)), &result); err != nil {
		save()
		return nil, err
	}
	status = "lineage_failed"
	if inv.invocationLineageForStage != nil {
		if lineage := inv.invocationLineageForStage(stage, payload, result); lineage != nil {
			receipt["lineage"] = lineage
		}
	}
	if refusal != "" {
		status = "refused"
	} else if finishReason == "length" {
		status = "truncated"
	} else {
		status = "parsed"
	}
	// 对齐 :375-376 finally: save()（成功路径的最终小票）
	save()
	// 对齐 :352-358 未消费回执登记 + 上限淘汰（FIFO）
	if inv.receiptSink != nil {
		envelope := copyMap(receipt)
		envelope["attempt_id"] = attemptID
		envelope["request_id"] = orDefault(requestID, "")
		for k, v := range callContext {
			envelope[k] = v
		}
		if key, ok := resultIdentity(result); ok {
			inv.validationReceipts[key] = pendingValidation{result: result, envelope: envelope}
			for len(inv.validationReceipts) > _MAX_PENDING_VALIDATION_RECEIPTS {
				inv.popOldestValidation()
			}
		}
	}
	if inv.returnResponseMetadata {
		return map[string]any{
			"result":               result,
			"provider_response_id": receipt["provider_response_id"],
			"usage":                receipt["usage"],
			"actual_model":         receipt["actual_model"],
		}, nil
	}
	return result, nil
}

// RecordValidation 消费方登记校验结论。对齐 Python topic_model.py:378-381 record_validation。
// Python 用 `entry[0] is response` 比身份；Go 以指针键命中即视为同一结果
// （map/slice/pointer 的键即身份），其余类型在登记侧已被拒绝。
func (inv *ArkStageInvoker) RecordValidation(response any, outcome string) {
	key, ok := resultIdentity(response)
	if !ok {
		return
	}
	entry, exists := inv.validationReceipts[key]
	if !exists || inv.receiptSink == nil {
		return
	}
	delete(inv.validationReceipts, key)
	record := copyMap(entry.envelope)
	record["status"] = outcome
	inv.receiptSink(record)
}

// Aclose 清空未消费回执并关闭客户端。对齐 Python topic_model.py:383-385 aclose。
func (inv *ArkStageInvoker) Aclose() error {
	inv.validationReceipts = make(map[uintptr]pendingValidation)
	if inv.client == nil {
		return nil
	}
	return inv.client.Close()
}

// RecordProviderValidation 通过 invoke_stage 形态的适配器登记校验结论。
// 对齐 Python topic_model.py:432-435 record_provider_validation。
func RecordProviderValidation(invoker *ArkStageInvoker, response any, outcome string) {
	if invoker != nil {
		invoker.RecordValidation(response, outcome)
	}
}

// PersistProviderInvocation 请求、收到的用量与适配器结论分开记账。
// 对齐 Python topic_model.py:396-414 persist_provider_invocation。
func PersistProviderInvocation(store *TopicStarterStore, receipt map[string]any) {
	payload := copyMap(receipt)
	payload["generation_attempt_id"] = currentBackgroundGenerationAttemptID()
	sessionID := anyToString(receipt["request_id"])
	if sessionID == "" {
		sessionID = anyToString(receipt["attempt_id"])
	}
	store.RecordEvent(map[string]any{
		"session_id":        sessionID,
		"slate_id":          receipt["attempt_id"],
		"event_type":        PROVIDER_INVOCATION_EVENT_TYPE,
		"topic_id":          receipt["status"],
		"policy_id":         PROVIDER_INVOCATION_POLICY_ID,
		"selection_mode":    "provider_invocation",
		"propensity_reason": "not_estimated",
		"payload":           payload,
		"created_at":        float64(time.Now().UnixMilli()) / 1_000,
	})
}

// ProviderFailureReason 命名一次失败的 provider 调用；拒绝类失败在设置变更前持续生效。
// 对齐 Python topic_model.py:168-186 provider_failure_reason。
func ProviderFailureReason(err error) string {
	var failure ProviderFailure
	if !errors.As(err, &failure) {
		return "provider_request_failed"
	}
	if failure.Code() == "AccountOverdueError" {
		return "account_overdue"
	}
	// Ark 对已关闭模型端点回 400 而非 404
	if failure.Code() == "InvalidEndpoint.ClosedEndpoint" {
		return "model_unavailable"
	}
	// 拒绝各阶段严格结构化输出的模型根本无法服务该链路
	if failure.StatusCode() == 400 &&
		(strings.Contains(failure.Param(), "response_format") ||
			strings.Contains(failure.Error(), "response_format")) {
		return "model_unavailable"
	}
	switch failure.StatusCode() {
	case 401:
		return "invalid_key"
	case 403:
		return "access_denied"
	case 404:
		return "model_unavailable"
	case 429:
		return "rate_limited"
	}
	return "provider_request_failed"
}

// ProviderInvocationIdentities 当前 provider 调用上下文身份（对齐 _INVOCATION_CONTEXT.get()）。
func ProviderInvocationIdentities(ctx context.Context) map[string]string {
	return providerInvocationIdentities(ctx)
}

// providerInvocationIdentities 从 ctx 读取身份（contextvar → Go ctx 的平台等价）。
func providerInvocationIdentities(ctx context.Context) map[string]string {
	identities, _ := ctx.Value(providerInvocationContextKey{}).(map[string]string)
	if identities == nil {
		return map[string]string{}
	}
	return identities
}

// ──────────────────────────── 非导出类型/函数 ────────────────────────────

// providerInvocationContextKey ctx 键（对齐 ContextVar "topic_starter_provider_invocation_context"）。
type providerInvocationContextKey struct{}

// structuredOutputMode 输出模式标识（备用，保留语义命名）。
func structuredOutputMode(strict bool) string {
	if strict {
		return "json_schema_strict"
	}
	return "json_object"
}

var _ = structuredOutputMode

// orDefault 空串回退。
func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// anyToString 安全转字符串。
func anyToString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// copyMap 浅拷贝（对齐 Python {**receipt}）。
func copyMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// resultIdentity 结果对象身份（对齐 Python id(result) 与 `is` 比较）。
// 仅 map/slice/pointer 有稳定指针；其余类型无身份（对应 Python id() 对不可变对象
// 的语义差异，登记为平台偏差：非容器结果不登记未消费回执）。
func resultIdentity(result any) (uintptr, bool) {
	if result == nil {
		return 0, false
	}
	v := reflect.ValueOf(result)
	switch v.Kind() {
	case reflect.Map, reflect.Slice, reflect.Ptr:
		if v.IsNil() {
			return 0, false
		}
		return v.Pointer(), true
	}
	return 0, false
}

// popOldestValidation 淘汰最旧未消费回执（对齐 :357-358 pop(next(iter(...)))）。
func (inv *ArkStageInvoker) popOldestValidation() {
	var oldest uintptr
	found := false
	for key := range inv.validationReceipts {
		if !found || key < oldest {
			oldest = key
			found = true
		}
	}
	if found {
		delete(inv.validationReceipts, oldest)
	}
}

// roundMillis 毫秒时延（对齐 :274-276 round(...*1000, 3)）。
func roundMillis(seconds float64) float64 {
	return float64(int64(seconds*1000*1000+0.5)) / 1000
}

// defaultArkChatClient 默认 OpenAI 兼容 HTTP 客户端（对齐 _default_client_factory）。
type defaultArkChatClient struct {
	apiKey  string
	baseURL string
	hc      *http.Client
}

// NewDefaultChatClientFactory 默认客户端工厂（max_retries=0：不发重试）。
func NewDefaultChatClientFactory() ChatClientFactory {
	return func(apiKey, baseURL string) (ChatClient, error) {
		return &defaultArkChatClient{apiKey: apiKey, baseURL: baseURL, hc: &http.Client{}}, nil
	}
}

// CreateChatCompletion OpenAI 兼容 /chat/completions 调用。
func (c *defaultArkChatClient) CreateChatCompletion(ctx context.Context, request map[string]any) (map[string]any, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(c.baseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &ProviderFailureError{Msg: "ark chat completion failed", HTTPStatus: resp.StatusCode}
	}
	return payload, nil
}

// Close 关闭底层连接（http.Client 无需显式关闭，保留接口面）。
func (c *defaultArkChatClient) Close() error { return nil }
