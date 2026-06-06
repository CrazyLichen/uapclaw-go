package crypto

import (
	"fmt"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 接口 ────────────────────────────

// BaseCrypt 加密算法抽象接口，密钥由调用方传入。
//
// 对应 Python: openjiuwen/core/common/security/crypt_utils.py BaseCrypt
//
// 适用场景：底层加密操作，密钥由调用方管理（如存储层编解码）。
type BaseCrypt interface {
	// Encrypt 使用指定密钥加密明文，返回密文字符串。
	Encrypt(key []byte, plaintext string) (string, error)
	// Decrypt 使用指定密钥解密密文，返回明文字符串。
	Decrypt(key []byte, ciphertext string) (string, error)
}

// ──────────────────────────── 结构体 ────────────────────────────

// CryptRegistry 加密算法注册表（并发安全）。
//
// 对应 Python: openjiuwen/core/common/security/crypt_utils.py CryptUtils
//
// 提供按名称注册、获取、注销加密算法的能力。
// 内部使用 sync.RWMutex 保护，支持多 goroutine 并发访问。
type CryptRegistry struct {
	mu     sync.RWMutex
	crypts map[string]BaseCrypt
}

// ──────────────────────────── 全局变量 ────────────────────────────

// globalRegistry 全局加密算法注册表实例。
var globalRegistry = NewCryptRegistry()

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCryptRegistry 创建加密算法注册表实例。
func NewCryptRegistry() *CryptRegistry {
	return &CryptRegistry{
		crypts: make(map[string]BaseCrypt),
	}
}

// Register 注册加密算法。若 name 已存在则覆盖。
//
// crypt 必须实现 BaseCrypt 接口，否则返回错误。
func (r *CryptRegistry) Register(name string, crypt BaseCrypt) error {
	if crypt == nil {
		return exception.NewBaseError(exception.StatusCommonEncryptionError,
			exception.WithMsg(fmt.Sprintf("注册的加密算法不能为 nil，名称: %s", name)))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.crypts[name] = crypt
	return nil
}

// Unregister 注销加密算法。
func (r *CryptRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.crypts, name)
}

// Get 获取加密算法。若名称不存在，返回 nil, false。
func (r *CryptRegistry) Get(name string) (BaseCrypt, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.crypts[name]
	return c, ok
}

// Register 在全局注册表中注册加密算法。
// 若 name 已存在则覆盖。
func Register(name string, crypt BaseCrypt) error {
	return globalRegistry.Register(name, crypt)
}

// Unregister 在全局注册表中注销加密算法。
func Unregister(name string) {
	globalRegistry.Unregister(name)
}

// Get 从全局注册表中获取加密算法。
func Get(name string) (BaseCrypt, bool) {
	return globalRegistry.Get(name)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 自动注册内置的 AES-256-GCM 算法到全局注册表。
// 对应 Python: AesGcmCrypt() 在模块加载时自动注册。
func init() {
	_ = Register(AesGcmName, &AesGcmCrypt{})
}
