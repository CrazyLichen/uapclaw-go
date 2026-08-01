package sdk

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CryptoUtility 加解密扩展接口，对齐 Python CryptoUtility
// ⤵️ 10.5.10 延后实现：CryptoProvider 接口待定义
type CryptoUtility interface {
	BaseExtension
	// GetCrypto 返回实际执行 encrypt/decrypt 的实例，对齐 Python get_crypto()
	// ⤵️ 10.5.10 延后：返回类型待定为 CryptoProvider 接口
	GetCrypto() any
}

// CryptoUtilityStub CryptoUtility 的 stub 实现，⤵️ 10.5.10 延后
type CryptoUtilityStub struct {
	BaseExtensionImpl
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetCrypto 返回 nil，⤵️ 10.5.10 延后实现后返回 CryptoProvider
func (c *CryptoUtilityStub) GetCrypto() any { return nil }

// Initialize 空实现，⤵️ 10.5.10 延后
func (c *CryptoUtilityStub) Initialize(ctx context.Context, config *extensions.ExtensionConfig) error {
	return nil
}

// Shutdown 空实现，对齐 Python CryptoUtility.shutdown()
func (c *CryptoUtilityStub) Shutdown(ctx context.Context) error { return nil }
