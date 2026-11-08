package permissions

import (
	"net/netip"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/idna"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// secretQueryKeys 直接匹配的敏感查询键（下划线/连字符形式）。
	//
	// Python: SECRET_QUERY_KEYS
	secretQueryKeys = map[string]struct{}{
		"access_token": {}, "api_key": {}, "apikey": {}, "auth": {},
		"authorization": {}, "client_secret": {}, "credential": {}, "key": {},
		"password": {}, "private_key": {}, "secret": {}, "session": {},
		"signature": {}, "token": {},
	}

	// secretQueryCompactKeys 分词后拼接匹配的敏感键（驼峰/连字符归一）。
	//
	// Python: SECRET_QUERY_COMPACT_KEYS
	secretQueryCompactKeys = map[string]struct{}{
		"accesstoken": {}, "apikey": {}, "authkey": {}, "authtoken": {},
		"authorization": {}, "bearer": {}, "bearertoken": {}, "clientsecret": {},
		"credential": {}, "credentials": {}, "idtoken": {}, "jwt": {},
		"oauth": {}, "oauth2": {}, "password": {}, "privatekey": {},
		"refreshtoken": {}, "secret": {}, "session": {}, "sessionid": {},
		"sessionkey": {}, "sessiontoken": {}, "signature": {}, "token": {},
	}

	// secretQueryWords 单个词命中即视为敏感的词表。
	//
	// Python: SECRET_QUERY_WORDS
	secretQueryWords = map[string]struct{}{
		"auth": {}, "authorization": {}, "bearer": {}, "credential": {},
		"credentials": {}, "jwt": {}, "passwd": {}, "password": {}, "pwd": {},
		"secret": {}, "session": {}, "token": {},
	}

	// secretQueryKeyQualifiers 与 key 组合构成敏感键的限定词。
	//
	// Python: SECRET_QUERY_KEY_QUALIFIERS
	secretQueryKeyQualifiers = map[string]struct{}{
		"access": {}, "api": {}, "auth": {}, "authorization": {},
		"client": {}, "private": {}, "secret": {}, "session": {},
	}

	// publicSuffixOnlyHosts 只有公共后缀、不构成可授权主机的名字。
	//
	// Python: PUBLIC_SUFFIX_ONLY_HOSTS
	publicSuffixOnlyHosts = map[string]struct{}{
		"app": {}, "co.uk": {}, "com": {}, "dev": {}, "github.io": {},
		"gov": {}, "io": {}, "net": {}, "org": {}, "test": {},
	}

	// camelBoundaryRe 在「小写/数字 后跟小写」处插入下划线（驼峰切分）。
	//
	// Python 用 re.sub(r"([a-z0-9])([A-Z])", r"\1_\2", key)。Go 的 regexp
	// 不支持反向引用，因此改用 ReplaceAllStringFunc 手动重建。
	camelBoundaryRe = regexp.MustCompile(`[a-z0-9][A-Z]`)

	// nonAlnumRe 按非字母数字切分。
	//
	// Python: re.split(r"[^A-Za-z0-9]+", text)
	nonAlnumRe = regexp.MustCompile(`[^A-Za-z0-9]+`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeNetworkHost 返回用于网络权限匹配的规范小写主机名。
//
// 三条路径（对齐 Python normalize_network_host）：
//  1. 裁剪空白、去方括号、去尾部点、转小写
//  2. 能解析为 IP → 用标准化的 IP 文本（IPv6 压缩、IPv4 点分）
//  3. 否则按 IDNA 转 ASCII（punycode）；转换失败返回空串
//
// Python: normalize_network_host(host)
func NormalizeNetworkHost(host string) string {
	raw := strings.TrimSpace(host)
	raw = strings.Trim(raw, "[]")
	raw = strings.TrimSuffix(raw, ".")
	raw = strings.ToLower(raw)
	if raw == "" {
		return ""
	}
	if addr, err := netip.ParseAddr(raw); err == nil {
		return addr.String()
	}
	ascii, err := idna.ToASCII(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(ascii)
}

// NetworkHostRejectionReason 返回不安全授权主机的拒绝原因；可授权时返回空串。
//
// 三类拒绝（对齐 Python network_host_rejection_reason）：
//   - network_host_missing：归一化后为空
//   - network_single_label_host：不含点的单标签（如 localhost）
//   - network_public_suffix_host：纯公共后缀（如 com / co.uk / github.io）
//   - network_host_not_public：IP 但非全局可路由
//
// Python: network_host_rejection_reason(host)
func NetworkHostRejectionReason(host string) string {
	normalized := NormalizeNetworkHost(host)
	if normalized == "" {
		return "network_host_missing"
	}
	addr, err := netip.ParseAddr(normalized)
	if err != nil {
		// 非 IP：按域名规则判定
		labels := strings.Split(normalized, ".")
		if len(labels) < 2 {
			return "network_single_label_host"
		}
		if _, isSuffix := publicSuffixOnlyHosts[normalized]; isSuffix {
			return "network_public_suffix_host"
		}
		return ""
	}
	if !isGlobalAddr(addr) {
		return "network_host_not_public"
	}
	return ""
}

// HostMatchesAllowedDomain 判定 host 是否为 allowedDomain 本身或其子域。
//
// 两侧任一不合规（被拒为主机）都返回 false——避免把 localhost 之类
// 危险名字当作可授权域。
//
// Python: host_matches_allowed_domain(host, allowed_domain)
func HostMatchesAllowedDomain(host, allowedDomain string) bool {
	normalizedHost := NormalizeNetworkHost(host)
	normalizedAllowed := NormalizeNetworkHost(allowedDomain)
	if normalizedHost == "" || normalizedAllowed == "" {
		return false
	}
	if NetworkHostRejectionReason(normalizedHost) != "" {
		return false
	}
	if NetworkHostRejectionReason(normalizedAllowed) != "" {
		return false
	}
	if normalizedHost == normalizedAllowed {
		return true
	}
	return strings.HasSuffix(normalizedHost, "."+normalizedAllowed)
}

// HasSecretQuery 判定 URL 查询串是否含敏感键名。
//
// Python: has_secret_query(query)
func HasSecretQuery(query string) bool {
	for _, pair := range parseQueryPairs(query) {
		if isSecretQueryKey(pair[0]) {
			return true
		}
	}
	return false
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isSecretQueryKey 判定单个查询键名是否敏感。
//
// 四条判定（顺序对齐 Python _is_secret_query_key）：
//  1. 小写后直接命中 secretQueryKeys
//  2. 驼峰插入下划线、按非字母数字切词、拼接后命中 secretQueryCompactKeys
//  3. 任一词命中 secretQueryWords
//  4. 含 key 且任一词命中 secretQueryKeyQualifiers
//
// Python: _is_secret_query_key(key)
func isSecretQueryKey(key string) bool {
	rawKey := strings.TrimSpace(key)
	if rawKey == "" {
		return false
	}
	normalized := strings.ToLower(rawKey)
	if _, hit := secretQueryKeys[normalized]; hit {
		return true
	}

	// Python: re.sub(r"([a-z0-9])([A-Z])", r"\1_\2", raw_key)
	// RE2 无反向引用，用 ReplaceAllStringFunc 手动在边界插下划线。
	withBoundaries := camelBoundaryRe.ReplaceAllStringFunc(rawKey, func(m string) string {
		return string(m[0]) + "_" + string(m[1])
	})
	words := splitNonAlnumLower(withBoundaries)

	compact := strings.Join(words, "")
	if _, hit := secretQueryCompactKeys[compact]; hit {
		return true
	}
	for _, w := range words {
		if _, hit := secretQueryWords[w]; hit {
			return true
		}
	}
	hasKey := false
	for _, w := range words {
		if w == "key" {
			hasKey = true
			break
		}
	}
	if !hasKey {
		return false
	}
	for _, w := range words {
		if _, hit := secretQueryKeyQualifiers[w]; hit {
			return true
		}
	}
	return false
}

// splitNonAlnumLower 按非字母数字切分并转小写，丢弃空段。
func splitNonAlnumLower(s string) []string {
	parts := nonAlnumRe.Split(s, -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		out = append(out, strings.ToLower(p))
	}
	return out
}

// parseQueryPairs 解析查询串为保序的 (key, value) 对。
//
// 与 url.ParseQuery 的差别：
//   - 保持原始顺序（ParseQuery 返回 map，顺序丢失）
//   - keep_blank_values：`b=` 与裸 `d` 都保留为空值
//   - 单项解码失败只影响该项，保留原始文本（Python parse_qsl 同样吞掉错误）
//
// Python: urllib.parse.parse_qsl(query, keep_blank_values=True)
func parseQueryPairs(query string) [][2]string {
	out := make([][2]string, 0, 8)
	if query == "" {
		return out
	}
	for _, segment := range strings.Split(query, "&") {
		if segment == "" {
			continue
		}
		key, value, hasEq := strings.Cut(segment, "=")
		decodedKey := key
		if v, err := url.QueryUnescape(key); err == nil {
			decodedKey = v
		}
		decodedValue := ""
		if hasEq {
			decodedValue = value
			if v, err := url.QueryUnescape(value); err == nil {
				decodedValue = v
			}
		}
		out = append(out, [2]string{decodedKey, decodedValue})
	}
	return out
}

// isGlobalAddr 判定 IP 是否为全局可路由地址（对齐 Python ipaddress.is_global）。
//
// CPython 的 IPv4 is_global 判据：不在下列网段内
//
//	0.0.0.0/8, 10/8, 100.64/10(共享), 127/8, 169.254/16, 172.16/12,
//	192.0.0/29, 192.0.2/24, 192.88.99/24, 192.168/16, 198.18/15,
//	198.51.100/24, 203.0.113/24, 224/4(multicast), 240/4(reserved)
//
// IPv6：不在 ::/128, ::1/128, ::ffff:0:0/96, 64:ff9b::/96, 100::/64,
// 2001::/23, 2001:db8::/32, fc00::/7, fe80::/10, ff00::/8 内。
func isGlobalAddr(addr netip.Addr) bool {
	if !addr.IsValid() {
		return false
	}
	if addr.Is4In6() {
		return isGlobalIPv4(addr.Unmap())
	}
	if addr.Is4() {
		return isGlobalIPv4(addr)
	}
	return isGlobalIPv6(addr)
}

// isGlobalIPv4 判定 IPv4 是否全局可路由。逐网段对照 CPython ipaddress.py。
func isGlobalIPv4(addr netip.Addr) bool {
	b := addr.As4()
	switch {
	case b[0] == 0: // 0.0.0.0/8 "本网络"
		return false
	case b[0] == 10: // 10/8
		return false
	case b[0] == 100 && b[1] >= 64 && b[1] <= 127: // 100.64/10 共享地址
		return false
	case b[0] == 127: // 127/8 回环
		return false
	case b[0] == 169 && b[1] == 254: // 169.254/16 link-local
		return false
	case b[0] == 172 && b[1] >= 16 && b[1] <= 31: // 172.16/12
		return false
	case b[0] == 192 && b[1] == 0 && b[2] == 0 && b[3] <= 7: // 192.0.0/29
		return false
	case b[0] == 192 && b[1] == 0 && b[2] == 2: // 192.0.2/24 TEST-NET-1
		return false
	case b[0] == 192 && b[1] == 88 && b[2] == 99: // 192.88.99/24 6to4 relay
		return false
	case b[0] == 192 && b[1] == 168: // 192.168/16
		return false
	case b[0] == 198 && (b[1] == 18 || b[1] == 19): // 198.18/15 benchmark
		return false
	case b[0] == 198 && b[1] == 51 && b[2] == 100: // 198.51.100/24 TEST-NET-2
		return false
	case b[0] == 203 && b[1] == 0 && b[2] == 113: // 203.0.113/24 TEST-NET-3
		return false
	case b[0] >= 224 && b[0] <= 239: // 224/4 multicast
		return false
	case b[0] >= 240: // 240/4 reserved（含 255.255.255.255）
		return false
	}
	return true
}

// isGlobalIPv6 判定 IPv6 是否全局可路由。
func isGlobalIPv6(addr netip.Addr) bool {
	b := addr.As16()

	// ::/128 unspecified
	if allZeroBytes(b[:]) {
		return false
	}
	// ::1/128 loopback
	isLoopback := b[15] == 1
	for i := 0; i < 15; i++ {
		if b[i] != 0 {
			isLoopback = false
			break
		}
	}
	if isLoopback {
		return false
	}
	// ::ffff:0:0/96 IPv4-mapped（调用方已 Unmap，此处兜底）
	if allZeroBytes(b[:10]) && b[10] == 0xff && b[11] == 0xff {
		return false
	}
	// 64:ff9b::/96 NAT64
	if b[0] == 0x00 && b[1] == 0x64 && b[2] == 0xff && b[3] == 0x9b &&
		allZeroBytes(b[4:12]) {
		return false
	}
	// 100::/64 discard-only
	if b[0] == 0x01 && b[1] == 0x00 && allZeroBytes(b[2:8]) {
		return false
	}
	// 2001::/23 协议分配（含 Teredo 2001::/32、ORCHID 2001:10::/28）
	if b[0] == 0x20 && b[1] == 0x01 && b[2] < 0x02 {
		return false
	}
	// 2001:db8::/32 文档
	if b[0] == 0x20 && b[1] == 0x01 && b[2] == 0x0d && b[3] == 0xb8 {
		return false
	}
	// fc00::/7 unique local
	if b[0]&0xfe == 0xfc {
		return false
	}
	// fe80::/10 link-local
	if b[0] == 0xfe && b[1]&0xc0 == 0x80 {
		return false
	}
	// ff00::/8 multicast
	if b[0] == 0xff {
		return false
	}
	return true
}

// allZeroBytes 判定字节切片是否全为 0。
func allZeroBytes(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}
