package memory

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EmbeddingProvider 嵌入向量提供者接口。对齐 Python EmbeddingProvider ABC（embeddings.py）。
// Python 基类属性 id/model/dims 在 Go 侧以方法暴露（ID/Model/Dims）
type EmbeddingProvider interface {
	// EmbedQuery 生成查询向量
	EmbedQuery(ctx context.Context, text string) ([]float64, error)
	// EmbedDocuments 批量生成文档向量
	EmbedDocuments(ctx context.Context, texts []string) ([][]float64, error)
	// ID 提供者标识
	ID() string
	// Model 模型名
	Model() string
	// Dims 向量维度
	Dims() int
}

// OpenAICompatibleEmbeddingProvider OpenAI 兼容嵌入提供者（支持 DashScope/OpenAI 等）。
// 对齐 Python OpenAICompatibleEmbeddingProvider（embeddings.py L31-102）。
// 差异说明：Python 用 httpx.AsyncClient(timeout=60.0)；Go 用 net/http 60s 超时，均无重试（单次 POST）。
// 字段命名对齐 lite 包先例：非导出字段 + 导出方法（APIKey/GetModel/GetBaseURL 供测试与诊断读取）
type OpenAICompatibleEmbeddingProvider struct {
	// apiKey 密钥（参数 > EMBED_KEY > EMBED_API_KEY > config embed 段）
	apiKey string
	// model 模型名（参数 > EMBED_MODEL > EMBEDDING_MODEL > config embed 段）
	model string
	// baseURL 基础地址（参数 > EMBED_BASE > EMBED_BASE_URL > config embed 段；/embeddings 后缀裁剪）
	baseURL string

	// client HTTP 客户端（惰性创建，对齐 Python _get_client）
	client *http.Client
	// dims 向量维度（初始 1024，首次成功后按实际更新）
	dims int
	// dimsMu dims 并发保护
	dimsMu sync.Mutex
}

// MockEmbeddingProvider 模拟嵌入提供者（测试用）。
// 对齐 Python MockEmbeddingProvider（embeddings.py L105-124）。
// 差异说明：Python 用 md5 作 random.seed(Mersenne Twister)；
// Go 无法复刻 PRNG 序列，用 md5 哈希逐轮扩展为 float64
// （值与 Python 不等价，但保证确定性与 128 维，见 EmbedQuery 注释）
type MockEmbeddingProvider struct {
	// id 提供者标识
	id string
	// model 模型名
	model string
	// dims 维度
	dims int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证接口满足
var (
	_ EmbeddingProvider = (*OpenAICompatibleEmbeddingProvider)(nil)
	_ EmbeddingProvider = (*MockEmbeddingProvider)(nil)
	_                   = math.Abs
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOpenAICompatibleEmbeddingProvider 构造 OpenAI 兼容提供者。
// 对齐 Python __init__（embeddings.py L39-57）的环境变量 fallback 链
// 与 /embeddings 后缀裁剪
func NewOpenAICompatibleEmbeddingProvider(apiKey, model, baseURL string) *OpenAICompatibleEmbeddingProvider {
	// Python: from .config import get_embed_config; embed_config = get_embed_config()
	embedConfig := GetEmbedConfig()

	// Python: api_key or os.getenv("EMBED_KEY") or os.getenv("EMBED_API_KEY") or embed_config.get("api_key", "")
	if apiKey == "" {
		apiKey = os.Getenv("EMBED_KEY")
	}
	if apiKey == "" {
		apiKey = os.Getenv("EMBED_API_KEY")
	}
	if apiKey == "" {
		apiKey = embedConfig.APIKey
	}
	// Python: model or os.getenv("EMBED_MODEL") or os.getenv("EMBEDDING_MODEL") or embed_config.get("model", "")
	if model == "" {
		model = os.Getenv("EMBED_MODEL")
	}
	if model == "" {
		model = os.Getenv("EMBEDDING_MODEL")
	}
	if model == "" {
		model = embedConfig.Model
	}
	// Python: base_url or os.getenv("EMBED_BASE") or os.getenv("EMBED_BASE_URL") or embed_config.get("base_url", "")
	if baseURL == "" {
		baseURL = os.Getenv("EMBED_BASE")
	}
	if baseURL == "" {
		baseURL = os.Getenv("EMBED_BASE_URL")
	}
	if baseURL == "" {
		baseURL = embedConfig.BaseURL
	}

	// Python: if self.base_url and self.base_url.endswith("/embeddings"): rsplit("/embeddings", 1)[0]
	if baseURL != "" && strings.HasSuffix(baseURL, "/embeddings") {
		baseURL = baseURL[:len(baseURL)-len("/embeddings")]
	}

	return &OpenAICompatibleEmbeddingProvider{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		dims:    1024,
	}
}

// APIKey 返回密钥（测试与诊断用）
func (p *OpenAICompatibleEmbeddingProvider) APIKey() string { return p.apiKey }

// GetBaseURL 返回基础地址（测试与诊断用）
func (p *OpenAICompatibleEmbeddingProvider) GetBaseURL() string { return p.baseURL }

// NewMockEmbeddingProvider 构造模拟提供者。对齐 Python MockEmbeddingProvider 类属性
// Python: id="mock", model="mock", dims=128
func NewMockEmbeddingProvider() *MockEmbeddingProvider {
	return &MockEmbeddingProvider{id: "mock", model: "mock", dims: 128}
}

// CreateEmbeddingProvider 根据配置创建嵌入提供者。
// 对齐 Python create_embedding_provider（embeddings.py L127-165）：
// provider=="mock" 直返；否则查 embed_key（EMBED_KEY > EMBED_API_KEY > config），
// 有则 OpenAICompatible；无且 fallback=="mock" 时 warn + Mock；否则 error
func CreateEmbeddingProvider(provider, model, fallback string) (EmbeddingProvider, error) {
	if provider == "mock" {
		return NewMockEmbeddingProvider(), nil
	}

	embedConfig := GetEmbedConfig()
	// Python: embed_key = os.getenv("EMBED_KEY") or os.getenv("EMBED_API_KEY") or embed_config.get("api_key", "")
	embedKey := os.Getenv("EMBED_KEY")
	if embedKey == "" {
		embedKey = os.Getenv("EMBED_API_KEY")
	}
	if embedKey == "" {
		embedKey = embedConfig.APIKey
	}

	if embedKey != "" {
		// Python: OpenAICompatibleEmbeddingProvider(api_key=embed_key,
		//   model=model or EMBED_MODEL or EMBEDDING_MODEL or config, base_url=EMBED_BASE or EMBED_BASE_URL or config)
		m := model
		if m == "" {
			m = os.Getenv("EMBED_MODEL")
		}
		if m == "" {
			m = os.Getenv("EMBEDDING_MODEL")
		}
		if m == "" {
			m = embedConfig.Model
		}
		b := os.Getenv("EMBED_BASE")
		if b == "" {
			b = os.Getenv("EMBED_BASE_URL")
		}
		if b == "" {
			b = embedConfig.BaseURL
		}
		return NewOpenAICompatibleEmbeddingProvider(embedKey, m, b), nil
	}

	if fallback == "mock" {
		// 对齐 Python: logger.warning("Embedding API key not found, using mock provider")
		logger.Warn(logComponent).Msg("Embedding API key not found, using mock provider")
		return NewMockEmbeddingProvider(), nil
	}

	return nil, fmt.Errorf(
		"Embedding API key not configured. Set embed.api_key in config.yaml or EMBED_API_KEY environment variable.")
}

// EmbedQuery 生成查询向量。对齐 Python embed_query（embeddings.py L69-72）：转发 embed_documents 取首个
func (p *OpenAICompatibleEmbeddingProvider) EmbedQuery(ctx context.Context, text string) ([]float64, error) {
	embeddings, err := p.EmbedDocuments(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	// Python: return embeddings[0] if embeddings else []
	if len(embeddings) == 0 {
		return []float64{}, nil
	}
	return embeddings[0], nil
}

// EmbedDocuments 批量生成文档向量。对齐 Python embed_documents（embeddings.py L74-102）
func (p *OpenAICompatibleEmbeddingProvider) EmbedDocuments(ctx context.Context, texts []string) ([][]float64, error) {
	if p.apiKey == "" {
		// 对齐 Python: raise ValueError("Embedding API key not configured. ...")
		return nil, fmt.Errorf(
			"Embedding API key not configured. Set embed.api_key in config.yaml or EMBED_API_KEY environment variable.")
	}

	// 对齐 Python: client = self._get_client()（httpx.AsyncClient(timeout=60.0) 惰性创建）
	if p.client == nil {
		p.client = &http.Client{Timeout: 60 * time.Second}
	}

	reqBody := map[string]any{
		"model":           p.model,
		"input":           texts,
		"encoding_format": "float",
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/embeddings", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Embedding API 请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// 对齐 Python: raise RuntimeError(f"Embedding API failed: {response.text}")
		respText, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Embedding API failed: %s", string(respText))
	}

	var data struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	// Python: for item in sorted(data.get("data", []), key=lambda x: x.get("index", 0))
	sort.SliceStable(data.Data, func(i, j int) bool {
		return data.Data[i].Index < data.Data[j].Index
	})

	embeddings := make([][]float64, 0, len(data.Data))
	for _, item := range data.Data {
		embeddings = append(embeddings, item.Embedding)
	}

	// Python: if embeddings: self.dims = len(embeddings[0])
	if len(embeddings) > 0 {
		p.dimsMu.Lock()
		p.dims = len(embeddings[0])
		p.dimsMu.Unlock()
	}
	return embeddings, nil
}

// ID 提供者标识。对齐 Python 类属性 id = "openai_compatible"
func (p *OpenAICompatibleEmbeddingProvider) ID() string { return "openai_compatible" }

// Model 模型名
func (p *OpenAICompatibleEmbeddingProvider) Model() string { return p.model }

// Dims 向量维度
func (p *OpenAICompatibleEmbeddingProvider) Dims() int {
	p.dimsMu.Lock()
	defer p.dimsMu.Unlock()
	return p.dims
}

// EmbedQuery 生成模拟向量。对齐 Python MockEmbeddingProvider.embed_query（embeddings.py L110-115）：
// md5(text) 作 random.seed，uniform(-1,1) 128 维。
// Go 无法复刻 Mersenne Twister 序列，改用 md5 链式哈希：每轮 md5 产 16 字节
// 供 8 个维度（每维 2 字节），映射 [-1,1]（确定性 + 128 维等价，数值与 Python 不等价——见结构体注释）
func (m *MockEmbeddingProvider) EmbedQuery(_ context.Context, text string) ([]float64, error) {
	out := make([]float64, m.dims)
	prefix := ""
	// 每轮提供 8 维：round*8 < dims
	for round := 0; round*8 < m.dims; round++ {
		sum := md5.Sum([]byte(prefix + text))
		prefix = string(sum[:])
		for b := 0; b < 8 && round*8+b < m.dims; b++ {
			v := binary.LittleEndian.Uint16(sum[b*2 : b*2+2])
			out[round*8+b] = float64(v)/65535.0*2.0 - 1.0 // 映射到 [-1, 1]
		}
	}
	return out, nil
}

// EmbedDocuments 批量生成模拟向量。对齐 Python：[await self.embed_query(t) for t in texts]
func (m *MockEmbeddingProvider) EmbedDocuments(ctx context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, 0, len(texts))
	for _, t := range texts {
		v, err := m.EmbedQuery(ctx, t)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// ID 提供者标识。对齐 Python 类属性 id = "mock"
func (m *MockEmbeddingProvider) ID() string { return m.id }

// Model 模型名。对齐 Python 类属性 model = "mock"
func (m *MockEmbeddingProvider) Model() string { return m.model }

// Dims 向量维度。对齐 Python 类属性 dims = 128
func (m *MockEmbeddingProvider) Dims() int { return m.dims }
