# 9.73 SignalDetector 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现自演化信号检测模块（ConversationSignalDetector + TeamSignalDetector + base 补全），将轨迹/消息/评估结果转换为结构化 EvolutionSignal。

**Architecture:** 三文件并行实现——signal.go 补全枚举/工厂/指纹、from_conv.go 对话信号检测器、team.go 团队信号检测器。严格一比一复刻 Python，提示词不翻译。ConversationSignalDetector 直接调用 Model.Invoke，TeamSignalDetector 使用 llm_resilience.InvokeTextWithRetry。

**Tech Stack:** Go 1.22+, regexp, encoding/json, context

---

### Task 1: trajectory 包新增 CrossMemberMetaKeys 常量

**Files:**
- Modify: `internal/evolving/trajectory/types.go`
- Test: `internal/evolving/trajectory/types_test.go`

- [ ] **Step 1: 在 types.go 全局变量区新增 CrossMemberMetaKeys**

在 `internal/evolving/trajectory/types.go` 的 `// ──────────────────────────── 全局变量 ────────────────────────────` 区块中新增：

```go
var (
	// CrossMemberMetaKeys 跨成员元数据键集合。
	// 用于判断 Trajectory 是否处于团队协作成员上下文。
	//
	// 对应 Python: openjiuwen/agent_evolving/trajectory/aggregator.py
	// CROSS_MEMBER_META_KEYS = frozenset({"invoke_id", "parent_invoke_id", "child_invokes"})
	CrossMemberMetaKeys = map[string]bool{
		"invoke_id":        true,
		"parent_invoke_id": true,
		"child_invokes":    true,
	}
)
```

- [ ] **Step 2: 在 types_test.go 新增测试**

```go
func TestCrossMemberMetaKeys_包含预期键(t *testing.T) {
	expectedKeys := []string{"invoke_id", "parent_invoke_id", "child_invokes"}
	for _, key := range expectedKeys {
		if !CrossMemberMetaKeys[key] {
			t.Errorf("CrossMemberMetaKeys missing key %q", key)
		}
	}
}

func TestCrossMemberMetaKeys_不包含无关键(t *testing.T) {
	if CrossMemberMetaKeys["member_id"] {
		t.Errorf("CrossMemberMetaKeys should not contain %q", "member_id")
	}
}
```

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/trajectory/ -run TestCrossMemberMetaKeys -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/evolving/trajectory/types.go internal/evolving/trajectory/types_test.go
git commit -m "feat(evolving): add CrossMemberMetaKeys constant in trajectory package"
```

---

### Task 2: signal.go 补全 — 枚举 + ToDict + 工厂函数 + 指纹

**Files:**
- Modify: `internal/evolving/signal/signal.go`
- Modify: `internal/evolving/signal/signal_test.go`

- [ ] **Step 1: 在 signal.go 枚举区新增 EvolutionCategory 和 EvolutionTarget**

在 `// ──────────────────────────── 枚举 ────────────────────────────` 区块中新增：

```go
// EvolutionCategory 演化类别枚举，保留向后兼容。
//
// 对应 Python: openjiuwen/agent_evolving/signal/base.py EvolutionCategory(str, Enum)
type EvolutionCategory string

const (
	// EvolutionCategorySkillExperience 技能经验
	EvolutionCategorySkillExperience EvolutionCategory = "skill_experience"
	// EvolutionCategoryNewSkill 新技能
	EvolutionCategoryNewSkill EvolutionCategory = "new_skill"
)

// EvolutionTarget 演化目标层枚举，标识技能经验作用的目标层。
//
// 对应 Python: openjiuwen/agent_evolving/signal/base.py EvolutionTarget(str, Enum)
type EvolutionTarget string

const (
	// EvolutionTargetDescription 描述层
	EvolutionTargetDescription EvolutionTarget = "description"
	// EvolutionTargetBody 主体层
	EvolutionTargetBody EvolutionTarget = "body"
	// EvolutionTargetScript 脚本层
	EvolutionTargetScript EvolutionTarget = "script"
)
```

- [ ] **Step 2: 在 EvolutionSignal 结构体下方新增 ToDict 方法**

在结构体区块中，`EvolutionSignal` 定义之后新增：

```go
// ToDict 将信号转换为字典形式。
// 对应 Python: EvolutionSignal.to_dict()
func (s *EvolutionSignal) ToDict() map[string]any {
	d := map[string]any{
		"type":       s.SignalType,
		"section":    s.Section,
		"excerpt":    s.Excerpt,
		"skill_name": s.SkillName,
	}
	if s.Context != nil {
		d["context"] = s.Context
	}
	return d
}
```

- [ ] **Step 3: 在 signal.go 新增 SignalOption 类型和选项函数**

在全局变量区块之后、导出函数区块之前，添加 SignalOption 相关类型。由于 Go 的声明顺序要求，将 SignalOption 放在导出函数区块中。

在 `// ──────────────────────────── 导出函数 ────────────────────────────` 区块中新增：

```go
// SignalOption 演化信号构造选项函数。
type SignalOption func(*evolutionSignalConfig)

// evolutionSignalConfig MakeEvolutionSignal 的内部配置。
type evolutionSignalConfig struct {
	source   *string
	toolName *string
	skillName *string
	context  map[string]any
}

// WithSource 设置信号来源。
func WithSource(source string) SignalOption {
	return func(cfg *evolutionSignalConfig) { cfg.source = &source }
}

// WithToolName 设置工具名称。
func WithToolName(toolName string) SignalOption {
	return func(cfg *evolutionSignalConfig) { cfg.toolName = &toolName }
}

// WithSkillName 设置技能名称。
func WithSkillName(skillName string) SignalOption {
	return func(cfg *evolutionSignalConfig) { cfg.skillName = &skillName }
}

// WithContext 设置诊断上下文。
func WithContext(context map[string]any) SignalOption {
	return func(cfg *evolutionSignalConfig) { cfg.context = context }
}

// MakeEvolutionSignal 创建演化信号，合并 source/tool_name 到 context。
//
// 对应 Python: make_evolution_signal(signal_type, section, excerpt, tool_name, skill_name, source, context)
func MakeEvolutionSignal(signalType, section, excerpt string, opts ...SignalOption) *EvolutionSignal {
	cfg := &evolutionSignalConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	mergedContext := map[string]any{}
	for k, v := range cfg.context {
		mergedContext[k] = v
	}
	if cfg.source != nil {
		if _, exists := mergedContext["source"]; !exists {
			mergedContext["source"] = *cfg.source
		}
	}
	if cfg.toolName != nil {
		if _, exists := mergedContext["tool_name"]; !exists {
			mergedContext["tool_name"] = *cfg.toolName
		}
	}

	var skillName *string
	if cfg.skillName != nil {
		skillName = cfg.skillName
	}

	var context map[string]any
	if len(mergedContext) > 0 {
		context = mergedContext
	}

	return &EvolutionSignal{
		SignalType: signalType,
		Section:    section,
		Excerpt:    excerpt,
		SkillName:  skillName,
		Context:    context,
	}
}

// GetSignalSource 从信号 context 中读取 source 元数据，向后兼容。
//
// 对应 Python: get_signal_source(signal)
func GetSignalSource(sig *EvolutionSignal) *string {
	if sig.Context == nil {
		return nil
	}
	source, ok := sig.Context["source"]
	if !ok || source == nil {
		return nil
	}
	s := fmt.Sprintf("%v", source)
	return &s
}

// MakeSignalFingerprint 构建信号去重指纹。
//
// 对应 Python: make_signal_fingerprint(signal)
// 返回 [4]string{signal_type, context.tool_name, skill_name, excerpt[:200]}
func MakeSignalFingerprint(sig *EvolutionSignal) [4]string {
	context := sig.Context
	if context == nil {
		context = map[string]any{}
	}
	toolName := ""
	if v, ok := context["tool_name"]; ok && v != nil {
		toolName = fmt.Sprintf("%v", v)
	}
	skillName := ""
	if sig.SkillName != nil {
		skillName = *sig.SkillName
	}
	excerpt := sig.Excerpt
	if len(excerpt) > 200 {
		excerpt = excerpt[:200]
	}
	return [4]string{sig.SignalType, toolName, skillName, excerpt}
}
```

注意：需要在 import 中添加 `"fmt"`。

- [ ] **Step 4: 在 signal_test.go 新增测试**

```go
func TestEvolutionCategory_值对齐(t *testing.T) {
	if EvolutionCategorySkillExperience != "skill_experience" {
		t.Errorf("EvolutionCategorySkillExperience = %q, want %q", EvolutionCategorySkillExperience, "skill_experience")
	}
	if EvolutionCategoryNewSkill != "new_skill" {
		t.Errorf("EvolutionCategoryNewSkill = %q, want %q", EvolutionCategoryNewSkill, "new_skill")
	}
}

func TestEvolutionTarget_值对齐(t *testing.T) {
	if EvolutionTargetDescription != "description" {
		t.Errorf("EvolutionTargetDescription = %q, want %q", EvolutionTargetDescription, "description")
	}
	if EvolutionTargetBody != "body" {
		t.Errorf("EvolutionTargetBody = %q, want %q", EvolutionTargetBody, "body")
	}
	if EvolutionTargetScript != "script" {
		t.Errorf("EvolutionTargetScript = %q, want %q", EvolutionTargetScript, "script")
	}
}

func TestEvolutionSignal_ToDict_含Context(t *testing.T) {
	skill := "my_skill"
	sig := EvolutionSignal{
		SignalType: "execution_failure",
		Section:    "Troubleshooting",
		Excerpt:    "error occurred",
		SkillName:  &skill,
		Context:    map[string]any{"source": "passive_conversation"},
	}
	d := sig.ToDict()
	if d["type"] != "execution_failure" {
		t.Errorf("ToDict type = %v, want %q", d["type"], "execution_failure")
	}
	if d["section"] != "Troubleshooting" {
		t.Errorf("ToDict section = %v, want %q", d["section"], "Troubleshooting")
	}
	if d["excerpt"] != "error occurred" {
		t.Errorf("ToDict excerpt = %v, want %q", d["excerpt"], "error occurred")
	}
	if _, ok := d["context"]; !ok {
		t.Error("ToDict missing context key")
	}
}

func TestEvolutionSignal_ToDict_无Context(t *testing.T) {
	sig := EvolutionSignal{
		SignalType: "low_score",
		Section:    "Troubleshooting",
		Excerpt:    "score=0.00",
	}
	d := sig.ToDict()
	if _, ok := d["context"]; ok {
		t.Error("ToDict should not contain context when nil")
	}
}

func TestMakeEvolutionSignal_基本(t *testing.T) {
	sig := MakeEvolutionSignal("execution_failure", "Troubleshooting", "error",
		WithSource("passive_conversation"),
		WithToolName("bash"),
		WithSkillName("my_skill"),
	)
	if sig.SignalType != "execution_failure" {
		t.Errorf("SignalType = %q, want %q", sig.SignalType, "execution_failure")
	}
	if sig.SkillName == nil || *sig.SkillName != "my_skill" {
		t.Errorf("SkillName = %v, want %q", sig.SkillName, "my_skill")
	}
	if sig.Context["source"] != "passive_conversation" {
		t.Errorf("Context[source] = %v, want %q", sig.Context["source"], "passive_conversation")
	}
	if sig.Context["tool_name"] != "bash" {
		t.Errorf("Context[tool_name] = %v, want %q", sig.Context["tool_name"], "bash")
	}
}

func TestMakeEvolutionSignal_不覆盖已有Context(t *testing.T) {
	sig := MakeEvolutionSignal("test", "section", "excerpt",
		WithContext(map[string]any{"source": "existing"}),
		WithSource("new_source"),
	)
	if sig.Context["source"] != "existing" {
		t.Errorf("Context[source] = %v, want %q (setdefault should not overwrite)", sig.Context["source"], "existing")
	}
}

func TestMakeEvolutionSignal_无选项时Context为nil(t *testing.T) {
	sig := MakeEvolutionSignal("test", "section", "excerpt")
	if sig.Context != nil {
		t.Errorf("Context = %v, want nil when no options", sig.Context)
	}
}

func TestGetSignalSource_有source(t *testing.T) {
	sig := MakeEvolutionSignal("test", "section", "excerpt", WithSource("passive_conversation"))
	src := GetSignalSource(sig)
	if src == nil || *src != "passive_conversation" {
		t.Errorf("GetSignalSource = %v, want %q", src, "passive_conversation")
	}
}

func TestGetSignalSource_无source(t *testing.T) {
	sig := &EvolutionSignal{SignalType: "test", Section: "s", Excerpt: "e"}
	src := GetSignalSource(sig)
	if src != nil {
		t.Errorf("GetSignalSource = %v, want nil", src)
	}
}

func TestMakeSignalFingerprint_基本(t *testing.T) {
	sig := MakeEvolutionSignal("execution_failure", "Troubleshooting", "error in bash command",
		WithToolName("bash"),
		WithSkillName("my_skill"),
	)
	fp := MakeSignalFingerprint(sig)
	if fp[0] != "execution_failure" {
		t.Errorf("fingerprint[0] = %q, want %q", fp[0], "execution_failure")
	}
	if fp[1] != "bash" {
		t.Errorf("fingerprint[1] = %q, want %q", fp[1], "bash")
	}
	if fp[2] != "my_skill" {
		t.Errorf("fingerprint[2] = %q, want %q", fp[2], "my_skill")
	}
	if fp[3] != "error in bash command" {
		t.Errorf("fingerprint[3] = %q, want %q", fp[3], "error in bash command")
	}
}

func TestMakeSignalFingerprint_截断长excerpt(t *testing.T) {
	longExcerpt := strings.Repeat("a", 300)
	sig := &EvolutionSignal{SignalType: "test", Section: "s", Excerpt: longExcerpt}
	fp := MakeSignalFingerprint(sig)
	if len(fp[3]) != 200 {
		t.Errorf("fingerprint[3] length = %d, want 200", len(fp[3]))
	}
}
```

注意：需要在 signal_test.go import 中添加 `"strings"`。

- [ ] **Step 5: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/signal/ -v`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add internal/evolving/signal/signal.go internal/evolving/signal/signal_test.go
git commit -m "feat(evolving): add EvolutionCategory/Target enums, ToDict, MakeEvolutionSignal, GetSignalSource, MakeSignalFingerprint"
```

---

### Task 3: from_conv.go — 常量和辅助函数

**Files:**
- Create: `internal/evolving/signal/from_conv.go`
- Create: `internal/evolving/signal/from_conv_test.go`

- [ ] **Step 1: 创建 from_conv.go 基础框架 — 常量 + 辅助函数**

```go
package signal

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent signal/from_conv 包日志组件常量
const logComponent = logger.ComponentAgentCore

// failureKeywords 匹配执行失败关键词（中英文）。
//
// 对应 Python: _FAILURE_KEYWORDS
var failureKeywords = regexp.MustCompile(
	`error(?!\s*=\s*None)|exception|traceback|failed|failure|timeout|timed out`+
		`|errno|connectionerror|oserror|valueerror|typeerror`+
		`|错误|异常|失败|超时`+
		`|no such file|permission denied|access denied`+
		`|command not found|not recognized`+
		`|module not found`+
		`|econnrefused|econnreset|enoent|enotfound`+
		`|npm err!`,
)

// correctionPatterns 用户纠正模式列表（中英文）。
//
// 对应 Python: _CORRECTION_PATTERNS
var correctionPatterns = []string{
	`不对[，,。!]?`,
	`不是[这那]`,
	`错[了啦]`,
	`应该(是|用|改|换)`,
	`你搞错[了啦]`,
	`这不对`,
	`重新(来|做|执行|尝试)`,
	`你理解错[了啦]`,
	`纠正一下`,
	`我的意思是`,
	`that('s| is) (wrong|incorrect|not right)`,
	`you'?re wrong`,
	`should (be|use|have)`,
	`actually[,，]`,
	`no[,，] (wait|actually)`,
	`correct(ion)?:`,
	`fix(ed)?:`,
}

// correctionPattern 合并后的用户纠正正则。
//
// 对应 Python: _CORRECTION_PATTERN
var correctionPattern = regexp.MustCompile(
	strings.Join(correctionPatterns, "|"),
)

// skillMDPattern 匹配 SKILL.md 路径。
//
// 对应 Python: _SKILL_MD_PATTERN
var skillMDPattern = regexp.MustCompile(`[/\\]+([^/\\]+)[/\\]+SKILL\.md`)

// toolSchemaPattern 匹配工具 schema 输出。
//
// 对应 Python: _TOOL_SCHEMA_PATTERN
var toolSchemaPattern = regexp.MustCompile(`\{'content': '---\\nname: [^\n]+\\ndescription:`)

// userFeedbackPromptCN 中文用户反馈检测提示词。
//
// 对应 Python: _USER_FEEDBACK_PROMPT_CN（原文复刻，不翻译）
const userFeedbackPromptCN = (
	"判断以下用户消息是否包含对当前 skill 的被动纠正或可沉淀的改进反馈。\n" +
		"只有当用户消息明确指出 agent 的理解、步骤、顺序或工具使用需要调整时，" +
		"才认为值得转成演进信号。\n\n" +
		"当前 skill：{skill_name}\n" +
		"最近用户消息：{user_messages}\n\n" +
		`输出 JSON: {{"is_feedback": true/false, "excerpt": "str"}}` + "\n")

// userFeedbackPromptEN 英文用户反馈检测提示词。
//
// 对应 Python: _USER_FEEDBACK_PROMPT_EN（原文复刻，不翻译）
const userFeedbackPromptEN = (
	"Determine whether the following user messages contain passive corrective feedback " +
		"or reusable improvement guidance for the current skill.\n" +
		"Only treat the messages as an evolution signal when the user is clearly correcting " +
		"the agent's understanding, ordering, steps, or tool usage.\n\n" +
		"Current skill: {skill_name}\n" +
		"Recent user messages: {user_messages}\n\n" +
		`Output JSON: {{"is_feedback": true/false, "excerpt": "str"}}` + "\n")

// dataFetchTools 数据获取工具集合。
//
// 对应 Python: _DATA_FETCH_TOOLS
var dataFetchTools = map[string]bool{
	"mcp_fetch_webpage": true,
	"fetch_webpage":     true,
	"web_fetch":         true,
	"search":            true,
	"web_search":        true,
	"google_search":     true,
	"bing_search":       true,
	"view_file":         true,
	"read_file":         true,
	"cat_file":          true,
	"list_directory":    true,
	"ls":                true,
	"get_url":           true,
	"curl":              true,
	"wget":              true,
}

// codeExecTools 代码执行工具集合。
//
// 对应 Python: _CODE_EXEC_TOOLS
var codeExecTools = map[string]bool{
	"code":              true,
	"bash":              true,
	"execute_python_code": true,
	"run_python":        true,
	"exec_code":         true,
	"execute_code":      true,
	"python_exec":       true,
	"run_code":          true,
}

// execContentKeys 可执行内容参数键。
//
// 对应 Python: _EXEC_CONTENT_KEYS
var execContentKeys = []string{
	"code",
	"code_block",
	"script",
	"source",
	"python_code",
	"command",
	"cmd",
	"shell_command",
}

// collaborationSignalTypes 协作信号类型集合。
//
// 对应 Python: _COLLABORATION_SIGNAL_TYPES
var collaborationSignalTypes = map[string]bool{
	"collaboration_send":    true,
	"collaboration_claim":   true,
	"collaboration_view":    true,
	"collaboration_receive": true,
	"collaboration_failure": true,
}

// collaborationFailurePattern 协作失败匹配正则。
//
// 对应 Python: _COLLABORATION_FAILURE_PATTERN
var collaborationFailurePattern = regexp.MustCompile(
	`member.*failed|member.*error|member.*timeout`+
		`|invoke.*exception|spawn.*failed`+
		`|task.*error|task.*timeout`+
		`|collaboration.*failed`+
		`|协作.*失败|成员.*异常|任务.*超时`,
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// getField 统一从 dict 或 struct 读取字段值。
//
// 对应 Python: _get_field(obj, key, default)
func getField(obj any, key string, defaultVal any) any {
	if m, ok := obj.(map[string]any); ok {
		if v, exists := m[key]; exists {
			return v
		}
		return defaultVal
	}
	// 非 dict 类型，尝试通过反射读取
	return defaultVal
}

// extractAroundMatch 返回匹配位置前后的摘录。
//
// 对应 Python: _extract_around_match(content, match, before, after)
func extractAroundMatch(content string, matchStart, matchEnd, before, after int) string {
	start := matchStart - before
	if start < 0 {
		start = 0
	}
	end := matchEnd + after
	if end > len(content) {
		end = len(content)
	}
	return content[start:end]
}

// responseToText 将常见 LLM 响应格式转换为纯文本。
//
// 对应 Python: _response_to_text(response)
func responseToText(response any) string {
	if response == nil {
		return ""
	}
	// AssistantMessage 类型
	if am, ok := response.(*llmschema.AssistantMessage); ok {
		if am != nil {
			return am.GetContent().String()
		}
		return ""
	}
	// dict 类型
	if m, ok := response.(map[string]any); ok {
		if v, exists := m["content"]; exists && v != nil {
			return fmt.Sprintf("%v", v)
		}
		if v, exists := m["text"]; exists && v != nil {
			return fmt.Sprintf("%v", v)
		}
		return ""
	}
	return fmt.Sprintf("%v", response)
}
```

- [ ] **Step 2: 创建 from_conv_test.go — 辅助函数测试**

```go
package signal

import (
	"testing"
)

func TestGetField_dict类型(t *testing.T) {
	obj := map[string]any{"role": "user", "content": "hello"}
	if v := getField(obj, "role", ""); v != "user" {
		t.Errorf("getField role = %v, want %q", v, "user")
	}
	if v := getField(obj, "missing", "default"); v != "default" {
		t.Errorf("getField missing = %v, want %q", v, "default")
	}
}

func TestGetField_非dict类型(t *testing.T) {
	if v := getField("string", "key", "fallback"); v != "fallback" {
		t.Errorf("getField non-dict = %v, want %q", v, "fallback")
	}
}

func TestExtractAroundMatch_基本(t *testing.T) {
	content := "0123456789ABCDEFGHIJ"
	result := extractAroundMatch(content, 10, 12, 3, 3)
	if result != "789ABCEFG" {
		t.Errorf("extractAroundMatch = %q, want %q", result, "789ABCEFG")
	}
}

func TestExtractAroundMatch_边界(t *testing.T) {
	content := "hello world"
	result := extractAroundMatch(content, 0, 5, 100, 100)
	if result != "hello world" {
		t.Errorf("extractAroundMatch = %q, want %q", result, "hello world")
	}
}

func TestResponseToText_dict类型(t *testing.T) {
	resp := map[string]any{"content": "hello world"}
	if v := responseToText(resp); v != "hello world" {
		t.Errorf("responseToText = %q, want %q", v, "hello world")
	}
}

func TestResponseToText_nil(t *testing.T) {
	if v := responseToText(nil); v != "" {
		t.Errorf("responseToText nil = %q, want %q", v, "")
	}
}

func TestFailureKeywords_匹配(t *testing.T) {
	cases := []string{"Error: something", "exception raised", "失败", "超时", "Traceback"}
	for _, c := range cases {
		if !failureKeywords.MatchString(c) {
			t.Errorf("failureKeywords should match %q", c)
		}
	}
}

func TestCorrectionPattern_匹配(t *testing.T) {
	cases := []string{"不对，应该这样", "you're wrong", "actually, it should be", "纠正一下"}
	for _, c := range cases {
		if !correctionPattern.MatchString(c) {
			t.Errorf("correctionPattern should match %q", c)
		}
	}
}

func TestSkillMDPattern_匹配(t *testing.T) {
	input := `/path/to/my_skill/SKILL.md`
	matches := skillMDPattern.FindStringSubmatch(input)
	if len(matches) < 2 || matches[1] != "my_skill" {
		t.Errorf("skillMDPattern matches = %v, want my_skill", matches)
	}
}

func TestDataFetchTools_包含(t *testing.T) {
	if !dataFetchTools["search"] {
		t.Error("dataFetchTools should contain 'search'")
	}
	if !dataFetchTools["read_file"] {
		t.Error("dataFetchTools should contain 'read_file'")
	}
}

func TestCodeExecTools_包含(t *testing.T) {
	if !codeExecTools["bash"] {
		t.Error("codeExecTools should contain 'bash'")
	}
	if !codeExecTools["python_exec"] {
		t.Error("codeExecTools should contain 'python_exec'")
	}
}

func TestCollaborationSignalTypes_包含(t *testing.T) {
	if !collaborationSignalTypes["collaboration_send"] {
		t.Error("collaborationSignalTypes should contain 'collaboration_send'")
	}
	if !collaborationSignalTypes["collaboration_failure"] {
		t.Error("collaborationSignalTypes should contain 'collaboration_failure'")
	}
}
```

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/signal/ -run "TestGetField|TestExtractAroundMatch|TestResponseToText|TestFailureKeywords|TestCorrectionPattern|TestSkillMDPattern|TestDataFetchTools|TestCodeExecTools|TestCollaborationSignalTypes" -v`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```bash
git add internal/evolving/signal/from_conv.go internal/evolving/signal/from_conv_test.go
git commit -m "feat(evolving): add from_conv constants and helper functions for ConversationSignalDetector"
```

---

### Task 4: from_conv.go — ConversationSignalDetector 结构体和核心检测方法

**Files:**
- Modify: `internal/evolving/signal/from_conv.go`
- Modify: `internal/evolving/signal/from_conv_test.go`

- [ ] **Step 1: 在 from_conv.go 结构体区块添加 ConversationSignalDetector**

```go
// ConversationSignalDetector 从 Trajectory 或消息列表中提取演化信号。
//
// 统一在线/离线信号检测接口，支持执行失败、脚本产物、协作信号和
// LLM 辅助用户反馈检测。
//
// 对应 Python: openjiuwen/agent_evolving/signal/from_conv.py ConversationSignalDetector
type ConversationSignalDetector struct {
	// existingSkills 已有技能名称集合，用于 skill_name 解析
	existingSkills map[string]bool
	// llm 可选 LLM 实例，用于被动用户消息检测
	llm *llm.Model
	// model 模型名称
	model string
	// language 语言（"cn" 或 "en"）
	language string
}

// ConvDetectorOption ConversationSignalDetector 构造选项函数。
type ConvDetectorOption func(*ConversationSignalDetector)

// SignalDetector 向后兼容别名。
//
// 对应 Python: SignalDetector = ConversationSignalDetector
type SignalDetector = ConversationSignalDetector
```

- [ ] **Step 2: 在导出函数区块添加构造函数和主入口方法**

```go
// NewConversationSignalDetector 创建 ConversationSignalDetector 实例。
//
// 对应 Python: ConversationSignalDetector(existing_skills)
func NewConversationSignalDetector(opts ...ConvDetectorOption) *ConversationSignalDetector {
	d := &ConversationSignalDetector{
		existingSkills: map[string]bool{},
		language:       "cn",
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// WithExistingSkills 设置已有技能集合。
func WithExistingSkills(skills map[string]bool) ConvDetectorOption {
	return func(d *ConversationSignalDetector) {
		if skills != nil {
			d.existingSkills = skills
		}
	}
}

// BindLLM 绑定可选 LLM 上下文，用于被动用户消息检测。
// 返回自身以支持链式调用。
//
// 对应 Python: ConversationSignalDetector.bind_llm(llm, model, language)
func (d *ConversationSignalDetector) BindLLM(llm *llm.Model, model, language string) *ConversationSignalDetector {
	d.llm = llm
	d.model = model
	if language != "" {
		d.language = language
	}
	return d
}

// Detect 从 Trajectory 或消息列表中检测演化信号，返回去重后的 EvolutionSignal 列表。
//
// 对应 Python: ConversationSignalDetector.detect(trajectory_or_messages)
func (d *ConversationSignalDetector) Detect(trajectoryOrMessages any) []*EvolutionSignal {
	var signals []*EvolutionSignal

	switch v := trajectoryOrMessages.(type) {
	case *trajectory.Trajectory:
		messages := d.ConvertTrajectoryToMessages(v)
		signals = append(signals, d.detectFromMessages(messages)...)
		signals = append(signals, d.detectCollaborationSignals(v)...)
	case []*trajectory.Trajectory:
		// 不应出现，忽略
	default:
		if msgs, ok := toMessageList(trajectoryOrMessages); ok {
			signals = append(signals, d.detectFromMessages(msgs)...)
		}
	}

	return d.deduplicate(signals)
}

// DetectTrajectorySignals 使用常规对话规则检测被动轨迹信号。
//
// 对应 Python: ConversationSignalDetector.detect_trajectory_signals(trajectory, messages)
func (d *ConversationSignalDetector) DetectTrajectorySignals(
	traj *trajectory.Trajectory,
	messages []map[string]any,
) []*EvolutionSignal {
	if messages != nil {
		signals := d.detectFromMessages(messages)
		if traj != nil {
			signals = append(signals, d.detectCollaborationSignals(traj)...)
		}
		return d.deduplicate(signals)
	}
	if traj == nil {
		return nil
	}
	return d.Detect(traj)
}
```

- [ ] **Step 3: 在非导出函数区块添加 toMessageList 辅助函数**

```go
// toMessageList 尝试将 any 转换为 []map[string]any 消息列表。
func toMessageList(v any) ([]map[string]any, bool) {
	if msgs, ok := v.([]map[string]any); ok {
		return msgs, true
	}
	return nil, false
}
```

- [ ] **Step 4: 添加 ConvertTrajectoryToMessages 静态方法**

在导出函数区块添加：

```go
// ConvertTrajectoryToMessages 将 Trajectory.steps 转换为消息列表格式。
//
// 消息格式与 Detect() 期望的格式匹配：
//   - LLM 步骤：从 LLMCallDetail 提取 messages（含 tool_calls）
//   - Tool 步骤：从 ToolCallDetail.call_result 提取工具结果
//
// 对应 Python: ConversationSignalDetector.convert_trajectory_to_messages(trajectory)
func (d *ConversationSignalDetector) ConvertTrajectoryToMessages(traj *trajectory.Trajectory) []map[string]any {
	var messages []map[string]any
	toolCallIDToName := map[string]string{}

	for _, step := range traj.Steps {
		if step.Kind == trajectory.StepKindLLM {
			llmDetail, ok := step.Detail.(*trajectory.LLMCallDetail)
			if !ok {
				continue
			}
			for _, msg := range llmDetail.Messages {
				messages = append(messages, msg)
				toolCalls := getField(msg, "tool_calls", []any{})
				if tcSlice, ok := toolCalls.([]any); ok {
					for _, tc := range tcSlice {
						tcID := fmt.Sprintf("%v", getField(tc, "id", ""))
						tcName := fmt.Sprintf("%v", getField(tc, "name", ""))
						if tcID != "" && tcName != "" {
							toolCallIDToName[tcID] = tcName
						}
					}
				}
			}
		} else if step.Kind == trajectory.StepKindTool {
			toolDetail, ok := step.Detail.(*trajectory.ToolCallDetail)
			if !ok {
				continue
			}
			toolName := toolDetail.ToolName
			toolCallID := toolDetail.ToolCallID
			if toolCallID == "" && toolDetail.Meta != nil {
				if v, ok := toolDetail.Meta["tool_call_id"]; ok {
					toolCallID = fmt.Sprintf("%v", v)
				}
			}

			if toolName == "" && toolCallID != "" {
				toolName = toolCallIDToName[toolCallID]
			}

			resultContent := ""
			if toolDetail.CallResult != nil {
				resultContent = fmt.Sprintf("%v", toolDetail.CallResult)
			}

			toolMsg := map[string]any{
				"role":    "tool",
				"content": resultContent,
			}
			if toolCallID != "" {
				toolMsg["tool_call_id"] = toolCallID
			}
			if toolName != "" {
				toolMsg["name"] = toolName
			}

			messages = append(messages, toolMsg)
		}
	}

	return messages
}
```

- [ ] **Step 5: 添加核心检测方法 detectFromMessages**

在非导出函数区块添加：

```go
// detectFromMessages 扫描消息列表，返回检测到的信号。
//
// 对应 Python: ConversationSignalDetector._detect_from_messages(messages)
func (d *ConversationSignalDetector) detectFromMessages(messages []map[string]any) []*EvolutionSignal {
	var signals []*EvolutionSignal
	type skillReadEntry struct {
		msgIdx    int
		skillName string
	}
	var skillReadHistory []skillReadEntry
	pendingScripts := map[string]string{}
	toolCallIDToName := map[string]string{}

	for msgIdx, msg := range messages {
		role := fmt.Sprintf("%v", getField(msg, "role", ""))
		content := fmt.Sprintf("%v", getField(msg, "content", ""))
		toolCalls := getField(msg, "tool_calls", []any{})

		if role == "assistant" {
			if tcSlice, ok := toolCalls.([]any); ok && len(tcSlice) > 0 {
				if detected := d.detectSkillFromToolCalls(tcSlice); detected != "" {
					skillReadHistory = append(skillReadHistory, skillReadEntry{msgIdx, detected})
				}
				for _, tc := range tcSlice {
					tcID := fmt.Sprintf("%v", getField(tc, "id", ""))
					tcName := fmt.Sprintf("%v", getField(tc, "name", ""))
					if tcID != "" && tcName != "" {
						toolCallIDToName[tcID] = tcName
					}
					if codeExecTools[strings.ToLower(tcName)] {
						code := d.extractCodeFromArgs(tc)
						if code != "" && tcID != "" {
							pendingScripts[tcID] = code
						}
					}
				}
			}
		}

		if role == "tool" || role == "function" {
			toolName := ""
			if v, ok := msg["name"]; ok {
				toolName = fmt.Sprintf("%v", v)
			}
			if toolName == "" {
				if v, ok := msg["tool_name"]; ok {
					toolName = fmt.Sprintf("%v", v)
				}
			}
			toolCallID := ""
			if v, ok := msg["tool_call_id"]; ok {
				toolCallID = fmt.Sprintf("%v", v)
			}
			if toolName == "" && toolCallID != "" {
				toolName = toolCallIDToName[toolCallID]
			}

			activeSkill := d.resolveActiveSkill(msgIdx, skillReadHistory)

			// 脚本产物检测
			if toolCallID != "" {
				if script, exists := pendingScripts[toolCallID]; exists {
					hasFailure := content != "" && failureKeywords.MatchString(content)
					if !hasFailure {
						signals = append(signals, MakeEvolutionSignal(
							"script_artifact", "Scripts", truncateString(script, 600),
							WithToolName(toolName),
							WithSkillName(activeSkill),
							WithSource("passive_conversation"),
						))
					}
					delete(pendingScripts, toolCallID)
				}
			}

			// 跳过数据获取工具
			if dataFetchTools[strings.ToLower(toolName)] {
				continue
			}

			// 执行失败检测
			if content != "" {
				match := failureKeywords.FindStringIndex(content)
				if match != nil {
					// 跳过工具 schema 输出
					if toolSchemaPattern.MatchString(content) {
						continue
					}
					excerpt := extractAroundMatch(content, match[0], match[1], 300, 300)
					var toolNameOpt string
					if toolName != "" {
						toolNameOpt = toolName
					}
					opts := []SignalOption{
						WithSkillName(activeSkill),
						WithSource("passive_conversation"),
					}
					if toolNameOpt != "" {
						opts = append(opts, WithToolName(toolNameOpt))
					}
					signals = append(signals, MakeEvolutionSignal(
						"execution_failure", "Troubleshooting", excerpt, opts...,
					))
				}
			}
		}
	}
	return signals
}

// resolveActiveSkill 返回 msgIdx 处或之前最近读取的技能名称。
//
// 对应 Python: ConversationSignalDetector._resolve_active_skill(msg_idx, skill_read_history)
func (d *ConversationSignalDetector) resolveActiveSkill(msgIdx int, history []struct {
	msgIdx    int
	skillName string
}) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].msgIdx <= msgIdx {
			return history[i].skillName
		}
	}
	return ""
}
```

注意：由于 Go 的类型系统，`resolveActiveSkill` 的 `history` 参数类型需要与 `detectFromMessages` 中的局部类型一致。这里改为使用匿名结构体切片参数。但更好的做法是定义一个包级类型。让我修正：

实际上，为了类型一致性，将 skillReadEntry 定义为包级类型：

在结构体区块添加：
```go
// skillReadEntry 技能读取历史条目。
type skillReadEntry struct {
	msgIdx    int
	skillName string
}
```

然后 `detectFromMessages` 中使用 `[]skillReadEntry`，`resolveActiveSkill` 签名改为：

```go
func (d *ConversationSignalDetector) resolveActiveSkill(msgIdx int, history []skillReadEntry) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].msgIdx <= msgIdx {
			return history[i].skillName
		}
	}
	return ""
}
```

- [ ] **Step 6: 添加 skill 检测和辅助方法**

```go
// detectSkillFromToolCalls 从工具调用中检测 SKILL.md 读取的技能名称。
//
// 对应 Python: ConversationSignalDetector._detect_skill_from_tool_calls(tool_calls)
func (d *ConversationSignalDetector) detectSkillFromToolCalls(toolCalls []any) string {
	for _, tc := range toolCalls {
		name := strings.ToLower(fmt.Sprintf("%v", getField(tc, "name", "")))
		arguments := fmt.Sprintf("%v", getField(tc, "arguments", ""))

		var skillName string

		matched := skillMDPattern.FindStringSubmatch(arguments)
		if len(matched) >= 2 && d.isSkillMDReadTool(name) {
			skillName = matched[1]
		} else if name == "skill_tool" {
			var argsDict map[string]any
			if argsStr, ok := tc.(string); ok {
				_ = json.Unmarshal([]byte(argsStr), &argsDict)
			} else {
				argsDict, _ = tc.(map[string]any)
				if argsDict == nil {
					// 尝试从 arguments 字段解析
					if rawArgs := getField(tc, "arguments", ""); rawArgs != nil {
						if argsStr, ok := rawArgs.(string); ok {
							_ = json.Unmarshal([]byte(argsStr), &argsDict)
						} else if m, ok := rawArgs.(map[string]any); ok {
							argsDict = m
						}
					}
				}
			}
			if argsDict != nil {
				if v, ok := argsDict["skill_name"]; ok && v != nil {
					skillName = fmt.Sprintf("%v", v)
				}
			}
		}

		if skillName != "" && d.isExistingSkill(skillName) {
			return skillName
		}
	}
	return ""
}

// isExistingSkill 检查技能名称是否在已有技能集合中。
//
// 对应 Python: ConversationSignalDetector._is_existing_skill(skill_name)
func (d *ConversationSignalDetector) isExistingSkill(skillName string) bool {
	if len(d.existingSkills) == 0 {
		return true
	}
	return d.existingSkills[skillName]
}

// isSkillMDReadTool 判断工具是否为文件读取类工具。
//
// 对应 Python: ConversationSignalDetector._is_skill_md_read_tool(name)
func (d *ConversationSignalDetector) isSkillMDReadTool(name string) bool {
	if name == "" {
		return true
	}
	return strings.Contains(name, "file") || strings.Contains(name, "read")
}

// inferSkillFromMessages 从消息列表推断当前活跃技能。
//
// 对应 Python: ConversationSignalDetector._infer_skill_from_messages(messages)
func (d *ConversationSignalDetector) inferSkillFromMessages(messages []map[string]any) string {
	var skillReadHistory []skillReadEntry
	for msgIdx, msg := range messages {
		role := fmt.Sprintf("%v", getField(msg, "role", ""))
		toolCalls := getField(msg, "tool_calls", []any{})
		if role == "assistant" {
			if tcSlice, ok := toolCalls.([]any); ok && len(tcSlice) > 0 {
				if detected := d.detectSkillFromToolCalls(tcSlice); detected != "" {
					skillReadHistory = append(skillReadHistory, skillReadEntry{msgIdx, detected})
				}
			}
		}
	}
	return d.resolveActiveSkill(len(messages), skillReadHistory)
}

// extractCodeFromArgs 从代码执行工具调用中提取内联代码或命令内容。
//
// 对应 Python: ConversationSignalDetector._extract_code_from_args(tool_call)
func (d *ConversationSignalDetector) extractCodeFromArgs(toolCall any) string {
	rawArgs := getField(toolCall, "arguments", "")
	if rawArgs == nil {
		return ""
	}

	var argsDict map[string]any
	switch v := rawArgs.(type) {
	case string:
		if err := json.Unmarshal([]byte(v), &argsDict); err != nil {
			return ""
		}
	case map[string]any:
		argsDict = v
	default:
		return ""
	}

	if argsDict == nil {
		return ""
	}
	for _, key := range execContentKeys {
		if val, exists := argsDict[key]; exists {
			if s, ok := val.(string); ok && len(strings.TrimSpace(s)) > 20 {
				return s
			}
		}
	}
	return ""
}
```

- [ ] **Step 7: 运行编译检查**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/evolving/signal/`
Expected: 无编译错误

- [ ] **Step 8: Commit**

```bash
git add internal/evolving/signal/from_conv.go
git commit -m "feat(evolving): add ConversationSignalDetector struct and core detection methods"
```

---

### Task 5: from_conv.go — LLM 用户反馈检测 + 协作信号检测 + 去重

**Files:**
- Modify: `internal/evolving/signal/from_conv.go`
- Modify: `internal/evolving/signal/from_conv_test.go`

- [ ] **Step 1: 添加 DetectUserMessageFeedback 和 DetectUserIntent**

在导出函数区块添加：

```go
// DetectUserMessageFeedback 使用 LLM 判断用户消息是否为被动纠正，返回 user_correction 信号。
//
// 对应 Python: ConversationSignalDetector.detect_user_message_feedback(trajectory_or_messages)
func (d *ConversationSignalDetector) DetectUserMessageFeedback(
	ctx context.Context,
	trajectoryOrMessages any,
) []*EvolutionSignal {
	signals, _ := d.DetectUserIntent(ctx, trajectoryOrMessages)
	var result []*EvolutionSignal
	for _, sig := range signals {
		result = append(result, MakeEvolutionSignal(
			"user_correction",
			"Examples",
			sig.Excerpt,
			WithSkillName(stringPtrValue(sig.SkillName)),
			WithContext(sig.Context),
		))
	}
	return result
}

// DetectUserIntent 使用 LLM 判断被动用户消息，转换为标准信号。
//
// 对应 Python: ConversationSignalDetector.detect_user_intent(trajectory_or_messages)
func (d *ConversationSignalDetector) DetectUserIntent(
	ctx context.Context,
	trajectoryOrMessages any,
) ([]*EvolutionSignal, error) {
	var messages []map[string]any
	switch v := trajectoryOrMessages.(type) {
	case *trajectory.Trajectory:
		messages = d.ConvertTrajectoryToMessages(v)
	default:
		if msgs, ok := toMessageList(trajectoryOrMessages); ok {
			messages = msgs
		}
	}

	// 提取最近 5 条用户消息
	var userMessages []string
	for _, msg := range messages {
		role := fmt.Sprintf("%v", getField(msg, "role", ""))
		content := strings.TrimSpace(fmt.Sprintf("%v", getField(msg, "content", "")))
		if role == "user" && content != "" {
			userMessages = append(userMessages, content)
		}
	}
	if len(userMessages) > 5 {
		userMessages = userMessages[len(userMessages)-5:]
	}
	if len(userMessages) == 0 {
		return nil, nil
	}

	skillName := d.inferSkillFromMessages(messages)
	if skillName == "" {
		return nil, nil
	}

	// 无 LLM 时走 fallback 正则
	if d.llm == nil || d.model == "" {
		return d.fallbackUserFeedbackSignals(userMessages, skillName), nil
	}

	promptTemplate := userFeedbackPromptCN
	if d.language != "cn" {
		promptTemplate = userFeedbackPromptEN
	}
	userText := strings.Join(userMessages, "\n")
	if len(userText) > 2000 {
		userText = userText[:2000]
	}
	prompt := strings.ReplaceAll(promptTemplate, "{skill_name}", skillName)
	prompt = strings.ReplaceAll(prompt, "{user_messages}", userText)

	// 直接调用 Model.Invoke，对齐 Python
	timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	llmMessages := model_clients.NewMessagesParam(llmschema.NewUserMessage(prompt))
	resp, err := d.llm.Invoke(timeoutCtx, llmMessages, model_clients.WithInvokeModel(d.model))
	if err != nil {
		logger.Warn(logComponent).
			Str("method", "DetectUserIntent").
			Str("error", err.Error()).
			Msg("[ConversationSignalDetector] user feedback detection failed")
		return d.fallbackUserFeedbackSignals(userMessages, skillName), nil
	}

	raw := responseToText(resp)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return d.fallbackUserFeedbackSignals(userMessages, skillName), nil
	}
	if _, ok := parsed["is_feedback"]; !ok {
		return d.fallbackUserFeedbackSignals(userMessages, skillName), nil
	}

	isFeedback := false
	switch v := parsed["is_feedback"].(type) {
	case bool:
		isFeedback = v
	default:
		isFeedback = fmt.Sprintf("%v", v) == "true"
	}

	if !isFeedback {
		return nil, nil
	}

	excerpt := strings.TrimSpace(fmt.Sprintf("%v", parsed["excerpt"]))
	if excerpt == "" {
		excerpt = userMessages[len(userMessages)-1]
	}
	return []*EvolutionSignal{d.makeUserFeedbackSignal(excerpt, skillName)}, nil
}
```

- [ ] **Step 2: 添加 fallback 和 makeUserFeedbackSignal 私有方法**

```go
// fallbackUserFeedbackSignals 正则 fallback 检测用户纠正。
//
// 对应 Python: ConversationSignalDetector._fallback_user_feedback_signals(user_messages, skill_name)
func (d *ConversationSignalDetector) fallbackUserFeedbackSignals(userMessages []string, skillName string) []*EvolutionSignal {
	for i := len(userMessages) - 1; i >= 0; i-- {
		if correctionPattern.MatchString(userMessages[i]) {
			return []*EvolutionSignal{d.makeUserFeedbackSignal(userMessages[i], skillName)}
		}
	}
	return nil
}

// makeUserFeedbackSignal 构建用户反馈信号。
//
// 对应 Python: ConversationSignalDetector._make_user_feedback_signal(excerpt, skill_name)
func (d *ConversationSignalDetector) makeUserFeedbackSignal(excerpt, skillName string) *EvolutionSignal {
	return MakeEvolutionSignal(
		schema.UserIntentSignal,
		"Instructions",
		truncateString(excerpt, 600),
		WithSkillName(skillName),
		WithSource("passive_conversation"),
	)
}
```

- [ ] **Step 3: 添加协作信号检测方法**

```go
// detectCollaborationSignals 检测团队协作信号。
// 仅在 TeamSkill 成员执行上下文中触发。
//
// 对应 Python: ConversationSignalDetector._detect_collaboration_signals(trajectory)
func (d *ConversationSignalDetector) detectCollaborationSignals(traj *trajectory.Trajectory) []*EvolutionSignal {
	if !d.isTeamMemberContext(traj) {
		return nil
	}

	var signals []*EvolutionSignal
	meta := traj.Meta
	if meta == nil {
		meta = map[string]any{}
	}
	memberID := fmt.Sprintf("%v", meta["member_id"])
	if memberID == "" || memberID == "<nil>" {
		memberID = "unknown"
	}

	// 从轨迹步骤构建技能读取历史
	var skillReadHistory []skillReadEntry
	for idx, step := range traj.Steps {
		if step.Kind == trajectory.StepKindLLM {
			llmDetail, ok := step.Detail.(*trajectory.LLMCallDetail)
			if !ok {
				continue
			}
			for _, msg := range llmDetail.Messages {
				toolCalls := getField(msg, "tool_calls", []any{})
				if tcSlice, ok := toolCalls.([]any); ok && len(tcSlice) > 0 {
					if detected := d.detectSkillFromToolCalls(tcSlice); detected != "" {
						skillReadHistory = append(skillReadHistory, skillReadEntry{idx, detected})
					}
				}
			}
		}
	}

	for _, step := range traj.Steps {
		if step.Kind != trajectory.StepKindTool {
			continue
		}
		toolDetail, ok := step.Detail.(*trajectory.ToolCallDetail)
		if !ok {
			continue
		}

		toolName := strings.ToLower(toolDetail.ToolName)
		stepMeta := step.Meta
		if stepMeta == nil {
			stepMeta = map[string]any{}
		}

		activeSkill := d.resolveActiveSkillForStep(step, traj.Steps, skillReadHistory)

		// 1. send_message
		if toolName == "send_message" {
			callArgs := fmt.Sprintf("%v", toolDetail.CallArgs)
			toMember := d.extractToMember(callArgs)
			if toMember != "" && toMember != memberID {
				signals = append(signals, MakeEvolutionSignal(
					"collaboration_send", "Collaboration",
					fmt.Sprintf("发送消息给成员 %s", toMember),
					WithToolName(toolName),
					WithSkillName(activeSkill),
					WithSource("passive_collaboration"),
					WithContext(map[string]any{"from_member": memberID, "to_member": toMember}),
				))
			}
		}

		// 2. claim_task
		if toolName == "claim_task" {
			callArgs := fmt.Sprintf("%v", toolDetail.CallArgs)
			taskID := d.extractTaskID(callArgs)
			if taskID != "" {
				signals = append(signals, MakeEvolutionSignal(
					"collaboration_claim", "Collaboration",
					fmt.Sprintf("认领任务 %s", taskID),
					WithToolName(toolName),
					WithSkillName(activeSkill),
					WithSource("passive_collaboration"),
					WithContext(map[string]any{"member_id": memberID, "task_id": taskID}),
				))
			}
		}

		// 3. view_task
		if toolName == "view_task" {
			signals = append(signals, MakeEvolutionSignal(
				"collaboration_view", "Collaboration",
				"查看团队任务状态",
				WithToolName(toolName),
				WithSkillName(activeSkill),
				WithSource("passive_collaboration"),
				WithContext(map[string]any{"member_id": memberID}),
			))
		}

		// 4. parent_invoke_id（接收上下文）
		if _, exists := stepMeta["parent_invoke_id"]; exists {
			parentID := fmt.Sprintf("%v", stepMeta["parent_invoke_id"])
			var tnOpt string
			if toolName != "" {
				tnOpt = toolName
			}
			opts := []SignalOption{
				WithSkillName(activeSkill),
				WithSource("passive_collaboration"),
				WithContext(map[string]any{"member_id": memberID, "parent_invoke_id": parentID}),
			}
			if tnOpt != "" {
				opts = append(opts, WithToolName(tnOpt))
			}
			signals = append(signals, MakeEvolutionSignal(
				"collaboration_receive", "Collaboration",
				fmt.Sprintf("接收来自 %s 的上下文/结果", parentID),
				opts...,
			))
		}

		// 5. 协作失败
		content := fmt.Sprintf("%v", toolDetail.CallResult)
		if match := collaborationFailurePattern.FindStringIndex(content); match != nil {
			excerpt := extractAroundMatch(content, match[0], match[1], 300, 300)
			signals = append(signals, MakeEvolutionSignal(
				"collaboration_failure", "Collaboration", excerpt,
				WithToolName(toolName),
				WithSkillName(activeSkill),
				WithSource("passive_collaboration"),
				WithContext(map[string]any{"member_id": memberID}),
			))
		}
	}

	return signals
}

// isTeamMemberContext 判断轨迹是否处于团队协作成员上下文。
//
// 对应 Python: ConversationSignalDetector._is_team_member_context(trajectory)
func (d *ConversationSignalDetector) isTeamMemberContext(traj *trajectory.Trajectory) bool {
	meta := traj.Meta
	if meta == nil {
		return false
	}
	if _, exists := meta["member_id"]; exists {
		if source, ok := meta["source"]; !ok || fmt.Sprintf("%v", source) != "standalone" {
			return true
		}
	}
	for key := range trajectory.CrossMemberMetaKeys {
		if _, exists := meta[key]; exists {
			return true
		}
	}
	return false
}

// resolveActiveSkillForStep 为轨迹步骤解析活跃技能。
//
// 对应 Python: ConversationSignalDetector._resolve_active_skill_for_step(step, all_steps, skill_read_history)
func (d *ConversationSignalDetector) resolveActiveSkillForStep(
	step *trajectory.TrajectoryStep,
	allSteps []*trajectory.TrajectoryStep,
	skillReadHistory []skillReadEntry,
) string {
	stepIdx := -1
	for i, s := range allSteps {
		if s == step {
			stepIdx = i
			break
		}
	}
	if stepIdx < 0 {
		return ""
	}
	for i := len(skillReadHistory) - 1; i >= 0; i-- {
		if skillReadHistory[i].msgIdx <= stepIdx {
			return skillReadHistory[i].skillName
		}
	}
	return ""
}

// extractToMember 从 send_message 参数中提取目标成员。
//
// 对应 Python: ConversationSignalDetector._extract_to_member(call_args)
func (d *ConversationSignalDetector) extractToMember(callArgs string) string {
	var argsDict map[string]any
	if err := json.Unmarshal([]byte(callArgs), &argsDict); err == nil {
		if v := argsDict["to_member_name"]; v != nil && fmt.Sprintf("%v", v) != "" {
			return fmt.Sprintf("%v", v)
		}
		if v := argsDict["to"]; v != nil && fmt.Sprintf("%v", v) != "" {
			return fmt.Sprintf("%v", v)
		}
	}
	// 正则回退
	patterns := []string{
		`to_member_name["']?\s*[:=]\s*["']([^"']+)["']`,
		`to["']?\s*[:=]\s*["']([^"']+)["']`,
	}
	for _, p := range patterns {
		re := regexp.MustCompile(p)
		if m := re.FindStringSubmatch(callArgs); len(m) >= 2 {
			return m[1]
		}
	}
	return ""
}

// extractTaskID 从 claim_task 参数中提取任务 ID。
//
// 对应 Python: ConversationSignalDetector._extract_task_id(call_args)
func (d *ConversationSignalDetector) extractTaskID(callArgs string) string {
	var argsDict map[string]any
	if err := json.Unmarshal([]byte(callArgs), &argsDict); err == nil {
		if v := argsDict["task_id"]; v != nil && fmt.Sprintf("%v", v) != "" {
			return fmt.Sprintf("%v", v)
		}
	}
	re := regexp.MustCompile(`task_id["']?\s*[:=]\s*["']([^"']+)["']`)
	if m := re.FindStringSubmatch(callArgs); len(m) >= 2 {
		return m[1]
	}
	return ""
}

// deduplicate 基于 fingerprint 去重信号列表。
//
// 对应 Python: ConversationSignalDetector._deduplicate(signals)
func (d *ConversationSignalDetector) deduplicate(signals []*EvolutionSignal) []*EvolutionSignal {
	seen := map[[4]string]bool{}
	var deduped []*EvolutionSignal
	for _, sig := range signals {
		key := MakeSignalFingerprint(sig)
		if seen[key] {
			continue
		}
		seen[key] = true
		deduped = append(deduped, sig)
	}
	return deduped
}
```

- [ ] **Step 4: 添加包级辅助函数 truncateString 和 stringPtrValue**

```go
// truncateString 截断字符串到指定最大长度。
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// stringPtrValue 安全获取 *string 的值，nil 返回空字符串。
func stringPtrValue(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
```

- [ ] **Step 5: 添加 from_conv_test.go 核心检测测试**

```go
func TestConversationSignalDetector_Detect_从消息列表(t *testing.T) {
	d := NewConversationSignalDetector()
	messages := []map[string]any{
		{"role": "assistant", "content": "let me run", "tool_calls": []any{
			map[string]any{"id": "tc1", "name": "bash", "arguments": `{"command": "ls"}`},
		}},
		{"role": "tool", "tool_call_id": "tc1", "name": "bash", "content": "Error: file not found"},
	}
	signals := d.Detect(messages)
	if len(signals) == 0 {
		t.Fatal("Detect should return at least one signal for execution_failure")
	}
	if signals[0].SignalType != "execution_failure" {
		t.Errorf("SignalType = %q, want %q", signals[0].SignalType, "execution_failure")
	}
}

func TestConversationSignalDetector_Detect_从Trajectory(t *testing.T) {
	d := NewConversationSignalDetector()
	traj := &trajectory.Trajectory{
		Steps: []*trajectory.TrajectoryStep{
			{
				Kind: trajectory.StepKindTool,
				Detail: &trajectory.ToolCallDetail{
					ToolName:   "bash",
					CallResult: map[string]any{"output": "error: something failed"},
				},
			},
		},
		Meta: map[string]any{},
	}
	signals := d.Detect(traj)
	if len(signals) == 0 {
		t.Fatal("Detect from Trajectory should return at least one signal")
	}
}

func TestConversationSignalDetector_detectFromMessages_脚本产物(t *testing.T) {
	d := NewConversationSignalDetector()
	messages := []map[string]any{
		{"role": "assistant", "content": "", "tool_calls": []any{
			map[string]any{"id": "tc1", "name": "bash", "arguments": `{"command": "echo hello world this is a long command that exceeds twenty chars"}`},
		}},
		{"role": "tool", "tool_call_id": "tc1", "name": "bash", "content": "hello world this is a long command that exceeds twenty chars"},
	}
	signals := d.detectFromMessages(messages)
	found := false
	for _, sig := range signals {
		if sig.SignalType == "script_artifact" {
			found = true
			break
		}
	}
	if !found {
		t.Error("detectFromMessages should detect script_artifact")
	}
}

func TestConversationSignalDetector_detectFromMessages_数据获取工具跳过(t *testing.T) {
	d := NewConversationSignalDetector()
	messages := []map[string]any{
		{"role": "tool", "name": "search", "content": "Error: timeout"},
	}
	signals := d.detectFromMessages(messages)
	if len(signals) != 0 {
		t.Errorf("data fetch tools should be skipped, got %d signals", len(signals))
	}
}

func TestConversationSignalDetector_detectFromMessages_工具Schema跳过(t *testing.T) {
	d := NewConversationSignalDetector()
	messages := []map[string]any{
		{"role": "tool", "name": "some_tool", "content": "{'content': '---\\nname: my_tool\\ndescription: a tool"},
	}
	signals := d.detectFromMessages(messages)
	if len(signals) != 0 {
		t.Errorf("tool schema output should be skipped, got %d signals", len(signals))
	}
}

func TestConversationSignalDetector_fallbackUserFeedbackSignals(t *testing.T) {
	d := NewConversationSignalDetector()
	signals := d.fallbackUserFeedbackSignals([]string{"hello", "不对，应该这样做"}, "my_skill")
	if len(signals) != 1 {
		t.Fatalf("fallbackUserFeedbackSignals = %d signals, want 1", len(signals))
	}
	if signals[0].SignalType != schema.UserIntentSignal {
		t.Errorf("SignalType = %q, want %q", signals[0].SignalType, schema.UserIntentSignal)
	}
}

func TestConversationSignalDetector_deduplicate(t *testing.T) {
	d := NewConversationSignalDetector()
	sig1 := MakeEvolutionSignal("execution_failure", "Troubleshooting", "error",
		WithToolName("bash"), WithSkillName("skill1"), WithSource("passive_conversation"))
	sig2 := MakeEvolutionSignal("execution_failure", "Troubleshooting", "error",
		WithToolName("bash"), WithSkillName("skill1"), WithSource("passive_conversation"))
	sig3 := MakeEvolutionSignal("low_score", "Troubleshooting", "score=0.0",
		WithSkillName("skill1"))
	signals := []*EvolutionSignal{sig1, sig2, sig3}
	deduped := d.deduplicate(signals)
	if len(deduped) != 2 {
		t.Errorf("deduplicate = %d signals, want 2", len(deduped))
	}
}

func TestConversationSignalDetector_isTeamMemberContext(t *testing.T) {
	d := NewConversationSignalDetector()
	trajWithMember := &trajectory.Trajectory{Meta: map[string]any{"member_id": "m1"}}
	if !d.isTeamMemberContext(trajWithMember) {
		t.Error("should detect team member context with member_id")
	}
	trajStandalone := &trajectory.Trajectory{Meta: map[string]any{"member_id": "m1", "source": "standalone"}}
	if d.isTeamMemberContext(trajStandalone) {
		t.Error("should not detect team member context in standalone mode")
	}
	trajWithParent := &trajectory.Trajectory{Meta: map[string]any{"parent_invoke_id": "p1"}}
	if !d.isTeamMemberContext(trajWithParent) {
		t.Error("should detect team member context with parent_invoke_id")
	}
	trajNoMeta := &trajectory.Trajectory{Meta: nil}
	if d.isTeamMemberContext(trajNoMeta) {
		t.Error("should not detect team member context with nil meta")
	}
}

func TestExtractToMember_JSON解析(t *testing.T) {
	d := NewConversationSignalDetector()
	result := d.extractToMember(`{"to_member_name": "agent_1"}`)
	if result != "agent_1" {
		t.Errorf("extractToMember = %q, want %q", result, "agent_1")
	}
}

func TestExtractTaskID_JSON解析(t *testing.T) {
	d := NewConversationSignalDetector()
	result := d.extractTaskID(`{"task_id": "task_123"}`)
	if result != "task_123" {
		t.Errorf("extractTaskID = %q, want %q", result, "task_123")
	}
}

func TestConversationSignalDetector_ConvertTrajectoryToMessages(t *testing.T) {
	d := NewConversationSignalDetector()
	traj := &trajectory.Trajectory{
		Steps: []*trajectory.TrajectoryStep{
			{
				Kind: trajectory.StepKindLLM,
				Detail: &trajectory.LLMCallDetail{
					Messages: []map[string]any{
						{"role": "user", "content": "hello"},
						{"role": "assistant", "content": "hi", "tool_calls": []any{
							map[string]any{"id": "tc1", "name": "bash", "arguments": `{"command":"ls"}`},
						}},
					},
				},
			},
			{
				Kind: trajectory.StepKindTool,
				Detail: &trajectory.ToolCallDetail{
					ToolName:   "bash",
					ToolCallID: "tc1",
					CallResult: map[string]any{"output": "file1.txt"},
				},
			},
		},
	}
	msgs := d.ConvertTrajectoryToMessages(traj)
	if len(msgs) < 3 {
		t.Fatalf("ConvertTrajectoryToMessages = %d messages, want >= 3", len(msgs))
	}
	// 验证最后一条是 tool 消息
	last := msgs[len(msgs)-1]
	if last["role"] != "tool" {
		t.Errorf("last message role = %v, want %q", last["role"], "tool")
	}
}
```

注意：需要在 from_conv_test.go import 中添加 `"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"`。

- [ ] **Step 6: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/signal/ -v`
Expected: ALL PASS

- [ ] **Step 7: Commit**

```bash
git add internal/evolving/signal/from_conv.go internal/evolving/signal/from_conv_test.go
git commit -m "feat(evolving): add LLM user feedback detection, collaboration signals, and dedup for ConversationSignalDetector"
```

---

### Task 6: team.go — 常量 + 类型 + JSON 解析器 + 轨迹摘要

**Files:**
- Create: `internal/evolving/signal/team.go`
- Create: `internal/evolving/signal/team_test.go`

- [ ] **Step 1: 创建 team.go 基础框架**

```go
package signal

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// UserIntent 解析后的用户改进意图。
//
// 对应 Python: openjiuwen/agent_evolving/signal/team.py UserIntent(frozen dataclass)
type UserIntent struct {
	// IsImprovement 是否包含改进意图
	IsImprovement bool
	// Intent 改进意图摘要
	Intent string
}

// TrajectoryIssue 规范化的轨迹问题。
//
// 对应 Python: openjiuwen/agent_evolving/signal/team.py TrajectoryIssue(frozen dataclass)
type TrajectoryIssue struct {
	// IssueType 问题类型
	IssueType string
	// Description 问题描述
	Description string
	// AffectedRole 受影响角色
	AffectedRole string
	// Severity 严重程度（low/medium/high）
	Severity string
}

// ──────────────────────────── 枚举 ────────────────────────────

// TeamSignalType 团队域信号类型枚举。
// UserRequest 保留为向后兼容别名。
//
// 对应 Python: TeamSignalType(str, Enum)
type TeamSignalType string

const (
	// TeamSignalTypeUserIntent 用户意图信号
	TeamSignalTypeUserIntent TeamSignalType = "user_intent"
	// TeamSignalTypeUserRequest 用户请求信号（向后兼容）
	TeamSignalTypeUserRequest TeamSignalType = "user_request"
	// TeamSignalTypeTrajectoryIssue 轨迹问题信号
	TeamSignalTypeTrajectoryIssue TeamSignalType = "trajectory_issue"
)

// ──────────────────────────── 常量 ────────────────────────────

// teamUserRequestPromptCN 中文团队用户请求检测提示词。
//
// 对应 Python: _TEAM_USER_REQUEST_PROMPT_CN（原文复刻，不翻译）
const teamUserRequestPromptCN = (
	"判断以下用户输入是否包含对当前团队任务或团队协作方式的改进意见。\n" +
		"如果是，提取改进意图的摘要。\n\n" +
		"团队技能描述：{team_skill_description}\n" +
		"当前角色：{roles}\n" +
		"用户输入：{user_messages}\n\n" +
		`输出 JSON: {{"is_improvement": true/false, "intent": "str"}}` + "\n")

// teamUserRequestPromptEN 英文团队用户请求检测提示词。
//
// 对应 Python: _TEAM_USER_REQUEST_PROMPT_EN（原文复刻，不翻译）
const teamUserRequestPromptEN = (
	"Determine if the following user input contains improvement suggestions " +
		"for the current team task or collaboration approach.\n" +
		"If yes, extract a summary of the improvement intent.\n\n" +
		"Team skill description: {team_skill_description}\n" +
		"Current roles: {roles}\n" +
		"User input: {user_messages}\n\n" +
		`Output JSON: {{"is_improvement": true/false, "intent": "str"}}` + "\n")

// teamTrajectoryIssuePromptCN 中文团队轨迹问题检测提示词。
//
// 对应 Python: _TEAM_TRAJECTORY_ISSUE_PROMPT_CN（原文复刻，不翻译）
const teamTrajectoryIssuePromptCN = (
	"分析以下执行轨迹，判断团队技能是否存在不足需要演进。\n\n" +
		"当前团队技能：\n{skill_content}\n\n" +
		"执行轨迹摘要：\n{trajectory_summary}\n\n" +
		"请从以下维度分析：\n" +
		"- 角色配合是否恰当（是否有角色间协作断裂、数据未传递）\n" +
		"- 约束是否被违反（超时、产出格式不合规）\n" +
		"- 流程是否低效（重复调用、多余步骤）\n" +
		"- 角色能力是否不足（某角色多次失败或产出质量不达标）\n\n" +
		"如果存在不足，输出 JSON 数组：\n" +
		`[{{"issue_type": str, "description": str, "affected_role": str, "severity": "low"|"medium"|"high"}}]` + "\n" +
		"如果没有问题，输出空数组 []。")

// teamTrajectoryIssuePromptEN 英文团队轨迹问题检测提示词。
//
// 对应 Python: _TEAM_TRAJECTORY_ISSUE_PROMPT_EN（原文复刻，不翻译）
const teamTrajectoryIssuePromptEN = (
	"Analyze the following execution trajectory and determine whether the team skill has deficiencies.\n\n" +
		"Current team skill:\n{skill_content}\n\n" +
		"Trajectory summary:\n{trajectory_summary}\n\n" +
		"Analyze from these dimensions:\n" +
		"- Role coordination (collaboration breaks, data not passed)\n" +
		"- Constraint violations (timeout, output format issues)\n" +
		"- Workflow inefficiency (redundant calls, extra steps)\n" +
		"- Role capability gaps (repeated failures, poor output quality)\n\n" +
		"If issues exist, output a JSON array:\n" +
		`[{{"issue_type": str, "description": str, "affected_role": str, "severity": "low"|"medium"|"high"}}]` + "\n" +
		"If no issues, output empty array [].\n")

// teamTrajectoryIssuesKey 轨迹问题在信号 context 中的键名。
const teamTrajectoryIssuesKey = "trajectory_issues"

// teamSkillContentKey 技能内容在信号 context 中的键名。
const teamSkillContentKey = "skill_content"

// jsonBlockRE 匹配 JSON 代码块的正则。
var jsonBlockRE = regexp.MustCompile("```(?:json)?\\s*\\n(.*?)```")

// keyTools 协作关键工具集合。
var keyTools = map[string]bool{
	"spawn_member": true,
	"create_task":  true,
	"build_team":   true,
	"view_task":    true,
	"send_message": true,
}

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseTeamModelJSON 健壮的 JSON 解析器，从团队技能 LLM 输出中解析 dict/list JSON。
// 支持代码块提取、平衡括号提取、格式修复（去除注释/尾逗号）。
//
// 对应 Python: parse_team_model_json(raw)
func ParseTeamModelJSON(raw string) any {
	if raw == "" {
		return nil
	}

	var candidates []string

	// 尝试从代码块中提取
	match := jsonBlockRE.FindStringSubmatch(raw)
	if len(match) >= 2 {
		candidates = append(candidates, strings.TrimSpace(match[1]))
	}
	candidates = append(candidates, strings.TrimSpace(raw))
	candidates = append(candidates, fixJSONText(raw))

	balancedObject := extractBalancedJSON(raw, '{', '}')
	if balancedObject != "" {
		candidates = append(candidates, balancedObject)
		candidates = append(candidates, fixJSONText(balancedObject))
	}

	balancedArray := extractBalancedJSON(raw, '[', ']')
	if balancedArray != "" {
		candidates = append(candidates, balancedArray)
		candidates = append(candidates, fixJSONText(balancedArray))
	}

	seen := map[string]bool{}
	for _, candidate := range candidates {
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		var data any
		if err := json.Unmarshal([]byte(candidate), &data); err == nil {
			switch data.(type) {
			case map[string]any, []any:
				return data
			}
		}
	}

	head := raw
	if len(head) > 600 {
		head = head[:600]
	}
	head = strings.ReplaceAll(head, "\n", "\\n")
	logger.Warn(logComponent).
		Str("method", "ParseTeamModelJSON").
		Int("raw_len", len(raw)).
		Str("head", head).
		Msg("[TeamSignal] JSON parse failed")

	return nil
}

// BuildTeamTrajectorySummary 将轨迹步骤摘要为文本，对关键协作工具保留更多细节。
//
// 对应 Python: build_team_trajectory_summary(trajectory)
func BuildTeamTrajectorySummary(traj *trajectory.Trajectory) string {
	toolBudget := 20000
	llmBudget := 10000
	var toolLines []string
	var llmLines []string
	llmCount := 0
	toolCount := 0

	for _, step := range traj.Steps {
		if step.Kind == trajectory.StepKindTool && step.Detail != nil {
			toolDetail, ok := step.Detail.(*trajectory.ToolCallDetail)
			if !ok {
				continue
			}
			toolCount++
			toolName := toolDetail.ToolName
			isKey := keyTools[toolName]
			argsLimit := 150
			if isKey {
				argsLimit = 500
			}
			resultLimit := 200
			if isKey {
				resultLimit = 500
			}
			args := truncateString(fmt.Sprintf("%v", toolDetail.CallArgs), argsLimit)
			result := truncateString(fmt.Sprintf("%v", toolDetail.CallResult), resultLimit)
			toolLines = append(toolLines, fmt.Sprintf("[Tool:%s] args=%s result=%s", toolName, args, result))
		} else if step.Kind == trajectory.StepKindLLM && step.Detail != nil {
			llmDetail, ok := step.Detail.(*trajectory.LLMCallDetail)
			if !ok {
				continue
			}
			llmCount++
			if llmDetail.Response != nil {
				llmLines = append(llmLines, fmt.Sprintf("[LLM] %s", truncateString(fmt.Sprintf("%v", llmDetail.Response), 300)))
			}
		}
	}

	toolSection := strings.Join(toolLines, "\n")
	if len(toolSection) > toolBudget {
		toolSection = toolSection[:toolBudget] + "\n... (tool section truncated)"
	}

	llmSection := strings.Join(llmLines, "\n")
	if len(llmSection) > llmBudget {
		llmSection = llmSection[:llmBudget] + "\n... (LLM section truncated)"
	}

	summary := fmt.Sprintf("### Tool Calls (%d)\n%s\n\n### LLM Responses (%d)\n%s", toolCount, toolSection, llmCount, llmSection)
	logger.Info(logComponent).
		Str("method", "BuildTeamTrajectorySummary").
		Int("llm_count", llmCount).
		Int("tool_count", toolCount).
		Int("tool_section_len", len(toolSection)).
		Int("llm_section_len", len(llmSection)).
		Int("total_len", len(summary)).
		Msg("[TeamSignal] trajectory summary")

	return summary
}

// MakeTeamUserIntentSignal 构建团队用户意图信号。
//
// 对应 Python: make_team_user_intent_signal(skill_name, user_intent)
func MakeTeamUserIntentSignal(skillName, userIntent string) *EvolutionSignal {
	return MakeEvolutionSignal(
		string(TeamSignalTypeUserIntent),
		"Instructions",
		userIntent,
		WithSkillName(skillName),
		WithSource("explicit_request"),
	)
}

// MakeTeamTrajectorySignal 构建团队轨迹问题信号。
//
// 对应 Python: make_team_trajectory_signal(skill_name, skill_content, trajectory_issues)
func MakeTeamTrajectorySignal(skillName, skillContent string, trajectoryIssues []map[string]string) *EvolutionSignal {
	issues := make([]any, len(trajectoryIssues))
	for i, item := range trajectoryIssues {
		issues[i] = item
	}
	return MakeEvolutionSignal(
		string(TeamSignalTypeTrajectoryIssue),
		"",
		"Detected team skill trajectory issues requiring evolution.",
		WithSkillName(skillName),
		WithSource("passive_trajectory"),
		WithContext(map[string]any{
			teamTrajectoryIssuesKey: issues,
			teamSkillContentKey:    skillContent,
		}),
	)
}

// GetTeamTrajectoryIssues 从信号中读取轨迹问题列表。
//
// 对应 Python: get_team_trajectory_issues(signal)
func GetTeamTrajectoryIssues(sig *EvolutionSignal) []map[string]string {
	context := sig.Context
	if context == nil {
		return nil
	}
	issues, ok := context[teamTrajectoryIssuesKey]
	if !ok {
		return nil
	}
	slice, ok := issues.([]any)
	if !ok {
		return nil
	}
	var result []map[string]string
	for _, item := range slice {
		if m, ok := item.(map[string]any); ok {
			entry := make(map[string]string)
			for k, v := range m {
				entry[k] = fmt.Sprintf("%v", v)
			}
			result = append(result, entry)
		}
		if m, ok := item.(map[string]string); ok {
			result = append(result, m)
		}
	}
	return result
}

// GetTeamSignalSkillContent 从信号中读取关联的团队技能内容。
//
// 对应 Python: get_team_signal_skill_content(signal)
func GetTeamSignalSkillContent(sig *EvolutionSignal) string {
	context := sig.Context
	if context == nil {
		return ""
	}
	v, ok := context[teamSkillContentKey]
	if !ok || v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// tryParseJSON 尝试解析 JSON 字符串。
//
// 对应 Python: _try_parse_json(text)
func tryParseJSON(text string) any {
	var data any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		return nil
	}
	return data
}

// fixJSONText 对常见 LLM JSON 格式问题做轻量修复。
//
// 对应 Python: _fix_json_text(text)
func fixJSONText(text string) string {
	text = regexp.MustCompile("(?m)^```(?:json)?\\s*").ReplaceAllString(strings.TrimSpace(text), "")
	text = regexp.MustCompile("(?m)^```\\s*$").ReplaceAllString(text, "")
	text = regexp.MustCompile("//[^\\n]*").ReplaceAllString(text, "")
	text = regexp.MustCompile(",\\s*([}\\]])").ReplaceAllString(text, "$1")
	return strings.TrimSpace(text)
}

// extractBalancedJSON 提取第一个平衡的 JSON 子串。
//
// 对应 Python: _extract_balanced_json(text, opener, closer)
func extractBalancedJSON(text string, opener, closer rune) string {
	openerStr := string(opener)
	start := strings.IndexRune(text, opener)
	if start == -1 {
		return ""
	}
	depth := 0
	inString := false
	escape := false
	for i := start; i < len(text); i++ {
		ch := rune(text[i])
		if inString {
			if escape {
				escape = false
			} else if ch == '\\' {
				escape = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			continue
		}
		if ch == opener {
			depth++
		} else if ch == closer {
			depth--
			if depth == 0 {
				return text[start : i+1]
			}
		}
	}
	return ""
}

// extractRolesSummary 从团队技能内容中提取紧凑的角色摘要。
//
// 对应 Python: _extract_roles_summary(team_skill_content)
func extractRolesSummary(teamSkillContent string) string {
	if teamSkillContent == "" {
		return ""
	}

	lines := strings.Split(teamSkillContent, "\n")
	var roleLines []string
	inRoles := false
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		lowered := strings.ToLower(stripped)
		if strings.HasPrefix(lowered, "roles:") {
			value := strings.TrimSpace(stripped[strings.Index(stripped, ":")+1:])
			if value != "" {
				roleLines = append(roleLines, value)
			}
			inRoles = true
			continue
		}
		if inRoles {
			if stripped == "" {
				continue
			}
			if strings.HasPrefix(stripped, "-") || (len(line) > 0 && (line[0] == ' ' || line[0] == '\t')) {
				roleLines = append(roleLines, stripped)
				continue
			}
			break
		}
	}

	if len(roleLines) == 0 {
		for _, line := range lines {
			stripped := strings.TrimSpace(line)
			lowered := strings.ToLower(stripped)
			if strings.HasPrefix(lowered, "role:") || strings.HasPrefix(lowered, "角色：") || strings.HasPrefix(lowered, "角色:") {
				roleLines = append(roleLines, stripped)
			}
			if len(roleLines) >= 5 {
				break
			}
		}
	}

	result := strings.Join(roleLines, "\n")
	if len(result) > 500 {
		result = result[:500]
	}
	return result
}

// normalizeIssue 规范化轨迹问题项。
//
// 对应 Python: TeamSignalDetector._normalize_issue(item)
func normalizeIssue(item any) map[string]string {
	m, ok := item.(map[string]any)
	if !ok {
		return nil
	}
	severity := "medium"
	if v, exists := m["severity"]; exists && v != nil {
		s := fmt.Sprintf("%v", v)
		if s == "low" || s == "medium" || s == "high" {
			severity = s
		}
	}
	return map[string]string{
		"issue_type":    stringOrDefault(m, "issue_type", "unknown"),
		"description":   stringOrDefault(m, "description", ""),
		"affected_role": stringOrDefault(m, "affected_role", ""),
		"severity":      severity,
	}
}

// stringOrDefault 从 map 中安全获取字符串值。
func stringOrDefault(m map[string]any, key, defaultVal string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return defaultVal
	}
	s := fmt.Sprintf("%v", v)
	if s == "" {
		return defaultVal
	}
	return s
}
```

- [ ] **Step 2: 创建 team_test.go — JSON 解析器和辅助函数测试**

```go
package signal

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

func TestParseTeamModelJSON_有效dict(t *testing.T) {
	raw := `{"is_improvement": true, "intent": "test"}`
	result := ParseTeamModelJSON(raw)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("ParseTeamModelJSON = %T, want map[string]any", result)
	}
	if m["is_improvement"] != true {
		t.Errorf("is_improvement = %v, want true", m["is_improvement"])
	}
}

func TestParseTeamModelJSON_有效list(t *testing.T) {
	raw := `[{"issue_type": "coordination", "severity": "high"}]`
	result := ParseTeamModelJSON(raw)
	_, ok := result.([]any)
	if !ok {
		t.Fatalf("ParseTeamModelJSON = %T, want []any", result)
	}
}

func TestParseTeamModelJSON_代码块提取(t *testing.T) {
	raw := "```json\n{\"is_improvement\": true}\n```"
	result := ParseTeamModelJSON(raw)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("ParseTeamModelJSON from code block = %T, want map[string]any", result)
	}
	if m["is_improvement"] != true {
		t.Errorf("is_improvement = %v, want true", m["is_improvement"])
	}
}

func TestParseTeamModelJSON_空输入(t *testing.T) {
	if ParseTeamModelJSON("") != nil {
		t.Error("ParseTeamModelJSON empty should return nil")
	}
}

func TestParseTeamModelJSON_尾逗号修复(t *testing.T) {
	raw := `{"key": "value",}`
	result := ParseTeamModelJSON(raw)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("ParseTeamModelJSON with trailing comma = %T, want map[string]any", result)
	}
	if m["key"] != "value" {
		t.Errorf("key = %v, want value", m["key"])
	}
}

func TestFixJSONText_去除注释(t *testing.T) {
	input := `{"key": "value" // comment}`
	result := fixJSONText(input)
	if result != `{"key": "value" }` {
		t.Errorf("fixJSONText = %q, want %q", result, `{"key": "value" }`)
	}
}

func TestExtractBalancedJSON_基本(t *testing.T) {
	text := `some text {"key": "value"} more text`
	result := extractBalancedJSON(text, '{', '}')
	if result != `{"key": "value"}` {
		t.Errorf("extractBalancedJSON = %q, want %q", result, `{"key": "value"}`)
	}
}

func TestBuildTeamTrajectorySummary_基本(t *testing.T) {
	traj := &trajectory.Trajectory{
		Steps: []*trajectory.TrajectoryStep{
			{
				Kind: trajectory.StepKindTool,
				Detail: &trajectory.ToolCallDetail{
					ToolName:   "bash",
					CallArgs:   map[string]any{"command": "ls"},
					CallResult: map[string]any{"output": "file1.txt"},
				},
			},
			{
				Kind: trajectory.StepKindLLM,
				Detail: &trajectory.LLMCallDetail{
					Response: map[string]any{"content": "done"},
				},
			},
		},
	}
	summary := BuildTeamTrajectorySummary(traj)
	if summary == "" {
		t.Error("BuildTeamTrajectorySummary should return non-empty summary")
	}
	if !contains(summary, "Tool Calls") {
		t.Error("summary should contain 'Tool Calls'")
	}
	if !contains(summary, "LLM Responses") {
		t.Error("summary should contain 'LLM Responses'")
	}
}

func TestMakeTeamUserIntentSignal(t *testing.T) {
	sig := MakeTeamUserIntentSignal("my_skill", "improve collaboration")
	if sig.SignalType != "user_intent" {
		t.Errorf("SignalType = %q, want %q", sig.SignalType, "user_intent")
	}
	if sig.Section != "Instructions" {
		t.Errorf("Section = %q, want %q", sig.Section, "Instructions")
	}
	if sig.Excerpt != "improve collaboration" {
		t.Errorf("Excerpt = %q, want %q", sig.Excerpt, "improve collaboration")
	}
}

func TestMakeTeamTrajectorySignal(t *testing.T) {
	issues := []map[string]string{
		{"issue_type": "coordination", "severity": "high"},
	}
	sig := MakeTeamTrajectorySignal("team_skill", "skill content", issues)
	if sig.SignalType != "trajectory_issue" {
		t.Errorf("SignalType = %q, want %q", sig.SignalType, "trajectory_issue")
	}
	if sig.Context == nil {
		t.Fatal("Context should not be nil")
	}
	if _, ok := sig.Context[teamTrajectoryIssuesKey]; !ok {
		t.Error("Context should contain trajectory_issues")
	}
	if sig.Context[teamSkillContentKey] != "skill content" {
		t.Errorf("Context[skill_content] = %v, want %q", sig.Context[teamSkillContentKey], "skill content")
	}
}

func TestGetTeamTrajectoryIssues(t *testing.T) {
	issues := []map[string]string{
		{"issue_type": "coordination", "severity": "high"},
	}
	sig := MakeTeamTrajectorySignal("skill", "content", issues)
	result := GetTeamTrajectoryIssues(sig)
	if len(result) != 1 {
		t.Fatalf("GetTeamTrajectoryIssues = %d items, want 1", len(result))
	}
}

func TestGetTeamSignalSkillContent(t *testing.T) {
	sig := MakeTeamTrajectorySignal("skill", "my content", nil)
	content := GetTeamSignalSkillContent(sig)
	if content != "my content" {
		t.Errorf("GetTeamSignalSkillContent = %q, want %q", content, "my content")
	}
}

func TestExtractRolesSummary_Roles段(t *testing.T) {
	content := "Roles:\n- leader\n- worker\n\nOther: stuff"
	result := extractRolesSummary(content)
	if !contains(result, "leader") {
		t.Errorf("extractRolesSummary = %q, should contain 'leader'", result)
	}
}

func TestNormalizeIssue_基本(t *testing.T) {
	item := map[string]any{
		"issue_type":    "coordination",
		"description":   "data not passed",
		"affected_role": "worker",
		"severity":      "high",
	}
	result := normalizeIssue(item)
	if result["issue_type"] != "coordination" {
		t.Errorf("issue_type = %q, want %q", result["issue_type"], "coordination")
	}
	if result["severity"] != "high" {
		t.Errorf("severity = %q, want %q", result["severity"], "high")
	}
}

func TestNormalizeIssue_无效severity回退(t *testing.T) {
	item := map[string]any{
		"issue_type": "test",
		"severity":   "critical",
	}
	result := normalizeIssue(item)
	if result["severity"] != "medium" {
		t.Errorf("severity = %q, want %q (default)", result["severity"], "medium")
	}
}

func TestNormalizeIssue_非dict返回nil(t *testing.T) {
	result := normalizeIssue("not a dict")
	if result != nil {
		t.Errorf("normalizeIssue non-dict = %v, want nil", result)
	}
}

// contains 辅助函数，检查字符串是否包含子串。
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
```

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/signal/ -run "TestParseTeamModelJSON|TestFixJSONText|TestExtractBalancedJSON|TestBuildTeamTrajectorySummary|TestMakeTeam|TestGetTeam|TestExtractRolesSummary|TestNormalizeIssue" -v`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```bash
git add internal/evolving/signal/team.go internal/evolving/signal/team_test.go
git commit -m "feat(evolving): add TeamSignalDetector types, JSON parser, trajectory summary and signal builders"
```

---

### Task 7: team.go — TeamSignalDetector 结构体和检测方法

**Files:**
- Modify: `internal/evolving/signal/team.go`
- Modify: `internal/evolving/signal/team_test.go`

- [ ] **Step 1: 在 team.go 结构体区块添加 TeamSignalDetector**

```go
// TeamSignalDetector 团队域信号检测器，从用户输入和轨迹中检测演化信号。
//
// 对应 Python: openjiuwen/agent_evolving/signal/team.py TeamSignalDetector
type TeamSignalDetector struct {
	// llm LLM 模型实例
	llm *llm.Model
	// model 模型名称
	model string
	// language 语言（"cn" 或 "en"）
	language string
	// trajectoryIssueLLMPolicy 轨迹问题检测的 LLM 策略
	trajectoryIssueLLMPolicy llm_resilience.LLMInvokePolicy
	// userIntentLLMPolicy 用户意图检测的 LLM 策略
	userIntentLLMPolicy llm_resilience.LLMInvokePolicy
}
```

注意：需要在 import 中添加：
```go
"context"

"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
```

- [ ] **Step 2: 在导出函数区块添加构造函数**

```go
// NewTeamSignalDetector 创建 TeamSignalDetector 实例。
// 必须至少传入一个 LLMInvokePolicy，否则 panic。
//
// 对应 Python: TeamSignalDetector(llm, model, language, llm_policy, ...)
func NewTeamSignalDetector(
	llmModel *llm.Model,
	model string,
	language string,
	trajectoryIssueLLMPolicy *llm_resilience.LLMInvokePolicy,
	userIntentLLMPolicy *llm_resilience.LLMInvokePolicy,
) *TeamSignalDetector {
	var policy llm_resilience.LLMInvokePolicy
	if trajectoryIssueLLMPolicy != nil {
		policy = *trajectoryIssueLLMPolicy
	} else if userIntentLLMPolicy != nil {
		policy = *userIntentLLMPolicy
	}
	if policy.MaxAttempts == 0 && policy.TotalBudgetSecs == 0 {
		panic("TeamSignalDetector requires at least one LLM policy")
	}

	trajectoryPolicy := policy
	if trajectoryIssueLLMPolicy != nil {
		trajectoryPolicy = *trajectoryIssueLLMPolicy
	}
	userIntentPolicy := policy
	if userIntentLLMPolicy != nil {
		userIntentPolicy = *userIntentLLMPolicy
	}

	return &TeamSignalDetector{
		llm:                      llmModel,
		model:                    model,
		language:                 language,
		trajectoryIssueLLMPolicy: trajectoryPolicy,
		userIntentLLMPolicy:      userIntentPolicy,
	}
}
```

- [ ] **Step 3: 添加 DetectUserIntent 方法**

```go
// DetectUserIntent 检测用户消息是否包含团队技能改进意图。
//
// 对应 Python: TeamSignalDetector.detect_user_intent(messages, team_skill_content)
func (d *TeamSignalDetector) DetectUserIntent(
	ctx context.Context,
	messages []map[string]any,
	teamSkillContent string,
) (*UserIntent, error) {
	var userMsgs []string
	for _, m := range messages {
		role := fmt.Sprintf("%v", m["role"])
		if role == "user" {
			userMsgs = append(userMsgs, fmt.Sprintf("%v", m["content"]))
		}
	}
	if len(userMsgs) > 10 {
		userMsgs = userMsgs[len(userMsgs)-10:]
	}
	if len(userMsgs) == 0 {
		return nil, nil
	}

	userText := strings.Join(userMsgs, "\n")
	promptTemplate := teamUserRequestPromptCN
	if d.language != "cn" {
		promptTemplate = teamUserRequestPromptEN
	}
	skillDesc := ""
	if teamSkillContent != "" {
		if len(teamSkillContent) > 1000 {
			skillDesc = teamSkillContent[:1000]
		} else {
			skillDesc = teamSkillContent
		}
	}
	prompt := strings.ReplaceAll(promptTemplate, "{team_skill_description}", skillDesc)
	prompt = strings.ReplaceAll(prompt, "{roles}", extractRolesSummary(teamSkillContent))
	userTextTruncated := userText
	if len(userTextTruncated) > 2000 {
		userTextTruncated = userTextTruncated[:2000]
	}
	prompt = strings.ReplaceAll(prompt, "{user_messages}", userTextTruncated)

	raw, err := llm_resilience.InvokeTextWithRetry(
		ctx, d.llm, d.model, prompt, d.userIntentLLMPolicy,
		llm_resilience.WithIsResultUsable(func(text string) bool {
			return ParseTeamModelJSON(text) != nil
		}),
	)
	if err != nil {
		logger.Warn(logComponent).
			Str("method", "DetectUserIntent").
			Str("error", err.Error()).
			Msg("[TeamSignalDetector] detect_user_intent failed")
		return nil, err
	}

	parsed := ParseTeamModelJSON(raw)
	m, ok := parsed.(map[string]any)
	if !ok {
		return nil, nil
	}
	isImprovement := false
	switch v := m["is_improvement"].(type) {
	case bool:
		isImprovement = v
	default:
		isImprovement = fmt.Sprintf("%v", v) == "true"
	}
	if isImprovement {
		intent := fmt.Sprintf("%v", m["intent"])
		if intent == "<nil>" {
			intent = ""
		}
		return &UserIntent{IsImprovement: true, Intent: intent}, nil
	}
	return nil, nil
}
```

- [ ] **Step 4: 添加 DetectTrajectorySignals 和 DetectTrajectoryIssues 方法**

```go
// DetectTrajectorySignals 分析团队轨迹，返回标准被动演化信号。
//
// 对应 Python: TeamSignalDetector.detect_trajectory_signals(trajectory, skill_name, skill_content)
func (d *TeamSignalDetector) DetectTrajectorySignals(
	ctx context.Context,
	traj *trajectory.Trajectory,
	skillName, skillContent string,
) ([]*EvolutionSignal, error) {
	issues, err := d.DetectTrajectoryIssues(ctx, traj, skillContent)
	if err != nil {
		return nil, err
	}
	if len(issues) == 0 {
		return nil, nil
	}
	return []*EvolutionSignal{
		MakeTeamTrajectorySignal(skillName, skillContent, issues),
	}, nil
}

// DetectTrajectoryIssues 返回规范化的 medium/high 严重度轨迹问题。
//
// 对应 Python: TeamSignalDetector.detect_trajectory_issues(trajectory, skill_content)
func (d *TeamSignalDetector) DetectTrajectoryIssues(
	ctx context.Context,
	traj *trajectory.Trajectory,
	skillContent string,
) ([]map[string]string, error) {
	trajectorySummary := BuildTeamTrajectorySummary(traj)
	promptTemplate := teamTrajectoryIssuePromptCN
	if d.language != "cn" {
		promptTemplate = teamTrajectoryIssuePromptEN
	}
	skillContentTruncated := skillContent
	if len(skillContentTruncated) > 10000 {
		skillContentTruncated = skillContentTruncated[:10000]
	}
	prompt := strings.ReplaceAll(promptTemplate, "{skill_content}", skillContentTruncated)
	prompt = strings.ReplaceAll(prompt, "{trajectory_summary}", trajectorySummary)

	raw, err := llm_resilience.InvokeTextWithRetry(
		ctx, d.llm, d.model, prompt, d.trajectoryIssueLLMPolicy,
		llm_resilience.WithIsResultUsable(func(text string) bool {
			parsed := ParseTeamModelJSON(text)
			_, ok := parsed.([]any)
			return ok
		}),
	)
	if err != nil {
		logger.Warn(logComponent).
			Str("method", "DetectTrajectoryIssues").
			Str("error", err.Error()).
			Msg("[TeamSignalDetector] detect_trajectory_issues failed")
		return nil, err
	}

	parsed := ParseTeamModelJSON(raw)
	if parsed == nil {
		return nil, nil
	}
	list, ok := parsed.([]any)
	if !ok {
		return nil, nil
	}

	var issues []map[string]string
	for _, item := range list {
		normalized := normalizeIssue(item)
		if normalized == nil {
			continue
		}
		if normalized["severity"] == "medium" || normalized["severity"] == "high" {
			issues = append(issues, normalized)
		}
	}
	return issues, nil
}
```

- [ ] **Step 5: 在 team_test.go 添加 TeamSignalDetector 测试**

```go
func TestNewTeamSignalDetector_无策略panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewTeamSignalDetector without policy should panic")
		}
	}()
	NewTeamSignalDetector(nil, "", "cn", nil, nil)
}

func TestTeamSignalType_值对齐(t *testing.T) {
	if TeamSignalTypeUserIntent != "user_intent" {
		t.Errorf("TeamSignalTypeUserIntent = %q, want %q", TeamSignalTypeUserIntent, "user_intent")
	}
	if TeamSignalTypeUserRequest != "user_request" {
		t.Errorf("TeamSignalTypeUserRequest = %q, want %q", TeamSignalTypeUserRequest, "user_request")
	}
	if TeamSignalTypeTrajectoryIssue != "trajectory_issue" {
		t.Errorf("TeamSignalTypeTrajectoryIssue = %q, want %q", TeamSignalTypeTrajectoryIssue, "trajectory_issue")
	}
}

func TestUserIntent_字段(t *testing.T) {
	ui := UserIntent{IsImprovement: true, Intent: "improve collaboration"}
	if !ui.IsImprovement {
		t.Error("IsImprovement should be true")
	}
	if ui.Intent != "improve collaboration" {
		t.Errorf("Intent = %q, want %q", ui.Intent, "improve collaboration")
	}
}

func TestTrajectoryIssue_字段(t *testing.T) {
	ti := TrajectoryIssue{
		IssueType:    "coordination",
		Description:  "data not passed",
		AffectedRole: "worker",
		Severity:     "high",
	}
	if ti.Severity != "high" {
		t.Errorf("Severity = %q, want %q", ti.Severity, "high")
	}
}
```

- [ ] **Step 6: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/signal/ -v`
Expected: ALL PASS

- [ ] **Step 7: Commit**

```bash
git add internal/evolving/signal/team.go internal/evolving/signal/team_test.go
git commit -m "feat(evolving): add TeamSignalDetector with DetectUserIntent, DetectTrajectorySignals and DetectTrajectoryIssues"
```

---

### Task 8: 更新 doc.go 文件

**Files:**
- Modify: `internal/evolving/signal/doc.go`

- [ ] **Step 1: 更新 signal/doc.go 文件目录**

```go
// Package signal 提供自演化信号类型与转换工具。
//
// 信号（EvolutionSignal）标识 Agent 执行过程中的问题类型和诊断信息，
// 驱动优化器决定优化方向。本包同时提供离线评估结果到信号的转换函数，
// 以及在线对话信号检测和团队域信号检测。
//
// 文件目录：
//
//	signal/
//	├── doc.go           # 包文档
//	├── signal.go        # EvolutionSignal 结构体 + 枚举 + 工厂 + 指纹
//	├── from_eval.go     # EvaluatedCase → EvolutionSignal 转换
//	├── from_conv.go     # ConversationSignalDetector 对话信号检测
//	└── team.go          # TeamSignalDetector 团队信号检测 + JSON 解析
//
// 对应 Python 代码：openjiuwen/agent_evolving/signal/
package signal
```

- [ ] **Step 2: Commit**

```bash
git add internal/evolving/signal/doc.go
git commit -m "docs(evolving): update signal doc.go with from_conv and team entries"
```

---

### Task 9: 更新 IMPLEMENTATION_PLAN.md 状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 9.73 状态为 ✅**

将 9.73 行的状态从 `☐` 改为 `✅`。

- [ ] **Step 2: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "chore: mark 9.73 SignalDetector as completed"
```

---

### Task 10: 全量测试和编译验证

**Files:** 无修改

- [ ] **Step 1: 运行全量编译**

Run: `cd /home/opensource/uap-claw-go && go build ./...`
Expected: 无编译错误

- [ ] **Step 2: 运行 signal 包测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/signal/ -v -cover`
Expected: ALL PASS, coverage ≥ 85%

- [ ] **Step 3: 运行 trajectory 包测试确认无回归**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/trajectory/ -v`
Expected: ALL PASS

- [ ] **Step 4: 运行 evolving 全包测试确认无回归**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/... -v`
Expected: ALL PASS
