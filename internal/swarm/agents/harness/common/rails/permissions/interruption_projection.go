// Package permissions 的中断快照投影。
//
// 本文件对齐 Python jiuwenswarm/agents/harness/common/rails/stream_event_rail.py 的
// 4 个 queue 消费函数（L811-952）。
//
// # 为什么在 permissions 包而不是 rail 上
//
// Python 里这些是 JiuClawStreamEventRail 的方法，直接 import root_permission_queue。
// Go 里 permissions 已经 import rails（root_ask_user.go / permission_interrupt_rail.go），
// rail 反向 import permissions 会成环，而投影必须碰 RootPermissionQueue 的具体类型。
//
// 解法：rail 声明 InterruptionProjectionDeps 回调结构体，本包提供实现，
// 装配层（code_adapter）接线。偏差已在 rails.InterruptionProjectionDeps 的
// 类型注释登记。
//
// # 三条投影路径
//
//  1. 队列就绪且命中 pending 卡片 → 渲染「队列验证过的权限卡片」
//  2. 队列缺失 / 快照无效 → 撤销中断快照 + 提示重试（fail-closed）
//  3. 非权限中断 → 普通中断投影
package permissions

import (
	"context"
	"errors"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// interruptionRetryMessageCN 中文重试提示
	interruptionRetryMessageCN = "本次审批批次无法安全恢复，请重新发起请求。"
	// interruptionRetryMessageEN 英文重试提示
	interruptionRetryMessageEN = "This approval batch could not be resumed safely. Please retry the request."
	// retractReasonInterruptSnapshotCanceled 撤销事件的 reason
	retractReasonInterruptSnapshotCanceled = "interrupt_snapshot_canceled"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ErrInterruptCleanupScopeUnavailable 无法获取清理范围
	ErrInterruptCleanupScopeUnavailable = errors.New("interrupt_cleanup_scope_unavailable")
	// ErrInterruptCleanupScopeMismatch 清理范围的会话与当前会话不符
	ErrInterruptCleanupScopeMismatch = errors.New("interrupt_cleanup_scope_mismatch")
	// ErrInterruptCleanupStoreUnavailable 队列不可用
	ErrInterruptCleanupStoreUnavailable = errors.New("interrupt_cleanup_store_unavailable")
	// ErrInterruptCleanupIncomplete 清理后仍有残留
	ErrInterruptCleanupIncomplete = errors.New("interrupt_cleanup_incomplete")
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ProjectCompleteInterruption 中断结束后投影「已完成的权限审批」。
//
// Python: JiuSwarmStreamEventRail._project_complete_interruption(ctx) (L811-860)
//
// 四条分支：
//  1. 队列未注入：有权限交互 → 撤销快照；否则普通投影
//  2. reconcile 报错：撤销本会话范围内的快照
//  3. 无快照：普通投影
//  4. 有快照：渲染队首卡片；渲染失败则撤销全部快照
func ProjectCompleteInterruption(
	ctx context.Context,
	rail *rails.JiuClawStreamEventRail,
	cbc *agentinterfaces.AgentCallbackContext,
) error {
	queue := ResolveRootPermissionQueue(rail)
	session := cbc.Session()
	sid := rail.ResolveSID(cbc, session)
	result := invokeResultOf(cbc)

	if queue == nil {
		if ContainsPermissionInteraction(result) {
			return CancelInterruptionSnapshot(ctx, rail, cbc, nil, "permission_queue_unavailable")
		}
		return ProjectOrdinaryInterruption(ctx, rail, cbc)
	}

	resultType, state, interruptIDs := interruptPartsOf(result)
	snapshot, err := queue.Reconcile(resultType, state, interruptIDs, sid)
	if err != nil {
		reason := err.Error()
		var queueErr *RootPermissionQueueError
		if errors.As(err, &queueErr) {
			reason = queueErr.Code
		}
		return CancelInterruptionSnapshot(ctx, rail, cbc, queue.SnapshotScope(sid, nil), reason)
	}
	if snapshot == nil {
		return ProjectOrdinaryInterruption(ctx, rail, cbc)
	}

	if len(snapshot.Interactions) == 0 || len(snapshot.Cards) == 0 {
		return CancelInterruptionSnapshot(
			ctx, rail, cbc, cardKeys(snapshot.Cards), "permission_queue_projection_invalid")
	}
	card := snapshot.Cards[0]
	payload := interrupt.BuildVerifiedPermissionAskUserQuestion(
		snapshot.Interactions[0],
		card.Key.ToolCallID,
		card.Key.InvocationID,
	)
	if payload == nil {
		return CancelInterruptionSnapshot(
			ctx, rail, cbc, cardKeys(snapshot.Cards), "permission_queue_projection_invalid")
	}
	return writeAskUserQuestion(ctx, session, payload)
}

// ProjectOrdinaryInterruption 投影普通中断（非权限队列路径）。
//
// Python: JiuSwarmStreamEventRail._project_ordinary_interruption(ctx) (L862-884)
//
// 只处理 result_type == "interrupt" 且 state 里确实有问题交互的情形。
// 转换不出 payload 或 request_id 不在中断列表里时，退化为重试提示。
func ProjectOrdinaryInterruption(
	ctx context.Context,
	rail *rails.JiuClawStreamEventRail,
	cbc *agentinterfaces.AgentCallbackContext,
) error {
	result := invokeResultOf(cbc)
	if result["result_type"] != "interrupt" {
		return nil
	}

	// Python: state = result.get("state") or []，元素经 _iter_interactions +
	// _extract_interaction_parts 解包（OutputSchema(type="__interaction__") →
	// InteractionOutput{id, value}）。Go 侧用本包 interactionOf 等价解包——
	// BuildInterruptResult 的 state 元素是类型化 *stream.OutputSchema，
	// 纯 map 断言会得到空列表 → payload=nil → 误发重试提示
	// （2026-11-14 问答闭环实锤）。
	stateRaw, _ := result["state"].([]any)
	interactions := make([]map[string]any, 0, len(stateRaw))
	for _, item := range stateRaw {
		if unwrapped := interactionOf(item); unwrapped != nil {
			interactions = append(interactions, unwrapped)
		}
	}
	if len(interactions) > 0 && len(interrupt.QuestionInteractions(interactions)) == 0 {
		return nil
	}

	payload := interrupt.ConvertInteractionsToAskUserQuestion(interactions)
	if payload == nil || !interruptIDContains(result["interrupt_ids"], payload["request_id"]) {
		return writeInterruptionRetryError(ctx, rail, cbc.Session())
	}
	return writeAskUserQuestion(ctx, cbc.Session(), payload)
}

// CancelInterruptionSnapshot 撤销中断快照，并清理权限记录。
//
// Python: JiuSwarmStreamEventRail._cancel_interruption_snapshot(ctx, keys=, reason=)
// (L886-917)
//
// fail-closed：任何一步失败都隔离该会话——损坏的运行时不能继续接受新工作。
func CancelInterruptionSnapshot(
	ctx context.Context,
	rail *rails.JiuClawStreamEventRail,
	cbc *agentinterfaces.AgentCallbackContext,
	keys []*ToolInvocationKeyV1,
	reason string,
) error {
	session := cbc.Session()
	sid := rail.ResolveSID(cbc, session)

	err := func() error {
		session.UpdateState(map[string]any{saschema.InterruptionKey: nil})
		if writeErr := writeStream(ctx, session, stream.OutputSchema{
			Type: "chat.retract",
			Payload: map[string]any{
				"reason": retractReasonInterruptSnapshotCanceled,
			},
		}); writeErr != nil {
			return writeErr
		}

		cleanupKeys, scopeErr := trustedCleanupScope(rail, cbc)
		if scopeErr != nil {
			return scopeErr
		}
		merged := dedupeKeys(append(append([]*ToolInvocationKeyV1{}, keys...), cleanupKeys...))
		if discardErr := discardPermissionRecords(rail, merged); discardErr != nil {
			return discardErr
		}
		return writeInterruptionRetryError(ctx, rail, session)
	}()

	if err != nil {
		// Python: except BaseException: self.quarantine_session(sid, session); raise
		rail.QuarantineSession(sid, session)
		return err
	}
	return nil
}

// InterruptionRetryMessage 返回中断恢复失败时给用户的提示文案。
//
// Python: JiuSwarmStreamEventRail._interruption_retry_message() (L954-957)
func InterruptionRetryMessage(rail *rails.JiuClawStreamEventRail) string {
	if rail.PromptLanguage() == "en" {
		return interruptionRetryMessageEN
	}
	return interruptionRetryMessageCN
}

// ResolveRootPermissionQueue 从 rail 取出根权限队列（未注入返回 nil）。
//
// rail 侧只持有窄接口 RootPermissionQueueAccessor，这里断言回具体类型。
// 断言失败说明装配错误，按「队列不可用」处理（fail-closed）。
func ResolveRootPermissionQueue(rail *rails.JiuClawStreamEventRail) *RootPermissionQueue {
	accessor := rail.RootPermissionQueue()
	if accessor == nil {
		return nil
	}
	queue, ok := accessor.(*RootPermissionQueue)
	if !ok {
		return nil
	}
	return queue
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// trustedCleanupScope 计算可信清理范围（本会话 + 本请求的队列卡片）。
//
// Python: JiuSwarmStreamEventRail._trusted_cleanup_scope(ctx) (L919-939)
//
// 三个前置必须成立：能从 run_context.extra 解出决策上下文、上下文会话与当前
// 会话一致、队列可用。任一不成立即报错，由调用方隔离会话。
func trustedCleanupScope(
	rail *rails.JiuClawStreamEventRail,
	cbc *agentinterfaces.AgentCallbackContext,
) ([]*ToolInvocationKeyV1, error) {
	carrier, err := RootDecisionContextFromExtra(runContextExtraOf(cbc))
	if err != nil || carrier == nil {
		return nil, ErrInterruptCleanupScopeUnavailable
	}

	sid := rail.ResolveSID(cbc, cbc.Session())
	if carrier.SessionID != sid {
		return nil, ErrInterruptCleanupScopeMismatch
	}

	queue := ResolveRootPermissionQueue(rail)
	if queue == nil {
		return nil, ErrInterruptCleanupStoreUnavailable
	}
	requestID := carrier.RequestID
	return queue.SnapshotScope(carrier.SessionID, &requestID), nil
}

// discardPermissionRecords 取消快照并确认无残留。
//
// Python: JiuSwarmStreamEventRail._discard_permission_records(keys, reason=) (L941-952)
func discardPermissionRecords(
	rail *rails.JiuClawStreamEventRail,
	keys []*ToolInvocationKeyV1,
) error {
	queue := ResolveRootPermissionQueue(rail)
	if queue == nil {
		return ErrInterruptCleanupStoreUnavailable
	}
	queue.CancelSnapshot(keys)
	for _, key := range keys {
		if _, exists := queue.Get(key); exists {
			return ErrInterruptCleanupIncomplete
		}
	}
	return nil
}

// writeAskUserQuestion 发送一条 chat.ask_user_question 事件。
func writeAskUserQuestion(
	ctx context.Context,
	session sessioninterfaces.SessionFacade,
	payload map[string]any,
) error {
	return writeStream(ctx, session, stream.OutputSchema{
		Type:    "chat.ask_user_question",
		Payload: payload,
	})
}

// writeInterruptionRetryError 发送中断恢复失败提示。
func writeInterruptionRetryError(
	ctx context.Context,
	rail *rails.JiuClawStreamEventRail,
	session sessioninterfaces.SessionFacade,
) error {
	return writeStream(ctx, session, stream.OutputSchema{
		Type:    "error",
		Payload: map[string]any{"error": InterruptionRetryMessage(rail)},
	})
}

// writeStream 包装 session.WriteStream（nil session 视为无操作）。
func writeStream(
	ctx context.Context,
	session sessioninterfaces.SessionFacade,
	output stream.OutputSchema,
) error {
	if session == nil {
		return nil
	}
	return session.WriteStream(ctx, output)
}

// cardKeys 提取快照里全部卡片的主键。
func cardKeys(cards []RootPermissionCard) []*ToolInvocationKeyV1 {
	keys := make([]*ToolInvocationKeyV1, 0, len(cards))
	for _, card := range cards {
		keys = append(keys, card.Key)
	}
	return keys
}

// dedupeKeys 按首次出现顺序去重。
//
// Python: dict.fromkeys((*keys, *scope))
func dedupeKeys(keys []*ToolInvocationKeyV1) []*ToolInvocationKeyV1 {
	seen := make(map[string]struct{}, len(keys))
	result := make([]*ToolInvocationKeyV1, 0, len(keys))
	for _, key := range keys {
		if key == nil {
			continue
		}
		if _, dup := seen[key.InvocationID]; dup {
			continue
		}
		seen[key.InvocationID] = struct{}{}
		result = append(result, key)
	}
	return result
}

// invokeResultOf 取 invoke 结果（无结果为 nil map）。
func invokeResultOf(cbc *agentinterfaces.AgentCallbackContext) map[string]any {
	inputs, ok := cbc.Inputs().(*agentinterfaces.InvokeInputs)
	if !ok || inputs.Result == nil {
		return map[string]any{}
	}
	return inputs.Result
}

// interruptPartsOf 从结果中拆出 (result_type, state, interrupt_ids)。
func interruptPartsOf(result map[string]any) (string, []any, []any) {
	resultType, _ := result["result_type"].(string)
	state, _ := result["state"].([]any)
	interruptIDs, _ := result["interrupt_ids"].([]any)
	return resultType, state, interruptIDs
}

// interruptIDContains 判断 request_id 是否在中断列表里。
func interruptIDContains(raw any, requestID any) bool {
	ids, ok := raw.([]any)
	if !ok {
		return false
	}
	target, _ := requestID.(string)
	for _, id := range ids {
		if text, ok := id.(string); ok && text == target {
			return true
		}
	}
	return false
}

// runContextExtraOf 取 ctx.inputs.run_context.extra。
func runContextExtraOf(cbc *agentinterfaces.AgentCallbackContext) map[string]any {
	inputs, ok := cbc.Inputs().(*agentinterfaces.InvokeInputs)
	if !ok || inputs.RunContext == nil {
		return nil
	}
	return inputs.RunContext.Extra
}
