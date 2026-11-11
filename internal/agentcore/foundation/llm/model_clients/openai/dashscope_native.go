package openai

// 本文件由 dashscope 包迁移而来：DashScope 原生 API 的响应类型与调用封装。
//
// 上游 DashScope 多模态三方法内联在 OpenAIModelClient，其底层调用
// dashscope SDK 的 MultiModalConversation.call / VideoSynthesis.call。
// Go 不引入 SDK，自实现 HTTP 调用（与原 dashscope 包一致）。
//
// Python: openai_model_client.py:2241-2437（generate_image/speech/video）

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────
// DashScopeResponse DashScope 原生 API 通用响应外层（图片/语音/视频共用）。
// DashScope 原生协议响应格式：
//
//	{"status_code": 200, "request_id": "xxx", "code": "", "message": "", "output": {...}, "usage": {...}}
//
// Python: dashscope SDK Response 对象的 status_code/code/message/output 属性
type DashScopeResponse struct {
	// StatusCode HTTP 状态码，200 表示成功
	StatusCode int `json:"status_code"`
	// RequestID 请求唯一标识
	RequestID string `json:"request_id"`
	// Code 错误码（成功时为空）
	Code string `json:"code,omitempty"`
	// Message 错误消息（成功时为空）
	Message string `json:"message,omitempty"`
	// Output 业务输出（不同接口结构不同，使用 RawMessage 延迟解析）
	Output json.RawMessage `json:"output,omitempty"`
	// Usage 用量信息（使用 RawMessage 延迟解析）
	Usage json.RawMessage `json:"usage,omitempty"`
}

// MultiModalOutput DashScope 多模态 API 输出（图片/语音共用）。
// Python: response.output（MultiModalConversation 响应）
type MultiModalOutput struct {
	// Choices 选项列表
	Choices []MultiModalChoice `json:"choices"`
}

// MultiModalChoice 多模态 API 响应选项。
type MultiModalChoice struct {
	// FinishReason 完成原因（"stop" 等）
	FinishReason string `json:"finish_reason"`
	// Message 消息内容
	Message *MultiModalMessage `json:"message"`
}

// MultiModalMessage 多模态 API 响应消息。
type MultiModalMessage struct {
	// Role 角色类型
	Role string `json:"role"`
	// Content 内容列表（文本/图片/音频混合）
	Content []ContentItem `json:"content"`
}

// ContentItem 多模态内容项（文本/图片/音频三选一）。
// DashScope 原生协议使用 {"text": "..."} / {"image": "url"} / {"audio": "url"} 格式。
type ContentItem struct {
	// Text 文本内容
	Text string `json:"text,omitempty"`
	// Image 图片 URL
	Image string `json:"image,omitempty"`
	// Audio 音频 URL 或 Base64 数据
	Audio string `json:"audio,omitempty"`
}

// VideoOutput DashScope 视频生成 API 输出。
// Python: response.output（VideoSynthesis 响应）
type VideoOutput struct {
	// VideoURL 生成的视频 URL
	VideoURL string `json:"video_url"`
}

// AudioOutput DashScope 语音生成 API 输出。
// Python: response.output.audio（Cosyvoice 语音合成响应）
type AudioOutput struct {
	// URL 音频文件 URL
	URL string `json:"url,omitempty"`
	// Base64 编码的音频数据
	Data string `json:"data,omitempty"`
}

// VideoUsage 视频生成用量信息。
type VideoUsage struct {
	// Duration 视频时长
	Duration any `json:"duration,omitempty"`
	// OutputVideoDuration 输出视频时长
	OutputVideoDuration any `json:"output_video_duration,omitempty"`
	// Size 视频分辨率
	Size string `json:"size,omitempty"`
}

// ──────────────────────────── 常量 ────────────────────────────

// DashScope 默认 API 域名
const (
	// compatibleModeMarker OpenAI 兼容模式路径标识
	compatibleModeMarker = "/compatible-mode/"
)

// DashScope 原生 API 路径
const (
	// multiModalPath 多模态生成 API 路径（图片/语音）
	multiModalPath = "/api/v1/services/aigc/multimodal-generation/generation"

	// videoSynthesisPath 视频生成 API 路径
	videoSynthesisPath = "/api/v1/services/aigc/video-generation/generation"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// DashScopeVoices DashScope Cosyvoice 语音合成支持的声音列表。
// Python: DASHSCOPE_VOICE 常量
var DashScopeVoices = []string{
	"Cherry", "Serena", "Ethan", "Chelsie", "Momo", "Vivian", "Moon", "Maia",
	"Kai", "Nofish", "Bella", "Jennifer", "Ryan", "Katerina", "Aiden",
	"Eldric Sage", "Mia", "Mochi", "Bellona", "Vincent", "Bunny", "Neil",
	"Elias", "Arthur", "Nini", "Ebona", "Seren", "Pip", "Stella", "Bodega",
	"Sonrisa", "Alek", "Dolce", "Sohee", "Ono Anna", "Lenn", "Emilien",
	"Andre", "Radio Gol", "Jada", "Dylan", "Li", "Marcus", "Roy", "Peter",
	"Sunny", "Eric", "Rocky", "Kiki",
}

// DashScopeLanguageTypes DashScope Cosyvoice 语音合成支持的语言类型。
// Python: DASHSCOPE_LANGUAGE_TYPE 常量
var DashScopeLanguageTypes = []string{
	"Chinese", "English", "German", "Italian", "Portuguese",
	"Spanish", "Japanese", "Korean", "French", "Russian",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveDashScopeBaseURL 从 api_base 推导 DashScope 原生 API 的 base URL。
// 推导规则：
//   - 如果 api_base 包含 "/compatible-mode/"，说明是 OpenAI 兼容模式 URL，
//     需要去掉兼容模式部分，得到原生 base URL
//   - 否则直接使用 api_base 作为原生 API 的 base
//
// 示例：
//
// URL 转换：
//
//	https://dashscope.aliyuncs.com/compatible-mode/v1 → https://dashscope.aliyuncs.com
//	https://dashscope.aliyuncs.com                    → https://dashscope.aliyuncs.com
//	https://custom-proxy.example.com                  → https://custom-proxy.example.com
//
// Python: dashscope.base_http_api_url = self.model_client_config.api_base
func ResolveDashScopeBaseURL(apiBase string) string {
	// 先归一（strip 空白/尾斜杠/已带 /chat/completions）——偏差 D-4：上游原生路径原样透传，
	// Go 多做一步，使本函数也能接受用户粘贴的完整端点 URL。
	apiBase = NormalizeAPIBase(apiBase)
	idx := strings.Index(apiBase, compatibleModeMarker)
	if idx == -1 {
		return strings.TrimRight(apiBase, "/")
	}
	// 去掉 /compatible-mode/ 及其后的路径部分
	return apiBase[:idx]
}

// CallDashScopeAPI 通用 DashScope 原生 API 调用。
// 统一封装认证、请求构建、响应解析、错误处理逻辑。
// 不依赖 DashScope Go SDK，自行实现 HTTP 调用（与 OpenAI 客户端风格一致）。
// Python: dashscope.MultiModalConversation.call() / dashscope.VideoSynthesis.call()
func CallDashScopeAPI(
	ctx context.Context,
	apiBase, apiKey, path string,
	reqBody map[string]any,
	timeout *float64,
	verifySSL bool,
	sslCert string,
	authMode llmschema.AuthMode,
) (*DashScopeResponse, error) {
	// 1. 推导原生 API base URL
	nativeBase := ResolveDashScopeBaseURL(apiBase)

	// 2. 构建完整 API URL
	apiURL := nativeBase + path

	// 3. 序列化请求体
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf("DashScope API 请求序列化失败: %s", err)),
		)
	}

	// 3.5 总超时挂 ctx（非流式语义）；必须在建请求之前派生，让 req 携带带超时的 ctx
	dashScopeTimeout := 120.0 // 多模态 API 默认 120 秒（比文本生成更长）
	if timeout != nil && *timeout > 0 {
		dashScopeTimeout = *timeout
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(dashScopeTimeout*float64(time.Second)))
	defer cancel()

	// 4. 创建 HTTP 请求
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf("DashScope API 创建请求失败: %s", err)),
		)
	}

	// 5. 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	// 6. 共享 Transport（C-A/C-14 收敛）
	client := &http.Client{Transport: model_clients.GetSharedTransport(authMode, apiKey, apiBase, verifySSL, sslCert)}

	// 7. 发送请求（对齐 Python: DashScope 多模态 API 使用 logger，非回调）
	logger.Info(logComponent).
		Str("model_provider", "DashScope").
		Str("api_url", apiURL).
		Msg("DashScope API request started.")

	resp, err := client.Do(req)
	if err != nil {
		return nil, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf("DashScope API 请求发送失败: %s", err)),
		)
	}
	defer func() { _ = resp.Body.Close() }()

	// 8. 读取响应体
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf("DashScope API 读取响应失败: %s", err)),
		)
	}

	// 9. 解析响应
	var dashResp DashScopeResponse
	if err := json.Unmarshal(respBody, &dashResp); err != nil {
		return nil, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf("DashScope API 响应解析失败: %s, body: %s", err, string(respBody))),
		)
	}

	// 10. 检查 DashScope 状态码
	if dashResp.StatusCode != 200 {
		errMsg := fmt.Sprintf(
			"DashScope API 调用失败. HTTP status: %d, Error code: %s, Error message: %s",
			dashResp.StatusCode, dashResp.Code, dashResp.Message,
		)
		logger.Error(logComponent).
			Str("model_provider", "DashScope").
			Int("status_code", dashResp.StatusCode).
			Str("code", dashResp.Code).
			Str("message", dashResp.Message).
			Msg("DashScope API call failed.")
		return nil, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(errMsg),
		)
	}

	logger.Info(logComponent).
		Str("model_provider", "DashScope").
		Int("status_code", dashResp.StatusCode).
		Msg("DashScope API response received.")

	return &dashResp, nil
}
