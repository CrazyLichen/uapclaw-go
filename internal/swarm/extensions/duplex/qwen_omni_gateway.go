package duplex

import (
	"errors"
	"net/url"
	"os"
	"strings"
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
