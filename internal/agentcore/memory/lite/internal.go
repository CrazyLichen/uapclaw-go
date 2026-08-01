package lite

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"os"
	"strings"
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
	if norm1 == 0 || norm2 == 0 {
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
	startLine := 0

	for i, line := range lines {
		lineTokens := EstimateTokens(line)
		if currentTokens+lineTokens > maxTokens && len(currentLines) > 0 {
			chunks = append(chunks, MemoryChunk{
				Text:      strings.Join(currentLines, "\n"),
				StartLine: startLine,
				EndLine:   i - 1,
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
			startLine = i - overlapLines
		}
		currentLines = append(currentLines, line)
		currentTokens += lineTokens
	}
	if len(currentLines) > 0 {
		chunks = append(chunks, MemoryChunk{
			Text:      strings.Join(currentLines, "\n"),
			StartLine: startLine,
			EndLine:   len(lines) - 1,
		})
	}
	return chunks
}

// EnsureDir 确保目录存在。对齐 Python ensure_dir — 真实实现
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

// ──────────────────────────── 薄接口函数 ────────────────────────────

// BuildFTSQuery 构建 FTS5 查询。⤵️ 回填: 7.4
func BuildFTSQuery(query string) string { return "" }

// BM25RankToScore BM25 排名转相似度分数。⤵️ 回填: 7.4
func BM25RankToScore(rank int) float64 { return 0 }

// IsMemoryPath 判断是否为记忆文件路径。⤵️ 回填: 7.4
func IsMemoryPath(relPath string) bool { return false }

// ListMemoryFiles 列出 workspace 下所有 .md 记忆文件。⤵️ 回填: 7.4
func ListMemoryFiles(workspace any, extraPaths []string, nodeName string) []string { return nil }

// NormalizeExtraMemoryPaths 归一化额外记忆路径。⤵️ 回填: 7.4
func NormalizeExtraMemoryPaths(paths []string, workspaceDir string) []string { return nil }
