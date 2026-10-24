package plugin

import (
	"context"
	"time"

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

const (
	// cryptoRPCTimeout 单次加解密 RPC 兜底超时（M-04）。
	// Python 侧为同进程函数调用无挂起风险；Go 侧跨进程 gRPC 默认无 deadline，
	// 插件子进程挂起时会永久阻塞 config.get/set 等请求路径，故必须兜底
	// （与同包 Shutdown 的 2s 超时同一设计考量，取 5s 覆盖跨进程 GC/调度抖动）。
	cryptoRPCTimeout = 5 * time.Second
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期断言：GRPCPluginCryptoProvider 满足 crypto.CryptoProvider 接口
var _ crypto.CryptoProvider = (*GRPCPluginCryptoProvider)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGRPCPluginCryptoProvider 创建插件 crypto 代理
func NewGRPCPluginCryptoProvider(pc *PluginClient) *GRPCPluginCryptoProvider {
	return &GRPCPluginCryptoProvider{pc: pc}
}

// Encrypt 加密明文（代理到插件 Encrypt RPC）。
//
// 透传调用方 ctx；入参无 deadline 时施加 cryptoRPCTimeout 兜底
// （Background 调用点同样受超时保护）。
func (p *GRPCPluginCryptoProvider) Encrypt(ctx context.Context, plaintext string) (string, error) {
	ctx, cancel := withCryptoTimeout(ctx)
	defer cancel()
	return p.pc.Encrypt(ctx, plaintext)
}

// Decrypt 解密密文（代理到插件 Decrypt RPC）。
//
// 透传调用方 ctx；入参无 deadline 时施加 cryptoRPCTimeout 兜底（同 Encrypt）。
func (p *GRPCPluginCryptoProvider) Decrypt(ctx context.Context, ciphertext string) (string, error) {
	ctx, cancel := withCryptoTimeout(ctx)
	defer cancel()
	return p.pc.Decrypt(ctx, ciphertext)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// withCryptoTimeout 为无 deadline 的 ctx 施加 cryptoRPCTimeout 兜底超时；
// 调用方已带 deadline 时原样返回（noop cancel），不叠加超时。
func withCryptoTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, cryptoRPCTimeout)
}
