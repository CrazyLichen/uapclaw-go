// 8.23 IntentDetectionComp——意图识别组件（Python intent_detection_comp.py 一比一）。
// 组件自带 BranchRouter 路由；解析失败三类回落默认分类；历史读写走 ModelContext。
// 已知偏差见 spec docs/superpowers/specs/2026-11-23-intent-detection-comp-8.23-design.md §6。
package llm

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/pythonsem"
	"github.com/uapclaw/uapclaw-go/internal/common/security"
	forllm "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	modelclients "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/prompt"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph"
	graphexe "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/executable"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner"
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
	// testInvoker 测试注入缝（D-23-4，Python 所无）
	testInvoker LLMInvoker
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
	// Python dataclass 默认值语义（字段缺省时 __init__ 自动填）：max_turn=3
	if cfg.ChatHistoryMaxTurn <= 0 {
		cfg.ChatHistoryMaxTurn = 3
	}
	e := &IntentDetectionExecutable{cfg: cfg}
	e.initDefaultConfigCategoryList()
	e.appendDefaultCategory()
	// InvokeFn 槽：Python invoke :207-230 十步（8.21 审查惯例——基座统一提 kwargs
	// context 下发，组件经槽接入）
	e.InvokeFn = e.invokeEntry
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

// ──────────────────────────── Task 2 解析层 ────────────────────────────

// refixRegex 首个 { 到最后一个 }（贪婪，re.DOTALL 语义，Python :198）
var refixRegex = regexp.MustCompile(`(?s)\{.*\}`)

// refixLLMOutput 提取首个 {...} 块并修 false/true/null → False/True/None
// （Python _refix_llm_output :196-205 一比一）。替换波及普通文本照搬不修。
func refixLLMOutput(inputStr string) string {
	loc := refixRegex.FindStringIndex(inputStr)
	if loc == nil {
		return inputStr
	}
	res := inputStr[loc[0]:loc[1]]
	res = strings.ReplaceAll(res, "false", "False")
	res = strings.ReplaceAll(res, "true", "True")
	res = strings.ReplaceAll(res, "null", "None")
	return res
}

// ──────────────────────────── pyLiteralEval（D-23-1）────────────────────────────

// pyLiteralParser Python 字面量递归下降解析器（ast.literal_eval 等价子集）
type pyLiteralParser struct {
	s   string
	pos int
}

// pyLiteralEval Python 字面量安全求值（ast.literal_eval 等价子集：dict/list/tuple/
// str/bool/None/int/float，spec D-23-1）。不支持语法或残缺输入返回 error。
func pyLiteralEval(s string) (any, error) {
	p := &pyLiteralParser{s: s}
	v, err := p.parseValue()
	if err != nil {
		return nil, err
	}
	p.skipBlank()
	if p.pos != len(p.s) {
		return nil, fmt.Errorf("pyLiteralEval: 尾随字符 %q", p.s[p.pos:])
	}
	return v, nil
}

// skipBlank 跳过空白
func (p *pyLiteralParser) skipBlank() {
	for p.pos < len(p.s) && (p.s[p.pos] == ' ' || p.s[p.pos] == '\t' || p.s[p.pos] == '\n' || p.s[p.pos] == '\r') {
		p.pos++
	}
}

// parseValue 递归分派：容器/串/关键字/数字
func (p *pyLiteralParser) parseValue() (any, error) {
	p.skipBlank()
	if p.pos >= len(p.s) {
		return nil, fmt.Errorf("pyLiteralEval: 意外结束")
	}
	switch c := p.s[p.pos]; {
	case c == '{':
		return p.parseDict()
	case c == '[' || c == '(':
		return p.parseSeq(c)
	case c == '\'' || c == '"':
		return p.parseString()
	case c == '-' || (c >= '0' && c <= '9') || c == '+' || c == '.':
		return p.parseNumber()
	default:
		return p.parseKeyword()
	}
}

// parseDict 解析 {'k': v, ...}
func (p *pyLiteralParser) parseDict() (any, error) {
	p.pos++ // 跳过 '{'
	m := map[string]any{}
	p.skipBlank()
	if p.pos < len(p.s) && p.s[p.pos] == '}' {
		p.pos++
		return m, nil
	}
	for {
		key, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		ks, ok := key.(string)
		if !ok {
			return nil, fmt.Errorf("pyLiteralEval: dict 键须为字符串")
		}
		p.skipBlank()
		if p.pos >= len(p.s) || p.s[p.pos] != ':' {
			return nil, fmt.Errorf("pyLiteralEval: dict 缺冒号")
		}
		p.pos++
		val, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		m[ks] = val
		p.skipBlank()
		if p.pos >= len(p.s) {
			return nil, fmt.Errorf("pyLiteralEval: dict 意外结束")
		}
		switch p.s[p.pos] {
		case ',':
			p.pos++
		case '}':
			p.pos++
			return m, nil
		default:
			return nil, fmt.Errorf("pyLiteralEval: dict 缺逗号或右括号")
		}
	}
}

// parseSeq 解析 [...] / (...)（元组归并为列表，与 parseDetectionResult 消费形态一致）
func (p *pyLiteralParser) parseSeq(open byte) (any, error) {
	p.pos++
	items := []any{}
	p.skipBlank()
	if p.pos < len(p.s) && (p.s[p.pos] == ']' || p.s[p.pos] == ')') {
		p.pos++
		return items, nil
	}
	for {
		v, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		items = append(items, v)
		p.skipBlank()
		if p.pos >= len(p.s) {
			return nil, fmt.Errorf("pyLiteralEval: 序列意外结束")
		}
		switch p.s[p.pos] {
		case ',':
			p.pos++
			// 尾逗号允许（Python 字面量特性）
			p.skipBlank()
			if p.pos < len(p.s) && (p.s[p.pos] == ']' || p.s[p.pos] == ')') {
				p.pos++
				return items, nil
			}
		case ']', ')':
			p.pos++
			return items, nil
		default:
			return nil, fmt.Errorf("pyLiteralEval: 序列缺逗号或右括号")
		}
	}
}

// parseString 解析引号串（不支持转义——LLM 输出场景无转义需求，残缺即报错）
func (p *pyLiteralParser) parseString() (any, error) {
	quote := p.s[p.pos]
	p.pos++
	var sb strings.Builder
	for p.pos < len(p.s) {
		if p.s[p.pos] == quote {
			p.pos++
			return sb.String(), nil
		}
		sb.WriteByte(p.s[p.pos])
		p.pos++
	}
	return nil, fmt.Errorf("pyLiteralEval: 字符串未闭合")
}

// parseNumber 解析整数/浮点（含正负号与小数点）
func (p *pyLiteralParser) parseNumber() (any, error) {
	start := p.pos
	if p.pos < len(p.s) && (p.s[p.pos] == '-' || p.s[p.pos] == '+') {
		p.pos++
	}
	isFloat := false
	for p.pos < len(p.s) && (p.s[p.pos] >= '0' && p.s[p.pos] <= '9' || p.s[p.pos] == '.') {
		if p.s[p.pos] == '.' {
			if isFloat {
				return nil, fmt.Errorf("pyLiteralEval: 非法数字")
			}
			isFloat = true
		}
		p.pos++
	}
	lit := p.s[start:p.pos]
	if lit == "" || lit == "-" || lit == "+" || lit == "." {
		return nil, fmt.Errorf("pyLiteralEval: 非法数字")
	}
	if isFloat {
		f, err := strconv.ParseFloat(lit, 64)
		if err != nil {
			return nil, err
		}
		return f, nil
	}
	i, err := strconv.ParseInt(lit, 10, 64)
	if err != nil {
		return nil, err
	}
	return i, nil
}

// parseKeyword 解析 True/False/None（含前缀陷阱逐字报错）
func (p *pyLiteralParser) parseKeyword() (any, error) {
	for kw, v := range map[string]any{"True": true, "False": false, "None": nil} {
		if strings.HasPrefix(p.s[p.pos:], kw) {
			p.pos += len(kw)
			return v, nil
		}
	}
	return nil, fmt.Errorf("pyLiteralEval: 无法识别的记号 %q", p.s[p.pos:])
}

// ──────────────────────────── 解析主链 ────────────────────────────

// postProcessIntentDetection 解析主链（Python :377-414 步骤一比一）：
// refix → literal_eval（异常/非 dict → 默认分类+JSON 失败文案）→ 缺 class →
// 缺键文案 → 去 \n/空格/引号 → 正则规整 分类N/CategoryN → 不在表内 → 默认分类+
// 校验失败文案（reason 同步覆盖）→ 返回 (class, reason)
func (e *IntentDetectionExecutable) postProcessIntentDetection(result string) (string, string) {
	lang := e.cfg.AcceptLanguage
	jsonFailReason := jsonParseFailReasonZH
	classMissingReason := classKeyMissingReasonZH
	validationFailReason := validationFailReasonZH
	categoryPattern := `分类\d+`
	if lang == "en" {
		jsonFailReason = jsonParseFailReasonEN
		classMissingReason = classKeyMissingReasonEN
		validationFailReason = validationFailReasonEN
		categoryPattern = `Category\d+`
	}

	// 步骤1：refix + 字面量求值（:385-389）
	refixed := refixLLMOutput(result)
	parsedDict, err := pyLiteralEval(refixed)
	if err != nil {
		return e.defaultCfg.DefaultClass, strings.ReplaceAll(jsonFailReason, "{result}", refixed)
	}

	// 步骤2：非 dict → JSON 失败文案（:391-392；result 形参为 refixed 原文）
	dict, ok := parsedDict.(map[string]any)
	if !ok {
		return e.defaultCfg.DefaultClass, strings.ReplaceAll(jsonFailReason, "{result}", refixed)
	}

	// 步骤3：缺 class 键（Python :395-396 `not parsed_dict.get(CLASS)` 全 falsy 语义——
	// None/""/False/0/0.0/空容器均判缺，审查拍板②A；文案形参是解析后 dict 的 repr）
	rawClass, hasClass := dict[classKey]
	if !hasClass || pyFalsy(rawClass) {
		return e.defaultCfg.DefaultClass,
			strings.ReplaceAll(classMissingReason, "{result}", pythonsem.PyRepr(dict))
	}

	// 步骤4：class 清洗（Python :398：去 \n/空格/双引/单引）
	intentClass, _ := rawClass.(string)
	if intentClass == "" {
		intentClass = fmt.Sprintf("%v", rawClass)
	}
	intentClass = strings.ReplaceAll(intentClass, "\n", "")
	intentClass = strings.ReplaceAll(intentClass, " ", "")
	intentClass = strings.ReplaceAll(intentClass, "\"", "")
	intentClass = strings.ReplaceAll(intentClass, "'", "")

	// 步骤5：正则规整（Python :399-405；IGNORECASE；en 再提数字组重排 CategoryN）
	match := regexp.MustCompile(`(?i)` + categoryPattern).FindString(intentClass)
	if match != "" {
		if lang == "en" {
			digitPart := regexp.MustCompile(`\d+`).FindString(match)
			if digitPart != "" {
				match = "Category" + digitPart
			}
		}
		intentClass = match
		dict[classKey] = intentClass
	}

	// 步骤6：不在分类表 → 默认分类 + 校验失败文案（:407-412）
	if !e.validateLLMParsedResult(intentClass) {
		reason := strings.ReplaceAll(validationFailReason, "{intent_class}", intentClass)
		reason = strings.ReplaceAll(reason, "{category_list}", pythonsem.PyRepr(e.defaultCfg.CategoryList))
		dict[classKey] = e.defaultCfg.DefaultClass
		dict[reasonKey] = reason
	}

	// 步骤7：返回 class 与 reason（缺省 ""，:414——class 取 dict 终值：
	// 命中分类表时为规整值，回落时被 :412 update 为默认分类）
	reason, _ := dict[reasonKey].(string)
	finalClass, _ := dict[classKey].(string)
	return finalClass, reason
}

// validateLLMParsedResult 类别表校验（Python _validate_llm_parsed_result :416-418）
func (e *IntentDetectionExecutable) validateLLMParsedResult(result string) bool {
	for _, category := range e.defaultCfg.CategoryList {
		if category == result {
			return true
		}
	}
	return false
}

// pyFalsy Python falsy 判定（`not x` 等价面：nil/""/False/0/0.0/空 map/空 slice）。
// 审查拍板②A——补齐 :395 `not parsed_dict.get(CLASS)` 的全 falsy 语义。
func pyFalsy(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case bool:
		return !x
	case int:
		return x == 0
	case int64:
		return x == 0
	case float64:
		return x == 0
	case map[string]any:
		return len(x) == 0
	case []any:
		return len(x) == 0
	default:
		return false
	}
}

// getIntentIDAndName 分类号与名字解析（Python :426-433 一比一）：顺序扫描命中返回
// {classificationId: idx, name: 名字表[idx]}；未命中 {0, 默认名}
func (e *IntentDetectionExecutable) getIntentIDAndName(intentClass string) (int, string) {
	defaultName := classificationDefaultNameZH
	if e.cfg.AcceptLanguage == "en" {
		defaultName = classificationDefaultNameEN
	}
	for idx, category := range e.defaultCfg.CategoryList {
		if category == intentClass {
			return idx, e.cfg.CategoryNameList[idx]
		}
	}
	return classificationDefaultID, defaultName
}

// buildOutputMap 输出构造（Python IntentDetectionOutput.model_dump(exclude_defaults=True)
// :332-334 等价面）：字段==默认值（-1 / ""）时剔除键
func buildOutputMap(classificationID int, reason string, categoryName string) map[string]any {
	out := map[string]any{}
	if classificationID != -1 {
		out[classificationIDKey] = classificationID
	}
	if reason != "" {
		out["reason"] = reason
	}
	if categoryName != "" {
		out[classificationNameKey] = categoryName
	}
	return out
}

// parseDetectionResult 解析总入口（Python :329-334 一比一）：
// postProcess → getIntentIDAndName → exclude_defaults 输出 map
func (e *IntentDetectionExecutable) parseDetectionResult(llmOutput string) map[string]any {
	intentClass, reason := e.postProcessIntentDetection(llmOutput)
	classificationID, categoryName := e.getIntentIDAndName(intentClass)
	return buildOutputMap(classificationID, reason, categoryName)
}

// ──────────────────────────── Task 3 模型双路径 ────────────────────────────

// createLLMInstance 模型实例创建（Python _create_llm_instance :254-264 一比一）：
// model_id 为 nil → client/request config 双非空校验（缺任一
// COMPONENT_INTENT_DETECTION_INVOKE_CALL_FAILED）后 NewModel；否则按 ID 走 ResourceMgr
func (e *IntentDetectionExecutable) createLLMInstance(ctx context.Context) (LLMInvoker, error) {
	if e.cfg.ModelID == nil {
		if e.cfg.ModelClientConfig == nil || e.cfg.ModelConfig == nil {
			return nil, exception.BuildError(exception.StatusComponentIntentDetectionInvokeCallFailed,
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

// initializeIfNeeded 懒初始化（Python _initialize_if_needed :266-276 一比一）：
// 未初始化时创建并置位；异常包装 COMPONENT_INTENT_DETECTION_LLM_INIT_FAILED
func (e *IntentDetectionExecutable) initializeIfNeeded(ctx context.Context) error {
	if e.initialized {
		return nil
	}
	model, err := e.createLLMInstance(ctx)
	if err != nil {
		return exception.BuildError(exception.StatusComponentIntentDetectionLLMInitFailed,
			exception.WithMsg("failed to initialize llm if needed"), exception.WithCause(err))
	}
	e.llModel = model
	e.initialized = true
	return nil
}

// ──────────────────────────── Task 4 输入组装 ────────────────────────────

// getCategoryInfo 分类信息串（Python _get_category_info :247-249 一比一）：
// 分类表与名字表 zip 拼 "cid: cname"，"\n" 连接
func (e *IntentDetectionExecutable) getCategoryInfo() string {
	lines := make([]string, 0, len(e.defaultCfg.CategoryList))
	for cid := range e.defaultCfg.CategoryList {
		if cid < len(e.cfg.CategoryNameList) {
			lines = append(lines, fmt.Sprintf("%d: %s", cid, e.cfg.CategoryNameList[cid]))
		}
	}
	return strings.Join(lines, "\n")
}

// prepareDetectionInputs 检测输入组装（Python _prepare_detection_inputs :278-312 十步一比一）：
// 十槽 update → enable_history 时格式化覆盖 chat_history → enable_input 时校验
// query（ValidationError → COMPONENT_INTENT_DETECTION_INPUT_PARAM_ERROR）→
// global_intent_map 空表收尾
func (e *IntentDetectionExecutable) prepareDetectionInputs(inputs map[string]any, chatHistory []llmschema.BaseMessage) (map[string]any, error) {
	currentInputs := map[string]any{}
	globalIntentMap := []any{}

	// 步骤1：静态槽位（:282-291）
	currentInputs[userPromptKey] = e.cfg.UserPrompt
	currentInputs[categoryInfoKey] = e.getCategoryInfo()
	currentInputs[defaultClassKey] = e.defaultCfg.DefaultClass
	currentInputs[enableHistoryKey] = e.cfg.EnableHistory
	currentInputs[enableInputKey] = e.defaultCfg.EnableInput
	currentInputs[exampleContentKey] = strings.Join(e.cfg.ExampleContent, "\n\n")
	currentInputs[chatHistoryMaxTurnKey] = e.cfg.ChatHistoryMaxTurn
	currentInputs[chatHistoryKey] = ""

	// 步骤2：历史格式化覆盖（:293-296）
	if e.cfg.EnableHistory {
		currentInputs[chatHistoryKey] = e.formatChatHistory(chatHistory)
	}

	// 步骤3：当前输入校验（:298-308；extra=allow → 只取 query，缺失报参数错误）
	if e.defaultCfg.EnableInput {
		query, err := validateIntentDetectionInput(inputs)
		if err != nil {
			return nil, exception.BuildError(exception.StatusComponentIntentDetectionInputParamError,
				exception.WithMsg("failed to validate intent detection input"), exception.WithCause(err))
		}
		currentInputs[inputKey] = query
	}

	// 步骤4：global_intent_map 空表收尾（:310）
	currentInputs["global_intent_map"] = globalIntentMap
	return currentInputs, nil
}

// validateIntentDetectionInput 输入校验（Python IntentDetectionInput.model_validate
// :301-302 等价面：query 缺失/非串/None → error；query 缺省空串)
func validateIntentDetectionInput(inputs map[string]any) (string, error) {
	raw, ok := inputs["query"]
	if !ok || raw == nil {
		return "", fmt.Errorf("query field required")
	}
	qs, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("query field must be string, got %T", raw)
	}
	return qs, nil
}

// formatChatHistory 历史格式化（Python _format_chat_history :314-322 一比一）：
// 语言选角色表 → 尾部截 chat_history_max_turn 条 → 表内角色 "角色: 内容\n" 拼接
func (e *IntentDetectionExecutable) formatChatHistory(chatHistory []llmschema.BaseMessage) string {
	roleMap := roleMapZH
	if e.cfg.AcceptLanguage == "en" {
		roleMap = roleMapEN
	}
	var sb strings.Builder
	start := len(chatHistory) - e.cfg.ChatHistoryMaxTurn
	if start < 0 {
		start = 0
	}
	for _, history := range chatHistory[start:] {
		role := history.GetRole().String()
		if _, ok := roleMap[role]; ok {
			sb.WriteString(fmt.Sprintf("%s: %s\n", roleMap[role], history.GetContent().String()))
		}
	}
	return sb.String()
}

// ──────────────────────────── Task 5 invokeEntry 主链 ────────────────────────────

// setSession 设置当前会话（Python _set_session :251-252）
func (e *IntentDetectionExecutable) setSession(sess *session.NodeSessionFacade) {
	e.session = sess
}

// setRouter 设置分类路由器（Python set_router :232-234，返回 self 链式语义由
// NewIntentDetectionComponent 装配承载）
func (e *IntentDetectionExecutable) setRouter(router *flow.BranchRouter) {
	e.router = router
}

// postCommit 提交后钩子（Python post_commit :236-237 恒 true）
func (e *IntentDetectionExecutable) postCommit() bool {
	return true
}

// getChatHistoryFromContext 取历史窗口（Python _get_chat_history_from_context :239-245
// 一比一）：enable_history 且 context 非 nil → GetContextWindow(dialogue_round=max_turn)
// → 窗口非 nil 取消息；否则空表。窗口取数失败按 Python 语义**直接传播 error**
// （Python 无 try，异常直达组件调用方——2026-11-23 审查拍板①A）
func (e *IntentDetectionExecutable) getChatHistoryFromContext(ctx context.Context, context iface.ModelContext) ([]llmschema.BaseMessage, error) {
	chatHistory := []llmschema.BaseMessage{}
	if e.cfg.EnableHistory && context != nil {
		window, err := context.GetContextWindow(ctx, nil, nil, 0, e.cfg.ChatHistoryMaxTurn)
		if err != nil {
			return nil, err
		}
		if window != nil {
			chatHistory = window.ContextMessages
		}
	}
	return chatHistory, nil
}

// invokeEntry 批入批出槽（Python invoke :207-230 十步一比一）
func (e *IntentDetectionExecutable) invokeEntry(ctx context.Context, inputs map[string]any, sess *session.NodeSessionFacade, modelContext iface.ModelContext) (map[string]any, error) {
	// 步骤1-2：setSession + router.SetSession（Session._inner 等价 = InnerNodeSession，
	// 8.19 branch_comp 同款接线）
	e.setSession(sess)
	if e.router != nil {
		if err := e.router.SetSession(sess.InnerNodeSession()); err != nil {
			return nil, err
		}
	}
	// 步骤3：懒初始化（:211）
	if err := e.initializeIfNeeded(ctx); err != nil {
		return nil, err
	}
	// 步骤4：取历史窗口（:212；失败按 Python 语义传播——审查拍板①A）
	chatHistory, err := e.getChatHistoryFromContext(ctx, modelContext)
	if err != nil {
		return nil, err
	}
	// 步骤5：组装输入（:213）
	currentInputs, err := e.prepareDetectionInputs(inputs, chatHistory)
	if err != nil {
		return nil, err
	}
	// 步骤6：调 LLM（:214；内含 START/完成两段日志）
	llmOutput, err := e.invokeLLMAndGetResult(ctx, currentInputs)
	if err != nil {
		return nil, err
	}
	// 步骤7：组件完成日志（Python invoke :215-226）
	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "WORKFLOW_COMPONENT_END").
		Str("component_id", e.session.GetExecutableID()).
		Str("component_type_str", "IntentDetectionComponent").
		Str("session_id", e.session.GetSessionID()).
		Bool("has_output", llmOutput != "").
		Int("output_length", len(llmOutput)).
		Bool("sensitive_mode", security.IsSensitive()).
		Msg("Intent detection completed")
	// 步骤8：写 user 消息进 context（:227）
	e.writeUserMessageToContext(ctx, inputs, modelContext)
	// 步骤9：写 assistant 消息进 context（:228）
	e.writeAssistantMessageToContext(ctx, llmOutput, modelContext)
	// 步骤10：解析返回（:229-230）
	return e.parseDetectionResult(llmOutput), nil
}

// invokeLLMAndGetResult 调 LLM 取文本（Python _invoke_llm_and_get_result :336-375）：
// 模板 format → to_messages → START 日志 → Invoke → 异常包装 InvokeCallFailed →
// 完成日志 → 返回 content
func (e *IntentDetectionExecutable) invokeLLMAndGetResult(ctx context.Context, currentInputs map[string]any) (string, error) {
	rendered, err := e.defaultCfg.IntentDetectionTemplate.Format(currentInputs)
	if err != nil {
		return "", exception.BuildError(exception.StatusComponentIntentDetectionInvokeCallFailed,
			exception.WithMsg("failed to format intent detection template"), exception.WithCause(err))
	}
	llmInputs, err := rendered.ToMessages()
	if err != nil {
		return "", exception.BuildError(exception.StatusComponentIntentDetectionInvokeCallFailed,
			exception.WithMsg("failed to convert template to messages"), exception.WithCause(err))
	}
	// START 日志（Python :339-350）
	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "WORKFLOW_COMPONENT_START").
		Str("component_id", e.session.GetExecutableID()).
		Str("component_type_str", "IntentDetectionComponent").
		Str("session_id", e.session.GetSessionID()).
		Bool("has_inputs", len(llmInputs) > 0).
		Int("input_count", len(llmInputs)).
		Bool("sensitive_mode", security.IsSensitive()).
		Msg("Intent detection LLM invoke started")

	llmOutputContent := ""
	// 步骤：Invoke（Python :353-361）
	response, invokeErr := e.llModel.Invoke(ctx, modelclients.NewMessagesParam(llmInputs...))
	if invokeErr != nil {
		return "", exception.BuildError(exception.StatusComponentIntentDetectionInvokeCallFailed,
			exception.WithMsg("failed to invoke llm and get result"), exception.WithCause(invokeErr))
	}
	llmOutputContent = response.Content.String()
	// 完成日志（Python :362-373）
	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "WORKFLOW_COMPONENT_END").
		Str("component_id", e.session.GetExecutableID()).
		Str("component_type_str", "IntentDetectionComponent").
		Str("session_id", e.session.GetSessionID()).
		Bool("has_output", llmOutputContent != "").
		Int("output_length", len(llmOutputContent)).
		Bool("sensitive_mode", security.IsSensitive()).
		Msg("Intent detection LLM invoke completed")
	return llmOutputContent, nil
}

// writeUserMessageToContext 写 user 消息（Python _write_user_message_to_context
// :446-455 一比一）：context nil / 未开历史 → 返回；query 校验失败静默返回；
// query 非空才写
func (e *IntentDetectionExecutable) writeUserMessageToContext(ctx context.Context, inputs map[string]any, context iface.ModelContext) {
	if context == nil || !e.cfg.EnableHistory {
		return
	}
	query, err := validateIntentDetectionInput(inputs)
	if err != nil {
		return
	}
	if query != "" {
		_, _ = context.AddMessages(ctx, []llmschema.BaseMessage{llmschema.NewUserMessage(query)})
	}
}

// writeAssistantMessageToContext 写 assistant 消息（Python :456-460 一比一）：
// context nil / 未开历史 → 返回；content 非空才写
func (e *IntentDetectionExecutable) writeAssistantMessageToContext(ctx context.Context, content string, context iface.ModelContext) {
	if context == nil || !e.cfg.EnableHistory {
		return
	}
	if content != "" {
		_, _ = context.AddMessages(ctx, []llmschema.BaseMessage{llmschema.NewAssistantMessage(content)})
	}
}

// preProcess 预处理（Python _pre_process :324-327，定义未消费死方法照搬）
func (e *IntentDetectionExecutable) preProcess(inputs map[string]any) []llmschema.BaseMessage {
	rendered, err := e.defaultCfg.IntentDetectionTemplate.Format(inputs)
	if err != nil {
		return nil
	}
	msgs, mErr := rendered.ToMessages()
	if mErr != nil {
		return nil
	}
	return msgs
}

// ──────────────────────────── Task 6 组合面 ────────────────────────────

// NewIntentDetectionComponent 构造组件组合面（Python __init__ :464-468；
// executable 懒初始化经 ToExecutableFn 槽首调构造缓存；opts 注入缝为
// spec D-23-4 测试缝，Python 所无）
func NewIntentDetectionComponent(cfg *IntentDetectionCompConfig, opts ...func(*IntentDetectionComponent)) *IntentDetectionComponent {
	c := &IntentDetectionComponent{
		cfg:    cfg,
		router: flow.NewBranchRouter(false), // Python BranchRouter() 默认 report_trace=False
	}
	for _, opt := range opts {
		opt(c)
	}
	c.ToExecutableFn = c.toExecutableCached
	return c
}

// WithIntentLLM 测试注入缝（D-23-4）：注入 LLMInvoker 替身，集成测试专用
func WithIntentLLM(inv LLMInvoker) func(*IntentDetectionComponent) {
	return func(c *IntentDetectionComponent) { c.testInvoker = inv }
}

// toExecutableCached 可执行体供应槽（Python to_executable :480-481 + executable
// property 懒初始化合并承载：首调构造并 SetRouter 缓存）
func (c *IntentDetectionComponent) toExecutableCached() graphexe.GeneralExecutor {
	if c.executable == nil {
		e := NewIntentDetectionExecutable(c.cfg)
		e.setRouter(c.router)
		if c.testInvoker != nil {
			e.llModel = c.testInvoker
			e.initialized = true
		}
		c.executable = e
	}
	return c.executable
}

// AddComponent 注册节点与条件边（Python add_component :476-478 一比一）：
// AddNode(node_id, executable, wait_for_all) + AddConditionalEdges(node_id, router)
func (c *IntentDetectionComponent) AddComponent(g *graph.PregelGraph, nodeID string, waitForAll bool) error {
	if waitForAll {
		g.AddNode(nodeID, c.toExecutableCached(), graph.WithWaitForAll())
	} else {
		g.AddNode(nodeID, c.toExecutableCached())
	}
	g.AddConditionalEdges(nodeID, c.router)
	return nil
}

// AddBranch 登记分支（Python add_branch :483-487 一比一：str target 归一
// [target]；校验与登记委派 BranchRouter.AddBranch）
func (c *IntentDetectionComponent) AddBranch(cond any, target []string, branchID string) error {
	return c.router.AddBranch(cond, target, branchID)
}

// Router 返回路由器（Python router() :489-490）
func (c *IntentDetectionComponent) Router() *flow.BranchRouter {
	return c.router
}
