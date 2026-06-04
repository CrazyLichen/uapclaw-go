// utils 包提供通用工具函数。
//
// net.go 实现网络相关工具：本机 IP 获取、URL 密码脱敏、HTTP 头清洗。
// 对应 Python：
//   - openjiuwen/core/common/utils/ip_utils.py
//   - openjiuwen/core/common/utils/url_utils.py
//   - openjiuwen/core/common/utils/header_utils.py

package utils

import (
	"net"
	"net/url"
	"strings"
)

// GetLocalIP 获取本机可用 IPv4 地址（排除 127.0.0.1）。
//
// 对应 Python: get_local_ip()
// 通过向公共 DNS（8.8.8.8:80）发起 UDP 连接来检测出口 IP，
// 不实际发送数据，仅利用 socket 获取本地地址。
// 如果检测失败，回退到 "127.0.0.1"。
func GetLocalIP() string {
	conn, err := net.DialTimeout("udp4", "8.8.8.8:80", 0)
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()

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

	// host:port
	buf.WriteString(parsed.Hostname())
	if parsed.Port() != "" {
		buf.WriteByte(':')
		buf.WriteString(parsed.Port())
	}

	// path
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

// ProtectedHeaders 受保护的 HTTP 头名称（小写）。
//
// 这些头部由 HTTP 客户端/传输层自动管理，不应由用户手动设置。
// 对应 Python: PROTECTED_HEADERS
var ProtectedHeaders = map[string]bool{
	"host":             true,
	"content-length":   true,
	"transfer-encoding": true,
	"connection":       true,
	"authorization":    true,
}

// SanitizeHeaders 清洗 HTTP 头，移除受保护键和空值。
//
// 对应 Python: sanitize_headers()
// 处理规则：
//  1. 跳过空键或空值
//  2. 跳过受保护头部（见 ProtectedHeaders）
//  3. 值规范化为字符串
//  4. 跳过值为纯空白的条目
func SanitizeHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return map[string]string{}
	}

	sanitized := make(map[string]string, len(headers))
	for key, value := range headers {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}

		if ProtectedHeaders[strings.ToLower(key)] {
			continue
		}

		if strings.TrimSpace(value) == "" {
			continue
		}

		sanitized[key] = value
	}
	return sanitized
}
