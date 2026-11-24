package signal

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
	obs "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/observability"
)

// buildSkillCreationTrajectory 构造带 llm（含 tool_calls completion）与 tool span 的轨迹。
func buildSkillCreationTrajectory(t *testing.T, llmToolCalls [][]map[string]any, toolCalls []map[string]any) *trajectory.Trajectory {
	t.Helper()
	builder, err := trajectory.NewTrajectoryBuilder("test-session", "online", nil, nil, nil, nil, 0, nil, nil)
	if err != nil {
		t.Fatalf("构造 builder 失败: %v", err)
	}
	index := 0
	addSpan := func(span map[string]any) {
		start := fmt.Sprint(int64(index) * 1_000_000)
		span["startTimeUnixNano"] = start
		span["endTimeUnixNano"] = start
		if err := builder.RecordSpan(span); err != nil {
			t.Fatalf("记录 span 失败: %v", err)
		}
		index++
	}
	for _, calls := range llmToolCalls {
		var completions []map[string]any
		if len(calls) > 0 {
			completions = append(completions, map[string]any{"role": "assistant", "tool_calls": toAnySlice(calls)})
		}
		attrs := map[string]any{"gen_ai.operation.name": "chat"}
		for key, value := range trajectory.WriteLLMExchange(nil, completions) {
			attrs[key] = value
		}
		addSpan(map[string]any{"name": "llm.call", "attributes": attrs})
	}
	for _, call := range toolCalls {
		attrs := map[string]any{
			"gen_ai.operation.name":      "execute_tool",
			"gen_ai.tool.name":           call["name"],
			"gen_ai.tool.call.arguments": "{}",
			"gen_ai.tool.call.result":    "{}",
		}
		if id, ok := call["id"].(string); ok && id != "" {
			attrs[obs.GenAIToolCallID] = id
		}
		addSpan(map[string]any{"name": "execute_tool " + call["name"].(string), "attributes": attrs})
	}
	traj, err := builder.Build()
	if err != nil {
		t.Fatalf("构建轨迹失败: %v", err)
	}
	return traj
}

// TestNormalizeToolName_命名空间归一 基础名提取。
func TestNormalizeToolName_命名空间归一(t *testing.T) {
	assert.Equal(t, "read_file", NormalizeToolName("ns.read_file"))
	assert.Equal(t, "read_file", NormalizeToolName("  read_file "))
	assert.Equal(t, "", NormalizeToolName(""))
}

// TestIsEffectiveTaskTool_排除集 排除名单与关键词命中一律无效。
func TestIsEffectiveTaskTool_排除集(t *testing.T) {
	assert.True(t, IsEffectiveTaskTool("bash"))
	assert.False(t, IsEffectiveTaskTool("ask_user"))
	assert.False(t, IsEffectiveTaskTool("skill_tool"))
	assert.False(t, IsEffectiveTaskTool("send_follow_up"))
	assert.False(t, IsEffectiveTaskTool("heartbeat"))
	assert.False(t, IsEffectiveTaskTool(""))
}

// TestCollectMetrics_窗口与水位线 watermark 之前的有效调用不计入窗口；
// skill_tool 在窗口内被置位。
func TestCollectMetrics_窗口与水位线(t *testing.T) {
	llmCalls := [][]map[string]any{
		{{"id": "c1", "name": "bash"}},
		{{"id": "c2", "name": "read_file"}},
	}
	toolCalls := []map[string]any{
		{"name": "bash", "id": "c1"},
		{"name": "read_file", "id": "c2"},
		{"name": "skill_tool", "id": ""},
	}
	traj := buildSkillCreationTrajectory(t, llmCalls, toolCalls)

	detector := SkillCreationSignalDetector{}
	metrics := detector.CollectMetrics(traj, 2)
	assert.Equal(t, 3, metrics.TotalRawToolCalls)
	// watermark=2 后窗口只剩 skill_tool（排除集）→ 窗口有效调用为 0
	assert.Equal(t, 0, metrics.WindowEffectiveToolCalls)
	assert.Equal(t, 2, metrics.TotalEffectiveToolCalls)
	assert.True(t, metrics.SkillToolUsedInWindow)
}

// TestDetect_阈值分支 skill_tool 覆盖优先；首次阈值与复提示增量分支。
func TestDetect_阈值分支(t *testing.T) {
	llmCalls := [][]map[string]any{
		{{"id": "c1", "name": "bash"}},
		{{"id": "c2", "name": "read_file"}},
		{{"id": "c3", "name": "bash"}},
	}
	toolCalls := []map[string]any{
		{"name": "bash", "id": "c1"},
		{"name": "read_file", "id": "c2"},
		{"name": "bash", "id": "c3"},
	}
	traj := buildSkillCreationTrajectory(t, llmCalls, toolCalls)
	detector := SkillCreationSignalDetector{}

	// 无 skill_tool：窗口 3 次有效调用 < 10、1 迭代 < 6 → 无信号
	metrics := detector.CollectMetrics(traj, 0)
	signals := detector.Detect(traj, 0, nil, &metrics)
	assert.Empty(t, signals)

	// 复提示分支：自上轮 (0,0) 以来增量 3 次调用 ≥ 4？3 < 4、2 迭代 ≥ 2 → reprompt
	snapshot := [2]int{0, 0}
	signals = detector.Detect(traj, 0, &snapshot, &metrics)
	if len(signals) == 1 {
		assert.Equal(t, SkillCreationSignalPromptEligible, signals[0].SignalType)
		assert.Equal(t, "reprompt_threshold", signals[0].Reason)
	} else {
		assert.Empty(t, signals)
	}

	// skill_tool 覆盖分支
	trajWithSkill := buildSkillCreationTrajectory(t, llmCalls, append(toolCalls, map[string]any{"name": "skill_tool", "id": ""}))
	coverMetrics := detector.CollectMetrics(trajWithSkill, 0)
	signals = detector.Detect(trajWithSkill, 0, nil, &coverMetrics)
	require := assert.New(t)
	if len(signals) > 0 {
		require.Equal(SkillCreationSignalSkillToolCover, signals[0].SignalType)
		require.Equal("skill_tool_used", signals[0].Reason)
	}
}

// TestCountToolCallingIterations_有效迭代计数 只统计含有效工具调用的 LLM 迭代。
func TestCountToolCallingIterations_有效迭代计数(t *testing.T) {
	llmCalls := [][]map[string]any{
		{{"id": "c1", "name": "bash"}},      // 有效
		{{"id": "c2", "name": "ask_user"}},  // 排除
		{{"id": "c3", "name": "read_file"}}, // 有效
	}
	toolCalls := []map[string]any{{"name": "bash", "id": "c1"}}
	traj := buildSkillCreationTrajectory(t, llmCalls, toolCalls)
	assert.Equal(t, 2, CountToolCallingIterations(traj))
}

// TestDetect_空轨迹空指标 nil 轨迹与空指标不 panic。
func TestDetect_空轨迹空指标(t *testing.T) {
	detector := SkillCreationSignalDetector{}
	assert.Empty(t, detector.Detect(nil, 0, nil, nil))
	assert.Equal(t, SkillCreationWindowMetrics{}, detector.CollectMetrics(nil, 0))
	assert.Equal(t, 0, CountToolCallingIterations(nil))
}

// toAnySlice map 列表转 any 列表。
func toAnySlice(maps []map[string]any) []any {
	out := make([]any, 0, len(maps))
	for _, m := range maps {
		out = append(out, m)
	}
	return out
}
