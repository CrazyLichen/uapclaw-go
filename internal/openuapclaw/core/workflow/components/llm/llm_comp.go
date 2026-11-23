package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/security"
	schemautils "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/common/utils/schemautils"
	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	forllm "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	modelclients "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/prompt"
	graphexe "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/executable"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components"
)

// ──────────────────────────── 常量 ────────────────────────────

// WORKFLOW_CHAT_HISTORY 工作流聊天历史键（llm_comp.py:25 逐字保留）
const WORKFLOW_CHAT_HISTORY = "workflow_chat_history"

// 模板元素键名（llm_comp.py:26-34 逐字保留，Python 私有 → Go 非导出）
const (
	// roleKey 模板元素角色键
	roleKey = "role"
	// contentKey 模板元素内容键
	contentKey = "content"
	// spanKey 追踪 span 键
	spanKey = "span"
	// workflowDataKey 工作流数据键
	workflowDataKey = "workflow_data"
	// idKey id 键
	idKey = "id"
	// typeKey type 键
	typeKey = "type"
	// instructionNameKey 指令名键
	instructionNameKey = "instruction_name"
	// templateNameKey 模板名键
	templateNameKey = "template_name"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ROLE_MAP 角色中文映射（llm_comp.py:28 逐字保留）
var ROLE_MAP = map[string]string{"user": "用户", "assistant": "助手", "system": "系统"}

// RESPONSE_FORMAT_TO_PROMPT_MAP 响应格式到提示词指令的映射（llm_comp.py:49-58 一比一）
var RESPONSE_FORMAT_TO_PROMPT_MAP = map[string]map[string]string{
	"json":     {instructionNameKey: "jsonInstruction", templateNameKey: "llm_json_formatting"},
	"markdown": {instructionNameKey: "markdownInstruction", templateNameKey: "llm_markdown_formatting"},
}

// responseFormatPattern 响应格式 type 正则（llm_comp.py:391 pattern）
var responseFormatPattern = regexp.MustCompile(`^(text|markdown|json)$`)

// ──────────────────────────── 枚举 ────────────────────────────

// MessageRole 模板消息角色（Python str Enum，值承载；llm_comp.py:37-40）
type MessageRole string

const (
	// MessageRoleUser 用户角色
	MessageRoleUser MessageRole = "user"
	// MessageRoleAssistant 助手角色
	MessageRoleAssistant MessageRole = "assistant"
	// MessageRoleFunction 函数角色
	MessageRoleFunction MessageRole = "function"
)

// WorkflowLLMResponseType 响应格式类型（llm_comp.py:43-46）
type WorkflowLLMResponseType string

const (
	// ResponseTypeJSON JSON 格式
	ResponseTypeJSON WorkflowLLMResponseType = "json"
	// ResponseTypeMarkdown Markdown 格式
	ResponseTypeMarkdown WorkflowLLMResponseType = "markdown"
	// ResponseTypeText 纯文本
	ResponseTypeText WorkflowLLMResponseType = "text"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LLMCompConfig LLM 组件配置（Python dataclass LLMCompConfig :377-388 一比一）
type LLMCompConfig struct {
	// ModelID 预注册模型 ID（None ≙ nil，走 client/request config 直构）
	ModelID *string
	// ModelClientConfig 模型客户端配置
	ModelClientConfig *llmschema.ModelClientConfig
	// ModelConfig 模型请求配置
	ModelConfig *llmschema.ModelRequestConfig
	// TemplateContent 模板消息列表（dict 元素；Python List[Any]）
	TemplateContent []any
	// SystemPromptTemplate 系统提示词模板
	SystemPromptTemplate *llmschema.SystemMessage
	// UserPromptTemplate 用户提示词模板
	UserPromptTemplate *llmschema.UserMessage
	// ResponseFormat 响应格式声明
	ResponseFormat map[string]any
	// OutputConfig 输出配置
	OutputConfig map[string]any
	// EnableHistory 是否启用会话历史
	EnableHistory bool
	// CacheStream 是否缓存流式结果
	CacheStream bool
}

// LLMExecutableState 流式缓存状态（Python LLMExecutableState :400-424 一比一）
type LLMExecutableState struct {
	// finalResult 最终结果缓存（Python self.final_result）
	finalResult map[string]any
	// accumulatedContent 累积内容（Python self._accumulated_content）
	accumulatedContent string
}

// LLMExecutable LLM 组件执行体（Python LLMExecutable :427-436 一比一）。
// 挂接方式（spec 拍板 #3）：复用 8.17 WorkflowComponent 槽位模式，invoke/stream
// 逻辑经 InvokeFn/StreamFn 槽接入（Task 6/7 装配）；实例字段 _session/_context/
// _state 逐字对齐无锁（spec 拍板 #4，Vertex 对同节点串行）。
type LLMExecutable struct {
	// WorkflowComponent 8.17 组件基座（四槽 + 组合面，spec 拍板 #3）
	components.WorkflowComponent
	// cfg 组件配置（Python self._config；字段名避开同名方法）
	cfg *LLMCompConfig
	// llModel 懒初始化模型实例（Python self._llm，Union[Model, None]）
	llModel LLMInvoker
	// initialized 懒初始化标记（Python self._initialized）
	initialized bool
	// session 当前会话（Python self._session）
	session *session.NodeSessionFacade
	// context 当前模型上下文（Python self._context）
	context iface.ModelContext
	// state 流式缓存状态（Python self._state）
	state *LLMExecutableState
}

// 统一模型调用面（导出，注入缝对外契约；（Python Union[Model, None]；*llm.Model 与
// model_clients.BaseModelClient 均满足——Runner.GetModel 返回后者，spec D-18-5）
type LLMInvoker interface {
	Invoke(ctx context.Context, messages modelclients.MessagesParam, opts ...modelclients.InvokeOption) (*llmschema.AssistantMessage, error)
	Stream(ctx context.Context, messages modelclients.MessagesParam, opts ...modelclients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error)
}

// LLMComponent LLM 组件组合面（Python LLMComponent :793-806；Task 8 落组合面槽，
// 本 Task 先落骨架占位——executable 懒初始化语义见 Task 8 D-18-4）
type LLMComponent struct {
	// BaseComponentComposable 组合面基座（Python ComponentComposable mixin 嵌入；
	// executable property 懒初始化由 ToExecutableFn 槽缓存承载，D-18-4）
	components.BaseComponentComposable
	// cfg 组件配置（Python self._config；字段名避开同名方法）
	cfg *LLMCompConfig
	// executable 缓存产物（Python self._executable）
	executable *LLMExecutable
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewLLMComponent 构造组件组合面（Python __init__ :794-797；
// _executable=None 懒初始化 → ToExecutableFn 槽首调构造缓存，D-18-4）
func NewLLMComponent(componentConfig *LLMCompConfig, opts ...func(*LLMComponent)) *LLMComponent {
	c := &LLMComponent{cfg: componentConfig}
	for _, opt := range opts {
		opt(c)
	}
	c.ToExecutableFn = c.toExecutableCached
	return c
}

// toExecutableCached 可执行体供应槽（Python to_executable :805-806 + executable
// property 懒初始化合并承载：首调构造并缓存）
func (c *LLMComponent) toExecutableCached() graphexe.GeneralExecutor {
	if c.executable == nil {
		e, err := NewLLMExecutable(c.cfg)
		if err != nil {
			panic(err)
		}
		c.executable = e
	}
	return c.executable
}

// NewLLMExecutable 构造执行体（Python __init__ :428-436：validate_config → 字段装配）。
// Python 构造异常抛出 → Go error 承载（构造期校验错误码 101000/101001/101002 原样透传）。
func NewLLMExecutable(componentConfig *LLMCompConfig) (*LLMExecutable, error) {
	if err := validateConfig(componentConfig); err != nil {
		return nil, err
	}
	e := &LLMExecutable{
		cfg:   componentConfig,
		state: newLLMExecutableState(),
	}
	// 四槽装配：invoke + stream 槽
	e.InvokeFn = e.invokeEntry
	e.StreamFn = e.streamEntry
	return e, nil
}

// ──────────────────────────── LLMExecutable 钩子遮蔽（已删）────────────────────────────
// 八次记录 8.21 审查收口：OnInvoke/OnStream 遮蔽与 baseInvoke/baseStream 删除——
// Python 基座 on_invoke 统一提取 kwargs["context"] 作 invoke 第三形参下发
// （component.py:95-99），Go 基座 InvokeFn/StreamFn 槽已补 modelContext 形参
// 同语义，组件无需再遮蔽拦截（Python LLMExecutable 亦无 on_invoke override）。

// ──────────────────────────── LLMExecutableState 方法 ────────────────────────────

// AccumulateContent 累积流式内容块（Python accumulate_content :407-409）
func (s *LLMExecutableState) AccumulateContent(content string) {
	s.accumulatedContent += content
}

// BuildFinalResult 从累积内容构建最终结果（Python build_final_result :411-419：
// 空累积返回空 dict，否则 OutputFormatter.format_response）
func (s *LLMExecutableState) BuildFinalResult(responseFormat map[string]any, outputConfig map[string]any) map[string]any {
	if s.accumulatedContent == "" {
		return map[string]any{}
	}
	return formatResponse(s.accumulatedContent, responseFormat, outputConfig)
}

// Clear 清空状态（Python clear :421-424）
func (s *LLMExecutableState) Clear() {
	s.finalResult = map[string]any{}
	s.accumulatedContent = ""
}

// ──────────────────────────── LLMExecutable 访问器 ────────────────────────────

// getConfig 返回配置（Python config property :438-440）
func (e *LLMExecutable) getConfig() *LLMCompConfig { return e.cfg }

// getState 返回流式缓存状态（Go 槽位消费面；Python 无对应 property）
func (e *LLMExecutable) getState() *LLMExecutableState { return e.state }

// ──────────────────────────── 非导出函数 ────────────────────────────

// newLLMExecutableState 空状态构造（Python __init__ :404-405）
func newLLMExecutableState() *LLMExecutableState {
	return &LLMExecutableState{finalResult: map[string]any{}}
}

// validateConfig 构造期校验链（Python _validate_config :787-790，顺序一比一）
func validateConfig(config *LLMCompConfig) error {
	if err := validateTemplate(config.TemplateContent, config.SystemPromptTemplate, config.UserPromptTemplate); err != nil {
		return err
	}
	if err := validateResponseFormat(config.ResponseFormat, config.OutputConfig); err != nil {
		return err
	}
	return validateOutputConfig(config.OutputConfig)
}

// validateTemplate 模板校验（Python _validate_template :443-472 一比一）
func validateTemplate(templateContent []any, systemPromptTemplate *llmschema.SystemMessage, userPromptTemplate *llmschema.UserMessage) error {
	// 步骤1：system/user 模板任一非 None 或模板内容为空 → 直接返回（:444-445）
	if systemPromptTemplate != nil || userPromptTemplate != nil || len(templateContent) == 0 {
		return nil
	}
	// 步骤2：校验所有 system 元素（Pydantic SystemMessage.model_validate 等价，
	// :446-455）；Python ValidationError → 101000 "system message is invalid"
	for _, element := range templateContent {
		dict, ok := element.(map[string]any)
		if !ok {
			continue
		}
		if roleString(dict) == "system" && !validSystemElement(dict) {
			return exception.BuildError(exception.StatusComponentLLMTemplateConfigError,
				exception.WithMsg("system message is invalid"))
		}
	}
	// 步骤3：遍历——user 必须存在，且 user 之后不得再出现 system（:457-472）
	ifContainUserMessage := false
	for _, element := range templateContent {
		dict, ok := element.(map[string]any)
		if !ok {
			continue
		}
		if roleString(dict) == "user" {
			// Pydantic UserMessage.model_validate 等价（:460）
			if !validUserElement(dict) {
				return exception.BuildError(exception.StatusComponentLLMTemplateConfigError,
					exception.WithMsg("user message is invalid"))
			}
			ifContainUserMessage = true
		}
		if ifContainUserMessage && roleString(dict) == "system" {
			// Python 同元素先 validate 再 raise，语义等价保留（:462-467）
			return exception.BuildError(exception.StatusComponentLLMTemplateConfigError,
				exception.WithMsg("system message must be before user message"))
		}
	}
	// 步骤4：无 user → 101000（:468-472）
	if !ifContainUserMessage {
		return exception.BuildError(exception.StatusComponentLLMTemplateConfigError,
			exception.WithMsg("user message is required"))
	}
	return nil
}

// validateResponseFormat 响应格式校验（Python _validate_response_format :507-522 一比一）
func validateResponseFormat(responseFormat map[string]any, outputConfig map[string]any) error {
	// 步骤1：type 正则校验（Pydantic ResponseFormatConfig.model_validate 等价，:509-516）
	typeStr, _ := responseFormat[typeKey].(string)
	if !responseFormatPattern.MatchString(typeStr) {
		return exception.BuildError(exception.StatusComponentLLMResponseConfigInvalid,
			exception.WithMsg(fmt.Sprintf("response format %v is invalid", responseFormat)))
	}
	// 步骤2：text/markdown 要求 output_config 恰一字段（:518-522）
	if (typeStr == "text" || typeStr == "markdown") && len(outputConfig) != 1 {
		return exception.BuildError(exception.StatusComponentLLMResponseConfigInvalid,
			exception.WithMsg("output config must contain exactly one parameter for text or markdown response type"))
	}
	return nil
}

// validateOutputConfig 输出配置校验（Python _validate_output_config :475-504 一比一）
func validateOutputConfig(outputConfig map[string]any) error {
	// 步骤1：空配置 → 101002（:476-480）
	if len(outputConfig) == 0 {
		return exception.BuildError(exception.StatusComponentLLMConfigError,
			exception.WithMsg("output config is empty"))
	}
	// 步骤2：object 型整体 schema 直通（:481-483）
	if t, ok := outputConfig[typeKey].(string); ok && t == "object" {
		return nil
	}
	// 步骤3：逐字段校验（:484-504）——空参数名 101002；OutputParamConfig 等价校验，
	// 失败按敏感模式分文案（:493-504）
	for param, value := range outputConfig {
		if param == "" {
			return exception.BuildError(exception.StatusComponentLLMConfigError,
				exception.WithMsg(fmt.Sprintf("output config parameter %s is empty", param)))
		}
		if !validOutputParam(value) {
			if security.IsSensitive() {
				return exception.BuildError(exception.StatusComponentLLMConfigError,
					exception.WithMsg("output config parameter's config value is invalid"))
			}
			return exception.BuildError(exception.StatusComponentLLMConfigError,
				exception.WithMsg(fmt.Sprintf("output config parameter's config %v is invalid", value)))
		}
	}
	return nil
}

// validSystemElement SystemMessage.model_validate 等价：role 必须为 system 且
// content 必填非空（pydantic 必填字段校验面，spec §4）
func validSystemElement(dict map[string]any) bool {
	content, ok := dict[contentKey]
	if !ok {
		return false
	}
	str, isStr := content.(string)
	return isStr && str != ""
}

// validUserElement UserMessage.model_validate 等价（同 validSystemElement）
func validUserElement(dict map[string]any) bool {
	return validSystemElement(dict)
}

// validOutputParam OutputParamConfig.model_validate 等价（llm_comp.py:394-397）：
// value 须为 map，type/description 为 string（缺省 ""）、required 为 bool（缺省 false）。
// Python pydantic 默认忽略多余键（extra=ignore），Go 同样放行未声明键。
func validOutputParam(value any) bool {
	dict, ok := value.(map[string]any)
	if !ok {
		return false
	}
	if t, exists := dict["type"]; exists {
		if _, isStr := t.(string); !isStr {
			return false
		}
	}
	if d, exists := dict["description"]; exists {
		if _, isStr := d.(string); !isStr {
			return false
		}
	}
	if r, exists := dict["required"]; exists {
		if _, isBool := r.(bool); !isBool {
			return false
		}
	}
	return true
}

// roleString 模板元素 role 键取值（Python element.get("role", "") 等价）
func roleString(dict map[string]any) string {
	role, _ := dict[roleKey].(string)
	return role
}

// formatResponse 响应格式化分派（Python OutputFormatter.format_response :201-215 一比一；
// Task 2 提前落地：BuildFinalResult 消费面，json 分支的 _format_json_response 于 Task 4 补齐）
func formatResponse(responseContent string, responseFormat map[string]any, outputsConfig map[string]any) map[string]any {
	responseType, _ := responseFormat[typeKey].(string)
	// Python: validate_outputs_config(outputs_config)（:203）
	validateOutputsConfig(outputsConfig)

	switch responseType {
	case "text", "markdown":
		return formatTextResponse(responseContent, outputsConfig)
	case "json":
		return formatJSONResponse(responseContent, outputsConfig)
	default:
		// Python: formatter 为 None → raise_invalid_params_error（:212-213）
		panicErr := exception.BuildError(exception.StatusComponentLLMConfigInvalid,
			exception.WithMsg(fmt.Sprintf("no supported response type: '%s'", responseType)))
		panic(panicErr)
	}
}

// validateOutputsConfig 输出配置非空校验（Python validate_outputs_config :132-137）
func validateOutputsConfig(outputsConfig map[string]any) {
	if len(outputsConfig) == 0 {
		panic(exception.BuildError(exception.StatusComponentLLMConfigInvalid,
			exception.WithMsg("outputs config must not be empty")))
	}
}

// formatTextResponse text/markdown 单字段包装（Python _format_text_response :218-223：
// 恰一字段，返回 {字段名: 原文}）
func formatTextResponse(responseContent string, outputsConfig map[string]any) map[string]any {
	if len(outputsConfig) != 1 {
		panic(exception.BuildError(exception.StatusComponentLLMConfigInvalid,
			exception.WithMsg("text/markdown response type, outputs_config must contain only one field")))
	}
	for fieldName := range outputsConfig {
		return map[string]any{fieldName: responseContent}
	}
	return map[string]any{}
}

// formatJSONResponse json 响应格式化（Python _format_json_response :226-241 一比一）
func formatJSONResponse(responseContent string, outputsConfig map[string]any) map[string]any {
	// 步骤1：空配置拒绝（:227-229）
	if len(outputsConfig) == 0 {
		panic(exception.BuildError(exception.StatusComponentLLMConfigInvalid,
			exception.WithMsg("json response format, output config should contain at least one field")))
	}
	// 步骤2：解析（含围栏剥离，:231）
	parsedJSON := parseJSONContent(responseContent)
	// 步骤3：object 型 schema 直用；否则生成 schema 校验后提取字段（:233-241）
	configType, _ := outputsConfig[typeKey].(string)
	if configType == "object" {
		// Python: SchemaUtils.validate_with_schema + format_with_schema（llm_comp.py:235-236，
		// 共享实现 core/common/utils/schemautils，D-18-1 收口）。错误按 Python 异常分类
		// 直传：SCHEMA_VALIDATE_INVALID/SCHEMA_FORMAT_INVALID 均为 BaseError，
		// _validate_json_schema（:247-248 except BaseError: raise）与 _create_output
		// （:687-695 仅拦截 101007）都原样再抛——Go panic(BaseError) 等价承载。
		if err := (schemautils.SchemaUtils{}).ValidateWithSchemaMap(parsedJSON, outputsConfig); err != nil {
			panic(err)
		}
		formatted, err := (schemautils.SchemaUtils{}).FormatWithSchemaMap(parsedJSON, outputsConfig)
		if err != nil {
			panic(err)
		}
		return formatted
	}
	jsonSchema := generateJSONSchema(outputsConfig)
	validateJSONSchemaForOutput(parsedJSON, jsonSchema, responseContent)
	return extractConfiguredFields(parsedJSON, outputsConfig)
}

// parseJSONContent 解析 JSON 文本（Python JsonParser.parse_json_content :170-179 一比一：
// 剥围栏后 json.loads；失败按敏感模式分文案 101007）
func parseJSONContent(responseContent string) map[string]any {
	content := cleanMarkdownBlocks(responseContent)
	var out map[string]any
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		if security.IsSensitive() {
			panic(exception.BuildError(exception.StatusComponentLLMConfigInvalid,
				exception.WithMsg("Json parse error")))
		}
		panic(exception.BuildError(exception.StatusComponentLLMConfigInvalid,
			exception.WithMsg(fmt.Sprintf("Json parse error: %s", responseContent))))
	}
	return out
}

// cleanMarkdownBlocks 剥 markdown 代码围栏（Python _clean_markdown_blocks :182-196 一比一）
func cleanMarkdownBlocks(content string) string {
	content = strings.TrimSpace(content)
	// 非 ``` 头尾围栏原样返回（:185-186）
	if !(strings.HasPrefix(content, "```") && strings.HasSuffix(content, "```")) {
		return content
	}
	lines := strings.Split(content, "\n")
	// 首行以 ``` 开头则剥除（:190-191）
	if strings.HasPrefix(lines[0], "```") {
		lines = lines[1:]
	}
	// 尾行恰为 ``` 则剥除（:193-194）
	if len(lines) > 0 && lines[len(lines)-1] == "```" {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// validateJSONSchemaForOutput json 输出校验包装（Python _validate_json_schema :244-253：
// BaseError 原样上抛、其他异常按敏感模式分文案）
func validateJSONSchemaForOutput(parsedJSON map[string]any, jsonSchema map[string]any, originalContent string) {
	validateJSONSchemaRecover(parsedJSON, jsonSchema, originalContent)
}

// validateJSONSchemaRecover 校验并按 Python 异常分类包装（BaseError 直传 / 其他包装）
func validateJSONSchemaRecover(parsedJSON map[string]any, jsonSchema map[string]any, originalContent string) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		if be, ok := r.(*exception.BaseError); ok {
			panic(be) // Python: except BaseError as e: raise e（:247-248）
		}
		if security.IsSensitive() {
			panic(exception.BuildError(exception.StatusComponentLLMConfigInvalid,
				exception.WithMsg("json schema validation failed.")))
		}
		panic(exception.BuildError(exception.StatusComponentLLMConfigInvalid,
			exception.WithMsg(fmt.Sprintf("json schema validation failed: %s", originalContent))))
	}()
	validateJSONSchema(parsedJSON, jsonSchema)
}

// extractConfiguredFields 提取声明字段（Python _extract_configured_fields :256-277 一比一：
// 缺 required 记入 missing；dict 值遍历剔除未声明 key——Python 海象怪癖逐字复刻；
// missing 非空按敏感模式分文案）
func extractConfiguredFields(parsedJSON map[string]any, outputsConfig map[string]any) map[string]any {
	output := map[string]any{}
	missingKeys := []string{}

	for fieldName, fieldConfigAny := range outputsConfig {
		value, present := parsedJSON[fieldName]
		if !present {
			fieldConfig, ok := fieldConfigAny.(map[string]any)
			// Python: field_config.get("required", True) —— 扁平 dict 缺省视为 required
			if !ok || requiredOf(fieldConfig) {
				missingKeys = append(missingKeys, fieldName)
			}
			continue
		}
		// Python: iterable_data = list(v) if (v := parsed_json[field_name]) and isinstance(v, dict) else []
		// —— 值非空且为 dict 才遍历剔除未声明 key
		if dict, isDict := value.(map[string]any); isDict && len(dict) > 0 {
			fieldConfig, _ := fieldConfigAny.(map[string]any)
			props, _ := fieldConfig["properties"].(map[string]any)
			for key := range dict {
				if _, declared := props[key]; !declared {
					delete(dict, key)
				}
			}
		}
		output[fieldName] = value
	}

	if len(missingKeys) > 0 {
		// D-18-6：Python dict 保序 → Go map 无序，missing 顺序不可复刻；
		// 取排序保确定性行为（错误文案字段顺序可能与 Python 不同）
		sort.Strings(missingKeys)
		if security.IsSensitive() {
			panic(exception.BuildError(exception.StatusComponentLLMConfigInvalid,
				exception.WithMsg("missing required fields.")))
		}
		panic(exception.BuildError(exception.StatusComponentLLMConfigInvalid,
			exception.WithMsg(fmt.Sprintf("missing required fields: %s", strings.Join(missingKeys, ", ")))))
	}
	return output
}

// validateType 六类型校验（Python validate_type :78-94 一比一：integer 特判 bool）
func validateType(instance any, expectedType string) bool {
	switch expectedType {
	case "object":
		_, ok := instance.(map[string]any)
		return ok
	case "array":
		_, ok := instance.([]any)
		return ok
	case "string":
		_, ok := instance.(string)
		return ok
	case "integer":
		if _, ok := instance.(int); ok {
			return true
		}
		// json.Unmarshal 数字落 float64：整值即整数（Python isinstance(int) 语义面）
		f, ok := instance.(float64)
		return ok && !math.IsNaN(f) && !math.IsInf(f, 0) && isIntegerFloat(f)
	case "boolean":
		_, ok := instance.(bool)
		return ok
	case "number":
		switch v := instance.(type) {
		case bool:
			return false
		case float64, int:
			return true
		default:
			_ = v
			return false
		}
	default:
		panic(exception.BuildError(exception.StatusComponentLLMConfigInvalid,
			exception.WithMsg(fmt.Sprintf("%s is not a valid type", expectedType))))
	}
}

// isIntegerFloat float64 是否整值（json 解析路径的 integer 判定辅助）
func isIntegerFloat(f float64) bool {
	return f == math.Trunc(f)
}

// validateJSONSchema JSON Schema 递归校验（Python validate_json_schema :97-106 一比一）
func validateJSONSchema(instance any, schema map[string]any) {
	schemaType, ok := schema[typeKey].(string)
	if !ok {
		panic(exception.BuildError(exception.StatusComponentLLMConfigInvalid,
			exception.WithMsg("schema must have 'type' key")))
	}
	if !validateType(instance, schemaType) {
		panic(exception.BuildError(exception.StatusComponentLLMConfigInvalid,
			exception.WithMsg(fmt.Sprintf("expected type %s but got %T", schemaType, instance))))
	}
	switch schemaType {
	case "object":
		validateObjectProperties(instance, schema)
	case "array":
		validateArrayItems(instance, schema)
	}
}

// validateObjectProperties object 属性校验（Python _validate_object_properties :109-118 一比一）
func validateObjectProperties(instance any, schema map[string]any) {
	dict, ok := instance.(map[string]any)
	if !ok {
		return
	}
	props, hasProps := schema["properties"].(map[string]any)
	if !hasProps {
		return
	}
	// required 缺失检查（:112-115）
	if requiredList, ok := schema["required"].([]any); ok {
		missing := []string{}
		for _, r := range requiredList {
			rs, _ := r.(string)
			if _, present := dict[rs]; !present {
				missing = append(missing, rs)
			}
		}
		if len(missing) > 0 {
			panic(exception.BuildError(exception.StatusComponentLLMConfigInvalid,
				exception.WithMsg(fmt.Sprintf("missing required properties %v", missing))))
		}
	}
	// properties 逐个递归（:116-118）
	for propName, propSchemaAny := range props {
		propSchema, ok := propSchemaAny.(map[string]any)
		if !ok {
			continue
		}
		if value, present := dict[propName]; present {
			validateJSONSchema(value, propSchema)
		}
	}
}

// validateArrayItems array 元素校验（Python _validate_array_items :121-129 一比一：
// 无 items 直接返回；逐项校验失败重包装 "invalid array item i: <异常类名>"）
func validateArrayItems(instance any, schema map[string]any) {
	list, ok := instance.([]any)
	if !ok {
		return
	}
	itemsSchema, hasItems := schema["items"].(map[string]any)
	if !hasItems {
		return
	}
	for i, item := range list {
		func() {
			defer func() {
				r := recover()
				if r == nil {
					return
				}
				be, isBase := r.(*exception.BaseError)
				typeName := fmt.Sprintf("%T", r)
				if isBase {
					typeName = be.Status().Name()
				}
				panic(exception.BuildError(exception.StatusComponentLLMConfigInvalid,
					exception.WithMsg(fmt.Sprintf("invalid array item %d: %s", i, typeName))))
			}()
			validateJSONSchema(item, itemsSchema)
		}()
	}
}

// generateJSONSchema outputs_config → 标准 JSON Schema（Python generate_json_schema
// :142-165 一比一：type 缺省 string、description 缺省 ""、array 透传 items、
// object 透传 properties、required 缺省 true）
func generateJSONSchema(outputsConfig map[string]any) map[string]any {
	properties := map[string]any{}
	required := []string{}

	for fieldName, fieldConfigAny := range outputsConfig {
		fieldConfig, _ := fieldConfigAny.(map[string]any)
		propType := "string"
		if t, ok := fieldConfig["type"].(string); ok && t != "" {
			propType = t
		}
		description := ""
		if d, ok := fieldConfig["description"].(string); ok {
			description = d
		}
		prop := map[string]any{"type": propType, "description": description}

		if propType == "array" {
			if items, ok := fieldConfig["items"]; ok {
				prop["items"] = items
			}
		}
		if propType == "object" {
			if p, ok := fieldConfig["properties"]; ok {
				prop["properties"] = p
			}
		}
		properties[fieldName] = prop

		if requiredOf(fieldConfig) {
			required = append(required, fieldName)
		}
	}

	// D-18-6：required 顺序排序保确定性（Python 插入序不可复刻）
	sort.Strings(required)
	return map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}
}

// requiredOf 扁平 output_config 字段的 required 语义（Python field_config.get("required", True)
// 与 OutputParamConfig param_required 默认 False 的双轨——format 链路取前者缺省 True）。
// 扁平 dict 缺省 → True；OutputParamConfig 已由 validOutputParam 校验面单独承载缺省 False。
func requiredOf(fieldConfig map[string]any) bool {
	if r, ok := fieldConfig["required"].(bool); ok {
		return r
	}
	return true
}

// ──────────────────────────── LLMPromptFormatter（llm_comp.py:280-373） ────────────────────────────

// defaultMarkdownInstruction 默认 markdown 格式指令（Python _DEFAULT_MARKDOWN_INSTRUCTION
// :282-292 英文原文逐字复制，含 ${query} 占位符）
const defaultMarkdownInstruction = "Please return the answer in markdown format.\n" +
	"- For headings, use number signs (#).\n" +
	"- For list items, start with dashes (-).\n" +
	"- To emphasize text, wrap it with asterisks (*).\n" +
	"- For code or commands, surround them with backticks (`).\n" +
	"- For quoted text, use greater than signs (>).\n" +
	"- For links, wrap the text in square brackets [], followed by the URL in parentheses ().\n" +
	"- For images, use square brackets [] for the alt text, followed by the image URL in parentheses ().\n" +
	"The question is: ${query}."

// defaultJSONInstruction 默认 JSON 格式指令（Python _DEFAULT_JSON_INSTRUCTION :294-302 逐字复制）
const defaultJSONInstruction = "Carefully consider the user's question to ensure your answer is logical and makes sense.\n" +
	"- Make sure your explanation is concise and easy to understand, not verbose.\n" +
	"- Strictly return the answer in valid JSON format only, and " +
	"\"DO NOT ADD ANY COMMENTS BEFORE OR AFTER IT\" to ensure it could be formatted " +
	"as a JSON instance that conforms to the JSON schema below.\n" +
	"Here is the JSON schema: ${json_schema}.\n" +
	"The question is: ${query}."

// findLastUserIndex 最后一条 user 消息下标（Python _find_last_user_index :327-331 一比一：
// 倒序遍历 role=="user"；无则 -1 ≙ Python None）
func findLastUserIndex(history []llmschema.BaseMessage) int {
	for idx := len(history) - 1; idx >= 0; idx-- {
		if history[idx].GetRole().String() == "user" {
			return idx
		}
	}
	return -1
}

// formatPrompt 提示词格式化（Python format_prompt :334-373 一比一：text 原样返回；
// 无 user 消息原样返回；markdown/json 找最后一条 user 消息改写后原地写回）
func formatPrompt(history []llmschema.BaseMessage, responseFormat map[string]any, outputConfig map[string]any) []llmschema.BaseMessage {
	resType, _ := responseFormat[typeKey].(string)
	// text 原样（:340-341）
	if resType == "text" {
		return history
	}
	lastUserIdx := findLastUserIndex(history)
	if lastUserIdx < 0 {
		return history
	}
	query := history[lastUserIdx].GetContent().String()
	prompt := query

	switch resType {
	case "markdown":
		// 自带 markdownInstruction 优先（:349-354）
		instruction, ok := responseFormat["markdownInstruction"].(string)
		if !ok || instruction == "" {
			instruction = defaultMarkdownInstruction
		}
		prompt = strings.Replace(instruction, "${query}", query, 1)
	case "json":
		// object 型 config_type 直接用作 schema，否则生成（:357-361）
		var jsonSchema map[string]any
		configType, _ := outputConfig[typeKey].(string)
		if configType == "object" {
			jsonSchema = outputConfig
		} else {
			jsonSchema = generateJSONSchema(outputConfig)
		}
		instruction, ok := responseFormat["jsonInstruction"].(string)
		if !ok || instruction == "" {
			instruction = defaultJSONInstruction
		}
		// ${json_schema} 序列化：ensure_ascii=False → Go Encoder SetEscapeHTML(false)
		//（spec 预登记偏差第 4 条，8.9a D-9 同款）
		var sb strings.Builder
		enc := json.NewEncoder(&sb)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(jsonSchema); err != nil {
			panic(exception.BuildError(exception.StatusComponentLLMConfigInvalid,
				exception.WithMsg(fmt.Sprintf("json schema marshal failed: %v", err))))
		}
		schemaJSON := strings.TrimSuffix(sb.String(), "\n")
		prompt = strings.Replace(instruction, "${json_schema}", schemaJSON, 1)
		prompt = strings.Replace(prompt, "${query}", query, 1)
	}
	// 原地写回（:372）
	history[lastUserIdx].SetContent(llmschema.NewTextContent(prompt))
	return history
}

// ──────────────────────────── LLMExecutable invoke 链（llm_comp.py:524-572） ────────────────────────────

// setSession 注入会话（Python _set_session :697-698）
func (e *LLMExecutable) setSession(sess *session.NodeSessionFacade) {
	e.session = sess
}

// setContext 注入模型上下文（Python _set_context :700-701）
func (e *LLMExecutable) setContext(mc iface.ModelContext) {
	e.context = mc
}

// invokeEntry 批入批出槽函数（Python invoke :524-572 十步一比一）
func (e *LLMExecutable) invokeEntry(ctx context.Context, inputs map[string]any, sess *session.NodeSessionFacade, modelContext iface.ModelContext) (map[string]any, error) {
	// 步骤1：setSession/setContext（:525-526；context 由基座统一下发，
	// 八次记录 8.21 审查对齐 Python on_invoke 形参语义）
	e.setSession(sess)
	e.setContext(modelContext)
	// 步骤2：准备模型输入（懒初始化 + 提示词组装，:527）
	modelInputs, err := e.prepareModelInputs(ctx, inputs)
	if err != nil {
		return nil, err
	}
	// 步骤3：START 日志（:528-539）
	modelName := ""
	if e.cfg.ModelConfig != nil {
		modelName = e.cfg.ModelConfig.ModelName
	}
	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "WORKFLOW_COMPONENT_START").
		Str("component_id", e.session.GetExecutableID()).
		Str("component_type_str", "LLMComponent").
		Str("session_id", e.session.GetSessionID()).
		Str("model_name", modelName).
		Bool("has_inputs", len(modelInputs) > 0).
		Bool("sensitive_mode", security.IsSensitive()).
		Msg("LLM component invoke started")
	// 步骤4：写最后一条 user 消息进 context（:540）
	e.writeUserMessageToContext(ctx, modelInputs)
	// 步骤5-6：invoke → 取 content（:541-557）
	response := ""
	llmResponse, invokeErr := e.llModel.Invoke(ctx, modelclients.NewMessagesParam(modelInputs...))
	if invokeErr != nil {
		msg := invokeErr.Error()
		if security.IsSensitive() {
			msg = "invoke llm failed"
		}
		return nil, exception.BuildError(exception.StatusComponentLLMInvokeCallFailed,
			exception.WithMsg(msg), exception.WithCause(invokeErr))
	}
	response = llmResponse.Content.String()
	// 步骤7：END 日志（:559-570）
	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "WORKFLOW_COMPONENT_END").
		Str("component_id", e.session.GetExecutableID()).
		Str("component_type_str", "LLMComponent").
		Str("session_id", e.session.GetSessionID()).
		Bool("has_response", response != "").
		Int("response_length", len(response)).
		Bool("sensitive_mode", security.IsSensitive()).
		Msg("LLM component invoke completed")
	// 步骤8：写 assistant 消息进 context（:571）
	e.writeAssistantMessageToContext(ctx, response)
	// 步骤9：格式化输出（101007 → 101004 转换，:681-695 + :572；
	// Python 异常上抛 → Go recover 转 error）
	return e.createOutputAsResult(response)
}

// createOutputAsResult createOutput 的 error 返回形态（Python 调用面异常上抛承载）
func (e *LLMExecutable) createOutputAsResult(llmOutput string) (out map[string]any, err error) {
	defer func() {
		if r := recover(); r != nil {
			be, ok := r.(*exception.BaseError)
			if !ok {
				panic(r)
			}
			err = be
		}
	}()
	return e.createOutput(llmOutput), nil
}

// prepareModelInputs 懒初始化 + 组装输入（Python _prepare_model_inputs :703-705）
func (e *LLMExecutable) prepareModelInputs(ctx context.Context, inputs map[string]any) ([]llmschema.BaseMessage, error) {
	if err := e.initializeIfNeeded(ctx); err != nil {
		return nil, err
	}
	return e.getModelInput(inputs), nil
}

// initializeIfNeeded 懒初始化（Python _initialize_if_needed :612-622：
// 失败包装 101005）
func (e *LLMExecutable) initializeIfNeeded(ctx context.Context) error {
	if e.initialized {
		return nil
	}
	model, err := e.createLLMInstance(ctx)
	if err != nil {
		return exception.BuildError(exception.StatusComponentLLMInitFailed,
			exception.WithMsg("failed to initialize llm if needed"), exception.WithCause(err))
	}
	e.llModel = model
	e.initialized = true
	return nil
}

// createLLMInstance 模型实例创建（Python _create_llm_instance :624-634 一比一：
// model_id 为 nil → client/request config 双非空校验（缺任一 101003）后 NewModel；
// 否则按 ID 走 Runner）
func (e *LLMExecutable) createLLMInstance(ctx context.Context) (LLMInvoker, error) {
	if e.cfg.ModelID == nil {
		if e.cfg.ModelClientConfig == nil || e.cfg.ModelConfig == nil {
			return nil, exception.BuildError(exception.StatusComponentLLMInvokeCallFailed,
				exception.WithMsg("failed to create llm instance"))
		}
		return forllm.NewModel(e.cfg.ModelClientConfig, e.cfg.ModelConfig)
	}
	model, err := e.getModel(ctx, *e.cfg.ModelID)
	if err != nil {
		return nil, err
	}
	return model, nil
}

// getModel 按 ID 取预注册模型（Python Runner.resource_mgr.get_model(id=...) 一比一；
// 8.21 连带收口：D-18-5 解析缝废弃，消费点直调全局单例）
func (e *LLMExecutable) getModel(ctx context.Context, modelID string) (LLMInvoker, error) {
	mgr := runner.GetResourceMgr()
	if mgr == nil {
		return nil, fmt.Errorf("resource mgr is not initialized")
	}
	clients, err := mgr.GetModel(ctx, []string{modelID})
	if err != nil {
		return nil, err
	}
	if len(clients) == 0 {
		return nil, fmt.Errorf("model %s not found", modelID)
	}
	return clients[0], nil
}

// getModelInput 组装模型输入（Python _get_model_input :656-662 步骤一比一：
// system prompt → user prompt → 拼接历史 → 提示词格式化）
func (e *LLMExecutable) getModelInput(inputs map[string]any) []llmschema.BaseMessage {
	systemPrompt := e.buildSystemPrompt(inputs)
	userPrompt := e.buildUserPromptContent(inputs)
	allPrompts := e.insertHistoryToSystemAndUserPrompt(systemPrompt, userPrompt)
	return formatPrompt(allPrompts, e.cfg.ResponseFormat, e.cfg.OutputConfig)
}

// insertHistoryToSystemAndUserPrompt 历史拼接（Python :664-670 一比一：
// enable_history 且 context 非 None 时取 context.get_messages() 拼在 system 后、user 前）
func (e *LLMExecutable) insertHistoryToSystemAndUserPrompt(systemPrompt []llmschema.BaseMessage, userPrompt []llmschema.BaseMessage) []llmschema.BaseMessage {
	originalHistory := make([]llmschema.BaseMessage, 0, len(systemPrompt)+len(userPrompt))
	originalHistory = append(originalHistory, systemPrompt...)
	if e.cfg.EnableHistory && e.context != nil {
		chatHistory, err := e.context.GetMessages(0, true)
		if err == nil && chatHistory != nil {
			originalHistory = append(originalHistory, chatHistory...)
		}
	}
	originalHistory = append(originalHistory, userPrompt...)
	return originalHistory
}

// buildSystemPrompt 系统提示词组装（Python _build_system_prompt :754-769 一比一：
// 模板优先分支；否则取 template_content 头部连续 system 元素——遇非 system 即 break）
func (e *LLMExecutable) buildSystemPrompt(inputs map[string]any) []llmschema.BaseMessage {
	// 分支1：system/user 模板任一非 nil（:758-761）
	if e.cfg.SystemPromptTemplate != nil || e.cfg.UserPromptTemplate != nil {
		if e.cfg.SystemPromptTemplate == nil {
			return nil
		}
		return formatTemplateMessage(e.cfg.SystemPromptTemplate.GetRole(), e.cfg.SystemPromptTemplate.GetContent().String(), inputs)
	}
	// 分支2：template_content 头部连续 system 元素（:763-768）
	systemPrompt := []any{}
	for _, element := range e.cfg.TemplateContent {
		dict, ok := element.(map[string]any)
		if !ok {
			break
		}
		if roleString(dict) == "system" {
			systemPrompt = append(systemPrompt, dict)
		} else {
			break
		}
	}
	if len(systemPrompt) == 0 {
		return nil
	}
	return formatTemplateDicts(systemPrompt, inputs)
}

// buildUserPromptContent 用户提示词组装（Python _build_user_prompt_content :636-654 一比一）
func (e *LLMExecutable) buildUserPromptContent(inputs map[string]any) []llmschema.BaseMessage {
	// 分支1：system/user 模板任一非 nil——user 缺省 → [UserMessage(content="")]（:640-643）
	if e.cfg.SystemPromptTemplate != nil || e.cfg.UserPromptTemplate != nil {
		if e.cfg.UserPromptTemplate == nil {
			return []llmschema.BaseMessage{llmschema.NewUserMessage("")}
		}
		return formatTemplateMessage(e.cfg.UserPromptTemplate.GetRole(), e.cfg.UserPromptTemplate.GetContent().String(), inputs)
	}
	// 分支2：template_content 取首个 user 元素（:645-654）
	templateContent := e.cfg.TemplateContent
	if len(templateContent) == 0 {
		return []llmschema.BaseMessage{llmschema.NewUserMessage("")}
	}
	var userPrompt []any
	for _, element := range templateContent {
		dict, ok := element.(map[string]any)
		if !ok {
			continue
		}
		if roleString(dict) == string(MessageRoleUser) {
			userPrompt = append(userPrompt, dict)
		}
	}
	if len(userPrompt) == 0 {
		return []llmschema.BaseMessage{llmschema.NewUserMessage("")}
	}
	return formatTemplateDicts([]any{userPrompt[0]}, inputs)
}

// formatTemplateDicts 模板 dict 元素 → 格式化消息（Python PromptTemplate(content=[...])
// .format(inputs).to_messages() 等价面：取 content 文本 → Format(inputs) → 按原 role
// 重建消息；Go ToMessages 仅支持 string/[]BaseMessage，role 由本助手承载）
func formatTemplateDicts(elements []any, inputs map[string]any) []llmschema.BaseMessage {
	out := make([]llmschema.BaseMessage, 0, len(elements))
	for _, element := range elements {
		dict, ok := element.(map[string]any)
		if !ok {
			continue
		}
		content, _ := dict[contentKey].(string)
		out = append(out, formatTemplateString(roleString(dict), content, inputs))
	}
	return out
}

// formatTemplateMessage 模板消息对象 → 格式化消息（同上，role/content 取自消息对象）
func formatTemplateMessage(role llmschema.RoleType, content string, inputs map[string]any) []llmschema.BaseMessage {
	return []llmschema.BaseMessage{formatTemplateString(role.String(), content, inputs)}
}

// formatTemplateString 单条模板渲染（role 保留 + content 经 PromptTemplate.Format）
func formatTemplateString(role, content string, inputs map[string]any) llmschema.BaseMessage {
	tpl := prompt.NewPromptTemplate("", content)
	rendered, err := tpl.Format(inputs)
	if err != nil {
		rendered = tpl
	}
	text := content
	if msgs, mErr := rendered.ToMessages(); mErr == nil && len(msgs) > 0 {
		text = msgs[0].GetContent().String()
	}
	// role 重建：system → SystemMessage，其余 → UserMessage（模板面仅此两角色）
	if role == llmschema.RoleTypeSystem.String() {
		return llmschema.NewSystemMessage(text)
	}
	return llmschema.NewUserMessage(text)
}

// writeUserMessageToContext 写最后一条 user 消息（Python _write_user_message_to_context
// :771-779 一比一：context 非空 + enable_history 才写；倒序找首个 role=user 且 content 非空）
func (e *LLMExecutable) writeUserMessageToContext(ctx context.Context, modelInputs []llmschema.BaseMessage) {
	if e.context == nil || !e.cfg.EnableHistory {
		return
	}
	for i := len(modelInputs) - 1; i >= 0; i-- {
		msg := modelInputs[i]
		if msg.GetRole().String() == "user" {
			if content := msg.GetContent().String(); content != "" {
				_, _ = e.context.AddMessages(ctx, []llmschema.BaseMessage{llmschema.NewUserMessage(content)})
			}
			break
		}
	}
}

// writeAssistantMessageToContext 写 assistant 消息（Python _write_assistant_message_to_context
// :781-785 一比一）
func (e *LLMExecutable) writeAssistantMessageToContext(ctx context.Context, content string) {
	if e.context == nil || !e.cfg.EnableHistory {
		return
	}
	if content != "" {
		_, _ = e.context.AddMessages(ctx, []llmschema.BaseMessage{llmschema.NewAssistantMessage(content)})
	}
}

// createOutput 格式化输出（Python _create_output :681-695 一比一：101007 → 101004
// 转换重抛，其他 BaseError 原样上抛；panic 承载 Python raise 控制流）
func (e *LLMExecutable) createOutput(llmOutput string) (out map[string]any) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		be, ok := r.(*exception.BaseError)
		if !ok {
			panic(r)
		}
		if be.Status().Code() == exception.StatusComponentLLMConfigInvalid.Code() {
			// Python: e.code == CONFIG_INVALID.code → 转 EXECUTION_PROCESS_ERROR
			panic(exception.BuildError(exception.StatusComponentLLMExecutionProcessError,
				exception.WithMsg(be.Message()), exception.WithCause(be)))
		}
		panic(be)
	}()
	return formatResponse(llmOutput, e.cfg.ResponseFormat, e.cfg.OutputConfig)
}

// ──────────────────────────── LLMExecutable stream 链（llm_comp.py:574-752） ────────────────────────────

// streamEntry 批入流出槽函数（Python stream :574-600 外层 + :707-752 双分支一比一）
func (e *LLMExecutable) streamEntry(ctx context.Context, inputs map[string]any, sess *session.NodeSessionFacade, modelContext iface.ModelContext) (<-chan graphexe.StreamChunk[any], error) {
	// Python stream 是 async generator——入口先执行的部分（:575-580 + 分派）同步，
	// 产出经 channel；入口错误走第二返回值（流式签名 A'，8.2 spec）
	e.setSession(sess)
	e.setContext(modelContext) // Python :576 _set_context
	// Reset state for new stream（:577-579）
	if e.cfg.CacheStream {
		e.state.Clear()
	}
	responseFormatType, _ := e.cfg.ResponseFormat[typeKey].(string)
	out := make(chan graphexe.StreamChunk[any])
	go func() {
		defer close(out)
		streamErr := func() error {
			defer func() {
				// except 分支：cache_stream 清状态重抛（:588-600 + :748-751）
				if r := recover(); r != nil {
					if be, ok := r.(*exception.BaseError); ok {
						if e.cfg.CacheStream {
							e.state.Clear()
						}
						panic(be)
					}
					panic(r)
				}
			}()
			if responseFormatType == string(ResponseTypeJSON) {
				return e.invokeForJSONFormat(ctx, inputs, out)
			}
			return e.streamWithChunks(ctx, inputs, out)
		}()
		if streamErr != nil {
			// 外层异常包装 101003（敏感 "failed to stream" vs str(e)，:588-600）
			msg := streamErr.Error()
			if security.IsSensitive() {
				msg = "failed to stream"
			}
			out <- graphexe.StreamChunk[any]{Err: exception.BuildError(exception.StatusComponentLLMInvokeCallFailed,
				exception.WithMsg(msg), exception.WithCause(streamErr))}
		}
	}()
	return out, nil
}

// getStreamOutput 流式缓存产物（Python get_stream_output :602-610 一比一：
// cache_stream 且 session 非空才产出；空结果返 nil）
func (e *LLMExecutable) getStreamOutput() map[string]any {
	output := map[string]any(nil)
	if e.cfg.CacheStream && e.session != nil {
		finalResult := e.state.BuildFinalResult(e.cfg.ResponseFormat, e.cfg.OutputConfig)
		if len(finalResult) > 0 {
			output = finalResult
		}
	}
	return output
}

// invokeForJSONFormat json 格式流式分支（Python _invoke_for_json_format :707-731
// 一比一：整体 Invoke 后单帧 yield）
func (e *LLMExecutable) invokeForJSONFormat(ctx context.Context, inputs map[string]any, out chan<- graphexe.StreamChunk[any]) error {
	modelInputs, err := e.prepareModelInputs(ctx, inputs)
	if err != nil {
		return err
	}
	// START 日志（response_format:"json"，:709-720）
	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "WORKFLOW_COMPONENT_START").
		Str("component_id", e.session.GetExecutableID()).
		Str("component_type_str", "LLMComponent").
		Str("session_id", e.session.GetSessionID()).
		Str("response_format", "json").
		Bool("has_inputs", len(modelInputs) > 0).
		Bool("sensitive_mode", security.IsSensitive()).
		Msg("LLM component JSON format invoke started")
	e.writeUserMessageToContext(ctx, modelInputs)
	// 整体 Invoke（:722-723）
	llmResponse, invokeErr := e.llModel.Invoke(ctx, modelclients.NewMessagesParam(modelInputs...))
	if invokeErr != nil {
		return invokeErr
	}
	llmOutputContent := llmResponse.Content.String()
	// cache_stream 累积（:725-726）
	if e.cfg.CacheStream {
		e.state.AccumulateContent(llmOutputContent)
	}
	// 写 assistant 消息（:728）
	e.writeAssistantMessageToContext(ctx, llmOutputContent)
	// 格式化后单帧 yield（:729-731；createOutput 的 101007→101004 转换同 invoke 链）
	output, oErr := e.createOutputAsResult(llmOutputContent)
	if oErr != nil {
		return oErr
	}
	out <- graphexe.StreamChunk[any]{Val: output}
	return nil
}

// streamWithChunks 逐 chunk 流式分支（Python _stream_with_chunks :733-752 一比一）
func (e *LLMExecutable) streamWithChunks(ctx context.Context, inputs map[string]any, out chan<- graphexe.StreamChunk[any]) error {
	modelInputs, err := e.prepareModelInputs(ctx, inputs)
	if err != nil {
		return err
	}
	e.writeUserMessageToContext(ctx, modelInputs)
	accumulatedContent := ""
	chunkCh, streamStartErr := e.llModel.Stream(ctx, modelclients.NewMessagesParam(modelInputs...))
	if streamStartErr != nil {
		if e.cfg.CacheStream {
			e.state.Clear()
		}
		return streamStartErr
	}
	for chunk := range chunkCh {
		// StreamError 哨兵：流读取中断（foundation/llm 契约，Python 生成器 raise 等价）
		if chunk != nil && chunk.StreamError != nil {
			if e.cfg.CacheStream {
				e.state.Clear()
			}
			return chunk.StreamError
		}
		content := extractContent(chunk)
		if content == "" {
			continue
		}
		if e.cfg.CacheStream {
			e.state.AccumulateContent(content)
		}
		accumulatedContent += content
		// 每 chunk 单独过格式化器（:744-747；text/markdown 单字段包装逐帧可行）
		formatted, fErr := func() (m map[string]any, ferr error) {
			defer func() {
				if r := recover(); r != nil {
					be, ok := r.(*exception.BaseError)
					if !ok {
						panic(r)
					}
					ferr = be
				}
			}()
			return formatResponse(content, e.cfg.ResponseFormat, e.cfg.OutputConfig), nil
		}()
		if fErr != nil {
			if e.cfg.CacheStream {
				e.state.Clear()
			}
			return fErr
		}
		out <- graphexe.StreamChunk[any]{Val: formatted}
	}
	// 成功收尾写 assistant 消息（:752）
	e.writeAssistantMessageToContext(ctx, accumulatedContent)
	return nil
}

// extractContent 响应内容提取（Python WorkflowLLMUtils.extract_content :64-65 一比一：
// has content 属性取之，否则 str() 兜底——AssistantMessageChunk 取 Content，其他 fmt 转串）
func extractContent(response any) string {
	switch r := response.(type) {
	case *llmschema.AssistantMessageChunk:
		if r == nil {
			return ""
		}
		return r.Content.String()
	case *llmschema.AssistantMessage:
		if r == nil {
			return ""
		}
		return r.Content.String()
	case string:
		return r
	default:
		return fmt.Sprintf("%v", response)
	}
}
