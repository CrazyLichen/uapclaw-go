package codec

import (
	"github.com/uapclaw/uapclaw-go/internal/common/crypto"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AesStorageCodec AES-256-GCM 存储编解码器。
//
// 容错模式：key 为空时 passthrough 不加解密，key 非空时解密失败返回原文并记录 Warn 日志。
// 对应 Python: openjiuwen/core/memory/codec/aes_storage_codec.py (AesStorageCodec)
// 对齐 Python 容错行为：Python 解密失败时返回原文，Go 也应返回原文而非 error，
// 确保 Go/Python 数据互操作性。
type AesStorageCodec struct {
	// key AES 加密密钥（nil/空 → passthrough）
	key []byte
	// provider 加密提供者（key 非空时初始化）
	provider *crypto.AesGcmProvider
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAesStorageCodec 创建存储编解码器。
// key 为 nil/空 → passthrough 模式；key 非空 → 必须为 32 字节，否则返回 error。
//
// 注意：Python 不校验 key 长度，Go 校验 key 必须 32 字节。Go 更安全——
// AES-256 本身要求 32 字节密钥，Python 不校验是缺陷。
// 此处保持 Go 严格校验。
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
// key 为空时原样返回（passthrough）；key 非空时加密返回加密结果。
// 加密失败时返回原文并记录 Warn 日志（对齐 Python 容错行为）。
func (c *AesStorageCodec) Encode(plaintext string) (string, error) {
	if c.provider == nil || plaintext == "" {
		return plaintext, nil
	}
	encrypted, err := c.provider.Encrypt(plaintext)
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Str("method", "Encode").
			Msg("加密失败，返回原文（对齐 Python 容错行为）")
		return plaintext, nil
	}
	return encrypted, nil
}

// Decode 解密密文。
// key 为空时原样返回（passthrough）；key 非空时解密返回解密结果。
// 解密失败时返回原文并记录 Warn 日志（对齐 Python 容错行为），
// 确保 Go 能读取 Python 写入的未加密（明文）数据。
func (c *AesStorageCodec) Decode(ciphertext string) (string, error) {
	if c.provider == nil || ciphertext == "" {
		return ciphertext, nil
	}
	decrypted, err := c.provider.Decrypt(ciphertext)
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Str("method", "Decode").
			Msg("解密失败，返回原文（对齐 Python 容错行为）")
		return ciphertext, nil
	}
	return decrypted, nil
}
