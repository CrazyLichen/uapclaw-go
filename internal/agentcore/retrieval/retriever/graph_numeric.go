// 数值工具：graph_retriever 的余弦打分与 topk 选择。
//
// Python: graph_retriever.py (_cosine_scores L52-63 / _topk L65-72)
package retriever

import (
	"sort"

	"gonum.org/v1/gonum/floats"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// cosineScores 计算查询向量与候选向量集的余弦相似度。
//
// 对齐 Python _cosine_scores：逐向量 L2 归一化（+1e-12 哨兵防零向量
// 除零）后点积。float32 截断不复刻（numpy BLAS 求和序 bit-exact 不可达，
// spec §8-3）。
func cosineScores(queryVec []float64, candVecs [][]float64) []float64 {
	scores := make([]float64, len(candVecs))
	qNorm := floats.Norm(queryVec, 2) + 1e-12
	for i, cand := range candVecs {
		cNorm := floats.Norm(cand, 2) + 1e-12
		dot := 0.0
		for j := range queryVec {
			if j < len(cand) {
				// Python 先除后乘（q/q_norm @ c/c_norm）的逐步语义
				dot += queryVec[j] / qNorm * (cand[j] / cNorm)
			}
		}
		scores[i] = dot
	}
	return scores
}

// topkIndices 返回分数最高的前 k 个（索引+分数），降序。
//
// 对齐 Python _topk：k=min(k,N)；k<=0 返回空。argpartition 的选择集+
// 降序语义用全排序实现（等值非稳定与 Python 同构，spec §8-4）。
func topkIndices(scores []float64, k int) ([]int, []float64) {
	k = min(k, len(scores))
	if k <= 0 {
		return nil, nil
	}
	idx := make([]int, len(scores))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return scores[idx[a]] > scores[idx[b]] })
	idx = idx[:k]
	out := make([]float64, k)
	for i, v := range idx {
		out[i] = scores[v]
	}
	return idx, out
}
