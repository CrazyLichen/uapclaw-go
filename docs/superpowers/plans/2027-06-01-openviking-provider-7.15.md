# OpenVikingProvider 7.15 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 OpenVikingProvider，适配 OpenViking REST API，为 Agent 提供 5 个双向记忆工具和会话型记忆同步。

**Architecture:** 一比一翻译 Python `openviking_memory_provider.py`，拆为 `viking_client.go`（HTTP 客户端）+ `viking_provider.go`（Provider 逻辑），不内建熔断器，依赖 ExternalMemoryRail 外层熔断器。

**Tech Stack:** Go 标准库 `net/http` + `net/http/httptest`（测试 mock），`encoding/json`

**Design Doc:** `docs/superpowers/specs/2027-06-01-openviking-provider-7.15-design.md`

**Python Source:** `openjiuwen/core/memory/external/openviking_memory_provider.py`

---

### Task 1: viking_client.go — HTTP 客户端

**Files:**
- Create: `internal/agentcore/memory/external/viking_client.go`

- [ ] **Step 1: 创建 viking_client.go 文件**

```go
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

// ──────────────────────────── 枚举 ────────────────────────────

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
	defer resp.Body.Close()
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
	defer resp.Body.Close()

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
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/memory/external/`
Expected: 编译成功，无错误

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/memory/external/viking_client.go
git commit -m "feat(memory): 添加 vikingClient HTTP 客户端 (7.15 步骤1)"
```

---

### Task 2: viking_client_test.go — HTTP 客户端测试

**Files:**
- Create: `internal/agentcore/memory/external/viking_client_test.go`

- [ ] **Step 1: 创建 viking_client_test.go**

```go
package external

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestVikingClient 创建使用 mock server 的 vikingClient
func newTestVikingClient(server *httptest.Server) *vikingClient {
	return newVikingClient(server.URL, "test-api-key", "test-account", "test-user", "test-agent")
}

func TestVikingClient_Health_成功(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newTestVikingClient(server)
	if !client.health(context.Background()) {
		t.Error("health() = false, want true")
	}
}

func TestVikingClient_Health_失败(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := newTestVikingClient(server)
	if client.health(context.Background()) {
		t.Error("health() = true, want false (500)")
	}
}

func TestVikingClient_Health_连接失败(t *testing.T) {
	// 使用一个不存在的地址
	client := newVikingClient("http://127.0.0.1:1", "", "a", "u", "g")
	if client.health(context.Background()) {
		t.Error("health() = true, want false (连接失败)")
	}
}

func TestVikingClient_Post_成功(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/test" {
			t.Errorf("Path = %q, want /api/v1/test", r.URL.Path)
		}

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["key"] != "value" {
			t.Errorf("body[key] = %v, want value", body["key"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"result": "ok"})
	}))
	defer server.Close()

	client := newTestVikingClient(server)
	result, err := client.post(context.Background(), "/api/v1/test", map[string]any{"key": "value"})
	if err != nil {
		t.Fatalf("post() error = %v", err)
	}
	if result["result"] != "ok" {
		t.Errorf("result = %v, want ok", result["result"])
	}
}

func TestVikingClient_Post_错误状态码(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	client := newTestVikingClient(server)
	_, err := client.post(context.Background(), "/api/v1/test", map[string]any{})
	if err == nil {
		t.Fatal("post() 应返回错误（500）")
	}
}

func TestVikingClient_Get_带参数(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Method = %q, want GET", r.Method)
		}
		if r.URL.Query().Get("uri") != "viking://test" {
			t.Errorf("uri param = %q, want 'viking://test'", r.URL.Query().Get("uri"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"result": "data"})
	}))
	defer server.Close()

	client := newTestVikingClient(server)
	result, err := client.get(context.Background(), "/api/v1/content/read", map[string]string{"uri": "viking://test"})
	if err != nil {
		t.Fatalf("get() error = %v", err)
	}
	if result["result"] != "data" {
		t.Errorf("result = %v, want data", result["result"])
	}
}

func TestVikingClient_Get_错误状态码(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Not Found"))
	}))
	defer server.Close()

	client := newTestVikingClient(server)
	_, err := client.get(context.Background(), "/api/v1/test", nil)
	if err == nil {
		t.Fatal("get() 应返回错误（404）")
	}
}

func TestVikingClient_Close(t *testing.T) {
	client := newTestVikingClient(htptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})))
	// 不应 panic
	client.close()
}

func TestVikingClient_请求Header(t *testing.T) {
	var captured *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newTestVikingClient(server)
	client.health(context.Background())

	if captured == nil {
		t.Fatal("未捕获请求")
	}
	if captured.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", captured.Header.Get("Content-Type"))
	}
	if captured.Header.Get("X-OpenViking-Account") != "test-account" {
		t.Errorf("X-OpenViking-Account = %q, want test-account", captured.Header.Get("X-OpenViking-Account"))
	}
	if captured.Header.Get("X-OpenViking-User") != "test-user" {
		t.Errorf("X-OpenViking-User = %q, want test-user", captured.Header.Get("X-OpenViking-User"))
	}
	if captured.Header.Get("X-OpenViking-Agent") != "test-agent" {
		t.Errorf("X-OpenViking-Agent = %q, want test-agent", captured.Header.Get("X-OpenViking-Agent"))
	}
	if captured.Header.Get("X-API-Key") != "test-api-key" {
		t.Errorf("X-API-Key = %q, want test-api-key", captured.Header.Get("X-API-Key"))
	}
}

func TestVikingClient_请求Header_无APIKey(t *testing.T) {
	var captured *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newVikingClient(server.URL, "", "a", "u", "g")
	client.health(context.Background())

	if captured == nil {
		t.Fatal("未捕获请求")
	}
	if captured.Header.Get("X-API-Key") != "" {
		t.Errorf("X-API-Key = %q, want empty (无 apiKey)", captured.Header.Get("X-API-Key"))
	}
}
```

- [ ] **Step 2: 运行测试验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/memory/external/ -run "TestVikingClient" -v`
Expected: 全部 PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/memory/external/viking_client_test.go
git commit -m "test(memory): 添加 vikingClient 测试 (7.15 步骤2)"
```

---

### Task 3: viking_provider.go — Provider 结构体 + 构造函数 + 工具 Schema + 基础接口方法

**Files:**
- Create: `internal/agentcore/memory/external/viking_provider.go`

- [ ] **Step 1: 创建 viking_provider.go（第一部分：结构体 + 构造函数 + Schema 变量 + Name/IsAvailable/IsInitialized/SystemPromptBlock/GetToolSchemas）**

```go
package external

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OpenVikingProvider OpenViking 外部记忆提供者。
// 对齐 Python: OpenVikingMemoryProvider (openjiuwen/core/memory/external/openviking_memory_provider.py)
//
// 适配 OpenViking 上下文数据库 REST API，提供 5 个双向记忆工具和会话型记忆同步。
// 与 Mem0Provider 不同，OpenViking 是会话型的：sync_turn 记录对话轮次，on_session_end 时 commit session。
// 不内建熔断器，依赖 ExternalMemoryRail 外层熔断器（对齐 Python 无内建熔断器）。
type OpenVikingProvider struct {
	BaseMemoryProvider
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
	// client OpenViking HTTP 客户端（Initialize 中创建）
	client *vikingClient
	// sessionID 会话标识
	sessionID string
	// initialized 是否已初始化
	initialized bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var vikingLogComponent = logger.ComponentAgentCore

// vikingSearchSchema viking_search 工具 Schema
// 对齐 Python: VIKING_SEARCH_SCHEMA
var vikingSearchSchema = ToolSchema{
	Name:        "viking_search",
	Description: "在知识库中进行全域搜索.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "搜索查询词."},
			"mode": map[string]any{
				"type":        "string",
				"enum":        []string{"auto", "fast", "deep"},
				"description": "搜索模式（默认：auto）.",
			},
			"top_k": map[string]any{"type": "integer", "description": "最大返回结果数（默认10）."},
		},
		"required": []string{"query"},
	},
}

// vikingReadSchema viking_read 工具 Schema
// 对齐 Python: VIKING_READ_SCHEMA
var vikingReadSchema = ToolSchema{
	Name:        "viking_read",
	Description: "读取 viking:// URI 上的内容.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"uri": map[string]any{"type": "string", "description": "要读取的 viking:// URI."},
			"detail": map[string]any{
				"type":        "string",
				"enum":        []string{"abstract", "overview", "full"},
				"description": "详情级别（默认：overview）.",
			},
		},
		"required": []string{"uri"},
	},
}

// vikingBrowseSchema viking_browse 工具 Schema
// 对齐 Python: VIKING_BROWSE_SCHEMA
var vikingBrowseSchema = ToolSchema{
	Name:        "viking_browse",
	Description: "浏览知识库结构.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list", "tree", "stat"},
				"description": "浏览操作.",
			},
			"path": map[string]any{"type": "string", "description": "浏览路径（默认：/）."},
		},
		"required": []string{"action"},
	},
}

// vikingRememberSchema viking_remember 工具 Schema
// 对齐 Python: VIKING_REMEMBER_SCHEMA
var vikingRememberSchema = ToolSchema{
	Name:        "viking_remember",
	Description: "显式存储一个事实或偏好.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content": map[string]any{"type": "string", "description": "要记住的事实."},
			"category": map[string]any{
				"type":        "string",
				"enum":        []string{"preference", "entity", "event", "case", "pattern"},
				"description": "记忆类别.",
			},
		},
		"required": []string{"content"},
	},
}

// vikingAddResourceSchema viking_add_resource 工具 Schema
// 对齐 Python: VIKING_ADD_RESOURCE_SCHEMA
var vikingAddResourceSchema = ToolSchema{
	Name:        "viking_add_resource",
	Description: "索引一个 URL 或文档以供后续搜索.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url":   map[string]any{"type": "string", "description": "要索引的 URL 或文件路径."},
			"title": map[string]any{"type": "string", "description": "可选标题."},
		},
		"required": []string{"url"},
	},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOpenVikingProvider 创建 OpenVikingProvider 实例。
// 对齐 Python: OpenVikingMemoryProvider.__init__(endpoint, api_key, account, user, agent)
//
// 参数为空时从环境变量 fallback：
//   - endpoint → OPENVIKING_ENDPOINT
//   - apiKey → OPENVIKING_API_KEY
//   - account → OPENVIKING_ACCOUNT（默认 "default"）
//   - user → OPENVIKING_USER（默认 "default"）
//   - agent → OPENVIKING_AGENT（默认 "hermes"）
func NewOpenVikingProvider(endpoint, apiKey, account, user, agent string) *OpenVikingProvider {
	// 对齐 Python: self._endpoint = endpoint or os.environ.get("OPENVIKING_ENDPOINT", "")
	if endpoint == "" {
		endpoint = os.Getenv("OPENVIKING_ENDPOINT")
	}
	if apiKey == "" {
		apiKey = os.Getenv("OPENVIKING_API_KEY")
	}
	// 对齐 Python: self._account = account or os.environ.get("OPENVIKING_ACCOUNT", "default")
	if account == "" {
		if envVal := os.Getenv("OPENVIKING_ACCOUNT"); envVal != "" {
			account = envVal
		} else {
			account = "default"
		}
	}
	if user == "" {
		if envVal := os.Getenv("OPENVIKING_USER"); envVal != "" {
			user = envVal
		} else {
			user = "default"
		}
	}
	if agent == "" {
		if envVal := os.Getenv("OPENVIKING_AGENT"); envVal != "" {
			agent = envVal
		} else {
			agent = "hermes"
		}
	}
	return &OpenVikingProvider{
		endpoint: endpoint,
		apiKey:   apiKey,
		account:  account,
		user:     user,
		agent:    agent,
	}
}

// Name 返回 Provider 唯一名称。
// Python: @property name -> "openviking"
func (p *OpenVikingProvider) Name() string {
	return "openviking"
}

// IsAvailable 检查 Provider 是否已配置且就绪。
// Python: is_available() -> bool(self._endpoint)
func (p *OpenVikingProvider) IsAvailable() bool {
	return p.endpoint != ""
}

// IsInitialized 返回 Provider 是否已初始化。
// Python: @property is_initialized -> self._client is not None
func (p *OpenVikingProvider) IsInitialized() bool {
	return p.initialized
}

// GetToolSchemas 返回 Provider 提供的工具 Schema 列表。
// Python: get_tool_schemas() -> [VIKING_SEARCH_SCHEMA, ...]
func (p *OpenVikingProvider) GetToolSchemas() []ToolSchema {
	return []ToolSchema{
		vikingSearchSchema,
		vikingReadSchema,
		vikingBrowseSchema,
		vikingRememberSchema,
		vikingAddResourceSchema,
	}
}

// SystemPromptBlock 返回 Provider 的系统提示词引导块。
// Python: system_prompt_block()
func (p *OpenVikingProvider) SystemPromptBlock() string {
	return "# OpenViking Memory\n\n" +
		"Use `viking_search` to find knowledge (modes: auto/fast/deep).\n" +
		"Use `viking_read` to read content at a viking:// URI (levels: abstract/overview/full).\n" +
		"Use `viking_browse` to navigate the knowledge structure.\n" +
		"Use `viking_remember` to explicitly store facts.\n" +
		"Use `viking_add_resource` to index URLs/documents."
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/memory/external/`
Expected: 编译成功（OpenVikingProvider 还未实现所有接口方法，先不验证编译——等 Task 4 补全所有方法后再验证）

- [ ] **Step 3: 不提交，继续 Task 4**

---

### Task 4: viking_provider.go — Initialize / Prefetch / SyncTurn / HandleToolCall / OnSessionEnd / Shutdown

**Files:**
- Modify: `internal/agentcore/memory/external/viking_provider.go`（追加方法）

- [ ] **Step 1: 在 viking_provider.go 的 `// ──────────────────────────── 非导出函数 ────────────────────────────` 之前追加接口方法实现**

在 `SystemPromptBlock` 方法之后、`// ──────────────────────────── 非导出函数 ────────────────────────────` 注释之前追加以下导出函数：

```go
// Initialize 初始化 Provider。
// 对齐 Python: async def initialize(self, **kwargs) -> None
func (p *OpenVikingProvider) Initialize(ctx context.Context, opts ...ProviderOption) error {
	po := applyOptions(opts...)

	// 对齐 Python: self._session_id = kwargs.get("session_id", "")
	if po.SessionID != "" {
		p.sessionID = po.SessionID
	}

	// 对齐 Python: self._client = _VikingClient(self._endpoint, self._api_key, ...)
	p.client = newVikingClient(p.endpoint, p.apiKey, p.account, p.user, p.agent)

	// 对齐 Python: healthy = await asyncio.to_thread(self._client.health)
	healthy := p.client.health(ctx)
	if !healthy {
		// 对齐 Python: logger.warning("OpenViking at %s not reachable", self._endpoint)
		logger.Warn(vikingLogComponent).
			Str("endpoint", p.endpoint).
			Msg("OpenViking 不可达")
		p.client = nil
		p.initialized = false
		return nil
	}

	p.initialized = true
	return nil
}

// Prefetch 根据查询预取记忆上下文。
// 对齐 Python: async def prefetch(self, query, **kwargs) -> str
func (p *OpenVikingProvider) Prefetch(ctx context.Context, query string, _ ...ProviderOption) (string, error) {
	// 对齐 Python: if not self._client or not query: return ""
	if p.client == nil || query == "" {
		return "", nil
	}

	// 对齐 Python: resp = await asyncio.to_thread(self._client.post, "/api/v1/search/find", {"query": query, "top_k": 5})
	resp, err := p.client.post(ctx, "/api/v1/search/find", map[string]any{
		"query": query,
		"top_k": 5,
	})
	if err != nil {
		// 对齐 Python: logger.debug("OpenViking prefetch failed: %s", e)
		logger.Debug(vikingLogComponent).Err(err).Msg("OpenViking prefetch 失败")
		return "", nil
	}

	// 对齐 Python: result = resp.get("result", {})
	result, _ := resp["result"].(map[string]any)
	if result == nil {
		return "", nil
	}

	// 对齐 Python: for ctx_type in ("memories", "resources"): items = result.get(ctx_type, [])  for item in items[:3]:
	parts := make([]string, 0)
	for _, ctxType := range []string{"memories", "resources"} {
		items, _ := result[ctxType].([]any)
		for i, item := range items {
			if i >= 3 {
				break
			}
			itemMap, _ := item.(map[string]any)
			if itemMap == nil {
				continue
			}
			uri, _ := itemMap["uri"].(string)
			abstract, _ := itemMap["abstract"].(string)
			score := floatVal(itemMap["score"])
			if abstract != "" {
				parts = append(parts, fmt.Sprintf("- [%.2f] %s (%s)", score, abstract, uri))
			}
		}
	}

	// 对齐 Python: if not parts: return ""
	if len(parts) == 0 {
		return "", nil
	}
	// 对齐 Python: return "## OpenViking Context\n" + "\n".join(parts)
	return "## OpenViking Context\n" + strings.Join(parts, "\n"), nil
}

// SyncTurn 同步一轮对话到外部记忆。
// 对齐 Python: async def sync_turn(self, user_msg, assistant_msg, **kwargs) -> None
func (p *OpenVikingProvider) SyncTurn(ctx context.Context, userMsg, assistantMsg string, opts ...ProviderOption) error {
	// 对齐 Python: if not self._client: return
	if p.client == nil {
		return nil
	}

	// 对齐 Python: sid = kwargs.get("session_id", self._session_id)
	sid := p.sessionID
	po := applyOptions(opts...)
	if po.SessionID != "" {
		sid = po.SessionID
	}

	// 对齐 Python: await asyncio.to_thread(self._client.post, f"/api/v1/sessions/{sid}/messages", {"role": "user", "content": user_msg[:4000]})
	_, err := p.client.post(ctx, fmt.Sprintf("/api/v1/sessions/%s/messages", sid), map[string]any{
		"role":    "user",
		"content": truncStr(userMsg, 4000),
	})
	if err != nil {
		// 对齐 Python: logger.debug("OpenViking sync failed: %s", e)
		logger.Debug(vikingLogComponent).Err(err).Msg("OpenViking sync_turn 失败")
		return nil
	}

	// 对齐 Python: await asyncio.to_thread(self._client.post, ..., {"role": "assistant", "content": assistant_msg[:4000]})
	_, err = p.client.post(ctx, fmt.Sprintf("/api/v1/sessions/%s/messages", sid), map[string]any{
		"role":    "assistant",
		"content": truncStr(assistantMsg, 4000),
	})
	if err != nil {
		logger.Debug(vikingLogComponent).Err(err).Msg("OpenViking sync_turn 失败")
		return nil
	}

	return nil
}

// HandleToolCall 处理工具调用并返回结果字符串。
// 对齐 Python: async def handle_tool_call(self, tool_name, args) -> str
func (p *OpenVikingProvider) HandleToolCall(ctx context.Context, toolName string, args map[string]any) (string, error) {
	// 对齐 Python: if not self._client: return json.dumps({"error": "OpenViking not connected"})
	if p.client == nil {
		b, _ := json.Marshal(map[string]string{"error": "OpenViking not connected"})
		return string(b), nil
	}

	// 对齐 Python: try: ... except Exception as e: return json.dumps({"error": str(e)})
	result, err := p.dispatchToolCall(ctx, toolName, args)
	if err != nil {
		b, _ := json.Marshal(map[string]string{"error": err.Error()})
		return string(b), nil
	}
	b, _ := json.Marshal(result)
	return string(b), nil
}

// OnSessionEnd 会话结束时回调。
// 对齐 Python: async def on_session_end(self, messages) -> None
func (p *OpenVikingProvider) OnSessionEnd(ctx context.Context, _ []map[string]any) error {
	// 对齐 Python: if not self._client or not self._session_id: return
	if p.client == nil || p.sessionID == "" {
		return nil
	}

	// 对齐 Python: await asyncio.to_thread(self._client.post, f"/api/v1/sessions/{self._session_id}/commit", {})
	_, err := p.client.post(ctx, fmt.Sprintf("/api/v1/sessions/%s/commit", p.sessionID), map[string]any{})
	if err != nil {
		// 对齐 Python: logger.debug("OpenViking session commit failed: %s", e)
		logger.Debug(vikingLogComponent).Err(err).Msg("OpenViking session commit 失败")
	}
	return nil
}

// Shutdown 关闭 Provider 释放资源。
// 对齐 Python: async def shutdown(self) -> None
func (p *OpenVikingProvider) Shutdown(_ context.Context) error {
	if p.client != nil {
		// 对齐 Python: self._client.close() — try/except 包裹
		func() {
			defer func() {
				if r := recover(); r != nil {
					// 对齐 Python: logger.debug("OpenViking client close failed: %s", e)
					logger.Debug(vikingLogComponent).
						Str("event_type", "viking_provider_shutdown").
						Msgf("关闭客户端失败: %v", r)
				}
			}()
			p.client.close()
		}()
		p.client = nil
	}
	p.initialized = false
	return nil
}
```

- [ ] **Step 2: 在 `// ──────────────────────────── 非导出函数 ────────────────────────────` 之后追加 dispatchToolCall + 5 个 handleXxx + 辅助函数**

```go
// dispatchToolCall 分发工具调用到对应的 handleXxx 方法。
// 对齐 Python: handle_tool_call 中的 if/elif 分支
func (p *OpenVikingProvider) dispatchToolCall(ctx context.Context, toolName string, args map[string]any) (any, error) {
	switch toolName {
	case "viking_search":
		return p.handleVikingSearch(ctx, args)
	case "viking_read":
		return p.handleVikingRead(ctx, args)
	case "viking_browse":
		return p.handleVikingBrowse(ctx, args)
	case "viking_remember":
		return p.handleVikingRemember(ctx, args)
	case "viking_add_resource":
		return p.handleVikingAddResource(ctx, args)
	default:
		return nil, fmt.Errorf("Unknown tool: %s", toolName)
	}
}

// handleVikingSearch 处理 viking_search 工具调用。
// 对齐 Python: handle_tool_call 中 tool_name == "viking_search" 分支
func (p *OpenVikingProvider) handleVikingSearch(ctx context.Context, args map[string]any) (any, error) {
	// 对齐 Python: query = args.get("query", ""); if not query: return json.dumps({"error": "query is required"})
	query, _ := args["query"].(string)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}

	// 对齐 Python: payload = {"query": query}
	payload := map[string]any{"query": query}

	// 对齐 Python: mode = args.get("mode", "auto"); if mode != "auto": payload["mode"] = mode
	mode, _ := args["mode"].(string)
	if mode != "" && mode != "auto" {
		payload["mode"] = mode
	}

	// 对齐 Python: if args.get("scope"): payload["target_uri"] = args["scope"]
	if scope, _ := args["scope"].(string); scope != "" {
		payload["target_uri"] = scope
	}

	// 对齐 Python: if args.get("limit"): payload["top_k"] = args["limit"]
	if limit := floatVal(args["limit"]); limit > 0 {
		payload["top_k"] = int(limit)
	}

	// 对齐 Python: resp = await asyncio.to_thread(self._client.post, "/api/v1/search/find", payload)
	resp, err := p.client.post(ctx, "/api/v1/search/find", payload)
	if err != nil {
		return nil, err
	}

	// 对齐 Python: result = resp.get("result", {})
	resultMap, _ := resp["result"].(map[string]any)
	if resultMap == nil {
		return map[string]any{"results": []any{}, "total": 0}, nil
	}

	// 对齐 Python: for ctx_type in ("memories", "resources", "skills"): ...
	type scoredEntry struct {
		score float64
		entry map[string]any
	}
	var scoredEntries []scoredEntry

	for _, ctxType := range []string{"memories", "resources", "skills"} {
		items, _ := resultMap[ctxType].([]any)
		for _, item := range items {
			itemMap, _ := item.(map[string]any)
			if itemMap == nil {
				continue
			}
			// 对齐 Python: raw_score = item.get("score"); sort_score = raw_score if raw_score is not None else 0.0
			rawScore := floatVal(itemMap["score"])
			sortScore := rawScore

			entry := map[string]any{
				"uri":      strVal(itemMap["uri"]),
				"type":     strings.TrimRight(ctxType, "s"), // 对齐 Python: ctx_type.rstrip("s")
				"abstract": strVal(itemMap["abstract"]),
			}
			// 对齐 Python: entry["score"] = round(raw_score, 3) if raw_score is not None else 0.0
			if rawScore > 0 {
				entry["score"] = roundTo3(rawScore)
			} else {
				entry["score"] = 0.0
			}

			// 对齐 Python: if item.get("relations"): entry["related"] = [r.get("uri") for r in item["relations"][:3]]
			if relations, ok := itemMap["relations"].([]any); ok && len(relations) > 0 {
				related := make([]string, 0, 3)
				for i, r := range relations {
					if i >= 3 {
						break
					}
					if rMap, ok := r.(map[string]any); ok {
						related = append(related, strVal(rMap["uri"]))
					}
				}
				entry["related"] = related
			}

			scoredEntries = append(scoredEntries, scoredEntry{score: sortScore, entry: entry})
		}
	}

	// 对齐 Python: scored_entries.sort(key=lambda x: x[0], reverse=True)
	sort.Slice(scoredEntries, func(i, j int) bool {
		return scoredEntries[i].score > scoredEntries[j].score
	})

	formatted := make([]map[string]any, len(scoredEntries))
	for i, se := range scoredEntries {
		formatted[i] = se.entry
	}

	// 对齐 Python: result = {"results": formatted, "total": resp.get("result", {}).get("total", len(formatted))}
	total := floatVal(resultMap["total"])
	if total == 0 {
		total = float64(len(formatted))
	}
	return map[string]any{
		"results": formatted,
		"total":   int(total),
	}, nil
}

// handleVikingRead 处理 viking_read 工具调用。
// 对齐 Python: handle_tool_call 中 tool_name == "viking_read" 分支
func (p *OpenVikingProvider) handleVikingRead(ctx context.Context, args map[string]any) (any, error) {
	// 对齐 Python: uri = args.get("uri", ""); if not uri: return json.dumps({"error": "uri is required"})
	uri, _ := args["uri"].(string)
	if uri == "" {
		return nil, fmt.Errorf("uri is required")
	}

	// 对齐 Python: level = args.get("level", args.get("detail", "overview"))
	level := strVal(args["level"])
	if level == "" {
		level = strVal(args["detail"])
	}
	if level == "" {
		level = "overview"
	}

	var result map[string]any
	var err error

	// 对齐 Python: if level == "abstract": ... elif level == "full": ... else: overview
	switch level {
	case "abstract":
		result, err = p.client.get(ctx, "/api/v1/content/abstract", map[string]string{"uri": uri})
	case "full":
		result, err = p.client.get(ctx, "/api/v1/content/read", map[string]string{"uri": uri})
	default:
		result, err = p.client.get(ctx, "/api/v1/content/overview", map[string]string{"uri": uri})
	}
	if err != nil {
		return nil, err
	}

	// 对齐 Python: content = result.get("result", "")
	content, _ := result["result"]
	var contentStr string
	switch v := content.(type) {
	case string:
		contentStr = v
	case map[string]any:
		// 对齐 Python: if not isinstance(content, str): content = content.get("content", "")
		contentStr, _ = v["content"].(string)
	default:
		contentStr = fmt.Sprintf("%v", content)
	}

	// 对齐 Python: if len(content) > 8000: content = content[:8000] + "\n\n[... truncated, ...]"
	if len(contentStr) > 8000 {
		contentStr = contentStr[:8000] + "\n\n[... truncated, use a more specific URI or abstract level]"
	}

	return map[string]any{
		"uri":     uri,
		"level":   level,
		"content": contentStr,
	}, nil
}

// handleVikingBrowse 处理 viking_browse 工具调用。
// 对齐 Python: handle_tool_call 中 tool_name == "viking_browse" 分支
func (p *OpenVikingProvider) handleVikingBrowse(ctx context.Context, args map[string]any) (any, error) {
	// 对齐 Python: action = args.get("action", "list")
	action, _ := args["action"].(string)
	if action == "" {
		action = "list"
	}

	// 对齐 Python: browse_path = args.get("path", "viking://")
	browsePath, _ := args["path"].(string)
	if browsePath == "" {
		browsePath = "viking://"
	}

	// 对齐 Python: endpoint_map = {"tree": "/api/v1/fs/tree", "list": "/api/v1/fs/ls", "stat": "/api/v1/fs/stat"}
	endpointMap := map[string]string{
		"tree": "/api/v1/fs/tree",
		"list": "/api/v1/fs/ls",
		"stat": "/api/v1/fs/stat",
	}
	path, ok := endpointMap[action]
	if !ok {
		path = "/api/v1/fs/ls"
	}

	// 对齐 Python: result = await asyncio.to_thread(self._client.get, path, {"uri": browse_path})
	result, err := p.client.get(ctx, path, map[string]string{"uri": browsePath})
	if err != nil {
		return nil, err
	}

	// 对齐 Python: entries = result.get("result", {})
	entries, _ := result["result"]

	// 对齐 Python: if action in ("list", "tree") and isinstance(entries, list):
	if (action == "list" || action == "tree") {
		if entriesList, ok := entries.([]any); ok {
			// 对齐 Python: for e in entries[:50]:
			formatted := make([]map[string]any, 0, len(entriesList))
			for i, e := range entriesList {
				if i >= 50 {
					break
				}
				eMap, _ := e.(map[string]any)
				if eMap == nil {
					continue
				}
				name := strVal(eMap["rel_path"])
				if name == "" {
					name = strVal(eMap["name"])
				}
				entryType := "file"
				if isDir, _ := eMap["isDir"].(bool); isDir {
					entryType = "dir"
				}
				formatted = append(formatted, map[string]any{
					"name":     name,
					"uri":      strVal(eMap["uri"]),
					"type":     entryType,
					"abstract": strVal(eMap["abstract"]),
				})
			}
			return map[string]any{
				"path":    browsePath,
				"entries": formatted,
			}, nil
		}
	}

	// stat 或非 list/tree 时直接返回原始 result
	return result, nil
}

// handleVikingRemember 处理 viking_remember 工具调用。
// 对齐 Python: handle_tool_call 中 tool_name == "viking_remember" 分支
func (p *OpenVikingProvider) handleVikingRemember(ctx context.Context, args map[string]any) (any, error) {
	// 对齐 Python: content = args.get("content", ""); if not content: return json.dumps({"error": "content is required"})
	content, _ := args["content"].(string)
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}

	// 对齐 Python: category = args.get("category", "")
	// 对齐 Python: text = f"[Remember] {content}"; if category: text = f"[Remember — {category}] {content}"
	category, _ := args["category"].(string)
	text := fmt.Sprintf("[Remember] %s", content)
	if category != "" {
		text = fmt.Sprintf("[Remember — %s] %s", category, content)
	}

	// 对齐 Python: await asyncio.to_thread(self._client.post, f"/api/v1/sessions/{self._session_id}/messages", {"role": "user", "parts": [...]})
	_, err := p.client.post(ctx, fmt.Sprintf("/api/v1/sessions/%s/messages", p.sessionID), map[string]any{
		"role":  "user",
		"parts": []map[string]any{{"type": "text", "text": text}},
	})
	if err != nil {
		return nil, err
	}

	// 对齐 Python: result = {"status": "stored", "message": "Memory recorded. ..."}
	return map[string]any{
		"status":  "stored",
		"message": "Memory recorded. Will be extracted and indexed on session commit.",
	}, nil
}

// handleVikingAddResource 处理 viking_add_resource 工具调用。
// 对齐 Python: handle_tool_call 中 tool_name == "viking_add_resource" 分支
func (p *OpenVikingProvider) handleVikingAddResource(ctx context.Context, args map[string]any) (any, error) {
	// 对齐 Python: url = args.get("url", ""); if not url: return json.dumps({"error": "url is required"})
	urlStr, _ := args["url"].(string)
	if urlStr == "" {
		return nil, fmt.Errorf("url is required")
	}

	// 对齐 Python: payload = {"path": url}
	payload := map[string]any{"path": urlStr}

	// 对齐 Python: if args.get("reason"): payload["reason"] = args["reason"]
	if reason, _ := args["reason"].(string); reason != "" {
		payload["reason"] = reason
	}

	// 对齐 Python: result = await asyncio.to_thread(self._client.post, "/api/v1/resources", payload)
	result, err := p.client.post(ctx, "/api/v1/resources", payload)
	if err != nil {
		return nil, err
	}

	// 对齐 Python: res_data = result.get("result", {}); result = {"status": "added", "root_uri": res_data.get("root_uri", ""), ...}
	resData, _ := result["result"].(map[string]any)
	rootURI := ""
	if resData != nil {
		rootURI, _ = resData["root_uri"].(string)
	}

	return map[string]any{
		"status":   "added",
		"root_uri": rootURI,
		"message":  "Resource queued for processing. Use viking_search after a moment to find it.",
	}, nil
}

// truncStr 截断字符串到指定长度。
// 对齐 Python: user_msg[:4000]
func truncStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// floatVal 从 map 值中提取 float64。
func floatVal(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}

// strVal 从 map 值中提取 string。
func strVal(v any) string {
	s, _ := v.(string)
	return s
}

// roundTo3 保留 3 位小数。
// 对齐 Python: round(raw_score, 3)
func roundTo3(f float64) float64 {
	return float64(int(f*1000+0.5)) / 1000
}
```

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/memory/external/`
Expected: 编译成功，无错误

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/memory/external/viking_provider.go
git commit -m "feat(memory): 添加 OpenVikingProvider 实现 (7.15 步骤3-4)"
```

---

### Task 5: viking_provider_test.go — Provider 测试

**Files:**
- Create: `internal/agentcore/memory/external/viking_provider_test.go`

- [ ] **Step 1: 创建 viking_provider_test.go**

```go
package external

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestVikingProvider 创建使用 mock server 的 OpenVikingProvider
func newTestVikingProvider(server *httptest.Server) *OpenVikingProvider {
	p := NewOpenVikingProvider(server.URL, "test-key", "test-account", "test-user", "test-agent")
	p.sessionID = "test-session"
	p.client = newVikingClient(server.URL, "test-key", "test-account", "test-user", "test-agent")
	p.initialized = true
	return p
}

func TestNewOpenVikingProvider_显式参数(t *testing.T) {
	p := NewOpenVikingProvider("http://localhost:8080", "key123", "acc", "usr", "ag")
	if p.endpoint != "http://localhost:8080" {
		t.Errorf("endpoint = %q, want http://localhost:8080", p.endpoint)
	}
	if p.apiKey != "key123" {
		t.Errorf("apiKey = %q, want key123", p.apiKey)
	}
	if p.account != "acc" {
		t.Errorf("account = %q, want acc", p.account)
	}
	if p.user != "usr" {
		t.Errorf("user = %q, want usr", p.user)
	}
	if p.agent != "ag" {
		t.Errorf("agent = %q, want ag", p.agent)
	}
}

func TestNewOpenVikingProvider_默认值(t *testing.T) {
	// 设置环境变量
	os.Setenv("OPENVIKING_ENDPOINT", "http://env-endpoint")
	os.Setenv("OPENVIKING_API_KEY", "env-key")
	os.Setenv("OPENVIKING_ACCOUNT", "env-account")
	os.Setenv("OPENVIKING_USER", "env-user")
	os.Setenv("OPENVIKING_AGENT", "env-agent")
	defer func() {
		os.Unsetenv("OPENVIKING_ENDPOINT")
		os.Unsetenv("OPENVIKING_API_KEY")
		os.Unsetenv("OPENVIKING_ACCOUNT")
		os.Unsetenv("OPENVIKING_USER")
		os.Unsetenv("OPENVIKING_AGENT")
	}()

	p := NewOpenVikingProvider("", "", "", "", "")
	if p.endpoint != "http://env-endpoint" {
		t.Errorf("endpoint = %q, want http://env-endpoint", p.endpoint)
	}
	if p.apiKey != "env-key" {
		t.Errorf("apiKey = %q, want env-key", p.apiKey)
	}
	if p.account != "env-account" {
		t.Errorf("account = %q, want env-account", p.account)
	}
	if p.user != "env-user" {
		t.Errorf("user = %q, want env-user", p.user)
	}
	if p.agent != "env-agent" {
		t.Errorf("agent = %q, want env-agent", p.agent)
	}
}

func TestNewOpenVikingProvider_无环境变量默认值(t *testing.T) {
	// 清除环境变量
	os.Unsetenv("OPENVIKING_ENDPOINT")
	os.Unsetenv("OPENVIKING_API_KEY")
	os.Unsetenv("OPENVIKING_ACCOUNT")
	os.Unsetenv("OPENVIKING_USER")
	os.Unsetenv("OPENVIKING_AGENT")

	p := NewOpenVikingProvider("", "", "", "", "")
	if p.endpoint != "" {
		t.Errorf("endpoint = %q, want empty", p.endpoint)
	}
	if p.account != "default" {
		t.Errorf("account = %q, want default", p.account)
	}
	if p.user != "default" {
		t.Errorf("user = %q, want default", p.user)
	}
	if p.agent != "hermes" {
		t.Errorf("agent = %q, want hermes", p.agent)
	}
}

func TestOpenVikingProvider_Name(t *testing.T) {
	p := NewOpenVikingProvider("http://localhost", "", "", "", "")
	if p.Name() != "openviking" {
		t.Errorf("Name() = %q, want %q", p.Name(), "openviking")
	}
}

func TestOpenVikingProvider_IsAvailable_有Endpoint(t *testing.T) {
	p := NewOpenVikingProvider("http://localhost", "", "", "", "")
	if !p.IsAvailable() {
		t.Error("IsAvailable() = false, want true (有 endpoint)")
	}
}

func TestOpenVikingProvider_IsAvailable_无Endpoint(t *testing.T) {
	p := NewOpenVikingProvider("", "", "", "", "")
	if p.IsAvailable() {
		t.Error("IsAvailable() = true, want false (无 endpoint)")
	}
}

func TestOpenVikingProvider_Initialize_成功(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := NewOpenVikingProvider(server.URL, "key", "acc", "usr", "ag")
	err := p.Initialize(context.Background(), WithSessionID("sess-1"))
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if !p.IsInitialized() {
		t.Error("IsInitialized() = false, want true")
	}
	if p.sessionID != "sess-1" {
		t.Errorf("sessionID = %q, want sess-1", p.sessionID)
	}
}

func TestOpenVikingProvider_Initialize_健康检查失败(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	p := NewOpenVikingProvider(server.URL, "key", "acc", "usr", "ag")
	err := p.Initialize(context.Background())
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if p.IsInitialized() {
		t.Error("IsInitialized() = true, want false (健康检查失败)")
	}
	if p.client != nil {
		t.Error("client 应为 nil (健康检查失败)")
	}
}

func TestOpenVikingProvider_SystemPromptBlock(t *testing.T) {
	p := NewOpenVikingProvider("http://localhost", "", "", "", "")
	block := p.SystemPromptBlock()
	if block == "" {
		t.Fatal("SystemPromptBlock() 返回空串")
	}
	if !strings.Contains(block, "viking_search") {
		t.Errorf("SystemPromptBlock() = %q, 应包含 viking_search", block)
	}
	if !strings.Contains(block, "viking_read") {
		t.Errorf("SystemPromptBlock() = %q, 应包含 viking_read", block)
	}
}

func TestOpenVikingProvider_GetToolSchemas(t *testing.T) {
	p := NewOpenVikingProvider("http://localhost", "", "", "", "")
	schemas := p.GetToolSchemas()
	if len(schemas) != 5 {
		t.Fatalf("len(GetToolSchemas()) = %d, want 5", len(schemas))
	}
	expected := []string{"viking_search", "viking_read", "viking_browse", "viking_remember", "viking_add_resource"}
	for i, name := range expected {
		if schemas[i].Name != name {
			t.Errorf("schemas[%d].Name = %q, want %q", i, schemas[i].Name, name)
		}
	}
}

func TestOpenVikingProvider_Prefetch_有结果(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search/find" {
			t.Errorf("Path = %q, want /api/v1/search/find", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"memories": []map[string]any{
					{"uri": "viking://mem1", "abstract": "记忆1", "score": 0.9},
					{"uri": "viking://mem2", "abstract": "记忆2", "score": 0.7},
				},
				"resources": []map[string]any{
					{"uri": "viking://res1", "abstract": "资源1", "score": 0.85},
				},
			},
		})
	}))
	defer server.Close()

	p := newTestVikingProvider(server)
	result, err := p.Prefetch(context.Background(), "测试查询")
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	if !strings.Contains(result, "## OpenViking Context") {
		t.Errorf("Prefetch() = %q, 应包含 '## OpenViking Context'", result)
	}
	if !strings.Contains(result, "记忆1") {
		t.Errorf("Prefetch() = %q, 应包含 '记忆1'", result)
	}
	if !strings.Contains(result, "资源1") {
		t.Errorf("Prefetch() = %q, 应包含 '资源1'", result)
	}
}

func TestOpenVikingProvider_Prefetch_无Client(t *testing.T) {
	p := NewOpenVikingProvider("http://localhost", "", "", "", "")
	result, err := p.Prefetch(context.Background(), "测试")
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	if result != "" {
		t.Errorf("Prefetch() = %q, want empty (无 client)", result)
	}
}

func TestOpenVikingProvider_Prefetch_空查询(t *testing.T) {
	p := newTestVikingProvider(htptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})))
	defer p.client.httpClient.CloseIdleConnections()

	result, err := p.Prefetch(context.Background(), "")
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	if result != "" {
		t.Errorf("Prefetch() = %q, want empty (空查询)", result)
	}
}

func TestOpenVikingProvider_SyncTurn_成功(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer server.Close()

	p := newTestVikingProvider(server)
	err := p.SyncTurn(context.Background(), "用户消息", "助手回复")
	if err != nil {
		t.Fatalf("SyncTurn() error = %v", err)
	}
	if callCount != 2 {
		t.Errorf("API 调用次数 = %d, want 2 (user + assistant)", callCount)
	}
}

func TestOpenVikingProvider_SyncTurn_无Client(t *testing.T) {
	p := NewOpenVikingProvider("http://localhost", "", "", "", "")
	err := p.SyncTurn(context.Background(), "你好", "你好！")
	if err != nil {
		t.Fatalf("SyncTurn() error = %v", err)
	}
}

func TestOpenVikingProvider_HandleToolCall_VikingSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"memories": []map[string]any{
					{"uri": "viking://m1", "score": 0.95, "abstract": "摘要1"},
				},
				"resources": []map[string]any{},
				"skills":    []map[string]any{},
				"total":     1,
			},
		})
	}))
	defer server.Close()

	p := newTestVikingProvider(server)
	result, err := p.HandleToolCall(context.Background(), "viking_search", map[string]any{"query": "测试"})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}

	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	results, _ := parsed["results"].([]any)
	if len(results) != 1 {
		t.Errorf("results 长度 = %d, want 1", len(results))
	}
}

func TestOpenVikingProvider_HandleToolCall_VikingRead(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"result": "这是读取到的内容",
		})
	}))
	defer server.Close()

	p := newTestVikingProvider(server)
	result, err := p.HandleToolCall(context.Background(), "viking_read", map[string]any{
		"uri":    "viking://doc1",
		"detail": "overview",
	})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}

	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	if parsed["uri"] != "viking://doc1" {
		t.Errorf("uri = %v, want viking://doc1", parsed["uri"])
	}
	if parsed["content"] != "这是读取到的内容" {
		t.Errorf("content = %v, want '这是读取到的内容'", parsed["content"])
	}
}

func TestOpenVikingProvider_HandleToolCall_VikingBrowse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"result": []map[string]any{
				{"name": "folder1", "uri": "viking://folder1", "isDir": true, "abstract": "文件夹"},
				{"name": "file1", "uri": "viking://file1", "isDir": false, "abstract": "文件"},
			},
		})
	}))
	defer server.Close()

	p := newTestVikingProvider(server)
	result, err := p.HandleToolCall(context.Background(), "viking_browse", map[string]any{
		"action": "list",
		"path":   "viking://",
	})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}

	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	entries, _ := parsed["entries"].([]any)
	if len(entries) != 2 {
		t.Errorf("entries 长度 = %d, want 2", len(entries))
	}
}

func TestOpenVikingProvider_HandleToolCall_VikingRemember(t *testing.T) {
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer server.Close()

	p := newTestVikingProvider(server)
	result, err := p.HandleToolCall(context.Background(), "viking_remember", map[string]any{
		"content":  "我喜欢Go语言",
		"category": "preference",
	})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}

	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	if parsed["status"] != "stored" {
		t.Errorf("status = %v, want stored", parsed["status"])
	}

	// 验证 [Remember — preference] 前缀
	parts, _ := receivedBody["parts"].([]any)
	if len(parts) == 0 {
		t.Fatal("parts 为空")
	}
	firstPart, _ := parts[0].(map[string]any)
	text, _ := firstPart["text"].(string)
	if !strings.Contains(text, "[Remember — preference]") {
		t.Errorf("text = %q, 应包含 '[Remember — preference]'", text)
	}
}

func TestOpenVikingProvider_HandleToolCall_VikingAddResource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{"root_uri": "viking://resource1"},
		})
	}))
	defer server.Close()

	p := newTestVikingProvider(server)
	result, err := p.HandleToolCall(context.Background(), "viking_add_resource", map[string]any{
		"url": "https://example.com/doc",
	})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}

	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	if parsed["status"] != "added" {
		t.Errorf("status = %v, want added", parsed["status"])
	}
	if parsed["root_uri"] != "viking://resource1" {
		t.Errorf("root_uri = %v, want viking://resource1", parsed["root_uri"])
	}
}

func TestOpenVikingProvider_HandleToolCall_未知工具(t *testing.T) {
	p := NewOpenVikingProvider("http://localhost", "", "", "", "")
	p.initialized = true
	p.client = newVikingClient("http://localhost", "", "a", "u", "g")

	result, err := p.HandleToolCall(context.Background(), "unknown_tool", map[string]any{})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}

	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	errMsg, _ := parsed["error"].(string)
	if !strings.Contains(errMsg, "Unknown tool") {
		t.Errorf("error = %q, 应提及 Unknown tool", errMsg)
	}
}

func TestOpenVikingProvider_HandleToolCall_无Client(t *testing.T) {
	p := NewOpenVikingProvider("http://localhost", "", "", "", "")

	result, err := p.HandleToolCall(context.Background(), "viking_search", map[string]any{"query": "test"})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}

	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	if parsed["error"] != "OpenViking not connected" {
		t.Errorf("error = %v, want 'OpenViking not connected'", parsed["error"])
	}
}

func TestOpenVikingProvider_OnSessionEnd_成功(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/commit") {
			called = true
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer server.Close()

	p := newTestVikingProvider(server)
	err := p.OnSessionEnd(context.Background(), nil)
	if err != nil {
		t.Fatalf("OnSessionEnd() error = %v", err)
	}
	if !called {
		t.Error("commit API 未被调用")
	}
}

func TestOpenVikingProvider_OnSessionEnd_无Client(t *testing.T) {
	p := NewOpenVikingProvider("http://localhost", "", "", "", "")
	err := p.OnSessionEnd(context.Background(), nil)
	if err != nil {
		t.Fatalf("OnSessionEnd() error = %v", err)
	}
}

func TestOpenVikingProvider_OnSessionEnd_无SessionID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer server.Close()

	p := newTestVikingProvider(server)
	p.sessionID = ""
	err := p.OnSessionEnd(context.Background(), nil)
	if err != nil {
		t.Fatalf("OnSessionEnd() error = %v", err)
	}
}

func TestOpenVikingProvider_Shutdown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer server.Close()

	p := newTestVikingProvider(server)
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

func TestOpenVikingProvider_接口满足(t *testing.T) {
	// 编译时验证 OpenVikingProvider 满足 MemoryProvider 接口
	var _ MemoryProvider = (*OpenVikingProvider)(nil)
}
```

- [ ] **Step 2: 运行测试验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/memory/external/ -run "TestOpenViking|TestViking" -v`
Expected: 全部 PASS

- [ ] **Step 3: 运行全量包测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test -cover ./internal/agentcore/memory/external/`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/memory/external/viking_provider_test.go
git commit -m "test(memory): 添加 OpenVikingProvider 测试 (7.15 步骤5)"
```

---

### Task 6: doc.go 更新

**Files:**
- Modify: `internal/agentcore/memory/external/doc.go`

- [ ] **Step 1: 更新 doc.go 文件目录**

将文件目录从：

```
//	external/
//	├── doc.go              # 包文档
//	├── provider.go         # MemoryProvider 接口 + BaseMemoryProvider + ToolSchema + ProviderOption
//	├── mem0_client.go      # Mem0 HTTP 客户端（REST API 封装：search/getAll/add）
//	└── mem0_provider.go    # Mem0Provider — MemoryProvider 的 Mem0 实现（熔断器+工具调用）
```

改为：

```
//	external/
//	├── doc.go              # 包文档
//	├── provider.go         # MemoryProvider 接口 + BaseMemoryProvider + ToolSchema + ProviderOption
//	├── mem0_client.go      # Mem0 HTTP 客户端（REST API 封装：search/getAll/add）
//	├── mem0_provider.go    # Mem0Provider — MemoryProvider 的 Mem0 实现（熔断器+工具调用）
//	├── viking_client.go    # OpenViking HTTP 客户端（REST API 封装：health/post/get/close + 身份 Header）
//	└── viking_provider.go  # OpenVikingProvider — MemoryProvider 的 OpenViking 实现（5 工具+会话管理）
```

- [ ] **Step 2: 提交**

```bash
git add internal/agentcore/memory/external/doc.go
git commit -m "docs(memory): 更新 external/doc.go 文件目录 (7.15)"
```

---

### Task 7: IMPLEMENTATION_PLAN.md 更新

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 7.15 行**

将：

```
| 7.15 | ☐ | OpenVikingProvider | OpenViking 适配 | `openjiuwen/core/memory/external/openviking_memory_provider.py` |
```

改为：

```
| 7.15 | ✅ | OpenVikingProvider | ✅ vikingClient（health/post/get/close + 身份 Header + 30s 超时）+ ✅ OpenVikingProvider（5 工具 Schema + Initialize 健康检查 + Prefetch + SyncTurn + HandleToolCall 5 分支 + OnSessionEnd commit + Shutdown）；环境变量 fallback；无内建熔断器；日志同步 7 点；测试覆盖率 ≥ 85% | `openjiuwen/core/memory/external/openviking_memory_provider.py` |
```

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 7.15 OpenVikingProvider 实现状态 ✅"
```

---

## 自审清单

1. **Spec 覆盖**: 逐条检查设计文档 — vikingClient 4 方法 ✅, 5 个 Schema ✅, 9 个接口方法 ✅, 5 个 handleXxx ✅, 环境变量 fallback ✅, health check ✅, 无内建熔断器 ✅, 日志 7 点 ✅, doc.go ✅, IMPLEMENTATION_PLAN.md ✅
2. **占位符扫描**: 无 TBD/TODO
3. **类型一致性**: `vikingClient` 在 client 和 provider 中使用一致；`ToolSchema` 与 provider.go 定义一致；`ProviderOption` / `applyOptions` 与 provider.go 一致；`floatVal` / `strVal` / `roundTo3` / `truncStr` 在 provider 和测试中一致
