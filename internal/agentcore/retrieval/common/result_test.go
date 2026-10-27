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

// TestMultiKBRetrievalResult_全字段构造 六字段读写一致（R1）
func TestMultiKBRetrievalResult_全字段构造(t *testing.T) {
	r := MultiKBRetrievalResult{
		Text:           "测试文本",
		Score:          0.9,
		RawScore:       0.85,
		RawScoreScaled: 0.8,
		KbIDs:          []string{"kb-1", "kb-2"},
		Metadata:       map[string]any{"source": "web"},
	}
	assert.Equal(t, "测试文本", r.Text)
	assert.Equal(t, 0.9, r.Score)
	assert.Equal(t, 0.85, r.RawScore)
	assert.Equal(t, 0.8, r.RawScoreScaled)
	assert.Equal(t, []string{"kb-1", "kb-2"}, r.KbIDs)
	assert.Equal(t, map[string]any{"source": "web"}, r.Metadata)
}

// TestMultiKBRetrievalResult_JSON字段名 序列化含六键名（R2）
func TestMultiKBRetrievalResult_JSON字段名(t *testing.T) {
	r := MultiKBRetrievalResult{
		Text:           "text",
		Score:          0.9,
		RawScore:       0.85,
		RawScoreScaled: 0.8,
		KbIDs:          []string{"kb-1"},
		Metadata:       map[string]any{},
	}
	data, err := json.Marshal(r)
	assert.NoError(t, err)
	s := string(data)
	for _, key := range []string{`"text"`, `"score"`, `"raw_score"`, `"raw_score_scaled"`, `"kb_ids"`, `"metadata"`} {
		assert.Contains(t, s, key)
	}
}

// TestMultiKBRetrievalResult_KbIDs无omitempty nil 序列化为 null 而非省略（R3，default_factory 先例）
func TestMultiKBRetrievalResult_KbIDs无omitempty(t *testing.T) {
	r := MultiKBRetrievalResult{Text: "text", Score: 0.9}
	data, err := json.Marshal(r)
	assert.NoError(t, err)
	assert.Contains(t, string(data), `"kb_ids":null`)
}

// TestMultiKBRetrievalResult_Metadata无omitempty 同款注记（R4）
func TestMultiKBRetrievalResult_Metadata无omitempty(t *testing.T) {
	r := MultiKBRetrievalResult{Text: "text", Score: 0.9}
	data, err := json.Marshal(r)
	assert.NoError(t, err)
	assert.Contains(t, string(data), `"metadata":null`)
}
