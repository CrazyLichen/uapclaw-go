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
	"fmt"
	"sort"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	ceinterface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// isInterrupted 判定工具结果是否为工作流中断。
// WorkflowOutput 且 State==INPUT_REQUIRED；或 list 中存在 __interaction__ 块。
// Python: react_agent.py _is_interrupted L2515-1522。
func (a *ReActAgent) isInterrupted(toolResult any) bool {
	if out, ok := toolResult.(*workflow.WorkflowOutput); ok {
		return out.State == workflow.WorkflowExecutionStateInputRequired
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
// Python: react_agent.py _extract_component_ids L2525-1552。
func (a *ReActAgent) extractComponentIDs(toolResult any) []string {
	if out, ok := toolResult.(*workflow.WorkflowOutput); ok {
		if list, ok := out.Result.([]stream.Schema); ok {
			ids := make([]string, 0)
			for _, item := range list {
				if item == nil || item.SchemaType() != interaction.InteractionType {
					continue
				}
				if os, ok := item.(*stream.OutputSchema); ok {
					if p, ok := os.Payload.(*interaction.InteractionOutput); ok {
						ids = append(ids, p.ID)
					} else if m, ok := os.Payload.(map[string]any); ok {
						ids = append(ids, fmt.Sprint(m["id"]))
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
				if m, ok := os.Payload.(map[string]any); ok {
					ids = append(ids, fmt.Sprint(m["component_id"]))
				}
			}
		}
		sort.Strings(ids)
		return ids
	}
	return []string{}
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
		interrupted[workflowID] = &agentschema.WorkflowInterruptEntry{
			ToolCall:               toolCalls[i],
			ComponentIDs:           componentIDs,
			WorkflowExecutionState: toolResult,
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
	st, ok := val.(*agentschema.InterruptionState)
	if !ok {
		return nil
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
