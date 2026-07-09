package gateway_push

import "github.com/uapclaw/uapclaw-go/internal/swarm/transport"

// ──────────────────────────── 非导出函数 ────────────────────────────

// 接口合规：ChannelTransport 实现 transport.AgentTransport
var _ transport.AgentTransport = (*ChannelTransport)(nil)
