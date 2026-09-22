// Package config 提供记忆系统的配置类型定义。
//
// 包含图记忆（GraphMemory）相关的配置结构体、枚举类型和默认值。
// 这些配置类型控制图记忆的检索策略、添加记忆行为和搜索参数。
//
// 文件目录：
//
//	config/
//	├── doc.go              # 包文档
//	└── graph_config.go     # 图记忆配置类型（EpisodeType/BaseStrategy/AddMemStrategy/SearchConfig 等）
//
// 对应 Python 代码：openjiuwen/core/memory/config/graph.py
package config

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/graph"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/query"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseStrategy 基础检索策略
//
// Python: BaseStrategy (Pydantic BaseModel)
type BaseStrategy struct {
	// TopK 检索返回的最大结果数
	TopK int `json:"top_k"`
	// MinScore 最低相似度分数阈值
	MinScore float64 `json:"min_score"`
	// RankConfig 排序配置
	RankConfig graph.BaseRankConfig `json:"rank_config"`
}

// RetrievalStrategy 检索策略（扩展 BaseStrategy）
//
// Python: RetrievalStrategy (BaseStrategy)
type RetrievalStrategy struct {
	BaseStrategy
	// SameKind 是否只检索同类型对象
	SameKind bool `json:"same_kind"`
}

// EpisodeRetrievalStrategy Episode 检索策略（扩展 RetrievalStrategy）
//
// Python: EpisodeRetrievalStrategy (RetrievalStrategy)
type EpisodeRetrievalStrategy struct {
	RetrievalStrategy
	// ExcludeFutureResults 是否排除未来的结果
	ExcludeFutureResults bool `json:"exclude_future_results"`
}

// AddMemStrategy 添加记忆的策略配置
//
// Python: AddMemStrategy (Pydantic BaseModel)
type AddMemStrategy struct {
	// ChineseEntity 是否使用中文进行实体抽取（无论 Episode 语言，推荐小模型使用 True）
	ChineseEntity bool `json:"chinese_entity"`
	// ChineseEntityDedupe 是否使用中文进行实体去重
	ChineseEntityDedupe bool `json:"chinese_entity_dedupe"`
	// ChineseRelation 是否使用中文进行关系抽取（通常不推荐）
	ChineseRelation bool `json:"chinese_relation"`
	// SkipUUIDDedupe 是否跳过 UUID 去重
	SkipUUIDDedupe bool `json:"skip_uuid_dedupe"`
	// RecallEpisode Episode 检索策略
	RecallEpisode EpisodeRetrievalStrategy `json:"recall_episode"`
	// RecallEntity 实体检索策略
	RecallEntity RetrievalStrategy `json:"recall_entity"`
	// RecallRelation 关系检索策略
	RecallRelation RetrievalStrategy `json:"recall_relation"`
	// SummaryTarget 实体摘要目标字数
	SummaryTarget int `json:"summary_target"`
	// MergeEntities 是否执行实体合并
	MergeEntities bool `json:"merge_entities"`
	// MergeRelations 是否执行关系合并
	MergeRelations bool `json:"merge_relations"`
	// MergeFilter 是否在实体合并后过滤关系
	MergeFilter bool `json:"merge_filter"`
}

// SearchConfig 搜索配置（扩展 BaseStrategy）
//
// Python: SearchConfig (BaseStrategy)
type SearchConfig struct {
	BaseStrategy
	// BFSK BFS 图扩展的 K 值
	BFSK int `json:"bfs_k"`
	// BFSDepth BFS 图扩展的深度
	BFSDepth int `json:"bfs_depth"`
	// FilterExpr 过滤表达式
	FilterExpr query.QueryExpr `json:"filter_expr,omitempty"`
	// OutputFields 输出字段列表
	OutputFields []string `json:"output_fields,omitempty"`
	// Rerank 是否使用 Reranker 重排
	Rerank bool `json:"rerank"`
	// Language 搜索语言（cn 或 en）
	Language string `json:"language"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// EpisodeType 片段类型枚举
//
// Python: EpisodeType (Enum)
type EpisodeType int

const (
	// EpisodeTypeConversation 对话类型
	EpisodeTypeConversation EpisodeType = 0
	// EpisodeTypeDocument 文档类型
	EpisodeTypeDocument EpisodeType = 1
	// EpisodeTypeJSON JSON 类型
	EpisodeTypeJSON EpisodeType = 2
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// DefaultBaseStrategy BaseStrategy 默认值
	//
	// Python: BaseStrategy()
	DefaultBaseStrategy = BaseStrategy{
		TopK:       3,
		MinScore:   0.3,
		RankConfig: graph.NewRRFRankConfig(),
	}

	// DefaultRetrievalStrategy RetrievalStrategy 默认值
	//
	// Python: RetrievalStrategy()
	DefaultRetrievalStrategy = RetrievalStrategy{
		BaseStrategy: BaseStrategy{
			TopK:       3,
			MinScore:   0.3,
			RankConfig: graph.NewRRFRankConfig(),
		},
		SameKind: false,
	}

	// DefaultEpisodeRetrievalStrategy EpisodeRetrievalStrategy 默认值
	//
	// Python: EpisodeRetrievalStrategy()
	DefaultEpisodeRetrievalStrategy = EpisodeRetrievalStrategy{
		RetrievalStrategy: RetrievalStrategy{
			BaseStrategy: BaseStrategy{
				TopK:       3,
				MinScore:   0.025,
				RankConfig: graph.NewRRFRankConfig(),
			},
			SameKind: false,
		},
		ExcludeFutureResults: true,
	}

	// DefaultAddMemStrategy AddMemStrategy 默认值
	//
	// Python: DEFAULT_STRATEGY = AddMemStrategy()
	DefaultAddMemStrategy = AddMemStrategy{
		ChineseEntity:       true,
		ChineseEntityDedupe: false,
		ChineseRelation:     false,
		SkipUUIDDedupe:      false,
		RecallEpisode: EpisodeRetrievalStrategy{
			RetrievalStrategy: RetrievalStrategy{
				BaseStrategy: BaseStrategy{
					TopK:       3,
					MinScore:   0.025,
					RankConfig: graph.NewRRFRankConfig(),
				},
				SameKind: false,
			},
			ExcludeFutureResults: true,
		},
		RecallEntity: RetrievalStrategy{
			BaseStrategy: BaseStrategy{
				TopK:     3,
				MinScore: 0.1,
				RankConfig: &graph.WeightedRankConfig{
					NameDense:     0.7,
					ContentDense:  0.1,
					ContentSparse: 0.2,
				},
			},
			SameKind: false,
		},
		RecallRelation: RetrievalStrategy{
			BaseStrategy: BaseStrategy{
				TopK:       3,
				MinScore:   0.02,
				RankConfig: graph.NewRRFRankConfig(),
			},
			SameKind: false,
		},
		SummaryTarget:  250,
		MergeEntities:  true,
		MergeRelations: true,
		MergeFilter:    true,
	}

	// DefaultSearchConfig SearchConfig 默认值
	//
	// Python: SearchConfig()
	DefaultSearchConfig = SearchConfig{
		BaseStrategy: BaseStrategy{
			TopK:       3,
			MinScore:   0.3,
			RankConfig: graph.NewRRFRankConfig(),
		},
		BFSK:         3,
		BFSDepth:     0,
		FilterExpr:   nil,
		OutputFields: nil,
		Rerank:       false,
		Language:     "en",
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// String 返回 EpisodeType 的字符串表示
//
// Python: EpisodeType.name
func (e EpisodeType) String() string {
	switch e {
	case EpisodeTypeConversation:
		return "CONVERSATION"
	case EpisodeTypeDocument:
		return "DOCUMENT"
	case EpisodeTypeJSON:
		return "JSON"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", int(e))
	}
}

// Value 返回 EpisodeType 的整数值
func (e EpisodeType) Value() int {
	return int(e)
}

// NewAddMemStrategy 创建 AddMemStrategy（使用默认值）
//
// Python: AddMemStrategy()
func NewAddMemStrategy() *AddMemStrategy {
	s := DefaultAddMemStrategy
	return &s
}

// NewSearchConfig 创建 SearchConfig（使用默认值）
//
// Python: SearchConfig()
func NewSearchConfig() *SearchConfig {
	s := DefaultSearchConfig
	return &s
}

// ──────────────────────────── 非导出函数 ────────────────────────────
