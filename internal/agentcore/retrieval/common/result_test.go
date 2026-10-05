package common

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSearchResult_JSON序列化 测试 SearchResult 序列化
func TestSearchResult_JSON序列化(t *testing.T) {
	sr := SearchResult{
		ID:       "chunk-1",
		Text:     "测试文本",
		Score:    0.95,
		Metadata: map[string]any{"doc_id": "doc-1"},
	}
	data, err := json.Marshal(sr)
	assert.NoError(t, err)
	assert.Contains(t, string(data), `"id"`)
	assert.Contains(t, string(data), `"score"`)
}

// TestRetrievalResult_JSON序列化 测试 RetrievalResult 序列化
func TestRetrievalResult_JSON序列化(t *testing.T) {
	rr := RetrievalResult{
		Text:     "测试文本",
		Score:    0.88,
		Metadata: map[string]any{"source": "web"},
		DocID:    "doc-1",
		ChunkID:  "chunk-1",
	}
	data, err := json.Marshal(rr)
	assert.NoError(t, err)
	assert.Contains(t, string(data), `"doc_id"`)
	assert.Contains(t, string(data), `"chunk_id"`)
}

// TestRetrievalResult_OmitEmpty 测试空 DocID/ChunkID 不序列化
func TestRetrievalResult_OmitEmpty(t *testing.T) {
	rr := RetrievalResult{
		Text:     "测试",
		Score:    0.5,
		Metadata: map[string]any{},
	}
	data, err := json.Marshal(rr)
	assert.NoError(t, err)
	assert.NotContains(t, string(data), `"doc_id"`)
	assert.NotContains(t, string(data), `"chunk_id"`)
}
