package external

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mem0HTTPClient Mem0 REST API 的 HTTP 客户端封装。
// 对齐 Python: _get_client() + _client_call() 调用的底层 HTTP 请求。
//
// Python 使用 mem0ai SDK（AsyncMemoryClient），Go 侧无官方 SDK，
// 因此自封装 HTTP 客户端直接调 REST API。
type mem0HTTPClient struct {
	// apiKey API 密钥
	apiKey string
	// baseURL API 基础地址
	baseURL string
	// httpClient HTTP 客户端
	httpClient *http.Client
}

// mem0SearchRequest search API 请求体
type mem0SearchRequest struct {
	Query   string         `json:"query"`
	Filters map[string]any `json:"filters,omitempty"`
	Rerank  bool           `json:"rerank,omitempty"`
	TopK    int            `json:"top_k,omitempty"`
}

// mem0AddRequest add API 请求体
type mem0AddRequest struct {
	Messages []mem0Message  `json:"messages"`
	Filters  map[string]any `json:"filters,omitempty"`
	Infer    *bool          `json:"infer,omitempty"` // pointer 区分零值和未设置
}

// mem0Message 消息格式
type mem0Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// mem0MemoryItem 记忆项
// 对齐 Python: _unwrap_results 中的 dict，含 memory 字段和可选 score
type mem0MemoryItem struct {
	Memory string  `json:"memory"`
	Score  float64 `json:"score,omitempty"`
}

// mem0SearchResponse search API 响应（dict 格式）
type mem0SearchResponse struct {
	Results []mem0MemoryItem `json:"results"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// mem0DefaultBaseURL Mem0 API 默认地址
	mem0DefaultBaseURL = "https://api.mem0.ai"
	// mem0HTTPTimeout HTTP 请求超时
	mem0HTTPTimeout = 60 * time.Second
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newMem0HTTPClient 创建 Mem0 HTTP 客户端。
// 对齐 Python: _get_client() 中 AsyncMemoryClient(api_key=...) 的初始化。
func newMem0HTTPClient(apiKey string, baseURL string) *mem0HTTPClient {
	if baseURL == "" {
		baseURL = mem0DefaultBaseURL
	}
	return &mem0HTTPClient{
		apiKey:  apiKey,
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: mem0HTTPTimeout,
		},
	}
}

// unwrapResults 统一解析 Mem0 API 响应，处理两种格式。
// 对齐 Python: _unwrap_results(response)
//   - dict 格式: {"results": [...]}
//   - list 格式: [...]
func unwrapResults(data []byte) ([]mem0MemoryItem, error) {
	// 先尝试 dict 格式
	var dictResp mem0SearchResponse
	if err := json.Unmarshal(data, &dictResp); err == nil && len(dictResp.Results) > 0 {
		return dictResp.Results, nil
	}
	// 再尝试 list 格式
	var listResp []mem0MemoryItem
	if err := json.Unmarshal(data, &listResp); err == nil {
		return listResp, nil
	}
	// 两种都不匹配，返回空
	return nil, nil
}

// doRequest 执行 HTTP 请求并返回响应体。
func (c *mem0HTTPClient) doRequest(ctx context.Context, method, path string, body any) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("序列化请求体失败: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Token "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("mem0 API 错误: status=%d body=%s", resp.StatusCode, string(respData))
	}

	return respData, nil
}

// search 执行语义搜索。
// 对齐 Python: _client_call("search", query=..., filters=..., rerank=..., top_k=...)
// REST: POST /v1/memories/search/
func (c *mem0HTTPClient) search(ctx context.Context, query string, filters map[string]any, rerank bool, topK int) ([]mem0MemoryItem, error) {
	req := mem0SearchRequest{
		Query:   query,
		Filters: filters,
		Rerank:  rerank,
		TopK:    topK,
	}
	data, err := c.doRequest(ctx, http.MethodPost, "/v1/memories/search/", req)
	if err != nil {
		return nil, err
	}
	items, err := unwrapResults(data)
	if err != nil {
		return nil, err
	}
	return items, nil
}

// getAll 获取全部记忆。
// 对齐 Python: _client_call("get_all", filters=...)
// REST: GET /v1/memories/
func (c *mem0HTTPClient) getAll(ctx context.Context, filters map[string]any) ([]mem0MemoryItem, error) {
	// 构建 query string
	path := "/v1/memories/"
	if len(filters) > 0 {
		params := make([]string, 0, len(filters))
		for k, v := range filters {
			params = append(params, fmt.Sprintf("%s=%v", k, v))
		}
		path += "?" + joinParams(params)
	}

	data, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	items, err := unwrapResults(data)
	if err != nil {
		return nil, err
	}
	return items, nil
}

// add 添加记忆。
// 对齐 Python: _client_call("add", messages, **filters) 和 _client_call("add", ..., infer=False)
// REST: POST /v1/memories/
func (c *mem0HTTPClient) add(ctx context.Context, messages []mem0Message, filters map[string]any, infer *bool) error {
	req := mem0AddRequest{
		Messages: messages,
		Filters:  filters,
		Infer:    infer,
	}
	_, err := c.doRequest(ctx, http.MethodPost, "/v1/memories/", req)
	return err
}

// joinParams 拼接 query 参数。
func joinParams(params []string) string {
	result := ""
	for i, p := range params {
		if i > 0 {
			result += "&"
		}
		result += p
	}
	return result
}
