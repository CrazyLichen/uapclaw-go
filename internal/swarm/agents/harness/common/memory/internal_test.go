package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEstimateTokens 对齐 Python estimate_tokens：len//4
func TestEstimateTokens(t *testing.T) {
	if EstimateTokens("") != 0 {
		t.Errorf("空串应为 0")
	}
	if EstimateTokens("12345678") != 2 {
		t.Errorf("8字符应为2 token")
	}
}

// TestListMemoryFiles_递归与根文件 对齐 swarm 语义（与 lite 非递归不同）
func TestListMemoryFiles_递归与根文件(t *testing.T) {
	dir := t.TempDir()
	// MEMORY.md 根文件
	_ = os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("# m"), 0o644)
	// memory/ 子目录递归
	_ = os.MkdirAll(filepath.Join(dir, "memory", "sub"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "memory", "a.md"), []byte("a"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "memory", "sub", "b.md"), []byte("b"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "memory", "c.txt"), []byte("c"), 0o644) // 非 md 排除

	files := ListMemoryFiles(dir, nil)
	if len(files) != 3 {
		t.Fatalf("应找到 3 个文件（MEMORY.md+a.md+sub/b.md），got %v", files)
	}
}

// TestListMemoryFiles_大小写变体 根目录 memory.md 也识别
func TestListMemoryFiles_大小写变体(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "memory.md"), []byte("# m"), 0o644)
	files := ListMemoryFiles(dir, nil)
	if len(files) != 1 {
		t.Fatalf("应找到 memory.md，got %v", files)
	}
}

// TestListMemoryFiles_extraPaths 对齐 Python extra_paths：文件或递归目录
func TestListMemoryFiles_extraPaths(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "extra", "deep"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "extra", "deep", "x.md"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "extra", "y.txt"), []byte("y"), 0o644)
	// 单文件 extra（相对路径）
	_ = os.WriteFile(filepath.Join(dir, "note.md"), []byte("n"), 0o644)

	files := ListMemoryFiles(dir, []string{"extra", "note.md"})
	if len(files) != 2 {
		t.Fatalf("应找到 extra/deep/x.md + note.md，got %v", files)
	}
}

// TestChunkMarkdown_基本分块 对齐 Python chunk_markdown：target_tokens=4, overlap=0
func TestChunkMarkdown_基本分块(t *testing.T) {
	// 每行 8 字符 = 2 token；tokens=4 → 2 行/块（对齐 Python len//4 估算）
	content := "12345678\n12345678\n12345678\n12345678"
	chunks := ChunkMarkdown(content, ChunkingConfig{Tokens: 4, Overlap: 0})
	if len(chunks) != 2 {
		t.Fatalf("4 行 2行/块应切 2 块，got %d", len(chunks))
	}
	if chunks[0].StartLine != 1 {
		t.Errorf("首块 startLine 应为 1")
	}
}

// TestChunkMarkdown_行号语义 对齐 Python：enumerate(lines, 1)，endLine=i-1
func TestChunkMarkdown_行号语义(t *testing.T) {
	// 每行 6 字符 = 1 token（6//4=1）；tokens=4 → 4 行一块；无尾换行（len(lines)=8）
	content := "aaaaaa\nbbbbbb\ncccccc\ndddddd\neeeeee\nffffff\ngggggg\nhhhhhh"
	chunks := ChunkMarkdown(content, ChunkingConfig{Tokens: 4, Overlap: 0})
	if len(chunks) != 2 {
		t.Fatalf("8 行 4行/块应切 2 块，got %d", len(chunks))
	}
	if chunks[0].StartLine != 1 || chunks[0].EndLine != 4 {
		t.Errorf("首块 [1,4]，got [%d,%d]", chunks[0].StartLine, chunks[0].EndLine)
	}
	if chunks[1].StartLine != 5 || chunks[1].EndLine != 8 {
		t.Errorf("次块 [5,8]，got [%d,%d]", chunks[1].StartLine, chunks[1].EndLine)
	}
}

// TestChunkMarkdown_空内容 边界：空串分块（无内容不产生 chunk）
func TestChunkMarkdown_空内容(t *testing.T) {
	chunks := ChunkMarkdown("", ChunkingConfig{Tokens: 4, Overlap: 0})
	// Python: "".split("\n") = [""]，单空行 current_chunk=[""]，产出 1 个 chunk
	if len(chunks) != 1 {
		t.Fatalf("空串按 Python 语义应产出 1 个空块，got %d", len(chunks))
	}
}

// TestChunkMarkdown_零值直传 默认值职责在 settings 层（CreateMemorySettings
// 已按键合并默认 256/32），本函数直用传入值——零值 cfg 不回退（与 Python
// "键存在值为 0" 语义一致：0 token 恒不切行，单行不可再分仍产 1 块）
func TestChunkMarkdown_零值直传(t *testing.T) {
	// 1500 字符单行；零值 cfg（tokens=0）恒不触发切块
	long := strings.Repeat("a", 1500)
	chunks := ChunkMarkdown(long, ChunkingConfig{})
	if len(chunks) != 1 {
		t.Fatalf("单行不可再分也只产出 1 块，got %d", len(chunks))
	}
}

// TestHashText 对齐 Python sha256 hexdigest[:16]
func TestHashText(t *testing.T) {
	h := HashText("hello")
	if len(h) != 16 {
		t.Fatalf("hash 长度应为 16，got %d", len(h))
	}
	if h != HashText("hello") {
		t.Errorf("确定性失败")
	}
	if h == HashText("world") {
		t.Errorf("不同输入应不同 hash")
	}
}

// TestBuildFTSQuery 对齐 Python：\w+ 分词前 10 个 OR 拼接
func TestBuildFTSQuery(t *testing.T) {
	if BuildFTSQuery("  ") != "" {
		t.Errorf("空查询应为空串")
	}
	q := BuildFTSQuery("hello world")
	if q != `"hello" OR "world"` {
		t.Errorf("got %s", q)
	}
}

// TestBuildFTSQuery_最多10个词 对齐 Python tokens[:10]
func TestBuildFTSQuery_最多10个词(t *testing.T) {
	// 12 个词只取前 10
	q := BuildFTSQuery("w1 w2 w3 w4 w5 w6 w7 w8 w9 w10 w11 w12")
	count := strings.Count(q, " OR ")
	if count != 9 {
		t.Errorf("应只有 10 个词（9 个 OR），got %d", count)
	}
}

// TestBm25RankToScore 对齐 Python：正负 rank 两种公式
func TestBm25RankToScore(t *testing.T) {
	if Bm25RankToScore(0) != 1.0 {
		t.Errorf("rank=0 应为 1.0")
	}
	if Bm25RankToScore(1) != 0.5 {
		t.Errorf("rank=1 应为 0.5")
	}
	if Bm25RankToScore(-1) != 0.5 {
		t.Errorf("rank=-1 应为 0.5（1/(1-(-1))）")
	}
}

// TestIsMemoryPath_swarm语义 对齐 Python：basename ∈ {memory.md, memory} 或路径含 memory 段
func TestIsMemoryPath_swarm语义(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"memory/MEMORY.md", true},   // basename memory.md（小写化）
		{"MEMORY.md", true},          // basename 为 memory（无扩展名场景）
		{"notes/memory/x.txt", true}, // 路径段含 memory
		{"a/b/c.md", false},          // 不含 memory 段
		{`a\memory\d.md`, true},      // 反斜杠归一化
	}
	for _, c := range cases {
		if got := IsMemoryPath(c.path); got != c.want {
			t.Errorf("IsMemoryPath(%q)=%v want %v", c.path, got, c.want)
		}
	}
}

// TestParseEmbedding 三态解析
func TestParseEmbedding(t *testing.T) {
	if ParseEmbedding([]any{1.0, 2.0}) == nil {
		t.Errorf("list 应直接返回")
	}
	if ParseEmbedding(map[string]any{"embedding": []any{1.0}}) == nil {
		t.Errorf("dict.embedding 应递归")
	}
	if ParseEmbedding(map[string]any{"data": []any{map[string]any{"embedding": []any{1.0}}}}) == nil {
		t.Errorf("dict.data[0].embedding 应提取")
	}
	if ParseEmbedding("nope") != nil {
		t.Errorf("无法解析返回 nil")
	}
}

// TestCosineSimilarity 对齐 Python
func TestCosineSimilarity(t *testing.T) {
	if CosineSimilarity([]float64{1, 0}, []float64{1, 0}) != 1.0 {
		t.Errorf("同向应为 1")
	}
	if CosineSimilarity([]float64{1, 0}, []float64{0, 1}) != 0.0 {
		t.Errorf("正交应为 0")
	}
	if CosineSimilarity([]float64{1}, []float64{1, 2}) != 0.0 {
		t.Errorf("维度不等应为 0")
	}
	if CosineSimilarity([]float64{0, 0}, []float64{0, 0}) != 0.0 {
		t.Errorf("零向量应为 0")
	}
}

// TestNormalizeExtraMemoryPaths 相对路径 join workspace
func TestNormalizeExtraMemoryPaths(t *testing.T) {
	got := NormalizeExtraMemoryPaths([]string{"rel", "/abs"}, "/ws")
	if len(got) != 2 || got[0] != filepath.Join("/ws", "rel") || got[1] != "/abs" {
		t.Fatalf("got %v", got)
	}
	if NormalizeExtraMemoryPaths(nil, "/ws") != nil {
		t.Errorf("nil 返回 nil")
	}
}

// TestBuildFileEntry 对齐 Python build_file_entry
func TestBuildFileEntry(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "MEMORY.md")
	_ = os.WriteFile(p, []byte("content"), 0o644)

	entry, err := BuildFileEntry(p, dir)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if entry.Path != "MEMORY.md" {
		t.Errorf("相对路径 got %s", entry.Path)
	}
	if entry.Hash != HashText("content") {
		t.Errorf("hash 应等于内容 hash")
	}
	if entry.Size != 7 {
		t.Errorf("size got %d", entry.Size)
	}
	if entry.MtimeMs <= 0 {
		t.Errorf("mtimeMs 应为正")
	}
}
