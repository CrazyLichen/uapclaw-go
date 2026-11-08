package lite

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// maxQueryTokens 一次查询最多取用的 token 数。对齐 Python build_fts_query 的 tokens[:10]。
	maxQueryTokens = 10
	// ftsTrigramMinChars trigram 分词器能匹配的最小字符数。
	// 少于这个长度的 token（典型是两个汉字）无法命中 FTS，须走 LIKE 兜底。
	// 对齐 Python split_query_tokens 的 `len(t) < 3`（internal.py:193）。
	ftsTrigramMinChars = 3
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ftsWordPattern 从查询里切出词元（字母/数字/下划线连续段）。
// 对齐 Python `re.findall(r'\w+', cleaned)`。
var ftsWordPattern = regexp.MustCompile(`[\p{L}\p{N}_]+`)

// ──────────────────────────── 导出函数 ────────────────────────────

// EstimateTokens 估算 token 数（~4字符/token）。
// Python: estimate_tokens — 真实实现
func EstimateTokens(text string) int {
	return len(text) / 4
}

// HashText SHA256 前16字符。对齐 Python hash_text — 真实实现
func HashText(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:8]) // 前 8 字节 = 16 字符
}

// CosineSimilarity 余弦相似度。对齐 Python cosine_similarity — 真实实现
func CosineSimilarity(vec1, vec2 []float64) float64 {
	if len(vec1) != len(vec2) || len(vec1) == 0 {
		return 0
	}
	var dot, norm1, norm2 float64
	for i := range vec1 {
		dot += vec1[i] * vec2[i]
		norm1 += vec1[i] * vec1[i]
		norm2 += vec2[i] * vec2[i]
	}
	if math.Sqrt(norm1) < 1e-10 || math.Sqrt(norm2) < 1e-10 {
		return 0
	}
	return dot / (math.Sqrt(norm1) * math.Sqrt(norm2))
}

// ChunkMarkdown 按 token 切分 Markdown。
// Python: chunk_markdown — 真实实现
func ChunkMarkdown(content string, maxTokens int, overlap int) []MemoryChunk {
	if maxTokens <= 0 {
		maxTokens = 256
	}
	if overlap <= 0 {
		overlap = 32
	}
	lines := strings.Split(content, "\n")
	var chunks []MemoryChunk
	var currentLines []string
	var currentTokens int
	startLine := 1

	for i, line := range lines {
		lineNum := i + 1 // Python: 1-based 行号
		lineTokens := EstimateTokens(line)
		if currentTokens+lineTokens > maxTokens && len(currentLines) > 0 {
			chunks = append(chunks, MemoryChunk{
				Text:      strings.Join(currentLines, "\n"),
				StartLine: startLine,
				EndLine:   lineNum - 1,
			})
			// overlap: 回退 overlap tokens 的行数
			overlapLines := 0
			overlapTokens := 0
			for j := len(currentLines) - 1; j >= 0 && overlapTokens < overlap; j-- {
				overlapTokens += EstimateTokens(currentLines[j])
				overlapLines++
			}
			currentLines = currentLines[len(currentLines)-overlapLines:]
			currentTokens = overlapTokens
			startLine = lineNum - overlapLines
		}
		currentLines = append(currentLines, line)
		currentTokens += lineTokens
	}
	if len(currentLines) > 0 {
		chunks = append(chunks, MemoryChunk{
			Text:      strings.Join(currentLines, "\n"),
			StartLine: startLine,
			EndLine:   len(lines), // Python: 1-based 最后一行
		})
	}
	return chunks
}

// EnsureDir 确保目录存在。对齐 Python ensure_dir — 真实实现
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

// SplitQueryTokens 按 3 字符阈值把查询切成"长 token"与"短 token"。
//
// 阈值来自 FTS 表的 trigram 分词器：少于 3 个字符的 token（典型是两个汉字，如
// "组网""日志"）无法与 trigram 索引匹配，必须交给调用方的 LIKE 兜底；3 个及以上
// 走 BM25 排名。切分规则与上游一致：正则取词、最多 10 个、按 rune 数判长短。
//
// 对齐 Python split_query_tokens（internal.py:179）。
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

// BuildFTSQuery 用长 token 构建 FTS5 MATCH 表达式。
//
// 只用长 token：短 token 由调用方的 LIKE 兜底，混进来反而会让 MATCH 恒为空。
// 无长 token 时返回空串，表示"本次查询没有可走 FTS 的部分"。
// 连接符用 OR，与上游一致——多 token 用 AND 会让长查询几乎必然落空。
//
// 对齐 Python build_fts_query（internal.py:199）。
func BuildFTSQuery(query string) string {
	longTokens, _ := SplitQueryTokens(query)
	if len(longTokens) == 0 {
		return ""
	}

	parts := make([]string, len(longTokens))
	for i, t := range longTokens {
		parts[i] = `"` + t + `"`
	}
	return strings.Join(parts, " OR ")
}

// EscapeLikePattern 把 token 转成 LIKE 的 `%token%` 模式，并转义 LIKE 元字符。
//
// 反斜杠、百分号、下划线都要转义（配合 SQL 里的 `ESCAPE '\'`），否则用户查询里的
// `%` 会变成通配符，把整张表都捞出来。
// 对齐 Python MemoryIndexManager._escape_like（manager.py:1519）。
func EscapeLikePattern(token string) string {
	escaped := strings.ReplaceAll(token, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, "%", `\%`)
	escaped = strings.ReplaceAll(escaped, "_", `\_`)
	return "%" + escaped + "%"
}

// BM25RankToScore BM25 排名转相似度分数。对齐 Python bm25_rank_to_score — 真实实现
// BM25 rank 是负数（越小越好），转换为 0-1 的分数
func BM25RankToScore(rank float64) float64 {
	if rank >= 0 {
		return 1.0 / (1.0 + rank)
	}
	return 1.0 / (1.0 - rank)
}

// IsMemoryPath 判断是否为记忆文件路径。对齐 Python is_memory_path — 真实实现
func IsMemoryPath(relPath string) bool {
	normalized := strings.ReplaceAll(relPath, `\`, "/")
	return strings.HasSuffix(normalized, ".md")
}

// ListMemoryFiles 列出 workspace 下所有 .md 记忆文件。对齐 Python list_memory_files — 真实实现
func ListMemoryFiles(ws *workspace.Workspace, extraPaths []string, nodeName string) []string {
	files := make([]string, 0)
	if ws == nil {
		return files
	}

	// 1. 扫描 memory_dir 根目录下的 .md 文件
	memoryDir := ""
	if nodePath := ws.GetNodePath(nodeName); nodePath != nil {
		memoryDir = *nodePath
	}
	if memoryDir != "" {
		if entries, err := os.ReadDir(memoryDir); err == nil {
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
					files = append(files, filepath.Join(memoryDir, e.Name()))
				}
			}
		}

		// 2. 扫描 daily_memory 子目录
		dailyRel := ws.GetDirectory("daily_memory")
		if dailyRel != "" {
			dailyDir := filepath.Join(memoryDir, dailyRel)
			if entries, err := os.ReadDir(dailyDir); err == nil {
				for _, e := range entries {
					if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
						files = append(files, filepath.Join(dailyDir, e.Name()))
					}
				}
			}
		}
	}

	// 3. 收集 USER.md
	if userPath := ws.GetNodePath("USER.md"); userPath != nil {
		if _, err := os.Stat(*userPath); err == nil {
			files = append(files, *userPath)
		}
	}

	// 4. 收集 extra_paths 额外路径
	for _, extra := range extraPaths {
		fullPath := extra
		if memoryDir != "" && !filepath.IsAbs(extra) {
			fullPath = filepath.Join(memoryDir, extra)
		}
		if info, err := os.Stat(fullPath); err == nil {
			if info.IsDir() {
				if entries, err2 := os.ReadDir(fullPath); err2 == nil {
					for _, e := range entries {
						if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
							files = append(files, filepath.Join(fullPath, e.Name()))
						}
					}
				}
			} else if strings.HasSuffix(fullPath, ".md") {
				files = append(files, fullPath)
			}
		}
	}

	// 去重排序，对齐 Python: sorted(set(files))
	sort.Strings(files)
	return dedupStrings(files)
}

// NormalizeExtraMemoryPaths 归一化额外记忆路径。对齐 Python normalize_extra_memory_paths — 真实实现
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

// ──────────────────────────── 非导出函数 ────────────────────────────

// dedupStrings 对已排序的字符串切片去重
func dedupStrings(sorted []string) []string {
	if len(sorted) <= 1 {
		return sorted
	}
	result := make([]string, 0, len(sorted))
	result = append(result, sorted[0])
	for i := 1; i < len(sorted); i++ {
		if sorted[i] != sorted[i-1] {
			result = append(result, sorted[i])
		}
	}
	return result
}
