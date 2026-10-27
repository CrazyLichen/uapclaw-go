package common

import "slices"

// ──────────────────────────── 结构体 ────────────────────────────

// TripleBeam beam search 路径载体（GraphRetriever 的 TripleBeamSearch 消费）。
//
// 一条 beam = 候选三元组路径 + 累积分数；构造时一次性建立 text 去重集合，
// Contains 按 text O(1) 判重（对齐 Python __contains__ 查 _exist_triples）。
// 构造后事实不可变——防环靠新建 beam 而非原地修改（对齐 Python：展开恒
// beam.triples + [triple] 新建列表，零突变路径）。
//
// Python: openjiuwen/core/retrieval/common/triple_beam.py
type TripleBeam struct {
	// beam 路径节点（RetrievalResult 列表）
	beam []RetrievalResult
	// existTriples text 去重集合（构造时建立）
	existTriples map[string]struct{}
	// score 累积分数
	score float64
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTripleBeam 创建 beam 路径。existTriples 集合在构造时一次性建立
// （对齐 Python __init__ 的 {x.text for x in nodes}）。
func NewTripleBeam(nodes []RetrievalResult, score float64) *TripleBeam {
	existTriples := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		existTriples[node.Text] = struct{}{}
	}
	return &TripleBeam{
		beam:         nodes,
		existTriples: existTriples,
		score:        score,
	}
}

// Get 按索引取节点，负索引从尾部数（对齐 Python __getitem__；生产用
// Get(-1) 取尾）。越界经 slice 下标自然 panic（IndexError 对应物，
// 偏差归档 spec §8-5）。
func (b *TripleBeam) Get(idx int) RetrievalResult {
	if idx < 0 {
		idx += len(b.beam)
	}
	return b.beam[idx]
}

// Len 返回路径长度（对齐 __len__）。
func (b *TripleBeam) Len() int {
	return len(b.beam)
}

// Contains 判断节点是否已在 beam 中——按 text 判重而非全字段比较
// （对齐 __contains__：triple.text in self._exist_triples）。
func (b *TripleBeam) Contains(triple RetrievalResult) bool {
	_, ok := b.existTriples[triple.Text]
	return ok
}

// Triples 返回路径节点副本（浅拷贝密封：元素 Metadata map 仍共享，与
// Python 引用列表语义一致；返回新 slice 使消费方 append 不写穿内部
// 状态——偏差归档 spec §8-1）。
func (b *TripleBeam) Triples() []RetrievalResult {
	return slices.Clone(b.beam)
}

// Score 返回累积分数（对齐 score property）。
func (b *TripleBeam) Score() float64 {
	return b.score
}
