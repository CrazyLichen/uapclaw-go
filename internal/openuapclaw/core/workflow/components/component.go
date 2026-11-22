package components

import (
	"context"
	"errors"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph"
	graphexe "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/executable"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ComponentComposable 组件组合契约：定义组件如何接入工作流图
// （Python component.py:236-283 ComponentComposable ABC；与执行面
// ComponentExecutable 双 mixin，修订偏差 D-17-1：单结构体 → 执行面基座嵌入 +
// 组合面接口/基座双形态承载）。
type ComponentComposable interface {
	// AddComponent 将自身注册进图（Python add_component，wait_for_all 默认 False）
	AddComponent(g *graph.PregelGraph, nodeID string, waitForAll bool) error
	// ToExecutable 产出可执行体（Python to_executable）
	ToExecutable() graphexe.GeneralExecutor
}

// BaseComponentComposable 组合面基座：纯组合件（只嵌组合面、无执行面）的默认实现
// 承载者，回调经 ToExecutableFn 槽（基座永远只调槽，不调外层——无基类调子类问题）。
type BaseComponentComposable struct {
	// ToExecutableFn 产物槽（Python to_executable 的可执行体供应点；nil ≙ 未实现）
	ToExecutableFn func() graphexe.GeneralExecutor
}

// WorkflowComponent 工作流组件基类：运行时面（四钩子适配）+ 构图面合成体。
// Python: components/component.py:285-306 (WorkflowComponent) —— ComponentExecutable
// 与 ComponentComposable 双 mixin 在 Go 以「执行面基座嵌入 + 组合面合成体」承载
// （修订偏差 D-17-1，8.14 spec §4：单结构体 → 双基座嵌入组合；组合面默认实现
// 落于合成体——ToExecutable 自返回 ≙ isinstance(self, Executable) 恒真，
// AddComponent ≙ 继承 mixin 默认实现，外层同名方法即可遮蔽 ≙ Python override）。
// 偏差 D-17-1：Python Input/Output TypeVar=Any → Go 具体化 map[string]any。
// 子类覆写方式：为四个函数槽赋值（Python 方法覆写 → Go 函数槽，8.10 AtomicNode 先例）；
// 未赋值的钩子走 BaseExecutable 默认未实现错误。
// 偏差 D-17-5：Python __init_subclass__ 经 emit_before/transform_io/emit_after
// 装饰器包装四钩子（component.py:22-73），Go 无类继承钩子，改为四钩子方法体内
// Trigger 等价序（前 Trigger → TransformIOInput → 原逻辑 → TransformIOOutput →
// 后 Trigger，2.14 Model 门面先例）。
type WorkflowComponent struct {
	graphexe.BaseExecutable[map[string]any, map[string]any]
	// BaseComponentComposable 组合面基座（Python ComponentComposable mixin 嵌入）
	BaseComponentComposable

	// InvokeFn 批入批出（Python invoke(inputs, Session(session, False), context)）
	InvokeFn func(ctx context.Context, inputs map[string]any, sess *session.NodeSessionFacade) (map[string]any, error)
	// StreamFn 批入流出（Python stream(inputs, Session(session, False), context)）
	StreamFn func(ctx context.Context, inputs map[string]any, sess *session.NodeSessionFacade) (<-chan graphexe.StreamChunk[any], error)
	// CollectFn 流入批出（Python collect(inputs, Session(session, True), context)）
	CollectFn func(ctx context.Context, inputs <-chan map[string]any, sess *session.NodeSessionFacade) (map[string]any, error)
	// TransformFn 流入流出（Python transform(inputs, Session(session, True), context)）
	TransformFn func(ctx context.Context, inputs <-chan map[string]any, sess *session.NodeSessionFacade) (<-chan graphexe.StreamChunk[any], error)
	// TraceMetadataFn 组件追踪元数据（Python trace_metadata，默认 nil 不产出）
	TraceMetadataFn func() map[string]any
}

// ──────────────────────────── WorkflowComponent 方法 ────────────────────────────

// ToExecutable 产出可执行体（Python component.py:263-283 to_executable）：
// 槽优先——ToExecutableFn 已设（子类注入 self）→ 返回槽产物（保住外层类型与
// GraphInvoker 等钩子，D-17-1"基座永远只调槽"先例贯彻）；未设 → 返回基座自身
// （向后兼容：Python isinstance(self, Executable) 恒真语义）。
func (c *WorkflowComponent) ToExecutable() graphexe.GeneralExecutor {
	if c.ToExecutableFn != nil {
		return c.ToExecutableFn()
	}
	return c
}

// AddComponent 将自身注册进图（Python component.py:248-260 默认实现落于合成体）。
// 槽优先同 ToExecutable：子类注入 ToExecutableFn=self 后无需 override 本方法与
// ToExecutable（8.14b/8.20 成对 override 惯例由本改动收口为单点槽注入）。
func (c *WorkflowComponent) AddComponent(g *graph.PregelGraph, nodeID string, waitForAll bool) error {
	prod := c.ToExecutable
	if c.ToExecutableFn != nil {
		prod = c.ToExecutableFn
	}
	opts := []graph.AddNodeOption{}
	if waitForAll {
		opts = append(opts, graph.WithWaitForAll())
	}
	g.AddNode(nodeID, prod(), opts...)
	return nil
}

// ──────────────────────────── BaseComponentComposable 方法 ────────────────────────────

// AddComponent 纯组合件默认实现（Python component.py:248-260；nil 槽 ≙
// to_executable NotImplementedError 分支，文案复刻去类名）。
func (m *BaseComponentComposable) AddComponent(g *graph.PregelGraph, nodeID string, waitForAll bool) error {
	if m.ToExecutableFn == nil {
		return errors.New("Component is missing required method: to_executable()\n" +
			"  → Expected signature: def to_executable(self) -> Executable")
	}
	opts := []graph.AddNodeOption{}
	if waitForAll {
		opts = append(opts, graph.WithWaitForAll())
	}
	g.AddNode(nodeID, m.ToExecutableFn(), opts...)
	return nil
}

// ToExecutable 纯组合件产物（Python to_executable 转发槽）。
func (m *BaseComponentComposable) ToExecutable() graphexe.GeneralExecutor {
	if m.ToExecutableFn == nil {
		return nil
	}
	return m.ToExecutableFn()
}

// ──────────────────────────── 占位待删────────────────────────────

// TraceMetadata 组件追踪元数据（Python component.py:124-135，默认 None）
func (c *WorkflowComponent) TraceMetadata() map[string]any {
	if c.TraceMetadataFn == nil {
		return nil
	}
	return c.TraceMetadataFn()
}

// OnInvoke 单进单出钩子：会话校验 → 包装组件级会话 → Trigger 序列 → 转调 InvokeFn。
// Python: component.py:95-99 on_invoke + 装饰器序列
// emit_before(COMPONENT_BATCH_INPUT)/transform_io/emit_after(COMPONENT_BATCH_OUTPUT)。
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

// OnStream 单进流出钩子：会话校验（streamMode=false）→ Trigger 序列 → 转调 StreamFn，
// 输出转换按帧应用（COMPONENT_STREAM_OUTPUT 逐项语义）。
// Python: component.py:101-107 on_stream。
func (c *WorkflowComponent) OnStream(ctx context.Context, inputs map[string]any, sess session.BaseSession, kwargs map[string]any) (<-chan graphexe.StreamChunk[any], error) {
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
	var ch <-chan graphexe.StreamChunk[any]
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

// OnCollect 流进单出钩子：会话校验（streamMode=true）→ Trigger 序列 → 转调 CollectFn。
// Python: component.py:109-114 on_collect。
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

// OnTransform 流进流出钩子：会话校验（streamMode=true）→ Trigger 序列 → 转调
// TransformFn，输出转换按帧应用。Python: component.py:116-122 on_transform。
func (c *WorkflowComponent) OnTransform(ctx context.Context, inputs <-chan map[string]any, sess session.BaseSession, kwargs map[string]any) (<-chan graphexe.StreamChunk[any], error) {
	compSess, err := session.NewComponentSession(sess, true)
	if err != nil {
		return nil, err
	}
	fw := callback.GetCallbackFramework()
	fw.TriggerWorkflow(ctx, &callback.WorkflowEventData{Event: callback.ComponentStreamInput, NodeID: compSess.GetComponentID()})
	var ch <-chan graphexe.StreamChunk[any]
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
// Python transform_io 对 AsyncIterator 输出的逐项 output_event 语义
// （events.py:143 COMPONENT_STREAM_OUTPUT 注释 "Fired for each item"）。
func transformStreamOutput(ctx context.Context, fw *callback.CallbackFramework, nodeID string, ch <-chan graphexe.StreamChunk[any]) <-chan graphexe.StreamChunk[any] {
	out := make(chan graphexe.StreamChunk[any])
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
