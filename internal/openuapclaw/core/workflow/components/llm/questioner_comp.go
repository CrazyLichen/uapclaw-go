// 8.24 QuestionerComp——交互提问组件（Python questioner_comp.py 一比一）。
// 多轮交互参数收集：START 直发问/LLM 抽取 → USER_INTERACT 挂起恢复循环 → END 清状态重入。
// 中断经 session.interact（PanicGraphInterrupt，5.7 已回填）；状态 questioner_state 存节点会话。
// 已知偏差见 spec docs/superpowers/specs/2026-11-24-questioner-comp-8.24-design.md §5。
package llm

import (
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

// 以下为 Task 3-7 渐进实现区（状态机/Utils/Handler/Executable/Component）
