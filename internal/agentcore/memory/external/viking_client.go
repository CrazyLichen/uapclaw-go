package external

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// vikingClient OpenViking REST API 的 HTTP 客户端封装。
// 对齐 Python: _VikingClient (openjiuwen/core/memory/external/openviking_memory_provider.py)
type vikingClient struct {
	// endpoint API 地址
	endpoint string
	// apiKey API 密钥
	apiKey string
	// account 账户标识
	account string
	// user 用户标识
	user string
	// agent Agent 标识
	agent string
	// httpClient HTTP 客户端
	httpClient *http.Client
}

// ──────────────────────────── 枚 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// vikingHTTPTimeout HTTP 请求超时
	// 对齐 Python: httpx.Client(timeout=30.0)
	vikingHTTPTimeout = 30 * time.Second
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newVikingClient 创建 OpenViking HTTP 客户端。
// 对齐 Python: _VikingClient.__init__(endpoint, api_key, account, user, agent)
func newVikingClient(endpoint, apiKey, account, user, agent string) *vikingClient {
	// 对齐 Python: endpoint.rstrip("/")
	endpoint = strings.TrimRight(endpoint, "/")
	return &vikingClient{
		endpoint: endpoint,
		apiKey:   apiKey,
		account:  account,
		user:     user,
		agent:    agent,
		httpClient: &http.Client{
			Timeout: vikingHTTPTimeout,
		},
	}
}

// health 检查 OpenViking 服务是否可达。
// 对齐 Python: _VikingClient.health() -> bool
func (c *vikingClient) health(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/health", nil)
	if err != nil {
		return false
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode == http.StatusOK
}

// post 发送 POST 请求并返回响应 dict。
// 对齐 Python: _VikingClient.post(path, body) -> dict
func (c *vikingClient) post(ctx context.Context, path string, body map[string]any) (map[string]any, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("序列化请求体失败: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	c.setHeaders(req)

	return c.doRequest(req)
}

// get 发送 GET 请求并返回响应 dict。
// 对齐 Python: _VikingClient.get(path, params) -> dict
func (c *vikingClient) get(ctx context.Context, path string, params map[string]string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+path, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	c.setHeaders(req)

	// 对齐 Python: httpx.Client.get(path, params=params)
	if len(params) > 0 {
		q := url.Values{}
		for k, v := range params {
			q.Set(k, v)
		}
		req.URL.RawQuery = q.Encode()
	}

	return c.doRequest(req)
}

// close 关闭 HTTP 客户端。
// 对齐 Python: _VikingClient.close()
func (c *vikingClient) close() {
	c.httpClient.CloseIdleConnections()
}

// setHeaders 设置请求 Header。
// 对齐 Python: _VikingClient.__init__ 中的 headers 构造
func (c *vikingClient) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OpenViking-Account", c.account)
	req.Header.Set("X-OpenViking-User", c.user)
	req.Header.Set("X-OpenViking-Agent", c.agent)
	// 对齐 Python: if api_key: headers["X-API-Key"] = api_key
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
}

// doRequest 执行 HTTP 请求并返回响应 dict。
// 对齐 Python: httpx response.raise_for_status() + response.json()
func (c *vikingClient) doRequest(req *http.Request) (map[string]any, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("发送请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	// 对齐 Python: r.raise_for_status()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("OpenViking API 错误: status=%d body=%s", resp.StatusCode, string(respData))
	}

	// 对齐 Python: r.json()
	var result map[string]any
	if err := json.Unmarshal(respData, &result); err != nil {
		return nil, fmt.Errorf("解析响应 JSON 失败: %w", err)
	}
	return result, nil
}
