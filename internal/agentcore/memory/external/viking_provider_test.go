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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer server.Close()

	p := newTestVikingProvider(server)
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

func TestOpenVikingProvider_SyncTurn_从Opts取SessionID(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		// 验证使用了 opts 中的 sessionID
		if callCount == 1 && !strings.Contains(r.URL.Path, "opts-session") {
			t.Errorf("Path = %q, 应包含 opts-session", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer server.Close()

	p := newTestVikingProvider(server)
	p.sessionID = "default-session"
	err := p.SyncTurn(context.Background(), "用户消息", "助手回复", WithSessionID("opts-session"))
	if err != nil {
		t.Fatalf("SyncTurn() error = %v", err)
	}
	if callCount != 2 {
		t.Errorf("API 调用次数 = %d, want 2", callCount)
	}
}

func TestOpenVikingProvider_SyncTurn_API失败(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	p := newTestVikingProvider(server)
	// 对齐 Python: SyncTurn 失败时 debug 日志 + return nil
	err := p.SyncTurn(context.Background(), "用户消息", "助手回复")
	if err != nil {
		t.Fatalf("SyncTurn() error = %v, 对齐 Python 返回 nil", err)
	}
}

func TestOpenVikingProvider_HandleToolCall_VikingSearch_缺query(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer server.Close()

	p := newTestVikingProvider(server)
	result, err := p.HandleToolCall(context.Background(), "viking_search", map[string]any{})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}
	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	if !strings.Contains(parsed["error"].(string), "query is required") {
		t.Errorf("error = %v, 应提及 query is required", parsed["error"])
	}
}

func TestOpenVikingProvider_HandleToolCall_VikingSearch_带Mode(t *testing.T) {
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"memories":  []map[string]any{{"uri": "viking://m1", "score": 0.9, "abstract": "a"}},
				"resources": []map[string]any{},
				"skills":    []map[string]any{},
				"total":     1,
			},
		})
	}))
	defer server.Close()

	p := newTestVikingProvider(server)
	_, err := p.HandleToolCall(context.Background(), "viking_search", map[string]any{
		"query": "测试", "mode": "deep", "scope": "viking://scope1", "limit": 5,
	})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}
	// 验证 payload 中包含 mode, target_uri, top_k
	if receivedBody["mode"] != "deep" {
		t.Errorf("mode = %v, want deep", receivedBody["mode"])
	}
	if receivedBody["target_uri"] != "viking://scope1" {
		t.Errorf("target_uri = %v, want viking://scope1", receivedBody["target_uri"])
	}
	if receivedBody["top_k"] != float64(5) {
		t.Errorf("top_k = %v, want 5", receivedBody["top_k"])
	}
}

func TestOpenVikingProvider_HandleToolCall_VikingSearch_带Relations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"memories": []map[string]any{
					{
						"uri": "viking://m1", "score": 0.95, "abstract": "摘要1",
						"relations": []map[string]any{
							{"uri": "viking://r1"},
							{"uri": "viking://r2"},
						},
					},
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
		t.Fatalf("results 长度 = %d, want 1", len(results))
	}
	firstResult, _ := results[0].(map[string]any)
	related, _ := firstResult["related"].([]any)
	if len(related) != 2 {
		t.Errorf("related 长度 = %d, want 2", len(related))
	}
}

func TestOpenVikingProvider_HandleToolCall_VikingRead_抽象级别(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"result": "抽象内容",
		})
	}))
	defer server.Close()

	p := newTestVikingProvider(server)
	result, err := p.HandleToolCall(context.Background(), "viking_read", map[string]any{
		"uri":    "viking://doc1",
		"detail": "abstract",
	})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}
	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	if parsed["level"] != "abstract" {
		t.Errorf("level = %v, want abstract", parsed["level"])
	}
}

func TestOpenVikingProvider_HandleToolCall_VikingRead_嵌套Content(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{"content": "嵌套内容"},
		})
	}))
	defer server.Close()

	p := newTestVikingProvider(server)
	result, err := p.HandleToolCall(context.Background(), "viking_read", map[string]any{
		"uri": "viking://doc1",
	})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}
	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	if parsed["content"] != "嵌套内容" {
		t.Errorf("content = %v, want '嵌套内容'", parsed["content"])
	}
}

func TestOpenVikingProvider_HandleToolCall_VikingRead_缺URI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer server.Close()

	p := newTestVikingProvider(server)
	result, err := p.HandleToolCall(context.Background(), "viking_read", map[string]any{})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}
	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	if !strings.Contains(parsed["error"].(string), "uri is required") {
		t.Errorf("error = %v, 应提及 uri is required", parsed["error"])
	}
}

func TestOpenVikingProvider_HandleToolCall_VikingRemember_无Category(t *testing.T) {
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer server.Close()

	p := newTestVikingProvider(server)
	result, err := p.HandleToolCall(context.Background(), "viking_remember", map[string]any{
		"content": "记住这个",
	})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}
	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	if parsed["status"] != "stored" {
		t.Errorf("status = %v, want stored", parsed["status"])
	}
	// 无 category 时只含 [Remember]
	parts, _ := receivedBody["parts"].([]any)
	firstPart, _ := parts[0].(map[string]any)
	text, _ := firstPart["text"].(string)
	if !strings.Contains(text, "[Remember]") {
		t.Errorf("text = %q, 应包含 '[Remember]'", text)
	}
	if strings.Contains(text, "— ") {
		t.Errorf("text = %q, 无 category 时不应包含 '— '", text)
	}
}

func TestOpenVikingProvider_HandleToolCall_VikingRemember_缺Content(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer server.Close()

	p := newTestVikingProvider(server)
	result, err := p.HandleToolCall(context.Background(), "viking_remember", map[string]any{})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}
	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	if !strings.Contains(parsed["error"].(string), "content is required") {
		t.Errorf("error = %v, 应提及 content is required", parsed["error"])
	}
}

func TestOpenVikingProvider_HandleToolCall_VikingAddResource_缺URL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer server.Close()

	p := newTestVikingProvider(server)
	result, err := p.HandleToolCall(context.Background(), "viking_add_resource", map[string]any{})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}
	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	if !strings.Contains(parsed["error"].(string), "url is required") {
		t.Errorf("error = %v, 应提及 url is required", parsed["error"])
	}
}

func TestOpenVikingProvider_HandleToolCall_VikingBrowse_Stat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{"size": 1024, "type": "file"},
		})
	}))
	defer server.Close()

	p := newTestVikingProvider(server)
	result, err := p.HandleToolCall(context.Background(), "viking_browse", map[string]any{
		"action": "stat",
		"path":   "viking://file1",
	})
	if err != nil {
		t.Fatalf("HandleToolCall() error = %v", err)
	}
	var parsed map[string]any
	json.Unmarshal([]byte(result), &parsed)
	// stat 直接返回原始 result
	if parsed["result"] == nil {
		t.Error("stat 结果应包含 result")
	}
}

func TestTruncStr(t *testing.T) {
	if truncStr("hello", 10) != "hello" {
		t.Error("短字符串不应截断")
	}
	if truncStr("hello world", 5) != "hello" {
		t.Error("长字符串应截断到 maxLen")
	}
}

func TestFloatVal(t *testing.T) {
	if floatVal(float64(3.14)) != 3.14 {
		t.Error("float64 提取失败")
	}
	if floatVal(int(5)) != 5.0 {
		t.Error("int 提取失败")
	}
	if floatVal(int64(7)) != 7.0 {
		t.Error("int64 提取失败")
	}
	if floatVal("not a number") != 0 {
		t.Error("非数字应返回 0")
	}
}

func TestRoundTo3(t *testing.T) {
	if roundTo3(0.1234) != 0.123 {
		t.Errorf("roundTo3(0.1234) = %v, want 0.123", roundTo3(0.1234))
	}
	if roundTo3(0.9995) != 1.0 {
		t.Errorf("roundTo3(0.9995) = %v, want 1.0", roundTo3(0.9995))
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer server.Close()

	p := newTestVikingProvider(server)
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
