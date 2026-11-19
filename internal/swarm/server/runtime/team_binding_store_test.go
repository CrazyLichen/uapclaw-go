// Python 源：team_binding_store.py 用例 1:1 转 Go（T6）

package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateTeamName(t *testing.T) {
	normalized, err := ValidateTeamName("  dev team ")
	require.NoError(t, err)
	assert.Equal(t, "dev team", normalized)
	_, err = ValidateTeamName("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "team_name is required")
	_, err = ValidateTeamName("a/b")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "1-64 characters")
}

func TestTeamBindingStore_CRUD(t *testing.T) {
	dir := t.TempDir()
	store := NewTeamBindingStore(filepath.Join(dir, "bindings.json"))

	created, err := store.Create("t1", "tpl-1", map[string]any{"k": "v"}, false)
	require.NoError(t, err)
	assert.Equal(t, "t1", created.TeamName)
	// 重名 CONFLICT
	_, err = store.Create("t1", "tpl-2", nil, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
	// Get
	assert.NotNil(t, store.Get("t1"))
	assert.Nil(t, store.Get("missing"))
	// BindSession + 跨团解除
	_, err = store.BindSession("t1", "s1")
	require.NoError(t, err)
	store.Create("t2", "tpl-1", nil, false)
	_, err = store.BindSession("t2", "s1")
	require.NoError(t, err)
	b1 := store.Get("t1")
	assert.NotContains(t, b1.SessionIDs, "s1") // 被转移到 t2
	b2 := store.Get("t2")
	assert.Contains(t, b2.SessionIDs, "s1")
	// UnbindSession
	unbound, err := store.UnbindSession("s1", "")
	require.NoError(t, err)
	require.NotNil(t, unbound)
	assert.Empty(t, unbound.SessionIDs)
	// Delete
	assert.True(t, store.Delete("t1"))
	assert.False(t, store.Delete("t1"))
}

func TestTeamBindingStore_模型选择(t *testing.T) {
	dir := t.TempDir()
	store := NewTeamBindingStore(filepath.Join(dir, "bindings.json"))
	_, err := store.Create("t1", "tpl-1", nil, false)
	require.NoError(t, err)
	_, err = store.SetTeamSelection("t1", map[string]any{"mode": "model", "model_name": "m1"})
	require.NoError(t, err)
	selection := store.GetTeamSelection("t1")
	require.NotNil(t, selection)
	assert.Equal(t, "m1", selection["model_name"])
	// 未绑定团得 nil
	assert.Nil(t, store.GetTeamSelection("missing"))
}

func TestTeamBindingStore_持久化往返(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bindings.json")
	store := NewTeamBindingStore(path)
	_, err := store.Create("t1", "tpl-1", map[string]any{"k": 1}, false)
	require.NoError(t, err)
	_, err = store.BindSession("t1", "s1")
	require.NoError(t, err)
	// 新 store 实例读回（含 legacy 迁移不覆盖已有文件）；
	// 注意 s1 已在前半被转移到 t2，t1 不再持有它。
	store2 := NewTeamBindingStore(path)
	binding := store2.Get("t1")
	require.NotNil(t, binding)
	assert.Equal(t, "tpl-1", binding.TemplateID)
	assert.Empty(t, binding.SessionIDs)
	t2Binding := store2.Get("t2")
	require.NotNil(t, t2Binding)
	assert.Equal(t, []string{"s1"}, t2Binding.SessionIDs)
	assert.Equal(t, float64(1), binding.TemplateSnapshot["k"])
	// legacy 迁移：目标文件不存在时搬移旧目录
	legacyDir := t.TempDir()
	legacyPath := filepath.Join(legacyDir, "teams", "bindings.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(legacyPath), 0o755))
	require.NoError(t, os.WriteFile(legacyPath, []byte(`{"teams":{"old":{"team_name":"old","template_id":"t"}}}`), 0o644))
	target := filepath.Join(legacyDir, "new", "bindings.json")
	legacyStore := NewTeamBindingStore(target)
	legacyStore.migrateLegacyFile(legacyPath)
	assert.NotNil(t, legacyStore.Get("old"))
	_, statErr := os.Stat(legacyPath)
	assert.True(t, os.IsNotExist(statErr))
}
