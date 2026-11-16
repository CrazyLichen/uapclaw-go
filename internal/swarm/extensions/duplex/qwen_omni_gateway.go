package duplex

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// QwenOmniProxyPath 阿里云 Qwen-Omni Realtime 的网关 WS 代理路径。
// 对齐 qwen_omni_gateway.py:23 QWEN_OMNI_PROXY_PATH。
const QwenOmniProxyPath = "/ws/video/qwen-omni"

// ──────────────────────────── 结构体 ────────────────────────────

// QwenOmniRealtimeConfig 实时上游连接配置（只读）。
// 对齐 qwen_omni_gateway.py:25-33 QwenOmniRealtimeConfig。
type QwenOmniRealtimeConfig struct {
	// UpstreamURL 上游 ws/wss 地址
	UpstreamURL string
	// APIKey 上游鉴权密钥
	APIKey string
	// Model 模型名
	Model string
	// Voice 音色
	Voice string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// QwenOmniRealtimeConfigFromEnvironment 从环境变量构建实时配置（voice 缺省 Tina）。
// 对齐 qwen_omni_gateway.py:35-44 from_environment。
func QwenOmniRealtimeConfigFromEnvironment() QwenOmniRealtimeConfig {
	voice := strings.TrimSpace(os.Getenv("QWEN_OMNI_VOICE"))
	if voice == "" {
		voice = "Tina"
	}
	return QwenOmniRealtimeConfig{
		UpstreamURL: strings.TrimSpace(os.Getenv("QWEN_OMNI_REALTIME_URL")),
		APIKey:      strings.TrimSpace(os.Getenv("QWEN_OMNI_API_KEY")),
		Model:       strings.TrimSpace(os.Getenv("QWEN_OMNI_MODEL_NAME")),
		Voice:       voice,
	}
}

// Validate 校验上游配置：URL 合法、无内嵌凭据、key/model 齐备。
// 对齐 qwen_omni_gateway.py:46-55 validate。
func (c QwenOmniRealtimeConfig) Validate() error {
	if c.UpstreamURL == "" {
		return errors.New("请配置 QWEN_OMNI_REALTIME_URL")
	}
	parsed, err := url.Parse(c.UpstreamURL)
	if err != nil {
		return errors.New("QWEN_OMNI_REALTIME_URL 必须是有效的 ws:// 或 wss:// 地址")
	}
	if (parsed.Scheme != "ws" && parsed.Scheme != "wss") || parsed.Host == "" {
		return errors.New("QWEN_OMNI_REALTIME_URL 必须是有效的 ws:// 或 wss:// 地址")
	}
	if parsed.User != nil {
		return errors.New("QWEN_OMNI_REALTIME_URL 不得包含用户名或密码")
	}
	if c.APIKey == "" {
		return errors.New("请配置 QWEN_OMNI_API_KEY")
	}
	if c.Model == "" {
		return errors.New("请配置 QWEN_OMNI_MODEL_NAME")
	}
	return nil
}

// UpstreamWithModel 校验后把 model 并入查询串返回完整上游地址。
// 对齐 qwen_omni_gateway.py:57-60 upstream_with_model（keep_blank_values 保留空参数）。
func (c QwenOmniRealtimeConfig) UpstreamWithModel() string {
	if err := c.Validate(); err != nil {
		return ""
	}
	parsed, _ := url.Parse(c.UpstreamURL)
	query := parsed.Query()
	query.Set("model", c.Model)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// ──────────────────────────── 导出函数（WS 中继段） ────────────────────────────

// ServeQwenOmniWebsocket 把一个浏览器会话中继到 Qwen-Omni，不暴露 API key。
// 对齐 qwen_omni_gateway.py:102-173 serve_qwen_omni_websocket。
// 偏差 D-5：FastAPI WebSocket/websockets → gorilla 双向中继；
// asyncio.wait FIRST_COMPLETED → 两个 goroutine + 任一结束即收拢（偏差 D-2）。
func ServeQwenOmniWebsocket(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	browserConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer browserConn.Close()
	browser := &safeConn{conn: browserConn}
	config := QwenOmniRealtimeConfigFromEnvironment()
	upstreamURL := config.UpstreamWithModel()
	if err := config.Validate(); err != nil {
		sendGatewayError(browser, "qwen_gateway_config_error", err.Error())
		closeWithReason(browser, 1008, "Qwen-Omni gateway is not configured")
		return
	}

	header := http.Header{"Authorization": []string{"Bearer " + config.APIKey}}
	dialer := websocket.Dialer{
		HandshakeTimeout:  15 * time.Second,
		ReadBufferSize:    8 * 1024 * 1024,
		WriteBufferSize:   8 * 1024 * 1024,
		EnableCompression: false,
	}
	upstreamRaw, resp, dialErr := dialer.Dial(upstreamURL, header)
	if dialErr != nil {
		logger.Warn(logComponent).Err(dialErr).Str("model", config.Model).Msg("Qwen-Omni Realtime relay failed")
		message := safeUpstreamError(dialErr, config.APIKey)
		sendGatewayError(browser, "qwen_gateway_upstream_error", message)
		closeWithReason(browser, 1011, "Qwen upstream connection failed")
		return
	}
	defer upstreamRaw.Close()
	upstream := &safeConn{conn: upstreamRaw}
	// ping/pong：gorilla 默认响应 ping；主动 ping 由读侧看门狗等价（ping_interval=20）
	logger.Info(logComponent).Str("model", config.Model).Msg("Qwen-Omni Realtime relay connected")

	closeCode, closeReason := 1000, ""
	relayDone := relayBoth(browser, upstream)
	relayErr := <-relayDone
	if relayErr != nil {
		if websocket.IsCloseError(relayErr, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
			// 对端正常关闭：读取 close 帧语义（ConnectionClosed.rcvd）
			closeCode, closeReason = closeFrameInfo(relayErr)
		} else if isBrowserDisconnect(relayErr) {
			// 浏览器侧断开：静默返回（对齐 WebSocketDisconnect 分支）
			return
		} else {
			closeCode, closeReason = 1011, "Upstream disconnected without a close frame"
		}
		message := safeUpstreamError(errors.New(fmt.Sprintf("Qwen Realtime 连接已关闭（%d）：%s", closeCode, orFallbackReason(closeReason))), config.APIKey)
		sendGatewayError(browser, "qwen_gateway_upstream_closed", message)
	}
	_ = resp
	// finally 段（对齐 :160-172）
	if closeReason != "" {
		closeReason = safeUpstreamError(errors.New(closeReason), config.APIKey)
	}
	logger.Info(logComponent).Int("code", closeCode).Str("reason", closeReason).Msg("Qwen-Omni relay closed")
	if closeCode == 1004 || closeCode == 1005 || closeCode == 1006 || closeCode == 1015 {
		closeReason = fmt.Sprintf("Upstream close %d: %s", closeCode, closeReason)
		closeCode = 1011
	}
	closeWithReason(browser, closeCode, truncateReason(closeReason))
}

// ──────────────────────────── 非导出函数（WS 中继段） ────────────────────────────

// sendGatewayError 向浏览器发送错误帧。
// 对齐 qwen_omni_gateway.py:61-71 _send_gateway_error（断连后静默返回）。
func sendGatewayError(conn *safeConn, code, message string) {
	payload, _ := json.Marshal(map[string]any{
		"type":  "error",
		"error": map[string]any{"code": code, "message": message},
	})
	if err := conn.writeMessage(websocket.TextMessage, payload); err != nil {
		logger.Debug(logComponent).Err(err).Msg("发送网关错误帧失败")
	}
}

// safeUpstreamError 错误文案脱密钥并截断。
// 对齐 qwen_omni_gateway.py:73-78 _safe_upstream_error。
func safeUpstreamError(err error, apiKey string) string {
	message := strings.TrimSpace(err.Error())
	if message == "" {
		message = "error"
	}
	if apiKey != "" {
		message = strings.ReplaceAll(message, apiKey, "******")
	}
	if len(message) > 1_000 {
		message = message[:1_000]
	}
	return message
}

// safeConn 带写锁的连接包装（gorilla 单写者约束）。
// Python 侧无对应实现：Python asyncio 单线程写天然串行，Go 双 goroutine 中继需显式写锁。
type safeConn struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

// writeMessage 串行写。
func (c *safeConn) writeMessage(messageType int, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.WriteMessage(messageType, data)
}

// writeControl 串行控制帧。
func (c *safeConn) writeControl(messageType int, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.WriteControl(messageType, data, time.Now().Add(time.Second))
}

// relayBoth 双向中继直到任一侧结束。
// 对齐 qwen_omni_gateway.py:80-100 两个 relay 协程。
func relayBoth(browser, upstream *safeConn) <-chan error {
	done := make(chan error, 2)
	// browser → upstream（对齐 _relay_browser_to_upstream）
	go func() {
		for {
			messageType, data, err := browser.conn.ReadMessage()
			if err != nil {
				done <- err
				return
			}
			if err := upstream.writeMessage(messageType, data); err != nil {
				done <- err
				return
			}
		}
	}()
	// upstream → browser（对齐 _relay_upstream_to_browser）
	go func() {
		for {
			messageType, data, err := upstream.conn.ReadMessage()
			if err != nil {
				done <- err
				return
			}
			if err := browser.writeMessage(messageType, data); err != nil {
				done <- err
				return
			}
		}
	}()
	return done
}

// isBrowserDisconnect 判断是否为浏览器侧断开（读端 1001/1000/AbnormalClosure）。
// 对齐 qwen_omni_gateway.py:152-154 WebSocketDisconnect 分支。
func isBrowserDisconnect(err error) bool {
	return websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure)
}

// closeFrameInfo 从 close 错误提取码与原因。
// 对齐 qwen_omni_gateway.py:147-150 ConnectionClosed.rcvd。
func closeFrameInfo(err error) (int, string) {
	if e, ok := err.(*websocket.CloseError); ok {
		return e.Code, e.Text
	}
	return 1011, "Upstream disconnected without a close frame"
}

// orFallbackReason 关闭原因缺省文案。
// 对齐 qwen_omni_gateway.py:150 close_reason or '服务端未提供具体原因'。
func orFallbackReason(reason string) string {
	if reason == "" {
		return "服务端未提供具体原因"
	}
	return reason
}

// closeWithReason 带原因关闭（123 字节上限内截断）。
// 对齐 qwen_omni_gateway.py:168-172（close_reason.encode()[:123]）。
func closeWithReason(conn *safeConn, code int, reason string) {
	reason = truncateReason(reason)
	_ = conn.writeControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(code, reason))
	_ = conn.conn.Close()
}

// truncateReason 关闭原因按 UTF-8 安全截断到 123 字节。
// 对齐 qwen_omni_gateway.py:169 close_reason.encode("utf-8")[:123].decode(errors="ignore")。
func truncateReason(reason string) string {
	data := []byte(reason)
	if len(data) <= 123 {
		return reason
	}
	cut := data[:123]
	for len(cut) > 0 && !utf8.Valid(cut) {
		cut = cut[:len(cut)-1]
	}
	return string(cut)
}
