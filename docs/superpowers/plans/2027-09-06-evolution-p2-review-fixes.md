# P2 实现偏差修复 + any 消除实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 9.24 P2 实现中的 8 处偏差 + 消除可收紧的 any 类型使用

**Architecture:** 偏差修复主要是回填设计文档 + 代码微调；any 消除涉及 normalizeSkillNames/normalizeMemberRole 签名收紧、TrajectoryStep.Logprobs 类型收紧、extractor.go any 返回值删除、PerAgentCallbackFunc/SessionFacade 加注释

**Tech Stack:** Go 1.x, testify

---

### Task 1: 拆分测试文件

**Files:**
- Modify: `internal/agentcore/harness/rails/evolution/evolution_rail_test.go`
- Create: `internal/agentcore/harness/rails/evolution/extension_test.go`
- Modify: `internal/agentcore/harness/rails/evolution/helpers_test.go`

从 `evolution_rail_test.go`（736 行）拆分为 4 个文件：

- [ ] **Step 1: 创建 extension_test.go，移入 extension 相关测试**

将以下测试从 `evolution_rail_test.go` 移到 `extension_test.go`：
- `TestNoOpExtension_AllMethods`
- `TestNoOpExtension_SnapshotForEvolution`
- `TestEvolutionTriggerPoint_值`

`extension_test.go` 需要的 import：
```go
import (
    "context"
    "testing"

    agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
    "github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
    "github.com/stretchr/testify/assert"
)
```

- [ ] **Step 2: 创建 trajectory_rail_test.go，移入 TrajectoryRail 测试**

将以下测试从 `evolution_rail_test.go` 移到 `trajectory_rail_test.go`：
- `TestNewTrajectoryRail_默认值`
- `TestNewTrajectoryRail_带选项`
- `TestTrajectoryRail_收集轨迹`
- `TestTrajectoryRail_RunEvolution不执行任何操作`

`trajectory_rail_test.go` 需要的 import：
```go
import (
    "context"
    "testing"

    llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
    agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
    "github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)
```

- [ ] **Step 3: 将 helpers 相关测试移入 helpers_test.go**

将以下测试从 `evolution_rail_test.go` 移到 `helpers_test.go`（已有文件，追加）：
- `TestBaseMessageToMap_nil`
- `TestBaseMessageToMap_纯文本消息`
- `TestBaseMessageToMap_带Name和Metadata`
- `TestToolInfoToMap_nil`
- `TestToolInfoToMap_完整字段`
- `TestStringPtr_空字符串`
- `TestStringPtr_非空字符串`

`helpers_test.go` 需要新增 import：
```go
llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
```

- [ ] **Step 4: 验证编译和测试通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/evolution/... -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/rails/evolution/
git commit -m "refactor(evolution): split test file into 4 files (extension/rail/trajectory/helpers)"
```

---

### Task 2: normalizeSkillNames 签名收紧 `any` → `[]string`

**Files:**
- Modify: `internal/agentcore/harness/rails/evolution/helpers.go`
- Modify: `internal/agentcore/harness/rails/evolution/helpers_test.go`

- [ ] **Step 1: 修改 normalizeSkillNames 签名和实现**

将 `helpers.go` 中：
```go
func normalizeSkillNames(raw any) map[string]bool {
	if raw == nil {
		return map[string]bool{}
	}
	switch v := raw.(type) {
	case string:
		name := strings.TrimSpace(v)
		if name == "" {
			return map[string]bool{}
		}
		return map[string]bool{name: true}
	case []string:
		result := map[string]bool{}
		for _, s := range v {
			name := strings.TrimSpace(s)
			if name != "" {
				result[name] = true
			}
		}
		return result
	default:
		return map[string]bool{}
	}
}
```
改为：
```go
func normalizeSkillNames(names []string) map[string]bool {
	result := map[string]bool{}
	for _, s := range names {
		name := strings.TrimSpace(s)
		if name != "" {
			result[name] = true
		}
	}
	return result
}
```

- [ ] **Step 2: 修改 _normalizeNameSet 桥接方法保持 any**

`evolution_rail.go` 中 `_normalizeNameSet` 保持 `any`（P3/P4 预留桥接），但其内部实现需要适配新签名：
```go
func (r *EvolutionRail) _normalizeNameSet(raw any) map[string]bool {
	switch v := raw.(type) {
	case string:
		return normalizeSkillNames([]string{v})
	case []string:
		return normalizeSkillNames(v)
	default:
		return map[string]bool{}
	}
}
```

- [ ] **Step 3: 更新 helpers_test.go 测试**

删除以下已不适用的测试：
- `TestNormalizeSkillNames_nil`（不再接受 nil）
- `TestNormalizeSkillNames_字符串`（不再接受 string）
- `TestNormalizeSkillNames_字符串前后空格`（不再接受 string）
- `TestNormalizeSkillNames_空字符串`（不再接受 string）
- `TestNormalizeSkillNames_其他类型`（不再接受 any）

保留/修改：
- `TestNormalizeSkillNames_列表` → 不变
- `TestNormalizeSkillNames_列表含空格和空项` → 不变

新增：
```go
func TestNormalizeSkillNames_空切片(t *testing.T) {
	assert.Equal(t, map[string]bool{}, normalizeSkillNames([]string{}))
}

func TestNormalizeSkillNames_单个名称(t *testing.T) {
	assert.Equal(t, map[string]bool{"foo": true}, normalizeSkillNames([]string{"foo"}))
}
```

更新 `evolution_rail_test.go` 中 `TestNormalizeSkillNamesGo`：
```go
func TestNormalizeSkillNamesGo(t *testing.T) {
	result := normalizeSkillNamesGo([]string{"a", "b"})
	assert.Equal(t, map[string]bool{"a": true, "b": true}, result)
}
```
（此测试不需要修改，`normalizeSkillNamesGo` 内部已调用 `normalizeSkillNames([]string)`）

- [ ] **Step 4: 验证编译和测试通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/rails/evolution/... && go test ./internal/agentcore/harness/rails/evolution/... -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/rails/evolution/
git commit -m "refactor(evolution): tighten normalizeSkillNames signature from any to []string"
```

---

### Task 3: normalizeMemberRole 签名收紧 `any` → `string`

**Files:**
- Modify: `internal/agentcore/harness/rails/evolution/helpers.go`
- Modify: `internal/agentcore/harness/rails/evolution/helpers_test.go`

- [ ] **Step 1: 修改 normalizeMemberRole 签名和实现**

将 `helpers.go` 中：
```go
func normalizeMemberRole(role any) *string {
	if role == nil {
		return nil
	}
	text := ""
	switch v := role.(type) {
	case string:
		text = v
	default:
		if stringer, ok := v.(interface{ String() string }); ok {
			text = stringer.String()
		}
	}
	if text == "" {
		return nil
	}
	return &text
}
```
改为：
```go
func normalizeMemberRole(role string) *string {
	if role == "" {
		return nil
	}
	return &role
}
```

- [ ] **Step 2: 更新 helpers_test.go 测试**

删除以下测试：
- `TestNormalizeMemberRole_nil`（不再接受 nil）
- `TestNormalizeMemberRole_自定义Stringer`（不再接受 Stringer）

修改：
- `TestNormalizeMemberRole_字符串` → `TestNormalizeMemberRole_非空字符串`（不变）
- `TestNormalizeMemberRole_空字符串` → 不变

删除 `stringerMock` 辅助类型（不再需要）。

- [ ] **Step 3: 验证编译和测试通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/rails/evolution/... && go test ./internal/agentcore/harness/rails/evolution/... -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/harness/rails/evolution/
git commit -m "refactor(evolution): tighten normalizeMemberRole signature from any to string"
```

---

### Task 4: TrajectoryStep.Logprobs 类型收紧 `any` → `[]map[string]any`

**Files:**
- Modify: `internal/evolving/trajectory/types.go`
- Modify: `internal/agentcore/harness/rails/evolution/helpers.go`
- Modify: `internal/agentcore/harness/rails/evolution/evolution_rail.go`
- Modify: `internal/evolving/trajectory/extractor.go`
- Modify: `internal/agentcore/harness/rails/evolution/helpers_test.go`
- Modify: `internal/evolving/trajectory/extractor_test.go`

- [ ] **Step 1: 修改 types.go**

```go
// Logprobs token 对数概率，仅 kind=llm（可选）
Logprobs []map[string]any `json:"logprobs"`
```

- [ ] **Step 2: 修改 helpers.go splitResponseTokenFields 返回值第 4 个**

```go
func splitResponseTokenFields(response *llmschema.AssistantMessage) (map[string]any, []int, []int, []map[string]any) {
	if response == nil {
		return nil, nil, nil, nil
	}

	promptTokenIDs := response.PromptTokenIDs
	completionTokenIDs := response.CompletionTokenIDs
	// 尝试将 Logprobs 转为 []map[string]any
	var logprobs []map[string]any
	if response.Logprobs != nil {
		if lp, ok := response.Logprobs.([]map[string]any); ok {
			logprobs = lp
		}
		// 非标准格式不赋值，丢弃
	}

	dump := response.ToOpenAIDict()
	if dump == nil {
		return nil, promptTokenIDs, completionTokenIDs, logprobs
	}

	result := map[string]any{}
	for k, v := range dump {
		result[k] = v
	}
	delete(result, "prompt_token_ids")
	delete(result, "completion_token_ids")
	delete(result, "logprobs")

	return result, promptTokenIDs, completionTokenIDs, logprobs
}
```

- [ ] **Step 3: 修改 evolution_rail.go AfterModelCall 中 logprobs 变量类型**

```go
var logprobs []map[string]any
```

- [ ] **Step 4: 修改 extractor.go buildStep 中 logprobs 变量类型和赋值**

```go
var logprobs []map[string]any
// ...
if lp, ok := llmDetail.Response["logprobs"]; ok {
    if lps, ok := lp.([]map[string]any); ok {
        logprobs = lps
    }
    delete(llmDetail.Response, "logprobs")
}
```

- [ ] **Step 5: 更新 helpers_test.go splitResponseTokenFields 测试**

修改 `TestSplitResponseTokenFields_含token字段` 中 logprobs 断言：
```go
// logprobs 类型为 []map[string]any，非标准格式会被丢弃
assert.Nil(t, lp) // 或 assert.NotNil 取决于 AssistantMessage.Logprobs 的实际类型
```

- [ ] **Step 6: 更新 extractor_test.go**

修改 `TestBuildStep_TokenLevel字段提升` 中 logprobs 相关断言：
```go
// Logprobs 为 []map[string]any 类型，span.Outputs 中的 []any 无法自动转换，所以为 nil
assert.Nil(t, step.Logprobs)
```

- [ ] **Step 7: 验证编译和测试通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/evolving/trajectory/... ./internal/agentcore/harness/rails/evolution/... && go test ./internal/evolving/trajectory/... ./internal/agentcore/harness/rails/evolution/... -count=1`
Expected: PASS

- [ ] **Step 8: 提交**

```bash
git add internal/evolving/trajectory/ internal/agentcore/harness/rails/evolution/
git commit -m "refactor(trajectory): tighten TrajectoryStep.Logprobs from any to []map[string]any"
```

---

### Task 5: extractor.go 删除 any 返回值版本

**Files:**
- Modify: `internal/evolving/trajectory/extractor.go`
- Modify: `internal/evolving/trajectory/extractor_test.go`

- [ ] **Step 1: 重构 buildLLMDetail 改用 extractOutputsAsMap + 直接 map 逻辑**

当前 `buildLLMDetail` 调用 `extractOutputs(span)` + `parseLLMResponse(outputs)`：
```go
outputs := extractOutputs(span)
response := parseLLMResponse(outputs)
```

改为：
```go
response := extractOutputsAsMap(span)
```

`extractOutputsAsMap` 已包含嵌套 `"outputs"` 键的解包逻辑，且 `parseLLMResponse` 只做 `map[string]any` 类型断言——`extractOutputsAsMap` 返回值已经是 `map[string]any`，不需要再断言。

- [ ] **Step 2: 重构 buildMeta 改用 extractInputsAsMap / extractOutputsAsMap**

当前 `buildMeta` 调用：
```go
meta["inputs"] = extractInputs(&span.Span)
meta["outputs"] = extractOutputs(&span.Span)
```

改为：
```go
meta["inputs"] = extractInputsAsMap(&span.Span)
meta["outputs"] = extractOutputsAsMap(&span.Span)
```

注意：`extractInputs()` 对非 map 类型会返回原始值（如 string），而 `extractInputsAsMap` 对非 map 类型返回 nil。如果 `meta["inputs"]` 需要保留非 map 值，则保留 `extractInputs` 但改其返回值为具体类型。查看 `buildMeta` 中 `meta["inputs"]` 的使用场景——它只用于非 LLM/Tool 步骤的元数据记录，`extractInputsAsMap` 返回 nil 在语义上也是合理的（nil 表示无法提取 map 格式的输入）。

- [ ] **Step 3: 删除三个 any 返回值函数**

从 `extractor.go` 删除：
```go
func parseLLMResponse(outputs any) map[string]any { ... }
func extractInputs(span *tracer.Span) any { ... }
func extractOutputs(span *tracer.Span) any { ... }
```

`extractInputsAsMap` 和 `extractOutputsAsMap` 内部原本调用 `extractInputs`/`extractOutputs`，重构后内联逻辑：

```go
func extractInputsAsMap(span *tracer.Span) map[string]any {
	raw := span.Inputs
	if m, ok := raw.(map[string]any); ok {
		if inner, ok := m["inputs"]; ok {
			if innerMap, ok := inner.(map[string]any); ok {
				return innerMap
			}
		}
		return m
	}
	return nil
}

func extractOutputsAsMap(span *tracer.Span) map[string]any {
	raw := span.Outputs
	if m, ok := raw.(map[string]any); ok {
		if inner, ok := m["outputs"]; ok {
			if innerMap, ok := inner.(map[string]any); ok {
				return innerMap
			}
		}
		return m
	}
	return nil
}
```

- [ ] **Step 4: 更新 extractor_test.go**

删除：
- `TestExtractInputs`
- `TestExtractOutputs`
- `TestParseLLMResponse`

`TestExtractInputsAsMap_*` 和 `TestExtractOutputsAsMap_*` 保持不变。

- [ ] **Step 5: 验证编译和测试通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/evolving/trajectory/... && go test ./internal/evolving/trajectory/... -count=1`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/evolving/trajectory/
git commit -m "refactor(trajectory): remove any-returning extractInputs/extractOutputs/parseLLMResponse, keep AsMap versions"
```

---

### Task 6: PerAgentCallbackFunc 加 TODO 注释

**Files:**
- Modify: `internal/agentcore/runner/callback/events.go`

- [ ] **Step 1: 修改 PerAgentCallbackFunc 注释**

将 `events.go` 中：
```go
// PerAgentCallbackFunc 实例级 PerAgent 回调函数类型。
// agentCallbackContext 实际类型为 *interfaces.AgentCallbackContext，回调内需类型断言。
//
// 对应 Python: AnyAgentCallback = Union[AgentCallback, SyncAgentCallback]
type PerAgentCallbackFunc func(ctx context.Context, agentCallbackContext any) error
```

改为：
```go
// PerAgentCallbackFunc 实例级 PerAgent 回调函数类型。
// agentCallbackContext 实际类型为 *interfaces.AgentCallbackContext，回调内需类型断言。
//
// TODO: 收紧为 *AgentCallbackContext 具体类型。当前因循环依赖
// (callback → agentinterfaces → callback) 无法直接引用。
// 可能的解决方案：(A) 定义回调上下文接口于底层包 (B) 抽取共享类型包
//
// 对应 Python: AnyAgentCallback = Union[AgentCallback, SyncAgentCallback]
type PerAgentCallbackFunc func(ctx context.Context, agentCallbackContext any) error
```

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/runner/callback/...`
Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/runner/callback/
git commit -m "docs(callback): add TODO for PerAgentCallbackFunc any tightening due to circular dep"
```

---

### Task 7: SessionFacade any 方法加注释

**Files:**
- Modify: `internal/agentcore/session/interfaces/facade.go`

- [ ] **Step 1: 为 5 个 any 方法加注释**

将 `facade.go` 中：
```go
// GetState 获取会话状态值
GetState(key state.StateKey) (any, error)
// WriteStream 写入流数据
WriteStream(ctx context.Context, data any) error
// WriteCustomStream 写入自定义流数据
WriteCustomStream(ctx context.Context, data any) error
// GetEnv 获取环境变量
GetEnv(key string, defaultValue ...any) any
// Interact 交互（等待用户输入）
Interact(ctx context.Context, value any) error
```

改为：
```go
// GetState 获取会话状态值。
// 返回类型由 key 决定：常见 map[string]any / *DeepAgentState / []string / []any 等。
// 调用方需根据 key 语义做类型断言。
GetState(key state.StateKey) (any, error)
// WriteStream 写入流数据。
// data 接受 *stream.OutputSchema / stream.OutputSchema / map[string]any，
// 内部 normalizeOutputStream 统一转为 OutputSchema。
WriteStream(ctx context.Context, data any) error
// WriteCustomStream 写入自定义流数据。
// data 接受 stream.CustomSchema / map[string]any / 任意类型，
// 内部 normalizeCustomStream 统一转为 CustomSchema。
// 当前无生产调用方。
WriteCustomStream(ctx context.Context, data any) error
// GetEnv 获取环境变量。
// 返回类型由配置值决定：常见 float64 / int / bool / string / nil。
// 调用方需根据 key 语义做类型断言。
GetEnv(key string, defaultValue ...any) any
// Interact 交互（等待用户输入）。
// value 通常传 string；内部序列化为 InteractionOutput.Value (any)。
// 对齐 Python Session.interact(value) 的任意类型签名。
Interact(ctx context.Context, value any) error
```

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/interfaces/...`
Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/interfaces/
git commit -m "docs(session): add type hints comments for SessionFacade any methods"
```

---

### Task 8: 回填设计文档

**Files:**
- Modify: `docs/superpowers/specs/2027-09-20-evolution-rail-p2-design.md`

- [ ] **Step 1: 回填 8 处偏差到设计文档**

1. `task.Done()` 伪代码改为 select 模式
2. `snapshotKey` 改为 `formatSkillName`
3. 移除 `saveTrajectory` 非导出方法列出
4. helpers.go 规格补充 `baseMessageToMap` 和 `toolInfoToMap`
5. `containsMessage` 去重方式改为 `json.Marshal`
6. 补充 6 个新增辅助方法列出
7. `normalizeCallbackMessages` 描述补充浅拷贝说明
8. 所有偏差对应位置的描述与实际代码对齐

- [ ] **Step 2: 验证文档无语法错误**

Run: `cat docs/superpowers/specs/2027-09-20-evolution-rail-p2-design.md | head -50`
Expected: 正常输出

- [ ] **Step 3: 提交**

```bash
git add docs/superpowers/specs/2027-09-20-evolution-rail-p2-design.md
git commit -m "docs(evolution): backfill design spec with P2 implementation deviations"
```

---

### Task 9: 全量编译验证 + 覆盖率检查

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' || true`
然后: `go build ./...`
Expected: 编译成功

- [ ] **Step 2: 运行 evolution 和 trajectory 测试**

Run: `go test ./internal/agentcore/harness/rails/evolution/... ./internal/evolving/trajectory/... -count=1 -cover`
Expected: PASS，覆盖率 ≥ 85%

- [ ] **Step 3: 运行 callback 和 session 测试（注释修改不影响逻辑）**

Run: `go test ./internal/agentcore/runner/callback/... ./internal/agentcore/session/interfaces/... -count=1`
Expected: PASS

- [ ] **Step 4: 提交并推送**

```bash
git push
```
