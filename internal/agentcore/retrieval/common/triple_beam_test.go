package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// makeBeamNodes 构造测试用 beam 节点（3 节点典型路径）。
func makeBeamNodes() []RetrievalResult {
	return []RetrievalResult{
		{Text: "entity1 relation entity2", Score: 0.9, Metadata: map[string]any{"triple": `["entity1","relation","entity2"]`}},
		{Text: "entity2 relation2 entity3", Score: 0.8, Metadata: map[string]any{"triple": `["entity2","relation2","entity3"]`}},
		{Text: "entity3 relation3 entity4", Score: 0.7, Metadata: map[string]any{"triple": `["entity3","relation3","entity4"]`}},
	}
}

// TestNewTripleBeam 构造后 Len/Score/元素一致（B1）
func TestNewTripleBeam(t *testing.T) {
	nodes := makeBeamNodes()
	b := NewTripleBeam(nodes, 0.85)
	assert.Equal(t, 3, b.Len())
	assert.Equal(t, 0.85, b.Score())
	assert.Equal(t, nodes[0], b.Get(0))
	assert.Equal(t, nodes[2], b.Get(2))
}

// TestTripleBeam_Get正索引 正索引取节点（B2）
func TestTripleBeam_Get正索引(t *testing.T) {
	b := NewTripleBeam(makeBeamNodes(), 0.5)
	assert.Equal(t, "entity1 relation entity2", b.Get(0).Text)
	assert.Equal(t, "entity3 relation3 entity4", b.Get(2).Text)
}

// TestTripleBeam_Get负索引 负索引从尾部数，Get(-1) 取尾（B3，生产形态对应物）
func TestTripleBeam_Get负索引(t *testing.T) {
	b := NewTripleBeam(makeBeamNodes(), 0.5)
	assert.Equal(t, "entity3 relation3 entity4", b.Get(-1).Text)
	assert.Equal(t, "entity2 relation2 entity3", b.Get(-2).Text)
}

// TestTripleBeam_Get越界panic 越界自然 panic（B4，IndexError 对应物）
func TestTripleBeam_Get越界panic(t *testing.T) {
	b := NewTripleBeam(makeBeamNodes(), 0.5)
	assert.Panics(t, func() { b.Get(3) })
	assert.Panics(t, func() { b.Get(-4) })
}

// TestTripleBeam_Len 长度（B5）
func TestTripleBeam_Len(t *testing.T) {
	assert.Equal(t, 0, NewTripleBeam(nil, 0.5).Len())
	assert.Equal(t, 3, NewTripleBeam(makeBeamNodes(), 0.5).Len())
}

// TestTripleBeam_Contains按text判重 同 text 异 score/metadata 视为已存在（B6，钉"按 text 非全字段"）
func TestTripleBeam_Contains按text判重(t *testing.T) {
	b := NewTripleBeam(makeBeamNodes(), 0.5)
	sameTextDiffRest := RetrievalResult{
		Text:     "entity1 relation entity2",
		Score:    0.123, // 与节点 0 同 text 异 score
		Metadata: nil,   // 异 metadata
	}
	assert.True(t, b.Contains(sameTextDiffRest))
}

// TestTripleBeam_Contains不存在 text 不同则不存在（B7）
func TestTripleBeam_Contains不存在(t *testing.T) {
	b := NewTripleBeam(makeBeamNodes(), 0.5)
	assert.False(t, b.Contains(RetrievalResult{Text: "absent text", Score: 0.9}))
}

// TestTripleBeam_Triples密封 跨对象写穿防护（B8，C2 决策钉死）
//
// 真实写穿场景（13.23 展开形态）：NewTripleBeam(append(b1.Triples(), t), s)
// 若 Triples 返回内部引用且 backing 有空闲容量，两 beam 共享数组——
// b1 二次展开的 append 写穿 b2 的第三节点。C2 每次返回独立分配，
// 跨调用写穿不可能。注：仅"append 后查本对象视图不变"不足以判别
// C1/C2（slice len 固定，append 只写 backing[len] 越界位，视图不变）。
func TestTripleBeam_Triples密封(t *testing.T) {
	nodes := makeBeamNodes()
	// 构造 cap > len 的内部切片（append 链形态，13.23 展开的真实输入）
	base := make([]RetrievalResult, 0, 3)
	base = append(base, nodes[0], nodes[1]) // len 2, cap 3
	b1 := NewTripleBeam(base, 0.5)

	// 13.23 展开形态：append 构造扩展路径 → 新 beam
	extended := append(b1.Triples(), nodes[2])
	b2 := NewTripleBeam(extended, 0.6)
	assert.Equal(t, 3, b2.Len())

	// b1 二次展开不同节点：C1 下写穿共享 backing[2] 污染 b2；
	// C2 下各自独立分配互不影响
	sneaky := append(b1.Triples(), RetrievalResult{Text: "sneaky", Score: 1.0})
	assert.Equal(t, 3, len(sneaky))
	assert.Equal(t, "entity3 relation3 entity4", b2.Get(2).Text,
		"b1 二次展开不应写穿 b2 内部状态")
}

// TestTripleBeam_Triples元素一致 逐元素等于原构造节点（B9）
func TestTripleBeam_Triples元素一致(t *testing.T) {
	nodes := makeBeamNodes()
	got := NewTripleBeam(nodes, 0.5).Triples()
	assert.Equal(t, nodes, got)
}

// TestTripleBeam_Score 分数透传（B10）
func TestTripleBeam_Score(t *testing.T) {
	assert.Equal(t, 0.42, NewTripleBeam(nil, 0.42).Score())
}
