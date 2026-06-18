package tracer

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseWorkflowSession 工作流会话最小接口，避免依赖具体实现。
// 对应 Python TracerWorkflowUtils 中 session 参数的隐式接口（tracer/executable_id/parent_id/workflow_id/node_id/node_type/state/config）。
type BaseWorkflowSession interface {
	// Tracer 返回追踪器
	Tracer() *Tracer
	// ExecutableID 返回可执行标识
	ExecutableID() string
	// ParentID 返回父节点标识
	ParentID() string
	// WorkflowID 返回工作流标识
	WorkflowID() string
	// NodeID 返回节点标识
	NodeID() string
	// NodeType 返回节点类型
	NodeType() string
	// State 返回会话状态
	State() state.SessionState
	// Config 返回配置
	Config() any
}

// TracerWorkflowUtils 工作流追踪工具集，对应 Python TracerWorkflowUtils。
// 空结构体，所有方法均为静态风格（不持有状态）。
type TracerWorkflowUtils struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TraceWorkflowStart 追踪工作流开始，对应 Python TracerWorkflowUtils.trace_workflow_start。
// 调用 TriggerWorkflow(TraceWFCallStart, parentNodeID="", metadata=工作流元数据, inputs=inputs, needSend=true)。
func (TracerWorkflowUtils) TraceWorkflowStart(ctx context.Context, session BaseWorkflowSession, inputs any) {
	if session.Tracer() == nil {
		return
	}
	session.Tracer().TriggerWorkflow(ctx, TraceWFCallStart, "", &TriggerParams{
		InvokeID: session.WorkflowID(),
		Inputs:   inputs,
		Metadata: getWorkflowMetadata(session),
		NeedSend: true,
	})
}

// TraceComponentBegin 追踪组件开始，对应 Python TracerWorkflowUtils.trace_component_begin。
// 调用 TriggerWorkflow(TraceWFCallStart, parentNodeID=session.ParentID(), invokeID=session.ExecutableID(), sourceIDs=sourceIDs, metadata=组件元数据)。
func (TracerWorkflowUtils) TraceComponentBegin(ctx context.Context, session BaseWorkflowSession, sourceIDs []string) {
	if session.Tracer() == nil {
		return
	}
	session.Tracer().TriggerWorkflow(ctx, TraceWFCallStart, session.ParentID(), &TriggerParams{
		InvokeID:  session.ExecutableID(),
		SourceIDs: sourceIDs,
		Metadata:  getComponentMetadata(session),
	})
}

// TraceComponentInputs 追踪组件输入，对应 Python TracerWorkflowUtils.trace_component_inputs。
// 调用 TriggerWorkflow(TraceWFPreInvoke, ...)。
func (TracerWorkflowUtils) TraceComponentInputs(ctx context.Context, session BaseWorkflowSession, inputs any, send bool) {
	if session.Tracer() == nil {
		return
	}
	session.Tracer().TriggerWorkflow(ctx, TraceWFPreInvoke, session.ParentID(), &TriggerParams{
		InvokeID:          session.ExecutableID(),
		Inputs:            inputs,
		NeedSend:          send,
		ComponentMetadata: getComponentMetadata(session),
	})
}

// TraceComponentStreamInput 追踪组件流式输入，对应 Python TracerWorkflowUtils.trace_component_stream_input。
// chunk 为 string 时跳过（与 Python isinstance(chunk, str) 一致）。
// 调用 TriggerWorkflow(TraceWFPreStream, ...)。
func (TracerWorkflowUtils) TraceComponentStreamInput(ctx context.Context, session BaseWorkflowSession, chunk any, send bool) {
	if session.Tracer() == nil {
		return
	}
	if _, ok := chunk.(string); ok {
		return
	}
	session.Tracer().TriggerWorkflow(ctx, TraceWFPreStream, session.ParentID(), &TriggerParams{
		InvokeID: session.ExecutableID(),
		Chunk:    chunk,
		NeedSend: send,
	})
}

// TraceComponentOutputs 追踪组件输出，对应 Python TracerWorkflowUtils.trace_component_outputs。
// 调用 TriggerWorkflow(TraceWFPostInvoke, ...)。
func (TracerWorkflowUtils) TraceComponentOutputs(ctx context.Context, session BaseWorkflowSession, outputs any) {
	if session.Tracer() == nil {
		return
	}
	session.Tracer().TriggerWorkflow(ctx, TraceWFPostInvoke, session.ParentID(), &TriggerParams{
		InvokeID: session.ExecutableID(),
		Outputs:  outputs,
	})
}

// TraceComponentStreamOutput 追踪组件流式输出，对应 Python TracerWorkflowUtils.trace_component_stream_output。
// chunk 为 string 时跳过（与 Python isinstance(chunk, str) 一致）。
// 调用 TriggerWorkflow(TraceWFPostStream, ...)。
func (TracerWorkflowUtils) TraceComponentStreamOutput(ctx context.Context, session BaseWorkflowSession, chunk any) {
	if session.Tracer() == nil {
		return
	}
	if _, ok := chunk.(string); ok {
		return
	}
	session.Tracer().TriggerWorkflow(ctx, TraceWFPostStream, session.ParentID(), &TriggerParams{
		InvokeID: session.ExecutableID(),
		Chunk:    chunk,
	})
}

// TraceWorkflowDone 追踪工作流完成，对应 Python TracerWorkflowUtils.trace_workflow_done。
// 调用 TriggerWorkflow(TraceWFCallDone, parentNodeID="", outputs=outputs, metadata=工作流元数据)。
func (TracerWorkflowUtils) TraceWorkflowDone(ctx context.Context, session BaseWorkflowSession, outputs any) {
	if session.Tracer() == nil {
		return
	}
	session.Tracer().TriggerWorkflow(ctx, TraceWFCallDone, "", &TriggerParams{
		InvokeID: session.WorkflowID(),
		Outputs:  outputs,
		Metadata: getWorkflowMetadata(session),
	})
}

// TraceComponentDone 追踪组件完成，对应 Python TracerWorkflowUtils.trace_component_done。
// 调用 TriggerWorkflow(TraceWFCallDone, ...)，然后 PopWorkflowSpan。
// 循环组件的 PopWorkflowSpan 处理暂用 TODO 标注，后续回填。
func (TracerWorkflowUtils) TraceComponentDone(ctx context.Context, session BaseWorkflowSession) {
	if session.Tracer() == nil {
		return
	}
	executableID := session.ExecutableID()
	parentID := session.ParentID()
	session.Tracer().TriggerWorkflow(ctx, TraceWFCallDone, parentID, &TriggerParams{
		InvokeID: executableID,
	})

	// TODO: 循环组件的 PopWorkflowSpan 处理，需从 state.GetGlobal(LOOP_ID) 获取 loop_id，
	// 当 loop_id 非空时执行 PopWorkflowSpan。后续回填。

	session.Tracer().PopWorkflowSpan(executableID, parentID)
}

// Trace 追踪运行时数据，对应 Python TracerWorkflowUtils.trace。
// 调用 TriggerWorkflow(TraceWFInvoke, ...)。
func (TracerWorkflowUtils) Trace(ctx context.Context, session BaseWorkflowSession, data map[string]any) {
	if session.Tracer() == nil {
		return
	}
	session.Tracer().TriggerWorkflow(ctx, TraceWFInvoke, session.ParentID(), &TriggerParams{
		InvokeID:     session.ExecutableID(),
		OnInvokeData: data,
	})
}

// TraceError 追踪错误，对应 Python TracerWorkflowUtils.trace_error。
// 调用 TriggerWorkflow(TraceWFInvoke, ..., exception=err)。
func (TracerWorkflowUtils) TraceError(ctx context.Context, session BaseWorkflowSession, err error) {
	if session.Tracer() == nil {
		return
	}
	session.Tracer().TriggerWorkflow(ctx, TraceWFInvoke, session.ParentID(), &TriggerParams{
		InvokeID: session.ExecutableID(),
		Error:    err,
	})
}

// TraceComponentInteractiveInputs 追踪组件交互式输入，对应 Python TracerWorkflowUtils.trace_component_interactive_inputs。
// 调用 TriggerWorkflow(TraceWFInteract, ...)。
func (TracerWorkflowUtils) TraceComponentInteractiveInputs(ctx context.Context, session BaseWorkflowSession, inputs any, send bool) {
	if session.Tracer() == nil {
		return
	}
	session.Tracer().TriggerWorkflow(ctx, TraceWFInteract, session.ParentID(), &TriggerParams{
		InvokeID:          session.ExecutableID(),
		Inputs:            inputs,
		NeedSend:          send,
		ComponentMetadata: getComponentMetadata(session),
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getWorkflowMetadata 获取工作流元数据，对应 Python TracerWorkflowUtils._get_workflow_metadata。
// 返回 workflow_id/workflow_version/workflow_name。
func getWorkflowMetadata(session BaseWorkflowSession) map[string]any {
	workflowID := session.WorkflowID()
	// Python 中从 config 获取 workflow_config.card 提取 version 和 name，
	// Go 中 Config() 返回 any，暂时只填充基础字段，后续回填 config 解析逻辑。
	return map[string]any{
		"workflow_id":      workflowID,
		"workflow_version": "",
		"workflow_name":    "",
	}
}

// getComponentMetadata 获取组件元数据，对应 Python TracerWorkflowUtils._get_component_metadata。
// 返回 component_id/component_name/component_type/workflow_id。
// loop_node_id/loop_index 部分用 TODO 标注后续回填。
func getComponentMetadata(session BaseWorkflowSession) map[string]any {
	metadata := map[string]any{
		"component_id":   session.NodeID(),
		"component_name": session.NodeID(),
		"component_type": session.NodeType(),
		"workflow_id":    session.WorkflowID(),
	}

	// TODO: 循环组件的 loop_node_id/loop_index 逻辑，
	// 需从 state.GetGlobal(LOOP_ID) 获取 loop_id，
	// 当 loop_id 非空时追加 loop_node_id 和 loop_index 字段。后续回填。

	return metadata
}
