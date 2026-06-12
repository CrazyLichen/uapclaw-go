package codec

import (
	"github.com/uapclaw/uapclaw-go/internal/common/crypto"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AesStorageCodec AES-256-GCM 存储编解码器。
//
// 严格模式：key 为空时 passthrough 不加解密，key 非空时加解密失败返回 error。
// 对应 Python: openjiuwen/core/memory/codec/aes_storage_codec.py (AesStorageCodec)
// 差异：Python 加密失败返回原文（容错），Go 返回 error（严格模式）
type AesStorageCodec struct {
	// key AES 加密密钥（nil/空 → passthrough）
	key []byte
	// provider 加密提供者（key 非空时初始化）
	provider *crypto.AesGcmProvider
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAesStorageCodec 创建存储编解码器。
// key 为 nil/空 → passthrough 模式；key 非空 → 必须为 32 字节，否则返回 error。
func NewAesStorageCodec(key []byte) (*AesStorageCodec, error) {
	if len(key) == 0 {
		return &AesStorageCodec{key: nil, provider: nil}, nil
	}

	provider, err := crypto.NewAesGcmProvider(key)
	if err != nil {
		return nil, err
	}

	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)

	return &AesStorageCodec{
		key:      keyCopy,
		provider: provider,
	}, nil
}

// Encode 加密明文。
// key 为空时原样返回（passthrough）；key 非空时加密失败返回 error（严格模式）。
func (c *AesStorageCodec) Encode(plaintext string) (string, error) {
	if c.provider == nil || plaintext == "" {
		return plaintext, nil
	}
	return c.provider.Encrypt(plaintext)
}

// Decode 解密密文。
// key 为空时原样返回（passthrough）；key 非空时解密失败返回 error（严格模式）。
func (c *AesStorageCodec) Decode(ciphertext string) (string, error) {
	if c.provider == nil || ciphertext == "" {
		return ciphertext, nil
	}
	return c.provider.Decrypt(ciphertext)
}
