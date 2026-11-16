package duplex

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// logComponent 本包日志组件。
// Python 侧无对应实现：Python 用模块级 logging.getLogger(__name__)。
const logComponent = logger.ComponentCommon

// ──────────────────────────── 导出函数 ────────────────────────────

// CheckSavedConnection 检查已保存的 provider 连通性（不采集麦克风或视频）。
// 对齐 connection_check.py:14-42 check_saved_connection：
// qwen_omni 分支——WS 连上游，等 session.created/updated 判定连通，
// error 事件报「provider 拒绝会话」；joyai 分支——GET /models 验证模型已列出。
// 整体 15 秒超时（asyncio.timeout(15)），子连接 10 秒（open_timeout=10）。
func CheckSavedConnection() error {
	done := make(chan error, 1)
	go func() { done <- checkSavedConnectionOnce() }()
	select {
	case err := <-done:
		return err
	case <-time.After(15 * time.Second):
		return errors.New("connection check timed out")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// checkSavedConnectionOnce 执行一次连通检查（15 秒总超时内的实际逻辑）。
// 对齐 connection_check.py:16-42 函数体。
func checkSavedConnectionOnce() error {
	values := SettingsPayload(true)["values"].(map[string]string)
	if values["video_live_provider"] == "qwen_omni" {
		config := QwenOmniRealtimeConfigFromEnvironment()
		if err := config.Validate(); err != nil {
			return err
		}
		return checkQwenSession(config)
	}
	base := strings.TrimRight(values["joyai_api_base"], "/")
	model := values["joyai_model"]
	key := strings.TrimSpace(os.Getenv("JOYAI_API_KEY"))
	if base == "" || model == "" || key == "" {
		return errors.New("Configure the JoyAI endpoint, model and API key first.")
	}
	return checkJoyaiModels(base, model, key)
}

// checkQwenSession 建立 WS 连接并等待会话初始化事件。
// 对齐 connection_check.py:20-26（websockets.connect + additional_headers）。
// 偏差 D-5：Python websockets 库 → gorilla。
func checkQwenSession(config QwenOmniRealtimeConfig) error {
	header := http.Header{"Authorization": []string{"Bearer " + config.APIKey}}
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.Dial(config.UpstreamWithModel(), header)
	if err != nil {
		return err
	}
	defer conn.Close()
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			logger.Warn(logComponent).Err(err).Str("provider", "qwen_omni").Msg("上游连接中断")
			return err
		}
		var event map[string]any
		if err := json.Unmarshal(data, &event); err != nil {
			logger.Warn(logComponent).Err(err).Str("provider", "qwen_omni").Msg("事件解析失败")
			return err
		}
		eventType, _ := event["type"].(string)
		if eventType == "session.created" || eventType == "session.updated" {
			return nil
		}
		if eventType == "error" {
			return errors.New("The provider rejected the session. Check the model and API key.")
		}
	}
}

// checkJoyaiModels 请求 /models 并确认模型已列出。
// 对齐 connection_check.py:33-42（httpx → net/http，偏差 D-3）。
func checkJoyaiModels(base, model, key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("provider", "joyai").Str("base", base).Msg("构造 /models 请求失败")
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("provider", "joyai").Str("base", base).Msg("/models 请求失败")
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logger.Error(logComponent).Str("provider", "joyai").Int("status", resp.StatusCode).Msg("/models 非成功状态码")
		return errors.New("joyai /models request failed with status " + http.StatusText(resp.StatusCode))
	}
	var body struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		logger.Error(logComponent).Err(err).Str("provider", "joyai").Msg("/models 响应解析失败")
		return err
	}
	for _, item := range body.Data {
		if id, _ := item["id"].(string); id == model {
			return nil
		}
	}
	return errors.New("The configured model is not listed by the provider.")
}
