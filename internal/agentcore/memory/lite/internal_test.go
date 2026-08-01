package lite

import (
	"math"
	"os"
	"testing"
)

// TestEstimateTokens 测试 token 估算
func TestEstimateTokens(t *testing.T) {
	if EstimateTokens("hello world") != 2 { // 11/4 ≈ 2 (整数除法)
		t.Errorf("Expected 2, got %d", EstimateTokens("hello world"))
	}
	if EstimateTokens("") != 0 {
		t.Errorf("Expected 0 for empty string")
	}
	if EstimateTokens("a") != 0 { // 1/4 = 0
		t.Errorf("Expected 0 for single char, got %d", EstimateTokens("a"))
	}
}

// TestHashText 测试 SHA256 前缀哈希
func TestHashText(t *testing.T) {
	h := HashText("test")
	if len(h) != 16 { // SHA256 前8字节=16字符
		t.Errorf("Expected 16 char hash, got %d chars", len(h))
	}
	// 相同输入相同输出
	if HashText("test") != h {
		t.Errorf("HashText not deterministic")
	}
	// 不同输入不同输出
	if HashText("other") == h {
		t.Errorf("Different inputs should produce different hashes")
	}
}

// TestCosineSimilarity 测试余弦相似度
func TestCosineSimilarity(t *testing.T) {
	// 相同向量 → 1.0
	vec := []float64{1.0, 2.0, 3.0}
	sim := CosineSimilarity(vec, vec)
	if math.Abs(sim-1.0) > 0.001 {
		t.Errorf("Expected ~1.0 for same vectors, got %f", sim)
	}
	// 长度不等 → 0
	if CosineSimilarity([]float64{1.0}, []float64{1.0, 2.0}) != 0 {
		t.Errorf("Expected 0 for mismatched lengths")
	}
	// 零向量 → 0
	if CosineSimilarity([]float64{0, 0}, []float64{1, 2}) != 0 {
		t.Errorf("Expected 0 for zero vector")
	}
	// 空向量 → 0
	if CosineSimilarity([]float64{}, []float64{}) != 0 {
		t.Errorf("Expected 0 for empty vectors")
	}
	// 正交向量 → 0
	if math.Abs(CosineSimilarity([]float64{1, 0}, []float64{0, 1})) > 0.001 {
		t.Errorf("Expected ~0.0 for orthogonal vectors")
	}
}

// TestChunkMarkdown 测试 Markdown 分块
func TestChunkMarkdown(t *testing.T) {
	content := "line1\nline2\nline3\nline4\nline5"
	chunks := ChunkMarkdown(content, 10, 2)
	if len(chunks) == 0 {
		t.Errorf("Expected at least 1 chunk")
	}
	// 验证第一个 chunk 的 StartLine
	if chunks[0].StartLine != 0 {
		t.Errorf("Expected StartLine=0, got %d", chunks[0].StartLine)
	}
}

// TestChunkMarkdown_默认参数 测试默认参数
func TestChunkMarkdown_默认参数(t *testing.T) {
	// maxTokens=0 和 overlap=0 应使用默认值
	chunks := ChunkMarkdown("short content", 0, 0)
	if len(chunks) != 1 {
		t.Errorf("Expected 1 chunk for short content with defaults, got %d", len(chunks))
	}
}

// TestEnsureDir 测试目录创建
func TestEnsureDir(t *testing.T) {
	dir := t.TempDir()
	subDir := dir + "/sub/nested"
	if err := EnsureDir(subDir); err != nil {
		t.Fatalf("EnsureDir failed: %v", err)
	}
	if _, err := os.Stat(subDir); os.IsNotExist(err) {
		t.Errorf("Directory not created")
	}
}
