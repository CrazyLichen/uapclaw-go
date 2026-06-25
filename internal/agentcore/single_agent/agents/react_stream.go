package agents

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// WriteInvokeResultToStream 将 invoke 结果写入会话流。
//
// 对应 Python: ReActAgent._write_invoke_result_to_stream()
func (a *ReActAgent) WriteInvokeResultToStream(
	ctx context.Context,
	result map[string]any,
	sess sessioninterfaces.SessionFacade,
) {
	resultType, _ := result["result_type"].(string)
	if resultType == "interrupt" {
		if _, hasInterruptIDs := result["interrupt_ids"]; hasInterruptIDs {
			// HITL 中断写入流
			if a.hitlHandler != nil {
				_ = a.hitlHandler.WriteInterruptToStream(ctx, result, sess)
			}
		}
		// ⤵️ 6.12: Workflow 中断写入流（暂未实现）
		// else 分支：workflowState := result["workflow_execution_state"]
		// componentIDs := result["component_ids"]
		// pendingID := componentIDs[0]
		// 遍历 workflowState.result，写入 payload.id == pendingID 的 schema
	} else {
		// 正常 answer 结果
		output, _ := result["output"].(string)
		_ = sess.WriteStream(ctx, &stream.OutputSchema{
			Type:  "answer",
			Index: 0,
			Payload: map[string]any{
				"output":      output,
				"result_type": resultType,
			},
		})
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// innerStream 内部流式执行。
//
// 对应 Python: ReActAgent._inner_stream()
func (a *ReActAgent) innerStream(
	ctx context.Context,
	sess sessioninterfaces.SessionFacade,
	agentSess *session.Session,
	isAgentSess bool,
	needCleanup bool,
	inputs map[string]any,
	opts []interfaces.AgentOption,
	outCh chan<- stream.Schema,
) {
	// streamProcess: 在后台执行 invoke，结果写入 session stream
	streamProcess := func() {
		defer func() {
			// finally: 清理（对齐 Python L1555-1560）
			// save_contexts 由 needCleanup 控制，close_stream/commit 由 is_agent_session 控制
			if needCleanup {
				a.saveContexts(sess)
			}
			if isAgentSess && agentSess != nil {
				_ = agentSess.CloseStream()
				_ = agentSess.Commit(ctx)
			}
		}()

		// 捕获 panic 防止 goroutine 崩溃
		defer func() {
			if r := recover(); r != nil {
				logger.Error(logComponent).Any("panic", r).Msg("streamProcess panic")
				a.WriteInvokeResultToStream(ctx, map[string]any{
					"output":      fmt.Sprintf("panic: %v", r),
					"result_type": "error",
				}, sess)
			}
		}()

		// 走完整虚分发（对齐 Python: self.invoke(inputs, session, _streaming=True)）
		result, err := a.base.Invoke(ctx, inputs, opts...)
		if err != nil {
			// 错误结果写入流
			logger.Error(logComponent).Err(err).Str("event_type", "LLM_CALL_ERROR").Msg("streamProcess invoke 错误")
			a.WriteInvokeResultToStream(ctx, map[string]any{
				"output":      err.Error(),
				"result_type": "error",
			}, sess)
			return
		}

		// 正常结果写入流
		if resultMap, ok := result.(map[string]any); ok {
			a.WriteInvokeResultToStream(ctx, resultMap, sess)
		} else if resultList, ok := result.([]stream.Schema); ok {
			// invoke 返回 schema 列表（中断路径）
			for _, schema := range resultList {
				_ = sess.WriteStream(ctx, schema)
			}
		}
	}

	if isAgentSess && agentSess != nil {
		// Agent session: 启动 streamProcess goroutine，从 StreamIterator 消费
		go streamProcess()

		for chunk := range agentSess.StreamIterator() {
			outCh <- chunk
		}
	} else {
		// Workflow session: 直接执行 streamProcess
		// 输出通过 session.WriteStream → StreamWriterManager 传递给 Workflow
		streamProcess()
	}
}
