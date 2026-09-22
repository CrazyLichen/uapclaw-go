package schema

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewVectorNode(t *testing.T) {
	n := NewVectorNode("id1", "hello", []float64{1, 2, 3}, map[string]any{"type": "task"})
	assert.Equal(t, "id1", n.ID)
	assert.Equal(t, "hello", n.Content)
	assert.Equal(t, []float64{1, 2, 3}, n.Embedding)
	assert.Equal(t, "task", n.Metadata["type"])
}

func TestNewVectorNode_nil元数据(t *testing.T) {
	n := NewVectorNode("id1", "hello", nil, nil)
	assert.NotNil(t, n.Metadata) // 应自动初始化空 map
	assert.Equal(t, 0, len(n.Metadata))
}

func TestVectorNode_ToDict_FromDict_往返(t *testing.T) {
	original := NewVectorNode("id2", "content text", []float64{0.1, 0.2}, map[string]any{"key": "val"})
	dict := original.ToDict()

	recovered, err := VectorNodeFromDict(dict)
	require.NoError(t, err)
	assert.Equal(t, original.ID, recovered.ID)
	assert.Equal(t, original.Content, recovered.Content)
	assert.InDeltaSlice(t, original.Embedding, recovered.Embedding, 0.001)
	assert.Equal(t, original.Metadata["key"], recovered.Metadata["key"])
}

func TestVectorNode_JSON_序列化(t *testing.T) {
	n := NewVectorNode("id3", "text", []float64{1.0}, map[string]any{"x": 1.0})
	data, err := json.Marshal(n)
	require.NoError(t, err)

	var recovered VectorNode
	err = json.Unmarshal(data, &recovered)
	require.NoError(t, err)
	assert.Equal(t, n.ID, recovered.ID)
	assert.Equal(t, n.Content, recovered.Content)
}

func TestVectorNode_Embedding_omitempty(t *testing.T) {
	n := &VectorNode{ID: "id4", Content: "text", Metadata: map[string]any{}}
	data, err := json.Marshal(n)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "embedding")
}

func TestVectorNodeFromDict_缺失字段(t *testing.T) {
	_, err := VectorNodeFromDict(map[string]any{})
	require.Error(t, err)

	_, err = VectorNodeFromDict(map[string]any{"id": "x"})
	require.Error(t, err) // content 缺失
}

func TestVectorNodeFromDict_从JSON反序列化的embedding(t *testing.T) {
	// JSON 反序列化后 embedding 是 []any 而非 []float64
	data := map[string]any{
		"id":      "n1",
		"content": "text",
		"embedding": []any{1.0, 2.0, 3.0},
		"metadata": map[string]any{"k": "v"},
	}
	node, err := VectorNodeFromDict(data)
	require.NoError(t, err)
	assert.InDeltaSlice(t, []float64{1, 2, 3}, node.Embedding, 0.001)
}

func TestVectorNode_String(t *testing.T) {
	n := NewVectorNode("id5", "short", nil, nil)
	s := n.String()
	assert.Contains(t, s, "id5")

	longContent := "this is a very long content that should be truncated in the string representation"
	n2 := NewVectorNode("id6", longContent, nil, nil)
	s2 := n2.String()
	assert.Contains(t, s2, "...")
}

func TestVectorNode_ToDict_无embedding时不包含(t *testing.T) {
	n := &VectorNode{ID: "id7", Content: "text", Metadata: map[string]any{"k": "v"}}
	dict := n.ToDict()
	_, hasEmb := dict["embedding"]
	assert.False(t, hasEmb) // 无 embedding 时不输出该字段
}
