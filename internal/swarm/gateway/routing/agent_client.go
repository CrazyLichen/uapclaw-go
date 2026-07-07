package routing

// ──────────────────────────── 结构体 ────────────────────────────

// AgentClient AgentServer 客户端
// 本次为骨架实现，后续完善 WebSocket 客户端连接。
type AgentClient struct{}

// ──────────────────────────── 枚 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentClient 创建 AgentServer 客户端
func NewAgentClient() *AgentClient {
	return &AgentClient{}
}

// ServerReady 检查 AgentServer 是否就绪
func (ac *AgentClient) ServerReady() bool {
	return false
}

// ──────────────────────────── 非导出函数 ────────────────────────────
