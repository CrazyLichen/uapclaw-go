# 8.17 StartComp / EndComp 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 落地 8.17——工作流组件基类体系（ComponentExecutable/WorkflowComponent）+ Start/End 组件全家（含模板渲染状态机）+ CallbackFramework 组件事件与 TransformIO 扩展。

**Architecture:** 组件作者面 `WorkflowComponent`（函数槽四钩子，非泛型具体化 I=O=map[string]any 对齐 Python Any）嵌入 graph 层 `BaseExecutable`；四钩子内做 WorkflowVertexSession 校验 → `session.NewComponentSession` 包装 → Trigger 序列（emit_before/transform_io/emit_after 的 Go 惯用法）；End 组件承载模板分段渲染状态机（mutex + notify channel 广播替代 asyncio.Condition）。无被替代旧实现。

**Tech Stack:** Go 1.22+ 泛型接口（graph.Executable 已有）、sync.Mutex/channel 广播、regexp RE2、runner/callback 域。

**Spec:** docs/superpowers/specs/2026-11-19-workflow-flow-components-8.17-design.md（偏差 D-17-1~6，其中 D-17-6 已关闭）

**Python 源:** `/home/opensource/agent-core/openjiuwen/core/workflow/components/{component.py,base.py,flow/start_comp.py,flow/end_comp.py}`

---

### Task 1: CallbackFramework 组件事件补全 + TransformWorkflowIO 族

**Files:**
- Modify: `internal/agentcore/runner/callback/events.go`（WorkflowEventType 常量块，ComponentStreamInput 后）
- Modify: `internal/agentcore/runner/callback/framework.go`（TransformWorkflowIO 方法）
- Modify: `internal/agentcore/runner/callback/models.go`（TransformWorkflowIOFunc 类型）
- Test: `internal/agentcore/runner/callback/framework_test.go`

- [ ] **Step 1: 写失败测试**

在 `framework_test.go` 追加：

```go
// TestTransformWorkflowIO_无回调原样返回 验证未注册转换函数时输入输出原样透传。
func TestTransformWorkflowIO_无回调原样返回(t *testing.T) {
	fw := NewCallbackFramework()
	got := fw.TransformWorkflowIOInput(context.Background(), ComponentBatchInput, map[string]any{"k": 1})
	if got == nil || got.(map[string]any)["k"] != 1 {
		t.Fatalf("期望原样返回输入, got %v", got)
	}
}

// TestTransformWorkflowIO_注册后改写 验证注册转换函数后输入被改写。
func TestTransformWorkflowIO_注册后改写(t *testing.T) {
	fw := NewCallbackFramework()
	RegisterWorkflowTransformIO(&fwOpts{fw}, ComponentBatchInput, ComponentBatchOutput,
		func(ctx context.Context, event WorkflowEventType, input any) any {
			return map[string]any{"transformed": true}
		}, nil)
	got := fw.TransformWorkflowIOInput(context.Background(), ComponentBatchInput, map[string]any{})
	if m, ok := got.(map[string]any); !ok || !m["transformed"].(bool) {
		t.Fatalf("期望改写后的输入, got %v", got)
	}
}
```

（`fwOpts` 为测试期框架持有方式；若仓内已有拿 `*CallbackFramework` 的测试惯例则照抄。）

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/agentcore/runner/callback/ -run TestTransformWorkflowIO -v
```
预期：编译失败 `fw.TransformWorkflowIOInput undefined`。

- [ ] **Step 3: 最小实现**

events.go：在 `ComponentStreamInput` 常量后追加：

```go
	// ComponentStreamOutput 组件流式输出
	ComponentStreamOutput WorkflowEventType = "_framework:component_stream_output"
```

models.go：在 TransformAgentIO* 类型附近追加：

```go
// TransformWorkflowIOInputFunc Workflow 层输入变换回调函数类型。
// Python: transform_io 的 input_event（COMPONENT_BATCH_INPUT / COMPONENT_STREAM_INPUT）
type TransformWorkflowIOInputFunc func(ctx context.Context, event WorkflowEventType, input any) any

// TransformWorkflowIOOutputFunc Workflow 层输出变换回调函数类型。
// Python: transform_io 的 output_event（COMPONENT_BATCH_OUTPUT / COMPONENT_STREAM_OUTPUT）
type TransformWorkflowIOOutputFunc func(ctx context.Context, event WorkflowEventType, output any) any
```

framework.go：仿 `RegisterAgentTransformIO`（framework.go:670+）——先看该函数与其存储结构（`agentTransformIO map[...]...entry{inputFn,outputFn}`）的完整形状，然后同款新增：

```go
// workflowTransformIO Workflow 层输入输出转换函数注册表
// （结构对齐 agentTransformIO：map[WorkflowEventType]*transformIOEntry 按输入事件键存）
workflowTransformIO map[WorkflowEventType]*transformIOEntry
```

（`transformIOEntry` 若 LLM/Agent 共用同一匿名结构则直接复用；否则定义 `{inputFn TransformWorkflowIOInputFunc; outputFn TransformWorkflowIOOutputFunc}`。）

```go
// RegisterWorkflowTransformIO 注册 Workflow 组件调用前后的输入输出转换函数。
// Python: transform_io(input_event=..., output_event=...)（component.py:32-35）
func (fw *CallbackFramework) RegisterWorkflowTransformIO(
	inputEvent WorkflowEventType,
	outputEvent WorkflowEventType,
	inputFn TransformWorkflowIOInputFunc,
	outputFn TransformWorkflowIOOutputFunc,
) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.workflowTransformIO[inputEvent] = &transformIOEntry{
		inputFn:  inputFn,
		outputFn: outputFn,
	}
}

// TransformWorkflowIOInput 用已注册的转换函数加工组件调用的输入（无注册原样返回）。
func (fw *CallbackFramework) TransformWorkflowIOInput(ctx context.Context, event WorkflowEventType, input any) any {
	fw.mu.RLock()
	entry, ok := fw.workflowTransformIO[event]
	fw.mu.RUnlock()
	if !ok || entry.inputFn == nil {
		return input
	}
	return entry.inputFn(ctx, event, input)
}

// TransformWorkflowIOOutput 用已注册的转换函数加工组件调用的输出（无注册原样返回）。
func (fw *CallbackFramework) TransformWorkflowIOOutput(ctx context.Context, event WorkflowEventType, output any) any {
	fw.mu.RLock()
	entry, ok := fw.workflowTransformIO[event]
	fw.mu.RUnlock()
	if !ok || entry.outputFn == nil {
		return output
	}
	return entry.outputFn(ctx, event, output)
}
```

注意：`NewCallbackFramework` 初始化处补 `workflowTransformIO: make(map[WorkflowEventType]*transformIOEntry)`；输出事件键需按 outputEvent 也存一份映射（对齐 Agent 版实现细节，实施时照抄其键控方式）。

- [ ] **Step 4: 跑测试确认通过**

```bash
go test ./internal/agentcore/runner/callback/ -run TestTransformWorkflowIO -v
```
预期：PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/runner/callback/events.go internal/agentcore/runner/callback/framework.go internal/agentcore/runner/callback/models.go internal/agentcore/runner/callback/framework_test.go
git commit -m "feat(callback): Workflow 组件事件 ComponentStreamOutput + TransformWorkflowIO 族（8.17 前置）"
```

---

### Task 2: components/base.go 补三符号

**Files:**
- Modify: `internal/agentcore/workflow/components/base.go`
- Test: `internal/agentcore/workflow/components/base_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestWorkflowComponentMetadata_字段 验证元数据结构字段可读写。
func TestWorkflowComponentMetadata_字段(t *testing.T) {
	m := WorkflowComponentMetadata{NodeID: "n1", NodeType: "End", NodeName: "结束"}
	if m.NodeID != "n1" || m.NodeType != "End" || m.NodeName != "结束" {
		t.Fatalf("字段读写不符: %+v", m)
	}
}

// TestComponentState_默认 验证状态结构零值可用。
func TestComponentState_默认(t *testing.T) {
	s := ComponentState{CompID: "c1"}
	if s.CompID != "c1" || s.Status != nil {
		t.Fatalf("状态字段不符: %+v", s)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**（编译错误）

- [ ] **Step 3: 实现**（base.go 结构体区追加，对齐 Python base.py:43-56 dataclass）

```go
// WorkflowComponentMetadata 组件元数据
// Python: components/base.py:43-46 (WorkflowComponentMetadata dataclass)
type WorkflowComponentMetadata struct {
	// NodeID 节点标识
	NodeID string
	// NodeType 节点类型
	NodeType string
	// NodeName 节点名称
	NodeName string
}

// ComponentConfig 组件配置
// Python: components/base.py:50-51 (ComponentConfig dataclass)
type ComponentConfig struct {
	// Metadata 组件元数据（可空）
	Metadata *WorkflowComponentMetadata
}

// ComponentState 组件运行状态
// Python: components/base.py:55-56 (ComponentState dataclass)
// 偏差 D-17-7：Python status 字段类型为 Enum 但上游未定义任何成员，
// Go 以 any 承载、零消费留档，待上游定义成员后收窄。
type ComponentState struct {
	// CompID 组件标识
	CompID string
	// Status 状态枚举（Python status: Enum，成员上游未定义，D-17-7）
	Status any
}
```

- [ ] **Step 4: 跑测试通过** → **Step 5: 提交**

```bash
git add internal/agentcore/workflow/components/base.go internal/agentcore/workflow/components/base_test.go
git commit -m "feat(workflow): components/base.go 补 WorkflowComponentMetadata/ComponentConfig/ComponentState（8.17）"
```

---

### Task 3: session.NewComponentSession + component.go 组件基类

**Files:**
- Modify: `internal/agentcore/session/node.go`（新增 NewComponentSession）
- Create: `internal/agentcore/workflow/components/component.go`
- Modify: `internal/agentcore/workflow/components/doc.go`（文件树补 component.go）
- Test: `internal/agentcore/workflow/components/component_test.go`

- [ ] **Step 1: session 包助手（node.go，WorkflowVertexSession 定义后）**

```go
// NewComponentSession 组件级会话构造：校验入参为工作流顶点会话并包装为
// 组件开发者面门面。Python: component.py:95-99 isinstance(session, NodeSession)
// 校验 + Session(session, False/True) 包装；类型不符返回
// StatusWorkflowInnerOrchestrationError（实施时核对 codes_workflow.go 中该错误码
// 的实际 Go 名，若无则按 Python StatusCode.WORKFLOW_INNER_ORCHESTRATION_ERROR
// 的码值补常量）。
func NewComponentSession(sess BaseSession, streamMode bool) (*NodeSessionFacade, error) {
	vsess, ok := sess.(WorkflowVertexSession)
	if !ok {
		return nil, exception.NewStatusCode(exception.WorkflowInnerOrchestration,
			"session type must be NodeSession when component hook invoked")
	}
	inner, ok2 := vsess.(*internal.NodeSession)
	if !ok2 {
		return nil, exception.NewStatusCode(exception.WorkflowInnerOrchestration,
			"session type must be NodeSession when component hook invoked")
	}
	return NewNodeSessionFacade(inner, streamMode), nil
}
```

（import 与错误码名以 codes_workflow.go 现状为准；NewNodeSessionFacade 已有，直接复用。）

- [ ] **Step 2: 写失败测试**

```go
package components

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/graph"
)

// fakeBaseSession 非 WorkflowVertexSession 的假会话，用于校验拒绝分支。
type fakeBaseSession struct{ session.BaseSession }

// TestWorkflowComponent_OnInvoke_会话类型拒绝 验证非顶点会话返回编排错误。
func TestWorkflowComponent_OnInvoke_会话类型拒绝(t *testing.T) {
	comp := &WorkflowComponent{InvokeFn: func(ctx context.Context, inputs map[string]any, sess *session.NodeSessionFacade) (map[string]any, error) {
		return inputs, nil
	}}
	_, err := comp.OnInvoke(context.Background(), map[string]any{"a": 1}, fakeBaseSession{}, nil)
	if err == nil || !strings.Contains(err.Error(), "NodeSession") {
		t.Fatalf("期望 NodeSession 类型错误, got %v", err)
	}
}

// TestWorkflowComponent_OnInvoke_透传 验证 InvokeFn 收到组件级会话并透传输入。
func TestWorkflowComponent_OnInvoke_透传(t *testing.T) {
	var gotSess *session.NodeSessionFacade
	comp := &WorkflowComponent{InvokeFn: func(ctx context.Context, inputs map[string]any, sess *session.NodeSessionFacade) (map[string]any, error) {
		gotSess = sess
		return inputs, nil
	}}
	inner := session.NewWorkflowVertexSession(newFakeInnerSession(t), "n1", "Start", false)
	out, err := comp.OnInvoke(context.Background(), map[string]any{"a": 1}, inner, nil)
	if err != nil || out["a"] != 1 || gotSess == nil {
		t.Fatalf("透传失败: out=%v err=%v sess=%v", out, err, gotSess)
	}
}

// TestWorkflowComponent_默认钩子未实现 验证未覆写时走 BaseExecutable 默认错误。
func TestWorkflowComponent_默认钩子未实现(t *testing.T) {
	comp := &WorkflowComponent{}
	inner := session.NewWorkflowVertexSession(newFakeInnerSession(t), "n1", "Start", false)
	_, err := comp.OnStream(context.Background(), map[string]any{}, inner, nil)
	if err == nil {
		t.Fatal("期望默认未实现错误")
	}
}
```

（`newFakeInnerSession` 参考 vertex_test.go 既有 fake 惯例——实施时在 components 包内建 `testutil_test.go` 仿制；fake 需满足 interfaces.InnerSession + tracer.WorkflowNodeSession。）

- [ ] **Step 3: 跑测试确认失败 → Step 4: 实现 component.go**

```go
// Package components——本文件对齐 Python components/component.py 一比一。
package components

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/graph"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WorkflowComponent 工作流组件基类：运行时面（四钩子适配）+ 构图面合成体。
// Python: component.py:285-306 (WorkflowComponent) —— ComponentExecutable 与
// ComponentComposable 双 mixin 在 Go 以单结构体承载（偏差 D-17-1：Python
// Input/Output TypeVar=Any → Go 具体化 map[string]any）。
// 子类覆写方式：为四个函数槽赋值（Python 方法覆写 → Go 函数槽，8.10 AtomicNode 先例）；
// 未赋值的钩子走 BaseExecutable 默认未实现错误。
type WorkflowComponent struct {
	graph.BaseExecutable[map[string]any, map[string]any]

	// InvokeFn 批入批出（Python invoke(inputs, Session(session, False), context)）
	InvokeFn func(ctx context.Context, inputs map[string]any, sess *session.NodeSessionFacade) (map[string]any, error)
	// StreamFn 批入流出（Python stream(inputs, Session(session, False), context)）
	StreamFn func(ctx context.Context, inputs map[string]any, sess *session.NodeSessionFacade) (<-chan graph.StreamChunk[map[string]any], error)
	// CollectFn 流入批出（Python collect(inputs, Session(session, True), context)）
	CollectFn func(ctx context.Context, inputs <-chan map[string]any, sess *session.NodeSessionFacade) (map[string]any, error)
	// TransformFn 流入流出（Python transform(inputs, Session(session, True), context)）
	TransformFn func(ctx context.Context, inputs <-chan map[string]any, sess *session.NodeSessionFacade) (<-chan graph.StreamChunk[map[string]any], error)
	// TraceMetadataFn 组件追踪元数据（Python trace_metadata，默认 nil 不产出）
	TraceMetadataFn func() map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TraceMetadata 组件追踪元数据（Python component.py:124-135，默认 None）
func (c *WorkflowComponent) TraceMetadata() map[string]any {
	if c.TraceMetadataFn == nil {
		return nil
	}
	return c.TraceMetadataFn()
}

// ──────────────────────────── WorkflowComponent 方法 ────────────────────────────

// OnInvoke 单进单出钩子：会话校验 → 包装组件级会话 → Trigger 序列 → 转调 InvokeFn。
// Python: component.py:95-99 + _apply_callback_framework_to_executable 的
// emit_before(COMPONENT_BATCH_INPUT)/transform_io/emit_after(COMPONENT_BATCH_OUTPUT)
// 序列（偏差 D-17-5：方法体内 Trigger 等价序，2.14 Model 门面先例）。
func (c *WorkflowComponent) OnInvoke(ctx context.Context, inputs map[string]any, sess session.BaseSession, kwargs map[string]any) (map[string]any, error) {
	compSess, err := session.NewComponentSession(sess, false)
	if err != nil {
		return nil, err
	}
	fw := callback.GetCallbackFramework()
	fw.TriggerWorkflow(ctx, &callback.WorkflowEventData{Event: callback.ComponentBatchInput, NodeID: compSess.GetComponentID(), Inputs: inputs})
	if t := fw.TransformWorkflowIOInput(ctx, callback.ComponentBatchInput, inputs); t != nil {
		if m, ok := t.(map[string]any); ok {
			inputs = m
		}
	}
	var output map[string]any
	if c.InvokeFn == nil {
		output, err = c.BaseExecutable.OnInvoke(ctx, inputs, sess, kwargs)
	} else {
		output, err = c.InvokeFn(ctx, inputs, compSess)
	}
	if err != nil {
		return nil, err
	}
	if t := fw.TransformWorkflowIOOutput(ctx, callback.ComponentBatchOutput, output); t != nil {
		if m, ok := t.(map[string]any); ok {
			output = m
		}
	}
	fw.TriggerWorkflow(ctx, &callback.WorkflowEventData{Event: callback.ComponentBatchOutput, NodeID: compSess.GetComponentID(), Result: output})
	return output, nil
}

// OnStream 单进流出钩子：同 OnInvoke 结构，出入流事件为
// COMPONENT_BATCH_INPUT / COMPONENT_STREAM_OUTPUT，输出转换按帧应用。
// Python: component.py:101-107。
func (c *WorkflowComponent) OnStream(ctx context.Context, inputs map[string]any, sess session.BaseSession, kwargs map[string]any) (<-chan graph.StreamChunk[map[string]any], error) {
	compSess, err := session.NewComponentSession(sess, false)
	if err != nil {
		return nil, err
	}
	fw := callback.GetCallbackFramework()
	fw.TriggerWorkflow(ctx, &callback.WorkflowEventData{Event: callback.ComponentBatchInput, NodeID: compSess.GetComponentID(), Inputs: inputs})
	if t := fw.TransformWorkflowIOInput(ctx, callback.ComponentBatchInput, inputs); t != nil {
		if m, ok := t.(map[string]any); ok {
			inputs = m
		}
	}
	var ch <-chan graph.StreamChunk[map[string]any]
	if c.StreamFn == nil {
		ch, err = c.BaseExecutable.OnStream(ctx, inputs, sess, kwargs)
	} else {
		ch, err = c.StreamFn(ctx, inputs, compSess)
	}
	if err != nil {
		return nil, err
	}
	return transformStreamOutput(ctx, fw, compSess.GetComponentID(), ch), nil
}

// OnCollect 流进单出钩子：streamMode=true，事件 COMPONENT_STREAM_INPUT /
// COMPONENT_BATCH_OUTPUT。Python: component.py:109-114。
func (c *WorkflowComponent) OnCollect(ctx context.Context, inputs <-chan map[string]any, sess session.BaseSession, kwargs map[string]any) (map[string]any, error) {
	compSess, err := session.NewComponentSession(sess, true)
	if err != nil {
		return nil, err
	}
	fw := callback.GetCallbackFramework()
	fw.TriggerWorkflow(ctx, &callback.WorkflowEventData{Event: callback.ComponentStreamInput, NodeID: compSess.GetComponentID()})
	var output map[string]any
	if c.CollectFn == nil {
		output, err = c.BaseExecutable.OnCollect(ctx, inputs, sess, kwargs)
	} else {
		output, err = c.CollectFn(ctx, inputs, compSess)
	}
	if err != nil {
		return nil, err
	}
	if t := fw.TransformWorkflowIOOutput(ctx, callback.ComponentBatchOutput, output); t != nil {
		if m, ok := t.(map[string]any); ok {
			output = m
		}
	}
	fw.TriggerWorkflow(ctx, &callback.WorkflowEventData{Event: callback.ComponentBatchOutput, NodeID: compSess.GetComponentID(), Result: output})
	return output, nil
}

// OnTransform 流进流出钩子：streamMode=true，出入流事件为
// COMPONENT_STREAM_INPUT / COMPONENT_STREAM_OUTPUT，输出转换按帧应用。
// Python: component.py:116-122。
func (c *WorkflowComponent) OnTransform(ctx context.Context, inputs <-chan map[string]any, sess session.BaseSession, kwargs map[string]any) (<-chan graph.StreamChunk[map[string]any], error) {
	compSess, err := session.NewComponentSession(sess, true)
	if err != nil {
		return nil, err
	}
	fw := callback.GetCallbackFramework()
	fw.TriggerWorkflow(ctx, &callback.WorkflowEventData{Event: callback.ComponentStreamInput, NodeID: compSess.GetComponentID()})
	var ch <-chan graph.StreamChunk[map[string]any]
	if c.TransformFn == nil {
		ch, err = c.BaseExecutable.OnTransform(ctx, inputs, sess, kwargs)
	} else {
		ch, err = c.TransformFn(ctx, inputs, compSess)
	}
	if err != nil {
		return nil, err
	}
	return transformStreamOutput(ctx, fw, compSess.GetComponentID(), ch), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// transformStreamOutput 对流出通道逐帧应用 COMPONENT_STREAM_OUTPUT 转换与触发。
// Python transform_io 对 AsyncIterator 输出的逐项 output_event 语义。
func transformStreamOutput(ctx context.Context, fw *callback.CallbackFramework, nodeID string, ch <-chan graph.StreamChunk[map[string]any]) <-chan graph.StreamChunk[map[string]any] {
	out := make(chan graph.StreamChunk[map[string]any])
	go func() {
		defer close(out)
		for chunk := range ch {
			if chunk.Err == nil {
				if t := fw.TransformWorkflowIOOutput(ctx, callback.ComponentStreamOutput, chunk.Val); t != nil {
					if m, ok := t.(map[string]any); ok {
						chunk.Val = m
					}
				}
				fw.TriggerWorkflow(ctx, &callback.WorkflowEventData{Event: callback.ComponentStreamOutput, NodeID: nodeID, Result: chunk.Val})
			}
			out <- chunk
		}
	}()
	return out
}
```

编译期兜底（文件尾）：

```go
var _ graph.Executable[map[string]any, map[string]any] = (*WorkflowComponent)(nil)
```

- [ ] **Step 5: 跑测试通过 → Step 6: 提交**

```bash
git add internal/agentcore/session/node.go internal/agentcore/workflow/components/component.go internal/agentcore/workflow/components/component_test.go internal/agentcore/workflow/components/doc.go internal/agentcore/workflow/components/testutil_test.go
git commit -m "feat(workflow): WorkflowComponent 组件基类四钩子+会话校验+回调Trigger序列（8.17）"
```

---

### Task 4: TemplateUtils（end_comp.go 第一部分）

**Files:**
- Create: `internal/agentcore/workflow/components/flow/end_comp.go`（先只含 TemplateUtils）
- Create: `internal/agentcore/workflow/components/flow/doc.go`
- Test: `internal/agentcore/workflow/components/flow/end_comp_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestTemplateUtils_RenderTemplate_基本替换 验证 {{var}} 批量替换语义。
func TestTemplateUtils_RenderTemplate_基本替换(t *testing.T) {
	got := TemplateUtilsRenderTemplate("Hello {{name}}", map[string]any{"name": "alice"})
	if got != "Hello alice" {
		t.Fatalf("got %q", got)
	}
}

// TestTemplateUtils_RenderTemplate_缺键保留 验证 safe_substitute 缺键保留原样（偏差 D-17-4）。
func TestTemplateUtils_RenderTemplate_缺键保留(t *testing.T) {
	got := TemplateUtilsRenderTemplate("Hello {{name}}", map[string]any{})
	if got != "Hello {{name}}" {
		t.Fatalf("期望缺键保留原样, got %q", got)
	}
}

// TestTemplateUtils_RenderTemplateToList_分段 验证正则分段与空段过滤。
func TestTemplateUtils_RenderTemplateToList_分段(t *testing.T) {
	got := TemplateUtilsRenderTemplateToList("Hi {{a}} bye{{b}}")
	want := []string{"Hi ", "{{a}}", " bye", "{{b}}"}
	if len(got) != len(want) {
		t.Fatalf("段数不符: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("段 %d 不符: %q != %q", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 2: 失败 → Step 3: 实现（end_comp.go，对齐 end_comp.py:470-486）**

```go
// Package flow 流程组件：Start/End 及后续 Branch/Loop（Python components/flow/）。
package flow

// TemplateUtils 模板工具（Python end_comp.py:470-486 TemplateUtils 一比一）。
// 偏差 D-17-4：Python string.Template.safe_substitute → Go 手写等价（缺键保留原样）。

// renderTemplateVarRe {{var}} 段匹配（end_comp.py:486 re.split 捕获组）
var renderTemplateVarRe = regexp.MustCompile(`(\{\{[^{}]+\}\})`)

// TemplateUtilsRenderTemplate 模板整体渲染。
// Python: TemplateUtils.render_template（end_comp.py:472-482）：
// "{{"→"$"、"}}"删除后 safe_substitute；Go 直接按 {{var}} 正则替换，语义等价。
func TemplateUtilsRenderTemplate(template string, inputs map[string]any) string {
	return renderTemplateVarRe.ReplaceAllStringFunc(template, func(m string) string {
		key := strings.TrimSpace(m[2 : len(m)-2])
		if v, ok := inputs[key]; ok {
			return fmt.Sprintf("%v", v)
		}
		return m // 缺键保留原样（safe_substitute 语义）
	})
}

// TemplateUtilsRenderTemplateToList 模板分段：捕获组切分 + 滤除空段。
// Python: TemplateUtils.render_template_to_list（end_comp.py:484-486）
func TemplateUtilsRenderTemplateToList(template string) []string {
	parts := renderTemplateVarRe.Split(template, -1)
	segs := renderTemplateVarRe.FindAllString(template, -1)
	// Split 与 FindAllString 交替：[文本, 变量, 文本, 变量, ..., 尾文本]
	var out []string
	for i, txt := range parts {
		if txt != "" {
			out = append(out, txt)
		}
		if i < len(segs) {
			out = append(out, segs[i])
		}
	}
	return out
}
```

（导入：fmt/regexp/strings。Python 先 `{{`→`$` 再 safe_substitute 与直接正则替换对缺键、嵌套花括号行为等价；不等价点（如 `$` 字面量）登记 D-17-4 回收条件=上游模板语义变更。）

- [ ] **Step 4: 通过 → Step 5: 提交**

```bash
git add internal/agentcore/workflow/components/flow/end_comp.go internal/agentcore/workflow/components/flow/doc.go internal/agentcore/workflow/components/flow/end_comp_test.go
git commit -m "feat(workflow): TemplateUtils 模板分段与安全替换（8.17 End 前置）"
```

---

### Task 5: TemplateProcessor 状态机（非流式方法）

**Files:**
- Modify: `internal/agentcore/workflow/components/flow/end_comp.go`
- Test: `internal/agentcore/workflow/components/flow/end_comp_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestTemplateProcessor_构造分段 验证变量段剥壳与位置集合。
func TestTemplateProcessor_构造分段(t *testing.T) {
	p := NewTemplateProcessor("Hi {{a}} bye")
	if len(p.segments) != 2 {
		t.Fatalf("期望 2 段(Hi / a / bye → filter 后), got %v", p.segments)
	}
	if !p.variablesPositions[0] {
		t.Fatalf("位置 0 应为变量段: %v", p.variablesPositions)
	}
	if !p.shouldRender() {
		t.Fatal("当前位置应为变量段")
	}
}

// TestTemplateProcessor_NeedRender 验证变量有值才渲染。
func TestTemplateProcessor_NeedRender(t *testing.T) {
	p := NewTemplateProcessor("{{a}}")
	if p.needRender(map[string]any{}) {
		t.Fatal("空输入不应渲染")
	}
	if !p.needRender(map[string]any{"a": "x"}) {
		t.Fatal("有值应渲染")
	}
}

// TestTemplateProcessor_Reset 验证位置与计数归零。
func TestTemplateProcessor_Reset(t *testing.T) {
	p := NewTemplateProcessor("a {{b}} c")
	p.currentPosition = 2
	p.count = 1
	p.reset()
	if p.currentPosition != 0 || p.count != 0 || p.chunkIndex != 0 {
		t.Fatalf("reset 不彻底: %+v", p)
	}
}

// TestTemplateProcessor_SetDataSourceCount 验证数据源计数切换。
func TestTemplateProcessor_SetDataSourceCount(t *testing.T) {
	p := NewTemplateProcessor("a {{b}}")
	p.setDataSourceCount(2)
	if p.dataSourceCount != 2 || p.count != 0 {
		t.Fatalf("setDataSourceCount 不符: %+v", p)
	}
}
```

- [ ] **Step 2: 失败 → Step 3: 实现（end_comp.go 追加，对齐 end_comp.py:250-308）**

```go
// ──────────────────────────── 结构体 ────────────────────────────

// templateFrame 模板渲染流帧（Python render_stream 产出的 {"data","index"} dict）
type templateFrame struct {
	// Data 帧数据（静态文本串或变量值/流帧）
	Data any
	// Index 帧序号
	Index int
}

// TemplateProcessor 模板分段渲染状态机。
// Python: end_comp.py:250-444 (TemplateProcessor)。
// 并发原语映射（偏差 D-17-2）：asyncio.Lock → sync.Mutex；
// asyncio.Condition + anyio.fail_after → notify channel 广播（close+换新）
// + timer select（sync.Cond 无超时等待能力）。
type TemplateProcessor struct {
	// template 原始模板串
	template string
	// segments 分段列表（变量段已剥壳）
	segments []string
	// variablesPositions 变量段位置集合
	variablesPositions map[int]bool
	// currentPosition 当前推进位置
	currentPosition int
	// chunkIndex 帧序号
	chunkIndex int
	// dataSourceCount 数据源数（mix 模式=2）
	dataSourceCount int
	// count 已完成的数据源数
	count int
	// mu 互斥锁（Python _lock）
	mu sync.Mutex
	// notify 条件广播通道：notifyAll 时 close 并换新（Python _condition）
	notify chan struct{}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTemplateProcessor 构造：分段解析 + 变量段剥壳。
// Python: __init__（end_comp.py:251-267）
func NewTemplateProcessor(template string) *TemplateProcessor {
	responseList := TemplateUtilsRenderTemplateToList(template)
	p := &TemplateProcessor{
		template:           template,
		segments:           responseList,
		variablesPositions: make(map[int]bool),
		notify:             make(chan struct{}),
	}
	for pos, res := range responseList {
		if strings.HasPrefix(res, "{{") && strings.HasSuffix(res, "}}") {
			p.variablesPositions[pos] = true
			p.segments[pos] = res[2 : len(res)-2]
		}
	}
	return p
}

// SetDataSourceCount 设置数据源数并清零计数。
// Python: set_data_source_count（end_comp.py:269-271）
func (p *TemplateProcessor) SetDataSourceCount(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dataSourceCount = n
	p.count = 0
}

// CurrentPosition 当前位置（Python current_position）
func (p *TemplateProcessor) CurrentPosition() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.currentPosition
}

// Render 模板整体渲染（Python render，end_comp.py:301-302）
func (p *TemplateProcessor) Render(inputs map[string]any) string {
	return TemplateUtilsRenderTemplate(p.template, inputs)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// needRender 是否存在有值的变量段。
// Python: _need_render（end_comp.py:279-287）
func (p *TemplateProcessor) needRender(inputs map[string]any) bool {
	for pos, seg := range p.segments {
		if p.variablesPositions[pos] {
			if utils.GetValueByNestedPath(seg, inputs) != nil {
				return true
			}
		}
	}
	return false
}

// getSegment 取位置段文本（越界返回空串）。
// Python: _get_segment（end_comp.py:289-292）
func (p *TemplateProcessor) getSegment(pos int) string {
	if pos >= len(p.segments) {
		return ""
	}
	return p.segments[pos]
}

// shouldRender 当前位置是否变量段。Python: should_render（end_comp.py:294-295）
func (p *TemplateProcessor) shouldRender() bool {
	return p.variablesPositions[p.currentPosition]
}

// advancePosition 推进位置。Python: advance_position（end_comp.py:297-299）
func (p *TemplateProcessor) advancePosition() int {
	p.currentPosition++
	return p.currentPosition
}

// reset 归零位置/帧序/计数。Python: reset（end_comp.py:304-308）
func (p *TemplateProcessor) reset() {
	if p.currentPosition != 0 {
		p.currentPosition = 0
	}
	p.chunkIndex = 0
	p.count = 0
}

// isFinished 是否推进完毕。Python: is_finished（end_comp.py:443-444）
func (p *TemplateProcessor) isFinished() bool {
	return p.currentPosition >= len(p.segments)
}

// notifyAll 广播唤醒等待方：close 现通道并换新（须持 mu）。
// Python: _condition.notify_all（end_comp.py:440-441）
func (p *TemplateProcessor) notifyAll() {
	close(p.notify)
	p.notify = make(chan struct{})
}
```

（import：strings、sync、`session/utils` 的 GetValueByNestedPath——包路径 `github.com/uapclaw/uapclaw-go/internal/agentcore/session/utils`。）

- [ ] **Step 4: 通过 → Step 5: 提交**

```bash
git add internal/agentcore/workflow/components/flow/end_comp.go internal/agentcore/workflow/components/flow/end_comp_test.go
git commit -m "feat(workflow): TemplateProcessor 分段状态机非流式方法（8.17）"
```

---

### Task 6: TemplateProcessor.renderStream + consumeAllIterators

**Files:**
- Modify: `internal/agentcore/workflow/components/flow/end_comp.go`
- Test: `internal/agentcore/workflow/components/flow/end_comp_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestTemplateProcessor_RenderStream_静态段直出变量段取值 验证主循环基础路径。
func TestTemplateProcessor_RenderStream_静态段直出变量段取值(t *testing.T) {
	p := NewTemplateProcessor("Hello {{name}}!")
	var frames []templateFrame
	for f := range p.renderStream(context.Background(), map[string]any{"name": "alice"}, nil, 0.2) {
		if f.Err != nil {
			t.Fatalf("流中错误: %v", f.Err)
		}
		frames = append(frames, f.Val)
	}
	if len(frames) != 2 {
		t.Fatalf("期望 2 帧, got %d: %v", len(frames), frames)
	}
	if frames[0].Data != "Hello " || frames[1].Data != "alice" || frames[1].Index != 1 {
		t.Fatalf("帧内容不符: %+v", frames)
	}
}

// TestTemplateProcessor_RenderStream_变量缺失跳过 验证单源 None 值跳过（debug 日志路径）。
func TestTemplateProcessor_RenderStream_变量缺失跳过(t *testing.T) {
	p := NewTemplateProcessor("A {{x}} B")
	var datas []any
	for f := range p.renderStream(context.Background(), map[string]any{}, nil, 0.2) {
		frames := f
		if frames.Err != nil {
			t.Fatalf("流中错误: %v", frames.Err)
		}
		datas = append(datas, frames.Val.Data)
	}
	if len(datas) != 2 || datas[0] != "A " || datas[1] != " B" {
		t.Fatalf("期望静态段直出+变量段跳过: %v", datas)
	}
}

// TestTemplateProcessor_RenderStream_变量流叶子 验证 channel 叶子逐帧转发。
func TestTemplateProcessor_RenderStream_变量流叶子(t *testing.T) {
	p := NewTemplateProcessor("{{name}}")
	ch := make(chan any, 2)
	ch <- "f1"
	ch <- "f2"
	close(ch)
	var datas []any
	for f := range p.renderStream(context.Background(), map[string]any{"name": (<-chan any)(ch)}, nil, 0.2) {
		if f.Err != nil {
			t.Fatalf("流中错误: %v", f.Err)
		}
		datas = append(datas, f.Val.Data)
	}
	if len(datas) != 2 || datas[0] != "f1" || datas[1] != "f2" {
		t.Fatalf("流叶子转发不符: %v", datas)
	}
}

// TestConsumeAllIterators_递归耗尽 验证嵌套 dict/list 中 channel 全部耗尽。
func TestConsumeAllIterators_递归耗尽(t *testing.T) {
	ch1 := make(chan any, 1)
	ch1 <- 1
	close(ch1)
	inputs := map[string]any{"a": []any{ch1, map[string]any{"b": make(chan any)}}, "c": (<-chan any)(make(chan any))}
	consumeAllIterators(context.Background(), inputs)
	// 耗尽后再次读立即关闭——用 range 验证无死锁即通过
	for range ch1 {
	}
}
```

- [ ] **Step 2: 失败 → Step 3: 实现（end_comp.go 追加，对齐 end_comp.py:310-442）**

```go
// RenderStream 模板流式渲染入口：主循环产出帧；结束后耗尽输入中残留流并按
// 数据源计数归零。Python: render_stream（end_comp.py:356-366）。
func (p *TemplateProcessor) RenderStream(ctx context.Context, inputs map[string]any, sess *session.NodeSessionFacade, timeout float64) <-chan graph.StreamChunk[templateFrame] {
	out := make(chan graph.StreamChunk[templateFrame])
	go func() {
		defer func() {
			// finally：耗尽输入中所有迭代器（end_comp.py:361-362）
			consumeAllIterators(ctx, inputs)
			p.mu.Lock()
			p.count++
			done := p.count == p.dataSourceCount
			if done {
				p.reset()
			}
			p.mu.Unlock()
		}()
		p.renderStreamLoop(ctx, inputs, sess, timeout, out)
	}()
	return out
}

// renderStreamLoop 主推进循环。
// Python: _render_stream（end_comp.py:368-441）。
// 三态 None 处理：mix 等待（count<data_source_count）/ 单源跳过（!has_any_value，
// debug 日志）/ 有值等待；静态段直出；channel 叶子逐帧转发。
func (p *TemplateProcessor) renderStreamLoop(ctx context.Context, inputs map[string]any, sess *session.NodeSessionFacade, timeout float64, out chan<- graph.StreamChunk[templateFrame]) {
	hasAnyValue := p.needRender(inputs)
	shouldWait := false
	for {
		if shouldWait {
			// Python: async with self._condition + anyio.fail_after(wait_timeout)
			p.mu.Lock()
			waitCh := p.notify
			p.mu.Unlock()
			var waitDur time.Duration
			if timeout > 0 {
				waitDur = time.Duration(timeout * float64(time.Second))
			}
			timer := time.NewTimer(waitDur)
			select {
			case <-waitCh:
				timer.Stop()
			case <-timer.C:
				// TimeoutError → warning + advance（end_comp.py:380-388）
				logComp.Warn(logger.ComponentAgentCore).
					Float64("timeout", timeout).
					Str("component_id", compIDOf(sess)).
					Msg("template render stream timeout")
				p.mu.Lock()
				p.advancePosition()
				p.mu.Unlock()
			case <-ctx.Done():
				// asyncio.CancelledError → warning + break（end_comp.py:389-397）
				logComp.Warn(logger.ComponentAgentCore).
					Str("component_id", compIDOf(sess)).
					Msg("template render stream cancelled")
				return
			}
			shouldWait = false
		}
		p.mu.Lock()
		if p.isFinished() {
			p.mu.Unlock()
			return
		}
		segment := p.getSegment(p.currentPosition)
		if !p.shouldRender() {
			// 静态文本段直出（end_comp.py:404-409）
			f := templateFrame{Data: segment, Index: p.chunkIndex}
			p.chunkIndex++
			p.advancePosition()
			p.mu.Unlock()
			out <- graph.StreamChunk[templateFrame]{Val: f}
			continue
		}
		value := utils.GetValueByNestedPath(segment, inputs)
		p.mu.Unlock()
		if value == nil {
			p.mu.Lock()
			if p.count < p.dataSourceCount {
				shouldWait = true // mix 模式等待另一数据源（end_comp.py:415-417）
				p.mu.Unlock()
				continue
			}
			if !hasAnyValue {
				// 单源跳过 None 值位置（end_comp.py:419-428）
				logComp.Debug(logger.ComponentAgentCore).
					Str("segment", segment).
					Str("component_id", compIDOf(sess)).
					Msg("template segment skip none value position")
				p.advancePosition()
				p.mu.Unlock()
				continue
			}
			shouldWait = true // 有值场景等待（end_comp.py:429-430）
			p.mu.Unlock()
			continue
		}
		if ch, ok := value.(<-chan any); ok {
			// 流叶子逐帧转发（end_comp.py:432-435）
			for frame := range ch {
				p.mu.Lock()
				f := templateFrame{Data: frame, Index: p.chunkIndex}
				p.chunkIndex++
				p.mu.Unlock()
				out <- graph.StreamChunk[templateFrame]{Val: f}
			}
		} else {
			p.mu.Lock()
			f := templateFrame{Data: value, Index: p.chunkIndex}
			p.chunkIndex++
			p.mu.Unlock()
			out <- graph.StreamChunk[templateFrame]{Val: f}
		}
		p.mu.Lock()
		p.advancePosition()
		p.notifyAll()
		p.mu.Unlock()
	}
}

// consumeAllIterators 递归收集并耗尽 inputs 中所有 channel 叶子。
// Python: TemplateProcessor._consume_all_iterators（end_comp.py:310-354）；
// 偏差 D-17-3：AsyncGenerator/Generator → Go channel（含反射兜底任意 chan 种类）。
func consumeAllIterators(ctx context.Context, data any) {
	switch v := data.(type) {
	case map[string]any:
		for _, val := range v {
			consumeAllIterators(ctx, val)
		}
	case []any:
		for _, val := range v {
			consumeAllIterators(ctx, val)
		}
	case <-chan any:
		for range v {
		}
	case chan any:
		for range v {
		}
	default:
		rv := reflect.ValueOf(data)
		if rv.Kind() == reflect.Chan {
			for {
				if _, ok := rv.Recv(); !ok {
					break
				}
			}
		}
	}
}

// compIDOf 组件 ID 安全取值（sess 为 nil 时返回空串）。
func compIDOf(sess *session.NodeSessionFacade) string {
	if sess == nil {
		return ""
	}
	return sess.GetComponentID()
}
```

（`logComp` 为包级 `const logComponent = logger.ComponentAgentCore` 不成立——本包用 `var logComp = logger.ComponentAgentCore` 惯例；实施时按包内既有 logger 惯例统一。流叶子发送与锁的顺序实施时以 `-race` 验证调整。）

- [ ] **Step 4: 通过 → Step 5: 提交**

```bash
git add internal/agentcore/workflow/components/flow/end_comp.go internal/agentcore/workflow/components/flow/end_comp_test.go
git commit -m "feat(workflow): TemplateProcessor.renderStream 主循环+流叶子转发+迭代器耗尽（8.17）"
```

---

### Task 7: Start 组件

**Files:**
- Create: `internal/agentcore/workflow/components/flow/start_comp.go`
- Test: `internal/agentcore/workflow/components/flow/start_comp_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestStart_Invoke_透传 验证 Start 原样返回 inputs（start_comp.py:14-15）。
func TestStart_Invoke_透传(t *testing.T) {
	s := NewStart()
	inner := session.NewWorkflowVertexSession(newFakeInnerSession(t), "n1", "Start", false)
	in := map[string]any{"q": "hi"}
	out, err := s.OnInvoke(context.Background(), in, inner, nil)
	if err != nil || !maps.Equal(out, in) {
		t.Fatalf("透传失败: out=%v err=%v", out, err)
	}
}

// TestStart_其余钩子默认未实现 验证 stream/collect/transform 走基类默认错误。
func TestStart_其余钩子默认未实现(t *testing.T) {
	s := NewStart()
	inner := session.NewWorkflowVertexSession(newFakeInnerSession(t), "n1", "Start", false)
	if _, err := s.OnStream(context.Background(), nil, inner, nil); err == nil {
		t.Fatal("期望默认未实现错误")
	}
}
```

- [ ] **Step 2: 失败 → Step 3: 实现（start_comp.go，对齐 start_comp.py 全文 14 行）**

```go
// Start 工作流入口组件：invoke 原样透传输入。
// Python: components/flow/start_comp.py:10-15 一比一。
type Start struct {
	WorkflowComponent
}

// NewStart 构造 Start 组件（Python __init__ 仅调 super().__init__()）。
func NewStart() *Start {
	return &Start{
		WorkflowComponent: WorkflowComponent{
			InvokeFn: func(ctx context.Context, inputs map[string]any, sess *session.NodeSessionFacade) (map[string]any, error) {
				return inputs
			},
		},
	}
}
```

- [ ] **Step 4: 通过 → Step 5: 提交**

```bash
git add internal/agentcore/workflow/components/flow/start_comp.go internal/agentcore/workflow/components/flow/start_comp_test.go
git commit -m "feat(workflow): Start 入口组件透传实现（8.17）"
```

---

### Task 8: EndConfig + End 构造/SetMix

**Files:**
- Modify: `internal/agentcore/workflow/components/flow/end_comp.go`
- Test: `internal/agentcore/workflow/components/flow/end_comp_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestNewEnd_带配置 验证模板处理器建立。
func TestNewEnd_带配置(t *testing.T) {
	e, err := NewEnd(EndConfigFromMap(map[string]any{"responseTemplate": "Hi {{n}}"}))
	if err != nil || e.template == nil {
		t.Fatalf("构造失败: err=%v template=%v", err, e.template)
	}
}

// TestNewEnd_配置无效 验证 100010 错误（end_comp.py:46）。
func TestNewEnd_配置无效(t *testing.T) {
	_, err := NewEnd(EndConfigFromMap(map[string]any{})) // 缺 responseTemplate
	if err == nil || !errors.Is(err, exception.StatusComponentEndParamInvalid) {
		t.Fatalf("期望 COMPONENT_END_PARAM_INVALID, got %v", err)
	}
}

// TestNewEnd_无配置 验证 conf/template 双 nil。
func TestNewEnd_无配置(t *testing.T) {
	e, err := NewEnd(nil)
	if err != nil || e.conf != nil || e.template != nil {
		t.Fatalf("无配置构造不符: err=%v", err)
	}
}

// TestEnd_SetMix 验证 mix 置位与模板数据源计数（end_comp.py:53-57）。
func TestEnd_SetMix(t *testing.T) {
	e, _ := NewEnd(EndConfigFromMap(map[string]any{"responseTemplate": "Hi {{n}}"}))
	e.SetMix()
	if !e.mix || e.template.dataSourceCount != 2 {
		t.Fatalf("SetMix 不符: mix=%v count=%d", e.mix, e.template.dataSourceCount)
	}
}
```

- [ ] **Step 2: 失败 → Step 3: 实现（end_comp.go 追加，对齐 end_comp.py:26-57）**

```go
// EndConfig End 组件配置。
// Python: end_comp.py:26-35 (EndConfig pydantic)；alias responseTemplate、min_length=1。
type EndConfig struct {
	// ResponseTemplate 最终输出格式化模板，渲染结果 {"response": "..."}
	ResponseTemplate string `json:"responseTemplate"`
}

// NewEndConfig 构造校验（min_length=1 拒绝空模板）。
func NewEndConfig(responseTemplate string) (*EndConfig, error) {
	if responseTemplate == "" {
		return nil, exception.NewStatusCode(exception.Code("COMPONENT_END_PARAM_INVALID"), "response_template must not be empty")
	}
	return &EndConfig{ResponseTemplate: responseTemplate}, nil
}

// EndConfigFromMap 从 map 构造（populate_by_name 语义；键缺失返回零值配置交由 NewEnd 校验拒绝）。
func EndConfigFromMap(m map[string]any) *EndConfig {
	c := &EndConfig{}
	if v, ok := m["responseTemplate"].(string); ok {
		c.ResponseTemplate = v
	}
	return c
}

// End 工作流出口组件：模板格式化最终输出。
// Python: end_comp.py:38-51 (End)。
type End struct {
	WorkflowComponent
	// conf 配置（可 nil）
	conf *EndConfig
	// template 模板处理器（conf 非 nil 时建立）
	template *TemplateProcessor
	// batchTemplate 批渲染处理器（_render 懒建立）
	batchTemplate *TemplateBatchProcessor
	// mix 混合模式（模板+裸流并存）
	mix bool
}

// NewEnd 构造：conf 非 nil → 校验 + 建 TemplateProcessor，失败抛
// StatusComponentEndParamInvalid（reason 拼 f"conf is invalid, {str(e)}"）。
// Python: __init__（end_comp.py:39-51）。
func NewEnd(conf *EndConfig) (*End, error) {
	e := &End{}
	if conf != nil {
		if conf.ResponseTemplate == "" {
			return nil, exception.NewStatusCode(exception.Code("COMPONENT_END_PARAM_INVALID"), "conf is invalid, response_template must not be empty")
		}
		e.conf = conf
		e.template = NewTemplateProcessor(conf.ResponseTemplate)
	}
	// invoke/stream/collect/transform 四槽在 Task 9-12 填充
	return e, nil
}

// SetMix 进入混合模式：模板数据源计数置 2 并复位。
// Python: set_mix（end_comp.py:53-57）。
func (e *End) SetMix() {
	e.mix = true
	if e.template != nil {
		e.template.SetDataSourceCount(2)
		e.template.mu.Lock()
		e.template.reset()
		e.template.mu.Unlock()
	}
}
```

（错误码：核对 codes_workflow.go:74 `StatusComponentEndParamInvalid` 的实际构造方式（`NewStatusCode(...)` 的签名），照抄同款调用形式；reason 文案逐字对齐 `f"conf is invalid, {str(e)}"` 语义。）

- [ ] **Step 4: 通过 → Step 5: 提交**

```bash
git add internal/agentcore/workflow/components/flow/end_comp.go internal/agentcore/workflow/components/flow/end_comp_test.go
git commit -m "feat(workflow): EndConfig + End 构造与 SetMix（8.17）"
```

---

### Task 9: End.Invoke

**Files:**
- Modify: `internal/agentcore/workflow/components/flow/end_comp.go`（NewEnd 内填 InvokeFn）
- Test: `internal/agentcore/workflow/components/flow/end_comp_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestEnd_Invoke_无模板_过滤None信封 验证 {"output": {...}}（end_comp.py:74-79）。
func TestEnd_Invoke_无模板_过滤None信封(t *testing.T) {
	e, _ := NewEnd(nil)
	inner := session.NewWorkflowVertexSession(newFakeInnerSession(t), "n1", "End", false)
	out, err := e.OnInvoke(context.Background(), map[string]any{"a": 1, "b": nil}, inner, nil)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	wrapper, ok := out["output"].(map[string]any)
	if !ok || wrapper["a"] != 1 || len(wrapper) != 1 {
		t.Fatalf("期望 output 信封过滤 None: %v", out)
	}
}

// TestEnd_Invoke_无模板_全None返回nil 验证 output 为 nil 时结果 nil（end_comp.py:79）。
func TestEnd_Invoke_无模板_全None返回nil(t *testing.T) {
	e, _ := NewEnd(nil)
	inner := session.NewWorkflowVertexSession(newFakeInnerSession(t), "n1", "End", false)
	out, err := e.OnInvoke(context.Background(), map[string]any{"b": nil}, inner, nil)
	if err != nil || out != nil {
		t.Fatalf("期望 nil 输出: out=%v err=%v", out, err)
	}
}

// TestEnd_Invoke_有模板_渲染 验证 {"response": 渲染串}（end_comp.py:70-73）。
func TestEnd_Invoke_有模板_渲染(t *testing.T) {
	e, _ := NewEnd(EndConfigFromMap(map[string]any{"responseTemplate": "Hi {{n}}"}))
	inner := session.NewWorkflowVertexSession(newFakeInnerSession(t), "n1", "End", false)
	out, err := e.OnInvoke(context.Background(), map[string]any{"n": "bob"}, inner, nil)
	if err != nil || out["response"] != "Hi bob" {
		t.Fatalf("模板渲染不符: out=%v err=%v", out, err)
	}
}
```

- [ ] **Step 2: 失败 → Step 3: 实现（NewEnd 的 InvokeFn 赋值，对齐 end_comp.py:59-89）**

```go
		InvokeFn: func(ctx context.Context, inputs map[string]any, sess *session.NodeSessionFacade) (map[string]any, error) {
			logComp.Debug(logger.ComponentAgentCore).
				Str("component_id", sess.GetComponentID()).
				Str("session_id", sess.GetSessionID()).
				Bool("has_template", e.template != nil).
				Bool("mix", e.mix).
				Msg("End component invoke started")
			var result map[string]any
			if e.template != nil {
				if inputs == nil {
					inputs = map[string]any{}
				}
				r, rerr := e.render(ctx, inputs, sess.GetEnv(sessionconstants.EndCompTemplateBranchRenderTimeoutKey), sess)
				if rerr != nil {
					return nil, rerr
				}
				result = r
			} else {
				var output any
				if inputs != nil {
					filtered := make(map[string]any, len(inputs))
					for k, v := range inputs {
						if v != nil {
							filtered[k] = v
						}
					}
					if len(filtered) > 0 {
						output = filtered
					}
				}
				if output != nil {
					result = map[string]any{"output": output}
				}
			}
			logComp.Debug(logger.ComponentAgentCore).
				Str("component_id", sess.GetComponentID()).
				Str("session_id", sess.GetSessionID()).
				Bool("has_template", e.template != nil).
				Bool("mix", e.mix).
				Msg("End component invoke succeed")
			return result, nil
		},
```

（`render` 在 Task 12 实现——本任务先以同名占位方法返回 nil/nil？**不允许占位**：Task 9 与 Task 12 合并提交或本任务先实现 render 的非 mix 分支（batchTemplate 懒建立 + render + notify_all + 清 nil），mix 分支 Task 12 补。按后者执行。）

render 非 mix 分支（end_comp.py:214-245 无 mix 部分）：

```go
// render 批渲染：懒建 batchTemplate → 渲染 → notifyAll → 清空。
// Python: _render（end_comp.py:214-245），mix 分支在 Task 12 补齐。
func (e *End) render(ctx context.Context, inputs map[string]any, timeout any, sess *session.NodeSessionFacade) (map[string]any, error) {
	if e.batchTemplate == nil {
		e.batchTemplate = &TemplateBatchProcessor{template: e.template, inputs: inputs, notify: make(chan struct{})}
	}
	answer, err := e.batchTemplate.render(ctx, nil, sess)
	if err != nil {
		return nil, err
	}
	e.batchTemplate.mu.Lock()
	e.batchTemplate.notifyAll()
	e.batchTemplate.mu.Unlock()
	e.batchTemplate = nil
	return map[string]any{"response": answer}, nil
}

// TemplateBatchProcessor 批渲染处理器。
// Python: end_comp.py:447-467 (TemplateBatchProcessor)。
type TemplateBatchProcessor struct {
	template *TemplateProcessor
	inputs   map[string]any
	// isRendered 是否已渲染过
	isRendered bool
	mu         sync.Mutex
	notify     chan struct{}
}

// render 合并 inputs（Python self._inputs | inputs：调用方 inputs 优先）并逐帧拼接。
// Python: render（end_comp.py:457-467）。
func (b *TemplateBatchProcessor) render(ctx context.Context, extra map[string]any, sess *session.NodeSessionFacade) (string, error) {
	b.isRendered = true
	merged := make(map[string]any, len(b.inputs)+len(extra))
	for k, v := range b.inputs {
		merged[k] = v
	}
	for k, v := range extra {
		merged[k] = v
	}
	var sb strings.Builder
	for chunk := range b.template.RenderStream(ctx, merged, sess, 0) {
		if chunk.Err != nil {
			return "", chunk.Err
		}
		sb.WriteString(fmt.Sprintf("%v", chunk.Val.Data))
	}
	return sb.String(), nil
}
```

（timeout any→float 转换用 vertex.go 既有的 anyToFloat64 或本地同款私有助手 `envTimeoutToFloat`——实施时复用 graph 包导出版或复制并注明来源。）

- [ ] **Step 4: 通过 → Step 5: 提交**

```bash
git add internal/agentcore/workflow/components/flow/end_comp.go internal/agentcore/workflow/components/flow/end_comp_test.go
git commit -m "feat(workflow): End.Invoke 批渲染路径+TemplateBatchProcessor 非 mix 分支（8.17）"
```

---

### Task 10: End.Stream

**Files:**
- Modify: `internal/agentcore/workflow/components/flow/end_comp.go`（NewEnd 内填 StreamFn）
- Test: `internal/agentcore/workflow/components/flow/end_comp_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestEnd_Stream_有模板_信封帧 验证 OutputSchema{EndNodeStream} 包装（end_comp.py:106-114）。
func TestEnd_Stream_有模板_信封帧(t *testing.T) {
	e, _ := NewEnd(EndConfigFromMap(map[string]any{"responseTemplate": "Hi {{n}}"}))
	inner := session.NewWorkflowVertexSession(newFakeInnerSession(t), "n1", "End", false)
	ch, err := e.OnStream(context.Background(), map[string]any{"n": "cat"}, inner, nil)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	var got []stream.OutputSchema
	for c := range ch {
		if c.Err != nil {
			t.Fatalf("流中错误: %v", c.Err)
		}
		got = append(got, c.Val["frame"].(stream.OutputSchema))
	}
	if len(got) != 2 || got[0].Type != sessionconstants.EndNodeStream {
		t.Fatalf("信封帧不符: %+v", got)
	}
}

// TestEnd_Stream_无模板_逐key吐出 验证 dict 逐 key 输出（end_comp.py:116-119）。
func TestEnd_Stream_无模板_逐key吐出(t *testing.T) {
	e, _ := NewEnd(nil)
	inner := session.NewWorkflowVertexSession(newFakeInnerSession(t), "n1", "End", false)
	ch, err := e.OnStream(context.Background(), map[string]any{"a": 1, "b": 2}, inner, nil)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	count := 0
	for c := range ch {
		if c.Err != nil {
			t.Fatalf("流中错误: %v", c.Err)
		}
		count++
	}
	if count != 2 {
		t.Fatalf("期望 2 帧, got %d", count)
	}
}
```

- [ ] **Step 2: 失败 → Step 3: 实现（StreamFn，对齐 end_comp.py:91-142）**

```go
		StreamFn: func(ctx context.Context, inputs map[string]any, sess *session.NodeSessionFacade) (<-chan graph.StreamChunk[map[string]any], error) {
			logComp.Debug(logger.ComponentAgentCore).
				Str("component_id", sess.GetComponentID()).
				Str("session_id", sess.GetSessionID()).
				Bool("has_template", e.template != nil).
				Bool("mix", e.mix).
				Msg("End component stream started")
			if inputs == nil {
				inputs = map[string]any{}
			}
			out := make(chan graph.StreamChunk[map[string]any])
			frameCount := 0
			go func() {
				defer close(out)
				defer func() {
					logComp.Debug(logger.ComponentAgentCore).
						Str("component_id", sess.GetComponentID()).
						Str("session_id", sess.GetSessionID()).
						Bool("has_template", e.template != nil).
						Bool("mix", e.mix).
						Int("chunk_num", frameCount).
						Msg("End component stream succeed")
				}()
				if e.template != nil {
					timeout := envTimeoutToFloat(sess.GetEnv(sessionconstants.EndCompTemplateRenderPositionTimeoutKey))
					for chunk := range e.template.RenderStream(ctx, inputs, sess, timeout) {
						if chunk.Err != nil {
							logComp.Error(logger.ComponentAgentCore).
								Err(chunk.Err).
								Str("component_id", sess.GetComponentID()).
								Int("chunk_num", frameCount).
								Msg("End component stream failed")
							out <- graph.StreamChunk[map[string]any]{Err: chunk.Err}
							return
						}
						frameCount++
						out <- graph.StreamChunk[map[string]any]{Val: map[string]any{
							"frame": stream.OutputSchema{
								Type:    sessionconstants.EndNodeStream,
								Index:   chunk.Val.Index,
								Payload: map[string]any{"response": chunk.Val.Data},
							},
						}}
					}
					return
				}
				// 无模板：dict 逐 key 吐 {"output": {k:v}}（end_comp.py:116-119）
				for key, value := range inputs {
					frameCount++
					out <- graph.StreamChunk[map[string]any]{Val: map[string]any{
						"output": map[string]any{key: value},
					}}
				}
			}()
			return out, nil
		},
```

（`stream.OutputSchema` 字段名以 `internal/agentcore/session/stream` 现状为准（Type/Index/Payload），实施时核对；Payload Python 是 `dict(response=...)`。）

- [ ] **Step 4: 通过 → Step 5: 提交**

```bash
git add internal/agentcore/workflow/components/flow/end_comp.go internal/agentcore/workflow/components/flow/end_comp_test.go
git commit -m "feat(workflow): End.Stream 模板信封帧与无模板逐 key 吐出（8.17）"
```

---

### Task 11: End.Transform / End.Collect

**Files:**
- Modify: `internal/agentcore/workflow/components/flow/end_comp.go`（NewEnd 内填 TransformFn/CollectFn）
- Test: `internal/agentcore/workflow/components/flow/end_comp_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestEnd_Transform_无模板_叶子帧 验证流入经叶子抽取逐帧吐出（end_comp.py:163-171）。
func TestEnd_Transform_无模板_叶子帧(t *testing.T) {
	e, _ := NewEnd(nil)
	inner := session.NewWorkflowVertexSession(newFakeInnerSession(t), "n1", "End", false)
	in := make(chan map[string]any, 1)
	in <- map[string]any{"llm1": map[string]any{"text": "hi"}}
	close(in)
	ch, err := e.OnTransform(context.Background(), in, inner, nil)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	var first map[string]any
	for c := range ch {
		if c.Err != nil {
			t.Fatalf("流中错误: %v", c.Err)
		}
		if first == nil {
			first = c.Val
		}
	}
	// 首帧含 {"output": {"llm1.text": "hi"}} 形状（叶子抽取+format_path）
	if first == nil {
		t.Fatal("无输出帧")
	}
}

// TestEnd_Collect_无模板_聚合 验证 {"output": chunks}（end_comp.py:193-202）。
func TestEnd_Collect_无模板_聚合(t *testing.T) {
	e, _ := NewEnd(nil)
	inner := session.NewWorkflowVertexSession(newFakeInnerSession(t), "n1", "End", false)
	in := make(chan map[string]any, 1)
	in <- map[string]any{"k": "v"}
	close(in)
	out, err := e.OnCollect(context.Background(), in, inner, nil)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if _, ok := out["output"]; !ok {
		t.Fatalf("期望 output 聚合信封: %v", out)
	}
}

// TestEnd_Collect_有模板_渲染 验证模板路径（end_comp.py:191-192）。
func TestEnd_Collect_有模板_渲染(t *testing.T) {
	e, _ := NewEnd(EndConfigFromMap(map[string]any{"responseTemplate": "Hi {{n}}"}))
	inner := session.NewWorkflowVertexSession(newFakeInnerSession(t), "n1", "End", false)
	in := make(chan map[string]any, 1)
	in <- map[string]any{"n": "joe"}
	close(in)
	out, err := e.OnCollect(context.Background(), in, inner, nil)
	if err != nil || out["response"] != "Hi joe" {
		t.Fatalf("模板聚合不符: out=%v err=%v", out, err)
	}
}
```

- [ ] **Step 2: 失败 → Step 3: 实现（TransformFn/CollectFn，对齐 end_comp.py:144-212）**

```go
		TransformFn: func(ctx context.Context, inputs <-chan map[string]any, sess *session.NodeSessionFacade) (<-chan graph.StreamChunk[map[string]any], error) {
			logComp.Debug(logger.ComponentAgentCore).
				Str("component_id", sess.GetExecutableID()).
				Str("session_id", sess.GetSessionID()).
				Bool("has_template", e.template != nil).
				Msg("End component transform started")
			out := make(chan graph.StreamChunk[map[string]any])
			frameCount := 0
			go func() {
				defer close(out)
				defer func() {
					logComp.Debug(logger.ComponentAgentCore).
						Int("chunk_num", frameCount).
						Msg("End component transform succeed")
				}()
				if e.template != nil {
					timeout := envTimeoutToFloat(sess.GetEnv(sessionconstants.EndCompTemplateRenderPositionTimeoutKey))
					for chunk := range e.template.RenderStream(ctx, flattenChannelInputs(inputs), sess, timeout) {
						if chunk.Err != nil {
							out <- graph.StreamChunk[map[string]any]{Err: chunk.Err}
							return
						}
						frameCount++
						out <- graph.StreamChunk[map[string]any]{Val: map[string]any{
							"frame": stream.OutputSchema{
								Type:    sessionconstants.EndNodeStream,
								Index:   chunk.Val.Index,
								Payload: map[string]any{"response": chunk.Val.Data},
							},
						}}
					}
					return
				}
				// 无模板：收帧 → 叶子抽取 → 逐帧吐 {"output": {path: value}}
				// Python transform 无模板路径逐 (path,value) 吐 dict(output={path: frame})
				for frame := range inputs {
					for _, kv := range utils.ExtractLeafNodes(frame) {
						path := utils.FormatPath(kv.Path)
						frameCount++
						out <- graph.StreamChunk[map[string]any]{Val: map[string]any{
							"output": map[string]any{path: kv.Value},
						}}
					}
				}
			}()
			return out, nil
		},
		CollectFn: func(ctx context.Context, inputs <-chan map[string]any, sess *session.NodeSessionFacade) (map[string]any, error) {
			logComp.Debug(logger.ComponentAgentCore).
				Str("component_id", sess.GetExecutableID()).
				Str("session_id", sess.GetSessionID()).
				Bool("has_template", e.template != nil).
				Msg("End component collect started")
			var result map[string]any
			if e.template != nil {
				merged := flattenChannelInputs(inputs)
				timeout := envTimeoutToFloat(sess.GetEnv(sessionconstants.EndCompTemplateBranchRenderTimeoutKey))
				r, rerr := e.render(ctx, merged, timeout, sess)
				if rerr != nil {
					return nil, rerr
				}
				result = r
			} else {
				var chunks []any
				for frame := range inputs {
					for _, kv := range utils.ExtractLeafNodes(frame) {
						chunks = append(chunks, map[string]any{utils.FormatPath(kv.Path): kv.Value})
					}
				}
				result = map[string]any{"output": chunks}
			}
			logComp.Debug(logger.ComponentAgentCore).
				Str("component_id", sess.GetExecutableID()).
				Msg("End component collect succeed")
			return result, nil
		},
```

辅助函数（同文件非导出区）：

```go
// flattenChannelInputs 将流入的多帧合并为单 dict（同名后帧覆盖前帧）。
// Python 流入侧收的是 dict + AsyncGenerator 叶子（D-17-3），Go 统一为
// 帧 channel 后的等价聚合；流叶子在 RenderStream/consumeAllIterators 内消化。
func flattenChannelInputs(inputs <-chan map[string]any) map[string]any {
	merged := map[string]any{}
	for frame := range inputs {
		for k, v := range frame {
			merged[k] = v
		}
	}
	return merged
}
```

（`utils.ExtractLeafNodes`/`FormatPath` 返回形状以 `internal/common/utils/dict.go` 现状为准：实施时先读签名，若为 `(path []string, value any)` 元组切片则照用；通道叶子（chan 值）在 ExtractLeafNodes 中走 consumeAllIterators 同款 drain 后取末值或逐帧吐——实施时按 dict.go:77 既有实现形状写死并补一条对应测试。）

- [ ] **Step 4: 通过 → Step 5: 提交**

```bash
git add internal/agentcore/workflow/components/flow/end_comp.go internal/agentcore/workflow/components/flow/end_comp_test.go
git commit -m "feat(workflow): End.Transform/Collect 流入侧叶子抽取与聚合（8.17）"
```

---

### Task 12: End.render 的 mix 分支

**Files:**
- Modify: `internal/agentcore/workflow/components/flow/end_comp.go`（render 补 mix 分支）
- Test: `internal/agentcore/workflow/components/flow/end_comp_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestEnd_Render_Mix_首调用未渲染超时同步渲染兜底 验证 mix 首拍超时降级（end_comp.py:219-236）。
func TestEnd_Render_Mix_首调用未渲染超时同步渲染兜底(t *testing.T) {
	e, _ := NewEnd(EndConfigFromMap(map[string]any{"responseTemplate": "Hi {{n}}"}))
	e.SetMix()
	inner := session.NewWorkflowVertexSession(newFakeInnerSession(t), "n1", "End", false)
	// timeout 传极小正数触发超时路径（batchTemplate 未被第二拍渲染）
	out, err := e.render(context.Background(), map[string]any{"n": "x"}, 0.01, inner)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if out != nil && out["response"] == nil {
		t.Fatalf("mix 兜底返回不符: %v", out)
	}
	// 渲染态被消费后 batchTemplate 清空
	if e.batchTemplate != nil && !e.batchTemplate.isRendered {
		t.Fatal("超时后未渲染态应已清理")
	}
}

// TestEnd_Render_Mix_第二拍等待首拍渲染完成 验证 condition.wait 唤醒路径（end_comp.py:220-238）。
func TestEnd_Render_Mix_第二拍等待首拍渲染完成(t *testing.T) {
	e, _ := NewEnd(EndConfigFromMap(map[string]any{"responseTemplate": "Hi {{n}}"}))
	e.SetMix()
	inner := session.NewWorkflowVertexSession(newFakeInnerSession(t), "n1", "End", false)
	var wg sync.WaitGroup
	var first, second map[string]any
	wg.Add(2)
	go func() { defer wg.Done(); first, _ = e.render(context.Background(), map[string]any{"n": "a"}, 5, inner) }()
	time.Sleep(10 * time.Millisecond) // 保证首拍先建 batchTemplate
	go func() { defer wg.Done(); second, _ = e.render(context.Background(), map[string]any{"n": "b"}, 5, inner) }()
	wg.Wait()
	// 首拍返回 {"response": ...}；第二拍等渲染完成后返回 nil（end_comp.py:237-238）
	if first == nil || first["response"] == nil {
		t.Fatalf("首拍应返回渲染结果: %v", first)
	}
	if second != nil {
		t.Fatalf("第二拍应返回 nil: %v", second)
	}
}
```

- [ ] **Step 2: 失败 → Step 3: 实现（render 补 mix 分支，对齐 end_comp.py:219-238）**

```go
// render mix 分支（补入 Task 9 的 render 方法 batchTemplate==nil 分支内）：
	if e.batchTemplate == nil {
		e.batchTemplate = &TemplateBatchProcessor{template: e.template, inputs: inputs, notify: make(chan struct{})}
		if e.mix {
			bt := e.batchTemplate
			bt.mu.Lock()
			btCh := bt.notify
			bt.mu.Unlock()
			var waitDur time.Duration
			if t := timeout; t > 0 {
				waitDur = time.Duration(t * float64(time.Second))
			}
			timer := time.NewTimer(waitDur)
			select {
			case <-btCh: // 首拍渲染完成唤醒（notify_all）
				timer.Stop()
			case <-timer.C:
				// 渲染超时 error 日志（end_comp.py:225-231）
				logComp.Error(logger.ComponentAgentCore).
					Float64("timeout", t).
					Str("component_id", compIDOf(sess)).
					Msg("End component render timeout")
				if !bt.isRendered {
					// 未渲染过 → 同步渲染兜底（end_comp.py:232-236）
					answer, aerr := bt.render(ctx, nil, sess)
					e.batchTemplate = nil
					if aerr != nil {
						return nil, aerr
					}
					return map[string]any{"response": answer}, nil
				}
				return nil // 已渲染过 → 返回 None（end_comp.py:236）
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			e.batchTemplate = nil
			return nil // mix 首拍（渲染完成后）：返回 None（end_comp.py:237-238）
		}
	}
```

- [ ] **Step 4: 通过 → Step 5: 提交**

```bash
git add internal/agentcore/workflow/components/flow/end_comp.go internal/agentcore/workflow/components/flow/end_comp_test.go
git commit -m "feat(workflow): End.render mix 分支条件等待与超时降级（8.17）"
```

---

### Task 13: doc.go 更新 + 全量回归 + 计划表回填

**Files:**
- Modify: `internal/agentcore/workflow/components/doc.go`、`internal/agentcore/workflow/components/flow/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`（8.17 行 + 新增 8.14b 行 + 十五次记录注记）

- [ ] **Step 1: doc.go 文件树更新**

components/doc.go 补：
```
//	├── component.go     # WorkflowComponent 组件基类（四钩子适配+回调Trigger序列）
//	├── flow/            # 流程组件子目录（Start/End，对应 Python components/flow/）
```
flow/doc.go 补：
```
//	flow/
//	├── doc.go           # 包文档
//	├── start_comp.go    # Start 入口组件
//	└── end_comp.go      # End 出口组件（模板渲染状态机）
```

- [ ] **Step 2: 全量回归**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...
```
预期：零 FAIL。有 FAIL 先修复再继续。

- [ ] **Step 3: 覆盖率检查**

```bash
go test -cover ./internal/agentcore/workflow/components/... ./internal/agentcore/runner/callback/
```
预期：≥ 85%；不达标补测试。

- [ ] **Step 4: IMPLEMENTATION_PLAN.md 回填**

1. 8.17 行 `☐`→`✅`，附 Go 事实（component.go 四钩子+Trigger 序列、flow/ Start/End 全家、callback ComponentStreamOutput+TransformWorkflowIO、偏差 D-17-1~7 归档 spec §6）
2. 8.14 行后插入新行：`| 8.14b | ☐ | SubWorkflowComponent | 子工作流嵌套组件 | openjiuwen/core/workflow/components/flow/workflow_comp.py |`
3. 注记区追加：`> **8.x 实施顺序（2026-11-19 十五次记录）**：8.17 一次收口（方案 A+选项 2），下一节 8.14 → 8.14b → 8.18-8.26，8.13 继续压后（依据见 8.17 spec docs/superpowers/specs/2026-11-19-workflow-flow-components-8.17-design.md §2）。原注记保留。`

- [ ] **Step 5: 提交并推送**

```bash
git add internal/agentcore/workflow/components/doc.go internal/agentcore/workflow/components/flow/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs(workflow): 8.17 完成回填——doc.go 文件树+计划表状态+8.14b 新条目"
git push
```

---

## Self-Review 记录

1. **Spec 覆盖**：§5.1 基类→Task 3；§5.2 Start→Task 7、End 全家→Task 8-12、base.py 后三符号→Task 2；§5.3 callback→Task 1；§7 测试→各任务+Task 13；§8 计划表回填→Task 13；§9 无删除项。✔
2. **占位扫描**：无 TBD/TODO；Task 9 的 render 依赖处理为「本任务实现非 mix 分支」而非占位。✔
3. **类型一致性**：WorkflowComponent 函数槽签名在 Task 3/7/9/10/11 一致；templateFrame 在 Task 5/6/10 一致；TemplateBatchProcessor.notify 在 Task 9/12 一致。✔

## 实施注意（执行者必读）

- 提交纪律：**禁止 `git add -A`/`-am`**，每次显式 add 本任务文件；提交后 `git show --stat HEAD` 核对。
- 全量回归必须带标签：`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`。
- 遇到与既有 Vertex/fake 结构不匹配处（newFakeInnerSession、OutputSchema 字段名、错误码构造签名、ExtractLeafNodes 返回形状），**以仓内现状为准改测试辅助代码**，不得改生产对齐面；偏差记入 spec §6 追加编号。
- 日志 Msg 统一英文、注释统一中文（go-code-conventions）。
