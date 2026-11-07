package dashscope

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
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/openai"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

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
	apiBase = openai.NormalizeAPIBase(apiBase)
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
	client := &http.Client{Transport: model_clients.GetSharedTransport(verifySSL, sslCert)}

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

// ──────────────────────────── 非导出函数 ────────────────────────────
