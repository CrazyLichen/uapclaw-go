# AM 执行逻辑对齐 Python 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 对齐 Go AbilityManager 与 Python 的 Tool/Agent/Workflow 执行逻辑，修复 BuildToolMessageContent、Agent 子会话生命周期、Tool session 传递、Workflow 隔离与中断检测。

**Architecture:** 扩展 BuildToolMessageContent 支持 map 反射双路径；在 runner 包新增 RunAgent/RunWorkflow 全局函数编排生命周期；ToolCallOptions 增加 Session 字段并修改 InvokeFunction 用户函数签名透传 opts；新增 workflow 包定义 WorkflowOutput/WorkflowExecutionState；executeWorkflow 增加 session/context 隔离和 INPUT_REQUIRED 中断检测。

**Tech Stack:** Go 1.22+, reflect, encoding/json, session 包, context_engine 接口

---

## 文件结构

| 文件 | 操作 | 职责 |
|---|---|---|
| `internal/agentcore/single_agent/ability/ability_types.go` | 修改 | P0-1: BuildToolMessageContent 扩展 + InterruptAutoConfirmKey 常量 |
| `internal/agentcore/single_agent/ability/ability_types_test.go` | 修改 | P0-1: BuildToolMessageContent 新增测试 |
| `internal/agentcore/runner/runner.go` | 新增 | P0-2: RunAgent + RunWorkflow 全局函数 |
| `internal/agentcore/runner/runner_test.go` | 新增 | P0-2: RunAgent/RunWorkflow 测试 |
| `internal/agentcore/runner/doc.go` | 新增 | P0-2: runner 包文档 |
| `internal/agentcore/single_agent/ability/ability_manager.go` | 修改 | P0-2/P0-3/P1: executeAgent/executeTool/executeWorkflow 改造 |
| `internal/agentcore/foundation/tool/base.go` | 修改 | P0-3: ToolCallOptions + Session + WithToolSession |
| `internal/agentcore/foundation/tool/invoke_function.go` | 修改 | P0-3: 用户函数签名改为 func(ctx, I, opts...ToolOption) |
| `internal/agentcore/foundation/tool/invoke_function_test.go` | 修改 | P0-3: 测试函数签名适配 |
| `internal/agentcore/foundation/tool/stream_function.go` | 修改 | P0-3: 用户函数签名同上 |
| `internal/agentcore/foundation/tool/stream_function_test.go` | 修改 | P0-3: 测试函数签名适配 |
| `internal/agentcore/workflow/base.go` | 新增 | P1-2: WorkflowExecutionState + WorkflowOutput |
| `internal/agentcore/workflow/base_test.go` | 新增 | P1-2: 单元测试 |
| `internal/agentcore/workflow/doc.go` | 新增 | P1-2: 包文档 |
| `internal/agentcore/single_agent/interfaces/interface.go` | 修改 | P1-1: WorkflowOptions 扩展 |

---

### Task 1: P0-1 BuildToolMessageContent 扩展

**Files:**
- Modify: `internal/agentcore/single_agent/ability/ability_types.go`
- Modify: `internal/agentcore/single_agent/ability/ability_types_test.go`

- [ ] **Step 1: 写 BuildToolMessageContent 的失败测试**

在 `ability_types_test.go` 中新增以下测试：

```go
func TestBuildToolMessageContent_result解包(t *testing.T) {
    // structToMap 包装的 {"result": "search..."} 应解包为 "search..."
    result := map[string]any{"result": "search results..."}
    got := BuildToolMessageContent(result)
    assert.Equal(t, "search results...", got)
}

func TestBuildToolMessageContent_普通map走JSON序列化(t *testing.T) {
    result := map[string]any{"message": "created", "count": 2}
    got := BuildToolMessageContent(result)
    assert.Equal(t, `{"count":2,"message":"created"}`, got)
}

func TestBuildToolMessageContent_反射提取DataContent(t *testing.T) {
    type toolOutput struct {
        Data    map[string]any
        Success bool
        Error   string
    }
    result := toolOutput{
        Data:    map[string]any{"content": "hello"},
        Success: true,
    }
    got := BuildToolMessageContent(result)
    assert.Equal(t, "hello", got)
}

func TestBuildToolMessageContent_反射提取Error(t *testing.T) {
    type toolOutput struct {
        Data    any
        Success bool
        Error   string
    }
    result := toolOutput{
        Data:    nil,
        Success: false,
        Error:   "timeout",
    }
    got := BuildToolMessageContent(result)
    assert.Equal(t, "timeout", got)
}

func TestBuildToolMessageContent_反射指针类型(t *testing.T) {
    type toolOutput struct {
        Data    map[string]any
        Success bool
        Error   string
    }
    result := &toolOutput{
        Data:    map[string]any{"content": "ptr hello"},
        Success: true,
    }
    got := BuildToolMessageContent(result)
    assert.Equal(t, "ptr hello", got)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/single_agent/ability/... -run "TestBuildToolMessageContent_结果解包|TestBuildToolMessageContent_普通map走JSON序列化|TestBuildToolMessageContent_反射" -v`

Expected: FAIL（新测试不通过）

- [ ] **Step 3: 修改 BuildToolMessageContent 实现**

修改 `ability_types.go` 中的 `BuildToolMessageContent`，增加 import `encoding/json` 和 `reflect`，扩展为四路径：

```go
func BuildToolMessageContent(result any) string {
    // 路径 1：map[string]any — 按 key 提取
    if m, ok := result.(map[string]any); ok {
        // 1a. data.content 提取
        if data, ok := m["data"].(map[string]any); ok {
            if content, ok := data["content"]; ok {
                if s := fmt.Sprintf("%v", content); s != "" {
                    return s
                }
            }
        }
        // 1b. success=false + error 提取
        if success, ok := m["success"].(bool); ok && !success {
            if errVal, ok := m["error"]; ok {
                return fmt.Sprintf("%v", errVal)
            }
        }
        // 1c. structToMap 的 {"result": v} 包装 — 解包后递归处理
        // 对齐 Python: LocalFunction 返回 string 时，Go 包装为 {"result": v}，
        // 需解包后递归，使 "search..." 走到路径 3 的 fmt.Sprintf("%v", result) 返回原值。
        if v, ok := m["result"]; ok && len(m) == 1 {
            return BuildToolMessageContent(v)
        }
        // 1d. 普通 map — JSON 序列化（对齐 Python str(dict)）
        if jsonBytes, err := json.Marshal(m); err == nil {
            return string(jsonBytes)
        }
    }

    // 路径 2：反射提取（对齐 Python getattr(result, "data", None)）
    v := reflect.ValueOf(result)
    if v.Kind() == reflect.Ptr {
        v = v.Elem()
    }
    if v.Kind() == reflect.Struct {
        if f := v.FieldByName("Data"); f.IsValid() {
            if dataMap, ok := f.Interface().(map[string]any); ok {
                if content, ok := dataMap["content"]; ok {
                    if s := fmt.Sprintf("%v", content); s != "" {
                        return s
                    }
                }
            }
        }
        if f := v.FieldByName("Success"); f.IsValid() && f.Kind() == reflect.Bool && !f.Bool() {
            if ef := v.FieldByName("Error"); ef.IsValid() {
                return fmt.Sprintf("%v", ef.Interface())
            }
        }
    }

    // 路径 3：最终 fallback
    return fmt.Sprintf("%v", result)
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/single_agent/ability/... -run TestBuildToolMessageContent -v`

Expected: ALL PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/single_agent/ability/ability_types.go internal/agentcore/single_agent/ability/ability_types_test.go
git commit -m "feat(ability): 扩展 BuildToolMessageContent 支持反射提取和 result 解包"
```

---

### Task 2: P0-3 ToolCallOptions 增加 Session + InvokeFunction 签名改造

**Files:**
- Modify: `internal/agentcore/foundation/tool/base.go`
- Modify: `internal/agentcore/foundation/tool/invoke_function.go`
- Modify: `internal/agentcore/foundation/tool/invoke_function_test.go`
- Modify: `internal/agentcore/foundation/tool/stream_function.go`
- Modify: `internal/agentcore/foundation/tool/stream_function_test.go`

- [ ] **Step 1: 在 base.go 中增加 Session 字段和 WithToolSession**

在 `ToolCallOptions` 结构体中增加 `Session` 字段，新增 `WithToolSession` 函数：

```go
// ToolCallOptions 工具调用的扩展选项。
type ToolCallOptions struct {
    SkipNoneValue      bool
    SkipInputsValidate bool
    Timeout            float64
    MaxResponseBytes   int
    RaiseForStatus     bool
    Session            *session.Session  // 会话实例（对齐 Python kwargs["session"]）
}

// WithToolSession 设置会话实例。
func WithToolSession(sess *session.Session) ToolOption {
    return func(o *ToolCallOptions) { o.Session = sess }
}
```

需要新增 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/session"`。

- [ ] **Step 2: 修改 InvokeFunction 用户函数签名**

在 `invoke_function.go` 中：
- 将 `fn` 字段类型从 `func(context.Context, I) (O, error)` 改为 `func(context.Context, I, ...ToolOption) (O, error)`
- 将 `Invoke` 方法内部调用从 `f.fn(ctx, input)` 改为 `f.fn(ctx, input, opts...)`
- 更新 `NewInvokeFunction` 的文档注释中的用户函数签名说明

- [ ] **Step 3: 修改 StreamFunction 用户函数签名**

在 `stream_function.go` 中同样修改 `fn` 字段类型和调用方式。

- [ ] **Step 4: 修改测试文件中所有用户函数签名**

在 `invoke_function_test.go` 中，将所有测试用函数从 `func(ctx context.Context, input Xxx) (Yyy, error)` 改为 `func(ctx context.Context, input Xxx, opts ...ToolOption) (Yyy, error)`。函数体内忽略 opts。

在 `stream_function_test.go` 中同样处理。

- [ ] **Step 5: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/foundation/tool/... -v`

Expected: ALL PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/foundation/tool/base.go internal/agentcore/foundation/tool/invoke_function.go internal/agentcore/foundation/tool/invoke_function_test.go internal/agentcore/foundation/tool/stream_function.go internal/agentcore/foundation/tool/stream_function_test.go
git commit -m "feat(tool): ToolCallOptions 增加 Session，InvokeFunction 用户函数签名增加 opts ...ToolOption"
```

---

### Task 3: P0-2 Runner 全局函数 + executeAgent 改造

**Files:**
- Create: `internal/agentcore/runner/runner.go`
- Create: `internal/agentcore/runner/runner_test.go`
- Create: `internal/agentcore/runner/doc.go`
- Modify: `internal/agentcore/single_agent/ability/ability_types.go`
- Modify: `internal/agentcore/single_agent/ability/ability_manager.go`

- [ ] **Step 1: 创建 runner/runner.go — RunAgent 和 RunWorkflow 全局函数**

```go
package runner

import (
    "context"

    "github.com/uapclaw/uapclaw-go/internal/agentcore/session"
    "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RunAgent 执行单个 Agent，管理完整的会话生命周期。
//
// 对齐 Python: Runner.run_agent(agent, inputs, *, session, context, envs)
// Python 源码: openjiuwen/core/runner/runner.py L399-427
//
// 步骤对照：
//
//	Python L417: with self._root_task_group_scope()
//	Python L418: agent_instance, agent_session = await self._prepare_agent(agent, inputs, session)
//	  Python L504-512: if isinstance(session, AgentSession) → pre_run + return
//	  Python L513-514: session_id = inputs.get(conversation_id, ...)
//	  Python L515-522: if isinstance(agent, str) → get_agent + remote check
//	  Python L524-526: agent_session = _create_agent_session + pre_run
//	Python L419: if _is_remote_agent → invoke(inputs)
//	Python L421-423: elif LegacyBaseAgent → invoke(inputs, session=None)
//	Python L425: else → invoke(inputs, agent_session)
//	Python L426: await agent_session.post_run()
func RunAgent(
    ctx context.Context,
    agent interfaces.BaseAgent,
    inputs map[string]any,
    sess *session.Session,
) (any, error) {
    // 步骤 1：任务组作用域（对齐 Python L417: with self._root_task_group_scope()）
    // ⤵️ 预留章节回填：任务组作用域

    // 步骤 2：_prepare_agent → pre_run（对齐 Python L418 → L509/L511/L525: await session.pre_run(inputs=inputs)）
    // ⤵️ 预留章节回填：session.PreRun

    // 步骤 3：远程 Agent 判断（对齐 Python L419-420: if _is_remote_agent → invoke(inputs)）
    // ⤵️ 预留章节回填：远程 Agent 支持

    // 步骤 4：LegacyBaseAgent 判断（对齐 Python L421-423: elif LegacyBaseAgent → invoke(inputs, session=None)）
    // ⤵️ 预留章节回填：LegacyBaseAgent 兼容

    // 步骤 5：正常 Agent 调用（对齐 Python L425: res = await agent_instance.invoke(inputs, agent_session)）
    result, err := agent.Invoke(ctx, inputs, interfaces.WithSession(sess))
    if err != nil {
        return nil, err
    }

    // 步骤 6：post_run 清理（对齐 Python L426: await agent_session.post_run()）
    // ⤵️ 预留章节回填：session.PostRun

    return result, nil
}

// RunWorkflow 执行单个 Workflow，管理会话和上下文生命周期。
//
// 对齐 Python: Runner.run_workflow(workflow, inputs, *, session, context, envs)
// Python 源码: openjiuwen/core/runner/runner.py L350-369
//
// 步骤对照：
//
//	Python L367: with self._root_task_group_scope()
//	Python L368: workflow_instance, workflow_session = await self._prepare_workflow(workflow, session)
//	Python L369: workflow_instance.invoke(inputs, session=workflow_session, context=context)
func RunWorkflow(
    ctx context.Context,
    workflow interfaces.Workflow,
    inputs map[string]any,
    workflowSess *session.WorkflowSession,
    wfCtx any,
) (any, error) {
    // 步骤 1：任务组作用域（对齐 Python L367: with self._root_task_group_scope()）
    // ⤵️ 预留章节回填：任务组作用域

    // 步骤 2：_prepare_workflow（对齐 Python L368）
    // ⤵️ 预留章节回填：_prepare_workflow 完整逻辑

    // 步骤 3：调用 workflow.Invoke（对齐 Python L369: workflow_instance.invoke(inputs, session=workflow_session, context=context)）
    // ⤵️ 预留章节回填：WorkflowOptions 传 session + context
    result, err := workflow.Invoke(ctx, inputs)
    if err != nil {
        return nil, err
    }
    return result, nil
}
```

- [ ] **Step 2: 创建 runner/doc.go**

```go
// Package runner 提供全局运行器，编排 Agent/Workflow 的执行生命周期。
//
// 对齐 Python: openjiuwen/core/runner/runner.py (Runner)
//
// Python 中 Runner 是全局单例类（@classmethod 代理到 GLOBAL_RUNNER），
// Go 采用包级全局函数模式，更符合 Go 惯用法。
//
// 文件目录：
//
//	runner/
//	├── doc.go                # 包文档
//	├── callback/             # 回调框架子包
//	└── runner.go             # RunAgent/RunWorkflow 全局函数
//
// 对应 Python 代码：openjiuwen/core/runner/runner.py
package runner
```

- [ ] **Step 3: 创建 runner/runner_test.go — 基础测试**

测试 RunAgent 和 RunWorkflow 的基本调用路径（使用 mock agent/workflow）。

- [ ] **Step 4: 在 ability_types.go 中新增 InterruptAutoConfirmKey 常量**

```go
// InterruptAutoConfirmKey 中断自动确认状态键。
// 对齐 Python: openjiuwen/core/single_agent/interrupt/state.py INTERRUPT_AUTO_CONFIRM_KEY
var InterruptAutoConfirmKey = state.StringKey("__interrupt_auto_confirm__")
```

需要新增 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"`。

- [ ] **Step 5: 修改 ability_manager.go 中的 executeAgent**

将当前 `executeAgent` 方法改造为包含子会话创建、conversation_id 注入、auto_confirm 传播、runner.RunAgent 调用的完整步骤。具体对照 spec 中 P0-2 的步骤 1-9。

- [ ] **Step 6: 修改 ability_manager.go 中的 executeTool 和 executeFallbackTool**

将 `lt.Invoke(ctx, toolArgs)` 改为 `lt.Invoke(ctx, toolArgs, tool.WithToolSession(sess))`。

- [ ] **Step 7: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/... ./internal/agentcore/single_agent/ability/... -v`

Expected: ALL PASS

- [ ] **Step 8: 提交**

```bash
git add internal/agentcore/runner/runner.go internal/agentcore/runner/runner_test.go internal/agentcore/runner/doc.go internal/agentcore/single_agent/ability/ability_types.go internal/agentcore/single_agent/ability/ability_manager.go
git commit -m "feat(ability): Runner 全局函数 + executeAgent 子会话生命周期 + executeTool 传 session"
```

---

### Task 4: P1-2 WorkflowOutput 和 WorkflowExecutionState 类型定义

**Files:**
- Create: `internal/agentcore/workflow/base.go`
- Create: `internal/agentcore/workflow/base_test.go`
- Create: `internal/agentcore/workflow/doc.go`

- [ ] **Step 1: 创建 workflow/base.go**

```go
package workflow

// ──────────────────────────── 结构体 ────────────────────────────

// WorkflowOutput 工作流执行结果。
// 对应 Python: openjiuwen/core/workflow/base.py WorkflowOutput(BaseModel)
type WorkflowOutput struct {
    // Result 输出数据
    Result any
    // State 执行状态
    State WorkflowExecutionState
}

// ──────────────────────────── 枚举 ────────────────────────────

// WorkflowExecutionState 工作流执行状态。
// 对应 Python: openjiuwen/core/workflow/base.py WorkflowExecutionState(str, Enum)
type WorkflowExecutionState string

const (
    // WorkflowExecutionStateCompleted 执行完成
    WorkflowExecutionStateCompleted WorkflowExecutionState = "COMPLETED"
    // WorkflowExecutionStateInputRequired 需要用户输入（中断）
    WorkflowExecutionStateInputRequired WorkflowExecutionState = "INPUT_REQUIRED"
    // WorkflowExecutionStateError 执行出错
    WorkflowExecutionStateError WorkflowExecutionState = "ERROR"
)
```

- [ ] **Step 2: 创建 workflow/doc.go**

```go
// Package workflow 提供工作流执行的基础类型定义。
//
// 对齐 Python: openjiuwen/core/workflow/base.py
//
// 文件目录：
//
//	workflow/
//	├── doc.go                # 包文档
//	└── base.go               # WorkflowOutput + WorkflowExecutionState
//
// 对应 Python 代码：openjiuwen/core/workflow/base.py
package workflow
```

- [ ] **Step 3: 创建 workflow/base_test.go**

```go
package workflow

import (
    "testing"

    "github.com/stretchr/testify/assert"
)

func TestWorkflowExecutionState_常量值(t *testing.T) {
    assert.Equal(t, WorkflowExecutionState("COMPLETED"), WorkflowExecutionStateCompleted)
    assert.Equal(t, WorkflowExecutionState("INPUT_REQUIRED"), WorkflowExecutionStateInputRequired)
    assert.Equal(t, WorkflowExecutionState("ERROR"), WorkflowExecutionStateError)
}

func TestWorkflowOutput_字段赋值(t *testing.T) {
    wo := WorkflowOutput{
        Result: map[string]any{"key": "value"},
        State:  WorkflowExecutionStateCompleted,
    }
    assert.Equal(t, WorkflowExecutionStateCompleted, wo.State)
    assert.NotNil(t, wo.Result)
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/workflow/... -v`

Expected: ALL PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/workflow/base.go internal/agentcore/workflow/base_test.go internal/agentcore/workflow/doc.go
git commit -m "feat(workflow): 新增 WorkflowOutput 和 WorkflowExecutionState 类型定义"
```

---

### Task 5: P1-1+P1-3 WorkflowOptions 扩展 + executeWorkflow 改造

**Files:**
- Modify: `internal/agentcore/single_agent/interfaces/interface.go`
- Modify: `internal/agentcore/single_agent/ability/ability_manager.go`

- [ ] **Step 1: 扩展 WorkflowOptions**

在 `interfaces/interface.go` 中：

将 `WorkflowOptions` 从空结构体改为：

```go
// WorkflowOptions 工作流执行选项。
type WorkflowOptions struct {
    // Session 工作流会话（对齐 Python workflow.invoke(inputs, session=...)）
    Session *session.WorkflowSession
    // Context 模型上下文，待领域八具体化（对齐 Python workflow.invoke(inputs, context=...)）
    Context any
}

// WithWorkflowSession 设置工作流会话。
func WithWorkflowSession(sess *session.WorkflowSession) WorkflowOption {
    return func(o *WorkflowOptions) { o.Session = sess }
}

// WithWorkflowContext 设置模型上下文。
func WithWorkflowContext(ctx any) WorkflowOption {
    return func(o *WorkflowOptions) { o.Context = ctx }
}
```

新增 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/session"`。

同时新增 `NewWorkflowOptions` 函数：

```go
// NewWorkflowOptions 从选项列表构建 WorkflowOptions。
func NewWorkflowOptions(opts ...WorkflowOption) *WorkflowOptions {
    o := &WorkflowOptions{}
    for _, opt := range opts {
        opt(o)
    }
    return o
}
```

- [ ] **Step 2: 修改 ability_manager.go 中的 executeWorkflow**

对照 spec 中 P1-1 的步骤 1-8 改造 executeWorkflow：
- 步骤 3: 创建 workflow session (`sess.CreateWorkflowSession()`)
- 步骤 4: 创建隔离 context (`am.contextEngine.CreateContext()`)
- 步骤 5: 通过 `runner.RunWorkflow()` 执行
- 步骤 6: 检测 `WorkflowOutput.INPUT_REQUIRED` 中断，返回 `ExecuteResult{ToolMsg: nil}`
- 步骤 7: 正常完成时从 `WorkflowOutput` 提取 `.Result`
- 步骤 8: 用 `BuildToolMessageContent(actualResult)` 构建 ToolMessage

需要新增 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow"` 和 `"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"`。

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/single_agent/ability/... ./internal/agentcore/single_agent/interfaces/... -v`

Expected: ALL PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/single_agent/interfaces/interface.go internal/agentcore/single_agent/ability/ability_manager.go
git commit -m "feat(ability): WorkflowOptions 扩展 + executeWorkflow 增加 session/context 隔离和中断检测"
```

---

### Task 6: 全量编译验证

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && go build ./...`

Expected: BUILD SUCCESS

- [ ] **Step 2: 全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/... -count=1`

Expected: ALL PASS

- [ ] **Step 3: 提交最终状态（如有遗漏修复）**

```bash
git add -A
git commit -m "chore: AM 执行逻辑对齐 Python 全量编译验证通过"
```
