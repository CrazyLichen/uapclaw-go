package dsh

import (
	"testing"
	"time"

	deepseekharness "github.com/theoneLee/deepseek-harness-sdk-go"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
)

// notification 构造测试通知。
func notification(method string, payload deepseekharness.JSONObject) deepseekharness.Notification {
	return deepseekharness.Notification{Method: method, Payload: payload}
}

func newAccumulator(t *testing.T) *DshTurnAccumulator {
	t.Helper()
	return NewDshTurnAccumulator("turn-1", "root-session")
}

// 子会话事件分流 + turn/end 脱敏：mapping.py:88-100 + 572-592。
func TestAccumulator_Consume子会话脱敏(t *testing.T) {
	acc := newAccumulator(t)
	mapped, err := acc.Consume(notification("session.event", deepseekharness.JSONObject{
		"sessionId": "child-session",
		"event": map[string]any{
			"type": "turn/end",
			"data": map[string]any{
				"reason": map[string]any{
					"kind":   "error",
					"reason": map[string]any{"kind": "provider", "message": "secret stderr"},
					"error":  map[string]any{"code": "E1", "status": 500, "requestId": "r1", "retryable": true, "stack": "leak"},
				},
			},
		},
	}))
	require.NoError(t, err)
	require.Len(t, mapped, 1)
	providerEvent, ok := mapped[0].Payload.(harnessprotocol.ProviderEvent)
	require.True(t, ok)
	assert.Equal(t, "child.session.event", providerEvent.EventType)
	assert.Equal(t, "child-session", mapped[0].ProviderSessionID)
	// 脱敏：message/stack 不出现，白名单键保留
	payloadText := stringify(providerEvent.Payload)
	assert.NotContains(t, payloadText, "secret stderr")
	assert.NotContains(t, payloadText, "leak")
	assert.Contains(t, payloadText, "E1")
	assert.Contains(t, payloadText, "requestId")
}

// 非 session.event 方法 → ProviderEvent（mapping.py:86）。
func TestAccumulator_Consume非session事件(t *testing.T) {
	acc := newAccumulator(t)
	mapped, err := acc.Consume(notification("other.method", deepseekharness.JSONObject{"x": 1}))
	require.NoError(t, err)
	require.Len(t, mapped, 1)
	providerEvent := mapped[0].Payload.(harnessprotocol.ProviderEvent)
	assert.Equal(t, "other.method", providerEvent.EventType)
	assert.Equal(t, providerName, providerEvent.Provider)

	mapped, err = acc.Consume(notification("", nil))
	require.NoError(t, err)
	assert.Equal(t, "unknown-notification", mapped[0].Payload.(harnessprotocol.ProviderEvent).EventType)
}

// text-delta/reasoning-delta → DELTA OutputEvent：mapping.py:295-312。
func TestAccumulator_assistantChunk文本与推理(t *testing.T) {
	acc := newAccumulator(t)
	mapped, err := acc.Consume(notification("session.event", deepseekharness.JSONObject{
		"sessionId": "root-session",
		"event": map[string]any{"type": "assistant/chunk", "data": map[string]any{
			"turn": 1, "step": 2,
			"chunk": map[string]any{"type": "text-delta", "index": 3, "text": "hello"},
		}},
	}))
	require.NoError(t, err)
	require.Len(t, mapped, 1)
	output := mapped[0].Payload.(harnessprotocol.OutputEvent)
	assert.Equal(t, "dsh-output:1:2:3:answer", output.OutputID)
	assert.Equal(t, harnessprotocol.OutputOperationDelta, output.Operation)
	assert.Equal(t, harnessprotocol.OutputChannelAnswer, output.Channel)

	mapped, err = acc.Consume(notification("session.event", deepseekharness.JSONObject{
		"sessionId": "root-session",
		"event": map[string]any{"type": "assistant/chunk", "data": map[string]any{
			"chunk": map[string]any{"type": "reasoning-delta", "index": 0, "text": "think"},
		}},
	}))
	require.NoError(t, err)
	output = mapped[0].Payload.(harnessprotocol.OutputEvent)
	assert.Equal(t, harnessprotocol.OutputChannelReasoning, output.Channel)

	// text 缺失 → 空（mapping.py:297-299）
	mapped, err = acc.Consume(notification("session.event", deepseekharness.JSONObject{
		"sessionId": "root-session",
		"event": map[string]any{"type": "assistant/chunk", "data": map[string]any{
			"chunk": map[string]any{"type": "text-delta", "index": 0},
		}},
	}))
	require.NoError(t, err)
	assert.Empty(t, mapped)
}

// block-end → FINAL + last_text_output：mapping.py:314-335。
func TestAccumulator_assistantChunk块结束(t *testing.T) {
	acc := newAccumulator(t)
	mapped, err := acc.Consume(notification("session.event", deepseekharness.JSONObject{
		"sessionId": "root-session",
		"event": map[string]any{"type": "assistant/chunk", "data": map[string]any{
			"turn": 1, "step": 0,
			"chunk": map[string]any{"type": "block-end", "index": 0,
				"block": map[string]any{"type": "text", "text": "final answer"}},
		}},
	}))
	require.NoError(t, err)
	require.Len(t, mapped, 1)
	output := mapped[0].Payload.(harnessprotocol.OutputEvent)
	assert.Equal(t, harnessprotocol.OutputOperationFinal, output.Operation)
	assert.Equal(t, "final answer", acc.lastTextOutput)
}

// 未知 chunk 类型 → ProviderEvent（mapping.py:339）。
func TestAccumulator_assistantChunk未知类型(t *testing.T) {
	acc := newAccumulator(t)
	mapped, err := acc.Consume(notification("session.event", deepseekharness.JSONObject{
		"sessionId": "root-session",
		"event": map[string]any{"type": "assistant/chunk", "data": map[string]any{
			"chunk": map[string]any{"type": "mystery"},
		}},
	}))
	require.NoError(t, err)
	assert.Equal(t, "assistant/chunk", mapped[0].Payload.(harnessprotocol.ProviderEvent).EventType)
}

// assistant/message：FINAL 去重 + TurnMessage + last_text_output + usage：
// mapping.py:341-404。
func TestAccumulator_assistantMessage(t *testing.T) {
	acc := newAccumulator(t)
	base := deepseekharness.JSONObject{
		"sessionId": "root-session",
		"event": map[string]any{"type": "assistant/message", "seq": 7, "data": map[string]any{
			"turn": 1, "step": 1,
			"message": map[string]any{
				"id": "m1",
				"content": []any{
					map[string]any{"type": "text", "text": "part1"},
					map[string]any{"type": "reasoning", "text": "why"},
				},
			},
			"usage": map[string]any{"inputTokens": 10, "outputTokens": 5},
		}},
	}
	mapped, err := acc.Consume(notification("session.event", base))
	require.NoError(t, err)
	// text + reasoning 两条 FINAL + usage 一条
	require.Len(t, mapped, 3)
	_, isOutput := mapped[0].Payload.(harnessprotocol.OutputEvent)
	require.True(t, isOutput)
	usage, ok := mapped[2].Payload.(harnessprotocol.UsageUpdatedEvent)
	require.True(t, ok)
	assert.Equal(t, harnessprotocol.UsageUpdateModeCumulative, usage.Mode)
	assert.Equal(t, 15, *usage.Usage.TotalTokens)

	assert.Equal(t, "part1", acc.lastTextOutput)
	require.Len(t, acc.messages, 1)
	assert.Equal(t, harnessprotocol.MessageRoleAssistant, acc.messages[0].Role)
	assert.Len(t, acc.messages[0].Content, 2)

	// 同一 message_id 重复 → 不重复入列，FINAL 不重发（仅 usage 无变化 → 0 条）
	mapped, err = acc.Consume(notification("session.event", base))
	require.NoError(t, err)
	assert.Empty(t, mapped)

	// 缺 id → 回落 dsh-message:turn:step:seq
	other := deepseekharness.JSONObject{
		"sessionId": "root-session",
		"event": map[string]any{"type": "assistant/message", "seq": 9, "data": map[string]any{
			"turn": 2, "step": 3,
			"message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "x"}}},
		}},
	}
	_, err = acc.Consume(notification("session.event", other))
	require.NoError(t, err)
	assert.Equal(t, "dsh-message:2:3:9", acc.messages[1].MessageID)
}

// tool/call 与 tool/result：mapping.py:406-448。
func TestAccumulator_toolCall与Result(t *testing.T) {
	acc := newAccumulator(t)
	mapped, err := acc.Consume(notification("session.event", deepseekharness.JSONObject{
		"sessionId": "root-session",
		"event": map[string]any{"type": "tool/call", "data": map[string]any{
			"callId": "c1", "name": "read_file", "turn": 1, "step": 1,
			"arguments": map[string]any{"path": "/a"},
		}},
	}))
	require.NoError(t, err)
	item := mapped[0].Payload.(harnessprotocol.ItemLifecycleEvent)
	assert.Equal(t, harnessprotocol.ItemEventKindStarted, item.Kind)
	assert.Equal(t, "c1", mapped[0].ItemID)
	assert.Contains(t, stringify(item.Data), "read_file")

	mapped, err = acc.Consume(notification("session.event", deepseekharness.JSONObject{
		"sessionId": "root-session",
		"event": map[string]any{"type": "tool/result", "data": map[string]any{
			"turn": 1, "step": 2, "error": "boom", "meta": "m",
			"message": map[string]any{
				"source":  map[string]any{"callId": "c1"},
				"content": "contents",
			},
		}},
	}))
	require.NoError(t, err)
	item = mapped[0].Payload.(harnessprotocol.ItemLifecycleEvent)
	assert.Equal(t, harnessprotocol.ItemEventKindCompleted, item.Kind)
	assert.Contains(t, stringify(item.Data), `"tool_name":"read_file"`)
	assert.Contains(t, stringify(item.Data), "boom")
}

// step/start|end 与 subagent.*：mapping.py:450-472。
func TestAccumulator_step与subagent(t *testing.T) {
	acc := newAccumulator(t)
	mapped, err := acc.Consume(notification("session.event", deepseekharness.JSONObject{
		"sessionId": "root-session",
		"event":     map[string]any{"type": "step/start", "data": map[string]any{"turn": 1, "step": 1}},
	}))
	require.NoError(t, err)
	item := mapped[0].Payload.(harnessprotocol.ItemLifecycleEvent)
	assert.Equal(t, harnessprotocol.ItemEventKindStarted, item.Kind)
	assert.Equal(t, "dsh-step:1:1", mapped[0].ItemID)

	mapped, err = acc.Consume(notification("subagent.started", deepseekharness.JSONObject{
		"childSessionId": "child-1", "extra": "data",
	}))
	require.NoError(t, err)
	item = mapped[0].Payload.(harnessprotocol.ItemLifecycleEvent)
	assert.Equal(t, "subagent", item.ItemType)
	assert.Equal(t, "child-1", mapped[0].ItemID)
	assert.Equal(t, "child-1", mapped[0].ProviderSessionID)
}

// usage 按 (turn,step) 去重累计 + cache_write_tokens：mapping.py:238-262/474-486。
func TestAccumulator_usage按步去重累计(t *testing.T) {
	acc := newAccumulator(t)
	send := func(input, output, cacheWrite int) []*MappedDshEvent {
		mapped, err := acc.Consume(notification("session.event", deepseekharness.JSONObject{
			"sessionId": "root-session",
			"event": map[string]any{"type": "assistant/chunk", "data": map[string]any{
				"turn": 1, "step": 1,
				"chunk": map[string]any{"type": "usage", "usage": map[string]any{
					"inputTokens": input, "outputTokens": output,
					"cacheReadTokens": 2, "cacheWriteTokens": cacheWrite,
					"reasoningTokens": 1,
				}},
			}},
		}))
		require.NoError(t, err)
		return mapped
	}
	require.Len(t, send(10, 5, 3), 1) // 首次 → UsageUpdatedEvent
	assert.Empty(t, send(10, 5, 3))   // 同值去重
	require.Len(t, send(12, 6, 4), 1) // 变化 → 更新

	usage := acc.totalUsage()
	require.NotNil(t, usage)
	assert.Equal(t, 12, *usage.InputTokens)
	assert.Equal(t, 6, *usage.OutputTokens)
	assert.Equal(t, 2, *usage.CachedInputTokens)
	assert.Equal(t, 1, *usage.ReasoningOutputTokens)
	assert.Equal(t, 24, *usage.TotalTokens) // 12+6+2+4
	assert.Equal(t, 4, usage.ProviderData["cache_write_tokens"])
}

// terminal 六分支：mapping.py:135-187。
func TestAccumulator_terminal六分支(t *testing.T) {
	acc := newAccumulator(t)
	startedAt := 1000.0
	startedMonotonic := time.Now()

	run := func(reason *string) (harnessprotocol.TurnEventKind, harnessprotocol.TurnResult) {
		kind, result, err := acc.BuildTerminalResult(
			&deepseekharness.RunResult{FinalResponse: "out", FinishReason: reason},
			startedAt, startedMonotonic)
		require.NoError(t, err)
		return kind, result
	}

	// completed → COMPLETED
	kind, result := run(strPtr("completed"))
	assert.Equal(t, harnessprotocol.TurnEventKindFinished, kind)
	assert.Equal(t, harnessprotocol.TurnStatusCompleted, result.Status)

	// max-tokens → COMPLETED
	kind, _ = run(strPtr("max-tokens"))
	assert.Equal(t, harnessprotocol.TurnEventKindFinished, kind)

	// blocked → INTERRUPTED + POLICY
	kind, result = run(strPtr("blocked"))
	assert.Equal(t, harnessprotocol.TurnEventKindAborted, kind)
	assert.Equal(t, harnessprotocol.TurnTerminationKindPolicy, result.Termination.Kind)

	// aborted（reason.kind=user）→ USER_ABORT
	acc.lastTurnReason = map[string]any{"reason": map[string]any{"kind": "user"}}
	kind, result = run(strPtr("aborted"))
	assert.Equal(t, harnessprotocol.TurnTerminationKindUserAbort, result.Termination.Kind)

	// interrupted → PROVIDER
	kind, result = run(strPtr("interrupted"))
	assert.Equal(t, harnessprotocol.TurnTerminationKindProvider, result.Termination.Kind)

	// error → provider 错误码
	acc.lastTurnReason = map[string]any{"error": map[string]any{"code": "E9"}}
	kind, result = run(strPtr("error"))
	assert.Equal(t, harnessprotocol.TurnEventKindFailed, kind)
	assert.Equal(t, "E9", *result.Error.Code)
	assert.Equal(t, "provider", *result.Error.Category)

	// 缺 turn/end → DSH_MISSING_TURN_END（protocol 类）
	acc.lastTurnReason = nil
	kind, result = run(nil)
	assert.Equal(t, harnessprotocol.TurnEventKindFailed, kind)
	assert.Equal(t, "DSH_MISSING_TURN_END", *result.Error.Code)
	assert.Equal(t, "protocol", *result.Error.Category)

	// 未知 finish_reason → DSH_UNKNOWN_FINISH_REASON
	kind, result = run(strPtr("weird"))
	assert.Equal(t, "DSH_UNKNOWN_FINISH_REASON", *result.Error.Code)
}

// failed/stopped 结果：mapping.py:189-236。
func TestAccumulator_failed与Stopped结果(t *testing.T) {
	acc := newAccumulator(t)
	result, err := acc.BuildFailedResult(assert.AnError, 1000.0, time.Now())
	require.NoError(t, err)
	assert.Equal(t, harnessprotocol.TurnStatusFailed, result.Status)
	assert.Equal(t, "provider_sdk", *result.Error.Category)
	assert.NotContains(t, stringify(result.ProviderData), "assert") // 不复制诊断

	result, err = acc.BuildStoppedResult(1000.0, time.Now())
	require.NoError(t, err)
	assert.Equal(t, harnessprotocol.TurnTerminationKindHarnessStop, result.Termination.Kind)
}

// termination kind 映射：mapping.py:505-517。
func TestAccumulator_terminationKind映射(t *testing.T) {
	acc := newAccumulator(t)
	assert.Equal(t, harnessprotocol.TurnTerminationKindProvider, acc.terminationKind("interrupted"))
	cases := map[string]harnessprotocol.TurnTerminationKind{
		"user":     harnessprotocol.TurnTerminationKindUserAbort,
		"disposed": harnessprotocol.TurnTerminationKindHarnessStop,
		"hook":     harnessprotocol.TurnTerminationKindPolicy,
		"other":    harnessprotocol.TurnTerminationKindProvider,
	}
	for kindValue, want := range cases {
		acc.lastTurnReason = map[string]any{"reason": map[string]any{"kind": kindValue}}
		assert.Equal(t, want, acc.terminationKind("aborted"), kindValue)
	}
}

// 脱敏助手：mapping.py:552-592。
func TestSafeReasonData与RedactTurnEnd(t *testing.T) {
	data := safeReasonData(map[string]any{
		"kind":   "error",
		"reason": map[string]any{"kind": "k", "message": "drop me"},
		"error":  map[string]any{"code": "E", "stack": "drop"},
	})
	assert.Contains(t, stringify(data), "error")
	assert.NotContains(t, stringify(data), "drop me")

	event := redactTurnEndEvent(map[string]any{
		"type": "turn/end",
		"data": map[string]any{"reason": map[string]any{
			"kind": "error", "reason": map[string]any{"kind": "k", "message": "drop"},
		}},
	})
	text := stringify(event)
	assert.NotContains(t, text, "drop")
	assert.Contains(t, text, "turn/end")

	assert.Empty(t, safeReasonData(nil))
}

// build_queued_stop_result：mapping.py:520-533。
func TestQueuedStopResult(t *testing.T) {
	result, err := BuildQueuedStopResult()
	require.NoError(t, err)
	assert.Equal(t, harnessprotocol.TurnStatusInterrupted, result.Status)
	assert.Equal(t, harnessprotocol.TurnTerminationKindHarnessStop, result.Termination.Kind)
	assert.Equal(t, 0, *result.DurationMS)
}

// stringify JSON 序列化辅助（断言用）。
func stringify(value any) string {
	encoded, err := marshalJSONValue(value)
	if err != nil {
		return ""
	}
	return encoded
}
