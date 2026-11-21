package evaluator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
)

// makeToolTrajectory 经 fixture 构造含 LLM+tool span 的轨迹。
func makeToolTrajectory(t *testing.T, sessionID string) *trajectory.Trajectory {
	t.Helper()
	traj, err := trajectory.NewTestTrajectory(sessionID, nil, []trajectory.TestStep{
		{Kind: "llm", Messages: []map[string]any{{"role": "user", "content": "hi"}},
			Response: map[string]any{"content": "hello"}},
		{Kind: "tool", ToolName: "bash", CallArgs: map[string]any{"cmd": "ls"},
			CallResult: map[string]any{"out": "files"}},
	})
	require.NoError(t, err)
	return traj
}

func attrEntry(key, value string) map[string]any {
	return map[string]any{"key": key, "value": map[string]any{"stringValue": value}}
}

func spanMap(category string, attrs map[string]any) map[string]any {
	attributes := []any{}
	for key, value := range attrs {
		attributes = append(attributes, map[string]any{
			"key": key, "value": map[string]any{"stringValue": fmtSprint(value)},
		})
	}
	return map[string]any{
		"name":       category,
		"spanId":     "sp-" + category,
		"attributes": attributes,
	}
}

func fmtSprint(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	b, _ := json.Marshal(value)
	return string(b)
}

// TestRoleFileTrajectoryStore_覆写写紧凑快照 对齐 Python save 主链。
func TestRoleFileTrajectoryStore_覆写写紧凑快照(t *testing.T) {
	baseDir := t.TempDir()
	store := NewRoleFileTrajectoryStore(baseDir, "solver role", false)
	traj := makeToolTrajectory(t, "sess-1")
	require.NoError(t, store.Save(traj, ""))

	path := filepath.Join(baseDir, "solver_role.jsonl")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(raw, &payload))
	assert.NotEmpty(t, payload["execution_id"])
	steps, _ := payload["steps"].([]any)
	require.Len(t, steps, 2)
	first, _ := steps[0].(map[string]any)
	assert.Equal(t, "llm", first["kind"])
	// 覆写语义：再次保存不追加。
	require.NoError(t, store.Save(traj, ""))
	raw2, _ := os.ReadFile(path)
	var payload2 map[string]any
	require.NoError(t, json.Unmarshal(raw2, &payload2))
	steps2, _ := payload2["steps"].([]any)
	assert.Len(t, steps2, 2, "覆写不应追加")
}

// TestRoleFileTrajectoryStore_合并模式 merge_invocations=true 时步骤累积。
func TestRoleFileTrajectoryStore_合并模式(t *testing.T) {
	baseDir := t.TempDir()
	store := NewRoleFileTrajectoryStore(baseDir, "solver", true)
	traj := makeToolTrajectory(t, "sess-1")
	require.NoError(t, store.Save(traj, ""))
	require.NoError(t, store.Save(traj, ""))
	raw, _ := os.ReadFile(filepath.Join(baseDir, "solver.jsonl"))
	var payload map[string]any
	require.NoError(t, json.Unmarshal(raw, &payload))
	steps, _ := payload["steps"].([]any)
	assert.Len(t, steps, 4, "合并模式应累积两轮步骤")
}

// TestSafeRoleFileStem 对齐 Python 正则净化。
func TestSafeRoleFileStem(t *testing.T) {
	assert.Equal(t, "solver_role", SafeRoleFileStem("solver role"))
	assert.Equal(t, "unknown", SafeRoleFileStem(""))
	assert.Equal(t, "a.b-c", SafeRoleFileStem("a.b-c"))
	assert.Equal(t, "x", SafeRoleFileStem("x\n"))
	assert.Equal(t, "unknown", SafeRoleFileStem("._-"))
}

// TestTruncateText_截断标记 对齐 Python "...[truncated N chars]"。
func TestTruncateText_截断标记(t *testing.T) {
	got := truncateText(strings.Repeat("a", 1300), MaxSavedTextChars)
	text, _ := got.(string)
	assert.Contains(t, text, "...[truncated 100 chars]")
	assert.Len(t, text, MaxSavedTextChars+len("...[truncated 100 chars]"))
	if truncateText("short", 100) != "short" {
		t.Fatal("短文本原样")
	}
}

// TestBoundedTrajectoryDict_消息头尾保留 超量消息 head=2/tail=2 且 meta 记省略数。
func TestBoundedTrajectoryDict_消息头尾保留(t *testing.T) {
	messages := []any{}
	for i := 0; i < 10; i++ {
		messages = append(messages, map[string]any{"role": "user", "content": strings.Repeat("x", 10)})
	}
	data := map[string]any{"steps": []any{
		map[string]any{"kind": "llm", "detail": map[string]any{"messages": messages}},
	}}
	bounded := BoundedTrajectoryDict(data)
	steps, _ := bounded["steps"].([]any)
	detail, _ := steps[0].(map[string]any)["detail"].(map[string]any)
	kept, _ := detail["messages"].([]any)
	assert.Len(t, kept, MaxSavedLLMMessages)
	meta, _ := detail["meta"].(map[string]any)
	assert.Equal(t, 6, meta["omitted_message_count"])
}

// TestRoleExecutionEvidence_全分支 对齐 Python role_execution_evidence 判定链。
func TestRoleExecutionEvidence_全分支(t *testing.T) {
	traceRoot := t.TempDir()
	// 缺归档 → missing_role_archive。
	ev := RoleExecutionEvidence(traceRoot, "solver", false, nil, nil, nil)
	assert.Equal(t, "incomplete", ev["status"])
	reasons, _ := ev["reasons"].([]string)
	assert.Contains(t, reasons, "run_root_not_recording")
	assert.Contains(t, reasons, "missing_role_archive")

	// 完整归档 + 根录制 + 计数匹配 → complete。
	role := "solver"
	archive := map[string]any{"steps": []any{
		map[string]any{"kind": "llm", "detail": map[string]any{"model": "m"}},
		map[string]any{"kind": "tool", "detail": map[string]any{"tool_name": "bash",
			"call_args": `{"cmd":"ls"}`, "call_result": `{"out":1}`}},
	}}
	raw, _ := json.Marshal(archive)
	require.NoError(t, os.WriteFile(filepath.Join(traceRoot, role+".jsonl"), raw, 0o644))
	var modelCalls any = 1
	var toolCalls any = 1
	ev = RoleExecutionEvidence(traceRoot, role, true, nil, modelCalls, toolCalls)
	assert.Equal(t, "complete", ev["status"])
	assert.Equal(t, 1, ev["llm_call_count"])
	assert.Equal(t, true, ev["timing_complete"])

	// 计数不匹配 + 截断载荷 → incomplete + 双原因。
	broken := map[string]any{"steps": []any{
		map[string]any{"kind": "tool", "detail": map[string]any{"tool_name": "bash",
			"call_args": strings.Repeat("a", 1300) + "", "call_result": nil}},
	}}
	raw, _ = json.Marshal(broken)
	require.NoError(t, os.WriteFile(filepath.Join(traceRoot, role+".jsonl"), raw, 0o644))
	var expect2 any = 2
	ev = RoleExecutionEvidence(traceRoot, role, true, []string{"tool_payload_json_error"}, nil, expect2)
	assert.Equal(t, "incomplete", ev["status"])
	reasons, _ = ev["reasons"].([]string)
	assert.Contains(t, reasons, "tool_payload_json_error")
	assert.Contains(t, reasons, "missing_model_calls")
	assert.Contains(t, reasons, "missing_tool_result")
	assert.Contains(t, reasons, "tool_call_count_mismatch")
	// call_args 为字符串（非 nil）不触发 missing_tool_arguments（Python get() is None 同语义）；
	// 明文 1300 字符未过 bounded 落盘链 → 无截断标记。
	assert.Equal(t, false, ev["payloads_truncated"])

	// 非法归档 → invalid_role_archive。
	require.NoError(t, os.WriteFile(filepath.Join(traceRoot, role+".jsonl"), []byte("{bad"), 0o644))
	ev = RoleExecutionEvidence(traceRoot, role, true, nil, nil, nil)
	reasons, _ = ev["reasons"].([]string)
	assert.Contains(t, reasons, "invalid_role_archive")
}
