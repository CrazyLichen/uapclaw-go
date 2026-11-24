// 8.24 QuestionerComp——交互提问组件（Python questioner_comp.py 一比一）。
// 多轮交互参数收集：START 直发问/LLM 抽取 → USER_INTERACT 挂起恢复循环 → END 清状态重入。
// 中断经 session.interact（PanicGraphInterrupt，5.7 已回填）；状态 questioner_state 存节点会话。
// 已知偏差见 spec docs/superpowers/specs/2026-11-24-questioner-comp-8.24-design.md §5。
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/pythonsem"
	"github.com/uapclaw/uapclaw-go/internal/common/security"
	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	forllm "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	modelclients "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/prompt"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components"
)

// ──────────────────────────── 结构体 ────────────────────────────

// QuestionerFieldInfo 抽取字段定义（Python FieldInfo :120-126 一比一）
type QuestionerFieldInfo struct {
	// FieldName 字段名（LLM 输出 JSON 键名锚点）
	FieldName string `json:"field_name"`
	// Description 字段描述（追问文案/必填名单的回落显示名）
	Description string `json:"description"`
	// Type 期望类型（string/integer/number/boolean，默认 string）
	Type string `json:"type"`
	// CnFieldName 中文名（优先用于追问文案与必填名单）
	CnFieldName string `json:"cn_field_name"`
	// Required 是否必填（缺则追问/超限报错）
	Required bool `json:"required"`
	// DefaultValue 兜底默认值（始终未抽到时回填）
	DefaultValue any `json:"default_value"`
}

// QuestionerConfig 组件配置（Python QuestionerConfig :129-143 一比一；默认值经
// newQuestionerConfigDefaults 补齐 dataclass 默认）
type QuestionerConfig struct {
	// ModelID 预注册模型 ID（nil ≙ None）
	ModelID *string
	// ModelClientConfig 模型客户端配置
	ModelClientConfig *llmschema.ModelClientConfig
	// ModelConfig 模型请求配置
	ModelConfig *llmschema.ModelRequestConfig
	// ResponseType 响应类型（唯一成员 reply_directly，默认）
	ResponseType string
	// QuestionContent 固定问题模板（{{}} 占位符用输入额外字段渲染）
	QuestionContent string
	// ExtractFieldsFromResponse 是否启用字段抽取（Python dataclass 默认 True；
	// D-24-9：Go bool 零值歧义，改 *bool，nil ≙ Python 默认 True）
	ExtractFieldsFromResponse *bool
	// FieldNames 字段表
	FieldNames []QuestionerFieldInfo
	// MaxResponse 最大追问轮数（默认 3，超限抛 101074）
	MaxResponse int
	// WithChatHistory 是否启用对话历史（默认 false）
	WithChatHistory bool
	// ChatHistoryMaxRounds 历史最大轮数（默认 5）
	ChatHistoryMaxRounds int
	// ExtraPromptForFieldsExtraction 抽取附加约束（{{extra_info}} 槽）
	ExtraPromptForFieldsExtraction string
	// ExampleContent 抽取示例（{{example}} 槽）
	ExampleContent string
	// AcceptLanguage 语言（'zh'|'en'，默认 'zh'；贯穿模板/追问文案/必填名单分隔符）
	AcceptLanguage string
}

// newQuestionerConfigDefaults 补 Python dataclass 默认值（构造入口统一调用）。
// 注意零值歧义：MaxResponse/ChatHistoryMaxRounds 未配置（0）时补默认，与
// Python dataclass default 语义一致；ResponseType 空串补 reply_directly。
func newQuestionerConfigDefaults(cfg *QuestionerConfig) *QuestionerConfig {
	if cfg.ResponseType == "" {
		cfg.ResponseType = ResponseTypeReplyDirectly
	}
	if cfg.MaxResponse == 0 {
		cfg.MaxResponse = 3
	}
	if cfg.ChatHistoryMaxRounds == 0 {
		cfg.ChatHistoryMaxRounds = 5
	}
	if cfg.AcceptLanguage == "" {
		cfg.AcceptLanguage = "zh"
	}
	return cfg
}

// IfExtractFields 取字段抽取开关（Python config.extract_fields_from_response 消费点，
// nil ≙ True 默认）
func (c *QuestionerConfig) IfExtractFields() bool {
	if c.ExtractFieldsFromResponse == nil {
		return true
	}
	return *c.ExtractFieldsFromResponse
}

// QuestionerOutputCache 单轮输出缓存（Python OutputCache :160-163 一比一）
type QuestionerOutputCache struct {
	// UserResponse 用户回答（str 或 dict，默认 ""）
	UserResponse any
	// Question 当前问题
	Question string
	// KeyFields 本轮抽取键值（默认空 dict）
	KeyFields map[string]any
}

// newQuestionerOutputCache 新建缓存（Python dataclass 默认值：user_response=""/
// question=""/key_fields={}；UserResponse 起始为 "" 而非 nil，PyEqual 才能命中
// exclude_defaults 语义）
func newQuestionerOutputCache() *QuestionerOutputCache {
	return &QuestionerOutputCache{UserResponse: "", KeyFields: map[string]any{}}
}

// QuestionerOutput 节点输出（Python QuestionerOutput :166-169；key_fields 经
// extra=allow 展开为顶层键，见 questionerFormatQuestionerOutput）
type QuestionerOutput struct {
	// UserResponse 用户回答（str 或 dict，默认 ""）
	UserResponse any `json:"user_response"`
	// Question 问题（默认 ""）
	Question string `json:"question"`
	// Extra 抽取键值（Python extra=allow 顶层展开的等价承载）
	Extra map[string]any `json:"-"`
}

// questionerDefaultConfig 运行期派生默认配置（Python QuestionerDefaultConfig :146-152）
type questionerDefaultConfig struct {
	// promptTemplate 默认提示词模板（按 accept_language 双语）
	promptTemplate []llmschema.BaseMessage
}

// questionerDefaultConfigFromLanguage 按语言构造（Python from_language :150-152）
func questionerDefaultConfigFromLanguage(acceptLanguage string) *questionerDefaultConfig {
	return &questionerDefaultConfig{promptTemplate: questionerDefaultTemplate(acceptLanguage)}
}

// ──────────────────────────── 枚举 ────────────────────────────

// QuestionerExecutionStatus 执行状态（Python ExecutionStatus :104-107，值为字符串）
type QuestionerExecutionStatus string

const (
	// QuestionerStatusStart START 态（Python START = "start"）
	QuestionerStatusStart QuestionerExecutionStatus = questionerStartStr
	// QuestionerStatusUserInteract 交互态（Python USER_INTERACT = "user_interact"）
	QuestionerStatusUserInteract QuestionerExecutionStatus = questionerUserInteractStr
	// QuestionerStatusEnd 终态（Python END = "end"）
	QuestionerStatusEnd QuestionerExecutionStatus = questionerEndStr
)

// QuestionerEvent 事件（Python QuestionerEvent :110-113，与状态同值域）
type QuestionerEvent string

const (
	// QuestionerEventStart 事件：进入 START
	QuestionerEventStart QuestionerEvent = questionerStartStr
	// QuestionerEventUserInteract 事件：进入 USER_INTERACT
	QuestionerEventUserInteract QuestionerEvent = questionerUserInteractStr
	// QuestionerEventEnd 事件：进入 END
	QuestionerEventEnd QuestionerEvent = questionerEndStr
)

// ResponseTypeReplyDirectly 响应类型（Python ResponseType :116-118 单成员死枚举仍复刻）
const ResponseTypeReplyDirectly = "reply_directly"

// ──────────────────────────── 常量 ────────────────────────────

// Python :27-36 模块级常量一比一（死常量 workflow_chat_history/template_name 仍复刻）
const (
	questionerStartStr              = "start"
	questionerEndStr                = "end"
	questionerUserInteractStr       = "user_interact"
	questionerSubPlaceholderPattern = `\{\{([^}]*)\}\}`
	questionerContinueAskStatementZh = "请您提供{non_extracted_key_fields_names}相关的信息"
	questionerContinueAskStatementEn = "Please provide information related to: {non_extracted_key_fields_names}"
	questionerWorkflowChatHistory    = "workflow_chat_history"
	questionerTemplateName           = "questioner"
	questionerStateKey               = "questioner_state"
)

// 双语默认提示词模板——Python :38-88 逐字复制（提示词一比一纪律，禁止改写）
const (
	questionerSystemTemplateZh = `你是一个信息收集助手，你需要根据指定的参数收集用户的信息，然后提交到系统。
请注意：不要使用任何工具、不用理会问题的具体含义，并保证你的输出仅有 JSON 格式的结果数据。
请严格遵循如下规则：
  1. 让我们一步一步思考。
  2. 用户输入中没有提及的参数提取为 null，并直接向询问用户没有明确提供的参数。
  3. 通过用户提供的对话历史以及当前输入中提取 {{required_name}}，不要追问任何其他信息。
  4. 参数收集完成后，将收集到的信息通过 JSON 的方式展示给用户。

## Specified Parameters
{{required_params_list}}

## Constraints
{{extra_info}}

## Examples
{{example}}
`

	questionerUserTemplateZh = `对话历史
{{dialogue_history}}

请充分考虑以上对话历史及用户输入，正确提取最符合约束要求的 JSON 格式参数。
`

	questionerSystemTemplateEn = `You are an information collection assistant. You need to collect user information based on the specified parameters and submit it to the system.
Please note: Do not use any tools, do not consider the specific meaning of the questions, and ensure your output contains only JSON-formatted result data.
Strictly follow these rules:
  1. Let's think step by step.
  2. Parameters not mentioned in user input should be extracted as null, and directly ask the user for parameters not explicitly provided.
  3. Extract {{required_name}} from the conversation history and current user input. Do not ask for any other information.
  4. After parameter collection is complete, display the collected information in JSON format.

## Specified Parameters
{{required_params_list}}

## Constraints
{{extra_info}}

## Examples
{{example}}
`

	questionerUserTemplateEn = `Conversation History
{{dialogue_history}}

Please fully consider the above conversation history and user input, and correctly extract the JSON-formatted parameters that best meet the constraints.
`
)

// ──────────────────────────── 导出函数 ────────────────────────────

// QuestionerDefaultTemplate 按语言返回默认提示词模板（Python questioner_default_template
// :91-101 一比一：en 返回英文对，其余（含默认）返回中文对）
func QuestionerDefaultTemplate(acceptLanguage string) []llmschema.BaseMessage {
	if acceptLanguage == "en" {
		return []llmschema.BaseMessage{
			llmschema.NewSystemMessage(questionerSystemTemplateEn),
			llmschema.NewUserMessage(questionerUserTemplateEn),
		}
	}
	return []llmschema.BaseMessage{
		llmschema.NewSystemMessage(questionerSystemTemplateZh),
		llmschema.NewUserMessage(questionerUserTemplateZh),
	}
}

// questionerDefaultTemplate 内部别名（Python 模块级函数，供 defaultConfig 消费）
func questionerDefaultTemplate(acceptLanguage string) []llmschema.BaseMessage {
	return QuestionerDefaultTemplate(acceptLanguage)
}

// ──────────────────────────── Task 3 状态机 ────────────────────────────

// QuestionerState 组件状态（Python QuestionerState/Start/Interact/End :172-251；
// D-24-5 定案：Python 三个子类仅重写 handle_event 与默认 status、from_state 恒为
// 字段拷贝+置位，Go 以单结构体 status 分派逐行对照，序列化形状不变）
type QuestionerState struct {
	// ResponseNum 已收反馈轮数（每次 interact 反馈 +1）
	ResponseNum int `json:"response_num"`
	// UserResponse 用户回答（str/dict，默认 ""）
	UserResponse any `json:"user_response"`
	// Question 当前问题
	Question string `json:"question"`
	// ExtractedKeyFields 跨轮累积抽取结果
	ExtractedKeyFields map[string]any `json:"extracted_key_fields"`
	// Status 当前状态（字符串序列化 ≙ Python Enum.value）
	Status QuestionerExecutionStatus `json:"status"`
}

// NewQuestionerState 新建状态（Python __init__ 默认值；map 零值防 nil）
func NewQuestionerState() *QuestionerState {
	return &QuestionerState{
		ExtractedKeyFields: map[string]any{},
		Status:             QuestionerStatusStart,
	}
}

// HandleEvent 状态分派（Python 四类 handle_event :187-194/:212-217/:231-234/:248-251
// 合流；分支表逐条对照：Start→{Interact,End}；Interact→{End}；End→{Start=回环新建}）
func (s *QuestionerState) HandleEvent(event QuestionerEvent) *QuestionerState {
	switch s.Status {
	case QuestionerStatusStart:
		switch event {
		case QuestionerEventUserInteract:
			return s.copyWithStatus(QuestionerStatusUserInteract) // QuestionerInteractState.from_state
		case QuestionerEventEnd:
			return s.copyWithStatus(QuestionerStatusEnd) // QuestionerEndState.from_state
		}
		return s
	case QuestionerStatusUserInteract:
		if event == QuestionerEventEnd {
			return s.copyWithStatus(QuestionerStatusEnd)
		}
		return s
	case QuestionerStatusEnd:
		if event == QuestionerEventStart {
			return NewQuestionerState().HandleEvent(event) // loop back to START :250
		}
		return s
	}
	return s
}

// copyWithStatus from_state 等价（字段拷贝+置位）
func (s *QuestionerState) copyWithStatus(st QuestionerExecutionStatus) *QuestionerState {
	c := *s
	c.Status = st
	if c.ExtractedKeyFields == nil {
		c.ExtractedKeyFields = map[string]any{}
	}
	return &c
}

// IsUndergoingInteraction Python :196-197
func (s *QuestionerState) IsUndergoingInteraction() bool {
	return s.Status == QuestionerStatusUserInteract
}

// IsFreshState Python :199-200
func (s *QuestionerState) IsFreshState() bool {
	return s.Status == QuestionerStatusStart && s.ResponseNum == 0
}

// Serialize Python :184-185（model_dump ≙ JSON marshal 往返）
func (s *QuestionerState) Serialize() map[string]any {
	raw, _ := json.Marshal(s)
	m := map[string]any{}
	_ = json.Unmarshal(raw, &m)
	return m
}

// QuestionerStateDeserialize Python deserialize :179-182（validate + handle_event
// 按存量 status 恢复为对应子类形态；解析失败包装 101073）
func QuestionerStateDeserialize(raw map[string]any) (*QuestionerState, error) {
	b, _ := json.Marshal(raw)
	st := &QuestionerState{}
	if err := json.Unmarshal(b, st); err != nil {
		return nil, exception.BuildError(exception.StatusComponentQuestionerStateInitFailed,
			exception.WithMsg(fmt.Sprintf("failed to deserialize questioner state: %v", err)),
			exception.WithCause(err))
	}
	if st.ExtractedKeyFields == nil {
		st.ExtractedKeyFields = map[string]any{}
	}
	// Python deserialize 尾调 handle_event(QuestionerEvent(state.status.value))
	return st.HandleEvent(QuestionerEvent(st.Status)), nil
}

// ──────────────────────────── Task 4 QuestionerUtils ────────────────────────────

// questionerFormatTemplate 模板渲染（Python format_template :255-265 一比一）：
// {{key}} 占位符替换为 user_fields[key] 的 str()；D-24-1 缺键怪癖——Python
// user_fields.get(key) 缺失得 None → str(None)="None"，Go 用 pythonsem.PyStr(nil)
// 对齐；Python except (KeyError,TypeError,AttributeError) 返回 "" 的分支对应渲染
// 过程 panic 场景（Go 值访问不会 panic，保留等价防御）
func questionerFormatTemplate(template string, userFields map[string]any) string {
	re := regexp.MustCompile(questionerSubPlaceholderPattern)
	return re.ReplaceAllStringFunc(template, func(m string) string {
		sub := re.FindStringSubmatch(m)
		if len(sub) < 2 {
			return ""
		}
		return pythonsem.PyStr(pythonsem.MapValue(userFields, sub[1]))
	})
}

// questionerFormatContinueAsk 追问语句（Python format_continue_ask_question
// :267-277 一比一：cn_field_name or description 回落、", " 拼接、双语模板选择）
func questionerFormatContinueAsk(fields []QuestionerFieldInfo, acceptLanguage string) string {
	names := make([]string, 0, len(fields))
	for _, p := range fields {
		if p.CnFieldName != "" {
			names = append(names, p.CnFieldName)
		} else {
			names = append(names, p.Description)
		}
	}
	tpl := questionerContinueAskStatementZh
	if acceptLanguage == "en" {
		tpl = questionerContinueAskStatementEn
	}
	return strings.ReplaceAll(tpl, "{non_extracted_key_fields_names}", strings.Join(names, ", "))
}

// questionerFormatQuestionerOutput 输出构造（Python format_questioner_output
// :279-284 一比一）：QuestionerOutput(**key_fields) extra 展开 + user_response/
// question 赋值 + model_dump(exclude_defaults=True)——user_response=="" 与
// question=="" 剔除（默认值），extra 键保留
func questionerFormatQuestionerOutput(cache *QuestionerOutputCache) map[string]any {
	out := map[string]any{}
	for k, v := range cache.KeyFields {
		out[k] = v
	}
	if !pythonsem.PyEqual(cache.UserResponse, "") {
		out["user_response"] = cache.UserResponse
	}
	if cache.Question != "" {
		out["question"] = cache.Question
	}
	return out
}

// questionerValidateInputs 输入校验（Python validate_inputs :286-295 一比一）：
// query 允许 str/dict/None（D-24-3），缺失回落 ""；非法类型抛 101070
func questionerValidateInputs(inputs map[string]any) (map[string]any, error) {
	if inputs == nil {
		inputs = map[string]any{}
	}
	q, has := inputs["query"]
	if has && q != nil {
		switch q.(type) {
		case string, map[string]any:
		default:
			return nil, exception.BuildError(exception.StatusComponentQuestionerInputParamError,
				exception.WithMsg(fmt.Sprintf("query must be str or dict, got %T", q)))
		}
	} else if !has {
		inputs["query"] = ""
	}
	return inputs, nil
}

// questionerIsValidValue 有效值判定（Python is_valid_value :297-306 一比一）：
// None/""/{}/[]/"null"/"none"(strip+lower) 无效；false/0 是有效值（Python 怪癖，
// 后续 _update_state_of_key_fields 的 truthy 过滤才拦 falsy——D-24-4 非全 falsy）
func questionerIsValidValue(v any) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case string:
		if val == "" {
			return false
		}
		low := strings.ToLower(strings.TrimSpace(val))
		return low != "null" && low != "none"
	case map[string]any:
		return len(val) > 0
	case []any:
		return len(val) > 0
	}
	return true
}

// questionerValidateAndConvertType 四类类型转换（Python :309-377；D-24-2 拍板②
// 落 pythonsem.ValidateAndConvertType，包内禁止私抄）
func questionerValidateAndConvertType(value any, expectedType string) (any, bool) {
	return pythonsem.ValidateAndConvertType(value, expectedType)
}

// ──────────────────────────── Task 5 QuestionerExecutable ────────────────────────────

// QuestionerExecutable 执行体（Python QuestionerExecutable :744-878 字段一比一）
type QuestionerExecutable struct {
	// WorkflowComponent 8.17 组件基座（InvokeFn 槽承载 invoke）
	components.WorkflowComponent
	// cfg 配置
	cfg *QuestionerConfig
	// defaultCfg 运行期默认配置（双语模板）
	defaultCfg *questionerDefaultConfig
	// llModel 懒初始化模型实例（Python self._llm）
	llModel LLMInvoker
	// initialized 懒初始化标志
	initialized bool
	// promptTemplate 提示词模板（Python self._prompt）
	promptTemplate *prompt.PromptTemplate
	// state 处理器传递态（Python self._state）
	state *QuestionerState
}

// NewQuestionerExecutable 构造（Python __init__ :745-753 一比一：校验配置 →
// 默认配置 → prompt 初始化）
func NewQuestionerExecutable(cfg *QuestionerConfig) (*QuestionerExecutable, error) {
	cfg = newQuestionerConfigDefaults(cfg)
	if err := validateQuestionerResponseType(cfg.ResponseType); err != nil {
		return nil, err
	}
	if err := validateQuestionerExtractKeyFields(cfg.IfExtractFields(), cfg.FieldNames); err != nil {
		return nil, err
	}
	if err := validateQuestionerMaxResponse(cfg.MaxResponse); err != nil {
		return nil, err
	}
	return &QuestionerExecutable{
		cfg:            cfg,
		defaultCfg:     questionerDefaultConfigFromLanguage(cfg.AcceptLanguage),
		promptTemplate: prompt.NewPromptTemplate("", questionerDefaultTemplate(cfg.AcceptLanguage)),
	}, nil
}

// validateQuestionerResponseType Python _validate_response_type_config :790-797
func validateQuestionerResponseType(rt string) error {
	if rt != ResponseTypeReplyDirectly {
		return exception.BuildError(exception.StatusComponentQuestionerConfigError,
			exception.WithMsg(fmt.Sprintf("response type %s is invalid", rt)))
	}
	return nil
}

// validateQuestionerExtractKeyFields Python _validate_extract_key_fields_config :776-788
func validateQuestionerExtractKeyFields(ifExtract bool, fields []QuestionerFieldInfo) error {
	if ifExtract && len(fields) == 0 {
		return exception.BuildError(exception.StatusComponentQuestionerConfigError,
			exception.WithMsg("extracted key fields cannot be empty"))
	}
	for _, item := range fields {
		if item.FieldName == "" {
			return exception.BuildError(exception.StatusComponentQuestionerConfigError,
				exception.WithMsg("extracted key field name cannot be empty"))
		}
	}
	return nil
}

// validateQuestionerMaxResponse Python _validate_max_response_num_config :768-774
func validateQuestionerMaxResponse(n int) error {
	if n <= 0 {
		return exception.BuildError(exception.StatusComponentQuestionerConfigError,
			exception.WithMsg("max response must be greater than 0"))
	}
	return nil
}

// loadQuestionerStateFromSession Python _load_state_from_session :755-761：
// 状态字典存在则反序列化恢复；否则新建 START 态
func loadQuestionerStateFromSession(sess *session.NodeSessionFacade) *QuestionerState {
	raw, err := sess.GetState(state.StringKey(questionerStateKey))
	if err == nil {
		if m, ok := raw.(map[string]any); ok && len(m) > 0 {
			if st, derr := QuestionerStateDeserialize(m); derr == nil {
				return st
			}
		}
	}
	return NewQuestionerState()
}

// storeQuestionerStateToSession Python _store_state_to_session :763-766
func storeQuestionerStateToSession(st *QuestionerState, sess *session.NodeSessionFacade) {
	sess.UpdateState(map[string]any{questionerStateKey: st.Serialize()})
}

// createLLMInstance 双路径模型创建（Python _create_llm_instance :831-841 一比一；
// 8.23 D-23-3 同款防御分支：mgr==nil / GetModel 未命中；皆缺 → 101075）
func (e *QuestionerExecutable) createLLMInstance(ctx context.Context) (LLMInvoker, error) {
	if e.cfg.ModelID == nil {
		if e.cfg.ModelClientConfig == nil || e.cfg.ModelConfig == nil {
			return nil, exception.BuildError(exception.StatusComponentQuestionerInvokeCallFailed,
				exception.WithMsg("failed to create llm instance"))
		}
		return forllm.NewModel(e.cfg.ModelClientConfig, e.cfg.ModelConfig)
	}
	mgr := runner.GetResourceMgr()
	if mgr == nil {
		return nil, fmt.Errorf("resource mgr is not initialized")
	}
	clients, err := mgr.GetModel(ctx, []string{*e.cfg.ModelID})
	if err != nil {
		return nil, err
	}
	if len(clients) == 0 {
		return nil, fmt.Errorf("model %s not found", *e.cfg.ModelID)
	}
	return clients[0], nil
}

// initializeIfNeeded 懒初始化（Python _initialize_if_needed :843-853 一比一；
// 异常包装 101075）
func (e *QuestionerExecutable) initializeIfNeeded(ctx context.Context) error {
	if e.initialized {
		return nil
	}
	model, err := e.createLLMInstance(ctx)
	if err != nil {
		return exception.BuildError(exception.StatusComponentQuestionerInvokeCallFailed,
			exception.WithMsg("failed to initialize llm if needed"), exception.WithCause(err))
	}
	e.llModel = model
	e.initialized = true
	return nil
}

// initPrompt 初始化提示词模板（Python _init_prompt :855-856）
func (e *QuestionerExecutable) initPrompt() *prompt.PromptTemplate {
	return prompt.NewPromptTemplate("", e.defaultCfg.promptTemplate)
}

// ──────────────────────────── Task 6 DirectReplyHandler ────────────────────────────

// questionerDirectReplyHandler 直接回复处理器（Python QuestionerDirectReplyHandler
// :380-741 字段一比一）
type questionerDirectReplyHandler struct {
	// cfg 配置（Python self._config）
	cfg *QuestionerConfig
	// llModel 模型实例（Python self._model）
	llModel LLMInvoker
	// state 当前状态（Python self._state）
	state *QuestionerState
	// promptT 提示词模板（Python self._prompt）
	promptT *prompt.PromptTemplate
	// query 当前输入（Python self._query，str 或 dict）
	query any
}

// newQuestionerDirectReplyHandler 构造（Python __init__ :381-386 默认值 + 链式
// 注入 config/model/state/prompt 的等价合并；get_state 经 handler.state 直读——D-24-8）
func newQuestionerDirectReplyHandler(cfg *QuestionerConfig, llModel LLMInvoker,
	st *QuestionerState, promptT *prompt.PromptTemplate) *questionerDirectReplyHandler {
	return &questionerDirectReplyHandler{cfg: cfg, llModel: llModel, state: st, promptT: promptT}
}

// handle 状态分发（Python handle :407-414 一比一）
func (h *questionerDirectReplyHandler) handle(ctx context.Context, inputs map[string]any,
	sess *session.NodeSessionFacade, context iface.ModelContext) (map[string]any, error) {
	switch h.state.Status {
	case QuestionerStatusStart:
		return h.handleStartState(ctx, inputs, sess, context)
	case QuestionerStatusUserInteract:
		return h.handleUserInteractState(ctx, inputs, sess, context)
	case QuestionerStatusEnd:
		return h.handleEndState(), nil
	}
	return map[string]any{}, nil
}

// handleStartState START 态处理（Python :416-453 十步一比一）
func (h *questionerDirectReplyHandler) handleStartState(ctx context.Context, inputs map[string]any,
	sess *session.NodeSessionFacade, context iface.ModelContext) (map[string]any, error) {
	// 1. 输入校验
	questionerInput, err := questionerValidateInputs(inputs)
	if err != nil {
		return nil, err
	}
	output := newQuestionerOutputCache()
	// 2. query 取值（str/dict 两态；None/缺省回落 ""）
	h.query = h.extractQuery(questionerInput)
	// 3. START 态写 user 消息
	if err := h.writeUserMessageToContext(ctx, h.query, context); err != nil {
		return nil, err
	}
	// 4. 取对话历史
	chatHistory, err := h.getLatestChatHistory(ctx, context)
	if err != nil {
		return nil, err
	}
	// 5a. question_content 分支：模板渲染发问 → USER_INTERACT
	if h.isSetQuestionContent() {
		userFields := h.inputFieldsExcludeQuery(questionerInput)
		output.Question = questionerFormatTemplate(h.cfg.QuestionContent, userFields)
		h.updateQuestionerStatesQuestion(output.Question)
		h.state = h.state.HandleEvent(QuestionerEventUserInteract)
		// 6. 写 assistant 消息（问题）
		if err := h.writeAssistantMessageToContext(ctx, output.Question, context); err != nil {
			return nil, err
		}
		return questionerFormatQuestionerOutput(output), nil
	}
	// 5b. 字段抽取分支
	if h.needExtractFields() {
		isContinueAsk, cerr := h.initialExtractFromChatHistory(ctx, chatHistory, output)
		if cerr != nil {
			return nil, cerr
		}
		event := QuestionerEventEnd
		if isContinueAsk {
			event = QuestionerEventUserInteract
			h.updateQuestionerStatesQuestion(output.Question)
			// 写 assistant 消息（追问）
			if err := h.writeAssistantMessageToContext(ctx, output.Question, context); err != nil {
				return nil, err
			}
		} else {
			// 写 assistant 消息（抽取结果 JSON）
			if err := h.writeAssistantMessageToContext(ctx, h.jsonDump(output.KeyFields), context); err != nil {
				return nil, err
			}
		}
		h.state = h.state.HandleEvent(event)
	} else {
		// question_content 为空且无抽取字段 → 101072
		return nil, exception.BuildError(exception.StatusComponentQuestionerInputInvalid,
			exception.WithMsg("question_content is empty and no extractable fields are configured"))
	}
	return questionerFormatQuestionerOutput(output), nil
}

// handleUserInteractState USER_INTERACT 态处理（Python :455-488 一比一）
func (h *questionerDirectReplyHandler) handleUserInteractState(ctx context.Context, inputs map[string]any,
	sess *session.NodeSessionFacade, context iface.ModelContext) (map[string]any, error) {
	// 1. 取最新用户反馈（重放语义）
	if err := h.getLatestHumanFeedback(ctx, sess); err != nil {
		return nil, err
	}
	output := newQuestionerOutputCache()
	output.Question = h.state.Question
	output.UserResponse = h.query
	// 2. 写 user 反馈消息
	if err := h.writeUserMessageToContext(ctx, h.query, context); err != nil {
		return nil, err
	}
	// 3. 取历史
	chatHistory, err := h.getLatestChatHistory(ctx, context)
	if err != nil {
		return nil, err
	}
	userResponse := ""
	if len(chatHistory) > 0 {
		userResponse = chatHistory[len(chatHistory)-1].GetContent().String()
	}
	// 4a. question_content 且无抽取 → 直接 END
	if h.isSetQuestionContent() && !h.needExtractFields() {
		output.UserResponse = userResponse
		h.state = h.state.HandleEvent(QuestionerEventEnd)
		return questionerFormatQuestionerOutput(output), nil
	}
	// 4b. 字段抽取分支
	if h.needExtractFields() {
		isContinueAsk, cerr := h.repeatExtractFromChatHistory(ctx, chatHistory, output)
		if cerr != nil {
			return nil, cerr
		}
		event := QuestionerEventEnd
		if isContinueAsk {
			event = QuestionerEventUserInteract
			h.updateQuestionerStatesQuestion(output.Question)
			if err := h.writeAssistantMessageToContext(ctx, output.Question, context); err != nil {
				return nil, err
			}
		} else {
			if err := h.writeAssistantMessageToContext(ctx, h.jsonDump(output.KeyFields), context); err != nil {
				return nil, err
			}
		}
		h.state = h.state.HandleEvent(event)
	} else {
		return nil, exception.BuildError(exception.StatusComponentQuestionerInputInvalid,
			exception.WithMsg("question_content is empty and no extractable fields are configured"))
	}
	return questionerFormatQuestionerOutput(output), nil
}

// handleEndState END 态处理（Python _handle_end_state :490-494 一比一）
func (h *questionerDirectReplyHandler) handleEndState() map[string]any {
	out := map[string]any{}
	for k, v := range h.state.ExtractedKeyFields {
		out[k] = v
	}
	if !pythonsem.PyEqual(h.state.UserResponse, "") {
		out["user_response"] = h.state.UserResponse
	}
	if h.state.Question != "" {
		out["question"] = h.state.Question
	}
	return out
}

// isSetQuestionContent Python :496-497
func (h *questionerDirectReplyHandler) isSetQuestionContent() bool {
	return len(h.cfg.QuestionContent) > 0
}

// needExtractFields Python :499-501（字段数 > 已抽取数）
func (h *questionerDirectReplyHandler) needExtractFields() bool {
	return h.cfg.IfExtractFields() && len(h.cfg.FieldNames) > len(h.state.ExtractedKeyFields)
}

// initialExtractFromChatHistory Python :503-509
func (h *questionerDirectReplyHandler) initialExtractFromChatHistory(ctx context.Context,
	chatHistory []llmschema.BaseMessage, output *QuestionerOutputCache) (bool, error) {
	if err := h.invokeLLMAndParseResult(ctx, chatHistory, output); err != nil {
		return false, err
	}
	h.updateParamDefaultValue(output)
	h.updateStateOfKeyFields(output.KeyFields)
	return h.checkIfContinueAsk(output), nil
}

// repeatExtractFromChatHistory Python :511-517（与 initial 体相同，按 Python 保持两方法）
func (h *questionerDirectReplyHandler) repeatExtractFromChatHistory(ctx context.Context,
	chatHistory []llmschema.BaseMessage, output *QuestionerOutputCache) (bool, error) {
	if err := h.invokeLLMAndParseResult(ctx, chatHistory, output); err != nil {
		return false, err
	}
	h.updateParamDefaultValue(output)
	h.updateStateOfKeyFields(output.KeyFields)
	return h.checkIfContinueAsk(output), nil
}

// getLatestChatHistory Python _get_latest_chat_history :519-532 一比一：
// with_chat_history 且 context 非 nil → GetContextWindow(dialogue_round)；历史为空
// 或末条 assistant → 追加 UserMessage(query)（dict query 包 [dict]，Python content
// 为 List[Union[str, Dict]] 承载）
func (h *questionerDirectReplyHandler) getLatestChatHistory(ctx context.Context,
	context iface.ModelContext) ([]llmschema.BaseMessage, error) {
	var result []llmschema.BaseMessage
	if h.cfg.WithChatHistory && context != nil {
		window, err := context.GetContextWindow(ctx, nil, nil, 0, h.cfg.ChatHistoryMaxRounds)
		if err != nil {
			return nil, err
		}
		if window != nil {
			result = window.ContextMessages
		}
	}
	if len(result) == 0 || result[len(result)-1].GetRole() == llmschema.RoleTypeAssistant {
		result = append(result, llmschema.NewUserMessage(h.queryString()))
	}
	return result, nil
}

// buildLLMInputs 组装 LLM 输入（Python _build_llm_inputs :534-537）
func (h *questionerDirectReplyHandler) buildLLMInputs(chatHistory []llmschema.BaseMessage) ([]llmschema.BaseMessage, error) {
	templateInput := h.createPromptTemplateKeywords(chatHistory)
	formatted, err := h.promptT.Format(templateInput)
	if err != nil {
		return nil, err
	}
	return formatted.ToMessages()
}

// createPromptTemplateKeywords 模板关键字（Python :539-555 一比一：params_list
// 逐行 "field: desc"；required_name 中英分隔符 "、"/", " 与数量后缀；dialogue_history
// 逐条 "role: content" 行拼接）
func (h *questionerDirectReplyHandler) createPromptTemplateKeywords(chatHistory []llmschema.BaseMessage) map[string]any {
	paramsList := make([]string, 0, len(h.cfg.FieldNames))
	requiredNameList := make([]string, 0, len(h.cfg.FieldNames))
	for _, param := range h.cfg.FieldNames {
		paramsList = append(paramsList, fmt.Sprintf("%s: %s", param.FieldName, param.Description))
		if param.Required {
			if param.CnFieldName != "" {
				requiredNameList = append(requiredNameList, param.CnFieldName)
			} else {
				requiredNameList = append(requiredNameList, param.Description)
			}
		}
	}
	lang := h.cfg.AcceptLanguage
	var requiredNameStr, dialogueHistoryStr string
	if lang == "en" {
		requiredNameStr = strings.Join(requiredNameList, ", ") +
			fmt.Sprintf(" (%d required field(s))", len(requiredNameList))
		parts := make([]string, 0, len(chatHistory))
		for _, m := range chatHistory {
			parts = append(parts, fmt.Sprintf("%s: %s", m.GetRole(), m.GetContent().String()))
		}
		dialogueHistoryStr = strings.Join(parts, "\n")
	} else {
		requiredNameStr = strings.Join(requiredNameList, "、") +
			fmt.Sprintf("%d个必要信息", len(requiredNameList))
		parts := make([]string, 0, len(chatHistory))
		for _, m := range chatHistory {
			parts = append(parts, fmt.Sprintf("%s：", m.GetRole())+m.GetContent().String())
		}
		dialogueHistoryStr = strings.Join(parts, "\n")
	}
	return map[string]any{
		"required_name":       requiredNameStr,
		"required_params_list": strings.Join(paramsList, "\n"),
		"extra_info":          h.cfg.ExtraPromptForFieldsExtraction,
		"example":             h.cfg.ExampleContent,
		"dialogue_history":    dialogueHistoryStr,
	}
}

// invokeLLMForExtraction LLM 抽取与解析（Python :557-614 一比一）
func (h *questionerDirectReplyHandler) invokeLLMForExtraction(ctx context.Context,
	llmInputs []llmschema.BaseMessage) (map[string]any, error) {
	response := ""
	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "WORKFLOW_COMPONENT_START").
		Str("component_id", "QuestionerComponent").
		Str("component_type_str", "QuestionerComponent").
		Bool("has_inputs", len(llmInputs) > 0).
		Int("input_count", len(llmInputs)).
		Bool("sensitive_mode", security.IsSensitive()).
		Msg("Questioner LLM extraction started")

	resp, invokeErr := h.llModel.Invoke(ctx, modelclients.NewMessagesParam(llmInputs...))
	if invokeErr != nil {
		return nil, exception.BuildError(exception.StatusComponentQuestionerInvokeCallFailed,
			exception.WithMsg("failed to invoke llm for extraction"), exception.WithCause(invokeErr))
	}
	if resp != nil {
		response = resp.GetContent().String()
	}

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "WORKFLOW_COMPONENT_END").
		Str("component_id", "QuestionerComponent").
		Str("component_type_str", "QuestionerComponent").
		Bool("has_response", response != "").
		Int("response_length", len(response)).
		Bool("sensitive_mode", security.IsSensitive()).
		Msg("Questioner LLM extraction completed")

	result := map[string]any{}
	// 剥壳两正则（Python :593-594，flags=IGNORECASE）：```json 与 '''json
	cleaned := response
	cleaned = strings.TrimSpace(cleaned)
	jsonFence := regexp.MustCompile("(?i)^\\s*```json\\s*|\\s*```\\s*$")
	cleaned = jsonFence.ReplaceAllString(cleaned, "")
	squoteFence := regexp.MustCompile("(?i)^\\s*'''json\\s*|\\s*'''\\s*$")
	cleaned = squoteFence.ReplaceAllString(cleaned, "")
	if err := json.Unmarshal([]byte(strings.TrimSpace(cleaned)), &result); err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "WORKFLOW_COMPONENT_ERROR").
			Str("component_id", "QuestionerComponent").
			Str("component_type_str", "QuestionerComponent").
			Str("error", "Failed to parse JSON from LLM response").
			Msg("Questioner JSON parse failed")
		return result, nil // Python :596-603 解析失败返空 dict 不报错
	}
	// 非 dict：json.Unmarshal 到 map 已保证；数值化校验（Python :605-609 不可达分支
	// 由 Unmarshal 目标类型承载）
	// JSON 数字归一化（D-24-11）：Python json.loads 整数字面量→int、带小数→float；
	// Go 全为 float64，整值归一 int64 对齐主流 LLM 输出（42→"42" 而非 "42.0"；
	// 42.0 亦归一为 int，与 Python float(42.0) 偏差，登记 spec）
	for k, v := range result {
		if f, ok := v.(float64); ok && f == float64(int64(f)) {
			result[k] = int64(f)
		}
	}
	// is_valid_value 过滤（Python :610）
	filtered := map[string]any{}
	for k, v := range result {
		if questionerIsValidValue(v) {
			filtered[k] = v
		}
	}
	// 类型校验转换（Python :612-614）
	return h.validateAndConvertFields(filtered), nil
}

// validateAndConvertFields 字段类型校验转换（Python :616-654 一比一：field_type_map
// 缺键回落 "string"；失败剔除 + warning log 含 field_name/value(敏感置 nil)/expected_type）
func (h *questionerDirectReplyHandler) validateAndConvertFields(extracted map[string]any) map[string]any {
	fieldTypeMap := map[string]string{}
	for _, f := range h.cfg.FieldNames {
		fieldTypeMap[f.FieldName] = f.Type
	}
	validated := map[string]any{}
	for fieldName, value := range extracted {
		expectedType := fieldTypeMap[fieldName]
		if expectedType == "" {
			expectedType = "string"
		}
		converted, ok := questionerValidateAndConvertType(value, expectedType)
		if ok {
			validated[fieldName] = converted
		} else {
			logger.Warn(logger.ComponentAgentCore).
				Str("event_type", "WORKFLOW_COMPONENT_ERROR").
				Str("component_id", "QuestionerComponent").
				Str("component_type_str", "QuestionerComponent").
				Str("field_name", fieldName).
				Str("value", questionerSensitiveValueRepr(value)).
				Str("expected_type", expectedType).
				Msg("Questioner field validation failed")
		}
	}
	return validated
}

// questionerSensitiveValueRepr 敏感模式下的值脱敏（Python :649 `str(value) if not
// UserConfig.is_sensitive() else None`——敏感时 None ≙ 空串占位）
func questionerSensitiveValueRepr(v any) string {
	if security.IsSensitive() {
		return ""
	}
	return pythonsem.PyStr(v)
}

// filterNonExtractedKeyFields 缺失必填字段（Python :656-661）
func (h *questionerDirectReplyHandler) filterNonExtractedKeyFields() []QuestionerFieldInfo {
	var result []QuestionerFieldInfo
	for _, item := range h.cfg.FieldNames {
		if item.Required {
			if _, ok := h.state.ExtractedKeyFields[item.FieldName]; !ok {
				result = append(result, item)
			}
		}
	}
	return result
}

// updateStateOfKeyFields Python :663-666（truthy 才进——Python `if v:`）
func (h *questionerDirectReplyHandler) updateStateOfKeyFields(keyFields map[string]any) {
	for k, v := range keyFields {
		if !pyFalsy(v) {
			h.state.ExtractedKeyFields[k] = v
		}
	}
}

// updateParamDefaultValue 默认值兜底（Python :668-676：default_value 非零值
// 且未抽到才兜底——Python `if default_value:` truthy 判定）
func (h *questionerDirectReplyHandler) updateParamDefaultValue(output *QuestionerOutputCache) {
	result := map[string]any{}
	for _, param := range h.cfg.FieldNames {
		if !pyFalsy(param.DefaultValue) {
			if _, ok := h.state.ExtractedKeyFields[param.FieldName]; !ok {
				result[param.FieldName] = param.DefaultValue
			}
		}
	}
	for k, v := range result {
		output.KeyFields[k] = v
	}
}

// incrementStateOfResponseNum Python :678-679
func (h *questionerDirectReplyHandler) incrementStateOfResponseNum() {
	h.state.ResponseNum++
}

// exceedMaxResponse Python :681-682
func (h *questionerDirectReplyHandler) exceedMaxResponse() bool {
	return h.state.ResponseNum >= h.cfg.MaxResponse
}

// checkIfContinueAsk 继续追问判定（Python :684-702 一比一：缺必填+未超限 →
// 追问+key_fields.clear；缺必填+超限 → 101074；齐 → key_fields.update(state)）
func (h *questionerDirectReplyHandler) checkIfContinueAsk(output *QuestionerOutputCache) bool {
	isContinueAsk := false
	nonExtracted := h.filterNonExtractedKeyFields()
	if len(nonExtracted) > 0 {
		if !h.exceedMaxResponse() {
			output.Question = questionerFormatContinueAsk(nonExtracted, h.cfg.AcceptLanguage)
			isContinueAsk = true
		} else {
			return false
		}
	}
	if isContinueAsk {
		output.KeyFields = map[string]any{}
	} else {
		for k, v := range h.state.ExtractedKeyFields {
			output.KeyFields[k] = v
		}
	}
	return isContinueAsk
}

// invokeLLMAndParseResult LLM 调用与结果合并（Python :704-711）
func (h *questionerDirectReplyHandler) invokeLLMAndParseResult(ctx context.Context,
	chatHistory []llmschema.BaseMessage, output *QuestionerOutputCache) error {
	llmInputs, err := h.buildLLMInputs(chatHistory)
	if err != nil {
		return err
	}
	extracted, err := h.invokeLLMForExtraction(ctx, llmInputs)
	if err != nil {
		return err
	}
	for k, v := range extracted {
		if !pyFalsy(v) {
			output.KeyFields[k] = v
		}
	}
	h.updateStateOfKeyFields(extracted)
	return nil
}

// getLatestHumanFeedback 反馈重放（Python :713-716 一比一：循环 response_num+1 次
// interact——恢复后重放已收集反馈、最后一轮取新反馈；尾 increment。InteractWithResult
// 首次无待用输入时 PanicGraphInterrupt 向上传播 ≙ Python raise GraphInterrupt）
func (h *questionerDirectReplyHandler) getLatestHumanFeedback(ctx context.Context,
	sess *session.NodeSessionFacade) error {
	for i := 0; i < h.state.ResponseNum+1; i++ {
		res, err := sess.InteractWithResult(ctx, h.state.Question) // keep the last question
		if err != nil {
			return err
		}
		h.query = res
	}
	h.incrementStateOfResponseNum()
	return nil
}

// updateQuestionerStatesQuestion Python :718-719
func (h *questionerDirectReplyHandler) updateQuestionerStatesQuestion(question string) {
	h.state.Question = question
}

// writeUserMessageToContext Python :721-730（with_chat_history 且 content 非空才写）
func (h *questionerDirectReplyHandler) writeUserMessageToContext(ctx context.Context,
	content any, context iface.ModelContext) error {
	if context == nil || !h.cfg.WithChatHistory {
		return nil
	}
	if content == nil || pythonsem.PyEqual(content, "") {
		return nil
	}
	msg := llmschema.NewUserMessage(pythonsem.PyStr(content))
	_, err := context.AddMessages(ctx, []llmschema.BaseMessage{msg})
	return err
}

// writeAssistantMessageToContext Python :732-741
func (h *questionerDirectReplyHandler) writeAssistantMessageToContext(ctx context.Context,
	content string, context iface.ModelContext) error {
	if context == nil || !h.cfg.WithChatHistory {
		return nil
	}
	if content == "" {
		return nil
	}
	msg := llmschema.NewAssistantMessage(content)
	_, err := context.AddMessages(ctx, []llmschema.BaseMessage{msg})
	return err
}

// extractQuery query 三态提取（Python :419 `questioner_input.query or ""`：
// str/dict 保留，None/"" 回落 ""——D-24-3）
func (h *questionerDirectReplyHandler) extractQuery(inputs map[string]any) any {
	q, ok := inputs["query"]
	if !ok || q == nil {
		return ""
	}
	if s, isStr := q.(string); isStr && s == "" {
		return ""
	}
	return q
}

// queryString query 的字符串形态（Python content 直接用 str；dict 场景 Python
// 包 [dict] 作 content，Go MessageContent 暂以 str 承载——D-24-10 登记）
func (h *questionerDirectReplyHandler) queryString() string {
	return pythonsem.PyStr(h.query)
}

// inputFieldsExcludeQuery 输入除 query 外字段（Python :426 model_dump(exclude={'query'})）
func (h *questionerDirectReplyHandler) inputFieldsExcludeQuery(inputs map[string]any) map[string]any {
	fields := map[string]any{}
	for k, v := range inputs {
		if k != "query" {
			fields[k] = v
		}
	}
	return fields
}

// jsonDump JSON 序列化（Python json.dumps(ensure_ascii=False)）
func (h *questionerDirectReplyHandler) jsonDump(v map[string]any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// 以下为 Task 3-7 渐进实现区（状态机/Utils/Handler/Executable/Component）
