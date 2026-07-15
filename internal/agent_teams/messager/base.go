package messager

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// MessagerPeerConfig 消息通信对等节点配置
type MessagerPeerConfig struct {
	// AgentID Agent 标识
	AgentID string `json:"agent_id"`
	// PeerID 对等节点标识
	PeerID string `json:"peer_id,omitempty"`
	// Addrs 地址列表
	Addrs []string `json:"addrs,omitempty"`
	// Metadata 元数据
	Metadata map[string]any `json:"metadata,omitempty"`
}

// MessagerTransportConfig 消息通信传输配置
type MessagerTransportConfig struct {
	// Backend 通信后端，"inprocess" | "pyzmq" 等
	Backend string `json:"backend"`
	// TeamName 团队名称
	TeamName string `json:"team_name"`
	// NodeID 节点标识
	NodeID string `json:"node_id,omitempty"`
	// DirectAddr 直连地址
	DirectAddr string `json:"direct_addr,omitempty"`
	// PubsubPublishAddr 发布订阅发布地址
	PubsubPublishAddr string `json:"pubsub_publish_addr,omitempty"`
	// PubsubSubscribeAddr 发布订阅订阅地址
	PubsubSubscribeAddr string `json:"pubsub_subscribe_addr,omitempty"`
	// ListenAddrs 监听地址列表
	ListenAddrs []string `json:"listen_addrs,omitempty"`
	// BootstrapPeers 引导对等节点列表
	BootstrapPeers []MessagerPeerConfig `json:"bootstrap_peers,omitempty"`
	// KnownPeers 已知对等节点列表
	KnownPeers []MessagerPeerConfig `json:"known_peers,omitempty"`
	// RequestTimeout 请求超时秒数
	RequestTimeout float64 `json:"request_timeout"`
	// Metadata 元数据
	Metadata map[string]any `json:"metadata,omitempty"`
}

// SubscriptionHandle 订阅句柄
type SubscriptionHandle struct {
	// SubscriptionID 订阅标识
	SubscriptionID string `json:"subscription_id"`
	// Topic 订阅主题
	Topic string `json:"topic"`
	// AgentID Agent 标识
	AgentID string `json:"agent_id,omitempty"`
	// BackendMetadata 后端元数据
	BackendMetadata map[string]any `json:"backend_metadata,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMessagerTransportConfig 创建默认消息通信传输配置。
// 默认值：backend="inprocess", team_name="default", request_timeout=10.0
func NewMessagerTransportConfig() MessagerTransportConfig {
	return MessagerTransportConfig{
		Backend:        "inprocess",
		TeamName:       "default",
		RequestTimeout: 10.0,
	}
}

// BroadcastTopic 返回广播主题名称，格式为 "team:{team_name}:broadcast"
func (c MessagerTransportConfig) BroadcastTopic() string {
	return fmt.Sprintf("team:%s:broadcast", c.TeamName)
}

// CreateMessager 根据 config 构建 Messager 实例。
// ⤵️ 回填: 9.65 — 当前返回 nil, nil
func CreateMessager(_ MessagerTransportConfig) (any, error) {
	// ⤵️ 回填: 9.65 — 根据 backend 选择 InProcessMessager 或 PyZmqMessager
	return nil, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
