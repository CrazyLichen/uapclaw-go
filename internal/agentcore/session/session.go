package session

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/checkpointer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/tracer"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ProxySession 代理会话，将所有 InnerSession 方法委托给内部 stub。
// 对应 Python: openjiuwen/core/session/session.py ProxySession
//
// 修正 Python 遗漏：Python 的 ProxySession 未覆盖 actor_manager 和 close 方法，
// 导致这两个方法不委托给 stub。Go 实现中全部 8 个方法均委托给 stub。
//
// 使用模式：先 NewProxySession() 创建空实例，后续通过 SetSession() 注入真正的会话。
// stub 为 nil 时调用任何方法会 panic。
type ProxySession struct {
	// stub 被代理的底层会话
	stub interfaces.InnerSession
}

// ──────────────────────────── 枚举 ────────────────────────────

// Deprecated: 使用 InnerSession
type BaseSession = interfaces.InnerSession

// ──────────────────────────── 导出函数 ────────────────────────────

// NewProxySession 创建代理会话实例（stub 为 nil）。
// 必须在调用 InnerSession 方法之前通过 SetSession 注入底层会话，否则 panic。
func NewProxySession() *ProxySession {
	return &ProxySession{}
}

// SetSession 设置被代理的底层会话
func (p *ProxySession) SetSession(stub interfaces.InnerSession) {
	p.stub = stub
}

// Config 获取底层会话的配置
func (p *ProxySession) Config() config.SessionConfig {
	return p.stub.Config()
}

// State 获取底层会话的状态
func (p *ProxySession) State() state.SessionState {
	return p.stub.State()
}

// Tracer 获取底层会话的追踪器
func (p *ProxySession) Tracer() *tracer.Tracer {
	return p.stub.Tracer()
}

// StreamWriterManager 获取底层会话的流写入管理器
func (p *ProxySession) StreamWriterManager() *stream.StreamWriterManager {
	return p.stub.StreamWriterManager()
}

// SessionID 获取底层会话的唯一标识
func (p *ProxySession) SessionID() string {
	return p.stub.SessionID()
}

// Checkpointer 获取底层会话的检查点管理器
func (p *ProxySession) Checkpointer() checkpointer.Checkpointer {
	return p.stub.Checkpointer()
}

// ActorManager 获取底层会话的 Actor 管理器
func (p *ProxySession) ActorManager() any {
	return p.stub.ActorManager()
}

// Close 关闭底层会话
func (p *ProxySession) Close() error {
	return p.stub.Close()
}
