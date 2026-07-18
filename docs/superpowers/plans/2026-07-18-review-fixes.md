# 审查文档 2026-07-18 修复计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复审查文档 2026-07-18-logic-review.md 中已确认的 10 个真实存在的问题（S01-S06, G03-G04, G06-G07），对齐 Python 参考实现。

**Architecture:** 按依赖关系分三批执行：第一批修基础设施层（schema/共享常量/工具函数），第二批修业务逻辑层（operator/evaluator），第三批修服务层（swarm/server）。每批内的任务无强依赖可并行。

**Tech Stack:** Go 1.x, 标准库 encoding/json, 项目内部 schema/operator/session 包

---

## 文件变更清单

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 修改 | `internal/evolving/schema/update.go` | 扩展 ApplyResultWithErrors 签名 |
| 修改 | `internal/agentcore/operator/skill_call/skill_experience_operator.go` | 删除本地常量，修复 PreviewUpdate 错误路径 |
| 修改 | `internal/agentcore/operator/base.go` | 修复 DefaultApplyUpdate 调用点 |
| 修改 | `internal/evolving/trainer/trainer.go` | 修复 ApplyUpdates 调用点 |
| 修改 | `internal/agentcore/operator/llm_call/llm_call_operator.go` | 参数改为 []BaseMessage，重写 promptContent/GetState/LoadState |
| 修改 | `internal/evolving/evaluator/evaluator.go` | fmt.Sprintf→json.Marshal（2组6处） |
| 修改 | `internal/evolving/evaluator/metrics/llm_as_judge.go` | fmt.Sprintf→json.Marshal（1组3处） |
| 新建 | `internal/evolving/update_execution.go` | ExecuteUpdates/ApplyUpdates/SummarizeApplyResults |
| 新建 | `internal/evolving/update_execution_test.go` | 对应测试 |
| 修改 | `internal/evolving/doc.go` | 文件目录新增条目 |
| 修改 | `internal/swarm/server/handle_envelope.go` | switchMode 条件+session 持久化，cancel 路径纯读取，ACP 注入 |
| 修改 | `internal/swarm/server/runtime/uapclaw.go` | 实现 SwitchMode 完整逻辑 |
| 修改 | `internal/agentcore/operator/tool_call/tool_call_operator.go` | descriptions→map[string]any，删 toString |
| 修改 | `internal/swarm/server/handle_session.go` | sessionId→session_id |

---

## 第一批：基础设施层

### Task 1: S03 — 删除 SkillExperienceOperator 本地常量，改用 schema.ExperiencesTarget

**Files:**
- Modify: `internal/agentcore/operator/skill_call/skill_experience_operator.go:33-37,71,72,85,101`

- [ ] **Step 1: 删除本地常量**

将第 33-37 行的 `experiencesTarget` 本地常量块删除：

```go
// 删除以下代码：
const (
    // experiencesTarget 经验目标名，一比一复刻 Python protocols.EXPERIENCES_TARGET。
    // 对应 Python: EXPERIENCES_TARGET = "experiences"
    experiencesTarget = "experiences"
)
```

- [ ] **Step 2: 替换 4 处引用为 `schema.ExperiencesTarget`**

```go
// 第 71 行：GetTunables
// 原：experiencesTarget: {
// 改：
schema.ExperiencesTarget: {

// 第 72 行：GetTunables
// 原：Name: experiencesTarget,
// 改：
Name: schema.ExperiencesTarget,

// 第 85 行：SetParameter
// 原：if target != experiencesTarget {
// 改：
if target != schema.ExperiencesTarget {

// 第 101 行：PreviewUpdate
// 原：if target != experiencesTarget {
// 改：
if target != schema.ExperiencesTarget {
```

- [ ] **Step 3: 运行测试确认**

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uap-claw-go
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; sleep 1
go test ./internal/agentcore/operator/skill_call/... -v -count=1
```

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/operator/skill_call/skill_experience_operator.go
git commit -m "fix(S03): 删除 SkillExperienceOperator 本地常量，改用 schema.ExperiencesTarget"
```

---

### Task 2: S02 — 扩展 ApplyResultWithErrors 签名，补全 ChangeType 和 Metadata

**Files:**
- Modify: `internal/evolving/schema/update.go:198-211`（定义）
- Modify: `internal/agentcore/operator/skill_call/skill_experience_operator.go:102-107,111-118`（2处调用）
- Modify: `internal/agentcore/operator/base.go:117-121`（1处调用）
- Modify: `internal/evolving/trainer/trainer.go:173-177`（1处调用）

- [ ] **Step 1: 扩展 ApplyResultWithErrors 签名**

在 `internal/evolving/schema/update.go` 中，将第 198-211 行改为：

```go
// ApplyResultWithErrors 创建失败的 ApplyResult，包含 changeType 和 metadata。
//
// 对应 Python: ApplyResult(applied=False, change_type=..., metadata=dict(...), errors=[...])
func ApplyResultWithErrors(operatorID, target string, mode UpdateMode, effect UpdateEffect, value any, changeType *string, metadata map[string]any, errs ...string) ApplyResult {
	return ApplyResult{
		OperatorID: operatorID,
		Target:     target,
		Applied:    false,
		Mode:       mode,
		Effect:     effect,
		Value:      value,
		ChangeType: changeType,
		Records:    []any{},
		Errors:     errs,
		Metadata:   MetadataClone(metadata),
	}
}
```

- [ ] **Step 2: 修复 SkillExperienceOperator 的 2 处调用**

在 `internal/agentcore/operator/skill_call/skill_experience_operator.go` 中：

第 102-107 行改为：
```go
return schema.ApplyResultWithErrors(
	op.OperatorID(), target,
	update.Mode, update.Effect, update.Payload,
	update.ChangeType, update.Metadata,
	fmt.Sprintf("unsupported target for SkillExperienceOperator: %s", target),
)
```

第 111-118 行改为：
```go
return schema.ApplyResultWithErrors(
	op.OperatorID(), target,
	update.Mode, update.Effect, update.Payload,
	update.ChangeType, update.Metadata,
	fmt.Sprintf(
		"unsupported update mode/effect for SkillExperienceOperator: %s/%s",
		update.Mode, update.Effect,
	),
)
```

- [ ] **Step 3: 修复 DefaultApplyUpdate 的调用**

在 `internal/agentcore/operator/base.go` 第 117-121 行改为：

```go
return schema.ApplyResultWithErrors(
	op.OperatorID(), target,
	update.Mode, update.Effect, update.Payload,
	update.ChangeType, update.Metadata,
	"unsupported update mode/effect for compatibility operator: "+string(update.Mode)+"/"+string(update.Effect),
)
```

- [ ] **Step 4: 修复 trainer.ApplyUpdates 的调用**

在 `internal/evolving/trainer/trainer.go` 第 173-177 行改为：

```go
results = append(results, schema.ApplyResultWithErrors(
	key.OperatorID(), key.Target(),
	update.Mode, update.Effect, update.Payload,
	update.ChangeType, update.Metadata,
	"operator not found: "+key.OperatorID(),
))
```

- [ ] **Step 5: 修复测试文件中的调用**

搜索所有测试文件中 `ApplyResultWithErrors` 的调用，补充 `nil, nil` 两个新参数：

```bash
cd /home/opensource/uap-claw-go
grep -rn "ApplyResultWithErrors" --include="*_test.go"
```

对每个匹配行，在 `update.Payload`（或等价的 value 参数）后面插入 `nil, nil`。

- [ ] **Step 6: 运行测试确认**

```bash
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; sleep 1
go test ./internal/evolving/schema/... ./internal/agentcore/operator/... ./internal/evolving/trainer/... -v -count=1
```

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/evolving/schema/update.go internal/agentcore/operator/skill_call/skill_experience_operator.go internal/agentcore/operator/base.go internal/evolving/trainer/trainer.go
# 加上测试文件
git add -u
git commit -m "fix(S02): 扩展 ApplyResultWithErrors 签名，补全 ChangeType 和 Metadata"
```

---

### Task 3: S04 — 统一替换 fmt.Sprintf 为 JSON 序列化 + nil 保护（4 组 11 处）

**Files:**
- Modify: `internal/evolving/evaluator/evaluator.go:120-122,284-286`
- Modify: `internal/evolving/evaluator/metrics/llm_as_judge.go:88-90`
- Modify: `internal/agentcore/operator/llm_call/llm_call_operator.go:228-237`

- [ ] **Step 1: 在 evaluator 包中新增工具函数 `formatValue`**

在 `internal/evolving/evaluator/evaluator.go` 的非导出函数区新增：

```go
// formatValue 将任意值序列化为字符串，用于 LLM 模板填充。
// nil 或零值 → 空字符串，非空 → JSON 序列化。
// 对齐 Python 的 str(value or "") 语义。
func formatValue(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		if val == "" {
			return ""
		}
		return val
	case map[string]any:
		if len(val) == 0 {
			return ""
		}
	case []any:
		if len(val) == 0 {
			return ""
		}
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}
```

确保 import 中有 `"encoding/json"` 和 `"fmt"`。

- [ ] **Step 2: 替换 evaluator.go 第 120-122 行**

```go
// 原：
// "question":        fmt.Sprintf("%v", case_.Inputs),
// "expected_answer": fmt.Sprintf("%v", case_.Label),
// "model_answer":    fmt.Sprintf("%v", predict),
// 改：
"question":        formatValue(case_.Inputs),
"expected_answer": formatValue(case_.Label),
"model_answer":    formatValue(predict),
```

- [ ] **Step 3: 替换 evaluator.go 第 284-286 行**

```go
// 原：
// "question":                     fmt.Sprintf("%v", case_.Inputs),
// "expected_answer":              fmt.Sprintf("%v", case_.Label),
// "model_answer":                 fmt.Sprintf("%v", predict),
// 改：
"question":                     formatValue(case_.Inputs),
"expected_answer":              formatValue(case_.Label),
"model_answer":                 formatValue(predict),
```

- [ ] **Step 4: 替换 llm_as_judge.go 第 88-90 行**

在 `internal/evolving/evaluator/metrics/llm_as_judge.go` 中，添加 import `"encoding/json"` 和 `"fmt"`。

同样在非导出函数区新增 `formatValue` 函数（与 evaluator.go 相同实现），然后替换：

```go
// 原：
// "question":        fmt.Sprintf("%v", mc.question),
// "expected_answer": fmt.Sprintf("%v", label),
// "model_answer":    fmt.Sprintf("%v", prediction),
// 改：
"question":        formatValue(mc.question),
"expected_answer": formatValue(label),
"model_answer":    formatValue(prediction),
```

注意：如果 `formatValue` 可以提取到共享位置（如 metrics 包的公共文件），优先提取。但由于 evaluator.go 和 llm_as_judge.go 在不同包，各自维护一份也可接受。

- [ ] **Step 5: 重写 llm_call_operator.go 的 promptContent 函数**

在 `internal/agentcore/operator/llm_call/llm_call_operator.go` 中，将第 228-237 行的 `promptContent` 函数改为：

```go
// promptContent 将任意值转为 PromptTemplate 可接受的内容。
// string 直接返回，[]any 保留原始结构，其他类型 JSON 序列化。
// 对应 Python: content = value if isinstance(value, (str, list)) else str(value)
func promptContent(value any) any {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		return v
	case []map[string]any:
		return v
	default:
		if v == nil {
			return ""
		}
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(data)
	}
}
```

同时需要修改 `promptContent` 的所有调用点——因为返回类型从 `string` 变为 `any`，`NewPromptTemplate` 的第二个参数需要确认是否支持 `any`。根据验证结果，`PromptTemplate.Content` 类型为 `any`，`NewPromptTemplate(name, content any)` 已支持。

检查 `SetParameter` 和 `LoadState` 中调用 `promptContent` 后传给 `prompt.NewPromptTemplate` 的代码，确认兼容。

确保 import 中有 `"encoding/json"` 和 `"fmt"`。

- [ ] **Step 6: 运行测试确认**

```bash
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; sleep 1
go test ./internal/evolving/evaluator/... ./internal/agentcore/operator/llm_call/... -v -count=1
```

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/evolving/evaluator/evaluator.go internal/evolving/evaluator/metrics/llm_as_judge.go internal/agentcore/operator/llm_call/llm_call_operator.go
git commit -m "fix(S04): fmt.Sprintf 替换为 JSON 序列化 + nil 保护，对齐 Python str(value or '')"
```

---

## 第二批：业务逻辑层

### Task 4: S01 — LLMCallOperator 构造函数参数改为 []schema.BaseMessage

**Files:**
- Modify: `internal/agentcore/operator/llm_call/llm_call_operator.go:66,73-74,139-144,151-166`
- Modify: `internal/agentcore/operator/llm_call/llm_call_operator_test.go`

- [ ] **Step 1: 修改 NewLLMCallOperator 签名**

将第 66 行的构造函数签名从：

```go
func NewLLMCallOperator(systemPrompt, userPrompt string, opts ...LLMCallOperatorOption) *LLMCallOperator {
```

改为：

```go
func NewLLMCallOperator(systemPrompt, userPrompt []schema.BaseMessage, opts ...LLMCallOperatorOption) *LLMCallOperator {
```

同时修改第 73-74 行：

```go
// 原：
// systemPrompt:       prompt.NewPromptTemplate("", systemPrompt),
// userPrompt:         prompt.NewPromptTemplate("", userPrompt),
// 改：
systemPrompt:       prompt.NewPromptTemplate("", systemPrompt),
userPrompt:         prompt.NewPromptTemplate("", userPrompt),
```

确保 import 中有 `schema "github.com/uapclaw/uapclaw-go/internal/agentcore/schema"`（或已有的正确 import 路径）。需要确认 `BaseMessage` 的实际 import 路径。

- [ ] **Step 2: 修改 GetState 返回值**

第 139-144 行改为：

```go
func (op *LLMCallOperator) GetState() map[string]any {
	return map[string]any{
		TargetSystemPrompt: op.systemPrompt.Content,
		TargetUserPrompt:   op.userPrompt.Content,
	}
}
```

无需改动代码——`Content` 字段类型已是 `any`，会自动保留原始类型（`string` 或 `[]schema.BaseMessage`）。

- [ ] **Step 3: 修改 LoadState 支持多类型还原**

第 151-166 行改为：

```go
func (op *LLMCallOperator) LoadState(state map[string]any) {
	if sp, ok := state[TargetSystemPrompt]; ok {
		content := promptContent(sp)
		op.systemPrompt = prompt.NewPromptTemplate("", content)
		if !op.freezeSystemPrompt && op.onParameterUpdated != nil {
			op.onParameterUpdated(TargetSystemPrompt, op.systemPrompt.Content)
		}
	}
	if up, ok := state[TargetUserPrompt]; ok {
		content := promptContent(up)
		op.userPrompt = prompt.NewPromptTemplate("", content)
		if !op.freezeUserPrompt && op.onParameterUpdated != nil {
			op.onParameterUpdated(TargetUserPrompt, op.userPrompt.Content)
		}
	}
}
```

注意：`promptContent` 已在 Task 3 中改为返回 `any`，支持保留 `string`、`[]any`、`[]map[string]any` 等原始结构。

- [ ] **Step 4: 修改 SetParameter 适配多类型**

SetParameter 中调用 `promptContent` 的部分（约第 122 行），`promptContent` 已改为返回 `any`，需要确认后续传给 `prompt.NewPromptTemplate` 兼容。

- [ ] **Step 5: 修改测试文件**

所有测试中 `NewLLMCallOperator("system prompt", "user prompt", ...)` 调用需要改为：

```go
NewLLMCallOperator(
    []schema.BaseMessage{{Role: "user", Content: "system prompt"}},
    []schema.BaseMessage{{Role: "user", Content: "user prompt"}},
    ...,
)
```

需要确认 `BaseMessage` 结构体字段名。搜索实际定义确认。

- [ ] **Step 6: 运行测试确认**

```bash
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; sleep 1
go test ./internal/agentcore/operator/llm_call/... -v -count=1
```

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/agentcore/operator/llm_call/
git commit -m "fix(S01): LLMCallOperator 参数改为 []BaseMessage，支持多消息格式 prompt"
```

---

### Task 5: S05 — 新建 update_execution.go，实现 ExecuteUpdates/ApplyUpdates/SummarizeApplyResults

**Files:**
- Create: `internal/evolving/update_execution.go`
- Create: `internal/evolving/update_execution_test.go`
- Modify: `internal/evolving/doc.go`

- [ ] **Step 1: 创建 update_execution.go**

```go
package evolving

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ExecuteUpdates 归一化并执行一批更新，不包含持久化或审批。
//
// 1. 过滤 nil 值更新，为 nil 值生成错误结果
// 2. 归一化后逐一应用到对应 operator
// 3. operator 不存在时生成完整 ApplyResult（含 ChangeType 和 Metadata）
//
// 对应 Python: openjiuwen/agent_evolving/update_execution.py execute_updates
func ExecuteUpdates(
	operators map[string]operator.Operator,
	updates map[schema.UpdateKey]any,
) []schema.ApplyResult {
	var results []schema.ApplyResult

	// 1. 过滤 nil 值更新
	nonNilUpdates := make(map[schema.UpdateKey]any)
	for key, value := range updates {
		if value != nil {
			nonNilUpdates[key] = value
		}
	}

	// 2. 归一化后逐一应用
	normalized := schema.NormalizeUpdates(nonNilUpdates)
	for key, update := range normalized {
		op, ok := operators[key.OperatorID()]
		if !ok {
			results = append(results, schema.ApplyResult{
				OperatorID: key.OperatorID(),
				Target:     key.Target(),
				Applied:    false,
				Mode:       update.Mode,
				Effect:     update.Effect,
				Value:      update.Payload,
				ChangeType: update.ChangeType,
				Records:    []any{},
				Errors:     []string{fmt.Sprintf("operator not found: %s", key.OperatorID())},
				Metadata:   schema.MetadataClone(update.Metadata),
			})
			continue
		}
		results = append(results, op.ApplyUpdate(key.Target(), update))
	}

	// 3. 为 nil 值更新生成错误结果
	for key, value := range updates {
		if value == nil {
			results = append(results, schema.ApplyResult{
				OperatorID: key.OperatorID(),
				Target:     key.Target(),
				Applied:    false,
				Records:    []any{},
				Errors:     []string{"update value is nil"},
			})
		}
	}

	return results
}

// ApplyUpdates ExecuteUpdates 的兼容别名。
//
// 对应 Python: openjiuwen/agent_evolving/update_execution.py apply_updates
func ApplyUpdates(
	operators map[string]operator.Operator,
	updates map[schema.UpdateKey]any,
) []schema.ApplyResult {
	return ExecuteUpdates(operators, updates)
}

// SummarizeApplyResults 返回更新执行的聚合统计。
//
// 对应 Python: openjiuwen/agent_evolving/update_execution.py summarize_apply_results
func SummarizeApplyResults(results []schema.ApplyResult) map[string]int {
	applied := 0
	for _, r := range results {
		if r.Applied {
			applied++
		}
	}
	return map[string]int{
		"total":   len(results),
		"applied": applied,
		"failed":  len(results) - applied,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

注意：需确认 `schema.NormalizeUpdates` 函数签名和 `schema.UpdateKey.OperatorID()` / `schema.UpdateKey.Target()` 方法是否存在。

- [ ] **Step 2: 创建 update_execution_test.go**

```go
package evolving

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeOperator 用于测试的模拟 Operator
type fakeOperator struct {
	operatorID string
}

func (f *fakeOperator) OperatorID() string                          { return f.operatorID }
func (f *fakeOperator) GetTunables() map[string]operator.TunableSpec { return nil }
func (f *fakeOperator) GetState() map[string]any                    { return nil }
func (f *fakeOperator) SetParameter(target string, value any)       {}
func (f *fakeOperator) ApplyUpdate(target string, update schema.UpdateValue) schema.ApplyResult {
	return schema.ApplyResult{
		OperatorID: f.operatorID,
		Target:     target,
		Applied:    true,
		Mode:       update.Mode,
		Effect:     update.Effect,
		Value:      update.Payload,
		ChangeType: update.ChangeType,
		Records:    []any{},
		Errors:     []string{},
		Metadata:   schema.MetadataClone(update.Metadata),
	}
}
func (f *fakeOperator) LoadState(state map[string]any) {}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestExecuteUpdates_正常应用(t *testing.T) {
	operators := map[string]operator.Operator{
		"llm_call": &fakeOperator{operatorID: "llm_call"},
	}
	updates := map[schema.UpdateKey]any{
		schema.NewUpdateKey("llm_call", "system_prompt"): "new prompt",
	}
	results := ExecuteUpdates(operators, updates)
	assert.Len(t, results, 1)
	assert.True(t, results[0].Applied)
}

func TestExecuteUpdates_operator不存在(t *testing.T) {
	operators := map[string]operator.Operator{}
	updates := map[schema.UpdateKey]any{
		schema.NewUpdateKey("missing_op", "target"): "value",
	}
	results := ExecuteUpdates(operators, updates)
	assert.Len(t, results, 1)
	assert.False(t, results[0].Applied)
	assert.Contains(t, results[0].Errors[0], "operator not found")
}

func TestExecuteUpdates_nil值过滤(t *testing.T) {
	operators := map[string]operator.Operator{
		"llm_call": &fakeOperator{operatorID: "llm_call"},
	}
	updates := map[schema.UpdateKey]any{
		schema.NewUpdateKey("llm_call", "target1"): "value",
		schema.NewUpdateKey("llm_call", "target2"): nil,
	}
	results := ExecuteUpdates(operators, updates)
	assert.Len(t, results, 2)
	// nil 值应有错误结果
	var nilResult *schema.ApplyResult
	for i := range results {
		if results[i].Target == "target2" {
			nilResult = &results[i]
		}
	}
	assert.NotNil(t, nilResult)
	assert.False(t, nilResult.Applied)
	assert.Contains(t, nilResult.Errors[0], "nil")
}

func TestSummarizeApplyResults(t *testing.T) {
	results := []schema.ApplyResult{
		{Applied: true},
		{Applied: false},
		{Applied: true},
	}
	summary := SummarizeApplyResults(results)
	assert.Equal(t, 3, summary["total"])
	assert.Equal(t, 2, summary["applied"])
	assert.Equal(t, 1, summary["failed"])
}
```

注意：需确认 `schema.NewUpdateKey` 构造函数是否存在，如不存在需改用其他方式构造 UpdateKey。

- [ ] **Step 3: 更新 doc.go**

在 `internal/evolving/doc.go` 的文件目录中新增条目：

``//	├── update_execution.go  # 更新执行函数（ExecuteUpdates/ApplyUpdates/SummarizeApplyResults）``

- [ ] **Step 4: 运行测试**

```bash
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; sleep 1
go test ./internal/evolving/... -v -count=1 -run "TestExecuteUpdates|TestSummarizeApplyResults"
```

Expected: PASS（可能需要根据 `NormalizeUpdates` 和 `UpdateKey` 的实际签名微调）

- [ ] **Step 5: Commit**

```bash
git add internal/evolving/update_execution.go internal/evolving/update_execution_test.go internal/evolving/doc.go
git commit -m "feat(S05): 新建 update_execution.go，实现 ExecuteUpdates/ApplyUpdates/SummarizeApplyResults"
```

---

### Task 6: G04 — ToolCallOperator descriptions 改为 map[string]any，删除 toString

**Files:**
- Modify: `internal/agentcore/operator/tool_call/tool_call_operator.go`
- Modify: `internal/agentcore/operator/tool_call/tool_call_operator_test.go`

- [ ] **Step 1: 修改 descriptions 字段类型和相关函数**

在 `internal/agentcore/operator/tool_call/tool_call_operator.go` 中：

(a) 第 24 行，descriptions 类型改为：
```go
// descriptions 工具描述字典 map[tool_name]description
descriptions map[string]any
```

(b) 第 52 行，NewToolCallOperator 初始化：
```go
descriptions: make(map[string]any),
```

(c) 第 96-107 行，SetParameter 简化：
```go
func (op *ToolCallOperator) SetParameter(target string, value any) {
	if target != TargetToolDescription {
		return
	}
	descMap, ok := value.(map[string]any)
	if !ok {
		return
	}
	op.descriptions = cloneAnyMap(descMap)
	if op.onParameterUpdated != nil {
		op.onParameterUpdated(target, cloneAnyMap(op.descriptions))
	}
}
```

(d) 第 121 行，GetState：
```go
func (op *ToolCallOperator) GetState() map[string]any {
	return map[string]any{
		TargetToolDescription: cloneAnyMap(op.descriptions),
	}
}
```

(e) 第 129-147 行，LoadState 简化：
```go
func (op *ToolCallOperator) LoadState(state map[string]any) {
	if td, ok := state[TargetToolDescription]; ok {
		if descMap, ok := td.(map[string]any); ok {
			op.descriptions = cloneAnyMap(descMap)
			if op.onParameterUpdated != nil {
				op.onParameterUpdated(TargetToolDescription, cloneAnyMap(op.descriptions))
			}
		}
	}
}
```

(f) 第 158-163 行，WithDescriptions 选项：
```go
func WithDescriptions(descriptions map[string]any) ToolCallOperatorOption {
	return func(op *ToolCallOperator) {
		if descriptions != nil {
			op.descriptions = cloneAnyMap(descriptions)
		}
	}
}
```

(g) 替换 `cloneMap` 为 `cloneAnyMap`，删除 `toString`：

```go
// cloneAnyMap 克隆 map[string]any。
func cloneAnyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}
```

删除 `toString` 函数（第 186-191 行）。

- [ ] **Step 2: 修改测试文件**

所有测试中 `map[string]string` 改为 `map[string]any`，`WithDescriptions` 参数类型同步修改。

- [ ] **Step 3: 运行测试**

```bash
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; sleep 1
go test ./internal/agentcore/operator/tool_call/... -v -count=1
```

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/operator/tool_call/
git commit -m "fix(G04): ToolCallOperator descriptions 改为 map[string]any，删除 toString"
```

---

## 第三批：服务层

### Task 7: S06 — switchMode 补充 team 条件 + 实现 session 持久化

**Files:**
- Modify: `internal/swarm/server/handle_envelope.go:296-299,354-356`
- Modify: `internal/swarm/server/runtime/uapclaw.go:440-442`

- [ ] **Step 1: 实现 UapClaw.SwitchMode**

在 `internal/swarm/server/runtime/uapclaw.go` 中，将第 440-442 行改为：

```go
// SwitchMode 切换运行模式，执行完整的 session 生命周期。
// 流程：preRun → switchMode → loadState → updateState → postRun
//
// 对应 Python: jiuwenswarm/server/agent_ws_server.py:1145-1154
func (uc *UapClaw) SwitchMode(ctx context.Context, sessionID, subMode string) error {
	if uc.deepAdapter == nil {
		return nil
	}
	agent := uc.deepAdapter.GetDeepAgent()
	if agent == nil {
		return nil
	}
	card := agent.GetCard()
	if card == nil {
		return nil
	}

	// 创建 session 并执行生命周期
	sess := session.CreateAgentSession(sessionID, card, nil)
	if err := sess.PreRun(ctx); err != nil {
		return fmt.Errorf("SwitchMode PreRun 失败: %w", err)
	}

	agent.SwitchMode(sess, subMode)
	state := agent.LoadState(sess)

	sess.UpdateState(map[string]any{
		hschema.SessionStateKey: state.ToSessionDict(),
	})

	if err := sess.PostRun(ctx); err != nil {
		return fmt.Errorf("SwitchMode PostRun 失败: %w", err)
	}

	return nil
}
```

需要添加 import：
- `"context"`
- `"fmt"`
- `"github.com/uapclaw/uapclaw-go/internal/agentcore/session"`
- `hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"`

注意：需要确认 `uc.deepAdapter` 是否存在、`GetDeepAgent()` 方法是否存在、`agent.GetCard()` 方法是否存在。根据代码结构，UapClaw 应持有 DeepAdapter 或 DeepAgent 实例。

- [ ] **Step 2: 修改 handle_envelope.go 两处 switchMode 调用**

第一处（第 296-299 行，非流式路径）：

```go
// 原：
// if mode == "code" {
//     _ = agent.SwitchMode(mode, subMode)
// }
// 改：
if mode == "code" && subMode != "team" {
    _ = agent.SwitchMode(ctx, requestSessionID(request), subMode)
}
```

第二处（第 354-356 行，流式路径）：

```go
// 原：
// if mode == "code" {
//     _ = agent.SwitchMode(mode, subMode)
// }
// 改：
if mode == "code" && subMode != "team" {
    _ = agent.SwitchMode(ctx, requestSessionID(request), subMode)
}
```

需确认 `requestSessionID` 辅助函数是否存在，如不存在需要新增：

```go
func requestSessionID(request *schema.AgentRequest) string {
	if request.SessionID != nil {
		return *request.SessionID
	}
	return ""
}
```

- [ ] **Step 3: 运行测试**

```bash
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; sleep 1
go build ./internal/swarm/server/...
```

Expected: 编译通过

- [ ] **Step 4: Commit**

```bash
git add internal/swarm/server/handle_envelope.go internal/swarm/server/runtime/uapclaw.go
git commit -m "fix(S06): switchMode 补充 subMode!=team 条件 + 实现 session 持久化"
```

---

### Task 8: G03 — ACP capabilities 注入补充 INITIALIZE 排除和 setdefault 语义

**Files:**
- Modify: `internal/swarm/server/handle_envelope.go:59-61,726-736`

- [ ] **Step 1: 修改调用点，增加 INITIALIZE 排除**

第 59-61 行改为：

```go
// 2. ACP channel 特殊处理：注入 client_capabilities 到 metadata（排除 INITIALIZE 方法）
if request.ChannelID == acpChannelID && request.ReqMethod != schema.ReqMethodInitialize {
    s.injectACPCapabilities(request, envelope)
}
```

需确认 `schema.ReqMethodInitialize` 常量是否存在。

- [ ] **Step 2: 修改 injectACPCapabilities 为 setdefault 语义**

第 726-736 行改为：

```go
// injectACPCapabilities 为 ACP 通道注入 client_capabilities 到 metadata。
// 使用 setdefault 语义：不覆盖已有 client_capabilities 值。
//
// 对应 Python: jiuwenswarm/server/agent_ws_server.py:803-810
// ⤵️ 10.3.12: 补充 agent_manager.get_client_capabilities("acp") fallback
func (s *AgentServer) injectACPCapabilities(request *schema.AgentRequest, envelope *e2a.E2AEnvelope) {
	if request.Metadata == nil {
		request.Metadata = make(map[string]any)
	}
	// setdefault 语义：已有 client_capabilities 则不覆盖
	if _, exists := request.Metadata["client_capabilities"]; exists {
		return
	}
	if envelope.Params != nil {
		if caps, ok := envelope.Params["client_capabilities"]; ok {
			request.Metadata["client_capabilities"] = caps
			return
		}
	}
	// ⤵️ 10.3.12: fallback 到 agent_manager.get_client_capabilities("acp")
	// 待 AgentManager 完整实现后补充
}
```

- [ ] **Step 3: 运行测试**

```bash
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; sleep 1
go test ./internal/swarm/server/... -v -count=1
```

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/swarm/server/handle_envelope.go
git commit -m "fix(G03): ACP capabilities 注入补充 INITIALIZE 排除和 setdefault 语义"
```

---

### Task 9: G06 — session.create 返回字段名 sessionId → session_id

**Files:**
- Modify: `internal/swarm/server/handle_session.go:404`

- [ ] **Step 1: 修改字段名**

第 404 行：

```go
// 原：
// schema.WithPayload(map[string]any{"sessionId": sessionID}),
// 改：
schema.WithPayload(map[string]any{"session_id": sessionID}),
```

- [ ] **Step 2: 运行测试**

```bash
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; sleep 1
go test ./internal/swarm/server/... -v -count=1
```

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/swarm/server/handle_session.go
git commit -m "fix(G06): session.create 返回字段名 sessionId 改为 session_id"
```

---

### Task 10: G07 — cancel 路径改为先纯读取 resolve，fallback 时才调 applyResolvedModeToRequest

**Files:**
- Modify: `internal/swarm/server/handle_envelope.go:430,560-646`

- [ ] **Step 1: 新增纯读取的 resolveMode 函数**

在 `handle_envelope.go` 的非导出函数区新增：

```go
// resolveMode 从 request 中纯读取并解析 mode/subMode，不修改 request。
// 对应 Python: resolve_agent_request_mode(mode_param)
func resolveMode(request *schema.AgentRequest) (mode, subMode string) {
	if request.Params == nil {
		return "agent", "plan"
	}
	var params map[string]any
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return "agent", "plan"
	}
	modeText := ""
	if modeVal, ok := params["mode"]; ok {
		if s, ok := modeVal.(string); ok {
			modeText = strings.TrimSpace(strings.ToLower(s))
		}
	}
	if modeText == "" {
		modeText = "agent.plan"
	}
	parts := strings.SplitN(modeText, ".", 2)
	mode = parts[0]
	if mode == "" {
		mode = "agent"
	}
	defaultSubModes := map[string]string{
		"agent": "plan",
		"code":  "normal",
	}
	if len(parts) > 1 && parts[1] != "" {
		subMode = parts[1]
	} else {
		if def, ok := defaultSubModes[mode]; ok {
			subMode = def
		}
	}
	// team 分支
	if mode == "team" {
		if subMode == "plan" {
			mode = "code"
			subMode = "team"
		} else {
			subMode = ""
		}
	}
	// code subMode 白名单
	if mode == "code" && subMode != "plan" && subMode != "normal" && subMode != "team" {
		subMode = "normal"
	}
	return
}
```

- [ ] **Step 2: 修改 handleCancel**

第 430 行：

```go
// 原：
// mode, subMode := applyResolvedModeToRequest(request)
// 改为三级模式：

// 第1级：纯读取 mode（不修改 request）
mode, subMode := resolveMode(request)
projectDir := resolveRequestProjectDir(request)

var agent *runtime.UapClaw

// 第1级：按 mode 查找已有 agent
if mode != "" {
    agent = s.agentManager.GetAgentNoWait(request.ChannelID, mode, projectDir, subMode)
}

// 第2级：按 projectDir 查找任何已有 agent（不限 mode）
if agent == nil {
    agent = s.agentManager.GetAgentNoWait(request.ChannelID, "", projectDir, "")
}

// 第3级：fallback 创建（此时才调 applyResolvedModeToRequest 回写 mode）
if agent == nil {
    mode, subMode = applyResolvedModeToRequest(request)
    var err error
    agent, err = s.agentManager.GetAgent(request.ChannelID, mode, projectDir, subMode)
    if err != nil {
        // ... 错误日志 + writeErrorResponse（保持原逻辑）
        return
    }
}
```

- [ ] **Step 3: 运行测试**

```bash
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; sleep 1
go test ./internal/swarm/server/... -v -count=1
```

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/swarm/server/handle_envelope.go
git commit -m "fix(G07): cancel 路径改为先纯读取 resolve，fallback 时才调 applyResolvedModeToRequest"
```

---

## 最终验证

- [ ] **Step 1: 全量编译检查**

```bash
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; sleep 1
go build ./...
```

Expected: 编译通过，无错误

- [ ] **Step 2: 全量测试**

```bash
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; sleep 1
go test -cover ./...
```

Expected: 所有测试通过，覆盖率 ≥ 85%

- [ ] **Step 3: 最终 commit（如有格式修正）**

```bash
git add -A
git commit -m "chore: 审查文档 2026-07-18 修复收尾"
```
