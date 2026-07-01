package codec

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/crypto"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AesStorageCodec AES-256-GCM 存储编解码器。
//
// 容错模式：key 为空时 passthrough 不加解密，key 非空时解密失败返回原文并记录 Warn 日志。
// 对应 Python: openjiuwen/core/memory/codec/aes_storage_codec.py (AesStorageCodec)
// 对齐 Python 容错行为：Python 解密失败时返回原文，Go 也应返回原文而非 error，
// 确保 Go/Python 数据互操作性。
//
// 通过 crypto 全局注册表获取加密算法（对齐 Python CryptUtils.get_crypt），
// 注册表中找不到时 passthrough 降级（对齐 Python 行为）。
type AesStorageCodec struct {
	// key AES 加密密钥（nil/空 → passthrough）
	key []byte
}

// keyedProvider 持有密钥的加密提供者适配器。
// 将 BaseCrypt（密钥由调用方传入）适配为 CryptoProvider（密钥内部持有）。
type keyedProvider struct {
	// key 加密密钥
	key []byte
	// crypt 加密算法实例
	crypt crypto.BaseCrypt
}

// ──────────────────────────── 常量 ────────────────────────────
const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAesStorageCodec 创建存储编解码器。
// key 为 nil/空 → passthrough 模式；key 非空 → 必须为 32 字节，否则返回 error。
//
// 注意：Python 不校验 key 长度，Go 校验 key 必须 32 字节。Go 更安全——
// AES-256 本身要求 32 字节密钥，Python 不校验是缺陷。
// 此处保持 Go 严格校验。
// 对齐 Python：不在构造时缓存 provider，而是在 Encode/Decode 时通过注册表动态查找。
func NewAesStorageCodec(key []byte) (*AesStorageCodec, error) {
	if len(key) == 0 {
		return &AesStorageCodec{key: nil}, nil
	}

	if len(key) != crypto.KeyLength {
		return nil, exception.NewBaseError(exception.StatusCommonEncryptionError,
			exception.WithMsg(fmt.Sprintf("密钥长度必须为 %d 字节，实际 %d 字节", crypto.KeyLength, len(key))))
	}

	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)

	return &AesStorageCodec{
		key: keyCopy,
	}, nil
}

// Encode 加密明文。
// key 为空时原样返回（passthrough）；key 非空时加密返回加密结果。
// 加密失败时返回原文并记录 Warn 日志（对齐 Python 容错行为）。
// 不返回 error（对齐 Python StorageCodec Protocol 签名：encode → str）。
func (c *AesStorageCodec) Encode(plaintext string) string {
	if plaintext == "" {
		return plaintext
	}
	provider := c.getProvider()
	if provider == nil {
		return plaintext
	}
	encrypted, err := provider.Encrypt(plaintext)
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Str("method", "Encode").
			Str("event_type", "MEMORY_PROCESS").
			Msg("加密失败，返回原文（对齐 Python 容错行为）")
		return plaintext
	}
	return encrypted
}

// Decode 解密密文。
// key 为空时原样返回（passthrough）；key 非空时解密返回解密结果。
// 解密失败时返回原文并记录 Warn 日志（对齐 Python 容错行为），
// 确保 Go 能读取 Python 写入的未加密（明文）数据。
// 不返回 error（对齐 Python StorageCodec Protocol 签名：decode → str）。
func (c *AesStorageCodec) Decode(ciphertext string) string {
	if ciphertext == "" {
		return ciphertext
	}
	provider := c.getProvider()
	if provider == nil {
		return ciphertext
	}
	decrypted, err := provider.Decrypt(ciphertext)
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Str("method", "Decode").
			Str("event_type", "MEMORY_PROCESS").
			Msg("解密失败，返回原文（对齐 Python 容错行为）")
		return ciphertext
	}
	return decrypted
}

// Encrypt 加密明文，委托 BaseCrypt 并传入持有密钥
func (p *keyedProvider) Encrypt(plaintext string) (string, error) {
	return p.crypt.Encrypt(p.key, plaintext)
}

// Decrypt 解密密文，委托 BaseCrypt 并传入持有密钥
func (p *keyedProvider) Decrypt(ciphertext string) (string, error) {
	return p.crypt.Decrypt(p.key, ciphertext)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getProvider 通过全局注册表获取加密算法，并构造持有当前密钥的 CryptoProvider。
// 对齐 Python AesStorageCodec 中 CryptUtils.get_crypt() 的动态查找模式：
//   - key 为空时返回 nil（passthrough）
//   - 注册表中找不到算法时返回 nil（passthrough 降级，对齐 Python）
//   - 找到时构造 AesGcmProvider 并返回
func (c *AesStorageCodec) getProvider() crypto.CryptoProvider {
	if len(c.key) == 0 {
		return nil
	}
	crypt, ok := crypto.Get(crypto.AesGcmName)
	if !ok {
		// 对齐 Python：CryptUtils.get_crypt() 返回 None 时 passthrough
		logger.Warn(logComponent).
			Str("method", "getProvider").
			Str("event_type", "MEMORY_PROCESS").
			Str("crypt_name", crypto.AesGcmName).
			Msg("注册表中未找到加密算法，passthrough 降级")
		return nil
	}
	return &keyedProvider{key: c.key, crypt: crypt}
}
