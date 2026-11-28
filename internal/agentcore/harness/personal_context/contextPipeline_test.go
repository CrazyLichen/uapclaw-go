package personal_context

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// 构造语义词向量：每页一个词，便于控制两两余弦。
func oneWordVectors(words ...string) map[string]map[string]float64 {
	out := map[string]map[string]float64{}
	for _, word := range words {
		out[word] = map[string]float64{"bm25:" + word: 1.0}
	}
	return out
}

func TestSemanticTokens_分词与停用词(t *testing.T) {
	tokens := SemanticTokens("The Install Guide")
	require.Equal(t, []string{"install", "guide"}, tokens)
	// CJK 段 + 2/3/4 元组
	tokens = SemanticTokens("知识库检索")
	require.Equal(t, []string{"知识库检索", "知识", "识库", "库检", "检索", "知识库", "识库检", "库检索", "知识库检", "识库检索"}, tokens)
	// 停用词过滤
	tokens = SemanticTokens("the 使用 资料")
	require.Empty(t, tokens)
	// 数字与符号词
	tokens = SemanticTokens("v1.2 go+dev")
	require.Equal(t, []string{"v1.2", "go+dev"}, tokens)
}

func TestSemanticFields_字段权重(t *testing.T) {
	fields := SemanticFields("安装", []string{"指南", "指南"}, "安装说明")
	require.InDelta(t, 5.0, fields["安装"], 1e-9) // title 4.0 + preview 1.0
	require.InDelta(t, 4.0, fields["指南"], 1e-9) // headings 两行各 2.0
}

func TestAdaptiveAcceptCut_边界(t *testing.T) {
	// 单值 → 下限
	require.InDelta(t, semanticAcceptFloor, AdaptiveAcceptCut([]float64{0.5, 0.5}), 1e-12)
	// 两簇分离：0.01 x3 与 0.2 x3 → 切点落在两者之间
	cut := AdaptiveAcceptCut([]float64{0.01, 0.01, 0.01, 0.2, 0.2, 0.2})
	require.Greater(t, cut, 0.01)
	require.Less(t, cut, 0.2)
	// 全体一致高分 → 超上限回落下限
	require.InDelta(t, semanticAcceptFloor, AdaptiveAcceptCut([]float64{0.9, 0.9}), 1e-12)
	// 全低分 → 下限
	require.InDelta(t, semanticAcceptFloor, AdaptiveAcceptCut([]float64{0.01, 0.02}), 1e-12)
	// 确定性：同输入同输出
	require.Equal(t, AdaptiveAcceptCut([]float64{0.01, 0.05, 0.2, 0.3}), AdaptiveAcceptCut([]float64{0.3, 0.2, 0.05, 0.01}))
}

func TestCapacityConstrainedClusters_确定性(t *testing.T) {
	vectors := oneWordVectors("alpha", "beta", "gamma", "delta", "安装", "部署", "升级", "运维")
	first := CapacityConstrainedClusters(vectors, 20, 20, nil, nil, false, 0, nil)
	second := CapacityConstrainedClusters(vectors, 20, 20, nil, nil, false, 0, nil)
	require.Equal(t, first, second, "同输入必须同输出")
	require.NotEmpty(t, first)
	// 全部成员恰好覆盖一次
	total := 0
	for _, cluster := range first {
		total += len(cluster)
	}
	require.Equal(t, len(vectors), total)
}

func TestCapacityConstrainedClusters_孤岛保留(t *testing.T) {
	// 全部页面两两相似度为 0（不同词）→ 无边 → 每页一个连通分量
	// 经分量递归后每页独立成簇（孤岛不被硬凑）
	vectors := oneWordVectors("a", "b", "c", "d")
	clusters := CapacityConstrainedClusters(vectors, 1, 1, nil, nil, false, 0, nil)
	require.Len(t, clusters, 4)
}

func TestCapacityConstrainedClusters_硬容量上限(t *testing.T) {
	// 8 个同词页面（两两余弦 1.0）+ max_members=3 → 不超 3
	vectors := oneWordVectors("w1", "w2", "w3", "w4", "w5", "w6", "w7", "w8")
	// 同词会互相归一？不同词余弦 0；改为同一向量：
	same := map[string]float64{"bm25:same": 1.0}
	vectors = map[string]map[string]float64{}
	for _, id := range []string{"p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8"} {
		vectors[id] = map[string]float64{"bm25:same": 1.0}
	}
	_ = same
	clusters := CapacityConstrainedClusters(vectors, 3, 3, nil, nil, false, 0, nil)
	for _, cluster := range clusters {
		require.LessOrEqual(t, len(cluster), 3)
	}
	total := 0
	for _, cluster := range clusters {
		total += len(cluster)
	}
	require.Equal(t, 8, total)
}

func TestCapacityConstrainedClusters_affinity保组(t *testing.T) {
	// 4 页两两相似 0（不同词），强制 affinity [a,b] 同簇
	vectors := oneWordVectors("a", "b", "c", "d")
	clusters := CapacityConstrainedClusters(vectors, 20, 20, nil, nil, false, 0, [][]string{{"a", "b"}})
	joined := false
	for _, cluster := range clusters {
		if len(cluster) == 2 && (cluster[0] == "a" || cluster[1] == "a") && cluster[0] != cluster[1] {
			joined = cluster[0] == "a" && cluster[1] == "b" || cluster[0] == "b" && cluster[1] == "a"
		}
	}
	require.True(t, joined, "affinity 组应保持同簇")
}

func TestSourceAwareScore_来源惩罚与加成(t *testing.T) {
	left := SourceDistribution{"provider\x00github": 1.0}
	right := SourceDistribution{"provider\x00github": 1.0}
	other := SourceDistribution{"provider\x00local": 1.0}
	// 同来源：加成 0.2
	require.InDelta(t, math.Min(1.0, 0.5+0.2*0.4), SourceAwareScore(0.5, left, right), 1e-9)
	// 跨来源：重叠 0
	require.InDelta(t, 0.5, SourceAwareScore(0.5, left, other), 1e-9)
	// 零语义分不加成
	require.InDelta(t, 0.0, SourceAwareScore(0.0, left, right), 1e-9)
}

func TestSparseSemanticScores_自分数归一(t *testing.T) {
	query := map[string]float64{"安装": 1.0}
	corpus := []map[string]float64{
		{"安装": 1.0, "指南": 0.5}, // 命中
		{"无关": 1.0},            // 未命中
	}
	scores := SparseSemanticScores(query, corpus)
	require.Len(t, scores, 2)
	require.Greater(t, scores[0], scores[1])
	require.Greater(t, scores[0], 0.9, "自身文档 BM25 归一后占主导")
	require.InDelta(t, 0.0, scores[1], 1e-9)
}

func TestClusteringSparseVectors_前缀与归一(t *testing.T) {
	records := map[string]SemanticRecord{
		"a": {Title: "安装指南", Preview: "pip install"},
		"b": {Title: "部署手册", Preview: "kubectl apply"},
	}
	vectors := ClusteringSparseVectors(records)
	for identifier, vector := range vectors {
		normSquared := 0.0
		for term, value := range vector {
			require.Contains(t, term, "bm25:")
			normSquared += value * value
		}
		if len(vector) > 0 {
			require.InDelta(t, 1.0, math.Sqrt(normSquared), 1e-9, identifier)
		}
	}
}

func TestCosineSimilarity_边界(t *testing.T) {
	cosine, ok := CosineSimilarity([]float64{1, 0}, []float64{0, 1})
	require.True(t, ok)
	require.InDelta(t, 0.0, cosine, 1e-9)
	_, ok = CosineSimilarity(nil, nil)
	require.False(t, ok)
	_, ok = CosineSimilarity([]float64{1}, []float64{1, 2})
	require.False(t, ok)
}

func TestValidatedEmbeddingVectors_校验(t *testing.T) {
	require.NotNil(t, ValidatedEmbeddingVectors([][]float64{{1, 0}, {0, 1}}, 2))
	require.Nil(t, ValidatedEmbeddingVectors([][]float64{{1, 0}}, 2))
	require.Nil(t, ValidatedEmbeddingVectors([][]float64{{1, 0}, {}}, 2))
	require.Nil(t, ValidatedEmbeddingVectors([][]float64{{1, 0}, {0}}, 2), "维度不一致")
	require.Nil(t, ValidatedEmbeddingVectors([][]float64{{math.Inf(1), 0}, {0, 1}}, 2))
}

func TestFusedSemanticScore_融合(t *testing.T) {
	require.InDelta(t, 0.5, FusedSemanticScore(0.5, 0, false), 1e-9)
	require.InDelta(t, 0.75*0.5+0.25*1.0, FusedSemanticScore(0.5, 1.0, true), 1e-9)
}

func TestSafePipelineSegment_分支(t *testing.T) {
	text, err := safePipelineSegment("svc-1", "service_id")
	require.NoError(t, err)
	require.Equal(t, "svc-1", text)
	_, err = safePipelineSegment("", "service_id")
	require.ErrorContains(t, err, "service_id must be a non-empty string")
	_, err = safePipelineSegment("../x", "run_id")
	require.ErrorContains(t, err, "unsafe run_id")
	_, err = safePipelineSegment("ok-id", "run_id")
	require.NoError(t, err)
}

func TestBaselineContextDirectories_祖先集合(t *testing.T) {
	dirs := BaselineContextDirectories([]string{"a/b/c.md", "x.md"})
	require.Contains(t, dirs, "a")
	require.Contains(t, dirs, "a/b")
	require.NotContains(t, dirs, "a/b/c.md")
	require.NotContains(t, dirs, "x.md")
}
