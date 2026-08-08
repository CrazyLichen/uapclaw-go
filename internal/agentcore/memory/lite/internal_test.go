package lite

import (
	"os"
	"testing"
)

// TestBuildFTSQuery 测试 FTS5 查询构建
func TestBuildFTSQuery(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		wantEmpty bool
	}{
		{"空字符串", "", true},
		{"空格", "   ", true},
		{"简单词", "hello", false},
		{"多个词", "hello world", false},
		{"中文", "你好世界", true}, // Go 的 \w+ 不匹配中文字符，与 Python 不同
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildFTSQuery(tt.query)
			if (got == "") != tt.wantEmpty {
				t.Errorf("BuildFTSQuery(%q) = %q, wantEmpty=%v", tt.query, got, tt.wantEmpty)
			}
		})
	}
}

// TestBuildFTSQuery_格式验证 测试 FTS5 查询格式
func TestBuildFTSQuery_格式验证(t *testing.T) {
	got := BuildFTSQuery("hello world")
	if got != `"hello" OR "world"` {
		t.Errorf("BuildFTSQuery('hello world') = %q, 期望 %q", got, `"hello" OR "world"`)
	}
}

// TestBM25RankToScore 测试 BM25 排名转分数
func TestBM25RankToScore(t *testing.T) {
	if score := BM25RankToScore(0); score != 1.0 {
		t.Errorf("BM25RankToScore(0) = %f, want 1.0", score)
	}
	if score := BM25RankToScore(-1); score <= 0 {
		t.Errorf("BM25RankToScore(-1) = %f, want > 0", score)
	}
	if score := BM25RankToScore(-5); score <= 0 || score >= 1 {
		t.Errorf("BM25RankToScore(-5) = %f, want between 0 and 1", score)
	}
	// BM25 rank 通常为负数，越小越不相关
	score1 := BM25RankToScore(-1)
	score2 := BM25RankToScore(-10)
	if score1 <= score2 {
		t.Errorf("BM25RankToScore(-1)=%f 应大于 BM25RankToScore(-10)=%f", score1, score2)
	}
}

// TestIsMemoryPath 测试记忆路径判断
func TestIsMemoryPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"notes.md", true},
		{"MEMORY.md", true},
		{"data.json", false},
		{"readme.txt", false},
		{"dir/file.md", true},
		{"dir\\file.md", true}, // Windows 路径
		{"file.md.bak", false},
	}
	for _, tt := range tests {
		if got := IsMemoryPath(tt.path); got != tt.want {
			t.Errorf("IsMemoryPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

// TestNormalizeExtraMemoryPaths 测试额外记忆路径归一化
func TestNormalizeExtraMemoryPaths(t *testing.T) {
	// 绝对路径保持不变
	result := NormalizeExtraMemoryPaths([]string{"/tmp/mem"}, "/workspace")
	if len(result) != 1 || result[0] != "/tmp/mem" {
		t.Errorf("绝对路径应保持不变，实际为 %v", result)
	}
	// 相对路径拼接 workspaceDir
	result = NormalizeExtraMemoryPaths([]string{"extra"}, "/workspace")
	if len(result) != 1 || result[0] != "/workspace/extra" {
		t.Errorf("相对路径应拼接 workspaceDir，实际为 %v", result)
	}
	// nil 返回 nil
	result = NormalizeExtraMemoryPaths(nil, "/workspace")
	if result != nil {
		t.Errorf("nil 应返回 nil，实际为 %v", result)
	}
	// 空切片返回 nil
	result = NormalizeExtraMemoryPaths([]string{}, "/workspace")
	if result != nil {
		t.Errorf("空切片应返回 nil，实际为 %v", result)
	}
}

// TestEstimateTokens 测试 token 估算
func TestEstimateTokens(t *testing.T) {
	if tokens := EstimateTokens("hello world"); tokens != 2 {
		t.Errorf("EstimateTokens('hello world') = %d, want 2", tokens)
	}
	if tokens := EstimateTokens(""); tokens != 0 {
		t.Errorf("EstimateTokens('') = %d, want 0", tokens)
	}
}

// TestHashText 测试哈希函数
func TestHashText(t *testing.T) {
	hash1 := HashText("hello")
	hash2 := HashText("hello")
	if hash1 != hash2 {
		t.Error("相同输入应产生相同哈希")
	}
	if len(hash1) != 16 {
		t.Errorf("哈希长度应为 16，实际为 %d", len(hash1))
	}
	hash3 := HashText("world")
	if hash1 == hash3 {
		t.Error("不同输入应产生不同哈希")
	}
}

// TestCosineSimilarity 测试余弦相似度
func TestCosineSimilarity(t *testing.T) {
	// 相同向量
	vec := []float64{1.0, 0.0, 0.0}
	if sim := CosineSimilarity(vec, vec); sim != 1.0 {
		t.Errorf("相同向量相似度应为 1.0，实际为 %f", sim)
	}
	// 正交向量
	vec1 := []float64{1.0, 0.0}
	vec2 := []float64{0.0, 1.0}
	if sim := CosineSimilarity(vec1, vec2); sim != 0.0 {
		t.Errorf("正交向量相似度应为 0.0，实际为 %f", sim)
	}
	// 不同长度
	if sim := CosineSimilarity([]float64{1.0}, []float64{1.0, 2.0}); sim != 0 {
		t.Errorf("不同长度向量应返回 0，实际为 %f", sim)
	}
}

// TestChunkMarkdown 测试 Markdown 分块
func TestChunkMarkdown(t *testing.T) {
	// 短文本不分块
	chunks := ChunkMarkdown("hello", 256, 32)
	if len(chunks) != 1 {
		t.Errorf("短文本应产生 1 个 chunk，实际为 %d", len(chunks))
	}
	// 长文本分块
	longText := ""
	for i := 0; i < 100; i++ {
		longText += "This is a line of text for testing chunking.\n"
	}
	chunks = ChunkMarkdown(longText, 50, 10)
	if len(chunks) <= 1 {
		t.Errorf("长文本应产生多个 chunk，实际为 %d", len(chunks))
	}
	// 零参数使用默认值
	chunks = ChunkMarkdown("test", 0, 0)
	if len(chunks) != 1 {
		t.Errorf("零参数应使用默认值，实际为 %d chunks", len(chunks))
	}
}

// TestEnsureDir 测试目录创建
func TestEnsureDir(t *testing.T) {
	tmpDir := t.TempDir()
	testDir := tmpDir + "/test/nested"
	if err := EnsureDir(testDir); err != nil {
		t.Fatalf("EnsureDir 失败: %v", err)
	}
	if _, err := os.Stat(testDir); os.IsNotExist(err) {
		t.Error("EnsureDir 后目录应存在")
	}
}
