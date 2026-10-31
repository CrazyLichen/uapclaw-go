package a2a

import (
	"fmt"

	sdka2a "github.com/a2aproject/a2a-go/v2/a2a"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildAgentCard 由通道配置构造 A2A Agent Card。
//
// Python: a2a_connect.py start() 内 AgentCard 构造 (L213-238)
func BuildAgentCard(cfg *A2AChannelConfig) *sdka2a.AgentCard {
	if cfg == nil {
		return nil
	}
	return &sdka2a.AgentCard{
		Name:        cfg.AppName,
		Description: cfg.AppDescription,
		Version:     cfg.AppVersion,
		SupportedInterfaces: []*sdka2a.AgentInterface{{
			URL: fmt.Sprintf("http://%s:%d%s", cfg.Host, cfg.Port, cfg.RPCPath),
			// 偏差 D2：两侧 SDK 的规范常量均为大写 "JSONRPC"（Python 侧写小写属 lax string）
			ProtocolBinding: sdka2a.TransportProtocolJSONRPC,
			ProtocolVersion: sdka2a.ProtocolVersion(cfg.ProtocolVersion),
		}},
		Capabilities:       sdka2a.AgentCapabilities{Streaming: true, PushNotifications: false},
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		Skills: []sdka2a.AgentSkill{{
			ID:   "chat",
			Name: "chat",
			// 偏差 D9：品牌串映射 JiuwenClaw → UapClaw / jiuwenswarm → uapclawswarm
			Description: "Send user prompt to UapClaw via Gateway",
			Tags:        []string{"chat", "gateway", "uapclawswarm"},
			Examples:    []string{"Hello", "Summarize this"},
			InputModes:  []string{"text/plain"},
			OutputModes: []string{"text/plain"},
		}},
	}
}
