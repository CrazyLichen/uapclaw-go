package chunker

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ChunkerOptions 分块器工厂参数，对齐 Python get_chunker(**kwargs)。
type ChunkerOptions struct {
	// ChunkSize 分块大小（默认 512）
	ChunkSize int
	// ChunkOverlap 相邻块重叠（默认 50）
	ChunkOverlap int
	// InnerChunker hybrid 工厂的 inner 分块器（对齐 inner_chunker）
	InnerChunker Chunker
	// NoSplitWhen hybrid 单块判定谓词（对齐 no_split_when）
	NoSplitWhen func(*reranker.Document) bool
	// Extra 其他透传参数（对齐 **kwargs 剩余项）
	Extra map[string]any
}

// ChunkerOption 工厂可选参数函数。
type ChunkerOption func(*ChunkerOptions)

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultChunkSize 默认分块大小（对齐 Python CharChunker 默认值）
	defaultChunkSize = 512
	// defaultChunkOverlap 默认分块重叠
	defaultChunkOverlap = 50
)

// ChunkerFactory 分块器工厂签名。返回 error 承载构造校验失败
//（对齐 Python 工厂内构造抛 ValueError）。
//
// 错误风格说明：Python registry 系错误（ValueError/KeyError/TypeError）
// 是原生异常非 StatusCode，Go 侧按项目先例（retrieval/embedding/utils.go 等）
// 统一用 fmt.Errorf。
type ChunkerFactory func(opts ...ChunkerOption) (Chunker, error)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// chunkerRegistry 名称 → 工厂。并发访问由 registryMu 保护
	//（Python 无并发问题，Go 包级 map 需要互斥）
	chunkerRegistry = map[string]ChunkerFactory{}
	// registryMu registry 互斥锁
	registryMu sync.Mutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewChunkerOptions 构建带默认值的选项并应用可变参数。
func NewChunkerOptions(opts ...ChunkerOption) *ChunkerOptions {
	o := &ChunkerOptions{
		ChunkSize:    defaultChunkSize,
		ChunkOverlap: defaultChunkOverlap,
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithChunkSize 设置分块大小。
func WithChunkSize(size int) ChunkerOption {
	return func(o *ChunkerOptions) { o.ChunkSize = size }
}

// WithChunkOverlap 设置分块重叠。
func WithChunkOverlap(overlap int) ChunkerOption {
	return func(o *ChunkerOptions) { o.ChunkOverlap = overlap }
}

// WithInnerChunker 设置 hybrid 工厂的 inner 分块器。
func WithInnerChunker(c Chunker) ChunkerOption {
	return func(o *ChunkerOptions) { o.InnerChunker = c }
}

// WithNoSplitWhen 设置 hybrid 单块判定谓词。
func WithNoSplitWhen(fn func(*reranker.Document) bool) ChunkerOption {
	return func(o *ChunkerOptions) { o.NoSplitWhen = fn }
}

// WithExtra 设置透传参数（对齐 **kwargs）。
func WithExtra(key string, value any) ChunkerOption {
	return func(o *ChunkerOptions) {
		if o.Extra == nil {
			o.Extra = make(map[string]any)
		}
		o.Extra[key] = value
	}
}

// RegisterChunker 按名注册分块器工厂。
//
// 对齐 Python register_chunker：空名/重名（overwrite=false）报错。
//
// Python: chunker/__init__.py register_chunker
func RegisterChunker(name string, factory ChunkerFactory, overwrite bool) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("chunker name must be a non-empty string")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := chunkerRegistry[name]; exists && !overwrite {
		return fmt.Errorf("Chunker '%s' is already registered. Use overwrite to replace it", name)
	}
	chunkerRegistry[name] = factory
	return nil
}

// GetChunker 按名获取分块器实例。
//
// 对齐 Python get_chunker：未知名报错且错误信息含已注册名列表；
// 工厂返回 nil 视为类型错误（对齐 Python TypeError 断言）。
//
// Python: chunker/__init__.py get_chunker
func GetChunker(name string, opts ...ChunkerOption) (Chunker, error) {
	registryMu.Lock()
	factory, exists := chunkerRegistry[name]
	if !exists {
		names := make([]string, 0, len(chunkerRegistry))
		for k := range chunkerRegistry {
			names = append(names, k)
		}
		registryMu.Unlock()
		sort.Strings(names)
		return nil, fmt.Errorf("Unknown chunker: %s. Registered: %s", name, strings.Join(names, ", "))
	}
	registryMu.Unlock()

	chunker, err := factory(opts...)
	if err != nil {
		return nil, err
	}
	if chunker == nil {
		return nil, fmt.Errorf("Chunker entry '%s' must return a Chunker instance, got nil", name)
	}
	return chunker, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 内置注册。对齐 Python __init__.py 尾部的 register_chunker 调用。
func init() {
	// "char" 注册随 13.6 CharChunker 落地 ⤴️
	_ = RegisterChunker("hybrid", hybridFactory, false)
}

// hybridFactory hybrid 分块器工厂。
//
// 对齐 Python _hybrid_factory：
//   - inner 缺省：由 chunk_size/chunk_overlap 构造默认 CharChunker
//     （13.6 前占位报错 ⤴️），多余参数透传给 CharChunker（前向兼容）
//   - inner 已给：多余 kwargs 报错（TypeError 语义）；inner 必须实现 Chunker
//   - HybridChunker 实体在 13.8 回填 ⤴️，当前 inner 分支为占位
//
// Python: chunker/__init__.py _hybrid_factory
func hybridFactory(opts ...ChunkerOption) (Chunker, error) {
	o := NewChunkerOptions(opts...)
	if o.InnerChunker == nil {
		// ⤴️ 13.6：替换为 CharChunker 构造（多余 Extra 透传）
		return nil, fmt.Errorf("default inner chunker (char) not implemented until 13.6")
	}
	if len(o.Extra) > 0 {
		keys := make([]string, 0, len(o.Extra))
		for k := range o.Extra {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("Unknown kwargs for 'hybrid' chunker: %s", strings.Join(keys, ", "))
	}
	// ⤴️ 13.8：替换为 HybridChunker(inner, noSplitWhen) 构造
	_ = o.NoSplitWhen
	return nil, fmt.Errorf("HybridChunker not implemented until 13.8")
}
