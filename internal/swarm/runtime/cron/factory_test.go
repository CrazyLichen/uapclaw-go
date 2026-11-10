package cron

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 配置解析 ────────────────────────────

// TestLoadCronStoreSettings_默认文件后端 无配置时 backend=file。
// Python: common/cron_settings.py:31-44
func TestLoadCronStoreSettings_默认文件后端(t *testing.T) {
	settings := LoadCronStoreSettings(nil, "/tmp/cron.json")
	require.Equal(t, CronStoreBackendFile, settings.Backend)
	require.Equal(t, "/tmp/cron.json", settings.FilePath)
	require.Equal(t, defaultEtcdPrefix, settings.Prefix)
	require.Empty(t, settings.Endpoints)
}

// TestLoadCronStoreSettings_etcd配置 显式 opt-in 才用 etcd。
// Python: common/cron_settings.py:35-40
func TestLoadCronStoreSettings_etcd配置(t *testing.T) {
	settings := LoadCronStoreSettings(map[string]any{
		"gateway": map[string]any{
			"cron": map[string]any{
				"store_backend":  "ETCD",
				"etcd_endpoints": []any{"127.0.0.1:2379", " 10.0.0.2:2379 "},
				"etcd_prefix":    "/custom/prefix/",
			},
		},
	}, "/tmp/cron.json")

	require.Equal(t, CronStoreBackendEtcd, settings.Backend, "小写化")
	require.Equal(t, []string{"127.0.0.1:2379", "10.0.0.2:2379"}, settings.Endpoints)
	require.Equal(t, "/custom/prefix/", settings.Prefix)
}

// TestLoadCronStoreSettings_逗号分隔端点 字符串端点按逗号切分。
// Python: common/cron_settings.py:22-28 _parse_endpoints
func TestLoadCronStoreSettings_逗号分隔端点(t *testing.T) {
	settings := LoadCronStoreSettings(map[string]any{
		"gateway": map[string]any{
			"cron": map[string]any{"etcd_endpoints": "a:2379, b:2379"},
		},
	}, "")
	require.Equal(t, []string{"a:2379", "b:2379"}, settings.Endpoints)
}

// TestLoadCronStoreSettings_不从端点推断 etcd 端点非空但 backend 未指定时仍是 file。
// Python: factory.py:30-32 "etcd is not inferred from ... non-empty endpoints list"
func TestLoadCronStoreSettings_不从端点推断(t *testing.T) {
	settings := LoadCronStoreSettings(map[string]any{
		"gateway": map[string]any{
			"cron": map[string]any{"etcd_endpoints": []any{"a:2379"}},
		},
	}, "/tmp/cron.json")
	require.Equal(t, CronStoreBackendFile, settings.Backend, "端点存在也不推断 etcd")
}

// ──────────────────────────── 存储构造 ────────────────────────────

// TestCreateCronStore_文件后端 默认配置产出文件存储。
// Python: factory.py:38-40
func TestCreateCronStore_文件后端(t *testing.T) {
	store := CreateCronStore(CronStoreSettings{
		Backend:  CronStoreBackendFile,
		FilePath: t.TempDir() + "/cron.json",
	}, time.Second)

	fileStore, ok := store.(*FileCronJobStore)
	require.True(t, ok)
	require.False(t, fileStore.SupportsWatch())
}

// TestCreateCronStore_etcd后端 etcd 配置产出 etcd 存储。
// Python: factory.py:44-52
func TestCreateCronStore_etcd后端(t *testing.T) {
	store := CreateCronStore(CronStoreSettings{
		Backend:   CronStoreBackendEtcd,
		Endpoints: []string{"127.0.0.1:2379"},
	}, time.Second)

	etcdStore, ok := store.(*EtcdCronJobStore)
	require.True(t, ok)
	require.True(t, etcdStore.SupportsWatch())
}

// TestCreateCronStore_etcd空端点不回落 etcd 端点为空时不回落文件后端。
// Python: factory.py:46-50 "not falling back to local file"
func TestCreateCronStore_etcd空端点不回落(t *testing.T) {
	store := CreateCronStore(CronStoreSettings{
		Backend: CronStoreBackendEtcd,
	}, time.Second)

	_, ok := store.(*EtcdCronJobStore)
	require.True(t, ok, "空端点仍构造 etcd 存储，避免静默写本地")
}

// ──────────────────────────── 前缀归一 ────────────────────────────

// TestNewEtcdCronJobStore_前缀补斜杠 缺尾斜杠自动补齐。
// Python: etcd_store.py:51-53
func TestNewEtcdCronJobStore_前缀补斜杠(t *testing.T) {
	store := NewEtcdCronJobStore(nil, "/custom", nil, time.Second)
	require.Equal(t, "/custom/", store.Prefix())
}

// TestNewEtcdCronJobStore_空前缀用默认 空前缀回默认前缀。
func TestNewEtcdCronJobStore_空前缀用默认(t *testing.T) {
	store := NewEtcdCronJobStore(nil, "", nil, time.Second)
	require.Equal(t, defaultEtcdPrefix, store.Prefix())
}
