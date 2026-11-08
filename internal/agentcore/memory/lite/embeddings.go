package lite

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/rand"
	"os"
	"strings"

	baseEmbedding "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	apiEmbedding "github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EmbeddingProvider 嵌入向量提供者接口。对齐 Python EmbeddingProvider
type EmbeddingProvider interface {
	// EmbedQuery 嵌入单个查询文本
	EmbedQuery(ctx context.Context, text string) ([]float64, error)
	// EmbedDocuments 嵌入多个文档文本
	EmbedDocuments(ctx context.Context, texts []string) ([][]float64, error)
	// ID 返回提供者标识。对齐 Python: self.provider.id
	ID() string
	// Model 返回提供者模型名。对齐 Python: self.provider.model
	Model() string
	// Dims 返回嵌入向量维度。对齐 Python: self.provider.dims
	Dims() int
	// ConfigFingerprint 返回影响向量结果的配置指纹。
	//
	// 用于两件事：判定配置是否变更（需要重建索引），以及隔离 embedding_cache。
	// 对齐 Python EmbeddingProvider.config_fingerprint（embeddings.py:19）。
	ConfigFingerprint() string
}

// MockEmbeddingProvider 模拟嵌入提供者。对齐 Python MockEmbeddingProvider
type MockEmbeddingProvider struct {
	// id 提供者标识
	id string
	// model 提供者模型名
	model string
	// dims 嵌入向量维度。对齐 Python MockEmbeddingProvider.dims = 128
	dims int
}

// baseEmbeddingAdapter 将 foundation/store/embedding.BaseEmbedding 适配为 lite.EmbeddingProvider
type baseEmbeddingAdapter struct {
	base  baseEmbedding.BaseEmbedding
	prov  string // 提供者标识
	model string // 提供者模型名
	dims  int    // 嵌入向量维度
	// baseURL 归一化后的端点（不含尾部 /embeddings 与斜杠），参与 config fingerprint
	baseURL string
	// apiKeyHash api_key 的 sha256 前 16 位（不存原文），参与 config fingerprint
	apiKeyHash string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMockEmbeddingProvider 创建模拟嵌入提供者
func NewMockEmbeddingProvider() *MockEmbeddingProvider {
	return &MockEmbeddingProvider{id: "mock", model: "mock", dims: 128}
}

// NormalizeEmbeddingBaseURL 归一化嵌入端点，使等价写法得到同一形态。
//
// 先去尾部斜杠，再剥掉 `/embeddings` 后缀（请求路径会再补回来）。
// 这样 "https://x.com/embeddings"、"https://x.com/"、"https://x.com/embeddings/"
// 三者归一到 "https://x.com"。
//
// 对齐 Python OpenAICompatibleEmbeddingProvider.normalize_base_url（embeddings.py:51）。
func NormalizeEmbeddingBaseURL(baseURL string) string {
	url := strings.TrimSpace(baseURL)
	// 先收敛尾部所有斜杠，否则 ".../embeddings/" 会因 endswith 判断落空而漏剥
	for strings.HasSuffix(url, "/") {
		url = url[:len(url)-1]
	}
	if strings.HasSuffix(url, "/embeddings") {
		url = url[:len(url)-len("/embeddings")]
	}
	return url
}

// EmbedQuery 返回基于文本 hash 的 128 维确定性随机向量。
// Python: MockEmbeddingProvider.embed_query — random.seed(md5(text).hexdigest()), [random.uniform(-1,1) for _ in range(128)]
func (m *MockEmbeddingProvider) EmbedQuery(_ context.Context, text string) ([]float64, error) {
	h := md5.Sum([]byte(text))
	// Python 用完整 32 位 hex md5 做 seed：random.seed(md5(text).hexdigest())
	hexStr := hex.EncodeToString(h[:])
	seed := int64(binary.BigEndian.Uint64([]byte(hexStr[:8])))
	r := rand.New(rand.NewSource(seed))
	vec := make([]float64, 128)
	for i := range vec {
		vec[i] = r.Float64()*2 - 1 // 均匀分布 uniform(-1, 1)
	}
	return vec, nil
}

// EmbedDocuments 批量嵌入文档。
// Python: MockEmbeddingProvider.embed_documents
func (m *MockEmbeddingProvider) EmbedDocuments(ctx context.Context, texts []string) ([][]float64, error) {
	result := make([][]float64, len(texts))
	for i, text := range texts {
		vec, err := m.EmbedQuery(ctx, text)
		if err != nil {
			return nil, err
		}
		result[i] = vec
	}
	return result, nil
}

// ID 返回提供者标识
func (m *MockEmbeddingProvider) ID() string { return m.id }

// Model 返回提供者模型名
func (m *MockEmbeddingProvider) Model() string { return m.model }

// Dims 返回嵌入向量维度
func (m *MockEmbeddingProvider) Dims() int { return m.dims }

// ConfigFingerprint 返回配置指纹。
// Mock 提供者无外部配置，沿用基类形态 `id:model`。对齐 Python EmbeddingProvider.config_fingerprint
func (m *MockEmbeddingProvider) ConfigFingerprint() string {
	return fmt.Sprintf("%s:%s", m.id, m.model)
}

// ResolveEmbeddingConfigFromEnv 从环境变量构建 EmbeddingConfig。对齐 Python resolve_embedding_config_from_env
func ResolveEmbeddingConfigFromEnv(modelName, fallbackBaseURL, fallbackAPIKey string) *apiEmbedding.EmbeddingConfig {
	envModelName := os.Getenv("EMBEDDING_MODEL_NAME")
	if envModelName == "" {
		envModelName = modelName
	}
	baseURL := os.Getenv("EMBEDDING_BASE_URL")
	if baseURL == "" {
		baseURL = fallbackBaseURL
	}
	apiKey := os.Getenv("EMBEDDING_API_KEY")
	if apiKey == "" {
		apiKey = fallbackAPIKey
	}
	// Python: if model_name and base_url and api_key → 返回配置，否则返回 None
	if envModelName == "" || baseURL == "" || apiKey == "" {
		return nil
	}
	return &apiEmbedding.EmbeddingConfig{
		ModelName: envModelName,
		BaseURL:   baseURL,
		APIKey:    apiKey,
	}
}

// CreateEmbeddingProvider 根据配置创建嵌入提供者。对齐 Python create_embedding_provider
//
// 未配置 embedding（无 embeddingConfig 或 api_key 为空）时**返回 (nil, nil)**，表示
// "记忆系统以 FTS5 关键词检索模式运行"。绝不自动回落 mock：mock 产出的伪随机向量会
// 污染检索结果（向量侧权重 0.7），并写进 chunks.embedding 与 embedding_cache 持久化。
// 需要 mock 必须显式把 fallback 配成 "mock"。
//
// 对齐 Python _initialize_provider（manager.py:648-659）。
func CreateEmbeddingProvider(provider, model, fallback string, embeddingConfig *apiEmbedding.EmbeddingConfig) (EmbeddingProvider, error) {
	// 显式指定 mock 时仍然给 mock（供测试与本地联调）
	if provider == "mock" || fallback == "mock" {
		return NewMockEmbeddingProvider(), nil
	}

	// 已配置 api_key → 建真实提供者
	if embeddingConfig != nil && embeddingConfig.APIKey != "" {
		// Python: base_url 以 /embeddings 结尾时裁剪（归一化后参与 fingerprint）
		normalized := NormalizeEmbeddingBaseURL(embeddingConfig.BaseURL)
		embeddingConfig.BaseURL = normalized
		base := apiEmbedding.NewAPIEmbedding(*embeddingConfig)
		keyHash := sha256.Sum256([]byte(embeddingConfig.APIKey))
		return &baseEmbeddingAdapter{
			base:  base,
			prov:  provider,
			model: model,
			dims:  1024,
			// 归一化端点与 key 哈希参与 config fingerprint（Python: config_fingerprint）
			baseURL:    normalized,
			apiKeyHash: hex.EncodeToString(keyHash[:])[:16],
		}, nil
	}

	// 未配置 embedding：退回关键词检索，交 nil 让调用方进入 FTS5-only 模式
	logger.Info(logComponent).
		Str("event_type", "memory_init").
		Str("provider", provider).
		Str("model", model).
		Msg("embedding api key not configured, falling back to FTS5 keyword search only")
	return nil, nil
}

// EmbedQuery baseEmbeddingAdapter 的 EmbedQuery 实现，委托给 base。
func (a *baseEmbeddingAdapter) EmbedQuery(ctx context.Context, text string) ([]float64, error) {
	return a.base.EmbedQuery(ctx, text)
}

// EmbedDocuments baseEmbeddingAdapter 的 EmbedDocuments 实现，委托给 base。
func (a *baseEmbeddingAdapter) EmbedDocuments(ctx context.Context, texts []string) ([][]float64, error) {
	return a.base.EmbedDocuments(ctx, texts)
}

// ID 返回提供者标识。
func (a *baseEmbeddingAdapter) ID() string { return a.prov }

// Model 返回提供者模型名。
func (a *baseEmbeddingAdapter) Model() string { return a.model }

// Dims 返回嵌入向量维度。
func (a *baseEmbeddingAdapter) Dims() int { return a.dims }

// ConfigFingerprint 返回覆盖全部影响向量结果字段的指纹。
//
// 组成：`{id}:{model}:{归一化 base_url}:{api_key 的 sha256 前 16 位}`。
// 端点 / 密钥 / 模型任一变化都会得出不同指纹，从而：
//   - 让 needsRebuildOnConfigChange 检测到配置变更并重建索引；
//   - 让 embedding_cache 按 (base_url, api_key, model) 隔离。
//
// 只存 api_key 的哈希，不落原文。
// 对齐 Python OpenAICompatibleEmbeddingProvider.config_fingerprint（embeddings.py:78）。
func (a *baseEmbeddingAdapter) ConfigFingerprint() string {
	return fmt.Sprintf("%s:%s:%s:%s", a.prov, a.model, a.baseURL, a.apiKeyHash)
}
