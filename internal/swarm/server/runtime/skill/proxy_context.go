package skill

import "os"

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// skillnetNetworkContext 在 SkillNet 调用期间临时设置代理环境变量，调用结束后恢复。
// 对齐 Python: _skillnet_network_context (skill_manager.py L49-58, L172-193)
//
// 返回恢复函数，调用方应 defer 调用以恢复原始环境变量。
func skillnetNetworkContext() func() {
	proxyURL := os.Getenv("FREE_SEARCH_PROXY_URL")
	if proxyURL == "" {
		return func() {} // 无需设置
	}

	// 对齐 Python: _SKILLNET_PROXY_ENV_KEYS
	proxyKeys := []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"}
	// 对齐 Python: _SKILLNET_NO_PROXY_ENV_KEYS
	noProxyKeys := []string{"NO_PROXY", "no_proxy"}
	// 对齐 Python: _FREE_SEARCH_DEFAULT_NO_PROXY
	defaultNoProxy := "127.0.0.1,.huawei.com,localhost,local,.local,10.155.97.247,.myhuaweicloud.com"

	// 保存所有代理和 NO_PROXY 环境变量的原始值
	prev := make(map[string]string)
	for _, k := range append(proxyKeys, noProxyKeys...) {
		prev[k] = os.Getenv(k)
	}

	// 设置代理环境变量
	for _, k := range proxyKeys {
		_ = os.Setenv(k, proxyURL)
	}

	// 对齐 Python: if not os.environ.get("NO_PROXY") and not os.environ.get("no_proxy"):
	if os.Getenv("NO_PROXY") == "" && os.Getenv("no_proxy") == "" {
		for _, k := range noProxyKeys {
			_ = os.Setenv(k, defaultNoProxy)
		}
	}

	// 返回恢复函数
	return func() {
		for k, v := range prev {
			if v == "" {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, v)
			}
		}
	}
}
