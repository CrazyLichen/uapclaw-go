package heartbeat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
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

// LastTickOK 最近一次心跳是否成功，nil 表示尚未执行过任何一次 tick。
// 对齐 Python: last_tick_ok property (heartbeat.py L258-261)。
func (s *GatewayHeartbeatService) LastTickOK() *bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastTickOK
}

// LastTickAt 最近一次心跳执行时间（Unix 时间戳），nil 表示尚未执行过。
// 对齐 Python: last_tick_at property (heartbeat.py L263-266)。
func (s *GatewayHeartbeatService) LastTickAt() *float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastTickAt
}

// GetHeartbeatConf 返回当前心跳配置摘要（every/target/active_hours）。
// 对齐 Python: get_heartbeat_conf (heartbeat.py L303-309)。
func (s *GatewayHeartbeatService) GetHeartbeatConf() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var relay any
	if s.config.RelayChannelID != nil {
		relay = *s.config.RelayChannelID
	}
	// 注意 nil-interface 陷阱：nil 的 map[string]string 装入 any 会变成
	// 非 nil interface（打印 map[]）——须显式保 nil，对齐 Python None → null
	var activeHours any
	if s.config.ActiveHours != nil {
		activeHours = NormalizeActiveHours(s.config.ActiveHours)
	}
	return map[string]any{
		"every":        s.config.IntervalSeconds,
		"target":       relay,
		"active_hours": activeHours,
	}
}

// SetHeartbeatConf 更新配置并按需重启。
// 对齐 Python: set_heartbeat_conf (heartbeat.py L311-354)。
// 内部重启用 context.Background()（脱离 Start 传入的 ctx 生命周期，
// 服务自身 cancel 管理——对齐 Python stop/start 不依赖外部 ctx）。
func (s *GatewayHeartbeatService) SetHeartbeatConf(every *float64, target *string, activeHours map[string]any) error {
	updated := false
	s.mu.Lock()
	if every != nil {
		if *every <= 0 {
			s.mu.Unlock()
			return fmt.Errorf("heartbeat 'every' must be > 0")
		}
		s.config.IntervalSeconds = *every
		updated = true
	}
	if target != nil {
		s.config.RelayChannelID = target
		updated = true
	}
	if activeHours != nil {
		// 原样存储（对齐 Python：不做值类型转换，数值 1320 存 int，
		// 由 NormalizeActiveHours 回转 "22:00"）
		s.config.ActiveHours = activeHours
		updated = true
	}
	s.mu.Unlock()

	if !updated {
		return nil
	}

	wasRunning := s.running.Load()
	if wasRunning {
		s.Stop()
	}

	// 重置最近一次心跳状态（对齐 L343-344）
	s.mu.Lock()
	s.lastTickOK = nil
	s.lastTickAt = nil
	s.mu.Unlock()

	if wasRunning {
		if err := s.Start(context.Background()); err != nil {
			return err
		}
	}

	s.mu.Lock()
	everyVal := s.config.IntervalSeconds
	var targetVal any
	if s.config.RelayChannelID != nil {
		targetVal = *s.config.RelayChannelID
	}
	activeVal := s.config.ActiveHours
	s.mu.Unlock()
	logger.Info(logComponent).
		Float64("every", everyVal).
		Any("target", targetVal).
		Any("active_hours", activeVal).
		Msg("Gateway heartbeat config updated")
	return nil
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
func (s *GatewayHeartbeatService) tick(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error(logComponent).
				Any("recover", r).
				Msg("Gateway heartbeat loop error")
		}
	}()

	// ① 活跃时段判断（对齐 L172-177）
	if !s.isActiveNow() {
		s.mu.Lock()
		activeHours := s.config.ActiveHours
		s.mu.Unlock()
		// 对齐 Python logger.debug("...: %r", self._config.active_hours)——打印原始配置
		logger.Debug(logComponent).
			Any("active_hours", activeHours).
			Msg("Gateway heartbeat skipped due to inactive hours")
		return
	}

	// ② 构造标识与 envelope（对齐 L179-197）
	ts := fmt.Sprintf("%x", time.Now().UnixMilli()) // format(int(time.time()*1000), "x")
	suffix := generateRandomHex(3)                  // secrets.token_hex(3)，6 hex 字符
	requestID := fmt.Sprintf("heartbeat-%s_%s", ts, suffix)
	sessionID := fmt.Sprintf("heartbeat_%s_%s", ts, suffix)
	s.mu.Lock()
	channelID := s.config.ChannelID
	s.mu.Unlock()
	envelope := e2a.E2AFromAgentFields(requestID,
		e2a.WithFieldChannelID(channelID),
		e2a.WithFieldSessionID(sessionID),
		e2a.WithFieldParams(map[string]any{
			"heartbeat": HeartbeatPrompt,
			"run": map[string]any{
				"kind": "heartbeat",
				"context": map[string]any{
					"reason":     "interval",
					"session_id": sessionID,
				},
			},
		}),
	)

	// ③ 发送（对齐 L199-205 双分支）
	tickCtx := ctx
	var cancel context.CancelFunc
	s.mu.Lock()
	timeout := s.config.TimeoutSeconds
	s.mu.Unlock()
	if timeout != nil && *timeout > 0 {
		tickCtx, cancel = context.WithTimeout(ctx, time.Duration(*timeout*float64(time.Second)))
		defer cancel()
	}
	// nil 客户端守卫（对齐 Python agent_client=None 时 send_request 抛
	// AttributeError 被 except 捕获置 failed——Go nil 接口方法调用直接
	// panic，须显式拦截为失败分支；cron runAgent 同款先例）
	if s.agentClient == nil {
		s.mu.Lock()
		s.lastTickOK = ptrBool(false)
		s.lastTickAt = ptrFloat64(s.nowFn())
		s.mu.Unlock()
		logger.Warn(logComponent).
			Str("request_id", requestID).
			Msg("Gateway heartbeat request failed")
		return
	}
	resp, err := s.agentClient.SendRequest(tickCtx, envelope)

	if err != nil {
		// ⑥ 失败路径（对齐 L245-256）
		s.mu.Lock()
		s.lastTickOK = ptrBool(false)
		s.lastTickAt = ptrFloat64(s.nowFn())
		s.mu.Unlock()
		if errors.Is(err, context.DeadlineExceeded) {
			logger.Warn(logComponent).
				Str("request_id", requestID).
				Float64("timeout_seconds", func() float64 {
					if timeout != nil {
						return *timeout
					}
					return 0
				}()).
				Msg("Gateway heartbeat timeout")
		} else {
			logger.Warn(logComponent).
				Str("request_id", requestID).
				Err(err).
				Msg("Gateway heartbeat request failed")
		}
		return
	}

	// ④ 成功路径（对齐 L206-226）
	nowTS := s.nowFn()
	s.mu.Lock()
	s.lastTickOK = ptrBool(true)
	s.lastTickAt = ptrFloat64(nowTS)
	s.mu.Unlock()
	payload := resp.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	heartbeatContent := ""
	if hb, ok := payload["heartbeat"].(string); ok {
		heartbeatContent = hb
	}
	if heartbeatContent == "" {
		// 兼容 Agent 执行 HEARTBEAT.md 任务时返回的 chat 结构：
		// payload = {"content": {"output": "...", "result_type": "answer"}}
		switch content := payload["content"].(type) {
		case map[string]any:
			if output, ok := content["output"].(string); ok {
				heartbeatContent = output
			}
		case string:
			heartbeatContent = content
		}
	}
	logger.Info(logComponent).
		Str("content", heartbeatContent).
		Msg("Gateway heartbeat content")
	if strings.Contains(strings.ToUpper(heartbeatContent), HeartbeatOK) {
		logger.Info(logComponent).
			Str("request_id", requestID).
			Float64("last_tick_at", nowTS).
			Msg("Gateway heartbeat OK")
	} else {
		logger.Info(logComponent).
			Str("request_id", requestID).
			Float64("last_tick_at", nowTS).
			Msg("Gateway heartbeat complete")
	}

	// ⑤ relay 回传（对齐 L229-243）
	s.mu.Lock()
	relayChannelID := s.config.RelayChannelID
	s.mu.Unlock()
	if relayChannelID != nil && *relayChannelID != "" && s.publisher != nil {
		// 直接构造 Message（偏差⑥：NewEventMessage ID 自动 UUID；
		// Python 显式 id=f"heartbeat-relay-{request_id}" 需自定义 ID）
		relayMsg := &schema.Message{
			ID:        fmt.Sprintf("heartbeat-relay-%s", requestID),
			Type:      schema.MessageTypeEvent,
			ChannelID: *relayChannelID,
			SessionID: sessionID,
			Params:    json.RawMessage(`{}`),
			Timestamp: nowTS,
			OK:        true,
			Payload:   map[string]any{"heartbeat": heartbeatContent},
			EventType: schema.EventTypeHeartbeatRelay,
		}
		s.publisher.PublishRobotMessages(relayMsg)
		logger.Debug(logComponent).
			Str("relay_channel_id", *relayChannelID).
			Msg("Gateway heartbeat relay to channel")
	}
}

// generateRandomHex 生成 n 字节的随机 hex（2n 字符）。
// 对齐 Python secrets.token_hex(n)；与 routing/session_map.go 同款实现
// （包私有复制不跨包引用——提取公共会引入额外重构，超出本节边界）。
func generateRandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())[:n*2]
	}
	return hex.EncodeToString(b)
}

// ptrBool bool 指针辅助。
func ptrBool(v bool) *bool { return &v }

// ptrFloat64 float64 指针辅助。
func ptrFloat64(v float64) *float64 { return &v }

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
