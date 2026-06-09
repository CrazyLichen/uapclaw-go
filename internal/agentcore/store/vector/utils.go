package vector

import "math"

// ──────────────────────────── 导出函数 ────────────────────────────

// ConvertL2Squared 将 L2 平方距离转换为归一化相似度 [0, 1]。
// 公式: max(0, (maxDist - rawScore) / maxDist)
// 默认 maxDist=4.0（单位向量假设下 L2 平方距离上限）。
//
// 对应 Python: vector/utils.py (convert_l2_squared)
func ConvertL2Squared(rawScore, maxDist float64) float64 {
	return math.Max(0, (maxDist-rawScore)/maxDist)
}

// ConvertCosineSimilarity 将余弦相似度 [-1, 1] 转换为归一化相似度 [0, 1]。
// 公式: (rawScore + 1) / 2
//
// 对应 Python: vector/utils.py (convert_cosine_similarity)
func ConvertCosineSimilarity(rawScore float64) float64 {
	return (rawScore + 1.0) / 2.0
}

// ConvertCosineDistance 将余弦距离 [0, 2] 转换为归一化余弦相似度 [0, 1]。
// 公式: (2 - rawScore) / 2
// Chroma 使用余弦距离。
//
// 对应 Python: vector/utils.py (convert_cosine_distance)
func ConvertCosineDistance(rawScore float64) float64 {
	return (2.0 - rawScore) / 2.0
}

// ConvertIPSimilarity 将原始内积转换为归一化相似度 [0, 1]。
// 公式: clamp((rawScore + 1) / 2, 0, 1)
// Milvus 使用内积。
//
// 对应 Python: vector/utils.py (convert_ip_similarity)
func ConvertIPSimilarity(rawScore float64) float64 {
	return math.Max(0, math.Min(1, (rawScore+1.0)/2.0))
}

// ConvertIPDistance 将 Chroma 的内积距离 [0, 2] 转换为归一化相似度 [0, 1]。
// 公式: clamp((2 - rawScore) / 2, 0, 1)
//
// 对应 Python: vector/utils.py (convert_ip_distance)
func ConvertIPDistance(rawScore float64) float64 {
	return math.Max(0, math.Min(1, (2.0-rawScore)/2.0))
}
