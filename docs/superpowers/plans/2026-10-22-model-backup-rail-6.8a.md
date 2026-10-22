# ModelBackupRail（6.8a）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 一比一复刻 Python `ModelBackupRail`——LLM 调用异常时按序切换备用模型并请求重试的 failover 护栏，补齐测试对齐 A 类缺口第 1 项。

**Architecture:** 新文件 `internal/agentcore/single_agent/rail/model_backup.go`（目录对齐 Python），嵌入 `BaseRail` 覆写 `OnModelException` + `GetCallbacks`；通过本地 `modelSwitcher` 接口断言（Python `hasattr` 等价，沿用 task_planning.go 先例）；重试闭环复用既有 RailExecutor 的 consumeRetryRequest 机制，零生产代码改动。单测同包 + 集成测试对齐 Python `test_model_backup_rail.py` 意图（主模型失败 → failover → 重试成功）。

**Tech Stack:** Go 1.26 / testify / go suite（集成测试）/ mockllm（tests/integration/mockllm 设施）

**Spec:** `docs/superpowers/specs/2026-10-22-model-backup-rail-6.8a-design.md`

**编译规则提醒（规则 5）:** 编译/测试前先 `pgrep -f 'go (build|test)'` 检查残留进程；设置 `export GOPROXY=https://goproxy.cn,direct`；不清理缓存。

---

## 关键背景（执行者必读）

1. **Python 原文**（`/home/opensource/agent-core/openjiuwen/core/single_agent/rail/model_backup.py`，19 行）：

```python
class ModelBackupRail(AgentRail):
    def __init__(self, backup_models: List[Model]):
        super().__init__()
        self.backup_models = backup_models
        self.index = 0

    async def on_model_exception(self, ctx: AgentCallbackContext) -> None:
        if hasattr(ctx.agent, "set_llm") and self.index < len(self.backup_models):
            ctx.agent.set_llm(self.backup_models[self.index])
            self.index += 1
            ctx.request_retry()
```

2. **重试闭环已就绪（勿改动）**：`react_model_call.go` 的 `rail.ModelCallRail.Execute` 包裹 LLM 调用 → 异常 → fire `CallbackOnModelException` → rail 调 `RequestRetry(0)` → `executor.go` except 分支 `ConsumeRetryRequest` → attempt+1 重执行 fn → `railedModelCall` 重新 `getLLM()`（每次重新取）→ 成功。`ReActAgent.SetLLM`（react_helpers.go:45）同步 config 三字段并重置 llmOnce。

3. **Go 无覆写检测**：嵌入 `BaseRail` 后必须在 `GetCallbacks()` 中显式声明已覆盖的钩子（`CallbackFrom` + `BuildCallbacks`），否则钩子不会被注册。

4. **声明顺序（规范 2）**：结构体 → 枚举 → 常量 → 全局变量 → 导出函数 → 非导出函数，分节注释分隔。modelSwitcher 接口归结构体区块。

---

### Task 1: ModelBackupRail 生产代码 + 同包单元测试（TDD）

**Files:**
- Create: `internal/agentcore/single_agent/rail/model_backup.go`
- Test: `internal/agentcore/single_agent/rail/model_backup_test.go`

- [ ] **Step 1.1: 写失败的单元测试**

创建 `internal/agentcore/single_agent/rail/model_backup_test.go`。rail 包现有测试仅 executor_test.go，fake agent 需在本文件自带（不能 import harness/rails 测试文件）：

```go
package rail

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	streamschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/stream/schema"
)

// ──────────────────────────── Mock 实现 ────────────────────────────

// fakeSwitcherAgent 同时实现 BaseAgent + modelSwitcher 的测试桩
type fakeSwitcherAgent struct {
	// setLLMCalls 记录 SetLLM 收到的模型
	setLLMCalls []*llm.Model
	// cbMgr 回调管理器（BaseAgent 接口要求）
	cbMgr *agentinterfaces.AgentCallbackManager
}

func newFakeSwitcherAgent() *fakeSwitcherAgent {
	return &fakeSwitcherAgent{cbMgr: agentinterfaces.NewAgentCallbackManager("mb-test-agent")}
}

func (f *fakeSwitcherAgent) SetLLM(model *llm.Model) {
	f.setLLMCalls = append(f.setLLMCalls, model)
}
func (f *fakeSwitcherAgent) GetLLM() (*llm.Model, error) { return nil, nil }

func (f *fakeSwitcherAgent) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}
func (f *fakeSwitcherAgent) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}
func (f *fakeSwitcherAgent) Stream(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (<-chan streamschema.Schema, error) {
	return nil, nil
}
func (f *fakeSwitcherAgent) Card() *agentschema.AgentCard {
	return &agentschema.AgentCard{BaseCard: agentschema.BaseCard{ID: "mb-test-agent"}}
}
func (f *fakeSwitcherAgent) Config() agentinterfaces.AgentConfig                     { return nil }
func (f *fakeSwitcherAgent) AbilityManager() agentinterfaces.AbilityManagerInterface { return nil }
func (f *fakeSwitcherAgent) CallbackManager() *agentinterfaces.AgentCallbackManager  { return f.cbMgr }
func (f *fakeSwitcherAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	return saprompt.NewSystemPromptBuilder()
}
func (f *fakeSwitcherAgent) RegisterCallback(_ context.Context, _ agentinterfaces.AgentCallbackEvent, _ cb.PerAgentCallbackFunc, _ ...cb.CallbackOption) error {
	return nil
}
func (f *fakeSwitcherAgent) RegisterRail(_ context.Context, _ agentinterfaces.AgentRail, _ ...cb.CallbackOption) error {
	return nil
}
func (f *fakeSwitcherAgent) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error { return nil }

// 编译时验证 fakeSwitcherAgent 同时满足 BaseAgent 与 modelSwitcher
var _ agentinterfaces.BaseAgent = (*fakeSwitcherAgent)(nil)
var _ modelSwitcher = (*fakeSwitcherAgent)(nil)

// fakePlainAgent 仅实现 BaseAgent（不实现 modelSwitcher）
type fakePlainAgent struct {
	fakeSwitcherAgent
}

func (f *fakePlainAgent) SetLLM(model *llm.Model) { panic("不应被调用") }
func (f *fakePlainAgent) GetLLM() (*llm.Model, error) {
	panic("不应被调用")
}

// 注意：fakePlainAgent 通过嵌入 fakeSwitcherAgent 仍携带 SetLLM/GetLLM 方法，
// 类型断言会成功——为测「不实现 modelSwitcher」分支，需用独立类型。
// 改用显式不实现：直接复用 executor_test.go 的 fakeRailAgent（若已存在）
// 或定义不带 SetLLM 的独立结构体。见下方 fakeNoSwitcherAgent。

type fakeNoSwitcherAgent struct {
	cbMgr *agentinterfaces.AgentCallbackManager
}

func newFakeNoSwitcherAgent() *fakeNoSwitcherAgent {
	return &fakeNoSwitcherAgent{cbMgr: agentinterfaces.NewAgentCallbackManager("mb-noswitch-agent")}
}

func (f *fakeNoSwitcherAgent) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}
func (f *fakeNoSwitcherAgent) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}
func (f *fakeNoSwitcherAgent) Stream(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (<-chan streamschema.Schema, error) {
	return nil, nil
}
func (f *fakeNoSwitcherAgent) Card() *agentschema.AgentCard {
	return &agentschema.AgentCard{BaseCard: agentschema.BaseCard{ID: "mb-noswitch-agent"}}
}
func (f *fakeNoSwitcherAgent) Config() agentinterfaces.AgentConfig                     { return nil }
func (f *fakeNoSwitcherAgent) AbilityManager() agentinterfaces.AbilityManagerInterface { return nil }
func (f *fakeNoSwitcherAgent) CallbackManager() *agentinterfaces.AgentCallbackManager  { return f.cbMgr }
func (f *fakeNoSwitcherAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	return saprompt.NewSystemPromptBuilder()
}
func (f *fakeNoSwitcherAgent) RegisterCallback(_ context.Context, _ agentinterfaces.AgentCallbackEvent, _ cb.PerAgentCallbackFunc, _ ...cb.CallbackOption) error {
	return nil
}
func (f *fakeNoSwitcherAgent) RegisterRail(_ context.Context, _ agentinterfaces.AgentRail, _ ...cb.CallbackOption) error {
	return nil
}
func (f *fakeNoSwitcherAgent) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error { return nil }

var _ agentinterfaces.BaseAgent = (*fakeNoSwitcherAgent)(nil)

// newTestModel 构造轻量 Model 指针（仅用于指针断言，不发起真实调用）
func newTestModel(id string) *llm.Model {
	return &llm.Model{
		ModelConfig:  &llmschema.ModelRequestConfig{ModelName: id},
		ClientConfig: &llmschema.ModelClientConfig{ClientID: id},
	}
}

// ──────────────────────────── 单元测试 ────────────────────────────

func TestNewModelBackupRail_构造(t *testing.T) {
	t.Parallel()
	m1, m2 := newTestModel("m1"), newTestModel("m2")
	r := NewModelBackupRail([]*llm.Model{m1, m2})
	assert.Equal(t, []*llm.Model{m1, m2}, r.backupModels)
	assert.Equal(t, 0, r.index)
	assert.Equal(t, 50, r.Priority(), "默认优先级应为 50，对齐 Python AgentRail.priority")
}

func TestNewModelBackupRail_空列表(t *testing.T) {
	t.Parallel()
	r := NewModelBackupRail(nil)
	assert.Empty(t, r.backupModels)
}

func TestModelBackupRail_GetCallbacks映射(t *testing.T) {
	t.Parallel()
	r := NewModelBackupRail([]*llm.Model{newTestModel("m1")})
	callbacks := r.GetCallbacks()
	assert.Len(t, callbacks, 1, "仅声明 OnModelException 一个钩子")
	fn, ok := callbacks[agentinterfaces.CallbackOnModelException]
	require.True(t, ok, "应包含 CallbackOnModelException 映射")
	require.NotNil(t, fn)

	// wrap 函数类型断言后应可正常调用（不 panic）
	agent := newFakeSwitcherAgent()
	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ModelCallInputs{}, nil)
	require.NoError(t, fn(context.Background(), cbc))
	// 触发了一次切换
	require.Len(t, agent.setLLMCalls, 1)
}

func TestModelBackupRail_OnModelException_切换并请求重试(t *testing.T) {
	t.Parallel()
	m1, m2 := newTestModel("m1"), newTestModel("m2")
	r := NewModelBackupRail([]*llm.Model{m1, m2})
	agent := newFakeSwitcherAgent()
	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ModelCallInputs{}, nil)

	require.NoError(t, r.OnModelException(context.Background(), cbc))
	// 切到第一个备用模型
	require.Len(t, agent.setLLMCalls, 1)
	assert.Equal(t, m1, agent.setLLMCalls[0])
	assert.Equal(t, 1, r.index)
	// 请求了重试（consume-and-clear 语义：消费一次后清空）
	require.NotNil(t, cbc.ConsumeRetryRequest())
	assert.Nil(t, cbc.ConsumeRetryRequest(), "retry 请求应一次性消费")
}

func TestModelBackupRail_OnModelException_连续异常依次切换(t *testing.T) {
	t.Parallel()
	m1, m2 := newTestModel("m1"), newTestModel("m2")
	r := NewModelBackupRail([]*llm.Model{m1, m2})
	agent := newFakeSwitcherAgent()
	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ModelCallInputs{}, nil)

	// 第一次异常 → m1
	_ = cbc.ConsumeRetryRequest() // 清掉上次（模拟 executor 循环行为）
	require.NoError(t, r.OnModelException(context.Background(), cbc))
	_ = cbc.ConsumeRetryRequest()
	// 第二次异常 → m2
	require.NoError(t, r.OnModelException(context.Background(), cbc))
	require.Len(t, agent.setLLMCalls, 2)
	assert.Equal(t, m2, agent.setLLMCalls[1])
	assert.Equal(t, 2, r.index)
	_ = cbc.ConsumeRetryRequest()
	// 第三次异常 → 备用耗尽，不再切换
	require.NoError(t, r.OnModelException(context.Background(), cbc))
	assert.Len(t, agent.setLLMCalls, 2, "备用耗尽后不应再切换")
	assert.Nil(t, cbc.ConsumeRetryRequest(), "备用耗尽后不应请求重试")
}

func TestModelBackupRail_OnModelException_agent不支持切换(t *testing.T) {
	t.Parallel()
	r := NewModelBackupRail([]*llm.Model{newTestModel("m1")})
	agent := newFakeNoSwitcherAgent()
	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ModelCallInputs{}, nil)

	require.NoError(t, r.OnModelException(context.Background(), cbc))
	assert.Equal(t, 0, r.index, "不切换时 index 不应推进")
	assert.Nil(t, cbc.ConsumeRetryRequest(), "不切换时不应请求重试")
}

func TestModelBackupRail_OnModelException_空备用列表(t *testing.T) {
	t.Parallel()
	r := NewModelBackupRail(nil)
	agent := newFakeSwitcherAgent()
	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ModelCallInputs{}, nil)

	require.NoError(t, r.OnModelException(context.Background(), cbc))
	assert.Empty(t, agent.setLLMCalls)
	assert.Nil(t, cbc.ConsumeRetryRequest())
}
```

**注意事项：**
- `agentschema.BaseCard` 的字段名以 `internal/agentcore/single_agent/schema/agent_card.go` 实际定义为准，若 `BaseCard{ID: ...}` 编译失败，改用 `agentschema.NewAgentCard(agentschema.WithAgentID("..."))`（agent_suite.go:84 先例）
- `llm.Model` 字段名以 `foundation/llm/model.go` 为准（已确认有导出字段 `ModelConfig`/`ClientConfig`）
- `streamschema` 的导入路径若与 executor_test.go 不一致，以 executor_test.go 现有导入为准（Stream 返回类型）
- fakePlainAgent 定义是冗余的教训示例，实现时直接删除（用 fakeNoSwitcherAgent 覆盖该分支）

- [ ] **Step 1.2: 运行测试确认失败**

```bash
cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' ; export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/single_agent/rail/ -run TestModelBackupRail -v 2>&1 | head -30
```

预期：编译失败 `undefined: NewModelBackupRail`（以及 `undefined: modelSwitcher`）。

- [ ] **Step 1.3: 写生产实现**

创建 `internal/agentcore/single_agent/rail/model_backup.go`：

```go
// Package rail 提供 Agent 生命周期 Rail 执行器。
// 本文件：ModelBackupRail 模型备份护栏（一比一复刻 Python）。

package rail

import (
	"context"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// modelSwitcher 模型切换接口。
// Python: hasattr(ctx.agent, "set_llm") 动态能力检测的 Go 等价，
// 沿用 harness/rails/task_planning.go 同名接口先例。
type modelSwitcher interface {
	// SetLLM 设置预构建的 LLM 模型实例
	SetLLM(model *llm.Model)
	// GetLLM 返回 LLM 模型实例
	GetLLM() (*llm.Model, error)
}

// ModelBackupRail 模型备份护栏，LLM 调用异常时按序切换备用模型并请求重试。
//
// Python: ModelBackupRail (openjiuwen/core/single_agent/rail/model_backup.py)
//
// 流程位置：ReAct 循环 Think 步骤的 ModelCallRail.Execute 内，
// LLM 调用抛异常 → fire ON_MODEL_EXCEPTION → 本护栏切换备用模型
// → RequestRetry → RailExecutor 吞异常重试（此时已换备用模型）。
// 备用耗尽后不再请求重试，异常正常上抛。
type ModelBackupRail struct {
	agentinterfaces.BaseRail
	// backupModels 备用模型链，按序依次切换
	// Python: backup_models
	backupModels []*llm.Model
	// index 下一个待切换的备用模型下标
	// Python: index；无锁对齐 Python（ReAct 循环内串行触发）
	index int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewModelBackupRail 创建模型备份护栏。
// Python: ModelBackupRail(backup_models)
func NewModelBackupRail(backupModels []*llm.Model) *ModelBackupRail {
	return &ModelBackupRail{
		BaseRail:     *agentinterfaces.NewBaseRail(),
		backupModels: backupModels,
	}
}

// OnModelException LLM 调用异常时切换到下一个备用模型并请求重试。
// Python: on_model_exception (L15-18)
func (r *ModelBackupRail) OnModelException(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// Python L16: if hasattr(ctx.agent, "set_llm") and self.index < len(self.backup_models)
	switcher, ok := cbc.Agent().(modelSwitcher)
	if !ok || r.index >= len(r.backupModels) {
		// agent 不支持切换或备用耗尽 → 不请求重试，异常正常上抛
		return nil
	}
	switcher.SetLLM(r.backupModels[r.index])
	r.index++
	// Python: ctx.request_retry()（默认 delay_seconds=0.0）
	cbc.RequestRetry(0)
	return nil
}

// GetCallbacks 返回已覆盖的钩子方法映射。
// Go 无 Python 的覆写检测，需显式声明（CallbackFrom + BuildCallbacks）。
func (r *ModelBackupRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return r.BuildCallbacks(
		r.CallbackFrom(agentinterfaces.CallbackOnModelException, func(ctx context.Context, railCtx any) error {
			return r.OnModelException(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
		}),
	)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

**注意：**
- 文件头部包注释简化（包注释在 doc.go，本文件只留职责说明行，避免双包注释）
- `modelSwitcher` 与 task_planning.go 的同名接口重复定义——两个包各自持有小接口是 Go 惯例（consumer-side interface），非重复代码问题，无需共享
- 若 `rail` 包与 `runner/callback` 存在循环依赖编译报错，检查 executor.go 现有导入（它已导入 interfaces 而 callback 在 runner 侧被 interfaces 依赖；若 callback 不可导入，改用 `agentinterfaces` 包内已导出的回调类型——executor.go 现有 import 块是权威参照）

- [ ] **Step 1.4: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/single_agent/rail/ -run "TestModelBackupRail|TestNewModelBackupRail" -v 2>&1 | tail -20
```

预期：全部 PASS（7 个测试函数）。

- [ ] **Step 1.5: 跑全包测试 + 覆盖率**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/single_agent/... -cover 2>&1 | grep -E "rail|FAIL|ok" | head -10
```

预期：`rail` 包 ok，覆盖率 ≥ 85%（model_backup.go 全部行覆盖：构造 2 行 + OnModelException 全分支 + GetCallbacks）。

- [ ] **Step 1.6: 提交**

```bash
cd /home/opensource/uap-claw-go && git add internal/agentcore/single_agent/rail/model_backup.go internal/agentcore/single_agent/rail/model_backup_test.go && git commit -m "feat(6.8a): ModelBackupRail 模型备份护栏一比一复刻

对齐 Python openjiuwen/core/single_agent/rail/model_backup.py：
- OnModelException 按序切换备用模型 + RequestRetry
- modelSwitcher 本地接口断言（hasattr 等价）
- GetCallbacks 显式声明钩子映射
- 7 个单元测试覆盖全分支"
```

---

### Task 2: 集成测试（对齐 Python test_model_backup_rail.py 意图）

**Files:**
- Create: `tests/integration/agentcore/single_agent/model_backup_rail_test.go`
- Test: 同文件（build tag integration）

**Python 原测试意图**：主模型指向必然失败的 mock_url → invoke 触发 ON_MODEL_EXCEPTION → rail 切换到 MockLLMModel → 重试成功 → 断言输出。

- [ ] **Step 2.1: 写集成测试**

创建 `tests/integration/agentcore/single_agent/model_backup_rail_test.go`：

```go
//go:build integration

// Package single_agent 单 Agent 层集成测试。
// 本文件：ModelBackupRail 模型备份护栏 failover 全链路测试。

package single_agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	rail "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	"github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ModelBackupRailSuite 模型备份护栏集成测试套件。
// 对齐 Python: tests/system_tests/rail/test_model_backup_rail.py
// (test_middleware_executes_when_react_agent_invoke)
type ModelBackupRailSuite struct {
	suite.AgentSuite
	// failClient 主模型 mock 客户端（始终返回错误）
	failClient *mockllm.MockModelClient
	// failProviderName 主模型 provider 注册名
	failProviderName string
	// backupClient 备用模型 mock 客户端
	backupClient *mockllm.MockModelClient
	// backupProviderName 备用模型 provider 注册名
	backupProviderName string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SetupSuite 注册主/备两个 mock provider。
// 主模型 = 始终失败的 mock（对齐 Python mock_url 真实连接失败，
// Go 用 AddErrorResponse 错误注入，语义等价：都触发 ON_MODEL_EXCEPTION）。
func (s *ModelBackupRailSuite) SetupSuite() {
	s.AgentSuite.SetupSuite()

	s.failClient = mockllm.NewMockModelClient()
	s.failClient.AddErrorResponse(errors.New("模拟主模型服务不可用"))
	s.failProviderName = fmt.Sprintf("mb_fail_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(s.failProviderName, "llm", s.failClient.Factory())

	s.backupClient = mockllm.NewMockModelClient()
	s.backupClient.AddTextResponse("备份模型接管成功：计算完成 1+2=3")
	s.backupProviderName = fmt.Sprintf("mb_backup_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(s.backupProviderName, "llm", s.backupClient.Factory())
}

// TearDownSuite 注销 mock provider。
func (s *ModelBackupRailSuite) TearDownSuite() {
	_ = model_clients.GetClientRegistry().Unregister(s.failProviderName, "llm")
	_ = model_clients.GetClientRegistry().Unregister(s.backupProviderName, "llm")
	s.AgentSuite.TearDownSuite()
}

// TestModelBackupRailSuite 运行模型备份护栏集成测试套件
func TestModelBackupRailSuite(t *testing.T) {
	suite.Run(t, new(ModelBackupRailSuite))
}

// TestModelBackupRail_Failover主模型故障切换备用 测试主模型调用异常时
// ModelBackupRail 切换到备用模型并重试成功。
// 对齐 Python: test_middleware_executes_when_react_agent_invoke
func (s *ModelBackupRailSuite) TestModelBackupRail_Failover主模型故障切换备用() {
	// 主模型：指向始终失败的 provider（对齐 Python api_base="mock_url"）
	failModel, err := llm.NewModel(
		&llmschema.ModelClientConfig{
			ClientID:       s.failProviderName + "_id",
			ClientProvider: s.failProviderName,
			APIKey:         "mock",
		},
		&llmschema.ModelRequestConfig{ModelName: "fail-model"},
	)
	s.Require().NoError(err, "创建失败主 Model 失败")

	// 备用模型：指向成功 provider（对齐 Python 测试中 backup_models[0]=MockLLMModel）
	backupModel, err := llm.NewModel(
		&llmschema.ModelClientConfig{
			ClientID:       s.backupProviderName + "_id",
			ClientProvider: s.backupProviderName,
			APIKey:         "mock",
		},
		&llmschema.ModelRequestConfig{ModelName: "backup-model"},
	)
	s.Require().NoError(err, "创建备用 Model 失败")

	// 创建 DeepAgent：主模型 failModel + 挂 ModelBackupRail（内含 backupModel）
	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Model: failModel,
		Rails: []agentinterfaces.AgentRail{
			rail.NewModelBackupRail([]*llm.Model{backupModel}),
		},
		MaxIterations: 3,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	// Invoke：主模型失败 → ON_MODEL_EXCEPTION → 切换备用 → 重试 → 成功
	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "计算1+2"})
	s.Require().NoError(err, "failover 后 Invoke 应成功")
	s.Require().NotNil(result)

	// 断言输出来自备用模型
	output, _ := result["output"].(string)
	s.Contains(output, "计算完成 1+2=3", "输出应来自备用模型")

	// 主模型确实被调用过（至少 1 次，验证失败源真实触发）
	s.GreaterOrEqual(s.failClient.InvokeCallCount(), 1, "主模型应至少被调用一次")
	// 备用模型接管后产生响应
	s.GreaterOrEqual(s.backupClient.InvokeCallCount(), 1, "备用模型应接管响应")
}

// TestModelBackupRail_备用耗尽异常上抛 测试备用模型耗尽后异常正常上抛。
// 对齐 Python 语义：index >= len(backup_models) 时不 request_retry。
func (s *ModelBackupRailSuite) TestModelBackupRail_备用耗尽异常上抛() {
	failModel, err := llm.NewModel(
		&llmschema.ModelClientConfig{
			ClientID:       s.failProviderName + "_id",
			ClientProvider: s.failProviderName,
			APIKey:         "mock",
		},
		&llmschema.ModelRequestConfig{ModelName: "fail-model"},
	)
	s.Require().NoError(err, "创建失败主 Model 失败")

	// 空备用链：任何异常都不请求重试
	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Model: failModel,
		Rails: []agentinterfaces.AgentRail{
			rail.NewModelBackupRail(nil),
		},
		MaxIterations: 3,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "必然失败"})
	s.Require().Error(err, "备用耗尽后异常应正常上抛")
	s.True(strings.Contains(err.Error(), "模拟主模型服务不可用") ||
		strings.Contains(err.Error(), "invoke") ||
		strings.Contains(err.Error(), "模型"),
		"错误信息应包含主模型失败原因")
}
```

**注意事项：**
- `model_clients` 的导入路径以 planning 集成测试为准（`internal/agentcore/foundation/llm/model_clients`，见 tests/integration/agentcore/harness/rails/planning/task_planning_rail_test.go 现有 import）
- 若 `rail` 包导入名与 suite/其他包冲突，加别名
- AgentSuite 的 `Invoke` 返回 `map[string]any`，`output` 为 string（code_agent_test.go:79 先例 `result["output"].(string)`）
- 若 `s.Ctx` 超时 5 分钟不够，检查 fail 场景是否死循环重试（rail 只重试一次/异常，若 executor 无限重试说明 RequestRetry 语义理解有误，回查 executor.go L172-197）

- [ ] **Step 2.2: 运行集成测试**

```bash
cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' ; go test -tags=integration ./tests/integration/agentcore/single_agent/ -run TestModelBackupRailSuite -v 2>&1 | tail -25
```

预期：2 个测试 PASS。若 `备用耗尽异常上抛` 失败（错误被 DeepAgent 包装吞掉），检查 react_invoke.go 的错误传播路径——DeepAgent 可能把迭代错误包装为 result 而非返回 error；若是，改断言 result["output"] 包含错误文本或 result 非成功（以 force_finish_test.go / 工具失败错误传播测试的既有模式为准）。

- [ ] **Step 2.3: 本地跑集成审计两层门禁**

```bash
cd /home/opensource/uap-claw-go && go run ./cmd/integration-audit -verify 2>&1 | tail -15
```

预期：新目录 `tests/integration/agentcore/single_agent` 通过第一层（真实调用跨度）或进入灰区后第二层覆盖验证通过（agents + rail + interfaces + llm + model_clients ≥ 3 子系统）。若失败按输出提示修正（通常是断言数不足 2 或调用形式是函数值引用而非调用）。

- [ ] **Step 2.4: 提交**

```bash
cd /home/opensource/uap-claw-go && git add tests/integration/agentcore/single_agent/model_backup_rail_test.go && git commit -m "test(6.8a): ModelBackupRail failover 集成测试

对齐 Python tests/system_tests/rail/test_model_backup_rail.py：
- 主模型 mock 错误注入 → ON_MODEL_EXCEPTION → 切备用 → 重试成功
- 备用耗尽后异常正常上抛
- 跨 agents/rail/interfaces/llm/model_clients 子系统"
```

---

### Task 3: doc.go 回填 + IMPLEMENTATION_PLAN.md 回填 + 记忆更新

**Files:**
- Modify: `internal/agentcore/single_agent/rail/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`（领域六 6.x Rail 系统分组，6.8 行后新增 6.8a）
- Modify: `/root/.codebuddy/projects/home-opensource-uap-claw-go/memory/agentcore-test-alignment-gaps.md`
- Modify: `/root/.codebuddy/projects/home-opensource-uap-claw-go/memory/MEMORY.md`（该行 hook 更新为已完结）

- [ ] **Step 3.1: 更新 rail/doc.go**

文件目录段与核心类型索引段修改（`internal/agentcore/single_agent/rail/doc.go`）：

```go
// 文件目录：
//
//	rail/
//	├── doc.go           # 包文档
//	├── executor.go      # RailExecutor（Rail执行器） 结构体（@rail 装饰器等价）+ ModelCallRail（模型调用Rail）/ToolCallRail（工具调用Rail）
//	└── model_backup.go  # ModelBackupRail（模型备份护栏）：LLM 调用异常时按序切换备用模型并请求重试
//
// 核心类型索引：
//
//	RailExecutor    — @rail 装饰器等价（before/gate/exception/retry/after 包裹执行）
//	ModelCallRail   — 模型调用 Rail 执行器
//	ToolCallRail    — 工具调用 Rail 执行器
//	ModelBackupRail — 模型备份护栏（LLM 异常 failover 切换备用模型）
//
// 对应 Python 代码：openjiuwen/core/single_agent/rail/base.py（RailExecutor 部分）
//                   openjiuwen/core/single_agent/rail/model_backup.py（ModelBackupRail）
```

同时包概述段补一句：本包还提供 ModelBackupRail 模型备份护栏。

- [ ] **Step 3.2: 更新 IMPLEMENTATION_PLAN.md**

在 6.8 行之后（6.9 之前）插入新行。6.8 行内容以当前文件实际为准（L407），新行：

```markdown
| 6.8a | ✅ | ModelBackupRail | ✅ 模型备份护栏一比一复刻 Python（on_model_exception 按序切换备用模型 + request_retry；modelSwitcher 本地接口断言等价 hasattr；GetCallbacks 显式声明钩子映射；index 无锁对齐 Python 串行语义）；✅ 单测 7 用例全分支覆盖；✅ 集成测试对齐 Python test_model_backup_rail.py 意图（主模型 mock 错误注入 → failover 切换 → 重试成功；备用耗尽异常上抛）；跨 agents/rail/interfaces/llm/model_clients 5 子系统 | `openjiuwen/core/single_agent/rail/model_backup.py` |
```

注意：Python 参考路径列为 `openjiuwen/core/single_agent/rail/model_backup.py`（与 6.7/6.8 的 base.py 并列，同目录）。

- [ ] **Step 3.3: 更新记忆文件**

`/root/.codebuddy/projects/home-opensource-uap-claw-go/memory/agentcore-test-alignment-gaps.md`：

A 类第 1 项从缺口改为完成：

```markdown
A. 生产代码未移植导致（测试无法对齐，需先补生产代码）：
1. ~~ModelBackupRail~~ **已完成（2026-10-22）**：internal/agentcore/single_agent/rail/model_backup.go + 7 单测 + tests/integration/agentcore/single_agent/model_backup_rail_test.go（2 用例）；计划新增 6.8a 条目
```

同时更新 "How to apply" 段：下一步选择改为 agent_builder 24 文件移植决策 或 工作流 8.14-8.24（ModelBackupRail 已完结）。

`/root/.codebuddy/projects/home-opensource-uap-claw-go/memory/MEMORY.md` 对应行更新：

```markdown
- [agentcore 测试对齐差距清单](agentcore-test-alignment-gaps.md) — 已对齐~70文件；B类全完结✅；A类 ModelBackupRail✅(2026-10-22 6.8a)；余 WorkflowAgent/RL/dev_tools/auto_harness/A2A
```

- [ ] **Step 3.4: 提交回填**

```bash
cd /home/opensource/uap-claw-go && git add internal/agentcore/single_agent/rail/doc.go IMPLEMENTATION_PLAN.md && git commit -m "docs(6.8a): 回填 ModelBackupRail 到 doc.go 与实施计划"
```

（记忆文件在 ~/.codebuddy 下，不入仓库 git。）

---

### Task 4: 远端 CI 验证（规则 6 完成标准）

- [ ] **Step 4.1: 推送远端**

```bash
cd /home/opensource/uap-claw-go && git push
```

（在当前分支直接推送；若用户要求走分支/PR 则按用户指示。）

- [ ] **Step 4.2: 监控 CI 直到全绿**

```bash
cd /home/opensource/uap-claw-go && gh run list --limit 3
gh run watch <run-id>
```

预期：Build / Lint / Unit Test / Integration Test 四 job 全部 SUCCESS。任何失败：分析日志、修复、再推送，直到全绿。重点关注：
- Integration Test job 的 integration-audit 两层门禁（新目录跨度判定）
- Unit Test job 的 rail 包覆盖率 ≥ 85%

- [ ] **Step 4.3: 宣告完成**

CI 全绿后，向用户报告：生产代码 + 单测 + 集成测试 + 三处回填全部完成，6.8a 闭环。

---

## 自审记录（写计划时已核对）

1. **Spec 覆盖**：spec §3 类型设计→Task 1；§4 核心逻辑→Task 1 Step 1.3；§5.1 单测 7 项→Task 1 Step 1.1（7 个测试函数一一对应）；§5.2 集成测试→Task 2；§6 回填 4 项→Task 3 + Task 4（CI 标准）；§7 YAGNI 排除项均未出现。无遗漏。
2. **占位符扫描**：无 TBD/TODO；所有代码步骤含完整代码；测试断言失败时的修正路径已写明（BaseCard 字段名、output 类型断言、DeepAgent 错误包装三种既有先例的回退方案）。
3. **类型一致性**：`NewModelBackupRail([]*llm.Model)` 签名 Task 1 定义、Task 2 使用一致；`modelSwitcher`（SetLLM/GetLLM）与 task_planning.go 先例一致；`RequestRetry(0)` 签名与 interfaces/callback.go:600 一致；`AddErrorResponse`/`Factory`/`InvokeCallCount` 均为 mockllm 实际 API（mock_client.go:279/333/290）。
