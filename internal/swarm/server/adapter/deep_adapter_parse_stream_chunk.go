// Python 源：interface_deep.py 流式循环局部累加状态（usage / emitted_ask_user_ids /
// has_streamed_content / accumulated_text / accumulated_reasoning）+ :21155-21172
// _parse_stream_chunk 的 llm_output/llm_reasoning 分支 + :20760-20767
// _stream_text_payload。15.18 从 deep_adapter.go 流式 goroutine 内联提取，
// 循环与 external harness 投影共用（计划 Task 6，Q4 方案 A）。
package adapter

import (
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/utils"
)

// ──────────────────────────── 结构体 ────────────────────────────

// streamParseState 流式投影的跨块累加状态。
// Python: interface.py/interface_deep.py 流式循环局部变量。
type streamParseState struct {
	// usage usage 累加器（Python: usage accumulator）
	usage *utils.UsageAccumulator
	// emittedAskUserIDs askUser 去重集合（Python: emitted_ask_user_ids）
	emittedAskUserIDs map[string]bool
	// hasStreamedContent 是否已流式发出内容（Python: has_streamed_content）
	hasStreamedContent bool
	// accumulatedText 累积文本（Python: accumulated_text）
	accumulatedText string
	// accumulatedReasoning 累积推理（Python: accumulated_reasoning）
	accumulatedReasoning string
}

// newStreamParseState 构造零值状态。
func newStreamParseState() *streamParseState {
	return &streamParseState{
		usage:             &utils.UsageAccumulator{},
		emittedAskUserIDs: make(map[string]bool),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// streamTextPayload 构造文本事件；空内容丢弃（不丢格式化片段之外的空帧）。
// Python: interface_deep.py:20760-20767 _stream_text_payload。
func streamTextPayload(eventType, content string) map[string]any {
	if content == "" {
		return nil
	}
	return map[string]any{"event_type": eventType, "content": content}
}

// parseAccumulatingChunk 处理三类跨块累加块（llm_usage/llm_reasoning/llm_output）。
// Python: _parse_stream_chunk 的对应分支（:21161-21172）+ 循环内联 usage 累加段。
// 返回 (payloads, handled)：handled=false 时调用方继续走 utils.ParseStreamChunk；
// payloads 内各帧按序 yield（Python 单 return，Go 侧 usage 累加后直接产出帧等价）。
// sessionID 用于 usage 帧 stamping（Go 循环既有行为，Python 循环同义）。
func parseAccumulatingChunk(output *stream.OutputSchema, st *streamParseState, sessionID string) ([]map[string]any, bool) {
	payload := payloadToMap(output.Payload)
	switch output.Type {
	case "llm_usage":
		// Python: 循环内联 — 累加 usage → yield chat.usage_metadata
		utils.AccumulateUsage(st.usage, payload)
		usagePayload := map[string]any{
			"event_type":    "chat.usage_metadata",
			"input_tokens":  st.usage.InputTokens,
			"output_tokens": st.usage.OutputTokens,
			"total_tokens":  st.usage.TotalTokens,
			"input_cost":    st.usage.InputCost,
			"output_cost":   st.usage.OutputCost,
			"total_cost":    st.usage.TotalCost,
		}
		if metadata, ok := payload["metadata"]; ok {
			usagePayload["metadata"] = metadata
		}
		if sessionID != "" {
			usagePayload["session_id"] = sessionID
		}
		return []map[string]any{usagePayload}, true
	case "llm_reasoning":
		// Python: :20260-20285 — 空内容跳过（_stream_text_payload :20765），直转发不累加。
		// 修正（2026-11-22 对齐复核）：Python 循环从不累加（accumulated_reasoning 在
		// :18926 函数内 write-never，flush 块为上游死代码）；Go 原实现的累加属移植期
		// 多余逻辑，会造成 answer/default flush 重复发 reasoning，随本对齐删除。
		reasoningContent := extractReasoningContent(payload)
		if frame := streamTextPayload("chat.reasoning", reasoningContent); frame != nil {
			return []map[string]any{frame}, true
		}
		return nil, true
	case "llm_output":
		// Python: :20290-20327 — flush 累积 reasoning（emit 后清空；恒空无操作）→
		// 发 chat.delta；置位 has_streamed_content；不累加 accumulated_text。
		textContent := extractTextContent(payload)
		st.hasStreamedContent = true
		var frames []map[string]any
		if reasoning, ok := st.flushAccumulatedReasoning(); ok {
			frames = append(frames, map[string]any{
				"event_type": "chat.reasoning",
				"content":    reasoning,
			})
		}
		if frame := streamTextPayload("chat.delta", textContent); frame != nil {
			frames = append(frames, frame)
		}
		if frames == nil {
			return nil, true
		}
		return frames, true
	default:
		return nil, false
	}
}

// flushAccumulatedText 累积文本 flush 并清空（Python: 先 emit 再置空）。
func (st *streamParseState) flushAccumulatedText() (string, bool) {
	if st.accumulatedText == "" {
		return "", false
	}
	text := st.accumulatedText
	st.accumulatedText = ""
	return text, true
}

// flushAccumulatedReasoning 累积推理 flush 并清空（Python: 先 emit 再置空）。
func (st *streamParseState) flushAccumulatedReasoning() (string, bool) {
	if st.accumulatedReasoning == "" {
		return "", false
	}
	reasoning := st.accumulatedReasoning
	st.accumulatedReasoning = ""
	return reasoning, true
}
