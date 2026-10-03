# AgentArtsMemoryProvider 7.17 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 AgentArtsProvider，适配华为云 AgentArts 记忆服务，对齐 Python `agentarts_memory_provider.py`

**Architecture:** 自封装 HTTP REST 客户端对接 AgentArts Data Plane API（3 个端点：search_memories / create_memory_session / add_messages），实现 MemoryProvider 接口。通过 `kv.BaseKVStore` 管理 session 映射，仅跟踪 consecutiveFailures 计数无正式熔断器。

**Tech Stack:** Go 标准库 `net/http` + `httptest` + `kv.BaseKVStore` + `encoding/json`

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 创建 | `internal/agentcore/memory/external/agentarts_client.go` | HTTP 客户端 + 请求/响应结构体 |
| 创建 | `internal/agentcore/memory/external/agentarts_provider.go` | AgentArtsProvider 实现 MemoryProvider |
| 创建 | `internal/agentcore/memory/external/agentarts_client_test.go` | 客户端单元测试 |
| 创建 | `internal/agentcore/memory/external/agentarts_provider_test.go` | Provider 单元测试 |
| 修改 | `internal/agentcore/memory/external/doc.go` | 文件目录添加条目 |
| 修改 | `internal/swarm/server/adapter/deep_adapter_rails.go` | 工厂添加 agentarts 分支 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 7.17 状态 ☐ → ✅ |

---

### Task 1: agentarts_client.go — 结构体与常量

**Files:**
- Create: `internal/agentcore/memory/external/agentarts_client.go`

- [ ] **Step 1: 创建客户端文件，写入结构体和常量**

```go
package external

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// agentartsClient AgentArts Data Plane HTTP 客户端。
// 对齐 Python: agentarts.sdk.memory.MemoryClient（自封装 REST 替代 SDK 依赖）
type agentartsClient struct {
	// baseURL Data Plane 基础 URL
	baseURL string
	// apiKey Bearer token
	apiKey string
	// httpClient HTTP 客户端
	httpClient *http.Client
}

// memorySearchFilter 记忆搜索过滤条件。
// 对齐 Python: MemorySearchFilter (agentarts/sdk/memory/inner/config.py)
type memorySearchFilter struct {
	Query        string  `json:"query"`
	TopK         int     `json:"top_k,omitempty"`
	MinScore     float64 `json:"min_score,omitempty"`
	StrategyType string  `json:"strategy_type,omitempty"`
	ActorID      string  `json:"actor_id,omitempty"`
	AssistantID  string  `json:"assistant_id,omitempty"`
	SessionID    string  `json:"session_id,omitempty"`
}

// memorySearchResponse 记忆搜索响应。
type memorySearchResponse struct {
	Records []memorySearchRecord `json:"records"`
	Total   int                  `json:"total"`
	Query   string               `json:"query"`
}

// memorySearchRecord 单条搜索结果。
type memorySearchRecord struct {
	Record struct {
		ID           string `json:"id"`
		Content      string `json:"content"`
		StrategyType string `json:"strategy_type"`
	} `json:"record"`
	Score float64 `json:"score"`
}

// sessionCreateRequest 创建会话请求。
type sessionCreateRequest struct {
	ActorID     string `json:"actor_id,omitempty"`
	AssistantID string `json:"assistant_id,omitempty"`
}

// sessionInfo 会话信息响应。
type sessionInfo struct {
	ID          string `json:"id"`
	SpaceID     string `json:"space_id"`
	ActorID     string `json:"actor_id"`
	AssistantID string `json:"assistant_id"`
}

// textMessagePart 文本消息部分。
type textMessagePart struct {
	Type string `json:"type"` // "text"
	Text string `json:"text"`
}

// textMessage 文本消息。
// 对齐 Python: TextMessage.to_dict() — wire 格式使用 parts 数组
type textMessage struct {
	Role        string          `json:"role"` // "user"/"assistant"/"system"
	Parts       []textMessagePart `json:"parts"`
	ActorID     string          `json:"actor_id,omitempty"`
	AssistantID string          `json:"assistant_id,omitempty"`
}

// addMessagesRequest 添加消息请求。
type addMessagesRequest struct {
	Messages []textMessage `json:"messages"`
}

// normalizedMemoryItem 归一化记忆条目（Provider 内部使用）。
// 对齐 Python: {"memory": content, "score": score}
type normalizedMemoryItem struct {
	Memory string  `json:"memory"`
	Score  float64 `json:"score"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// agentartsDefaultBaseURL 默认 Data Plane 基础 URL
	agentartsDefaultBaseURL = "https://memory.cn-southwest-2.huaweicloud-agentarts.com"
	// agentartsHTTPTimeout HTTP 请求超时
	agentartsHTTPTimeout = 60 * time.Second
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newAgentArtsClient 创建 AgentArts HTTP 客户端。
// 对齐 Python: _get_client() 中 MemoryClient(api_key=...) 的初始化
func newAgentArtsClient(baseURL, apiKey string) *agentartsClient {
	if baseURL == "" {
		baseURL = agentartsDefaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	return &agentartsClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: agentartsHTTPTimeout,
		},
	}
}

// doRequest 执行 HTTP 请求并解析 JSON 响应。
func (c *agentartsClient) doRequest(ctx context.Context, method, path string, body any, result any) error {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("序列化请求体失败: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("发送请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("AgentArts API 错误: status=%d body=%s", resp.StatusCode, string(respData))
	}

	if result != nil {
		if err := json.Unmarshal(respData, result); err != nil {
			return fmt.Errorf("解析响应 JSON 失败: %w", err)
		}
	}

	return nil
}

// searchMemories 搜索记忆。
// 对齐 Python: client.search_memories(space_id, filters=...)
// REST: POST /v1/core/spaces/{space_id}/memories/search
func (c *agentartsClient) searchMemories(ctx context.Context, spaceID string, filter memorySearchFilter) ([]normalizedMemoryItem, error) {
	path := fmt.Sprintf("/v1/core/spaces/%s/memories/search", spaceID)
	var resp memorySearchResponse
	if err := c.doRequest(ctx, http.MethodPost, path, filter, &resp); err != nil {
		return nil, err
	}

	items := make([]normalizedMemoryItem, 0, len(resp.Records))
	for _, r := range resp.Records {
		if r.Record.Content != "" {
			items = append(items, normalizedMemoryItem{
				Memory: r.Record.Content,
				Score:  r.Score,
			})
		}
	}
	return items, nil
}

// createMemorySession 创建记忆会话。
// 对齐 Python: client.create_memory_session(space_id=..., actor_id=..., assistant_id=...)
// REST: POST /v1/core/spaces/{space_id}/sessions
func (c *agentartsClient) createMemorySession(ctx context.Context, spaceID string, req sessionCreateRequest) (*sessionInfo, error) {
	path := fmt.Sprintf("/v1/core/spaces/%s/sessions", spaceID)
	var info sessionInfo
	if err := c.doRequest(ctx, http.MethodPost, path, req, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// addMessages 添加消息到会话。
// 对齐 Python: client.add_messages(space_id=..., session_id=..., messages=[...])
// REST: POST /v1/core/spaces/{space_id}/sessions/{session_id}/messages
func (c *agentartsClient) addMessages(ctx context.Context, spaceID, sessionID string, msgs []textMessage) error {
	path := fmt.Sprintf("/v1/core/spaces/%s/sessions/%s/messages", spaceID, sessionID)
	return c.doRequest(ctx, http.MethodPost, path, addMessagesRequest{Messages: msgs}, nil)
}

// buildTextMessage 构建 TextMessage。
// 对齐 Python: _text_message(role, content, actor_id=..., assistant_id=...)
func buildTextMessage(role, content, actorID, assistantID string) textMessage {
	msg := textMessage{
		Role:  role,
		Parts: []textMessagePart{{Type: "text", Text: content}},
	}
	if actorID != "" {
		msg.ActorID = actorID
	}
	if assistantID != "" {
		msg.AssistantID = assistantID
	}
	return msg
}
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/external/`
Expected: 编译通过

---

### Task 2: agentarts_client_test.go — 客户端测试

**Files:**
- Create: `internal/agentcore/memory/external/agentarts_client_test.go`

- [ ] **Step 1: 创建客户端测试文件**

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

// newTestAgentArtsClient 创建使用 mock server 的 agentartsClient。
func newTestAgentArtsClient(server *httptest.Server) *agentartsClient {
	return &agentartsClient{
		baseURL:    server.URL,
		apiKey:     "test-api-key",
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func TestNewAgentArtsClient_默认URL(t *testing.T) {
	client := newAgentArtsClient("key", "")
	if client.baseURL != agentartsDefaultBaseURL {
		t.Errorf("baseURL = %q, want %q", client.baseURL, agentartsDefaultBaseURL)
	}
}

func TestNewAgentArtsClient_自定义URL(t *testing.T) {
	client := newAgentArtsClient("https://custom.example.com", "key")
	if client.baseURL != "https://custom.example.com" {
		t.Errorf("baseURL = %q, want custom URL", client.baseURL)
	}
}

func TestNewAgentArtsClient_尾部斜杠去除(t *testing.T) {
	client := newAgentArtsClient("https://custom.example.com/", "key")
	if client.baseURL != "https://custom.example.com" {
		t.Errorf("baseURL = %q, want no trailing slash", client.baseURL)
	}
}

func TestAgentArtsClient_SearchMemories_正常(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/v1/core/spaces/space1/memories/search" {
			t.Errorf("Path = %q, want /v1/core/spaces/space1/memories/search", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-api-key" {
			t.Errorf("Authorization = %q, want 'Bearer test-api-key'", r.Header.Get("Authorization"))
		}

		var filter memorySearchFilter
		json.NewDecoder(r.Body).Decode(&filter)
		if filter.Query != "测试查询" {
			t.Errorf("Query = %q, want %q", filter.Query, "测试查询")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"records": []map[string]any{
				{
					"record": map[string]any{"id": "m1", "content": "用户偏好Go语言", "strategy_type": "semantic"},
					"score":   0.95,
				},
				{
					"record": map[string]any{"id": "m2", "content": "用户使用VSCode", "strategy_type": "semantic"},
					"score":   0.8,
				},
			},
			"total": 2,
			"query": "测试查询",
		})
	}))
	defer server.Close()

	client := newTestAgentArtsClient(server)
	items, err := client.searchMemories(context.Background(), "space1", memorySearchFilter{
		Query:    "测试查询",
		TopK:     10,
		MinScore: 0.5,
	})
	if err != nil {
		t.Fatalf("searchMemories() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if items[0].Memory != "用户偏好Go语言" {
		t.Errorf("Memory = %q, want %q", items[0].Memory, "用户偏好Go语言")
	}
	if items[0].Score != 0.95 {
		t.Errorf("Score = %f, want 0.95", items[0].Score)
	}
}

func TestAgentArtsClient_SearchMemories_空结果(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"records": []any{},
			"total":   0,
			"query":   "不存在",
		})
	}))
	defer server.Close()

	client := newTestAgentArtsClient(server)
	items, err := client.searchMemories(context.Background(), "space1", memorySearchFilter{Query: "不存在"})
	if err != nil {
		t.Fatalf("searchMemories() error = %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len(items) = %d, want 0", len(items))
	}
}

func TestAgentArtsClient_SearchMemories_过滤空content(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"records": []map[string]any{
				{"record": map[string]any{"id": "m1", "content": "", "strategy_type": "semantic"}, "score": 0.9},
				{"record": map[string]any{"id": "m2", "content": "有效内容", "strategy_type": "semantic"}, "score": 0.8},
			},
			"total": 2,
		})
	}))
	defer server.Close()

	client := newTestAgentArtsClient(server)
	items, err := client.searchMemories(context.Background(), "space1", memorySearchFilter{Query: "test"})
	if err != nil {
		t.Fatalf("searchMemories() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1（空 content 应被过滤）", len(items))
	}
	if items[0].Memory != "有效内容" {
		t.Errorf("Memory = %q, want %q", items[0].Memory, "有效内容")
	}
}

func TestAgentArtsClient_CreateMemorySession_正常(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/v1/core/spaces/space1/sessions" {
			t.Errorf("Path = %q, want /v1/core/spaces/space1/sessions", r.URL.Path)
		}

		var req sessionCreateRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.ActorID != "actor1" {
			t.Errorf("ActorID = %q, want %q", req.ActorID, "actor1")
		}
		if req.AssistantID != "asst1" {
			t.Errorf("AssistantID = %q, want %q", req.AssistantID, "asst1")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":           "session-uuid-123",
			"space_id":     "space1",
			"actor_id":     "actor1",
			"assistant_id": "asst1",
		})
	}))
	defer server.Close()

	client := newTestAgentArtsClient(server)
	info, err := client.createMemorySession(context.Background(), "space1", sessionCreateRequest{
		ActorID:     "actor1",
		AssistantID: "asst1",
	})
	if err != nil {
		t.Fatalf("createMemorySession() error = %v", err)
	}
	if info.ID != "session-uuid-123" {
		t.Errorf("ID = %q, want %q", info.ID, "session-uuid-123")
	}
}

func TestAgentArtsClient_CreateMemorySession_失败(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal"}`))
	}))
	defer server.Close()

	client := newTestAgentArtsClient(server)
	_, err := client.createMemorySession(context.Background(), "space1", sessionCreateRequest{})
	if err == nil {
		t.Fatal("createMemorySession() 应返回错误")
	}
}

func TestAgentArtsClient_AddMessages_正常(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Method = %q, want POST", r.Method)
		}
		expectedPath := "/v1/core/spaces/space1/sessions/sess1/messages"
		if r.URL.Path != expectedPath {
			t.Errorf("Path = %q, want %q", r.URL.Path, expectedPath)
		}

		var req addMessagesRequest
		json.NewDecoder(r.Body).Decode(&req)
		if len(req.Messages) != 2 {
			t.Errorf("len(Messages) = %d, want 2", len(req.Messages))
		}
		if req.Messages[0].Role != "user" {
			t.Errorf("Messages[0].Role = %q, want %q", req.Messages[0].Role, "user")
		}
		if len(req.Messages[0].Parts) != 1 || req.Messages[0].Parts[0].Type != "text" {
			t.Errorf("Messages[0].Parts 格式不正确")
		}
		if req.Messages[0].Parts[0].Text != "你好" {
			t.Errorf("Messages[0].Parts[0].Text = %q, want %q", req.Messages[0].Parts[0].Text, "你好")
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"messages": []any{}})
	}))
	defer server.Close()

	client := newTestAgentArtsClient(server)
	msgs := []textMessage{
		buildTextMessage("user", "你好", "actor1", "asst1"),
		buildTextMessage("assistant", "你好！", "actor1", "asst1"),
	}
	err := client.addMessages(context.Background(), "space1", "sess1", msgs)
	if err != nil {
		t.Fatalf("addMessages() error = %v", err)
	}
}

func TestAgentArtsClient_AddMessages_失败(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error": "session not found"}`))
	}))
	defer server.Close()

	client := newTestAgentArtsClient(server)
	err := client.addMessages(context.Background(), "space1", "invalid-session", nil)
	if err == nil {
		t.Fatal("addMessages() 应返回错误")
	}
}

func TestAgentArtsClient_认证失败(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "invalid api key"}`))
	}))
	defer server.Close()

	client := newTestAgentArtsClient(server)
	_, err := client.searchMemories(context.Background(), "space1", memorySearchFilter{Query: "test"})
	if err == nil {
		t.Fatal("searchMemories() 应返回认证错误")
	}
}

func TestAgentArtsClient_请求超时(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(3 * time.Second)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	client := newTestAgentArtsClient(server)
	_, err := client.searchMemories(ctx, "space1", memorySearchFilter{Query: "test"})
	if err == nil {
		t.Fatal("searchMemories() 应返回超时错误")
	}
}

func TestBuildTextMessage_无actor(t *testing.T) {
	msg := buildTextMessage("user", "hello", "", "")
	if msg.Role != "user" {
		t.Errorf("Role = %q, want %q", msg.Role, "user")
	}
	if len(msg.Parts) != 1 || msg.Parts[0].Text != "hello" {
		t.Errorf("Parts 格式不正确")
	}
	if msg.ActorID != "" {
		t.Errorf("ActorID 应为空，got %q", msg.ActorID)
	}
	if msg.AssistantID != "" {
		t.Errorf("AssistantID 应为空，got %q", msg.AssistantID)
	}
}

func TestBuildTextMessage_带actor(t *testing.T) {
	msg := buildTextMessage("assistant", "hi", "actor1", "asst1")
	if msg.ActorID != "actor1" {
		t.Errorf("ActorID = %q, want %q", msg.ActorID, "actor1")
	}
	if msg.AssistantID != "asst1" {
		t.Errorf("AssistantID = %q, want %q", msg.AssistantID, "asst1")
	}
}
```

- [ ] **Step 2: 运行客户端测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/external/ -run "TestNewAgentArts|TestAgentArtsClient|TestBuildTextMessage" -v`
Expected: 全部 PASS

---

### Task 3: agentarts_provider.go — Provider 结构体与构造函数

**Files:**
- Create: `internal/agentcore/memory/external/agentarts_provider.go`

- [ ] **Step 1: 创建 Provider 文件，写入结构体、常量、全局变量、构造函数和基本属性方法**

```go
package external

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentArtsProvider 华为云 AgentArts 记忆服务适配器。
// 对齐 Python: AgentArtsMemoryProvider (agentarts_memory_provider.py)
//
// 将 AgentArts Data Plane REST API 适配为 MemoryProvider 接口。
// 仅跟踪 consecutiveFailures 计数，无内建熔断器（对齐 Python 语义），
// 靠外层 ExternalMemoryRail 统一熔断。
type AgentArtsProvider struct {
	BaseMemoryProvider
	// baseURL Data Plane 基础 URL
	baseURL string
	// apiKey Bearer token
	apiKey string
	// spaceID AgentArts Space 标识
	spaceID string
	// defaultActorID 构造时传入的默认 actor_id
	defaultActorID string
	// actorID 运行时 actor_id（Initialize 可覆盖）
	actorID string
	// defaultAssistantID 构造时传入的默认 assistant_id
	defaultAssistantID string
	// assistantID 运行时 assistant_id（Initialize 可覆盖）
	assistantID string
	// client 懒加载 HTTP 客户端
	client *agentartsClient
	// clientOnce 客户端初始化守卫
	clientOnce sync.Once
	// sessionID 当前会话标识
	sessionID string
	// sessionMappingStore session_id → memory_session_id 映射存储
	sessionMappingStore kv.BaseKVStore
	// initialized 是否已初始化
	initialized bool
	// mu 保护并发访问
	mu sync.RWMutex
	// consecutiveFailures 连续失败计数（对齐 Python _consecutive_failures）
	consecutiveFailures int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sessionMappingKeyPrefix session 映射 KV 前缀
	// 对齐 Python: _SESSION_MAPPING_KEY_PREFIX
	sessionMappingKeyPrefix = "agentarts/session_mapping"
	// maxTopK 搜索最大返回数
	// 对齐 Python: _MAX_TOP_K = 100
	maxTopK = 100
	// defaultTopK 搜索默认返回数
	// 对齐 Python: _DEFAULT_TOP_K = 10
	defaultTopK = 10
	// defaultMinScore 搜索默认最低相似度
	// 对齐 Python: _DEFAULT_MIN_SCORE = 0.5
	defaultMinScore = 0.5
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// agentartsLogComponent 日志组件标识
	agentartsLogComponent = logger.ComponentAgentCore

	// externalMemorySearchSchema external_memory_search 工具 Schema
	// 对齐 Python: EXTERNAL_MEMORY_SEARCH_SCHEMA（提示词从 Python 源码直接复制）
	externalMemorySearchSchema = ToolSchema{
		Name: "external_memory_search",
		Description: "Search long-term external memory for durable facts, user preferences, " +
			"and prior conversation context.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Memory search query.",
				},
				"top_k": map[string]any{
					"type":        "integer",
					"description": "Max results, default 10, max 100.",
				},
				"strategy_type": map[string]any{
					"type":        "string",
					"enum":        []string{"semantic", "summary", "user_preference", "episodic", "event", "custom"},
					"description": "Optional memory strategy type filter. Acceptable values: semantic, summary, user_preference, episodic, event, custom.",
				},
				"min_score": map[string]any{
					"type":        "number",
					"description": "Optional minimum similarity score. (default: 0.5)",
				},
			},
			"required": []string{"query"},
		},
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentArtsProvider 创建 AgentArtsProvider 实例。
// 对齐 Python: AgentArtsMemoryProvider.__init__(base_url=..., api_key=..., space_id=...,
//
//	actor_id=..., assistant_id=..., session_mapping_store=...)
//
// sessionMappingStore 为 nil 时默认使用 kv.NewInMemoryKVStore()。
func NewAgentArtsProvider(baseURL, apiKey, spaceID, actorID, assistantID string, sessionMappingStore kv.BaseKVStore) *AgentArtsProvider {
	if sessionMappingStore == nil {
		sessionMappingStore = kv.NewInMemoryKVStore()
	}
	baseURL = (baseURL)
	return &AgentArtsProvider{
		baseURL:              baseURL,
		apiKey:               apiKey,
		spaceID:              spaceID,
		defaultActorID:       actorID,
		actorID:              actorID,
		defaultAssistantID:   assistantID,
		assistantID:          assistantID,
		sessionMappingStore:  sessionMappingStore,
	}
}

// Name 返回 Provider 唯一名称。
// Python: @property name -> "agentarts"
func (p *AgentArtsProvider) Name() string {
	return "agentarts"
}

// IsAvailable 检查 Provider 是否已配置且就绪（无网络调用）。
// Python: is_available() -> bool(self._api_key and self._space_id)
func (p *AgentArtsProvider) IsAvailable() bool {
	return p.apiKey != "" && p.spaceID != ""
}

// IsInitialized 返回 Provider 是否已初始化。
// Python: @property is_initialized -> self._initialized
func (p *AgentArtsProvider) IsInitialized() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.initialized
}

// GetToolSchemas 返回 Provider 提供的工具 Schema 列表。
// Python: get_tool_schemas() -> [EXTERNAL_MEMORY_SEARCH_SCHEMA]
func (p *AgentArtsProvider) GetToolSchemas() []ToolSchema {
	return []ToolSchema{externalMemorySearchSchema}
}

// SystemPromptBlock 返回 Provider 的系统提示词引导块。
// Python: system_prompt_block() -> "# External Memory\nUse `external_memory_search`..."
func (p *AgentArtsProvider) SystemPromptBlock() string {
	return "# External Memory\n" +
		"Use `external_memory_search` to retrieve durable facts, user preferences, " +
		"and prior conversation context from long-term external memory."
}
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/external/`
Expected: 编译通过

---

### Task 4: agentarts_provider.go — Initialize / Shutdown / getClient

**Files:**
- Modify: `internal/agentcore/memory/external/agentarts_provider.go`

- [ ] **Step 1: 在 `agentarts_provider.go` 导出函数区块末尾追加 Initialize / Shutdown / getClient 方法**

在 `SystemPromptBlock()` 方法后追加：

```go
// Initialize 初始化 Provider。
// 对齐 Python: async def initialize(self, **kwargs) -> None
func (p *AgentArtsProvider) Initialize(ctx context.Context, opts ...ProviderOption) error {
	po := applyOptions(opts...)

	// 对齐 Python: kwargs.get("user_id") → self._actor_id
	if po.UserID != "" {
		p.actorID = po.UserID
	}
	// 对齐 Python: kwargs.get("assistant_id") or kwargs.get("scope_id") → self._assistant_id
	if po.ScopeID != "" {
		p.assistantID = po.ScopeID
	}
	sessionID := po.SessionID

	logger.Info(agentartsLogComponent).
		Str("provider", "agentarts").
		Str("user_id", p.actorID).
		Str("assistant_id", p.assistantID).
		Str("session_id", sessionID).
		Msg("[AgentArtsProvider] initializing")

	// 对齐 Python: await self._ensure_memory_session(session_id, ...)
	if sessionID != "" {
		if _, err := p.ensureMemorySession(ctx, sessionID, p.actorID, p.assistantID); err != nil {
			logger.Warn(agentartsLogComponent).Err(err).Msg("[AgentArtsProvider] ensureMemorySession 失败")
			// 对齐 Python: 仅 warn，继续运行
		}
	}

	p.mu.Lock()
	p.sessionID = sessionID
	p.initialized = true
	p.mu.Unlock()

	return nil
}

// Shutdown 关闭 Provider 释放资源。
// 对齐 Python: async def shutdown(self) -> None
func (p *AgentArtsProvider) Shutdown(_ context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clientOnce = sync.Once{}
	p.client = nil
	p.initialized = false
	return nil
}
```

在非导出函数区块追加：

```go
// getClient 延迟初始化并返回 AgentArts HTTP 客户端。
// 对齐 Python: _get_client() — 懒加载模式
func (p *AgentArtsProvider) getClient() *agentartsClient {
	p.clientOnce.Do(func() {
		if p.client == nil {
			p.client = newAgentArtsClient(p.baseURL, p.apiKey)
		}
	})
	return p.client
}
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/external/`
Expected: 编译通过

---

### Task 5: agentarts_provider.go — session 映射与搜索核心方法

**Files:**
- Modify: `internal/agentcore/memory/external/agentarts_provider.go`

- [ ] **Step 1: 在非导出函数区块追加 session 映射与搜索方法**

```go
// ensureMemorySession 确保记忆会话存在，返回 memory_session_id。
// 对齐 Python: _ensure_memory_session(session_id, actor_id=..., assistant_id=...)
func (p *AgentArtsProvider) ensureMemorySession(ctx context.Context, sessionID, actorID, assistantID string) (string, error) {
	if sessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}

	// 对齐 Python: actor_id = actor_id or self._actor_id or self._default_actor_id
	if actorID == "" {
		actorID = p.actorID
	}
	if actorID == "" {
		actorID = p.defaultActorID
	}
	// 对齐 Python: assistant_id = assistant_id or self._assistant_id or self._default_assistant_id
	if assistantID == "" {
		assistantID = p.assistantID
	}
	if assistantID == "" {
		assistantID = p.defaultAssistantID
	}

	mappingKey := p.sessionMappingKey(sessionID)

	// 对齐 Python: memory_session_id = await self._session_mapping_store.get(mapping_key)
	existing, err := p.sessionMappingStore.Get(ctx, mappingKey)
	if err != nil {
		logger.Debug(agentartsLogComponent).Err(err).Str("key", mappingKey).Msg("[AgentArtsProvider] 读取 session 映射失败")
	}
	if len(existing) > 0 {
		memorySessionID := string(existing)
		logger.Info(agentartsLogComponent).
			Str("session_id", sessionID).
			Str("memory_session_id", memorySessionID).
			Msg("[AgentArtsProvider] use exist session mapping entry")
		return memorySessionID, nil
	}

	// 对齐 Python: client.create_memory_session(space_id=..., actor_id=..., assistant_id=...)
	client := p.getClient()
	req := sessionCreateRequest{
		ActorID:     actorID,
		AssistantID: assistantID,
	}
	info, err := client.createMemorySession(ctx, p.spaceID, req)
	if err != nil {
		return "", fmt.Errorf("create_memory_session 失败: %w", err)
	}
	if info.ID == "" {
		return "", fmt.Errorf("AgentArts create_memory_session did not return a session id")
	}

	logger.Info(agentartsLogComponent).
		Str("session_id", sessionID).
		Str("memory_session_id", info.ID).
		Msg("[AgentArtsProvider] add session mapping entry")

	// 对齐 Python: await self._session_mapping_store.set(mapping_key, memory_session_id)
	if err := p.sessionMappingStore.Set(ctx, mappingKey, []byte(info.ID)); err != nil {
		logger.Warn(agentartsLogComponent).Err(err).Msg("[AgentArtsProvider] 存储 session 映射失败")
	}

	return info.ID, nil
}

// search 执行记忆搜索并返回归一化结果。
// 对齐 Python: _search(query, args) -> list[dict]
func (p *AgentArtsProvider) search(ctx context.Context, query string, args map[string]any) ([]normalizedMemoryItem, error) {
	client := p.getClient()

	// 对齐 Python: _memory_search_filter(query, args)
	filter := memorySearchFilter{
		Query: query,
	}

	// 对齐 Python: actor_id = self._runtime_actor_id(args)
	actorID := p.runtimeActorID(args)
	if actorID != "" {
		filter.ActorID = actorID
	}

	// 对齐 Python: top_k = args.get("top_k") or _DEFAULT_TOP_K
	topK := defaultTopK
	if v, ok := args["top_k"]; ok {
		switch n := v.(type) {
		case float64:
			topK = int(n)
		case int:
			topK = n
		}
	}
	if topK > maxTopK {
		topK = maxTopK
	}
	filter.TopK = topK

	// 对齐 Python: min_score = _DEFAULT_MIN_SCORE if args.get("min_score") is None else args["min_score"]
	minScore := defaultMinScore
	if v, ok := args["min_score"]; ok {
		switch n := v.(type) {
		case float64:
			minScore = n
		case int:
			minScore = float64(n)
		}
	}
	filter.MinScore = minScore

	// 对齐 Python: if args.get("strategy_type") is not None
	if v, ok := args["strategy_type"].(string); ok && v != "" {
		filter.StrategyType = v
	}

	logger.Info(agentartsLogComponent).
		Str("space_id", p.spaceID).
		Str("query", query).
		Int("top_k", topK).
		Float64("min_score", minScore).
		Str("strategy_type", filter.StrategyType).
		Msg("[AgentArtsProvider] search agentarts memory")

	items, err := client.searchMemories(ctx, p.spaceID, filter)
	if err != nil {
		return nil, err
	}

	logger.Info(agentartsLogComponent).
		Int("count", len(items)).
		Msg("[AgentArtsProvider] found relevant memory records")

	return items, nil
}

// runtimeActorID 解析运行时 actor_id。
// 对齐 Python: _runtime_actor_id(params) — 优先 params.user_id → self._actor_id → self._default_actor_id
func (p *AgentArtsProvider) runtimeActorID(args map[string]any) string {
	if v, ok := args["user_id"].(string); ok && v != "" {
		return v
	}
	p.mu.RLock()
	actorID := p.actorID
	p.mu.RUnlock()
	if actorID != "" {
		return actorID
	}
	return p.defaultActorID
}

// runtimeAssistantID 解析运行时 assistant_id。
// 对齐 Python: _runtime_assistant_id(params) — 优先 params.assistant_id → params.scope_id → self._assistant_id → self._default_assistant_id
func (p *AgentArtsProvider) runtimeAssistantID(args map[string]any) string {
	if v, ok := args["assistant_id"].(string); ok && v != "" {
		return v
	}
	if v, ok := args["scope_id"].(string); ok && v != "" {
		return v
	}
	p.mu.RLock()
	assistantID := p.assistantID
	p.mu.RUnlock()
	if assistantID != "" {
		return assistantID
	}
	return p.defaultAssistantID
}

// sessionMappingKey 生成 session 映射 KV key。
// 对齐 Python: _session_mapping_key(session_id) -> f"{_SESSION_MAPPING_KEY_PREFIX}/{session_id}"
func (p *AgentArtsProvider) sessionMappingKey(sessionID string) string {
	return sessionMappingKeyPrefix + "/" + sessionID
}

// recordSuccess 记录成功，重置连续失败计数。
// 对齐 Python: self._consecutive_failures = 0
func (p *AgentArtsProvider) recordSuccess() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.consecutiveFailures = 0
}

// recordFailure 记录失败，递增连续失败计数。
// 对齐 Python: self._consecutive_failures += 1
func (p *AgentArtsProvider) recordFailure() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.consecutiveFailures++
}
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/external/`
Expected: 编译通过

---

### Task 6: agentarts_provider.go — Prefetch / HandleToolCall / SyncTurn

**Files:**
- Modify: `internal/agentcore/memory/external/agentarts_provider.go`

- [ ] **Step 1: 在导出函数区块追加 Prefetch / HandleToolCall / SyncTurn 方法**

在 `Initialize` 方法后、`Shutdown` 方法前追加：

```go
// Prefetch 根据查询预取记忆上下文。
// 对齐 Python: async def prefetch(self, query, **kwargs) -> str
func (p *AgentArtsProvider) Prefetch(ctx context.Context, query string, _ ...ProviderOption) (string, error) {
	if query == "" {
		return "", nil
	}

	items, err := p.search(ctx, query, map[string]any{})
	if err != nil {
		p.recordFailure()
		logger.Debug(agentartsLogComponent).Err(err).Msg("[AgentArtsProvider] prefetch failed")
		return "", nil
	}

	p.recordSuccess()

	if len(items) == 0 {
		return "", nil
	}

	lines := make([]string, 0, len(items))
	for _, item := range items {
		if item.Memory != "" {
			lines = append(lines, "- "+item.Memory)
		}
	}
	if len(lines) == 0 {
		return "", nil
	}
	return "## External Memory\n" + joinLines(lines), nil
}

// HandleToolCall 处理工具调用并返回结果字符串。
// 对齐 Python: async def handle_tool_call(self, tool_name, args) -> str
func (p *AgentArtsProvider) HandleToolCall(ctx context.Context, toolName string, args map[string]any) (string, error) {
	// 对齐 Python: tool_name != "external_memory_search" → {"error": "Unknown tool: ..."}
	if toolName != "external_memory_search" {
		b, _ := json.Marshal(map[string]string{"error": fmt.Sprintf("Unknown tool: %s", toolName)})
		return string(b), nil
	}

	query, _ := args["query"].(string)
	if query == "" {
		b, _ := json.Marshal(map[string]string{"error": "Missing required parameter: query"})
		return string(b), nil
	}

	items, err := p.search(ctx, query, args)
	if err != nil {
		p.recordFailure()
		logger.Warn(agentartsLogComponent).Err(err).Msg("[AgentArtsProvider] failed to search relevant memories")
		b, _ := json.Marshal(map[string]string{"error": err.Error()})
		return string(b), nil
	}

	p.recordSuccess()

	if len(items) == 0 {
		b, _ := json.Marshal(map[string]any{"result": "No relevant memories found.", "count": 0})
		return string(b), nil
	}

	payload := make([]map[string]any, len(items))
	for i, item := range items {
		payload[i] = map[string]any{"memory": item.Memory, "score": item.Score}
	}
	b, _ := json.Marshal(map[string]any{"results": payload, "count": len(payload)})
	return string(b), nil
}

// SyncTurn 同步一轮对话到外部记忆。
// 对齐 Python: async def sync_turn(self, user_msg, assistant_msg, **kwargs) -> None
// 错误被静默吞掉（对齐 Python: except Exception → logger.warning + self._consecutive_failures += 1）
func (p *AgentArtsProvider) SyncTurn(ctx context.Context, userMsg, assistantMsg string, opts ...ProviderOption) error {
	if userMsg == "" || assistantMsg == "" {
		return nil
	}

	po := applyOptions(opts...)
	actorID := po.UserID
	if actorID == "" {
		p.mu.RLock()
		actorID = p.actorID
		p.mu.RUnlock()
	}
	assistantID := po.ScopeID
	if assistantID == "" {
		p.mu.RLock()
		assistantID = p.assistantID
		p.mu.RUnlock()
	}

	sessionID := po.SessionID
	if sessionID == "" {
		p.mu.RLock()
		sessionID = p.sessionID
		p.mu.RUnlock()
	}

	// 对齐 Python: await self._ensure_memory_session(kwargs.get("session_id"), ...)
	memorySessionID, err := p.ensureMemorySession(ctx, sessionID, actorID, assistantID)
	if err != nil {
		p.recordFailure()
		logger.Warn(agentartsLogComponent).Err(err).Msg("[AgentArtsProvider] SyncTurn ensureMemorySession 失败")
		return nil // 对齐 Python: 吞掉错误
	}

	client := p.getClient()
	msgs := []textMessage{
		buildTextMessage("user", userMsg, actorID, assistantID),
		buildTextMessage("assistant", assistantMsg, actorID, assistantID),
	}
	if err := client.addMessages(ctx, p.spaceID, memorySessionID, msgs); err != nil {
		p.recordFailure()
		logger.Warn(agentartsLogComponent).Err(err).Msg("[AgentArtsProvider] AgentArts sync failed")
		return nil // 对齐 Python: 吞掉错误
	}

	p.recordSuccess()
	return nil
}
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/external/`
Expected: 编译通过

---

### Task 7: agentarts_provider_test.go — Provider 测试

**Files:**
- Create: `internal/agentcore/memory/external/agentarts_provider_test.go`

- [ ] **Step 1: 创建 Provider 测试文件**

```go
package external

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestAgentArtsProvider 创建使用 mock server 的 AgentArtsProvider
func newTestAgentArtsProvider(server *httptest.Server) *AgentArtsProvider {
	p := NewAgentArtsProvider(server.URL, "test-key", "test-space", "default-actor", "default-asst", nil)
	p.client = newAgentArtsClient(server.URL, "test-key")
	p.clientOnce = func() {} // 禁用 sync.Once 以注入 client
	return p
}

func TestNewAgentArtsProvider_默认KVStore(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)
	if p.sessionMappingStore == nil {
		t.Error("sessionMappingStore 不应为 nil")
	}
}

func TestNewAgentArtsProvider_自定义KVStore(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	p := NewAgentArtsProvider("", "key", "space", "", "", store)
	if p.sessionMappingStore != store {
		t.Error("sessionMappingStore 应为自定义 store")
	}
}

func TestAgentArtsProvider_Name(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)
	if p.Name() != "agentarts" {
		t.Errorf("Name() = %q, want %q", p.Name(), "agentarts")
	}
}

func TestAgentArtsProvider_IsAvailable_有key有space(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)
	if !p.IsAvailable() {
		t.Error("IsAvailable() 应返回 true")
	}
}

func TestAgentArtsProvider_IsAvailable_无key(t *testing.T) {
	p := NewAgentArtsProvider("", "", "space", "", "", nil)
	if p.IsAvailable() {
		t.Error("IsAvailable() 应返回 false（无 api_key）")
	}
}

func TestAgentArtsProvider_IsAvailable_无space(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "", "", "", nil)
	if p.IsAvailable() {
		t.Error("IsAvailable() 应返回 false（无 space_id）")
	}
}

func TestAgentArtsProvider_IsInitialized(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)
	if p.IsInitialized() {
		t.Error("IsInitialized() 初始应为 false")
	}
}

func TestAgentArtsProvider_GetToolSchemas(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)
	schemas := p.GetToolSchemas()
	if len(schemas) != 1 {
		t.Fatalf("len(schemas) = %d, want 1", len(schemas))
	}
	if schemas[0].Name != "external_memory_search" {
		t.Errorf("Name = %q, want %q", schemas[0].Name, "external_memory_search")
	}
}

func TestAgentArtsProvider_SystemPromptBlock(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)
	block := p.SystemPromptBlock()
	if block == "" {
		t.Error("SystemPromptBlock() 不应为空")
	}
	if block[:16] != "# External Memory" {
		t.Errorf("SystemPromptBlock() 应以 '# External Memory' 开头, got %q", block[:16])
	}
}

func TestAgentArtsProvider_Initialize_正常(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// createMemorySession 响应
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":           "ms-uuid-123",
			"space_id":     "test-space",
			"actor_id":     "user1",
			"assistant_id": "scope1",
		})
	}))
	defer server.Close()

	p := NewAgentArtsProvider(server.URL, "test-key", "test-space", "default-actor", "default-asst", nil)
	p.client = newAgentArtsClient(server.URL, "test-key")

	err := p.Initialize(context.Background(),
		WithUserID("user1"),
		WithScopeID("scope1"),
		WithSessionID("sess1"),
	)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if !p.IsInitialized() {
		t.Error("Initialize() 后 IsInitialized() 应为 true")
	}
	if p.actorID != "user1" {
		t.Errorf("actorID = %q, want %q", p.actorID, "user1")
	}
	if p.assistantID != "scope1" {
		t.Errorf("assistantID = %q, want %q", p.assistantID, "scope1")
	}
}

func TestAgentArtsProvider_Initialize_无sessionID(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)
	err := p.Initialize(context.Background(), WithUserID("user1"))
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if !p.IsInitialized() {
		t.Error("Initialize() 后 IsInitialized() 应为 true")
	}
}

func TestAgentArtsProvider_Prefetch_正常(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"records": []map[string]any{
				{"record": map[string]any{"id": "m1", "content": "用户偏好中文", "strategy_type": "semantic"}, "score": 0.9},
			},
			"total": 1,
		})
	}))
	defer server.Close()

	p := newTestAgentArtsProvider(server)
	result, err := p.Prefetch(context.Background(), "用户偏好")
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	if result == "" {
		t.Fatal("Prefetch() 结果不应为空")
	}
	if result[:17] != "## External Memory" {
		t.Errorf("Prefetch() 应以 '## External Memory' 开头, got %q", result[:17])
	}
}

func TestAgentArtsProvider_Prefetch_空查询(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)
	result, err := p.Prefetch(context.Background(), "")
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	if result != "" {
		t.Errorf("Prefetch() 空查询应返回空字符串, got %q", result)
	}
}

func TestAgentArtsProvider_Prefetch_搜索失败(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	p := newTestAgentArtsProvider(server)
	result, err := p.Prefetch(context.Background(), "test")
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	if result != "" {
		t.Errorf("Prefetch() 搜索失败应返回空字符串, got %q", result)
	}
	if p.consecutiveFailures != 1 {
		t.Errorf("consecutiveFailures = %d, want 1", p.consecutiveFailures)
	}
}

func TestAgentArtsProvider_HandleToolCall_正常搜索(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"records": []map[string]any{
				{"record": map[string]any{"id": "m1", "content": "记忆内容", "strategy_type": "semantic"}, "score": 0.95},
			},
			"total": 1,
		})
	}))
	defer server.Close()

	p := newTestAgentArtsProvider(server)
	result, err := p.HandleToolCall(context.Background(), "external_memory_search", map[string]any{
		"query": "test",
	})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}

	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	if _, ok := parsed["results"]; !ok {
		t.Errorf("HandleToolCall() 结果应包含 'results', got %v", parsed)
	}
}

func TestAgentArtsProvider_HandleToolCall_未知工具(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)
	result, err := p.HandleToolCall(context.Background(), "unknown_tool", map[string]any{})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}

	var parsed map[string]string
	json.Unmarshal([]byte(result), &parsed)
	if parsed["error"] != "Unknown tool: unknown_tool" {
		t.Errorf("error = %q, want 'Unknown tool: unknown_tool'", parsed["error"])
	}
}

func TestAgentArtsProvider_HandleToolCall_缺少query(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)
	result, err := p.HandleToolCall(context.Background(), "external_memory_search", map[string]any{})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}

	var parsed map[string]string
	json.Unmarshal([]byte(result), &parsed)
	if parsed["error"] != "Missing required parameter: query" {
		t.Errorf("error = %q, want 'Missing required parameter: query'", parsed["error"])
	}
}

func TestAgentArtsProvider_HandleToolCall_搜索失败(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	p := newTestAgentArtsProvider(server)
	result, err := p.HandleToolCall(context.Background(), "external_memory_search", map[string]any{
		"query": "test",
	})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}

	var parsed map[string]string
	json.Unmarshal([]byte(result), &parsed)
	if _, ok := parsed["error"]; !ok {
		t.Errorf("搜索失败应返回 error, got %v", parsed)
	}
	if p.consecutiveFailures != 1 {
		t.Errorf("consecutiveFailures = %d, want 1", p.consecutiveFailures)
	}
}

func TestAgentArtsProvider_SyncTurn_正常(t *testing.T) {
	sessionCreated := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/core/spaces/test-space/sessions" {
			sessionCreated = true
			json.NewEncoder(w).Encode(map[string]any{
				"id": "ms-uuid-456", "space_id": "test-space",
			})
			return
		}
		// addMessages 响应
		json.NewEncoder(w).Encode(map[string]any{"messages": []any{}})
	}))
	defer server.Close()

	p := newTestAgentArtsProvider(server)
	err := p.SyncTurn(context.Background(), "你好", "你好！", WithSessionID("sess1"))
	if err != nil {
		t.Fatalf("SyncTurn() error = %v", err)
	}
	if !sessionCreated {
		t.Error("SyncTurn 应创建 memory session")
	}
	if p.consecutiveFailures != 0 {
		t.Errorf("consecutiveFailures = %d, want 0", p.consecutiveFailures)
	}
}

func TestAgentArtsProvider_SyncTurn_空消息跳过(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)
	err := p.SyncTurn(context.Background(), "", "assistant msg")
	if err != nil {
		t.Fatalf("SyncTurn() error = %v", err)
	}
	err = p.SyncTurn(context.Background(), "user msg", "")
	if err != nil {
		t.Fatalf("SyncTurn() error = %v", err)
	}
}

func TestAgentArtsProvider_SyncTurn_错误吞掉(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	p := newTestAgentArtsProvider(server)
	err := p.SyncTurn(context.Background(), "user msg", "assistant msg", WithSessionID("sess1"))
	if err != nil {
		t.Fatalf("SyncTurn() 应吞掉错误返回 nil, got %v", err)
	}
	if p.consecutiveFailures != 1 {
		t.Errorf("consecutiveFailures = %d, want 1", p.consecutiveFailures)
	}
}

func TestAgentArtsProvider_Shutdown(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)
	p.initialized = true
	p.client = newAgentArtsClient("http://example.com", "key")

	err := p.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if p.IsInitialized() {
		t.Error("Shutdown() 后 IsInitialized() 应为 false")
	}
	if p.client != nil {
		t.Error("Shutdown() 后 client 应为 nil")
	}
}

func TestAgentArtsProvider_ConsecutiveFailures_计数(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)

	p.recordFailure()
	p.recordFailure()
	if p.consecutiveFailures != 2 {
		t.Errorf("consecutiveFailures = %d, want 2", p.consecutiveFailures)
	}

	p.recordSuccess()
	if p.consecutiveFailures != 0 {
		t.Errorf("consecutiveFailures = %d, want 0 after recordSuccess", p.consecutiveFailures)
	}
}

func TestAgentArtsProvider_RuntimeActorID_优先级(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "default-actor", "", nil)
	p.actorID = "runtime-actor"

	// 1. args["user_id"] 优先
	args := map[string]any{"user_id": "args-actor"}
	if got := p.runtimeActorID(args); got != "args-actor" {
		t.Errorf("runtimeActorID() = %q, want %q", got, "args-actor")
	}

	// 2. p.actorID 其次
	if got := p.runtimeActorID(map[string]any{}); got != "runtime-actor" {
		t.Errorf("runtimeActorID() = %q, want %q", got, "runtime-actor")
	}

	// 3. p.defaultActorID 最后
	p.actorID = ""
	if got := p.runtimeActorID(map[string]any{}); got != "default-actor" {
		t.Errorf("runtimeActorID() = %q, want %q", got, "default-actor")
	}
}

func TestAgentArtsProvider_RuntimeAssistantID_优先级(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "default-asst", nil)
	p.assistantID = "runtime-asst"

	// 1. args["assistant_id"] 优先
	args := map[string]any{"assistant_id": "args-asst"}
	if got := p.runtimeAssistantID(args); got != "args-asst" {
		t.Errorf("runtimeAssistantID() = %q, want %q", got, "args-asst")
	}

	// 2. args["scope_id"] 其次
	args = map[string]any{"scope_id": "scope-asst"}
	if got := p.runtimeAssistantID(args); got != "scope-asst" {
		t.Errorf("runtimeAssistantID() = %q, want %q", got, "scope-asst")
	}

	// 3. p.assistantID 再次
	if got := p.runtimeAssistantID(map[string]any{}); got != "runtime-asst" {
		t.Errorf("runtimeAssistantID() = %q, want %q", got, "runtime-asst")
	}

	// 4. p.defaultAssistantID 最后
	p.assistantID = ""
	if got := p.runtimeAssistantID(map[string]any{}); got != "default-asst" {
		t.Errorf("runtimeAssistantID() = %q, want %q", got, "default-asst")
	}
}

func TestAgentArtsProvider_SessionMappingKey(t *testing.T) {
	p := NewAgentArtsProvider("", "key", "space", "", "", nil)
	key := p.sessionMappingKey("sess1")
	expected := "agentarts/session_mapping/sess1"
	if key != expected {
		t.Errorf("sessionMappingKey() = %q, want %q", key, expected)
	}
}
```

- [ ] **Step 2: 运行 Provider 测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/external/ -run "TestNewAgentArtsProvider|TestAgentArtsProvider" -v`
Expected: 全部 PASS

- [ ] **Step 3: 修复 clientOnce 注入问题**

注意 `newTestAgentArtsProvider` 中 `p.clientOnce = func() {}` 无法直接覆盖 `sync.Once`。需要改用另一种方式注入 client。修改 `newTestAgentArtsProvider`：

```go
func newTestAgentArtsProvider(server *httptest.Server) *AgentArtsProvider {
	p := NewAgentArtsProvider(server.URL, "test-key", "test-space", "default-actor", "default-asst", nil)
	// 直接注入 client，绕过 sync.Once
	p.clientOnce = sync.Once{}
	p.client = newAgentArtsClient(server.URL, "test-key")
	return p
}
```

需要在 `agentarts_provider_test.go` 顶部添加 import `"sync"`。

- [ ] **Step 4: 重新运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/external/ -run "TestNewAgentArtsProvider|TestAgentArtsProvider" -v`
Expected: 全部 PASS

---

### Task 8: 回填 — doc.go

**Files:**
- Modify: `internal/agentcore/memory/external/doc.go`

- [ ] **Step 1: 在 doc.go 文件目录中添加 agentarts 条目**

将文件目录从：

```
//	external/
//	├── doc.go                    # 包文档
//	├── provider.go               # MemoryProvider 接口 + BaseMemoryProvider + ToolSchema + ProviderOption
//	├── mem0_client.go            # Mem0 HTTP 客户端（REST API 封装：search/getAll/add）
//	├── mem0_provider.go          # Mem0Provider — MemoryProvider 的 Mem0 实现（熔断器+工具调用）
//	├── openjiuwen_provider.go    # OpenJiuwenProvider — MemoryProvider 的 openjiuwen LTM 实现（全局单例+双模式构造）
//	├── viking_client.go          # OpenViking HTTP 客户端（REST API 封装：health/post/get/close + 身份 Header）
//	└── viking_provider.go        # OpenVikingProvider — MemoryProvider 的 OpenViking 实现（5 工具+会话管理）
```

改为：

```
//	external/
//	├── doc.go                    # 包文档
//	├── provider.go               # MemoryProvider 接口 + BaseMemoryProvider + ToolSchema + ProviderOption
//	├── agentarts_client.go       # AgentArts HTTP 客户端（REST API 封装：searchMemories/createMemorySession/addMessages）
//	├── agentarts_provider.go     # AgentArtsProvider — MemoryProvider 的 AgentArts 实现（session 映射+consecutiveFailures 计数）
//	├── mem0_client.go            # Mem0 HTTP 客户端（REST API 封装：search/getAll/add）
//	├── mem0_provider.go          # Mem0Provider — MemoryProvider 的 Mem0 实现（熔断器+工具调用）
//	├── openjiuwen_provider.go    # OpenJiuwenProvider — MemoryProvider 的 openjiuwen LTM 实现（全局单例+双模式构造）
//	├── viking_client.go          # OpenViking HTTP 客户端（REST API 封装：health/post/get/close + 身份 Header）
//	└── viking_provider.go        # OpenVikingProvider — MemoryProvider 的 OpenViking 实现（5 工具+会话管理）
```

---

### Task 9: 回填 — deep_adapter_rails.go 工厂

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_rails.go`

- [ ] **Step 1: 在 `buildExternalMemoryProvider` switch 中添加 `case "agentarts"` 分支**

在 `case "openjiuwen"` 分支后、`default` 前插入：

```go
	case "agentarts":
		// 对齐 Python: agentarts provider 分支
		baseURL := strVal(cfg["base_url"])
		apiKey := strVal(cfg["api_key"])
		spaceID := strVal(cfg["space_id"])
		actorID := strVal(cfg["actor_id"])
		assistantID := strVal(cfg["assistant_id"])
		provider := ext.NewAgentArtsProvider(baseURL, apiKey, spaceID, actorID, assistantID, nil)
		if !provider.IsAvailable() {
			logger.Warn(logComponent).Msg("buildExternalMemoryProvider: AgentArtsProvider unavailable")
			return nil
		}
		return provider
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/adapter/`
Expected: 编译通过

---

### Task 10: 回填 — IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 7.17 状态**

将 `| 7.17 | ☐ | AgentArtsMemoryProvider | AgentArts 适配 |` 改为：

`| 7.17 | ✅ | AgentArtsMemoryProvider | ✅ AgentArtsProvider（自封装 HTTP REST 客户端 + session 映射 + consecutiveFailures 计数 + external_memory_search 工具） |`

---

### Task 11: 全量测试验证

**Files:**
- 无修改

- [ ] **Step 1: 运行 external 包全量测试**

Run: `cd /home/opensource/uapclaw-gateway && go test -cover ./internal/agentcore/memory/external/ -v`
Expected: 全部 PASS，覆盖率 ≥ 85%

- [ ] **Step 2: 运行 adapter 包编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/adapter/`
Expected: 编译通过

- [ ] **Step 3: 运行全项目编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./...`
Expected: 编译通过

---

### Task 12: 提交

- [ ] **Step 1: git commit**

```bash
cd /home/opensource/uapclaw-gateway && git add \
  internal/agentcore/memory/external/agentarts_client.go \
  internal/agentcore/memory/external/agentarts_client_test.go \
  internal/agentcore/memory/external/agentarts_provider.go \
  internal/agentcore/memory/external/agentarts_provider_test.go \
  internal/agentcore/memory/external/doc.go \
  internal/swarm/server/adapter/deep_adapter_rails.go \
  IMPLEMENTATION_PLAN.md \
  docs/superpowers/specs/2027-07-14-agentarts-provider-7.17-design.md
git commit -m "feat(memory): 实现 AgentArtsProvider 7.17 — 自封装 HTTP REST 客户端 + session 映射 + consecutiveFailures 计数"
```
