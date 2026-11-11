package protocol

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSingleAgentMode_四值 验证模式枚举。
// Python: model.py:38-44 SingleAgentMode
func TestSingleAgentMode_四值(t *testing.T) {
	require.Equal(t, "agent.work.normal", string(ModeAgentWorkNormal))
	require.Equal(t, "agent.work.plan", string(ModeAgentWorkPlan))
	require.Equal(t, "agent.code.normal", string(ModeAgentCodeNormal))
	require.Equal(t, "agent.code.plan", string(ModeAgentCodePlan))
}

// TestRunStatus_四值 验证状态枚举。
// Python: model.py:47-53 RunStatus
func TestRunStatus_四值(t *testing.T) {
	require.Equal(t, "completed", string(StatusCompleted))
	require.Equal(t, "failed", string(StatusFailed))
	require.Equal(t, "cancelled", string(StatusCancelled))
	require.Equal(t, "timed_out", string(StatusTimedOut))
}

// TestFreezeJson_拒绝非有限数字 验证 NaN/Inf 被拒。
// Python: model.py:143-146 if not math.isfinite(value): raise ValueError
func TestFreezeJson_拒绝非有限数字(t *testing.T) {
	_, err := FreezeJson("x", math.Inf(1))
	require.Error(t, err)
	require.Contains(t, err.Error(), "finite")

	_, err = FreezeJson("x", math.NaN())
	require.Error(t, err)
}

// TestFreezeJson_拒绝循环引用 验证递归保护。
// Python: model.py:148-151 if marker in active: raise ValueError("reference cycles")
func TestFreezeJson_拒绝循环引用(t *testing.T) {
	m := map[string]any{}
	m["self"] = m
	_, err := FreezeJson("x", m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "reference cycles")
}

// TestFreezeJson_拒绝非JSON类型 验证类型守卫。
// Python: model.py:172 raise TypeError("must contain only JSON-compatible values")
func TestFreezeJson_拒绝非JSON类型(t *testing.T) {
	_, err := FreezeJson("x", make(chan int))
	require.Error(t, err)
	require.Contains(t, err.Error(), "JSON-compatible")
}

// TestThawJson_还原为可变副本 验证解冻。
// Python: model.py:182-187 _thaw_json
func TestThawJson_还原为可变副本(t *testing.T) {
	frozen, err := FreezeJson("x", map[string]any{"a": []any{1, 2}})
	require.NoError(t, err)
	thawed, ok := ThawJson(frozen).(map[string]any)
	require.True(t, ok)
	require.Equal(t, []any{1, 2}, thawed["a"])
}

// TestAgentSpec_name校验 验证三条 name 规则。
// Python: model.py:210-213
func TestAgentSpec_name校验(t *testing.T) {
	_, err := NewAgentSpec("ab", "do it")
	require.Error(t, err)
	require.Contains(t, err.Error(), "[A-Za-z0-9_-]{3,50}")

	_, err = NewAgentSpec("my agent", "do it")
	require.Error(t, err)

	s, err := NewAgentSpec("my_agent-1", "do it")
	require.NoError(t, err)
	require.Equal(t, "my_agent-1", s.Name)
}

// TestAgentSpec_instructions保留原文 验证 preserve=True 不 strip。
// Python: model.py:216-218 _required_text(preserve=True)
func TestAgentSpec_instructions保留原文(t *testing.T) {
	s, err := NewAgentSpec("abc", "  spaced  ")
	require.NoError(t, err)
	require.Equal(t, "  spaced  ", s.Instructions, "instructions 必须保留原文空白")
}

// TestAgentSpec_空instructions报错
func TestAgentSpec_空instructions报错(t *testing.T) {
	_, err := NewAgentSpec("abc", "   ")
	require.Error(t, err)
	require.Contains(t, err.Error(), "instructions must not be empty")
}

// TestAgentSpec_star必须唯一 验证 "*" 不能与其它工具共存。
// Python: model.py:226-227 if "*" in tools and tools != ("*",): raise ValueError
func TestAgentSpec_star必须唯一(t *testing.T) {
	_, err := NewAgentSpecWithTools("abc", "do", []string{"*", "read_file"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "'*' must be the only tools entry")

	s, err := NewAgentSpecWithTools("abc", "do", []string{"*"})
	require.NoError(t, err)
	require.Equal(t, []string{"*"}, s.Tools)
}

// TestAgentSpec_tools去重 验证重复项被拒。
// Python: model.py:107-109 if len(set(result)) != len(result): raise ValueError
func TestAgentSpec_tools去重(t *testing.T) {
	_, err := NewAgentSpecWithTools("abc", "do", []string{"a", "a"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicates")
}

// TestAgentSpec_maxIterations零报错 验证 0 被拒。
// Python: model.py:230-233
func TestAgentSpec_maxIterations零报错(t *testing.T) {
	s, err := NewAgentSpec("abc", "do")
	require.NoError(t, err)
	zero := 0
	require.Error(t, s.ValidateMaxIterations(&zero))
	one := 1
	require.NoError(t, s.ValidateMaxIterations(&one))
}

// TestAgentSpec_FromDict_拒未知字段
// Python: model.py:112-131 _strict_object
func TestAgentSpec_FromDict_拒未知字段(t *testing.T) {
	_, err := AgentSpecFromDict(map[string]any{
		"name": "abc", "instructions": "do", "bogus": 1,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown fields")
}

// TestAgentSpec_FromDict_缺必填报错
func TestAgentSpec_FromDict_缺必填报错(t *testing.T) {
	_, err := AgentSpecFromDict(map[string]any{"name": "abc"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing required fields")
}

// TestHostToolSpec_schema必须object
// Python: model.py:297-300 if schema.get("type") != "object": raise ValueError
func TestHostToolSpec_schema必须object(t *testing.T) {
	_, err := HostToolSpecFromDict(map[string]any{
		"name": "my_tool", "description": "d",
		"input_schema": map[string]any{"type": "array"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must describe an object")
}

// TestHostToolSpec_名字必须是ASCII标识符
// Python: model.py:289-291 if _TOOL_NAME.fullmatch(name) is None
func TestHostToolSpec_名字必须是ASCII标识符(t *testing.T) {
	_, err := HostToolSpecFromDict(map[string]any{
		"name": "1_bad", "description": "d",
		"input_schema": map[string]any{"type": "object"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ASCII tool identifier")
}

// runDoc 构造一份最小合法 run 文档。
func runDoc(extra map[string]any) map[string]any {
	doc := map[string]any{
		"schema_version": "0.1",
		"type":           "run",
		"input":          "hello",
	}
	for k, v := range extra {
		doc[k] = v
	}
	return doc
}

// TestOneShotRunInput_mode省略且新建会话默认codeNormal 验证第 13 步。
// Python: model.py:472-473 elif self.session_id is None: mode = CODE_NORMAL
func TestOneShotRunInput_mode省略且新建会话默认codeNormal(t *testing.T) {
	in, err := OneShotRunInputFromDict(runDoc(nil))
	require.NoError(t, err)
	require.NotNil(t, in.Mode)
	require.Equal(t, string(ModeAgentCodeNormal), *in.Mode)
}

// TestOneShotRunInput_mode省略但恢复会话保持nil 验证第 13 步的另一半。
// Python: model.py:472 elif self.session_id is None（有 session_id 则不设默认）
func TestOneShotRunInput_mode省略但恢复会话保持nil(t *testing.T) {
	in, err := OneShotRunInputFromDict(runDoc(map[string]any{"session_id": "sess-1"}))
	require.NoError(t, err)
	require.Nil(t, in.Mode, "恢复会话时 mode 必须保持 nil，由 descriptor 继承")
}

// TestOneShotRunInput_非法mode报错
// Python: model.py:466-471 raise ValueError(f"unsupported single-Agent mode: {mode}")
func TestOneShotRunInput_非法mode报错(t *testing.T) {
	_, err := OneShotRunInputFromDict(runDoc(map[string]any{"mode": "team.normal"}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported single-Agent mode")
}

// TestOneShotRunInput_maxTurns越界报错
// Python: model.py:447-450 if not 1 <= turns <= 1000: raise ValueError
func TestOneShotRunInput_maxTurns越界报错(t *testing.T) {
	for _, n := range []int{0, 1001} {
		_, err := OneShotRunInputFromDict(runDoc(map[string]any{"max_turns": n}))
		require.Error(t, err, "max_turns=%d 应报错", n)
	}
}

// TestOneShotRunInput_permissions层级校验
// Python: model.py:426-441 只允许 allow/ask/deny
func TestOneShotRunInput_permissions层级校验(t *testing.T) {
	_, err := OneShotRunInputFromDict(runDoc(map[string]any{
		"permissions": map[string]any{"tools": map[string]any{"rm": "maybe"}},
	}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "allow, ask, or deny")

	_, err = OneShotRunInputFromDict(runDoc(map[string]any{
		"permissions": map[string]any{"tools": map[string]any{"rm": "deny"}},
	}))
	require.NoError(t, err)
}

// TestOneShotRunInput_outputSchema必须object
// Python: model.py:442-446 if schema.get("type") != "object": raise ValueError
func TestOneShotRunInput_outputSchema必须object(t *testing.T) {
	_, err := OneShotRunInputFromDict(runDoc(map[string]any{
		"output_schema": map[string]any{"type": "array"},
	}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "output_schema must describe an object")
}

// TestOneShotRunInput_hostTools名字唯一
// Python: model.py:463-464 if len({tool.name}) != len(host_tools): raise ValueError
func TestOneShotRunInput_hostTools名字唯一(t *testing.T) {
	tool := map[string]any{
		"name": "my_tool", "description": "d",
		"input_schema": map[string]any{"type": "object"},
	}
	_, err := OneShotRunInputFromDict(runDoc(map[string]any{
		"host_tools": []any{tool, tool},
	}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "host_tools names must be unique")
}

// TestOneShotRunInput_不支持的schema_version
// Python: model.py:399-403
func TestOneShotRunInput_不支持的schema_version(t *testing.T) {
	doc := runDoc(nil)
	doc["schema_version"] = "9.9"
	_, err := OneShotRunInputFromDict(doc)
	require.Error(t, err)
	require.Contains(t, err.Error(), "schema_version is not supported")
}

// TestOneShotRunInput_type必须是run
// Python: model.py:542-543 if data["type"] != "run": raise ValueError
func TestOneShotRunInput_type必须是run(t *testing.T) {
	doc := runDoc(nil)
	doc["type"] = "query"
	_, err := OneShotRunInputFromDict(doc)
	require.Error(t, err)
	require.Contains(t, err.Error(), "run.type must be 'run'")
}

// TestOneShotRunResult_completed三条件 验证终态一致性守卫。
// Python: model.py:802-808 completed → session_id 非空 + exit_code 0 + error nil
func TestOneShotRunResult_completed三条件(t *testing.T) {
	// 缺 session_id
	_, err := NewOneShotRunResult(OneShotRunResultParams{
		Sequence: 0, RequestID: "r1", Status: string(StatusCompleted), ExitCode: 0,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "session_id")

	// exit_code 非 0
	sid := "s1"
	_, err = NewOneShotRunResult(OneShotRunResultParams{
		Sequence: 0, RequestID: "r1", Status: string(StatusCompleted), ExitCode: 1,
		SessionID: &sid,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "exit_code 0")

	// 带 error
	_, err = NewOneShotRunResult(OneShotRunResultParams{
		Sequence: 0, RequestID: "r1", Status: string(StatusCompleted), ExitCode: 0,
		SessionID: &sid, Error: &RuntimeErrorInfo{Code: "X", Message: "m"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not contain an error")
}

// TestOneShotRunResult_非completed两条件 验证失败态必须带 error 与非 0 码。
// Python: model.py:809-813 非 completed → exit_code 非 0 + error 非 nil
func TestOneShotRunResult_非completed两条件(t *testing.T) {
	_, err := NewOneShotRunResult(OneShotRunResultParams{
		Sequence: 0, RequestID: "r1", Status: string(StatusFailed), ExitCode: 0,
		Error: &RuntimeErrorInfo{Code: "X", Message: "m"},
	})
	require.Error(t, err)

	_, err = NewOneShotRunResult(OneShotRunResultParams{
		Sequence: 0, RequestID: "r1", Status: string(StatusFailed), ExitCode: 1,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must contain an error")
}

// TestOneShotEvent_payloadEventType须匹配 验证一致性守卫。
// Python: model.py:665-672 payload.event_type 必须与顶层一致
func TestOneShotEvent_payloadEventType须匹配(t *testing.T) {
	_, err := NewOneShotEvent(OneShotEventParams{
		Sequence: 0, RequestID: "r1", EventType: "chat.delta",
		Payload: map[string]any{"event_type": "chat.final"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must match the top-level event_type")
}

// TestOneShotRunInput_往返 验证 ToDict/FromDict 对称。
func TestOneShotRunInput_往返(t *testing.T) {
	orig, err := OneShotRunInputFromDict(runDoc(map[string]any{
		"request_id": "r1",
		"agent": map[string]any{
			"name": "agent1", "instructions": "be nice",
			"tools": []any{"read_file"},
		},
	}))
	require.NoError(t, err)

	dict := orig.ToDict()
	raw, err := json.Marshal(dict)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))

	back, err := OneShotRunInputFromDict(decoded)
	require.NoError(t, err)
	require.Equal(t, orig.Input, back.Input)
	require.Equal(t, orig.Agent.Name, back.Agent.Name)
	require.Equal(t, orig.Agent.Tools, back.Agent.Tools)
}
