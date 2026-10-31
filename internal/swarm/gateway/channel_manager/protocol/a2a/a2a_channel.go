package a2a

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// A2AChannelConfig A2A 入站通道配置。
//
// Python: a2a_connect.py A2AChannelConfig (L23-35)，11 字段一一对应；
// ChannelID 在 Python 为固定默认值 "a2a"（无 env），Go 同。
type A2AChannelConfig struct {
	// Enabled 是否启用通道（默认 false）
	Enabled bool
	// Host HTTP 监听地址
	Host string
	// Port HTTP 监听端口
	Port int
	// RPCPath JSON-RPC 入口路径
	RPCPath string
	// CardPath Agent Card 路径
	CardPath string
	// ExtendedCardPath Extended Agent Card 路径
	ExtendedCardPath string
	// ProtocolVersion 写入 AgentInterface 的协议版本
	ProtocolVersion string
	// ChannelID 通道标识
	ChannelID string
	// AppName Agent Card name
	AppName string
	// AppDescription Agent Card description
	AppDescription string
	// AppVersion Agent Card version
	AppVersion string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentGateway
	// defaultListenPort A2A 默认监听端口
	defaultListenPort = 19100
	// defaultChannelID 通道标识默认值（Python 无 env，固定 "a2a"）
	defaultChannelID = "a2a"
	// pendingBufferSize 单个在途请求的应答队列容量（偏差 D11：有界背压）
	pendingBufferSize = 64
	// shutdownWakeupError 停止时唤醒在途请求的错误文案
	// Python: a2a_connect.py stop() L298
	shutdownWakeupError = "a2a channel stopped"
	// startProbeWindow Start 后的存活检查窗口（对齐 Python await sleep(0.2) 后判定）
	startProbeWindow = 200 * time.Millisecond
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ConfigFromEnv 从环境变量构造通道配置。
//
// getenv 可注入（生产传 os.Getenv），便于测试；所有字符串值 TrimSpace 后
// 为空则回落默认（对齐 Python str(...).strip() or 默认）。
// Python: app_gateway.py L1071-1107
func ConfigFromEnv(getenv func(string) string) A2AChannelConfig {
	cfg := A2AChannelConfig{
		Enabled:          a2aEnvBool(getenv("A2A_SERVER_ENABLED")),
		Host:             orDefault(strings.TrimSpace(getenv("A2A_SERVER_HOST")), "127.0.0.1"),
		Port:             defaultListenPort,
		RPCPath:          orDefault(strings.TrimSpace(getenv("A2A_SERVER_PATH")), "/a2a"),
		CardPath:         orDefault(strings.TrimSpace(getenv("A2A_SERVER_CARD_PATH")), "/.well-known/agent-card.json"),
		ExtendedCardPath: orDefault(strings.TrimSpace(getenv("A2A_SERVER_EXTENDED_CARD_PATH")), "/agent/authenticatedExtendedCard"),
		ProtocolVersion:  orDefault(strings.TrimSpace(getenv("A2A_SERVER_PROTOCOL_VERSION")), "1.0.0"),
		ChannelID:        defaultChannelID,
		// 偏差 D9：品牌串映射 JiuwenClaw → UapClaw
		AppName:        orDefault(strings.TrimSpace(getenv("A2A_SERVER_APP_NAME")), "UapClaw Gateway A2A Server"),
		AppDescription: orDefault(strings.TrimSpace(getenv("A2A_SERVER_APP_DESCRIPTION")), "A2A ingress for UapClaw Gateway"),
		AppVersion:     orDefault(strings.TrimSpace(getenv("A2A_SERVER_APP_VERSION")), "0.1.0"),
	}

	if raw := strings.TrimSpace(getenv("A2A_SERVER_PORT")); raw != "" {
		if p, err := strconv.Atoi(raw); err == nil && p > 0 {
			cfg.Port = p
		} else {
			// 审查惯例：Python int() 会直接抛 ValueError 炸装配；Go 静默回落但补 Warn 保证可观测
			logger.Warn(logComponent).
				Str("env", "A2A_SERVER_PORT").
				Str("value", raw).
				Msg("invalid A2A_SERVER_PORT, fallback to default")
		}
	}
	return cfg
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// orDefault 值非空则返回，否则返回默认值（对齐 Python `x.strip() or 默认`）。
func orDefault(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}

// a2aEnvBool 解析布尔型 env（1/true/yes/on 为真）。
//
// Python: app_gateway.py L1071-1075 的集合判断
func a2aEnvBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// getenvOS os.Getenv 的适配器。
func getenvOS(key string) string { return os.Getenv(key) }
