// 本文件是外部团队客户端的 WebSocket 事件发布器与 hybrid messager。
// 语义对齐 Python messager/hybrid.py（publish-only）：外部进程（team-mcp
// server、外部操作者界面）没有进程内总线可订阅，它的出站事件经 Gateway
// 中继 RPC team.mq.publish 送回壳进程的进程内总线；订阅侧由 Gateway
// handler 承担（见 swarm/server），本包只承载出站方向。
// Python: openjiuwen/agent_teams/messager/hybrid.py
package messager

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	atevents "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// logComponent 组件常量。
const hybridLogComponent = logger.ComponentTeam

// hybridHeartbeatInterval WS 心跳间隔。
// Python: ws_connect(heartbeat=30.0)（hybrid.py:48）
const hybridHeartbeatInterval = 30 * time.Second

// ──────────────────────────── 结构体 ────────────────────────────

// WebSocketEventPublisher 经配置的 Gateway 中继发布标准团队事件。
// Python: WebSocketEventPublisher (hybrid.py:38-129)
type WebSocketEventPublisher struct {
	// url Gateway 中继 WS 端点（ws:// 或 wss://）
	url string
	// sessionID 团队会话标识（进 RPC params 与 topic 校验）
	sessionID string
	// teamName 团队标识（进 RPC params 与 topic 校验）
	teamName string
	// requestTimeout 单次发布的应答超时（秒）
	requestTimeout float64

	mu   sync.Mutex
	conn *websocket.Conn
}

// NewWebSocketEventPublisher 构造发布器。
// Python: __init__ (hybrid.py:41-53)
func NewWebSocketEventPublisher(url, sessionID, teamName string, requestTimeout float64) *WebSocketEventPublisher {
	return &WebSocketEventPublisher{
		url:            strings.TrimSpace(url),
		sessionID:      sessionID,
		teamName:       teamName,
		requestTimeout: requestTimeout,
	}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Start 建立 WS 连接（幂等：已连接则跳过）。
// Python: start (hybrid.py:55-63)
func (p *WebSocketEventPublisher) Start(ctx context.Context) error {
	if !strings.HasPrefix(p.url, "ws://") && !strings.HasPrefix(p.url, "wss://") {
		return fmt.Errorf("external_publish_url must use ws:// or wss://")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn != nil {
		return nil
	}
	dialer := websocket.Dialer{HandshakeTimeout: time.Duration(p.requestTimeout * float64(time.Second))}
	conn, _, err := dialer.DialContext(ctx, p.url, nil)
	if err != nil {
		return fmt.Errorf("connect gateway relay: %w", err)
	}
	// 心跳：gorilla 无内建 heartbeat，由 keepAlive goroutine 定期发 ping
	// 保持连接活性（Python aiohttp ws_connect(heartbeat=30.0) 内建）。
	go keepAlive(conn)
	p.conn = conn
	return nil
}

// Stop 关闭 WS 连接。
// Python: stop (hybrid.py:65-71)
func (p *WebSocketEventPublisher) Stop(_ context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn != nil {
		_ = p.conn.Close()
		p.conn = nil
	}
	return nil
}

// Publish 发布一条团队事件并等待 unary 应答。
// Python: publish (hybrid.py:96-129)
func (p *WebSocketEventPublisher) Publish(ctx context.Context, topicID string, message *atevents.EventMessage) error {
	request, err := p.buildRequest(topicID, message, requestID())
	if err != nil {
		return err
	}
	deadline := time.Duration(p.requestTimeout * float64(time.Second))

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn == nil {
		return fmt.Errorf("WebSocket event publisher is not started")
	}
	if err := p.conn.WriteJSON(request); err != nil {
		_ = p.conn.Close()
		p.conn = nil
		return fmt.Errorf("send publish request: %w", err)
	}
	return p.awaitAck(request["id"].(string), deadline)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildRequest 组装 team.mq.publish RPC 信封并校验 topic 归属。
// Python: _build_request (hybrid.py:73-93)
func (p *WebSocketEventPublisher) buildRequest(topicID string, message *atevents.EventMessage, requestID string) (map[string]any, error) {
	// Python: TeamTopic(topic_id.rsplit(":", 1)[-1]) 的枚举门卫
	lastSeg := topicID[strings.LastIndex(topicID, ":")+1:]
	topic := atevents.TeamTopic(lastSeg)
	switch topic {
	case atevents.TeamTopicTeam, atevents.TeamTopicTask, atevents.TeamTopicMessage:
	default:
		return nil, fmt.Errorf("unsupported team topic: %s", lastSeg)
	}
	// Python: topic_id != topic.build(session_id, team_name) → ValueError
	if expected := topic.Build(p.sessionID, p.teamName); topicID != expected {
		return nil, fmt.Errorf("team topic does not match the external client identity: %s", topicID)
	}
	event, err := atevents.ExternalTeamEventToWire(topic, message)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"type":   "req",
		"id":     requestID,
		"method": "team.mq.publish",
		"params": map[string]any{
			"session_id": p.sessionID,
			"payload":    event,
		},
		"is_stream": false,
	}, nil
}

// awaitAck 读取应答帧，跳过连接级 event 帧，校验 unary ack。
// Python: _validate_response + 接收循环（hybrid.py:96-127）
func (p *WebSocketEventPublisher) awaitAck(requestID string, deadline time.Duration) error {
	_ = p.conn.SetReadDeadline(time.Now().Add(deadline))
	defer func() { _ = p.conn.SetReadDeadline(time.Time{}) }()
	for {
		var data map[string]any
		if err := p.conn.ReadJSON(&data); err != nil {
			_ = p.conn.Close()
			p.conn = nil
			return fmt.Errorf("team event WebSocket closed before acknowledgement: %w", err)
		}
		if t, _ := data["type"].(string); t == "event" {
			continue
		}
		if id, _ := data["id"].(string); id != requestID {
			continue
		}
		// Python: type == "res" && ok is True → return；否则取 error
		if t, _ := data["type"].(string); t == "res" {
			if ok, _ := data["ok"].(bool); ok {
				return nil
			}
		}
		errText := ""
		if e, ok := data["error"]; ok && e != nil {
			errText = fmt.Sprintf("%v", e)
		} else if payload, ok := data["payload"].(map[string]any); ok {
			if e, ok := payload["error"]; ok && e != nil {
				errText = fmt.Sprintf("%v", e)
			}
		}
		if errText == "" {
			errText = "team event publish failed"
		}
		return fmt.Errorf("%s", errText)
	}
}

// keepAlive 以 ping 帧维持连接活性（gorilla 无内建 heartbeat）。
// Python 侧无对应实现：aiohttp ws_connect(heartbeat=30.0) 内建；
// Go 侧以独立 goroutine 补齐同等的保活语义。
func keepAlive(conn *websocket.Conn) {
	ticker := time.NewTicker(hybridHeartbeatInterval)
	defer ticker.Stop()
	for range ticker.C {
		if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(time.Second)); err != nil {
			return
		}
	}
}

// requestID 生成单次发布的请求标识。
// Python: f"team-event-{uuid.uuid4()}"（hybrid.py:98）
func requestID() string {
	return "team-event-" + strings.ReplaceAll(uuid.New().String(), "-", "")
}

// ──────────────────────────── HybridMessager ────────────────────────────

// HybridMessager 仅支持发布的 messager：外部进程经 Gateway 中继发布团队事件。
// subscribe/send 等入站面全部不支持（Python 同款 NotImplementedError）。
// Python: HybridMessager (hybrid.py:135-170)
type HybridMessager struct {
	// publisher 出站发布器
	publisher EventPublisher
	// senderID 发布时回填的发送者节点 ID
	senderID string
}

// EventPublisher 出站事件发布器抽象（协议接口）。
// Python: EventPublisher Protocol (hybrid.py:20-28)
type EventPublisher interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Publish(ctx context.Context, topicID string, message *atevents.EventMessage) error
}

// NewHybridMessager 构造 publish-only messager。
// Python: __init__ (hybrid.py:141-144)
func NewHybridMessager(publisher EventPublisher, senderID string) *HybridMessager {
	return &HybridMessager{publisher: publisher, senderID: senderID}
}

// Start 启动发布器。
// Python: start (hybrid.py:146-148)
func (m *HybridMessager) Start(ctx context.Context) error {
	return m.publisher.Start(ctx)
}

// Stop 停止发布器。
// Python: stop (hybrid.py:150-152)
func (m *HybridMessager) Stop(ctx context.Context) error {
	return m.publisher.Stop(ctx)
}

// Publish 发布事件；sender_id 缺失时回填 senderID。
// Python: publish (hybrid.py:154-159)
func (m *HybridMessager) Publish(ctx context.Context, topicID string, message *atevents.EventMessage) error {
	if message != nil && message.SenderID == "" {
		copied := *message
		copied.SenderID = m.senderID
		message = &copied
	}
	return m.publisher.Publish(ctx, topicID, message)
}

// Subscribe 不支持：hybrid messager 只发布。
// Python: subscribe → NotImplementedError (hybrid.py:161-162)
func (m *HybridMessager) Subscribe(_ context.Context, _ string, _ MessagerHandler) error {
	return errHybridPublishOnly("subscribe")
}

// Unsubscribe 不支持。
// Python: unsubscribe → NotImplementedError (hybrid.py:164-165)
func (m *HybridMessager) Unsubscribe(_ context.Context, _ string) error {
	return errHybridPublishOnly("unsubscribe")
}

// Send 不支持。
// Python: send → NotImplementedError (hybrid.py:167-168)
func (m *HybridMessager) Send(_ context.Context, _ string, _ *atevents.EventMessage) error {
	return errHybridPublishOnly("send")
}

// RegisterDirectMessageHandler 不支持。
// Python: register_direct_message_handler → NotImplementedError (hybrid.py:170-171)
func (m *HybridMessager) RegisterDirectMessageHandler(_ context.Context, _ MessagerHandler) error {
	return errHybridPublishOnly("register_direct_message_handler")
}

// UnregisterDirectMessageHandler 不支持。
// Python: unregister_direct_message_handler → NotImplementedError (hybrid.py:173-174)
func (m *HybridMessager) UnregisterDirectMessageHandler(_ context.Context) error {
	return errHybridPublishOnly("unregister_direct_message_handler")
}

// errHybridPublishOnly 构造 publish-only 拒绝错误。
// Python: NotImplementedError(f"HybridMessager only supports publishing")
func errHybridPublishOnly(op string) error {
	logger.Warn(hybridLogComponent).Str("op", op).Msg("HybridMessager only supports publishing")
	return fmt.Errorf("HybridMessager only supports publishing")
}

// 编译期检查：满足 Messager 接口。
var _ Messager = (*HybridMessager)(nil)
