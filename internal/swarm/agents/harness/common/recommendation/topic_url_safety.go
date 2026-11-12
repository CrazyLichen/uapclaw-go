package recommendation

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strings"

	"golang.org/x/net/idna"
)

// ──────────────────────────── 常量 ────────────────────────────

// FETCH_URL_SCHEMES 允许抓取的协议（对齐 topic_url_safety.py:18）
var FETCH_URL_SCHEMES = map[string]bool{"http": true, "https": true}

// INTERNAL_HOST_SUFFIXES 内部主机名后缀（对齐 topic_url_safety.py:19-24）
var INTERNAL_HOST_SUFFIXES = []string{".internal", ".local", ".lan", ".home.arpa"}

// METADATA_HOSTS 云 metadata 主机名（对齐 topic_url_safety.py:25-30）
var METADATA_HOSTS = map[string]bool{"metadata": true, "metadata.google.internal": true}

// ──────────────────────────── 枚举 ────────────────────────────

// 端口解析错误（对齐 Python parsed.port 的 ValueError 语义）
var (
	errEmptyPort   = errors.New("empty port")
	errInvalidPort = errors.New("invalid port")
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeFetchHost 返回规范化小写主机名；读不出来返回空串。
// 对齐 Python topic_url_safety.py:33-46。
func NormalizeFetchHost(host string) string {
	raw := strings.ToLower(strings.TrimSuffix(strings.Trim(strings.TrimSpace(host), "[]"), "."))
	if raw == "" {
		return ""
	}
	// 对齐 :39-42 ipaddress.ip_address 归一化
	if addr, err := netip.ParseAddr(raw); err == nil {
		return addr.String()
	}
	// 对齐 :43-46 idna 编码
	return idnaToASCII(raw)
}

// PublicFetchURLRejectionReason 返回该 URL 不可抓取的原因，公网目标返回空串。
// 对齐 Python topic_url_safety.py:96-113（异步版）。
// 已知偏差：Python 用 asyncio loop.getaddrinfo 无 ctx 参数，Go 解析器需要 ctx（平台差异）。
func PublicFetchURLRejectionReason(ctx context.Context, urlText string) string {
	reason, host, _ := endpointOrReason(urlText)
	if reason != "" {
		return reason
	}
	// 对齐 :107-112 解析失败放行：请求本身会失败，不会触达内部目标
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return ""
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		ips = append(ips, a.IP)
	}
	return resolvedRejectionReason(ips)
}

// PublicFetchURLRejectionReasonSync 同步版，供逐跳跟随重定向的工作线程调用。
// 对齐 Python topic_url_safety.py:116-126。
func PublicFetchURLRejectionReasonSync(urlText string) string {
	reason, host, _ := endpointOrReason(urlText)
	if reason != "" {
		return reason
	}
	addrs, err := net.LookupIP(host)
	if err != nil {
		return ""
	}
	return resolvedRejectionReason(addrs)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// hostRejectionReason 主机名静态拒绝判定。对齐 Python topic_url_safety.py:49-65。
func hostRejectionReason(host string) string {
	if host == "" {
		return "network_host_missing"
	}
	if METADATA_HOSTS[host] {
		return "network_metadata_host"
	}
	// 对齐 :54-55 endswith(INTERNAL_HOST_SUFFIXES)
	for _, suffix := range INTERNAL_HOST_SUFFIXES {
		if strings.HasSuffix(host, suffix) {
			return "network_internal_hostname"
		}
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		// 对齐 :59-61 非地址主机名：无点单标签拒绝，其余放行待解析复核
		if !strings.Contains(host, ".") {
			return "network_single_label_host"
		}
		return ""
	}
	if addr.IsLinkLocalUnicast() {
		// 云 metadata 端点位于链路本地段，绝不会在主机之外（对齐 :62-64 注释）
		return "network_metadata_host"
	}
	if !isGlobalAddr(addr) {
		return "network_host_not_public"
	}
	return ""
}

// endpointOrReason 协议/主机检查与待解析主机，或拒绝原因。
// 对齐 Python topic_url_safety.py:68-82。
func endpointOrReason(urlText string) (string, string, int) {
	parsed, err := url.Parse(strings.TrimSpace(urlText))
	if err != nil {
		// Go url.Parse 对非法端口（如 :notaport）直接报错，而 Python urlparse
		// 延迟到 .port 访问才抛 ValueError → network_url_invalid_port。
		// 对齐该语义：解析失败时先检查是否为端口问题。
		if isInvalidPortFailure(urlText) {
			return "network_url_invalid_port", "", 0
		}
		return "network_scheme_not_http", "", 0
	}
	if !FETCH_URL_SCHEMES[strings.ToLower(parsed.Scheme)] {
		return "network_scheme_not_http", "", 0
	}
	host := NormalizeFetchHost(parsed.Hostname())
	if reason := hostRejectionReason(host); reason != "" {
		return reason, host, 0
	}
	// 对齐 :78-81 端口解析失败拒绝
	port := 0
	if parsed.Port() != "" {
		if p, err := parsePort(parsed.Port()); err != nil {
			return "network_url_invalid_port", host, 0
		} else {
			port = p
		}
	} else if strings.ToLower(parsed.Scheme) == "https" {
		port = 443
	} else {
		port = 80
	}
	return "", host, port
}

// resolvedRejectionReason 解析结果必须全部全球可达。对齐 Python topic_url_safety.py:85-93。
func resolvedRejectionReason(addrs []net.IP) string {
	for _, ip := range addrs {
		addr, ok := netip.AddrFromSlice(ip)
		if !ok || !isGlobalAddr(addr.Unmap()) {
			return "network_host_not_public"
		}
	}
	return ""
}

// isGlobalAddr 对齐 Python ipaddress.is_global：私网/回环/链路本地/组播/未指定/
// 保留段（测试网、基准、共享段）均非全球可达。
func isGlobalAddr(addr netip.Addr) bool {
	if addr.Is4In6() {
		addr = addr.Unmap()
	}
	if !addr.IsValid() || addr.IsUnspecified() || addr.IsLoopback() ||
		addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() {
		return false
	}
	if addr.Is6() {
		// fc00::/7 ULA 私有（netip.IsPrivate 已含）；否则 IPv6 全球单播视为可达
		return !addr.IsPrivate()
	}
	v4 := addr.As4()
	switch {
	case v4[0] == 10,
		v4[0] == 172 && v4[1] >= 16 && v4[1] < 32,
		v4[0] == 192 && v4[1] == 168,
		v4[0] == 100 && v4[1] == 64, // 100.64/10 共享段（Python is_private）
		v4[0] == 0,
		v4[0] == 192 && v4[1] == 0 && v4[2] == 0,       // 192.0.0.0/24
		v4[0] == 192 && v4[1] == 0 && v4[2] == 2,       // 192.0.2.0/24 测试网
		v4[0] == 198 && v4[1] == 51 && v4[2] == 100,    // 198.51.100.0/24 测试网
		v4[0] == 203 && v4[1] == 0 && v4[2] == 113,     // 203.0.113.0/24 测试网
		v4[0] == 198 && (v4[1] == 18 || v4[1] == 19),   // 198.18/15 基准
		v4[0] >= 224:                                   // 组播 + 保留段 240/4
		return false
	}
	return true
}

// idnaToASCII IDN 主机名转 ASCII（对齐 Python raw.encode("idna")）。
func idnaToASCII(host string) string {
	// 只含 ASCII 时直接返回（含 punycode 形式）
	needsEncode := false
	for _, r := range host {
		if r > 127 {
			needsEncode = true
			break
		}
	}
	if !needsEncode {
		return strings.ToLower(host)
	}
	labels := strings.Split(host, ".")
	for i, label := range labels {
		if !strings.ContainsFunc(label, func(r rune) bool { return r > 127 }) {
			continue
		}
		// Go 标准库无公开 IDNA 编码器；golang.org/x/net/idna 可用时用它，
		// 否则拒绝（对齐 Python UnicodeError → 返回空串）
		encoded, err := idnaEncodeLabel(label)
		if err != nil {
			return ""
		}
		labels[i] = encoded
	}
	return strings.ToLower(strings.Join(labels, "."))
}

// isInvalidPortFailure 对齐 Python urlparse 在访问 .port 时才校验端口的行为：
// URL 形如 scheme://host:notaport/ 时判定为端口非法。
func isInvalidPortFailure(urlText string) bool {
	idx := strings.Index(urlText, "://")
	if idx < 0 {
		return false
	}
	authority := urlText[idx+3:]
	if slash := strings.IndexAny(authority, "/?#"); slash >= 0 {
		authority = authority[:slash]
	}
	if at := strings.LastIndex(authority, "@"); at >= 0 {
		authority = authority[at+1:]
	}
	colon := strings.LastIndex(authority, ":")
	if colon < 0 {
		return false
	}
	_, err := parsePort(authority[colon+1:])
	return err != nil
}

// parsePort 解析 URL 端口字符串（对齐 Python parsed.port 的 ValueError 语义）。
func parsePort(text string) (int, error) {
	if text == "" {
		return 0, errEmptyPort
	}
	n := 0
	for _, r := range text {
		if r < '0' || r > '9' {
			return 0, errInvalidPort
		}
		n = n*10 + int(r-'0')
		if n > 65535 {
			return 0, errInvalidPort
		}
	}
	if n == 0 {
		return 0, errInvalidPort
	}
	return n, nil
}

// idnaEncodeLabel 单个 IDN 标签 punycode 编码（对齐 Python raw.encode("idna")）。
func idnaEncodeLabel(label string) (string, error) {
	return idna.Lookup.ToASCII(label)
}
