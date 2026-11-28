package personal_context

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigFromDict_完整配置归一化(t *testing.T) {
	root := t.TempDir()
	config, err := ConfigFromDict(map[string]any{
		"collection_enabled": true,
		"agent_use_enabled":  false,
		"strategy_profile":   "rules",
		"fetch_services": []any{
			map[string]any{
				"service_id": "local",
				"provider":   "local_files",
				"enabled":    true,
				"time_range": map[string]any{"mode": "all"},
				"source":     map[string]any{"root_dir": root},
			},
			map[string]any{
				"service_id": "feed",
				"provider":   "rss_feed",
				"enabled":    true,
				"time_range": map[string]any{"mode": "all"},
				"source":     map[string]any{"feed_url": "https://example.com/feed.xml"},
			},
		},
	})
	require.NoError(t, err)
	require.True(t, config.CollectionEnabled)
	require.Equal(t, "rules", config.StrategyProfile)
	require.Equal(t, DefaultMaxPagesPerDirectory, config.MaxPagesPerDirectory)
	// fetch_services 按 service_id 排序
	require.Equal(t, "feed", config.FetchServices[0].ServiceID)
	require.Equal(t, "local", config.FetchServices[1].ServiceID)
	// 零凭据源传 credentials 报错
	_, err = ConfigFromDict(map[string]any{
		"strategy_profile": "rules",
		"fetch_services": []any{map[string]any{
			"service_id": "local", "provider": "local_files", "enabled": true,
			"time_range":  map[string]any{"mode": "all"},
			"source":      map[string]any{"root_dir": root},
			"credentials": map[string]any{"token": "x"},
		}},
	})
	require.ErrorContains(t, err, "does not accept credentials")
}

func TestConfigFromDict_仓库类凭据与白名单(t *testing.T) {
	base := map[string]any{"strategy_profile": "rules", "fetch_services": []any{
		map[string]any{
			"service_id": "gh", "provider": "github", "enabled": true,
			"time_range": map[string]any{"mode": "all"},
			"source":     map[string]any{"owner": "org", "repo": "repo"},
		},
	}}
	// github 凭据缺 token 报错
	_, err := ConfigFromDict(base)
	require.ErrorContains(t, err, "github credentials must contain only token")

	base["fetch_services"].([]any)[0].(map[string]any)["credentials"] = map[string]any{"token": "t"}
	config, err := ConfigFromDict(base)
	require.NoError(t, err)
	require.Equal(t, []any{"readme", "issues", "pull_requests", "commits", "code"},
		config.FetchServices[0].Source["resources"])
	require.Equal(t, map[string]string{"token": "t"}, config.FetchServices[0].Credentials)

	// resources 白名单外报错
	base["fetch_services"].([]any)[0].(map[string]any)["source"] =
		map[string]any{"owner": "org", "repo": "repo", "resources": []any{"wiki"}}
	_, err = ConfigFromDict(base)
	require.ErrorContains(t, err, "contains an unsupported resource")
}

func TestConfigFromDict_语义校验(t *testing.T) {
	service := map[string]any{
		"service_id": "s1", "provider": "rss_feed", "enabled": true,
		"time_range": map[string]any{"mode": "all"},
		"source":     map[string]any{"feed_url": "https://example.com/feed"},
	}
	// balanced 需要 model 配对
	_, err := ConfigFromDict(map[string]any{
		"strategy_profile": "balanced", "fetch_services": []any{service},
	})
	require.ErrorContains(t, err, "balanced strategy requires model_client and model_request")

	// service_id 重复
	dup := map[string]any{"service_id": "s1", "provider": "rss_feed", "enabled": true,
		"time_range": map[string]any{"mode": "all"},
		"source":     map[string]any{"feed_url": "https://example.com/feed2"}}
	_, err = ConfigFromDict(map[string]any{
		"strategy_profile": "rules", "fetch_services": []any{service, dup},
	})
	require.ErrorContains(t, err, "must be unique")

	// strategy_profile 非法
	_, err = ConfigFromDict(map[string]any{
		"strategy_profile": "magic", "fetch_services": []any{service},
	})
	require.ErrorContains(t, err, "strategy_profile")
}

func TestNormalizeServiceSource_各源分支(t *testing.T) {
	root := t.TempDir()
	// zhihu 列 URL
	result, err := normalizeServiceSource("zhihu_reader",
		map[string]any{"column_url": "https://zhuanlan.zhihu.com/column/abc"})
	require.NoError(t, err)
	require.Equal(t, "https://zhuanlan.zhihu.com/column/abc", result["column_url"])

	// zhihu 非 column 域拒绝
	_, err = normalizeServiceSource("zhihu_reader",
		map[string]any{"column_url": "https://evil.com/column/abc"})
	require.ErrorContains(t, err, "must point to zhihu.com")

	// rss feed
	result, err = normalizeServiceSource("rss_feed",
		map[string]any{"feed_url": "https://example.com/feed"})
	require.NoError(t, err)
	require.Equal(t, "https://example.com/feed", result["feed_url"])

	// toutiao 尾斜杠保留
	result, err = normalizeServiceSource("toutiao_reader",
		map[string]any{"profile_url": "https://www.toutiao.com/c/user/token/123/"})
	require.NoError(t, err)
	require.Equal(t, "https://www.toutiao.com/c/user/token/123/", result["profile_url"])

	// feishu account 双通道拒绝
	_, err = normalizeServiceSource("feishu", map[string]any{
		"mode": "account", "resources": []any{"docs"},
		"document_ids": []any{"d1"}, "query": "q",
	})
	require.ErrorContains(t, err, "not both")

	// local_files 多字段拒绝
	_, err = normalizeServiceSource("local_files",
		map[string]any{"root_dir": root, "extra": 1})
	require.ErrorContains(t, err, "must contain only root_dir")

	// 未知 provider
	_, err = normalizeServiceSource("mystery", map[string]any{})
	require.ErrorContains(t, err, "unsupported provider")
}

func TestNormalizeTimeRange_三模式(t *testing.T) {
	result, err := normalizeTimeRange(map[string]any{"mode": "recent", "recent_days": float64(7)})
	require.NoError(t, err)
	require.Equal(t, 7, result["recent_days"])

	_, err = normalizeTimeRange(map[string]any{"mode": "recent", "recent_days": float64(0)})
	require.ErrorContains(t, err, "positive integer")

	result, err = normalizeTimeRange(map[string]any{"mode": "fixed",
		"start_at": "2026-01-01T00:00:00Z", "end_at": "2026-02-01T00:00:00Z"})
	require.NoError(t, err)
	require.Equal(t, "2026-01-01T00:00:00Z", result["start_at"])

	_, err = normalizeTimeRange(map[string]any{"mode": "fixed",
		"start_at": "2026-02-01T00:00:00Z", "end_at": "2026-01-01T00:00:00Z"})
	require.ErrorContains(t, err, "must be before end_at")
}

func TestNormalizePath_敏感路径与相对路径(t *testing.T) {
	_, err := normalizePath("relative/path", "root_dir")
	require.ErrorContains(t, err, "must be absolute or start with '~'")

	home, _ := userHomeDir()
	_, err = normalizePath("~", "root_dir")
	require.NoError(t, err)

	// Windows 敏感路径模式
	if os.Getenv("GO_TEST_SENSITIVE") == "" {
		_ = filepath.Join(home, "AppData")
	}
	_, err = normalizePath("C:\\Windows\\system32", "root_dir")
	// 非 Windows 下 EvalSymlinks 可能失败回落 Abs，敏感路径正则按 windows 形态匹配
	require.Error(t, err)
}
