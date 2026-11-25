// 8.26 层 2 ReAct 工具级中断桥——判定/提取/收集/存取方法族。
// Python: openjiuwen/core/single_agent/agents/react_agent.py L2515-1623 +
// L2625-1657（_is_interrupted / _extract_component_ids / _extract_workflow_id /
// _after_execute_tool_call / _save·_load·_clear_interruption_state /
// _build_interrupt_result / _commit_interrupt workflow 分支）。
// 与 HITL 中断链（ToolInterruptionState / hitlHandler）并存，按状态类型分派
// （spec 2026-11-24-react-comp-8.26-design.md §3，偏差 D-26-1 双入口承载）。
package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	ceinterface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/constants"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	interfaces2 "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/workflowbase"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// isInterrupted 判定工具结果是否为工作流中断。
// WorkflowOutput 且 State==INPUT_REQUIRED；或 list 中存在 __interaction__ 块。
// Python: react_agent.py _is_interrupted L2515-1522。
func (a *ReActAgent) isInterrupted(toolResult any) bool {
	if out, ok := toolResult.(*workflowbase.WorkflowOutput); ok {
		return out.State == workflowbase.WorkflowExecutionStateInputRequired
	}
	if list, ok := toolResult.([]stream.Schema); ok {
		for _, item := range list {
			if item != nil && item.SchemaType() == interaction.InteractionType {
				return true
			}
		}
	}
	return false
}

// extractComponentIDs 从被中断的工作流结果提取组件 ID（sorted 稳定序）。
// WorkflowOutput.result 扫 __interaction__ 取 payload.id；
// list 分支取 payload["component_id"]。
// 帧形态双收：workflow.go Invoke 收集的 chunk 值类型 stream.OutputSchema 与
// 指针 *stream.OutputSchema 都会出现（8.24 questioner 套件实证值类型）。
// Python: react_agent.py _extract_component_ids L2525-1552。
func (a *ReActAgent) extractComponentIDs(toolResult any) []string {
	if out, ok := toolResult.(*workflowbase.WorkflowOutput); ok {
		if list, ok := out.Result.([]stream.Schema); ok {
			ids := make([]string, 0)
			for _, item := range list {
				if item == nil || item.SchemaType() != interaction.InteractionType {
					continue
				}
				if os, ok := item.(*stream.OutputSchema); ok {
					if id, ok := interactionPayloadID(os.Payload); ok {
						ids = append(ids, id)
					}
				} else if osv, ok := item.(stream.OutputSchema); ok {
					if id, ok := interactionPayloadID(osv.Payload); ok {
						ids = append(ids, id)
					}
				}
			}
			sort.Strings(ids)
			return ids
		}
		return []string{}
	}
	if list, ok := toolResult.([]stream.Schema); ok {
		ids := make([]string, 0)
		for _, item := range list {
			if item == nil || item.SchemaType() != interaction.InteractionType {
				continue
			}
			if os, ok := item.(*stream.OutputSchema); ok {
				if id, ok := interactionPayloadComponentID(os.Payload); ok {
					ids = append(ids, id)
				}
			} else if osv, ok := item.(stream.OutputSchema); ok {
				if id, ok := interactionPayloadComponentID(osv.Payload); ok {
					ids = append(ids, id)
				}
			}
		}
		sort.Strings(ids)
		return ids
	}
	return []string{}
}

// interactionPayloadID 从交互帧 payload 提取组件 ID（WorkflowOutput 分支，
// Python payload.id 属性访问）。InteractionOutput 值/指针双形态 + map 兜底。
func interactionPayloadID(payload any) (string, bool) {
	switch p := payload.(type) {
	case interaction.InteractionOutput:
		return p.ID, true
	case *interaction.InteractionOutput:
		return p.ID, true
	case map[string]any:
		return fmt.Sprint(p["id"]), true
	}
	return "", false
}

// interactionPayloadComponentID 从交互帧 payload 提取组件 ID（list 分支，
// Python payload["component_id"]）。双形态同 interactionPayloadID。
func interactionPayloadComponentID(payload any) (string, bool) {
	switch p := payload.(type) {
	case interaction.InteractionOutput:
		return p.ID, true
	case *interaction.InteractionOutput:
		return p.ID, true
	case map[string]any:
		return fmt.Sprint(p["component_id"]), true
	}
	return "", false
}

// extractWorkflowID 经 ability_manager 按 tool_call.name 反查 workflow_id。
// miss 时 fallback tool_call.name。
// Python: react_agent.py _extract_workflow_id L2531-1536。
func (a *ReActAgent) extractWorkflowID(toolCall *llmschema.ToolCall) string {
	am := a.getAbilityManager()
	if am != nil {
		for _, ability := range am.List() {
			if ability.AbilityName() == toolCall.Name {
				return ability.AbilityID()
			}
		}
	}
	return toolCall.Name
}

// afterExecuteToolCall 工具执行后收集全部被中断工作流，构建 InterruptionState。
// pending（待处理项）取第一个被中断项（component_ids[0]，空则 ""）；无中断返回 nil。
// 与 AfterExecuteToolCallForHITL（HITL 链）并存，本方法只承载 workflow 中断。
// Python: react_agent.py _after_execute_tool_call L2554-1596。
func (a *ReActAgent) afterExecuteToolCall(
	results []agentschema.ExecuteResult,
	toolCalls []*llmschema.ToolCall,
	aiMessage *llmschema.AssistantMessage,
	iteration int,
	originalQuery string,
) *agentschema.InterruptionState {
	interrupted := make(map[string]*agentschema.WorkflowInterruptEntry)
	firstWorkflowID := ""
	firstComponentID := ""
	for i := range results {
		toolResult := results[i].Result
		if !a.isInterrupted(toolResult) {
			continue
		}
		workflowID := a.extractWorkflowID(toolCalls[i])
		componentIDs := a.extractComponentIDs(toolResult)
		// 8.26 收窄回填：toolResult 边界仍 any（executeToolCalls 闭包），此处
		// isInterrupted 已证 WorkflowOutput 形态，断言收窄后强类型入 entry
		wfOut, _ := toolResult.(*workflowbase.WorkflowOutput)
		interrupted[workflowID] = &agentschema.WorkflowInterruptEntry{
			ToolCall:               toolCalls[i],
			ComponentIDs:           componentIDs,
			WorkflowExecutionState: wfOut,
		}
		if firstWorkflowID == "" {
			firstWorkflowID = workflowID
			if len(componentIDs) > 0 {
				firstComponentID = componentIDs[0]
			}
		}
	}
	if len(interrupted) == 0 {
		return nil
	}
	return &agentschema.InterruptionState{
		BaseInterruptionState: agentschema.BaseInterruptionState{
			AIMessage:     aiMessage,
			Iteration:     iteration,
			OriginalQuery: originalQuery,
		},
		InterruptedWorkflows: interrupted,
		PendingWorkflowID:    firstWorkflowID,
		PendingComponentID:   firstComponentID,
	}
}

// saveInterruptionState 持久化工作流中断态。
// Python: react_agent.py _save_interruption_state L2611-1613。
func (a *ReActAgent) saveInterruptionState(st *agentschema.InterruptionState, sess sessioninterfaces.SessionFacade) {
	if sess == nil {
		return
	}
	sess.UpdateState(map[string]any{agentschema.InterruptionKey: st})
}

// loadInterruptionState 加载工作流中断态。
// Python: react_agent.py _load_interruption_state L2615-1618。
func (a *ReActAgent) loadInterruptionState(sess sessioninterfaces.SessionFacade) *agentschema.InterruptionState {
	if sess == nil {
		return nil
	}
	val, _ := sess.GetState(state.StringKey(agentschema.InterruptionKey))
	if val == nil {
		return nil
	}
	if st, ok := val.(*agentschema.InterruptionState); ok {
		return st
	}
	// checkpoint 持久化往返把结构体指针降级为 map（questioner_state 同款问题，
	// 其用 Serialize/Deserialize 承载）——JSON 重建恢复语义。
	data, err := json.Marshal(val)
	if err != nil {
		logger.Error(logComponent).Err(err).
			Str("action", "load_interruption_state").
			Msg("failed to marshal persisted interruption state for rebuild")
		return nil
	}
	st := &agentschema.InterruptionState{}
	if err := json.Unmarshal(data, st); err != nil {
		logger.Error(logComponent).Err(err).
			Str("action", "load_interruption_state").
			Msg("failed to unmarshal persisted interruption state")
		return nil
	}
	// WorkflowInterruptEntry.WorkflowExecutionState 同被降级为 map——
	// 重建回 *workflowbase.WorkflowOutput（下游 isInterrupted/流写入断言依赖）。
	for _, entry := range st.InterruptedWorkflows {
		if entry == nil || entry.WorkflowExecutionState == nil {
			continue
		}
		// 字段已强类型 *workflowbase.WorkflowOutput；此处处理 checkpoint JSON
		// 往返把 Result 降级为 []any 的情形（帧重建）
		if entry.WorkflowExecutionState.Result != nil {
			if _, isList := entry.WorkflowExecutionState.Result.([]stream.Schema); isList {
				continue
			}
		}
		blob, err := json.Marshal(entry.WorkflowExecutionState)
		if err != nil {
			continue
		}
		out := &workflowbase.WorkflowOutput{}
		if err := json.Unmarshal(blob, out); err != nil {
			continue
		}
		// Result 里的帧同样是值形态 map/[]any——重建回 []stream.Schema
		if rawList, ok := out.Result.([]any); ok {
			schemas := make([]stream.Schema, 0, len(rawList))
			for _, raw := range rawList {
				b, err := json.Marshal(raw)
				if err != nil {
					continue
				}
				os := &stream.OutputSchema{}
				if err := json.Unmarshal(b, os); err != nil {
					continue
				}
				schemas = append(schemas, os)
			}
			out.Result = schemas
		}
		entry.WorkflowExecutionState = out
	}
	return st
}

// clearInterruptionState 清除工作流中断态。
// Python: react_agent.py _clear_interruption_state L2620-1623。
func (a *ReActAgent) clearInterruptionState(sess sessioninterfaces.SessionFacade) {
	if sess == nil {
		return
	}
	sess.UpdateState(map[string]any{agentschema.InterruptionKey: nil})
}

// buildInterruptResult 组装返回给调用方的 interrupt 结果 dict。
// Python: react_agent.py _build_interrupt_result L2625-1631。
func (a *ReActAgent) buildInterruptResult(st *agentschema.InterruptionState) map[string]any {
	pendingEntry := st.InterruptedWorkflows[st.PendingWorkflowID]
	return map[string]any{
		"result_type":              "interrupt",
		"workflow_execution_state": pendingEntry.WorkflowExecutionState,
		"component_ids":            []string{st.PendingComponentID},
	}
}

// CommitWorkflowInterrupt 提交工作流中断：写占位 ToolMessage + 保存上下文 + 存态 +
// 构建 interrupt 结果并设置 invokeInputs.result（D-26-1 双入口：与 HITL 的
// CommitInterrupt 并存，调用方按状态类型分派）。
// Python: react_agent.py _commit_interrupt L2633-1657 的非 HITL 分支。
func (a *ReActAgent) CommitWorkflowInterrupt(
	ctx context.Context,
	st *agentschema.InterruptionState,
	modelCtx ceinterface.ModelContext,
	sess sessioninterfaces.SessionFacade,
	invokeInputs *interfaces.InvokeInputs,
) (map[string]any, error) {
	pendingEntry := st.InterruptedWorkflows[st.PendingWorkflowID]
	// 占位 ToolMessage（Python L2645-1650）
	if modelCtx != nil && pendingEntry != nil && pendingEntry.ToolCall != nil {
		toolMsg := llmschema.NewToolMessage(pendingEntry.ToolCall.ID, "[INTERRUPTED - Waiting for user input]")
		if _, err := modelCtx.AddMessages(ctx, []llmschema.BaseMessage{toolMsg}); err != nil {
			logger.Error(logComponent).Err(err).Msg("commit_workflow_interrupt: failed to add placeholder tool message")
			return nil, fmt.Errorf("commit_workflow_interrupt: 写入占位 ToolMessage 失败: %w", err)
		}
	}
	// 持久化上下文引擎状态（Python L2655 save_contexts；HITL 分支同款守卫）
	if ce := a.ContextEngine(); ce != nil && sess != nil {
		if _, err := ce.SaveContexts(ctx, sess, nil); err != nil {
			logger.Error(logComponent).Err(err).Msg("commit_workflow_interrupt: failed to save context")
			return nil, fmt.Errorf("commit_workflow_interrupt: 保存上下文失败: %w", err)
		}
	}
	// 保存中断状态到 session（Python L2657）
	a.saveInterruptionState(st, sess)
	// 构建中断结果并设置 invokeInputs.result（Python L2659-1660）
	result := a.buildInterruptResult(st)
	if invokeInputs != nil {
		invokeInputs.Result = result
	}
	return result, nil
}

// handleResumeWorkflow 处理一轮工作流中断恢复。
// 步1 记录当前 pending 反馈；步2 未全收齐 → 轮转下一个未收集 workflow 并再 commit；
// 步3 全收齐 → 回写 ai_message + 恢复输入对象旁路注入并并发恢复全部 workflow →
// 再中断则再 commit；全完成设置 ResumeStartIterationKey 续跑循环并返回 nil。
// Python: react_agent.py _handle_resume L2663-1740（workflow 分支；HITL 分支由
// hitlHandler.HandleResume 承载，本方法只接 InterruptionState）。
func (a *ReActAgent) handleResumeWorkflow(
	ctx context.Context,
	st *agentschema.InterruptionState,
	userInput any,
	cbc *interfaces.AgentCallbackContext,
	modelCtx ceinterface.ModelContext,
	sess sessioninterfaces.SessionFacade,
	invokeInputs *interfaces.InvokeInputs,
) (map[string]any, error) {
	resumeIteration := st.BaseInterruptionState.Iteration
	logger.Info(logComponent).Int("iteration", resumeIteration+1).
		Msg("Resuming ReAct from iteration") // Python L2673

	// 步1：记录当前 pending 反馈（Python L2679-1682）
	pendingEntry := st.InterruptedWorkflows[st.PendingWorkflowID]
	interactiveInput := a.buildInteractiveInput(userInput, []string{st.PendingComponentID})
	pendingEntry.CollectedInput = interactiveInput

	// 步2：检查是否全部收齐（Python L2684-1694）
	// map 迭代序 Go 不确定，但判定只依赖"是否存在未收集项"，顺序无语义影响
	allCollected := true
	for _, entry := range st.InterruptedWorkflows {
		if entry.CollectedInput == nil {
			allCollected = false
			break
		}
	}
	if !allCollected {
		// 轮转到下一个未收集 workflow（Python L2687-1694）
		for wfID, entry := range st.InterruptedWorkflows {
			if entry.CollectedInput != nil {
				continue
			}
			nextCompID := ""
			if len(entry.ComponentIDs) > 0 {
				nextCompID = entry.ComponentIDs[0]
			}
			st.PendingWorkflowID = wfID
			st.PendingComponentID = nextCompID
			return a.CommitWorkflowInterrupt(ctx, st, modelCtx, sess, invokeInputs)
		}
	}

	// 步3：全部收齐 → 回写 ai_message + 并发恢复全部 workflow（Python L2696-1734）
	resumeAIMessage := deepCloneAssistantMessage(st.BaseInterruptionState.AIMessage) // D-26-2
	if modelCtx != nil {
		if _, err := modelCtx.AddMessages(ctx, []llmschema.BaseMessage{resumeAIMessage}); err != nil {
			logger.Error(logComponent).Err(err).Msg("handle_resume: failed to add resume ai message")
			return nil, fmt.Errorf("handle_resume: 回写 ai_message 失败: %w", err)
		}
	}
	allToolCalls := make([]*llmschema.ToolCall, 0, len(st.InterruptedWorkflows))
	// D-26-5（重写版）：恢复输入对象旁路通道。
	// Python 经 tc_copy.arguments = InteractiveInput 对象走私（pydantic 不校验
	// 赋值 + ability_manager _parse_tool_arguments_with_repair isinstance(str)
	// 短路透传，react_agent.py L2707-1708）；Go 的 string Arguments 无法承载
	// 对象，改由 cbc extra {tool_call_id: InteractiveInput} 承载——起点、终点
	// （run_workflow(inputs=对象)）与 finally 清理语义同 Python，仅载体异。
	// 注意 Arguments 保持原值不动（对象通道命中时解析器跳过，内容无消费方）。
	resumeInputs := make(map[string]any, len(st.InterruptedWorkflows))
	for _, entry := range st.InterruptedWorkflows {
		tc := deepCloneToolCall(entry.ToolCall) // D-26-2 深拷贝
		if tc != nil {
			allToolCalls = append(allToolCalls, tc)
		}
		if entry.CollectedInput != nil {
			callID := ""
			if tc != nil {
				callID = tc.ID
			}
			resumeInputs[callID] = entry.CollectedInput
		}
	}
	if len(resumeInputs) > 0 {
		cbc.SetExtra(agentschema.WorkflowResumeInputsKey, resumeInputs)
	}
	// finally 清理（Python handler.py L372-1377 finally pop 同语义：
	// 无论重执行成功、失败还是再中断，恢复输入都不残留到下一轮）
	defer cbc.DeleteExtra(agentschema.WorkflowResumeInputsKey)
	// 并发恢复由 executeToolCalls 内部并行承载（Python L2725 _execute_tool_call 并发；
	// D-26-3：Go goroutine 并发语义与 executeToolCalls 既有实现一致）
	results, err := a.executeToolCalls(ctx, cbc, allToolCalls, sess, modelCtx)
	if err != nil {
		return nil, err
	}
	// 恢复后再判中断（Python L2728-1733）
	workflowInterrupt := a.afterExecuteToolCall(results, allToolCalls, resumeAIMessage, resumeIteration, st.BaseInterruptionState.OriginalQuery)
	if workflowInterrupt != nil {
		return a.CommitWorkflowInterrupt(ctx, workflowInterrupt, modelCtx, sess, invokeInputs)
	}
	// 全完成：设置续跑起点（Python L2736-1738）
	cbc.SetExtra(agentschema.ResumeStartIterationKey, resumeIteration+1)
	return nil, nil
}

// buildInteractiveInput 由用户反馈与组件 ID 构建交互输入。
// 已是 InteractiveInput → 补缺失组件 ID（fallback 取现有首个值）；
// 有组件 ID → 逐个绑定 str(userQuery)；无组件 ID → raw_inputs=str(userQuery)。
// Python: react_agent.py _build_interactive_input L2754-1766。
func (a *ReActAgent) buildInteractiveInput(userQuery any, componentIDs []string) *interaction.InteractiveInput {
	if ii, ok := userQuery.(*interaction.InteractiveInput); ok {
		fallback := ""
		for _, v := range ii.UserInputs {
			fallback = fmt.Sprint(v)
			break
		}
		for _, compID := range componentIDs {
			if _, exists := ii.UserInputs[compID]; !exists {
				ii.UserInputs[compID] = fallback
			}
		}
		return ii
	}
	if len(componentIDs) > 0 {
		ii, err := interaction.NewInteractiveInput()
		if err != nil {
			return nil
		}
		for _, compID := range componentIDs {
			ii.UserInputs[compID] = fmt.Sprint(userQuery)
		}
		return ii
	}
	ii, err := interaction.NewInteractiveInput(fmt.Sprint(userQuery))
	if err != nil {
		return nil
	}
	return ii
}

// deepCloneAssistantMessage 深拷贝 AI 消息（Python copy.deepcopy，D-26-2）。
// JSON 往返承载：AssistantMessage 全字段 JSON 序列化面与 Python deepcopy 语义等价。
func deepCloneAssistantMessage(msg *llmschema.AssistantMessage) *llmschema.AssistantMessage {
	if msg == nil {
		return nil
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return msg // 序列化失败回退原引用（不中断主流程）
	}
	clone := &llmschema.AssistantMessage{}
	if err := json.Unmarshal(data, clone); err != nil {
		return msg
	}
	return clone
}

// deepCloneToolCall 深拷贝工具调用（Python copy.deepcopy，D-26-2）。
func deepCloneToolCall(tc *llmschema.ToolCall) *llmschema.ToolCall {
	if tc == nil {
		return nil
	}
	data, err := json.Marshal(tc)
	if err != nil {
		return tc
	}
	clone := &llmschema.ToolCall{}
	if err := json.Unmarshal(data, clone); err != nil {
		return tc
	}
	return clone
}

// ──────────────────────────── 批式 workflow 挂起桥（D-26-8） ────────────────────────────

// popWorkflowResumeInput 从节点交互队列取恢复输入（nil = 队列空）。
// workflow-in-workflow 恢复场景：引擎重放节点前 checkpointer 把用户反馈按节点
// executable ID 路由进节点状态 InteractiveInputKey，队列消费与 questioner 的
// InteractWithResult 恢复同机制。
func (a *ReActAgent) popWorkflowResumeInput(sess sessioninterfaces.SessionFacade) any {
	nodeSess, ok := sess.(interface {
		InnerNodeSession() interfaces2.InnerSession
	})
	if !ok {
		return nil
	}
	inner := nodeSess.InnerNodeSession()
	wi := interaction.NewWorkflowInteraction(inner)
	res := wi.PopPendingInteractiveInput()
	if res != nil {
		// 消费后同步清空节点状态中的交互队列（vertex.clearInteractive 同款）：
		// 后续轮转挂起会以全新 interaction 实例重读 state 队列，不清空会被
		// SuspendNodeInteraction 的"队列仍有待消费输入"防御分支拒绝。
		_ = inner.State().Update(map[string]any{constants.InteractiveInputKey: []any{}})
	}
	return res
}

// pendingInteractionValue 从 interrupt 结果中提取 pending 交互帧的载荷值
// （问题文本）。帧形态双收：workflow 收集的 chunk 值/指针都可能出现。
func pendingInteractionValue(result map[string]any) (any, bool) {
	workflowState, _ := result["workflow_execution_state"].(*workflowbase.WorkflowOutput)
	if workflowState == nil {
		return nil, false
	}
	componentIDs, _ := result["component_ids"].([]string)
	if len(componentIDs) == 0 {
		return nil, false
	}
	pendingID := componentIDs[0]
	list, ok := workflowState.Result.([]stream.Schema)
	if !ok {
		return nil, false
	}
	for _, sch := range list {
		if sch == nil || sch.SchemaType() != interaction.InteractionType {
			continue
		}
		var payload any
		switch t := sch.(type) {
		case *stream.OutputSchema:
			payload = t.Payload
		case stream.OutputSchema:
			payload = t.Payload
		default:
			continue
		}
		switch p := payload.(type) {
		case interaction.InteractionOutput:
			if p.ID == pendingID {
				return p.Value, true
			}
		case *interaction.InteractionOutput:
			if p.ID == pendingID {
				return p.Value, true
			}
		case map[string]any:
			// JSON 往返形态（checkpoint 恢复）
			if fmt.Sprint(p["id"]) == pendingID {
				return p["value"], true
			}
		}
	}
	return nil, false
}

// suspendWorkflowInteraction 挂起外层 workflow 图执行（节点会话面桥接）。
// panic GraphInterrupt 向上冒泡穿过 Invoke → invokeEntry → graph 引擎捕获
// （checkpoint + workflow 层 INPUT_REQUIRED），不经任何 recover 吞没
// （ability 层 isInterruptPanic 只覆盖工具执行期 panic，此处不在其域内）。
func (a *ReActAgent) suspendWorkflowInteraction(ctx context.Context, sess sessioninterfaces.SessionFacade, value any) {
	nodeSess, ok := sess.(interface {
		InnerNodeSession() interfaces2.InnerSession
		GetExecutableID() string
	})
	if !ok {
		logger.Warn(logComponent).
			Str("event_type", "workflow_interrupt_suspend_skipped").
			Msg("session does not expose node inner session, workflow interrupt suspend skipped")
		return
	}
	wi := interaction.NewWorkflowInteraction(nodeSess.InnerNodeSession())
	if err := wi.SuspendNodeInteraction(ctx, value); err != nil {
		logger.Error(logComponent).
			Str("event_type", "workflow_interrupt_suspend_error").
			Err(err).
			Str("executable_id", nodeSess.GetExecutableID()).
			Msg("failed to suspend workflow interaction for react interrupt")
	}
}
