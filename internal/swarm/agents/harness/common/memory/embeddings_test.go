package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// clearEmbedEnv 清理嵌入相关环境变量（测试隔离）
func clearEmbedEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"EMBED_KEY", "EMBED_API_KEY", "EMBED_MODEL", "EMBEDDING_MODEL", "EMBED_BASE", "EMBED_BASE_URL"} {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}
	ClearConfigCache()
}

// TestOpenAICompatibleEmbeddingProvider_环境变量链 对齐 Python __init__ fallback 链：
// 参数 > EMBED_KEY > EMBED_API_KEY > config embed 段
func TestOpenAICompatibleEmbeddingProvider_环境变量链(t *testing.T) {
	clearEmbedEnv(t)
	injectTestConfigLoader(t, "")

	// 1. 无任何配置：api_key 为空
	p := NewOpenAICompatibleEmbeddingProvider("", "", "")
	if p.APIKey() != "" {
		t.Errorf("无配置时 api_key 应为空")
	}

	// 2. EMBED_KEY 优先于 EMBED_API_KEY
	t.Setenv("EMBED_KEY", "key1")
	t.Setenv("EMBED_API_KEY", "key2")
	p = NewOpenAICompatibleEmbeddingProvider("", "", "")
	if p.APIKey() != "key1" {
		t.Errorf("EMBED_KEY 应优先，got %s", p.APIKey())
	}

	// 3. 参数优先于环境变量
	p = NewOpenAICompatibleEmbeddingProvider("param-key", "param-model", "param-base")
	if p.APIKey() != "param-key" || p.Model() != "param-model" || p.GetBaseURL() != "param-base" {
		t.Errorf("参数应最优先: provider=%s model=%s base=%s", p.APIKey(), p.Model(), p.GetBaseURL())
	}

	// 4. EMBED_MODEL 优先于 EMBEDDING_MODEL
	t.Setenv("EMBED_MODEL", "m1")
	t.Setenv("EMBEDDING_MODEL", "m2")
	p = NewOpenAICompatibleEmbeddingProvider("", "", "")
	if p.Model() != "m1" {
		t.Errorf("EMBED_MODEL 应优先，got %s", p.Model())
	}

	// 5. EMBED_BASE 优先于 EMBED_BASE_URL
	t.Setenv("EMBED_BASE", "b1")
	t.Setenv("EMBED_BASE_URL", "b2")
	p = NewOpenAICompatibleEmbeddingProvider("", "", "")
	if p.GetBaseURL() != "b1" {
		t.Errorf("EMBED_BASE 应优先，got %s", p.GetBaseURL())
	}

	// 6. config embed 段兜底（环境变量全空）
	clearEmbedEnv(t)
	injectTestConfigLoader(t, `
embed:
  embed_api_key: cfg-key
  embed_model: cfg-model
  embed_base_url: https://cfg.example.com
`)
	p = NewOpenAICompatibleEmbeddingProvider("", "", "")
	if p.APIKey() != "cfg-key" || p.Model() != "cfg-model" || p.GetBaseURL() != "https://cfg.example.com" {
		t.Errorf("config 段兜底失败: provider=%s model=%s base=%s", p.APIKey(), p.Model(), p.GetBaseURL())
	}
}

// TestOpenAICompatibleEmbeddingProvider_baseURL裁剪 对齐 Python：以 /embeddings 结尾裁剪
func TestOpenAICompatibleEmbeddingProvider_baseURL裁剪(t *testing.T) {
	clearEmbedEnv(t)
	injectTestConfigLoader(t, "")
	t.Setenv("EMBED_KEY", "k")
	t.Setenv("EMBED_MODEL", "m")
	t.Setenv("EMBED_BASE", "https://api.example.com/v1/embeddings")
	p := NewOpenAICompatibleEmbeddingProvider("", "", "")
	if p.GetBaseURL() != "https://api.example.com/v1" {
		t.Fatalf("裁剪失败: %s", p.GetBaseURL())
	}
}

// TestOpenAICompatibleEmbeddingProvider_EmbedDocuments 对齐 Python embed_documents：
// POST {base_url}/embeddings + Bearer + encoding_format=float + index 排序 + dims 更新
func TestOpenAICompatibleEmbeddingProvider_EmbedDocuments(t *testing.T) {
	var gotBody map[string]any
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Errorf("路径应为 /embeddings，got %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		// 故意乱序返回（index 1 在前）
		_, _ = w.Write([]byte(`{"data":[{"index":1,"embedding":[3.0,4.0]},{"index":0,"embedding":[1.0,2.0]}]}`))
	}))
	defer server.Close()

	clearEmbedEnv(t)
	injectTestConfigLoader(t, "")
	t.Setenv("EMBED_KEY", "test-key")
	t.Setenv("EMBED_MODEL", "test-model")
	t.Setenv("EMBED_BASE", server.URL)

	p := NewOpenAICompatibleEmbeddingProvider("", "", "")
	if p.Dims() != 1024 {
		t.Errorf("初始 dims 应为 1024")
	}

	emb, err := p.EmbedDocuments(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("EmbedDocuments 失败: %v", err)
	}
	if len(emb) != 2 {
		t.Fatalf("应返回 2 个向量")
	}
	// index 排序：0 在前
	if emb[0][0] != 1.0 || emb[1][0] != 3.0 {
		t.Errorf("应按 index 排序: %v", emb)
	}
	if p.Dims() != 2 {
		t.Errorf("dims 应更新为 2，got %d", p.Dims())
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Bearer 认证失败: %s", gotAuth)
	}
	if gotBody["model"] != "test-model" || gotBody["encoding_format"] != "float" {
		t.Errorf("请求体错误: %v", gotBody)
	}
	inputs, _ := gotBody["input"].([]any)
	if len(inputs) != 2 || inputs[0] != "hello" {
		t.Errorf("input 错误: %v", inputs)
	}
}

// TestOpenAICompatibleEmbeddingProvider_EmbedDocuments_无Key 对齐 Python：无 key 抛 ValueError
func TestOpenAICompatibleEmbeddingProvider_EmbedDocuments_无Key(t *testing.T) {
	clearEmbedEnv(t)
	injectTestConfigLoader(t, "")
	p := NewOpenAICompatibleEmbeddingProvider("", "", "")
	_, err := p.EmbedDocuments(context.Background(), []string{"x"})
	if err == nil {
		t.Fatalf("无 key 应报错")
	}
}

// TestOpenAICompatibleEmbeddingProvider_EmbedDocuments_非200 对齐 Python：非 200 抛 RuntimeError
func TestOpenAICompatibleEmbeddingProvider_EmbedDocuments_非200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("upstream error"))
	}))
	defer server.Close()
	clearEmbedEnv(t)
	injectTestConfigLoader(t, "")
	t.Setenv("EMBED_KEY", "k")
	t.Setenv("EMBED_MODEL", "m")
	t.Setenv("EMBED_BASE", server.URL)

	p := NewOpenAICompatibleEmbeddingProvider("", "", "")
	_, err := p.EmbedDocuments(context.Background(), []string{"x"})
	if err == nil {
		t.Fatalf("非 200 应报错")
	}
}

// TestOpenAICompatibleEmbeddingProvider_EmbedQuery 对齐 Python：embed_query 转发 embed_documents 取首个
func TestOpenAICompatibleEmbeddingProvider_EmbedQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1.5,2.5,3.5]}]}`))
	}))
	defer server.Close()
	clearEmbedEnv(t)
	injectTestConfigLoader(t, "")
	t.Setenv("EMBED_KEY", "k")
	t.Setenv("EMBED_MODEL", "m")
	t.Setenv("EMBED_BASE", server.URL)

	p := NewOpenAICompatibleEmbeddingProvider("", "", "")
	vec, err := p.EmbedQuery(context.Background(), "q")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(vec) != 3 || vec[0] != 1.5 {
		t.Errorf("got %v", vec)
	}
}

// TestOpenAICompatibleEmbeddingProvider_EmbedQuery_空结果 对齐 Python：embeddings 为空时返回 []
func TestOpenAICompatibleEmbeddingProvider_EmbedQuery_空结果(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()
	clearEmbedEnv(t)
	injectTestConfigLoader(t, "")
	t.Setenv("EMBED_KEY", "k")
	t.Setenv("EMBED_MODEL", "m")
	t.Setenv("EMBED_BASE", server.URL)

	p := NewOpenAICompatibleEmbeddingProvider("", "", "")
	vec, err := p.EmbedQuery(context.Background(), "q")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(vec) != 0 {
		t.Errorf("空结果应返回空切片，got %v", vec)
	}
}

// TestMockEmbeddingProvider_确定性与维度 对齐 Python：md5 种子 128 维
func TestMockEmbeddingProvider_确定性与维度(t *testing.T) {
	m := NewMockEmbeddingProvider()
	if m.ID() != "mock" || m.Model() != "mock" || m.Dims() != 128 {
		t.Errorf("元数据错误: %s/%s/%d", m.ID(), m.Model(), m.Dims())
	}
	v1, err := m.EmbedQuery(context.Background(), "same text")
	if err != nil {
		t.Fatal(err)
	}
	v2, _ := m.EmbedQuery(context.Background(), "same text")
	v3, _ := m.EmbedQuery(context.Background(), "different")
	if len(v1) != 128 {
		t.Errorf("应为 128 维，got %d", len(v1))
	}
	for i := range v1 {
		if v1[i] != v2[i] {
			t.Errorf("同输入应确定性")
			break
		}
		if v1[i] < -1 || v1[i] > 1 {
			t.Errorf("值应在 [-1,1]")
			break
		}
	}
	sameAsDiff := true
	for i := range v1 {
		if v1[i] != v3[i] {
			sameAsDiff = false
			break
		}
	}
	if sameAsDiff {
		t.Errorf("不同输入应产生不同向量")
	}
}

// TestMockEmbeddingProvider_EmbedDocuments 对齐 Python：逐条转 embed_query
func TestMockEmbeddingProvider_EmbedDocuments(t *testing.T) {
	m := NewMockEmbeddingProvider()
	docs, err := m.EmbedDocuments(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 || len(docs[0]) != 128 || len(docs[1]) != 128 {
		t.Errorf("got %d", len(docs))
	}
}

// TestCreateEmbeddingProvider_mock 对齐 Python：provider=="mock" 直接返回
func TestCreateEmbeddingProvider_mock(t *testing.T) {
	p, err := CreateEmbeddingProvider("mock", "", "mock")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID() != "mock" {
		t.Errorf("应返回 Mock")
	}
}

// TestCreateEmbeddingProvider_有key 对齐 Python：有 embed_key 时返回 OpenAICompatible
func TestCreateEmbeddingProvider_有key(t *testing.T) {
	clearEmbedEnv(t)
	injectTestConfigLoader(t, "")
	t.Setenv("EMBED_KEY", "k")
	t.Setenv("EMBED_MODEL", "m")
	t.Setenv("EMBED_BASE", "https://x.example.com")
	p, err := CreateEmbeddingProvider("auto", "", "mock")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID() != "openai_compatible" {
		t.Errorf("应返回 OpenAICompatible，got %s", p.ID())
	}
	if p.Model() != "m" {
		t.Errorf("model 应为 m，got %s", p.Model())
	}
}

// TestCreateEmbeddingProvider_降级mock 对齐 Python：无 key + fallback=mock → warn + Mock
func TestCreateEmbeddingProvider_降级mock(t *testing.T) {
	clearEmbedEnv(t)
	injectTestConfigLoader(t, "")
	p, err := CreateEmbeddingProvider("auto", "", "mock")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID() != "mock" {
		t.Errorf("应降级 Mock，got %s", p.ID())
	}
}

// TestCreateEmbeddingProvider_无key无fallback 对齐 Python：无 key + fallback!=mock → ValueError
func TestCreateEmbeddingProvider_无key无fallback(t *testing.T) {
	clearEmbedEnv(t)
	injectTestConfigLoader(t, "")
	_, err := CreateEmbeddingProvider("auto", "", "error")
	if err == nil {
		t.Fatalf("应报错")
	}
}
