package recall

import (
	"math"
	"regexp"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BM25Index 固定文档集合上的不可变 BM25 索引。
// 构建后不再修改；元数据变化时整体替换实例。
// Python: recall/bm25.py (BM25Index)
type BM25Index struct {
	// tokenized 分词后的文档集合
	tokenized [][]string
	// documentFrequency 词 → 含该词的文档数
	documentFrequency map[string]int
	// documentCount 文档数
	documentCount int
	// averageLength 文档平均长度
	averageLength float64
	// k1 BM25 饱和参数
	k1 float64
	// b BM25 长度归一参数
	b float64
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultK1 BM25 默认饱和参数
	DefaultK1 = 1.5
	// DefaultB BM25 默认长度归一参数
	DefaultB = 0.75
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// asciiTokenRe ASCII 标识符分词
	asciiTokenRe = regexp.MustCompile(`[a-z0-9_]+`)
	// cjkSequenceRe CJK 连续片段
	cjkSequenceRe = regexp.MustCompile(`[\x{3400}-\x{4dbf}\x{4e00}-\x{9fff}]+`)
	// yamlFrontmatterRe 匹配 YAML frontmatter 区块（首尾 --- 之间）
	yamlFrontmatterRe = regexp.MustCompile(`(?s)\A---\s*\n.*?\n---\s*(?:\n|\z)`)
	// markdownFenceRe 代码围栏行
	markdownFenceRe = regexp.MustCompile(`(?m)^\s*\x60\x60\x60[^\n]*$`)
	// markdownHeadingRe 标题标记
	markdownHeadingRe = regexp.MustCompile(`(?m)^\s{0,3}#{1,6}\s*`)
	// markdownListRe 列表标记
	markdownListRe = regexp.MustCompile(`(?m)^\s*(?:[-+*]|\d+[.)])\s+`)
	// markdownLinkRe 链接与图片
	markdownLinkRe = regexp.MustCompile(`!?\[([^\]]*)\]\(([^)]+)\)`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeMarkdown 去掉 Markdown 语法但保留可见文本与代码内容。
// Python: normalize_markdown
func NormalizeMarkdown(text string) string {
	normalized := yamlFrontmatterRe.ReplaceAllString(text, "")
	normalized = markdownFenceRe.ReplaceAllString(normalized, "")
	normalized = markdownLinkRe.ReplaceAllString(normalized, "$1 $2")
	normalized = markdownHeadingRe.ReplaceAllString(normalized, "")
	normalized = markdownListRe.ReplaceAllString(normalized, "")
	return strings.Join(strings.Fields(normalized), " ")
}

// Tokenize 对中英混排、路径与代码标识符分词。
// Python: tokenize
func Tokenize(text string) []string {
	return tokenize(text)
}

// BuildBM25Index 构建索引。
// Python: BM25Index.build
func BuildBM25Index(documents []string, k1 float64, b float64) *BM25Index {
	tokenized := make([][]string, len(documents))
	documentFrequency := make(map[string]int)
	for i, document := range documents {
		tokens := tokenize(document)
		tokenized[i] = tokens
		seen := make(map[string]struct{}, len(tokens))
		for _, token := range tokens {
			if _, ok := seen[token]; ok {
				continue
			}
			seen[token] = struct{}{}
			documentFrequency[token]++
		}
	}
	index := &BM25Index{
		tokenized:         tokenized,
		documentFrequency: documentFrequency,
		documentCount:     len(tokenized),
		k1:                k1,
		b:                 b,
	}
	if index.documentCount > 0 {
		total := 0
		for _, tokens := range tokenized {
			total += len(tokens)
		}
		index.averageLength = float64(total) / float64(index.documentCount)
	}
	return index
}

// RankBM25 对一组文档一次性打分。
// Python: rank_bm25
func RankBM25(query string, documents []string, k1 float64, b float64) []float64 {
	return BuildBM25Index(documents, k1, b).Scores(query)
}

// DocumentCount 返回索引文档数。
// Python: BM25Index.document_count
func (idx *BM25Index) DocumentCount() int { return idx.documentCount }

// Scores 返回每个文档一个 BM25 分数。
// Python: BM25Index.scores
func (idx *BM25Index) Scores(query string) []float64 {
	if idx.documentCount == 0 {
		return []float64{}
	}
	queryTokens := tokenize(query)
	if len(queryTokens) == 0 || idx.averageLength <= 0 {
		return make([]float64, idx.documentCount)
	}
	uniqueQueryTokens := make(map[string]struct{}, len(queryTokens))
	for _, token := range queryTokens {
		uniqueQueryTokens[token] = struct{}{}
	}
	scores := make([]float64, 0, idx.documentCount)
	for _, document := range idx.tokenized {
		frequencies := make(map[string]int, len(document))
		for _, token := range document {
			frequencies[token]++
		}
		documentLength := float64(len(document))
		score := 0.0
		for token := range uniqueQueryTokens {
			frequency := frequencies[token]
			if frequency <= 0 {
				continue
			}
			documentFrequency := idx.documentFrequency[token]
			inverseDocumentFrequency := math.Log(
				1.0 + (float64(idx.documentCount-documentFrequency)+0.5)/(float64(documentFrequency)+0.5),
			)
			denominator := float64(frequency) + idx.k1*(1.0-idx.b+idx.b*documentLength/idx.averageLength)
			score += inverseDocumentFrequency * float64(frequency) * (idx.k1 + 1.0) / denominator
		}
		scores = append(scores, score)
	}
	return scores
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// tokenize 小写化后切 ASCII 标识符与 CJK 片段。
// 下划线拆分子词；CJK 片段整体保留并追加双字滑窗。
// Python: tokenize
func tokenize(text string) []string {
	lowered := strings.ToLower(text)
	tokens := asciiTokenRe.FindAllString(lowered, -1)
	expanded := make([]string, 0, len(tokens)*2)
	expanded = append(expanded, tokens...)
	for _, token := range tokens {
		if !strings.Contains(token, "_") {
			continue
		}
		for _, part := range strings.Split(token, "_") {
			if part != "" {
				expanded = append(expanded, part)
			}
		}
	}
	for _, sequence := range cjkSequenceRe.FindAllString(lowered, -1) {
		expanded = append(expanded, sequence)
		runes := []rune(sequence)
		for i := 0; i+1 < len(runes); i++ {
			expanded = append(expanded, string(runes[i:i+2]))
		}
	}
	return expanded
}
