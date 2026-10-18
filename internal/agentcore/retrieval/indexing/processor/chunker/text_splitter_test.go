package chunker

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
)

// TestNewCharSplitter_显式默认值 对齐 Python test_init_with_defaults
// （Go 无无参构造，显式传默认 200/40）
func TestNewCharSplitter_显式默认值(t *testing.T) {
	s := NewCharSplitter(200, 40)
	assert.Equal(t, 200, s.ChunkSize)
	assert.Equal(t, 40, s.ChunkOverlap)
}

// TestNewCharSplitter_自定义值 对齐 Python test_init_with_custom_values
func TestNewCharSplitter_自定义值(t *testing.T) {
	s := NewCharSplitter(512, 50)
	assert.Equal(t, 512, s.ChunkSize)
	assert.Equal(t, 50, s.ChunkOverlap)
}

// TestNewCharSplitter_size零值取默认200 对齐 Python `chunk_size or DEFAULT`（0→200）
func TestNewCharSplitter_size零值取默认200(t *testing.T) {
	s := NewCharSplitter(0, 40)
	assert.Equal(t, 200, s.ChunkSize)
}

// TestNewCharSplitter_overlap零是合法显式值 Go 无 None：0 不做默认替换（与 Python chunk_overlap=0 一致）
func TestNewCharSplitter_overlap零是合法显式值(t *testing.T) {
	s := NewCharSplitter(0, 0)
	assert.Equal(t, 200, s.ChunkSize)
	assert.Equal(t, 0, s.ChunkOverlap)
}

// TestNewCharSplitter_overlap越界钳制 对齐 Python test_init_overlap_adjusted
func TestNewCharSplitter_overlap越界钳制(t *testing.T) {
	s := NewCharSplitter(100, 150)
	assert.Equal(t, 100, s.ChunkSize)
	assert.Equal(t, 99, s.ChunkOverlap) // clamp(0, 150, 100-1)
}

// TestNewCharSplitter_overlap负值钳制 对齐 Python test_init_overlap_negative
func TestNewCharSplitter_overlap负值钳制(t *testing.T) {
	s := NewCharSplitter(100, -10)
	assert.Equal(t, 100, s.ChunkSize)
	assert.Equal(t, 0, s.ChunkOverlap)
}

// TestNewCharSplitter_size负值抬到1 Python 真值语义：-5 非 0 不走默认，max(1,-5)=1
func TestNewCharSplitter_size负值抬到1(t *testing.T) {
	s := NewCharSplitter(-5, 50)
	assert.Equal(t, 1, s.ChunkSize)
	assert.Equal(t, 0, s.ChunkOverlap) // clamp 后 overlap=size-1=0
}

// TestNewCharSplitter_size最小值 对齐 Python test_init_chunk_size_minimum（0→200≥1）
func TestNewCharSplitter_size最小值(t *testing.T) {
	s := NewCharSplitter(0, 40)
	assert.GreaterOrEqual(t, s.ChunkSize, 1)
}

// TestCharSplitter_Split_短文本单块 对齐 Python test_split_short_text
func TestCharSplitter_Split_短文本单块(t *testing.T) {
	s := NewCharSplitter(100, 10)
	doc := &reranker.Document{ID: "doc_1", Text: "Short text"}
	chunks, err := s.Split(doc)
	assert.NoError(t, err)
	assert.Len(t, chunks, 1)
	assert.Equal(t, "Short text", chunks[0].Text)
	assert.Equal(t, "doc_1", chunks[0].DocID)
	assert.NotEmpty(t, chunks[0].ID)
}

// TestCharSplitter_Split_长文本多块 对齐 Python test_split_long_text
// 65 字符，size=10 overlap=2 → step=8 → 9 块，末块 [64:65]="s"
func TestCharSplitter_Split_长文本多块(t *testing.T) {
	s := NewCharSplitter(10, 2)
	text := "This is a longer text that needs to be split into multiple chunks"
	doc := &reranker.Document{ID: "doc_1", Text: text}
	chunks, err := s.Split(doc)
	assert.NoError(t, err)
	assert.Len(t, chunks, 9)
	for _, chunk := range chunks {
		assert.Equal(t, "doc_1", chunk.DocID)
	}
	assert.Equal(t, "This is a ", chunks[0].Text)
	assert.Equal(t, "s", chunks[8].Text)
}

// TestCharSplitter_Split_重叠校验 对齐 Python test_split_with_overlap
// 33 字符，size=10 overlap=3 → step=7 → 5 块；相邻块首尾 3 字符重叠
func TestCharSplitter_Split_重叠校验(t *testing.T) {
	s := NewCharSplitter(10, 3)
	doc := &reranker.Document{ID: "doc_1", Text: "This is a test text for splitting"}
	chunks, err := s.Split(doc)
	assert.NoError(t, err)
	assert.Len(t, chunks, 5)
	// 块 i 的末 3 字符 == 块 i+1 的首 3 字符（overlap=3）
	for i := 0; i+1 < len(chunks); i++ {
		tail := chunks[i].Text[len(chunks[i].Text)-3:]
		head := chunks[i+1].Text[:3]
		assert.Equal(t, tail, head)
	}
}

// TestCharSplitter_Split_保留metadata 对齐 Python test_split_preserves_metadata
func TestCharSplitter_Split_保留metadata(t *testing.T) {
	s := NewCharSplitter(10, 2)
	doc := &reranker.Document{
		ID:       "doc_1",
		Text:     "This is a test",
		Metadata: map[string]any{"source": "test", "author": "test_author"},
	}
	chunks, err := s.Split(doc)
	assert.NoError(t, err)
	assert.NotEmpty(t, chunks)
	for _, chunk := range chunks {
		assert.Equal(t, "test", chunk.Metadata["source"])
		assert.Equal(t, "test_author", chunk.Metadata["author"])
	}
}

// TestCharSplitter_Split_中文rune切分 Go 专项：多字节字符不被截断
func TestCharSplitter_Split_中文rune切分(t *testing.T) {
	s := NewCharSplitter(3, 1)
	doc := &reranker.Document{ID: "doc_1", Text: "你好世界再见"}
	chunks, err := s.Split(doc)
	assert.NoError(t, err)
	assert.Len(t, chunks, 3)
	assert.Equal(t, "你好世", chunks[0].Text)
	assert.Equal(t, "世界再", chunks[1].Text)
	assert.Equal(t, "再见", chunks[2].Text)
}

// TestCharSplitter_Split_空文本 空文档产出空列表（对齐 Python while start < len(text)）
func TestCharSplitter_Split_空文本(t *testing.T) {
	s := NewCharSplitter(10, 2)
	chunks, err := s.Split(&reranker.Document{ID: "doc_1"})
	assert.NoError(t, err)
	assert.Empty(t, chunks)
}

// TestCharSplitter_Split_nil文档防御 Go 防御：nil 不 panic，返回空
func TestCharSplitter_Split_nil文档防御(t *testing.T) {
	s := NewCharSplitter(10, 2)
	chunks, err := s.Split(nil)
	assert.NoError(t, err)
	assert.Empty(t, chunks)
}

// TestCharSplitter_Split_nilMetadata转空map 对齐 Python `doc.metadata or {}`
func TestCharSplitter_Split_nilMetadata转空map(t *testing.T) {
	s := NewCharSplitter(10, 2)
	doc := &reranker.Document{ID: "doc_1", Text: "This is a test"}
	chunks, err := s.Split(doc)
	assert.NoError(t, err)
	assert.NotEmpty(t, chunks)
	for _, chunk := range chunks {
		assert.NotNil(t, chunk.Metadata)
		assert.Empty(t, chunk.Metadata)
	}
}

// ──────────────────────────── 13.7 Tokenizer 接口三态 ────────────────────────────

// fakeTokEncOnly 只实现 Encode（对齐 Python tokenize-only 分支②）
type fakeTokEncOnly struct{}

// Encode 按 rune 计数编码
func (f *fakeTokEncOnly) Encode(text string) ([]int, error) {
	return make([]int, len([]rune(text))), nil
}

// fakeTokFull 实现 Encode+Decode+MaxLength（对齐分支①+HF max_length）
type fakeTokFull struct{ maxLen int }

// Encode 按 rune 计数编码
func (f *fakeTokFull) Encode(text string) ([]int, error) {
	return make([]int, len([]rune(text))), nil
}

// Decode 解码为重复字符
func (f *fakeTokFull) Decode(ids []int) (string, error) {
	return strings.Repeat("a", len(ids)), nil
}

// MaxLength 返回最大序列长度
func (f *fakeTokFull) MaxLength() int { return f.maxLen }

// fakeTokEncFail Encode/Decode 恒失败（验证上抛）
type fakeTokEncFail struct{}

// Encode 恒失败
func (f *fakeTokEncFail) Encode(text string) ([]int, error) {
	return nil, fmt.Errorf("encode boom")
}

// Decode 恒失败
func (f *fakeTokEncFail) Decode(ids []int) (string, error) {
	return "", fmt.Errorf("decode boom")
}

func mustEnc(t *testing.T, enc func(string) ([]int, error), text string) []int {
	t.Helper()
	ids, err := enc(text)
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

func TestResolveTokenizers_完整实现(t *testing.T) {
	ft := &fakeTokFull{maxLen: 1024}
	enc, dec, maxLen := resolveTokenizers(ft)
	if maxLen != 1024 {
		t.Fatalf("maxLen 应为 1024, got %d", maxLen)
	}
	if enc == nil || dec == nil {
		t.Fatal("完整实现 enc/dec 都应有")
	}
	if got := len(mustEnc(t, enc, "你好")); got != 2 {
		t.Fatalf("enc 应按 rune 计数, got %d", got)
	}
}

func TestResolveTokenizers_空白文本返回空(t *testing.T) {
	ft := &fakeTokFull{}
	enc, _, _ := resolveTokenizers(ft)
	ids, err := enc("   \n ")
	if err != nil || len(ids) != 0 {
		t.Fatalf("空白文本应返回空切片: %v %v", ids, err)
	}
}

func TestResolveTokenizers_仅Encode(t *testing.T) {
	enc, dec, maxLen := resolveTokenizers(&fakeTokEncOnly{})
	if enc == nil {
		t.Fatal("enc 应有")
	}
	if dec != nil {
		t.Fatal("encode-only 实现 dec 应为 nil")
	}
	if maxLen != 0 {
		t.Fatalf("无 MaxLengthProvider maxLen 应为 0, got %d", maxLen)
	}
}

func TestResolveTokenizers_nil走tiktoken(t *testing.T) {
	enc, dec, maxLen := resolveTokenizers(nil)
	if enc == nil || dec == nil {
		t.Fatal("nil 应 fallback tiktoken（enc+dec）")
	}
	if maxLen != 0 {
		t.Fatalf("偏差1：maxLen 应为 0（max_token_value 不模拟）, got %d", maxLen)
	}
	// 真实 cl100k_base 编解码往返
	text := "你好世界 hello"
	ids, err := enc(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) == 0 {
		t.Fatal("真实 tiktoken 编码应有结果")
	}
	back, err := dec(ids)
	if err != nil {
		t.Fatal(err)
	}
	if back != text {
		t.Fatalf("tiktoken 往返不一致: %q != %q", back, text)
	}
}

func TestResolveTokenizers_encode失败上抛(t *testing.T) {
	enc, _, _ := resolveTokenizers(&fakeTokEncFail{})
	_, err := enc("anything")
	if err == nil {
		t.Fatal("encode 失败应上抛（决策 2b）")
	}
}

func TestResolveChunkSize(t *testing.T) {
	tests := []struct {
		name      string
		chunkSize int
		maxLen    int
		want      int
	}{
		{"未设+有max", 0, 1024, 1024},
		{"都设取min", 512, 256, 256},
		{"chunkSize小max大", 100, 200, 100},
		{"都无默认200", 0, 0, 200},
		{"仅显式", 300, 0, 300},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveChunkSize(tt.chunkSize, tt.maxLen); got != tt.want {
				t.Fatalf("resolveChunkSize(%d, %d) = %d, want %d", tt.chunkSize, tt.maxLen, got, tt.want)
			}
		})
	}
}
