package heartbeat

import (
	"context"
	"fmt"
	"strconv"
	"strings"
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

// Start 启动周期任务。
// 对齐 Python: start (heartbeat.py L128-137)：幂等；先睡 interval 再 tick。
func (s *GatewayHeartbeatService) Start(ctx context.Context) error {
	if s.running.Load() {
		return nil
	}
	s.running.Store(true)
	loopCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()
	s.wg.Add(1)
	go s.runLoop(loopCtx)
	s.mu.Lock()
	every := s.config.IntervalSeconds
	s.mu.Unlock()
	logger.Info(logComponent).
		Float64("every_seconds", every).
		Msg("Gateway heartbeat started")
	return nil
}

// Stop 停止周期任务。
// 对齐 Python: stop (heartbeat.py L139-149)：cancel 中断 sleep 与 in-flight SendRequest，
// wg.Wait 等待循环退出（对齐 await self._task 吞 CancelledError）。
func (s *GatewayHeartbeatService) Stop() {
	s.running.Store(false)
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
	logger.Info(logComponent).Msg("Gateway heartbeat stopped")
}

// IsRunning 是否运行中。
// 对齐 Python: is_running (heartbeat.py L151-153)。
func (s *GatewayHeartbeatService) IsRunning() bool {
	return s.running.Load()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// runLoop 主循环：先睡 interval 再 tick（逐字对齐 Python _run_loop L155-165，已拍板）。
// time.After 每轮进入 select 前从 mu 保护下快照最新 IntervalSeconds——
// 对齐 Python 每轮 sleep 读最新值（set_conf 改间隔下轮生效）。
// tick 内部自行 recover；循环层不因 tick 异常退出（对齐 Python except 仅日志）。
func (s *GatewayHeartbeatService) runLoop(ctx context.Context) {
	defer s.wg.Done()
	for s.running.Load() {
		s.mu.Lock()
		interval := s.config.IntervalSeconds
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(interval * float64(time.Second))):
			// 先睡 interval 再 tick（对齐 L159），启动后首跳等一个完整间隔
		}
		if s.running.Load() {
			s.tick(ctx)
		}
	}
}

// tick 执行一次探活：构造 E2A 发往 AgentServer，不向 Channel 下发。
// 对齐 Python: _tick (heartbeat.py L167-256) 六步：
// ① 活跃时段判断 ② 构造标识与 envelope ③ 发送（可选超时）
// ④ 成功路径三级 content 提取 ⑤ relay 回传 ⑥ 失败路径。
// 完整实现于 11.11 Task 4 落地；此处先以最小占位保证 runLoop 编译。
func (s *GatewayHeartbeatService) tick(ctx context.Context) {
	_ = ctx
}

// isActiveNow 根据 active_hours 判断当前时间心跳是否应当生效。
// 对齐 Python: _is_active_now (heartbeat.py L268-301)。
// 偏差⑤：nowFn 为 float64 Unix 秒，经 time.Unix().Local() 还原本地时区对齐 Python time.localtime()。
func (s *GatewayHeartbeatService) isActiveNow() bool {
	s.mu.Lock()
	activeHours := s.config.ActiveHours
	s.mu.Unlock()
	normalized := NormalizeActiveHours(activeHours)
	if len(normalized) == 0 {
		return true
	}
	startStr, startOK := normalized["start"]
	endStr, endOK := normalized["end"]
	if !startOK || !endOK {
		return true
	}
	startMinutes, err := parseHM(startStr)
	if err != nil {
		logger.Warn(logComponent).
			Any("active_hours", normalized).Err(err).
			Msg("Invalid heartbeat active_hours config")
		return true
	}
	endMinutes, err := parseHM(endStr)
	if err != nil {
		logger.Warn(logComponent).
			Any("active_hours", normalized).Err(err).
			Msg("Invalid heartbeat active_hours config")
		return true
	}
	now := time.Unix(int64(s.nowFn()), 0).Local()
	nowMinutes := now.Hour()*60 + now.Minute()
	if startMinutes <= endMinutes {
		// 普通区间：如 08:00-22:00
		return startMinutes <= nowMinutes && nowMinutes < endMinutes
	}
	// 跨午夜区间：如 22:00-06:00
	return nowMinutes >= startMinutes || nowMinutes < endMinutes
}

// parseHM 解析 "HH:MM" 为当日分钟数。
// 对齐 Python _parse_hm（split(":", 1) + 长度检查 + int 解析）。
func parseHM(s string) (int, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid time format: %q", s)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf("invalid hour %q: %w", parts[0], err)
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, fmt.Errorf("invalid minute %q: %w", parts[1], err)
	}
	return h*60 + m, nil
}

// minutesToHM 分钟数转 "HH:MM"（对齐 Python divmod(minutes, 60)）。
func minutesToHM(minutes int) string {
	h, m := minutes/60, minutes%60
	return fmt.Sprintf("%02d:%02d", h, m)
}
