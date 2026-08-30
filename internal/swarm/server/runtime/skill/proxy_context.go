package skill

import "os"

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// skillnetNetworkContext 在 SkillNet 调用期间临时设置代理环境变量，调用结束后恢复。
// 对齐 Python: _skillnet_network_context
//
// 返回恢复函数，调用方应 defer 调用以恢复原始环境变量。
func skillnetNetworkContext() func() {
	proxyURL := os.Getenv("FREE_SEARCH_PROXY_URL")
	if proxyURL == "" {
		return func() {} // 无需设置
	}

	// 保存原始值
	origHTTPProxy := os.Getenv("HTTP_PROXY")
	origHTTPSProxy := os.Getenv("HTTPS_PROXY")
	origAllProxy := os.Getenv("ALL_PROXY")

	// 设置代理（对齐 Python: os.environ["HTTP_PROXY"] = proxy_url 等）
	_ = os.Setenv("HTTP_PROXY", proxyURL)
	_ = os.Setenv("HTTPS_PROXY", proxyURL)
	_ = os.Setenv("ALL_PROXY", proxyURL)

	// 返回恢复函数
	return func() {
		_ = os.Setenv("HTTP_PROXY", origHTTPProxy)
		_ = os.Setenv("HTTPS_PROXY", origHTTPSProxy)
		_ = os.Setenv("ALL_PROXY", origAllProxy)
	}
}
