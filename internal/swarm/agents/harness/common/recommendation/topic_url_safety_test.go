package recommendation

import "testing"

// 对齐 Python topic_url_safety.py 全部函数的等价性用例（TDD 先行）。
func TestNormalizeFetchHost_各类输入(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Example.COM.", "example.com"},    // 对齐 :36 rstrip(".").lower()
		{"[::1]", "::1"},                   // 剥离方括号
		{"192.168.001.1", "192.168.001.1"}, // 前导零非法地址 → 走 idna 路径原样返回（对齐 Python ipaddress 3.9.5+ 拒绝前导零）
		{"", ""},
		{"  ", ""},
		{"例え.jp", "xn--r8jz45g.jp"}, // idna 编码
	}
	for _, c := range cases {
		if got := NormalizeFetchHost(c.in); got != c.want {
			t.Fatalf("NormalizeFetchHost(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestPublicFetchURLRejectionReasonSync_拒绝矩阵(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"http://192.168.1.1/x", "network_host_not_public"},      // 私网字面量
		{"http://127.0.0.1/x", "network_host_not_public"},        // 回环
		{"http://169.254.169.254/meta", "network_metadata_host"}, // 云 metadata（链路本地）
		{"http://metadata/v1", "network_metadata_host"},          // metadata 主机名
		{"http://metadata.google.internal/v1", "network_metadata_host"},
		{"http://box.local/x", "network_internal_hostname"}, // 内部后缀
		{"http://box.internal/x", "network_internal_hostname"},
		{"http://box.lan/x", "network_internal_hostname"},
		{"http://box.home.arpa/x", "network_internal_hostname"},
		{"http://myhost/x", "network_single_label_host"}, // 无点单标签
		{"ftp://example.com/x", "network_scheme_not_http"},
		{"http://example.com:notaport/", "network_url_invalid_port"},
		{"", "network_scheme_not_http"},
	}
	for _, c := range cases {
		if got := PublicFetchURLRejectionReasonSync(c.url); got != c.want {
			t.Fatalf("Sync(%q)=%q want %q", c.url, got, c.want)
		}
	}
	// 合法公网：域名不解析时放行（对齐 :101-102 注释语义），返回 ""
	if got := PublicFetchURLRejectionReasonSync("https://uapclaw-nonexistent-host-invalid.invalid/x"); got != "" {
		t.Fatalf("不可解析域名应放行，got %q", got)
	}
}

func TestPublicFetchURLRejectionReason_异步版等价(t *testing.T) {
	ctx := t.Context()
	if got := PublicFetchURLRejectionReason(ctx, "http://10.0.0.1/"); got != "network_host_not_public" {
		t.Fatalf("异步版私网拒绝失败: %q", got)
	}
	if got := PublicFetchURLRejectionReason(ctx, "https://example.com/"); got != "" {
		t.Fatalf("异步版公网应放行: %q", got)
	}
}
