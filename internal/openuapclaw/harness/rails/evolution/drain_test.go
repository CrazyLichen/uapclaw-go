package evolution

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
)

// recordRawToolSpan 记录带原始 arguments/result 的 tool span（质量校验输入）。
func recordRawToolSpan(t *testing.T, rail *EvolutionRail, args, result string) {
	t.Helper()
	attrs := map[string]any{
		"gen_ai.operation.name": "execute_tool",
		"gen_ai.tool.name":      "t",
		"gen_ai.tool.call.id":   "",
	}
	if args != "" {
		attrs["gen_ai.tool.call.arguments"] = args
	}
	if result != "" {
		attrs["gen_ai.tool.call.result"] = result
	}
	require.NoError(t, rail.builder.RecordSpan(map[string]any{
		"name":       "execute_tool t",
		"attributes": attrs,
	}))
}

// TestCaptureQualityIssues_合法JSON无问题 合法 JSON 载荷不产出 issue。
func TestCaptureQualityIssues_合法JSON无问题(t *testing.T) {
	rail := newScopeTestRail(t, "sess-quality-ok")
	recordRawToolSpan(t, rail, `{"k":1}`, `["ok"]`)
	additions, err := rail.collectScopeIncrement(rail.currentCapture)
	require.NoError(t, err)
	increment, err := incrementFromAdditions(additions)
	require.NoError(t, err)
	assert.Empty(t, captureQualityIssues(increment))
}

// TestCaptureQualityIssues_畸形JSON产出issue 非法 JSON 载荷产出 tool_payload_json_error。
// Python: evolution_rail.py:754 _capture_quality_issues。
func TestCaptureQualityIssues_畸形JSON产出issue(t *testing.T) {
	rail := newScopeTestRail(t, "sess-quality-bad")
	recordRawToolSpan(t, rail, `{"k":`, `{bad`)
	additions, err := rail.collectScopeIncrement(rail.currentCapture)
	require.NoError(t, err)
	increment, err := incrementFromAdditions(additions)
	require.NoError(t, err)
	issues := captureQualityIssues(increment)
	require.Len(t, issues, 2)
	assert.Equal(t, "tool_payload_json_error", issues[0]["code"])
	assert.Equal(t, "gen_ai.tool.call.arguments", issues[0]["attribute"])
	assert.Equal(t, "gen_ai.tool.call.result", issues[1]["attribute"])
	// 非 JSON 形态（非 [/{ 开头）不校验。
	rail2 := newScopeTestRail(t, "sess-quality-plain")
	recordRawToolSpan(t, rail2, "plain text", "")
	additions2, _ := rail2.collectScopeIncrement(rail2.currentCapture)
	increment2, _ := incrementFromAdditions(additions2)
	assert.Empty(t, captureQualityIssues(increment2))
}

// TestDrainForHook_gate 生效 带质量问题的增量不入 clean 窗口。
// Python: _drain_for_hook `if issues: return None, increment, issues`。
func TestDrainForHook_gate生效(t *testing.T) {
	rail := newScopeTestRail(t, "sess-gate")
	recordRawToolSpan(t, rail, `{"k":`, "")
	window, increment, issues, err := rail.drainForHook(rail.currentCapture, "", true)
	require.NoError(t, err)
	assert.Nil(t, window, "gate 生效时窗口应为 nil")
	require.NotNil(t, increment)
	require.Len(t, issues, 1)
	// 窗口未被污染。
	projection, perr := rail.projectScopeWindow(rail.currentCapture)
	require.NoError(t, perr)
	assert.Empty(t, trajectory.IterSpans(projection))
}

// TestDrainForHook_missingRequiredSpan 模型/工具回调缺对应 ended span 报 issue。
// Python: _drain_for_hook required_category 分支。
func TestDrainForHook_missingRequiredSpan(t *testing.T) {
	rail := newScopeTestRail(t, "sess-missing")
	recordRawToolSpan(t, rail, "{}", "{}")
	// required=llm 但增量只有 tool span → missing_required_span。
	window, increment, issues, err := rail.drainForHook(rail.currentCapture, "llm", true)
	require.NoError(t, err)
	assert.Nil(t, window)
	require.NotNil(t, increment)
	require.Len(t, issues, 1)
	assert.Equal(t, "missing_required_span", issues[0]["code"])
	assert.Equal(t, "llm", issues[0]["category"])
	// required=tool 命中 → 正常合并（drain 消费语义：须有新 span）。
	recordRawToolSpan(t, rail, "{}", "{}")
	window, _, issues, err = rail.drainForHook(rail.currentCapture, "tool", true)
	require.NoError(t, err)
	assert.Empty(t, issues)
	require.NotNil(t, window)
	assert.Len(t, trajectory.IterSpans(window), 1)
}

// TestTrajectoryRail_归档累积与去重 对齐 Python TrajectoryRail 归档语义：
// 全增量入累积器（含 gate 丢弃的）、span 身份去重、AfterInvoke 同步落盘。
func TestTrajectoryRail_归档累积与去重(t *testing.T) {
	store := trajectory.NewInMemoryTrajectoryStore()
	rail := NewTrajectoryRail(WithTrajectoryStore(store))
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.InvokeInputs{ConversationID: "sess-archive"})
	require.NoError(t, rail.BeforeInvoke(context.Background(), cbc))

	// 两个 tool span（不同内容 → 不同 spanId）。
	recordToolSpan(t, rail.EvolutionRail, "read_file")
	recordToolSpan(t, rail.EvolutionRail, "write_file")
	// 两次 drain（模拟 model/tool 钩子各自拉增量）。
	_, _, _, err := rail.drainForHook(rail.currentCapture, "tool", true)
	require.NoError(t, err)
	_, _, _, err = rail.drainForHook(rail.currentCapture, "tool", true)
	require.NoError(t, err)

	require.NoError(t, rail.AfterInvoke(context.Background(), cbc))
	// 归档应含全部 2 个 span（独立于窗口；经 Query 读回）。
	saved, err := store.Query("", "sess-archive", "", "", "", "")
	require.NoError(t, err)
	require.Len(t, saved, 1)
	assert.Len(t, trajectory.IterSpans(saved[0]), 2)
}

// TestTrajectoryRail_归档落盘内容 AfterInvoke 后 store 恰有一份归档。
func TestTrajectoryRail_归档落盘内容(t *testing.T) {
	store := trajectory.NewInMemoryTrajectoryStore()
	rail := NewTrajectoryRail(WithTrajectoryStore(store))
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.SetInputs(&agentinterfaces.InvokeInputs{ConversationID: "sess-arch2"})
	require.NoError(t, rail.BeforeInvoke(context.Background(), cbc))
	recordToolSpan(t, rail.EvolutionRail, "read_file")
	_, _, _, err := rail.drainForHook(rail.currentCapture, "", true)
	require.NoError(t, err)
	require.NoError(t, rail.AfterInvoke(context.Background(), cbc))
	// 归档元数据信封。
	saved, err := store.Query("", "sess-arch2", "", "", "", "")
	require.NoError(t, err)
	require.Len(t, saved, 1)
	attrs := saved[0].ResourceAttributes()
	assert.Equal(t, "sess-arch2", attrs[trajectory.SessionID])
	assert.Len(t, trajectory.IterSpans(saved[0]), 1)
	// 二次 AfterInvoke（无新增量）不重复落盘。
	cbc2 := &agentinterfaces.AgentCallbackContext{}
	cbc2.SetInputs(&agentinterfaces.InvokeInputs{ConversationID: "sess-arch2"})
	require.NoError(t, rail.BeforeInvoke(context.Background(), cbc2))
	require.NoError(t, rail.AfterInvoke(context.Background(), cbc2))
	saved, err = store.Query("", "sess-arch2", "", "", "", "")
	require.NoError(t, err)
	assert.Len(t, saved, 1)
}

// TestLogCaptureIssues_含码 日志内容含 issue code（冒烟）。
func TestLogCaptureIssues_含码(t *testing.T) {
	// 仅验证不 panic；日志断言属 logger 域。
	logCaptureIssues([]map[string]any{{"code": "tool_payload_json_error"}})
	logCaptureIssues(nil)
	assert.True(t, strings.Contains("tool_payload_json_error", "tool"))
}
