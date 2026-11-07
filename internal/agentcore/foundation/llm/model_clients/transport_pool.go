package model_clients

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// 池参数对齐上游 httpx.Limits（openai_model_client.py:1460-1464）与 keepalive_expiry=60.0（L1457）。
const (
	// sharedMaxIdleConns 对齐 max_connections=100
	sharedMaxIdleConns = 100
	// sharedMaxIdlePerHost 对齐 max_keepalive_connections=20
	sharedMaxIdlePerHost = 20
	// sharedIdleConnTimeout 对齐 keepalive_expiry=60.0；同时防复用服务端已关的 idle 连接
	sharedIdleConnTimeout = 60 * time.Second
	// sharedResponseHeaderTimeout 兜「连上但永不给响应头」；上游由 read timeout 覆盖（偏差 D-2）
	sharedResponseHeaderTimeout = 360 * time.Second
	// sharedDialTimeout 拨号上限
	sharedDialTimeout = 30 * time.Second
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	transportPool   = map[transportPoolKey]*http.Transport{}
	transportPoolMu sync.Mutex
	// sharedDialer 共享拨号器（超时挂 DialContext）
	sharedDialer = net.Dialer{Timeout: sharedDialTimeout}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetSharedTransport 获取按 (verify_ssl, ssl_cert) 分桶的共享 Transport。
// Go 的 Transport host 无关（URL/鉴权头 per-request），按安全形态分桶即可最大化复用；
// 上游按 connection_key（含 api_base）分桶——见设计文档偏差 D-1。
// 证书读取失败时退化为系统信任库形态并照常入池：前置校验（ValidateConfig）
// 已在配置层拦截不可读证书，此处不重复报错。
func GetSharedTransport(verifySSL bool, sslCert string) *http.Transport {
	key := transportPoolKey{verifySSL: verifySSL, sslCert: sslCert}
	transportPoolMu.Lock()
	defer transportPoolMu.Unlock()
	if tr, ok := transportPool[key]; ok {
		return tr
	}
	tr := &http.Transport{
		MaxIdleConns:          sharedMaxIdleConns,
		MaxIdleConnsPerHost:   sharedMaxIdlePerHost,
		IdleConnTimeout:       sharedIdleConnTimeout,
		ResponseHeaderTimeout: sharedResponseHeaderTimeout,
		DialContext:           sharedDialer.DialContext,
		Proxy:                 http.ProxyFromEnvironment,
	}
	// SSL 配置
	if !verifySSL {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	} else if sslCert != "" {
		caCert, err := os.ReadFile(sslCert)
		if err == nil {
			caCertPool := x509.NewCertPool()
			if caCertPool.AppendCertsFromPEM(caCert) {
				// Python: ssl_utils.create_strict_ssl_context —— 系统信任库之上叠加用户 CA（additive）
				tr.TLSClientConfig = &tls.Config{RootCAs: caCertPool}
			} else {
				tr.TLSClientConfig = &tls.Config{}
			}
		} else {
			tr.TLSClientConfig = &tls.Config{}
		}
	} else {
		// verify_ssl=true 且无自定义证书：系统默认信任库（对齐 Python create_strict_ssl_context 的
		// "公共 CA 开箱即用" 语义；显式空 Config 表示走系统池）
		tr.TLSClientConfig = &tls.Config{}
	}
	transportPool[key] = tr
	return tr
}

// CloseIdleTransports 关闭全部池化 Transport 的 idle 连接。
// Python: openai_model_client.py:1500 aclose（teardown 专用，禁止热路径调用）
func CloseIdleTransports() {
	transportPoolMu.Lock()
	snapshot := make([]*http.Transport, 0, len(transportPool))
	for _, tr := range transportPool {
		snapshot = append(snapshot, tr)
	}
	transportPoolMu.Unlock()
	for _, tr := range snapshot {
		tr.CloseIdleConnections()
	}
}

// ResetSharedTransports 清空池（测试用；生产 teardown 走 CloseIdleTransports）。
func ResetSharedTransports() {
	transportPoolMu.Lock()
	transportPool = map[transportPoolKey]*http.Transport{}
	transportPoolMu.Unlock()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// transportPoolKey Transport 池分桶键（安全形态）。
type transportPoolKey struct {
	verifySSL bool
	sslCert   string
}
