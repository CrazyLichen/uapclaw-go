package model_clients

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

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

// GetSharedTransport 获取按五元组分桶的共享 Transport。
// Python: anthropic_model_client.py:655-673 connection_key
// (auth_mode, api_key, normalized_base_url, verify_ssl, ssl_cert)——api_key 入桶
// 保证租户隔离（同一 base 上不同 key 不共用连接池）；normalized_base_url 决定代理。
// spec D-1 偏差回收：原 (verify_ssl, ssl_cert) 二元桶升级为五元桶。
// 证书读取失败时退化为系统信任库形态并照常入池：前置校验（ValidateConfig）
// 已在配置层拦截不可读证书，此处不重复报错。
func GetSharedTransport(authMode llmschema.AuthMode, apiKey, baseURL string, verifySSL bool, sslCert string) *http.Transport {
	key := transportPoolKey{
		authMode:  authMode,
		apiKey:    apiKey,
		baseURL:   baseURL,
		verifySSL: verifySSL,
		sslCert:   sslCert,
	}
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

// transportPoolKey Transport 池分桶键（连接身份五元组）。
// api_key 明文入 key（用户裁决，与 Python connection_key 同构）；
// 由 String/GoString 脱敏堵住 %v/%+v/%#v 泄漏面。
type transportPoolKey struct {
	authMode  llmschema.AuthMode
	apiKey    string
	baseURL   string
	verifySSL bool
	sslCert   string
}

// String 脱敏输出：api_key 只显示长度与前 4 位。
func (k transportPoolKey) String() string {
	return fmt.Sprintf("transport{auth=%s,key=len:%d/%s...,base=%s,verify=%v,cert=%s}",
		k.authMode, len(k.apiKey), maskAPIKey(k.apiKey), k.baseURL, k.verifySSL, k.sslCert)
}

// GoString 同 String，堵住 %#v。
func (k transportPoolKey) GoString() string { return k.String() }

// maskAPIKey 返回前 4 位（不足 4 位全遮）。
func maskAPIKey(k string) string {
	if len(k) <= 4 {
		return "****"
	}
	return k[:4]
}
