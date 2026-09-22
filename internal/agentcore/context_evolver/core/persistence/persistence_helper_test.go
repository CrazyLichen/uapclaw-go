package persistence

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMemoryPersistenceHelper(t *testing.T) {
	h := NewMemoryPersistenceHelper()
	assert.NotNil(t, h)
	assert.Equal(t, "json", h.PersistType()) // P1 默认 json
}

func TestNewMemoryPersistenceHelper_自定义选项(t *testing.T) {
	h := NewMemoryPersistenceHelper(
		WithPersistType("json"),
		WithPersistPath("./custom/{algo_name}/{user_id}.json"),
		WithMilvusHost("my-host"),
		WithMilvusPort(19530),
		WithMilvusCollection("test_coll"),
	)
	assert.Equal(t, "json", h.PersistType())
	assert.Equal(t, "./custom/{algo_name}/{user_id}.json", h.PersistPath())
	assert.Equal(t, "my-host", h.MilvusHost())
	assert.Equal(t, 19530, h.MilvusPort())
	assert.Equal(t, "test_coll", h.MilvusCollection())
}

func TestMemoryPersistenceHelper_Save_Load_JSON往返(t *testing.T) {
	dir := t.TempDir()
	h := NewMemoryPersistenceHelper(
		WithPersistPath(dir+"/{algo_name}/{user_id}.json"),
	)

	nodes := map[string]any{
		"node1": map[string]any{"id": "node1", "content": "hello", "metadata": map[string]any{}},
	}

	require.NoError(t, h.Save("alice", "ace", nodes))

	loaded, err := h.Load("alice", "ace")
	require.NoError(t, err)
	assert.Equal(t, "hello", loaded["node1"].(map[string]any)["content"])
}

func TestMemoryPersistenceHelper_Save_空数据(t *testing.T) {
	h := NewMemoryPersistenceHelper(WithPersistPath(t.TempDir()+"/{algo_name}/{user_id}.json"))
	err := h.Save("alice", "ace", nil)
	assert.NoError(t, err) // 空数据直接返回，对齐 Python
}

func TestMemoryPersistenceHelper_Load_不存在(t *testing.T) {
	h := NewMemoryPersistenceHelper(WithPersistPath(t.TempDir()+"/{algo_name}/{user_id}.json"))
	loaded, err := h.Load("alice", "ace")
	require.NoError(t, err)
	assert.Empty(t, loaded) // 对齐 Python：文件不存在返回 {}
}

func TestMemoryPersistenceHelper_Save_合并(t *testing.T) {
	dir := t.TempDir()
	h := NewMemoryPersistenceHelper(WithPersistPath(dir + "/{algo_name}/{user_id}.json"))

	require.NoError(t, h.Save("alice", "ace", map[string]any{"n1": map[string]any{"id": "n1", "content": "a"}}))
	require.NoError(t, h.Save("alice", "ace", map[string]any{"n2": map[string]any{"id": "n2", "content": "b"}}))

	loaded, err := h.Load("alice", "ace")
	require.NoError(t, err)
	_, hasN1 := loaded["n1"]
	_, hasN2 := loaded["n2"]
	assert.True(t, hasN1) // 旧数据保留
	assert.True(t, hasN2) // 新数据存在
}

func TestMemoryPersistenceHelper_路径模板替换(t *testing.T) {
	dir := t.TempDir()
	h := NewMemoryPersistenceHelper(WithPersistPath(dir + "/{algo_name}/{user_id}.json"))
	require.NoError(t, h.Save("bob", "rb", map[string]any{"n1": map[string]any{"id": "n1", "content": "x"}}))

	loaded, err := h.Load("bob", "rb")
	require.NoError(t, err)
	assert.NotEmpty(t, loaded)

	// 验证其他算法名/用户名不影响
	loadedOther, err := h.Load("bob", "ace")
	require.NoError(t, err)
	assert.Empty(t, loadedOther)
}

func TestMemoryPersistenceHelper_SetMilvusConnector(t *testing.T) {
	h := NewMemoryPersistenceHelper()
	mock := &mockMilvusConnector{}
	h.SetMilvusConnector(mock)
	// 通过行为间接验证：SetMilvusConnector 不报错即表示注入成功
	// P7 启用 Milvus 后，可验证 Save/Load 走 Milvus 后端
	assert.Equal(t, "json", h.ResolvedType()) // P1 仍为 json，P7 auto 模式下会变
}

func TestMemoryPersistenceHelper_ResolvedType(t *testing.T) {
	h := NewMemoryPersistenceHelper()
	assert.Equal(t, "json", h.ResolvedType()) // P1 固定 json
}

func TestMemoryPersistenceHelper_Namespace(t *testing.T) {
	ns := Namespace("alice", "ace")
	assert.Equal(t, "memory_ace_alice", ns) // 对齐 Python _namespace
}

func TestMemoryPersistenceHelper_String(t *testing.T) {
	h := NewMemoryPersistenceHelper()
	s := h.String()
	assert.Contains(t, s, "json")
}

// mockMilvusConnector MilvusConnector 的 mock 实现
type mockMilvusConnector struct{}

func (m *mockMilvusConnector) SaveToDB(_ string, _ map[string]any) error  { return nil }
func (m *mockMilvusConnector) LoadFromDB(_ string) (map[string]any, error) { return nil, nil }
func (m *mockMilvusConnector) Exists(_ string) bool                        { return false }
func (m *mockMilvusConnector) Delete(_ string) bool                        { return false }
