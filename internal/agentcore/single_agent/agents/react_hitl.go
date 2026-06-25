package agents

import (
	"context"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interrupt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// AfterExecuteToolCallForHITL 执行工具后检测 HITL 中断。
//
// 对应 Python: ReActAgent._after_execute_tool_call_for_hitl()
func (a *ReActAgent) AfterExecuteToolCallForHITL(
	results []any,
	toolCalls []*llmschema.ToolCall,
	aiMessage *llmschema.AssistantMessage,
	iteration int,
	originalQuery string,
) (*interrupt.ToolInterruptionState, []any) {
	if a.hitlHandler == nil {
		return nil, nil
	}
	intState, payloads := a.hitlHandler.BuildInterruptState(
		results, toolCalls, aiMessage, iteration, originalQuery,
	)
	if intState == nil {
		return nil, nil
	}
	return intState, payloads
}

// CommitInterrupt 提交中断状态。
//
// 对应 Python: ReActAgent._commit_interrupt() 的 HITL 分支
func (a *ReActAgent) CommitInterrupt(
	ctx context.Context,
	intState *interrupt.ToolInterruptionState,
	modelCtx ceinterface.ModelContext,
	sess sessioninterfaces.SessionFacade,
	invokeInputs *rail.InvokeInputs,
	subAgentOutputs []any,
) (map[string]any, error) {
	if a.hitlHandler == nil {
		return nil, nil
	}
	return a.hitlHandler.CommitInterrupt(ctx, intState, modelCtx, sess, invokeInputs, subAgentOutputs)
}

// ClearContextMessages 清除当前上下文消息（保留历史）。
//
// 对应 Python: ReActAgent.clear_context_messages(with_history=False)
func (a *ReActAgent) ClearContextMessages(sess sessioninterfaces.SessionFacade) {
	if a.contextEngine == nil {
		return
	}
	ctx := context.Background()
	sessionID := sess.GetSessionID()
	mc := a.contextEngine.GetContext(
		"default_context", sessionID,
	)
	if mc != nil {
		_ = mc.ClearMessages(ctx, false)
	}
}
