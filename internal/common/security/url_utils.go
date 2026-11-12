// Package security 提供通用安全工具。
//
// 文件目录：
//
//	security/
//	├── doc.go             # 包文档
//	├── ssl_utils.go       # SSL 上下文构建
//	├── ssl_utils_unix.go  # Unix 平台 SSL
//	├── ssl_utils_windows.go # Windows 平台 SSL
//	└── url_utils.go      # 全局代理 URL 与 NO_PROXY 匹配
//
// 对应 Python 代码：openjiuwen/core/common/security/
package security

import (
	"net"
	"net/url"
	"os"
	"strings"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetGlobalProxyURL 获取全局代理 URL（等价 get_proxy_url，无显式配置）。
// Python: core/common/security/url_utils.py:34 get_global_proxy_url
func GetGlobalProxyURL(targetURL string) string {
	return GetProxyURL(targetURL, "")
}

// GetProxyURL 获取代理 URL；目标地址命中 NO_PROXY 时返回空。
// Python: url_utils.py:40 get_proxy_url
func GetProxyURL(targetURL string, configuredProxy string) string {
	if targetURL != "" && ShouldBypassProxy(targetURL) {
		return ""
	}

	proxyURL := configuredProxy
	if proxyURL == "" {
		proxyURL = os.Getenv("http_proxy")
	}
	if proxyURL == "" {
		proxyURL = os.Getenv("https_proxy")
	}
	if proxyURL == "" {
		proxyURL = os.Getenv("HTTP_PROXY")
	}
	if proxyURL == "" {
		proxyURL = os.Getenv("HTTPS_PROXY")
	}
	if proxyURL != "" {
		return strings.TrimSpace(proxyURL)
	}
	return proxyURL
}

// ShouldBypassProxy 按 NO_PROXY 环境变量判断目标是否绕过代理。
// Python: url_utils.py:82 should_bypass_proxy
func ShouldBypassProxy(targetURL string) bool {
	hostname := extractHostname(targetURL)
	if hostname == "" {
		return false
	}
	noProxyList := getNoProxyList()
	if len(noProxyList) == 0 {
		return false
	}
	return hostnameMatchesNoProxy(hostname, noProxyList)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// extractHostname 从普通 URL 与无 scheme 的 host:port 提取主机名。
// Python: url_utils.py:91 _extract_hostname
func extractHostname(targetURL string) string {
	if parsed, err := url.Parse(targetURL); err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	if parsed, err := url.Parse("//" + targetURL); err == nil {
		return parsed.Hostname()
	}
	return ""
}

// getNoProxyList 解析 NO_PROXY 环境变量（大小写两份，去重保序）。
// Python: url_utils.py:100 _get_no_proxy_list
func getNoProxyList() []string {
	result := []string{}
	seen := map[string]bool{}
	process := func(envValue string) {
		if envValue == "" {
			return
		}
		normalized := strings.NewReplacer(" ", ",", ";", ",").Replace(envValue)
		for _, item := range strings.Split(normalized, ",") {
			item = strings.ToLower(strings.TrimSpace(item))
			if item == "" || seen[item] {
				continue
			}
			seen[item] = true
			result = append(result, item)
		}
	}
	process(os.Getenv("NO_PROXY"))
	process(os.Getenv("no_proxy"))
	return result
}

// hostnameMatchesNoProxy 主机名与 NO_PROXY 条目匹配。
// Python: url_utils.py:123 _hostname_matches_no_proxy
func hostnameMatchesNoProxy(hostname string, noProxyList []string) bool {
	hostnameLower := strings.ToLower(hostname)
	for _, entry := range noProxyList {
		// 1. 通配符 "*" 匹配一切
		if entry == "*" {
			return true
		}
		// 2. 精确域名匹配
		if entry == hostnameLower {
			return true
		}
		// 3. 后缀匹配：".example.com" 匹配 "*.example.com"
		if strings.HasPrefix(entry, ".") && strings.HasSuffix(hostnameLower, entry) {
			return true
		}
		// 4. IP/CIDR 精确匹配
		if isIPMatch(hostnameLower, entry) {
			return true
		}
	}
	return false
}

// isIPMatch 主机名为 IP 时与条目（IP 或 CIDR）比对；非 IP 返回 false。
// Python: url_utils.py:141 _is_ip_match
func isIPMatch(hostname string, entry string) bool {
	ip := net.ParseIP(hostname)
	if ip == nil {
		return false
	}
	if strings.Contains(entry, "/") {
		_, network, err := net.ParseCIDR(entry)
		if err != nil {
			return false
		}
		return network.Contains(ip)
	}
	entryIP := net.ParseIP(entry)
	return entryIP != nil && entryIP.Equal(ip)
}
