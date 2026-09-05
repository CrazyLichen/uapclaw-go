# 9.24 P2 EvolutionRail 基类 + TrajectoryRail 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 EvolutionRail 基类（4 个 final 回调自动收集轨迹 + 9 个扩展点接口 + 异步演化框架）和 TrajectoryRail 纯收集轨道，以及 extractor.go 中 resourceManager any 类型消除。

**Architecture:** EvolutionRail 嵌入 DeepAgentRail，通过 EvolutionExtension 接口字段实现子类多态分派。TrajectoryRail 嵌入 EvolutionRail 使用 noOpExtension。Functional Options 构造。异步演化通过 BackgroundTask + chan struct{} 信号量实现。

**Tech Stack:** Go 1.22+, 内部 trajectory/signal/background/utils/stream 包

**Design Spec:** `docs/superpowers/specs/2027-09-20-evolution-rail-p2-design.md`

---

### Task 1: helpers.go — 辅助函数

**Files:**
- Create: `internal/agentcore/harness/rails/evolution/helpers.go`
- Test: `internal/agentcore/harness/rails/evolution/helpers_test.go`

- [ ] **Step 1: 创建 helpers_test.go — 编写 splitResponseTokenFields 测试**

```go
package evolution

import (
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/stretchr/testify/assert"
)

func TestSplitResponseTokenFields_nil(t *testing.T) {
	resp, ptids, ctids, lp := splitResponseTokenFields(nil)
	assert.Nil(t, resp)
	assert.Nil(t, ptids)
	assert.Nil(t, ctids)
	assert.Nil(t, lp)
}

func TestSplitResponseTokenFields_无token字段(t *testing.T) {
	orig := &llmschema.AssistantMessage{Content: "hello"}
	resp, ptids, ctids, lp := splitResponseTokenFields(orig)
	assert.Nil(t, resp) // AssistantMessage 无 model_dump，返回原始
	assert.Nil(t, ptids)
	assert.Nil(t, ctids)
	assert.Nil(t, lp)
}
```

- [ ] **Step 2: 创建 helpers.go — 实现 splitResponseTokenFields**

```go
package evolution

import (
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// splitResponseTokenFields 从 LLM 响应中分离 token 级字段。
//
// 返回 (response_for_detail, prompt_token_ids, completion_token_ids, logprobs)。
// 返回的 response_for_detail 已移除这三个字段，避免在轨迹中重复存储。
//
// 对齐 Python: _split_response_token_fields(response)
func splitResponseTokenFields(response *llmschema.AssistantMessage) (map[string]any, []int, []int, any) {
	if response == nil {
		return nil, nil, nil, nil
	}
	// AssistantMessage 没有直接 map 表示，返回 nil map + 原始 token 字段从 response 中提取
	// 实际 token 字段通过 response 的属性访问
	var promptTokenIDs []int
	var completionTokenIDs []int
	var logprobs any

	dump := response.ToMap()
	if dump == nil {
		return nil, nil, nil, nil
	}
	result := map[string]any{}
	for k, v := range dump {
		result[k] = v
	}
	if ptids, ok := result["prompt_token_ids"]; ok {
		if ids, ok := ptids.([]int); ok {
			promptTokenIDs = ids
			delete(result, "prompt_token_ids")
		}
	}
	if ctids, ok := result["completion_token_ids"]; ok {
		if ids, ok := ctids.([]int); ok {
			completionTokenIDs = ids
			delete(result, "completion_token_ids")
		}
	}
	if lp, ok := result["logprobs"]; ok {
		logprobs = lp
		delete(result, "logprobs")
	}
	return result, promptTokenIDs, completionTokenIDs, logprobs
}
```

> **注意:** `AssistantMessage.ToMap()` 是否存在需在实现时确认。如果不存在，需要根据 `AssistantMessage` 的实际字段构建 map。具体实现以编译通过为准。

- [ ] **Step 3: 补充 helpers_test.go — 编写 normalizeSkillNames 测试**

```go
func TestNormalizeSkillNames_nil(t *testing.T) {
	assert.Equal(t, map[string]bool{}, normalizeSkillNames(nil))
}

func TestNormalizeSkillNames_字符串(t *testing.T) {
	assert.Equal(t, map[string]bool{"foo": true}, normalizeSkillNames("foo"))
}

func TestNormalizeSkillNames_字符串前后空格(t *testing.T) {
	assert.Equal(t, map[string]bool{"bar": true}, normalizeSkillNames("  bar  "))
}

func TestNormalizeSkillNames_空字符串(t *testing.T) {
	assert.Equal(t, map[string]bool{}, normalizeSkillNames(""))
}

func TestNormalizeSkillNames_列表(t *testing.T) {
	assert.Equal(t, map[string]bool{"a": true, "b": true}, normalizeSkillNames([]string{"a", "b"}))
}

func TestNormalizeSkillNames_列表含空格和空项(t *testing.T) {
	assert.Equal(t, map[string]bool{"x": true}, normalizeSkillNames([]string{" x ", "", "  "}))
}
```

- [ ] **Step 4: 在 helpers.go 中实现 normalizeSkillNames**

```go
// normalizeSkillNames 将技能名称规范化为集合。
//
// 字符串视为单个技能名；切片视为多个名称。前后空格会被裁剪。
//
// 对齐 Python: _normalize_skill_names(raw)
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

- [ ] **Step 5: 补充 helpers_test.go — 编写 normalizeMemberRole 测试**

```go
func TestNormalizeMemberRole_nil(t *testing.T) {
	assert.Nil(t, normalizeMemberRole(nil))
}

func TestNormalizeMemberRole_字符串(t *testing.T) {
	s := "leader"
	assert.Equal(t, &s, normalizeMemberRole("leader"))
}

func TestNormalizeMemberRole_空字符串(t *testing.T) {
	assert.Nil(t, normalizeMemberRole(""))
}
```

- [ ] **Step 6: 在 helpers.go 中实现 normalizeMemberRole**

```go
// normalizeMemberRole 将成员角色规范化为稳定字符串值。
//
// 对齐 Python: _normalize_member_role(role)
func normalizeMemberRole(role any) *string {
	if role == nil {
		return nil
	}
	// 如果是枚举类型，尝试取 .Value() 或 .String()
	// 使用反射检查 Value() 方法
	text := ""
	switch v := role.(type) {
	case string:
		text = v
	default:
		// 尝试 fmt.Stringer 接口
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

- [ ] **Step 7: 补充 helpers_test.go — 编写 collectMessagesFromTrajectory 测试**

```go
func TestCollectMessagesFromTrajectory_nil(t *testing.T) {
	assert.Equal(t, []map[string]any{}, collectMessagesFromTrajectory(nil))
}

func TestCollectMessagesFromTrajectory_空轨迹(t *testing.T) {
	traj := &trajectory.Trajectory{Steps: []*trajectory.TrajectoryStep{}}
	assert.Equal(t, []map[string]any{}, collectMessagesFromTrajectory(traj))
}

func TestCollectMessagesFromTrajectory_含LLM步骤(t *testing.T) {
	traj := &trajectory.Trajectory{
		Steps: []*trajectory.TrajectoryStep{
			{
				Kind: trajectory.StepKindLLM,
				Detail: &trajectory.LLMCallDetail{
					Messages: []map[string]any{
						{"role": "user", "content": "hi"},
						{"role": "assistant", "content": "hello"},
					},
				},
			},
		},
	}
	msgs := collectMessagesFromTrajectory(traj)
	assert.Len(t, msgs, 2)
	assert.Equal(t, "user", msgs[0]["role"])
	assert.Equal(t, "assistant", msgs[1]["role"])
}
```

- [ ] **Step 8: 在 helpers.go 中实现 collectMessagesFromTrajectory**

```go
// collectMessagesFromTrajectory 从轨迹中提取消息列表。
//
// 调用 ConversationSignalDetector.ConvertTrajectoryToMessages 获取消息列表，
// 然后通过 normalizeCallbackMessages 规范化，最后去重。
//
// 对齐 Python: _collect_messages_from_trajectory(trajectory)
func collectMessagesFromTrajectory(traj *trajectory.Trajectory) []map[string]any {
	if traj == nil {
		return []map[string]any{}
	}
	detector := &signal.ConversationSignalDetector{}
	raw := detector.ConvertTrajectoryToMessages(traj)
	normalized := normalizeCallbackMessages(raw)
	// 去重：对齐 Python deduped 逻辑
	deduped := make([]map[string]any, 0, len(normalized))
	seen := make(map[string]bool)
	for _, msg := range normalized {
		key := fmt.Sprintf("%v", msg)
		if !seen[key] {
			seen[key] = true
			deduped = append(deduped, msg)
		}
	}
	return deduped
}

// normalizeCallbackMessages 将回调可见的消息规范化为 JSON 安全的字典列表。
//
// 对齐 Python: _normalize_callback_messages(messages)
func normalizeCallbackMessages(messages []map[string]any) []map[string]any {
	result := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		result = append(result, msg)
	}
	return result
}
```

- [ ] **Step 9: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/harness/rails/evolution/ -run TestSplit|TestNormalize|TestCollectMessages -v`

- [ ] **Step 10: 提交**

```bash
git add internal/agentcore/harness/rails/evolution/helpers.go internal/agentcore/harness/rails/evolution/helpers_test.go
git commit -m "feat(evolution): 添加 P2 辅助函数 helpers.go"
```

---

### Task 2: extension.go — EvolutionExtension 接口 + noOpExtension

**Files:**
- Create: `internal/agentcore/harness/rails/evolution/extension.go`
- Test: `internal/agentcore/harness/rails/evolution/extension_test.go`

- [ ] **Step 1: 创建 extension_test.go — 编写 noOpExtension 基本测试**

```go
package evolution

import (
	"context"
	"testing"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
	"github.com/stretchr/testify/assert"
)

func TestNoOpExtension_OnBeforeInvoke(t *testing.T) {
	ext := noOpExtension{}
	err := ext.OnBeforeInvoke(context.Background(), nil)
	assert.NoError(t, err)
}

func TestNoOpExtension_OnAfterModelCall(t *testing.T) {
	ext := noOpExtension{}
	err := ext.OnAfterModelCall(context.Background(), nil)
	assert.NoError(t, err)
}

func TestNoOpExtension_OnAfterToolCall(t *testing.T) {
	ext := noOpExtension{}
	err := ext.OnAfterToolCall(context.Background(), nil)
	assert.NoError(t, err)
}

func TestNoOpExtension_OnAfterInvoke(t *testing.T) {
	ext := noOpExtension{}
	err := ext.OnAfterInvoke(context.Background(), nil)
	assert.NoError(t, err)
}

func TestNoOpExtension_OnAfterTaskIteration(t *testing.T) {
	ext := noOpExtension{}
	err := ext.OnAfterTaskIteration(context.Background(), nil)
	assert.NoError(t, err)
}

func TestNoOpExtension_OnAfterEvolutionTriggered(t *testing.T) {
	ext := noOpExtension{}
	err := ext.OnAfterEvolutionTriggered(context.Background(), nil, nil)
	assert.NoError(t, err)
}

func TestNoOpExtension_AllowEvolutionTrigger(t *testing.T) {
	ext := noOpExtension{}
	assert.True(t, ext.AllowEvolutionTrigger(TriggerAfterInvoke, nil))
	assert.True(t, ext.AllowEvolutionTrigger(TriggerNone, nil))
}

func TestNoOpExtension_SnapshotForEvolution(t *testing.T) {
	ext := noOpExtension{}
	traj := &trajectory.Trajectory{SessionID: "test-session"}
	snapshot := ext.SnapshotForEvolution(context.Background(), traj, nil)
	assert.NotNil(t, snapshot)
	assert.Equal(t, traj, snapshot.Trajectory)
}

func TestNoOpExtension_SnapshotForEvolution_nil(t *testing.T) {
	ext := noOpExtension{}
	snapshot := ext.SnapshotForEvolution(context.Background(), nil, nil)
	assert.NotNil(t, snapshot)
	assert.Nil(t, snapshot.Trajectory)
}

func TestNoOpExtension_RunEvolution(t *testing.T) {
	ext := noOpExtension{}
	err := ext.RunEvolution(context.Background(), nil, nil)
	assert.NoError(t, err)
}
```

- [ ] **Step 2: 创建 extension.go — 实现 EvolutionExtension 接口 + noOpExtension**

```go
package evolution

import (
	"context"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// noOpExtension EvolutionExtension 的默认空实现。
// TrajectoryRail 直接使用此实例；子类可嵌入后只覆写需要的方法。
type noOpExtension struct{}

// ──────────────────────────── 接口 ────────────────────────────

// EvolutionExtension 演化轨道扩展点接口。
//
// EvolutionRail 的 4 个 final 回调内部完成轨迹收集后，
// 通过此接口将控制权交给子类实现的自定义逻辑。
// Go 嵌入无虚方法分派，必须通过接口实现多态。
//
// 对齐 Python: EvolutionRail._on_xxx + run_evolution 系列扩展点。
type EvolutionExtension interface {
	// OnBeforeInvoke 在每次 invoke 开始时调用（轨迹 builder 已初始化或复用）。
	// 对齐 Python: _on_before_invoke(ctx)
	OnBeforeInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error

	// OnAfterModelCall 在每次模型调用后调用（LLM 步骤已记录到 builder）。
	// 对齐 Python: _on_after_model_call(ctx)
	OnAfterModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error

	// OnAfterToolCall 在每次工具调用后调用（Tool 步骤已记录到 builder）。
	// 对齐 Python: _on_after_tool_call(ctx)
	OnAfterToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error

	// OnAfterInvoke 在每次 invoke 结束时调用（轨迹已保存，builder 仍可用）。
	// 对齐 Python: _on_after_invoke(ctx)
	OnAfterInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error

	// OnAfterTaskIteration 在每次任务循环迭代后调用。
	// 对齐 Python: _on_after_task_iteration(ctx)
	OnAfterTaskIteration(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error

	// OnAfterEvolutionTriggered 在 after_invoke 演化触发完成后调用。
	// 子类可覆写此方法消费对 AllowEvolutionTrigger 和快照可见的状态。
	// 对齐 Python: _on_after_evolution_triggered(trajectory, ctx)
	OnAfterEvolutionTriggered(ctx context.Context, traj *trajectory.Trajectory, cbc *agentinterfaces.AgentCallbackContext) error

	// AllowEvolutionTrigger 返回当前触发点是否允许启动演化。
	// 对齐 Python: _allow_evolution_trigger(trigger_point, ctx) -> bool
	AllowEvolutionTrigger(trigger EvolutionTriggerPoint, cbc *agentinterfaces.AgentCallbackContext) bool

	// SnapshotForEvolution 同步捕获快照（cbc 仍活跃），供后台演化任务使用。
	// 对齐 Python: _snapshot_for_evolution(trajectory, ctx) -> Optional[dict]
	SnapshotForEvolution(ctx context.Context, traj *trajectory.Trajectory, cbc *agentinterfaces.AgentCallbackContext) *EvolutionSnapshot

	// RunEvolution 执行演化逻辑。
	// 异步模式下 cbc 不可用，数据来自 snapshot。
	// 对齐 Python: run_evolution(trajectory, ctx=None, *, snapshot=None)
	RunEvolution(ctx context.Context, traj *trajectory.Trajectory, snapshot *EvolutionSnapshot) error
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func (noOpExtension) OnBeforeInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	return nil
}

func (noOpExtension) OnAfterModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	return nil
}

func (noOpExtension) OnAfterToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	return nil
}

func (noOpExtension) OnAfterInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	return nil
}

func (noOpExtension) OnAfterTaskIteration(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	return nil
}

func (noOpExtension) OnAfterEvolutionTriggered(ctx context.Context, traj *trajectory.Trajectory, cbc *agentinterfaces.AgentCallbackContext) error {
	return nil
}

func (noOpExtension) AllowEvolutionTrigger(trigger EvolutionTriggerPoint, cbc *agentinterfaces.AgentCallbackContext) bool {
	return true
}

func (noOpExtension) SnapshotForEvolution(ctx context.Context, traj *trajectory.Trajectory, cbc *agentinterfaces.AgentCallbackContext) *EvolutionSnapshot {
	messages := collectMessagesFromTrajectory(traj)
	return &EvolutionSnapshot{Trajectory: traj, Messages: messages}
}

func (noOpExtension) RunEvolution(ctx context.Context, traj *trajectory.Trajectory, snapshot *EvolutionSnapshot) error {
	return nil
}
```

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/harness/rails/evolution/ -run TestNoOp -v`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/harness/rails/evolution/extension.go internal/agentcore/harness/rails/evolution/extension_test.go
git commit -m "feat(evolution): 添加 EvolutionExtension 接口 + noOpExtension 默认实现"
```

---

### Task 3: evolution_rail.go — EvolutionRail 结构体 + 回调逻辑

**Files:**
- Create: `internal/agentcore/harness/rails/evolution/evolution_rail.go`
- Test: `internal/agentcore/harness/rails/evolution/evolution_rail_test.go`

这是最大的 Task。按子步骤拆分：

- [ ] **Step 1: 创建 evolution_rail.go — EvolutionTriggerPoint 枚举 + 结构体定义 + 构造函数 + Options**

EvolutionTriggerPoint 枚举、EvolutionRail 结构体（所有字段）、Functional Options、NewEvolutionRail 构造函数、Priority() 方法、TrajectoryStore()/DisabledSkills()/Builder() 访问器、SetTrajectorySink() 方法。

> 具体字段和签名参见设计文档 Section 2。编译期确认 import 路径正确。

- [ ] **Step 2: 创建 evolution_rail_test.go — 构造函数和 Options 测试**

```go
func TestNewEvolutionRail_默认值(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	assert.Equal(t, 60, rail.Priority())
	assert.NotNil(t, rail.TrajectoryStore())
	assert.Equal(t, TriggerAfterInvoke, rail.evolutionTrigger)
	assert.True(t, rail.asyncEvolution)
	assert.Equal(t, 200, rail.maxTrajectorySteps)
}

func TestNewEvolutionRail_WithOptions(t *testing.T) {
	store := trajectory.NewInMemoryTrajectoryStore()
	rail := NewEvolutionRail(noOpExtension{},
		WithTrajectoryStore(store),
		WithMaxTrajectorySteps(100),
		WithEvolutionTrigger(TriggerAfterModelCall),
		WithAsyncEvolution(false),
		WithMaxConcurrentEvolution(3),
		WithDisabledSkills([]string{"skill_a", "skill_b"}),
	)
	assert.Equal(t, store, rail.TrajectoryStore())
	assert.Equal(t, 100, rail.maxTrajectorySteps)
	assert.Equal(t, TriggerAfterModelCall, rail.evolutionTrigger)
	assert.False(t, rail.asyncEvolution)
	assert.Equal(t, map[string]bool{"skill_a": true, "skill_b": true}, rail.DisabledSkills())
}

func TestNewEvolutionRail_ext不能为nil(t *testing.T) {
	// ext 为 nil 时 GetCallbacks 调用 ext 方法会 panic
	// 构造函数不强制 ext 非 nil（对齐 Python），但使用方应保证
	// 这里只验证正常路径
	rail := NewEvolutionRail(noOpExtension{})
	assert.NotNil(t, rail.ext)
}
```

- [ ] **Step 3: 在 evolution_rail.go 中实现 GetCallbacks**

注册 5 个事件回调：CallbackBeforeInvoke、CallbackAfterModelCall、CallbackAfterToolCall、CallbackAfterInvoke、CallbackAfterTaskIteration。

- [ ] **Step 4: 在 evolution_rail_test.go 中编写 GetCallbacks 测试**

```go
func TestEvolutionRail_GetCallbacks(t *testing.T) {
	rail := NewEvolutionRail(noOpExtension{})
	callbacks := rail.GetCallbacks()
	assert.NotNil(t, callbacks[agentinterfaces.CallbackBeforeInvoke])
	assert.NotNil(t, callbacks[agentinterfaces.CallbackAfterModelCall])
	assert.NotNil(t, callbacks[agentinterfaces.CallbackAfterToolCall])
	assert.NotNil(t, callbacks[agentinterfaces.CallbackAfterInvoke])
	assert.NotNil(t, callbacks[agentinterfaces.CallbackAfterTaskIteration])
}
```

- [ ] **Step 5: 在 evolution_rail.go 中实现 BeforeInvoke 回调**

1. type switch `cbc.Inputs()` → `*InvokeInputs`
2. 调用 `resolveSessionID(cbc, inputs)` 
3. builder 复用或新建
4. 调用 `ext.OnBeforeInvoke(ctx, cbc)`

实现 `resolveSessionID` 非导出方法：从 `cbc.Session().GetSessionID()` 或 `inputs.ConversationID` 获取。

- [ ] **Step 6: 在 evolution_rail_test.go 中编写 BeforeInvoke 测试**

验证 builder 新建和复用逻辑。需构造 `*AgentCallbackContext`（含 mock Session 和 InvokeInputs）。

- [ ] **Step 7: 在 evolution_rail.go 中实现 AfterModelCall 回调**

1. guard: builder == nil → return
2. type switch → `*ModelCallInputs`
3. 构建 `LLMCallDetail` + `splitResponseTokenFields`
4. 构建 `TrajectoryStep` + `builder.RecordStep(step)`
5. 调用 `ext.OnAfterModelCall(ctx, cbc)`
6. 触发检查

- [ ] **Step 8: 在 evolution_rail_test.go 中编写 AfterModelCall 测试**

使用 `mockEvolutionExtension` 记录调用，验证 step 被正确记录到 builder，验证扩展点被调用。

- [ ] **Step 9: 在 evolution_rail.go 中实现 AfterToolCall 回调**

同设计文档 AfterToolCall 流程。

- [ ] **Step 10: 在 evolution_rail_test.go 中编写 AfterToolCall 测试**

- [ ] **Step 11: 在 evolution_rail.go 中实现 AfterTaskIteration 回调**

- [ ] **Step 12: 在 evolution_rail_test.go 中编写 AfterTaskIteration 测试**

- [ ] **Step 13: 在 evolution_rail.go 中实现 AfterInvoke 回调**

buildTrajectory → save → publishSnapshot → ext.OnAfterInvoke → triggerEvolution → ext.OnAfterEvolutionTriggered

实现 `buildTrajectory`、`saveTrajectory`、`publishTrajectorySnapshot`、`resetTrajectoryBuilder` 非导出方法。

- [ ] **Step 14: 在 evolution_rail_test.go 中编写 AfterInvoke 测试**

验证轨迹保存、snapshot 发布、扩展点调用。

- [ ] **Step 15: 在 evolution_rail.go 中实现 triggerEvolution + safeRunEvolution**

异步模式：`ext.SnapshotForEvolution` → `utils.CreateBackgroundTask` → `safeRunEvolution`
同步模式：直接调用 `ext.RunEvolution`

实现 `safeRunEvolution`：信号量控制 + 异常捕获 + `emitBackgroundOutcomeEvent`。

- [ ] **Step 16: 在 evolution_rail_test.go 中编写 triggerEvolution 测试**

验证异步和同步两种模式。验证信号量限制。

- [ ] **Step 17: 在 evolution_rail.go 中实现 EmitHostEvent / DrainPendingHostEvents / DrainPendingApprovalEvents / collectPendingHostEvents / emitBackgroundOutcomeEvent / CleanupBackgroundTasks**

对齐 Python 中对应的公开方法和内部方法。

- [ ] **Step 18: 在 evolution_rail_test.go 中编写 DrainPendingHostEvents 和 EmitHostEvent 测试**

- [ ] **Step 19: 运行完整测试套件**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/harness/rails/evolution/ -v`

- [ ] **Step 20: 提交**

```bash
git add internal/agentcore/harness/rails/evolution/evolution_rail.go internal/agentcore/harness/rails/evolution/evolution_rail_test.go
git commit -m "feat(evolution): 实现 EvolutionRail 基类 — 4个final回调 + 9个扩展点 + 异步演化"
```

---

### Task 4: trajectory_rail.go — TrajectoryRail

**Files:**
- Create: `internal/agentcore/harness/rails/evolution/trajectory_rail.go`
- Test: `internal/agentcore/harness/rails/evolution/trajectory_rail_test.go`

- [ ] **Step 1: 创建 trajectory_rail_test.go — 编写 TrajectoryRail 测试**

```go
package evolution

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

func TestNewTrajectoryRail(t *testing.T) {
	rail := NewTrajectoryRail()
	assert.NotNil(t, rail)
	assert.NotNil(t, rail.EvolutionRail)
	assert.Equal(t, 10, rail.Priority())
}

func TestNewTrajectoryRail_WithOptions(t *testing.T) {
	store := trajectory.NewInMemoryTrajectoryStore()
	rail := NewTrajectoryRail(
		WithTrajectoryStore(store),
		WithMaxTrajectorySteps(50),
	)
	assert.Equal(t, store, rail.TrajectoryStore())
	assert.Equal(t, 50, rail.maxTrajectorySteps)
}

func TestTrajectoryRail_完整流程(t *testing.T) {
	rail := NewTrajectoryRail()
	// 构造 BeforeInvoke → AfterModelCall → AfterToolCall → AfterInvoke 完整流程
	// 验证轨迹被正确收集到 TrajectoryStore 中
	// （需要构造 mock AgentCallbackContext）
}
```

- [ ] **Step 2: 创建 trajectory_rail.go — 实现 TrajectoryRail**

```go
package evolution

// ──────────────────────────── 结构体 ────────────────────────────

// TrajectoryRail 纯轨迹收集轨道，不触发任何演化逻辑。
//
// 嵌入 EvolutionRail，使用 noOpExtension 作为扩展点实现。
// RunEvolution 为空操作，EvolutionTriggerPoint 默认 AFTER_INVOKE
// 但 RunEvolution 不做任何事。
//
// 对齐 Python: TrajectoryRail(priority=10)
type TrajectoryRail struct {
	*EvolutionRail
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTrajectoryRail 创建纯轨迹收集轨道。
//
// 对齐 Python: TrajectoryRail(trajectory_store=None)
func NewTrajectoryRail(opts ...EvolutionRailOption) *TrajectoryRail {
	rail := NewEvolutionRail(noOpExtension{}, opts...)
	return &TrajectoryRail{EvolutionRail: rail}
}

// Priority 返回优先级 10。
// 对齐 Python: TrajectoryRail.priority = 10
func (r *TrajectoryRail) Priority() int { return 10 }
```

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/harness/rails/evolution/ -run TestTrajectoryRail -v`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/harness/rails/evolution/trajectory_rail.go internal/agentcore/harness/rails/evolution/trajectory_rail_test.go
git commit -m "feat(evolution): 实现 TrajectoryRail 纯轨迹收集轨道"
```

---

### Task 5: extractor.go — resourceManager any 类型消除

**Files:**
- Modify: `internal/evolving/trajectory/extractor.go`
- Modify: `internal/evolving/trajectory/extractor_test.go`

- [ ] **Step 1: 修改 extractor.go — 字段类型和构造函数**

将 `resourceManager any` 改为 `resourceManager *resources_manager.ResourceMgr`。
将 `NewTracerTrajectoryExtractor(resourceManager ...any)` 改为 `NewTracerTrajectoryExtractor(resourceManager ...*resources_manager.ResourceMgr)`。
新增 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"`。

- [ ] **Step 2: 修改 extractor.go — 实现 buildToolDetail 中 GetToolInfos 调用**

```go
if e.resourceManager != nil && toolName != "" {
	toolInfos, err := e.resourceManager.GetToolInfos([]string{toolName}, nil)
	if err == nil && len(toolInfos) > 0 {
		info := toolInfos[0]
		toolDescription = info.GetDescription()
		if params := info.Parameters(); params != nil {
			toolSchema = params
		}
	}
}
```

删除 `// TODO: 待实现 resourceManager.get_tool_infos(tool_name)` 注释和 `_ = toolDescription` / `_ = toolSchema` 赋值。

- [ ] **Step 3: 修改 extractor_test.go — 更新调用签名**

所有 `NewTracerTrajectoryExtractor(anyValue)` 调用改为 `NewTracerTrajectoryExtractor()` 或传入 `*resources_manager.ResourceMgr`。

- [ ] **Step 4: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/evolving/trajectory/ -v`

- [ ] **Step 5: 提交**

```bash
git add internal/evolving/trajectory/extractor.go internal/evolving/trajectory/extractor_test.go
git commit -m "fix(trajectory): 消除 TracerTrajectoryExtractor.resourceManager any 类型，改用 *ResourceMgr"
```

---

### Task 6: doc.go 更新 + 覆盖率验证 + IMPLEMENTATION_PLAN.md 状态更新

**Files:**
- Modify: `internal/agentcore/harness/rails/evolution/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 doc.go 文件目录**

在文件目录树中添加新增的 6 个文件：
```
//	evolution/
//	├── doc.go                # 包文档
//	├── contracts.go          # 契约类型 + ApprovalManager 接口
//	├── approval_events.go    # 审批事件构建函数（7 导出 + 1 非导出）
//	├── approval_runtime.go   # EvolutionApprovalRuntime 结构体 + 4 方法
//	├── extension.go          # EvolutionExtension 接口 + noOpExtension 默认实现
//	├── evolution_rail.go     # EvolutionRail 基类 + EvolutionTriggerPoint 枚举 + Options
//	├── trajectory_rail.go    # TrajectoryRail 纯轨迹收集轨道
//	└── helpers.go            # 辅助函数（splitResponseTokenFields / normalizeSkillNames / normalizeMemberRole / collectMessagesFromTrajectory）
```

同时更新包功能概述，补充 P2 内容。

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md 中 9.24 P2 状态**

将 `P2(☐ 基类+纯收集: EvolutionRail基类+TrajectoryRail)` 改为 `P2(✅ 基类+纯收集: EvolutionRail基类+TrajectoryRail)`

- [ ] **Step 3: 运行覆盖率检查**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/harness/rails/evolution/ -v`

确保覆盖率 ≥ 85%。

- [ ] **Step 4: 运行全量编译检查**

Run: `cd /home/opensource/uap-claw-go && go build ./...`

确保无编译错误。

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/rails/evolution/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs(evolution): 更新 doc.go + IMPLEMENTATION_PLAN.md P2 状态"
```
