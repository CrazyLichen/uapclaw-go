package sdk

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentServerClientExtension AgentServer 客户端扩展接口，对齐 Python AgentServerClientExtension
// 持有真正的 AgentTransport 实现，通过 GetClient() 暴露
type AgentServerClientExtension interface {
	BaseExtension
	// GetClient 返回与 AgentServer 通信的客户端，对齐 Python get_client() @abstractmethod
	// 返回 transport.AgentTransport 接口（Go 用 AgentTransport 对齐规则 6 Transport 架构）
	GetClient() transport.AgentTransport
}

// AgentServerClientExtensionImpl AgentServerClientExtension 默认实现
type AgentServerClientExtensionImpl struct {
	BaseExtensionImpl
	// client 与 AgentServer 通信的传输层客户端
	client transport.AgentTransport
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────
// ──────────────────────────── 导出函数 ────────────────────────────

// GetClient 返回与 AgentServer 通信的客户端，对齐 Python AgentServerClientExtension.get_client()
func (e *AgentServerClientExtensionImpl) GetClient() transport.AgentTransport {
	return e.client
}

// Initialize 扩展初始化，对齐 Python AgentServerClientExtension.initialize()
func (e *AgentServerClientExtensionImpl) Initialize(ctx context.Context, config *extensions.ExtensionConfig) error {
	return nil
}

// Shutdown 扩展关闭，对齐 Python AgentServerClientExtension.shutdown()
func (e *AgentServerClientExtensionImpl) Shutdown(ctx context.Context) error {
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
