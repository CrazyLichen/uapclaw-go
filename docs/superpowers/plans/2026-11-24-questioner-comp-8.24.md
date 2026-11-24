# 8.24 QuestionerComp 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 一比一移植 Python `questioner_comp.py`（888 行）为 Go `QuestionerComp` 交互提问组件，多轮人机交互参数收集，中断经 checkpoint 恢复。

**Architecture:** 与 8.23 IntentDetectionComp 同包同机械：`components/llm` 新增单文件一比一映射；状态机单结构体 status 分派（D-24-5 定案，Python 子类仅重写 handle_event/默认 status，Go 以 status 分派逐行对照 from_state=拷贝+置位）；中断走既有 `InteractWithResult`（PanicGraphInterrupt，5.7 已回填）；`validate_and_convert_type` 入 pythonsem（拍板②）。

**Tech Stack:** Go + 既有 `exception`/`logger`/`pythonsem`/`foundation.prompt`/`foundation.llm`/`context_engine`/`session` 基座。

**真相源与规约:**
- Python: `/home/opensource/agent-core/openjiuwen/core/workflow/components/llm/questioner_comp.py`（下记 `:N` 行号）
- Spec: `docs/superpowers/specs/2026-11-24-questioner-comp-8.24-design.md`
- 机械模板: `internal/openuapclaw/core/workflow/components/llm/intent_detection_comp.go`
- 提示词/文案一比一纪律：中英模板逐字复制禁止改写（feedback_prompt_verbatim_copy）
- 测试：真实链路纪律（feedback-real-chain-testing）；全量回归带标签（CGO_ENABLED=1 + `-tags "sqlite_fts5 test"`）
- 共享 checkout git 纪律：path-limited commit、porcelain 核对、禁裸 amend（feedback 系列）

---

### Task 1: pythonsem 新增 ValidateAndConvertType

**Files:**
- Modify: `internal/common/pythonsem/helpers.go`（追加导出函数，尾部）
- Test: `internal/common/pythonsem/helpers_test.go`（追加用例）

- [ ] **Step 1: 写失败测试**（追加到 helpers_test.go）

```go
func TestValidateAndConvertType(t *testing.T) {
	// (value, expected, wantResult, wantOK)
	cases := []struct {
		val  any
		typ  string
		want any
		ok   bool
	}{
		{nil, "string", nil, false},                        // Python :322-323
		{"abc", "string", "abc", true},                     // str(value)
		{123, "string", "123", true},
		{true, "integer", nil, false},                      // bool≠int :332-334
		{int64(5), "integer", int64(5), true},
		{5.0, "integer", int64(5), true},                   // 整值 float :337-340
		{5.5, "integer", nil, false},
		{" 7 ", "integer", int64(7), true},                 // str 解析 :342-345
		{"x", "integer", nil, false},
		{true, "number", nil, false},                       // :350-351
		{int64(3), "number", 3.0, true},                    // :352-353
		{2.5, "number", 2.5, true},
		{"3.5", "number", 3.5, true},
		{true, "boolean", true, true},                      // :361-362
		{" True ", "boolean", true, true},                  // strip+lower :363-368
		{"yes", "boolean", nil, false},
		{int64(9), "unknown", "9", true},                   // 未知类型回落 str :372-374
	}
	for _, c := range cases {
		got, ok := ValidateAndConvertType(c.val, c.typ)
		if ok != c.ok || (ok && !PyDeepEqual(got, c.want)) {
			t.Errorf("ValidateAndConvertType(%v,%q) = (%v,%v), want (%v,%v)", c.val, c.typ, got, ok, c.want, c.ok)
		}
	}
}
```

- [ ] **Step 2: 运行确认失败** — `go test ./internal/common/pythonsem/ -run TestValidateAndConvertType`，预期编译错误 `undefined: ValidateAndConvertType`

- [ ] **Step 3: 实现**（helpers.go 导出函数区追加，中文注释）

```go
// ValidateAndConvertType 按 Python 期望类型校验并转换值（Python questioner_comp.py
// validate_and_convert_type :309-377 一比一；结构偏差 D-24-2：Python 定义在组件内，
// 2026-11-24 拍板②迁 pythonsem 共享）。返回 (转换值, 是否有效)；失败 (nil, false)。
func ValidateAndConvertType(value any, expectedType string) (any, bool) {
	if value == nil {
		return nil, false
	}
	switch expectedType {
	case "string":
		return PyStr(value), true
	case "integer":
		if IsBool(value) {
			return nil, false
		}
		if IsInt(value) {
			return PyInt(value), true
		}
		if f, ok := value.(float64); ok {
			if f == float64(int64(f)) {
				return int64(f), true
			}
			return nil, false
		}
		if s, ok := value.(string); ok {
			n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
			if err != nil {
				return nil, false
			}
			return n, true
		}
		return nil, false
	case "number":
		if IsBool(value) {
			return nil, false
		}
		if IsNumber(value) {
			return PyFloat(value), true
		}
		if s, ok := value.(string); ok {
			f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
			if err != nil {
				return nil, false
			}
			return f, true
		}
		return nil, false
	case "boolean":
		if b, ok := value.(bool); ok {
			return b, true
		}
		if s, ok := value.(string); ok {
			switch strings.ToLower(strings.TrimSpace(s)) {
			case "true":
				return true, true
			case "false":
				return false, true
			}
			return nil, false
		}
		return nil, false
	default:
		return PyStr(value), true
	}
}
```

注意：helpers.go 需已 import `strconv`（若无则补）；`PyFloat` 对 int64 输入返回 float64（核对既有实现，签名不符则以 PyFloat 实际行为为准调整用例）。

- [ ] **Step 4: 运行测试通过** — `go test ./internal/common/pythonsem/ -run TestValidateAndConvertType -v` 全绿

- [ ] **Step 5: 提交**（path-limited）

```bash
git add internal/common/pythonsem/helpers.go internal/common/pythonsem/helpers_test.go
git commit internal/common/pythonsem/helpers.go internal/common/pythonsem/helpers_test.go \
  -m "feat(pythonsem): 新增 ValidateAndConvertType（questioner 四类类型转换，bool≠int 边界）"
```

---

### Task 2: 常量/双语模板/枚举/数据模型

**Files:**
- Create: `internal/openuapclaw/core/workflow/components/llm/questioner_comp.go`
- Modify: `internal/openuapclaw/core/workflow/components/llm/doc.go`（文件树加条目）
- Test: `internal/openuapclaw/core/workflow/components/llm/questioner_comp_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestQuestionerDefaultTemplate(t *testing.T) {
	zh := questionerDefaultTemplate("zh")
	if len(zh) != 2 || zh[0].GetRole() != "system" || zh[1].GetRole() != "user" {
		t.Fatalf("zh template roles wrong")
	}
	if !strings.Contains(zh[0].GetContent(), "信息收集助手") {
		t.Fatalf("zh system template content mismatch")
	}
	en := questionerDefaultTemplate("en")
	if !strings.Contains(en[0].GetContent(), "information collection assistant") {
		t.Fatalf("en system template content mismatch")
	}
}

func TestQuestionerConfigDefaults(t *testing.T) {
	cfg := &QuestionerConfig{}
	if cfg.ResponseType != ResponseTypeReplyDirectly || cfg.MaxResponse != 3 ||
		cfg.ChatHistoryMaxRounds != 5 || cfg.AcceptLanguage != "zh" ||
		cfg.ExtractFieldsFromResponse != true {
		t.Fatalf("config defaults mismatch Python :130-143")
	}
}
```

- [ ] **Step 2: 运行确认失败** — 编译错误 undefined

- [ ] **Step 3: 实现**（新文件，结构体→枚举→常量→var→导出→非导出排序，注释中文）

```go
// 8.24 QuestionerComp——交互提问组件（Python questioner_comp.py 一比一）。
// 多轮交互参数收集：START 直发问/LLM 抽取 → USER_INTERACT 挂起恢复循环 → END 清状态重入。
// 中断经 session.interact（PanicGraphInterrupt）；状态 questioner_state 存节点会话。
// 已知偏差见 spec docs/superpowers/specs/2026-11-24-questioner-comp-8.24-design.md §5。
package llm

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/pythonsem"
	"github.com/uapclaw/uapclaw-go/internal/common/security"
	forllm "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/prompt"
	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	graphexe "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/executable"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components"
)
```

结构体区（按 Python :104-251 顺序）：

```go
// QuestionerFieldInfo 抽取字段定义（Python FieldInfo :120-126 一比一）
type QuestionerFieldInfo struct {
	// FieldName 字段名（LLM 输出 JSON 键名锚点）
	FieldName string `json:"field_name"`
	// Description 字段描述（追问文案/必填名单的回落显示名）
	Description string `json:"description"`
	// Type 期望类型（string/integer/number/boolean，默认 string）
	Type string `json:"type"`
	// CnFieldName 中文名（优先用于追问文案）
	CnFieldName string `json:"cn_field_name"`
	// Required 是否必填（缺则追问/超限报错）
	Required bool `json:"required"`
	// DefaultValue 兜底默认值（始终未抽到时回填）
	DefaultValue any `json:"default_value"`
}

// QuestionerConfig 组件配置（Python QuestionerConfig :129-143 一比一；默认值同 Python dataclass）
type QuestionerConfig struct {
	// ModelID 预注册模型 ID（nil ≙ None）
	ModelID *string
	// ModelClientConfig 模型客户端配置
	ModelClientConfig *llmschema.ModelClientConfig
	// ModelConfig 模型请求配置
	ModelConfig *llmschema.ModelRequestConfig
	// ResponseType 响应类型（唯一成员 reply_directly）
	ResponseType string
	// QuestionContent 固定问题模板（{{}} 占位符用输入额外字段渲染）
	QuestionContent string
	// ExtractFieldsFromResponse 是否启用字段抽取
	ExtractFieldsFromResponse bool
	// FieldNames 字段表
	FieldNames []QuestionerFieldInfo
	// MaxResponse 最大追问轮数（超限抛 101074）
	MaxResponse int
	// WithChatHistory 是否启用对话历史
	WithChatHistory bool
	// ChatHistoryMaxRounds 历史最大轮数
	ChatHistoryMaxRounds int
	// ExtraPromptForFields_extraction 抽取附加约束（{{extra_info}} 槽）
	ExtraPromptForFieldsExtraction string
	// ExampleContent 抽取示例（{{example}} 槽）
	ExampleContent string
	// AcceptLanguage 语言（zh/en）
	AcceptLanguage string
}

// newQuestionerConfigDefaults 补 Python dataclass 默认值（构造入口统一调用）
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

// QuestionerOutputCache 单轮输出缓存（Python OutputCache :160-163）
type QuestionerOutputCache struct {
	// UserResponse 用户回答（str 或 dict）
	UserResponse any
	// Question 当前问题
	Question string
	// KeyFields 本轮抽取键值
	KeyFields map[string]any
}

// QuestionerOutput 节点输出（Python QuestionerOutput :166-169；key_fields 经
// extra=allow 展开为顶层键，见 formatQuestionerOutput）
type QuestionerOutput struct {
	// UserResponse 用户回答
	UserResponse any `json:"user_response,omitempty"`
	// Question 问题
	Question string `json:"question,omitempty"`
	// Extra 抽取键值（Python extra=allow 顶层展开的等价承载）
	Extra map[string]any `json:"-"`
}
```

枚举区：

```go
// QuestionerExecutionStatus 执行状态（Python ExecutionStatus :104-107，值为字符串）
type QuestionerExecutionStatus string

const (
	// QuestionerStatusStart START 态（Python "start"）
	QuestionerStatusStart QuestionerExecutionStatus = "start"
	// QuestionerStatusUserInteract 交互态（Python "user_interact"）
	QuestionerStatusUserInteract QuestionerExecutionStatus = "user_interact"
	// QuestionerStatusEnd 终态（Python "end"）
	QuestionerStatusEnd QuestionerExecutionStatus = "end"
)

// QuestionerEvent 事件（Python QuestionerEvent :110-113）
type QuestionerEvent string

const (
	// QuestionerEventStart 事件：进入 START
	QuestionerEventStart QuestionerEvent = "start"
	// QuestionerEventUserInteract 事件：进入 USER_INTERACT
	QuestionerEventUserInteract QuestionerEvent = "user_interact"
	// QuestionerEventEnd 事件：进入 END
	QuestionerEventEnd QuestionerEvent = "end"
)

// ResponseTypeReplyDirectly 响应类型（Python ResponseType :116-118 单成员，死枚举仍复刻）
const ResponseTypeReplyDirectly = "reply_directly"
```

常量区（**模板逐字复制 Python :27-101，禁止改写**；此处从略——实施时从 Python 文件 :27-101 逐行复制 QUESTIONER_SYSTEM_TEMPLATE_ZH/EN、QUESTIONER_USER_TEMPLATE_ZH/EN、CONTINUE_ASK_STATEMENT_ZH/EN、START_STR/END_STR/USER_INTERACT_STR、SUB_PLACEHOLDER_PATTERN、WORKFLOW_CHAT_HISTORY、TEMPLATE_NAME、QUESTIONER_STATE_KEY="questioner_state"）：

```go
const (
	questionerStartStr       = "start"
	questionerEndStr         = "end"
	questionerUserInteractStr = "user_interact"
	questionerSubPlaceholderPattern = `\{\{([^}]*)\}\}`
	questionerContinueAskStatementZh = "请您提供{non_extracted_key_fields_names}相关的信息"
	questionerContinueAskStatementEn = "Please provide information related to: {non_extracted_key_fields_names}"
	questionerWorkflowChatHistory    = "workflow_chat_history"
	questionerTemplateName           = "questioner"
	questionerStateKey               = "questioner_state"
)

// questionerSystemTemplateZh / questionerSystemTemplateEn / questionerUserTemplateZh /
// questionerUserTemplateEn —— 从 Python :38-88 逐字复制（含 {{required_name}} 等占位符）
```

`questionerDefaultTemplate(acceptLanguage)`（Python :91-101 一比一，返回 `[]llmschema.BaseMessage{NewSystemMessage(...), NewUserMessage(...)}`）。

`questionerDefaultConfig` / `questionerDefaultConfigFromLanguage`（Python :146-152）。

- [ ] **Step 4: doc.go 加文件树条目** `questioner_comp.go  # 8.24 交互提问组件（Python questioner_comp.py 一比一）`

- [ ] **Step 5: 测试通过** + **Step 6: 提交**（path-limited，同 Task 1 样式）

---

### Task 3: 状态机（单结构体 status 分派）

**Files:** 同 Task 2 两文件

- [ ] **Step 1: 失败测试**

```go
func TestQuestionerStateHandleEvent(t *testing.T) {
	st := NewQuestionerState() // status=start
	if !st.IsFreshState() { t.Fatal("fresh state expected") }
	// START + USER_INTERACT → Interact
	st2 := st.HandleEvent(QuestionerEventUserInteract)
	if st2.Status != QuestionerStatusUserInteract { t.Fatal("want interact") }
	// USER_INTERACT 只响应 END
	st3 := st2.HandleEvent(QuestionerEventUserInteract)
	if st3.Status != QuestionerStatusUserInteract { t.Fatal("interact should stay") }
	// END + START 回环新建（Python :249-250）
	st4 := st3.HandleEvent(QuestionerEventEnd)
	if st4.Status != QuestionerStatusEnd { t.Fatal("want end") }
	st5 := st4.HandleEvent(QuestionerEventStart)
	if st5.Status != QuestionerStatusStart || st5.ResponseNum != 0 { t.Fatal("loop back to new start") }
	if st2.IsUndergoingInteraction() != true { t.Fatal("undergoing interaction") }
}

func TestQuestionerStateSerializeRoundTrip(t *testing.T) {
	st := NewQuestionerState()
	st.ResponseNum = 2
	st.ExtractedKeyFields = map[string]any{"name": "张三"}
	st.Question = "请您提供身份证号相关的信息"
	st = st.HandleEvent(QuestionerEventUserInteract)
	raw := st.Serialize()
	got, err := QuestionerStateDeserialize(raw)
	if err != nil { t.Fatal(err) }
	if got.Status != QuestionerStatusUserInteract || got.ResponseNum != 2 ||
		got.ExtractedKeyFields["name"] != "张三" {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
}
```

- [ ] **Step 2: 确认失败** → **Step 3: 实现**

```go
// QuestionerState 组件状态（Python QuestionerState/Start/Interact/End :172-251；
// D-24-5 定案：Python 三个子类仅重写 handle_event 与默认 status、from_state 恒为
// 字段拷贝+置位，Go 以单结构体 status 分派逐行对照，序列化形状不变）
type QuestionerState struct {
	// ResponseNum 已收反馈轮数（每次 interact 反馈 +1）
	ResponseNum int `json:"response_num"`
	// UserResponse 用户回答（str/dict）
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

// HandleEvent 状态分派（Python 四类 handle_event :187-194/:212-217/:231-234/:248-251 合流；
// 分支表逐条对照：Start→{Interact,End}；Interact→{End}；End→{Start=回环新建}）
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

// Serialize Python :184-185（model_dump ≙ JSON marshal）
func (s *QuestionerState) Serialize() map[string]any {
	raw, _ := json.Marshal(s)
	m := map[string]any{}
	_ = json.Unmarshal(raw, &m)
	return m
}

// QuestionerStateDeserialize Python deserialize :179-182（validate + handle_event
// 按存量 status 恢复为对应子类形态；解析失败包装 StatusComponentQuestionerStateInitFailed）
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
```

注意 `QuestionerStateDeserialize` 的 Python 语义：反序列化后按 status 调 handle_event，各子类 handle_event 对"本态不响应的事件"返回 self——Go 合流版 HandleEvent 的默认 return s 已覆盖；**回环分支（End+Start）与 Python 一致会新建**。

- [ ] **Step 4: 通过** → **Step 5: 提交**

---

### Task 4: QuestionerUtils 五助手

**Files:** 同前

- [ ] **Step 1: 失败测试**

```go
func TestQuestionerFormatTemplate(t *testing.T) {
	// 正常渲染
	got := questionerFormatTemplate("您好 {{name}}，{{age}}岁", map[string]any{"name": "张三", "age": 18})
	if got != "您好 张三，18岁" { t.Fatalf("got %q", got) }
	// D-24-1 缺键怪癖：Python user_fields.get(key) 缺失 → str(None)="None"
	got = questionerFormatTemplate("{{missing}}", map[string]any{})
	if got != "None" { t.Fatalf("D-24-1 quirk: got %q", got) }
}

func TestQuestionerFormatContinueAsk(t *testing.T) {
	fields := []QuestionerFieldInfo{
		{FieldName: "id_card", CnFieldName: "身份证号", Description: "身份证"},
		{FieldName: "addr", Description: "住址"},
	}
	if got := questionerFormatContinueAsk(fields, "zh"); got != "请您提供身份证号, 住址相关的信息" {
		t.Fatalf("zh: %q", got)
	}
	if got := questionerFormatContinueAsk(fields, "en"); got != "Please provide information related to: 身份证号, 住址" {
		t.Fatalf("en: %q", got)
	}
}

func TestQuestionerIsValidValue(t *testing.T) {
	invalid := []any{nil, "", map[string]any{}, []any{}, "null", " NONE "}
	for _, v := range invalid {
		if questionerIsValidValue(v) { t.Errorf("%v should be invalid", v) }
	}
	valid := []any{false, int64(0), 0.0, "0", "abc", map[string]any{"k": 1}}
	for _, v := range valid {
		if !questionerIsValidValue(v) { t.Errorf("%v should be valid", v) }
	}
}

func TestQuestionerFormatOutputExcludeDefaults(t *testing.T) {
	cache := &QuestionerOutputCache{
		UserResponse: "", // 等于默认 → 被剔除
		Question:     "q1",
		KeyFields:    map[string]any{"name": "张三"},
	}
	out := questionerFormatQuestionerOutput(cache)
	if _, ok := out["user_response"]; ok { t.Fatal("user_response should be excluded (default)") }
	if out["question"] != "q1" || out["name"] != "张三" { t.Fatalf("out=%v", out) }
	// 全默认 → 空 dict（Python exclude_defaults 极端情况）
	empty := questionerFormatQuestionerOutput(&QuestionerOutputCache{KeyFields: map[string]any{}})
	if len(empty) != 0 { t.Fatalf("want empty map, got %v", empty) }
}
```

- [ ] **Step 2: 确认失败** → **Step 3: 实现**（包级函数，Python QuestionerUtils :254-377）

```go
// questionerFormatTemplate 模板渲染（Python format_template :255-265 一比一）：
// {{key}} 占位符替换为 user_fields[key] 的 str()；D-24-1 缺键怪癖——Python
// user_fields.get(key) 缺失得 None → str(None)="None"，Go 用 pythonsem.PyStr(nil)="None"
// 对齐；re.sub 整体失败返回 ""（Python except 返回 ""）
func questionerFormatTemplate(template string, userFields map[string]any) string {
	re := regexp.MustCompile(questionerSubPlaceholderPattern)
	var paniced bool
	replace := func(match []string) string {
		defer func() { if recover() != nil { paniced = true } }()
		return pythonsem.PyStr(pythonsem.MapValue(userFields, match[1]))
	}
	result := re.ReplaceAllStringFunc(template, func(m string) string {
		sub := re.FindStringSubmatch(m)
		return replace(sub)
	})
	if paniced {
		return ""
	}
	return result
}

// questionerFormatContinueAsk 追问语句（Python :267-277 一比一：cn_field_name or
// description 回落、", " 拼接、双语模板选择）
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

// questionerFormatQuestionerOutput 输出构造（Python :279-284 一比一）：
// QuestionerOutput(**key_fields) extra 展开 + user_response/question 赋值 +
// model_dump(exclude_defaults=True)——user_response=="" 与 question=="" 剔除，
// extra 键保留
func questionerFormatQuestionerOutput(cache *QuestionerOutputCache) map[string]any {
	out := map[string]any{}
	for k, v := range cache.KeyFields {
		out[k] = v
	}
	if !pythonsem.PyEqual(cache.UserResponse, "") { // 非 "" 才输出（exclude_defaults）
		out["user_response"] = cache.UserResponse
	}
	if cache.Question != "" {
		out["question"] = cache.Question
	}
	return out
}

// questionerValidateInputs 输入校验（Python validate_inputs :286-295 一比一）：
// query 允许 str/dict/None（D-24-3），缺省 ""；非法类型抛 101070
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
// 后续 _update_state_of_key_fields 的 truthy 过滤才拦 falsy）
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
```

`questionerValidateAndConvertType` 直接调 `pythonsem.ValidateAndConvertType`（Task 1）。

- [ ] **Step 4: 通过** → **Step 5: 提交**

---

### Task 5: QuestionerExecutable 构造/校验/状态存取/双路径 LLM

**Files:** 同前

- [ ] **Step 1: 失败测试**

```go
func TestQuestionerValidateConfig(t *testing.T) {
	// response_type 非法 → 101071
	err := validateQuestionerResponseType("bad")
	if err == nil { t.Fatal("want error") }
	// extract_fields 且字段表空 → 101071
	err = validateQuestionerExtractKeyFields(true, nil)
	if err == nil { t.Fatal("want error") }
	// 字段名空 → 101071
	err = validateQuestionerExtractKeyFields(true, []QuestionerFieldInfo{{Description: "x"}})
	if err == nil { t.Fatal("want error") }
	// max_response<=0 → 101071
	err = validateQuestionerMaxResponse(0)
	if err == nil { t.Fatal("want error") }
}

func TestQuestionerLoadStoreState(t *testing.T) {
	sess := newTestNodeSession(t)
	// 空会话 → 新建 START 态
	st := loadQuestionerStateFromSession(sess)
	if st.Status != QuestionerStatusStart { t.Fatal("want fresh") }
	// 存取往返
	st = st.HandleEvent(QuestionerEventUserInteract)
	storeQuestionerStateToSession(st, sess)
	got := loadQuestionerStateFromSession(sess)
	if got.Status != QuestionerStatusUserInteract { t.Fatalf("roundtrip: %+v", got) }
}
```

- [ ] **Step 2: 确认失败** → **Step 3: 实现**

```go
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
	// testInvoker 测试注入缝（D-24-7 同 D-23-4 惯例，集成测试专用，Python 所无）
	testInvoker LLMInvoker
}

// NewQuestionerExecutable 构造（Python __init__ :745-753 一比一）：
// 校验配置 → 默认配置 → prompt 初始化
func NewQuestionerExecutable(cfg *QuestionerConfig) (*QuestionerExecutable, error) {
	cfg = newQuestionerConfigDefaults(cfg)
	if err := validateQuestionerResponseType(cfg.ResponseType); err != nil { return nil, err }
	if err := validateQuestionerExtractKeyFields(cfg.ExtractFieldsFromResponse, cfg.FieldNames); err != nil { return nil, err }
	if err := validateQuestionerMaxResponse(cfg.MaxResponse); err != nil { return nil, err }
	dflt := questionerDefaultConfigFromLanguage(cfg.AcceptLanguage)
	return &QuestionerExecutable{
		cfg:            cfg,
		defaultCfg:     dflt,
		promptTemplate: prompt.NewPromptTemplate(dflt.promptTemplate),
	}, nil
}

// validateQuestionerResponseType Python :790-797
func validateQuestionerResponseType(rt string) error {
	if rt != ResponseTypeReplyDirectly {
		return exception.BuildError(exception.StatusComponentQuestionerConfigError,
			exception.WithMsg(fmt.Sprintf("response type %s is invalid", rt)))
	}
	return nil
}

// validateQuestionerExtractKeyFields Python :776-788
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

// validateQuestionerMaxResponse Python :768-774
func validateQuestionerMaxResponse(n int) error {
	if n <= 0 {
		return exception.BuildError(exception.StatusComponentQuestionerConfigError,
			exception.WithMsg("max response must be greater than 0"))
	}
	return nil
}

// loadQuestionerStateFromSession Python _load_state_from_session :755-761
func loadQuestionerStateFromSession(sess *session.NodeSessionFacade) *QuestionerState {
	raw, err := sess.GetState(state.StateKey(questionerStateKey))
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

// createLLMInstance 双路径模型创建（Python _create_llm_instance :831-841；8.23
// D-23-3 同款防御：mgr==nil / GetModel 未命中；皆缺 → 101075）
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

// initializeIfNeeded Python _initialize_if_needed :843-853（异常包装 101075）
```

`import state "…/session/state"` 需要补。`questionerDefaultConfig` 结构体持 `promptTemplate []llmschema.BaseMessage` 字段（Python dataclass field）。

- [ ] **Step 4: 通过** → **Step 5: 提交**

---

### Task 6: QuestionerDirectReplyHandler 三态处理

**Files:** 同前

- [ ] **Step 1: 失败测试**（question_content 直发问挂起 + 抽取模式抽齐 END 两主链）

```go
func TestQuestionerHandlerStartWithQuestionContent(t *testing.T) {
	sess := newTestNodeSession(t)
	cfg := &QuestionerConfig{QuestionContent: "您好 {{name}}", ExtractFieldsFromResponse: false}
	e, _ := NewQuestionerExecutable(cfg)
	handler := newQuestionerDirectReplyHandler(e, NewQuestionerState())
	result, err := handler.handle(context.Background(),
		map[string]any{"query": "", "name": "张三"}, sess, nil)
	if err != nil { t.Fatal(err) }
	if result["question"] != "您好 张三" { t.Fatalf("question: %v", result) }
	if handler.state.Status != QuestionerStatusUserInteract { t.Fatal("should be interact (挂起)") }
}

func TestQuestionerHandlerExtractionComplete(t *testing.T) {
	sess := newTestNodeSession(t)
	cfg := &QuestionerConfig{
		ExtractFieldsFromResponse: true,
		FieldNames: []QuestionerFieldInfo{
			{FieldName: "name", Description: "姓名", Type: "string", Required: true},
		},
	}
	e, _ := NewQuestionerExecutable(cfg)
	e.llModel = &fakeQuestionerLLM{content: `{"name": "张三"}`}
	e.initialized = true
	handler := newQuestionerDirectReplyHandler(e, NewQuestionerState())
	result, err := handler.handle(context.Background(),
		map[string]any{"query": "我叫张三"}, sess, nil)
	if err != nil { t.Fatal(err) }
	if result["name"] != "张三" { t.Fatalf("extracted: %v", result) }
	if handler.state.Status != QuestionerStatusEnd { t.Fatal("want END") }
}

func TestQuestionerHandlerExtractionContinueAsk(t *testing.T) {
	// 必填缺失 → 追问 + INTERACT 态 + key_fields 清空
	sess := newTestNodeSession(t)
	cfg := &QuestionerConfig{
		ExtractFieldsFromResponse: true,
		FieldNames: []QuestionerFieldInfo{
			{FieldName: "name", Description: "姓名", Required: true},
			{FieldName: "id", CnFieldName: "身份证号", Description: "证件", Required: true},
		},
	}
	e, _ := NewQuestionerExecutable(cfg)
	e.llModel = &fakeQuestionerLLM{content: `{"name": "张三"}`}
	e.initialized = true
	handler := newQuestionerDirectReplyHandler(e, NewQuestionerState())
	result, err := handler.handle(context.Background(), map[string]any{"query": "我叫张三"}, sess, nil)
	if err != nil { t.Fatal(err) }
	if handler.state.Status != QuestionerStatusUserInteract { t.Fatal("want interact") }
	if result["question"] != "请您提供身份证号相关的信息" { t.Fatalf("ask: %v", result["question"]) }
	if _, ok := result["name"]; ok { t.Fatal("key_fields should be cleared on continue-ask") }
}

func TestQuestionerHandlerMaxResponseExceeded(t *testing.T) {
	// response_num 已达 max_response 且仍缺必填 → 101074
	sess := newTestNodeSession(t)
	cfg := &QuestionerConfig{
		ExtractFieldsFromResponse: true, MaxResponse: 1,
		FieldNames: []QuestionerFieldInfo{{FieldName: "id", CnFieldName: "身份证号", Description: "证件", Required: true}},
	}
	e, _ := NewQuestionerExecutable(cfg)
	e.llModel = &fakeQuestionerLLM{content: `{}`}
	e.initialized = true
	st := NewQuestionerState()
	st.ResponseNum = 1 // 已用完
	st = st.HandleEvent(QuestionerEventUserInteract)
	handler := newQuestionerDirectReplyHandler(e, st)
	_, err := handler.handle(context.Background(), map[string]any{"query": ""}, sess, nil)
	if err == nil { t.Fatal("want 101074") }
}

// fakeQuestionerLLM LLMInvoker 替身（仅网络边界，feedback-real-chain-testing 允许）
type fakeQuestionerLLM struct{ content string }
func (f *fakeQuestionerLLM) Invoke(ctx context.Context, msgs []llmschema.BaseMessage,
	opts ...forllm.InvokeOption) (*llmschema.AssistantMessage, error) {
	return &llmschema.AssistantMessage{Content: f.content}, nil
}
```

`fakeQuestionerLLM` 若与 `testutil_test.go` 既有 fake LLMInvoker 重复则复用既有（`grep type.*LLMInvoker` 检查 testutil），签名以 `llm_comp.go:149` 的 `LLMInvoker` 接口为准调整。

- [ ] **Step 2: 确认失败** → **Step 3: 实现**（Python :380-741，方法归属不变）

```go
// questionerDirectReplyHandler 直接回复处理器（Python QuestionerDirectReplyHandler :380-741）
type questionerDirectReplyHandler struct {
	cfg     *QuestionerConfig
	llModel LLMInvoker
	state   *QuestionerState
	promptT *prompt.PromptTemplate
	query   any // Python self._query（str 或 dict）
}

func newQuestionerDirectReplyHandler(e *QuestionerExecutable, st *QuestionerState) *questionerDirectReplyHandler {
	return &questionerDirectReplyHandler{cfg: e.cfg, llModel: e.llModel, state: st, promptT: e.promptTemplate}
}

// handle 状态分发（Python :407-414 一比一）
func (h *questionerDirectReplyHandler) handle(ctx context.Context, inputs map[string]any,
	sess *session.NodeSessionFacade, context iface.ModelContext) (map[string]any, error) {
	switch h.state.Status {
	case QuestionerStatusStart:
		return h.handleStartState(ctx, inputs, sess, context)
	case QuestionerStatusUserInteract:
		return h.handleUserInteractState(ctx, inputs, sess, context)
	case QuestionerStatusEnd:
		return h.handleEndState(inputs), nil
	}
	return map[string]any{}, nil
}
```

`handleStartState`（Python :416-453 十步一比一）：validateInputs → OutputCache → query 取值（str/dict 两态）→ writeUserMessageToContext → getLatestChatHistory → `isSetQuestionContent` 分支（formatTemplate 渲染、置 question、转 USER_INTERACT、writeAssistantMessage）→ `needExtractFields` 分支（initialExtractFromChatHistory、continue_ask 判定转态/写 assistant JSON 帧）→ 两者皆缺抛 101072 → 返回 formatQuestionerOutput。

`handleUserInteractState`（Python :455-488）：getLatestHumanFeedback → OutputCache(question=state.question, user_response=query) → writeUserMessage → getLatestChatHistory → `user_response = chat_history[-1].content`（历史非空取末条 content）→ `isSetQuestionContent && !needExtractFields` 分支 END → `needExtractFields` 分支 repeatExtract → 皆缺 101072。

`handleEndState`（Python :490-494）：`QuestionerOutput(**state.extracted_key_fields)` + user_response/question → exclude_defaults map。

辅助方法逐一对齐：`isSetQuestionContent`（:496-497）/`needExtractFields`（:499-501，注意 `len(field_names) > len(state.extracted_key_fields)` 字段数与**抽取数**比较）/`initialExtractFromChatHistory` 与 `repeatExtractFromChatHistory`（:503-517，两方法体相同仍按 Python 保持两个方法）/`getLatestChatHistory`（:519-532，with_chat_history 且 context 非 nil → GetContextWindow(dialogue_round)；**末条为 assistant 或空表 → 追加 UserMessage(query)**，dict query 包 `[dict]`）/`buildLLMInputs`+`createPromptTemplateKeywords`（:534-555，params_list "field: desc" 换行拼接、required_name 中英分隔符 "、"/", "、数字后缀文案双语）/`invokeLLMForExtraction`（:557-614，前 log WORKFLOW_COMPONENT_START → invoke → 后 log WORKFLOW_COMPONENT_END → 剥壳两正则（```json 与 '''json，flags=IGNORECASE）→ json 失败 error log 返回空 dict → 非 dict 抛 101076 → is_valid_value 过滤 → validateAndConvertFields）/`validateAndConvertFields`（:616-654，field_type_map 缺键回落 "string"，失败 warning log 含 field_name/value(敏感模式置 nil)/expected_type）/`filterNonExtractedKeyFields`（:656-661）/`updateStateOfKeyFields`（:663-666，truthy 才进）/`updateParamDefaultValue`（:668-676，default_value 非零值且未抽到才兜底）/`incrementStateOfResponseNum`/`exceedMaxResponse`（:678-682）/`checkIfContinueAsk`（:684-702，缺必填+未超限 → 追问+key_fields.clear；缺必填+超限 → 101074；齐 → key_fields.update(state.extracted_key_fields)）/`invokeLLMAndParseResult`（:704-711）/`getLatestHumanFeedback`（:713-716，**`for range response_num+1` 次 InteractWithResult 取反馈**——恢复后重放已收集反馈语义，最后一次的新反馈生效；尾 increment）/`updateQuestionerStatesQuestion`（:718-719）/`writeUserMessageToContext`/`writeAssistantMessageToContext`（:721-741，with_chat_history 且 content 非空才写）。

`getLatestHumanFeedback` Go 签名（ctx + sess）：`res, err := sess.InteractWithResult(ctx, h.state.Question)`；首次调用会 PanicGraphInterrupt 向上传播（5.7 语义），恢复后返回反馈值。

- [ ] **Step 4: 通过** → **Step 5: 提交**

---

### Task 7: invoke 编排 + 组合面 + doc.go

**Files:** 同前

- [ ] **Step 1: 失败测试**（挂起全链 + END 清状态重入）

```go
func TestQuestionerInvokeSuspendAndResume(t *testing.T) {
	sess := newTestNodeSession(t)
	cfg := &QuestionerConfig{
		ExtractFieldsFromResponse: true,
		FieldNames: []QuestionerFieldInfo{
			{FieldName: "name", Description: "姓名", Required: true},
		},
	}
	e, _ := NewQuestionerExecutable(cfg)
	e.llModel = &fakeQuestionerLLM{content: `{"name": "张三"}`}
	e.initialized = true
	// 首调：抽齐 → END → 清状态（invoke 末尾 state 非 interact → 存新 state）
	result, err := e.invokeEntry(context.Background(), map[string]any{"query": "我叫张三"}, sess, nil)
	if err != nil { t.Fatal(err) }
	if result["name"] != "张三" { t.Fatalf("result: %v", result) }
	got := loadQuestionerStateFromSession(sess)
	if got.Status != QuestionerStatusStart || got.ResponseNum != 0 { t.Fatalf("state cleared: %+v", got) }
}

func TestQuestionerComponentCompose(t *testing.T) {
	cfg := &QuestionerConfig{
		ExtractFieldsFromResponse: true, MaxResponse: 2,
		FieldNames: []QuestionerFieldInfo{{FieldName: "q", Description: "问题", Required: false}},
	}
	comp := NewQuestionerComponent(cfg)
	if comp.ToExecutableFn == nil { t.Fatal("ToExecutableFn slot must be set") }
	// 两次调用同一实例缓存
	a, b := comp.ToExecutableFn(), comp.ToExecutableFn()
	if a != b { t.Fatal("executable should be cached") }
}
```

- [ ] **Step 2: 确认失败** → **Step 3: 实现**

```go
// invokeEntry 批入批出槽（Python invoke :803-829 十一步一比一）
func (e *QuestionerExecutable) invokeEntry(ctx context.Context, inputs map[string]any,
	sess *session.NodeSessionFacade, context iface.ModelContext) (map[string]any, error) {
	// 1. 从会话加载状态
	stateFromSession := loadQuestionerStateFromSession(sess)
	// 2. 交互中 → 恢复态；否则新建
	var currentState *QuestionerState
	if stateFromSession.IsUndergoingInteraction() {
		currentState = stateFromSession
	} else {
		currentState = NewQuestionerState()
	}
	// 3. START 事件
	currentState = currentState.HandleEvent(QuestionerEventStart)
	// 4. 懒初始化
	if err := e.initializeIfNeeded(ctx); err != nil {
		return nil, exception.BuildError(exception.StatusComponentQuestionerInvokeCallFailed,
			exception.WithMsg("failed to initialize llm if needed"), exception.WithCause(err))
	}
	// 5. 直回复处理（safe 版：state 经结果回传，Python :865-873 '_state' 缝；
	// Go 用 handler.state 直读替代 map 键传递——语义等价，偏差 D-24-8）
	invokeResult := map[string]any{}
	handler := newQuestionerDirectReplyHandler(e, currentState)
	result, herr := handler.handle(ctx, inputs, sess, context)
	if herr != nil {
		return nil, herr
	}
	e.state = handler.state
	for k, v := range result {
		invokeResult[k] = v
	}
	currentState = handler.state
	// 6. 交互中 → 存状态并发起交互（InteractWithResult 首调 PanicGraphInterrupt
	// 挂起，恢复后返回反馈值——Python :822 丢弃返回值，Go 同样忽略）
	if currentState.IsUndergoingInteraction() {
		storeQuestionerStateToSession(currentState, sess)
		_, _ = sess.InteractWithResult(ctx, invokeResult["question"])
	} else {
		// 7. 正常完成清状态支持重入（Python :824-827）
		newState := NewQuestionerState()
		e.state = newState
		storeQuestionerStateToSession(newState, sess)
	}
	return invokeResult, nil
}

// QuestionerComponent 组合面（Python QuestionerComponent :881-888）
type QuestionerComponent struct {
	components.WorkflowComponent
	cfg        *QuestionerConfig
	executable graphexe.GeneralExecutor
}

// NewQuestionerComponent 构造（槽位模式，勿 override 基座两方法——8.20 拍板⚠️）
func NewQuestionerComponent(cfg *QuestionerConfig, opts ...func(*QuestionerComponent)) *QuestionerComponent {
	c := &QuestionerComponent{cfg: newQuestionerConfigDefaults(cfg)}
	for _, opt := range opts {
		opt(c)
	}
	c.ToExecutableFn = c.toExecutableCached
	return c
}

// toExecutableCached 首调构造缓存（Python to_executable :887-888 + executable 属性）
func (c *QuestionerComponent) toExecutableCached() graphexe.GeneralExecutor {
	if c.executable == nil {
		e, err := NewQuestionerExecutable(c.cfg)
		if err != nil {
			panic(err) // Python to_executable 同步抛错，Go 以 panic 对齐构造期失败
		}
		c.executable = e
	}
	return c.executable
}
```

注意：`invokeEntry` 挂上基座槽（参照 8.23 `invokeEntry` 与基座 `InvokeFn` 接线方式，照抄 intent_detection_comp.go 对应装配段）。集成测试若需注入 fake LLM，加 `WithQuestionerLLM(inv LLMInvoker)` 选项（D-24-7，同 D-23-4 惯例：仅测试装配路径消费）。

- [ ] **Step 4: 通过** → **Step 5: 提交**

---

### Task 8: 集成测试（中断恢复端到端）+ 6.25 回填

**Files:**
- Create: `tests/integration/agentcore/workflow_questioner/questioner_interaction_test.go`（含 `//go:build integration` 标签 + `suite.Run(`）
- Modify: `tests/integration/agentcore/runner/runner_test.go:133` 与 `doc.go:24`（A 类缺口标注改写）

- [ ] **Step 1: 写集成测试**（真实链路：Runner + ResourceMgr + BaseWorkflow + Questioner 节点 + interaction 中断/恢复，跨 ≥3 子系统）

```go
//go:build integration

package workflow_questioner_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"
	// ……按既有 integration suite 惯例 import（runner/agentcore 同款基类）
)

// TestQuestionerSuite 8.24 交互提问组件中断恢复端到端：
// 首调 workflow → questioner 节点 INTERACTION 帧 + TASK_STATUS_INTERRUPT →
// checkpoint 落盘；InteractiveInput 二次调用 → pending 节点恢复 → 下游节点输出。
// 对齐 Python test_runner_agent_resource_management 欠的 Questioner __interaction__
// → 二次调用恢复场景（6.25 ⤵️ 回填兑现）。
type TestQuestionerSuite struct{ suite.Suite }

func TestQuestioner(t *testing.T) { suite.Run(t, new(TestQuestionerSuite)) }

func (s *TestQuestionerSuite) TestQuestionerInteractionResume() {
	// 1. 组建 workflow：start → questioner(收集 name 必填) → recorder(下游节点)
	//    questioner 经 LLM 抽取（WithQuestionerLLM 注入 fake：首轮 {"name": null}，
	//    恢复轮 {"name": "张三"}，按对话历史区分）
	// 2. 首调 Runner.run_workflow 等价 Go 入口：收到 WorkflowOutput(INPUT_REQUIRED)
	//    断言 result 含 type=INTERACTION 帧（payload.value=追问文案）
	// 3. InteractiveInput 等价构造（update(node_id, "我叫张三")）二次调用同 session
	// 4. 断言：workflow COMPLETED；questioner 节点输出含 name=张三 与 user_response；
	//    recorder 下游节点真实执行；checkpoint 清理（post_workflow_execute CLEAR）
}
```

具体断言与装配细节按 `tests/integration/agentcore/runner/runner_test.go` 既有 suite 基类（GetResourceMgr 等）与 8.14 BaseWorkflow 测试的真实装配惯例补齐；**mock 只允许 LLM 网络边界**（WithQuestionerLLM），workflow/session/checkpointer 全真实。

- [ ] **Step 2: 本地跑通** — `go run ./cmd/integration-audit -verify`（两层门禁自检）+ `CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/agentcore/workflow_questioner/`

- [ ] **Step 3: 6.25 注释改写**（回填兑现）

`runner_test.go:133-135` 改为：
```go
// 注：Python 原测试的交互中断恢复部分（Questioner __interaction__ → 二次调用恢复）
// 已随 8.24 QuestionerComp 移植补齐（tests/integration/agentcore/workflow_questioner）。
```
`doc.go:24` 同步（"计划 8.14-8.24 未移植，待工作流组件移植后补充" → "已随 8.14-8.24 移植补齐"）。

- [ ] **Step 4: 提交**（path-limited：新测试目录 + 两处注释文件）

---

### Task 9: IMPLEMENTATION_PLAN 同步 + 删除清单 + 全量回归

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`（:531 行 ☐→✅ + 追加二十二次注记）

- [ ] **Step 1: IMPLEMENTATION_PLAN 更新**

`:531` 行改为：
```
| 8.24 | ✅ | QuestionerComp | 交互提问组件（888 行一比一；D-24-1..8；中断经 InteractWithResult/PanicGraphInterrupt；状态机单结构体 status 分派 D-24-5；pythonsem ValidateAndConvertType；集成端到端兑现 6.25 回填；ReAct 工具级中断桥 ⤴️8.26） | `openjiuwen/core/workflow/components/llm/` |
```
实施顺序注记区追加（沿用既有格式）：
```
> **8.x 实施顺序（2026-11-24 二十二次记录）**：8.24 一次收口（questioner_comp.py 888 行一比一；D-24-1..8；层2 中断桥勘误——workflow-as-tool 独立 emitter/WorkflowOutput(INPUT_REQUIRED)/pending_component_id 过滤单发已记 spec §2；pythonsem 增 ValidateAndConvertType；集成 WorkflowAgent/Runner 中断恢复端到端兑现 6.25 A 类缺口回填）；下一节 8.25（KnowledgeRetrievalComp，13.26-13.28 依赖已就绪）→ 8.26（ReactComponent，层2 ReAct 中断桥回填点），8.13 drawable 缝继续压后。原注记保留。
```

- [ ] **Step 2: 删除清单锚点核查（规则 8）**

```bash
grep -rn "Questioner" internal/ tests/ cmd/ --include="*.go" | grep -v "components/llm/questioner_comp\|integration/agentcore/workflow_questioner\|integration/agentcore/runner\|doc.go"
```
预期：0 命中（除本组件与已改写注释）；旧实现不存在（纯新增节），核对 `grep -rn "QuestionerComp internal/"` 无重复实现。

- [ ] **Step 3: 全量本地验证**

```bash
export GOPROXY=https://goproxy.cn,direct
go build ./... && go vet ./...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/... ./tests/integration/... 
go test -cover ./internal/openuapclaw/core/workflow/components/llm/ ./internal/common/pythonsem/
```
预期：零 FAIL；llm 包按文件整体 ≥85%、pythonsem 不低于既有基线。

- [ ] **Step 4: 提交 + 推送**（path-limited IMPLEMENTATION_PLAN.md；全部 commits push）

---

## 已知偏差登记（实施中如有新增须回报 spec）

| # | 条目 | 处置 |
|---|---|---|
| D-24-1 | format_template 缺键 "None" | 对齐复刻（Task 4） |
| D-24-2 | ValidateAndConvertType 入 pythonsem | 拍板②（Task 1） |
| D-24-3 | query 三态（str/dict/None） | any + 类型分支（Task 4） |
| D-24-4 | is_valid_value "null"/"none" 特判非全 falsy | 组件内特判（Task 4） |
| D-24-5 | 状态机四子类 → 单结构体 status 分派 | Python 子类仅重写 handle_event/默认 status，逐行对照等价（Task 3） |
| D-24-6 | ReAct 工具级中断桥（层 2） | ⤴️8.26 回填，本节集成走 WorkflowAgent/Runner 直调 |
| D-24-7 | WithQuestionerLLM 测试注入缝 | Python 所无，同 D-23-4 惯例（Task 7） |
| D-24-8 | `_state` map 缝 → handler.state 直读 | Python :818/:872 result['_state'] 传递，Go 直读 handler.state 语义等价（Task 7） |

## 删除清单（规则 8）

纯新增无被替代旧实现。锚点核查见 Task 9 Step 2。
