package dashscope

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveDashScopeBaseURL 从 api_base 推导 DashScope 原生 API 的 base URL。
//
// 推导规则：
//   - 如果 api_base 包含 "/compatible-mode/"，说明是 OpenAI 兼容模式 URL，
//     需要去掉兼容模式部分，得到原生 base URL
//   - 否则直接使用 api_base 作为原生 API 的 base
//
// 示例：
//
//	https://dashscope.aliyuncs.com/compatible-mode/v1 → https://dashscope.aliyuncs.com
//	https://dashscope.aliyuncs.com                    → https://dashscope.aliyuncs.com
//	https://custom-proxy.example.com                  → https://custom-proxy.example.com
//
// 对应 Python: dashscope.base_http_api_url = self.model_client_config.api_base
func ResolveDashScopeBaseURL(apiBase string) string {
	idx := strings.Index(apiBase, compatibleModeMarker)
	if idx == -1 {
		return strings.TrimRight(apiBase, "/")
	}
	// 去掉 /compatible-mode/ 及其后的路径部分
	return apiBase[:idx]
}

// CallDashScopeAPI 通用 DashScope 原生 API 调用。
//
// 统一封装认证、请求构建、响应解析、错误处理逻辑。
// 不依赖 DashScope Go SDK，自行实现 HTTP 调用（与 OpenAI 客户端风格一致）。
//
// 对应 Python: dashscope.MultiModalConversation.call() / dashscope.VideoSynthesis.call()
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

	// 6. 构建 HTTP 客户端
	client, err := buildDashScopeHTTPClient(timeout, verifySSL, sslCert)
	if err != nil {
		return nil, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf("DashScope API 构建 HTTP 客户端失败: %s", err)),
		)
	}

	// 7. 发送请求
	callback.GetCallbackFramework().Trigger(ctx, &callback.LLMCallEventData{
		Event:         callback.LLMCallStarted,
		ModelProvider: "DashScope",
		Extra: map[string]any{
			"api_url": apiURL,
		},
	})

	resp, err := client.Do(req)
	if err != nil {
		return nil, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf("DashScope API 请求发送失败: %s", err)),
		)
	}
	defer resp.Body.Close()

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
		callback.GetCallbackFramework().Trigger(ctx, &callback.LLMCallEventData{
			Event:         callback.LLMCallError,
			ModelProvider: "DashScope",
			Error:         fmt.Errorf("%s", errMsg),
			Extra: map[string]any{
				"status_code": dashResp.StatusCode,
				"code":        dashResp.Code,
				"message":     dashResp.Message,
			},
		})
		return nil, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(errMsg),
		)
	}

	callback.GetCallbackFramework().Trigger(ctx, &callback.LLMCallEventData{
		Event:         callback.LLMResponseReceived,
		ModelProvider: "DashScope",
		Extra: map[string]any{
			"status_code": dashResp.StatusCode,
		},
	})

	return &dashResp, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildDashScopeHTTPClient 构建配置了 SSL/代理/超时的 HTTP 客户端。
//
// 逻辑与 openai 包的 buildHTTPClient 一致，但独立维护以避免跨包依赖内部函数。
func buildDashScopeHTTPClient(timeout *float64, verifySSL bool, sslCert string) (*http.Client, error) {
	transport := &http.Transport{}

	// SSL 配置
	if !verifySSL {
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	} else if sslCert != "" {
		caCert, err := os.ReadFile(sslCert)
		if err != nil {
			return nil, fmt.Errorf("读取 SSL 证书失败: %w", err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("解析 SSL 证书失败: %s", sslCert)
		}
		transport.TLSClientConfig = &tls.Config{
			RootCAs: caCertPool,
		}
	}

	// 代理配置：优先环境变量
	transport.Proxy = http.ProxyFromEnvironment

	// 构建超时时间
	clientTimeout := 120.0 // 多模态 API 默认 120 秒（比文本生成更长）
	if timeout != nil && *timeout > 0 {
		clientTimeout = *timeout
	}

	return &http.Client{
		Transport: transport,
		Timeout:   time.Duration(clientTimeout * float64(time.Second)),
	}, nil
}
