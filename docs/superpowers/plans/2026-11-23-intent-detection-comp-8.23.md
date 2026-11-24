# 8.23 IntentDetectionComp 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 对齐 Python `core/workflow/components/llm/intent_detection_comp.py`（462 行）为 Go 工作流意图识别组件：LLM 分类 + 自带 BranchRouter 路由 + ModelContext 历史读写，解析失败三类回落默认分类。

**Architecture:** 新文件落在既有 llm 子包 `internal/openuapclaw/core/workflow/components/llm/`（与 8.18 LLMComp 同包单文件一比一）。`IntentDetectionComponent`（组合面，持有 BranchRouter）+ `IntentDetectionExecutable`（WorkflowComponent 基座，InvokeFn 槽）。模型双路径：model_id 走 `ModelMgr.GetModel`（返回 BaseModelClient，满足 LLMInvoker），否则 `llm.NewModel` 直构。测试按拍板 A：真实 session/ModelContext 链，fake 只在 LLM 网络边界。

**Tech Stack:** Go regexp（`\{.*\}` DOTALL）；`foundation/prompt` PromptTemplate；`foundation/llm` schema/Model；`context_engine` iface.ModelContext；`components/flow` BranchRouter（8.19 资产）；`common/exception`（三个 StatusComponentIntentDetection* 错误码已存在）；自实现 `pyLiteralEval`（D-23-1）。

**Spec:** `docs/superpowers/specs/2026-11-23-intent-detection-comp-8.23-design.md`

---

## 全局纪律（每个 Task 都适用）

1. 注释全中文；声明排序 结构体→枚举→常量→全局变量→导出函数→非导出函数，分隔注释块归位（规则 2）
2. **path-limited 提交**：`git status --porcelain` 先查非本任务 staged 项；只 `git commit <paths> -m "..."`，禁 `-a`/`-A`/裸 commit/amend（amend 前必 `git log -1` 核对归属）
3. Python 死常量照搬（RESULT/FEW_SHOT_NUM/ENABLE_Q2L/SEARCH_TYPE/SEARCH_NUM/KG_*/DEFAULT_QUERY_CATE/DEFAULT_CLASS_CATE/DEFAULT_INT/CATEGORY_NAME_ITS1/MODEL_SOURCE/MODEL_NAME/EXTENSION/LLM_INPUTS/LLM_OUTPUTS/WORKFLOW_CHAT_HISTORY/LUI/NAME/MODEL/INTENT_DETECTION_TEMPLATE/CATEGORY_LIST）
4. 提示词/文案逐字复制，日志英文（feedback-logs-english）
5. 单测命令：`go test ./internal/openuapclaw/core/workflow/components/llm/...`；全量回归 `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`

---

### Task 1: 常量、配置结构体与默认模板

**Files:**
- Modify: `internal/openuapclaw/core/workflow/components/llm/intent_detection_comp.go`（新建，本 Task 落常量段+配置段）
- Modify: `internal/openuapclaw/core/workflow/components/llm/doc.go`（文件树追加条目）
- Test: `internal/openuapclaw/core/workflow/components/llm/intent_detection_comp_test.go`

- [ ] **Step 1: 写常量段（全部模块常量，含死常量）**

Python :29-91 逐条转 lowerCamel（键名常量加 Key 后缀防冲突，如 `NAME`→`nameKey`）：

```go
// ──────────────────────────── 常量 ────────────────────────────

const (
	lui                       = "llm"
	nameKey                   = "name"
	modelKey                  = "model"
	classKey                  = "class"
	reasonKey                 = "reason"
	inputKey                  = "input"
	userPromptKey             = "user_prompt"
	categoryInfoKey           = "category_info"
	categoryListKey           = "category_list"
	categoryNameListKey       = "category_name_list"
	defaultClassKey           = "default_class"
	chatHistoryKey            = "chat_history"
	exampleContentKey         = "example_content"
	enableHistoryKey          = "enable_history"
	enableInputKey            = "enable_input"
	llmInputsKey              = "llm_inputs"
	llmOutputsKey             = "llm_outputs"
	modelSourceKey            = "modelType"
	modelNameKey              = "modelName"
	extensionKey              = "extension"
	chatHistoryMaxTurnKey     = "chat_history_max_turn"
	intentDetectionTemplateK  = "intent_detection_template"
	roleKey                   = "role"
	contentKey                = "content"
	workflowChatHistoryKey    = "workflow_chat_history"
	resultKey                 = "result"
	categoryNameIts1          = "category_name_list"
	fewShotNum                = 5
	enableQ2L                 = "enableKnowledge"
	defaultQueryCate          = "title"
	defaultClassCate          = "content"
	defaultIntent             = "不确定，其他的意图"
	searchType                = "faq"
	searchNum                 = 5
	classificationIDKey       = "classificationId"
	classificationDefaultID   = 0
	classificationNameKey     = "name"
	classificationDefaultNameZH = "默认意图"
	classificationDefaultNameEN = "Default intent"
	kgFilterKey               = "filter_string"
	kgFilterPrefix            = "category:"
	kgScopeKey                = "scope"
)

// roleMapZH / roleMapEN 角色映射（Python ROLE_MAP_ZH/EN :53-54，逐字）
var (
	roleMapZH = map[string]string{"user": "用户", "assistant": "助手", "system": "系统", "tool": "工具"}
	roleMapEN = map[string]string{"user": "User", "assistant": "Assistant", "system": "System", "tool": "Tool"}

	// providerNameMap 供应商展示名（Python _PROVIDER_NAME_MAP :84-87，死常量照搬）
	providerNameMap = map[string]string{"openai": "OpenAI", "siliconflow": "SiliconFlow"}
)

// 六段失败 reason 文案 + 双语默认提示词模板（:55-132）逐字复制进 const 块
// （jsonParseFailReasonZH/EN、classKeyMissingReasonZH/EN、validationFailReasonZH/EN、
//   defaultSystemPromptZH/EN、defaultUserPromptZH/EN——原文见 Python 源码 :55-132，
//   复制时保持换行与缩进逐字一致）
```

- [ ] **Step 2: 写配置/输入/输出结构体**

```go
// ──────────────────────────── 结构体 ────────────────────────────

// IntentDetectionCompConfig 意图识别组件配置（Python IntentDetectionCompConfig :152-162 一比一）
type IntentDetectionCompConfig struct {
	// ModelID 预注册模型 ID（Python model_id）
	ModelID *string
	// ModelClientConfig 模型客户端配置
	ModelClientConfig *llmschema.ModelClientConfig
	// ModelConfig 模型请求配置
	ModelConfig *llmschema.ModelRequestConfig
	// CategoryNameList 分类名列表（用户可见分支名）
	CategoryNameList []string
	// UserPrompt 用户提示词（业务语境描述）
	UserPrompt string
	// ExampleContent few-shot 示例列表
	ExampleContent []string
	// EnableHistory 是否启用会话历史
	EnableHistory bool
	// ChatHistoryMaxTurn 历史最大轮数（默认 3）
	ChatHistoryMaxTurn int
	// AcceptLanguage 语言（'zh'|'en'，默认 'zh'）
	AcceptLanguage string
}

// NewIntentDetectionCompConfig 默认配置（dataclass 默认值语义）
func NewIntentDetectionCompConfig() *IntentDetectionCompConfig {
	return &IntentDetectionCompConfig{ChatHistoryMaxTurn: 3, AcceptLanguage: "zh"}
}

// IntentDetectionDefaultConfig 运行期派生默认配置（Python :165-171；default_class 由语言派生）
type IntentDetectionDefaultConfig struct {
	CategoryList             []string
	IntentDetectionTemplate  *prompt.PromptTemplate
	DefaultClass             string
	EnableInput              bool
}

// IntentDetectionInput 节点输入（Python :173-175，query 必填 + extra allow）
type IntentDetectionInput struct {
	// Query 当前用户输入
	Query string
}

// IntentDetectionOutput 节点输出（Python :178-182；exclude_defaults 语义见 buildOutputMap）
type IntentDetectionOutput struct {
	// ClassificationID 分类索引（默认 -1）
	ClassificationID int
	// Reason 选择理由
	Reason string
	// CategoryName 分类名
	CategoryName string
}
```

- [ ] **Step 3: 写 GetDefaultTemplate + 执行体骨架**

```go
// GetDefaultTemplate 按语言返回默认意图识别模板（Python get_default_template :135-149）。
// 内容为 [SystemMessage(默认系统提示词), UserMessage(默认用户提示词)] 双消息列表。
func GetDefaultTemplate(acceptLanguage string) *prompt.PromptTemplate {
	sys, usr := defaultSystemPromptZH, defaultUserPromptZH
	if acceptLanguage == "en" {
		sys, usr = defaultSystemPromptEN, defaultUserPromptEN
	}
	return prompt.NewPromptTemplate("", []llmschema.BaseMessage{
		&llmschema.SystemMessage{Content: sys},
		&llmschema.UserMessage{Content: usr},
	})
}

// IntentDetectionExecutable 意图识别执行体（Python :184-194 字段一比一）
type IntentDetectionExecutable struct {
	// WorkflowComponent 8.17 组件基座（InvokeFn 槽）
	components.WorkflowComponent
	// session 当前会话（Python self._session）
	session *session.NodeSessionFacade
	// llModel 懒初始化模型实例（Python self._llm）
	llModel LLMInvoker
	// initialized 懒初始化标记（Python self._initialized）
	initialized bool
	// cfg 组件配置（Python self._config）
	cfg *IntentDetectionCompConfig
	// defaultCfg 运行期派生默认配置（Python self._default_config）
	defaultCfg *IntentDetectionDefaultConfig
	// router 分类路由器（Python self._router）
	router *flow.BranchRouter
}
```

`__init__` 四步（:186-194）→ 构造器：

```go
// NewIntentDetectionExecutable 构造执行体（Python __init__ :186-194 四步：
// 字段初始化 → _init_default_config_category_list → _append_default_category → router=nil）
func NewIntentDetectionExecutable(cfg *IntentDetectionCompConfig) *IntentDetectionExecutable {
	e := &IntentDetectionExecutable{cfg: cfg}
	e.initDefaultConfigCategoryList()
	e.appendDefaultCategory()
	e.InvokeFn = e.invokeEntry
	return e
}
```

- [ ] **Step 4: 写 initDefaultConfigCategoryList + appendDefaultCategory + GetDefaultTemplate 单测**

```go
// TestNewIntentDetectionExecutable_分类列表派生ZH 构造期派生：分类1..N + 默认分类头部追加
func TestNewIntentDetectionExecutable_分类列表派生ZH(t *testing.T) {
	e := NewIntentDetectionExecutable(&IntentDetectionCompConfig{
		CategoryNameList: []string{"退款申请", "物流查询"},
	})
	// 分类表 = [分类0, 分类1, 分类2]（默认头部）；名字表 = [默认意图, 退款申请, 物流查询]
	if len(e.defaultCfg.CategoryList) != 3 || e.defaultCfg.CategoryList[0] != "分类0" ||
		e.defaultCfg.CategoryList[1] != "分类1" {
		t.Fatalf("分类表派生错误: %v", e.defaultCfg.CategoryList)
	}
	if len(e.cfg.CategoryNameList) != 3 || e.cfg.CategoryNameList[0] != "默认意图" {
		t.Fatalf("名字表头部追加错误: %v", e.cfg.CategoryNameList)
	}
	if e.defaultCfg.DefaultClass != "分类0" {
		t.Fatalf("默认分类错误: %q", e.defaultCfg.DefaultClass)
	}
}

// TestNewIntentDetectionExecutable_英文派生 en → CategoryN/Default intent/分类0→Category0
func TestNewIntentDetectionExecutable_英文派生(t *testing.T) { /* 同上断言 Category0/Category1/Default intent */ }

// TestGetDefaultTemplate_双语言 zh/en 模板消息逐字（System 内容 == defaultSystemPromptZH/EN）
func TestGetDefaultTemplate_双语言(t *testing.T) { /* 断言消息数=2、角色、内容逐字相等 */ }
```

- [ ] **Step 5: 运行测试通过后提交**

```bash
go test ./internal/openuapclaw/core/workflow/components/llm/...
git status --porcelain
git commit internal/openuapclaw/core/workflow/components/llm/intent_detection_comp.go \
  internal/openuapclaw/core/workflow/components/llm/doc.go \
  internal/openuapclaw/core/workflow/components/llm/intent_detection_comp_test.go \
  -m "feat(8.23): IntentDetectionComp 常量/配置/默认模板/分类表派生"
```

---

### Task 2: 解析层——refixLLMOutput + pyLiteralEval + 解析回落链

**Files:**
- Modify: `intent_detection_comp.go`（解析函数段）
- Test: 同 `_test.go`

- [ ] **Step 1: 写失败测试（解析全分支，spec §5 覆盖点 4）**

```go
// TestPostProcessIntentDetection_全分支 Python :377-414 逐分支
func TestPostProcessIntentDetection_全分支(t *testing.T) {
	e := NewIntentDetectionExecutable(&IntentDetectionCompConfig{CategoryNameList: []string{"退款"}})

	// 分支1：非 JSON 文本 → 默认分类 + jsonParseFailReason
	class, reason := e.postProcessIntentDetection("完全不是JSON")
	if class != "分类0" || !strings.Contains(reason, "格式不符合有效的JSON规范") { t.Fatal(...) }

	// 分支2：修复 true/false/null 后可解析（LLM 常见输出）
	class, _ = e.postProcessIntentDetection(`{"class": "分类1", "reason": "r", "ok": true}`)
	if class != "分类1" { t.Fatal(...) }

	// 分支3：单引号 dict（ast.literal_eval 特性，JSON 解析不了的形态）
	class, _ = e.postProcessIntentDetection(`{'class': '分类1', 'reason': 'r'}`)
	if class != "分类1" { t.Fatal(...) }

	// 分支4：缺 class 键 → 默认分类 + classKeyMissingReason
	class, reason = e.postProcessIntentDetection(`{"reason": "r"}`)
	if class != "分类0" || !strings.Contains(reason, "缺少必要的输出") { t.Fatal(...) }

	// 分支5：类别不在列表 → 默认分类 + validationFailReason；带杂音文本正则规整
	class, reason = e.postProcessIntentDetection(`{"class": "分类 9\n", "reason": "r"}`)
	if class != "分类0" || !strings.Contains(reason, "不在预定义的分类列表") { t.Fatal(...) }

	// 分支6：规整命中——"分类 1\n" 去空白引号后正则捕获 分类1
	class, _ = e.postProcessIntentDetection(`{"class": "分类 1\n", "reason": "r"}`)
	if class != "分类1" { t.Fatal(...) }
}
```

- [ ] **Step 2: 实现 pyLiteralEval（D-23-1，Python 字面量子集求值）**

```go
// pyLiteralEval Python 字面量安全求值（ast.literal_eval 等价子集：dict/list/tuple/
// str/bool/None/数字），spec D-23-1。不支持的语法返回 error。
// 实现：递归下降——跳过空白；识别 True/False/None/数字(含负/浮点)/引号串/'-串"/串/
// '['列表'('/'元组'{'字典'；dict 项 key ':' value 逗号分隔。~70 行。
func pyLiteralEval(s string) (any, error) { /* 见下方实现要点 */ }
```

实现要点（完整落码）：`parser` 结构体持 `s string; pos int`；`parseValue` 分派 `'{'`→parseDict、`'['`→parseList、`'('`→parseTuple、`'\''|'"'`→parseString、字母开头→True/False/None/数字、其余→数字；`parseDict` 期望 `key: value` 对、`parseList` 逗号分隔元素。末尾要求 `pos == len(s)`（Python literal_eval 对残缺输入报错）。数字用 `strconv.ParseInt/ParseFloat`。

- [ ] **Step 3: 实现 refixLLMOutput + postProcessIntentDetection + parseDetectionResult + getIntentIDAndName**

```go
// refixLLMOutput 提取首个 {...} 块并修 false/true/null（Python :196-205，DOTALL）
func refixLLMOutput(inputStr string) string {
	match := regexp.MustCompile(`(?s)\{.*\}`).FindStringIndex(inputStr) // 首个 { 到最后 }
	if match == nil { return inputStr }
	res := inputStr[match[0]:match[1]]
	res = strings.ReplaceAll(res, "false", "False")
	res = strings.ReplaceAll(res, "true", "True")
	res = strings.ReplaceAll(res, "null", "None")
	return res
}
```

注意对齐陷阱：Python `re.search(r'\{.*\}', ..., re.DOTALL)` 是**贪婪**匹配——从首个 `{` 到**最后一个** `}`（`FindString` 语义一致）；替换 true→True 会波及用户文本中的英文单词（如 "true story"→"True story"），照搬不修。

```go
// postProcessIntentDetection 解析主链（Python :377-414 步骤一比一）：
// refix → pyLiteralEval（异常→默认分类+jsonParseFailReason）→ 非 dict 同上 →
// 缺 class → classKeyMissingReason → 去 \n/空格/引号 → 正则
// `(?i)分类\d+`（en 为 `(?i)Category\d+`，en 再提取数字组重排 CategoryN）→
// 不在 category_list → 默认分类+validationFailReason（reason 同步覆盖）→
// 返回 (class, parsed[reason] 缺省 "")
// parseDetectionResult（Python :329-334）：postProcess → getIntentIDAndName →
// buildOutputMap（exclude_defaults=True 等价：字段==默认值(-1/"")时剔除）
// getIntentIDAndName（Python :426-433）：idx 顺序扫描 category_list，命中返回
// {classificationId: idx, name: nameList[idx]}；否则 {0, 默认名}
```

- [ ] **Step 4: 测试通过 → commit（path-limited，同 Task 1 模式）**

---

### Task 3: 模型双路径创建

**Files:**
- Modify: `intent_detection_comp.go`
- Test: 同 `_test.go`

- [ ] **Step 1: 写失败测试（spec §5 覆盖点 7）**

```go
// TestCreateLLMInstance_双路径 spec 拍板 A：model_id 走 ModelMgr / 直构 / 双缺报错
func TestCreateLLMInstance_双缺配置报错(t *testing.T) {
	e := NewIntentDetectionExecutable(&IntentDetectionCompConfig{})
	_, err := e.createLLMInstance(context.Background(), nil)
	if err == nil || !errors.Is(err, exception.StatusComponentIntentDetectionInvokeCallFailed) {
		t.Fatalf("期望 COMPONENT_INTENT_DETECTION_INVOKE_CALL_FAILED, got %v", err)
	}
}

// TestCreateLLMInstance_直构路径 client/request config 齐备 → 返回 *llm.Model 非 nil
func TestCreateLLMInstance_直构路径(t *testing.T) { /* NewIntentDetectionExecutable(&Cfg{ModelClientConfig:..., ModelConfig:...}) → createLLMInstance 返回 LLMInvoker 非 nil、类型 *llm.Model */ }
```

- [ ] **Step 2: 实现 createLLMInstance + initializeIfNeeded**

```go
// createLLMInstance 双路径（Python :254-264 一比一）：
// model_id != nil → runner resources_manager ModelMgr.GetModel(ctx, modelID, session)
//                 （返回 model_clients.BaseModelClient，LLMInvoker 双实现 D-18-5）
// 否则 client/request config 任一 nil → StatusComponentIntentDetectionInvokeCallFailed
// 否则 llm.NewModel(clientConfig, requestConfig)（签名按 model.go:73 实参核对）
// initializeIfNeeded（Python :266-276）：未初始化时创建并置位；异常包装
// StatusComponentIntentDetectionLLMInitFailed + .Err(err)
```

- [ ] **Step 3: 测试通过 → commit**

---

### Task 4: 输入组装与历史格式化

**Files:**
- Modify: `intent_detection_comp.go`
- Test: 同 `_test.go`

- [ ] **Step 1: 写失败测试（spec §5 覆盖点 1/2/5）**

```go
// TestPrepareDetectionInputs_槽位填充 Python :278-312 十字段
func TestPrepareDetectionInputs_槽位填充(t *testing.T) {
	e := NewIntentDetectionExecutable(&IntentDetectionCompConfig{
		CategoryNameList: []string{"退款"}, UserPrompt: "客服语境",
		ExampleContent: []string{"示例1", "示例2"},
	})
	inputs := map[string]any{"query": "我的快递到哪了"}
	cur := e.prepareDetectionInputs(inputs, nil)
	// 断言：user_prompt/category_info("0: 默认意图\n1: 退款")/default_class/
	// example_content("示例1\n\n示例2")/chat_history("")/input("我的快递到哪了")/
	// global_intent_map([]any{}) 十槽齐全
}

// TestPrepareDetectionInputs_query缺失报错 Python :300-308 ValidationError 映射
func TestPrepareDetectionInputs_query缺失报错(t *testing.T) {
	// inputs 缺 query → StatusComponentIntentDetectionInputParamError
}

// TestFormatChatHistory_角色映射 Python :314-322：zh 角色名 + 只留 roleMap 内角色 + 尾部截 N 条
func TestFormatChatHistory_角色映射(t *testing.T) {
	// 4 条历史（含未知角色）+ maxTurn 2 → 只格式化最后 2 条内已知角色，"用户: ...\n" 拼接
}
```

- [ ] **Step 2: 实现 prepareDetectionInputs / formatChatHistory / getCategoryInfo**

步骤逐条对照 Python（:247-312）：current_inputs 十字段 update → enable_history 时格式化覆盖 CHAT_HISTORY → enable_input 时 IntentDetectionInput 校验（缺 query 报 InputParamError，`ExceptionUtils.format_validation_error` 等价 error_msg）→ `global_intent_map` 空 list 收尾。`getCategoryInfo`（:247-249）`cid: cname` 按两表 zip 拼接。

- [ ] **Step 3: 测试通过 → commit**

---

### Task 5: invokeEntry 主链（真实 ModelContext 历史读写 + 日志）

**Files:**
- Modify: `intent_detection_comp.go`
- Test: 同 `_test.go`

- [ ] **Step 1: 写真实链失败测试（spec §5 覆盖点 3/8；真实 session 管线 + fake LLM 边界）**

测试基座复用 8.18 `testutil_test.go` 惯例（真实 NodeSessionFacade + 真实 ModelContext；fake 只挂 LLMInvoker）：

```go
// fakeLLMInvoker 网络边界替身（按 feedback-real-chain-testing：仅边界允许）
type fakeLLMInvoker struct{ content string }

func (f *fakeLLMInvoker) Invoke(_ context.Context, _ modelclients.MessagesParam,
	_ ...modelclients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return &llmschema.AssistantMessage{Content: f.content}, nil
}
func (f *fakeLLMInvoker) Stream(_ context.Context, _ modelclients.MessagesParam,
	_ ...modelclients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, errors.New("意图识别组件不使用流式")
}

// TestInvokeEntry_历史真实链 spec §5 覆盖点 3：GetContextWindow 取 N 轮 + user/assistant 写回
func TestInvokeEntry_历史真实链(t *testing.T) {
	e := NewIntentDetectionExecutable(&IntentDetectionCompConfig{
		CategoryNameList: []string{"退款"}, EnableHistory: true, ChatHistoryMaxTurn: 1,
	})
	e.llModel = &fakeLLMInvoker{content: `{"class": "分类1", "reason": "r"}`}
	// 真实 NodeSessionFacade + 真实 ModelContext（testutil 构造）；
	// 先 AddMessages 预置 2 轮历史 → invokeEntry(query=...) →
	// 断言1：格式化进模板的 chat_history 恰 1 轮（fakeLLMInvoker 捕获 messages 数）
	// 断言2：执行后 ModelContext 尾部追加 UserMessage(query) + AssistantMessage(LLM原文)
	// 断言3：返回 map = {"classificationId":1, "reason":"r"}（name "退款" 非默认保留）
}

// TestInvokeEntry_未开历史 不读不写 ModelContext（context 消息数执行前后不变）
```

- [ ] **Step 2: 实现 invokeEntry（Python invoke :207-230 十步一比一）**

```go
// invokeEntry 批入批出槽（Python invoke :207-230 十步）：
// 1 setSession 2 router.SetSession(session._inner 等价——按 8.19 branch_comp
//   SetSession 既有接线) 3 initializeIfNeeded 4 getChatHistoryFromContext
//   （enable_history 且 context!=nil → GetContextWindow(ctx,nil,nil,0,maxTurn)，
//   取 cw.ContextMessages；nil 窗口→空） 5 prepareDetectionInputs
// 6 invokeLLMAndGetResult（模板 Format→ToMessages→llModel.Invoke→取 content；
//   异常包装 InvokeCallFailed + .Err；三段日志：started/调用后/completed，
//   event_type 用 WORKFLOW_COMPONENT_START/END，字段 has_inputs/input_count/
//   has_output/output_length/sensitive_mode/component_id/component_type_str
//   ="IntentDetectionComponent"/session_id——logger.ComponentCommon，英文文案）
// 7/8 writeUser/AssistantMessageToContext（enable_history 且 context!=nil 且
//   query/content 非空才写；query 校验失败静默返回，Python :446-460）
// 9 parseDetectionResult 10 返回
```

- [ ] **Step 3: 测试通过 → commit**

---

### Task 6: Component 组合面 + 路由集成单测

**Files:**
- Modify: `intent_detection_comp.go`
- Test: 同 `_test.go`

- [ ] **Step 1: 写失败测试（spec §5 覆盖点 6）**

```go
// TestIntentDetectionComponent_路由命中 输出 commit 后 BranchRouter 按 id 命中 target
func TestIntentDetectionComponent_路由命中(t *testing.T) {
	c := NewIntentDetectionComponent(&IntentDetectionCompConfig{CategoryNameList: []string{"退款"}})
	_ = c.AddBranch("${classificationId} == 1", []string{"refund_node"}, "b1")
	// 真实图（PregelBuilder，8.3a）AddNode + AddConditionalEdges(nodeID, c.router)，
	// start→intent 注入 query，执行后断言 refund_node 被执行
}

// TestIntentDetectionComponent_默认分支 LLM 输出不可解析 → 落 classificationId=0 分支
```

- [ ] **Step 2: 实现 IntentDetectionComponent（Python :463-490）**

```go
// IntentDetectionComponent 组合面（Python :463-490；executable 懒加载经
// ToExecutableFn 槽首调缓存——8.20 槽优先基座纪律，勿 override 基座方法）
type IntentDetectionComponent struct {
	components.BaseComponentComposable
	cfg         *IntentDetectionCompConfig
	executable  *IntentDetectionExecutable
	router      *flow.BranchRouter
}

// NewIntentDetectionComponent 构造：BaseComponentComposable + router=NewBranchRouter()，
// ToExecutableFn 槽 = 构造 executable 并 SetRouter
// AddComponent（:476-478）graph.AddNode + graph.AddConditionalEdges(nodeID, router)
// AddBranch（:483-487）target string→[string] 归一后 router.AddBranch
// Router()（:489-490）访问器
```

- [ ] **Step 3: 测试通过 → commit**

---

### Task 7: 集成测试

**Files:**
- Create: `tests/integration/agentcore/workflow_intent_detection/intent_detection_test.go`（2026-11-23 勘误：实施落点在 agentcore/ 域下，对齐仓库既有惯例 workflow_branch_comp 等；审查拍板⑤）

- [ ] **Step 1: 写集成测试（真实跨子系统：workflow 组件 + pregel 图引擎 + session + context_engine + foundation/llm，满足规则 7 两层门禁）**

```go
//go:build integration

package workflow_intent_detection

import (...)

// IntentDetectionSuite 集成测试套件
type IntentDetectionSuite struct{ suite.Suite }

func TestSuiteIntentDetection(t *testing.T) { suite.Run(t, new(IntentDetectionSuite)) }

// TestIntentDetection_端到端路由 真实图执行：
// 1 构造 PregelBuilder 图：start → intent（IntentDetectionComponent，fake LLM 边界
//   返回 {"class": "分类1"}）→ 分支条件边 → refund/end 两终端
// 2 真实 NodeSessionFacade + 真实 ModelContext（EnableHistory=true）
// 3 断言：intent 输出经 commit 后可见（spec §3 状态归属——io_state node 前缀）
// 4 断言：路由命中 refund 节点；LLM 返回垃圾文本时命中 end（默认分支）
// 5 断言：ModelContext 追加两条消息（历史写回真实链）
// 6 断言：同节点连续两轮 set_outputs 快照替换（同键覆盖，spec §3 最新覆盖语义）

// TestIntentDetection_不commit不可见 stage 语义钉子：仅跑 invoke 不触发引擎 commit
// → get_global 等价读取拿不到输出（spec §3 staged 不可见 + 8.20 memory 同款）
```

- [ ] **Step 2: 本地门禁**

```bash
go build ./... && go vet ./...
go test -tags=integration ./tests/integration/workflow_intent_detection/...
go run ./cmd/integration-audit -verify
```

- [ ] **Step 3: commit**

---

### Task 8: 收口——doc.go / 计划状态 / 全量回归 / 表达式缺键核对

- [ ] **Step 1: D-23-2 实证核对**：对 ExpressionCondition 既有 Go 实现喂缺失键（如 `${classificationId}` 未落任何值时求值），核对与 Python `get_global` 缺键返回 None → 转 `var_None` 比较语义是否一致；不一致则登记 spec §6 偏差（不重写 expression.go）

- [ ] **Step 2: doc.go llm 子包文件树补 intent_detection_comp.go 条目；grep 锚点核查** `grep -rn IntentDetection internal/ tests/ cmd/` 仅本节产物命中

- [ ] **Step 3: IMPLEMENTATION_PLAN.md** 8.22 行 ☐→✅（memory 证实已完成）、8.23 行 ☐→✅

- [ ] **Step 4: 全量回归 + 提交**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...
git commit internal/openuapclaw/core/workflow/components/llm/... IMPLEMENTATION_PLAN.md \
  docs/... -m "feat(8.23): IntentDetectionComp 收口（doc/计划状态/全量回归）"
git push origin main
```

---

## 自查记录

- **Spec 覆盖**：§2 类型清单→Task 1/2/3/4/5/6；§3 状态归属→Task 5 断言+Task 7 钉子 3/4/6；§5 覆盖点 1-8→Task 1(1)/2(4,5)/3(7)/4(2)/5(3,8)/6(6)/7(集成)；§6 D-23-1→Task 2、D-23-2→Task 8、D-23-3→Task 3；§8 删除清单→Task 8 grep 锚点
- **占位符**：无 TBD/TODO；pyLiteralEval 给实现要点+落码要求，buildOutputMap/文案常量给逐字来源行号
- **类型一致性**：LLMInvoker/llmschema/ModelMgr.GetModel/GetContextWindow(6 参)/cw.ContextMessages/三个 StatusComponentIntentDetection* 均经源码核实（本轮 grep 证据在会话）
