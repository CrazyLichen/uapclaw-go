package splitter

import (
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
)

// fakeSplitDocSplitter Splitter 接口 fake：返回固定片段
type fakeSplitDocSplitter struct {
	*BaseSplitter
	spans []ChunkSpan
}

// SplitDoc 返回预置片段
func (f *fakeSplitDocSplitter) SplitDoc(doc string) ([]ChunkSpan, error) {
	return f.spans, nil
}

func TestNewBaseSplitter_合法参数(t *testing.T) {
	enc := func(s string) ([]int, error) { return make([]int, len([]rune(s))), nil }
	bs, err := NewBaseSplitter(512, 50, enc, nil)
	if err != nil {
		t.Fatalf("合法参数不应报错: %v", err)
	}
	if bs.ChunkSize != 512 || bs.ChunkOverlap != 50 {
		t.Fatalf("字段未正确保存: %+v", bs)
	}
}

func TestNewBaseSplitter_size非正数报错(t *testing.T) {
	_, err := NewBaseSplitter(0, 50, nil, nil)
	if err == nil {
		t.Fatal("chunk_size<=0 应报 155100")
	}
}

func TestNewBaseSplitter_overlap负数报错(t *testing.T) {
	_, err := NewBaseSplitter(512, -1, nil, nil)
	if err == nil {
		t.Fatal("overlap<0 应报 155101")
	}
}

func TestNewBaseSplitter_overlap大于等于size报错(t *testing.T) {
	_, err := NewBaseSplitter(50, 50, nil, nil)
	if err == nil {
		t.Fatal("overlap>=size 应报 155101")
	}
}

func TestGetTokenCount_有enc(t *testing.T) {
	enc := func(s string) ([]int, error) { return make([]int, len([]rune(s))), nil }
	bs, _ := NewBaseSplitter(512, 50, enc, nil)
	if got := bs.GetTokenCount("你好ab"); got != 4 {
		t.Fatalf("enc 计数应为 4 (rune 数), got %d", got)
	}
}

func TestGetTokenCount_无enc按字符(t *testing.T) {
	bs, _ := NewBaseSplitter(512, 50, nil, nil)
	if got := bs.GetTokenCount("你好ab"); got != 4 {
		t.Fatalf("无 enc 应按 rune 计数 4, got %d", got)
	}
}

func TestSplitText(t *testing.T) {
	enc := func(s string) ([]int, error) { return make([]int, len([]rune(s))), nil }
	bs, _ := NewBaseSplitter(512, 50, enc, nil)
	fake := &fakeSplitDocSplitter{BaseSplitter: bs, spans: []ChunkSpan{
		{Text: "第一句。", Start: 0, End: len("第一句。")},
		{Text: "第二句！", Start: len("第一句。"), End: len("第一句。第二句！")},
	}}
	bs.SetSelf(fake)
	got, err := bs.SplitText("whatever")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "第一句。" || got[1] != "第二句！" {
		t.Fatalf("SplitText 应只取 text: %v", got)
	}
}

func TestGetNodesFromDocuments_空文档跳过(t *testing.T) {
	enc := func(s string) ([]int, error) { return make([]int, len([]rune(s))), nil }
	bs, _ := NewBaseSplitter(512, 50, enc, nil)
	fake := &fakeSplitDocSplitter{BaseSplitter: bs, spans: []ChunkSpan{
		{Text: "第一句。", Start: 0, End: len("第一句。")},
	}}
	bs.SetSelf(fake)
	docs := []reranker.Document{
		{ID: "empty", Text: ""},
		{ID: "ok", Text: "第一句。"},
	}
	nodes, err := bs.GetNodesFromDocuments(docs)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("空文档应跳过只留 1 块: %+v", nodes)
	}
	if nodes[0].DocID != "ok" || nodes[0].Text != "第一句。" {
		t.Fatalf("块内容/doc_id 不符: %+v", nodes[0])
	}
	if nodes[0].ID == "" {
		t.Fatal("NewTextChunkFromDocument 应自动生成 UUID")
	}
}

func TestGetNodesFromDocuments_未注入self报错(t *testing.T) {
	bs, _ := NewBaseSplitter(512, 50, nil, nil)
	if _, err := bs.GetNodesFromDocuments([]reranker.Document{{ID: "x", Text: "文本"}}); err == nil {
		t.Fatal("未 SetSelf 应报错")
	}
	_ = strings.TrimSpace
}
