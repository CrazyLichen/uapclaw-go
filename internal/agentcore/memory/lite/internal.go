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

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// EstimateTokens 估算 token 数（~4字符/token）。
// 对齐 Python estimate_tokens — 真实实现
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
// 对齐 Python chunk_markdown — 真实实现
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
		lineNum := i + 1 // 对齐 Python: 1-based 行号
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
			EndLine:   len(lines), // 对齐 Python: 1-based 最后一行
		})
	}
	return chunks
}

// EnsureDir 确保目录存在。对齐 Python ensure_dir — 真实实现
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

// BuildFTSQuery 构建 FTS5 查询。对齐 Python build_fts_query — 真实实现
// 将查询文本分词后用 OR 连接，每个词用双引号包裹
func BuildFTSQuery(query string) string {
	cleaned := strings.TrimSpace(query)
	if cleaned == "" {
		return ""
	}
	tokens := regexp.MustCompile(`[\p{L}\p{N}_]+`).FindAllString(cleaned, 10)
	if len(tokens) == 0 {
		return ""
	}
	parts := make([]string, len(tokens))
	for i, t := range tokens {
		parts[i] = `"` + t + `"`
	}
	return strings.Join(parts, " OR ")
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

	// 3. USER.md
	if userPath := ws.GetNodePath("USER.md"); userPath != nil {
		if _, err := os.Stat(*userPath); err == nil {
			files = append(files, *userPath)
		}
	}

	// 4. extra_paths
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
