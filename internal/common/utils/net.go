// utils 包提供通用工具函数。
//
// net.go 实现网络相关工具：本机 IP 获取、URL 密码脱敏。
// 对应 Python：
//   - openjiuwen/core/common/utils/ip_utils.py
//   - openjiuwen/core/common/utils/url_utils.py
//
// HTTP 头清洗相关功能已迁移至：
//   - internal/agentcore/foundation/llm/headers_helper/ (SanitizeHeaders, ProtectedHeaders 等)

package utils

import (
	"net"
	"net/url"
	"regexp"
	"strings"
)

// urlPattern 匹配以 scheme:// 开头的 URL 字符串。
// 对应 Python: _URL_PATTERN = re.compile(r"^[a-zA-Z][a-zA-Z0-9+.-]*://")
// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var urlPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://`)

// GetLocalIP 获取本机可用 IPv4 地址（排除 127.0.0.1）。
// 对应 Python: get_local_ip()
// 通过向公共 DNS（8.8.8.8:80）发起 UDP 连接来检测出口 IP，
// 不实际发送数据，仅利用 socket 获取本地地址。
// 如果检测失败，回退到 "127.0.0.1"。
// ──────────────────────────── 导出函数 ────────────────────────────

func GetLocalIP() string {
	conn, err := net.DialTimeout("udp4", "8.8.8.8:80", 0)
	if err != nil {
		return "127.0.0.1"
	}
	defer func() { _ = conn.Close() }()

	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || addr.IP.IsLoopback() {
		return "127.0.0.1"
	}
	return addr.IP.String()
}

// RedactURLPassword 对 URL 中的密码进行脱敏，用于安全日志。
//
// 对应 Python: redact_url_password()
// 将 URL 中的密码替换为 "***"，不包含密码的 URL 原样返回。
// 解析失败时返回原始字符串。
//
// 示例：
//
//	RedactURLPassword("redis://:secret@host:6379/0") → "redis://:***@host:6379/0"
//	RedactURLPassword("redis://user:secret@host:6379/0") → "redis://user:***@host:6379/0"
//	RedactURLPassword("redis://host:6379/0") → "redis://host:6379/0"
func RedactURLPassword(rawURL string) string {
	if rawURL == "" {
		return rawURL
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	// 没有密码，无需脱敏
	if parsed.User == nil {
		return rawURL
	}
	password, hasPassword := parsed.User.Password()
	if !hasPassword || password == "" {
		return rawURL
	}

	// 重建脱敏后的 URL
	// 由于 Go 的 url.URL.String() 会对 Host 中的 @ 等字符编码，
	// 这里采用与 Python 一致的方式：直接拼装各部分
	var buf strings.Builder
	if parsed.Scheme != "" {
		buf.WriteString(parsed.Scheme)
		buf.WriteString("://")
	}

	// userinfo 部分
	username := parsed.User.Username()
	if username != "" {
		buf.WriteString(username)
	}
	buf.WriteString(":***")
	buf.WriteByte('@')

	// 主机:端口
	buf.WriteString(parsed.Hostname())
	if parsed.Port() != "" {
		buf.WriteByte(':')
		buf.WriteString(parsed.Port())
	}

	// 路径
	buf.WriteString(parsed.Path)
	if parsed.RawQuery != "" {
		buf.WriteByte('?')
		buf.WriteString(parsed.RawQuery)
	}
	if parsed.Fragment != "" {
		buf.WriteByte('#')
		buf.WriteString(parsed.Fragment)
	}

	return buf.String()
}

// RedactURLInValue 递归脱敏值中的 URL 密码，用于安全日志输出。
//
// 对应 Python: _redact_url_in_value(value)
// 递归遍历 map[string]any、[]any 和 string 类型：
//   - string: 若匹配 URL 模式则脱敏密码
//   - map: 递归处理每个值
//   - slice: 递归处理每个元素
//   - 其他类型: 原样返回
func RedactURLInValue(value any) any {
	switch v := value.(type) {
	case string:
		if urlPattern.MatchString(v) {
			return RedactURLPassword(v)
		}
		return v
	case map[string]any:
		result := make(map[string]any, len(v))
		for k, val := range v {
			result[k] = RedactURLInValue(val)
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = RedactURLInValue(item)
		}
		return result
	default:
		return value
	}
}
