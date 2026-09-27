# 7.14 Mem0Provider 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Mem0Provider，将 Mem0 云端 REST API 适配为 Go MemoryProvider 接口，对齐 Python `openjiuwen/core/memory/external/mem0_provider.py`

**Architecture:** 在 `internal/agentcore/memory/external/` 包下新增 `mem0_client.go`（自封装 HTTP 客户端）和 `mem0_provider.go`（MemoryProvider 实现）。Provider 内部自建熔断器（对齐 Python），ExternalMemoryRail 层熔断器作为外层防线。QueuePrefetch 用 goroutine + context cancel 对齐 Python asyncio.create_task。

**Tech Stack:** Go 1.22+, net/http, net/http/httptest (测试), encoding/json, sync, time, context

**Design Spec:** `docs/superpowers/specs/2027-11-25-mem0-provider-7.14-design.md`

---

### Task 1: mem0_client.go — HTTP 客户端结构体与构造函数

**Files:**
- Create: `internal/agentcore/memory/external/mem0_client.go`

- [ ] **Step 1: 创建 mem0_client.go 文件骨架**

```go
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
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/external/...`
Expected: 编译成功

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/memory/external/mem0_client.go
git commit -m "feat(7.14): 添加 mem0_client.go HTTP 客户端结构体与基础方法"
```

---

### Task 2: mem0_client.go — search/getAll/add 方法

**Files:**
- Modify: `internal/agentcore/memory/external/mem0_client.go`

- [ ] **Step 1: 在非导出函数区块添加 search/getAll/add 方法**

在 `mem0_client.go` 末尾（`doRequest` 之后）添加：

```go
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
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/external/...`
Expected: 编译成功

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/memory/external/mem0_client.go
git commit -m "feat(7.14): 添加 mem0_client search/getAll/add 方法"
```

---

### Task 3: mem0_client_test.go — HTTP 客户端测试

**Files:**
- Create: `internal/agentcore/memory/external/mem0_client_test.go`

- [ ] **Step 1: 编写 mem0_client_test.go**

```go
package external

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func newTestMem0Client(server *httptest.Server) *mem0HTTPClient {
	return &mem0HTTPClient{
		apiKey:     "test-api-key",
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func TestNewMem0HTTPClient_默认URL(t *testing.T) {
	client := newMem0HTTPClient("key", "")
	if client.baseURL != mem0DefaultBaseURL {
		t.Errorf("baseURL = %q, want %q", client.baseURL, mem0DefaultBaseURL)
	}
}

func TestNewMem0HTTPClient_自定义URL(t *testing.T) {
	client := newMem0HTTPClient("key", "https://custom.mem0.ai")
	if client.baseURL != "https://custom.mem0.ai" {
		t.Errorf("baseURL = %q, want custom URL", client.baseURL)
	}
}

func TestUnwrapResults_dict格式(t *testing.T) {
	data := []byte(`{"results": [{"memory": "偏好中文", "score": 0.9}]}`)
	items, err := unwrapResults(data)
	if err != nil {
		t.Fatalf("unwrapResults() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Memory != "偏好中文" {
		t.Errorf("Memory = %q, want %q", items[0].Memory, "偏好中文")
	}
	if items[0].Score != 0.9 {
		t.Errorf("Score = %f, want 0.9", items[0].Score)
	}
}

func TestUnwrapResults_list格式(t *testing.T) {
	data := []byte(`[{"memory": "偏好英文", "score": 0.8}]`)
	items, err := unwrapResults(data)
	if err != nil {
		t.Fatalf("unwrapResults() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Memory != "偏好英文" {
		t.Errorf("Memory = %q, want %q", items[0].Memory, "偏好英文")
	}
}

func TestUnwrapResults_空结果(t *testing.T) {
	data := []byte(`[]`)
	items, err := unwrapResults(data)
	if err != nil {
		t.Fatalf("unwrapResults() error = %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len(items) = %d, want 0", len(items))
	}
}

func TestUnwrapResults_dict空结果(t *testing.T) {
	data := []byte(`{"results": []}`)
	items, err := unwrapResults(data)
	if err != nil {
		t.Fatalf("unwrapResults() error = %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len(items) = %d, want 0", len(items))
	}
}

func TestMem0HTTPClient_Search(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/v1/memories/search/" {
			t.Errorf("Path = %q, want /v1/memories/search/", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Token test-api-key" {
			t.Errorf("Authorization = %q, want 'Token test-api-key'", r.Header.Get("Authorization"))
		}

		var req mem0SearchRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Query != "测试查询" {
			t.Errorf("Query = %q, want %q", req.Query, "测试查询")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"memory": "用户偏好Go语言", "score": 0.95},
				{"memory": "用户使用VSCode", "score": 0.8},
			},
		})
	}))
	defer server.Close()

	client := newTestMem0Client(server)
	items, err := client.search(context.Background(), "测试查询", map[string]any{"user_id": "u1"}, true, 10)
	if err != nil {
		t.Fatalf("search() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if items[0].Memory != "用户偏好Go语言" {
		t.Errorf("Memory = %q, want %q", items[0].Memory, "用户偏好Go语言")
	}
}

func TestMem0HTTPClient_Search_空结果(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]any{})
	}))
	defer server.Close()

	client := newTestMem0Client(server)
	items, err := client.search(context.Background(), "不存在", nil, false, 5)
	if err != nil {
		t.Fatalf("search() error = %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len(items) = %d, want 0", len(items))
	}
}

func TestMem0HTTPClient_GetAll(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Method = %q, want GET", r.Method)
		}
		if r.URL.Path != "/v1/memories/" {
			t.Errorf("Path = %q, want /v1/memories/", r.URL.Path)
		}
		uid := r.URL.Query().Get("user_id")
		if uid != "u1" {
			t.Errorf("user_id = %q, want %q", uid, "u1")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"memory": "偏好暗色主题"},
			{"memory": "使用zsh"},
		})
	}))
	defer server.Close()

	client := newTestMem0Client(server)
	items, err := client.getAll(context.Background(), map[string]any{"user_id": "u1"})
	if err != nil {
		t.Fatalf("getAll() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
}

func TestMem0HTTPClient_GetAll_带agentID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid := r.URL.Query().Get("user_id")
		aid := r.URL.Query().Get("agent_id")
		if uid != "u1" {
			t.Errorf("user_id = %q, want %q", uid, "u1")
		}
		if aid != "a1" {
			t.Errorf("agent_id = %q, want %q", aid, "a1")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]any{})
	}))
	defer server.Close()

	client := newTestMem0Client(server)
	items, err := client.getAll(context.Background(), map[string]any{"user_id": "u1", "agent_id": "a1"})
	if err != nil {
		t.Fatalf("getAll() error = %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len(items) = %d, want 0", len(items))
	}
}

func TestMem0HTTPClient_Add(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/v1/memories/" {
			t.Errorf("Path = %q, want /v1/memories/", r.URL.Path)
		}

		var req mem0AddRequest
		json.NewDecoder(r.Body).Decode(&req)
		if len(req.Messages) != 2 {
			t.Errorf("len(Messages) = %d, want 2", len(req.Messages))
		}
		if req.Messages[0].Role != "user" {
			t.Errorf("Messages[0].Role = %q, want %q", req.Messages[0].Role, "user")
		}
		if req.Infer != nil {
			t.Errorf("Infer = %v, want nil (未设置)", *req.Infer)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"id": "mem-123"}
	}))
	defer server.Close()

	client := newTestMem0Client(server)
	msgs := []mem0Message{
		{Role: "user", Content: "你好"},
		{Role: "assistant", Content: "你好！"},
	}
	err := client.add(context.Background(), msgs, map[string]any{"user_id": "u1", "agent_id": "a1"}, nil)
	if err != nil {
		t.Fatalf("add() error = %v", err)
	}
}

func TestMem0HTTPClient_Add_Conclude(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req mem0AddRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Infer == nil || *req.Infer != false {
			t.Errorf("Infer = %v, want false", req.Infer)
		}
		if len(req.Messages) != 1 || req.Messages[0].Role != "user" {
			t.Errorf("Messages = %v, want single user message", req.Messages)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"id": "mem-456"}
	}))
	defer server.Close()

	client := newTestMem0Client(server)
	inferFalse := false
	msgs := []mem0Message{{Role: "user", Content: "我喜欢Go语言"}}
	err := client.add(context.Background(), msgs, map[string]any{"user_id": "u1", "agent_id": "a1"}, &inferFalse)
	if err != nil {
		t.Fatalf("add() error = %v", err)
	}
}

func TestMem0HTTPClient_认证失败(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"detail": "Invalid API key"})
	}))
	defer server.Close()

	client := newTestMem0Client(server)
	_, err := client.search(context.Background(), "test", nil, false, 5)
	if err == nil {
		t.Fatal("search() 应返回认证错误")
	}
}

func TestMem0HTTPClient_服务器错误(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	client := newTestMem0Client(server)
	_, err := client.search(context.Background(), "test", nil, false, 5)
	if err == nil {
		t.Fatal("search() 应返回服务器错误")
	}
}

func TestMem0HTTPClient_请求超时(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(3 * time.Second)
	}))
	defer server.Close()

	// 用短超时的 context
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	client := newTestMem0Client(server)
	_, err := client.search(ctx, "test", nil, false, 5)
	if err == nil {
		t.Fatal("search() 应返回超时错误")
	}
}
```

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test -v -count=1 ./internal/agentcore/memory/external/... -run TestNewMem0HTTPClient|TestUnwrapResults|TestMem0HTTPClient`
Expected: 全部 PASS

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/memory/external/mem0_client_test.go
git commit -m "test(7.14): 添加 mem0_client HTTP 客户端单元测试"
```

---

### Task 4: mem0_provider.go — 结构体、构造函数、Schema 常量

**Files:**
- Create: `internal/agentcore/memory/external/mem0_provider.go`

- [ ] **Step 1: 创建 mem0_provider.go 骨架**

```go
package external

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Mem0Provider Mem0 外部记忆提供者。
// 对齐 Python: Mem0MemoryProvider (openjiuwen/core/memory/external/mem0_provider.py)
//
// 将 Mem0 云端 REST API 适配为 MemoryProvider 接口。
// 内置熔断器（对齐 Python _BREAKER_THRESHOLD / _BREAKER_COOLDOWN_SECS），
// ExternalMemoryRail 层熔断器作为外层防线。
type Mem0Provider struct {
	BaseMemoryProvider

	// apiKey API 密钥
	apiKey string
	// userID 用户标识
	userID string
	// agentID Agent 标识
	agentID string
	// rerank 是否启用重排序
	rerank bool

	// client Mem0 HTTP 客户端（延迟初始化，对齐 Python _get_client 懒加载）
	client *mem0HTTPClient
	// initialized 是否已初始化
	initialized bool
	// mu 保护 client 延迟初始化
	mu sync.Mutex

	// consecutiveFailures 连续失败次数
	// 对齐 Python: _consecutive_failures
	consecutiveFailures int
	// breakerOpenUntil 熔断器冷却截止时间
	// 对齐 Python: _breaker_open_until
	breakerOpenUntil time.Time

	// prefetchCancel 取消后台 prefetch goroutine
	// 对齐 Python: _prefetch_task (asyncio.Task)
	prefetchCancel context.CancelFunc
	// prefetchWg 等待后台 prefetch goroutine 退出
	prefetchWg sync.WaitGroup
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// mem0BreakerThreshold 熔断器连续失败阈值
	// 对齐 Python: _BREAKER_THRESHOLD = 5
	mem0BreakerThreshold = 5
	// mem0BreakerCooldownSecs 熔断器冷却时间（秒）
	// 对齐 Python: _BREAKER_COOLDOWN_SECS = 120.0
	mem0BreakerCooldownSecs = 120.0
	// mem0ShutdownWaitTimeout Shutdown 等待 prefetch goroutine 退出的超时
	// 对齐 Python: asyncio.wait_for(..., timeout=2.0)
	mem0ShutdownWaitTimeout = 2 * time.Second
)

var mem0LogComponent = logger.ComponentAgentCore

// mem0ProfileSchema mem0_profile 工具 Schema
// 对齐 Python: PROFILE_SCHEMA
var mem0ProfileSchema = ToolSchema{
	Name: "mem0_profile",
	Description: "Retrieve all stored memories about the user — preferences, facts, " +
		"project context. Fast, no reranking. Use at conversation start.",
	Parameters: map[string]any{
		"type":       "object",
		"properties": map[string]any{},
		"required":   []string{},
	},
}

// mem0SearchSchema mem0_search 工具 Schema
// 对齐 Python: SEARCH_SCHEMA
var mem0SearchSchema = ToolSchema{
	Name: "mem0_search",
	Description: "Search memories by meaning. Returns relevant facts ranked by similarity. " +
		"Set rerank=true for higher accuracy on important queries.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query":  map[string]any{"type": "string", "description": "What to search for."},
			"rerank": map[string]any{"type": "boolean", "description": "Enable reranking for precision (default: false)."},
			"top_k":  map[string]any{"type": "integer", "description": "Max results (default: 10, max: 50)."},
		},
		"required": []string{"query"},
	},
}

// mem0ConcludeSchema mem0_conclude 工具 Schema
// 对齐 Python: CONCLUDE_SCHEMA
var mem0ConcludeSchema = ToolSchema{
	Name: "mem0_conclude",
	Description: "Store a durable fact about the user. Stored verbatim (no LLM extraction). " +
		"Use for explicit preferences, corrections, or decisions.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"conclusion": map[string]any{"type": "string", "description": "The fact to store."},
		},
		"required": []string{"conclusion"},
	},
}

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMem0Provider 创建 Mem0Provider 实例。
// 对齐 Python: Mem0MemoryProvider.__init__(api_key=..., user_id=..., agent_id=..., rerank=...)
func NewMem0Provider(apiKey, userID, agentID string, rerank bool) *Mem0Provider {
	return &Mem0Provider{
		apiKey:   apiKey,
		userID:   userID,
		agentID:  agentID,
		rerank:   rerank,
	}
}

// Name 返回 Provider 唯一名称。
// Python: @property name -> "mem0"
func (p *Mem0Provider) Name() string {
	return "mem0"
}

// IsAvailable 检查 Provider 是否已配置且就绪。
// Python: is_available() -> bool(self._api_key)
func (p *Mem0Provider) IsAvailable() bool {
	return p.apiKey != ""
}

// IsInitialized 返回 Provider 是否已初始化。
// Python: @property is_initialized -> self._initialized
// 覆盖 BaseMemoryProvider 的默认值（false）。
func (p *Mem0Provider) IsInitialized() bool {
	return p.initialized
}

// GetToolSchemas 返回 Provider 提供的工具 Schema 列表。
// Python: get_tool_schemas() -> [PROFILE_SCHEMA, SEARCH_SCHEMA, CONCLUDE_SCHEMA]
func (p *Mem0Provider) GetToolSchemas() []ToolSchema {
	return []ToolSchema{mem0ProfileSchema, mem0SearchSchema, mem0ConcludeSchema}
}

// SystemPromptBlock 返回 Provider 的系统提示词引导块。
// Python: system_prompt_block() -> "# Mem0 Memory\nActive. User: {self._user_id}.\n..."
func (p *Mem0Provider) SystemPromptBlock() string {
	return "# Mem0 Memory\n" +
		fmt.Sprintf("Active. User: %s.\n", p.userID) +
		"Use mem0_search to find memories, mem0_conclude to store facts, " +
		"mem0_profile for a full overview."
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getClient 延迟初始化并返回 Mem0 HTTP 客户端。
// 对齐 Python: _get_client() — 懒加载模式
func (p *Mem0Provider) getClient() *mem0HTTPClient {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.client != nil {
		return p.client
	}
	p.client = newMem0HTTPClient(p.apiKey, "")
	return p.client
}

// readFilters 构造读操作的 filters。
// 对齐 Python: _read_filters() -> {"user_id": ..., "agent_id": ...(可选)}
func (p *Mem0Provider) readFilters() map[string]any {
	filters := map[string]any{"user_id": p.userID}
	if p.agentID != "" {
		filters["agent_id"] = p.agentID
	}
	return filters
}

// writeFilters 构造写操作的 filters。
// 对齐 Python: _write_filters() -> {"user_id": ..., "agent_id": ...}
func (p *Mem0Provider) writeFilters() map[string]any {
	return map[string]any{
		"user_id":  p.userID,
		"agent_id": p.agentID,
	}
}

// isBreakerOpen 检查熔断器是否处于开启状态。
// 对齐 Python: _is_breaker_open()
func (p *Mem0Provider) isBreakerOpen() bool {
	if p.consecutiveFailures < mem0BreakerThreshold {
		return false
	}
	if time.Now().After(p.breakerOpenUntil) {
		p.consecutiveFailures = 0
		return false
	}
	return true
}

// recordSuccess 记录成功，重置连续失败计数。
// 对齐 Python: _record_success()
func (p *Mem0Provider) recordSuccess() {
	p.consecutiveFailures = 0
}

// recordFailure 记录失败，递增连续失败计数，达到阈值时开启熔断器。
// 对齐 Python: _record_failure()
func (p *Mem0Provider) recordFailure() {
	p.consecutiveFailures++
	if p.consecutiveFailures >= mem0BreakerThreshold {
		p.breakerOpenUntil = time.Now().Add(time.Duration(mem0BreakerCooldownSecs * float64(time.Second)))
		logger.Warn(mem0LogComponent).
			Int("consecutive_failures", p.consecutiveFailures).
			Float64("cooldown_secs", mem0BreakerCooldownSecs).
			Msg("[Mem0Provider] 熔断器开启")
	}
}
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/external/...`
Expected: 编译成功

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/memory/external/mem0_provider.go
git commit -m "feat(7.14): 添加 Mem0Provider 结构体、构造函数、Schema 常量和熔断器"
```

---

### Task 5: mem0_provider.go — Initialize / Prefetch / QueuePrefetch

**Files:**
- Modify: `internal/agentcore/memory/external/mem0_provider.go`

- [ ] **Step 1: 在导出函数区块添加 Initialize / Prefetch / QueuePrefetch 方法**

在 `SystemPromptBlock()` 方法之后添加：

```go
// Initialize 初始化 Provider。
// 对齐 Python: async def initialize(self, **kwargs) -> None
func (p *Mem0Provider) Initialize(_ context.Context, opts ...ProviderOption) error {
	po := applyOptions(opts...)

	// 对齐 Python: kwargs.get("api_key") or self._api_key
	if po.UserID != "" {
		p.userID = po.UserID
	}
	if po.ScopeID != "" {
		p.agentID = po.ScopeID // ScopeID 对应 Python 的 agent_id
	}

	if p.apiKey == "" {
		return fmt.Errorf("Mem0 API key is required. Provide api_key in provider initialization")
	}

	p.mu.Lock()
	p.client = newMem0HTTPClient(p.apiKey, "")
	p.initialized = true
	p.mu.Unlock()

	return nil
}

// Prefetch 根据查询预取记忆上下文。
// 对齐 Python: async def prefetch(self, query, **kwargs) -> str
func (p *Mem0Provider) Prefetch(ctx context.Context, query string, opts ...ProviderOption) (string, error) {
	if query == "" || p.isBreakerOpen() {
		return "", nil
	}

	topK := 5
	rerank := p.rerank

	client := p.getClient()
	response, err := client.search(ctx, query, p.readFilters(), rerank, topK)
	if err != nil {
		p.recordFailure()
		logger.Debug(mem0LogComponent).Err(err).Msg("Mem0 prefetch 失败")
		return "", nil
	}

	if len(response) == 0 {
		p.recordSuccess()
		return "", nil
	}

	lines := make([]string, 0, len(response))
	for _, item := range response {
		if item.Memory != "" {
			lines = append(lines, "- "+item.Memory)
		}
	}

	p.recordSuccess()
	if len(lines) == 0 {
		return "", nil
	}
	result := "## Mem0 Memory\n" + joinLines(lines)
	return result, nil
}

// QueuePrefetch 后台预热查询。
// 对齐 Python: async def queue_prefetch(self, query, **kwargs) -> None
// 使用 goroutine + context cancel 对齐 Python asyncio.create_task
func (p *Mem0Provider) QueuePrefetch(ctx context.Context, query string, opts ...ProviderOption) {
	if query == "" || p.isBreakerOpen() {
		return
	}

	// 取消上一次 prefetch goroutine
	if p.prefetchCancel != nil {
		p.prefetchCancel()
	}

	topK := 5
	ctx, cancel := context.WithCancel(ctx)
	p.prefetchCancel = cancel

	p.prefetchWg.Add(1)
	go func() {
		defer p.prefetchWg.Done()
		client := p.getClient()
		_, err := client.search(ctx, query, p.readFilters(), p.rerank, topK)
		if err != nil {
			// context.Canceled 是正常取消，不算失败
			if ctx.Err() != nil {
				return
			}
			p.recordFailure()
			logger.Debug(mem0LogComponent).Err(err).Msg("Mem0 queue_prefetch 失败")
			return
		}
		p.recordSuccess()
	}()
}

// joinLines 拼接多行文本。
func joinLines(lines []string) string {
	result := ""
	for i, line := range lines {
		if i > 0 {
			result += "\n"
		}
		result += line
	}
	return result
}
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/external/...`
Expected: 编译成功

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/memory/external/mem0_provider.go
git commit -m "feat(7.14): 添加 Mem0Provider Initialize/Prefetch/QueuePrefetch 方法"
```

---

### Task 6: mem0_provider.go — SyncTurn / HandleToolCall / Shutdown

**Files:**
- Modify: `internal/agentcore/memory/external/mem0_provider.go`

- [ ] **Step 1: 在导出函数区块添加 SyncTurn / HandleToolCall / Shutdown**

在 `QueuePrefetch()` 方法之后添加：

```go
// SyncTurn 同步一轮对话到外部记忆。
// 对齐 Python: async def sync_turn(self, user_msg, assistant_msg, **kwargs) -> None
func (p *Mem0Provider) SyncTurn(ctx context.Context, userMsg, assistantMsg string, _ ...ProviderOption) error {
	if p.isBreakerOpen() || userMsg == "" || assistantMsg == "" {
		return nil
	}

	messages := []mem0Message{
		{Role: "user", Content: userMsg},
		{Role: "assistant", Content: assistantMsg},
	}

	client := p.getClient()
	err := client.add(ctx, messages, p.writeFilters(), nil)
	if err != nil {
		p.recordFailure()
		logger.Warn(mem0LogComponent).Err(err).Msg("Mem0 sync_turn 失败")
		return err
	}

	p.recordSuccess()
	return nil
}

// HandleToolCall 处理工具调用并返回结果字符串。
// 对齐 Python: async def handle_tool_call(self, tool_name, args) -> str
func (p *Mem0Provider) HandleToolCall(ctx context.Context, toolName string, args map[string]any) (string, error) {
	if p.isBreakerOpen() {
		return json.Marshal(map[string]string{
			"error": "Mem0 API temporarily unavailable (multiple consecutive failures). Will retry automatically.",
		})
	}

	client := p.getClient()

	switch toolName {
	case "mem0_profile":
		return p.handleProfile(ctx, client)
	case "mem0_search":
		return p.handleSearch(ctx, client, args)
	case "mem0_conclude":
		return p.handleConclude(ctx, client, args)
	default:
		return json.Marshal(map[string]string{"error": fmt.Sprintf("Unknown tool: %s", toolName)})
	}
}

// handleProfile 处理 mem0_profile 工具调用。
// 对齐 Python: handle_tool_call 中 tool_name == "mem0_profile" 分支
func (p *Mem0Provider) handleProfile(ctx context.Context, client *mem0HTTPClient) (string, error) {
	response, err := client.getAll(ctx, p.readFilters())
	if err != nil {
		p.recordFailure()
		return json.Marshal(map[string]string{"error": err.Error()})
	}

	p.recordSuccess()

	if len(response) == 0 {
		return json.Marshal(map[string]string{"result": "No memories stored yet."})
	}

	lines := make([]string, 0, len(response))
	for _, item := range response {
		if item.Memory != "" {
			lines = append(lines, item.Memory)
		}
	}

	return json.Marshal(map[string]any{
		"result": joinLines(lines),
		"count":  len(lines),
	})
}

// handleSearch 处理 mem0_search 工具调用。
// 对齐 Python: handle_tool_call 中 tool_name == "mem0_search" 分支
func (p *Mem0Provider) handleSearch(ctx context.Context, client *mem0HTTPClient, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return json.Marshal(map[string]string{"error": "Missing required parameter: query"})
	}

	topK := 10
	if v, ok := args["top_k"]; ok {
		switch n := v.(type) {
		case float64:
			topK = int(n)
		case int:
			topK = n
		}
	}
	if topK > 50 {
		topK = 50
	}

	rerank := false
	if v, ok := args["rerank"]; ok {
		rerank, _ = v.(bool)
	}

	response, err := client.search(ctx, query, p.readFilters(), rerank, topK)
	if err != nil {
		p.recordFailure()
		return json.Marshal(map[string]string{"error": err.Error()})
	}

	p.recordSuccess()

	if len(response) == 0 {
		return json.Marshal(map[string]string{"result": "No relevant memories found."})
	}

	payload := make([]map[string]any, 0, len(response))
	for _, item := range response {
		payload = append(payload, map[string]any{
			"memory": item.Memory,
			"score":  item.Score,
		})
	}

	return json.Marshal(map[string]any{
		"results": payload,
		"count":   len(payload),
	})
}

// handleConclude 处理 mem0_conclude 工具调用。
// 对齐 Python: handle_tool_call 中 tool_name == "mem0_conclude" 分支
func (p *Mem0Provider) handleConclude(ctx context.Context, client *mem0HTTPClient, args map[string]any) (string, error) {
	conclusion, _ := args["conclusion"].(string)
	if conclusion == "" {
		return json.Marshal(map[string]string{"error": "Missing required parameter: conclusion"})
	}

	inferFalse := false
	messages := []mem0Message{{Role: "user", Content: conclusion}}
	err := client.add(ctx, messages, p.writeFilters(), &inferFalse)
	if err != nil {
		p.recordFailure()
		return json.Marshal(map[string]string{"error": err.Error()})
	}

	p.recordSuccess()
	return json.Marshal(map[string]string{"result": "Fact stored."})
}

// Shutdown 关闭 Provider 释放资源。
// 对齐 Python: async def shutdown(self) -> None
func (p *Mem0Provider) Shutdown(_ context.Context) error {
	// 取消 prefetch goroutine，对齐 Python: self._prefetch_task.cancel()
	if p.prefetchCancel != nil {
		p.prefetchCancel()
	}

	// 等待 goroutine 退出，带超时，对齐 Python: asyncio.wait_for(..., timeout=2.0)
	done := make(chan struct{})
	go func() {
		p.prefetchWg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(mem0ShutdownWaitTimeout):
		// 超时后继续清理，对齐 Python: except TimeoutError -> pass
	}

	p.prefetchCancel = nil
	p.mu.Lock()
	p.client = nil
	p.initialized = false
	p.mu.Unlock()

	return nil
}
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/external/...`
Expected: 编译成功

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/memory/external/mem0_provider.go
git commit -m "feat(7.14): 添加 Mem0Provider SyncTurn/HandleToolCall/Shutdown 方法"
```

---

### Task 7: mem0_provider_test.go — Provider 测试

**Files:**
- Create: `internal/agentcore/memory/external/mem0_provider_test.go`

- [ ] **Step 1: 编写 mem0_provider_test.go**

```go
package external

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestMem0Provider 创建使用 mock server 的 Mem0Provider
func newTestMem0Provider(server *httptest.Server) *Mem0Provider {
	p := NewMem0Provider("test-key", "user-1", "agent-1", false)
	// 替换 client 指向 mock server
	p.mu.Lock()
	p.client = newMem0HTTPClient("test-key", server.URL)
	p.initialized = true
	p.mu.Unlock()
	return p
}

func TestMem0Provider_Name(t *testing.T) {
	p := NewMem0Provider("key", "u1", "a1", false)
	if p.Name() != "mem0" {
		t.Errorf("Name() = %q, want %q", p.Name(), "mem0")
	}
}

func TestMem0Provider_IsAvailable(t *testing.T) {
	p1 := NewMem0Provider("key", "u1", "", false)
	if !p1.IsAvailable() {
		t.Error("IsAvailable() = false, want true (有 apiKey)")
	}
	p2 := NewMem0Provider("", "u1", "", false)
	if p2.IsAvailable() {
		t.Error("IsAvailable() = true, want false (无 apiKey)")
	}
}

func TestMem0Provider_Initialize(t *testing.T) {
	p := NewMem0Provider("key", "", "", false)
	err := p.Initialize(context.Background())
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if !p.IsInitialized() {
		t.Error("IsInitialized() = false, want true")
	}
	if p.client == nil {
		t.Error("client 应已初始化")
	}
}

func TestMem0Provider_Initialize_覆盖参数(t *testing.T) {
	p := NewMem0Provider("key", "old-user", "old-agent", false)
	err := p.Initialize(context.Background(),
		WithUserID("new-user"),
		WithScopeID("new-agent"),
	)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if p.userID != "new-user" {
		t.Errorf("userID = %q, want %q", p.userID, "new-user")
	}
	if p.agentID != "new-agent" {
		t.Errorf("agentID = %q, want %q", p.agentID, "new-agent")
	}
}

func TestMem0Provider_Initialize_apiKey缺失(t *testing.T) {
	p := NewMem0Provider("", "", "", false)
	err := p.Initialize(context.Background())
	if err == nil {
		t.Fatal("Initialize() 应返回错误（apiKey 为空）")
	}
}

func TestMem0Provider_GetToolSchemas(t *testing.T) {
	p := NewMem0Provider("key", "u1", "", false)
	schemas := p.GetToolSchemas()
	if len(schemas) != 3 {
		t.Fatalf("len(GetToolSchemas()) = %d, want 3", len(schemas))
	}
	names := []string{schemas[0].Name, schemas[1].Name, schemas[2].Name}
	expected := []string{"mem0_profile", "mem0_search", "mem0_conclude"}
	for i, name := range names {
		if name != expected[i] {
			t.Errorf("schemas[%d].Name = %q, want %q", i, name, expected[i])
		}
	}
}

func TestMem0Provider_SystemPromptBlock(t *testing.T) {
	p := NewMem0Provider("key", "user-42", "", false)
	block := p.SystemPromptBlock()
	if block == "" {
		t.Fatal("SystemPromptBlock() 返回空串")
	}
	// 应包含 userID
	if !containsStr(block, "user-42") {
		t.Errorf("SystemPromptBlock() = %q, 应包含 userID", block)
	}
}

func TestMem0Provider_Prefetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"memory": "偏好Go", "score": 0.9},
				{"memory": "使用vim", "score": 0.7},
			},
		})
	}))
	defer server.Close()

	p := newTestMem0Provider(server)
	result, err := p.Prefetch(context.Background(), "测试", WithUserID("user-1"))
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	if !containsStr(result, "## Mem0 Memory") {
		t.Errorf("Prefetch() = %q, 应包含 '## Mem0 Memory'", result)
	}
	if !containsStr(result, "偏好Go") {
		t.Errorf("Prefetch() = %q, 应包含 '偏好Go'", result)
	}
}

func TestMem0Provider_Prefetch_空结果(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode([]any{})
	}))
	defer server.Close()

	p := newTestMem0Provider(server)
	result, err := p.Prefetch(context.Background(), "不存在")
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	if result != "" {
		t.Errorf("Prefetch() = %q, want empty", result)
	}
}

func TestMem0Provider_Prefetch_空query(t *testing.T) {
	p := NewMem0Provider("key", "u1", "", false)
	result, err := p.Prefetch(context.Background(), "")
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	if result != "" {
		t.Errorf("Prefetch() = %q, want empty (空 query)", result)
	}
}

func TestMem0Provider_Prefetch_API失败(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	p := newTestMem0Provider(server)
	result, err := p.Prefetch(context.Background(), "测试")
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	if result != "" {
		t.Errorf("Prefetch() = %q, want empty (API 失败)", result)
	}
	if p.consecutiveFailures != 1 {
		t.Errorf("consecutiveFailures = %d, want 1", p.consecutiveFailures)
	}
}

func TestMem0Provider_SyncTurn(t *testing.T) {
	var called int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&called, 1)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"id": "mem-1"}
	}))
	defer server.Close()

	p := newTestMem0Provider(server)
	err := p.SyncTurn(context.Background(), "你好", "你好！")
	if err != nil {
		t.Fatalf("SyncTurn() error = %v", err)
	}
	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("API 调用次数 = %d, want 1", atomic.LoadInt32(&called))
	}
}

func TestMem0Provider_SyncTurn_空消息(t *testing.T) {
	p := NewMem0Provider("key", "u1", "", false)
	err := p.SyncTurn(context.Background(), "", "assistant")
	if err != nil {
		t.Fatalf("SyncTurn() error = %v", err)
	}
	err = p.SyncTurn(context.Background(), "user", "")
	if err != nil {
		t.Fatalf("SyncTurn() error = %v", err)
	}
}

func TestMem0Provider_HandleToolCall_Profile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{
			{"memory": "偏好暗色主题"},
			{"memory": "使用zsh"},
		})
	}))
	defer server.Close()

	p := newTestMem0Provider(server)
	result, err := p.HandleToolCall(context.Background(), "mem0_profile", map[string]any{})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}

	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	if parsed["result"] == nil {
		t.Error("result 应有值")
	}
	if count, ok := parsed["count"].(float64); !ok || int(count) != 2 {
		t.Errorf("count = %v, want 2", parsed["count"])
	}
}

func TestMem0Provider_HandleToolCall_Search(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"memory": "Go语言", "score": 0.95},
			},
		})
	}))
	defer server.Close()

	p := newTestMem0Provider(server)
	result, err := p.HandleToolCall(context.Background(), "mem0_search", map[string]any{
		"query": "语言偏好",
	})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}

	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	if parsed["results"] == nil {
		t.Error("results 应有值")
	}
}

func TestMem0Provider_HandleToolCall_Search_缺query(t *testing.T) {
	p := NewMem0Provider("key", "u1", "", false)
	result, err := p.HandleToolCall(context.Background(), "mem0_search", map[string]any{})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}

	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	errMsg, _ := parsed["error"].(string)
	if !containsStr(errMsg, "query") {
		t.Errorf("error = %q, 应提及 query", errMsg)
	}
}

func TestMem0Provider_HandleToolCall_Conclude(t *testing.T) {
	var called int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&called, 1)
		// 验证 infer=false
		var req mem0AddRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Infer == nil || *req.Infer != false {
			t.Errorf("infer = %v, want false", req.Infer)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"id": "mem-1"}
	}))
	defer server.Close()

	p := newTestMem0Provider(server)
	result, err := p.HandleToolCall(context.Background(), "mem0_conclude", map[string]any{
		"conclusion": "我喜欢Go语言",
	})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}

	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	if parsed["result"] != "Fact stored." {
		t.Errorf("result = %v, want 'Fact stored.'", parsed["result"])
	}
}

func TestMem0Provider_HandleToolCall_未知工具(t *testing.T) {
	p := NewMem0Provider("key", "u1", "", false)
	p.initialized = true
	result, err := p.HandleToolCall(context.Background(), "unknown_tool", map[string]any{})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}

	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	errMsg, _ := parsed["error"].(string)
	if !containsStr(errMsg, "Unknown tool") {
		t.Errorf("error = %q, 应提及 Unknown tool", errMsg)
	}
}

func TestMem0Provider_熔断器_开启与恢复(t *testing.T) {
	p := NewMem0Provider("key", "u1", "", false)
	p.initialized = true

	// 连续失败 5 次
	for i := 0; i < 5; i++ {
		p.recordFailure()
	}
	if !p.isBreakerOpen() {
		t.Error("熔断器应已开启")
	}

	// 冷却期后应自动恢复
	p.breakerOpenUntil = time.Now().Add(-1 * time.Second) // 模拟冷却期已过
	if p.isBreakerOpen() {
		t.Error("冷却期过后熔断器应关闭")
	}

	// 成功后重置
	p.consecutiveFailures = 4
	p.recordSuccess()
	if p.consecutiveFailures != 0 {
		t.Errorf("consecutiveFailures = %d, want 0 (成功后重置)", p.consecutiveFailures)
	}
}

func TestMem0Provider_HandleToolCall_熔断器开启(t *testing.T) {
	p := NewMem0Provider("key", "u1", "", false)
	p.initialized = true

	// 触发熔断
	for i := 0; i < 5; i++ {
		p.recordFailure()
	}

	result, err := p.HandleToolCall(context.Background(), "mem0_search", map[string]any{"query": "test"})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}

	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	errMsg, _ := parsed["error"].(string)
	if !containsStr(errMsg, "temporarily unavailable") {
		t.Errorf("error = %q, 应提及 temporarily unavailable", errMsg)
	}
}

func TestMem0Provider_Prefetch_熔断器开启(t *testing.T) {
	p := NewMem0Provider("key", "u1", "", false)
	p.initialized = true

	for i := 0; i < 5; i++ {
		p.recordFailure()
	}

	result, err := p.Prefetch(context.Background(), "测试")
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	if result != "" {
		t.Errorf("Prefetch() = %q, want empty (熔断器开启)", result)
	}
}

func TestMem0Provider_QueuePrefetch(t *testing.T) {
	var called int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&called, 1)
		json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	}))
	defer server.Close()

	p := newTestMem0Provider(server)
	p.QueuePrefetch(context.Background(), "预热查询")

	// 等待 goroutine 完成
	time.Sleep(200 * time.Millisecond)

	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("API 调用次数 = %d, want 1", atomic.LoadInt32(&called))
	}
}

func TestMem0Provider_QueuePrefetch_熔断器开启(t *testing.T) {
	p := NewMem0Provider("key", "u1", "", false)
	p.initialized = true

	for i := 0; i < 5; i++ {
		p.recordFailure()
	}

	// 不应启动 goroutine
	p.QueuePrefetch(context.Background(), "测试")
	// 无 panic 即可
}

func TestMem0Provider_Shutdown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	}))
	defer server.Close()

	p := newTestMem0Provider(server)
	p.QueuePrefetch(context.Background(), "预热")

	err := p.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	if p.IsInitialized() {
		t.Error("Shutdown 后 IsInitialized() 应为 false")
	}
	if p.client != nil {
		t.Error("Shutdown 后 client 应为 nil")
	}
}

func TestMem0Provider_IsInitialized(t *testing.T) {
	p := NewMem0Provider("key", "u1", "", false)
	if p.IsInitialized() {
		t.Error("初始化前 IsInitialized() 应为 false")
	}
	p.Initialize(context.Background())
	if !p.IsInitialized() {
		t.Error("初始化后 IsInitialized() 应为 true")
	}
}

func TestMem0Provider_接口满足(t *testing.T) {
	// 编译时验证 Mem0Provider 满足 MemoryProvider 接口
	var _ MemoryProvider = (*Mem0Provider)(nil)
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test -v -count=1 ./internal/agentcore/memory/external/...`
Expected: 全部 PASS

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/memory/external/mem0_provider_test.go
git commit -m "test(7.14): 添加 Mem0Provider 单元测试"
```

---

### Task 8: doc.go 更新与 IMPLEMENTATION_PLAN 回填

**Files:**
- Modify: `internal/agentcore/memory/external/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 doc.go 文件目录树**

将 `doc.go` 的文件目录部分更新为：

```go
// 文件目录：
//
//	external/
//	├── doc.go              # 包文档
//	├── provider.go         # MemoryProvider 接口 + BaseMemoryProvider + ToolSchema + ProviderOption
//	├── mem0_client.go      # Mem0 HTTP 客户端（REST API 封装：search/getAll/add）
//	└── mem0_provider.go    # Mem0Provider — MemoryProvider 的 Mem0 实现（熔断器+工具调用）
```

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md 7.14 状态**

将 7.14 行的 `☐` 改为 `✅`：

```
| 7.14 | ✅ | Mem0Provider | Mem0 适配 | `openjiuwen/core/memory/external/mem0_provider.py` |
```

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/memory/external/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs(7.14): 更新 doc.go 文件目录和 IMPLEMENTATION_PLAN 7.14 状态"
```

---

### Task 9: 覆盖率验证与最终编译

**Files:** 无新增

- [ ] **Step 1: 运行完整测试并检查覆盖率**

Run: `cd /home/opensource/uapclaw-gateway && go test -cover -count=1 ./internal/agentcore/memory/external/...`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 2: 如覆盖率不足，补充缺失测试用例**

根据覆盖率报告，补充以下可能缺失的测试：
- `SyncTurn` API 失败场景
- `SyncTurn` 熔断器开启场景
- `QueuePrefetch` API 失败场景
- `readFilters` 无 agentID 场景
- `writeFilters` 验证

- [ ] **Step 3: 全包编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./...`
Expected: 编译成功

- [ ] **Step 4: 最终 Commit**

```bash
git add -A
git commit -m "test(7.14): 补充覆盖率至达标"
```
