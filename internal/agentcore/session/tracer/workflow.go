package tracer

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WorkflowNodeSession 工作流节点会话接口，TracerWorkflowUtils 方法的 session 参数类型。
//
// 对应 Python TracerWorkflowUtils 中 session 参数的隐式接口（duck typing），
// Python 不定义接口，Go 必须显式定义。
//
// 仅 *internal.NodeSession（及嵌入它的 *SubWorkflowSession）满足此接口，
// WorkflowSession 不满足（缺少 ExecutableID/ParentID/NodeID/NodeType）。
//
// 与 interfaces.InnerSession + Provider 的映射关系：
//
//	Tracer()       → InnerSession.Tracer()
//	State()        → InnerSession.State()
//	Config()       → InnerSession.Config()
//	WorkflowID()   → WorkflowIDProvider.WorkflowID()
//	ExecutableID() → ExecutableIDProvider.ExecutableID()
//	NodeID()       → 无对应 Provider（需补充）
//	NodeType()     → 无对应 Provider（需补充）
//	ParentID()     → 无对应 Provider（需补充）
//
// 此接口定义在 tracer 包内（而非 interfaces 包），因为 Tracer() 返回 *Tracer
// 导致 tracer 无法导入 interfaces（interfaces 已导入 tracer，会形成循环）。
type WorkflowNodeSession interface {
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
	Config() config.SessionConfig
}

// TracerWorkflowUtils 工作流追踪工具集，对应 Python TracerWorkflowUtils。
// 空结构体，所有方法均为静态风格（不持有状态）。
type TracerWorkflowUtils struct{}

// ──────────────────────────── 常量 ────────────────────────────

// loopID 循环节点标识的 state 全局键，对应 Python LOOP_ID = "__sys_loop_id"
// （openjiuwen/core/common/constants/constant.py）
// 循环组件（8.20）执行时将 loop_node_id 写入 state.GetGlobal(loopID)，
// 此处读取以判断当前组件是否处于循环中。
const loopID = "__sys_loop_id"

// loopIndexSuffix 循环索引的后缀，对应 Python INDEX = "index" + NESTED_PATH_SPLIT = "."
// 完整 key 为 loopID_value + "." + "index"，如 "loop_node_1.index"
const loopIndexSuffix = ".index"

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TraceWorkflowStart 追踪工作流开始，对应 Python TracerWorkflowUtils.trace_workflow_start。
// 调用 TriggerWorkflow(TraceWFCallStart, parentNodeID="", metadata=工作流元数据, inputs=inputs, needSend=true)。
func (TracerWorkflowUtils) TraceWorkflowStart(ctx context.Context, session WorkflowNodeSession, inputs any) {
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
func (TracerWorkflowUtils) TraceComponentBegin(ctx context.Context, session WorkflowNodeSession, sourceIDs []string) {
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
func (TracerWorkflowUtils) TraceComponentInputs(ctx context.Context, session WorkflowNodeSession, inputs any, send bool) {
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
func (TracerWorkflowUtils) TraceComponentStreamInput(ctx context.Context, session WorkflowNodeSession, chunk any, send bool) {
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
func (TracerWorkflowUtils) TraceComponentOutputs(ctx context.Context, session WorkflowNodeSession, outputs any) {
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
func (TracerWorkflowUtils) TraceComponentStreamOutput(ctx context.Context, session WorkflowNodeSession, chunk any) {
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
func (TracerWorkflowUtils) TraceWorkflowDone(ctx context.Context, session WorkflowNodeSession, outputs any) {
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
// TraceComponentDone 组件执行完成追踪，对应 Python TracerWorkflowUtils.trace_component_done。
// 调用 TriggerWorkflow(TraceWFCallDone, ...)。
// Python 中 loop_id 非 None 时才执行 pop_workflow_span，非循环组件不 pop。
// 当前循环组件（8.20）未实现，loop_id 暂时为空，不执行 PopWorkflowSpan。
// 8.20 实现后，循环组件在 state 中写入 LOOP_ID，此处读取到非空值后执行 PopWorkflowSpan。
func (TracerWorkflowUtils) TraceComponentDone(ctx context.Context, session WorkflowNodeSession) {
	if session.Tracer() == nil {
		return
	}
	executableID := session.ExecutableID()
	parentID := session.ParentID()
	session.Tracer().TriggerWorkflow(ctx, TraceWFCallDone, parentID, &TriggerParams{
		InvokeID: executableID,
	})

	// 对齐 Python: loop_id = state.get_global(LOOP_ID); if loop_id is None: return
	// 循环组件未实现前（8.20），state.GetGlobal(loopID) 返回 nil，不执行 PopWorkflowSpan。
	// 8.20 实现后，循环组件在 state 中写入 loopID，此处读取到非空值后执行 PopWorkflowSpan。
	// ⤵️ 8.20 回填：从 state.GetGlobal(loopID) 获取 loop_id，
	// 当 loop_id 非空时执行 PopWorkflowSpan。
}

// Trace 追踪运行时数据，对应 Python TracerWorkflowUtils.trace。
// 调用 TriggerWorkflow(TraceWFInvoke, ...)。
func (TracerWorkflowUtils) Trace(ctx context.Context, session WorkflowNodeSession, data map[string]any) {
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
// err 为 nil 时静默返回，防御性避免空错误触发追踪事件。
func (TracerWorkflowUtils) TraceError(ctx context.Context, session WorkflowNodeSession, err error) {
	if session.Tracer() == nil {
		return
	}
	if err == nil {
		return
	}
	session.Tracer().TriggerWorkflow(ctx, TraceWFInvoke, session.ParentID(), &TriggerParams{
		InvokeID: session.ExecutableID(),
		Error:    err,
	})
}

// TraceComponentInteractiveInputs 追踪组件交互式输入，对应 Python TracerWorkflowUtils.trace_component_interactive_inputs。
// 调用 TriggerWorkflow(TraceWFInteract, ...)。
func (TracerWorkflowUtils) TraceComponentInteractiveInputs(ctx context.Context, session WorkflowNodeSession, inputs any, send bool) {
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
// ⤵️ 5.12 回填：workflow_version/workflow_name 当前硬编码为空字符串，
// Python 从 session.config().get_workflow_config(executable_id).card 提取 version 和 name，
// Go 中 Config() 返回 any，无法调用 get_workflow_config。
func getWorkflowMetadata(session WorkflowNodeSession) map[string]any {
	workflowID := session.WorkflowID()
	metadata := map[string]any{
		"workflow_id":      workflowID,
		"workflow_version": "",
		"workflow_name":    "",
	}

	cfg := session.Config()
	if cfg == nil {
		return metadata
	}
	wfc := cfg.GetWorkflowConfig(workflowID)
	if wfc == nil {
		return metadata
	}
	// ⤵️ 8.15 回填：WorkflowConfig 实现后，从 wfc 提取 card.version 和 card.name
	return metadata
}

// getComponentMetadata 获取组件元数据，对应 Python TracerWorkflowUtils._get_component_metadata。
// 返回 component_id/component_name/component_type/workflow_id。
// 当循环组件写入 LOOP_ID 后，额外返回 loop_node_id/loop_index。
// 对齐 Python:
//
//	loop_id = state.get_global(LOOP_ID)
//	if loop_id is None: return component_metadata
//	index = state.get_global(loop_id + NESTED_PATH_SPLIT + INDEX)
//	component_metadata.update({"loop_node_id": loop_id, "loop_index": index})
func getComponentMetadata(session WorkflowNodeSession) map[string]any {
	metadata := map[string]any{
		"component_id":   session.NodeID(),
		"component_name": session.NodeID(),
		"component_type": session.NodeType(),
		"workflow_id":    session.WorkflowID(),
	}

	// 对齐 Python: loop_id = state.get_global(LOOP_ID)
	if session.State() != nil {
		loopIDVal := session.State().GetGlobal(state.StringKey(loopID))
		if loopIDVal != nil {
			if loopIDStr, ok := loopIDVal.(string); ok && loopIDStr != "" {
				// 对齐 Python: index = state.get_global(loop_id + NESTED_PATH_SPLIT + INDEX)
				indexKey := loopIDStr + loopIndexSuffix
				indexVal := session.State().GetGlobal(state.StringKey(indexKey))
				metadata["loop_node_id"] = loopIDStr
				metadata["loop_index"] = indexVal
			}
		}
	}

	return metadata
}
