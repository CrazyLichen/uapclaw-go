package plugin

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/crypto"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GRPCPluginCryptoProvider 扩展插件 crypto 的宿主侧代理，实现 crypto.CryptoProvider。
//
// 将 Encrypt/Decrypt 映射到插件 gRPC 的两个 RPC（10.5.10）。
// 对齐 Python get_crypto() 返回的 CryptoProvider 生态位；每次调用一次 gRPC 往返
// （设计偏差 #2：子进程架构固有代价，config 面板低频路径可接受）。
type GRPCPluginCryptoProvider struct {
	// pc 插件客户端
	pc *PluginClient
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期断言：GRPCPluginCryptoProvider 满足 crypto.CryptoProvider 接口
var _ crypto.CryptoProvider = (*GRPCPluginCryptoProvider)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGRPCPluginCryptoProvider 创建插件 crypto 代理
func NewGRPCPluginCryptoProvider(pc *PluginClient) *GRPCPluginCryptoProvider {
	return &GRPCPluginCryptoProvider{pc: pc}
}

// Encrypt 加密明文（代理到插件 Encrypt RPC）
func (p *GRPCPluginCryptoProvider) Encrypt(plaintext string) (string, error) {
	return p.pc.Encrypt(context.Background(), plaintext)
}

// Decrypt 解密密文（代理到插件 Decrypt RPC）
func (p *GRPCPluginCryptoProvider) Decrypt(ciphertext string) (string, error) {
	return p.pc.Decrypt(context.Background(), ciphertext)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
