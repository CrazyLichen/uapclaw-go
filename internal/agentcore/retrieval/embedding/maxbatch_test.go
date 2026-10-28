package embedding

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Test四实现MaxBatchSizeGetter 构造默认 8 + 选项覆写回读。
//
// Python: TripleBeamSearch 经 hasattr/embed_model.max_batch_size 消费
// （graph_retriever.py L91）；Python 四实现 __init__ 默认 8（VLLM 继承
// OpenAIEmbedding，Go 委托同构）。
func Test四实现MaxBatchSizeGetter(t *testing.T) {
	a := NewAPIEmbedding(EmbeddingConfig{ModelName: "m", BaseURL: "http://localhost"})
	assert.Equal(t, 8, a.MaxBatchSize())
	a2 := NewAPIEmbedding(EmbeddingConfig{ModelName: "m", BaseURL: "http://localhost"}, WithAPIMaxBatchSize(32))
	assert.Equal(t, 32, a2.MaxBatchSize())

	o := NewOpenAIEmbedding(EmbeddingConfig{ModelName: "m", BaseURL: "http://localhost", APIKey: "sk"})
	assert.Equal(t, 8, o.MaxBatchSize())
	o2 := NewOpenAIEmbedding(EmbeddingConfig{ModelName: "m", BaseURL: "http://localhost", APIKey: "sk"}, WithOpenAIMaxBatchSize(16))
	assert.Equal(t, 16, o2.MaxBatchSize())

	d := NewDashscopeEmbedding(EmbeddingConfig{ModelName: "m", BaseURL: "http://localhost", APIKey: "sk"})
	assert.Equal(t, 8, d.MaxBatchSize())
	d2 := NewDashscopeEmbedding(EmbeddingConfig{ModelName: "m", BaseURL: "http://localhost", APIKey: "sk"}, WithDashscopeMaxBatchSize(4))
	assert.Equal(t, 4, d2.MaxBatchSize())

	v := NewVLLMEmbedding(o2)
	assert.Equal(t, 16, v.MaxBatchSize()) // 委托 openAI（Python 继承同构）
}

// Test四实现MaxBatchSizeProvider断言 BaseEmbedding 接口断言四实现均实现
func Test四实现MaxBatchSizeProvider断言(t *testing.T) {
	// 消费点形态（retriever 包同款）：断言成功即 MaxBatchSizeProvider
	var models = []interface{ MaxBatchSize() int }{
		NewAPIEmbedding(EmbeddingConfig{ModelName: "m", BaseURL: "http://localhost"}),
		NewOpenAIEmbedding(EmbeddingConfig{ModelName: "m", BaseURL: "http://localhost", APIKey: "sk"}),
		NewDashscopeEmbedding(EmbeddingConfig{ModelName: "m", BaseURL: "http://localhost", APIKey: "sk"}),
	}
	for i, m := range models {
		assert.Equal(t, 8, m.MaxBatchSize(), "model %d", i)
	}
}
