package security

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestGetSSLConfig_非HTTPS(t *testing.T) {
	verify, certPath, err := GetSSLConfig("SSL_VERIFY", "SSL_CERT", []string{"false"}, false)
	if err != nil {
		t.Fatalf("非HTTPS 不应报错: %v", err)
	}
	if verify {
		t.Error("非 HTTPS 期望 verify=false")
	}
	if certPath != "" {
		t.Error("非 HTTPS 期望 certPath 为空")
	}
}

func TestGetSSLConfig_验证关闭(t *testing.T) {
	os.Setenv("TEST_SSL_VERIFY_OFF", "false")
	defer os.Unsetenv("TEST_SSL_VERIFY_OFF")

	verify, certPath, err := GetSSLConfig("TEST_SSL_VERIFY_OFF", "SSL_CERT", []string{"false"}, true)
	if err != nil {
		t.Fatalf("verify off 不应报错: %v", err)
	}
	if verify {
		t.Error("verify off 期望 verify=false")
	}
	if certPath != "" {
		t.Error("verify off 期望 certPath 为空")
	}
}

func TestGetSSLConfig_验证开启无证书(t *testing.T) {
	os.Unsetenv("TEST_SSL_CERT_MISSING")
	verify, _, err := GetSSLConfig("TEST_SSL_VERIFY_MISSING", "TEST_SSL_CERT_MISSING", []string{"false"}, true)
	if err == nil {
		t.Fatal("verify=true 但无证书应报错")
	}
	if verify {
		t.Error("出错时期望 verify=false")
	}
}

func TestGetSSLConfig_验证开启有证书(t *testing.T) {
	// 创建临时证书文件
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "ca.pem")
	if err := os.WriteFile(certFile, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	os.Unsetenv("TEST_SSL_VERIFY_EMPTY")
	os.Setenv("TEST_SSL_CERT_VALID", certFile)
	defer os.Unsetenv("TEST_SSL_CERT_VALID")

	verify, gotCertPath, err := GetSSLConfig("TEST_SSL_VERIFY_EMPTY", "TEST_SSL_CERT_VALID", []string{"false"}, true)
	if err != nil {
		t.Fatalf("有证书不应报错: %v", err)
	}
	if !verify {
		t.Error("有证书期望 verify=true")
	}
	if gotCertPath != certFile {
		t.Errorf("certPath = %q, want %q", gotCertPath, certFile)
	}
}

func TestCreateStrictTLSConfig_无证书(t *testing.T) {
	cfg, err := CreateStrictTLSConfig("")
	if err != nil {
		t.Fatalf("无证书不应报错: %v", err)
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Error("期望最低 TLS 1.2")
	}
	if len(cfg.CipherSuites) != 4 {
		t.Errorf("期望 4 个密码套件，实际 %d", len(cfg.CipherSuites))
	}
}

func TestCreateStrictTLSConfig_无效证书路径(t *testing.T) {
	os.Setenv("SAFE_CERT_DIR", "/nonexistent")
	defer os.Unsetenv("SAFE_CERT_DIR")

	_, err := CreateStrictTLSConfig("/nonexistent/cert.pem")
	if err == nil {
		t.Fatal("无效证书路径应报错")
	}
}

func TestBoolEnv(t *testing.T) {
	os.Setenv("TEST_BOOL_TRUE", "false")
	defer os.Unsetenv("TEST_BOOL_TRUE")

	if !boolEnv("TEST_BOOL_TRUE", []string{"false"}) {
		t.Error("期望返回 true")
	}
	if boolEnv("TEST_BOOL_TRUE", []string{"true"}) {
		t.Error("期望返回 false")
	}
	if boolEnv("TEST_BOOL_MISSING", []string{"false"}) {
		t.Error("未设置的环境变量期望返回 false")
	}
}
