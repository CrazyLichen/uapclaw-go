package wsorigin

import (
	"net/url"
	"os"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OriginChecker WebSocket Origin 安全校验器。
//
// 对应 Python: jiuwenswarm/common/security/ws_origin.py 全部函数
//
// 在 WebSocket 握手阶段校验浏览器 Origin 头，防止跨站 WebSocket 劫持（CSWSH）。
// 每次调用 IsEnabled/GetAllowedHosts/IsAllowed 时重新读取环境变量，支持热生效。
//
// 使用方式：
//
//	checker := wsorigin.NewOriginChecker()
//	if checker.IsEnabled() && !checker.IsAllowed(origin) {
//	    // 拒绝连接
//	}
type OriginChecker struct {
	// enableCheckEnv 控制是否启用校验的环境变量名
	enableCheckEnv string
	// allowedHostsEnv 白名单环境变量名
	allowedHostsEnv string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultEnableCheckEnv 默认的总开关环境变量名。
	// 对应 Python: JIUWENSWARM_ENABLE_ORIGIN_CHECK
	DefaultEnableCheckEnv = "UAPCLAW_ENABLE_ORIGIN_CHECK"

	// DefaultAllowedHostsEnv 默认的白名单环境变量名。
	// 对应 Python: JIUWENSWARM_WS_ALLOWED_ORIGIN_HOSTS
	DefaultAllowedHostsEnv = "UAPCLAW_WS_ALLOWED_ORIGIN_HOSTS"

	// noneKeyword 特殊关键词，白名单中包含此值时允许无 Origin 的请求。
	// 对应 Python: is_allowed_browser_origin 中 origin 为 None 时检查 "none" in allowed_hosts
	noneKeyword = "none"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOriginChecker 创建 Origin 校验器，使用默认环境变量名。
func NewOriginChecker() *OriginChecker {
	return NewOriginCheckerWithEnv(DefaultEnableCheckEnv, DefaultAllowedHostsEnv)
}

// NewOriginCheckerWithEnv 创建 Origin 校验器，使用自定义环境变量名。
// 用于测试场景或需要多实例不同配置的场景。
func NewOriginCheckerWithEnv(enableCheckEnv, allowedHostsEnv string) *OriginChecker {
	return &OriginChecker{
		enableCheckEnv:  enableCheckEnv,
		allowedHostsEnv: allowedHostsEnv,
	}
}

// ──────────────────────────── 方法 ────────────────────────────

// IsEnabled 返回是否启用了 Origin 校验。
//
// 对应 Python: is_origin_check_enabled()
// 环境变量值为 "1" 时启用，未设置或其他值时禁用。
func (c *OriginChecker) IsEnabled() bool {
	return strings.TrimSpace(os.Getenv(c.enableCheckEnv)) == "1"
}

// GetAllowedHosts 返回当前白名单主机名集合（小写）。
//
// 对应 Python: get_allowed_origin_hosts()
// 每次调用重新读取环境变量，支持热生效。
// 环境变量为逗号分隔的主机名，空项和前后空格会被忽略。
func (c *OriginChecker) GetAllowedHosts() map[string]bool {
	raw := os.Getenv(c.allowedHostsEnv)
	hosts := make(map[string]bool)
	if raw == "" {
		return hosts
	}
	for _, item := range strings.Split(raw, ",") {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			hosts[strings.ToLower(trimmed)] = true
		}
	}
	return hosts
}

// IsAllowed 校验 Origin 是否允许。
//
// 对应 Python: is_allowed_browser_origin(origin)
//   - origin 为空字符串 → 白名单含 "none" 才放行（允许非浏览器客户端）
//   - URL 解析失败 → 拒绝
//   - 取 hostname 小写匹配白名单
func (c *OriginChecker) IsAllowed(origin string) bool {
	allowedHosts := c.GetAllowedHosts()

	// 无 Origin 头（非浏览器客户端），检查白名单是否包含 "none"
	// 对应 Python: if origin is None: return "none" in allowed_hosts
	if origin == "" {
		return allowedHosts[noneKeyword]
	}

	// 解析 Origin URL
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}

	// 取 hostname 小写匹配白名单
	// 对应 Python: (parsed.hostname or "").lower() in allowed_hosts
	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "" {
		return false
	}
	return allowedHosts[hostname]
}

// ForbiddenResponse 返回校验失败时的 HTTP 响应信息。
//
// 对应 Python: forbidden_origin_response()
// 返回值：HTTP 状态码 403、响应头、响应体。
// 调用方可根据返回值自行构造 http.ResponseWriter 响应。
func (c *OriginChecker) ForbiddenResponse() (code int, headers map[string][]string, body string) {
	return 403, map[string][]string{
		"Content-Type": {"text/plain; charset=utf-8"},
	}, forbiddenBody
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// forbiddenBody 校验失败时的响应体。
// 对应 Python: _FORBIDDEN_BODY = b"Forbidden: Origin not allowed\n"
var forbiddenBody = "Forbidden: Origin not allowed\n"
