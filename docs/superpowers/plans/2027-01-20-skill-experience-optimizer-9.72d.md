# 9.72d SkillExperienceOptimizer 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 全量复刻 Python skill_call/ 目录的 SkillExperienceOptimizer + TeamSkillExperienceOptimizer + experience_draft_parser + templates，包含 TextualParameter.Gradients 类型从 string→any 的前置变更。

**Architecture:** SkillExperienceOptimizerBase 作为共享基类嵌入 BaseOptimizerMixin，两个优化器（个体 + 团队）分别嵌入该基类。draft_parser.go 统一 JSON 提取/解析辅助函数。templates.go 一比一复刻 Python 提示词原文。

**Tech Stack:** Go 1.x, internal/evolving 既有类型（checkpointing/signal/trajectory/experience/schema/optimizer/llm_resilience）

---

## 文件结构

**新建文件（skill_call 包）**：
- `internal/evolving/optimizer/skill_call/doc.go` — 包文档
- `internal/evolving/optimizer/skill_call/base.go` — 基类 + 常量
- `internal/evolving/optimizer/skill_call/draft_parser.go` — ParsedExperienceDraft + JSON 辅助
- `internal/evolving/optimizer/skill_call/experience_optimizer.go` — 个体优化器
- `internal/evolving/optimizer/skill_call/team_optimizer.go` — 团队优化器
- `internal/evolving/optimizer/skill_call/templates.go` — 提示词模板
- `internal/evolving/optimizer/skill_call/base_test.go` — 基类测试
- `internal/evolving/optimizer/skill_call/draft_parser_test.go` — 解析辅助测试
- `internal/evolving/optimizer/skill_call/experience_optimizer_test.go` — 个体优化器测试
- `internal/evolving/optimizer/skill_call/team_optimizer_test.go` — 团队优化器测试

**修改文件（前置变更）**：
- `internal/evolving/optimizer/base.go` — Gradients 类型 string→any
- `internal/evolving/optimizer/base_test.go` — 测试对齐
- `internal/evolving/optimizer/llm_call/instruction_optimizer.go` — GetGradient 类型断言

**修改文件（回填）**：
- `internal/evolving/optimizer/doc.go` — 添加 skill_call 子包条目
- `IMPLEMENTATION_PLAN.md` — 更新 9.72d 状态

---

## Task 1: TextualParameter.Gradients 类型从 string → any

**Files:**
- Modify: `internal/evolving/optimizer/base.go`
- Modify: `internal/evolving/optimizer/base_test.go`
- Modify: `internal/evolving/optimizer/llm_call/instruction_optimizer.go`
- Test: `go test ./internal/evolving/optimizer/... -v`

- [ ] **Step 1: 修改 base.go — TextualParameter 类型和方法签名**

将 `TextualParameter.Gradients` 从 `map[string]string` 改为 `map[string]any`，`SetGradient`/`GetGradient` 方法签名对应变更，空字符串""表示未设置改为 nil 表示未设置。

修改点：
1. `Gradients map[string]string` → `Gradients map[string]any`
2. `NewTextualParameter` 中 `Gradients: map[string]string{}` → `Gradients: map[string]any{}`
3. `SetGradient(name string, gradient string)` → `SetGradient(name string, gradient any)`
4. `GetGradient(name string) string` → `GetGradient(name string) any`
5. 注释更新：`// Gradients 梯度映射 target → 梯度值（any 类型，对齐 Python Dict[str, Any]）`，`// nil 表示未设置/已清除，对齐 Python 的 None 语义`

- [ ] **Step 2: 修改 base_test.go — 测试对齐**

调整 `TestTextualParameter_梯度操作`：
```go
p.SetGradient("system_prompt", "improved prompt")  // string → any 自动兼容
val := p.GetGradient("system_prompt")
if val != "improved prompt" {
    t.Error("gradient mismatch")
}
if p.GetGradient("nonexistent") != nil {  // nil 替代 "" 表示未设置
    t.Error("nonexistent gradient should be nil")
}
```

调整 `TestTextualParameter_默认空Gradients`：
```go
if p.Gradients == nil {
    t.Error("Gradients should be initialized as empty map, not nil")
}
if len(p.Gradients) != 0 {
    t.Errorf("Gradients len = %d, want 0", len(p.Gradients))
}
```
（此处无需改动，map[string]any 的空 map 和 map[string]string 一样）

- [ ] **Step 3: 修改 instruction_optimizer.go — GetGradient 类型断言**

所有 `param.SetGradient("xxx", "")` 改为 `param.SetGradient("xxx", nil)`。
所有 `param.GetGradient("xxx")` 的直接 string 使用改为类型断言：

```go
// 之前: param.SetGradient("system_prompt_optimized", "")
param.SetGradient("system_prompt_optimized", nil)

// 之前: if sysVal := param.GetGradient("system_prompt_optimized"); sysVal != "" {
sysValAny := param.GetGradient("system_prompt_optimized")
sysVal, _ := sysValAny.(string)
if sysVal != "" {
    updates[schema.UpdateKey{opID, "system_prompt"}] = sysVal
}

// 之前: gradient := param.GetGradient("system_prompt")
gradientAny := param.GetGradient("system_prompt")
gradient, _ := gradientAny.(string)
```

同理 `user_prompt_optimized` 和 `user_prompt` 的所有 GetGradient 调用。

- [ ] **Step 4: 运行测试确认现有代码不受影响**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/evolving/optimizer/... -v -count=1`
Expected: 所有 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/evolving/optimizer/base.go internal/evolving/optimizer/base_test.go internal/evolving/optimizer/llm_call/instruction_optimizer.go
git commit -m "refactor: TextualParameter.Gradients 从 map[string]string 改为 map[string]any

对齐 Python TextualParameter.gradients 的 Dict[str, Any] 类型。
SetGradient 参数从 string → any，GetGradient 返回值从 string → any。
空字符串""表示未设置改为 nil 表示未设置（对齐 Python None）。
InstructionOptimizer 的 GetGradient 调用改为类型断言 .(string)。"
```

---

## Task 2: skill_call 包骨架 — doc.go + base.go + base_test.go

**Files:**
- Create: `internal/evolving/optimizer/skill_call/doc.go`
- Create: `internal/evolving/optimizer/skill_call/base.go`
- Create: `internal/evolving/optimizer/skill_call/base_test.go`
- Test: `go test ./internal/evolving/optimizer/skill_call/... -v`

- [ ] **Step 1: 创建 doc.go**

```go
// Package skill_call 提供技能经验维度优化器。
//
// SkillExperienceOptimizerBase 固定 domain="skill_experience"，默认优化目标为 experiences，
// 对齐 Python SkillExperienceOptimizer 的共享基类行为。
// SkillExperienceOptimizer 通过 LLM 三通道分析生成经验草稿，
// TeamSkillExperienceOptimizer 在此基础上增加双路径生成逻辑和团队专属功能。
//
// 文件目录：
//
//	skill_call/
//	├── doc.go                    # 包文档
//	├── base.go                   # SkillExperienceOptimizerBase（技能经验优化器基类） 共享字段/方法/常量
//	├── draft_parser.go           # ParsedExperienceDraft + JSON 提取/解析辅助函数
//	├── experience_optimizer.go   # SkillExperienceOptimizer（个体技能经验优化器） Backward/Step/GenerateRecords/RetryParse + 辅助函数
//	├── team_optimizer.go         # TeamSkillExperienceOptimizer（团队技能经验优化器） 双路径 GenerateRecords/UserPatch/TrajectoryPatch/RegenerateBody/callLLM + 辅助函数
//	└── templates.go              # 提示词模板（CN+EN 双语，一比一复刻 Python 原文）
//
// 对应 Python 代码：openjiuwen/agent_evolving/optimizer/skill_call/
package skill_call
```

- [ ] **Step 2: 创建 base.go — SkillExperienceOptimizerBase + 常量**

完整实现 `SkillExperienceOptimizerBase` 结构体（嵌入 `BaseOptimizerMixin` + `llm/model/language/onlineContexts` 字段）、Domain/DefaultTargets/RequiresForwardData/Bind/UpdateLLM 方法、所有共享常量（InitialScoreBySignal/GenerateRecordsLLMPolicy/TeamSkillRecordLLMPolicy/TeamInitialScoreBySignal 等）。

按项目编码规范排列：结构体 → 枚举 → 常量 → 全局变量 → 导出函数 → 非导出函数，使用中文注释，分隔注释行。

```go
package skill_call

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/experience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillExperienceOptimizerBase 技能经验优化器共享基类。
//
// 两个子类（SkillExperienceOptimizer / TeamSkillExperienceOptimizer）
// 共享 Domain/DefaultTargets/RequiresForwardData/Bind 等行为，
// 通过嵌入 BaseOptimizerMixin 避免重复实现。
//
// 对应 Python: SkillExperienceOptimizer 和 TeamSkillExperienceOptimizer 的共享字段
type SkillExperienceOptimizerBase struct {
	optimizer.BaseOptimizerMixin
	// llm LLM 模型实例
	llm *llm.Model
	// model 模型名称
	model string
	// language 语言（"cn" 或 "en"）
	language string
	// onlineContexts 在线演进上下文映射（skill_name → EvolutionContext）
	onlineContexts map[string]*experience.EvolutionContext
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// SkillContentMaxChars 个体优化器技能内容截断上限
	SkillContentMaxChars = 6000
	// SectionPreviewChars 章节预览字符数
	SectionPreviewChars = 200
	// ContextMaxChars 上下文拼接上限
	ContextMaxChars = 500
	// RetryParseTimeoutSecs 重试解析超时（秒）
	RetryParseTimeoutSecs = 20
	// TeamSkillContentMaxChars 团队优化器技能内容截断上限
	TeamSkillContentMaxChars = 6000
	// PatchRetrySkillContentChars 团队 patch 重试时技能内容截断上限
	PatchRetrySkillContentChars = 3000
	// PatchRetryTrajectoryChars 团队 patch 重试时轨迹截断上限
	PatchRetryTrajectoryChars = 6000
	// TrajectoryIssuesRetryChars 团队 patch 重试时轨迹问题截断上限
	TrajectoryIssuesRetryChars = 2000
	// UserIntentRetryChars 团队 patch 重试时用户意图截断上限
	UserIntentRetryChars = 500
	// SummaryRetryChars 团队 patch 重试时摘要截断上限
	SummaryRetryChars = 200
	// TeamEvolutionPreviewChars 团队已有演进预览截断上限
	TeamEvolutionPreviewChars = 200
	// TeamEvolutionMaxRecords 团队已有演进最大展示条数
	TeamEvolutionMaxRecords = 6
	// TeamRetryParseTimeoutSecs 团队重试解析超时（秒）
	TeamRetryParseTimeoutSecs = 20
)

// ──────────────────────────── 全局变量 ────────────────────────────

// InitialScoreBySignal 个体优化器信号类型→初始评分映射
// 对应 Python: INITIAL_SCORE_BY_SIGNAL
var InitialScoreBySignal = map[string]float64{
	"execution_failure":   0.65,
	"user_correction":     0.70,
	"script_artifact":     0.60,
	"conversation_review": 0.50,
}

// GenerateRecordsLLMPolicy 默认的个体记录生成 LLM 调用策略
// 对应 Python: GENERATE_RECORDS_LLM_POLICY
var GenerateRecordsLLMPolicy = llm_resilience.LLMInvokePolicy{
	AttemptTimeoutSecs: 150,
	TotalBudgetSecs:    300,
	MaxAttempts:        2,
}

// TeamSkillRecordLLMPolicy 默认的团队记录生成 LLM 调用策略
// 对应 Python: TEAM_SKILL_RECORD_LLM_POLICY
var TeamSkillRecordLLMPolicy = llm_resilience.LLMInvokePolicy{
	AttemptTimeoutSecs: 120,
	TotalBudgetSecs:    420,
	MaxAttempts:        3,
}

// TeamInitialScoreBySignal 团队优化器信号类型→初始评分映射
// 对应 Python: TEAM_INITIAL_SCORE_BY_SIGNAL
var TeamInitialScoreBySignal = map[string]float64{
	"trajectory_issue": 0.65,
	"user_intent":      0.70,
	"team_skill_mixed": 0.68,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Domain 返回优化器域 "skill_experience"。
func (b *SkillExperienceOptimizerBase) Domain() string {
	return "skill_experience"
}

// DefaultTargets 返回默认优化目标列表 ["experiences"]。
func (b *SkillExperienceOptimizerBase) DefaultTargets() []string {
	return []string{schema.ExperiencesTarget}
}

// RequiresForwardData 返回 true，技能经验优化需要前向数据。
func (b *SkillExperienceOptimizerBase) RequiresForwardData() bool {
	return true
}

// Bind 过滤并绑定可优化的 Operator，从 config 提取 online_contexts。
//
// 对齐 Python:
//
//	self._online_contexts = dict(config.get("online_contexts") or {})
//	return super().bind(operators=operators, targets=targets, **config)
func (b *SkillExperienceOptimizerBase) Bind(operators map[string]operator.Operator, targets []string, config map[string]any) int {
	if len(targets) == 0 {
		targets = b.DefaultTargets()
	}
	// 对齐 Python: self._online_contexts = dict(config.get("online_contexts") or {})
	if config != nil {
		if oc, ok := config["online_contexts"]; ok && oc != nil {
			b.onlineContexts = make(map[string]*experience.EvolutionContext)
			switch v := oc.(type) {
			case map[string]*experience.EvolutionContext:
				for k, val := range v {
					b.onlineContexts[k] = val
				}
			case map[string]any:
				for k, val := range v {
					if ectx, ok := val.(*experience.EvolutionContext); ok {
						b.onlineContexts[k] = ectx
					}
				}
			default:
				b.onlineContexts = map[string]*experience.EvolutionContext{}
			}
		} else {
			b.onlineContexts = map[string]*experience.EvolutionContext{}
		}
	} else {
		b.onlineContexts = map[string]*experience.EvolutionContext{}
	}
	return b.BaseOptimizerMixin.Bind(operators, targets, config)
}

// UpdateLLM 更新运行时 llm/model（热重载）。
// 对齐 Python: SkillExperienceOptimizer.update_llm(llm, model)
func (b *SkillExperienceOptimizerBase) UpdateLLM(newLLM *llm.Model, newModel string) {
	if newLLM == nil {
		logger.Warn(logger.ComponentAgentCore).Msg("[SkillExperienceOptimizer] UpdateLLM: llm 为 nil，拒绝更新")
		return
	}
	b.llm = newLLM
	b.model = newModel
}

// LLM 返回配置的 LLM 客户端。
func (b *SkillExperienceOptimizerBase) LLM() *llm.Model {
	return b.llm
}

// ModelName 返回配置的模型名称。
func (b *SkillExperienceOptimizerBase) ModelName() string {
	return b.model
}

// Language 返回配置的语言。
func (b *SkillExperienceOptimizerBase) Language() string {
	return b.language
}

// OnlineContexts 返回在线演进上下文映射。
func (b *SkillExperienceOptimizerBase) OnlineContexts() map[string]*experience.EvolutionContext {
	return b.onlineContexts
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// logComponent skill_call 包日志组件常量
const logComponent = logger.ComponentAgentCore

// removeSkillPrefix 从 operator ID 中移除 "skill_experience_" 前缀。
// 对齐 Python: op_id.removeprefix("skill_experience_")
func removeSkillPrefix(opID string) string {
	return strings.TrimPrefix(opID, "skill_experience_")
}
```

- [ ] **Step 3: 创建 base_test.go**

测试 SkillExperienceOptimizerBase 的 Domain/DefaultTargets/RequiresForwardData/Bind/UpdateLLM/常量验证：

```go
package skill_call

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	skillop "github.com/uapclaw/uap-claw-go/internal/agentcore/operator/skill_call"
	"github.com/uapclaw/uapclaw-go/internal/evolving/experience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSkillExperienceOptimizerBase_Domain(t *testing.T) {
	b := &SkillExperienceOptimizerBase{}
	assert.Equal(t, "skill_experience", b.Domain())
}

func TestSkillExperienceOptimizerBase_DefaultTargets(t *testing.T) {
	b := &SkillExperienceOptimizerBase{}
	assert.Equal(t, []string{schema.ExperiencesTarget}, b.DefaultTargets())
}

func TestSkillExperienceOptimizerBase_RequiresForwardData(t *testing.T) {
	b := &SkillExperienceOptimizerBase{}
	assert.True(t, b.RequiresForwardData())
}

func TestSkillExperienceOptimizerBase_Bind提取OnlineContexts(t *testing.T) {
	b := &SkillExperienceOptimizerBase{}
	op := skillop.NewSkillExperienceOperator("test_skill")
	ops := map[string]operator.Operator{op.OperatorID(): op}

	ctx := &experience.EvolutionContext{SkillName: "test_skill"}
	config := map[string]any{"online_contexts": map[string]*experience.EvolutionContext{"test_skill": ctx}}
	n := b.Bind(ops, nil, config)
	assert.Equal(t, 1, n)
	assert.NotNil(t, b.onlineContexts["test_skill"])
}

func TestSkillExperienceOptimizerBase_Bind无OnlineContexts(t *testing.T) {
	b := &SkillExperienceOptimizerBase{}
	op := skillop.NewSkillExperienceOperator("test_skill")
	ops := map[string]operator.Operator{op.OperatorID(): op}

	n := b.Bind(ops, nil, nil)
	assert.Equal(t, 1, n)
	assert.Equal(t, map[string]*experience.EvolutionContext{}, b.onlineContexts)
}

func TestSkillExperienceOptimizerBase_UpdateLLM(t *testing.T) {
	b := &SkillExperienceOptimizerBase{}
	// nil llm 应被拒绝
	b.UpdateLLM(nil, "new-model")
	assert.Nil(t, b.llm)
}

func TestInitialScoreBySignal_常量验证(t *testing.T) {
	assert.Equal(t, 0.65, InitialScoreBySignal["execution_failure"])
	assert.Equal(t, 0.70, InitialScoreBySignal["user_correction"])
	assert.Equal(t, 0.60, InitialScoreBySignal["script_artifact"])
	assert.Equal(t, 0.50, InitialScoreBySignal["conversation_review"])
	assert.Equal(t, 4, len(InitialScoreBySignal))
}

func TestGenerateRecordsLLMPolicy_常量验证(t *testing.T) {
	assert.Equal(t, 150.0, GenerateRecordsLLMPolicy.AttemptTimeoutSecs)
	assert.Equal(t, 300.0, GenerateRecordsLLMPolicy.TotalBudgetSecs)
	assert.Equal(t, 2, GenerateRecordsLLMPolicy.MaxAttempts)
}

func TestTeamInitialScoreBySignal_常量验证(t *testing.T) {
	assert.Equal(t, 0.65, TeamInitialScoreBySignal["trajectory_issue"])
	assert.Equal(t, 0.70, TeamInitialScoreBySignal["user_intent"])
	assert.Equal(t, 0.68, TeamInitialScoreBySignal["team_skill_mixed"])
	assert.Equal(t, 3, len(TeamInitialScoreBySignal))
}
```

注意：import path 中的 `skillop` alias 需要根据实际 module name 调整。先检查 go.mod 中的 module name：

- [ ] **Step 4: 运行测试确认编译通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/evolving/optimizer/skill_call/... -v -count=1`
Expected: PASS（可能 import path 需要微调，根据编译错误修正）

- [ ] **Step 5: 提交**

```bash
git add internal/evolving/optimizer/skill_call/doc.go internal/evolving/optimizer/skill_call/base.go internal/evolving/optimizer/skill_call/base_test.go
git commit -m "feat(skill_call): 添加 SkillExperienceOptimizerBase 基类 + 常量 + doc.go

SkillExperienceOptimizerBase 嵌入 BaseOptimizerMixin，
domain='skill_experience', default_targets=['experiences'],
requires_forward_data=true, bind 提取 online_contexts。
包含个体/团队评分映射、LLM 调用策略等共享常量。"
```

---

## Task 3: draft_parser.go — ParsedExperienceDraft + JSON 辅助函数

**Files:**
- Create: `internal/evolving/optimizer/skill_call/draft_parser.go`
- Create: `internal/evolving/optimizer/skill_call/draft_parser_test.go`
- Test: `go test ./internal/evolving/optimizer/skill_call/... -v -run TestDraft`

- [ ] **Step 1: 创建 draft_parser.go**

实现 `ParsedExperienceDraft` 结构体 + `NormalizeKeywords`/`NormalizeSummary`/`ParseExperienceDraft`/`ParseExperienceDraftsWithError` + `ExtractJSONWithError`/`LooksTruncated`/`FixJSONText`/`tryParse`/`extractJSON`。

对齐 Python `experience_draft_parser.py` 和 `experience_optimizer.py` 中的 `_extract_json_with_error`/`_fix_json_text`/`_try_parse`/`_extract_json`/`_looks_truncated`。

按项目编码规范排列，中文注释。关键是：
- `FixJSONText`: 去除代码块标记(```json)、去除注释(`//`)、去除尾逗号
- `tryParse`: 尝试 json.Unmarshal，失败返回 nil
- `ExtractJSONWithError`: 顺序尝试 → 直接解析 → fixJSONText 后解析 → 正则提取 `[\s\S]*` 和 `\{[\s\S]*\}` → fixJSONText 后再解析 → 返回 (nil, lastError)
- `LooksTruncated`: 开括号数 > 关括号数+1
- `ParseExperienceDraft`: action=="skip" 时返回 skip patch；否则 section∈VALID_SECTIONS 校验，target 解析为 EvolutionTarget，merge_target/keywords/summary 处理
- `ParseExperienceDraftsWithError`: 调用 extractFn 提取 JSON → data 为 list 或 dict → 逐条 ParseExperienceDraft

全部函数均为导出函数（供 skill_call 包内两个优化器使用），但不导出到包外（因为子包内部的逻辑不需要 optimizer 包级别导出）。

实际上按 Go 习惯，这些辅助函数可以都是导出的（首字母大写），因为它们在同一个包内被两个优化器文件使用。

- [ ] **Step 2: 创建 draft_parser_test.go**

约 15 个测试用例覆盖：
- `TestNormalizeKeywords_正常列表`: ["a","b","c"] → ["a","b","c"]
- `TestNormalizeKeywords_空列表`: [] → nil
- `TestNormalizeKeywords_非列表`: "abc" → nil
- `TestNormalizeKeywords_含空字符串`: ["a","","c"] → ["a","c"]
- `TestNormalizeSummary_正常字符串`: "hello  world" → "hello world"
- `TestNormalizeSummary_空字符串`: "" → nil
- `TestNormalizeSummary_null字符串`: "null" → nil
- `TestNormalizeSummary_非字符串`: 123 → nil
- `TestParseExperienceDraft_append`: 正常解析 → ParsedExperienceDraft
- `TestParseExperienceDraft_skip`: action=skip → patch.Action=="skip"
- `TestParseExperienceDraft_invalidSectionFallback`: section 不在 VALID_SECTIONS → fallback "Troubleshooting"
- `TestParseExperienceDraft_invalidTargetFallback`: target 无效 → fallback BODY
- `TestParseExperienceDraftsWithError_正常JSON数组`
- `TestParseExperienceDraftsWithError_单个JSON对象`
- `TestParseExperienceDraftsWithError_解析失败返回Nil`
- `TestExtractJSONWithError_正常JSON`
- `TestExtractJSONWithError_修复后成功`（含注释/尾逗号）
- `TestExtractJSONWithError_正则提取成功`
- `TestExtractJSONWithError_完全失败`
- `TestLooksTruncated_截断`
- `TestLooksTruncated_完整`
- `TestFixJSONText_去除代码块`
- `TestFixJSONText_去除注释`
- `TestFixJSONText_去除尾逗号`

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/evolving/optimizer/skill_call/... -v -run TestDraft -count=1`
Expected: ALL PASS

- [ ] **Step 4: 提交**

```bash
git add internal/evolving/optimizer/skill_call/draft_parser.go internal/evolving/optimizer/skill_call/draft_parser_test.go
git commit -m "feat(skill_call): 添加 ParsedExperienceDraft + JSON 提取/解析辅助函数

ParsedExperienceDraft 结构体、NormalizeKeywords/NormalizeSummary、
ParseExperienceDraft/ParseExperienceDraftsWithError、
ExtractJSONWithError/LooksTruncated/FixJSONText。
对齐 Python experience_draft_parser.py + experience_optimizer.py JSON 辅助。"
```

---

## Task 4: templates.go — 提示词模板（一比一复刻 Python）

**Files:**
- Create: `internal/evolving/optimizer/skill_call/templates.go`
- Test: `go build ./internal/evolving/optimizer/skill_call/...`

- [ ] **Step 1: 创建 templates.go**

**必须一比一复刻 Python 原文，不做自行翻译。**

从 `/home/opensource/agent-core/openjiuwen/agent_evolving/optimizer/skill_call/templates.py` 逐字复制所有提示词常量，转为 Go 的 `const` 或 `var` 定义。

包含以下变量（全部对齐 Python `templates.py` 的 `__all__` 导出列表）：
- `SkillExperienceGeneratePromptCN` — 个体经验生成提示词（中文）
- `SkillExperienceGeneratePromptEN` — 个体经验生成提示词（英文）
- `SkillExperienceGeneratePrompt` — `map[string]string{"cn": CN, "en": EN}`
- `JSONFixPrompt` — JSON 修复提示词
- `JSONFixPromptStrict` — JSON 严格修复提示词
- `UserPatchPromptCN` / `UserPatchPromptEN` / `UserPatchPrompt` — 用户 patch 提示词
- `TrajectoryPatchPromptCN` / `TrajectoryPatchPromptEN` / `TrajectoryPatchPrompt` — 轨迹 patch 提示词
- `TeamExperienceGeneratePromptCN` / `TeamExperienceGeneratePromptEN` / `TeamExperienceGeneratePrompt` — 团队经验生成提示词
- `TeamJSONFixPrompt` / `TeamJSONFixPromptStrict` — 团队 JSON 修复提示词

Go 实现方式：Python 的 f-string `{variable}` 转为 Go 的占位符 `{variable}`（保持原格式，后续通过 `strings.ReplaceAll` 替换），因为 Python 的 `.format()` 和 Go 的占位符格式冲突（Go 的 `{{` 在 Python `.format()` 中是 `{` 的转义），需要将 Python 中的 `{{` 转为 Go 中的 `{`（Python 的 `{{` 在 `.format()` 中表示字面量 `{`，Go 不需要双层转义）。

**关键转换规则**：
- Python f-string 中的 `{{` → Go 中的 `{`（因为 Python `.format()` 中 `{{` 是 `{` 的转义，Go 不需要）
- Python 中的 `{skill_content}` → Go 中的 `{skill_content}`（保持原占位符名，后续用 ReplaceAll 替换）
- Python 中的 `\n` → Go 中的实际换行（const 字符串可以跨行）
- Python 中的 `\\"` → Go 中的 `\"`（JSON 转义在提示词中是字面量文本）

- [ ] **Step 2: 确认编译通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/evolving/optimizer/skill_call/...`
Expected: 编译成功（无错误）

- [ ] **Step 3: 提交**

```bash
git add internal/evolving/optimizer/skill_call/templates.go
git commit -m "feat(skill_call): 添加提示词模板（一比一复刻 Python 原文）

包含个体/团队经验生成提示词(CN+EN)、JSON修复/严格修复提示词、
用户patch提示词、轨迹patch提示词、团队经验生成提示词。
对齐 Python templates.py 全量导出。"
```

---

## Task 5: experience_optimizer.go — SkillExperienceOptimizer 个体优化器

**Files:**
- Create: `internal/evolving/optimizer/skill_call/experience_optimizer.go`
- Create: `internal/evolving/optimizer/skill_call/experience_optimizer_test.go`
- Test: `go test ./internal/evolving/optimizer/skill_call/... -v`

- [ ] **Step 1: 创建 experience_optimizer.go**

实现 `SkillExperienceOptimizer` 结构体 + 构造函数 + Backward/Step/GenerateRecords/RetryParseDrafts/generateDraftsWithRetries 方法 + 辅助函数（buildConversationSnippet/summarizeSkillContent/splitIntoSections/previewSection/buildExistingSummary/limitSummaryLines/buildContext/_buildEvolutionContext）。

关键实现细节：
- `NewSkillExperienceOptimizer(llm, model, language, policy)` 构造函数
- `Backward(ctx, signals)` — 遍历 operators，按 skill_name 分组，构建 EvolutionContext，调用 GenerateRecords，写入 gradient（使用 SetGradient(any)），永远返回 nil
- `_buildEvolutionContext` — 从 onlineContexts 查找，不存在时返回 `exception.BuildError(exception.StatusToolchainAgentParamError, ...)`
- `GenerateRecords(ctx, evoCtx)` — 构建 primary/retry prompt，调用 generateDraftsWithRetries，限制 text≤2/script≤1
- `generateDraftsWithRetries(ctx, prompt, retryPrompt)` — invokeTextWithRetryAndPrompt + ParseExperienceDraftsWithError + RetryParseDrafts 循环（最多3次）
- `RetryParseDrafts(ctx, brokenRaw, originalPrompt, attempt, parseError)` — truncated→重新生成 / 格式错误→JSON_FIX_PROMPT / attempt≥3→JSON_FIX_PROMPT_STRICT
- `Step()` — 从 gradient 读取 []EvolutionRecord，类型断言，构建 UpdateKey 映射
- 辅助函数全部为非导出（首字母小写）

日志逐条对齐 Python（使用 `[SkillExperienceOptimizer]` 前缀，`logger.ComponentAgentCore`），字段映射对齐规则3。

- [ ] **Step 2: 创建 experience_optimizer_test.go**

约 12 个测试用例：
- `TestSkillExperienceOptimizer_构造函数`
- `TestSkillExperienceOptimizer_Bind提取OnlineContexts`
- `TestSkillExperienceOptimizer_Backward无信号`
- `TestSkillExperienceOptimizer_Backward上下文缺失时跳过`
- `TestSkillExperienceOptimizer_Step空Gradient`
- `TestBuildConversationSnippet_正常`
- `TestBuildConversationSnippet_空消息`
- `TestSummarizeSkillContent_短内容`
- `TestSummarizeSkillContent_长内容分节`
- `TestBuildExistingSummary_有记录`
- `TestBuildExistingSummary_无记录`
- `TestBuildContext_多信号`

LLM mock 测试（需要 mock llm.Model）：
- `TestSkillExperienceOptimizer_GenerateRecords_LLM返回有效JSON`
- `TestSkillExperienceOptimizer_GenerateRecords_LLM失败`

Mock 方式：复用 `llm_resilience_test.go` 的 `newMockModel` 模式（注册 mockBaseModelClient 到 ClientRegistry）。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' | xargs kill 2>/dev/null; go test ./internal/evolving/optimizer/skill_call/... -v -count=1`
Expected: ALL PASS

- [ ] **Step 4: 提交**

```bash
git add internal/evolving/optimizer/skill_call/experience_optimizer.go internal/evolving/optimizer/skill_call/experience_optimizer_test.go
git commit -m "feat(skill_call): 添加 SkillExperienceOptimizer 个体优化器

Backward/Step/GenerateRecords/RetryParseDrafts/generateDraftsWithRetries
+ buildConversationSnippet/summarizeSkillContent/buildExistingSummary
等辅助函数。日志逐条对齐 Python。"
```

---

## Task 6: team_optimizer.go — TeamSkillExperienceOptimizer 团队优化器

**Files:**
- Create: `internal/evolving/optimizer/skill_call/team_optimizer.go`
- Create: `internal/evolving/optimizer/skill_call/team_optimizer_test.go`
- Test: `go test ./internal/evolving/optimizer/skill_call/... -v`

- [ ] **Step 1: 创建 team_optimizer.go**

实现 `TeamSkillExperienceOptimizer` 结构体 + 构造函数 + Backward/Step/GenerateRecords/GenerateUserPatch/GenerateTrajectoryPatch/RegenerateBody/callLLM/RetryParseDrafts/generateDraftsWithRetries 方法 + 辅助函数（parsePatchResponse/summarizeSkillContentTeam/shortenExistingEvolutionsSummary/summarizeExistingEvolutions/buildFrontmatter/dumpRaw/_buildEvolutionContext/_buildContext）+ `TeamSkillOptimizer` 类型别名。

关键实现细节：
- `NewTeamSkillExperienceOptimizer(llm, model, language, debugDir, recordLLMPolicy, evolutionStore)` 构造函数
- `Backward(ctx, signals)` — 使用 Trajectory（从 Mixin.GetTrajectories() 获取），default_trajectory 为 fallback
- `_buildEvolutionContext` — 从 onlineContexts 查找，trajectory 为 nil 时填充 default_trajectory 返回副本
- `GenerateRecords(ctx, evoCtx)` — 双路径：检查 trajectory.steps 是否有 kind 属性 → 逐信号 patch 或聚合 LLM 流
- `GenerateUserPatch(ctx, trajectory, skillName, userIntent)` — 使用 USER_PATCH_PROMPT，parsePatchResponse，need_patch==false 返回 nil
- `GenerateTrajectoryPatch(ctx, trajectory, skillName, skillContent, issues)` — 使用 TRAJECTORY_PATCH_PROMPT
- `RegenerateBody(ctx, skillName, currentBody, records, userIntent)` — 重写 SKILL.md body
- `callLLM(ctx, prompt, retryPrompt, policy, isResultUsable)` — 无 policy→llm.Invoke，有 policy→InvokeTextWithRetry
- `TeamSkillOptimizer = TeamSkillExperienceOptimizer` 类型别名

日志使用 `[TeamSkillOptimizer]` 前缀，`logger.ComponentAgentCore`，逐条对齐 Python。

- [ ] **Step 2: 创建 team_optimizer_test.go**

约 12 个测试用例：
- `TestTeamSkillExperienceOptimizer_构造函数`
- `TestTeamSkillExperienceOptimizer_Domain`
- `TestTeamSkillExperienceOptimizer_DefaultTargets`
- `TestTeamSkillExperienceOptimizer_Bind`
- `TestTeamSkillExperienceOptimizer_Step空Gradient`
- `TestParsePatchResponse_正常`
- `TestParsePatchResponse_非dict`
- `TestSummarizeSkillContentTeam_截断`
- `TestShortenExistingEvolutionsSummary_截断`
- `TestSummarizeExistingEvolutions_有记录`
- `TestTeamSkillOptimizer_类型别名`

LLM mock 测试：
- `TestTeamSkillExperienceOptimizer_GenerateUserPatch_不需要`
- `TestTeamSkillExperienceOptimizer_callLLM_无Policy`

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' | xargs kill 2>/dev/null; go test ./internal/evolving/optimizer/skill_call/... -v -count=1`
Expected: ALL PASS

- [ ] **Step 4: 提交**

```bash
git add internal/evolving/optimizer/skill_call/team_optimizer.go internal/evolving/optimizer/skill_call/team_optimizer_test.go
git commit -m "feat(skill_call): 添加 TeamSkillExperienceOptimizer 团队优化器

双路径 GenerateRecords（聚合 LLM 流/逐信号 patch）、
GenerateUserPatch/GenerateTrajectoryPatch/RegenerateBody/callLLM。
TeamSkillOptimizer 类型别名对齐 Python。"
```

---

## Task 7: 回填 — optimizer/doc.go + IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `internal/evolving/optimizer/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`
- Test: `go build ./internal/evolving/optimizer/...`

- [ ] **Step 1: 更新 optimizer/doc.go — 添加 skill_call 子包条目**

在文件目录树中添加：

```
	└── skill_call/            # 技能经验优化器
	    ├── doc.go            # 包文档
	    ├── base.go           # SkillExperienceOptimizerBase（技能经验优化器基类） 共享字段/方法/常量
	    ├── draft_parser.go   # ParsedExperienceDraft + JSON 提取/解析辅助函数
	    ├── experience_optimizer.go # SkillExperienceOptimizer（个体技能经验优化器）
	    ├── team_optimizer.go  # TeamSkillExperienceOptimizer（团队技能经验优化器）
	    └── templates.go      # 提示词模板
```

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md — 9.72d 状态**

将 `| 9.72d | ☐ | SkillExperienceOptimizer |` 改为 `| 9.72d | ✅ | SkillExperienceOptimizer |`，更新描述加入回填信息。

- [ ] **Step 3: 确认编译通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/evolving/optimizer/...`
Expected: 编译成功

- [ ] **Step 4: 提交**

```bash
git add internal/evolving/optimizer/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 optimizer/doc.go 添加 skill_call 子包 + 更新 IMPLEMENTATION_PLAN.md 9.72d 状态为 ✅"
```

---

## Task 8: 全量测试 + 覆盖率验证

**Files:**
- Test: `go test ./internal/evolving/optimizer/... -cover`

- [ ] **Step 1: 运行全量 optimizer 包测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' | xargs kill 2>/dev/null; go test ./internal/evolving/optimizer/... -v -cover -count=1`
Expected: ALL PASS，skill_call 包覆盖率 ≥ 85%

- [ ] **Step 2: 检查覆盖率详情**

Run: `cd /home/opensource/uap-claw-go && go test -coverprofile=coverage.out ./internal/evolving/optimizer/skill_call/... && go tool cover -func=coverage.out | grep skill_call`
Expected: 各文件覆盖率达标

- [ ] **Step 3: 如果覆盖率不足 85%，补充测试**

根据覆盖率报告找出未覆盖的函数，补充针对性测试用例。

- [ ] **Step 4: 最终提交（如有补充）**

如果补充了测试用例，提交：
```bash
git add internal/evolving/optimizer/skill_call/
git commit -m "test(skill_call): 补充测试用例达到 85% 覆盖率"
```
