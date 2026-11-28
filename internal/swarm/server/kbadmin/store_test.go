package kbadmin

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOpenStore_AutoMigrate 测试打开存储并自动建齐五表
func TestOpenStore_AutoMigrate(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "kbadmin.db")
	st, err := OpenStore(dbPath)
	require.NoError(t, err)
	defer func() { require.NoError(t, st.Close()) }()

	// 五表全部可写（AutoMigrate 已建）
	for _, m := range AllModels() {
		require.NoError(t, st.DB().Create(m).Error)
	}
}

// TestOpenStore_重复打开幂等 测试二次 AutoMigrate 幂等（存量库升级场景）
func TestOpenStore_重复打开幂等(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "kbadmin.db")
	st1, err := OpenStore(dbPath)
	require.NoError(t, err)
	require.NoError(t, st1.Close())

	st2, err := OpenStore(dbPath)
	require.NoError(t, err)
	defer func() { require.NoError(t, st2.Close()) }()
}
