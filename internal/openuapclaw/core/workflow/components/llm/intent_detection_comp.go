// 8.23 IntentDetectionComp——意图识别组件（Python intent_detection_comp.py 一比一）。
// 组件自带 BranchRouter 路由；解析失败三类回落默认分类；历史读写走 ModelContext。
// 已知偏差见 spec docs/superpowers/specs/2026-11-23-intent-detection-comp-8.23-design.md §6。
package llm

import (
	"strconv"

	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/prompt"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/flow"
)

// ──────────────────────────── 结构体 ────────────────────────────

// IntentDetectionCompConfig 意图识别组件配置（Python IntentDetectionCompConfig :152-162 一比一）
type IntentDetectionCompConfig struct {
	// ModelID 预注册模型 ID（Python model_id；nil ≙ None）
	ModelID *string
	// ModelClientConfig 模型客户端配置
	ModelClientConfig *llmschema.ModelClientConfig
	// ModelConfig 模型请求配置
	ModelConfig *llmschema.ModelRequestConfig
	// CategoryNameList 分类名列表（用户可见分支名，顺序即分类号顺序）
	CategoryNameList []string
	// UserPrompt 用户提示词（业务语境描述，模板 {{user_prompt}} 槽）
	UserPrompt string
	// ExampleContent few-shot 示例列表（模板 {{example_content}} 槽，"\n\n" 拼接）
	ExampleContent []string
	// EnableHistory 是否启用会话历史（默认 false）
	EnableHistory bool
	// ChatHistoryMaxTurn 历史最大轮数（默认 3）
	ChatHistoryMaxTurn int
	// AcceptLanguage 语言（'zh'|'en'，默认 'zh'；贯穿模板/角色名/默认分类/失败文案）
	AcceptLanguage string
}

// IntentDetectionDefaultConfig 运行期派生默认配置（Python IntentDetectionDefaultConfig :165-171；
// default_class 由 accept_language 派生：分类0 / Category0）
type IntentDetectionDefaultConfig struct {
	// CategoryList 内部分类表（构造期派生 分类N/CategoryN + 默认分类头部追加）
	CategoryList []string
	// IntentDetectionTemplate 默认意图识别模板（双语，GetDefaultTemplate 产物）
	IntentDetectionTemplate *prompt.PromptTemplate
	// DefaultClass 默认分类名
	DefaultClass string
	// EnableInput 是否启用当前输入（恒 true，Python 默认值）
	EnableInput bool
}

// IntentDetectionInput 节点输入（Python :173-175；query 必填 + extra allow）
type IntentDetectionInput struct {
	// Query 当前用户输入
	Query string
}

// IntentDetectionOutput 节点输出（Python :178-182；exclude_defaults 语义见 buildOutputMap）
type IntentDetectionOutput struct {
	// ClassificationID 分类索引（Python 默认 -1）
	ClassificationID int
	// Reason 选择理由
	Reason string
	// CategoryName 分类名
	CategoryName string
}

// IntentDetectionExecutable 意图识别执行体（Python :184-194 字段一比一）
type IntentDetectionExecutable struct {
	// WorkflowComponent 8.17 组件基座（InvokeFn 槽承载 invoke）
	components.WorkflowComponent
	// session 当前会话（Python self._session，Union[Session, None]）
	session *session.NodeSessionFacade
	// llModel 懒初始化模型实例（Python self._llm，Union[Model, None]）
	llModel LLMInvoker
	// initialized 懒初始化标记（Python self._initialized）
	initialized bool
	// cfg 组件配置（Python self._config）
	cfg *IntentDetectionCompConfig
	// defaultCfg 运行期派生默认配置（Python self._default_config）
	defaultCfg *IntentDetectionDefaultConfig
	// router 分类路由器（Python self._router，Union[BranchRouter, None]）
	router *flow.BranchRouter
}

// IntentDetectionComponent 意图识别组件组合面（Python :463-468；本 Task 先占位骨架，
// 组合面方法 Task 6 落地）
type IntentDetectionComponent struct {
	// BaseComponentComposable 组合面基座（executable 懒初始化由 ToExecutableFn 槽承载）
	components.BaseComponentComposable
	// executable 缓存产物（Python self._executable）
	executable *IntentDetectionExecutable
	// cfg 组件配置（Python self._config）
	cfg *IntentDetectionCompConfig
	// router 分类路由器（Python self._router）
	router *flow.BranchRouter
}

// ──────────────────────────── 常量 ────────────────────────────

// 模块键名常量（Python intent_detection_comp.py :29-82 逐条转 lowerCamel；
// 定义未消费的死常量照搬不删——spec §2）。
const (
	// lui Python LUI（死常量）
	lui = "llm"
	// nameKey Python NAME（死常量）
	nameKey = "name"
	// modelKey Python MODEL（死常量）
	modelKey = "model"
	// classKey class 字段键
	classKey = "class"
	// reasonKey reason 字段键
	reasonKey = "reason"
	// inputKey input 槽键
	inputKey = "input"
	// userPromptKey 用户提示词槽键
	userPromptKey = "user_prompt"
	// categoryInfoKey 分类信息槽键
	categoryInfoKey = "category_info"
	// categoryListKey 分类列表槽键（死常量）
	categoryListKey = "category_list"
	// categoryNameListKey 分类名列表槽键（死常量）
	categoryNameListKey = "category_name_list"
	// defaultClassKey 默认分类槽键
	defaultClassKey = "default_class"
	// chatHistoryKey 对话历史槽键
	chatHistoryKey = "chat_history"
	// exampleContentKey 示例内容槽键
	exampleContentKey = "example_content"
	// enableHistoryKey 历史开关槽键（死常量）
	enableHistoryKey = "enable_history"
	// enableInputKey 输入开关槽键（死常量）
	enableInputKey = "enable_input"
	// llmInputsKey Python LLM_INPUTS（死常量）
	llmInputsKey = "llm_inputs"
	// llmOutputsKey Python LLM_OUTPUTS（死常量）
	llmOutputsKey = "llm_outputs"
	// modelSourceKey Python MODEL_SOURCE（死常量）
	modelSourceKey = "modelType"
	// modelNameKey Python MODEL_NAME（死常量）
	modelNameKey = "modelName"
	// extensionKey Python EXTENSION（死常量）
	extensionKey = "extension"
	// chatHistoryMaxTurnKey 历史轮数槽键
	chatHistoryMaxTurnKey = "chat_history_max_turn"
	// intentDetectionTemplateKey Python INTENT_DETECTION_TEMPLATE（死常量；
	// Python 名 INTENT_DETECTION_TEMPLATE，同包 llm_comp 无碰撞，保语义名）
	intentDetectionTemplateKey = "intent_detection_template"
	// workflowChatHistoryKey Python WORKFLOW_CHAT_HISTORY（死常量）——
	// 同包 llm_comp 已有同值导出常量 WORKFLOW_CHAT_HISTORY，Go 同包复用不重声明
	// resultKey Python RESULT（死常量）
	resultKey = "result"
	// categoryNameIts1 Python CATEGORY_NAME_ITS1（死常量）
	categoryNameIts1 = "category_name_list"
	// fewShotNum Python FEW_SHOT_NUM（死常量）
	fewShotNum = 5
	// enableQ2L Python ENABLE_Q2L（死常量）
	enableQ2L = "enableKnowledge"
	// defaultQueryCate Python DEFAULT_QUERY_CATE（死常量）
	defaultQueryCate = "title"
	// defaultClassCate Python DEFAULT_CLASS_CATE（死常量）
	defaultClassCate = "content"
	// defaultIntent Python DEFAULT_INT（死常量）
	defaultIntent = "不确定，其他的意图"
	// searchType Python SEARCH_TYPE（死常量）
	searchType = "faq"
	// searchNum Python SEARCH_NUM（死常量）
	searchNum = 5
	// classificationIDKey 输出分类号键
	classificationIDKey = "classificationId"
	// classificationDefaultID 默认分类号
	classificationDefaultID = 0
	// classificationNameKey 输出分类名键
	classificationNameKey = "name"
	// classificationDefaultNameZH 默认分类名（中文）
	classificationDefaultNameZH = "默认意图"
	// classificationDefaultNameEN 默认分类名（英文）
	classificationDefaultNameEN = "Default intent"
	// kgFilterKey Python KG_FILTER_KEY（死常量）
	kgFilterKey = "filter_string"
	// kgFilterPrefix Python KG_FILTER_PREFIX（死常量）
	kgFilterPrefix = "category:"
	// kgScopeKey Python KG_SCOPE（死常量）
	kgScopeKey = "scope"
	// intentRoleKey Python ROLE（死常量）——同包 llm_comp 已有同值 roleKey，
	// 碰撞改名保常量（注释即偏差记录）
	intentRoleKey = "role"
	// intentContentKey Python CONTENT（死常量）——同上，同包碰撞改名
	intentContentKey = "content"
)

// 六段失败 reason 文案（Python :55-63 逐字；按 accept_language 二选一）
const (
	jsonParseFailReasonZH    = "当前意图识别的输出:'{result}'格式不符合有效的JSON规范，导致解析失败，因此返回默认分类。"
	jsonParseFailReasonEN    = "The intent detection output '{result}' does not conform to valid JSON format, parsing failed, therefore default category is returned."
	classKeyMissingReasonZH  = "当前意图识别的输出 '{result}' 缺少必要的输出'class'分类信息，因此返回默认分类。"
	classKeyMissingReasonEN  = "The intent detection output '{result}' is missing the required 'class' classification field, therefore default category is returned."
	validationFailReasonZH   = "当前意图识别的输出类别 '{intent_class}' 不在预定义的分类列表: '{category_list}'中，因此系统返回默认分类。"
	validationFailReasonEN   = "The intent detection output class '{intent_class}' is not in the predefined category list: '{category_list}', therefore default category is returned."
)

// 默认提示词（Python :89-132 逐字复制，双语双段）
const (
	defaultSystemPromptZH = "你是一个识别用户输入意图的AI助手。"
	defaultSystemPromptEN = "You are an AI assistant that identifies user input intent."

	defaultUserPromptZH = `
{{user_prompt}}

当前可供选择的功能分类如下：
{{category_info}}

用户与助手的对话历史：
{{chat_history}}

当前输入：
{{input}}

请根据当前输入和对话历史分析并输出最适合的功能分类。输出格式为 JSON，包含以下两个字段：
class: 代表分类结果
reason: 说明为何选择该分类
例如: {"class": "分类xx", "reason": "当前输入xxx"}
请参考以下示例：
{{example_content}}
如果没有合适的分类，请输出 {{default_class}}。
`

	defaultUserPromptEN = `
{{user_prompt}}

Available function categories:
{{category_info}}

Conversation history between user and assistant:
{{chat_history}}

Current input:
{{input}}

Please analyze the current input and conversation history, and output the most suitable function category. Output format is JSON with the following two fields:
class: represents the classification result
reason: explains why this classification was chosen
Example: {"class": "Category1", "reason": "Current input xxx"}
Please refer to the following examples:
{{example_content}}
If no suitable category exists, output {{default_class}}.
`
)

// ──────────────────────────── 全局变量 ────────────────────────────

// roleMapZH / roleMapEN 历史角色映射（Python ROLE_MAP_ZH/EN :53-54 逐字）
var (
	roleMapZH = map[string]string{"user": "用户", "assistant": "助手", "system": "系统", "tool": "工具"}
	roleMapEN = map[string]string{"user": "User", "assistant": "Assistant", "system": "System", "tool": "Tool"}
	// providerNameMap 供应商展示名（Python _PROVIDER_NAME_MAP :84-87，死常量照搬）
	providerNameMap = map[string]string{"openai": "OpenAI", "siliconflow": "SiliconFlow"}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewIntentDetectionCompConfig 默认组件配置（Python dataclass 默认值语义：
// chat_history_max_turn=3、accept_language='zh'，其余零值）
func NewIntentDetectionCompConfig() *IntentDetectionCompConfig {
	return &IntentDetectionCompConfig{ChatHistoryMaxTurn: 3, AcceptLanguage: "zh"}
}

// GetDefaultTemplate 按语言返回默认意图识别模板（Python get_default_template :135-149）。
// 内容为 [SystemMessage(默认系统提示词), UserMessage(默认用户提示词)] 双消息。
func GetDefaultTemplate(acceptLanguage string) *prompt.PromptTemplate {
	sys, usr := defaultSystemPromptZH, defaultUserPromptZH
	if acceptLanguage == "en" {
		sys, usr = defaultSystemPromptEN, defaultUserPromptEN
	}
	return prompt.NewPromptTemplate("", []llmschema.BaseMessage{
		llmschema.NewSystemMessage(sys),
		llmschema.NewUserMessage(usr),
	})
}

// NewIntentDetectionExecutable 构造执行体（Python __init__ :186-194 四步：
// 字段初始化 → _init_default_config_category_list → _append_default_category →
// router=nil；InvokeFn 槽承载 invoke）
func NewIntentDetectionExecutable(cfg *IntentDetectionCompConfig) *IntentDetectionExecutable {
	e := &IntentDetectionExecutable{cfg: cfg}
	e.initDefaultConfigCategoryList()
	e.appendDefaultCategory()
	// InvokeFn 槽接线在 Task 5 invokeEntry 落地时补（Python invoke :207-230）
	return e
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// initDefaultConfigCategoryList 构造期派生分类表（Python _init_default_config_category_list
// :435-444 一比一）：lang 兜底 'zh' → 前缀 分类/Category → default_class=前缀0 →
// default_config（模板=双语默认、default_class）→ 按 category_name_list 序号追加 前缀N
func (e *IntentDetectionExecutable) initDefaultConfigCategoryList() {
	lang := e.cfg.AcceptLanguage
	if lang == "" {
		lang = "zh"
	}
	categoryPrefix := "分类"
	if lang == "en" {
		categoryPrefix = "Category"
	}
	defaultClass := categoryPrefix + "0"
	e.defaultCfg = &IntentDetectionDefaultConfig{
		IntentDetectionTemplate: GetDefaultTemplate(lang),
		DefaultClass:            defaultClass,
		EnableInput:             true, // Python dataclass 默认 enable_input=True，构造未显式传
	}
	for index := range e.cfg.CategoryNameList {
		e.defaultCfg.CategoryList = append(e.defaultCfg.CategoryList, categoryPrefix+strconv.Itoa(index+1))
	}
}

// appendDefaultCategory 默认分类头部追加（Python _append_default_category :420-424
// 一比一）：分类表头部插 default_class；名字表头部插默认名（按语言）
func (e *IntentDetectionExecutable) appendDefaultCategory() {
	defaultName := classificationDefaultNameZH
	if e.cfg.AcceptLanguage == "en" {
		defaultName = classificationDefaultNameEN
	}
	e.defaultCfg.CategoryList = append([]string{e.defaultCfg.DefaultClass}, e.defaultCfg.CategoryList...)
	e.cfg.CategoryNameList = append([]string{defaultName}, e.cfg.CategoryNameList...)
}
