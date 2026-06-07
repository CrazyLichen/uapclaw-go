package service_api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// ──────────────────────────── RestfulApiCard 测试 ────────────────────────────

// TestNewRestfulApiCard_正常创建 测试正常创建 RestfulApiCard
func TestNewRestfulApiCard_正常创建(t *testing.T) {
	card, err := NewRestfulApiCard("test-api", "测试API", "https://api.example.com/users", "GET", nil)
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if card.Method != "GET" {
		t.Errorf("Method: 期望 GET，实际 %s", card.Method)
	}
	if card.Timeout != defaultTimeout {
		t.Errorf("Timeout: 期望 %v，实际 %v", defaultTimeout, card.Timeout)
	}
	if card.MaxResponseByteSize != defaultMaxResponseByteSize {
		t.Errorf("MaxResponseByteSize: 期望 %v，实际 %v", defaultMaxResponseByteSize, card.MaxResponseByteSize)
	}
}

// TestNewRestfulApiCard_默认POST 测试默认方法为 POST
func TestNewRestfulApiCard_默认POST(t *testing.T) {
	card, err := NewRestfulApiCard("test-api", "测试API", "https://api.example.com/users", "", nil)
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if card.Method != "POST" {
		t.Errorf("Method: 期望 POST，实际 %s", card.Method)
	}
}

// TestNewRestfulApiCard_不支持的方法 测试不支持的方法
func TestNewRestfulApiCard_不支持的方法(t *testing.T) {
	_, err := NewRestfulApiCard("test-api", "测试API", "https://api.example.com/users", "PATCH", nil)
	if err != nil {
		t.Fatalf("PATCH 应被支持: %v", err)
	}

	_, err = NewRestfulApiCard("test-api", "测试API", "https://api.example.com/users", "INVALID", nil)
	if err == nil {
		t.Error("不支持的方法应返回错误")
	}
}

// TestNewRestfulApiCard_无效URL 测试无效 URL
func TestNewRestfulApiCard_无效URL(t *testing.T) {
	_, err := NewRestfulApiCard("test-api", "测试API", "", "GET", nil)
	if err == nil {
		t.Error("空 URL 应返回错误")
	}

	_, err = NewRestfulApiCard("test-api", "测试API", "ftp://example.com", "GET", nil)
	if err == nil {
		t.Error("非 http/https 协议应返回错误")
	}
}

// TestNewRestfulApiCard_路径参数校验 测试路径参数校验
func TestNewRestfulApiCard_路径参数校验(t *testing.T) {
	// URL 有路径参数但无 InputSchema
	_, err := NewRestfulApiCard("test-api", "测试API", "https://api.example.com/users/{id}", "GET", nil)
	if err == nil {
		t.Error("URL 有路径参数但无 InputSchema 应返回错误")
	}

	// URL 有路径参数，InputSchema 中未标记 location:path
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{"type": "integer"}, // 缺少 location:path
		},
	}
	_, err = NewRestfulApiCard("test-api", "测试API", "https://api.example.com/users/{id}", "GET", schema)
	if err == nil {
		t.Error("路径参数未标记 location:path 应返回错误")
	}

	// 正确：路径参数标记了 location:path
	schema2 := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{"type": "integer", "location": "path"},
		},
	}
	card, err := NewRestfulApiCard("test-api", "测试API", "https://api.example.com/users/{id}", "GET", schema2)
	if err != nil {
		t.Fatalf("路径参数正确标记应通过校验: %v", err)
	}
	if card == nil {
		t.Error("card 不应为 nil")
	}
}

// TestRestfulApiCard_ToolInfo 测试 ToolInfo 覆写
func TestRestfulApiCard_ToolInfo(t *testing.T) {
	inputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":   map[string]any{"type": "integer", "location": "path"},
			"name": map[string]any{"type": "string", "location": "body"},
		},
	}
	card, _ := NewRestfulApiCard("test-api", "测试API", "https://api.example.com/users/{id}", "PUT", inputSchema)

	info := card.ToolInfo()
	if info.Name != "test-api" {
		t.Errorf("Name: 期望 test-api，实际 %s", info.Name)
	}
	// 验证 Parameters 是同一个 InputSchema 引用
	if info.Parameters == nil {
		t.Error("Parameters 不应为 nil")
	}
}

// ──────────────────────────── RestfulApi 测试 ────────────────────────────

// TestRestfulApi_Invoke_GET 测试 GET 请求
func TestRestfulApi_Invoke_GET(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("Method: 期望 GET，实际 %s", r.Method)
		}
		// 验证 query 参数
		q := r.URL.Query()
		if q.Get("q") != "search" {
			t.Errorf("query q: 期望 search，实际 %s", q.Get("q"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"result": "ok"})
	}))
	defer server.Close()

	inputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"q": map[string]any{"type": "string", "location": "query"},
		},
	}
	card, _ := NewRestfulApiCard("test-api", "测试API", server.URL+"/search", "GET", inputSchema)
	api, err := NewRestfulApi(card)
	if err != nil {
		t.Fatalf("NewRestfulApi 失败: %v", err)
	}

	result, err := api.Invoke(context.Background(), map[string]any{"q": "search"})
	if err != nil {
		t.Fatalf("Invoke 失败: %v", err)
	}

	code, _ := result["code"].(int)
	if code != 200 {
		t.Errorf("code: 期望 200，实际 %d", code)
	}
	message, _ := result["message"].(string)
	if message != "success" {
		t.Errorf("message: 期望 success，实际 %s", message)
	}
}

// TestRestfulApi_Invoke_POST 测试 POST 请求
func TestRestfulApi_Invoke_POST(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Method: 期望 POST，实际 %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type: 期望 application/json，实际 %s", r.Header.Get("Content-Type"))
		}
		// 读取 body
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "Alice" {
			t.Errorf("body.name: 期望 Alice，实际 %v", body["name"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": 1, "name": body["name"]})
	}))
	defer server.Close()

	inputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"}, // 无 location，默认 body
		},
	}
	card, _ := NewRestfulApiCard("test-api", "测试API", server.URL+"/users", "POST", inputSchema)
	api, _ := NewRestfulApi(card)

	result, err := api.Invoke(context.Background(), map[string]any{"name": "Alice"})
	if err != nil {
		t.Fatalf("Invoke 失败: %v", err)
	}

	code, _ := result["code"].(int)
	if code != 200 {
		t.Errorf("code: 期望 200，实际 %d", code)
	}
}

// TestRestfulApi_Invoke_路径参数替换 测试路径参数替换
func TestRestfulApi_Invoke_路径参数替换(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证路径
		if r.URL.Path != "/users/42" {
			t.Errorf("Path: 期望 /users/42，实际 %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": 42})
	}))
	defer server.Close()

	inputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":   map[string]any{"type": "integer", "location": "path"},
			"name": map[string]any{"type": "string", "location": "body"},
		},
		"required": []any{"id"},
	}
	card, _ := NewRestfulApiCard("test-api", "测试API", server.URL+"/users/{id}", "PUT", inputSchema)
	api, _ := NewRestfulApi(card)

	result, err := api.Invoke(context.Background(), map[string]any{"id": 42, "name": "Alice"})
	if err != nil {
		t.Fatalf("Invoke 失败: %v", err)
	}
	if result == nil {
		t.Error("结果不应为 nil")
	}
}

// TestRestfulApi_Invoke_Header参数 测试 header 参数
func TestRestfulApi_Invoke_Header参数(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// header 的 key 与 schema 中的参数名一致
		if r.Header.Get("token") != "secret123" {
			t.Errorf("token header: 期望 secret123，实际 %s", r.Header.Get("token"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	}))
	defer server.Close()

	inputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"token": map[string]any{"type": "string", "location": "header"},
		},
	}
	card, _ := NewRestfulApiCard("test-api", "测试API", server.URL+"/data", "GET", inputSchema)
	api, _ := NewRestfulApi(card)

	_, err := api.Invoke(context.Background(), map[string]any{"token": "secret123"})
	if err != nil {
		t.Fatalf("Invoke 失败: %v", err)
	}
}

// TestRestfulApi_Invoke_Query数组参数 测试 query 数组参数展开
func TestRestfulApi_Invoke_Query数组参数(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		ids := q["ids"]
		if len(ids) != 3 {
			t.Errorf("ids 数量: 期望 3，实际 %d", len(ids))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"count": len(ids)})
	}))
	defer server.Close()

	inputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ids": map[string]any{"type": "array", "location": "query"},
		},
	}
	card, _ := NewRestfulApiCard("test-api", "测试API", server.URL+"/items", "GET", inputSchema)
	api, _ := NewRestfulApi(card)

	_, err := api.Invoke(context.Background(), map[string]any{"ids": []any{1, 2, 3}})
	if err != nil {
		t.Fatalf("Invoke 失败: %v", err)
	}
}

// TestRestfulApi_Invoke_超时 测试超时
func TestRestfulApi_Invoke_超时(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	card, _ := NewRestfulApiCard("test-api", "测试API", server.URL+"/slow", "GET", nil,
		WithRestfulApiCardTimeout(1), // 1 秒超时
	)
	api, _ := NewRestfulApi(card)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := api.Invoke(ctx, map[string]any{})
	if err == nil {
		t.Error("超时应返回错误")
	}
}

// TestRestfulApi_Invoke_HTTP错误状态码 测试 HTTP 错误状态码
func TestRestfulApi_Invoke_HTTP错误状态码(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, "not found")
	}))
	defer server.Close()

	card, _ := NewRestfulApiCard("test-api", "测试API", server.URL+"/missing", "GET", nil)
	api, _ := NewRestfulApi(card)

	_, err := api.Invoke(context.Background(), map[string]any{},
		tool.WithRaiseForStatus(true),
	)
	if err == nil {
		t.Error("HTTP 404 应返回错误（raiseForStatus=true）")
	}
}

// TestRestfulApi_Invoke_HTTP错误不抛异常 测试 raiseForStatus=false
func TestRestfulApi_Invoke_HTTP错误不抛异常(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "not found")
	}))
	defer server.Close()

	card, _ := NewRestfulApiCard("test-api", "测试API", server.URL+"/missing", "GET", nil)
	api, _ := NewRestfulApi(card)

	result, err := api.Invoke(context.Background(), map[string]any{},
		tool.WithRaiseForStatus(false),
	)
	if err != nil {
		t.Fatalf("raiseForStatus=false 时不应报错: %v", err)
	}
	code, _ := result["code"].(int)
	if code != 404 {
		t.Errorf("code: 期望 404，实际 %d", code)
	}
	message, _ := result["message"].(string)
	if message == "success" {
		t.Error("404 的 message 不应是 success")
	}
}

// TestRestfulApi_Invoke_DELETE 测试 DELETE 请求（body 走 query）
func TestRestfulApi_Invoke_DELETE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("Method: 期望 DELETE，实际 %s", r.Method)
		}
		q := r.URL.Query()
		if q.Get("reason") != "cleanup" {
			t.Errorf("query reason: 期望 cleanup，实际 %s", q.Get("reason"))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	inputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"reason": map[string]any{"type": "string"}, // 无 location，DELETE 默认走 query
		},
	}
	card, _ := NewRestfulApiCard("test-api", "测试API", server.URL+"/items/1", "DELETE", inputSchema)
	api, _ := NewRestfulApi(card)

	result, err := api.Invoke(context.Background(), map[string]any{"reason": "cleanup"})
	if err != nil {
		t.Fatalf("Invoke 失败: %v", err)
	}
	code, _ := result["code"].(int)
	if code != 204 {
		t.Errorf("code: 期望 204，实际 %d", code)
	}
}

// TestRestfulApi_Stream 测试 Stream 不支持
func TestRestfulApi_Stream(t *testing.T) {
	card, _ := NewRestfulApiCard("test-api", "测试API", "https://api.example.com/test", "GET", nil)
	api, _ := NewRestfulApi(card)

	_, err := api.Stream(context.Background(), map[string]any{})
	if err == nil {
		t.Error("Stream 应返回 ErrStreamNotSupported")
	}
}

// TestRestfulApi_Invoke_默认参数合并 测试默认参数合并
func TestRestfulApi_Invoke_默认参数合并(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("api_key") != "default-key" {
			t.Errorf("query api_key: 期望 default-key，实际 %s", q.Get("api_key"))
		}
		if q.Get("q") != "search" {
			t.Errorf("query q: 期望 search，实际 %s", q.Get("q"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	}))
	defer server.Close()

	inputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"q":       map[string]any{"type": "string", "location": "query"},
			"api_key": map[string]any{"type": "string", "location": "query"},
		},
	}
	card, _ := NewRestfulApiCard("test-api", "测试API", server.URL+"/search", "GET", inputSchema,
		WithRestfulApiCardQueries(map[string]any{"api_key": "default-key"}),
	)
	api, _ := NewRestfulApi(card)

	result, err := api.Invoke(context.Background(), map[string]any{"q": "search"})
	if err != nil {
		t.Fatalf("Invoke 失败: %v", err)
	}
	code, _ := result["code"].(int)
	if code != 200 {
		t.Errorf("code: 期望 200，实际 %d", code)
	}
}

// TestGetParametersByLocation 测试 GetParametersByLocation
func TestGetParametersByLocation(t *testing.T) {
	inputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":     map[string]any{"type": "integer", "location": "path", "description": "用户ID"},
			"q":      map[string]any{"type": "string", "location": "query"},
			"token":  map[string]any{"type": "string", "location": "header"},
			"name":   map[string]any{"type": "string"}, // 无 location → 默认 body
		},
		"required": []any{"id"},
	}
	card, _ := NewRestfulApiCard("test-api", "测试API", "https://api.example.com/users/{id}", "GET", inputSchema)

	result := GetParametersByLocation(card)

	if len(result["path"]) != 1 {
		t.Errorf("path 参数数量: 期望 1，实际 %d", len(result["path"]))
	}
	if len(result["query"]) != 1 {
		t.Errorf("query 参数数量: 期望 1，实际 %d", len(result["query"]))
	}
	if len(result["header"]) != 1 {
		t.Errorf("header 参数数量: 期望 1，实际 %d", len(result["header"]))
	}
	if len(result["body"]) != 1 {
		t.Errorf("body 参数数量: 期望 1，实际 %d", len(result["body"]))
	}

	// 检查 path 参数的 name
	pathParam := result["path"][0]
	if pathParam["name"] != "id" {
		t.Errorf("path 参数 name: 期望 id，实际 %v", pathParam["name"])
	}
	if pathParam["required"] != true {
		t.Errorf("path 参数 required: 期望 true，实际 %v", pathParam["required"])
	}
}

// TestGetParametersByLocation_空Schema 测试空 schema
func TestGetParametersByLocation_空Schema(t *testing.T) {
	card, _ := NewRestfulApiCard("test-api", "测试API", "https://api.example.com/test", "GET", nil)
	result := GetParametersByLocation(card)
	for loc, params := range result {
		if len(params) != 0 {
			t.Errorf("%s: 期望空数组，实际 %d", loc, len(params))
		}
	}
}

// TestNewRestfulApi_Card校验 测试 Card 校验
func TestNewRestfulApi_Card校验(t *testing.T) {
	// nil card 应该被 ValidateToolCard 捕获，但 Go 中 NewRestfulApiCard 不会返回 nil card
	// 测试正常情况
	card, _ := NewRestfulApiCard("test-api", "测试API", "https://api.example.com/test", "GET", nil)
	_, err := NewRestfulApi(card)
	if err != nil {
		t.Fatalf("正常创建应成功: %v", err)
	}
}

// TestRestfulApi_Card 测试 Card() 方法
func TestRestfulApi_Card(t *testing.T) {
	card, _ := NewRestfulApiCard("test-api", "测试API", "https://api.example.com/test", "GET", nil)
	api, _ := NewRestfulApi(card)
	returnedCard := api.Card()
	if returnedCard == nil {
		t.Error("Card() 不应返回 nil")
	}
	if returnedCard.Name != "test-api" {
		t.Errorf("Card().Name: 期望 test-api，实际 %s", returnedCard.Name)
	}
}

// TestNewRestfulApiCard_AllOptions 测试所有构造选项
func TestNewRestfulApiCard_AllOptions(t *testing.T) {
	card, err := NewRestfulApiCard("test-api", "测试API", "https://api.example.com/test", "GET", nil,
		WithRestfulApiCardHeaders(map[string]any{"X-Custom": "value"}),
		WithRestfulApiCardPaths(map[string]any{"id": "1"}),
		WithRestfulApiCardMaxResponseByteSize(1024),
	)
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if card.Headers["X-Custom"] != "value" {
		t.Errorf("Headers[X-Custom]: 期望 value，实际 %v", card.Headers["X-Custom"])
	}
	if card.Paths["id"] != "1" {
		t.Errorf("Paths[id]: 期望 1，实际 %v", card.Paths["id"])
	}
	if card.MaxResponseByteSize != 1024 {
		t.Errorf("MaxResponseByteSize: 期望 1024，实际 %d", card.MaxResponseByteSize)
	}
}

// TestRestfulApi_Invoke_响应体超限 测试响应体超出大小限制
func TestRestfulApi_Invoke_响应体超限(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		// 写 200 字节
		for i := 0; i < 200; i++ {
			w.Write([]byte("x"))
		}
	}))
	defer server.Close()

	card, _ := NewRestfulApiCard("test-api", "测试API", server.URL+"/big", "GET", nil,
		WithRestfulApiCardMaxResponseByteSize(100),
	)
	api, _ := NewRestfulApi(card)

	_, err := api.Invoke(context.Background(), map[string]any{})
	if err == nil {
		t.Error("响应体超限应返回错误")
	}
}

// TestRestfulApi_Invoke_Form参数Fallback 测试 form 参数 fallback 到 body
func TestRestfulApi_Invoke_Form参数Fallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(body)
	}))
	defer server.Close()

	inputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file":    map[string]any{"type": "string", "location": "form"},
			"comment": map[string]any{"type": "string"}, // body
		},
	}
	card, _ := NewRestfulApiCard("test-api", "测试API", server.URL+"/upload", "POST", inputSchema)
	api, _ := NewRestfulApi(card)

	result, err := api.Invoke(context.Background(), map[string]any{"file": "data.txt", "comment": "test"})
	if err != nil {
		t.Fatalf("Invoke 失败: %v", err)
	}
	code, _ := result["code"].(int)
	if code != 200 {
		t.Errorf("code: 期望 200，实际 %d", code)
	}
}

// TestRestfulApi_Invoke_HEAD 测试 HEAD 请求
func TestRestfulApi_Invoke_HEAD(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "HEAD" {
			t.Errorf("Method: 期望 HEAD，实际 %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	card, _ := NewRestfulApiCard("test-api", "测试API", server.URL+"/check", "HEAD", nil)
	api, _ := NewRestfulApi(card)

	result, err := api.Invoke(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Invoke 失败: %v", err)
	}
	code, _ := result["code"].(int)
	if code != 200 {
		t.Errorf("code: 期望 200，实际 %d", code)
	}
}

// TestRestfulApi_Invoke_PATCH 测试 PATCH 请求
func TestRestfulApi_Invoke_PATCH(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			t.Errorf("Method: 期望 PATCH，实际 %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"updated": true})
	}))
	defer server.Close()

	card, _ := NewRestfulApiCard("test-api", "测试API", server.URL+"/users/1", "PATCH", nil)
	api, _ := NewRestfulApi(card)

	result, err := api.Invoke(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Invoke 失败: %v", err)
	}
	code, _ := result["code"].(int)
	if code != 200 {
		t.Errorf("code: 期望 200，实际 %d", code)
	}
}

// TestRestfulApi_Invoke_响应解析失败 测试响应解析失败
func TestRestfulApi_Invoke_响应解析失败(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write([]byte("binary data"))
	}))
	defer server.Close()

	card, _ := NewRestfulApiCard("test-api", "测试API", server.URL+"/binary", "GET", nil)
	api, _ := NewRestfulApi(card)

	_, err := api.Invoke(context.Background(), map[string]any{})
	if err == nil {
		t.Error("未知 content-type 应返回解析错误")
	}
}

// TestGetParametersByLocation_form参数 测试 form 参数分组
func TestGetParametersByLocation_form参数(t *testing.T) {
	inputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file": map[string]any{"type": "string", "location": "form"},
		},
	}
	card, _ := NewRestfulApiCard("test-api", "测试API", "https://api.example.com/upload", "POST", inputSchema)
	result := GetParametersByLocation(card)
	if len(result["form"]) != 1 {
		t.Errorf("form 参数数量: 期望 1，实际 %d", len(result["form"]))
	}
}

// TestValidateURL_解析失败 测试 URL 解析失败
func TestValidateURL_解析失败(t *testing.T) {
	err := validateURL("://invalid")
	if err == nil {
		t.Error("无效 URL 应返回错误")
	}
}

// TestRestfulApi_Invoke_Text响应 测试纯文本响应
func TestRestfulApi_Invoke_Text响应(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "hello world")
	}))
	defer server.Close()

	card, _ := NewRestfulApiCard("test-api", "测试API", server.URL+"/text", "GET", nil)
	api, _ := NewRestfulApi(card)

	result, err := api.Invoke(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Invoke 失败: %v", err)
	}
	code, _ := result["code"].(int)
	if code != 200 {
		t.Errorf("code: 期望 200，实际 %d", code)
	}
}

// TestRestfulApi_Invoke_自定义Header默认值 测试预设 header 默认值
func TestRestfulApi_Invoke_自定义Header默认值(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "preset-key" {
			t.Errorf("X-Api-Key: 期望 preset-key，实际 %s", r.Header.Get("X-Api-Key"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	inputSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"X-Api-Key": map[string]any{"type": "string", "location": "header"},
		},
	}
	card, _ := NewRestfulApiCard("test-api", "测试API", server.URL+"/data", "GET", inputSchema,
		WithRestfulApiCardHeaders(map[string]any{"X-Api-Key": "preset-key"}),
	)
	api, _ := NewRestfulApi(card)

	_, err := api.Invoke(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Invoke 失败: %v", err)
	}
}
