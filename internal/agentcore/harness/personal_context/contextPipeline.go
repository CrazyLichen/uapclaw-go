package personal_context

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// 单进程采集处理与 Context 发布流水线（第一块：常量 + 纯算法层）。
// Python: openjiuwen/harness/personal_context/context_pipeline.py（一比一移植）。
// 本文件按 Python 单文件一比一原则承载全部 pipeline 符号；后续块（9b-9f）追加于本文件。

// ──────────────────────────── 常量 ────────────────────────────

// 语义相关度绝对下限（F_05 实测标定：页 vs 自目录 ≥~0.11，跨目录 ≤~0.045）。
// Python: context_pipeline.py:155 _SEMANTIC_ACCEPT_FLOOR
const semanticAcceptFloor = 0.06

// 自适应切点上限：超过即整簇同质，回落到下限。
// Python: context_pipeline.py:158 _CLUSTER_SCORE_CEILING
const clusterScoreCeiling = 0.35

// 目录路由的分数优势边距。
// Python: context_pipeline.py:159 _DIRECTORY_MARGIN
const directoryMargin = 0.10

// 稀疏短名单长度。
// Python: context_pipeline.py:160 _SPARSE_SHORTLIST_LIMIT
const sparseShortlistLimit = 8

// BM25 参数。Python: context_pipeline.py:145-146
const (
	bm25K1p = 1.2
	bm25Bp  = 0.75
)

// 单块字符上限。Python: context_pipeline.py:109 _MAX_BLOCK_CHARS
const maxBlockChars = 4_000

// balanced 档分组与并发。Python: context_pipeline.py:121/123
const (
	balancedGroupSize        = 5
	balancedModelConcurrency = 6
)

// profile 档位排序。Python: context_pipeline.py:166 _PROFILE_RANK
var profileRank = map[string]int{"rules": 0, "balanced": 1, "agent": 2}

// 保留段集合。Python: context_pipeline.py:167 _RESERVED_CONTEXT_SEGMENTS
var reservedContextSegments = map[string]struct{}{"sources": {}, "topics": {}, "待整理": {}}

// 通用主题名集合。Python: context_pipeline.py:168 _GENERIC_TOPIC_NAMES
var genericTopicNames = map[string]struct{}{
	"context": {}, "docs": {}, "documents": {}, "information": {}, "notes": {},
	"sources": {}, "topics": {}, "内容": {}, "文档": {}, "文档资料": {}, "材料": {},
	"知识": {}, "资料": {}, "资料文档": {}, "主题": {},
}

// 语义停用词。Python: context_pipeline.py:187 _SEMANTIC_STOP_TERMS
var semanticStopTerms = map[string]struct{}{
	"and": {}, "for": {}, "from": {}, "the": {}, "with": {},
	"一个": {}, "以及": {}, "使用": {}, "信息": {}, "内容": {},
	"文档": {}, "相关": {}, "进行": {}, "资料": {},
}

// provider 展示名。Python: context_pipeline.py:199 _PROVIDER_DISPLAY_NAMES
var providerDisplayNames = map[string]string{
	"feishu": "飞书", "local_files": "本地文件", "browser_bookmarks": "浏览器书签",
	"zhihu_reader": "知乎", "toutiao_reader": "今日头条", "rss_feed": "RSS订阅",
	"github": "GitHub", "gitcode": "GitCode", "local": "本地来源",
}

// 语义分词边界/裁剪字符集。Python: context_pipeline.py:140-141
const (
	semanticBoundaries = " \t-_,，。；;：:!?！？、()（）[]【】<>《》"
	semanticTrim       = " .-_,，。；;：:!?！？、()（）[]【】<>《》"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// safeSegmentPattern 安全段正则。Python: context_pipeline.py:294 使用 models._SAFE_SEGMENT
var safeSegmentPatternPipeline = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// asciiSemanticTokenPattern ascii 语义词。
// Python: context_pipeline.py:306 内联 re.findall(r"[a-z][a-z0-9_.+#-]{1,63}|[0-9]+")
var asciiSemanticTokenPattern = regexp.MustCompile(`[a-z][a-z0-9_.+#-]{1,63}|[0-9]+`)

// cjkRunPattern CJK 连续段。
// Python: context_pipeline.py:308 内联 r"[\u3400-\u9fff]{2,32}"
var cjkRunPattern = regexp.MustCompile(`[\x{3400}-\x{9fff}]{2,32}`)

// cjkCharPattern CJK 字符判定（shared_phrase 权重分母）。
// Python: context_pipeline.py:910 内联 re.search(r"[\u3400-\u9fff]", term)
var cjkCharPattern = regexp.MustCompile(`[\x{3400}-\x{9fff}]`)

// conventionalCommitPattern 约定式提交标题。
// Python: context_pipeline.py:142 _CONVENTIONAL_COMMIT
var conventionalCommitPattern = regexp.MustCompile(
	`(?i)^(?:fix|feat|docs|refactor|test|chore|perf|build|ci|style|revert)(?:\(([^()]{1,80})\))?!?:\s*(.+)$`)

// ──────────────────────────── 结构体 ────────────────────────────

// SemanticRecord 语义记录三元组（title, headings, preview）。
// Python: context_pipeline.py 各处 tuple[str, Sequence[str], str]。
type SemanticRecord struct {
	// Title 标题
	Title string
	// Headings 标题列表
	Headings []string
	// Preview 正文预览
	Preview string
}

// SourceDistribution (字段, 值) → 权重 的来源分布。
// Python: context_pipeline.py:478 _SourceDistribution = Mapping[tuple[str,str], float]。
type SourceDistribution map[string]float64

// SemanticEmbedder 语义文本嵌入函数缝（embedding 失败由实现返回错误）。
// Python: context_pipeline.py 内 _SemanticEmbedder 协议（await embed_texts(texts)）。
type SemanticEmbedder func(texts []string) ([][]float64, error)

// ──────────────────────────── 导出函数 ────────────────────────────

// BaselineContextDirectories 相对路径集合的全部祖先目录。
// Python: context_pipeline.py:979 _baseline_context_directories
func BaselineContextDirectories(relativePaths []string) map[string]struct{} {
	directories := map[string]struct{}{}
	for _, relative := range relativePaths {
		parts := strings.Split(relative, "/")
		for depth := 1; depth < len(parts); depth++ {
			directories[strings.Join(parts[:depth], "/")] = struct{}{}
		}
	}
	return directories
}

// CosineSimilarity 稠密向量余弦（非法输入返回 false 对齐 Python None）。
// Python: context_pipeline.py:915 _cosine_similarity
func CosineSimilarity(left []float64, right []float64) (float64, bool) {
	if len(left) == 0 || len(left) != len(right) {
		return 0, false
	}
	leftNormSquared := 0.0
	rightNormSquared := 0.0
	for index := range left {
		leftNormSquared += left[index] * left[index]
		rightNormSquared += right[index] * right[index]
	}
	if math.IsInf(leftNormSquared, 0) || math.IsNaN(leftNormSquared) || leftNormSquared <= 0.0 {
		return 0, false
	}
	if math.IsInf(rightNormSquared, 0) || math.IsNaN(rightNormSquared) || rightNormSquared <= 0.0 {
		return 0, false
	}
	dot := 0.0
	for index := range left {
		dot += left[index] * right[index]
	}
	cosine := dot / (math.Sqrt(leftNormSquared) * math.Sqrt(rightNormSquared))
	if math.IsInf(cosine, 0) || math.IsNaN(cosine) {
		return 0, false
	}
	return cosine, true
}

// FusedSemanticScore 稀疏分与稠密余弦的融合分。
// Python: context_pipeline.py:930 _fused_semantic_score
func FusedSemanticScore(sparse float64, cosine float64, hasCosine bool) float64 {
	if !hasCosine {
		return sparse
	}
	normalizedCosine := math.Max(0.0, math.Min(1.0, (cosine+1.0)/2.0))
	return 0.75*sparse + 0.25*normalizedCosine
}

// ValidatedEmbeddingVectors 校验嵌入响应形状与数值有效性。
// Python: context_pipeline.py:890 _validated_embedding_vectors
func ValidatedEmbeddingVectors(value [][]float64, expectedCount int) [][]float64 {
	if len(value) != expectedCount {
		return nil
	}
	vectors := make([][]float64, 0, len(value))
	dimension := -1
	for _, rawVector := range value {
		if len(rawVector) == 0 {
			return nil
		}
		vector := make([]float64, 0, len(rawVector))
		for _, number := range rawVector {
			if math.IsInf(number, 0) || math.IsNaN(number) {
				return nil
			}
			vector = append(vector, number)
		}
		if dimension == -1 {
			dimension = len(vector)
		}
		normSquared := 0.0
		for _, number := range vector {
			normSquared += number * number
		}
		if len(vector) != dimension || math.IsInf(normSquared, 0) || math.IsNaN(normSquared) || normSquared <= 0.0 {
			return nil
		}
		vectors = append(vectors, vector)
	}
	return vectors
}

// SemanticEmbeddingText 组装嵌入输入文本（NFKC + 截断）。
// Python: context_pipeline.py:884 _semantic_embedding_text
func SemanticEmbeddingText(title string, headings []string, preview string) string {
	parts := append([]string{title}, headings...)
	parts = append(parts, preview)
	text := norm.NFKC.String(strings.Join(parts, "\n"))
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) > maxBlockChars {
		text = string(runes[:maxBlockChars])
	}
	if text == "" {
		return "（无可用语义文本）"
	}
	return text
}

// SourceAwareScore 语义分 + 来源重叠加成（仅正分加成）。
// Python: context_pipeline.py:507 _source_aware_score
func SourceAwareScore(semantic float64, left SourceDistribution, right SourceDistribution) float64 {
	if semantic <= 0.0 {
		return 0.0
	}
	return math.Min(1.0, semantic+0.2*SourceOverlap(left, right))
}

// SourceOverlap 来源分布加权最小重叠。
// Python: context_pipeline.py:496 _source_overlap
func SourceOverlap(left SourceDistribution, right SourceDistribution) float64 {
	weights := map[string]float64{"provider": 0.4, "source_type": 0.4, "service": 0.2}
	overlap := 0.0
	for key, value := range left {
		field := strings.SplitN(key, "\x00", 2)[0]
		weight, exists := weights[field]
		if !exists {
			panic(fmt.Sprintf("unknown source field: %s", field))
		}
		overlap += weight * math.Min(value, right[key])
	}
	return overlap
}

// MeanSourceDistribution 多个来源分布的均值。
// Python: context_pipeline.py:488 _mean_source_distribution
func MeanSourceDistribution(values []SourceDistribution) SourceDistribution {
	result := SourceDistribution{}
	for _, value := range values {
		for key, weight := range value {
			result[key] += weight / float64(len(values))
		}
	}
	return result
}

// SourceDistributionOf 元数据列表的来源分布。
// Python: context_pipeline.py:477 _source_distribution
func SourceDistributionOf(metadata []map[string]any) SourceDistribution {
	result := SourceDistribution{}
	for _, item := range metadata {
		for _, field := range []string{"provider", "source_type", "service"} {
			value, ok := item[field].(string)
			if ok && strings.TrimSpace(value) != "" {
				key := field + "\x00" + strings.TrimSpace(value)
				result[key] += 1.0 / float64(len(metadata))
			}
		}
	}
	return result
}

// SparseVectorCosine 稀疏向量余弦（截断到 [0,1]）。
// Python: context_pipeline.py:438 _sparse_vector_cosine
func SparseVectorCosine(left map[string]float64, right map[string]float64) float64 {
	if len(left) > len(right) {
		left, right = right, left
	}
	dot := 0.0
	for term, value := range left {
		dot += value * right[term]
	}
	return math.Max(0.0, math.Min(1.0, dot))
}

// AdaptiveAcceptCut 从成对分数总体导出聚类边切点（F_05）。
// Python: context_pipeline.py:374 _adaptive_accept_cut —— 一维最优二分
// （最大类间方差）排序单趟扫描 + 低侧递归下探，全部确定性。
func AdaptiveAcceptCut(scores []float64) float64 {
	counts := map[float64]int{}
	for _, score := range scores {
		key := math.Round(score*1e12) / 1e12
		counts[key]++
	}
	values := make([]float64, 0, len(counts))
	for value := range counts {
		values = append(values, value)
	}
	sort.Float64s(values)
	if len(values) < 2 {
		return semanticAcceptFloor
	}
	cut, lowValues := bestSplit(values, counts)
	if cut < semanticAcceptFloor || cut > clusterScoreCeiling {
		return semanticAcceptFloor
	}
	for len(lowValues) >= 2 {
		subCut, subLow := bestSplit(lowValues, counts)
		if subCut < semanticAcceptFloor {
			return semanticAcceptFloor
		}
		if subCut > clusterScoreCeiling {
			return cut
		}
		cut = subCut
		lowValues = subLow
	}
	return cut
}

// CapacityConstrainedClusters 容量约束的确定性语义聚类。
// Python: context_pipeline.py:511 _capacity_constrained_clusters。
// denseVectorsByID 允许 nil；acceptScore 用 hasAccept 区分 Python None；
// affinityGroups 允许 nil。
func CapacityConstrainedClusters(
	vectorsByID map[string]map[string]float64,
	maxMembers int,
	targetMembers int,
	denseVectorsByID map[string][]float64,
	sourceDistributionsByID map[string]SourceDistribution,
	hasAccept bool,
	acceptScore float64,
	affinityGroups [][]string,
) [][]string {
	if maxMembers < 1 || targetMembers < 1 {
		panic("cluster capacities must be positive")
	}
	identifiers := sortedIdentifiers(vectorsByID)
	if len(identifiers) == 0 {
		return [][]string{}
	}
	sparse := map[string]map[string]float64{}
	for _, identifier := range identifiers {
		copyVector := map[string]float64{}
		for term, value := range vectorsByID[identifier] {
			copyVector[term] = value
		}
		sparse[identifier] = copyVector
	}
	dense := normalizedDenseVectors(denseVectorsByID, identifiers)
	sources := sourceDistributionsByID

	similarityVectors := func(leftID string, rightID string) float64 {
		cosine := 0.0
		hasCosine := false
		if dense != nil {
			leftDense, leftOK := dense[leftID]
			rightDense, rightOK := dense[rightID]
			if leftOK && rightOK {
				cosine, hasCosine = CosineSimilarity(leftDense, rightDense)
			}
		}
		return SourceAwareScore(
			FusedSemanticScore(SparseVectorCosine(sparse[leftID], sparse[rightID]), cosine, hasCosine),
			sources[leftID], sources[rightID])
	}

	// 成对分数一次算齐：无强制切点时按总体分布自适应导出（F_05），矩阵复用于连通分量。
	pairScores := map[string]map[string]float64{}
	for _, identifier := range identifiers {
		pairScores[identifier] = map[string]float64{}
	}
	for position, leftID := range identifiers {
		for _, rightID := range identifiers[position+1:] {
			score := similarityVectors(leftID, rightID)
			pairScores[leftID][rightID] = score
			pairScores[rightID][leftID] = score
		}
	}
	threshold := acceptScore
	if !hasAccept {
		allScores := make([]float64, 0, len(identifiers)*(len(identifiers)-1)/2)
		for _, leftID := range identifiers {
			for _, score := range pairScores[leftID] {
				allScores = append(allScores, score)
			}
		}
		threshold = AdaptiveAcceptCut(allScores)
	}

	// 语义孤岛保序：每个连通分量独立聚类，不用无关页硬凑容量。
	affinityAdjacency := map[string]map[string]struct{}{}
	var filteredAffinityGroups [][]string
	if affinityGroups != nil {
		known := map[string]struct{}{}
		for _, identifier := range identifiers {
			known[identifier] = struct{}{}
		}
		for _, group := range affinityGroups {
			var members []string
			for _, member := range group {
				if _, ok := known[member]; ok {
					members = append(members, member)
				}
			}
			sort.Slice(members, func(i, j int) bool {
				return identifierLess(members[i], members[j])
			})
			if len(members) < 2 {
				continue
			}
			filteredAffinityGroups = append(filteredAffinityGroups, members)
			for _, leftID := range members {
				if affinityAdjacency[leftID] == nil {
					affinityAdjacency[leftID] = map[string]struct{}{}
				}
				for _, member := range members {
					if member != leftID {
						affinityAdjacency[leftID][member] = struct{}{}
					}
				}
			}
		}
	}

	connected := func(componentCurrent string, candidate string) bool {
		if _, linked := affinityAdjacency[componentCurrent][candidate]; linked {
			return true
		}
		return pairScores[componentCurrent][candidate] >= threshold
	}

	unseen := map[string]struct{}{}
	for _, identifier := range identifiers {
		unseen[identifier] = struct{}{}
	}
	var components [][]string
	for len(unseen) > 0 {
		seed := minIdentifier(unseen)
		componentMembers := []string{seed}
		delete(unseen, seed)
		frontier := []string{seed}
		for len(frontier) > 0 {
			componentCurrent := frontier[len(frontier)-1]
			frontier = frontier[:len(frontier)-1]
			var connectedCandidates []string
			for _, matchedIdentifier := range sortedIdentifiers(unseen) {
				if !connected(componentCurrent, matchedIdentifier) {
					continue
				}
				connectedCandidates = append(connectedCandidates, matchedIdentifier)
			}
			for _, identifier := range connectedCandidates {
				delete(unseen, identifier)
				componentMembers = append(componentMembers, identifier)
				frontier = append(frontier, identifier)
			}
		}
		sort.Slice(componentMembers, func(i, j int) bool {
			return identifierLess(componentMembers[i], componentMembers[j])
		})
		components = append(components, componentMembers)
	}
	if len(components) > 1 {
		var clustered [][]string
		for _, componentIDs := range components {
			subVectors := map[string]map[string]float64{}
			for _, identifier := range componentIDs {
				subVectors[identifier] = sparse[identifier]
			}
			var subDense map[string][]float64
			if dense != nil {
				subDense = map[string][]float64{}
				for _, identifier := range componentIDs {
					subDense[identifier] = dense[identifier]
				}
			}
			componentIDSet := map[string]struct{}{}
			for _, identifier := range componentIDs {
				componentIDSet[identifier] = struct{}{}
			}
			var componentAffinity [][]string
			for _, group := range filteredAffinityGroups {
				var filtered []string
				for _, member := range group {
					if _, ok := componentIDSet[member]; ok {
						filtered = append(filtered, member)
					}
				}
				if len(filtered) > 0 {
					componentAffinity = append(componentAffinity, filtered)
				}
			}
			clustered = append(clustered, CapacityConstrainedClusters(
				subVectors, maxMembers, targetMembers, subDense, sources,
				true, threshold, componentAffinity)...)
		}
		sort.Slice(clustered, func(i, j int) bool {
			return identifierLessFold(clustered[i][0], clustered[j][0])
		})
		return clustered
	}

	// 代表点选取：最远点 + 词法平局裁决。
	byMax := int(math.Ceil(float64(len(identifiers)) / float64(maxMembers)))
	byTarget := int(math.Ceil(float64(len(identifiers)) / float64(targetMembers)))
	clusterCount := byMax
	if byTarget > clusterCount {
		clusterCount = byTarget
	}
	if clusterCount > len(identifiers) {
		clusterCount = len(identifiers)
	}
	representatives := []string{identifiers[0]}
	inRepresentatives := func(identifier string) bool {
		for _, representative := range representatives {
			if representative == identifier {
				return true
			}
		}
		return false
	}
	for len(representatives) < clusterCount {
		var remaining []string
		for _, identifier := range identifiers {
			if !inRepresentatives(identifier) {
				remaining = append(remaining, identifier)
			}
		}
		minDistance := func(identifier string) float64 {
			best := 1.0
			for _, representative := range representatives {
				distance := 1.0 - similarityVectors(identifier, representative)
				if distance < best {
					best = distance
				}
			}
			return best
		}
		selected := remaining[0]
		selectedDistance := minDistance(selected)
		for _, identifier := range remaining[1:] {
			distance := minDistance(identifier)
			if distance > selectedDistance ||
				(math.Abs(distance-selectedDistance) <= 1e-12 && identifierLess(identifier, selected)) {
				selected = identifier
				selectedDistance = distance
			}
		}
		representatives = append(representatives, selected)
	}

	// 8 轮容量约束 k-means。
	centroidOf := func(memberIDs []string) (map[string]float64, []float64) {
		sparseSum := map[string]float64{}
		for _, identifier := range memberIDs {
			for term, value := range sparse[identifier] {
				sparseSum[term] += value
			}
		}
		sparseNormSquared := 0.0
		for _, value := range sparseSum {
			sparseNormSquared += value * value
		}
		sparseNorm := math.Sqrt(sparseNormSquared)
		sparseCenter := map[string]float64{}
		if sparseNorm > 0.0 {
			for term, value := range sparseSum {
				sparseCenter[term] = value / sparseNorm
			}
		}
		if dense == nil {
			return sparseCenter, nil
		}
		denseSum := make([]float64, len(firstDense(dense)))
		for _, identifier := range memberIDs {
			for index, value := range dense[identifier] {
				denseSum[index] += value
			}
		}
		denseNormSquared := 0.0
		for _, value := range denseSum {
			denseNormSquared += value * value
		}
		denseNorm := math.Sqrt(denseNormSquared)
		if denseNorm <= 0.0 {
			return sparseCenter, nil
		}
		center := make([]float64, len(denseSum))
		for index, value := range denseSum {
			center[index] = value / denseNorm
		}
		return sparseCenter, center
	}

	var previousClusters [][]string
	var clusters [][]string
	centersSparse := make([]map[string]float64, 0, len(representatives))
	centersDense := make([][]float64, 0, len(representatives))
	centersSource := make([]SourceDistribution, 0, len(representatives))
	for _, representative := range representatives {
		centersSparse = append(centersSparse, sparse[representative])
		if dense != nil {
			centersDense = append(centersDense, dense[representative])
		} else {
			centersDense = append(centersDense, nil)
		}
		centersSource = append(centersSource, sources[representative])
	}
	for iteration := 0; iteration < 8; iteration++ {
		members := make([][]string, len(representatives))
		for index := range members {
			members[index] = []string{representatives[index]}
		}
		var unassigned []string
		for _, identifier := range identifiers {
			if !inRepresentatives(identifier) {
				unassigned = append(unassigned, identifier)
			}
		}
		type rankedItem struct {
			identifier string
			ordered    []struct {
				score float64
				index int
			}
			best   float64
			margin float64
		}
		var ranked []rankedItem
		for _, identifier := range unassigned {
			scores := make([]float64, len(centersSparse))
			for index, center := range centersSparse {
				centerCosine, centerHasCosine := cosineOf(dense, identifier, centersDense[index])
				scores[index] = SourceAwareScore(
					FusedSemanticScore(SparseVectorCosine(sparse[identifier], center),
						centerCosine, centerHasCosine),
					sources[identifier], centersSource[index])
			}
			ordered := make([]struct {
				score float64
				index int
			}, len(scores))
			for index := range scores {
				ordered[index] = struct {
					score float64
					index int
				}{scores[index], index}
			}
			sort.Slice(ordered, func(i, j int) bool {
				if ordered[i].score != ordered[j].score {
					return ordered[i].score > ordered[j].score
				}
				return ordered[i].index < ordered[j].index
			})
			best := ordered[0].score
			second := 0.0
			if len(ordered) > 1 {
				second = ordered[1].score
			}
			ranked = append(ranked, rankedItem{
				identifier: identifier, ordered: ordered,
				best: best, margin: best - second,
			})
		}
		sort.Slice(ranked, func(i, j int) bool {
			if ranked[i].best != ranked[j].best {
				return ranked[i].best > ranked[j].best
			}
			if ranked[i].margin != ranked[j].margin {
				return ranked[i].margin > ranked[j].margin
			}
			return identifierLess(ranked[i].identifier, ranked[j].identifier)
		})
		for _, item := range ranked {
			targetIndex := -1
			for _, entry := range item.ordered {
				if len(members[entry.index]) < maxMembers {
					targetIndex = entry.index
					break
				}
			}
			if targetIndex == -1 {
				smallest := 0
				for index := range members {
					if len(members[index]) < len(members[smallest]) {
						smallest = index
					}
				}
				targetIndex = smallest
			}
			members[targetIndex] = append(members[targetIndex], item.identifier)
		}
		clusters = make([][]string, len(members))
		for index, memberIDs := range members {
			sort.Slice(memberIDs, func(i, j int) bool {
				return identifierLess(memberIDs[i], memberIDs[j])
			})
			clusters[index] = memberIDs
		}
		clusterState := append([][]string{}, clusters...)
		sort.Slice(clusterState, func(i, j int) bool {
			return identifierLessFold(clusterState[i][0], clusterState[j][0])
		})
		if clustersEqual(clusterState, previousClusters) {
			break
		}
		previousClusters = clusterState
		centersSparse = nil
		centersDense = nil
		centersSource = nil
		for _, memberIDs := range members {
			sparseCenter, denseCenter := centroidOf(memberIDs)
			centersSparse = append(centersSparse, sparseCenter)
			centersDense = append(centersDense, denseCenter)
			var memberSources []SourceDistribution
			for _, member := range memberIDs {
				memberSources = append(memberSources, sources[member])
			}
			centersSource = append(centersSource, MeanSourceDistribution(memberSources))
		}
	}

	// 仅合并小且真相似的簇，绝不超硬上限。
	for {
		type candidate struct {
			score      float64
			leftKey    string
			rightKey   string
			leftIndex  int
			rightIndex int
		}
		var candidates []candidate
		for leftIndex, left := range clusters {
			for rightIndex := leftIndex + 1; rightIndex < len(clusters); rightIndex++ {
				right := clusters[rightIndex]
				if len(left) >= targetMembers && len(right) >= targetMembers {
					continue
				}
				if len(left)+len(right) > maxMembers {
					continue
				}
				leftSparse, leftDense := centroidOf(left)
				rightSparse, rightDense := centroidOf(right)
				var leftSources []SourceDistribution
				for _, member := range left {
					leftSources = append(leftSources, sources[member])
				}
				var rightSources []SourceDistribution
				for _, member := range right {
					rightSources = append(rightSources, sources[member])
				}
				leftCosine := 0.0
				leftHasCosine := false
				if leftDense != nil && rightDense != nil {
					leftCosine, leftHasCosine = CosineSimilarity(leftDense, rightDense)
				}
				score := SourceAwareScore(
					FusedSemanticScore(SparseVectorCosine(leftSparse, rightSparse), leftCosine, leftHasCosine),
					MeanSourceDistribution(leftSources), MeanSourceDistribution(rightSources))
				if score >= semanticAcceptFloor {
					candidates = append(candidates, candidate{
						score: score, leftKey: strings.ToLower(left[0]), rightKey: strings.ToLower(right[0]),
						leftIndex: leftIndex, rightIndex: rightIndex,
					})
				}
			}
		}
		if len(candidates) == 0 {
			break
		}
		sort.Slice(candidates, func(i, j int) bool {
			a, b := candidates[i], candidates[j]
			if a.score != b.score {
				return a.score > b.score
			}
			if a.leftKey != b.leftKey {
				return a.leftKey < b.leftKey
			}
			if a.rightKey != b.rightKey {
				return a.rightKey < b.rightKey
			}
			if a.leftIndex != b.leftIndex {
				return a.leftIndex < b.leftIndex
			}
			return a.rightIndex < b.rightIndex
		})
		chosen := candidates[0]
		merged := append(append([]string{}, clusters[chosen.leftIndex]...), clusters[chosen.rightIndex]...)
		sort.Slice(merged, func(i, j int) bool {
			return identifierLess(merged[i], merged[j])
		})
		var next [][]string
		for index, cluster := range clusters {
			if index != chosen.leftIndex && index != chosen.rightIndex {
				next = append(next, cluster)
			}
		}
		next = append(next, merged)
		sort.Slice(next, func(i, j int) bool {
			return identifierLessFold(next[i][0], next[j][0])
		})
		clusters = next
	}
	return clusters
}

// RankSemanticCandidates 稀疏语义候选排序。
// Python: context_pipeline.py:876 _rank_semantic_candidates
func RankSemanticCandidates(query map[string]float64, candidates []map[string]float64) []struct {
	Index int
	Score float64
} {
	scores := SparseSemanticScores(query, candidates)
	type pair = struct {
		Index int
		Score float64
	}
	pairs := make([]pair, len(scores))
	for index, score := range scores {
		pairs[index] = pair{Index: index, Score: score}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Score != pairs[j].Score {
			return pairs[i].Score > pairs[j].Score
		}
		return pairs[i].Index < pairs[j].Index
	})
	return pairs
}

// SparseSemanticScores 查询 vs 语料的 BM25 融合分。
// Python: context_pipeline.py:831 _sparse_semantic_scores
func SparseSemanticScores(query map[string]float64, corpus []map[string]float64) []float64 {
	if len(query) == 0 {
		return make([]float64, len(corpus))
	}
	comparisonCorpus := append(append([]map[string]float64{}, corpus...), query)
	lengths := make([]float64, len(comparisonCorpus))
	total := 0.0
	for index, item := range comparisonCorpus {
		lengths[index] = sumValues(item)
		total += lengths[index]
	}
	averageLength := math.Max(total/float64(len(comparisonCorpus)), 1.0)
	documentFrequencies := map[string]int{}
	for term := range query {
		count := 0
		for _, item := range comparisonCorpus {
			if item[term] > 0 {
				count++
			}
		}
		documentFrequencies[term] = count
	}
	inverses := map[string]float64{}
	for term, documentFrequency := range documentFrequencies {
		inverses[term] = math.Log(1.0 + (float64(len(comparisonCorpus)-documentFrequency)+0.5)/(float64(documentFrequency)+0.5))
	}
	bm25Of := func(document map[string]float64) float64 {
		documentLength := sumValues(document)
		lengthNormalization := 1.0 - bm25Bp + bm25Bp*documentLength/averageLength
		score := 0.0
		for term, queryWeight := range query {
			frequency := document[term]
			if frequency <= 0 {
				continue
			}
			denominator := frequency + bm25K1p*lengthNormalization
			score += queryWeight * inverses[term] * frequency * (bm25K1p + 1.0) / denominator
		}
		return score
	}
	selfScore := math.Max(bm25Of(query), 1e-9)
	queryWeight := math.Max(sumValues(query), 1e-9)
	scores := make([]float64, len(corpus))
	for index, document := range corpus {
		normalizedBM25 := math.Min(1.0, bm25Of(document)/selfScore)
		overlap := 0.0
		for term, weight := range query {
			overlap += math.Min(weight, document[term])
		}
		overlap /= queryWeight
		sharedPhrase := 0.0
		for term := range query {
			if _, exists := document[term]; !exists || isAllDigits(term) {
				continue
			}
			denominator := 8.0
			if cjkCharPattern.MatchString(term) {
				denominator = 4.0
			}
			sharedPhrase = math.Max(sharedPhrase, math.Min(1.0, float64(len(term))/denominator))
		}
		scores[index] = math.Min(1.0, 0.50*normalizedBM25+0.25*overlap+0.25*sharedPhrase)
	}
	return scores
}

// BM25Raw 裸 BM25 评分。
// Python: context_pipeline.py:808 _bm25_raw
func BM25Raw(query map[string]float64, document map[string]float64, corpus []map[string]float64) float64 {
	if len(query) == 0 || len(document) == 0 || len(corpus) == 0 {
		return 0.0
	}
	lengths := make([]float64, len(corpus))
	average := 0.0
	for index, item := range corpus {
		lengths[index] = sumValues(item)
		average += lengths[index]
	}
	average /= float64(len(corpus))
	documentLength := sumValues(document)
	score := 0.0
	for term, queryWeight := range query {
		frequency := document[term]
		if frequency <= 0 {
			continue
		}
		documentFrequency := 0
		for _, item := range corpus {
			if item[term] > 0 {
				documentFrequency++
			}
		}
		inverse := math.Log(1.0 + (float64(len(corpus)-documentFrequency)+0.5)/(float64(documentFrequency)+0.5))
		lengthNormalization := 1.0 - bm25Bp + bm25Bp*documentLength/math.Max(average, 1.0)
		denominator := frequency + bm25K1p*lengthNormalization
		score += queryWeight * inverse * frequency * (bm25K1p + 1.0) / denominator
	}
	return score
}

// SemanticTokens 语义分词（NFKC + casefold + ascii 词 + CJK 段与 2/3/4 元组）。
// Python: context_pipeline.py:306 _semantic_tokens
func SemanticTokens(value string) []string {
	normalized := strings.ToLower(norm.NFKC.String(value))
	var tokens []string
	tokens = append(tokens, asciiSemanticTokenPattern.FindAllString(normalized, -1)...)
	for _, run := range cjkRunPattern.FindAllString(normalized, -1) {
		tokens = append(tokens, run)
		runes := []rune(run)
		for _, width := range []int{2, 3, 4} {
			for index := 0; index+width <= len(runes); index++ {
				tokens = append(tokens, string(runes[index:index+width]))
			}
		}
	}
	var filtered []string
	for _, token := range tokens {
		if _, stopped := semanticStopTerms[token]; !stopped {
			filtered = append(filtered, token)
		}
	}
	return filtered
}

// SemanticFields 字段加权语义向量。
// Python: context_pipeline.py:316 _semantic_fields
func SemanticFields(title string, headings []string, preview string) map[string]float64 {
	weighted := map[string]float64{}
	for _, entry := range []struct {
		value  string
		weight float64
	}{{title, 4.0}, {strings.Join(headings, "\n"), 2.0}, {preview, 1.0}} {
		for _, token := range SemanticTokens(entry.value) {
			weighted[token] += entry.weight
		}
	}
	return weighted
}

// ClusteringSparseVectors 语义记录的 L2 归一 BM25 稀疏向量（bm25: 前缀）。
// Python: context_pipeline.py:355 _clustering_sparse_vectors
func ClusteringSparseVectors(recordsByID map[string]SemanticRecord) map[string]map[string]float64 {
	fieldsByID := map[string]map[string]float64{}
	for identifier, record := range recordsByID {
		fieldsByID[identifier] = SemanticFields(record.Title, record.Headings, record.Preview)
	}
	base := BM25SparseVectors(fieldsByID)
	result := map[string]map[string]float64{}
	for _, identifier := range sortedIdentifiers(recordsByID) {
		vector := map[string]float64{}
		for term, value := range base[identifier] {
			vector["bm25:"+term] = value
		}
		normSquared := 0.0
		for _, value := range vector {
			normSquared += value * value
		}
		vectorNorm := math.Sqrt(normSquared)
		if vectorNorm > 0.0 {
			for term, value := range vector {
				vector[term] = value / vectorNorm
			}
		}
		result[identifier] = vector
	}
	return result
}

// BM25SparseVectors 语义字段的确定性 L2 归一 BM25 向量。
// Python: context_pipeline.py:324 _bm25_sparse_vectors
func BM25SparseVectors(fieldsByID map[string]map[string]float64) map[string]map[string]float64 {
	identifiers := sortedIdentifiers(fieldsByID)
	corpus := make([]map[string]float64, len(identifiers))
	lengths := make([]float64, len(identifiers))
	lengthTotal := 0.0
	for index, identifier := range identifiers {
		corpus[index] = fieldsByID[identifier]
		length := 0.0
		for _, value := range fieldsByID[identifier] {
			length += math.Max(0.0, value)
		}
		lengths[index] = length
		lengthTotal += length
	}
	averageLength := math.Max(lengthTotal/math.Max(float64(len(lengths)), 1.0), 1.0)
	termSet := map[string]struct{}{}
	for _, fields := range corpus {
		for term := range fields {
			termSet[term] = struct{}{}
		}
	}
	terms := sortedIdentifiers(termSet)
	documentFrequency := map[string]int{}
	for _, term := range terms {
		count := 0
		for _, fields := range corpus {
			if math.Max(0.0, fields[term]) > 0.0 {
				count++
			}
		}
		documentFrequency[term] = count
	}
	result := map[string]map[string]float64{}
	for index, identifier := range identifiers {
		fields := corpus[index]
		length := lengths[index]
		weighted := map[string]float64{}
		normalization := 1.0 - bm25Bp + bm25Bp*length/averageLength
		var fieldTerms []string
		for term := range fields {
			fieldTerms = append(fieldTerms, term)
		}
		sort.Strings(fieldTerms)
		for _, term := range fieldTerms {
			if isAllDigits(term) {
				continue
			}
			frequency := math.Max(0.0, fields[term])
			if frequency <= 0.0 {
				continue
			}
			df := documentFrequency[term]
			inverse := math.Log(1.0 + (float64(len(corpus)-df)+0.5)/(float64(df)+0.5))
			weighted[term] = inverse * frequency * (bm25K1p + 1.0) / (frequency + bm25K1p*normalization)
		}
		normSquared := 0.0
		for _, value := range weighted {
			normSquared += value * value
		}
		vectorNorm := math.Sqrt(normSquared)
		if vectorNorm > 0.0 {
			for term, value := range weighted {
				weighted[term] = value / vectorNorm
			}
		}
		result[identifier] = weighted
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// pipelineError 构造 CONTEXT_PROACTIVE_PIPELINE_EXECUTION_ERROR。
// Python: context_pipeline.py:286 _pipeline_error
func pipelineError(message string) error {
	return BuildError(StatusCodeContextProactivePipelineExecutionError, exception.WithMsg(message))
}

// publishError 构造 CONTEXT_PROACTIVE_PUBLISH_EXECUTION_ERROR。
// Python: context_pipeline.py:290 _publish_error
func publishError(message string) error {
	return BuildError(StatusCodeContextProactivePublishExecutionError, exception.WithMsg(message))
}

// safeSegment 队列标识安全化（service_id 走 ValidateServiceID，其余走安全段正则）。
// Python: context_pipeline.py:294 _safe_segment
func safePipelineSegment(value string, name string) (string, error) {
	if name == "service_id" {
		text, err := ValidateServiceID(value)
		if err != nil {
			return "", publishError(err.Error())
		}
		return text, nil
	}
	if value == "" || value == "." || value == ".." || !safeSegmentPatternPipeline.MatchString(value) {
		return "", publishError(fmt.Sprintf("unsafe %s", name))
	}
	return value, nil
}

// normalizedDenseVectors 稠密向量校验与 L2 归一（任一非法返回 nil）。
// Python: context_pipeline.py:444 _normalized_dense_vectors
func normalizedDenseVectors(vectorsByID map[string][]float64, identifiers []string) map[string][]float64 {
	if vectorsByID == nil {
		return nil
	}
	for _, identifier := range identifiers {
		if _, exists := vectorsByID[identifier]; !exists {
			return nil
		}
	}
	result := map[string][]float64{}
	dimension := -1
	for _, identifier := range identifiers {
		raw := vectorsByID[identifier]
		if len(raw) == 0 {
			return nil
		}
		for _, value := range raw {
			if math.IsInf(value, 0) || math.IsNaN(value) {
				return nil
			}
		}
		if dimension != -1 && len(raw) != dimension {
			return nil
		}
		dimension = len(raw)
		normSquared := 0.0
		for _, value := range raw {
			normSquared += value * value
		}
		if math.IsInf(normSquared, 0) || math.IsNaN(normSquared) || normSquared <= 0.0 {
			return nil
		}
		vectorNorm := math.Sqrt(normSquared)
		vector := make([]float64, len(raw))
		for index, value := range raw {
			vector[index] = value / vectorNorm
		}
		result[identifier] = vector
	}
	return result
}

// bestSplit 一维最大类间方差切分：返回切点与低侧 distinct 值。
// Python: context_pipeline.py:402 内嵌 best_split。
func bestSplit(splitValues []float64, counts map[float64]int) (float64, []float64) {
	totalN := 0
	totalSum := 0.0
	for _, value := range splitValues {
		totalN += counts[value]
		totalSum += value * float64(counts[value])
	}
	bestCut := splitValues[0]
	bestBetween := -1.0
	var lowValues []float64
	leftN := 0
	leftSum := 0.0
	var leftValues []float64
	for _, value := range splitValues[:len(splitValues)-1] {
		leftN += counts[value]
		leftSum += value * float64(counts[value])
		leftValues = append(leftValues, value)
		rightN := totalN - leftN
		rightSum := totalSum - leftSum
		leftMean := leftSum / float64(leftN)
		rightMean := rightSum / float64(rightN)
		between := float64(leftN*rightN) * (leftMean - rightMean) * (leftMean - rightMean)
		if between > bestBetween {
			bestBetween = between
			bestCut = (leftMean + rightMean) / 2.0
			lowValues = append([]float64{}, leftValues...)
		}
	}
	return bestCut, lowValues
}

// cosineOf dense 中心余弦助手（k-means 内联使用）。
func cosineOf(dense map[string][]float64, identifier string, center []float64) (float64, bool) {
	if dense == nil || center == nil {
		return 0, false
	}
	return CosineSimilarity(dense[identifier], center)
}

// firstDense 取任一稠密向量确定维度。
func firstDense(dense map[string][]float64) []float64 {
	keys := make([]string, 0, len(dense))
	for key := range dense {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return dense[keys[0]]
}

// identifierLess (casefold, value) 字典序。
func identifierLess(a string, b string) bool {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	if la != lb {
		return la < lb
	}
	return a < b
}

// identifierLessFold 仅 casefold 序（Python cluster[0].casefold() 排序）。
func identifierLessFold(a string, b string) bool {
	return strings.ToLower(a) < strings.ToLower(b)
}

// sortedIdentifiers (casefold, value) 排序的键列表。
func sortedIdentifiers[T any](collection map[string]T) []string {
	keys := make([]string, 0, len(collection))
	for key := range collection {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return identifierLess(keys[i], keys[j])
	})
	return keys
}

// minIdentifier 取 (casefold, value) 最小键。
func minIdentifier(unseen map[string]struct{}) string {
	best := ""
	for key := range unseen {
		if best == "" || identifierLess(key, best) {
			best = key
		}
	}
	return best
}

// clustersEqual 簇集合逐项相等。
func clustersEqual(a [][]string, b [][]string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil || len(a) != len(b) {
		return false
	}
	for index := range a {
		if len(a[index]) != len(b[index]) {
			return false
		}
		for memberIndex := range a[index] {
			if a[index][memberIndex] != b[index][memberIndex] {
				return false
			}
		}
	}
	return true
}

// sumValues 向量求和。
func sumValues(vector map[string]float64) float64 {
	total := 0.0
	for _, value := range vector {
		total += value
	}
	return total
}

// isAllDigits 全数字判定（Python str.isdigit，含全角数字，此处 ASCII 等价 +
// NFKC 已归一，语义一致）。
func isAllDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// 流水线第二块：磁盘工具层。
// Python: context_pipeline.py:1012-1426（一比一移植）。
// asyncio 协作式取消 → goroutine + ctx 轮询（偏差 D-24.3a-4，spec §5）。

// ──────────────────────────── 常量 ────────────────────────────

// briefing 摘要上限。Python: context_pipeline.py:118-120
const (
	briefingSummaryChars = 450
	briefingHeadingLimit = 8
	briefingHeadingChars = 160
)

// ──────────────────────────── 全局变量 ────────────────────────────

// markdownHeadingPattern 发布页标题（1-6 级，含尾部 # 剥离）。
// Python: context_pipeline.py:82 _MARKDOWN_HEADING
var markdownHeadingPattern = regexp.MustCompile(`(?m)^ {0,3}(#{1,6})[ \t]+(.+?)\s*#*\s*$`)

// briefingHeadingPattern briefing 提取用（fullmatch 单行）。
// Python: context_pipeline.py:1378 内 _MARKDOWN_HEADING.fullmatch(line)
var briefingHeadingPattern = regexp.MustCompile(`^ {0,3}(#{1,6})[ \t]+(.+?)\s*#*\s*$`)

// windowsDriveFullPattern 盘符绝对路径。
// Python: context_pipeline.py:1213 内 r"[A-Za-z]:.*"
var windowsDriveFullPattern = regexp.MustCompile(`^[A-Za-z]:.*`)

// fenceStartPrefixes 围栏开头标记。
var fenceStartPrefixes = []string{"```", "~~~"}

// ──────────────────────────── 导出函数 ────────────────────────────

// CancelSafeToThread 等线程 IO 落定后再上抛调用方取消。
// Python: context_pipeline.py:1021 _cancel_safe_to_thread —— Go：goroutine 执行 +
// ctx.Done 后等待结果再返回 ctx.Err()（偏差 D-24.3a-4）。
func CancelSafeToThread(ctx context.Context, fn func() error) error {
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		<-done // 等线程结果落定（Python: task.result() 吞异常后 raise cancellation）
		return ctx.Err()
	}
}

// CancelCooperativeToThread 协作式可停只读工作。
// Python: context_pipeline.py:1061 _cancel_cooperative_to_thread —— fn 接收
// cancelRequested 轮询函数；ctx 取消时置位并等待结果后上抛取消。
func CancelCooperativeToThread(ctx context.Context, fn func(cancelRequested func() bool) error) error {
	done := make(chan error, 1)
	go func() { done <- fn(func() bool { return ctx.Err() != nil }) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		<-done
		return ctx.Err()
	}
}

// RaiseIfThreadCancelled 取消检查点。
// Python: context_pipeline.py:1056 _raise_if_thread_cancelled
func RaiseIfThreadCancelled(cancelRequested func() bool) error {
	if cancelRequested != nil && cancelRequested() {
		return context.Canceled
	}
	return nil
}

// Digest 管理路径摘要（sha256 前 32 位，保 Windows MAX_PATH 之下）。
// Python: context_pipeline.py:1091 _digest
func Digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:32]
}

// SplitBlocks 段落切块并按上限再切。
// Python: context_pipeline.py:1362 _split_blocks
func SplitBlocks(markdown string) []string {
	paragraphPattern := regexp.MustCompile(`\n\s*\n`)
	var paragraphs []string
	for _, part := range paragraphPattern.Split(markdown, -1) {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			paragraphs = append(paragraphs, trimmed)
		}
	}
	if len(paragraphs) == 0 {
		trimmed := strings.TrimSpace(markdown)
		if trimmed != "" {
			paragraphs = []string{trimmed}
		}
	}
	var blocks []string
	for _, paragraph := range paragraphs {
		runes := []rune(paragraph)
		for index := 0; index < len(runes); index += maxBlockChars {
			end := index + maxBlockChars
			if end > len(runes) {
				end = len(runes)
			}
			blocks = append(blocks, string(runes[index:end]))
		}
	}
	if len(blocks) == 0 {
		return []string{""}
	}
	return blocks
}

// NormalizeMarkdown 规范化采集的源 Markdown（去 NUL、统一换行、行尾去空白）。
// Python: context_pipeline.py:1372 _normalize_markdown
func NormalizeMarkdown(content string) string {
	normalized := strings.ReplaceAll(content, "\x00", "")
	normalized = strings.ReplaceAll(normalized, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	var lines []string
	for _, line := range strings.Split(normalized, "\n") {
		lines = append(lines, strings.TrimRight(line, " \t\v\f\r"))
	}
	return strings.TrimSpace(strings.Join(lines, "\n")) + "\n"
}

// DeterministicBriefingPreview 无模型抽取有界大纲与首段。
// Python: context_pipeline.py:1378 _deterministic_briefing_preview
func DeterministicBriefingPreview(content string) map[string]any {
	normalized := NormalizeMarkdown(SourceLinkPreview(content))
	lines := strings.Split(strings.TrimSuffix(normalized, "\n"), "\n")
	var headings []map[string]any
	var paragraph []string
	var fallbackLines []string
	paragraphComplete := false
	inFrontmatter := len(lines) > 0 && strings.TrimSpace(lines[0]) == "---"
	inFence := false

	for index, line := range lines {
		stripped := strings.TrimSpace(line)
		if inFrontmatter {
			if index > 0 && (stripped == "---" || stripped == "...") {
				inFrontmatter = false
			}
			continue
		}
		if strings.HasPrefix(stripped, "```") || strings.HasPrefix(stripped, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		heading := briefingHeadingPattern.FindStringSubmatch(line)
		if heading != nil {
			if len(headings) < briefingHeadingLimit {
				text := strings.TrimSpace(heading[2])
				text = strings.ReplaceAll(text, "`", "'")
				runes := []rune(text)
				if len(runes) > briefingHeadingChars {
					text = string(runes[:briefingHeadingChars])
				}
				headings = append(headings, map[string]any{
					"level": len(heading[1]), "text": text,
				})
			}
			if len(paragraph) > 0 {
				paragraphComplete = true
			}
			continue
		}
		if stripped == "" || stripped == "---" || stripped == "***" || stripped == "___" {
			if len(paragraph) > 0 {
				paragraphComplete = true
			}
			continue
		}
		fallbackLines = append(fallbackLines, stripped)
		if !paragraphComplete {
			paragraph = append(paragraph, stripped)
		}
	}

	summary := strings.Join(paragraph, " ")
	if summary == "" {
		summary = strings.Join(fallbackLines, " ")
	}
	if summary == "" {
		summary = strings.TrimSpace(normalized)
	}
	runes := []rune(summary)
	if len(runes) > briefingSummaryChars {
		summary = strings.TrimRight(string(runes[:briefingSummaryChars]), "")
	}
	return map[string]any{
		"headings":      headings,
		"summary":       summary,
		"content_chars": len([]rune(normalized)),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// pipelinePathExists 探测存在性（扩展路径）。
// Python: context_pipeline.py:1110 _path_exists —— 委托 pathSafety 同名实现。
func pipelinePathExists(path string) bool {
	return pathExists(path)
}

// pipelinePathIsFile 是否常规文件。
// Python: context_pipeline.py:1114 _path_is_file
func pipelinePathIsFile(path string) bool {
	info, err := os.Lstat(extendedPath(path))
	return err == nil && info.Mode().IsRegular()
}

// pipelinePathIsDir 是否目录。
// Python: context_pipeline.py:1118 _path_is_dir
func pipelinePathIsDir(path string) bool {
	info, err := os.Lstat(extendedPath(path))
	return err == nil && info.IsDir()
}

// pipelinePathIsLinkOrReparse 链接或重解析点。
// Python: context_pipeline.py:1122 _path_is_link_or_reparse
func pipelinePathIsLinkOrReparse(path string) bool {
	return pathIsLinkOrReparse(path)
}

// walkTreePaths 确定性遍历（先目录后文件，casefold 排序）。
// Python: context_pipeline.py:1127 _walk_tree_paths
func walkTreePaths(root string) []string {
	var result []string
	if !pipelinePathExists(root) {
		return result
	}
	extendedRoot := extendedPath(root)
	var walk func(current string, relative string)
	walk = func(current string, relative string) {
		entries, err := os.ReadDir(current)
		if err != nil {
			return
		}
		var dirNames, fileNames []string
		for _, entry := range entries {
			if entry.IsDir() {
				dirNames = append(dirNames, entry.Name())
			} else {
				fileNames = append(fileNames, entry.Name())
			}
		}
		sort.Slice(dirNames, func(i, j int) bool { return identifierLess(dirNames[i], dirNames[j]) })
		sort.Slice(fileNames, func(i, j int) bool { return identifierLess(fileNames[i], fileNames[j]) })
		for _, name := range append(append([]string{}, dirNames...), fileNames...) {
			childRelative := name
			if relative != "" {
				childRelative = relative + "/" + name
			}
			result = append(result, filepath.Join(root, filepath.FromSlash(childRelative)))
			if entry := current + string(filepath.Separator) + name; pipelinePathIsDir(entry) &&
				!pathIsLinkOrReparse(entry) {
				walk(entry, childRelative)
			}
		}
	}
	walk(extendedRoot, "")
	return result
}

// pipelineReplacePath rename 替换（父目录自动创建）。
// Python: context_pipeline.py:1144 _replace_path
func pipelineReplacePath(source string, target string) error {
	if err := os.MkdirAll(filepath.Dir(extendedPath(target)), 0o755); err != nil {
		return err
	}
	return os.Rename(extendedPath(source), extendedPath(target))
}

// pipelineAtomicWrite 临时文件 + fsync + rename（前缀 .tmp-）。
// Python: context_pipeline.py:1150 _atomic_write
func pipelineAtomicWrite(path string, data []byte) error {
	target := extendedPath(path)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tempFile, err := os.CreateTemp(filepath.Dir(target), ".tmp-")
	if err != nil {
		return err
	}
	temporary := tempFile.Name()
	defer func() {
		if temporary != "" {
			_ = os.Remove(temporary)
		}
	}()
	if _, err := tempFile.Write(data); err != nil {
		tempFile.Close()
		return err
	}
	if err := tempFile.Sync(); err != nil {
		tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, target); err != nil {
		return err
	}
	temporary = ""
	return nil
}

// assertNoSymlinks 受管目录内禁 symlink/reparse。
// Python: context_pipeline.py:1168 _assert_no_symlinks
func assertNoSymlinks(root string) error {
	if err := assertPipelinePathChainNoSymlinks(root); err != nil {
		return err
	}
	if pipelinePathIsLinkOrReparse(root) {
		return publishError("managed directory must not contain symlinks")
	}
	if !pipelinePathExists(root) {
		return nil
	}
	for _, entry := range walkTreePaths(root) {
		if pipelinePathIsLinkOrReparse(entry) {
			return publishError("managed directory must not contain symlinks")
		}
	}
	return nil
}

// assertPipelinePathChainNoSymlinks 既有路径与其父链禁 symlink。
// Python: context_pipeline.py:1181 _assert_path_chain_no_symlinks
func assertPipelinePathChainNoSymlinks(path string) error {
	current := path
	for {
		if pipelinePathIsLinkOrReparse(current) {
			return publishError("managed path must not traverse symlinks")
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

// copyTree 复制目录树（symlinks=False 语义：链接按实体复制）。
// Python: context_pipeline.py:1193 _copy_tree
func copyTree(source string, target string) error {
	if !pipelinePathExists(source) {
		return os.MkdirAll(extendedPath(target), 0o755)
	}
	if err := assertNoSymlinks(source); err != nil {
		return err
	}
	if err := os.MkdirAll(extendedPath(target), 0o755); err != nil {
		return err
	}
	var copyErr error
	for _, entry := range walkTreePaths(source) {
		relative, relErr := filepath.Rel(source, entry)
		if relErr != nil {
			return relErr
		}
		destination := filepath.Join(target, relative)
		info, statErr := os.Lstat(entry)
		if statErr != nil {
			return statErr
		}
		if info.IsDir() {
			if err := os.MkdirAll(destination, info.Mode().Perm()); err != nil {
				return err
			}
			continue
		}
		data, readErr := os.ReadFile(entry)
		if readErr != nil {
			return readErr
		}
		if copyErr = os.WriteFile(destination, data, info.Mode().Perm()); copyErr != nil {
			return copyErr
		}
	}
	return nil
}

// pipelineRelativeFile 相对受管根的 POSIX 相对路径。
// Python: context_pipeline.py:1206 _relative_file
func pipelineRelativeFile(path string, root string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", publishError("managed path escaped its root")
	}
	rootAbsolute, err := filepath.Abs(root)
	if err != nil {
		return "", publishError("managed path escaped its root")
	}
	relative, relErr := filepath.Rel(rootAbsolute, absolute)
	if relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", publishError("managed path escaped its root")
	}
	return filepath.ToSlash(relative), nil
}

// validatedRelativePath 工作区相对路径校验。
// Python: context_pipeline.py:1213 _validated_relative_path
func validatedRelativePath(value string, name string) (string, error) {
	normalized := strings.ReplaceAll(value, "\\", "/")
	if windowsDriveFullPattern.MatchString(normalized) || normalized == "" ||
		strings.HasPrefix(normalized, "/") {
		return "", publishError(fmt.Sprintf("%s is unsafe", name))
	}
	for _, part := range strings.Split(normalized, "/") {
		if part == "" || part == "." || part == ".." {
			return "", publishError(fmt.Sprintf("%s is unsafe", name))
		}
	}
	return normalized, nil
}

// isProgramDescription 目录级描述页是程序元数据。
// Python: context_pipeline.py:1226 _is_program_description
func isProgramDescription(relative string) bool {
	return strings.EqualFold(filepath.Base(relative), "description.md")
}

// assertCanonicalDescriptionNames 拒绝非规范大小写描述名。
// Python: context_pipeline.py:1233 _assert_canonical_description_names
func assertCanonicalDescriptionNames(contextRoot string, repairable bool) error {
	for _, entry := range walkTreePaths(contextRoot) {
		name := filepath.Base(entry)
		if strings.EqualFold(name, "description.md") && name != "description.md" {
			if repairable {
				return pipelineError("description.md must use canonical lowercase casing")
			}
			return publishError("description.md must use canonical lowercase casing")
		}
	}
	return nil
}

// removeTreeEntry 删除单个树条目（文件/链接删除，目录递归）。
// Python: context_pipeline.py:1242 _remove_tree_entry
func removeTreeEntry(path string) error {
	if pipelinePathIsLinkOrReparse(path) || pipelinePathIsFile(path) {
		return os.Remove(extendedPath(path))
	}
	if pipelinePathIsDir(path) {
		return removeTree(path)
	}
	return nil
}

// removeTree 递归删除（只读文件 chmod 后重试）。
// Python: context_pipeline.py:1266 _remove_tree
func removeTree(path string) error {
	err := os.RemoveAll(path)
	if err == nil {
		return nil
	}
	if !os.IsNotExist(err) && !os.IsPermission(err) {
		return err
	}
	if !pipelinePathExists(path) && !pipelinePathIsLinkOrReparse(path) {
		return nil
	}
	return os.RemoveAll(extendedPath(path))
}

// makeTreeWritable 只读源副本落地后使候选可删。
// Python: context_pipeline.py:1280 _make_tree_writable
func makeTreeWritable(path string) error {
	if !pipelinePathExists(path) || pipelinePathIsLinkOrReparse(path) {
		return nil
	}
	entries := append([]string{path}, walkTreePaths(path)...)
	for _, entry := range entries {
		info, statErr := os.Stat(extendedPath(entry))
		if statErr != nil {
			continue
		}
		if err := os.Chmod(extendedPath(entry), info.Mode().Perm()|0o222); err != nil {
			return err
		}
	}
	return nil
}

// removeEmptyDirectories 自深而浅删除空目录。
// Python: context_pipeline.py:1290 _remove_empty_directories
func removeEmptyDirectories(root string) error {
	if !pipelinePathExists(root) {
		return nil
	}
	var directories []string
	for _, entry := range walkTreePaths(root) {
		if pipelinePathIsDir(entry) {
			directories = append(directories, entry)
		}
	}
	sort.Slice(directories, func(i, j int) bool {
		ri, _ := filepath.Rel(root, directories[i])
		rj, _ := filepath.Rel(root, directories[j])
		depthI := len(strings.Split(filepath.ToSlash(ri), "/"))
		depthJ := len(strings.Split(filepath.ToSlash(rj), "/"))
		if depthI != depthJ {
			return depthI > depthJ
		}
		return filepath.ToSlash(ri) < filepath.ToSlash(rj)
	})
	for _, directory := range directories {
		_ = os.Remove(extendedPath(directory))
	}
	return nil
}

// copyAndPublishTree 候选文件增量发布（不动旧根描述仍引用的文件）。
// Python: context_pipeline.py:1302 _copy_and_publish_tree
func copyAndPublishTree(candidate string, target string, skipRelative string) (map[string]struct{}, error) {
	if err := assertNoSymlinks(candidate); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(extendedPath(target), 0o755); err != nil {
		return nil, err
	}
	if err := assertNoSymlinks(target); err != nil {
		return nil, err
	}
	candidateFiles := map[string]struct{}{}
	for _, entry := range walkTreePaths(candidate) {
		if pipelinePathIsFile(entry) {
			relative, err := pipelineRelativeFile(entry, candidate)
			if err != nil {
				return nil, err
			}
			candidateFiles[relative] = struct{}{}
		}
	}
	targetFiles := map[string]struct{}{}
	for _, entry := range walkTreePaths(target) {
		if pipelinePathIsFile(entry) {
			relative, err := pipelineRelativeFile(entry, target)
			if err != nil {
				return nil, err
			}
			targetFiles[relative] = struct{}{}
		}
	}
	if skipRelative != "" {
		delete(candidateFiles, skipRelative)
		delete(targetFiles, skipRelative)
	}
	var ordinaryFiles, nestedDescriptions []string
	for relative := range candidateFiles {
		if isProgramDescription(relative) {
			nestedDescriptions = append(nestedDescriptions, relative)
		} else {
			ordinaryFiles = append(ordinaryFiles, relative)
		}
	}
	sort.Strings(ordinaryFiles)
	sort.Slice(nestedDescriptions, func(i, j int) bool {
		depthI := len(strings.Split(nestedDescriptions[i], "/"))
		depthJ := len(strings.Split(nestedDescriptions[j], "/"))
		if depthI != depthJ {
			return depthI > depthJ
		}
		return nestedDescriptions[i] < nestedDescriptions[j]
	})
	for _, relative := range append(append([]string{}, ordinaryFiles...), nestedDescriptions...) {
		sourceData, err := os.ReadFile(extendedPath(filepath.Join(candidate, filepath.FromSlash(relative))))
		if err != nil {
			return nil, err
		}
		destination := extendedPath(filepath.Join(target, filepath.FromSlash(relative)))
		if info, statErr := os.Lstat(destination); statErr == nil && info.Mode().IsRegular() {
			if existing, readErr := os.ReadFile(destination); readErr == nil &&
				string(existing) == string(sourceData) {
				continue
			}
		}
		if err := pipelineAtomicWrite(filepath.Join(target, filepath.FromSlash(relative)), sourceData); err != nil {
			return nil, err
		}
	}
	obsolete := map[string]struct{}{}
	for relative := range targetFiles {
		if _, stillUsed := candidateFiles[relative]; !stillUsed {
			obsolete[relative] = struct{}{}
		}
	}
	return obsolete, nil
}

// removePublishedTreeEntries 新根描述可见后清理废弃文件。
// Python: context_pipeline.py:1339 _remove_published_tree_entries
func removePublishedTreeEntries(target string, relatives map[string]struct{}) error {
	if err := assertNoSymlinks(target); err != nil {
		return err
	}
	var sortedRelatives []string
	for relative := range relatives {
		sortedRelatives = append(sortedRelatives, relative)
	}
	sort.Strings(sortedRelatives)
	for _, relative := range sortedRelatives {
		if err := removeTreeEntry(filepath.Join(target, filepath.FromSlash(relative))); err != nil {
			return err
		}
	}
	return removeEmptyDirectories(target)
}

// commitContextTree 把校验后的候选作为取消安全提交单元发布。
// Python: context_pipeline.py:1348 _commit_context_tree
func commitContextTree(candidate string, target string) error {
	obsolete, err := copyAndPublishTree(candidate, target, "description.md")
	if err != nil {
		return err
	}
	descriptionData, err := os.ReadFile(filepath.Join(candidate, "description.md"))
	if err != nil {
		return err
	}
	if err := pipelineAtomicWrite(filepath.Join(target, "description.md"), descriptionData); err != nil {
		return err
	}
	if err := removePublishedTreeEntries(target, obsolete); err != nil {
		return err
	}
	return assertNoSymlinks(target)
}

// 流水线第三块 A：标记常量、受管块替换、容量、语义部件与上下文目录助手。
// Python: context_pipeline.py:63-103（标记）、1427-1780、1795-1843、3763-3789、3986-4035、
// 7030-7130（语义名）。路由/导航/来源分布留后续块。

// ──────────────────────────── 常量 ────────────────────────────

// 管理块标记。Python: context_pipeline.py:88-103
const (
	rootNavigationStart       = "<!-- personal-context:navigation:start -->"
	rootNavigationEnd         = "<!-- personal-context:navigation:end -->"
	directoryOverviewStart    = "<!-- personal-context:directory-overview:start -->"
	directoryOverviewEnd      = "<!-- personal-context:directory-overview:end -->"
	directoryDescriptionStart = "<!-- personal-context:directory-description:start -->"
	directoryDescriptionEnd   = "<!-- personal-context:directory-description:end -->"
	sourceLinksStart          = "<!-- personal-context:source-links:start -->"
	sourceLinksEnd            = "<!-- personal-context:source-links:end -->"
	topicLinksStart           = "<!-- personal-context:topic-links:start -->"
	topicLinksEnd             = "<!-- personal-context:topic-links:end -->"
	relatedStart              = "<!-- personal-context-related:start -->"
	relatedEnd                = "<!-- personal-context-related:end -->"
	emptyDeterministicPreview = "（没有可用的确定性预览。）"
)

// 已知源标题扩展名。Python: context_pipeline.py:125 _KNOWN_SOURCE_TITLE_EXTENSIONS
var knownSourceTitleExtensions = []string{
	".markdown", ".docx", ".pptx", ".xlsx", ".html", ".epub", ".pdf",
	".doc", ".ppt", ".xls", ".txt", ".md", ".htm",
}

// ──────────────────────────── 全局变量 ────────────────────────────

// markdownAngleDestination / markdownBareDestination 目的地词法。
// Python: context_pipeline.py:76-77
const markdownAngleDestinationPatternSrc = `<[^<>\r\n]+>(?:\s+(?:"[^"\r\n]*"|'[^'\r\n]*'|\([^)]*\)))?`
const markdownBareDestinationPatternSrc = `(?:[^()\r\n]|\([^()\r\n]*\))+`

// markdownDestinationTokenPattern 目的地标记。
var markdownDestinationTokenPattern = regexp.MustCompile(
	`(` + markdownAngleDestinationPatternSrc + `|` + markdownBareDestinationPatternSrc + `)`)

// markdownLinkTokenPattern 链接（含图片）。
// Python: context_pipeline.py:79 _MARKDOWN_LINK_TOKEN
var markdownLinkTokenPattern = regexp.MustCompile(
	`\[[^\]\r\n]*\]\(` + markdownDestinationTokenPattern.String() + `\)`)

// markdownInlineLinkPattern 行内链接。
// Python: context_pipeline.py:81 _MARKDOWN_INLINE_LINK
var markdownInlineLinkPattern = regexp.MustCompile(
	`!?\[([^\]\r\n]+)\]\(` + markdownDestinationTokenPattern.String() + `\)`)

// shortReferencePattern 受管引用令牌。
// Python: context_pipeline.py:83 _SHORT_REFERENCE
var shortReferencePattern = regexp.MustCompile(`\[\[ref:(0|[1-9][0-9]*)\]\]`)

// sourceMetadataIDPattern 源 ID。
// Python: context_pipeline.py:85 _SOURCE_METADATA_ID
var sourceMetadataIDPattern = regexp.MustCompile(`^src_[0-9a-f]{32}$`)

// uriSchemePattern URI scheme 前缀。
// Python: context_pipeline.py:86 _URI_SCHEME
var uriSchemePattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*:`)

// personalContextManagedMarkerPattern 管理块标记全集。
// Python: context_pipeline.py:87 _PERSONAL_CONTEXT_MANAGED_MARKER
var personalContextManagedMarkerPattern = regexp.MustCompile(
	`<!--\s*personal-context(?::[a-z0-9-]+|-[a-z0-9-]+):(?:start|end)\s*-->`)

// managedSourceMarkerPattern 受管源标记行。
// Python: context_pipeline.py:100 _MANAGED_SOURCE_MARKER
var managedSourceMarkerPattern = regexp.MustCompile(
	`(?m)^<!-- personal-context-managed-source: (src_[0-9a-f]{32}) -->\r?$`)

// managedLocalLinkPattern 受管块内链接行。
// Python: context_pipeline.py:1668 内联 fullmatch
var managedLocalLinkPattern = regexp.MustCompile(`- \[([^\]\r\n]+)\]\(([^\r\n]+)\)`)

// connectorSuffixPattern 连接词尾缀。
// Python: context_pipeline.py 内 _trim_semantic_connector 的 connector
var connectorSuffixPattern = regexp.MustCompile(
	`(?i)(?:\s+(?:and|or|for|from|with|after|before|to|of|in|on|via|the|以及|和|与|或|及|的|在|用于|支持))$`)

// chineseConnectorSuffixPattern 中文连接词尾缀。
// Python: _trim_truncated_chinese_connector 的 connector
var chineseConnectorSuffixPattern = regexp.MustCompile(`(?:以及|用于|与|和|或|及|的)$`)

// fenceClosingPattern 围栏闭合行。
var fenceClosingPattern = regexp.MustCompile(`^ {0,3}(` + "`{3,}|~{3,})[ \\t]*$")

// fenceOpeningPattern 围栏开启行。
var fenceOpeningPattern = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")

// inlineCodeRunPattern 行内代码段。
var inlineCodeRunPattern = regexp.MustCompile("`+[^`\r\n]*`+")

// htmlCommentPattern HTML 注释。
var htmlCommentPattern = regexp.MustCompile("(?s)<!--.*?-->")

// readableNonTextPattern 纯数字/符号判定。
// Python: _readable_topic_identity 内 r"[\d\W_]+"
var readableNonTextPattern = regexp.MustCompile(`^[\d\W_]+$`)

// ──────────────────────────── 导出函数 ────────────────────────────

// TitleFor 变更项标题回落逻辑。
// Python: context_pipeline.py:1427 _title_for
func TitleFor(item *RawChangeItem) string {
	title := ""
	if item.Title != nil {
		title = strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(*item.Title))
	}
	if title != "" {
		runes := []rune(title)
		if len(runes) > 512 {
			return string(runes[:512])
		}
		return title
	}
	parts := strings.Split(item.LogicalID, "/")
	fallback := parts[len(parts)-1]
	if fallback == "" {
		return item.LogicalID
	}
	return fallback
}

// MarkdownReferenceTextP 保持 pipeline 内调用的别名（Python 同名引用）。
// Python: context_pipeline.py:7160 _markdown_reference_text → markdown_reference_text
func MarkdownReferenceTextP(markdown string) string {
	return MarkdownReferenceText(markdown)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// managedSourceComment 受管源标记注释。
// Python: context_pipeline.py:1434 _managed_source_comment
func managedSourceComment(sourceID string) (string, error) {
	if !sourceMetadataIDPattern.MatchString(sourceID) {
		return "", pipelineError("managed source ID is invalid")
	}
	return fmt.Sprintf("<!-- personal-context-managed-source: %s -->", sourceID), nil
}

// managedPagesBySource 扫描受管源标记 → 页路径。
// Python: context_pipeline.py:1440 _managed_pages_by_source
func managedPagesBySource(contextRoot string) (map[string]string, error) {
	result := map[string]string{}
	var pages []string
	for _, page := range walkTreePaths(contextRoot) {
		if strings.EqualFold(filepath.Ext(page), ".md") {
			pages = append(pages, page)
		}
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i] < pages[j] })
	for _, page := range pages {
		if strings.EqualFold(filepath.Base(page), "description.md") ||
			pipelinePathIsLinkOrReparse(page) || !pipelinePathIsFile(page) {
			continue
		}
		raw, err := os.ReadFile(extendedPath(page))
		if err != nil {
			return nil, publishError("managed source identity could not be inspected")
		}
		markers := managedSourceMarkerPattern.FindAllStringSubmatch(string(raw), -1)
		if len(markers) > 1 {
			return nil, pipelineError("managed source identity is duplicated")
		}
		if len(markers) == 1 {
			if _, duplicated := result[markers[0][1]]; duplicated {
				return nil, pipelineError("managed source identity is duplicated")
			}
			result[markers[0][1]] = page
		}
	}
	return result, nil
}

// removeRulesPagesForDeletedSourceIDs 删除已删源的受管页。
// Python: context_pipeline.py:1458 _remove_rules_pages_for_deleted_source_ids
func removeRulesPagesForDeletedSourceIDs(contextRoot string, deletedSourceIDs []string) error {
	managedPages, err := managedPagesBySource(contextRoot)
	if err != nil {
		return err
	}
	unique := map[string]struct{}{}
	for _, sourceID := range deletedSourceIDs {
		unique[sourceID] = struct{}{}
	}
	var sortedIDs []string
	for sourceID := range unique {
		sortedIDs = append(sortedIDs, sourceID)
	}
	sort.Strings(sortedIDs)
	for _, sourceID := range sortedIDs {
		if !sourceMetadataIDPattern.MatchString(sourceID) {
			return pipelineError("deleted managed source ID is invalid")
		}
		if page, exists := managedPages[sourceID]; exists {
			if err := removeTreeEntry(page); err != nil {
				return err
			}
		}
	}
	return nil
}

// directoryOrdinaryMarkdownCount 目录普通 Markdown 页计数。
// Python: context_pipeline.py:1472 _directory_ordinary_markdown_count
func directoryOrdinaryMarkdownCount(directory string) (int, error) {
	target := extendedPath(directory)
	if _, err := os.Stat(target); err != nil {
		return 0, nil
	}
	info, err := os.Lstat(target)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return 0, publishError("Context directory capacity target is invalid")
	}
	entries, readErr := os.ReadDir(target)
	if readErr != nil {
		return 0, publishError("Context directory capacity could not be inspected")
	}
	count := 0
	for _, entry := range entries {
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".md") ||
			strings.EqualFold(entry.Name(), "description.md") {
			continue
		}
		child := filepath.Join(directory, entry.Name())
		if !pipelinePathIsFile(child) || pipelinePathIsLinkOrReparse(child) {
			continue
		}
		count++
	}
	return count, nil
}

// defaultDirectoryCapacity 从公共 config 模型读默认值。
// Python: context_pipeline.py:1495 _default_directory_capacity —— Go 侧
// PersonalContextConfig 默认值即 config.go 常量（挂账③锚点）。
func defaultDirectoryCapacity(fieldName string) (int, error) {
	switch fieldName {
	case "max_pages_per_directory":
		return DefaultMaxPagesPerDirectory, nil
	case "max_subdirectories_per_directory":
		return DefaultMaxSubdirectoriesPerDirectory, nil
	}
	return 0, publishError("PersonalContext directory capacity default is invalid")
}

// capacityWarningThreshold 容量告警阈值（80%）。
// Python: context_pipeline.py:1504 _capacity_warning_threshold
func capacityWarningThreshold(limit int) int {
	return int(math.Ceil(float64(limit) * 0.8))
}

// directoryDirectSubdirectoryCount 直接子目录计数。
// Python: context_pipeline.py:1508 _directory_direct_subdirectory_count
func directoryDirectSubdirectoryCount(directory string) (int, error) {
	target := extendedPath(directory)
	if _, err := os.Stat(target); err != nil {
		return 0, nil
	}
	if pipelinePathIsLinkOrReparse(directory) || !pipelinePathIsDir(directory) {
		return 0, publishError("Context directory capacity target is invalid")
	}
	entries, readErr := os.ReadDir(target)
	if readErr != nil {
		return 0, publishError("Context directory capacity could not be inspected")
	}
	count := 0
	for _, entry := range entries {
		child := filepath.Join(directory, entry.Name())
		if pipelinePathIsDir(child) && !pipelinePathIsLinkOrReparse(child) {
			count++
		}
	}
	return count, nil
}

// directoryAcceptsNewPage 页容量闸。
// Python: context_pipeline.py:1520 _directory_accepts_new_page
func directoryAcceptsNewPage(directory string, maxPages int, hasMax bool) (bool, error) {
	if pipelinePathExists(directory) &&
		(pipelinePathIsLinkOrReparse(directory) || !pipelinePathIsDir(directory)) {
		return false, nil
	}
	limit := maxPages
	if !hasMax {
		var err error
		limit, err = defaultDirectoryCapacity("max_pages_per_directory")
		if err != nil {
			return false, err
		}
	}
	count, err := directoryOrdinaryMarkdownCount(directory)
	if err != nil {
		return false, err
	}
	return count < limit, nil
}

// directoryAcceptsNewSubdirectory 子目录容量闸。
// Python: context_pipeline.py:1528 _directory_accepts_new_subdirectory
func directoryAcceptsNewSubdirectory(directory string, maxSubdirectories int, hasMax bool) (bool, error) {
	if pipelinePathExists(directory) &&
		(pipelinePathIsLinkOrReparse(directory) || !pipelinePathIsDir(directory)) {
		return false, nil
	}
	limit := maxSubdirectories
	if !hasMax {
		var err error
		limit, err = defaultDirectoryCapacity("max_subdirectories_per_directory")
		if err != nil {
			return false, err
		}
	}
	count, err := directoryDirectSubdirectoryCount(directory)
	if err != nil {
		return false, err
	}
	return count < limit, nil
}

// validateContextCapacities 全树容量校验。
// Python: context_pipeline.py:1544 _validate_context_capacities
func validateContextCapacities(contextRoot string, maxPages int, maxSubdirectories int, hasLimits bool, repairable bool, capacityExempt bool) error {
	buildErr := publishError
	if repairable {
		buildErr = pipelineError
	}
	_ = buildErr
	if err := assertNoSymlinks(contextRoot); err != nil {
		return err
	}
	if capacityExempt {
		return nil
	}
	pagesLimit := maxPages
	subdirectoriesLimit := maxSubdirectories
	if !hasLimits {
		var err error
		pagesLimit, err = defaultDirectoryCapacity("max_pages_per_directory")
		if err != nil {
			return err
		}
		subdirectoriesLimit, err = defaultDirectoryCapacity("max_subdirectories_per_directory")
		if err != nil {
			return err
		}
	}
	for _, directory := range contextDirectories(contextRoot) {
		pageCount, err := directoryOrdinaryMarkdownCount(directory)
		if err != nil {
			return err
		}
		if pageCount > pagesLimit {
			if repairable {
				return pipelineError("candidate Context directory page capacity is exceeded")
			}
			return publishError("candidate Context directory page capacity is exceeded")
		}
		subdirectoryCount, err := directoryDirectSubdirectoryCount(directory)
		if err != nil {
			return err
		}
		if subdirectoryCount > subdirectoriesLimit {
			if repairable {
				return pipelineError("candidate Context directory subdirectory capacity is exceeded")
			}
			return publishError("candidate Context directory subdirectory capacity is exceeded")
		}
	}
	return nil
}

// contextDirectories 根 + 排序子目录列表。
// Python: context_pipeline.py:3782 _context_directories
func contextDirectories(contextRoot string) []string {
	result := []string{contextRoot}
	var subdirectories []string
	for _, path := range walkTreePaths(contextRoot) {
		if pipelinePathIsDir(path) {
			subdirectories = append(subdirectories, path)
		}
	}
	sort.Slice(subdirectories, func(i, j int) bool {
		ri, _ := filepath.Rel(contextRoot, subdirectories[i])
		rj, _ := filepath.Rel(contextRoot, subdirectories[j])
		return filepath.ToSlash(ri) < filepath.ToSlash(rj)
	})
	return append(result, subdirectories...)
}

// contextOrdinaryPages 排序普通页列表。
// Python: context_pipeline.py:3763 _context_ordinary_pages
func contextOrdinaryPages(contextRoot string) ([]string, error) {
	if err := assertNoSymlinks(contextRoot); err != nil {
		return nil, err
	}
	var ordinaryPages []string
	for _, page := range walkTreePaths(contextRoot) {
		if !strings.EqualFold(filepath.Ext(page), ".md") ||
			strings.EqualFold(filepath.Base(page), "description.md") ||
			!pipelinePathIsFile(page) || pipelinePathIsLinkOrReparse(page) {
			continue
		}
		ordinaryPages = append(ordinaryPages, page)
	}
	sort.Slice(ordinaryPages, func(i, j int) bool {
		ri, _ := filepath.Rel(contextRoot, ordinaryPages[i])
		rj, _ := filepath.Rel(contextRoot, ordinaryPages[j])
		return filepath.ToSlash(ri) < filepath.ToSlash(rj)
	})
	return ordinaryPages, nil
}

// managedBlockBounds 定位受管块（畸形/嵌套报错）。
// Python: context_pipeline.py:1575 _managed_block_bounds
func managedBlockBounds(markdown string, start string, end string) (int, int, error) {
	startPattern := regexp.MustCompile(regexp.QuoteMeta(start))
	endPattern := regexp.MustCompile(regexp.QuoteMeta(end))
	var starts, ends []int
	for _, loc := range startPattern.FindAllStringIndex(markdown, -1) {
		starts = append(starts, loc[0])
	}
	for _, loc := range endPattern.FindAllStringIndex(markdown, -1) {
		ends = append(ends, loc[0])
	}
	if len(starts) == 0 && len(ends) == 0 {
		return 0, 0, nil
	}
	if len(starts) != 1 || len(ends) != 1 || starts[0] >= ends[0] {
		return 0, 0, pipelineError("managed Markdown block markers are malformed")
	}
	blockEnd := ends[0] + len(end)
	blockStart := starts[0]
	nested := personalContextManagedMarkerPattern.FindAllString(markdown[blockStart:blockEnd], -1)
	if len(nested) != 2 || nested[0] != start || nested[1] != end {
		return 0, 0, pipelineError("managed Markdown block markers are nested")
	}
	return blockStart, blockEnd, nil
}

// markdownNewline 首个换行形态。
// Python: context_pipeline.py:1590 _markdown_newline
func markdownNewline(markdown string) string {
	if strings.Contains(markdown, "\r\n") {
		return "\r\n"
	}
	if strings.Contains(markdown, "\n") {
		return "\n"
	}
	return "\n"
}

// normalizeMarkdownNewlines 统一换行形态。
// Python: context_pipeline.py:1595 _normalize_markdown_newlines
func normalizeMarkdownNewlines(value string, newline string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.ReplaceAll(value, "\n", newline)
}

// replaceManagedBlock 替换合法块或在首个 H1 后插入。
// Python: context_pipeline.py:1599 _replace_managed_block
func replaceManagedBlock(markdown string, start string, end string, body string, hasBody bool, defaultHeading string) (string, error) {
	begin, finish, boundsErr := managedBlockBounds(markdown, start, end)
	if boundsErr != nil {
		return "", boundsErr
	}
	hasBounds := !(begin == 0 && finish == 0)
	newline := markdownNewline(markdown)
	normalizedBody := ""
	hasNormalized := false
	if hasBody {
		normalizedBody = strings.TrimRight(normalizeMarkdownNewlines(body, newline), "\r\n")
		hasNormalized = true
	}
	block := ""
	hasBlock := false
	if hasNormalized {
		block = start + newline + normalizedBody + newline + end
		hasBlock = true
	}
	if hasBounds {
		if hasBlock {
			return markdown[:begin] + block + markdown[finish:], nil
		}
		return markdown[:begin] + markdown[finish:], nil
	}
	if !hasBlock {
		return markdown, nil
	}
	if markdown == "" {
		markdown = fmt.Sprintf("# %s%s", defaultHeading, newline)
	}
	headingPattern := regexp.MustCompile(`(?m)^# [^\r\n]+(?:\r?\n|$)`)
	heading := headingPattern.FindStringIndex(markdown)
	if heading == nil {
		return "", pipelineError("managed Markdown file has no top-level heading")
	}
	return markdown[:heading[1]] + newline + block + newline + markdown[heading[1]:], nil
}

// markdownHeading 首个 H1（无则回落）。
// Python: context_pipeline.py:1627 _markdown_heading
func markdownHeading(markdown string, fallback string) string {
	for _, line := range strings.Split(markdown, "\n") {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(line[2:])
		}
	}
	return fallback
}

// markdownLabel 标签归一（方括号替换 + 截断）。
// Python: context_pipeline.py:1632 _markdown_label
func markdownLabel(value string) string {
	normalized := strings.NewReplacer("[", "(", "]", ")", "\r", " ", "\n", " ").
		Replace(value)
	normalized = strings.TrimSpace(normalized)
	runes := []rune(normalized)
	if len(runes) > 512 {
		normalized = string(runes[:512])
	}
	if normalized == "" {
		return "来源"
	}
	return normalized
}

// relativeMarkdownTarget 相对 POSIX 目标。
// Python: context_pipeline.py:1637 _relative_markdown_target
func relativeMarkdownTarget(path string, fromDirectory string) string {
	relative, err := filepath.Rel(fromDirectory, path)
	if err != nil {
		return path
	}
	return strings.ReplaceAll(relative, "\\", "/")
}

// managedLocalLinks 解析描述页受管链接块。
// Python: context_pipeline.py:1641 _managed_local_links
func managedLocalLinks(descriptionPath string, contextRoot string, start string, end string, heading string) (map[string]string, error) {
	raw, err := os.ReadFile(extendedPath(descriptionPath))
	if err != nil {
		return nil, err
	}
	markdown := string(raw)
	begin, finish, boundsErr := managedBlockBounds(markdown, start, end)
	if boundsErr != nil {
		return nil, boundsErr
	}
	if begin == 0 && finish == 0 {
		return map[string]string{}, nil
	}
	block := markdown[begin+len(start) : finish-len(end)]
	result := map[string]string{}
	for _, line := range strings.Split(block, "\n") {
		stripped := strings.TrimSpace(line)
		if stripped == "" || stripped == heading {
			continue
		}
		match := managedLocalLinkPattern.FindStringSubmatch(stripped)
		if match == nil {
			return nil, pipelineError("managed local link block is malformed")
		}
		destination := markdownDestination(match[2])
		if destination == "" || uriSchemePattern.MatchString(destination) {
			return nil, pipelineError("managed local link target is invalid")
		}
		rootAbs, _ := filepath.Abs(contextRoot)
		target := filepath.Join(filepath.Dir(descriptionPath), filepath.FromSlash(destination))
		targetAbs, _ := filepath.Abs(target)
		relative, relErr := filepath.Rel(rootAbs, targetAbs)
		if relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, pipelineError("managed local link escaped Context")
		}
		if pipelinePathIsFile(targetAbs) {
			result[relativeMarkdownTarget(targetAbs, filepath.Dir(descriptionPath))] = match[1]
		}
	}
	return result, nil
}

// appendManagedSourceLink 追加来源关联链接行。
// Python: context_pipeline.py:1680 _append_managed_source_link
func appendManagedSourceLink(descriptionPath string, contextRoot string, sourcePage string, title string) error {
	links, err := managedLocalLinks(descriptionPath, contextRoot, sourceLinksStart, sourceLinksEnd, "## PersonalContext 来源关联")
	if err != nil {
		return err
	}
	target := relativeMarkdownTarget(sourcePage, filepath.Dir(descriptionPath))
	links[target] = markdownLabel(title)
	var paths []string
	for path := range links {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	rows := make([]string, 0, len(paths))
	for _, path := range paths {
		rows = append(rows, fmt.Sprintf("- [%s](%s)", links[path], markdownLinkTarget(path)))
	}
	raw, err := os.ReadFile(extendedPath(descriptionPath))
	if err != nil {
		return err
	}
	updated, err := replaceManagedBlock(string(raw), sourceLinksStart, sourceLinksEnd,
		"## PersonalContext 来源关联\n\n"+strings.Join(rows, "\n"), true, filepath.Base(filepath.Dir(descriptionPath)))
	if err != nil {
		return err
	}
	return pipelineAtomicWrite(descriptionPath, []byte(updated))
}

// appendManagedTopicLink 追加受管主题链接行。
// Python: context_pipeline.py:1696 _append_managed_topic_link
func appendManagedTopicLink(contextRoot string, topicDirectory string, title string) error {
	topicsRoot := filepath.Join(contextRoot, "topics")
	descriptionPath := filepath.Join(topicsRoot, "description.md")
	if !pipelinePathIsFile(descriptionPath) {
		if err := pipelineAtomicWrite(descriptionPath, []byte("# 主题导航\n")); err != nil {
			return err
		}
	}
	links, err := managedLocalLinks(descriptionPath, contextRoot, topicLinksStart, topicLinksEnd, "## PersonalContext 受控主题")
	if err != nil {
		return err
	}
	targetPath := filepath.Join(topicDirectory, "description.md")
	target := relativeMarkdownTarget(targetPath, filepath.Dir(descriptionPath))
	links[target] = markdownLabel(title)
	var paths []string
	for path := range links {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	rows := make([]string, 0, len(paths))
	for _, path := range paths {
		rows = append(rows, fmt.Sprintf("- [%s](%s)", links[path], markdownLinkTarget(path)))
	}
	raw, err := os.ReadFile(extendedPath(descriptionPath))
	if err != nil {
		return err
	}
	updated, err := replaceManagedBlock(string(raw), topicLinksStart, topicLinksEnd,
		"## PersonalContext 受控主题\n\n"+strings.Join(rows, "\n"), true, "主题导航")
	if err != nil {
		return err
	}
	return pipelineAtomicWrite(descriptionPath, []byte(updated))
}

// normalizedTopicTerm 主题词归一（过短/泛名 → 空）。
// Python: context_pipeline.py:1728 _normalized_topic_term
func normalizedTopicTerm(value string) string {
	term := regexp.MustCompile("[`*_#]").ReplaceAllString(value, "")
	term = strings.TrimSpace(term)
	compact := regexp.MustCompile(`\s+`).ReplaceAllString(term, "")
	if len([]rune(compact)) < 4 {
		return ""
	}
	if _, generic := genericTopicNames[strings.ToLower(term)]; generic {
		return ""
	}
	return term
}

// topicTermMatchesTitle 主题词与标题匹配（CJK 包含 / ascii 词边界）。
// Python: context_pipeline.py:1746 _topic_term_matches_title —— RE2 无 lookahead，
// ascii 词边界手写扫描。
func topicTermMatchesTitle(term string, title string) bool {
	if cjkCharPattern.MatchString(term) {
		return strings.Contains(strings.ToLower(title), strings.ToLower(term))
	}
	termLower := strings.ToLower(term)
	titleLower := strings.ToLower(title)
	for index := 0; index+len(termLower) <= len(titleLower); index++ {
		if titleLower[index:index+len(termLower)] != termLower {
			continue
		}
		if index > 0 && isAlnumByte(titleLower[index-1]) {
			continue
		}
		if index+len(termLower) < len(titleLower) && isAlnumByte(titleLower[index+len(termLower)]) {
			continue
		}
		return true
	}
	return false
}

// isAlnumByte ascii 字母数字。
func isAlnumByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

// uniqueRulesTopicDescription 唯一匹配主题描述目录。
// Python: context_pipeline.py:1753 _unique_rules_topic_description
func uniqueRulesTopicDescription(contextRoot string, title string) string {
	matches := map[string]struct{}{}
	for _, description := range walkTreePaths(contextRoot) {
		if filepath.Base(description) != "description.md" {
			continue
		}
		relative, relErr := filepath.Rel(contextRoot, description)
		if relErr != nil {
			continue
		}
		if filepath.Dir(description) == contextRoot ||
			strings.HasPrefix(filepath.ToSlash(relative), "sources/") {
			continue
		}
		raw, err := os.ReadFile(description)
		if err != nil {
			continue
		}
		values := []string{filepath.Base(filepath.Dir(description)), markdownHeading(string(raw), "")}
		matched := false
		for _, value := range values {
			term := normalizedTopicTerm(value)
			if term != "" && topicTermMatchesTitle(term, title) {
				matched = true
				break
			}
		}
		if matched {
			matches[description] = struct{}{}
		}
	}
	if len(matches) == 1 {
		for match := range matches {
			return match
		}
	}
	return ""
}

// documentSemanticParts 文档语义三元组（含 balanced 富集优先）。
// Python: context_pipeline.py:1781 _document_semantic_parts
func documentSemanticParts(document map[string]any) SemanticRecord {
	titleSource := document["title"]
	if titleSource == nil {
		titleSource = document["logical_id"]
	}
	titleText, _ := titleSource.(string)
	if titleText == "" {
		titleText = "来源"
	}
	title := markdownLabel(titleText)
	markdownSource, _ := document["markdown"].(string)
	markdown := strings.TrimSpace(shortReferencePattern.ReplaceAllString(markdownSource, ""))
	briefing := DeterministicBriefingPreview(markdown)
	var headings []string
	if headingValues, ok := briefing["headings"].([]map[string]any); ok {
		for _, heading := range headingValues {
			if text, ok := heading["text"].(string); ok && text != "" {
				headings = append(headings, text)
			}
		}
	}
	if enrichment, ok := document["_balanced_semantics"].(map[string]any); ok {
		pageTitle, _ := enrichment["page_title"].(string)
		keywords, _ := enrichment["keywords"].([]string)
		summary, _ := enrichment["summary"].(string)
		return SemanticRecord{Title: pageTitle, Headings: append(headings, keywords...), Preview: summary}
	}
	summary, _ := briefing["summary"].(string)
	return SemanticRecord{Title: title, Headings: headings, Preview: strings.TrimSpace(summary)}
}

// directorySemanticParts 目录语义三元组。
// Python: context_pipeline.py:1795 _directory_semantic_parts
func directorySemanticParts(directory string, excludePage string) SemanticRecord {
	var headings []string
	var previews []string
	description := filepath.Join(directory, "description.md")
	if pipelinePathIsFile(description) && !pipelinePathIsLinkOrReparse(description) {
		raw, _ := os.ReadFile(extendedPath(description))
		markdown := string(raw)
		if strings.Contains(markdown, directoryDescriptionStart) {
			markdown = regexp.MustCompile(`(?m)^# [^\r\n]+`).ReplaceAllString(markdown, "")
		}
		markdown = sanitizedSemanticMarkdown(markdown)
		headings = append(headings, markdownHeading(markdown, filepath.Base(directory)))
		previewMap := DeterministicBriefingPreview(markdown)
		summary, _ := previewMap["summary"].(string)
		previews = append(previews, summary)
	}
	entries, err := os.ReadDir(extendedPath(directory))
	if err != nil {
		return SemanticRecord{Title: filepath.Base(directory), Headings: headings, Preview: strings.Join(previews, "\n")}
	}
	var directEntries []string
	for _, entry := range entries {
		directEntries = append(directEntries, filepath.Join(directory, entry.Name()))
	}
	sort.Slice(directEntries, func(i, j int) bool { return directEntries[i] < directEntries[j] })
	for _, page := range directEntries {
		if !strings.EqualFold(filepath.Ext(page), ".md") {
			continue
		}
		if page == excludePage ||
			strings.EqualFold(filepath.Base(page), "description.md") ||
			pipelinePathIsLinkOrReparse(page) || !pipelinePathIsFile(page) {
			continue
		}
		raw, _ := os.ReadFile(extendedPath(page))
		markdown := string(raw)
		headings = append(headings, markdownHeading(markdown, strings.TrimSuffix(filepath.Base(page), filepath.Ext(page))))
		previewMap := DeterministicBriefingPreview(markdown)
		summary, _ := previewMap["summary"].(string)
		previews = append(previews, summary)
	}
	return SemanticRecord{Title: filepath.Base(directory), Headings: headings, Preview: strings.Join(previews, "\n")}
}

// directorySemanticFields 目录语义字段向量。
// Python: context_pipeline.py:1815 _directory_semantic_fields
func directorySemanticFields(directory string) map[string]float64 {
	parts := directorySemanticParts(directory, "")
	return SemanticFields(parts.Title, parts.Headings, parts.Preview)
}

// directorySemanticText 目录嵌入文本。
// Python: context_pipeline.py:1819 _directory_semantic_text
func directorySemanticText(directory string) string {
	parts := directorySemanticParts(directory, "")
	return SemanticEmbeddingText(parts.Title, parts.Headings, parts.Preview)
}

// semanticContextDirectories 含普通页的非保留目录。
// Python: context_pipeline.py:1825 _semantic_context_directories
func semanticContextDirectories(contextRoot string) []string {
	var result []string
	directories := contextDirectories(contextRoot)
	for _, directory := range directories[1:] {
		relative, relErr := filepath.Rel(contextRoot, directory)
		if relErr != nil {
			continue
		}
		parts := strings.Split(filepath.ToSlash(relative), "/")
		reserved := false
		for _, part := range parts {
			if _, isReserved := reservedContextSegments[strings.ToLower(part)]; isReserved {
				reserved = true
				break
			}
		}
		if reserved {
			continue
		}
		entries, err := os.ReadDir(extendedPath(directory))
		if err != nil {
			continue
		}
		hasMarkdown := false
		for _, entry := range entries {
			if strings.EqualFold(filepath.Ext(entry.Name()), ".md") && pipelinePathIsFile(filepath.Join(directory, entry.Name())) {
				hasMarkdown = true
				break
			}
		}
		if hasMarkdown {
			result = append(result, directory)
		}
	}
	return result
}

// sanitizedSemanticMarkdown 剥离程序块与代码后供语义抽取。
// Python: context_pipeline.py:3986 _sanitized_semantic_markdown
func sanitizedSemanticMarkdown(markdown string) string {
	semanticMarkdown := markdown
	for _, pair := range [][2]string{
		{directoryDescriptionStart, directoryDescriptionEnd},
		{rootNavigationStart, rootNavigationEnd},
		{directoryOverviewStart, directoryOverviewEnd},
		{sourceLinksStart, sourceLinksEnd},
		{topicLinksStart, topicLinksEnd},
		{relatedStart, relatedEnd},
	} {
		begin, finish, boundsErr := managedBlockBounds(semanticMarkdown, pair[0], pair[1])
		if boundsErr == nil && (begin > 0 || finish > 0) {
			semanticMarkdown = semanticMarkdown[:begin] + semanticMarkdown[finish:]
		}
	}
	semanticMarkdown = strings.ReplaceAll(semanticMarkdown, emptyDeterministicPreview, " ")
	var semanticLines []string
	fenceCharacter := byte(0)
	fenceLength := 0
	for _, line := range strings.Split(semanticMarkdown, "\n") {
		if fenceCharacter != 0 {
			closing := fenceClosingPattern.FindStringSubmatch(line)
			if closing != nil && closing[1][0] == fenceCharacter && len(closing[1]) >= fenceLength {
				fenceCharacter = 0
				fenceLength = 0
			}
			continue
		}
		opening := fenceOpeningPattern.FindStringSubmatch(line)
		if opening != nil {
			fenceCharacter = opening[1][0]
			fenceLength = len(opening[1])
			continue
		}
		semanticLines = append(semanticLines, inlineCodeRunPattern.ReplaceAllString(line, " "))
	}
	return strings.Join(semanticLines, "\n")
}

// semanticMarkdownParts 语义标题与预览。
// Python: context_pipeline.py:4019 _semantic_markdown_parts
func semanticMarkdownParts(markdown string) ([][2]string, string) {
	semanticMarkdown := sanitizedSemanticMarkdown(markdown)
	var headings [][2]string
	for _, line := range strings.Split(semanticMarkdown, "\n") {
		match := briefingHeadingPattern.FindStringSubmatch(line)
		if match != nil {
			headings = append(headings, [2]string{fmt.Sprint(len(match[1])), strings.TrimSpace(match[2])})
		}
	}
	previewMarkdown := htmlCommentPattern.ReplaceAllString(semanticMarkdown, " ")
	previewMarkdown = markdownInlineLinkPattern.ReplaceAllString(previewMarkdown, " ")
	var kept []string
	for _, line := range strings.Split(previewMarkdown, "\n") {
		if briefingHeadingPattern.FindStringSubmatch(line) == nil {
			kept = append(kept, line)
		}
	}
	previewMarkdown = strings.Join(kept, "\n")
	previewMap := DeterministicBriefingPreview(previewMarkdown)
	preview, _ := previewMap["summary"].(string)
	return headings, strings.TrimSpace(preview)
}

// stripKnownSourceTitleExtension 剥离已知源标题扩展。
// Python: context_pipeline.py:6942 _strip_known_source_title_extension
func stripKnownSourceTitleExtension(value string) string {
	folded := strings.ToLower(value)
	for _, extension := range knownSourceTitleExtensions {
		if strings.HasSuffix(folded, extension) && len([]rune(value)) > len(extension) {
			return strings.TrimRight(string([]rune(value)[:len([]rune(value))-len(extension)]), semanticTrim)
		}
	}
	return value
}

// removeUnbalancedBracketSuffix 截掉未平衡括号后缀。
// Python: context_pipeline.py:6950 _remove_unbalanced_bracket_suffix
func removeUnbalancedBracketSuffix(value string) string {
	pairs := map[rune]rune{'(': ')', '（': '）', '[': ']', '【': '】', '<': '>', '《': '》'}
	closings := map[rune]struct{}{}
	for _, closing := range pairs {
		closings[closing] = struct{}{}
	}
	var stack []rune
	runes := []rune(value)
	for index, character := range runes {
		if closing, isOpening := pairs[character]; isOpening {
			_ = closing
			stack = append(stack, pairs[character])
		} else if _, isClosing := closings[character]; isClosing {
			if len(stack) == 0 || stack[len(stack)-1] != character {
				return strings.TrimRight(string(runes[:index]), semanticTrim)
			}
			stack = stack[:len(stack)-1]
		}
	}
	if len(stack) > 0 {
		unmatchedAt := -1
		for opening, closing := range pairs {
			for _, pending := range stack {
				if pending == closing {
					pos := strings.IndexRune(value, opening)
					if pos >= 0 && (unmatchedAt < 0 || pos < unmatchedAt) {
						unmatchedAt = pos
					}
				}
			}
		}
		if unmatchedAt < 0 {
			unmatchedAt = 0
		}
		return strings.TrimRight(string(runes[:unmatchedAt]), semanticTrim)
	}
	return strings.TrimRight(value, semanticTrim)
}

// semanticBoundaryPositions 语义边界（自然边界 + 驼峰）。
// Python: context_pipeline.py:6967 _semantic_boundary_positions
func semanticBoundaryPositions(value string) ([]int, []int) {
	naturalSet := map[int]struct{}{}
	runes := []rune(value)
	for index, character := range runes {
		if index > 0 && strings.ContainsRune(semanticBoundaries, character) {
			naturalSet[index] = struct{}{}
		}
	}
	var camelSet []int
	for index := 1; index < len(runes); index++ {
		previous := runes[index-1]
		current := runes[index]
		if (unicode.IsLower(previous) || (previous >= '0' && previous <= '9')) && unicode.IsUpper(current) {
			camelSet = append(camelSet, index)
		}
	}
	var natural []int
	for position := range naturalSet {
		natural = append(natural, position)
	}
	sort.Ints(natural)
	sort.Ints(camelSet)
	return natural, camelSet
}

// trimSemanticConnector 剥离语义连接词尾缀。
// Python: context_pipeline.py:6980 _trim_semantic_connector
func trimSemanticConnector(value string) string {
	result := strings.TrimRight(value, semanticTrim)
	for {
		shortened := strings.TrimRight(connectorSuffixPattern.ReplaceAllString(result, ""), semanticTrim)
		if shortened == result {
			return result
		}
		result = shortened
	}
}

// trimTruncatedChineseConnector 剥离截断后暴露的中文连接词。
// Python: context_pipeline.py:6990 _trim_truncated_chinese_connector
func trimTruncatedChineseConnector(value string) string {
	result := trimSemanticConnector(value)
	for {
		shortened := strings.TrimRight(chineseConnectorSuffixPattern.ReplaceAllString(result, ""), semanticTrim)
		if shortened == result {
			return result
		}
		result = shortened
	}
}

// semanticPrefix 语义前缀截断（自然边界/驼峰/省略号）。
// Python: context_pipeline.py:7007 _semantic_prefix
func semanticPrefix(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	balanced := removeUnbalancedBracketSuffix(value)
	if len([]rune(balanced)) <= limit {
		return trimSemanticConnector(balanced)
	}
	ellipsis := "…"
	// Python len("…") == 1（按字符计）；Go len 按字节，必须取 rune 数。
	ellipsisLen := len([]rune(ellipsis))
	naturalBoundaries, camelBoundaries := semanticBoundaryPositions(balanced)
	var natural []int
	for _, position := range naturalBoundaries {
		if position <= limit {
			natural = append(natural, position)
		}
	}
	if len(natural) > 0 {
		candidate := trimTruncatedChineseConnector(string([]rune(balanced)[:natural[len(natural)-1]]))
		if candidate != "" {
			return candidate
		}
	}
	boundaryLimit := limit - ellipsisLen
	if boundaryLimit < 0 {
		boundaryLimit = 0
	}
	var camel []int
	for _, position := range camelBoundaries {
		if position <= boundaryLimit {
			camel = append(camel, position)
		}
	}
	if len(camel) > 0 {
		candidate := trimTruncatedChineseConnector(string([]rune(balanced)[:camel[len(camel)-1]]))
		if candidate != "" {
			return candidate + ellipsis
		}
	}
	runes := []rune(balanced)
	candidate := trimTruncatedChineseConnector(string(runes[:boundaryLimit]))
	if candidate == "" {
		candidate = strings.TrimRight(string(runes[:boundaryLimit]), semanticTrim)
	}
	return candidate + ellipsis
}

// truncateSemanticContextSegment 语义段截断（后缀保形）。
// Python: context_pipeline.py:7030 _truncate_semantic_context_segment
func truncateSemanticContextSegment(value string, suffix string) (string, error) {
	ending := ""
	if suffix != "" {
		ending = "-" + suffix
	}
	bodyLimit := semanticNameMaxChars - len([]rune(ending))
	if bodyLimit <= 0 {
		return "", pipelineError("semantic Context name suffix exceeds the safety limit")
	}
	body := semanticPrefix(value, bodyLimit)
	if body == "" {
		body = semanticPrefix("主题", bodyLimit)
	}
	return body + ending, nil
}

// safeSemanticName 语义名安全化（约定式提交/非法字符/回落摘要）。
// Python: context_pipeline.py:7039 _safe_semantic_name
func safeSemanticName(title string) (string, error) {
	normalized := strings.Join(strings.Fields(norm.NFKC.String(strings.TrimSpace(title))), " ")
	normalized = stripKnownSourceTitleExtension(normalized)
	if conventional := conventionalCommitPattern.FindStringSubmatch(normalized); conventional != nil {
		normalized = strings.TrimSpace(conventional[2])
		normalized = stripKnownSourceTitleExtension(normalized)
	}
	var characters []rune
	for _, character := range normalized {
		unsafe := strings.ContainsRune(PortableForbidden, character) ||
			unicode.Is(unicode.Cc, character) || unicode.Is(unicode.Cf, character)
		replacement := character
		if unsafe {
			replacement = '-'
		}
		if replacement == '-' && len(characters) > 0 && characters[len(characters)-1] == '-' {
			continue
		}
		characters = append(characters, replacement)
	}
	cleaned := removeUnbalancedBracketSuffix(strings.Trim(strings.TrimSpace(string(characters)), " .-"))
	if cleaned == "" {
		return fmt.Sprintf("主题-%s", Digest(title)[:12]), nil
	}
	candidate, err := truncateSemanticContextSegment(cleaned, "")
	if err != nil {
		return "", err
	}
	if PortableContextSegmentIsSafe(candidate) {
		return candidate, nil
	}
	return fmt.Sprintf("主题-%s", Digest(title)[:12]), nil
}

// semanticTopicMatchesTitle 描述页标题与主题匹配。
// Python: context_pipeline.py:7107 _semantic_topic_matches_title
func semanticTopicMatchesTitle(description string, title string) bool {
	if pipelinePathIsLinkOrReparse(description) || !pipelinePathIsFile(description) {
		return false
	}
	raw, err := os.ReadFile(extendedPath(description))
	if err != nil {
		return false
	}
	existingTitle := markdownHeading(string(raw), "")
	return strings.ToLower(normalizedBalancedTopicTitle(existingTitle)) ==
		strings.ToLower(normalizedBalancedTopicTitle(title))
}

// normalizedBalancedTopicTitle 主题标题归一。
// Python: context_pipeline.py:7103 _normalized_balanced_topic_title
func normalizedBalancedTopicTitle(title string) string {
	return strings.ToLower(strings.Join(strings.Fields(norm.NFKC.String(markdownLabel(title))), " "))
}

// markdownLinkTarget 本地 Markdown 目标渲染（空格/括号加尖括号）。
// Python: context_pipeline.py:7155 _markdown_link_target
func markdownLinkTarget(relative string) string {
	for _, character := range relative {
		if unicode.IsSpace(character) || character == '(' || character == ')' {
			return "<" + relative + ">"
		}
	}
	return relative
}

// markdownDestination 解析行内 Markdown 目的地（剥片段与标题）。
// Python: context_pipeline.py:7166 _markdown_destination
func markdownDestination(rawTarget string) string {
	target := strings.TrimSpace(rawTarget)
	if target == "" || strings.HasPrefix(target, "#") {
		return ""
	}
	var destination string
	if strings.HasPrefix(target, "<") {
		match := regexp.MustCompile(`<([^<>]+)>(?:\s+(?:"[^"]*"|'[^']*'|\([^)]*\)))?$`).FindStringSubmatch(target)
		if match == nil {
			return ""
		}
		destination = match[1]
	} else {
		match := regexp.MustCompile(`(\S+)(?:\s+(?:"[^"]*"|'[^']*'|\([^)]*\)))?$`).FindStringSubmatch(target)
		if match == nil {
			return ""
		}
		destination = match[1]
	}
	destination = regexp.MustCompile("[?#]").Split(destination, 2)[0]
	return strings.TrimSpace(destination)
}

// semanticNameMaxChars 语义名上限（pathSafety 常量别名）。
// Python: context_pipeline.py:44 import SEMANTIC_NAME_MAX_CHARS
const semanticNameMaxChars = SemanticNameMaxChars

// semanticEquivalentChildren NFC 大小写等价兄弟目录。
// Python: context_pipeline.py:1962 _semantic_equivalent_children
func semanticEquivalentChildren(parent string, name string) ([]string, error) {
	if !pipelinePathExists(parent) {
		return nil, nil
	}
	if pipelinePathIsLinkOrReparse(parent) || !pipelinePathIsDir(parent) {
		return nil, pipelineError("semantic Context parent directory is invalid")
	}
	key := strings.ToLower(norm.NFC.String(name))
	entries, err := os.ReadDir(extendedPath(parent))
	if err != nil {
		return nil, pipelineError("semantic Context directory siblings could not be inspected")
	}
	var matches []string
	for _, entry := range entries {
		if strings.ToLower(norm.NFC.String(entry.Name())) == key {
			matches = append(matches, filepath.Join(parent, entry.Name()))
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		ni, nj := filepath.Base(matches[i]), filepath.Base(matches[j])
		if strings.ToLower(ni) != strings.ToLower(nj) {
			return strings.ToLower(ni) < strings.ToLower(nj)
		}
		return ni < nj
	})
	return matches, nil
}

// semanticDirectoryCandidate 冲突消解的目录候选。
// Python: context_pipeline.py:1978 _semantic_directory_candidate
func semanticDirectoryCandidate(parent string, title string, seed string) (string, error) {
	base, err := safeSemanticName(title)
	if err != nil {
		return "", err
	}
	// Python seed[:N] 越界自动钳制；Go 显式钳制。
	clampSeed := func(n int) string {
		if n > len(seed) {
			return seed
		}
		return seed[:n]
	}
	suffixes := []string{"", clampSeed(8), clampSeed(12), clampSeed(16)}
	for _, suffix := range suffixes {
		name := base
		if suffix != "" {
			name, err = truncateSemanticContextSegment(base, suffix)
			if err != nil {
				return "", err
			}
		}
		candidate := filepath.Join(parent, name)
		equivalent, eqErr := semanticEquivalentChildren(parent, name)
		if eqErr != nil {
			return "", eqErr
		}
		if len(equivalent) == 0 {
			return candidate, nil
		}
		candidateIsAvailable := len(equivalent) == 1 && pipelinePathIsDir(equivalent[0]) &&
			!pipelinePathIsLinkOrReparse(equivalent[0])
		if candidateIsAvailable &&
			semanticTopicMatchesTitle(filepath.Join(equivalent[0], "description.md"), title) {
			return equivalent[0], nil
		}
	}
	return "", pipelineError("semantic Context directory conflicts with an existing path")
}
