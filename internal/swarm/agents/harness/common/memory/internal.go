package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// maxQueryTokens 一次查询最多取用的 token 数。对齐 Python build_fts_query 的 tokens[:10]。
	maxQueryTokens = 10
	// ftsTrigramMinChars trigram 分词器能匹配的最小字符数。
	// 少于这个长度的 token（典型是两个汉字）无法命中 FTS，须走 LIKE 兜底。
	// 对齐 Python split_query_tokens 的 `len(t) < 3`（agent-core internal.py:193）。
	ftsTrigramMinChars = 3
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ftsWordPattern 从查询里切出词元（字母/数字/下划线连续段）。
// 对齐 Python re.findall(r'\w+')：Python 的 \w 在 str 模式下匹配 Unicode 字母，
// 而 Go 正则默认 \w 只匹配 ASCII，中文会整体落空，故显式写出 Unicode 字符类。
var ftsWordPattern = regexp.MustCompile(`[\p{L}\p{N}_]+`)

// ──────────────────────────── 导出函数 ────────────────────────────

// EstimateTokens 估算文本 token 数。对齐 Python estimate_tokens（internal.py）：len//4，~4 字符/token
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return len(text) / 4
}

// EnsureDir 确保目录存在。对齐 Python ensure_dir（internal.py）：空路径跳过
func EnsureDir(path string) error {
	if path == "" {
		return nil
	}
	return os.MkdirAll(path, 0o755)
}

// ListMemoryFiles 列出工作区全部记忆文件。对齐 Python list_memory_files（internal.py，swarm 语义）：
// 根目录 MEMORY.md/memory.md + memory/ 子目录递归遍历 .md + extraPaths（文件或递归目录）。
// 注意：与 agentcore lite 版（非递归+daily_memory+USER.md）语义不同，本包对齐 swarm 基线。
// 返回 sorted(set(files)) 等价的排序去重结果。
func ListMemoryFiles(workspaceDir string, extraPaths []string) []string {
	files := make([]string, 0)

	// 根目录 MEMORY.md / memory.md
	for _, filename := range []string{"MEMORY.md", "memory.md"} {
		fp := filepath.Join(workspaceDir, filename)
		if st, err := os.Stat(fp); err == nil && !st.IsDir() {
			files = append(files, fp)
		}
	}

	// memory/ 子目录递归（os.walk 等价）
	memoryDir := filepath.Join(workspaceDir, "memory")
	if st, err := os.Stat(memoryDir); err == nil && st.IsDir() {
		_ = filepath.Walk(memoryDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if !info.IsDir() && strings.HasSuffix(info.Name(), ".md") {
				files = append(files, path)
			}
			return nil
		})
	}

	// extraPaths：文件直接加入，目录递归遍历 .md
	for _, extra := range extraPaths {
		full := filepath.Join(workspaceDir, extra)
		if st, err := os.Stat(full); err == nil && !st.IsDir() {
			files = append(files, full)
		} else if st != nil && st.IsDir() {
			_ = filepath.Walk(full, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				if !info.IsDir() && strings.HasSuffix(info.Name(), ".md") {
					files = append(files, path)
				}
				return nil
			})
		}
	}

	// 对齐 Python sorted(set(files))
	seen := make(map[string]struct{}, len(files))
	unique := files[:0]
	for _, f := range files {
		if _, ok := seen[f]; !ok {
			seen[f] = struct{}{}
			unique = append(unique, f)
		}
	}
	sort.Strings(unique)
	return unique
}

// BuildFileEntry 构建文件索引条目。对齐 Python build_file_entry（internal.py）
func BuildFileEntry(absPath, workspaceDir string) (*FileEntry, error) {
	st, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("stat 文件失败: %w", err)
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	rel, err := filepath.Rel(workspaceDir, absPath)
	if err != nil {
		rel = absPath
	}
	return &FileEntry{
		Path:    rel,
		AbsPath: absPath,
		Hash:    HashText(string(content)),
		MtimeMs: st.ModTime().UnixMilli(),
		Size:    st.Size(),
	}, nil
}

// ChunkMarkdown 按 token 估算分块。对齐 Python chunk_markdown（internal.py）：
// 逐行累加 tokens，超限切块（endLine=i-1）；尾部回溯构造 overlap。
// 注意：Python enumerate(lines, 1) 行号从 1 起，Go range 从 0 起，
// 故 Go 的 i 对应 Python 的 i-1（endLine 直接用 i）。
// 默认值职责划分：Python 在函数内 settings.get("overlap", 32) 补缺键，
// Go 侧 CreateMemorySettings 已按键合并到带默认值的 ChunkingConfig（语义等价），
// 本函数直用传入值——显式 0（如 overlap: 0 表示无重叠）必须原样生效，
// 不做零值回退（回退会吞掉显式 0，违背 Python "键存在值为 0" 语义）
func ChunkMarkdown(content string, cfg ChunkingConfig) []MemoryChunk {
	targetTokens := cfg.Tokens
	overlap := cfg.Overlap

	lines := strings.Split(content, "\n")
	var chunks []MemoryChunk
	var currentChunk []string
	currentTokens := 0
	startLine := 1

	for i, line := range lines {
		lineTokens := EstimateTokens(line)

		if currentTokens+lineTokens > targetTokens && len(currentChunk) > 0 {
			chunks = append(chunks, MemoryChunk{
				Text:      strings.Join(currentChunk, "\n"),
				StartLine: startLine,
				EndLine:   i, // 对齐 Python endLine = i - 1（Python i 从 1 起，Go 从 0 起）
			})

			// 尾部回溯构造 overlap（对齐 Python for j in range(len-1, -1, -1)）
			var overlapLines []string
			overlapTokens := 0
			for j := len(currentChunk) - 1; j >= 0; j-- {
				lt := EstimateTokens(currentChunk[j])
				if overlapTokens+lt > overlap {
					break
				}
				overlapLines = append([]string{currentChunk[j]}, overlapLines...)
				overlapTokens += lt
			}
			currentChunk = overlapLines
			currentTokens = overlapTokens
			startLine = i + 1 - len(overlapLines)
		}

		currentChunk = append(currentChunk, line)
		currentTokens += lineTokens
	}

	if len(currentChunk) > 0 {
		chunks = append(chunks, MemoryChunk{
			Text:      strings.Join(currentChunk, "\n"),
			StartLine: startLine,
			EndLine:   len(lines),
		})
	}
	return chunks
}

// HashText 生成文本 hash。对齐 Python hash_text（internal.py）：sha256 hexdigest[:16]
func HashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])[:16]
}

// ParseEmbedding 解析 API 响应中的嵌入向量。对齐 Python parse_embedding（internal.py）：
// list 直返 / dict.embedding 递归 / dict.data[0].embedding 提取，其余返回 nil
func ParseEmbedding(data any) []float64 {
	switch v := data.(type) {
	case []any:
		out := make([]float64, 0, len(v))
		for _, item := range v {
			if f, ok := item.(float64); ok {
				out = append(out, f)
			}
		}
		return out
	case []float64:
		return v
	case map[string]any:
		if emb, ok := v["embedding"]; ok {
			return ParseEmbedding(emb)
		}
		if dataArr, ok := v["data"].([]any); ok && len(dataArr) > 0 {
			if first, ok := dataArr[0].(map[string]any); ok {
				if emb, ok := first["embedding"]; ok {
					return ParseEmbedding(emb)
				}
			}
		}
	}
	return nil
}

// SplitQueryTokens 按 3 字符阈值把查询切成"长 token"与"短 token"。
//
// 阈值来自 FTS 表的 trigram 分词器：少于 3 个字符的 token（典型是两个汉字，如
// "组网""日志"）无法与 trigram 索引匹配，必须交给调用方的 LIKE 兜底；3 个及以上
// 走 BM25 排名。
//
// 对齐 Python split_query_tokens（agent-core internal.py:179）。本包上游（jiuwenswarm）
// 尚未引入该函数，属超出上游的加固，见设计文档偏差 D-4。
func SplitQueryTokens(query string) (longTokens, shortTokens []string) {
	cleaned := strings.TrimSpace(query)
	if cleaned == "" {
		return nil, nil
	}
	tokens := ftsWordPattern.FindAllString(cleaned, -1)
	if len(tokens) > maxQueryTokens {
		tokens = tokens[:maxQueryTokens]
	}
	for _, t := range tokens {
		if utf8.RuneCountInString(t) < ftsTrigramMinChars {
			shortTokens = append(shortTokens, t)
		} else {
			longTokens = append(longTokens, t)
		}
	}
	return longTokens, shortTokens
}

// BuildFTSQuery 用长 token 构造 FTS5 查询。
//
// 只用长 token：短 token 由调用方的 LIKE 兜底，混进来反而让 MATCH 恒为空。
// 无长 token 时返回空串。连接符用 OR，与上游一致。
// 对齐 Python build_fts_query（internal.py:199）。
func BuildFTSQuery(query string) string {
	longTokens, _ := SplitQueryTokens(query)
	if len(longTokens) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(longTokens))
	for _, tok := range longTokens {
		quoted = append(quoted, `"`+tok+`"`)
	}
	return strings.Join(quoted, " OR ")
}

// EscapeLikePattern 把 token 转成 LIKE 的 `%token%` 模式并转义 LIKE 元字符。
//
// 反斜杠、百分号、下划线都要转义（配合 SQL 的 `ESCAPE '\'`），否则用户查询里的
// `%` 会变成通配符把整张表捞出来。
// 对齐 Python MemoryIndexManager._escape_like（agent-core manager.py:1519）。
func EscapeLikePattern(token string) string {
	escaped := strings.ReplaceAll(token, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, "%", `\%`)
	escaped = strings.ReplaceAll(escaped, "_", `\_`)
	return "%" + escaped + "%"
}

// Bm25RankToScore BM25 rank 转相似度分数。对齐 Python bm25_rank_to_score（internal.py）：
// rank>=0 时取 1/(1+rank)；rank<0 时取 1/(1-rank)
func Bm25RankToScore(rank float64) float64 {
	if rank >= 0 {
		return 1.0 / (1.0 + rank)
	}
	return 1.0 / (1.0 - rank)
}

// IsMemoryPath 判断路径是否为记忆文件。对齐 Python is_memory_path（internal.py，swarm 语义）：
// basename 小写 ∈ {memory.md, memory}，或路径任一段小写 == "memory"。
// 注意：与 lite 版（.md 后缀判断）语义不同。
func IsMemoryPath(relPath string) bool {
	base := strings.ToLower(filepath.Base(relPath))
	if base == "memory.md" || base == "memory" {
		return true
	}
	parts := strings.Split(strings.ReplaceAll(relPath, `\`, "/"), "/")
	for _, p := range parts {
		if strings.ToLower(p) == "memory" {
			return true
		}
	}
	return false
}

// NormalizeExtraMemoryPaths 归一化额外记忆路径。对齐 Python normalize_extra_memory_paths（internal.py）：
// 空返回空；绝对路径直返；相对路径 join workspaceDir
func NormalizeExtraMemoryPaths(paths []string, workspaceDir string) []string {
	if len(paths) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(paths))
	for _, p := range paths {
		if filepath.IsAbs(p) {
			normalized = append(normalized, p)
		} else {
			normalized = append(normalized, filepath.Join(workspaceDir, p))
		}
	}
	return normalized
}

// CosineSimilarity 计算余弦相似度。对齐 Python cosine_similarity（internal.py）：
// 维度不等返回 0；范数 < 1e-10 返回 0
func CosineSimilarity(vec1, vec2 []float64) float64 {
	if len(vec1) != len(vec2) {
		return 0.0
	}
	var dot, n1, n2 float64
	for i := range vec1 {
		dot += vec1[i] * vec2[i]
		n1 += vec1[i] * vec1[i]
		n2 += vec2[i] * vec2[i]
	}
	norm1 := math.Sqrt(n1)
	norm2 := math.Sqrt(n2)
	if norm1 < 1e-10 || norm2 < 1e-10 {
		return 0.0
	}
	return dot / (norm1 * norm2)
}
