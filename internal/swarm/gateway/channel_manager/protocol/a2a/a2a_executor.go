package a2a

import (
	"context"
	"iter"
	"strings"

	sdka2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// a2aExecutor A2A 执行器：把内部消息事件流翻译成 A2A 事件序列。
//
// 协议侧（任务建档、状态机、SSE）由 a2asrv 负责，本类型只做桥接。
// Python: a2a_connect.py _A2AAgentExecutor (L43-176)
type a2aExecutor struct {
	// ch 所属通道
	ch *A2AChannel
}

// ──────────────────────────── 导出函数 ────────────────────────────

// executorFor 构造通道对应的 A2A 执行器。
func executorFor(ch *A2AChannel) a2asrv.AgentExecutor { return &a2aExecutor{ch: ch} }

// Execute 执行一次任务：产出 A2A 事件序列。
//
// 序列：Task(submitted) → StatusUpdate(WORKING) → Artifact... → StatusUpdate(终态)。
// 首事件必须是 Task（偏差 D1，a2asrv 强制）。
// Python: _A2AAgentExecutor.execute (L49-162)
func (e *a2aExecutor) Execute(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[sdka2a.Event, error] {
	return func(yield func(sdka2a.Event, error) bool) {
		requestID := string(ec.TaskID)
		defer e.ch.ClearPendingRequest(requestID)

		if !yield(sdka2a.NewSubmittedTask(ec, ec.Message), nil) {
			return
		}

		query, files := MapA2APartsToParams(ec.Message)
		if query == "" {
			query = joinTextParts(ec.Message)
		}
		if query == "" {
			yield(sdka2a.NewStatusUpdateEvent(ec, sdka2a.TaskStateFailed, nil), nil)
			return
		}

		if !yield(sdka2a.NewStatusUpdateEvent(ec, sdka2a.TaskStateWorking, nil), nil) {
			return
		}

		pending, err := e.ch.DispatchA2ARequest(ctx, requestID, ec.ContextID, query, files, ec.Metadata)
		if err != nil {
			logger.Error(logComponent).Err(err).Str("request_id", requestID).Msg("A2A dispatch failed")
			yield(sdka2a.NewStatusUpdateEvent(ec, sdka2a.TaskStateFailed, nil), nil)
			return
		}

		artifactStarted := false
		for {
			var msg *schema.Message
			select {
			case <-ctx.Done():
				return
			case msg = <-pending.C():
			}
			if msg == nil {
				return
			}

			parts := MessageToA2AParts(msg, false)
			terminal := IsTerminalMessage(msg)
			if terminal && len(parts) == 0 && msg.EventType == schema.EventTypeChatError {
				// 错误响应保持可读（对齐 Python L104-112）
				parts = MessageToA2AParts(msg, true)
			}
			parts = filterSentinelParts(parts)

			if len(parts) > 0 {
				artifact := &sdka2a.Artifact{
					ID:       sdka2a.ArtifactID(requestID + "_response"),
					Name:     "response",
					Parts:    sdka2a.ContentParts(parts),
					Metadata: msg.Metadata,
				}
				if !yield(&sdka2a.TaskArtifactUpdateEvent{
					ContextID: ec.ContextID,
					TaskID:    ec.TaskID,
					Artifact:  artifact,
					Append:    artifactStarted,
					LastChunk: terminal,
				}, nil) {
					return
				}
				artifactStarted = true
			}

			if terminal {
				yield(sdka2a.NewStatusUpdateEvent(ec, terminalState(msg.EventType), nil), nil)
				return
			}
		}
	}
}

// Cancel 响应客户端取消请求。
//
// Python: _A2AAgentExecutor.cancel (L164-176)
func (e *a2aExecutor) Cancel(_ context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[sdka2a.Event, error] {
	return func(yield func(sdka2a.Event, error) bool) {
		defer e.ch.ClearPendingRequest(string(ec.TaskID))
		// 任务尚未建档时先补 Task（a2asrv 要求首事件为 Task/Message，偏差 D1）
		if ec.StoredTask == nil && ec.Message != nil {
			if !yield(sdka2a.NewSubmittedTask(ec, ec.Message), nil) {
				return
			}
		}
		yield(sdka2a.NewStatusUpdateEvent(ec, sdka2a.TaskStateCanceled, nil), nil)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// terminalState 把内部终态事件映射为 A2A 任务状态。
//
// Python: _A2AAgentExecutor.execute (L136-149)
func terminalState(eventType schema.EventType) sdka2a.TaskState {
	switch eventType {
	case schema.EventTypeChatError:
		return sdka2a.TaskStateFailed
	case schema.EventTypeChatInterruptResult:
		return sdka2a.TaskStateCanceled
	default:
		return sdka2a.TaskStateCompleted
	}
}

// filterSentinelParts 过滤完成哨兵文本 Part。
//
// Python: _A2AAgentExecutor.execute (L113-119)
func filterSentinelParts(parts []*sdka2a.Part) []*sdka2a.Part {
	if len(parts) == 0 {
		return nil
	}
	filtered := make([]*sdka2a.Part, 0, len(parts))
	for _, p := range parts {
		if p == nil {
			continue
		}
		if IsCompletionSentinelText(strings.TrimSpace(p.Text())) {
			continue
		}
		filtered = append(filtered, p)
	}
	return filtered
}
