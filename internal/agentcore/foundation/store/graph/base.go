package graph

import (
	"context"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/query"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseGraphStore 图存储基础接口
//
// 对应 Python: GraphStore Protocol (base_graph_store.py)
type BaseGraphStore interface {
	// Config 获取图存储配置
	Config() *GraphConfig
	// Rebuild 重建所有集合和索引
	Rebuild(ctx context.Context) error
	// Refresh 刷新数据（flush + compact）
	Refresh(ctx context.Context, opts ...Option) error
	// Close 关闭存储连接
	Close() error

	// AddEntity 添加实体，自动嵌入向量（除非 no_embed=true）
	AddEntity(ctx context.Context, entities []*Entity, opts ...Option) error
	// AddRelation 添加关系，自动嵌入向量（除非 no_embed=true）
	AddRelation(ctx context.Context, relations []*Relation, opts ...Option) error
	// AddEpisode 添加片段，自动嵌入向量（除非 no_embed=true）
	AddEpisode(ctx context.Context, episodes []*Episode, opts ...Option) error

	// Query 按ID或过滤表达式查询数据
	Query(ctx context.Context, collection string, opts ...Option) ([]map[string]any, error)
	// Delete 按ID或过滤表达式删除数据
	Delete(ctx context.Context, collection string, opts ...Option) error
	// IsEmpty 检查集合是否为空
	IsEmpty(ctx context.Context, collection string) (bool, error)

	// Search 混合搜索，支持 BFS 图扩展和可选 reranking
	Search(ctx context.Context, query string, opts ...Option) (map[string][]map[string]any, error)

	// AttachEmbedder 绑定嵌入模型
	AttachEmbedder(embedder embedding.BaseEmbedding)
}

// Option 函数式选项
type Option func(*Options)

// Options 图存储操作选项
type Options struct {
	// 写入选项
	Flush   bool
	Upsert  bool
	NoEmbed bool

	// 查询选项
	IDs           []any
	Expr          query.QueryExpr
	SilenceErrors bool

	// 搜索选项
	Collection     string
	K              int
	RankerConfig   BaseRankConfig
	Reranker       reranker.BaseReranker
	BFSDepth       int
	BFSK           int
	FilterExpr     query.QueryExpr
	OutputFields   []string
	QueryEmbedding []float64
	Language       string
	MinScore       float64
}

// GraphStoreFactory 图存储工厂（线程安全）
type GraphStoreFactory struct {
	mu       sync.RWMutex
	backends map[string]func(*GraphConfig) (BaseGraphStore, error)
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// EntityCollection 实体集合名称
	EntityCollection = "ENTITY_COLLECTION"
	// RelationCollection 关系集合名称
	RelationCollection = "RELATION_COLLECTION"
	// EpisodeCollection 片段集合名称
	EpisodeCollection = "EPISODE_COLLECTION"
	// AllCollections 搜索全部集合
	AllCollections = "all"
	// DefaultWorkerNum 默认嵌入并发数
	DefaultWorkerNum = 10
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// globalFactory 全局图存储工厂
	globalFactory = &GraphStoreFactory{
		backends: make(map[string]func(*GraphConfig) (BaseGraphStore, error)),
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithFlush 设置写入后刷盘
func WithFlush(flush bool) Option {
	return func(o *Options) { o.Flush = flush }
}

// WithUpsert 设置覆盖写入
func WithUpsert(upsert bool) Option {
	return func(o *Options) { o.Upsert = upsert }
}

// WithNoEmbed 跳过自动嵌入
func WithNoEmbed(noEmbed bool) Option {
	return func(o *Options) { o.NoEmbed = noEmbed }
}

// WithIDs 按ID查询/删除
func WithIDs(ids ...any) Option {
	return func(o *Options) { o.IDs = ids }
}

// WithExpr 按过滤表达式查询/删除
func WithExpr(expr query.QueryExpr) Option {
	return func(o *Options) { o.Expr = expr }
}

// WithSilenceErrors 静默错误
func WithSilenceErrors(silence bool) Option {
	return func(o *Options) { o.SilenceErrors = silence }
}

// WithCollection 指定搜索集合
func WithCollection(collection string) Option {
	return func(o *Options) { o.Collection = collection }
}

// WithK 设置搜索返回数量
func WithK(k int) Option {
	return func(o *Options) { o.K = k }
}

// WithRankerConfig 设置排序策略
func WithRankerConfig(config BaseRankConfig) Option {
	return func(o *Options) { o.RankerConfig = config }
}

// WithReranker 设置重排序器
func WithReranker(r reranker.BaseReranker) Option {
	return func(o *Options) { o.Reranker = r }
}

// WithBFS 设置BFS图扩展参数
func WithBFS(depth, k int) Option {
	return func(o *Options) { o.BFSDepth = depth; o.BFSK = k }
}

// WithFilterExpr 设置搜索过滤表达式
func WithFilterExpr(expr query.QueryExpr) Option {
	return func(o *Options) { o.FilterExpr = expr }
}

// WithOutputFields 设置返回字段
func WithOutputFields(fields ...string) Option {
	return func(o *Options) { o.OutputFields = fields }
}

// WithQueryEmbedding 直接提供查询向量（跳过嵌入步骤）
func WithQueryEmbedding(emb []float64) Option {
	return func(o *Options) { o.QueryEmbedding = emb }
}

// WithLanguage 设置搜索语言
func WithLanguage(lang string) Option {
	return func(o *Options) { o.Language = lang }
}

// WithMinScore 设置最低相似度分数
func WithMinScore(score float64) Option {
	return func(o *Options) { o.MinScore = score }
}

// RegisterBackend 注册后端构造函数
func RegisterBackend(name string, constructor func(*GraphConfig) (BaseGraphStore, error), force ...bool) error {
	globalFactory.mu.Lock()
	defer globalFactory.mu.Unlock()

	if _, ok := globalFactory.backends[name]; ok {
		if len(force) == 0 || !force[0] {
			return exception.BuildError(exception.StatusStoreGraphBackendAlreadyExists,
				exception.WithParam("name", name))
		}
	}
	globalFactory.backends[name] = constructor
	return nil
}

// NewFromConfig 从配置创建图存储实例
func NewFromConfig(config *GraphConfig, backendName ...string) (BaseGraphStore, error) {
	globalFactory.mu.RLock()
	defer globalFactory.mu.RUnlock()

	name := config.Backend
	if len(backendName) > 0 && backendName[0] != "" {
		name = backendName[0]
	}
	constructor, ok := globalFactory.backends[name]
	if !ok {
		return nil, exception.BuildError(exception.StatusStoreGraphBackendNotFound,
			exception.WithParam("name", name))
	}
	return constructor(config)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newOptions 应用选项
func newOptions(opts ...Option) Options {
	var o Options
	for _, opt := range opts {
		opt(&o)
	}
	return o
}
