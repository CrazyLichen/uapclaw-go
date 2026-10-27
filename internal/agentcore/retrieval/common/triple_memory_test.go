package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNewTripleMemory 零值状态（M1）
func TestNewTripleMemory(t *testing.T) {
	m := NewTripleMemory()
	assert.Equal(t, 0, m.Len())
	assert.Equal(t, "", m.TriplesStr())
}

// TestTripleMemory_ExtendMemory首添 首条入列（M2）
func TestTripleMemory_ExtendMemory首添(t *testing.T) {
	m := NewTripleMemory()
	m.ExtendMemory(TripleFact{"Alice", "knows", "Bob"})
	assert.Equal(t, 1, m.Len())
	assert.Equal(t, []TripleFact{{"Alice", "knows", "Bob"}}, m.Memory())
}

// TestTripleMemory_同串去重 相同事实二次 Extend 不重复入列（M3）
func TestTripleMemory_同串去重(t *testing.T) {
	m := NewTripleMemory()
	m.ExtendMemory(TripleFact{"Alice", "knows", "Bob"})
	m.ExtendMemory(TripleFact{"Alice", "knows", "Bob"})
	assert.Equal(t, 1, m.Len())
}

// TestTripleMemory_大小写不同视为重复 去重键逐项 lower（M4，_tuple2str 语义钉死）
func TestTripleMemory_大小写不同视为重复(t *testing.T) {
	m := NewTripleMemory()
	m.ExtendMemory(TripleFact{"Alice", "Knows", "BOB"})
	m.ExtendMemory(TripleFact{"alice", "knows", "bob"})
	assert.Equal(t, 1, m.Len(), "去重键小写化后应视为同一事实")
	// 保留首见原貌
	assert.Equal(t, []TripleFact{{"Alice", "Knows", "BOB"}}, m.Memory())
}

// TestTripleMemory_BatchExtendMemory含重复 批量 [x,y,x] → [x,y]（M5）
func TestTripleMemory_BatchExtendMemory含重复(t *testing.T) {
	m := NewTripleMemory()
	m.BatchExtendMemory([]TripleFact{
		{"A", "r1", "B"},
		{"B", "r2", "C"},
		{"A", "r1", "B"},
	})
	assert.Equal(t, 2, m.Len())
	assert.Equal(t, []TripleFact{{"A", "r1", "B"}, {"B", "r2", "C"}}, m.Memory())
}

// TestTripleMemory_TriplesStr格式 单条 "(s p o)"，多条换行连接（M6）
func TestTripleMemory_TriplesStr格式(t *testing.T) {
	m := NewTripleMemory()
	m.ExtendMemory(TripleFact{"Alice", "knows", "Bob"})
	assert.Equal(t, "(Alice knows Bob)", m.TriplesStr())

	m2 := NewTripleMemory()
	m2.BatchExtendMemory([]TripleFact{
		{"A", "r1", "B"},
		{"B", "r2", "C"},
	})
	assert.Equal(t, "(A r1 B)\n(B r2 C)", m2.TriplesStr())
}

// TestTripleMemory_TriplesStr保留原大小写 展示串不 lower（M7，与去重键两套逻辑钉死）
func TestTripleMemory_TriplesStr保留原大小写(t *testing.T) {
	m := NewTripleMemory()
	m.ExtendMemory(TripleFact{"Foo", "Is", "Bar"})
	assert.Equal(t, "(Foo Is Bar)", m.TriplesStr())
}

// TestTripleMemory_Memory密封 对返回值 append 不写穿内部（M8，C2 同链）
// 注：简单 append 形态已是本类型的诚实上限——13.25 消费方只读传参
// （memory.memory → _link_passages），无"从 Memory() 返回值 append 构造
// 新 memory"场景；且 ExtendMemory 的 append 恒在 len 尾部追加，超 len
// 的 backing 污染（若有）必被后续 ExtendMemory 覆盖。B8 的跨对象形态
// 对应 13.23 真实展开（NewTripleBeam(append(b1.Triples(), t), s)），故
// B8 必要、本测试不升级。
func TestTripleMemory_Memory密封(t *testing.T) {
	m := NewTripleMemory()
	m.ExtendMemory(TripleFact{"A", "r1", "B"})
	got := m.Memory()
	// 显式丢弃 append 返回值——本用例只关心对返回 slice 追加不写穿内部状态
	_ = append(got, TripleFact{"X", "y", "Z"})
	assert.Equal(t, 1, m.Len(), "对返回值 append 后内部长度不应变化")
	assert.Equal(t, []TripleFact{{"A", "r1", "B"}}, m.Memory())
}

// TestTripleMemory_Len 随去重累积计数（M9）
func TestTripleMemory_Len(t *testing.T) {
	m := NewTripleMemory()
	assert.Equal(t, 0, m.Len())
	m.BatchExtendMemory([]TripleFact{{"A", "r", "B"}, {"C", "r", "D"}})
	assert.Equal(t, 2, m.Len())
}
