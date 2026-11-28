package personal_context

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

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
