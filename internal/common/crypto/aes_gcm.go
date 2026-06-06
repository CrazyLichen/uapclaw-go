package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// NonceLength GCM nonce 长度（字节）。
	// 对应 Python: NONCE_LENGTH = 12
	NonceLength = 12

	// KeyLength AES-256 密钥长度（字节）。
	// 对应 Python: AES_KEY_LENGTH = 32
	KeyLength = 32

	// TagLength GCM 认证标签长度（字节）。
	// 对应 Python: TAG_LENGTH = 16
	TagLength = 16

	// AesGcmName 注册表中的 AES-GCM 算法名称。
	// 对应 Python: CryptUtils.AES_GCM_CRYPT_NAME = "aes_gcm"
	AesGcmName = "aes_gcm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AesGcmCrypt AES-256-GCM 加密实现。
//
// 密文格式：hex(nonce) + hex(tag) + hex(ciphertext)，与 Python 完全兼容。
// 对应 Python: openjiuwen/core/common/security/crypt_utils.py AesGcmCrypt
type AesGcmCrypt struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// Encrypt 使用 AES-256-GCM 加密明文。
//
// key 必须为 32 字节，返回 hex(nonce) + hex(tag) + hex(ciphertext) 格式密文。
// 密文格式与 Python AesGcmCrypt.encrypt 完全一致，可互解。
func (c *AesGcmCrypt) Encrypt(key []byte, plaintext string) (string, error) {
	if err := validateKey(key); err != nil {
		return "", err
	}

	// 创建 AES cipher block
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", exception.NewBaseError(exception.StatusCommonEncryptionError,
			exception.WithMsg(fmt.Sprintf("创建 AES cipher 失败: %s", err.Error())))
	}

	// 创建 GCM mode（指定 tag 长度为 16 字节，与 Python 一致）
	gcm, err := cipher.NewGCMWithTagSize(block, TagLength)
	if err != nil {
		return "", exception.NewBaseError(exception.StatusCommonEncryptionError,
			exception.WithMsg(fmt.Sprintf("创建 GCM mode 失败: %s", err.Error())))
	}

	// 随机生成 nonce
	nonce := make([]byte, NonceLength)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", exception.NewBaseError(exception.StatusCommonEncryptionError,
			exception.WithMsg(fmt.Sprintf("生成 nonce 失败: %s", err.Error())))
	}

	// GCM Seal 返回 ciphertext + tag（tag 在末尾）
	sealed := gcm.Seal(nil, nonce, []byte(plaintext), nil)

	// 拆分 ciphertext 和 tag
	rawCiphertext := sealed[:len(sealed)-TagLength]
	tag := sealed[len(sealed)-TagLength:]

	// 拼接：hex(nonce) + hex(tag) + hex(ciphertext)
	// 与 Python 的拼接顺序一致：nonce_hex + tag_hex + cipher_text_hex
	result := hex.EncodeToString(nonce) +
		hex.EncodeToString(tag) +
		hex.EncodeToString(rawCiphertext)

	return result, nil
}

// Decrypt 使用 AES-256-GCM 解密密文。
//
// 密文格式必须为 hex(nonce) + hex(tag) + hex(ciphertext)，与 Python 一致。
// 解密时会验证认证标签，确保密文未被篡改。
func (c *AesGcmCrypt) Decrypt(key []byte, ciphertext string) (string, error) {
	if err := validateKey(key); err != nil {
		return "", err
	}

	// 计算各部分的 hex 长度
	nonceHexLen := NonceLength * 2    // 24
	tagHexLen := TagLength * 2        // 32
	minLen := nonceHexLen + tagHexLen // 56

	if len(ciphertext) < minLen {
		return "", exception.NewBaseError(exception.StatusCommonDecryptionError,
			exception.WithMsg(fmt.Sprintf("密文过短: 期望至少 %d 字符，实际 %d 字符", minLen, len(ciphertext))))
	}

	// 拆分各部分
	nonceHex := ciphertext[:nonceHexLen]
	tagHex := ciphertext[nonceHexLen : nonceHexLen+tagHexLen]
	ciphertextHex := ciphertext[nonceHexLen+tagHexLen:]

	// hex 解码
	nonceBytes, err := hex.DecodeString(nonceHex)
	if err != nil {
		return "", exception.NewBaseError(exception.StatusCommonDecryptionError,
			exception.WithMsg(fmt.Sprintf("nonce hex 解码失败: %s", err.Error())))
	}
	tagBytes, err := hex.DecodeString(tagHex)
	if err != nil {
		return "", exception.NewBaseError(exception.StatusCommonDecryptionError,
			exception.WithMsg(fmt.Sprintf("tag hex 解码失败: %s", err.Error())))
	}
	ciphertextBytes, err := hex.DecodeString(ciphertextHex)
	if err != nil {
		return "", exception.NewBaseError(exception.StatusCommonDecryptionError,
			exception.WithMsg(fmt.Sprintf("ciphertext hex 解码失败: %s", err.Error())))
	}

	// 校验 nonce 和 tag 长度
	if len(nonceBytes) != NonceLength {
		return "", exception.NewBaseError(exception.StatusCommonDecryptionError,
			exception.WithMsg(fmt.Sprintf("nonce 长度错误: %d 字节，期望 %d 字节", len(nonceBytes), NonceLength)))
	}
	if len(tagBytes) != TagLength {
		return "", exception.NewBaseError(exception.StatusCommonDecryptionError,
			exception.WithMsg(fmt.Sprintf("tag 长度错误: %d 字节，期望 %d 字节", len(tagBytes), TagLength)))
	}

	// 创建 AES cipher block
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", exception.NewBaseError(exception.StatusCommonDecryptionError,
			exception.WithMsg(fmt.Sprintf("创建 AES cipher 失败: %s", err.Error())))
	}

	// 创建 GCM mode
	gcm, err := cipher.NewGCMWithTagSize(block, TagLength)
	if err != nil {
		return "", exception.NewBaseError(exception.StatusCommonDecryptionError,
			exception.WithMsg(fmt.Sprintf("创建 GCM mode 失败: %s", err.Error())))
	}

	// 拼接 ciphertext + tag（Go GCM Open 期望格式：ciphertext||tag）
	combined := append(ciphertextBytes, tagBytes...)

	// 解密 + 验证 tag
	plaintext, err := gcm.Open(nil, nonceBytes, combined, nil)
	if err != nil {
		return "", exception.NewBaseError(exception.StatusCommonDecryptionError,
			exception.WithMsg(fmt.Sprintf("解密失败（密文可能被篡改）: %s", err.Error())))
	}

	return string(plaintext), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// validateKey 校验 AES-256 密钥长度。
func validateKey(key []byte) error {
	if len(key) != KeyLength {
		return exception.NewBaseError(exception.StatusCommonEncryptionError,
			exception.WithMsg(fmt.Sprintf("密钥长度必须为 %d 字节，实际 %d 字节", KeyLength, len(key))))
	}
	return nil
}
