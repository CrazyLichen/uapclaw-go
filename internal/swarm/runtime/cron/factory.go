package cron

import (
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/etcd"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CronStoreSettings 定时任务存储后端配置。
// Python: common/cron_settings.py:13-19 CronStoreSettings
type CronStoreSettings struct {
	// Backend 存储后端（file 或 etcd，默认 file）
	Backend string
	// Endpoints etcd 端点列表
	Endpoints []string
	// Prefix etcd 键前缀
	Prefix string
	// FilePath 文件后端数据文件路径
	FilePath string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// CronStoreBackendFile 文件后端（个人版默认）
	// Python: common/cron_settings.py:35 backend 缺省 "file"
	CronStoreBackendFile = "file"
	// CronStoreBackendEtcd etcd 后端（HA 一体机显式开启）
	CronStoreBackendEtcd = "etcd"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// LoadCronStoreSettings 解析 cron 存储配置。
// Python: common/cron_settings.py:31-44 load_cron_store_settings
//
// 只从 `gateway.cron` 段读；etcd 是显式 opt-in
// （`gateway.cron.store_backend=etcd`），不从 AgentOS 路由或端点列表推断。
func LoadCronStoreSettings(config map[string]any, defaultFilePath string) CronStoreSettings {
	cronSection := map[string]any{}
	if gateway, ok := config["gateway"].(map[string]any); ok {
		if cronConfig, ok := gateway["cron"].(map[string]any); ok {
			cronSection = cronConfig
		}
	}

	backend := strings.ToLower(strings.TrimSpace(stringField(cronSection["store_backend"])))
	if backend == "" {
		backend = CronStoreBackendFile
	}

	prefix := strings.TrimSpace(stringField(cronSection["etcd_prefix"]))
	if prefix == "" {
		prefix = defaultEtcdPrefix
	}

	return CronStoreSettings{
		Backend:   backend,
		Endpoints: parseEtcdEndpoints(cronSection["etcd_endpoints"]),
		Prefix:    prefix,
		FilePath:  defaultFilePath,
	}
}

// CreateCronStore 按配置产出文件或 etcd 存储。
// Python: factory.py:23-53 create_gateway_cron_store
//
// etcd 后端在 endpoints 为空时**不回落**文件后端（上游显式行为）：
// 此时仍构造 etcd 存储，让后续操作以"端点为空"报错，避免静默写本地。
func CreateCronStore(settings CronStoreSettings, timeout time.Duration) CronJobStoreBackend {
	if settings.Backend != CronStoreBackendEtcd {
		return NewFileCronJobStore(settings.FilePath, 0)
	}
	return NewEtcdCronJobStore(settings.Endpoints, settings.Prefix, nil, timeout)
}

// NewEtcdClientForCron 构造 etcd 客户端（测试与外部装配用）。
// Python: etcd_store.py:50 self._client = client or EtcdJsonClient(...)
func NewEtcdClientForCron(endpoints []string, timeout time.Duration) *etcd.EtcdJsonClient {
	return etcd.NewEtcdJsonClient(endpoints, timeout)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseEtcdEndpoints 解析端点配置（数组或逗号分隔字符串）。
// Python: common/cron_settings.py:22-28 _parse_endpoints
func parseEtcdEndpoints(raw any) []string {
	switch typed := raw.(type) {
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(stringField(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(item); text != "" {
				out = append(out, text)
			}
		}
		return out
	}

	text := strings.TrimSpace(stringField(raw))
	if text == "" {
		return nil
	}
	parts := strings.Split(text, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
