// Package detectors 单元测试（repeat_tool / pingpong）。
//
// 对应 Python：detectors/repeat_tool.py、pingpong.py 行为内联断言。
package detectors

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/rails/reliability/core"
)

// newRepeatDetector 构造默认阈值检测器（30/10/10/20/30）。
func newRepeatDetector() *RepeatToolCallDetector {
	return NewRepeatToolCallDetector(30, 10, 10, 20, 30)
}

// call 一次完整调用：BEFORE（name+args）→ AFTER（result）。
func (d *RepeatToolCallDetector) call(t *testing.T, tool string, args map[string]any, result any) *core.Anomaly {
	t.Helper()
	assert.Nil(t, d.Observe(&core.Signal{Kind: core.SignalKindBeforeToolCall, MemberName: "coder", ToolName: &tool, ToolArgs: args}))
	return d.Observe(&core.Signal{Kind: core.SignalKindAfterToolCall, MemberName: "coder", ToolResult: result})
}

// fail 一次失败调用：BEFORE → TOOL_EXCEPTION。
func (d *RepeatToolCallDetector) fail(t *testing.T, tool string, args map[string]any, errMsg string) *core.Anomaly {
	t.Helper()
	assert.Nil(t, d.Observe(&core.Signal{Kind: core.SignalKindBeforeToolCall, MemberName: "coder", ToolName: &tool, ToolArgs: args}))
	e := errMsg
	return d.Observe(&core.Signal{Kind: core.SignalKindToolException, MemberName: "coder", Error: &e})
}

func TestRepeatToolCallDetector_同参同结果到LOW(t *testing.T) {
	d := newRepeatDetector()
	var got *core.Anomaly
	for i := 0; i < 10; i++ {
		got = d.call(t, "read_file", map[string]any{"path": "a.txt"}, "content")
	}
	assert.NotNil(t, got)
	assert.Equal(t, core.AnomalyKindRepeatToolCall, got.Kind)
	assert.Equal(t, core.SeverityLow, got.Severity)
	assert.Equal(t, map[string]any{"call_repeats": 10}, got.Evidence)
	assert.Equal(t, "repeat_tool_call detected: {'call_repeats': 10}", got.Summary,
		"evidence 渲染对齐 Python dict repr（golden 对拍锚定）")
}

func TestRepeatToolCallDetector_结果变化不算重复(t *testing.T) {
	d := newRepeatDetector()
	var got *core.Anomaly
	for i := 0; i < 12; i++ {
		got = d.call(t, "read_file", map[string]any{"path": "a.txt"}, fmt.Sprintf("v%d", i))
	}
	assert.Nil(t, got, "同参不同结果：正当迭代，不判重复")
}

func TestRepeatToolCallDetector_参数乱序同哈希(t *testing.T) {
	d := newRepeatDetector()
	// 前 5 次键序 A，后 5 次键序 B，合计 10 次同一 call_key → LOW
	for i := 0; i < 5; i++ {
		d.call(t, "read_file", map[string]any{"a": 1, "b": 2}, "x")
	}
	var got *core.Anomaly
	for i := 0; i < 5; i++ {
		got = d.call(t, "read_file", map[string]any{"b": 2, "a": 1}, "x")
	}
	assert.NotNil(t, got)
	assert.Equal(t, 10, got.Evidence["call_repeats"])
}

func TestRepeatToolCallDetector_交替乒乓MEDIUM封顶(t *testing.T) {
	d := newRepeatDetector()
	var got *core.Anomaly
	for i := 0; i < 10 && got == nil; i++ {
		d.call(t, "read_file", map[string]any{"p": "a"}, "ra")
		got = d.call(t, "write_file", map[string]any{"p": "b"}, "rb")
	}
	assert.NotNil(t, got, "尾部交替游程达 pingpong_warn 时首次发射")
	assert.Equal(t, core.AnomalyKindToolCallLoop, got.Kind)
	assert.Equal(t, core.SeverityMedium, got.Severity, "交替档封顶 MEDIUM")
	assert.Equal(t, map[string]any{"trailing_alternation": 10}, got.Evidence)
	// 继续交替不重复发射（同档边沿触发）
	d.call(t, "read_file", map[string]any{"p": "a"}, "ra")
	assert.Nil(t, d.call(t, "write_file", map[string]any{"p": "b"}, "rb"))
}

func TestRepeatToolCallDetector_交替结果不稳不算(t *testing.T) {
	d := newRepeatDetector()
	var got *core.Anomaly
	for i := 0; i < 12; i++ {
		d.call(t, "read_file", map[string]any{"p": "a"}, fmt.Sprintf("ra%d", i))
		got = d.call(t, "write_file", map[string]any{"p": "b"}, fmt.Sprintf("rb%d", i))
	}
	assert.Nil(t, got, "交替但结果每次变化：非乒乓")
}

func TestRepeatToolCallDetector_无进展循环HIGH与熔断CRITICAL(t *testing.T) {
	d := NewRepeatToolCallDetector(30, 10, 10, 20, 30)
	var got *core.Anomaly
	for i := 0; i < 20; i++ {
		got = d.call(t, "read_file", map[string]any{"p": "a"}, "same")
	}
	assert.NotNil(t, got)
	assert.Equal(t, core.SeverityHigh, got.Severity, "尾部相同 20 次 → HIGH")
	// 同 detector 继续到 30 → CRITICAL（仅升档时再发射）
	for i := 0; i < 10; i++ {
		got = d.call(t, "read_file", map[string]any{"p": "a"}, "same")
	}
	assert.NotNil(t, got)
	assert.Equal(t, core.SeverityCritical, got.Severity)
	assert.Equal(t, map[string]any{"trailing_identical": 30}, got.Evidence)
}

func TestRepeatToolCallDetector_失败计入outcome(t *testing.T) {
	d := newRepeatDetector()
	var got *core.Anomaly
	for i := 0; i < 10; i++ {
		got = d.fail(t, "bash", map[string]any{"cmd": "boom"}, "exit 1")
	}
	assert.NotNil(t, got, "同调用同错误文本也算循环")
}

func TestRepeatToolCallDetector_Reset(t *testing.T) {
	d := newRepeatDetector()
	for i := 0; i < 10; i++ {
		d.call(t, "read_file", map[string]any{"p": "a"}, "x")
	}
	d.Reset()
	var got *core.Anomaly
	for i := 0; i < 9; i++ {
		got = d.call(t, "read_file", map[string]any{"p": "a"}, "x")
	}
	assert.Nil(t, got, "重置后历史清空，9 次未达阈值")
}

func TestRepeatToolCallDetector_历史窗口截断(t *testing.T) {
	d := NewRepeatToolCallDetector(30, 10, 10, 20, 30)
	// 先 25 次别的调用填满历史
	for i := 0; i < 25; i++ {
		d.call(t, "noise", map[string]any{"i": i}, fmt.Sprintf("n%d", i))
	}
	// 再 6 次同参调用：历史窗口 30 只保留最近 30 条，其中同 call_key 只有 6 次
	var got *core.Anomaly
	for i := 0; i < 6; i++ {
		got = d.call(t, "read_file", map[string]any{"p": "a"}, "x")
	}
	assert.Nil(t, got, "6 次 < repeat_warn=10（旧记录已滚出窗口）")
}

// msg 构造 MESSAGE 信号。
func msg(t *testing.T, from, to string) *core.Signal {
	t.Helper()
	toPtr := to
	return &core.Signal{Kind: core.SignalKindMessage, MemberName: from, PeerMember: &toPtr}
}

func TestPingPongDetector_方向反转延长连击(t *testing.T) {
	d := NewPingPongDetector(6)
	// A→B, B→A, A→B ... 严格反转延长连击
	var got *core.Anomaly
	for i := 0; i < 12 && got == nil; i++ {
		got = d.Observe(msg(t, "alice", "bob"))
		if got == nil {
			got = d.Observe(msg(t, "bob", "alice"))
		}
	}
	assert.NotNil(t, got)
	assert.Equal(t, core.SeverityMedium, got.Severity, "连击 6 达 min_volleys 首次发射 MEDIUM")
	assert.Equal(t, map[string]any{"volleys": 6, "pair": []string{"alice", "bob"}}, got.Evidence)
	assert.Equal(t, "bob", got.MemberName, "第 6 击触发方为 bob（A,B 交替序）")
	assert.NotNil(t, got.PeerMember)
	assert.Equal(t, "6 consecutive volleys between bob and alice", got.Summary)
}

func TestPingPongDetector_两档升级(t *testing.T) {
	d := NewPingPongDetector(6)
	var got *core.Anomaly
	for i := 0; i < 48; i++ {
		if got != nil && got.Severity == core.SeverityHigh {
			break
		}
		got = d.Observe(msg(t, "alice", "bob"))
		if got == nil {
			got = d.Observe(msg(t, "bob", "alice"))
		}
	}
	assert.NotNil(t, got)
	assert.Equal(t, core.SeverityHigh, got.Severity, "连击 12 ≥ min_volleys*2 首次发射 HIGH")
}

func TestPingPongDetector_第三方重置(t *testing.T) {
	d := NewPingPongDetector(6)
	for i := 0; i < 5; i++ {
		d.Observe(msg(t, "alice", "bob"))
		d.Observe(msg(t, "bob", "alice"))
	}
	// 第三方介入打断
	d.Observe(msg(t, "carol", "alice"))
	var got *core.Anomaly
	for i := 0; i < 5; i++ {
		got = d.Observe(msg(t, "alice", "bob"))
		if got == nil {
			got = d.Observe(msg(t, "bob", "alice"))
		}
	}
	assert.Nil(t, got, "连击被第三方重置，仅 10 < 12 不到 HIGH，且 MEDIUM 已在重置前发过")
}

func TestPingPongDetector_非反转不延长(t *testing.T) {
	d := NewPingPongDetector(6)
	for i := 0; i < 6; i++ {
		d.Observe(msg(t, "alice", "bob"))
	}
	// 同方向连发：非反转，连击重置为 1
	got := d.Observe(msg(t, "alice", "bob"))
	assert.Nil(t, got, "连击重置后 1 < 6")
}

func TestPingPongDetector_非MESSAGE信号忽略(t *testing.T) {
	d := NewPingPongDetector(6)
	peer := "bob"
	assert.Nil(t, d.Observe(&core.Signal{Kind: core.SignalKindAfterToolCall, MemberName: "alice", PeerMember: &peer}))
	assert.Nil(t, d.Observe(&core.Signal{Kind: core.SignalKindMessage, MemberName: "alice"}), "无对端忽略")
}
