// Package crypto 提供 AES 加密/解密工具。
//
// 用于 API Key 等敏感信息的加密存储和安全读取。
//
// 本包包含两层抽象：
//   - 底层：BaseCrypt 接口 + AesGcmCrypt 实现 + CryptRegistry 注册表
//     密钥由调用方传入，适用于存储层编解码等底层场景。
//   - 上层：CryptoProvider 接口 + AesGcmProvider 封装 + 全局提供者管理
//     密钥由实现内部持有，适用于配置文件解密、通道参数加解密等上层场景。
//
// 密文格式与 Python (openjiuwen) 完全兼容：
//
//	hex(nonce_12字节) + hex(tag_16字节) + hex(ciphertext)
//
// 文件目录：
//
//	crypto/
//	├── doc.go           # 包文档
//	├── aes_gcm.go       # AES-256-GCM 加解密实现
//	├── registry.go      # 加密算法注册表（并发安全）
//	├── crypto.go        # CryptoProvider 接口 + 全局管理 + AesGcmProvider
//	└── crypto_test.go   # 单元测试
//
// 对应 Python 代码：
//   - openjiuwen/core/common/security/crypt_utils.py  (BaseCrypt, AesGcmCrypt, CryptUtils)
//   - jiuwenswarm/common/security/base_crypto.py      (CryptoProvider, 全局提供者)
package crypto
