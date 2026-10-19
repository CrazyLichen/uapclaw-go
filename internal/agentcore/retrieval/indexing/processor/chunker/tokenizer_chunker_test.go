package chunker

import (
	"context"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
)

// tokByRune rune 计数 tokenizer（Encode+Decode 全实现）
type tokByRune struct{}

// Encode 按 rune 位置编码
func (t *tokByRune) Encode(text string) ([]int, error) {
	rs := []rune(text)
	ids := make([]int, len(rs))
	for i := range rs {
		ids[i] = i + 1
	}
	return ids, nil
}

// Decode 解码为可见字符
func (t *tokByRune) Decode(ids []int) (string, error) {
	runes := make([]rune, len(ids))
	for i, id := range ids {
		runes[i] = rune('a' + (id % 26))
	}
	return string(runes), nil
}

func TestNewTokenizerChunker_构造校验(t *testing.T) {
	if _, err := NewTokenizerChunker(0, 50, nil, "auto", nil); err == nil {
		t.Fatal("chunk_size<=0 应过基类校验 155100")
	}
	if _, err := NewTokenizerChunker(512, 600, nil, "auto", nil); err == nil {
		t.Fatal("overlap>=size 应过基类校验 155101")
	}
	c, err := NewTokenizerChunker(20, 4, &tokByRune{}, "auto", nil)
	if err != nil {
		t.Fatalf("合法参数不应报错: %v", err)
	}
	if c.ChunkSize != 20 || c.ChunkOverlap != 4 {
		t.Fatalf("基类字段未保存: %+v", c.BaseChunker)
	}
}

func TestTokenizerChunker_ChunkText_空文本(t *testing.T) {
	c, _ := NewTokenizerChunker(20, 4, &tokByRune{}, "auto", nil)
	got, err := c.ChunkText("")
	if err != nil || len(got) != 0 {
		t.Fatalf("空文本应返回空: %v %v", got, err)
	}
}

func TestTokenizerChunker_ChunkText_分块(t *testing.T) {
	c, _ := NewTokenizerChunker(12, 0, &tokByRune{}, "auto", nil) // 12 token 预算
	text := "一二三。四五。六七。八九。十。"                                     // 每句4token，共20 token
	got, err := c.ChunkText(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 {
		t.Fatalf("20 token 预算 12 应至少 2 块: %v", got)
	}
	joined := strings.Join(got, "")
	if !strings.Contains(joined, "一二三。") {
		t.Fatalf("块内容应含首句: %v", got)
	}
}

func TestTokenizerChunker_ChunkDocuments_带元数据(t *testing.T) {
	c, _ := NewTokenizerChunker(12, 0, &tokByRune{}, "auto", nil)
	docs := []reranker.Document{{ID: "doc1", Text: "一二三。四五。六七。", Metadata: map[string]any{"src": "test"}}}
	chunks, err := c.ChunkDocuments(context.TODO(), docs)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 {
		t.Fatal("应有块产出")
	}
	for _, ch := range chunks {
		if ch.DocID != "doc1" {
			t.Fatalf("DocID 应为 doc1: %s", ch.DocID)
		}
		if ch.Metadata["src"] != "test" {
			t.Fatalf("元数据应透传: %v", ch.Metadata)
		}
		if _, ok := ch.Metadata["chunk_index"]; !ok {
			t.Fatal("应有 chunk_index 元数据")
		}
		if _, ok := ch.Metadata["chunk_id"]; !ok {
			t.Fatal("应有 chunk_id 元数据")
		}
	}
}

func TestTokenizerChunker_ChunkText_nilTokenizer走tiktoken(t *testing.T) {
	// nil tokenizer → tiktoken cl100k_base fallback（分支④）
	c, err := NewTokenizerChunker(512, 50, nil, "auto", nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.ChunkText("大模型是重要技术。它通过海量数据训练。")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("tiktoken fallback 应有分块产出")
	}
}

func TestIndexSentenceSplitter_splitterConfig归一化不报错(t *testing.T) {
	// splitter_config 非 map（nil 或任意值）归一化后忽略——不应 panic/报错（偏差 3a）
	_, err := NewIndexSentenceSplitter(&tokByRune{}, 20, 4, nil, "auto")
	if err != nil {
		t.Fatalf("splitterConfig=nil 应归一化不报错: %v", err)
	}
}

func TestIndexSentenceSplitter_overlap为0取五分之一(t *testing.T) {
	// Python `chunk_overlap or chunk_size // 5`：overlap==0 → chunkSize/5
	//（与 CharSplitter 的"0 是合法显式值"语义相反是有意的，Python 源码即如此）
	sp, err := NewIndexSentenceSplitter(&tokByRune{}, 20, 0, nil, "auto")
	if err != nil {
		t.Fatal(err)
	}
	if sp.splitter.ChunkOverlap != 4 {
		t.Fatalf("overlap=0 应取 chunkSize/5=4, got %d", sp.splitter.ChunkOverlap)
	}
}

func TestIndexSentenceSplitter_maxLength参与chunkSize(t *testing.T) {
	// fakeTokFull 在 text_splitter_test.go 定义（maxLen=15 < chunkSize=100 → 钳到 15）
	sp, err := NewIndexSentenceSplitter(&fakeTokFull{maxLen: 15}, 100, 0, nil, "auto")
	if err != nil {
		t.Fatal(err)
	}
	if sp.splitter.ChunkSize != 15 {
		t.Fatalf("chunkSize 应被 maxLen 钳到 15, got %d", sp.splitter.ChunkSize)
	}
}

func TestIndexSentenceSplitter_实现TextSplitter接口(t *testing.T) {
	// 编译期断言已在 text_splitter.go；运行时验证 Split 委托链
	sp, _ := NewIndexSentenceSplitter(&tokByRune{}, 100, 0, nil, "auto")
	doc := &reranker.Document{ID: "d1", Text: "第一句。第二句。", Metadata: map[string]any{"k": "v"}}
	chunks, err := sp.Split(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("预算内应 1 块: %+v", chunks)
	}
	if chunks[0].DocID != "d1" {
		t.Fatalf("DocID 应透传: %+v", chunks[0])
	}
	if chunks[0].Metadata["k"] != "v" {
		t.Fatalf("元数据应透传: %+v", chunks[0].Metadata)
	}
	if chunks[0].ID == "" {
		t.Fatal("NewTextChunkFromDocument 应生成 UUID")
	}
}
