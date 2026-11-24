// 8.24 QuestionerComp——交互提问组件（Python questioner_comp.py 一比一）。
// 多轮交互参数收集：START 直发问/LLM 抽取 → USER_INTERACT 挂起恢复循环 → END 清状态重入。
// 中断经 session.interact（PanicGraphInterrupt，5.7 已回填）；状态 questioner_state 存节点会话。
// 已知偏差见 spec docs/superpowers/specs/2026-11-24-questioner-comp-8.24-design.md §5。
package llm

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/pythonsem"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
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
// question=""/key_fields={}）
func newQuestionerOutputCache() *QuestionerOutputCache {
	return &QuestionerOutputCache{KeyFields: map[string]any{}}
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

// 以下为 Task 3-7 渐进实现区（状态机/Utils/Handler/Executable/Component）
