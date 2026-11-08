package permissions

import "testing"

// TestNormalizeNetworkHost 测试 IPv4/IPv6/域名/IDNA/异常的归一化
func TestNormalizeNetworkHost(t *testing.T) {
	cases := []struct{ in, want string }{
		// 基本
		{"", ""},
		{"  ", ""},
		{"Example.COM", "example.com"},
		{"example.com.", "example.com"}, // 去尾部点
		{"[::1]", "::1"},                // 去方括号
		{"127.0.0.1", "127.0.0.1"},      // IP 原样
		{"0:0:0:0:0:0:0:1", "::1"},      // IPv6 压缩
		{"2001:0db8:0000:0000:0000:0000:0000:0001", "2001:db8::1"},
		{"EXAMPLE.com", "example.com"},
	}
	for _, c := range cases {
		if got := NormalizeNetworkHost(c.in); got != c.want {
			t.Errorf("NormalizeNetworkHost(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestNormalizeNetworkHost_IDNA 测试 Unicode 域名转为 punycode
func TestNormalizeNetworkHost_IDNA(t *testing.T) {
	got := NormalizeNetworkHost("例え.jp")
	if got == "例え.jp" || got == "" {
		t.Errorf("NormalizeNetworkHost(例え.jp) = %q，应转为 punycode", got)
	}
	// punycode 形式本身应保持不变
	if again := NormalizeNetworkHost(got); again != got {
		t.Errorf("punycode 形式不稳定：%q → %q", got, again)
	}
}

// TestNetworkHostRejectionReason 测试四类拒绝原因
func TestNetworkHostRejectionReason(t *testing.T) {
	cases := []struct{ host, want string }{
		{"", "network_host_missing"},
		{"   ", "network_host_missing"},
		{"localhost", "network_single_label_host"}, // 单标签
		{"com", "network_single_label_host"},       // 单标签（也是公共后缀）
		{"co.uk", "network_public_suffix_host"},    // 多标签但纯公共后缀
		{"github.io", "network_public_suffix_host"},
		{"127.0.0.1", "network_host_not_public"},       // 回环
		{"10.0.0.1", "network_host_not_public"},        // 私有
		{"169.254.169.254", "network_host_not_public"}, // link-local
		{"::1", "network_host_not_public"},             // IPv6 回环
		{"fc00::1", "network_host_not_public"},         // unique local
		{"8.8.8.8", ""},                                // 公网 IP 放行
		{"example.com", ""},                            // 正常域名放行
		{"sub.example.com", ""},                        // 子域放行
	}
	for _, c := range cases {
		if got := NetworkHostRejectionReason(c.host); got != c.want {
			t.Errorf("NetworkHostRejectionReason(%q) = %q, want %q", c.host, got, c.want)
		}
	}
}

// TestHostMatchesAllowedDomain 测试域名与子域匹配
func TestHostMatchesAllowedDomain(t *testing.T) {
	cases := []struct {
		host, allowed string
		want          bool
	}{
		{"example.com", "example.com", true},
		{"sub.example.com", "example.com", true},
		{"deep.sub.example.com", "example.com", true},
		{"notexample.com", "example.com", false},       // 必须有点分隔
		{"example.com.evil.com", "example.com", false}, // 后缀不同
		{"example.org", "example.com", false},
		// 任一侧不合规都返回 false
		{"127.0.0.1", "127.0.0.1", false}, // 私有 IP 被拒
		{"localhost", "localhost", false}, // 单标签被拒
		{"", "example.com", false},
		{"example.com", "", false},
		{"EXAMPLE.COM", "example.com", true}, // 大小写
	}
	for _, c := range cases {
		if got := HostMatchesAllowedDomain(c.host, c.allowed); got != c.want {
			t.Errorf("HostMatchesAllowedDomain(%q, %q) = %v, want %v", c.host, c.allowed, got, c.want)
		}
	}
}

// TestHasSecretQuery 测试查询串里的敏感键识别
func TestHasSecretQuery(t *testing.T) {
	secret := []string{
		"token=abc",
		"access_token=x",
		"api_key=x",
		"apikey=x",
		"password=x",
		"secret=x",
		"client_secret=x",
		"authorization=x",
		"a=1&token=x",
		// 驼峰切分
		"accessToken=x",
		"apiKey=x",
		"authToken=x",
		// 下划线 / 连字符 / 点分隔
		"auth_token=x",
		"auth-token=x",
		// key + 限定词
		"private_key=x",
		"secretKey=x",
		// 空值也识别（keep_blank_values）
		"token=",
	}
	for _, q := range secret {
		if !HasSecretQuery(q) {
			t.Errorf("HasSecretQuery(%q) = false, want true", q)
		}
	}

	notSecret := []string{
		"",
		"page=2",
		"q=hello",
		"id=123",
		"sort=desc&limit=10",
		"keyword=test",
	}
	for _, q := range notSecret {
		if HasSecretQuery(q) {
			t.Errorf("HasSecretQuery(%q) = true, want false", q)
		}
	}
}

// TestIsSecretQueryKey 测试键名判定的三类规则
func TestIsSecretQueryKey(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		// 直接命中 SECRET_QUERY_KEYS
		{"token", true},
		{"TOKEN", true},
		{"  token  ", true},
		// compact 命中（分词后拼接）
		{"accessToken", true},
		{"clientSecret", true},
		{"idToken", true},
		{"refreshToken", true},
		{"sessionId", true},
		// 单词命中 SECRET_QUERY_WORDS
		{"my_token_here", true},
		{"userPasswordReset", true},
		// key + 限定词
		{"apiKey", true},
		{"privateKey", true},
		{"secretKey", true},
		// 不命中
		{"", false},
		{"page", false},
		{"id", false},
		{"key", false}, // 单独的 key 不命中（在 SECRET_QUERY_KEYS 里？→ 是，命中）
	}
	for _, c := range cases {
		got := isSecretQueryKey(c.key)
		// "key" 在 SECRET_QUERY_KEYS 里，所以应命中
		if c.key == "key" {
			if !got {
				t.Error("isSecretQueryKey(\"key\") 应命中（在 SECRET_QUERY_KEYS 内）")
			}
			continue
		}
		if got != c.want {
			t.Errorf("isSecretQueryKey(%q) = %v, want %v", c.key, got, c.want)
		}
	}
}

// TestParseQueryPairs 测试查询串解析（保序、保空值、解码）
func TestParseQueryPairs(t *testing.T) {
	pairs := parseQueryPairs("a=1&b=&c=x%20y&d")
	if len(pairs) != 4 {
		t.Fatalf("pairs = %d 组, want 4: %v", len(pairs), pairs)
	}
	if pairs[0][0] != "a" || pairs[0][1] != "1" {
		t.Errorf("第 0 组 = %v, want [a 1]", pairs[0])
	}
	if pairs[1][0] != "b" || pairs[1][1] != "" {
		t.Errorf("第 1 组 = %v, want [b \"\"]（保空值）", pairs[1])
	}
	if pairs[2][1] != "x y" {
		t.Errorf("第 2 组值 = %q, want \"x y\"（解码）", pairs[2][1])
	}
	if pairs[3][0] != "d" {
		t.Errorf("第 3 组键 = %q, want d", pairs[3][0])
	}
	// 空查询串
	if got := parseQueryPairs(""); len(got) != 0 {
		t.Errorf("空查询串应返回 0 组，got %v", got)
	}
}

// TestIsGlobalIPv4_逐网段 测试 CPython is_global 的 IPv4 全部 15 个网段
//
// 这是安全关键函数：漏判任一网段都会让内部地址被当作公网放行。
func TestIsGlobalIPv4_逐网段(t *testing.T) {
	nonGlobal := []string{
		"0.0.0.0", // 0.0.0.0/8
		"0.255.255.255",
		"10.0.0.1", // 10/8
		"10.255.255.255",
		"100.64.0.1", // 100.64/10 共享
		"100.127.255.255",
		"127.0.0.1",   // 127/8
		"169.254.1.1", // 169.254/16
		"172.16.0.1",  // 172.16/12
		"172.31.255.255",
		"192.0.0.1",   // 192.0.0/29
		"192.0.2.1",   // 192.0.2/24
		"192.88.99.1", // 192.88.99/24
		"192.168.1.1", // 192.168/16
		"198.18.0.1",  // 198.18/15
		"198.19.255.255",
		"198.51.100.1", // 198.51.100/24
		"203.0.113.1",  // 203.0.113/24
		"224.0.0.1",    // 224/4 multicast
		"239.255.255.255",
		"240.0.0.1", // 240/4 reserved
		"255.255.255.255",
	}
	for _, s := range nonGlobal {
		if NetworkHostRejectionReason(s) != "network_host_not_public" {
			t.Errorf("%q 应被判为非公网", s)
		}
	}

	global := []string{
		"1.1.1.1", // 边界外
		"8.8.8.8",
		"100.63.255.255",  // 100.64/10 之前
		"100.128.0.0",     // 100.64/10 之后
		"172.15.255.255",  // 172.16/12 之前
		"172.32.0.0",      // 172.16/12 之后
		"192.0.0.8",       // 192.0.0/29 之后
		"198.17.255.255",  // 198.18/15 之前
		"198.20.0.0",      // 198.18/15 之后
		"223.255.255.255", // 224/4 之前
	}
	for _, s := range global {
		if got := NetworkHostRejectionReason(s); got != "" {
			t.Errorf("%q 应被判为公网，got %q", s, got)
		}
	}
}

// TestIsGlobalIPv6_逐网段 测试 CPython is_global 的 IPv6 全部 9 个网段
func TestIsGlobalIPv6_逐网段(t *testing.T) {
	nonGlobal := []string{
		"::",                // ::/128
		"::1",               // ::1/128
		"64:ff9b::1",        // 64:ff9b::/96 NAT64
		"100::1",            // 100::/64
		"2001::1",           // 2001::/23
		"2001:db8::1",       // 2001:db8::/32
		"fc00::1",           // fc00::/7
		"fd12:3456:789a::1", // fc00::/7 内
		"fe80::1",           // fe80::/10
		"ff02::1",           // ff00::/8
	}
	for _, s := range nonGlobal {
		if NetworkHostRejectionReason(s) != "network_host_not_public" {
			t.Errorf("%q 应被判为非公网", s)
		}
	}

	global := []string{
		"2001:4860:4860::8888", // Google DNS
		"2606:4700:4700::1111", // Cloudflare
		"2a00:1450:4001::1",
	}
	for _, s := range global {
		if got := NetworkHostRejectionReason(s); got != "" {
			t.Errorf("%q 应被判为公网，got %q", s, got)
		}
	}
}

// TestNormalizeNetworkHost_IPv6压缩 测试 IPv6 未压缩形式被规范化
func TestNormalizeNetworkHost_IPv6压缩(t *testing.T) {
	got := NormalizeNetworkHost("2001:0db8:0000:0000:0000:0000:0000:0001")
	if got != "2001:db8::1" {
		t.Errorf("got %q, want 2001:db8::1", got)
	}
}

// TestIsSecretQueryKey_限定词组合 测试 key + 限定词的组合判定
func TestIsSecretQueryKey_限定词组合(t *testing.T) {
	// key 单独出现时由 SECRET_QUERY_KEYS 命中
	if !isSecretQueryKey("key") {
		t.Error("key 应命中（在 SECRET_QUERY_KEYS 内）")
	}
	// key 与其他词组合：非限定词不命中
	if isSecretQueryKey("monkey") {
		t.Error("monkey 不应命中")
	}
	// 限定词 + key
	if !isSecretQueryKey("client_key") {
		t.Error("client_key 应命中")
	}
	if !isSecretQueryKey("authKey") {
		t.Error("authKey 应命中")
	}
}
