package utils

import (
	"sort"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RRFFusion 倒数秩融合（Reciprocal Rank Fusion）。
//
// 将多路检索结果融合为单一排序结果。
// 对齐 Python retrieval/utils/fusion.py (rrf_fusion)。
//   - 用 text 作为唯一键做去重
//   - RRF 分数 = Σ 1/(k + rank)
//   - 保留首次出现的 metadata
//   - 按分数降序排列
//
// Python:
//
//	for rank, result in enumerate(results, start=1):
//	    score_dict[key] += 1.0 / (k + rank)
//	    if key not in result_dict: result_dict[key] = result
func RRFFusion(resultsList [][]common.RetrievalResult, k int) []common.RetrievalResult {
	if len(resultsList) == 0 {
		return nil
	}

	scoreDict := make(map[string]float64)
	resultDict := make(map[string]*common.RetrievalResult)

	// 对齐 Python: for results in results_list:
	//   for rank, result in enumerate(results, start=1):
	for _, results := range resultsList {
		for rank, result := range results {
			// 对齐 Python: key = result.text
			key := result.Text
			// 对齐 Python: score_dict[key] += 1.0 / (k + rank)
			// Python enumerate(start=1)，Go rank 从 0 开始，所以用 rank+1
			scoreDict[key] += 1.0 / float64(k+rank+1)
			// 对齐 Python: if key not in result_dict: result_dict[key] = result
			if _, exists := resultDict[key]; !exists {
				resultDict[key] = &result
			}
		}
	}

	// 对齐 Python: sorted_items = sorted(score_dict.items(), key=lambda x: x[1], reverse=True)
	type kv struct {
		key   string
		score float64
	}
	var sorted []kv
	for key, score := range scoreDict {
		sorted = append(sorted, kv{key: key, score: score})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].score > sorted[j].score
	})

	// 对齐 Python: fused_results = []
	//   for key, score in sorted_items:
	//     result = result_dict[key]; result.score = score; fused_results.append(result)
	fusedResults := make([]common.RetrievalResult, 0, len(sorted))
	for _, item := range sorted {
		result := *resultDict[item.key]
		result.Score = item.score
		fusedResults = append(fusedResults, result)
	}
	return fusedResults
}

// ──────────────────────────── 非导出函数 ────────────────────────────
