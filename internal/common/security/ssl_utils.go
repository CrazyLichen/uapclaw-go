package security

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// GetSSLConfig 解析 SSL 配置环境变量，返回是否验证和证书路径。
//
// 对应 Python: SslUtils.get_ssl_config()
//
// 逻辑：
//   - urlIsHTTPS=false → 不验证
//   - verifySwitchEnv 环境变量命中 triggerValue → 不验证
//   - 否则必须提供 sslCertEnv 环境变量指向的证书路径
func GetSSLConfig(verifySwitchEnv, sslCertEnv string, triggerValue []string, urlIsHTTPS bool) (verify bool, certPath string, err error) {
	if !urlIsHTTPS {
		return false, "", nil
	}

	if boolEnv(verifySwitchEnv, triggerValue) {
		return false, "", nil
	}

	certPath = os.Getenv(sslCertEnv)
	if certPath == "" {
		return false, "", exception.BuildError(
			exception.StatusCommonSSLCertInvalid,
			exception.WithParam("reason", fmt.Sprintf("当 %s=true 时，必须提供 SSL 证书环境变量 %s", verifySwitchEnv, sslCertEnv)),
		)
	}
	return true, certPath, nil
}

// CreateStrictTLSConfig 创建严格 TLS 配置。
//
// 对应 Python: SslUtils.create_strict_ssl_context()
//
// 安全策略（对齐 Python）：
//   - 最低 TLS 1.2
//   - 密码套件限制为 ECDHE-AES256-GCM/ECDHE-AES128-GCM
//   - 如果提供 certPath，安全加载证书（SAFE_CERT_DIR 校验 + O_NOFOLLOW + 大小限制）
func CreateStrictTLSConfig(certPath string) (*tls.Config, error) {
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
	}

	if certPath != "" {
		if err := secureLoadCert(cfg, certPath); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// boolEnv 解析布尔环境变量，如果值在 triggerValue 中则返回 true。
//
// 对应 Python: SslUtils._bool_env()
func boolEnv(name string, triggerValue []string) bool {
	val := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	for _, tv := range triggerValue {
		if val == tv {
			return true
		}
	}
	return false
}

// secureLoadCert 安全加载 CA 证书文件到 TLS 配置。
//
// 对应 Python: SslUtils._secure_load_cert()
//
// 安全策略：
//   - 证书路径必须在 SAFE_CERT_DIR 目录下
//   - 使用 O_NOFOLLOW 防止符号链接攻击
//   - 文件大小必须在 1~1MB 之间
//   - 文件必须是常规文件
func secureLoadCert(cfg *tls.Config, certPath string) error {
	// 检查 SAFE_CERT_DIR
	realCertPath, err := resolvePath(certPath)
	if err != nil {
		return exception.BuildError(
			exception.StatusCommonSSLContextInitFailed,
			exception.WithParam("reason", "无法解析证书路径"),
			exception.WithCause(err),
		)
	}

	safeCertDir := os.Getenv("SAFE_CERT_DIR")
	if safeCertDir == "" {
		return exception.BuildError(
			exception.StatusCommonSSLContextInitFailed,
			exception.WithParam("reason", "SAFE_CERT_DIR 环境变量未设置"),
		)
	}
	safePrefix, err := resolvePath(safeCertDir)
	if err != nil {
		return exception.BuildError(
			exception.StatusCommonSSLContextInitFailed,
			exception.WithParam("reason", "无法解析 SAFE_CERT_DIR 路径"),
			exception.WithCause(err),
		)
	}
	if !strings.HasPrefix(realCertPath, safePrefix+string(os.PathSeparator)) {
		return exception.BuildError(
			exception.StatusCommonSSLContextInitFailed,
			exception.WithParam("reason", "证书路径不在允许的目录范围内"),
		)
	}

	// 安全打开文件（O_NOFOLLOW 防止符号链接）
	fd, err := os.OpenFile(certPath, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return exception.BuildError(
			exception.StatusCommonSSLContextInitFailed,
			exception.WithParam("reason", "打开证书文件失败"),
			exception.WithCause(err),
		)
	}
	defer func() { _ = fd.Close() }()

	// 校验文件信息
	stat, err := fd.Stat()
	if err != nil {
		return exception.BuildError(
			exception.StatusCommonSSLContextInitFailed,
			exception.WithParam("reason", "获取证书文件信息失败"),
			exception.WithCause(err),
		)
	}
	if !stat.Mode().IsRegular() {
		return exception.BuildError(
			exception.StatusCommonSSLContextInitFailed,
			exception.WithParam("reason", "文件路径无效"),
		)
	}
	if stat.Size() == 0 || stat.Size() > 1024*1024 {
		return exception.BuildError(
			exception.StatusCommonSSLContextInitFailed,
			exception.WithParam("reason", "文件大小无效"),
		)
	}

	// 从已校验的 fd 读取证书内容（防止 TOCTOU：避免重新打开文件）
	caPEM, err := io.ReadAll(fd)
	if err != nil {
		return exception.BuildError(
			exception.StatusCommonSSLContextInitFailed,
			exception.WithParam("reason", "读取证书文件失败"),
			exception.WithCause(err),
		)
	}
	if len(caPEM) == 0 {
		return exception.BuildError(
			exception.StatusCommonSSLContextInitFailed,
			exception.WithParam("reason", "文件内容为空"),
		)
	}

	// 加载到 CertPool
	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caPEM) {
		return exception.BuildError(
			exception.StatusCommonSSLContextInitFailed,
			exception.WithParam("reason", "解析证书失败"),
		)
	}
	cfg.RootCAs = certPool

	return nil
}

// resolvePath 解析文件路径的真实路径。
func resolvePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}
