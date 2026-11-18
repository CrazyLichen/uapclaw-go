// Package detectors golden 对拍测试（25.1g 补强，spec §4）。
//
// 期望值由上游 Python 真跑生成（gen_golden.py 手法：importlib 逐文件加载
// reliability 六模块绕过包 __init__ 的全链依赖），生成脚本与输出见
// docs/superpowers/specs/2026-12-02-25.1g-team-reliability-design.md 附录 A。
// 上游修改哈希/summary 格式时，重跑脚本更新本文件期望值。
//
// 已捕获并修复的 parity 缺陷：repeat summary 的 evidence 渲染应为 Python
// dict repr 形态（"{'call_repeats': 3}"）而非 "call_repeats=3"。
package detectors

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/rails/reliability/core"
)

// ──────────────────────────── golden 期望值 ────────────────────────────

// goldenValues 上游真跑产出的期望值（2026-12-02，上游 commit 对齐基准见 spec）。
//
// 生成命令（在 agent-core 仓库根执行）：
//
//	python3 -c "import importlib.util,types,sys,json; ... " # 见 spec 附录 A
var goldenValues = map[string]string{
	"call_hash_basic":    "51775c08377a2b6e6f4cce845a64f0678d0aa61981edaf36b9fba3e04b534d2a",
	"call_hash_reversed": "d09af1acebb117c880b28f429ea4e649d8ff6d4d2028df9d51047d7339cc452e",
	"call_hash_none_args": "9b751a138bd7788d4842702ada5167ed489887ca9cf0a3d37ecbc3ab3c5dfd45",
	"call_hash_unicode":  "141e649be3595344c7adc18a310f8333591624965cb4332c304b3ed9ba06fec2",
	"result_hash_str":    "ed7002b439e9ac845f22357d822bac1444730fbdb6016d3ec9432297b9ec9f73",
	"result_hash_dict":   "e1f864311fc0857f3fa94c5fa04a70052a6c477757897bd24efed7031ea1194c",
	"result_hash_number": "73475cb40a568e8da8a045ced110137e159f890ac4da883b6b17dc651b3a8049",
	"burst_summary":      "2 consecutive failures (2 within 60s)",
	"burst_evidence":     `{"consecutive": 2, "last_error": null, "window_count": 2, "window_seconds": 60.0}`,
	"output_summary":     "output_too_long: 200 chars exceeds 100",
	"output_kind":        "output_too_long",
	"repeat_summary":     "repeat_tool_call detected: {'call_repeats': 3}",
	"repeat_evidence":    `{"call_repeats": 3}`,
}

// ──────────────────────────── 稳定哈希对拍 ────────────────────────────

func TestGolden_稳定哈希_对拍上游(t *testing.T) {
	assert.Equal(t, goldenValues["call_hash_basic"],
		core.StableCallHash("read_file", map[string]any{"path": "a.txt"}))
	assert.Equal(t, goldenValues["call_hash_reversed"],
		core.StableCallHash("read_file", map[string]any{"b": 2, "a": 1}), "键序无关")
	assert.Equal(t, goldenValues["call_hash_none_args"],
		core.StableCallHash("bash", nil), "None args → {}")
	assert.Equal(t, goldenValues["call_hash_unicode"],
		core.StableCallHash("搜索", map[string]any{"问题": "你好"}), "ensure_ascii=False")
	assert.Equal(t, goldenValues["result_hash_str"], core.StableResultHash("content"))
	assert.Equal(t, goldenValues["result_hash_dict"],
		core.StableResultHash(map[string]any{"output": "ok", "code": 0}))
	assert.Equal(t, goldenValues["result_hash_number"], core.StableResultHash(42))
}

// ──────────────────────────── summary 渲染对拍 ────────────────────────────

// goldenBurstEvidence 把 golden JSON 期望值解回 map 供 Evidence 对拍。
func goldenBurstEvidence(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	assert.NoError(t, json.Unmarshal([]byte(goldenValues["burst_evidence"]), &m))
	return m
}

func TestGolden_ErrorBurst_summary对拍(t *testing.T) {
	now := 0.0
	d := NewErrorBurstDetector("tool_error_rate", core.SignalKindToolException,
		core.SignalKindAfterToolCall, core.AnomalyKindToolErrorRate,
		60, 5, 2, func() float64 { return now })
	// 对齐上游 golden 脚本：Signal 不带 error（last_error=None）
	var a *core.Anomaly
	for i := 0; i < 2; i++ {
		a = d.Observe(&core.Signal{Kind: core.SignalKindToolException, MemberName: "coder"})
	}
	assert.NotNil(t, a)
	assert.Equal(t, goldenValues["burst_summary"], a.Summary)
	// JSON 期望值数字解出 float64，与 Evidence 的 int 逐键比较（数值等价）
	ev := goldenBurstEvidence(t)
	assert.Equal(t, ev["consecutive"], float64(a.Evidence["consecutive"].(int)))
	assert.Equal(t, ev["window_count"], float64(a.Evidence["window_count"].(int)))
	assert.Equal(t, ev["window_seconds"], a.Evidence["window_seconds"])
	assert.Nil(t, a.Evidence["last_error"])
}

func TestGolden_OutputLength_summary对拍(t *testing.T) {
	d := NewOutputLengthDetector(100, 50)
	n := 200
	a := d.Observe(&core.Signal{Kind: core.SignalKindAfterModelCall, MemberName: "coder", TextLen: &n})
	assert.NotNil(t, a)
	assert.Equal(t, goldenValues["output_summary"], a.Summary)
	assert.Equal(t, goldenValues["output_kind"], string(a.Kind))
}

func TestGolden_RepeatTool_summary对拍(t *testing.T) {
	d := NewRepeatToolCallDetector(30, 3, 10, 20, 30)
	var a *core.Anomaly
	for i := 0; i < 3; i++ {
		d.Observe(&core.Signal{Kind: core.SignalKindBeforeToolCall, MemberName: "coder",
			ToolName: ptrTo("read_file"), ToolArgs: map[string]any{"path": "a.txt"}})
		a = d.Observe(&core.Signal{Kind: core.SignalKindAfterToolCall, MemberName: "coder", ToolResult: "same"})
	}
	assert.NotNil(t, a)
	assert.Equal(t, goldenValues["repeat_summary"], a.Summary,
		"evidence 渲染为 Python dict repr 形态（golden 捕获的 parity 缺陷修复锚点）")
	assert.Equal(t, map[string]any{"call_repeats": 3}, a.Evidence)
}
