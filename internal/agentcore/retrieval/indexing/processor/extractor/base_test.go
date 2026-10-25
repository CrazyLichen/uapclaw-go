package extractor

import (
	"context"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// concreteExtractor 测试用具体抽取器（对齐 test_base.py ConcreteExtractor）
type concreteExtractor struct{}

// Extract 简单抽取逻辑：文本含 "knows" 时产出一条三元组（对齐 Python 实现）
func (c *concreteExtractor) Extract(_ context.Context, chunks []common.TextChunk, _ ...ExtractOption) ([]common.Triple, error) {
	triples := make([]common.Triple, 0)
	for _, chunk := range chunks {
		if strings.Contains(chunk.Text, "knows") {
			triples = append(triples, common.Triple{
				Subject:   "Alice",
				Predicate: "knows",
				Object:    "Bob",
				Metadata:  map[string]any{"doc_id": chunk.DocID},
			})
		}
	}
	return triples, nil
}

// Process 转发 Extract（对齐 Python 基类 process → self.extract）
func (c *concreteExtractor) Process(ctx context.Context, chunks []common.TextChunk, opts ...ExtractOption) ([]common.Triple, error) {
	return c.Extract(ctx, chunks, opts...)
}

// TestExtractor_Extract 测试具体实现 Extract（对齐 test_base.py test_extract）
func TestExtractor_Extract(t *testing.T) {
	extractor := &concreteExtractor{}
	chunks := []common.TextChunk{
		{ID: "1", Text: "Alice knows Bob", DocID: "doc_1"},
		{ID: "2", Text: "Some other text", DocID: "doc_1"},
	}
	triples, err := extractor.Extract(context.Background(), chunks)
	if err != nil {
		t.Fatalf("Extract 不应报错: %v", err)
	}
	if len(triples) != 1 {
		t.Fatalf("应抽取 1 条三元组，实际 %d", len(triples))
	}
	if triples[0].Subject != "Alice" || triples[0].Predicate != "knows" || triples[0].Object != "Bob" {
		t.Errorf("三元组字段断言失败: %+v", triples[0])
	}
}

// TestExtractor_Process 测试 Process 转发（对齐 test_base.py test_process）
func TestExtractor_Process(t *testing.T) {
	extractor := &concreteExtractor{}
	chunks := []common.TextChunk{{ID: "1", Text: "Alice knows Bob", DocID: "doc_1"}}
	triples, err := extractor.Process(context.Background(), chunks)
	if err != nil {
		t.Fatalf("Process 不应报错: %v", err)
	}
	if len(triples) != 1 {
		t.Fatalf("Process 应转发产出 1 条三元组，实际 %d", len(triples))
	}
}
