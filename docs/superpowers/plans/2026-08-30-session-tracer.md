# 5.11 Session Tracer 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现会话追踪系统，记录 Agent/Workflow 执行过程中的每一步调用轨迹，并通过 StreamWriter 实时推送给客户端。

**Architecture:** 单包 `session/tracer/` 下 7 个文件按职责拆分（data/span/tracer/handler/decorator/workflow），与 Python `openjiuwen/core/session/tracer/` 一一对应。Tracer 核心通过 map 分发表实现 TriggerAgent/TriggerWorkflow 两个方法分发到 Handler。装饰器用包装器结构体替代 Python 的 `_TraceProxy`。回填已有代码中 `any` 类型为 `*Tracer`，取消注释 tracer 自动创建逻辑。

**Tech Stack:** Go 1.22+, 已有的 `session/stream` 包（StreamWriterManager/StreamWriter/TraceSchema）, `common/logger` 包

---

## 文件结构

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 创建 | `session/tracer/doc.go` | 包文档 |
| 创建 | `session/tracer/data.go` | InvokeType/NodeStatus/TraceEvent 枚举 |
| 创建 | `session/tracer/span.go` | Span/TraceAgentSpan/TraceWorkflowSpan/SpanManager |
| 创建 | `session/tracer/tracer.go` | Tracer 核心 + TriggerParams |
| 创建 | `session/tracer/handler.go` | TraceBaseHandler/TraceAgentHandler/TraceWorkflowHandler |
| 创建 | `session/tracer/decorator.go` | TracedModelClient/TracedTool/TracedWorkflow + DecorateXxx 函数 |
| 创建 | `session/tracer/workflow.go` | TracerWorkflowUtils + BaseWorkflowSession 接口 |
| 创建 | `session/tracer/data_test.go` | 枚举测试 |
| 创建 | `session/tracer/span_test.go` | Span/SpanManager 测试 |
| 创建 | `session/tracer/tracer_test.go` | Tracer 核心测试 |
| 创建 | `session/tracer/handler_test.go` | Handler 测试 |
| 创建 | `session/tracer/decorator_test.go` | 装饰器测试 |
| 创建 | `session/tracer/workflow_test.go` | TracerWorkflowUtils 测试 |
| 修改 | `session/interfaces/interfaces.go` | Tracer() any → Tracer() *tracer.Tracer |
| 修改 | `session/internal/agent_session.go` | tracer any → *tracer.Tracer + 取消注释 |
| 修改 | `session/internal/workflow_session.go` | tracer any → *tracer.Tracer |
| 修改 | `session/node.go` | Trace/TraceError 桩 → 真实逻辑 |
| 修改 | `session/wrapper.go` | 无需改动（委托链已通） |
| 修改 | `session/doc.go` | 增加 tracer/ 子包描述 |
| 修改 | `session/session.go` | ProxySession.Tracer() 返回类型 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 5.11 状态 ☐→✅ |

---

### Task 1: data.go — 枚举定义

**Files:**
- Create: `internal/agentcore/session/tracer/data.go`
- Test: `internal/agentcore/session/tracer/data_test.go`

- [ ] **Step 1: 创建 data.go**

```go
package tracer

// ──────────────────────────── 枚举 ────────────────────────────

// InvokeType 调用类型枚举，对应 Python InvokeType。
type InvokeType string

const (
	// InvokeTypePrompt 提示词调用
	InvokeTypePrompt InvokeType = "prompt"
	// InvokeTypeLLM LLM 调用
	InvokeTypeLLM InvokeType = "llm"
	// InvokeTypePlugin 插件调用
	InvokeTypePlugin InvokeType = "plugin"
	// InvokeTypeWorkflow 工作流调用
	InvokeTypeWorkflow InvokeType = "workflow"
	// InvokeTypeChain 链式调用
	InvokeTypeChain InvokeType = "chain"
	// InvokeTypeRetriever 检索调用
	InvokeTypeRetriever InvokeType = "retriever"
	// InvokeTypeEvaluator 评估调用
	InvokeTypeEvaluator InvokeType = "evaluator"
)

// NodeStatus 节点状态枚举，对应 Python NodeStatus。
type NodeStatus string

const (
	// NodeStatusStart 开始
	NodeStatusStart NodeStatus = "start"
	// NodeStatusFinish 完成
	NodeStatusFinish NodeStatus = "finish"
	// NodeStatusRunning 运行中
	NodeStatusRunning NodeStatus = "running"
	// NodeStatusInterrupted 已中断
	NodeStatusInterrupted NodeStatus = "interrupted"
	// NodeStatusError 错误
	NodeStatusError NodeStatus = "error"
)

// TraceEvent 追踪事件枚举，替代 Python 的字符串反射分发。
type TraceEvent string

const (
	// ─── Agent 事件（由装饰器触发） ───

	// TraceChainStart 链式调用开始
	TraceChainStart TraceEvent = "on_chain_start"
	// TraceChainEnd 链式调用结束
	TraceChainEnd TraceEvent = "on_chain_end"
	// TraceChainError 链式调用错误
	TraceChainError TraceEvent = "on_chain_error"
	// TraceLLMStart LLM 调用开始
	TraceLLMStart TraceEvent = "on_llm_start"
	// TraceLLMRequest LLM 请求详情
	TraceLLMRequest TraceEvent = "on_llm_request"
	// TraceLLMEnd LLM 调用结束
	TraceLLMEnd TraceEvent = "on_llm_end"
	// TraceLLMError LLM 调用错误
	TraceLLMError TraceEvent = "on_llm_error"
	// TracePromptStart 提示词调用开始
	TracePromptStart TraceEvent = "on_prompt_start"
	// TracePromptEnd 提示词调用结束
	TracePromptEnd TraceEvent = "on_prompt_end"
	// TracePromptError 提示词调用错误
	TracePromptError TraceEvent = "on_prompt_error"
	// TracePluginStart 插件调用开始
	TracePluginStart TraceEvent = "on_plugin_start"
	// TracePluginEnd 插件调用结束
	TracePluginEnd TraceEvent = "on_plugin_end"
	// TracePluginError 插件调用错误
	TracePluginError TraceEvent = "on_plugin_error"
	// TraceRetrieverStart 检索调用开始
	TraceRetrieverStart TraceEvent = "on_retriever_start"
	// TraceRetrieverEnd 检索调用结束
	TraceRetrieverEnd TraceEvent = "on_retriever_end"
	// TraceRetrieverError 检索调用错误
	TraceRetrieverError TraceEvent = "on_retriever_error"
	// TraceEvaluatorStart 评估调用开始
	TraceEvaluatorStart TraceEvent = "on_evaluator_start"
	// TraceEvaluatorEnd 评估调用结束
	TraceEvaluatorEnd TraceEvent = "on_evaluator_end"
	// TraceEvaluatorError 评估调用错误
	TraceEvaluatorError TraceEvent = "on_evaluator_error"
	// TraceWorkflowStart 工作流调用开始（Agent 层视角）
	TraceWorkflowStart TraceEvent = "on_workflow_start"
	// TraceWorkflowEnd 工作流调用结束（Agent 层视角）
	TraceWorkflowEnd TraceEvent = "on_workflow_end"
	// TraceWorkflowError 工作流调用错误（Agent 层视角）
	TraceWorkflowError TraceEvent = "on_workflow_error"

	// ─── Workflow 事件（由 TracerWorkflowUtils 触发） ───

	// TraceWFCallStart 组件调用开始
	TraceWFCallStart TraceEvent = "on_call_start"
	// TraceWFPreInvoke 组件预调用
	TraceWFPreInvoke TraceEvent = "on_pre_invoke"
	// TraceWFPreStream 组件预流式
	TraceWFPreStream TraceEvent = "on_pre_stream"
	// TraceWFInvoke 组件调用中（运行时数据/错误）
	TraceWFInvoke TraceEvent = "on_invoke"
	// TraceWFPostStream 组件后流式
	TraceWFPostStream TraceEvent = "on_post_stream"
	// TraceWFPostInvoke 组件后调用
	TraceWFPostInvoke TraceEvent = "on_post_invoke"
	// TraceWFCallDone 组件调用完成
	TraceWFCallDone TraceEvent = "on_call_done"
	// TraceWFInteract 组件交互
	TraceWFInteract TraceEvent = "on_interact"
)
```

- [ ] **Step 2: 创建 data_test.go**

```go
package tracer

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

// TestInvokeType_值对齐 验证 InvokeType 枚举值与 Python 一致
func TestInvokeType_值对齐(t *testing.T) {
	expected := map[InvokeType]string{
		InvokeTypePrompt:    "prompt",
		InvokeTypeLLM:       "llm",
		InvokeTypePlugin:    "plugin",
		InvokeTypeWorkflow:  "workflow",
		InvokeTypeChain:     "chain",
		InvokeTypeRetriever: "retriever",
		InvokeTypeEvaluator: "evaluator",
	}
	for typ, val := range expected {
		if string(typ) != val {
			t.Errorf("InvokeType 值不匹配: got %q, want %q", typ, val)
		}
	}
}

// TestNodeStatus_值对齐 验证 NodeStatus 枚举值与 Python 一致
func TestNodeStatus_值对齐(t *testing.T) {
	expected := map[NodeStatus]string{
		NodeStatusStart:       "start",
		NodeStatusFinish:      "finish",
		NodeStatusRunning:     "running",
		NodeStatusInterrupted: "interrupted",
		NodeStatusError:       "error",
	}
	for typ, val := range expected {
		if string(typ) != val {
			t.Errorf("NodeStatus 值不匹配: got %q, want %q", typ, val)
		}
	}
}

// TestTraceEvent_Agent事件完整性 验证 Agent 事件共 21 种
func TestTraceEvent_Agent事件完整性(t *testing.T) {
	agentEvents := []TraceEvent{
		TraceChainStart, TraceChainEnd, TraceChainError,
		TraceLLMStart, TraceLLMRequest, TraceLLMEnd, TraceLLMError,
		TracePromptStart, TracePromptEnd, TracePromptError,
		TracePluginStart, TracePluginEnd, TracePluginError,
		TraceRetrieverStart, TraceRetrieverEnd, TraceRetrieverError,
		TraceEvaluatorStart, TraceEvaluatorEnd, TraceEvaluatorError,
		TraceWorkflowStart, TraceWorkflowEnd, TraceWorkflowError,
	}
	if len(agentEvents) != 21 {
		t.Errorf("Agent 事件数量: got %d, want 21", len(agentEvents))
	}
}

// TestTraceEvent_Workflow事件完整性 验证 Workflow 事件共 8 种
func TestTraceEvent_Workflow事件完整性(t *testing.T) {
	wfEvents := []TraceEvent{
		TraceWFCallStart, TraceWFPreInvoke, TraceWFPreStream,
		TraceWFInvoke, TraceWFPostStream, TraceWFPostInvoke,
		TraceWFCallDone, TraceWFInteract,
	}
	if len(wfEvents) != 8 {
		t.Errorf("Workflow 事件数量: got %d, want 8", len(wfEvents))
	}
}
```

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/tracer/... -run "TestInvokeType|TestNodeStatus|TestTraceEvent" -v`
Expected: 全部 PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/tracer/data.go internal/agentcore/session/tracer/data_test.go
git commit -m "feat(tracer): 添加 InvokeType/NodeStatus/TraceEvent 枚举定义"
```

---

### Task 2: span.go — Span 体系与 SpanManager

**Files:**
- Create: `internal/agentcore/session/tracer/span.go`
- Test: `internal/agentcore/session/tracer/span_test.go`

- [ ] **Step 1: 创建 span.go** — 包含 Span/TraceAgentSpan/TraceWorkflowSpan/SpanManager 完整实现，所有字段 json tag 使用 camelCase，TraceWorkflowSpan.LLMInvokeData 使用 `json:"-"` exclude。SpanManager 实现 GetSpan/PopSpan/CreateAgentSpan/CreateWorkflowSpan/UpdateSpan/LastSpan。

- [ ] **Step 2: 创建 span_test.go** — 覆盖 Span.Update、AppendChildInvokeID、TraceAgentSpan JSON 序列化（camelCase 验证）、TraceWorkflowSpan JSON 序列化（LLMInvokeData exclude 验证）、AppendStreamOutput/AppendStreamInputs、SpanManager 全部方法。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/tracer/... -run "TestSpan|TestTraceAgentSpan|TestTraceWorkflowSpan|TestSpanManager" -v`
Expected: 全部 PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/tracer/span.go internal/agentcore/session/tracer/span_test.go
git commit -m "feat(tracer): 添加 Span 体系与 SpanManager"
```

---

### Task 3: handler.go — TraceBaseHandler/TraceAgentHandler/TraceWorkflowHandler

**Files:**
- Create: `internal/agentcore/session/tracer/handler.go`
- Test: `internal/agentcore/session/tracer/handler_test.go`

- [ ] **Step 1: 创建 handler.go** — 包含 traceBaseHandler（EmitStreamWriter/GetElapsedTime/GetNodeStatus/FormatData 抽象）、TraceAgentHandler（21 个 OnXxx 方法 + 4 个 update 辅助方法 + FormatData）、TraceWorkflowHandler（8 个 OnXxx 方法 + FormatData）。日志：updateStartTraceData 中 json.Marshal 失败记 Error 日志，EmitStreamWriter 写入失败记 Error 日志。Handler 方法签名带 context.Context。

- [ ] **Step 2: 创建 handler_test.go** — 用 mock StreamWriter 验证：TraceAgentHandler 的 OnLLMStart/OnLLMRequest/OnLLMEnd/OnLLMError（span 字段 + 写入验证）、OnPluginStart/End/Error、updateStartTraceData Marshal 失败、GetElapsedTime 毫秒/秒分支、GetNodeStatus 各状态、FormatData 输出结构。TraceWorkflowHandler 的 OnCallStart（needSend 控制）、OnPreInvoke、OnInvoke 正常/异常/BaseError/GraphInterrupt/InnerError 各分支、OnPostInvoke、OnPostStream、OnCallDone、OnInteract、FormatData。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/tracer/... -run "TestTraceAgent|TestTraceWorkflow|TestTraceBase" -v`
Expected: 全部 PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/tracer/handler.go internal/agentcore/session/tracer/handler_test.go
git commit -m "feat(tracer): 添加 TraceAgentHandler/TraceWorkflowHandler"
```

---

### Task 4: tracer.go — Tracer 核心 + TriggerParams

**Files:**
- Create: `internal/agentcore/session/tracer/tracer.go`
- Test: `internal/agentcore/session/tracer/tracer_test.go`

- [ ] **Step 1: 创建 tracer.go** — 包含 Tracer 结构体（AgentSpanManager/WorkflowSpanManagerDict/agentDispatch/workflowDispatch/streamWriterManager）、NewTracer/Init/TriggerAgent/TriggerWorkflow/RegisterWorkflowSpanManager/GetWorkflowSpan/PopWorkflowSpan。Init 中创建 TraceAgentHandler + TraceWorkflowHandler，构建 agentDispatch map（21 个 Agent 事件）和 workflowDispatch map（8 个 Workflow 事件，按 parentID 分组）。TriggerAgent/TriggerWorkflow 内部 map 查表分发，找不到 handler 时 Warn 日志。TriggerParams 结构体含 Span/Inputs/Outputs/Error/InstanceInfo/InvokeID/Metadata/SourceIDs/NeedSend/OnInvokeData/Chunk/ComponentMetadata。

- [ ] **Step 2: 创建 tracer_test.go** — 用 mock StreamWriterManager 验证：NewTracer traceID 自动生成、Init 分发表初始化、TriggerAgent 各事件分发（LLMStart/End/Error + 未注册事件 Warn）、TriggerWorkflow 各事件分发（带 parentNodeID）、RegisterWorkflowSpanManager 新 SpanManager 注册、GetWorkflowSpan/PopWorkflowSpan。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/tracer/... -run "TestNewTracer|TestTracer_" -v`
Expected: 全部 PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/tracer/tracer.go internal/agentcore/session/tracer/tracer_test.go
git commit -m "feat(tracer): 添加 Tracer 核心与 TriggerAgent/TriggerWorkflow 分发"
```

---

### Task 5: decorator.go — 包装器与装饰函数

**Files:**
- Create: `internal/agentcore/session/tracer/decorator.go`
- Test: `internal/agentcore/session/tracer/decorator_test.go`

- [ ] **Step 1: 创建 decorator.go** — 包含 TracedModelClient（实现 model_clients.BaseModelClient，Invoke/Stream 加追踪，其他方法委托 inner）、TracedTool（实现 tool.Tool，Invoke 加追踪，Stream/Card 委托 inner）、TracedWorkflow（最小 WorkflowInterface 占位，Invoke/Stream 加追踪）、DecorateModelWithTrace/DecorateToolWithTrace/DecorateWorkflowWithTrace 三个装饰函数（session 有 tracer+span 返回包装器，否则返回原始对象）。TracedModelClient.Invoke 内部：CreateAgentSpan → TriggerAgent(Start) → inner.Invoke → TriggerAgent(End/Error)。TracedModelClient.Stream 内部：CreateAgentSpan → TriggerAgent(Start) → inner.Stream 收集结果 → TriggerAgent(End/Error)。

- [ ] **Step 2: 创建 decorator_test.go** — 用 fakeModelClient/fakeTool/fakeWorkflowSession 验证：TracedModelClient Invoke 成功/失败、Stream 成功、GenerateImage 直接委托。TracedTool Invoke 成功/失败、Card/Stream 委托。DecorateModelWithTrace 有/无 Tracer、DecorateToolWithTrace 有/无 Tracer、DecorateWorkflowWithTrace 有/无 Tracer。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/tracer/... -run "TestTraced|TestDecorate" -v`
Expected: 全部 PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/tracer/decorator.go internal/agentcore/session/tracer/decorator_test.go
git commit -m "feat(tracer): 添加 TracedModelClient/TracedTool/TracedWorkflow 包装器"
```

---

### Task 6: workflow.go — TracerWorkflowUtils + BaseWorkflowSession

**Files:**
- Create: `internal/agentcore/session/tracer/workflow.go`
- Test: `internal/agentcore/session/tracer/workflow_test.go`

- [ ] **Step 1: 创建 workflow.go** — 包含 BaseWorkflowSession 最小接口（Tracer/ExecutableID/ParentID/WorkflowID/NodeID/NodeType/State/Config）、TracerWorkflowUtils 结构体及 11 个静态方法（TraceWorkflowStart/TraceComponentBegin/TraceComponentInputs/TraceComponentStreamInput/TraceComponentOutputs/TraceComponentStreamOutput/TraceWorkflowDone/TraceComponentDone/Trace/TraceError/TraceComponentInteractiveInputs）。每个方法内部：tracer 为 nil 时静默返回，否则调 TriggerWorkflow。TraceComponentStreamInput 中 chunk 为 string 时跳过（与 Python 一致）。_getWorkflowMetadata/_getComponentMetadata 辅助函数。

- [ ] **Step 2: 创建 workflow_test.go** — 用 fakeWorkflowSession 验证：TraceWorkflowStart/Done 参数传递、TraceComponentBegin/Inputs/Outputs/Done 完整生命周期、Trace/TraceError 参数传递、Tracer 为 nil 静默返回、TraceComponentStreamInput chunk 为 string 时跳过、TraceComponentStreamOutput、TraceComponentInteractiveInputs。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/tracer/... -run "TestTracerWorkflow" -v`
Expected: 全部 PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/tracer/workflow.go internal/agentcore/session/tracer/workflow_test.go
git commit -m "feat(tracer): 添加 TracerWorkflowUtils 与 BaseWorkflowSession 接口"
```

---

### Task 7: doc.go — 包文档

**Files:**
- Create: `internal/agentcore/session/tracer/doc.go`

- [ ] **Step 1: 创建 doc.go** — 包含包功能概述（会话追踪，记录 Agent/Workflow 执行轨迹，通过 StreamWriter 实时推送）、文件目录树（7 个 .go 文件）、对应 Python 代码路径、核心类型索引（Tracer/SpanManager/TraceAgentHandler/TraceWorkflowHandler/TracerWorkflowUtils/InvokeType/NodeStatus/TraceEvent）。

- [ ] **Step 2: 提交**

```bash
git add internal/agentcore/session/tracer/doc.go
git commit -m "feat(tracer): 添加包文档"
```

---

### Task 8: 回填 — interfaces/agent_session/workflow_session 类型更新

**Files:**
- Modify: `internal/agentcore/session/interfaces/interfaces.go`
- Modify: `internal/agentcore/session/internal/agent_session.go`
- Modify: `internal/agentcore/session/internal/workflow_session.go`

- [ ] **Step 1: 修改 interfaces.go** — `Tracer() any` → `Tracer() *tracer.Tracer`，添加 `tracer` 包 import，删除 ⤵️ 5.11 回填注释。

- [ ] **Step 2: 修改 agent_session.go** — `tracer any` → `tracer *tracer.Tracer`，`WithTracer(tracer any)` → `WithTracer(t *tracer.Tracer)`，`Tracer() any` → `Tracer() *tracer.Tracer`，`agentSpan any` → `agentSpan *TraceAgentSpan`，`WithAgentSpan(span any)` → `WithAgentSpan(span *TraceAgentSpan)`，`AgentSpan() any` → `AgentSpan() *TraceAgentSpan`。取消注释 tracer 自动创建逻辑（`s.tracer = tracer.NewTracer()` + `s.tracer.Init(s.streamWriterManager)` + `s.agentSpan = s.tracer.AgentSpanManager.CreateAgentSpan()`），添加 `tracer` 包 import。

- [ ] **Step 3: 修改 workflow_session.go** — `tracer any` → `tracer *tracer.Tracer`，`SetTracer(tracer any)` → `SetTracer(t *tracer.Tracer)`，`Tracer() any` → `Tracer() *tracer.Tracer`，添加 `tracer` 包 import。NodeSession 的 `Tracer() any` → `Tracer() *tracer.Tracer`（委托给 delegate.Tracer()，需类型断言或直接返回）。

- [ ] **Step 4: 修改 session.go** — ProxySession 的 `Tracer() any` → `Tracer() *tracer.Tracer`。

- [ ] **Step 5: 运行编译检查**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/...`
Expected: 编译通过

- [ ] **Step 6: 运行已有测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/... -v -count=1 2>&1 | head -100`
Expected: 全部 PASS（修复因类型变更导致的测试编译错误）

- [ ] **Step 7: 修复测试编译错误** — 更新所有测试中的 fakeBaseSession/fakeSession 等的 `Tracer() any` → `Tracer() *tracer.Tracer`，mockStub 的 tracerVal 类型，以及 `WithTracer("test_tracer")` 等 string 类型的 tracer 参数改为 `tracer.NewTracer()` 或 nil。

- [ ] **Step 8: 提交**

```bash
git add -A internal/agentcore/session/
git commit -m "feat(tracer): 回填 Tracer 返回类型从 any 到 *tracer.Tracer"
```

---

### Task 9: 回填 — node.go/wrapper.go 真实逻辑

**Files:**
- Modify: `internal/agentcore/session/node.go`
- Modify: `internal/agentcore/session/wrapper.go`

- [ ] **Step 1: 修改 node.go Trace/TraceError** — `Trace()` 方法：删除桩注释，添加 `return tracer.TracerWorkflowUtils{}.Trace(ctx, f.inner, data)` 调用。`TraceError()` 方法：删除桩注释，添加 `return tracer.TracerWorkflowUtils{}.TraceError(ctx, f.inner, err)` 调用。添加 `tracer` 包 import。需确保 NodeSession 满足 `tracer.BaseWorkflowSession` 接口（Tracer/ExecutableID/ParentID/WorkflowID/NodeID/NodeType/State/Config），如果不满足需在 NodeSession 上补充缺失方法或在 tracer 包中调整接口。

- [ ] **Step 2: wrapper.go 无需修改** — Trace/TraceError 已经委托给 inner（NodeSessionFacade），回填后自动走真实逻辑。

- [ ] **Step 3: 运行编译 + 测试**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/... && go test ./internal/agentcore/session/... -v -count=1 2>&1 | head -100`
Expected: 编译通过 + 全部 PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/node.go internal/agentcore/session/wrapper.go
git commit -m "feat(tracer): 回填 NodeSessionFacade.Trace/TraceError 真实逻辑"
```

---

### Task 10: 回填集成测试 + doc.go 更新 + IMPLEMENTATION_PLAN.md 状态更新

**Files:**
- Modify: `internal/agentcore/session/internal/agent_session_test.go`
- Modify: `internal/agentcore/session/internal/workflow_session_test.go`
- Modify: `internal/agentcore/session/node_test.go`
- Modify: `internal/agentcore/session/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 添加回填集成测试** — 在 `agent_session_test.go` 中添加 `TestAgentSession_Tracer自动初始化`（创建 AgentSession 后 tracer 非 nil、AgentSpanManager 非 nil）、`TestAgentSession_AgentSpan自动创建`（tracer 非空时 agentSpan 非 nil）。在 `node_test.go` 中更新 `TestNodeSessionFacade_Trace_走真实逻辑`（SkipTrace=false 时 tracer 触发 TraceWFInvoke，验证 StreamWriter 收到追踪数据）、`TestNodeSessionFacade_TraceError_走真实逻辑`。

- [ ] **Step 2: 更新 session/doc.go** — 增加 `tracer/` 子包描述条目。

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md** — 将 5.11 行的 `☐` 改为 `✅`，将 5.2/5.3/5.4/5.5 行中的 `⤵️ 5.11 回填` 标记改为 `✅ 5.11 已回填`。

- [ ] **Step 4: 运行全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/... -cover -count=1`
Expected: 全部 PASS，覆盖率 ≥ 85%

- [ ] **Step 5: 提交**

```bash
git add -A
git commit -m "feat(tracer): 回填集成测试 + doc.go + IMPLEMENTATION_PLAN 状态更新"
```

---

## 自审清单

**1. 规格覆盖率：** 设计文档中所有章节（data/span/tracer/handler/decorator/workflow/日志/测试/回填）均有对应 Task 覆盖。

**2. 占位符扫描：** 无 TBD/TODO/类似TaskN 等占位符。每个 Task 的代码步骤都有具体描述。

**3. 类型一致性：** Tracer() 返回类型在所有文件中统一为 `*tracer.Tracer`，agentSpan 类型为 `*TraceAgentSpan`，BaseWorkflowSession 接口方法签名与 NodeSession 实际方法对齐。
