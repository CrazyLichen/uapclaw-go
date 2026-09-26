package persistence

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMemoryPersistenceHelper(t *testing.T) {
	h := NewMemoryPersistenceHelper()
	assert.NotNil(t, h)
	assert.Equal(t, "auto", h.PersistType()) // P7 默认 auto（对齐 Python）
	assert.Equal(t, "", h.ResolvedType())    // auto 模式下待探测
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

// ──────────────────────────── auto 模式测试 ────────────────────────────

func TestAuto_默认Auto(t *testing.T) {
	h := NewMemoryPersistenceHelper()
	assert.Equal(t, "auto", h.PersistType())
	assert.Equal(t, "", h.ResolvedType()) // 未探测前为空
}

func TestAuto_Milvus可达(t *testing.T) {
	// 注入 mock MilvusConnector，probeMilvus 对非 MilvusConnectorImpl 总是返回 true
	h := NewMemoryPersistenceHelper(WithPersistType("auto"))
	h.SetMilvusConnector(&mockMilvusConnector{})

	// 触发探测（通过 Save）
	err := h.Save("alice", "ace", map[string]any{"n1": map[string]any{"id": "n1", "content": "hello"}})
	require.NoError(t, err)
	assert.Equal(t, "milvus", h.ResolvedType()) // 探测到可达 → milvus
}

func TestAuto_Milvus不可达(t *testing.T) {
	// 不注入 MilvusConnector，milvusHost 为默认 localhost（无真实 Milvus 服务）
	// auto 模式探测失败后回退 JSON
	h := NewMemoryPersistenceHelper(
		WithPersistType("auto"),
		WithPersistPath(t.TempDir()+"/{algo_name}/{user_id}.json"),
		WithMilvusHost(""), // 空 host → 不会自动创建 MilvusConnectorImpl
	)

	err := h.Save("alice", "ace", map[string]any{"n1": map[string]any{"id": "n1", "content": "hello"}})
	require.NoError(t, err)
	assert.Equal(t, "json", h.ResolvedType()) // 不可达 → 回退 json
}

func TestAuto_显式JSON(t *testing.T) {
	h := NewMemoryPersistenceHelper(
		WithPersistType("json"),
		WithPersistPath(t.TempDir()+"/{algo_name}/{user_id}.json"),
	)
	// 显式 json 不需要探测，首次 Save 时 resolveBackend 直接设为 json
	err := h.Save("alice", "ace", map[string]any{"n1": map[string]any{"id": "n1", "content": "x"}})
	require.NoError(t, err)
	assert.Equal(t, "json", h.ResolvedType())
}

func TestAuto_显式Milvus(t *testing.T) {
	h := NewMemoryPersistenceHelper(WithPersistType("milvus"))
	h.SetMilvusConnector(&mockMilvusConnector{}) // 注入 mock 防止 nil 调用
	// 显式 milvus 不探测，直接设为 milvus
	_ = h.Save("alice", "ace", map[string]any{"n1": map[string]any{"id": "n1", "content": "x"}})
	assert.Equal(t, "milvus", h.ResolvedType())
}

// ──────────────────────────── JSON 后端测试 ────────────────────────────

func TestMemoryPersistenceHelper_Save_Load_JSON往返(t *testing.T) {
	dir := t.TempDir()
	h := NewMemoryPersistenceHelper(
		WithPersistType("json"),
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
	h := NewMemoryPersistenceHelper(
		WithPersistType("json"),
		WithPersistPath(t.TempDir()+"/{algo_name}/{user_id}.json"),
	)
	err := h.Save("alice", "ace", nil)
	assert.NoError(t, err) // 空数据直接返回，对齐 Python
}

func TestMemoryPersistenceHelper_Load_不存在(t *testing.T) {
	h := NewMemoryPersistenceHelper(
		WithPersistType("json"),
		WithPersistPath(t.TempDir()+"/{algo_name}/{user_id}.json"),
	)
	loaded, err := h.Load("alice", "ace")
	require.NoError(t, err)
	assert.Empty(t, loaded) // 对齐 Python：文件不存在返回 {}
}

func TestMemoryPersistenceHelper_Save_合并(t *testing.T) {
	dir := t.TempDir()
	h := NewMemoryPersistenceHelper(
		WithPersistType("json"),
		WithPersistPath(dir+"/{algo_name}/{user_id}.json"),
	)

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
	h := NewMemoryPersistenceHelper(
		WithPersistType("json"),
		WithPersistPath(dir+"/{algo_name}/{user_id}.json"),
	)
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
	h := NewMemoryPersistenceHelper(WithPersistType("auto"))
	mock := &mockMilvusConnector{}
	h.SetMilvusConnector(mock)
	// 注入后未调用 Save/Load，resolvedType 仍为空
	assert.Equal(t, "", h.ResolvedType())
}

func TestMemoryPersistenceHelper_ResolvedType(t *testing.T) {
	h := NewMemoryPersistenceHelper()
	assert.Equal(t, "", h.ResolvedType()) // auto 模式未探测前为空
}

func TestMemoryPersistenceHelper_Namespace(t *testing.T) {
	ns := Namespace("alice", "ace")
	assert.Equal(t, "memory_ace_alice", ns) // 对齐 Python _namespace
}

func TestMemoryPersistenceHelper_String(t *testing.T) {
	h := NewMemoryPersistenceHelper()
	s := h.String()
	assert.Contains(t, s, "auto") // P7 默认 persistType=auto
}

// ──────────────────────────── mock 实现 ────────────────────────────

// mockMilvusConnector MilvusConnector 的 mock 实现
type mockMilvusConnector struct{}

func (m *mockMilvusConnector) SaveToDB(_ context.Context, _ string, _ map[string]any) error   { return nil }
func (m *mockMilvusConnector) LoadFromDB(_ context.Context, _ string) (map[string]any, error) { return nil, nil }
func (m *mockMilvusConnector) Exists(_ context.Context, _ string) bool                        { return false }
func (m *mockMilvusConnector) Delete(_ context.Context, _ string) bool                        { return false }
