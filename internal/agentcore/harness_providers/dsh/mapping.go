package dsh

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"

	deepseekharness "github.com/theoneLee/deepseek-harness-sdk-go"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// providerName ProviderEvent.provider（Python: mapping.py:37 _PROVIDER）
	providerName = "deepseek-harness"
	// schemaVersion ProviderEvent.schema_version（Python: mapping.py:38 _SCHEMA_VERSION）
	schemaVersion = "1"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mappedDshEvent 一个规约后的 payload 加信封级 provider 关联。
// Python: mapping.py:54-61 MappedDshEvent（item_id/provider_session_id 为可空）
type mappedDshEvent struct {
	payload           harnessprotocol.HarnessEventPayload
	itemID            string // 空串表示 nil
	providerSessionID string // 空串表示 nil
}

// dshTurnAccumulatorState 规约一个外部驱动的 DSH 活动区间所需的状态。
// Python: mapping.py:63-76 DshTurnAccumulator
type DshTurnAccumulator struct {
	// turnID 所属宿主轮次标识（Python: turn_id）
	turnID string
	// rootSessionID 根会话标识（Python: root_session_id）
	rootSessionID string
	// messages 归一化消息序列（Python: messages）
	messages []harnessprotocol.TurnMessage
	// messageIDs 已见消息去重（Python: _message_ids）
	messageIDs map[string]bool
	// finalizedOutputIDs 已发 FINAL 的输出块（Python: _finalized_output_ids）
	finalizedOutputIDs map[string]bool
	// usageByStep 按 (native_turn, native_step) 去重的用量（Python: _usage_by_step）
	usageByStep map[[2]int]harnessprotocol.TurnUsage
	// toolNames call_id → 工具名（Python: _tool_names）
	toolNames map[string]string
	// lastTurnReason 最近一次 turn/end 的 reason（Python: last_turn_reason）
	lastTurnReason map[string]any
	// nativeTurnEndCount 已见 DSH 原生 turn/end 计数（Python: native_turn_end_count）
	nativeTurnEndCount int
	// lastTextOutput 最新 assistant 文本投影（Python: last_text_output）
	lastTextOutput string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDshTurnAccumulator 构造规约器。Python: mapping.py:66-76 __init__
func NewDshTurnAccumulator(turnID string, rootSessionID string) *DshTurnAccumulator {
	return &DshTurnAccumulator{
		turnID:             turnID,
		rootSessionID:      rootSessionID,
		messageIDs:         map[string]bool{},
		finalizedOutputIDs: map[string]bool{},
		usageByStep:        map[[2]int]harnessprotocol.TurnUsage{},
		toolNames:          map[string]string{},
	}
}

// Consume 规约一条 SDK 通知且不保留其可变容器。
// Python: mapping.py:78-100 consume
func (a *DshTurnAccumulator) Consume(notification deepseekharness.Notification) ([]mappedDshEvent, error) {
	method := asString(notification.Method)
	// 注意：Notification.Payload 是具名类型 deepseekharness.JSONObject，
	// 对接口做 .(map[string]any) 断言会失败，必须显式转换（嵌套值由 SDK
	// 反序列化为普通 map[string]any，asMap 可用）
	payload := map[string]any(notification.Payload)
	// Python: mapping.py:83-84 subagent 生命周期
	if method == "subagent.started" || method == "subagent.finished" {
		return []mappedDshEvent{a.mapSubagent(method, payload)}, nil
	}
	// Python: mapping.py:85-86 非 session.event → ProviderEvent
	if method != "session.event" {
		name := method
		if name == "" {
			name = "unknown-notification"
		}
		return a.providerEvent(name, map[string]any{}, "")
	}

	sessionID := asString(payload["sessionId"])
	if sessionID == "" {
		sessionID = a.rootSessionID
	}
	rawEvent := asMap(payload["event"])
	// Python: mapping.py:90-99 子会话：turn/end 先脱敏后作 ProviderEvent
	if sessionID != a.rootSessionID {
		if asString(rawEvent["type"]) == "turn/end" {
			redacted, err := redactTurnEndEvent(rawEvent)
			if err != nil {
				return nil, err
			}
			rawEvent = redacted
		}
		return a.providerEvent("child.session.event",
			map[string]any{"event": rawEvent}, sessionID)
	}
	return a.mapRootSessionEvent(rawEvent)
}

// BuildTerminalResult 在 whole-agent idle 时构建唯一的外部终态结果。
// Python: mapping.py:102-187 build_terminal_result
func (a *DshTurnAccumulator) BuildTerminalResult(
	runResult *deepseekharness.RunResult,
	startedAt float64,
	startedMonotonic time.Time,
) (harnessprotocol.TurnEventKind, harnessprotocol.TurnResult, error) {
	completedAt := float64(time.Now().UnixNano()) / 1e9
	durationMs := int(math.Max(0, float64(time.Since(startedMonotonic).Milliseconds())))

	// Python: mapping.py:113-115 finish_reason 非串 str 化
	finishReason := ""
	if runResult != nil && runResult.FinishReason != nil {
		finishReason = *runResult.FinishReason
	}
	// Python: mapping.py:116-118 final_response 非串回落 last_text_output
	finalResponse := a.lastTextOutput
	if runResult != nil && runResult.FinalResponse != "" {
		finalResponse = runResult.FinalResponse
	}

	providerData := map[string]any{
		"native_turn_end_count": a.nativeTurnEndCount,
	}
	if finishReason != "" {
		providerData["native_finish_reason"] = finishReason
	}
	frozenProviderData, err := harnessprotocol.FreezeJsonObject(providerData)
	if err != nil {
		return "", harnessprotocol.TurnResult{}, err
	}
	usage := a.TotalUsage()

	// Python: mapping.py:135-144 缺 turn/end → 协议失败
	if finishReason == "" {
		return a.failedTerminal("DeepSeek Harness became idle without ending a native turn",
			strPtr("DSH_MISSING_TURN_END"), strPtr("protocol"), frozenProviderData, frozenProviderData,
			finalResponse, usage, startedAt, completedAt, durationMs)
	}
	// Python: mapping.py:145-146 completed/max-tokens → COMPLETED
	if finishReason == "completed" || finishReason == "max-tokens" {
		result, err := harnessprotocol.NewTurnResult(harnessprotocol.TurnStatusCompleted, harnessprotocol.ResultOptions{
			Messages: a.messages, FinalOutput: finalResponse, StopReason: strPtr(finishReason),
			Usage: usage, StartedAt: &startedAt, CompletedAt: &completedAt,
			DurationMS: &durationMs, ProviderData: frozenProviderData,
		})
		return harnessprotocol.TurnEventKindFinished, result, err
	}
	// Python: mapping.py:147-155 blocked → POLICY
	if finishReason == "blocked" {
		termination, err := harnessprotocol.NewTurnTermination(harnessprotocol.TurnTerminationKindPolicy,
			strPtr("DeepSeek Harness blocked the turn"), nil, nil)
		if err != nil {
			return "", harnessprotocol.TurnResult{}, err
		}
		result, err := harnessprotocol.NewTurnResult(harnessprotocol.TurnStatusInterrupted, harnessprotocol.ResultOptions{
			Messages: a.messages, FinalOutput: finalResponse, StopReason: strPtr(finishReason),
			Termination: &termination, Usage: usage, StartedAt: &startedAt,
			CompletedAt: &completedAt, DurationMS: &durationMs, ProviderData: frozenProviderData,
		})
		return harnessprotocol.TurnEventKindAborted, result, err
	}
	// Python: mapping.py:156-166 aborted/interrupted → 按 reason.kind 映射
	if finishReason == "aborted" || finishReason == "interrupted" {
		kind := a.terminationKind(finishReason)
		safeData, err := safeReasonData(a.lastTurnReason)
		if err != nil {
			return "", harnessprotocol.TurnResult{}, err
		}
		termination, err := harnessprotocol.NewTurnTermination(kind,
			strPtr("DeepSeek Harness interrupted the turn"), nil, safeData)
		if err != nil {
			return "", harnessprotocol.TurnResult{}, err
		}
		result, err := harnessprotocol.NewTurnResult(harnessprotocol.TurnStatusInterrupted, harnessprotocol.ResultOptions{
			Messages: a.messages, FinalOutput: finalResponse, StopReason: strPtr(finishReason),
			Termination: &termination, Usage: usage, StartedAt: &startedAt,
			CompletedAt: &completedAt, DurationMS: &durationMs, ProviderData: frozenProviderData,
		})
		return harnessprotocol.TurnEventKindAborted, result, err
	}
	// Python: mapping.py:167-177 error → provider 错误码
	if finishReason == "error" {
		code := reasonErrorCode(a.lastTurnReason)
		safeData, err := safeReasonData(a.lastTurnReason)
		if err != nil {
			return "", harnessprotocol.TurnResult{}, err
		}
		return a.failedTerminal("DeepSeek Harness reported a failed turn",
			code, strPtr("provider"), safeData, frozenProviderData,
			finalResponse, usage, startedAt, completedAt, durationMs)
	}
	// Python: mapping.py:178-187 未知结束 → 协议失败
	extra, err := harnessprotocol.FreezeJsonObject(map[string]any{"finish_reason": finishReason})
	if err != nil {
		return "", harnessprotocol.TurnResult{}, err
	}
	return a.failedTerminal("DeepSeek Harness returned an unknown turn ending",
		strPtr("DSH_UNKNOWN_FINISH_REASON"), strPtr("protocol"), extra, frozenProviderData,
		finalResponse, usage, startedAt, completedAt, durationMs)
}

// BuildFailedResult 规约一次 SDK 失败且不复制可能含敏感信息的 stderr。
// Python: mapping.py:189-213 build_failed_result
func (a *DshTurnAccumulator) BuildFailedResult(
	exc error,
	startedAt float64,
	startedMonotonic time.Time,
) (harnessprotocol.TurnResult, error) {
	completedAt := float64(time.Now().UnixNano()) / 1e9
	// Python: mapping.py:205 code=type(exc).__name__；Go 取错误类型名
	code := fmt.Sprintf("%T", exc)
	durationMs := int(math.Max(0, float64(time.Since(startedMonotonic).Milliseconds())))
	providerData, err := harnessprotocol.FreezeJsonObject(map[string]any{
		"exception_type": fmt.Sprintf("%T", exc),
	})
	if err != nil {
		return harnessprotocol.TurnResult{}, err
	}
	turnErr, err := harnessprotocol.NewTurnError("DeepSeek Harness SDK turn failed",
		strPtr(code), strPtr("provider_sdk"), nil, nil)
	if err != nil {
		return harnessprotocol.TurnResult{}, err
	}
	// Python: mapping.py:212 provider_data={"exception_type": ...} 位于结果级
	// common kwargs，不在 error 上
	return harnessprotocol.NewTurnResult(harnessprotocol.TurnStatusFailed, harnessprotocol.ResultOptions{
		Messages: a.messages, FinalOutput: a.lastTextOutput, Error: &turnErr,
		Usage: a.TotalUsage(), StartedAt: &startedAt, CompletedAt: &completedAt,
		DurationMS: &durationMs, ProviderData: providerData,
	})
}

// BuildStoppedResult 在周期关闭停止活动时构建 interrupted 结果。
// Python: mapping.py:215-236 build_stopped_result
func (a *DshTurnAccumulator) BuildStoppedResult(
	startedAt float64,
	startedMonotonic time.Time,
) (harnessprotocol.TurnResult, error) {
	completedAt := float64(time.Now().UnixNano()) / 1e9
	durationMs := int(math.Max(0, float64(time.Since(startedMonotonic).Milliseconds())))
	termination, err := harnessprotocol.NewTurnTermination(harnessprotocol.TurnTerminationKindHarnessStop,
		strPtr("DeepSeek Harness stopped before the turn completed"), nil, nil)
	if err != nil {
		return harnessprotocol.TurnResult{}, err
	}
	return harnessprotocol.NewTurnResult(harnessprotocol.TurnStatusInterrupted, harnessprotocol.ResultOptions{
		Messages: a.messages, FinalOutput: a.lastTextOutput,
		Termination: &termination, Usage: a.TotalUsage(),
		StartedAt: &startedAt, CompletedAt: &completedAt, DurationMS: &durationMs,
	})
}

// TotalUsage 返回跨 DSH 模型调用步骤的累计用量。
// Python: mapping.py:238-262 total_usage
func (a *DshTurnAccumulator) TotalUsage() *harnessprotocol.TurnUsage {
	if len(a.usageByStep) == 0 {
		return nil
	}
	inputTokens, outputTokens, cachedInputTokens := 0, 0, 0
	reasoningOutputTokens, cacheWriteTokens := 0, 0
	for _, usage := range a.usageByStep {
		inputTokens += derefOrZero(usage.InputTokens)
		outputTokens += derefOrZero(usage.OutputTokens)
		cachedInputTokens += derefOrZero(usage.CachedInputTokens)
		reasoningOutputTokens += derefOrZero(usage.ReasoningOutputTokens)
		cacheWriteTokens += asNonNegativeInt(usage.ProviderData["cache_write_tokens"])
	}
	total := inputTokens + outputTokens + cachedInputTokens + cacheWriteTokens
	usage, err := harnessprotocol.NewTurnUsage(
		intPtr(inputTokens), intPtr(outputTokens), intPtr(cachedInputTokens),
		intPtr(reasoningOutputTokens), intPtr(total),
		map[string]any{"cache_write_tokens": cacheWriteTokens},
	)
	if err != nil {
		return nil
	}
	return &usage
}

// BuildQueuedStopResult 返回已接受但在执行前被停止的轮次终态。
// Python: mapping.py:520-533 build_queued_stop_result
func BuildQueuedStopResult() (harnessprotocol.TurnResult, error) {
	now := float64(time.Now().UnixNano()) / 1e9
	zero := 0
	termination, err := harnessprotocol.NewTurnTermination(harnessprotocol.TurnTerminationKindHarnessStop,
		strPtr("DeepSeek Harness stopped before the queued turn started"), nil, nil)
	if err != nil {
		return harnessprotocol.TurnResult{}, err
	}
	return harnessprotocol.NewTurnResult(harnessprotocol.TurnStatusInterrupted, harnessprotocol.ResultOptions{
		Termination: &termination, StartedAt: &now, CompletedAt: &now, DurationMS: &zero,
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// mapRootSessionEvent 分发根会话事件。
// Python: mapping.py:264-282 _map_root_session_event
func (a *DshTurnAccumulator) mapRootSessionEvent(event map[string]any) ([]mappedDshEvent, error) {
	eventType := asString(event["type"])
	if eventType == "" {
		eventType = "unknown"
	}
	data := asMap(event["data"])
	switch eventType {
	case "assistant/chunk":
		return a.mapAssistantChunk(data, event)
	case "assistant/message":
		return a.mapAssistantMessage(data, event)
	case "tool/call":
		mapped, err := a.mapToolCall(data)
		if err != nil {
			return nil, err
		}
		return []mappedDshEvent{mapped}, nil
	case "tool/result":
		mapped, err := a.mapToolResult(data)
		if err != nil {
			return nil, err
		}
		return []mappedDshEvent{mapped}, nil
	case "step/start", "step/end":
		mapped, err := mapStep(eventType, data)
		if err != nil {
			return nil, err
		}
		return []mappedDshEvent{mapped}, nil
	case "turn/end":
		// Python: mapping.py:277-281 记录 reason、计数、脱敏后作 ProviderEvent
		a.lastTurnReason = asMap(data["reason"])
		a.nativeTurnEndCount++
		redacted, err := redactTurnEndEvent(event)
		if err != nil {
			return nil, err
		}
		return a.providerEvent(eventType, map[string]any{"event": redacted}, "")
	default:
		return a.providerEvent(eventType, map[string]any{"event": event}, "")
	}
}

// mapAssistantChunk 规约流式 assistant 块。
// Python: mapping.py:284-339 _map_assistant_chunk
func (a *DshTurnAccumulator) mapAssistantChunk(
	data map[string]any, rawEvent map[string]any,
) ([]mappedDshEvent, error) {
	chunk := asMap(data["chunk"])
	chunkType := asString(chunk["type"])
	if chunkType == "" {
		chunkType = "unknown"
	}
	nativeTurn := asInt(data["turn"])
	nativeStep := asInt(data["step"])
	contentIndex := asInt(chunk["index"])

	// Python: mapping.py:295-312 text/reasoning 增量
	if chunkType == "text-delta" || chunkType == "reasoning-delta" {
		text := asString(chunk["text"])
		if text == "" {
			return nil, nil
		}
		channel := harnessprotocol.OutputChannelAnswer
		if chunkType != "text-delta" {
			channel = harnessprotocol.OutputChannelReasoning
		}
		outputID := outputID(nativeTurn, nativeStep, contentIndex, channel)
		event, err := harnessprotocol.NewOutputEvent(outputID, harnessprotocol.OutputKindText,
			text, harnessprotocol.OutputOperationDelta, channel, contentIndex, "", nil)
		if err != nil {
			return nil, err
		}
		return []mappedDshEvent{{payload: event}}, nil
	}

	// Python: mapping.py:314-335 block-end → FINAL
	if chunkType == "block-end" {
		block := asMap(chunk["block"])
		blockType := asString(block["type"])
		if blockType == "text" || blockType == "reasoning" {
			channel := harnessprotocol.OutputChannelAnswer
			if blockType != "text" {
				channel = harnessprotocol.OutputChannelReasoning
			}
			outputID := outputID(nativeTurn, nativeStep, contentIndex, channel)
			a.finalizedOutputIDs[outputID] = true
			text := asString(block["text"])
			if channel == harnessprotocol.OutputChannelAnswer {
				a.lastTextOutput = text
			}
			event, err := harnessprotocol.NewOutputEvent(outputID, harnessprotocol.OutputKindText,
				text, harnessprotocol.OutputOperationFinal, channel, contentIndex, "", nil)
			if err != nil {
				return nil, err
			}
			return []mappedDshEvent{{payload: event}}, nil
		}
	}

	// Python: mapping.py:337-338 usage 块
	if chunkType == "usage" {
		return a.updateUsage(nativeTurn, nativeStep, asMap(chunk["usage"]))
	}
	// Python: mapping.py:339 其余 → ProviderEvent
	return a.providerEvent("assistant/chunk", map[string]any{"event": rawEvent}, "")
}

// mapAssistantMessage 规约完整 assistant 消息。
// Python: mapping.py:341-404 _map_assistant_message
func (a *DshTurnAccumulator) mapAssistantMessage(
	data map[string]any, rawEvent map[string]any,
) ([]mappedDshEvent, error) {
	nativeTurn := asInt(data["turn"])
	nativeStep := asInt(data["step"])
	message := asMap(data["message"])
	if message == nil {
		message = data
	}
	rawContent, _ := message["content"].([]any)
	seq := 0
	switch value := rawEvent["seq"].(type) {
	case int:
		seq = value
	case float64:
		seq = int(value)
	}
	messageID := asString(message["id"])
	if messageID == "" {
		messageID = fmt.Sprintf("dsh-message:%d:%d:%d", nativeTurn, nativeStep, seq)
	}
	var blocks []harnessprotocol.ContentBlock
	var mapped []mappedDshEvent
	var answerParts []string

	// Python: mapping.py:356-381 内容块遍历
	for index, rawBlock := range rawContent {
		block := asMap(rawBlock)
		blockType := asString(block["type"])
		if blockType == "" {
			blockType = "unknown"
		}
		blockID := asString(block["id"])
		if blockID == "" {
			blockID = fmt.Sprintf("%s:block:%d", messageID, index)
		}
		contentBlock, err := contentBlock(blockID, blockType, block)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, contentBlock)
		if blockType != "text" && blockType != "reasoning" {
			continue
		}
		channel := harnessprotocol.OutputChannelAnswer
		if blockType != "text" {
			channel = harnessprotocol.OutputChannelReasoning
		}
		outputID := outputID(nativeTurn, nativeStep, index, channel)
		text := asString(block["text"])
		if channel == harnessprotocol.OutputChannelAnswer {
			answerParts = append(answerParts, text)
		}
		if !a.finalizedOutputIDs[outputID] {
			a.finalizedOutputIDs[outputID] = true
			event, err := harnessprotocol.NewOutputEvent(outputID, harnessprotocol.OutputKindText,
				text, harnessprotocol.OutputOperationFinal, channel, index, "", nil)
			if err != nil {
				return nil, err
			}
			mapped = append(mapped, mappedDshEvent{payload: event})
		}
	}

	// Python: mapping.py:383-385 final_response 投影 = 最新 assistant message 的 text 块拼接
	a.lastTextOutput = concatStrings(answerParts)

	// Python: mapping.py:387-400 消息去重后入列
	if !a.messageIDs[messageID] {
		a.messageIDs[messageID] = true
		source := asMap(message["source"])
		messageData := map[string]any{"native_turn": nativeTurn, "native_step": nativeStep}
		if source != nil {
			frozenSource, err := harnessprotocol.FreezeJsonObject(source)
			if err != nil {
				return nil, err
			}
			messageData["source"] = frozenSource
		}
		turnMessage, err := harnessprotocol.NewTurnMessage(messageID,
			harnessprotocol.MessageRoleAssistant, blocks, messageData)
		if err != nil {
			return nil, err
		}
		a.messages = append(a.messages, turnMessage)
	}
	// Python: mapping.py:401-403 usage 追加
	if usage := asMap(data["usage"]); usage != nil {
		extra, err := a.updateUsage(nativeTurn, nativeStep, usage)
		if err != nil {
			return nil, err
		}
		mapped = append(mapped, extra...)
	}
	return mapped, nil
}

// mapToolCall 规约工具调用开始。
// Python: mapping.py:406-424 _map_tool_call
func (a *DshTurnAccumulator) mapToolCall(data map[string]any) (mappedDshEvent, error) {
	callID := asString(data["callId"])
	if callID == "" {
		callID = "unknown-tool-call"
	}
	toolName := asString(data["name"])
	if toolName == "" {
		toolName = "unknown"
	}
	a.toolNames[callID] = toolName
	frozen, err := harnessprotocol.FreezeJsonObject(map[string]any{
		"name":          toolName,
		"arguments":     harnessprotocol.JsonValueToBuiltin(anyValue(data["arguments"])),
		"provider_turn": asInt(data["turn"]),
		"provider_step": asInt(data["step"]),
	})
	if err != nil {
		return mappedDshEvent{}, err
	}
	event, err := harnessprotocol.NewItemLifecycleEvent(harnessprotocol.ItemEventKindStarted, "tool", frozen)
	if err != nil {
		return mappedDshEvent{}, err
	}
	return mappedDshEvent{payload: event, itemID: callID}, nil
}

// mapToolResult 规约工具结果完成。
// Python: mapping.py:426-448 _map_tool_result
func (a *DshTurnAccumulator) mapToolResult(data map[string]any) (mappedDshEvent, error) {
	message := asMap(data["message"])
	source := asMap(message["source"])
	callID := asString(source["callId"])
	if callID == "" {
		callID = asString(data["callId"])
	}
	if callID == "" {
		callID = "unknown-tool-call"
	}
	var content any
	if message != nil {
		content = message["content"]
	} else {
		content = data["content"]
	}
	itemData := map[string]any{
		"tool_name":     a.toolNames[callID],
		"result":        harnessprotocol.JsonValueToBuiltin(anyValue(content)),
		"provider_turn": asInt(data["turn"]),
		"provider_step": asInt(data["step"]),
	}
	if _, ok := data["error"]; ok && data["error"] != nil {
		itemData["error"] = harnessprotocol.JsonValueToBuiltin(anyValue(data["error"]))
	}
	if _, ok := data["meta"]; ok && data["meta"] != nil {
		itemData["meta"] = harnessprotocol.JsonValueToBuiltin(anyValue(data["meta"]))
	}
	frozen, err := harnessprotocol.FreezeJsonObject(itemData)
	if err != nil {
		return mappedDshEvent{}, err
	}
	event, err := harnessprotocol.NewItemLifecycleEvent(harnessprotocol.ItemEventKindCompleted, "tool", frozen)
	if err != nil {
		return mappedDshEvent{}, err
	}
	return mappedDshEvent{payload: event, itemID: callID}, nil
}

// mapStep 规约步骤生命周期。
// Python: mapping.py:450-462 _map_step（staticmethod）
func mapStep(eventType string, data map[string]any) (mappedDshEvent, error) {
	nativeTurn := asInt(data["turn"])
	nativeStep := asInt(data["step"])
	kind := harnessprotocol.ItemEventKindCompleted
	if eventType == "step/start" {
		kind = harnessprotocol.ItemEventKindStarted
	}
	frozen, err := harnessprotocol.FreezeJsonObject(map[string]any{
		"provider_turn": nativeTurn, "provider_step": nativeStep,
	})
	if err != nil {
		return mappedDshEvent{}, err
	}
	event, err := harnessprotocol.NewItemLifecycleEvent(kind, "step", frozen)
	if err != nil {
		return mappedDshEvent{}, err
	}
	return mappedDshEvent{
		payload: event,
		itemID:  fmt.Sprintf("dsh-step:%d:%d", nativeTurn, nativeStep),
	}, nil
}

// mapSubagent 规约子代理生命周期。
// Python: mapping.py:464-472 _map_subagent（staticmethod）
func (a *DshTurnAccumulator) mapSubagent(method string, payload map[string]any) mappedDshEvent {
	childID := asString(payload["childSessionId"])
	if childID == "" {
		childID = asString(payload["sessionId"])
	}
	if childID == "" {
		childID = "unknown-subagent"
	}
	kind := harnessprotocol.ItemEventKindCompleted
	if method == "subagent.started" {
		kind = harnessprotocol.ItemEventKindStarted
	}
	// Python: freeze_json_object(payload) 失败仅在上游非法输入时发生；
	// 通知形状由 SDK 保证，此处冻结失败降级为空 data（不可达防御）
	frozen, _ := harnessprotocol.FreezeJsonObject(payload)
	event, _ := harnessprotocol.NewItemLifecycleEvent(kind, "subagent", frozen)
	return mappedDshEvent{payload: event, itemID: childID, providerSessionID: childID}
}

// updateUsage 更新按步去重的用量并在变化时发出累计事件。
// Python: mapping.py:474-486 _update_usage
func (a *DshTurnAccumulator) updateUsage(
	nativeTurn, nativeStep int, rawUsage map[string]any,
) ([]mappedDshEvent, error) {
	usage, err := turnUsage(rawUsage)
	if err != nil {
		return nil, err
	}
	key := [2]int{nativeTurn, nativeStep}
	if existing, ok := a.usageByStep[key]; ok && usageEqual(existing, usage) {
		return nil, nil
	}
	a.usageByStep[key] = usage
	total := a.TotalUsage()
	if total == nil {
		return nil, nil
	}
	event, err := harnessprotocol.NewUsageUpdatedEvent(*total, harnessprotocol.UsageUpdateModeCumulative)
	if err != nil {
		return nil, err
	}
	return []mappedDshEvent{{payload: event}}, nil
}

// providerEvent 构造 ProviderEvent。
// Python: mapping.py:488-503 _provider_event（staticmethod）
func (a *DshTurnAccumulator) providerEvent(
	eventType string, payload map[string]any, providerSessionID string,
) ([]mappedDshEvent, error) {
	frozen, err := harnessprotocol.FreezeJsonObject(payload)
	if err != nil {
		return nil, err
	}
	event, err := harnessprotocol.NewProviderEvent(providerName, eventType, schemaVersion, frozen)
	if err != nil {
		return nil, err
	}
	return []mappedDshEvent{{payload: event, providerSessionID: providerSessionID}}, nil
}

// terminationKind 由 finish_reason 与最后 reason 映射终止类别。
// Python: mapping.py:505-517 _termination_kind
func (a *DshTurnAccumulator) terminationKind(finishReason string) harnessprotocol.TurnTerminationKind {
	if finishReason == "interrupted" {
		return harnessprotocol.TurnTerminationKindProvider
	}
	reason := asMap(a.lastTurnReason["reason"])
	cause := reason
	causeKind := asString(cause["kind"])
	switch causeKind {
	case "user":
		return harnessprotocol.TurnTerminationKindUserAbort
	case "disposed":
		return harnessprotocol.TurnTerminationKindHarnessStop
	case "hook":
		return harnessprotocol.TurnTerminationKindPolicy
	default:
		return harnessprotocol.TurnTerminationKindProvider
	}
}

// failedTerminal 组装 FAILED 终态（build_terminal_result 各失败分支公用形状）。
// Python: mapping.py:136-144 / 167-177 / 178-187 的公共 kwargs：
// common.provider_data 携带 native 计数，error.provider_data 是脱敏 reason。
func (a *DshTurnAccumulator) failedTerminal(
	message string, code, category *string, errorData, commonData harnessprotocol.JsonObject,
	finalResponse string, usage *harnessprotocol.TurnUsage,
	startedAt, completedAt float64, durationMs int,
) (harnessprotocol.TurnEventKind, harnessprotocol.TurnResult, error) {
	turnErr, err := harnessprotocol.NewTurnError(message, code, category, nil, nil)
	if err != nil {
		return "", harnessprotocol.TurnResult{}, err
	}
	turnErr.ProviderData = errorData
	result, err := harnessprotocol.NewTurnResult(harnessprotocol.TurnStatusFailed, harnessprotocol.ResultOptions{
		Messages: a.messages, FinalOutput: finalResponse, Error: &turnErr,
		Usage: usage, StartedAt: &startedAt, CompletedAt: &completedAt,
		DurationMS: &durationMs, ProviderData: commonData,
	})
	return harnessprotocol.TurnEventKindFailed, result, err
}

// safeReasonData 保留有用的非 message 失败字段且不复制 provider 诊断文本。
// Python: mapping.py:552-569 _safe_reason_data
func safeReasonData(reason map[string]any) (harnessprotocol.JsonObject, error) {
	if reason == nil {
		return nil, nil
	}
	result := map[string]any{}
	if kind := asString(reason["kind"]); kind != "" {
		result["kind"] = kind
	}
	if cause := asMap(reason["reason"]); cause != nil {
		filtered := map[string]any{}
		for key, value := range cause {
			if key != "message" {
				filtered[key] = value
			}
		}
		frozen, err := harnessprotocol.FreezeJsonObject(filtered)
		if err != nil {
			return nil, err
		}
		result["cause"] = frozen
	}
	if errData := asMap(reason["error"]); errData != nil {
		filtered := map[string]any{}
		for _, key := range []string{"code", "status", "requestId", "retryable"} {
			if value, ok := errData[key]; ok {
				filtered[key] = value
			}
		}
		frozen, err := harnessprotocol.FreezeJsonObject(filtered)
		if err != nil {
			return nil, err
		}
		result["error"] = frozen
	}
	return harnessprotocol.FreezeJsonObject(result)
}

// redactTurnEndEvent 保留 DSH 事件形状但不暴露 provider 错误文本。
// Python: mapping.py:572-592 _redact_turn_end_event
func redactTurnEndEvent(event map[string]any) (map[string]any, error) {
	safeEvent := copyMap(event)
	data := copyMap(asMap(event["data"]))
	reason := asMap(data["reason"])
	safeReason := map[string]any{}
	if kind := asString(reason["kind"]); kind != "" {
		safeReason["kind"] = kind
	}
	if cause := asMap(reason["reason"]); cause != nil {
		filtered := map[string]any{}
		for key, value := range cause {
			if key != "message" {
				filtered[key] = value
			}
		}
		safeReason["reason"] = filtered
	}
	if errData := asMap(reason["error"]); errData != nil {
		filtered := map[string]any{}
		for _, key := range []string{"code", "status", "requestId", "retryable"} {
			if value, ok := errData[key]; ok {
				filtered[key] = value
			}
		}
		safeReason["error"] = filtered
	}
	data["reason"] = safeReason
	safeEvent["data"] = data
	return safeEvent, nil
}

// reasonErrorCode 从 reason.error 提取错误码。
// Python: mapping.py:595-598 _reason_error_code
func reasonErrorCode(reason map[string]any) *string {
	if reason == nil {
		return nil
	}
	errData := asMap(reason["error"])
	code := errData["code"]
	switch value := code.(type) {
	case string:
		return strPtr(value)
	case int:
		return strPtr(strconv.Itoa(value))
	case float64:
		return strPtr(strconv.Itoa(int(value)))
	default:
		return nil
	}
}

// turnUsage 规约 DSH 原生 usage 字段。
// Python: mapping.py:601-614 _turn_usage
func turnUsage(raw map[string]any) (harnessprotocol.TurnUsage, error) {
	inputTokens := asNonNegativeInt(raw["inputTokens"])
	outputTokens := asNonNegativeInt(raw["outputTokens"])
	cacheReadTokens := asNonNegativeInt(raw["cacheReadTokens"])
	cacheWriteTokens := asNonNegativeInt(raw["cacheWriteTokens"])
	reasoningTokens := asNonNegativeInt(raw["reasoningTokens"])
	total := inputTokens + outputTokens + cacheReadTokens + cacheWriteTokens
	return harnessprotocol.NewTurnUsage(
		nonNegPtr(inputTokens), nonNegPtr(outputTokens), nonNegPtr(cacheReadTokens),
		nonNegPtr(reasoningTokens), nonNegPtr(total),
		map[string]any{"cache_write_tokens": cacheWriteTokens},
	)
}

// contentBlock 规约一个内容块。
// Python: mapping.py:617-630 _content_block
func contentBlock(blockID, blockType string, block map[string]any) (harnessprotocol.ContentBlock, error) {
	if blockType == "text" || blockType == "reasoning" {
		return harnessprotocol.NewContentBlock(blockID, blockType,
			asString(block["text"]), nil)
	}
	if blockType == "tool-call" {
		arguments, err := harnessprotocol.FreezeJsonValue(anyValue(block["arguments"]))
		if err != nil {
			return harnessprotocol.ContentBlock{}, err
		}
		callID := asString(block["id"])
		if callID == "" {
			callID = blockID
		}
		return harnessprotocol.NewContentBlock(blockID, "tool_call",
			map[string]any{
				"name":      orDefault(asString(block["name"]), "unknown"),
				"arguments": arguments,
			},
			map[string]any{"call_id": callID})
	}
	frozen, err := harnessprotocol.FreezeJsonObject(block)
	if err != nil {
		return harnessprotocol.ContentBlock{}, err
	}
	return harnessprotocol.NewContentBlock(blockID, blockType, frozen, nil)
}

// outputID 稳定输出块标识。
// Python: mapping.py:633-634 _output_id
func outputID(nativeTurn, nativeStep, contentIndex int, channel harnessprotocol.OutputChannel) string {
	return fmt.Sprintf("dsh-output:%d:%d:%d:%s", nativeTurn, nativeStep, contentIndex, channel)
}

// asMap any → map（非 map 返回 nil）。Python: mapping.py:637-638 _mapping
func asMap(value any) map[string]any {
	mapping, _ := value.(map[string]any)
	return mapping
}

// asString any → string（非串返回空）。Python: mapping.py:641-642 _string
func asString(value any) string {
	str, _ := value.(string)
	return str
}

// asInt any → int（bool 排除）。Python: mapping.py:645-646 _int
func asInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		if v == math.Trunc(v) {
			return int(v)
		}
		return 0
	default:
		return 0
	}
}

// asNonNegativeInt 非负整数，否则 0。Python: mapping.py:649-651 _non_negative_int
func asNonNegativeInt(value any) int {
	n := asInt(value)
	if n < 0 {
		return 0
	}
	return n
}

// anyValue 把 any 视作 JSON 值原样返回（冻结由 FreezeJson* 承担）。
// Python freeze_json_value 的输入收集点
func anyValue(value any) any {
	if value == nil {
		return nil
	}
	return value
}

// usageEqual 比较两份 TurnUsage 的五个计数与 provider_data。
// Python: mapping.py:482 `self._usage_by_step.get(key) == usage`（dataclass 等值）
func usageEqual(left, right harnessprotocol.TurnUsage) bool {
	return derefOrZero(left.InputTokens) == derefOrZero(right.InputTokens) &&
		derefOrZero(left.OutputTokens) == derefOrZero(right.OutputTokens) &&
		derefOrZero(left.CachedInputTokens) == derefOrZero(right.CachedInputTokens) &&
		derefOrZero(left.ReasoningOutputTokens) == derefOrZero(right.ReasoningOutputTokens) &&
		derefOrZero(left.TotalTokens) == derefOrZero(right.TotalTokens) &&
		jsonEqual(left.ProviderData, right.ProviderData)
}

// jsonEqual 深比较两份 JSON 对象（键序无关）。
func jsonEqual(left, right map[string]any) bool {
	l, errL := json.Marshal(left)
	r, errR := json.Marshal(right)
	return errL == nil && errR == nil && string(l) == string(r)
}

// derefOrZero 可空 int 取值。
func derefOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

// intPtr 非零转指针；0 保留为 0（usage 计数 0 合法）。
func intPtr(value int) *int { return &value }

// nonNegPtr 非负 int 指针。
func nonNegPtr(value int) *int { return &value }

// strPtr 字符串指针。
func strPtr(value string) *string { return &value }

// orDefault 空串回落默认值。
func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// copyMap 浅拷贝 map（nil 返回 nil）。
func copyMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	out := make(map[string]any, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

// concatStrings 拼接字符串切片。
func concatStrings(parts []string) string {
	out := ""
	for _, part := range parts {
		out += part
	}
	return out
}
