package crypto

import (
	"fmt"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 接口 ────────────────────────────

// CryptoProvider 加密提供者接口，密钥由实现内部持有。
//
// 对应 Python: jiuwenswarm/common/security/base_crypto.py CryptoProvider
//
// 适用场景：上层业务调用，调用方无需关心密钥管理。
// 例如：配置文件中的 api_key 解密、Web/TUI 通道敏感参数加解密。
type CryptoProvider interface {
	// Encrypt 加密明文，返回密文字符串。
	Encrypt(plaintext string) (string, error)
	// Decrypt 解密密文，返回明文字符串。
	Decrypt(ciphertext string) (string, error)
}

// ──────────────────────────── 结构体 ────────────────────────────

// AesGcmProvider 基于 AES-256-GCM 的 CryptoProvider 实现。
//
// 持有密钥，将 BaseCrypt（key 外传）封装为 CryptoProvider（key 内持）。
// 对应 Python: 通过 ExtensionRegistry.get_crypto_provider() 获取的 CryptoProvider 实例。
type AesGcmProvider struct {
	key   []byte
	crypt BaseCrypt
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// defaultProvider 全局默认加密提供者。
	defaultProvider CryptoProvider
	// providerMu 保护 defaultProvider 的读写锁。
	providerMu sync.RWMutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAesGcmProvider 创建基于 AES-256-GCM 的加密提供者。
//
// key 必须为 32 字节（AES-256），否则返回错误。
// 创建后可设置为全局提供者，供 config.DecryptFunc 等上层场景使用。
func NewAesGcmProvider(key []byte) (*AesGcmProvider, error) {
	if len(key) != KeyLength {
		return nil, exception.NewBaseError(exception.StatusCommonEncryptionError,
			exception.WithMsg(fmt.Sprintf("密钥长度必须为 %d 字节，实际 %d 字节", KeyLength, len(key))))
	}

	// 从全局注册表获取 AES-GCM 算法
	crypt, ok := Get(AesGcmName)
	if !ok {
		return nil, exception.NewBaseError(exception.StatusCommonEncryptionError,
			exception.WithMsg("AES-GCM 算法未注册，请确认 crypto 包已正确初始化"))
	}

	// 复制密钥，避免外部修改影响内部状态
	keyCopy := make([]byte, KeyLength)
	copy(keyCopy, key)

	return &AesGcmProvider{
		key:   keyCopy,
		crypt: crypt,
	}, nil
}

// Encrypt 加密明文。
// 委托内部 BaseCrypt 实例，使用持有密钥进行加密。
func (p *AesGcmProvider) Encrypt(plaintext string) (string, error) {
	return p.crypt.Encrypt(p.key, plaintext)
}

// Decrypt 解密密文。
// 委托内部 BaseCrypt 实例，使用持有密钥进行解密。
func (p *AesGcmProvider) Decrypt(ciphertext string) (string, error) {
	return p.crypt.Decrypt(p.key, ciphertext)
}

// SetCryptoProvider 设置全局加密提供者。
//
// 对应 Python: set_crypto_provider(provider)
func SetCryptoProvider(p CryptoProvider) {
	providerMu.Lock()
	defer providerMu.Unlock()
	defaultProvider = p
}

// GetCryptoProvider 获取全局加密提供者。
// 若未设置，返回 nil。
//
// 对应 Python: get_crypto_provider()
func GetCryptoProvider() CryptoProvider {
	providerMu.RLock()
	defer providerMu.RUnlock()
	return defaultProvider
}

// NewDecryptFunc 创建适配 config.DecryptFunc 的解密函数。
//
// 将 CryptoProvider 的 Decrypt 方法桥接为 config 包需要的签名：
//   - 当变量名包含 api_key 或 token 时，调用 CryptoProvider.Decrypt 解密
//   - 当变量名不包含敏感关键词时，返回 (value, false) 表示不解密
//   - 当全局 CryptoProvider 未设置时，返回 (value, false) 表示不解密
//
// 使用示例：
//
//	provider, _ := crypto.NewAesGcmProvider(key)
//	crypto.SetCryptoProvider(provider)
//	cfg, _ := config.New("config.yaml", config.WithDecrypt(crypto.NewDecryptFunc()))
func NewDecryptFunc() func(envName, value string) (string, bool) {
	// 敏感字段关键词（与 config/envvar.go 中 sensitiveKeywords 保持一致）
	sensitiveKeywords := []string{"api_key", "token"}

	return func(envName, value string) (string, bool) {
		provider := GetCryptoProvider()
		if provider == nil {
			return value, false
		}

		// 检查变量名是否包含敏感关键词
		normalized := strings.ReplaceAll(strings.ToLower(envName), "-", "_")
		isSensitive := false
		for _, kw := range sensitiveKeywords {
			if strings.Contains(normalized, kw) {
				isSensitive = true
				break
			}
		}
		if !isSensitive {
			return value, false
		}

		// 调用 CryptoProvider 解密
		decrypted, err := provider.Decrypt(value)
		if err != nil {
			return value, false
		}
		return decrypted, true
	}
}
