package model_clients

import "testing"

// TestGetSharedTransport_按SSL形态分桶 同 (verify_ssl, ssl_cert) 复用同一实例；不同形态分桶。
//
// 对齐设计文档批次 C-A（上游 _client_cache 分桶语义的 Go 等价物，偏差 D-1）。
func TestGetSharedTransport_按SSL形态分桶(t *testing.T) {
	ResetSharedTransports()
	t1 := GetSharedTransport(false, "")
	t2 := GetSharedTransport(false, "")
	if t1 != t2 {
		t.Error("同形态应复用同一 Transport 指针")
	}
	t3 := GetSharedTransport(true, "")
	if t3 == t2 {
		t.Error("verify_ssl 不同应分桶")
	}
	ResetSharedTransports()
	t4 := GetSharedTransport(false, "")
	if t4 == t2 {
		t.Error("Reset 后应重建")
	}
}

// TestGetSharedTransport_池参数 参数照抄上游 httpx Limits。
//
// Python: openai_model_client.py:1460-1464 httpx.Limits(max_connections=100,
// max_keepalive_connections=20, keepalive_expiry=60.0)
func TestGetSharedTransport_池参数(t *testing.T) {
	ResetSharedTransports()
	tr := GetSharedTransport(false, "")
	if tr.MaxIdleConns != 100 {
		t.Errorf("MaxIdleConns = %d want 100", tr.MaxIdleConns)
	}
	if tr.MaxIdleConnsPerHost != 20 {
		t.Errorf("MaxIdleConnsPerHost = %d want 20", tr.MaxIdleConnsPerHost)
	}
	if tr.IdleConnTimeout.Seconds() != 60 {
		t.Errorf("IdleConnTimeout = %v want 60s", tr.IdleConnTimeout)
	}
	if tr.ResponseHeaderTimeout.Seconds() != 360 {
		t.Errorf("ResponseHeaderTimeout = %v want 360s", tr.ResponseHeaderTimeout)
	}
	if tr.TLSClientConfig == nil || !tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("verify_ssl=false 应设 InsecureSkipVerify")
	}
	strict := GetSharedTransport(true, "")
	if strict.TLSClientConfig == nil || strict.TLSClientConfig.InsecureSkipVerify {
		t.Error("verify_ssl=true 不应 InsecureSkipVerify")
	}
}

// TestGetSharedTransport_证书形态分桶 ssl_cert 不同时分桶，证书可加载时设 RootCAs。
func TestGetSharedTransport_证书形态分桶(t *testing.T) {
	ResetSharedTransports()
	t1 := GetSharedTransport(true, "/nonexistent/cert.pem")
	t2 := GetSharedTransport(true, "")
	if t1 == t2 {
		t.Error("ssl_cert 不同应分桶")
	}
	// 证书路径不存在：退化为系统信任库（不 panic、仍可用）
	if t1.TLSClientConfig == nil {
		t.Error("退化形态 TLSClientConfig 不应为 nil")
	}
}

// TestCloseIdleTransports 关闭入口不 panic 且池条目保留（连接关闭、桶可继续取用）。
//
// Python: openai_model_client.py:1500 aclose（teardown 专用）
func TestCloseIdleTransports(t *testing.T) {
	ResetSharedTransports()
	tr := GetSharedTransport(false, "")
	CloseIdleTransports() // 不应 panic
	after := GetSharedTransport(false, "")
	if after != tr {
		t.Error("Close 后桶条目保留（连接关闭，Transport 可继续取用）")
	}
}
