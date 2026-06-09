package security

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	_ = os.Setenv("TEST_SSL_VERIFY_OFF", "false")
	defer func() { _ = os.Unsetenv("TEST_SSL_VERIFY_OFF") }()

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
	_ = os.Unsetenv("TEST_SSL_CERT_MISSING")
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

	_ = os.Unsetenv("TEST_SSL_VERIFY_EMPTY")
	_ = os.Setenv("TEST_SSL_CERT_VALID", certFile)
	defer func() { _ = os.Unsetenv("TEST_SSL_CERT_VALID") }()

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

func TestGetSSLConfig_triggerValue多个值命中(t *testing.T) {
	_ = os.Setenv("TEST_SSL_VERIFY_MULTI", "no")
	defer func() { _ = os.Unsetenv("TEST_SSL_VERIFY_MULTI") }()

	verify, certPath, err := GetSSLConfig("TEST_SSL_VERIFY_MULTI", "SSL_CERT", []string{"false", "no", "0"}, true)
	if err != nil {
		t.Fatalf("triggerValue 命中不应报错: %v", err)
	}
	if verify {
		t.Error("triggerValue 命中期望 verify=false")
	}
	if certPath != "" {
		t.Error("triggerValue 命中期望 certPath 为空")
	}
}

func TestGetSSLConfig_triggerValue未命中(t *testing.T) {
	_ = os.Setenv("TEST_SSL_VERIFY_OTHER", "yes")
	defer func() { _ = os.Unsetenv("TEST_SSL_VERIFY_OTHER") }()
	_ = os.Unsetenv("TEST_SSL_CERT_OTHER")

	verify, _, err := GetSSLConfig("TEST_SSL_VERIFY_OTHER", "TEST_SSL_CERT_OTHER", []string{"false", "no"}, true)
	if err == nil {
		t.Fatal("triggerValue 未命中且无证书应报错")
	}
	if verify {
		t.Error("出错时期望 verify=false")
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
	if cfg.RootCAs != nil {
		t.Error("无证书时期望 RootCAs 为 nil")
	}
}

func TestCreateStrictTLSConfig_无效证书路径(t *testing.T) {
	_ = os.Setenv("SAFE_CERT_DIR", "/nonexistent")
	defer func() { _ = os.Unsetenv("SAFE_CERT_DIR") }()

	_, err := CreateStrictTLSConfig("/nonexistent/cert.pem")
	if err == nil {
		t.Fatal("无效证书路径应报错")
	}
}

func TestCreateStrictTLSConfig_有效证书加载(t *testing.T) {
	tmpDir := t.TempDir()

	certPEM := generateTestCertPEM(t)
	certFile := filepath.Join(tmpDir, "ca.pem")
	if err := os.WriteFile(certFile, certPEM, 0644); err != nil {
		t.Fatal(err)
	}

	_ = os.Setenv("SAFE_CERT_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("SAFE_CERT_DIR") }()

	cfg, err := CreateStrictTLSConfig(certFile)
	if err != nil {
		t.Fatalf("有效证书不应报错: %v", err)
	}
	if cfg.RootCAs == nil {
		t.Error("有证书时期望 RootCAs 不为 nil")
	}
}

func TestBoolEnv(t *testing.T) {
	_ = os.Setenv("TEST_BOOL_TRUE", "false")
	defer func() { _ = os.Unsetenv("TEST_BOOL_TRUE") }()

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

func TestBoolEnv_大小写和空格(t *testing.T) {
	_ = os.Setenv("TEST_BOOL_CASE", " FALSE ")
	defer func() { _ = os.Unsetenv("TEST_BOOL_CASE") }()

	if !boolEnv("TEST_BOOL_CASE", []string{"false"}) {
		t.Error("带空格和大写的值应该匹配")
	}
}

func TestBoolEnv_空triggerValue(t *testing.T) {
	_ = os.Setenv("TEST_BOOL_SOME", "value")
	defer func() { _ = os.Unsetenv("TEST_BOOL_SOME") }()

	if boolEnv("TEST_BOOL_SOME", []string{}) {
		t.Error("空 triggerValue 应返回 false")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestSecureLoadCert_SAFE_CERT_DIR未设置(t *testing.T) {
	_ = os.Unsetenv("SAFE_CERT_DIR")

	cfg := &tls.Config{}
	err := secureLoadCert(cfg, "/some/path/cert.pem")
	if err == nil {
		t.Fatal("SAFE_CERT_DIR 未设置应报错")
	}
	// 错误通过 BuildError 构建，Error() 格式为 [code] message
	// 验证是 SSL 上下文初始化失败错误即可
	if !strings.Contains(err.Error(), "188000") {
		t.Errorf("错误信息应包含状态码 188000，实际: %v", err)
	}
}

func TestSecureLoadCert_证书路径不在安全目录内(t *testing.T) {
	tmpDir := t.TempDir()
	otherDir := t.TempDir()

	_ = os.Setenv("SAFE_CERT_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("SAFE_CERT_DIR") }()

	cfg := &tls.Config{}
	err := secureLoadCert(cfg, filepath.Join(otherDir, "cert.pem"))
	if err == nil {
		t.Fatal("证书路径不在安全目录内应报错")
	}
}

func TestSecureLoadCert_证书文件不存在(t *testing.T) {
	tmpDir := t.TempDir()

	_ = os.Setenv("SAFE_CERT_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("SAFE_CERT_DIR") }()

	cfg := &tls.Config{}
	err := secureLoadCert(cfg, filepath.Join(tmpDir, "nonexistent.pem"))
	if err == nil {
		t.Fatal("证书文件不存在应报错")
	}
}

func TestSecureLoadCert_证书文件为符号链接(t *testing.T) {
	tmpDir := t.TempDir()

	// 创建真实文件和符号链接
	realFile := filepath.Join(tmpDir, "real_cert.pem")
	if err := os.WriteFile(realFile, []byte("dummy"), 0644); err != nil {
		t.Fatal(err)
	}
	linkFile := filepath.Join(tmpDir, "link_cert.pem")
	if err := os.Symlink(realFile, linkFile); err != nil {
		t.Fatal(err)
	}

	_ = os.Setenv("SAFE_CERT_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("SAFE_CERT_DIR") }()

	cfg := &tls.Config{}
	err := secureLoadCert(cfg, linkFile)
	if err == nil {
		t.Fatal("符号链接证书应报错")
	}
}

func TestSecureLoadCert_证书文件为空(t *testing.T) {
	tmpDir := t.TempDir()

	certFile := filepath.Join(tmpDir, "empty.pem")
	if err := os.WriteFile(certFile, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	_ = os.Setenv("SAFE_CERT_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("SAFE_CERT_DIR") }()

	cfg := &tls.Config{}
	err := secureLoadCert(cfg, certFile)
	if err == nil {
		t.Fatal("空证书文件应报错")
	}
}

func TestSecureLoadCert_证书文件过大(t *testing.T) {
	tmpDir := t.TempDir()

	// 创建超过 1MB 的文件
	bigFile := filepath.Join(tmpDir, "big.pem")
	bigData := make([]byte, 1024*1024+1) // 1MB + 1 字节
	if err := os.WriteFile(bigFile, bigData, 0644); err != nil {
		t.Fatal(err)
	}

	_ = os.Setenv("SAFE_CERT_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("SAFE_CERT_DIR") }()

	cfg := &tls.Config{}
	err := secureLoadCert(cfg, bigFile)
	if err == nil {
		t.Fatal("过大证书文件应报错")
	}
}

func TestSecureLoadCert_证书内容不是有效PEM(t *testing.T) {
	tmpDir := t.TempDir()

	certFile := filepath.Join(tmpDir, "invalid.pem")
	if err := os.WriteFile(certFile, []byte("this is not a valid PEM"), 0644); err != nil {
		t.Fatal(err)
	}

	_ = os.Setenv("SAFE_CERT_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("SAFE_CERT_DIR") }()

	cfg := &tls.Config{}
	err := secureLoadCert(cfg, certFile)
	if err == nil {
		t.Fatal("无效 PEM 内容应报错")
	}
}

func TestSecureLoadCert_证书文件是目录(t *testing.T) {
	tmpDir := t.TempDir()

	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	_ = os.Setenv("SAFE_CERT_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("SAFE_CERT_DIR") }()

	cfg := &tls.Config{}
	err := secureLoadCert(cfg, subDir)
	if err == nil {
		t.Fatal("目录作为证书路径应报错")
	}
}

func TestSecureLoadCert_成功加载有效证书(t *testing.T) {
	tmpDir := t.TempDir()

	certPEM := generateTestCertPEM(t)
	certFile := filepath.Join(tmpDir, "ca.pem")
	if err := os.WriteFile(certFile, certPEM, 0644); err != nil {
		t.Fatal(err)
	}

	_ = os.Setenv("SAFE_CERT_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("SAFE_CERT_DIR") }()

	cfg := &tls.Config{}
	err := secureLoadCert(cfg, certFile)
	if err != nil {
		t.Fatalf("有效证书不应报错: %v", err)
	}
	if cfg.RootCAs == nil {
		t.Error("成功加载后 RootCAs 不应为 nil")
	}
}

func TestSecureLoadCert_SAFE_CERT_DIR路径无法解析(t *testing.T) {
	tmpDir := t.TempDir()

	// 设置一个不存在的 SAFE_CERT_DIR 路径
	_ = os.Setenv("SAFE_CERT_DIR", "/nonexistent/safe/dir")
	defer func() { _ = os.Unsetenv("SAFE_CERT_DIR") }()

	cfg := &tls.Config{}
	err := secureLoadCert(cfg, filepath.Join(tmpDir, "cert.pem"))
	if err == nil {
		t.Fatal("SAFE_CERT_DIR 路径无法解析应报错")
	}
}

func TestSecureLoadCert_证书路径无法解析(t *testing.T) {
	tmpDir := t.TempDir()

	_ = os.Setenv("SAFE_CERT_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("SAFE_CERT_DIR") }()

	cfg := &tls.Config{}
	// 证书路径包含不存在的前缀目录，resolvePath 会失败
	err := secureLoadCert(cfg, "/nonexistent/deep/path/cert.pem")
	if err == nil {
		t.Fatal("证书路径无法解析应报错")
	}
}

func TestResolvePath_相对路径(t *testing.T) {
	result, err := resolvePath(".")
	if err != nil {
		t.Fatalf("解析当前目录不应报错: %v", err)
	}
	if !filepath.IsAbs(result) {
		t.Errorf("期望绝对路径，实际: %s", result)
	}
}

func TestResolvePath_绝对路径(t *testing.T) {
	result, err := resolvePath("/tmp")
	if err != nil {
		t.Fatalf("解析 /tmp 不应报错: %v", err)
	}
	if result != "/tmp" {
		t.Errorf("期望 /tmp，实际: %s", result)
	}
}

func TestResolvePath_不存在的路径(t *testing.T) {
	// EvalSymlinks 对不存在的路径会报错
	_, err := resolvePath("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Error("不存在的路径应该报错")
	}
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// generateTestCertPEM 生成自签名 CA 证书 PEM 字节
func generateTestCertPEM(t *testing.T) []byte {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成私钥失败: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test CA"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("创建证书失败: %v", err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
}
