// Package reliability 数据层单元测试。
//
// 对应 Python：tests 于上游各模块内联；此处按 Go 测试函数规范组织。
package reliability

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSeverityRank_秩序表(t *testing.T) {
	assert.Equal(t, 0, SeverityLow.Rank())
	assert.Equal(t, 1, SeverityMedium.Rank())
	assert.Equal(t, 2, SeverityHigh.Rank())
	assert.Equal(t, 3, SeverityCritical.Rank())
	assert.Equal(t, -1, Severity("bogus").Rank())
}

func TestAnomalyKind_全量取值(t *testing.T) {
	// Python anomaly.py AnomalyKind 10 成员全量映射，缺一即漂移
	kinds := []AnomalyKind{
		AnomalyKindToolErrorRate, AnomalyKindRepeatToolCall, AnomalyKindToolCallLoop,
		AnomalyKindModelError, AnomalyKindModelUnavailable, AnomalyKindModelRouteNotice,
		AnomalyKindOutputTooLong, AnomalyKindThinkingTooLong, AnomalyKindFrequentCompaction,
		AnomalyKindPingPong,
	}
	assert.Len(t, kinds, 10)
}

func TestSignalKind_全量取值(t *testing.T) {
	// Python signals.py SignalKind 7 成员全量映射
	kinds := []SignalKind{
		SignalKindBeforeToolCall, SignalKindAfterToolCall, SignalKindToolException,
		SignalKindModelException, SignalKindAfterModelCall, SignalKindBeforeModelCall,
		SignalKindMessage,
	}
	assert.Len(t, kinds, 7)
}

func TestSlidingWindowCounter_窗口驱逐(t *testing.T) {
	w := NewSlidingWindowCounter(10)
	w.Add(1)
	w.Add(5)
	// cutoff = ts - window；事件严格小于 cutoff 才驱逐（对齐 events[0] < cutoff）
	assert.Equal(t, 1, w.Count(14), "cutoff=4：ts=1 < 4 被驱逐，ts=5 保留")
	assert.Equal(t, 2, w.Count(13), "cutoff=3：两个都保留")
	assert.Equal(t, 0, w.Count(16), "cutoff=6：ts=5 也被驱逐")
	assert.Equal(t, 1, w.Add(16), "add 后窗口内仅 [16]")
}

func TestSlidingWindowCounter_Reset(t *testing.T) {
	w := NewSlidingWindowCounter(10)
	w.Add(1)
	w.Add(2)
	w.Reset()
	assert.Equal(t, 0, w.Count(2))
}

func TestStableCallHash_参数顺序无关(t *testing.T) {
	a := StableCallHash("read_file", map[string]any{"a": 1, "b": map[string]any{"x": 2, "y": 1}})
	b := StableCallHash("read_file", map[string]any{"b": map[string]any{"y": 1, "x": 2}, "a": 1})
	assert.Equal(t, a, b, "嵌套 map 键递归排序")
	assert.NotEqual(t, a, StableCallHash("read_file", map[string]any{"a": 1, "b": 2}))
	assert.NotEqual(t, a, StableCallHash("write_file", map[string]any{"a": 1, "b": map[string]any{"x": 2, "y": 1}}))
}

func TestStableCallHash_NilArgs(t *testing.T) {
	// Python: tool_args or {} — nil 参数与空 map 同哈希
	assert.Equal(t, StableCallHash("t", nil), StableCallHash("t", map[string]any{}))
}

func TestStableResultHash_分支(t *testing.T) {
	assert.Equal(t, "none", StableResultHash(nil))
	assert.Equal(t, StableResultHash("abc"), StableResultHash("abc"))
	assert.NotEqual(t, StableResultHash("abc"), StableResultHash("abd"))
	// 非 string 非 nil 走 json 序列化
	m1 := map[string]any{"a": 1, "b": 2}
	m2 := map[string]any{"b": 2, "a": 1}
	assert.Equal(t, StableResultHash(m1), StableResultHash(m2), "map 键排序后同哈希")
	// 不可序列化值兜底不 panic（对齐 default=str）
	type unserializable struct {
		Fn func()
	}
	assert.NotPanics(t, func() { StableResultHash(unserializable{Fn: func() {}}) })
}
