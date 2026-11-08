package tool_discovery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor/compressor/recall"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BM25ToolIndex 模型可见工具 schema 上的内存 BM25 索引。
// 构建后不可变；元数据变化时整体替换实例，因此搜索无需加锁。
// Python: harness/tools/tool_discovery/bm25.py (BM25ToolIndex)
type BM25ToolIndex struct {
	// documents 文档集合（与 index 顺序一致）
	documents []cschema.ToolInfoInterface
	// index BM25 索引
	index *recall.BM25Index
	// sourceFingerprint 建立索引时的元数据指纹
	sourceFingerprint string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildBM25ToolIndex 构建索引；extraSearchTexts 与 documents 按下标对齐。
// Python: BM25ToolIndex.build
func BuildBM25ToolIndex(documents []cschema.ToolInfoInterface, extraSearchTexts []string) *BM25ToolIndex {
	texts := make([]string, len(documents))
	for i, document := range documents {
		extra := ""
		if extraSearchTexts != nil && i < len(extraSearchTexts) {
			extra = extraSearchTexts[i]
		}
		texts[i] = strings.TrimSpace(toolText(document) + " " + extra)
	}
	return &BM25ToolIndex{
		documents:         documents,
		index:             recall.BuildBM25Index(texts, recall.DefaultK1, recall.DefaultB),
		sourceFingerprint: Fingerprint(documents, extraSearchTexts),
	}
}

// Fingerprint 返回完整文档集合的确定性指纹。
// Python: BM25ToolIndex.fingerprint
func Fingerprint(documents []cschema.ToolInfoInterface, extraSearchTexts []string) string {
	pairs := make([]string, 0, len(documents))
	for i, document := range documents {
		extra := ""
		if extraSearchTexts != nil && i < len(extraSearchTexts) {
			extra = extraSearchTexts[i]
		}
		pairs = append(pairs, ToolFingerprint(document)+"\x00"+extra)
	}
	sort.Strings(pairs)
	return hashStrings(pairs)
}

// ToolFingerprint 返回单个工具记录的确定性指纹。
// Python: BM25ToolIndex.tool_fingerprint
func ToolFingerprint(document cschema.ToolInfoInterface) string {
	return hashStrings([]string{
		document.GetType(),
		document.GetName(),
		document.GetDescription(),
		parametersText(document.GetParameters()),
	})
}

// DocumentCount 返回索引文档数。
// Python: BM25ToolIndex.document_count
func (idx *BM25ToolIndex) DocumentCount() int { return idx.index.DocumentCount() }

// SourceFingerprint 返回建立索引时的元数据指纹。
// Python: BM25ToolIndex.source_fingerprint
func (idx *BM25ToolIndex) SourceFingerprint() string { return idx.sourceFingerprint }

// Documents 返回索引持有的文档副本。
// Python: BM25ToolIndex.documents
func (idx *BM25ToolIndex) Documents() []cschema.ToolInfoInterface {
	return append([]cschema.ToolInfoInterface(nil), idx.documents...)
}

// Search 检索：分数为正或精确名命中的文档，按
// (非精确名, -分数, 名称, 原始序) 排序后截取 limit。
// Python: BM25ToolIndex.search
func (idx *BM25ToolIndex) Search(query string, limit int) []cschema.ToolInfoInterface {
	if limit <= 0 || query == "" || len(idx.documents) == 0 {
		return nil
	}
	scores := idx.index.Scores(query)
	candidates := make([]toolCandidate, 0, len(scores))
	for i, score := range scores {
		document := idx.documents[i]
		exact := containsExactToolName(query, document.GetName())
		if score <= 0 && !exact {
			continue
		}
		candidates = append(candidates, toolCandidate{
			exactMatch: exact,
			score:      score,
			order:      i,
			document:   document,
		})
	}
	sort.SliceStable(candidates, func(a, b int) bool {
		left, right := candidates[a], candidates[b]
		if left.exactMatch != right.exactMatch {
			return left.exactMatch
		}
		if left.score != right.score {
			return left.score > right.score
		}
		if left.document.GetName() != right.document.GetName() {
			return left.document.GetName() < right.document.GetName()
		}
		return left.order < right.order
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	result := make([]cschema.ToolInfoInterface, 0, len(candidates))
	for _, item := range candidates {
		result = append(result, item.document)
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toolCandidate 检索排序的中间结构。
type toolCandidate struct {
	// exactMatch 是否精确名命中
	exactMatch bool
	// score BM25 分数
	score float64
	// order 原始顺序（稳定排序兜底）
	order int
	// document 工具记录
	document cschema.ToolInfoInterface
}

// toolText 拼出参与检索的工具文本。
// Python: _tool_text
func toolText(document cschema.ToolInfoInterface) string {
	return strings.Join([]string{
		document.GetName(),
		document.GetDescription(),
		parametersText(document.GetParameters()),
	}, " ")
}

// parametersText 序列化参数 schema。
// Python: _parameters_text（dict 走 json.dumps(sort_keys=True)）
func parametersText(parameters map[string]any) string {
	if parameters == nil {
		return ""
	}
	payload, err := json.Marshal(parameters)
	if err != nil {
		return ""
	}
	return string(payload)
}

// hashStrings 对字符串列表做稳定哈希。
// Python: hashlib.sha256(json.dumps(...).encode("utf-8")).hexdigest()
func hashStrings(values []string) string {
	payload, err := json.Marshal(values)
	if err != nil {
		payload = []byte(strings.Join(values, "\x00"))
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// containsExactToolName 判断 query 是否以完整标识符边界命中工具名。
// 工具名通常是 ASCII 标识符，中文字符与标点视为边界。
// Python: _contains_exact_tool_name
func containsExactToolName(query string, toolName string) bool {
	normalizedQuery := strings.ToLower(query)
	normalizedName := strings.TrimSpace(strings.ToLower(toolName))
	if normalizedQuery == "" || normalizedName == "" {
		return false
	}
	start := 0
	for start < len(normalizedQuery) {
		index := strings.Index(normalizedQuery[start:], normalizedName)
		if index < 0 {
			return false
		}
		match := start + index
		before := byte(0)
		if match > 0 {
			before = normalizedQuery[match-1]
		}
		end := match + len(normalizedName)
		after := byte(0)
		if end < len(normalizedQuery) {
			after = normalizedQuery[end]
		}
		if !isToolNameBoundaryChar(before) && !isToolNameBoundaryChar(after) {
			return true
		}
		start = match + 1
	}
	return false
}

// isToolNameBoundaryChar 判断字节是否可作为工具标识符的一部分。
// Python: _is_tool_name_boundary_char（value.isascii() and (alnum or "_.-")）
func isToolNameBoundaryChar(c byte) bool {
	if c == 0 || c >= 0x80 {
		return false
	}
	if c >= 'a' && c <= 'z' {
		return true
	}
	if c >= 'A' && c <= 'Z' {
		return true
	}
	if c >= '0' && c <= '9' {
		return true
	}
	return c == '_' || c == '.' || c == '-'
}
