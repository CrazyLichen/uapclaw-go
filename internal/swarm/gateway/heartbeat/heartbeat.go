package heartbeat

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HeartbeatConfig 心跳配置。
// 对齐 Python: HeartbeatConfig dataclass (heartbeat.py L59-76)。
type HeartbeatConfig struct {
	// IntervalSeconds 心跳间隔（秒），MUST > 0
	IntervalSeconds float64
	// TimeoutSeconds 单次心跳请求超时（秒），nil 表示不限
	TimeoutSeconds *float64
	// ChannelID 心跳请求 channel_id，默认 __heartbeat__
	ChannelID string
	// RelayChannelID 心跳响应回传的 channel_id（如 "web"），nil 不回传
	RelayChannelID *string
	// ActiveHours 生效时间段 {"start":"HH:MM","end":"HH:MM"}，nil 始终生效
	ActiveHours map[string]any
}

// AgentRequester 心跳服务所需的最小 AgentClient 接口。
// routing.AgentClient 隐式满足；测试 fake 零成本（复刻 cron.SchedulerAgentClient）。
type AgentRequester interface {
	// SendRequest 发送非流式请求并等待完整响应
	SendRequest(ctx context.Context, envelope *e2a.E2AEnvelope) (*schema.AgentResponse, error)
}

// MessagePublisher 消息发布接口（破环关键：message_handler 隐式实现）。
// 对齐 Python: MessageHandler.publish_robot_messages。
type MessagePublisher interface {
	// PublishRobotMessages 发布机器人消息
	PublishRobotMessages(msg *schema.Message)
}

// IHeartbeat 心跳接口。
// 对齐 Python: IHeartbeat ABC (heartbeat.py L79-99)。
type IHeartbeat interface {
	// Start 启动周期任务；之后每隔 IntervalSeconds 执行一次心跳
	Start(ctx context.Context) error
	// Stop 停止周期任务，不再发送心跳
	Stop()
	// IsRunning 返回周期任务是否正在运行
	IsRunning() bool
}

// GatewayHeartbeatService 周期性向 AgentServer 发送探活请求。
// 对齐 Python: GatewayHeartbeatService (heartbeat.py L102-354)。
type GatewayHeartbeatService struct {
	// agentClient AgentServer 客户端（接口注入）
	agentClient AgentRequester
	// config 心跳配置（可变，set_conf 修改，mu 保护）
	config *HeartbeatConfig
	// publisher 消息发布器（relay 回传用，可为 nil）
	publisher MessagePublisher
	// nowFn 时钟函数（可注入假时钟做确定性测试；偏差①：Python 无此参数）
	nowFn func() float64

	// mu 保护 config/lastTickOK/lastTickAt 的并发读写
	mu sync.Mutex
	// running 是否运行中（对齐 Python self._running）
	running atomic.Bool
	// cancel 主循环取消函数
	cancel context.CancelFunc
	// wg 等待 runLoop goroutine 退出
	wg sync.WaitGroup
	// lastTickOK 最近一次心跳是否成功，nil 表示尚未执行（对齐 Python None 三态，偏差④）
	lastTickOK *bool
	// lastTickAt 最近一次心跳执行时间（Unix 时间戳），nil 表示尚未执行
	lastTickAt *float64
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// HeartbeatChannelID 心跳请求使用的默认标识，AgentServer 可据此识别探活请求
	HeartbeatChannelID = "__heartbeat__"
	// HeartbeatOK 心跳正常响应标识
	HeartbeatOK = "HEARTBEAT_OK"
)

// HeartbeatPrompt 探活请求发送的 content，AgentServer 可识别为心跳。
// 逐字复制 Python heartbeat.py L26（含半角逗号空格，禁止改写）。
const HeartbeatPrompt = "如果你的workspace目录存在HEARTBEAT.md文件, 读取文件内容并且根据文件内容执行任务. 如果没有HEARTBEAT.md文件, 仅回复HEARTBEAT_OK"

// logComponent 日志组件
const logComponent = logger.ComponentGateway

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGatewayHeartbeatService 创建心跳服务。
// 对齐 Python: GatewayHeartbeatService.__init__(agent_client, config, message_handler)；
// nowFn 为 Go 侧时钟注入（偏差①：测试确定性，形态对齐 cron 11.10 先例）。
func NewGatewayHeartbeatService(
	agentClient AgentRequester,
	config *HeartbeatConfig,
	publisher MessagePublisher,
	nowFn func() float64,
) *GatewayHeartbeatService {
	if config == nil {
		config = &HeartbeatConfig{IntervalSeconds: 60, ChannelID: HeartbeatChannelID}
	}
	if config.ChannelID == "" {
		config.ChannelID = HeartbeatChannelID
	}
	if nowFn == nil {
		nowFn = func() float64 { return float64(time.Now().UnixNano()) / 1e9 }
	}
	return &GatewayHeartbeatService{
		agentClient: agentClient,
		config:      config,
		publisher:   publisher,
		nowFn:       nowFn,
	}
}

// NormalizeActiveHours 将 active_hours 的 start/end 规范为 "HH:MM" 字符串。
// 对齐 Python: normalize_active_hours (heartbeat.py L29-46)。
//
// YAML 中未加引号的 22:00 在 PyYAML 会解析为 1320（60 进制）；
// yaml.v3 解析为字符串（偏差②，实测验证）。本函数数字回转逻辑仍保留：
// web set_conf 传数字等路径语义完整。
func NormalizeActiveHours(activeHours map[string]any) map[string]string {
	if len(activeHours) == 0 {
		// 对齐 Python `if not active_hours: return active_hours` 原样语义
		// （nil 进 nil 出；空 map 进空 map 出）
		if activeHours == nil {
			return nil
		}
		return map[string]string{}
	}
	result := make(map[string]string)
	for k, v := range activeHours {
		switch val := v.(type) {
		case int:
			if k == "start" || k == "end" {
				result[k] = minutesToHM(val)
			} else {
				result[k] = fmt.Sprintf("%d", val)
			}
		case int64:
			if k == "start" || k == "end" {
				result[k] = minutesToHM(int(val))
			} else {
				result[k] = fmt.Sprintf("%d", val)
			}
		case float64:
			if k == "start" || k == "end" {
				result[k] = minutesToHM(int(val))
			} else {
				result[k] = fmt.Sprintf("%v", val)
			}
		case string:
			result[k] = val
		case nil:
			result[k] = ""
		default:
			result[k] = fmt.Sprintf("%v", val)
		}
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// minutesToHM 分钟数转 "HH:MM"（对齐 Python divmod(minutes, 60)）。
func minutesToHM(minutes int) string {
	h, m := minutes/60, minutes%60
	return fmt.Sprintf("%02d:%02d", h, m)
}
