package dsh

import (
	"encoding/json"
	"testing"
	"time"

	deepseekharness "github.com/theoneLee/deepseek-harness-sdk-go"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// notification 构造一条 SDK 通知。Python 通知形状：{method, payload}
func notification(method string, payload map[string]any) deepseekharness.Notification {
	return deepseekharness.Notification{Method: method, Payload: payload}
}

// sessionEvent 构造一条根会话事件通知。
func sessionEvent(event map[string]any, sessionID string) deepseekharness.Notification {
	return notification("session.event", map[string]any{
		"sessionId": sessionID,
		"event":     event,
	})
}

// ──────────────────────────── consume 分流 ────────────────────────────

// Python: mapping.py:83-84 + 464-472
func TestAccumulator_Consume子代理事件(t *testing.T) {
	acc := NewDshTurnAccumulator("turn-1", "root-1")
	events, err := acc.Consume(notification("subagent.started",
		map[string]any{"childSessionId": "child-9", "k": "v"}))
	require.NoError(t, err)
	require.Len(t, events, 1)
	lifecycle := events[0].payload.(harnessprotocol.ItemLifecycleEvent)
	assert.Equal(t, harnessprotocol.ItemEventKindStarted, lifecycle.Kind)
	assert.Equal(t, "subagent", lifecycle.ItemType)
	assert.Equal(t, "child-9", events[0].itemID)
	assert.Equal(t, "child-9", events[0].providerSessionID)
}

// Python: mapping.py:85-86 非 session.event → ProviderEvent
func TestAccumulator_Consume非session事件(t *testing.T) {
	acc := NewDshTurnAccumulator("turn-1", "root-1")
	events, err := acc.Consume(notification("log.record", map[string]any{"line": "x"}))
	require.NoError(t, err)
	require.Len(t, events, 1)
	provider := events[0].payload.(harnessprotocol.ProviderEvent)
	assert.Equal(t, "log.record", provider.EventType)
	assert.Equal(t, providerName, provider.Provider)
	assert.Equal(t, schemaVersion, provider.SchemaVersion)

	// 空方法名回落 unknown-notification
	events, err = acc.Consume(deepseekharness.Notification{})
	require.NoError(t, err)
	assert.Equal(t, "unknown-notification", events[0].payload.(harnessprotocol.ProviderEvent).EventType)
}

// Python: mapping.py:88-100 子会话 turn/end 脱敏
func TestAccumulator_Consume子会话脱敏(t *testing.T) {
	acc := NewDshTurnAccumulator("turn-1", "root-1")
	events, err := acc.Consume(sessionEvent(map[string]any{
		"type": "turn/end",
		"data": map[string]any{
			"reason": map[string]any{
				"kind": "error",
				"reason": map[string]any{
					"kind": "api", "message": "SECRET provider diagnostics",
				},
				"error": map[string]any{
					"code": "E1", "status": 500, "requestId": "r1",
					"retryable": true, "diagnostic": "SECRET-STACK",
				},
			},
		},
	}, "child-7"))
	require.NoError(t, err)
	require.Len(t, events, 1)
	provider := events[0].payload.(harnessprotocol.ProviderEvent)
	assert.Equal(t, "child.session.event", provider.EventType)
	assert.Equal(t, "child-7", events[0].providerSessionID)
	// payload 形状 {"event": {...}}；断言 message 未进入
	b, _ := marshalJSON(provider.Payload)
	assert.NotContains(t, string(b), "SECRET provider diagnostics")
	assert.NotContains(t, string(b), "SECRET-STACK")
	assert.Contains(t, string(b), `"kind":"error"`)
}

// ──────────────────────────── assistant 事件 ────────────────────────────

// Python: mapping.py:295-312
func TestAccumulator_assistantChunk文本与推理(t *testing.T) {
	acc := NewDshTurnAccumulator("turn-1", "root-1")
	events, err := acc.Consume(sessionEvent(map[string]any{
		"type": "assistant/chunk",
		"data": map[string]any{
			"turn": 1, "step": 2,
			"chunk": map[string]any{"type": "text-delta", "index": 3, "text": "hello"},
		},
	}, "root-1"))
	require.NoError(t, err)
	require.Len(t, events, 1)
	out := events[0].payload.(harnessprotocol.OutputEvent)
	assert.Equal(t, harnessprotocol.OutputOperationDelta, out.Operation)
	assert.Equal(t, harnessprotocol.OutputChannelAnswer, out.Channel)
	assert.Equal(t, "hello", out.Content)
	assert.Equal(t, 3, out.ContentIndex)

	// reasoning-delta 走 REASONING 通道
	events, err = acc.Consume(sessionEvent(map[string]any{
		"type": "assistant/chunk",
		"data": map[string]any{
			"turn": 1, "step": 2,
			"chunk": map[string]any{"type": "reasoning-delta", "index": 4, "text": "think"},
		},
	}, "root-1"))
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, harnessprotocol.OutputChannelReasoning,
		events[0].payload.(harnessprotocol.OutputEvent).Channel)

	// 无文本不产事件
	events, err = acc.Consume(sessionEvent(map[string]any{
		"type": "assistant/chunk",
		"data": map[string]any{
			"turn": 1, "step": 2,
			"chunk": map[string]any{"type": "text-delta", "index": 5},
		},
	}, "root-1"))
	require.NoError(t, err)
	assert.Empty(t, events)
}

// Python: mapping.py:314-335 block-end → FINAL；answer 通道更新 last_text_output
func TestAccumulator_assistantChunk块结束(t *testing.T) {
	acc := NewDshTurnAccumulator("turn-1", "root-1")
	events, err := acc.Consume(sessionEvent(map[string]any{
		"type": "assistant/chunk", "turn": 1, "step": 0,
		"data": map[string]any{
			"turn": 1, "step": 0,
			"chunk": map[string]any{
				"type": "block-end", "index": 0,
				"block": map[string]any{"type": "text", "text": "final text"},
			},
		},
	}, "root-1"))
	require.NoError(t, err)
	require.Len(t, events, 1)
	out := events[0].payload.(harnessprotocol.OutputEvent)
	assert.Equal(t, harnessprotocol.OutputOperationFinal, out.Operation)
	assert.Equal(t, "final text", out.Content)
	assert.Equal(t, "final text", acc.lastTextOutput)
	assert.Equal(t, "dsh-output:1:0:0:answer", out.OutputID)
}

// Python: mapping.py:341-404 assistant/message：FINAL 去重 + TurnMessage + answer 拼接
func TestAccumulator_assistantMessage(t *testing.T) {
	acc := NewDshTurnAccumulator("turn-1", "root-1")
	event := map[string]any{
		"type": "assistant/message", "turn": 1, "step": 0,
		"data": map[string]any{
			"turn": 1, "step": 0,
			"message": map[string]any{
				"id": "m-1",
				"content": []any{
					map[string]any{"type": "text", "id": "b1", "text": "A"},
					map[string]any{"type": "reasoning", "id": "b2", "text": "R"},
				},
				"source": map[string]any{"kind": "model"},
			},
		},
	}
	events, err := acc.Consume(sessionEvent(event, "root-1"))
	require.NoError(t, err)
	// text + reasoning 两个 FINAL
	require.Len(t, events, 2)
	assert.Equal(t, "A", acc.lastTextOutput)
	require.Len(t, acc.messages, 1)
	msg := acc.messages[0]
	assert.Equal(t, "m-1", msg.MessageID)
	assert.Equal(t, harnessprotocol.MessageRoleAssistant, msg.Role)
	require.Len(t, msg.Content, 2)
	assert.Equal(t, "text", msg.Content[0].Kind)

	// 重复消息不再入列、FINAL 不再重发
	events, err = acc.Consume(sessionEvent(event, "root-1"))
	require.NoError(t, err)
	assert.Empty(t, events)
	assert.Len(t, acc.messages, 1)
}

// ──────────────────────────── 工具与步骤 ────────────────────────────

// Python: mapping.py:406-448
func TestAccumulator_toolCall与Result(t *testing.T) {
	acc := NewDshTurnAccumulator("turn-1", "root-1")
	events, err := acc.Consume(sessionEvent(map[string]any{
		"type": "tool/call",
		"data": map[string]any{
			"turn": 1, "step": 1,
			"callId": "c-1", "name": "read_file", "arguments": map[string]any{"path": "x"},
		},
	}, "root-1"))
	require.NoError(t, err)
	require.Len(t, events, 1)
	started := events[0].payload.(harnessprotocol.ItemLifecycleEvent)
	assert.Equal(t, harnessprotocol.ItemEventKindStarted, started.Kind)
	assert.Equal(t, "c-1", events[0].itemID)

	events, err = acc.Consume(sessionEvent(map[string]any{
		"type": "tool/result", "turn": 1, "step": 1,
		"data": map[string]any{
			"callId": "c-1", "error": map[string]any{"msg": "boom"}, "meta": nil,
			"message": map[string]any{
				"source":  map[string]any{"callId": "c-1"},
				"content": "body",
			},
		},
	}, "root-1"))
	require.NoError(t, err)
	require.Len(t, events, 1)
	completed := events[0].payload.(harnessprotocol.ItemLifecycleEvent)
	assert.Equal(t, harnessprotocol.ItemEventKindCompleted, completed.Kind)
	assert.Equal(t, "c-1", events[0].itemID)
	b, _ := marshalJSON(completed.Data)
	assert.Contains(t, string(b), `"tool_name":"read_file"`)
	assert.Contains(t, string(b), `"error"`)
}

// Python: mapping.py:450-462
func TestAccumulator_step事件(t *testing.T) {
	acc := NewDshTurnAccumulator("turn-1", "root-1")
	events, err := acc.Consume(sessionEvent(map[string]any{
		"type": "step/start",
		"data": map[string]any{"turn": 2, "step": 3},
	}, "root-1"))
	require.NoError(t, err)
	require.Len(t, events, 1)
	step := events[0].payload.(harnessprotocol.ItemLifecycleEvent)
	assert.Equal(t, harnessprotocol.ItemEventKindStarted, step.Kind)
	assert.Equal(t, "dsh-step:2:3", events[0].itemID)
}

// ──────────────────────────── usage ────────────────────────────

// Python: mapping.py:474-486 + 238-262 + 601-614
func TestAccumulator_usage按步去重累计(t *testing.T) {
	acc := NewDshTurnAccumulator("turn-1", "root-1")
	usageEvent := func() map[string]any {
		return map[string]any{
			"type": "assistant/chunk", "turn": 1, "step": 1,
			"data": map[string]any{
				"turn": 1, "step": 1,
				"chunk": map[string]any{
					"type": "usage",
					"usage": map[string]any{
						"inputTokens": 10, "outputTokens": 5,
						"cacheReadTokens": 2, "cacheWriteTokens": 3, "reasoningTokens": 1,
					},
				},
			},
		}
	}
	events, err := acc.Consume(sessionEvent(usageEvent(), "root-1"))
	require.NoError(t, err)
	require.Len(t, events, 1)
	updated := events[0].payload.(harnessprotocol.UsageUpdatedEvent)
	assert.Equal(t, harnessprotocol.UsageUpdateModeCumulative, updated.Mode)
	require.NotNil(t, updated.Usage.TotalTokens)
	assert.Equal(t, 10+5+2+3, *updated.Usage.TotalTokens)

	// 同 (turn,step) 相同 usage 不重复发事件
	events, err = acc.Consume(sessionEvent(usageEvent(), "root-1"))
	require.NoError(t, err)
	assert.Empty(t, events)
}

// ──────────────────────────── 终态 ────────────────────────────

var (
	testTimingAt  = float64(time.Now().UnixNano()) / 1e9
	testTimingMon = time.Now()
)

// Python: mapping.py:135-187 六分支
func TestAccumulator_terminal六分支(t *testing.T) {
	cases := []struct {
		finishReason string
		wantKind     harnessprotocol.TurnEventKind
		wantStatus   harnessprotocol.TurnStatus
		wantTermKind harnessprotocol.TurnTerminationKind
		wantErrCode  string
	}{
		{"completed", harnessprotocol.TurnEventKindFinished, harnessprotocol.TurnStatusCompleted, "", ""},
		{"max-tokens", harnessprotocol.TurnEventKindFinished, harnessprotocol.TurnStatusCompleted, "", ""},
		{"blocked", harnessprotocol.TurnEventKindAborted, harnessprotocol.TurnStatusInterrupted, harnessprotocol.TurnTerminationKindPolicy, ""},
		{"aborted", harnessprotocol.TurnEventKindAborted, harnessprotocol.TurnStatusInterrupted, harnessprotocol.TurnTerminationKindUserAbort, ""},
		{"interrupted", harnessprotocol.TurnEventKindAborted, harnessprotocol.TurnStatusInterrupted, harnessprotocol.TurnTerminationKindProvider, ""},
		{"error", harnessprotocol.TurnEventKindFailed, harnessprotocol.TurnStatusFailed, "", "E1"},
		{"???", harnessprotocol.TurnEventKindFailed, harnessprotocol.TurnStatusFailed, "", "DSH_UNKNOWN_FINISH_REASON"},
	}
	for _, tc := range cases {
		t.Run(tc.finishReason, func(t *testing.T) {
			acc := NewDshTurnAccumulator("turn-1", "root-1")
			acc.lastTurnReason = map[string]any{
				"kind":   "user",
				"reason": map[string]any{"kind": "user"},
				"error":  map[string]any{"code": "E1"},
			}
			kind, result, err := acc.BuildTerminalResult(&deepseekharness.RunResult{
				FinalResponse: "done", FinishReason: strPtr(tc.finishReason),
			}, testTimingAt, testTimingMon)
			require.NoError(t, err)
			assert.Equal(t, tc.wantKind, kind)
			assert.Equal(t, tc.wantStatus, result.Status)
			if tc.wantTermKind != "" {
				require.NotNil(t, result.Termination)
				assert.Equal(t, tc.wantTermKind, result.Termination.Kind)
			}
			if tc.wantErrCode != "" {
				require.NotNil(t, result.Error)
				require.NotNil(t, result.Error.Code)
				assert.Equal(t, tc.wantErrCode, *result.Error.Code)
			}
			assert.Equal(t, "done", result.FinalOutput)
			assert.NotNil(t, result.ProviderData["native_turn_end_count"])
		})
	}
}

// Python: mapping.py:135-144 缺 turn/end → DSH_MISSING_TURN_END
func TestAccumulator_idleWithoutTurnEnd(t *testing.T) {
	acc := NewDshTurnAccumulator("turn-1", "root-1")
	kind, result, err := acc.BuildTerminalResult(&deepseekharness.RunResult{
		FinalResponse: "text", FinishReason: nil,
	}, testTimingAt, testTimingMon)
	require.NoError(t, err)
	assert.Equal(t, harnessprotocol.TurnEventKindFailed, kind)
	require.NotNil(t, result.Error)
	require.NotNil(t, result.Error.Code)
	assert.Equal(t, "DSH_MISSING_TURN_END", *result.Error.Code)
	// final_response 非串回落 last_text_output
	assert.Equal(t, "text", result.FinalOutput)
}

// Python: mapping.py:189-213
func TestAccumulator_failedResult(t *testing.T) {
	acc := NewDshTurnAccumulator("turn-1", "root-1")
	result, err := acc.BuildFailedResult(assert.AnError, testTimingAt, testTimingMon)
	require.NoError(t, err)
	assert.Equal(t, harnessprotocol.TurnStatusFailed, result.Status)
	require.NotNil(t, result.Error)
	assert.Equal(t, "provider_sdk", *result.Error.Category)
	b, _ := marshalJSON(result.ProviderData)
	assert.Contains(t, string(b), "exception_type")
	// 不复制 stderr 文本
	assert.NotContains(t, string(b), assert.AnError.Error())
}

// Python: mapping.py:215-236
func TestAccumulator_stoppedResult(t *testing.T) {
	acc := NewDshTurnAccumulator("turn-1", "root-1")
	result, err := acc.BuildStoppedResult(testTimingAt, testTimingMon)
	require.NoError(t, err)
	assert.Equal(t, harnessprotocol.TurnStatusInterrupted, result.Status)
	require.NotNil(t, result.Termination)
	assert.Equal(t, harnessprotocol.TurnTerminationKindHarnessStop, result.Termination.Kind)
}

// Python: mapping.py:505-517
func TestAccumulator_terminationKind映射(t *testing.T) {
	acc := NewDshTurnAccumulator("turn-1", "root-1")
	cases := map[string]harnessprotocol.TurnTerminationKind{
		"user":     harnessprotocol.TurnTerminationKindUserAbort,
		"disposed": harnessprotocol.TurnTerminationKindHarnessStop,
		"hook":     harnessprotocol.TurnTerminationKindPolicy,
		"other":    harnessprotocol.TurnTerminationKindProvider,
	}
	for cause, want := range cases {
		acc.lastTurnReason = map[string]any{"reason": map[string]any{"kind": cause}}
		assert.Equal(t, want, acc.terminationKind("aborted"), cause)
	}
	// interrupted 恒 PROVIDER
	assert.Equal(t, harnessprotocol.TurnTerminationKindProvider,
		acc.terminationKind("interrupted"))
}

// Python: mapping.py:520-533
func TestQueuedStopResult(t *testing.T) {
	result, err := BuildQueuedStopResult()
	require.NoError(t, err)
	assert.Equal(t, harnessprotocol.TurnStatusInterrupted, result.Status)
	assert.Equal(t, int64(0), int64(*result.DurationMS))
	require.NotNil(t, result.Termination)
	assert.Equal(t, harnessprotocol.TurnTerminationKindHarnessStop, result.Termination.Kind)
}

// ──────────────────────────── 助手 ────────────────────────────

// marshalJSON 测试用 JSON 序列化。
func marshalJSON(value any) ([]byte, error) {
	return jsonMarshal(value)
}

// jsonMarshal encoding/json 别名。
func jsonMarshal(value any) ([]byte, error) { return json.Marshal(value) }
