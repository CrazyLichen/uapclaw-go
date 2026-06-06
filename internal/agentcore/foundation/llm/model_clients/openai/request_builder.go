package openai

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// ──────────────────────────── 常量 ────────────────────────────

// chatCompletionsPath Chat Completion API 路径
const chatCompletionsPath = "/chat/completions"

// ──────────────────────────── 导出函数 ────────────────────────────

// AdjustParamsForOpenAI 对请求参数做 OpenAI 特有调整。
//
// 当 api_base 含 "openai.com" 时，temperature 和 top_p 互斥：
//   - temperature 优先级高于 top_p
//   - 如果同时存在，删除 top_p
//   - 如果只有 top_p，保留 top_p
//
// 对应 Python: OpenAIModelClient._build_request_params() 中的 OpenAI 特有逻辑
func AdjustParamsForOpenAI(params map[string]any, apiBase string) {
	apiBaseLower := strings.ToLower(apiBase)
	if !strings.Contains(apiBaseLower, "openai.com") {
		return
	}

	_, hasTemp := params["temperature"]
	_, hasTopP := params["top_p"]

	// temperature 和 top_p 同时存在时，删除 top_p
	if hasTemp && hasTopP {
		delete(params, "top_p")
	}
}

// BuildHTTPRequest 构建发送给 OpenAI API 的 HTTP 请求。
//
// 将请求参数序列化为 JSON，设置 Authorization 和 Content-Type 头，
// 处理 SSL 证书验证和代理配置。
func BuildHTTPRequest(
	ctx context.Context,
	apiBase, apiKey string,
	params map[string]any,
	headers map[string]string,
	timeout *float64,
	verifySSL bool,
	sslCert string,
) (*http.Request, *http.Client, error) {
	// 序列化请求体
	body, err := encodeJSON(params)
	if err != nil {
		return nil, nil, fmt.Errorf("序列化请求参数失败: %w", err)
	}

	// 构建 API URL
	apiURL := strings.TrimRight(apiBase, "/") + chatCompletionsPath

	// 创建 HTTP 请求
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("创建 HTTP 请求失败: %w", err)
	}

	// 设置必要请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	// 合并自定义请求头
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// 构建 HTTP 客户端
	client, err := buildHTTPClient(timeout, verifySSL, sslCert)
	if err != nil {
		return nil, nil, fmt.Errorf("构建 HTTP 客户端失败: %w", err)
	}

	return req, client, nil
}

// HandleExtraBody 将 params 中的 return_token_ids 移入 extra_body。
//
// OpenAI SDK 会丢弃未知的顶级参数，vLLM 需要 return_token_ids 在 JSON body 中，
// 因此将其移入 extra_body 字段。
//
// 对应 Python: OpenAIModelClient.invoke() 中的 return_token_ids 处理
func HandleExtraBody(params map[string]any) {
	tokenIDs, ok := params["return_token_ids"]
	if !ok {
		return
	}
	delete(params, "return_token_ids")

	// 获取或创建 extra_body
	extraBody, _ := params["extra_body"].(map[string]any)
	if extraBody == nil {
		extraBody = make(map[string]any)
	}
	extraBody["return_token_ids"] = tokenIDs
	params["extra_body"] = extraBody
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildHTTPClient 构建配置了 SSL/代理/超时的 HTTP 客户端。
func buildHTTPClient(timeout *float64, verifySSL bool, sslCert string) (*http.Client, error) {
	transport := &http.Transport{}

	// SSL 配置
	if !verifySSL {
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	} else if sslCert != "" {
		// 加载自定义 CA 证书
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
	clientTimeout := 60.0 // 默认 60 秒
	if timeout != nil && *timeout > 0 {
		clientTimeout = *timeout
	}

	return &http.Client{
		Transport: transport,
		Timeout:   time.Duration(clientTimeout * float64(time.Second)),
	}, nil
}

// encodeJSON 将 map 编码为 JSON 字节。
func encodeJSON(params map[string]any) ([]byte, error) {
	return json.Marshal(params)
}
