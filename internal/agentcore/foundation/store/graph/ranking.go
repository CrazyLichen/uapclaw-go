package graph

import "sync"

// ──────────────────────────── 结构体 ────────────────────────────

// BaseRankConfig 排序基础配置接口
type BaseRankConfig interface {
	// Name 排序策略名称
	Name() string
	// HigherIsBetter 分数越高是否越好
	HigherIsBetter() bool
	// IsActive 各通道开关 [name_dense, content_dense, content_sparse]
	IsActive() [3]int
	// Args 返回构建排序器所需的位置参数和关键字参数
	Args() ([]any, map[string]any)
}

// WeightedRankConfig 加权排序配置
type WeightedRankConfig struct {
	// NameDense 名称向量权重
	NameDense float64 `json:"name_dense"`
	// ContentDense 内容向量权重
	ContentDense float64 `json:"content_dense"`
	// ContentSparse 内容BM25稀疏权重
	ContentSparse float64 `json:"content_sparse"`
}

// RRFRankConfig 倒数排名融合配置
type RRFRankConfig struct {
	// K RRF常数
	K int `json:"k"`
	// NameDense 是否包含名称向量通道
	NameDense bool `json:"name_dense"`
	// ContentDense 是否包含内容向量通道
	ContentDense bool `json:"content_dense"`
	// ContentSparse 是否包含BM25稀疏通道
	ContentSparse bool `json:"content_sparse"`
}

// RankerRegistry 排序器注册表（线程安全）
type RankerRegistry struct {
	mu       sync.RWMutex
	backends map[string]map[string]any
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultWeightNameDense 默认名称向量权重
	DefaultWeightNameDense = 0.15
	// DefaultWeightContentDense 默认内容向量权重
	DefaultWeightContentDense = 0.60
	// DefaultWeightContentSparse 默认内容稀疏权重
	DefaultWeightContentSparse = 0.25
	// DefaultRRFK 默认RRF常数
	DefaultRRFK = 40
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// GlobalRankerRegistry 全局排序器注册表
	GlobalRankerRegistry = NewRankerRegistry()
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWeightedRankConfig 创建带默认值的加权排序配置
func NewWeightedRankConfig() *WeightedRankConfig {
	return &WeightedRankConfig{
		NameDense:     DefaultWeightNameDense,
		ContentDense:  DefaultWeightContentDense,
		ContentSparse: DefaultWeightContentSparse,
	}
}

// NewRRFRankConfig 创建带默认值的RRF排序配置
func NewRRFRankConfig() *RRFRankConfig {
	return &RRFRankConfig{
		K:              DefaultRRFK,
		NameDense:      true,
		ContentDense:   true,
		ContentSparse:  true,
	}
}

// Name 排序策略名称
func (w *WeightedRankConfig) Name() string { return "weighted" }

// HigherIsBetter 分数越高是否越好
func (w *WeightedRankConfig) HigherIsBetter() bool { return false }

// IsActive 各通道开关
func (w *WeightedRankConfig) IsActive() [3]int {
	return [3]int{
		boolToInt(w.NameDense > 0),
		boolToInt(w.ContentDense > 0),
		boolToInt(w.ContentSparse > 0),
	}
}

// Args 返回归一化权重
func (w *WeightedRankConfig) Args() ([]any, map[string]any) {
	// 归一化权重：过滤零值后除以总和
	var weights []float64
	var total float64
	for _, v := range []float64{w.NameDense, w.ContentDense, w.ContentSparse} {
		if v > 0 {
			weights = append(weights, v)
			total += v
		}
	}
	if total > 0 {
		for i := range weights {
			weights[i] /= total
		}
	}
	positional := make([]any, len(weights))
	for i, v := range weights {
		positional[i] = v
	}
	return positional, map[string]any{}
}

// Name 排序策略名称
func (r *RRFRankConfig) Name() string { return "rrf" }

// HigherIsBetter 分数越高是否越好
func (r *RRFRankConfig) HigherIsBetter() bool { return true }

// IsActive 各通道开关
func (r *RRFRankConfig) IsActive() [3]int {
	return [3]int{
		boolToInt(r.NameDense),
		boolToInt(r.ContentDense),
		boolToInt(r.ContentSparse),
	}
}

// Args 返回RRF参数
func (r *RRFRankConfig) Args() ([]any, map[string]any) {
	return []any{r.K}, map[string]any{}
}

// NewRankerRegistry 创建排序器注册表
func NewRankerRegistry() *RankerRegistry {
	return &RankerRegistry{
		backends: make(map[string]map[string]any),
	}
}

// Register 注册某后端的排序器构造函数
func (r *RankerRegistry) Register(backend string, rankers map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.backends[backend] = rankers
}

// GetRanker 获取指定后端和策略的排序器
func (r *RankerRegistry) GetRanker(backend, strategy string) (any, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rankers, ok := r.backends[backend]
	if !ok {
		return nil, false
	}
	v, ok := rankers[strategy]
	return v, ok
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// boolToInt 布尔值转整数
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
